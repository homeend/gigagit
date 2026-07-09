package domain

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitIn runs git in dir, failing the test on error (mirrors newRealRepo's run).
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeIn(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Two diverged branches: A changed a.txt (and renamed r-old.txt), B changed
// b.txt and also a shared.txt. Origin sets must attribute paths to the branch
// that touched them since the merge base, including both rename sides.
func TestCompareOriginsAttributesPaths(t *testing.T) {
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	// Base: add r-old.txt and shared.txt on main.
	writeIn(t, dir, "r-old.txt", "rename me\nlots of stable content\nso git sees a rename\n")
	writeIn(t, dir, "shared.txt", "shared\n")
	gitIn(t, dir, "add", "r-old.txt", "shared.txt")
	gitIn(t, dir, "commit", "-m", "base files")

	// Branch A: change a.txt, rename r-old.txt -> r-new.txt.
	gitIn(t, dir, "checkout", "-b", "feat/a")
	writeIn(t, dir, "a.txt", "a\n")
	gitIn(t, dir, "mv", "r-old.txt", "r-new.txt")
	gitIn(t, dir, "add", "a.txt")
	gitIn(t, dir, "commit", "-m", "a work")

	// Branch B (from main): change b.txt and shared.txt.
	gitIn(t, dir, "checkout", "main")
	gitIn(t, dir, "checkout", "-b", "feat/b")
	writeIn(t, dir, "b.txt", "b\n")
	writeIn(t, dir, "shared.txt", "shared, changed by b\n")
	gitIn(t, dir, "add", "b.txt", "shared.txt")
	gitIn(t, dir, "commit", "-m", "b work")

	got, err := svc.CompareOrigins(ctx, "feat/a", "feat/b")
	if err != nil {
		t.Fatalf("CompareOrigins: %v", err)
	}
	for _, p := range []string{"a.txt", "r-old.txt", "r-new.txt"} {
		if !got.APaths[p] {
			t.Errorf("APaths missing %q (have %v)", p, got.APaths)
		}
	}
	for _, p := range []string{"b.txt", "shared.txt"} {
		if !got.BPaths[p] {
			t.Errorf("BPaths missing %q (have %v)", p, got.BPaths)
		}
	}
	if got.APaths["b.txt"] || got.APaths["shared.txt"] {
		t.Errorf("APaths contains B-only paths: %v", got.APaths)
	}
	if got.BPaths["a.txt"] {
		t.Errorf("BPaths contains A-only path a.txt: %v", got.BPaths)
	}
}

// Unrelated histories (orphan branch) have no merge base: the typed sentinel
// lets the TUI show "filter unavailable" without string matching.
func TestCompareOriginsNoMergeBase(t *testing.T) {
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	gitIn(t, dir, "checkout", "--orphan", "orphan")
	writeIn(t, dir, "o.txt", "o\n")
	gitIn(t, dir, "add", "o.txt")
	// The orphan index still holds README.md from main; commit everything.
	gitIn(t, dir, "commit", "-m", "orphan root")

	_, err := svc.CompareOrigins(ctx, "main", "orphan")
	if !errors.Is(err, ErrNoMergeBase) {
		t.Fatalf("err = %v, want ErrNoMergeBase", err)
	}
}
