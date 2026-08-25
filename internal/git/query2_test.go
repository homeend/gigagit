package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRevParseAndCommitMessage(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	// A second commit with a multi-line message.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit(context.Background(), "second\n\nbody line", true, false); err != nil {
		t.Fatalf("commit: %v", err)
	}

	head, err := repo.RevParse(context.Background(), "HEAD")
	if err != nil || len(head) < 7 {
		t.Fatalf("rev-parse HEAD: %v %q", err, head)
	}
	if _, err := repo.RevParse(context.Background(), "HEAD~5"); err == nil {
		t.Fatal("want error for an out-of-range revision")
	}
	// Root commit has no parent: rev-parse <root>^ must error (root detection).
	if _, err := repo.RevParse(context.Background(), "HEAD~1"); err != nil {
		t.Fatalf("rev-parse HEAD~1 should resolve: %v", err)
	}

	msg, err := repo.CommitMessage(context.Background(), "HEAD")
	if err != nil || !strings.Contains(msg, "body line") || !strings.Contains(msg, "second") {
		t.Fatalf("commit message: %v %q", err, msg)
	}
}
