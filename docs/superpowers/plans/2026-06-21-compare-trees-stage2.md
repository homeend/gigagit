# Compare Trees — Stage 2 Implementation Plan (Mark & compare, commit↔commit)

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans. Steps use `- [ ]`.

**Goal:** Mark a commit with the existing `m` key, then on another commit pick `.`-menu **Compare with marked commit** to open the files view as a commit↔commit whole-tree diff.

**Architecture:** Reuse Stage 1's `openCompareFiles(left, right model.Endpoint)` and the existing single-mark machinery (`m.mark`, `markAlive`, the marker glyph). A new `.`-menu row appears when a live commit mark exists on a *different* row; it orders the two commits older→newer by feed index (the feed is newest-first `--date-order`, so a larger `m.commits` index is older) and opens the comparison.

**Tech Stack:** Go 1.26, Bubble Tea.

## Global Constraints

- Reuse `openCompareFiles` (Stage 1, `internal/tui/files_view.go`) — no new git/domain/engine code.
- `commitList.Key(i)` is the commit **hash**, so `m.mark.key` is the marked commit's hash directly.
- Order endpoints older→newer by `m.commits` index (newest-first feed → older = larger index).
- Advertise the action in `help.go` (the marker glyph already gives discoverability on the marked row).
- TDD, real git via `loadedModel`. Verify test exit explicitly (no `| tail`).
- Branch `compare-mark` (worktree); human merges.

---

### Task 1: `commitCompareMarkedRow` + registration

**Files:**
- Modify: `internal/tui/commit_scope.go` (add the row func near `commitCompareWorktreeRow`)
- Modify: `internal/tui/action_menu.go` (register in the `panelCommits` block, next to the other compare rows)
- Test: `internal/tui/compare_mark_test.go`

**Interfaces:**
- Consumes: `m.mark`, `m.markAlive()`, `m.backingIndex(panelCommits)`, `m.commits`, `openCompareFiles`, `shortHash`.
- Produces: `func (m Model) commitCompareMarkedRow() (actionRow, bool)`.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/compare_mark_test.go`:

```go
package tui

import "testing"

func TestCommitCompareMarkedRow(t *testing.T) {
	m := loadedModel(t)
	if len(m.commits) < 2 {
		t.Skip("need two commits")
	}
	m.focus = panelCommits

	// No mark yet → no row.
	if _, ok := m.commitCompareMarkedRow(); ok {
		t.Fatal("row must be absent with no mark")
	}

	// Mark commit[1] (older), select commit[0] (newer).
	m.mark = &markState{panel: panelCommits, key: m.commits[1].Hash, display: m.commits[1].Hash}
	m.sel[panelCommits] = 0
	r, ok := m.commitCompareMarkedRow()
	if !ok {
		t.Fatal("row must be present with a mark on another commit")
	}
	u, cmd := r.run(m)
	mm := u.(Model)
	if !mm.filesCompare {
		t.Fatal("running the row must open compare mode")
	}
	// older→newer: left = commit[1] (older), right = commit[0] (newer)
	if mm.filesLeft.Hash != m.commits[1].Hash || mm.filesRight.Hash != m.commits[0].Hash {
		t.Fatalf("endpoints = %s ↔ %s, want %s ↔ %s",
			mm.filesLeft.Hash, mm.filesRight.Hash, m.commits[1].Hash, m.commits[0].Hash)
	}
	if cmd == nil {
		t.Fatal("expected a load command")
	}
}

func TestCommitCompareMarkedRowOrdersByFeed(t *testing.T) {
	m := loadedModel(t)
	if len(m.commits) < 2 {
		t.Skip("need two commits")
	}
	m.focus = panelCommits
	// Mark the NEWER commit[0], select the OLDER commit[1]: still older→newer.
	m.mark = &markState{panel: panelCommits, key: m.commits[0].Hash, display: m.commits[0].Hash}
	m.sel[panelCommits] = 1
	r, _ := m.commitCompareMarkedRow()
	u, _ := r.run(m)
	mm := u.(Model)
	if mm.filesLeft.Hash != m.commits[1].Hash || mm.filesRight.Hash != m.commits[0].Hash {
		t.Fatalf("endpoints = %s ↔ %s, want older→newer %s ↔ %s",
			mm.filesLeft.Hash, mm.filesRight.Hash, m.commits[1].Hash, m.commits[0].Hash)
	}
}

