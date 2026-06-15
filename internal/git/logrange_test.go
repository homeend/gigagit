package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLogRangeMessages(t *testing.T) {
	dir, runner := newTestRepo(t) // commit "initial" on main
	repo := &Repo{Runner: runner}
	git := func(args ...string) {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("checkout", "-b", "work")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	git("add", ".")
	git("commit", "-m", "first")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	git("add", ".")
	git("commit", "-m", "second line subject", "-m", "body para")

	cs, err := repo.LogRangeMessages(context.Background(), "main", "work")
	if err != nil {
		t.Fatalf("log range: %v", err)
	}
	if len(cs) != 2 {
		t.Fatalf("got %d commits, want 2", len(cs))
	}
	// oldest-first (git todo order)
	if cs[0].Subject != "first" {
		t.Fatalf("cs[0].Subject = %q, want first", cs[0].Subject)
	}
	if cs[1].Subject != "second line subject" {
		t.Fatalf("cs[1].Subject = %q", cs[1].Subject)
	}
	if cs[1].Message != "second line subject\n\nbody para\n" {
		t.Fatalf("cs[1].Message = %q", cs[1].Message)
	}
}
