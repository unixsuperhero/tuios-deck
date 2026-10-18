# tuios-deck as modular, data-driven units

Report, 2026-10-18. Answers the TODO item: what components make the deck
work, what they would look like as modular, data-driven systems that change
and grow independently, and whether that needs an orchestrator.

Related: [the feature inventory plan](../plans/2026-10-18-feature-inventory.md)
lists what is enumerable (pages, groupings, actions, palette commands,
modals, routes, structs) and proposes a registry with review marks. This
report does not redo that inventory. It proposes the unit boundaries that
registry would live inside, and step 3 of the migration below is that plan's
stage 2.

## Summary

- Yes, it needs an orchestrator, and it already has one in rough form:
  `cmd/deck` builds the client, the store, the event runner and the
  recorders, then hands everything to `ui.Model` or `web.Server`.
- Recommended shape: **(a) a thin orchestrator** in `cmd/deck` that wires
  units through small interfaces and owns one in-process **command bus**
  (writes) and the existing **event runner** (reads). Not a free-for-all
  message bus.
- The biggest single change is **a shared core** (`internal/core`) between
  the two front ends: today ui and web each re-implement starting agents,
  target resolution, presets, previews and JSON shaping.
- The store splits into **per-table units** behind one transaction API, so
  history can stop rewriting projects, and the sqlite TODO becomes a
  backend swap.
- Pages, palette commands, groupings, modals and routes become **definition
  tables** registered by units; a new page is one table entry plus its
  render and key functions, not edits in five files.
- `ui.Model` stays the TUI's view-state holder (selection, scroll, modal
  slot); it stops holding domain logic.
- Migration is nine steps, each green on every e2e suite, smallest first.

## Measurements

Measured with `go list -f '{{.Imports}}'`, `wc -l`, `grep -c` over the clone
at `21d9e0d`. Script: `go list` per package for imports and importers,
`grep -cE '^func (\(s \*Store\) )[A-Z]'` for methods.

| Package | Files | Lines | Exported funcs/methods | Structs |
|---|---:|---:|---:|---:|
| cmd/deck | 3 | 631 | 0 | 1 |
| internal/ui | 41 | 13,589 | 16 | 66 |
| internal/web (Go) | 16 | 2,439 | 3 | 11 |
| internal/web/static | 4 | 2,933 | | |
| internal/store | 10 | 2,387 | 89 (73 on `*Store`) | 19 |
| internal/daemon | 4 | 746 | 33 | 24 |
| internal/context | 2 | 376 | 10 | 2 |
| internal/config | 2 | 342 | 10 | 3 |
| internal/events | 1 | 330 | 5 | 4 |
| internal/panes | 2 | 293 | 9 | 2 |
| internal/complete | 3 | 289 | 3 | 0 |
| internal/history | 1 | 288 | 5 | 1 |
| internal/autocapture | 1 | 210 | 4 | 2 |
| setup | 1 | 204 | 9 | 2 |
| internal/capture | 2 | 196 | 9 | 1 |
| internal/reminders | 1 | 89 | 1 | 0 |
| internal/prompts | 1 | 76 | 1 | 0 |
| internal/focus | 1 | 37 | 3 | 1 |
| **Go total** | | **22,522** | | |

Other counts:

```js
{
  uiShareOfGo: "60%",                 // 13,589 / 22,522
  storeCallsFromUi:  { calls: 189, distinctMethods: 63 },
  storeCallsFromWeb: { calls: 111, distinctMethods: 59 },
  storeMethodsUsedByBoth: 57,         // the shared surface a core would own
  storeSaveSites: 37,                 // s.save() calls, each writes the whole file
  webRoutes: 61, pageApiPaths: 23,    // 57 /api/ references in index.html
  uiPages: 7, uiModes: 7, uiMsgTypes: 18, updateCases: 26,
  pageConstantRefs: { refs: 67, files: 16 },
  modelFields: 78,                    // ui.Model, model.go:87-188
  tables: { groupingPresets: 12, keyActions: 51, deckPaletteItems: 25, contextHelpModes: 11 },
  sameNamedFilesInUiAndWeb: 10,       // attach captures contextactions history notes
                                      // previews promptqueue prompts reminders reviews
  panesListCallSites: 9,              // ui 1, web 5, history 1, autocapture 1 (+ panes itself)
  e2eLines: 3549, e2eSuites: 11
}
```

## 1. The components today

### Dependency graph today

```
                               cmd/deck
   ┌──────┬──────┬──────┬──────┬──┴───┬──────┬──────────┬───────────┬───────┐
   v      v      v      v      v      v      v          v           v       v
  ui     web   config setup reminders history autocapture events  capture  store
   │      │                      │      │      │          │
   │      │                      │      ├──────┼──> events├──> daemon
   │      │                      │      └──> panes <─────┘
   │      │                      │            │  └──> capture ──> store (DataDir)
   │      │                      │            └──> store
   ├──────┴──> context ──> daemon, store
   ├──────┴──> prompts ──> daemon, panes, store
   ├──────┴──> complete, capture, history, panes, daemon, store
   └──> config, focus

store: imported by 8 packages, imports none.   daemon: imported by 9, imports none.
ui and web import the same 9 internal packages; ui adds config and focus,
web adds events. Neither imports the other.
```

The leaves (`daemon`, `store`, `config`, `focus`, `complete`, `setup`) are
clean. The middle is where things cross: `panes` imports `store` and
`capture`, `capture` imports `store` only for `DataDir`.

### cmd/deck

- Owns: subcommands (`tui web config setup update version`), the shared
  flags, startup order.
- Imports: 11 internal packages. Imported by: nothing.
- Side effects: opens the store, starts the event runner, starts
  reminders, runs Bubble Tea or the HTTP server.
- Wiring it does today, `cmd/deck/main.go:120-185`:

```js
// runTUI, in order
{ client: new daemon.Client(socket),
  store:  store.Open(dataPath),
  config: config.Load(configPath),           // TUI only
  reminders: go reminders.Run(ctx, client, store),
  runner: events.New(client, log, autocapture?, history),
  tap:    runner.Tap(ui.EventTypes),          // before Start
  model:  ui.New(client, store, version, dir) // then SetEvents, SetSettings
}
```

