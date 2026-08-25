package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCommitExists(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	// Add a second commit so HEAD~1 resolves.
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.txt")
	run("commit", "-m", "second")

	for _, ref := range []string{"HEAD", "HEAD~1", "main"} {
		ok, err := repo.CommitExists(ctx, ref)
		if err != nil {
			t.Fatalf("CommitExists(%q): %v", ref, err)
		}
		if !ok {
			t.Errorf("CommitExists(%q) = false, want true", ref)
		}
	}

	for _, ref := range []string{"deadbeef", "nope", "HEAD~9"} {
		ok, err := repo.CommitExists(ctx, ref)
		if err != nil {
			t.Fatalf("CommitExists(%q): unexpected err %v", ref, err)
		}
		if ok {
			t.Errorf("CommitExists(%q) = true, want false", ref)
		}
	}
}
