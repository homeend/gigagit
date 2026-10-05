# Note store partitions — design

Status: agreed in brainstorm 2026-10-04, awaiting spec review.
Spec 1 of 2. Spec 2 (`2026-10-04-working-reviews-design.md`) stores
working-changes reviews on top of this layout.

## 1. Goal

Every note — line notes, commit/branch/range reviews, preview notes, shelf
notes, and (spec 2) working-changes reviews — keeps living in ONE note store
behind ONE engine (`notes.Store` + the domain read/write models). Only the
storage splits: the store keeps several files, so a read that cares about one
kind of anchor (the working tree on every status refresh) does not parse every
other note in the repo.

Non-goals: changing what a note is, how it resolves, or where it shows. No
frontend change beyond what the store API forces.

## 2. Today

One per-repo `notes.toml` under `$XDG_STATE_HOME/gg/notes/<repoKey>/`
(`internal/domain/notesstore.go`), one `notes.toml.lock`, one process mutex,
one `[notes] max_entries` cap (default 2000; entry-level notes exempt,
`capOldestFirst`). Every read calls `Store.Load()` and filters the whole set
(18 call sites in `internal/domain`). A corrupt `notes.toml` takes every note
surface down until quarantined.

## 3. Routing — `notes.PartOf`

A pure, total function on `model.Note` decides a ROOT note's file. A reply
always goes to its root's file: it copies the root's address but carries no
`Preview` of its own (`loadPreviewNotes`: "a reply carries no scope"), so
`PartOf` alone would send a preview note's reply to `commits`. The store
routes a reply by looking its parent up; a reply whose parent is gone falls
back to `PartOf` (and the orphan prune drops it, as today).

| Note | Part | File |
|---|---|---|
| no commit and no shelf id (`worktreeScopedNote`: staged / unstaged / untracked) — line notes on live files, and spec 2's working reviews | `wt-<key>` | `worktrees/<key>.toml` |
| a shelf id (`Address.State == shelf`) | `shelf` | `shelf.toml` |
| committed AND `Preview` is a merge-preview scope (contains `...`) | `previews` | `previews.toml` |
| every other committed note: plain commit line notes, commit/branch reviews, commit-pair notes (`Preview` = `a7..b7`), pull-request notes (their `Preview` is empty — `PreviewNoteSet.scope()`) | `commits` | `commits.toml` |

`<key>` = the first 8 bytes of sha256 over the cleaned worktree top-level
path, hex — the formula `repoKey` uses for the repo directory today. A
live-state note with an empty `Address.Worktree` (written before worktrees
were recorded) routes to `worktrees/unscoped.toml`: such notes already match
no checkout today (`sameWorktreePath` rejects ""), so they stay stored and
invisible exactly as now, and age out through the sweep.

The routing table is pinned by a table test over every `FileState` and
`Preview` shape; a new `FileState` without a row fails it.

## 4. Engine API

```go
type Part string // "commits", "previews", "shelf", "wt-<key>"

func PartOf(n model.Note) Part
func WorktreePart(top string) Part // "wt-<key>", "wt-unscoped" for ""

type Store interface {
	Load(p Part) ([]model.Note, error) // one file; never writes
	LoadAll() ([]model.Note, error)    // every file in the directory
	Put(n model.Note) error            // routes by PartOf; add or replace by ID
	Remove(id string) error            // finds the id across parts; a root takes its replies
	Sweep(keep func(model.Note) bool) (dropped int, err error) // per part, each under its own lock
	SetPolicy(p Policy)
}
```

- **Ids stay unique across parts.** `NewID` is minted against `LoadAll()`.
  `NoteGet` / `NoteEdit` / `NoteReply` / `NoteRemove` keep taking an id alone
  and scan the parts (user-paced, one read per file).
- **A note never moves between parts.** An address is fixed at creation
  (re-anchoring changes only `Range`). `Put` of an existing id whose computed
  part differs from where it is stored is an error, not a silent move.
- **Locks.** One `<file>.lock` (internal/filelock) and one process mutex PER
  PART. A mutation locks only the file it changes. `Remove` of a root locks
  one file (its replies live in the same file).
- **Cap.** `[notes] max_entries` applies to EACH part. Entry-level notes and
  their replies stay exempt (`capOldestFirst` unchanged).
