package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/hunkpick"
	"github.com/gigagit/gg/internal/model"
)

func ids(rows []actionRow) map[string]bool {
	m := map[string]bool{}
	for _, r := range rows {
		m[r.id] = true
	}
	return m
}

func findRow(rows []actionRow, id string) (actionRow, bool) {
	for _, r := range rows {
		if r.id == id {
			return r, true
		}
	}
	return actionRow{}, false
}

func TestInContentWindowTrueCases(t *testing.T) {
	cases := map[string]func(Model) Model{
		"diffView":  func(m Model) Model { m.diffView = &diffView{title: "a.go"}; return m },
		"filesView": func(m Model) Model { m.filesView = &contentPopup{}; return m },
		"stashView": func(m Model) Model { m.stashView = &stashView{}; m.focus = panelCommits; return m },
		"history":   func(m Model) Model { return m.pushSurface(newHistoryView(navContext{path: "a.go"})) },
		"blame":     func(m Model) Model { return m.pushSurface(newBlameView(navContext{path: "a.go"})) },
	}
	for name, setup := range cases {
		m := setup(footerModel())
		if !m.inContentWindow() {
			t.Errorf("%s: inContentWindow() = false, want true", name)
		}
	}
}

func TestInContentWindowFalseCases(t *testing.T) {
	if footerModel().inContentWindow() {
		t.Error("base panel layout: inContentWindow() = true, want false")
	}
	ire := footerModel().pushSurface(newIrebaseEditor("feat", "main", nil, "gg"))
	if ire.inContentWindow() {
		t.Error("irebase editor is a transient editor, not a content window")
	}
	hp := footerModel().pushSurface(newStagePicker("f.txt", &hunkpick.Doc{}))
	if hp.inContentWindow() {
		t.Error("hunk picker is a transient editor, not a content window")
	}
}

func TestContextCopyRowsDiffView(t *testing.T) {
	m := footerModel()
	m.diffView = &diffView{title: "dir/b.go", rev: "deadbeefcafe0000"}
	rows := m.contextCopyRows()
	got := ids(rows)
	if !got["copy-file-path"] || !got["copy-file-name"] || !got["copy-commit-id"] {
		t.Fatalf("diff view rows = %v, want path/name/commit-id", got)
	}
	if r, _ := findRow(rows, "copy-file-path"); r.copyText != "dir/b.go" {
		t.Errorf("path copyText = %q", r.copyText)
	}
	if r, _ := findRow(rows, "copy-file-name"); r.copyText != "b.go" {
		t.Errorf("name copyText = %q", r.copyText)
	}
	if r, _ := findRow(rows, "copy-commit-id"); r.copyText != "deadbeefcafe0000" {
		t.Errorf("commit copyText = %q", r.copyText)
	}
}

func TestContextCopyRowsDiffViewWorkingTree(t *testing.T) {
	m := footerModel()
	m.diffView = &diffView{title: "b.go", rev: ""} // working tree: no commit
	if got := ids(m.contextCopyRows()); got["copy-commit-id"] {
		t.Errorf("working-tree diff must not offer copy-commit-id, got %v", got)
	}
}

func TestContextCopyRowsHistory(t *testing.T) {
	m := footerModel().pushSurface(newHistoryView(navContext{path: "x/y.go", rev: "abc123"}))
	rows := m.contextCopyRows()
	got := ids(rows)
	if !got["copy-file-path"] || !got["copy-file-name"] || !got["copy-commit-id"] {
		t.Fatalf("history rows = %v, want path/name/commit-id", got)
	}
	if r, _ := findRow(rows, "copy-file-path"); r.copyText != "x/y.go" {
		t.Errorf("path copyText = %q", r.copyText)
	}
}

func TestContextCopyRowsFilesTree(t *testing.T) {
	m := footerModel()
	m.filesView = &contentPopup{lines: []contentLine{{text: "dir/f.go", path: "dir/f.go"}}}
	m.filesTreeFocused = true
	m.filesHash = "feedface1234"
	rows := m.contextCopyRows()
	got := ids(rows)
	if !got["copy-file-path"] || !got["copy-file-name"] || !got["copy-commit-id"] {
		t.Fatalf("files-tree rows = %v, want path/name/commit-id", got)
	}
	if r, _ := findRow(rows, "copy-file-name"); r.copyText != "f.go" {
		t.Errorf("name copyText = %q", r.copyText)
	}
}

