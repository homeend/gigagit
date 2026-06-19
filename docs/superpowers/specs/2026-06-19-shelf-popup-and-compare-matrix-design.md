# Shelf popup + compare matrix — design

**Date:** 2026-06-19
**Status:** approved (brainstorm)
**Scope:** `internal/tui` (+ help/footer/docs). No engine/domain/CLI changes —
all byte resolution reuses existing `ResolveBytes` / `BookmarkBytes` / `ShelfBlob`
and the existing `Differ`. One cohesive feature, built in two stages, single merge.

## Goal

Present the shelf the same way as bookmarks (a global keyed quick-switcher
popup, replacing the Shelf left tab), and let any file be diffed against a
shelf entry — including cross-store compares (bookmark↔shelf) via a
"pick in one popup, the other popup replaces it, pick again, diff" flow.

## Decisions (locked in brainstorm)

1. **Replace the Shelf left tab with a popup** — full symmetry with bookmarks
   (which have no tab). `panelShelf` is removed; its actions move into the popup.
2. **Open key `G`** — free; pairs with bookmarks' `g` (the two file-reference
   switchers).
3. **One feature, two stages, single merge** — Stage A: shelf popup + tab
   removal; Stage B: the compare matrix (files-menu Compare against shelf +
   cross-compare `c` in both popups).

## Background (existing machinery being reused)

- `bookmarkPopup` (internal/tui/bookmark_popup.go) is a windowed list popup with
  `/` filter sub-mode, `z` cutoff/wrap/scroll, navigation-first keys
  (enter/p/m/x), and a **compare mode**: fields `compareRef *model.FileRef` +
  `compareLabel`; when set, `enter` diffs the focused file (left, via
  `ResolveBytes(compareRef)`) against the picked bookmark (right, via
  `BookmarkBytes`).
- `pendingCompare{ref model.FileRef; label string}` (bookmark_compare.go) carries
  the frozen focused file across the async list load; the `bookmarksLoadedMsg`
  handler stamps it onto the new popup as `compareRef`/`compareLabel`.
- The `.`-menu row **Compare against bookmark** (bookmark.go
  `compareAgainstBookmarkRow`) sets `pendingCompare` and loads bookmarks.
- `openBookmarkDiff(v, tag, load)` closes the popup, `clearStack()`s, and shows
  the full-screen diff (the critical fix so the diff isn't hidden under a
  history/blame surface).
- Shelf already has: `openShelfCompare`/`loadShelfCompareCmd` (entry vs
  working tree), `openShelfCompareTwo`/`loadShelfCompareTwoCmd` (two entries),
  `shelfRestorePopup` (mandatory-dest restore via `engine.WriteFile`),
  `shelfRemoveCmd`, `selectedShelfEntry`. These move from tab-driven to
  popup-driven.
- A shelf entry maps to `model.FileRef{Source: SourceShelf, Locator: id, Path}`;
  a bookmark maps via `bookmarkToFileRef(b)`. Both resolve through `ResolveBytes`.

## Stage A — Shelf popup (replaces the tab)

New `shelfPopup` (new file `internal/tui/shelf_popup.go`), a pointer field on
`Model`, structurally mirroring `bookmarkPopup`:

```go
type shelfPopup struct {
    items   []model.ShelfEntry
    rows    []string // e.Origin.Display(), parallel to items
    sel     int
    filter  string
    filtering bool
    markID  string   // first mark for two-entry compare ("" = none)
    mode    dispMode
    hscroll int
    compareRef   *model.FileRef // compare mode: enter diffs compareRef (left) vs the picked entry (right)
    compareLabel string
}
```

- **Open:** key `G`, global, via `openShelfSwitcher` mirroring
  `openBookmarkSwitcher` (guard `opsIdle()` + popup nil, fire `loadShelfCmd`);
  wired into the same surfaces `g` is (panels, file tree, diff, history, blame).
  The `shelfLoadedMsg` handler builds the popup (and stamps a pending compare —
  Stage B).
- **Render** `renderShelfPopup` mirrors `renderBookmarkPopup` (header "Shelf",
  `renderWindow`, h cap 12, footer hint). Compare-mode header
  "Compare <path> against:".
- **Keys** `updateShelfPopupKey` mirrors `updateBookmarkPopupKey`:
  - enter = jump (`openShelfCompare` on the selected entry) — or compare-mode
    branch (Stage B);
  - p = restore (open `shelfRestorePopup` for the selected entry);
  - m = mark/compare two entries (`openShelfCompareTwo`);
  - x = remove (confirm modal → `shelfRemoveCmd`, then reload+reopen);
  - c = compare against bookmark (Stage B);
  - `/` filter, `z`/`shift+←/→` modes, esc close.
  Compare mode disables p/m/x/c (mirrors the bookmark popup).
