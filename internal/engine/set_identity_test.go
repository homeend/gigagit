package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/git"
)

func TestSetIdentityWritesLocalScope(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	_, repo := newRepo(t)
	ctx := context.Background()

	res, err := SetIdentity{Name: "Ada L", Email: "ada@local"}.Run(ctx, OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	n, set, _ := repo.ConfigGet(ctx, git.ConfigLocal, "user.name")
	e, _, _ := repo.ConfigGet(ctx, git.ConfigLocal, "user.email")
	if !set || n != "Ada L" || e != "ada@local" {
		t.Fatalf("local identity = %q <%q> set=%v", n, e, set)
	}
	// Global must remain untouched.
	if _, gset, _ := repo.ConfigGet(ctx, git.ConfigGlobal, "user.name"); gset {
		t.Fatal("global was written; expected local-only")
	}
}

func TestSetIdentityWritesGlobalScope(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	_, repo := newRepo(t)
	ctx := context.Background()

	if _, err := (SetIdentity{Name: "G", Email: "g@x", Global: true}).Run(ctx, OpDeps{Repo: repo}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, gset, _ := repo.ConfigGet(ctx, git.ConfigGlobal, "user.name"); !gset {
		t.Fatal("global user.name not set")
	}
	if _, lset, _ := repo.ConfigGet(ctx, git.ConfigLocal, "user.name"); lset {
		t.Fatal("local was written; expected global-only")
	}
}
