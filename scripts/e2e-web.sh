#!/usr/bin/env bash
# End-to-end check of `deck web` against a throwaway tuios daemon: the JSON
# API, the event stream and the WebSocket pane stream. Every check prints
# PASS or FAIL; the exit status is the number of failures. Responses are
# kept under $SCRATCH/out.
set -uo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
DECK=${DECK:-$here/.dev/deck}
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
(unset TUIOS_ENV TUIOS_SESSION TUIOS_PANE_ID TUIOS_PANE_TOKEN TUIOS_SOCKET; export TUIOS_DECK_DATA="$SCRATCH/deck.json"; exec "$DECK" web --listen "127.0.0.1:$PORT" --socket "$ASOCK" --captures "$SCRATCH/captures" > "$SCRATCH/out/web.log" 2>&1) &
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

echo "== projects, tasks, assignments"
field() { python3 -I -c 'import json,sys; d=json.load(open(sys.argv[1]))
for k in sys.argv[2:]: d=d[int(k)] if isinstance(d,list) else d[k]
print(d)' "$@"; }
curl -s -X POST -H "$J" -d '{"name":"deckproj","theme":{"color":"blue"}}' "$base/api/projects" > "$SCRATCH/out/project.json"
check "create project" "$SCRATCH/out/project.json" '"name":"deckproj"'
check "project theme has css" "$SCRATCH/out/project.json" '"css":"#0000ee"'
PID=$(field "$SCRATCH/out/project.json" project id)
curl -s -X POST -H "$J" -d "{\"project_id\":\"$PID\",\"title\":\"palette\"}" "$base/api/tasks" > "$SCRATCH/out/task.json"
check "create task" "$SCRATCH/out/task.json" '"path":"deckproj / palette"'
TID=$(field "$SCRATCH/out/task.json" task id)
curl -s -X POST -H "$J" -d "{\"parent_id\":\"$TID\",\"title\":\"autocomplete\"}" "$base/api/tasks" > "$SCRATCH/out/subtask.json"
check "subtask has parent_id" "$SCRATCH/out/subtask.json" "\"parent_id\":\"$TID\""
check "subtask project_id resolved through its root" "$SCRATCH/out/subtask.json" "\"project_id\":\"$PID\""
check "subtask own_project_id is empty" "$SCRATCH/out/subtask.json" '"own_project_id":""'
check "subtask path and depth" "$SCRATCH/out/subtask.json" '"path":"deckproj / palette / autocomplete"'
check "subtask depth" "$SCRATCH/out/subtask.json" '"depth":1'
SID=$(field "$SCRATCH/out/subtask.json" task id)
curl -s -X PATCH -H "$J" -d '{"status":"done"}' "$base/api/tasks/$SID" > "$SCRATCH/out/task-patch.json"
check "PATCH task status" "$SCRATCH/out/task-patch.json" '"status":"done"'
curl -s -X PATCH -H "$J" -d '{"status":"maybe"}' "$base/api/tasks/$SID" > "$SCRATCH/out/task-patch-bad.json"
check "PATCH bad status refused" "$SCRATCH/out/task-patch-bad.json" '"error":"status'
curl -s "$base/api/tasks?project=$PID" > "$SCRATCH/out/tasks.json"
check "tasks filtered by project" "$SCRATCH/out/tasks.json" '"title":"autocomplete"'
curl -s "$base/api/state" > "$SCRATCH/out/state-store.json"
check "state carries projects" "$SCRATCH/out/state-store.json" '"projects":\[{.*"name":"deckproj"'
check "state carries tasks" "$SCRATCH/out/state-store.json" '"tasks":\[{.*"title":"palette"'
curl -s -X POST -H "$J" -d "{\"session\":\"target\",\"name\":\"tasked\",\"cwd\":\"$SCRATCH\",\"task\":\"autocomplete\"}" "$base/api/terminals" > "$SCRATCH/out/terminal-task.json"
check "new terminal assigned to a task by title" "$SCRATCH/out/terminal-task.json" 'deckproj › palette › autocomplete'
TASKED=$(field "$SCRATCH/out/terminal-task.json" result window_id)
curl -s -X POST -H "$J" -d "{\"session\":\"target\",\"name\":\"untargeted\",\"cwd\":\"$SCRATCH\",\"task\":\"\",\"project\":\"\"}" "$base/api/terminals" > "$SCRATCH/out/terminal-empty.json"
check "new terminal with empty target refused" "$SCRATCH/out/terminal-empty.json" 'a task or a project is required'
curl -s "$base/api/state" > "$SCRATCH/out/state-assigned.json"
python3 -I -c 'import json,sys; d=json.load(open(sys.argv[1]))
print(json.dumps([p["assignment"] for s in d["sessions"] for p in s["panes"] if p["id"]==sys.argv[2]]))' "$SCRATCH/out/state-assigned.json" "$TASKED" > "$SCRATCH/out/pane-assignment.json"
check "state pane shows its task" "$SCRATCH/out/pane-assignment.json" '"task": "autocomplete"'
check "state pane shows its project" "$SCRATCH/out/pane-assignment.json" '"project": "deckproj"'
check "state pane shows its top task" "$SCRATCH/out/pane-assignment.json" '"top": "palette"'
curl -s -X PUT -H "$J" -d '{"project":"deckproj"}' "$base/api/panes/target/$TASKED/assignment" > "$SCRATCH/out/assign.json"
check "PUT assignment to a project" "$SCRATCH/out/assign.json" '"project":"deckproj"'
check "PUT assignment to a project drops the task" "$SCRATCH/out/assign.json" '"task_id":""'
curl -s -X PUT -H "$J" -d '{}' "$base/api/panes/target/$TASKED/assignment" > "$SCRATCH/out/assign-bad.json"
check "PUT assignment without target refused" "$SCRATCH/out/assign-bad.json" 'a task or a project is required'
curl -s "$base/api/assignments" > "$SCRATCH/out/assignments.json"
check "assignments lists the pane" "$SCRATCH/out/assignments.json" "\"$TASKED\":{\"project_id\":\"$PID\",\"resolved\":{\"project\":\"deckproj\""
curl -s -X DELETE "$base/api/panes/target/$TASKED/assignment" > "$SCRATCH/out/unassign.json"
check "DELETE assignment" "$SCRATCH/out/unassign.json" '"status":"unassigned"'
curl -s "$base/api/assignments" > "$SCRATCH/out/assignments-after.json"
if grep -q "$TASKED" "$SCRATCH/out/assignments-after.json"; then echo "FAIL assignment still listed"; fails=$((fails+1)); else echo "PASS assignment is gone"; fi

echo "== captures api"
curl -s "$base/api/captures" > "$SCRATCH/out/captures.json"
check "captures lists the file" "$SCRATCH/out/captures.json" '"name":"target-build-'
check "captures parsed the session" "$SCRATCH/out/captures.json" '"session":"target","pane":"build"'
CAP=$(field "$SCRATCH/out/captures.json" captures 0 name)
curl -s "$base/api/captures/$CAP" > "$SCRATCH/out/capture-read.json"
check "capture content" "$SCRATCH/out/capture-read.json" 'hello-from-build'
curl -s "$base/api/captures/a%2Fb.txt" > "$SCRATCH/out/capture-bad.json"
check "capture name with a separator refused" "$SCRATCH/out/capture-bad.json" 'bare file name'
curl -s -X DELETE "$base/api/captures/$CAP" > "$SCRATCH/out/capture-delete.json"
check "capture deleted" "$SCRATCH/out/capture-delete.json" '"status":"deleted '
if [ -e "$SCRATCH/captures/$CAP" ]; then echo "FAIL capture file still there"; fails=$((fails+1)); else echo "PASS capture file is gone"; fi

echo "== delete project cascades"
curl -s -X PUT -H "$J" -d "{\"task_id\":\"$SID\"}" "$base/api/panes/target/$TASKED/assignment" > "$SCRATCH/out/assign2.json"
check "PUT assignment by task_id" "$SCRATCH/out/assign2.json" '"task":"autocomplete"'
curl -s -X DELETE "$base/api/projects/$PID" > "$SCRATCH/out/project-delete.json"
check "DELETE project reports its tasks" "$SCRATCH/out/project-delete.json" '"tasks":2'
check "DELETE project reports its assignments" "$SCRATCH/out/project-delete.json" '"assignments":1'
curl -s "$base/api/tasks" > "$SCRATCH/out/tasks-after.json"
check "tasks gone with the project" "$SCRATCH/out/tasks-after.json" '"tasks":\[\]'
curl -s "$base/api/assignments" > "$SCRATCH/out/assignments-cascade.json"
check "assignments gone with the project" "$SCRATCH/out/assignments-cascade.json" '"assignments":{}'
grep -q '"version": 1' "$SCRATCH/deck.json" && echo "PASS store written under the scratch folder" || { echo "FAIL store file"; fails=$((fails+1)); }

echo "== palette, verbs and call"
curl -s "$base/api/palette" > "$SCRATCH/out/palette.json"
check "palette lists blue with its css" "$SCRATCH/out/palette.json" '{"css":"#0000ee","name":"blue"}'
curl -s "$base/api/verbs" > "$SCRATCH/out/verbs.json"
check "verbs lists list-sessions" "$SCRATCH/out/verbs.json" '"verb":"list-sessions"'
check "verbs carries param schemas" "$SCRATCH/out/verbs.json" '"params":\['
curl -s -X POST -H "$J" -d '{"verb":"list-sessions","params":{}}' "$base/api/call" > "$SCRATCH/out/call.json"
check "call list-sessions returns the session" "$SCRATCH/out/call.json" '"result":{.*"name":"target"'
curl -s -X POST -H "$J" -d '{"verb":"no-such-verb","params":{}}' "$base/api/call" > "$SCRATCH/out/call-unknown.json"
check "call refuses an unknown verb" "$SCRATCH/out/call-unknown.json" '"code":"unknown_verb"'
curl -s -X POST -H "$J" -d '{"verb":"list-windows","params":{"session":"nowhere"}}' "$base/api/call" > "$SCRATCH/out/call-daemon-err.json"
check "call passes the daemon error through" "$SCRATCH/out/call-daemon-err.json" '"code":"session_not_found"'
for id in page-deck page-tasks page-captures dlg-palette; do
	check "page has #$id" "$SCRATCH/out/index.html" "id=\"$id\""
done
check "page has the group select" "$SCRATCH/out/index.html" 'id="group"'

echo "== websocket pane stream"
EXTRA=$(a list-windows -s target --json | python3 -c 'import json,sys; print([w["window_id"] for w in json.load(sys.stdin)["windows"] if w["title"]=="extra"][0])')
(cd "$here" && GOWORK=off go run ./scripts/wscheck "ws://127.0.0.1:$PORT/api/panes/target/$EXTRA/stream?cols=100&rows=30" ws-marker-7) | tee "$SCRATCH/out/ws.txt"
grep -q 'FAIL' "$SCRATCH/out/ws.txt" && fails=$((fails+1))
grep -q 'PASS output' "$SCRATCH/out/ws.txt" || fails=$((fails+1))

echo; echo "responses in $SCRATCH/out; $fails failure(s)"
exit $fails
