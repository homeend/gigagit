package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// dispMode is how a window lays out rows that are wider than its box. It is
// cycled with the `w` key and generalizes the diff view's long-line modes to
// every list/text window.
type dispMode int

const (
	modeCutoff dispMode = iota // truncate each row to width (one line) + reveal
	modeWrap                   // wrap each row onto multiple lines
	modeScroll                 // keep rows full; reveal via horizontal scroll
	dispModeCount
)

// next returns the following mode, wrapping around.
func (d dispMode) next() dispMode { return (d + 1) % dispModeCount }

// winRow is one logical row before layout: raw (unstyled) text plus an optional
// style applied AFTER truncation/wrapping. Callers bake any cursor/mark prefix
// into text and set style for the selected row (selectedRow) or headings
// (titleStyle); the primitive never adds prefixes itself.
type winRow struct {
	text  string
	style lipgloss.Style // zero value renders the text unchanged
}

// winOpts is everything renderWindow needs besides the rows. anchor is the
// logical row kept visible by the vertical window (typically the selection).
type winOpts struct {
	w, h    int
	mode    dispMode
	anchor  int
	hscroll int // modeScroll horizontal offset (display columns)
}

// renderWindow lays rows out under o and returns exactly o.h display lines,
// each padded to o.w columns. Row styling is applied only after truncation or
// wrapping, so it can never corrupt the width-based slicing (ANSI-safety).
func renderWindow(rows []winRow, o winOpts) []string {
	w, h := o.w, o.h
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}

	type dline struct {
		text  string
		style lipgloss.Style
		row   int
	}
	var dl []dline
	for ri, r := range rows {
		var segs []string
		switch o.mode {
		case modeWrap:
			segs = wrapWidth(r.text, w, 1<<20) // huge cap => clean full wrap, no ellipsis
		case modeScroll:
			segs = []string{hslice(r.text, o.hscroll, w)}
		default:
			segs = []string{truncate(r.text, w)}
		}
		if len(segs) == 0 {
			segs = []string{""}
		}
		for _, s := range segs {
			dl = append(dl, dline{text: s, style: r.style, row: ri})
		}
	}

	anchorLine := 0
	for i, d := range dl {
		if d.row == o.anchor {
			anchorLine = i
			break
		}
	}
	start := windowStart(len(dl), h, anchorLine)

	out := make([]string, 0, h)
	for i := 0; i < h; i++ {
		idx := start + i
		if idx >= len(dl) {
			out = append(out, padRight("", w))
			continue
		}
		out = append(out, dl[idx].style.Render(padRight(dl[idx].text, w)))
	}
	return out
}

// hslice returns the display-column window [off, off+w) of raw text s. Width-
// aware so wide glyphs never split. The caller pads the result to w.
func hslice(s string, off, w int) string {
	if off > 0 {
		s = ansi.TruncateLeft(s, off, "")
	}
	return ansi.Truncate(s, w, "")
}

// rowTruncated reports whether s would be cut off in a w-wide cutoff window
// (drives the truncated-row reveal).
func rowTruncated(s string, w int) bool { return lipgloss.Width(s) > w }
