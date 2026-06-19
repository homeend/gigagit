package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// renameBranchPopup holds the in-flight rename-branch dialog. The text field is
// pre-filled with the branch's current name.
type renameBranchPopup struct {
	old  string // the branch being renamed
	name string // typed new name
}

// openRenameBranchPopup opens the dialog for the selected Branches-panel row,
// prefilled with its current name. Returns (model, false) when no row selected.
func (m Model) openRenameBranchPopup() (Model, bool) {
	bi, ok := m.backingIndex(panelBranches)
	if !ok {
		return m, false
	}
	cur := m.branches[bi].Name
	m.renameBranchPopup = &renameBranchPopup{old: cur, name: cur}
	return m, true
}

// renameBranchRow offers Rename branch on the Branches panel (no dedicated
// key; opens the rename popup). Available only when a branch row is selected
// and no op is running.
func (m Model) renameBranchRow() (actionRow, bool) {
	if m.focus != panelBranches || !m.opsIdle() {
		return actionRow{}, false
	}
	if _, ok := m.backingIndex(panelBranches); !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "rename-branch",
		label: "Rename branch",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m, _ = m.openRenameBranchPopup()
			return m, nil
		},
	}, true
}

// updateRenameBranchPopupKey handles one key while the popup is open. It
// swallows every key; ctrl+c still quits so the user is never trapped.
func (m Model) updateRenameBranchPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	p := m.renameBranchPopup
	switch msg.Type {
	case tea.KeyEsc:
		m.renameBranchPopup = nil
	case tea.KeyEnter:
		if p.name == "" || p.name == p.old {
			m.renameBranchPopup = nil
			return m, nil
		}
		op := engine.RenameBranch{Old: p.old, New: p.name}
		m.renameBranchPopup = nil
		return m.startOp(op)
	case tea.KeyBackspace, tea.KeyCtrlH:
		if r := []rune(p.name); len(r) > 0 {
			p.name = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		// branch names cannot contain spaces — ignore
	case tea.KeyRunes:
		p.name += string(msg.Runes)
	}
	return m, nil
}

// renderRenameBranchPopup draws the rename-branch dialog.
func (m Model) renderRenameBranchPopup() string {
	p := m.renameBranchPopup
	var b strings.Builder
	b.WriteString("Rename branch " + p.old + "\n\n")
	b.WriteString("name: " + p.name + "\n\n")
	b.WriteString("[type] name  [enter] rename  [esc] cancel")
	w, _ := m.overlayDims()
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}
