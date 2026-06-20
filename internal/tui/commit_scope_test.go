package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
)

func branchesPanelModel(names ...string) Model {
	m := footerModel()
	m.branches = nil // overwrite footerModel's seed so sel 0 = names[0]
	for _, n := range names {
		m.branches = append(m.branches, model.Branch{Name: n})
	}
	m.focus = panelBranches
	m.sel[panelBranches] = 0
	return m
}

func TestCommitSoloSetsAndClearsScope(t *testing.T) {
	m := branchesPanelModel("feat", "main")
	r, ok := findRow(availableActions(m), "commits-solo")
	if !ok {
		t.Fatalf("Solo this branch missing on Branches panel")
	}
	mm, _ := r.run(m)
	m = mm.(Model)
	if len(m.commitScopeBranches) != 1 || m.commitScopeBranches[0] != "feat" {
		t.Fatalf("solo should scope to feat, got %v", m.commitScopeBranches)
	}
	// Re-solo the same branch → un-solo (back to all).
	r2, _ := findRow(availableActions(m), "commits-solo")
	mm, _ = r2.run(m)
	m = mm.(Model)
	if len(m.commitScopeBranches) != 0 {
		t.Fatalf("re-solo should clear scope, got %v", m.commitScopeBranches)
	}
}

func TestCommitShowAllVisibilityAndClear(t *testing.T) {
	m := branchesPanelModel("feat")
	if _, ok := findRow(availableActions(m), "commits-showall"); ok {
		t.Fatalf("Show all should be absent in all-mode")
	}
	m.commitScopeBranches = []string{"feat"}
	r, ok := findRow(availableActions(m), "commits-showall")
	if !ok {
		t.Fatalf("Show all should be present when scoped")
	}
	mm, _ := r.run(m)
	m = mm.(Model)
	if len(m.commitScopeBranches) != 0 {
		t.Fatalf("show-all should clear scope")
	}
}

func TestCommitShowAllOnCommitsPanel(t *testing.T) {
	m := footerModel()
	m.focus = panelCommits
	m.commitScopeBranches = []string{"feat"}
	if _, ok := findRow(availableActions(m), "commits-showall"); !ok {
		t.Fatalf("Show all should be offered from the Commits panel menu when scoped")
	}
}

// TestCommitSoloReloadEndToEnd drives the full chain: the menu row's run handler
// returns a reload cmd; executing it reloads the (scoped) feed; the resulting
// commitsReloadedMsg flows back through Update and paints the commits.
func TestCommitSoloReloadEndToEnd(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: "h1\x1f\x1fAda\x1f0\x1fsubject\x1fHEAD -> feat\n"})
	svc := domain.New(&git.Repo{Runner: f})
	m := branchesPanelModel("feat")
	m.svc = svc
	m.feed = svc.CommitFeed()

	r, ok := findRow(availableActions(m), "commits-solo")
	if !ok {
		t.Fatal("Solo this branch missing")
	}
	mm, cmd := r.run(m)
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("solo should return a reload cmd")
	}
	msg := cmd() // executes reloadFeedCmd → feed.LoadInitial against the fake
	mm, _ = m.Update(msg)
	m = mm.(Model)
	if len(m.commits) != 1 || m.commits[0].Hash != "h1" {
		t.Fatalf("after solo reload, commits = %+v", m.commits)
	}
	if m.commitScopeLabel() != "solo: feat" {
		t.Fatalf("scope label = %q", m.commitScopeLabel())
	}
	rows := m.commitRows()
	if len(rows) != 1 || !strings.Contains(rows[0], "‹*feat›") {
		t.Fatalf("commit row should carry the head-branch label: %q", rows)
	}
}

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

func TestCommitScopeLabel(t *testing.T) {
	m := footerModel()
	if m.commitScopeLabel() != "all" {
		t.Fatalf("empty scope label = %q, want all", m.commitScopeLabel())
	}
	m.commitScopeBranches = []string{"feat"}
	if m.commitScopeLabel() != "solo: feat" {
		t.Fatalf("solo label = %q", m.commitScopeLabel())
	}
}

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

func TestCommitRowsRenderLabels(t *testing.T) {
	m := footerModel()
	m.commits = []model.Commit{
		{Hash: "a1b2c3d4", Subject: "do a thing", Refs: []model.Ref{
			{Name: "main", Kind: model.RefLocal, Head: true},
			{Name: "feature", Kind: model.RefLocal},
			{Name: "origin/main", Kind: model.RefRemote},
		}},
		{Hash: "ffff0000", Subject: "plain"},
	}
	rows := m.commitRows()
	if !strings.Contains(rows[0], "a1b2c3d") || !strings.Contains(rows[0], "*main") || !strings.Contains(rows[0], "feature") {
		t.Fatalf("row0 should show local branch labels with *head: %q", rows[0])
	}
	if strings.Contains(rows[0], "origin/main") {
		t.Fatalf("remote labels not rendered in Phase 1: %q", rows[0])
	}
	if strings.Contains(rows[1], "‹") {
		t.Fatalf("undecorated row should have no labels: %q", rows[1])
	}
}
