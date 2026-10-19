package agentdocs

import (
	"os"
	"path/filepath"
	"testing"
)

// Two attached worktrees hold the same ids: the root-scoped stamp and note
// text read exactly their worktree's document; the id-only forms find
// nobody (ambiguous).
func TestStampAndNoteTextAreRootScoped(t *testing.T) {
	t.Parallel()
	s := New()
	defer s.Close()
	wtA, wtB := t.TempDir(), t.TempDir()
	dirA, dirB := filepath.Join(t.TempDir(), "docs"), filepath.Join(t.TempDir(), "docs")
	for i, d := range []string{dirA, dirB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		letter := string(rune('A' + i))
		writeRaw(t, d, "same.md", "+++\ntitle = \"T"+letter+"\"\n+++\nbody\n")
		writeRaw(t, d, "n.toml", "format = 1\npath = \"a.go\"\nstart = 1\nend = 1\nsummary = \"s\"\nlines = [\"line "+letter+"\"]\n")
	}
	if err := s.Attach("A", wtA, dirA); err != nil {
		t.Fatal(err)
	}
	if err := s.Attach("B", wtB, dirB); err != nil {
		t.Fatal(err)
	}
	s.Align("A", "a.go", []string{"line A"})
	s.Align("B", "a.go", []string{"line B"})
	if s.OverviewStampIn("A", "same") == "" || s.OverviewStampIn("B", "same") == "" {
		t.Fatal("a held overview has a stamp")
	}
	if s.OverviewStampIn("A", "same") == s.OverviewStampIn("B", "same") {
		t.Fatal("two different documents must not share a stamp")
	}
	if s.OverviewStamp("same") != "" {
		t.Fatal("an ambiguous id must stamp nothing")
	}
	if got := s.NoteTextIn("B", "n"); len(got) != 1 || got[0] != "line B" {
		t.Fatalf("NoteTextIn(B) = %q", got)
	}
	if got := s.NoteText("n"); got != nil {
		t.Fatalf("an ambiguous id must read nothing, got %q", got)
	}
	if got := s.NoteTextIn("A", "zz"); got != nil {
		t.Fatalf("unknown id reads nothing, got %q", got)
	}
}
