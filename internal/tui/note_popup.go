package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// noteFormMode is what the popup will do on ctrl+s.
type noteFormMode int

const (
	noteAdd noteFormMode = iota
	noteEdit
	noteReply
)

// notePopup collects a note's summary and optional rationale — the commit
// popup's title/description pair, with the same keys (tab switches, enter is
// next/newline, ctrl+s saves, esc cancels, ctrl+t maximizes). An empty summary
// on ctrl+s is a cancel (§4.4).
type notePopup struct {
	popupMax
	summary   textfield
	rationale textfield
	field     int // 0 = summary, 1 = rationale
	ratScroll int

	mode     noteFormMode
	targetID string // noteEdit: the note; noteReply: the parent
	addr     model.FileAddress
	side     model.NoteSide
	line     int
	hash     string
	author   string
}

// openNotePopup pushes the form for mode, anchored at the cursor (add) or at
// the note nearest the cursor (edit/reply). Inert when the surface carries no
// address, or when edit/reply have no note to act on.
func (m Model) openNotePopup(mode noteFormMode) (tea.Model, tea.Cmd) {
	addr, ok := m.diffNoteAddress()
	if !ok {
		return m, nil
	}
	p := &notePopup{mode: mode, addr: addr, author: m.identity.EffectiveName}
	switch mode {
	case noteAdd:
		side, line, hash, aok := m.noteAnchorAtCursor()
		if !aok {
			return m, nil
		}
		p.side, p.line, p.hash = side, line, hash
		p.summary, p.rationale = newTextField(""), newTextField("")
	case noteEdit, noteReply:
		r, rok := m.noteNearCursor()
		if !rok {
			return m, nil
		}
		p.targetID = r.Note.ID
		p.side, p.line, p.hash = r.Note.Side, r.Range[1], r.Note.ContextHash
		if mode == noteEdit {
			p.summary, p.rationale = newTextField(r.Note.Summary), newTextField(r.Note.Rationale)
		} else {
			p.summary, p.rationale = newTextField(""), newTextField("")
		}
	}
	return m.pushLayer(p), nil
}

// update handles one key. It swallows every key: esc cancels, ctrl+c quits,
// ctrl+s saves (or cancels on an empty summary).
func (p *notePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyCtrlS:
		m = m.popLayer()
		if strings.TrimSpace(p.summary.Value()) == "" {
			return m, nil // an empty summary is a cancel
		}
		return m, m.noteSubmitCmd(p)
	case tea.KeyTab, tea.KeyShiftTab:
		p.field = (p.field + 1) % 2
		return m, nil
	case tea.KeyEnter:
		if p.field == 0 {
			p.field = 1
		} else {
			p.rationale.InsertNewline()
		}
		return m, nil
	case tea.KeyUp:
		if p.field == 1 {
			p.rationale.Up()
		}
		return m, nil
	case tea.KeyDown:
		if p.field == 1 {
			p.rationale.Down()
		}
		return m, nil
	}
	if p.field == 0 {
		p.summary.HandleEditKey(msg)
	} else {
		p.rationale.HandleEditKey(msg)
	}
	return m, nil
}

func (p *notePopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *notePopup) box(m Model) string {
	w, _ := m.overlayDims()
	innerW := popupResolveWidth(w, p.maximized, commitNormalWidth(w))
	contentW := popupTextWidth(innerW)
	heading := i18n.T("Add note")
	switch p.mode {
	case noteEdit:
		heading = i18n.T("Edit note")
	case noteReply:
		heading = i18n.T("Reply to note")
	}
	footer := packHints([]string{
		i18n.T("[tab] switch field"),
		i18n.T("[enter] newline/next"),
		i18n.T("[ctrl+t] fullscreen"),
		i18n.T("[ctrl+s] save"),
		i18n.T("[esc] cancel"),
	}, contentW)

	sumCur, ratCur := "  ", "  "
	if p.field == 0 {
		sumCur = "> "
	} else {
		ratCur = "> "
	}
	var b strings.Builder
	b.WriteString(heading + "  " + i18n.T("line %d", p.line) + "\n\n")
	// Field labels follow the commit popup: plain, not translated.
	b.WriteString(viewField(sumCur+"summary:   ", p.summary, p.field == 0, contentW) + "\n")
	b.WriteString(viewFieldWindow(ratCur+"rationale: ", p.rationale, p.field == 1, contentW, 8, &p.ratScroll) + "\n")
	b.WriteString("\n" + footer)
	return popupBox(innerW, b.String())
}

// noteSubmitCmd performs the popup's write off the UI thread.
func (m Model) noteSubmitCmd(p *notePopup) tea.Cmd {
	svc := m.svc
	if svc == nil {
		return nil
	}
	summary := strings.TrimSpace(p.summary.Value())
	rationale := strings.TrimSpace(p.rationale.Value())
	mode, id := p.mode, p.targetID
	n := model.Note{
		Source: model.NoteSourceUser, Author: p.author, Address: p.addr,
		Side: p.side, Range: [2]int{p.line, p.line}, ContextHash: p.hash,
		Summary: summary, Rationale: rationale,
	}
	return func() tea.Msg {
		ctx := context.Background()
		var err error
		switch mode {
		case noteEdit:
			err = svc.NoteEdit(ctx, id, summary, rationale)
		case noteReply:
			_, err = svc.NoteReply(ctx, id, n)
		default:
			_, err = svc.NoteAdd(ctx, n)
		}
		return noteMutatedMsg{err: err}
	}
}
