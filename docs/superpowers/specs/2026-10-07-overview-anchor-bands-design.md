# Overview anchor bands — see and walk every anchor of a file

Date: 2026-10-07 · Branch: `feat/overview-anchor-bands` · Frontends: TUI + gg web

## Intent (the user's words, condensed)

When a file is opened from an overview anchor, show **every** line/range
anchor that overview has in that file, not only the one that was opened. The
opened one is drawn in one colour, the others in a different colour. `n` / `p`
move between them, so the reader gets back to the marked places faster than
by scrolling. The landing place stays the anchor picked in the overview.

## Rulings (asked and answered 2026-10-07)

1. **Kinds:** line anchors (`path:N`) and range anchors (`path:N-M`) both
   become bands; a line anchor is a one-line band. Note anchors (`note:t7`)
   stay as today — the note box is their mark, `}` / `{` their keys.
2. **Order:** `n` / `p` walk the file's bands in LINE order and WRAP inside
   the file; they never leave it.
3. **Current band is a band, not a selection:** landing on an anchor no
   longer selects its range. The bands persist through cursor moves, scrolling
   and esc; space/space still selects lines on top of them.

## Behaviour

### Which bands a file shows

A document shows bands exactly while it has a way back to an overview —
the TUI's `openFile.from` / the web's `view.from` is set and that overview is
still open (the condition backspace already checks). The bands are that
overview's anchors whose target is THIS file (`Anchor.Path` equal to the
document's path, both worktree-relative slash form) and whose `Start > 0`:

- sorted by `(Start, End)`; two anchors with the same `Start..End` make ONE
  band (it stands for the first of them in document order);
- a band whose `Start` is past the file's last line is dropped; an `End` past
  it is clamped;
- overlapping bands are kept apart; where they overlap the current band's
  colour wins;
- a file opened another way (F window, a link, `gg session files focus`)
  has no `from` and shows no bands; a later anchor open gives it one.

Bands are computed at draw time from the overview's CURRENT anchors (≤ 100),
so `gg session overview set` moves them at once and closing the overview
takes them away (and the way back with them, as today).

**The current band** is the one the reader opened or last stepped to. It is
remembered as the overview anchor's destination + index (`anchorCur`), and
re-found by destination after a `set` the way the overview's own selection is
(`keepAnchor`); when it is gone, every band is drawn as an "other" band and
`n` / `p` step from the cursor.

### Looks

- Two new theme roles: `anchor_current_bg` (the current band) and
  `anchor_bg` (the others, a dimmer tint of the same hue). `Dark` / `Light`
  get values; `Terminal` leaves both unset (inherit). Both are overridable in
  `[themes.<name>]` like every role (`roleFields`, `RoleDocs`).
- Paint order on a row, bottom to top: band → cursor band → selection stripe
  → search emphasis. The cursor and a selection stay readable over a band.
- The 2-column note gutter now also appears when the file has bands. Its
  mark, by precedence: `┃` current band, `╎` other band, `│` under a note,
  blank. The gutter carries the bands in the colourless `Terminal` theme too.
- gg web: viewer line classes `vanchor` / `vanchor cur`, coloured from the
  same two roles' CSS variables, plus the same gutter idea as a left border.

### Keys

In a document with bands (both frontends):

- `n` — the next band below; `p` — the previous band above. The step starts
  from the current band when the cursor is inside it, else from the cursor
  line. Past the last (first) band it wraps to the first (last).
- A step lands like an anchor open: the cursor on the band's first line,
  centred; a note box hanging under it is brought in as `}` does.
