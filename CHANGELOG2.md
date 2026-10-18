# Changelog

A diff of the deck between two tags, newest first. The format is the
Changelog section of AGENTS.md. The log starts at v0.4.8.

## v0.4.8 — 2026-10-18

### Store
- added: sqlite database `deck.db` beside the old file, driver `modernc.org/sqlite` (no cgo); WAL, mode 0600
- added: migration on first open when every table is empty, `deck.json` → `deck.db`, the JSON renamed `deck.json.migrated`, one line on stderr
- added: `TUIOS_DECK_BUSY_TIMEOUT` ms to wait for a locked database, default `10000`
- added: status `planned` for tasks and projects, mark `◌`, not final, not counted as open
- changed: a save writes the changed rows in one transaction; was the whole file (≈8 ms per save and growing, now ≈0.4–1.2 ms flat)
- changed: history from two processes on one database merges by id; was the last writer's file

### CLI
- added: `deck export`, the store as the JSON shape on stdout
- added: `deck import FILE.json`, replaces every table
- changed: `deck web` loads the config file at startup and stops on a broken one; was never read

### Settings
- added: `stale_after`, a duration, default `45m`, `0` off; `deck config get/set stale_after`
- fixed: saving settings from the TUI dropped `stale_after`

### Pages
- changed: tasks page `s` cycles open → done → skipped → planned → open; was open → done → skipped → open
- changed: tasks page rows show a mark for every status but open (`✓` `⊘` `◌`); was done and skipped only

### Modals
- added: task tree, `w` on a pane or group header: the whole project tree, the pane's node marked `◂ this pane`, pane counts per node
- added: task tree keys `j`/`k` move, `s` status, `e` edit, `n` subtask via the outline, `x` delete with confirmation, `a` assign the pane, `enter` to the tasks page, `esc` close
- added: keybindings help `/` search across every mode, results under a `── <mode>` header; `enter` keeps the filter, `esc` clears it, `backspace` on empty leaves
- changed: keybindings help rows sit under a header per scope, deck motion keys under `deck page, motion`, the pages tab under a header per page; was one flat list per tab
- added: keybindings help tab bar under the mode line, clipped with `…` when the tabs do not fit; hidden under 14 rows
- added: `close stale panes…` confirmation listing age and session / name, `y` closes exactly those

### Actions and keybindings
- added: deck `tree` = `w`
- added: help `/`

### Palette
- added: `task tree…`
- added: `close stale panes…`

### Groupings
- added: `stale` level and preset, groups `stale` and `active`, reached by `o`/`O` and `group by stale`

### Rail and detail
- added: stale pane row `⏾ 2h` before the state; detail header `stale · last activity 2h ago`
- added: last activity = newest of the agent's `agent_state_at`, the window's latest history entry, and when this deck first saw the pane; a working pane is never stale

### Web page
- added: `⏾` and age on stale pane rows; header stale text
- added: `Close stale (N)` button in the rail header, asks first; `close stale panes` palette entry
- added: task labels show `⊘` and `◌`; was `✓` only
- changed: refreshes every 30 s while visible; was on daemon events only (selection and scroll kept)

### API
- added: `/api/state` `stale_after_ms`; per pane `last_activity`, `idle_ms`, `stale`
- changed: `PATCH /api/tasks/{id}` and `/api/projects/{id}` accept `"status": "planned"`

### Scripts and suites
- changed: `release.sh` pushes a local `main` ahead of origin before tagging; refuses one behind or diverged, or a rejected push; was refuse unless equal
- changed: `e2e-all.sh` takes `SCRATCH_BASE` and `PORT_BASE`
- changed: suites read the store through `deck export`; was grep of `deck.json`
- changed: `e2e-prompt-stash.sh` makes a save fail by holding the write lock; was replacing the file with a folder
- added: `scripts/structs`, every struct type as a Markdown table per package
- added: checks for `planned`, the help search and headers, the tree modal, stale marks and closing, the release script's push and refuse paths

### Docs
- added: `docs/plans/2026-10-18-feature-inventory.md`
- added: `docs/reports/2026-10-18-modular-redesign.md`, with every struct by module
- added: `CHANGELOG.md`
- changed: README for the sqlite store, export and import, help search, the task tree modal, stale panes, `stale_after`, `planned`
- changed: AGENTS.md for the sqlite store and adding a table, the task tree file, the suites' `deck export` reads
