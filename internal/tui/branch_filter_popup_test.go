package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
)

// These tests call t.Setenv, so they must NOT call t.Parallel (newTestModel
// itself does not — verified in source_test.go:18). t.Setenv and t.Parallel
// are mutually exclusive: the runtime panics if a parallel test sets an env
// var, and XDG_CONFIG_HOME is process-global anyway.
//
// bfKey, not key: irebase_view_test.go:135 already owns `key` in this
// package (and only produces KeyRunes — these tests need enter/esc/arrows).
func bfKey(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func bfTypeText(v *branchFilterView, m Model, s string) Model {
	for _, r := range s {
		m, _ = v.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func bfPopupModel(t *testing.T) (Model, *branchFilterView, string) {
	t.Helper()
	m := bfModel(t)
	dir := t.TempDir()
	m.repoConfigPath = filepath.Join(dir, ".gg.toml")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "cfg")) // global path → temp
	m, _ = m.openBranchFilterSettings()
	v, ok := m.topLayer().(*branchFilterView)
	if !ok {
		t.Fatalf("top layer = %T", m.topLayer())
	}
	return m, v, m.repoConfigPath
}

func TestBranchFilterPopupListsFiveSlots(t *testing.T) {
	m, v, _ := bfPopupModel(t)
	out := v.box(m)
	for _, want := range []string{"1  feat", "hide · prefix feat/", "2  stale", "3  only-fix", "show only", "4", "invalid", "5", "—"} {
		if !strings.Contains(out, want) {
			t.Errorf("popup lacks %q:\n%s", want, out)
		}
	}
}

func TestBranchFilterPopupEditSavesRepoBlockAndReloads(t *testing.T) {
	m, v, repoPath := bfPopupModel(t)
	// Move to slot 5 (empty) and open the form.
	for i := 0; i < 4; i++ {
		m, _ = v.update(m, bfKey("down"))
	}
	m, _ = v.update(m, bfKey("enter"))
	if v.mode != bfvForm {
		t.Fatalf("mode = %v", v.mode)
	}
	m = bfTypeText(v, m, "wip")      // name
	m, _ = v.update(m, bfKey("tab")) // mode: leave hide
	m, _ = v.update(m, bfKey("tab")) // older than
	m = bfTypeText(v, m, "30d")
	m, _ = v.update(m, bfKey("tab")) // younger than
	m, _ = v.update(m, bfKey("tab")) // prefix
	m, _ = v.update(m, bfKey("tab")) // suffix
	m = bfTypeText(v, m, "-wip")
	m, _ = v.update(m, bfKey("tab")) // contains
	m, _ = v.update(m, bfKey("tab")) // regex
	m, _ = v.update(m, bfKey("tab")) // scope: toggle to repo
	m, _ = v.update(m, bfKey("l"))
	m, _ = v.update(m, bfKey("enter"))
	if v.formErr != "" {
		t.Fatalf("formErr = %q", v.formErr)
	}
	raw, err := os.ReadFile(repoPath)
	if err != nil {
		t.Fatalf("repo config not written: %v", err)
	}
	if !strings.Contains(string(raw), "slot = 5") || !strings.Contains(string(raw), `suffix = "-wip"`) || !strings.Contains(string(raw), `older_than = "30d"`) {
		t.Errorf("written:\n%s", raw)
	}
	if !m.branchFilters[4].Usable() || m.branchFilters[4].Name != "wip" {
		t.Errorf("model not reloaded: %+v", m.branchFilters[4].Slot)
	}
	if v.mode != bfvBrowse {
		t.Errorf("form should close on save")
	}
}

func TestBranchFilterPopupRefusesBadRegexInline(t *testing.T) {
	m, v, repoPath := bfPopupModel(t)
	m, _ = v.update(m, bfKey("enter")) // edit slot 1
	for i := 0; i < 7; i++ {
		m, _ = v.update(m, bfKey("tab"))
	}
	m = bfTypeText(v, m, "(")
	m, _ = v.update(m, bfKey("enter"))
	if !strings.Contains(v.formErr, "regex") {
		t.Errorf("formErr = %q", v.formErr)
	}
	if _, err := os.Stat(repoPath); err == nil {
		t.Error("a refused save wrote the repo file")
	}
	if v.mode != bfvForm {
		t.Error("form closed on a refused save")
	}
}

// An empty form is refused too: a block with no clause would compile Usable()
// == false, so saving it would write a rule alt+N then refuses to activate.
func TestBranchFilterPopupRefusesEmptyRule(t *testing.T) {
	m, v, repoPath := bfPopupModel(t)
	for i := 0; i < 4; i++ {
		m, _ = v.update(m, bfKey("down")) // slot 5, empty
	}
	m, _ = v.update(m, bfKey("enter"))
	m = bfTypeText(v, m, "nothing") // a name is not a clause
	m, _ = v.update(m, bfKey("enter"))
	if !strings.Contains(v.formErr, "at least one clause") {
		t.Errorf("formErr = %q", v.formErr)
	}
	if _, err := os.Stat(repoPath); err == nil {
		t.Error("a refused save wrote the repo file")
	}
}

func TestBranchFilterPopupDeletePeelsRepoThenGlobal(t *testing.T) {
	m, v, repoPath := bfPopupModel(t)
	globalPath := config.DefaultGlobalPath() // under the test's XDG_CONFIG_HOME
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalPath, []byte("[[branches.filter]]\nslot = 1\nname = \"g\"\nprefix = \"g/\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repoPath, []byte("[[branches.filter]]\nslot = 1\nname = \"r\"\nprefix = \"r/\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = m.reloadConfigAfterBranchFilterWrite()
	m.branchFilterSlot[panelBranches] = 1
	if !strings.Contains(v.box(m), "[this repo, overrides global]") {
		t.Errorf("provenance tag missing:\n%s", v.box(m))
	}
	// First d: only the repo definition goes; global still applies.
	m, _ = v.update(m, bfKey("d"))
	if raw, _ := os.ReadFile(repoPath); strings.Contains(string(raw), "slot = 1") {
		t.Errorf("repo block not removed:\n%s", raw)
	}
	if raw, _ := os.ReadFile(globalPath); !strings.Contains(string(raw), "slot = 1") {
		t.Errorf("global block must survive the first d:\n%s", raw)
	}
	if m.branchFilters[0].Name != "g" || m.branchFilterSlot[panelBranches] != 1 {
		t.Errorf("global rule should now be the effective one and stay active: %+v slot=%d", m.branchFilters[0].Slot, m.branchFilterSlot[panelBranches])
	}
	if !strings.Contains(m.statusMsg, "global") {
		t.Errorf("status = %q", m.statusMsg)
	}
	// Second d: the global definition goes; the active slot clears.
	m, _ = v.update(m, bfKey("d"))
	if raw, _ := os.ReadFile(globalPath); strings.Contains(string(raw), "slot = 1") {
		t.Errorf("global block not removed on second d:\n%s", raw)
	}
	if m.branchFilterSlot[panelBranches] != 0 || !m.branchFilters[0].Empty {
		t.Errorf("after both removals: slot=%d compiled=%+v", m.branchFilterSlot[panelBranches], m.branchFilters[0].Slot)
	}
}

// esc from the browse list pops back to the Settings popup underneath; esc
// from the form only leaves the form (the popup convention: a window opened
// from a popup returns to that popup).
func TestBranchFilterPopupEscapeLayering(t *testing.T) {
	m, v, _ := bfPopupModel(t)
	m, _ = v.update(m, bfKey("enter"))
	m, _ = v.update(m, bfKey("esc"))
	if v.mode != bfvBrowse {
		t.Fatalf("esc in the form should return to browse, mode = %v", v.mode)
	}
	if m.topLayer() != v {
		t.Fatal("esc in the form must not pop the layer")
	}
	m, _ = v.update(m, bfKey("esc"))
	if m.topLayer() == layer(v) {
		t.Fatal("esc in browse should pop the layer")
	}
}

// The form's scope follows the slot's provenance: editing a rule this repo
// defines defaults to rewriting THAT block, so a save that never touches the
// scope row cannot leak a global copy into every other repo. A slot no file
// defines opens global (the only writable choice that is always available).
func TestBranchFilterPopupFormScopeFollowsProvenance(t *testing.T) {
	m, v, repoPath := bfPopupModel(t)
	globalPath := config.DefaultGlobalPath()
	if err := os.WriteFile(repoPath, []byte("[[branches.filter]]\nslot = 2\nname = \"r2\"\nprefix = \"r2/\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = m.reloadConfigAfterBranchFilterWrite()

	// Slot 2: defined only in the repo file.
	m, _ = v.update(m, bfKey("down"))
	m, _ = v.update(m, bfKey("enter"))
	if v.scope != bfScopeRepo {
		t.Fatalf("scope = %v, want repo for a repo-defined slot", v.scope)
	}
	if out := v.box(m); !strings.Contains(out, "this repo only") {
		t.Errorf("form does not show the repo scope:\n%s", out)
	}
	// Saving without touching the scope row rewrites the REPO block.
	m, _ = v.update(m, bfKey("enter"))
	if v.formErr != "" {
		t.Fatalf("formErr = %q", v.formErr)
	}
	raw, err := os.ReadFile(repoPath)
	if err != nil {
		t.Fatalf("repo config: %v", err)
	}
	if !strings.Contains(string(raw), "slot = 2") || !strings.Contains(string(raw), `prefix = "r2/"`) {
		t.Errorf("repo block not rewritten:\n%s", raw)
	}
	if _, err := os.Stat(globalPath); err == nil {
		t.Error("a repo-scoped save wrote the global config")
	}

	// Slot 5: defined nowhere — global is the only always-writable scope.
	for i := 0; i < 3; i++ {
		m, _ = v.update(m, bfKey("down"))
	}
	m, _ = v.update(m, bfKey("enter"))
	if v.scope != bfScopeGlobal {
		t.Errorf("scope = %v, want global for an undefined slot", v.scope)
	}
}
