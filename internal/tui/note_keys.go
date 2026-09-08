package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

// notesLoadedMsg carries the resolved notes for one open diff. tag gates a
// stale result exactly like diffMsg (the diff may have been stepped away).
type notesLoadedMsg struct {
	tag   string
	notes []domain.ResolvedNote
	err   error
}

// noteMutatedMsg reports the outcome of an add/edit/reply/remove.
type noteMutatedMsg struct{ err error }

// diffNoteAddress is the address notes hang off for the open diff: the same
// provenance focusedBookmark builds for this surface (working tree vs commit).
// Not available on a two-sided compare (no single file).
func (m Model) diffNoteAddress() (model.FileAddress, bool) {
	v := m.diffLayer()
	if v == nil || v.compare || v.title == "" {
		return model.FileAddress{}, false
	}
	b, ok := m.focusedBookmark()
	if !ok || b.Path == "" {
		return model.FileAddress{}, false
	}
	return b.Address(), true
}

// loadNotesCmd resolves this diff's notes off the UI thread. The rows are the
// SHARED cached rows: they are read, wrapped in a domain.Diff value and never
// mutated.
func (m Model) loadNotesCmd() tea.Cmd {
	v := m.diffLayer()
	addr, ok := m.diffNoteAddress()
	if v == nil || !ok || m.svc == nil {
		return nil
	}
	svc, tag, rows := m.svc, m.diffTag, v.full
	return func() tea.Msg {
		ns, err := svc.NotesFor(context.Background(), addr,
			domain.Diff{Result: textdiff.Result{Rows: rows}})
		return notesLoadedMsg{tag: tag, notes: ns, err: err}
	}
}

// noteAnchorAtCursor is where `c` puts a new note: the cursor row's new-side
// line, or the old-side line on a Del row (§4.1's cursorRow contract). The
// fingerprint is taken from the text the user is looking at.
func (m Model) noteAnchorAtCursor() (model.NoteSide, int, string, bool) {
	v := m.diffLayer()
	if v == nil {
		return "", 0, "", false
	}
	r, ok := v.cursorRow()
	if !ok {
		return "", 0, "", false
	}
	if r.RightNo > 0 {
		return model.NoteSideNew, r.RightNo, model.NoteContextHash([]string{r.Right}), true
	}
	if r.LeftNo > 0 {
		return model.NoteSideOld, r.LeftNo, model.NoteContextHash([]string{r.Left}), true
	}
	return "", 0, "", false
}

// noteNearCursor is the root note E/R/Delete act on: the last one anchored at
// or above the cursor line, else the first note in the view.
func (m Model) noteNearCursor() (domain.ResolvedNote, bool) {
	v := m.diffLayer()
	if v == nil || len(v.notes) == 0 {
		return domain.ResolvedNote{}, false
	}
	best, found := domain.ResolvedNote{}, false
	first, hasFirst := domain.ResolvedNote{}, false
	for _, r := range v.notes {
		if v.hideAgent && r.Note.Source == model.NoteSourceAgent {
			continue
		}
		li, _ := v.noteAnchorLine(r)
		if li < 0 {
			continue
		}
		if !hasFirst {
			first, hasFirst = r, true
		}
		if li <= v.curLine {
			best, found = r, true
		}
	}
	if found {
		return best, true
	}
	return first, hasFirst
}

// nextNoteLine is the next (dir>0) / previous (dir<0) logical line that carries
// a note — including a FOLD entry that hides one, which the caller expands.
func (v *diffView) nextNoteLine(dir int) (int, bool) {
	byLine, foldMark := v.noteRowIndex()
	best, found := -1, false
	consider := func(li int) {
		if dir > 0 && li <= v.curLine {
			return
		}
		if dir < 0 && li >= v.curLine {
			return
		}
		if !found || (dir > 0 && li < best) || (dir < 0 && li > best) {
			best, found = li, true
		}
	}
	for li := range byLine {
		consider(li)
	}
	for li := range foldMark {
		consider(li)
	}
	return best, found
}

// jumpNote moves the cursor to the next/previous annotated line, expanding the
// view when the target hides under a fold (what f does), and reports whether
// it moved.
func (m Model) jumpNote(dir int) (Model, bool) {
	v := m.diffLayer()
	if v == nil {
		return m, false
	}
	body := m.diffBodyRows()
	li, ok := v.nextNoteLine(dir)
	if !ok {
		return m, false
	}
	if li < len(v.lines) && v.lines[li].Fold > 0 {
		// The note hides under this fold: expand to the full file (the f
		// toggle), then re-find the anchor in the REBUILT stream. curLine is a
		// partial-mode index and would be stale after the rebuild — always
		// smaller than the same row's full-mode index — so a backward search
		// would reject the very note we expanded for. Re-anchor first.
		cr, hadRow := v.cursorRow()
		v.partial = false
		v.rebuild()
		m.diffPartial = false
		if hadRow {
			v.reanchorCursor(cr.LeftNo, cr.RightNo)
		}
		li, ok = v.nextNoteLine(dir)
		if !ok {
			return m, false
		}
	}
	v.setCursorLine(li, body)
	return m, true
}

// peekNotedFile / stepNotedFile are the }/{ file step: the next file in the
// open diff's source list that CARRIES notes (NoteCounts, no store read).
// They mirror stepDiffFile's structure and reuse its per-nav steppers.
func (m Model) peekNotedFile(dir int) bool {
	_, ok := m.nextNotedFile(dir)
	return ok
}

func (m Model) stepNotedFile(dir int) (tea.Model, tea.Cmd) {
	steps, ok := m.nextNotedFile(dir)
	if !ok {
		return m, nil
	}
	nm, cmd := m, tea.Cmd(nil)
	for i := 0; i < steps; i++ {
		var tm tea.Model
		tm, cmd = nm.stepDiffFile(dir)
		nm = tm.(Model)
	}
	return nm, cmd
}

// nextNotedFile counts how many plain file steps in direction dir land on a
// file that carries notes (0, false = none). Stepping N times reuses the
// existing per-nav steppers unchanged, so tree/status/staged all work.
func (m Model) nextNotedFile(dir int) (int, bool) {
	paths := m.diffFileSequence(dir) // paths after the current one, in step order
	for i, p := range paths {
		if m.notedFilePath(p) {
			return i + 1, true
		}
	}
	return 0, false
}

// notedFilePath reports whether a path carries notes at the open diff's
// provenance: by path for a working-tree diff, by "<sha>:<path>" for a commit.
func (m Model) notedFilePath(path string) bool {
	if v := m.diffLayer(); v != nil && v.rev != "" {
		return m.noteCounts.ByCommitPath[v.rev+":"+path] > 0
	}
	return m.noteCounts.ByPath[path] > 0
}

// noteDeleteRow is the . menu's "Delete note" (there is no key for it).
func (m Model) noteDeleteRow() (actionRow, bool) {
	if _, ok := m.topLayer().(*diffView); !ok {
		return actionRow{}, false
	}
	r, ok := m.noteNearCursor()
	if !ok {
		return actionRow{}, false
	}
	id := r.Note.ID
	return actionRow{
		id:    "note-delete",
		label: i18n.T("Delete note"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.noteRemoveCmd(id)
		},
	}, true
}

// noteRemoveCmd deletes a note (a root takes its replies) off the UI thread.
func (m Model) noteRemoveCmd(id string) tea.Cmd {
	svc := m.svc
	if svc == nil {
		return nil
	}
	return func() tea.Msg {
		return noteMutatedMsg{err: svc.NoteRemove(context.Background(), id)}
	}
}
