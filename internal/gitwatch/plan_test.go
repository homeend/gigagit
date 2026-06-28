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

func TestPlanEmptyEnabledYieldsNoGroups(t *testing.T) {
	c := filepath.Join("/repo", ".git")
	groups := Plan(c, c, []Source{})
	if len(groups) != 0 {
		t.Errorf("empty enabled slice must yield no groups, got %v", dirsOf(groups))
	}
}

func TestPlanBranchesWatchesRefsHeadsRecursive(t *testing.T) {
	c := filepath.Join("/repo", ".git")
	groups := Plan(c, c, []Source{Branches})
	g := groupFor(t, groups, filepath.Join(c, "refs", "heads"))
	if !g.Recursive {
		t.Error("refs/heads group must be recursive")
	}
	if !hasSource(g.Match("main"), Branches) {
		t.Error("a ref under refs/heads should affect Branches")
	}
}

func TestPlanPackedRefsAffectsBothWhenEnabled(t *testing.T) {
	c := filepath.Join("/repo", ".git")
	groups := Plan(c, c, []Source{Branches, Remotes})
	g := groupFor(t, groups, c) // the commonDir shared group
	ss := g.Match("packed-refs")
	if !hasSource(ss, Branches) || !hasSource(ss, Remotes) {
		t.Errorf("packed-refs should affect both Branches and Remotes, got %v", ss)
	}
	if cfg := g.Match("config"); !hasSource(cfg, Remotes) || hasSource(cfg, Branches) {
		t.Errorf("config should affect Remotes only, got %v", cfg)
	}
}

func TestPlanPackedRefsOnlyEnabledSource(t *testing.T) {
	c := filepath.Join("/repo", ".git")
	groups := Plan(c, c, []Source{Branches}) // remotes NOT enabled
	g := groupFor(t, groups, c)
	if ss := g.Match("packed-refs"); hasSource(ss, Remotes) {
		t.Errorf("packed-refs must not emit Remotes when remotes disabled, got %v", ss)
	}
}

func TestPlanBranchesWatchesHEADOnWorktreeDir(t *testing.T) {
	c := filepath.Join("/repo", ".git")
	w := filepath.Join(c, "worktrees", "wt1")
	groups := Plan(c, w, []Source{Branches})
	g := groupFor(t, groups, w)
	if !hasSource(g.Match("HEAD"), Branches) {
		t.Error("HEAD change should affect Branches (current-branch line)")
	}
}
