package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/theme"
)

// D (shift+d) resets the WHOLE active theme: a yes/no question naming the
// theme and the override count, then the global [themes.<name>] table goes.
// Serial + XDG-isolated like every editor test (they swap the styles pointer
// and write the global config).

// seedGlobalOverrides writes two global overrides for the light theme
// through the editor's own writer and reloads the popup's global layer.
func seedGlobalOverrides(t *testing.T, p *themeEditorPopup) {
	t.Helper()
	if err := config.SetThemeRole(p.globalPath, "light", "dim", "#123456"); err != nil {
		t.Fatal(err)
	}
	if err := config.SetThemeRole(p.globalPath, "light", "lanes", "#654321", "", "", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	p.global = themeOverrideIn(p.globalPath, "", "light")
	if got := themeOverrideCount(p.global); got != 2 {
		t.Fatalf("seeded %d overrides, want 2", got)
	}
}

func TestThemeEditorResetNothingToDo(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := themeEditorModel(t, "light")
	um, _ := m.Update(keyMsg("D"))
	m = um.(Model)
	if p.confirming {
		t.Fatal("D with no global overrides must not ask")
	}
	if p.statusErr || !strings.Contains(p.status, "[themes.light]") {
		t.Fatalf("status = %q (err=%v)", p.status, p.statusErr)
	}
	if _, err := os.Stat(config.DefaultGlobalPath()); err == nil {
		t.Fatal("nothing may be written when there is nothing to reset")
	}
}

func TestThemeEditorResetAsksThenKeeps(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := themeEditorModel(t, "light")
	seedGlobalOverrides(t, p)
	m, _ = p.reapply(m)
	before, _ := os.ReadFile(p.globalPath)

	um, _ := m.Update(keyMsg("D"))
	m = um.(Model)
	if !p.confirming {
		t.Fatal("D must put the question up")
	}
	body := p.box(m)
	for _, want := range []string{"Reset the light theme?", "Removes 2 overrides from [themes.light]", "[y] reset", "[n/esc] keep"} {
		if !strings.Contains(body, want) {
			t.Fatalf("question must carry %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "[D] reset theme") {
		t.Fatalf("the browse footer must be replaced while asking:\n%s", body)
	}

	// n keeps everything; so does esc — and neither pops the layer.
	for _, key := range []string{"n", "esc"} {
		um, _ = m.Update(keyMsg("D"))
		m = um.(Model)
		um, _ = m.Update(keyMsg(key))
		m = um.(Model)
		if p.confirming {
			t.Fatalf("%s must cancel the question", key)
		}
		if layerOf[*themeEditorPopup](m) != p {
			t.Fatalf("%s while asking must not close the editor", key)
		}
	}
	after, _ := os.ReadFile(p.globalPath)
	if string(after) != string(before) {
		t.Fatalf("cancelling must not touch the file:\n%s", after)
	}
	if themeOverrideCount(p.global) != 2 {
		t.Fatal("cancelling must keep the overrides")
	}
}

func TestThemeEditorResetRemovesTheTable(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := themeEditorModel(t, "light")
	seedGlobalOverrides(t, p)
	m, _ = p.reapply(m)
	if got := string(st().dim.GetForeground().(lipgloss.Color)); got != "#123456" {
		t.Fatalf("seeded override not applied, dim = %q", got)
	}

	um, _ := m.Update(keyMsg("D"))
	m = um.(Model)
	um, cmd := m.Update(keyMsg("y"))
	m = um.(Model)
	if !isClearScreen(cmd) {
		t.Fatal("a reset must repaint through tea.ClearScreen")
	}
	if p.confirming {
		t.Fatal("y must leave the question")
	}
	raw, _ := os.ReadFile(p.globalPath)
	if strings.Contains(string(raw), "[themes.light]") || strings.Contains(string(raw), "#123456") {
		t.Fatalf("the table must be gone:\n%s", raw)
	}
	if themeOverrideCount(p.global) != 0 {
		t.Fatalf("popup still carries %+v", p.global)
	}
	if got := string(st().dim.GetForeground().(lipgloss.Color)); got != theme.Light.Dim {
		t.Fatalf("dim must be the built-in again, got %q", got)
	}
	if _, ok := m.cfg.Themes["light"]; ok && themeOverrideCount(m.cfg.Themes["light"]) != 0 {
		t.Fatalf("model config still overrides: %+v", m.cfg.Themes["light"])
	}
	if p.statusErr || !strings.Contains(p.status, "removed 2 overrides") || !strings.Contains(p.status, "[themes.light]") {
		t.Fatalf("status = %q (err=%v)", p.status, p.statusErr)
	}
	body := p.box(m)
	for _, row := range []string{"* dim", "* lanes[0]"} {
		if strings.Contains(body, row) {
			t.Fatalf("row %q must lose its override mark:\n%s", row, body)
		}
	}
}

func TestThemeEditorResetKeepsRepoValuesAndNamesThem(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m, dir := settingsModel(t)
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"),
		[]byte("[themes.light]\nfg = \"#abcdef\"\nlanes = [\"#111111\", \"\", \"\", \"\", \"\", \"\", \"\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.cfg.UI.Theme = "light"
	m, _ = m.applyTheme()
	m, _ = m.openSettings()
	sp := layerOf[*settingsPopup](m)
	sp.menuSel = slices.Index(settingsMenu, settingsMenuThemeColours)
	um, _ := m.Update(keyMsg("enter"))
	m = um.(Model)
	p := layerOf[*themeEditorPopup](m)
	seedGlobalOverrides(t, p)
	m, _ = p.reapply(m)

	um, _ = m.Update(keyMsg("D"))
	m = um.(Model)
	um, _ = m.Update(keyMsg("y"))
	m = um.(Model)
	if got := string(st().dim.GetForeground().(lipgloss.Color)); got != theme.Light.Dim {
		t.Fatalf("dim must be the built-in again, got %q", got)
	}
	if _, fg := st().frame(); string(fg) != "#abcdef" {
		t.Fatalf("the repo fg must still paint, got %q", fg)
	}
	for _, want := range []string{".gg.toml", "fg", "lanes"} {
		if !strings.Contains(p.status, want) {
			t.Fatalf("status must name the repo-pinned keys (%q): %q", want, p.status)
		}
	}
}

// In filter mode D is query text, not the reset.
func TestThemeEditorResetKeyTypesWhileFiltering(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := themeEditorModel(t, "light")
	seedGlobalOverrides(t, p)
	um, _ := m.Update(keyMsg("/"))
	m = um.(Model)
	um, _ = m.Update(keyMsg("D"))
	m = um.(Model)
	if p.confirming || p.filter.Value() != "D" {
		t.Fatalf("confirming=%v filter=%q", p.confirming, p.filter.Value())
	}
}

// isClearScreen reports whether cmd is tea.ClearScreen (its message type is
// unexported, so the check goes through the message's type name).
func isClearScreen(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	return fmt.Sprintf("%T", cmd()) == fmt.Sprintf("%T", tea.ClearScreen())
}
