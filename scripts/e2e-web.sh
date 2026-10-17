#!/usr/bin/env bash
# End-to-end check of deck-web against a throwaway tuios daemon: the JSON
# API, the event stream and the WebSocket pane stream. Every check prints
# PASS or FAIL; the exit status is the number of failures. Responses are
# kept under $SCRATCH/out.
set -uo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
DECKWEB=${DECKWEB:-$here/.dev/deck-web}
PORT=${PORT:-18787}
SCRATCH=${SCRATCH:-${TMPDIR:-/tmp}/tuios-deck-web-e2e-$(id -u)}
rm -rf "$SCRATCH"; mkdir -p "$SCRATCH/out" "$SCRATCH/captures"
fails=0
check() { if grep -q -- "$3" "$2"; then echo "PASS $1"; else echo "FAIL $1: no '$3' in $2"; fails=$((fails+1)); fi; }

isolate() {
	local dir=$1
	mkdir -p "$dir"/{run,config/tuios,state,cache,data,tmp}
	chmod 700 "$dir/run"
	export XDG_RUNTIME_DIR="$dir/run" XDG_CONFIG_HOME="$dir/config" \
		XDG_CONFIG_DIRS="$dir/config" XDG_STATE_HOME="$dir/state" \
		XDG_CACHE_HOME="$dir/cache" XDG_DATA_HOME="$dir/data" TMPDIR="$dir/tmp"
	unset TUIOS_SESSION TUIOS_PANE_ID TUIOS_PANE_TOKEN TUIOS_WINDOW_ID \
		TUIOS_SOCKET TUIOS_HOST TUIOS_SSH TUIOS_ENV TUIOS_SERVER
}
A="$SCRATCH/A"
a() { (isolate "$A"; tuios "$@"); }
cleanup() { [ -n "${webpid:-}" ] && kill "$webpid" 2>/dev/null; a kill-server >/dev/null 2>&1; }
trap cleanup EXIT
base="http://127.0.0.1:$PORT"
J='Content-Type: application/json'

echo "== daemon A with a shell and a fake agent"
a new target --detach --cwd "$SCRATCH" >/dev/null
a new-window -s target build --cwd "$SCRATCH" --no-focus >/dev/null
a send-text -s target -w build 'echo hello-from-build
'
a new-window -s target fake --no-focus -- sh -c 'tuios set-agent-state idle -s "$TUIOS_SESSION" -w "$TUIOS_PANE_ID" --harness claude-code -m "fake agent at rest"; exec sleep 600' >/dev/null
a wait-for agent-state -s target -w fake --until idle --timeout 10000 >/dev/null
ASOCK="$A/run/tuios/tuios.sock"
BUILD=$(a list-windows -s target --json | python3 -c 'import json,sys; print([w["window_id"] for w in json.load(sys.stdin)["windows"] if w["title"]=="build"][0])')
FAKE=$(a list-windows -s target --json | python3 -c 'import json,sys; print([w["window_id"] for w in json.load(sys.stdin)["windows"] if w["title"]=="fake"][0])')

if lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1; then
	echo "FAIL port $PORT is already in use; a stale deck-web? (lsof -nP -iTCP:$PORT)"; exit 1
fi
echo "== deck-web, outside every pane of A"
(unset TUIOS_ENV TUIOS_SESSION TUIOS_PANE_ID TUIOS_PANE_TOKEN TUIOS_SOCKET; exec "$DECKWEB" --listen "127.0.0.1:$PORT" --socket "$ASOCK" --captures "$SCRATCH/captures" > "$SCRATCH/out/web.log" 2>&1) &
webpid=$!
for i in $(seq 1 50); do curl -sf "$base/api/state" > "$SCRATCH/out/state.json" 2>/dev/null && break; sleep 0.2; done
check "state lists the session" "$SCRATCH/out/state.json" '"name":"target"'
check "state lists the fake agent as idle" "$SCRATCH/out/state.json" '"name":"fake","state":"idle"'
check "presence held, transcripts on" "$SCRATCH/out/state.json" '"transcripts":true'
curl -s "$base/" > "$SCRATCH/out/index.html"
check "page served" "$SCRATCH/out/index.html" '<title>tuios deck</title>'

