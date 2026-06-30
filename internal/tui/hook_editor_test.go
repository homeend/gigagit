package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
