package git

import (
	"context"
	"testing"
)

func TestEmptyTreeIsWritableAndStable(t *testing.T) {
	t.Parallel()
	_, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()

	sha, err := r.EmptyTree(ctx)
	if err != nil {
		t.Fatalf("EmptyTree: %v", err)
	}
	if sha == "" {
		t.Fatal("EmptyTree returned an empty sha")
	}

	// The object must exist in the odb: update-ref refuses a missing target.
	if err := r.UpdateRef(ctx, "refs/gg/meta/probe/1", sha); err != nil {
		t.Fatalf("UpdateRef to the empty tree: %v", err)
	}

	again, err := r.EmptyTree(ctx)
	if err != nil {
		t.Fatalf("EmptyTree (second call): %v", err)
	}
	if again != sha {
		t.Errorf("EmptyTree is not stable: %q then %q", sha, again)
	}
}
