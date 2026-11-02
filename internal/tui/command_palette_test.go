package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ctrl+p opens the command palette.
func TestCommandPaletteOpens(t *testing.T) {
	t.Parallel()
	m := gotoModel(t, gotoFullHash)
	m, _ = send(m, keyType(tea.KeyCtrlP))
	if layerOf[*commandPalette](m) == nil {
		t.Fatal("ctrl+p should open the command palette")
	}
}

// enter on "Show commit" opens the show-commit popup ON TOP of the palette (the
// palette is the source, so it stays underneath for esc to reveal).
func TestCommandPaletteEnterRunsShowCommit(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	m := gotoModel(t, gotoFullHash)
	m, _ = send(m, keyType(tea.KeyCtrlP))
	m, _ = send(m, keyType(tea.KeyEsc))
	if layerOf[*commandPalette](m) != nil {
		t.Fatal("esc should close the command palette")
	}
}

// The render path draws the palette (guards the green-unit/broken-render class).
func TestCommandPaletteRenders(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	want := []struct{ label, keyHint string }{
		{"Agents, AI tasks, overviews & notes…", "ctrl+\\"},
		{"Apply patch…", ""},
		{"Branch versions…", ""},
		{"Browse remote branches", ""},
		{"Compare with link…", ""},
		{"File blame", ""},
		{"File history", ""},
		{"Find", "F"},
		{"Git config explorer", ""},
		{"Leave workspace", ""},
		{"Open gg:// link…", "#"},
		{"Open repo", ""},
		{"Open shell", "ctrl+o"},
		{"Quit", "q"},
		{"Switch to web UI", ""}, // "web" finds it
		{"Run shell command…", ""},
		{"Search pull requests…", "A"},
		{"Set up agent skills (using-gg)", ""},
		{"Show commit", "#"},
		{"Text templates…", "alt+x"},
		{"Version", ""},
		{"View all notes…", ""},
		{"Workspaces…", ""},
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

// The palette reaches the ctrl+\\ popup: enter opens it in the palette's place,
// so its own close and its enter land on what was under the palette.
func TestCommandPaletteOpensSessionsPopup(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m, _ = m.openCommandPalette()
	p, ok := m.topLayer().(*commandPalette)
	if !ok {
		t.Fatalf("top layer = %T, want the palette", m.topLayer())
	}
	p.query = "agents, ai"
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if _, ok := m.topLayer().(*sessionsPopup); !ok {
		t.Fatalf("top layer = %T, want the sessions popup", m.topLayer())
	}
	if layerOf[*commandPalette](m) != nil {
		t.Error("the palette must not stay under the sessions popup")
	}
}

// The entry shows the key this user pressed, [console] sessions_key included.
func TestCommandPaletteSessionsHintFollowsConfig(t *testing.T) {
	t.Parallel()
	m := footerModel()
	hint := func(m Model) string {
		for _, c := range m.availablePaletteCommands() {
			if c.label == "Agents, AI tasks, overviews & notes…" {
				return c.keyHint
			}
		}
		t.Fatal("entry missing from the palette")
		return ""
	}
	if got := hint(m); got != "ctrl+\\" {
		t.Errorf("default hint = %q", got)
	}
	m.cfg.Console.SessionsKey = "alt+j"
	if got := hint(m); got != "alt+j" {
		t.Errorf("configured hint = %q, want alt+j", got)
	}
}

// Quit in the palette ends in the tea.QuitMsg every quit does, so quitFilter's
// live-session guard still holds it.
func TestCommandPaletteQuit(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m, _ = m.openCommandPalette()
	p := m.topLayer().(*commandPalette)
	p.query = "quit"
	m, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("command does not quit")
	}
	if layerOf[*commandPalette](m) != nil {
		t.Error("the palette must close: a held quit opens the sessions popup in its place")
	}
}
