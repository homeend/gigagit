package git

import (
	"context"
	"os/exec"
	"testing"
)

func TestRemoteDefaultBranch(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()

	got, err := r.RemoteDefaultBranch(ctx, "origin")
	if err != nil || got != "" {
		t.Fatalf("no remote HEAD: got %q, %v; want \"\" and no error", got, err)
	}
	for _, args := range [][]string{
		{"update-ref", "refs/remotes/origin/trunk", "HEAD"},
		{"symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	got, err = r.RemoteDefaultBranch(ctx, "origin")
	if err != nil || got != "origin/trunk" {
		t.Fatalf("got %q, %v; want origin/trunk", got, err)
	}
	if got, err = r.RemoteDefaultBranch(ctx, "upstream"); err != nil || got != "" {
		t.Fatalf("another remote: got %q, %v", got, err)
	}
}
