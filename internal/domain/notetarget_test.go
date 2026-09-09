package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
)

// targetRepo has one tracked-and-modified file and one untracked file, so both
// default-state rows of the §4.5 target table are reachable.
func targetRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gittest.Run(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "add", "a.txt")
	gittest.Run(t, dir, "commit", "-m", "seed")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fresh.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestNoteTargetStateTable(t *testing.T) {
	t.Parallel()
	dir := targetRepo(t)
	svc := svcIn(t, dir)
	ctx := context.Background()

	addr, err := svc.NoteTarget(ctx, "a.txt", false, "")
	if err != nil || addr.State != model.StateUnstaged || addr.Path != "a.txt" {
		t.Fatalf("default = %+v %v, want StateUnstaged a.txt", addr, err)
	}
	addr, err = svc.NoteTarget(ctx, "fresh.txt", false, "")
	if err != nil || addr.State != model.StateUntracked {
		t.Fatalf("untracked path = %+v %v, want StateUntracked", addr, err)
	}
	addr, err = svc.NoteTarget(ctx, "a.txt", true, "")
	if err != nil || addr.State != model.StateStaged {
		t.Fatalf("--cached = %+v %v, want StateStaged", addr, err)
	}
	addr, err = svc.NoteTarget(ctx, "a.txt", false, "HEAD")
	if err != nil {
		t.Fatalf("--rev HEAD: %v", err)
	}
	if addr.State != model.StateCommitted || len(addr.Commit) != 40 {
		t.Fatalf("--rev = %+v, want StateCommitted with a FULL sha", addr)
	}
}

func TestNoteTargetUsageErrors(t *testing.T) {
	t.Parallel()
	dir := targetRepo(t)
	svc := svcIn(t, dir)
	ctx := context.Background()
	for _, c := range []struct {
		name   string
		path   string
		cached bool
		rev    string
		want   string
	}{
		{"range refused", "a.txt", false, "main..HEAD", "a note anchors to one commit; pass the tip commit"},
		{"triple-dot range refused", "a.txt", false, "main...HEAD", "a note anchors to one commit; pass the tip commit"},
		{"cached with rev", "a.txt", true, "HEAD", "--cached and --rev are mutually exclusive"},
		{"no path", "", false, "", "a note needs a file path"},
		{"escaping path", "../outside.txt", false, "", "path escapes the repository"},
	} {
		_, err := svc.NoteTarget(ctx, c.path, c.cached, c.rev)
		if !errors.Is(err, ErrNoteTargetUsage) {
			t.Errorf("%s: err = %v, want ErrNoteTargetUsage", c.name, err)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %q, want it to contain %q", c.name, err, c.want)
		}
	}
	// An unknown rev is a FAILURE (exit 1), not a usage error (exit 2).
	if _, err := svc.NoteTarget(ctx, "a.txt", false, "no-such-rev"); err == nil || errors.Is(err, ErrNoteTargetUsage) {
		t.Fatalf("unknown rev err = %v, want a non-usage error", err)
	}
}

func TestNoteTargetNormalisesPathNotation(t *testing.T) {
	t.Parallel()
	dir := targetRepo(t)
	svc := svcIn(t, dir)
	addr, err := svc.NoteTarget(context.Background(), "./a.txt", false, "")
	if err != nil || addr.Path != "a.txt" {
		t.Fatalf("Path = %q (%v), want the cleaned git slash form a.txt", addr.Path, err)
	}
}
