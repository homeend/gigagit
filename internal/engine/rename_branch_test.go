package engine

import (
	"context"
	"strings"
	"testing"
)

func TestRenameBranchOpSuccess(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "old")

	res, err := RenameBranch{Old: "old", New: "renamed"}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("RenameBranch: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "renamed") {
		t.Fatalf("result = %+v, want Changed with new name in summary", res)
	}
	if !branchExists(t, dir, "renamed") {
		t.Fatal("renamed branch missing")
	}
	if branchExists(t, dir, "old") {
		t.Fatal("old branch still present")
	}
}

func TestRenameBranchOpExistingTarget(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "old")
	gitIn(t, dir, "branch", "taken")

	if _, err := (RenameBranch{Old: "old", New: "taken"}).Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("want error renaming onto an existing branch")
	}
}

func TestRenameBranchOpInvalidName(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "old")

	if _, err := (RenameBranch{Old: "old", New: "bad name"}).Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("want validation error for an illegal branch name")
	}
}

func TestRenameBranchOpRequiresBothNames(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	if _, err := (RenameBranch{Old: "old"}).Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("want error when New is empty")
	}
}
