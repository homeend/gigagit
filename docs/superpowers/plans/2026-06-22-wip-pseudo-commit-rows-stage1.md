# WIP pseudo-commit rows — Stage 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show uncommitted work as two chained, dirty-only pseudo-rows (`◇ Working tree (N)`, `◇ Staged (N)`) at the top of the Commits panel — graph-connected to HEAD, selectable, with a single-select diff — while refusing all commit-operations on them.

**Architecture:** `m.commits` stays the **pure** domain feed (HEAD = `commits[0]`). A `m.wipRows` slice (0–2 entries derived from `m.status`) is unified into the Commits panel **only at the display layer**: the `commitList` length and the commit graph prepend the WIP rows, so the unified display index space is `wipRows ++ commits`. The selection→commit funnel `backingIndex(panelCommits)` becomes wip-aware (returns `ok=false` for a wip row, otherwise the offset-corrected index into the pure feed), so every existing op site that guards `ok` is automatically wip-safe and fails loud, never leaking a sentinel into git.

**Tech Stack:** Go 1.26, Bubble Tea (value-receiver `Model`), lipgloss, `internal/commitgraph`, `model.Endpoint`.

## Global Constraints

- TUI-only: no changes to `internal/engine`, `internal/domain`, `internal/git`, `internal/cli`, or `internal/agentskill` (no `agentskill.Version` bump).
- `m.commits` stays the pure feed — never inject WIP into it; HEAD stays `m.commits[0]`. The WIP/commit unification lives in the display layer (`commitList`, graph, `backingIndex`).
- WIP rows are dirty-only: a `Working tree` row iff there are unstaged changes; a `Staged` row iff there are staged changes; clean tree → none (today's behavior, a regression guard).
- Chain order top→down: `Working tree → Staged → HEAD`; if only one is dirty it parents directly to HEAD.
- A WIP row is never a real commit: `backingIndex(panelCommits)` returns `ok=false` for it, and every commit-operation is unavailable on it.
- WIP node glyph is `◇` (hollow), distinct from a real commit's `●`.
- Rendering stays O(visible rows) (preserve the windowed render perf model).
- Commit messages end with the trailers:
  `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`
  `Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro`
- Run `go test ./internal/tui/` after each task; `./test.sh race` before merge.

---

### Task 1: WIP row types, derivation, accessors (no behavior change)

**Files:**
- Create: `internal/tui/wip_rows.go`
- Create: `internal/tui/wip_rows_test.go`

**Interfaces:**
- Consumes: `model.WorkingTreeStatus` (`Files []FileStatus`; `inFilesPanel`/`inStagedPanel` in viewstate.go classify unstaged/staged).
- Produces:
  - `type wipKind int` with `wipWorktree`, `wipStaged`.
  - `type wipRow struct { kind wipKind; count int }`.
  - `func deriveWipRows(st model.WorkingTreeStatus) []wipRow` — `[Working tree, Staged]` order, each present only when its count > 0.
  - `func (r wipRow) label() string` — `"Working tree"` / `"Staged"`.
  - `func (m Model) wipCount() int` — `len(m.wipRows)`.
  - `func (m Model) isWipRow(unified int) bool` — `unified >= 0 && unified < m.wipCount()`.
  - `func (m Model) wipRowAt(unified int) (wipRow, bool)`.
  - `func (m Model) commitsTotal() int` — `m.wipCount() + len(m.commits)`.
- NOTE: this task does NOT populate `m.wipRows` from status yet, so the running app is unchanged. Tests set `m.wipRows` directly.

- [ ] **Step 1: Write the failing test**

```go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func st(files ...model.FileStatus) model.WorkingTreeStatus {
	return model.WorkingTreeStatus{Files: files}
}

func TestDeriveWipRows(t *testing.T) {
	unstaged := model.FileStatus{Path: "a", Unstaged: 'M'}
	staged := model.FileStatus{Path: "b", Staged: 'M'}
	both := model.FileStatus{Path: "c", Staged: 'M', Unstaged: 'M'}

	cases := []struct {
		name string
		in   model.WorkingTreeStatus
		want []wipRow
	}{
		{"clean", st(), nil},
		{"only unstaged", st(unstaged), []wipRow{{wipWorktree, 1}}},
		{"only staged", st(staged), []wipRow{{wipStaged, 1}}},
		{"both via one file", st(both), []wipRow{{wipWorktree, 1}, {wipStaged, 1}}},
		{"both via two files", st(unstaged, staged), []wipRow{{wipWorktree, 1}, {wipStaged, 1}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := deriveWipRows(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("row %d: got %v, want %v", i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestWipAccessors(t *testing.T) {
	m := Model{
		wipRows: []wipRow{{wipWorktree, 2}, {wipStaged, 1}},
		commits: []model.Commit{{Hash: "h0"}, {Hash: "h1"}},
	}
	if m.wipCount() != 2 || m.commitsTotal() != 4 {
		t.Fatalf("wipCount=%d total=%d", m.wipCount(), m.commitsTotal())
	}
	if !m.isWipRow(0) || !m.isWipRow(1) || m.isWipRow(2) {
		t.Fatal("isWipRow boundary wrong")
	}
	if r, ok := m.wipRowAt(1); !ok || r.kind != wipStaged {
		t.Fatalf("wipRowAt(1) = %v,%v", r, ok)
	}
	if _, ok := m.wipRowAt(2); ok {
		t.Fatal("wipRowAt past wip range must be false")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestDeriveWipRows|TestWipAccessors' -v`
Expected: FAIL — undefined `wipRow`, `deriveWipRows`, etc.

- [ ] **Step 3: Add the `m.wipRows` field**

In `internal/tui/model.go`, in the `Model` struct near the other Commits-panel state (e.g. just after `commitGraphLanes`), add:

```go
	wipRows []wipRow // 0–2 derived pseudo-rows (Working tree / Staged) shown atop the Commits feed when dirty
```

- [ ] **Step 4: Create `internal/tui/wip_rows.go`**

```go
package tui

import "github.com/gigagit/gg/internal/model"

// wipKind distinguishes the two pseudo-rows that represent uncommitted work.
type wipKind int

const (
	wipWorktree wipKind = iota // unstaged changes (working tree)
	wipStaged                  // staged changes (index)
)

// wipRow is one pseudo-commit row at the top of the Commits panel.
type wipRow struct {
	kind  wipKind
	count int
}

func (r wipRow) label() string {
	if r.kind == wipStaged {
		return "Staged"
	}
	return "Working tree"
}

// deriveWipRows builds the dirty-only pseudo-rows from a status snapshot:
// a Working tree row when there are unstaged changes, a Staged row when there
// are staged changes, in that top→down order. A clean tree yields none.
func deriveWipRows(st model.WorkingTreeStatus) []wipRow {
	unstaged, staged := 0, 0
	for _, f := range st.Files {
		if inFilesPanel(f) {
			unstaged++
		}
		if inStagedPanel(f) {
			staged++
		}
	}
	var rows []wipRow
	if unstaged > 0 {
		rows = append(rows, wipRow{wipWorktree, unstaged})
	}
	if staged > 0 {
		rows = append(rows, wipRow{wipStaged, staged})
	}
	return rows
}

// wipCount is the number of pseudo-rows currently prepended to the Commits feed.
func (m Model) wipCount() int { return len(m.wipRows) }

// commitsTotal is the unified Commits-panel length: wip rows + real commits.
func (m Model) commitsTotal() int { return m.wipCount() + len(m.commits) }

// isWipRow reports whether a unified Commits index addresses a pseudo-row
// (the first wipCount entries) rather than a real commit.
func (m Model) isWipRow(unified int) bool {
	return unified >= 0 && unified < m.wipCount()
}

// wipRowAt returns the pseudo-row at a unified index, or false if it is a commit.
func (m Model) wipRowAt(unified int) (wipRow, bool) {
	if m.isWipRow(unified) {
		return m.wipRows[unified], true
	}
	return wipRow{}, false
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestDeriveWipRows|TestWipAccessors' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/tui/wip_rows.go internal/tui/wip_rows_test.go
git add internal/tui/wip_rows.go internal/tui/wip_rows_test.go internal/tui/model.go
git commit -m "feat(tui): WIP pseudo-row types, derivation, and indexing accessors

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 2: Unified indexing — list, render, graph, wip-aware `backingIndex`

This is the atomic correctness change: once `m.wipRows` is populated and the
`commitList` reports the larger length, the render helpers, graph, and
`backingIndex` must ALL interpret the unified index space together.

**Files:**
- Modify: `internal/tui/model.go` — populate `m.wipRows` on status change; `rebuildCommitGraph` prepend; graph invariant sites.
- Modify: `internal/tui/viewstate.go` — `commitList` unified; `backingIndex` wip-aware.
- Modify: `internal/tui/view.go` — `commitIdentRowAt` / `commitHaystackAt` / `commitTextRevealAt` dispatch; `commitDecoratorsRange` dispatch; graph-prefix invariant sites.
- Modify: `internal/tui/commit_ident.go` — a `◇` WIP-row renderer + decorator.
- Test: `internal/tui/wip_rows_render_test.go` (new).

**Interfaces:**
- Consumes: Task 1 accessors; `commitgraph.Lay`; the existing `commitIdentRowAt(i, w, full)`.
- Produces: unified `commitList`; `backingIndex(panelCommits)` returning `(pureCommitIndex, ok)` with `ok=false` on a wip row; WIP rows rendered with `◇` + graph connectors.

**Decision — index convention:** the per-row render helpers (`commitIdentRowAt`, `commitHaystackAt`, `commitTextRevealAt`) and the graph caches (`commitGraphRows`, `commitGraphLanes`) all index the **unified** space `[0, commitsTotal())`: `u < wipCount` → wip row; else `m.commits[u-wipCount]`. `backingIndex` converts a unified index back to a pure-feed index (or `ok=false`).

- [ ] **Step 1: Write the failing test**

```go
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

// dirtyLoadedModel loads a real linear repo and dirties the tree: one unstaged
// file and one staged file, so deriveWipRows yields both rows.
func dirtyLoadedModel(t *testing.T, n int) (Model, string) {
	t.Helper()
	m := loadedModelLinearCommits(t, n)
	dir := m.currentWorktree
	if dir == "" { // fall back to the repo root the feed came from
		dir = repoDirOf(t, m)
	}
	return m, dir
}

func TestWipRowsAppearAndBackingIndexGuards(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	// Inject a known dirty status (Task 2 derives wipRows from status on reload;
	// here set directly to test the unified indexing deterministically).
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "u", Unstaged: 'M'},
		{Path: "s", Staged: 'M'},
	}}
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()

	if m.wipCount() != 2 {
		t.Fatalf("wipCount=%d want 2", m.wipCount())
	}
	// commitList reports the unified length.
	if l := m.listFor(panelCommits); l.Len() != m.commitsTotal() {
		t.Fatalf("commitList.Len=%d want %d", l.Len(), m.commitsTotal())
	}
	// Selecting a wip row: backingIndex must refuse (ok=false).
	m.sel[panelCommits] = 0
	if _, ok := m.backingIndex(panelCommits); ok {
		t.Fatal("backingIndex must be ok=false on a wip row")
	}
	// Selecting the first real commit (unified index = wipCount) maps to feed[0].
	m.sel[panelCommits] = m.wipCount()
	bi, ok := m.backingIndex(panelCommits)
	if !ok || bi != 0 {
		t.Fatalf("first real commit => bi=%d ok=%v, want 0,true", bi, ok)
	}
	if m.commits[bi].Hash == "" {
		t.Fatal("backing commit must be a real feed commit")
	}
}

