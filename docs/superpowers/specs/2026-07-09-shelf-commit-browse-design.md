# Browse a shelved commit in the files view — design

**Date:** 2026-07-09 · **Status:** approved (user, this session)

## Problem

A shelved commit (`ShelfKindCommit`, the `G` switcher entries created by
"Shelf this commit") is a frozen tar of the commit's changed files. Until now
the switcher's per-file actions refused it with a notice (the guard that fixed
the "is a directory" error). The user wants to *see* what is inside a shelved
commit and restore chosen files to the working tree.

## Behavior

- `enter` on a shelved-commit entry in the `G` switcher opens the existing
  **files view** (the file tree window) populated with every file in the tar,
  replacing the guard notice. The switcher stack is cleared first (the
  `compareCommitBookmark` precedent); `esc` from the view returns to the panel
  that was focused before the switcher opened.
- Each row behaves like any files-view row:
  - `enter` → side-by-side diff **shelf version (old) ↔ working tree (new)**.
  - preview → the frozen content.
  - `.` menu → the standard focused-file rows, notably **Copy to working
    dir** (writes the frozen bytes to the file's own repo-relative path as an
    unstaged change; `engine.WriteFile` owns the overwrite/cancel fork),
    plus Compare against working dir / bookmark / shelf and Bookmark this
    file. Restoring several files = copying them one by one, as for any set
    of files in this window (user decision — no new multi-select machinery).
- The tree shows the commit's **added/modified files only** — deletions were
  dropped at shelve time (no content to archive), same as `[t]` export.
  User accepted this explicitly.
- The right column is inert like compare mode (no follow-live commit list —
  a shelved commit is not part of the feed); the tree owns focus.
- `p`/`e`/`m`/`c` on a commit entry keep the notice (reworded to point at
  `enter` and `[t]`).

## Design

Two seams carry the whole feature; everything else is existing plumbing.

### 1. domain: member-aware shelf resolution

- `ResolveBytes` for `SourceShelf` currently returns the whole blob — for a
  commit entry that is the raw tar. New behavior, discriminated by the
  **entry kind** (never by content sniffing — a shelved `.tar` *file* must
  stay a blob):
  - file entry → whole blob (unchanged; `Path` stays display-only),
  - commit entry + `Path` → that member's bytes extracted from the tar
    (error when the path is not in the entry),
  - commit entry + empty `Path` → the tar (unchanged, backs export).
- `shelfEntryByID(ctx, id)` domain-internal lookup: scan `Buckets()` +
  `List` (the shelf is small; commit entries live in the default bucket).
- `ShelfCommitFiles(ctx, id) ([]model.CommitFile, error)` — tar **header**
  scan (no data copy) → rows with `Status: ""` (A-vs-M relative to the
  original parent is not recorded in the tar; blank is honest).

### 2. tui: `filesModeShelf`

The `filesModeStash` pattern:

- Cluster fields `filesShelfID` / `filesShelfLabel` (display), zeroed in
  `closeFilesView`.
- `openShelfCommitFiles(e)` — clean slate, title from `shelfEntryDisplay(e)`,
  loader `loadShelfFilesCmd` → `shelfFilesMsg{id, files, err}`, stale results
  dropped by id+mode match.
- Switch-point branches:
  - `focusedBookmark()` → `Bookmark{State: StateShelf, ShelfID, Path}`; all
    `.`-menu rows light up via the existing
    `bookmarkToFileRef` → `ResolveBytes` chain — zero new action code.
  - `openDiffForFileLine` → `loadCompareTwoRefsCmd` with
    left `{SourceShelf, id, path}`, right `{SourceUnstaged, path}` (the
    compare-against-working-dir machinery, reused).
  - file preview → member bytes via `ResolveBytes`.
  - focus/`moveListUnderFilesView`/full-tree `a` gates: shelf mode behaves
    like compare mode (tree-only, no follow-live, no full-tree toggle).
- `G` switcher: `enter` on a commit entry routes to `openShelfCommitFiles`;
  compare-mode enter keeps its refusal; help/cheat-sheet text updated.

### Alternatives considered

- Tar-backed `Endpoint` kind through `CompareFiles` (would add ≠/▲▼ markers
  vs worktree): touches the git-backed compare machinery far more deeply;
  can be layered on later without redoing this.
- Dedicated popup: rejected by user — the files view is the wanted surface.

## Testing

- domain: real repo + shelf store — member extraction (present, absent,
  file-entry passthrough), `ShelfCommitFiles` listing.
- tui: `G`-enter on a commit entry opens the files view (mode, title, rows);
  `focusedBookmark` returns the shelf member ref; the copy row is offered;
  `enter` builds the member↔working diff tag; esc/closeFilesView zeroes the
  cluster; file entries keep today's behavior.
- `./test.sh race` before merge.
