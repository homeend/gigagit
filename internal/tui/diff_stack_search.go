package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// In-view search inside a STACK (design §11 item 2). The stream a stack
// splices is one document, so `/`, `@`, `]` and `[` already search every file
// whose diff has arrived and is unfolded — nothing here re-implements the
// search. What a stack adds is the part of the document that is NOT in the
// stream: a folded file's rows, and a file that has never been fetched. Those
// are reached the way a }/{ note step reaches them (diff_stack_notes.go) — the
// file is unfolded, sent for, and the step is PARKED until its lines exist.

// searchableFile reports whether file i could hold a hit at all. A conflicted
// file is a header and a resolver line; a binary, too-large, errored or
// content-identical file has no rows of its own. None of them is ever stepped
// into, and none of them holds the badge's "+" open.
func (v *diffView) searchableFile(i int) bool {
	if v.stk == nil || i < 0 || i >= len(v.stk.files) {
		return false
	}
	f := v.stk.files[i]
	if f.conflict || f.bin {
		return false
	}
	if d := f.d; d != nil {
		return d.err == nil && !d.binary && !d.tooLarge && len(d.full) > 0
	}
	return true // not fetched: assume it has rows until it says otherwise
}

// unsearchedFiles counts the searchable files whose rows are not in the stream:
// folded, or never fetched. It is what the badge's "+" and the ]/[ step read.
func (v *diffView) unsearchedFiles() int {
	if v.stk == nil {
		return 0
	}
	n := 0
	for i := range v.stk.files {
		if !v.searchableFile(i) {
			continue
		}
		if v.stk.files[i].collapsed || v.stk.files[i].d == nil {
			n++
		}
	}
	return n
}

// searchBadge is the header's search status. Stacked, a trailing "+" says the
// count is over the files searched SO FAR — more may turn up when ] steps into
// a folded or unfetched file (design D1). Like badge() it carries no i18n.T:
// it is punctuation around the user's own query.
func (v *diffView) searchBadge() string {
	bd := v.search.badge()
	if bd == "" || v.stk == nil || v.search.query == "" || v.unsearchedFiles() == 0 {
		return bd
	}
	return bd + "+"
}

// hitIn is the index into v.search.hits of the first (dir>0) / last (dir<0)
// hit whose row lies in [lo, hi] — one file's edge hit.
func (v *diffView) hitIn(lo, hi, dir int) (int, bool) {
	best, found := -1, false
	for i, h := range v.search.hits {
		if h.row < lo || h.row > hi {
			continue
		}
		if !found || (dir > 0 && i < best) || (dir < 0 && i > best) {
			best, found = i, true
		}
	}
	return best, found
}

// huntTarget is the next file in dir that is searchable but not searched — the
// file a ] / [ must open to keep going. It starts from the file after `from`.
func (v *diffView) huntTarget(from, dir int) (int, bool) {
	if v.stk == nil {
		return 0, false
	}
	for i := from + dir; i >= 0 && i < len(v.stk.files); i += dir {
		if !v.searchableFile(i) {
			continue
		}
		if v.stk.files[i].collapsed || v.stk.files[i].d == nil {
			return i, true
		}
	}
	return 0, false
}

// searchFile is the file the search is currently IN: the current hit's file,
// else the cursor's. It is where a hunt counts from.
func (v *diffView) searchFile() int {
	if v.search.cur >= 0 && v.search.cur < len(v.search.hits) {
		if r := v.search.hits[v.search.cur].row; r >= 0 && r < len(v.lines) {
			return v.lines[r].file
		}
	}
	return v.curFile()
}

// stackHitStep is ] / [ in a stack. It is STRICT first: a hit further on in the
// stream wins outright. Only when the direction is exhausted does the stack
// look for a file it has not searched — running out is the signal, which is
// why the wrap is deferred until nothing is left to open (design D3).
func (m Model) stackHitStep(v *diffView, dir, body int) (Model, tea.Cmd, bool) {
	if v.stk == nil || v.search.query == "" {
		return m, nil, false
	}
	pos := v.searchPos()
	if i := stepHitStrict(v.search.hits, pos, dir); i >= 0 {
		v.stk.hunt = nil
		v.goToHit(i, body)
		v.syncStackTitle()
		return m, nil, true
	}
	if i, ok := v.huntTarget(v.searchFile(), dir); ok {
		return m.openHunt(v, i, dir, huntHit, body)
	}
	// Nothing left to search: the wrap stands.
	v.stk.hunt = nil
	if i := stepHit(v.search.hits, pos, dir); i >= 0 {
		v.goToHit(i, body)
		v.syncStackTitle()
	}
	return m, nil, true
}

// openHunt unfolds file i and moves the cursor to its header — which is also
// what puts it inside the loader's window, since wantLoads only picks files
// near the reading position — then either lands at once (its rows are already
// here) or parks the hunt and pumps the queue.
func (m Model) openHunt(v *diffView, i, dir int, kind huntKind, body int) (Model, tea.Cmd, bool) {
	f := &v.stk.files[i]
	had := f.d != nil
	f.collapsed = false
	v.rebuild()
	v.setCursorLine(f.hdr, body)
	v.syncStackTitle()
	v.stk.land = nil
	v.stk.hunt = &stackHunt{file: i, dir: dir, kind: kind}
	if had {
		nm, cmd := m.drainStackHunt(i, body)
		return nm, cmd, true
	}
	nm, cmd := m.pumpStack()
	return nm, cmd, true
}

// drainStackHunt consumes a hunt parked on file idx once its lines are in the
// stream: it lands on that file's edge hit, or — when the file turned out to
// hold none — hands the step on to the next unsearched file, one round trip
// per file, never a bulk load (design D2).
func (m Model) drainStackHunt(idx, body int) (Model, tea.Cmd) {
	v := m.diffLayer()
	if v == nil || v.stk == nil || v.stk.hunt == nil || v.stk.hunt.file != idx {
		return m, nil
	}
	dir, kind := v.stk.hunt.dir, v.stk.hunt.kind
	v.stk.hunt = nil
	lo, hi := v.fileLineRange(idx)
	if kind == huntChange {
		if b, ok := v.blockIn(lo, hi, dir); ok {
			v.focusBlock(b, body)
			v.syncStackTitle()
			return m, nil
		}
	} else if i, ok := v.hitIn(lo, hi, dir); ok {
		v.goToHit(i, body)
		v.syncStackTitle()
		return m, nil
	}
	if next, ok := v.huntTarget(idx, dir); ok {
		nm, cmd, _ := m.openHunt(v, next, dir, kind, body)
		return nm, cmd
	}
	return m, nil // the stack ran out: the reader stays on this file's header
}

