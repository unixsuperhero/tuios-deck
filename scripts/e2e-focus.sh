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
a() { (isolate "$SCRATCH/A"; /Users/unixsuperhero/go/bin/tuios "$@"); }
b() { (isolate "$SCRATCH/B"; /Users/unixsuperhero/go/bin/tuios "$@"); }
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
if [ "${FOCUS_WORK_SMOKE:-0}" = 1 ]; then
 keys 'F l j j'; shot work-picker
 check "$SCRATCH/out/work-picker.txt" 'Empty sibling' 'hierarchical focus picker reaches empty task'
 keys Enter; shot work-empty
 check "$SCRATCH/out/work-empty.txt" 'FOCUS · Empty sibling' 'empty task is focused'
 absent "$SCRATCH/out/work-empty.txt" 'parent-pane\|child-pane\|unrelated-pane' 'empty focus excludes other panes'
 keys t; shot work-terminal-form
 check "$SCRATCH/out/work-terminal-form.txt" 'Focus project / Empty sibling' 'terminal form prefills focused task without a selected pane'
 b send-text -s host -w focus target >/dev/null
 keys Enter
 b send-text -s host -w focus focus-work-terminal >/dev/null
 keys ctrl+s
 wait_deck 'opened focus-work-terminal' 15
 keys g; shot work-created
 check "$SCRATCH/out/work-created.txt" 'focus-work-terminal' 'new terminal remains visible in focus'
 a list-windows -s target --json > "$SCRATCH/out/work-windows.json"
 bun -e 'const fs=require("fs"), assert=require("assert/strict"); const d=JSON.parse(fs.readFileSync(process.argv[1])); const a=Object.values(d.assignments).find(a=>a.task_id==="t_sibling"); assert.ok(a,"new terminal assigned to empty focused task"); assert.equal(a.session,"target"); console.log("PASS persisted focused task assignment")' "$SCRATCH/deck.json"
 keys p
 b send-text -s host -w focus "printf '\\033[31mFOCUS_WORK_%s\\033[0m\\n' OK" >/dev/null
 shot work-command-form
 keys ctrl+s
 a wait-for window-output -s target -w focus-work-terminal --pattern FOCUS_WORK_OK --timeout 10000 >/dev/null
 a capture-pane -s target -w focus-work-terminal --ansi > "$SCRATCH/out/work-terminal.ansi"
 wait_deck FOCUS_WORK_OK 15
 shot work-output
 b capture-pane -s host -w focus --ansi > "$SCRATCH/out/work-output.ansi"
 check "$SCRATCH/out/work-output.txt" 'FOCUS · Empty sibling' 'sending a command keeps focus active'
 absent "$SCRATCH/out/work-output.txt" 'unrelated-pane\|parent-pane\|child-pane' 'command output excludes unrelated panes'
 keys n; shot work-agent-form
 check "$SCRATCH/out/work-agent-form.txt" 'new agent' 'existing agent launch form opens in focus'
 check "$SCRATCH/out/work-agent-form.txt" 'Focus project / Empty sibling' 'agent launch form prefills focused task'
 keys 'ctrl+c :'; shot work-palette
 check "$SCRATCH/out/work-palette.txt" '─ command palette' 'palette remains usable during focus'
 keys Escape
 b call resize '{"session":"host","window":"focus","width":120,"height":32}' > "$SCRATCH/out/work-resize.json"
 keys r; shot work-resized
 bun -e 'const fs=require("fs"), assert=require("assert/strict"); for(const [file,w,h] of [["work-output.txt",158,43],["work-resized.txt",120,32]]){const lines=fs.readFileSync(process.argv[1]+"/"+file,"utf8").trimEnd().split("\n"); assert.equal(lines.length,h,file); assert.equal(lines[0],"┌"+"─".repeat(w-2)+"┐",file); assert.equal(lines.at(-1),"└"+"─".repeat(w-2)+"┘",file); for(const l of lines.slice(1,-1)){assert.ok(l.startsWith("│")&&l.endsWith("│"),file); assert.equal([...l].length,w,file)} assert.match(lines.at(-2),/help/,"footer remains inside border");} const ansi=fs.readFileSync(process.argv[1]+"/work-output.ansi","utf8"); assert.match(ansi,/\x1b\[[0-9;]*31[;m]/,"terminal red ANSI survives focus"); console.log("PASS one-cell borders, footer, resize and terminal ANSI")' "$SCRATCH/out"
 cp "$SCRATCH/deck.json" "$SCRATCH/out/work-store.json"
 echo "PASS focused work smoke; artifacts: $SCRATCH/out"
 exit 0
fi
keys 'T j E ctrl+f'; shot easy
check "$SCRATCH/out/easy.txt" 'Parent task \[easy\]' 'saved easy list contains marked task'
absent "$SCRATCH/out/easy.txt" 'Unrelated' 'saved easy list hides unrelated tasks'
check "$SCRATCH/deck.json" '"easy": true' 'easy flag persisted on disk'
keys F; shot parent
check "$SCRATCH/out/parent.txt" 'FOCUS · Parent task' 'focus starts from saved easy list'
check "$SCRATCH/out/parent.txt" 'parent-pane' 'task scope includes its own pane'
check "$SCRATCH/out/parent.txt" 'child-pane' 'task scope includes descendant pane'
absent "$SCRATCH/out/parent.txt" 'unrelated-pane' 'task scope excludes unrelated pane'
keys 'C I :'; shot palette
check "$SCRATCH/out/palette.txt" '─ command palette' 'palette is available during focus'
check "$SCRATCH/out/palette.txt" 'FOCUS · Parent task' 'opening other pages keeps focus active'
keys 'Escape T'; shot subtree
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
