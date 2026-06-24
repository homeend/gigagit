package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/repogate"
)

func cloneWithDeletedOriginFoo(t *testing.T) *git.Repo {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone := filepath.Join(root, "clone")
	gitAt(t, root, "init", "--bare", origin)
	gitAt(t, root, "clone", origin, seed)
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v1\n"), 0o644)
	gitAt(t, seed, "checkout", "-b", "main")
	gitAt(t, seed, "add", ".")
	gitAt(t, seed, "commit", "-m", "v1")
	gitAt(t, seed, "push", "-u", "origin", "main")
	gitAt(t, seed, "push", "origin", "main:foo") // create origin/foo
	gitAt(t, root, "clone", origin, clone)       // clone gets refs/remotes/origin/foo
	gitAt(t, seed, "push", "origin", "--delete", "foo")
	return &git.Repo{Runner: gitexec.NewExecRunner("git", clone, observ.NewRing(50))}
}

func hasRemote(rbs []model.RemoteBranch, name string) bool {
	for _, rb := range rbs {
		if rb.Name == name {
			return true
		}
	}
	return false
}

func TestPruneRemovesDeletedUpstreamRef(t *testing.T) {
	repo := cloneWithDeletedOriginFoo(t)
	rbs, _ := repo.RemoteBranches(context.Background())
	if !hasRemote(rbs, "origin/foo") {
		t.Fatal("setup: origin/foo should exist before prune")
	}
	res, err := Prune{}.Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	rbs, _ = repo.RemoteBranches(context.Background())
	if hasRemote(rbs, "origin/foo") {
		t.Fatal("origin/foo should be pruned")
	}
}

func TestPruneNoRemotesIsNoOp(t *testing.T) {
	_, repo := newRepo(t)
	res, err := Prune{}.Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("Prune no-remotes: %v", err)
	}
	if res.Changed {
		t.Fatalf("no remotes -> not Changed, got %+v", res)
	}
}

func TestPruneLockModeIsRefWrite(t *testing.T) {
	if (Prune{}).LockMode() != repogate.RefWrite {
		t.Fatal("Prune must be RefWrite")
	}
}
