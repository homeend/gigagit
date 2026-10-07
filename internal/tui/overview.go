package tui

import (
	"strconv"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/syntax"
)

// Overview documents: an agent's in-memory markdown whose links are ANCHORS —
// to a working-tree file, a line or range in it, or one of the agent's
// temporary notes. The user tabs from anchor to anchor, opens one, and
// backspace brings them back (spec 2026-09-30-agent-overview-documents).
//
// gg lays the rows out itself, at the frame's reading width, so one display
// row is one line and a click maps straight to a line and a column.

// anchorSpan is where an anchor sits: runes [from, to) of line. A label
// wrapped over two rows has a span on each.
type anchorSpan struct{ line, from, to int }

// anchor is the TUI's copy of a store anchor plus where it was laid out.
// target is what it opens: a file (Start 0: no line), a line or range in it,
// or a note (Note "t<n>"); its Missing is not read — missing is.
type anchor struct {
	dest    string // the destination as written
	target  agentdocs.Anchor
	spans   []anchorSpan // nil: clipped away (a wide table)
	missing bool         // its file or note was not found when last checked
}

// overview is the part of an open file that makes it an overview.
type overview struct {
	text    string // the agent's markdown, as sent
	anchors []anchor
	sel     int      // the selected anchor; -1 = none
	w       int      // the width the rows were laid out at; 0 = never
	mode    dispMode // the view they were laid out for (ctrl+w)
	closed  bool     // taken out of the open files: its files lose their bands
}

// overviewLines lays text out at width for view mode: the rows, and the
// anchors in document order with their spans. Prose wraps at width; a code
// or table row past it follows the view, as every TUI text does — cut
// (an ellipsis, anchors past it have no span), wrapped onto more rows, or
// kept whole for the horizontal scroll. Links past agentdocs.MaxAnchors are
// plain text.
func overviewLines(text string, width int, mode dispMode) ([]contentLine, []anchor) {
	doc, as := agentdocs.ParseOverview(text)
	anchors := make([]anchor, len(as))
	for i, a := range as {
		anchors[i] = anchor{dest: a.Dest, target: a}
	}
	rows := overviewView(mdRowsWide(doc, width), width, mode)
	if len(rows) == 0 {
		return []contentLine{{text: i18n.T("(empty overview)")}}, nil
	}
	lines := make([]contentLine, len(rows))
	for i, row := range rows {
		cls := append([]syntax.Class(nil), row.cls...)
		for k := 0; k < len(cls); {
			c := cls[k]
			if c < mdAnchorID0 || int(c-mdAnchorID0) >= len(anchors) {
				k++
				continue
			}
			j := k
			for j < len(cls) && cls[j] == c {
				cls[j] = mdAnchor
				j++
			}
			a := &anchors[c-mdAnchorID0]
			a.spans = append(a.spans, anchorSpan{line: i, from: k, to: j})
			k = j
		}
		lines[i] = contentLine{text: row.text, raw: row.text, src: true, cls: cls, noWrap: true}
	}
	return lines, anchors
}

// overviewView fits the rows wider than width to mode: cut with an
// ellipsis, split into width-wide rows, or left whole (modeScroll).
func overviewView(rows []mdRow, width int, mode dispMode) []mdRow {
	if width <= 0 || mode == modeScroll {
		return rows
	}
	var out []mdRow
	for _, row := range rows {
		if lipgloss.Width(row.text) <= width {
			out = append(out, row)
			continue
		}
		if mode == modeCutoff {
			cut := mdClip(row, width-1)
			out = append(out, mdRow{text: cut.text + "…", cls: append(cut.cls, mdDim), pre: row.pre})
			continue
		}
		for rest := row; rest.text != ""; {
			piece := mdClip(rest, width)
			if piece.text == "" { // a glyph wider than the whole column
				piece = mdRow{text: string([]rune(rest.text)[:1]), cls: rest.cls[:1]}
			}
			piece.pre = row.pre
			out = append(out, piece)
			n := len([]rune(piece.text))
			rest = mdRow{text: string([]rune(rest.text)[n:]), cls: rest.cls[n:]}
		}
	}
	return out
}

// paint gives every anchor span its class: selected, gone, or plain.
func (ov *overview) paint(lines []contentLine) {
	for i, a := range ov.anchors {
		c := mdAnchor
		switch {
		case i == ov.sel:
			c = mdAnchorSel
		case a.missing:
			c = mdAnchorGone
		}
		for _, s := range a.spans {
			if s.line < len(lines) && s.to <= len(lines[s.line].cls) {
				for k := s.from; k < s.to; k++ {
					lines[s.line].cls[k] = c
				}
			}
		}
	}
}

