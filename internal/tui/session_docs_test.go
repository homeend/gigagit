package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// attachedDocsModel is privateDocsModel with its worktree attached to a
// session-docs directory of the test's own (spec 2026-10-18-gg-session-docs).
func attachedDocsModel(t *testing.T) (m Model, root, dir string) {
	t.Helper()
	m = privateDocsModel(t)
	root = domain.CheckoutKey(m.currentWorktree)
	dir = filepath.Join(t.TempDir(), "docs")
	m.docsDirOf = func(string) string { return dir } // the test's own directory, whatever the worktree
	m = m.attachShownRoots()
	if m.docs.Attached(root) != dir {
		t.Fatalf("attached = %q", m.docs.Attached(root))
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
	// A memory root (no Attach) still numbers its overviews f<n> — the
	// store's id, whatever instance number the document carries.
	pm := privateDocsModel(t)
	_, pd := addOverviewViaSteer(t, pm, "Mem", "[a](a.txt:1)")
	if pd.id() != "f1" || pd.id() != pd.ovID || "f"+strconv.FormatInt(pd.seq, 10) == pd.id() {
		t.Fatalf("memory id = %q (ovID %q, seq %d)", pd.id(), pd.ovID, pd.seq)
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
	m, _ = m.deleteDoc(d)
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
	others := len(m.openFiles.list(m.currentWorktree)) - 1 // the file its anchor opened, if any
	m.docs.Reload(domain.CheckoutKey(m.currentWorktree))
	m, _ = m.onAgentDocsChanged()
	if m.docShown(d) || m.openFiles.has(m.currentWorktree, d) || m.statusMsg != `overview ov-1 "Tour" was deleted` {
		t.Fatalf("shown=%v listed=%v status=%q", m.docShown(d), m.openFiles.has(m.currentWorktree, d), m.statusMsg)
	}
	if n := len(m.openFiles.list(m.currentWorktree)); n != others {
		t.Fatalf("%d other documents left, want %d: nothing else closes", n, others)
	}
}

// writeRawDoc writes a document file verbatim (no encoder, no reload).
func writeRawDoc(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Without a resolved inbox nothing attaches; once the snapshot target lands
// (the inbox's beat) the home worktree is attached and its documents listed.
func TestTheHomeWorktreeAttachesWhenTheInboxResolves(t *testing.T) {
	t.Parallel()
	m := privateDocsModel(t)
	root := domain.CheckoutKey(m.currentWorktree)
	m.snapshotCommonDir, m.snapshotWorktree = "", ""
	if m.docsDirFor(m.currentWorktree) != "" || m.docs.Attached(root) != "" {
		t.Fatal("attached with no inbox")
	}
	dir := filepath.Join(t.TempDir(), "docs")
	wt := m.currentWorktree
	m.docsDirOf = func(w string) string {
		if model.SamePath(w, wt) {
			return dir
		}
		return ""
	}
	writeRawDoc(t, dir, "ov-1.md", "+++\ntitle = \"Tour\"\n+++\n[a](a.txt:1)\n")
	tm, _ := m.Update(snapshotTargetMsg{svc: m.svc, commonDir: "/c", worktree: m.currentWorktree})
	m = tm.(Model)
	t.Cleanup(m.docs.Close)
	if m.docs.Attached(root) != dir {
		t.Fatalf("attached = %q", m.docs.Attached(root))
	}
	if d, why := m.findOverview("ov-1"); d == nil {
		t.Fatal(why)
	}
}

// A note file for a path that is not open opens nothing; the store counts it
// and the file shows the box once opened and loaded.
func TestANoteOnAClosedFileWaitsForTheFile(t *testing.T) {
	t.Parallel()
	m, root, dir := attachedDocsModel(t)
	writeSessionNote(t, m, dir, "n-1", "a.txt", 2, 2, "s", []string{"x2"})
	m, _ = m.onAgentDocsChanged()
	if len(m.openFiles.list(m.currentWorktree)) != 0 || m.docs.NoteCount(root, "a.txt") != 1 {
		t.Fatalf("open=%d count=%d", len(m.openFiles.list(m.currentWorktree)), m.docs.NoteCount(root, "a.txt"))
	}
	d := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 5)
	alignDoc(m, d)
	d.syncNotes()
	if len(d.notes) != 1 || d.notes[0].Start != 2 {
		t.Fatalf("notes = %+v", d.notes)
	}
}

// An unusable docs directory attaches nothing and says nothing: the TUI
// runs without session rows.
func TestAnUnusableDocsDirIsSilent(t *testing.T) {
	t.Parallel()
	m := privateDocsModel(t)
	file := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.docsDirOf = func(string) string { return filepath.Join(file, "docs") } // MkdirAll fails: a file in the way
	m.statusMsg = "quiet"
	m = m.attachShownRoots()
	if m.docs.Attached(domain.CheckoutKey(m.currentWorktree)) != "" || m.statusMsg != "quiet" {
		t.Fatalf("attached=%q status=%q", m.docs.Attached(domain.CheckoutKey(m.currentWorktree)), m.statusMsg)
	}
}

// A session overview's row names its author and age; a memory overview's
// row is as before.
func TestASessionOverviewRowShowsAuthorAndAge(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "ov-1", "Tour", "2026-01-01T22:00:00Z", "[a](a.txt:1) [b](a.txt:2)")
	m, _ = m.onAgentDocsChanged()
	d, _ := m.findOverview("ov-1")
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	if got := openFileRowText(d, false, now); got != "○ Tour  overview · 2 anchors · agent · 2h" {
		t.Fatalf("row = %q", got)
	}
	pm := privateDocsModel(t)
	_, pd := addOverviewViaSteer(t, pm, "Mem", "[a](a.txt:1)")
	if got := openFileRowText(pd, true, now); got != "● Mem  overview · 1 anchors" {
		t.Fatalf("memory row = %q", got)
	}
}

// readOnly makes dir refuse unlinks for the test's duration.
func readOnly(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

// A delete the directory refuses keeps the row and the file, and says so —
// never "deleted".
func TestAFailedDeleteKeepsTheRowAndSaysSo(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a directory mode does not refuse the unlink here")
	}
	m, _, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "ov-1", "Tour", "", "[a](a.txt:1)")
	d := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 5)
	writeSessionNote(t, m, dir, "n-1", "a.txt", 2, 2, "one", []string{"x2"})
	alignDoc(m, d)
	m, _ = m.onAgentDocsChanged()
	ov, _ := m.findOverview("ov-1")
	readOnly(t, dir)
	tm, _ := m.confirmSessionOverviewDelete(ov)
	m = tm.(Model)
	tm, _ = m.modal.onResolve(m, "Delete")
	m = tm.(Model)
	if _, err := os.Stat(filepath.Join(dir, "ov-1.md")); err != nil || !m.openFiles.has(m.currentWorktree, ov) || m.statusMsg != `could not delete overview ov-1 "Tour"` {
		t.Fatalf("file err=%v listed=%v status=%q", err, m.openFiles.has(m.currentWorktree, ov), m.statusMsg)
	}
	tm, _ = m.confirmFileNoteDelete(d, d.notes[0])
	m = tm.(Model)
	tm, _ = m.modal.onResolve(m, "Delete")
	m = tm.(Model)
	if len(d.notes) != 1 || m.statusMsg != "could not delete note n-1" {
		t.Fatalf("notes=%d status=%q", len(d.notes), m.statusMsg)
	}
	tm, _ = m.confirmFileNotesDelete(d)
	m = tm.(Model)
	tm, _ = m.modal.onResolve(m, "Delete")
	m = tm.(Model)
	if len(d.notes) != 1 || m.statusMsg != "could not delete the notes on a.txt" {
		t.Fatalf("notes=%d status=%q", len(d.notes), m.statusMsg)
	}
}

