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
	a := layerOf[*stashActionPopup](got)
	if a == nil {
		t.Fatal("enter should open the stash-action popup")
	}
	if a.ref != "stash@{0}" {
		t.Errorf("action popup ref = %q", a.ref)
	}
}

func TestStashActionZCyclesMode(t *testing.T) {
	m := Model{width: 100, height: 30}
	a := &stashActionPopup{ref: "stash@{0}", subject: "WIP"}
	m = m.pushLayer(a)
	got, _ := a.update(m, keyMsg("z"))
	a2 := layerOf[*stashActionPopup](got)
	if a2 == nil || a2.mode != modeWrap {
		t.Fatalf("z should cycle the stash-action mode to modeWrap; got %+v", a2)
	}
}

func TestStashActionApplyDispatches(t *testing.T) {
	m := loadedModel(t)
	a := &stashActionPopup{ref: "stash@{0}", sel: 0} // 0 = Apply
	m = m.pushLayer(a)
	got, cmd := a.update(m, keyMsg("enter"))
	if layerOf[*stashActionPopup](got) != nil {
		t.Error("apply should close the popup")
	}
	if !got.running || cmd == nil {
		t.Error("apply should start an op")
	}
	_ = driveOp(t, got, cmd) // op fails (no stash) but must not panic
}

func TestStashActionDropConfirms(t *testing.T) {
	m := loadedModel(t)
	a := &stashActionPopup{ref: "stash@{0}", sel: 2} // 2 = Drop
	m = m.pushLayer(a)
	got, _ := a.update(m, keyMsg("enter"))
	a2 := layerOf[*stashActionPopup](got)
	if a2 == nil || !a2.confirming {
		t.Fatal("drop should enter a confirm state, not run immediately")
	}
	got2, cmd := a2.update(got, keyMsg("y"))
	if layerOf[*stashActionPopup](got2) != nil || !got2.running {
		t.Error("y should confirm drop and run the op")
	}
	_ = driveOp(t, got2, cmd)
}
