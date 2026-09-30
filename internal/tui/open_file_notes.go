package tui

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

// Temporary notes on an open file: remarks an agent puts on the lines of a
// working-tree document so the user can read them next to the code. They
// are memory only — closing the file (X) drops them, and nothing writes
// them to the notes store. Spec: 2026-09-30-open-file-notes-design.md.

const (
	maxFileNotes         = 50
	maxFileNoteSummary   = 500
	maxFileNoteRationale = 4000
)

// fileNote is one remark. start/end are 1-based lines of the content shown
// NOW; anchor is those lines' text as it was when the note was placed or
// last re-anchored, which is how a reload finds the lines again.
type fileNote struct {
	id         string // "t<seq>" — unique in the process, across all files
	seq        int64
	start, end int
	anchor     []string
	summary    string
	rationale  string
	author     string
	outdated   bool // its lines are gone from the file
}

// fileNoteSeq numbers notes. Process-global, so an id names one note
// without naming its file.
var fileNoteSeq atomic.Int64

// addNote puts a remark on lines start..end. The errors are English
// protocol prose: they go back to the agent that asked.
func (d *openFile) addNote(start, end int, summary, rationale, author string) (*fileNote, error) {
	summary = strings.TrimSpace(summary)
	switch {
	case d.src.kind != srcWorktree:
		return nil, errors.New("temporary notes go on working-tree files only")
	case !docLoaded(d) || d.p.img != nil:
		return nil, fmt.Errorf("%s has no lines to note", d.path)
	case start < 1:
		return nil, errors.New("a line number is 1-based")
	case end < start:
		return nil, errors.New("the range ends before it starts")
	case end > len(d.p.lines):
		return nil, fmt.Errorf("line %d is past the end of %s (%d lines)", end, d.path, len(d.p.lines))
	case summary == "":
		return nil, errors.New("a note needs a summary")
	case utf8.RuneCountInString(summary) > maxFileNoteSummary:
		return nil, fmt.Errorf("the summary is longer than %d characters", maxFileNoteSummary)
	case utf8.RuneCountInString(rationale) > maxFileNoteRationale:
		return nil, fmt.Errorf("the rationale is longer than %d characters", maxFileNoteRationale)
	case len(d.notes) >= maxFileNotes:
		return nil, fmt.Errorf("%s already carries %d notes", d.path, maxFileNotes)
	}
	if author == "" {
		author = "agent"
	}
	seq := fileNoteSeq.Add(1)
	n := &fileNote{
		id: "t" + strconv.FormatInt(seq, 10), seq: seq,
		start: start, end: end, anchor: d.rawLines(start, end),
		summary: summary, rationale: rationale, author: author,
	}
	d.notes = append(d.notes, n)
	d.sortNotes()
	d.backgrounded = true // esc steps aside; only X closes (and drops the notes)
	d.syncNoteRows()
	return n, nil
}

// rawLines is the source text of lines start..end (1-based, inclusive).
func (d *openFile) rawLines(start, end int) []string {
	out := make([]string, 0, end-start+1)
	for i := start; i <= end && i <= len(d.p.lines); i++ {
		out = append(out, d.p.lines[i-1].raw)
	}
	return out
}

// sortNotes keeps notes in reading order: by start line, then oldest first.
func (d *openFile) sortNotes() {
	sort.SliceStable(d.notes, func(i, j int) bool {
		a, b := d.notes[i], d.notes[j]
		if a.start != b.start {
			return a.start < b.start
		}
		return a.seq < b.seq
	})
}

// removeNote drops the note with that id; false when the file has none.
func (d *openFile) removeNote(id string) bool {
	for i, n := range d.notes {
		if n.id == id {
			d.notes = append(d.notes[:i], d.notes[i+1:]...)
			d.syncNoteRows()
			return true
		}
	}
	return false
}

// clearNotes drops every note and reports how many there were.
func (d *openFile) clearNotes() int {
	n := len(d.notes)
	d.notes = nil
	d.syncNoteRows()
	return n
}

// noteAt is the first note covering the 1-based line, or nil.
func (d *openFile) noteAt(line int) *fileNote {
	for _, n := range d.notes {
		if n.start <= line && line <= n.end {
			return n
		}
	}
	return nil
}