// "Delete all notes" counts the store's notes, not the copy a stale document
// has not adopted yet.
func TestDeleteAllNotesPromptCountsTheStore(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	d := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 5)
	writeSessionNote(t, m, dir, "n-1", "a.txt", 2, 2, "one", []string{"other"}) // not these lines: the copy stays empty
	m, _ = m.onAgentDocsChanged()
	if len(d.notes) != 0 || !d.hasNotes() {
		t.Fatalf("fixture: copy=%d hasNotes=%v", len(d.notes), d.hasNotes())
	}
	tm, _ := m.confirmFileNotesDelete(d)
	m = tm.(Model)
	if m.modal == nil || m.modal.req.Prompt != "Delete the note on a.txt?" {
		t.Fatalf("modal = %+v", m.modal)
	}
}

// The same id in two attached worktrees: the documents stay apart — their
// tags differ, deleting one leaves the other's file, export resolves by root.
func TestSameIdInTwoWorktreesStaysApart(t *testing.T) {
	t.Parallel()
	m, rootA, dirA := attachedDocsModel(t)
	wtB, dirB := t.TempDir(), filepath.Join(t.TempDir(), "docs")
	rootB := domain.CheckoutKey(wtB)
	if err := m.docs.Attach(rootB, wtB, dirB); err != nil {
		t.Fatal(err)
	}
	writeSessionOverview(t, m, dirA, "same", "A", "2026-01-01T22:00:00Z", "[a](a.txt:1)")
	writeSessionOverview(t, m, dirB, "same", "B", "2026-01-01T22:00:00Z", "[a](a.txt:1)")
	m.docs.Reload(rootB)
	m, _ = m.onAgentDocsChanged()
	dA, _ := m.findOverview("same")
	oB, _ := m.docs.OverviewIn(rootB, "same")
	dB := newOverviewDocFrom(oB, m.docs.NextFileSeq())
	if dA == nil || dA.tag == dB.tag {
		t.Fatalf("tags: %q %q", dA.tag, dB.tag)
	}
	if src, err := domain.AgentOverviewSource(m.docs, "same", rootB); err != nil || src.Title != "B" {
		t.Fatalf("export by root: %+v %v", src, err)
	}
	m, ok := m.deleteDoc(dA)
	if !ok {
		t.Fatal("delete refused")
	}
	if _, err := os.Stat(filepath.Join(dirA, "same.md")); !os.IsNotExist(err) {
		t.Fatal("A's file is still there")
	}
	if _, err := os.Stat(filepath.Join(dirB, "same.md")); err != nil {
		t.Fatal("B's file went with A's")
	}
	_ = rootA
}

