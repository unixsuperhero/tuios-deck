#!/usr/bin/env bash
# End-to-end check of the TUI's live mode against throwaway tuios daemons:
# enter goes live on the selected pane, typed keys reach the pane and its
# screen comes back, the prefix (ctrl+a) runs the live commands (zoom, the
# next and previous pane, the keys, detach), a paste is pasted, the size
# lease fits the pane to the deck and lets go on detach, under an attached
# tuios client Z and f zoom and fullscreen the pane in tuios with the tiling
# mode each needs, and a pane that closes under the deck ends the session. Same shape as e2e.sh: daemon A
# holds the panes, daemon B runs the deck in a pane driven by send-keys.
set -uo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
DECK=${DECK:-$here/.dev/deck}
SCRATCH=${SCRATCH:-/tmp/deck-e2e-live-$(id -u)}
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
check_absent() { # name, file, pattern
	if grep -q -- "$3" "$2"; then echo "FAIL $1: '$3' in $2"; fails=$((fails+1)); else echo "PASS $1"; fi
}
deckcap() { b capture-pane -s host -w deck > "$SCRATCH/out/$1.txt"; }
key() { b send-keys -s host -w deck "$@" >/dev/null; }
settle() { b wait-for window-idle -s host -w deck --idle 200 --timeout 10000 >/dev/null; }
# The deck draws on the alternate screen, which wait-for window-output does
# not read, so its screen is polled.
wait_deck() { # pattern, seconds
	local i
	for ((i=0; i<$2*5; i++)); do
		if b capture-pane -s host -w deck 2>/dev/null | grep -q -- "$1"; then return 0; fi
		sleep .2
	done
	return 1
}
wait_deck_gone() { # pattern, seconds
	local i
	for ((i=0; i<$2*5; i++)); do
		if ! b capture-pane -s host -w deck 2>/dev/null | grep -q -- "$1"; then return 0; fi
		sleep .2
	done
	return 1
}
# The pane's own size, as its program sees it: list-windows reports the
# window's rectangle, which a lease does not change. The shell in build
# prints it after the marker given, and the last such line is read back.
cols_of_build() { # marker
	a send-text -s target -w build "echo $1-\$(tput cols)x\$(tput lines)
"
	a wait-for window-output -s target -w build --pattern "$1-[0-9]*x[0-9]*" --timeout 10000 >/dev/null
	a capture-pane -s target -w build --scrollback | grep -o "$1-[0-9]*x[0-9]*" | tail -1 | sed "s/^$1-//"
}

echo "== daemon A: a wide session with a shell pane and a fake agent"
a new boot --detach --cwd "$SCRATCH" >/dev/null
a call new-session "{\"name\":\"target\",\"width\":200,\"height\":60,\"cwd\":\"$SCRATCH\"}" >/dev/null
a new-window -s target build --cwd "$SCRATCH" --no-focus >/dev/null
a send-text -s target -w build 'echo hello-from-build
'
a wait-for window-output -s target -w build --pattern 'hello-from-build' --timeout 10000 >/dev/null
a new-window -s target fake --no-focus -- sh -c 'tuios set-agent-state idle -s "$TUIOS_SESSION" -w "$TUIOS_PANE_ID" --harness claude-code -m "fake agent at rest"; exec sleep 600' >/dev/null
a wait-for agent-state -s target -w fake --until idle --timeout 10000 >/dev/null
ASOCK="$A/run/tuios/tuios.sock"
before=$(cols_of_build size0)
echo "build is $before before the deck"

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
b new boot --detach --cwd "$SCRATCH" >/dev/null
b call new-session "{\"name\":\"host\",\"width\":160,\"height\":50,\"cwd\":\"$SCRATCH\"}" >/dev/null
b new-window -s host deck --no-focus -- env TUIOS_DECK_DATA="$DATA" "$DECK" tui --socket "$ASOCK" --captures "$SCRATCH/captures" >/dev/null
b wait-for window-output -s host -w deck --pattern 'tuios-deck' --timeout 15000 >/dev/null
wait_deck 'fake' 10 || echo "WARN the rail never listed fake"

# Rows of the rail, in order: boot's shell, target's shell, build, fake.
echo "== enter goes live on build"
key g; key j --repeat 2; settle; deckcap 01-selected
check "build is selected before going live" "$SCRATCH/out/01-selected.txt" 'build'
key Enter
wait_deck 'LIVE' 5; deckcap 02-live
check "the live bar names the pane" "$SCRATCH/out/02-live.txt" 'LIVE .*target › build'
check "the help line says how to detach" "$SCRATCH/out/02-live.txt" 'ctrl+a d detach'
check "the pane's screen is shown live" "$SCRATCH/out/02-live.txt" 'hello-from-build'
during=$(cols_of_build size1)
if [ -n "$during" ] && [ "$during" != "$before" ]; then echo "PASS the lease fitted build to the deck ($during, was $before)"; else echo "FAIL build is still $before while live"; fails=$((fails+1)); fi
wait_deck "$(echo "$during" | tr x ×)" 5; deckcap 02b-size
check "the live bar shows the fitted size" "$SCRATCH/out/02b-size.txt" "$(echo "$during" | tr x ×)"

