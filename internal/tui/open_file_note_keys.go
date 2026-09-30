package tui

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// The user's side of an open file's temporary notes: } / { step through
// them (then on to the next open file that has any — the diff view's rule),
// d dismisses the note under the cursor, r copies a reference to it.

// reference is what the user pastes to the agent to talk about this note:
// the id (gg session note show resolves it) plus path:lines, which still
// mean something once the note is gone.
func (n *fileNote) reference(path string) string {
	ref := "gg note " + n.id + " " + path + ":" + strconv.Itoa(n.start)
	if n.end != n.start {
		ref += "-" + strconv.Itoa(n.end)
	}
	return ref
}

// anyFileNotes reports whether any open file of this worktree has notes.
func (m Model) anyFileNotes() bool {
	for _, d := range m.openFiles.list(m.currentWorktree) {
		if len(d.notes) > 0 {
			return true
		}
	}
	return false
}

// previewNoteKey gives the focused open file's notes first refusal on a key.
// It declines everything while no open file has notes, and d / r on a line
// that carries none, so those keys keep whatever else they mean there.
func (m Model) previewNoteKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	d, ok := m.focusedDoc()
	if !ok || !m.anyFileNotes() {
		return m, nil, false
	}
	switch msg.String() {
	case "}":
		return m.stepFileNote(d, 1)
	case "{":
		return m.stepFileNote(d, -1)
	case "d":
		if n := d.noteAt(d.p.cur + 1); n != nil {
			d.removeNote(n.id)
			m.statusMsg = i18n.T("note %s dismissed", n.id)
			return m, nil, true
		}
	case "r":
		if n := d.noteAt(d.p.cur + 1); n != nil {
			return m, m.copyToClipboardCmd(i18n.T("Copied note reference %s", n.id), n.reference(d.path)), true
		}
	}
	return m, nil, false
}

// stepFileNote moves to the next (dir > 0) or previous note: inside d first,
// by start line from the cursor; past its last one, the next open file with
// notes comes to the front on its first (last) note.
func (m Model) stepFileNote(d *openFile, dir int) (Model, tea.Cmd, bool) {
	line := d.p.cur + 1
	var hit *fileNote
	for _, n := range d.notes {
		if dir > 0 && n.start > line {
			hit = n
			break
		}
		if dir < 0 && n.start < line {
			hit = n // keep going: the LAST one above the cursor
		}
	}
	if hit != nil {
		_, rows, _, _ := m.activePreview()
		d.p.cur = hit.start - 1
		d.p.ensureCursorVisible(rows)
		// Bring the box in too, as far as that keeps the cursor line on screen.
		for d.p.sel < d.p.cur && d.p.rowsSpan(d.p.sel, hit.end) > rows {
			d.p.sel++
		}
		return m, nil, true
	}
	l := m.openFiles.list(m.currentWorktree)
	at := 0
	for i, e := range l {
		if e == d {
			at = i
		}
	}
	for i := 1; i < len(l); i++ {
		e := l[((at+dir*i)%len(l)+len(l))%len(l)]
		if len(e.notes) == 0 {
			continue
		}
		target := e.notes[0]
		if dir < 0 {
			target = e.notes[len(e.notes)-1]
		}
		e.pendingLine = target.start
		nm, load := m.bringToFront(e)
		if load == nil { // already loaded and not re-read: land the line now
			rows, _ := nm.viewerGeom()
			e.landPendingLine(rows)
		}
		return nm, load, true
	}
	if len(d.notes) == 0 {
		return m, nil, false // nothing anywhere to step to: not our key
	}
	m.statusMsg = i18n.T("no more notes")
	return m, nil, true
}

// fileNoteRows are the . menu's rows for the focused open file's notes —
// the keys for the mouse. None while it has no notes.
func (m Model) fileNoteRows() []actionRow {
	d, ok := m.focusedDoc()
	if !ok || len(d.notes) == 0 {
		return nil
	}
	rows := []actionRow{
		{id: "note-next", key: "}", label: i18n.T("Next note"), run: func(m Model) (tea.Model, tea.Cmd) {
			nm, cmd, _ := m.stepFileNote(d, 1)
			return nm, cmd
		}},
		{id: "note-prev", key: "{", label: i18n.T("Previous note"), run: func(m Model) (tea.Model, tea.Cmd) {
			nm, cmd, _ := m.stepFileNote(d, -1)
			return nm, cmd
		}},
	}
	n := d.noteAt(d.p.cur + 1)
	if n == nil {
		n = d.notes[0] // the menu has no cursor of its own: offer the first note
	}
	ref := m.copyRow("copy-note-ref", i18n.T("Copy note reference"), i18n.T("Copied note reference %s", n.id), n.reference(d.path))
	ref.key = "r"
	return append(rows, ref, actionRow{id: "note-dismiss", key: "d", label: i18n.T("Dismiss note"), run: func(m Model) (tea.Model, tea.Cmd) {
		d.removeNote(n.id)
		m.statusMsg = i18n.T("note %s dismissed", n.id)
		return m, nil
	}})
}

// noteHint leads hint with the note keys while d carries notes.
func (m Model) noteHint(d *openFile, hint string) string {
	if d == nil || len(d.notes) == 0 {
		return hint
	}
	return i18n.T("[}/{] notes  [d] dismiss  [r] reference") + "  " + hint
}
