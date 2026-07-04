package tui

import (
	"slices"
	"strings"
)

// commitFilterMemo caches the filtered Commits display-index slice between
// displayIndices calls. The pointer is shared across Model value copies —
// safe because Bubble Tea runs Update and View on one goroutine (the same
// discipline as statusList's shared mtime map). Without it, every
// displayIndices call under an active filter rescans the whole feed
// (haystack build + ToLower per row), and the call fires many times per
// keypress: at 600k commits that measured ~15 scans ≈ 5.6s per arrow key.
type commitFilterMemo struct {
	query string // lowercased query idx was built for; "" = invalid
	// wipRows is a copy of m.wipRows when built. Part of the key by CONTENT,
	// not count — a wip row's dirty-file count is filter-matchable text
	// (wipRow.text(), e.g. "Working tree (2)"), and it can change (a status
	// refresh) without the row COUNT changing (still one Working-tree row).
	// Keying on len(wipRows) alone would miss that and serve a stale
	// filtered result.
	wipRows  []wipRow
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
		c.wipRows = nil
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

// commitFilterScanRange is commitFilterScan over the half-open unified-index
// range [from, to) — the appended tail after a feed page-in.
func (m Model) commitFilterScanRange(l panelList, q string, from, to int) []int {
	match := filterMatchFn(l, q)
	out := make([]int, 0, 16)
	for i := from; i < to; i++ {
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
	feedLen := len(m.commits)
	baseHash := ""
	if feedLen > 0 {
		baseHash = m.commits[0].Hash
	}
	srt := m.sortModes[panelCommits]
	c := m.filterMemo
	if c != nil && c.query != "" && c.query == q && slices.Equal(c.wipRows, m.wipRows) &&
		c.feedLen == feedLen && c.baseHash == baseHash && c.sort == srt {
		return c.idx // hit: O(1), no scan
	}
	l := m.listFor(panelCommits)
	// sameShape: the memo describes this feed (same wip prefix BY CONTENT —
	// see the Task-1 review fix: a wip row's count is filter-matchable text —
	// same tip, same sort) with a live query: the precondition for both
	// incremental rebuilds. Both are pure optimizations: any doubt falls to
	// a full scan, which is always correct.
	wip := m.wipCount() // unified-index offset of the first real commit
	sameShape := c != nil && c.query != "" && slices.Equal(c.wipRows, m.wipRows) &&
		c.baseHash == baseHash && c.sort == srt
	var idx []int
	switch {
	case sameShape && c.feedLen == feedLen && strings.HasPrefix(q, c.query):
		// Narrowing (typing): the old query is a prefix of the new one, so
		// Contains(h, q) ⇒ Contains(h, c.query) — the new match set is a
		// subset of the cached one. Rescan only the cached matches:
		// O(matches) per typed character instead of O(feed). Order is
		// preserved, so the cached sort survives without re-sorting.
		idx = m.commitFilterScan(l, q, c.idx)
	case sameShape && c.query == q && feedLen > c.feedLen:
		// Append extension (paging): the feed grew under the same tip — the
		// strict newest→oldest append invariant rebuildCommitGraph's own
		// fast path keys on. Existing indices are stable; scan only the
		// appended tail and merge.
		tailIdx := m.commitFilterScanRange(l, q, wip+c.feedLen, wip+feedLen)
		idx = append(append(make([]int, 0, len(c.idx)+len(tailIdx)), c.idx...), tailIdx...)
		sortIndices(l, srt, idx) // no-op under sortDefault; merges the tail otherwise
	default:
		idx = m.commitFilterScan(l, q, nil)
		sortIndices(l, srt, idx)
	}
	if c != nil {
		c.query, c.feedLen, c.baseHash, c.sort, c.idx = q, feedLen, baseHash, srt, idx
		c.wipRows = append([]wipRow(nil), m.wipRows...)
	}
	return idx
}
