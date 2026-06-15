package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// commitPopup collects a commit message as a subject (title) plus an optional
// multi-line body (description), and commits the staged index on ctrl+s.
type commitPopup struct {
	title string
	desc  string
	field int // 0 = title, 1 = description
}

// message assembles the git commit message: subject alone, or subject + blank
// line + body when the body is non-empty.
func (p *commitPopup) message() string {
	t := strings.TrimSpace(p.title)
	if strings.TrimSpace(p.desc) == "" {
		return t
	}
	return t + "\n\n" + p.desc
}

// updateCommitPopupKey handles one key while the commit popup is open. It
// swallows every key (no fallthrough): esc cancels, ctrl+c quits, ctrl+s commits.
func (m Model) updateCommitPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	p := m.commitPopup
	switch msg.Type {
	case tea.KeyEsc:
		m.commitPopup = nil
	case tea.KeyCtrlS:
		if strings.TrimSpace(p.title) == "" {
			m.statusMsg = "title required"
			return m, nil
		}
		op := engine.Commit{Message: p.message()}
		m.commitPopup = nil
		return m.startOp(op)
	case tea.KeyTab, tea.KeyShiftTab:
		p.field = (p.field + 1) % 2
	case tea.KeyEnter:
		if p.field == 0 {
			p.field = 1 // title → description
		} else {
			p.desc += "\n" // newline within the body
		}
	case tea.KeyBackspace:
		if p.field == 0 {
			if r := []rune(p.title); len(r) > 0 {
				p.title = string(r[:len(r)-1])
			}
		} else {
			if r := []rune(p.desc); len(r) > 0 {
				p.desc = string(r[:len(r)-1])
			}
		}
	case tea.KeySpace:
		if p.field == 0 {
			p.title += " "
		} else {
			p.desc += " "
		}
	case tea.KeyRunes:
		if p.field == 0 {
			p.title += string(msg.Runes)
		} else {
			p.desc += string(msg.Runes)
		}
	}
	return m, nil
}

// renderCommitPopup draws the two-field commit dialog.
func (m Model) renderCommitPopup() string {
	p := m.commitPopup
	var b strings.Builder
	b.WriteString("Commit\n\n")

	titleCur, descCur := "  ", "  "
	if p.field == 0 {
		titleCur = "> "
	} else {
		descCur = "> "
	}
	b.WriteString(titleCur + "title:       " + p.title + "\n")
	descLines := strings.Split(p.desc, "\n")
	b.WriteString(descCur + "description: " + descLines[0] + "\n")
	for _, l := range descLines[1:] {
		b.WriteString("             " + l + "\n")
	}

	b.WriteString("\n[tab] switch field  [enter] newline/next  [ctrl+s] commit  [esc] cancel")

	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	return modalStyle.Width(inner).Render(b.String()) + "\n"
}