// A worker's worktree attached for its tours is detached with the shown
// roots (a repo switch, gg's exit): no follower outlives the repository.
func TestTourRootsDetachWithTheShownRoots(t *testing.T) {
	t.Parallel()
	m := privateDocsModel(t)
	wt, dir := t.TempDir(), filepath.Join(t.TempDir(), "docs")
	m.docsDirOf = func(w string) string {
		if model.SamePath(w, wt) {
			return dir
		}
		return ""
	}
	m.worktrees = []model.Worktree{{Path: wt}}
	m.attachTourRoot(wt)
	if m.docs.Attached(domain.CheckoutKey(wt)) != dir {
		t.Fatal("the tour root did not attach")
	}
	m.detachShownRoots()
	if m.docs.Attached(domain.CheckoutKey(wt)) != "" {
		t.Fatal("the tour root outlived the shown roots")
	}
}

// Delete overview is offered wherever the overview is on screen: the split
// (a wide terminal) and the full-screen viewer (a narrow one).
func TestDeleteOverviewRowInSplitAndViewer(t *testing.T) {
	t.Parallel()
	for _, width := range []int{200, 59} {
		m, _, dir := attachedDocsModel(t)
		m.width = width
		writeSessionOverview(t, m, dir, "ov-1", "Tour", "", "[a](a.txt:1)")
		m, _ = m.onAgentDocsChanged()
		d, _ := m.findOverview("ov-1")
		m, _ = m.showOverview(d)
		_, inSplit := m.topLayer().(*splitView)
		if inSplit != (width == 200) {
			t.Fatalf("width %d: split=%v", width, inSplit)
		}
		found := false
		for _, r := range availableActions(m) {
			found = found || r.id == "overview-delete"
		}
		if !found {
			t.Fatalf("width %d (split=%v): no Delete overview row", width, inSplit)
		}
	}
}

// An author is data from a file: control characters never reach the row.
func TestAnOverviewRowSanitizesItsAuthor(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "ov-1", "Tour", "", "[a](a.txt:1)")
	m, _ = m.onAgentDocsChanged()
	d, _ := m.findOverview("ov-1")
	d.ovAuthor = "bot\x1b[31m\nx"
	if got := openFileRowText(d, false, time.Time{}); strings.ContainsAny(got, "\x1b\n") {
		t.Fatalf("row = %q", got)
	}
}

// `gg session files focus <document id>` sends the id as a PATH (only f<n>
// reads as an id to the CLI): a path that matches nothing is tried as an id.
func TestFindOpenFileTriesAPathAsADocumentID(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "20261019T000000-ab12", "Tour", "2026-01-01T22:00:00Z", "[a](a.txt:1)")
	m, _ = m.onAgentDocsChanged()
	d := m.findOpenFile("", "20261019T000000-ab12")
	if d == nil || d.id() != "20261019T000000-ab12" {
		t.Fatalf("findOpenFile by path = %v", d)
	}
	if m.findOpenFile("", "nope.txt") != nil {
		t.Fatal("an unknown path must match nothing")
	}
}
