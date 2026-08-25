package git

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestRepoPushTag(t *testing.T) {
	t.Parallel()
	clone, runner := newClonePair(t)
	repo := &Repo{Runner: runner}
	gitIn(t, clone, "tag", "v1.0.0")
	if err := repo.PushTag(context.Background(), "origin", "v1.0.0"); err != nil {
		t.Fatalf("push tag: %v", err)
	}
	out, err := exec.Command("git", "-C", clone, "ls-remote", "--tags", "origin").Output()
	if err != nil || !strings.Contains(string(out), "refs/tags/v1.0.0") {
		t.Fatalf("tag not on origin: out=%q err=%v", out, err)
	}
}
