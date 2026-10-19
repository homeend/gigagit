package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// attachedDocsModel is privateDocsModel with its worktree attached to a
// session-docs directory of the test's own (spec 2026-10-18-gg-session-docs).
func attachedDocsModel(t *testing.T) (m Model, root, dir string) {
	t.Helper()
	m = privateDocsModel(t)
	root = domain.CheckoutKey(m.currentWorktree)
	dir = filepath.Join(t.TempDir(), "docs")
	if err := m.docs.Attach(root, m.currentWorktree, dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.docs.Close)
	return m, root, dir
}

// writeSessionOverview lands an overview document as an agent would (whole
// file, gg's own encoder) and has the store read the directory again.
func writeSessionOverview(t *testing.T, m Model, dir, id, title, created, text string) {
	t.Helper()
	var ts time.Time
	if created != "" {
		var err error
		if ts, err = time.Parse(time.RFC3339, created); err != nil {
			t.Fatal(err)
		}
	}
	data, err := agentdocs.EncodeOverviewFile(agentdocs.OverviewFile{Title: title, Author: "agent", Created: ts, Text: text})
	if err != nil {
		t.Fatal(err)
	}
	if err := agentdocs.WriteDoc(dir, id, agentdocs.KindOverview, data, true); err != nil {
		t.Fatal(err)
	}
	m.docs.Reload(domain.CheckoutKey(m.currentWorktree))
}

// writeSessionNote lands a note document on path's lines start..end.
func writeSessionNote(t *testing.T, m Model, dir, id, path string, start, end int, summary string, lines []string) {
	t.Helper()
	data, err := agentdocs.EncodeNoteFile(agentdocs.NoteFile{Path: path, Start: start, End: end, Summary: summary, Author: "agent", Lines: lines})
	if err != nil {
		t.Fatal(err)
	}
	if err := agentdocs.WriteDoc(dir, id, agentdocs.KindNote, data, true); err != nil {
		t.Fatal(err)
	}
	m.docs.Reload(domain.CheckoutKey(m.currentWorktree))
}

// alignDoc does for a test what a document's load does for the TUI
// (alignDocNotes): hands the store the lines the document shows, so notes
// written against them are placed and adopted.
func alignDoc(m Model, d *openFile) {
	m.docs.Align(d.root, d.path, rawOf(d.p.lines))
}

// A session overview joins the open-files list under its FILE id, carrying
// its author and created stamp; a memory overview keeps f<n>.
func TestASessionOverviewIsListedUnderItsFileID(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "20261019T000000-ab12", "Tour", "2026-01-01T22:00:00Z", "[a](a.txt:1)")
	m, _ = m.onAgentDocsChanged()
	d, why := m.findOverview("20261019T000000-ab12")
	if d == nil {
		t.Fatal(why)
	}
	if d.id() != "20261019T000000-ab12" || d.ovAuthor != "agent" || d.ovCreated.IsZero() || d.key() != docKey(fileSource{kind: srcOverview}, "overview-20261019T000000-ab12.md") {
		t.Fatalf("id=%q author=%q created=%v key=%q", d.id(), d.ovAuthor, d.ovCreated, d.key())
	}
	files := m.openFilesProto()
	if len(files) != 1 || files[0].ID != "20261019T000000-ab12" || files[0].Source != "overview" {
		t.Fatalf("files = %+v", files)
	}
	// A memory root (no Attach) still numbers its overviews.
	pm := privateDocsModel(t)
	_, pd := addOverviewViaSteer(t, pm, "Mem", "[a](a.txt:1)")
	if pd.id() != "f"+strconv.FormatInt(pd.seq, 10) {
		t.Fatalf("memory id = %q", pd.id())
	}
}

// A replaced file keeps the document (same id, new text).
func TestAReplacedSessionOverviewIsAdoptedInPlace(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "ov-1", "Tour", "", "[a](a.txt:1)")
	m, _ = m.onAgentDocsChanged()
	d, _ := m.findOverview("ov-1")
	writeSessionOverview(t, m, dir, "ov-1", "Tour 2", "", "[a](a.txt:1) [b](a.txt:2)")
	m, _ = m.onAgentDocsChanged()
	if d2, _ := m.findOverview("ov-1"); d2 != d || d.title != "Tour 2" || len(d.ov.anchors) != 2 {
		t.Fatalf("doc=%v title=%q anchors=%d", d2 == d, d.title, len(d.ov.anchors))
	}
}

