package git

import (
	"context"
	"testing"
)

func TestRepoCreateTag(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c1")
	head := gitOutIn(t, dir, "rev-parse", "HEAD")

	if err := repo.CreateTag(context.Background(), "v1.0.0", head, ""); err != nil {
		t.Fatalf("lightweight: %v", err)
	}
	if err := repo.CreateTag(context.Background(), "v2.0.0", head, "release two"); err != nil {
		t.Fatalf("annotated: %v", err)
	}
	if typ := gitOutIn(t, dir, "cat-file", "-t", "v1.0.0"); typ != "commit" {
		t.Fatalf("v1.0.0 type = %q, want commit (lightweight)", typ)
	}
	if typ := gitOutIn(t, dir, "cat-file", "-t", "v2.0.0"); typ != "tag" {
		t.Fatalf("v2.0.0 type = %q, want tag (annotated)", typ)
	}
}
