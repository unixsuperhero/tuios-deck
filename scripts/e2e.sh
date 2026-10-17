#!/usr/bin/env bash
# End-to-end check of the deck against a throwaway tuios daemon.
#
# Daemon A holds the panes the deck manages: a shell, a fake agent that
# reports its own state, and whatever the deck spawns. Daemon B holds one pane
# that runs the deck itself, so the deck is outside every pane of A and may
# hold the person's presence there. Both daemons live under a scratch folder
# and are killed at the end. The deck's store is a file under the scratch
# folder too, seeded by hand with one project and one task. Every check
# writes a capture into $SCRATCH/out and prints PASS or FAIL; the exit
# status is the number of failures.
set -uo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
DECK=${DECK:-$here/.dev/deck}
SCRATCH=${SCRATCH:-${TMPDIR:-/tmp}/tuios-deck-e2e-$(id -u)}
rm -rf "$SCRATCH"; mkdir -p "$SCRATCH/out" "$SCRATCH/captures"
fails=0

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
A="$SCRATCH/A"; B="$SCRATCH/B"
a() { (isolate "$A"; tuios "$@"); }
b() { (isolate "$B"; tuios "$@"); }
cleanup() { a kill-server >/dev/null 2>&1; b kill-server >/dev/null 2>&1; }
trap cleanup EXIT

check() { # name, file, pattern
	if grep -q -- "$3" "$2"; then echo "PASS $1"; else echo "FAIL $1: no '$3' in $2"; fails=$((fails+1)); fi
}
deckcap() { b capture-pane -s host -w deck > "$SCRATCH/out/$1.txt"; }
# Rows of the list, in order: the session's first shell, build, fake, extra, sleeper.
select_pane() { b send-keys -s host -w deck g >/dev/null; [ "$1" -gt 0 ] && b send-keys -s host -w deck j --repeat "$1" >/dev/null; settle; }
# Polls the deck's screen for a pattern, since wait-for window-output does not
# read the alternate screen the deck draws on.
wait_deck() { # pattern, seconds
	local i
	for ((i=0; i<$2; i++)); do
		if b capture-pane -s host -w deck 2>/dev/null | grep -q -- "$1"; then return 0; fi
		sleep 1
	done
	return 1
}
settle() { b wait-for window-idle -s host -w deck --idle 700 --timeout 10000 >/dev/null; }
window_id() { a list-windows -s target --json | python3 -c 'import json,sys; print([w["window_id"] for w in json.load(sys.stdin)["windows"] if w["title"]==sys.argv[1]][0])' "$1"; }
# Polls the store file, which the deck writes itself after a listing, so
# there is no daemon condition to wait on.
wait_store() { # pattern, present|absent, seconds
	local i
	for ((i=0; i<$3; i++)); do
		if grep -q -- "$1" "$DATA"; then [ "$2" = present ] && return 0; else [ "$2" = absent ] && return 0; fi
		sleep 1
	done
	return 1
}

echo "== daemon A: a shell pane and a fake agent"
a new target --detach --cwd "$SCRATCH" >/dev/null
a new-window -s target build --cwd "$SCRATCH" --no-focus >/dev/null
a send-text -s target -w build 'echo hello-from-build
'
a new-window -s target fake --no-focus -- sh -c 'tuios set-agent-state idle -s "$TUIOS_SESSION" -w "$TUIOS_PANE_ID" --harness claude-code -m "fake agent at rest"; exec sleep 600' >/dev/null
a wait-for agent-state -s target -w fake --until idle --timeout 10000 >/dev/null
a list-agents -s target --json > "$SCRATCH/out/a-agents.json"
check "fake agent is listed as idle" "$SCRATCH/out/a-agents.json" '"state": *"idle"'
ASOCK="$A/run/tuios/tuios.sock"

echo "== the store: a project and a task, written by hand in the store's shape"
DATA="$SCRATCH/deck.json"
cat > "$DATA" <<'JSON'
{
  "version": 1,
  "projects": [
    { "id": "p_e2e001", "name": "deck", "theme": { "color": "blue", "glyph": "◆" }, "created": 1760000000 }
  ],
  "tasks": [
    { "id": "t_e2e001", "parent_id": "", "project_id": "p_e2e001", "title": "command palette", "theme": { "color": "magenta", "glyph": "●" }, "status": "open", "created": 1760000000, "updated": 1760000000 }
  ],
  "assignments": {}
}
JSON

