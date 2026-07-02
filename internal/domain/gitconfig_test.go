package domain

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/git"
)

func TestGitConfigRowsMergesCatalogAndValues(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	_, svc := newRealRepo(t)
	ctx := context.Background()
	// fetch.writeCommitGraph is camelCase in the catalog; git stores the set
	// key lowercased — the merge must join them case-insensitively.
	if err := svc.Repo().ConfigSet(ctx, git.ConfigLocal, "fetch.writeCommitGraph", "true"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Repo().ConfigSet(ctx, git.ConfigGlobal, "user.name", "Global Person"); err != nil {
		t.Fatal(err)
	}

	rows, err := svc.GitConfigRows(ctx)
	if err != nil {
		t.Fatalf("GitConfigRows: %v", err)
	}
	if len(rows) < 100 {
		t.Fatalf("expected the whole catalog, got %d rows", len(rows))
	}
	byKey := map[string]int{}
	for i, r := range rows {
		byKey[r.Key] = i
	}
	i, ok := byKey["fetch.writeCommitGraph"] // display form = catalog camelCase
	if !ok {
		t.Fatal("catalog key fetch.writeCommitGraph missing (case-insensitive merge broken?)")
	}
	if r := rows[i]; !r.LocalSet || r.LocalValue != "true" || r.GlobalSet {
		t.Fatalf("fetch.writeCommitGraph row = %+v, want local true / global unset", r)
	}
	if r := rows[byKey["user.name"]]; !r.GlobalSet || r.GlobalValue != "Global Person" || r.LocalSet {
		t.Fatalf("user.name row = %+v, want global set / local unset", r)
	}
	// A catalog key never set anywhere: both scopes unset.
	if r := rows[byKey["add.ignoreErrors"]]; r.LocalSet || r.GlobalSet {
		t.Fatalf("add.ignoreErrors row = %+v, want both unset", r)
	}
}

func TestGitConfigRowsAppendsNonCatalogKeys(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	_, svc := newRealRepo(t)
	ctx := context.Background()
	if err := svc.Repo().ConfigSet(ctx, git.ConfigLocal, "alias.lg", "log --graph"); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.GitConfigRows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.Key == "alias.lg" {
			found = true
			if !r.LocalSet || r.LocalValue != "log --graph" {
				t.Fatalf("alias.lg row = %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("a set key outside the catalog must still get a row")
	}
}
