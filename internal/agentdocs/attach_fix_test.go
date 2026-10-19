package agentdocs

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Review finding A: rewriting the only note on a path with a new span must
// keep it in the store.
func TestRewritingTheOnlyNoteWithANewSpanKeepsIt(t *testing.T) {
	t.Parallel()
	s, _, dir := attached(t)
	writeRaw(t, dir, "n-1.toml", "path = \"a.go\"\nstart = 2\nend = 2\nsummary = \"s\"\nlines = [\"b\"]\n")
	s.Reload(root)
	if s.NoteCount(root, "a.go") != 1 {
		t.Fatal("loaded")
	}
	time.Sleep(20 * time.Millisecond) // a new mtime
	writeRaw(t, dir, "n-1.toml", "path = \"a.go\"\nstart = 3\nend = 3\nsummary = \"s\"\nlines = [\"c\"]\n")
	s.Reload(root)
	n, ok := s.FindNote("n-1")
	if !ok || n.Start != 3 || s.NoteCount(root, "a.go") != 1 {
		t.Fatalf("after rewrite: %+v %v count=%d", n, ok, s.NoteCount(root, "a.go"))
	}
}

// Review finding B: an id is unique per directory, not per store. Two roots
// may hold the same id; a lookup or a delete by id alone must never reach
// the other worktree's file.
func TestSameIDInTwoRootsStaysApart(t *testing.T) {
	t.Parallel()
	s := New()
	defer s.Close()
	r1, r2 := "/r1", "/r2"
	d1, d2 := filepath.Join(t.TempDir(), "docs"), filepath.Join(t.TempDir(), "docs")
	for _, d := range []string{d1, d2} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		writeRaw(t, d, "summary.md", "+++\ntitle = \"S\"\n+++\nbody\n")
		writeRaw(t, d, "n.toml", "path = \"a.go\"\nstart = 1\nend = 1\nsummary = \"s\"\nlines = [\"x\"]\n")
	}
	if err := s.Attach(r1, t.TempDir(), d1); err != nil {
		t.Fatal(err)
	}
	if err := s.Attach(r2, t.TempDir(), d2); err != nil {
		t.Fatal(err)
	}
	if len(s.Overviews(r1)) != 1 || len(s.Overviews(r2)) != 1 || s.NoteCount(r1, "a.go") != 1 || s.NoteCount(r2, "a.go") != 1 {
		t.Fatalf("each root shows its own: %d %d %d %d", len(s.Overviews(r1)), len(s.Overviews(r2)), s.NoteCount(r1, "a.go"), s.NoteCount(r2, "a.go"))
	}
	if o, ok := s.OverviewIn(r2, "summary"); !ok || o.Root != r2 {
		t.Fatalf("OverviewIn = %+v %v", o, ok)
	}
	if n, ok := s.FindNoteIn(r1, "n"); !ok || n.Root != r1 {
		t.Fatalf("FindNoteIn = %+v %v", n, ok)
	}
	// By id alone the document is ambiguous: nothing is touched.
	if _, ok := s.Overview("summary"); ok {
		t.Fatal("an ambiguous id must not resolve by itself")
	}
	if s.RemoveOverview("summary") || s.RemoveNote("n") {
		t.Fatal("an ambiguous delete must refuse")
	}
	for _, d := range []string{d1, d2} {
		if _, err := os.Stat(filepath.Join(d, "summary.md")); err != nil {
			t.Fatalf("a refused delete unlinked %s", d)
		}
	}
	// Scoped by root, only that worktree's file goes.
	if !s.RemoveOverviewIn(r2, "summary") {
		t.Fatal("RemoveOverviewIn")
	}
	if _, err := os.Stat(filepath.Join(d1, "summary.md")); err != nil {
		t.Fatal("r1's file must stay")
	}
	if _, err := os.Stat(filepath.Join(d2, "summary.md")); err == nil {
		t.Fatal("r2's file must be gone")
	}
	// Once unique, the id resolves alone again.
	if o, ok := s.Overview("summary"); !ok || o.Root != r1 {
		t.Fatalf("unique id = %+v %v", o, ok)
	}
}

