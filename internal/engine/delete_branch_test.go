package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeleteBranchMergedDeletes(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "merged")

	res, err := DeleteBranch{Name: "merged"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"delete-branch": "delete"}})
	if err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "merged") {
		t.Fatalf("result = %+v, want Changed with branch in summary", res)
	}
	if branchExists(t, dir, "merged") {
		t.Fatal("branch still exists")
	}
}

func TestDeleteBranchConfirmAbortKeepsBranch(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "stay")

	res, err := DeleteBranch{Name: "stay"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"delete-branch": "abort"}})
	if err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
	if res.Changed {
		t.Fatal("abort must not change anything")
	}
	if !branchExists(t, dir, "stay") {
		t.Fatal("branch should survive an aborted delete")
	}
}

// unmergedBranch creates a branch with a commit main doesn't have, then
// returns to main, so `git branch -d` refuses to delete it.
func unmergedBranch(t *testing.T, dir, name string) {
	t.Helper()
	gitIn(t, dir, "switch", "-c", name)
	if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "unmerged work")
	gitIn(t, dir, "switch", "main")
}

func TestDeleteBranchUnmergedForceDeletes(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	unmergedBranch(t, dir, "risky")

	res, err := DeleteBranch{Name: "risky"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{
			"delete-branch":   "delete",
			"branch-unmerged": "force-delete",
		}})
	if err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if branchExists(t, dir, "risky") {
		t.Fatal("unmerged branch should be force-deleted")
	}
}

func TestDeleteBranchUnmergedKeepKeeps(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	unmergedBranch(t, dir, "precious")

	res, err := DeleteBranch{Name: "precious"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{
			"delete-branch":   "delete",
			"branch-unmerged": "keep",
		}})
	if err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
	if res.Changed {
		t.Fatal("keeping the branch must not report Changed")
	}
	if !branchExists(t, dir, "precious") {
		t.Fatal("branch should be kept")
	}
	if !strings.Contains(res.Summary, "kept") {
		t.Fatalf("summary should mention the branch was kept: %q", res.Summary)
	}
}

func TestDeleteBranchGuardsCurrentBranch(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	_, err := DeleteBranch{Name: "main"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"delete-branch": "delete"}})
	if err == nil || !strings.Contains(err.Error(), "checked-out branch") {
		t.Fatalf("want current-branch guard error, got %v", err)
	}
}

func TestDeleteBranchGuardsWorktreeBranch(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	addWorktree(t, dir, "feature/wt", "wt-branchdel")

	_, err := DeleteBranch{Name: "feature/wt"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"delete-branch": "delete"}})
	if err == nil || !strings.Contains(err.Error(), "checked out in worktree") {
		t.Fatalf("want worktree guard error, got %v", err)
	}
	if !branchExists(t, dir, "feature/wt") {
		t.Fatal("guarded branch must survive")
	}
}

func TestDeleteBranchRequiresName(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	_, err := DeleteBranch{}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "Name is required") {
		t.Fatalf("want Name-required error, got %v", err)
	}
}

func TestDeleteBranchEmitsBothDecisions(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	unmergedBranch(t, dir, "forked")

	ch := make(chan Event, 32)
	_, err := DeleteBranch{Name: "forked"}.Run(context.Background(),
		OpDeps{Repo: repo, Events: ch, Decider: MapDecider{
			"delete-branch":   "delete",
			"branch-unmerged": "keep",
		}})
	close(ch)
	if err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
	got := map[string][]string{}
	for _, e := range drain(ch) {
		if d, ok := e.(DecisionNeeded); ok {
			got[d.Request.ID] = d.Request.Options
		}
	}
	if strings.Join(got["delete-branch"], ",") != "delete,abort" {
		t.Fatalf("delete-branch options = %v", got["delete-branch"])
	}
	if strings.Join(got["branch-unmerged"], ",") != "force-delete,keep" {
		t.Fatalf("branch-unmerged options = %v (must match RemoveWorktree's)", got["branch-unmerged"])
	}
}
