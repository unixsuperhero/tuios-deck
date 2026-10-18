# Feature inventory, review and documentation

Status: proposed, 2026-10-18. Not started.

## Problem

The deck has grown by feature without a list of what it has. Nobody can say
which features have been reviewed, which are worth keeping, or what every
page, modal, palette command and struct is for. The concrete case: `o`/`O`
cycle through 12 rail groupings and most of them are never useful, but there
is no place to mark one as cut and no way to find the rest of the same kind.

Three related TODO.md items fall out of the same gap: documentation of every
struct, page, concept and action; a `deck` subcommand that lists the key
actions; `deck config init` writing an entry for every possible value.

## What is enumerable today

| Thing | Count | Already data? | Where |
|---|---|---|---|
| Pages | 7 | enum | `internal/ui/model.go` `page` |
| Grouping presets (o/O) | 12 | slice | `internal/ui/grouping.go` `presets` |
| Grouping levels | 9 + tree + none | enum | `internal/ui/grouping.go` `level` |
| Named key actions | 49 (35 deck, 6 live, 8 form) | table | `internal/config/keys.go` `actions` |
| Deck palette commands | ~13 | slice literal | `internal/ui/palette.go` `deckCommands` |
| Daemon verbs in palette | all of `list-verbs` | runtime | daemon |
| Modals with context help | 11 | slice | `internal/ui/help_context.go` `contextHelpModes` |
| Web routes | 61 | code | `internal/web/server.go` |
| Daemon verb wrappers | 18 | code | `internal/daemon/verbs.go` |
| Structs | 126 (19 in store) | code | everywhere |
| feat / fix commits | 75 / 33 | git | `git log` |

The actions and presets are already tables. Pages, palette commands, modals
and routes are code. The plan turns the rest into data too, in two stages.

## The shape

Every feature has a stable id, a kind, and a review verdict. Ids are
`<kind>.<name>`, lower case, dots between parts, never renamed once written.

```js
// inventory.json: generated from the source, never edited
{ id: "grouping.cwd", kind: "grouping",
  name: "cwd", desc: "group the rail by working directory",
  surfaces: ["tui:o/O"], file: "internal/ui/grouping.go", line: 108,
  hash: "a91f…" }                       // of the defining text, for "changed since review"

{ id: "palette.deck.queued-prompts", kind: "palette",
  name: "queued prompts…", desc: "choose, edit, delete and start the queued prompts",
  surfaces: ["tui:palette"], file: "internal/ui/palette.go", line: 342, hash: "…" }

{ id: "struct.store.Task", kind: "struct",
  fields: [{ name: "ProjectID", type: "string", doc: "only on a top-level task" }, …],
  file: "internal/store/store.go", line: 33, hash: "…" }

// review.yaml (stage 1) → features table in the store (stage 2): edited by the person
{ "grouping.cwd":                { status: "cut",  on: "2026-10-18", note: "never useful" },
  "palette.deck.queued-prompts": { status: "keep", on: "2026-10-18" } }
// status: keep | cut | unreviewed (absent). A mark whose hash no longer matches is "changed".
```

Kinds: `page`, `grouping`, `level`, `action` (one per mode), `palette`,
`modal`, `route`, `verb`, `struct`, `concept`. A concept is a feature that is
not one symbol, such as "a task notification continues the turn"; it is
declared with a `// feature: concept.review.notification-continues-turn`
comment on the code that implements it, and the extractor collects those.

## Stage 1: inventory generated from the source, marks beside it

Goal: every feature in the source appears in `docs/inventory.json`, and
anything without a mark is unreviewed by definition.

