package tui

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
