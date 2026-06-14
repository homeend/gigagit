package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageStagesFile(t *testing.T) {
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
