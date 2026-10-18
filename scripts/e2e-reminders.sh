#!/usr/bin/env bash
# End-to-end check of recurring reminders against a throwaway tuios daemon.
#
# Daemon A is the one reminders are delivered to (as a session notice). Daemon
# B holds a pane that runs `deck tui`, which is driven through its palette.
# `deck web` is then run on a second store and driven with curl: delivery,
# recurrence, disable, persistence across a restart, a reminder missed while
# the deck was off, and an OS notification through stand-in osascript and
# notify-send that record their arguments (so quoting is seen, not trusted).
# Prints PASS or FAIL per check; the exit status is the number of failures.
set -uo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
DECK=${DECK:-$here/.dev/deck}
SCRATCH=${SCRATCH:-/tmp/deck-e2erem-$(id -u)}
PORT=${PORT:-18791}
rm -rf "$SCRATCH"; mkdir -p "$SCRATCH/out" "$SCRATCH/bin"
# A store as JSON: the seed it names is migrated into the .db beside it, read back through deck export.
store_json() { "$DECK" export -data "$1" > "$SCRATCH/out/store.json" && echo "$SCRATCH/out/store.json"; }
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
cleanup() { [ -n "${webpid:-}" ] && kill "$webpid" 2>/dev/null; a kill-server >/dev/null 2>&1; b kill-server >/dev/null 2>&1; }
trap cleanup EXIT
check() { # name, file, pattern
	if grep -q -- "$3" "$2"; then echo "PASS $1"; else echo "FAIL $1: no '$3' in $2"; fails=$((fails+1)); fi
}
ok() { # name, command...
	local n=$1; shift
	if "$@"; then echo "PASS $n"; else echo "FAIL $n"; fails=$((fails+1)); fi
}
settle() { b wait-for window-idle -s host -w deck --idle 200 --timeout 10000 >/dev/null; }
deckcap() { b capture-pane -s host -w deck > "$SCRATCH/out/$1.txt"; }
# notices counts the reminder notices in the session's ring.
notices() { a read-agent-messages -s target 2>/dev/null | grep -c "^subject: reminder: $1\$"; }
# next_notice waits for mail in the session; mail already read does not count.
next_notice() { a wait-for agent-message -s target --timeout "${1:-10000}" >/dev/null; }

# Stand-ins that record their arguments, one per line, then a marker line.
for t in osascript notify-send; do
	printf '#!/bin/sh\nfor x in "$@"; do printf "%%s\\n" "$x"; done >> "%s/os-args.txt"\necho "--call--" >> "%s/os-args.txt"\n' "$SCRATCH" "$SCRATCH" > "$SCRATCH/bin/$t"
