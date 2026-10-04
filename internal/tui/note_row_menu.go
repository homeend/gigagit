package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// A commit's Files view lists three kinds of note row above its files: a
// Reviews row (an AI review), a Range review row (the notes written in a
// commit pair or merge preview) and a Notes row (plain notes on a file the
// commit does not change). None is a file, so the "." menu on one is that
// row's own pair — Open (what enter does) and Delete — and nothing else: no
// file copies, no shelf/bookmark/compare, no commit id (user ruling
// 2026-10-05: a review is opened or removed).

// filesNoteRow is the note row under the files-view cursor, while the tree
// has the keys and no single-file surface sits over it.
func (m Model) filesNoteRow() (contentLine, bool) {
	if m.filesView == nil || !m.filesTreeFocused || m.diffLayer() != nil {
		return contentLine{}, false
	}
	switch m.topLayer().(type) {
	case *historyView, *blameView, *fileViewer:
		return contentLine{}, false
	}
	vis := m.filesView.visible()
	if m.filesView.sel < 0 || m.filesView.sel >= len(vis) {
		return contentLine{}, false
	}
	l := vis[m.filesView.sel]
	if l.noteID == "" && l.noteScope == "" && l.notedPath == "" {
		return contentLine{}, false
	}
	return l, true
}

// noteRowMenu is the whole "." menu on a note row (filesNoteRow).
func (m Model) noteRowMenu() ([]actionRow, bool) {
	l, ok := m.filesNoteRow()
	if !ok {
		return nil, false
	}
	open := func(m Model) (tea.Model, tea.Cmd) { return m.openDiffForFileLine(l) }
	hash := m.filesHash
	switch {
	case l.noteID != "":
		id, quote := l.noteID, reviewQuote("", "")
		for _, r := range m.noteCounts.Reviews {
			if r.ID == id {
				quote = reviewQuote(r.Agent, r.Summary)
			}
		}
		return []actionRow{
			{id: "open-review", label: i18n.T("Open review"), run: open},
			{id: "delete-review", label: i18n.T("Delete review"), run: func(m Model) (tea.Model, tea.Cmd) {
				return m.confirmStoredDelete(id, i18n.T("Delete this review?")+"\n"+quote, true)
			}},
		}, true
	case l.noteScope != "":
		scope := l.noteScope
		n := scopeNoteCount(m.noteCounts, hash, scope)
		return []actionRow{
			{id: "open-range-review", label: i18n.T("Open range review"), run: open},
			{id: "delete-range-review", label: i18n.T("Delete range review"), run: func(m Model) (tea.Model, tea.Cmd) {
				prompt := i18n.T("Delete this range review's %d notes?", n)
				if n == 1 {
					prompt = i18n.T("Delete this range review's note?")
				}
				return m.confirmRowNotesClear("range-review-remove", prompt+"\n"+scopeLabel(scope), hash, "", scope)
			}},
		}, true
	default:
		path := l.notedPath
		n := m.noteCounts.PlainByCommitPath[hash+":"+path]
		return []actionRow{
			{id: "open-notes", label: i18n.T("Open notes"), run: open},
			{id: "delete-notes", label: i18n.T("Delete notes"), run: func(m Model) (tea.Model, tea.Cmd) {
				prompt := i18n.T("Delete the %d notes on this file?", n)
				if n == 1 {
					prompt = i18n.T("Delete the note on this file?")
				}
				return m.confirmRowNotesClear("notes-remove", prompt+"\n"+path, hash, path, "")
			}},
		}, true
	}
}

// rowNotesClearedMsg reports a Range review or Notes row's notes removed:
// path set = a Notes row, else scope = a Range review row.
type rowNotesClearedMsg struct {
	hash, path, scope string
	n                 int
	err               error
}

// confirmRowNotesClear asks (Cancel is the default), then removes the row's
// notes at the commit off the UI thread.
func (m Model) confirmRowNotesClear(decisionID, prompt, hash, path, scope string) (tea.Model, tea.Cmd) {
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
				n, err := svc.NotesClearAtCommit(context.Background(), hash, path, scope)
				return rowNotesClearedMsg{hash: hash, path: path, scope: scope, n: n, err: err}
			}
		},
	}
	return m, nil
}

// onRowNotesCleared drops the row from the commit's list (when it is still
// the one on screen) and reloads the note counts.
func (m Model) onRowNotesCleared(msg rowNotesClearedMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = i18n.T("note: %s", msg.err.Error())
		return m, nil
	}
	m.statusMsg = i18n.T("deleted %d notes", msg.n)
	if msg.n == 1 {
		m.statusMsg = i18n.T("deleted the note")
	}
	if m.filesHash == msg.hash {
		m = m.dropFilesRows(func(l contentLine) bool {
			if msg.path != "" {
				return l.notedPath == msg.path
			}
			return l.noteScope == msg.scope
		})
	}
	return m.reloadSourcesCmd([]sourceKey{srcNotes}, reloadOpts{})
}

// dropFilesRows removes the files-view rows gone matches, and a group heading
// left with no rows under it; a list left empty reads "(no files)" again. The
// cursor stays on the row that took the removed one's place.
func (m Model) dropFilesRows(gone func(contentLine) bool) Model {
	p := m.filesView
	if p == nil {
		return m
	}
	kept := make([]contentLine, 0, len(p.lines))
	for _, l := range p.lines {
		if !gone(l) {
			kept = append(kept, l)
		}
	}
	if len(kept) == len(p.lines) {
		return m
	}
	out := make([]contentLine, 0, len(kept))
	for i, l := range kept {
		if l.heading && (i+1 == len(kept) || kept[i+1].heading) {
			continue // an emptied group: its heading goes with its last row
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		out = commitFileLines(nil)
	}
	p.lines = out
	if n := len(p.visible()); p.sel >= n {
		p.sel = max(n-1, 0)
	}
	return m
}