- Verified by every suite (each starts the binary).

### internal/daemon

- Owns: the JSON verb client: `Call`, `Subscribe`, `Presence`, `StreamPane`,
  18 typed verb wrappers.
- Imports: nothing internal. Imported by: 9 packages.
- Side effects: a unix socket connection per call; one long-lived
  subscription and stream connections.
- Types: 24 structs mirroring daemon replies.

```js
Event       = { seq: 812, type: "agent-state", session: "work", window: "w3f…", data: {…} }
StartAgentParams = { session: "work", agent: "claude", name: "reviewer", cwd: "~/proj", prompt: "…", ready_timeout: 5000 }
```

- Verified by e2e-web (curl through the API) and e2e.sh, against real daemons.
- Already a good unit: small, no internal imports, the contract is
  `tuios list-verbs`.

### internal/panes

- Owns: merging sessions and agents into one shape, activity and transcript
  rendering, naming reviews.
- Imports: daemon, store, capture. Imported by: autocapture, history,
  prompts, ui, web.
- Side effects: none of its own; `List` calls the daemon.

```js
Pane = { session: "work", id: "w3f…", name: "reviewer", state: "working",
         harness: "claude", foreground: "claude", cwd: "~/proj/deck",
         is_agent: true, ready: true, needs_you: false, queued: 0, unread: 2, working: true }
Session = { name: "work", attached: true, dir: "~/proj", branch: "main", panes: [Pane] }
```

- Reach-across: `HiddenTasks(sessions, *store.Store, now)` (panes.go:120)
  takes the whole store to read snoozes; `ReviewOutput` reads capture files.
  A pane listing should not need the task table.
- 9 independent `panes.List` call sites: every consumer lists on its own,
  there is no shared registry of live panes.

### internal/store

- Owns: `deck.json`. Ten tables in one struct: projects, tasks, assignments,
  context items, reminders, notes, presets, queued prompts, history, review
  marks, prompt stashes.
- Imports: nothing internal. Imported by: 8 packages.
- Side effects: one file, written whole and atomically under one mutex; 37
  save sites.

```js
Project    = { id: "p1", name: "deck", theme: { color: "cyan", glyph: "◆" }, status: "open", created: 1760… }
Task       = { id: "t9", parent_id: "", project_id: "p1", title: "command palette",
               theme: {…}, status: "open", easy: true, snoozed_until: 0, created, updated }
Assignment = { "w3f…": { session: "work", task_id: "t9", project_id: "" } }
ContextItem = { id, kind: "link", text: "https://…", session, window, pane,
                project_id, task_id, starred: false, pinned: false, created }
HistoryEntry = { id, kind: "prompt-submitted", at, observed, source: "agent-activity",
                 source_id, session, window, summary, detail }
QueuedPrompt = { id, text, preset, task_id, project_id, cwd, session, agent, window, created }
Reminder   = { id, title, message, interval: 3600, enabled: true, next, last, created }
Note       = { id, text, task_id, project_id, created, updated }
Preset     = { id, name: "question", prefix: "answer in under 10 words: ", cwd: "", created }
review_marks = { "<history id>": 1760…ms }
```

- Verified through the front ends: e2e seeds `deck.json` by hand and reads
  it back. No suite drives the store alone.

### internal/context

- Owns: extracting context items from scrollback, activity, transcript and
  command history (`Extract`, `Unwrap`, `Gather`); clipboard, opener and
  terminal actions.
- Imports: daemon, store. Imported by: ui, web.
- Types: `Item = store.ContextItem` (context.go:21), `Source`, `Counts`.
- Side effects: spawns opener processes, writes the clipboard.
- Verified by e2e.sh (context page) and e2e-web (extract, patch, insert).

### internal/events

- Owns: the one daemon subscription per process, reconnect with backoff,
  resume by `after_seq` + `boot_id`, fan-out to handlers and taps.
- Imports: daemon. Imported by: cmd/deck, autocapture, history, web.
- Side effects: one goroutine per handler, one per tap, bounded queues of 256.

```js
Handler = { Types(): ["agent-state", "window-closed"], Handle(ctx, ev), Resync(ctx, bootID), Close?() }
```

- Verified by e2e-history and e2e-web (event stream).
- Already the right shape for a unit boundary; the redesign reuses it.

### internal/autocapture and internal/history

- Own: background recorders, each an `events.Handler`. autocapture writes
  `-turn`/`-cmd` capture files when a turn or command ends; history batches
  events into `store.AddHistory` once a second.
- Import: daemon, events, panes, and capture or store.
- Side effects: files in the captures folder; whole-store saves.
- Verified by e2e-review.py, e2e-turns.py, e2e-history.sh.
- They run outside `ui.Model` on purpose (AGENTS.md). That is the pattern the
  redesign generalises.

### internal/capture

- Owns: capture files under `~/.local/share/tuios-deck/captures`.

```js
Entry = { name: "work-build-20261017-133455-turn.txt", path, size: 4096,
          modified: "2026-10-17T13:34:55Z", session: "work", pane: "build", kind: "turn" }
```

- Imports store only for `DataDir` (capture.go:20). Imported by 5.

### internal/config

- Owns: `~/.config/tuios-deck/config`, the key action table (51 actions over
  4 modes, `keys.go:27`), validation, bind/unbind.
- Imports nothing; imported by cmd/deck and ui.
- Already data-driven: actions are a table and keys resolve through it.

### internal/ui

- Owns: the Bubble Tea program: 7 pages, 7 modes, forms, palette, pickers,
  live mode, help. 41 files, 60% of the Go.
- Imports: 10 internal packages. Imported by: cmd/deck.
- Side effects: daemon calls as `tea.Cmd`, store writes, presence, live-mode
  goroutines (`live.go:132,288,310,311`), clipboard and openers via context.
- `Model` (model.go:87-188) has 78 fields: pane list and rail, tasks page,
  captures page, context page, reviews, history, prototypes, live, detail
  pane, turns, mode, form and form stack, confirm, prompt modal, glyph
  table, palette, verbs, status line, events channel, presence, previews.
