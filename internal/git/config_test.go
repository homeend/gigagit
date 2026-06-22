package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigGetLocalDistinctFromGlobal(t *testing.T) {
	// Isolate global so the write below cannot touch the real ~/.gitconfig.
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	dir, runner := newTestRepo(t)
	_ = dir
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	// Nothing set locally yet.
	if _, set, err := repo.ConfigGet(ctx, ConfigLocal, "user.name"); err != nil || set {
		t.Fatalf("local unset: set=%v err=%v", set, err)
	}

	if err := repo.ConfigSet(ctx, ConfigGlobal, "user.name", "Global Person"); err != nil {
		t.Fatalf("set global: %v", err)
	}
	if err := repo.ConfigSet(ctx, ConfigLocal, "user.name", "Local Person"); err != nil {
		t.Fatalf("set local: %v", err)
	}

	g, gset, _ := repo.ConfigGet(ctx, ConfigGlobal, "user.name")
	l, lset, _ := repo.ConfigGet(ctx, ConfigLocal, "user.name")
	e, _, _ := repo.ConfigGet(ctx, ConfigEffective, "user.name")
	if !gset || g != "Global Person" {
		t.Fatalf("global = %q set=%v", g, gset)
	}
	if !lset || l != "Local Person" {
		t.Fatalf("local = %q set=%v", l, lset)
	}
	if e != "Local Person" { // effective prefers local
		t.Fatalf("effective = %q, want Local Person", e)
	}
}
