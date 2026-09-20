package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// openedDiffWithNotes is a freshly opened diff — cursor on the first change
// block (line 4), exactly where applyDiff parks it — whose notes have arrived.
func openedDiffWithNotes(t *testing.T, lines ...int) Model {
	t.Helper()
	m := notedFileStepModel()
	m.height = 12
	m = arriveDiff(m)
	notes := make([]domain.ResolvedNote, 0, len(lines))
	for _, l := range lines {
		notes = append(notes, rootNote("n", l, "note", "", model.NoteSourceAgent, model.NoteActive))
	}
	nm, _ := m.Update(notesLoadedMsg{tag: m.diffTag, notes: notes})
	mm := nm.(Model)
	if got := mm.diffLayer().curLine; got != 4 {
		t.Fatalf("precondition: a fresh diff opens on its first change, got line %d", got)
	}
	return mm
}

// TestFirstNoteJumpReachesANoteAboveTheCursor: a diff opens on its first
// change block, so a note ABOVE it lies behind the cursor. The first } used
// to find nothing ahead and arm the file step — walking past a note the file
// plainly carries. While no note of this file has been visited, } with
// nothing ahead lands on the file's first note instead.
func TestFirstNoteJumpReachesANoteAboveTheCursor(t *testing.T) {
	t.Parallel()
	m := openedDiffWithNotes(t, 3)
	u, _ := m.Update(keyMsg("}"))
	mm := u.(Model)
	if got := mm.diffLayer().curLine; got != 2 {
		t.Fatalf("first } landed on line %d, want 2 (the note on line 3, above the first change)", got)
	}
	if mm.diffLayer().fileArm == fileArmNextNote {
		t.Fatal("first } must not arm the file step while the file's note is unvisited")
	}
	// The note is now visited: the next } has nothing ahead and arms the step.
	u, _ = mm.Update(keyMsg("}"))
	mm = u.(Model)
	if got := mm.diffLayer().curLine; got != 2 {
		t.Fatalf("second } moved the cursor to line %d, want it to stay on 2", got)
	}
}

// TestNoteJumpNeverWrapsOnceANoteWasVisited: the fallback is for the FIRST
// jump only. After } has walked the file's notes it must end the walk (and arm
// the file step), never loop back to the top.
func TestNoteJumpNeverWrapsOnceANoteWasVisited(t *testing.T) {
	t.Parallel()
	m := openedDiffWithNotes(t, 3, 31)
	u, _ := m.Update(keyMsg("}"))
	mm := u.(Model)
	if got := mm.diffLayer().curLine; got != 30 {
		t.Fatalf("} landed on line %d, want 30 (a note AHEAD wins over the unvisited one above)", got)
	}
	// Step OFF the note by hand, past every note: the file's notes were
	// visited, so } still ends the walk rather than looping to the top.
	mm.diffLayer().setCursorLine(35, mm.diffBodyRows())
	u, _ = mm.Update(keyMsg("}"))
	if got := u.(Model).diffLayer().curLine; got != 35 {
		t.Fatalf("} past the last note wrapped to line %d, want it to stay on 35", got)
	}
}

// TestFirstNoteJumpBackReachesANoteBelowTheCursor mirrors it: { with nothing
// behind the cursor lands on the file's LAST note.
func TestFirstNoteJumpBackReachesANoteBelowTheCursor(t *testing.T) {
	t.Parallel()
	m := openedDiffWithNotes(t, 31)
	u, _ := m.Update(keyMsg("{"))
	if got := u.(Model).diffLayer().curLine; got != 30 {
		t.Fatalf("first { landed on line %d, want 30 (the file's last note)", got)
	}
}
