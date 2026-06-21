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

// scanHighlightMatch finds the next matching commit from index `from` stepping by
// dir (+1 forward/newer→older down the feed, -1 backward), wrapping once over the
// whole loaded feed. inclusive lets `from` itself match (used by the type-time
// cursor snap); exclusive starts one step away (used by ctrl+↑/↓ nav). Returns
// (from, false) when there is no match or the query is empty. Cost is O(distance
// to the next match), never a full-feed scan unless there is no match.
func (m Model) scanHighlightMatch(from, dir int, inclusive bool) (int, bool) {
	n := len(m.commits)
	if n == 0 || m.highlightQuery == "" {
		return from, false
	}
	start := from
	if !inclusive {
		start = from + dir
	}
	for k := 0; k < n; k++ {
		i := ((start+dir*k)%n + n) % n
		if m.commitMatchesHighlight(i) {
			return i, true
		}
	}
	return from, false
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
