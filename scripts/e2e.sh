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
b send-keys -s host -w deck r >/dev/null; settle; deckcap 00-r-at-start
check "r right after start leaves the deck alive" "$SCRATCH/out/00-r-at-start.txt" 'tuios-deck'
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
b send-text -s host -w deck 'echo from-deck-prompt'; settle; deckcap 04a-prompt-box
check "prompt form is a box with its title" "$SCRATCH/out/04a-prompt-box.txt" '╭─ prompt ─'
check "prompt field is multiline and submits with ctrl+s" "$SCRATCH/out/04a-prompt-box.txt" 'enter newline · ctrl+s submit'
b send-keys -s host -w deck ctrl+s >/dev/null
a wait-for window-output -s target -w build --pattern 'from-deck-prompt' --timeout 10000 >/dev/null \
	&& echo "PASS prompt reached the build pane" || { echo "FAIL prompt never reached build"; fails=$((fails+1)); }
settle; deckcap 04-after-prompt
check "status reports the send" "$SCRATCH/out/04-after-prompt.txt" 'sent to build'

echo "== @ completes paths under the pane's cwd"
b send-keys -s host -w deck p >/dev/null; settle
b send-text -s host -w deck 'look at @'; settle; deckcap 04b-at
check "@ lists the folders of the pane's cwd" "$SCRATCH/out/04b-at.txt" '  captures/'
check "@ list shows files plain" "$SCRATCH/out/04b-at.txt" '  deck.json '
b send-text -s host -w deck 'capt'; settle; deckcap 04c-at-filter
check "typing after @ filters the list" "$SCRATCH/out/04c-at-filter.txt" '▸ captures/'
if grep -q 'deck.json' "$SCRATCH/out/04c-at-filter.txt"; then echo "FAIL @ filter kept deck.json"; fails=$((fails+1)); else echo "PASS @ filter hides unmatched entries"; fi
b send-keys -s host -w deck Tab >/dev/null; settle; deckcap 04d-at-descend
check "tab on a folder descends into it" "$SCRATCH/out/04d-at-descend.txt" 'look at @captures/▏'
b send-keys -s host -w deck Escape >/dev/null; settle; deckcap 04e-at-closed
check "esc closes the @ list and keeps the form" "$SCRATCH/out/04e-at-closed.txt" '╭─ prompt ─'
b send-keys -s host -w deck ctrl+u >/dev/null; b send-text -s host -w deck '@deck.j'; b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 04f-at-file
check "enter on a file puts its path in place of the @query" "$SCRATCH/out/04f-at-file.txt" '    deck.json▏'
b send-keys -s host -w deck Escape >/dev/null; settle; deckcap 04g-at-cancel
if grep -q '╭─ prompt' "$SCRATCH/out/04g-at-cancel.txt"; then echo "FAIL prompt form still open"; fails=$((fails+1)); else echo "PASS esc with no list cancels the form"; fi

echo "== prompt to the fake agent is queued"
select_pane 2
b send-keys -s host -w deck p >/dev/null; settle
b send-text -s host -w deck 'do the thing'
b send-keys -s host -w deck ctrl+s >/dev/null; settle; deckcap 05-queued
check "queue-prompt was used for the agent" "$SCRATCH/out/05-queued.txt" 'prompt \(queued\|delivering\)'

echo "== automatic captures: an agent turn and a shell command"
# A capture file is written by the deck itself, so there is no daemon
# condition to wait on; poll the folder like wait_deck polls the screen.
wait_file() { # glob, seconds
	local i
	for ((i=0; i<$2; i++)); do
		compgen -G "$1" >/dev/null && return 0
		sleep 1
	done
	return 1
}
a set-agent-state working -s target -w fake >/dev/null
a wait-for agent-state -s target -w fake --until working --timeout 10000 >/dev/null
a set-agent-state idle -s target -w fake -m "fake agent at rest" >/dev/null
wait_file "$SCRATCH/captures/target-fake-*-turn.txt" 15 \
	&& echo "PASS a turn end wrote target-fake-*-turn.txt" || { echo "FAIL no turn capture for fake"; fails=$((fails+1)); }
