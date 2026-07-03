# Commit-Filter Index Cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `/`-filter navigation on the Commits panel O(1) per keypress at any feed size by memoizing the filtered display-index slice (today: ~15 full 600k-commit rescans ≈ 5.6 s per arrow key).

**Architecture:** A `commitFilterMemo` pointer field on `Model` (shared across value copies, single-goroutine — the `statusList.mtime` discipline) caches the filtered index keyed by `(query, wipCount, feedLen, baseHash, sortMode)`. `displayIndices(panelCommits)` returns it in O(1) on a hit. On a miss, two incremental rebuilds — narrowing (query extension rescans only cached matches) and append extension (feed paging rescans only the tail) — fall back to the full scan. Same-tip same-length replacements are undetectable from the key, so `graphLayerReset()` (already called at every replacement site, the established graph-layer invariant) also invalidates the memo.

**Tech Stack:** Go 1.26, Bubble Tea TUI, `testing.AllocsPerRun` guards.

**Spec:** `docs/superpowers/specs/2026-07-03-commit-filter-index-cache-design.md`

## Global Constraints

- Work in the worktree `/mnt/t/others/gigagit/.claude/worktrees/commit-filter-cache` on branch `fix/commit-filter-index-cache`. Every file path below is relative to that worktree root; when using Write/Edit tools, prefix paths with the worktree root, NEVER the main checkout.
- `Model` is a value receiver. The memo is mutated only through its pointer; the returned `idx` slice is shared and read-only (the `m.commitsIdx` contract) — never sort or mutate a cached slice in place after storing it.
- Commits-panel only. Other panels' filter behavior is unchanged.
- Falling back to a full scan is always semantically correct; the incremental paths are pure optimizations. Any doubt in a fast-path condition ⇒ full scan.
- Run tests with `go test ./internal/tui -count=1` from the worktree root; the full `./test.sh` runs once at the end (Task 3).

---

### Task 1: The memo — O(1) repeated filtered `displayIndices`

**Files:**
- Create: `internal/tui/filter_memo.go`
- Create: `internal/tui/filter_memo_test.go`
- Modify: `internal/tui/viewstate.go` (displayIndices, after the fast-path block ~line 520)
- Modify: `internal/tui/model.go` (Model struct ~line 124, `New` ~line 223, `graphLayerReset` ~line 2520, `reRoot` ~line 2712)

**Interfaces:**
- Consumes: `m.listFor(panelCommits) panelList`, `haystacker` (viewstate.go:198), `sortIndices(l, mode, idx)`, `m.wipCount()`, `m.filterQuery`, `m.sortModes`.
- Produces: `type commitFilterMemo struct`, `(*commitFilterMemo).invalidate()`, `Model.commitFilterIndices() []int`, `Model.commitFilterScan(l panelList, q string, base []int) []int`, `filterMatchFn(l panelList, q string) func(int) bool`, `Model.filterMemo *commitFilterMemo` field. Task 2 extends `commitFilterIndices` and adds `commitFilterScanRange`.

- [ ] **Step 1: Write the failing O(1)-repeat test and the (currently passing) oracle test**

Create `internal/tui/filter_memo_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify the repeat test fails**

Run: `go test ./internal/tui -run 'TestCommitFilter(MemoMatchesReference|RepeatCallIsO1)' -count=1 -v`
Expected: compile error — `commitFilterMemo` undefined. Add the minimal type first if you prefer a clean red: after creating `filter_memo.go` from Step 3 WITHOUT the `displayIndices` wiring (Step 4), `TestCommitFilterRepeatCallIsO1` FAILS with allocs ≈ 60000+ (O(feed) rescan), and `TestCommitFilterMemoMatchesReference` PASSES (unmemoized path is correct). That is the red state to record.

- [ ] **Step 3: Implement the memo**

Create `internal/tui/filter_memo.go`:

```go
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
```

- [ ] **Step 4: Wire it into displayIndices and Model**

In `internal/tui/viewstate.go`, immediately after the closing brace of the unfiltered fast-path block (the `if m.sortModes[p] == sortDefault && !m.filterActive(p) { ... }` block ending ~line 520) and BEFORE `l := m.listFor(p)`:

```go
	// Commits + active filter: the memoized path (see commitFilterMemo). The
	// unfiltered commitsIdx fast path above cannot serve it, and the generic
	// scan below is O(feed) per call — fired many times per keypress, that
	// measured ~5.6s per arrow key at 600k commits.
	if p == panelCommits && m.filterActive(p) {
		return m.commitFilterIndices()
	}
