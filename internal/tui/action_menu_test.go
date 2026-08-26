package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/model"
)

func TestSynthKey(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	m := footerModel()
	m.loading = false
	u, _ := m.Update(keyMsg("."))
	mm := u.(Model)
	if mm.actionMenu == nil {
		t.Fatal(". must open the action menu")
	}
}

func TestActionMenuEscCloses(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	m := footerModel()
	m = m.pushLayer(&repoPopup{}) // a popup owns the keyboard
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
	t.Parallel()
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
	t.Parallel()
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

// Letters never run or close the menu — they type into the filter (q included,
// so q must NOT fall through to the quit binding either).
func TestActionMenuLetterTypesIntoFilter(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m.loading = false
	u, _ := m.Update(keyMsg("."))
	m = u.(Model)
	u, cmd := m.Update(keyMsg("q"))
	mm := u.(Model)
	if mm.actionMenu == nil {
		t.Fatal("q must type into the filter, not close the menu")
	}
	if cmd != nil {
		t.Fatal("q must not quit the app while the menu is open")
	}
	if mm.actionMenu.query != "q" {
		t.Fatalf("query = %q, want %q", mm.actionMenu.query, "q")
	}
}

// A row's replay key pressed directly must not run it anymore: space on the
// stage row stays filter input, and the menu stays open.
func TestActionMenuSpaceDoesNotRunRow(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m.loading = false
	m.focus = panelFiles
	m.status.Files = []model.FileStatus{{Path: "f.txt", Kind: model.KindTracked, Staged: '.', Unstaged: 'M'}}
	u, _ := m.Update(keyMsg(".")) // menu includes the stage row (replay key: space)
	m = u.(Model)
	u, _ = m.Update(keyMsg("space"))
	mm := u.(Model)
	if mm.actionMenu == nil {
		t.Fatal("space must not run the stage row; it extends the filter")
	}
	if mm.actionMenu.query != " " {
		t.Fatalf("query = %q, want a single space", mm.actionMenu.query)
	}
}

// Typing filters the visible rows exactly like the old / mode did — no /
// needed — and enter runs the highlighted match.
func TestActionMenuTypeToFilter(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m.loading = false
	m.focus = panelFiles
	m.status.Files = []model.FileStatus{{Path: "f.txt", Kind: model.KindTracked, Staged: '.', Unstaged: 'M'}}
	u, _ := m.Update(keyMsg("."))
	m = u.(Model)
	for _, k := range []string{"s", "t", "a", "g", "e"} {
		u, _ = m.Update(keyMsg(k))
		m = u.(Model)
	}
	a := m.actionMenu
	if a == nil || a.query != "stage" {
		t.Fatalf("query after typing = %v", a)
	}
	vis := a.visible()
	if len(vis) == 0 {
		t.Fatal("filter must leave the Stage file row visible")
	}
	for _, r := range vis {
		if !strings.Contains(strings.ToLower(r.label), "stage") {
			t.Errorf("row %q must match the query", r.label)
		}
	}
	u, _ = m.Update(keyMsg("enter"))
	if u.(Model).actionMenu != nil {
		t.Fatal("enter must run the highlighted row and close the menu")
	}
}

// Esc clears an active filter first; only the next esc closes the menu.
func TestActionMenuEscClearsFilterThenCloses(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m.loading = false
	u, _ := m.Update(keyMsg("."))
	m = u.(Model)
	u, _ = m.Update(keyMsg("x"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	if m.actionMenu == nil {
		t.Fatal("first esc must clear the filter, not close the menu")
	}
	if m.actionMenu.query != "" {
		t.Fatalf("query after esc = %q, want empty", m.actionMenu.query)
	}
	u, _ = m.Update(keyMsg("esc"))
	if u.(Model).actionMenu != nil {
		t.Fatal("second esc must close the menu")
	}
}

// ctrl+z cycles the display mode (z itself is filter text now).
func TestActionMenuCtrlZCyclesMode(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m.loading = false
	u, _ := m.Update(keyMsg("."))
	m = u.(Model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlZ})
	mm := u.(Model)
	if mm.actionMenu.mode != modeWrap {
		t.Fatalf("mode after ctrl+z = %v, want modeWrap", mm.actionMenu.mode)
	}
	if mm.actionMenu.query != "" {
		t.Fatalf("ctrl+z must not touch the query, got %q", mm.actionMenu.query)
	}
}

// No menu row may advertise a key hint — the [x]-style labels are footer-only.
func TestActionMenuLabelsCarryNoKeyHints(t *testing.T) {
	t.Parallel()
	for _, m := range []Model{footerModel(), filesMenuModel()} {
		m.loading = false
		for _, r := range availableActions(m) {
			if strings.Contains(r.label, "[") {
				t.Errorf("menu label %q must not contain a key hint", r.label)
			}
		}
	}
}

// Every menu-eligible registry binding (id, non-global scope) must have a clean
// menu label — or be explicitly footer-only because a dedicated direct-run row
// covers it. A new binding failing here: add an actionMenuLabel case.
func TestActionMenuLabelCoverage(t *testing.T) {
	t.Parallel()
	footerOnly := map[string]bool{
		"commit-message": true, // commitViewMessageRow / commitEditMessageRow
		"graph-window":   true, // graphWindowRows
	}
	for _, b := range contextBindings() {
		if b.id == "" || b.scope == scopeGlobal {
			continue
		}
		_, ok := actionMenuLabel(b.id)
		if ok == footerOnly[b.id] {
			t.Errorf("binding %q: menu label %v, footer-only %v — exactly one must hold", b.id, ok, footerOnly[b.id])
		}
	}
}

func TestActionMenuRenders(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m.loading = false
	u, _ := m.Update(keyMsg("."))
	out := ansi.Strip(u.(Model).View())
	if !strings.Contains(out, "Actions") || !strings.Contains(out, "New branch…") {
		t.Fatalf("rendered menu missing header/rows:\n%s", out)
	}
}

func TestContextCopyRowsCommits(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m.loading = false
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "0123456789abcdef0123456789abcdef01234567", Subject: "feat: do the thing"}}
	rows := m.contextCopyRows()
	id, okID := findRow(rows, "copy-commit-id")
	title, okTitle := findRow(rows, "copy-commit-title")
	if !okID || !okTitle {
		t.Fatalf("want copy-commit-id and copy-commit-title rows, got %v", rows)
	}
	if id.copyText != "0123456789abcdef0123456789abcdef01234567" {
		t.Errorf("commit-id copyText = %q, want the full hash", id.copyText)
	}
	if title.copyText != "feat: do the thing" {
		t.Errorf("commit-title copyText = %q, want the subject", title.copyText)
	}
	if id.run == nil || title.run == nil {
		t.Error("copy rows must carry a run handler")
	}
}

