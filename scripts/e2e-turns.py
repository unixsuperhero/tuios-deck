#!/usr/bin/env python3
"""Quick end-to-end check of the Turns tab for a plain shell pane.

Daemon A holds one real interactive shell (tuios' own OSC 133 integration).
The test waits for its first prompt, runs one command, waits for the daemon's
command-finished, reads command-history, then runs deck tui from daemon B
pointed at A, presses 4 and checks the row and its expansion. Whole run is
limited to 60 s; every subprocess to 10 s. Output lands in $SCRATCH/out.
"""
import json, os, subprocess, sys, tempfile, time

HERE = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DECK = os.environ.get("DECK", os.path.join(HERE, ".dev", "deck"))
DEADLINE = time.monotonic() + 50  # Reserve ten seconds for both daemon cleanups.
SCRATCH = os.environ.get("SCRATCH") or tempfile.mkdtemp(prefix="dt-", dir="/tmp")
OUT = os.path.join(SCRATCH, "out")
CMD = "echo deck-turns-proof"
fails = 0


def env_for(name):
    d = os.path.join(SCRATCH, name)
    for s in ("run", "config/tuios", "state", "cache", "data", "tmp"):
        os.makedirs(os.path.join(d, s), exist_ok=True)
    os.chmod(os.path.join(d, "run"), 0o700)
    e = {k: v for k, v in os.environ.items() if not k.startswith("TUIOS_")}
    e.update(XDG_RUNTIME_DIR=d + "/run", XDG_CONFIG_HOME=d + "/config",
             XDG_CONFIG_DIRS=d + "/config", XDG_STATE_HOME=d + "/state",
             XDG_CACHE_HOME=d + "/cache", XDG_DATA_HOME=d + "/data", TMPDIR=d + "/tmp")
    return e


ENVS = {}


def run(side, *args, timeout=10, tag=None):
    left = DEADLINE - time.monotonic()
    if left <= 0:
        raise TimeoutError("60 s total deadline")
    try:
        p = subprocess.run(["tuios", *args], env=ENVS[side], capture_output=True, text=True,
                           timeout=min(timeout, left), start_new_session=True)
    except subprocess.TimeoutExpired as e:
        raise TimeoutError("timeout: tuios " + " ".join(args)) from e
    if tag:
        with open(os.path.join(OUT, tag), "w") as f:
            f.write(p.stdout + p.stderr)
    return p


def stage(name, ok, detail=""):
    global fails
    print(("PASS " if ok else "FAIL ") + name + ("" if ok else ": " + detail), flush=True)
    if not ok:
        fails += 1
        raise RuntimeError(name)


def settle():
    run("B", "wait-for", "window-idle", "-s", "host", "-w", "deck", "--idle", "700", "--timeout", "8000")


def deck_screen(tag):
    return run("B", "capture-pane", "-s", "host", "-w", "deck", tag=tag).stdout


def wait_deck(pattern, tag):
    while time.monotonic() < DEADLINE:  # the alternate screen has no daemon condition
        if pattern in deck_screen(tag):
            return True
        time.sleep(0.5)
    return False


def main():
    os.makedirs(OUT, exist_ok=True)
    print("scratch:", SCRATCH, flush=True)
    stage("deck binary exists", os.access(DECK, os.X_OK), DECK + " (build it first)")
    ENVS["A"], ENVS["B"] = env_for("A"), env_for("B")
    run("A", "new", "target", "--detach", "--cwd", SCRATCH)
    p = run("A", "wait-for", "window-output", "-s", "target", "--pattern", r"[$#%>]\s*$", "--timeout", "8000", tag="01-first-prompt.txt")
    stage("shell printed its first prompt", p.returncode == 0, p.stderr)
    run("A", "wait-for", "window-idle", "-s", "target", "--idle", "500", "--timeout", "8000")
    w = json.loads(run("A", "list-windows", "-s", "target", "--json", tag="02-windows.json").stdout)
    window = w["windows"][0]
    stage("shell is at_prompt", window.get("at_prompt") is True, "see out/02-windows.json")
    win = window["window_id"]
    run("A", "send-text", "-s", "target", "-w", win, CMD + "\n")
    p = run("A", "wait-for", "command-finished", "-s", "target", "-w", win, "--command-seq", "0", "--timeout", "8000", tag="03-finished.txt")
    stage("daemon saw the command finish", p.returncode == 0, p.stderr + p.stdout)
    p = run("A", "call", "command-history", json.dumps({"session": "target", "window": win}), tag="04-history.json")
    cmds = json.loads(p.stdout or "{}").get("commands", [])
    hit = [c for c in cmds if c.get("cmdline") == CMD and c.get("exit") == 0]
    stage("command-history has the exact cmdline with exit 0", bool(hit), "see out/04-history.json")
    run("B", "new", "boot", "--detach", "--cwd", SCRATCH)
    run("B", "call", "new-session", json.dumps({"name": "host", "width": 160, "height": 50, "cwd": SCRATCH}))
    data = os.path.join(SCRATCH, "deck.json")
    with open(data, "w") as f:
        f.write('{"version": 1, "projects": [], "tasks": [], "assignments": {}}\n')
    sock = ENVS["A"]["XDG_RUNTIME_DIR"] + "/tuios/tuios.sock"
    run("B", "new-window", "-s", "host", "deck", "--no-focus", "--", "env", "TUIOS_DECK_DATA=" + data,
        DECK, "tui", "--socket", sock, "--captures", os.path.join(SCRATCH, "captures"))
    stage("deck tui drew", wait_deck("tuios-deck", "05-deck-start.txt"), "see out/05-deck-start.txt")
    run("B", "send-keys", "-s", "host", "-w", "deck", "g"); settle()
    run("B", "send-keys", "-s", "host", "-w", "deck", "4"); settle()
    stage("Turns tab shows the command row", wait_deck("echo deck-turns-proof", "06-turns.txt")
          and any("▸" in line and "$ exit 0" in line and CMD in line for line in deck_screen("06-turns.txt").splitlines()), "see out/06-turns.txt")
    run("B", "send-keys", "-s", "host", "-w", "deck", "l"); settle()
    run("B", "send-keys", "-s", "host", "-w", "deck", "enter"); settle()
    s = deck_screen("07-expanded.txt")
    stage("expansion shows cwd and exit detail", any("▾" in line and CMD in line for line in s.splitlines()) and "exit 0 after" in s and "cwd" in s and SCRATCH in s, "see out/07-expanded.txt")
    run("B", "send-keys", "-s", "host", "-w", "deck", "enter"); settle()
    stage("enter collapses the row", any("▸" in line and CMD in line for line in deck_screen("08-collapsed.txt").splitlines()), "see out/08-collapsed.txt")


def cleanup():
    for side in ("A", "B"):
        try:
            subprocess.run(["tuios", "kill-server"], env=ENVS[side], capture_output=True, timeout=5, start_new_session=True)
        except Exception:
            pass


try:
    main()
except (RuntimeError, TimeoutError, KeyError, ValueError) as e:
    if not isinstance(e, RuntimeError):
        print("FAIL aborted:", repr(e)); fails += 1
finally:
    cleanup()
    print("artifacts:", OUT)
    print("failures:", fails)
sys.exit(fails)