```

In `internal/tui/model.go`:

1. Model struct, next to the `commitsIdx` field (~line 124):

```go
	filterMemo          *commitFilterMemo               // memoized filtered Commits index (see filter_memo.go); nil in zero-value test Models = unmemoized
```

2. `New` (~line 224), add to the literal:

```go
		filterMemo:             &commitFilterMemo{},
```

3. `graphLayerReset` (~line 2520) — the memo shares the graph layer's undetectable same-length-replacement hole, so it invalidates here too:

```go
func (m Model) graphLayerReset() Model {
	m.graphLayer = nil
	m.filterMemo.invalidate() // same undetectable same-length-replacement hole as the graph layer
	return m
}
```

4. `reRoot` (~line 2712), next to `m.commitCompareSet = nil`:

```go
	m.filterMemo = &commitFilterMemo{} // fresh pointer: an in-flight copy from the old repo must not repopulate the new repo's memo
```

- [ ] **Step 5: Run the new tests — both pass**

Run: `go test ./internal/tui -run 'TestCommitFilter(MemoMatchesReference|RepeatCallIsO1)' -count=1 -v`
Expected: both PASS (repeat-call allocs ~0).

- [ ] **Step 6: Run the whole TUI package (regression check)**

Run: `go vet ./internal/tui && gofmt -l internal/tui && go test ./internal/tui -count=1`
Expected: vet clean, no gofmt output, all ~1542 tests pass — in particular `TestFilteredDisplayIndicesSkipsRowStyling` (haystack-not-Row rule) and the `filter_test.go` / `filter_motion_test.go` suites must stay green.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/filter_memo.go internal/tui/filter_memo_test.go internal/tui/viewstate.go internal/tui/model.go
git commit -m "perf(tui): memoize the filtered Commits display index

With a / filter active, displayIndices(panelCommits) lost its O(1) fast
path and rescanned the whole feed (haystack + ToLower per row) on every
call — ~15 calls per keypress made filter navigation take ~5.6s per
arrow key at 600k commits. A shared commitFilterMemo keyed by
(query, wipCount, feedLen, baseHash, sort) makes repeats O(1);
graphLayerReset also invalidates it (same undetectable same-length
replacement hole as the graph layer).

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_018GqPCZmEEMxLe8sJx6eq5c"
```

---

### Task 2: Incremental rebuilds — narrowing while typing, tail-scan on paging

**Files:**
- Modify: `internal/tui/filter_memo.go` (extend `commitFilterIndices`, add `commitFilterScanRange`)
- Modify: `internal/tui/filter_memo_test.go` (two new tests)

**Interfaces:**
- Consumes: everything Task 1 produced (`commitFilterMemo` fields, `commitFilterScan(l, q, base)`, `filterMatchFn`).
- Produces: `Model.commitFilterScanRange(l panelList, q string, from, to int) []int`; `commitFilterIndices` gains the narrowing and append-extension cases. No signature changes.

- [ ] **Step 1: Write the two failing tests**

Append to `internal/tui/filter_memo_test.go`:

```go
// TestCommitFilterNarrowingScansMatchesOnly pins the typing fast path: with a
// warm memo, extending the query must rescan only the cached matches, not the
// whole feed. The fixture holds the match count FIXED (50 "needle" rows)
// while the feed doubles — the extension's alloc count must stay ~flat.
// Pre-Task-2 (full rescan per extension) it doubles with the feed.
func TestCommitFilterNarrowingScansMatchesOnly(t *testing.T) {
	measure := func(n int) float64 {
		m := filterMemoModel(n)
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
```