func TestContextCopyRowsFiles(t *testing.T) {
	t.Parallel()
	m := filesMenuModel() // Files panel, selected "dir/f.txt"; currentWorktree "/repo"
	rows := m.contextCopyRows()
	if len(rows) != 3 {
		t.Fatalf("want path+abspath+name copy rows, got %v", rows)
	}
	if rows[0].id != "copy-file-path" || rows[0].copyText != "dir/f.txt" {
		t.Errorf("row[0] = {%q,%q}, want copy-file-path dir/f.txt", rows[0].id, rows[0].copyText)
	}
	if rows[1].id != "copy-file-abspath" || rows[1].copyText != filepath.FromSlash("/repo/dir/f.txt") {
		t.Errorf("row[1] = {%q,%q}, want copy-file-abspath /repo/dir/f.txt", rows[1].id, rows[1].copyText)
	}
	if rows[2].id != "copy-file-name" || rows[2].copyText != "f.txt" {
		t.Errorf("row[2] = {%q,%q}, want copy-file-name f.txt", rows[2].id, rows[2].copyText)
	}
}

func TestContextCopyRowsStaged(t *testing.T) {
	t.Parallel()
	// The Staged panel shares m.status.Files with the Files panel, so the same
	// copy rows must resolve there.
	m := footerModel()
	m.loading = false
	m.focus = panelStaged
	m.status.Files = []model.FileStatus{{Path: "dir/g.txt", Kind: model.KindTracked, Staged: 'M', Unstaged: '.'}}
	rows := m.contextCopyRows()
	if len(rows) != 3 {
		t.Fatalf("want path+abspath+name copy rows on the Staged panel, got %v", rows)
	}
	if rows[0].id != "copy-file-path" || rows[0].copyText != "dir/g.txt" {
		t.Errorf("row[0] = {%q,%q}, want copy-file-path dir/g.txt", rows[0].id, rows[0].copyText)
	}
	if rows[1].id != "copy-file-abspath" || rows[1].copyText != filepath.FromSlash("/repo/dir/g.txt") {
		t.Errorf("row[1] = {%q,%q}, want copy-file-abspath /repo/dir/g.txt", rows[1].id, rows[1].copyText)
	}
	if rows[2].id != "copy-file-name" || rows[2].copyText != "g.txt" {
		t.Errorf("row[2] = {%q,%q}, want copy-file-name g.txt", rows[2].id, rows[2].copyText)
	}
}

