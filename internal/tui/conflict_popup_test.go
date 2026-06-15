package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func conflictModel() Model {
	m := Model{width: 120, height: 30, focus: panelStatus, sel: map[panel]int{}}
	m.status = model.WorkingTreeStatus{Branch: "zzz", Files: []model.FileStatus{
		{Path: "uu.txt", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'},
		{Path: "md.txt", Kind: model.KindUnmerged, Staged: 'D', Unstaged: 'U'},
	}}
	return m
}

func TestStatusBarShowsConflictNotice(t *testing.T) {
	m := conflictModel()
	out := m.View()
	if !strings.Contains(out, "2 conflicts") || !strings.Contains(out, "[x]") {
		t.Errorf("status bar should announce conflicts:\n%s", out)
	}
}

func TestXOpensConflictPopup(t *testing.T) {
	m := conflictModel()
	mm, _ := m.Update(keyMsg("x"))
	if mm.(Model).conflictPopup == nil {
		t.Fatal("x should open the conflict popup when conflicts exist")
	}
}

func TestXNoOpWithoutConflicts(t *testing.T) {
	m := Model{width: 120, height: 30, sel: map[panel]int{}}
	mm, _ := m.Update(keyMsg("x"))
	if mm.(Model).conflictPopup != nil {
		t.Fatal("x must do nothing when there are no conflicts")
	}
}