- Verified by e2e.sh (1,372 lines) and the smaller TUI suites.

### internal/web

- Owns: HTTP routes (61, server.go:54-114), SSE, the WebSocket bridge to
  `stream-pane`, the embedded page (2,933 lines of HTML/JS).
- Imports: 9 internal packages. Imported by: cmd/deck.
- Side effects: an HTTP listener, one presence for its lifetime, one tap
  per browser tab, a 20 s keepalive ticker (server.go:254).
- Verified by e2e-web.sh (curl, SSE, `scripts/wscheck`).

### setup, complete, focus, prompts, reminders

| Package | Owns | Note |
|---|---|---|
| setup | embedded omp extension, install/status/uninstall | clean leaf; items are a table (`Items()`, setup.go:54) |
| complete | slash commands, `@` file completion | clean leaf, shared by ui and web |
| focus | the idle/activity clock | 37 lines, pure |
| prompts | `Dispatch` of a queued prompt | **the one place ui and web already share a use case** |
| reminders | `Run` loop delivering due reminders | a background unit like the recorders, but not an `events.Handler` |

### Where ui and web duplicate logic

| Logic | ui | web | Drift today |
|---|---|---|---|
| start an agent, record submission, assign | commands.go:206 | server.go:401 | web drops `BlockedBy` and `PromptNote` from the status text |
| new terminal (create session if missing) | commands.go:244 | server.go:457 | |
| resolve a task/project target | assign.go:75 | store.go:224 | web comment says "the rule is the TUI's" |
| assign a new window | assign.go:233 | store.go:596 | |
| prune assignments after two misses | assign.go:248 | **none** | a web-only deck never prunes |
| apply a preset (prefix, cwd) | prompts.go:126,151 | prompts.go:92 | |
| previews: `paneLine`, `paneRank`, `relevantPanes`, `pathHas` | previews.go:194,253,215,244 | previews.go:16,34,158,180 | same names, different argument types (`*Pane` vs `Pane`) |
| tail of text | previews.go:127 `tailLines` | previews.go:189 `recentLines` | |
| queue target | promptqueue.go:231 | promptqueue.go:57 | |
| JSON/row shaping of projects, tasks, notes, reminders, queue | rows built in tasks.go, notes.go… | `projectJSON`, `taskJSON`, `noteJSON`, `reminderJSON`, `queuedJSON` | each front end decides what a task "looks like" |

## 1b. Every struct, by module

Generated by `GOWORK=off go run ./scripts/structs` from the source on the date of this report; rerun it rather than editing the tables. `ui` holds 69 of the 143 structs, which is the same 60% its line count shows, and 24 of `daemon`'s are the verb wrappers' request and result shapes. The JSON column is the persistence boundary: every ✓ outside `store` is a shape the web page or the daemon protocol also knows, so a change there has two readers.

143 structs in 14 packages. Fields counts embedded types as one field each; JSON says whether any field carries a `json` tag, which is what the store writes or the web page reads.

### cmd/deck (1)

| Struct | Fields | JSON | Where | Purpose |
|---|---:|:---:|---|---|
| `common` · | 6 |  | `main.go:98` | common holds the flags every daemon-facing command takes. |

### internal/autocapture (2)

| Struct | Fields | JSON | Where | Purpose |
|---|---:|:---:|---|---|
| `pane` · | 6 |  | `autocapture.go:34` | pane is what the watcher keeps per window. |
| `watcher` · | 8 |  | `autocapture.go:45` | watcher is the handler. |

### internal/capture (1)

| Struct | Fields | JSON | Where | Purpose |
|---|---:|:---:|---|---|
| `Entry` | 7 | ✓ | `list.go:18` | Entry is one capture file in the folder. |

### internal/config (3)

| Struct | Fields | JSON | Where | Purpose |
|---|---:|:---:|---|---|
| `Action` | 4 |  | `keys.go:22` | Action is something a key does: its name in the file, the key it has by default, and what it does. |
| `Config` | 3 | ✓ | `config.go:20` | Config is every setting. |
| `Target` | 2 |  | `keys.go:217` | Target is what a pressed key stands for after rebinding: the default key of the action it is bound to, which the existing handlers answer. |

### internal/context (2)

| Struct | Fields | JSON | Where | Purpose |
|---|---:|:---:|---|---|
| `Counts` | 5 | ✓ | `context.go:188` | Counts is how much a Gather read, by where it came from. |
| `Source` | 5 |  | `context.go:27` | Source is where a piece of text came from. |

### internal/daemon (24)

| Struct | Fields | JSON | Where | Purpose |
|---|---:|:---:|---|---|
| `Activity` | 3 | ✓ | `verbs.go:120` | Activity is the result of agent-activity with recap. |
| `ActivityEntry` | 9 | ✓ | `verbs.go:107` | ActivityEntry is one entry of an agent's activity ring. |
| `Agent` | 14 | ✓ | `verbs.go:60` | Agent is one row of list-agents. |
| `Capture` | 5 | ✓ | `verbs.go:87` | Capture is the result of capture-pane. |
| `Client` | 1 |  | `client.go:18` | Client talks to one daemon socket. |
| `Error` | 3 | ✓ | `client.go:47` | Error is a verb failure: a stable code, a message, and the daemon's hint. |
| `Event` | 9 | ✓ | `client.go:125` | Event is one line of the subscribe stream. |
| `Hello` | 4 | ✓ | `verbs.go:298` | Hello identifies the daemon. |
| `HistoryEntry` | 9 | ✓ | `verbs.go:174` | HistoryEntry is one command of command-history: what a shell in a pane ran, read between its OSC 133 marks. |
| `NewWindowResult` | 2 | ✓ | `verbs.go:249` | NewWindowResult is what new-window reports. |
| `QueueResult` | 4 | ✓ | `verbs.go:272` | QueueResult is what queue-prompt reports. |
| `Resume` | 2 |  | `client.go:141` | Resume names where an earlier stream of the same daemon stopped: the seq of the last event received and the boot_id it came with. |
| `Session` | 8 | ✓ | `verbs.go:9` | Session is one row of list-sessions. |
| `SessionWindow` | 6 | ✓ | `verbs.go:21` | SessionWindow is a window as list-sessions reports it. |
| `StartAgentParams` | 7 | ✓ | `verbs.go:201` | StartAgentParams are the inputs of start-agent. |
| `StartAgentResult` | 12 | ✓ | `verbs.go:214` | StartAgentResult is what start-agent reports once the agent is ready or not. |
| `Stream` | 3 |  | `stream.go:26` | Stream is an open stream-pane connection: frames flow both ways. |
| `StreamParams` | 6 |  | `stream.go:33` | StreamParams are the inputs of stream-pane. |
| `StreamReply` | 8 | ✓ | `stream.go:14` | StreamReply is the one JSON line stream-pane answers before the frames. |
| `Transcript` | 3 | ✓ | `verbs.go:156` | Transcript is the result of agent-transcript. |
| `TranscriptEntry` | 8 | ✓ | `verbs.go:144` | TranscriptEntry is one entry of agent-transcript. |
| `Window` | 6 | ✓ | `verbs.go:336` | Window is get-window's answer. |
| `ctxConn` · | 2 |  | `client.go:74` |  |
| `envelope` · | 5 | ✓ | `client.go:55` |  |