func TestContextCopyRowsEmpty(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m.focus = panelWorktrees
	m.worktrees = nil // nothing selected → no copy rows
	m.loading = false
	if rows := m.contextCopyRows(); len(rows) != 0 {
		t.Errorf("worktrees panel with no selection yields no copy rows, got %v", rows)
	}
}

func TestContextCopyRowsWorktrees(t *testing.T) {
	t.Parallel()
	m := footerModel() // worktrees: /repo (main), /repo/wt-x (feat/x)
	m.loading = false
	m.focus = panelWorktrees
	m.sel[panelWorktrees] = 1
	rows := m.contextCopyRows()
	r, ok := findRow(rows, "copy-worktree-abspath")
	if !ok {
		t.Fatalf("want copy-worktree-abspath row, got %v", rows)
	}
	if r.copyText != "/repo/wt-x" {
		t.Errorf("copyText = %q, want the worktree's absolute path", r.copyText)
	}
	if r.run == nil {
		t.Error("copy rows must carry a run handler")
	}
}

func TestContextCopyRowsBranchWorktreePath(t *testing.T) {
	t.Parallel()
	m := footerModel() // main → /repo (current), feat/x → /repo/wt-x
	m.loading = false
	m.focus = panelBranches

	m.sel[panelBranches] = 1 // feat/x, checked out in another worktree
	r, ok := findRow(m.contextCopyRows(), "copy-worktree-abspath")
	if !ok {
		t.Fatal("branch checked out in a worktree must offer copy-worktree-abspath")
	}
	if r.copyText != "/repo/wt-x" {
		t.Errorf("copyText = %q, want the branch's worktree path", r.copyText)
	}

	m.sel[panelBranches] = 0 // head branch → the current worktree counts too
	if r, ok = findRow(m.contextCopyRows(), "copy-worktree-abspath"); !ok || r.copyText != "/repo" {
		t.Errorf("head branch row = (%v, %v), want copy-worktree-abspath /repo", r.copyText, ok)
	}

	m.branches = append(m.branches, model.Branch{Name: "feat/y"})
	m.sel[panelBranches] = 2 // not checked out anywhere → no row
	if _, ok := findRow(m.contextCopyRows(), "copy-worktree-abspath"); ok {
		t.Error("branch without a worktree must not offer copy-worktree-abspath")
	}
}

func TestRunVisibleRowInvokesHandler(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

func TestActionMenuMoveWraps(t *testing.T) {
	t.Parallel()
	a := &actionMenu{rows: []actionRow{{id: "a"}, {id: "b"}, {id: "c"}}}
	a.sel = 0
	a.move(-1) // up from the first row wraps to the last
	if a.sel != 2 {
		t.Fatalf("up from first → %d, want 2 (wrap to last)", a.sel)
	}
	a.sel = 2
	a.move(1) // down from the last row wraps to the first
	if a.sel != 0 {
		t.Fatalf("down from last → %d, want 0 (wrap to first)", a.sel)
	}
	a.sel = 0
	a.move(1) // ordinary move still advances
	if a.sel != 1 {
		t.Fatalf("down from first → %d, want 1", a.sel)
	}
}
