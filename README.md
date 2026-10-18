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
  ◐ deck command palette  reviewer   working│ you> review the diff on this branch
  ● deck captures page  tests    needs_input│ → Bash go test ./...
  · !!!! build                              │   ok Bash
▾ side                                      │ ■ turn ended
  ○ web  landing  helper                idle│
captured to ~/.local/share/tuios-deck/captures/work-build-20261017-133455.txt
 j/k move · l main area · {/} group · (/) level · [/] or 1-4 tab · J/K scroll · G follow · o group · n agent · t term · p prompt · a assign · c capture · i context · x close · T tasks · C captures · I context · r refresh · q quit
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

## Projects, tasks and assignments

A project holds top-level tasks; a task holds subtasks, to any depth, in
the same table. A subtask stores no project of its own: its project is its
root task's.

A project's or a task's status is `open`, `done` or `skipped`. Done and
skipped are final: a final task no longer counts as open, and a final
project or task no longer holds its colour. The tasks page marks a done row
`✓` and a skipped one `⊘`. A project written before it had a status reads
as open.

Every project and every task has its own theme, one of the 16 ANSI colours
and one glyph, drawn before its name in the list, on the tasks page and on
the web page. A new one takes a colour at random from the free ones, those
no open project or task holds, with `◆` for a project and `●` for a task,
until you change it. When every colour is held it takes any of them; black
and bright-black are never given out on their own. `m` on the tasks page
steps through the free colours first, then the rest of the palette.

