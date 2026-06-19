package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/model"
)

func loadedModel(t *testing.T) Model {
	t.Helper()
	repo := newRepo(t)
	m := New(domain.New(repo))
	updated, _ := m.Update(m.loadCmd()())
	return updated.(Model)
}

func TestTabCyclesFocus(t *testing.T) {
	m := loadedModel(t)
	start := m.focus
	updated, _ := m.Update(keyMsg("tab"))
	if updated.(Model).focus == start {
		t.Fatal("tab should change the focused panel")
	}
}

func TestDownClampsSelectionInFocusedPanel(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelBranches
	updated, _ := m.Update(keyMsg("down"))
	mm := updated.(Model)
	// With a single branch, selection must clamp at 0 (no out-of-range).
	if mm.sel[panelBranches] != 0 {
		t.Fatalf("selection = %d, want clamped 0 with one item", mm.sel[panelBranches])
	}
}

func TestViewRendersPanelsWithoutPanic(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 30
	// Focus off Branches: the checked-out branch row now carries its (long)
	// worktree path, so at Branches focus the truncation tooltip correctly
	// overlays the "Branches" header. Focusing Commits keeps every panel label
	// visible for this render smoke check.
	m.focus = panelCommits
	out := m.View()
	if !strings.Contains(out, "main") {
		t.Fatalf("view should mention branch 'main':\n%s", out)
	}
	for _, label := range []string{"Branches", "Files", "Commits"} {
		if !strings.Contains(out, label) {
			t.Fatalf("view missing panel label %q:\n%s", label, out)
		}
	}
}

func TestTabCyclesActiveTabStatusCommits(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24 // bodyH 21 >= 12 → Staged visible
	m.focus = panelBranches    // the active tab
	var got []panel
	for i := 0; i < 3; i++ {
		u, _ := m.Update(keyMsg("tab"))
		m = u.(Model)
		got = append(got, m.focus)
	}
	want := []panel{panelFiles, panelStaged, panelCommits}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tab walk[%d] = %v, want %v (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestTabNeverFocusesInactiveTab(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	m.activeLeftTab = panelBranches
	m.focus = panelBranches
	for i := 0; i < 8; i++ {
		u, _ := m.Update(keyMsg("tab"))
		m = u.(Model)
		if m.focus == panelWorktrees {
			t.Fatal("tab focused the inactive Worktrees tab")
		}
	}
}

func TestCtrlArrowSwitchesAndFocusesTab(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	m.focus = panelBranches
	if m.activeLeftTab != panelBranches {
		t.Fatal("default active tab should be Branches")
	}
	u, _ := m.Update(keyMsg("ctrl+right"))
	mm := u.(Model)
	if mm.activeLeftTab != panelRemotes {
		t.Errorf("ctrl+right: active tab = %v, want Remotes", mm.activeLeftTab)
	}
	if mm.focus != panelRemotes {
		t.Errorf("ctrl+right: focus = %v, want Remotes (focus follows the tab)", mm.focus)
	}
	u2, _ := mm.Update(keyMsg("ctrl+left"))
	if u2.(Model).activeLeftTab != panelBranches {
		t.Error("ctrl+left should switch back to Branches")
	}
}

func TestCtrlRightCyclesBranchesRemotesWorktrees(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	m.focus = panelBranches

	send := func(k string) {
		u, _ := m.Update(keyMsg(k))
		m = u.(Model)
	}
	// Forward: Branches -> Remotes -> Worktrees -> Branches (wrap).
	send("ctrl+right")
	if m.activeLeftTab != panelRemotes || m.focus != panelRemotes {
		t.Fatalf("1x ctrl+right: tab=%v focus=%v, want Remotes", m.activeLeftTab, m.focus)
	}
	send("ctrl+right")
	if m.activeLeftTab != panelWorktrees {
		t.Fatalf("2x ctrl+right: tab=%v, want Worktrees", m.activeLeftTab)
	}
	send("ctrl+right")
	if m.activeLeftTab != panelShelf {
		t.Fatalf("3x ctrl+right: tab=%v, want Shelf", m.activeLeftTab)
	}
	send("ctrl+right")
	if m.activeLeftTab != panelBranches {
		t.Fatalf("4x ctrl+right: tab=%v, want Branches (wrap)", m.activeLeftTab)
	}
	// Backward one step: Branches -> Shelf (wrap).
	send("ctrl+left")
	if m.activeLeftTab != panelShelf {
		t.Fatalf("ctrl+left from Branches: tab=%v, want Shelf", m.activeLeftTab)
	}
}

func TestLeftDoesNotFocusHiddenTab(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	m.lastLeftPanel = panelBranches
	// Switch the visible tab to Worktrees, then sit on Commits and go ←.
	m.activeLeftTab = panelWorktrees
	m.focus = panelCommits
	u, _ := m.Update(keyMsg("left"))
	got := u.(Model).focus
	if got == panelBranches {
		t.Fatalf("← focused the hidden Branches tab; want the active tab or Status, got %v", got)
	}
	if got != panelWorktrees && got != panelFiles {
		t.Fatalf("← focus = %v, want the active Worktrees tab or Status", got)
	}
}

func TestCheckoutRemoteRoutesCAndS(t *testing.T) {
	m := loadedModel(t)
	m.remoteBranches = []model.RemoteBranch{{Name: "origin/foo", Remote: "origin", Branch: "foo"}}
	m.focus = panelRemotes
	m.sel[panelRemotes] = 0

	// c on the Remotes tab starts an op (running=true) and does NOT open the commit popup.
	u, _ := m.Update(keyMsg("c"))
	mc := u.(Model)
	if mc.commitPopup != nil {
		t.Fatal("c on Remotes must not open the commit popup")
	}
	if !mc.running {
		t.Fatal("c on Remotes should start SmartCheckout (running)")
	}

	// s on the Remotes tab starts an op too.
	m2 := loadedModel(t)
	m2.remoteBranches = []model.RemoteBranch{{Name: "origin/foo", Remote: "origin", Branch: "foo"}}
	m2.focus = panelRemotes
	m2.sel[panelRemotes] = 0
	u2, _ := m2.Update(keyMsg("s"))
	if !u2.(Model).running {
		t.Fatal("s on Remotes should start SmartCheckout (running)")
	}
}

func TestCanCheckoutRemoteGating(t *testing.T) {
	m := loadedModel(t)
	if m.canCheckoutRemote() {
		t.Fatal("no remote selected -> false")
	}
	m.remoteBranches = []model.RemoteBranch{{Name: "origin/foo", Remote: "origin", Branch: "foo"}}
	m.sel[panelRemotes] = 0
	if !m.canCheckoutRemote() {
		t.Fatal("a remote selected + idle -> true")
	}
}

func TestFetchKeyOnRemotesTabStartsFetch(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelRemotes
	u, _ := m.Update(keyMsg("f"))
	if !u.(Model).running {
		t.Fatal("f on the Remotes tab should start Fetch")
	}
	m2 := loadedModel(t)
	m2.focus = panelBranches
	u2, _ := m2.Update(keyMsg("f"))
	if u2.(Model).running {
		t.Fatal("f on the Branches tab must not start Fetch")
	}
}
