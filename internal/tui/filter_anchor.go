package tui

import "sort"

// The /-filter searches from the cursor, not from the top (the @-snap rule,
// generalized to a list whose rows come and go with the query): filterAnchor
// remembers the backing row under the cursor before a query edit, and
// snapFilterSel re-seats the cursor on the first row at or after that anchor in
// the edited display list — so a user mid-way down five loaded pages lands on
// the nearest match below, not back at the top.

// filterAnchor returns the backing index under panel p's cursor in its CURRENT
// display list (clamped), or -1 when the panel shows nothing. Capture it before
// mutating the query; feed it to snapFilterSel after.
func (m Model) filterAnchor(p panel) int {
	idx := m.displayIndices(p)
	if len(idx) == 0 {
		return -1
	}
	s := m.sel[p]
	if s < 0 {
		s = 0
	}
	if s >= len(idx) {
		s = len(idx) - 1
	}
	return idx[s]
}

// snapFilterSel places panel p's cursor on the first display row at or after
// anchor in display order, wrapping to the top when every row sits above it
// (scanHighlightMatch's wrap rule). A -1 anchor (nothing was shown) falls back
// to the top. Because a filtered display list is a stable-sorted subsequence of
// the full one (displayIndices scans in backing order, sortIndices is stable),
// "at or after" is well-defined across query edits: the sort key first, backing
// order on ties — atOrAfter below MUST mirror viewLess or the binary search
// derails. Under sortDefault this degrades to a plain backing-index compare.
func (m Model) snapFilterSel(p panel, anchor int) Model {
	if anchor < 0 {
		m.sel[p] = 0
		return m
	}
	idx := m.displayIndices(p)
	if len(idx) == 0 {
		m.sel[p] = 0
		return m
	}
	l := m.listFor(p)
	mode := m.sortModes[p]
	atOrAfter := func(bi int) bool {
		if mode != sortDefault {
			if viewLess(l, mode, bi, anchor) {
				return false
			}
			if viewLess(l, mode, anchor, bi) {
				return true
			}
		}
		return bi >= anchor
	}
	// atOrAfter is monotone along idx (false… then true…), so binary search.
	d := sort.Search(len(idx), func(i int) bool { return atOrAfter(idx[i]) })
	if d == len(idx) {
		d = 0
	}
	m.sel[p] = d
	return m
}
