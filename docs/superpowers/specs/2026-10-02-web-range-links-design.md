# Range links in gg web — design

Date: 2026-10-02 · Follows `2026-10-02-range-links-design.md` (terminal side,
merged `151ac571`) and the TUI range note (`a8664e7a`).

## Goal

The browser page handles a range `gg://` link the way the terminal does:

1. Opening a range link marks the same lines on the same side (diff view,
   stacked diff, file viewer).
2. The reader can mark lines and copy a range link.
3. A note that covers a range shows a bar beside its lines.

Rules already decided and not reopened: one fingerprint over the whole block
(`model.BlockFingerprint`); a range is never re-found — a changed block is
"the link is no longer valid"; a ranged note sits under its last line.

## What exists today

- The server already sends the range end: `steerWire.EndLine` (`end_line`),
  and the op line says "the link names lines a-b". The page lands on the
  first line only.
- Diff: a click on a row marks it (`markDiffRow`, class `cur`, `state.diffRow`
  / the stack slot's `row`). That mark is what `c` (note) and a landing use.
  The right-click menu offers "copy gg link to this line".
- Diff, uncommitted only: click / shift+click / ctrl+click on a CHANGED row
  is the staging selection. It cannot hold context rows or commit-diff rows.
- File viewer: a line cursor (`view.cur`), and `view.range` — a tinted band
  (`.vrange`) set today only when an overview anchor opens a range. The `.`
  menu offers "copy file link (line N)".
- Notes: the wire note carries `range` (`[first, last]`; `line` = last). The
  page draws the note under `line` and ignores `range` in the diff.

## Design

### 1. The marked range (state)

One marked range per surface, beside the existing mark:

- Diff: `{side, first, last}` on the row mark's owner — `state.diffRange`
  for the single-file view, the slot's `range` in a stack (one slot at a time,
  like `row`). Rows inside it get class `rng` on that side's cells.
- Viewer: the existing `view.range` (`{start, end}`), band `.vrange`.

A range is always on ONE side of ONE file. It is cleared when the diff or
file is replaced, on Esc, and when a plain click marks another row.

### 2. Marking by hand — DECISION NEEDED (D1)

Recommended: **shift+click on a line number extends the mark to a range.**

- A plain click anywhere on a row keeps today's meaning (marks the row; in
  the viewer, moves the cursor) and clears any range.
- Shift+click on a line NUMBER (diff `td.no`, viewer `.vno`) marks the lines
  from the current mark to that number, on the side of the clicked number.
  With no mark, or a mark on the other side or in another file, it marks that
  one line (no range).
- In the diff, a shift+click on a line number is ONLY the range gesture: the
  staging selection ignores clicks that land on `td.no`. Shift+click on the
  code cell of a changed row stays the staging gesture. The two never fire
  together.
- Esc clears the range first, then (next Esc) the staging selection, then
  what Esc did before.
- Edge rows with no line on that side are trimmed, lines hidden in a fold
  inside the range count — the TUI's rule (`diffLinkSelection`).

Alternatives: the browser's text selection (cannot be restored by a link;
ambiguous across sides); landing only (drops goal 2).

### 3. Copying a range link

- Diff right-click menu: when the row under the pointer is inside the marked
  range, the link row reads **"copy gg link to lines a-b"** and copies
  `…:a-b` / `…:old:a-b`. Outside the range it stays "copy gg link to this
  line".
- Viewer `.` menu / right-click: "copy file link (lines a-b)" when a range
  is marked, else today's row.
- Uncommitted lines (working tree, index, the viewer showing the file on
  disk) carry the block fingerprint `~<fp>`; commit, pair and preview links
  never do — exactly the single-line rule.
- The block is read from the diff's full row set (not the rendered rows), so
  folded lines are in it. If it does not yield exactly `b-a+1` lines the row
  is not offered.
- A merge preview's old side has no link (as today for one line).
- `linkFor` gains the range end and block as arguments; no string surgery on
  a finished link.
- `blockFingerprint(lines)` in `links.js` is the twin of
  `model.BlockFingerprint` (trim each line with `GO_TRIM`, join with `\n`,
  FNV-1a 32, "" when every line is blank), pinned by a Go↔JS test like
  `TestLineFingerprintJSMatchesGo`.

### 4. Opening a range link

- Diff (single file): reveal both ends (unfold what hides them), mark the
  first line as today, set the range `first..last`, scroll the first line to
  the centre. If the last line is not in the diff, mark what is there from
  the first line on and say so in the op line.
- Stack: `landStackLine` takes the end and does the same inside the file's
  section.
- Viewer (content link): after `openViewer`, set `view.range` and keep the
  cursor on the first line. A range starting past the end marks nothing
  (the TUI's rule); an end past the last line is clamped.
- A stale range (changed uncommitted block) is refused by the resolver before
  any steer is sent. The page's own link prompt must show the resolver's
  message ("the link is no longer valid: lines a-b of <path> have changed
  since it was copied") — verified, fixed if it shows anything else.

### 5. The note bar

- In the diff (single and stacked), every line inside the range of a note
  whose box is drawn gets a bar in its line-number cell on the note's side
  (`box-shadow: inset` in the note colour; no layout change). Rule copied
  from the TUI: only notes with `range[1] > range[0]`, only when the box is
  on screen (not filtered out, e.g. a hidden agent layer); a collapsed note
  keeps its bar, as in the TUI.
- The viewer shows agent open-file notes only; those already band their
  range. No change there.

### Calls of mine (say if you want them changed)

- C1. No keyboard marking in this round (no shift+↑/↓); the viewer's cursor
  keys keep working and clear nothing.
- C2. The web's `c` with a range marked still writes a one-line note at the
  mark. A web twin of the TUI range note is a separate follow-up.
- C3. Clicking a marked row's number again does not toggle the range off;
  Esc or a plain click elsewhere does.

## Structure

- `static/links.js`: `blockFingerprint`, `linkFor(…, end, block)` — pure.
- `static/rangemark.js` (new, pure): `extendRange(mark, hit)` (the state
  step for shift+click), `rangeRows(rows, side, first, last)` (trim + block
  text from full rows), `noteBars(notes)` (the barred line set per side).
  Tested under node (the `texttemplatesjs_test.go` pattern).
- `static/files.js`: render `rng` and the bar in `diffHTML`; the shift+click
  handler; the staging handlers skip `td.no`; the menu row; Esc order.
- `static/stackview.js`: slot `range`, `landStackLine(path, side, line, end)`.
- `static/viewer.js`: shift+click on `.vno`, the menu row, clamp rule.
- `static/live.js`: pass `end_line` on both landing paths.
- `static/style.css`: `.rng`, the bar.
- Go: no new endpoint expected; tests only (fingerprint twin, link text
  twin, source pins).

## Testing

- Go↔JS: block fingerprint (trailing spaces, CR, interior blank line, all
  blank, U+00A0, U+FEFF); range link text for new/old × uncommitted, commit,
  pair, preview, content hint — against `model.Link.String()`.
- Node: `extendRange` (no mark, other side, other file, backwards),
  `rangeRows` (gap rows at the edges, folded lines, wrong count refused),
  `noteBars`.
- Browser (playwright, against the rebuilt binary, asserting visibility):
  shift+click marks a band; the menu copies `:a-b`; shift+click on a number
  of a changed working-tree row does NOT change the staging selection;
  opening a range link bands the lines in diff, stack and viewer; a ranged
  note shows its bar; Esc order.
- `go test ./internal/web` after every `static/*.js` edit.

## Out of scope

Range notes written from the web; keyboard marking; temporary agent notes on
diff lines; the open minors of the terminal side.
