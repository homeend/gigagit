package agentdocs

import (
	"os"
	"path/filepath"
	"testing"
)

// Two attached worktrees hold the same overview id: the id-only check finds
// nobody (ambiguous), the root-scoped one checks exactly its worktree.
func TestAnchorChecksAreRootScoped(t *testing.T) {
	t.Parallel()
	s := New()
	defer s.Close()
	wtA, wtB := t.TempDir(), t.TempDir()
	dirA, dirB := filepath.Join(t.TempDir(), "docs"), filepath.Join(t.TempDir(), "docs")
	for _, d := range []string{dirA, dirB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		writeRaw(t, d, "same.md", "+++\ntitle = \"T\"\n+++\n[a](a.go:1)\n")
	}
	if err := os.WriteFile(filepath.Join(wtB, "a.go"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err) // only B has the file
	}
	if err := s.Attach("A", wtA, dirA); err != nil {
		t.Fatal(err)
	}
	if err := s.Attach("B", wtB, dirB); err != nil {
		t.Fatal(err)
	}
	if s.CheckAnchors("same") {
		t.Fatal("an ambiguous id must check nothing")
	}
	if !s.CheckAnchorsIn("A", "same") {
		t.Fatal("A's anchor is missing: the check must change its flag")
	}
	a, _ := s.OverviewIn("A", "same")
	b, _ := s.OverviewIn("B", "same")
	if !a.Anchors[0].Missing || b.Anchors[0].Missing {
		t.Fatalf("A missing=%v B missing=%v", a.Anchors[0].Missing, b.Anchors[0].Missing)
	}
	if !s.SetAnchorMissingIn("B", "same", 0, true) || s.SetAnchorMissingIn("B", "same", 0, true) {
		t.Fatal("SetAnchorMissingIn must report the change exactly once")
	}
	if s.SetAnchorMissing("same", 0, false) {
		t.Fatal("an ambiguous id must set nothing")
	}
	if b, _ = s.OverviewIn("B", "same"); !b.Anchors[0].Missing {
		t.Fatal("B's flag was not set")
	}
}
