package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/textdiff"
)

// cellMark is the cursor marker for one rendered cell: when row is set, base
// is laid under every non-hot run and the padding (hot add/del backgrounds
// win — they stay as they are); gut styles the gutter number. noMark() is the
// unmarked default.
type cellMark struct {
	row  bool
	base lipgloss.Style
	gut  lipgloss.Style
}

// noMark is a function, not a package var: it reads st(), and package-level
// var initializers run before styles.go's init() stores the first *styles —
// a package var here would race init order and read a nil pointer.
func noMark() cellMark { return cellMark{gut: st().diffGutter} }

// cursorMark is the cellMark for a cursor row under the given style.
func cursorMark(style string) cellMark {
	s := st()
	switch style {
	case "row":
		return cellMark{row: true, base: s.diffCursorRow, gut: s.diffCursorNo}
	case "number":
		return cellMark{gut: s.diffCursorNo}
	}
	return noMark()
}

// hotFor is the background a hot (add/del) cell wears: its own shade, or the
// one-step-brighter cursor variant when the row is the cursor row.
func (mk cellMark) hotFor(hot lipgloss.Style) lipgloss.Style {
	if !mk.row {
		return hot
	}
	s := st()
	switch hot.GetBackground() {
	case s.diffAddCell.GetBackground():
		return s.diffAddCursor
	case s.diffDelCell.GetBackground():
		return s.diffDelCursor
	}
	return hot
}

// gapFor is the style of the dotted gap filler: banded on the cursor row.
func (mk cellMark) gapFor() lipgloss.Style {
	if mk.row {
		return st().diffGapCursor
	}
	return st().diffGapCell
}

// diffHintFor builds the diff-view hint for the current long-line mode. Every
// diff-view binding that is not help-only appears here, so the line is packed:
// the widest (scroll) English variant measures 139 display columns and MUST
// stay at or under 140, the width TestRenderDiffViewPanes renders at and the
// narrowest common wide terminal — past that the truncation eats [esc] close
// first, hiding the way out. That budget is why the labels are terse (chg,
// hist/blame) and why the three note keys share one [c/}{] notes group
// (E/R/a are help-and-menu-only). Shortening a label is the way to add a
// group; growing the line is not. The scroll variant appends the pan keys.
func diffHintFor(long longMode) string {
	mode := i18n.T("scroll")
	switch long {
	case longWrap:
		mode = i18n.T("wrap")
	case longTruncate:
		mode = i18n.T("trunc")
	}
	pan := ""
	if long == longScroll {
		pan = i18n.T("  [←→/0] pan")
	}
	return i18n.T("[↑↓] scroll  [j/k] line  [c/}{] notes  [z] align  [e] edit  [n/p] chg  [f] part  [ctrl+w] %s", mode) + pan + i18n.T("  [h/b] hist/blame  [esc] close")
}

// cellSeg is one pane's text for one display row: the sanitized display runes
// with the parallel emphasis and syntax-class masks, already ≤ the pane's text
// width. A zero cellSeg renders blank.
type cellSeg struct {
	disp []rune
	emph []bool
	cls  []syntax.Class
}

// wrapCells splits a sanitized (disp, emph, cls) line into segments each ≤ tw
// display columns. It greedily fills to tw, then breaks after the last space
// in the segment (word-wrap); a word longer than tw is hard-broken at the fill
// boundary, and a single rune wider than tw is taken alone (never an empty
// loop). The emph and cls masks are sliced alongside so emphasis and syntax
// colour survive a split. An empty input yields one empty segment (so the row
// still draws a line).
func wrapCells(disp []rune, emph []bool, cls []syntax.Class, tw int) []cellSeg {
	if tw < 1 {
		tw = 1
	}
	if len(disp) == 0 {
		return []cellSeg{{}}
	}
	// Callers pass sanitizeCell's parallel mask; nil is the plain/test shorthand.
	if cls == nil {
		cls = make([]syntax.Class, len(disp))
	}
	var segs []cellSeg
	start := 0
	for start < len(disp) {
		end, width := start, 0
		for end < len(disp) {
			rw := lipgloss.Width(string(disp[end]))
			if width+rw > tw {
				break
			}
			width += rw
			end++
		}
		if end == start { // a single rune wider than tw
			end = start + 1
		}
		brk := end
		if end < len(disp) { // more to come: prefer a word boundary
			sp := -1
			for j := start; j < end; j++ {
				if disp[j] == ' ' {
					sp = j
				}
			}
			if sp > start {
				brk = sp + 1 // keep the space on this segment
			}
		}
		segs = append(segs, cellSeg{disp: disp[start:brk], emph: emph[start:brk], cls: cls[start:brk]})
		start = brk
	}
	return segs
}

