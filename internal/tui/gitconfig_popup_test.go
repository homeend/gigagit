package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

func explorerRows() []model.GitConfigRow {
	return []model.GitConfigRow{
		{Key: "add.ignoreErrors"},
		{Key: "fetch.writeCommitGraph", LocalValue: "true", LocalSet: true},
		{Key: "user.name", GlobalValue: "Ada L", GlobalSet: true},
		{Key: "alias.lg", LocalValue: "log --graph", LocalSet: true}, // non-curated
	}
}

// openExplorer drives Settings → "Git config explorer" → enter, then delivers
// the rows as if the background read landed.
func openExplorer(t *testing.T, m Model) Model {
	t.Helper()
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	p := layerOf[*settingsPopup](m)
	if p == nil {
		t.Fatal("settings popup did not open")
	}
	for i, entry := range settingsMenu {
		if entry == settingsMenuGitConfig {
			p.menuSel = i
		}
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(Model)
	if layerOf[*gitConfigPopup](m) == nil {
		t.Fatal("enter must open the explorer")
	}
	u, _ = m.Update(gitConfigRowsMsg{gen: m.gitConfigGen, rows: explorerRows()})
	return u.(Model)
}

func TestExplorerOpensLoadsAndRenders(t *testing.T) {
	m, _ := settingsModel(t)
	m = openExplorer(t, m)
	out := m.View()
	for _, want := range []string{"fetch.writeCommitGraph", "(unset)", "Ada L", "—"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view missing %q:\n%s", want, out)
		}
	}
}

func TestExplorerStaleRowsDropped(t *testing.T) {
	m, _ := settingsModel(t)
	m = openExplorer(t, m)
	u, _ := m.Update(gitConfigRowsMsg{gen: m.gitConfigGen - 1, rows: []model.GitConfigRow{{Key: "stale.key"}}})
	if strings.Contains(u.(Model).View(), "stale.key") {
		t.Fatal("a stale-generation rows msg must be dropped")
	}
}

func TestExplorerFilterMovesWhileTyping(t *testing.T) {
	m, _ := settingsModel(t)
	m = openExplorer(t, m)
	p := layerOf[*gitConfigPopup](m)
	u, _ := m.Update(keyMsg("/"))
	m = u.(Model)
	for _, r := range "user" {
		u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = u.(Model)
	}
	vis := p.visible()
	if len(vis) != 1 || vis[0].Key != "user.name" {
		t.Fatalf("filtered view = %+v, want just user.name", vis)
	}
	// esc clears the filter and stays open; second esc closes.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	if layerOf[*gitConfigPopup](m) == nil {
		t.Fatal("esc while filtering must only exit the filter")
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if layerOf[*gitConfigPopup](u.(Model)) != nil {
		t.Fatal("esc in navigation mode must close the explorer")
	}
}

func TestExplorerShowsCuratedDescription(t *testing.T) {
	m, _ := settingsModel(t)
	m = openExplorer(t, m)
	p := layerOf[*gitConfigPopup](m)
	for i, r := range p.visible() {
		if r.Key == "fetch.writeCommitGraph" {
			p.sel = i
		}
	}
	if out := m.View(); !strings.Contains(out, "notification center sets this") {
		t.Fatalf("selected curated row must show its description:\n%s", out)
	}
}

func TestExplorerSwallowsGlobalKeys(t *testing.T) {
	m, _ := settingsModel(t)
	m = openExplorer(t, m)
	before := len(m.layers.entries)
	for _, k := range []string{"p", "!", "G", ",", "R"} {
		u, _ := m.Update(keyMsg(k))
		m = u.(Model)
	}
	if len(m.layers.entries) != before || layerOf[*gitConfigPopup](m) == nil {
		t.Fatal("explorer must swallow global keys")
	}
}
