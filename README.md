# tuios-deck

A dashboard over the [tuios](https://tuios.dev) daemon, as a terminal program
(`deck tui`) and as a web page (`deck web`). Both list every terminal and agent
the daemon holds, start new ones with a first prompt, queue prompts, follow
each pane's screen, activity and transcript, and save captures to files. The
web page also shows a pane live, in xterm.js, and types into it.

The deck also keeps a record of its own: projects, the tasks and subtasks
under them, and which pane works on what. Every agent or terminal it starts
is assigned to a task or a project, the list can be grouped by them, a tasks
page manages them, a captures page reviews the files `c` wrote, and a
command palette reaches every verb the daemon has.

Neither owns a PTY. Everything goes through the daemon's JSON verb protocol
on its unix socket, so the panes persist when the deck quits and are the
same panes you see in `tuios attach`.

```
 tuios-deck tuios 1.4.0  /run/user/1000/tuios/tuios.sock         1 need you  3 agents
▾ work                   │ 1 Screen   2 Activity   3 Transcript   work › reviewer
  ◐ reviewer     working │you> review the diff on this branch
  ● tests    needs_input │→ Bash go test ./...
  · build                │  ok Bash
▾ deck                   │■ turn ended
  ○ helper          idle │
captured to ~/.local/share/tuios-deck/captures/work-build-20261017-133455.txt
 j/k move · 1-3 tab · J/K scroll · G follow · o group · n agent · t term · p prompt · a assign · c capture · x close · T tasks · C captures · r refresh · q quit
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
./deck tui --data ~/deck.json  # projects, tasks and assignments; web takes it too
./deck setup                 # the files the deck needs on this machine
```

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

## Projects, tasks and assignments

A project holds top-level tasks; a task holds subtasks, to any depth, in
the same table. A subtask stores no project of its own: its project is its
root task's. Every project and every task has its own theme, one of the 16
ANSI colours and one glyph, drawn before its name in the list, on the tasks
page and on the web page. A new one takes the next colour of the palette
round-robin, with `◆` for a project and `●` for a task, until you change
it.

Every agent and terminal the deck starts is assigned to a task or, failing
that, to a project: the `n` and `t` forms ask for one after the cwd, with
`tab` completing task paths (`project / top task / … / title`) and project
names, and refuse a pane with neither. A task implies its project. `a`
assigns, or reassigns, a pane already open; both fields left empty drop the
assignment. The assignment of a pane that is gone from two listings in a
row is dropped on its own.

The record is one JSON file, `$XDG_DATA_HOME/tuios-deck/deck.json`, else
`~/.local/share/tuios-deck/deck.json`. `TUIOS_DECK_DATA`, or the `-data`
flag of `deck tui` and `deck web`, names another file. It is written whole,
through a temp file and a rename, so it can be read or copied at any time:

```json
{
  "version": 1,
  "projects": [
    { "id": "p_8f3a1c", "name": "deck", "theme": { "color": "blue", "glyph": "◆" }, "created": 1760000000 }
  ],
  "tasks": [
    { "id": "t_1a2b3c", "parent_id": "", "project_id": "p_8f3a1c", "title": "command palette",
      "theme": { "color": "magenta", "glyph": "●" }, "status": "open", "created": 1760000000, "updated": 1760000000 },
    { "id": "t_9c0d4e", "parent_id": "t_1a2b3c", "project_id": "", "title": "verb autocomplete",
      "theme": { "color": "green", "glyph": "●" }, "status": "open", "created": 1760000001, "updated": 1760000001 }
  ],
  "assignments": {
    "98db8226-1829-468e-89a8-41a2baa0ddab": { "session": "work", "task_id": "t_9c0d4e", "project_id": "" }
  }
}
```

A task's `status` is `open` or `done`. Deleting a task takes its subtasks
with it, deleting a project takes its tasks, and either takes the
assignments that pointed at any of them.

## The web page

`deck web` serves one page and a JSON API. The left list and the state
badges follow the daemon's event stream; the detail re-reads every two
seconds without touching the list, the selection, an open dialog or a
scroll position that is not at the bottom.

| Tab | Shows |
| --- | --- |
| Live | The pane in xterm.js through `stream-pane`: snapshot, output and resizes in, keys and a size lease out. Typing goes to the pane |
| Screen | The last lines of the pane's screen and scrollback as text |
| Activity | Prompts, tool calls, commands and finished turns from the agent's hooks |
| Transcript | The conversation, when the server holds the person's presence (below) |

The header's Deck, Tasks and Captures buttons switch pages, and the page is
in the URL's hash (`#tasks`, `#captures`, none for the deck), so a page can
be bookmarked or reloaded. On the deck page the buttons do what the TUI's
keys do: New agent, New terminal, Assign, Prompt, Capture, Close, and a
Group by select over the left list with the TUI's groupings. The tasks page
is the TUI's: the tree of projects, tasks and subtasks with their counts,
New project, New task, Rename, Toggle done, Theme and Delete, and the panes
assigned to the selected row. The captures page lists the folder and shows
a file.

The `⌘` button, `ctrl+k` or `ctrl+p` open the command palette, with the
same two categories as the TUI's. A tuios verb's parameters become a form,
required ones first, each with its description as the placeholder and the
sessions and windows offered as you type; the result replaces the detail
text under a `result` tab until another tab or pane is picked. When the
focus is not in the terminal or a field, `j` `k` move the selection, `1` to
`4` switch tabs, `o` `O` cycle the grouping, `a` assigns, `T` `C` and `esc`
switch pages, and `n` `t` `p` `c` press the buttons.

The working directory fields have a Browse button that walks the folders of
the machine the daemon runs on, through the server, since a browser's own
picker cannot name a path there.

There is no password. `deck web` listens on loopback and refuses any other
address unless `--allow-remote` is given; put it behind something that
authenticates before doing that. xterm.js loads from jsdelivr, so the Live
tab needs the browser to reach the internet once.

### The API

```
GET    /api/state                              sessions, panes, daemon, whether transcripts work
GET    /api/events                             server-sent events relayed from subscribe
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
GET    /api/projects                           {"projects": [{id, name, theme: {color, glyph, css}, created, open_tasks, panes}]}
POST   /api/projects                           {"name", "theme"?: {"color", "glyph"}}
PATCH  /api/projects/{id}                      {"name"} and/or {"theme": {"color", "glyph"}}, either half of the theme alone
DELETE /api/projects/{id}                      with its tasks and their assignments; the answer counts them
GET    /api/tasks?project=                     {"tasks": [{id, parent_id, project_id, title, path, depth, theme, status, panes, …}]}
POST   /api/tasks                              {"project_id", "title"} for a top-level task; {"parent_id", "title"} for a subtask; "theme"?
PATCH  /api/tasks/{id}                         {"title"}, {"status": "open"|"done"}, {"theme": {…}}
DELETE /api/tasks/{id}                         with its subtasks and their assignments
GET    /api/assignments                        {window id: {session, task_id, project_id, resolved: {project, task, top}}}
PUT    /api/panes/{session}/{id}/assignment    a target; names resolve the way the TUI's forms do
DELETE /api/panes/{session}/{id}/assignment    drop it
GET    /api/captures                           the folder, newest first: {name, path, size, modified, session, pane}
GET    /api/captures/{name}                    {"name", "content", "lines"}
DELETE /api/captures/{name}                    remove the file
GET    /api/palette                            the 16 colour names with their CSS hex
GET    /api/verbs                              list-verbs, as the daemon answers it
POST   /api/call                               {"verb", "params"}: one daemon verb, refused unless list-verbs named it
```

## The TUI

The TUI has three pages. The deck page is the one at the top of this file:
the panes on the left, the selected pane's screen, activity or transcript
on the right. `T` opens the tasks page and `C` the captures page from
anywhere, `esc` comes back to the deck, `:` or `ctrl+p` open the command
palette and `q` quits, on every page. The title line names the page and,
on the deck, the grouping when it is not by session.

### The deck page

| Key | Does |
| --- | --- |
| `j` `k` `g` | Select a pane |
| `1` `2` `3`, `tab` `shift+tab` (`l` `h`) | Screen, activity or transcript of the selected pane |
| `J` `K`, `G` | Scroll the detail; `G` follows the newest output again |
| `o` `O` | Group the list the next way, or the previous one (below) |
| `n` | Start an agent: session, agent command, name, cwd, task or project, first prompt. Waits until it is at its prompt |
| `t` | Open a terminal: session, name, cwd, task or project. Always an interactive shell, the pane tuios itself opens; send commands to it with `p`. Creates the session when it does not exist |
| `p` | Send a prompt. An agent gets it through `queue-prompt`, typed when the agent is at rest; a plain terminal gets the text and Enter |
| `a` | Assign the pane to a task or a project; both fields empty drop the assignment |
| `c` | Save the pane's screen and scrollback to a file |
| `x` | Close the pane, after a confirmation |
| `r`, `enter` | Refresh |
| `T` `C` | The tasks page, the captures page |
| `:` `ctrl+p` | The command palette |
| `q` | Quit; the panes stay |

The list refreshes itself on the daemon's `agent-state`, `attention` and
window events. The detail pane re-reads once a second and keeps your scroll
position unless you are following the bottom.

A pane the deck cannot talk to (one on `needs_input`) is refused a prompt;
answer it in tuios.

### Grouping the list

`o` cycles how the panes are grouped: by session, project, task, top-level
task, cwd, harness, state and kind (agents or terminals), then round to
session again. `O` goes the other way, and `group by …` in the palette picks
one by name. Each group is a header row over its panes, in the project's or
task's theme when the group is one; a task header carries the path
`project › top task › … › task`. Panes with nothing to group by sit under
`(none)`, last. The selection stays on the same pane across a regroup.

### The tasks page

| Key | Does |
| --- | --- |
| `j` `k` `g` `G` | Select a project or a task |
| `N` | New project: its name |
| `n` | New task under the selected project, or a new subtask under the selected task |
| `e` | Rename the project or task |
| `s` | Mark the task done, or open again |
| `m` | The next colour of the palette for the row's theme |
| `M` | Set the row's glyph |
| `a` | Assign the pane selected on the deck page to the row |
| `enter` | Back to the deck with the first pane assigned to the row selected |
| `x` | Delete the row, what is under it and their assignments, after a confirmation that counts them |
| `r` | Refresh |
| `esc` | Back to the deck |

### The captures page

| Key | Does |
| --- | --- |
| `j` `k` `g` | Select a capture, newest first |
| `J` `K`, `G` | Scroll the file; `G` follows its end again |
| `enter` | Back to the deck with the pane the capture came from selected, when a pane of that session and name is still open |
| `x` | Delete the file, after a confirmation |
| `r` | Read the folder again |
| `esc` | Back to the deck |

### The command palette

`:` or `ctrl+p` opens the palette over the lower part of the screen. Typing
filters: a name starting with the query ranks first, then a word of it,
then the query anywhere in it, then its letters in order. `↑` `↓` pick,
`tab` puts the picked name in the query, `enter` runs, `esc` closes. Every
entry carries its category:

- `[deck]`, listed first: assign to task…, assign to project…, clear
  assignment, new agent, new terminal, prompt, capture, close pane,
  refresh, go to deck, go to tasks, go to captures, group by each of the
  groupings, new project, new task, new subtask of…, delete capture. Each
  does what its key does.
- `[tuios]`: every verb the daemon lists in `list-verbs`, asked for once per
  run, with its description. Picking one asks for its parameters one field
  at a time, required ones first, with the parameter's description as the
  hint and known values on `tab`: session names, the windows of the session
  typed, the keys of `get-option` and `set-option`, theme names for a
  `.theme` key, and the values a parameter accepts. What you type is typed
  by the verb's contract, ints and bools parsed, arrays and objects as JSON
  or a comma-separated list. A verb with no parameters runs at once.

The result, pretty-printed, or the daemon's error with its code and hint,
takes the place of the detail pane on the deck page under a
`result of <verb> {params}` bar and stays there, with no re-read on the
timer, until the selection or the tab moves.

## Transcript needs the person's presence

The transcript tab reads `agent-transcript`, which the daemon only serves to
the person, through `attach-presence`. The daemon refuses that call from a
process inside one of its own panes. Run `deck tui` or `deck web` from a
terminal outside tuios, or inside a pane of a different daemon, to read
transcripts. Screen and activity work from anywhere. `deck web` asks again
when the daemon drops the presence, as it does across a restart.

## Captures

`c` writes `<session>-<pane>-<timestamp>.txt` under `$TUIOS_DECK_CAPTURES`,
else `$XDG_DATA_HOME/tuios-deck/captures`, else
`~/.local/share/tuios-deck/captures`, the folder the deck's `deck.json`
lives in. The captures page (`C`) lists that folder newest first, shows a
file and deletes one; the web page has the same.

## Tests

`scripts/e2e.sh` runs the TUI against two throwaway daemons under a scratch
folder: one holds the panes it manages, the other runs the deck in a pane and
drives it with `send-keys`. It seeds a `deck.json` by hand and points the
deck at it through `TUIOS_DECK_DATA`, so your own is never touched.
`scripts/e2e-web.sh` runs `deck web` against a throwaway daemon the same
way and checks the API with curl, the event stream, and the WebSocket pane
stream with `scripts/wscheck`. `scripts/e2e-setup.sh` runs
`deck setup` against a scratch HOME and drives the omp extension under bun
with a fake pi and a fake tuios. Every check keeps what it saw under
`$SCRATCH/out` and prints PASS or FAIL.

```sh
go build -o .dev/deck ./cmd/deck
scripts/e2e.sh && scripts/e2e-web.sh && scripts/e2e-setup.sh
```

## Releasing

```sh
scripts/release.sh            # bumps the patch:  v0.3.9 → v0.3.10
scripts/release.sh minor      # v0.3.9 → v0.4.0
scripts/release.sh major      # v0.3.9 → v1.0.0
scripts/release.sh v0.5.0     # exactly this version
scripts/release.sh minor --dry-run   # show the version, change nothing
```

With no tag yet, a bare run tags v0.0.1 and `minor` tags v0.1.0. It checks the tree is clean and on an up-to-date `main`, builds and vets,
tags, pushes `main` and the tag, then asks proxy.golang.org for the new
version so `deck update` sees it at once. Nothing else is needed: the Go
module proxy and pkg.go.dev fetch from GitHub on request, and `@latest` is
the newest tag, or the newest commit on `main` while there are no tags.

## Layout

- `internal/daemon`: the JSON client (`Call`, `Subscribe`, `Presence`) and
  typed wrappers for the verbs the deck uses.
- `internal/panes`: the merged view of sessions and agents, and the text
  rendering of activity and transcripts, shared by both front ends.
- `internal/store`: projects, tasks and assignments in `deck.json`, and the
  themes they are drawn in.
- `internal/ui`: the Bubble Tea model, keys, forms, daemon commands and
  view, with the tasks and captures pages, the grouping of the list and
  the command palette.
- `internal/web`: the HTTP API, the event relay, the WebSocket bridge and
  the embedded page in `static/index.html`.
- `internal/capture`: capture files, and the listing of the folder.
- `setup`: the files the deck installs on a machine, embedded, and the code
  that installs, checks and removes them.
- `cmd/deck`: the binary, with the `tui`, `web` and `setup` commands.
