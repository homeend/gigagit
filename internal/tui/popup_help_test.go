package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

// helpJoin flattens contentLines to one searchable string.
func helpJoin(lines []contentLine) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.text)
		b.WriteString("\n")
	}
	return b.String()
}

func TestBookmarkSwitcherHelpListsKeys(t *testing.T) {
	h := helpJoin(bookmarkSwitcherHelp(false))
	for _, want := range []string{"enter", "paste", "mark", "remove", "filter"} {
		if !strings.Contains(h, want) {
			t.Errorf("bookmark switcher help missing %q:\n%s", want, h)
		}
	}
}

func TestShelfSwitcherHelpListsKeys(t *testing.T) {
	h := helpJoin(shelfSwitcherHelp(false))
	for _, want := range []string{"enter", "restore", "mark", "remove", "filter"} {
		if !strings.Contains(h, want) {
			t.Errorf("shelf switcher help missing %q:\n%s", want, h)
		}
	}
}

// In compare mode the action keys are inert, so the sheet must omit them.
func TestSwitcherHelpCompareModeOmitsInertKeys(t *testing.T) {
	h := helpJoin(bookmarkSwitcherHelp(true))
	if !strings.Contains(h, "compare the focused file") {
		t.Errorf("compare-mode bookmark help must describe the compare action:\n%s", h)
	}
	for _, gone := range []string{"paste", "remove the bookmark"} {
		if strings.Contains(h, gone) {
			t.Errorf("compare-mode bookmark help must omit the inert %q:\n%s", gone, h)
		}
	}
	if strings.Contains(helpJoin(shelfSwitcherHelp(true)), "restore") {
		t.Error("compare-mode shelf help must omit the inert restore key")
	}
}

// bookmarkPopupModel returns a model with the bookmark switcher open.
func bookmarkPopupModel() Model {
	m := Model{width: 80, height: 24, sel: map[panel]int{}, sortModes: map[panel]sortMode{}}
	m = m.pushLayer(newBookmarkPopup([]model.Bookmark{{ID: "b1", Path: "a.go"}}))
	return m
}

func shelfPopupModel() Model {
	m := Model{width: 80, height: 24, sel: map[panel]int{}, sortModes: map[panel]sortMode{}}
	m = m.pushLayer(newShelfPopup([]model.ShelfEntry{{ID: "s1"}}))
	return m
}

func TestQuestionMarkOpensCheatSheetOverBookmarkPopup(t *testing.T) {
	m := bookmarkPopupModel()
	u, _ := m.Update(keyMsg("?"))
	m = u.(Model)
	if layerOf[*contentPopup](m) == nil {
		t.Fatal("? must open the cheat sheet")
	}
	if m.bookmarkSwitcher() == nil {
		t.Fatal("the bookmark switcher must stay open under the cheat sheet")
	}
	if !strings.Contains(layerOf[*contentPopup](m).title, "Bookmark") {
		t.Fatalf("cheat sheet title = %q, want the bookmark switcher", layerOf[*contentPopup](m).title)
	}
	if !strings.Contains(helpJoin(layerOf[*contentPopup](m).lines), "paste") {
		t.Fatal("cheat sheet must list the bookmark switcher keys")
	}
}

func TestQuestionMarkOpensCheatSheetOverShelfPopup(t *testing.T) {
	m := shelfPopupModel()
	u, _ := m.Update(keyMsg("?"))
	m = u.(Model)
	if layerOf[*contentPopup](m) == nil || m.shelfSwitcher() == nil {
		t.Fatal("? must open the cheat sheet and keep the shelf switcher open")
	}
	if !strings.Contains(layerOf[*contentPopup](m).title, "Shelf") {
		t.Fatalf("cheat sheet title = %q, want the shelf switcher", layerOf[*contentPopup](m).title)
	}
}

// With the cheat sheet open over the picker, keys must route to the cheat sheet,
// not the picker underneath (dispatch hoist).
func TestCheatSheetCapturesKeysOverPicker(t *testing.T) {
	m := bookmarkPopupModel()
	u, _ := m.Update(keyMsg("?"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("/")) // / must start the cheat sheet's search, not the bookmark filter
	m = u.(Model)
	if !layerOf[*contentPopup](m).typing {
		t.Fatal("/ must start the cheat sheet search (keys route to the overlay)")
	}
	if m.bookmarkSwitcher().filtering {
		t.Fatal("the bookmark filter must not engage while the cheat sheet owns the keys")
	}
}

// esc closes the cheat sheet and returns to the picker with its filter/mark
// state intact — the differentiator of the chosen overlay-return behavior.
func TestCheatSheetEscReturnsToPicker(t *testing.T) {
	m := bookmarkPopupModel()
	m.bookmarkSwitcher().filter = "ab"
	m.bookmarkSwitcher().markID = "b1"
	u, _ := m.Update(keyMsg("?"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	if layerOf[*contentPopup](m) != nil {
		t.Fatal("esc must close the cheat sheet")
	}
	if m.bookmarkSwitcher() == nil {
		t.Fatal("esc must return to the bookmark switcher, still open")
	}
	if m.bookmarkSwitcher().filter != "ab" || m.bookmarkSwitcher().markID != "b1" {
		t.Fatalf("the switcher's filter/mark must survive: filter=%q markID=%q", m.bookmarkSwitcher().filter, m.bookmarkSwitcher().markID)
	}
}

// The cheat sheet renders on top of the picker.
func TestCheatSheetRendersOverPicker(t *testing.T) {
	m := bookmarkPopupModel()
	u, _ := m.Update(keyMsg("?"))
	m = u.(Model)
	out := m.render()
	if !strings.Contains(out, "Bookmark switcher") {
		t.Fatalf("the cheat sheet must paint over the picker:\n%s", out)
	}
}

// Both switchers must advertise the new ? key in their footer hint.
func TestSwitcherFootersAdvertiseQuestionMark(t *testing.T) {
	bm := bookmarkPopupModel()
	if f := bm.renderBookmarkPopupBox(bm.bookmarkSwitcher()); !strings.Contains(f, "[?] keys") {
		t.Errorf("bookmark switcher footer must advertise [?] keys:\n%s", f)
	}
	sh := shelfPopupModel()
	if f := sh.renderShelfPopupBox(sh.shelfSwitcher()); !strings.Contains(f, "[?] keys") {
		t.Errorf("shelf switcher footer must advertise [?] keys:\n%s", f)
	}
}
