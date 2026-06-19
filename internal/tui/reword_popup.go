package tui

import (
	"context"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// rewordPopup wraps the shared commit-message editor (commitPopup) with the
// target commit. Pre-filled from the row Subject immediately; the full message
// arrives async via rewordPrefillMsg and replaces it if the user hasn't typed.
// Submit is gated on loaded so a fast ctrl+s can't commit the subject-only
// prefill and silently drop the commit body.
type rewordPopup struct {
	commit  string
	ggBin   string
	popup   commitPopup
	touched bool // user edited before the async prefill landed
	loaded  bool // the full message arrived; submit is refused until then
}

// rewordPrefillMsg carries the fetched full message for commit.
type rewordPrefillMsg struct {
	commit string
	msg    string
	err    error
}

// openRewordPopup opens the dialog for the selected Commits-panel row, prefilled
// from its subject (the full body arrives async). Returns (m, false) when no row.
func (m Model) openRewordPopup() (Model, bool) {
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return m, false
	}
	c := m.commits[bi]
	ggBin, _ := os.Executable()
	t, d := splitMessage(c.Subject)
	m.rewordPopup = &rewordPopup{commit: c.Hash, ggBin: ggBin, popup: commitPopup{title: t, desc: d}}
	return m, true
}

// fetchRewordPrefill loads the commit's full message off the UI thread.
func (m Model) fetchRewordPrefill(commit string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		msg, err := svc.CommitMessage(context.Background(), commit)
		return rewordPrefillMsg{commit: commit, msg: msg, err: err}
	}
}

// rewordRow offers Rename commit on the Commits panel (no dedicated key; opens
// the reword popup). Available only when a commit is selected and no op runs.
func (m Model) rewordRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	hash := m.commits[bi].Hash
	return actionRow{
		id:    "reword-commit",
		label: "Rename commit",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m, ok := m.openRewordPopup()
			if !ok {
				return m, nil
			}
			return m, m.fetchRewordPrefill(hash)
		},
	}, true
}

// updateRewordPopupKey handles one key while the reword popup is open.
func (m Model) updateRewordPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	rp := m.rewordPopup
	submit, cancel := rp.popup.applyEditKey(msg)
	rp.touched = true
	switch {
	case cancel:
		m.rewordPopup = nil
	case submit:
		if !rp.loaded {
			// The full message hasn't arrived yet — submitting now would commit
			// the subject-only prefill and silently drop the commit body.
			m.statusMsg = "loading message…"
			return m, nil
		}
		if strings.TrimSpace(rp.popup.title) == "" {
			m.statusMsg = "title required"
			return m, nil
		}
		op := engine.Reword{Commit: rp.commit, NewMsg: rp.popup.message(), GGBin: rp.ggBin}
		m.rewordPopup = nil
		return m.startOp(op)
	}
	return m, nil
}

// renderRewordPopup draws the reword dialog (reuses the commit field renderer).
func (m Model) renderRewordPopup() string {
	rp := m.rewordPopup
	var b strings.Builder
	b.WriteString("Reword commit " + shortHash(rp.commit) + "\n\n")
	b.WriteString(renderCommitFields(&rp.popup))
	b.WriteString("\n[tab] switch field  [enter] newline/next  [ctrl+s] reword  [esc] cancel")
	w, _ := m.overlayDims()
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}
