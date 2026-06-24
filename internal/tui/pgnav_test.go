package tui

import (
	"fmt"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestShiftTabCyclesBackwards(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelBranches // the active left tab
	// shift+tab from the active tab wraps to the bottom of the order (Commits).
	u, _ := m.Update(keyMsg("shift+tab"))
	m = u.(Model)
	if m.focus != panelCommits {
		t.Fatalf("focus = %v, want panelCommits", m.focus)
	}
	// A full reverse cycle returns to Branches and never visits the inactive
	// Worktrees tab (focus order is [activeTab, Status, Commits]).
	order := m.focusOrder()
	for i := 0; i < len(order)-1; i++ {
		u, _ = m.Update(keyMsg("shift+tab"))
		m = u.(Model)
		if m.focus == panelWorktrees {
			t.Fatal("reverse cycle visited the inactive Worktrees tab")
		}
	}
	if m.focus != panelBranches {
		t.Fatalf("full reverse cycle should return to panelBranches, got %v", m.focus)
	}
}

func TestPageDownMovesQuarterViewportAndClamps(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 27 // bodyH 24 → commits rowsCap 21 → step 5
	m.focus = panelCommits
	m.commits = make([]model.Commit, 12)
	for i := range m.commits {
		m.commits[i] = model.Commit{Hash: fmt.Sprintf("%040d", i), Subject: fmt.Sprintf("c%d", i), UnixTime: int64(i)}
	}
	if got := m.pageStep(); got != 5 {
		t.Fatalf("pageStep = %d, want 5", got)
	}
	u, _ := m.Update(keyMsg("pgdown"))
	m = u.(Model)
	if m.sel[panelCommits] != 5 {
		t.Fatalf("sel = %d, want 5", m.sel[panelCommits])
	}
	u, _ = m.Update(keyMsg("pgdown"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("pgdown"))
	m = u.(Model)
	if m.sel[panelCommits] != 11 {
		t.Fatalf("sel = %d, want clamped 11", m.sel[panelCommits])
	}
	u, _ = m.Update(keyMsg("pgup"))
	m = u.(Model)
	if m.sel[panelCommits] != 6 {
		t.Fatalf("sel = %d, want 6", m.sel[panelCommits])
	}
}

func TestPageStepFallsBackTo1WhenPanelHidden(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 80, 11 // bodyH 8 < 9: Worktrees panel hidden by layout
	m.focus = panelWorktrees
	if got := m.pageStep(); got != 1 {
		t.Fatalf("pageStep = %d, want fallback 1", got)
	}
}
