package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Targeted: restore reverts a tracked edit; the file returns to HEAD content.
func TestDiscardRestoresTrackedEdit(t *testing.T) {
	dir, repo := newRepo(t)
	readme := filepath.Join(dir, "README.md")
	orig, _ := os.ReadFile(readme)
	os.WriteFile(readme, []byte("dirty\n"), 0o644)

	res, err := Discard{Restore: []string{"README.md"}}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("discard: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v", res)
	}
	b, _ := os.ReadFile(readme)
	if string(b) != string(orig) {
		t.Fatalf("README.md = %q, want original %q", b, orig)
	}
}

// Targeted: remove deletes an untracked new file.
func TestDiscardRemovesUntracked(t *testing.T) {
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)

	_, err := Discard{Remove: []string{"new.txt"}}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("discard: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new.txt should be gone, stat err = %v", err)
	}
}

// All: discards every unstaged change — both a tracked edit and an untracked file.
func TestDiscardAll(t *testing.T) {
	dir, repo := newRepo(t)
	readme := filepath.Join(dir, "README.md")
	orig, _ := os.ReadFile(readme)
	os.WriteFile(readme, []byte("dirty\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)

	res, err := Discard{All: true}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("discard all: %v", err)
	}
	if !res.Changed || res.Summary != "discarded" {
		t.Fatalf("result = %+v", res)
	}
	b, _ := os.ReadFile(readme)
	if string(b) != string(orig) {
		t.Fatalf("README.md not reverted: %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new.txt should be gone, stat err = %v", err)
	}
}

// Mixed targeted: restore + remove in one op.
func TestDiscardMixed(t *testing.T) {
	dir, repo := newRepo(t)
	readme := filepath.Join(dir, "README.md")
	orig, _ := os.ReadFile(readme)
	os.WriteFile(readme, []byte("dirty\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)

	_, err := Discard{Restore: []string{"README.md"}, Remove: []string{"new.txt"}}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("discard mixed: %v", err)
	}
	if b, _ := os.ReadFile(readme); string(b) != string(orig) {
		t.Fatalf("README.md not reverted: %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new.txt should be gone")
	}
}

// Partial failure: a clean error is surfaced, not swallowed.
func TestDiscardPartialFailureReturnsError(t *testing.T) {
	fr := &discardFakeRepo{cleanErr: true}
	_, err := Discard{Restore: []string{"a"}, Remove: []string{"b"}}.Run(context.Background(), OpDeps{Repo: fr})
	if err == nil {
		t.Fatal("expected error from clean failure")
	}
	if !strings.Contains(err.Error(), "clean") {
		t.Fatalf("error = %v, want clean", err)
	}
	if !fr.restoreCalled {
		t.Fatal("restore should still have been attempted")
	}
}

// discardFakeRepo exercises Discard's error handling without a real repo.
type discardFakeRepo struct {
	GitOps        // nil embed: only the two discard verbs are implemented
	restoreCalled bool
	cleanErr      bool
}

func (f *discardFakeRepo) RestoreWorktree(ctx context.Context, paths []string) error {
	f.restoreCalled = true
	return nil
}

func (f *discardFakeRepo) CleanUntracked(ctx context.Context, paths []string) error {
	if f.cleanErr {
		return errTestClean
	}
	return nil
}

var errTestClean = fmt.Errorf("boom")
