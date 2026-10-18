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
# A stand-in opener, so opening an item is seen in a file and never opens a browser.
mkdir -p "$SCRATCH/bin"
for t in open xdg-open; do printf '#!/bin/sh\necho "$@" >> "%s/opened.txt"\n' "$SCRATCH" > "$SCRATCH/bin/$t"; done
chmod +x "$SCRATCH"/bin/*
(unset TUIOS_ENV TUIOS_SESSION TUIOS_PANE_ID TUIOS_PANE_TOKEN TUIOS_SOCKET; export TUIOS_DECK_DATA="$SCRATCH/deck.json" PATH="$SCRATCH/bin:$PATH"; exec "$DECK" web --listen "127.0.0.1:$PORT" --socket "$ASOCK" --captures "$SCRATCH/captures" > "$SCRATCH/out/web.log" 2>&1) &
webpid=$!
for i in $(seq 1 50); do curl -sf "$base/api/state" > "$SCRATCH/out/state.json" 2>/dev/null && break; sleep 0.2; done
check "state lists the session" "$SCRATCH/out/state.json" '"name":"target"'
check "state lists the fake agent as idle" "$SCRATCH/out/state.json" '"name":"fake","state":"idle"'
check "presence held, transcripts on" "$SCRATCH/out/state.json" '"transcripts":true'
curl -s "$base/" > "$SCRATCH/out/index.html"
check "page served" "$SCRATCH/out/index.html" '<title>tuios deck</title>'
check "pane rows carry an agent kind tag" "$SCRATCH/out/index.html" "'kind-agent'"
check "pane rows carry a terminal kind tag" "$SCRATCH/out/index.html" "'kind-terminal'"

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

echo "== attachments: the file picker and the @ and / completions"
mkdir -p "$SCRATCH/.claude/commands" "$SCRATCH/.claude/skills/web-skill"
printf 'run\n' > "$SCRATCH/.claude/commands/web-cmd.md"
printf -- '---\nname: web-skill\n---\n' > "$SCRATCH/.claude/skills/web-skill/SKILL.md"
printf 'x\n' > "$SCRATCH/notes.txt"
curl -s "$base/api/files?path=$SCRATCH" > "$SCRATCH/out/files.json"
check "file picker lists folders and files" "$SCRATCH/out/files.json" '"files":\[.*"notes.txt"'
curl -s "$base/api/complete?kind=at&cwd=$SCRATCH&word=capt" > "$SCRATCH/out/at.json"
check "@ completes a folder with a trailing slash" "$SCRATCH/out/at.json" '"captures/"'
curl -s "$base/api/complete?kind=slash&harness=claude-code&cwd=$SCRATCH&word=web" > "$SCRATCH/out/slash.json"
check "/ lists the project command" "$SCRATCH/out/slash.json" '"web-cmd"'
check "/ lists the project skill" "$SCRATCH/out/slash.json" '"web-skill"'
printf 'x\n' > "$SCRATCH/web spaced.txt"
curl -s "$base/api/complete?kind=at&cwd=$SCRATCH&word=web%20sp" > "$SCRATCH/out/at-space.json"
check "@ offers a name with a space, whole" "$SCRATCH/out/at-space.json" '"web spaced.txt"'
mkdir -p "$SCRATCH/.omp/commands" "$SCRATCH/.omp/skills/web-omp" "$SCRATCH/.omp/skills/web-omp-nodesc"
printf 'run\n' > "$SCRATCH/.omp/commands/web-omp-cmd.md"
printf -- '---\nname: web-omp\ndescription: d\n---\n' > "$SCRATCH/.omp/skills/web-omp/SKILL.md"
printf -- '---\nname: web-omp-nodesc\n---\n' > "$SCRATCH/.omp/skills/web-omp-nodesc/SKILL.md"
curl -s "$base/api/complete?kind=slash&harness=omp&cwd=$SCRATCH&word=web-omp" > "$SCRATCH/out/slash-omp.json"
check "OMP lists its command" "$SCRATCH/out/slash-omp.json" '"web-omp-cmd"'
check "OMP lists its skill as skill:name" "$SCRATCH/out/slash-omp.json" '"skill:web-omp"'
if grep -q 'nodesc' "$SCRATCH/out/slash-omp.json"; then echo "FAIL OMP listed a skill with no description"; fails=$((fails+1)); else echo "PASS OMP skips a skill with no description"; fi
curl -s "$base/api/complete?kind=slash&harness=codex&cwd=$SCRATCH&word=web" > "$SCRATCH/out/slash-codex.json"
check "/ lists nothing for a harness with no known convention" "$SCRATCH/out/slash-codex.json" '"items":\[\]'

echo "== context items: extract from the build pane"
field() { python3 -I -c 'import json,sys; d=json.load(open(sys.argv[1]))
for k in sys.argv[2:]: d=d[int(k)] if isinstance(d,list) else d[k]
print(d)' "$@"; }
: > "$SCRATCH/ctx-file.txt"
a send-text -s target -w build "echo see https://example.com/ctx-link and https://github.com/acme/widgets/pull/77 in $SCRATCH/ctx-file.txt
"
a wait-for window-output -s target -w build --pattern 'see https://example.com/ctx-link' --timeout 10000 >/dev/null
curl -s -X POST "$base/api/panes/target/$BUILD/context/extract" > "$SCRATCH/out/ctx-extract.json"
check "extract reports new items" "$SCRATCH/out/ctx-extract.json" '"status":"[1-9][0-9]* new context item'
curl -s -X POST "$base/api/panes/target/$BUILD/context/extract" > "$SCRATCH/out/ctx-extract2.json"
check "a second extract finds nothing new" "$SCRATCH/out/ctx-extract2.json" '"new":0'
curl -s "$base/api/context" > "$SCRATCH/out/ctx-list.json"
check "link extracted" "$SCRATCH/out/ctx-list.json" '"kind":"link","text":"https://example.com/ctx-link"'
check "pr extracted" "$SCRATCH/out/ctx-list.json" '"kind":"pr","text":"https://github.com/acme/widgets/pull/77"'
check "existing file extracted" "$SCRATCH/out/ctx-list.json" "\"kind\":\"file\",\"text\":\"[^\"]*ctx-file.txt\""
check "item names its pane" "$SCRATCH/out/ctx-list.json" '"pane":"build"'
a send-text -s target -w fake 'https://example.com/fake-only-link
'
a wait-for window-output -s target -w fake --pattern 'fake-only-link' --timeout 10000 >/dev/null
curl -s -X POST "$base/api/panes/target/$FAKE/context/extract" > "$SCRATCH/out/ctx-extract-fake.json"
curl -s "$base/api/context?session=target&window=$BUILD" > "$SCRATCH/out/ctx-scope-build.json"
check "window scope keeps the build pane's items" "$SCRATCH/out/ctx-scope-build.json" 'ctx-link'
if grep -q 'fake-only-link' "$SCRATCH/out/ctx-scope-build.json"; then echo "FAIL window scope kept the fake pane's item"; fails=$((fails+1)); else echo "PASS window scope drops the other pane"; fi
curl -s "$base/api/context?session=target&window=$FAKE" > "$SCRATCH/out/ctx-scope-fake.json"
check "window scope keeps the fake pane's item" "$SCRATCH/out/ctx-scope-fake.json" 'fake-only-link'
if grep -q 'ctx-link' "$SCRATCH/out/ctx-scope-fake.json"; then echo "FAIL fake scope kept the build item"; fails=$((fails+1)); else echo "PASS fake scope drops the build pane"; fi
curl -s "$base/api/context" > "$SCRATCH/out/ctx-scope-all.json"
check "no window means all panes (build)" "$SCRATCH/out/ctx-scope-all.json" 'ctx-link'
check "no window means all panes (fake)" "$SCRATCH/out/ctx-scope-all.json" 'fake-only-link'
curl -s "$base/api/context?kind=link" > "$SCRATCH/out/ctx-links.json"
check "filter by kind keeps the link" "$SCRATCH/out/ctx-links.json" 'ctx-link'
if grep -q '"kind":"pr"' "$SCRATCH/out/ctx-links.json"; then echo "FAIL kind filter kept a pr"; fails=$((fails+1)); else echo "PASS kind filter drops the pr"; fi
LINK=$(python3 -I -c 'import json,sys; print([i["id"] for i in json.load(open(sys.argv[1]))["items"] if i["kind"]=="link" and "ctx-link" in i["text"]][0])' "$SCRATCH/out/ctx-list.json")
curl -s -X PATCH -H "$J" -d '{"starred":true}' "$base/api/context/$LINK" > "$SCRATCH/out/ctx-star.json"
check "PATCH stars the link" "$SCRATCH/out/ctx-star.json" '"starred":true'
curl -s "$base/api/context?starred=1" > "$SCRATCH/out/ctx-starred.json"
check "starred filter lists it" "$SCRATCH/out/ctx-starred.json" "\"id\":\"$LINK\""
if grep -q '"kind":"pr"' "$SCRATCH/out/ctx-starred.json"; then echo "FAIL starred filter kept an unstarred item"; fails=$((fails+1)); else echo "PASS starred filter drops the rest"; fi
a send-keys -s target -w build ctrl+u >/dev/null
(a wait-for window-output -s target -w build --pattern 'ctx-link' --timeout 10000 >/dev/null; echo $? > "$SCRATCH/out/ctx-insert-wait.txt") &
waitpid=$!
curl -s -X POST -H "$J" -d "{\"session\":\"target\",\"window\":\"$BUILD\"}" "$base/api/context/$LINK/insert" > "$SCRATCH/out/ctx-insert.json"
wait "$waitpid"
check "insert answered" "$SCRATCH/out/ctx-insert.json" '"status":"inserted (not submitted)"'
grep -qx 0 "$SCRATCH/out/ctx-insert-wait.txt" && echo "PASS insert reached the build pane" || { echo "FAIL insert never reached build"; fails=$((fails+1)); }
a send-keys -s target -w build ctrl+u >/dev/null
curl -s -X POST -H "$J" -d '{}' "$base/api/context/$LINK/open" > "$SCRATCH/out/ctx-open.json"
check "open runs the platform opener" "$SCRATCH/out/ctx-open.json" '"status":"opened with \(open\|xdg-open\)"'
for i in $(seq 1 20); do grep -q ctx-link "$SCRATCH/opened.txt" 2>/dev/null && break; sleep 0.1; done
grep -q 'https://example.com/ctx-link' "$SCRATCH/opened.txt" && echo "PASS the opener got the link" || { echo "FAIL the opener got nothing"; fails=$((fails+1)); }
curl -s -X POST -H "$J" -d '{"program":"vim"}' "$base/api/context/$LINK/open" > "$SCRATCH/out/ctx-open-vim.json"
check "open refuses a terminal program" "$SCRATCH/out/ctx-open-vim.json" 'needs a terminal'

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
curl -s -X PATCH -H "$J" -d "{\"task\":\"deckproj / palette\"}" "$base/api/context/$LINK" > "$SCRATCH/out/ctx-assign.json"
check "PATCH assigns the link to a task" "$SCRATCH/out/ctx-assign.json" "\"task_id\":\"$TID\""
check "the task brings its project" "$SCRATCH/out/ctx-assign.json" "\"project_id\":\"$PID\""
curl -s "$base/api/context?task=$TID" > "$SCRATCH/out/ctx-bytask.json"
check "filter by task lists it" "$SCRATCH/out/ctx-bytask.json" "\"id\":\"$LINK\""
curl -s -X PATCH -H "$J" -d "{\"task_id\":\"$TID\",\"project_id\":\"$PID\"}" "$base/api/context/$LINK" > "$SCRATCH/out/ctx-assign-both.json"
check "PATCH resolves task over project like an assignment" "$SCRATCH/out/ctx-assign-both.json" "\"task_id\":\"$TID\""
curl -s -X PATCH -H "$J" -d '{"status":"done"}' "$base/api/tasks/$SID" > "$SCRATCH/out/task-patch.json"
check "PATCH task status" "$SCRATCH/out/task-patch.json" '"status":"done"'
curl -s -X PATCH -H "$J" -d '{"status":"maybe"}' "$base/api/tasks/$SID" > "$SCRATCH/out/task-patch-bad.json"
check "PATCH bad status refused" "$SCRATCH/out/task-patch-bad.json" '"error":"status'
curl -s -X PATCH -H "$J" -d '{"status":"skipped"}' "$base/api/tasks/$TID" > "$SCRATCH/out/task-skip.json"
check "PATCH task status skipped" "$SCRATCH/out/task-skip.json" '"status":"skipped"'
curl -s "$base/api/projects" > "$SCRATCH/out/projects-skip.json"
check "project carries its status, open by default" "$SCRATCH/out/projects-skip.json" '"name":"deckproj".*"status":"open"'
check "done and skipped tasks are not open" "$SCRATCH/out/projects-skip.json" '"open_tasks":0'
curl -s -X PATCH -H "$J" -d '{"status":"open"}' "$base/api/tasks/$TID" > /dev/null
curl -s -X PATCH -H "$J" -d '{"status":"skipped"}' "$base/api/projects/$PID" > "$SCRATCH/out/project-skip.json"
check "PATCH project status skipped" "$SCRATCH/out/project-skip.json" '"status":"skipped"'
check "an open task counts once more" "$SCRATCH/out/project-skip.json" '"open_tasks":1'
curl -s "$base/api/projects" > "$SCRATCH/out/projects-skipped.json"
check "GET project shows it skipped" "$SCRATCH/out/projects-skipped.json" '"status":"skipped"'
curl -s -X PATCH -H "$J" -d '{"status":"later"}' "$base/api/projects/$PID" > "$SCRATCH/out/project-skip-bad.json"
check "PATCH bad project status refused" "$SCRATCH/out/project-skip-bad.json" '"error":"status'
curl -s -X PATCH -H "$J" -d '{"status":"open"}' "$base/api/projects/$PID" > /dev/null
curl -s -X POST -H "$J" -d "{\"project_id\":\"$PID\",\"title\":\"docs\"}" "$base/api/tasks" > "$SCRATCH/out/task-docs.json"
DID=$(field "$SCRATCH/out/task-docs.json" task id)
curl -s -X PATCH -H "$J" -d "{\"parent_id\":\"$DID\"}" "$base/api/tasks/$SID" > "$SCRATCH/out/task-move.json"
check "PATCH parent_id moves the subtask" "$SCRATCH/out/task-move.json" "\"parent_id\":\"$DID\""
check "the moved subtask's path follows" "$SCRATCH/out/task-move.json" '"path":"deckproj / docs / autocomplete"'
curl -s -X PATCH -H "$J" -d '{"parent":"deckproj / docs / autocomplete"}' "$base/api/tasks/$DID" > "$SCRATCH/out/task-cycle.json"
check "PATCH refuses a move under the task's own subtask" "$SCRATCH/out/task-cycle.json" '"error":"a task cannot move under itself'
curl -s -X PATCH -H "$J" -d '{"parent_id":"","project":"deckproj"}' "$base/api/tasks/$SID" > "$SCRATCH/out/task-top.json"
check "PATCH project moves a subtask to the top level" "$SCRATCH/out/task-top.json" '"parent_id":"","path":"deckproj / autocomplete"'
check "a top-level task carries its own project_id" "$SCRATCH/out/task-top.json" "\"own_project_id\":\"$PID\""
curl -s -X PATCH -H "$J" -d '{"parent":"palette"}' "$base/api/tasks/$SID" > "$SCRATCH/out/task-back.json"
check "PATCH parent by path moves it back under palette" "$SCRATCH/out/task-back.json" '"path":"deckproj / palette / autocomplete"'
curl -s -X DELETE "$base/api/tasks/$DID" > /dev/null
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

echo "== automatic captures"
a set-agent-state working -s target -w fake >/dev/null
a wait-for agent-state -s target -w fake --until working --timeout 10000 >/dev/null
a set-agent-state idle -s target -w fake -m "fake agent at rest" >/dev/null
# The deck writes the file itself, so poll the API; there is no daemon condition.
for i in $(seq 1 15); do
	curl -s "$base/api/captures" > "$SCRATCH/out/captures-auto.json"
	grep -q '"name":"target-fake-[0-9-]*-turn.txt"' "$SCRATCH/out/captures-auto.json" && break
	sleep 1
done
check "a turn end listed a target-fake turn capture" "$SCRATCH/out/captures-auto.json" '"name":"target-fake-[0-9-]*-turn.txt"'
check "the turn capture's kind is turn" "$SCRATCH/out/captures-auto.json" '"session":"target","pane":"fake","kind":"turn"'
check "web log names the auto capture" "$SCRATCH/out/web.log" 'auto-captured turn to .*target-fake-'
[ "$(grep -c 'auto-captured turn to .*target-fake-' "$SCRATCH/out/web.log")" = 1 ] \
	&& echo "PASS one log line for the one turn capture" || { echo "FAIL turn capture logged more than once"; fails=$((fails+1)); }

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
curl -s "$base/api/context?kind=link" > "$SCRATCH/out/ctx-after-project.json"
check "context item outlives its project" "$SCRATCH/out/ctx-after-project.json" "\"id\":\"$LINK\""
check "and its references are cleared" "$SCRATCH/out/ctx-after-project.json" '"project_id":"","task_id":""'
curl -s -X DELETE "$base/api/context/$LINK" > "$SCRATCH/out/ctx-delete.json"
check "DELETE context item" "$SCRATCH/out/ctx-delete.json" '"status":"deleted '
curl -s "$base/api/context" > "$SCRATCH/out/ctx-after-delete.json"
if grep -q "$LINK" "$SCRATCH/out/ctx-after-delete.json"; then echo "FAIL context item still listed"; fails=$((fails+1)); else echo "PASS context item is gone"; fi
grep -q '"version": 1' "$SCRATCH/deck.json" && echo "PASS store written under the scratch folder" || { echo "FAIL store file"; fails=$((fails+1)); }

echo "== notes: ancestors and descendants see each other, siblings do not"
post() { curl -s -X POST -H "$J" -d "$2" "$base$1"; }
NP=$(post /api/projects '{"name":"notesproj"}' | python3 -I -c 'import json,sys; print(json.load(sys.stdin)["project"]["id"])')
tid() { python3 -I -c 'import json,sys; print(json.load(sys.stdin)["task"]["id"])'; }
NA=$(post /api/tasks "{\"project_id\":\"$NP\",\"title\":\"na\"}" | tid)
NA1=$(post /api/tasks "{\"parent_id\":\"$NA\",\"title\":\"na1\"}" | tid)
NA2=$(post /api/tasks "{\"parent_id\":\"$NA\",\"title\":\"na2\"}" | tid)
NB=$(post /api/tasks "{\"project_id\":\"$NP\",\"title\":\"nb\"}" | tid)
post /api/notes "{\"project_id\":\"$NP\",\"text\":\"note-on-project\"}" > "$SCRATCH/out/note-p.json"
check "POST note on a project" "$SCRATCH/out/note-p.json" '"status":"added note n_'
N1=$(post /api/notes "{\"task_id\":\"$NA1\",\"text\":\"note-on-na1\"}" | field /dev/stdin note id 2>/dev/null || true)
post /api/notes "{\"task_id\":\"$NA\",\"text\":\"note-on-na\"}" > /dev/null
post /api/notes "{\"task_id\":\"$NA2\",\"text\":\"note-on-na2\"}" > /dev/null
post /api/notes "{\"task_id\":\"$NB\",\"text\":\"note-on-nb\"}" > /dev/null
post /api/notes "{\"task_id\":\"$NA\",\"project_id\":\"$NP\",\"text\":\"x\"}" > "$SCRATCH/out/note-both.json"
check "a note on a task and a project is refused" "$SCRATCH/out/note-both.json" '"error"'
curl -s "$base/api/notes?task=$NA1" > "$SCRATCH/out/notes-na1.json"
check "child sees its own note" "$SCRATCH/out/notes-na1.json" 'note-on-na1'
check "child sees its parent's note" "$SCRATCH/out/notes-na1.json" 'note-on-na"'
check "child sees the project's note" "$SCRATCH/out/notes-na1.json" 'note-on-project'
if grep -q 'note-on-na2\|note-on-nb' "$SCRATCH/out/notes-na1.json"; then echo "FAIL sibling note leaked into child"; fails=$((fails+1)); else echo "PASS siblings and cousins are not inherited"; fi
curl -s "$base/api/notes?task=$NA" > "$SCRATCH/out/notes-na.json"
check "parent sees a child's note" "$SCRATCH/out/notes-na.json" 'note-on-na1'
check "parent sees the other child's note" "$SCRATCH/out/notes-na.json" 'note-on-na2'
if grep -q 'note-on-nb' "$SCRATCH/out/notes-na.json"; then echo "FAIL other branch leaked into parent"; fails=$((fails+1)); else echo "PASS another top-level task's note is not seen"; fi
[ "$(grep -o 'note-on-na1' "$SCRATCH/out/notes-na.json" | wc -l | tr -d ' ')" = 1 ] && echo "PASS a note is listed once, not duplicated" || { echo "FAIL duplicate note"; fails=$((fails+1)); }
curl -s "$base/api/notes?project=$NP" > "$SCRATCH/out/notes-proj.json"
check "project sees a task's note" "$SCRATCH/out/notes-proj.json" 'note-on-nb'
N2=$(field "$SCRATCH/out/notes-na1.json" notes 0 id)
[ "$(field "$SCRATCH/out/notes-na1.json" notes 0 text)" = note-on-na1 ] && echo "PASS own notes come first" || { echo "FAIL own notes come first"; fails=$((fails+1)); }
curl -s -X PATCH -H "$J" -d '{"text":"edited-na1"}' "$base/api/notes/$N2" > "$SCRATCH/out/note-edit.json"
check "PATCH note" "$SCRATCH/out/note-edit.json" '"status":"edited note '
curl -s "$base/api/notes?task=$NA" > "$SCRATCH/out/notes-na-edited.json"
check "edit shows on the parent" "$SCRATCH/out/notes-na-edited.json" 'edited-na1'
grep -q 'edited-na1' "$SCRATCH/deck.json" && echo "PASS notes persisted in deck.json" || { echo "FAIL notes not persisted"; fails=$((fails+1)); }
curl -s -X PATCH -H "$J" -d "{\"parent_id\":\"\",\"project_id\":\"$NP\"}" "$base/api/tasks/$NA2" > /dev/null
curl -s "$base/api/notes?task=$NA" > "$SCRATCH/out/notes-na-moved.json"
if grep -q 'note-on-na2' "$SCRATCH/out/notes-na-moved.json"; then echo "FAIL moved-away child's note still seen by old parent"; fails=$((fails+1)); else echo "PASS moving a task out takes its notes out of the old parent"; fi
curl -s "$base/api/notes?task=$NA2" > "$SCRATCH/out/notes-na2-moved.json"
check "moved task keeps its own note" "$SCRATCH/out/notes-na2-moved.json" 'note-on-na2'
if grep -q 'note-on-na"\|edited-na1' "$SCRATCH/out/notes-na2-moved.json"; then echo "FAIL moved task still inherits old parent"; fails=$((fails+1)); else echo "PASS moved task no longer inherits the old parent's notes"; fi
curl -s -X DELETE "$base/api/notes/$N2" > "$SCRATCH/out/note-delete.json"
check "DELETE note" "$SCRATCH/out/note-delete.json" '"status":"deleted note '
curl -s "$base/api/notes?task=$NA" > "$SCRATCH/out/notes-na-deleted.json"
if grep -q 'edited-na1' "$SCRATCH/out/notes-na-deleted.json"; then echo "FAIL deleted note still seen"; fails=$((fails+1)); else echo "PASS deleted note gone from the parent too"; fi
curl -s -X DELETE "$base/api/tasks/$NA" > /dev/null
curl -s "$base/api/notes?project=$NP" > "$SCRATCH/out/notes-after-task.json"
if grep -q 'note-on-na"\|note-on-na1' "$SCRATCH/out/notes-after-task.json"; then echo "FAIL deleted task kept notes"; fails=$((fails+1)); else echo "PASS deleting a task deletes its notes"; fi
check "other notes survive" "$SCRATCH/out/notes-after-task.json" 'note-on-nb'
curl -s -X DELETE "$base/api/projects/$NP" > /dev/null
if grep -q 'note-on-' "$SCRATCH/deck.json"; then echo "FAIL project delete left notes"; fails=$((fails+1)); else echo "PASS deleting a project deletes all its notes"; fi

echo "== new items take random colours from the unassigned pool"
# Creates a project and tasks past the point where every colour is held.
# The first N items, N the colours free beforehand, must all differ; a
# done task frees its colour, which the next item must then take.
python3 -I - "$base" <<'PY'
import json, sys, urllib.request
base = sys.argv[1]
def call(method, path, body=None):
    req = urllib.request.Request(base + path, method=method, data=json.dumps(body).encode() if body is not None else None, headers={"Content-Type": "application/json"})
    return json.load(urllib.request.urlopen(req))
fails = 0
def check(name, ok):
    global fails
    print(("PASS " if ok else "FAIL ") + name)
    fails += 0 if ok else 1
palette = [c["name"] for c in call("GET", "/api/palette")["colors"]]
usable = [c for c in palette if c not in ("black", "bright-black")]
held = {p["theme"]["color"] for p in call("GET", "/api/projects")["projects"] if p["status"] == "open"}
held |= {t["theme"]["color"] for t in call("GET", "/api/tasks")["tasks"] if t["status"] == "open"}
n = len([c for c in usable if c not in held])
p = call("POST", "/api/projects", {"name": "poolproj"})["project"]
colours, tasks = [p["theme"]["color"]], []
for i in range(len(usable) + 2):
    t = call("POST", "/api/tasks", {"project_id": p["id"], "title": "pool %d" % i})["task"]
    tasks.append(t)
    colours.append(t["theme"]["color"])
check("every new colour is a palette name, never a black", all(c in usable for c in colours))
check("the first %d items got %d distinct colours" % (n, n), len(set(colours[:n])) == n)
check("past the pool items still get a colour", len(colours) > len(usable) and all(colours))
check("new items keep their glyphs", p["theme"]["glyph"] == "◆" and all(t["theme"]["glyph"] == "●" for t in tasks))
freed = tasks[0]["theme"]["color"]
same = [t for t in tasks if t["theme"]["color"] == freed]
for t in same:
    call("PATCH", "/api/tasks/" + t["id"], {"status": "done"})
if freed == p["theme"]["color"]:
    call("PATCH", "/api/projects/" + p["id"], {"status": "skipped"})
t = call("POST", "/api/tasks", {"project_id": p["id"], "title": "after done"})["task"]
check("a done task's colour is free again and the only free one is taken", t["theme"]["color"] == freed)
call("DELETE", "/api/projects/" + p["id"])
sys.exit(fails)
PY
fails=$((fails+$?))

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
for id in page-deck page-tasks page-captures page-context btn-extract ctx-copy ctx-insert ctx-open dlg-palette; do
	check "page has #$id" "$SCRATCH/out/index.html" "id=\"$id\""
done
check "page has the group select" "$SCRATCH/out/index.html" 'id="group"'
check "page has the free-text grouping field" "$SCRATCH/out/index.html" 'id="group-custom"'
check "page lists the nested presets" "$SCRATCH/out/index.html" "'harness > project > parent task'"
check "page lists task tree right after session" "$SCRATCH/out/index.html" "GROUPS = \['session', 'task tree',"
check "page builds the task tree from the tasks' parents" "$SCRATCH/out/index.html" "if (levels\[0\] === 'task tree') return treeRows();"
check "pane rows lead with the project chip" "$SCRATCH/out/index.html" "chip.className = 'chip'"
check "unassigned panes are marked" "$SCRATCH/out/index.html" "chip.classList.add('unassigned')"
check "web palette moves on ctrl+n and ctrl+p" "$SCRATCH/out/index.html" "(ctrl && e.key === 'p')"

echo "== prompt presets and queued prompts"
cat > "$SCRATCH/bin/fakeagent" <<FAKE
#!/bin/sh
echo "CWD: \$(pwd)" >> "$SCRATCH/agent.log"
st() { tuios set-agent-state "\$1" -s "\$TUIOS_SESSION" -w "\$TUIOS_PANE_ID" --harness claude-code -m "fake \$1"; }
st idle
while IFS= read -r l; do echo "GOT: \$l" >> "$SCRATCH/agent.log"; st working; sleep 1; st idle; done
FAKE
chmod +x "$SCRATCH/bin/fakeagent"
curl -s "$base/api/prompt-presets" > "$SCRATCH/out/presets0.json"
check "no preset exists until one is made" "$SCRATCH/out/presets0.json" '"presets":\[\]'
curl -s -X POST -H "$J" -d "{\"name\":\"brief\",\"prefix\":\"BRIEF:\",\"cwd\":\"$SCRATCH/out\"}" "$base/api/prompt-presets" > "$SCRATCH/out/preset.json"
check "a preset is created" "$SCRATCH/out/preset.json" '"status":"added preset brief"'
curl -s -X POST -H "$J" -d '{"name":"BRIEF","prefix":"x"}' "$base/api/prompt-presets" > "$SCRATCH/out/preset-dup.json"
check "a preset name is unique, ignoring case" "$SCRATCH/out/preset-dup.json" 'exists'
curl -s -X POST -H "$J" -d '{"name":"qproj"}' "$base/api/projects" > "$SCRATCH/out/qproj.json"
QP=$(field "$SCRATCH/out/qproj.json" project id)
curl -s -X POST -H "$J" -d "{\"project_id\":\"$QP\",\"title\":\"qtask\"}" "$base/api/tasks" > "$SCRATCH/out/qtask.json"
curl -s -X POST -H "$J" -d "{\"task\":\"qproj / qtask\",\"text\":\"ask\",\"preset\":\"brief\",\"agent\":\"$SCRATCH/bin/fakeagent\",\"session\":\"target\"}" "$base/api/queued-prompts" > "$SCRATCH/out/queued-good.json"
check "the prefix is in the queued text, once" "$SCRATCH/out/queued-good.json" '"text":"BRIEF: ask"'
check "the preset's cwd is the queued cwd" "$SCRATCH/out/queued-good.json" "\"cwd\":\"$SCRATCH/out\""
GOOD=$(field "$SCRATCH/out/queued-good.json" queued id)
curl -s -X POST -H "$J" -d '{"project":"qproj","text":"bad","agent":"/nonexistent/prog","session":"target"}' "$base/api/queued-prompts" > "$SCRATCH/out/queued-bad.json"
BAD=$(field "$SCRATCH/out/queued-bad.json" queued id)
curl -s -X POST "$base/api/queued-prompts/$BAD/start" > "$SCRATCH/out/queued-bad-start.json"
check "a failed start says it is kept" "$SCRATCH/out/queued-bad-start.json" 'kept in the queue'
curl -s "$base/api/queued-prompts" > "$SCRATCH/out/queued-after-bad.json"
check "the failed prompt is still queued" "$SCRATCH/out/queued-after-bad.json" "\"id\":\"$BAD\""
curl -s -X POST "$base/api/queued-prompts/$GOOD/start" > "$SCRATCH/out/queued-good-start.json"
check "the good prompt starts and is sent" "$SCRATCH/out/queued-good-start.json" 'the prompt was sent'
curl -s "$base/api/queued-prompts" > "$SCRATCH/out/queued-after-good.json"
if grep -q "\"id\":\"$GOOD\"" "$SCRATCH/out/queued-after-good.json"; then echo "FAIL the sent prompt is still queued"; fails=$((fails+1)); else echo "PASS the sent prompt left the queue"; fi
check "the agent started in the preset's cwd" "$SCRATCH/agent.log" "^CWD: .*/out$"
echo "prefixed prompt typed: $(grep -c '^GOT: BRIEF: ask$' "$SCRATCH/agent.log")" > "$SCRATCH/out/typed-count.txt"
check "the prompt was typed once, prefix once" "$SCRATCH/out/typed-count.txt" 'typed: 1$'
curl -s "$base/api/assignments" > "$SCRATCH/out/queued-assign.json"
check "the started pane is assigned to the task" "$SCRATCH/out/queued-assign.json" '"task":"qtask"'
curl -s -X DELETE "$base/api/queued-prompts/$BAD" > "$SCRATCH/out/queued-del.json"
check "a queued prompt is deleted" "$SCRATCH/out/queued-del.json" 'deleted'
curl -s -X POST "$base/api/prompt-presets/example" > "$SCRATCH/out/preset-example.json"
check "the example preset is added only on request" "$SCRATCH/out/preset-example.json" '"name":"short answer"'
curl -s -X DELETE "$base/api/projects/$QP" > /dev/null

echo "== websocket pane stream"
EXTRA=$(a list-windows -s target --json | python3 -c 'import json,sys; print([w["window_id"] for w in json.load(sys.stdin)["windows"] if w["title"]=="extra"][0])')
(cd "$here" && GOWORK=off go run ./scripts/wscheck "ws://127.0.0.1:$PORT/api/panes/target/$EXTRA/stream?cols=100&rows=30" ws-marker-7) | tee "$SCRATCH/out/ws.txt"
grep -q 'FAIL' "$SCRATCH/out/ws.txt" && fails=$((fails+1))
grep -q 'PASS output' "$SCRATCH/out/ws.txt" || fails=$((fails+1))

echo; echo "responses in $SCRATCH/out; $fails failure(s)"
exit $fails
