package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// renameBranchPopup holds the in-flight rename-branch dialog. The text field is
// pre-filled with the branch's current name.
type renameBranchPopup struct {
	old  string    // the branch being renamed
	name textfield // typed new name
}

// openRenameBranchPopup opens the dialog for the selected Branches-panel row,
// prefilled with its current name. Returns (model, false) when no row selected.
func (m Model) openRenameBranchPopup() (Model, bool) {
	bi, ok := m.backingIndex(panelBranches)
	if !ok {
		return m, false
	}
	cur := m.branches[bi].Name
	m = m.pushLayer(&renameBranchPopup{old: cur, name: newTextField(cur)})
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
		m = m.popLayer()
	case tea.KeyEnter:
		if p.name.Value() == "" || p.name.Value() == p.old {
			m = m.popLayer()
			return m, nil
		}
		op := engine.RenameBranch{Old: p.old, New: p.name.Value()}
		m = m.popLayer()
		return m.startOp(op)
	case tea.KeySpace:
		// branch names cannot contain spaces — drop it
	default:
		p.name.HandleEditKey(msg)
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
	w, _ := m.overlayDims()
	b.WriteString(viewField("name: ", p.name, true, popupContentWidth(w)) + "\n\n")
	b.WriteString("[type] name  [enter] rename  [esc] cancel")
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}
