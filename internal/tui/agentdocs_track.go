package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
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
	return m, tea.Batch(m.syncDocNotes(), m.waitDocsCmd())
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
