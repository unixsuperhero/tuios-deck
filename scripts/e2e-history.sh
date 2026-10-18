#!/usr/bin/env bash
# Real daemon activity, OSC 133 commands, persistence and TUI navigation.
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
DECK=${DECK:-$here/.dev/deck}
SCRATCH=${SCRATCH:-/tmp/deck-history-$$}
PORT=${PORT:-18849}
mkdir -p "$SCRATCH/out" "$SCRATCH/captures"
isolate() {
 local dir=$1 var
 mkdir -p "$dir"/{run,config/tuios,state,cache,data,tmp}
 chmod 700 "$dir/run"
 for var in ${!TUIOS_@}; do unset "$var"; done
 export XDG_RUNTIME_DIR="$dir/run" XDG_CONFIG_HOME="$dir/config" XDG_CONFIG_DIRS="$dir/config" XDG_STATE_HOME="$dir/state" XDG_CACHE_HOME="$dir/cache" XDG_DATA_HOME="$dir/data" TMPDIR="$dir/tmp"
}
a() { (isolate "$SCRATCH/A"; tuios "$@"); }
b() { (isolate "$SCRATCH/B"; tuios "$@"); }
cleanup() { [ -z "${webpid:-}" ] || kill "$webpid" 2>/dev/null || true; a kill-server >/dev/null 2>&1 || true; b kill-server >/dev/null 2>&1 || true; }
trap cleanup EXIT
base="http://127.0.0.1:$PORT"
if lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1; then echo "FAIL port $PORT is occupied"; exit 1; fi
start_web() {
 (isolate "$SCRATCH/web"; exec "$DECK" web --socket "$SCRATCH/A/run/tuios/tuios.sock" --data "$SCRATCH/deck.json" --captures "$SCRATCH/captures" --listen "127.0.0.1:$PORT" > "$SCRATCH/out/web.log" 2>&1) &
 webpid=$!
}
# The persisted history is deck state, not a daemon wait-for condition.
wait_history() {
 local expression=$1 file=$2
 for ((i=0;i<100;i++)); do
  if curl -fsS "$base/api/history" > "$file" 2>/dev/null && python3 - "$file" "$expression" <<'PY'
import json,sys
r=json.load(open(sys.argv[1])); rows=r['entries']
sys.exit(0 if eval(sys.argv[2]) and not r['observation_error'] else 1)
PY
  then return 0; fi
  sleep .1
 done
 echo "FAIL history condition: $expression"; cat "$file"; return 1
}
activity() {
 local state=$1 kind=$2 text=$3
 a call set-agent-state "{\"session\":\"target\",\"window\":\"marks\",\"state\":\"$state\",\"activity\":{\"event\":\"$kind\",\"text\":\"$text\"}}" >/dev/null
}
command_marks() {
 a send-text -s target -w marks $'printf "\\033]133;A\\007\\033]133;B\\007printf history-command\\033]133;C\\007history-command\\n\\033]133;D;7\\007"\n'
 a wait-for window-output -s target -w marks --pattern history-command --timeout 10000 >/dev/null
}
a new target --detach --cwd "$SCRATCH" >/dev/null
a new-window -s target marks --no-focus -- bash --noprofile --norc >/dev/null
WIN=$(a list-windows -s target --json | python3 -c 'import json,sys; print(next(w["window_id"] for w in json.load(sys.stdin)["windows"] if w["title"]=="marks"))')
activity working prompt 'retained-history-prompt'
activity idle turn_end 'retained-history-response'
command_marks
start_web
wait_history 'any(e["kind"]=="command-completed" for e in rows) and any("retained-history-response" in e["detail"] for e in rows)' "$SCRATCH/out/recovered.json"
python3 - "$SCRATCH/out/recovered.json" <<'PY'
import json,sys
rows=json.load(open(sys.argv[1]))['entries']
assert [e['kind'] for e in rows if e['kind'].startswith('command-')]==['command-completed','command-submitted']
assert len([e for e in rows if e['kind']=='prompt-submitted'])==1
assert len([e for e in rows if e['kind']=='response-completed'])==1
assert 'Exit: 7' in next(e['detail'] for e in rows if e['kind']=='command-completed')
assert all(e['source_id'] and e['at']<=e['observed'] for e in rows)
print('PASS retained sources recover distinct submissions and completions with time, identity and exit status')
PY
activity working prompt 'live-history-prompt'
activity idle turn_end 'live-history-response'
command_marks
wait_history 'len([e for e in rows if e["kind"]=="command-completed"])==2 and any("live-history-response" in e["detail"] for e in rows)' "$SCRATCH/out/live.json"
python3 - "$SCRATCH/out/live.json" <<'PY'
import json,sys
rows=json.load(open(sys.argv[1]))['entries']
assert len([e for e in rows if e['kind']=='command-submitted'])==2
assert len([e for e in rows if e['kind']=='response-completed'])==2
assert len([e for e in rows if e["kind"]=="command-started" and e["source"]=="daemon"])==1
assert len(set(e['id'] for e in rows))==len(rows)
assert [e['at'] for e in rows]==sorted([e['at'] for e in rows],reverse=True)
print('PASS live observations and repeated commands retain distinct source identities')
PY
curl -fsS -H 'Content-Type: application/json' -d '{"text":"echo deck-history-input","is_agent":false}' "$base/api/panes/target/$WIN/prompt" > "$SCRATCH/out/input.json"
wait_history 'any(e["kind"]=="input-submitted" and "deck-history-input" in e["detail"] for e in rows)' "$SCRATCH/out/input-history.json"
echo 'PASS deck terminal submission is recorded as input, not command execution'
activity working prompt 'queue-target-busy'
curl -fsS -H 'Content-Type: application/json' -d '{"text":"queued-history-input","is_agent":true}' "$base/api/panes/target/$WIN/prompt" > "$SCRATCH/out/queued.json"
wait_history 'any(e["kind"]=="prompt-queued" and "queued-history-input" in e["detail"] for e in rows)' "$SCRATCH/out/queue-history.json"
echo 'PASS queue acceptance is recorded without claiming delivery'
curl -fsS "$base/api/history?kind=response-completed&q=retained" > "$SCRATCH/out/filtered.json"
python3 - "$SCRATCH/out/filtered.json" <<'PY'
import json,sys
rows=json.load(open(sys.argv[1]))['entries']
assert [e['summary'] for e in rows]==['Agent response completed: retained-history-response']
print('PASS history filters return the requested response')
PY
# Save a baseline after all events, then restart without changing the sources.
wait_history 'any(e["kind"]=="prompt-queued" for e in rows)' "$SCRATCH/out/before-restart.json"
kill "$webpid"; wait "$webpid" 2>/dev/null || true
start_web
wait_history 'any(e["kind"]=="prompt-queued" for e in rows)' "$SCRATCH/out/after-restart.json"
python3 - "$SCRATCH/out/before-restart.json" "$SCRATCH/out/after-restart.json" <<'PY'
import json,sys
before=json.load(open(sys.argv[1]))['entries']; after=json.load(open(sys.argv[2]))['entries']
assert before==after
print('PASS restart preserves exact persisted records and recovery does not duplicate them')
PY
b new boot --detach --cwd "$SCRATCH" >/dev/null
b call new-session "{\"name\":\"host\",\"width\":160,\"height\":40,\"cwd\":\"$SCRATCH\"}" >/dev/null
b new-window -s host deck --no-focus -- "$DECK" tui --socket "$SCRATCH/A/run/tuios/tuios.sock" --data "$SCRATCH/tui.json" --captures "$SCRATCH/tui-captures" >/dev/null
wait_deck() {
 for ((i=0;i<40;i++)); do
  b capture-pane -s host -w deck > "$SCRATCH/out/tui-current.txt"
  if python3 -c 'import sys; sys.exit(0 if sys.argv[2] in open(sys.argv[1]).read() else 1)' "$SCRATCH/out/tui-current.txt" "$1"; then return 0; fi
  sleep .25
 done
 echo "FAIL TUI did not show $1"; return 1
}
wait_deck 'tuios-deck'
b send-text -s host -w deck H
wait_deck 'History ·'
b send-keys -s host -w deck G >/dev/null
b wait-for window-idle -s host -w deck --idle 200 --timeout 10000 >/dev/null
b capture-pane -s host -w deck > "$SCRATCH/out/tui-before.txt"
activity idle turn_end 'TUI-new-response'
wait_deck 'TUI-new'
b capture-pane -s host -w deck > "$SCRATCH/out/tui-after.txt"
python3 - "$SCRATCH/out/tui-before.txt" "$SCRATCH/out/tui-after.txt" <<'PY'
import sys
def detail(path):
 return '\n'.join(line.split('│',1)[1].rstrip() for line in open(path) if '│' in line)
assert detail(sys.argv[1])==detail(sys.argv[2])
print('PASS TUI H opens history and new events preserve the selected entry and detail')
PY
b send-keys -s host -w deck Enter >/dev/null
wait_deck 'deck '
echo 'PASS history Enter navigates back to its source pane'
a call close-window '{"session":"target","window":"marks"}' >/dev/null
wait_history 'any(e["kind"]=="window-closed" for e in rows)' "$SCRATCH/out/closed.json"
python3 - "$SCRATCH/out/closed.json" <<'PY'
import json,sys
rows=json.load(open(sys.argv[1]))['entries']
assert any('retained-history-response' in e['detail'] for e in rows)
assert any('live-history-response' in e['detail'] for e in rows)
print('PASS closing the source pane keeps its durable history and records the close')
PY
printf 'History evidence: %s/out\n' "$SCRATCH"
