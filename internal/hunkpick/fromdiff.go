package hunkpick

import (
	"bytes"

	"github.com/homeend/gigagit/internal/textdiff"
)

// eolOf reports whether b ends every line with CRLF, and whether it has any
// newlines at all (a side with none — or a nil side — expresses no opinion).
func eolOf(b []byte) (crlf, any bool) {
	nl := bytes.Count(b, []byte("\n"))
	if nl == 0 {
		return false, false
	}
	return bytes.Count(b, []byte("\r\n")) == nl, true
}

// FromDiff builds a Doc from a line diff of two file versions (left = the
// baseline, e.g. the index; right = the new side, e.g. the working tree).
// Equal runs become Literal items; each contiguous changed run becomes a
// Block{Current: left lines, Incoming: right lines}. Blocks start Undecided;
// the caller sets the default mode. Used by hunk staging.
func FromDiff(left, right []byte) *Doc {
	res := textdiff.Compare(left, right, textdiff.Options{})
	d := &Doc{FinalNewline: len(right) > 0 && right[len(right)-1] == '\n'}

	var lit []string
	var cur, inc []string
	inBlock := false
	flushLit := func() {
		if len(lit) > 0 {
			d.Items = append(d.Items, Item{Literal: lit})
			lit = nil
		}
	}
	flushBlock := func() {
		if inBlock {
			d.Items = append(d.Items, Item{Block: &Block{Current: cur, Incoming: inc}})
			cur, inc, inBlock = nil, nil, false
		}
	}

	for _, row := range res.Rows {
		if row.Kind == textdiff.Same {
			flushBlock()
			lit = append(lit, row.Left)
			continue
		}
		// a changed row (Changed/Del/Add): start/continue a block.
		flushLit()
		inBlock = true
		switch row.Kind {
		case textdiff.Changed:
			cur = append(cur, row.Left)
			inc = append(inc, row.Right)
		case textdiff.Del:
			cur = append(cur, row.Left)
		case textdiff.Add:
			inc = append(inc, row.Right)
		}
	}
	flushBlock()
	flushLit()

	lc, la := eolOf(left)
	rc, ra := eolOf(right)
	// CRLF only when at least one side has newlines and every side that
	// does is consistently CRLF; anything mixed stays LF (documented).
	if (la || ra) && (!la || lc) && (!ra || rc) && (lc || rc) {
		d.EOL = "\r\n"
	}

	return d
}
