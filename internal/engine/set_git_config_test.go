package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/git"
)

func TestSetGitConfigWritesLocalScope(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	_, repo := newRepo(t)
	ctx := context.Background()

	res, err := SetGitConfig{Key: "fetch.writeCommitGraph", Value: "true"}.Run(ctx, OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	v, set, _ := repo.ConfigGet(ctx, git.ConfigLocal, "fetch.writeCommitGraph")
	if !set || v != "true" {
		t.Fatalf("local fetch.writeCommitGraph = %q set=%v, want true", v, set)
	}
	if _, gset, _ := repo.ConfigGet(ctx, git.ConfigGlobal, "fetch.writeCommitGraph"); gset {
		t.Fatal("global was written; expected local-only")
	}
}

func TestSetGitConfigWritesGlobalScope(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	_, repo := newRepo(t)
	ctx := context.Background()

	if _, err := (SetGitConfig{Key: "fetch.writeCommitGraph", Value: "true", Global: true}).Run(ctx, OpDeps{Repo: repo}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, gset, _ := repo.ConfigGet(ctx, git.ConfigGlobal, "fetch.writeCommitGraph"); !gset {
		t.Fatal("global fetch.writeCommitGraph not set")
	}
	if _, lset, _ := repo.ConfigGet(ctx, git.ConfigLocal, "fetch.writeCommitGraph"); lset {
		t.Fatal("local was written; expected global-only")
	}
}

func TestSetGitConfigRequiresKey(t *testing.T) {
	_, repo := newRepo(t)
	if _, err := (SetGitConfig{Value: "x"}).Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("empty key must error")
	}
}
