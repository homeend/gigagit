package domain

import (
	"context"
	"os"
	"path/filepath"
	"testing"

)

func TestWorkspacesPath(t *testing.T) {
	t.Parallel()
	if got := WorkspacesPath(""); got != "" {
		t.Fatalf("WorkspacesPath(\"\") = %q", got)
	}
	want := filepath.Join("/x", "gg", "workspaces.toml")
	if got := WorkspacesPath(filepath.Join("/x", "gg", "repos.toml")); got != want {
		t.Fatalf("WorkspacesPath = %q, want %q", got, want)
	}
}

// TestRepoMainPathFromLinkedWorktree: a service opened in a linked worktree
// reports the MAIN checkout — the member identity of the workspace spec.
func TestRepoMainPathFromLinkedWorktree(t *testing.T) {
	t.Parallel()
	main := t.TempDir()
	gitIn(t, main, "init", "-b", "main")
	os.WriteFile(filepath.Join(main, "a.txt"), []byte("1\n"), 0o644)
	gitIn(t, main, "add", ".")
	gitIn(t, main, "commit", "-m", "c1")
	linked := filepath.Join(t.TempDir(), "linked")
	gitIn(t, main, "worktree", "add", "-b", "feat", linked)

	wantMain, _ := filepath.EvalSymlinks(main)
	for _, dir := range []string{main, linked} {
		got, err := svcAt(dir).RepoMainPath(context.Background())
		if err != nil {
			t.Fatalf("RepoMainPath(%s): %v", dir, err)
		}
		gotEval, _ := filepath.EvalSymlinks(got)
		if gotEval != wantMain {
			t.Fatalf("RepoMainPath(%s) = %q, want %q", dir, got, wantMain)
		}
	}
}

// TestRepoMainPathBare: a bare repository's identity is the bare directory.
func TestRepoMainPathBare(t *testing.T) {
	t.Parallel()
	bare := filepath.Join(t.TempDir(), "r.git")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, bare, "init", "--bare")
	got, err := svcAt(bare).RepoMainPath(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	gotEval, _ := filepath.EvalSymlinks(got)
	wantEval, _ := filepath.EvalSymlinks(bare)
	if gotEval != wantEval {
		t.Fatalf("RepoMainPath(bare) = %q, want %q", got, bare)
	}
}
