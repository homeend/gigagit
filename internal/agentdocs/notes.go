package agentdocs

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/homeend/gigagit/internal/textdiff"
)

// The limits of the open-file notes spec (2026-09-30-open-file-notes).
const (
	MaxNotesPerFile  = 50
	MaxNoteSummary   = 500
	MaxNoteRationale = 4000
)

// Note is one remark, as a copy. Start/End are 1-based lines of the content
// the file's notes were last aligned to (Notes reports its fingerprint).
type Note struct {
	ID        string // "t<n>"
	Seq       int64
	Root      string
	Path      string
	Start     int
	End       int
	Summary   string
	Rationale string
	Author    string
	Outdated  bool // its lines are gone from the file
}

type note struct {
	Note
	anchor []string // the lines' text when placed or last re-anchored
}

// fileNotes is one file's notes and the content they sit on.
type fileNotes struct {
	lines []string
	print Fingerprint
	notes []*note
}

// Fingerprint names one content: sha256 over its canonical lines.
type Fingerprint [32]byte

// Print fingerprints canonical lines (Lines' output, or the TUI's raw lines,
// which are split by the same rule).
func Print(lines []string) Fingerprint {
	h := sha256.New()
	for _, l := range lines {
		h.Write([]byte(l))
		h.Write([]byte{'\n'})
	}
	var f Fingerprint
	copy(f[:], h.Sum(nil))
	return f
}

// Lines splits a file the TUI's way (fileContentLinesTok): CRLF and a lone CR
// are line breaks, trailing breaks are dropped. No lines = nothing to note.
func Lines(data []byte) []string {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// canon is Lines over lines a caller split itself.
func canon(lines []string) []string { return Lines([]byte(strings.Join(lines, "\n"))) }

// AddNote puts a remark on lines start..end of path as lines holds it NOW,
// aligning the path's notes to lines first. The errors are English protocol
// prose for the agent that asked.
func (s *Store) AddNote(root, path string, lines []string, start, end int, summary, rationale, author string) (Note, error) {
	lines = canon(lines)
	summary = strings.TrimSpace(summary)
	s.mu.Lock()
	k := fileKey{root, path}
	f := s.files[k]
	aligned := f != nil && len(lines) > 0 && f.align(lines)
	count := 0
	if f != nil {
		count = len(f.notes)
	}
	var err error
	switch {
	case len(lines) == 0:
		err = fmt.Errorf("%s has no lines to note", path)
	case start < 1:
		err = errors.New("a line number is 1-based")
	case end < start:
		err = errors.New("the range ends before it starts")
	case end > len(lines):
		err = fmt.Errorf("line %d is past the end of %s (%d lines)", end, path, len(lines))
	case summary == "":
		err = errors.New("a note needs a summary")
	case utf8.RuneCountInString(summary) > MaxNoteSummary:
		err = fmt.Errorf("the summary is longer than %d characters", MaxNoteSummary)
	case utf8.RuneCountInString(rationale) > MaxNoteRationale:
		err = fmt.Errorf("the rationale is longer than %d characters", MaxNoteRationale)
	case count >= MaxNotesPerFile:
		err = fmt.Errorf("%s already carries %d notes", path, MaxNotesPerFile)
	}
	if err != nil {
		s.mu.Unlock()
		if aligned {
			s.b.signal()
		}
		return Note{}, err
	}
	if f == nil {
		f = &fileNotes{lines: lines, print: Print(lines)}
		s.files[k] = f
	}
	if author == "" {
		author = "agent"
	}
	s.noteSeq++
	n := &note{
		Note: Note{ID: "t" + strconv.FormatInt(s.noteSeq, 10), Seq: s.noteSeq, Root: root, Path: path,
			Start: start, End: end, Summary: summary, Rationale: rationale, Author: author},
		anchor: append([]string(nil), lines[start-1:end]...),
	}
	f.notes = append(f.notes, n)
	f.sort()
	out := n.Note
	s.mu.Unlock()
	s.b.signal()
	return out, nil
}

// Align re-anchors path's notes to lines, the content a side just read from
// disk. The same content as last time is a no-op, and no lines (a
// placeholder: deleted, empty, too large) never align. It reports whether
// anything changed.
func (s *Store) Align(root, path string, lines []string) bool {
	lines = canon(lines)
	if len(lines) == 0 {
		return false
	}
	s.mu.Lock()
	f := s.files[fileKey{root, path}]
	changed := f != nil && f.align(lines)
	s.mu.Unlock()
	if changed {
		s.b.signal()
	}
	return changed
}

func (f *fileNotes) align(cur []string) bool {
	p := Print(cur)
	if p == f.print {
		return false
	}
	reanchor(f.notes, f.lines, cur)
	f.lines, f.print = cur, p
	f.sort()
	return true
}

// Notes is a copy of path's notes in reading order and the fingerprint of
// the content they sit on (zero when there are none).
func (s *Store) Notes(root, path string) ([]Note, Fingerprint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.files[fileKey{root, path}]
	if f == nil {
		return nil, Fingerprint{}
	}
	out := make([]Note, len(f.notes))
	for i, n := range f.notes {
		out[i] = n.Note
	}
	return out, f.print
}

// NoteCount is how many notes path carries.
func (s *Store) NoteCount(root, path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f := s.files[fileKey{root, path}]; f != nil {
		return len(f.notes)
	}
	return 0
}

// NotedPaths is root's paths that carry notes, by their oldest note.
func (s *Store) NotedPaths(root string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	type byAge struct {
		path string
		seq  int64
	}
	var ps []byAge
	for k, f := range s.files {
		if k.root != root || len(f.notes) == 0 {
			continue
		}
		first := f.notes[0].Seq
		for _, n := range f.notes {
			first = min(first, n.Seq)
		}
		ps = append(ps, byAge{k.path, first})
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].seq < ps[j].seq })
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.path
	}
	return out
}

