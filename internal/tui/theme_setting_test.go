package tui

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/theme"
)

// These tests swap the process-global styles pointer, so they are SERIAL
// (no t.Parallel) and restore the previous theme on exit.

func themeCfg(v string) config.Config {
	c := config.Defaults()
	c.UI.Theme = v
	return c
}

func TestThemeAppliesOnConfigReady(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m := newTestModelForReload(t)
	m.refreshLastRun = map[refreshItem]time.Time{}
	nm, _ := m.Update(configReadyMsg{cfg: themeCfg("dark")})
	if activeTheme().Name != theme.NameDark {
		t.Fatalf("configReadyMsg with theme=dark must activate dark, got %q", activeTheme().Name)
	}
	nm2, _ := nm.(Model).Update(configReadyMsg{cfg: themeCfg("")})
	_ = nm2
	if activeTheme().Name != theme.NameTerminal {
		t.Fatalf("empty theme must fall back to terminal, got %q", activeTheme().Name)
	}
}

func TestThemeUnknownFallsBackWithNotice(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m := newTestModelForReload(t)
	m.refreshLastRun = map[refreshItem]time.Time{}
	nm, _ := m.Update(configReadyMsg{cfg: themeCfg("solarized")})
	if activeTheme().Name != theme.NameTerminal {
		t.Fatalf("unknown theme must fall back to terminal, got %q", activeTheme().Name)
	}
	if !strings.Contains(nm.(Model).statusMsg, "solarized") {
		t.Fatalf("status must name the unknown theme, got %q", nm.(Model).statusMsg)
	}
}

func TestCycleThemePersistsAndClears(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m, _ := settingsModel(t)
	m.cfg.UI.Theme = "terminal"

	m, cmd := m.cycleTheme()
	if m.cfg.UI.Theme != "dark" || activeTheme().Name != theme.NameDark {
		t.Fatalf("terminal → dark expected, cfg=%q active=%q", m.cfg.UI.Theme, activeTheme().Name)
	}
	if cmd == nil {
		t.Fatal("cycleTheme must return tea.ClearScreen so stale rows repaint")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("ClearScreen cmd produced nil msg")
	}
	raw, err := os.ReadFile(config.DefaultGlobalPath())
	if err != nil {
		t.Fatalf("cycle must write the global config: %v", err)
	}
	if !strings.Contains(string(raw), `theme = "dark"`) {
		t.Fatalf("config missing theme = \"dark\":\n%s", raw)
	}

	m, _ = m.cycleTheme()
	if m.cfg.UI.Theme != "light" {
		t.Fatalf("dark → light expected, got %q", m.cfg.UI.Theme)
	}
	m, _ = m.cycleTheme()
	if m.cfg.UI.Theme != "terminal" || activeTheme().Name != theme.NameTerminal {
		t.Fatalf("light → terminal expected, got %q", m.cfg.UI.Theme)
	}
}

func TestSettingsThemeRowShowsValue(t *testing.T) {
	m, _ := settingsModel(t)
	m.cfg.UI.Theme = "light"
	i := slices.Index(settingsMenu, settingsMenuTheme)
	if i < 0 {
		t.Fatal("settingsMenuTheme not present in settingsMenu")
	}
	row := settingsMenuLabel(m, i)
	if !strings.Contains(row, "light") {
		t.Fatalf("row = %q, want it to show the active value", row)
	}
}

var _ tea.Cmd = tea.ClearScreen
