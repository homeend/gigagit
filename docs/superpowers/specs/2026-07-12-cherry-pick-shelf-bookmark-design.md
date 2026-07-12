# Cherry-pick a bookmarked / shelved commit — design

**Date:** 2026-07-12
**Branch:** `feat/cherry-pick-shelf-bookmark`
**Status:** approved approach (two-lane pick with patch snapshot); spec pending user review

## Problem

A commit bookmark (`g` switcher) and a shelved commit (`G` switcher) both point at
a commit the user cared enough about to save, but neither surface can apply that
commit to the current branch. The user must copy the sha and run
`gg cherry-pick` (or raw git) by hand. Worse, the shelf's promise is that a
shelved commit *survives `git gc` / history rewrite* — yet once the commit
object is gone there is no way to re-apply it at all: the stored tar holds
result-state file contents, not a change, so per-file restore is the only
(lossy, clobbering) option.

## Goal

One key — **`a`** — in both switchers that applies the highlighted commit entry
onto the current branch:

- while the commit object still exists → a true `git cherry-pick` (existing
  `engine.CherryPick`: autostash, conflict fork, abort recovery);
- after the commit is gc'd, for a shelf entry that carries a stored patch →
  replay the patch as a new commit (existing `engine.ApplyPatch` in
  `ApplyModeCommits`: `git am --3way`, atomic rollback, preserves
  author/date/message);
- otherwise → a clear notice, never a raw git "bad object" error.

To make the second lane possible, shelving a commit starts storing the commit's
`git format-patch` mailbox alongside the existing changed-files tar.

## Non-goals

- **CLI lane** (`gg shelf cherry-pick <entry-id>` or similar) — deferred to a
  follow-up task (see Deferred work). The live lane is already covered by
  `gg cherry-pick <sha>`.
- **Reconstructing a commit from the tar.** The tar holds result-state, not a
  diff; applying it onto a different base would clobber unrelated changes in
  touched files. Explicitly rejected.
- **Backfilling patches into existing shelf entries.** Old entries degrade
  gracefully (sha lane only + notice); no migration pass.
- **Patches for bookmarks.** A bookmark is a record-only registry — "no blobs"
  is its design contract. Commit bookmarks cherry-pick only while the commit
  exists.
- **Merge commits.** `format-patch -1` cannot represent a merge, and
  `git cherry-pick` of a merge needs `-m`. A merge commit shelves as before
  (tar only, no patch); `a` on it works only via the sha lane while the commit
  exists, and refuses with git's own error (which names the -m requirement).

## Design

### 1. Shelf schema — patch snapshot (`internal/model`, `internal/shelf`)

- `model.ShelfEntry` gains two optional fields:
  - `PatchSHA string` — content hash of the patch blob; `""` = no patch
    (old entries, merge commits, oversized or failed `format-patch`).
  - `PatchSize int64` — informational, mirrors `Size`.
- `shelf.Store.PutCommit` gains a `patch []byte` parameter (nil/empty = none).
  The patch is a second content-addressed blob under `blobs/<sha>` via the
  existing `writeBlob` (dedup and atomicity for free).
- New `shelf.Store.GetPatch(entryID) ([]byte, error)`: `ErrNotFound` for an
  unknown entry, new sentinel `ErrNoPatch` for an entry without one. Callers
  that only need to *know* whether a patch exists use `Find` +
  `entry.PatchSHA != ""` (no blob read).
- `Remove` reclaim extends to the patch blob: each of the removed entry's
  blobs (SHA and PatchSHA) is deleted only when no surviving entry references
  it in *either* field.
- TOML index compatibility: new fields are optional; an old index decodes with
  zero values (no patch), and an old gg reading a new index ignores the
  unknown keys. No version bump needed.
- Size cap: the patch reuses `MaxCommitArchiveBytes` (200 MB). An oversized
  patch is *skipped*, not an error — the shelve still succeeds tar-only.

### 2. Domain (`internal/domain`)

- `ShelfAddCommit(ctx, sha, label)` — after building the tar, best-effort
  patch capture:
  1. `git.ParentCount(ctx, sha)` ≥ 2 (merge) → skip patch.
  2. else `git.FormatPatch(ctx, sha)` (whole commit, `--binary`); any error or
     an oversized result → skip patch (a log-worthy degradation, never a
     shelve failure).
  3. pass the patch bytes to `PutCommit`.
  Both git verbs already exist (export-as-patch uses them).
- New query `CommitLookup(ctx, rev) (model.LogLine, bool, error)` — a gated
  Read-reservation wrapper over `git.CommitLine` where "no such commit" is
  `(zero, false, nil)`, not an error (the `CommitTimes` precedent: the TUI
  never imports `internal/git`). Returns the short-sha + subject the confirm
  modal displays.
- New `ShelfPatchFile(ctx, entryID) (path string, err error)` — materializes
  the entry's patch blob to a temp file (for `engine.ApplyPatch`, which takes
  a disk path). The caller (TUI) owns deletion after the op finishes.

### 3. TUI (`internal/tui`)

**Key**: `a` in both `bookmarkPopup` and `shelfPopup`, navigation mode only
(while filtering it stays a query rune). Compare mode (`compareRef != nil`)
ignores it, matching the other action keys.