### internal/events (4)

| Struct | Fields | JSON | Where | Purpose |
|---|---:|:---:|---|---|
| `Runner` | 11 |  | `events.go:53` | Runner is the dispatcher. |
| `item` · | 3 |  | `events.go:71` | item is one entry of a handler's queue: an event, or a resync marker. |
| `tap` · | 2 |  | `events.go:84` |  |
| `worker` · | 4 |  | `events.go:77` |  |

### internal/focus (1)

| Struct | Fields | JSON | Where | Purpose |
|---|---:|:---:|---|---|
| `Clock` | 4 |  | `clock.go:6` |  |

### internal/history (1)

| Struct | Fields | JSON | Where | Purpose |
|---|---:|:---:|---|---|
| `recorder` · | 8 |  | `history.go:37` | recorder is the handler of the process's event stream (internal/events). |

### internal/panes (3)

| Struct | Fields | JSON | Where | Purpose |
|---|---:|:---:|---|---|
| `Activity` | 2 |  | `panes.go:132` | Activity tells stale panes from active ones. |
| `Pane` | 19 | ✓ | `panes.go:19` | Pane is one window of one session. |
| `Session` | 5 | ✓ | `panes.go:47` | Session is one session with its panes in window order. |

### internal/store (19)

| Struct | Fields | JSON | Where | Purpose |
|---|---:|:---:|---|---|
| `Assignment` | 3 | ✓ | `store.go:48` | Assignment ties a pane, keyed by window id, to a task or a project. |
| `ContextItem` | 11 | ✓ | `context.go:13` | ContextItem is something worth keeping that was found in a pane: a link, a file or a folder, a pull request, a prompt the person gave an agent or a command a shell ran. |
| `GlyphSection` | 2 | ✓ | `glyphs.go:6` | GlyphSection is a labelled group of the glyphs the glyph tables offer. |
| `HistoryEntry` | 10 | ✓ | `history.go:13` | HistoryEntry preserves a source observation, not a reconstructed conversation. |
| `Note` | 6 | ✓ | `notes.go:12` | Note is free text kept on a project or on a task: exactly one of TaskID and ProjectID is set. |
| `Preset` | 5 | ✓ | `prompts.go:13` | Preset is a named way to begin a prompt: Prefix is put in front of the prompt text once, and Cwd, when set, is where an agent or a terminal the prompt starts is opened. |
| `Project` | 5 | ✓ | `store.go:23` | Project groups tasks. |
| `QueuedPrompt` | 10 | ✓ | `prompts.go:26` | QueuedPrompt is a prompt kept for later. |
| `Reminder` | 8 | ✓ | `reminders.go:14` | Reminder repeats every Interval seconds while Enabled. |
| `ReminderEdit` | 4 |  | `reminders.go:126` | ReminderEdit holds the fields to change; nil leaves one as it was. |
| `Review` | 13 | ✓ | `reviews.go:19` | Review is a turn read out of history: a prompt the person gave, which the harness reported as a prompt-submitted entry (source agent-activity), and every response-completed entry of the same window until the person's next prompt. |
| `ReviewResponse` | 3 | ✓ | `reviews.go:40` | ReviewResponse is one response-completed entry of a turn. |
| `StashedPrompt` | 3 | ✓ | `stash.go:8` |  |
| `Store` | 15 |  | `store.go:55` | Store is the file's contents plus where it lives. |
| `Task` | 10 | ✓ | `store.go:33` | Task is a task or a subtask. |
| `Theme` | 2 | ✓ | `theme.go:10` | Theme is the colour and glyph a project or task is drawn with. |
| `VisibleNote` | 3 |  | `notes.go:30` | VisibleNote is a note a project or task sees, with how it got there and a name for its owner. |
| `file` · | 13 | ✓ | `store.go:74` | file is the JSON shape on disk. |
| `reviewWindow` · | 3 |  | `reviews.go:82` | window is the pairing state of one window while reviews walks history. |

### internal/ui (69)

