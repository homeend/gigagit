package tui

import "strings"

// The reading width: free text (a review overview, an AI result, a PR
// description, a commit message, View all notes) never runs across a whole
// fullscreen frame. Its text is laid in a column at most [ui] reading_width
// wide, left-aligned inside it, and the column is centred in the frame — the
// frame itself keeps its size, so ctrl+t still reads as "fullscreen".
// One file's code — View file, the F preview, blame — takes the same kind of
// centred column at [ui] file_width (fileColumn): its gutter plus at most
// that many characters, never wrapped narrower than the cap.
// Code, diffs, blame and file trees never take it: a long line is their
// content.

const (
	readingWidthDefault = 120
	readingWidthMin     = 40
)

// readingWidth is [ui] reading_width, 120 until config loads, floored at
// readingWidthMin.
func (m Model) readingWidth() int {
	if w := m.cfg.UI.ReadingWidth; w > 0 {
		return w
	}
	return readingWidthDefault
}

// readingColumn fits the reading column into textW columns: the column's
// width and the left margin that centres it. A textW no wider than the
// column is used whole (margin 0).
func readingColumn(textW, rw int) (col, margin int) {
	if rw < readingWidthMin {
		rw = readingWidthMin
	}
	if textW <= rw {
		return textW, 0
	}
	return rw, (textW - rw) / 2
}

// fileWidth is [ui] file_width: the widest text column of a single-file view
// (View file, the F preview, blame), -1 = unlimited; 200 until config loads.
func (m Model) fileWidth() int { return m.cfg.UI.FileWidthCols() }

// fileColumn fits a single-file view's column — its gutter plus at most fw
// text columns (-1 = no cap) — into w columns: the column's width and the
// left margin that centres it. Unlike prose, code is never wrapped narrower
// than the cap: the column is the cap, or the whole width when that is less.
func fileColumn(w, gutter, fw int) (col, margin int) {
	if fw < 0 || w <= gutter+fw {
		return w, 0
	}
	col = gutter + fw
	return col, (w - col) / 2
}

// previewColumn is the column a preview box lays its rows in, out of the
// box's innerW: a prose document in the full-screen viewer takes the reading
// column; one file's text (single: the full-screen viewer or the files view's
// preview, never a split pane) takes [ui] file_width on top of its gutter —
// not an image, not an overview. Anything else uses the whole width. The
// render and every key's geometry (activePreview, liveDoc) ask this one
// question, so the cursor and the drawn rows can never disagree.
func (m Model) previewColumn(p *contentPopup, innerW int, viewer, single bool) (col, margin int) {
	if p.prose {
		if viewer {
			return readingColumn(innerW, m.readingWidth())
		}
		return innerW, 0
	}
	d := m.previewDoc(p)
	if !single || p.img != nil || (d != nil && d.ov != nil) {
		return innerW, 0
	}
	gut := 0
	if d != nil {
		gut = d.gutterW()
	}
	return fileColumn(innerW, gut, m.fileWidth())
}

// centreBlock indents every line by margin and pads it to w — a column laid
// out narrower than its frame, centred in it.
func centreBlock(lines []string, margin, w int) {
	if margin <= 0 {
		return
	}
	pad := strings.Repeat(" ", margin)
	for i, l := range lines {
		lines[i] = padRight(pad+l, w)
	}
}

// indentBlock shifts every line of s right by margin columns.
func indentBlock(s string, margin int) string {
	if margin <= 0 {
		return s
	}
	pad := strings.Repeat(" ", margin)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = pad + l
		}
	}
	return strings.Join(lines, "\n")
}

// stackProseWidth is the column a stacked diff's prose section (a review's
// Overview, a shelved set's note) is laid out at: the screen less the row's
// margins, capped at the reading width.
func (m Model) stackProseWidth() int {
	w, _ := m.overlayDims()
	col, _ := readingColumn(w-4, m.readingWidth())
	return col
}

// stackProseLead is the lead a prose row draws before its text in a stack
// row w wide: the usual two-column indent plus the margin centring a col-wide
// column.
func stackProseLead(w, col int) string {
	margin := 0
	if w-4 > col {
		margin = (w - 4 - col) / 2
	}
	return "  " + strings.Repeat(" ", margin)
}
