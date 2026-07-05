# Popup fullscreen-maximize (`T`) — Design

**Date:** 2026-07-06
**Status:** approved (brainstorm), ready for planning

## Goal

Let a popup be toggled to a near-fullscreen bordered box with capital `T`, the
same key that already fullscreens a focused panel. This gives every
content-heavy popup an escape hatch when its default box is too narrow to show
its content clearly — instead of the box hard-truncating columns to `…` or
wrapping long lines.

## Motivation

Popups render as centered boxes at fixed inner widths (`popupInnerWidth` = 56,
`popupWideInnerWidth` = 96, `contentPopupWidth` = 100-capped). On real content
— a git-config table, a file body, long bookmark/shelf/repo paths — that fixed
width truncates the part the user cares about. Today the only remedy is
per-popup width hand-tuning. `T` turns "this box is too small" into a one-key
user action.

## Scope

**Universal mechanism, opt-in per popup.** One shared, embeddable maximize
state plus one shared width resolver. A popup "opts in" by embedding the state,
honoring it in `render`, and calling the shared toggle helper in its navigation
branch. Popups that don't opt in are unaffected — `T` is simply inert there.

**Initial opt-in set** (converted in this feature):

| Popup | Type | Why |
|---|---|---|
| Git config explorer | `gitConfigPopup` | 4-column table; columns already shrink to floors and truncate |
| Content viewer / `?` cheat-sheet | `contentPopup` | The literal "content too large" case (`?` pushes `newContentPopup`) |
| Bookmark switcher | `bookmarkPopup` | Long file-address rows |
| Shelf switcher | `shelfPopup` | Same — path tails get cut |
| Repo switcher | `repoPopup` | Long repo paths |
| Fuzzy file finder | `fileFinderPopup` | Long paths, many matches |

Every other popup inherits the mechanism the next time it is touched — that is
the opt-in bargain. The **external-tools wizard** already renders at
`popupFullInnerWidth` permanently; it is the visual precedent for "maximized,"
not a conversion target.

## Non-goals

- No borderless edge-to-edge takeover. A maximized popup keeps its border,
  title, and hint footer (unlike the panel `T`, which has no border to keep).
  A popup stripped of its frame loses its title and footer.
- No new config key, no persistence. `maximized` is transient view state on the
  popup instance; it resets when the popup closes.
- No height/row-budget work in this feature (see "Width-only" below).

## Architecture

### 1. Shared maximize state (embeddable)

```go
// popupMax is embedded by any popup that supports T-to-fullscreen. The layer
// stack holds the popup *pointer*, so the flag persists across Model value
// copies (same rationale as the modal/popup pointer fields).
type popupMax struct{ maximized bool }

func (p *popupMax) maxed() bool { return p.maximized }

// handleMaxKey toggles maximize on "T" and reports whether it consumed the key.
// Opted-in popups call this at the top of their *navigation* branch.
func (p *popupMax) handleMaxKey(msg tea.KeyMsg) bool {
	if msg.String() == "T" {
		p.maximized = !p.maximized
		return true
	}
	return false
}
```

A popup opts in by embedding `popupMax`, which gives it `maxed()` and
`handleMaxKey` for free.

### 2. Shared width resolver

```go
// popupResolveWidth returns the near-fullscreen inner width when maximized,
// else the popup's normal width. Reuses popupFullInnerWidth (w-8), the same
// width the external-tools wizard renders at permanently.
func popupResolveWidth(w int, maximized bool, normal int) int {
	if maximized {
		return popupFullInnerWidth(w)
	}
	return normal
}
```

Each opt-in popup changes one line in its `render`/`box`:

```go
// before
inner := popupWideInnerWidth(w)
// after
inner := popupResolveWidth(w, p.maximized, popupWideInnerWidth(w))
```

### 3. Key routing — per-popup nav branch, NOT a global intercept

