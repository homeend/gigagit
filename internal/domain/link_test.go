package domain

import (
	"context"
	"testing"
)

func TestRepoNamePrefersOriginAndStripsDotGit(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	runGitIn(t, dir, "remote", "add", "upstream", "https://example.com/other/thing.git")
	runGitIn(t, dir, "remote", "add", "origin", "git@github.com:homeend/gigagit.git")
	got, err := svc.RepoName(context.Background())
	if err != nil {
		t.Fatalf("RepoName: %v", err)
	}
	if got != "gigagit" {
		t.Errorf("RepoName = %q, want %q", got, "gigagit")
	}
}

func TestRepoNameFallsBackToTheFirstRemote(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	runGitIn(t, dir, "remote", "add", "upstream", "https://example.com/other/thing.git")
	got, err := svc.RepoName(context.Background())
	if err != nil {
		t.Fatalf("RepoName: %v", err)
	}
	if got != "thing" {
		t.Errorf("RepoName = %q, want %q", got, "thing")
	}
}

func TestRepoNameIsEmptyWithoutARemote(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	got, err := svc.RepoName(context.Background())
	if err != nil {
		t.Fatalf("RepoName: %v", err)
	}
	if got != "" {
		t.Errorf("RepoName = %q, want \"\" (no remote → the local link form)", got)
	}
}