- A step makes that band current AND moves the overview's selected anchor to
  it, so backspace (web: also the browser's Back) returns to the overview on
  the anchor the reader ended on.
- The status line (web: the op line) says `anchor 2/5 in this file · <label>`
  (label = the anchor's link text in the overview), with `· wrapped` when the
  step wrapped.
- One band: `n` / `p` re-centre on it. No bands: `n` / `p` do nothing.
- gg web: the viewer ALWAYS claims `n` / `p` (inert without bands), as the
  TUI's viewer swallows every key. (Correction 2026-10-07: a `p` there never
  reached the global pull — an open layer owns the keyboard, `keys.js`.)

Discoverability: the `.` menu gets "Next anchor in this file" (`n`) and
"Previous anchor in this file" (`p`) rows in a file with bands (beside the
existing "Back to overview" row); the `?` help and the viewer's hint line
name the keys.

### What changes for existing behaviour

- TUI `anchorStatted` no longer sets `pendingEnd` for an anchor open (the
  range is no longer selected); it sets the current band instead.
  `steer` landings (`gg session files focus <id>:<a>-<b>`) keep selecting
  their range as today.
- Web `openAnchor` no longer sets `view.range` / `rangeOwn = false`; the
  special case "an overview anchor's band does not clear" in the viewer's
  range code goes away. A link landing's range is unchanged.
- The using-gg skill's anchor list says ranges are drawn (not "selected")
  and that `n` / `p` walk a file's anchors; bump `agentskill.Version`.

## Design (where it goes)

### TUI

- **Pure helpers** (new `internal/tui/anchor_bands.go`):
  `anchorBands(anchors []anchor, path string, nLines int) []anchorBand`
  (`anchorBand{start, end, i int}` — `i` the overview anchor index) and
  `stepBand(bands, cur, cursorLine, dir) (next int, wrapped bool)`.
- **State:** `openFile.anchorCur string` (the current anchor's dest; "" =
  none) beside `from`. `bands()` on `*openFile` = `anchorBands` over
  `from.ov.anchors` when `from` is still in the open-files list, else nil.
- **Paint:** `previewRowMark` (the one place a viewer row is styled) gets the
  row's band kind and lays the band style under the cursor/selection;
  `gutterW()` counts bands; the gutter mark loop picks `┃` / `╎` / `│`.
- **Keys:** a `bandKey` hook in `fileViewer.update` (and the files view's
  preview hook list, beside `previewNoteKey`), reached through
  `focusedDoc()`; it declines `n` / `p` when the document has no bands.
  `overviewRows()` grows the two `.` rows.
- **Styles/theme:** two roles in `internal/theme` (`roleFields`, `Dark`,
  `Light`, `RoleDocs`), two styles in `styles.go`, the settings registry doc.
- **i18n:** every new string in all four bundles.

### gg web

- On an anchor open, `view.from` keeps a copy of the overview's anchors
  (`anchors`) beside `{id, sel, dest}`; an `agentdocs` live message re-reads
  the overview (the same fetch `anchorBack` uses) and replaces the copy.
- Pure helpers in `viewer.js` mirroring the TUI's (`anchorBands`,
  `stepBand`), pinned by a Go `*js_test.go` like `rangekeysjs_test.go`.
- `renderViewer` adds `vanchor` / `vanchor cur`; a step repaints the two
  bands that changed in place (the `paintViewerBand` precedent) and moves
  `view.from.sel` / `dest`, so `backAnchor` returns to it.
- `viewerKey`: `n` / `p` before the `default` fall-through, always claimed.
- Help overlay rows; CSS for the two classes in both themes.

## Out of scope

- Note anchors in the `n` / `p` walk (they keep `}` / `{`).
- Leaving the file at the ends (the walk wraps).
- Bands in a file opened without an overview; bands from several overviews at
  once (only the one `from` names).
- Re-finding a band when the file's text moved (anchors are line numbers
  the agent wrote; a `set` is how they move).

## Testing

- `anchorBands`: path filter, line + range anchors, sort, duplicates → one,
  past-EOF drop / clamp, note anchors ignored, no `from` → none.
- `stepBand`: from the current band, from a cursor outside every band,
  between bands, wrap both ways, one band, current gone after a `set`.
- Rendering: `previewRowMark` / gutter marks per band kind, the cursor and a
  selection over a band, `Terminal` theme (gutter only).
- Keys: `n` / `p` land + centre + move the overview's `sel`; backspace
  returns on the stepped-to anchor; `n` / `p` inert without bands (web: no pull); an anchor
  open no longer sets a selection; a steer range still does.
- Live: an overview `set` moves / removes bands; closing the overview drops
  them.
- Web: the JS helpers pinned from Go; the key wiring test; a manual browser
  check of both colours and Back after `n`.
- A TUI e2e golden screen of a file with a current and two other bands.
- i18n AST gates, theme role docs test, `./test.sh race` before merge.

## Docs

CHANGELOG; README (overview keys); `internal/agentskill/using-gg.md` + version
bump; `docs/CLAUDE-details.md` (the overview section: bands, `anchorCur`,
the dropped landing selection).
