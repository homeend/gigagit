package tui

import (
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/markdown"
	"github.com/homeend/gigagit/internal/syntax"
)

// Overview documents: an agent's in-memory markdown whose links are ANCHORS —
// to a working-tree file, a line or range in it, or one of the agent's
// temporary notes. The user tabs from anchor to anchor, opens one, and
// backspace brings them back (spec 2026-09-30-agent-overview-documents).
//
// gg lays the rows out itself, at the frame's reading width, so one display
// row is one line and a click maps straight to a line and a column.

// anchorTarget is what an anchor opens: a file (start 0: no line), a line or
// range in it (1-based), or a note ("t<n>").
type anchorTarget struct {
	path       string
	start, end int
	note       string
}

// anchorSpan is where an anchor sits: runes [from, to) of line. A label
// wrapped over two rows has a span on each.
type anchorSpan struct{ line, from, to int }

type anchor struct {
	dest    string // the destination as written
	target  anchorTarget
	spans   []anchorSpan // nil: clipped away (a wide table)
	missing bool         // its file or note was not found when last checked
}

// overview is the part of an open file that makes it an overview.
type overview struct {
	text    string // the agent's markdown, as sent
	anchors []anchor
	sel     int // the selected anchor; -1 = none
	w       int // the width the rows were laid out at; 0 = never
}

// parseAnchorDest reads a link destination as an anchor: `note:t<n>`, or a
// repo-relative slash path with an optional `:N` / `:N-M` line suffix. Anything
// that could leave the repository or is not a plain path is not an anchor.
func parseAnchorDest(dest string) (anchorTarget, bool) {
	d := strings.TrimSpace(dest)
	if id, ok := strings.CutPrefix(d, "note:"); ok {
		if len(id) > 1 && id[0] == 't' && allDigits(id[1:]) {
			return anchorTarget{note: id}, true
		}
		return anchorTarget{}, false
	}
	d = strings.TrimPrefix(d, "./")
	if d == "" || strings.HasPrefix(d, "/") || strings.Contains(d, "\\") || strings.Contains(d, "://") {
		return anchorTarget{}, false
	}
	if len(d) >= 2 && d[1] == ':' && (len(d) == 2 || d[2] == '/') && (d[0]|0x20) >= 'a' && (d[0]|0x20) <= 'z' {
		return anchorTarget{}, false // a Windows drive
	}
	for _, r := range d {
		if r <= ' ' || r == 0x7f {
			return anchorTarget{}, false
		}
	}
	t := anchorTarget{path: d}
	if i := strings.LastIndexByte(d, ':'); i >= 0 {
		lo, hi, ranged := d[i+1:], "", false
		if j := strings.IndexByte(lo, '-'); j >= 0 {
			lo, hi, ranged = lo[:j], lo[j+1:], true
		}
		if allDigits(lo) && (!ranged || allDigits(hi)) {
			s, _ := strconv.Atoi(lo)
			e := s
			if ranged {
				e, _ = strconv.Atoi(hi)
			}
			if s < 1 || e < s {
				return anchorTarget{}, false
			}
			t = anchorTarget{path: d[:i], start: s, end: e}
		}
	}
	if t.path == "" {
		return anchorTarget{}, false
	}
	for _, seg := range strings.Split(t.path, "/") {
		if seg == ".." {
			return anchorTarget{}, false
		}
	}
	return t, true
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isAnchorDest(d string) bool { _, ok := parseAnchorDest(d); return ok }

// overviewLines lays text out at width: the rows, and the anchors in document
// order with their spans. Links past overviewMaxAnchors become plain text.
func overviewLines(text string, width int) ([]contentLine, []anchor) {
	doc := markdown.ParseWith(text, markdown.Options{Anchor: isAnchorDest})
	var anchors []anchor
	var number func([]markdown.Inline)
	number = func(in []markdown.Inline) {
		for i := range in {
			n := &in[i]
			if n.Kind == markdown.InAnchor {
				if len(anchors) >= overviewMaxAnchors {
					*n = markdown.Inline{Kind: markdown.InText, Text: mdFlat(n.In)}
					continue
				}
				t, _ := parseAnchorDest(n.URL)
				n.Text = strconv.Itoa(len(anchors))
				anchors = append(anchors, anchor{dest: n.URL, target: t})
				continue
			}
			number(n.In)
		}
	}
	var blocks func([]markdown.Block)
	blocks = func(bs []markdown.Block) {
		for i := range bs {
			b := &bs[i]
			number(b.Inline)
			blocks(b.Blocks)
			for j := range b.Items {
				blocks(b.Items[j].Blocks)
			}
			for _, c := range b.Head {
				number(c)
			}
			for _, r := range b.Rows {
				for _, c := range r {
					number(c)
				}
			}
		}
	}
	blocks(doc.Blocks)
	rows := mdRows(doc, width)
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
