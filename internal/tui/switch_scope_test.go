package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// newTwoBranchRepo builds a repo where branch "side" has a commit main lacks,
// so a feed scoped to main visibly differs from the all-branches walk.
func newTwoBranchRepo(t *testing.T) (string, Model) {
	t.Helper()
	dir, repo := newRepoDir(t)
	runGit(t, dir, "switch", "-c", "side")
	os.WriteFile(filepath.Join(dir, "side.txt"), []byte("s\n"), 0o644)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "side work")
	runGit(t, dir, "switch", "main")
	m := New(domain.New(repo))
	updated, _ := m.Update(m.loadCmd()())
	return dir, updated.(Model)
}

// soloMain scopes m's feed to main (the ctrl+g state) and returns the model
// with the scoped walk applied.
func soloMain(t *testing.T, m Model) Model {
	t.Helper()
	m.commitScopeBranches = []string{"main"}
	m2, cmd := m.startFeedReload()
	updated, _ := m2.Update(cmd())
	m = updated.(Model)
	for _, c := range m.commits {
		if c.Subject == "side work" {
			t.Fatal("solo scope not applied: side commit still in the feed")
		}
	}
	return m
}

// TestSwitchBranchClearsCommitScope: a successful branch switch (SmartSwitch,
// same worktree — no reRoot involved) clears the solo/multi scope and the
// commit filter, and re-points the FEED's own scope so the post-op srcFeed
// reload walks all branches instead of the stale solo.
func TestSwitchBranchClearsCommitScope(t *testing.T) {
	t.Parallel()
	_, m := newTwoBranchRepo(t)
	m = soloMain(t, m)
	m.commitFilter = commitFilterFields{Author: "someone"}

	m2, cmd := m.startOp(engine.SmartSwitch{Branch: "side"})
	m = driveOp(t, m2, cmd)

	if len(m.commitScopeBranches) != 0 {
		t.Errorf("switch kept commitScopeBranches = %v, want empty", m.commitScopeBranches)
	}
	if m.commitFilter.filtered() {
		t.Errorf("switch kept commitFilter = %+v, want zero", m.commitFilter)
	}
	if m.feedScopeApplied != m.feedScopeSig() {
		t.Errorf("feedScopeApplied = %q not synced to cleared scope sig %q", m.feedScopeApplied, m.feedScopeSig())
	}
	// The feed itself must carry the cleared scope: its next hard refresh (what
	// the post-op srcFeed reload runs) walks all branches again.
	st, err := m.feed.LoadInitial(context.Background())
	if err != nil {
		t.Fatalf("LoadInitial: %v", err)
	}
	found := false
	for _, c := range st.Commits {
		if c.Subject == "side work" {
			found = true
		}
	}
	if !found {
		t.Error("feed still scoped to main after the switch: side commit missing")
	}
}

// TestFailedSwitchKeepsCommitScope: a switch that errors (target branch does
// not exist) must leave the solo scope alone — HEAD never moved.
func TestFailedSwitchKeepsCommitScope(t *testing.T) {
	t.Parallel()
	_, m := newTwoBranchRepo(t)
	m = soloMain(t, m)

	m2, cmd := m.startOp(engine.SmartSwitch{Branch: "no-such-branch"})
	m = driveOp(t, m2, cmd)

	if len(m.commitScopeBranches) != 1 || m.commitScopeBranches[0] != "main" {
		t.Errorf("failed switch changed commitScopeBranches = %v, want [main]", m.commitScopeBranches)
	}
}

// TestNoopSwitchKeepsCommitScope: switching to the branch already checked out
// reports Changed=false and must not clear the scope.
func TestNoopSwitchKeepsCommitScope(t *testing.T) {
	t.Parallel()
	_, m := newTwoBranchRepo(t)
	m = soloMain(t, m)

	m2, cmd := m.startOp(engine.SmartSwitch{Branch: "main"})
	m = driveOp(t, m2, cmd)

	if len(m.commitScopeBranches) != 1 || m.commitScopeBranches[0] != "main" {
		t.Errorf("no-op switch changed commitScopeBranches = %v, want [main]", m.commitScopeBranches)
	}
}

// TestNonSwitchOpKeepsCommitScope: an unrelated successful op (Commit) must
// not clear the solo scope — only checkout-family ops do.
func TestNonSwitchOpKeepsCommitScope(t *testing.T) {
	t.Parallel()
	dir, m := newTwoBranchRepo(t)
	m = soloMain(t, m)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("n\n"), 0o644)
	runGit(t, dir, "add", ".")

	m2, cmd := m.startOp(engine.Commit{Message: "unrelated"})
	m = driveOp(t, m2, cmd)

	if len(m.commitScopeBranches) != 1 || m.commitScopeBranches[0] != "main" {
		t.Errorf("commit op changed commitScopeBranches = %v, want [main]", m.commitScopeBranches)
	}
}
