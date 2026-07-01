package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/config"
)

func hookTypeRunes(m Model, p *hookEditorPopup, s string) Model {
	for _, r := range s {
		m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func TestHookEditorSeedsFromConfig(t *testing.T) {
	m := Model{}
	m.cfg.Worktree.PostCreateHook = "echo seeded"
	m = m.openHookEditor()
	p := layerOf[*hookEditorPopup](m)
	if p == nil {
		t.Fatal("editor not pushed")
	}
	if p.buf.Value() != "echo seeded" {
		t.Fatalf("seed = %q, want 'echo seeded'", p.buf.Value())
	}
}

func TestHookEditorSavesToRepoConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gg.toml")
	m := Model{}
	m.repoConfigPath = path
	m = m.openHookEditor()
	p := layerOf[*hookEditorPopup](m)
	m = hookTypeRunes(m, p, "echo one")
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter}) // newline, not submit
	m = hookTypeRunes(m, p, "echo two")
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})

	if m.cfg.Worktree.PostCreateHook != "echo one\necho two" {
		t.Fatalf("in-memory cfg = %q", m.cfg.Worktree.PostCreateHook)
	}
	cfg, err := config.Load(filepath.Join(t.TempDir(), "ng.toml"), path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Worktree.PostCreateHook != "echo one\necho two\n" {
		t.Fatalf("persisted = %q", cfg.Worktree.PostCreateHook)
	}
	if layerOf[*hookEditorPopup](m) != nil {
		t.Fatal("editor should close after save")
	}
	_ = os.Remove
}

func TestHookEditorEscDoesNotSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gg.toml")
	m := Model{}
	m.repoConfigPath = path
	m = m.openHookEditor()
	p := layerOf[*hookEditorPopup](m)
	m = hookTypeRunes(m, p, "echo nope")
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("esc must not write config")
	}
	if layerOf[*hookEditorPopup](m) != nil {
		t.Fatal("editor should close on esc")
	}
}

// The editor box must never be taller than the terminal, or overlayCenter gives
// a negative top offset and the bottom (the [ctrl+s] save help line) is clipped
// off-screen — the "can't see the bottom of the hook editor" bug.
func TestHookEditorBoxFitsTerminalHeight(t *testing.T) {
	for _, h := range []int{14, 24, 40} {
		p := &hookEditorPopup{buf: newTextField("echo one\necho two\necho three")}
		box := p.box(Model{}, 100, h)
		if got := lipgloss.Height(box); got > h {
			t.Fatalf("h=%d: box height %d exceeds terminal — bottom help line would be clipped", h, got)
		}
		if !strings.Contains(box, "ctrl+s") {
			t.Fatalf("h=%d: help line missing from box", h)
		}
	}
}

// A script with far more lines than fit still produces a box within the terminal
// height (the window scrolls; it must not grow the box past the screen).
func TestHookEditorBoxFitsWithTallScript(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&sb, "echo line %d\n", i)
	}
	p := &hookEditorPopup{buf: newTextField(sb.String())}
	const h = 24
	box := p.box(Model{}, 100, h)
	if got := lipgloss.Height(box); got > h {
		t.Fatalf("tall-script box height %d exceeds terminal height %d", got, h)
	}
	if !strings.Contains(box, "ctrl+s") {
		t.Fatal("help line missing from box with a tall script")
	}
}
