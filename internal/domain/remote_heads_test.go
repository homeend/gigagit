package domain

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

// narrowSvc builds a single-branch clone whose origin also holds branches the
// clone's fetch refspec does not cover, and returns a Service on the clone.
func narrowSvc(t *testing.T) *Service {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "bare.git")
	seed := filepath.Join(root, "seed")
	local := filepath.Join(root, "local")
	gitInDir(t, root, "init", "--bare", "-b", "main", bare)
	gitInDir(t, root, "clone", bare, seed)
	if err := os.WriteFile(filepath.Join(seed, "a.txt"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInDir(t, seed, "add", "-A")
	gitInDir(t, seed, "commit", "-m", "init")
	gitInDir(t, seed, "push", "origin", "main")
	gitInDir(t, seed, "branch", "hidden/a")
	gitInDir(t, seed, "branch", "hidden/b")
	gitInDir(t, seed, "push", "origin", "hidden/a", "hidden/b")
	gitInDir(t, root, "clone", "--single-branch", bare, local)
	repo := &git.Repo{Runner: gitexec.NewExecRunner("git", local, observ.NewRing(50))}
	return New(repo)
}

// UnfetchedRemoteHeads must list exactly the remote branches with no local
// remote-tracking ref — main is tracked by the narrow clone, the two hidden
// branches are not.
func TestUnfetchedRemoteHeads(t *testing.T) {
	t.Parallel()
	svc := narrowSvc(t)
	heads, err := svc.UnfetchedRemoteHeads(context.Background(), "origin")
	if err != nil {
		t.Fatalf("UnfetchedRemoteHeads: %v", err)
	}
	var names []string
	for _, h := range heads {
		if len(h.Hash) != 40 {
			t.Fatalf("head %q: hash %q, want full sha", h.Name, h.Hash)
		}
		names = append(names, h.Name)
	}
	if len(names) != 2 || names[0] != "hidden/a" || names[1] != "hidden/b" {
		t.Fatalf("names = %v, want [hidden/a hidden/b]", names)
	}
}
