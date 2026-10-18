# AGENTS.md: working on tuios-deck

For an agent changing this repository. A person using the deck wants
[README.md](README.md).

## What this is

One Go binary, `deck`, that drives the [tuios](https://tuios.dev) daemon over
its JSON verb protocol on the daemon's unix socket. It owns no PTYs and
embeds no terminal emulator: every action is a daemon verb, and every pane it
shows is a pane `tuios attach` would show too.

```
deck            help, exit 2
deck tui        Bubble Tea dashboard            internal/ui
deck web        HTTP + SSE + WebSocket + page   internal/web (page embedded from internal/web/static)
deck export     the store as the old JSON shape internal/store WriteJSON
deck import     replace the store from that JSON internal/store ImportJSON
deck setup      install files outside the binary setup/ (embedded)
deck update     go install the published module cmd/deck/update.go
deck version    the module version Go stamped in
```

## Layout

```
cmd/deck/            main.go (subcommands, the shared -socket -captures -data -auto-capture flags), update.go
internal/daemon/     the JSON client: Call, Subscribe, Presence, StreamPane; typed verb wrappers in verbs.go
internal/panes/      sessions+agents merged into one shape; activity and transcript text rendering. Both front ends read this;
                     reviews.go names a review's pane from the listing and attaches its turn capture
internal/store/      sqlite.go the sqlite database (deck.db): schema, Open and the deck.json migration, the per-table row sync, export/import;
                     store.go projects, tasks (status, MoveTask) and assignments;
                     theme.go the 16-colour themes and the free-colour pool; context.go the context_items table;
                     history.go the history table; reviews.go the turns computed from it and the review_marks table
internal/context/    context.go Extract, Unwrap and Gather: context items from scrollback, activity, transcript, command-history;
                     actions.go clipboard Copy, Target, Opener, Start (detached), Programs, Terminal
internal/events/     events.go the process's one daemon subscription: Handler, Runner (New, Include, Tap, Start), resume, fan-out
internal/autocapture/ autocapture.go the turn/command captures, an events.Handler behind either front end
internal/history/    history.go the history recorder, an events.Handler that batches live events into one save a second
internal/ui/         model.go state, keys.go dispatch, commands.go daemon calls as tea.Cmd, view.go (helpLine, paneRow)
                     form.go the form type and its keys, formbox.go the popover box and its key hints,
                     massadd.go the outline screen (A), assign.go the task/project fields and assignment,
                     tasks.go, captures.go and contextpage.go the other pages, contextactions.go copy/insert/open,
                     turns.go the Turns tab, grouping.go the left rail's groupings, palette.go the command palette and the verb forms,
                     jump.go the { } ( ) group and level jumps for the rail, tasks, context and Turns,
                     tree.go the task tree modal (w): a pane's whole project, kept in m.tree and shown again
                     whenever a form or confirmation it opened returns to modeNormal,
                     live.go the live mode (enter): keys to the pane through send-keys/send-text, the screen back through capture-pane,
                     a stream-pane held for its size lease and as the output signal; ctrl+a is the prefix, ctrl+a d detaches,
                     liveview.go the pane's view in its own session (ctrl+a Z zoom, ctrl+a f fullscreen) through run-command,
                     pickers.go the prototypes page (U), the picker interface and finish; picker_fuzzy/tree/hotkey/miller/chips.go
                     one prototype each (fuzzy also draws the two-pane one), picker_preview.go the shared ? preview and its screen reads
internal/web/        server.go routes (Handler), stream.go the WebSocket bridge to stream-pane, static/index.html the page
                     store.go the projects/tasks/assignments API, captures.go the captures API, verbs.go list-verbs and call,
                     context.go the context list/extract/patch/delete API, contextactions.go insert and open
internal/capture/    capture files under ~/.local/share/tuios-deck/captures; list.go reads the folder back, with the -turn/-cmd kind
setup/               setup.go installer; setup/omp/ the oh-my-pi extension it installs
scripts/             e2e.sh, e2e-web.sh, e2e-setup.sh, release.sh, wscheck/ (Go WebSocket probe), omp-ext/ (bun check of the extension)
```

## Build

```sh
GOWORK=off go build -o .dev/deck ./cmd/deck    # .dev/ is ignored
GOWORK=off go vet ./...
gofmt -l .
```

`GOWORK=off` matters when the shell inherits a Go workspace from another
checkout. Dependencies are pinned to the versions tuios itself uses
(`charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`,
`github.com/charmbracelet/x/ansi`), plus `github.com/coder/websocket`.

## Tests

There are no unit tests, on purpose. Verification is end to end against
real throwaway tuios daemons, and each suite leaves what it saw under a
scratch folder and prints PASS or FAIL per check. The exit status is the
number of failures.

```sh
GOWORK=off go build -o .dev/deck ./cmd/deck
scripts/e2e-all.sh     # every suite below at once, one line per suite; ONLY="web history" picks some
                       # SCRATCH_BASE=/tmp/deck-all-<name> PORT_BASE=<port> runs it beside another clone's
scripts/e2e.sh         # the TUI: daemon A holds panes, daemon B runs deck tui in a pane driven by send-keys
scripts/e2e-web.sh     # deck web: the API with curl, the event stream, the WebSocket stream with scripts/wscheck
scripts/e2e-setup.sh   # deck setup against a scratch HOME; release.sh bump maths; the omp extension under bun with a fake pi and a fake tuios
scripts/e2e-focus.sh scripts/e2e-history.sh scripts/e2e-prompt-stash.sh scripts/e2e-reminders.sh scripts/e2e-live.sh
python3 scripts/e2e-help.py; python3 scripts/e2e-turns.py; python3 scripts/e2e-review.py
```

Which to run, and when. The suites isolate by `SCRATCH` and `PORT`, so
`e2e-all.sh` takes as long as the longest one; a daemon starts in under
100 ms and the build takes 3 s, so neither is ever the cost:

- A change inside one feature: the suite that covers it, alone
  (`e2e-web.sh` is 15 s; the small TUI suites are under a minute).
- A change to the TUI's shared code (model, keys, view, forms): `e2e.sh`.
- Before the commit that ends the turn: `scripts/e2e-all.sh`, once.

Where the seconds go, so a new check does not bring them back. Almost all
of a suite's time is waiting, not work: 866 daemon round trips cost 20 s of
`e2e.sh`, while `settle` (a `wait-for window-idle` after a key) always runs
its whole idle window even on a quiet pane, and at 700 ms it was 277 of the
suite's 423 seconds. The window is 200 ms now: the deck redraws within
milliseconds and a daemon round trip is ~20 ms. Poll loops sleep 200 ms,
not a second. A program that never shows a prompt, started as an agent,
waits out the daemon's two-minute ready timeout unless the deck is given
`TUIOS_DECK_READY_TIMEOUT` (ms), which `e2e.sh` sets to 5000; that one
check was 122 s.

Rules the suites follow, and a new check should too:

- A throwaway daemon is started under an isolated set of XDG folders with
  every `TUIOS_*` variable unset, so a run from inside a tuios pane never
  reaches the daemon that owns the pane. Copy the `isolate` function.
- Wait on a daemon condition (`tuios wait-for`), never sleep and poll.
  The one exception is the deck's own alternate screen, which
  `wait-for window-output` does not read; `wait_deck` in e2e.sh polls
  `capture-pane` for it.
- A server started in the background is started with `exec` so the cleanup
  trap kills the binary, not a wrapper shell. e2e-web.sh refuses to run when
  its port is already taken: a stale server made the suite report
  `not_human` once, and that cost an hour.
- Needs `tuios` on PATH; the setup suite needs `bun` for the extension check
  and skips it otherwise.
- Never test with a headed browser if there is any other way. The web page
  is verified with curl against the API, the SSE stream and `scripts/wscheck`;
  if a check truly needs a DOM, drive a headless browser from a script that
  prints PASS or FAIL, and never open a window on the person's screen or ask
  them to click through it. Visual review of the page by a person is their
  call, not a step an agent takes.
- The scratch folder holds the daemons' sockets, and on macOS a unix socket
  path over 104 bytes fails. `$TMPDIR` there is already long, so give a
  short `/tmp` path, and a different one per run when suites run side by
  side: `SCRATCH=/tmp/deck-e2e-<name> scripts/e2e.sh`,
  `SCRATCH=/tmp/deck-e2eweb-<name> PORT=<port> scripts/e2e-web.sh`.

## Things that are easy to get wrong

- **Live mode embeds no emulator, and its input is one goroutine.** While
  `m.live` is set every key goes to `handleLiveKey`, before the modes and the
  pages; `loadDetail` returns nil and `detailMsg` is dropped, since the live
  goroutines feed the screen. The daemon does the terminal work: `capture-pane`
  (visible, styled) is the screen, `get-window` the cursor (only while a tuios
  client is attached to the session; the suites' detached daemons give none),
  `send-keys` encodes each key for the pane's modes and `send-text` types
  runs of text and pastes. Input calls run from one goroutine in order: a
  tea.Cmd per key would let a later key land first. `stream-pane` is opened
  for its size lease and its frames only; the lease must be renewed every ten
  seconds or the daemon drops it, and it changes the pane's own size (what
  `tput cols` prints), not the `width` `list-windows` reports, which is the
  window's rectangle. `liveSize` and `View` must agree on the live area.
- **A lease never makes a pane larger.** The live lease is `min` per
  dimension with what the session's clients ask for, so under an attached
  tuios client the pane stays the size of its tile and the live bar shows
  that. `fit` is already sent; it grows the pane only while no client shows
  the session. More room under a client comes from that client: liveview.go
  focuses the pane and runs `ToggleZoom` (tiling on) or `SnapFullscreen`
  (refused while tiling is on, so `DisableTiling` first) through
  `run-command`, remembers the tiling mode it changed in `live.view`, and
  puts it back with the `unsnap` action or a second `ToggleZoom`. There is
  no daemon verb for zoom; `run-command` answers `needs_client` headless, and
  the client slides the pane to its box after the verb returns, so a suite
  waits for the live bar's size to leave the tile's before it measures.
- **Presence.** `agent-transcript` is served only to a connection holding
  the person's nonce from `attach-presence`, and the daemon refuses that
  call from any process inside one of its own panes, by TTY, ancestry and
  environment. So the transcript tab works only when the deck runs outside
  tuios or in a pane of a different daemon. `deck web` holds one presence
  for its lifetime and asks again when the daemon drops it.
- **The daemon is the source of truth for harness support.** Activity comes
  from hook events the harness reports; transcripts come from a reader
  inside tuios, and today only Claude Code's format has one. Harness support
  is added with dotfiles, config and harness extensions, never by changing
  tuios. The omp extension under `setup/omp/` is the pattern: it reports
  through the public `set-agent-state` verb.
- **A terminal is always an interactive shell.** `new-window` can run a bare
  program instead, but such a pane closes when the program exits, which reads
  as a dead shell. The forms do not offer it; prompts go through `p`.
- **Nothing resets the person's view on a refresh.** The list re-renders on
  daemon events but keeps the selection and scroll; the detail re-reads on a
  timer but only replaces text that changed, and keeps a scroll position
  that is not at the bottom. Keep that property.
- **Working-directory fields need a picker.** The web page browses the
  daemon machine's folders through `/api/dirs` because a browser's own
  picker cannot name a server path. A new path field gets the same button.
- **Colours** in the TUI are the 16 ANSI colours only, so it follows the
  terminal's theme. A store theme's colour is one of their names;
  `Theme.ANSI()` is the lipgloss index, `Theme.CSS()` the hex for the web.
- **Assignments are pruned after two listings miss a window, not one.** A
  window the deck just made can be missing from a listing asked for before
  it existed, and pruning on that first miss would drop the assignment the
  form just wrote. `pruneAssignments` in ui/assign.go keeps the first miss
  in `m.missing` and prunes on the second.
- **A subtask stores no project_id.** Only a top-level task carries one;
  `store.ProjectOf` walks parents to the root and reads it there, and
  `Resolve` does the same for an assignment. Never read `Task.ProjectID`
  off a subtask and expect its project.
- **The store is one sqlite database (`deck.db`, modernc.org/sqlite, no
  cgo) mirrored in memory.** Every table is a slice or map on `Store`,
  guarded by a mutex because `deck web` serves requests concurrently. Every
  mutating method takes the lock, changes memory and calls `save()` before it
  returns; a new one does the same. `save()` diffs each table against the
  rows last written (`table.sync` in sqlite.go) and writes only the changed
  ones, all in one transaction; history is not diffed, `AddHistory` inserts
  its batch directly. A new table needs its `table[T]` in `newTables`, its
  `CREATE TABLE` in `schema`, a line in `syncAll`, `load`, `importJSON` and
  the `file` struct, and a bump of `schemaVersion` with a migration for
  databases that exist. A new field on a row type needs its column too, or
  it is silently not stored.
- **The palette's result view holds the detail pane.** While `m.result` is
  set, `loadDetail` returns nil, so the timed re-read does not overwrite a
  verb's result; the deck-page keys that move the selection or the tab call
  `clearResult`, which gives the pane back. A new key that changes what the
  detail shows must do the same.
- **The e2e seeds `deck.json` by hand** in the old JSON shape and points the
  deck at it with `TUIOS_DECK_DATA`, so a run never touches the person's
  store. The deck migrates the seed into the `deck.db` beside it and renames
  the seed to `deck.json.migrated`, so a check never reads `$DATA`: it reads
  `"$(store_json)"`, which runs `deck export` into a file and prints its path
  (python suites call `deck export` themselves, or open the `.db` with the
  `sqlite3` module while the deck is off). A seed is migrated only into a
  database with no rows; reseeding mid-suite needs `deck import`. A check
  that needs a project or a task adds it to that seed, not to the real file.
  To make a save fail, hold the write lock from a second connection with the
  deck started under `TUIOS_DECK_BUSY_TIMEOUT=0` (`lock_store` in
  e2e-prompt-stash.sh). Its seeded task is titled "command palette", which is
  also the palette's header text: a grep for the palette must match the
  `─ command palette` header, and a grep for the task must match its row,
  or a check passes for the wrong reason.
- **Adding a page** to the TUI: a `page` constant and its name in the
  `String` array in model.go, a `handle<Page>Key` block that keys.go
  dispatches to after the keys every page answers, the left/right lines in
  the `switch m.page` of `View`, and a `helpLine` branch. `T` and `C` set
  the page from anywhere and `esc` goes back to the deck; the tick re-reads
  the detail only on the deck page.
- **An emptied detail with a kept scroll panics.** The view slices
  `detail[scroll:]`, and `loadDetail` keeps the scroll while the pane and
  the tab are the same. Whatever empties `m.detail` without changing the
  pane or the tab (`g`, `clearResult`) resets `m.scroll` with it; `r` keeps
  the old lines and scroll and sets `revision = 0` so the fresh read
  replaces them.
- **Forms are popovers over one `form` type.** A caller sets `Fields`,
  `Validate` and `Done`; a `field` may add `Options` (the fuzzy list),
  `Hint` or `HintFn` (worked out on every draw), `Multi` (enter is a
  newline, `@` completes paths) and `New` ("project" or "task", which ends
  the list with `+ add new…`). `form.OnKey` sees every key first, which is
  how `ctrl+h`/`ctrl+l` walk a task form's parent. Taking `+ add new…` sets
  `form.adding`; keys.go parks the form in `m.formStack`, opens the
  mass-add form, and on its submit or `esc` puts the parked form back
  (`resumeWith` fills the field). There is one slot: never open a third
  form from the mass-add one.
- **One daemon event stream per process, and the recorders run outside the
  Bubble Tea model on purpose.** cmd/deck builds one `events.Runner` with
  the auto-capture and history handlers behind it; the TUI taps it once
  (`ui.EventTypes`, `SetEvents`) and `deck web` once per browser tab
  (`web.EventTypes`, `Server.Events`). The runner dials, reconnects with
  backoff and resumes with `after_seq` + `boot_id`; it calls every handler's
  `Resync` after the first `subscribed` reply, after a daemon `gap`, and
  after that handler's own queue overflowed. Each handler has its own
  goroutine and a bounded queue, so neither can move the selection, reset a
  scroll or write the status line; a tap's channel closes only when the
  process ends, never on a reconnect, and carries every `subscribed` and
  `gap` event so the front end lists again. `deck tui` gives the runner no
  logger; `deck web` logs. Keep the handlers out of `ui.Model`, and wait on
  `Start`'s done channel before exiting: history flushes its last batch in
  `Close`.
- **Reviews are a view over history; only the mark is stored.** `store.Reviews`
  walks history's `prompt-submitted`, `response-completed`, `session_start`
  and `subagent_start`/`stop` entries (source `agent-activity`) per window on
  every call. The person's prompt opens a turn and takes every response until
  the next one; a `<task-notification>` prompt (a subagent waking the turn) is
  not a new turn, it keeps the turn open for its next response. A turn with a
  response is listed only when it is not awaiting a response and the window's
  running-subagent count (counted across turns, reset on `session_start`) is
  zero; the rest are the header's "still running". `review_marks` holds the
  prompt's history id → reviewed time (a mark keyed by a response id is
  adopted onto the turn on read). Nothing polls the daemon for turns, and a
  review's output is the `-turn` capture auto-capture wrote, found on disk when
  the review is opened. A suite that wants output must run the deck with
  auto-capture on before the turn ends, and wait for the capture file.