- **Corruption.** `ErrCorrupt` and `Quarantine` are per part: one bad file is
  moved aside alone; the others keep working. `LoadAll` returns the parts it
  could read plus a joined error naming the bad ones; callers that today fail
  on a corrupt store keep failing the same way.
- **An emptied file is deleted.** A mutation that leaves a part with no
  notes removes its file, so a removed worktree leaves nothing behind.

## 5. Readers

Each domain read names the parts it needs:

| Reader | Parts |
|---|---|
| `NotesFor` / `NotesAt` / `NotesClear` on a live address | own `wt-<key>` |
| `NotesFor` / `NotesAt` on a commit address (a commit's notes include the ones written in a scope; `PlainNotes` filters later) | `commits` + `previews` |
| `NotesFor` / `NotesAt` on a shelf address | `shelf` |
| review read model (`reviewNotes`, `Review*`), branch delete/rename follow-up | `commits` |
| preview view (`loadPreviewNotes`): merge-preview scope (`...`) | `previews` |
| preview view: commit-pair scope (`a7..b7`) | `commits` |
| preview view: pull-request set (empty scope — gathers every note on its commits) | `commits` + `previews` |
| `NoteCounts`, `NoteAddresses`, `NotesOverview` — they already hide other worktrees' notes | the VISIBLE parts: `commits` + `previews` + `shelf` + own `wt-<key>` |
| id lookups (`NoteGet`/`Edit`/`Reply`/`Remove`), `NewID`, sweep | `LoadAll()` |

Other worktrees' files are read only by id lookups and the sweep.

## 6. Worktree lifecycle

- The main checkout is a worktree like any other and has its own file.
- **Deleted worktree:** its live notes resolve orphaned (their file is gone)
  and the sweep drops them; the emptied file is deleted with them (§4).
  (Amended after the final review: a `git worktree list` rule was dropped —
  git lists a submodule's or `--separate-git-dir` repo's main checkout by its
  git dir, so the rule deleted that checkout's notes.) Timid like today's
  sweep: a failed or cancelled list read removes nothing.
- **Recycled worktree** (same path, new branch): the file stays. Its line
  notes go stale and the existing sweep rules drop them; spec 2's working
  reviews are protected by their checksums.

## 7. Migration

Follows the saved-previews precedent (`FeaturePreviews`,
`internal/domain/features.go`):

- New store id `StoreNotes`; requirement `preflight.LegacyStore{Store:
  StoreNotes}` — fires whenever `notes.toml` exists.
- `Migration{Store: StoreNotes, From: 1, To: NotesFormat (2), Action:
  "split-notes", Lossless: true}` — runs at startup without asking.
- Action, under `notes.toml.lock`: read `notes.toml` → route every note with
  `PartOf` → merge into its part by id (the newer `Updated` wins) → rename
  `notes.toml` to `notes.toml.migrated-<unix>` (a backup; never deleted).
- **Idempotent.** An older gg still running (an installed `gg mcp`, a
  worker's `gg note add`) may recreate `notes.toml`; the next start merges it
  again. Nothing is lost, so no format range has to lock old binaries out.
- A corrupt `notes.toml` is not migrated: it is quarantined (today's
  behaviour) and the migration reports it.
- Besides the startup migration, every store resolution converts a legacy
  file: a TUI or hosted-web repo switch and `gg mcp` open a repo without
  running `RunAutoMigrations`.

## 8. Testing

- `internal/notes`: `PartOf` table test; per-part Load/Put/Remove/Sweep;
  cross-part id uniqueness; per-part cap; per-part quarantine with the other
  parts still readable; `Put` refusing a part change.
- `internal/domain`: each reader reads only its parts (a store fake that
  records which parts were loaded); migration from a real `notes.toml` with
  every note kind (lossless, re-run merges, backup kept); deleted-worktree
  sweep against real `git worktree remove`.
- Existing TUI/web/CLI/MCP note tests pass unchanged — the API change
  is internal to domain.
- No e2e scenario: the harness cannot seed a state file before a run. A
  domain integration test over a real repo covers it instead — a legacy
  `notes.toml` holding every note kind, `RunAutoMigrations`, then each note
  is found by the reader that showed it before (`NotesAt`, `NoteCounts`,
  `PreviewNotes`, `reviewNotes`, `NoteGet`).
