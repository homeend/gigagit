package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Switching repos must rebind repoConfigPath to the NEW repo's .gg.toml.
// reRoot reloads through the legacy loadCmd/dataLoadedMsg path (not the
// registry's configReadyMsg), which used to update m.cfg but not
// m.repoConfigPath — so every per-repo Settings write after a switch
// ("Show graph", "Commit sort", refresh rates, the hook editor) landed in the
// PREVIOUS repo's .gg.toml.
func TestRepoSwitchRebindsRepoConfigPath(t *testing.T) {
	t.Parallel()
	m, dirA := settingsModel(t)
	if !strings.HasSuffix(m.repoConfigPath, ".gg.toml") {
		t.Fatalf("initial load must set repoConfigPath, got %q", m.repoConfigPath)
	}

	// Switch to repo B and land its load result (reRoot dispatches loadCmd).
	dirB, _ := newRepoDir(t)
	nm, _ := m.reRoot(dirB)
	m = nm.(Model)
	u, _ := m.Update(m.loadCmd()())
	m = u.(Model)

	// A Settings write must now land in B's .gg.toml — and never touch A's.
	m = m.toggleShowGraph()
	rawB, err := os.ReadFile(filepath.Join(dirB, ".gg.toml"))
	if err != nil {
		t.Fatalf("toggle after repo switch must write the NEW repo's .gg.toml: %v", err)
	}
	if !strings.Contains(string(rawB), `show_graph = "off"`) {
		t.Fatalf("new repo's .gg.toml missing the write:\n%s", rawB)
	}
	if rawA, err := os.ReadFile(filepath.Join(dirA, ".gg.toml")); err == nil &&
		strings.Contains(string(rawA), "show_graph") {
		t.Fatalf("previous repo's .gg.toml must be untouched, got:\n%s", rawA)
	}
}