**Guard**: a *file* entry → status notice ("cherry-pick: only for a commit
entry"). This is the inverse polarity of the existing `commitBookmarkNotice`/
`commitShelfNotice` guards, so it is a separate check, not a reuse.

**Probe (async, gen-guarded)**: `a` on a commit entry dispatches a
`tea.Cmd` that calls `CommitLookup` off the UI thread and returns
`applyPickProbeMsg{gen, source, entryID, sha, subject, found}`. A model-level
generation counter (`pickGen`, the `pushCheckGen` pattern) is bumped on
dispatch, popup close, and `reRoot`; a stale result is dropped silently.

**Lanes** (on a fresh probe result):

- **Lane A — commit exists**: confirm modal
  `Cherry-pick <short-sha> <subject> onto <branch>?`
  `[Cherry-pick / Cancel]`. Confirm → `clearLayers()` (close the switcher so
  a conflicted pick lands in the main view's conflict process) →
  `startOp(engine.CherryPick{Commit: sha})`.
- **Lane B — commit missing, shelf entry with `PatchSHA != ""`**: confirm
  modal `Commit <short-sha> is no longer in the repo. Re-apply the shelved
  patch as a new commit?` `[Apply patch / Cancel]`. Confirm →
  `ShelfPatchFile` (synchronously inside the returned cmd, the `stageCmd`
  pattern) → `clearLayers()` →
  `startOp(engine.ApplyPatch{Path: tmp, Mode: ApplyModeCommits})`. The temp
  path is remembered in a model field and removed best-effort when the op
  finishes (`opFinishedMsg`) — also cleared/removed by `reRoot`.
- **Lane C — commit missing, no patch**: status notice.
  - bookmark: `commit no longer exists — a bookmark stores no snapshot
    (shelve commits to keep them applyable)`
  - shelf: `commit no longer exists and this entry has no stored patch
    (shelved before patch support, or a merge commit)`

**Modal placement**: the confirm modal renders above the still-open switcher
(the `[x]` remove-confirm pattern); Cancel reveals the switcher unchanged.

**Inertness**: both popups already no-op all keys while `m.running` (B1), so
`a` cannot double-dispatch.

**Refresh**: `opAffectedSources` already maps both `engine.CherryPick` and
`engine.ApplyPatch`; no changes.

**Conflicts / failure**:
- Lane A: a conflicted pick uses `CherryPick`'s existing decision fork; a kept
  conflict feeds the status→conflict-process wiring after the refresh.
- Lane B: `ApplyModeCommits` is atomic — on any failure `git am --abort`
  rolls back; nothing half-applied. Errors surface on the status line via the
  normal op-failure path.

**Discoverability** ("advertise in help AND footer" convention, applied to
popups): add `[a] cherry-pick` to both popups' hint lines and both `?` cheat
sheets (`popup_help.go`).

### 4. What does NOT change

- `engine` — both ops are used as-is; no new operations, no `GitOps` changes.
- `bookmark` package — untouched (record-only contract preserved).
- CLI — untouched this stage (see Deferred work).
- `gg shelf commit` (CLI creation path) gains patch storage automatically
  because it shares `domain.ShelfAddCommit` — that ride-along is in scope.

## Testing (TDD throughout)

- **shelf**: `PutCommit` with and without patch (fields, second blob on disk);
  `GetPatch` happy / `ErrNoPatch` / `ErrNotFound`; `Remove` reclaims the patch
  blob only when unreferenced (incl. tar-blob/patch-blob cross-reference
  cases); an index written without the new fields decodes cleanly.
- **domain**: `ShelfAddCommit` stores a patch for a normal commit, skips it
  for a merge commit, and survives a `FormatPatch` failure (entry still
  created, `PatchSHA == ""`); `CommitLookup` found vs missing (missing is not
  an error); `ShelfPatchFile` materializes bytes equal to the stored patch.
- **TUI**: `a` on a file entry → notice, no cmd; `a` on a commit entry →
  probe cmd dispatched; stale probe result dropped (gen mismatch); lane A
  modal text + confirm dispatches `CherryPick` and closes layers; lane B
  confirm dispatches `ApplyPatch{ApplyModeCommits}` with a real temp file and
  cleans it up on `opFinishedMsg`; lane C notices (both wordings); hint lines
  and `?` sheets mention the key; compare mode ignores `a`.
- **e2e**: none — the new surface is TUI-only; the CLI ride-along
  (`gg shelf commit` storing a patch) is not user-visible state the e2e
  harness asserts.

## Docs to update at completion

`CHANGELOG.md` (always), `README.md` (new switcher key), `CLAUDE.md` (shelf
package-map entry: patch snapshot + `a` key), memory feature file. No
`agentskill` bump (CLI unchanged).

## Deferred work (recorded as next task)

**CLI lane — cherry-pick a shelved commit from the command line.**
`gg shelf cherry-pick <entry-id>` (name TBD) driving the same two-lane logic
non-interactively: sha lane when the commit exists, patch lane otherwise
(flag-selectable, e.g. `--patch` to force the mailbox replay), exit codes per
the `gg apply` convention (0 applied, 1 failure/conflicts, 2 usage). Routed
through `runOne` so `gg batch` can drive it; needs an `agentskill.Version`
bump + regenerated dogfood skill when it lands.
