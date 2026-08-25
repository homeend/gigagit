package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageStagesFile(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)

	res, err := Stage{Paths: []string{"new.txt"}}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "staged new.txt") {
		t.Fatalf("result = %+v", res)
	}
	// new.txt is now in the index (added).
	if out := gitOut(t, dir, "diff", "--cached", "--name-only"); !strings.Contains(out, "new.txt") {
		t.Fatalf("new.txt not staged; cached names = %q", out)
	}
}

func TestStageUnstagesFile(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)
	gitE(t, dir, "add", "new.txt")

	res, err := Stage{Paths: []string{"new.txt"}, Unstage: true}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("unstage: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "unstaged new.txt") {
		t.Fatalf("result = %+v", res)
	}
	if out := gitOut(t, dir, "diff", "--cached", "--name-only"); strings.Contains(out, "new.txt") {
		t.Fatalf("new.txt still staged; cached names = %q", out)
	}
}

func TestStageAllStagesUntracked(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)

	res, err := Stage{All: true}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("stage all: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "staged all") {
		t.Fatalf("result = %+v", res)
	}
	if out := gitOut(t, dir, "diff", "--cached", "--name-only"); !strings.Contains(out, "new.txt") {
		t.Fatalf("new.txt not staged; cached names = %q", out)
	}
}

func TestStageAllRejectsPaths(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	if _, err := (Stage{All: true, Paths: []string{"x"}}).Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("want error for All+Paths")
	}
}

func TestStageAllRejectsUnstage(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	if _, err := (Stage{All: true, Unstage: true}).Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("want error for All+Unstage")
	}
}
