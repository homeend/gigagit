package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
)

func TestRestoreCurrentBranchResetsAndSnapshotsFirst(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()
	oldTip, _ := repo.RevParse(ctx, "HEAD")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644)
	if _, err := (Commit{Message: "second", All: true}).Run(ctx, OpDeps{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	ref := git.VersionRef("main", "rebase", 1753100000)
	if err := repo.UpdateRef(ctx, ref, oldTip); err != nil {
		t.Fatal(err)
	}
	newTip, _ := repo.RevParse(ctx, "HEAD")

	res, err := RestoreBranchVersion{Branch: "main", Ref: ref}.Run(ctx, enabledDeps(repo))
	if err != nil || !res.Changed {
		t.Fatalf("restore: %v %+v", err, res)
	}
	if head, _ := repo.RevParse(ctx, "HEAD"); head != oldTip {
		t.Fatalf("HEAD = %s, want restored %s", head, oldTip)
	}
	// Restore is itself undoable: a fresh "-restore" snapshot points at newTip.
	var sawRestore bool
	infos, _ := repo.ForEachRef(ctx, "refs/gg/versions")
	for _, i := range infos {
		if strings.HasSuffix(i.Ref, "-restore") && i.Hash == newTip {
			sawRestore = true
		}
	}
	if !sawRestore {
		t.Fatalf("no restore snapshot of the pre-restore tip: %+v", infos)
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
	ref := git.VersionRef("main", "rebase", 1753100000)
	repo.UpdateRef(ctx, ref, oldTip)

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
	ref := git.VersionRef("feat/gone", "delete-branch", 1753100000)
	repo.UpdateRef(ctx, ref, tip)

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
	ref := git.VersionRef("wt", "rebase", 1753100000)
	if err := repo.UpdateRef(ctx, ref, tip); err != nil {
		t.Fatal(err)
	}

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
