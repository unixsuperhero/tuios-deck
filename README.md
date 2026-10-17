# tuios-deck

A terminal dashboard over the [tuios](https://tuios.dev) daemon. It lists
every terminal and agent the daemon holds, starts new ones with a first
prompt, queues prompts, follows each pane's screen, activity and transcript,
and saves captures to files.

It owns no PTYs. Everything goes through the daemon's JSON verb protocol on
its unix socket, so the panes persist when the deck quits and are the same
panes you see in `tuios attach`.

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
./deck                       # $TUIOS_SOCKET, or the per-user runtime folder
./deck --socket /path/to/tuios.sock --captures ./captures
```

## Keys

| Key | Does |
| --- | --- |
| `j` `k` `g` | Select a pane |
| `1` `2` `3`, `tab` | Screen, activity or transcript of the selected pane |
| `J` `K`, `G` | Scroll the detail; `G` follows the newest output again |
| `n` | Start an agent: session, agent command, name, cwd, first prompt. Waits until it is at its prompt |
| `t` | Open a terminal: session, name, cwd, optional command. Creates the session when it does not exist |
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
process inside one of its own panes. Run the deck from a terminal outside
tuios, or inside a pane of a different daemon, to read transcripts. Screen
and activity work from anywhere.

## Captures

`c` writes `<session>-<pane>-<timestamp>.txt` under `$TUIOS_DECK_CAPTURES`,
else `$XDG_DATA_HOME/tuios-deck/captures`, else
`~/.local/share/tuios-deck/captures`.

## Tests

`scripts/e2e.sh` runs the deck against two throwaway daemons under a scratch
folder: one holds the panes it manages, the other runs the deck in a pane and
drives it with `send-keys`. Every check writes the deck's screen to
`$SCRATCH/out` and prints PASS or FAIL.

```sh
go build -o .dev/deck ./cmd/deck && scripts/e2e.sh
```

## Layout

- `internal/daemon`: the JSON client (`Call`, `Subscribe`, `Presence`) and
  typed wrappers for the verbs the deck uses.
- `internal/ui`: the Bubble Tea model, keys, forms, daemon commands and view.
- `internal/capture`: capture files.
- `cmd/deck`: the binary.
