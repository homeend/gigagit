package git

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
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

// A commit is complete at its name-status line: the streamed history must not
// hold commit N back until git finds commit N+1 (seconds apart on a rarely
// touched file in a huge repo).
func TestFileLogParserEmitsAtStatusLine(t *testing.T) {
	t.Parallel()
	var p fileLogParser
	if _, ok := p.line("aaa\x1f\x1fAda\x1f1700000000\x1fedit"); ok {
		t.Fatal("a format line alone must not complete a commit")
	}
	fc, ok := p.line("M\ta.go")
	if !ok || fc.Hash != "aaa" || fc.Status != "M" || fc.Path != "a.go" || fc.Subject != "edit" {
		t.Fatalf("status line must complete the commit, got %+v ok=%v", fc, ok)
	}
	if _, ok := p.flush(); ok {
		t.Fatal("nothing is left open after a completed commit")
	}
	// A commit with no status line completes at the next format line / flush.
	if _, ok := p.line("bbb\x1f\x1fBob\x1f1690000000\x1fmerge"); ok {
		t.Fatal("format line must not complete")
	}
	fc, ok = p.line("ccc\x1f\x1fAda\x1f1680000000\x1finit")
	if !ok || fc.Hash != "bbb" || fc.Status != "" {
		t.Fatalf("next format line must complete the open commit, got %+v ok=%v", fc, ok)
	}
	fc, ok = p.flush()
	if !ok || fc.Hash != "ccc" {
		t.Fatalf("flush must return the open commit, got %+v ok=%v", fc, ok)
	}
}

// FileLogStream yields exactly what FileLog returns, in order, across a rename.
func TestFileLogStreamMatchesFileLog(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "one\ntwo\nthree\nfour\n")
	gitIn(t, dir, "add", "a.go")
	gitIn(t, dir, "commit", "-m", "add a.go")
	write("a.go", "one\ntwo\nthree\nfour\nfive\n")
	gitIn(t, dir, "commit", "-am", "edit a.go")
	gitIn(t, dir, "mv", "a.go", "b.go")
	gitIn(t, dir, "commit", "-m", "rename to b.go")
	write("b.go", "one\ntwo\nthree\nfour\nfive\nsix\n")
	gitIn(t, dir, "commit", "-am", "edit b.go")

	want, err := repo.FileLog(context.Background(), "", "b.go", 50)
	if err != nil {
		t.Fatal(err)
	}
	var got []model.FileCommit
	if err := repo.FileLogStream(context.Background(), "", "b.go", 50, func(fc model.FileCommit) {
		got = append(got, fc)
	}); err != nil {
		t.Fatal(err)
	}
	if len(want) != 4 {
		t.Fatalf("FileLog fixture: want 4 commits, got %d: %+v", len(want), want)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stream != FileLog\n got: %+v\nwant: %+v", got, want)
	}
	if got[1].Status != "R" || got[1].OldPath != "a.go" || got[1].Path != "b.go" {
		t.Errorf("rename commit wrong: %+v", got[1])
	}
}

// The limit bounds the stream exactly like FileLog's -n.
func TestFileLogStreamHonoursLimit(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	for i := 0; i < 3; i++ {
		if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(strings.Repeat("x\n", i+1)), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, dir, "add", "a.go")
		gitIn(t, dir, "commit", "-m", "c")
	}
	n := 0
	if err := repo.FileLogStream(context.Background(), "", "a.go", 2, func(model.FileCommit) { n++ }); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("limit 2 emitted %d commits", n)
	}
}