// FindNote is the note with that id, in any root.
func (s *Store) FindNote(id string) (Note, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, n := s.findLocked(id); n != nil {
		return n.Note, true
	}
	return Note{}, false
}

// NoteText is the lines the note sits on now (nil when it is gone).
func (s *Store) NoteText(id string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, n := s.findLocked(id)
	if n == nil {
		return nil
	}
	var out []string
	for i := n.Start; i <= n.End && i <= len(f.lines); i++ {
		out = append(out, f.lines[i-1])
	}
	return out
}

func (s *Store) findLocked(id string) (*fileNotes, *note) {
	for _, f := range s.files {
		for _, n := range f.notes {
			if n.ID == id {
				return f, n
			}
		}
	}
	return nil, nil
}

// RemoveNote drops one note; false when it is gone already.
func (s *Store) RemoveNote(id string) bool {
	s.mu.Lock()
	removed := false
	for k, f := range s.files {
		for i, n := range f.notes {
			if n.ID == id {
				f.notes = append(f.notes[:i], f.notes[i+1:]...)
				if len(f.notes) == 0 {
					delete(s.files, k) // the kept content goes with the last note
				}
				removed = true
				break
			}
		}
		if removed {
			break
		}
	}
	s.mu.Unlock()
	if removed {
		s.b.signal()
	}
	return removed
}

// ClearPath drops every note of path (X on the file) and says how many.
func (s *Store) ClearPath(root, path string) int {
	s.mu.Lock()
	k := fileKey{root, path}
	n := 0
	if f := s.files[k]; f != nil {
		n = len(f.notes)
		delete(s.files, k)
	}
	s.mu.Unlock()
	if n > 0 {
		s.b.signal()
	}
	return n
}

// sort keeps notes in reading order: by start line, then oldest first.
func (f *fileNotes) sort() {
	sort.SliceStable(f.notes, func(i, j int) bool {
		a, b := f.notes[i], f.notes[j]
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		return a.Seq < b.Seq
	})
}

// reanchor moves every note to where its lines are in cur, the content that
// just landed; old is the content they were aligned to.
//
// A live note follows the line alignment: it moves when every one of its
// lines survived unchanged and still sits together. Otherwise it goes
// outdated and keeps its numbers, clamped to the file. An outdated note
// comes back only when its remembered text is at its old place again, or
// sits at exactly ONE place in the file: a lone "}" must never adopt some
// other brace.
func reanchor(notes []*note, old, cur []string) {
	to := sameLineMap(old, cur)
	for _, n := range notes {
		if !n.Outdated {
			if s, ok := mapRange(to, n.Start, n.End); ok {
				n.Start, n.End = s, s+(n.End-n.Start)
				continue
			}
		} else if s := relocate(cur, n.anchor, n.Start); s > 0 {
			n.Start, n.End, n.Outdated = s, s+len(n.anchor)-1, false
			continue
		}
		n.Outdated = true
		if n.End > len(cur) {
			n.End = len(cur)
		}
		if n.Start > n.End {
			n.Start = n.End
		}
		if n.Start < 1 {
			n.Start, n.End = 1, 1
		}
	}
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
