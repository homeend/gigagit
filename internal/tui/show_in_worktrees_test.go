package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// showInWorktreesModel: Branches tab focused on "feature", which is checked
// out in a linked worktree; "main" is the current worktree's branch and
// "loose" has no worktree at all.
func showInWorktreesModel() Model {
	m := branchMergeModel()
	m.branches = append(m.branches, model.Branch{Name: "loose", Hash: "0ff1ce0"})
	m.worktrees = []model.Worktree{
		{Path: "/repo", Branch: "main"},
		{Path: "/repo-wt/other", Branch: "other"},
		{Path: "/repo-wt/feature", Branch: "feature"},
	}
	return m
}

func TestShowInWorktreesRowOffered(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		sel  int
		want bool
	}{
		{0, true},  // feature: a linked worktree
		{1, true},  // main: the current worktree counts too
		{2, false}, // loose: no worktree
	} {
		m := showInWorktreesModel()
		m.sel[panelBranches] = tc.sel
		if got := ids(availableActions(m))["show-in-worktrees"]; got != tc.want {
			t.Errorf("branch %q: show-in-worktrees offered = %v, want %v", m.branches[tc.sel].Name, got, tc.want)
		}
	}
}

func TestShowInWorktreesRowAbsentOffBranchesTab(t *testing.T) {
	t.Parallel()
	m := showInWorktreesModel()
	m.focus = panelRemotes
	if ids(availableActions(m))["show-in-worktrees"] {
		t.Fatal("show-in-worktrees must not leak off the Branches tab")
	}
}

// runShowInWorktrees runs the row for the selected branch and returns the
// resulting Model.
func runShowInWorktrees(t *testing.T, m Model) Model {
	t.Helper()
	row, ok := rowByID(availableActions(m), "show-in-worktrees")
	if !ok {
		t.Fatal("show-in-worktrees row not offered")
	}
	nm, _ := row.run(m)
	return nm.(Model)
}

func TestShowInWorktreesSelectsTheWorktree(t *testing.T) {
	t.Parallel()
	m := runShowInWorktrees(t, showInWorktreesModel())
	if m.activeLeftTab != panelWorktrees || m.focus != panelWorktrees {
		t.Fatalf("tab/focus = %v/%v, want Worktrees", m.activeLeftTab, m.focus)
	}
	w, ok := m.selectedWorktree()
	if !ok || w.Branch != "feature" {
		t.Fatalf("selected worktree = %+v (ok=%v), want feature's", w, ok)
	}
}

// A / filter on the Worktrees list that hides the target is cleared, so the
// row can be selected; a filter bound to another panel is left alone.
func TestShowInWorktreesClearsHidingFilter(t *testing.T) {
	t.Parallel()
	m := showInWorktreesModel()
	m.filterPanel, m.filterQuery = panelWorktrees, "other"
	m = runShowInWorktrees(t, m)
	if m.filterQuery != "" {
		t.Fatalf("Worktrees filter not cleared: %q", m.filterQuery)
	}
	if w, ok := m.selectedWorktree(); !ok || w.Branch != "feature" {
		t.Fatalf("selected worktree = %+v (ok=%v), want feature's", w, ok)
	}
}

func TestShowInWorktreesKeepsOtherPanelFilter(t *testing.T) {
	t.Parallel()
	m := showInWorktreesModel()
	m.filterPanel, m.filterQuery = panelBranches, "feat"
	m = runShowInWorktrees(t, m)
	if m.filterQuery != "feat" || m.filterPanel != panelBranches {
		t.Fatalf("Branches filter disturbed: %v %q", m.filterPanel, m.filterQuery)
	}
	if w, ok := m.selectedWorktree(); !ok || w.Branch != "feature" {
		t.Fatalf("selected worktree = %+v (ok=%v), want feature's", w, ok)
	}
}

// The worktree list refreshed between opening the menu and running the row:
// the branch is no longer checked out anywhere. Status notice, tab unchanged.
func TestShowInWorktreesGoneSinceMenuOpened(t *testing.T) {
	t.Parallel()
	m := showInWorktreesModel()
	row, ok := rowByID(availableActions(m), "show-in-worktrees")
	if !ok {
		t.Fatal("show-in-worktrees row not offered")
	}
	m.worktrees = m.worktrees[:2]
	nm, _ := row.run(m)
	got := nm.(Model)
	if got.activeLeftTab == panelWorktrees {
		t.Fatal("tab switched to Worktrees for a vanished worktree")
	}
	if got.statusMsg == "" {
		t.Fatal("no status notice for a vanished worktree")
	}
}
