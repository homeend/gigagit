package tui

import (
	"strings"
	"testing"
)

// notedDoc is a loaded working-tree document of n lines ("line 1"…).
func notedDoc(t *testing.T, n int) *openFile {
	t.Helper()
	d := newOpenFile(fileSource{kind: srcWorktree}, "f.go")
	d.fill(fileContentMsg{lines: docLines(n)}, 10, 80)
	return d
}

func TestFileNoteAddKeepsTheRangeTextAndMarksTheFileBackgrounded(t *testing.T) {
	t.Parallel()
	d := notedDoc(t, 10)
	n, err := d.addNote(3, 4, "  look here  ", "because", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(n.id, "t") || n.start != 3 || n.end != 4 || n.summary != "look here" || n.author != "agent" {
		t.Fatalf("note = %+v", n)
	}
	if len(n.anchor) != 2 || n.anchor[0] != "line 3" || n.anchor[1] != "line 4" {
		t.Fatalf("anchor = %q, want the two lines' text", n.anchor)
	}
	if !d.backgrounded {
		t.Fatal("a file with a note must survive esc (backgrounded)")
	}
}

func TestFileNoteAddRefusals(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", maxFileNoteSummary+1)
	for _, tc := range []struct {
		name       string
		start, end int
		summary    string
		rationale  string
		want       string
	}{
		{"zero line", 0, 1, "s", "", "a line number is 1-based"},
		{"backwards", 4, 3, "s", "", "the range ends before it starts"},
		{"past the end", 9, 11, "s", "", "line 11 is past the end of f.go (10 lines)"},
		{"no summary", 1, 1, "   ", "", "a note needs a summary"},
		{"long summary", 1, 1, long, "", "the summary is longer than 500 characters"},
		{"long rationale", 1, 1, "s", strings.Repeat("y", maxFileNoteRationale+1), "the rationale is longer than 4000 characters"},
	} {
		d := notedDoc(t, 10)
		if _, err := d.addNote(tc.start, tc.end, tc.summary, tc.rationale, ""); err == nil || err.Error() != tc.want {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
		if len(d.notes) != 0 || d.backgrounded {
			t.Errorf("%s: a refused add changed the document", tc.name)
		}
	}
}

func TestFileNoteAddRefusesOtherSourcesAndPlaceholders(t *testing.T) {
	t.Parallel()
	c := newOpenFile(fileSource{kind: srcCommit, rev: "abc"}, "f.go")
	c.fill(fileContentMsg{lines: docLines(3)}, 10, 80)
	if _, err := c.addNote(1, 1, "s", "", ""); err == nil || err.Error() != "temporary notes go on working-tree files only" {
		t.Errorf("commit version: err = %v", err)
	}
	p := newOpenFile(fileSource{kind: srcWorktree}, "f.go") // still "(loading…)"
	if _, err := p.addNote(1, 1, "s", "", ""); err == nil || err.Error() != "f.go has no lines to note" {
		t.Errorf("placeholder: err = %v", err)
	}
}

func TestFileNoteCapPerFile(t *testing.T) {
	t.Parallel()
	d := notedDoc(t, 10)
	for i := 0; i < maxFileNotes; i++ {
		if _, err := d.addNote(1, 1, "s", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.addNote(1, 1, "s", "", ""); err == nil || err.Error() != "f.go already carries 50 notes" {
		t.Fatalf("err = %v", err)
	}
}

func TestFileNotesStayOrderedByLineThenAge(t *testing.T) {
	t.Parallel()
	d := notedDoc(t, 10)
	a, _ := d.addNote(7, 7, "a", "", "")
	b, _ := d.addNote(2, 3, "b", "", "")
	c, _ := d.addNote(2, 2, "c", "", "")
	got := []*fileNote{d.notes[0], d.notes[1], d.notes[2]}
	if got[0] != b || got[1] != c || got[2] != a {
		t.Fatalf("order = %s %s %s, want b c a", got[0].summary, got[1].summary, got[2].summary)
	}
	if d.noteAt(3) != b || d.noteAt(7) != a || d.noteAt(5) != nil {
		t.Fatal("noteAt does not find the covering note")
	}
	if !d.removeNote(b.id) || d.removeNote(b.id) || len(d.notes) != 2 {
		t.Fatal("removeNote must remove exactly once")
	}
	if n := d.clearNotes(); n != 2 || len(d.notes) != 0 {
		t.Fatalf("clearNotes = %d, left %d", n, len(d.notes))
	}
}

func TestEvictionSkipsAFileWithNotes(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	fill20(m) // f0.txt is the oldest
	src := fileSource{kind: srcWorktree}
	f0 := m.openFiles.find(m.currentWorktree, docKey(src, "f0.txt"))
	if _, err := f0.addNote(1, 1, "keep me", "", ""); err != nil {
		t.Fatal(err)
	}
	bgDoc(m, src, "new.txt", 1)
	if m.openFiles.find(m.currentWorktree, docKey(src, "f0.txt")) == nil {
		t.Fatal("the annotated file was evicted")
	}
	if m.openFiles.find(m.currentWorktree, docKey(src, "f1.txt")) != nil {
		t.Fatal("want the oldest file WITHOUT notes (f1.txt) evicted instead")
	}
}

func TestEvictionGrowsTheListWhenEveryFileHasNotes(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	fill20(m)
	for _, d := range m.openFiles.list(m.currentWorktree) {
		if _, err := d.addNote(1, 1, "n", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	bgDoc(m, fileSource{kind: srcWorktree}, "new.txt", 1)
	if got := len(m.openFiles.list(m.currentWorktree)); got != maxOpenFiles+1 {
		t.Fatalf("list = %d files, want %d (nothing evictable)", got, maxOpenFiles+1)
	}
}

func TestEscKeepsANotedFileAndXDropsItWithItsNotes(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	src := fileSource{kind: srcWorktree}
	d := bgDoc(m, src, "a.txt", 5)
	d.backgrounded = false // opened in the foreground
	m = m.pushLayer(&fileViewer{d})
	n, err := d.addNote(2, 2, "s", "", "")
	if err != nil {
		t.Fatal(err)
	}
	m = m.escDoc(d)
	if m.openFiles.find(m.currentWorktree, docKey(src, "a.txt")) != d || len(d.notes) != 1 {
		t.Fatal("esc closed a file that carries notes")
	}
	if fd, fn := m.findFileNote(n.id); fd != d || fn != n {
		t.Fatalf("findFileNote = %v %v", fd, fn)
	}
	m = m.closeDoc(d)
	if m.openFiles.find(m.currentWorktree, docKey(src, "a.txt")) != nil {
		t.Fatal("X did not close the file")
	}
	if fd, _ := m.findFileNote(n.id); fd != nil {
		t.Fatal("a closed file's note is still findable")
	}
}
