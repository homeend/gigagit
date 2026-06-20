package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestResolveEditorPrecedence(t *testing.T) {
	t.Setenv("VISUAL", "myvis")
	t.Setenv("EDITOR", "myed")
	if got := resolveEditor(); got != "myvis" {
		t.Fatalf("VISUAL should win, got %q", got)
	}
	t.Setenv("VISUAL", "")
	if got := resolveEditor(); got != "myed" {
		t.Fatalf("EDITOR fallback, got %q", got)
	}
	t.Setenv("EDITOR", "")
	if got := resolveEditor(); got != defaultEditor() {
		t.Fatalf("default fallback, got %q", got)
	}
}

// A whitespace-only env value must be treated as unset (else editorCommand
// would panic on an empty Fields slice).
func TestResolveEditorIgnoresWhitespace(t *testing.T) {
	t.Setenv("VISUAL", "   ")
	t.Setenv("EDITOR", "")
	if got := resolveEditor(); got != defaultEditor() {
		t.Fatalf("whitespace VISUAL should fall through, got %q", got)
	}
	// And the built command must not panic.
	cmd := editorCommand(resolveEditor(), "/x")
	if len(cmd.Args) == 0 || cmd.Args[len(cmd.Args)-1] != "/x" {
		t.Fatalf("args = %v", cmd.Args)
	}
}

func TestDefaultEditorNonEmpty(t *testing.T) {
	if defaultEditor() == "" {
		t.Fatal("default editor must be non-empty")
	}
}

func TestEditorCommandArgv(t *testing.T) {
	cmd := editorCommand("code -w", "/wt/a/b.go")
	want := []string{"code", "-w", "/wt/a/b.go"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("args = %v, want %v", cmd.Args, want)
	}
	cmd2 := editorCommand("vim", "/wt/x")
	if !reflect.DeepEqual(cmd2.Args, []string{"vim", "/wt/x"}) {
		t.Fatalf("args = %v", cmd2.Args)
	}
}

func editModel(files []model.FileStatus, sel int) Model {
	m := New(nil)
	m.width, m.height = 80, 30
	m.loading = false
	m.focus = panelFiles
	m.currentWorktree = "/wt"
	m.status = model.WorkingTreeStatus{Files: files}
	m.sel[panelFiles] = sel
	return m
}

func TestFileEditRowGating(t *testing.T) {
	f := model.FileStatus{Path: "a.go", Kind: model.KindTracked, Unstaged: 'M'}
	m := editModel([]model.FileStatus{f}, 0)
	r, ok := m.fileEditRow()
	if !ok || r.id != "edit-file" || r.label != "Edit in editor" {
		t.Fatalf("want edit row, got %+v ok=%v", r, ok)
	}
	var found bool
	for _, a := range availableActions(m) {
		if a.id == "edit-file" {
			found = true
		}
	}
	if !found {
		t.Fatal("availableActions missing edit-file")
	}

	m.focus = panelStaged
	if _, ok := m.fileEditRow(); ok {
		t.Fatal("no edit row on Staged panel")
	}
	m.focus = panelFiles
	m.running = true
	if _, ok := m.fileEditRow(); ok {
		t.Fatal("no edit row while running")
	}
}

func TestEditorFinishedMsgRefreshes(t *testing.T) {
	m := editModel([]model.FileStatus{{Path: "a.go", Kind: model.KindTracked}}, 0)
	m2, cmd := m.Update(editorFinishedMsg{path: "a.go", err: errors.New("boom")})
	if cmd == nil {
		t.Fatal("want a refresh cmd")
	}
	if !strings.Contains(m2.(Model).statusMsg, "boom") {
		t.Fatalf("status = %q", m2.(Model).statusMsg)
	}
	_, cmd2 := m.Update(editorFinishedMsg{path: "a.go"})
	if cmd2 == nil {
		t.Fatal("want a refresh cmd on success")
	}
}