Every agent and terminal the deck starts is assigned to a task or, failing
that, to a project: the `n` and `t` forms ask for one after the cwd, with
the known task paths (`project / top task / … / title`) and project names
listed under the field, and refuse a pane with neither. A task implies its
project. `a` assigns, or reassigns, a pane already open; both fields left
empty drop the assignment. The assignment of a pane that is gone from two
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
  ]
}
```

Deleting a task takes its subtasks with it, deleting a project takes its
tasks, and either takes the assignments that pointed at any of them. A
context item that pointed at one keeps its place and loses the reference.

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

Each item may be starred, pinned and assigned to a task or a project, the
way a pane is.

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

## Transcript needs the person's presence

The transcript tab reads `agent-transcript`, which the daemon only serves to
the person, through `attach-presence`. The daemon refuses that call from a
process inside one of its own panes. Run `deck tui` or `deck web` from a
terminal outside tuios, or inside a pane of a different daemon, to read
transcripts. Screen and activity work from anywhere. `deck web` asks again
when the daemon drops the presence, as it does across a restart. Without
presence the Turns tab reads an agent's prompts from its activity instead,
and context extraction leaves the transcript out.

## The TUI

The TUI has four pages. The deck page is the one at the top of this file:
the panes on the left, the selected pane's screen, activity, transcript or
turns on the right. On every page `T` opens the tasks page, `C` the
captures page, `I` the context page, `U` the prototypes page, `A` the
mass-add screen, `:` or
`ctrl+p` the command palette, and `q` quits; `esc` comes back to the deck.
The title line names the page and, on the deck, the grouping when it is not
by session; on the context page, its sort, grouping and filter.

### The deck page

Each pane's row in the left rail carries its state dot, the first four
letters of its project in the project's colour, its task's title in the
task's colour, its name, and its state flush right. A narrow rail cuts the
task title first, then the name. A pane assigned to nothing shows `!!!!`
in place of the project, and its name, in bold black on red, so it stands
out until `a` gives it a home.

The focus is on the rail or on the main area; `h` and `l` move it. The
selected pane is in reverse video while the rail has focus and keeps a dim
`▸` while the main area has it, when the tab bar brightens and underlines
the shown tab. The help line names the keys of the focused area.

With the rail focused:

| Key | Does |
| --- | --- |
| `j` `k` (`↓` `↑`), `g` | Select a pane, or the first one |
| `{` `}` | The first pane of the previous or next group at the same depth; stops at the ends |
| `(` `)` | With nested groups: `(` makes `{` `}` jump between the outer groups and selects the first pane of the one holding the selection; `)` steps back in a level. The status line names the level `{` `}` now jump by |
| `l` (`→`) | Focus the main area |
| `1` `2` `3` `4`, `[` `]`, `tab` `shift+tab` | Screen, activity, transcript or turns of the selected pane |
| `J` `K` (`ctrl+d` `ctrl+u`, `pgdown` `pgup`), `G` | Scroll the detail half a page; `G` follows the newest output again |
| `o` `O` | Group the list the next way, or the previous one (below) |
| `n` | Start an agent: session, agent command, name, cwd, task or project, first prompt. Waits until it is at its prompt |
| `t` | Open a terminal: session, name, cwd, task or project. Always an interactive shell, the pane tuios itself opens; send commands to it with `p`. Creates the session when it does not exist |
| `p` | Send a prompt. An agent gets it through `queue-prompt`, typed when the agent is at rest; a plain terminal gets the text and Enter |
| `a` | Assign the pane to a task or a project; both fields empty drop the assignment |
| `c` | Save the pane's screen and scrollback to a file |
| `i` | Extract context items from the pane (above) |
| `x` | Close the pane, after a confirmation |
| `r`, `enter` | Refresh |
| `T` `C` `I` `U` `A` | The tasks page, the captures page, the context page, the prototypes page, the mass-add screen |
| `:` `ctrl+p` | The command palette |
| `q` | Quit; the panes stay |

With the main area focused, these keys change and the rail's selection
stays where it is; every other key does what it does with the rail
focused:

| Key | Does |
| --- | --- |
| `h` (`←`) | Focus the rail |
| `j` `k` | Scroll the detail a line |
| `g` | Go to the top of the detail |
| `J` `K`, `G` | Scroll half a page; `G` follows the newest output again |
| `enter` | Nothing, outside the Turns tab |

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

### Grouping the list

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
name with `(none)` last. The selection stays on the same pane across a
regroup.

### The tasks page

| Key | Does |
| --- | --- |
| `j` `k` `g` `G` | Select a project or a task |
| `{` `}` | The previous or next row at the same depth: project to project, task to task |
| `(` `)` | The parent row (a task's project or parent task), or the first child |
| `N` | New project: its name |
| `n` | New task under the selected project, or a new subtask under the selected task. The form's parent field starts at the row; `ctrl+h` walks it up a level (to the project's top level at most) and `ctrl+l` back down, from any field, so a top-level task is a few keys away from a subtask row |
| `e` | Edit the row in one form: a task's title, parent, status, colour and glyph; a project's name, status, colour and glyph. The parent lists `(top level of <project>)` for every project and every task but the row and its subtasks, so a task moves within its project or to another, subtasks and all. Only what changed is written, and the status line says what that was |
| `s` | Cycle the task's or project's status: open, done, skipped, open |
| `m` | The next colour for the row's theme: the free ones first, then the rest of the palette |
| `M` | Set the row's glyph |
| `a` | Assign the pane selected on the deck page to the row |
| `enter` | Back to the deck with the first pane assigned to the row selected |
| `x` | Delete the row, what is under it and their assignments, after a confirmation that counts them |
| `r` | Refresh |
| `A` | Add many projects, tasks and subtasks at once (below) |
| `esc` | Back to the deck |

### Adding many at once

`A`, on any page, or `add many…` in the palette, opens a multiline form
titled `add projects and tasks`. Its text is an outline:

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
palette`. In this form `tab` indents the line being typed by one level
(two spaces) and `shift+tab` takes one level off it, so an outline can be
typed without counting spaces; `shift+tab` on a line with no indent does
nothing, and neither key moves between fields here. A title already taken
under the same parent is reused rather than added twice, which is how sub-items go under an existing task; a line with
no indent may also name an existing task by its path, `deck / command
palette`, to add under it. Blank lines and lines starting with `#` are
skipped. `ctrl+s` checks the whole outline first: a line indented under
nothing, an indent that jumps more than one level, or an odd number of
spaces is refused with its line number, and nothing is created. Then the
items are created in order, each new one with a free colour from the theme
pool, and the status says `added N projects, M tasks`. The tasks page keeps
its selection.

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
| `f` | Filter by kind, project, task and text; all empty clears the filter |
| `o` `O` | Group the next or the previous way: none, kind, project, task, pane |
| `*` | Star or unstar the item |
| `!` `P` | Pin or unpin it |
| `a` | Assign it to a task or a project; both fields empty clear it |
| `y` | Copy its text through OSC 52, and with `pbcopy`, `wl-copy` or `xclip` too when one is on `PATH` |
| `enter` | Type its text into the pane selected on the deck page, without Enter, so nothing is submitted |
| `ctrl+o` | Open a link, a pull request (`owner/repo#123` as its GitHub URL), a file or a folder with `open`, or `xdg-open` off macOS |
| `w` | Open it with a program: the field lists the platform's opener, `code`, `cursor`, `nvim`, `vim` and `less` when on `PATH`, and takes any other. A program that needs a terminal (vim, less and the like) runs in a new shell pane of the selected pane's session, assigned to the item's task or project; any other runs detached |
| `x` | Delete the item, after a confirmation |
| `r` | Redraw the list and count the items |
| `esc` | Back to the deck |

