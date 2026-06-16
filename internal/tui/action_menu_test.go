package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/gigagit/gg/internal/model"
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
	if ids["quit"] {
		t.Error("quit must not be a menu row (q closes the menu)")
	}
	for _, nav := range []string{"tab", "ctrl+←/→"} {
		if ids[nav] {
			t.Errorf("navigation key %q must not appear as an action", nav)
		}
	}
}

func TestActionMenuQCloses(t *testing.T) {
	m := footerModel()
	m.loading = false
	u, _ := m.Update(keyMsg("."))
	m = u.(Model)
	u, cmd := m.Update(keyMsg("q"))
	mm := u.(Model)
	if mm.actionMenu != nil {
		t.Fatal("q must close the menu")
	}
	if cmd != nil {
		t.Fatal("q must close the menu, not quit the app")
	}
}

// Space reports String() as " ", so the direct-key match must normalize it;
// pressing space on the [space] stage row runs (and closes the menu), not no-op.
func TestActionMenuRunsStageBySpace(t *testing.T) {
	m := footerModel()
	m.loading = false
	m.focus = panelFiles
	m.status.Files = []model.FileStatus{{Path: "f.txt", Kind: model.KindTracked, Staged: '.', Unstaged: 'M'}}
	u, _ := m.Update(keyMsg(".")) // menu includes the [space] stage row
	m = u.(Model)
	u, _ = m.Update(keyMsg("space"))
	if u.(Model).actionMenu != nil {
		t.Fatal("space must match the stage row and close the menu (not no-op)")
	}
}

func TestActionMenuRenders(t *testing.T) {
	m := footerModel()
	m.loading = false
	u, _ := m.Update(keyMsg("."))
	out := ansi.Strip(u.(Model).View())
	if !strings.Contains(out, "Actions") || !strings.Contains(out, "[p]ull") {
		t.Fatalf("rendered menu missing header/rows:\n%s", out)
	}
}
