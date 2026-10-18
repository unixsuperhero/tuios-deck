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
# Stand-ins for the clipboard tools and the opener, so y, ctrl+o and w are
# seen in files here and never touch the person's clipboard or browser.
mkdir -p "$SCRATCH/bin"
for t in pbcopy wl-copy xclip; do printf '#!/bin/sh\ncat > "%s/clipboard.txt"\n' "$SCRATCH" > "$SCRATCH/bin/$t"; done
for t in open xdg-open; do printf '#!/bin/sh\necho "$@" >> "%s/opened.txt"\n' "$SCRATCH" > "$SCRATCH/bin/$t"; done
chmod +x "$SCRATCH"/bin/*
b new-window -s host deck --no-focus -- env PATH="$SCRATCH/bin:$PATH" TUIOS_DECK_DATA="$DATA" "$DECK" tui --socket "$ASOCK" --captures "$SCRATCH/captures" >/dev/null
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

echo "== context items: extract with i, list with I, star, filter"
a send-text -s target -w build 'echo ctx https://example.com/tui-link
'
a wait-for window-output -s target -w build --pattern 'ctx https://example.com/tui-link' --timeout 10000 >/dev/null
select_pane 1
b send-keys -s host -w deck i >/dev/null
wait_deck 'new context item' 15 && echo "PASS i reported its extraction" || { echo "FAIL i reported nothing"; fails=$((fails+1)); }
deckcap 06j-extract
check "status counts the new context items from build" "$SCRATCH/out/06j-extract.txt" '[1-9][0-9]* new context items\? from build'
b send-text -s host -w deck 'I'; settle; deckcap 06k-context
check "title names the context page" "$SCRATCH/out/06k-context.txt" 'tuios-deck.* context .*sort newest'
check "the link is listed with its kind" "$SCRATCH/out/06k-context.txt" 'link *https://example.com/tui-link'
check "help line is the context page's" "$SCRATCH/out/06k-context.txt" 'f filter · o/O group · \* star'
b send-keys -s host -w deck o >/dev/null; b send-keys -s host -w deck g >/dev/null; settle; deckcap 06k2-by-kind
first_kind=$(grep -o '^▾ [a-z]*' "$SCRATCH/out/06k2-by-kind.txt" | sed -n 1p | sed 's/^▾ //')
second_kind=$(grep -o '^▾ [a-z]*' "$SCRATCH/out/06k2-by-kind.txt" | sed -n 2p | sed 's/^▾ //')
[ -n "$second_kind" ] && echo "PASS the extracted items fall in two kinds or more" || { echo "FAIL fewer than two kind groups"; fails=$((fails+1)); }
check "g selects an item of the first kind" "$SCRATCH/out/06k2-by-kind.txt" "kind *$first_kind"
b send-text -s host -w deck '}'; settle; deckcap 06k3-next-kind
check "} jumps to the first item of the next kind" "$SCRATCH/out/06k3-next-kind.txt" "kind *$second_kind"
b send-text -s host -w deck '{'; settle; deckcap 06k4-prev-kind
check "{ jumps back to the first kind" "$SCRATCH/out/06k4-prev-kind.txt" "kind *$first_kind"
b send-text -s host -w deck 'O'; settle
b send-keys -s host -w deck f >/dev/null; settle
b send-text -s host -w deck 'link'; b send-keys -s host -w deck Enter >/dev/null
b send-keys -s host -w deck Enter >/dev/null; b send-keys -s host -w deck Enter >/dev/null; b send-keys -s host -w deck Enter >/dev/null; settle
b send-keys -s host -w deck g >/dev/null; settle; deckcap 06l-filtered
check "title shows the kind filter" "$SCRATCH/out/06l-filtered.txt" 'context .*kind link'
check "status confirms the filter" "$SCRATCH/out/06l-filtered.txt" 'filtered: kind link'
if grep -q ' command  ' "$SCRATCH/out/06l-filtered.txt"; then echo "FAIL kind filter kept a command"; fails=$((fails+1)); else echo "PASS kind filter keeps only links"; fi
check "detail shows the whole link" "$SCRATCH/out/06l-filtered.txt" 'pane *target › build'
b send-text -s host -w deck '*'; settle; deckcap 06m-starred
check "status says it is starred" "$SCRATCH/out/06m-starred.txt" 'starred https://example.com/tui-link'
check "the row carries the star" "$SCRATCH/out/06m-starred.txt" '★  link'
python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); sys.exit(0 if any(c["text"]=="https://example.com/tui-link" and c["starred"] and c["pane"]=="build" for c in d["context_items"]) else 1)' "$DATA" \
	&& echo "PASS store holds the starred link" || { echo "FAIL store has no starred link"; fails=$((fails+1)); }
b send-keys -s host -w deck y >/dev/null; settle; deckcap 06m2-copied
check "y reports the clipboard mechanisms" "$SCRATCH/out/06m2-copied.txt" 'copied https://example.com/tui-link with OSC 52 and \(pbcopy\|wl-copy\|xclip\)'
grep -qx 'https://example.com/tui-link' "$SCRATCH/clipboard.txt" && echo "PASS the clipboard tool got the link" || { echo "FAIL clipboard tool got nothing"; fails=$((fails+1)); }
a send-keys -s target -w build ctrl+u >/dev/null
(a wait-for window-output -s target -w build --pattern 'tui-link' --timeout 10000 >/dev/null; echo $? > "$SCRATCH/out/insert-wait.txt") &
waitpid=$!
b send-keys -s host -w deck Enter >/dev/null
wait "$waitpid"
grep -qx 0 "$SCRATCH/out/insert-wait.txt" && echo "PASS enter inserted the link into build" || { echo "FAIL the link never reached build"; fails=$((fails+1)); }
settle; deckcap 06m3-inserted
check "status says inserted, not submitted" "$SCRATCH/out/06m3-inserted.txt" 'inserted https://example.com/tui-link into build (not submitted)'
a capture-pane -s target -w build > "$SCRATCH/out/build-inserted.txt"
grep -v '^ *$' "$SCRATCH/out/build-inserted.txt" | tail -1 | grep -q 'https://example.com/tui-link *$' && echo "PASS the link waits on the prompt line, unsubmitted" || { echo "FAIL the prompt line does not end with the link"; fails=$((fails+1)); }
a send-keys -s target -w build ctrl+u >/dev/null
b send-keys -s host -w deck ctrl+o >/dev/null; settle; deckcap 06m4-opened
check "ctrl+o opens with the platform opener" "$SCRATCH/out/06m4-opened.txt" 'opened https://example.com/tui-link with \(open\|xdg-open\)'
grep -q 'https://example.com/tui-link' "$SCRATCH/opened.txt" && echo "PASS the opener got the link" || { echo "FAIL the opener got nothing"; fails=$((fails+1)); }
b send-keys -s host -w deck w >/dev/null; settle; deckcap 06m5-with
check "w asks for a program, offering the opener" "$SCRATCH/out/06m5-with.txt" 'program: .*\(open\|xdg-open\) (tab to take)'
b send-keys -s host -w deck Tab >/dev/null; b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 06m6-with-done
check "open with reports the program" "$SCRATCH/out/06m6-with-done.txt" 'opened https://example.com/tui-link with \(open\|xdg-open\)'
[ "$(grep -c tui-link "$SCRATCH/opened.txt")" -ge 2 ] && echo "PASS open with ran the program" || { echo "FAIL open with did not run"; fails=$((fails+1)); }
b send-keys -s host -w deck s >/dev/null; settle; deckcap 06n-sorted
check "s cycles the sort" "$SCRATCH/out/06n-sorted.txt" 'sorted kind'
b send-keys -s host -w deck o >/dev/null; settle; deckcap 06o-grouped
check "o groups the items by kind" "$SCRATCH/out/06o-grouped.txt" '▾ link'
b send-keys -s host -w deck Escape >/dev/null; settle

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
# o first reaches task tree, checked after the mass add below; the second
# is project.
b send-text -s host -w deck 'oo'; settle; deckcap 07b-by-project
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
# deck › command palette holds extra; deck › (none) holds build and sleeper;
# the (none) project holds a Terminal pane and fake.
b send-keys -s host -w deck g >/dev/null; settle
b send-text -s host -w deck '}'; settle; deckcap 07b3-next-group
check "} jumps from extra to the first pane of the next group" "$SCRATCH/out/07b3-next-group.txt" 'target › build  ◆ deck'
b send-text -s host -w deck '{'; settle; deckcap 07b4-prev-group
check "{ returns to the first pane of the previous group" "$SCRATCH/out/07b4-prev-group.txt" 'target › extra  .*deck › command palette'
b send-text -s host -w deck '{'; settle; deckcap 07b5-first-group
check "{ in the first group stays put" "$SCRATCH/out/07b5-first-group.txt" 'target › extra  .*deck › command palette'
b send-text -s host -w deck '('; settle; deckcap 07b6-level-up
check "( steps the jumps up to the project level" "$SCRATCH/out/07b6-level-up.txt" '{/} jump by project'
b send-text -s host -w deck '}'; settle; deckcap 07b7-next-project
check "} at the project level jumps to the next project's first pane" "$SCRATCH/out/07b7-next-project.txt" 'target › Terminal [0-9a-f]*$'
b send-text -s host -w deck ')'; settle; deckcap 07b8-level-down
check ") steps back to the parent-task level" "$SCRATCH/out/07b8-level-down.txt" '{/} jump by parent task'
check "the help line names the group keys" "$SCRATCH/out/07b8-level-down.txt" '{/} group · (/) level'
b send-keys -s host -w deck o >/dev/null; settle; deckcap 07c-by-task
check "title says by task" "$SCRATCH/out/07c-by-task.txt" 'tuios-deck.* deck  by task'
check "task header row shows the path" "$SCRATCH/out/07c-by-task.txt" '^● deck › command palette'
b send-text -s host -w deck 'OOOO'; settle; deckcap 07d-by-session
check "O four times is back to session" "$SCRATCH/out/07d-by-session.txt" 'grouped by session'
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
check "task tree has an entry" "$SCRATCH/out/07f-filter.txt" 'group by task tree'
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
b send-text -s host -w deck 'ooo'; settle; deckcap 07g4-back
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
b send-text -s host -w deck 'OO'; settle; deckcap 07n-session-again
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
b send-text -s host -w deck '(('; settle; deckcap 12b-parent
check "( twice walks from the subtask up to its project" "$SCRATCH/out/12b-parent.txt" 'tasks *2 (2 open)'
b send-text -s host -w deck '{'; settle; deckcap 12c-prev-project
check "{ from a project selects the previous project" "$SCRATCH/out/12c-prev-project.txt" 'tasks *1 (1 open)'
b send-text -s host -w deck '}'; settle; deckcap 12d-next-project
check "} from a project selects the next project" "$SCRATCH/out/12d-next-project.txt" 'tasks *2 (2 open)'
b send-text -s host -w deck ')'; settle; deckcap 12e-child
check ") from a project selects its first task" "$SCRATCH/out/12e-child.txt" 'path *web › landing$'
b send-text -s host -w deck '('; settle; deckcap 12f-back
check "( from the task returns to the project" "$SCRATCH/out/12f-back.txt" 'tasks *2 (2 open)'
b send-text -s host -w deck '))'; settle; deckcap 12g-subtask
check ") twice is back on the subtask" "$SCRATCH/out/12g-subtask.txt" 'path *web › landing › hero copy'

echo "== rename through the edit form and toggle done"
b send-keys -s host -w deck e >/dev/null; settle; deckcap 13a-edit-form
check "e opens the edit form for the task" "$SCRATCH/out/13a-edit-form.txt" '╭─ edit task hero copy ─'
check "edit form offers the parent, current one by default" "$SCRATCH/out/13a-edit-form.txt" '  parent: web / landing'
check "edit form offers status, colour and glyph" "$SCRATCH/out/13a-edit-form.txt" '  status: open'
b send-keys -s host -w deck ctrl+u >/dev/null; b send-text -s host -w deck 'hero text'; b send-keys -s host -w deck ctrl+s >/dev/null; settle; deckcap 13-renamed
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

echo "== edit a task: move it and set its status in one form"
b send-keys -s host -w deck n >/dev/null; b send-text -s host -w deck 'docs'; b send-keys -s host -w deck Enter >/dev/null; settle
b send-keys -s host -w deck n >/dev/null; b send-text -s host -w deck 'api ref'; b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 20a-api-ref
check "api ref is a subtask of docs" "$SCRATCH/out/20a-api-ref.txt" 'path *web › docs › api ref'
b send-keys -s host -w deck e >/dev/null; settle
b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 20b-parent-list   # title kept
check "parent field lists the top-level entries" "$SCRATCH/out/20b-parent-list.txt" '(top level of deck)'
if grep -q 'web / docs / api ref' "$SCRATCH/out/20b-parent-list.txt"; then echo "FAIL the parent list offers the task itself"; fails=$((fails+1)); else echo "PASS the parent list leaves out the task itself"; fi
b send-text -s host -w deck 'web / landing'; b send-keys -s host -w deck Enter >/dev/null
b send-text -s host -w deck 'done'; b send-keys -s host -w deck ctrl+s >/dev/null; settle; deckcap 20c-moved
check "status reports the move and the status" "$SCRATCH/out/20c-moved.txt" 'api ref: moved under web / landing, status done'
check "selection stays on the edited task" "$SCRATCH/out/20c-moved.txt" 'path *web › landing › api ref'
python3 - "$SCRATCH/out/20c-moved.txt" <<'PY2' && echo "PASS the tree redraws api ref under landing" || { echo "FAIL api ref is not drawn under landing"; fails=$((fails+1)); }
import sys
lines = open(sys.argv[1]).read().splitlines()
at = lambda pat: next(i for i, l in enumerate(lines) if pat in l)
assert at(" landing") < at("✓ api ref") < at(" docs"), lines
PY2
python3 - "$DATA" <<'PY2' && echo "PASS store holds api ref under landing, done" || { echo "FAIL api ref's parent or status in the store"; fails=$((fails+1)); }
import json, sys
d = json.load(open(sys.argv[1]))
landing = [t for t in d["tasks"] if t["title"] == "landing"][0]
api = [t for t in d["tasks"] if t["title"] == "api ref"][0]
assert api["parent_id"] == landing["id"] and api["project_id"] == "" and api["status"] == "done", api
PY2
b send-keys -s host -w deck e >/dev/null; settle
b send-keys -s host -w deck Enter >/dev/null
b send-text -s host -w deck '(top level of deck)'; b send-keys -s host -w deck ctrl+s >/dev/null; settle; deckcap 20d-top
check "status reports the move to a project's top level" "$SCRATCH/out/20d-top.txt" 'api ref: moved to the top level of deck'
check "api ref is top-level in deck" "$SCRATCH/out/20d-top.txt" 'path *deck › api ref'
python3 - "$DATA" <<'PY2' && echo "PASS store holds api ref at the top level of deck" || { echo "FAIL api ref is not top-level in deck"; fails=$((fails+1)); }
import json, sys
d = json.load(open(sys.argv[1]))
api = [t for t in d["tasks"] if t["title"] == "api ref"][0]
assert api["parent_id"] == "" and api["project_id"] == "p_e2e001", api
PY2
b send-keys -s host -w deck n >/dev/null; b send-text -s host -w deck 'child'; b send-keys -s host -w deck Enter >/dev/null; settle
b send-keys -s host -w deck k >/dev/null; b send-keys -s host -w deck e >/dev/null; settle
b send-keys -s host -w deck Enter >/dev/null
b send-text -s host -w deck 'deck / api ref / child'; b send-keys -s host -w deck ctrl+s >/dev/null; settle; deckcap 20e-cycle
check "moving a task under its own subtask is refused" "$SCRATCH/out/20e-cycle.txt" 'cannot move under itself or one of its subtasks'
b send-keys -s host -w deck Escape >/dev/null; settle
python3 - "$DATA" <<'PY2' && echo "PASS the refused move left the store alone" || { echo "FAIL the cycle reached the store"; fails=$((fails+1)); }
import json, sys
d = json.load(open(sys.argv[1]))
api = [t for t in d["tasks"] if t["title"] == "api ref"][0]
assert api["parent_id"] == "" and api["project_id"] == "p_e2e001", api
PY2

echo "== ctrl+h and ctrl+l walk the parent of a new task"
b send-keys -s host -w deck j >/dev/null; b send-keys -s host -w deck n >/dev/null; settle; deckcap 21a-new-under-child
check "n on a subtask starts under it" "$SCRATCH/out/21a-new-under-child.txt" '╭─ new subtask of child ─'
check "the hint names the parent's full path" "$SCRATCH/out/21a-new-under-child.txt" 'under deck › api ref › child · ctrl+h'
b send-keys -s host -w deck ctrl+h >/dev/null; b send-keys -s host -w deck ctrl+h >/dev/null; b send-keys -s host -w deck ctrl+h >/dev/null; settle; deckcap 21b-up
check "ctrl+h walks up to the project's top level and stops there" "$SCRATCH/out/21b-up.txt" '╭─ new task in deck ─'
check "the parent field shows the top-level entry" "$SCRATCH/out/21b-up.txt" '  parent: (top level of deck)'
b send-keys -s host -w deck ctrl+l >/dev/null; settle; deckcap 21c-down
check "ctrl+l walks back down the way it came" "$SCRATCH/out/21c-down.txt" '╭─ new subtask of api ref ─'
b send-keys -s host -w deck ctrl+h >/dev/null
b send-text -s host -w deck 'top again'; b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 21d-added
check "the new task is added at the top level" "$SCRATCH/out/21d-added.txt" 'added deck / top again'
python3 - "$DATA" <<'PY2' && echo "PASS store holds the new task at the top level of deck" || { echo "FAIL top again is not top-level in deck"; fails=$((fails+1)); }
import json, sys
d = json.load(open(sys.argv[1]))
t = [t for t in d["tasks"] if t["title"] == "top again"][0]
assert t["parent_id"] == "" and t["project_id"] == "p_e2e001", t
PY2
b send-keys -s host -w deck x >/dev/null; b send-keys -s host -w deck y >/dev/null
wait_store '"top again"' absent 10 || { echo "FAIL top again still in $DATA"; fails=$((fails+1)); }
b send-keys -s host -w deck g >/dev/null; b send-keys -s host -w deck j --repeat 2 >/dev/null; settle
b send-keys -s host -w deck x >/dev/null; b send-keys -s host -w deck y >/dev/null
wait_store '"api ref"' absent 10 && echo "PASS api ref cleaned up" || { echo "FAIL api ref still in $DATA"; fails=$((fails+1)); }

echo "== edit a project: name and status in one form"
b send-keys -s host -w deck g >/dev/null; b send-keys -s host -w deck e >/dev/null; settle; deckcap 20f-project-form
check "e on a project opens its edit form" "$SCRATCH/out/20f-project-form.txt" '╭─ edit project deck ─'
b send-keys -s host -w deck Enter >/dev/null; b send-text -s host -w deck 'done'; b send-keys -s host -w deck ctrl+s >/dev/null; settle; deckcap 20g-project-edited
check "status reports the project's change" "$SCRATCH/out/20g-project-edited.txt" 'deck: status done'
b send-keys -s host -w deck s >/dev/null; settle   # skipped
b send-keys -s host -w deck s >/dev/null; settle   # open again for the checks that follow

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

echo "== tab and shift+tab change the nesting level in the mass-add outline"
b send-text -s host -w deck 'A'; settle
b send-keys -s host -w deck BTab >/dev/null
b send-text -s host -w deck 'deck'; settle; deckcap 20e-mass-btab0
check "shift+tab at no indent leaves the line alone" "$SCRATCH/out/20e-mass-btab0.txt" '│     deck▏'
check "the keys line names tab/shift+tab indent" "$SCRATCH/out/20e-mass-btab0.txt" 'tab/shift+tab indent'
b send-keys -s host -w deck Enter >/dev/null
b send-text -s host -w deck 'tabbed one'; b send-keys -s host -w deck Tab >/dev/null; settle; deckcap 20f-mass-tab
check "tab indents the line being typed by one level" "$SCRATCH/out/20f-mass-tab.txt" '│       tabbed one▏'
b send-keys -s host -w deck Enter >/dev/null
b send-text -s host -w deck 'tabbed child'; b send-keys -s host -w deck 'Tab Tab' >/dev/null; settle; deckcap 20g-mass-tab2
check "tab twice indents by two levels" "$SCRATCH/out/20g-mass-tab2.txt" '│         tabbed child▏'
b send-keys -s host -w deck Enter >/dev/null
b send-text -s host -w deck 'tabbed sibling'; b send-keys -s host -w deck 'Tab Tab BTab' >/dev/null; settle; deckcap 20h-mass-btab
check "shift+tab outdents by one level" "$SCRATCH/out/20h-mass-btab.txt" '│       tabbed sibling▏'
b send-keys -s host -w deck ctrl+s >/dev/null; settle; deckcap 20i-mass-tabbed
check "the tabbed outline is added" "$SCRATCH/out/20i-mass-tabbed.txt" 'added 0 projects, 3 tasks'
python3 - "$DATA" <<'PY3' && echo "PASS store nests the tabbed outline" || { echo "FAIL store shape after the tabbed mass add"; fails=$((fails+1)); }
import json, sys
d = json.load(open(sys.argv[1]))
deck = [p for p in d["projects"] if p["name"] == "deck"][0]
def one(title):
    ts = [t for t in d["tasks"] if t["title"] == title]
    assert len(ts) == 1, (title, ts)
    return ts[0]
a, c, s = one("tabbed one"), one("tabbed child"), one("tabbed sibling")
assert a["project_id"] == deck["id"] and a["parent_id"] == "", a
assert c["parent_id"] == a["id"], c
assert s["project_id"] == deck["id"] and s["parent_id"] == "", s
PY3

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

echo "== prototypes page: U lists the pickers, and each one assigns for real"
select_pane 2
FAKE=$(window_id fake)
b send-text -s host -w deck 'U'; settle; deckcap 21p-protos
check "title names the prototypes page" "$SCRATCH/out/21p-protos.txt" 'tuios-deck.* prototypes '
check "row 1 is the single fuzzy list" "$SCRATCH/out/21p-protos.txt" ' 1 single fuzzy list *assign'
check "row 2 is the two-pane preview" "$SCRATCH/out/21p-protos.txt" ' 2 two-pane preview *assign'
check "row 3 is the tree" "$SCRATCH/out/21p-protos.txt" ' 3 tree *assign'
check "row 4 is the hotkey quick-pick" "$SCRATCH/out/21p-protos.txt" ' 4 hotkey quick-pick *assign'
check "row 5 is the miller columns" "$SCRATCH/out/21p-protos.txt" ' 5 miller columns *assign'
check "row 6 is the type-ahead chips" "$SCRATCH/out/21p-protos.txt" ' 6 type-ahead chips *assign'
check "a filter row drives the context page" "$SCRATCH/out/21p-protos.txt" ' 1 single fuzzy list *filter'
check "the right side names the pane an assign is for" "$SCRATCH/out/21p-protos.txt" 'enter assigns target › fake'
check "the right side lists the highlighted prototype's keys" "$SCRATCH/out/21p-protos.txt" 'ctrl+n/ctrl+p or ↓/↑ move'
# assigned PANE TASK-ID PROJECT-ID: the store holds that assignment for the pane.
assigned() { python3 -c 'import json,sys; a=json.load(open(sys.argv[1]))["assignments"].get(sys.argv[2],{}); sys.exit(0 if a.get("task_id","")==sys.argv[3] and a.get("project_id","")==sys.argv[4] else 1)' "$DATA" "$@"; }
LANDING=$(python3 -c 'import json,sys; print([t["id"] for t in json.load(open(sys.argv[1]))["tasks"] if t["title"]=="landing"][0])' "$DATA")

b send-keys -s host -w deck 'g Enter' >/dev/null; settle; deckcap 21q-fuzzy
check "prototype 1 opens as a box" "$SCRATCH/out/21q-fuzzy.txt" '╭─ assign fake · single fuzzy list'
check "prototype 1 lists every target with its path" "$SCRATCH/out/21q-fuzzy.txt" 'deck / command palette / verb autocomplete'
check "prototype 1 ends with + add new…" "$SCRATCH/out/21q-fuzzy.txt" '+ add new…'
b send-text -s host -w deck 'landi'; settle; deckcap 21r-fuzzy-typed
check "a fragment keeps the matching target" "$SCRATCH/out/21r-fuzzy-typed.txt" 'web / landing'
if grep -q 'verb autocomplete' "$SCRATCH/out/21r-fuzzy-typed.txt"; then echo "FAIL the fragment kept unmatched targets"; fails=$((fails+1)); else echo "PASS unmatched targets hidden"; fi
b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 21s-fuzzy-picked
check "status reports the assignment like the form does" "$SCRATCH/out/21s-fuzzy-picked.txt" 'assigned fake to web › landing'
check "the prototypes page stays up to try the next one" "$SCRATCH/out/21s-fuzzy-picked.txt" 'tuios-deck.* prototypes '
assigned "$FAKE" "$LANDING" "" && echo "PASS prototype 1 assigned fake to the task" || { echo "FAIL fake is not assigned to landing"; fails=$((fails+1)); }

b send-keys -s host -w deck g >/dev/null; b send-keys -s host -w deck j --repeat 3 >/dev/null; b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 21t-hotkey
check "prototype 4 numbers the most used target first" "$SCRATCH/out/21t-hotkey.txt" ' 1 ◆ deck .*2 panes'
check "prototype 4 offers 0 for more" "$SCRATCH/out/21t-hotkey.txt" ' 0   more…'
b send-keys -s host -w deck 1 >/dev/null; settle; deckcap 21u-hotkey-picked
check "1 assigns at once" "$SCRATCH/out/21u-hotkey-picked.txt" 'assigned fake to deck'
assigned "$FAKE" "" p_e2e001 && echo "PASS prototype 4 assigned fake to the project" || { echo "FAIL fake is not assigned to project deck"; fails=$((fails+1)); }

b send-keys -s host -w deck g >/dev/null; b send-keys -s host -w deck j --repeat 2 >/dev/null; b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 21v-tree
check "prototype 3 starts with the projects collapsed" "$SCRATCH/out/21v-tree.txt" '▸ ◆ deck'
b send-keys -s host -w deck l >/dev/null; settle; deckcap 21w-tree-open
check "l expands the project" "$SCRATCH/out/21w-tree-open.txt" '▾ ◆ deck'
check "the expanded project shows its tasks" "$SCRATCH/out/21w-tree-open.txt" '● command palette'
b send-keys -s host -w deck 'j Enter' >/dev/null; settle; deckcap 21x-tree-picked
check "enter on the task assigns it" "$SCRATCH/out/21x-tree-picked.txt" 'assigned fake to deck › command palette'
assigned "$FAKE" t_e2e001 "" && echo "PASS prototype 3 assigned fake to the task" || { echo "FAIL fake is not assigned to command palette"; fails=$((fails+1)); }

echo "== ? previews the highlighted target's details and recent screen"
b send-keys -s host -w deck 'g Enter' >/dev/null; settle
b send-text -s host -w deck 'palette'; settle
b send-text -s host -w deck '?'; settle; deckcap 21x2-preview
check "? opens the preview beside the list" "$SCRATCH/out/21x2-preview.txt" '│path *deck › command palette'
check "the preview shows the task's status" "$SCRATCH/out/21x2-preview.txt" '│status *open'
check "the preview lists the task's panes" "$SCRATCH/out/21x2-preview.txt" '│  target › fake'
wait_deck '── screen of fake' 10 && echo "PASS the preview shows the assigned pane's recent screen" || { echo "FAIL no screen of fake in the preview"; fails=$((fails+1)); }
b send-text -s host -w deck '?'; settle; deckcap 21x3-preview-off
if grep -q '│path *deck › command palette' "$SCRATCH/out/21x3-preview-off.txt"; then echo "FAIL ? did not close the preview"; fails=$((fails+1)); else echo "PASS ? again closes the preview"; fi
b send-keys -s host -w deck Escape >/dev/null; settle

cp "$DATA" "$SCRATCH/out/store-before-esc.json"
for row in 1 4 5; do # prototypes 2, 5 and 6
	b send-keys -s host -w deck g >/dev/null; b send-keys -s host -w deck j --repeat $row >/dev/null; b send-keys -s host -w deck Enter >/dev/null; settle
	deckcap "21y-open-$row"
	b send-text -s host -w deck 'deck'; b send-keys -s host -w deck l >/dev/null; settle
	b send-keys -s host -w deck Escape >/dev/null; settle; deckcap "21y-esc-$row"
	check "esc leaves prototype row $((row+1)) without assigning" "$SCRATCH/out/21y-esc-$row.txt" 'cancelled'
done
check "prototype 2 shows a preview beside the list" "$SCRATCH/out/21y-open-1.txt" 'path *deck'
check "prototype 5 shows the breadcrumb" "$SCRATCH/out/21y-open-4.txt" '╭─ assign fake · miller columns'
check "prototype 6 shows the input" "$SCRATCH/out/21y-open-5.txt" '╭─ assign fake · type-ahead chips'
cmp -s "$DATA" "$SCRATCH/out/store-before-esc.json" && echo "PASS the store is unchanged after the three cancels" || { echo "FAIL a cancelled prototype changed the store"; fails=$((fails+1)); }
b send-keys -s host -w deck Escape >/dev/null; settle; deckcap 21z-back
check "esc on the prototypes page returns to the deck" "$SCRATCH/out/21z-back.txt" 'tuios-deck.* deck '

echo "== the task tree: project, task, subtasks, then the pane"
# Terminal goes on the subtask verb autocomplete the mass add made, and fake
# is unassigned again so the (none) header shows.
select_pane 2
b send-keys -s host -w deck a >/dev/null; settle
b send-keys -s host -w deck Enter >/dev/null; b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 23a-unassign-fake
check "both fields empty unassigns fake" "$SCRATCH/out/23a-unassign-fake.txt" 'unassigned fake'
select_pane 0
b send-keys -s host -w deck a >/dev/null; settle
b send-text -s host -w deck 'verb autocomplete'; b send-keys -s host -w deck Tab >/dev/null
b send-keys -s host -w deck Enter >/dev/null; b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 23b-assign-sub
check "Terminal is assigned to the subtask" "$SCRATCH/out/23b-assign-sub.txt" 'assigned Terminal [0-9a-f]* to deck › command palette › verb autocomplete'
b send-keys -s host -w deck o >/dev/null; settle; deckcap 23c-tree
check "one o from session is task tree" "$SCRATCH/out/23c-tree.txt" 'tuios-deck.* deck  by task tree'
check "the project header at depth 0, just its glyph and name" "$SCRATCH/out/23c-tree.txt" '^◆ deck *│'
check "a pane with only the project sits right under it" "$SCRATCH/out/23c-tree.txt" '^  · deck  *build'
check "the task header indented under the project" "$SCRATCH/out/23c-tree.txt" '^  ● command palette *│'
check "a later task sorts after the earlier one, its pane one level below it" "$SCRATCH/out/23c-tree.txt" '^    · deck brand new task  extra'
check "the subtask header under the task" "$SCRATCH/out/23c-tree.txt" '^    . verb autocomplete *│'
check "the pane under the subtask" "$SCRATCH/out/23c-tree.txt" '^      · deck verb autocompl.*Terminal'
check "the selection stays on Terminal" "$SCRATCH/out/23c-tree.txt" 'target › Terminal [0-9a-f]*  '
awk '/│/ && /^[^ │]/ { last = $0 } END { exit !(last ~ /^▾ \(none\)/) }' "$SCRATCH/out/23c-tree.txt" \
	&& echo "PASS (none) is the last top-level header" || { echo "FAIL (none) is not the last top-level header"; fails=$((fails+1)); }
check "fake sits under (none)" "$SCRATCH/out/23c-tree.txt" '^  . !!!! fake'
if grep -q 'captures page\|landing\|◆ web' "$SCRATCH/out/23c-tree.txt"; then echo "FAIL a project or task with no pane shows"; fails=$((fails+1)); else echo "PASS projects and tasks with no pane under them are left out"; fi
b send-text -s host -w deck '('; settle; deckcap 23d-up-task
check "( climbs to the task level" "$SCRATCH/out/23d-up-task.txt" '{/} jump by task'
check "( keeps Terminal, the first pane under command palette" "$SCRATCH/out/23d-up-task.txt" 'target › Terminal [0-9a-f]*  '
b send-text -s host -w deck '('; settle; deckcap 23e-up-project
check "( again climbs to the project level" "$SCRATCH/out/23e-up-project.txt" '{/} jump by project'
check "( selects the project's first pane" "$SCRATCH/out/23e-up-project.txt" 'target › build  '
b send-text -s host -w deck '}'; settle; deckcap 23f-next
check "} moves to the next top-level header, (none)" "$SCRATCH/out/23f-next.txt" 'target › fake  '
b send-text -s host -w deck '{'; settle; deckcap 23g-prev
check "{ moves back to the project" "$SCRATCH/out/23g-prev.txt" 'target › build  '
b send-text -s host -w deck 'O'; settle; deckcap 23h-session
check "O returns to session" "$SCRATCH/out/23h-session.txt" 'grouped by session'

echo "== h and l move the focus between the rail and the main area"
# The build pane's screen holds the seq of the check above, taller than the detail.
select_pane 1
b send-keys -s host -w deck 1 >/dev/null; settle
b send-keys -s host -w deck l >/dev/null; settle; deckcap 21a-main
check "l focuses the main area: its help line" "$SCRATCH/out/21a-main.txt" 'main area: h rail · j/k scroll'
check "the rail keeps a dim marker on the selected pane" "$SCRATCH/out/21a-main.txt" '^▸.*build'
b send-keys -s host -w deck j >/dev/null; settle; deckcap 21b-j
check "j in the main area scrolls the detail (the ↓G indicator)" "$SCRATCH/out/21b-j.txt" '[0-9]*/[0-9]* ↓G'
check "j in the main area keeps the rail on build" "$SCRATCH/out/21b-j.txt" 'target › build'
b send-keys -s host -w deck k >/dev/null; settle; deckcap 21c-k
pos() { grep -o '[0-9]*/[0-9]* ↓G' "$1" | cut -d/ -f1; }
if [ -n "$(pos "$SCRATCH/out/21b-j.txt")" ] && [ "$(pos "$SCRATCH/out/21c-k.txt")" = "$(( $(pos "$SCRATCH/out/21b-j.txt") - 1 ))" ]; then echo "PASS k scrolls the detail up one line"; else echo "FAIL k did not move the detail one line up"; fails=$((fails+1)); fi
check "k in the main area keeps the rail on build" "$SCRATCH/out/21c-k.txt" 'target › build'
b send-keys -s host -w deck h >/dev/null; settle; deckcap 21d-rail
check "h returns the focus to the rail" "$SCRATCH/out/21d-rail.txt" 'j/k move · l main area'
b send-keys -s host -w deck j >/dev/null; settle; deckcap 21e-rail-j
check "j on the rail moves the selection again" "$SCRATCH/out/21e-rail-j.txt" 'target › fake'

