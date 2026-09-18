package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// arriveDiff simulates the async loader landing on the diff a }/{ file step
// just opened: rows with changes at 4 and 24, the cursor parked on the first
// change exactly as applyDiff leaves it. The notes arrive separately, the way
// loadNotesCmd delivers them.
func arriveDiff(m Model) Model {
	v := diffViewWith(cursorRows(40, 4, 24), []int{4, 24})
	v.focusBlock(0, m.diffBodyRows())
	*m.diffLayer() = *v
	return m
}

// notedFileStepBackModel mirrors notedFileStepModel: the selection sits on
// c.go and a.go (two plain steps back) is the file that carries notes.
func notedFileStepBackModel() Model {
	m := treeDiffModel(3) // on c.go
	m.height = 12
	m.noteCounts = domain.NoteCounts{ByPath: map[string]int{"a.go": 1}}
	return m
}

// TestNoteFileStepLandsOnTheFirstNote: }} steps to the next file that carries
// notes and then LANDS on that file's first note — the point of the gesture.
// Before the fix the cursor stayed on the new file's first change block, which
// can be nowhere near the note.
func TestNoteFileStepLandsOnTheFirstNote(t *testing.T) {
	t.Parallel()
	m := notedFileStepModel()
	m.height = 12
	u, _ := m.Update(keyMsg("}"))          // primes
	u2, _ := u.(Model).Update(keyMsg("}")) // steps to c.go
	mm := u2.(Model)
	if mm.diffTag != "commit:abc:c.go" {
		t.Fatalf("}} must land on c.go, tag=%q", mm.diffTag)
	}
	mm = arriveDiff(mm)
	if mm.diffLayer().curLine != 4 {
		t.Fatalf("precondition: a fresh diff opens on its first change, got line %d", mm.diffLayer().curLine)
	}
	nm, _ := mm.Update(notesLoadedMsg{tag: mm.diffTag, notes: []domain.ResolvedNote{
		rootNote("n1", 31, "late", "", model.NoteSourceAgent, model.NoteActive),
	}})
	if got := nm.(Model).diffLayer().curLine; got != 30 {
		t.Fatalf("}} landed on line %d, want 30 (the note on line 31)", got)
	}
}

// TestNoteFileStepBackLandsOnTheLastNote: {{ steps back and lands on that
// file's LAST note, so a second { keeps walking backwards instead of dead-
// ending on "no previous file with notes".
func TestNoteFileStepBackLandsOnTheLastNote(t *testing.T) {
	t.Parallel()
	m := notedFileStepBackModel()
	u, _ := m.Update(keyMsg("{"))
	u2, _ := u.(Model).Update(keyMsg("{"))
	mm := u2.(Model)
	if mm.diffTag != "commit:abc:a.go" {
		t.Fatalf("{{ must land on a.go, tag=%q", mm.diffTag)
	}
	mm = arriveDiff(mm)
	nm, _ := mm.Update(notesLoadedMsg{tag: mm.diffTag, notes: []domain.ResolvedNote{
		rootNote("n1", 3, "early", "", model.NoteSourceAgent, model.NoteActive),
		rootNote("n2", 31, "late", "", model.NoteSourceAgent, model.NoteActive),
	}})
	if got := nm.(Model).diffLayer().curLine; got != 30 {
		t.Fatalf("{{ landed on line %d, want 30 (the LAST note, on line 31)", got)
	}
}

// TestNoteFileStepLandingExpandsAFold: the landing note may sit in a collapsed
// run — the landing expands the view the way } does inside one file.
func TestNoteFileStepLandingExpandsAFold(t *testing.T) {
	t.Parallel()
	m := notedFileStepModel()
	m.height = 12
	u, _ := m.Update(keyMsg("}"))
	u2, _ := u.(Model).Update(keyMsg("}"))
	mm := u2.(Model)
	v := diffViewWith(cursorRows(40, 4, 34), []int{4, 34})
	v.partial = true
	v.rebuild()
	v.focusBlock(0, mm.diffBodyRows())
	*mm.diffLayer() = *v
	nm, _ := mm.Update(notesLoadedMsg{tag: mm.diffTag, notes: []domain.ResolvedNote{
		rootNote("n1", 21, "buried", "", model.NoteSourceAgent, model.NoteActive),
	}})
	got := nm.(Model).diffLayer()
	if got.partial {
		t.Fatal("landing on a folded note must expand the view (like f)")
	}
	if got.lines[got.curLine].Row.RightNo != 21 {
		t.Fatalf("landed on RightNo %d, want 21", got.lines[got.curLine].Row.RightNo)
	}
}

// TestNoteFileStepLandingIsDroppedByAnyOtherKey: a slow notes load must not
// yank the cursor after the user has moved on.
func TestNoteFileStepLandingIsDroppedByAnyOtherKey(t *testing.T) {
	t.Parallel()
	m := notedFileStepModel()
	m.height = 12
	u, _ := m.Update(keyMsg("}"))
	u2, _ := u.(Model).Update(keyMsg("}"))
	mm := arriveDiff(u2.(Model))
	u3, _ := mm.Update(keyMsg("j")) // the user moves the cursor themselves
	mm = u3.(Model)
	want := mm.diffLayer().curLine
	nm, _ := mm.Update(notesLoadedMsg{tag: mm.diffTag, notes: []domain.ResolvedNote{
		rootNote("n1", 31, "late", "", model.NoteSourceAgent, model.NoteActive),
	}})
	if got := nm.(Model).diffLayer().curLine; got != want {
		t.Fatalf("a dropped landing moved the cursor to %d, want %d", got, want)
	}
}

// TestNoteFileStepLandingFiresOnce: the landing is spent when it lands. A
// later notes reload for the SAME diff — an edit, a reply, an srcNotes refresh
// — must leave the cursor wherever the user has since put it.
func TestNoteFileStepLandingFiresOnce(t *testing.T) {
	t.Parallel()
	notes := []domain.ResolvedNote{
		rootNote("n1", 31, "late", "", model.NoteSourceAgent, model.NoteActive),
	}
	m := notedFileStepModel()
	m.height = 12
	u, _ := m.Update(keyMsg("}"))
	u2, _ := u.(Model).Update(keyMsg("}"))
	mm := arriveDiff(u2.(Model))
	u3, _ := mm.Update(notesLoadedMsg{tag: mm.diffTag, notes: notes})
	mm = u3.(Model)
	if mm.diffLayer().curLine != 30 {
		t.Fatalf("precondition: the landing must fire, got line %d", mm.diffLayer().curLine)
	}
	mm.diffLayer().setCursorLine(4, mm.diffBodyRows()) // the user reads elsewhere
	nm, _ := mm.Update(notesLoadedMsg{tag: mm.diffTag, notes: notes})
	if got := nm.(Model).diffLayer().curLine; got != 4 {
		t.Fatalf("a second notes load re-landed the cursor on %d, want 4", got)
	}
}
