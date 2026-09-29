package domain

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// A review's notes are built at read time and never stored, so no write may
// touch them: the wire says so, and a page hides edit/reply/remove on them.
func TestToWireNoteMarksReviewNotesReadOnly(t *testing.T) {
	t.Parallel()
	rev := ToWireNote(ResolvedNote{Note: model.Note{ID: model.ReviewNoteIDPrefix + "abc:0", Source: model.NoteSourceAgent}})
	if !rev.ReadOnly {
		t.Error("a review note's wire form is not read_only")
	}
	own := ToWireNote(ResolvedNote{Note: model.Note{ID: "n1", Source: model.NoteSourceAgent}})
	if own.ReadOnly {
		t.Error("a stored agent note came out read_only")
	}
}
