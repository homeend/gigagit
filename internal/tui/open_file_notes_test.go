package tui

import (
	"errors"
	"strings"
	"testing"
)

var errTestLoad = errors.New("boom")

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

// textLines is content lines for the given source lines.
func textLines(ss ...string) []contentLine {
	return fileContentLinesTok([]byte(strings.Join(ss, "\n")+"\n"), nil)
}

// reload lands new content on d the way a watch reload does.
func reload(d *openFile, ss ...string) {
	d.fill(fileContentMsg{lines: textLines(ss...), reload: true}, 10, 80)
}

func abcDoc(t *testing.T) (*openFile, *fileNote) {
	t.Helper()
	d := newOpenFile(fileSource{kind: srcWorktree}, "f.go")
	d.fill(fileContentMsg{lines: textLines("a", "b", "c", "d", "e")}, 10, 80)
	n, err := d.addNote(3, 4, "on c and d", "", "") // anchor: c, d
	if err != nil {
		t.Fatal(err)
	}
	return d, n
}

func TestFileNoteMovesWhenLinesAreInsertedAbove(t *testing.T) {
	t.Parallel()
	d, n := abcDoc(t)
	reload(d, "NEW", "a", "b", "c", "d", "e")
	if n.start != 4 || n.end != 5 || n.outdated {
		t.Fatalf("note = %d-%d outdated=%v, want 4-5 live", n.start, n.end, n.outdated)
	}
}

func TestFileNoteMovesWhenLinesAreDeletedAbove(t *testing.T) {
	t.Parallel()
	d, n := abcDoc(t)
	reload(d, "b", "c", "d", "e")
	if n.start != 2 || n.end != 3 || n.outdated {
		t.Fatalf("note = %d-%d outdated=%v, want 2-3 live", n.start, n.end, n.outdated)
	}
}

func TestFileNoteGoesOutdatedWhenItsLinesAreEditedAndRecoversOnRevert(t *testing.T) {
	t.Parallel()
	d, n := abcDoc(t)
	reload(d, "a", "b", "C-EDITED", "d", "e")
	if !n.outdated || n.start != 3 || n.end != 4 {
		t.Fatalf("note = %d-%d outdated=%v, want 3-4 outdated", n.start, n.end, n.outdated)
	}
	reload(d, "a", "b", "c", "d", "e")
	if n.outdated || n.start != 3 || n.end != 4 {
		t.Fatalf("after the revert: %d-%d outdated=%v, want 3-4 live", n.start, n.end, n.outdated)
	}
}

func TestOutdatedFileNoteIsClampedWhenTheFileShrinks(t *testing.T) {
	t.Parallel()
	d, n := abcDoc(t)
	reload(d, "a")
	if !n.outdated || n.start != 1 || n.end != 1 {
		t.Fatalf("note = %d-%d outdated=%v, want 1-1 outdated", n.start, n.end, n.outdated)
	}
}

func TestOutdatedFileNoteDoesNotJumpToAnAmbiguousCopy(t *testing.T) {
	t.Parallel()
	d := newOpenFile(fileSource{kind: srcWorktree}, "f.go")
	d.fill(fileContentMsg{lines: textLines("x", "}", "y", "}", "z")}, 10, 80)
	n, _ := d.addNote(2, 2, "this brace", "", "")
	reload(d, "x", "}!", "y", "}", "z") // its line was edited; another "}" exists
	if !n.outdated || n.start != 2 {
		t.Fatalf("note = line %d outdated=%v, want it to stay outdated on line 2", n.start, n.outdated)
	}
}

func TestFileNoteFollowsAUniqueMovedBlockAfterGoingOutdated(t *testing.T) {
	t.Parallel()
	d, n := abcDoc(t)
	reload(d, "a", "b", "e")           // c, d deleted → outdated
	reload(d, "a", "b", "e", "c", "d") // …and pasted at the end
	if n.outdated || n.start != 4 || n.end != 5 {
		t.Fatalf("note = %d-%d outdated=%v, want 4-5 live", n.start, n.end, n.outdated)
	}
}

// Review Focus 5.
func TestFileNotesSurviveAnEmptiedAndRestoredFile(t *testing.T) {
	t.Parallel()
	d, n := abcDoc(t)
	d.fill(fileContentMsg{lines: []contentLine{{text: "(empty file)"}}, reload: true}, 10, 80) // a placeholder
	if len(d.notes) != 1 || n.start != 3 {
		t.Fatalf("a placeholder fill touched the notes: %+v", n)
	}
	d.fill(fileContentMsg{err: errTestLoad, reload: true}, 10, 80)
	if len(d.notes) != 1 {
		t.Fatal("a failed load dropped the notes")
	}
	reload(d, "a", "b", "c", "d", "e")
	if n.outdated || n.start != 3 || n.end != 4 {
		t.Fatalf("after the file came back: %d-%d outdated=%v, want 3-4 live", n.start, n.end, n.outdated)
	}
	big := make([]string, 6000) // a rewrite far past any alignment guard
	for i := range big {
		big[i] = "q" + strings.Repeat("z", i%7)
	}
	reload(d, big...)
	if !n.outdated || n.start < 1 || n.end > len(big) {
		t.Fatalf("after a full rewrite: %d-%d outdated=%v", n.start, n.end, n.outdated)
	}
}
