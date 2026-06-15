package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestCapitalSOpensStashView(t *testing.T) {
	m := loadedModel(t)
	mm, cmd := m.Update(keyMsg("S"))
	got := mm.(Model)
	if got.stashView == nil {
		t.Fatal("S should open the stash view")
	}
	if cmd == nil {
		t.Error("opening the stash view should fire its load cmd")
	}
}

func TestStashListAppliedToView(t *testing.T) {
	m := Model{width: 100, height: 30, sel: map[panel]int{}}
	m.stashView = &stashView{loading: true, tag: "stash"}
	entries := []model.StashEntry{{Ref: "stash@{0}", Subject: "On main: WIP on main"}}
	mm, _ := m.Update(stashListMsg{tag: "stash", entries: entries})
	got := mm.(Model)
	if got.stashView.loading || len(got.stashView.entries) != 1 {
		t.Fatalf("entries not applied: %+v", got.stashView)
	}
}

func TestStashViewRendersInRightColumn(t *testing.T) {
	m := Model{width: 100, height: 30, sel: map[panel]int{}, status: model.WorkingTreeStatus{Branch: "main"}}
	m.stashView = &stashView{entries: []model.StashEntry{
		{Ref: "stash@{0}", Subject: "On main: WIP on main"},
		{Ref: "stash@{1}", Subject: "On feat: sketch"},
	}}
	out := m.View()
	if !contains(out, "Stashes") {
		t.Errorf("right column should be titled Stashes:\n%s", out)
	}
	if !contains(out, "WIP on main") || !contains(out, "sketch") {
		t.Errorf("stash subjects missing:\n%s", out)
	}
}
