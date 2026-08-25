package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

// originFileContent clones origin afresh and returns main:path's content, or
// "" if the path is absent — a black-box read of what actually landed on the
// remote.
func originFileContent(t *testing.T, origin, path string) string {
	t.Helper()
	verify := filepath.Join(t.TempDir(), "verify")
	gitAt(t, filepath.Dir(verify), "clone", origin, verify)
	gitAt(t, verify, "checkout", "main")
	b, err := os.ReadFile(filepath.Join(verify, path))
	if err != nil {
		return ""
	}
	return string(b)
}

// TestPushRejectRebaseRealGit drives the whole recovery against a real moved
// remote: the clone is diverged (origin advanced to v2; the clone has its own
// local commit), so a plain push is rejected non-fast-forward. Choosing rebase
// must replay the local commit onto v2 and push, leaving BOTH on origin.
func TestPushRejectRebaseRealGit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone := filepath.Join(root, "clone")

	// origin seeded with v1 on main.
	gitAt(t, root, "init", "--bare", origin)
	gitAt(t, root, "clone", origin, seed)
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v1\n"), 0o644)
	gitAt(t, seed, "checkout", "-b", "main")
	gitAt(t, seed, "add", ".")
	gitAt(t, seed, "commit", "-m", "v1")
	gitAt(t, seed, "push", "-u", "origin", "main")

	// Clone, then the clone needs a persistent identity for the rebase the op
	// runs through ExecRunner (gitAt's env does not reach that subprocess).
	gitAt(t, root, "clone", origin, clone)
	gitAt(t, clone, "checkout", "main")
	gitAt(t, clone, "config", "user.name", "t")
	gitAt(t, clone, "config", "user.email", "t@t")

	// Origin advances to v2 (the "remote moved" commits).
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v2\n"), 0o644)
	gitAt(t, seed, "add", ".")
	gitAt(t, seed, "commit", "-m", "v2")
	gitAt(t, seed, "push", "origin", "main")

	// The clone makes its own commit → diverged from origin/main.
	os.WriteFile(filepath.Join(clone, "local.txt"), []byte("mine\n"), 0o644)
	gitAt(t, clone, "add", ".")
	gitAt(t, clone, "commit", "-m", "local work")

	repo := &git.Repo{Runner: gitexec.NewExecRunner("git", clone, observ.NewRing(50))}
	res, err := Push{Remote: "origin", Branch: "main", SetUpstream: true}.Run(
		context.Background(), OpDeps{Repo: repo, Decider: MapDecider{"push-rejected": "rebase"}})
	if err != nil {
		t.Fatalf("push (rebase recovery): %v", err)
	}
	if res.Summary != "rebased and pushed" {
		t.Fatalf("res.Summary = %q, want \"rebased and pushed\"", res.Summary)
	}

	// Origin must now carry BOTH the remote's v2 and the clone's local commit.
	if got := originFileContent(t, origin, "f.txt"); got != "v2\n" {
		t.Fatalf("origin f.txt = %q, want the rebased-onto v2", got)
	}
	if got := originFileContent(t, origin, "local.txt"); got != "mine\n" {
		t.Fatalf("origin local.txt = %q, want the replayed local commit \"mine\\n\"", got)
	}

	// The clone's working tree is clean after a successful rebase + push.
	if dirty, derr := repo.IsDirty(context.Background()); derr != nil || dirty {
		t.Fatalf("clone dirty=%v err=%v, want a clean tree", dirty, derr)
	}
}
