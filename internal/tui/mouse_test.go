package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/model"
)

// mouseModel is markModel sized 80x24: leftW=26, three left boxes of height 7 —
// the active tab slot at y=1, Files at y=8, Staged at y=15; Commits 26..79 full
// body height.
func mouseModel() Model {
	m := markModel()
	m.width, m.height = 80, 24
	return m
}

func TestPanelAt(t *testing.T) {
	t.Parallel()
	m := mouseModel()
	cases := []struct {
		x, y int
		want panel
		ok   bool
	}{
		{0, 1, panelBranches, true},  // top-left border cell (active tab slot, y=1..7)
		{5, 4, panelBranches, true},  // data area
		{0, 8, panelFiles, true},     // Files box (y=8..14)
		{0, 15, panelStaged, true},   // Staged box (y=15..21)
		{25, 21, panelStaged, true},  // bottom-right of the left column
		{26, 1, panelCommits, true},  // commits left edge
		{79, 21, panelCommits, true}, // commits bottom-right
		{5, 0, 0, false},             // header row
		{5, 22, 0, false},            // footer row
		{5, 23, 0, false},            // status row
	}
	for _, c := range cases {
		p, ok := m.panelAt(c.x, c.y)
		if ok != c.ok || (ok && p != c.want) {
			t.Errorf("panelAt(%d,%d) = %v,%v want %v,%v", c.x, c.y, p, ok, c.want, c.ok)
		}
	}
}

func TestPanelAtNarrowTerminal(t *testing.T) {
	t.Parallel()
	m := mouseModel()
	m.width = 30 // single commits column
	if p, ok := m.panelAt(5, 5); !ok || p != panelCommits {
		t.Fatalf("panelAt = %v,%v, want commits on a narrow terminal", p, ok)
	}
}

func TestPanelRowAt(t *testing.T) {
	t.Parallel()
	m := mouseModel() // branches box: border y=1, label y=2, data y=3..6
	if idx, ok := m.panelRowAt(panelBranches, 3); !ok || idx != 0 {
		t.Fatalf("row at y=3 = %d,%v, want 0,true", idx, ok)
	}
	if idx, ok := m.panelRowAt(panelBranches, 4); !ok || idx != 1 {
		t.Fatalf("row at y=4 = %d,%v, want 1,true", idx, ok)
	}
	if _, ok := m.panelRowAt(panelBranches, 2); ok {
		t.Fatal("the label line must not map to a row")
	}
	if _, ok := m.panelRowAt(panelBranches, 6); ok {
		t.Fatal("padding below the last row (3 branches) must not map") // rows y=3,4,5 hold the 3 branches
	}
}

func TestPanelRowAtScrolledPanel(t *testing.T) {
	t.Parallel()
	m := mouseModel()
	m.branches = nil
	for i := 0; i < 30; i++ {
		m.branches = append(m.branches, model.Branch{Name: string(rune('a'+i%26)) + "-br"})
	}
	m.sel[panelBranches] = 20 // tab slot boxH=bodyH/3=7; rowsCap=7-3=4; windowStart(30,4,20)=18
	if idx, ok := m.panelRowAt(panelBranches, 3); !ok || idx != 18 {
		t.Fatalf("scrolled row at y=3 = %d,%v, want 18,true (windowStart consistency)", idx, ok)
	}
}

func TestWindowStartMatchesWindowRows(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ total, n, sel int }{
		{2, 5, 0}, {10, 4, 0}, {10, 4, 9}, {30, 4, 20}, {30, 4, 2},
	} {
		rows := make([]string, c.total)
		_, _, start := windowRows(rows, c.n, c.sel)
		if got := windowStart(c.total, c.n, c.sel); got != start {
			t.Errorf("windowStart(%d,%d,%d) = %d, windowRows start = %d", c.total, c.n, c.sel, got, start)
		}
	}
}

func TestWheelStepHelper(t *testing.T) {
	t.Parallel()
	m := mouseModel()
	if m.wheelStep() != 3 {
		t.Fatalf("wheelStep = %d before any config load, want 3", m.wheelStep())
	}
	m.cfg = config.Config{UI: config.UIConfig{WheelStep: 5}}
	if m.wheelStep() != 5 {
		t.Fatalf("wheelStep = %d, want configured 5", m.wheelStep())
	}
}

func mouseMsg(x, y int, b tea.MouseButton) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: b}
}

