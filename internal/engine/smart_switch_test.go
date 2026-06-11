package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSmartSwitchCleanTree(t *testing.T) {
	dir, repo := newRepo(t)
	if err := repo.CreateBranch(context.Background(), "feature"); err != nil {
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
	if err := repo.CreateBranch(context.Background(), "feature"); err != nil {
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