- **A suite's "deck started" check must not match the typed command.** A
  clone's path can hold `tuios-deck`, so the command line echoed in the pane
  matches before the deck runs; wait for the alternate screen to replace it
  (e2e-review.py waits until `REVIEW-TUI-EXITED` leaves the screen).
- **`q` asks before quitting, and `esc` on an edited form asks before
  closing.** Both go through `modeConfirm` (`confirmState` in model.go: `Do`
  runs as a tea.Cmd, `Then` on the model, `Back` is the mode `n` returns
  to). A form remembers its values on the first key or paste (`form.remember`)
  and `form.changed` compares against them; an untouched form closes at
  once, and a prompt's draft is autosaved on every key before any of this. In
  a suite, close a form with `cancel_form` (e2e.sh) or `cancel`
  (e2e-prompt-stash.sh), which answer the question only when it appears, and
  quit with `q` then `y`.
- **Context items hold window ids too.** Each item in the store keeps the
  `window` it came from, so a check that a pane's assignment was pruned
  must match the assignment key (`"<window id>": {`), never the bare id,
  or a context item keeps it "present". Pruning drops assignments only;
  items outlive their panes.
- **`command-history` flattens wrapped command lines.** The daemon takes a
  cmdline from the screen rows between the shell's marks and turns every
  newline into a space, so a command that soft-wrapped comes back with a
  space where the row broke, splitting a long link or path in it. The
  extractor's `Unwrap` rejoins wrapped rows of the scrollback (a row as wide
  as the widest is taken to continue) before it searches them; history
  lines come as the daemon sends them. The real fix is in tuios.
