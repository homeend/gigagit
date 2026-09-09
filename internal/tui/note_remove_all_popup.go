package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// noteRemoveAllToken is what the user must type to confirm. It stays English
// in every language: it is a typed guard, not prose — a translated token would
// silently change the gesture under a user who switched language, and the
// popup quotes the literal string anyway.
const noteRemoveAllToken = "remove all"

// notesClearedMsg reports the outcome of a whole-address clear. It carries the
// count so the notice can name it; the refresh it triggers is the same one every
// other note mutation does.
type notesClearedMsg struct {
	n   int
	err error
}

// noteRemoveAllPopup is the typed confirmation behind "Remove all notes…".
// Deleting a file's whole review history is unrecoverable and — unlike the
// single-note delete — quotes nothing the user can check row by row, so a
// yes/no modal is too cheap a gesture: the token has to be typed out.
type noteRemoveAllPopup struct {
	popupMax
	field   textfield
	addr    model.FileAddress
	path    string
	roots   int  // threads the open diff shows (the number the prose quotes)
	replies int  // replies those threads carry
	refused bool // the last enter did not match: show the hint
}

// noteRemoveAllRow is the . menu's "Remove all notes…", offered under the same
// whole-diff gate as "List notes…" — the scope is the open diff's address, not
// the repository.
func (m Model) noteRemoveAllRow() (actionRow, bool) {
	if !m.diffHasNotes() {
		return actionRow{}, false
	}
	if _, ok := m.diffNoteAddress(); !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "note-remove-all",
		label: i18n.T("Remove all notes…"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openNoteRemoveAll()
		},
	}, true
}

// openNoteRemoveAll pushes the confirmation over the diff.
func (m Model) openNoteRemoveAll() (tea.Model, tea.Cmd) {
	addr, ok := m.diffNoteAddress()
	if !ok {
		return m, nil
	}
	v := m.diffLayer()
	if v == nil || len(v.notes) == 0 {
		return m, nil
	}
	p := &noteRemoveAllPopup{field: newTextField(""), addr: addr, path: addr.Path, roots: len(v.notes)}
	for _, r := range v.notes {
		p.replies += len(r.Replies)
	}
	return m.pushLayer(p), nil
}

// confirmed reports whether the field currently holds the token, ignoring
// surrounding whitespace and case.
func (p *noteRemoveAllPopup) confirmed() bool {
	return strings.EqualFold(strings.TrimSpace(p.field.Value()), noteRemoveAllToken)
}

// update handles one key. Every key is swallowed (the field is the whole
// popup); ctrl+c still quits so the user is never trapped.
func (p *noteRemoveAllPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		if !p.confirmed() {
			// Anything else keeps the popup and says what was expected —
			// never a partial removal, never a silent no-op.
			p.refused = true
			return m, nil
		}
		addr := p.addr
		m = m.popLayer()
		return m, m.notesClearCmd(addr)
	}
	p.refused = false // editing clears the complaint
	p.field.HandleEditKey(msg)
	return m, nil
}

func (p *noteRemoveAllPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *noteRemoveAllPopup) box(m Model) string {
	w, _ := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupInnerWidth(w))
	contentW := popupTextWidth(inner)

	lead := i18n.T("This deletes %d notes from %s.", p.roots, p.path)
	if p.replies > 0 {
		lead = i18n.T("This deletes %d notes and their replies from %s.", p.roots, p.path)
	}
	var b strings.Builder
	b.WriteString(i18n.T("Remove all notes…") + "\n\n")
	for _, ln := range wrapWords(lead, contentW) {
		b.WriteString(ln + "\n")
	}
	// The token is an argument, so it stays English in every bundle.
	for _, ln := range wrapWords(i18n.T("Type `%s` to confirm.", noteRemoveAllToken), contentW) {
		b.WriteString(ln + "\n")
	}
	b.WriteString("\n" + viewField("> ", p.field, true, contentW) + "\n")
	if p.refused {
		b.WriteString(errorStyle.Render(truncate(i18n.T("type exactly: %s", noteRemoveAllToken), contentW)) + "\n")
	}
	b.WriteString("\n" + packHints([]string{
		i18n.T("[enter] confirm"),
		i18n.T("[esc] cancel"),
	}, contentW))
	return popupBox(inner, b.String())
}

// notesClearCmd removes every note at addr off the UI thread.
func (m Model) notesClearCmd(addr model.FileAddress) tea.Cmd {
	svc := m.svc
	if svc == nil {
		return nil
	}
	return func() tea.Msg {
		n, err := svc.NotesClear(context.Background(), addr)
		return notesClearedMsg{n: n, err: err}
	}
}
