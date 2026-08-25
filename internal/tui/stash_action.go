package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// stashActionPopup is the Apply/Pop/Drop menu for one stash.
type stashActionPopup struct {
	popupMax
	ref        string
	subject    string
	sel        int      // 0 Apply, 1 Pop, 2 Drop
	confirming bool     // Drop awaiting y/n
	mode       dispMode // text display mode; z cycles (cutoff default)
	hscroll    int      // modeScroll horizontal offset
}

var stashActions = []string{"Apply", "Pop", "Drop"}

// stashActionRows offers Apply / Pop / Drop on the selected stash for the
// .-menu while the stash list is the front surface. Apply and Pop start their
// op directly; Drop confirms first (the same guard the enter popup has).
func (m Model) stashActionRows() []actionRow {
	v := m.stashView
	if v == nil || v.sel < 0 || v.sel >= len(v.entries) {
		return nil
	}
	e := v.entries[v.sel]
	return []actionRow{
		{id: "stash-apply", label: i18n.T("Apply stash"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startOp(engine.StashApply{Ref: e.Ref})
		}},
		{id: "stash-pop", label: i18n.T("Pop stash"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startOp(engine.StashPop{Ref: e.Ref})
		}},
		{id: "stash-drop", label: i18n.T("Drop stash"), run: func(m Model) (tea.Model, tea.Cmd) {
			m.modal = &decisionState{
				req: engine.DecisionRequest{
					ID:      "stash-drop",
					Prompt:  i18n.T("Drop %s?", e.Ref),
					Options: []string{"Drop", "Cancel"},
				},
				onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
					if opt == "Drop" {
						return m.startOp(engine.StashDrop{Ref: e.Ref})
					}
					return m, nil
				},
			}
			return m, nil
		}},
	}
}

// stashActionLabel translates a stashActions entry for display at render
// time (a package var initializer would freeze the English text before any
// language loads — see cfLabel's identical rationale).
func stashActionLabel(name string) string {
	switch name {
	case "Apply":
		return i18n.T("Apply")
	case "Pop":
		return i18n.T("Pop")
	case "Drop":
		return i18n.T("Drop")
	}
	return name
}

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
	inner := popupResolveWidth(w, a.maximized, popupInnerWidth(w))
	textW := popupTextWidth(inner)
	var b strings.Builder
	if a.confirming {
		b.WriteString(i18n.T("Drop %s?", a.ref) + "\n\n" + a.subject + "\n\n" + i18n.T("[y] drop   [n] cancel"))
		return popupBox(inner, b.String())
	}
	b.WriteString(i18n.T("Stash %s", a.ref) + "\n" + a.subject + "\n\n")
	wr := make([]winRow, len(stashActions))
	for i, name := range stashActions {
		prefix := "  "
		var st lipgloss.Style
		if i == a.sel {
			prefix, st = "> ", selectedRow
		}
		wr[i] = winRow{text: prefix + stashActionLabel(name), style: st}
	}
	for _, line := range renderWindow(wr, winOpts{w: textW, h: len(stashActions), mode: a.mode, anchor: a.sel, hscroll: a.hscroll}) {
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + i18n.T("[enter] do  [z] mode  [esc] cancel"))
	return popupBox(inner, b.String())
}
