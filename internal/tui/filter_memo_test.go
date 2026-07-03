package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// filterMemoModel builds a Commits-panel model with n commits whose subjects
// cycle alpha/beta/gamma (so substring queries partition the feed), a live
// filter memo, and no wip rows. The filter is bound to the Commits panel but
// starts empty.
func filterMemoModel(n int) Model {
	m := Model{
		sel:        map[panel]int{},
		sortModes:  map[panel]sortMode{},
		dispModes:  map[panel]dispMode{},
		hscroll:    map[panel]int{},
		focus:      panelCommits,
		width:      120,
		height:     40,
		filterMemo: &commitFilterMemo{},
	}
	m.commits = make([]model.Commit, n)
	kinds := []string{"alpha", "beta", "gamma"}
	for i := range m.commits {
		m.commits[i] = model.Commit{
			Hash:    fmt.Sprintf("%040x", i),
			Subject: fmt.Sprintf("subject %s %d", kinds[i%3], i),
			Source:  "main",
		}
	}
	m.filterPanel = panelCommits
	return m
}

// referenceFilter is the brute-force filter displayIndices ran before the
// memo existed: scan every row's haystack, then sort. The memoized path must
// equal it after ANY sequence of model mutations. Kept independent of the
// production matcher on purpose.
func referenceFilter(m Model, query string) []int {
	l := m.listFor(panelCommits)
	h := l.(haystacker)
	q := strings.ToLower(query)
	idx := []int{}
	for i := 0; i < l.Len(); i++ {
		if strings.Contains(strings.ToLower(h.Haystack(i)), q) {
			idx = append(idx, i)
		}
	}
	sortIndices(l, m.sortModes[panelCommits], idx)
	return idx
}

func idxEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestCommitFilterMemoMatchesReference walks the memo through every mutation
// class — repeat, extend, shrink, feed append, same-length replacement (via
// graphLayerReset, the invariant), wip-prefix changes, sort changes — and
// checks the memoized result against the brute-force oracle at each step.
func TestCommitFilterMemoMatchesReference(t *testing.T) {
	m := filterMemoModel(300)
	check := func(step string) {
		t.Helper()
		got := m.displayIndices(panelCommits)
		want := referenceFilter(m, m.filterQuery)
		if !idxEqual(got, want) {
			t.Fatalf("%s: displayIndices returned %d rows, oracle %d (query %q)\ngot  %v\nwant %v",
				step, len(got), len(want), m.filterQuery, got, want)
		}
	}
	m.filterQuery = "alpha"
	check("first query (cold)")
	check("repeat (memo hit)")
	m.filterQuery = "alpha 1"
	check("extend query (narrowing)")
	m.filterQuery = "alpha"
	check("shrink query (backspace, full rescan)")
	m.commits = append(m.commits, model.Commit{
		Hash: strings.Repeat("f", 40), Subject: "subject alpha appended", Source: "main"})
	check("feed append (paging)")
	// A same-length in-place replacement is undetectable from the memo key;
	// the contract is that every replacement site calls graphLayerReset,
	// which must invalidate the memo.
	m.commits[len(m.commits)-1].Subject = "subject beta replaced"
	m = m.graphLayerReset()
	check("same-length replace after graphLayerReset")
	m.wipRows = []wipRow{{wipWorktree, 2}}
	check("wip prefix appears (all indices shift)")
	m.wipRows = nil
	check("wip prefix clears")
	m.sortModes[panelCommits] = sortNameAsc
	check("sort change")
	m.sortModes[panelCommits] = sortDefault
	m.filterQuery = "zzz-no-match"
	check("no matches")
	m.filterQuery = "ALPHA"
	check("case-insensitive")
}

// TestCommitFilterRepeatCallIsO1 pins the fix for the user-visible bug: with
// an unchanged model, a repeated filtered displayIndices call must be a memo
// hit that does not rescan the feed. Pre-memo, every call allocated O(feed)
// (haystack build + ToLower per row) — ~15 such calls per keypress made
// filter navigation take seconds at 600k commits.
func TestCommitFilterRepeatCallIsO1(t *testing.T) {
	m := filterMemoModel(20_000)
	m.filterQuery = "alpha"
	_ = m.displayIndices(panelCommits) // warm the memo
	allocs := testing.AllocsPerRun(5, func() { _ = m.displayIndices(panelCommits) })
	if allocs > 10 {
		t.Fatalf("repeated filtered displayIndices allocated %.0f/call (want ~0): memo hit must not rescan the feed", allocs)
	}
}
