package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// Temporary notes on an open file: remarks an agent puts on the lines of a
// working-tree document so the user can read them next to the code. They
// live in the agentdocs store (memory only), which a gg web page this TUI
// hosts shares; a document keeps a COPY for drawing. Closing the file (X)
// drops them, and nothing writes them to the notes store. Specs:
// 2026-09-30-open-file-notes-design.md, 2026-10-01-agent-docs-web-design.md.

// addNote puts a remark on lines start..end through the store, as the
// document holds them now. The errors are English protocol prose: they go
// back to the agent that asked.
func (d *openFile) addNote(start, end int, summary, rationale, author string) (*agentdocs.Note, error) {
	switch {
	case d.src.kind != srcWorktree:
		return nil, errors.New("temporary notes go on working-tree files only")
	case !docLoaded(d) || d.p.img != nil:
		return nil, fmt.Errorf("%s has no lines to note", d.path)
	case d.docs == nil:
		return nil, fmt.Errorf("%s is not an open file", d.path) // never: note_add registers first
	}
	n, err := d.docs.AddNote(d.root, d.path, rawOf(d.p.lines), start, end, summary, rationale, author)
	if err != nil {
		return nil, err
	}
	d.backgrounded = true // esc steps aside; only X closes (and drops the notes)
	d.syncNotes()
	return d.noteByID(n.ID), nil
}

// syncNotes re-reads d's notes from the store into its copy. Notes aligned
// to content d does not show are NOT adopted — their lines would be wrong
// here: stale is true, d keeps its old copy, and the caller re-reads d,
// whose load aligns the store and syncs again.
func (d *openFile) syncNotes() (stale bool) {
	var ns []agentdocs.Note
	var fp agentdocs.Fingerprint
	if d.docs != nil && d.src.kind == srcWorktree {
		ns, fp = d.docs.Notes(d.root, d.path)
	}
	if len(ns) > 0 {
		d.backgrounded = true // however the notes came (the page it hosts): esc steps aside, only X closes — stale or not
	}
	if len(ns) > 0 && docLoaded(d) && d.p.img == nil && fp != agentdocs.Print(rawOf(d.p.lines)) {
		return true
	}
	d.notes = ns
	d.syncNoteRows()
	return false
}

// hasNotes reports whether d carries notes: its copy, or the store's while a
// re-read is still to adopt them (syncNotes refused content d does not show).
// Close and the cap decide by this, never by the copy alone.
func (d *openFile) hasNotes() bool {
	if len(d.notes) > 0 {
		return true
	}
	return d.docs != nil && d.src.kind == srcWorktree && d.docs.NoteCount(d.root, d.path) > 0
}

// noteByID is d's copy of the note with that id, or nil.
func (d *openFile) noteByID(id string) *agentdocs.Note {
	for i := range d.notes {
		if d.notes[i].ID == id {
			return &d.notes[i]
		}
	}
	return nil
}

// rawLines is the source text of lines start..end (1-based, inclusive).
func (d *openFile) rawLines(start, end int) []string {
	out := make([]string, 0, end-start+1)
	for i := start; i <= end && i <= len(d.p.lines); i++ {
		out = append(out, d.p.lines[i-1].raw)
	}
	return out
}

// removeNote drops the note with that id from the store; false when d has
// none.
func (d *openFile) removeNote(id string) bool {
	if d.docs == nil || d.noteByID(id) == nil {
		return false
	}
	ok := d.docs.RemoveNote(id)
	d.syncNotes()
	return ok
}

// clearNotes drops every note of d from the store and reports how many
// there were.
func (d *openFile) clearNotes() int {
	if d.docs == nil {
		return 0
	}
	n := d.docs.ClearPath(d.root, d.path)
	d.syncNotes()
	return n
}

// noteAt is the first note covering the 1-based line, or nil.
func (d *openFile) noteAt(line int) *agentdocs.Note {
	for i := range d.notes {
		if n := &d.notes[i]; n.Start <= line && line <= n.End {
			return n
		}
	}
	return nil
}

// findFileNote is the note with that id among the current worktree's open
// files, and the file carrying it; nil, nil when it is gone.
func (m Model) findFileNote(id string) (*openFile, *agentdocs.Note) {
	for _, d := range m.openFiles.list(m.currentWorktree) {
		if n := d.noteByID(id); n != nil {
			return d, n
		}
	}
	return nil, nil
}

// rawOf is the source text of content lines.
func rawOf(lines []contentLine) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.raw
	}
	return out
}

// noteGutterW is the column an annotated file's lines give up on the left
// for the range mark ("│ " on a line a note covers; "┃ " / "╎ " on an
// overview's anchors in the file it opened — gutterMark).
const noteGutterW = 2

// gutterW is the width of d's range-mark gutter: none without notes or
// anchor bands, so an ordinary file is laid out exactly as before.
func (d *openFile) gutterW() int {
	if (len(d.notes) == 0 && d.bands() == nil) || !docLoaded(d) || d.p.img != nil {
		return 0
	}
	return noteGutterW
}