func TestCommitCompareMarkedRowSameCommitAbsent(t *testing.T) {
	m := loadedModel(t)
	if len(m.commits) == 0 {
		t.Skip("no commits")
	}
	m.focus = panelCommits
	m.mark = &markState{panel: panelCommits, key: m.commits[0].Hash, display: m.commits[0].Hash}
	m.sel[panelCommits] = 0
	if _, ok := m.commitCompareMarkedRow(); ok {
		t.Fatal("row must be absent when the mark equals the selection")
	}
}
```

- [ ] **Step 2: Run, verify it fails**

Run: `cd /mnt/t/others/gg-compare-mark && go test ./internal/tui/ -run TestCommitCompareMarkedRow -v`
Expected: FAIL — `m.commitCompareMarkedRow undefined`.

- [ ] **Step 3: Implement the row**

In `internal/tui/commit_scope.go`, add after `commitCompareStagedRow`:

```go
// commitCompareMarkedRow offers "Compare with marked commit" when a commit is
// marked (m key) on a different row: opens the files view as a commit↔commit
// whole-tree diff, ordered older→newer by feed position (the feed is
// newest-first, so a larger m.commits index is the older commit).
func (m Model) commitCompareMarkedRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	if m.mark == nil || m.mark.panel != panelCommits || !m.markAlive() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	sel := m.commits[bi].Hash
	marked := m.mark.key
	if sel == marked {
		return actionRow{}, false
	}
	older, newer := orderByFeed(m.commits, marked, sel)
	return actionRow{
		id:    "commit-compare-marked",
		label: "Compare with marked commit (" + shortHash(marked) + ")",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openCompareFiles(
				model.Endpoint{Kind: model.EndpointCommit, Hash: older},
				model.Endpoint{Kind: model.EndpointCommit, Hash: newer})
		},
	}, true
}

// orderByFeed returns (older, newer) for two hashes by their position in the
// newest-first commit feed: the one at the larger index is older. A hash absent
// from commits sorts as newest (index -1 treated as before everything), but
// callers only pass loaded hashes.
func orderByFeed(commits []model.Commit, a, b string) (older, newer string) {
	ia, ib := -1, -1
	for i := range commits {
		switch commits[i].Hash {
		case a:
			ia = i
		case b:
			ib = i
		}
	}
	if ia >= ib { // a is older (or equal/absent-b): a is the older side
		return a, b
	}
	return b, a
}
```

(`internal/tui/commit_scope.go` already imports `model`; `shortHash` lives in `files_view.go`, same package.)

- [ ] **Step 4: Register the row**

In `internal/tui/action_menu.go`, in the `panelCommits` block right after the `commitCompareStagedRow` registration:

```go
	if r, ok := m.commitCompareMarkedRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 5: Run, verify it passes**

Run: `cd /mnt/t/others/gg-compare-mark && go test ./internal/tui/ -run TestCommitCompareMarkedRow -v`
Expected: PASS (all three).

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gg-compare-mark
git add internal/tui/commit_scope.go internal/tui/action_menu.go internal/tui/compare_mark_test.go
git commit -m "feat(tui): . menu — compare a commit with the marked commit

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 2: Help + CHANGELOG + gate

**Files:**
- Modify: `internal/tui/help.go` (Commits panel `.`-menu line; mention the `m`-mark flow)
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Help**

In `internal/tui/help.go`, extend the Commits-panel `.`-menu list to include `Compare with marked commit`, and add a line:

```go
		r("", "Compare with marked commit (.-menu): mark a commit with m, move to another, then this diffs the two commits (older→newer) as a whole-tree files view"),
```

- [ ] **Step 2: CHANGELOG**

Under `## [Unreleased]` → `### Added`, extend the comparison entry (or add a line):

```markdown
- **Compare two commits.** Mark a commit with `m`, move to another, and the
  Commits `.` menu offers *Compare with marked commit* — a whole-tree diff
  between the two (ordered older→newer). Stage 2 of commit comparison.
```

- [ ] **Step 3: Format + vet + full race gate**

```bash
cd /mnt/t/others/gg-compare-mark
gofmt -l internal/ cmd/
go vet ./...
./test.sh race
```
Expected: `gofmt` silent, `vet` exit 0, `./test.sh race` → `all green` exit 0 (read the status directly, no pipe).

- [ ] **Step 4: Commit**

```bash
cd /mnt/t/others/gg-compare-mark
git add internal/tui/help.go CHANGELOG.md
git commit -m "docs: compare two commits via the marked-commit . menu action

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

## Self-Review

- Spec entry point B (mark, then compare; commit↔commit) → Task 1. ✅
- Reuses Stage 1 `openCompareFiles` + existing mark glyph; no engine/git/domain change. ✅
- Older→newer ordering is explicit and tested both mark-orders (Task 1 tests). ✅
- Same-commit guard tested. ✅
- Names consistent: `commitCompareMarkedRow`, `orderByFeed`, `openCompareFiles`, `model.Endpoint`. ✅