| Struct | Fields | JSON | Where | Purpose |
|---|---:|:---:|---|---|
| `Model` | 83 |  | `model.go:88` | Model is the deck's Bubble Tea model. |
| `actionMsg` · | 3 |  | `commands.go:42` |  |
| `assignment` · | 7 |  | `assign.go:153` | assignment is the theme and the text a pane's assignment is drawn with: the task's, or the project's when the pane has only a project. |
| `attachPickedMsg` · | 3 |  | `attach.go:20` | attachPickedMsg carries the files the OS dialog returned for a form's Attach field. |
| `chipsPicker` · | 6 |  | `picker_chips.go:13` | chipsPicker is prototype 6: one input where each segment, once a slash ends it, locks as a chip: the project, then its task, then a subtask. |
| `confirmState` · | 4 |  | `model.go:37` | confirmState is the question modeConfirm asks. |
| `crow` · | 2 |  | `contextpage.go:42` | crow is a line of the context page: a group header or an item. |
| `ctxFilter` · | 4 |  | `contextpage.go:77` | ctxFilter narrows the list. |
| `ctxPage` · | 8 |  | `contextpage.go:22` | ctxPage is the state of the context page: the rows drawn, the selected item by id so it survives a refresh, and how the list is sorted, grouped and filtered. |
| `ctxScopePane` · | 3 |  | `contextpage.go:37` | ctxScopePane names the pane the "current" scope keeps. |
| `detailMsg` · | 7 |  | `commands.go:28` |  |
| `dirPickedMsg` · | 3 |  | `prompts.go:29` | dirPickedMsg carries the folder chosen in the operating system's dialog to the form that asked for it. |
| `field` · | 23 |  | `form.go:30` | field is one line of a form: a label, the text typed so far, and a default used when the person leaves it empty. |
| `fieldEdit` · | 6 |  | `form.go:66` |  |
| `focusScope` · | 3 |  | `focus.go:15` |  |
| `form` · | 16 |  | `form.go:131` | form is a sequence of fields, drawn as a box over the body. |
| `formPreviewMsg` · | 4 |  | `previews.go:44` |  |
| `fuzzyPicker` · | 5 |  | `picker_fuzzy.go:14` | fuzzyPicker is prototypes 1 and 2: every target in one ranked list under (or, two-pane, over) a single query line. |
| `glyphTable` · | 10 |  | `glyphtable.go:18` | glyphTable is the glyph picker: the store's glyph sections as a grid under a preview of the project or task drawn with the highlighted glyph. |
| `helloMsg` · | 2 |  | `commands.go:20` |  |
| `helpGroup` · | 2 |  | `help.go:293` | helpGroup is one header in the help body and the rows under it. |
| `helpRow` · | 3 |  | `help.go:25` |  |
| `historyPage` · | 6 |  | `history.go:15` |  |
| `hotItem` · | 3 |  | `picker_hotkey.go:21` |  |
| `hotkeyPicker` · | 3 |  | `picker_hotkey.go:15` | hotkeyPicker is prototype 4: the nine most used and most recent targets on the digits 1 to 9, and 0 for the single fuzzy list. |
| `keyHelp` · | 5 |  | `help.go:16` |  |
| `listMsg` · | 2 |  | `commands.go:24` |  |
| `live` · | 20 |  | `live.go:32` | live is the deck gone live on one pane: every key the person types goes to the pane through the daemon, and the pane's screen is read back each time it prints. |
| `liveEndMsg` · | 2 |  | `live.go:72` |  |
| `liveErrMsg` · | 2 |  | `live.go:76` |  |
| `liveInput` · | 3 |  | `live.go:55` | liveInput is one item of the input queue: text to type, a key name for send-keys, or a paste. |
| `liveScreenMsg` · | 5 |  | `live.go:62` |  |
| `liveSizeMsg` · | 3 |  | `live.go:68` |  |
| `liveViewMsg` · | 3 |  | `liveview.go:47` |  |
| `massResult` · | 4 |  | `massadd.go:97` | massResult is what an outline added, and the first project and task it named: the first one created, or the first one reused when it created none. |
| `millerPicker` · | 4 |  | `picker_miller.go:14` | millerPicker is prototype 5: a column per level, projects then tasks then subtasks, the path of the highlighted node as a breadcrumb above them. |
| `modalMsg` · | 5 |  | `prompts.go:311` | modalMsg ends an action started from the modal and brings it back. |
| `namesMsg` · | 3 |  | `palette.go:67` |  |
| `outlineLine` · | 3 |  | `massadd.go:31` | outlineLine is one item of a mass-add outline: its line number from 1, its text, and the index of the line it nests under, -1 at the top. |
| `palItem` · | 4 |  | `palette.go:39` | palItem is one entry of the command palette: a deck command or a tuios verb. |
| `palette` · | 4 |  | `palette.go:48` | palette is the command palette while it is open: the query typed so far, every entry, the entries the query keeps, and the highlighted one. |
| `paneViewState` · | 4 |  | `liveview.go:41` | paneViewState is what the deck changed in a pane's session, so it can be undone: the view, and the tiling mode the session had before the deck set the one the view needs ("" when it already had it). |
| `pickBase` · | 4 |  | `pickers.go:56` | pickBase is what every prototype shares: the model, the purpose and the finish it has not reported yet. |
| `pnode` · | 5 |  | `pickers.go:35` | pnode is one thing a picker offers. |
| `presenceMsg` · | 2 |  | `commands.go:38` |  |
| `previewMsg` · | 3 |  | `picker_preview.go:26` |  |
| `previewSpec` · | 4 |  | `previews.go:27` | previewSpec is what a field's Preview returns for one option: the lines worked out at once, and the pane whose recent output follows them. |
| `previewState` · | 6 |  | `previews.go:35` | previewState is the capture the form box is showing. |
| `promptModal` · | 4 |  | `prompts.go:20` | promptModal is the list over the deck that holds the prompt presets or the queued prompts: j/k choose, the letters in its help act on the row. |
| `promptRecovery` · | 5 |  | `promptstash.go:15` |  |
| `protoDef` · | 5 |  | `pickers.go:74` | protoDef is a row of the prototypes page. |
| `protoRun` · | 6 |  | `pickers.go:120` | protoRun is the prototype that is up, and the pane an assign is for. |
| `resultMsg` · | 4 |  | `palette.go:72` |  |
| `reviewPage` · | 6 |  | `reviews.go:16` | reviewPage is a view over history's turns (store.Reviews); the tick rebuilds it while it is open, and only a mark is ever written. |
| `row` · | 5 |  | `model.go:79` | row is a line of the left list: a group header or a pane. |
| `scored` · | 4 |  | `pickers.go:447` | scored is a node the query kept, how well, and which runes of its path matched. |
| `shot` · | 3 |  | `picker_preview.go:20` | shot is a pane's recent screen as a preview read it, kept per window id for as long as the picker is up. |
| `treeLine` · | 4 |  | `massadd.go:255` | treeLine is one line of the outline of the store: its depth, its text, and the project or task it names. |
| `treeModal` · | 5 |  | `tree.go:19` | treeModal is the task tree modal over the deck: the whole project tree of the selected pane, or of the first assigned pane under the selected group header, with a row per project, task and subtask. |
| `treePicker` · | 7 |  | `picker_tree.go:13` | treePicker is prototype 3: the targets as a tree with the first level collapsed. |
| `treeRow` · | 5 |  | `picker_tree.go:23` |  |
| `treeView` · | 7 |  | `picker_treeview.go:20` | treeView is the assign modal and the context filter, the winner of the prototypes: the targets as a tree on the left, every branch open, and the highlighted target's preview on the right. |
| `trow` · | 3 |  | `tasks.go:18` | trow is a line of the tasks page: a project header, or a task at a depth under its project. |
| `turn` · | 7 |  | `turns.go:20` | turn is a row of the Turns tab: a prompt the person gave an agent, with the agent's reply, or a command a shell ran. |
| `tvRow` · | 8 |  | `picker_treeview.go:35` | tvRow is a line of the tree: the node, how deep, whether it has children shown or hidden and whether they are shown, and the runes of its name the query matched. |
| `verb` · | 3 | ✓ | `palette.go:22` | verb is one entry of the daemon's list-verbs: the contract of one call. |
| `verbParam` · | 6 | ✓ | `palette.go:28` |  |
| `verbResult` · | 2 |  | `palette.go:57` | verbResult names the verb whose result the detail pane shows in place of the selected pane, until the selection or the tab changes. |
| `verbsMsg` · | 2 |  | `palette.go:63` |  |