echo "== daemon B: the deck in a pane, pointed at A"
# The host session is made wide so the list has room for the task titles.
b new boot --detach --cwd "$SCRATCH" >/dev/null
b call new-session "{\"name\":\"host\",\"width\":160,\"height\":50,\"cwd\":\"$SCRATCH\"}" >/dev/null
b new-window -s host deck --no-focus -- env TUIOS_DECK_DATA="$DATA" "$DECK" tui --socket "$ASOCK" --captures "$SCRATCH/captures" >/dev/null
b wait-for window-output -s host -w deck --pattern 'tuios-deck' --timeout 15000 >/dev/null
select_pane 1; deckcap 01-start
check "title shows the deck" "$SCRATCH/out/01-start.txt" 'tuios-deck'
check "session target listed" "$SCRATCH/out/01-start.txt" 'target'
check "build pane listed" "$SCRATCH/out/01-start.txt" 'build'
check "fake agent listed idle" "$SCRATCH/out/01-start.txt" 'fake.*idle'
check "screen tab shows the shell output" "$SCRATCH/out/01-start.txt" 'hello-from-build'

echo "== tabs"
select_pane 2
b send-keys -s host -w deck 2 >/dev/null; settle; deckcap 02-activity
check "activity tab for the fake agent" "$SCRATCH/out/02-activity.txt" 'state idle'
b send-keys -s host -w deck 3 >/dev/null; settle; deckcap 03-transcript
check "transcript tab reached the daemon (presence held)" "$SCRATCH/out/03-transcript.txt" 'transcript\|Transcript'
if grep -q 'refused' "$SCRATCH/out/03-transcript.txt"; then echo "FAIL presence refused from daemon B"; fails=$((fails+1)); else echo "PASS presence accepted"; fi
b send-keys -s host -w deck 1 >/dev/null; settle

echo "== prompt to a plain terminal"
select_pane 1
b send-keys -s host -w deck p >/dev/null; settle
b send-text -s host -w deck 'echo from-deck-prompt'
b send-keys -s host -w deck Enter >/dev/null
a wait-for window-output -s target -w build --pattern 'from-deck-prompt' --timeout 10000 >/dev/null \
	&& echo "PASS prompt reached the build pane" || { echo "FAIL prompt never reached build"; fails=$((fails+1)); }
settle; deckcap 04-after-prompt
check "status reports the send" "$SCRATCH/out/04-after-prompt.txt" 'sent to build'

echo "== prompt to the fake agent is queued"
select_pane 2
b send-keys -s host -w deck p >/dev/null; settle
b send-text -s host -w deck 'do the thing'
b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 05-queued
check "queue-prompt was used for the agent" "$SCRATCH/out/05-queued.txt" 'prompt \(queued\|delivering\)'

echo "== capture to file"
select_pane 1
b send-keys -s host -w deck c >/dev/null; settle; deckcap 06-capture
check "capture path reported" "$SCRATCH/out/06-capture.txt" 'captured to'
if ls "$SCRATCH"/captures/target-build-*.txt >/dev/null 2>&1 && grep -q 'hello-from-build' "$SCRATCH"/captures/target-build-*.txt; then echo "PASS capture file holds the pane"; else echo "FAIL capture file missing or empty"; fails=$((fails+1)); fi

echo "== a terminal needs a task or a project"
b send-keys -s host -w deck t >/dev/null; settle
b send-keys -s host -w deck Enter >/dev/null            # session
b send-text -s host -w deck 'nope'; b send-keys -s host -w deck Enter >/dev/null
b send-keys -s host -w deck Enter >/dev/null            # cwd
b send-keys -s host -w deck Enter >/dev/null            # no task
b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 06b-required   # no project either
check "form refuses without a task or a project" "$SCRATCH/out/06b-required.txt" 'a task or a project is required'
b send-keys -s host -w deck Escape >/dev/null; settle
a list-windows -s target --json > "$SCRATCH/out/a-windows-nope.json"
if grep -q '"nope"' "$SCRATCH/out/a-windows-nope.json"; then echo "FAIL nope opened without an assignment"; fails=$((fails+1)); else echo "PASS nothing opened"; fi

