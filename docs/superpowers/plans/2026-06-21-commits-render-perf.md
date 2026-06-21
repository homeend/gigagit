# Commits Panel Render Performance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Commits-panel navigation feel instant on a large feed by removing the O(feed²) per-frame render cost, and stop the commit files-view issuing a read per held keypress.

**Architecture:** Three layers, in order. (1) Kill the O(n²) term — `commitDecorators` calls the loop-invariant `commitIdentWidth()` once per row; hoist it. (2) Stop eagerly materializing the per-commit tooltip/filter slices every frame — make them per-index lazy. (3) ONLY IF a post-(1)/(2) benchmark says it's still over budget, window-then-style so expensive styling runs for ~visible rows. Part B (files-view read settle) is independent. Each task keeps rendered output byte-for-byte identical except Part B, which changes only *when* a read fires.

**Tech Stack:** Go 1.26, Bubble Tea TUI, lipgloss. Tests: `go test`. Package `internal/tui`.

## Global Constraints

- Scope is `internal/tui` only. No engine/domain/git/gitcmd/gitexec/CLI/agentskill changes.
- Tasks 2–4 must produce **byte-for-byte identical rendered output** for any given visible state (rows, `⋯` window markers, lane-colored `●`, dimmed lineage rows, identity-column width, selection prefix). The only behavior change in the whole plan is Part B's read timing.
- `panelView(panelCommits)` remains the single source of truth consumed by selection, paging, clamping, tooltips (`tooltip.go`), mouse hit-testing (`mouse.go`), and action keys; its `(rows, idx)` contract and `idx` semantics must not change.
- The narrow-terminal commits path (view.go:322) and the wide path (view.go:328) must both stay correct.
- TDD: failing test first, minimal change, run, commit. Run `./test.sh unit` (the tui package) before each commit; `./test.sh race` before finishing the branch.
- Benchmark machine numbers are indicative, not asserted literally — assert *scaling shape* (ratios), never absolute nanoseconds.

---

## File Structure

- `internal/tui/commits_render_bench_test.go` — **new.** The regression benchmark + an allocation-scaling test that pins the O(n²)→O(n) fix. Owns all perf assertions.
- `internal/tui/view.go` — modify `commitDecorators` (Task 2 hoist; Task 4 windowing), add per-index commit builders (Task 3).
- `internal/tui/viewstate.go` — `commitList` struct + `listFor` (Task 3 lazy per-index; Task 4 cheap-index helper).
- `internal/tui/model.go` — `Model` field + `commitFilesMsg` handler + `l` open handler (Task 5).
- `internal/tui/files_view.go` — `moveListUnderFilesView` / `syncFilesViewToSelectedCommit` (Task 5).

---

## Task 1: Regression benchmark + baseline

**Files:**
- Create: `internal/tui/commits_render_bench_test.go`

**Interfaces:**
- Consumes: `cellsWithNode(lanes, nodeLane int) string` (exists in `commit_graph_window_test.go`), `Model.panelView`, `Model.commitDecorators`.
- Produces: `benchModel(n, lanes, cols int) Model` and `BenchmarkCommitsRender` — reused by Tasks 2 and 4.

- [ ] **Step 1: Write the benchmark file**

