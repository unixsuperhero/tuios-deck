# Changelog

Releases of `deck`, newest first. Each entry lists what a person using the
deck sees, then what changed for someone working on it. Earlier releases
are not listed; the log starts at v0.4.8.

## v0.4.8 — 2026-10-18

### Added

- **The store is a sqlite database.** `deck.db` replaces `deck.json`, through
  the pure-Go `modernc.org/sqlite` driver, so `go install` still needs no
  cgo. The first run with this release imports `deck.json`, prints one line
  to stderr, and renames it to `deck.json.migrated`; nothing is deleted. A
  save writes only the rows that changed, in one transaction: about 0.4 ms
  for a history batch and 1.2 ms for a one-row change, where the JSON file
  took 8 ms and grew with history. WAL mode, file mode 0600.
- **`deck export` and `deck import`.** Export writes the store as the old
  JSON shape to stdout; import loads a JSON file into the database,
  replacing every table. `TUIOS_DECK_BUSY_TIMEOUT` (ms, default 10000) is
  how long a write waits for a locked database.
- **Searchable, grouped keybindings help.** `/` in the help modal (F1 or
  `?`) filters every mode at once by key, action or scope, with a header per
  mode the rows came from; `enter` keeps the filter, `esc` clears it. Within
  a tab, rows sit under a header per scope, the deck page's motion keys
  together; the pages tab has a header per page. A tab bar under the mode
  line clips with `…` when the tabs do not fit.
- **Task tree modal.** `w` on a pane or a group header of the deck page
  opens its whole project tree, the pane's node marked `◂ this pane`, with
  the tasks page keys inside it: `s` cycles status, `e` edits, `n` adds a
  subtask through the outline, `x` deletes after a confirmation, `a` assigns
  the pane to the highlighted node, `enter` jumps to the tasks page there.
  Also in the palette as `task tree…`, and bindable as the `tree` action.
- **Stale panes.** A pane with no activity for `stale_after` (a setting,
  default `45m`, `0` turns it off) shows a dim `⏾` and its age in the rail
  and on the web page, and the detail header says `stale · last activity 2h
  ago`. Activity is the newest of the agent's state change, the window's
  latest history entry and when the deck first saw the pane; a working pane
  is never stale. A `stale` grouping for `o`/`O`, the palette command `close
  stale panes…` and the page's "Close stale (N)" button list the panes and
  ask before closing. Nothing closes on its own.
- **`planned` status** for tasks and projects, after `skipped` in the `s`
  cycle, marked `◌`. A planned item keeps its colour and is not final, but
  does not count as open.
- `/api/state` carries `stale_after_ms`; each pane has `last_activity`,
  `idle_ms` and `stale`.

### Changed

- `deck web` loads the config file at startup, so a broken file stops it the
  way it stops `deck tui`.
- `scripts/release.sh` pushes a local `main` that is ahead of origin before
  tagging; a `main` behind or diverged from origin, or a rejected push,
  stops the release.
- Saving settings from the TUI keeps `stale_after`.

### Docs

- `docs/plans/2026-10-18-feature-inventory.md`: a plan for an inventory of
  every feature with review marks, growing into an in-app registry.
- `docs/reports/2026-10-18-modular-redesign.md`: the components today,
  every struct by module (from `scripts/structs`), what couples them, a
  data-driven redesign, the orchestrator question and a migration order.
- README: the sqlite store, export and import, help search, the task tree
  modal, stale panes and the `stale_after` setting, the `planned` status.

### Internal

- The e2e suites read the store back through `deck export` instead of
  grepping `deck.json`; `scripts/e2e-all.sh` takes `SCRATCH_BASE` and
  `PORT_BASE` so two checkouts can run it side by side. The setup suite
  covers the release script's push and refuse paths.
- `panes.Activity` computes last activity and staleness for both front
  ends; `store.LastActivity` gives the latest history time per window.
- The tasks page's status cycle, delete and edit take a row, shared with the
  tree modal.
- `scripts/structs` prints every struct type as one Markdown table per
  package.
