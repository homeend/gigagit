# Dynamic Branch-Indicator Gutter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render the Branches-panel status indicators (`◉` set, `*` head) in a dynamic-width LEFT gutter so the set marker is never truncated in a narrow panel.

**Architecture:** Rewrite `branchRows()` (`internal/tui/view.go`) around an ordered list of indicator functions (branch → glyph or `' '`). A column is rendered only when at least one branch yields a non-space glyph; the gutter is those columns + one separator space, then the name. `(↓N)` behind + worktree path stay on the right.

**Tech Stack:** Go 1.26, Bubble Tea TUI.

## Global Constraints

- Indicator order, left-to-right: **set `◉`** then **head `*`**.
- A column is "in play" iff some branch in `m.branches` yields a non-space glyph for it.
- Gutter = the in-play columns (one rune each) + exactly one separator space, then the name. No gutter at all when zero columns are in play.
- `◉` = a branch in `slices.Contains(m.commitScopeBranches, b.Name)`; `*` = `b.IsHead`.
- `(↓N)` behind-indicator and worktree-path suffix stay on the right, unchanged.
- The row string stays single-sourced (display + filter haystack + tooltip) — glyphs were already in it; only their position changes.
- TUI `Model` is a value receiver. Run `./test.sh race` before merge.

---

### Task 1: Dynamic gutter in `branchRows()`

**Files:**
- Modify: `internal/tui/view.go` (`branchRows()`, lines 592-612)
- Test: `internal/tui/commit_scope_test.go` (new tests; `branchesPanelModel` helper lives here)

**Interfaces:**
- Consumes: `Model.branches []model.Branch` (`model.Branch` has `Name string`, `IsHead bool`, `Behind int`); `Model.commitScopeBranches`; `Model.worktreePathOf(name) (string, bool)`; `slices.Contains`; test helper `branchesPanelModel(names ...string) Model`.
- Produces: rewritten `branchRows() []string` (same signature).

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/commit_scope_test.go`:

```go
func TestBranchRowsGutterOneColumnWhenNoSet(t *testing.T) {
	m := branchesPanelModel("main", "feat")
	m.branches[0].IsHead = true // main is head
	rows := m.branchRows()
	// No set active → 1-column gutter (head only), same as before.
	if !strings.HasPrefix(rows[0], "* main") {
		t.Fatalf("head row = %q, want '* main' prefix", rows[0])
	}
	if !strings.HasPrefix(rows[1], "  feat") {
		t.Fatalf("non-head row = %q, want '  feat' prefix", rows[1])
	}
}

func TestBranchRowsGutterTwoColumnsWhenSetActive(t *testing.T) {
	m := branchesPanelModel("main", "feat")
	m.branches[0].IsHead = true
	m.commitScopeBranches = []string{"feat"} // feat in the set
	rows := m.branchRows()
	// Set active → 2-column gutter [set][head] + separator. Names aligned at col 3.
	if !strings.HasPrefix(rows[0], " * main") { // head, not in set
		t.Fatalf("main row = %q, want ' * main' prefix", rows[0])
	}
	if !strings.HasPrefix(rows[1], "◉  feat") { // in set, not head
		t.Fatalf("feat row = %q, want '◉  feat' prefix", rows[1])
	}
}

func TestBranchRowsSetMarkerIsOnTheLeft(t *testing.T) {
	m := branchesPanelModel("feat")
	m.commitScopeBranches = []string{"feat"}
	row := m.branchRows()[0]
	// The fix: ◉ precedes the name and the row no longer ENDS with ◉.
	if strings.HasSuffix(row, "◉") {
		t.Fatalf("set marker must not be a right-hand suffix: %q", row)
	}
	dot := strings.IndexRune(row, '◉')
	name := strings.Index(row, "feat")
	if dot < 0 || dot >= name {
		t.Fatalf("◉ should precede the name: dot=%d name=%d in %q", dot, name, row)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run 'TestBranchRowsGutter|TestBranchRowsSetMarker' -v`
Expected: FAIL — current `branchRows` appends `◉` on the right, so the two-column and left-marker assertions fail.

- [ ] **Step 3: Rewrite `branchRows()`**

Replace `internal/tui/view.go` lines 592-612 with:

```go
func (m Model) branchRows() []string {
	inScope := func(b model.Branch) bool { return slices.Contains(m.commitScopeBranches, b.Name) }
	// Ordered left-to-right; each maps a branch to its glyph or ' '.
	indicators := []func(model.Branch) rune{
		func(b model.Branch) rune { // set / solo
			if inScope(b) {
				return '◉'
			}
			return ' '
		},
		func(b model.Branch) rune { // head
			if b.IsHead {
				return '*'
			}
			return ' '
		},
	}
	// A column is in play iff some branch yields a non-space glyph for it.
	active := make([]bool, len(indicators))
	for i, ind := range indicators {
		for _, b := range m.branches {
			if ind(b) != ' ' {
				active[i] = true
				break
			}
		}
	}
	out := make([]string, 0, len(m.branches))
	for _, b := range m.branches {
		gutter := make([]rune, 0, len(indicators)+1)
		for i, ind := range indicators {
			if active[i] {
				gutter = append(gutter, ind(b))
			}
		}
		if len(gutter) > 0 {
			gutter = append(gutter, ' ') // one separator before the name
		}
		row := string(gutter) + b.Name
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
```

- [ ] **Step 4: Run the new tests + the full package**

Run: `go test ./internal/tui/ -run 'TestBranchRowsGutter|TestBranchRowsSetMarker' -v && go test ./internal/tui/`
Expected: the three new tests PASS; the full `internal/tui` package is `ok` (existing branch/marker tests — e.g. the selected-set marker test, `fit_test.go`, `worktree_view_test.go` — still pass; if any asserted the old right-side `◉`, update it to the new left-gutter position, noting the change).

- [ ] **Step 5: Update the CHANGELOG**

Add to the top section of `CHANGELOG.md`:

```markdown
- **Branches panel — indicators moved to a dynamic left gutter.** The set/solo
  `◉` and head `*` markers now render in a left gutter (width adapts to how many
  indicator types are active) so the set marker is no longer truncated in a
  narrow panel.
```

- [ ] **Step 6: Run the race suite**

Run: `./test.sh race`
Expected: vet+gofmt clean, all unit packages `ok`, e2e `ok`.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/view.go internal/tui/commit_scope_test.go CHANGELOG.md
git commit -m "feat(tui): dynamic left gutter for branch indicators

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage:** Dynamic-width left gutter (Task 1 Step 3); indicator order set→head (the `indicators` slice); in-play rule (the `active` loop); `(↓N)`/path stay right (unchanged tail); single-sourced invariant (glyphs still in the row, only repositioned); tests for 1-col / 2-col / left-marker / alignment (Step 1). The detached/zero-column case is exercised implicitly (no test added — YAGNI; the `len(gutter) > 0` guard covers it and the 1-col/2-col tests pin the common cases).

**Placeholder scan:** No TBD/TODO; full code in every step. Step 4 flags that a pre-existing test may assert the old right-side `◉` and need updating — that's a real conditional lookup, not a placeholder.

**Type consistency:** `branchRows() []string` signature unchanged; `model.Branch` fields `Name`/`IsHead`/`Behind` used as they exist; `worktreePathOf`/`slices.Contains`/`strconv.Itoa` are the existing calls from the original function.
