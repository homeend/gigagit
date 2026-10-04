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

A pure, total function on `model.Note` decides the file. A reply copies its
root's address (`model.Note` contract), so it always lands with its root.

| Note | Part | File |
|---|---|---|
| `Address.State` ∈ {staged, unstaged, untracked} — line notes on live files, and spec 2's working reviews | `wt-<key>` | `worktrees/<key>.toml` |
| `Address.State == shelf` | `shelf` | `shelf.toml` |
| committed AND `Preview` is a merge-preview scope (contains `...`) | `previews` | `previews.toml` |
| every other committed note: plain commit line notes, commit/branch reviews, commit-pair notes (`Preview` = `a7..b7`), pull-request notes (their `Preview` is empty — `PreviewNoteSet.scope()`) | `commits` | `commits.toml` |

`<key>` = `repoKey(<worktree top-level path>)` — the same hash that names the
repo directory today. A live-state note with an empty `Address.Worktree`
(written before worktrees were recorded) routes to the MAIN worktree's file.

The routing table is pinned by a table test over every `FileState` and
`Preview` shape; a new `FileState` without a row fails it.

## 4. Engine API

```go
type Part string // "commits", "previews", "shelf", "wt-<key>"

func PartOf(n model.Note, mainWorktree string) Part

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
- **Worktree file header.** A `worktrees/<key>.toml` records
  `worktree = "<abs path>"` so `LoadAll` callers can label it and the sweep
  can test it without reversing the hash.

## 5. Readers

Each domain read names the parts it needs:

| Reader | Parts |
|---|---|
| Files panel badges, working-tree diffs, `NotesFor`/`NotesAt` on a live address | own `wt-<key>` |
| commit list badges, commit view, range review rows, review read model (`reviewNotes`, `Review*`) | `commits` |
| preview view (`PreviewNotes*`, `PreviewNoteCounts`) | `previews` (+ `commits` for a commit-pair set) |
| shelf view | `shelf` |
| `NoteCounts` | `commits` + own `wt-<key>` + `shelf` |
| View all notes, CLI `gg note list`, MCP, id lookups, `NoteAddresses`, sweep | `LoadAll()` |

`NoteCounts` no longer reads `previews.toml`; merge-preview notes never show on
a commit (user ruling 2026-10-02), so nothing it reports changes.

## 6. Worktree lifecycle

- The main checkout is a worktree like any other and has its own file.
- **Deleted worktree:** the sweep removes `worktrees/<key>.toml` once its
  recorded path is no longer listed by `git worktree list`. Timid like today's
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
- e2e: one scenario that starts with a legacy `notes.toml` and asserts every
  note still shows where it did.