```go
package tui

import (
	"fmt"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

// benchModel builds a Commits-panel Model with n commits, each carrying a
// `lanes`-wide cached graph row, focus on the Commits panel, default sort, no
// filter — i.e. the natural-order graph render path.
func benchModel(n, lanes, cols int) Model {
	m := Model{
		sel:       map[panel]int{},
		sortModes: map[panel]sortMode{},
		dispModes: map[panel]dispMode{},
		hscroll:   map[panel]int{},
		focus:     panelCommits,
		width:     120,
		height:    40,
	}
	m.commits = make([]model.Commit, n)
	m.commitGraphRows = make([]string, n)
	m.commitGraphLanes = make([]int, n)
	cells := cellsWithNode(lanes, lanes/2)
	for i := range m.commits {
		m.commits[i] = model.Commit{
			Hash:    fmt.Sprintf("%040x", i),
			Subject: "some commit subject line here",
			Source:  "main",
		}
		m.commitGraphRows[i] = cells
		m.commitGraphLanes[i] = lanes / 2
	}
	m.commitGraphCols = cols
	return m
}

// BenchmarkCommitsRender measures the per-frame Commits render hot path
// (filter+sort+materialize via panelView, then decorators) across feed sizes
// and graph widths.
func BenchmarkCommitsRender(b *testing.B) {
	for _, n := range []int{1000, 5000} {
		for _, cols := range []int{8, 80} {
			m := benchModel(n, 303, cols)
			b.Run(fmt.Sprintf("n=%d/cols=%d", n, cols), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					rows, idx := m.panelView(panelCommits)
					_ = m.commitDecorators(rows, idx)
				}
			})
		}
	}
}
```

- [ ] **Step 2: Run it and record the baseline**

Run: `go test ./internal/tui/ -run=XXX -bench=BenchmarkCommitsRender -benchtime=10x`
Expected: it completes and prints lines; record the `allocs/op` for `n=1000` and `n=5000`. Baseline observed on dev hardware: n=1000 ≈ 1.0M allocs, n=5000 ≈ 25M allocs (allocs scale ≈ n²). The point of this step is the recorded numbers, not a pass/fail.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/commits_render_bench_test.go
git commit -m "test(tui): regression benchmark for Commits render hot path"
```

---

## Task 2: Hoist `commitIdentWidth` out of the decorator loop (the O(n²) cliff)

**Files:**
- Modify: `internal/tui/view.go` — `commitDecorators` (loop at ~866–916)
- Test: `internal/tui/commits_render_bench_test.go` (add a scaling test)

**Interfaces:**
- Consumes: `benchModel` (Task 1), `Model.commitIdentWidth() int`, `Model.commitDecorators(rows []string, idx []int) []rowDecorator`.
- Produces: no signature change. `commitDecorators` still returns `[]rowDecorator`; output identical.

**Why this is safe:** `commitIdentWidth()` is a pure function of `m.commits`/`m.cfg`; neither changes inside the loop. It is currently invoked once per row at view.go:916 (the ONLY per-row caller; the call at view.go:827 is already outside its loop). Hoisting it is byte-for-byte identical and removes the entire O(n²) term.

- [ ] **Step 1: Write the failing scaling test**

Add to `commits_render_bench_test.go`:

```go
import "testing" // already imported

