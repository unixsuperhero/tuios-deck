#!/usr/bin/env bash
# End-to-end check of the active project and task, and of projects on a git
# repository. Daemon A holds a shell pane; daemon B runs the deck in a pane.
# The store is seeded with a project on a repository under the scratch folder,
# which has a second worktree, and a project with no repository. Every check
# writes a capture into $SCRATCH/out and prints PASS or FAIL; the exit status
# is the number of failures.
set -uo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
DECK=${DECK:-$here/.dev/deck}
SCRATCH=${SCRATCH:-/tmp/deck-e2e-active-$(id -u)}
rm -rf "$SCRATCH"; mkdir -p "$SCRATCH/out" "$SCRATCH/captures" "$SCRATCH/store"
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
check_absent() { # name, file, pattern
	if grep -q -- "$3" "$2"; then echo "FAIL $1: '$3' in $2"; fails=$((fails+1)); else echo "PASS $1"; fi
}
ok() { if "${@:2}"; then echo "PASS $1"; else echo "FAIL $1"; fails=$((fails+1)); fi; }
deckcap() { b capture-pane -s host -w deck > "$SCRATCH/out/$1.txt"; }
key() { b send-keys -s host -w deck "$@" >/dev/null; }
text() { b send-text -s host -w deck "$1" >/dev/null; }
settle() { b wait-for window-idle -s host -w deck --idle 200 --timeout 10000 >/dev/null; }
wait_deck() { # pattern, seconds
	local i
	for ((i=0; i<$2*5; i++)); do
		if b capture-pane -s host -w deck 2>/dev/null | grep -q -- "$1"; then return 0; fi
		sleep .2
	done
	return 1
}
cancel_form() {
	key Escape; settle
	if b capture-pane -s host -w deck | grep -q '· NORMAL ·'; then key Escape; settle; fi
	if b capture-pane -s host -w deck | grep -q 'close this form without submitting'; then key y; settle; fi
}
store_json() { "$DECK" export -data "$DATA" > "$SCRATCH/out/store.json" && echo "$SCRATCH/out/store.json"; }
active_of() { python3 -c 'import json,sys; a=json.load(open(sys.argv[1])).get("active",{}); print(a.get("project_id",""), a.get("task_id",""))' "$(store_json)"; }
project_field() { python3 -c 'import json,sys; p=[p for p in json.load(open(sys.argv[1]))["projects"] if p["name"]==sys.argv[2]]; print(p[0].get(sys.argv[3],"") if p else "")' "$(store_json)" "$1" "$2"; }

echo "== a repository with a second worktree, and daemon A with a shell"
REPO="$SCRATCH/repo"; WT="$SCRATCH/wt"
git init -q "$REPO"
git -C "$REPO" -c user.email=e2e@example.com -c user.name=e2e commit -q --allow-empty -m init
git -C "$REPO" worktree add -q "$WT" -b side
REPO=$(cd "$REPO" && pwd -P); WT=$(cd "$WT" && pwd -P)   # git reports real paths (/private/tmp on macOS)
a new target --detach --cwd "$SCRATCH" >/dev/null
a new-window -s target build --cwd "$SCRATCH" --no-focus >/dev/null
ASOCK="$A/run/tuios/tuios.sock"

echo "== the store: a project on the repo with one task, and a project with no repo"
DATA="$SCRATCH/store/deck.json"
cat > "$DATA" <<JSON
{
  "version": 1,
  "projects": [
    { "id": "p_e2e001", "name": "deck", "theme": { "color": "blue", "glyph": "◆" }, "status": "open", "repo": "$REPO", "created": 1760000000 },
    { "id": "p_e2e002", "name": "web", "theme": { "color": "green", "glyph": "◆" }, "status": "open", "created": 1760000001 }
  ],
  "tasks": [
    { "id": "t_e2e001", "parent_id": "", "project_id": "p_e2e001", "title": "command palette", "theme": { "color": "magenta", "glyph": "●" }, "status": "open", "created": 1760000000, "updated": 1760000000 }
  ],
  "assignments": {}
}
JSON

