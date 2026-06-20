# Visual Commit Graph (Phase 2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Draw a single-line-per-commit Unicode graph column in the Commits panel, laid out over the Phase-1 date-ordered all-branches feed.

**Architecture:** A pure `internal/commitgraph` engine folds the ordered commits (hash+parents) into per-row glyph strings via lane assignment; the TUI caches those strings and prepends them in `commitRows()` only when the panel is in natural order.

**Tech Stack:** Go 1.26, Bubble Tea. No new deps. No git/domain/CLI change.

## Global Constraints

- Work in the existing worktree on branch `worktree-commit-graph-lanes` (off `main` tip `122475f`). Worktree-relative paths only.
- The engine is **pure**: `internal/commitgraph` imports nothing from `git`/`tui`/`domain`/`lipgloss` (archtest-friendly; mirrors `textdiff`).
- **Unicode rounded** glyphs, **monochrome** (engine emits plain strings), **single line per commit**.
- Graph renders **only in natural feed order** — `!m.filterActive(panelCommits) && m.sortModes[panelCommits] == sortDefault`; otherwise plain rows.
- No hard lane cap (subject truncates under the panel cutoff mode). Run `./test.sh race` before done; commit trailers as in the repo.

---

### Task 1: The `internal/commitgraph` engine

A pure lane-assignment + single-line glyph renderer, pinned by canonical-topology tests.

**Files:**
- Create: `internal/commitgraph/graph.go`
- Test: `internal/commitgraph/graph_test.go`

**Interfaces:**
- Produces: `commitgraph.Commit{Hash string; Parents []string}`; `commitgraph.Row{Cells string; Lane int; Width int}`; `func Lay(commits []Commit) (rows []Row, width int)` — `rows` parallel to input; `Cells` padded to `width` (= 2 × max simultaneous lanes); `Lane` is the node's column.

- [ ] **Step 1: Write the failing canonical-topology tests**

Create `internal/commitgraph/graph_test.go`. Each case lists commits newest-first and the expected per-row `Cells` (right-trimmed for readability). The glyph alphabet: `● │ ─ ╮ ╭ ╯ ╰ ┬ ┴ ┼` and space.

```go
package commitgraph

import (
	"strings"
	"testing"
)

func trimRows(rows []Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = strings.TrimRight(r.Cells, " ")
	}
	return out
}

func assertGraph(t *testing.T, commits []Commit, want []string) {
	t.Helper()
	rows, _ := Lay(commits)
	got := trimRows(rows)
	if len(got) != len(want) {
		t.Fatalf("rows = %d, want %d\n got=%q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d = %q, want %q\n full got=%q", i, got[i], want[i], got)
		}
	}
}

func TestLinear(t *testing.T) {
	assertGraph(t, []Commit{
		{"c3", []string{"c2"}},
		{"c2", []string{"c1"}},
		{"c1", nil},
	}, []string{"●", "●", "●"})
}

func TestBranchAndMerge(t *testing.T) {
	// c5 merges c4 (main) and c3 (feat); both fork from c2 (base).
	assertGraph(t, []Commit{
		{"c5", []string{"c4", "c3"}},
		{"c4", []string{"c2"}},
		{"c3", []string{"c2"}},
		{"c2", []string{"c1"}},
		{"c1", nil},
	}, []string{
		"●─╮",
		"● │",
		"│ ●",
		"●─╯",
		"●",
	})
}

func TestPassThroughAcrossMerge(t *testing.T) {
	// An independent branch (lane for "bp") passes straight through the row where
	// "base" merges its right-hand child into lane 0 — the crossing must be ┼.
	// order: A, B, C, base, root, bp-tip
	assertGraph(t, []Commit{
		{"a", []string{"base"}},
		{"b", []string{"bp"}},
		{"c", []string{"base"}},
		{"base", []string{"root"}},
		{"root", nil},
		{"bp", nil},
	}, []string{
		"●",
		"│ ●",
		"│ │ ●",
		"●─┼─╯",
		"● │",
		"  ●",
	})
}

func TestOctopusMerge(t *testing.T) {
	// m has three parents → two forks on one row: ●─┬─╮
	assertGraph(t, []Commit{
		{"m", []string{"p1", "p2", "p3"}},
		{"p1", nil},
		{"p2", nil},
		{"p3", nil},
	}, []string{
		"●─┬─╮",
		"● │ │",
		"  ● │",
		"    ●",
	})
}

func TestTwoRoots(t *testing.T) {
	// Two independent histories interleaved; second tip opens a new lane.
	assertGraph(t, []Commit{
		{"a2", []string{"a1"}},
		{"b2", []string{"b1"}},
		{"a1", nil},
		{"b1", nil},
	}, []string{
		"●",
		"│ ●",
		"● │",
		"  ●",
	})
}

func TestWidthNormalized(t *testing.T) {
	rows, width := Lay([]Commit{
		{"m", []string{"p1", "p2"}},
		{"p1", []string{"r"}},
		{"p2", []string{"r"}},
		{"r", nil},
	})
	if width != 4 {
		t.Fatalf("width = %d, want 4 (2 lanes)", width)
	}
	for i, r := range rows {
		if len([]rune(r.Cells)) != 4 {
			t.Fatalf("row %d Cells %q has %d runes, want 4 (padded to width)", i, r.Cells, len([]rune(r.Cells)))
		}
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `go test ./internal/commitgraph/ 2>&1 | tail`
Expected: FAIL (build: package/`Lay` undefined).

- [ ] **Step 3: Implement the engine**

Create `internal/commitgraph/graph.go`:

```go
// Package commitgraph lays out a single-line-per-commit ASCII/Unicode commit
// graph: ordered commits (hash + parents, newest-first) → per-row lane glyphs.
// Pure; no git/TUI/lipgloss imports.
package commitgraph

