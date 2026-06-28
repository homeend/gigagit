package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// decoRowModel builds a minimal Model in list-mode with one commit that has
// multiple local-branch tips (main HEAD + branch1) and a tag (v1.0) at the
// same commit, subject "Hello world".
func decoRowModel() Model {
	m := Model{
		sel:            map[panel]int{},
		sortModes:      map[panel]sortMode{},
		dispModes:      map[panel]dispMode{},
		hscroll:        map[panel]int{},
		focus:          panelCommits,
		commitListMode: true,
		width:          120,
		height:         40,
	}
	m.branches = []model.Branch{
		{Name: "main", IsHead: true},
		{Name: "branch1"},
	}
	m.commits = []model.Commit{
		{
			Hash:    "aaaa111122223333444455556666777788889999aaaa",
			Subject: "Hello world",
			Source:  "main",
			Refs: []model.Ref{
				{Name: "main", Kind: model.RefLocal, Head: true},
				{Name: "branch1", Kind: model.RefLocal},
				{Name: "v1.0", Kind: model.RefTag},
			},
		},
		{
			Hash:    "bbbb111122223333444455556666777788889999bbbb",
			Subject: "Lineage commit",
			Source:  "dev",
		},
	}
	return m
}

// TestCommitRowHasDecoGroupBeforeSubject asserts that a multi-tip+tag commit
// row contains the deco group "(branch1, ⊙v1.0)" and that it appears before
// the subject text.
func TestCommitRowHasDecoGroupBeforeSubject(t *testing.T) {
	m := decoRowModel()
	row := m.commitIdentRowAt(0, m.commitIdentWidth(), false, -1)

	if !strings.Contains(row, "(branch1, ⊙v1.0)") {
		t.Fatalf("row = %q, want deco group \"(branch1, ⊙v1.0)\"", row)
	}
	groupIdx := strings.Index(row, "(branch1")
	subjectIdx := strings.Index(row, "Hello world")
	if groupIdx < 0 || subjectIdx < 0 {
		t.Fatalf("row = %q: group or subject not found", row)
	}
	if groupIdx > subjectIdx {
		t.Fatalf("row = %q: group at %d appears AFTER subject at %d", row, groupIdx, subjectIdx)
	}
}

// TestCommitRowLineageUnchanged asserts that a lineage row (no extra refs, no
// tags) has no deco-group opening parenthesis.
func TestCommitRowLineageUnchanged(t *testing.T) {
	m := decoRowModel()
	// commits[1] is the lineage row: Source="dev", no Refs.
	row := m.commitIdentRowAt(1, m.commitIdentWidth(), false, -1)
	if strings.Contains(row, "(") {
		t.Fatalf("lineage row = %q, must not contain a deco group \"(\"", row)
	}
}

// TestCommitHaystackIncludesTags asserts that the filter haystack for a
// commit with a tag ref includes the tag name, so /v1.0 finds it.
func TestCommitHaystackIncludesTags(t *testing.T) {
	m := decoRowModel()
	hay := m.commitHaystackAt(0)
	if !strings.Contains(hay, "v1.0") {
		t.Fatalf("haystack = %q, want tag name \"v1.0\"", hay)
	}
}
