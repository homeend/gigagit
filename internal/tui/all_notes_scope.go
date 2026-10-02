package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// View all notes → a range review's note.
//
// Such a note is stored on the range's newest commit but belongs to the
// review: the commit's own diff does not draw it, and the commit may not even
// change the file. So enter opens the file's diff over the RANGE the note was
// written in, frozen at that commit (domain.ScopeAtCommit) — the review's
// notes drawn, the cursor on this one — as a diff layer over the popup, like
// every other note the popup opens; esc returns to it.

// allNotesScopeMsg is that range, resolved off the UI thread.
type allNotesScopeMsg struct {
	tag   string // the loading layer's diffTag: a mismatch drops the result
	t     anTarget
	id    string
	scope string
	a, b  string
	set   domain.PreviewNoteSet
	// line is the file's row in the range: its status decides which sides the
	// diff reads (an added file has no old side), its old path follows a rename.
	line contentLine
	err  error
}

// openAllNotesReviewNote pushes the loading diff and resolves the range.
func (m Model) openAllNotesReviewNote(p *allNotesPopup, t anTarget, n model.Note) (Model, tea.Cmd) {
	p.notice = ""
	if m.width > 0 && m.width < 60 {
		p.notice = i18n.T("terminal too narrow for the diff view")
		return m, nil
	}
	svc := m.svc
	if svc == nil {
		return m, nil
	}
	tag := "scope:" + n.ID
	m = m.pushLayer(&diffView{title: t.path, context: "@ " + scopeTitle(n.Preview), loading: true,
		partial: m.diffPartial, long: m.diffLong, compare: true})
	m.diffTag = tag
	m.diffNotice = ""
	m.diffNav = diffNavNone // no source list to step through
	m.noteLand = &noteLanding{id: n.ID, tag: tag}
	msg := allNotesScopeMsg{tag: tag, t: t, id: n.ID, scope: n.Preview}
	return m, func() tea.Msg {
		ctx := context.Background()
		if msg.a, msg.b, msg.err = svc.ScopeAtCommit(ctx, msg.scope, t.commit); msg.err != nil {
			return msg
		}
		if msg.set, msg.err = svc.PairNotes(ctx, msg.a, msg.b); msg.err == nil && !msg.set.OK() {
			msg.err = errors.New("the range is not in this repository")
		}
		msg.set.Only = msg.scope // the review's own notes, under its own name
		msg.line = contentLine{path: t.path}
		left, lerr := model.CommitEndpoint(msg.a)
		right, rerr := model.CommitEndpoint(msg.b)
		if lerr == nil && rerr == nil {
			if files, ferr := svc.CompareFiles(ctx, left, right); ferr == nil {
				for _, f := range files {
					if f.Path == t.path {
						msg.line = contentLine{path: f.Path, oldPath: f.OldPath, status: f.Status}
					}
				}
			}
		}
		return msg
	}
}

// onAllNotesScope turns the loading layer into the range's diff of the file.
// A range that can no longer be worked out (its branch was merged, its target
// is gone) falls back to the commit the note is stored on, where the popup's
// diff draws range notes too — the note always opens somewhere.
func (m Model) onAllNotesScope(msg allNotesScopeMsg) (Model, tea.Cmd) {
	dv := m.diffLayer()
	if dv == nil || m.diffTag != msg.tag {
		return m, nil // closed, or another note was opened meanwhile
	}
	left, lerr := model.CommitEndpoint(msg.a)
	right, rerr := model.CommitEndpoint(msg.b)
	if err := errors.Join(msg.err, lerr, rerr); err != nil {
		m = m.popLayer()
		m.noteLand = nil
		p := layerOf[*allNotesPopup](m)
		if p == nil {
			return m, nil
		}
		m, cmd := m.openAllNotesTarget(p, msg.t, msg.id)
		m.statusMsg = i18n.T("range review: %s", err.Error())
		return m, cmd
	}
	set := msg.set
	dv.previewSet = &set
	dv.noteAddr = model.FileAddress{State: model.StateCommitted, Commit: set.Tip, Path: msg.t.path}
	m.diffTag = "cmp:" + left.CacheTag() + ":" + right.CacheTag() + ":" + msg.t.path
	if m.noteLand != nil && m.noteLand.id == msg.id {
		m.noteLand.tag = m.diffTag
	}
	return m, m.loadCompareDiffCmd(left, right, msg.line)
}
