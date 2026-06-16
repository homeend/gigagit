package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSynthKey(t *testing.T) {
	if got := synthKey("enter"); got.Type != tea.KeyEnter {
		t.Errorf("enter -> %v, want KeyEnter", got.Type)
	}
	if got := synthKey("space"); got.Type != tea.KeySpace {
		t.Errorf("space -> %v, want KeySpace", got.Type)
	}
	for _, k := range []string{"p", "P", "/", ",", "?", "."} {
		if got := synthKey(k); got.String() != k {
			t.Errorf("synthKey(%q).String() = %q", k, got.String())
		}
	}
}

func TestMenuActionsAllowlistFiltersAndOrders(t *testing.T) {
	m := footerModel()
	m.loading = false
	m.cfg.UI.MenuActions = []string{"repo", "pull"}
	mm := m.openActionMenu()
	got := []string{}
	for _, r := range mm.actionMenu.rows {
		got = append(got, r.id)
	}
	if len(got) != 2 || got[0] != "repo" || got[1] != "pull" {
		t.Errorf("menu rows = %v, want [repo pull] in order", got)
	}
}

func TestDotOpensActionMenu(t *testing.T) {
	m := footerModel()
	m.loading = false
	u, _ := m.Update(keyMsg("."))
	mm := u.(Model)
	if mm.actionMenu == nil {
		t.Fatal(". must open the action menu")
	}
}

func TestActionMenuRunsPullByKey(t *testing.T) {
	m := footerModel()
	m.loading = false
	u, _ := m.Update(keyMsg(".")) // open
	m = u.(Model)
	u, cmd := m.Update(keyMsg("p")) // direct key runs pull
	mm := u.(Model)
	if mm.actionMenu != nil {
		t.Fatal("running an action must close the menu")
	}
	if !mm.running || cmd == nil {
		t.Fatal("p from the menu must start SmartPull")
	}
}

func TestActionMenuEscCloses(t *testing.T) {
	m := footerModel()
	m.loading = false
	u, _ := m.Update(keyMsg("."))
	m = u.(Model)
	u, _ = m.Update(keyMsg("esc"))
	if u.(Model).actionMenu != nil {
		t.Fatal("esc must close the menu")
	}
}

func TestDotNoOpUnderPopup(t *testing.T) {
	m := footerModel()
	m.repoPopup = &repoPopup{} // a popup owns the keyboard
	u, _ := m.Update(keyMsg("."))
	if u.(Model).actionMenu != nil {
		t.Fatal(". must not open the menu while another popup is open")
	}
}

func TestAvailableActionsExcludesNavAndSelf(t *testing.T) {
	m := footerModel()
	m.loading = false
	ids := map[string]bool{}
	for _, r := range availableActions(m) {
		ids[r.id] = true
	}
	if !ids["pull"] || !ids["repo"] {
		t.Errorf("expected global actions present, got %v", ids)
	}
	if ids["actions"] {
		t.Error("the menu must not list itself (actions)")
	}
	for _, nav := range []string{"tab", "ctrl+←/→"} {
		if ids[nav] {
			t.Errorf("navigation key %q must not appear as an action", nav)
		}
	}
}
