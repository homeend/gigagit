package tui

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
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
