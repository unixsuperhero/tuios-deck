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

echo "== release.sh bump maths, in a scratch repository"
R="$SCRATCH/repo"; mkdir -p "$R/scripts"; cp "$here/scripts/release.sh" "$R/scripts/release.sh"
( cd "$R" && git init -q && git -c user.name=t -c user.email=t@t commit -q --allow-empty -m init \
  && git tag v0.3.9 && git tag v0.10.2 && git tag v0.3.10 \
  && for b in "" patch minor major v2.0.0; do echo "$b: $(bash scripts/release.sh $b --dry-run | sed -n 's/^next:  *//p')"; done ) > "$SCRATCH/out/bumps.txt" 2>&1
cat "$SCRATCH/out/bumps.txt"
check "bare run bumps the patch of the newest tag" "$SCRATCH/out/bumps.txt" "^: v0.10.3$"
check "patch bumps the patch" "$SCRATCH/out/bumps.txt" "^patch: v0.10.3$"
check "minor bumps the minor" "$SCRATCH/out/bumps.txt" "^minor: v0.11.0$"
check "major bumps the major" "$SCRATCH/out/bumps.txt" "^major: v1.0.0$"
check "an explicit version is taken as is" "$SCRATCH/out/bumps.txt" "^v2.0.0: v2.0.0$"
( cd "$R" && git tag -d v0.3.9 v0.10.2 v0.3.10 >/dev/null && bash scripts/release.sh --dry-run ) > "$SCRATCH/out/bump-none.txt" 2>&1
check "no tag yet starts at v0.0.1" "$SCRATCH/out/bump-none.txt" "^next:  *v0.0.1$"

echo "== release.sh pushes a local main that is ahead, and refuses one that diverged"
B="$SCRATCH/origin.git"; git init -q --bare -b main "$B"
S="$SCRATCH/sync"; mkdir -p "$S/scripts"; cp "$here/scripts/release.sh" "$S/scripts/release.sh"; cp "$here/AGENTS.md" "$S/AGENTS.md"; echo '.dev/' > "$S/.gitignore"
G="git -c user.name=t -c user.email=t@t"
( cd "$S" && git init -q -b main && $G add -A && $G commit -q -m base && git remote add origin "$B" && git push -q origin main \
  && $G commit -q --allow-empty -m ahead && bash scripts/release.sh patch ) > "$SCRATCH/out/release-nolog.txt" 2>&1 \
  && { echo "FAIL a release without a changelog entry went ahead"; fails=$((fails+1)); } || echo "PASS no changelog entry stops the release"
check "the missing entry is named" "$SCRATCH/out/release-nolog.txt" "CHANGELOG.md has no entry for v0.0.1"
check "the prompt holds the rules from AGENTS.md" "$S/.dev/changelog-prompt.md" "^- .added: <thing>"
check "the prompt lists the commits since the last tag" "$S/.dev/changelog-prompt.md" "^- [0-9a-f]* ahead$"
[ "$(git -C "$B" rev-parse main)" != "$(git -C "$S" rev-parse HEAD)" ] && echo "PASS nothing was pushed without the entry" || { echo "FAIL main was pushed without the entry"; fails=$((fails+1)); }
( cd "$S" && printf '# Changelog\n\n## v0.0.1 — today\n\n### CLI\n- added: everything\n' > CHANGELOG.md && $G add CHANGELOG.md && $G commit -q -m 'docs: changelog' \
  && bash scripts/release.sh patch ) > "$SCRATCH/out/release-ahead.txt" 2>&1 || true
check "an ahead main is pushed before anything else" "$SCRATCH/out/release-ahead.txt" "^pushed main to origin"
[ "$(git -C "$B" rev-parse main)" = "$(git -C "$S" rev-parse HEAD)" ] && echo "PASS origin now holds the local main" || { echo "FAIL origin did not get the local main"; fails=$((fails+1)); }
( cd "$S" && git reset -q --hard HEAD~1 && { git tag -d v0.0.1 >/dev/null 2>&1 || true; } && printf '# Changelog\n\n## v0.0.1 — today\n\n### CLI\n- added: a different everything\n' > CHANGELOG.md && $G add CHANGELOG.md && $G commit -q -m diverged && bash scripts/release.sh patch ) > "$SCRATCH/out/release-diverged.txt" 2>&1 && { echo "FAIL a diverged main released"; fails=$((fails+1)); } || echo "PASS a diverged main stops the release"
check "the diverged message says what to do" "$SCRATCH/out/release-diverged.txt" "diverged from origin/main.*rebase or pull first"
[ "$(git -C "$B" rev-parse main)" != "$(git -C "$S" rev-parse HEAD)" ] && echo "PASS origin kept its main" || { echo "FAIL origin took the diverged main"; fails=$((fails+1)); }

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
