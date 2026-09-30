package tui

import (
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestNoteReferenceText(t *testing.T) {
	t.Parallel()
	d := notedDoc(t, 10)
	a, _ := d.addNote(3, 4, "s", "", "")
	b, _ := d.addNote(7, 7, "s", "", "")
	if got, want := a.reference("dir/f.go"), "gg note "+a.id+" dir/f.go:3-4"; got != want {
		t.Errorf("reference = %q, want %q", got, want)
	}
	if got, want := b.reference("f.go"), "gg note "+b.id+" f.go:7"; got != want {
		t.Errorf("reference = %q, want %q", got, want)
	}
}

func TestBraceKeysStepThroughAFilesNotes(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	d.addNote(5, 6, "first", "", "")
	d.addNote(20, 20, "second", "", "")
	m = fvKeys(t, m, key("}"))
	if d.p.cur != 4 {
		t.Fatalf("} put the cursor on line %d, want 5", d.p.cur+1)
	}
	m = fvKeys(t, m, key("}"))
	if d.p.cur != 19 {
		t.Fatalf("second } put the cursor on line %d, want 20", d.p.cur+1)
	}
	m = fvKeys(t, m, key("}"))
	if d.p.cur != 19 || m.statusMsg == "" {
		t.Fatalf("} past the last note: cursor %d status %q, want it to stay and say so", d.p.cur+1, m.statusMsg)
	}
	m = fvKeys(t, m, key("{"))
	if d.p.cur != 4 {
		t.Fatalf("{ put the cursor on line %d, want 5", d.p.cur+1)
	}
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "first") {
		t.Fatalf("the note stepped to is not on screen:\n%s", out)
	}
}

func TestBraceKeyMovesToTheNextOpenFileWithNotes(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t) // a.txt — a real file in the repo, 40 lines
	d.addNote(5, 5, "in a", "", "")
	// A loaded commit version: bringing it to the front re-reads nothing, so
	// the test needs no second file on disk. (addNote refuses a commit
	// version; the note is planted directly — only the stepping is under test.)
	other := newOpenFile(fileSource{kind: srcCommit, rev: "abc"}, "b.txt")
	other.fill(fileContentMsg{lines: docLines(10)}, 10, 80)
	other.notes = []*fileNote{{id: "t-x", seq: 1 << 40, start: 3, end: 3, summary: "in other", author: "agent"}}
	other.syncNoteRows()
	m.openFiles.touch(m.currentWorktree, other, m.docShown)
	m.openFiles.touch(m.currentWorktree, d, m.docShown) // d is the most recent again
	m = fvKeys(t, m, key("}"), key("}"))                // a's note, then onwards
	fv, ok := m.topLayer().(*fileViewer)
	if !ok || fv.openFile != other || other.p.cur != 2 {
		t.Fatalf("top = %T cursor %d, want the other file's viewer on line 3", m.topLayer(), other.p.cur+1)
	}
}

func TestDismissAndReferenceActOnTheCursorLinesNote(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	copied := new(string)
	m.clipWrite = func(_ io.Writer, s string) (string, error) { *copied = s; return "fake", nil }
	n, _ := d.addNote(5, 6, "first", "", "")
	m = fvKeys(t, m, key("r"), key("d")) // cursor on line 1: no note there
	if *copied != "" || len(d.notes) != 1 {
		t.Fatal("r/d acted on a line that carries no note")
	}
	m = fvKeys(t, m, key("}"), key("r"))
	if want := n.reference("a.txt"); *copied != want {
		t.Fatalf("copied %q, want %q", *copied, want)
	}
	m = fvKeys(t, m, key("d"))
	if len(d.notes) != 0 || d.p.extraRows != nil {
		t.Fatal("d did not dismiss the note")
	}
	if fv, ok := m.topLayer().(*fileViewer); !ok || fv.openFile != d {
		t.Fatal("dismissing the last note closed the file")
	}
}

func TestNoteRowsInTheActionMenuAndHints(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	ours := map[string]bool{"note-next": true, "note-prev": true, "copy-note-ref": true, "note-dismiss": true}
	for _, r := range availableActions(m) {
		if ours[r.id] {
			t.Fatalf("row %q offered on a file without notes", r.id)
		}
	}
	if strings.Contains(ansi.Strip(m.View()), "[}/{] notes") {
		t.Fatal("the note keys are advertised on a file without notes")
	}
	d.addNote(1, 1, "s", "", "")
	ids := map[string]bool{}
	for _, r := range availableActions(m) {
		ids[r.id] = true
	}
	for _, want := range []string{"note-next", "note-prev", "copy-note-ref", "note-dismiss"} {
		if !ids[want] {
			t.Errorf(". menu lacks %q (have %v)", want, ids)
		}
	}
	if !strings.Contains(ansi.Strip(m.View()), "[}/{] notes") {
		t.Fatal("the viewer's hint line does not advertise the note keys")
	}
}

func TestSwitcherRowShowsTheNoteCount(t *testing.T) {
	t.Parallel()
	d := notedDoc(t, 10)
	if got := openFileRowText(d, false); strings.Contains(got, "note") {
		t.Fatalf("row %q names notes on a file without any", got)
	}
	d.addNote(1, 1, "s", "", "")
	if got := openFileRowText(d, false); !strings.Contains(got, "  1 note  ") {
		t.Fatalf("row = %q, want `1 note` after the line", got)
	}
	d.addNote(2, 2, "s", "", "")
	if got := openFileRowText(d, false); !strings.Contains(got, "  2 notes  ") {
		t.Fatalf("row = %q, want `2 notes` after the line", got)
	}
}
