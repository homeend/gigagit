package agentdocs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A note file's created stamp is kept on the loaded note; a replaced file
// with a new stamp updates it.
func TestANoteKeepsItsCreatedStamp(t *testing.T) {
	t.Parallel()
	s := New()
	defer s.Close()
	wt, dir := t.TempDir(), filepath.Join(t.TempDir(), "docs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRaw(t, dir, "n.toml", "format = 1\npath = \"a.go\"\nstart = 1\nend = 1\nsummary = \"s\"\ncreated = 2026-01-02T03:04:05Z\nlines = [\"x\"]\n")
	if err := s.Attach("A", wt, dir); err != nil {
		t.Fatal(err)
	}
	n, ok := s.FindNoteIn("A", "n")
	if !ok {
		t.Fatal("note not loaded")
	}
	if want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC); !n.Created.Equal(want) {
		t.Fatalf("Created = %v, want %v", n.Created, want)
	}
	writeRaw(t, dir, "n.toml", "format = 1\npath = \"a.go\"\nstart = 1\nend = 1\nsummary = \"s2\"\ncreated = 2026-01-02T03:04:06Z\nlines = [\"x\"]\n")
	s.Reload("A")
	n, _ = s.FindNoteIn("A", "n")
	if n.Summary != "s2" || n.Created.Second() != 6 {
		t.Fatalf("after replace: %+v", n)
	}
}
