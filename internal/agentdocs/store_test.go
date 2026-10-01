package agentdocs

import (
	"sync"
	"testing"
	"time"
)

func TestNextFileSeqCountsPerStore(t *testing.T) {
	t.Parallel()
	a, b := New(), New()
	if a.NextFileSeq() != 1 || a.NextFileSeq() != 2 || b.NextFileSeq() != 1 {
		t.Fatal("each store counts from 1, alone")
	}
	if Shared() != Shared() {
		t.Fatal("Shared must be one store")
	}
}

func TestEveryChangeSignalsAndANoOpDoesNot(t *testing.T) {
	t.Parallel()
	s := New()
	ch, cancel := s.Subscribe()
	defer cancel()
	got := func() bool {
		select {
		case <-ch:
			return true
		case <-time.After(50 * time.Millisecond):
			return false
		}
	}
	n := addOn(t, s, abc(), 1, 1)
	if !got() {
		t.Fatal("AddNote did not signal")
	}
	s.Align(root, "f.go", abc())
	if got() {
		t.Fatal("a no-op Align signalled")
	}
	s.RemoveNote(n.ID)
	if !got() {
		t.Fatal("RemoveNote did not signal")
	}
	if s.RemoveNote(n.ID); got() {
		t.Fatal("removing nothing signalled")
	}
}

func TestStoreIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()
	s := New()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				n, _ := s.AddNote(root, "f.go", abc(), 1+j%5, 1+j%5, "s", "", "")
				s.Align(root, "f.go", []string{"x", "a", "b", "c", "d", "e"}[i%2:])
				s.Notes(root, "f.go")
				s.NoteText(n.ID)
				s.RemoveNote(n.ID)
			}
		}(i)
	}
	wg.Wait()
}

func TestReplyHelpers(t *testing.T) {
	t.Parallel()
	n := Note{ID: "t7", Path: "a/b.go", Start: 12, End: 14, Summary: "s", Author: "agent"}
	if got := NoteReference(n); got != "gg note t7 a/b.go:12-14" {
		t.Errorf("reference = %q", got)
	}
	n1 := n
	n1.End = 12
	if got := NoteReference(n1); got != "gg note t7 a/b.go:12" {
		t.Errorf("one-line reference = %q", got)
	}
	if got := NotedDetail(n, "; closed x.go (20 files open)"); got != "noted a/b.go:12-14 as t7; closed x.go (20 files open)" {
		t.Errorf("detail = %q", got)
	}
	w := NoteWire(n, "f3", []string{"l"})
	if w.ID != "t7" || w.FileID != "f3" || w.Path != "a/b.go" || w.Start != 12 || w.End != 14 || w.Text[0] != "l" {
		t.Errorf("wire = %+v", w)
	}
}
