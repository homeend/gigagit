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

func TestConfigAddAndGetAllMultivar(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	// Missing key: empty, no error (the ConfigGet exit-1 pattern).
	vals, err := repo.ConfigGetAll(ctx, "remote.origin.fetch")
	if err != nil || len(vals) != 0 {
		t.Fatalf("missing key: vals=%v err=%v", vals, err)
	}

	if err := repo.ConfigAdd(ctx, ConfigLocal, "remote.origin.fetch", "+refs/heads/main:refs/remotes/origin/main"); err != nil {
		t.Fatalf("add 1: %v", err)
	}
	if err := repo.ConfigAdd(ctx, ConfigLocal, "remote.origin.fetch", "+refs/heads/feat:refs/remotes/origin/feat"); err != nil {
		t.Fatalf("add 2: %v", err)
	}
	vals, err = repo.ConfigGetAll(ctx, "remote.origin.fetch")
	if err != nil {
		t.Fatalf("get-all: %v", err)
	}
	want := []string{
		"+refs/heads/main:refs/remotes/origin/main",
		"+refs/heads/feat:refs/remotes/origin/feat",
	}
	if len(vals) != 2 || vals[0] != want[0] || vals[1] != want[1] {
		t.Fatalf("get-all = %v, want %v", vals, want)
	}
}

func TestConfigGetRegexp(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	// No match: empty, no error.
	kvs, err := repo.ConfigGetRegexp(ctx, `^branch\.`)
	if err != nil || len(kvs) != 0 {
		t.Fatalf("no match: kvs=%v err=%v", kvs, err)
	}

	if err := repo.ConfigSet(ctx, ConfigLocal, "branch.feat.remote", "origin"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ConfigSet(ctx, ConfigLocal, "branch.feat.merge", "refs/heads/feat"); err != nil {
		t.Fatal(err)
	}
	kvs, err = repo.ConfigGetRegexp(ctx, `^branch\.`)
	if err != nil || len(kvs) != 2 {
		t.Fatalf("kvs=%v err=%v", kvs, err)
	}
	// git lowercases section+key; value is verbatim.
	if kvs[0][0] != "branch.feat.remote" || kvs[0][1] != "origin" {
		t.Fatalf("kvs[0] = %v", kvs[0])
	}
	if kvs[1][0] != "branch.feat.merge" || kvs[1][1] != "refs/heads/feat" {
		t.Fatalf("kvs[1] = %v", kvs[1])
	}
}
