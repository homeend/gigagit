package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
)

// The [ui] show_graph setting ("on" default / "off" remembered) governs how the
// Commits panel renders on startup: "off" applies the same flat-list mode as the
// . menu's "Show as list". It lands via BOTH config arrival paths — the
// registry's configReadyMsg (app startup) and the legacy dataLoadedMsg (reRoot).

func showGraphCfg(v string) config.Config {
	c := config.Defaults()
	c.UI.ShowGraph = v
	return c
}

func TestShowGraphOffAppliesOnConfigReady(t *testing.T) {
	t.Parallel()
	m := newTestModelForReload(t)
	m.refreshLastRun = map[refreshItem]time.Time{} // configReadyMsg seeds it (New() initializes it in prod)
	nm, _ := m.Update(configReadyMsg{cfg: showGraphCfg("off")})
	if !nm.(Model).commitListMode {
		t.Fatal("show_graph=off must start the Commits panel in list mode")
	}
	// And back on: an explicit "on" re-applies graph mode.
	nm2, _ := nm.(Model).Update(configReadyMsg{cfg: showGraphCfg("on")})
	if nm2.(Model).commitListMode {
		t.Fatal("show_graph=on must render the graph (list mode off)")
	}
}

func TestShowGraphDefaultOnDataLoaded(t *testing.T) {
	t.Parallel()
	m := newTestModelForReload(t)
	m.commitListMode = true                                     // a stale session toggle; a fresh load applies config
	msg := dataLoadedMsg{gen: m.loadGen, cfg: showGraphCfg("")} // unset → on
	nm, _ := m.Update(msg)
	if nm.(Model).commitListMode {
		t.Fatal("unset show_graph must default to the graph (list mode off)")
	}
}

func TestShowGraphOffAppliesOnDataLoaded(t *testing.T) {
	t.Parallel()
	m := newTestModelForReload(t)
	msg := dataLoadedMsg{gen: m.loadGen, cfg: showGraphCfg("off")}
	nm, _ := m.Update(msg)
	if !nm.(Model).commitListMode {
		t.Fatal("show_graph=off must apply list mode on the legacy load path (reRoot)")
	}
}

func TestToggleShowGraphPersistsAndFlips(t *testing.T) {
	t.Parallel()
	m, dir := settingsModel(t)
	m.repoConfigPath = filepath.Join(dir, ".gg.toml")
	if m.commitListMode {
		t.Fatal("precondition: graph mode on")
	}

	m = m.toggleShowGraph()
	if !m.commitListMode {
		t.Fatal("toggling off must switch the Commits panel to list mode (same as Show as list)")
	}
	if m.cfg.UI.ShowGraph != "off" {
		t.Fatalf("cfg.UI.ShowGraph = %q, want off", m.cfg.UI.ShowGraph)
	}
	raw, err := os.ReadFile(m.repoConfigPath)
	if err != nil {
		t.Fatalf("toggle must write the repo .gg.toml: %v", err)
	}
	if !strings.Contains(string(raw), `show_graph = "off"`) {
		t.Fatalf(".gg.toml missing show_graph = \"off\":\n%s", raw)
	}

	// Toggle back: remembered as an explicit "on" (any set value is remembered).
	m = m.toggleShowGraph()
	if m.commitListMode {
		t.Fatal("toggling on must restore graph mode")
	}
	raw, _ = os.ReadFile(m.repoConfigPath)
	if !strings.Contains(string(raw), `show_graph = "on"`) {
		t.Fatalf(".gg.toml missing show_graph = \"on\":\n%s", raw)
	}
}

func TestShowGraphMenuLabelShowsState(t *testing.T) {
	t.Parallel()
	m, _ := settingsModel(t)
	idx := -1
	for i, entry := range settingsMenu {
		if entry == settingsMenuShowGraph {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("Show graph entry missing from the settings menu")
	}
	if got := settingsMenuLabel(m, idx); got != "Show graph: on" {
		t.Fatalf("label = %q, want 'Show graph: on'", got)
	}
	m.cfg.UI.ShowGraph = "off"
	if got := settingsMenuLabel(m, idx); got != "Show graph: off" {
		t.Fatalf("label = %q, want 'Show graph: off'", got)
	}
}
