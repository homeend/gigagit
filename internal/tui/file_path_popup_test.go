package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRepoRelPath(t *testing.T) {
	root := filepath.FromSlash("/repo")
	outside := filepath.FromSlash("/elsewhere/x.go")
	cases := []struct{ name, in, want string }{
		{"already relative", "internal/tui/model.go", "internal/tui/model.go"},
		{"dot-slash relative", "./internal/x.go", "internal/x.go"},
		{"absolute inside repo", filepath.FromSlash("/repo/internal/x.go"), "internal/x.go"},
		{"absolute outside repo", outside, filepath.ToSlash(filepath.Clean(outside))},
		{"blank", "   ", ""},
	}
	for _, c := range cases {
		if got := repoRelPath(root, c.in); got != c.want {
			t.Errorf("%s: repoRelPath(%q,%q)=%q want %q", c.name, root, c.in, got, c.want)
		}
	}
}

// palettePick opens the palette, navigates to the command labelled label, and
// presses enter. Reused by every palette entry's test.
func palettePick(t *testing.T, m Model, label string) (Model, tea.Cmd) {
	t.Helper()
	m, _ = send(m, keyType(tea.KeyCtrlP))
	p := layerOf[*commandPalette](m)
	if p == nil {
		t.Fatal("ctrl+p did not open the palette")
	}
	idx := -1
	for i, c := range p.cmds {
		if c.label == label {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("palette has no command %q", label)
	}
	for j := 0; j < idx; j++ {
		m, _ = send(m, keyType(tea.KeyDown))
	}
	return send(m, keyType(tea.KeyEnter))
}

func TestPaletteFileHistoryOpensPopup(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "File history")
	p := layerOf[*filePathPopup](m)
	if p == nil || p.kind != filePathHistory {
		t.Fatal("File history should open a history file-path popup")
	}
	if layerOf[*commandPalette](m) == nil {
		t.Fatal("the palette should stay underneath as the source")
	}
}

func TestFilePathPopupHistoryOpensSurface(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "File history")
	m = typeRunes(t, m, "README.md")
	m, _ = send(m, keyType(tea.KeyEnter))
	if layerOf[*filePathPopup](m) != nil || layerOf[*commandPalette](m) != nil {
		t.Fatal("submit must unwind both the popup and the palette")
	}
	hv := layerOf[*historyView](m)
	if hv == nil {
		t.Fatal("submit should open the history surface")
	}
	if hv.ctx.path != "README.md" || hv.ctx.rev != "" {
		t.Errorf("navContext = %+v, want {path:README.md rev:}", hv.ctx)
	}
}

func TestFilePathPopupBlameOpensSurface(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "File blame")
	m = typeRunes(t, m, "README.md")
	m, _ = send(m, keyType(tea.KeyEnter))
	bv := layerOf[*blameView](m)
	if bv == nil {
		t.Fatal("submit should open the blame surface")
	}
	if bv.ctx.path != "README.md" || bv.ctx.rev != "" {
		t.Errorf("navContext = %+v, want {path:README.md rev:}", bv.ctx)
	}
}

func TestFilePathPopupEmptyKeepsOpen(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m, _ = send(m, keyType(tea.KeyEnter)) // empty input
	if layerOf[*filePathPopup](m) == nil {
		t.Fatal("enter with an empty path must keep the popup open")
	}
}

func TestFilePathPopupEscRevealsPalette(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "File history")
	m, _ = send(m, keyType(tea.KeyEsc))
	if layerOf[*filePathPopup](m) != nil {
		t.Fatal("esc should close the file-path popup")
	}
	if p := layerOf[*commandPalette](m); p == nil || p != m.topLayer() {
		t.Fatal("esc should reveal the palette beneath")
	}
}

func TestFilePathPopupAllowsSpaces(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m, _ = send(m, key("a"))
	m, _ = send(m, keyType(tea.KeySpace))
	m, _ = send(m, key("b"))
	p := layerOf[*filePathPopup](m)
	if p == nil || p.input.Value() != "a b" {
		t.Fatalf("path popup must accept spaces; input=%q", p.input.Value())
	}
}

// A space typed into a path survives normalization all the way into the opened
// history surface's navContext — proving the space is preserved end-to-end, not
// just held in the textfield buffer.
func TestFilePathPopupSpaceReachesNavContext(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m = typeRunes(t, m, "a b.txt")
	m, _ = send(m, keyType(tea.KeyEnter))
	hv := layerOf[*historyView](m)
	if hv == nil {
		t.Fatal("submit should open the history surface")
	}
	if hv.ctx.path != "a b.txt" {
		t.Errorf("navContext.path = %q, want %q (space must survive normalization)", hv.ctx.path, "a b.txt")
	}
}

// The render path draws the popup box for both kinds, exercising p.title(),
// p.box(), and p.render() (guards the green-unit/broken-render class the way
// gotoCommitPopup's TestGotoCommitRendersWithError does).
func TestFilePathPopupRenders(t *testing.T) {
	cases := []struct {
		label string
		title string
	}{
		{"File history", "File history"},
		{"File blame", "File blame"},
	}
	for _, c := range cases {
		m := gotoModel(t, gotoFullHash)
		m, _ = palettePick(t, m, c.label)
		out := m.View()
		if !strings.Contains(out, c.title) {
			t.Errorf("%s render missing title %q", c.label, c.title)
		}
		if !strings.Contains(out, "[enter] show") {
			t.Errorf("%s render missing the %q hint", c.label, "[enter] show")
		}
	}
}

func TestPaletteFindOpensFinder(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "Find")
	if layerOf[*fileFinderPopup](m) == nil {
		t.Fatal("Find should open the fuzzy file finder")
	}
	if layerOf[*commandPalette](m) != nil {
		t.Fatal("Find replaces the palette (it does not stay beneath)")
	}
}
