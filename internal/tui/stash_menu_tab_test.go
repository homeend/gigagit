package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func stashModel(t *testing.T) Model {
	t.Helper()
	m := loadedModel(t)
	m.focus = panelFiles
	mm, _ := m.Update(keyMsg("S"))
	m = mm.(Model)
	m.stashView.loading = false
	m.stashView.entries = []model.StashEntry{{Ref: "stash@{0}", Subject: "WIP on main"}}
	return m
}

// tab from the stash list cycles focus into the left column (the stash list
// occupies the Commits slot in the normal focus order); the window stays open.
func TestStashViewTabCyclesFocusToLeftPanels(t *testing.T) {
	m := stashModel(t)
	mm, _ := m.updateStashViewKey(keyMsg("tab"))
	got := mm.(Model)
	if got.focus == panelCommits {
		t.Fatal("tab should move focus off the stash list into the left column")
	}
	if got.stashView == nil {
		t.Fatal("the stash window must stay open while tab cycles focus")
	}
	// And the normal dispatch cycles back around to the stash list eventually.
	for i := 0; i < 8 && got.focus != panelCommits; i++ {
		mm, _ = got.Update(keyMsg("tab"))
		got = mm.(Model)
	}
	if got.focus != panelCommits {
		t.Fatal("tab from the left panels should cycle back to the stash list")
	}
}

func TestStashViewShiftTabCyclesFocus(t *testing.T) {
	m := stashModel(t)
	mm, _ := m.updateStashViewKey(keyMsg("shift+tab"))
	got := mm.(Model)
	if got.focus == panelCommits {
		t.Fatal("shift+tab should move focus off the stash list")
	}
	if got.stashView == nil {
		t.Fatal("the stash window must stay open")
	}
}

// enter on a stash entry drills into its file list with focus on the tree —
// the commits-panel enter gesture ('l' keeps opening on the list side).
func TestStashViewEnterOpensFilesTreeFocused(t *testing.T) {
	m := stashModel(t)
	mm, cmd := m.updateStashViewKey(keyMsg("enter"))
	got := mm.(Model)
	if got.filesView == nil {
		t.Fatal("enter should open the stash's file list")
	}
	if !got.filesTreeFocused {
		t.Error("enter should land focus on the tree (like commits enter)")
	}
	if got.filesStashTag != "stash@{0}" {
		t.Errorf("filesStashTag = %q, want stash@{0}", got.filesStashTag)
	}
	if cmd == nil {
		t.Error("enter should fire the stash files load")
	}
}

// . on the stash list offers the stash actions (and the copy row) in the menu.
func TestStashViewDotMenuOffersApplyPopDrop(t *testing.T) {
	m := stashModel(t)
	mm, _ := m.updateStashViewKey(keyMsg("."))
	got := mm.(Model)
	if got.actionMenu == nil {
		t.Fatal(". should open the action menu")
	}
	ids := map[string]bool{}
	for _, r := range got.actionMenu.rows {
		ids[r.id] = true
	}
	for _, want := range []string{"stash-apply", "stash-pop", "stash-drop", "copy-stash-ref"} {
		if !ids[want] {
			t.Errorf("menu missing row %q (got %v)", want, ids)
		}
	}
}

// Apply / Pop rows start their op directly.
func TestStashMenuApplyRowStartsOp(t *testing.T) {
	m := stashModel(t)
	rows := m.stashActionRows()
	if len(rows) != 3 {
		t.Fatalf("want 3 stash action rows, got %d", len(rows))
	}
	_, cmd := rows[0].run(m)
	if cmd == nil {
		t.Fatal("Apply row should start the StashApply op")
	}
}

// Drop confirms first: the row opens a Drop/Cancel modal; Drop resolves to the op.
func TestStashMenuDropRowConfirms(t *testing.T) {
	m := stashModel(t)
	rows := m.stashActionRows()
	mm, _ := rows[2].run(m)
	got := mm.(Model)
	if got.modal == nil {
		t.Fatal("Drop row should open a confirm modal")
	}
	opts := got.modal.req.Options
	if len(opts) != 2 || opts[0] != "Drop" || opts[1] != "Cancel" {
		t.Fatalf("modal options = %v, want [Drop Cancel]", opts)
	}
	_, cmd := got.modal.onResolve(got, "Drop")
	if cmd == nil {
		t.Fatal("resolving Drop should start the StashDrop op")
	}
	if _, cmd := got.modal.onResolve(got, "Cancel"); cmd != nil {
		t.Fatal("Cancel must not start an op")
	}
}

// The stash rows must not leak into the menu when the file tree is open over
// the stash list (the files view is the front surface then).
func TestStashMenuRowsAbsentUnderFilesView(t *testing.T) {
	m := stashModel(t)
	mm, _ := m.updateStashViewKey(keyMsg("l"))
	m = mm.(Model)
	if m.filesView == nil {
		t.Fatal("setup: l should open the files view over the stash list")
	}
	for _, r := range availableActions(m) {
		if r.id == "stash-apply" || r.id == "stash-pop" || r.id == "stash-drop" {
			t.Fatalf("stash action row %q leaked into the files-view menu", r.id)
		}
	}
}
