package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// stashActionPopup is the Apply/Pop/Drop menu for one stash.
type stashActionPopup struct {
	ref        string
	subject    string
	sel        int  // 0 Apply, 1 Pop, 2 Drop
	confirming bool // Drop awaiting y/n
}

var stashActions = []string{"Apply", "Pop", "Drop"}

func (m Model) updateStashActionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	a := m.stashAction
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if a.confirming {
		switch msg.String() {
		case "y":
			m.stashAction = nil
			return m.startOp(engine.StashDrop{Ref: a.ref})
		case "n", "esc":
			a.confirming = false
		}
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.stashAction = nil
		return m, nil
	case "up", "k":
		if a.sel > 0 {
			a.sel--
		}
	case "down", "j":
		if a.sel < len(stashActions)-1 {
			a.sel++
		}
	case "enter":
		switch a.sel {
		case 0:
			m.stashAction = nil
			return m.startOp(engine.StashApply{Ref: a.ref})
		case 1:
			m.stashAction = nil
			return m.startOp(engine.StashPop{Ref: a.ref})
		case 2:
			a.confirming = true
		}
	}
	return m, nil
}

// renderStashActionPopup frames the menu like renderPairOpPopup.
func (m Model) renderStashActionPopup() string {
	a := m.stashAction
	w, _ := m.overlayDims()
	var b strings.Builder
	if a.confirming {
		b.WriteString("Drop " + a.ref + "?\n\n" + a.subject + "\n\n[y] drop   [n] cancel")
		return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
	}
	b.WriteString("Stash " + a.ref + "\n" + a.subject + "\n\n")
	for i, name := range stashActions {
		if i == a.sel {
			b.WriteString(selectedRow.Render("> "+name) + "\n")
		} else {
			b.WriteString("  " + name + "\n")
		}
	}
	b.WriteString("\n[enter] do  [esc] cancel")
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}
