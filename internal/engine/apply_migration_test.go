package engine

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/repogate"
)

func TestApplyMigrationDeletesRefsAndStamps(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	ctx := context.Background()
	deps := OpDeps{Repo: repo}

	head, err := repo.RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	refs := []string{
		"refs/gg/versions/main/1700000000-rebase",
		"refs/gg/versions/main/1700000001-merge",
	}
	for _, ref := range refs {
		if err := repo.UpdateRef(ctx, ref, head); err != nil {
			t.Fatal(err)
		}
	}

	op := ApplyMigration{
		Feature: "versions",
		Store:   "versions",
		To:      2,
		Action:  DiscardRefs{Refs: refs},
	}
	res, err := op.Run(ctx, deps)
	if err != nil {
		t.Fatalf("ApplyMigration: %v", err)
	}
	if !res.Changed {
		t.Error("Result.Changed = false, want true")
	}

	left, err := deps.Repo.ForEachRef(ctx, "refs/gg/versions/")
	if err != nil {
		t.Fatalf("ForEachRef: %v", err)
	}
	if len(left) != 0 {
		t.Errorf("%d version refs survived the migration, want 0", len(left))
	}

	formats, err := deps.Repo.StoreFormats(ctx)
	if err != nil {
		t.Fatalf("StoreFormats: %v", err)
	}
	if formats["versions"] != 2 {
		t.Errorf("StoreFormats = %v, want versions=2", formats)
	}
}

func TestApplyMigrationTakesRefWrite(t *testing.T) {
	t.Parallel()
	if got := (ApplyMigration{}).LockMode(); got != repogate.RefWrite {
		t.Errorf("LockMode = %v, want RefWrite", got)
	}
}

func TestApplyMigrationRequiresAStore(t *testing.T) {
	t.Parallel()
	if _, err := (ApplyMigration{To: 2}).Run(context.Background(), OpDeps{}); err == nil {
		t.Error("Run with no Store = nil error, want an error")
	}
}
