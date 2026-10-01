package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/agentdocs"
)

var errTestLoad = errors.New("boom")

// notedDoc is a loaded working-tree document of n lines ("line 1"…) filed
// in a store of its own.
func notedDoc(t *testing.T, n int) *openFile {
	t.Helper()
	d := newOpenFile(fileSource{kind: srcWorktree}, "f.go")
	d.docs, d.root = agentdocs.New(), "/r"
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
	if !strings.HasPrefix(n.ID, "t") || n.Start != 3 || n.End != 4 || n.Summary != "look here" || n.Author != "agent" {
		t.Fatalf("note = %+v", n)
	}
	if text := d.docs.NoteText(n.ID); len(text) != 2 || text[0] != "line 3" || text[1] != "line 4" {
		t.Fatalf("text = %q, want the two lines", text)
	}
	if !d.backgrounded {
		t.Fatal("a file with a note must survive esc (backgrounded)")
	}
}

func TestFileNoteAddRefusals(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", agentdocs.MaxNoteSummary+1)
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
		{"long rationale", 1, 1, "s", strings.Repeat("y", agentdocs.MaxNoteRationale+1), "the rationale is longer than 4000 characters"},
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
	c.docs = agentdocs.New()
	c.fill(fileContentMsg{lines: docLines(3)}, 10, 80)
	if _, err := c.addNote(1, 1, "s", "", ""); err == nil || err.Error() != "temporary notes go on working-tree files only" {
		t.Errorf("commit version: err = %v", err)
	}
	p := newOpenFile(fileSource{kind: srcWorktree}, "f.go") // still "(loading…)"
	p.docs = agentdocs.New()
	if _, err := p.addNote(1, 1, "s", "", ""); err == nil || err.Error() != "f.go has no lines to note" {
		t.Errorf("placeholder: err = %v", err)
	}
}

func TestFileNoteCapPerFile(t *testing.T) {
	t.Parallel()
	d := notedDoc(t, 10)
	for i := 0; i < agentdocs.MaxNotesPerFile; i++ {
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
	aID := a.ID
	b, _ := d.addNote(2, 3, "b", "", "")
	bID := b.ID
	c, _ := d.addNote(2, 2, "c", "", "")
	cID := c.ID
	got := d.notes
	if got[0].ID != bID || got[1].ID != cID || got[2].ID != aID {
		t.Fatalf("order = %s %s %s, want b c a", got[0].Summary, got[1].Summary, got[2].Summary)
	}
	if d.noteAt(3).ID != bID || d.noteAt(7).ID != aID || d.noteAt(5) != nil {
		t.Fatal("noteAt does not find the covering note")
	}
	if !d.removeNote(bID) || d.removeNote(bID) || len(d.notes) != 2 {
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
	id := n.ID
	m = m.escDoc(d)
	if m.openFiles.find(m.currentWorktree, docKey(src, "a.txt")) != d || len(d.notes) != 1 {
		t.Fatal("esc closed a file that carries notes")
	}
	if fd, fn := m.findFileNote(id); fd != d || fn == nil || fn.ID != id {
		t.Fatalf("findFileNote = %v %v", fd, fn)
	}
	m = m.closeDoc(d)
	if m.openFiles.find(m.currentWorktree, docKey(src, "a.txt")) != nil {
		t.Fatal("X did not close the file")
	}
	if fd, _ := m.findFileNote(id); fd != nil {
		t.Fatal("a closed file's note is still findable")
	}
}

// textLines is content lines for the given source lines.
func textLines(ss ...string) []contentLine {
	return fileContentLinesTok([]byte(strings.Join(ss, "\n")+"\n"), nil)
}