func TestWipRowsRender(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	u, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = u.(Model)
	m.loading = false
	m.focus = panelCommits
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "u", Unstaged: 'M'}, {Path: "s", Staged: 'M'},
	}}
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()

	out := ansi.Strip(m.View())
	if !strings.Contains(out, "Working tree") || !strings.Contains(out, "Staged") {
		t.Fatalf("expected WIP rows in render:\n%s", out)
	}
	if !strings.Contains(out, "◇") {
		t.Fatalf("expected the ◇ WIP node glyph:\n%s", out)
	}
	// graph cache parallels the unified list.
	if len(m.commitGraphRows) != m.commitsTotal() {
		t.Fatalf("graph rows=%d want commitsTotal=%d", len(m.commitGraphRows), m.commitsTotal())
	}
}
```

Add imports as needed (`tea "github.com/charmbracelet/bubbletea"`, `"github.com/charmbracelet/x/ansi"`). Drop `dirtyLoadedModel`/`repoDirOf` if unused — the tests above set status directly. Keep only what compiles.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestWipRows' -v`
Expected: FAIL — `commitList.Len` still `len(commits)`; `backingIndex` not wip-aware; no `◇`.

- [ ] **Step 3: Unify `commitList` (viewstate.go)**

Replace the `commitList` methods so the list spans `wipRows ++ commits`. `commitList` already holds `m *Model`; use it for the wip dispatch:

```go
func (l commitList) Len() int {
	if l.m == nil {
		return len(l.items)
	}
	return l.m.commitsTotal()
}
func (l commitList) Row(i int) string {
	if l.m == nil {
		return ""
	}
	return l.m.commitIdentRowAt(i, l.identW, false)
}
func (l commitList) Name(i int) string {
	if r, ok := l.wipAt(i); ok {
		return r.label()
	}
	return l.items[i-l.wip()].Subject
}
func (l commitList) Date(i int) int64 {
	if _, ok := l.wipAt(i); ok {
		return 1<<62 - 1 // wip rows are "newest": sort to the top under date sort
	}
	return l.items[i-l.wip()].UnixTime
}
func (l commitList) Key(i int) string {
	if r, ok := l.wipAt(i); ok {
		return "\x00wip-" + r.label()
	}
	return l.items[i-l.wip()].Hash
}
```

Add helpers near `commitList`:

```go
func (l commitList) wip() int {
	if l.m == nil {
		return 0
	}
	return l.m.wipCount()
}
func (l commitList) wipAt(i int) (wipRow, bool) {
	if l.m == nil {
		return wipRow{}, false
	}
	return l.m.wipRowAt(i)
}
```

Update `Haystack`/`Full`/`TextReveal` bounds checks to use the unified length and the unified helpers:

```go
func (l commitList) Haystack(i int) string {
	if l.m == nil || i >= l.Len() {
		return ""
	}
	return l.m.commitHaystackAt(i)
}
func (l commitList) Full(i int) string {
	if l.m == nil || i >= l.Len() {
		return ""
	}
	return l.m.commitIdentRowAt(i, l.identW, true)
}
func (l commitList) TextReveal(i int) string {
	if l.m == nil || i >= l.Len() {
		return ""
	}
	return l.m.commitTextRevealAt(i)
}
```

- [ ] **Step 4: Make `backingIndex` wip-aware (viewstate.go)**

```go
func (m Model) backingIndex(p panel) (int, bool) {
	idx := m.displayIndices(p)
	s := m.sel[p]
	if s < 0 || s >= len(idx) {
		return 0, false
	}
	u := idx[s]
	if p == panelCommits {
		if m.isWipRow(u) {
			return 0, false // a pseudo-row is not a real commit
		}
		return u - m.wipCount(), true
	}
	return u, true
}
```

- [ ] **Step 5: Dispatch the per-row render helpers (view.go) + the `◇` renderer (commit_ident.go)**

In `commit_ident.go`, add a WIP-row renderer and decorator:

```go
// wipNodeGlyph marks a pseudo-row's node in the graph/list (hollow, vs ● commit).
const wipNodeGlyph = "◇"

// wipIdentRow renders a pseudo-row body: "Working tree (2)" / "Staged (1)".
func (r wipRow) text() string {
	return fmt.Sprintf("%s (%d)", r.label(), r.count)
}
```

(Add `"fmt"` to `commit_ident.go` imports if not present.)

In `view.go`, make `commitIdentRowAt` dispatch on the unified index. Its current body indexes `m.commits[i]`; wrap it:

```go
func (m Model) commitIdentRowAt(i, w int, full bool) string {
	if r, ok := m.wipRowAt(i); ok {
		row := r.text()
		switch {
		case m.commitListMode:
			row = wipNodeGlyph + " " + row
		case !m.commitListMode && m.commitGraphOn() && len(m.commitGraphRows) == m.commitsTotal():
			win, _, _ := m.graphWindow(m.commitGraphRows[i])
			row = win + " " + row
		}
		return row
	}
	c := m.commits[i-m.wipCount()]
	// ... unchanged body, but every m.commits[i] below becomes c and the graph
	// branch uses m.commitGraphRows[i] (unified) and the == m.commitsTotal() guard.
	...
}
```