- [ ] **Step 2: Run them to verify both fail**

Run: `go test ./internal/tui -run 'TestCommitFilter(NarrowingScansMatchesOnly|AppendScansTailOnly)' -count=1 -v`
Expected: both FAIL — narrowing allocs grow ~2.0x with the feed; append rescan allocates ~200k (full 40k scan) against the ~1200 bound.

- [ ] **Step 3: Implement the incremental paths**

In `internal/tui/filter_memo.go`, add below `commitFilterScan`:

```go
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
```

Replace `commitFilterIndices`' miss path (the `l := ...; idx := ...; sortIndices(...)` lines) with:

```go
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
```

Note: after the Task-1 review fix, the memo keys on `wipRows []wipRow` (content), not a count — the hit-check and store in the CURRENT `filter_memo.go` already use `slices.Equal(c.wipRows, m.wipRows)` and the copy-on-store above. Keep them exactly as they are; this task only replaces the miss path between the hit-check and the store.

- [ ] **Step 4: Run the new tests — both pass; then the full package**

Run: `go test ./internal/tui -run 'TestCommitFilter' -count=1 -v`
Expected: all four TestCommitFilter* PASS (the Task-1 oracle test re-validates the incremental paths against brute force).

Run: `go vet ./internal/tui && gofmt -l internal/tui && go test ./internal/tui -count=1`
Expected: clean, all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/filter_memo.go internal/tui/filter_memo_test.go
git commit -m "perf(tui): incremental filter rebuilds — narrow on typing, tail-scan on paging

A query extension rescans only the cached matches (matches(q+s) ⊆
matches(q)); a feed page-in rescans only the appended tail (the same
strict-append invariant the graph layer keys on). First character and
backspace still pay one full scan; everything else is O(matches).

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_018GqPCZmEEMxLe8sJx6eq5c"
```

---

### Task 3: Full-suite verification + docs

**Files:**
- Modify: `CHANGELOG.md` (new entry at top)
- Modify: `CLAUDE.md` (one sentence in the `tui` package-map row)

**Interfaces:**
- Consumes: the finished implementation from Tasks 1–2.
- Produces: release notes; no code.

- [ ] **Step 1: Run the staged suite**

Run from the worktree root: `./test.sh`
Expected: vet+gofmt stage clean, unit tests pass, e2e passes.

- [ ] **Step 2: Run the race suite**

Run: `./test.sh race`
Expected: pass (the memo is single-goroutine by design; this guards that no test drives Update/View concurrently against the shared pointer).

- [ ] **Step 3: CHANGELOG entry**

Add at the top of `CHANGELOG.md`, matching the existing entry style (check the current top entry's heading format and mirror it):

```markdown
- perf(tui): `/`-filter navigation on the Commits panel is now O(1) per
  keypress at any feed size. The filtered display index is memoized
  (`commitFilterMemo`) and rebuilt incrementally — typing narrows the cached
  matches, paging scans only the appended tail. Previously every keypress
  rescanned the whole feed ~15×: ~5.6s per arrow key at 600k commits (linux
  repo), now sub-millisecond.
```

- [ ] **Step 4: CLAUDE.md package-map touch-up**

In the `tui` row of the package map, after the sentence describing `commitsIdx`/`commit_ident.go` caches (search for "commitsIdx"), add:

```
`filter_memo.go` adds `commitFilterMemo` — the memoized filtered Commits display index (keyed by query/wip/feedLen/baseHash/sort, invalidated by `graphLayerReset`, incrementally narrowed while typing and tail-extended on paging) so `/`-filter navigation stays O(1) per keypress on huge feeds.
```

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md CLAUDE.md
git commit -m "docs: changelog + package map for the commit-filter index cache

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_018GqPCZmEEMxLe8sJx6eq5c"
```
