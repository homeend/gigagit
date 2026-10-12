package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notemeta"
)

// Mark (spec 2026-10-12-note-meta §5.4, R6, R12): the reader tags a note
// agreed / disagreed / deferred — the same `status` key a double-checking
// agent writes, shown through the built-in status declaration. It is a tag
// and nothing else: it resolves, sends, ticks, folds and filters nothing.

// noteMarkedMsg is a mark's write result.
type noteMarkedMsg struct{ err error }

// markableNoteTargets are the threads in reach a mark can be written on:
// stored notes and review remarks — never a GitHub thread (read-only).
func markableNoteTargets(ts []noteTarget) []noteTarget {
	var out []noteTarget
	for _, t := range ts {
		if !t.forge && !model.IsForgeNoteID(t.note.ID) {
			out = append(out, t)
		}
	}
	return out
}

// noteMarkRow is the . menu's "Mark…": a choice of the three marks or clear.
func (m Model) noteMarkRow() (actionRow, bool) {
	if _, ok := m.topLayer().(*diffView); !ok || m.svc == nil {
		return actionRow{}, false
	}
	ts := markableNoteTargets(replyableNoteTargets(m.notesAtCursor(), m.prOfDiff() > 0))
	if len(ts) == 0 {
		return actionRow{}, false
	}
	return actionRow{id: "note-mark", label: i18n.T("Mark…"), run: func(m Model) (tea.Model, tea.Cmd) {
		return m.withNoteTargetIn(ts, func(m Model, t noteTarget) (tea.Model, tea.Cmd) {
			return m.askNoteMark(t)
		})
	}}, true
}

// askNoteMark raises the mark choice for one note (esc = Cancel).
func (m Model) askNoteMark(t noteTarget) (tea.Model, tea.Cmd) {
	id := t.note.ID
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "note-mark",
			Prompt:  i18n.T("Mark this note:") + "\n" + noteQuote(t.note),
			Options: []string{"agreed", "disagreed", "deferred", "clear", "Cancel"},
		},
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt == "Cancel" || m.svc == nil {
				return m, nil
			}
			svc, by := m.svc, m.identity.EffectiveName
			return m, func() tea.Msg {
				ctx := context.Background()
				if opt == "clear" {
					return noteMarkedMsg{err: svc.UnsetNoteMeta(ctx, id, by, []string{notemeta.StatusKey})}
				}
				_, err := svc.SetNoteMeta(ctx, id, by, []notemeta.KV{{Key: notemeta.StatusKey, Value: opt}})
				return noteMarkedMsg{err: err}
			}
		},
	}
	return m, nil
}

// onNoteMarked reloads the notes (the one reload path) or says why not.
func (m Model) onNoteMarked(msg noteMarkedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = i18n.T("note: %s", msg.err.Error())
		m.diffNotice = "▸ " + m.statusMsg
		return m, nil
	}
	nm, cmd := m.Update(noteMutatedMsg{})
	return nm, tea.Batch(cmd, nm.(Model).reviewMetaCountsCmd())
}

// reviewMetaCountsMsg is the open review view's counts read again after a
// mark (a declaration may count the status key).
type reviewMetaCountsMsg struct {
	id     string
	meta   notemeta.Counts
	byFile map[string]notemeta.Counts
}

// reviewMetaCountsCmd re-reads the open review view's counts; nil outside
// a review view.
func (m Model) reviewMetaCountsCmd() tea.Cmd {
	st, svc := m.filesReview, m.svc
	if st == nil || svc == nil {
		return nil
	}
	id := st.id
	return func() tea.Msg {
		ctx := context.Background()
		r, err := svc.Review(ctx, id)
		if err != nil {
			return nil
		}
		_, byFile, err := svc.ReviewFileMetaCounts(ctx, id)
		if err != nil {
			return nil
		}
		return reviewMetaCountsMsg{id: id, meta: domain.ReviewMetaCounts(r), byFile: byFile}
	}
}

// onReviewMetaCounts installs counts read for the review view still open.
func (m Model) onReviewMetaCounts(msg reviewMetaCountsMsg) (tea.Model, tea.Cmd) {
	if st := m.filesReview; st != nil && st.id == msg.id {
		st.meta, st.metaByFile = msg.meta, msg.byFile
	}
	return m, nil
}
