#!/usr/bin/env python3
"""Exercise help against isolated tuios daemons; captures remain in SCRATCH."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time

HERE = Path(__file__).resolve().parent.parent
DECK = os.environ.get("DECK", str(HERE / ".dev/deck"))
SCRATCH = Path(os.environ.get("SCRATCH") or tempfile.mkdtemp(prefix="dh-", dir="/tmp"))
OUT = SCRATCH / "out"
OUT.mkdir(parents=True, exist_ok=True)


def env_for(side):
    root = SCRATCH / side
    for name in ("run", "config/tuios", "state", "cache", "data", "tmp"):
        (root / name).mkdir(parents=True, exist_ok=True)
    (root / "run").chmod(0o700)
    env = {k: v for k, v in os.environ.items() if not k.startswith("TUIOS_")}
    for key, folder in {"XDG_RUNTIME_DIR": "run", "XDG_CONFIG_HOME": "config",
                        "XDG_CONFIG_DIRS": "config", "XDG_STATE_HOME": "state",
                        "XDG_CACHE_HOME": "cache", "XDG_DATA_HOME": "data", "TMPDIR": "tmp"}.items():
        env[key] = str(root / folder)
    return env


ENVS = {side: env_for(side) for side in ("A", "B")}


def run(side, *args):
    return subprocess.run(["tuios", *args], env=ENVS[side], text=True,
                          capture_output=True, check=True, timeout=20).stdout


def keys(value, session="host"):
    run("B", "send-keys", "-s", session, "-w", "deck", value)


def screen(tag, pattern, session="host"):
    # tuios wait-for does not read the deck's alternate screen.
    deadline = time.monotonic() + 15
    while True:
        text = run("B", "capture-pane", "-s", session, "-w", "deck")
        (OUT / (tag + ".txt")).write_text(text)
        if pattern in text:
            return text
        if time.monotonic() > deadline:
            raise AssertionError(f"{tag}: missing {pattern!r}; see {OUT}")
        time.sleep(.2)


def launch(session, width, height, config):
    run("B", "call", "new-session", json.dumps({"name": session, "width": width, "height": height, "cwd": str(SCRATCH)}))
    run("B", "new-window", "-s", session, "deck", "--no-focus", "--", "env",
        "XDG_CONFIG_HOME=" + str(config), "TUIOS_DECK_DATA=" + str(SCRATCH / "deck.json"),
        DECK, "tui", "--socket", str(SCRATCH / "A/run/tuios/tuios.sock"), "--auto-capture=false")
    screen(session + "-start", "tuios-deck", session)


try:
    print("scratch:", SCRATCH, flush=True)
    run("A", "new", "target", "--detach", "--cwd", str(SCRATCH))
    run("A", "new-window", "-s", "target", "second", "--no-focus")
    run("A", "send-text", "-s", "target", "-w", "second", "printf 'help-selection-proof\\n'\n")
    run("A", "wait-for", "window-output", "-s", "target", "-w", "second", "--pattern", "help-selection-proof", "--timeout", "10000")
    run("B", "new", "boot", "--detach", "--cwd", str(SCRATCH))
    (SCRATCH / "deck.json").write_text(json.dumps({"version": 1, "projects": [{"id": "p_help", "name": "help scope", "theme": {"color": "blue", "glyph": "H"}, "created": 1760000000}], "tasks": [], "assignments": {}}))
    config = SCRATCH / "settings"
    (config / "tuios-deck").mkdir(parents=True)
    (config / "tuios-deck/config").write_text(json.dumps({"vim_modes": True, "keys": {"deck": {"quit": "Z", "new_agent": "v"}, "form_normal": {"next_field": "n"}}}))
    launch("host", 110, 30, config)
    keys("g j")
    screen("selected", "help-selection-proof")
    keys("?")
    text = screen("help", "Keybindings and actions")
    assert "Z" in next(line for line in text.splitlines() if "quit:" in line)
    assert "V" == next(line for line in text.splitlines() if "review:" in line).strip("│ ").split()[0]
    print("PASS help shows configured quit key", flush=True)
    keys("u")
    screen("empty-unbound", "No actions without bindings")
    keys("u")
    keys("Tab")
    text = screen("normal", "Mode: form_normal")
    assert "n" == next(line for line in text.splitlines() if "next_field:" in line).strip("│ ").split()[0]
    keys("Tab")
    screen("insert", "Mode: form_insert")
    keys("Tab")
    text = screen("pages-easy", "toggle saved easy-task mark")
    assert "E" == next(line for line in text.splitlines() if "toggle saved easy-task mark" in line).strip("│ ").split()[0]
    keys("End")
    text = screen("pages-review", "refresh completed prompts")
    assert "open selected prompt and mark reviewed" in text and "switch awaiting review" in text and "scroll opened prompt" in text
    keys("Tab")
    screen("forms", "Mode: forms")
    keys("Tab")
    text = screen("modals", "continue focus for a fresh")
    assert "F/esc" in text and "fixed keys" in text and "q/ctrl+c" in text
    print("PASS help covers Review, easy marking and fixed focus buffer controls", flush=True)
    keys("Tab")
    text = screen("review-action", "go to review queue")
    assert "V" == next(line for line in text.splitlines() if "go to review queue" in line).strip("│ ").split()[0]
    keys("u")
    text = screen("unbound", "settings…")
    assert "(unbound)" in text and "new agent" not in text and "go to review queue" not in text
    print("PASS catalog includes Review with its binding, not as unbound", flush=True)
    keys("End")
    screen("unbound-end", "daemon command via palette")
    keys("Escape")
    text = screen("restored", "help-selection-proof")
    assert "Keybindings and actions" not in text
    print("PASS mode navigation, unbound commands, daemon actions and selection restore", flush=True)
    keys("p")
    run("B", "send-text", "-s", "host", "-w", "deck", "why?")
    text = screen("question-input", "why?")
    assert "Keybindings and actions" not in text
    keys("Escape Escape")
    keys(":")
    run("B", "send-text", "-s", "host", "-w", "deck", "?")
    text = screen("question-palette", "> ?▏")
    assert "Keybindings and actions" not in text
    keys("Escape")
    print("PASS question mark stays text in forms and palette", flush=True)
    launch("small", 36, 10, config)
    keys("?", "small")
    before = screen("small-help", "Mode: deck", "small")
    keys("PageDown", "small")
    after = screen("small-scrolled", "esc close", "small")
    assert before != after
    assert len(after.splitlines()) <= 10 and all(len(line) <= 36 for line in after.splitlines())
    keys("Escape", "small")
    screen("small-restored", "tuios-deck", "small")
    print("PASS help scrolls and clips at 36x10", flush=True)
    # A separately configured help key must replace ?, not shadow text fields.
    (config / "tuios-deck/config").write_text('{"keys":{"deck":{"help":"alt+h"}}}')
    launch("remapped", 100, 25, config)
    keys("alt+h", "remapped")
    text = screen("remapped-help", "Keybindings and actions", "remapped")
    assert "alt+h" == next(line for line in text.splitlines() if "help:" in line).strip("│ ").split()[0]
    print("PASS help itself follows configured binding", flush=True)
    keys("Escape T F", "remapped")
    keys("alt+h", "remapped")
    text = screen("focused-help", "unavailable in current focus", "remapped")
    assert "palette:" in text
    print("PASS help stays reachable in focus and labels unavailable actions", flush=True)
finally:
    for side in ENVS:
        subprocess.run(["tuios", "kill-server"], env=ENVS[side], capture_output=True, timeout=20)