// Commit is the engine's minimal input.
type Commit struct {
	Hash    string
	Parents []string
}

// Row is the laid-out graph for one commit. Cells is padded to the shared Width.
type Row struct {
	Cells string
	Lane  int
	Width int
}

// Lay folds commits (newest-first) into per-row graph cells. Deterministic.
func Lay(commits []Commit) ([]Row, int) {
	rows := make([]Row, 0, len(commits))
	var lanes []string // lanes[i] = hash lane i is waiting for ("" = free)
	maxLanes := 0

	for _, c := range commits {
		// 1. node lane = leftmost lane targeting this commit; extras = merging.
		node := -1
		var merging []int
		for i, t := range lanes {
			if t == c.Hash {
				if node < 0 {
					node = i
				} else {
					merging = append(merging, i)
				}
			}
		}
		if node < 0 {
			node = firstFree(lanes, nil)
			if node == len(lanes) {
				lanes = append(lanes, "")
			}
		}
		mergeSet := toSet(merging)

		// 2. outgoing targets: first parent keeps the node lane; extras fork into
		//    fresh free lanes (never reusing a merging slot on this row, to keep
		//    glyphs unambiguous). A root frees its lane.
		var forks []int
		if len(c.Parents) == 0 {
			lanes[node] = ""
		} else {
			lanes[node] = c.Parents[0]
			for _, p := range c.Parents[1:] {
				f := firstFree(lanes, mergeSet)
				if f == len(lanes) {
					lanes = append(lanes, "")
				}
				lanes[f] = p
				forks = append(forks, f)
			}
		}
		for _, mi := range merging { // free the merged-in children's lanes
			lanes[mi] = ""
		}

		// 3. render over the current lane count, then compact trailing frees.
		n := len(lanes)
		if n > maxLanes {
			maxLanes = n
		}
		rows = append(rows, Row{Cells: renderRow(n, node, merging, forks, lanes), Lane: node})
		for len(lanes) > 0 && lanes[len(lanes)-1] == "" {
			lanes = lanes[:len(lanes)-1]
		}
	}

	width := maxLanes * 2
	for i := range rows {
		rows[i].Width = width
		rows[i].Cells = pad(rows[i].Cells, width)
	}
	return rows, width
}

// renderRow draws one commit row across n lanes. Two display columns per lane:
// the lane glyph + a horizontal connector ('─' inside the node↔fork/merge span,
// else space). minC..maxC is that span.
func renderRow(n, node int, merging, forks []int, lanes []string) string {
	mergeSet, forkSet := toSet(merging), toSet(forks)
	minC, maxC := node, node
	for _, c := range append(append([]int{}, merging...), forks...) {
		if c < minC {
			minC = c
		}
		if c > maxC {
			maxC = c
		}
	}
	out := make([]rune, 0, n*2)
	for c := 0; c < n; c++ {
		var g rune
		switch {
		case c == node:
			g = '●'
		case mergeSet[c]: // child terminating from above, turning toward node
			switch {
			case c > minC && c < maxC:
				g = '┴'
			case c < node:
				g = '╰'
			default:
				g = '╯'
			}
		case forkSet[c]: // extra parent opening below, from node
			switch {
			case c > minC && c < maxC:
				g = '┬'
			case c < node:
				g = '╭'
			default:
				g = '╮'
			}
		case lanes[c] != "" && c > minC && c < maxC:
			g = '┼' // pass-through lane crossed by the horizontal span
		case lanes[c] != "":
			g = '│'
		case c > minC && c < maxC:
			g = '─' // empty column under the horizontal span
		default:
			g = ' '
		}
		out = append(out, g)
		if c >= minC && c < maxC {
			out = append(out, '─')
		} else {
			out = append(out, ' ')
		}
	}
	return string(out)
}

