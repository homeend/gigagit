package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/engine"
)

// markModel() has branches: main (IsHead), feat/a, feat/b; focus = panelBranches.

func TestPullForFocusBranchesPanel(t *testing.T) {
	m := markModel()
	m.focus = panelBranches

	// current branch (main, row 0) → plain pull-and-stay, no target.
	m.sel[panelBranches] = 0
	if op := m.pullForFocus(); op.Branch != "" || op.Intent != engine.PullAndStay {
		t.Fatalf("on current branch: got %+v, want {Branch:\"\", PullAndStay}", op)
	}

	// non-current branch (feat/a, row 1) → background pull of that branch.
	m.sel[panelBranches] = 1
	op := m.pullForFocus()
	if op.Branch != "feat/a" || op.Intent != engine.PullInBackground {
		t.Fatalf("on feat/a: got %+v, want {Branch:feat/a, PullInBackground}", op)
	}
}

func TestPullForFocusOtherPanelPullsCurrent(t *testing.T) {
	m := markModel()
	m.focus = panelCommits
	if op := m.pullForFocus(); op.Branch != "" || op.Intent != engine.PullAndStay {
		t.Fatalf("off the Branches panel: got %+v, want pull-current", op)
	}
}

func TestBackgroundPullRowGating(t *testing.T) {
	m := markModel()
	m.focus = panelBranches

	// current branch → no row (use plain pull).
	m.sel[panelBranches] = 0
	if _, ok := m.backgroundPullRow(); ok {
		t.Fatal("background-pull row must be absent on the current branch")
	}

	// non-current branch → row present, labeled with the branch.
	m.sel[panelBranches] = 1
	r, ok := m.backgroundPullRow()
	if !ok {
		t.Fatal("background-pull row must be present on a non-current branch")
	}
	if r.run == nil {
		t.Fatal("background-pull row must have a run handler")
	}
	if want := "Pull feat/a (stay here)"; r.label != want {
		t.Fatalf("label = %q, want %q", r.label, want)
	}

	// off the Branches panel → absent.
	m.focus = panelCommits
	if _, ok := m.backgroundPullRow(); ok {
		t.Fatal("background-pull row must be Branches-panel only")
	}
}
