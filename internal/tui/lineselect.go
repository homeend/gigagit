package tui

// lineSel is a tmux-style whole-line selection over a host's LOGICAL line
// indexes — the diff's index into v.lines, blame's into b.lines, the preview's
// into p.lines. Never a DISPLAY row: a wrapped line owns several of those, and
// every host already maps one to the other for its cursor.
//
// The zero value is "no selection". The key sequence is space / space / enter:
// the first space starts a range that follows the cursor, the second freezes
// its end (the cursor is then free to move — the user's ruling), a third starts
// a new range. enter copies bounds() and clears; esc clears and does nothing
// else.
type lineSel struct {
	on     bool // a selection exists (anchor is valid)
	anchor int  // the line the first space marked
	end    int  // the line the second space marked; meaningful when fixed
	fixed  bool // the second space landed: the range no longer follows the cursor
}

// start begins a fresh range at cur (the first space, and the third).
func (s *lineSel) start(cur int) {
	s.on, s.anchor, s.end, s.fixed = true, cur, cur, false
}

// mark freezes the range's far end at cur (the second space). Inert with no
// selection on: freezing a range that does not exist would invent one.
func (s *lineSel) mark(cur int) {
	if !s.on {
		return
	}
	s.end, s.fixed = cur, true
}

// press is the space key itself, identical in all three hosts: start, then
// freeze, then start again.
func (s *lineSel) press(cur int) {
	if s.on && !s.fixed {
		s.mark(cur)
		return
	}
	s.start(cur)
}

// clear drops the selection (esc, a copy, a rebuild of the line stream).
func (s *lineSel) clear() { *s = lineSel{} }

// bounds is the inclusive line range the selection covers right now: while the
// range is loose it is anchor..cur in either order, once frozen it is
// anchor..end. ok is false when there is no selection.
func (s lineSel) bounds(cur int) (lo, hi int, ok bool) {
	if !s.on {
		return 0, 0, false
	}
	other := cur
	if s.fixed {
		other = s.end
	}
	lo, hi = s.anchor, other
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi, true
}

// contains reports whether line i is inside the range measured from cursor cur.
func (s lineSel) contains(i, cur int) bool {
	lo, hi, ok := s.bounds(cur)
	return ok && i >= lo && i <= hi
}
