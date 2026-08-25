package tui

import (
	"slices"
	"strings"
	"testing"
)

// maxModel is a left-column-focused model: branches + a couple of status files,
// wide and tall enough for the three-box left split.
func maxModel() Model {
	m := footerModel()
	m.dispModes = map[panel]dispMode{}
	m.hscroll = map[panel]int{}
	return m
}

func TestLeftColumnPanelsTall(t *testing.T) {
	t.Parallel()
	m := maxModel() // width 120, height 40
	got := m.leftColumnPanels()
	want := []panel{panelBranches, panelFiles, panelStaged}
	if !slices.Equal(got, want) {
		t.Fatalf("tall terminal: got %v, want %v", got, want)
	}
}

func TestLeftColumnPanelsShortDropsStaged(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.height = 14 // bodyH = 11 < 12, so the normal split drops Staged
	got := m.leftColumnPanels()
	want := []panel{panelBranches, panelFiles}
	if !slices.Equal(got, want) {
		t.Fatalf("short terminal: got %v, want %v", got, want)
	}
}

func TestLeftColumnPanelsNarrowEmpty(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.width = 30
	if got := m.leftColumnPanels(); len(got) != 0 {
		t.Fatalf("narrow terminal: got %v, want empty", got)
	}
}

func TestLeftColumnPanelsReflectsActiveTabs(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.activeLeftTab = panelRemotes
	m.activeFilesTab = panelTags
	got := m.leftColumnPanels()
	want := []panel{panelRemotes, panelTags, panelStaged}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLayoutMaximizesPinnedPanel(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.leftMaxed = true
	m.leftMax = panelFiles
	g := m.layout()

	if g.boxH[panelFiles] != g.bodyH {
		t.Errorf("pinned panel boxH = %d, want bodyH %d", g.boxH[panelFiles], g.bodyH)
	}
	if g.pos[panelFiles] != (point{0, 1}) {
		t.Errorf("pinned panel pos = %v, want {0,1}", g.pos[panelFiles])
	}
	if g.boxH[panelBranches] != 0 {
		t.Errorf("top tab boxH = %d, want 0 (hidden)", g.boxH[panelBranches])
	}
	if g.boxH[panelStaged] != 0 {
		t.Errorf("staged boxH = %d, want 0 (hidden)", g.boxH[panelStaged])
	}
	// Commits geometry is untouched by maximize.
	if g.boxH[panelCommits] != g.bodyH || g.pos[panelCommits] != (point{g.leftW, 1}) {
		t.Errorf("commits geometry changed: boxH=%d pos=%v", g.boxH[panelCommits], g.pos[panelCommits])
	}
}

func TestLayoutNormalSplitWhenNotMaximized(t *testing.T) {
	t.Parallel()
	m := maxModel() // leftMaxed false
	g := m.layout()
	for _, p := range []panel{panelBranches, panelFiles, panelStaged} {
		if g.boxH[p] <= 0 {
			t.Errorf("normal split: %v should be visible, boxH=%d", p, g.boxH[p])
		}
	}
}

// TestLayoutMaximizePinnedNotVisibleFallsBack guards the invariant: if the
// pinned panel somehow isn't in the visible left set (a future writer switches
// the active tab without re-pinning), layout must fall back to the normal split
// rather than delete every left box and blank the column.
func TestLayoutMaximizePinnedNotVisibleFallsBack(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.leftMaxed = true
	m.leftMax = panelRemotes // NOT the active top tab (panelBranches)
	g := m.layout()
	for _, p := range []panel{panelBranches, panelFiles, panelStaged} {
		if g.boxH[p] <= 0 {
			t.Errorf("fallback: %v should be visible in the normal split, boxH=%d", p, g.boxH[p])
		}
	}
}

func TestFocusOrderCollapsesWhenMaximized(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.leftMaxed = true
	m.leftMax = panelFiles
	got := m.focusOrder()
	want := []panel{panelFiles, panelCommits}
	if !slices.Equal(got, want) {
		t.Fatalf("maximized focusOrder = %v, want %v", got, want)
	}
}

func TestFocusOrderNormalWhenNotMaximized(t *testing.T) {
	t.Parallel()
	m := maxModel()
	got := m.focusOrder()
	want := []panel{panelBranches, panelFiles, panelStaged, panelCommits}
	if !slices.Equal(got, want) {
		t.Fatalf("focusOrder = %v, want %v", got, want)
	}
}

func TestLeftReturnTargetIsPinnedWhenMaximized(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.leftMaxed = true
	m.leftMax = panelStaged
	if got := m.leftReturnTarget(); got != panelStaged {
		t.Fatalf("leftReturnTarget = %v, want panelStaged", got)
	}
}

func TestMaximizeKeyTogglesFocusedLeftPanel(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelBranches

	u, _ := m.Update(keyMsg("t"))
	m = u.(Model)
	if !m.leftMaxed || m.leftMax != panelBranches {
		t.Fatalf("after t: leftMaxed=%v leftMax=%v, want true/panelBranches", m.leftMaxed, m.leftMax)
	}

	u, _ = m.Update(keyMsg("t"))
	m = u.(Model)
	if m.leftMaxed {
		t.Fatalf("second t must restore: leftMaxed=%v", m.leftMaxed)
	}
}

func TestMaximizeKeyNoOpOnCommits(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelCommits
	u, _ := m.Update(keyMsg("t"))
	if u.(Model).leftMaxed {
		t.Fatal("t on Commits must be a no-op")
	}
}

func TestMaximizeKeyNoOpOnNarrowTerminal(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.width = 30
	m.focus = panelBranches
	u, _ := m.Update(keyMsg("t"))
	if u.(Model).leftMaxed {
		t.Fatal("t on a narrow terminal must be a no-op")
	}
}

func TestMaximizeKeyNoOpWhileFilesViewOpen(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelFiles
	m.filesView = &contentPopup{} // a full-screen surface owns the left area
	u, _ := m.Update(keyMsg("t"))
	if u.(Model).leftMaxed {
		t.Fatal("t while the files view is open must be a no-op")
	}
}

func TestCtrlArrowRepinsTopTab(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelBranches
	m.leftMaxed = true
	m.leftMax = panelBranches

	u, _ := m.Update(keyMsg("ctrl+right"))
	m = u.(Model)
	if m.activeLeftTab != panelRemotes {
		t.Fatalf("activeLeftTab = %v, want panelRemotes", m.activeLeftTab)
	}
	if m.focus != panelRemotes || m.leftMax != panelRemotes {
		t.Fatalf("re-pin failed: focus=%v leftMax=%v, want panelRemotes", m.focus, m.leftMax)
	}
	if !m.leftMaxed {
		t.Fatal("still maximized after re-pin")
	}
}

func TestCtrlArrowRepinsMiddleTab(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelFiles
	m.activeFilesTab = panelFiles
	m.leftMaxed = true
	m.leftMax = panelFiles

	u, _ := m.Update(keyMsg("ctrl+right"))
	m = u.(Model)
	if m.activeFilesTab != panelTags || m.focus != panelTags || m.leftMax != panelTags {
		t.Fatalf("middle re-pin failed: activeFilesTab=%v focus=%v leftMax=%v", m.activeFilesTab, m.focus, m.leftMax)
	}
}

func TestCtrlArrowFlipsPinnedBottomTab(t *testing.T) {
	t.Parallel()
	// The bottom slot is now a tab group (Staged ⇄ Reflog), so ctrl+arrow on a
	// pinned Staged flips to a maximized Reflog (re-pinning), and back. The top
	// tab is untouched.
	m := maxModel()
	m.focus = panelStaged
	m.leftMaxed = true
	m.leftMax = panelStaged
	before := m.activeLeftTab

	u, _ := m.Update(keyMsg("ctrl+right"))
	m = u.(Model)
	if m.focus != panelReflog || m.leftMax != panelReflog || m.activeLeftTab != before {
		t.Fatalf("ctrl+right on pinned Staged must flip to a pinned Reflog: focus=%v leftMax=%v activeLeftTab=%v",
			m.focus, m.leftMax, m.activeLeftTab)
	}

	u, _ = m.Update(keyMsg("ctrl+left"))
	m = u.(Model)
	if m.focus != panelStaged || m.leftMax != panelStaged || m.activeLeftTab != before {
		t.Fatalf("ctrl+left must flip back to a pinned Staged: focus=%v leftMax=%v activeLeftTab=%v",
			m.focus, m.leftMax, m.activeLeftTab)
	}
}

// TestRenderMaximizedHidesOtherLeftPanels is the end-to-end proof: with a panel
// maximized, the rendered left column shows ONLY that panel's box — the others
// (here Staged) are not drawn at all, so no degenerate zero-height box appears.
func TestRenderMaximizedHidesOtherLeftPanels(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelBranches

	normal := m.renderInterface()
	if !strings.Contains(normal, "Staged") {
		t.Fatal("normal split should render the Staged box")
	}

	m.leftMaxed = true
	m.leftMax = panelBranches
	maxed := m.renderInterface()
	if strings.Contains(maxed, "Staged") {
		t.Error("maximized on Branches must hide the Staged box")
	}
	if !strings.Contains(maxed, "Branches") {
		t.Error("maximized Branches box must still render")
	}
}
