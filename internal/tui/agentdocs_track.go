package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// The TUI's side of the agent-docs store (spec 2026-10-01-agent-docs-web):
// the open files keep COPIES of their notes, refreshed whenever the store
// changes — in this TUI, or in the gg web page it hosts.

// docsTrack is the TUI's one subscription to the store; it lives on a
// pointer so the value-copied Model shares it.
type docsTrack struct {
	ch     <-chan struct{}
	cancel func()
}

func newDocsTrack(s *agentdocs.Store) *docsTrack {
	ch, cancel := s.Subscribe()
	return &docsTrack{ch: ch, cancel: cancel}
}

// agentDocsChangedMsg: something in the store changed. onAgentDocsChanged
// re-arms the wait.
type agentDocsChangedMsg struct{}

func (m Model) waitDocsCmd() tea.Cmd {
	if m.quiet {
		return nil // headless: never-ending (headless.go)
	}
	if m.docsSub == nil {
		return nil // a Model built as a literal (tests)
	}
	ch := m.docsSub.ch
	return func() tea.Msg {
		<-ch
		return agentDocsChangedMsg{}
	}
}

// onAgentDocsChanged refreshes the current worktree's documents from the
// store and waits for the next change.
func (m Model) onAgentDocsChanged() (Model, tea.Cmd) {
	m, cmd := m.syncAgentDocs()
	return m, tea.Batch(cmd, m.waitDocsCmd())
}

// syncAgentDocs brings the current worktree's documents up to the store:
// their notes, and the overviews.
func (m Model) syncAgentDocs() (Model, tea.Cmd) {
	cmd := m.syncDocNotes()
	return m.syncOverviews(), cmd
}

// syncOverviews follows the store's overviews of this worktree: one that left
// it (closed in the browser) closes here — on screen, with a word in the
// status line; one whose text, title or check changed is laid out or painted
// again; one this list lacks joins it in the background.
func (m Model) syncOverviews() Model {
	if m.docs == nil || m.openFiles == nil {
		return m
	}
	rows, inner := m.viewerGeom()
	width := m.overviewWidth(inner)
	listed := map[string]bool{}
	var gone []*openFile
	for _, d := range m.openFiles.list(m.currentWorktree) {
		if d.ov == nil || d.src.kind != srcOverview { // a review's stored overview is not the store's
			continue
		}
		o, ok := m.docs.Overview(d.id())
		if !ok {
			gone = append(gone, d)
			continue
		}
		listed[o.ID] = true
		d.adoptOverview(o, rows, width)
	}
	for _, d := range gone {
		shown := m.docShown(d)
		m = m.closeDoc(d)
		if shown {
			m.statusMsg = i18n.T("overview %s was closed in the browser", d.overviewLabel())
		}
	}
	for _, o := range m.docs.Overviews(domain.CheckoutKey(m.currentWorktree)) {
		if listed[o.ID] {
			continue
		}
		d := newOverviewDocFrom(o)
		d.layOut(rows, width)
		d.adoptOverview(o, rows, width)
		status := m.statusMsg
		m = m.registerDoc(d)
		m.statusMsg = status // an agent's overview never takes over the status line
	}
	return m
}

// syncDocNotes re-reads every open document's notes; one whose notes sit on
// content it does not show is re-read from disk (its load aligns the store).
func (m Model) syncDocNotes() tea.Cmd {
	var cmds []tea.Cmd
	for _, d := range m.openFiles.list(m.currentWorktree) {
		if d.syncNotes() && !d.loading {
			cmds = append(cmds, m.reloadDocCmd(d))
		}
	}
	return tea.Batch(cmds...)
}

// adoptDoc files d under this Model's store and worktree as it joins the
// open-files list, and picks up any notes the store already has for it.
func (m Model) adoptDoc(d *openFile) {
	d.docs, d.root = m.docs, domain.CheckoutKey(m.currentWorktree)
	d.syncNotes()
}

// alignDocNotes aligns the store to a working-tree document's lines that
// just arrived from disk, before the fill draws them: the fill then adopts
// notes that sit on exactly these lines.
func (m Model) alignDocNotes(d *openFile, msg fileContentMsg) {
	if d.docs == nil || d.src.kind != srcWorktree || msg.err != nil || msg.img != nil || len(msg.lines) == 0 || !msg.lines[0].src {
		return
	}
	d.docs.Align(d.root, d.path, rawOf(msg.lines))
}