Update `commitHaystackAt` and `commitTextRevealAt` the same way — dispatch wip first (`commitHaystackAt` returns `r.text()` for a wip row; `commitTextRevealAt` returns `r.text()`), else index `m.commits[i-m.wipCount()]`.

Replace the four graph invariant checks (`len(m.commitGraphRows) == len(m.commits)` / `len(m.commitGraphLanes) == len(m.commits)`) at `view.go:904`, `view.go:935`, `view.go:936`, and `commit_graph_window.go:9` with `== m.commitsTotal()`.

In `commitDecoratorsRange` (view.go), dispatch the wip case at the top of the loop body (after the `ci` range guard) so a wip row gets the `◇` node color/no-lineage treatment and skips the commit-identity logic:

```go
		if r, ok := m.wipRowAt(ci); ok {
			_ = r
			// WIP rows: no lineage-ident dim; the ◇ node is part of the graph cells
			// already. Leave decos[j] nil (no per-row recolor needed for v1).
			continue
		}
		id := commitIdentOf(m.commits[ci-m.wipCount()])
		...
```

Update the remaining `m.commits[ci]` reads in that loop to `m.commits[ci-m.wipCount()]`.

- [ ] **Step 6: Prepend WIP nodes in `rebuildCommitGraph` (model.go)**

```go
func (m Model) rebuildCommitGraph() Model {
	cs := make([]commitgraph.Commit, 0, m.commitsTotal())
	// Synthetic WIP nodes chained Working tree → Staged → HEAD.
	headHash := ""
	if len(m.commits) > 0 {
		headHash = m.commits[0].Hash
	}
	for i, r := range m.wipRows {
		var parent string
		if i+1 < len(m.wipRows) {
			parent = wipSyntheticHash(m.wipRows[i+1]) // next wip row (Staged)
		} else {
			parent = headHash // last wip row parents to HEAD (may be "" in an empty repo)
		}
		var parents []string
		if parent != "" {
			parents = []string{parent}
		}
		cs = append(cs, commitgraph.Commit{Hash: wipSyntheticHash(r), Parents: parents})
	}
	for _, c := range m.commits {
		cs = append(cs, commitgraph.Commit{Hash: c.Hash, Parents: c.Parents})
	}
	rows, _ := commitgraph.Lay(cs)
	m.commitGraphRows = make([]string, len(rows))
	m.commitGraphLanes = make([]int, len(rows))
	for i, r := range rows {
		m.commitGraphRows[i] = r.Cells
		m.commitGraphLanes[i] = r.Lane
	}
	m.commitGraphScroll = m.clampScroll(m.commitGraphScroll)
	return m
}
```

Add to `wip_rows.go` a git-invalid synthetic hash (loud if it ever leaks):

```go
// wipSyntheticHash is a deliberately git-invalid id for a WIP node in the graph
// layout (contains a NUL, so it can never be mistaken for a real 40-hex SHA).
func wipSyntheticHash(r wipRow) string { return "\x00wip-" + r.label() }
```

- [ ] **Step 7: Populate `m.wipRows` on status change (model.go)**

Wherever `m.status` is assigned from a snapshot/reload (the same handlers that set `m.commits`, e.g. the `snapshotMsg`/status-refresh handlers at `model.go` ~238/258 and any targeted status refresh), set `m.wipRows = deriveWipRows(m.status)` immediately after, and ensure `rebuildCommitGraph` runs (it already does on those paths). Grep `m.status =` to find every assignment and add the derive call (centralize via a tiny helper `m = m.refreshWipRows()` if there are several):

```go
func (m Model) refreshWipRows() Model {
	m.wipRows = deriveWipRows(m.status)
	return m
}
```

- [ ] **Step 8: Run the tests**

Run: `go test ./internal/tui/ -run 'TestWipRows' -v`
Expected: PASS.

- [ ] **Step 9: Run the whole package (regression: clean tree unchanged)**

Run: `go test ./internal/tui/`
Expected: ok. (Clean-tree models have `wipCount()==0`, so every unified index equals its backing index and behavior is identical to today.)

- [ ] **Step 10: Commit**

