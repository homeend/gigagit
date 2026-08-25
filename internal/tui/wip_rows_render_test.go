package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/homeend/gigagit/internal/model"
)

// dirtyStatus is a status with one unstaged and one staged file, so
// deriveWipRows yields both pseudo-rows.
func dirtyStatus() model.WorkingTreeStatus {
	return model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "u", Unstaged: 'M'},
		{Path: "s", Staged: 'M'},
	}}
}

func TestWipRowsAppearAndBackingIndexGuards(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.status = dirtyStatus()
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()

	if m.wipCount() != 2 {
		t.Fatalf("wipCount=%d want 2", m.wipCount())
	}
	if l := m.listFor(panelCommits); l.Len() != m.commitsTotal() {
		t.Fatalf("commitList.Len=%d want %d", l.Len(), m.commitsTotal())
	}
	// Selecting a wip row: backingIndex must refuse (ok=false).
	m.sel[panelCommits] = 0
	if _, ok := m.backingIndex(panelCommits); ok {
		t.Fatal("backingIndex must be ok=false on a wip row")
	}
	// Selecting the first real commit (unified index = wipCount) maps to feed[0].
	m.sel[panelCommits] = m.wipCount()
	bi, ok := m.backingIndex(panelCommits)
	if !ok || bi != 0 {
		t.Fatalf("first real commit => bi=%d ok=%v, want 0,true", bi, ok)
	}
	if m.commits[bi].Hash == "" {
		t.Fatal("backing commit must be a real feed commit")
	}
}

func TestWipRowsRender(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 3)
	u, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = u.(Model)
	m.loading = false
	m.focus = panelCommits
	m.status = dirtyStatus()
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()

	out := ansi.Strip(m.View())
	if !strings.Contains(out, "Working tree") || !strings.Contains(out, "Staged") {
		t.Fatalf("expected WIP rows in render:\n%s", out)
	}
	if !strings.Contains(out, "◇") {
		t.Fatalf("expected the ◇ WIP node glyph:\n%s", out)
	}
	if len(m.commitGraphRows) != m.commitsTotal() {
		t.Fatalf("graph rows=%d want commitsTotal=%d", len(m.commitGraphRows), m.commitsTotal())
	}
}
