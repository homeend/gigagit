package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// commitNotesMsg carries the notes on one path AT a commit — a Notes row of
// the commit's Files view: a path the commit counts notes on but does not
// change (a merge preview's note is stored on the source tip).
type commitNotesMsg struct {
	label string
	notes []domain.ResolvedNote
	err   error
}

// commitNotesCmd loads path's notes at hash off the UI thread.
func (m Model) commitNotesCmd(hash, path string) tea.Cmd {
	if m.svc == nil {
		return nil
	}
	svc, label := m.svc, path+" @ "+shortHash(hash)
	return func() tea.Msg {
		ns, err := svc.NotesAt(context.Background(), model.FileAddress{State: model.StateCommitted, Commit: hash, Path: path})
		return commitNotesMsg{label: label, notes: domain.PlainNotes(ns), err: err}
	}
}

// openCommitNotes reads them in the read-only notes viewer (the shelf
// notes' popup); esc returns to the Files view.
func (m Model) openCommitNotes(msg commitNotesMsg) Model {
	switch {
	case msg.err != nil:
		m.statusMsg = i18n.T("note: %s", msg.err.Error())
		return m
	case len(msg.notes) == 0: // every one orphaned: the file is gone at the commit
		m.statusMsg = i18n.T("No readable notes on %s", msg.label)
		return m
	}
	cp := newContentPopup(i18n.T("Notes on %s", msg.label), shelfNoteLines(msg.notes))
	cp.fitContent = true
	cp.prose = true
	return m.pushLayer(cp)
}
