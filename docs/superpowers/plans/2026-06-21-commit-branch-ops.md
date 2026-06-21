# Rename / delete branch from a Commits-panel tip — Plan

> **For agentic workers:** TDD. Steps use `- [ ]`.

**Goal:** Commits-panel `.` menu offers Rename/Delete branch for each local tip
of the selected commit, reusing `renameBranchPopup` + `engine.DeleteBranch`.

**Architecture:** One new TUI accessor `commitBranchRows() []actionRow` + menu
wiring. No engine/git/CLI change.

## Global Constraints

- TUI-only; reuse `renameBranchPopup{old, name}` and `engine.DeleteBranch{Name}`.
- Tip = `model.Ref{Kind: model.RefLocal}` on `m.commits[bi].Refs`; `Ref.Head`
  marks the checked-out branch.
- Rename: every local tip (incl. HEAD). Delete: every local tip except HEAD.
- Gate: `m.focus == panelCommits && m.opsIdle()`, commit via `backingIndex`.
- Commit trailers: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` +
  `Claude-Session:`.

---

### Task 1: `commitBranchRows` accessor + wiring

**Files:** Modify `internal/tui/commit_scope.go`,
`internal/tui/action_menu.go`; Test `internal/tui/commit_branch_ops_test.go`.

**Produces:** `func (m Model) commitBranchRows() []actionRow`.

- [ ] **Step 1: Failing tests.** Build a Commits-panel Model with `m.commits`
  carrying `Refs`. Use the existing commit-row test fixture pattern (look at
  `commit_scope_test.go` for how `m.commits` + `m.focus = panelCommits` +
  `m.sel[panelCommits]` are set up; reuse its helper if present).

```go
func localRef(name string, head bool) model.Ref {
	return model.Ref{Name: name, Kind: model.RefLocal, Head: head}
}

func commitBranchModel(refs []model.Ref) Model {
	m := New(nil)
	m.width, m.height = 80, 30
	m.loading = false
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "abc1234", Subject: "x", Refs: refs}}
	m.sel[panelCommits] = 0
	return m
}

func ids(rows []actionRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.id)
	}
	return out
}

func TestCommitBranchRowsNonHeadTip(t *testing.T) {
	m := commitBranchModel([]model.Ref{localRef("feature", false)})
	rows := m.commitBranchRows()
	got := ids(rows)
	if len(got) != 2 || got[0] != "rename-branch" || got[1] != "delete-branch" {
		t.Fatalf("rows = %v", rowsLabels(rows))
	}
	if rows[0].label != "Rename branch feature" || rows[1].label != "Delete branch feature" {
		t.Fatalf("labels = %v", rowsLabels(rows))
	}
}

func TestCommitBranchRowsHeadTipNoDelete(t *testing.T) {
	m := commitBranchModel([]model.Ref{localRef("main", true)})
	got := ids(m.commitBranchRows())
	if len(got) != 1 || got[0] != "rename-branch" {
		t.Fatalf("head tip should offer rename only, got %v", got)
	}
}

func TestCommitBranchRowsTwoTips(t *testing.T) {
	m := commitBranchModel([]model.Ref{localRef("main", true), localRef("topic", false)})
	got := ids(m.commitBranchRows())
	// main → rename; topic → rename + delete.
	var rename, del int
	for _, id := range got {
		switch id {
		case "rename-branch":
			rename++
		case "delete-branch":
			del++
		}
	}
	if rename != 2 || del != 1 {
		t.Fatalf("want 2 rename + 1 delete, got %v", got)
	}
}

func TestCommitBranchRowsNonTip(t *testing.T) {
	if rows := commitBranchModel(nil).commitBranchRows(); rows != nil {
		t.Fatalf("no rows for a non-tip commit, got %v", ids(rows))
	}
	// Remote-only ref → no rows.
	m := commitBranchModel([]model.Ref{{Name: "origin/x", Kind: model.RefRemote}})
	if rows := m.commitBranchRows(); rows != nil {
		t.Fatalf("remote ref should yield no rows, got %v", ids(rows))
	}
}

func TestCommitBranchRowsGating(t *testing.T) {
	m := commitBranchModel([]model.Ref{localRef("feature", false)})
	m.focus = panelBranches
	if m.commitBranchRows() != nil {
		t.Fatal("no rows off the Commits panel")
	}
	m.focus = panelCommits
	m.running = true
	if m.commitBranchRows() != nil {
		t.Fatal("no rows while running")
	}
}

func TestAvailableActionsIncludesCommitBranchRows(t *testing.T) {
	m := commitBranchModel([]model.Ref{localRef("feature", false)})
	var found bool
	for _, r := range availableActions(m) {
		if r.id == "rename-branch" {
			found = true
		}
	}
	if !found {
		t.Fatal("availableActions missing rename-branch on a tip commit")
	}
}

func rowsLabels(rows []actionRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.label)
	}
	return out
}
```

- [ ] **Step 2: Run — fail.** `go test ./internal/tui/ -run 'TestCommitBranchRows|TestAvailableActionsIncludesCommitBranchRows'` → undefined.

- [ ] **Step 3: Implement** `commitBranchRows` in `commit_scope.go` (the body is
  in the spec verbatim). Ensure `engine` + `model` are imported (commit_scope.go
  already imports them).

- [ ] **Step 4: Wire** into `availableActions` (action_menu.go), after the
  `rewordRow` appender:

```go
	out = append(out, m.commitBranchRows()...)
```

- [ ] **Step 5: Run — pass.** Same test command → PASS; then full `go test ./internal/tui/`.

- [ ] **Step 6: Commit.** `feat(tui): rename/delete branch from a Commits-panel tip commit`

---

### Task 2: Docs

**Files:** Modify `internal/tui/help.go`, `CHANGELOG.md`.

- [ ] **Step 1: help.go** — extend the Commits-panel `.`-menu help line (around
  the existing `rename branch / … (.-menu)` entry) to mention rename/delete on a
  tip commit. Match the existing wording; update any help test that asserts text.

- [ ] **Step 2: CHANGELOG** — `### Added` bullet: rename/delete a branch from its
  tip commit in the Commits panel via the `.` menu (reuses the rename popup +
  delete-branch confirm; one pair per local tip; delete hidden for the current
  branch). TUI-only.

- [ ] **Step 3: Run help tests.** `go test ./internal/tui/ -run TestHelp` → PASS.

- [ ] **Step 4: Commit.** `docs: changelog + help for commit-tip branch rename/delete`

---

## Final verification

- [ ] `./test.sh race` green.
- [ ] `gofmt -l internal/ | head` empty.
- [ ] Merge to main; verify merged tree; clean up worktree; update memory.

## Self-review notes

- Spec coverage: accessor + wiring (T1), docs (T2). Backends untouched.
- Type consistency: `commitBranchRows`, `renameBranchPopup{old,name}`,
  `engine.DeleteBranch{Name}` used as defined.
