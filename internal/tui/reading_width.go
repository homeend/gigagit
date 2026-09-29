package tui

import "strings"

// The reading width: free text (a review overview, an AI result, a PR
// description, a commit message, View all notes) never runs across a whole
// fullscreen frame. Its text is laid in a column at most [ui] reading_width
// wide, left-aligned inside it, and the column is centred in the frame — the
// frame itself keeps its size, so ctrl+t still reads as "fullscreen".
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
