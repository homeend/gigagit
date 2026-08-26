package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPaletteOpenRepoOpensPopup(t *testing.T) {
	t.Parallel()
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "Open repo")
	if layerOf[*repoPathPopup](m) == nil {
		t.Fatal("Open repo should open the repo-path popup")
	}
	if layerOf[*commandPalette](m) == nil {
		t.Fatal("the palette should stay underneath")
	}
}

func TestRepoPathPopupGoodPathReRoots(t *testing.T) {
	t.Parallel()
	dir, _ := newRepoDir(t)
	want := filepath.Clean(gitOut(t, dir, "rev-parse", "--show-toplevel")) // TopLevel normalizes to native notation

	m := footerModel()
	m, _ = m.openRepoPathPopup()
	m = typeRunes(t, m, dir)
	m, cmd := send(m, keyType(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter on a non-empty path should fire the resolve cmd")
	}
	m, _ = send(m, cmd()) // run the real TopLevel probe + reRoot
	if layerOf[*repoPathPopup](m) != nil {
		t.Fatal("a valid repo path should close the popup")
	}
	if m.switchTarget != want {
		t.Errorf("switchTarget = %q, want the resolved top-level %q", m.switchTarget, want)
	}
	if !m.loading {
		t.Error("reRoot should put the model into the loading state")
	}
}

func TestRepoPathPopupSubdirResolvesToRoot(t *testing.T) {
	t.Parallel()
	dir, _ := newRepoDir(t)
	want := filepath.Clean(gitOut(t, dir, "rev-parse", "--show-toplevel")) // TopLevel normalizes to native notation
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	m := footerModel()
	m, _ = m.openRepoPathPopup()
	m = typeRunes(t, m, sub)
	m, cmd := send(m, keyType(tea.KeyEnter))
	m, _ = send(m, cmd())
	if m.switchTarget != want {
		t.Errorf("a subdirectory path should resolve to the repo root %q, got %q", want, m.switchTarget)
	}
}

func TestRepoPathPopupBadPathInlineError(t *testing.T) {
	t.Parallel()
	nonRepo := t.TempDir() // not a git repo

	m := footerModel()
	m, _ = m.openRepoPathPopup()
	m = typeRunes(t, m, nonRepo)
	m, cmd := send(m, keyType(tea.KeyEnter))
	m, _ = send(m, cmd())
	p := layerOf[*repoPathPopup](m)
	if p == nil {
		t.Fatal("a non-repo path must keep the popup open")
	}
	if p.err == "" {
		t.Error("a non-repo path must set an inline error")
	}
	if p.resolving {
		t.Error("resolving should be cleared after the result lands")
	}
	if m.switchTarget != "" {
		t.Error("a failed validation must not switch repos")
	}
}

func TestRepoPathPopupStaleResolveRejected(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m, _ = m.openRepoPathPopup()
	m = typeRunes(t, m, "a")
	m, _ = send(m, keyType(tea.KeyEnter)) // fires resolve for "a"
	m = typeRunes(t, m, "b")              // input is now "ab"
	m, _ = send(m, repoResolvedMsg{path: "a", top: "/x"})
	if layerOf[*repoPathPopup](m) == nil {
		t.Fatal("a stale resolve (input edited) must not close the popup")
	}
	if m.switchTarget == "/x" {
		t.Fatal("a stale resolve must not switch repos")
	}
}

func TestRepoPathPopupEscCloses(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m, _ = m.openRepoPathPopup()
	m, _ = send(m, keyType(tea.KeyEsc))
	if layerOf[*repoPathPopup](m) != nil {
		t.Fatal("esc should close the repo-path popup")
	}
}
