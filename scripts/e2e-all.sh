#!/usr/bin/env bash
# Runs every e2e suite at once, each under its own scratch folder and port,
# and prints one line per suite: its exit status, wall time and PASS/FAIL
# counts. The exit status is the number of suites that failed. Logs land
# under .dev/e2e-all/<suite>.log; each suite's own evidence stays in its
# scratch folder under /tmp/deck-all-<suite>.
#
# The suites isolate by SCRATCH and PORT already, and a daemon costs under
# 100 ms to start, so the full run takes as long as the longest suite.
set -uo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
DECK=${DECK:-$here/.dev/deck}
LOGS=${LOGS:-$here/.dev/e2e-all}
[ -x "$DECK" ] || { echo "no deck binary at $DECK; build it first: GOWORK=off go build -o .dev/deck ./cmd/deck" >&2; exit 2; }
command -v tuios >/dev/null || { echo "tuios is not on PATH" >&2; exit 2; }
mkdir -p "$LOGS"
export DECK

# name  command (PORT and PORT+1 must not collide; reminders takes two)
suites=(
	"e2e           PORT=18900 scripts/e2e.sh"
	"web           PORT=18902 scripts/e2e-web.sh"
	"setup         scripts/e2e-setup.sh"
	"focus         PORT=18904 scripts/e2e-focus.sh"
	"history       PORT=18906 scripts/e2e-history.sh"
	"prompt-stash  scripts/e2e-prompt-stash.sh"
	"reminders     PORT=18908 scripts/e2e-reminders.sh"
	"help          python3 scripts/e2e-help.py"
	"turns         python3 scripts/e2e-turns.py"
	"review        PORT=18910 python3 scripts/e2e-review.py"
	"live          scripts/e2e-live.sh"
)
only=${ONLY:-}
pids=() names=()
start=$(date +%s)
for s in "${suites[@]}"; do
	name=${s%% *}
	cmd=${s#"$name"}
	if [ -n "$only" ] && ! grep -qw -- "$name" <<<"$only"; then continue; fi
	scratch=/tmp/deck-all-$name
	rm -rf "$scratch"
	( cd "$here" && env SCRATCH="$scratch" bash -c "s=\$(date +%s); $cmd; st=\$?; echo \"__exit \$st \$((\$(date +%s)-s))\"; exit \$st" ) > "$LOGS/$name.log" 2>&1 &
	pids+=($!); names+=("$name")
done
failed=0
printf '%-13s %5s %6s %5s %5s\n' suite exit secs pass fail
for i in "${!pids[@]}"; do
	wait "${pids[$i]}"; st=$?
	log=$LOGS/${names[$i]}.log
	secs=$(sed -n 's/^__exit [0-9]* \([0-9]*\)$/\1/p' "$log" | tail -1)
	pass=$(grep -c '^PASS' "$log"); fail=$(grep -c '^FAIL' "$log")
	printf '%-13s %5s %6s %5s %5s\n' "${names[$i]}" "$st" "${secs:-?}" "$pass" "$fail"
	[ "$st" -eq 0 ] || failed=$((failed+1))
done
echo "total: $(( $(date +%s) - start ))s, $failed suite(s) failed, logs in $LOGS"
exit "$failed"
