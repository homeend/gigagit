package agentdocs

import (
	"testing"

	"github.com/homeend/gigagit/internal/notemeta"
)

func TestAddNoteMetaKeepsMetaAndDisplay(t *testing.T) {
	t.Parallel()
	s := New()
	n, err := s.AddNoteMeta("/r", "a.go", []string{"x", "y"}, 1, 1, NoteText{Summary: "s",
		Meta: []notemeta.KV{{Key: "kind", Value: "issue"}}, Display: notemeta.Display{"kind": {Show: []string{"badge"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(n.Meta) != 1 || n.Display["kind"].Show[0] != "badge" {
		t.Fatalf("note = %+v", n)
	}
	w := NoteWire(n, "f1", nil)
	if w.Meta["kind"] != "issue" || len(w.Display) == 0 {
		t.Fatalf("wire = %+v", w)
	}
}

func TestMetaFromMapIsSorted(t *testing.T) {
	t.Parallel()
	got := MetaFromMap(map[string]string{"b": "2", "a": "1"})
	if len(got) != 2 || got[0].Key != "a" || got[1].Key != "b" {
		t.Fatalf("got %v", got)
	}
}
