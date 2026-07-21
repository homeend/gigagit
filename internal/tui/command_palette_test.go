package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ctrl+p opens the command palette.
func TestCommandPaletteOpens(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = send(m, keyType(tea.KeyCtrlP))
	if layerOf[*commandPalette](m) == nil {
		t.Fatal("ctrl+p should open the command palette")
	}
}

// enter on "Show commit" opens the show-commit popup ON TOP of the palette (the
// palette is the source, so it stays underneath for esc to reveal).
func TestCommandPaletteEnterRunsShowCommit(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "Show commit")
	if layerOf[*gotoCommitPopup](m) == nil {
		t.Fatal("Show commit should open the goto-commit popup")
	}
	if layerOf[*commandPalette](m) == nil {
		t.Fatal("the palette should stay underneath as the source")
	}
}

// esc out of the show-commit popup returns to the palette it was opened from.
func TestCommandPaletteEscFromGotoReturnsToPalette(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "Show commit") // opens goto over the palette
	m, _ = send(m, keyType(tea.KeyEsc))     // back out of goto
	if layerOf[*gotoCommitPopup](m) != nil {
		t.Fatal("esc should close the show-commit popup")
	}
	if p := layerOf[*commandPalette](m); p == nil || p != m.topLayer() {
		t.Fatal("esc out of the show-commit popup should reveal the palette")
	}
}

// A successful resolve from the palette flow unwinds BOTH popups — the files view
// must not open over a stale palette.
func TestCommandPaletteResolveUnwindsBoth(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "Show commit")
	m = typeRunes(t, m, "abc")
	m, cmd := send(m, keyType(tea.KeyEnter))
	m, _ = send(m, cmd()) // run the resolve
	if layerOf[*gotoCommitPopup](m) != nil || layerOf[*commandPalette](m) != nil {
		t.Fatal("a successful resolve should leave neither the goto popup nor the palette")
	}
	if m.filesView == nil {
		t.Fatal("a successful resolve should open the files view")
	}
}

// esc closes the palette.
func TestCommandPaletteEscCloses(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = send(m, keyType(tea.KeyCtrlP))
	m, _ = send(m, keyType(tea.KeyEsc))
	if layerOf[*commandPalette](m) != nil {
		t.Fatal("esc should close the command palette")
	}
}

// The render path draws the palette (guards the green-unit/broken-render class).
func TestCommandPaletteRenders(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = send(m, keyType(tea.KeyCtrlP))
	out := m.View()
	for _, want := range []string{"Commands", "Show commit", "#"} {
		if !strings.Contains(out, want) {
			t.Errorf("palette render missing %q", want)
		}
	}
}

func TestPaletteRegistryOrder(t *testing.T) {
	want := []struct{ label, keyHint string }{
		{"Apply patch…", ""},
		{"Branch versions…", ""},
		{"File blame", ""},
		{"File history", ""},
		{"Find", "F"},
		{"Git config explorer", ""},
		{"Open repo", ""},
		{"Open shell", "ctrl+o"},
		{"Run shell command…", ""},
		{"Set up agent skills (using-gg)", ""},
		{"Show commit", "#"},
	}
	cmds := paletteCommands()
	if len(cmds) != len(want) {
		t.Fatalf("palette has %d commands, want %d", len(cmds), len(want))
	}
	for i, w := range want {
		if cmds[i].label != w.label || cmds[i].keyHint != w.keyHint {
			t.Errorf("cmd %d = {%q,%q}, want {%q,%q}", i, cmds[i].label, cmds[i].keyHint, w.label, w.keyHint)
		}
	}
}
