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
	title textfield
	desc  textfield
	field int // 0 = title, 1 = description
	amend bool
}

// message assembles the git commit message: subject alone, or subject + blank
// line + body when the body is non-empty.
func (p *commitPopup) message() string {
	t := strings.TrimSpace(p.title.Value())
	if strings.TrimSpace(p.desc.Value()) == "" {
		return t
	}
	return t + "\n\n" + p.desc.Value()
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
		return false, false
	case tea.KeyEnter:
		if p.field == 0 {
			p.field = 1 // title → description
		} else {
			p.desc.InsertNewline() // newline within the body
		}
		return false, false
	case tea.KeyUp:
		if p.field == 1 {
			p.desc.Up()
		}
		return false, false
	case tea.KeyDown:
		if p.field == 1 {
			p.desc.Down()
		}
		return false, false
	}
	if p.field == 0 {
		p.title.HandleEditKey(msg)
	} else {
		p.desc.HandleEditKey(msg)
	}
	return false, false
}

// update handles one key while the commit popup is open. It swallows every key:
// esc cancels, ctrl+c quits, ctrl+s commits.
func (p *commitPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	submit, cancel := p.applyEditKey(msg)
	switch {
	case cancel:
		m = m.popLayer()
	case submit:
		if strings.TrimSpace(p.title.Value()) == "" {
			m.statusMsg = "title required"
			return m, nil
		}
		op := engine.Commit{Message: p.message(), Amend: p.amend}
		m = m.popLayer()
		return m.startOp(op)
	}
	return m, nil
}

// render composites the commit dialog over the layer beneath.
func (p *commitPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

// box draws the two-field commit dialog (modal box only).
func (p *commitPopup) box(m Model) string {
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

// renderCommitFields draws the title/description fields with the focus cursor.
func renderCommitFields(p *commitPopup) string {
	var b strings.Builder
	titleCur, descCur := "  ", "  "
	if p.field == 0 {
		titleCur = "> "
	} else {
		descCur = "> "
	}
	b.WriteString(titleCur + "title:       " + p.title.View(p.field == 0) + "\n")
	descLines := strings.Split(p.desc.View(p.field == 1), "\n")
	b.WriteString(descCur + "description: " + descLines[0] + "\n")
	for _, l := range descLines[1:] {
		b.WriteString("             " + l + "\n")
	}
	return b.String()
}
