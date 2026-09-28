package engine

import (
	"context"
	"errors"
	"testing"
)

func TestRepoAtNilIsATypedError(t *testing.T) {
	t.Parallel()
	_, err := OpDeps{}.repoAt("/x")
	if !errors.Is(err, ErrNoRepoAt) {
		t.Fatalf("err = %v, want ErrNoRepoAt", err)
	}
}

func TestRepoAtHandsBackTheView(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	deps := OpDeps{Repo: repo, RepoAt: func(dir string) GitOps { return repo.InDir(dir) }}
	view, err := deps.repoAt("/x")
	if err != nil || view == nil {
		t.Fatalf("repoAt = %v, %v", view, err)
	}
	if top, err := view.TopLevel(context.Background()); err != nil || top != "/x" {
		t.Fatalf("view.TopLevel = %q, %v; want /x (Root pinned by InDir)", top, err)
	}
}
