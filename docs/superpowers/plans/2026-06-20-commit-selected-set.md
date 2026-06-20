# Multi-branch Selected Set Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `.`-menu action on the Branches panel that toggles a branch in/out of the Commits-feed scope (`commitScopeBranches`), and mark every scoped branch with `◉`.

**Architecture:** Pure TUI change. The feed scope was built list-shaped in Phase 1 (`commitScopeBranches []string`, empty = all), reloaded via `reloadFeedCmd()`, labelled by `commitScopeLabel()` (`all` / `solo: X` / `N branches`). This round adds one `actionRow` helper that mutates the slice and reloads, wires it into `availableActions`, and broadens the existing `◉` marker from "sole scope entry" to "any set member". No engine/domain/git changes.

**Tech Stack:** Go 1.26, Bubble Tea TUI, `internal/gitexec.FakeRunner` for the feed harness.

## Global Constraints

- A git verb is one invocation — N/A this round (no git/argv change).
- TUI `Model` is a value receiver; mutate the copy and return it.
- `internal/tui` never imports `internal/git` directly — reaches git via `internal/domain` (already satisfied; this plan adds no new imports).
- Tests use the `FakeRunner` feed harness (`gitexec.NewFakeRunner` + `domain.New`) — see `commit_scope_test.go` for the established shape.
- New menu action stable id: `commits-toggle`.
- Dynamic label copy, verbatim: `Add to commit view` (branch not in set) / `Remove from commit view` (branch in set).
- **Do NOT declare a package-level `contains` helper** — `worktree_popup_test.go` already defines `func contains(s, sub string) bool` in `package tui`; a second `contains` redeclares it in the test build. Use stdlib `slices.Contains` for membership reads. Keep the hand-rolled `without` (fresh allocation; `slices.Delete*` would mutate the backing array the value-receiver `Model` still shares with its prior copy).
- This feature adds **no keybinding** — it is a `.`-menu action like Phase 1's Solo. Advertise it in `help.go` only (no footer keybind hint).
- Run `./test.sh race` before merge.

---

### Task 1: The toggle menu action

**Files:**
- Modify: `internal/tui/commit_scope.go` (add `commitToggleRow` + `contains`/`without` helpers, after `commitSoloRow` at line 53)
- Modify: `internal/tui/action_menu.go:84-86` (wire `commitToggleRow` in after `commitSoloRow`)
- Test: `internal/tui/commit_scope_test.go` (add toggle tests)

**Interfaces:**
- Consumes: `Model.commitScopeBranches []string`; `Model.focus`, `panelBranches`; `Model.opsIdle() bool`; `Model.selectedBranch() (model.Branch, bool)`; `Model.reloadFeedCmd() tea.Cmd`; `actionRow{id, label string; run func(Model) (tea.Model, tea.Cmd)}`; test helpers `branchesPanelModel(names ...string) Model` and `findRow([]actionRow, id string) (actionRow, bool)`.
- Produces: `func (m Model) commitToggleRow() (actionRow, bool)` with id `commits-toggle`; unexported helper `without([]string, string) []string`. Membership reads use stdlib `slices.Contains` (no custom `contains`).

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/commit_scope_test.go`:

```go
func TestCommitToggleAddsBranch(t *testing.T) {
	m := branchesPanelModel("feat", "main")
	r, ok := findRow(availableActions(m), "commits-toggle")
	if !ok {
		t.Fatalf("toggle action missing on Branches panel")
	}
	if r.label != "Add to commit view" {
		t.Fatalf("label for unselected branch = %q, want Add to commit view", r.label)
	}
	mm, _ := r.run(m)
	m = mm.(Model)
	if len(m.commitScopeBranches) != 1 || m.commitScopeBranches[0] != "feat" {
		t.Fatalf("toggle-add should scope to [feat], got %v", m.commitScopeBranches)
	}
}

func TestCommitToggleRemovesBranch(t *testing.T) {
	m := branchesPanelModel("feat", "main")
	m.commitScopeBranches = []string{"feat", "main"}
	r, ok := findRow(availableActions(m), "commits-toggle")
	if !ok {
		t.Fatalf("toggle action missing")
	}
	if r.label != "Remove from commit view" {
		t.Fatalf("label for selected branch = %q, want Remove from commit view", r.label)
	}
	mm, _ := r.run(m)
	m = mm.(Model)
	if len(m.commitScopeBranches) != 1 || m.commitScopeBranches[0] != "main" {
		t.Fatalf("toggle-remove should leave [main], got %v", m.commitScopeBranches)
	}
}