- **`send-keys` in the e2e suites sends several keys only as one quoted
  argument**: `b send-keys ... 'Tab Tab BTab'`. Written as separate
  arguments, nothing is sent and the check fails on a screen that never
  changed.
- **The picker prototypes are parallel implementations on purpose.** Each
  `picker_*.go` sits behind the `picker` interface in `pickers.go` and
  shares only `pickBase`, the targets and `picker_preview.go`. Do not factor them
  together; once the person picks a winner the rest are deleted and the
  winner replaces the assign and filter forms.
- **Agents work in clones under `.dev/work/<name>`** (ignored, so the main
  tree's `go build ./...` never sees them), each on its own branch, and the
  coordinator fast-forwards `main` from them. Do not edit the main tree
  from a parallel agent, and rebase on `main` before reporting.

## Daemon protocol

`tuios list-verbs` (or `--json`) is the contract: every verb, parameter,
result shape and error code of the daemon you are talking to. Read it before
adding a wrapper to `internal/daemon/verbs.go`; a parameter the verb does not
take is refused, not ignored. `tuios call VERB '{...}'` tries a verb from the
shell. The wire format and `stream-pane` frames are in the tuios repository's
`docs/protocol.md`.

## Commits and releases

Conventional commits (`feat:`, `fix:`, `docs:`, `chore:`), one change per
commit, committed before the turn ends. Do not amend. `main` is pushed to
github.com/unixsuperhero/tuios-deck.

No version number lives in the source. `scripts/release.sh [patch|minor|major|vX.Y.Z] [--dry-run]`
tags, pushes and primes the Go module proxy; `deck update` installs the
result. Tagging is the maintainer's call. The script refuses to tag a
version that has no `## vX.Y.Z` entry in CHANGELOG.md, and writes the
prompt for one to `.dev/changelog-prompt.md` (`--changelog-prompt` writes it
without releasing).

## Changelog

CHANGELOG.md is a diff of the deck between two tags, not a story. One entry
per release, newest first, `## vX.Y.Z — YYYY-MM-DD`. Under it, one `###`
list per concept, in this order, and only the concepts that changed:

Store · CLI · Settings · Pages · Modals · Forms · Actions and keybindings ·
Palette · Groupings · Rail and detail · Live mode · Web page · API · Events
and recorders · Scripts and suites · Docs

Every line is one item, one change, and starts with its verb:

- `added: <thing>` with the facts that let someone use it: key, default,
  flag, path, unit.
- `changed: <thing>, <now>; was <before>` always names both sides.
- `removed: <thing>` and what replaces it, if anything.
- `fixed: <thing>`, the symptom, not the cause.

Rules: one line each, no paragraphs and no sentences that only explain;
names in backticks; a number only where it changes what the reader does
(a default, a limit, a timing that moved by an order); a key written as the
help shows it; a mark or glyph quoted as the character. Something that spans
concepts is listed once, under the concept the person meets first, and the
other lists refer to it by name. Internal changes that nobody sees go under
Scripts and suites or not at all. Write the entry from `git log
<last tag>..HEAD` and the diff, reading the README sections the commits
changed when a commit message is not enough.