echo "== typed keys reach the pane and its output comes back"
b send-text -s host -w deck 'echo live-o""k-123'
key Enter
if a wait-for window-output -s target -w build --pattern 'live-ok-123' --timeout 10000 >/dev/null; then echo "PASS the typed command ran in build"; else echo "FAIL build never printed live-ok-123"; fails=$((fails+1)); fi
if wait_deck 'live-ok-123' 5; then echo "PASS the output reached the live screen"; else echo "FAIL the live screen never showed live-ok-123"; fails=$((fails+1)); fi
deckcap 03-typed
check_absent "no form or palette opened on the typed letters" "$SCRATCH/out/03-typed.txt" '─ command palette'

echo "== a paste is pasted, not typed as keys"
b call send-text '{"session":"host","window":"deck","text":"echo pasted-\"\"zz","paste":true}' >/dev/null
key Enter
if a wait-for window-output -s target -w build --pattern 'pasted-zz' --timeout 10000 >/dev/null; then echo "PASS the paste ran in build"; else echo "FAIL build never printed pasted-zz"; fails=$((fails+1)); fi

echo "== the prefix: an unknown key, zoom, help, the next and previous pane"
key ctrl+a; key x; settle; deckcap 04-unknown
check "an unknown key after the prefix says so" "$SCRATCH/out/04-unknown.txt" 'ctrl+a x is not a live command'
key ctrl+a; key z; settle; deckcap 05-zoom
check "zoom keeps the pane live" "$SCRATCH/out/05-zoom.txt" 'LIVE .*target › build'
check_absent "zoom hides the rail" "$SCRATCH/out/05-zoom.txt" 'fake'
key ctrl+a; key z; settle; deckcap 06-unzoom
check "zoom again brings the rail back" "$SCRATCH/out/06-unzoom.txt" 'fake'
key ctrl+a; key '?'; settle; deckcap 07-help
check "the prefix and ? open the live keys" "$SCRATCH/out/07-help.txt" 'Mode: live'
check "the help lists detach" "$SCRATCH/out/07-help.txt" 'detach: back to the deck'
check "the help lists the tuios zoom" "$SCRATCH/out/07-help.txt" 'zoom_pane'
key Escape; settle; deckcap 08-help-closed
check "esc closes the help and the session stays live" "$SCRATCH/out/08-help-closed.txt" 'LIVE .*target › build'
key ctrl+a; key j
wait_deck 'LIVE .*target › fake' 5; deckcap 09-next
check "the prefix and j go live on the next pane" "$SCRATCH/out/09-next.txt" 'LIVE .*target › fake'
key ctrl+a; key k
wait_deck 'LIVE .*target › build' 5; deckcap 10-prev
check "the prefix and k come back to build" "$SCRATCH/out/10-prev.txt" 'LIVE .*target › build'
key ctrl+a; key Escape; settle; deckcap 11-cancel
check "esc after the prefix cancels it" "$SCRATCH/out/11-cancel.txt" 'prefix cancelled'
check "and the session stays live" "$SCRATCH/out/11-cancel.txt" 'LIVE .*target › build'

echo "== the prefix and d detach: keys are the deck's again and the lease lets go"
key ctrl+a; key d
wait_deck 'detached from build' 5; deckcap 12-detached
check "detach says so" "$SCRATCH/out/12-detached.txt" 'detached from build'
check "the help line is the deck's again" "$SCRATCH/out/12-detached.txt" 'F1 keybindings'
check_absent "the live bar is gone" "$SCRATCH/out/12-detached.txt" 'LIVE'
sleep 0.3 # the daemon resizes a pane at most once every 100 ms
after=$(cols_of_build size2)
if [ "$after" = "$before" ]; then echo "PASS build is $before again after detach"; else echo "FAIL build is $after after detach, not $before"; fails=$((fails+1)); fi
key T; settle; deckcap 13-tasks
check "T opens the tasks page, so keys are the deck's" "$SCRATCH/out/13-tasks.txt" 'command palette'
key Escape; settle
a capture-pane -s target -w build > "$SCRATCH/out/build-after.txt"
check_absent "T was not typed into build" "$SCRATCH/out/build-after.txt" '\$ T$'

echo "== a tuios client shows target: the lease cannot grow a tile, so Z zooms and f fullscreens the pane in tuios"
# A client of A, in a pane of B, with A's folders and none of B's pane identity.
b new-window -s host client --no-focus -- env -u TUIOS_SESSION -u TUIOS_PANE_ID -u TUIOS_PANE_TOKEN -u TUIOS_WINDOW_ID -u TUIOS_SOCKET -u TUIOS_HOST -u TUIOS_SSH -u TUIOS_ENV -u TUIOS_SERVER \
	XDG_RUNTIME_DIR="$A/run" XDG_CONFIG_HOME="$A/config" XDG_CONFIG_DIRS="$A/config" XDG_STATE_HOME="$A/state" XDG_CACHE_HOME="$A/cache" XDG_DATA_HOME="$A/data" TMPDIR="$A/tmp" \
	tuios attach target >/dev/null