```bash
gofmt -w internal/tui/*.go
git add internal/tui/ && git commit -m "feat(tui): unified WIP+commit indexing — list, render, graph, backingIndex

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 3: Single-select diff + status-bar label + op-row omission

**Files:**
- Modify: `internal/tui/files_view.go` — `l`/`enter` on a wip row opens its compare diff.
- Modify: `internal/tui/model.go` — the `l` handler at ~967 (currently `bi, _ := backingIndex`, ignores `ok`).
- Modify: `internal/tui/view.go` — the status-bar commit-id line (`view.go:782`) shows a wip label.
- Test: `internal/tui/wip_rows_diff_test.go` (new).

**Interfaces:**
- Consumes: `m.wipRowAt`, `openCompareFiles(left, right model.Endpoint)`, `model.EndpointWorkTree`/`EndpointIndex`/commit.
- Produces: `func (m Model) wipEndpoints(r wipRow) (left, right model.Endpoint)` — the node-vs-parent pair.

**Node-vs-parent endpoints:**
- Working tree row → left `EndpointWorkTree`, right = the Staged endpoint if a Staged row exists, else HEAD (`EndpointCommit` of `m.commits[0].Hash`). (Working tree vs index = the unstaged diff.)
- Staged row → left `EndpointIndex`, right = HEAD. (Index vs HEAD = the staged diff.)

`openCompareFiles(left, right)` renders `left ↔ right`; order left=newer/node, right=parent, matching the existing commit single-select convention.

- [ ] **Step 1: Write the failing test**

```go
func TestWipSingleSelectOpensCompare(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "u", Unstaged: 'M'}, {Path: "s", Staged: 'M'},
	}}
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()

	// Select the Staged row (unified index 1) and press l.
	m.sel[panelCommits] = 1
	u, cmd := m.Update(keyMsg("l"))
	mm := u.(Model)
	if mm.filesView == nil {
		t.Fatal("l on a wip row must open the compare files view")
	}
	if mm.filesLeft.Kind != model.EndpointIndex {
		t.Fatalf("staged row left endpoint = %v, want EndpointIndex", mm.filesLeft.Kind)
	}
	if mm.filesRight.Kind != model.EndpointCommit || mm.filesRight.Hash != m.commits[0].Hash {
		t.Fatalf("staged row right endpoint = %+v, want HEAD", mm.filesRight)
	}
	_ = cmd
}

func TestWipRowRefusesCommitOps(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "u", Unstaged: 'M'}}}
	m.wipRows = deriveWipRows(m.status)
	m.sel[panelCommits] = 0 // the Working tree wip row

	// A representative commit-op row must be unavailable on a wip row.
	if _, ok := m.commitDropRow(); ok {
		t.Fatal("Drop commit must be unavailable on a wip row")
	}
	if _, ok := m.cherryPickRow(); ok { // use the real cherry-pick row ctor name
		t.Fatal("Cherry-pick must be unavailable on a wip row")
	}
}
```

(Adjust `cherryPickRow` to the actual constructor name; pick any two `commitEditRow`/compare-marked builders that call `backingIndex(panelCommits)`.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestWip(SingleSelect|RowRefuses)' -v`
Expected: FAIL — `l` opens the commit files view (or panics on the synthetic), and the op rows may still appear.

NOTE: `TestWipRowRefusesCommitOps` likely PASSES already — the op-row builders go through `backingIndex` which now returns `ok=false` (Task 2). Keep it as a guard.

- [ ] **Step 3: Add `wipEndpoints` (wip_rows.go)**

```go
import "github.com/gigagit/gg/internal/model" // already imported

func (m Model) wipEndpoints(r wipRow) (left, right model.Endpoint) {
	head := model.Endpoint{Kind: model.EndpointCommit}
	if len(m.commits) > 0 {
		head.Hash = m.commits[0].Hash
	}
	switch r.kind {
	case wipStaged:
		return model.Endpoint{Kind: model.EndpointIndex}, head
	default: // wipWorktree
		// Working tree vs index when something is staged, else vs HEAD.
		hasStaged := false
		for _, w := range m.wipRows {
			if w.kind == wipStaged {
				hasStaged = true
			}
		}
		if hasStaged {
			return model.Endpoint{Kind: model.EndpointWorkTree}, model.Endpoint{Kind: model.EndpointIndex}
		}
		return model.Endpoint{Kind: model.EndpointWorkTree}, head
	}
}
```

- [ ] **Step 4: Handle `l`/`enter` on a wip row**

