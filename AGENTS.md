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
deck setup      install files outside the binary setup/ (embedded)
deck update     go install the published module cmd/deck/update.go
deck version    the module version Go stamped in
```

## Layout

```
cmd/deck/            main.go (subcommands), update.go
internal/daemon/     the JSON client: Call, Subscribe, Presence, StreamPane; typed verb wrappers in verbs.go
internal/panes/      sessions+agents merged into one shape; activity and transcript text rendering. Both front ends read this
internal/store/      store.go projects, tasks and assignments in one JSON file (deck.json); theme.go the 16-colour themes
internal/ui/         model.go state, keys.go dispatch, form.go the status-line forms, commands.go daemon calls as tea.Cmd, view.go
                     assign.go the task/project fields and assignment, tasks.go and captures.go the two other pages,
                     grouping.go the left rail's groupings, palette.go the command palette and the verb forms
internal/web/        server.go routes, stream.go the WebSocket bridge to stream-pane, static/index.html the page
                     store.go the projects/tasks/assignments API, captures.go the captures API, verbs.go list-verbs and call
internal/capture/    capture files under ~/.local/share/tuios-deck/captures; list.go reads the folder back
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
number of failures. Run them after any change; they take a few minutes.

```sh
GOWORK=off go build -o .dev/deck ./cmd/deck
scripts/e2e.sh         # the TUI: daemon A holds panes, daemon B runs deck tui in a pane driven by send-keys
scripts/e2e-web.sh     # deck web: the API with curl, the event stream, the WebSocket stream with scripts/wscheck
scripts/e2e-setup.sh   # deck setup against a scratch HOME; release.sh bump maths; the omp extension under bun with a fake pi and a fake tuios
```

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

## Things that are easy to get wrong

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
- **The store is one JSON file, written whole and atomically** (temp file,
  rename, 0600), guarded by a mutex because `deck web` serves requests
  concurrently. Every mutating method takes the lock and saves before it
  returns; a new one does the same. No partial writes, no second file.
- **The palette's result view holds the detail pane.** While `m.result` is
  set, `loadDetail` returns nil, so the timed re-read does not overwrite a
  verb's result; the deck-page keys that move the selection or the tab call
  `clearResult`, which gives the pane back. A new key that changes what the
  detail shows must do the same.
- **The e2e seeds `deck.json` by hand** in the store's shape and points the
  deck at it with `TUIOS_DECK_DATA`, so a run never touches the person's
  store. A check that needs a project or a task adds it to that seed, not
  to the real file. Its seeded task is titled "command palette", which is
  also the palette's header text: a grep for the palette must match the
  `─ command palette` header, and a grep for the task must match its row,
  or a check passes for the wrong reason.
- **Adding a page** to the TUI: a `page` constant and its name in the
  `String` array in model.go, a `handle<Page>Key` block that keys.go
  dispatches to after the keys every page answers, the left/right lines in
  the `switch m.page` of `View`, and a `helpLine` branch. `T` and `C` set
  the page from anywhere and `esc` goes back to the deck; the tick re-reads
  the detail only on the deck page.

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
result. Tagging is the maintainer's call.
