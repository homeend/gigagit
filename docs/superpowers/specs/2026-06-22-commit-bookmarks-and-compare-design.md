# Commit bookmarks + compare-against-bookmarked-commit — Design

**Goal:** Let a user bookmark a *commit* (not just a file) so it persists across
sessions in the `g` switcher, and compare any commit against a bookmarked commit
as a whole-tree diff.

**Status:** Approved design, pre-implementation.

## Background

Today every bookmark is a **file** reference: `model.Bookmark` always carries a
`Path`, is created via "Bookmark this file", and lives in the global `g`
quick-switcher. In the switcher, `enter` jumps (diffs the bookmarked bytes vs the
current working file), `m` compares two file bookmarks, `c` compares vs a shelf
entry, `p` pastes, `x` removes. Bookmark identity is its address
(`bookmark.AddressID`, a hash over `State`/`Path`/`Commit`/…); the store is an
atomic-rewrite `bookmarks.toml` keyed by git common dir.

Whole-tree commit comparison already exists ("compare-trees"):
`model.Endpoint{Kind: EndpointCommit, Hash}` → `domain.CompareFiles(left, right)`
→ the files view in compare mode. The Commits panel already compares two marked
commits via `commitCompareMarkedRow` →
`openCompareFiles(olderEndpoint, newerEndpoint)`.

This feature reuses both systems rather than adding parallel ones.

## Approach

Extend the existing bookmark registry with **path-less commit bookmarks**
(`State: StateCommitted, Commit: hash, Path: ""`). They store in the same
`bookmarks.toml`, render in the same `g` switcher, and compare through the
existing `Endpoint`/`CompareFiles` machinery. No new store, type, or popup.

Rejected alternatives: a separate commit-bookmark store + filtered switcher (more
infrastructure for a concept the user framed as part of bookmarks); persisting the
in-session `m`-mark (not a bookmark — loses cross-session persistence, the point).

## Components

### 1. Model & storage (`internal/model`, `internal/bookmark`)

- A **commit bookmark** is a path-less committed bookmark: `Commit` set,
  `Path == ""`, `State == StateCommitted`. No new `FileState`.
- Add `func (b Bookmark) IsCommit() bool { return b.Path == "" && b.State == StateCommitted }`.
- `FileAddress.Display()` drops the trailing `/ <path>` when `Path == ""`:
  - branch known → `feat / a1b2c3d`
  - no branch → `commit / a1b2c3d`
  - (file addresses unchanged: `container / mid / path`.)
- `bookmark.AddressID` already hashes the full address (incl. `Commit`); a
  path-less commit bookmark gets a stable id from its hash with no change. The
  `slug(b.Path)` fragment becomes empty for commit bookmarks — acceptable (the
  hash disambiguates); confirm the id stays unique and stable in a test.
- **`domain.BookmarkAdd` must not freeze a blob SHA for a commit bookmark.** It
  currently does `repo.BlobSHA(commit, path)` for any `StateCommitted` bookmark
  with empty `SHA`; for a commit bookmark `Path == ""`, so this would fail. Gate
  the freeze on `b.Path != ""`. A commit bookmark carries an **empty `SHA`** (the
  commit hash is its immutable anchor; we deliberately do not set `SHA = Commit`,
  which would tempt a consumer into `cat-file blob <commit-sha>`).
- **Central byte-resolution guard.** A commit bookmark has no file bytes, so
  `domain.BookmarkBytes` must early-return a clear error for it
  (`if b.IsCommit() { return nil, … }`) instead of `CatFileBlob("")`. This makes
  every byte-resolution path (TUI jump/paste/compare-two, the CLI) fail cleanly
  by construction; the per-key TUI guards below are the UX layer on top.

### 2. Creating a commit bookmark (`internal/tui`)

- New Commits-panel `.` menu action **"Bookmark this commit"**
  (`commitBookmarkRow`, run-handler, gated `focus == panelCommits && opsIdle()` +
  a backing commit). It builds
  `model.Bookmark{State: StateCommitted, Commit: <full sha>, Branch: <tip branch
  if the commit is a local branch tip, else "">, Path: ""}` and fires the
  existing `bookmarkAddCmd`. Mirrors `bookmarkAddRow` ("Bookmark this file").
- The branch decoration is best-effort display sugar (first local ref on the
  commit, via the existing `commitHasLocalRef`/`Refs`); absence yields the
  `commit / sha` form.

### 3. The `g` switcher with mixed bookmarks (`internal/tui/bookmark_popup.go`)

- One list, file + commit bookmarks intermixed, distinguished by their rendered
  `Display()` (commit rows have no path). No filter/section split.
- `enter` becomes kind-aware on the highlighted bookmark:
  - **file** bookmark → `bookmarkJump()` (unchanged).
  - **commit** bookmark → whole-tree compare against the **Commits-panel selected
    commit**: `openCompareFiles(left = bookmarked-commit endpoint, right =
    selected-commit endpoint)`, then close the popup + clear the layer stack (the
    existing full-screen-diff handoff used by `openPickerDiff`/`openBookmarkDiff`,
    so the diff is not drawn under the switcher).
- `x` (remove) works for both kinds (unchanged path; the address id resolves a
  commit bookmark fine).