echo "== detail endpoints"
curl -s "$base/api/panes/target/$BUILD/screen?lines=50" > "$SCRATCH/out/screen.json"
check "screen capture" "$SCRATCH/out/screen.json" 'hello-from-build'
curl -s "$base/api/panes/target/$FAKE/activity" > "$SCRATCH/out/activity.json"
check "activity recap" "$SCRATCH/out/activity.json" '"state":"idle"'
curl -s "$base/api/panes/target/$FAKE/transcript" > "$SCRATCH/out/transcript.json"
check "transcript answered (no presence refusal)" "$SCRATCH/out/transcript.json" '"lines"'
if grep -q refused "$SCRATCH/out/transcript.json"; then echo "FAIL transcript refused"; fails=$((fails+1)); fi
curl -s "$base/api/dirs?path=$SCRATCH" > "$SCRATCH/out/dirs.json"
check "folder picker lists subfolders" "$SCRATCH/out/dirs.json" '"out"'

echo "== events"
(curl -sN --max-time 6 "$base/api/events" > "$SCRATCH/out/events.txt") &
evpid=$!
sleep 1
curl -s -X POST -H "$J" -d "{\"text\":\"echo from-web-prompt\",\"is_agent\":false}" "$base/api/panes/target/$BUILD/prompt" > "$SCRATCH/out/prompt.json"
check "prompt to a terminal" "$SCRATCH/out/prompt.json" '"status":"sent"'
a wait-for window-output -s target -w build --pattern 'from-web-prompt' --timeout 10000 >/dev/null && echo "PASS prompt reached the pane" || { echo "FAIL prompt never reached build"; fails=$((fails+1)); }
curl -s -X POST -H "$J" -d '{"text":"do the thing","is_agent":true}' "$base/api/panes/target/$FAKE/prompt" > "$SCRATCH/out/queue.json"
check "prompt to an agent is queued" "$SCRATCH/out/queue.json" '"status":"prompt \(queued\|delivering\)'
curl -s -X POST -H "$J" -d "{\"session\":\"target\",\"name\":\"extra\",\"cwd\":\"$SCRATCH\"}" "$base/api/terminals" > "$SCRATCH/out/terminal.json"
check "new terminal" "$SCRATCH/out/terminal.json" '"status":"opened extra'
curl -s -X POST -H "$J" -d "{\"session\":\"fresh\",\"name\":\"first\",\"cwd\":\"$SCRATCH\",\"cols\":100,\"rows\":30}" "$base/api/terminals" > "$SCRATCH/out/terminal2.json"
check "new terminal creates a missing session" "$SCRATCH/out/terminal2.json" '"status":"opened first'
wait "$evpid"
check "event stream opened" "$SCRATCH/out/events.txt" 'event: open'
check "event stream carried window-created" "$SCRATCH/out/events.txt" 'event: window-created'

echo "== capture, agent, close"
curl -s -X POST "$base/api/panes/target/$BUILD/capture?name=build" > "$SCRATCH/out/capture.json"
check "capture written" "$SCRATCH/out/capture.json" '"path"'
grep -q 'hello-from-build' "$SCRATCH"/captures/target-build-*.txt && echo "PASS capture file holds the pane" || { echo "FAIL capture file"; fails=$((fails+1)); }
curl -s --max-time 150 -X POST -H "$J" -d "{\"session\":\"target\",\"agent\":\"sleep 600\",\"name\":\"sleeper\",\"cwd\":\"$SCRATCH\",\"ready_timeout\":5000}" "$base/api/agents" > "$SCRATCH/out/agent.json"
check "start-agent answered" "$SCRATCH/out/agent.json" '"status":"sleeper'
curl -s -X DELETE "$base/api/panes/target/$BUILD" > "$SCRATCH/out/close.json"
check "close pane" "$SCRATCH/out/close.json" '"status":"closed"'
a list-windows -s target --json > "$SCRATCH/out/windows-after.json"
if grep -q '"build"' "$SCRATCH/out/windows-after.json"; then echo "FAIL build still open"; fails=$((fails+1)); else echo "PASS build is gone"; fi

echo "== websocket pane stream"
EXTRA=$(a list-windows -s target --json | python3 -c 'import json,sys; print([w["window_id"] for w in json.load(sys.stdin)["windows"] if w["title"]=="extra"][0])')
(cd "$here" && GOWORK=off go run ./scripts/wscheck "ws://127.0.0.1:$PORT/api/panes/target/$EXTRA/stream?cols=100&rows=30" ws-marker-7) | tee "$SCRATCH/out/ws.txt"
grep -q 'FAIL' "$SCRATCH/out/ws.txt" && fails=$((fails+1))
grep -q 'PASS output' "$SCRATCH/out/ws.txt" || fails=$((fails+1))

echo; echo "responses in $SCRATCH/out; $fails failure(s)"
exit $fails
