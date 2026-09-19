package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitParents(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()

	root, err := r.CommitParents(ctx, "HEAD")
	if err != nil || len(root) != 0 {
		t.Fatalf("root commit parents = %v, %v; want none", root, err)
	}

	head, _ := r.ResolveCommit(ctx, "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("u\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "stash", "push", "-u", "-m", "wip")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("stash: %v\n%s", err, out)
	}
	ps, err := r.CommitParents(ctx, "stash@{0}")
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 3 || ps[0] != head {
		t.Fatalf("-u stash parents = %v, want 3 with the first = %s", ps, head)
	}
	for _, p := range ps {
		if len(p) < 40 || strings.ContainsAny(p, "@{}") {
			t.Errorf("parent %q is not a full sha", p)
		}
	}
	if up, _ := r.CommitParents(ctx, ps[2]); len(up) != 0 {
		t.Errorf("the untracked commit must be a root; parents = %v", up)
	}
	if _, err := r.CommitParents(ctx, "nosuchrev"); err == nil {
		t.Error("an unknown rev must error")
	}
}