done
chmod +x "$SCRATCH"/bin/*
ASOCK="$A/run/tuios/tuios.sock"

echo "== daemons: A receives the reminders, B runs deck tui"
a new target --detach --cwd "$SCRATCH" >/dev/null
b new host --detach --cwd "$SCRATCH" >/dev/null
TUIDATA="$SCRATCH/tui.json"
b new-window -s host deck --no-focus -- env TUIOS_DECK_DATA="$TUIDATA" PATH="$SCRATCH/bin:$PATH" "$DECK" tui --socket "$ASOCK" >/dev/null
b wait-for window-idle -s host -w deck --idle 1500 --timeout 15000 >/dev/null

echo "== deck tui: palette → new reminder…"
b send-text -s host -w deck ':'; settle
b send-text -s host -w deck 'new reminder'; settle
b send-keys -s host -w deck Enter; settle; deckcap 01-form
check "new reminder form opens" "$SCRATCH/out/01-form.txt" 'new reminder'
b send-text -s host -w deck 'stretch'; b send-keys -s host -w deck Enter
b send-text -s host -w deck 'stand up and stretch'; b send-keys -s host -w deck Enter
b send-text -s host -w deck 'abc'; settle; deckcap 02-bad
b send-keys -s host -w deck Enter; settle; deckcap 02-bad
check "a bad interval is refused in the form" "$SCRATCH/out/02-bad.txt" 'not a duration'
b send-keys -s host -w deck ctrl+u; b send-text -s host -w deck '2s'; b send-keys -s host -w deck Enter; settle; deckcap 03-added
check "status confirms the new reminder" "$SCRATCH/out/03-added.txt" 'reminder stretch every 2s'
check "reminder is in the store" "$(store_json "$TUIDATA")" '"title": "stretch"'
echo "== deck tui delivers it: a notice in daemon A and an OS notification"
next_notice 10000; ok "TUI reminder arrives as a notice in daemon A" test "$(notices stretch)" -ge 1
next_notice 10000; ok "it recurs" test "$(notices stretch)" -ge 2
check "notice carries the message" <(a read-agent-messages -s target 2>/dev/null) 'stand up and stretch'
b send-text -s host -w deck ':'; settle; b send-text -s host -w deck 'reminders'; settle; b send-keys -s host -w deck Enter; settle; deckcap 04-list
check "reminders command lists it" "$SCRATCH/out/04-list.txt" 'stretch · every 2s · next'
check "OS notifier got title and text as separate arguments" "$SCRATCH/os-args.txt" '^stand up and stretch$'
echo "== deck tui: disable reminder…"
b send-text -s host -w deck ':'; settle; b send-text -s host -w deck 'disable reminder'; settle; b send-keys -s host -w deck Enter; settle
b send-keys -s host -w deck Tab; settle
b send-keys -s host -w deck Enter; settle; deckcap 05-disable
check "status confirms disabled" "$SCRATCH/out/05-disable.txt" 'disabled reminder stretch'
check "store says disabled" <(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["reminders"][0]["enabled"])' "$(store_json "$TUIDATA")") 'False'
before=$(notices stretch)
a wait-for agent-message -s target --timeout 5000 >/dev/null 2>&1 && echo "FAIL a disabled reminder still fired" && fails=$((fails+1)) || echo "PASS disabled reminder stays quiet for 5s"
ok "no new notice while disabled" test "$(notices stretch)" -eq "$before"
b kill-server >/dev/null 2>&1

echo "== deck web: API, delivery, recurrence, disable, persistence"
if lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1; then echo "FAIL port $PORT is in use"; exit 1; fi
WEBDATA="$SCRATCH/web.json"; base="http://127.0.0.1:$PORT"; J='Content-Type: application/json'
startweb() {
	(unset TUIOS_ENV TUIOS_SESSION TUIOS_PANE_ID TUIOS_PANE_TOKEN TUIOS_SOCKET; export TUIOS_DECK_DATA="$WEBDATA" PATH="$SCRATCH/bin:$PATH"; exec "$DECK" web --listen "127.0.0.1:$PORT" --socket "$ASOCK" --captures "$SCRATCH/captures" > "$SCRATCH/out/web.log" 2>&1) &
	webpid=$!
	curl -s --retry 20 --retry-connrefused --retry-delay 1 "$base/api/reminders" > /dev/null
}
startweb
: > "$SCRATCH/os-args.txt"
curl -s -X POST -H "$J" "$base/api/reminders" -d '{"title":"web ping","message":"a \"q\" $(touch PWNED) ; `touch PWNED`","every":"2s"}' > "$SCRATCH/out/w1.json"
check "create returns the reminder" "$SCRATCH/out/w1.json" '"every":"2s"'
id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["reminder"]["id"])' "$SCRATCH/out/w1.json")
curl -s -X POST -H "$J" "$base/api/reminders" -d '{"title":"x","every":"zzz"}' > "$SCRATCH/out/w2.json"
check "a bad interval is a 400 with a message" "$SCRATCH/out/w2.json" 'not a duration'
curl -s -X POST -H "$J" "$base/api/reminders" -d '{"title":"","every":"1m"}' > "$SCRATCH/out/w2b.json"
check "an empty title is refused" "$SCRATCH/out/w2b.json" 'title is empty'
next_notice 10000; ok "web reminder is delivered" test "$(notices 'web ping')" -ge 1
next_notice 10000; ok "web reminder recurs" test "$(notices 'web ping')" -ge 2
ok "text reached the OS notifier verbatim and no shell ran" bash -c "grep -qxF 'a \"q\" \$(touch PWNED) ; \`touch PWNED\`' '$SCRATCH/os-args.txt' && [ ! -e PWNED ] && [ ! -e '$SCRATCH/PWNED' ]"
curl -s -X PATCH -H "$J" "$base/api/reminders/$id" -d '{"enabled":false}' > "$SCRATCH/out/w3.json"
check "disable via PATCH" "$SCRATCH/out/w3.json" '"enabled":false'
before=$(notices 'web ping')
a wait-for agent-message -s target --timeout 5000 >/dev/null 2>&1 && { echo "FAIL disabled web reminder still fired"; fails=$((fails+1)); } || echo "PASS disabled web reminder stays quiet for 5s"
ok "no new web notice while disabled" test "$(notices 'web ping')" -eq "$before"
curl -s -X POST -H "$J" "$base/api/reminders" -d '{"title":"hourly","every":"1h"}' > "$SCRATCH/out/w4.json"
hid=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["reminder"]["id"])' "$SCRATCH/out/w4.json")
nextA=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["reminder"]["next"])' "$SCRATCH/out/w4.json")
kill "$webpid"; wait "$webpid" 2>/dev/null
echo "== restart: schedule and state persist"
startweb
curl -s "$base/api/reminders" > "$SCRATCH/out/w5.json"
ok "hourly keeps its next due across a restart" python3 -c 'import json,sys; r={x["id"]:x for x in json.load(open(sys.argv[1]))["reminders"]}; sys.exit(0 if r[sys.argv[2]]["next"]==int(sys.argv[3]) else 1)' "$SCRATCH/out/w5.json" "$hid" "$nextA"
ok "the disabled one is still disabled" python3 -c 'import json,sys; r={x["id"]:x for x in json.load(open(sys.argv[1]))["reminders"]}; sys.exit(0 if not r[sys.argv[2]]["enabled"] else 1)' "$SCRATCH/out/w5.json" "$id"
curl -s -X PATCH -H "$J" "$base/api/reminders/$id" -d '{"enabled":true}' > /dev/null
next_notice 10000; ok "re-enabled reminder fires again after the restart" test "$(notices 'web ping')" -gt "$before"
echo "== a reminder missed while the deck was off fires once"
kill "$webpid"; wait "$webpid" 2>/dev/null
python3 - "${WEBDATA%.json}.db" "$id" <<'PY'
import sqlite3, sys, time
p, rid = sys.argv[1:]
with sqlite3.connect(p) as db:  # the deck is off; its migrated database
    db.execute("UPDATE reminders SET next = ? WHERE id = ?", (int(time.time()) - 7, rid))  # three and a half intervals ago
PY
mark=$(notices 'web ping')
startweb
# Counted as soon as it lands, not with next_notice: the catch-up can arrive
# before a wait started after startweb, which then sees only the next one.
for ((i=0; i<50; i++)); do [ "$(notices 'web ping')" -gt "$mark" ] && break; sleep .2; done
ok "exactly one catch-up notice, not one per missed interval" test "$(( $(notices 'web ping') - mark ))" -eq 1
curl -s "$base/api/reminders" > "$SCRATCH/out/w6.json"
ok "next due moved into the future" python3 -c 'import json,sys,time; r={x["id"]:x for x in json.load(open(sys.argv[1]))["reminders"]}; sys.exit(0 if r[sys.argv[2]]["next"]>time.time()-1 else 1)' "$SCRATCH/out/w6.json" "$id"
curl -s -X DELETE "$base/api/reminders/$id" > "$SCRATCH/out/w7.json"
check "delete via API" "$SCRATCH/out/w7.json" 'deleted'
curl -s "$base/api/reminders" > "$SCRATCH/out/w8.json"
if grep -q "web ping" "$SCRATCH/out/w8.json"; then echo "FAIL deleted reminder still listed"; fails=$((fails+1)); else echo "PASS deleted reminder is gone"; fi
echo "== an interval with days and hours round-trips through edit"
curl -s -X POST -H "$J" "$base/api/reminders" -d '{"title":"mixed","every":"1d2h3m4s"}' > "$SCRATCH/out/w9.json"
check "created with a mixed interval" "$SCRATCH/out/w9.json" '"every":"1d2h3m4s"'
mid=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["reminder"]["id"])' "$SCRATCH/out/w9.json")
curl -s -X PATCH -H "$J" "$base/api/reminders/$mid" -d '{"title":"mixed2","every":"1d2h3m4s"}' > "$SCRATCH/out/w10.json"
check "editing with the displayed interval unchanged succeeds" "$SCRATCH/out/w10.json" '"every":"1d2h3m4s"'
curl -s -X PATCH -H "$J" "$base/api/reminders/$mid" -d '{"every":"1d12h"}' > "$SCRATCH/out/w11.json"
check "days with hours are accepted as typed" "$SCRATCH/out/w11.json" '"interval":129600'
curl -s -X DELETE "$base/api/reminders/$mid" > /dev/null
kill "$webpid"; wait "$webpid" 2>/dev/null; webpid=
echo "== a daemon that never answers does not stop the OS notification"
# A unix socket that accepts and stays silent stands in for a hung daemon; no
# real session is involved. The notice call runs out its own 10s deadline and
# the OS notifier must still be called after it.
HUNG="$SCRATCH/hung.sock"
python3 - "$HUNG" <<'PY' &
import socket, sys, os, time
s = socket.socket(socket.AF_UNIX); s.bind(sys.argv[1]); s.listen(8)
conns = []
while True:
    c, _ = s.accept(); conns.append(c)
PY
hungpid=$!
for i in $(seq 20); do [ -S "$HUNG" ] && break; sleep 0.2; done
: > "$SCRATCH/os-args.txt"; HUNGDATA="$SCRATCH/hung.json"
cat > "$HUNGDATA" <<JSON
{"version":1,"projects":[],"tasks":[],"assignments":{},"context_items":[],"reminders":[{"id":"r_hung01","title":"hung ping","message":"daemon is silent","interval":3600,"enabled":true,"next":$(( $(date +%s) - 1 )),"last":0,"created":1760000000}]}
JSON
(unset TUIOS_ENV TUIOS_SESSION TUIOS_PANE_ID TUIOS_PANE_TOKEN TUIOS_SOCKET; export TUIOS_DECK_DATA="$HUNGDATA" PATH="$SCRATCH/bin:$PATH"; exec "$DECK" web --listen "127.0.0.1:$((PORT+1))" --socket "$HUNG" --auto-capture=false --captures "$SCRATCH/captures" > "$SCRATCH/out/hung.log" 2>&1) &
webpid=$!
got=no
for i in $(seq 125); do grep -qxF 'daemon is silent' "$SCRATCH/os-args.txt" && { got=yes; break; }; sleep .2; done
ok "OS notifier ran although the daemon call timed out" test "$got" = yes
for i in $(seq 25); do grep -q 'notice:' "$SCRATCH/out/hung.log" && break; sleep .2; done
check "the failed notice was logged" "$SCRATCH/out/hung.log" 'hung ping: notice:'
kill "$webpid" "$hungpid" 2>/dev/null; wait 2>/dev/null; webpid=
exit "$fails"