// Review finding C: a poll that listed the directory before a write landed
// must not undo the write's own reload.
func TestConcurrentReloadsLoseNothing(t *testing.T) {
	old := pollEvery
	pollEvery = time.Millisecond
	t.Cleanup(func() { pollEvery = old })
	wt := t.TempDir()
	dir := filepath.Join(t.TempDir(), "docs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 150; i++ {
		writeRaw(t, dir, fmt.Sprintf("ov-%03d.md", i), okOverview)
	}
	s := New()
	s.noWatch = true
	if err := s.Attach(root, wt, dir); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 60; i++ {
		n, err := s.AddNoteMeta(root, "a.go", []string{"a", "b"}, 1, 1, NoteText{Summary: "s"})
		if err != nil || n.ID == "" {
			t.Fatalf("add %d: %+v %v", i, n, err)
		}
		if _, ok := s.FindNote(n.ID); !ok {
			t.Fatalf("add %d: the new note is not in the store", i)
		}
		if !s.RemoveNote(n.ID) {
			t.Fatalf("remove %d refused", i)
		}
		if _, err := os.Stat(filepath.Join(dir, n.ID+".toml")); err == nil {
			t.Fatalf("remove %d left the file", i)
		}
	}
}

// Review finding D: attaching a root drops its memory documents — they have
// no file and could never be deleted afterwards.
func TestAttachDropsMemoryDocuments(t *testing.T) {
	t.Parallel()
	s := New()
	defer s.Close()
	wt := t.TempDir()
	if _, err := s.AddNote(root, "a.go", []string{"a"}, 1, 1, "s", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddOverview(root, wt, "T", "body\n"); err != nil {
		t.Fatal(err)
	}
	if err := s.Attach(root, wt, filepath.Join(t.TempDir(), "docs")); err != nil {
		t.Fatal(err)
	}
	if s.NoteCount(root, "a.go") != 0 || len(s.Overviews(root)) != 0 {
		t.Fatal("memory documents must go when the root is attached")
	}
}

// Review finding F: a reload reads only entries whose size or mtime changed.
// A file whose bytes change under an unchanged stamp is not re-parsed (the
// spec's "size or mtime, then bytes").
func TestReloadParsesOnlyChangedEntries(t *testing.T) {
	t.Parallel()
	// Poll only: a watcher would reload between the write and the Chtimes.
	wt := t.TempDir()
	dir := filepath.Join(t.TempDir(), "docs")
	s := New()
	s.noWatch = true
	if err := s.Attach(root, wt, dir); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	text := "+++\ntitle = \"Same\"\n+++\nbody\n"
	writeRaw(t, dir, "ov-1.md", text)
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(dir, "ov-1.md"), stamp, stamp); err != nil {
		t.Fatal(err)
	}
	s.Reload(root)
	if o, ok := s.Overview("ov-1"); !ok || o.Title != "Same" {
		t.Fatal("loaded")
	}
	// Same size, same mtime, different (and now invalid) bytes.
	garbage := make([]byte, len(text))
	for i := range garbage {
		garbage[i] = 'x'
	}
	writeRaw(t, dir, "ov-1.md", string(garbage))
	if err := os.Chtimes(filepath.Join(dir, "ov-1.md"), stamp, stamp); err != nil {
		t.Fatal(err)
	}
	s.Reload(root)
	if o, ok := s.Overview("ov-1"); !ok || o.Title != "Same" {
		t.Fatalf("an unchanged stamp must not be re-read: %+v %v", o, ok)
	}
	if len(s.Problems(root)) != 0 {
		t.Fatalf("problems = %+v", s.Problems(root))
	}
}
