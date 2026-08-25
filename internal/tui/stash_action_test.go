package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// enter on the stash list drills into the file tree (the commits gesture);
// the Apply/Pop/Drop actions live in the "." menu.
func TestStashEnterOpensFiles(t *testing.T) {
	m := Model{width: 100, height: 30, sel: map[panel]int{}}
	m.stashView = &stashView{entries: []model.StashEntry{{Ref: "stash@{0}", Subject: "WIP"}}}
	mm, _ := m.updateStashViewKey(keyMsg("enter"))
	got := mm.(Model)
	if got.topLayer() != nil {
		t.Fatal("enter must not open a popup from the stash list")
	}
	if got.filesView == nil || !got.filesTreeFocused {
		t.Fatal("enter should open the stash's file tree with focus on the tree")
	}
	if got.filesStashTag != "stash@{0}" {
		t.Errorf("filesStashTag = %q", got.filesStashTag)
	}
}
