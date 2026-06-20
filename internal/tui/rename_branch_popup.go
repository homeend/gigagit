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
	m = m.pushOverlay(&renameBranchPopup{old: cur, name: cur})
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

// update handles one key while the popup is open. It swallows every key; ctrl+c
// still quits so the user is never trapped.
func (p *renameBranchPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		m = m.popOverlay()
	case tea.KeyEnter:
		if p.name == "" || p.name == p.old {
			m = m.popOverlay()
			return m, nil
		}
		op := engine.RenameBranch{Old: p.old, New: p.name}
		m = m.popOverlay()
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

// render composites the rename-branch dialog over the layer beneath.
func (p *renameBranchPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

// box draws the rename-branch dialog (modal box only).
func (p *renameBranchPopup) box(m Model) string {
	var b strings.Builder
	b.WriteString("Rename branch " + p.old + "\n\n")
	b.WriteString("name: " + p.name + "\n\n")
	b.WriteString("[type] name  [enter] rename  [esc] cancel")
	w, _ := m.overlayDims()
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}
