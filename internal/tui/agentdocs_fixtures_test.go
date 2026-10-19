package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
)

// The fixtures the retired note_add / overview_add steer verbs used to be
// (session docs part 4): an agent now writes files, and a reader shows them
// through the store. Tests that pin what a reader DOES with a note or an
// overview build them here, straight on the store.

// addFileNote opens path in the background (as a navigate --background
// would), reads it, and puts a note on start..end as a reader does once it
// loads a noted file.
func addFileNote(t *testing.T, m Model, path string, start, end int, summary string) (Model, agentdocs.Note) {
	t.Helper()
	src := fileSource{kind: srcWorktree}
	d := m.openFiles.find(m.currentWorktree, docKey(src, path))
	if d == nil {
		d = m.newOpenFile(src, path)
		m = m.registerDoc(d)
	}
	m = pumpAll(t, m, m.loadDoc(d))
	n, err := d.addNote(start, end, summary, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return m, *n
}

// addOverviewDoc files an overview and shows it as the retired
// overview_add did: beside its code when the screen can take it (never
// with background), its anchors checked.
func addOverviewDoc(t *testing.T, m Model, title, text string, background bool) (Model, *openFile) {
	t.Helper()
	o, err := m.docs.AddOverview(domain.CheckoutKey(m.currentWorktree), m.currentWorktree, title, text)
	if err != nil {
		t.Fatal(err)
	}
	d := newOverviewDocFrom(o, m.docs.NextFileSeq())
	rows, inner := m.viewerGeom()
	d.layOut(rows, m.overviewWidth(inner))
	status := m.statusMsg
	m, _ = m.registerDocEv(d)
	var cmd tea.Cmd
	if !background && m.steerRefusal() == "" {
		m, cmd = m.showOverview(d)
	}
	m.statusMsg = status
	if cmd != nil {
		m = pumpAll(t, m, cmd)
	}
	m.docs.CheckAnchorsIn(d.root, d.id())
	if o, ok := m.docs.OverviewIn(d.root, d.id()); ok {
		rows, inner := m.viewerGeom()
		d.adoptOverview(o, rows, m.overviewWidth(inner))
	}
	return m, d
}
