package tui

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// themeCfgWith is themeCfg plus one [themes.<name>] override table.
func themeCfgWith(v, name string, o theme.Override) config.Config {
	c := themeCfg(v)
	c.Themes = map[string]theme.Override{name: o}
	return c
}

// A [themes.dark] table repaints roles of the built-in dark theme.
func TestThemeOverrideAppliesOnConfigReady(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m := newTestModelForReload(t)
	m.refreshLastRun = map[refreshItem]time.Time{}

	cfg := themeCfgWith("dark", "dark", theme.Override{Dim: "#123456"})
	nm, _ := m.Update(configReadyMsg{cfg: cfg})
	if got := string(st().dim.GetForeground().(lipgloss.Color)); got != "#123456" {
		t.Fatalf("st().dim = %q, want the overridden #123456", got)
	}
	if activeTheme().Name != theme.NameDark {
		t.Fatalf("an override must not change the theme name, got %q", activeTheme().Name)
	}
	if bg, _ := st().frame(); string(bg) != theme.Dark.Bg {
		t.Fatalf("unset roles must keep the built-in value, frame bg = %q", bg)
	}
	if s := nm.(Model).statusMsg; strings.Contains(s, "invalid") {
		t.Fatalf("a valid override must not complain: %q", s)
	}
}

// An invalid value is skipped and NAMED in the status bar; its valid siblings
// still apply.
func TestThemeOverrideInvalidNamedInStatus(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m := newTestModelForReload(t)
	m.refreshLastRun = map[refreshItem]time.Time{}

	cfg := themeCfgWith("dark", "dark", theme.Override{Dim: "zz", Muted: "#654321"})
	nm, _ := m.Update(configReadyMsg{cfg: cfg})
	status := nm.(Model).statusMsg
	if !strings.Contains(status, "dim=zz") {
		t.Fatalf("status must name the rejected key=value, got %q", status)
	}
	if !strings.Contains(status, "dark") {
		t.Fatalf("status must name the theme, got %q", status)
	}
	if got := string(st().dim.GetForeground().(lipgloss.Color)); got != theme.Dark.Dim {
		t.Fatalf("rejected role must keep the built-in value, got %q", got)
	}
	if got := string(st().noteBody.GetForeground().(lipgloss.Color)); got != "#654321" {
		t.Fatalf("valid sibling (muted) must still apply, got %q", got)
	}
}

// Both complaints show, unknown-name first.
func TestThemeUnknownNameAndInvalidOverride(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m := newTestModelForReload(t)
	m.refreshLastRun = map[refreshItem]time.Time{}

	cfg := themeCfgWith("solarized", "terminal", theme.Override{Bg: "#12"})
	nm, _ := m.Update(configReadyMsg{cfg: cfg})
	status := nm.(Model).statusMsg
	if !strings.Contains(status, "solarized") || !strings.Contains(status, "bg=#12") {
		t.Fatalf("status must carry both complaints, got %q", status)
	}
	if !strings.Contains(status, "; ") {
		t.Fatalf("the two complaints must be joined with \"; \", got %q", status)
	}
}

// [themes.terminal] makes the inherit-everything theme paintable.
func TestThemeOverrideMakesTerminalPaintable(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m := newTestModelForReload(t)
	m.refreshLastRun = map[refreshItem]time.Time{}

	cfg := themeCfgWith("terminal", "terminal", theme.Override{Bg: "#000000"})
	if _, cmd := m.Update(configReadyMsg{cfg: cfg}); cmd == nil {
		_ = cmd // the repaint cmd is asserted by TestThemeOverrideChangeClearsScreen
	}
	if bg, _ := st().frame(); string(bg) != "#000000" {
		t.Fatalf("terminal frame bg = %q, want the overridden #000000", bg)
	}
}

// A repo switch can change COLOURS under the same theme name (repo A sets
// [themes.dark], repo B doesn't), so the repaint gate compares the resolved
// theme, not just its name.
func TestThemeOverrideChangeClearsScreen(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	setTheme(theme.Terminal)

	m := newTestModelForReload(t)
	m.refreshLastRun = map[refreshItem]time.Time{}
	m.cfg = themeCfg("dark")
	m, _ = m.applyTheme()

	m.cfg = themeCfgWith("dark", "dark", theme.Override{Dim: "#123456"})
	m, cmd := m.applyTheme()
	if cmd == nil {
		t.Fatal("same name but different colours must still return tea.ClearScreen")
	}
	m.cfg = themeCfgWith("dark", "dark", theme.Override{Dim: "#123456"})
	if _, cmd2 := m.applyTheme(); cmd2 != nil {
		t.Fatal("re-applying the SAME resolved theme must return a nil cmd")
	}
}

// NOTE: serial (no t.Parallel) — swaps the process-global styles pointer.
func TestThemeChangeOnConfigReadyClearsScreen(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	setTheme(theme.Terminal)

	m := newTestModelForReload(t)
	m.refreshLastRun = map[refreshItem]time.Time{}
	m.cfg = themeCfg("dark")

	nm, cmd := m.applyTheme()
	if activeTheme().Name != theme.NameDark {
		t.Fatalf("applyTheme(dark) must activate dark, got %q", activeTheme().Name)
	}
	_ = nm
	if cmd == nil {
		t.Fatal("a name change (terminal → dark) must return tea.ClearScreen, got nil cmd")
	}

	m.cfg = themeCfg("dark")
	nm2, cmd2 := m.applyTheme()
	_ = nm2
	if cmd2 != nil {
		t.Fatal("re-applying the SAME theme name must return a nil cmd, not another ClearScreen")
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