func firstFree(lanes []string, exclude map[int]bool) int {
	for i, t := range lanes {
		if t == "" && !exclude[i] {
			return i
		}
	}
	return len(lanes)
}

func toSet(xs []int) map[int]bool {
	if len(xs) == 0 {
		return nil
	}
	s := make(map[int]bool, len(xs))
	for _, x := range xs {
		s[x] = true
	}
	return s
}

func pad(s string, w int) string {
	r := []rune(s)
	for len(r) < w {
		r = append(r, ' ')
	}
	return string(r)
}
```

- [ ] **Step 4: Run the topology tests; iterate to green**

Run: `go test ./internal/commitgraph/ -v 2>&1 | tail -30`
Expected: PASS. If a row mismatches, the failure prints `got`/`want` — adjust the glyph branches (the corner-direction or interior `┬`/`┴`/`┼` cases are the usual culprits) until every canonical topology matches. The expected strings in the tests are the source of truth.

- [ ] **Step 5: Commit**

```bash
git add internal/commitgraph/
git commit -m "feat(commitgraph): pure single-line lane-layout engine

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 2: TUI integration — cached graph column

**Files:**
- Modify: `internal/tui/model.go` (state fields; `rebuildCommitGraph`; call it at the 3 commit-set sites)
- Modify: `internal/tui/view.go` (`commitRows` prepends the graph in natural order)
- Test: `internal/tui/commit_graph_test.go` (create)

**Interfaces:**
- Consumes: `commitgraph.Lay`, `commitgraph.Commit`; `m.filterActive(panelCommits)`, `m.sortModes[panelCommits]`, `sortDefault`.
- Produces: `m.commitGraphRows []string`; `m.rebuildCommitGraph()`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/commit_graph_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func graphModel() Model {
	m := footerModel()
	m.commits = []model.Commit{
		{Hash: "c2", Parents: []string{"c1"}, Subject: "second"},
		{Hash: "c1", Subject: "first"},
	}
	m = m.rebuildCommitGraph()
	m.focus = panelCommits
	return m
}

func TestCommitRowsHaveGraphInNaturalOrder(t *testing.T) {
	m := graphModel()
	rows := m.commitRows()
	if len(rows) != 2 || !strings.HasPrefix(rows[0], "●") {
		t.Fatalf("natural-order rows should start with the graph node: %q", rows)
	}
	if !strings.Contains(rows[0], "second") {
		t.Fatalf("row should still contain the subject: %q", rows[0])
	}
}

func TestCommitRowsDropGraphWhenFiltered(t *testing.T) {
	m := graphModel()
	m.filterPanel = panelCommits
	m.filterQuery = "second"
	rows := m.commitRows()
	if strings.HasPrefix(rows[0], "●") {
		t.Fatalf("graph must be suppressed when the Commits panel is filtered: %q", rows[0])
	}
}

func TestCommitRowsDropGraphWhenSorted(t *testing.T) {
	m := graphModel()
	m.sortModes[panelCommits] = sortDateDesc
	rows := m.commitRows()
	if strings.HasPrefix(rows[0], "●") {
		t.Fatalf("graph must be suppressed when the Commits panel is re-sorted: %q", rows[0])
	}
}

