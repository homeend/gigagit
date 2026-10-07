package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// prOfDiff is the pull request whose diff is on top (0 = none): the view's
// own stamp, and only while that PR is still the open one.
func (m Model) prOfDiff() int {
	v := m.diffLayer()
	if v == nil || v.forgePR == 0 || v.forgePR != m.openPRNumber() {
		return 0
	}
	return v.forgePR
}

// noteSendRequest sends one local root (a note or an AI review's remark).
func noteSendRequest(pr int, t noteTarget) domain.PRSendRequest {
	return domain.PRSendRequest{PR: pr, Notes: []string{t.rootID}}
}

// threadActionRequest resolves a GitHub thread, or reopens a resolved one.
func threadActionRequest(pr int, t noteTarget) domain.PRSendRequest {
	if t.resolved {
		return domain.PRSendRequest{PR: pr, Unresolve: []string{t.rootID}}
	}
	return domain.PRSendRequest{PR: pr, Resolve: []string{t.rootID}}
}

// sendable is a local root that can go now (not on GitHub, not in flight).
func sendable(t noteTarget) bool {
	return !t.forge && t.sync != model.SyncSending && t.sync != model.SyncForge
}

// forgeNoteRows are the . menu's GitHub rows for the threads in reach,
// inside a PR's diff only (spec §4.1).
func (m Model) forgeNoteRows() []actionRow {
	pr := m.prOfDiff()
	if pr == 0 {
		return nil
	}
	all := m.notesAtCursor()
	var local, threads, drafted []noteTarget
	for _, t := range all {
		switch {
		case t.forge:
			threads = append(threads, t)
			if len(t.drafts) > 0 {
				drafted = append(drafted, t)
			}
		case sendable(t):
			local = append(local, t)
		}
	}
	var rows []actionRow
	if len(local) > 0 {
		label := i18n.T("Send to GitHub")
		if local[0].sync == model.SyncFailed {
			label = i18n.T("Retry sending to GitHub")
		}
		rows = append(rows, actionRow{id: "note-send", label: label, run: func(m Model) (tea.Model, tea.Cmd) {
			return m.withNoteTargetIn(local, func(m Model, t noteTarget) (tea.Model, tea.Cmd) {
				return m.forgeSendCmd(noteSendRequest(pr, t), "")
			})
		}})
		label = i18n.T("Send my draft review…")
		if local[0].group != domain.GroupMine {
			label = i18n.T("Send this AI review…")
		}
		rows = append(rows, actionRow{id: "note-send-review", label: label, run: func(m Model) (tea.Model, tea.Cmd) {
			return m.withNoteTargetIn(local, func(m Model, t noteTarget) (tea.Model, tea.Cmd) {
				return m.openSendReviewBody(pr, t.group)
			})
		}})
	}
	if len(threads) > 0 {
		rows = append(rows, actionRow{id: "note-reply-send", label: i18n.T("Reply & send…"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.withNoteTargetIn(threads, func(m Model, t noteTarget) (tea.Model, tea.Cmd) {
				nm, cmd := m.openNotePopupFor(noteReply, t)
				if p := layerOf[*notePopup](nm.(Model)); p != nil {
					p.sendPR = pr
				}
				return nm, cmd
			})
		}})
	}
	if len(drafted) > 0 {
		label := i18n.T("Send draft reply")
		if n := len(drafted[0].drafts); n > 1 {
			label = i18n.T("Send %d draft replies", n)
		}
		rows = append(rows, actionRow{id: "note-send-drafts", label: label, run: func(m Model) (tea.Model, tea.Cmd) {
			return m.withNoteTargetIn(drafted, func(m Model, t noteTarget) (tea.Model, tea.Cmd) {
				return m.forgeSendCmd(domain.PRSendRequest{PR: pr, Notes: t.drafts}, "")
			})
		}})
	}
	return rows
}
