package tui

import (
	"context"
	"strconv"
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
	field     int // 0 = summary, 1 = rationale, 2 = side (add only, when the row has both)
	ratScroll int

	// anchors are the sides a new note may hang off (new first); pick indexes
	// the chosen one. Empty on edit/reply, where the thread fixes the side.
	anchors []noteAnchor
	pick    int

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
		p.anchors = m.noteAnchorsAtCursor()
		if len(p.anchors) == 0 {
			return m, nil
		}
		p.setPick(0)
		p.summary, p.rationale = newTextField(""), newTextField("")
	case noteEdit, noteReply:
		return m.withNoteTarget(func(m Model, t noteTarget) (tea.Model, tea.Cmd) {
			return m.openNotePopupFor(mode, t)
		})
	}
	return m.pushLayer(p), nil
}

// openNotePopupFor opens the edit/reply form on one targeted note.
func (m Model) openNotePopupFor(mode noteFormMode, t noteTarget) (tea.Model, tea.Cmd) {
	addr, ok := m.diffNoteAddress()
	if !ok {
		return m, nil
	}
	p := &notePopup{mode: mode, addr: addr, author: m.identity.EffectiveName}
	p.side, p.line, p.hash = t.side, t.line, t.hash
	if mode == noteEdit {
		// Edit acts on the targeted ROW's own note (which may be a reply).
		p.targetID = t.note.ID
		p.summary, p.rationale = newTextField(t.note.Summary), newTextField(t.note.Rationale)
	} else {
		// Reply threads onto the ROOT — NoteReply flattens to it anyway,
		// and inherits the root's address/side/range/fingerprint.
		p.targetID = t.rootID
		p.summary, p.rationale = newTextField(""), newTextField("")
	}
	return m.pushLayer(p), nil
}

// setPick chooses anchor i as the note's side/line/fingerprint.
func (p *notePopup) setPick(i int) {
	if len(p.anchors) == 0 {
		return
	}
	p.pick = ((i % len(p.anchors)) + len(p.anchors)) % len(p.anchors)
	a := p.anchors[p.pick]
	p.side, p.line, p.hash = a.side, a.line, a.hash
}

// hasSideField reports whether the form offers a side choice: only on add,
// and only when the cursor row exists in both versions.
func (p *notePopup) hasSideField() bool { return p.mode == noteAdd && len(p.anchors) > 1 }

// fields is how many tab stops the form has.
func (p *notePopup) fields() int {
	if p.hasSideField() {
		return 3
	}
	return 2
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
	case tea.KeyTab:
		p.field = (p.field + 1) % p.fields()
		return m, nil
	case tea.KeyShiftTab:
		p.field = (p.field + p.fields() - 1) % p.fields()
		return m, nil
	case tea.KeyEnter:
		switch p.field {
		case 0:
			p.field = 1
		case 1:
			p.rationale.InsertNewline()
		default:
			p.field = 0
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
	switch p.field {
	case 0:
		p.summary.HandleEditKey(msg)
	case 1:
		p.rationale.HandleEditKey(msg)
	default:
		// The side field: ←/→, space, h/l or the side's own letter flip it.
		switch msg.String() {
		case "left", "h", "shift+tab":
			p.setPick(p.pick - 1)
		case "right", "l", " ":
			p.setPick(p.pick + 1)
		case "o":
			p.pickSide(model.NoteSideOld)
		case "n":
			p.pickSide(model.NoteSideNew)
		}
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

	cur := func(i int) string {
		if p.field == i {
			return "> "
		}
		return "  "
	}
	var b strings.Builder
	b.WriteString(heading + "  " + i18n.T("%s line %d", noteSideLabel(p.side), p.line) + "\n\n")
	// Field labels follow the commit popup: plain, not translated.
	b.WriteString(viewField(cur(0)+"summary:   ", p.summary, p.field == 0, contentW) + "\n")
	b.WriteString(viewFieldWindow(cur(1)+"rationale: ", p.rationale, p.field == 1, contentW, 8, &p.ratScroll) + "\n")
	if p.hasSideField() {
		// "side:  ● new R12   ○ old L12" — the picked anchor is filled in.
		var opts []string
		for i, a := range p.anchors {
			dot := "○ "
			if i == p.pick {
				dot = "● "
			}
			opts = append(opts, dot+noteSideLabel(a.side)+" "+noteSideMark(a.side)+strconv.Itoa(a.line))
		}
		line := cur(2) + "side:      " + strings.Join(opts, "   ")
		if p.field == 2 {
			line += "   " + i18n.T("[←/→] pick")
		}
		b.WriteString(truncate(line, contentW) + "\n")
	}
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

// pickSide selects the anchor on side if the form offers it.
func (p *notePopup) pickSide(side model.NoteSide) {
	for i, a := range p.anchors {
		if a.side == side {
			p.setPick(i)
			return
		}
	}
}

// noteSideLabel is the side's word in the form: "new" or "old".
func noteSideLabel(side model.NoteSide) string {
	if side == model.NoteSideOld {
		return i18n.T("old side")
	}
	return i18n.T("new side")
}

// noteSideMark is the side's letter in a line reference: R for the new side,
// L for the old (the box title uses the same).
func noteSideMark(side model.NoteSide) string {
	if side == model.NoteSideOld {
		return "L"
	}
	return "R"
}
