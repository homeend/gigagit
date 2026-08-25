package engine

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/repogate"
)

func TestAddFetchMappingsEmptyIsNoOp(t *testing.T) {
	t.Parallel()
	op := AddFetchMappings{Remote: "origin"}
	res, err := op.Run(context.Background(), OpDeps{})
	if err != nil || res.Changed {
		t.Fatalf("empty: res=%+v err=%v", res, err)
	}
}

func TestAddFetchMappingsLockModeRefWrite(t *testing.T) {
	t.Parallel()
	op := AddFetchMappings{}
	if op.LockMode() != repogate.RefWrite {
		t.Fatal("AddFetchMappings must reserve RefWrite")
	}
}

func TestAddFetchMappingsMapsAndFetches(t *testing.T) {
	t.Parallel()
	repo := narrowClone(t)
	ctx := context.Background()
	if err := repo.Push(ctx, "origin", "feat", true, 0); err != nil {
		t.Fatalf("seed push: %v", err)
	}
	res, err := AddFetchMappings{Remote: "origin", Branches: []string{"feat"}}.Run(ctx,
		OpDeps{Repo: repo, Events: make(chan Event, 64)})
	if err != nil {
		t.Fatalf("op: %v", err)
	}
	if !res.Changed || res.Summary != "mapped 1 branch for tracking" {
		t.Fatalf("res = %+v", res)
	}
	if refs, _ := repo.ForEachRef(ctx, "refs/remotes/origin/feat"); len(refs) != 1 {
		t.Fatalf("tracking ref missing: %v", refs)
	}
	res2, err := AddFetchMappings{Remote: "origin", Branches: []string{"feat"}}.Run(ctx,
		OpDeps{Repo: repo, Events: make(chan Event, 64)})
	if err != nil || res2.Summary != "mapped 1 branch for tracking" {
		t.Fatalf("rerun: res=%+v err=%v", res2, err)
	}
	specs, _ := repo.ConfigGetAll(ctx, "remote.origin.fetch")
	n := 0
	for _, s := range specs {
		if s == fetchSpec("origin", "feat") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("mapping duplicated: %v", specs)
	}
}