### internal/web (11)

| Struct | Fields | JSON | Where | Purpose |
|---|---:|:---:|---|---|
| `Server` | 11 |  | `server.go:37` | Server holds the daemon client, the store, the event dispatcher and the presence. |
| `contextView` · | 3 | ✓ | `context.go:15` | contextView is an item with its project's name and its task's path, so the page draws a row without looking them up. |
| `paneView` · | 3 | ✓ | `server.go:208` | paneView is a pane with what it is assigned to, by name, so the page draws a row without a second request. |
| `presetIn` · | 3 | ✓ | `prompts.go:16` |  |
| `queuedIn` · | 7 | ✓ | `promptqueue.go:43` |  |
| `queuedView` · | 3 | ✓ | `promptqueue.go:15` | queuedView is a queued prompt with its project's name and its task's path, so the page draws a row without looking them up. |
| `reminderView` · | 2 | ✓ | `reminders.go:10` | reminderView adds the interval as text, as the page and the forms write it. |
| `sessionView` · | 2 | ✓ | `server.go:214` |  |
| `target` · | 4 | ✓ | `store.go:179` | target names what to assign a pane to, by id or by name, the way the TUI's forms take it. |
| `themeIn` · | 2 | ✓ | `store.go:15` | themeIn is a theme as a request gives it; either half may be left out. |
| `verbList` · | 2 |  | `verbs.go:11` | verbList is the daemon's list-verbs answer, kept whole for the page and indexed by verb name so a call can be refused before it reaches the daemon. |

### setup (2)

| Struct | Fields | JSON | Where | Purpose |
|---|---:|:---:|---|---|
| `Item` | 6 |  | `setup.go:25` | Item is one file the deck can put on a machine. |
| `Need` | 3 |  | `setup.go:36` | Need is one precondition of an item. |

A `·` after a name marks an unexported struct.

## 2. What couples them

1. **One `Store` struct that everything mutates.** store.go:55-71 holds ten
   tables behind one mutex; `save()` (store.go:171) marshals all of them on
   every write. ui, web, history, reminders and prompts all write. A history
   batch rewrites projects and tasks; the 35 MB CPU incident came from this.
   Splitting files or moving to sqlite today means touching 73 methods.
2. **`ui.Model` holds everything.** 78 fields across every page and modal
   (model.go:87-188), and 26 message cases in `Update` (model.go:214). A page
   cannot be added, removed or tested without the model.
3. **Forms know the store through the model.** Form fields take closures
   over `Model`: `Options: m.taskOptions, Preview: m.taskPreview, New: "task"`
   (assign.go:107-108). The form type is generic; its uses are not, and the
   `+ add new…` path is a hard-coded jump to the mass-add screen through
   `formStack` (keys.go:82-88), one slot only.
4. **Pages are added by hand in five places.** The `page` enum
   (model.go:47-54) and its `String`, the dispatch chain in keys.go:166-183,
   the `switch m.page` in view.go:80-94, the title-line checks in
   view.go:151-180, the help loop (help.go:27,84), plus palette jumps
   (palette.go:349-408) and the tick (model.go:425-470). 67 references in 16
   files.
5. **Recorders outside the model, wired by hand.** Correct, but the list of
   handlers is code in `dispatcher` (main.go:120-127), reminders is a
   separate goroutine with its own loop, and the TUI and web each declare
   their event types (`ui.EventTypes`, `web.EventTypes`).
6. **No shared pane registry.** Nine `panes.List` calls; the TUI keeps its
   own `m.merged`, web lists per request, recorders list on resync.
7. **Domain rules live in front ends.** Pruning, target rules and status
   text are in ui; web copies some and misses others (table above).
8. **Cross-package reaches.** `panes.HiddenTasks` takes `*store.Store`;
   `capture.Dir` imports store for a path; `context.Item` is an alias of a
   store type, so the extractor's output shape is the storage shape.

## 3. A modular, data-driven redesign

### The unit contract

A unit is a package that owns one concept, its data and its rules. It
exposes a small Go interface, registers its definitions in tables, and talks
to other units only through the bus or through interfaces it was handed.

```js
// what a unit declares at registration; cmd/deck reads these
Unit = {
  id: "context",
  tables: ["context_items"],                       // the store tables it owns, nobody else writes them
  commands: ["context.extract", "context.patch", "context.delete", "context.insert", "context.open"],
  emits: ["context.changed"],                      // deck events, beside daemon events
  listens: ["window-closed"],                      // daemon event types it wants
  pages: ["context"], palette: ["context.extract-here"], routes: ["GET /api/context", …],
  cli: ["deck context list", "deck context extract <pane>"]
}
```

