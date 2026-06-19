# Bookmark popup windowed display modes — design

**Date:** 2026-06-19
**Status:** approved (brainstorm)
**Scope:** TUI only — `internal/tui/bookmark_popup.go` (+ project skill doc).

## Problem

The bookmark quick-switcher popup (`g`) renders its list by hand:
`renderBookmarkPopup` builds a `strings.Builder` and emits it through
`modalStyle.Width(popupInnerWidth(w)).Render(...)`. Long `bookmarkDisplay`
rows (`<container> / <commit-or-state> / <path>`) therefore **wrap
uncontrollably**, and the popup has none of the in-viewport text display modes
(cutoff / wrap / scroll via `z`) that every other gg list surface has.

The repo switcher (`internal/tui/repo_popup.go`) is the reference: it already
renders through the shared `renderWindow` primitive with a `dispMode` cycled by
`z`, horizontal pan via `shift+←/→` in scroll mode, and a capped viewport.

## Goal

Make the bookmark list popup behave exactly like the repo switcher: render
through `renderWindow`, support `z` cutoff/wrap/scroll, `shift+←/→` horizontal
pan in scroll mode, and a capped scrolling viewport that keeps the selected row
in view.

## Design

### `bookmarkPopup` struct
Add two fields, mirroring `repoPopup`:
- `mode dispMode` — text display mode; `z` cycles (cutoff is the default).
- `hscroll int` — `modeScroll` horizontal offset.

### `renderBookmarkPopup`
Replace the hand-rolled builder with the repo-popup shape:
- Compute `inner := popupInnerWidth(w)` and `textW := popupTextWidth(inner)`.
- Header `"Bookmarks"`, with a `  /<filter>` suffix when a filter is active
  (and the typing caret `█` while `p.filtering`), matching repo's `/query`.
- Build one `winRow` per filtered index. **Fold both indicators into the row
  text** (as repo_popup folds `"> "` + `"● "`): a `"> "`/`"  "` selection
  prefix, then a `"•"`/`" "` compare-mark, then the `bookmarkDisplay` string.
  The selected row also carries the `selectedRow` style.
- `(none)` path: a single `padRight("  (none)", textW)` body line.
- Cap visible height at **12** (matching the repo switcher) and call
  `renderWindow(wr, winOpts{w: textW, h: h, mode: p.mode, anchor: p.sel,
  hscroll: p.hscroll})`.
- Footer line gains `[z] mode`:
  `[enter] jump  [p] paste  [m] mark/compare  [x] remove  [/] filter  [z] mode  [esc] close`.
- Emit through `popupBox(inner, strings.Join(parts, "\n"))` instead of
  `modalStyle.Width(...)`.

### `updateBookmarkPopupKey`
Add display-mode keys in **navigation mode only** (when `!p.filtering`), taking
precedence like repo_popup:
- `z` → `p.mode = p.mode.next()`, reset `p.hscroll = 0`.
- `shift+left` → pan left only in `modeScroll` (clamp at 0, step
  `m.hscrollStep()`).
- `shift+right` → pan right only in `modeScroll`.

While `/`-filtering, `z` and the shift keys are left to the filter input (a
literal `z` types into the query), consistent with the action menu's typing
mode. Moving the selection (`j`/`k`/↑/↓) keeps the existing behavior; the
viewport follows because `renderWindow` anchors on `p.sel`.

### Out of scope
- The paste-destination popup (`renderBookmarkPastePopup`) stays a plain
  `modalStyle` modal — it is a single-line input, not a list.
- No change to bookmark data, the store, or jump/paste/compare/remove behavior.
- No change to the `g` open key.

## Skill update

`.claude/skills/adding-tui-windows/SKILL.md` currently tells popup authors to
"Style with `modalStyle.Width(inner)` so long content wraps instead of
overflowing" (Popup checklist item 3). That is correct for **single-line /
fixed input** popups but is the wrong default for **list** popups — it is the
exact gap this change fixes. Add a **"List popups"** note to the Popup checklist
making the rule explicit: a popup that renders a scrollable list of rows must
render through `renderWindow` (give the state struct `mode dispMode` +
`hscroll int`, handle `z` / `shift+←/→` in navigation mode, fold row indicators
into the row text, cap the viewport height, and emit via `popupBox`), so
cutoff/wrap/scroll come for free — `repo_popup.go` is the exemplar. Plain
`modalStyle.Width` stays the rule only for input/confirm popups.

## Testing

Unit tests in `internal/tui` (helpers: `loadedModel`, `keyMsg`):
- `z` in navigation mode cycles `p.mode` cutoff → wrap → scroll → cutoff and
  resets `hscroll`.
- `shift+right` pans (`hscroll > 0`) only when `mode == modeScroll`; it is a
  no-op in cutoff/wrap.
- `z` typed while `p.filtering` appends `"z"` to the filter instead of cycling
  the mode.
- A bookmark with a long `bookmarkDisplay` row, in cutoff mode, produces a
  rendered body whose every line width ≤ the viewport text width (no wrap).
- The existing open / move / jump / mark / paste / key-swallow behavior is
  unchanged (keep the current tests green).

## Docs

CHANGELOG.md (always). No README/CLAUDE/agentskill change — no user-facing CLI
surface or architecture change; `z`-cycles-everywhere is already the documented
TUI convention.
