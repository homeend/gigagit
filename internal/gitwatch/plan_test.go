package gitwatch

import (
	"path/filepath"
	"testing"
)

// groupFor returns the planned group whose Dir == want, or fails.
func groupFor(t *testing.T, groups []Group, want string) Group {
	t.Helper()
	for _, g := range groups {
		if g.Dir == want {
			return g
		}
	}
	t.Fatalf("no group for dir %q in %v", want, dirsOf(groups))
	return Group{}
}

func dirsOf(groups []Group) []string {
	out := make([]string, len(groups))
	for i, g := range groups {
		out[i] = g.Dir
	}
	return out
}

func hasSource(ss []Source, want Source) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestPlanReflogWatchesLogsHEAD(t *testing.T) {
	c := filepath.Join("/repo", ".git")
	w := filepath.Join("/repo", ".git", "worktrees", "wt1")
	groups := Plan(c, w, []Source{Reflog})
	g := groupFor(t, groups, filepath.Join(w, "logs"))
	if !hasSource(g.Match("HEAD"), Reflog) {
		t.Error("logs/HEAD change should affect Reflog")
	}
	if g.Match("ORIG_HEAD") != nil {
		t.Error("logs/ORIG_HEAD must not affect Reflog")
	}
	if g.Recursive {
		t.Error("reflog group must be non-recursive")
	}
}

func TestPlanWorktreesWatchesWorktreesDir(t *testing.T) {
	c := filepath.Join("/repo", ".git")
	groups := Plan(c, c, []Source{Worktrees})
	g := groupFor(t, groups, filepath.Join(c, "worktrees"))
	if !hasSource(g.Match("anything"), Worktrees) {
		t.Error("any change under worktrees/ should affect Worktrees")
	}
}

func TestPlanIgnoresUnimplementedSourcesInD1(t *testing.T) {
	c := filepath.Join("/repo", ".git")
	groups := Plan(c, c, []Source{Branches, Remotes})
	if len(groups) != 0 {
		t.Errorf("D1 Plan must ignore Branches/Remotes, got %v", dirsOf(groups))
	}
}
