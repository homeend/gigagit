package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseFileLog(t *testing.T) {
	t.Parallel()
	// One commit per format line ("%H\x1f%P\x1f%an\x1f%at\x1f%s"), each
	// followed by its --name-status line for the followed file.
	data := "" +
		"aaa\x1fppp\x1fAda\x1f1700000000\x1fmodify auth\n" +
		"M\tsrc/auth.go\n" +
		"\n" +
		"bbb\x1fqqq\x1fBob\x1f1690000000\x1frename file\n" +
		"R100\tsrc/old.go\tsrc/auth.go\n" +
		"\n" +
		"ccc\x1f\x1fAda\x1f1680000000\x1finitial\n" +
		"A\tsrc/old.go\n"

	got := ParseFileLog([]byte(data))
	if len(got) != 3 {
		t.Fatalf("want 3 commits, got %d", len(got))
	}
	if got[0].Hash != "aaa" || got[0].Status != "M" || got[0].Path != "src/auth.go" {
		t.Errorf("commit 0 wrong: %+v", got[0])
	}
	if got[0].Author != "Ada" || got[0].UnixTime != 1700000000 || got[0].Subject != "modify auth" {
		t.Errorf("commit 0 metadata wrong: %+v", got[0])
	}
	if got[1].Status != "R" || got[1].OldPath != "src/old.go" || got[1].Path != "src/auth.go" {
		t.Errorf("rename commit wrong: %+v", got[1])
	}
	if got[2].Status != "A" || got[2].Path != "src/old.go" || len(got[2].Parents) != 0 {
		t.Errorf("root commit wrong: %+v", got[2])
	}
}

func TestFileLog(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t) // creates repo with initial commit (README.md)
	repo := &Repo{Runner: runner}

	// Commit 1: add a.go
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "a.go")
	gitIn(t, dir, "commit", "-m", "add a.go")

	// Commit 2: edit a.go
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "a.go")
	gitIn(t, dir, "commit", "-m", "edit a.go")

	// Commit 3: add b.go — unrelated; must NOT appear in FileLog for a.go
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "b.go")
	gitIn(t, dir, "commit", "-m", "add b.go")

	got, err := repo.FileLog(context.Background(), "", "a.go", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 commits touching a.go, got %d: %+v", len(got), got)
	}
	if got[0].Subject != "edit a.go" || got[1].Subject != "add a.go" {
		t.Errorf("order/newest-first wrong: %+v", got)
	}
	if got[0].Status != "M" || got[1].Status != "A" {
		t.Errorf("statuses wrong: %+v", got)
	}
}

// TestFileLogNonASCIIPathRoundTrip guards the file-history view's twin of the
// CommitFiles bug: a file followed across a rename into a non-ASCII name must
// surface as a raw UTF-8 path, not git's quoted "timing \342\200\224 …" form,
// or the history diff's ShowFile(fc.Hash, fc.Path) fails with exit 128.
func TestFileLogNonASCIIPathRoundTrip(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	if err := os.WriteFile(filepath.Join(dir, "orig.log"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "orig.log")
	gitIn(t, dir, "commit", "-m", "add orig.log")

	const name = "timing — kopia.log" // em-dash U+2014
	gitIn(t, dir, "mv", "orig.log", name)
	gitIn(t, dir, "commit", "-m", "rename to non-ascii")

	got, err := repo.FileLog(context.Background(), "", name, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatalf("expected history for %q", name)
	}
	newest := got[0]
	if newest.Status != "R" || newest.Path != name {
		t.Fatalf("newest = %+v, want R with raw path %q", newest, name)
	}
	// The reported bug, via the history entry point: this must succeed.
	if _, err := repo.ShowFile(context.Background(), newest.Hash, newest.Path); err != nil {
		t.Fatalf("ShowFile(%q) failed: %v", newest.Path, err)
	}
}