func TestClickFocusesAndSelects(t *testing.T) {
	t.Parallel()
	m := mouseModel()                                      // focus starts on Branches
	u, _ := m.Update(mouseMsg(30, 4, tea.MouseButtonLeft)) // commits, 2nd data row
	m = u.(Model)
	if m.focus != panelCommits {
		t.Fatalf("focus = %v, want commits", m.focus)
	}
	if m.sel[panelCommits] != 1 {
		t.Fatalf("sel = %d, want the clicked row 1", m.sel[panelCommits])
	}
	if m.lastLeftPanel != panelBranches {
		t.Fatalf("lastLeftPanel = %v, want branches (recorded on leaving)", m.lastLeftPanel)
	}
}

func TestClickOnLabelFocusesWithoutSelecting(t *testing.T) {
	t.Parallel()
	m := mouseModel()
	m.focus = panelCommits
	m.sel[panelBranches] = 2
	u, _ := m.Update(mouseMsg(5, 2, tea.MouseButtonLeft)) // branches label line
	m = u.(Model)
	if m.focus != panelBranches {
		t.Fatalf("focus = %v, want branches", m.focus)
	}
	if m.sel[panelBranches] != 2 {
		t.Fatalf("sel = %d, a label click must not move the selection", m.sel[panelBranches])
	}
}

func TestClickOutsidePanelsNoOps(t *testing.T) {
	t.Parallel()
	m := mouseModel()
	u, _ := m.Update(mouseMsg(5, 0, tea.MouseButtonLeft)) // header
	if got := u.(Model).focus; got != panelBranches {
		t.Fatalf("focus = %v, header click must no-op", got)
	}
}

func TestClickIgnoredUnderOverlays(t *testing.T) {
	t.Parallel()
	overlays := []func(m *Model){
		func(m *Model) { m.modal = &decisionState{} },
		func(m *Model) { *m = m.pushLayer(&worktreePopup{}) },
		func(m *Model) { *m = m.pushLayer(&repoPopup{}) },
		func(m *Model) { *m = m.pushLayer(&settingsPopup{}) },
		func(m *Model) { *m = m.pushLayer(&branchPopup{}) },
		func(m *Model) { *m = m.pushLayer(&pairOpPopup{}) },
	}
	for i, set := range overlays {
		m := mouseModel()
		set(&m)
		u, _ := m.Update(mouseMsg(30, 4, tea.MouseButtonLeft))
		mm := u.(Model)
		if mm.focus != panelBranches || mm.sel[panelCommits] != 0 {
			t.Fatalf("overlay %d: click must be ignored (focus=%v sel=%d)", i, mm.focus, mm.sel[panelCommits])
		}
	}
}

func TestWheelScrollsHoveredPanelWithoutFocus(t *testing.T) {
	t.Parallel()
	m := mouseModel() // focus Branches; 2 commits
	u, _ := m.Update(mouseMsg(30, 5, tea.MouseButtonWheelDown))
	m = u.(Model)
	if m.focus != panelBranches {
		t.Fatal("wheel must not move focus")
	}
	if m.sel[panelCommits] != 1 {
		t.Fatalf("sel = %d, wheel over commits must move ITS selection (step 3 clamped to 1)", m.sel[panelCommits])
	}
	u, _ = m.Update(mouseMsg(30, 5, tea.MouseButtonWheelUp))
	if got := u.(Model).sel[panelCommits]; got != 0 {
		t.Fatalf("sel = %d after wheel up, want 0", got)
	}
}

func TestWheelStepRespectsConfig(t *testing.T) {
	t.Parallel()
	m := mouseModel() // 3 branches
	m.cfg = config.Config{UI: config.UIConfig{WheelStep: 1}}
	u, _ := m.Update(mouseMsg(5, 4, tea.MouseButtonWheelDown))
	if got := u.(Model).sel[panelBranches]; got != 1 {
		t.Fatalf("sel = %d, configured step 1 must move by exactly 1", got)
	}
}

func TestWheelOutsidePanelsNoOps(t *testing.T) {
	t.Parallel()
	m := mouseModel()
	u, _ := m.Update(mouseMsg(5, 23, tea.MouseButtonWheelDown)) // status line
	mm := u.(Model)
	if mm.sel[panelBranches] != 0 || mm.sel[panelCommits] != 0 {
		t.Fatal("wheel outside any panel must no-op")
	}
}

func TestHelpWindowKeepsWheelPriority(t *testing.T) {
	t.Parallel()
	m := mouseModel()
	m = m.pushLayer(newContentPopup("Help — keys", helpContent()))
	u, _ := m.Update(mouseMsg(30, 5, tea.MouseButtonWheelDown))
	mm := u.(Model)
	if layerOf[*contentPopup](mm).sel != 3 {
		t.Fatalf("help sel = %d, want 3 (wheel scrolls the help window)", layerOf[*contentPopup](mm).sel)
	}
	if mm.sel[panelCommits] != 0 {
		t.Fatal("the panel under the help window must not scroll")
	}
}

