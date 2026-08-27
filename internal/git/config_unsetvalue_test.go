package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// ConfigUnsetValue removes exactly ONE value of a multivar by fixed value —
// sibling values survive (unlike ConfigUnset, which removes them all) — and a
// value that is not present is a no-op success.
func TestConfigUnsetValueRemovesOnlyThatValue(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	keep := "+refs/heads/main:refs/remotes/origin/main"
	drop := "+refs/heads/gone:refs/remotes/origin/gone"
	if err := repo.ConfigAdd(ctx, ConfigLocal, "remote.origin.fetch", keep); err != nil {
		t.Fatalf("add keep: %v", err)
	}
	if err := repo.ConfigAdd(ctx, ConfigLocal, "remote.origin.fetch", drop); err != nil {
		t.Fatalf("add drop: %v", err)
	}

	if err := repo.ConfigUnsetValue(ctx, ConfigLocal, "remote.origin.fetch", drop); err != nil {
		t.Fatalf("unset value: %v", err)
	}
	vals, err := repo.ConfigGetAll(ctx, "remote.origin.fetch")
	if err != nil || len(vals) != 1 || vals[0] != keep {
		t.Fatalf("vals=%v err=%v, want only %q", vals, err, keep)
	}

	// Absent value: idempotent no-op, like ConfigUnset.
	if err := repo.ConfigUnsetValue(ctx, ConfigLocal, "remote.origin.fetch", drop); err != nil {
		t.Fatalf("unset absent value: %v", err)
	}
}
