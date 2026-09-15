package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
)

// writeVersionRef fabricates a version ref in the real (synthetic-commit)
// production shape: a wrapper commit whose first parent is tip, no preview
// endpoints (matching the one-branch ops these tests restore). Raw
// update-ref-straight-at-tip would leave op.Ref with no parent to unwrap and
// break RestoreBranchVersion's op.Ref+"^1" resolution.
func writeVersionRef(t *testing.T, repo *git.Repo, branch, opToken string, unix int64, tip string) string {
	t.Helper()
	ctx := context.Background()
	syn, err := repo.WriteVersionSnapshot(ctx, tip, git.VersionMeta{Op: opToken}, unix)
	if err != nil {
		t.Fatal(err)
	}
	ref := git.VersionRef(branch, opToken, unix)
	if err := repo.UpdateRef(ctx, ref, syn); err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestRestoreCurrentBranchResetsAndSnapshotsFirst(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()
	oldTip, _ := repo.RevParse(ctx, "HEAD")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644)
	if _, err := (Commit{Message: "second", All: true}).Run(ctx, OpDeps{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	ref := writeVersionRef(t, repo, "main", "rebase", 1753100000, oldTip)
	newTip, _ := repo.RevParse(ctx, "HEAD")

	res, err := RestoreBranchVersion{Branch: "main", Ref: ref}.Run(ctx, enabledDeps(repo))
	if err != nil || !res.Changed {
		t.Fatalf("restore: %v %+v", err, res)
	}
	if head, _ := repo.RevParse(ctx, "HEAD"); head != oldTip {
		t.Fatalf("HEAD = %s, want restored %s", head, oldTip)
	}
	// Restore is itself undoable: a fresh "-restore" snapshot points at newTip
	// (VersionRefs unwraps the synthetic wrapper commit it too is stored as).
	var sawRestore bool
	bvs, _ := repo.VersionRefs(ctx, "refs/gg/versions")
	for _, bv := range bvs {
		if strings.HasSuffix(bv.Ref, "-restore") && bv.Hash == newTip {
			sawRestore = true
		}
	}
	if !sawRestore {
		t.Fatalf("no restore snapshot of the pre-restore tip: %+v", bvs)
	}
}

func TestRestoreDirtyTreeForksAndCancelKeepsState(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()
	oldTip, _ := repo.RevParse(ctx, "HEAD")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644)
	if _, err := (Commit{Message: "second", All: true}).Run(ctx, OpDeps{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644) // uncommitted
	ref := writeVersionRef(t, repo, "main", "rebase", 1753100000, oldTip)

	deps := enabledDeps(repo)
	deps.Decider = staticTestDecider{answers: map[string]string{"restore-dirty": "cancel"}}
	res, err := RestoreBranchVersion{Branch: "main", Ref: ref}.Run(ctx, deps)
	if err != nil || res.Changed {
		t.Fatalf("cancelled restore: %v %+v (want Changed=false, nil err)", err, res)
	}
}

func TestRestoreOtherBranchMovesRefAndRecreatesDeleted(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	ctx := context.Background()
	tip, _ := repo.RevParse(ctx, "HEAD")
	// A version of a branch that does not exist (deleted-branch recovery).
	ref := writeVersionRef(t, repo, "feat/gone", "delete-branch", 1753100000, tip)

	res, err := RestoreBranchVersion{Branch: "feat/gone", Ref: ref}.Run(ctx, enabledDeps(repo))
	if err != nil || !res.Changed {
		t.Fatalf("restore deleted: %v %+v", err, res)
	}
	if sha, _ := repo.RevParse(ctx, "refs/heads/feat/gone"); sha != tip {
		t.Fatalf("feat/gone = %s, want %s", sha, tip)
	}
}

func TestRestoreRefusesBranchCheckedOutElsewhere(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()
	wt := addWorktree(t, dir, "wt", "wt-elsewhere")
	tip, _ := repo.RevParse(ctx, "refs/heads/wt")
	ref := writeVersionRef(t, repo, "wt", "rebase", 1753100000, tip)

	_, err := RestoreBranchVersion{Branch: "wt", Ref: ref}.Run(ctx, enabledDeps(repo))
	if err == nil {
		t.Fatal("expected a refusal error, got nil")
	}
	if !strings.Contains(err.Error(), wt) {
		t.Fatalf("error %q does not name the worktree path %q", err.Error(), wt)
	}
	if sha, _ := repo.RevParse(ctx, "refs/heads/wt"); sha != tip {
		t.Fatalf("refs/heads/wt moved to %s, want unchanged %s", sha, tip)
	}
}

func TestDeleteBranchVersion(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	ctx := context.Background()
	tip, _ := repo.RevParse(ctx, "HEAD")
	ref := git.VersionRef("main", "merge", 1753100000)
	repo.UpdateRef(ctx, ref, tip)

	if _, err := (DeleteBranchVersion{Ref: "refs/heads/main"}).Run(ctx, OpDeps{Repo: repo}); err == nil {
		t.Fatal("deleting outside the versions namespace must be refused")
	}
	res, err := DeleteBranchVersion{Ref: ref}.Run(ctx, OpDeps{Repo: repo})
	if err != nil || !res.Changed {
		t.Fatalf("delete: %v %+v", err, res)
	}
	if refs := versionRefs(t, repo); len(refs) != 0 {
		t.Fatalf("ref survived: %v", refs)
	}
}

// TestRestoreBranchVersionUnwrapsSyntheticCommit is Task 5's Step 5b
// regression test: a version ref now points at a synthetic wrapper commit
// (Step 4), so restore must resolve op.Ref^1 — its first parent, the
// snapshotted tip — not op.Ref itself. Before the fix, restoring landed the
// branch ON the wrapper commit rather than the tip it recorded: silently
// wrong history.
func TestRestoreBranchVersionUnwrapsSyntheticCommit(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()
	originalTip, err := repo.RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	// Snapshot "main" while it is still at originalTip.
	deps := enabledDeps(repo)
	snapshotBranchTip(ctx, deps, "main", "rebase", "", "")
	ref, snapshottedTip := findVersionRef(t, repo, "main", "rebase")
	if snapshottedTip != originalTip {
		t.Fatalf("findVersionRef unwrapped to %s, want %s", snapshottedTip, originalTip)
	}
	synSha, err := repo.RevParse(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if synSha == originalTip {
		t.Fatal("test fixture assumption broken: the synthetic wrapper commit's own sha must differ from the tip it wraps")
	}

	// Move the branch forward.
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644)
	if _, err := (Commit{Message: "second", All: true}).Run(ctx, OpDeps{Repo: repo}); err != nil {
		t.Fatal(err)
	}

	res, err := RestoreBranchVersion{Branch: "main", Ref: ref}.Run(ctx, deps)
	if err != nil || !res.Changed {
		t.Fatalf("restore: %v %+v", err, res)
	}
	head, err := repo.RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if head != originalTip {
		t.Fatalf("restored HEAD = %s, want the ORIGINAL tip %s", head, originalTip)
	}
	if head == synSha {
		t.Fatalf("restored HEAD = %s, the synthetic commit's OWN sha — restore must unwrap to p1, not land on the wrapper", head)
	}
}

// TestRestoreBranchVersionLegacyPlainTipRef is the fix-round companion to
// TestRestoreBranchVersionUnwrapsSyntheticCommit: a LEGACY version ref (raw
// update-ref straight at the tip, predating the synthetic-wrapper format —
// no Gg-Meta trailer, so VersionRefs takes Hash from %(objectname) rather
// than unwrapping a first parent) must restore to that same tip — the ref
// itself, not one commit further back. Restore resolves its target through
// deps.Repo.VersionRefs, the one place that already tells the two ref
// shapes apart; a second, independent "what does this ref mean" rule (e.g.
// blindly resolving ref^1) would disagree with the reader on exactly this
// shape and land on the wrong commit.
func TestRestoreBranchVersionLegacyPlainTipRef(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()
	originalTip, err := repo.RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	ref := git.VersionRef("main", "merge", 1753100000)
	if err := repo.UpdateRef(ctx, ref, originalTip); err != nil {
		t.Fatal(err)
	}

	// Move the branch forward.
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644)
	if _, err := (Commit{Message: "second", All: true}).Run(ctx, OpDeps{Repo: repo}); err != nil {
		t.Fatal(err)
	}

	res, err := RestoreBranchVersion{Branch: "main", Ref: ref}.Run(ctx, enabledDeps(repo))
	if err != nil || !res.Changed {
		t.Fatalf("restore: %v %+v", err, res)
	}
	head, err := repo.RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if head != originalTip {
		t.Fatalf("restored HEAD = %s, want the legacy ref's own tip %s", head, originalTip)
	}
}