func TestFilesViewTreeClickFocusesAndMovesCursor(t *testing.T) {
	t.Parallel()
	m := openFilesView(t, filesModel())
	u, _ := m.Update(mouseMsg(5, 4, tea.MouseButtonLeft)) // 2nd visible tree line
	m = u.(Model)
	if !m.filesTreeFocused {
		t.Fatal("a tree click must focus the tree side")
	}
	if m.filesView.sel != 1 {
		t.Fatalf("tree sel = %d, want the clicked line 1", m.filesView.sel)
	}
	u, _ = m.Update(mouseMsg(5, 2, tea.MouseButtonLeft)) // title line
	m = u.(Model)
	if m.filesView.sel != 1 {
		t.Fatal("a title click must not move the tree cursor")
	}
}

func TestFilesViewCommitsClickSelectsWithOneReload(t *testing.T) {
	t.Parallel()
	m := openFilesView(t, filesModel())
	u, _ := m.Update(mouseMsg(5, 4, tea.MouseButtonLeft)) // focus the tree first
	m = u.(Model)
	u, cmd := m.Update(mouseMsg(30, 4, tea.MouseButtonLeft)) // commits, 2nd row
	m = u.(Model)
	if m.filesTreeFocused {
		t.Fatal("a commits click must focus the commit side")
	}
	if m.sel[panelCommits] != 1 {
		t.Fatalf("sel = %d, want the clicked commit 1", m.sel[panelCommits])
	}
	if cmd == nil {
		t.Fatal("selecting another commit must fire ONE follow-live reload")
	}
	u, _ = m.Update(cmd())
	m = u.(Model)
	// Clicking the already-selected commit dedupes: no reload.
	_, cmd = m.Update(mouseMsg(30, 4, tea.MouseButtonLeft))
	if cmd != nil {
		t.Fatal("clicking the selected commit must not reload")
	}
}

func TestFilesViewWheelTargetsHoveredSide(t *testing.T) {
	t.Parallel()
	m := openFilesView(t, filesModel())
	u, _ := m.Update(mouseMsg(5, 5, tea.MouseButtonWheelDown)) // over the tree
	m = u.(Model)
	if m.filesView.sel == 0 {
		t.Fatal("wheel over the tree must scroll the tree")
	}
	if m.sel[panelCommits] != 0 {
		t.Fatal("wheel over the tree must not move the commit selection")
	}
	treeSel := m.filesView.sel
	u, cmd := m.Update(mouseMsg(30, 5, tea.MouseButtonWheelDown)) // over commits
	m = u.(Model)
	if m.sel[panelCommits] != 1 || cmd == nil {
		t.Fatalf("wheel over commits must move the commit selection with a reload (sel=%d)", m.sel[panelCommits])
	}
	if m.filesView.sel != treeSel {
		t.Fatal("wheel over commits must not scroll the tree")
	}
}

func TestFilesViewMouseOutsideColumnsNoOps(t *testing.T) {
	t.Parallel()
	m := openFilesView(t, filesModel())
	u, cmd := m.Update(mouseMsg(5, 0, tea.MouseButtonLeft)) // header
	mm := u.(Model)
	if mm.filesTreeFocused || mm.filesView.sel != 0 || cmd != nil {
		t.Fatal("header click must no-op in the files view")
	}
}

func TestClickCommitsFilterTyping(t *testing.T) {
	t.Parallel()
	m := mouseModel()
	m.filterPanel = panelBranches
	m.filterQuery = "fe"
	m.filterTyping = true
	u, _ := m.Update(mouseMsg(30, 4, tea.MouseButtonLeft)) // click commits
	mm := u.(Model)
	if mm.filterTyping {
		t.Fatal("a click must commit /-input mode (like enter)")
	}
	if mm.filterQuery != "fe" {
		t.Fatalf("query = %q, the committed filter must survive", mm.filterQuery)
	}
	if mm.focus != panelCommits {
		t.Fatalf("focus = %v, want the clicked panel", mm.focus)
	}
}

func TestModalOutranksHelpWindowWheel(t *testing.T) {
	t.Parallel()
	m := mouseModel()
	m = m.pushLayer(newContentPopup("Help — keys", helpContent()))
	m.modal = &decisionState{}
	u, _ := m.Update(mouseMsg(30, 5, tea.MouseButtonWheelDown))
	if got := layerOf[*contentPopup](u.(Model)).sel; got != 0 {
		t.Fatalf("help sel = %d, the modal must swallow the wheel", got)
	}
}
