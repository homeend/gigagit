package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// noteOnStackFile hangs one root note off file i of a stack, on the new side at
// line no. The note lives on the FILE's own view (stackFile.d), which is where
// its own loader stamped its own address.
func noteOnStackFile(v *diffView, i, no int, summary string) {
	d := v.stk.files[i].d
	d.notes = append(d.notes, rootNote(summary, no, summary, "", model.NoteSourceUser, model.NoteActive))
}

// noteTextAt returns every note-row text hanging off logical line li.
func noteTextAt(v *diffView, li int) string {
	byLine, _ := v.noteRowIndex()
	var b strings.Builder
	for _, nl := range byLine[li] {
		b.WriteString(nl.text)
		b.WriteString("\n")
	}
	return b.String()
}

// Line NUMBERS repeat across a stack's files — every file has a line 5 — so a
// note must anchor inside ITS OWN file's range, never on the first file that
// happens to carry the number.
func TestStackAnchorsEachFilesNotesInItsOwnFile(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, cursorRows(10), cursorRows(10))
	noteOnStackFile(v, 0, 5, "on A")
	noteOnStackFile(v, 1, 5, "on B")
	v.rebuild()

	byLine, _ := v.noteRowIndex()
	if len(byLine) != 2 {
		t.Fatalf("want note rows on 2 lines (one per file), got %d: %v", len(byLine), byLine)
	}
	loB, hiB := v.fileLineRange(1)
	found := -1
	for li := range byLine {
		if strings.Contains(noteTextAt(v, li), "on B") {
			found = li
		}
	}
	if found < loB || found > hiB {
		t.Fatalf("B's note anchored at line %d, want inside file B's range [%d,%d]", found, loB, hiB)
	}
	loA, hiA := v.fileLineRange(0)
	for li := range byLine {
		if strings.Contains(noteTextAt(v, li), "on A") && (li < loA || li > hiA) {
			t.Fatalf("A's note anchored at line %d, outside file A's range [%d,%d]", li, loA, hiA)
		}
	}
}

// A forge comment on the whole FILE hangs off that file's first body line, not
// the stack's first line.
func TestStackFileLevelNoteAnchorsAtItsOwnFileTop(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, cursorRows(6), cursorRows(6))
	d := v.stk.files[1].d
	n := rootNote("fl", 0, "whole file", "", model.NoteSourceForge, model.NoteActive)
	n.Note.Range = [2]int{}
	n.Range = [2]int{}
	d.notes = []domain.ResolvedNote{n}
	v.rebuild()

	byLine, _ := v.noteRowIndex()
	if len(byLine) != 1 {
		t.Fatalf("want one anchored file-level note, got %d", len(byLine))
	}
	lo, hi := v.fileLineRange(1)
	for li := range byLine {
		if li < lo || li > hi {
			t.Fatalf("file-level note anchored at %d, want inside file B [%d,%d]", li, lo, hi)
		}
		if v.lines[li].kind != lineBody {
			t.Fatalf("file-level note anchored on a %v line, want the file's first body line", v.lines[li].kind)
		}
	}
}

// End to end on a real repo: a stack resolves EVERY loaded file's notes
// against that file's own address, and splices its note rows under its own
// lines. One note on b.go must not land on a.go.
func TestStackLoadsEachFilesNotes(t *testing.T) {
	t.Parallel()
	m := stackRepoModel(t)
	m.svc.UseNotesDir(t.TempDir())
	sha := m.commits[0].Hash
	if _, err := m.svc.NoteAdd(context.Background(), model.Note{
		Address: model.FileAddress{State: model.StateCommitted, Commit: sha, Path: "b.go"},
		Side:    model.NoteSideNew, Range: [2]int{3, 3},
		Summary: "on b.go",
	}); err != nil {
		t.Fatalf("NoteAdd: %v", err)
	}

	m = m.setStackedPref(true)
	var pick contentLine
	for _, l := range m.filesView.visible() {
		if l.path != "" {
			pick = l
			break
		}
	}
	u, cmd := m.openDiffForFileLine(pick)
	m = drainCmds(t, u.(Model), cmd)

	v := m.diffLayer()
	if v == nil || v.stk == nil {
		t.Fatal("the diff must have opened stacked")
	}
	for i, f := range v.stk.files {
		want := 0
		if f.path == "b.go" {
			want = 1
		}
		if got := len(v.notesOf(i)); got != want {
			t.Fatalf("file %s resolved %d notes, want %d", f.path, got, want)
		}
	}
	// …and the row is spliced inside b.go's own range.
	bi := -1
	for i, f := range v.stk.files {
		if f.path == "b.go" {
			bi = i
		}
	}
	lo, hi := v.fileLineRange(bi)
	byLine, _ := v.noteRowIndex()
	if len(byLine) != 1 {
		t.Fatalf("want note rows on exactly one line, got %d", len(byLine))
	}
	for li := range byLine {
		if li < lo || li > hi {
			t.Fatalf("b.go's note rendered at line %d, outside [%d,%d]", li, lo, hi)
		}
	}
}
