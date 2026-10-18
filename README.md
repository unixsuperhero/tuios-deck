# tuios-deck

A dashboard over the [tuios](https://tuios.dev) daemon, as a terminal program
(`deck tui`) and as a web page (`deck web`). Both list every terminal and agent
the daemon holds, start new ones with a first prompt, queue prompts, follow
each pane's screen, activity and transcript, and save captures to files, by
hand and on their own when a turn or a command ends. The web page also shows
a pane live, in xterm.js, and types into it.

The deck also keeps a record of its own: projects, the tasks and subtasks
under them, which pane works on what, and the context items found in the
panes: links, pull requests, files, folders, prompts and commands. Every
agent or terminal it starts is assigned to a task or a project, the list can
be grouped by them, a tasks page manages them, a captures page reviews the
capture files, a context page keeps the items, and a command palette
reaches every verb the daemon has.

Neither owns a PTY. Everything goes through the daemon's JSON verb protocol
on its unix socket, so the panes persist when the deck quits and are the
same panes you see in `tuios attach`.

```
 tuios-deck tuios 1.4.0  /run/user/1000/tuios/tuios.sock                            1 need you  3 agents
▾ work                                      │ 1 Screen   2 Activity   3 Transcript   4 Turns   work › reviewer
  AI ◐ deck command palette  reviewer working│ you> review the diff on this branch
  AI ● deck captures page  tests needs_input│ → Bash go test ./...
  $  · !!!! build                           │   ok Bash
▾ side                                      │ ■ turn ended
  AI ○ web  landing  helper      idle claude│
captured to ~/.local/share/tuios-deck/captures/work-build-20261017-133455.txt
 F1 keybindings
```

## Install

Needs Go 1.26 and a running tuios daemon (`tuios start-server`, or any
session).

```sh
go install github.com/unixsuperhero/tuios-deck/cmd/deck@latest
deck update          # later: install the newest published version the same way
deck update --check  # only say what is running and what is published
deck version
```

`deck update` runs `go install` for you and reports where the binary landed
(`$GOBIN`, else `$GOPATH/bin`). `deck version` shows the module version Go
stamped into the binary, so a release needs no version number in the source.

## Build from the repository

```sh
go build -o deck ./cmd/deck
./deck                       # prints the commands
./deck tui                   # $TUIOS_SOCKET, or the per-user runtime folder
./deck tui --socket /path/to/tuios.sock --captures ./captures
./deck web                   # http://127.0.0.1:8787
./deck web --listen 127.0.0.1:9000 --socket /path/to/tuios.sock
./deck tui --data ~/deck.json  # projects, tasks, assignments and context items; web takes it too
./deck tui -auto-capture=false  # no automatic captures; web takes it too
./deck setup                 # the files the deck needs on this machine
```

`deck tui` and `deck web` share `-socket`, `-captures`, `-data` and
`-auto-capture`; `deck web` adds `-listen` and `-allow-remote`.

## Setting up a machine

Some of what the deck shows needs a file outside its own binary. `deck setup`
carries those files, embedded, and puts them in place:

```sh
deck setup list                    # what there is, and where each file goes
deck setup status                  # missing, current, outdated or foreign, per file
deck setup install                 # write them all; --force replaces a foreign file
deck setup uninstall omp-activity  # remove what the deck wrote, nothing else
deck setup print omp-activity      # show the file before installing it
```

Every file the deck writes carries a `TUIOS_DECK_SETUP_ID` marker, so a file
of the same name that the deck did not write is reported as foreign and never
overwritten or removed without `--force`. The sources live under `setup/` in
this repository.

| Item | File | Does |
| --- | --- | --- |
| `omp-activity` | `~/.omp/agent/extensions/tuios-deck-omp-activity.ts` | An [oh-my-pi](https://github.com/can1357/oh-my-pi) extension that reports the prompt typed, each tool call starting and ending, and the end of a turn to tuios through `tuios call set-agent-state`. With it, `tuios agent-activity` and the Activity tab show an omp pane the way they show Claude Code. It sits beside the state extension `tuios integration install omp` writes and does not replace it. |

The omp extension needs `tuios` on `PATH` (or `TUIOS_DECK_TUIOS` naming it)
and takes effect in omp sessions started after the install, inside a tuios
pane. Transcripts for omp are a different matter: the daemon serves
`agent-transcript` only for formats it has a reader for, and today that is
Claude Code's. No file outside tuios adds a reader.

## Settings

Settings live in one JSON file, `$XDG_CONFIG_HOME/tuios-deck/config`
(`~/.config/tuios-deck/config` when `XDG_CONFIG_HOME` is unset), apart from
the data store. A missing file means the defaults. A file that does not
parse, holds anything after its one JSON document, or names a setting that does not exist, stops `deck tui` at startup
with the file's name and the reason; nothing is silently ignored. The file is
written whole through a temp file and a rename.

```json
{
  "vim_modes": true,
  "keys": {
    "deck":        { "quit": "Z", "close": "w" },
    "form_normal": { "next_field": "n" },
    "form_insert": { "leave_insert": "ctrl+g" }
  }
}
```

`deck config path|show|get|set|init` reads and changes it from the shell;
`deck config keys` lists every action with its default and current key, and
`deck config bind <mode> <action> <key>` / `unbind <mode> <action>` change one.
In the TUI the palette's `settings…` form does the same (vim modes on or
off, and one rebinding at a time; an action with an empty key goes back to its
default) and saves the file; the change applies at once.

### Key bindings

Press uppercase `R` to reset the current page's view controls. The TUI action
is `reset_view` in mode `deck` and can be rebound with `deck config bind`.
The web Tasks, Context, History and Review pages also have a native **Reset**
button; their `R` shortcut stays inactive in text fields, dialogs and Live.

- Context returns to all panes, empty filters, no groups and newest first.
- Tasks clears the Saved easy tasks filter, not the saved easy marks, and
  restores the project/task hierarchy within the current focus scope.
- Web History clears its search and event-kind filters. TUI History has no
  filters to reset.
- Review returns to unread prompts, oldest completion first. It closes the
  opened detail without opening or marking another prompt reviewed.

Reset never deletes data or unlocks focus. A selection that remains visible
is retained. Captures, TUI History, reminders and the prototype page have no
page-level filters or sorting controls to reset. Background refresh does not
run reset or intentionally move the selection or scroll.

Captures, History and Context default to newest first. Equal Context creation
seconds use reverse append order; starring, pinning or re-extracting an existing
item does not make it new. Review completion order and task hierarchy are not
chronological-list defaults and stay unchanged.

Press `F1` anywhere in the TUI to open the current mode’s keybindings:
the focused page or detail area, prompt, saved-prompt picker, form, palette,
confirmation, picker, or focus buffer. Footers show only `F1 keybindings`
instead of a horizontal list. `?` on a page and the palette’s
`keybindings and actions…` command still work. Palette-triggered help uses
the page that opened the palette.
Tables show the current configured keys, action names and scopes. `tab` / `shift+tab` or
left/right select another mode, including vim form normal/insert, all page-local
keys, form editing, modal navigation, or all palette actions. A tab bar under
the mode line names every mode; when they do not fit, it shows the current
one and its neighbours with `…` for the rest. Within a tab, rows sit under a
header per scope: keys for every page first, the deck page's motion keys
together under `deck page, motion`, daemon verbs last and the rest
alphabetically. The pages tab has one header per page, in page order, and a
page's own tab lists that page's keys before the global ones.

`/` searches every mode at once. Type on the query line at the bottom of the
box; rows whose key, action or scope contain every word of the query
(case-insensitive substrings) are listed under a header for the mode they
come from. A row shared by several modes is listed once, under the first,
and the per-page tabs are left out because the pages tab holds their rows.
Up/down and PageUp/PageDown scroll the results while typing; `enter` keeps
the filter so `j/k` and the other scroll keys work, and `/` edits it again.
`esc` clears the query and returns to the tabs, as does `backspace` on an
empty query; changing tabs clears it too. In the actions
table, `u` limits the list to
commands without direct shortcuts, including daemon verbs. Empty results
in another mode mean every listed action has a binding; they do not disable
keys. `unbind` still restores an action's default key.

Use up/down or `j/k` to scroll, PageUp/PageDown for a page, and Home/End
for the ends. Help is centered with at least five blank rows above and below
and five columns on either side. Below 42 columns or 18 rows, gutters shrink
to preserve a usable table; very small terminals must clip the help. Browser
help uses five line-heights vertically and five character-widths horizontally,
with the same small-viewport fallback and a scrollable table. `F1` or `esc`
closes TUI help without changing the underlying draft, filter, selection or
scroll. The configurable `help` action can still be rebound; `F1` always
works, including in text fields. In vim normal mode it shows normal-mode
keys; in prompt insert mode it shows prompt editing and recovery. `?`
remains text in inputs, and picker previews keep their existing key.

The web header's **Help ?** button and `?` shortcut open the browser
version at the current page, including when invoked through the palette.
Select a page, Forms, Palette, or Focus and buffer mode and check
**Unbound only** to inspect its tables. Page tables distinguish browser
controls from TUI shortcuts, including Review and History navigation.
The TUI pages table also includes Review and task easy-marking keys; its
modals table documents the focus buffer. Help describes the scoped workspace
and the buffer's fixed Enter continuation, Escape/F unlock and timeout.
Browser help lists browser shortcuts, not the configured TUI bindings. Page keys
do not run in inputs, dialogs, or the focused Live terminal. The palette
shortcut, Ctrl/Meta+k or Ctrl/Meta+p, also works in inputs and Live when
no dialog is open, including during focus. Browser forms retain native Tab
and Escape navigation outside the focus buffer.

`GOWORK=off go build -o .dev/deck ./cmd/deck` followed by
`python3 scripts/e2e-help.py` checks help against isolated throwaway daemons
and keeps screen captures under the printed scratch directory.

`keys` maps a mode, then an action, to a key. An action not listed keeps its
default, so the file only holds the differences. A key is one character, a
name (`tab`, `shift+tab`, `enter`, `esc`, `up`…) or `ctrl+a` / `alt+a`. A
file is refused, at startup and on save, for an unknown mode or action, a
key that is not a key, a reserved key (`1`-`4`, arrows, `esc` and `ctrl+c`
on the deck, for example), or two actions of one mode on the same key. A key
whose action moved elsewhere does nothing; swapping two keys is allowed.

| Mode | Where | Actions |
| --- | --- | --- |
| `deck` | the dashboard; `help` `quit` `palette` `tasks` `captures` `add_many` `context` `review` `history` on every page, the rest on the deck page only | `help quit palette tasks captures add_many context review history left right down up top bottom scroll_down scroll_up next_tab prev_tab refresh group_next group_prev new_agent new_terminal prompt assign capture extract close live` |
| `live` | the deck live on a pane; `leader` is the prefix (`ctrl+a`), the rest are the keys after it | `leader detach zoom zoom_pane fullscreen_pane next_pane prev_pane help` |
| `form_normal` | forms, vim modes on, normal mode | `insert append next_field prev_field clear submit cancel` |
| `form_insert` | forms, vim modes on, insert mode | `leave_insert` (never a printable key, so no key that types can leave the mode) |

### Vim modes

With `vim_modes` off (the default) forms behave as described under Forms:
every key types, `esc` cancels. With it on, a form opens in **insert** mode,
where every key types exactly as before, and the box names the mode. `esc`
first closes an open `@` path list, then goes to **normal** mode. In normal
mode nothing types: `i`/`a` return to insert, `j`/`k` move between fields,
`D` empties the field, `enter` submits, and `esc` cancels the form (or goes
back to the parked form after `+ add new…`). Any other key is ignored, and a
paste is ignored too. In either setting, `esc` on a form whose fields you
have changed asks first, `y` discards and `n` keeps the form; an untouched
form closes at once.

## Projects, tasks and assignments

A project holds top-level tasks; a task holds subtasks, to any depth, in
the same table. A subtask stores no project of its own: its project is its
root task's.

A project's or a task's status is `open`, `done`, `skipped` or `planned`.
Done and skipped are final: a final task no longer counts as open, and a
final project or task no longer holds its colour. A planned item is not
started yet: it keeps its colour, but it does not count as open either.
The tasks page marks a done row `✓`, a skipped one `⊘` and a planned one
`◌`. A project written before it had a status reads as open.

### Snoozing and the attention rail

Press `z` on the TUI deck to snooze the selected pane's task until the next
midnight in the deck process's local timezone. On the Tasks page, `z` toggles
snooze for the selected task. In the browser, select a task on **Tasks** and
use **Snooze until midnight** or **Unsnooze**. The deadline survives restarts;
expiry restores eligible panes automatically without resetting filters or scroll.

Snooze hides that task's panes and its descendants' panes from the deck's
left rail in every grouping. It does not change status, stop processes, or
remove tasks from the Tasks page. A descendant's own snooze remains in effect
when its parent is unsnoozed.

The rail also hides a task's panes while every live pane assigned directly
to that task is working. One idle, done, errored, unknown, or needs-input pane
keeps the whole task visible. Agent state comes from the daemon. A terminal
counts as working only when OSC 133 command marks confirm a running command;
a terminal without those marks stays visible. Unassigned and project-only
panes are not hidden by this rule.

Hidden panes remain listed in task details. Opening one explicitly still works;
in the TUI it is temporarily revealed until selection moves elsewhere.
Tasks with no panes remain on the Tasks page, as before.

The API accepts `PATCH /api/tasks/{id}` with `{"snoozed": true}` or
`{"snoozed": false}`. Task responses contain `snoozed_until`, a Unix-seconds
deadline, or zero when cleared. `GET /api/state` retains every pane and adds
`rail_hidden` to each one; `rail_wake` is the next snooze expiry, or zero.

### Themes

Every project and every task has its own theme, one of the 16 ANSI colours
and one glyph, drawn before its name in the list, on the tasks page and on
the web page. A new one takes a colour at random from the free ones, those
no open project or task holds, with `◆` for a project and `●` for a task,
until you change it. When every colour is held it takes any of them; black
and bright-black are never given out on their own. `m` on the tasks page
steps through the free colours first, then the rest of the palette.

`M` on the tasks page opens the glyph table over the page: about 120
symbols in labelled sections (shapes, arrows, marks), then the letters and
the digits, laid out by display width so a wide glyph keeps the columns in
line. A preview above the grid draws the row with the highlighted glyph in
its colour, as the rail's group header and as the tasks page shows it:

```
╭─ glyph for task command palette ───────────────────╮
│ rail   ◉ deck › command palette                    │
│ tasks    ◉   command palette                       │
│ ◉ · shapes                                         │
├────────────────────────────────────────────────────┤
│ shapes                                             │
│  ◆   ◇   ◈   ●   ○  [◉]  ◎   ◌   ◐   ◑   ◒   ◓     │
│ …                                                  │
│ letters                                            │
│  A   B   C   D   E   F   G   H   I   J   K   L     │
╰────────────────────────────────────────────────────╯
```

`h`/`j`/`k`/`l` or the arrows move, `enter` picks and `esc` cancels.
Typing a character jumps to it (a lower-case letter to its capital; type
`H`, `J`, `K` or `L` for those, since the lower-case ones move). A typed
glyph the table does not hold, an emoji or a letter with an accent, joins
the end as a custom entry, so any single glyph is still possible. The
glyph field of the `e` form opens the same table on `tab` or `enter` while
it is empty, or on `ctrl+g` at any time, with the current glyph as its
default; typing a glyph there still works.

Every agent and terminal the deck starts is assigned to a task or, failing
that, to a project: the `n` and `t` forms ask for one after the cwd, with
the known task paths (`project / top task / … / title`) and project names
listed under the field, and refuse a pane with neither. A task implies its
project. In TUI and web modals, choosing a task fills the following project
field, including the project inherited by a subtask. Changing the task
updates the project; choosing a projectless task clears it. Prefilled tasks
also fill their project. The project field remains editable.
`a` assigns, or reassigns, a pane already open through the assign
tree (below); the palette's "clear assignment" drops it. The assignment of a pane that is gone from two
listings in a row is dropped on its own.

The record is one JSON file, `$XDG_DATA_HOME/tuios-deck/deck.json`, else
`~/.local/share/tuios-deck/deck.json`. `TUIOS_DECK_DATA`, or the `-data`
flag of `deck tui` and `deck web`, names another file. It is written whole,
through a temp file and a rename, so it can be read or copied at any time:

```json
{
  "version": 1,
  "projects": [
    { "id": "p_8f3a1c", "name": "deck", "theme": { "color": "blue", "glyph": "◆" }, "status": "open", "created": 1760000000 }
  ],
  "tasks": [
    { "id": "t_1a2b3c", "parent_id": "", "project_id": "p_8f3a1c", "title": "command palette",
      "theme": { "color": "magenta", "glyph": "●" }, "status": "open", "created": 1760000000, "updated": 1760000000 },
    { "id": "t_9c0d4e", "parent_id": "t_1a2b3c", "project_id": "", "title": "verb autocomplete",
      "theme": { "color": "green", "glyph": "●" }, "status": "skipped", "created": 1760000001, "updated": 1760000001 }
  ],
  "assignments": {
    "98db8226-1829-468e-89a8-41a2baa0ddab": { "session": "work", "task_id": "t_9c0d4e", "project_id": "" }
  },
  "context_items": [
    { "id": "c_4d5e6f", "kind": "pr", "text": "https://github.com/unixsuperhero/tuios-deck/pull/12",
      "session": "work", "window": "98db8226-1829-468e-89a8-41a2baa0ddab", "pane": "reviewer",
      "project_id": "p_8f3a1c", "task_id": "t_1a2b3c", "starred": true, "pinned": false, "created": 1760000002 }
  ],
  "notes": [
    { "id": "n_7a8b9c", "text": "ship after the palette lands", "task_id": "t_1a2b3c", "project_id": "", "created": 1760000003, "updated": 1760000003 }
  ]
}
```

Context items live in this same file, under `context_items`, not in a
separate store; the `-data` flag and `TUIOS_DECK_DATA` move them with the
rest. The deck keeps JSON for now: the file is
read whole at start and rewritten whole under one process-local mutex, and a
plain file can be read, diffed and copied by hand. SQLite would be worth
its dependency if the store grew past what a rewrite per change tolerates,
or if two deck processes had to write at once. Concurrent writers are a
known limit: two deck processes on one file (say `deck tui` and `deck web`)
each hold their own copy from start, so the later write wins; point them at
different `-data` files, or run one.

Deleting a task takes its subtasks with it, deleting a project takes its
tasks, and either takes the assignments that pointed at any of them. A
context item that pointed at one keeps its place and loses the reference.
Notes are the exception: they belong to their project or task and go with it.

### Notes

A note is free text on a project or a task (`notes` in the store: one of
`task_id` and `project_id`). Notes are shared along the tree, not copied: a
project or task sees its own notes, those of its ancestors (the task chain
above it and its project) and those of its descendants (subtasks at any
depth; a project sees every task's). Siblings and cousins are not related
and do not see each other's notes. A note stores only its owner, so moving a
task moves what it sees with it. Only a note's owner edits or deletes it;
the tasks page detail lists the ones it sees, own first, an inherited one
marked `from <owner>` or `under <owner>`. In the TUI, select a row on the
tasks page and use the palette: `add note` (a multi-line form, `ctrl+s`
saves), `edit note…` and `delete note…` (pick one of the row's own notes).
On the web page the tasks page's detail has a Notes section with add, edit
and delete.

## Prompt presets

A preset is a name, a prefix and, optionally, a working directory. Choose
one in the `preset` field of the prompt form (`p`), the new agent form (`n`)
or the new terminal form (`t`), in the TUI or on the web page. The prefix
goes in front of the prompt once, with a space between them unless the prefix
ends in whitespace, and only when there is a prompt. The preset's directory
is where a new agent or terminal starts, unless the `cwd` field is filled in.
Leave the field empty and nothing is added.

Presets are made and kept from the command palette: `prompt presets…` opens
a list (`n` new, `e` edit, `d` delete, `j`/`k` choose). No preset exists
until you make one. The list's `x` adds the example "short answer"
preset (`Answer this question in less than 10 words:`), and nothing else
adds it. In the preset form, `ctrl+o` opens the operating system's folder
dialog for the `cwd` field (macOS `osascript`, else `zenity` or `kdialog`); on
the web page the field has a Browse button. Presets live in `deck.json`
under `prompt_presets`.

On the web, the `preset` field of `POST /api/agents`, `/api/terminals` and
`/api/panes/{session}/{id}/prompt` names a preset, and the server applies it.

## Queued prompts

A prompt can be kept for a task or a project and started later. In the
command palette, `queue prompt…` (TUI and web page) asks for the task or
the project (the row selected on the tasks page is offered), a preset, the
agent, session and cwd, an optional running agent to send it to, and the
prompt. The preset's prefix goes into the stored text then, once, and its cwd
fills an empty cwd; the queue keeps the final text, so starting it adds
nothing. The cwd field has the folder dialog (`ctrl+o` in the TUI, Browse
on the page).

`queued prompts…` opens the modal: `j`/`k` choose, `enter` starts, `n`
queues another, `e` edits, `d` deletes, `esc` closes. Starting a prompt that
names a pane sends it there through `queue-prompt`. Otherwise it starts the
agent in the session at the cwd with the prompt, through `start-agent`, and
assigns the new pane to the prompt's task or project. A prompt leaves the
queue only when the daemon reports the agent ready and the prompt `sent`. An
error, a refused start, an agent that is not ready, a pane that is gone,
a prompt the daemon calls `held` or `stalled`, is shown and the prompt stays
queued: after a `held` or `stalled` one, look at the new pane before starting
it again, because the text may already be in it. Deleting a task or a project
deletes its queued prompts. They live in `deck.json` under `queued_prompts`.

## Review queue

Press `V` in either frontend, choose **Review** in the web navigation, or
run **go to review queue** from the command palette. The configurable TUI
action is `review`. Completed prompts are ordered by completion time,
oldest first, rather than by when they started.

Entering the page and highlighting a row do **not** open it or mark it
reviewed. Press Enter on the highlighted row (or click **Open** on the web)
to display its prompt, reported response and available pane output, and
persist its first reviewed timestamp. The opened detail stays visible when
the item leaves the pending list; nothing opens the next item automatically.
TUI `tab` switches between pending and reviewed history, `j/k` highlights,
`J/K` scrolls the opened detail, and `r` re-reads the list. The web uses the
**Awaiting review / Reviewed history** selector. A re-read preserves the
highlight, opened detail and scroll. Reopening history keeps the original
reviewed timestamp.

The queue is a view over [history](#history): a turn opens with a
`prompt-submitted` entry the harness reported for the person's prompt (the
daemon's `prompt` activity, including OMP hook reports) and takes every
`response-completed` entry of the same window until the person's next
prompt or a `session_start`. A turn has more than one response when
subagents woke it: Claude Code reports each wake-up as a prompt whose text
is `<task-notification>`, and such a prompt continues the turn instead of
opening one. The row shows `×3` for a turn with three responses, and the
detail lists them in order, the first ones marked interim and the last one
last. A turn is listed once it has a response, is not waiting for one, and
no subagent the window started is still running (a `subagent_start` without
its `subagent_stop`); until then the queue header counts it as still
running. Nothing polls the daemon for turns, and nothing about a turn is
stored twice: the list is computed from history on every read, and the only
review state is the mark, in `deck.json` under `review_marks` (the prompt's
history id → first reviewed time, ms; a mark an earlier deck keyed by a
response's id moves onto its turn). A prompt the
deck sent but the harness never reported, a queued-only prompt, a running
prompt, a command, a state change and an orphan turn-end are not review
items. Activity text can be truncated by the daemon or harness.

Opening a turn attaches the capture [auto-capture](#captures) took when it
ended: the newest `-turn` capture of that session and pane taken at or after
the completion and before the window's next prompt. With auto-capture off,
or for a turn that ended while no deck ran, the detail says no capture was
taken. A turn outlives its pane and a frontend restart as long as history
holds it (history keeps the newest 10,000 entries; a mark whose turn has
left history is dropped). Markdown/HTML documents and MCP are not review
sources.

## Context items

A context item is something worth keeping that a pane held: a `link`, a
pull request (`pr`), a `file`, a folder (`dir`), a `prompt` the person gave
an agent or a `command` a shell ran. `i` on the deck page, or Extract
context on the web page, reads the selected pane's scrollback, its agent's
activity, its transcript when the deck holds the person's presence, and its
shell's command history, and stores what it finds that is new:

- http and https URLs are links; a GitHub pull request or issue URL is a pr
  instead, as is `owner/repo#123`, and a bare `#123` when the same text also
  holds a pull request URL, which names its repository.
- Paths, absolute, `~/` or relative to the pane's cwd, are kept only when
  they exist on this machine, as a dir or a file by their absolute path.
- Each prompt and each command line is an item of its own, and is searched
  for links and paths too.

Rows a terminal wrapped are joined again before the scrollback is
searched, so a long link stays whole. An item's text is cut at 500 bytes,
and one with the kind, text and window of an item already kept is not
added twice. The status line says how many were new and how many found.

The context page has two scopes. `all panes` lists every item; `current
pane` lists only the items of the pane that was selected on the deck when
the page was opened, and remembers that pane until another is selected and
the page is opened again. The title (and, on the web, a line over the list)
names the scope. `t` toggles it in the TUI, the scope select on the web
page does, and so does the command palette entry "context scope". With no
pane selected the current scope says so instead of showing an empty list.
Filters, sort, groups, stars and pins apply inside either scope. On the web
the list is `/api/context?session=S&window=W`.

Each item may be starred, pinned and assigned to a task or a project, the
way a pane is.

## Recurring reminders

A reminder has a title, an optional message and an interval (`90s`, `30m`,
`2h`, `1d`, `1h30m`; a bare number is minutes). It repeats every interval
from when it was added or last enabled. Manage them from the command palette
(`:`): `new reminder…`, `edit reminder…` (an empty field keeps the value, `-`
clears the message), `enable reminder…`, `disable reminder…`,
`delete reminder…` and `reminders` (the list). The web page has a Reminders
page and the same palette entries; the API is `GET/POST /api/reminders` and
`PATCH/DELETE /api/reminders/{id}` (`title`, `message`, `every`, `enabled`).

**Reminders fire only while `deck tui` or `deck web` is running.** Nothing is
installed to run in the background. Each due reminder is delivered as a
notice in the daemon's most recently active session (read with
`tuios read-agent-messages`, shown by a tuios client) and as an OS
notification (`osascript` on macOS, `notify-send` on Linux; the text is
passed as an argument, never through a shell). `-reminders=false` turns the
scheduler off and `-reminders-os=false` the OS notification. Reminders live
in `deck.json`, with their next due time. One missed while the deck was off
fires once when it starts, and the next one is a whole interval on the
original schedule. Run only one deck on a store, or each fires its own copy.

## Captures

`c` writes the selected pane's screen and scrollback to
`<session>-<pane>-<timestamp>.txt` under `$TUIOS_DECK_CAPTURES`, else
`$XDG_DATA_HOME/tuios-deck/captures`, else
`~/.local/share/tuios-deck/captures`, beside the deck's `deck.json`.

Both `deck tui` and `deck web` also capture a pane on their own when an
agent's turn ends (working to idle, needs_input, done or errored) and when
a shell command finishes under shell integration, as
`<session>-<pane>-<timestamp>-turn.txt` or `-cmd.txt` in the same folder. A
window is captured at most once every two seconds, a turn winning over a
command that ends in the same gap, and not again while its screen is
unchanged. This runs beside the front end and never touches its view:
`deck web` logs each capture, `deck tui` says nothing. `-auto-capture=false`
turns it off.

The captures page (`C`) lists the folder newest first, with the kind of an
automatic capture after its session, shows a file and deletes one; the web
page has the same.

## History timeline

Open History with `H` in either front end, or choose `go to history` in the
command palette. The TUI's configurable `history` action appears in
`deck config keys` and the in-app `?` keybinding/action catalog. The web page
also has a History navigation button and lists `H` in its `?` catalog.
Entries are newest first, with source time, observation time, session, window,
source identity and detail. They remain in `deck.json` after panes close and
after the deck restarts. Re-reading retained sources does not duplicate entries.

The timeline records:

- Prompt hook reports and completed response hook reports from `agent-activity`.
  A completion's text is the harness's turn-end report, which may only be a
  summary or may be empty. It is labeled accordingly, not presented as a full
  transcript.
- Shell command submissions and completions from OSC 133 events and
  `command-history`, including reported exit status, duration and working
  directory. Sending text to a terminal is recorded separately as input, not
  proof that a shell executed a command.
  Live starts and retained submissions are labeled as separate source observations.
  The start event does not carry the command sequence, so the deck does not guess
  their identity by matching text or rounded timestamps.
- Successful deck prompt acknowledgements, including prompts dispatched from
  the saved queue and initial prompts for new agents. Daemon queue acceptance
  is labeled separately from confirmed delivery. A later harness prompt report
  is another source observation, not a second deck submission.
- Observed pane and session creation/closure, pane exit, workspace moves,
  minimize/restore, agent state changes and activity session/subagent events.
  These include actions performed outside the deck too. Retitles are not
  recorded: harnesses animate pane titles, a spinner frame per event, and
  they once made up 98% of a store's history.

On the TUI, `j/k` or arrows select, `g/G` select first/last, `J/K` or
Page Down/Up scroll the detail, `r` refreshes, and Enter goes to the source
pane if it is still open. Escape returns to the deck. The web page adds text
search and event-kind filtering; `j/k`, `g/G`, `r` and Enter work there
when focus is outside an input. Both keep the selected event and the detail
scroll when new events arrive.

Recording runs while the deck runs, even with `-auto-capture=false`. On
startup, and whenever the daemon reports a gap in its event stream (a restart
included), it reads each live pane's retained activity and command
history; a short disconnect is resumed from the last event seen and reads
nothing again. Those rings are bounded: activity retains 256 entries; command history
retains 200 commands and collapses repeat command lines to their latest run.
Closed panes, expired entries, commands without shell integration and prompts
never reported by a harness cannot be backfilled. Live repeat commands are
kept separately. Stream gaps are recorded when the daemon reports them; an
offline interval is not claimed to be complete. Existing capture files are
screen snapshots, so they are not treated as proof of a submission or response.

The deck stores submitted prompt text, including text that the daemon has not
masked, in the same private 0600 store as queued prompts. History currently
has no retention limit. Use one deck process per data file; the store's mutex
coordinates goroutines within a process, not separate TUI and web processes.

## Transcript needs the person's presence

The transcript tab reads `agent-transcript`, which the daemon only serves to
the person, through `attach-presence`. The daemon refuses that call from a
process inside one of its own panes. Run `deck tui` or `deck web` from a
terminal outside tuios, or inside a pane of a different daemon, to read
transcripts. Screen and activity work from anywhere. `deck web` asks again
when the daemon drops the presence, as it does across a restart. Without
presence the Turns tab reads an agent's prompts from its activity instead,
and context extraction leaves the transcript out.

## Focus mode

Press `F` in the TUI or use **Focus…** in the web header. On the tasks
page this locks to the selected task or project; elsewhere it opens a tree
with tasks nested under their project and subtasks under their actual parent.
Tasks without a project remain at the root. Select any project or task; a
task includes all descendants, and a project includes all its tasks.
Use Up/Down to move, Right to expand or enter a branch, Left to collapse
or move to its parent, and Enter to start focus. Escape cancels. In the TUI,
`j/k` and `h/l` also navigate; `/` filters while keeping ancestor branches,
and Escape clears an active filter first. The browser also supports clicking
a row and **Start focus**.
The pane rail and task tree show only the selected scope. Focus keeps work
tools available: prompt a pane, send terminal commands, start agents and
terminals, queue requests, edit tasks and notes, or use the command palette.
New terminal, agent and queued-request forms default to the focused task or
project, even when it has no panes yet. You can edit those defaults; a pane
assigned outside the scope will not appear in the focused rail.
The TUI binding is the configurable `focus` action. Focus belongs to this
TUI or browser tab, not other clients; it is a distraction filter, not access
control. Other pages remain available. Refreshes keep the selected pane and
scroll position. **Unlock focus** or `F` releases the scope. Browser shortcuts
do not intercept typing in the Live terminal; use **Unlock focus** in the
header there.
Focus adds a one-character border around the TUI and a thin perimeter border
in the browser, using the scope's colour. Backgrounds, text colours and
terminal ANSI output keep their normal appearance.
Focus unlocks after three minutes without user input. Keyboard, paste and
received mouse input count in the TUI; trusted keyboard, pointer, wheel,
touch and input events count in the browser, including the Live terminal.
Daemon events, detail polls and the clock itself never extend the deadline.
After fifteen consecutive completed one-minute intervals with user input in
each, a thirty-second buffer asks whether to continue. **Enter** (or the
browser’s **Continue focus**) starts a fresh cycle without moving selection
or scroll. Other input does not extend the buffer; expiry or **Unlock now**
releases focus. A minute without input breaks the streak. The clock starts
when focus begins; these are elapsed intervals, not wall-clock minutes.

### Focus plan: saved easy tasks

On the tasks page, `E` toggles a task’s saved easy flag. `ctrl+f` opens
(or clears) the **saved easy tasks** filter; it is the configurable
`focus_plan` action. Select an easy task and press `F` to focus it and all
its descendants, including descendants not themselves marked easy.

In the browser, select a task and use **Mark easy** / **Remove easy flag**.
**Focus plan** opens the saved list; **Saved easy tasks** toggles its filter.
Select a row and use **Focus…** to start. Easy flags survive restarts in
`deck.json`, are independent of task status, and are never inherited by
children. The task API exposes `easy`; `PATCH /api/tasks/{id}` with
`{"easy": true}` or `{"easy": false}` updates it.



## The TUI

The deck page is the one at the top of this file:
the panes on the left, the selected pane's screen, activity, transcript or
turns on the right. On every page `T` opens the tasks page, `C` the
captures page, `I` the context page, `H` the history timeline, `U` the
prototypes page, `V` the review queue, `A` the mass-add screen, `:` or
`ctrl+p` the command palette, and `q` asks and then quits; `esc` comes back
to the deck.
The title line names the page and, on the deck, the current sort and the
grouping when it is not by session; on the context page, its sort, grouping and filter.

### The deck page

Each pane's row in the left rail opens with its kind, a bold cyan `AI`
for an agent or a dim `$` for a terminal, then carries its state dot, the
first four letters of its project in the project's colour, its task's title
in the task's colour, its name, and its state flush right. An agent's
harness follows the state, dimmed, when the rail has room for it:

```
  AI ○ web  landing  helper      idle claude
  $  · deck build                       zsh
```

A narrow rail drops the harness first, then cuts the task title, then the
name. A pane assigned to nothing shows `!!!!`
in place of the project, and its name, in bold black on red, so it stands
out until `a` gives it a home.

The focus is on the rail or on the main area; `h` and `l` move it. The
selected pane is in reverse video while the rail has focus and keeps a dim
`▸` while the main area has it, when the tab bar brightens and underlines
the shown tab. `F1` opens the keys for the focused area.

With the rail focused:

| Key | Does |
| --- | --- |
| `j` `k` (`↓` `↑`), `g` | Select a pane, or the first one |
| `{` `}` | The first pane of the previous or next group at the same depth; stops at the ends |
| `(` `)` | With nested groups: `(` makes `{` `}` jump between the outer groups and selects the first pane of the one holding the selection; `)` steps back in a level. The status line names the level `{` `}` now jump by |
| `l` (`→`) | Focus the main area |
| `1` `2` `3` `4`, `[` `]`, `tab` `shift+tab` | Screen, activity, transcript or turns of the selected pane |
| `J` `K` (`ctrl+d` `ctrl+u`, `pgdown` `pgup`), `G` | Scroll the detail half a page; `G` follows the newest output again |
| `s` | Cycle rail sort: default, name, state, harness, session. Configurable as `sort_next`; only on the deck page |
| `R` | Reset the deck to session grouping and default sort, keeping selection, detail scroll and focus scope |
| `o` `O` | Group the list the next way, or the previous one (below) |
| `n` | Start an agent: session, agent command, name, cwd, task or project, first prompt. Waits until it is at its prompt |
| `t` | Open a terminal: session, name, cwd, task or project. Always an interactive shell, the pane tuios itself opens; send commands to it with `p`. Creates the session when it does not exist |
| `p` | Send a prompt. An agent gets it through `queue-prompt`, typed when the agent is at rest; a plain terminal gets the text and Enter |
| `a` | Assign the pane to a task or a project through the assign tree (below) |
| `w` | The task tree modal for the selected pane or group header: its whole project, managed in place (below) |
| `c` | Save the pane's screen and scrollback to a file |
| `i` | Extract context items from the pane (above) |
| `x` | Close the pane, after a confirmation |
| `enter` | Go live on the pane: type into it until `ctrl+a d` (below) |
| `r` | Refresh |
| `T` `C` `I` `U` `A` | The tasks page, the captures page, the context page, the prototypes page, the mass-add screen |
| `V` | The completed prompt review queue (Enter opens and marks reviewed) |
| `:` `ctrl+p` | The command palette |
| `q` | Quit, after a confirmation; the panes stay |

With the main area focused, these keys change and the rail's selection
stays where it is; every other key does what it does with the rail
focused:

| Key | Does |
| --- | --- |
| `h` (`←`) | Focus the rail |
| `j` `k` | Scroll the detail a line |
| `g` | Go to the top of the detail |
| `J` `K`, `G` | Scroll half a page; `G` follows the newest output again |
| `enter` | Go live on the pane, outside the Turns tab |

The Turns tab (`4`) is an accordion: one row per prompt the person gave an
agent, and one per command a pane's shell ran, oldest first, the two kinds
interleaved by time behind `you>` and `$`. A prompt row expands to the whole
prompt and the agent's reply text up to the next prompt, read from the
transcript when the deck holds the person's presence, and otherwise from the
agent's activity, which has the prompts and the end of each turn. A command
row, read from the daemon's `command-history`, shows its exit code, duration
and command line, and expands to its working directory and exit; the daemon
keeps no output for a command, so the Screen tab has that. With the main
area focused on the Turns tab:

| Key | Does |
| --- | --- |
| `j` `k` | Move between rows |
| `{` `}` | The previous or next prompt row, past the commands between |
| `enter` `space` | Expand the row, or fold it again |
| `y` | Copy the prompt or the command line through OSC 52 |
| `g` | The first row |
| `G` | The newest row, and follow it |
| `J` `K` | Scroll half a page |

A re-read keeps the expanded rows and the focused one.

The list refreshes itself on the daemon's `agent-state`, `attention` and
window events. The detail pane re-reads once a second and keeps your scroll
position unless you are following the bottom.

A pane the deck cannot talk to (one on `needs_input`) is refused a prompt;
answer it in tuios.

### Live: typing into a pane

`enter` on a pane goes live on it. The detail pane becomes the pane's
screen, redrawn as it prints, with its cursor, and every key you type goes
to the pane: letters, arrows, `ctrl+c`, `esc`, `q`, `:`, `F1`, all of them.
The daemon encodes each key for the pane's modes, so an editor's arrows and
a shell's history work as they would in tuios. A paste is pasted, bracketed
when the program asked for that.

The one key the pane does not get is the prefix, `ctrl+a`. The key after it
is a live command:

| After `ctrl+a` | Does |
| --- | --- |
| `d` | Detach: focus comes back to the deck, the pane keeps running |
| `z` | Zoom: the pane across the whole deck, or beside the rail again |
| `Z` | Zoom the pane in its own tuios session (tiling on), or put it back |
| `f` | Fullscreen the pane in its own tuios session (tiling off), or put it back |
| `j` `k` | Go live on the next or the previous pane of the rail |
| `?` | The live keys |
| `ctrl+a` | Type `ctrl+a` itself into the pane |
| `esc` | Cancel the prefix |

While the deck is live the pane is fitted to the live area through the
stream's size lease, so what you see is the whole screen; the live bar shows
the size, and the pane goes back to its own size on detach. An attached
tuios client shows the smaller pane meanwhile. A lease only ever makes a
pane smaller, though: while a tuios client shows the session, the pane
stays no larger than the tile that client draws it in, and the live bar
shows that tile's size. `ctrl+a Z` asks the client to zoom the pane and
`ctrl+a f` to make it fullscreen, each with the tiling mode it needs (zoom
wants tiling on, a fullscreen snap wants it off); the same key again puts
the pane and the tiling mode back, and the view moves with `j` and `k`. With
no client attached the lease already fits the pane to the deck, so neither
has anything to ask for. The session ends on its own when the pane closes. The prefix and the commands are the `live` mode of the
settings file (below), so `ctrl+a` can be another key.

The deck embeds no terminal emulator for this: the daemon renders the
screen (`capture-pane`), reports the cursor (`get-window`, while a tuios
client is attached to the session) and types the keys (`send-keys`,
`send-text`); `stream-pane` is held open only for its lease and as the
signal that the pane printed.

### Grouping and sorting the list

A grouping is a list of levels, outermost first, written `a > b > c`. The
levels are session, project, task, parent task (the task's parent, or the
task itself when it is top-level), top-level task, cwd, harness, state and
kind (agents or terminals). `o` cycles the presets:

```
session
task tree
project
project > parent task
task
top-level task
harness > project > parent task
cwd
harness
state
kind
none
```

then round to session again; `O` goes the other way. `task tree` (or
`tree`) stands alone: each pane sits under its project and every task on
its path from the root down, one plain header per level with nothing but
the name, so a pane on a sub-subtask is four levels deep, a pane with only
a project sits right under it, and unassigned panes are under `(none)`.
The palette has `group by …` for each preset, and `group by…` asks for any
list of levels,
such as `harness > project` (`top` and `parent` are short for top-level
task and parent task; the field lists the level being typed). Each group is
a header row, indented one step per level, with the next level's headers or
the panes under it; a project or task header is in its theme, and a task
header carries the path `project › top task › … › task`, without the
project when a project level sits above it. Within a level groups sort by
name with `(none)` last. `none` stands alone and shows a flat list across
sessions, without headers. During focus it only includes panes in the focus scope.

Sorting is independent of grouping. `s` cycles `default`, `name`, `state`,
`harness` and `session` on the deck page. The palette offers `sort rail by …`
for each choice; the web rail has a Sort select. Default keeps the existing
session/window listing order. The other choices sort ascending by the named
field, case-sensitive, with pane ID as the deterministic tie-breaker. State
treats non-agent panes as `terminal`; missing harness values sort first.
Sorting applies within each group's pane rows, or globally with `none`.
It does not reorder task headers, the review queue or other pages.

The selected pane and detail scroll survive sorting, regrouping and daemon
refreshes. `R` restores session grouping and default sort on the deck page;
the web also has Reset view. Reset never unlocks focus. The browser saves
grouping and rail sort locally. No recent sort is offered because the pane
listing has no reliable recency timestamp.

### The tasks page

| Key | Does |
| --- | --- |
| `j` `k` `g` `G` | Select a project or a task |
| `{` `}` | The previous or next row at the same depth: project to project, task to task |
| `(` `)` | The parent row (a task's project or parent task), or the first child |
| `N` | New project: its name |
| `n` | New task under the selected project, or a new subtask under the selected task. The form's parent field starts at the row; `ctrl+h` walks it up a level (to the project's top level at most) and `ctrl+l` back down, from any field, so a top-level task is a few keys away from a subtask row |
| `e` | Edit the row in one form: a task's title, parent, status, colour and glyph; a project's name, status, colour and glyph. The parent lists `(top level of <project>)` for every project and every task but the row and its subtasks, so a task moves within its project or to another, subtasks and all. Only what changed is written, and the status line says what that was |
| `s` | Cycle the task's or project's status: open, done, skipped, planned, open |
| `m` | The next colour for the row's theme: the free ones first, then the rest of the palette |
| `M` | Pick the row's glyph from the glyph table (above) |
| `a` | Assign the pane selected on the deck page to the row |
| `enter` | Back to the deck with the first pane assigned to the row selected |
| `x` | Delete the row, what is under it and their assignments, after a confirmation that counts them |
| `r` | Refresh |
| `A` | Add many projects, tasks and subtasks at once (below) |
| `esc` | Back to the deck |

### The task tree modal

`w` on the deck page, or `task tree…` in the palette, opens a modal over
the deck with the whole tree of the selected pane's project: the project at
the top, then every task and subtask under it, indented, with its glyph,
its status mark and how many panes are assigned to it. A pane assigned to a
subtask deep in the tree still shows the project's whole tree, with its own
node highlighted and marked `◂ this pane`. On a group header the tree is
the project of the first assigned pane under it. An unassigned pane says
so, and `a` opens the assign tree for it.

| Key | Does |
| --- | --- |
| `j` `k` `g` `G` | Select a node |
| `s` | Cycle the node's status: open, done, skipped, planned |
| `e` | Edit the node in the tasks page's edit form; the modal comes back when the form closes |
| `n` | Add a subtask under the node: the mass-add outline opens with an empty line under it |
| `x` | Delete the node, after a confirmation that counts the subtasks and assignments going with it |
| `a` | Assign the selected pane to the node |
| `enter` | The tasks page with the node selected |
| `esc` `q` | Close; the rail selection and the detail scroll are what they were |

Only one modal is up at a time: a form or a confirmation the modal starts
takes its place, and the modal returns when it closes.

### Adding many at once

`A`, on any page, or `add many…` in the palette, opens a multiline form
titled `add projects and tasks`. It opens holding every existing project,
sorted by name, with its tasks and subtasks nested beneath in the order
they were made, and the cursor on an empty line after them. Its text is an
outline:

```
deck
  command palette
    verb autocomplete
  captures page
web
  landing
```

A line with no indent is a project: an existing name is reused, a new one
is created. Each further indent, two spaces or a tab, nests a task under
the line above it, so `verb autocomplete` becomes a subtask of `command
palette`. The outline has a line cursor: `up` and `down` (or `ctrl+p` and
`ctrl+n`) move between lines, `left` and `right` within one, `home` and
`end` (or `ctrl+a` and `ctrl+e`) to its ends. Typing inserts at the
cursor; `enter` splits the line there and the new line below keeps the
indent of the one above; `backspace` at the start of a line joins it to
the one above; `ctrl+u` clears the cursor's line. `tab` indents the
cursor's line by one level (two spaces) and `shift+tab` takes one level
off it, so an outline can be typed without counting spaces; `shift+tab` on
a line with no indent does nothing, and neither key moves between fields
here. Up to 14 lines show at once, scrolled to keep the cursor in sight,
and `@` lists no files here. A title already taken
under the same parent is reused rather than added twice, which is how sub-items go under an existing task; a line with
no indent may also name an existing task by its path, `deck / command
palette`, to add under it. Blank lines and lines starting with `#` are
skipped. `ctrl+s` checks the whole outline first: a line indented under
nothing, an indent that jumps more than one level, or an odd number of
spaces is refused with its line number, and nothing is created. Then the
items are created in order, each new one with a free colour from the theme
pool, and the status says `added N projects, M tasks`. The tasks page keeps
its selection.

To add a subtask to an existing task, move the cursor to the task's line,
press `end` if needed, `enter` and `tab`, and type the title: the existing
lines are reused, so only the new line is created. Existing items are
drawn dim, a project or task that is not open with its mark and
status after it (`deck  ✓ done`), and they stay in the outline so items
can still be added under them. Mass-add only adds: removing a prefilled
line deletes nothing, and editing one renames nothing (the edited text
reads as a new item under the same parent). The hint says so. With an
empty store the box opens empty and the hint shows the example above. A
task whose title starts with `#` is listed at the end as its path, since a
line starting with `#` is a comment.

### The captures page

| Key | Does |
| --- | --- |
| `j` `k` `g` | Select a capture, newest first |
| `J` `K`, `G` | Scroll the file; `G` follows its end again |
| `enter` | Back to the deck with the pane the capture came from selected, when a pane of that session and name is still open |
| `x` | Delete the file, after a confirmation |
| `r` | Read the folder again |
| `esc` | Back to the deck |

### The context page

`I` lists the context items on the left, newest first, each with its star
`★`, its pin `^`, its kind and its text on one line; the right shows the
selected one whole, with the pane it came from, what it is assigned to,
when it was found and whether it is starred and pinned.

| Key | Does |
| --- | --- |
| `j` `k` `g` `G` | Select an item |
| `{` `}` | The first item of the previous or next group, when grouped |
| `s` | Sort the next way: newest, kind, starred first, pinned first |
| `f` | Filter through the assign tree's box (below): `all`, the kinds, then the projects and tasks, each with its item count |
| `o` `O` | Group the next or the previous way: none, kind, project, task, pane |
| `*` | Star or unstar the item |
| `!` `P` | Pin or unpin it |
| `a` | Assign it to a task or a project through the assign tree |
| `y` | Copy its text through OSC 52, and with `pbcopy`, `wl-copy` or `xclip` too when one is on `PATH` |
| `enter` | Type its text into the pane selected on the deck page, without Enter, so nothing is submitted |
| `ctrl+o` | Open a link, a pull request (`owner/repo#123` as its GitHub URL), a file or a folder with `open`, or `xdg-open` off macOS |
| `w` | Open it with a program: the field lists the platform's opener, `code`, `cursor`, `nvim`, `vim` and `less` when on `PATH`, and takes any other. A program that needs a terminal (vim, less and the like) runs in a new shell pane of the selected pane's session, assigned to the item's task or project; any other runs detached |
| `x` | Delete the item, after a confirmation |
| `r` | Redraw the list and count the items |
| `esc` | Back to the deck |

A prompt or a command has nothing to open; `y` and `enter` work on every
kind.

`f` (or the palette's "filter context…") opens the assign tree's box with
`all` and the kinds (`link`, `file`, `dir`, `pr`, `prompt`, `command`) on
top, then the project, task and subtask tree. Every row counts its items:
a task counts its own and its subtasks', a project its own and its tasks'.
A row with no items, kind or target, is hidden. The keys are the assign
tree's. The query keeps a row whose name matches it or that holds an item
whose text contains it, and the preview lists the items the row would keep,
the matching ones first and marked, with their count. `enter` on `all`
clears the kind, project and task filters; on a kind it sets the kind; on a
project or a task it sets that, keeping the kind. The typed text becomes
the text filter only when it did not match the picked row's name: `deck`
then project deck filters by the project alone, `github` then project deck
keeps deck's items holding `github`. The status line states the result.

### The assign tree

`a` on the deck page, `a` on the context page and the palette's "assign to
task…" and "assign to project…" open one box: on the left every project,
task and subtask as a tree with every branch open, on the right the
highlighted target's path, status, theme, last change, subtask (or task)
count, assigned panes with their state, and the last 15 lines of the first
pane's screen, read once per pane while the box is up. In a box under 100
columns wide the preview keeps the details and drops the screen. It opens
on what the pane or the item is assigned to now.

| Key | Does |
| --- | --- |
| `j` `k`, `↓` `↑`, `ctrl+n` `ctrl+p` | Move |
| `g` `G` | The first row, the last (`+ add new…`) |
| `h` `←` | Close the branch, or go to the parent |
| `l` `→` | Open the branch, or step into it |
| `{` `}` | The previous or next sibling |
| `(` | The parent |
| any other letter | Filter: fuzzy by name and path, keeping the matching branches and their ancestors, all open. Once something is typed every letter types; `/` first lets a query start with one of the keys above |
| `backspace` `ctrl+u` `ctrl+w` | Edit the query |
| `enter` | Assign to the highlighted project or task, reported as `assigned <pane> to <project> › <task>` |
| `esc` | Cancel |

`+ add new…`, always the last row, opens the mass-add screen with what was
typed as a task under the pane's project; `ctrl+s` there adds it and brings
the tree back with the new task highlighted, `esc` brings it back as it was.

### Prototypes

The two-pane preview with the tree on its left won, and is now the assign
tree above and the context page's `f`. The page stays for now.

`U` (or the palette's "prototypes") opens a page of picker prototypes for
assigning a pane and filtering the context page, so they can be tried on
the real store and panes side by side. Each row names the prototype, what
it drives and how it works; the right side lists its keys. `enter` runs it
for real: an assign row assigns the pane selected on the deck page and
reports it on the status line the way the assign form does, and a filter
row filters the context page and goes there. The first six rows assign,
the next five filter. While a prototype is up, `F1` opens its keys, and
`esc` (or `ctrl+c`, which works even where `esc` clears a filter first)
cancels it and comes back to the page; `esc` on the page goes back to the
deck. These are parallel takes on one job and will be consolidated into
one picker once one wins.

1. **Single fuzzy list**: one query line under a tall list of every target
   (`project`, `project / task`, `project / task / subtask`), ranked like
   the palette with the matched letters marked; `ctrl+n` `ctrl+p` move,
   `enter` picks, `+ add new…` at the end opens the mass-add screen and
   comes back.
2. **Two-pane preview**: the same list on the left and, on the right, the
   highlighted target's path, status, theme, assigned panes and the last
   lines of the first pane's screen.
3. **Tree**: projects collapsed; `l` opens a branch, `h` closes it or goes
   to the parent, `/` filters to the branches holding a match.
4. **Hotkey quick-pick**: the nine targets with the most panes and the
   latest changes on `1`-`9`; `0` falls into the single fuzzy list.
5. **Miller columns**: a project column, a task column and a subtask
   column under a breadcrumb; `l` `h` move between them and `enter` picks
   the highlighted node at any depth.
6. **Type-ahead chips**: one input; `deck/` locks the project as a chip
   and then tasks complete inside it, `tab` locks the highlighted
   completion, `backspace` on an empty segment pops a chip, `enter` picks
   what the chips spell.

`?` in prototypes 1, 3, 5 and 6 (and 2, where it is open from the start)
opens or closes a preview beside the list: the highlighted target's path,
status, theme, subtasks or tasks and assigned panes, and the last 15
lines of the first assigned pane's screen, read once per pane while the
picker is up. In the filter rows the preview counts the context items the
filter would keep and lists the first of them.

The filter rows (1, 2, 3, 5 and 6) start with the context kind, `any` for
every kind, then the project and the task; a typed text that names nothing
filters by the text instead.

### The command palette

`:` or `ctrl+p` opens the palette over the lower part of the screen. Typing
filters: a name starting with the query ranks first, then a word of it,
then the query anywhere in it, then its letters in order. `ctrl+n` `ctrl+p`
(or `↓` `↑`) pick, `tab` puts the picked name in the query, `enter` runs,
`esc` closes. Every entry carries its category:

- `[deck]`, listed first: assign to task…, assign to project…, clear
  assignment, new agent, new terminal, prompt, capture, close pane,
  refresh, go to deck, go to tasks, go to captures, go to context, filter context…, prototypes, extract
  context from pane, group by each of the presets, group by… for any
  levels, new project, new task, new subtask of…, add many…, add note,
  edit note…, delete note… (of the row selected on the tasks page),
  delete capture. Each does what its key does.
- `[tuios]`: every verb the daemon lists in `list-verbs`, asked for once per
  run, with its description. Picking one asks for its parameters in a form,
  required ones first, with the parameter's description as the hint and
  known values listed under the field: session names, the windows of the
  session typed, the keys of `get-option` and `set-option`, theme names for
  a `.theme` key, and the values a parameter accepts. What you type is
  typed by the verb's contract, ints and bools parsed, arrays and objects as
  JSON or a comma-separated list. A verb with no parameters runs at once.

The result, pretty-printed, or the daemon's error with its code and hint,
takes the place of the detail pane on the deck page under a
`result of <verb> {params}` bar and stays there, with no re-read on the
timer, until the selection or the tab moves.

### Forms

A form opens as a box over the middle of the screen: its title in the top
border, every field on its own line, the current one marked `›` with its
hint under it, and the keys at the bottom. A refused form keeps its message
on the status line.

| Key | Does |
| --- | --- |
| `enter` | The next field, or submit on the last one |
| `shift+tab` `↑` | The previous field |
| `tab` | Take the field's default when it is empty |
| `ctrl+s` | Submit from any field |
| `ctrl+u` | Stash the full draft in prompt fields, then clear the current line only; earlier lines stay intact |
| `ctrl+r` | In a prompt field, browse and search stashed prompts, newest first |
| `ctrl+w` | Delete the last word |
| `ctrl+z` | Undo the current field's last edit, including clearing, typing, completion or a paste; repeat to undo further |
| `esc` | Cancel |

A field with known values (tasks, projects, sessions, windows, option keys,
themes, accepted values, programs, grouping levels) lists up to 8 of them
under it, filtered as you type the way the palette filters. `ctrl+n`
`ctrl+p` (or `↓` `↑`) move the highlight, `tab` takes the highlighted
value, and `enter` takes it too unless what you typed is already one of the
values.

Task, project, parent and window fields (the new and edit
task forms' `parent`, the context filter's project and task, the queue
prompt form's task, project and pane, the palette's `window` parameter) also preview the highlighted entry under the list: its path and
status or the pane's metadata, the live panes assigned to it (the task's own
first, then needs-you ones, then agents), and the last six lines of the
first one's output. A task with no live pane says so. The output is read in
the background and re-read every couple of seconds while the entry stays
highlighted; a read for an entry you have moved off is dropped, and typing
and the list never wait on it. The preview takes only the lines the list
leaves in the box, so on a small terminal it shrinks and the key hints stay.

Every project or task field (new agent, new terminal, the palette's new
task and new subtask entries) ends its list with `+ add new…`.
Taking it puts the form aside and opens the mass-add screen, holding the
whole existing outline, with what you typed on a new line and the cursor
at its end: as a project line at the end in a project field, or in a task
field as a task under the selected pane's task (its project when it has
no task, the first project when it has neither). `ctrl+s` there adds the
outline and brings the form back with the field set to the first project
or task it added (a task as its path), or when it added none, to the item
on the cursor's line; `esc` brings the form back unchanged.

The prompt (`p`), the new agent's first prompt and the mass-add outline are
multiline: `enter` starts a new line (the outline edits at a line cursor,
see [Adding many at once](#adding-many-at-once); the others type at the end), `ctrl+s` (or `ctrl+enter` where the
terminal reports it) submits, and a paste keeps its newlines.
Undo history belongs to each field and lasts while the form is open; cancelling
or submitting the form discards it. A repeated `ctrl+u` on an empty line does
nothing to the text and does not replace the edit you can undo.

Prompt fields autosave their exact text after each edit or paste. In the TUI,
drafts live in `deck.json` under `prompt_stashes` and survive cancellation,
submission and restarting deck. One entry is updated while composing; clearing
the whole field preserves its last nonempty text. `ctrl+u` also saves a snapshot
before clearing a line. Recovery shows identical text once; empty drafts are not
saved. There is no automatic expiry or entry limit. Failed saves are shown in
the form and block dismissal until the text can be saved.

In a prompt field, `ctrl+r` opens **stashed prompts**. Type to search the full
text, use arrows or `ctrl+n`/`ctrl+p` to select, and `PgUp`/`PgDn` to scroll
the preview. `enter` restores the selected text without submitting it; `esc`
or `ctrl+c` returns to the unchanged draft. Restoring also saves any draft it
replaces, and `ctrl+z` can undo the replacement while the form stays open.
Recovery is available in pane prompts (including terminals), new-agent first
prompts, new/edit queued prompts, and the palette's `queue-prompt`/`start-agent`
prompt parameters. `ctrl+r` also works in a prompt field in vim normal mode.
Stashes are local plaintext, protected by the store file's 0600 permissions;
they are retained until removed from the store, not automatically expired.

Web prompt fields save synchronously in this browser's local storage, including
new-agent prompts, pane prompts and queued prompts. **Recover saved prompt…**
opens a list with a full-text preview; **Restore prompt** fills the editor
without submitting. Cancelled and submitted text remains recoverable after a
reload, and separate browser tabs do not overwrite each other's drafts. Browser
drafts are local plaintext, not synced to the server or other browsers; clearing
site data removes them. If browser storage is unavailable or full, the editor
shows **Prompt NOT saved** and blocks Cancel, Escape and submission.

In a multiline field other than the outline `@` at the start of a word lists what is in the
selected pane's cwd (the deck's own when no pane is selected): folders with
a trailing `/`, files plain, hidden ones left out, at most 200. Typing
after the `@` filters the list; `tab` or `enter` on a folder opens it, on a
file puts its path, relative to the cwd, in place of the `@` and what
followed it. `esc` closes the list and keeps the form. A symlink that leads
outside the cwd is not listed.

#### Attachments and slash commands in an agent's prompt

The daemon's `queue-prompt` carries text only, so an attachment is a file
reference the agent reads itself, not an upload. In the prompt for an agent
(`p`) and the new agent's first prompt, `@` keeps the `@` in front of the
path (`@src/main.go `, a space ends it) so Claude Code and OMP read the
file. A path with whitespace is written `@"my dir/a file.txt"`, the quoted
form both harnesses read (checked by asking each, with `claude -p` and
`omp -p`, what a file named `a b.txt` held); a folder with a space in its
name leaves the quote open so the list goes on inside it. Only a name with a
double quote or a newline in it cannot be written and is not listed; the OS
dialog reports it as not attached. `ctrl+o` opens the OS file dialog
(`osascript` on macOS, `zenity` or `kdialog` elsewhere), several files at
once, and adds each as `@path`: relative to the cwd when inside it, absolute
otherwise. A prompt to a plain terminal keeps the plain-path `@`.

A `/` typed as the first character lists the slash commands and skills the
agent can run. Nothing runs until you submit the prompt, and the deck only
reads folder names and `SKILL.md` frontmatter:

- Claude Code (harness `claude` or `claude-code`): `commands/<name>.md` (a
  subfolder becomes `folder:name`) and `skills/<name>/SKILL.md` under
  `~/.claude` and every `.claude` folder from the cwd up. Plugin-provided
  commands are not listed.
- OMP (harness `omp`), following its own discovery: skills as
  `/skill:<name>` from `<agent dir>/skills/*/SKILL.md` and `.omp/skills/*/SKILL.md`
  in the cwd and each folder up to the repository root, where the name is
  the frontmatter `name` (else the folder's) and a skill with no
  `description` or with `enabled: false` is skipped, as OMP skips it; commands
  as `/<name>` from `<agent dir>/commands/*.md` and `<cwd>/.omp/commands/*.md`
  (not recursive). The agent dir is `$PI_CODING_AGENT_DIR` of the deck, or
  `~/.omp/agent`; a profile's own dir is not followed. OMP also reads
  Claude, Codex and other folders and extension commands when they are
  switched on in its settings; those are off by default and not listed.
- Other harnesses list nothing, since the deck reads no convention for them.

`deck web` does the same in the prompt box and the new-agent first prompt:
the suggestions list under the box (`↓`/`↑` or `ctrl+n`/`ctrl+p`, `tab` or
`enter` to take, `esc` to close), and `Attach files…` browses the daemon
machine's files through `/api/files` (click folders to open, files to
select, across folders) since a browser's picker cannot name a path there.

The queued-prompt form takes both too, for the harness that will read the
prompt (the chosen running agent's, else the agent field's) and under the
folder it will start in: the cwd typed, else the preset's, else the chosen
pane's (or the selected pane's). In the TUI `ctrl+o` on the prompt picks files
and anywhere else picks the folder. The prompt (`p`) opens on its text, with
the preset one `shift+tab` up, and shifted letters type like any others; only
ctrl and alt chords are shortcuts.

A confirmation (closing a pane, deleting a row, a capture or a context
item) asks on the status line and defaults to yes, `(Y/n)`: `enter` or `y`
confirms, `n` or `esc` cancels, and any other key does nothing.

### Contextual keybindings

Every TUI screen and modal footer offers `F1 keybindings`. Forms also retain
the field position and INSERT/NORMAL state; these are status, not key lists.
Help opens for what currently has focus, including saved prompts and picker
variants, and lists bindings vertically. Use `F1` or `esc` to return without
changing the underlying input or selection. The help modal lists its own
navigation keys vertically and shows `F1 close` in its footer.

## The web page

`deck web` serves one page and a JSON API. The left list and the state
badges follow the daemon's event stream; the detail re-reads every two
seconds without touching the list, the selection, an open dialog or a
scroll position that is not at the bottom. Its rows read like the TUI's:
the kind tag (`AI` or `$`, classes `kind-agent` and `kind-terminal`), the
project's first four letters and the task title in their theme
colours, and `!!!!` with the name in black on red for an unassigned pane.

| Tab | Shows |
| --- | --- |
| Live | The pane in xterm.js through `stream-pane`: snapshot, output and resizes in, keys and a size lease out. Typing goes to the pane |
| Screen | The last lines of the pane's screen and scrollback as text |
| Activity | Prompts, tool calls, commands and finished turns from the agent's hooks |
| Transcript | The conversation, when the server holds the person's presence (below) |

The header's Deck, Tasks, Captures and Context buttons switch pages, and
the page is in the URL's hash (`#tasks`, `#captures`, `#context`, none for
the deck), so a page can be bookmarked or reloaded. On the deck page the
buttons do what the TUI's keys do: New agent, New terminal, Assign, Prompt,
Capture, Extract context, Close, and a Group by select over the left list
with the TUI's presets, and a field beside it that takes any `a > b > c`.
The tasks page is the TUI's: the tree of projects, tasks and subtasks with
their counts, New project, New task, Rename, Toggle done, Theme and Delete,
where the Theme dialog has the same glyph table as `M`, with a preview,
and the panes assigned to the selected row. The captures page lists the
folder and shows a file. The context page filters by kind, project, task,
text, starred and pinned, sorts and groups the way the TUI's does, and has
Copy (to the browser's clipboard), Insert, Open, Star, Pin, Assign and
Delete.

The task, project and window fields of the dialogs (and the palette's
`window` parameter) show a preview under the field once their value is one
of the datalist's entries: the same head and recent output as the TUI's,
fetched from `/api/preview`; a newer value cancels the older request. The
web page has no parent field, and its context filters are plain selects, so
they have no preview; a dialog's buttons stay in view on a short window.
The queue dialog's running-agent select previews too. A dialog's previews are
cleared when it closes and read again from its actual fields when it opens, so
a reopened dialog never shows the last one's preview.

The `⌘` button, `ctrl+k` or `ctrl+p` open the command palette, with the
same two categories as the TUI's; `ctrl+n` `ctrl+p` (or `↓` `↑`) move in
it and `esc` closes it. A tuios verb's parameters become a form, required
ones first, each with its description as the placeholder and the sessions
and windows offered as you type; the result replaces the detail text under
a `result` tab until another tab or pane is picked. When the focus is not
in the terminal or a field, `o` `O` cycle the grouping, `T` `C` `I` `H` and
`esc` switch pages, and on the deck page `j` `k` move the selection, `1` to
`4` switch tabs, `i` extracts context, and `n` `t` `p` `a` `c` press the
buttons.

The working directory fields have a Browse button that walks the folders of
the machine the daemon runs on, through the server, since a browser's own
picker cannot name a path there. Open runs on that machine too.

There is no password. `deck web` listens on loopback and refuses any other
address unless `--allow-remote` is given; put it behind something that
authenticates before doing that. xterm.js loads from jsdelivr, so the Live
tab needs the browser to reach the internet once.

### The API

```
GET    /api/state                              sessions, panes, daemon, whether transcripts work
GET    /api/events                             server-sent events relayed from subscribe
GET    /api/history?kind=&q=&window=           newest-first persisted entries, source boundary and observation error
GET    /api/dirs?path=                         folders under a path, for the picker
GET    /api/panes/{session}/{id}/screen        capture-pane, recent, ?lines=
GET    /api/panes/{session}/{id}/activity      agent-activity as lines plus the recap
GET    /api/panes/{session}/{id}/transcript    agent-transcript as lines
GET    /api/panes/{session}/{id}/stream        WebSocket: one text reply line, then stream-pane frames both ways
POST   /api/panes/{session}/{id}/prompt        {"text", "is_agent"}: queue-prompt, or send-text with Enter
POST   /api/panes/{session}/{id}/capture       writes the capture file, ?name=
DELETE /api/panes/{session}/{id}               close-window
POST   /api/agents                             start-agent: {"session","agent","name","cwd","prompt","ready_timeout"} plus a target (below)
POST   /api/terminals                          new-window with an interactive shell, creating the session first: {"session","name","cwd"} plus a target
```

A target names a task or a project: `{"task": "deck / palette"}` by path or
bare title, `{"project": "deck"}` by name, or `task_id` / `project_id`. A
task implies its project. A request that names no target field at all makes
an unassigned pane, for clients from before the store; one that sends them
empty is refused.

```
GET    /api/projects                           {"projects": [{id, name, theme: {color, glyph, css}, status, created, open_tasks, panes}]}
POST   /api/projects                           {"name", "theme"?: {"color", "glyph"}}
PATCH  /api/projects/{id}                      {"name"}, {"status": "open"|"done"|"skipped"|"planned"}, {"theme": {"color", "glyph"}}, either half of the theme alone
DELETE /api/projects/{id}                      with its tasks and their assignments; the answer counts them
GET    /api/tasks?project=                     {"tasks": [{id, parent_id, project_id, title, path, depth, theme, status, easy, snoozed_until, panes, …}]}
POST   /api/tasks                              {"project_id", "title"} for a top-level task; {"parent_id", "title"} for a subtask; "theme"?
PATCH  /api/tasks/{id}                         {"title"}, {"status": "open"|"done"|"skipped"|"planned"}, {"easy": true|false}, {"snoozed": true|false}, {"theme": {…}}; a move: {"parent_id"} or {"parent": path} under a task, {"project_id"} or {"project": name} to a project’s top level
DELETE /api/tasks/{id}                         with its subtasks and their assignments
GET    /api/notes?task=ID | ?project=ID        {"notes": [{id, text, relation: own|ancestor|descendant, owner, task_id, project_id, created, updated}]}: every note that task or project sees
POST   /api/notes                              {"task_id" | "project_id", "text"}
PATCH  /api/notes/{id}                         {"text"}
DELETE /api/notes/{id}                         remove the note
GET    /api/assignments                        {window id: {session, task_id, project_id, resolved: {project, task, top}}}
PUT    /api/panes/{session}/{id}/assignment    a target; names resolve the way the TUI's forms do
DELETE /api/panes/{session}/{id}/assignment    drop it
GET    /api/captures                           the folder, newest first: {name, path, size, modified, session, pane, kind}; kind is "turn", "cmd" or ""
GET    /api/captures/{name}                    {"name", "content", "lines"}
DELETE /api/captures/{name}                    remove the file
GET    /api/reviews                            {reviews, warning}; history's turns in completion order, no side effects; ?refresh=1 is accepted and ignored
POST   /api/reviews/{id}/open                  {review}; marks this turn reviewed, once, and attaches its auto-capture as output; the id is a history id, so URL-encode it
GET    /api/prompt-presets                     {"presets": [{id, name, prefix, cwd}]}
POST   /api/prompt-presets                     {name, prefix, cwd}; PATCH /api/prompt-presets/{id} changes any of them; DELETE removes it
GET    /api/queued-prompts                     {"queued": [{id, text, preset, task_id, project_id, project, task, cwd, session, agent, window}]}
POST   /api/queued-prompts                     {task|task_id|project|project_id, text, preset?, agent, session, cwd?, window?}; PATCH /api/queued-prompts/{id} changes any of them; DELETE removes it
POST   /api/queued-prompts/{id}/start          send it; 200 and it leaves the queue, or 502 {error, kept} and it stays
POST   /api/prompt-presets/example             add the short-answer example preset (only when asked)
GET    /api/context?kind=&project=&task=&starred=&pinned=&q=
                                               {"items": [{…the stored item, project, task}], "kinds"}; project and task by id or name, q in the text
POST   /api/panes/{session}/{id}/context/extract
                                               gather and store the pane's items: {"status", "new", "found", "kinds", "sources"}
PATCH  /api/context/{id}                       {"starred"}, {"pinned"}, a target; a target sent empty clears it
DELETE /api/context/{id}                       remove the item
POST   /api/context/{id}/insert                {"session", "window"}: send-text without Enter
POST   /api/context/{id}/open                  {"program"?}: open it on the server's machine, detached; the opener by default, a terminal program refused
GET    /api/preview?task=|project=|window=&session=
                                               {"head": [metadata lines], "lines": [the chosen pane's last 6 lines], "session", "window", "error"?}
GET    /api/palette                            the 16 colour names with their CSS hex
GET    /api/glyphs                             {"sections": [{label, glyphs: [...]}]}, the glyph table
GET    /api/verbs                              list-verbs, as the daemon answers it
POST   /api/call                               {"verb", "params"}: one daemon verb, refused unless list-verbs named it
```

## Tests

`scripts/e2e.sh` runs the TUI against two throwaway daemons under a scratch
folder: one holds the panes it manages, the other runs the deck in a pane and
drives it with `send-keys`. It seeds a `deck.json` by hand and points the
deck at it through `TUIOS_DECK_DATA`, so your own is never touched.
`scripts/e2e-prompt-stash.sh` drives prompt stashing and recovery through the
real TUI: exact text, preview/search, restart persistence, all composer forms,
undo, deduplication, vim mode and failed-save protection.
`scripts/e2e-web.sh` runs `deck web` against a throwaway daemon the same
way and checks the API with curl, the event stream, and the WebSocket pane
stream with `scripts/wscheck`. `scripts/e2e-reminders.sh` drives the reminder palette
forms in `deck tui` and the reminder API in `deck web`, and checks delivery,
recurrence, disable, persistence and the OS notifier's arguments.
`scripts/e2e-setup.sh` runs
`deck setup` against a scratch HOME and drives the omp extension under bun
with a fake pi and a fake tuios.
`scripts/e2e-history.sh` exercises retained and live history, repeated commands,
queue acknowledgement, filtering, restart deduplication, pane closure and TUI
navigation with selection preservation against isolated daemons.
`scripts/e2e-focus.sh` exercises task, descendant, project and empty scopes,
manual unlocking, saved easy flags and the full three-minute idle deadline
against real isolated daemons. It also checks the easy-flag API and disk state.

Run `scripts/e2e-turns.py` alone for the focused shell → command-history →
Turns row/expansion check. It needs an already-built `.dev/deck` (or `DECK`),
uses isolated daemons, stops at the first failure, and prints its artifacts path.
The deadline is 60 seconds including cleanup, with each subprocess capped at
10 seconds. Every check keeps what it saw under `$SCRATCH/out` and prints PASS or FAIL.

`scripts/e2e-review.py` checks completed-turn ordering, explicit opening,
unreviewed navigation, stable detail scroll, reviewed history, the attached
turn capture and persistence across the TUI, HTTP API and pane closure,
against isolated real daemons.
It uses `SCRATCH` and `PORT` (default 18963).

```sh
go build -o .dev/deck ./cmd/deck
scripts/e2e-all.sh                                        # every suite at once, one line each
scripts/e2e.sh && scripts/e2e-web.sh && scripts/e2e-setup.sh
scripts/e2e-prompt-stash.sh
SCRATCH=/tmp/deck-e2e-mine PORT=18799 scripts/e2e-web.sh   # a second run beside the first
SCRATCH=/tmp/deck-history-check PORT=18849 scripts/e2e-history.sh
SCRATCH=/tmp/deck-e2e-focus PORT=18748 scripts/e2e-focus.sh
```

`SCRATCH` names the scratch folder and `PORT` the web suite's port. On
macOS keep `SCRATCH` short and under `/tmp`: the daemons' sockets live in
it, and a unix socket path longer than 104 bytes fails.

## Releasing

```sh
scripts/release.sh            # bumps the patch:  v0.3.9 → v0.3.10
scripts/release.sh minor      # v0.3.9 → v0.4.0
scripts/release.sh major      # v0.3.9 → v1.0.0
scripts/release.sh v0.5.0     # exactly this version
scripts/release.sh minor --dry-run   # show the version, change nothing
```

With no tag yet, a bare run tags v0.0.1 and `minor` tags v0.1.0. It checks
the tree is clean and on an up-to-date `main`, builds and vets, tags,
pushes `main` and the tag, then asks proxy.golang.org for the new version
so `deck update` sees it at once. Nothing else is needed: the Go module
proxy and pkg.go.dev fetch from GitHub on request, and `@latest` is the
newest tag, or the newest commit on `main` while there are no tags.

## Layout

- `internal/daemon`: the JSON client (`Call`, `Subscribe`, `Presence`) and
  typed wrappers for the verbs the deck uses.
- `internal/panes`: the merged view of sessions and agents, and the text
  rendering of activity and transcripts, shared by both front ends.
- `internal/config`: the settings file and the key binding tables.
- `internal/store`: projects, tasks, assignments and context items in
  `deck.json`, and the themes they are drawn in.
- `internal/prompts`: sending a queued prompt, and the rule that takes it
  off the queue.
- `internal/context`: finding context items in a pane, and copying and
  opening them.
- `internal/reminders`: delivering due reminders as a daemon notice and an
  OS notification.
- `internal/events`: the one daemon event subscription a deck process
  keeps. It reconnects with a growing backoff, resumes with `after_seq` so a
  blip loses nothing, and fans each event out to the recorders behind it
  (auto-capture, history) and to the front end: the TUI once, the web page
  once per browser tab. When the daemon reports a gap, or a recorder falls
  behind, that recorder re-reads the daemon's rings.
- `internal/autocapture`: the automatic captures at a turn's or a
  command's end, a handler behind the event stream.
- `internal/history`: the history timeline's recorder, a handler behind the
  event stream that batches a second's events into one save.
- `internal/ui`: the Bubble Tea model, keys, forms, daemon commands and
  view, with the tasks, captures, context and prototypes pages, the
  mass-add screen, the Turns tab, the grouping of the list, the group
  jumps and the command palette.
- `internal/web`: the HTTP API, the event relay, the WebSocket bridge and
  the embedded page in `static/index.html`.
- `internal/capture`: capture files, and the listing of the folder.
- `setup`: the files the deck installs on a machine, embedded, and the code
  that installs, checks and removes them.
- `cmd/deck`: the binary, with the `tui`, `web` and `setup` commands.
