package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// alt+w closes every window (user ruling 2026-10-14). An open pull request
// (its files view replaces the left column): the first press CLOSES it and
// shows the Branches tab of the worktree on screen. The next press moves on.
func TestAltWClosesAnOpenPR(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.width, m.height = 120, 40
	m.activeLeftTab, m.focus = panelPRs, panelCommits
	m = pressAlt(t, m, 'w')
	if m.filesView != nil || m.previewOpen != nil {
		t.Fatalf("alt+w: filesView=%v previewOpen=%+v status=%q, want the PR closed", m.filesView != nil, m.previewOpen, m.statusMsg)
	}
	if m.activeLeftTab != panelBranches || m.focus != panelBranches {
		t.Fatalf("alt+w: tab=%v focus=%v, want Branches", m.activeLeftTab, m.focus)
	}
}

// With more worktrees the closing press stays on this worktree (a first
// hit, like a shown console's): the PR is closed, not parked with it, and
// the next press moves to the next worktree.
func TestAltWClosesAnOpenPRBeforeMovingOn(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.width, m.height = 120, 40
	m, _ = addWorktree(t, m, "wt2")
	home := m.viewed
	m = pressAlt(t, m, 'w')
	if m.filesView != nil || m.previewOpen != nil || m.viewed != home {
		t.Fatalf("first press: filesView=%v viewed=%q (home %q) status=%q, want the PR closed, still here", m.filesView != nil, m.viewed, home, m.statusMsg)
	}
	if m.activeLeftTab != panelBranches || m.focus != panelBranches {
		t.Fatalf("first press: tab=%v focus=%v, want Branches", m.activeLeftTab, m.focus)
	}
	m = pressAlt(t, m, 'w')
	if m.viewed == home {
		t.Fatalf("second press: viewed = %q, want the next worktree (status %q)", m.viewed, m.statusMsg)
	}
	m = landView(t, m)
	m = pressAlt(t, m, 'w')
	if m.viewed != home || m.filesView != nil {
		t.Fatalf("back home: viewed=%q filesView=%v, want home without the PR (it was closed, not parked)", m.viewed, m.filesView != nil)
	}
}

// A PR's review (the review view of a "<base>...refs/gg/pr/<n>" scope)
// closes the same way.
func TestAltWClosesAPRReview(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.width, m.height = 120, 40
	m.previewOpen = nil // the review view replaced the PR's diff
	m.filesReview = &reviewViewState{id: "r1", review: domain.Review{ID: "r1", Preview: "main...refs/gg/pr/7"}}
	m = pressAlt(t, m, 'w')
	if m.filesView != nil || m.filesReview != nil {
		t.Fatalf("alt+w: filesView=%v review=%v status=%q, want the PR review closed", m.filesView != nil, m.filesReview != nil, m.statusMsg)
	}
	if m.activeLeftTab != panelBranches || m.focus != panelBranches {
		t.Fatalf("alt+w: tab=%v focus=%v, want Branches", m.activeLeftTab, m.focus)
	}
}

// From inside a PR file's diff the diff goes with the PR.
func TestAltWFromAPRFileDiffClosesBoth(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.width, m.height = 120, 40
	m = m.pushLayer(&diffView{})
	m = pressAlt(t, m, 'w')
	if m.filesView != nil || m.topLayer() != nil {
		t.Fatalf("alt+w: filesView=%v top=%T, want the PR and its diff closed", m.filesView != nil, m.topLayer())
	}
	if m.activeLeftTab != panelBranches || m.focus != panelBranches {
		t.Fatalf("alt+w: tab=%v focus=%v, want Branches", m.activeLeftTab, m.focus)
	}
}

// Under a shown console the first press hides it AND closes the PR beside
// it: Branches is what is on screen.
func TestAltWUnderAConsoleClosesThePRToo(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.width, m.height = 160, 40
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.showConsole(s.Info().ID, false)
	if m.console == nil {
		t.Fatalf("precondition: no console shown")
	}
	m = pressAlt(t, m, 'w')
	if m.console != nil || m.filesView != nil || m.previewOpen != nil {
		t.Fatalf("alt+w: console=%v filesView=%v status=%q, want both gone", m.console != nil, m.filesView != nil, m.statusMsg)
	}
	if m.activeLeftTab != panelBranches || m.focus != panelBranches {
		t.Fatalf("alt+w: tab=%v focus=%v, want Branches", m.activeLeftTab, m.focus)
	}
}

// Any other files view closes as well: every window does.
func TestAltWClosesACommitFilesView(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.previewOpen = nil
	m.width, m.height = 120, 40
	m = pressAlt(t, m, 'w')
	if m.filesView != nil || m.focus != panelBranches {
		t.Fatalf("alt+w: filesView=%v focus=%v, want it closed, Branches", m.filesView != nil, m.focus)
	}
}

// A viewer over the PR goes too (its document to the background).
func TestAltWClosesAViewerAndThePRBeneath(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.width, m.height = 120, 40
	m = m.pushLayer(&fileViewer{})
	m = pressAlt(t, m, 'w')
	if m.topLayer() != nil || m.filesView != nil || m.focus != panelBranches {
		t.Fatalf("alt+w: top=%T filesView=%v focus=%v, want everything closed, Branches", m.topLayer(), m.filesView != nil, m.focus)
	}
}

