package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// alt+w over an open pull request (its files view replaces the left
// column): the first press CLOSES the PR and shows the Branches tab of the
// worktree on screen — the tab it activates is no longer hidden behind the
// PR's tree. The next press moves on.
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

// Any other files view (a commit's files) is not a PR: alt+w keeps today's
// behaviour for it.
func TestAltWLeavesACommitFilesViewAlone(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.previewOpen = nil
	m.width, m.height = 120, 40
	m = pressAlt(t, m, 'w')
	if m.filesView == nil {
		t.Fatalf("alt+w closed a non-PR files view")
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

// An open document (the viewer) over the PR is closed only by its own keys:
// alt+w keeps its ordinary walk there.
func TestAltWLeavesADocumentOverAPROpen(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.width, m.height = 120, 40
	m = m.pushLayer(&fileViewer{})
	if m.prViewShown() {
		t.Fatalf("a viewer over the PR counts as the PR alone")
	}
}
