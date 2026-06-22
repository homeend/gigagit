package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFastForwardAdvancesToDescendant(t *testing.T) {
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
	dir, repo := newRepo(t)
	ctx := context.Background()
	head := gitOut(t, dir, "rev-parse", "HEAD")
	gitE(t, dir, "checkout", "--detach", head)
	if _, err := (FastForward{Commit: head}).Run(ctx, OpDeps{Repo: repo}); err == nil {
		t.Fatal("detached HEAD must error")
	}
}