func TestContextCopyRowsStashFileTree(t *testing.T) {
	// A stash file tree has no commit id (filesHash == "").
	m := footerModel()
	m.filesView = &contentPopup{lines: []contentLine{{text: "dir/f.go", path: "dir/f.go"}}}
	m.filesTreeFocused = true
	m.filesHash = ""
	got := ids(m.contextCopyRows())
	if got["copy-commit-id"] {
		t.Errorf("stash file tree must not offer copy-commit-id, got %v", got)
	}
	if !got["copy-file-path"] || !got["copy-file-name"] {
		t.Errorf("stash file tree should offer path/name, got %v", got)
	}
}

func TestContextCopyRowsStashList(t *testing.T) {
	m := footerModel()
	m.stashView = &stashView{entries: []model.StashEntry{{Ref: "stash@{0}", Subject: "wip"}}, sel: 0}
	m.focus = panelCommits
	rows := m.contextCopyRows()
	r, ok := findRow(rows, "copy-stash-ref")
	if !ok || r.copyText != "stash@{0}" {
		t.Fatalf("stash list rows = %v, want copy-stash-ref stash@{0}", rows)
	}
}

// When a diff view AND a pushed history/blame are both live (open a diff, then
// h/b), the stack surface is the top visible/key-receiving layer, so the menu
// must describe it — not the diff underneath. Precedence must match dispatch.
func TestContextCopyRowsStackBeatsDiffView(t *testing.T) {
	m := footerModel()
	m.diffView = &diffView{title: "old/a.go", rev: "aaaa1111"}
	m = m.pushSurface(newBlameView(navContext{path: "new/b.go", rev: "bbbb2222"}))
	rows := m.contextCopyRows()
	if r, _ := findRow(rows, "copy-file-path"); r.copyText != "new/b.go" {
		t.Errorf("path copyText = %q, want the top (blame) surface's path new/b.go", r.copyText)
	}
	if r, _ := findRow(rows, "copy-commit-id"); r.copyText != "bbbb2222" {
		t.Errorf("commit copyText = %q, want the top (blame) surface's rev bbbb2222", r.copyText)
	}
}

func TestContextCopyRowsBranchName(t *testing.T) {
	m := footerModel()
	m.focus = panelBranches // selection defaults to row 0 = "main"
	rows := m.contextCopyRows()
	r, ok := findRow(rows, "copy-branch-name")
	if !ok || r.copyText != "main" {
		t.Fatalf("branches panel rows = %v, want copy-branch-name=main", rows)
	}
}

func TestContextCopyRowsRemoteName(t *testing.T) {
	m := footerModel()
	m.focus = panelRemotes
	m.remoteBranches = []model.RemoteBranch{{Name: "origin/foo", Remote: "origin", Branch: "foo"}}
	rows := m.contextCopyRows()
	r, ok := findRow(rows, "copy-branch-name")
	if !ok || r.copyText != "origin/foo" {
		t.Fatalf("remotes panel rows = %v, want copy-branch-name=origin/foo", rows)
	}
}

func TestAvailableActionsContentWindowCopyOnly(t *testing.T) {
	// File tree open while focus is still panelCommits: the commit-files [l]
	// binding is available, but the menu must list ONLY copy rows (replaying l
	// would close the window).
	m := footerModel()
	m.commits = []model.Commit{{Hash: "0123456789abcdef0123456789abcdef01234567", Subject: "x"}}
	m.filesView = &contentPopup{lines: []contentLine{{text: "dir/f.go", path: "dir/f.go"}}}
	m.filesTreeFocused = true
	m.filesHash = "0123456789abcdef0123456789abcdef01234567"
	got := ids(availableActions(m))
	if got["commit-files"] {
		t.Errorf("content-window menu leaked the commit-files row: %v", got)
	}
	if !got["copy-file-path"] || !got["copy-commit-id"] {
		t.Errorf("content-window menu should offer copy rows, got %v", got)
	}
}
