package engine

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/repogate"
)

func TestFetchUpdatesAllRemotes(t *testing.T) {
	t.Parallel()
	clone, repo := cloneOnMainBehindOrigin(t)
	before := revAt(t, clone, "origin/main")
	res, err := Fetch{}.Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if revAt(t, clone, "origin/main") == before {
		t.Fatal("Fetch did not advance refs/remotes/origin/main")
	}
}

func TestFetchLockModeIsRefWrite(t *testing.T) {
	t.Parallel()
	if (Fetch{}).LockMode() != repogate.RefWrite {
		t.Fatal("Fetch must be RefWrite")
	}
}
