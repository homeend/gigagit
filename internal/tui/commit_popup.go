package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// commitPopup collects a commit message as a subject (title) plus an optional
// multi-line body (description), and commits the staged index on ctrl+s.
// amend=true rewrites the last commit instead of creating a new one.
type commitPopup struct {
	title string
	desc  string
	field int // 0 = title, 1 = description
	amend bool
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

// splitMessage parses an existing commit message into (subject, body) for the
// amend pre-fill: the first line is the subject, the rest (after blank lines)
// the body.
func splitMessage(msg string) (title, desc string) {
	msg = strings.TrimRight(msg, "\n")
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		return msg[:i], strings.TrimLeft(msg[i+1:], "\n")
	}
	return msg, ""
}

// applyEditKey applies one key to the popup's title/description fields and
// reports control outcomes: submit=true on ctrl+s, cancel=true on esc. Editing
// keys (tab/enter/backspace/space/runes) mutate in place and return false,false.
// ctrl+c is handled by the caller (it quits the program). Reused by F2's commit
// popup and the interactive-rebase editor's reword sub-mode.
func (p *commitPopup) applyEditKey(msg tea.KeyMsg) (submit, cancel bool) {
	switch msg.Type {
	case tea.KeyEsc:
		return false, true
	case tea.KeyCtrlS:
		return true, false
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
	return false, false
}

// updateCommitPopupKey handles one key while the commit popup is open. It
// swallows every key (no fallthrough): esc cancels, ctrl+c quits, ctrl+s commits.
func (m Model) updateCommitPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	p := m.commitPopup
	submit, cancel := p.applyEditKey(msg)
	switch {
	case cancel:
		m.commitPopup = nil
	case submit:
		if strings.TrimSpace(p.title) == "" {
			m.statusMsg = "title required"
			return m, nil
		}
		op := engine.Commit{Message: p.message(), Amend: p.amend}
		m.commitPopup = nil
		return m.startOp(op)
	}
	return m, nil
}

// renderCommitFields draws the title/description fields with the focus cursor.
func renderCommitFields(p *commitPopup) string {
	var b strings.Builder
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
	return b.String()
}

// renderCommitPopup draws the two-field commit dialog.
func (m Model) renderCommitPopup() string {
	p := m.commitPopup
	var b strings.Builder
	heading := "Commit"
	if p.amend {
		heading = "Amend last commit"
	}
	b.WriteString(heading + "\n\n")
	b.WriteString(renderCommitFields(p))
	b.WriteString("\n[tab] switch field  [enter] newline/next  [ctrl+s] commit  [esc] cancel")

	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	return modalStyle.Width(inner).Render(b.String()) + "\n"
}
