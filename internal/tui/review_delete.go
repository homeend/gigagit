package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// Deleting a stored AI review (or, from View all notes, any stored note):
// the "." menu's "Delete review" on a Branches review sub-row and in the
// review view, and ctrl+d on a View all notes row. Each asks first (Cancel is
// the default), then removes the note off the UI thread; storedDeletedMsg
// closes a review view showing it and reloads an open All notes popup.

// storedDeletedMsg reports one stored note (a review is one) removed.
type storedDeletedMsg struct {
	id     string
	review bool // a review, not a note thread: the status line says which
	err    error
}

// reviewQuote is the one line a delete confirm quotes: who and which review.
func reviewQuote(agent, summary string) string {
	who := agent
	if who == "" {
		who = "agent"
	}
	return truncate(sanitizeLine("◆ "+who+" · "+summary), 72)
}

// deleteReviewRow is the "." menu's "Delete review": the review under the
// Branches cursor, or the one the review view shows. A commit's Reviews row
// has its own (noteRowMenu).
func (m Model) deleteReviewRow() (actionRow, bool) {
	var id, quote string
	switch {
	case m.filesReview != nil && m.inContentWindow() && m.diffLayer() == nil:
		switch m.topLayer().(type) {
		case *historyView, *blameView:
			return actionRow{}, false
		}
		st := m.filesReview
		id, quote = st.id, reviewQuote(st.review.Agent, st.review.Summary)
	case m.focus == panelBranches && !m.inContentWindow():
		h, ok := m.selectedBranchReview()
		if !ok {
			return actionRow{}, false
		}
		id, quote = h.ID, reviewQuote(h.Agent, h.Summary)
	case m.focus == panelPreviews && !m.inContentWindow():
		h, ok := m.selectedPreviewReview()
		if !ok {
			return actionRow{}, false
		}
		id, quote = h.ID, reviewQuote(h.Agent, h.Summary)
	default:
		return actionRow{}, false
	}
	return actionRow{
		id:    "delete-review",
		label: i18n.T("Delete review"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.confirmStoredDelete(id, i18n.T("Delete this review?")+"\n"+quote, true)
		},
	}, true
}

// confirmStoredDelete raises the yes/no modal; Delete removes id.
func (m Model) confirmStoredDelete(id, prompt string, review bool) (tea.Model, tea.Cmd) {
	decisionID := "note-remove" // confirmNoteDelete's id: the same question
	if review {
		decisionID = "review-remove"
	}
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      decisionID,
			Prompt:  prompt,
			Options: []string{"Delete", "Cancel"},
		},
		sel: 1, // default highlight = Cancel
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt != "Delete" || m.svc == nil {
				return m, nil
			}
			svc := m.svc
			return m, func() tea.Msg {
				return storedDeletedMsg{id: id, review: review, err: svc.NoteRemove(context.Background(), id)}
			}
		},
	}
	return m, nil
}

// onStoredDeleted lands a delete: a review view showing the note closes (as
// esc would), an open All notes popup re-reads, and the note counts reload.
func (m Model) onStoredDeleted(msg storedDeletedMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = i18n.T("note: %s", msg.err.Error())
		return m, nil
	}
	m.statusMsg = i18n.T("deleted the note")
	if msg.review {
		m.statusMsg = i18n.T("deleted the review")
	}
	var cmds []tea.Cmd
	if st := m.filesReview; st != nil && st.id == msg.id {
		var c tea.Cmd
		m, c = m.leaveReviewView()
		cmds = append(cmds, c)
	} else if msg.review {
		// Deleted off a commit's Reviews row (or elsewhere while that list is
		// open): the row goes with it.
		m = m.dropFilesRows(func(l contentLine) bool { return l.noteID == msg.id })
	}
	if p := layerOf[*allNotesPopup](m); p != nil {
		svc, gen := m.svc, m.loadGen
		cmds = append(cmds, func() tea.Msg {
			ov, err := svc.NotesOverview(context.Background())
			return allNotesMsg{ov: ov, err: err, gen: gen}
		})
	}
	var counts tea.Cmd
	m, counts = m.reloadSourcesCmd([]sourceKey{srcNotes}, reloadOpts{})
	cmds = append(cmds, counts)
	return m, tea.Batch(cmds...)
}

// leaveReviewView is the review view's esc: back to the commit's files it was
// opened from, else closed onto the panel (and popup) that opened it.
func (m Model) leaveReviewView() (Model, tea.Cmd) {
	if st := m.filesReview; st != nil && st.back.Hash != "" {
		// Opened from this commit's Reviews: back to its files, the keys on
		// the tree and the cursor on the review's row once the list lands.
		id := st.id
		m, cmd := m.openChangedFiles(st.back)
		m.focus = panelCommits
		m = m.focusTree()
		m.filesLandNote = id
		return m, cmd
	}
	if b := m.filesBack; b != nil {
		// A range opened from a commit's Range review row: back to that
		// commit's files, the cursor on the row once the list lands.
		scope := b.scope
		m, cmd := m.openChangedFiles(b.commit)
		m.focus = panelCommits
		m = m.focusTree()
		m.filesLandScope = scope
		return m, cmd
	}
	ret, parked := m.filesReturnFocus, m.filesReturnLayers
	m = m.closeFilesView()
	m.focus = ret                             // return to the panel that opened the view (Tags/Reflog/Commits/…)
	return m.restoreParkedLayers(parked), nil // …and to the popup that opened it, when one did
}

// allNotesDelete is View all notes' ctrl+d: the review or thread under the
// cursor, after a confirm.
func (m Model) allNotesDelete(p *allNotesPopup) (tea.Model, tea.Cmd) {
	vis := p.visible()
	if p.sel < 0 || p.sel >= len(vis) {
		return m, nil
	}
	switch r := vis[p.sel]; r.kind {
	case anReview:
		return m.confirmStoredDelete(r.review.ID, i18n.T("Delete this review?")+"\n"+reviewQuote(r.review.Agent, r.review.Summary), true)
	case anNote:
		prompt := i18n.T("Delete this note?")
		if n := len(r.note.Replies); n > 0 {
			prompt = i18n.T("Delete this note and its %d replies?", n)
		}
		return m.confirmStoredDelete(r.note.Note.ID, prompt+"\n"+noteQuote(r.note.Note), false)
	}
	return m, nil
}