- The diff handoff reuses `openBookmarkDiff`'s behavior; factor it to a shared
  `openPickerDiff(v, tag, load)` (rename, or add a shelf alias) so both popups
  close-popup + clearStack + show diff identically.

**Tab removal:**
- Remove `panelShelf` from `leftTabs` and the panel enum; delete the
  `panelShelf` arm in `mark.go` (popup `m` replaces the pair-op mark), the
  `panelShelf` footer bindings (footer.go), and `selectedShelfEntry`'s
  `m.focus == panelShelf` gating (it now reads the popup selection).
- `shelf_actions.go` helpers (`openShelfCompare`, `openShelfCompareTwo`,
  `shelfRestorePopup`, `shelfRemoveCmd`, `loadShelfCompare*Cmd`) are retargeted
  to the popup selection instead of the tab. `shelfRows`/`viewstate.go shelfList`
  drop their tab role (rows now built in the popup).
- `m.shelfEntries` stays as the loaded list backing the popup.
- Anything referencing `panelShelf` (model.go selection clamp, view.go layout)
  is removed; the tab bar shows the remaining three tabs.

## Stage B — Compare matrix

Generalize `pendingCompare` to target either popup:

```go
type pendingCompare struct {
    ref    model.FileRef
    label  string
    target comparePopupKind // bookmark | shelf — which list to open in compare mode
}
```

The entry point chooses which list to load; the matching `…LoadedMsg` handler
stamps `compareRef`/`compareLabel` onto its popup.

Entry points:
- **Files `.`-menu "Compare against shelf"** (`compareAgainstShelfRow`, mirror of
  `compareAgainstBookmarkRow`): freeze the focused `FileRef` →
  `pendingCompare{ref, label, target: shelf}` → `loadShelfCmd`.
- **Bookmark popup `c`** ("compare against shelf"): left =
  `bookmarkToFileRef(selectedBookmark)` + `bookmarkDisplay` label →
  `pendingCompare{…, target: shelf}` → close bookmark popup → `loadShelfCmd`.
- **Shelf popup `c`** ("compare against bookmark"): left =
  `FileRef{SourceShelf, selectedEntry.ID, Path}` + `Origin.Display()` label →
  `pendingCompare{…, target: bookmark}` → close shelf popup → `loadBookmarksCmd`.

Right-side resolution on `enter` in compare mode:
- shelf popup → `ResolveBytes(FileRef{SourceShelf, picked.ID})`
- bookmark popup → `BookmarkBytes(picked)` (existing
  `openCompareFocusedVsBookmark`)

Add `openCompareFocusedVsShelf(ref, label, entry)` mirroring
`openCompareFocusedVsBookmark` (left `ResolveBytes(ref)`, right
`ResolveBytes(FileRef{SourceShelf, entry.ID})`), diff via the shared primitive.

## Help / footer

- help.go: a `G` row describing the shelf switcher (mirror the `g` row), and note
  the two `.`-menu compare actions and the `c` cross-compare in each popup.
- footer.go: remove the `panelShelf` footer bindings; the popups carry their own
  hint lines. The `G` global hint joins the footer tail near `g`.

## Testing

- `shelfPopup`: render shows `Origin.Display()`; `z` cycles mode; `/` filters;
  enter jumps; p opens restore; m+m compares two; x confirms remove.
- Tab removal: `leftTabs` no longer contains `panelShelf`; tab cycling covers the
  three remaining; no panel references `panelShelf`.
- Files `.`-menu has **Compare against shelf**; selecting an entry diffs focused
  vs shelf (right source = `ResolveBytes(SourceShelf)`).
- Cross-compare: bookmark popup `c` → shelf popup in compare mode with the
  bookmark's `FileRef` as `compareRef`; shelf popup `c` → bookmark popup in
  compare mode with the entry's `SourceShelf` ref; each enter resolves the
  correct two byte-sources and opens the diff (assert `diffTag`).
- `openShelfSwitcher` is reachable from the same surfaces as `g`.

## Docs

CHANGELOG (always); README (Shelf tab → `G` popup; new compare actions);
CLAUDE.md (Shelf is now a popup, not a tab; package-map/TUI note); help.go +
footer. No CLI surface change → no agentskill bump.
