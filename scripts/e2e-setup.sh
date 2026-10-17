#!/usr/bin/env bash
# End-to-end check of `deck setup` and of the omp activity extension it
# installs. The setup commands run against a scratch HOME, so nothing on the
# machine changes. The extension is driven under bun with a fake pi and a
# fake tuios that records what would have been reported. Every check prints
# PASS or FAIL; the exit status is the number of failures.
set -uo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
DECK=${DECK:-$here/.dev/deck}
SCRATCH=${SCRATCH:-${TMPDIR:-/tmp}/tuios-deck-setup-e2e-$(id -u)}
rm -rf "$SCRATCH"; mkdir -p "$SCRATCH/home/.omp/agent" "$SCRATCH/out"
fails=0
check() { if grep -q -- "$3" "$2"; then echo "PASS $1"; else echo "FAIL $1: no '$3' in $2"; fails=$((fails+1)); fi; }
d() { HOME="$SCRATCH/home" "$DECK" setup "$@"; }
EXT="$SCRATCH/home/.omp/agent/extensions/tuios-deck-omp-activity.ts"

echo "== the cli"
"$DECK" > "$SCRATCH/out/noargs.txt" 2>&1; echo "exit=$?" >> "$SCRATCH/out/noargs.txt"
check "no subcommand prints help and exits 2" "$SCRATCH/out/noargs.txt" "exit=2"
check "help names the commands" "$SCRATCH/out/noargs.txt" "setup"
d > "$SCRATCH/out/setup-noargs.txt" 2>&1; echo "exit=$?" >> "$SCRATCH/out/setup-noargs.txt"
check "setup alone prints its help" "$SCRATCH/out/setup-noargs.txt" "omp-activity"
"$DECK" version > "$SCRATCH/out/version.txt" 2>&1
check "version names a build" "$SCRATCH/out/version.txt" "^deck \(v\|dev\)"
"$DECK" update -h > "$SCRATCH/out/update-help.txt" 2>&1
check "update -h names the module" "$SCRATCH/out/update-help.txt" "go install github.com/unixsuperhero/tuios-deck/cmd/deck@latest"

echo "== install lifecycle in a scratch HOME"
d status > "$SCRATCH/out/status-1.txt" 2>&1; check "status: missing" "$SCRATCH/out/status-1.txt" "omp-activity   missing"
d install > "$SCRATCH/out/install-1.txt" 2>&1; check "install writes the file" "$SCRATCH/out/install-1.txt" "installed"
[ -f "$EXT" ] && echo "PASS file exists at the target" || { echo "FAIL file missing"; fails=$((fails+1)); }
d status > "$SCRATCH/out/status-2.txt" 2>&1; check "status: current" "$SCRATCH/out/status-2.txt" "omp-activity   current"
d install > "$SCRATCH/out/install-2.txt" 2>&1; check "install again is a no-op" "$SCRATCH/out/install-2.txt" "already current"
echo "// edited" >> "$EXT"
d status > "$SCRATCH/out/status-3.txt" 2>&1; check "status: outdated after an edit" "$SCRATCH/out/status-3.txt" "omp-activity   outdated"
d install > "$SCRATCH/out/install-3.txt" 2>&1; check "install updates an outdated copy" "$SCRATCH/out/install-3.txt" "updated"
d print omp-activity > "$SCRATCH/out/print.ts" 2>&1; cmp -s "$SCRATCH/out/print.ts" "$EXT" && echo "PASS print matches the installed file" || { echo "FAIL print differs"; fails=$((fails+1)); }
d uninstall > "$SCRATCH/out/uninstall-1.txt" 2>&1; check "uninstall removes it" "$SCRATCH/out/uninstall-1.txt" "removed"
[ ! -e "$EXT" ] && echo "PASS file is gone" || { echo "FAIL file still there"; fails=$((fails+1)); }
echo "// mine" > "$EXT"
d status > "$SCRATCH/out/status-4.txt" 2>&1; check "status: foreign for a file the deck did not write" "$SCRATCH/out/status-4.txt" "omp-activity   foreign"
d install > "$SCRATCH/out/install-4.txt" 2>&1; check "install refuses a foreign file" "$SCRATCH/out/install-4.txt" "FAILED"
d uninstall > "$SCRATCH/out/uninstall-2.txt" 2>&1; check "uninstall leaves a foreign file" "$SCRATCH/out/uninstall-2.txt" "left in place"
[ -e "$EXT" ] && echo "PASS foreign file kept" || { echo "FAIL foreign file removed"; fails=$((fails+1)); }
d install --force > "$SCRATCH/out/install-5.txt" 2>&1; check "install --force replaces it" "$SCRATCH/out/install-5.txt" "replaced a foreign file"

echo "== the extension under bun, with a fake pi and a fake tuios"
if command -v bun >/dev/null; then
	export FAKE_TUIOS_LOG="$SCRATCH/out/fake-tuios.log"; : > "$FAKE_TUIOS_LOG"
	TUIOS_ENV=1 TUIOS_SESSION=work TUIOS_PANE_ID=pane-1 TUIOS_DECK_TUIOS="$here/scripts/omp-ext/fake-tuios.sh" \
		bun run "$here/scripts/omp-ext/check.ts" "$EXT" | tee "$SCRATCH/out/ext.txt"
	n=$(grep -c '^FAIL' "$SCRATCH/out/ext.txt"); fails=$((fails+n))
else
	echo "SKIP extension check: bun is not installed"
fi

echo; echo "output in $SCRATCH/out; $fails failure(s)"
exit $fails