// TestCommitDecoratorsAllocScaleLinear pins the O(n²)→O(n) fix: doubling the
// feed must not ~quadruple the allocations of building decorators. Pre-fix this
// fails (≈4× growth); post-fix it is ≈2×.
func TestCommitDecoratorsAllocScaleLinear(t *testing.T) {
	measure := func(n int) float64 {
		m := benchModel(n, 303, 8)
		rows, idx := m.panelView(panelCommits)
		return testing.AllocsPerRun(3, func() {
			_ = m.commitDecorators(rows, idx)
		})
	}
	a := measure(400)
	b := measure(800)
	ratio := b / a
	if ratio > 2.6 { // linear ≈2.0; allow headroom. O(n²) would be ≈4.0.
		t.Fatalf("decorator allocs grew %.2fx for 2x feed (want ≤2.6x, O(n^2) bug present)", ratio)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run TestCommitDecoratorsAllocScaleLinear -v`
Expected: FAIL — ratio ≈ 4x ("decorator allocs grew ~4.0x").

- [ ] **Step 3: Hoist the call**

In `internal/tui/view.go`, `commitDecorators`, find the loop preamble:

```go
	decos := make([]rowDecorator, len(rows))
	for j := range rows {
```

Change to:

```go
	decos := make([]rowDecorator, len(rows))
	identW := m.commitIdentWidth() // loop-invariant: compute once, not per row
	for j := range rows {
```

Then change the per-row call (view.go:916):

```go
		decos[j] = commitLineDecorator(hasDot, dotCol, dotColor, dim, identStart, m.commitIdentWidth())
```

to:

```go
		decos[j] = commitLineDecorator(hasDot, dotCol, dotColor, dim, identStart, identW)
```

- [ ] **Step 4: Run the scaling test + existing render tests**

Run: `go test ./internal/tui/ -run 'TestCommitDecoratorsAllocScaleLinear|Commit|Graph|Panel|Render' -v`
Expected: PASS — scaling ratio ≈2x; all existing commit/graph/render tests still green (proves byte-identical output).

- [ ] **Step 5: Re-run the benchmark and record the new numbers**

Run: `go test ./internal/tui/ -run=XXX -bench=BenchmarkCommitsRender -benchtime=10x`
Expected: `allocs/op` and `ns/op` now scale ~linearly with n (n=5000 ≈ 5× n=1000, not ≈25×). **Record n=1000 and n=5000 ns/op — these drive the Task 4 decision gate below.**

- [ ] **Step 6: Commit**

```bash
git add internal/tui/view.go internal/tui/commits_render_bench_test.go
git commit -m "perf(tui): hoist commitIdentWidth out of the decorator loop (O(n^2)->O(n))"
```

---

## DECISION GATE (before Task 4)

After Task 2 (and Task 3), read the recorded `ns/op` from Task 2 Step 5 / Task 3.

- A frame budget of ~16 ms means held keys drain within ~one frame. If the per-frame path (panelView + decorators, which the benchmark measures) at the largest **realistically loaded** feed is comfortably under that (e.g. ≲5 ms), **Part A is done — skip Task 4 entirely (YAGNI).** Note the decision in the commit/PR and proceed to Task 5.
- If it is still over budget at realistic feed sizes, do Task 4.

"Realistically loaded feed" = how many commits the feed actually pages in during normal scrolling, not a synthetic 20k. Confirm by loading `/home/homeend/others/linux` and scrolling a few pages, or reason from the page size; do not author Task 4 on the assumption that 20k is typical.

---

## Task 3: Lazy per-index tooltip/filter slices

**Files:**
- Modify: `internal/tui/viewstate.go` — `commitList` struct (~260–266), its `Haystack`/`Full`/`TextReveal` (~278–300), and `listFor`'s commits case (~345)
- Modify: `internal/tui/view.go` — add per-index builders; keep the slice builders or replace their callers
- Test: `internal/tui/commit_ident_test.go` (or a new `commit_lazy_test.go`)

**Interfaces:**
- Consumes: `commitIdentOf(model.Commit)`, `Model.commitIdentRows`.
- Produces: `commitList` no longer carries `full`, `text`, `hay` slices; gains the inputs it needs to compute one entry on demand. `Haystack(i)/Full(i)/TextReveal(i)` compute from `items[i]`.

**Why:** `listFor` builds `full`, `text`, and `hay` for every commit on every call, but `full`/`text` are read only by the reveal tooltip (one row) and `hay` only while a filter is active. Computing per index removes 3 all-n builds from the no-filter/no-tooltip frame, with identical results. (Skip this task if the Decision Gate already shows Part A is done and you judge the extra constant irrelevant; it is low-risk insurance, not required for correctness.)

- [ ] **Step 1: Write failing equivalence tests**

In a new `internal/tui/commit_lazy_test.go`:

```go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func lazyModel() Model {
	m := Model{sel: map[panel]int{}, sortModes: map[panel]sortMode{}, focus: panelCommits, width: 120, height: 40}
	m.commits = []model.Commit{
		{Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Subject: "first", Source: "main"},
		{Hash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Subject: "second", Source: "feat"},
	}
	return m
}

// The lazy per-index methods must equal the old eager all-n slices entry-for-entry.
func TestLazyCommitListMatchesEagerSlices(t *testing.T) {
	m := lazyModel()
	cl := m.listFor(panelCommits).(commitList)
	wantHay := m.commitHaystacks()
	wantFull := m.commitFullRows()
	wantText := m.commitTextReveals()
	for i := range m.commits {
		if got := cl.Haystack(i); got != wantHay[i] {
			t.Errorf("Haystack(%d) = %q, want %q", i, got, wantHay[i])
		}
		if got := cl.Full(i); got != wantFull[i] {
			t.Errorf("Full(%d) = %q, want %q", i, got, wantFull[i])
		}
		if got := cl.TextReveal(i); got != wantText[i] {
			t.Errorf("TextReveal(%d) = %q, want %q", i, got, wantText[i])
		}
	}
}
```

- [ ] **Step 2: Run to verify it compiles-and-passes against current code first**

Run: `go test ./internal/tui/ -run TestLazyCommitListMatchesEagerSlices -v`
Expected: PASS against the *current* eager implementation (it pins current behavior). This is the golden lock before refactoring.

- [ ] **Step 3: Add per-index builders in `view.go`**

Add next to the existing slice builders:

```go
// commitHaystackAt is the per-index form of commitHaystacks: full hash + full
// branch name(s) + subject, for filter matching. No styling.
func (m Model) commitHaystackAt(i int) string {
	c := m.commits[i]
	id := commitIdentOf(c)
	names := id.label()
	for _, e := range id.extra {
		names += " " + e
	}
	return c.Hash + " " + names + " " + c.Subject
}

// commitTextRevealAt is the per-index form of commitTextReveals.
func (m Model) commitTextRevealAt(i int) string {
	id := commitIdentOf(m.commits[i])
	label := id.label()
	if label != "" {
		label += " "
	}
	return label + id.pills() + m.commits[i].Subject
}
```

For the untrimmed Full row, extract a per-index form from `commitIdentRows`. Add:

```go
// commitFullRowAt is the per-index form of commitFullRows(true): one untrimmed
// identity row. w is the shared identity-column width.
func (m Model) commitFullRowAt(i, w int) string {
	c := m.commits[i]
	id := commitIdentOf(c)
	row := id.fullToken(w) + " " + id.pills() + c.Subject
	if m.commitListMode {
		row = "● " + row
	} else if !m.commitListMode && m.commitGraphOn() && len(m.commitGraphRows) == len(m.commits) {
		win, _, _ := m.graphWindow(m.commitGraphRows[i])
		row = win + " " + row
	}
	return row
}
```

(Confirm this matches the `full` branch of `commitIdentRows` exactly — same `fullToken`/pills/Subject/list-mode/graph-prefix logic. The equivalence test from Step 1 is the proof.)

- [ ] **Step 4: Make `commitList` carry inputs, not slices, and compute on demand**

In `viewstate.go`, change `commitList`:

```go
type commitList struct {
	items  []model.Commit
	rows   []string // display rows (trimmed identity token, no commit id) — still eager
	m      *Model   // source for lazy per-index full/text/haystack
	identW int      // shared identity-column width for Full
}
```

Replace the slice-indexing methods with lazy ones:

```go
func (l commitList) Haystack(i int) string  { return l.m.commitHaystackAt(i) }
func (l commitList) Full(i int) string      { return l.m.commitFullRowAt(i, l.identW) }
func (l commitList) TextReveal(i int) string { return l.m.commitTextRevealAt(i) }
```

In `listFor`'s commits case (viewstate.go:345), build it with a pointer to the receiver and the width computed once:

```go
case panelCommits:
	return commitList{items: m.commits, rows: m.commitRows(), m: &m, identW: m.commitIdentWidth()}
```

Note: `listFor` has a value receiver `m Model`; `&m` points at that copy, which lives as long as the returned `commitList` is used within the same call chain — safe because the list is consumed synchronously inside the same render/event pass and never stored across turns.

Delete the now-unused `commitFullRows`, `commitTextReveals`, `commitHaystacks` slice builders **only if** no other caller remains — grep first: `grep -rn 'commitFullRows\|commitTextReveals\|commitHaystacks' internal/tui`. Keep `commitFullRows`/etc. if a test references them; otherwise remove to stay DRY.

- [ ] **Step 5: Run the equivalence + render tests**

Run: `go test ./internal/tui/ -run 'TestLazyCommitListMatchesEagerSlices|Tooltip|Reveal|Filter|Commit|Graph' -v`
Expected: PASS — lazy values equal the old slices; tooltip/filter/render unaffected.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/view.go internal/tui/viewstate.go internal/tui/commit_lazy_test.go
git commit -m "perf(tui): compute commit tooltip/filter strings per-index, not all-n per frame"
```

---

## Task 4: Window-then-style (CONTINGENT — only if the Decision Gate says so)

**Do not start this task unless the Decision Gate concluded Part A is still over budget.** If skipped, say so explicitly and move to Task 5.

**Files:**
- Modify: `internal/tui/view.go` — commits render path (322–329, 354) + a cheap-index helper
- Modify: `internal/tui/viewstate.go` — extract the filter+sort half of `panelView` into a reusable `commitDisplayIndices()`
- Test: `internal/tui/commits_window_render_test.go` (new) — golden byte-identical output

**Interfaces:**
- Produces: `Model.commitDisplayIndices() []int` (filter+sort, no styling); a windowed commits render that styles only the visible slice.

**Algorithm (wrap-safe, no per-mode branching):** style outward from the anchor (selection) until the window's `h` display lines are filled. Each row is ≥1 display line, so at most ~`h` rows above and ~`h` below the anchor are ever materialized — O(visible) in every display mode. This is the only correct way to pre-bound materialization when wrap mode can expand a row to multiple lines.

- [ ] **Step 1: Write the golden equivalence test**

```go
package tui

import "testing"

// The windowed render must equal the full-materialization render for the same
// visible state, across display modes and a scrolled selection.
func TestWindowedCommitsRenderMatchesFull(t *testing.T) {
	for _, mode := range []dispMode{modeCutoff, modeScroll, modeWrap} {
		for _, sel := range []int{0, 25, 199} {
			m := benchModel(200, 40, 8)
			m.dispModes[panelCommits] = mode
			m.sel[panelCommits] = sel
			full := m.renderCommitsPanelFull()     // reference: existing path
			win := m.renderCommitsPanelWindowed()   // new path
			if full != win {
				t.Fatalf("mode=%v sel=%d: windowed render differs from full", mode, sel)
			}
		}
	}
}
```

(Step 2 introduces `renderCommitsPanelFull` as a thin wrapper around the *current* assembly at view.go:328/354 so the test has a stable reference; the new windowed path replaces it once green.)

- [ ] **Step 2: Run to verify it fails to compile / fails**

Run: `go test ./internal/tui/ -run TestWindowedCommitsRenderMatchesFull -v`
Expected: FAIL — `renderCommitsPanelWindowed` undefined.

- [ ] **Step 3: Implement `commitDisplayIndices` (cheap idx) in `viewstate.go`**

Extract panelView's filter+sort into a helper that does no row materialization:

```go
// commitDisplayIndices returns the Commits panel's display order (filter + sort
// applied) without materializing any styled row strings. Filtering uses the
// cheap Haystack; sorting uses Name/Date. This is panelView minus the rows build.
func (m Model) commitDisplayIndices() []int {
	l := m.listFor(panelCommits)
	q := ""
	if m.filterActive(panelCommits) {
		q = strings.ToLower(m.filterQuery)
	}
	idx := make([]int, 0, l.Len())
	for i := 0; i < l.Len(); i++ {
		if q != "" {
			text := l.Row(i)
			if h, ok := l.(haystacker); ok {
				text = h.Haystack(i)
			}
			if !strings.Contains(strings.ToLower(text), q) {
				continue
			}
		}
		idx = append(idx, i)
	}
	sortIndices(l, m.sortModes[panelCommits], idx)
	return idx
}
```

(Note: `l.Row(i)` in the filter branch only runs when a filter is active; in that case styling the matched rows is acceptable. Optimize later only if filtering on a huge feed is itself a hotspot.)

- [ ] **Step 4: Implement the windowed render path in `view.go`**

Add `renderCommitsPanelWindowed` that: computes `idx := m.commitDisplayIndices()`; resolves the row window via `windowStart(len(idx), rowsCap, sel)` expanded to fill `h` display lines from the anchor; styles only those commits (build their `rows` via `commitIdentOf`-based row build + decorators with one `commitIdentWidth()`); and renders them through `renderWindow`/`renderPanel` with the anchor adjusted to the window. Keep `renderCommitsPanelFull` as the current assembly for the test reference, then switch view.go:328/354 (and the narrow path 322/324) to call the windowed version once the golden test is green.

Exact materialization bounds and the renderWindow integration are derived against the golden test — the test is the contract; do not merge until `TestWindowedCommitsRenderMatchesFull` passes for all three modes and all three selections.

- [ ] **Step 5: Run golden + benchmark**

Run: `go test ./internal/tui/ -run TestWindowedCommitsRenderMatchesFull -v` → PASS for every mode/sel.
Run: `go test ./internal/tui/ -run=XXX -bench=BenchmarkCommitsRender -benchtime=10x` → time now ~flat across n (n=5000 ≈ n=1000), independent of `cols`.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/view.go internal/tui/viewstate.go internal/tui/commits_window_render_test.go
git commit -m "perf(tui): window-then-style the Commits panel (O(visible) per frame)"
```

---

## Task 5: Part B — files-view read settle (pure-drop)

**Files:**
- Modify: `internal/tui/model.go` — add `Model` field; `commitFilesMsg` handler (192); `l` open handler (~868)
- Modify: `internal/tui/files_view.go` — `moveListUnderFilesView` (~347–356), `syncFilesViewToSelectedCommit` (~363–369)
- Test: `internal/tui/files_view_test.go` (new cases)

**Interfaces:**
- Produces: `Model.filesReadInflight bool` — set when a `CommitFiles` read is issued for the files view, cleared when `commitFilesMsg` arrives.

**Behavior:** while a per-commit `CommitFiles` read is in flight, a further `j`/`k` that would issue another such read is dropped entirely (selection does not move). The completion of the read clears the flag, so the next keypress advances. This paces commit navigation by read speed instead of queuing one read per held-key repeat.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/files_view_test.go`:

```go
// While a files-view CommitFiles read is in flight, a further j/k must not issue
// a second read or move the selection (pure-drop); the completion clears the gate.
func TestFilesViewDropsReadWhileInflight(t *testing.T) {
	m := loadedModelTwoCommits(t) // helper that opens the files view on commit 0
	m.filesReadInflight = true
	before := m.sel[panelCommits]
	u, cmd := m.Update(keyMsg("j"))
	mm := u.(Model)
	if cmd != nil {
		t.Fatal("j must not issue a read while one is in flight")
	}
	if mm.sel[panelCommits] != before {
		t.Fatal("selection must not move while a read is in flight")
	}
	// The completion clears the gate.
	u2, _ := mm.Update(commitFilesMsg{hash: m.filesHash, subject: "x"})
	if u2.(Model).filesReadInflight {
		t.Fatal("commitFilesMsg must clear filesReadInflight")
	}
}
```

If a `loadedModelTwoCommits` helper does not already exist, build the model inline in the test from two `model.Commit`s with the files view open (`m.filesView = &contentPopup{...}`, `m.filesHash = commits[0].Hash`, `m.focus = panelCommits`), mirroring `files_view_test.go:176` setup.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestFilesViewDropsReadWhileInflight -v`
Expected: FAIL — `m.filesReadInflight` undefined.

- [ ] **Step 3: Add the field and wire set/clear**

In `internal/tui/model.go`, add to the `Model` struct (near `filesHash`/`filesView`):

```go
	filesReadInflight bool // a per-commit files-view CommitFiles read is in flight; drop further nav reads until it lands
```

In the `commitFilesMsg` case (model.go:192), clear it on arrival — add as the first line of the case, before the closed/stale guard:

```go
	case commitFilesMsg:
		m.filesReadInflight = false
		if m.filesView == nil || msg.hash != m.filesHash {
```

In the `l` open handler (model.go:~868), set it when issuing the initial load:

```go
			m.filesReadInflight = true
			return m, m.loadCommitFilesCmd(c)
```

- [ ] **Step 4: Gate the per-commit reload in `files_view.go`**

In `moveListUnderFilesView`, before the hash-change reload block (~347), drop when a read is in flight:

```go
	if m.filesReadInflight {
		return m, nil // pure-drop: pace commit nav by read completion
	}
	bi, ok := m.backingIndex(panelCommits)
```

And where it issues the reload (the `filesCmd := m.loadCommitFilesCmd(...)` path), set the flag just before returning the command:

```go
	m.filesHash = m.commits[bi].Hash
	m.filesReadInflight = true
	filesCmd := m.loadCommitFilesCmd(m.commits[bi])
```

Apply the same `m.filesReadInflight = true` immediately before the `loadCommitFilesCmd` return in `syncFilesViewToSelectedCommit` (~369).

- [ ] **Step 5: Run the test + the files-view suite**

Run: `go test ./internal/tui/ -run 'FilesView|FilesView.*|TestFilesViewDropsReadWhileInflight' -v`
Expected: PASS — read dropped while in flight, selection frozen, gate cleared on completion; existing files-view tests still green.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/files_view.go internal/tui/files_view_test.go
git commit -m "perf(tui): settle files-view commit reads — drop held-key reloads until in-flight read lands"
```

---

## Task 6: Docs + full suite

**Files:**
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Add a CHANGELOG entry** under the appropriate heading:

```
- Commits panel no longer recomputes the whole loaded feed every frame — fixes
  severe navigation lag on large repos (was O(commits²) per keystroke). The
  commit files view also stops issuing a read per held key, loading where
  navigation settles.
```

- [ ] **Step 2: Run the full suite with race**

Run: `./test.sh race`
Expected: `all green`.

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: changelog for Commits render performance fix"
```

---

## Self-Review

**Spec coverage:**
- Spec goal "O(visible) expensive work / sub-frame nav" → Tasks 2 (kills O(n²)), 3 (drops all-n tooltip/filter builds), 4 (window-then-style, contingent). ✓
- Spec goal "files-view does not read per intermediate keypress" → Task 5. ✓
- Spec "rendered output byte-for-byte unchanged" → Task 2 relies on existing render tests; Task 3 has `TestLazyCommitListMatchesEagerSlices`; Task 4 has `TestWindowedCommitsRenderMatchesFull` across modes/selections. ✓
- Spec "regression benchmark as acceptance" → Task 1, re-run in Tasks 2 & 4. ✓
- Spec non-goal "don't rework renderWindow lightly" → Task 4 is contingent and gated by golden tests precisely because it touches the shared render. ✓

**Placeholder scan:** Task 4 Steps 4 intentionally defers exact renderWindow-integration micro-code to the golden test contract — this is a genuinely test-driven step, not a hand-wave; its acceptance ("do not merge until the golden test passes for all modes/selections") is concrete. All other steps carry complete code.

**Type consistency:** `filesReadInflight bool`, `commitDisplayIndices() []int`, `commitHaystackAt/commitTextRevealAt/commitFullRowAt(i[,w] int) string`, `commitList{items, rows, m *Model, identW int}`, `benchModel(n, lanes, cols int)` — names used consistently across tasks.
