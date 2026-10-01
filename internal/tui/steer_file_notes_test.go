package tui

import (
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/steer"
)

func noteAddCmd(id, file string, start, end int, summary string) steer.Command {
	return steer.Command{ID: id, Cmd: "note_add", File: file, Start: start, End: end, Summary: summary, Wait: true}
}

func awaitNote(t *testing.T, m Model, id string) steer.Reply {
	t.Helper()
	r, ok := steer.AwaitReply(m.steerDir, id, time.Second)
	if !ok {
		t.Fatalf("no reply to %s", id)
	}
	return r
}

func TestNoteAddOpensTheFileInTheBackgroundAndAnswersWithTheNote(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.filterTyping = true // never refused: nothing on screen moves
	nm, cmd := m.applySteer(noteAddCmd("n-1", "a.txt", 18, 19, "the edited line"))
	nm = pumpAll(t, nm, cmd)
	if layerOf[*fileViewer](nm) != nil || nm.filesPreview != nil {
		t.Fatal("note add put the file on screen")
	}
	d := nm.openFiles.find(nm.currentWorktree, docKey(fileSource{kind: srcWorktree}, "a.txt"))
	if d == nil || len(d.notes) != 1 {
		t.Fatalf("doc = %+v, want a.txt open with one note", d)
	}
	r := awaitNote(t, nm, "n-1")
	n := d.notes[0]
	if !r.OK || r.Detail != "noted a.txt:18-19 as "+n.ID || len(r.Notes) != 1 {
		t.Fatalf("reply = %+v", r)
	}
	if w := r.Notes[0]; w.ID != n.ID || w.FileID != d.id() || w.Path != "a.txt" || w.Start != 18 || w.End != 19 || w.Summary != "the edited line" || w.Author != "agent" {
		t.Fatalf("wire note = %+v", w)
	}
}

func TestNoteAddOnAnOpenFileKeepsTheReadersPlace(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	d.p.cur = 9 // (the 40-line file fits the window: the top stays line 1)
	top, cur := d.p.sel, d.p.cur
	nm, cmd := m.applySteer(noteAddCmd("n-2", "a.txt", 5, 5, "s"))
	nm = pumpAll(t, nm, cmd) // its lines were never read from disk: re-read first
	r := awaitNote(t, nm, "n-2")
	if !r.OK || len(d.notes) != 1 || d.p.sel != top || d.p.cur != cur {
		t.Fatalf("reply=%+v notes=%d top=%d cur=%d — want one note, the reader's place untouched", r, len(d.notes), d.p.sel, d.p.cur)
	}
}

func TestNoteAddRefusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		c    steer.Command
		want string
	}{
		{"missing file", noteAddCmd("r-1", "nope.txt", 1, 1, "s"), "nope.txt is not in the working tree"},
		{"past the end", noteAddCmd("r-2", "a.txt", 39, 41, "s"), "line 41 is past the end of a.txt (40 lines)"},
		{"unknown id", steer.Command{ID: "r-3", Cmd: "note_add", FileID: "f999999", Start: 1, End: 1, Summary: "s", Wait: true}, "no open file f999999"},
		{"other worktree", func() steer.Command {
			c := noteAddCmd("r-4", "a.txt", 1, 1, "s")
			c.Worktree = "/somewhere/else"
			return c
		}(), ""},
	} {
		m := loadedNavModel(t)
		nm, cmd := m.applySteer(tc.c)
		nm = pumpAll(t, nm, cmd)
		r := awaitNote(t, nm, tc.c.ID)
		if r.OK || (tc.want != "" && r.Error != tc.want) {
			t.Errorf("%s: reply = %+v, want the error %q", tc.name, r, tc.want)
		}
	}
	for _, tc := range []struct {
		c    steer.Command
		want string
	}{
		{steer.Command{Cmd: "note_add", Start: 1, End: 1, Summary: "s"}, "note_add needs a file"},
		{steer.Command{Cmd: "note_add", File: "a.txt", Summary: "s"}, "a line number is 1-based"},
		{steer.Command{Cmd: "note_add", File: "a.txt", Start: 3, End: 2, Summary: "s"}, "the range ends before it starts"},
		{steer.Command{Cmd: "note_add", File: "a.txt", Start: 1, End: 1}, "a note needs a summary"},
		{steer.Command{Cmd: "note_show"}, "note_show needs a note id"},
		{steer.Command{Cmd: "note_rm"}, "note_rm needs a note id or a file"},
	} {
		if got := steerEnumRefusal(tc.c); got != tc.want {
			t.Errorf("steerEnumRefusal(%+v) = %q, want %q", tc.c, got, tc.want)
		}
	}
}

