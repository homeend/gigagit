package git

import (
	"context"
	"path/filepath"
	"testing"
)

func TestTopLevelReturnsRepoRoot(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	got, err := repo.TopLevel(context.Background())
	if err != nil {
		t.Fatalf("TopLevel: %v", err)
	}
	// git may resolve symlinks (e.g. /var -> /private/var on macOS); compare by
	// resolving both sides.
	wantResolved, _ := filepath.EvalSymlinks(dir)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != wantResolved {
		t.Fatalf("TopLevel = %q, want %q", gotResolved, wantResolved)
	}
}
