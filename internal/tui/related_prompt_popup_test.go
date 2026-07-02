package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// openSettingsAndToggleShowGraph drives the real key path: open Settings with
// ',', move the menu selection to "Show graph", press enter. Returns the model
// after the toggle (and possibly with a related prompt pushed).
func openSettingsAndToggleShowGraph(t *testing.T, m Model) Model {
	t.Helper()
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	p := layerOf[*settingsPopup](m)
	if p == nil {
		t.Fatal("settings popup did not open")
	}
	for i, entry := range settingsMenu {
		if entry == settingsMenuShowGraph {
			p.menuSel = i
		}
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return u.(Model)
}

func TestToggleShowGraphOffPushesPrompt(t *testing.T) {
	m, _ := promptTestModel(t)
	m.cfg.UI.CommitSort = "date-order"
	m = openSettingsAndToggleShowGraph(t, m) // on → off
	pp := layerOf[*relatedPromptPopup](m)
	if pp == nil {
		t.Fatal("toggling show_graph off with date-order must push the related prompt")
	}
	if pp.prompt.id != "show_graph_off.commit_sort_plain" {
		t.Fatalf("wrong prompt: %q", pp.prompt.id)
	}
	out := m.View()
	if !strings.Contains(out, "Commit sort") {
		t.Fatalf("prompt question must be visible, view:\n%s", out)
	}
	if !strings.Contains(out, "Not now") || !strings.Contains(out, "don't ask again") {
		t.Fatalf("all three options must be visible, view:\n%s", out)
	}
}

func TestToggleShowGraphOffNoPromptWhenAlreadyPlain(t *testing.T) {
	m, _ := promptTestModel(t)
	m.cfg.UI.CommitSort = "plain"
	m = openSettingsAndToggleShowGraph(t, m)
	if layerOf[*relatedPromptPopup](m) != nil {
		t.Fatal("commit_sort already plain: no prompt")
	}
	if layerOf[*settingsPopup](m) == nil {
		t.Fatal("settings must stay open after a promptless toggle")
	}
}

func TestPromptEscMeansNotNow(t *testing.T) {
	m, _ := promptTestModel(t)
	m.cfg.UI.CommitSort = "date-order"
	m = openSettingsAndToggleShowGraph(t, m)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	if layerOf[*relatedPromptPopup](m) != nil {
		t.Fatal("esc must close the prompt")
	}
	if layerOf[*settingsPopup](m) == nil {
		t.Fatal("esc must return to the Settings popup beneath")
	}
	if m.cfg.UI.CommitSort != "date-order" {
		t.Fatal("Not now must not touch commit_sort")
	}
	// Not now is session-only: the next toggle round-trip asks again.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // Settings enter: off → on (no prompt: sort is date-order)
	m = u.(Model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // on → off again
	m = u.(Model)
	if layerOf[*relatedPromptPopup](m) == nil {
		t.Fatal("Not now must not suppress the prompt permanently")
	}
}

func TestPromptYesAppliesCommitSortPlain(t *testing.T) {
	m, _ := promptTestModel(t)
	m.cfg.UI.CommitSort = "date-order"
	m = openSettingsAndToggleShowGraph(t, m)
	// sel starts on Yes; enter applies.
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(Model)
	if layerOf[*relatedPromptPopup](m) != nil {
		t.Fatal("Yes must close the prompt")
	}
	if m.cfg.UI.CommitSort != "plain" {
		t.Fatalf("Yes must set commit_sort=plain via cycleCommitSort, got %q", m.cfg.UI.CommitSort)
	}
	if cmd == nil {
		t.Fatal("cycleCommitSort re-walks the feed — a command must be returned")
	}
	raw, err := os.ReadFile(m.repoConfigPath)
	if err != nil {
		t.Fatalf("Yes must persist to the repo .gg.toml: %v", err)
	}
	if !strings.Contains(string(raw), `commit_sort = "plain"`) {
		t.Fatalf(".gg.toml missing commit_sort = \"plain\":\n%s", raw)
	}
}

func TestPromptDontAskAgainSuppressesForever(t *testing.T) {
	m, st := promptTestModel(t)
	m.cfg.UI.CommitSort = "date-order"
	m = openSettingsAndToggleShowGraph(t, m)
	// Move to the third option (Yes → Not now → don't ask again) and choose it.
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	u, _ = u.(Model).Update(tea.KeyMsg{Type: tea.KeyDown})
	u, _ = u.(Model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(Model)
	if layerOf[*relatedPromptPopup](m) != nil {
		t.Fatal("don't-ask-again must close the prompt")
	}
	if m.cfg.UI.CommitSort != "date-order" {
		t.Fatal("don't-ask-again must not touch commit_sort")
	}
	if !st.SuppressedPrompts()["show_graph_off.commit_sort_plain"] {
		t.Fatal("don't-ask-again must persist the suppression")
	}
	// Toggle on then off again: the suppressed prompt never comes back.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // off → on
	m = u.(Model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // on → off
	m = u.(Model)
	if layerOf[*relatedPromptPopup](m) != nil {
		t.Fatal("suppressed prompt must never fire again")
	}
}

func TestPromptSwallowsGlobalKeys(t *testing.T) {
	m, _ := promptTestModel(t)
	m.cfg.UI.CommitSort = "date-order"
	m = openSettingsAndToggleShowGraph(t, m)
	before := len(m.layers.entries)
	for _, k := range []string{"p", "g", "G", ",", "r", "?"} {
		u, _ := m.Update(keyMsg(k))
		m = u.(Model)
	}
	if len(m.layers.entries) != before {
		t.Fatal("global keys must be swallowed while the prompt is open")
	}
	if layerOf[*relatedPromptPopup](m) == nil {
		t.Fatal("prompt must still be open")
	}
}

func TestPromptFooterNamesStateFile(t *testing.T) {
	m, _ := promptTestModel(t)
	m.cfg.UI.CommitSort = "date-order"
	m = openSettingsAndToggleShowGraph(t, m)
	pp := layerOf[*relatedPromptPopup](m)
	if pp == nil {
		t.Fatal("prompt must be open")
	}
	if p := defaultPromptStatePath(); p != "" && !strings.Contains(m.View(), "prompts.toml") {
		t.Fatal("the popup must name the prompts.toml state file so the choice is resettable")
	}
}
