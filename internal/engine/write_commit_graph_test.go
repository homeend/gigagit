package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteCommitGraphCreatesFile(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	res, err := WriteCommitGraph{}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "objects", "info", "commit-graph")); err != nil {
		t.Fatalf("commit-graph file missing: %v", err)
	}
}

func TestWriteCommitGraphEmitsProgressAndDone(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	events := make(chan Event, 16)
	if _, err := (WriteCommitGraph{}).Run(context.Background(), OpDeps{Repo: repo, Events: events}); err != nil {
		t.Fatalf("run: %v", err)
	}
	close(events)
	var sawProgress, sawDone bool
	for e := range events {
		switch e.(type) {
		case Progress:
			sawProgress = true
		case Done:
			sawDone = true
		}
	}
	if !sawProgress || !sawDone {
		t.Fatalf("events: progress=%v done=%v, want both", sawProgress, sawDone)
	}
}
