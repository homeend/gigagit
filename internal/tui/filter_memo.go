package tui

import "strings"

// commitFilterMemo caches the filtered Commits display-index slice between
// displayIndices calls. The pointer is shared across Model value copies —
// safe because Bubble Tea runs Update and View on one goroutine (the same
// discipline as statusList's shared mtime map). Without it, every
// displayIndices call under an active filter rescans the whole feed
// (haystack build + ToLower per row), and the call fires many times per
// keypress: at 600k commits that measured ~15 scans ≈ 5.6s per arrow key.
type commitFilterMemo struct {
	query    string   // lowercased query idx was built for; "" = invalid
	wip      int      // wipCount() when built
	feedLen  int      // len(m.commits) when built
	baseHash string   // m.commits[0].Hash when built ("" for an empty feed)
	sort     sortMode // Commits sort mode when built
	idx      []int    // cached display→backing indices (shared, read-only)
}

// invalidate marks the memo stale through the shared pointer so every Model
// copy holding it sees the reset. nil-safe: zero-value Models (tests,
// benchModel) have no memo and simply run unmemoized.
func (c *commitFilterMemo) invalidate() {
	if c != nil {
		c.query = ""
		c.idx = nil
	}
}

// filterMatchFn returns the per-row filter predicate for l under the
// lowercased query q, preferring the cheap haystack over the styled Row —
// displayIndices' long-standing rule (Row on the Commits panel is full
// graph+identity styling; calling it per row re-adds O(n) styling per scan).
func filterMatchFn(l panelList, q string) func(int) bool {
	if h, ok := l.(haystacker); ok {
		return func(i int) bool { return strings.Contains(strings.ToLower(h.Haystack(i)), q) }
	}
	return func(i int) bool { return strings.Contains(strings.ToLower(l.Row(i)), q) }
}

// commitFilterScan collects the indices of rows matching q (already
// lowercased). base nil scans the whole list; non-nil scans only those
// indices — the narrowing path (an empty non-nil base yields empty). The
// result is in scan order: backing order for a full scan, base order for a
// narrowing scan.
func (m Model) commitFilterScan(l panelList, q string, base []int) []int {
	match := filterMatchFn(l, q)
	if base != nil {
		out := make([]int, 0, len(base))
		for _, i := range base {
			if match(i) {
				out = append(out, i)
			}
		}
		return out
	}
	out := make([]int, 0, 64)
	for i := 0; i < l.Len(); i++ {
		if match(i) {
			out = append(out, i)
		}
	}
	return out
}

// commitFilterIndices is displayIndices' Commits+active-filter path: the
// filtered display-index slice, memoized. A hit is O(1). A miss runs the full
// scan and stores the result back through the shared memo pointer. The
// returned slice is shared and read-only (the m.commitsIdx contract).
func (m Model) commitFilterIndices() []int {
	q := strings.ToLower(m.filterQuery)
	wip := m.wipCount()
	feedLen := len(m.commits)
	baseHash := ""
	if feedLen > 0 {
		baseHash = m.commits[0].Hash
	}
	srt := m.sortModes[panelCommits]
	c := m.filterMemo
	if c != nil && c.query != "" && c.query == q && c.wip == wip &&
		c.feedLen == feedLen && c.baseHash == baseHash && c.sort == srt {
		return c.idx // hit: O(1), no scan
	}
	l := m.listFor(panelCommits)
	idx := m.commitFilterScan(l, q, nil)
	sortIndices(l, srt, idx)
	if c != nil {
		c.query, c.wip, c.feedLen, c.baseHash, c.sort, c.idx = q, wip, feedLen, baseHash, srt, idx
	}
	return idx
}
