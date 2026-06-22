package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

// e on a file bookmark resolves its bytes into the read-only temp file the
// editorViewMsg carries.
func TestBookmarkOpenExternalResolvesContent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := bmPopupModel(model.Bookmark{ID: "b1", State: model.StateUnstaged, Worktree: dir, Path: "a.go"})
	mm, cmd := m.Update(keyMsg("e"))
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("e should dispatch a resolve/open command")
	}
	if m.bookmarkSwitcher() == nil {
		t.Fatal("the switcher stays open while the editor is launched")
	}
	msg, ok := cmd().(editorViewMsg)
	if !ok {
		t.Fatalf("want editorViewMsg, got %T", cmd())
	}
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	defer removeTempFile(msg.path)
	data, _ := os.ReadFile(msg.path)
	if string(data) != "package a\n" {
		t.Fatalf("temp file should hold the bookmarked file's bytes, got %q", data)
	}
}

// A commit-pointer bookmark has no file content, so e is a guarded no-op (notice,
// no editor) — mirroring p/m.
func TestCommitBookmarkOpenExternalIsNoop(t *testing.T) {
	m := commitBmPopupModel(t)
	mm, cmd := m.Update(keyMsg("e"))
	m = mm.(Model)
	if m.bookmarkSwitcher() == nil {
		t.Fatal("e on a commit bookmark must not leave/replace the switcher")
	}
	if cmd != nil {
		t.Fatal("e on a commit bookmark must not launch an editor")
	}
	if m.statusMsg == "" {
		t.Fatal("expected a notice that external view is unavailable for a commit bookmark")
	}
}

// e on a shelf entry dispatches the resolve/open command and keeps the switcher.
func TestShelfOpenExternalDispatches(t *testing.T) {
	m := shelfPopModel(shEntry("a", "x.go"))
	mm, cmd := m.Update(keyMsg("e"))
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("e should dispatch a resolve/open command")
	}
	if m.shelfSwitcher() == nil {
		t.Fatal("the switcher stays open while the editor is launched")
	}
}

// In compare mode the file-action keys are inert, so e must not launch anything.
func TestShelfOpenExternalInertInCompareMode(t *testing.T) {
	m := shelfPopModel(shEntry("a", "x.go"))
	m.shelfSwitcher().compareRef = &model.FileRef{Source: model.SourceUnstaged, Path: "focused.go"}
	_, cmd := m.Update(keyMsg("e"))
	if cmd != nil {
		t.Fatal("e must be inert while the shelf switcher is in compare mode")
	}
}
