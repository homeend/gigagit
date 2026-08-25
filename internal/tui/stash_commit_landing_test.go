package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// The reported bug: with the stash window open and focus released to the left
// column (←), ctrl+g on Branches soloed the feed and — once the reload landed —
// focused panelCommits while the stash list still covered it: keys fell into
// the stash view, and the hidden commit row's reveal drew over the stash box.
// Landing in the Commits feed must first close the surface that covers it.
func TestFocusCommitsPanelClosesStashView(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.focus = panelFiles
	mm, _ := m.Update(keyMsg("S")) // stash window opens; lastLeftPanel=panelFiles
	m = mm.(Model)
	m.focus = panelBranches // ← released focus to the left column

	m = m.focusCommitsPanel()
	if m.stashView != nil {
		t.Fatal("focusing the Commits feed must close the stash window covering it")
	}
	if m.focus != panelCommits {
		t.Fatalf("focus = %v, want panelCommits", m.focus)
	}
}

// End-to-end: stash open → ← to Branches → ctrl+g (solo + tip). Once the scope
// reload lands and the goto-tip drain runs, the stash window must be closed and
// the cursor must sit in the (now visible) Commits feed.
func TestStashOpenBranchCtrlGLandsInVisibleCommits(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.focus = panelFiles
	mm, _ := m.Update(keyMsg("S"))
	m = mm.(Model)
	mm, _ = m.updateStashViewKey(keyMsg("left")) // focus → left column
	m = mm.(Model)
	m.focus = panelBranches

	mm, cmd := m.Update(keyMsg("ctrl+g"))
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("ctrl+g on Branches should fire the solo feed reload")
	}
	if m.pendingGotoTip == "" {
		t.Fatal("ctrl+g should remember the tip for the post-reload landing")
	}
	mm, _ = m.Update(cmd()) // reload lands → goto-tip drain
	got := mm.(Model)
	if got.stashView != nil {
		t.Fatal("landing on the tip must close the stash window (the feed was hidden behind it)")
	}
	if got.focus != panelCommits {
		t.Fatalf("focus = %v, want panelCommits (the revealed feed)", got.focus)
	}
}

// enter on Branches ("Go to tip in commits") shares the landing path and must
// equally close the covering stash window.
func TestStashOpenBranchEnterGotoTipClosesStash(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.focus = panelFiles
	mm, _ := m.Update(keyMsg("S"))
	m = mm.(Model)
	m.focus = panelBranches

	mm, _ = m.Update(keyMsg("enter"))
	got := mm.(Model)
	if got.stashView != nil {
		t.Fatal("goto-tip must close the stash window covering the Commits feed")
	}
	if got.focus != panelCommits {
		t.Fatalf("focus = %v, want panelCommits", got.focus)
	}
}

// A tip that isn't loaded falls back to the eager deep-search; the stash window
// must close up front so the search lands in a visible feed.
func TestGotoCommitByHashClosesStashBeforeEagerFallback(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.focus = panelFiles
	mm, _ := m.Update(keyMsg("S"))
	m = mm.(Model)

	m2, _ := m.gotoCommitByHash("0000000000000000000000000000000000000000")
	if m2.stashView != nil {
		t.Fatal("gotoCommitByHash must close the stash window even on the eager-search fallback path")
	}
}

// Guard against a stale entry: closing via the landing path must keep the
// selection valid for the stash list is gone (smoke: render doesn't panic).
func TestStashCloseViaLandingRendersCleanly(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.width, m.height = 100, 30
	m.focus = panelFiles
	mm, _ := m.Update(keyMsg("S"))
	m = mm.(Model)
	m.stashView.entries = []model.StashEntry{{Ref: "stash@{0}", Subject: "WIP"}}
	m.stashView.loading = false
	m = m.focusCommitsPanel()
	_ = m.View()
}
