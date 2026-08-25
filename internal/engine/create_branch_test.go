package engine

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// engineRevParse resolves ref to a full sha in dir.
func engineRevParse(t *testing.T, dir, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", ref).Output()
	if err != nil {
		t.Fatalf("rev-parse %s: %v", ref, err)
	}
	return strings.TrimSpace(string(out))
}

func TestCreateBranchAtHead(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ch := make(chan Event, 16)
	res, err := CreateBranch{Name: "feat/x"}.Run(context.Background(), OpDeps{Repo: repo, Events: ch})
	close(ch)
	if err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "feat/x") {
		t.Fatalf("result = %+v, want Changed with branch in summary", res)
	}
	if !branchExists(t, dir, "feat/x") {
		t.Fatal("branch not created")
	}
	var sawDone bool
	for _, e := range drain(ch) {
		if _, ok := e.(Done); ok {
			sawDone = true
		}
	}
	if !sawDone {
		t.Fatal("expected a Done event")
	}
}

func TestCreateBranchFromStartPoint(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "base")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "advance main")

	_, err := CreateBranch{Name: "feat/from-base", StartPoint: "base"}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if got, want := engineRevParse(t, dir, "feat/from-base"), engineRevParse(t, dir, "base"); got != want {
		t.Fatalf("tip = %s, want %s (the start point)", got, want)
	}
}

func TestCreateBranchInvalidNameFailsFast(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	res, err := CreateBranch{Name: "bad..name"}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "invalid branch name") {
		t.Fatalf("want invalid-name error, got res=%+v err=%v", res, err)
	}
	if branchExists(t, dir, "bad..name") {
		t.Fatal("invalid branch must not be created")
	}
}

func TestCreateBranchExistingNameErrors(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "taken")
	res, err := CreateBranch{Name: "taken"}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil {
		t.Fatalf("creating an existing branch must error, got %+v", res)
	}
}

func TestCreateBranchRequiresName(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	_, err := CreateBranch{}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "Name is required") {
		t.Fatalf("want Name-required error, got %v", err)
	}
}
