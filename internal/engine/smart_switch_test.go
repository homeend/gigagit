package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSmartSwitchCleanTree(t *testing.T) {
	dir, repo := newRepo(t)
	if err := repo.CreateBranch(context.Background(), "feature", ""); err != nil {
		t.Fatal(err)
	}
	_ = dir

	res, err := SmartSwitch{Branch: "feature"}.Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("smart switch: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	cur, _ := repo.CurrentBranch(context.Background())
	if cur != "feature" {
		t.Fatalf("current branch = %q, want feature", cur)
	}
}

func TestSmartSwitchStashesDirtyTreeAndRestores(t *testing.T) {
	dir, repo := newRepo(t)
	if err := repo.CreateBranch(context.Background(), "feature", ""); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644)

	res, err := SmartSwitch{Branch: "feature"}.Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("smart switch: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v", res)
	}
	cur, _ := repo.CurrentBranch(context.Background())
	if cur != "feature" {
		t.Fatalf("current branch = %q, want feature", cur)
	}
	dirty, _ := repo.IsDirty(context.Background())
	if !dirty {
		t.Fatal("expected stashed change to be restored on feature")
	}
}

func TestSmartSwitchAlreadyOnBranchIsNoop(t *testing.T) {
	_, repo := newRepo(t)
	res, err := SmartSwitch{Branch: "main"}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("smart switch: %v", err)
	}
	if res.Changed {
		t.Fatalf("switching to current branch should not be Changed: %+v", res)
	}
}

func TestSmartSwitchStashPopConflictPreservesStash(t *testing.T) {
	dir, repo := newRepo(t)
	ctx := context.Background()

	// feature commits an overlapping change to README.md.
	if err := repo.CreateBranch(ctx, "feature", ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.Switch(ctx, "feature"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("feature-version\n"), 0o644)
	if err := repo.Commit(ctx, "feature change", true, false); err != nil {
		t.Fatal(err)
	}
	if err := repo.Switch(ctx, "main"); err != nil {
		t.Fatal(err)
	}

	// Uncommitted, overlapping change on main — will conflict when restored onto feature.
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("my-uncommitted\n"), 0o644)

	_, err := SmartSwitch{Branch: "feature"}.Run(ctx, OpDeps{Repo: repo, Decider: MapDecider{}})
	if err == nil {
		t.Fatal("expected an error from the stash-pop conflict")
	}
	cur, _ := repo.CurrentBranch(ctx)
	if cur != "feature" {
		t.Fatalf("current branch = %q, want feature (switch occurs before the failed pop)", cur)
	}
	list, lerr := repo.StashList(ctx)
	if lerr != nil {
		t.Fatalf("stash list: %v", lerr)
	}
	if len(list) == 0 {
		t.Fatal("stash MUST be preserved after a pop conflict (never dropped)")
	}
}
