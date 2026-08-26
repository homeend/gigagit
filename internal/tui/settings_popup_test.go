package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/agentinit"
	"github.com/homeend/gigagit/internal/agentskill"
	"github.com/homeend/gigagit/internal/domain"
)

// lineWith returns the first rendered line containing substr (substr matched
// against the ANSI-stripped text), or "" if none.
func lineWith(out, substr string) string {
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ansi.Strip(ln), substr) {
			return ln
		}
	}
	return ""
}

// The , menu highlights the selected row with the same graphical style as the
// . action menu (selectedRow): the selected row carries ANSI styling, others
// do not.
func TestSettingsMenuHighlightsSelectedRow(t *testing.T) {
	// Force a color profile so the reverse-video highlight is actually emitted
	// (tests run without a TTY, where lipgloss strips styling by default).
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m, _ := settingsModel(t)
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	out := m.View()

	// selectedRow = Reverse(true), which renders the reverse-video SGR (\x1b[7m).
	// The frame around the row uses color SGRs, not reverse, so checking for the
	// reverse code specifically isolates the row highlight from frame styling.
	const reverse = "\x1b[7m"

	sel := lineWith(out, "External tools") // settingsMenuTools, now the first row (menuSel 0)
	if sel == "" {
		t.Fatalf("selected menu row not found:\n%s", ansi.Strip(out))
	}
	if !strings.Contains(sel, reverse) {
		t.Fatalf("selected row must carry the reverse-video highlight; got: %q", sel)
	}

	other := lineWith(out, "Identity & profiles") // not selected
	if other == "" {
		t.Fatalf("unselected menu row not found:\n%s", ansi.Strip(out))
	}
	if strings.Contains(other, reverse) {
		t.Fatalf("unselected row must not be highlighted; got: %q", other)
	}
}

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
	if layerOf[*settingsPopup](m) == nil {
		t.Fatal(", should open the settings popup")
	}
	if layerOf[*settingsPopup](m).picker {
		t.Fatal("should open on the menu screen, not the picker")
	}
	out := m.View()
	if !strings.Contains(out, "Settings") || !strings.Contains(out, "External tools") {
		t.Fatalf("menu content missing:\n%s", out)
	}
}

func TestSettingsMenuWrapsAround(t *testing.T) {
	m, _ := settingsModel(t)
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	p := layerOf[*settingsPopup](m)
	if p == nil || p.menuSel != 0 {
		t.Fatalf("menu should open on the first option; got %+v", p)
	}
	// up on the first option wraps to the last.
	u, _ = m.Update(keyMsg("up"))
	m = u.(Model)
	last := len(settingsMenu) - 1
	if got := layerOf[*settingsPopup](m).menuSel; got != last {
		t.Fatalf("up on first option should wrap to last (%d); got %d", last, got)
	}
	// down on the last option wraps back to the first.
	u, _ = m.Update(keyMsg("down"))
	m = u.(Model)
	if got := layerOf[*settingsPopup](m).menuSel; got != 0 {
		t.Fatalf("down on last option should wrap to first (0); got %d", got)
	}
}

func TestSettingsPopupZCyclesMode(t *testing.T) {
	m, _ := settingsModel(t)
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	u, _ = m.Update(keyMsg("ctrl+w"))
	m = u.(Model)
	p := layerOf[*settingsPopup](m)
	if p == nil || p.mode != modeWrap {
		t.Fatalf("z should cycle the settings mode to modeWrap; got %+v", p)
	}
}

func TestPickerCheckboxDefaults(t *testing.T) {
	// The picker is no longer reached via the , menu (agent skills moved to
	// the command palette) — open it the way the palette entry does.
	m, _ := settingsModel(t)
	m, _ = m.openSettings()
	m = m.openAgentPicker()
	p := layerOf[*settingsPopup](m)
	if p == nil || !p.picker {
		t.Fatal("openAgentPicker should open the picker")
	}
	// claude-project (.claude, new) unchecked; agents-md (old block) checked.
	byID := map[string]bool{}
	for i, d := range p.dets {
		byID[d.Agent.ID] = p.checked[i]
	}
	if byID["claude-project"] {
		t.Error("new target must default unchecked")
	}
	if !byID["agents-md"] {
		t.Error("already-installed target must default checked")
	}
}

