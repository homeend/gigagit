package git

import (
	"context"
	"testing"
)

func TestRepoDeleteTag(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c1")
	gitIn(t, dir, "tag", "v1.0.0")
	if err := repo.DeleteTag(context.Background(), "v1.0.0"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if out := gitOutIn(t, dir, "tag", "-l"); out != "" {
		t.Fatalf("tag still present: %q", out)
	}
}
