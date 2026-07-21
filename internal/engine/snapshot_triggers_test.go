package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/rebaseplan"
)

func enabledDeps(repo GitOps) OpDeps {
	return OpDeps{Repo: repo, Versions: VersionsPolicy{Enabled: true, MaxAgeDays: 90}}
}

// staticTestDecider answers a mapped decision ID from answers, else the
// fork's first option — used where a test doesn't care which fork fires
// (e.g. a clean merge that never forks) but still needs a non-nil Decider.
type staticTestDecider struct{ answers map[string]string }

func (d staticTestDecider) Decide(_ context.Context, req DecisionRequest) (DecisionResponse, error) {
	if a, ok := d.answers[req.ID]; ok {
		return DecisionResponse{Option: a}, nil
	}
	return DecisionResponse{Option: req.Options[0]}, nil
}

// findVersionRef locates the single version ref recorded for branch/opToken
// (refs/gg/versions/<branch>/<ts>-<opToken>) and returns its ref name + the
// hash it points at, failing the test if there isn't exactly one.
func findVersionRef(t *testing.T, repo *git.Repo, branch, opToken string) (ref, hash string) {
	t.Helper()
	ctx := context.Background()
	infos, err := repo.ForEachRef(ctx, "refs/gg/versions/"+branch)
	if err != nil {
		t.Fatal(err)
	}
	suffix := "-" + opToken
	var matches []string
	for _, i := range infos {
		if strings.HasSuffix(i.Ref, suffix) {
			matches = append(matches, i.Ref)
			ref, hash = i.Ref, i.Hash
		}
	}
	if len(matches) != 1 {
		t.Fatalf("version refs for branch %s op %s = %v, want exactly 1", branch, opToken, matches)
	}
	return ref, hash
}

func TestAmendSnapshotsAndPlainCommitDoesNot(t *testing.T) {
	dir, repo := newRepo(t)
	ctx := context.Background()

	os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644)
	if _, err := (Commit{Message: "second", All: true}).Run(ctx, enabledDeps(repo)); err != nil {
		t.Fatal(err)
	}
	if refs := versionRefs(t, repo); len(refs) != 0 {
		t.Fatalf("plain commit snapshotted: %v", refs)
	}

	if _, err := (Commit{Message: "second (amended)", Amend: true}).Run(ctx, enabledDeps(repo)); err != nil {
		t.Fatal(err)
	}
	refs := versionRefs(t, repo)
	if len(refs) != 1 || !strings.HasSuffix(refs[0], "-amend") {
		t.Fatalf("amend refs = %v", refs)
	}
}