echo "== daemon B: the deck in a pane, pointed at A"
b new boot --detach --cwd "$SCRATCH" >/dev/null
b call new-session "{\"name\":\"host\",\"width\":160,\"height\":50,\"cwd\":\"$SCRATCH\"}" >/dev/null
b new-window -s host deck --no-focus -- env TUIOS_DECK_DATA="$DATA" "$DECK" tui --socket "$ASOCK" --captures "$SCRATCH/captures" >/dev/null
b wait-for window-output -s host -w deck --pattern 'tuios-deck' --timeout 15000 >/dev/null
settle; deckcap 00-start
check "the store was migrated with the repo kept" "$(store_json)" "\"repo\": \"$REPO\""
check_absent "no active project at the start" "$SCRATCH/out/00-start.txt" '▶'

echo "== S opens the picker; a project becomes active"
key S; settle; deckcap 01-picker
check "S opens the active picker" "$SCRATCH/out/01-picker.txt" '╭─ active project or task'
check "the picker ends with + new project…" "$SCRATCH/out/01-picker.txt" '+ new project…'
text 'deck'; key Enter; settle; deckcap 02-active
check "the status names the active project" "$SCRATCH/out/02-active.txt" 'active: deck'
check "the title shows the active project" "$SCRATCH/out/02-active.txt" '▶ ◆ deck'
[ "$(active_of)" = "p_e2e001 " ] && echo "PASS the store holds the active project" || { echo "FAIL active is '$(active_of)'"; fails=$((fails+1)); }

echo "== a new terminal defaults to the active project and starts in its repo"
key 't i'; settle; deckcap 03-terminal-form
check "the project field defaults to the active project" "$SCRATCH/out/03-terminal-form.txt" '  project: deck'
key Enter; text 'inrepo'; key Enter; settle; deckcap 03b-cwd-hint
check "the cwd hint names the project's repo" "$SCRATCH/out/03b-cwd-hint.txt" "empty starts in $REPO"
key ctrl+s; settle
a wait-for window-output -s target -w inrepo --pattern '.' --timeout 10000 >/dev/null 2>&1 || true
a list-windows -s target --json > "$SCRATCH/out/windows.json"
check "the terminal opened" "$SCRATCH/out/windows.json" '"inrepo"'
cwd=$(python3 -c 'import json,sys; print([w.get("cwd","") for w in json.load(open(sys.argv[1]))["windows"] if w["title"]=="inrepo"][0])' "$SCRATCH/out/windows.json")
[ "$cwd" = "$REPO" ] && echo "PASS the terminal started in the repo" || { echo "FAIL the terminal's cwd is '$cwd', not $REPO"; fails=$((fails+1)); }
wait_deck 'inrepo' 10
python3 -c 'import json,sys; a=[v for v in json.load(open(sys.argv[1]))["assignments"].values() if v["project_id"]=="p_e2e001"]; sys.exit(0 if a else 1)' "$(store_json)" \
	&& echo "PASS the terminal is assigned to the active project" || { echo "FAIL no assignment to the active project"; fails=$((fails+1)); }

echo "== the project edit form offers the repo's worktrees"
text 'T'; settle; key g; settle
key 'e r'; settle; deckcap 04-repo-field   # r: the repo field's hotkey, straight into input mode there
check "the edit form has the repo field with its value" "$SCRATCH/out/04-repo-field.txt" "› repo: $REPO"
key Enter; settle; deckcap 04b-worktrees
check "the worktree field lists the second worktree" "$SCRATCH/out/04b-worktrees.txt" "$WT"
key ctrl+n; key ctrl+n; key Enter; settle; deckcap 04c-picked   # the second entry: the side worktree
check "the worktree is taken" "$SCRATCH/out/04c-picked.txt" "› worktree: $WT"
key ctrl+s; settle; deckcap 04d-saved
check "the status reports the worktree" "$SCRATCH/out/04d-saved.txt" "worktree .*wt"
[ "$(project_field deck worktree)" = "$WT" ] && echo "PASS the store holds the worktree" || { echo "FAIL worktree is '$(project_field deck worktree)'"; fails=$((fails+1)); }
check "the detail shows repo and worktree" "$SCRATCH/out/04d-saved.txt" 'worktree .*wt'
check "the tasks page marks the active project" "$SCRATCH/out/04d-saved.txt" 'deck ▶'