In Go that is roughly `type Unit interface { ID() string; Register(*Registry) }`
plus whatever narrow interface other units need (`Tasks.Resolve(windowID)`).

### The units

| Unit | Owns | Interface others see | Today |
|---|---|---|---|
| daemon | the socket | `Call`, typed verbs | daemon (unchanged) |
| panes | the live pane registry, one listing shared | `All()`, `ByID()`, emits `panes.changed` | panes + 9 list sites |
| store | transactions, backends | `Tx(func(tx))`, per-table repos | store |
| work | projects, tasks, assignments, themes, snoozes, pruning | `Resolve(window)`, `Target(task, project)` | store.go + ui/assign.go + web/store.go |
| context | items, extraction, actions | commands above | context + store/context.go |
| prompts | presets, queue, stashes, dispatch, agent start | `Start(spec)`, `Dispatch(q)` | prompts + ui/web prompt code |
| notes, reminders | their tables, the reminder loop | commands | store files + reminders |
| history, reviews | the history table, turns, marks | `Turns(window)` | history + store/reviews.go |
| captures | files, autocapture | `List`, `Read`, a `Handler` | capture + autocapture |
| config | settings, key actions | `Binding(mode, action)` | config (unchanged) |
| registry | definitions of pages, actions, palette, modals, groupings, routes, features | `All(kind)`, `Status(id)` | the inventory plan's `internal/feature` |
| tui shell, web shell | rendering, view state | consume the above | ui, web |

### Store: per-table, one transaction

```js
// store/schema: each unit declares its table; the store does not know the fields
Table = { name: "context_items", owner: "context", key: "id", version: 1,
          migrate: { 0: "add starred,pinned default false" } }

// a write is a transaction over named tables; only touched tables are saved
tx = store.Tx(["tasks", "assignments"], (t) => {
  t.tasks.put({ id: "t9", status: "done" })
  t.assignments.delete("w3f…")
})
// JSON backend: one file per table under ~/.local/share/tuios-deck/
//   tasks.json, assignments.json, history.jsonl (append-only)
// sqlite backend: one table each, same Tx API
```

