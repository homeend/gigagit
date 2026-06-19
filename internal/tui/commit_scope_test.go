package tui

import (
	"strings"
	"testing"

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