Keys reach an open popup through the layer stack: `topLayer().update(m, msg)`
(`model.go:903`). A single global `T`-swallow placed just before that call
would be the smallest change — but it is **wrong**, because capital `T` is a
legal character in a branch name, commit message, tag name, or `/`-filter
query. Globally swallowing `T` would make those un-typeable.

The fix: each opted-in popup calls `handleMaxKey` at the top of its
**navigation** branch (the branch it already forks to when *not* typing). So
`T` toggles maximize only when the user is navigating, and stays a literal
character while typing. Cost is one line per opt-in popup, which is edited to
honor the flag regardless.

Consequence, stated as a limit: **bare `T` only works for popups that have a
navigation mode.** An always-typing popup (every printable rune extends a
query, no nav mode) cannot opt in via bare `T`; it would need a non-colliding
affordance (`ctrl+t`, or `T`-only-when-query-empty). None of the six in the
initial set are always-typing — the fuzzy finder is navigation-first with a
`/` filter sub-mode, so `T` is free in its nav branch.

## Behavior

- **`T`** toggles the top popup between normal and maximized.
- **`T` again** restores normal size.
- **`esc`** closes the popup outright, exactly as today. Maximize is *only* a
  size toggle, not a step on the exit ladder — no trap, esc always leaves.
- The `maximized` flag lives on the popup instance, so it resets when the popup
  closes. A child popup pushed over a maximized parent starts un-maximized (its
  own flag).
- **Discoverability** (per the "advertise in help + footer" convention): each
  opt-in popup adds `T full` to its hint line, and `help.go` notes that `T`
  also maximizes popups.

## Width-only (why no height work)

All six opt-in popups already derive their box **height** from the terminal:
they render through `overlayCenter(clipToHeight(below, h), …)`, and their
visible-row budget scales with `h` (`contentPopup` uses
`contentPageRows = h - 7`). Their content already fills the available height.
The dimension that hurts is **width** — truncated columns and clipped path
tails. So maximize is a width change; the height already scales.

This keeps the shared mechanism minimal: `popupMax` is just a flag + toggle;
*how* a popup spends the maximized state (width here) is the popup's own choice
in `render`. A future non-full-height opt-in popup could additionally grow its
height, but nothing in the initial set needs it.

## Edge cases / gotchas

- **Typing collision** (the reason for per-popup routing): a popup in
  `/`-filter or text-entry state must treat `T` as a literal character. Because
  `handleMaxKey` sits in the nav branch, this holds by construction. A test
  guards it.
- **Flag survives in-popup reloads.** `gitConfigPopup` re-reads its rows after
  a config write (`gitConfigRowsMsg`); the handler mutates the existing stack
  instance in place (`layerOf[*gitConfigPopup](m)` → `p.loading = false`,
  `model.go:333`) rather than pushing a fresh popup, so `maximized` survives.
  Any new opt-in popup with an async reload must mutate-in-place, not replace.
- **No existing capital-`T` binding** in any of the six opt-in popups (verified),
  so `T` is free to claim.

## Testing

Following the panel-`T` lesson (*substring assertions are toothless — a 0-width
box truncates labels to `…`*), tests assert **box geometry**, not text
presence:

1. `popupResolveWidth` unit: `maximized` → `popupFullInnerWidth(w)`; else the
   normal width.
2. `handleMaxKey`: `"T"` flips `maximized` and reports consumed; any other key
   leaves it untouched and reports not-consumed.
3. Per opt-in popup: render normal vs. maximized on a wide terminal; assert
   `lipgloss.Width(maximizedBox) > lipgloss.Width(normalBox)`.
4. Typing-collision: a popup in its filter/text-entry state fed `"T"` inserts
   the character and does **not** maximize.
5. Exit: `esc` on a maximized popup closes it outright (does not merely
   un-maximize).
6. Reload survival (gitConfig): maximize, deliver a `gitConfigRowsMsg` reload,
   assert still maximized.

## Deferred

- `ctrl+t` (or query-empty `T`) maximize for always-typing popups, if any are
  added later.
- Converting the remaining ~24 popups eagerly — left to "as touched."
- Height maximize for a future non-full-height opt-in popup.
