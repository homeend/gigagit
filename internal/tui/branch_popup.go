package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// branchPopup holds the in-flight create-branch dialog.
type branchPopup struct {
	startPoint  string // selected branch the new one is based on
	name        string // typed branch name
	switchAfter bool   // B: smart-switch to the branch after creating it
}

// openBranchPopup builds the popup for the currently-selected branch. Returns
// (model, false) when no branch row is selected.
func (m Model) openBranchPopup(switchAfter bool) (Model, bool) {
	bi, ok := m.backingIndex(panelBranches)
	if !ok {
		return m, false
	}
	m.branchPopup = &branchPopup{startPoint: m.branches[bi].Name, switchAfter: switchAfter}
	return m, true
}

// updateBranchPopupKey handles one key while the popup is open. The popup
// swallows every key; ctrl+c still quits so the user is never trapped.
func (m Model) updateBranchPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	p := m.branchPopup
	switch msg.Type {
	case tea.KeyEsc:
		m.branchPopup = nil
	case tea.KeyEnter:
		if p.name == "" {
			return m, nil
		}
		op := engine.CreateBranch{Name: p.name, StartPoint: p.startPoint}
		if p.switchAfter {
			m.pendingSwitchBranch = p.name
		}
		m.branchPopup = nil
		return m.startOp(op)
	case tea.KeyBackspace, tea.KeyCtrlH:
		if r := []rune(p.name); len(r) > 0 {
			p.name = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		// Branch names cannot contain spaces; not inserting one avoids a
		// guaranteed validation error on create.
	case tea.KeyRunes:
		p.name += string(msg.Runes)
	}
	return m, nil
}

// renderBranchPopup draws the create-branch dialog.
func (m Model) renderBranchPopup() string {
	p := m.branchPopup
	var b strings.Builder
	title := "Create branch from " + p.startPoint
	if p.switchAfter {
		title = "Create + switch branch from " + p.startPoint
	}
	b.WriteString(title + "\n\n")
	b.WriteString("name: " + p.name + "\n\n")
	b.WriteString("[type] name  [enter] create  [esc] cancel")
	w, _ := m.overlayDims()
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}
