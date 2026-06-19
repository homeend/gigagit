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
	m := filesMenuModel()
	m.cfg.UI.MenuActions = []string{"file-diff", "stage"}
	mm := m.openActionMenu()
	got := []string{}
	for _, r := range mm.actionMenu.rows {
		got = append(got, r.id)
	}
	if len(got) != 2 || got[0] != "file-diff" || got[1] != "stage" {
		t.Errorf("menu rows = %v, want [file-diff stage] in order", got)
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

// filesMenuModel is a footerModel focused on the Files panel with one tracked,
// modified, stashable file selected — exercises the row- and window-scoped
// context actions (stage/stage-hunks/file-diff/mark-file are row; stash is
// window).
func filesMenuModel() Model {
	m := footerModel()
	m.loading = false
	m.focus = panelFiles
	m.status.Files = []model.FileStatus{{Path: "dir/f.txt", Kind: model.KindTracked, Staged: '.', Unstaged: 'M'}}
	return m
}

func TestAvailableActionsExcludesGlobals(t *testing.T) {
	m := filesMenuModel()
	ids := map[string]bool{}
	for _, r := range availableActions(m) {
		ids[r.id] = true
	}
	for _, g := range []string{"pull", "repo", "commit", "view", "actions", "quit", "help"} {
		if ids[g] {
			t.Errorf("global action %q must not appear in the . menu", g)
		}
	}
	if !ids["stage"] {
		t.Error("expected the row-scoped stage action in the menu")
	}
}

func TestAvailableActionsRowBeforeWindow(t *testing.T) {
	m := filesMenuModel()
	rows := availableActions(m)
	stageAt, stashAt := -1, -1
	for i, r := range rows {
		switch r.id {
		case "stage":
			stageAt = i
		case "stash":
			stashAt = i
		}
	}
	if stageAt < 0 || stashAt < 0 {
		t.Fatalf("want both stage (row) and stash (window) rows, got %v", rows)
	}
	if stageAt > stashAt {
		t.Errorf("row-scoped stage (%d) must precede window-scoped stash (%d)", stageAt, stashAt)
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
	if !strings.Contains(out, "Actions") || !strings.Contains(out, "[b]ranch") {
		t.Fatalf("rendered menu missing header/rows:\n%s", out)
	}
}

func TestContextCopyRowsCommits(t *testing.T) {
	m := footerModel()
	m.loading = false
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "0123456789abcdef0123456789abcdef01234567", Subject: "x"}}
	rows := m.contextCopyRows()
	if len(rows) != 1 || rows[0].id != "copy-commit-id" {
		t.Fatalf("want one copy-commit-id row, got %v", rows)
	}
	if rows[0].copyText != "0123456789abcdef0123456789abcdef01234567" {
		t.Errorf("copyText = %q, want the full hash", rows[0].copyText)
	}
	if rows[0].run == nil {
		t.Error("copy row must carry a run handler")
	}
}

func TestContextCopyRowsFiles(t *testing.T) {
	m := filesMenuModel() // Files panel, selected "dir/f.txt"
	rows := m.contextCopyRows()
	if len(rows) != 2 {
		t.Fatalf("want path+name copy rows, got %v", rows)
	}
	if rows[0].id != "copy-file-path" || rows[0].copyText != "dir/f.txt" {
		t.Errorf("row[0] = {%q,%q}, want copy-file-path dir/f.txt", rows[0].id, rows[0].copyText)
	}
	if rows[1].id != "copy-file-name" || rows[1].copyText != "f.txt" {
		t.Errorf("row[1] = {%q,%q}, want copy-file-name f.txt", rows[1].id, rows[1].copyText)
	}
}

func TestContextCopyRowsStaged(t *testing.T) {
	// The Staged panel shares m.status.Files with the Files panel, so the same
	// copy rows must resolve there.
	m := footerModel()
	m.loading = false
	m.focus = panelStaged
	m.status.Files = []model.FileStatus{{Path: "dir/g.txt", Kind: model.KindTracked, Staged: 'M', Unstaged: '.'}}
	rows := m.contextCopyRows()
	if len(rows) != 2 {
		t.Fatalf("want path+name copy rows on the Staged panel, got %v", rows)
	}
	if rows[0].id != "copy-file-path" || rows[0].copyText != "dir/g.txt" {
		t.Errorf("row[0] = {%q,%q}, want copy-file-path dir/g.txt", rows[0].id, rows[0].copyText)
	}
	if rows[1].id != "copy-file-name" || rows[1].copyText != "g.txt" {
		t.Errorf("row[1] = {%q,%q}, want copy-file-name g.txt", rows[1].id, rows[1].copyText)
	}
}

func TestContextCopyRowsEmpty(t *testing.T) {
	m := footerModel()
	m.focus = panelWorktrees // no copy rows defined for the Worktrees panel
	m.loading = false
	if rows := m.contextCopyRows(); len(rows) != 0 {
		t.Errorf("worktrees panel yields no copy rows, got %v", rows)
	}
}

func TestRunVisibleRowInvokesHandler(t *testing.T) {
	m := filesMenuModel()
	m = m.openActionMenu()
	// The first row is the copy-file-path handler row.
	if m.actionMenu.rows[0].id != "copy-file-path" {
		t.Fatalf("expected copy-file-path to lead the menu, got %q", m.actionMenu.rows[0].id)
	}
	res, cmd := m.runVisibleRow(0)
	if res.(Model).actionMenu != nil {
		t.Error("running a row must close the menu")
	}
	if cmd == nil {
		t.Error("the copy handler must return a clipboard command")
	}
}

func TestPruneRowOnRemotesTab(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelRemotes
	found := false
	for _, r := range availableActions(m) {
		if r.id == "prune-remotes" {
			found = true
		}
	}
	if !found {
		t.Fatal("Remotes tab . menu should offer Prune")
	}
}
