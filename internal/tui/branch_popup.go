package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// branchPopup holds the in-flight create-branch dialog.
type branchPopup struct {
	startPoint  string    // selected branch the new one is based on
	name        textfield // typed branch name
	switchAfter bool      // B: smart-switch to the branch after creating it
}

// openBranchPopup builds the popup for the currently-selected branch. Returns
// (model, false) when no branch row is selected.
func (m Model) openBranchPopup(switchAfter bool) (Model, bool) {
	bi, ok := m.backingIndex(panelBranches)
	if !ok {
		return m, false
	}
	m = m.pushLayer(&branchPopup{startPoint: m.branches[bi].Name, switchAfter: switchAfter})
	return m, true
}

// update handles one key while the popup is open. The popup swallows every
// key; ctrl+c still quits so the user is never trapped.
func (p *branchPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		m = m.popLayer()
	case tea.KeyEnter:
		if p.name.Value() == "" {
			return m, nil
		}
		op := engine.CreateBranch{Name: p.name.Value(), StartPoint: p.startPoint}
		if p.switchAfter {
			m.pendingSwitchBranch = p.name.Value()
		}
		m = m.popLayer()
		return m.startOp(op)
	case tea.KeySpace:
		// Branch names cannot contain spaces; dropping it avoids a guaranteed
		// validation error on create.
	default:
		p.name.HandleEditKey(msg)
	}
	return m, nil
}

// render composites the popup box over the layer beneath.
func (p *branchPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

// displayStart shortens a full 40-char hex SHA to 7 chars for display (the op
// still receives the full, unambiguous start-point). Branch names pass through.
func displayStart(s string) string {
	if len(s) != 40 {
		return s
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return s
		}
	}
	return s[:7]
}

// box draws the create-branch dialog (modal box only).
func (p *branchPopup) box(m Model) string {
	var b strings.Builder
	start := displayStart(p.startPoint)
	title := "Create branch from " + start
	if p.switchAfter {
		title = "Create + switch branch from " + start
	}
	b.WriteString(title + "\n\n")
	w, _ := m.overlayDims()
	b.WriteString(viewField("name: ", p.name, true, popupContentWidth(w)) + "\n\n")
	b.WriteString("[type] name  [enter] create  [esc] cancel")
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}
