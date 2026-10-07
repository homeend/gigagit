package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
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
	hash, svc := m.filesHash, m.svc
	switch {
	case l.noteID != "":
		id, quote := l.noteID, reviewQuote("", "")
		for _, r := range m.noteCounts.Reviews {
			if r.ID == id {
				quote = reviewQuote(r.Agent, r.Summary)
			}
		}
		// A preview's Reviews block: its heads are the preview's, never in
		// noteCounts.Reviews (R5).
		for _, r := range m.filesPreviewReviews {
			if r.ID == id {
				quote = reviewQuote(r.Agent, r.Summary)
			}
		}
		return []actionRow{
			{id: "open-review", label: i18n.T("Open review"), run: open},
			m.asyncCopyLinkRow("copy-gg-link", i18n.T("Copy gg link"), func(ctx context.Context) (string, error) {
				return svc.ReviewLink(ctx, id)
			}),
			{id: "delete-review", label: i18n.T("Delete review"), run: func(m Model) (tea.Model, tea.Cmd) {
				return m.confirmStoredDelete(id, i18n.T("Delete this review?")+"\n"+quote, true)
			}},
		}, true
	case l.noteScope != "":
		scope := l.noteScope
		n := scopeNoteCount(m.noteCounts, hash, scope)
		return []actionRow{
			{id: "open-range-review", label: i18n.T("Open range review"), run: open},
			m.asyncCopyLinkRow("copy-gg-link", i18n.T("Copy gg link"), func(ctx context.Context) (string, error) {
				return svc.ScopeLinkText(ctx, scope, hash)
			}),
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
			m.asyncCopyLinkRow("copy-gg-link", i18n.T("Copy gg link"), func(ctx context.Context) (string, error) {
				return svc.CommitFileLinkText(ctx, hash, path)
			}),
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
// path set = a Notes row, else scope = a Range review row; n counts threads,
// as the confirm did.
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

// dropFilesRows removes the files-view rows gone matches, and a note group's
// heading left with no rows under it; a list left empty reads "(no files)"
// again. A group heading is told by the rows under it, never by what follows
// the group: root files carry no heading of their own, so a stale "Notes"
// would read as theirs.
func (m Model) dropFilesRows(gone func(contentLine) bool) Model {
	p := m.filesView
	if p == nil {
		return m
	}
	isNote := func(l contentLine) bool { return l.noteID != "" || l.noteScope != "" || l.notedPath != "" }
	out := make([]contentLine, 0, len(p.lines))
	for i, l := range p.lines {
		if gone(l) {
			continue
		}
		if l.heading && i+1 < len(p.lines) && isNote(p.lines[i+1]) {
			kept := false
			for j := i + 1; j < len(p.lines) && isNote(p.lines[j]); j++ {
				kept = kept || !gone(p.lines[j])
			}
			if !kept {
				continue // an emptied group: its heading goes with its last row
			}
		}
		out = append(out, l)
	}
	if len(out) == len(p.lines) {
		return m
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

// asyncCopyLinkRow is a copy row whose link needs git (a review's revs, a
// pair's full shas): the link is built when the row RUNS, off the Update
// thread, then copied — and recorded in gg links — like every copy.
func (m Model) asyncCopyLinkRow(id, label string, build func(context.Context) (string, error)) actionRow {
	return actionRow{id: id, label: label, run: func(m Model) (tea.Model, tea.Cmd) {
		cp := m.copyToClipboardCmd
		return m, func() tea.Msg {
			text, err := build(context.Background())
			if err != nil {
				return clipboardCopiedMsg{err: err}
			}
			return cp(i18n.T("Copied link: %s", text), text)()
		}
	}}
}

// reviewViewCopyLinkRow is the open review view's "Copy review link".
func (m Model) reviewViewCopyLinkRow() (actionRow, bool) {
	st, svc := m.filesReview, m.svc
	if st == nil || svc == nil || !m.inContentWindow() || m.diffLayer() != nil {
		return actionRow{}, false
	}
	id := st.id
	return m.asyncCopyLinkRow("copy-review-link", i18n.T("Copy review link"), func(ctx context.Context) (string, error) {
		return svc.ReviewLink(ctx, id)
	}), true
}

// reviewFileCopyLinkRow is a reviewed file row's "Copy review link to this
// file": the review link with the file's path, which opens the review on it.
func (m Model) reviewFileCopyLinkRow() (actionRow, bool) {
	st, svc := m.filesReview, m.svc
	if st == nil || svc == nil || !m.inContentWindow() || m.diffLayer() != nil {
		return actionRow{}, false
	}
	path, ok := m.fileListRowPath()
	if !ok || path == "" {
		return actionRow{}, false
	}
	id := st.id
	return m.asyncCopyLinkRow("copy-review-file-link", i18n.T("Copy review link to this file"), func(ctx context.Context) (string, error) {
		return svc.ReviewFileLink(ctx, id, path)
	}), true
}

// reviewRemarkRows are a review remark's copy rows in a review diff: its
// review link (Copy remark link) and its id review:<id>:<n> (Copy remark id,
// what gg note reply / resolve take). They act on the thread ROOT, so a reply
// under the cursor copies its remark. nil when no remark is in reach.
func (m Model) reviewRemarkRows() []actionRow {
	v, ok := m.topLayer().(*diffView)
	if !ok || v.reviewID == "" || m.svc == nil {
		return nil
	}
	rootID := ""
	for _, t := range replyableNoteTargets(m.notesAtCursor()) {
		if model.IsReviewNoteID(t.rootID) {
			rootID = t.rootID
			break
		}
	}
	if rootID == "" {
		return nil
	}
	svc := m.svc
	return []actionRow{
		m.asyncCopyLinkRow("copy-remark-link", i18n.T("Copy remark link"), func(ctx context.Context) (string, error) {
			return svc.ReviewRemarkLink(ctx, rootID)
		}),
		m.copyRow("copy-remark-id", i18n.T("Copy remark id"), i18n.T("Copied remark id: %s", rootID), rootID),
	}
}

// reviewRemarkLinkRow is the remark's "Copy remark link" — what L copies on a
// review remark.
func (m Model) reviewRemarkLinkRow() (actionRow, bool) {
	if rows := m.reviewRemarkRows(); len(rows) > 0 {
		return rows[0], true
	}
	return actionRow{}, false
}

// branchReviewCopyLinkRow is a Branches review sub-row's "Copy gg link".
func (m Model) branchReviewCopyLinkRow() (actionRow, bool) {
	if m.focus != panelBranches || m.inContentWindow() || m.svc == nil {
		return actionRow{}, false
	}
	h, ok := m.selectedBranchReview()
	if !ok {
		return actionRow{}, false
	}
	svc, id := m.svc, h.ID
	return m.asyncCopyLinkRow("copy-gg-link", i18n.T("Copy gg link"), func(ctx context.Context) (string, error) {
		return svc.ReviewLink(ctx, id)
	}), true
}
