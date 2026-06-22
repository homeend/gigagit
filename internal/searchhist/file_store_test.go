package searchhist

import (
	"path/filepath"
	"testing"
)

func newStore(t *testing.T) *FileStore {
	t.Helper()
	return NewFileStore(t.TempDir())
}

func TestRecordNewestFirstAndDedupToTop(t *testing.T) {
	s := newStore(t)
	for _, p := range []string{"alpha", "beta", "gamma"} {
		if err := s.Record("panel", p, 20); err != nil {
			t.Fatalf("Record(%q): %v", p, err)
		}
	}
	// Re-record an existing phrase: moves to top, no duplicate.
	if err := s.Record("panel", "alpha", 20); err != nil {
		t.Fatal(err)
	}
	got := s.All()["panel"]
	want := []string{"alpha", "gamma", "beta"}
	if len(got) != len(want) {
		t.Fatalf("ring = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ring = %v, want %v", got, want)
		}
	}
}

func TestRecordEmptyIsNoop(t *testing.T) {
	s := newStore(t)
	if err := s.Record("panel", "", 20); err != nil {
		t.Fatal(err)
	}
	if err := s.Record("panel", "   ", 20); err != nil {
		t.Fatal(err)
	}
	if got := s.All()["panel"]; len(got) != 0 {
		t.Fatalf("empty/blank phrase must not record, got %v", got)
	}
}

func TestRecordTrimsToSize(t *testing.T) {
	s := newStore(t)
	for _, p := range []string{"a", "b", "c", "d"} {
		if err := s.Record("panel", p, 2); err != nil {
			t.Fatal(err)
		}
	}
	got := s.All()["panel"]
	if len(got) != 2 || got[0] != "d" || got[1] != "c" {
		t.Fatalf("ring = %v, want [d c] (trimmed to 2, newest-first)", got)
	}
}

func TestRecordClampsSizeToMax(t *testing.T) {
	s := newStore(t)
	if err := s.Record("panel", "x", MaxSize+500); err != nil {
		t.Fatal(err)
	}
	// The oversized size argument must not panic/overflow and the entry survives.
	if got := s.All()["panel"]; len(got) != 1 || got[0] != "x" {
		t.Fatalf("ring = %v, want [x]", got)
	}
}

func TestScopesAreIndependentAndPersist(t *testing.T) {
	root := t.TempDir()
	s1 := NewFileStore(root)
	if err := s1.Record("panel", "p1", 20); err != nil {
		t.Fatal(err)
	}
	if err := s1.Record("bookmark", "b1", 20); err != nil {
		t.Fatal(err)
	}
	// A fresh store over the same root reads what s1 wrote.
	s2 := NewFileStore(root)
	all := s2.All()
	if len(all["panel"]) != 1 || all["panel"][0] != "p1" {
		t.Fatalf("panel = %v, want [p1]", all["panel"])
	}
	if len(all["bookmark"]) != 1 || all["bookmark"][0] != "b1" {
		t.Fatalf("bookmark = %v, want [b1]", all["bookmark"])
	}
}

func TestRecordReadMergesConcurrentSibling(t *testing.T) {
	root := t.TempDir()
	a := NewFileStore(root)
	b := NewFileStore(root)
	if err := a.Record("panel", "from-a", 20); err != nil {
		t.Fatal(err)
	}
	// b records without having seen a's write in memory: read-merge must keep both.
	if err := b.Record("panel", "from-b", 20); err != nil {
		t.Fatal(err)
	}
	got := NewFileStore(root).All()["panel"]
	if len(got) != 2 || got[0] != "from-b" || got[1] != "from-a" {
		t.Fatalf("ring = %v, want [from-b from-a] (read-merge kept the sibling)", got)
	}
}

func TestAllOnMissingFileIsEmpty(t *testing.T) {
	s := NewFileStore(filepath.Join(t.TempDir(), "nope"))
	if got := s.All(); len(got) != 0 {
		t.Fatalf("missing file should yield empty map, got %v", got)
	}
}