1. `scripts/inventory/` is a Go program (`go/ast`, `go/doc`) run as
   `go run ./scripts/inventory > docs/inventory.json`. Extractors in order of
   payoff, each one a function returning `[]Feature`:
   - actions: walk the `actions` map literal in `internal/config/keys.go`.
   - presets and levels: the `presets` slice and the `level` `String` array.
   - pages: the `page` `String` array in `internal/ui/model.go`.
   - palette: the `palItem` literals in `deckCommands`.
   - modals: `contextHelpModes`.
   - routes: `HandleFunc` calls in `internal/web/server.go`, method and path.
   - verbs: exported functions of `internal/daemon/verbs.go`.
   - structs: every exported struct under `internal/`, fields and doc comments.
   - concepts: `// feature:` comments anywhere.
   → verify: the counts in the table above come out of the program, and
   `go run ./scripts/inventory | jq length` is stable across two runs.
2. `docs/review.yaml` holds the marks. `scripts/inventory -review` prints the
   backlog: unreviewed ids, then changed ids (hash differs from the mark's).
   → verify: with an empty review file every id is listed; after marking
   `grouping.cwd` cut it leaves the list; after editing the preset's text it
   returns as changed.
3. `scripts/docs/` renders `docs/inventory.json` plus one prose file per kind
   (`docs/features/<kind>.md`, hand-written for what the parser cannot see)
   into `docs/reference/`. The struct section comes from the extractor's doc
   comments, so a struct with no comment renders as a visible gap.
   → verify: the rendered reference names every id in the inventory, checked
   by the renderer itself (exit status is the count of ids missing prose).
4. A pre-commit hook regenerates `docs/inventory.json`, so `git diff` of that
   file per commit is the changelog of features. No commit trailers needed.
5. Seed the review: one pass over the backlog marking keep/cut. The seven
   groupings that are not useful are cut here, which is a record, not yet a
   behaviour change.

Stage 1 changes no behaviour. Cutting a feature is still a code change made
by hand from the cut list.

## Stage 2: the registry lives in the app and gates the features

Goal: cutting a feature is a keypress, and the review happens where the
feature is met.

1. `internal/feature/`: `Register(id, kind, name, desc)` builds the registry
   at init, `All()` lists it, `Status(id)` reads the mark. The stage 1
   extractor output seeds the registrations; once every surface registers,
   `deck features` prints the registry and the parser is kept only for structs
   and routes.
2. Marks move from `docs/review.yaml` into the store (`features` table in
   `deck.json`, or sqlite when that migration lands). Every mutating method
   locks and saves whole, like the rest of the store.
3. The four surfaces that are already tables consult the registry: `presets`
   skip cut groupings when `o`/`O` cycle, `deckCommands` and the help omit
   cut commands, `config init` writes every registered action. A cut feature's
   code stays until the person deletes it.
   → verify: an e2e check that seeds `grouping.cwd` as cut in `deck.json`
   and asserts the `o` cycle never shows `cwd` in the title line.
4. A Features page in the TUI (new `page` constant, per the AGENTS.md
   recipe): rows by kind, `k` keep, `x` cut, `u` unreview, `?` opens the help
   row for the feature. The help and palette show a glyph on anything
   unreviewed.
5. `deck docs` replaces `scripts/docs/`: the registry plus `go doc` rendered
   to markdown, so docs cannot drift from the binary.

## Alternatives considered

- **A hand-kept `features.yaml` and a `deck features` CLI, nothing in the
  app reads it.** Simplest and lowest blast radius; coverage is not enforced,
  so it drifts the first time a palette command lands without an entry.
  Rejected as the whole plan, but its commit trailer idea (`Feature: <id>`)
  is available if the inventory diff proves too noisy as a changelog.
- **Stage 2 alone, without stage 1.** The registry is the right end state,
  but registering 126 structs and 61 routes by hand is the same work as the
  extractor with none of the drift check. Stage 1 seeds it.

## Open questions for the person

- Does the review verdict belong per machine (in `deck.json`) or per repo
  (in `docs/review.yaml`)? Stage 1 says repo, stage 2 says store. A cut
  grouping is personal; a cut page is a project decision. Possibly both: the
  store holds the person's cuts, the repo holds the project's.
- Should `cut` ever delete code automatically? This plan says no: the cut
  list is the deletion backlog, and deletion is a commit.
