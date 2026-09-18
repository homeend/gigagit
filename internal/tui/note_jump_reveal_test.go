package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// noteBelowFoldModel opens a 200-line diff whose only change is at the top and
// whose only note hangs off line 150 — the shape a merge preview has, where
// the new side is the whole file at the tip and an agent's note can sit far
// from any hunk. The body is small enough that landing on line 150 with a
// minimal scroll leaves the note's box off the bottom of the screen.
func noteBelowFoldModel() Model {
	m := openedDiffModel(12, cursorRows(200, 4), []int{4})
	v := m.diffLayer()
	v.notes = []domain.ResolvedNote{
		rootNote("n1", 150, "far from any change", "", model.NoteSourceAgent, model.NoteActive),
	}
	v.relayout(0)
	return m
}

// noteRowsVisible reports whether ANY row of a note box is inside the
// viewport — what the user must see for the jump to have shown them anything.
func noteRowsVisible(v *diffView, body int) bool {
	for i := v.offset; i < v.offset+body && i < len(v.disp); i++ {
		if v.disp[i].note != nil {
			return true
		}
	}
	return false
}

// TestNoteJumpRevealsTheNote: } lands on the note's anchor line, and the note
// itself must be ON SCREEN. Landing on the anchor alone scrolls the anchor to
// the bottom row and leaves the whole box below the fold — the jump looks like
// it went nowhere.
func TestNoteJumpRevealsTheNote(t *testing.T) {
	t.Parallel()
	m := noteBelowFoldModel()
	body := m.diffBodyRows()
	v := m.diffLayer()
	v.setCursorLine(0, body)
	m, _ = v.update(m, synthKey("}"))
	v = m.diffLayer()
	if v.lines[v.curLine].Row.RightNo != 150 {
		t.Fatalf("} landed on RightNo %d, want 150", v.lines[v.curLine].Row.RightNo)
	}
	if !noteRowsVisible(v, body) {
		t.Fatalf("the note box is off screen after }: offset=%d body=%d anchor row=%d",
			v.offset, body, v.lineStart[v.curLine])
	}
}

// TestNoteJumpBackRevealsTheNote is the same for {, arriving from below.
func TestNoteJumpBackRevealsTheNote(t *testing.T) {
	t.Parallel()
	m := noteBelowFoldModel()
	body := m.diffBodyRows()
	v := m.diffLayer()
	v.setCursorLine(len(v.lines)-1, body)
	m, _ = v.update(m, synthKey("{"))
	v = m.diffLayer()
	if v.lines[v.curLine].Row.RightNo != 150 {
		t.Fatalf("{ landed on RightNo %d, want 150", v.lines[v.curLine].Row.RightNo)
	}
	if !noteRowsVisible(v, body) {
		t.Fatalf("the note box is off screen after {: offset=%d body=%d", v.offset, body)
	}
}

// TestNoteFileStepLandingRevealsTheNote: the }/{ FILE step's parked landing
// goes through the same reveal — it is the same gesture, one file over.
func TestNoteFileStepLandingRevealsTheNote(t *testing.T) {
	t.Parallel()
	m := notedFileStepModel()
	m.height = 12
	u, _ := m.Update(keyMsg("}"))
	u2, _ := u.(Model).Update(keyMsg("}"))
	mm := u2.(Model)
	v := diffViewWith(cursorRows(200, 4), []int{4})
	v.focusBlock(0, mm.diffBodyRows())
	*mm.diffLayer() = *v
	nm, _ := mm.Update(notesLoadedMsg{tag: mm.diffTag, notes: []domain.ResolvedNote{
		rootNote("n1", 150, "far from any change", "", model.NoteSourceAgent, model.NoteActive),
	}})
	got := nm.(Model)
	if !noteRowsVisible(got.diffLayer(), got.diffBodyRows()) {
		t.Fatalf("the note box is off screen after the file step: offset=%d body=%d",
			got.diffLayer().offset, got.diffBodyRows())
	}
}