echo "== S on the tasks page makes the selected task active"
key j; key S; settle; deckcap 05-task-active
check "the status names project and task" "$SCRATCH/out/05-task-active.txt" 'active: deck › command palette'
check "the title shows both" "$SCRATCH/out/05-task-active.txt" '▶ ◆ deck › command palette'
[ "$(active_of)" = "p_e2e001 t_e2e001" ] && echo "PASS the store holds the active task" || { echo "FAIL active is '$(active_of)'"; fails=$((fails+1)); }
key Escape; settle
key 't i'; settle; deckcap 05b-task-default
check "the task field holds the active task" "$SCRATCH/out/05b-task-default.txt" 'task: deck / command palette'
key Enter; key Enter; settle; deckcap 05c-worktree-hint
check "an empty cwd now starts in the worktree" "$SCRATCH/out/05c-worktree-hint.txt" "empty starts in $WT"
cancel_form

echo "== + new project… makes a repository of a plain folder and activates the project"
key S; settle; key G; settle; deckcap 06-new-entry
check "G reaches + new project…" "$SCRATCH/out/06-new-entry.txt" 'opens the new project form'
key Enter; settle; deckcap 06b-form
check "the new project form opens" "$SCRATCH/out/06b-form.txt" '╭─ new project'
check "it asks for the repo" "$SCRATCH/out/06b-form.txt" 'repo:'
key i; text 'fresh'; key Enter; text "$SCRATCH/fresh"; key ctrl+s
wait_deck 'added project fresh' 10; deckcap 06c-added
check "the status names the project and its repo" "$SCRATCH/out/06c-added.txt" 'added project fresh on .*fresh · active'
ok "the folder was made a git repository" test -d "$SCRATCH/fresh/.git"
FRESH=$(cd "$SCRATCH/fresh" && pwd -P)
[ "$(project_field fresh repo)" = "$FRESH" ] && echo "PASS the store holds the repo root git reports" || { echo "FAIL repo is '$(project_field fresh repo)'"; fails=$((fails+1)); }
check "the new project is the active one" "$SCRATCH/out/06c-added.txt" '▶ ◆ fresh'
key 't i'; settle; deckcap 06d-fresh-hint
check "a new terminal defaults to the new project" "$SCRATCH/out/06d-fresh-hint.txt" '  project: fresh'
cancel_form

echo "== a worktree without a repo is refused; clearing the active pair"
text 'T'; settle; key g; settle; key j; settle   # the second row: fresh? rows sort by name: deck, fresh, web
key G; settle; key 'e w'; text "$WT"; key ctrl+s; settle; deckcap 07-refused   # w: the worktree field's hotkey
check "a worktree needs its repo" "$SCRATCH/out/07-refused.txt" 'a worktree needs its repo'
cancel_form
key Escape; settle
text ':'; settle; text 'clear active'; key Enter; settle; deckcap 08-cleared
check "the status says there is no active project" "$SCRATCH/out/08-cleared.txt" 'no active project'
check_absent "the title no longer shows ▶" "$SCRATCH/out/08-cleared.txt" '▶ ◆'
[ "$(active_of)" = " " ] && echo "PASS the store holds no active pair" || { echo "FAIL active is '$(active_of)'"; fails=$((fails+1)); }

echo "== quit"
key q; settle; key y

echo "$fails failure(s); captures in $SCRATCH/out"
exit "$fails"