// newOverviewDocFrom is the TUI's document for a store overview, under the
// store's id, not yet laid out. It starts backgrounded: esc steps aside, only
// X closes it.
func newOverviewDocFrom(o agentdocs.Overview) *openFile {
	d := newOpenFileSeq(fileSource{kind: srcOverview}, "overview-"+strconv.FormatInt(o.Seq, 10)+".md", o.Seq) // named by its id
	d.title, d.backgrounded = o.Title, true
	d.p.prose, d.p.mode = true, modeScroll
	d.ov = &overview{text: o.Text, sel: -1}
	return d
}

// overviewLabel is how a status line names overview d after the word
// "overview": its id and quoted title (agentdocs.OverviewName's words).
func (d *openFile) overviewLabel() string { return d.id() + " " + strconv.Quote(d.title) }

// overviewCopy is d as a store overview, from the TUI's copy (the wire, r).
func (d *openFile) overviewCopy() agentdocs.Overview {
	o := agentdocs.Overview{ID: d.id(), Seq: d.seq, Title: d.title, Text: d.ov.text}
	for _, a := range d.ov.anchors {
		t := a.target
		t.Dest, t.Missing = a.dest, a.missing
		o.Anchors = append(o.Anchors, t)
	}
	return o
}

// adoptOverview brings d's copy up to the store's o: a new text or title is
// laid out again (the selected anchor kept by its destination), and the
// missing flags follow the last check.
func (d *openFile) adoptOverview(o agentdocs.Overview, rows, width int) {
	if o.Text != d.ov.text || o.Title != d.title {
		ov := d.ov
		old := ""
		if ov.sel >= 0 && ov.sel < len(ov.anchors) {
			old = ov.anchors[ov.sel].dest
		}
		ov.text, ov.sel, d.title = o.Text, -1, o.Title
		d.layOut(rows, width)
		for i, a := range ov.anchors {
			if old != "" && a.dest == old {
				d.selectAnchor(i, rows)
				break
			}
		}
	}
	if len(o.Anchors) == len(d.ov.anchors) {
		for i, a := range o.Anchors {
			if a.Dest == d.ov.anchors[i].dest {
				d.ov.anchors[i].missing = a.Missing
			}
		}
	}
	d.ov.paint(d.p.lines)
}

// overviewWidth is the width an overview is laid out at in a full-screen
// viewer whose content is innerW wide: its reading column.
func (m Model) overviewWidth(innerW int) int {
	w, _ := readingColumn(innerW, m.readingWidth())
	return w
}

// layOut lays the overview out at width for a rows-row frame — its first
// load, a new width, a new text. The reader's place is kept: the selected
// anchor when there is one, else the cursor line and the window top.
func (d *openFile) layOut(rows, width int) {
	ov := d.ov
	lines, anchors := overviewLines(ov.text, width, d.p.mode)
	if len(anchors) == len(ov.anchors) { // the same text: keep what a check found
		for i := range anchors {
			if anchors[i].dest == ov.anchors[i].dest {
				anchors[i].missing = ov.anchors[i].missing
			}
		}
	}
	ov.anchors, ov.w, ov.mode = anchors, width, d.p.mode
	if ov.sel >= len(anchors) {
		ov.sel = -1
	}
	d.fill(fileContentMsg{tag: d.tag, lines: lines, reload: docLoaded(d)}, rows, width)
	if ov.sel >= 0 {
		d.selectAnchor(ov.sel, rows)
		return
	}
	ov.paint(d.p.lines)
}

// selectAnchor selects anchor i: the cursor goes to its first row, which is
// scrolled into view — across too, in the horizontal-scroll view. An anchor
// cut out of every row is selected where the cursor is.
func (d *openFile) selectAnchor(i, rows int) {
	ov := d.ov
	ov.sel = i
	if i >= 0 && i < len(ov.anchors) && len(ov.anchors[i].spans) > 0 {
		s := ov.anchors[i].spans[0]
		d.p.cur = s.line
		d.p.ensureCursorVisible(rows)
		if d.p.mode == modeScroll && s.line < len(d.p.lines) {
			r := []rune(d.p.lines[s.line].text)
			from, to := lipgloss.Width(string(r[:s.from])), lipgloss.Width(string(r[:s.to]))
			d.p.hscroll = panTo(d.p.hscroll, from, to, ov.w)
		}
	}
	ov.paint(d.p.lines)
}

// panTo is the horizontal offset that shows columns [from, to) in a
// width-wide window at hscroll: unchanged when they show, else the least pan
// that brings them in (back to 0 when they fit the first screen).
func panTo(hscroll, from, to, width int) int {
	switch {
	case width <= 0 || (from >= hscroll && to <= hscroll+width):
		return hscroll
	case to <= width:
		return 0
	case to-from > width:
		return from
	}
	return to - width
}

// selectAnchorAt selects anchor i with the cursor on line, one of its rows
// the user clicked: already on screen, so nothing scrolls and a second click
// on the same spot lands on it again.
func (d *openFile) selectAnchorAt(i, line, rows int) {
	d.ov.sel = i
	d.p.cur = line
	d.p.ensureCursorVisible(rows)
	d.ov.paint(d.p.lines)
}