echo "== new terminal through the form, assigned to a task"
b send-keys -s host -w deck t >/dev/null; settle
b send-keys -s host -w deck Enter >/dev/null            # session: default target
b send-text -s host -w deck 'extra'; b send-keys -s host -w deck Enter >/dev/null
b send-keys -s host -w deck Enter >/dev/null            # cwd default; always a shell
b send-text -s host -w deck 'comm'; b send-keys -s host -w deck Tab >/dev/null; settle; deckcap 06c-complete
check "tab completes the task from its title" "$SCRATCH/out/06c-complete.txt" 'task: deck / command palette'
b send-keys -s host -w deck Enter >/dev/null            # task
b send-keys -s host -w deck Enter >/dev/null            # project: implied by the task
a wait-for window-output -s target -w extra --pattern '.' --timeout 10000 >/dev/null 2>&1 || true
a list-windows -s target --json > "$SCRATCH/out/a-windows.json"
check "extra terminal opened in A" "$SCRATCH/out/a-windows.json" '"extra"'
EXTRA=$(window_id extra)
wait_store "\"$EXTRA\"" present 10 && echo "PASS store holds an assignment for extra" || { echo "FAIL no assignment for $EXTRA in $DATA"; fails=$((fails+1)); }
python3 -c 'import json,sys; a=json.load(open(sys.argv[1]))["assignments"][sys.argv[2]]; sys.exit(0 if a["task_id"]=="t_e2e001" and a["session"]=="target" else 1)' "$DATA" "$EXTRA" \
	&& echo "PASS extra is assigned to the task in session target" || { echo "FAIL extra assignment is not the task"; fails=$((fails+1)); }
select_pane 3; deckcap 06d-assigned
check "list row shows the task title" "$SCRATCH/out/06d-assigned.txt" 'extra *command palette'
check "detail bar shows project › task" "$SCRATCH/out/06d-assigned.txt" 'deck › command palette'

echo "== assign a pane with a"
select_pane 1
b send-keys -s host -w deck a >/dev/null; settle
b send-keys -s host -w deck Enter >/dev/null            # no task
b send-text -s host -w deck 'deck'; b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 06e-assign
check "status reports the assignment" "$SCRATCH/out/06e-assign.txt" 'assigned build to deck'
BUILD=$(window_id build)
python3 -c 'import json,sys; a=json.load(open(sys.argv[1]))["assignments"][sys.argv[2]]; sys.exit(0 if a["project_id"]=="p_e2e001" and a["task_id"]=="" else 1)' "$DATA" "$BUILD" \
	&& echo "PASS build is assigned to the project" || { echo "FAIL build assignment is not the project"; fails=$((fails+1)); }

echo "== new agent through the form (a quiet program counts as ready)"
b send-keys -s host -w deck n >/dev/null; settle
b send-keys -s host -w deck Enter >/dev/null            # session
b send-keys -s host -w deck ctrl+u >/dev/null; b send-text -s host -w deck 'sleep 600'; b send-keys -s host -w deck Enter >/dev/null
b send-text -s host -w deck 'sleeper'; b send-keys -s host -w deck Enter >/dev/null
b send-keys -s host -w deck Enter >/dev/null            # cwd
b send-keys -s host -w deck Enter >/dev/null            # no task
b send-keys -s host -w deck Tab >/dev/null              # project: the selected pane's, deck
b send-keys -s host -w deck Enter >/dev/null
b send-keys -s host -w deck Enter >/dev/null            # no prompt
wait_deck 'sleeper .* is' 120 \
	&& echo "PASS start-agent returned" || { echo "FAIL start-agent did not report"; fails=$((fails+1)); }
settle; deckcap 07-agent
check "sleeper listed" "$SCRATCH/out/07-agent.txt" 'sleeper'
SLEEPER=$(window_id sleeper)
wait_store "\"$SLEEPER\"" present 10 && echo "PASS store holds an assignment for sleeper" || { echo "FAIL no assignment for $SLEEPER in $DATA"; fails=$((fails+1)); }
check "sleeper row shows the project" "$SCRATCH/out/07-agent.txt" 'sleeper *deck'

echo "== close a pane with confirmation"
select_pane 1
b send-keys -s host -w deck x >/dev/null; settle; deckcap 08-confirm
check "confirmation asked" "$SCRATCH/out/08-confirm.txt" 'close build'
b send-keys -s host -w deck y >/dev/null; settle
a list-windows -s target --json > "$SCRATCH/out/a-windows-after.json"
if grep -q '"build"' "$SCRATCH/out/a-windows-after.json"; then echo "FAIL build still open"; fails=$((fails+1)); else echo "PASS build closed"; fi
deckcap 09-final
check "list refreshed without build" "$SCRATCH/out/09-final.txt" 'closed build'
wait_store "\"$BUILD\"" absent 10 && echo "PASS closed build's assignment pruned" || { echo "FAIL $BUILD still assigned in $DATA"; fails=$((fails+1)); }

echo "== quit"
b send-keys -s host -w deck q >/dev/null
b wait-for window-exit -s host -w deck --timeout 10000 >/dev/null && echo "PASS deck exited on q" || { echo "FAIL deck did not exit"; fails=$((fails+1)); }

echo; echo "captures in $SCRATCH/out; $fails failure(s)"
exit $fails
