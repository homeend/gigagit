package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFastForwardAdvancesToDescendant(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t) // real repo on main with an initial commit
	ctx := context.Background()

	gitE(t, dir, "branch", "feat")
	gitE(t, dir, "checkout", "feat")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "ahead")
	featTip := gitOut(t, dir, "rev-parse", "HEAD")
	gitE(t, dir, "checkout", "main")

	res, err := FastForward{Commit: featTip}.Run(ctx, OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("FastForward: %v", err)
	}
	if !res.Changed {
		t.Fatal("Changed must be true on a real advance")
	}
	if got := gitOut(t, dir, "rev-parse", "HEAD"); got != featTip {
		t.Fatalf("HEAD = %s, want %s", got, featTip)
	}
}

func TestFastForwardRefusesNonDescendant(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()
	gitE(t, dir, "checkout", "--orphan", "other")
	os.WriteFile(filepath.Join(dir, "z.txt"), []byte("z\n"), 0o644)
	gitE(t, dir, "add", "z.txt")
	gitE(t, dir, "commit", "-m", "orphan")
	other := gitOut(t, dir, "rev-parse", "HEAD")
	gitE(t, dir, "checkout", "main")
	before := gitOut(t, dir, "rev-parse", "HEAD")

	if _, err := (FastForward{Commit: other}).Run(ctx, OpDeps{Repo: repo}); err == nil {
		t.Fatal("non-descendant fast-forward must error")
	}
	if gitOut(t, dir, "rev-parse", "HEAD") != before {
		t.Fatal("HEAD must not move on a refused fast-forward")
	}
}

func TestFastForwardAlreadyUpToDate(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()
	head := gitOut(t, dir, "rev-parse", "HEAD")
	res, err := FastForward{Commit: head}.Run(ctx, OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("up-to-date FF: %v", err)
	}
	if res.Changed {
		t.Fatal("Changed must be false when already at the target")
	}
}

func TestFastForwardDetachedHeadErrors(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()
	head := gitOut(t, dir, "rev-parse", "HEAD")
	gitE(t, dir, "checkout", "--detach", head)
	if _, err := (FastForward{Commit: head}).Run(ctx, OpDeps{Repo: repo}); err == nil {
		t.Fatal("detached HEAD must error")
	}
}

func TestFastForwardBranchAdvancesNonCurrent(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()

	gitE(t, dir, "branch", "old") // old stays at the initial commit
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "main ahead")
	mainTip := gitOut(t, dir, "rev-parse", "HEAD")

	res, err := FastForward{Branch: "old", Commit: "main"}.Run(ctx, OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("FastForward branch: %v", err)
	}
	if !res.Changed {
		t.Fatal("Changed must be true on a real advance")
	}
	if got := gitOut(t, dir, "rev-parse", "old"); got != mainTip {
		t.Fatalf("old = %s, want %s", got, mainTip)
	}
	if got := gitOut(t, dir, "rev-parse", "HEAD"); got != mainTip {
		t.Fatal("HEAD (main) must not move when advancing another branch")
	}
}

func TestFastForwardBranchCurrentUpdatesWorktree(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()

	gitE(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("c\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "feat ahead")
	featTip := gitOut(t, dir, "rev-parse", "HEAD")
	gitE(t, dir, "checkout", "main")

	res, err := FastForward{Branch: "main", Commit: "feat"}.Run(ctx, OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("FastForward current branch: %v", err)
	}
	if !res.Changed {
		t.Fatal("Changed must be true on a real advance")
	}
	if got := gitOut(t, dir, "rev-parse", "HEAD"); got != featTip {
		t.Fatalf("HEAD = %s, want %s", got, featTip)
	}
	if _, err := os.Stat(filepath.Join(dir, "c.txt")); err != nil {
		t.Fatal("working tree must be updated when fast-forwarding the current branch")
	}
}

func TestFastForwardBranchRefusesNonAncestor(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()

	gitE(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "d.txt"), []byte("d\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "feat ahead")
	gitE(t, dir, "checkout", "main")
	os.WriteFile(filepath.Join(dir, "e.txt"), []byte("e\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "main diverged")
	featBefore := gitOut(t, dir, "rev-parse", "feat")

	if _, err := (FastForward{Branch: "feat", Commit: "main"}).Run(ctx, OpDeps{Repo: repo}); err == nil {
		t.Fatal("diverged fast-forward must error")
	}
	if gitOut(t, dir, "rev-parse", "feat") != featBefore {
		t.Fatal("feat must not move on a refused fast-forward")
	}
}

func TestFastForwardBranchAlreadyUpToDate(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()
	gitE(t, dir, "branch", "twin") // same tip as main
	res, err := FastForward{Branch: "twin", Commit: "main"}.Run(ctx, OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("up-to-date FF: %v", err)
	}
	if res.Changed {
		t.Fatal("Changed must be false when already at the target")
	}
}

func TestFastForwardBranchCheckedOutElsewhereErrors(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()

	gitE(t, dir, "branch", "wt") // at the initial commit
	gitE(t, dir, "worktree", "add", filepath.Join(dir, ".wt"), "wt")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("f\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "main ahead")
	wtBefore := gitOut(t, dir, "rev-parse", "wt")

	if _, err := (FastForward{Branch: "wt", Commit: "main"}).Run(ctx, OpDeps{Repo: repo}); err == nil {
		t.Fatal("fast-forwarding a branch checked out in another worktree must error")
	}
	if gitOut(t, dir, "rev-parse", "wt") != wtBefore {
		t.Fatal("wt must not move on a refused fast-forward")
	}
}
