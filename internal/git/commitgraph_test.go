package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestCommitGraphWriteArgv(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git commit-graph write", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.CommitGraphWrite(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.Calls[0].Argv, " "); got != "commit-graph write --reachable" {
		t.Fatalf("argv = %q, want 'commit-graph write --reachable'", got)
	}
}

func TestCommitGraphWriteRealRepoCreatesFile(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	writeFile(t, dir, "a.txt", "hello\n")
	commitAll(t, dir, "one commit")

	if err := repo.CommitGraphWrite(context.Background(), nil); err != nil {
		t.Fatalf("CommitGraphWrite: %v", err)
	}
	cg := filepath.Join(dir, ".git", "objects", "info", "commit-graph")
	if _, err := os.Stat(cg); err != nil {
		t.Fatalf("commit-graph file not written at %s: %v", cg, err)
	}
}