// A popup on screen: alt+w is ignored — nothing closes, nothing moves.
func TestAltWIgnoredUnderAPopup(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.width, m.height = 120, 40
	m, _ = addWorktree(t, m, "wt2")
	viewed := m.viewed
	pop := newContentPopup("x", nil)
	m = m.pushLayer(pop)
	m = pressAlt(t, m, 'w')
	if m.topLayer() != layer(pop) || m.filesView == nil || m.viewed != viewed {
		t.Fatalf("alt+w under a popup: top=%T filesView=%v viewed=%q, want nothing changed", m.topLayer(), m.filesView != nil, m.viewed)
	}
}

// A popup on screen: alt+a / alt+t are ignored too — no console shows.
func TestAltAIgnoredUnderAPopup(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.width, m.height = 120, 40
	startTestSession(t, m, `sleep 5`)
	m, ok := m.openBranchPopup(false) // a popup the walk used to park
	if !ok {
		t.Fatal("precondition: the branch popup did not open")
	}
	pop := m.topLayer()
	for _, r := range []rune{'a', 'A', 't', 'T'} {
		m = pressAlt(t, m, r)
		if m.console != nil || m.topLayer() != pop {
			t.Fatalf("alt+%c under a popup: console=%v top=%T, want ignored", r, m.console != nil, m.topLayer())
		}
	}
}

// Branches focused but its cursor on another branch: the first press only
// puts the cursor on the viewed worktree's branch; the next one moves.
func TestAltWFirstSelectsTheViewedWorktree(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, _ = addWorktree(t, m, "wt2")
	m = m.activateTab(panelBranches)
	home := m.viewed
	want, ok := m.worktreeBranchRow(m.viewPath(home))
	if !ok {
		t.Fatalf("no branch row for home")
	}
	other := 0
	if want == 0 {
		other = 1
	}
	m.sel[panelBranches] = other
	m = pressAlt(t, m, 'w')
	if m.viewed != home || m.sel[panelBranches] != want {
		t.Fatalf("first press: viewed=%q sel=%d, want home with sel %d (status %q)", m.viewed, m.sel[panelBranches], want, m.statusMsg)
	}
	m = pressAlt(t, m, 'w')
	if m.viewed == home {
		t.Fatalf("second press stayed home (status %q)", m.statusMsg)
	}
}

// The arriving worktree's parked windows close as well: alt+w shows its
// Branches row.
func TestAltWClosesTheArrivingWorktreesWindows(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	m, ok := m.switchView(other)
	if !ok {
		t.Fatalf("switch to wt2: %q", m.statusMsg)
	}
	m = landView(t, m)
	m = m.pushLayer(&diffView{})
	home := m.home
	m, ok = m.switchView(m.viewPath(home))
	if !ok {
		t.Fatalf("switch home: %q", m.statusMsg)
	}
	m = landView(t, m)
	m = m.activateTab(panelBranches).selectWorktreeBranch(m.viewPath(home))
	m = pressAlt(t, m, 'w')
	if m.viewed == home {
		t.Fatalf("alt+w stayed home (status %q)", m.statusMsg)
	}
	if m.topLayer() != nil || m.focus != panelBranches {
		t.Fatalf("arrived: top=%T focus=%v, want wt2's diff closed, Branches", m.topLayer(), m.focus)
	}
}

// A popup under a window: the windows over it close, the popup is then on
// screen and keeps the keyboard — the next alt+w is ignored.
func TestAltWStopsAtAPopupUnderAWindow(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, _ = addWorktree(t, m, "wt2")
	m, ok := m.openBranchPopup(false)
	if !ok {
		t.Fatal("precondition: the branch popup did not open")
	}
	pop := m.topLayer()
	m = m.pushLayer(&diffView{title: "a.go", rev: "abc123"})
	viewed := m.viewed
	m = pressAlt(t, m, 'w')
	if m.topLayer() != pop || m.viewed != viewed {
		t.Fatalf("first press: top=%T viewed=%q, want the diff closed and the popup on screen", m.topLayer(), m.viewed)
	}
	m = pressAlt(t, m, 'w')
	if m.topLayer() != pop || m.viewed != viewed {
		t.Fatalf("second press: top=%T viewed=%q, want ignored under the popup", m.topLayer(), m.viewed)
	}
}

// The stash list is a window: alt+w closes it.
func TestAltWClosesTheStashList(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m.stashView, m.focus = &stashView{tag: "stash"}, panelCommits
	m = pressAlt(t, m, 'w')
	if m.stashView != nil || m.focus != panelBranches {
		t.Fatalf("alt+w: stash=%v focus=%v, want it closed, Branches", m.stashView != nil, m.focus)
	}
}

// The interactive-rebase and hunk editors hold an operation's input: alt+w
// is ignored over them.
func TestAltWIgnoredOverTheEditors(t *testing.T) {
	t.Parallel()
	for _, ed := range []layer{&irebaseEditor{}, &hunkPicker{}} {
		m := loadedModel(t)
		m.width, m.height = 120, 40
		m.focus = panelFiles
		m = m.pushLayer(ed)
		m = pressAlt(t, m, 'w')
		if m.topLayer() != ed || m.focus != panelFiles {
			t.Fatalf("%T: top=%T focus=%v, want ignored", ed, m.topLayer(), m.focus)
		}
	}
}