Buys: history appends without rewriting tasks; sqlite is a backend, not a
rewrite; a unit's table can change shape without the others noticing.
Costs: a cross-table rule (a task's project via its root) needs a
transaction over two tables, and the "one file, written whole" rule in
AGENTS.md is replaced by "one transaction, written whole per table".

### Command bus: writes

Every mutation is a named command with a JSON-shaped payload, handled by the
unit that owns it. ui, web and a future CLI send the same command.

```js
Command = { name: "prompts.start-agent",
            args: { session: "work", agent: "claude", name: "reviewer", cwd: "~/proj",
                    prompt: "review the diff", preset: "question", task: "t9" } }
Result  = { status: "reviewer (w3f) is ready: idle; prompt sent",
            data: { window: "w3f…", assignment: { task_id: "t9" } } }
```

`POST /api/call` already does this for daemon verbs; the same handler serves
deck commands. The CLI TODO ("one subcommand per concept mirroring the
APIs") becomes `deck <unit> <command> --json '{…}'`, generated from the
command table.

### Event bus: reads

The existing `events.Runner` stays the only daemon subscription. Units also
emit deck events through it (`tasks.changed`, `context.changed`), so the web
page and the TUI both refresh from the same signal instead of each
re-listing.

```js
DeckEvent = { type: "tasks.changed", source: "deck", ids: ["t9"], at: 1760… }
// handlers and taps declare types as today; deck events never reach the daemon
```

### Definitions instead of code

```js
// pages: the TUI and web shells both read this table
Page = { id: "page.context", key: "C", title: "context", unit: "context",
         rail: "contextRail", detail: "contextDetail",    // named render functions
         keys: "context", help: "context", tick: false }

// palette
PaletteCommand = { id: "palette.deck.go-tasks", group: "deck", label: "go to tasks",
                   hint: "the projects, tasks and subtasks page", key: "T",
                   run: { command: "ui.page", args: { page: "page.tasks" } } }

// groupings (o/O): a preset is already a slice; make it a definition with an id
Grouping = { id: "grouping.harness-project", levels: ["harness", "project", "top-task"] }

// modals: one slot, contents replaced; answers the catalog-modal TODO
Modal = { id: "modal.catalog", kind: "list", source: "registry.kinds",
          onPick: { replace: "modal.catalog.<kind>" } }

// forms: fields name a source, not a closure over Model
Form = { id: "form.assign", submit: "work.assign",
         fields: [ { name: "task",    source: "work.tasks",    preview: "work.task", add: "task" },
                   { name: "project", source: "work.projects", preview: "work.project", add: "project" } ] }

// routes: generated for commands and table reads
Route = { method: "PATCH", path: "/api/tasks/{id}", command: "work.patch-task" }
```

| Definition | Data-driven buys | Costs |
|---|---|---|
| Page | a new page is one entry + two render funcs + a key block; help and palette list it for free | render functions are still code; the table only removes the wiring |
| Palette command | palette, help, CLI and inventory read one list | commands that need a form still need the form |
| Grouping | cut ones by review mark, add one with a level list | levels themselves stay code |
| Modal | one slot enforced by the shell, nested catalogs by `replace` | a modal that needs custom keys needs a key table too |
| Form | fields bound to unit sources; ui and web forms from one definition | web forms are hand-written HTML today; generating them is a later step |
| Route | 61 hand routes become ~20 table reads + `/api/call` | URL design is less free-form |
| Harness | already data: omp support is a dotfile (`setup/omp/`) reporting through `set-agent-state` | nothing new; keep it that way |

### The proposed graph

```
                         cmd/deck  (orchestrator)
        builds units, owns startup order, Runner, command bus, store Tx
   ┌───────────┬──────────────┬──────┴───────┬──────────────┬───────────┐
   v           v              v              v              v           v
 tui shell  web shell     cli shell      registry        config      setup
   │           │              │          (definitions)
   └─────┬─────┴──────┬───────┘              ^  units register pages,
         v            v                      │  commands, routes, groupings
   command bus     event bus (events.Runner) │
         │            │                      │
   ┌─────┴────┬───────┴──┬─────────┬─────────┴┬──────────┬──────────┐
   v          v          v         v          v          v          v
  work     prompts    context   history/   captures   notes/     panes
                                reviews    (+auto)    reminders  (registry)
   └──────────┴──────────┴─────────┴──────────┴──────────┴──────────┘
                 │ store.Tx (per-table)          │ daemon client
                 v                               v
           JSON files / sqlite              tuios socket

Shells import the buses and the registry, never a unit's internals.
Units import daemon, store and other units' interfaces, never a shell.
```

## 4. The orchestrator question

Yes. Once units are separate, something has to own:

- startup order: store before units, units before the runner's first dial
  (the subscribe filter is the union of types), taps before `Start`;
- the one daemon subscription and its resync fan-out;
- routing commands to the unit that owns them;
- store transactions that span two units' tables;
- the two front ends agreeing on the same rules;
- shutdown: history's last flush in `Close`, waiting on `Start`'s done.

| | (a) thin orchestrator in cmd/deck | (b) message bus, nobody orders | (c) ui.Model and web.Server orchestrate, shared core |
|---|---|---|---|
| Startup order | explicit, one function | implicit; races on first dial | explicit but twice |
| One subscription | yes, as today | yes, but every unit subscribes itself | yes |
| Who validates a write | the owning unit, via command | the owning unit, if it is listening | the core |
| ui/web consistency | one command path | one path, but async replies | one core, two callers |
| Debugging | read the wiring function; commands are logged | trace across async hops | as today |
| Adds a CLI | a third shell on the same bus | a third subscriber | a third orchestrator |
| Change cost | moderate | high | lowest |

**Recommendation: (a), with (c) as its first stage.** Build the shared core
first (it is (c)), then move wiring out of `ui.New` and `web.Server` into
`cmd/deck` as units get their own interfaces. Commands are synchronous
calls through a dispatch table, not queued messages. The only async path
stays the daemon event stream, which already has the right properties
(bounded queues, resync, per-handler goroutines).

Why not (b): the deck's hard rules are about ordering and the person's view.
A free bus makes "who changed the selection" and "which write landed first"
hard to answer, and the live mode already shows that input order matters
(one goroutine for keys, live.go:288).

Risks and how (a) handles them:

- **Ordering.** Commands are synchronous and the store serialises
  transactions; events are delivered in seq order per handler as today.
- **Debugging.** One command log line per dispatch (`name`, args hash,
  duration, error) gives a trace the TUI can show in the History page.
- **"Nothing resets the person's view on a refresh."** Units never touch
  view state. A `tasks.changed` event makes the shell re-read data and
  re-render; selection and scroll stay in the shell, keyed by id, exactly as
  `tselID` and `cselName` do now. A unit that wants attention emits an event;
  only the shell decides to move anything.
- **One form slot / one modal at a time.** The shell owns a single modal
  slot; a definition can only `replace` the current modal or close it. The
  mass-add `formStack` becomes the one allowed parked modal, enforced in the
  shell rather than by convention.
- **Two-miss pruning.** Moves into the work unit, fed by `panes.changed`, so
  web gets it too. The rule (prune on the second miss) is kept verbatim.
- **Presence.** Stays a shell concern (ui and web hold it differently); the
  transcript reader takes the nonce as an argument.

## 5. Migration order

Each step is one or a few commits, behaviour unchanged unless stated, and
ends with `scripts/e2e-all.sh` green. Smallest first.

| # | Step | Verify | Unlocks (TODO) |
|---|---|---|---|
| 1 | `internal/core`: move the duplicated helpers (previews, preset apply, target resolve, start agent, new terminal, status text) into one package; ui and web call it. Fixes the web status text drift. | e2e.sh, e2e-web.sh | CLI subcommands per concept (one place to call) |
| 2 | Move `pruneAssignments` into core, called by both shells (web gets pruning). | e2e.sh pruning check; add one to e2e-web | stale panes (the same miss counter can drive "stale for N minutes") |
| 3 | Registry (the inventory plan's stage 2): pages, palette, groupings, modals, actions register definitions; help and palette read it. | e2e-help.py, e2e.sh palette checks | searchable help, `deck list-actions`, `config init` with every value, feature inventory review marks |
| 4 | Pages from the table: `page` enum replaced by `Page` definitions; keys.go and view.go dispatch through it. | e2e.sh page switches | tree modal page, new pages without five edits |
| 5 | One modal slot in the shell with `replace`; forms take field sources by name. | e2e.sh forms, e2e-prompt-stash.sh | catalog modal and meta catalog, modal input library (key → action → method fits a modal's key table) |
| 6 | Store: per-table repos behind `Tx`; history to an append-only file; one file per table. | every suite (seeds move to per-table files; e2e seeding helper updated once) | sqlite store (a second backend), CPU from history writes |
| 7 | Command bus: every store mutation is a named command; `/api/call` serves them; routes generated for reads. | e2e-web.sh | CLI per concept generated from the command table |
| 8 | Pane registry unit: one listing shared, `panes.changed` events; the 9 list sites read it. | e2e.sh, e2e-history.sh, e2e-review.py | stale panes threshold, fewer daemon round trips |
| 9 | Wiring moves to cmd/deck: units built there, shells receive interfaces only; reminders becomes an `events`-style unit. | e2e-all | units testable alone by a suite; parallel agents can own a unit each |

Order rationale: 1 and 2 remove drift that users can see and cost nothing
structurally. 3 is already planned and gives the data tables everything
later reads. 4 and 5 are TUI-only. 6 is the largest and waits until the
command paths are few. 7 and 8 need 6's transactions and 1's core. 9 is a
cleanup once nothing else reaches across.

## What this does not change

- The daemon stays the source of truth for panes and harness support;
  harnesses are still added by dotfiles and `set-agent-state`, never tuios.
- No unit tests: each unit is verified by the suites that drive it.
- Colours, live mode, presence and the TUI's look are untouched.
