package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// ModifiedIgnoringEOL must omit a file whose only working-tree change is line
// endings (LF→CRLF) while keeping a file with a genuine content edit.
//
// core.autocrlf is forced false and no .gitattributes exists, so git applies NO
// line-ending normalization of its own: plain `git diff` reports BOTH files as
// modified, and --ignore-cr-at-eol is the only thing that suppresses the
// EOL-only one. Without this isolation the test would pass even with the flag
// removed (ambient autocrlf would mask the CRLF file), proving nothing.
func TestModifiedIgnoringEOL(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	gitRun(t, dir, "config", "core.autocrlf", "false")
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Commit both with LF endings.
	write("crlf.txt", "line1\nline2\nline3\n")
	write("real.txt", "alpha\nbeta\n")
	gitRun(t, dir, "add", "crlf.txt", "real.txt")
	gitRun(t, dir, "commit", "-m", "add files")

	// crlf.txt: convert to CRLF (EOL-only). real.txt: a true content edit.
	write("crlf.txt", "line1\r\nline2\r\nline3\r\n")
	write("real.txt", "alpha\nBETA\n")

	got, err := repo.ModifiedIgnoringEOL(ctx, []string{"crlf.txt", "real.txt"})
	if err != nil {
		t.Fatalf("ModifiedIgnoringEOL: %v", err)
	}
	if len(got) != 1 || got[0] != "real.txt" {
		t.Fatalf("got %v, want [real.txt] (crlf.txt differs only by line endings)", got)
	}
}

// Empty input must not shell out to git at all (and returns no paths).
func TestModifiedIgnoringEOLNoPaths(t *testing.T) {
	t.Parallel()
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	got, err := repo.ModifiedIgnoringEOL(context.Background(), nil)
	if err != nil {
		t.Fatalf("ModifiedIgnoringEOL(nil): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want empty", got)
	}
}
