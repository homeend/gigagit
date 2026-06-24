package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/engine"
)

// stashActionPopup is the Apply/Pop/Drop menu for one stash.
type stashActionPopup struct {
	ref        string
	subject    string
	sel        int      // 0 Apply, 1 Pop, 2 Drop
	confirming bool     // Drop awaiting y/n
	mode       dispMode // text display mode; z cycles (cutoff default)
	hscroll    int      // modeScroll horizontal offset
}

var stashActions = []string{"Apply", "Pop", "Drop"}

// update handles all keys while the stash-action popup is open.
func (a *stashActionPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if a.confirming {
		switch msg.String() {
		case "y":
			m = m.popLayer()
			return m.startOp(engine.StashDrop{Ref: a.ref})
		case "n", "esc":
			a.confirming = false
		}
		return m, nil
	}
	switch msg.String() {
	case "z": // cycle the text display mode (cutoff / wrap / scroll)
		a.mode = a.mode.next()
		a.hscroll = 0
		return m, nil
	case "shift+left":
		if a.mode == modeScroll && a.hscroll > 0 {
			if a.hscroll -= m.hscrollStep(); a.hscroll < 0 {
				a.hscroll = 0
			}
		}
		return m, nil
	case "shift+right":
		if a.mode == modeScroll {
			a.hscroll += m.hscrollStep()
		}
		return m, nil
	case "esc":
		m = m.popLayer()
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
			m = m.popLayer()
			return m.startOp(engine.StashApply{Ref: a.ref})
		case 1:
			m = m.popLayer()
			return m.startOp(engine.StashPop{Ref: a.ref})
		case 2:
			a.confirming = true
		}
	}
	return m, nil
}

// render composites the stash-action popup over the layer beneath.
func (a *stashActionPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), a.box(m), w, h)
}

// box draws the stash-action popup box (modal box only).
func (a *stashActionPopup) box(m Model) string {
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	textW := popupTextWidth(inner)
	var b strings.Builder
	if a.confirming {
		b.WriteString("Drop " + a.ref + "?\n\n" + a.subject + "\n\n[y] drop   [n] cancel")
		return popupBox(inner, b.String())
	}
	b.WriteString("Stash " + a.ref + "\n" + a.subject + "\n\n")
	wr := make([]winRow, len(stashActions))
	for i, name := range stashActions {
		prefix := "  "
		var st lipgloss.Style
		if i == a.sel {
			prefix, st = "> ", selectedRow
		}
		wr[i] = winRow{text: prefix + name, style: st}
	}
	for _, line := range renderWindow(wr, winOpts{w: textW, h: len(stashActions), mode: a.mode, anchor: a.sel, hscroll: a.hscroll}) {
		b.WriteString(line + "\n")
	}
	b.WriteString("\n[enter] do  [z] mode  [esc] cancel")
	return popupBox(inner, b.String())
}
