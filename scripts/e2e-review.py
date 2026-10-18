#!/usr/bin/env python3
"""Exercise finished-turn review in a real TUI and HTTP server, using isolated daemons."""
import json
import os
from pathlib import Path
import shlex
import subprocess
import tempfile
import time
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
DECK = os.environ.get("DECK", str(ROOT / ".dev/deck"))
SCRATCH = Path(os.environ.get("SCRATCH") or tempfile.mkdtemp(prefix="dr-", dir="/tmp"))
OUT = SCRATCH / "out"
OUT.mkdir(parents=True, exist_ok=True)
PORT = int(os.environ.get("PORT", "18963"))
BASE = f"http://127.0.0.1:{PORT}"
DATA = SCRATCH / "deck.json"
CAPTURES = SCRATCH / "captures"
ENVS = {}
web = None


def environment(side):
    d = SCRATCH / side
    for folder in ("run", "config/tuios", "state", "cache", "data", "tmp"):
        (d / folder).mkdir(parents=True, exist_ok=True)
    (d / "run").chmod(0o700)
    env = {k: v for k, v in os.environ.items() if not k.startswith("TUIOS_")}
    env.update(XDG_RUNTIME_DIR=str(d / "run"), XDG_CONFIG_HOME=str(d / "config"),
               XDG_CONFIG_DIRS=str(d / "config"), XDG_STATE_HOME=str(d / "state"),
               XDG_CACHE_HOME=str(d / "cache"), XDG_DATA_HOME=str(d / "data"), TMPDIR=str(d / "tmp"))
    return env


def run(side, *args):
    p = subprocess.run(["tuios", *args], env=ENVS[side], capture_output=True, text=True,
                       timeout=20, start_new_session=True)
    if p.returncode:
        raise RuntimeError(f"tuios {args}: {p.stdout} {p.stderr}")
    return p.stdout


def call(side, verb, **params):
    return json.loads(run(side, "call", verb, json.dumps(params)))


def event(window, kind, text, state):
    call("A", "set-agent-state", session="target", window=window, harness="omp", state=state,
         activity={"event": kind, "text": text})


def check(name, condition):
    if not condition:
        raise AssertionError(name)
    print("PASS " + name, flush=True)


def stored():
    """The reviews the store file yields: history's prompt/turn-end pairs per window, plus review_marks.
    Nothing else about a review is stored."""
    data = json.loads(DATA.read_text())
    marks = data.get("review_marks", {})
    turns = sorted((e for e in data.get("history", []) if e["source"] == "agent-activity"
                    and e["kind"] in ("prompt-submitted", "response-completed", "session_start")), key=lambda e: e["at"])
    pending, out = {}, []
    for e in turns:
        if e["kind"] == "session_start":
            pending.pop(e["window"], None)
        elif e["kind"] == "prompt-submitted":
            pending[e["window"]] = e
        elif e["window"] in pending:
            prompt = pending.pop(e["window"])
            out.append({"id": e["id"], "prompt": prompt["detail"], "response": e["detail"].split("\n\n", 1)[1],
                        "completed_at": e["at"], "reviewed_at": marks.get(e["id"], 0)})
    return sorted(out, key=lambda r: (r["completed_at"], r["id"]))


def stable(review):
    """A review without the fields filled from the live listing, which a closed pane no longer has."""
    return {k: v for k, v in review.items() if k not in ("pane", "harness")}


def screen(tag, contains, without=None):
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        text = run("B", "capture-pane", "-s", "host", "-w", "deck")
        (OUT / (tag + ".txt")).write_text(text)
        if contains in text and (without is None or without not in text):
            return text
        time.sleep(.2)  # alternate screens are not visible to wait-for window-output
    raise AssertionError(f"{tag}: screen never contained {contains!r}" + (f" without {without!r}" if without else ""))


def key(keys):
    run("B", "send-keys", "-s", "host", "-w", "deck", keys)
    run("B", "wait-for", "window-idle", "-s", "host", "-w", "deck", "--idle", "300", "--timeout", "8000")


def wait_file(pattern):
    """Auto-capture writes a turn capture on its own after a turn end; wait for the file, not a fixed time."""
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        if list(CAPTURES.glob(pattern)):
            return
        time.sleep(.2)
    raise AssertionError(f"no capture matched {pattern} under {CAPTURES}")


def api(path, method="GET"):
    with urllib.request.urlopen(urllib.request.Request(BASE + path, method=method), timeout=15) as r:
        return json.load(r)


def start_web():
    global web
    import socket
    with socket.socket() as sock:
        check("web port is unused", sock.connect_ex(("127.0.0.1", PORT)) != 0)
    log = open(OUT / "web.log", "a")
    web = subprocess.Popen([DECK, "web", "--listen", f"127.0.0.1:{PORT}", "--socket", SOCK,
                            "--data", str(DATA), "--captures", str(CAPTURES), "--auto-capture=false", "--reminders=false"],
                           env=ENVS["A"], stdout=log, stderr=log, start_new_session=True)
    log.close()
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        try:
            return api("/api/reviews?refresh=1")
        except OSError:
            if web.poll() is not None:
                raise RuntimeError("web exited; see web.log")
            time.sleep(.2)
    raise RuntimeError("web did not become ready")


