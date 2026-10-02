package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// A note over several lines names them in its box title; a one-line note
// keeps the single number.
func TestNoteBoxTitleNamesARange(t *testing.T) {
	t.Parallel()
	v := &diffView{noteAddr: model.FileAddress{Path: "a.go"}}
	r := domain.ResolvedNote{Note: model.Note{ID: "n1", Side: model.NoteSideNew}, Range: [2]int{8, 10}}
	if got := v.noteBoxTitle(r); got != "note · a.go R8-10" {
		t.Errorf("range title = %q", got)
	}
	r.Note.Side, r.Range = model.NoteSideOld, [2]int{4, 4}
	if got := v.noteBoxTitle(r); got != "note · a.go L4" {
		t.Errorf("one-line title = %q", got)
	}
}