// X on a shown session overview is local: it leaves the screen, stays in
// the list and in the store, and its file stays.
func TestClosingASessionOverviewIsLocal(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "ov-1", "Tour", "", "[a](a.txt:1)")
	m, _ = m.onAgentDocsChanged()
	d, _ := m.findOverview("ov-1")
	m, _ = m.showOverview(d)
	if !m.docShown(d) {
		t.Fatal("fixture: not shown")
	}
	m = m.closeDoc(d)
	if m.docShown(d) || !m.openFiles.has(m.currentWorktree, d) || !d.backgrounded {
		t.Fatalf("shown=%v listed=%v bg=%v", m.docShown(d), m.openFiles.has(m.currentWorktree, d), d.backgrounded)
	}
	if _, err := os.Stat(filepath.Join(dir, "ov-1.md")); err != nil {
		t.Fatal("close unlinked the file")
	}
}

// Closing a file with notes leaves the notes in the store (the files on
// disk): a reader deletes only what the user asked for.
func TestClosingAFileKeepsItsNotes(t *testing.T) {
	t.Parallel()
	m, root, dir := attachedDocsModel(t)
	d := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 5)
	writeSessionNote(t, m, dir, "n-1", "a.txt", 2, 2, "s", []string{"x2"})
	alignDoc(m, d)
	m, _ = m.onAgentDocsChanged()
	if len(d.notes) != 1 {
		t.Fatalf("notes = %+v", d.notes)
	}
	m = m.closeDoc(d)
	if m.openFiles.has(m.currentWorktree, d) || m.docs.NoteCount(root, "a.txt") != 1 {
		t.Fatalf("listed=%v notes=%d", m.openFiles.has(m.currentWorktree, d), m.docs.NoteCount(root, "a.txt"))
	}
	if _, err := os.Stat(filepath.Join(dir, "n-1.toml")); err != nil {
		t.Fatal("close unlinked the note")
	}
}

// deleteDoc unlinks the overview's file; the row and the screen follow.
func TestDeletingASessionOverviewUnlinksItsFile(t *testing.T) {
	t.Parallel()
	m, root, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "ov-1", "Tour", "", "[a](a.txt:1)")
	m, _ = m.onAgentDocsChanged()
	d, _ := m.findOverview("ov-1")
	m, _ = m.showOverview(d)
	m = m.deleteDoc(d)
	if _, err := os.Stat(filepath.Join(dir, "ov-1.md")); !os.IsNotExist(err) {
		t.Fatal("the file is still there")
	}
	if _, ok := m.docs.OverviewIn(root, "ov-1"); ok || m.openFiles.has(m.currentWorktree, d) || m.docShown(d) {
		t.Fatal("the overview is still held")
	}
}

// The D key in the open-files window asks first; Delete unlinks, Cancel
// keeps. A row with nothing to delete only says so.
func TestDInTheOpenFilesWindowAsksThenDeletes(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "ov-1", "Tour", "", "[a](a.txt:1)")
	plain := bgDoc(m, fileSource{kind: srcWorktree}, "b.txt", 3)
	m, _ = m.onAgentDocsChanged()
	d, _ := m.findOverview("ov-1")
	m, _ = m.openSessionsPopup(false)
	p := m.topLayer().(*sessionsPopup)
	p.tab = tabFiles
	p.refresh(m)
	cursorOn := func(want *openFile) {
		for i, f := range p.files {
			if f == want {
				p.sel = i
			}
		}
	}
	cursorOn(plain) // nothing to delete
	m, _ = p.update(m, keyMsg("D"))
	if m.modal != nil || m.statusMsg != "nothing to delete on this row" {
		t.Fatalf("modal=%v status=%q", m.modal != nil, m.statusMsg)
	}
	cursorOn(d)
	m, _ = p.update(m, keyMsg("D"))
	if m.modal == nil || m.modal.req.ID != "session-overview-delete" || m.modal.sel != 1 {
		t.Fatalf("modal = %+v", m.modal)
	}
	tm, _ := m.modal.onResolve(m, "Cancel")
	m = tm.(Model)
	if _, err := os.Stat(filepath.Join(dir, "ov-1.md")); err != nil {
		t.Fatal("Cancel unlinked the file")
	}
	m, _ = p.update(m, keyMsg("D"))
	tm, _ = m.modal.onResolve(m, "Delete")
	m = tm.(Model)
	if _, err := os.Stat(filepath.Join(dir, "ov-1.md")); !os.IsNotExist(err) {
		t.Fatal("Delete left the file")
	}
}

