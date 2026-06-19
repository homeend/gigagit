package tui

import (
	"context"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// rewordPopup wraps the shared commit-message editor (commitPopup) with the
// target commit. The full message is fetched synchronously at open time (safe:
// the menu row is opsIdle-gated, so the Read reservation acquires immediately
// and a single `git log -1` for an explicit popup-open is imperceptible), so
// the body can never be dropped by a fast keystroke.
type rewordPopup struct {
	commit string
	ggBin  string
	popup  commitPopup
}

// openRewordPopup opens the dialog for the selected Commits-panel row, prefilled
// with the commit's full message (falling back to its subject if the read
// fails). Returns (m, false) when no row is selected.
func (m Model) openRewordPopup() (Model, bool) {
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return m, false
	}
	c := m.commits[bi]
	ggBin, _ := os.Executable()
	msg := c.Subject
	if full, err := m.svc.CommitMessage(context.Background(), c.Hash); err == nil && strings.TrimSpace(full) != "" {
		msg = full
	}
	t, d := splitMessage(msg)
	m.rewordPopup = &rewordPopup{commit: c.Hash, ggBin: ggBin, popup: commitPopup{title: t, desc: d}}
	return m, true
}

// rewordRow offers Rename commit on the Commits panel (no dedicated key; opens
// the reword popup). Available only when a commit is selected and no op runs.
func (m Model) rewordRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	if _, ok := m.backingIndex(panelCommits); !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "reword-commit",
		label: "Rename commit",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m, _ = m.openRewordPopup()
			return m, nil
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
	switch {
	case cancel:
		m.rewordPopup = nil
	case submit:
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
