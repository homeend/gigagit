package tui

// Deleting session documents (spec 2026-10-18-gg-session-docs §4.4): close
// is LOCAL — the document's file stays and its row stays in the open-files
// list — and DELETE unlinks the file, after the usual Delete/Cancel modal
// (Cancel highlighted). The TUI never deletes on its own.

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// heldOverview reports whether d is an agent's overview the store still
// holds (a session document, or a memory overview): the one kind a close
// keeps listed and a delete can unlink.
func (m Model) heldOverview(d *openFile) bool {
	if d.ov == nil || d.src.kind != srcOverview || m.docs == nil {
		return false
	}
	_, held := m.docs.OverviewIn(d.root, d.id())
	return held
}

// deleteDoc removes overview d's document through the store (its file is
// unlinked) and takes it off the screen and out of the list.
func (m Model) deleteDoc(d *openFile) Model {
	if m.heldOverview(d) {
		m.docs.RemoveOverviewIn(d.root, d.id())
	}
	if d.ov != nil {
		d.ov.forgetDiff()
	}
	m = m.detachDoc(d)
	m.openFiles.remove(m.currentWorktree, d)
	return m
}

// confirmSessionOverviewDelete asks before an agent's overview document goes.
func (m Model) confirmSessionOverviewDelete(d *openFile) (tea.Model, tea.Cmd) {
	m.modal = &decisionState{
		req: engine.DecisionRequest{ID: "session-overview-delete", Prompt: i18n.T("Delete this overview?") + "\n" + strconv.Quote(d.title),
			Options: []string{"Delete", "Cancel"}},
		sel: 1, // default highlight = Cancel
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt != "Delete" {
				return m, nil
			}
			m = m.deleteDoc(d)
			m.statusMsg = i18n.T("overview %s deleted", d.overviewLabel())
			return m, nil
		},
	}
	return m, nil
}

// confirmFileNoteDelete asks before one note on d goes.
func (m Model) confirmFileNoteDelete(d *openFile, n agentdocs.Note) (tea.Model, tea.Cmd) {
	m.modal = &decisionState{
		req: engine.DecisionRequest{ID: "session-note-delete", Prompt: i18n.T("Delete this note?") + "\n" + n.Summary,
			Options: []string{"Delete", "Cancel"}},
		sel: 1, // default highlight = Cancel
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt != "Delete" {
				return m, nil
			}
			d.removeNote(n.ID)
			m.statusMsg = i18n.T("note %s deleted", n.ID)
			return m, nil
		},
	}
	return m, nil
}

// confirmFileNotesDelete asks before every note on d goes.
func (m Model) confirmFileNotesDelete(d *openFile) (tea.Model, tea.Cmd) {
	n := len(d.notes)
	prompt := i18n.T("Delete the %d notes on %s?", n, d.path)
	if n == 1 {
		prompt = i18n.T("Delete the note on %s?", d.path)
	}
	m.modal = &decisionState{
		req: engine.DecisionRequest{ID: "session-notes-delete", Prompt: prompt, Options: []string{"Delete", "Cancel"}},
		sel: 1, // default highlight = Cancel
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt != "Delete" {
				return m, nil
			}
			k := d.clearNotes()
			m.statusMsg = i18n.T("deleted %d notes", k)
			if k == 1 {
				m.statusMsg = i18n.T("deleted the note")
			}
			return m, nil
		},
	}
	return m, nil
}

// sessionsDeleteRow is D on an open-files row: an overview document asks to
// be deleted, a file with notes asks to lose them, anything else only says.
func (m Model) sessionsDeleteRow(d *openFile) (Model, tea.Cmd) {
	switch {
	case m.heldOverview(d):
		tm, cmd := m.confirmSessionOverviewDelete(d)
		return tm.(Model), cmd
	case d.ov == nil && d.hasNotes():
		tm, cmd := m.confirmFileNotesDelete(d)
		return tm.(Model), cmd
	}
	m.statusMsg = i18n.T("nothing to delete on this row")
	return m, nil
}
