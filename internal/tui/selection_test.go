package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestRestoreSelByIdentitySurvivesReorder(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.branches = []model.Branch{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	m.sel[panelBranches] = 2 // "c"
	key := m.panelSelKey(panelBranches)
	// Refresh reorders the list; "c" moves to index 0.
	m.branches = []model.Branch{{Name: "c"}, {Name: "a"}, {Name: "b"}}
	m = m.restorePanelSel(panelBranches, key)
	if m.sel[panelBranches] != 0 {
		t.Fatalf("selection should follow 'c' to index 0, got %d", m.sel[panelBranches])
	}
}

func TestRestoreSelClampsWhenItemGone(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.branches = []model.Branch{{Name: "a"}, {Name: "b"}}
	m.sel[panelBranches] = 1 // "b"
	key := m.panelSelKey(panelBranches)
	m.branches = []model.Branch{{Name: "a"}} // "b" deleted
	m = m.restorePanelSel(panelBranches, key)
	if m.sel[panelBranches] != 0 {
		t.Fatalf("selection should clamp to 0 when item gone, got %d", m.sel[panelBranches])
	}
}
