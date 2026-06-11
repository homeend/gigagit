package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hasProgress reports whether a Progress event was emitted.
func hasProgress(events []Event) bool {
	for _, e := range events {
		if _, ok := e.(Progress); ok {
			return true
		}
	}
	return false
}

func TestCreateWorktreeRelativePathSucceeds(t *testing.T) {
	dir, repo := newRepo(t)
	ch := make(chan Event, 32)

	// "../wt-rel" is resolved against the repo top-level (dir), i.e. a sibling.
	op := CreateWorktree{StartPoint: "main", Branch: "feature/rel", Path: "../wt-rel"}
	res, err := op.Run(context.Background(), OpDeps{Repo: repo, Events: ch})
	close(ch)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	wantPath := filepath.Clean(filepath.Join(dir, "..", "wt-rel"))
	if _, statErr := os.Stat(filepath.Join(wantPath, "README.md")); statErr != nil {
		t.Fatalf("worktree not created at %s: %v", wantPath, statErr)
	}
	if !hasProgress(drain(ch)) {
		t.Error("expected a Progress event")
	}
}

func TestCreateWorktreeInvalidBranchErrors(t *testing.T) {
	_, repo := newRepo(t)
	op := CreateWorktree{StartPoint: "main", Branch: "bad..name", Path: "../wt-bad"}
	_, err := op.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "branch") {
		t.Fatalf("want invalid-branch error, got %v", err)
	}
}

func TestCreateWorktreeExistingPathErrors(t *testing.T) {
	dir, repo := newRepo(t)
	existing := filepath.Join(filepath.Dir(dir), "wt-exists")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	op := CreateWorktree{StartPoint: "main", Branch: "feature/x", Path: existing}
	_, err := op.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "exists") {
		t.Fatalf("want path-exists error, got %v", err)
	}
}

func TestCreateWorktreeDuplicateBranchErrors(t *testing.T) {
	_, repo := newRepo(t)
	if err := repo.CreateBranch(context.Background(), "dup"); err != nil {
		t.Fatal(err)
	}
	op := CreateWorktree{StartPoint: "main", Branch: "dup", Path: "../wt-dup"}
	_, err := op.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil {
		t.Fatal("creating a worktree on an existing branch should error")
	}
}

func TestCreateWorktreeMissingFieldsError(t *testing.T) {
	_, repo := newRepo(t)
	_, err := CreateWorktree{Branch: "x", Path: "../p"}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil {
		t.Fatal("missing StartPoint should error")
	}
}
