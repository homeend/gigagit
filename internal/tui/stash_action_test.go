package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestStashEnterOpensActions(t *testing.T) {
	m := Model{width: 100, height: 30, sel: map[panel]int{}}
	m.stashView = &stashView{entries: []model.StashEntry{{Ref: "stash@{0}", Subject: "WIP"}}}
	mm, _ := m.updateStashViewKey(keyMsg("enter"))
	got := mm.(Model)
	if got.stashAction == nil {
		t.Fatal("enter should open the stash-action popup")
	}
	if got.stashAction.ref != "stash@{0}" {
		t.Errorf("action popup ref = %q", got.stashAction.ref)
	}
}

func TestStashActionZCyclesMode(t *testing.T) {
	m := Model{width: 100, height: 30}
	m.stashAction = &stashActionPopup{ref: "stash@{0}", subject: "WIP"}
	mm, _ := m.updateStashActionKey(keyMsg("z"))
	got := mm.(Model)
	if got.stashAction == nil || got.stashAction.mode != modeWrap {
		t.Fatalf("z should cycle the stash-action mode to modeWrap; got %+v", got.stashAction)
	}
}

func TestStashActionApplyDispatches(t *testing.T) {
	m := loadedModel(t)
	m.stashAction = &stashActionPopup{ref: "stash@{0}", sel: 0} // 0 = Apply
	mm, cmd := m.updateStashActionKey(keyMsg("enter"))
	got := mm.(Model)
	if got.stashAction != nil {
		t.Error("apply should close the popup")
	}
	if !got.running || cmd == nil {
		t.Error("apply should start an op")
	}
	_ = driveOp(t, got, cmd) // op fails (no stash) but must not panic
}

func TestStashActionDropConfirms(t *testing.T) {
	m := loadedModel(t)
	m.stashAction = &stashActionPopup{ref: "stash@{0}", sel: 2} // 2 = Drop
	mm, _ := m.updateStashActionKey(keyMsg("enter"))
	got := mm.(Model)
	if got.stashAction == nil || !got.stashAction.confirming {
		t.Fatal("drop should enter a confirm state, not run immediately")
	}
	mm, cmd := got.updateStashActionKey(keyMsg("y"))
	got = mm.(Model)
	if got.stashAction != nil || !got.running {
		t.Error("y should confirm drop and run the op")
	}
	_ = driveOp(t, got, cmd)
}
