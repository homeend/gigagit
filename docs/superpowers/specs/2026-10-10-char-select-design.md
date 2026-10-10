# Character selection in the TUI: `v` copy mode for the review summary and the diff — design

Date: 2026-10-10. Status: design approved in brainstorm (rulings C1–C8
below); this document is the spec for the user's review. One plan follows
(§9).

## 0. What this is

The TUI copies whole lines today: `space` / `space` / `enter` in the diff,
blame and the View-file preview (`lineselect.go`, spec
`2026-09-08-hunk-parity-roadmap.md` §4.7). The review view's **≡ Summary**
is prose with no cursor at all; `y` copies the whole summary's markdown.

This spec adds a **character** selection, a modal copy mode in the spirit
of vim's visual mode and tmux's copy mode: `v` enters it and shows a
character cursor, the user moves it to the start, `space` fixes the start,
the user moves to the end, `enter` (or `y`) copies the covered text and
leaves the mode, `esc` backs out. The copied text is **what is on screen**
(rendered prose, a diff side's source lines), never the markdown behind it
(C1).

Hosts in this round (C3): the **Summary popup** (`reviewSummaryPopup`),
the **stacked review view** (`diffStack`: its summary element and its
files), and the **single diff view** (`diffView`). Blame and the preview
adopt the same model in a follow-up.

## 1. Rulings (the user's, in brainstorm)

- **C1** The copy is the rendered text, not the markdown source. `y`
  outside the mode keeps copying the summary's markdown.
- **C2** Both summary surfaces get the mode: the popup and the stacked
  view's summary element.
- **C3** This round: the summary (both surfaces) and the diff (single and
  stacked). Blame and the View-file preview later, through the same model.
- **C4** `v` enters the mode. The diff's `space` keeps its whole-line
  meaning outside the mode; inside the mode `space` fixes the start.
- **C5** The mode is **modal**, as vim's visual mode: only its keys act,
  every other key is inert, so no host key collides.
- **C6** `enter` confirms (copies and leaves); `y` is a synonym inside the
  mode (vim's yank). `esc` drops the fixed start first, then leaves the
  mode, copying nothing.
- **C7** One reusable model over "rows of text" with thin host adapters
  (approach A), not per-host code and not a screen-level overlay.
- **C8** Sections 2–5 of the design as presented ("as you wish" on the
  adapters, "continue" on rendering, copy rules, tests).

## 2. The model (`internal/tui/charsel.go`, pure)

No Bubble Tea, no host types; unit-tested on its own.

```go
type pos struct{ row, col int } // a host's LOGICAL row, a rune index into it

type charSel struct {
    on    bool // the mode is active: a cursor shows, the keys are modal
    fixed bool // space fixed the start: anchor..cur is the live range
    anchor, cur pos
    want  int  // the column ↑/↓ try to keep (clamped on a shorter row)
}
```

- **Rows** are the host's logical rows in display order: a summary prose
  row after layout (a wrapped markdown row is several rows, as on screen),
  a diff side's line. `col` is a rune index into the row's text; the model
  never sees display cells, gutters or line numbers.
- **Enter** (`v`): `on = true`, the cursor at the host's start position
  (§3). Inert on a host with no selectable rows (`▸ nothing to select`).
- **space** fixes the start at the cursor (`fixed = true, anchor = cur`).
  A second `space` fixes it again at the cursor: the previous range is
  dropped and a new one starts (the line selection's "third space" rule).
- **enter** / **y**: with `fixed`, copy `bounds()` (§5) and leave the mode;
  without, `▸ space fixes the start first` and stay.
- **esc**: with `fixed`, drop the start (`fixed = false`, the cursor
  stays); without, leave the mode.
- **Movement**, clamped to the host's rows; a row the host marks dead is
  stepped over in the direction of travel; the row the cursor is on is
  always a live one:
  - `←` / `h`, `→` / `l`: one rune; past a row's start or end runs onto
    the previous row's last rune / the next row's first rune.
  - `↑` / `k`, `↓` / `j`: one row, keeping `want` (set by every horizontal
    move) and clamping to the shorter row.
  - `home` / `0`, `end` / `$`: the row's first / last rune.
  - `w` / `b`: the next / previous word start (a word = a run of
    non-space runes), across rows.
  - `pgup` / `pgdn`: a page of rows (the host's visible height), keeping
    `want`.
- Any other key while `on` is inert: handled (so it never reaches the
  host) and does nothing (C5). `v` inside the mode is inert too.
- `bounds() (lo, hi pos, ok bool)`: with `fixed`, `anchor` and `cur` in
  reading order (row, then col), inclusive on both ends; `ok` false
  otherwise.

The key handling is one function the hosts share:

```go
// charSelKey applies msg to cs over host. handled says the key was the
// mode's (true for every key while on); copy is the text enter/y produced
// (""= nothing copied); notice is a status line to show ("" = none).
func charSelKey(cs *charSel, host charHost, msg tea.KeyMsg) (handled bool, copy, notice string)
```

## 3. The host interface and the three adapters

```go
type charHost interface {
    charRows() []charRow // the selectable text, in display order
    charPage() int       // the visible height (pgup/pgdn)
}

type charRow struct {
    text  []rune
    wraps bool // continues the previous row: a copy joins them with a space
    dead  bool // not selectable: an absent diff cell, a folded line, a placeholder
}
```

Each host keeps its own `cs charSel`, enters the mode on `v`, routes every
key through `charSelKey` while `cs.on`, and clears `cs` whenever its rows
are rebuilt (a diff reload, a stack re-layout, a popup width or display-mode
change) — as it clears the line selection today.

- **Summary popup** (`reviewSummaryPopup` over `contentPopup`). Rows: the
  popup's laid-out lines at the current width — the same rows
  `renderWindow` draws in `modeWrap`, so a wrapped markdown row yields one
  `charRow` per display row with `wraps` on the continuations. The meta
  and "Other notes" rows are text too (selectable). Start position: the
  first visible row, column 0. While `on`, `o`, `y`, `/`, `s`, `ctrl+w`
  are inert (C5). A `/` filter that is live when `v` is pressed is left as
  it is (the rows are the filtered ones).
- **Stacked review view** (`diffStack`). `v` on a prose row (`lineProse`)
  selects within the **summary element**: its `f.prose` rows, `wraps` from
  the layout. `v` on a file's row selects within **that file**, on the
  cursor's side, the file's lines being the rows (`dead` for an absent
  cell or a folded line). The element where `v` was pressed bounds the
  selection: moving past its ends clamps and says `▸ the selection stays
  in this element` (the summary) / `▸ the selection stays in this file`.
  Start position: the cursor row (its first prose row for the summary
  element; the cursor line's column 0 for a file). The stack's line cursor
  follows the character cursor's row, so the view scrolls with it and
  `n`/`p` positions stay sane after the mode ends.
- **Single diff view** (`diffView`). Rows: the cursor side's lines
  (`onOld` decides), `dead` for an absent cell or a folded line. Start
  position: the line cursor's line, column 0. `alt+←/→` are inert in the
  mode (the line selection locks its side the same way). The line cursor
  follows the character cursor's row (the existing scrolling keeps it on
  screen).
- Entering the mode clears a live line selection in the diff, and a line
  selection's `space` is unreachable while the mode is on: one selection
  kind at a time.

## 4. Rendering

- The covered runes draw with the theme role `selection_bg` (the line
  selection's stripe, so "selected" looks the same everywhere); the cursor
  cell is reversed on top so it stays visible inside the selection. A row
  partly covered keeps its own colours outside the range.
- One helper, `charSelSpans(cs charSel, row int, n int) (from, to, cursor
  int)` (`from..to` the covered rune range of logical row `row`, `-1`
  none; `cursor` the cursor column or `-1`), applied where each host
  already builds a row's styles (`colouredLine` / `winRow.cls` for the
  popup and the stack's prose rows, the diff renderer's cell painter for
  diff lines). No new renderer.
- The diff view highlights the cursor side only; the other side's cells
  are untouched.
- The footer swaps to the mode's keys while `on`: `[←→↑↓ hjkl] move  [w b]
  word  [space] start  [enter] copy  [esc] leave`; after `space`: `[←→↑↓
  hjkl] move  [space] restart  [enter y] copy N chars  [esc] drop`. Every
  string through `i18n.T` in all four bundles.
- The status line reports the copy: `copied N characters` (the line
  selection's wording pattern), and the notices of §2–§3.

## 5. Copy rules

- The covered text: on the first row from `lo.col`, on the last row to
  `hi.col` inclusive, whole rows between; dead rows skipped.
- Row joins: a `wraps` row joins the previous one with a single space (a
  wrapped paragraph comes out as one line); any other boundary with `\n`.
  No trailing newline.
- Runes are the host's text runes: tabs stay tabs where the host's rows
  hold them (a diff side's source lines), spaces where the layout already
  expanded them (prose rows).
- A one-rune selection (`space` then `enter` on the same cell) copies that
  rune.
- Clipboard through `copyToClipboardCmd` (tests capture it with
  `captureClip`, never the real clipboard).

## 6. Help and docs

- Each host's help lists `v` with one line ("select characters to copy:
  move, space fixes the start, enter copies, esc leaves").
- README: the review view's summary paragraph and the diff's selection
  paragraph gain the mode; CHANGELOG; `docs/CLAUDE-details.md` a block on
  `charSel` / `charHost` and the adapters. No CLI, web or skill change.

## 7. Tests

- `charsel_test.go`: the pure model — every move and its clamping, dead
  rows stepped over, `want` kept across short rows, word steps across
  rows, `space` restart, `esc` stages, `bounds` in either order, the copy
  join (`wraps` = space, else `\n`), the one-rune copy, inert keys.
- Per host, a key-path test with `captureClip` like `diff_select_test.go`:
  the popup (a wrapped summary, a selection across the wrap copies one
  joined line), the stacked view (a selection in the summary element and
  one in a file, the element bound notice), the single diff (the cursor
  side's text, `alt+←/→` inert, a line selection cleared on `v`).
- The i18n AST gates and `diff_menu_parity_test.go` stay green (no new
  `.` menu rows: the mode is keys only).

## 8. Out of scope

Blame and the View-file preview (next round, one adapter each); the web
page (the browser selects text natively); a mouse; a `.` menu row for the
mode; copying across elements of a stack.

## 9. Plan

One implementation plan: the pure model and its tests; the summary popup
adapter + rendering; the diff view adapter; the stacked view adapter;
help, bundles, docs.
