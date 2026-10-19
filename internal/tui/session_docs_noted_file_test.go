package tui

import (
	"testing"
)

// A note written on a file the TUI has not opened lists that file in the
// background (as gg web's follow pass does), so the note shows and
// `gg session files focus <path>` reaches it — the retired note_add used
// to open the file itself.
func TestANoteOnAFileNotOpenListsItInTheBackground(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	src := fileSource{kind: srcWorktree}
	if d := m.openFiles.find(m.currentWorktree, docKey(src, "a.txt")); d != nil {
		t.Fatal("a.txt is open before the note")
	}
	writeSessionNote(t, m, dir, "20260101T220100-cd34", "a.txt", 2, 2, "why two?", []string{"line 2"})
	m, cmd := m.syncAgentDocs() // onAgentDocsChanged's batch carries the store's blocking wait
	m = pumpAll(t, m, cmd)
	d := m.openFiles.find(m.currentWorktree, docKey(src, "a.txt"))
	if d == nil {
		t.Fatal("the noted file is not listed")
	}
	if m.docShown(d) {
		t.Fatal("the noted file took the screen")
	}
	if len(d.notes) != 1 {
		t.Fatalf("notes on it = %d, want the one just written", len(d.notes))
	}
	if f := m.findOpenFile("", "a.txt"); f != d {
		t.Fatal("files focus by path does not find it")
	}
}
