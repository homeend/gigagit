package git

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gigagit/gg/internal/gitexec"
)

func TestWriteCommitGraph(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	if err := repo.WriteCommitGraph(context.Background()); err != nil {
		t.Fatalf("WriteCommitGraph: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "objects", "info", "commit-graph")); err != nil {
		t.Fatalf("commit-graph not written: %v", err)
	}
}

func TestLogScopedDateOrderFlag(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: ""})
	repo := &Repo{Runner: f}

	repo.LogScoped(context.Background(), 10, 0, LogScope{}, true)
	if !slices.Contains(f.Calls[len(f.Calls)-1].Argv, "--date-order") {
		t.Error("dateOrder=true must include --date-order")
	}
	f.Calls = nil
	repo.LogScoped(context.Background(), 10, 0, LogScope{}, false)
	if slices.Contains(f.Calls[len(f.Calls)-1].Argv, "--date-order") {
		t.Error("dateOrder=false must omit --date-order")
	}
}
