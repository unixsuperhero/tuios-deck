#!/usr/bin/env bash
# Real task scopes, saved easy flags, and idle unlock against isolated daemons.
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
DECK=${DECK:-$here/.dev/deck}
SCRATCH=${SCRATCH:-/tmp/deck-focus-$$}
PORT=${PORT:-18748}
mkdir -p "$SCRATCH/out" "$SCRATCH/captures"
isolate() {
 local dir=$1 name
 for name in ${!TUIOS_@}; do unset "$name"; done
 mkdir -p "$dir"/{run,config/tuios,state,cache,data,tmp}; chmod 700 "$dir/run"
 export XDG_RUNTIME_DIR="$dir/run" XDG_CONFIG_HOME="$dir/config" XDG_CONFIG_DIRS="$dir/config" XDG_STATE_HOME="$dir/state" XDG_CACHE_HOME="$dir/cache" XDG_DATA_HOME="$dir/data" TMPDIR="$dir/tmp"
}
a() { (isolate "$SCRATCH/A"; tuios "$@"); }
b() { (isolate "$SCRATCH/B"; tuios "$@"); }
webpid=
cleanup() { [ -z "$webpid" ] || kill "$webpid" 2>/dev/null || :; a kill-server >/dev/null 2>&1 || :; b kill-server >/dev/null 2>&1 || :; }
trap cleanup EXIT
fail() { echo "FAIL $*; artifacts: $SCRATCH/out"; exit 1; }
check() { grep -q -- "$2" "$1" || fail "$3"; echo "PASS $3"; }
absent() { if grep -q -- "$2" "$1"; then fail "$3"; fi; echo "PASS $3"; }
shot() { b capture-pane -s host -w focus > "$SCRATCH/out/$1.txt"; }
keys() { b send-keys -s host -w focus "$1" >/dev/null; b wait-for window-idle -s host -w focus --idle 700 --timeout 10000 >/dev/null; }
# capture-pane reads the alternate screen; window-output does not.
wait_deck() {
 local i
 for ((i=0; i<$2; i++)); do
  shot waiting
  if grep -q -- "$1" "$SCRATCH/out/waiting.txt"; then return 0; fi
  sleep 1
 done
 fail "deck did not show $1"
}
if curl -s --max-time 1 "http://127.0.0.1:$PORT/api/state" >/dev/null; then fail "port $PORT already in use"; fi
a new target --detach --cwd "$SCRATCH" >/dev/null
parent=$(a new-window -s target parent-pane --no-focus --print-id)
child=$(a new-window -s target child-pane --no-focus --print-id)
other=$(a new-window -s target unrelated-pane --no-focus --print-id)
cat > "$SCRATCH/deck.json" <<JSON
{"version":1,"projects":[{"id":"p_focus","name":"Focus project","status":"open","theme":{"color":"blue","glyph":"D"}},{"id":"p_other","name":"Unrelated project","status":"open","theme":{"color":"red","glyph":"D"}}],"tasks":[{"id":"t_parent","project_id":"p_focus","parent_id":"","title":"Parent task","status":"open","theme":{"color":"green","glyph":"O"}},{"id":"t_child","project_id":"","parent_id":"t_parent","title":"Child task","status":"open","theme":{"color":"cyan","glyph":"O"}},{"id":"t_sibling","project_id":"p_focus","parent_id":"","title":"Empty sibling","status":"open","theme":{"color":"yellow","glyph":"O"}},{"id":"t_other","project_id":"p_other","parent_id":"","title":"Unrelated task","status":"open","theme":{"color":"red","glyph":"O"}}],"assignments":{"$parent":{"session":"target","task_id":"t_parent"},"$child":{"session":"target","task_id":"t_child"},"$other":{"session":"target","task_id":"t_other"}}}
JSON
cp "$SCRATCH/deck.json" "$SCRATCH/web.json"
b new boot --detach --cwd "$SCRATCH" >/dev/null
b call new-session "{\"name\":\"host\",\"width\":160,\"height\":45,\"cwd\":\"$SCRATCH\"}" >/dev/null
b new-window -s host focus --no-focus -- env -u NO_COLOR TERM=xterm-256color "$DECK" tui -socket "$SCRATCH/A/run/tuios/tuios.sock" -data "$SCRATCH/deck.json" -captures "$SCRATCH/captures" >/dev/null
wait_deck tuios-deck 15
keys 'T j E ctrl+f'; shot easy
check "$SCRATCH/out/easy.txt" 'Parent task \[easy\]' 'saved easy list contains marked task'
absent "$SCRATCH/out/easy.txt" 'Unrelated' 'saved easy list hides unrelated tasks'
check "$SCRATCH/deck.json" '"easy": true' 'easy flag persisted on disk'
keys F; shot parent
check "$SCRATCH/out/parent.txt" 'FOCUS · Parent task' 'focus starts from saved easy list'
check "$SCRATCH/out/parent.txt" 'parent-pane' 'task scope includes its own pane'
check "$SCRATCH/out/parent.txt" 'child-pane' 'task scope includes descendant pane'
absent "$SCRATCH/out/parent.txt" 'unrelated-pane' 'task scope excludes unrelated pane'
keys 'C I :'; shot guarded
check "$SCRATCH/out/guarded.txt" 'FOCUS · Parent task' 'global pages and palette cannot escape focus'
keys T; shot subtree
absent "$SCRATCH/out/subtree.txt" 'Empty sibling\|Unrelated' 'focused task tree excludes siblings'
keys 'j F'; shot unlocked
check "$SCRATCH/out/unlocked.txt" 'Unrelated project' 'manual unlock restores all tasks'
keys F; shot leaf
check "$SCRATCH/out/leaf.txt" 'FOCUS · Child task' 'any descendant can be focused'
absent "$SCRATCH/out/leaf.txt" 'parent-pane\|unrelated-pane' 'leaf scope excludes ancestor and unrelated panes'
keys 'F T g F T'; shot project
check "$SCRATCH/out/project.txt" 'Empty sibling' 'project scope includes sibling tasks without panes'
absent "$SCRATCH/out/project.txt" 'Unrelated' 'project scope excludes other projects'
keys 'F g j j j F'; shot empty
check "$SCRATCH/out/empty.txt" 'FOCUS · Empty sibling' 'unassigned task can be focused'
absent "$SCRATCH/out/empty.txt" 'parent-pane\|child-pane\|unrelated-pane' 'empty scope leaks no pane'
keys 'F ctrl+f F'; shot idle-start
check "$SCRATCH/out/idle-start.txt" 'FOCUS · Parent task' 'idle scenario starts locked'
# No input goes to the deck while its normal detail polling keeps running.
wait_deck 'focus unlocked' 190
shot idle-unlocked
check "$SCRATCH/out/idle-unlocked.txt" 'unrelated-pane' 'three-minute idle unlock restores full rail'
(isolate "$SCRATCH/W"; exec "$DECK" web -socket "$SCRATCH/A/run/tuios/tuios.sock" -data "$SCRATCH/web.json" -captures "$SCRATCH/captures" -listen "127.0.0.1:$PORT") > "$SCRATCH/out/web.log" 2>&1 &
webpid=$!
for ((i=0; i<15; i++)); do if curl -fsS "http://127.0.0.1:$PORT/api/state" > "$SCRATCH/out/state.json" 2>/dev/null; then break; fi; sleep 1; done
curl -fsS -X PATCH -H 'Content-Type: application/json' -d '{"easy":true}' "http://127.0.0.1:$PORT/api/tasks/t_child" > "$SCRATCH/out/easy-api.json"
check "$SCRATCH/out/easy-api.json" '"easy":true' 'web API saves an easy flag on a descendant'
check "$SCRATCH/web.json" '"easy": true' 'web easy flag persisted in the existing store'
curl -fsS -X PATCH -H 'Content-Type: application/json' -d '{"easy":false}' "http://127.0.0.1:$PORT/api/tasks/t_child" > "$SCRATCH/out/easy-clear.json"
check "$SCRATCH/out/easy-clear.json" '"easy":false' 'web API clears a saved easy flag'
echo "PASS focus scenarios; artifacts: $SCRATCH/out"
