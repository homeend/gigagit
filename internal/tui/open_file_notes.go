package tui

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"
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
			return true
		}
	}
	return false
}

// clearNotes drops every note and reports how many there were.
func (d *openFile) clearNotes() int {
	n := len(d.notes)
	d.notes = nil
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
