package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func shelfPopModel(entries ...model.ShelfEntry) Model {
	m := footerModel()
	m.width, m.height = 100, 30
	m.shelfEntries = entries
	m.shelfPopup = newShelfPopup(entries)
	return m
}

func shEntry(id, path string) model.ShelfEntry {
	return model.ShelfEntry{ID: id, Origin: model.FileAddress{State: model.StateUnstaged, Worktree: "/wt", Path: path}, SHA: id + "0000"}
}

func TestShelfPopupRendersOrigin(t *testing.T) {
	m := shelfPopModel(shEntry("a", "dir/x.go"))
	out := m.renderShelfPopup()
	if !strings.Contains(out, "dir/x.go") || !strings.Contains(out, "Shelf") {
		t.Fatalf("popup missing content:\n%s", out)
	}
}

func TestShelfPopupZCyclesMode(t *testing.T) {
	m := shelfPopModel(shEntry("a", "x.go"))
	mm, _ := m.updateShelfPopupKey(keyMsg("z"))
	m = mm.(Model)
	if m.shelfPopup.mode != modeWrap {
		t.Fatalf("z should cycle to wrap, got %v", m.shelfPopup.mode)
	}
}

func TestShelfPopupEnterJumps(t *testing.T) {
	m := shelfPopModel(shEntry("a", "x.go"))
	mm, _ := m.updateShelfPopupKey(keyMsg("enter"))
	m = mm.(Model)
	if m.shelfPopup != nil {
		t.Fatalf("enter should close the popup (jump to diff)")
	}
	if m.diffView == nil || m.diffTag != "shelf:a" {
		t.Fatalf("enter should open the shelf-vs-worktree diff, tag=%q", m.diffTag)
	}
}

func TestShelfPopupRemoveConfirms(t *testing.T) {
	m := shelfPopModel(shEntry("a", "x.go"))
	mm, _ := m.updateShelfPopupKey(keyMsg("x"))
	m = mm.(Model)
	if m.modal == nil {
		t.Fatalf("x should open a remove-confirm modal")
	}
}

func TestShelfPopupRestoreOpensDest(t *testing.T) {
	m := shelfPopModel(shEntry("a", "x.go"))
	mm, _ := m.updateShelfPopupKey(keyMsg("p"))
	m = mm.(Model)
	if m.shelfRestorePopup == nil || m.shelfRestorePopup.entryID != "a" {
		t.Fatalf("p should open the restore-destination popup")
	}
}

func TestShelfPopupMarkThenCompare(t *testing.T) {
	m := shelfPopModel(shEntry("a", "a.go"), shEntry("b", "b.go"))
	mm, _ := m.updateShelfPopupKey(keyMsg("m"))
	m = mm.(Model)
	if m.shelfPopup == nil || m.shelfPopup.markID != "a" {
		t.Fatalf("first m should mark entry a")
	}
	m.shelfPopup.sel = 1
	mm, _ = m.updateShelfPopupKey(keyMsg("m"))
	m = mm.(Model)
	if m.diffView == nil || m.diffTag != "shelf2:a:b" {
		t.Fatalf("second m should open the two-entry diff, tag=%q", m.diffTag)
	}
}