try:
    ENVS = {side: environment(side) for side in ("A", "B")}
    SOCK = ENVS["A"]["XDG_RUNTIME_DIR"] + "/tuios/tuios.sock"
    DATA.write_text(json.dumps({"version": 1, "projects": [], "tasks": [], "assignments": {},
                               "queued_prompts": [{"id": "never-started", "text": "queued-only"}]}))
    run("A", "new", "target", "--detach", "--cwd", str(SCRATCH))
    for name in ("slow", "fast", "third", "running"):
        output = "\n".join(f"response line {i}" for i in range(90)) if name == "slow" else name + " output"
        run("A", "new-window", "-s", "target", name, "--no-focus", "--", "sh", "-c", "printf '%s\\n' " + shlex.quote(output) + "; exec sleep 600")
        run("A", "wait-for", "window-output", "-s", "target", "-w", name, "--pattern", "response line 89" if name == "slow" else name + " output", "--timeout", "10000")
    run("B", "new", "boot", "--detach", "--cwd", str(SCRATCH))
    call("B", "new-session", name="host", width=160, height=40, cwd=str(SCRATCH))
    # An interactive shell remains after deck exits, allowing a deterministic exit check.
    run("B", "new-window", "-s", "host", "deck", "--no-focus")
    # Auto-capture stays on: the review's pane output is the turn capture it writes when a turn ends.
    cmd = shlex.join([DECK, "tui", "--socket", SOCK, "--data", str(DATA), "--captures", str(CAPTURES), "--reminders=false"])
    run("B", "send-text", "-s", "host", "-w", "deck", cmd + "; echo REVIEW-TUI-EXITED\n")
    # The typed command line also holds "tuios-deck"; the deck has started once its alternate screen replaces it.
    screen("01-deck", "tuios-deck", without="REVIEW-TUI-EXITED")
    # The turns end while the deck runs, so history observes them live and auto-capture sees each turn end.
    event("slow", "prompt", "started-first-finished-second", "working")
    event("fast", "prompt", "started-second-finished-first", "working")
    event("fast", "turn_end", "fast-response", "done")
    event("slow", "turn_end", "slow-response", "done")
    event("third", "prompt", "third-completed-prompt", "working")
    event("third", "turn_end", "third-response", "done")
    event("running", "prompt", "still-running-prompt", "working")
    key("V")
    screen("02-unopened", "Review queue (3)")
    rows = stored()
    text = screen("03-order", "Enter opens a prompt")
    check("TUI orders by completion, not start", text.index("started-second") < text.index("started-first") < text.index("third-completed"))
    check("entering review does not mark anything", all(r["reviewed_at"] == 0 for r in rows))
    check("queued and running prompts excluded", {r["prompt"] for r in rows} == {
        "started-first-finished-second", "started-second-finished-first", "third-completed-prompt"})
    key("j")
    screen("04-highlight", "Review queue (3)")
    check("highlight alone does not mark anything", all(r["reviewed_at"] == 0 for r in stored()))
    wait_file("target-slow-*-turn.txt")
    key("Enter")
    screen("05-opened", "slow-response")
    reviewed = [r for r in stored() if r["reviewed_at"]]
    check("Enter marks exactly the selected prompt", [r["prompt"] for r in reviewed] == ["started-first-finished-second"])
    stamp = reviewed[0]["reviewed_at"]
    key("J")
    before = screen("06-scrolled", "response line")
    key("r")
    after = screen("07-refresh", "response line")
    check("refresh preserves opened response scroll", before.splitlines()[:35] == after.splitlines()[:35])
    event("running", "turn_end", "finished-while-reviewing", "done")
    live = screen("07-background", "Review queue (3)")
    check("background completion preserves opened detail scroll",
          [line.split("│", 1)[-1] for line in before.splitlines()[1:35]] ==
          [line.split("│", 1)[-1] for line in live.splitlines()[1:35]])
    check("background completion is persisted without opening it",
          [(r["response"], r["reviewed_at"]) for r in stored() if r["prompt"] == "still-running-prompt"] == [("finished-while-reviewing", 0)])
    key("Tab")
    screen("08-history", "Reviewed history (1)")
    key("Enter")
    check("reopening history does not reset reviewed timestamp", [r["reviewed_at"] for r in stored() if r["reviewed_at"]] == [stamp])
    key("q")
    key("y")  # q asks before quitting
    run("B", "wait-for", "window-output", "-s", "host", "-w", "deck", "--pattern", "REVIEW-TUI-EXITED", "--timeout", "10000")
    result = start_web()
    (OUT / "09-web-reviews.json").write_text(json.dumps(result, indent=2))
    rows = result["reviews"]
    check("API orders completion oldest first", [r["prompt"] for r in rows] == [
        "started-second-finished-first", "started-first-finished-second", "third-completed-prompt", "still-running-prompt"])
    check("review survives frontend restart", [r["reviewed_at"] for r in rows if r["reviewed_at"]] == [stamp])
    check("GET does not mark top item reviewed", rows[0]["reviewed_at"] == 0)
    result = api("/api/reviews/" + urllib.parse.quote(rows[1]["id"], safe="") + "/open", "POST")
    check("explicit API reopen returns prompt and response", result["review"]["prompt"] == rows[1]["prompt"] and result["review"]["response"].startswith("slow-response"))
    call("A", "close-window", session="target", window="slow")
    check("reviewed history survives its pane closing", stable(api("/api/reviews?refresh=1")["reviews"][1]) == stable(rows[1]))
    print("artifacts:", OUT, flush=True)
    if os.environ.get("KEEP") == "1":
        (SCRATCH / "live.json").write_text(json.dumps({"web_pid": web.pid, "base": BASE, "envs": ENVS}, indent=2))
        print("left live for browser verification:", BASE, flush=True)
except Exception:
    os.environ.pop("KEEP", None)
    raise
finally:
    if os.environ.get("KEEP") != "1":
        if web:
            web.terminate()
            web.wait(timeout=10)
        for side in ENVS:
            subprocess.run(["tuios", "kill-server"], env=ENVS[side], capture_output=True, timeout=10, start_new_session=True)
