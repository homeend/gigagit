# File-reference bookmark + compare-against-bookmark — design

Date: 2026-06-19
Status: approved (brainstorm)

## Problem

A bookmark is a richly-addressed reference to a file (worktree/branch/commit/
shelf-id/path + state). Two file-oriented actions should be available from the
`.` menu **everywhere a single file is referenced** — the file tree, history,
blame, the Files/Staged/Shelf panels, stash file trees, and the diff view:

a. **Bookmark this file** — capture the focused file's address as a bookmark.
b. **Compare against a bookmark** — pick a bookmark and diff the focused file
   against it.

Action (a) already exists in most contexts (`bookmarkAddRow` +
`focusedBookmark`). Action (b) is new. Two small gaps in (a) are closed here.

## What already works (no change)

- The `.` menu opens in every navigable window (file tree, diff, history,
  blame, stash) and the base panels.
- `focusedBookmark()` resolves the focused file to a `model.Bookmark`
  (identity = address) in: history, blame, the commit file tree, **stash file
  trees** (`m.filesHash` carries the stash commit SHA, so the committed branch
  fires), and the Files / Staged / Shelf panels.
- `bookmarkAddRow` ("Bookmark this file") is appended in `availableActions`
  for both the content-window branch and the panel branch.
- `domain.BookmarkBytes(Bookmark)` resolves a *stored* bookmark's bytes
  (frozen blob SHA for committed/shelf; cross-worktree address for live).
- `domain.ResolveBytes(FileRef)` resolves a `model.FileRef` by **address**
  (commit→`git show rev:path`, unstaged→worktree file, staged→index blob,
  shelf→shelf blob) — no pre-resolved blob SHA required.
- The `Differ` consumes two arbitrary `ByteSource` closures
  (`Request{Old,New}`); the two sides need not be the same type.

## Design

### 1. New action: "Compare against bookmark"

`compareAgainstBookmarkRow() (actionRow, bool)` — a menu-only row, present in
exactly the contexts where `focusedBookmark()` succeeds (so it sits beside
"Bookmark this file" in both branches of `availableActions`).

The row **captures the focused file as a `model.FileRef` at build time**
(frozen against later selection movement), and its `run` handler:

1. stores the captured `FileRef` on the **Model** (`pendingCompareRef *model.FileRef`),
2. opens the global bookmark switcher (`loadBookmarksCmd`).

When `bookmarksLoadedMsg` builds the popup, a non-nil `pendingCompareRef` puts
the popup in **compare mode** (`bookmarkPopup.compareRef *model.FileRef`,
`compareLabel string` for the header). The Model field is the source of truth
across the async round-trip; it is cleared once consumed (mirrors the
reword-prefill fix — never thread async state through a not-yet-built popup).

**Compare-mode picker behaviour:**

- Header: `Compare <file> against:` (instead of `Bookmarks`).
- `↑↓/jk` move, `/` filters — unchanged.
- `enter` → run the comparison against the highlighted bookmark (NOT the
  normal jump-to-working-tree).
- `esc` → cancel (clears `compareRef`, closes popup).
- `m` (mark), `p` (paste), `x` (remove) are inert in compare mode.
- Empty list → `(none)`; `enter` is a no-op.

### 2. Asymmetric resolution (no domain semantics change)

`loadCompareFocusedVsBookmarkCmd(ref model.FileRef, bm model.Bookmark) tea.Cmd`
— mirrors `loadBookmarkCompareTwoCmd` but resolves the two sides differently:

- **Old (focused)** → `svc.ResolveBytes(ctx, ref)`.
- **New (picked bookmark)** → `svc.BookmarkBytes(ctx, bm)`.

It opens a `diffView` (`title: <ref path> ↔ <bm.Path>`,
`context: <ref label> → bookmarkDisplay(bm)`, tag `cmpbm:<…>`) and feeds both
sources into the cached `Differ`. Pure reads; **no new engine op, no CLI**.

### 3. Focused-file → FileRef

A single per-context resolver `focusedFileRef() (model.FileRef, string, bool)`
returns the focused file as a `FileRef` plus a short human label, covering the
same contexts as `focusedBookmark()`:

| Context | FileRef |
|---|---|
| history (selected row) | `{SourceCommit, Locator: commits[sel].Hash, Path: commits[sel].Path}` |
| blame | `{SourceCommit, Locator: ctx.rev, Path: ctx.path}` |
| commit / stash file tree | `{SourceCommit, Locator: filesHash, Path: row.path}` |
| diff view | NEW side: `{SourceCommit, Locator: rev, Path: title}` if `rev!=""` else `{SourceUnstaged, Path: title}` |
| Files panel | `{SourceUnstaged, Path}` |
| Staged panel | `{SourceStaged, Path}` |
| Shelf panel | `{SourceShelf, Locator: ShelfID, Path}` |

(`focusedBookmark()` stays the resolver for action (a); the two share the same
context precedence. Implementation may derive one from the other to avoid drift.)

### 4. Gap-closing in `focusedBookmark` (action a)

- **History per-row**: switch from the view's starting `ctx.rev` to the
  **selected commit row** — `{StateCommitted, Commit: commits[sel].Hash,
  Path: commits[sel].Path}` (rename-aware). This changes existing "Bookmark
  this file" behaviour in history to target the selected version (intended).
  The history copy-rows (`contextCopyRows`) align to the selected row's hash too.
- **Diff view**: today returns `false`. Add the NEW-side file
  (`{StateCommitted, Commit: rev, Path: title}` for a commit diff;
  `{StateUnstaged, Worktree, Branch, Path: title}` for a working-tree diff).

## Scope decisions

- Diff-view NEW side: **included**.
- **TUI-only** — no `gg bookmark compare` CLI surface.
- Reuse the existing global bookmark popup (no separate picker widget).

## Testing

- `focusedFileRef` / `focusedBookmark` resolution per context, including
  the stash file tree, the history selected-row version, and the diff view.
- `loadCompareFocusedVsBookmarkCmd` produces a diff from focused-vs-bookmark
  (FakeRunner / real-git fixture).
- Compare-mode picker: `enter` dispatches compare (not jump); `esc` cancels and
  clears `compareRef`; `m`/`p`/`x` inert.
- `pendingCompareRef` survives the async `loadBookmarksCmd` round-trip and is
  cleared after the popup is built.
- `compareAgainstBookmarkRow` appears wherever `bookmarkAddRow` does.

## Out of scope / follow-ups

- No CLI compare verb (YAGNI).
- The pending shelf-naming unification introduces `Bookmark.FileRef()`; this
  feature's `focusedFileRef` aligns with that direction but does not depend on it.