func TestCommitToggleRemoveLastReturnsToAll(t *testing.T) {
	m := branchesPanelModel("feat")
	m.commitScopeBranches = []string{"feat"}
	r, _ := findRow(availableActions(m), "commits-toggle")
	mm, _ := r.run(m)
	m = mm.(Model)
	if len(m.commitScopeBranches) != 0 {
		t.Fatalf("removing the last branch should clear scope, got %v", m.commitScopeBranches)
	}
	if m.commitScopeLabel() != "all" {
		t.Fatalf("empty scope label = %q, want all", m.commitScopeLabel())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run TestCommitToggle -v`
Expected: FAIL — `m.commitToggleRow undefined` / `commits-toggle` row not found.

- [ ] **Step 3: Implement `commitToggleRow` + helpers**

Add `"slices"` to the imports of `internal/tui/commit_scope.go` (the import block currently holds `"context"`, the bubbletea alias `tea`, and `internal/domain`). Then append (after `commitShowAllRow`):

```go
// commitToggleRow offers "Add to commit view" / "Remove from commit view" on the
// Branches panel: add or remove the selected branch from the multi-branch
// Commits-feed scope. Removing the last branch returns the feed to all branches.
func (m Model) commitToggleRow() (actionRow, bool) {
	if m.focus != panelBranches || !m.opsIdle() {
		return actionRow{}, false
	}
	b, ok := m.selectedBranch()
	if !ok {
		return actionRow{}, false
	}
	in := slices.Contains(m.commitScopeBranches, b.Name)
	label := "Add to commit view"
	if in {
		label = "Remove from commit view"
	}
	return actionRow{
		id:    "commits-toggle",
		label: label,
		run: func(m Model) (tea.Model, tea.Cmd) {
			if in {
				m.commitScopeBranches = without(m.commitScopeBranches, b.Name)
			} else {
				m.commitScopeBranches = append(append([]string(nil), m.commitScopeBranches...), b.Name)
			}
			return m, m.reloadFeedCmd()
		},
	}, true
}

// without returns a new slice with the first occurrence of s removed, preserving
// the order of the remaining elements. A fresh allocation is deliberate: the
// value-receiver Model shares its slice backing with the prior copy, so an
// in-place delete would corrupt it.
func without(ss []string, s string) []string {
	out := make([]string, 0, len(ss))
	for _, x := range ss {
		if x == s {
			continue
		}
		out = append(out, x)
	}
	return out
}
```

- [ ] **Step 4: Wire into `availableActions`**

In `internal/tui/action_menu.go`, after the `commitSoloRow` block (lines 84-86) and before the `commitShowAllRow` block:

```go
	if r, ok := m.commitSoloRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.commitToggleRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.commitShowAllRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run TestCommitToggle -v`
Expected: PASS (all three).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/commit_scope.go internal/tui/action_menu.go internal/tui/commit_scope_test.go
git commit -m "feat(tui): toggle branches in/out of the Commits-feed scope

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: Mark every scoped branch with `◉`

**Files:**
- Modify: `internal/tui/view.go:623-625` (broaden the `◉` marker condition in `branchRows`)
- Test: `internal/tui/commit_scope_test.go` (add a marker test)

**Interfaces:**
- Consumes: `Model.branchRows() []string`; `Model.commitScopeBranches`; stdlib `slices.Contains`; `branchesPanelModel(names ...string) Model`.
- Produces: no new exported symbols.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/commit_scope_test.go`:

```go
func TestBranchRowsMarkAllScopedBranches(t *testing.T) {
	m := branchesPanelModel("a", "b", "c")
	m.commitScopeBranches = []string{"a", "c"}
	rows := m.branchRows()
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	if !strings.Contains(rows[0], "◉") {
		t.Fatalf("row a should be marked: %q", rows[0])
	}
	if strings.Contains(rows[1], "◉") {
		t.Fatalf("row b should NOT be marked: %q", rows[1])
	}
	if !strings.Contains(rows[2], "◉") {
		t.Fatalf("row c should be marked: %q", rows[2])
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run TestBranchRowsMarkAllScopedBranches -v`
Expected: FAIL — row `c` unmarked (current code only marks when the set is exactly `[b.Name]`).

- [ ] **Step 3: Broaden the marker condition**

In `internal/tui/view.go`, replace the marker condition in `branchRows` (lines 623-625). Ensure `"slices"` is in the file's import block (add it if absent):

```go
		if slices.Contains(m.commitScopeBranches, b.Name) {
			row += " ◉" // included in the Commits feed scope
		}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/tui/ -run TestBranchRowsMarkAllScopedBranches -v`
Expected: PASS.

- [ ] **Step 5: Run the full TUI package to confirm no regression**

Run: `go test ./internal/tui/`
Expected: `ok` — existing solo tests (`TestCommitSoloSetsAndClearsScope`, `TestCommitSoloReloadEndToEnd`) still pass; a size-1 set still marks the soloed branch (`contains` covers it).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/view.go internal/tui/commit_scope_test.go
git commit -m "feat(tui): mark every scoped branch in the Commits-feed set

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: End-to-end reload + docs

**Files:**
- Test: `internal/tui/commit_scope_test.go` (end-to-end toggle→reload→paint)
- Modify: `CHANGELOG.md` (top entry)
- Modify: `internal/tui/help.go` and the Commits-panel context footer (advertise the new action)

**Interfaces:**
- Consumes: `gitexec.NewFakeRunner()`, `f.SetResponse("git log", ...)`, `domain.New(&git.Repo{Runner: f})`, `svc.CommitFeed()`, `Model.Update`, `Model.commitScopeLabel()`; the `commits-toggle` row from Task 1.
- Produces: no new exported symbols.

- [ ] **Step 1: Write the end-to-end test**

Add to `internal/tui/commit_scope_test.go`:

```go
// TestCommitToggleReloadEndToEnd drives toggle → reload cmd → commitsReloadedMsg
// → Update, and confirms the multi-branch label paints.
func TestCommitToggleReloadEndToEnd(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: "h1\x1f\x1fAda\x1f0\x1fsubject\x1fHEAD -> feat\n"})
	svc := domain.New(&git.Repo{Runner: f})
	m := branchesPanelModel("feat", "main")
	m.svc = svc
	m.feed = svc.CommitFeed()
	m.commitScopeBranches = []string{"main"} // pre-existing one-branch set

	r, ok := findRow(availableActions(m), "commits-toggle")
	if !ok {
		t.Fatal("toggle row missing")
	}
	if r.label != "Add to commit view" {
		t.Fatalf("feat not in set → label = %q, want Add to commit view", r.label)
	}
	mm, cmd := r.run(m)
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("toggle should return a reload cmd")
	}
	msg := cmd()
	mm, _ = m.Update(msg)
	m = mm.(Model)
	if len(m.commitScopeBranches) != 2 {
		t.Fatalf("set should now hold 2 branches, got %v", m.commitScopeBranches)
	}
	if m.commitScopeLabel() != "2 branches" {
		t.Fatalf("scope label = %q, want 2 branches", m.commitScopeLabel())
	}
	if len(m.commits) != 1 || m.commits[0].Hash != "h1" {
		t.Fatalf("after toggle reload, commits = %+v", m.commits)
	}
}
```

- [ ] **Step 2: Run it to verify it passes**

Run: `go test ./internal/tui/ -run TestCommitToggleReloadEndToEnd -v`
Expected: PASS (the implementation from Tasks 1-2 already satisfies it; this test locks the full chain).

- [ ] **Step 3: Update the CHANGELOG**

Add to the top "Unreleased"/latest section of `CHANGELOG.md`:

```markdown
- **Commits panel — multi-branch selected set.** The `.` menu on a Branches row
  now offers **Add to commit view** / **Remove from commit view** to scope the
  Commits feed to a custom set of branches (alongside one-tap Solo and Show all).
  Every branch in the set is marked `◉`.
```

- [ ] **Step 4: Advertise in help (help.go only — no footer keybind)**

This action has no keybinding (it lives in the `.` menu, exactly like Phase 1's Solo), so there is **no footer hint** — Solo has none either; mirror that precedent. Update the existing Solo description rows in `internal/tui/help.go` to mention the toggle:

- Line 49 (Branches panel `.`-menu description) — extend it to:
  ```go
  r("", "Solo this branch (.-menu): scope the Commits panel to this branch (re-run to un-solo); Add/Remove from commit view builds a multi-branch set; Show all branches clears it"),
  ```
- Line 48 (`.` row summary) — append `/ Add to commit view`:
  ```go
  r(".", "rename branch / copy branch name / Solo this branch / Add to commit view (.-menu)"),
  ```

Keep copy density consistent with surrounding rows. No change to footer files.

- [ ] **Step 5: Run the full suite**

Run: `./test.sh`
Expected: vet+gofmt clean, unit tests `ok`, e2e `ok` (no e2e scenario added this round).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/commit_scope_test.go internal/tui/help.go CHANGELOG.md
git commit -m "test(tui): e2e toggle reload; docs + help for the selected set

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage:**
- Spec §1 (state/data flow, no new plumbing) — Tasks 1 & 3 reuse `commitScopeBranches` + `reloadFeedCmd`; no engine/domain/git change. ✓
- Spec §2 (toggle action, dynamic label, wiring order Solo→toggle→Show all, id `commits-toggle`) — Task 1. ✓
- Spec §3 (marker on every set member via `contains`) — Task 2. ✓
- Spec §4 (header label unchanged; size-1 reads `solo: X`) — verified by `TestCommitToggleRemoveLastReturnsToAll` (all) and the existing `TestCommitScopeLabel` (solo: X); no code change needed. ✓
- Spec testing list (toggle add/remove, remove-last→all, marker, e2e) — Tasks 1-3 map 1:1. ✓
- Spec "out of scope" (navigation, lane color, header listing) — none implemented. ✓

**Placeholder scan:** No TBD/TODO; every code step shows full code, including the help.go rows in Task 3 Step 4 (now literal). No footer change (no keybinding).

**Type consistency:** `without([]string,string) []string` defined in Task 1, consumed there; membership uses stdlib `slices.Contains` in both Task 1 and Task 2 (no custom `contains`, which would collide with `worktree_popup_test.go`'s `contains(s, sub string)`). `commits-toggle` id consistent across Tasks 1 & 3. `actionRow.run` signature `func(Model) (tea.Model, tea.Cmd)` matches the existing `commitSoloRow`. `commitScopeLabel()` outputs (`all` / `solo: X` / `N branches`) used verbatim in assertions.
