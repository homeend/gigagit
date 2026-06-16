package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/agentskill"
	"github.com/gigagit/gg/internal/domain"
)

// settingsModel: loaded model whose project dir contains .claude and an
// AGENTS.md that already has an OLD installed block (so defaults differ).
func settingsModel(t *testing.T) (Model, string) {
	t.Helper()
	dir, repo := newRepoDir(t)
	os.MkdirAll(filepath.Join(dir, ".claude"), 0o755)
	os.WriteFile(filepath.Join(dir, "AGENTS.md"),
		[]byte("mine\n\n<!-- gg:using-gg:v0:begin -->\nold\n<!-- gg:using-gg:end -->\n"), 0o644)
	m := New(domain.New(repo))
	u, _ := m.Update(m.loadCmd()())
	m = u.(Model)
	return m, dir
}

func TestCommaOpensSettingsMenu(t *testing.T) {
	m, _ := settingsModel(t)
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	if m.settings == nil {
		t.Fatal(", should open the settings popup")
	}
	if m.settings.picker {
		t.Fatal("should open on the menu screen, not the picker")
	}
	out := m.View()
	if !strings.Contains(out, "Settings") || !strings.Contains(out, "agent") {
		t.Fatalf("menu content missing:\n%s", out)
	}
}

func TestSettingsPopupZCyclesMode(t *testing.T) {
	m, _ := settingsModel(t)
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	u, _ = m.Update(keyMsg("z"))
	m = u.(Model)
	if m.settings == nil || m.settings.mode != modeWrap {
		t.Fatalf("z should cycle the settings mode to modeWrap; got %+v", m.settings)
	}
}

func TestPickerCheckboxDefaults(t *testing.T) {
	m, _ := settingsModel(t)
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	u, _ = m.Update(keyMsg("enter")) // menu entry -> picker
	m = u.(Model)
	if !m.settings.picker {
		t.Fatal("enter on the menu entry should open the picker")
	}
	// claude-project (.claude, new) unchecked; agents-md (old block) checked.
	byID := map[string]bool{}
	for i, d := range m.settings.dets {
		byID[d.Agent.ID] = m.settings.checked[i]
	}
	if byID["claude-project"] {
		t.Error("new target must default unchecked")
	}
	if !byID["agents-md"] {
		t.Error("already-installed target must default checked")
	}
}

func TestPickerToggleAndApply(t *testing.T) {
	m, dir := settingsModel(t)
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	// Move to the claude-project row and check it.
	idx := -1
	for i, d := range m.settings.dets {
		if d.Agent.ID == "claude-project" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("claude-project not in picker")
	}
	m.settings.sel = idx
	u, _ = m.Update(keyMsg("space"))
	m = u.(Model)
	if !m.settings.checked[idx] {
		t.Fatal("space should toggle the checkbox")
	}
	u, _ = m.Update(keyMsg("enter")) // apply
	m = u.(Model)
	if m.settings != nil {
		t.Fatal("apply should close the popup")
	}
	// claude-project installed AND agents-md refreshed (was checked by default).
	skill, err := os.ReadFile(filepath.Join(dir, ".claude", "skills", "using-gg", "SKILL.md"))
	if err != nil || agentskill.InstalledVersion(skill) != agentskill.Version {
		t.Errorf("claude skill not installed: %v", err)
	}
	agents, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if agentskill.InstalledVersion(agents) != agentskill.Version {
		t.Error("agents-md not refreshed")
	}
	if !strings.Contains(string(agents), "mine") {
		t.Error("surrounding AGENTS.md content lost")
	}
	if m.statusMsg == "" {
		t.Error("apply should report in the status line")
	}
}

func TestSettingsEscBackThenClose(t *testing.T) {
	m, _ := settingsModel(t)
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("esc")) // picker -> menu
	m = u.(Model)
	if m.settings == nil || m.settings.picker {
		t.Fatal("esc in the picker should go back to the menu")
	}
	u, _ = m.Update(keyMsg("esc")) // menu -> closed
	m = u.(Model)
	if m.settings != nil {
		t.Fatal("esc on the menu should close the popup")
	}
}

func TestSettingsSwallowsGlobalKeys(t *testing.T) {
	m, _ := settingsModel(t)
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	u, _ = m.Update(keyMsg("p"))
	m = u.(Model)
	if m.running {
		t.Fatal("settings popup leaked a global key")
	}
	if m.settings == nil {
		t.Fatal("popup should still be open")
	}
	u, cmd := m.Update(keyMsg("q"))
	m = u.(Model)
	if cmd != nil {
		t.Fatal("q inside the popup must not emit a command (quit leak)")
	}
	if m.settings == nil {
		t.Fatal("popup should still be open after q")
	}
}