A prompt or a command has nothing to open; `y` and `enter` work on every
kind.

### Prototypes

`U` (or the palette's "prototypes") opens a page of picker prototypes for
assigning a pane and filtering the context page, so they can be tried on
the real store and panes side by side. Each row names the prototype, what
it drives and how it works; the right side lists its keys. `enter` runs it
for real: an assign row assigns the pane selected on the deck page and
reports it on the status line the way the assign form does, and a filter
row filters the context page and goes there. The first six rows assign,
the next five filter. While a prototype is up the help line names it, and
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
  refresh, go to deck, go to tasks, go to captures, go to context, prototypes, extract
  context from pane, group by each of the presets, group by… for any
  levels, new project, new task, new subtask of…, add many…, delete
  capture. Each does what its key does.
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
| `ctrl+u` `ctrl+w` | Clear the field, delete the last word |
| `esc` | Cancel |

A field with known values (tasks, projects, sessions, windows, option keys,
themes, accepted values, programs, grouping levels) lists up to 8 of them
under it, filtered as you type the way the palette filters. `ctrl+n`
`ctrl+p` (or `↓` `↑`) move the highlight, `tab` takes the highlighted
value, and `enter` takes it too unless what you typed is already one of the
values.

Every project or task field (new agent, new terminal, assign, the palette's
assign, new task and new subtask entries) ends its list with `+ add new…`.
Taking it puts the form aside and opens the mass-add screen with what you
typed: as a project line in a project field, or as a task under the
selected pane's project in a task field. `ctrl+s` there adds the outline
and brings the form back with the field set to the first project or task
it added (a task as its path); `esc` brings the form back unchanged.

The prompt (`p`), the new agent's first prompt and the mass-add outline are
multiline: `enter` starts a new line, `ctrl+s` (or `ctrl+enter` where the
terminal reports it) submits, and a paste keeps its newlines.

In a multiline field `@` at the start of a word lists what is in the
selected pane's cwd (the deck's own when no pane is selected): folders with
a trailing `/`, files plain, hidden ones left out, at most 200. Typing
after the `@` filters the list; `tab` or `enter` on a folder opens it, on a
file puts its path, relative to the cwd, in place of the `@` and what
followed it. `esc` closes the list and keeps the form. A symlink that leads
outside the cwd is not listed.

A confirmation (closing a pane, deleting a row, a capture or a context
item) asks on the status line and defaults to yes, `(Y/n)`: `enter` or `y`
confirms, `n` or `esc` cancels, and any other key does nothing.

### Help lines

The bottom line names the keys of what has focus. On the deck page, rail
focused:

```
 j/k move · l main area · {/} group · (/) level · [/] or 1-4 tab · J/K scroll · G follow · o group · n agent · t term · p prompt · a assign · c capture · i context · x close · T tasks · C captures · I context · r refresh · q quit
```

Main area focused, and main area focused on the Turns tab:

```
 main area: h rail · j/k scroll · J/K half page · g top · G follow · [/] or 1-4 tab · p prompt · c capture · T tasks · C captures · q quit
 main area: h rail · j/k row · enter/space expand · y copy · {/} group · g top · G newest · J/K half page · [/] or 1-4 tab · p prompt · T tasks · q quit
```

The tasks, captures, context and prototypes pages:

```
 j/k select · {/} group · (/) level · N project · n task/subtask · A add many · e edit · s status · m colour · M glyph · a assign pane · enter go to pane · x delete · esc deck · q quit
 j/k select · J/K scroll · G follow · enter go to pane · x delete · r refresh · T tasks · esc deck · q quit
 j/k select · {/} group · s sort · f filter · o/O group · * star · ! pin · a assign · y copy · enter insert · ^o open · w open with · x delete · esc deck · q quit
 j/k select · enter try it for real · T tasks · I context · esc deck · q quit
```

A form, a confirmation, the palette and a running prototype:

```
 <form title> · the keys are in the box · esc cancel
 enter/y confirm · n/esc cancel
 type to filter · ↑/↓ ctrl+p/n pick · enter run · tab complete · esc close
 <assign <pane> | filter context> · <prototype> · the keys are in the box · ctrl+c cancel
```

Inside a prototype the box's last line names its keys:

```
type to filter · ctrl+n/ctrl+p move · enter pick · ? preview · esc cancel
j/k move · l open · h close · / filter · enter pick · ? preview · esc cancel
type to filter · ↓/↑ move · enter pick · ? preview · esc clear the filter
1-9 pick · 0 more… · j/k and enter · esc cancel
j/k move · l/h next/previous column · enter pick · ? preview · esc cancel
/ or tab lock · backspace pops · ctrl+n/p move · enter pick · ? preview · esc cancel
```