// Review Focus 4.
func TestNoteAddOnAFileClosedBeforeItLandsFails(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	nm, cmd := m.applySteer(noteAddCmd("c-1", "a.txt", 1, 1, "s"))
	d := nm.openFiles.find(nm.currentWorktree, docKey(fileSource{kind: srcWorktree}, "a.txt"))
	nm = nm.closeDoc(d) // the user pressed x before the load landed
	nm = pumpAll(t, nm, cmd)
	r := awaitNote(t, nm, "c-1")
	if r.OK || r.Error != "a.txt was closed before the note landed" {
		t.Fatalf("reply = %+v", r)
	}
}

func TestNoteListShowAndRemove(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	a, _ := d.addNote(2, 3, "first", "why", "")
	b, _ := d.addNote(9, 9, "second", "", "claude")

	nm, cmd := m.applySteer(steer.Command{ID: "l-1", Cmd: "note_list", Wait: true})
	runSteerCmd(t, cmd)
	if r := awaitNote(t, nm, "l-1"); !r.OK || len(r.Notes) != 2 || r.Notes[0].ID != a.ID || r.Notes[1].Author != "claude" {
		t.Fatalf("list = %+v", r)
	}
	if got := nm.openFilesProto(); len(got) != 1 || got[0].Notes != 2 {
		t.Fatalf("open files = %+v, want the note count 2", got)
	}

	nm, cmd = nm.applySteer(steer.Command{ID: "s-1", Cmd: "note_show", NoteID: a.ID, Wait: true})
	runSteerCmd(t, cmd)
	r := awaitNote(t, nm, "s-1")
	if !r.OK || len(r.Notes) != 1 || len(r.Notes[0].Text) != 2 || r.Notes[0].Text[0] != "line 2" || r.Notes[0].Rationale != "why" {
		t.Fatalf("show = %+v", r)
	}

	nm, cmd = nm.applySteer(steer.Command{ID: "d-1", Cmd: "note_rm", NoteID: a.ID, Wait: true})
	runSteerCmd(t, cmd)
	if r := awaitNote(t, nm, "d-1"); !r.OK || r.Detail != "removed "+a.ID || len(d.notes) != 1 {
		t.Fatalf("rm = %+v notes=%d", r, len(d.notes))
	}
	nm, cmd = nm.applySteer(steer.Command{ID: "s-2", Cmd: "note_show", NoteID: a.ID, Wait: true})
	runSteerCmd(t, cmd)
	if r := awaitNote(t, nm, "s-2"); r.OK || r.Error != "no note "+a.ID {
		t.Fatalf("show of a removed note = %+v", r)
	}
	nm, cmd = nm.applySteer(steer.Command{ID: "d-2", Cmd: "note_rm", File: "a.txt", Wait: true})
	runSteerCmd(t, cmd)
	if r := awaitNote(t, nm, "d-2"); !r.OK || r.Detail != "removed 1 notes from a.txt" || len(d.notes) != 0 {
		t.Fatalf("clear = %+v", r)
	}
	_ = b
	nm, cmd = nm.applySteer(steer.Command{ID: "l-2", Cmd: "note_list", Wait: true})
	runSteerCmd(t, cmd)
	if r := awaitNote(t, nm, "l-2"); !r.OK || len(r.Notes) != 0 || r.Detail != "no notes" {
		t.Fatalf("empty list = %+v", r)
	}
}
