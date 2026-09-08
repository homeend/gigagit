package tui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/syntax"
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
	// cls is an optional syntax class per DISPLAY RUNE of text (so
	// len(cls) == len([]rune(text)); nil = the plain path every other caller
	// takes). The window slices it alongside the text in all three modes, so a
	// coloured run lands on the right columns after a cutoff, a horizontal
	// scroll, or a wrap. cls WINS over decorate: a row that sets both is
	// rendered coloured and decorate is never called (no caller combines them;
	// TestRenderWindowClsWinsOverDecorate pins it). A row whose style reverses
	// video (selectedRow) also ignores cls — reverse swaps foreground and
	// background, so per-token colours would paint per-token BACKGROUNDS.
	cls []syntax.Class
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

// windowRowBounds returns the half-open [lo, hi) slice of the n logical rows
// that a window of height h anchored on row `anchor` can ever show, for the
// given mode. It is the same bound renderWindow computes internally (see the
// comment inside it) before building any per-row state, factored out so a
// caller with expensive per-row construction — e.g. blame's per-line
// lex-mapped winRow — can build only the rows that will actually be laid
// out. renderWindow calls this too, so the two can never disagree. Callers
// pass n == the FULL row count; when n <= h every row is visible (lo=0,
// hi=n) and no windowing is needed.
func windowRowBounds(n, h, anchor int, mode dispMode) (lo, hi int) {
	if n <= h {
		return 0, n
	}
	if mode != modeWrap {
		// Single-line modes (cutoff/scroll): each row occupies exactly one
		// display line, so the visible slice is deterministic.
		lo = windowStart(n, h, anchor)
		return lo, lo + h
	}
	// Wrap mode: a row spans a variable number of lines, but every row is at
	// least ONE line, so the h-line window anchored on row a can only ever
	// show rows within h of a. Rows further away contribute only to the line
	// COUNTS windowStart sees, and its clamps bind only when the lines on
	// that side of the anchor number fewer than h — which implies no rows on
	// that side were dropped. Slicing to [a-h, a+h] before wrapping is
	// therefore output-identical to the full layout
	// (TestRenderWindowWrapMatchesFullLayout pins this for every anchor).
	a := anchor
	if a < 0 || a >= n {
		a = 0 // mirrors renderWindow's anchor scan: no matching row → line 0
	}
	lo = a - h
	if lo < 0 {
		lo = 0
	}
	hi = a + h + 1
	if hi > n {
		hi = n
	}
	return lo, hi
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
		lo, hi := windowRowBounds(len(rows), h, o.anchor, o.mode)
		rows = rows[lo:hi]
		o.anchor -= lo
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
		// Coloured rows only (winRow.cls): the segment's own class mask and its
		// frozen prefix, kept OUT of text so the body can be painted run by run
		// while the gutter and the padding stay under style. nil cls = the plain
		// path, where text already carries the prefix.
		cls []syntax.Class
		pre string
	}
	var dl []dline
	for ri, r := range rows {
		var segs []string
		var segCls [][]syntax.Class // nil unless the row carries a class mask
		hs := 0
		// A row whose style reverses video would turn per-token foregrounds into
		// per-token backgrounds, so it takes the plain path (see winRow.cls).
		rcls := r.cls
		if r.style.GetReverse() {
			rcls = nil
		}
		switch o.mode {
		case modeWrap:
			indent := wrapAlignIndent(r.text, bodyW)
			segs = wrapHang(r.text, bodyW, indent, 1<<20) // huge cap => clean full wrap, no ellipsis
			if rcls != nil {
				segCls = wrapSegCls(r.text, rcls, segs, indent, bodyW)
			}
		case modeScroll:
			segs = []string{hslice(r.text, o.hscroll, bodyW)}
			hs = o.hscroll
			if rcls != nil {
				segCls = [][]syntax.Class{sliceCls(rcls, hscrollRuneOff(r.text, o.hscroll), len([]rune(segs[0])))}
			}
		default:
			segs = []string{truncate(r.text, bodyW)}
			if rcls != nil {
				mask := sliceCls(rcls, 0, len([]rune(segs[0])))
				// When r.text is wider than bodyW, truncate keeps a prefix and
				// appends "…" after it — the ellipsis is a synthetic rune, not
				// part of r.text. sliceCls doesn't know that: it just slices
				// len(segs[0]) classes off the front of rcls, so the mask's
				// last entry lands on the class of the first DROPPED rune
				// (the one at index len(kept), i.e. right after the prefix
				// truncate kept). Force that slot to Plain so the ellipsis
				// never wears a colour it didn't earn.
				if lipgloss.Width(r.text) > bodyW && len(mask) > 0 {
					mask[len(mask)-1] = syntax.Plain
				}
				segCls = [][]syntax.Class{mask}
			}
		}
		if len(segs) == 0 {
			segs = []string{""}
			segCls = nil
		}
		for si, s := range segs {
			pre := ""
			if pw > 0 {
				pre = strings.Repeat(" ", pw) // blank gutter on continuations
				if si == 0 {
					pre = padRight(truncate(r.prefix, pw), pw)
				}
			}
			if segCls == nil {
				dl = append(dl, dline{text: pre + s, style: r.style, deco: r.decorate, hs: hs, si: si, row: ri})
				continue
			}
			dl = append(dl, dline{text: s, pre: pre, cls: segCls[si], style: r.style, hs: hs, si: si, row: ri})
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
		if dl[idx].cls != nil {
			out = append(out, colouredLine(dl[idx].pre, dl[idx].text, dl[idx].cls, dl[idx].style, w))
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

// colouredLine renders one display line of a class-masked row: the frozen
// prefix and the trailing padding under style, the body painted run by run
// (styledRuns, no word-diff emphasis). An all-Plain mask under the zero style
// is byte-identical to the plain path — syntaxStyle leaves base alone for
// Plain, and lipgloss renders an unstyled string unchanged.
func colouredLine(pre, body string, cls []syntax.Class, style lipgloss.Style, w int) string {
	disp := []rune(body)
	cls = sliceCls(cls, 0, len(disp)) // defensive: exactly one class per rune
	var b strings.Builder
	if pre != "" {
		b.WriteString(style.Render(pre))
	}
	b.WriteString(styledRuns(disp, make([]bool, len(disp)), cls, style))
	if pad := w - lipgloss.Width(pre) - lipgloss.Width(body); pad > 0 {
		b.WriteString(style.Render(strings.Repeat(" ", pad)))
	}
	return b.String()
}

// sliceCls returns n classes of cls starting at off, padding with Plain when
// the mask runs out (a cutoff ellipsis, a clamped token end) and reading an
// out-of-range window as all-Plain.
func sliceCls(cls []syntax.Class, off, n int) []syntax.Class {
	out := make([]syntax.Class, n)
	if off < 0 {
		off = 0
	}
	for i := 0; i < n && off+i < len(cls); i++ {
		out[i] = cls[off+i]
	}
	return out
}

// hscrollRuneOff is how many leading runes of s the modeScroll slice drops at
// horizontal offset off. Derived from ansi.TruncateLeft's own output rather
// than re-walking widths, so a wide glyph straddling the cut is counted
// exactly the way hslice cuts it.
func hscrollRuneOff(s string, off int) int {
	if off <= 0 {
		return 0
	}
	return len([]rune(s)) - len([]rune(ansi.TruncateLeft(s, off, "")))
}

// wrapSegCls maps a class mask onto the segments wrapHang produced for text.
// Every segment is a verbatim rune slice of text (wrapWidth slices runes and
// never rewrites them), preceded on continuations by indent pad spaces, so the
// mask is sliced at the running rune offset and the pad is Plain. The layout is
// verified against text before it is trusted: if the segments do not
// reconstruct text (a future wrapper that rewrote content), every segment is
// reported all-Plain and the row renders uncoloured rather than mis-coloured.
func wrapSegCls(text string, cls []syntax.Class, segs []string, indent, bodyW int) [][]syntax.Class {
	if indent > bodyW-1 {
		indent = bodyW - 1 // wrapHang's own clamp
	}
	try := func(pad int) [][]syntax.Class {
		out := make([][]syntax.Class, len(segs))
		var joined strings.Builder
		off := 0
		for i, s := range segs {
			r := []rune(s)
			p := 0
			if i > 0 {
				p = pad
			}
			if len(r) < p {
				return nil
			}
			for _, c := range r[:p] {
				if c != ' ' {
					return nil
				}
			}
			m := make([]syntax.Class, p)
			out[i] = append(m, sliceCls(cls, off, len(r)-p)...)
			joined.WriteString(string(r[p:]))
			off += len(r) - p
		}
		if joined.String() != text {
			return nil
		}
		return out
	}
	if indent > 0 {
		if out := try(indent); out != nil {
			return out
		}
	}
	if out := try(0); out != nil {
		return out
	}
	plain := make([][]syntax.Class, len(segs))
	for i, s := range segs {
		plain[i] = make([]syntax.Class, len([]rune(s)))
	}
	return plain
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

// wrapAlignIndent returns a row's wrap-mode hanging indent: the display width
// of the row's leading run of spaces and marker/graph glyphs — everything
// before its first letter or digit — capped at half the body width so a
// decoration-heavy row keeps a usable continuation column. This is intrinsic
// to modeWrap (not an option): every wrapped row's continuations start where
// that row's text starts, past whatever cursor, marker, or graph glyphs lead
// it. Per-row, because leading decoration varies row to row (a branch's `* `,
// a commit's graph lanes); rows whose text starts at column 0 — prose — get
// 0, so content viewers are unaffected by construction.
func wrapAlignIndent(s string, bodyW int) int {
	max := bodyW / 2
	w := 0
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			break
		}
		w += lipgloss.Width(string(r))
		if w >= max {
			return max
		}
	}
	if w >= lipgloss.Width(s) {
		return 0 // no letter/digit anywhere: nothing to align under
	}
	return w
}

// splitWidth splits s at the last rune boundary that fits w display columns.
// Width-aware like wrapWidth; a single glyph wider than w is emitted rather
// than looping forever.
func splitWidth(s string, w int) (head, tail string) {
	r := []rune(s)
	n, width := 0, 0
	for n < len(r) {
		cw := lipgloss.Width(string(r[n]))
		if width+cw > w {
			break
		}
		width += cw
		n++
	}
	if n == 0 && len(r) > 0 {
		n = 1
	}
	return string(r[:n]), string(r[n:])
}

// wrapHang wraps s like wrapWidth but hang-indents every continuation line by
// indent columns (the first line keeps the full width w). wrapWidth itself is
// untouched: tooltip paths depend on its exact cap/ellipsis behavior.
func wrapHang(s string, w, indent, maxLines int) []string {
	if w < 1 {
		w = 1
	}
	if indent > w-1 {
		indent = w - 1 // always leave at least one column for the text
	}
	if indent <= 0 {
		return wrapWidth(s, w, maxLines)
	}
	head, tail := splitWidth(s, w)
	if tail == "" || maxLines <= 1 {
		return wrapWidth(s, w, maxLines)
	}
	pad := strings.Repeat(" ", indent)
	out := []string{head}
	for _, seg := range wrapWidth(tail, w-indent, maxLines-1) {
		out = append(out, pad+seg)
	}
	return out
}

// wrapContentLines returns how many display lines rows occupy under o in wrap
// mode (indent included), capped at max with early exit — callers use it to
// size a popup's height budget to its wrapped content instead of the
// one-line-per-row count the other modes use. Non-wrap modes are one line per
// row by construction.
func wrapContentLines(rows []winRow, o winOpts, max int) int {
	if max < 1 {
		max = 1
	}
	if o.mode != modeWrap {
		if len(rows) < max {
			return len(rows)
		}
		return max
	}
	w := o.w
	if w < 1 {
		w = 1
	}
	pw := o.prefixW
	if pw < 0 {
		pw = 0
	}
	if pw > w-1 {
		pw = w - 1
	}
	n := 0
	for _, r := range rows {
		segs := len(wrapHang(r.text, w-pw, wrapAlignIndent(r.text, w-pw), 1<<20))
		if segs == 0 {
			segs = 1 // renderWindow substitutes one blank line for an empty row
		}
		n += segs
		if n >= max {
			return max
		}
	}
	return n
}
