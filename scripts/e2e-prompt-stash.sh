#!/usr/bin/env bash
# Prompt stashing and recovery through the real TUI, with isolated daemons.
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
DECK=${DECK:-$here/.dev/deck}
SCRATCH=${SCRATCH:-$(mktemp -d /tmp/deck-stash.XXXX)}
mkdir -p "$SCRATCH/out" "$SCRATCH/captures"
DATA="$SCRATCH/deck.json"
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
a() { (isolate "$SCRATCH/A"; tuios "$@"); }
B="$SCRATCH/B"
b() { (isolate "$B"; tuios "$@"); }
cleanup() { a kill-server >/dev/null 2>&1 || :; b kill-server >/dev/null 2>&1 || :; }
trap cleanup EXIT
keys() { b send-keys -s host -w deck "$1" >/dev/null; settle; }
text() { b send-text -s host -w deck "$1" >/dev/null; settle; }
paste() { text $'\033[200~'"$1"$'\033[201~'; }
settle() { b wait-for window-idle -s host -w deck --idle 700 --timeout 10000 >/dev/null; }
cap() { b capture-pane -s host -w deck > "$SCRATCH/out/$1.txt"; }
check() { grep -Fq -- "$2" "$SCRATCH/out/$1.txt" || { echo "FAIL $1: missing $2"; exit 1; }; echo "PASS $1: $2"; }
absent() { if grep -Fq -- "$2" "$SCRATCH/out/$1.txt"; then echo "FAIL $1: unexpected $2"; exit 1; fi; echo "PASS $1: absent $2"; }
palette() { keys ctrl+p; text "$1"; keys Enter; }
start_deck() {
  b new boot --detach --cwd "$SCRATCH" >/dev/null
  b call new-session "{\"name\":\"host\",\"width\":160,\"height\":50,\"cwd\":\"$SCRATCH\"}" >/dev/null
  b new-window -s host deck --no-focus -- env TUIOS_DECK_DATA="$DATA" "$DECK" tui --socket "$SCRATCH/A/run/tuios/tuios.sock" --captures "$SCRATCH/captures" >/dev/null
  b wait-for window-output -s host -w deck --pattern tuios-deck --timeout 15000 >/dev/null
  settle
}
a new target --detach --cwd "$SCRATCH" >/dev/null
a new-window -s target fake --no-focus -- sh -c 'tuios set-agent-state working -s "$TUIOS_SESSION" -w "$TUIOS_PANE_ID" --harness claude-code; exec sleep 600' >/dev/null
a wait-for agent-state -s target -w fake --until working --timeout 10000 >/dev/null
cat > "$DATA" <<'JSON'
{"version":1,"assignments":{},"queued_prompts":[{"id":"pq_stash","text":"queued draft","session":"target","agent":"claude","created":1760000000}]}
JSON
start_deck
keys 'g j p ctrl+r'; cap empty
check empty 'No stashed prompts.'
keys Escape
paste $'keep-first\n日本語 café\nrecover-last'
keys ctrl+u; cap cleared
check cleared 'keep-first'
check cleared '日本語 café'
absent cleared 'recover-last'
python3 - "$DATA" <<'PY'
import json,sys
s=json.load(open(sys.argv[1]))['prompt_stashes']
assert [p['text'] for p in s] == ['keep-first\n日本語 café\nrecover-last'], s
print('PASS full exact Unicode prompt persisted before line deletion')
PY
keys 'ctrl+z ctrl+u Escape p'
paste 'displaced draft'
keys ctrl+r
paste 'café'; cap preview
check preview 'recover-last'
keys Escape; cap cancel
check cancel 'displaced draft▏'
keys ctrl+r; paste 'no-match'; cap no-match
check no-match 'No matching stashed prompts.'
keys 'ctrl+u'; paste 'café'; keys Enter; cap restored
check restored 'recover-last▏'
keys ctrl+z; cap undo-restore
check undo-restore 'displaced draft▏'
keys ctrl+r; paste 'café'; keys Enter
keys ctrl+s
python3 - "$DATA" <<'PY'
import json,sys
s=json.load(open(sys.argv[1]))
assert [p['text'] for p in s['prompt_stashes']] == ['displaced draft','keep-first\n日本語 café\nrecover-last'], s['prompt_stashes']
accepted=[p['detail'].split('\n\n',1)[1] for p in s['history'] if p['kind']=='prompt-queued']
assert accepted == ['keep-first\n日本語 café\nrecover-last'], accepted
print('PASS restoration saves replaced draft, deduplicates, and submits exact recovered text')
PY
keys 'p ctrl+r Down'; cap arrow-selection
check arrow-selection 'Preview 2/2'
keys 'Escape shift+tab ctrl+r'; cap nonprompt
absent nonprompt '╭─ stashed prompts'
keys Escape
# A fresh deck process must load the same stashes, not session-only history.
b kill-server >/dev/null
B="$SCRATCH/B-restarted"
start_deck
keys 'g j p ctrl+r'; paste 'café'; keys Enter; cap restart
check restart 'recover-last▏'
keys Escape
# Every prompt entry point offers recovery; notes and other fields do not.
keys 'g p ctrl+r'; paste 'café'; keys Enter; cap terminal
check terminal 'recover-last▏'
keys Escape
keys n
for i in 1 2 3 4 5 6 7; do keys Enter; done
keys ctrl+r; paste 'café'; keys Enter; cap new-agent
check new-agent 'recover-last▏'
keys Escape
palette 'queue prompt'
for i in 1 2 3 4 5 6 7; do keys Enter; done
keys ctrl+r; paste 'café'; keys Enter; cap new-queue
check new-queue 'recover-last▏'
keys Escape
palette 'queued prompts'
keys e
for i in 1 2 3 4 5 6; do keys Enter; done
keys ctrl+r; paste 'café'; keys Enter; cap edit-queue
check edit-queue 'recover-last▏'
keys 'Escape Escape'
keys 'g j p'
long_prompt=$(python3 -c 'print("LONG-PREVIEW-START\n" + "\n".join("preview row %d" % n for n in range(80)) + "\nLONG-PREVIEW-END")')
paste "$long_prompt"; keys ctrl+u; keys 'Escape p ctrl+r'; paste 'LONG-PREVIEW-START'; cap long-preview-start
check long-preview-start 'preview row 0'
absent long-preview-start 'LONG-PREVIEW-END'
b send-keys -s host -w deck PageDown --repeat 20 >/dev/null; settle; cap long-preview-end
check long-preview-end 'LONG-PREVIEW-END'
keys 'Escape Escape'
# An empty final line still permits stashing; identical and empty drafts do not multiply.
keys 'g j p'; paste $'trailing-newline\n'; keys 'ctrl+u ctrl+u'; keys Escape
keys p; paste 'single stash'; keys 'ctrl+u ctrl+u ctrl+u'; keys Escape
python3 - "$DATA" <<'PY'
import json,sys
texts=[p['text'] for p in json.load(open(sys.argv[1]))['prompt_stashes']]
assert texts.count('trailing-newline\n') == 1, texts
assert texts.count('single stash') == 1, texts
assert all(t.strip() for t in texts), texts
print('PASS trailing newline, identical-draft deduplication, empty-draft suppression')
PY
# Force atomic rename to fail even when running as root; never touch the real store.
keys p; paste 'cannot-lose-this-draft'
mv "$DATA" "$DATA.saved"; mkdir "$DATA"
keys ctrl+u; cap save-failure
check save-failure 'cannot-lose-this-draft▏'
check save-failure 'prompt not saved; draft unchanged'
keys ctrl+r; paste 'café'; keys Enter; cap restore-failure
check restore-failure 'prompt not saved; draft unchanged'
keys Escape; cap failed-restore-draft
check failed-restore-draft 'cannot-lose-this-draft▏'
rmdir "$DATA"; mv "$DATA.saved" "$DATA"
keys ctrl+r; paste 'cannot-lose'; cap rollback
check rollback 'No matching stashed prompts.'
keys 'Escape Escape'
# Vim normal mode can open recovery without interpreting picker input as commands.
b kill-server >/dev/null
B="$SCRATCH/B-vim"
mkdir -p "$B/config/tuios-deck"
printf '{"vim_modes":true}\n' > "$B/config/tuios-deck/config"
start_deck
keys 'g j p Escape ctrl+r'; paste 'café'; keys Enter; cap vim-recovery
check vim-recovery 'recover-last▏'
printf 'PASS prompt stash suite; evidence in %s/out\n' "$SCRATCH"
