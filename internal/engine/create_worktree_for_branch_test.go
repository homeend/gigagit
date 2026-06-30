package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func wtHead(t *testing.T, wtPath string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", wtPath, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		t.Fatalf("symbolic-ref in %s: %v", wtPath, err)
	}
	return strings.TrimSpace(string(out))
}

func TestCreateWorktreeForBranchHappyPath(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "existing/y")

	wt := filepath.Join(filepath.Dir(dir), "wt-for-y")
	ch := make(chan Event, 32)
	res, err := CreateWorktreeForBranch{Branch: "existing/y", Path: wt}.Run(
		context.Background(), OpDeps{Repo: repo, Events: ch})
	close(ch)
	if err != nil {
		t.Fatalf("CreateWorktreeForBranch: %v", err)
	}
	if !res.Changed || res.Path == "" {
		t.Fatalf("result = %+v, want Changed with Path set", res)
	}
	if got := wtHead(t, res.Path); got != "existing/y" {
		t.Fatalf("worktree HEAD = %q, want existing/y", got)
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

func TestCreateWorktreeForBranchRelativePathResolvesAgainstRoot(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "rel/z")

	res, err := CreateWorktreeForBranch{Branch: "rel/z", Path: "../wt-rel-z"}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("CreateWorktreeForBranch: %v", err)
	}
	want := filepath.Clean(filepath.Join(dir, "..", "wt-rel-z"))
	gotR, _ := filepath.EvalSymlinks(res.Path)
	wantR, _ := filepath.EvalSymlinks(want)
	if gotR != wantR {
		t.Fatalf("res.Path = %q, want %q", res.Path, want)
	}
}

func TestCreateWorktreeForBranchMissingBranchGuard(t *testing.T) {
	dir, repo := newRepo(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-none")
	_, err := CreateWorktreeForBranch{Branch: "nope", Path: wt}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "no local branch") {
		t.Fatalf("want missing-branch guard, got %v", err)
	}
	if _, statErr := os.Stat(wt); !os.IsNotExist(statErr) {
		t.Fatal("no worktree dir may be created for a missing branch")
	}
}

func TestCreateWorktreeForBranchAlreadyCheckedOutGuard(t *testing.T) {
	dir, repo := newRepo(t)
	// "main" is checked out in the primary worktree.
	wt := filepath.Join(filepath.Dir(dir), "wt-dup-main")
	_, err := CreateWorktreeForBranch{Branch: "main", Path: wt}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "already checked out") {
		t.Fatalf("want already-checked-out guard, got %v", err)
	}

	// Same for a branch held by a linked worktree.
	addWorktree(t, dir, "feature/held", "wt-held")
	wt2 := filepath.Join(filepath.Dir(dir), "wt-held-2")
	_, err = CreateWorktreeForBranch{Branch: "feature/held", Path: wt2}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "already checked out") {
		t.Fatalf("want already-checked-out guard for linked worktree, got %v", err)
	}
}

func TestCreateWorktreeForBranchExistingPathGuard(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "pathy")
	taken := filepath.Join(filepath.Dir(dir), "wt-taken")
	if err := os.MkdirAll(taken, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := CreateWorktreeForBranch{Branch: "pathy", Path: taken}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "path already exists") {
		t.Fatalf("want existing-path guard, got %v", err)
	}
}

func TestCreateWorktreeForBranchRequiresFields(t *testing.T) {
	_, repo := newRepo(t)
	_, err := CreateWorktreeForBranch{}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("want required-fields error, got %v", err)
	}
}

func TestCreateWorktreeForBranchRunsHook(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "hooked/b")
	wt := filepath.Join(filepath.Dir(dir), "wt-fb-hook")
	fh := &fakeHookRunner{lines: []string{"setup done"}}
	res, err := CreateWorktreeForBranch{Branch: "hooked/b", Path: wt, PostCreateHook: "echo hi"}.Run(
		context.Background(), OpDeps{Repo: repo, HookRunner: fh, Decider: MapDecider{HookDecisionID: "run"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !fh.called || fh.spec.Dir != res.Path {
		t.Fatalf("hook not run in worktree: called=%v dir=%q want=%q", fh.called, fh.spec.Dir, res.Path)
	}
	if got := hookEnv(fh.spec, "GG_BRANCH"); got != "hooked/b" {
		t.Fatalf("GG_BRANCH = %q, want hooked/b", got)
	}
}

func TestCreateWorktreeForBranchHookFailureNonFatal(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "failhook/b")
	wt := filepath.Join(filepath.Dir(dir), "wt-fb-failhook")
	fh := &fakeHookRunner{code: 1}
	res, err := CreateWorktreeForBranch{Branch: "failhook/b", Path: wt, PostCreateHook: "false"}.Run(
		context.Background(), OpDeps{Repo: repo, HookRunner: fh, Decider: MapDecider{HookDecisionID: "run"}})
	if err != nil {
		t.Fatalf("hook failure must not fail the op: %v", err)
	}
	if !res.Changed {
		t.Fatal("worktree should still count as created")
	}
	if !strings.Contains(res.Summary, "exit 1") {
		t.Fatalf("Summary should note hook failure, got %q", res.Summary)
	}
}

func TestCreateWorktreeForBranchEmptyHookSkips(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "nohook/b")
	wt := filepath.Join(filepath.Dir(dir), "wt-fb-nohook")
	fh := &fakeHookRunner{}
	_, err := CreateWorktreeForBranch{Branch: "nohook/b", Path: wt}.Run(
		context.Background(), OpDeps{Repo: repo, HookRunner: fh})
	if err != nil {
		t.Fatal(err)
	}
	if fh.called {
		t.Fatal("empty hook must not run")
	}
}
