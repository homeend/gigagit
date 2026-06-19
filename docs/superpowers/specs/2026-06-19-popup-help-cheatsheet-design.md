# `?` cheat sheet for the bookmark / shelf switchers — design

**Date:** 2026-06-19

## Problem

Pressing `?` while the bookmark (`g`) or shelf (`G`) quick-switcher is open does
nothing — the popup swallows the key. There is no in-context key reference for
those switchers beyond the single footer hint line at the bottom of the popup.

## Decision (user-approved)

`?` while the bookmark/shelf switcher is open opens a **compact cheat sheet**: a
small read-only popup listing **only that switcher's keys**, overlaid on top of
the still-open switcher. `esc` (or `q`) closes the cheat sheet and returns to the
switcher with its filter/mark state intact.

Rejected alternatives: full `?` help reordered to lead with the popup section
(heavier than asked); replace-and-drop-the-popup (loses switcher state).

## Scope

Bookmark switcher and shelf switcher only. Other popups (repo switcher, settings,
pair-op, …) keep swallowing `?` as today — a deliberate, stated inconsistency.

## Implementation (TUI-only)

- **Content:** two cheat-sheet builders in `help.go` returning `[]contentLine`
  (`bookmarkSwitcherHelp()`, `shelfSwitcherHelp()`), each mirroring that popup's
  bottom hint line (enter / p / m / c / x / ↑↓jk / `/` / z / esc). Reuses the
  generic `contentPopup` viewer (scroll, `/`-search, `z` mode, esc/q close).
- **Trigger:** add `case "?"` in the navigation switch of `updateBookmarkPopupKey`
  and `updateShelfPopupKey` — sets `m.contentPopup = newContentPopup(title, rows)`
  and **keeps** the picker. (`?` typed during `/`-filter sub-mode stays a query
  char — the filtering branch returns early.)
- **Overlay + return:** the cheat sheet must paint over and capture keys above the
  picker. The picker is checked before `contentPopup` in dispatch (model.go),
  render (view.go), and mouse (mouse.go). Add a `contentPopup` check **gated on a
  picker being open** above each picker check at all three sites; the base-layout
  `?` paths (over panels) are left untouched, so only the new overlay case
  changes. `contentPopup`'s existing `esc` just nils itself, so the still-set
  `bookmarkPopup`/`shelfPopup` re-emerges automatically — no explicit return path.

## Testing

- Unit: each cheat-sheet builder lists the expected key columns.
- `?` in the bookmark popup opens `contentPopup` AND keeps `bookmarkPopup`; title
  is the switcher's; content leads with its keys. Same for shelf.
- Dispatch hoist: with both open, a subsequent key (e.g. `/`) goes to the cheat
  sheet (`contentPopup.typing`), not the picker filter.
- `esc` closes the cheat sheet and the picker is still open.
- Regression: `TestHelpNotOpenedWhileAnotherPopupIsOpen` (repoPopup) stays green.
