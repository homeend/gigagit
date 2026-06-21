package tui

import "strings"

// highlightActive reports whether @-highlight search is engaged on the Commits
// panel — either mid-entry (typing) or with a committed query. Drives keybinding
// gating (ctrl+↑/↓ match nav, esc) and the label/footer display. Whether any row
// is actually dimmed depends additionally on highlightQuery != "".
func (m Model) highlightActive() bool {
	return m.highlightTyping || m.highlightQuery != ""
}

// commitMatchesHighlight reports whether commit i matches the current highlight
// query. An empty query matches nothing (so navigation no-ops and — combined
// with the dim gate — nothing is dimmed). Reuses the filter haystack.
func (m Model) commitMatchesHighlight(i int) bool {
	if m.highlightQuery == "" || i < 0 || i >= len(m.commits) {
		return false
	}
	return strings.Contains(
		strings.ToLower(m.commitHaystackAt(i)),
		strings.ToLower(m.highlightQuery),
	)
}

// scanHighlightMatch finds the next matching commit from DISPLAY position
// `fromDisplay` (the same space as m.sel[panelCommits]) stepping by dir (+1
// down the visible feed, -1 up), wrapping once. inclusive lets `fromDisplay`
// itself match (used by the type-time cursor snap); exclusive starts one step
// away (used by ctrl+↑/↓ nav). Returns a DISPLAY position, or (fromDisplay,
// false) when there is no match or the query is empty.
//
// It scans display positions and maps each through displayIndices to the backing
// commit before testing — the same display→backing mapping renderPanel/tooltip/
// backingIndex use — so navigation stays correct under a non-default sort (where
// display order ≠ feed order). Cost is one O(feed) displayIndices build plus an
// O(distance-to-match) walk, on a keypress (not per render row).
func (m Model) scanHighlightMatch(fromDisplay, dir int, inclusive bool) (int, bool) {
	if m.highlightQuery == "" {
		return fromDisplay, false
	}
	idx := m.displayIndices(panelCommits)
	n := len(idx)
	if n == 0 {
		return fromDisplay, false
	}
	start := fromDisplay
	if !inclusive {
		start = fromDisplay + dir
	}
	for k := 0; k < n; k++ {
		d := ((start+dir*k)%n + n) % n
		if m.commitMatchesHighlight(idx[d]) {
			return d, true
		}
	}
	return fromDisplay, false
}

// snapToHighlightMatch moves the Commits cursor to the nearest match at or after
// its current position (wrapping). No-op when there is no match (or empty query),
// leaving the cursor where it is.
func (m Model) snapToHighlightMatch() Model {
	if i, ok := m.scanHighlightMatch(m.sel[panelCommits], +1, true); ok {
		m.sel[panelCommits] = i
	}
	return m
}