for ((i=0; i<50; i++)); do a call session-info '{"session":"target"}' | grep -q '"tui_attached": *true' && break; sleep .2; done
tiling() { a call session-info '{"session":"target"}' | grep -o '"tiling_mode": *"[a-z]*"' | grep -o '[a-z]*"$' | tr -d '"'; }
zoomed() { a list-windows -s target --json | grep -c '"zoomed": *true'; }
if [ "$(tiling)" = tiling ]; then echo "PASS the client attached with tiling on"; else echo "FAIL target is not tiled under the client ($(tiling))"; fails=$((fails+1)); fi
tile=$(cols_of_build size3)
key g; key j --repeat 2; settle; key Enter
wait_deck 'LIVE .*target › build' 5
live=$(cols_of_build size4)
if [ "$live" = "$tile" ]; then echo "PASS the lease left build at its tile, $tile, under the client"; else echo "FAIL build is $live live under the client, its tile was $tile"; fails=$((fails+1)); fi
key ctrl+a; key Z
wait_deck 'build is zoomed in target' 5; deckcap 15-zoomed
check "Z after the prefix zooms build in its session" "$SCRATCH/out/15-zoomed.txt" 'build is zoomed in target (tiling on)'
check "the live bar says zoomed" "$SCRATCH/out/15-zoomed.txt" 'LIVE .*· zoomed'
if [ "$(zoomed)" = 1 ]; then echo "PASS list-windows marks build zoomed"; else echo "FAIL no zoomed window in target"; fails=$((fails+1)); fi
# The client slides the pane to its zoom box; the stream's R frame moves the live bar's size when it lands.
wait_deck_gone "$(echo "$tile" | tr x ×)" 5 || echo "WARN the live bar still shows the tile size $tile"
zoom=$(cols_of_build size5)
if [ "${zoom#*x}" -gt "${tile#*x}" ]; then echo "PASS the zoom gave build more rows ($zoom, tile $tile)"; else echo "FAIL build is $zoom zoomed, tile $tile"; fails=$((fails+1)); fi
key ctrl+a; key Z
wait_deck 'build is back in its tile' 5; deckcap 16-unzoomed
check "Z again puts build back" "$SCRATCH/out/16-unzoomed.txt" 'build is back in its tile'
check_absent "and the live bar no longer says zoomed" "$SCRATCH/out/16-unzoomed.txt" '· zoomed'
if [ "$(zoomed)" = 0 ]; then echo "PASS no window is zoomed after Z again"; else echo "FAIL build is still zoomed"; fails=$((fails+1)); fi
key ctrl+a; key f
wait_deck 'build is fullscreen in target' 5; deckcap 17-fullscreen
check "f after the prefix makes build fullscreen in its session" "$SCRATCH/out/17-fullscreen.txt" 'build is fullscreen in target (tiling off)'
check "the live bar says fullscreen" "$SCRATCH/out/17-fullscreen.txt" 'LIVE .*· fullscreen'
if [ "$(tiling)" = floating ]; then echo "PASS fullscreen turned tiling off"; else echo "FAIL target is $(tiling) while fullscreen"; fails=$((fails+1)); fi
wait_deck_gone "$(echo "$tile" | tr x ×)" 5 || echo "WARN the live bar still shows the tile size $tile"
full=$(cols_of_build size6)
if [ "${full#*x}" -gt "${tile#*x}" ]; then echo "PASS fullscreen gave build more rows ($full, tile $tile)"; else echo "FAIL build is $full fullscreen, tile $tile"; fails=$((fails+1)); fi
key ctrl+a; key f
wait_deck 'build is back in its tile' 5; deckcap 18-unfull
check "f again puts build back" "$SCRATCH/out/18-unfull.txt" 'build is back in its tile'
if [ "$(tiling)" = tiling ]; then echo "PASS and tiling is on again"; else echo "FAIL target is $(tiling) after fullscreen ended"; fails=$((fails+1)); fi
key ctrl+a; key Z
wait_deck 'build is zoomed in target' 5
key ctrl+a; key j
wait_deck 'fake is zoomed in target' 5; deckcap 19-zoom-moves
check "the zoom moves with j to the next pane" "$SCRATCH/out/19-zoom-moves.txt" 'LIVE .*target › fake.*· zoomed'
key ctrl+a; key Z
wait_deck 'fake is back in its tile' 5
key ctrl+a; key d
wait_deck 'detached from fake' 5
b close-window -s host client >/dev/null
for ((i=0; i<50; i++)); do a call session-info '{"session":"target"}' | grep -q '"tui_attached": *false' && break; sleep .2; done

echo "== a pane that closes under the deck ends the session"
key g; key j --repeat 2; settle
key Enter
wait_deck 'LIVE .*target › build' 5
a close-window -s target build >/dev/null
wait_deck 'live ended' 5; deckcap 14-ended
check "the session ends when the pane closes" "$SCRATCH/out/14-ended.txt" 'live ended: closed'
check_absent "and the live bar is gone" "$SCRATCH/out/14-ended.txt" 'LIVE'

key q; key y
echo "$fails failure(s); evidence in $SCRATCH/out"
exit "$fails"