// TestSmartMergeSnapshotsTarget: feat and main diverge from the same base
// (disjoint files, no conflict); merging feat into main must snapshot main's
// pre-merge tip.
func TestSmartMergeSnapshotsTarget(t *testing.T) {
	dir, repo := newRepo(t)
	ctx := context.Background()

	if err := repo.CreateBranch(ctx, "feat", ""); err != nil {
		t.Fatal(err)
	}

	// New (untracked) files: stage explicitly — Commit's All (`-a`) only
	// picks up modifications to already-tracked files.
	os.WriteFile(filepath.Join(dir, "main.txt"), []byte("m\n"), 0o644)
	gitE(t, dir, "add", ".")
	if _, err := (Commit{Message: "main change"}).Run(ctx, OpDeps{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	preMergeTip, err := repo.RevParse(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.Switch(ctx, "feat"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("f\n"), 0o644)
	gitE(t, dir, "add", ".")
	if _, err := (Commit{Message: "feat change"}).Run(ctx, OpDeps{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Switch(ctx, "main"); err != nil {
		t.Fatal(err)
	}

	deps := enabledDeps(repo)
	deps.Decider = staticTestDecider{}
	if _, err := (SmartMerge{Source: "feat"}).Run(ctx, deps); err != nil {
		t.Fatalf("merge: %v", err)
	}

	_, hash := findVersionRef(t, repo, "main", "merge")
	if hash != preMergeTip {
		t.Fatalf("snapshot hash = %s, want main's pre-merge tip %s", hash, preMergeTip)
	}
}

// TestSmartRebaseSnapshotsBranch: feat and main diverge with disjoint files;
// rebasing feat onto main must snapshot feat's pre-rebase tip.
func TestSmartRebaseSnapshotsBranch(t *testing.T) {
	dir, repo := divergedRepo(t)
	ctx := context.Background()
	_ = dir

	if err := repo.Switch(ctx, "feat"); err != nil {
		t.Fatal(err)
	}
	preRebaseTip, err := repo.RevParse(ctx, "feat")
	if err != nil {
		t.Fatal(err)
	}

	deps := enabledDeps(repo)
	deps.Decider = staticTestDecider{}
	if _, err := (SmartRebase{Branch: "feat", Onto: "main"}).Run(ctx, deps); err != nil {
		t.Fatalf("rebase: %v", err)
	}

	_, hash := findVersionRef(t, repo, "feat", "rebase")
	if hash != preRebaseTip {
		t.Fatalf("snapshot hash = %s, want feat's pre-rebase tip %s", hash, preRebaseTip)
	}
}

func TestUndoLastCommitSnapshots(t *testing.T) {
	dir, repo := newRepo(t)
	ctx := context.Background()

	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	gitE(t, dir, "add", ".")
	if _, err := (Commit{Message: "a"}).Run(ctx, OpDeps{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	preUndoTip, err := repo.RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	deps := enabledDeps(repo)
	if _, err := (UndoLastCommit{}).Run(ctx, deps); err != nil {
		t.Fatalf("undo: %v", err)
	}

	_, hash := findVersionRef(t, repo, "main", "undo-commit")
	if hash != preUndoTip {
		t.Fatalf("snapshot hash = %s, want pre-undo tip %s", hash, preUndoTip)
	}
}

func TestResetSnapshots(t *testing.T) {
	dir, repo := newRepo(t)
	ctx := context.Background()

	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "add a")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "add b")
	preResetTip, err := repo.RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	deps := enabledDeps(repo)
	if _, err := (Reset{Commit: "HEAD~1", Mode: "hard"}).Run(ctx, deps); err != nil {
		t.Fatalf("reset: %v", err)
	}

	_, hash := findVersionRef(t, repo, "main", "reset")
	if hash != preResetTip {
		t.Fatalf("snapshot hash = %s, want pre-reset tip %s", hash, preResetTip)
	}
}

// TestDeleteBranchSnapshots: feat carries a commit main doesn't have, so the
// safe `branch -d` refuses and the branch-unmerged fork fires; the snapshot
// must still land (after the delete-branch confirm, before either delete
// attempt) and point at feat's tip — recoverable after the branch is gone.
func TestDeleteBranchSnapshots(t *testing.T) {
	dir, repo := newRepo(t)
	ctx := context.Background()

	if err := repo.CreateBranch(ctx, "feat", ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.Switch(ctx, "feat"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("f\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "feat change")
	preDeleteTip, err := repo.RevParse(ctx, "feat")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Switch(ctx, "main"); err != nil {
		t.Fatal(err)
	}

	deps := enabledDeps(repo)
	deps.Decider = MapDecider{"delete-branch": "delete", "branch-unmerged": "force-delete"}
	if _, err := (DeleteBranch{Name: "feat"}).Run(ctx, deps); err != nil {
		t.Fatalf("delete branch: %v", err)
	}

	_, hash := findVersionRef(t, repo, "feat", "delete-branch")
	if hash != preDeleteTip {
		t.Fatalf("snapshot hash = %s, want feat's pre-delete tip %s", hash, preDeleteTip)
	}
}

func TestDeleteBranchCancelledDoesNotSnapshot(t *testing.T) {
	_, repo := newRepo(t)
	ctx := context.Background()

	if err := repo.CreateBranch(ctx, "feat", ""); err != nil {
		t.Fatal(err)
	}

	deps := enabledDeps(repo)
	deps.Decider = MapDecider{"delete-branch": "abort"}
	if _, err := (DeleteBranch{Name: "feat"}).Run(ctx, deps); err != nil {
		t.Fatalf("delete branch: %v", err)
	}

	if refs := versionRefs(t, repo); len(refs) != 0 {
		t.Fatalf("cancelled delete snapshotted: %v", refs)
	}
}

// TestInteractiveRebaseInvalidOntoDoesNotSnapshot: an invalid Onto ref
// (non-existent commit) must be rejected before snapshotting.
func TestInteractiveRebaseInvalidOntoDoesNotSnapshot(t *testing.T) {
	_, repo := newRepo(t)
	ctx := context.Background()

	if err := repo.CreateBranch(ctx, "feat", ""); err != nil {
		t.Fatal(err)
	}

	deps := enabledDeps(repo)
	deps.Decider = staticTestDecider{}

	// Minimal valid plan: one Pick entry for a real commit.
	// We reuse head commit for the entry's sha even though the plan
	// won't actually execute (we expect an earlier validation error).
	head, err := repo.RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	// Run with a non-existent Onto: should fail validation and not snapshot.
	_, err = (InteractiveRebase{
		Branch: "feat",
		Onto:   "nosuchref",
		Plan: rebaseplan.Plan{Entries: []rebaseplan.Entry{
			{Sha: head, Action: rebaseplan.Pick, Orig: "dummy"},
		}},
		GGBin: "/bin/true",
	}).Run(ctx, deps)

	if err == nil {
		t.Fatal("expected error for invalid Onto, got nil")
	}
	if !strings.Contains(err.Error(), "no such commit") {
		t.Fatalf("error = %v, want 'no such commit'", err)
	}

	if refs := versionRefs(t, repo); len(refs) != 0 {
		t.Fatalf("invalid Onto snapshotted: %v", refs)
	}
}