echo "== the Turns tab: a shell's commands and an agent's prompts"
select_pane 1
b send-keys -s host -w deck 4 >/dev/null; settle; deckcap 22a-turns-build
check "the tab bar names the Turns tab" "$SCRATCH/out/22a-turns-build.txt" '4 Turns'
check "a row for the build pane's first command, exit 0" "$SCRATCH/out/22a-turns-build.txt" '\$ exit 0 .*echo hello-from-build'
b send-keys -s host -w deck l >/dev/null; b send-keys -s host -w deck k --repeat 20 >/dev/null
b send-keys -s host -w deck j >/dev/null; b send-keys -s host -w deck k >/dev/null; settle; deckcap 22b-turns-main
check "the help line names the accordion keys" "$SCRATCH/out/22b-turns-main.txt" 'j/k row · enter/space expand · y copy'
b send-keys -s host -w deck Enter >/dev/null; settle; deckcap 22c-turns-expanded
check "enter expands the first command row" "$SCRATCH/out/22c-turns-expanded.txt" '▾ [0-9:]* \$ exit 0 .*echo hello-from-build'
check "the expanded command shows its cwd" "$SCRATCH/out/22c-turns-expanded.txt" 'cwd  .*deck-e2e'
check "the expanded command shows its exit" "$SCRATCH/out/22c-turns-expanded.txt" 'exit 0 after'
b send-keys -s host -w deck y >/dev/null; settle; deckcap 22d-turns-copy
check "y copies the command line" "$SCRATCH/out/22d-turns-copy.txt" 'copied echo hello-from-build with OSC 52'
b send-keys -s host -w deck space >/dev/null; settle; deckcap 22e-turns-collapsed
check "space collapses the row again" "$SCRATCH/out/22e-turns-collapsed.txt" '▸ [0-9:]* \$ exit 0 .*echo hello-from-build'
b send-keys -s host -w deck h >/dev/null
select_pane 2; deckcap 22f-turns-fake
# The suite queued "do the thing" for the fake agent, which never takes it
# from the queue, so no prompt reaches its activity or a transcript.
if grep -q 'you> do the thing' "$SCRATCH/out/22f-turns-fake.txt"; then
	echo "PASS the fake agent's Turns tab lists the queued prompt"
