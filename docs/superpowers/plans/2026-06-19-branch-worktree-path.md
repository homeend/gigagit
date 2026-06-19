# Branch Worktree Path Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** In the TUI Branches panel, show each worktree-backed branch's worktree path in `()`, replacing the `◫` glyph.

**Architecture:** A single pure-render change in `internal/tui/view.go`: `branchRows()` appends ` (<worktree-path>)` for any branch that is a worktree HEAD, via a new `worktreePathOf` lookup. Two existing tests that assert the old `◫` glyph are updated to assert the path.

**Tech Stack:** Go 1.26, Bubble Tea, `internal/tui`.

## Global Constraints

- `branchRows()` is a pure function of `m.branches` + `m.worktrees`; tests need no git.
- Path shown for **every** worktree-backed branch incl. the current; **full** path (matches `worktreeRows()`); `◫` glyph **removed**.
- Row order: `<marker><name>[ (↓N)][ (<path>)]` — behind-count before the path.
- Run `./test.sh race` before declaring done.

---

### Task 1: Show the worktree path on branch rows

**Files:**
- Modify: `internal/tui/view.go` (`branchRows()` ~line 612; remove `worktreeBranchSet()` ~line 601; add `worktreePathOf`)
- Test: `internal/tui/worktree_view_test.go` (update `TestBranchRowsShowWorktreeMarker` → path; update `TestWorktreeMarkersFireOnRealRepo`)

**Interfaces:**
- Consumes: `model.Branch{Name, IsHead, Behind}`, `model.Worktree{Path, Branch}`, `m.branches`, `m.worktrees`.
- Produces: `func (m Model) worktreePathOf(branch string) (string, bool)`; `branchRows()` now appends ` (<path>)` and no longer emits `◫`.

- [ ] **Step 1: Update the existing glyph tests to assert the path**

Replace `TestBranchRowsShowWorktreeMarker` in `internal/tui/worktree_view_test.go` with:

```go
func TestBranchRowsShowWorktreePath(t *testing.T) {
	m := Model{
		branches: []model.Branch{
			{Name: "main", IsHead: true},
			{Name: "feature/x"},
			{Name: "lonely"},
		},
		worktrees: []model.Worktree{
			{Path: "/repo", Branch: "main"},
			{Path: "/repo.worktrees/x", Branch: "feature/x"},
		},
		sel: map[panel]int{},
	}
	rows := m.branchRows()
	if !strings.Contains(rows[0], "(/repo)") {
		t.Errorf("main should show its worktree path: %q", rows[0])
	}
	if !strings.Contains(rows[1], "(/repo.worktrees/x)") {
		t.Errorf("feature/x should show its worktree path: %q", rows[1])
	}
	if strings.Contains(rows[2], "(") {
		t.Errorf("lonely is in no worktree, expected no path: %q", rows[2])
	}
	if strings.Contains(strings.Join(rows, "\n"), "◫") {
		t.Errorf("the ◫ glyph should be gone: %v", rows)
	}
}
```

And in `TestWorktreeMarkersFireOnRealRepo`, replace the `◫`-marker block (the `foundMarker` loop and its check) with:

```go
	// The checked-out branch (main) is in a worktree, so its row shows the path.
	foundPath := false
	for _, row := range m.branchRows() {
		if strings.Contains(row, m.currentWorktree) {
			foundPath = true
		}
	}
	if !foundPath {
		t.Errorf("expected the checked-out branch row to show its worktree path; rows=%v current=%q", m.branchRows(), m.currentWorktree)
	}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestBranchRowsShowWorktreePath|TestWorktreeMarkersFireOnRealRepo'`
Expected: FAIL — rows still contain `◫` and not the path.

- [ ] **Step 3: Implement**

In `internal/tui/view.go`, replace `branchRows()` with:

```go
func (m Model) branchRows() []string {
	out := make([]string, 0, len(m.branches))
	for _, b := range m.branches {
		marker := "  "
		if b.IsHead {
			marker = "* "
		}
		row := marker + b.Name
		if b.Behind > 0 {
			row += " (↓" + strconv.Itoa(b.Behind) + ")"
		}
		if path, ok := m.worktreePathOf(b.Name); ok {
			row += " (" + path + ")"
		}
		out = append(out, row)
	}
	return out
}

// worktreePathOf returns the path of the worktree that has branch checked out,
// if any (git allows a branch in at most one worktree). Includes the current
// worktree.
func (m Model) worktreePathOf(branch string) (string, bool) {
	if branch == "" {
		return "", false
	}
	for _, w := range m.worktrees {
		if w.Branch == branch {
			return w.Path, true
		}
	}
	return "", false
}
```

Then delete the now-unused `worktreeBranchSet()` function (its only caller was `branchRows`). Confirm with:

Run: `grep -rn worktreeBranchSet internal/`
Expected: no matches.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestBranchRows|TestWorktreeMarkers'`
Expected: PASS (incl. `TestBranchRowsBehindIndicator`, which has no worktrees and is unaffected).

- [ ] **Step 5: Update CHANGELOG and run the full gate**

In `CHANGELOG.md`, under `## [Unreleased]` → `### Changed` (create the heading just under `### Added` if absent), add:

```markdown
- TUI: the Branches panel now shows each worktree-backed branch's **worktree
  path** in `()` (replacing the `◫` glyph), so you can see where a branch is
  checked out — including the current worktree.
```

Run: `gofmt -l internal/tui/ && go vet ./internal/tui/ && ./test.sh race`
Expected: gofmt prints nothing; vet clean; `all green`.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/view.go internal/tui/worktree_view_test.go CHANGELOG.md
git commit -m "feat(tui): show worktree path on branch rows"
```

---

## Notes for the implementer

- `strconv` is already imported in `view.go` (the behind-indicator uses it).
- README's keybindings table describes panels, not row decorations; no README change needed.
- The `◫` glyph currently lives only at the one `branchRows` site; removing it leaves no dangling references.
