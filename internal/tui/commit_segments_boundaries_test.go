package tui

import (
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// The diverged-base bug: main moved on past the fork, so no decoration exists
// inside the soloed walk — the merge-base hash set must supply the boundary.
func TestSegBoundaryPredicateIncludesHashSet(t *testing.T) {
	t.Parallel()
	m := Model{
		commitScopeBranches: []string{"feat/a"},
		segBoundaryHashes:   map[string]bool{"forkforkforkforkforkforkforkforkforkfork": true},
	}
	boundary := m.segBoundary()
	if !boundary(segCommit("forkforkforkforkforkforkforkforkforkfork")) {
		t.Fatal("a merge-base hash must be a boundary even with no decoration")
	}
	if boundary(segCommit("otherotherotherotherotherotherotherother")) {
		t.Fatal("a non-boundary undecorated commit must not be a boundary")
	}
}

// divergedSoloModel: feat/x soloed over an UNDECORATED linear history (main's
// tip moved past the fork, so no ref marks bbbbbbb) — segments start uniform.
func divergedSoloModel() Model {
	m := footerModel()
	m.focus = panelCommits
	m.commitScopeBranches = []string{"feat/x"}
	m.commits = []model.Commit{
		segCommit("aaaaaaa", "bbbbbbb"),
		segCommit("bbbbbbb", "ccccccc"),
		segCommit("ccccccc"),
	}
	m = m.rebuildCommitGraph()
	m.commitListMode = true
	return m
}

func TestScopeBoundariesMsgRecolors(t *testing.T) {
	m := divergedSoloModel()
	if want := []int{0, 0, 0}; !slices.Equal(m.commitSegs, want) {
		t.Fatalf("pre-msg segments = %v, want uniform %v", m.commitSegs, want)
	}
	mm, _ := m.Update(scopeBoundariesMsg{
		sig:    m.feedScopeSig(),
		hashes: map[string]bool{"bbbbbbb": true},
	})
	m = mm.(Model)
	if want := []int{0, 1, 1}; !slices.Equal(m.commitSegs, want) {
		t.Fatalf("post-msg segments = %v, want %v", m.commitSegs, want)
	}
}

func TestScopeBoundariesMsgStaleSigDropped(t *testing.T) {
	m := divergedSoloModel()
	mm, _ := m.Update(scopeBoundariesMsg{
		sig:    "someone-elses-scope",
		hashes: map[string]bool{"bbbbbbb": true},
	})
	m = mm.(Model)
	if len(m.segBoundaryHashes) != 0 {
		t.Fatalf("stale-sig hashes must be dropped, got %v", m.segBoundaryHashes)
	}
	if want := []int{0, 0, 0}; !slices.Equal(m.commitSegs, want) {
		t.Fatalf("stale msg must not recolor: %v", m.commitSegs)
	}
}

func TestStartFeedReloadClearsBoundaryHashes(t *testing.T) {
	m := divergedSoloModel()
	m.segBoundaryHashes = map[string]bool{"bbbbbbb": true}
	m, _ = m.startFeedReload()
	if len(m.segBoundaryHashes) != 0 {
		t.Fatal("a scope change must drop the previous scope's fork points")
	}
}

func TestLoadScopeBoundariesCmdGating(t *testing.T) {
	m := divergedSoloModel()
	if m.loadScopeBoundariesCmd() == nil {
		t.Fatal("scoped model with other branches should produce a boundaries cmd")
	}
	m.commitScopeBranches = nil
	if m.loadScopeBoundariesCmd() != nil {
		t.Fatal("unscoped model must not query boundaries")
	}
	m.commitScopeBranches = []string{"feat/x"}
	m.branches = []model.Branch{{Name: "feat/x"}} // no OTHER branches
	if m.loadScopeBoundariesCmd() != nil {
		t.Fatal("no other branches → nothing to merge-base against")
	}
}
