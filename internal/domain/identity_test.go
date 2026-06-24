package domain

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/git"
)

func TestIdentityDistinguishesScopes(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	// No local identity set → LocalSet false.
	id, err := svc.Identity(ctx)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	if id.LocalSet {
		t.Fatalf("expected no local identity, got %q", id.LocalName)
	}

	// Set local + global and confirm they are kept distinct.
	if err := svc.Repo().ConfigSet(ctx, git.ConfigGlobal, "user.name", "Global Person"); err != nil {
		t.Fatalf("set global: %v", err)
	}
	if err := svc.Repo().ConfigSet(ctx, git.ConfigLocal, "user.email", "local@x"); err != nil {
		t.Fatalf("set local: %v", err)
	}
	_ = dir

	id, err = svc.Identity(ctx)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	if !id.GlobalSet || id.GlobalName != "Global Person" {
		t.Fatalf("global = %q set=%v", id.GlobalName, id.GlobalSet)
	}
	if !id.LocalSet || id.LocalEmail != "local@x" {
		t.Fatalf("local = %q set=%v", id.LocalEmail, id.LocalSet)
	}
}
