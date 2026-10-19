package agentdocs

import "testing"

// A note anchor names a document id (spec §3.2 `note:<id>`): a session
// note's file name as well as a memory t<n>; a non-id is still no anchor.
func TestNoteAnchorTakesADocumentID(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		dest string
		ok   bool
	}{
		{"note:t7", true},
		{"note:20260101T220100-cd34", true},
		{"note:tour-brief-12-3", true},
		{"note:", false},
		{"note:a/b", false},
		{"note:a.b", false},
	} {
		a, ok := ParseAnchorDest(c.dest)
		if ok != c.ok || (ok && a.Note != c.dest[len("note:"):]) {
			t.Errorf("ParseAnchorDest(%q) = %+v, %v; want ok=%v", c.dest, a, ok, c.ok)
		}
	}
}