func TestPickerToggleAndApply(t *testing.T) {
	// The picker is no longer reached via the , menu (agent skills moved to
	// the command palette) — open it the way the palette entry does.
	m, dir := settingsModel(t)
	m, _ = m.openSettings()
	m = m.openAgentPicker()
	// Move to the claude-project row and check it.
	p := layerOf[*settingsPopup](m)
	idx := -1
	for i, d := range p.dets {
		if d.Agent.ID == "claude-project" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("claude-project not in picker")
	}
	p.sel = idx
	u, _ := m.Update(keyMsg("space"))
	m = u.(Model)
	if !layerOf[*settingsPopup](m).checked[idx] {
		t.Fatal("space should toggle the checkbox")
	}
	u, _ = m.Update(keyMsg("enter")) // apply
	m = u.(Model)
	if layerOf[*settingsPopup](m) != nil {
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

// Two-level esc navigation within Settings: esc from a sub-screen (here the
// Tools wizard, menuSel 0 since agent skills moved to the palette) backs out
// to the menu, then esc from the menu closes the popup entirely.
func TestSettingsEscBackThenClose(t *testing.T) {
	m, _ := settingsModel(t)
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	u, _ = m.Update(keyMsg("enter")) // menu entry (External tools) -> tools wizard
	m = u.(Model)
	u, _ = m.Update(keyMsg("esc")) // tools wizard -> menu
	m = u.(Model)
	p := layerOf[*settingsPopup](m)
	if p == nil || p.toolsView {
		t.Fatal("esc in the tools wizard should go back to the menu")
	}
	u, _ = m.Update(keyMsg("esc")) // menu -> closed
	m = u.(Model)
	if layerOf[*settingsPopup](m) != nil {
		t.Fatal("esc on the menu should close the popup")
	}
}

func TestToggleAutoRefreshFlipsInMemory(t *testing.T) {
	// Redirect XDG_CONFIG_HOME so toggleAutoRefresh doesn't write the real
	// user config during tests (DefaultGlobalPath honors XDG_CONFIG_HOME).
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newTestModel(t)
	if m.cfg.Refresh.Enabled {
		t.Fatal("precondition: starts disabled")
	}
	m = m.toggleAutoRefresh()
	if !m.cfg.Refresh.Enabled {
		t.Fatal("toggle should enable in-memory")
	}
	m = m.toggleAutoRefresh()
	if m.cfg.Refresh.Enabled {
		t.Fatal("toggle should disable in-memory")
	}
}

func TestToggleAutoRemoteTagsFlipsInMemory(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // don't write the real user config
	m := newTestModel(t)
	// default: enabled (DisableRemoteTagsAuto false)
	if m.cfg.Refresh.DisableRemoteTagsAuto {
		t.Fatal("precondition: auto-refresh remote tags on by default")
	}
	m = m.toggleAutoRemoteTags()
	if !m.cfg.Refresh.DisableRemoteTagsAuto {
		t.Fatal("toggle should disable in-memory")
	}
	m = m.toggleAutoRemoteTags()
	if m.cfg.Refresh.DisableRemoteTagsAuto {
		t.Fatal("toggle should re-enable in-memory")
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
	if layerOf[*settingsPopup](m) == nil {
		t.Fatal("popup should still be open")
	}
	u, cmd := m.Update(keyMsg("q"))
	m = u.(Model)
	if cmd != nil {
		t.Fatal("q inside the popup must not emit a command (quit leak)")
	}
	if layerOf[*settingsPopup](m) == nil {
		t.Fatal("popup should still be open after q")
	}
}

func TestSettingsOpensHookEditor(t *testing.T) {
	m := Model{}
	m, _ = m.openSettings()
	sp := layerOf[*settingsPopup](m)
	if sp == nil {
		t.Fatal("settings not open")
	}
	// Move selection to the hook entry.
	for i, name := range settingsMenu {
		if name == settingsMenuHook {
			sp.menuSel = i
		}
	}
	m, _ = sp.update(m, keyMsg("enter"))
	if layerOf[*hookEditorPopup](m) == nil {
		t.Fatal("Enter on hook entry should open the editor")
	}
}

func TestSettingsPopupMaximizeWidensAndLiftsRowCap(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	var dets []agentinit.Detection
	var checked []bool
	for i := 0; i < 20; i++ { // more than the fixed cap of 12
		dets = append(dets, agentinit.Detection{Agent: agentinit.Agent{ID: fmt.Sprintf("a%d", i), Label: fmt.Sprintf("Agent %d", i)}})
		checked = append(checked, false)
	}
	p := &settingsPopup{picker: true, dets: dets, checked: checked}

	normal := p.box(m)
	p.maximized = true
	maxed := p.box(m)

	if lipgloss.Width(maxed) <= lipgloss.Width(normal) {
		t.Fatalf("maximized width %d must exceed normal %d", lipgloss.Width(maxed), lipgloss.Width(normal))
	}
	if lipgloss.Height(maxed) <= lipgloss.Height(normal) {
		t.Fatalf("maximized must show more rows: height %d vs %d", lipgloss.Height(maxed), lipgloss.Height(normal))
	}
}
