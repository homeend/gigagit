package tui

import (
	"strings"

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

// rowDecorator restyles one already-sliced+padded visual line without changing
// its visible width (e.g. recoloring a single glyph). hscroll is the horizontal
// offset applied to this line (0 unless modeScroll); visualLine is the segment
// index (0 = a row's first line, 1+ = wrap continuations).
type rowDecorator func(visible string, hscroll, visualLine int) string

// winRow is one logical row before layout: raw (unstyled) text plus an optional
// style applied AFTER truncation/wrapping. Callers bake any cursor/mark prefix
// into text and set style for the selected row (selectedRow) or headings
// (titleStyle); the primitive never adds prefixes itself.
//
// prefix is an optional frozen left column (e.g. a blame gutter): it is shown on
// the row's first display line and blanked on wrap continuations, and the text
// wraps/scrolls within the remaining width (winOpts.prefixW) so the gutter never
// moves. prefix "" (with prefixW 0) is the plain whole-row path.
type winRow struct {
	text     string
	prefix   string
	style    lipgloss.Style // zero value renders the text unchanged
	decorate rowDecorator   // optional; applied post-slice, post-pad
}

// winOpts is everything renderWindow needs besides the rows. anchor is the
// logical row kept visible by the vertical window (typically the selection).
type winOpts struct {
	w, h    int
	mode    dispMode
	anchor  int
	hscroll int // modeScroll horizontal offset (display columns)
	prefixW int // width of the frozen winRow.prefix column (0 = none)
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

	// Window BEFORE building any per-row state, making the whole call O(visible)
	// instead of O(len(rows)). Without this a 40k-row panel rebuilds every row on
	// every frame, and gg's perpetual 1s heartbeat re-renders the whole UI, so a
	// huge untracked or commit set pegs the CPU (and, under any extra event
	// traffic, freezes it).
	if len(rows) > h {
		if o.mode != modeWrap {
			// Single-line modes (cutoff/scroll): each row occupies exactly one
			// display line, so the visible slice is deterministic.
			start := windowStart(len(rows), h, o.anchor)
			rows = rows[start : start+h]
			o.anchor -= start
		} else {
			// Wrap mode: a row spans a variable number of lines, but every row is
			// at least ONE line, so the h-line window anchored on row a can only
			// ever show rows within h of a. Rows further away contribute only to
			// the line COUNTS windowStart sees, and its clamps bind only when the
			// lines on that side of the anchor number fewer than h — which implies
			// no rows on that side were dropped. Slicing to [a-h, a+h] before
			// wrapping is therefore output-identical to the full layout
			// (TestRenderWindowWrapMatchesFullLayout pins this for every anchor).
			a := o.anchor
			if a < 0 || a >= len(rows) {
				a = 0 // mirrors the anchor scan below: no matching row → line 0
			}
			lo := a - h
			if lo < 0 {
				lo = 0
			}
			hi := a + h + 1
			if hi > len(rows) {
				hi = len(rows)
			}
			rows = rows[lo:hi]
			o.anchor -= lo
		}
	}

	// A frozen prefix column (o.prefixW>0) reserves the leftmost columns; the
	// body wraps/scrolls within the rest, and the prefix shows on a row's first
	// display line only (blank on wrap continuations) so the gutter never moves.
	pw := o.prefixW
	if pw < 0 {
		pw = 0
	}
	if pw > w-1 {
		pw = w - 1 // always leave at least one column for the body
	}
	bodyW := w - pw

	type dline struct {
		text  string
		style lipgloss.Style
		deco  rowDecorator
		hs    int
		si    int
		row   int
	}
	var dl []dline
	for ri, r := range rows {
		var segs []string
		hs := 0
		switch o.mode {
		case modeWrap:
			segs = wrapWidth(r.text, bodyW, 1<<20) // huge cap => clean full wrap, no ellipsis
		case modeScroll:
			segs = []string{hslice(r.text, o.hscroll, bodyW)}
			hs = o.hscroll
		default:
			segs = []string{truncate(r.text, bodyW)}
		}
		if len(segs) == 0 {
			segs = []string{""}
		}
		for si, s := range segs {
			if pw > 0 {
				pre := strings.Repeat(" ", pw) // blank gutter on continuations
				if si == 0 {
					pre = padRight(truncate(r.prefix, pw), pw)
				}
				s = pre + s
			}
			dl = append(dl, dline{text: s, style: r.style, deco: r.decorate, hs: hs, si: si, row: ri})
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
		line := padRight(dl[idx].text, w)
		if dl[idx].deco != nil {
			line = dl[idx].deco(line, dl[idx].hs, dl[idx].si)
		}
		out = append(out, dl[idx].style.Render(line))
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
