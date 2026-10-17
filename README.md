# tuios-deck

A dashboard over the [tuios](https://tuios.dev) daemon, as a terminal program
(`deck tui`) and as a web page (`deck web`). Both list every terminal and agent
the daemon holds, start new ones with a first prompt, queue prompts, follow
each pane's screen, activity and transcript, and save captures to files. The
web page also shows a pane live, in xterm.js, and types into it.

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
 j/k select · 1/2/3 tab · J/K scroll · G follow · n agent · t terminal · p prompt · c capture · x close · r refresh · q quit
```

## Build

Needs Go 1.26 and a running tuios daemon (`tuios start-server`, or any
session).

```sh
go build -o deck ./cmd/deck
./deck                       # prints the commands
./deck tui                   # $TUIOS_SOCKET, or the per-user runtime folder
./deck tui --socket /path/to/tuios.sock --captures ./captures
./deck web                   # http://127.0.0.1:8787
./deck web --listen 127.0.0.1:9000 --socket /path/to/tuios.sock
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

The buttons do what the TUI's keys do: New agent, New terminal, Prompt,
Capture, Close. `j` `k` move the selection and `1` to `4` switch tabs when
the focus is not in the terminal or a field. The working directory fields
have a Browse button that walks the folders of the machine the daemon runs
on, through the server, since a browser's own picker cannot name a path
there.

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
POST   /api/agents                             start-agent: {"session","agent","name","cwd","prompt","ready_timeout"}
POST   /api/terminals                          new-window with an interactive shell, creating the session first: {"session","name","cwd"}
```

## The TUI's keys

| Key | Does |
| --- | --- |
| `j` `k` `g` | Select a pane |
| `1` `2` `3`, `tab` | Screen, activity or transcript of the selected pane |
| `J` `K`, `G` | Scroll the detail; `G` follows the newest output again |
| `n` | Start an agent: session, agent command, name, cwd, first prompt. Waits until it is at its prompt |
| `t` | Open a terminal: session, name, cwd. Always an interactive shell, the pane tuios itself opens; send commands to it with `p`. Creates the session when it does not exist |
| `p` | Send a prompt. An agent gets it through `queue-prompt`, typed when the agent is at rest; a plain terminal gets the text and Enter |
| `c` | Save the pane's screen and scrollback to a file |
| `x` | Close the pane, after a confirmation |
| `r` | Refresh |
| `q` | Quit; the panes stay |

The list refreshes itself on the daemon's `agent-state`, `attention` and
window events. The detail pane re-reads once a second and keeps your scroll
position unless you are following the bottom.

A pane the deck cannot talk to (one on `needs_input`) is refused a prompt;
answer it in tuios.

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
`~/.local/share/tuios-deck/captures`.

## Tests

`scripts/e2e.sh` runs the TUI against two throwaway daemons under a scratch
folder: one holds the panes it manages, the other runs the deck in a pane and
drives it with `send-keys`. `scripts/e2e-web.sh` runs `deck web` against a
throwaway daemon and checks the API with curl, the event stream, and the
WebSocket pane stream with `scripts/wscheck`. `scripts/e2e-setup.sh` runs
`deck setup` against a scratch HOME and drives the omp extension under bun
with a fake pi and a fake tuios. Every check keeps what it saw under
`$SCRATCH/out` and prints PASS or FAIL.

```sh
go build -o .dev/deck ./cmd/deck
scripts/e2e.sh && scripts/e2e-web.sh && scripts/e2e-setup.sh
```

## Layout

- `internal/daemon`: the JSON client (`Call`, `Subscribe`, `Presence`) and
  typed wrappers for the verbs the deck uses.
- `internal/panes`: the merged view of sessions and agents, and the text
  rendering of activity and transcripts, shared by both front ends.
- `internal/ui`: the Bubble Tea model, keys, forms, daemon commands and view.
- `internal/web`: the HTTP API, the event relay, the WebSocket bridge and
  the embedded page in `static/index.html`.
- `internal/capture`: capture files.
- `setup`: the files the deck installs on a machine, embedded, and the code
  that installs, checks and removes them.
- `cmd/deck`: the binary, with the `tui`, `web` and `setup` commands.
