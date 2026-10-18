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


def screen(tag, pattern, session="host", previous=None):
    # tuios wait-for does not read the deck's alternate screen.
    deadline = time.monotonic() + 15
    while True:
        text = run("B", "capture-pane", "-s", session, "-w", "deck")
        (OUT / (tag + ".txt")).write_text(text)
        if pattern in text and text != previous:
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
    launch("host", 110, 40, config)
    keys("g j")
    screen("selected", "help-selection-proof")
    keys("?")
    text = screen("help", "Keybindings and actions")
    occupied = [(y, line) for y, line in enumerate(text.splitlines()) if line.strip()]
    assert occupied[0][0] >= 5 and occupied[-1][0] < 35
    assert all(len(line) - len(line.lstrip()) >= 5 and len(line.rstrip()) <= 105 for _, line in occupied)
    print("PASS five-row/column help gutters", flush=True)
    assert "Z" in next(line for line in text.splitlines() if "quit:" in line)
    assert "V" == next(line for line in text.splitlines() if "review:" in line).strip("│ ").split()[0]
    print("PASS help shows configured quit key", flush=True)
    assert "── all pages, not dialogs" in text
    keys("PageDown")
    screen("deck-motion", "── deck page, motion", previous=text)
    keys("Home")
    screen("deck-top", "── all pages, not dialogs")
    assert any(" deck │ form_normal │ form_insert " in line for line in text.splitlines())
    print("PASS help groups a tab under scope headers and shows the tab bar", flush=True)
    keys("/")
    run("B", "send-text", "-s", "host", "-w", "deck", "pal")
    text = screen("search-pal", "/pal▏")
    assert "Search: every mode" in text and "── modals" in text and "filter commands" in text and "── deck" in text
    assert "Mode: deck" not in text and "quit:" not in text
    keys("Down")
    text = screen("search-scrolled", "/pal▏", previous=text)
    keys("Enter")
    text = screen("search-kept", "/pal · / edit", previous=text)
    keys("j")
    screen("search-kept-j", "/pal · / edit", previous=text)
    keys("Escape")
    text = screen("search-cleared", "Mode: deck")
    assert "/pal" not in text and "Keybindings and actions" in text
    keys("/")
    run("B", "send-text", "-s", "host", "-w", "deck", "q")
    screen("search-q", "/q▏")
    keys("Backspace Backspace")
    text = screen("search-left", "Mode: deck")
    keys("/")
    run("B", "send-text", "-s", "host", "-w", "deck", "zzznomatch")
    screen("search-none", "Nothing matches")
    keys("Escape")
    screen("search-none-cleared", "Mode: deck")
    print("PASS / searches every mode under mode headers; esc and backspace leave search", flush=True)
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
    assert "── tasks page" in text
    keys("/")
    run("B", "send-text", "-s", "host", "-w", "deck", "review")
    keys("Enter")
    text = screen("pages-review", "/review · / edit")
    assert "refresh completed prompts" in text
    assert "open selected prompt and mark reviewed" in text and "switch awaiting review" in text and "scroll opened prompt" in text
    keys("Escape")
    screen("pages-back", "Mode: pages")
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
    text = screen("unbound", "Mode: actions · unbound only")
    assert "(unbound)" in text and "new agent" not in text and "go to review queue" not in text
    print("PASS catalog includes Review with its binding, not as unbound", flush=True)
    keys("End")
    screen("unbound-end", "Help navigation")
    keys("/")
    run("B", "send-text", "-s", "host", "-w", "deck", "daemon command")
    screen("unbound-daemon", "[daemon command via palette]")
    keys("Escape")
    screen("unbound-search-cleared", "Mode: actions · unbound only")
    keys("Escape")
    text = screen("restored", "help-selection-proof")
    assert "Keybindings and actions" not in text
    print("PASS mode navigation, unbound commands, daemon actions and selection restore", flush=True)
    keys("p i")
    run("B", "send-text", "-s", "host", "-w", "deck", "why?")
    text = screen("question-input", "why?")
    assert "Keybindings and actions" not in text
    keys("Escape ?")
    screen("form-normal-default", "Mode: form_normal")
    keys("Escape i")
    screen("form-restored", "why?")
    keys("Escape Escape y")  # normal mode, then the question an edited form asks
    keys(":")
    run("B", "send-text", "-s", "host", "-w", "deck", "?")
    text = screen("question-palette", "> ?▏")
    assert "Keybindings and actions" not in text
    keys("Escape")
    print("PASS question mark stays text in forms and palette", flush=True)
    for key, mode, action in [("T", "tasks", "cycle task status"), ("I", "context", "cycle sort"),
                              ("H", "history", "go to source pane"), ("V", "review", "mark reviewed"),
                              ("C", "captures", "scroll capture")]:
        keys(key + " ?")
        text = screen(mode + "-default", "Mode: " + mode)
        assert action in text
        if mode != "tasks":
            assert "cycle task status" not in text
        keys("Escape :")
        run("B", "send-text", "-s", "host", "-w", "deck", "keybindings")
        keys("Enter")
        screen(mode + "-palette", "Mode: " + mode)
        keys("Escape Escape")
    print("PASS active-page and palette help scopes", flush=True)
    plain = SCRATCH / "plain"
    (plain / "tuios-deck").mkdir(parents=True)
    (plain / "tuios-deck/config").write_text("{}")
    launch("contextual", 120, 45, plain)
    keys("F1", "contextual")
    screen("f1-page", "Mode: deck", "contextual")
    keys("F1 p i", "contextual")
    run("B", "send-text", "-s", "contextual", "-w", "deck", "recover this?")
    screen("draft-before-help", "recover this?", "contextual")
    keys("F1", "contextual")
    screen("f1-prompt", "browse, search and restore saved prompts", "contextual")
    run("B", "send-text", "-s", "contextual", "-w", "deck", "\x1b[200~hidden edit\x1b[201~")
    keys("F1", "contextual")
    text = screen("draft-after-help", "recover this?", "contextual")
    assert "hidden edit" not in text
    keys("ctrl+u ctrl+r", "contextual")
    run("B", "send-text", "-s", "contextual", "-w", "deck", "recover")
    screen("stash-query", "recover this?", "contextual")
    keys("F1", "contextual")
    screen("f1-stashes", "Mode: saved prompts", "contextual")
    keys("Escape Enter", "contextual")
    screen("stash-restored-after-help", "recover this?", "contextual")
    keys("Escape Escape y :", "contextual")  # input mode to normal, then an edited form asks before closing
    run("B", "send-text", "-s", "contextual", "-w", "deck", "keybindings")
    keys("F1", "contextual")
    screen("f1-palette", "Mode: palette", "contextual")
    keys("F1", "contextual")
    screen("palette-query-preserved", "> keybindings", "contextual")
    keys("Escape x F1", "contextual")
    screen("f1-confirmation", "Mode: confirmation", "contextual")
    keys("Escape n", "contextual")
    screen("confirmation-cancelled", "tuios-deck", "contextual")
    keys("a F1", "contextual")
    screen("f1-picker", "Mode: picker", "contextual")
    keys("F1 ctrl+c", "contextual")
    screen("picker-cancelled", "tuios-deck", "contextual")
    keys("l F1", "contextual")
    screen("f1-detail", "Mode: detail", "contextual")
    keys("F1 4 F1", "contextual")
    screen("f1-turns", "Mode: turns", "contextual")
    keys("F1 h T M F1", "contextual")
    screen("f1-glyph", "Mode: glyph table", "contextual")
    keys("F1 Escape Escape", "contextual")
    for command, mode in [("prompt presets", "presets"), ("queued prompts", "queue")]:
        keys(":", "contextual")
        run("B", "send-text", "-s", "contextual", "-w", "deck", command)
        screen(mode + "-command", "> " + command, "contextual")
        keys("Enter F1", "contextual")
        screen("f1-" + mode, "Mode: " + mode, "contextual")
        keys("F1 Escape", "contextual")
    print("PASS F1 contexts, draft/paste protection, stash recovery, palette query and cancellation", flush=True)
    launch("small", 36, 10, config)
    keys("?", "small")
    before = screen("small-help", "Mode: deck", "small")
    keys("PageDown", "small")
    after = screen("small-scrolled", "Mode: deck", "small", previous=before)
    assert before != after
    assert len(after.splitlines()) <= 10 and all(len(line) <= 36 for line in after.splitlines())
    keys("Escape", "small")
    screen("small-restored", "tuios-deck", "small")
    print("PASS help scrolls and clips at 36x10", flush=True)
    # A separately configured help key must replace ?, not shadow text fields.
    (config / "tuios-deck/config").write_text(
        '{"vim_modes":true,"keys":{"deck":{"help":"alt+h"}}}')
    launch("remapped", 100, 40, config)
    keys("alt+h", "remapped")
    text = screen("remapped-help", "Keybindings and actions", "remapped")
    assert "alt+h" == next(line for line in text.splitlines() if "help:" in line).strip("│ ").split()[0]
    print("PASS help itself follows configured binding", flush=True)
    keys("Escape p i", "remapped")
    run("B", "send-text", "-s", "remapped", "-w", "deck", "keep?")
    keys("alt+h", "remapped")
    screen("form-insert-default", "Mode: prompt", "remapped")
    keys("Escape", "remapped")
    screen("form-insert-restored", "keep?", "remapped")
    keys("Escape", "remapped")
    keys("Escape y T F", "remapped")  # the edited form asks before closing
    keys("alt+h", "remapped")
    screen("focused-mode", "Mode: deck", "remapped")
finally:
    for side in ENVS:
        subprocess.run(["tuios", "kill-server"], env=ENVS[side], capture_output=True, timeout=20)