// sanitizeLine makes raw file content safe to render on one line: tabs
// expand to a fixed 4-column stop (lipgloss.Width doesn't expand them but
// the terminal would, pushing text through the pane separator), a trailing
// \r is stripped, remaining control characters become '·'. Display only —
// Compare sees the raw lines.
func sanitizeLine(s string) string {
	s = strings.TrimSuffix(s, "\r")
	var b strings.Builder
	col := 0
	for _, r := range s {
		switch {
		case r == '\t':
			n := 4 - col%4
			b.WriteString(strings.Repeat(" ", n))
			col += n
		case r < 0x20 || r == 0x7f:
			b.WriteRune('·')
			col++
		default:
			b.WriteRune(r)
			col++
		}
	}
	return b.String()
}

// withDiffFileNotice overlays the diff view's bottom-left notice — the primed
// file-step cue, or the transient arrival / no-file message — as a rounded box,
// mirroring the search-recall dropdown's look and anchor. A no-op when the diff
// isn't the active surface (a popup/layer on top owns the screen) or there is
// nothing to show. NOTE: shares the x=2/bottom anchor with withRecall — safe
// today because recall never opens over the diff; revisit if it ever does.
func (m Model) withDiffFileNotice(frame string) string {
	if m.diffLayer() == nil {
		return frame
	}
	// If the diff is not the top layer, a popup/surface owns the screen above it.
	if top := m.topLayer(); top != nil && top != m.diffLayer() {
		return frame
	}
	msg := m.diffNotice
	if msg == "" {
		msg = fileArmCue(m.diffLayer().fileArm)
	}
	if msg == "" {
		msg = m.boundaryCue() // proactive: advertise wrap + N/P file-step at a boundary
	}
	if msg == "" {
		return frame
	}
	w, h := m.overlayDims()
	if maxW := w - 6; lipgloss.Width(msg) > maxW && maxW > 0 {
		msg = truncate(msg, maxW)
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Render(msg)
	top := h - lipgloss.Height(box) - 1
	if top < 0 {
		top = 0
	}
	return overlayAt(frame, box, 2, top, w, h)
}

// renderDiffView draws the whole screen: header, aligned panes, hint line.
func (m Model) renderDiffView() string {
	v := m.diffLayer()
	w, h := m.overlayDims()
	body := h - 2 // header + hint are the only chrome (must match diffBodyRows)
	if body < 1 {
		body = 1
	}

	note := ""
	switch {
	case v.truncated:
		note = i18n.T("  (alignment skipped: large file)")
	// loading/err/binary/tooLarge render their own body state below; the
	// guards here keep the note from doubling up with them.
	case !v.loading && v.err == nil && !v.binary && !v.tooLarge && len(v.blocks) == 0:
		note = i18n.T("  (no content difference)")
	}
	head := i18n.T("diff: %s", v.title) + "  " + v.context + note
	// Right-aligned status: which change is in view (1-based) of how many, then
	// the visible row range.
	right := ""
	if len(v.blocks) > 0 {
		right = i18n.T("change %d/%d", v.currentBlockOrdinal()+1, len(v.blocks))
	}
	if r, ok := v.cursorRow(); ok {
		ln := ""
		if r.RightNo > 0 {
			ln = i18n.T("line %d", r.RightNo)
		} else if r.LeftNo > 0 {
			ln = i18n.T("old line %d", r.LeftNo)
		}
		if ln != "" {
			if right != "" {
				right = ln + "  " + right
			} else {
				right = ln
			}
		}
	}
	if n := len(v.disp); n > 0 {
		hi := v.offset + body
		if hi > n {
			hi = n
		}
		rangeStr := i18n.T("rows %d–%d/%d", v.offset+1, hi, n)
		if right != "" {
			right += "  " + rangeStr
		} else {
			right = rangeStr
		}
	}
	// Primed wrap-around cue: only when armed, so the unarmed header stays
	// byte-identical. Leads the status so a narrow terminal keeps the prompt.
	if cue := wrapCue(v.wrapArm); cue != "" {
		if right != "" {
			right = cue + "  " + right
		} else {
			right = cue
		}
	}
	avail := w - lipgloss.Width(right) - 2
	if avail < 1 {
		avail = 1
	}
	head = padRight(truncate(head, avail), avail)
	header := truncate(head+"  "+right, w)

	lines := make([]string, 0, h)
	lines = append(lines, header)
	switch {
	case v.loading:
		lines = append(lines, i18n.T("  (loading…)"))
	case v.err != nil:
		lines = append(lines, truncate(i18n.T("  error: %s", v.err.Error()), w))
	case v.binary:
		lines = append(lines, i18n.T("  (binary file)"))
	case v.tooLarge:
		lines = append(lines, i18n.T("  (file too large)"))
	default:
		s, e := v.cursorDispRange()
		lines = append(lines, m.diffPaneLines(v, w, body, s, e, m.cursorStyle())...)
	}
	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	lines = append(lines, truncate(diffHintFor(v.long), w))
	return strings.Join(lines, "\n")
}

// gutterWidth is the line-number column width, derived from the full rows so it
// is stable across mode/wrap toggles. Minimum 3.
func gutterWidth(full []textdiff.Row) int {
	maxNo := 0
	for _, r := range full {
		if r.LeftNo > maxNo {
			maxNo = r.LeftNo
		}
		if r.RightNo > maxNo {
			maxNo = r.RightNo
		}
	}
	g := len(fmt.Sprint(maxNo))
	if g < 3 {
		g = 3
	}
	return g
}

// diffPaneLines renders the visible window of display rows. A note dRow is a
// full-width review-note row; a fold dRow is a full-width separator. Otherwise: wrap off draws the row via diffCell (raw
// text, truncated — byte-identical to before); wrap on draws each side's
// pre-wrapped segment via segCell. Display rows in [curStart, curEnd) carry
// the cursor marker per style ("row" | "number" | "off"); curStart == curEnd
// (the history pane) draws no marker. A fold row is never marked.
func (m Model) diffPaneLines(v *diffView, w, body int, curStart, curEnd int, style string) []string {
	paneW := (w - 1) / 2
	if paneW < 4 {
		paneW = 4
	}
	gut := gutterWidth(v.full)
	s := st()

	out := make([]string, 0, body)
	for i := v.offset; i < v.offset+body && i < len(v.disp); i++ {
		dr := v.disp[i]
		if dr.note != nil {
			out = append(out, noteRowCells(*dr.note, paneW))
			continue
		}
		if dr.fold > 0 {
			out = append(out, foldSeparator(dr.fold, w, dr.noteMark))
			continue
		}
		mk := noMark()
		if i >= curStart && i < curEnd {
			mk = cursorMark(style)
		}
		r := dr.row
		// Syntax runs for this row's source lines (nil on a gap side or an
		// unlexed file); the wrap case already carries them in dr.left/right.
		lt, rt := tokAt(v.oldTok, r.LeftNo), tokAt(v.newTok, r.RightNo)
		switch v.long {
		case longWrap:
			leftGap := r.Kind == textdiff.Add
			rightGap := r.Kind == textdiff.Del
			leftNo, rightNo := 0, 0
			if dr.first && !leftGap {
				leftNo = r.LeftNo
			}
			if dr.first && !rightGap {
				rightNo = r.RightNo
			}
			left := segCell(leftNo, dr.left, gut, paneW, leftGap,
				r.Kind == textdiff.Del || r.Kind == textdiff.Changed, s.diffDelCell, mk)
			right := segCell(rightNo, dr.right, gut, paneW, rightGap,
				r.Kind == textdiff.Add || r.Kind == textdiff.Changed, s.diffAddCell, mk)
			out = append(out, left+"│"+right)
		case longTruncate:
			left := diffCell(r.LeftNo, r.Left, gut, paneW,
				r.Kind == textdiff.Add,
				r.Kind == textdiff.Del || r.Kind == textdiff.Changed, s.diffDelCell, r.LeftSpans, lt, mk)
			right := diffCell(r.RightNo, r.Right, gut, paneW,
				r.Kind == textdiff.Del,
				r.Kind == textdiff.Add || r.Kind == textdiff.Changed, s.diffAddCell, r.RightSpans, rt, mk)
			out = append(out, left+"│"+right)
		default: // longScroll
			left := scrollCell(r.LeftNo, r.Left, r.LeftSpans, lt, v.hOffset, gut, paneW,
				r.Kind == textdiff.Add,
				r.Kind == textdiff.Del || r.Kind == textdiff.Changed, s.diffDelCell, mk)
			right := scrollCell(r.RightNo, r.Right, r.RightSpans, rt, v.hOffset, gut, paneW,
				r.Kind == textdiff.Del,
				r.Kind == textdiff.Add || r.Kind == textdiff.Changed, s.diffAddCell, mk)
			out = append(out, left+"│"+right)
		}
	}
	return out
}

// segCell renders one pane's pre-wrapped segment into a width-col cell: gutter
// (number when no>0, blank on a continuation) + the styled, padded body. gap
// draws the · filler (absent side). hot applies the add/del background;
// emphasis rides in seg.emph.
func segCell(no int, seg cellSeg, gut, width int, gap, hot bool, hotStyle lipgloss.Style, mk cellMark) string {
	if gap {
		return mk.gapFor().Render(strings.Repeat("·", width))
	}
	if gut > width-2 { // degenerate pane: keep the cell inside its width
		gut = width - 2
		if gut < 1 {
			gut = 1
		}
	}
	num := strings.Repeat(" ", gut+1)
	if no > 0 {
		num = fmt.Sprintf("%*d ", gut, no)
	}
	tw := width - gut - 1
	if tw < 1 {
		tw = 1
	}
	base := lipgloss.NewStyle()
	if mk.row {
		base = mk.base
	}
	if hot {
		base = mk.hotFor(hotStyle)
	}
	body := styledRuns(seg.disp, seg.emph, seg.cls, base)
	if pad := tw - lipgloss.Width(string(seg.disp)); pad > 0 {
		body += base.Render(strings.Repeat(" ", pad))
	}
	return mk.gut.Render(truncate(num, gut+1)) + body
}

// scrollCell renders one pane's line through a horizontal window starting at
// hOffset display columns. At hOffset==0 with a line that fits the pane it
// delegates to diffCell (byte-identical to truncate at rest). Otherwise it
// shows the column slice, with ‹ in the first column when hOffset>0 and › in
// the last when text extends past the window. Emphasis and syntax classes ride
// in the sanitized masks and are sliced with the window.
func scrollCell(no int, text string, spans []textdiff.Span, toks []syntax.Tok, hOffset, gut, width int, gap, hot bool, hotStyle lipgloss.Style, mk cellMark) string {
	if gap {
		return mk.gapFor().Render(strings.Repeat("·", width))
	}
	if gut > width-2 { // degenerate pane: keep the cell inside its width
		gut = width - 2
		if gut < 1 {
			gut = 1
		}
	}
	tw := width - gut - 1
	if tw < 1 {
		tw = 1
	}
	disp, emph, cls := sanitizeCell(text, spans, toks)
	full := lipgloss.Width(string(disp))
	if hOffset <= 0 && full <= tw {
		return diffCell(no, text, gut, width, false, hot, hotStyle, spans, toks, mk)
	}
	hasLeft := hOffset > 0
	hasRight := full > hOffset+tw
	// Content occupies the window minus any marker columns.
	contentStart, contentEnd := hOffset, hOffset+tw
	if hasLeft {
		contentStart++
	}
	if hasRight {
		contentEnd--
	}
	var wdisp []rune
	var wemph []bool
	var wcls []syntax.Class
	col := 0
	for i, r := range disp {
		rw := lipgloss.Width(string(r))
		if col >= contentStart && col+rw <= contentEnd {
			wdisp = append(wdisp, r)
			wemph = append(wemph, emph[i])
			wcls = append(wcls, cls[i])
		}
		col += rw
	}
	base := lipgloss.NewStyle()
	if mk.row {
		base = mk.base
	}
	if hot {
		base = mk.hotFor(hotStyle)
	}
	var b strings.Builder
	if hasLeft {
		// The pan markers are body furniture, not gutter: "number" mode bolds
		// the line number only, so they keep the plain gutter style.
		b.WriteString(st().diffGutter.Render("‹"))
	}
	b.WriteString(styledRuns(wdisp, wemph, wcls, base))
	inner := tw
	if hasLeft {
		inner--
	}
	if hasRight {
		inner--
	}
	if pad := inner - lipgloss.Width(string(wdisp)); pad > 0 {
		b.WriteString(base.Render(strings.Repeat(" ", pad)))
	}
	if hasRight {
		b.WriteString(st().diffGutter.Render("›"))
	}
	num := fmt.Sprintf("%*d ", gut, no)
	return mk.gut.Render(truncate(num, gut+1)) + b.String()
}

// maxCellWidth is the widest single cell (either side, gap sides skipped)
// across the logical lines — the horizontal extent scroll mode can pan to.
func maxCellWidth(lines []textdiff.Line) int {
	max := 0
	for _, ln := range lines {
		if ln.Fold > 0 {
			continue
		}
		r := ln.Row
		if r.Kind != textdiff.Add {
			if w := lipgloss.Width(sanitizeLine(r.Left)); w > max {
				max = w
			}
		}
		if r.Kind != textdiff.Del {
			if w := lipgloss.Width(sanitizeLine(r.Right)); w > max {
				max = w
			}
		}
	}
	return max
}

// foldSeparator renders a fold marker as a centered label on a dim rule
// spanning the full width. marked prefixes ◆: a review note anchors on a line
// this fold hides (f, or }/{, brings it into view).
func foldSeparator(n, w int, marked bool) string {
	label := i18n.T(" ⤬ %d unchanged lines ", n)
	if n == 1 {
		label = i18n.T(" ⤬ 1 unchanged line ")
	}
	if marked {
		label = "◆" + label
	}
	lw := lipgloss.Width(label)
	if lw >= w {
		return st().diffFold.Render(truncate(label, w))
	}
	left := (w - lw) / 2
	right := w - lw - left
	return st().diffFold.Render(strings.Repeat("─", left) + label + strings.Repeat("─", right))
}

// noteRowCells paints one box row as a full diff row: the box in the pane the
// note belongs to (old = left, new = right) and blank space in the other, so
// the note visibly hangs off one version of the file. The text was wrapped to
// the pane when the rows were laid out; truncate is only a guard against a
// width the layout has not caught up with. Frame rows draw the rounded rule
// (the title sits in the top one), summary rows bold, rationale rows dim, and
// a stale box is grey throughout.
func noteRowCells(nl noteLine, paneW int) string {
	if paneW < 4 {
		paneW = 4
	}
	s := st()
	frame := s.noteFrameUser
	if nl.agent {
		frame = s.noteFrameAgent
	}
	text := s.noteBody
	if nl.kind == noteRowSummary {
		text = s.noteSummary
	}
	if nl.stale {
		frame, text = s.noteFrameStale, s.noteDim
	}
	inner := paneW - noteBoxFrame
	var cell string
	switch nl.kind {
	case noteRowTop:
		title := truncate(sanitizeLine(nl.text), paneW-6) // "╭─ " + title + " ─╮" at least
		rule := paneW - 4 - lipgloss.Width(title)
		if rule < 1 {
			rule = 1
		}
		cell = frame.Render("╭─ " + title + " " + strings.Repeat("─", rule-1) + "╮")
	case noteRowBottom:
		cell = frame.Render("╰" + strings.Repeat("─", paneW-2) + "╯")
	case noteRowBlank:
		cell = frame.Render("│") + strings.Repeat(" ", paneW-2) + frame.Render("│")
	default:
		body := padRight(truncate(sanitizeLine(nl.text), inner), inner)
		cell = frame.Render("│ ") + text.Render(body) + frame.Render(" │")
	}
	blank := strings.Repeat(" ", paneW)
	if nl.side == model.NoteSideOld {
		return cell + "│" + blank
	}
	return blank + "│" + cell
}

// diffCell renders one pane cell: gutter + text, or the dim gap filler. With
// neither spans nor syntax runs (plain mode, non-Changed rows, unknown
// language, or enrichment give-up) it is byte-identical to the pre-enrichment
// renderer; otherwise it layers intraline emphasis and syntax colour over the
// (optional) hot cell background.
func diffCell(no int, text string, gut, width int, gap, hot bool, hotStyle lipgloss.Style, spans []textdiff.Span, toks []syntax.Tok, mk cellMark) string {
	if gap {
		return mk.gapFor().Render(strings.Repeat("·", width))
	}
	if gut > width-2 { // degenerate pane: keep the cell inside its width
		gut = width - 2
		if gut < 1 {
			gut = 1
		}
	}
	num := fmt.Sprintf("%*d ", gut, no)
	tw := width - gut - 1
	if tw < 1 {
		tw = 1
	}
	var bodyTxt string
	if len(spans) > 0 || len(toks) > 0 {
		base := lipgloss.NewStyle()
		if mk.row {
			base = mk.base
		}
		if hot {
			base = mk.hotFor(hotStyle)
		}
		bodyTxt = hotEmphBody(text, spans, toks, tw, base)
	} else {
		bodyTxt = padRight(truncate(sanitizeLine(text), tw), tw)
		switch {
		case hot:
			bodyTxt = mk.hotFor(hotStyle).Render(bodyTxt)
		case mk.row:
			bodyTxt = mk.base.Render(bodyTxt)
		}
	}
	return mk.gut.Render(truncate(num, gut+1)) + bodyTxt
}

// hotEmphBody renders an enriched cell's text into a tw-column body: sanitized
// like sanitizeLine, the whole cell carrying base (the hot add/del background,
// or a zero style on a context row), with the runes whose raw index falls in a
// span additionally wearing st().diffEmph and the rest wearing their syntax class's
// foreground. Truncation mirrors truncate()'s trailing ellipsis.
func hotEmphBody(text string, spans []textdiff.Span, toks []syntax.Tok, tw int, base lipgloss.Style) string {
	disp, emph, cls := sanitizeCell(text, spans, toks)
	if lipgloss.Width(string(disp)) <= tw {
		body := styledRuns(disp, emph, cls, base)
		if pad := tw - lipgloss.Width(string(disp)); pad > 0 {
			body += base.Render(strings.Repeat(" ", pad))
		}
		return body
	}
	if tw == 1 {
		return base.Render("…")
	}
	w, cut := 0, len(disp)
	for i, r := range disp {
		rw := lipgloss.Width(string(r))
		if w+rw+1 > tw { // reserve one column for the ellipsis
			cut = i
			break
		}
		w += rw
	}
	body := styledRuns(disp[:cut], emph[:cut], cls[:cut], base) + base.Render("…")
	// A double-width rune at the cut boundary can leave the body one column
	// short; pad to tw so the row width matches the plain path exactly.
	if pad := tw - lipgloss.Width(body); pad > 0 {
		body += base.Render(strings.Repeat(" ", pad))
	}
	return body
}

// styledRuns renders disp grouping consecutive runes by (emph, cls): emphasized
// runs wear st().diffEmph inherited over base (so the cell background shows through),
// the rest wear base plus their syntax class's foreground. Emphasis WINS over
// the syntax colour so the word-diff stays legible. An all-Plain cls with no
// emphasis renders byte-identically to the pre-syntax renderer.
func styledRuns(disp []rune, emph []bool, cls []syntax.Class, base lipgloss.Style) string {
	s := st()
	var b strings.Builder
	for i := 0; i < len(disp); {
		j := i + 1
		for j < len(disp) && emph[j] == emph[i] && cls[j] == cls[i] {
			j++
		}
		seg := string(disp[i:j])
		switch {
		case emph[i]:
			b.WriteString(base.Inherit(s.diffEmph).Render(seg))
		default:
			b.WriteString(s.syntaxStyle(base, cls[i]).Render(seg))
		}
		i = j
	}
	return b.String()
}