# command-finished needs the build pane's shell to mark commands (OSC 133).
# When the daemon saw no command finish there, the cmd path is not testable.
if a list-windows -s target --json | python3 -c 'import json,sys; sys.exit(0 if [w for w in json.load(sys.stdin)["windows"] if w["title"]=="build" and w.get("command_seq",0)>0] else 1)'; then
	wait_file "$SCRATCH/captures/target-build-*-cmd.txt" 15 \
		&& echo "PASS a finished command wrote target-build-*-cmd.txt" || { echo "FAIL no cmd capture for build"; fails=$((fails+1)); }
	grep -q 'from-deck-prompt' "$SCRATCH"/captures/target-build-*-cmd.txt 2>/dev/null \
		&& echo "PASS cmd capture holds the command's output" || { echo "FAIL cmd capture lacks from-deck-prompt"; fails=$((fails+1)); }
else
	echo "SKIP build's shell marks no commands, so command-finished is not tested"
fi
b send-text -s host -w deck 'C'; settle; deckcap 05b-auto-captures
check "captures page shows the turn capture's kind" "$SCRATCH/out/05b-auto-captures.txt" ' fake  target turn '
b send-keys -s host -w deck Escape >/dev/null; settle
# The page checks below are about a capture taken by hand.
rm -f "$SCRATCH"/captures/*-turn.txt "$SCRATCH"/captures/*-cmd.txt

echo "== capture to file"
select_pane 1
b send-keys -s host -w deck c >/dev/null; settle; deckcap 06-capture
check "capture path reported" "$SCRATCH/out/06-capture.txt" 'captured to'
if ls "$SCRATCH"/captures/target-build-*.txt >/dev/null 2>&1 && grep -q 'hello-from-build' "$SCRATCH"/captures/target-build-*.txt; then echo "PASS capture file holds the pane"; else echo "FAIL capture file missing or empty"; fails=$((fails+1)); fi

echo "== captures page: review and delete the capture"
b send-text -s host -w deck 'C'; settle; deckcap 06f-captures
check "title names the captures page" "$SCRATCH/out/06f-captures.txt" 'tuios-deck.* captures '
check "capture row names the pane and its session" "$SCRATCH/out/06f-captures.txt" ' build  target '
check "capture content is shown on the right" "$SCRATCH/out/06f-captures.txt" 'hello-from-build'
check "help line is the captures page's" "$SCRATCH/out/06f-captures.txt" 'x delete · r refresh'
b send-keys -s host -w deck x >/dev/null; settle; deckcap 06g-confirm
check "delete asks by file name" "$SCRATCH/out/06g-confirm.txt" 'delete target-build-[0-9-]*\.txt? (Y/n)'
check "help line gives the confirm keys" "$SCRATCH/out/06g-confirm.txt" 'enter/y confirm · n/esc cancel'
b send-text -s host -w deck 'z'; settle; deckcap 06g2-stray
check "a stray key leaves the question open" "$SCRATCH/out/06g2-stray.txt" 'delete target-build-[0-9-]*\.txt? (Y/n)'
b send-text -s host -w deck 'n'; settle; deckcap 06g3-cancelled
check "n cancels the delete" "$SCRATCH/out/06g3-cancelled.txt" 'cancelled'
if ls "$SCRATCH"/captures/target-build-*.txt >/dev/null 2>&1; then echo "PASS capture file kept after n"; else echo "FAIL capture file gone after n"; fails=$((fails+1)); fi
b send-keys -s host -w deck x >/dev/null; settle
b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 06h-deleted
check "enter confirms the delete" "$SCRATCH/out/06h-deleted.txt" 'deleted target-build-'
check "status reports the deletion" "$SCRATCH/out/06h-deleted.txt" 'deleted target-build-'
if ls "$SCRATCH"/captures/target-build-*.txt >/dev/null 2>&1; then echo "FAIL capture file still there"; fails=$((fails+1)); else echo "PASS capture file removed"; fi
check "list is empty after the delete" "$SCRATCH/out/06h-deleted.txt" 'no captures'
b send-keys -s host -w deck Escape >/dev/null; settle; deckcap 06i-back
check "esc returns to the deck from captures" "$SCRATCH/out/06i-back.txt" 'tuios-deck.* deck '

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
check "list row leads with the project's four letters and the task title" "$SCRATCH/out/06d-assigned.txt" '^  . deck command palette  extra'
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
b send-keys -s host -w deck ctrl+s >/dev/null           # no prompt; the prompt field is multiline
wait_deck 'sleeper .* is' 120 \
	&& echo "PASS start-agent returned" || { echo "FAIL start-agent did not report"; fails=$((fails+1)); }
settle; deckcap 07-agent
check "sleeper listed" "$SCRATCH/out/07-agent.txt" 'sleeper'
SLEEPER=$(window_id sleeper)
wait_store "\"$SLEEPER\"" present 10 && echo "PASS store holds an assignment for sleeper" || { echo "FAIL no assignment for $SLEEPER in $DATA"; fails=$((fails+1)); }
check "unassigned fake agent row shows !!!!" "$SCRATCH/out/07-agent.txt" '^  . !!!! fake'
check "sleeper row leads with the project's four letters" "$SCRATCH/out/07-agent.txt" '^  . deck sleeper'

echo "== group the left rail with o and O"
b send-keys -s host -w deck o >/dev/null; settle; deckcap 07b-by-project
check "title says by project" "$SCRATCH/out/07b-by-project.txt" 'tuios-deck.* deck  by project'
check "project header row in its glyph" "$SCRATCH/out/07b-by-project.txt" '^◆ deck '
check "unassigned panes under (none)" "$SCRATCH/out/07b-by-project.txt" '(none)'
check "status confirms the grouping" "$SCRATCH/out/07b-by-project.txt" 'grouped by project'
check "selection stays on build across the regroup" "$SCRATCH/out/07b-by-project.txt" 'target › build  ◆ deck'
b send-keys -s host -w deck o >/dev/null; settle; deckcap 07b2-by-parent
check "title says by project > parent task" "$SCRATCH/out/07b2-by-parent.txt" 'tuios-deck.* deck  by project > parent task'
check "project header at the outer level" "$SCRATCH/out/07b2-by-parent.txt" '^◆ deck '
check "parent-task header indented under the project" "$SCRATCH/out/07b2-by-parent.txt" '^  ● command palette'
check "pane indented under the parent-task header" "$SCRATCH/out/07b2-by-parent.txt" '^    · deck command palette  extra'
b send-keys -s host -w deck o >/dev/null; settle; deckcap 07c-by-task
check "title says by task" "$SCRATCH/out/07c-by-task.txt" 'tuios-deck.* deck  by task'
check "task header row shows the path" "$SCRATCH/out/07c-by-task.txt" '^● deck › command palette'
b send-text -s host -w deck 'OOO'; settle; deckcap 07d-by-session
check "O three times is back to session" "$SCRATCH/out/07d-by-session.txt" 'grouped by session'
check "session header row is back" "$SCRATCH/out/07d-by-session.txt" '▾ target'
if grep -q ' deck  by ' "$SCRATCH/out/07d-by-session.txt"; then echo "FAIL title still names a grouping"; fails=$((fails+1)); else echo "PASS title drops the grouping by session"; fi

echo "== command palette: deck commands"
b send-text -s host -w deck ':'; settle; deckcap 07e-palette
check "palette opens with its header" "$SCRATCH/out/07e-palette.txt" '─ command palette'
check "palette shows its prompt" "$SCRATCH/out/07e-palette.txt" '^ > ▏'
check "help line explains the palette keys" "$SCRATCH/out/07e-palette.txt" 'enter run · tab complete · esc close'
check "deck commands listed first" "$SCRATCH/out/07e-palette.txt" '\[deck\] *assign to task'
b send-text -s host -w deck 'group'; settle; deckcap 07f-filter
check "typing filters to the group-by entries" "$SCRATCH/out/07f-filter.txt" '\[deck\] *group by session'
check "every grouping has an entry" "$SCRATCH/out/07f-filter.txt" 'group by top-level task'
if grep -q 'assign to task' "$SCRATCH/out/07f-filter.txt"; then echo "FAIL unmatched entries still shown"; fails=$((fails+1)); else echo "PASS unmatched entries hidden"; fi
b send-text -s host -w deck ' by pro'; b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 07g-regrouped
check "enter on group by project regroups the rail" "$SCRATCH/out/07g-regrouped.txt" 'tuios-deck.* deck  by project'
check "status confirms the palette's regroup" "$SCRATCH/out/07g-regrouped.txt" 'grouped by project'
if grep -q '─ command palette' "$SCRATCH/out/07g-regrouped.txt"; then echo "FAIL palette still open after enter"; fails=$((fails+1)); else echo "PASS palette closed after enter"; fi
b send-text -s host -w deck ':'; settle
b send-text -s host -w deck 'group by…'; b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 07g2-group-form
check "group by… opens the levels form" "$SCRATCH/out/07g2-group-form.txt" '╭─ group by ─'
b send-text -s host -w deck 'harness > project'; b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 07g3-free-text
check "free-text grouping named in the title" "$SCRATCH/out/07g3-free-text.txt" 'tuios-deck.* deck  by harness > project'
check "harness header at the outer level" "$SCRATCH/out/07g3-free-text.txt" '^▾ claude-code'
check "project header indented under a harness" "$SCRATCH/out/07g3-free-text.txt" '^  ◆ deck '
b send-text -s host -w deck 'oo'; settle; deckcap 07g4-back
check "o from a custom grouping restarts the presets" "$SCRATCH/out/07g4-back.txt" 'tuios-deck.* deck  by project '
b send-keys -s host -w deck ctrl+p >/dev/null; settle; deckcap 07g5-ctrlp
check "ctrl+p opens the palette" "$SCRATCH/out/07g5-ctrlp.txt" '─ command palette'
check "help line names ctrl+p/n" "$SCRATCH/out/07g5-ctrlp.txt" 'ctrl+p/n pick'
b send-keys -s host -w deck ctrl+n >/dev/null; settle
b send-keys -s host -w deck Tab >/dev/null; settle; deckcap 07g6-ctrln
check "ctrl+n moves to the second entry" "$SCRATCH/out/07g6-ctrln.txt" '^ > assign to project'
b send-keys -s host -w deck ctrl+u >/dev/null
b send-keys -s host -w deck ctrl+n >/dev/null
b send-keys -s host -w deck ctrl+p >/dev/null; settle; deckcap 07g7-ctrlp-back
check "palette stays open after ctrl+p" "$SCRATCH/out/07g7-ctrlp-back.txt" '─ command palette'
b send-keys -s host -w deck Tab >/dev/null; settle; deckcap 07g8-first
check "ctrl+p moves back to the first entry" "$SCRATCH/out/07g8-first.txt" '^ > assign to task'
b send-keys -s host -w deck ctrl+u >/dev/null; settle
b send-text -s host -w deck 'nosuchthing'; settle; deckcap 07h-nomatch
check "an unknown query says no matches" "$SCRATCH/out/07h-nomatch.txt" 'no matches'
b send-keys -s host -w deck Escape >/dev/null; settle; deckcap 07i-closed
if grep -q '─ command palette' "$SCRATCH/out/07i-closed.txt"; then echo "FAIL palette still open after esc"; fails=$((fails+1)); else echo "PASS esc closes the palette"; fi

echo "== command palette: a tuios verb from list-verbs"
b send-text -s host -w deck ':'; settle
b send-text -s host -w deck 'list-win'; settle; deckcap 07j-verb
check "verb listed from list-verbs with its description" "$SCRATCH/out/07j-verb.txt" '\[tuios\] *list-windows *— List the windows'
b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 07k-form
check "form box is titled with the verb" "$SCRATCH/out/07k-form.txt" '╭─ list-windows ─'
check "form asks for the verb's session param" "$SCRATCH/out/07k-form.txt" '› session: '
check "param description is the help-line hint" "$SCRATCH/out/07k-form.txt" 'Session name'
b send-text -s host -w deck 'tar'; b send-keys -s host -w deck Tab >/dev/null; settle; deckcap 07k2-complete
check "tab completes the session name" "$SCRATCH/out/07k2-complete.txt" 'session: target'
b send-keys -s host -w deck Enter >/dev/null
wait_deck 'result of list-windows' 15 && echo "PASS verb result reached the detail pane" || { echo "FAIL no result of list-windows"; fails=$((fails+1)); }
deckcap 07l-result
check "result title names the verb and its params" "$SCRATCH/out/07l-result.txt" 'result of list-windows .*{"session":"target"}'
check "result holds the windows array" "$SCRATCH/out/07l-result.txt" '"windows"'
check "result names the build pane" "$SCRATCH/out/07l-result.txt" '"build"'
check "status reports the call" "$SCRATCH/out/07l-result.txt" 'list-windows: ok'
b send-keys -s host -w deck j >/dev/null; settle; deckcap 07m-moved
if grep -q 'result of list-windows' "$SCRATCH/out/07m-moved.txt"; then echo "FAIL result view stayed after moving"; fails=$((fails+1)); else echo "PASS moving the selection ends the result view"; fi
b send-text -s host -w deck 'O'; settle; deckcap 07n-session-again
check "O is back to session for the checks that follow" "$SCRATCH/out/07n-session-again.txt" 'grouped by session'

echo "== tasks page: the seeded project and task"
b send-text -s host -w deck 'T'; settle; deckcap 10-tasks
check "title names the tasks page" "$SCRATCH/out/10-tasks.txt" 'tuios-deck.* tasks '
check "seeded project listed with its open count" "$SCRATCH/out/10-tasks.txt" '◆ deck *1 open'
check "seeded task listed with its pane" "$SCRATCH/out/10-tasks.txt" 'command palette *1 pane'
check "project detail lists the panes assigned under it" "$SCRATCH/out/10-tasks.txt" 'target › extra'
check "help line is the tasks page's" "$SCRATCH/out/10-tasks.txt" 'N project'

echo "== new project, task and subtask"
b send-text -s host -w deck 'N'; settle
b send-text -s host -w deck 'web'; b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 11-project
check "project added and selected" "$SCRATCH/out/11-project.txt" 'added project web'
check "project detail shows its theme" "$SCRATCH/out/11-project.txt" 'theme *[a-z-]* ◆'
b send-keys -s host -w deck n >/dev/null; settle; deckcap 11b-task-form
check "n on a project opens a task form for it" "$SCRATCH/out/11b-task-form.txt" 'new task in web'
b send-text -s host -w deck 'landing'; b send-keys -s host -w deck Enter >/dev/null; settle
b send-keys -s host -w deck n >/dev/null; settle; deckcap 11c-subtask-form
check "n on a task opens a subtask form for it" "$SCRATCH/out/11c-subtask-form.txt" 'new subtask of landing'
b send-text -s host -w deck 'hero copy'; b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 12-tree
check "task listed under the project" "$SCRATCH/out/12-tree.txt" 'landing'
check "subtask listed under the task" "$SCRATCH/out/12-tree.txt" '    . *hero copy'
check "subtask detail shows its path" "$SCRATCH/out/12-tree.txt" 'path *web › landing › hero copy'

echo "== rename and toggle done"
b send-keys -s host -w deck e >/dev/null; settle
b send-keys -s host -w deck ctrl+u >/dev/null; b send-text -s host -w deck 'hero text'; b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 13-renamed
check "subtask renamed" "$SCRATCH/out/13-renamed.txt" 'renamed to hero text'
b send-keys -s host -w deck s >/dev/null; settle; deckcap 14-done
check "subtask marked done" "$SCRATCH/out/14-done.txt" '✓ hero text'
check "status says it is done" "$SCRATCH/out/14-done.txt" 'hero text is done'
python3 - "$DATA" <<'PY' && echo "PASS store holds the project, task and subtask in shape" || { echo "FAIL store shape after the tasks page"; fails=$((fails+1)); }
import json, sys
d = json.load(open(sys.argv[1]))
web = [p for p in d["projects"] if p["name"] == "web"][0]
landing = [t for t in d["tasks"] if t["title"] == "landing"][0]
hero = [t for t in d["tasks"] if t["title"] == "hero text"][0]
assert landing["project_id"] == web["id"] and landing["parent_id"] == "", landing
assert hero["parent_id"] == landing["id"] and hero["project_id"] == "", hero
assert hero["status"] == "done" and landing["status"] == "open"
assert landing["theme"]["color"] != hero["theme"]["color"]
PY
b send-keys -s host -w deck s >/dev/null; settle; deckcap 14b-skipped
check "s again marks the subtask skipped" "$SCRATCH/out/14b-skipped.txt" '⊘ hero text'
check "status says it is skipped" "$SCRATCH/out/14b-skipped.txt" 'hero text is skipped'
check "a skipped task does not count as open" "$SCRATCH/out/14b-skipped.txt" '◆ web *1 open'
python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); sys.exit(0 if [t for t in d["tasks"] if t["title"]=="hero text"][0]["status"]=="skipped" else 1)' "$DATA" \
	&& echo "PASS store holds the subtask as skipped" || { echo "FAIL hero text is not skipped in $DATA"; fails=$((fails+1)); }

echo "== delete the subtask"
b send-keys -s host -w deck x >/dev/null; settle; deckcap 15-confirm
check "delete asks and names what cascades" "$SCRATCH/out/15-confirm.txt" 'delete task hero text, 0 subtasks and 0 assignments'
b send-keys -s host -w deck y >/dev/null; settle
wait_store '"hero text"' absent 10 && echo "PASS subtask gone from the store" || { echo "FAIL hero text still in $DATA"; fails=$((fails+1)); }
deckcap 16-deleted
check "selection fell back to the task" "$SCRATCH/out/16-deleted.txt" 'path *web › landing$'

echo "== a project's status cycles too"
b send-keys -s host -w deck k >/dev/null; b send-keys -s host -w deck s >/dev/null; settle; deckcap 16b-project-done
check "s marks the project done" "$SCRATCH/out/16b-project-done.txt" 'web is done'
check "a done project's header shows it" "$SCRATCH/out/16b-project-done.txt" '◆ web ✓ done · 1 open'
b send-keys -s host -w deck s >/dev/null; settle; deckcap 16c-project-skipped
check "a skipped project's header shows it" "$SCRATCH/out/16c-project-skipped.txt" '◆ web ⊘ skipped · 1 open'
check "project detail shows its status" "$SCRATCH/out/16c-project-skipped.txt" 'status *skipped'
python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); sys.exit(0 if [p for p in d["projects"] if p["name"]=="web"][0]["status"]=="skipped" else 1)' "$DATA" \
	&& echo "PASS store holds the project as skipped" || { echo "FAIL web is not skipped in $DATA"; fails=$((fails+1)); }
b send-keys -s host -w deck s >/dev/null; settle; deckcap 16d-project-open
check "a third s opens the project again" "$SCRATCH/out/16d-project-open.txt" 'web is open'
python3 - "$DATA" <<'PY' && echo "PASS the new project's colour is a palette name, not a black" || { echo "FAIL web's colour is not a usable palette name"; fails=$((fails+1)); }
import json, sys
d = json.load(open(sys.argv[1]))
web = [p for p in d["projects"] if p["name"] == "web"][0]
assert web["theme"]["color"] in "red green yellow blue magenta cyan white bright-red bright-green bright-yellow bright-blue bright-magenta bright-cyan bright-white".split(), web
PY
b send-keys -s host -w deck m >/dev/null; settle; deckcap 16e-colour
check "m reports the project's new colour" "$SCRATCH/out/16e-colour.txt" 'web is [a-z-]*'
python3 - "$DATA" <<'PY' && echo "PASS m moved the project to a colour no other open item holds" || { echo "FAIL m picked a held colour"; fails=$((fails+1)); }
import json, sys
d = json.load(open(sys.argv[1]))
web = [p for p in d["projects"] if p["name"] == "web"][0]
held = {p["theme"]["color"] for p in d["projects"] if p["status"] == "open" and p is not web}
held |= {t["theme"]["color"] for t in d["tasks"] if t["status"] == "open"}
assert web["theme"]["color"] not in held, (web, held)
PY

echo "== back to the deck, and enter jumps to an assigned pane"
b send-keys -s host -w deck Escape >/dev/null; settle; deckcap 17-deck
check "esc returns to the deck page" "$SCRATCH/out/17-deck.txt" 'tuios-deck.* deck '
check "deck help line is back" "$SCRATCH/out/17-deck.txt" 'n agent'
b send-text -s host -w deck 'T'; settle
b send-keys -s host -w deck g >/dev/null; b send-keys -s host -w deck j >/dev/null
b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 18-jump
check "enter on the task selects its pane on the deck" "$SCRATCH/out/18-jump.txt" 'target › extra .*deck › command palette'

echo "== form box: fuzzy completion list"
b send-keys -s host -w deck a >/dev/null; settle; deckcap 19-assign-box
check "assign form box shows its title" "$SCRATCH/out/19-assign-box.txt" '╭─ assign extra ─'
check "every field has its own line" "$SCRATCH/out/19-assign-box.txt" '  project: '
b send-text -s host -w deck 'and'; settle; deckcap 19b-fuzzy
check "a partial task lists the fuzzy matches, best highlighted" "$SCRATCH/out/19b-fuzzy.txt" '▸ deck / command palette'
check "the second match is listed" "$SCRATCH/out/19b-fuzzy.txt" '      web / landing'
b send-keys -s host -w deck ctrl+n >/dev/null; settle; deckcap 19c-next
check "ctrl+n highlights the second match" "$SCRATCH/out/19c-next.txt" '▸ web / landing'
b send-keys -s host -w deck Tab >/dev/null; settle; deckcap 19d-accept
check "tab accepts the highlighted match" "$SCRATCH/out/19d-accept.txt" '› task: web / landing▏'
b send-keys -s host -w deck Escape >/dev/null; settle; deckcap 19e-cancel
if grep -q '╭─ assign' "$SCRATCH/out/19e-cancel.txt"; then echo "FAIL form box still open after esc"; fails=$((fails+1)); else echo "PASS esc closes the form box"; fi

echo "== r refreshes every page without a panic"
# A screen taller than the detail pane leaves the scroll past zero while it
# follows, which is what r used to slice an emptied detail with.
a send-text -s target -w build 'seq 1 300; echo end-of-seq | tr a-z A-Z
'
a wait-for window-output -s target -w build --pattern 'END-OF-SEQ' --timeout 10000 >/dev/null
select_pane 1
b send-keys -s host -w deck r >/dev/null; settle; deckcap 20-r-deck
check "r on a long screen leaves the deck alive" "$SCRATCH/out/20-r-deck.txt" 'tuios-deck.* deck '
check "r keeps the followed screen in view" "$SCRATCH/out/20-r-deck.txt" '│END-OF-SEQ'
b send-text -s host -w deck ':'; settle
b send-text -s host -w deck 'list-win'; b send-keys -s host -w deck Enter >/dev/null; settle
b send-text -s host -w deck 'target'; b send-keys -s host -w deck Enter >/dev/null
wait_deck 'result of list-windows' 15 || true
b send-keys -s host -w deck r >/dev/null; settle; deckcap 20b-r-result
check "r in the result view leaves the deck alive" "$SCRATCH/out/20b-r-result.txt" 'tuios-deck.* deck '
if grep -q 'result of list-windows' "$SCRATCH/out/20b-r-result.txt"; then echo "FAIL r kept the result view"; fails=$((fails+1)); else echo "PASS r ends the result view"; fi
b send-text -s host -w deck 'T'; settle
b send-keys -s host -w deck r >/dev/null; settle; deckcap 20c-r-tasks
check "r on the tasks page leaves the deck alive" "$SCRATCH/out/20c-r-tasks.txt" 'tuios-deck.* tasks '
b send-keys -s host -w deck Escape >/dev/null; settle
b send-keys -s host -w deck c >/dev/null; settle
b send-text -s host -w deck 'C'; settle
b send-keys -s host -w deck r >/dev/null; settle; deckcap 20d-r-captures
check "r on the captures page leaves the deck alive" "$SCRATCH/out/20d-r-captures.txt" 'tuios-deck.* captures '
check "r on the captures page reports the count" "$SCRATCH/out/20d-r-captures.txt" '^[0-9][0-9]* captures*$'
b send-keys -s host -w deck Escape >/dev/null; settle

echo "== A adds projects, tasks and subtasks from an outline"
# Types an outline line by line, Enter between lines, into the open form.
type_lines() {
	local l first=1
	while IFS= read -r l; do
		[ "$first" = 1 ] || b send-keys -s host -w deck Enter >/dev/null
		first=0
		b send-text -s host -w deck "$l" >/dev/null
	done
}
b send-text -s host -w deck 'T'; settle
b send-keys -s host -w deck g >/dev/null; b send-keys -s host -w deck j >/dev/null; settle
b send-text -s host -w deck 'A'; settle; deckcap 20-mass-box
check "A opens the mass-add box" "$SCRATCH/out/20-mass-box.txt" '╭─ add projects and tasks ─'
check "the hint shows the example outline" "$SCRATCH/out/20-mass-box.txt" '│         verb autocomplete'
printf '  orphan\n' | type_lines
b send-keys -s host -w deck ctrl+s >/dev/null; settle; deckcap 20b-mass-refused
check "an indent under nothing is refused with its line" "$SCRATCH/out/20b-mass-refused.txt" 'line 1: indented under nothing'
b send-keys -s host -w deck ctrl+u >/dev/null
printf 'deck\n    too deep\n' | type_lines
b send-keys -s host -w deck ctrl+s >/dev/null; settle; deckcap 20c-mass-jump
check "an indent jump of two levels is refused with its line" "$SCRATCH/out/20c-mass-jump.txt" 'line 2: indented more than one level'
b send-keys -s host -w deck ctrl+u >/dev/null
printf '# the example from the hint\ndeck\n  command palette\n    verb autocomplete\n  captures page\nweb\n  landing\n' | type_lines
b send-keys -s host -w deck ctrl+s >/dev/null; settle; deckcap 20d-mass-added
check "status counts what was added; existing ones are reused" "$SCRATCH/out/20d-mass-added.txt" 'added 0 projects, 2 tasks'
check "the tasks page shows the new subtask" "$SCRATCH/out/20d-mass-added.txt" 'verb autocomplete'
check "the tasks page keeps its selection" "$SCRATCH/out/20d-mass-added.txt" 'path *deck › command palette$'
python3 - "$DATA" <<'PY2' && echo "PASS store holds the outline, nothing duplicated" || { echo "FAIL store shape after the mass add"; fails=$((fails+1)); }
import json, sys
d = json.load(open(sys.argv[1]))
proj = {p["name"]: p for p in d["projects"]}
assert [p["name"] for p in d["projects"]].count("web") == 1
deck, web = proj["deck"], proj["web"]
def tasks(title): return [t for t in d["tasks"] if t["title"] == title]
assert len(tasks("command palette")) == 1, tasks("command palette")
assert len(tasks("landing")) == 1 and tasks("landing")[0]["project_id"] == web["id"]
cp = tasks("command palette")[0]
cap = tasks("captures page")
assert len(cap) == 1 and cap[0]["project_id"] == deck["id"] and cap[0]["parent_id"] == "", cap
va = tasks("verb autocomplete")
assert len(va) == 1 and va[0]["parent_id"] == cp["id"], va
assert va[0]["theme"]["color"] and cap[0]["theme"]["color"]
PY2
b send-keys -s host -w deck Escape >/dev/null; settle

echo "== a task picker offers + add new…, which opens the mass-add screen"
select_pane 3
b send-keys -s host -w deck a >/dev/null; settle
b send-text -s host -w deck 'zzz'; b send-keys -s host -w deck ctrl+n >/dev/null; settle; deckcap 21-add-entry
check "the list ends with + add new…, highlighted by ctrl+n" "$SCRATCH/out/21-add-entry.txt" '▸ + add new…'
b send-keys -s host -w deck Tab >/dev/null; settle; deckcap 21b-mass-from-form
check "+ add new… opens the mass-add box" "$SCRATCH/out/21b-mass-from-form.txt" '╭─ add projects and tasks ─'
check "it is prefilled with the pane's project" "$SCRATCH/out/21b-mass-from-form.txt" '│     deck  '
check "and the typed task under it" "$SCRATCH/out/21b-mass-from-form.txt" '│       zzz▏'
b send-keys -s host -w deck Escape >/dev/null; settle; deckcap 21c-mass-esc
check "esc in the mass-add box returns to the assign form unchanged" "$SCRATCH/out/21c-mass-esc.txt" '› task: zzz▏'
b send-keys -s host -w deck ctrl+u >/dev/null
b send-text -s host -w deck 'brand new task'; b send-keys -s host -w deck ctrl+n >/dev/null; settle
b send-keys -s host -w deck Tab >/dev/null; settle
b send-keys -s host -w deck ctrl+s >/dev/null; settle; deckcap 21d-resumed
check "ctrl+s in the mass-add box returns to the assign form" "$SCRATCH/out/21d-resumed.txt" '╭─ assign extra ─'
check "the task field holds the new task's path" "$SCRATCH/out/21d-resumed.txt" '› task: deck / brand new task▏'
b send-keys -s host -w deck Enter >/dev/null            # task
b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 21e-assigned   # project: implied
check "status reports the assignment to the new task" "$SCRATCH/out/21e-assigned.txt" 'assigned extra to deck › brand new task'
python3 - "$DATA" "$EXTRA" <<'PY2' && echo "PASS extra is assigned to the new task in the store" || { echo "FAIL extra is not assigned to brand new task"; fails=$((fails+1)); }
import json, sys
d = json.load(open(sys.argv[1]))
deck = [p for p in d["projects"] if p["name"] == "deck"][0]
t = [t for t in d["tasks"] if t["title"] == "brand new task"]
assert len(t) == 1 and t[0]["project_id"] == deck["id"], t
assert d["assignments"][sys.argv[2]]["task_id"] == t[0]["id"]
PY2

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