func TestRebuildCommitGraphAligns(t *testing.T) {
	m := graphModel()
	if len(m.commitGraphRows) != len(m.commits) {
		t.Fatalf("graph rows (%d) must align with commits (%d)", len(m.commitGraphRows), len(m.commits))
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `go test ./internal/tui/ -run 'TestCommitRows|TestRebuildCommitGraph' 2>&1 | tail`
Expected: FAIL (`rebuildCommitGraph`/`commitGraphRows` undefined).

- [ ] **Step 3: Add state + rebuild helper**

In `internal/tui/model.go`, add to the Model struct (near `commitScopeBranches`):

```go
	commitGraphRows  []string // cached single-line graph cells, parallel to commits; empty = none
```

Add the import `"github.com/gigagit/gg/internal/commitgraph"` to model.go, and the helper (near `commitScopeLabel`):

```go
// rebuildCommitGraph recomputes the cached graph cells from m.commits. Called
// whenever m.commits changes (the lane fold needs the whole loaded window).
func (m Model) rebuildCommitGraph() Model {
	cs := make([]commitgraph.Commit, len(m.commits))
	for i, c := range m.commits {
		cs[i] = commitgraph.Commit{Hash: c.Hash, Parents: c.Parents}
	}
	rows, _ := commitgraph.Lay(cs)
	m.commitGraphRows = make([]string, len(rows))
	for i, r := range rows {
		m.commitGraphRows[i] = r.Cells
	}
	return m
}
```

- [ ] **Step 4: Call it at the 3 commit-set sites**

In `internal/tui/model.go`, immediately after each `m.commits = …`:
- in the `commitsPagedMsg` case (`m.commits = st.Commits`) → add `m = m.rebuildCommitGraph()`;
- in the `commitsReloadedMsg` case (`m.commits = msg.state.Commits`) → add `m = m.rebuildCommitGraph()`;
- in the `dataLoadedMsg` case (`m.commits = msg.commits`) → add `m = m.rebuildCommitGraph()`.

- [ ] **Step 5: Prepend the graph in `commitRows`**

In `internal/tui/view.go`, replace `commitRows`:

```go
func (m Model) commitRows() []string {
	graph := m.commitGraphOn() && len(m.commitGraphRows) == len(m.commits)
	out := make([]string, 0, len(m.commits))
	for i, c := range m.commits {
		h := c.Hash
		if len(h) > 7 {
			h = h[:7]
		}
		row := h + " " + commitRefLabels(c.Refs) + c.Subject
		if graph {
			row = m.commitGraphRows[i] + " " + row
		}
		out = append(out, row)
	}
	return out
}

// commitGraphOn reports whether the graph is coherent to draw: the Commits panel
// must be in natural feed order (no filter, default sort) so rows are contiguous
// and the lane topology stays valid.
func (m Model) commitGraphOn() bool {
	return !m.filterActive(panelCommits) && m.sortModes[panelCommits] == sortDefault
}
```

- [ ] **Step 6: Run tests**

Run: `go build ./... && go test ./internal/tui/ -run 'TestCommitRows|TestRebuildCommitGraph' -v 2>&1 | tail -12 && go test ./internal/tui/ 2>&1 | tail -3`
Expected: PASS, and the full tui suite stays green (existing commit-row tests still pass — in natural order they now carry a graph prefix; if `TestCommitRowsRenderLabels` asserts the row *starts* with the hash, it builds its model via `footerModel()` without `rebuildCommitGraph`, so `commitGraphRows` is empty → `graph` is false → no prefix; verify and, if needed, assert `Contains` not `HasPrefix`).

- [ ] **Step 7: Commit**

```bash
git add internal/tui/model.go internal/tui/view.go internal/tui/commit_graph_test.go
git commit -m "feat(tui): draw the commit graph column in natural feed order

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 3: Docs + race gate

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`, `internal/tui/help.go`

- [ ] **Step 1: CHANGELOG / README / CLAUDE / help**

- CHANGELOG `### Added`: the Commits panel now draws a single-line Unicode commit **graph** (lane layout over the all-branches date-ordered feed); shown in natural order (hidden while filtering/sorting the panel).
- README: extend the Commits-panel row — it now draws a graph column to the left of each commit when unfiltered/unsorted.
- CLAUDE.md package map: add a `commitgraph` row — "Pure single-line commit-graph lane engine (ordered `{hash,parents}` → per-row Unicode glyph cells); no git/TUI deps. Consumed by the TUI Commits panel (cached in `m.commitGraphRows`, drawn only in natural feed order)."
- help.go: note that the Commits panel draws a graph in natural order (hidden under filter/sort) — add to the existing Commits-panel help block.

- [ ] **Step 2: Full race gate**

Run: `./test.sh race`
Expected: vet+gofmt clean, all unit tests pass, e2e green.

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "docs: commit graph column (Phase 2); help

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

## Self-Review

**Spec coverage:**
- Pure `internal/commitgraph` engine, Unicode rounded, single-line, lane fold → Task 1. ✓
- Canonical topology tests (linear, branch+merge, pass-through-cross `┼`, octopus `┬`, two roots, width) → Task 1 Step 1. ✓
- Cache `commitGraphRows`, recompute on commits-change at the 3 sites → Task 2 Steps 3-4. ✓
- Prepend in natural order only (filter/sort suppression), haystack-safe → Task 2 Step 5 (`commitGraphOn`). ✓
- Monochrome (engine emits strings; `Lane` retained for future color) → Task 1 `Row`. ✓
- No hard lane cap; subject truncates → no cap code; natural width. ✓
- Docs/help → Task 3. ✓

**Placeholder scan:** none — the engine is fully implemented; Task 1 Step 4 explicitly directs iterating glyph branches against the pinned expected strings (a real TDD instruction, not a deferred TODO).

**Type consistency:** `commitgraph.Commit{Hash,Parents}`, `Row{Cells,Lane,Width}`, `Lay(commits) ([]Row,int)`; `m.commitGraphRows []string`, `rebuildCommitGraph() Model`, `commitGraphOn() bool` — consistent across tasks.
