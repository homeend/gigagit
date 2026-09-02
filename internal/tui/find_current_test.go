package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// f on the Branches tab jumps the cursor to the checked-out branch; on the
// Worktrees tab to the worktree gg is running in. The viewport follows the
// cursor (windowStart), so the row is on screen after the press.
func findModel() Model {
	m := footerModel()
	m.branches = []model.Branch{
		{Name: "feat/a"},
		{Name: "feat/b"},
		{Name: "main", IsHead: true},
		{Name: "feat/c"},
	}
	m.worktrees = []model.Worktree{
		{Path: "/repo/wt-x", Branch: "feat/x"},
		{Path: "/repo", Branch: "main"},
	}
	m.currentWorktree = "/repo"
	return m
}

func TestFindCurrentBranchMovesCursor(t *testing.T) {
	t.Parallel()
	m := findModel()
	m.focus = panelBranches
	m.sel[panelBranches] = 0
	m = press(t, m, "f")
	if got := m.rowKeyAt(panelBranches, m.sel[panelBranches]); got != "main" {
		t.Fatalf("cursor on %q after f, want main (sel=%d)", got, m.sel[panelBranches])
	}
	if m.statusMsg != "" {
		t.Errorf("unexpected status: %q", m.statusMsg)
	}
}

func TestFindCurrentBranchHonorsSort(t *testing.T) {
	t.Parallel()
	m := findModel()
	m.focus = panelBranches
	m.sortModes[panelBranches] = sortNameDesc
	m = press(t, m, "f")
	if got := m.rowKeyAt(panelBranches, m.sel[panelBranches]); got != "main" {
		t.Fatalf("cursor on %q under name-desc sort, want main", got)
	}
}

func TestFindCurrentBranchFilteredOutTellsWhy(t *testing.T) {
	t.Parallel()
	m := findModel()
	m.focus = panelBranches
	m.sel[panelBranches] = 1
	m.filterPanel = panelBranches
	m.filterQuery = "feat"
	m = press(t, m, "f")
	if m.sel[panelBranches] != 1 {
		t.Errorf("cursor moved to %d although main is filtered out", m.sel[panelBranches])
	}
	if !strings.Contains(m.statusMsg, "main") || !strings.Contains(m.statusMsg, "filter") {
		t.Errorf("status must name the branch and the filter: %q", m.statusMsg)
	}
}

func TestFindCurrentBranchDetachedHead(t *testing.T) {
	t.Parallel()
	m := findModel()
	m.focus = panelBranches
	for i := range m.branches {
		m.branches[i].IsHead = false
	}
	m.status.Branch = ""
	m.sel[panelBranches] = 3
	m = press(t, m, "f")
	if m.sel[panelBranches] != 3 {
		t.Errorf("cursor moved on detached HEAD")
	}
	if !strings.Contains(m.statusMsg, "detached") {
		t.Errorf("status must say HEAD is detached: %q", m.statusMsg)
	}
}

func TestFindCurrentWorktreeMovesCursor(t *testing.T) {
	t.Parallel()
	m := findModel()
	m.focus = panelWorktrees
	m.sel[panelWorktrees] = 0
	m = press(t, m, "f")
	if got := m.rowKeyAt(panelWorktrees, m.sel[panelWorktrees]); got != "/repo" {
		t.Fatalf("cursor on %q after f, want /repo", got)
	}
}

// Remotes keeps f = fetch: the find binding must not claim the key there.
func TestFindCurrentNotOnRemotes(t *testing.T) {
	t.Parallel()
	m := findModel()
	m.focus = panelRemotes
	if m.canFindCurrent() {
		t.Error("canFindCurrent must be false on the Remotes tab (f is fetch there)")
	}
}

func TestFindCurrentFooterHint(t *testing.T) {
	t.Parallel()
	m := findModel()
	m.loading = false
	m.focus = panelBranches
	if line := m.footerLine(); !strings.Contains(line, "[f]ind current") {
		t.Errorf("Branches footer lacks [f]ind current: %q", line)
	}
	m.focus = panelWorktrees
	if line := m.footerLine(); !strings.Contains(line, "[f]ind current") {
		t.Errorf("Worktrees footer lacks [f]ind current: %q", line)
	}
	m.focus = panelRemotes
	if line := m.footerLine(); strings.Contains(line, "[f]ind current") {
		t.Errorf("Remotes footer must not advertise [f]ind current: %q", line)
	}
}
