package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/model"
)

func TestCapitalSOpensStashView(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelStatus
	mm, cmd := m.Update(keyMsg("S"))
	got := mm.(Model)
	if got.stashView == nil {
		t.Fatal("S should open the stash view")
	}
	if cmd == nil {
		t.Error("opening the stash view should fire its load cmd")
	}
	if got.focus != panelCommits {
		t.Error("opening the stash window should move focus to the right column (panelCommits)")
	}
	if got.panelFocused(panelStatus) {
		t.Error("left panels must dim (unfocused) while the stash window is open")
	}
}

func TestStashWindowArrowFocusSwitch(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelCommits
	m.stashView = &stashView{entries: []model.StashEntry{{Ref: "stash@{0}", Subject: "WIP"}}}
	mm, _ := m.updateStashViewKey(keyMsg("l"))
	m = mm.(Model)
	if m.filesView == nil || m.filesTreeFocused {
		t.Fatal("l should open the tree with focus on the stash list (filesTreeFocused=false)")
	}
	mm, _ = m.updateFilesViewKey(keyMsg("left"))
	if !mm.(Model).filesTreeFocused {
		t.Error("← should focus the file tree")
	}
	mm, _ = mm.(Model).updateFilesViewKey(keyMsg("right"))
	if mm.(Model).filesTreeFocused {
		t.Error("→ should focus the stash list")
	}
}

func TestStashWindowCloseRestoresFocus(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelStatus
	mm, _ := m.Update(keyMsg("S"))
	mm, _ = mm.(Model).updateStashViewKey(keyMsg("esc"))
	got := mm.(Model)
	if got.stashView != nil {
		t.Fatal("esc should close the stash window")
	}
	if got.focus != panelStatus {
		t.Errorf("closing should restore focus to panelStatus, got %v", got.focus)
	}
}

func TestStashListEnterUnderTreeOpensActions(t *testing.T) {
	m := loadedModel(t)
	m.stashView = &stashView{entries: []model.StashEntry{{Ref: "stash@{0}", Subject: "WIP"}}}
	m.filesView = &contentPopup{lines: []contentLine{{text: "a.go", path: "a.go"}}}
	m.filesTreeFocused = false // focused on the stash list
	mm, _ := m.updateFilesViewKey(keyMsg("enter"))
	if mm.(Model).stashAction == nil {
		t.Fatal("enter on the stash-list side (tree open) should open the action popup")
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
	if got.filesTreeFocused {
		t.Error("the stash file tree should open with focus on the list (follow-live), like commits")
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

func TestStashFollowLiveReloadsTree(t *testing.T) {
	m := loadedModel(t)
	m.stashView = &stashView{
		entries: []model.StashEntry{{Ref: "stash@{0}", Subject: "a"}, {Ref: "stash@{1}", Subject: "b"}},
		sel:     0,
	}
	m.filesView = &contentPopup{lines: []contentLine{{text: "x"}}}
	m.filesTreeFocused = false // list side
	m.filesStashTag = "stash@{0}"
	mm, cmd := m.updateFilesViewKey(keyMsg("j"))
	got := mm.(Model)
	if got.stashView.sel != 1 {
		t.Fatal("j on the list side should move the stash selection")
	}
	if got.filesStashTag != "stash@{1}" {
		t.Errorf("follow-live should retarget the tree to stash@{1}, got %q", got.filesStashTag)
	}
	if cmd == nil {
		t.Error("landing on a different stash should fire the follow-live reload")
	}
}
