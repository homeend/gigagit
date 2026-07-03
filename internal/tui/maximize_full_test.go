package tui

import (
	"testing"
)

func TestLayoutFullscreenLeftPanel(t *testing.T) {
	m := maxModel() // 120×40: Branches/Files/Staged + Commits
	m.fullMaxed = true
	m.fullMax = panelFiles
	g := m.layout()

	if g.boxH[panelFiles] != g.bodyH {
		t.Errorf("pinned boxH = %d, want bodyH %d", g.boxH[panelFiles], g.bodyH)
	}
	if g.pos[panelFiles] != (point{0, 1}) {
		t.Errorf("pinned pos = %v, want {0,1}", g.pos[panelFiles])
	}
	if g.leftW != g.w {
		t.Errorf("leftW = %d, want full width %d", g.leftW, g.w)
	}
	if g.rightW != 0 {
		t.Errorf("rightW = %d, want 0", g.rightW)
	}
	for _, p := range []panel{panelBranches, panelStaged, panelCommits} {
		if g.boxH[p] != 0 {
			t.Errorf("%v boxH = %d, want 0 (hidden)", p, g.boxH[p])
		}
	}
}

func TestLayoutFullscreenCommits(t *testing.T) {
	m := maxModel()
	m.fullMaxed = true
	m.fullMax = panelCommits
	g := m.layout()

	if g.boxH[panelCommits] != g.bodyH || g.pos[panelCommits] != (point{0, 1}) {
		t.Errorf("commits boxH=%d pos=%v, want bodyH at {0,1}", g.boxH[panelCommits], g.pos[panelCommits])
	}
	if g.rightW != g.w || g.leftW != 0 {
		t.Errorf("leftW=%d rightW=%d, want 0 and %d", g.leftW, g.rightW, g.w)
	}
	for _, p := range []panel{panelBranches, panelFiles, panelStaged} {
		if g.boxH[p] != 0 {
			t.Errorf("%v boxH = %d, want 0 (hidden)", p, g.boxH[p])
		}
	}
}

// Fullscreen wins over a t column-pin underneath (the ladder's top level).
func TestLayoutFullscreenBeatsColumnPin(t *testing.T) {
	m := maxModel()
	m.leftMaxed = true
	m.leftMax = panelFiles
	m.fullMaxed = true
	m.fullMax = panelFiles
	g := m.layout()
	if g.leftW != g.w || g.boxH[panelCommits] != 0 {
		t.Errorf("leftW=%d commits boxH=%d, want full width and hidden commits", g.leftW, g.boxH[panelCommits])
	}
}

// Stale pin: fullMax not in the visible set ⇒ normal split, never a blank screen.
func TestLayoutFullscreenStalePinFallsBack(t *testing.T) {
	m := maxModel()
	m.fullMaxed = true
	m.fullMax = panelRemotes // NOT the active top tab (panelBranches)
	g := m.layout()
	for _, p := range []panel{panelBranches, panelFiles, panelStaged, panelCommits} {
		if g.boxH[p] <= 0 {
			t.Errorf("fallback: %v should be visible, boxH=%d", p, g.boxH[p])
		}
	}
}

// Full-screen-ish surfaces win: files view, stash list, file preview all need
// their column, so an active one suspends the fullscreen pin (it resumes when
// the surface closes — the flag is not cleared).
func TestFullMaxActiveYieldsToSurfaces(t *testing.T) {
	m := maxModel()
	m.fullMaxed = true
	m.fullMax = panelFiles
	if !m.fullMaxActive() {
		t.Fatal("baseline: pin should be active")
	}
	fv := m
	fv.filesView = &contentPopup{}
	if fv.fullMaxActive() {
		t.Error("filesView active: pin must yield")
	}
	sv := m
	sv.stashView = &stashView{}
	if sv.fullMaxActive() {
		t.Error("stashView active: pin must yield")
	}
	pv := m
	pv.filesPreview = &contentPopup{}
	if pv.fullMaxActive() {
		t.Error("filesPreview active: pin must yield")
	}
}

