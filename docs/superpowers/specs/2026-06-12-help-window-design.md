# Help window + generic searchable content popup — design

Status: approved in chat (2026-06-12). Branch: `feat/tui-help`.

## Goal

`?` opens a help window listing **every key binding in the TUI**, searchable by
typing. The window is built on a new **generic content popup** — a reusable
read-only viewer for any list of lines with type-to-filter search and
scrolling — so future read-only surfaces (log detail, diff preview) get it for
free. Help is its first consumer.

## Decisions (user-confirmed)

- **Trigger:** `?` in the plain state (no modal/popup open). Footer gains
  `[?] help`.
- **Content:** all key contexts in one grouped, searchable list — Global,
  panel-specific actions, filter mode, each popup, the decision modal.
- **Search:** type-to-filter immediately (like the `R` repo popup); no `/`
  prefix.
- **Generic component:** the viewer is content-agnostic and must be tested
  with content that fits the viewport AND content that overflows (scrolling),
  including searching while scrolled.
- **Scrolling inputs:** ↑/↓ (1 row), ctrl+↑/ctrl+↓ (5 rows), pgup/pgdn (one
  visible page), mouse wheel (3 rows per tick).

## UX contract

- Centered overlay (`overlayCenter`), `modalStyle`, width `popupInnerWidth`;
  long lines truncate display-aware (no horizontal scroll).
- Title line shows the window title and, while filtering, ` /query`.
- A hint line at the bottom shows position (`12/87`) when the content
  overflows, plus `[esc] close`.
- Cursor-based scrolling: a `> ` cursor row; the visible window follows the
  cursor via the same `windowRows` helper the panels use. When the content
  fits, everything is visible and movement just moves the cursor.
- **Filtering:** typed runes/space append to the query, backspace deletes.
  Case-insensitive substring match against non-heading lines. Section
  headings survive only while at least one of their lines matches. Every
  query change resets the cursor to 0 (searching while scrolled lands you at
  the top of the results, never on an empty window). No match → `(no match)`.
- **esc** clears the query if one is set; otherwise closes the popup.
  **enter** closes (read-only window). **ctrl+c** quits. Every key is
  swallowed — nothing falls through to global handlers.
- **Mouse:** wheel up/down scrolls the popup when it is open; mouse events
  are ignored everywhere else. Enabling mouse reporting means terminals need
  shift+drag for native text selection while gg runs — accepted trade-off
  (standard for full-screen TUIs).

## Architecture

Two new files in `internal/tui`, plus wiring:

1. **`content_popup.go`** — the generic viewer:

   ```go
   type contentLine struct {
       text    string // display text and the filter-match target
       heading bool   // section header: never matched, kept while its section has matches
   }
   type contentPopup struct {
       title string
       lines []contentLine // full, unfiltered content
       query string        // case-insensitive substring
       sel   int           // cursor index into the FILTERED view
   }
   ```

   Pure helpers: `visible() []contentLine` (filter + heading retention),
   `move(delta, max)` clamping, and a render function that windows the
   visible lines around `sel` with `windowRows`. Key handling in
   `updateContentPopupKey` mirrors the repo popup's structure.

2. **`help.go`** — `helpContent() []contentLine`: a hand-maintained table of
   every binding, grouped under headings: Global, Branches panel, Worktrees
   panel, Filter mode, Worktree popup, Branch popup, Repo switcher, Settings,
   Decision modal, Help window itself. Rows are `"  key        description"`
   aligned. `?` opens `newContentPopup("Help — keys", helpContent())`.

3. **Wiring** (per the adding-tui-windows popup checklist):
   - `Model.contentPopup *contentPopup` (pointer field — value-receiver
     invariant).
   - `Update` key routing: after the other popups, before filter mode.
   - `Update` gets a `tea.MouseMsg` case: wheel up/down → popup scroll when
     `contentPopup != nil`, else ignored.
   - `run.go`: add `tea.WithMouseCellMotion()` to `tea.NewProgram`.
   - `render()`: composite via `overlayCenter`; add `contentPopup` to the
     tooltip-suppression condition.
   - Footer string gains `[?] help`.

### Viewport math

Visible rows = `termH − 7` (double border 2, padding 2, title 1, blank 1,
hint 1), floored at 3. Content with ≤ that many visible lines renders fully;
more scrolls.

## Drift guard

A test asserts every `[x]`-style key abbreviated in the footer string appears
as a key in `helpContent()` — adding a key to the footer without documenting
it in help fails the build. (Full coverage of `Update`'s switch stays
hand-maintained; the help table is reviewed whenever keys change, same as the
footer.)

## Testing (TDD, `internal/tui`)

Generic popup (fed synthetic content, not help text):
- **Fits viewport:** all lines rendered; cursor to the last line causes no
  windowing.
- **Overflows:** exactly the cap is rendered; moving the cursor past the
  window scrolls it (`windowRows` offset honored); position indicator shown.
- **Search while scrolled:** scroll deep, type a query → cursor resets to 0,
  view shows the top filtered results.
- Filter narrows; headings of fully-filtered-out sections disappear;
  headings with surviving rows stay; `(no match)` on no hits.
- Two-stage esc: first clears the query, second closes.
- ctrl+↑/↓ moves 5; pgup/pgdn moves one page; wheel msg moves 3; all clamp.
- Key swallow: `p` (and other global keys) while open do nothing.
- Fit test: output never exceeds width×height at small sizes.

Help window:
- `?` opens it; rendered output contains known bindings (e.g. `SmartPull`).
- Footer-coverage drift test (see above).
- `?` while a popup/modal is open does NOT open help (swallowed by them).

## Non-goals

- No context-sensitive help (one searchable window lists everything).
- No mouse support outside the content popup (no panel clicks/wheel).
- No markdown/color markup in content lines; plain aligned text.
- No horizontal scrolling.
