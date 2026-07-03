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
