package tui

import "testing"

// The window fields live in one group: saving the live group into a slot
// and loading an empty one leaves no window on the Model, and loading the
// saved one brings every field back. The test touches one field of each
// family; the compile-time guarantee is the embed itself.
func TestWindowStateRoundTrip(t *testing.T) {
	m := loadedModel(t)
	m = m.pushLayer(&diffView{title: "f"})
	m.filesView = &contentPopup{}
	m.filesMode = filesModeWorktree
	m.wtFiles = &worktreeFiles{query: "q"}
	m.stashView = &stashView{}
	m.diffTag = "tag"
	m.pendingSteer = &pendingSteer{}
	m.versionsGen = 7
	m.tour = "ov-1"
	saved := m.windowState
	m.windowState = windowState{}
	if m.topLayer() != nil || m.filesView != nil || m.wtFiles != nil || m.stashView != nil || m.diffTag != "" || m.pendingSteer != nil || m.versionsGen != 0 || m.tour != "" {
		t.Fatal("a window field survived an empty group")
	}
	m.windowState = saved
	if m.topLayer() == nil || m.filesView == nil || m.wtFiles == nil || m.wtFiles.query != "q" || m.stashView == nil || m.diffTag != "tag" || m.pendingSteer == nil || m.versionsGen != 7 || m.tour != "ov-1" {
		t.Fatal("a window field did not come back")
	}
}