Inside a form box the bottom line follows the current field:

```
1/6 · enter next · tab default · ctrl+s submit · shift+tab back · esc cancel
6/6 · enter submit · tab default · shift+tab back · esc cancel
2/6 · ctrl+n/ctrl+p pick · tab accept · enter accept or next · shift+tab back · esc cancel
6/6 · enter newline · ctrl+s submit · @ a file · shift+tab back · esc cancel
1/1 · enter newline · tab/shift+tab indent · ctrl+s submit · @ a file · esc cancel
6/6 · ctrl+n/ctrl+p pick · tab or enter take the path, or open the folder · esc close the list
```

## The web page

`deck web` serves one page and a JSON API. The left list and the state
badges follow the daemon's event stream; the detail re-reads every two
seconds without touching the list, the selection, an open dialog or a
scroll position that is not at the bottom. Its rows read like the TUI's:
the project's first four letters and the task title in their theme
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
and the panes assigned to the selected row. The captures page lists the
folder and shows a file. The context page filters by kind, project, task,
text, starred and pinned, sorts and groups the way the TUI's does, and has
Copy (to the browser's clipboard), Insert, Open, Star, Pin, Assign and
Delete.

The `⌘` button, `ctrl+k` or `ctrl+p` open the command palette, with the
same two categories as the TUI's; `ctrl+n` `ctrl+p` (or `↓` `↑`) move in
it and `esc` closes it. A tuios verb's parameters become a form, required
ones first, each with its description as the placeholder and the sessions
and windows offered as you type; the result replaces the detail text under
a `result` tab until another tab or pane is picked. When the focus is not
in the terminal or a field, `o` `O` cycle the grouping, `T` `C` `I` and
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
PATCH  /api/projects/{id}                      {"name"}, {"status": "open"|"done"|"skipped"}, {"theme": {"color", "glyph"}}, either half of the theme alone
DELETE /api/projects/{id}                      with its tasks and their assignments; the answer counts them
GET    /api/tasks?project=                     {"tasks": [{id, parent_id, project_id, title, path, depth, theme, status, panes, …}]}
POST   /api/tasks                              {"project_id", "title"} for a top-level task; {"parent_id", "title"} for a subtask; "theme"?
PATCH  /api/tasks/{id}                         {"title"}, {"status": "open"|"done"|"skipped"}, {"theme": {…}}; a move: {"parent_id"} or {"parent": path} under a task, {"project_id"} or {"project": name} to a project's top level
DELETE /api/tasks/{id}                         with its subtasks and their assignments
GET    /api/assignments                        {window id: {session, task_id, project_id, resolved: {project, task, top}}}
PUT    /api/panes/{session}/{id}/assignment    a target; names resolve the way the TUI's forms do
DELETE /api/panes/{session}/{id}/assignment    drop it
GET    /api/captures                           the folder, newest first: {name, path, size, modified, session, pane, kind}; kind is "turn", "cmd" or ""
GET    /api/captures/{name}                    {"name", "content", "lines"}
DELETE /api/captures/{name}                    remove the file
GET    /api/context?kind=&project=&task=&starred=&pinned=&q=
                                               {"items": [{…the stored item, project, task}], "kinds"}; project and task by id or name, q in the text
POST   /api/panes/{session}/{id}/context/extract
                                               gather and store the pane's items: {"status", "new", "found", "kinds", "sources"}
PATCH  /api/context/{id}                       {"starred"}, {"pinned"}, a target; a target sent empty clears it
DELETE /api/context/{id}                       remove the item
POST   /api/context/{id}/insert                {"session", "window"}: send-text without Enter
POST   /api/context/{id}/open                  {"program"?}: open it on the server's machine, detached; the opener by default, a terminal program refused
GET    /api/palette                            the 16 colour names with their CSS hex
GET    /api/verbs                              list-verbs, as the daemon answers it
POST   /api/call                               {"verb", "params"}: one daemon verb, refused unless list-verbs named it
```

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
SCRATCH=/tmp/deck-e2e-mine PORT=18799 scripts/e2e-web.sh   # a second run beside the first
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
- `internal/store`: projects, tasks, assignments and context items in
  `deck.json`, and the themes they are drawn in.
- `internal/context`: finding context items in a pane, and copying and
  opening them.
- `internal/autocapture`: the automatic captures at a turn's or a
  command's end.
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