else
	check "the fake agent's Turns tab says it has no prompts" "$SCRATCH/out/22f-turns-fake.txt" '(no prompts in the'
fi
b send-keys -s host -w deck 1 >/dev/null; settle

echo "== close a pane with confirmation"
select_pane 1
b send-keys -s host -w deck x >/dev/null; settle; deckcap 08-confirm
check "confirmation asked" "$SCRATCH/out/08-confirm.txt" 'close build'
b send-keys -s host -w deck y >/dev/null; settle
a list-windows -s target --json > "$SCRATCH/out/a-windows-after.json"
if grep -q '"build"' "$SCRATCH/out/a-windows-after.json"; then echo "FAIL build still open"; fails=$((fails+1)); else echo "PASS build closed"; fi
deckcap 09-final
check "list refreshed without build" "$SCRATCH/out/09-final.txt" 'closed build'
wait_store "\"$BUILD\": {" absent 10 && echo "PASS closed build's assignment pruned" || { echo "FAIL $BUILD still assigned in $DATA"; fails=$((fails+1)); }

echo "== quit"
b send-keys -s host -w deck q >/dev/null
b wait-for window-exit -s host -w deck --timeout 10000 >/dev/null && echo "PASS deck exited on q" || { echo "FAIL deck did not exit"; fails=$((fails+1)); }

echo; echo "captures in $SCRATCH/out; $fails failure(s)"
exit $fails