In the `l` handler (`model.go` ~967, currently `bi, _ := m.backingIndex(panelCommits)`), branch on the selected unified row before the existing commit-files path:

```go
				if u := m.commitSelUnified(); m.isWipRow(u) {
					if r, ok := m.wipRowAt(u); ok {
						left, right := m.wipEndpoints(r)
						return m.openCompareFiles(left, right)
					}
				}
				bi, ok := m.backingIndex(panelCommits)
				if !ok {
					return m, nil
				}
				c := m.commits[bi]
				...
```

Add `commitSelUnified` to `wip_rows.go` (the selected unified index, honoring sort/filter):

```go
// commitSelUnified returns the unified Commits index currently selected, or -1.
func (m Model) commitSelUnified() int {
	idx := m.displayIndices(panelCommits)
	s := m.sel[panelCommits]
	if s < 0 || s >= len(idx) {
		return -1
	}
	return idx[s]
}
```

Do the same guard in any `enter`-on-commit path that opens files / a diff for the selected commit (check the `enter` handler and `syncFilesViewToSelectedCommit`/`moveCommitUnderFilesView` in files_view.go — those already guard `ok` from `backingIndex`, so they no-op on a wip row; only the explicit `l`/`enter` entry needs the wip-diff branch).

- [ ] **Step 5: Status-bar label for a wip row (view.go ~782)**

The status-bar helper returns the selected commit's id line; for a wip row show a plain label instead. At the top of that function:

```go
	if u := m.commitSelUnified(); m.isWipRow(u) {
		if r, ok := m.wipRowAt(u); ok {
			return strings.ToLower(r.label()) + " · " + fmt.Sprintf("%d files", r.count)
		}
	}
	bi, ok := m.backingIndex(panelCommits)
	...
```

(Ensure `fmt`/`strings` are imported in that file.)

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/tui/ -run 'TestWip' -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/tui/*.go
git add internal/tui/ && git commit -m "feat(tui): WIP-row single-select diff, status-bar label, op-row guards

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 4: Docs + full race

**Files:**
- Modify: `internal/tui/help.go` — Commits-panel help note for the WIP rows.
- Modify: `CHANGELOG.md` — `[Unreleased]` → `### Added`.

- [ ] **Step 1: Help entry**

In the `h("Commits panel")` section of `internal/tui/help.go`, after the existing intro rows, add:

```go
		r("", "uncommitted work shows as ◇ Working tree / ◇ Staged rows atop the graph (only when dirty); l/enter diffs them (working tree vs index, index vs HEAD); commit-only actions are unavailable on them"),
```

- [ ] **Step 2: CHANGELOG entry**

Under `## [Unreleased]` → `### Added`:

```markdown
- **Uncommitted work shows in the Commits graph (WIP rows).** When the tree is
  dirty, the Commits panel shows `◇ Working tree (N)` and/or `◇ Staged (N)` rows
  chained above HEAD. `l`/`enter` opens their whole-tree diff (working tree vs
  index, index vs HEAD); commit-only operations are unavailable on them. (Stage 3
  of the compare-trees arc; the ◉ compare integration lands in a follow-up.)
```

- [ ] **Step 3: Full race suite**

Run: `./test.sh race`
Expected: `all green`.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/help.go CHANGELOG.md
git commit -m "docs(tui): WIP pseudo-commit rows — help + changelog

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

## Notes for the implementer

- **Pure feed invariant:** never assign WIP into `m.commits`. `m.commits` is the domain feed; HEAD is `m.commits[0]`. WIP lives in `m.wipRows` and is unified only in the display layer.
- **Loud failure:** the synthetic graph hash contains a NUL (`\x00`), so a leak into git fails immediately rather than silently.
- **Sort scatter (v1-acceptable):** under a non-default `o` sort, wip rows sort by their sentinel Date/Name and may not stay pinned at the top; the graph is already suppressed then, so this is cosmetic. Do not expand scope to pin them.
- **Verify the funnel after Task 2:** re-grep `m.commits[` for any selection-derived index that does not come from `backingIndex` (Task 2 makes that the only wip-safe path). The known non-`backingIndex` reads are render helpers (unified) and the compare-set hash loop (Stage 2).
- **Value-receiver `Model`:** helpers return the modified copy.
