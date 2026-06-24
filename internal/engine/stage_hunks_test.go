package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/hunkpick"
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

// TestStageHunksPartialRoundTrip exercises the real glue: diff index vs working
// tree with hunkpick.FromDiff, stage ONE of two changed hunks, and confirm the
// index holds exactly that hunk while the other stays unstaged and the working
// tree is untouched.
func TestStageHunksPartialRoundTrip(t *testing.T) {
	dir, repo := newConflictRepo(t)
	ctx := context.Background()
	run := func(args ...string) {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		_ = c.Run()
	}
	run("merge", "--abort")
	// Commit a 3-line baseline, then change line 1 and line 3 in the working tree.
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\ntwo\nthree\n"), 0o644)
	run("add", "f.txt")
	run("commit", "-qm", "base3")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("ONE\ntwo\nTHREE\n"), 0o644)

	index, err := repo.ShowFile(ctx, "", "f.txt") // index == HEAD == "one\ntwo\nthree\n"
	if err != nil {
		t.Fatal(err)
	}
	work, err := repo.ReadWorktreeFile(ctx, "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	doc := hunkpick.FromDiff(index, work)
	doc.SetAll(hunkpick.TakeCurrent) // default: nothing staged
	bs := doc.Blocks()
	if len(bs) != 2 {
		t.Fatalf("got %d hunks, want 2 (line1 + line3)", len(bs))
	}
	// Stage only the first hunk (ONE), leave the third (THREE) unstaged.
	bs[0].Mode = hunkpick.TakeIncoming
	out, ok := doc.Resolved()
	if !ok {
		t.Fatal("Resolved ok=false")
	}
	if string(out) != "ONE\ntwo\nthree\n" {
		t.Fatalf("assembled index = %q, want only the first hunk staged", out)
	}

	if _, err := (StageHunks{Path: "f.txt", Content: out}).
		Run(ctx, OpDeps{Repo: repo, Events: make(chan Event, 16)}); err != nil {
		t.Fatal(err)
	}
	// Working tree still has both changes.
	if b, _ := os.ReadFile(filepath.Join(dir, "f.txt")); string(b) != "ONE\ntwo\nTHREE\n" {
		t.Fatalf("working tree = %q, want both changes (untouched)", b)
	}
	// Index has only the first change staged.
	idx, _ := exec.Command("git", "-C", dir, "show", ":f.txt").CombinedOutput()
	if string(idx) != "ONE\ntwo\nthree\n" {
		t.Fatalf("index = %q, want only the first hunk", idx)
	}
}
