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

// TestExplorerWrapModeUnsetCellsNotCorrupted is a regression test for a bug
// where configCell/gitConfigDefaultCell baked unsetStyle's ANSI escape into
// winRow.text BEFORE renderWindow's width-based slicing. wrapWidth measures
// width rune-by-rune with no escape-sequence awareness, so in wrap mode
// (`z` once from the default cutoff mode) a line break could land mid-escape,
// leaving a dangling open sequence on one physical line and the "(unset)"
// text stranded on the next WITHOUT its dim-color prefix — real, observed
// corruption (verified empirically against the pre-fix code: an "(unset)"
// reappearing on a wrap-continuation line with no preceding color escape).
// The fix decorates post-slice instead, so every "(unset)" in the rendered
// view must always be immediately preceded by unsetStyle's full escape
// prefix, never orphaned across a line break.
func TestExplorerWrapModeUnsetCellsNotCorrupted(t *testing.T) {
	forceColor(t)
	m, _ := settingsModel(t)
	m = openExplorer(t, m)

	// One `z` press cycles modeCutoff -> modeWrap (dispMode.next()).
	u, _ := m.Update(keyMsg("z"))
	m = u.(Model)
	p := layerOf[*gitConfigPopup](m)
	if p.mode != modeWrap {
		t.Fatalf("expected wrap mode after one z press, got %v", p.mode)
	}

	out := m.View()
	if !strings.Contains(out, "(unset)") {
		t.Fatalf("wrap-mode view must still contain the literal (unset) text:\n%s", out)
	}

	// unsetStyle's rendered escape prefix, independent of the content it
	// wraps (Render's opening SGR sequence depends only on the style).
	probe := unsetStyle.Render("(unset)")
	openSeq := probe[:strings.Index(probe, "(")]
	if openSeq == "" {
		t.Fatal("unsetStyle produced no escape prefix; forceColor may not have taken effect")
	}

	total := strings.Count(out, "(unset)")
	prefixed := strings.Count(out, openSeq+"(unset)")
	if prefixed != total {
		t.Fatalf("an \"(unset)\" cell lost its dim-color escape across a wrap line break: only %d of %d occurrences are immediately prefixed by the color escape %q\nview:\n%s",
			prefixed, total, openSeq, out)
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
