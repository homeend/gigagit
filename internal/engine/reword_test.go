package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRewordHeadAmend(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t) // main: "initial"
	if err := os.WriteFile(filepath.Join(dir, "top.txt"), []byte("top\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "top")
	head, _ := repo.RevParse(context.Background(), "HEAD")

	res, err := Reword{Commit: head, NewMsg: "top reworded", GGBin: buildGG(t)}.
		Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("reword HEAD: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if msg, _ := repo.CommitMessage(context.Background(), "HEAD"); !strings.Contains(msg, "top reworded") {
		t.Fatalf("HEAD message not changed: %q", msg)
	}
}

func TestRewordMidBranchPreservesLater(t *testing.T) {
	t.Parallel()
	dir, repo := threeCommitBranch(t)                       // work: wip1 -> wip2 -> wip3
	mid, _ := repo.RevParse(context.Background(), "work~1") // wip2

	_, err := Reword{Commit: mid, NewMsg: "wip2 reworded", GGBin: buildGG(t)}.
		Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("reword mid: %v", err)
	}
	got := subjects(t, dir, "main..work") // newest-first
	want := []string{"wip3", "wip2 reworded", "wip1"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("subjects = %v, want %v", got, want)
	}
}

func TestRewordNonHeadRootRefused(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t) // main: "initial" (the root)
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c2")
	root := gitOut(t, dir, "rev-list", "--max-parents=0", "HEAD")
	root = strings.TrimSpace(root)

	if _, err := (Reword{Commit: root, NewMsg: "x", GGBin: buildGG(t)}).
		Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("want refusal rewording a non-HEAD root commit")
	}
}

func TestRewordOffBranchRefused(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t) // on main
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c2")
	gitIn(t, dir, "branch", "side")
	gitIn(t, dir, "checkout", "side")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "side-only")
	side, _ := repo.RevParse(context.Background(), "HEAD")
	gitIn(t, dir, "checkout", "main")

	if _, err := (Reword{Commit: side, NewMsg: "x", GGBin: buildGG(t)}).
		Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("want refusal rewording a commit not on the current branch")
	}
}