// noteTitle is the box's top-rule text: who, which note, which lines.
func noteTitle(n agentdocs.Note) string {
	switch {
	case n.Outdated && n.Start == n.End:
		return i18n.T("%s · %s · line %d · outdated", n.Author, n.ID, n.Start)
	case n.Outdated:
		return i18n.T("%s · %s · lines %d-%d · outdated", n.Author, n.ID, n.Start, n.End)
	case n.Start == n.End:
		return i18n.T("%s · %s · line %d", n.Author, n.ID, n.Start)
	}
	return i18n.T("%s · %s · lines %d-%d", n.Author, n.ID, n.Start, n.End)
}

// noteBoxMaxRows is the most rows a note box may take in a rowsCap-row
// window: half of it, so the code the note is about stays on screen and the
// pager — whose unit is a file line — can always scroll past the box.
func noteBoxMaxRows(rowsCap int) int { return max(rowsCap/2, 6) }

// boxLines lays the note out as the diff view's note box, innerW columns of
// text inside the frame (<= 0: no wrapping). The body rows are the diff's
// own (noteBodyLines), so a remark reads the same in both places. A box
// taller than maxRows (> 0) keeps its first rows and says how many more
// there are; enter opens the whole note (openFileNote).
func noteBoxLines(n agentdocs.Note, innerW, maxRows int) []noteLine {
	frame := func(kind noteRowKind, text string) noteLine {
		return noteLine{id: n.ID, rootID: n.ID, kind: kind, side: model.NoteSideNew, text: text, stale: n.Outdated, agent: true}
	}
	r := domain.ResolvedNote{Note: model.Note{
		ID: n.ID, Source: model.NoteSourceAgent, Author: n.Author, Side: model.NoteSideNew,
		Summary: n.Summary, Rationale: n.Rationale,
	}}
	body := noteBodyLines(r, n.ID, 0, innerW, n.Outdated)
	if keep := max(maxRows-5, 1); maxRows > 0 && len(body)+4 > maxRows && len(body) > keep {
		more := frame(noteRowText, i18n.T("… %d more lines — [enter] full note", len(body)-keep))
		body = append(body[:keep:keep], more)
	}
	rows := []noteLine{frame(noteRowTop, noteTitle(n)), frame(noteRowBlank, "")}
	rows = append(rows, body...)
	return append(rows, frame(noteRowBlank, ""), frame(noteRowBottom, ""))
}

// openFileNote shows one note in full in a window of its own — a box cut to
// the file window has more to say than fits under its line. esc returns.
func (m Model) openFileNote(n agentdocs.Note) Model {
	lines := []contentLine{{text: sanitizeLine(n.Summary)}}
	if n.Rationale != "" {
		lines = append(lines, contentLine{})
		for _, l := range strings.Split(n.Rationale, "\n") {
			lines = append(lines, contentLine{text: sanitizeLine(l)})
		}
	}
	cp := newContentPopup(noteTitle(n), lines)
	cp.fitContent = true
	cp.prose = true
	return m.pushLayer(cp)
}

// noteRowsUnder is the rows the boxes under lines [from, to) take (0-based
// line indexes): a box hangs under the line its note ENDS on.
func (d *openFile) noteRowsUnder(from, to int) int {
	rows := 0
	for _, n := range d.notes {
		if i := n.End - 1; i >= from && i < to {
			rows += len(noteBoxLines(n, d.noteW-noteBoxFrame, d.noteH))
		}
	}
	return rows
}

// syncNoteRows installs the pager's row-count hook while d has notes and
// takes it away when the last one goes, so a file without notes is back on
// the plain one-row-per-line path.
func (d *openFile) syncNoteRows() {
	if len(d.notes) == 0 {
		d.p.extraRows = nil
		return
	}
	d.p.extraRows = d.noteRowsUnder
}

// previewDoc is the open document p belongs to — a viewer on the stack or
// the files view's preview — or nil (the help window, F's live preview).
func (m Model) previewDoc(p *contentPopup) *openFile {
	if d := m.filesPreview; d != nil && d.p == p {
		return d
	}
	if m.layers != nil {
		for _, l := range m.layers.entries {
			if fv, ok := l.(*fileViewer); ok && fv.p == p {
				return fv.openFile
			}
		}
	}
	return nil
}

// fileNoteRow is one box row as a preview window row: blank text the window
// pads to its width, painted by a decorator — so the box ignores the
// horizontal scroll and is never reflowed by wrap mode. A box narrower than
// its own frame can carry is a blank row.
func fileNoteRow(nl noteLine, w, gut int) winRow {
	if w-gut < 8 {
		return winRow{noWrap: true}
	}
	return winRow{noWrap: true, decorate: func(string, int, int) string {
		return strings.Repeat(" ", gut) + noteBoxCell(nl, w-gut)
	}}
}
