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

// TestCommitFilterMemoWipContentChange pins the content-sensitive wip key: a
// status refresh can change a wip row's dirty-file count without changing
// the ROW count — "Working tree (2)" → "Working tree (3)" — and that count
// is filter-matchable text, so the memo must miss and rescan.
func TestCommitFilterMemoWipContentChange(t *testing.T) {
	m := filterMemoModel(50)
	m.wipRows = []wipRow{{wipWorktree, 2}}
	m.filterQuery = "working tree (2)"
	got := m.displayIndices(panelCommits)
	if len(got) != 1 || got[0] != 0 {
		t.Fatalf("setup: query should match exactly the wip row, got %v", got)
	}
	m.wipRows = []wipRow{{wipWorktree, 3}} // same row count, new content
	got = m.displayIndices(panelCommits)
	want := referenceFilter(m, m.filterQuery)
	if !idxEqual(got, want) {
		t.Fatalf("stale memo after wip content change: got %v want %v", got, want)
	}
	if len(got) != 0 {
		t.Fatalf("query for the old count must no longer match, got %v", got)
	}
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

// TestCommitFilterNarrowingScansMatchesOnly pins the typing fast path: with a
// warm memo, extending the query must rescan only the cached matches, not the
// whole feed. The fixture holds the match count FIXED (50 "needle" rows)
// while the feed doubles — the extension's alloc count must stay ~flat.
// Pre-Task-2 (full rescan per extension) it doubles with the feed.
func TestCommitFilterNarrowingScansMatchesOnly(t *testing.T) {
	measure := func(n int) float64 {
		m := filterMemoModel(n)
		// Warm the ident-width cache the way production always has it warm by
		// the time a filter is active (rebuildCommitGraph sets it after the
		// feed loads). Without this, listFor's identW field recomputes via an
		// O(n) lipgloss scan on every miss, swamping the O(matches)/O(tail)
		// signal this test measures — a fixture gap, not a production cost.
		m.identWValid = true
		for i := 0; i < 50; i++ { // fixed k=50 matches regardless of n
			m.commits[i*(n/50)].Subject = fmt.Sprintf("needle row %d", i)
		}
		m.filterQuery = "needle"
		_ = m.displayIndices(panelCommits) // warm: one full scan
		saved := *m.filterMemo             // the warm "needle" state
		m.filterQuery = "needle row"       // one typed extension
		return testing.AllocsPerRun(5, func() {
			*m.filterMemo = saved // re-arm the narrowing precondition each run
			_ = m.displayIndices(panelCommits)
		})
	}
	a, b := measure(20_000), measure(40_000)
	if b > a*1.5+10 {
		t.Fatalf("query-extension allocs grew %.1fx when the feed doubled (a=%.0f, b=%.0f): narrowing is rescanning the feed", b/a, a, b)
	}
}

// TestCommitFilterAppendScansTailOnly pins the paging fast path: growing the
// feed under a warm memo (the commitsPagedMsg strict-append shape) must
// rescan only the appended tail. Also cross-checks the result against the
// oracle, since this path splices cached matches with tail matches.
func TestCommitFilterAppendScansTailOnly(t *testing.T) {
	const n, tail = 40_000, 100
	m := filterMemoModel(n + tail)
	// See TestCommitFilterNarrowingScansMatchesOnly: warm the ident-width
	// cache to match production's steady state, else listFor's O(n) identW
	// recompute (unused by Haystack-based matching) swamps the tail-scan cost
	// this test measures.
	m.identWValid = true
	full := m.commits
	m.commits = full[:n] // the pre-append feed
	m.filterQuery = "alpha"
	_ = m.displayIndices(panelCommits) // warm at feedLen=n
	saved := *m.filterMemo
	m.commits = full // paging appended `tail` older commits
	allocs := testing.AllocsPerRun(5, func() {
		*m.filterMemo = saved // re-arm the append precondition each run
		_ = m.displayIndices(panelCommits)
	})
	// Tail scan ≈ tail rows × ~5 allocs + one result-copy alloc. A full
	// rescan is ≥ n allocs (40k+) — orders of magnitude over this bound.
	if allocs > float64(tail)*10+200 {
		t.Fatalf("append rescan allocated %.0f (bound %d): paging is rescanning the whole feed, not just the %d-row tail", allocs, tail*10+200, tail)
	}
	got := m.displayIndices(panelCommits)
	want := referenceFilter(m, "alpha")
	if !idxEqual(got, want) {
		t.Fatalf("append-extension result diverges from oracle: got %d rows, want %d", len(got), len(want))
	}
}

// TestCommitFilterAppendMergesUnderSort pins the append path's re-sort: under
// a non-default sort the spliced cached+tail indices MUST be re-sorted (the
// sortIndices call in the append branch — a no-op under sortDefault, which is
// all the other append tests run). The fixture's subjects sort
// lexicographically, so a tail match like "subject alpha 300" lands BETWEEN
// "subject alpha 30" and "subject alpha 31" — the middle of the existing
// name-asc matches — meaning an un-sorted concatenation (tail matches left at
// the end) would diverge from the oracle, which sorts everything.
func TestCommitFilterAppendMergesUnderSort(t *testing.T) {
	const n, tail = 300, 30
	m := filterMemoModel(n + tail)
	m.sortModes[panelCommits] = sortNameAsc
	full := m.commits
	m.commits = full[:n] // the pre-append feed
	m.filterQuery = "alpha"
	warm := m.displayIndices(panelCommits) // warm the memo under name-asc
	if len(warm) == 0 {
		t.Fatal("setup: warm query must match some pre-append rows")
	}
	m.commits = full // paging appended rows, ~1/3 of which match "alpha"
	got := m.displayIndices(panelCommits)
	want := referenceFilter(m, "alpha")
	if len(got) <= len(warm) {
		t.Fatalf("setup: appended tail must add matches (warm %d, got %d)", len(warm), len(got))
	}
	if !idxEqual(got, want) {
		t.Fatalf("append under sortNameAsc diverges from oracle: the merged tail was not re-sorted\ngot  %v\nwant %v", got, want)
	}
}

// TestCommitFilterAppendNoTailMatches pins the append path's empty-tail case:
// when no appended row matches, the result must equal the pre-append match
// set (and the oracle) — the splice degenerates to a copy of the cached idx.
func TestCommitFilterAppendNoTailMatches(t *testing.T) {
	const n, tail = 300, 30
	m := filterMemoModel(n + tail)
	full := m.commits
	for i := n; i < n+tail; i++ {
		full[i].Subject = fmt.Sprintf("subject zzz %d", i) // tail never matches "alpha"
	}
	m.commits = full[:n]
	m.filterQuery = "alpha"
	warm := m.displayIndices(panelCommits)
	m.commits = full
	got := m.displayIndices(panelCommits)
	if !idxEqual(got, warm) {
		t.Fatalf("append with no tail matches changed the result: got %d rows, warm had %d", len(got), len(warm))
	}
	if want := referenceFilter(m, "alpha"); !idxEqual(got, want) {
		t.Fatalf("append with no tail matches diverges from oracle: got %v want %v", got, want)
	}
}

// TestCommitFilterNarrowingFromEmpty pins narrowing over an already-empty
// cached idx: a warm no-match memo extended by more typing must take the
// narrowing branch over an empty (non-nil) base and return empty, not error.
func TestCommitFilterNarrowingFromEmpty(t *testing.T) {
	m := filterMemoModel(300)
	m.filterQuery = "zzz-no-match"
	if got := m.displayIndices(panelCommits); len(got) != 0 {
		t.Fatalf("setup: warm query must match nothing, got %v", got)
	}
	m.filterQuery = "zzz-no-match-extended" // narrowing from an empty match set
	got := m.displayIndices(panelCommits)
	if len(got) != 0 {
		t.Fatalf("narrowing from an empty cached idx must stay empty, got %v", got)
	}
	if want := referenceFilter(m, m.filterQuery); !idxEqual(got, want) {
		t.Fatalf("narrowing from empty diverges from oracle: got %v want %v", got, want)
	}
}
