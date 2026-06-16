package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestStageHunksStagesContentLeavesWorktree(t *testing.T) {
	dir, repo := newConflictRepo(t) // gives us a real repo; we ignore the conflict
	ctx := context.Background()
	// Start clean on a tracked file.
	run := func(args ...string) {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		_ = c.Run()
	}
	run("merge", "--abort")
	os.WriteFile(filepath.Join(dir, "uu.txt"), []byte("line1\nWORK\n"), 0o644)

	_, err := StageHunks{Path: "uu.txt", Content: []byte("line1\nSTAGED\n")}.
		Run(ctx, OpDeps{Repo: repo, Events: make(chan Event, 16)})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "uu.txt")); string(b) != "line1\nWORK\n" {
		t.Fatalf("working tree = %q, want WORK (untouched)", b)
	}
	out, _ := exec.Command("git", "-C", dir, "show", ":uu.txt").CombinedOutput()
	if string(out) != "line1\nSTAGED\n" {
		t.Fatalf("index = %q, want STAGED", out)
	}
}
