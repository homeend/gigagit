package git

import (
	"context"
	"testing"
)

func TestRepoSwitchDetach(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c1")
	gitIn(t, dir, "tag", "v1.0.0")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c2")
	if err := repo.SwitchDetach(context.Background(), "v1.0.0"); err != nil {
		t.Fatalf("detach: %v", err)
	}
	if b := gitOutIn(t, dir, "branch", "--show-current"); b != "" {
		t.Fatalf("expected detached HEAD, on branch %q", b)
	}
	if gitOutIn(t, dir, "rev-parse", "--short", "HEAD") != gitOutIn(t, dir, "rev-parse", "--short", "v1.0.0") {
		t.Fatal("HEAD not at the tag")
	}
}

func TestRepoSwitchCreate(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c1")
	gitIn(t, dir, "tag", "v1.0.0")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c2")
	if err := repo.SwitchCreate(context.Background(), "rel", "v1.0.0"); err != nil {
		t.Fatalf("create+switch: %v", err)
	}
	if b := gitOutIn(t, dir, "branch", "--show-current"); b != "rel" {
		t.Fatalf("on branch %q, want rel", b)
	}
	if gitOutIn(t, dir, "rev-parse", "--short", "HEAD") != gitOutIn(t, dir, "rev-parse", "--short", "v1.0.0") {
		t.Fatal("new branch not at the tag")
	}
}
