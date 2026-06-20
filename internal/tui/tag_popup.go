package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// tagPopup is the create-tag dialog at a commit. An empty message creates a
// lightweight tag; a non-empty one an annotated tag. tab toggles the field.
type tagPopup struct {
	commit  string // full SHA the tag points at
	name    string
	message string
	onMsg   bool // false = editing name, true = editing message
}

func (p *tagPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyTab:
		p.onMsg = !p.onMsg
		return m, nil
	case tea.KeyEnter:
		if p.name == "" {
			return m, nil
		}
		op := engine.CreateTag{Name: p.name, Commit: p.commit, Message: p.message}
		m = m.popLayer()
		return m.startOp(op)
	case tea.KeyBackspace, tea.KeyCtrlH:
		if p.onMsg {
			if r := []rune(p.message); len(r) > 0 {
				p.message = string(r[:len(r)-1])
			}
		} else if r := []rune(p.name); len(r) > 0 {
			p.name = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		// Tag names cannot contain spaces; the annotated message can.
		if p.onMsg {
			p.message += " "
		}
	case tea.KeyRunes:
		if p.onMsg {
			p.message += string(msg.Runes)
		} else {
			p.name += string(msg.Runes)
		}
	}
	return m, nil
}

func (p *tagPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *tagPopup) box(m Model) string {
	nameMark, msgMark := "> ", "  "
	if p.onMsg {
		nameMark, msgMark = "  ", "> "
	}
	var b strings.Builder
	b.WriteString("Create tag at " + displayStart(p.commit) + "\n\n")
	b.WriteString(nameMark + "name:    " + p.name + "\n")
	b.WriteString(msgMark + "message: " + p.message + "  (empty = lightweight)\n\n")
	b.WriteString("[tab] field  [enter] create  [esc] cancel")
	w, _ := m.overlayDims()
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}
