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

> **Revision (2026-07-06, post-review):** the "opt-in, convert the content-heavy
> ones first" framing below was the wrong call — the ticket's requirement is
> that **every** popup the user sees can go fullscreen with `T`, and leaving
> most popups silently inert defeats the purpose. The mechanism is now
> **centralized**: a single `T` handler on the layer stack (`maximizableLayer`
> interface) toggles whatever popup is on top, gated by a per-popup
> `capturingText()` so `T` stays literal in text fields. Every centered-box
> popup is swept to render wider (and taller where it caps rows) when maximized.
> Full-screen surfaces (`diffView`, the conflict editor, history/blame/rebase)
> are excluded — they already own the screen. The original per-popup
> `handleMaxKey`-in-nav-branch approach (below) is superseded; the six
> already-converted popups are refactored onto the central handler.

**Universal mechanism, opt-in per popup.** One shared, embeddable maximize
state plus one shared width resolver. A popup "opts in" by embedding the state,
honoring it in `render`, and calling the shared toggle helper in its navigation
branch. Popups that don't opt in are unaffected — `T` is simply inert there.

**Initial opt-in set** (converted in this feature). The last column records
what maximize must change per popup — a fact established by reading each
popup's `render`/`box` (see "What maximize changes" below):

| Popup | Type | Why | Maximize changes |
|---|---|---|---|
| Content viewer / `?` cheat-sheet | `contentPopup` | The literal "content too large" case (`?` pushes `newContentPopup`) | width only (rows already `h-7`) |
| Git config explorer | `gitConfigPopup` | 4-column table; columns already shrink to floors and truncate | width only (rows already `termH-12`) |
| Bookmark switcher | `bookmarkPopup` | Long file-address rows | width + row cap (fixed 12) |
| Shelf switcher | `shelfPopup` | Same — path tails get cut | width + row cap (fixed 12) |
| Repo switcher | `repoPopup` | Long repo paths; uses the narrow 56-col `popupInnerWidth` | width + row cap (fixed 12) |
| Fuzzy file finder | `fileFinderPopup` | Long paths, many matches | width + row cap (fixed 16) |

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

### 2. Shared resolvers (width + row cap)

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

// popupMaxRowCap is the visible-row budget for a maximized list popup whose
// normal budget is a small fixed constant: terminal height minus box chrome,
// floored so a tiny terminal still shows a few rows. Mirrors gitConfigPopup's
// existing capRows (termH - 12).
func popupMaxRowCap(termH int) int {
	n := termH - 12
	if n < 3 {
		n = 3
	}
	return n
}
```

Each opt-in popup changes its width line in `render`/`box`:

```go
// before
inner := popupWideInnerWidth(w)
// after
inner := popupResolveWidth(w, p.maximized, popupWideInnerWidth(w))
```

The four fixed-cap popups additionally lift their row cap when maximized
(their `render`/`box` currently discards the terminal height with
`w, _ := m.overlayDims()`, so it must switch to `w, termH := m.overlayDims()`):

```go
// before
capRows := 12          // fixed; 16 in fileFinderPopup
// after
capRows := 12          // fixed; 16 in fileFinderPopup
if p.maximized {
	capRows = popupMaxRowCap(termH)
}
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

## What maximize changes (per popup)

The dimension that always hurts is **width** — truncated columns and clipped
path tails — so every opt-in popup widens to `popupFullInnerWidth` when
maximized. Height is popup-dependent, established by reading each `render`/`box`:

- **`contentPopup`** (row budget `contentPageRows = h - 7`) and
  **`gitConfigPopup`** (row budget `capRows = termH - 12`) already derive their
  visible-row count from the terminal, so their height already fills. These two
  are **width-only**.
- **`bookmarkPopup`**, **`shelfPopup`**, **`repoPopup`** (fixed cap of 12) and
  **`fileFinderPopup`** (fixed cap of 16) cap their rows at a small constant
  regardless of terminal height. A full-width-but-12-rows box is not
  "fullscreen," so these four also **lift their row cap** to `popupMaxRowCap`
  when maximized.

The shared mechanism stays minimal — `popupMax` is just a flag + toggle; *how*
a popup spends the maximized state (width, and row cap where the cap is fixed)
is each popup's own choice in `render`. `popupResolveWidth` and
`popupMaxRowCap` are the two shared helpers that keep those choices uniform.

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
3. Per opt-in popup: render normal vs. maximized on a wide, tall terminal;
   assert `lipgloss.Width(maximizedBox) > lipgloss.Width(normalBox)`. For the
   four fixed-cap popups (bookmark/shelf/repo/finder), also feed more rows than
   the fixed cap and assert the maximized box has more body lines than the
   normal box (the row cap lifted).
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
