package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
)

func TestNewUsesTheSharedStore(t *testing.T) {
	t.Parallel()
	if m := newTestModel(t); m.docs != agentdocs.Shared() {
		t.Fatal("tui.New must hold agentdocs.Shared() — the store a hosted page shares")
	}
}

// privateDocsModel is loadedNavModel over a store of its own.
func privateDocsModel(t *testing.T) Model {
	t.Helper()
	m := loadedNavModel(t)
	m.docs = agentdocs.New()
	return m
}

// A dismiss made elsewhere (the browser) leaves the TUI's copy on the next
// change message; the document stays open.
func TestANoteRemovedElsewhereLeavesTheDocument(t *testing.T) {
	t.Parallel()
	m := privateDocsModel(t)
	d := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 5)
	n, err := d.addNote(2, 2, "s", "", "")
	if err != nil {
		t.Fatal(err)
	}
	m.docs.RemoveNote(n.ID)
	m, _ = m.onAgentDocsChanged()
	if len(d.notes) != 0 || m.openFiles.find(m.currentWorktree, d.key()) != d {
		t.Fatalf("notes = %d, doc listed = %v", len(d.notes), m.openFiles.find(m.currentWorktree, d.key()) == d)
	}
}

// A note filed elsewhere (a hosted page's store write) shows on the next
// change message.
func TestANoteAddedElsewhereShows(t *testing.T) {
	t.Parallel()
	m := privateDocsModel(t)
	d := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 5)
	if _, err := m.docs.AddNote(d.root, d.path, rawOf(d.p.lines), 3, 3, "from elsewhere", "", ""); err != nil {
		t.Fatal(err)
	}
	m, _ = m.onAgentDocsChanged()
	if len(d.notes) != 1 || d.notes[0].Start != 3 {
		t.Fatalf("notes = %+v", d.notes)
	}
}

func TestXOnANotedFileClearsItsNotesInTheStore(t *testing.T) {
	t.Parallel()
	m := privateDocsModel(t)
	d := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 5)
	if _, err := d.addNote(2, 2, "s", "", ""); err != nil {
		t.Fatal(err)
	}
	m = m.closeDoc(d)
	if got := m.docs.NoteCount(domain.CheckoutKey(m.currentWorktree), "a.txt"); got != 0 {
		t.Fatalf("store still holds %d notes after X", got)
	}
}

// Review Focus 1: notes aligned to content the document does not show are
// not adopted; the document is re-read instead.
func TestNotesAlignedToOtherContentTriggerAReRead(t *testing.T) {
	t.Parallel()
	m := privateDocsModel(t)
	d := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 5)
	if _, err := d.addNote(2, 2, "s", "", ""); err != nil {
		t.Fatal(err)
	}
	m.docs.Align(d.root, d.path, append([]string{"NEW"}, rawOf(d.p.lines)...)) // the web read a newer file
	m, cmd := m.onAgentDocsChanged()
	if d.notes[0].Start != 2 {
		t.Fatalf("adopted positions for content not shown: start = %d", d.notes[0].Start)
	}
	if cmd == nil || !d.loading {
		t.Fatal("want a re-read of the document")
	}
}

// The fill path aligns through the store: a reload with a line inserted
// above moves the note.
func TestAReloadAlignsTheNotesThroughTheStore(t *testing.T) {
	t.Parallel()
	m := privateDocsModel(t)
	d := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 5)
	n, err := d.addNote(3, 4, "s", "", "")
	if err != nil {
		t.Fatal(err)
	}
	m.filesPreview = d // a frame shows it, so liveDoc finds it
	tm, _ := m.Update(fileContentMsg{tag: d.tag, lines: textLines(append([]string{"NEW"}, rawOf(d.p.lines)...)...), reload: true})
	m = tm.(Model)
	if g := d.notes[0]; g.ID != n.ID || g.Start != 4 || g.End != 5 {
		t.Fatalf("note = %+v, want 4-5", g)
	}
}

// A file whose notes arrive through the store (filed by the page it hosts)
// follows the TUI's rule all the same: esc steps aside, only X closes.
func TestEscKeepsAFileWhoseNotesCameFromTheStore(t *testing.T) {
	t.Parallel()
	m := privateDocsModel(t)
	src := fileSource{kind: srcWorktree}
	d := bgDoc(m, src, "a.txt", 5)
	d.backgrounded = false // opened in the foreground
	m = m.pushLayer(&fileViewer{d})
	if _, err := m.docs.AddNote(d.root, d.path, rawOf(d.p.lines), 2, 2, "from elsewhere", "", ""); err != nil {
		t.Fatal(err)
	}
	m, _ = m.onAgentDocsChanged()
	m = m.escDoc(d)
	if m.openFiles.find(m.currentWorktree, d.key()) != d || m.docs.NoteCount(d.root, d.path) != 1 {
		t.Fatal("esc closed a noted file and dropped its notes")
	}
}