- `p` (paste), `c` (vs shelf), `m` (mark/compare) remain **file-only**. On a
  commit bookmark each is a no-op with a brief status notice (e.g. `"not
  available for a commit bookmark"`). This is an **enforced guard with a test**,
  not just an intention: `m` checks the highlighted bookmark's kind before
  recording/using a mark, so a file and a commit bookmark can never be paired.
- The switcher is also reachable in **compare mode** (`compareRef != nil`, a file
  being compared against a picked bookmark). A commit bookmark highlighted there
  → notice, no-op (can't compare a file against a commit bookmark).
- **Resolution-path audit (explicit task).** Enumerate every site that resolves a
  selected bookmark to bytes and confirm each commit bookmark is routed away or
  guarded: `enter` non-compare (→ compare, not jump), `enter` compare-mode
  (guard), `p` (guard), `c` (guard), `m` (guard), `x` (id-only, safe), and the
  central `BookmarkBytes` guard backstopping the CLI. The plan lists these as a
  checklist.
- Footer hint unchanged in content; the kind-specific behavior is documented in
  the `?` cheat sheet (one line: "enter on a commit bookmark compares it against
  the selected commit").

### 4. Compare direction & reuse

- Direction is **deterministic and feed-independent**: the bookmarked commit is
  the **left/base**, the selected commit is the **right/subject**. This matches
  "compare [selected] against [bookmarked]" and avoids `orderByFeed`, which needs
  both commits in the loaded feed (a bookmark may be outside it). The endpoints
  carry full hashes, so `CompareFiles` resolves them directly via git. Additions
  (`+`) read as present in the selected commit but not the bookmark; this is
  intentional and must not be "fixed" into age-ordering later.
- Everything downstream is the existing path: `openCompareFiles` → files view
  compare mode → `enter` on a file diffs that path (`loadCompareDiffCmd`).

## Data flow

```
Commits panel: . → "Bookmark this commit"
   → bookmarkAddCmd(Bookmark{StateCommitted, Commit, Path:""})
   → bookmarks.toml (same store)

g switcher: highlight a commit bookmark, enter
   → openCompareFiles(
        left  = Endpoint{EndpointCommit, bookmarked.Commit},
        right = Endpoint{EndpointCommit, selectedCommit.Hash})
   → close popup + clear stack
   → files view (compare mode) → enter diffs a file
```

## Error handling

- `enter` on a commit bookmark when the Commits panel has no loaded commit, or
  the selected commit equals the bookmark → status notice, no-op (do not open an
  empty/self compare).
- Bookmarked commit hash invalid (gc'd / rewritten) → `CompareFiles` errors; the
  files view surfaces it like any other compare error. No special handling.
- "Bookmark this commit" on an already-bookmarked commit → the store is
  idempotent by address id (same id → upsert, no duplicate row).

## Testing

- **Model** (`internal/model`): `Display()` for a path-less committed address
  (branch and no-branch forms); file-address display unchanged.
- **Store** (`internal/bookmark`): `AddressID` of a commit bookmark is stable and
  distinct from a file bookmark at the same commit; round-trips through the store.
- **Domain** (`internal/domain`): `BookmarkAdd` of a commit bookmark succeeds
  **without** calling `BlobSHA`/`cat-file` (a `FakeRunner` that errors on those
  proves it) and stores an empty `SHA`; `BookmarkBytes` of a commit bookmark
  returns a clear error and never runs `cat-file blob ""`.
- **TUI**:
  - `commitBookmarkRow` present on the Commits panel for a real commit, absent
    off-panel/while busy; running it adds a path-less bookmark.
  - switcher renders a commit bookmark with no path and (where present) the
    branch container.
  - `enter` on a commit bookmark calls `openCompareFiles` with
    left = bookmark hash, right = selected hash (assert the compare tag /
    endpoints); `enter` when `selected == bookmark` → notice, no compare;
    `enter` on a **file** bookmark still jumps (unchanged).
  - `p`/`c`/`m` on a commit bookmark are no-ops with a notice.
- **Real-git integration**: a two-commit repo (`loadedModelLinearCommits`),
  bookmark the older commit, select the newer, `enter` → the compare files view
  lists the changed files between them (proves the real CompareFiles path, not a
  fixture).

## Staging (for the implementation plan)

- **Stage 1 — model + create + render.** `IsCommit`, `Display` path-less form,
  "Bookmark this commit" action, switcher renders commit bookmarks. `enter` on a
  commit bookmark is a temporary notice ("compare wiring lands next"). Independently
  shippable: you can create and see commit bookmarks.
- **Stage 2 — enter-compare.** Wire `enter` on a commit bookmark to
  `openCompareFiles(base, subject)` + the no-op guards, and make `p`/`c`/`m`
  file-only with the notice. Closes the feature.

CLI (`gg bookmark` for commits) is out of scope for v1.

## Out of scope / non-goals

- No separate "Compare against bookmarked commit" Commits-panel menu action — the
  switcher `enter` is the single compare entry point (per the approved interaction
  model).
- No comparing a commit bookmark against a *file* bookmark or a shelf entry.
- No CLI surface, no change to the commit `m`-mark / `◉` selection compare.