// findFileNote is the note with that id among the current worktree's open
// files, and the file carrying it; nil, nil when it is gone.
func (m Model) findFileNote(id string) (*openFile, *fileNote) {
	for _, d := range m.openFiles.list(m.currentWorktree) {
		for _, n := range d.notes {
			if n.id == id {
				return d, n
			}
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

// reanchorNotes moves every note to where its lines are in cur, the content
// that just landed. old is the content shown before (nil when that was a
// placeholder — a file deleted on disk, a failed read).
//
// A live note follows the line alignment: it moves when every one of its
// lines survived unchanged and still sits together. Otherwise it goes
// outdated and keeps its numbers, clamped to the file. An outdated note —
// and any note when there is nothing to align against — comes back only
// when its remembered text is at its old place again, or sits at exactly
// ONE place in the file: a lone "}" must never adopt some other brace.
func (d *openFile) reanchorNotes(old, cur []string) {
	var to []int
	if old != nil {
		to = sameLineMap(old, cur)
	}
	for _, n := range d.notes {
		if !n.outdated && to != nil {
			if s, ok := mapRange(to, n.start, n.end); ok {
				n.start, n.end = s, s+(n.end-n.start)
				continue
			}
		} else if s := relocate(cur, n.anchor, n.start); s > 0 {
			n.start, n.end, n.outdated = s, s+len(n.anchor)-1, false
			continue
		}
		n.outdated = true
		if n.end > len(cur) {
			n.end = len(cur)
		}
		if n.start > n.end {
			n.start = n.end
		}
		if n.start < 1 {
			n.start, n.end = 1, 1
		}
	}
	d.sortNotes()
}

// sameLineMap maps each old line (1-based index) to the new line it survived
// as, unchanged; 0 = edited or gone.
func sameLineMap(old, cur []string) []int {
	res := textdiff.Compare([]byte(strings.Join(old, "\n")+"\n"), []byte(strings.Join(cur, "\n")+"\n"), textdiff.Options{})
	to := make([]int, len(old)+1)
	for _, r := range res.Rows {
		if r.Kind == textdiff.Same && r.LeftNo >= 1 && r.LeftNo <= len(old) {
			to[r.LeftNo] = r.RightNo
		}
	}
	return to
}

// mapRange maps old lines start..end through to: ok only when every line
// survived and they are still consecutive.
func mapRange(to []int, start, end int) (int, bool) {
	if start < 1 || end >= len(to) || to[start] == 0 {
		return 0, false
	}
	for i := start; i <= end; i++ {
		if to[i] != to[start]+(i-start) {
			return 0, false
		}
	}
	return to[start], true
}

// relocate finds anchor in cur: at start when it is there, else at its one
// and only occurrence. 0 = not found, or found more than once.
func relocate(cur, anchor []string, start int) int {
	if len(anchor) == 0 {
		return 0
	}
	at := func(s int) bool { // s is 1-based
		if s < 1 || s+len(anchor)-1 > len(cur) {
			return false
		}
		for i, a := range anchor {
			if cur[s-1+i] != a {
				return false
			}
		}
		return true
	}
	if at(start) {
		return start
	}
	found := 0
	for s := 1; s+len(anchor)-1 <= len(cur); s++ {
		if at(s) {
			if found != 0 {
				return 0
			}
			found = s
		}
	}
	return found
}

// noteGutterW is the column an annotated file's lines give up on the left
// for the range mark ("│ " on a line a note covers).
const noteGutterW = 2

// gutterW is the width of d's range-mark gutter: none without notes, so an
// ordinary file is laid out exactly as before.
func (d *openFile) gutterW() int {
	if len(d.notes) == 0 || !docLoaded(d) || d.p.img != nil {
		return 0
	}
	return noteGutterW
}

// title is the box's top-rule text: who, which note, which lines.
func (n *fileNote) title() string {
	switch {
	case n.outdated && n.start == n.end:
		return i18n.T("%s · %s · line %d · outdated", n.author, n.id, n.start)
	case n.outdated:
		return i18n.T("%s · %s · lines %d-%d · outdated", n.author, n.id, n.start, n.end)
	case n.start == n.end:
		return i18n.T("%s · %s · line %d", n.author, n.id, n.start)
	}
	return i18n.T("%s · %s · lines %d-%d", n.author, n.id, n.start, n.end)
}

// boxLines lays the note out as the diff view's note box, innerW columns of
// text inside the frame (<= 0: no wrapping). The body rows are the diff's
// own (noteBodyLines), so a remark reads the same in both places.
func (n *fileNote) boxLines(innerW int) []noteLine {
	frame := func(kind noteRowKind, text string) noteLine {
		return noteLine{id: n.id, rootID: n.id, kind: kind, side: model.NoteSideNew, text: text, stale: n.outdated, agent: true}
	}
	r := domain.ResolvedNote{Note: model.Note{
		ID: n.id, Source: model.NoteSourceAgent, Author: n.author, Side: model.NoteSideNew,
		Summary: n.summary, Rationale: n.rationale,
	}}
	rows := []noteLine{frame(noteRowTop, n.title()), frame(noteRowBlank, "")}
	rows = append(rows, noteBodyLines(r, n.id, 0, innerW, n.outdated)...)
	return append(rows, frame(noteRowBlank, ""), frame(noteRowBottom, ""))
}

// noteRowsUnder is the rows the boxes under lines [from, to) take (0-based
// line indexes): a box hangs under the line its note ENDS on.
func (d *openFile) noteRowsUnder(from, to int) int {
	rows := 0
	for _, n := range d.notes {
		if i := n.end - 1; i >= from && i < to {
			rows += len(n.boxLines(d.noteW - noteBoxFrame))
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
