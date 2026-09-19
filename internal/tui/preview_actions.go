package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// previewMutatedMsg is the result of any store mutation from the TUI: the tab
// reloads; focusID selects a row once the fresh rows land; open then opens it.
// fromTab is true for the tab's own keys (a/e/d/s) and false for the pair
// dialog: only the former moves focus onto the Previews tab afterwards.
type previewMutatedMsg struct {
	err            error
	focusID        string
	open           bool
	fromTab        bool
	source, target string
	// pairSaved is the label of the commit pair a save just stored ("" for
	// every other mutation); pairExisted says the pair was already there.
	pairSaved   string
	pairExisted bool
}

// previewAddCmd saves a pair off the UI thread. A duplicate is NOT an error:
// the store hands back the existing record, which the tab then focuses (and
// opens, when asked) exactly as if it had just been created.
func (m Model) previewAddCmd(source, target, label string, open, fromTab bool) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		p, err := svc.PreviewAdd(context.Background(), source, target, label)
		if errors.Is(err, domain.ErrPreviewExists) {
			err = nil // focus the existing row instead
		}
		return previewMutatedMsg{err: err, focusID: p.ID, open: open, fromTab: fromTab, source: source, target: target}
	}
}

// previewRenameCmd and previewRemoveCmd take the row KIND because each domain
// surface refuses the other's rows: PreviewRename on a pair id is "not found",
// by design (a preview surface never relabels what it cannot show).
func (m Model) previewRenameCmd(kind previewRowKind, id, label string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		var err error
		if kind == rowPair {
			err = svc.PairRename(context.Background(), id, label)
		} else {
			err = svc.PreviewRename(context.Background(), id, label)
		}
		return previewMutatedMsg{err: err, focusID: id, fromTab: true}
	}
}

func (m Model) previewRemoveCmd(kind previewRowKind, id string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		var err error
		if kind == rowPair {
			err = svc.PairRemove(context.Background(), id)
		} else {
			err = svc.PreviewRemove(context.Background(), id)
		}
		return previewMutatedMsg{err: err, fromTab: true}
	}
}

// pairAddCmd saves the commit pair a..b (both freeze to full shas in domain)
// and focuses its Previews row; an already-saved pair focuses the existing
// row instead of failing, like previewAddCmd.
func (m Model) pairAddCmd(a, b string, fromTab bool) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		p, err := svc.PairAdd(context.Background(), a, b, "")
		existed := errors.Is(err, domain.ErrPairExists)
		if existed {
			err = nil
		}
		return previewMutatedMsg{err: err, focusID: p.ID, fromTab: fromTab, pairSaved: p.Label, pairExisted: existed}
	}
}

// handlePreviewMutatedMsg reports errors, else reloads the tab and remembers
// what to focus (and open) once the fresh rows land.
func (m Model) handlePreviewMutatedMsg(msg previewMutatedMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = i18n.T("error: %s", msg.err.Error())
		return m, nil
	}
	m.previewFocusID = msg.focusID
	m.previewFocusTab = msg.fromTab
	if msg.pairSaved != "" {
		m.statusMsg = i18n.T("saved to previews: %s", msg.pairSaved)
		if msg.pairExisted {
			m.statusMsg = i18n.T("already saved as %s", msg.pairSaved)
		}
	}
	var open tea.Cmd
	if msg.open {
		open = m.openPreviewCmd(msg.focusID, msg.source, msg.target, "")
	}
	// chainPreviewsRead: a mutation reload can supersede a manual previews read
	// still in flight (add, save, then r), and a silent supersession strands
	// srcLoading[previews] — with r itself the key that could no longer clear it.
	var reload tea.Cmd
	m, reload = m.chainPreviewsRead()
	return m, tea.Batch(reload, open)
}

// canAddPreview gates a: the Previews tab is focused and nothing is running.
// Adding needs no row — an empty tab is exactly where a is most useful.
func (m Model) canAddPreview() bool { return m.focus == panelPreviews && m.opsIdle() }

// canEditPreview gates enter/e/d/s: the same, plus a selected row.
func (m Model) canEditPreview() bool {
	_, ok := m.selectedPreview()
	return m.focus == panelPreviews && ok && m.opsIdle()
}

// openPreviewAddPopup pushes the empty add form.
func (m Model) openPreviewAddPopup() Model {
	return m.pushLayer(&previewAddPopup{source: newTextField(""), target: newTextField("")})
}

// openPreviewRenamePopup pushes the rename form for the selected row.
func (m Model) openPreviewRenamePopup() (Model, bool) {
	r, ok := m.selectedPreview()
	if !ok {
		return m, false
	}
	return m.pushLayer(&previewRenamePopup{id: r.id(), kind: r.kind, label: newTextField(r.label())}), true
}

// confirmPreviewRemove raises the remove confirm for the selected row. The
// option VALUES stay English (agent-facing protocol); optionDisplayName
// renders them. "Cancel" is last so esc resolves to it.
func (m Model) confirmPreviewRemove() Model {
	r, ok := m.selectedPreview()
	if !ok {
		return m
	}
	id, kind := r.id(), r.kind
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "preview-remove",
			Prompt:  i18n.T("Remove preview %s?", r.label()),
			Options: []string{"Remove", "Cancel"},
		},
		sel: 1,
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt == "Remove" {
				return m, m.previewRemoveCmd(kind, id)
			}
			return m, nil
		},
	}
	return m
}

// openPreviewPairDialog is the once/save/swap dialog behind the Branches
// pair-picker row. A preview is worth saving when you will come back to it and
// worth showing once when you will not, and which of the two a pair is only
// becomes clear with the pair in front of you — so the dialog asks instead of
// the row guessing. Option VALUES are protocol (English); optionDisplayName
// renders them. "abort" is offered last so esc (abortOption) cancels.
func (m Model) openPreviewPairDialog(source, target string) (Model, tea.Cmd) {
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "preview-pair",
			Prompt:  i18n.T("Preview merging %s into %s", source, target),
			Options: []string{"show once", "show and save", "swap direction", "abort"},
		},
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			switch opt {
			case "show once":
				// No id: the compare view opens transiently and the store is
				// untouched (a re-arm still follows the tips while it is open).
				return m, m.openPreviewCmd("", source, target, "")
			case "show and save":
				// fromTab=false: the dialog can fire from any tab, so saving
				// must not yank the user onto Previews behind the opened view.
				return m, m.previewAddCmd(source, target, "", true, false)
			case "swap direction":
				// Marked-vs-selected is easy to get backwards; re-ask reversed
				// rather than making the user re-pair in the other order.
				return m.openPreviewPairDialog(target, source)
			}
			return m, nil
		},
	}
	return m, nil
}

// previewSwapCmd saves the reversed pair as a second preview (the record's id
// is direction-sensitive, so this never collides with the row it came from).
// It is saved, not opened: s is a bookkeeping key, not a viewing one.
func (m Model) previewSwapCmd() tea.Cmd {
	r, ok := m.selectedPreview()
	if !ok {
		return nil
	}
	rec, isMerge := r.merge()
	if !isMerge {
		return m.pairAddCmd(r.pair.B, r.pair.A, true)
	}
	return m.previewAddCmd(rec.Target, rec.Source, "", false, true)
}
