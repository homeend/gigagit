package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestLayoutFullscreenLeftPanel(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

// countBoxTops counts rendered box top-left corners — lines that BEGIN with
// the rounded-border glyph ╭ (immediately after a newline, or at the very
// start of the view). This is robust against unrelated ╭ occurrences inside
// box content: the commit-graph's fork glyph (internal/commitgraph/graph.go)
// also renders as ╭, but only mid-line (after the panel's left border and
// padding), never as the first byte of a rendered line, so a raw
// strings.Count(v, "╭") would over-count once a fixture has a forked commit.
func countBoxTops(v string) int {
	n := strings.Count(v, "\n╭")
	if strings.HasPrefix(v, "╭") {
		n++
	}
	return n
}

func TestFullscreenToggleT(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelFiles

	m = press(t, m, "ctrl+t")
	if !m.fullMaxed || m.fullMax != panelFiles {
		t.Fatalf("after T: fullMaxed=%v fullMax=%v", m.fullMaxed, m.fullMax)
	}
	if m.leftMaxed {
		t.Fatal("T must not set the t column pin")
	}
	m = press(t, m, "ctrl+t")
	if m.fullMaxed {
		t.Fatal("second T must restore")
	}
}

func TestFullscreenOnCommits(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelCommits
	m = press(t, m, "ctrl+t")
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
	t.Parallel()
	m := maxModel()
	m.focus = panelFiles
	m = press(t, m, "t")
	m = press(t, m, "ctrl+t")
	if !m.fullMaxed || !m.leftMaxed {
		t.Fatalf("t then T: fullMaxed=%v leftMaxed=%v, want both", m.fullMaxed, m.leftMaxed)
	}
	m = press(t, m, "ctrl+t")
	if m.fullMaxed || !m.leftMaxed || m.leftMax != panelFiles {
		t.Fatalf("T again: fullMaxed=%v leftMaxed=%v leftMax=%v, want column-maximized Files", m.fullMaxed, m.leftMaxed, m.leftMax)
	}
}

// t while fullscreen drops exactly one level: to column-maximized, never a
// hidden double-toggle back to normal.
func TestLadderTDropsFullscreenToColumn(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelFiles
	m = press(t, m, "ctrl+t")
	m = press(t, m, "t")
	if m.fullMaxed {
		t.Fatal("t while fullscreen must clear the fullscreen pin")
	}
	if !m.leftMaxed || m.leftMax != panelFiles {
		t.Fatalf("t while fullscreen: leftMaxed=%v leftMax=%v, want column pin on Files", m.leftMaxed, m.leftMax)
	}
}

func TestEscExitsFullscreen(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelFiles
	m = press(t, m, "ctrl+t")
	m = press(t, m, "esc")
	if m.fullMaxed {
		t.Fatal("esc must exit fullscreen")
	}
}

// esc is the lowest-priority consumer: an active filter clears first and the
// same press must NOT also drop fullscreen.
func TestEscPrefersFilterOverFullscreen(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelFiles
	m = press(t, m, "ctrl+t")
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
	t.Parallel()
	m := maxModel()
	m.focus = panelFiles
	m.filesView = &contentPopup{}
	m = press(t, m, "ctrl+t")
	if m.fullMaxed {
		t.Fatal("T must be inert while the files view owns the screen")
	}
}

func TestFocusOrderCollapsesFullscreen(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelFiles
	m = press(t, m, "ctrl+t")
	got := m.focusOrder()
	if len(got) != 1 || got[0] != panelFiles {
		t.Fatalf("fullscreen focusOrder = %v, want [Files]", got)
	}
	// tab cycles nowhere
	m = press(t, m, "tab")
	if m.focus != panelFiles {
		t.Fatalf("tab moved focus to %v, want pinned Files", m.focus)
	}
}

// A stale fullscreen pin must not trap focus on a hidden panel: focusOrder
// falls back to the normal order, mirroring layout's stale-pin fallback.
func TestFocusOrderStaleFullscreenPinFallsBack(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.fullMaxed = true
	m.fullMax = panelRemotes // not visible
	got := m.focusOrder()
	want := []panel{panelBranches, panelFiles, panelStaged, panelCommits}
	if !slices.Equal(got, want) {
		t.Fatalf("stale pin focusOrder = %v, want %v", got, want)
	}
}

func TestArrowsStayInsideFullscreen(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelFiles
	m = press(t, m, "ctrl+t")
	m = press(t, m, "right")
	if m.focus != panelFiles {
		t.Fatalf("→ moved focus to hidden %v", m.focus)
	}

	c := maxModel()
	c.focus = panelCommits
	c = press(t, c, "ctrl+t")
	c = press(t, c, "left")
	if c.focus != panelCommits {
		t.Fatalf("← moved focus to hidden %v", c.focus)
	}
}

// ctrl+→ while fullscreen re-pins fullscreen to the newly shown tab (mirrors
// the leftMaxed re-pin in activateTab). From Commits the pin transfers to the
// activated left tab instead of stranding focus on a hidden box.
func TestTabSwitchRepinsFullscreen(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelBranches
	m = press(t, m, "ctrl+t")
	m = m.activateTab(panelWorktrees)
	if !m.fullMaxed || m.fullMax != panelWorktrees {
		t.Fatalf("after tab switch: fullMaxed=%v fullMax=%v, want Worktrees pinned", m.fullMaxed, m.fullMax)
	}

	c := maxModel()
	c.focus = panelCommits
	c = press(t, c, "ctrl+t")
	c = c.activateTab(panelWorktrees)
	if !c.fullMaxed || c.fullMax != panelWorktrees || c.focus != panelWorktrees {
		t.Fatalf("from Commits: fullMaxed=%v fullMax=%v focus=%v, want Worktrees", c.fullMaxed, c.fullMax, c.focus)
	}
}

// A deliberate jump-to-Commits action transfers the fullscreen pin instead of
// stranding focus on a hidden panel (same rule as activateTab's re-pin).
func TestFocusCommitsPanelTransfersFullscreenPin(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelBranches
	m = press(t, m, "ctrl+t")
	m = m.focusCommitsPanel()
	if m.focus != panelCommits {
		t.Fatalf("focus = %v, want Commits", m.focus)
	}
	if !m.fullMaxed || m.fullMax != panelCommits {
		t.Fatalf("fullMaxed=%v fullMax=%v, want pin transferred to Commits", m.fullMaxed, m.fullMax)
	}
	// without a pin the helper is a plain focus move
	n := maxModel()
	n.focus = panelBranches
	n = n.focusCommitsPanel()
	if n.fullMaxed || n.focus != panelCommits {
		t.Fatalf("no-pin case: fullMaxed=%v focus=%v", n.fullMaxed, n.focus)
	}
}

// End-to-end: "Solo this tag" while Tags is the fullscreen pin must transfer
// the pin to Commits, not strand focus on the hidden Tags panel (the finding
// from review — file_preview.go/file_finder.go/tags_actions.go/commit_scope.go
// all wrote m.focus = panelCommits directly, bypassing the pin).
func TestTagSoloTransfersFullscreenPinEndToEnd(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.activeFilesTab = panelTags
	m.tags = []model.Tag{{Name: "v1.0.0"}}
	m.focus = panelTags
	m = press(t, m, "ctrl+t")
	if !m.fullMaxed || m.fullMax != panelTags {
		t.Fatalf("setup: fullMaxed=%v fullMax=%v, want pinned on Tags", m.fullMaxed, m.fullMax)
	}
	row, ok := m.tagSoloRow()
	if !ok {
		t.Fatal("tagSoloRow not offered while Tags is focused")
	}
	tm, _ := row.run(m)
	m = tm.(Model)
	if m.focus != panelCommits {
		t.Fatalf("focus = %v, want Commits", m.focus)
	}
	if !m.fullMaxed || m.fullMax != panelCommits {
		t.Fatalf("fullMaxed=%v fullMax=%v, want pin transferred to Commits", m.fullMaxed, m.fullMax)
	}
}

// The pin transfer is only coherent when the pin goes live immediately: a
// jump fired under a suspending surface must leave the pin alone (the
// surface's close path restores its own remembered focus, which would
// otherwise mismatch a rewritten pin). The stash list is the exception —
// focusCommitsPanel closes it itself (see below), so this contract now
// covers the surfaces it does NOT close: the files view / file preview.
func TestFocusCommitsPanelNoTransferWhileYielded(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelStaged
	m = press(t, m, "ctrl+t") // pin Staged
	m.filesPreview = &contentPopup{}
	m = m.focusCommitsPanel()
	if m.focus != panelCommits {
		t.Fatalf("focus = %v, want Commits", m.focus)
	}
	if m.fullMax != panelStaged {
		t.Fatalf("fullMax = %v, want untouched Staged pin", m.fullMax)
	}
}

// A covering stash list is closed by focusCommitsPanel itself (landing in the
// feed is the point), so the pin goes live immediately and transfers to
// Commits — leaving it on the old panel would resume a fullscreen that hides
// the commit the user just jumped to.
func TestFocusCommitsPanelClosesStashAndTransfersPin(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelStaged
	m = press(t, m, "ctrl+t") // pin Staged
	m = press(t, m, "S")      // stash list opens (suspends the pin)
	m = m.focusCommitsPanel()
	if m.stashView != nil {
		t.Fatal("focusCommitsPanel must close the covering stash list")
	}
	if m.focus != panelCommits {
		t.Fatalf("focus = %v, want Commits", m.focus)
	}
	if !m.fullMaxed || m.fullMax != panelCommits {
		t.Fatalf("fullMaxed=%v fullMax=%v, want pin transferred to Commits", m.fullMaxed, m.fullMax)
	}
}

// Closing the stash list over a Commits fullscreen must land focus back on
// Commits — lastLeftPanel would strand focus on a panel the resuming pin
// hides (T on Commits, S, esc = three ordinary keystrokes).
func TestStashCloseRestoresCommitsPin(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelCommits
	m = press(t, m, "ctrl+t")
	m = press(t, m, "S")   // open stash list (suspends the pin)
	m = press(t, m, "esc") // close it
	if m.focus != panelCommits || !m.fullMaxed || m.fullMax != panelCommits {
		t.Fatalf("after S+esc: focus=%v fullMaxed=%v fullMax=%v, want Commits pin live", m.focus, m.fullMaxed, m.fullMax)
	}
}

// Regression (found in Task 2 review): before focusOrder was gated on the
// fullscreen pin, tab could silently drift focus off the pinned panel and a
// following t would column-pin the WRONG (hidden) panel. The full sequence
// must land on the panel that was actually on screen.
func TestLadderTabThenTDropsToPinnedPanel(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelFiles
	m = press(t, m, "ctrl+t")
	m = press(t, m, "tab") // must not drift: focusOrder is collapsed
	m = press(t, m, "t")
	if m.fullMaxed {
		t.Fatal("t must drop fullscreen")
	}
	if !m.leftMaxed || m.leftMax != panelFiles {
		t.Fatalf("leftMax=%v, want the on-screen panel Files", m.leftMax)
	}
}

// View must not draw a degenerate 0-width column: no Commits box under a
// left-panel fullscreen, no left boxes (tab labels included) under a Commits
// fullscreen, and never a panic.
func TestViewFullscreenLeftPanelHidesCommits(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelFiles
	m = press(t, m, "ctrl+t")
	v := m.View()
	if strings.Contains(v, "Commits (") {
		t.Error("fullscreen Files: Commits box should not render")
	}
	// The "Commits (" substring check above passes even pre-fix: rightW==0
	// clamps renderPanel's innerW to 1, truncating the label to "…" before it
	// ever reaches this string. The real, visible bug is a degenerate 0-width
	// Commits box still drawn beside the fullscreen Files box (a second
	// bordered sliver: "╭───╮ / │ … │ / ╰───╯"). A correctly-rendered
	// fullscreen body draws exactly one box, so exactly one ╭. A RAW count is
	// required here: a leaked Commits sliver is joined to the RIGHT of the
	// full-width Files box, so its ╭ sits mid-line where countBoxTops would
	// miss it. It is also collision-safe here: no Commits panel renders in
	// the expected state, so no commit-graph fork glyph can appear.
	if n := strings.Count(v, "╭"); n != 1 {
		t.Errorf("fullscreen Files: want exactly 1 box top (╭), found %d (degenerate column)", n)
	}
}

func TestViewFullscreenCommitsHidesLeftColumn(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelCommits
	m = press(t, m, "ctrl+t")
	v := m.View()
	if !strings.Contains(v, "Commits (") {
		t.Error("fullscreen Commits: Commits box missing")
	}
	if strings.Contains(v, "Branches") {
		t.Error("fullscreen Commits: left-column labels should not render")
	}
	// "Staged" is NOT checked above: it is also a legitimate WIP pseudo-commit
	// row label drawn INSIDE the Commits panel (wip_rows.go wipRow.label()), so
	// the substring check would only pass by coincidence of the fixture having
	// zero staged files. Like the sibling test, a leaked degenerate 0-width
	// left box truncates its label to "…" before it ever reaches a substring
	// check anyway. A correctly-rendered fullscreen body draws exactly one box,
	// so exactly one top-left corner glyph; a leaked left column draws two.
	// countBoxTops (not a raw glyph count) so a forked commit's ╭ in the graph
	// gutter can't collide with the box-border glyph.
	if n := countBoxTops(v); n != 1 {
		t.Errorf("fullscreen Commits: want exactly 1 box top (╭), found %d (degenerate column)", n)
	}
}

// The general resume rule: any pin-resume point re-asserts focus == fullMax.
func TestReconcileFullscreenFocus(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelFiles
	m = press(t, m, "ctrl+t")
	m.focus = panelCommits // simulate a surface having moved focus
	m = m.reconcileFullscreenFocus()
	if m.focus != panelFiles {
		t.Fatalf("focus = %v, want re-asserted Files pin", m.focus)
	}
}

// Left-panel pin survives a stash excursion even when lastLeftPanel drifts.
func TestStashCloseRestoresLeftPanelPin(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelFiles
	m = press(t, m, "ctrl+t")
	m = press(t, m, "S")
	m.lastLeftPanel = panelBranches // drift during the excursion
	m = press(t, m, "esc")
	if m.focus != panelFiles || !m.fullMaxed || m.fullMax != panelFiles {
		t.Fatalf("after stash excursion: focus=%v fullMaxed=%v fullMax=%v, want Files pin live", m.focus, m.fullMaxed, m.fullMax)
	}
}

// esc must not clear a pin that isn't driving the layout (suspended by a
// surface) — the user would see nothing happen and lose the pin silently.
func TestEscIgnoresSuspendedPin(t *testing.T) {
	t.Parallel()
	m := maxModel()
	m.focus = panelFiles
	m = press(t, m, "ctrl+t")
	m.stashView = &stashView{} // suspend
	m.focus = panelFiles
	m = press(t, m, "esc")
	if !m.fullMaxed {
		t.Fatal("esc while suspended must not clear the pin")
	}
}