// d on a note box asks, then unlinks that one note; "delete all notes on
// this file" unlinks every note file with that path.
func TestNoteDeletionAsksThenUnlinks(t *testing.T) {
	t.Parallel()
	m, root, dir := attachedDocsModel(t)
	d := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 5)
	writeSessionNote(t, m, dir, "n-1", "a.txt", 2, 2, "one", []string{"x2"})
	writeSessionNote(t, m, dir, "n-2", "a.txt", 4, 4, "two", []string{"x4"})
	alignDoc(m, d)
	m, _ = m.onAgentDocsChanged()
	tm, _ := m.confirmFileNoteDelete(d, d.notes[0])
	m = tm.(Model)
	if m.modal == nil || m.modal.req.ID != "session-note-delete" {
		t.Fatalf("modal = %+v", m.modal)
	}
	tm, _ = m.modal.onResolve(m, "Delete")
	m = tm.(Model)
	if m.docs.NoteCount(root, "a.txt") != 1 || m.statusMsg != "note n-1 deleted" {
		t.Fatalf("count=%d status=%q", m.docs.NoteCount(root, "a.txt"), m.statusMsg)
	}
	tm, _ = m.confirmFileNotesDelete(d)
	m = tm.(Model)
	tm, _ = m.modal.onResolve(m, "Delete")
	m = tm.(Model)
	if m.docs.NoteCount(root, "a.txt") != 0 || m.statusMsg != "deleted the note" {
		t.Fatalf("count=%d status=%q", m.docs.NoteCount(root, "a.txt"), m.statusMsg)
	}
	if _, err := os.Stat(filepath.Join(dir, "n-2.toml")); !os.IsNotExist(err) {
		t.Fatal("n-2.toml is still there")
	}
}

// A session overview whose file disappears closes on screen with a word.
func TestADeletedSessionOverviewClosesWithAWord(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "ov-1", "Tour", "", "[a](a.txt:1)")
	m, _ = m.onAgentDocsChanged()
	d, _ := m.findOverview("ov-1")
	m, _ = m.showOverview(d)
	if err := os.Remove(filepath.Join(dir, "ov-1.md")); err != nil {
		t.Fatal(err)
	}
	m.docs.Reload(domain.CheckoutKey(m.currentWorktree))
	m, _ = m.onAgentDocsChanged()
	if m.docShown(d) || m.openFiles.has(m.currentWorktree, d) || m.statusMsg != `overview ov-1 "Tour" was deleted` {
		t.Fatalf("shown=%v listed=%v status=%q", m.docShown(d), m.openFiles.has(m.currentWorktree, d), m.statusMsg)
	}
}

// The agent's overview_rm removes its document (the file goes), and says so.
func TestOverviewRmRemovesTheDocument(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "ov-1", "Tour", "", "[a](a.txt:1)")
	m, _ = m.onAgentDocsChanged()
	m, r := applyOverview(t, m, steer.Command{ID: "rm-1", Cmd: "overview_rm", FileID: "ov-1", Wait: true})
	if !r.OK || r.Detail != "removed ov-1" {
		t.Fatalf("reply = %+v", r)
	}
	if _, err := os.Stat(filepath.Join(dir, "ov-1.md")); !os.IsNotExist(err) {
		t.Fatal("the file is still there")
	}
	if d, _ := m.findOverview("ov-1"); d != nil {
		t.Fatal("still listed")
	}
}
