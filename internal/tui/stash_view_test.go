package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/engine"
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

func TestStashViewNavAndClose(t *testing.T) {
	m := Model{width: 100, height: 30, sel: map[panel]int{}}
	m.stashView = &stashView{entries: []model.StashEntry{{Ref: "stash@{0}"}, {Ref: "stash@{1}"}}}
	mm, _ := m.updateStashViewKey(keyMsg("j"))
	if mm.(Model).stashView.sel != 1 {
		t.Fatal("j should move stash selection")
	}
	mm, _ = mm.(Model).updateStashViewKey(keyMsg("S"))
	if mm.(Model).stashView != nil {
		t.Fatal("S should close the stash view")
	}
}

func TestStashViewLLoadsFiles(t *testing.T) {
	m := loadedModel(t)
	m.stashView = &stashView{entries: []model.StashEntry{{Ref: "stash@{0}", Subject: "On main: WIP"}}}
	mm, cmd := m.updateStashViewKey(keyMsg("l"))
	got := mm.(Model)
	if got.filesView == nil {
		t.Fatal("l should open the file tree for the stash")
	}
	if !got.filesTreeFocused {
		t.Error("the stash file tree should open focused")
	}
	if got.filesStashTag != "stash@{0}" {
		t.Errorf("filesStashTag = %q", got.filesStashTag)
	}
	if cmd == nil {
		t.Error("l should fire the stash-files load cmd")
	}
}

func TestStashListRefreshesAfterOp(t *testing.T) {
	m := loadedModel(t)
	m.stashView = &stashView{entries: []model.StashEntry{{Ref: "stash@{0}"}}, tag: "stash"}
	mm, cmd := m.Update(opFinishedMsg{res: engine.Result{Changed: true}})
	got := mm.(Model)
	if cmd == nil {
		t.Fatal("op finishing with the stash window open should refresh the list")
	}
	if !got.stashView.loading {
		t.Error("the stash list should be marked loading during the refresh")
	}
}