func TestCanFullMaximize(t *testing.T) {
	m := maxModel()
	for _, p := range []panel{panelBranches, panelFiles, panelStaged, panelCommits} {
		m.focus = p
		if !m.canFullMaximize() {
			t.Errorf("focus %v: want canFullMaximize", p)
		}
	}
	fv := m
	fv.filesView = &contentPopup{}
	if fv.canFullMaximize() {
		t.Error("filesView active: T must be inert")
	}
}

// press dispatches one key and unwraps the model.
func press(t *testing.T, m Model, key string) Model {
	t.Helper()
	u, _ := m.Update(keyMsg(key))
	return u.(Model)
}

func TestFullscreenToggleT(t *testing.T) {
	m := maxModel()
	m.focus = panelFiles

	m = press(t, m, "T")
	if !m.fullMaxed || m.fullMax != panelFiles {
		t.Fatalf("after T: fullMaxed=%v fullMax=%v", m.fullMaxed, m.fullMax)
	}
	if m.leftMaxed {
		t.Fatal("T must not set the t column pin")
	}
	m = press(t, m, "T")
	if m.fullMaxed {
		t.Fatal("second T must restore")
	}
}

func TestFullscreenOnCommits(t *testing.T) {
	m := maxModel()
	m.focus = panelCommits
	m = press(t, m, "T")
	if !m.fullMaxed || m.fullMax != panelCommits {
		t.Fatalf("after T on Commits: fullMaxed=%v fullMax=%v", m.fullMaxed, m.fullMax)
	}
	// t stays inert on Commits, fullscreen or not.
	m = press(t, m, "t")
	if m.leftMaxed || !m.fullMaxed {
		t.Fatalf("t on fullscreen Commits: leftMaxed=%v fullMaxed=%v, want false/true", m.leftMaxed, m.fullMaxed)
	}
}

// t → T → T lands back on column-maximized: the t pin survives underneath.
func TestLadderColumnThenFullscreenThenBack(t *testing.T) {
	m := maxModel()
	m.focus = panelFiles
	m = press(t, m, "t")
	m = press(t, m, "T")
	if !m.fullMaxed || !m.leftMaxed {
		t.Fatalf("t then T: fullMaxed=%v leftMaxed=%v, want both", m.fullMaxed, m.leftMaxed)
	}
	m = press(t, m, "T")
	if m.fullMaxed || !m.leftMaxed || m.leftMax != panelFiles {
		t.Fatalf("T again: fullMaxed=%v leftMaxed=%v leftMax=%v, want column-maximized Files", m.fullMaxed, m.leftMaxed, m.leftMax)
	}
}

// t while fullscreen drops exactly one level: to column-maximized, never a
// hidden double-toggle back to normal.
func TestLadderTDropsFullscreenToColumn(t *testing.T) {
	m := maxModel()
	m.focus = panelFiles
	m = press(t, m, "T")
	m = press(t, m, "t")
	if m.fullMaxed {
		t.Fatal("t while fullscreen must clear the fullscreen pin")
	}
	if !m.leftMaxed || m.leftMax != panelFiles {
		t.Fatalf("t while fullscreen: leftMaxed=%v leftMax=%v, want column pin on Files", m.leftMaxed, m.leftMax)
	}
}

func TestEscExitsFullscreen(t *testing.T) {
	m := maxModel()
	m.focus = panelFiles
	m = press(t, m, "T")
	m = press(t, m, "esc")
	if m.fullMaxed {
		t.Fatal("esc must exit fullscreen")
	}
}

// esc is the lowest-priority consumer: an active filter clears first and the
// same press must NOT also drop fullscreen.
func TestEscPrefersFilterOverFullscreen(t *testing.T) {
	m := maxModel()
	m.focus = panelFiles
	m = press(t, m, "T")
	m.filterQuery = "x"
	m = press(t, m, "esc")
	if m.filterQuery != "" {
		t.Fatal("esc should clear the filter")
	}
	if !m.fullMaxed {
		t.Fatal("the filter-clearing esc must not also exit fullscreen")
	}
	m = press(t, m, "esc")
	if m.fullMaxed {
		t.Fatal("second esc exits fullscreen")
	}
}

func TestFullscreenInertInFilesView(t *testing.T) {
	m := maxModel()
	m.focus = panelFiles
	m.filesView = &contentPopup{}
	m = press(t, m, "T")
	if m.fullMaxed {
		t.Fatal("T must be inert while the files view owns the screen")
	}
}
