package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// The full round-trip with a switcher ON the layer stack: editorViewMsg must
// reach the central Update handler (not be swallowed by the popup) so the editor
// launches from g/G, and editorViewFinishedMsg must clean up the temp even with
// the popup still open. Guards against routing custom messages to topLayer().
func TestOpenExternalRoundTripWithPopupOpen(t *testing.T) {
	path, err := writeReadOnlyTempFile("x.go", []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	m := bmPopupModel(model.Bookmark{ID: "b1", State: model.StateUnstaged, Worktree: "/wt", Path: "x.go"})
	mm, cmd := m.Update(editorViewMsg{path: path, name: "x.go"})
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("editorViewMsg must reach the central handler (launch the editor) with a popup open")
	}
	if m.bookmarkSwitcher() == nil {
		t.Fatal("the switcher should remain on the stack during the editor launch")
	}
	mm, _ = m.Update(editorViewFinishedMsg{path: path, name: "x.go"})
	m = mm.(Model)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the temp file must be removed even with the popup open, stat err = %v", err)
	}
}

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
