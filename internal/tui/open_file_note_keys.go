package tui

import (
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/notebatch"
)

// The user's side of an open file's temporary notes: } / { step through
// them (then on to the next open file that has any — the diff view's rule),
// d dismisses the note under the cursor, r copies a reference to it.

// anyFileNotes reports whether any open file of this worktree has notes.
func (m Model) anyFileNotes() bool {
	for _, d := range m.openFiles.list(m.currentWorktree) {
		if len(d.shownNotes()) > 0 {
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
	if !ok || !m.anyFileNotes() && len(d.rowNotes()) == 0 {
		return m, nil, false
	}
	switch msg.String() {
	case "}":
		return m.stepFileNote(d, 1)
	case "{":
		return m.stepFileNote(d, -1)
	case "d":
		if n := d.noteAtCursor(); n != nil && d.readOnlyNote(n) {
			m.statusMsg = d.readOnlyText()
			return m, nil, true
		}
		if n := d.noteAtCursor(); n != nil {
			tm, cmd := m.confirmFileNoteDelete(d, *n) // a copy: the removal rebuilds what n points into
			return tm.(Model), cmd, true
		}
	case "enter":
		if n := d.noteAtCursor(); n != nil {
			return m.openFileNote(d.boxNote(*n)), nil, true
		}
	case "r":
		if n := d.noteAtCursor(); n != nil && d.readOnlyNote(n) {
			m.statusMsg = d.readOnlyText()
			return m, nil, true
		}
		if n := d.noteAtCursor(); n != nil {
			return m, m.copyToClipboardCmd(i18n.T("Copied note reference %s", n.ID), agentdocs.NoteReference(*n)), true
		}
	}
	return m, nil, false
}

// stepFileNote moves to the next (dir > 0) or previous note: inside d first,
// by start line from the cursor; past its last one, the next open file with
// notes comes to the front on its first (last) note.
func (m Model) stepFileNote(d *openFile, dir int) (Model, tea.Cmd, bool) {
	line, mine, p := d.p.cur+1, d.shownNotes(), d.p
	if d.diff != nil && d.diff.notice == "" { // the diff's boxes, by row (split_diff.go)
		line, mine, p = d.diff.p.cur+1, d.rowNotes(), d.diff.p
	}
	var hit *agentdocs.Note
	for i := range mine {
		n := &mine[i]
		if dir > 0 && n.Start > line {
			hit = n
			break
		}
		if dir < 0 && n.Start < line {
			hit = n // keep going: the LAST one above the cursor
		}
	}
	if hit != nil {
		_, rows, _, _ := m.activePreview()
		p.cur = hit.Start - 1
		p.ensureCursorVisible(rows)
		// Bring the box in too, as far as that keeps the cursor line on screen.
		for p.sel < p.cur && p.rowsSpan(p.sel, hit.End) > rows {
			p.sel++
		}
		return m, nil, true
	}
	// The other annotated files, in the order they were opened — NOT the
	// list's most-recently-shown order, which every step here would reshuffle
	// (a third file would never come up).
	var others []*openFile
	if sv := m.splitShowing(d); sv != nil { // a split's file pane: the overview's other files, the split stays
		if nm, cmd, ok := m.stepSplitNote(sv, d, dir); ok {
			return nm, cmd, true
		}
		if len(mine) == 0 {
			return m, nil, false
		}
		m.statusMsg = i18n.T("no more notes")
		return m, nil, true
	}
	for _, e := range m.openFiles.list(m.currentWorktree) {
		if e != d && len(e.shownNotes()) > 0 {
			others = append(others, e)
		}
	}
	sort.Slice(others, func(i, j int) bool { return others[i].seq < others[j].seq })
	if len(others) > 0 {
		e := others[0]
		if dir < 0 {
			e = others[len(others)-1]
		}
		for _, o := range others { // the next one after d; else wrap around
			if dir > 0 && o.seq > d.seq {
				e = o
				break
			}
			if dir < 0 && o.seq < d.seq {
				e = o // keep going: the LAST one before d
			}
		}
		ens := e.shownNotes()
		target := ens[0]
		if dir < 0 {
			target = ens[len(ens)-1]
		}
		e.pendingLine = target.Start
		nm, load := m.bringToFront(e)
		if load == nil { // already loaded and not re-read: land the line now
			rows, _ := nm.viewerGeom()
			e.landPendingLine(rows)
		}
		return nm, load, true
	}
	if len(mine) == 0 {
		return m, nil, false // nothing anywhere to step to: not our key
	}
	m.statusMsg = i18n.T("no more notes")
	return m, nil, true
}

// fileNoteRows are the . menu's rows for the focused open file's notes —
// the keys for the mouse. None while it has no notes.
func (m Model) fileNoteRows() []actionRow {
	d, ok := m.focusedDoc()
	if !ok || len(d.shownNotes()) == 0 {
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
	n := d.shownNotes()[0] // the menu has no cursor of its own: offer the first note
	if at := d.noteAtCursor(); at != nil {
		n = *at
	}
	rows = append(rows, actionRow{id: "note-show", key: "enter", label: i18n.T("Show full note"), run: func(m Model) (tea.Model, tea.Cmd) {
		return m.openFileNote(d.boxNote(n)), nil
	}})
	if d.readOnlyNote(&n) { // the review's (or a stack's): nothing to copy a reference to or delete
		return rows
	}
	ref := m.copyRow("copy-note-ref", i18n.T("Copy note reference"), i18n.T("Copied note reference %s", n.ID), agentdocs.NoteReference(n))
	ref.key = "r"
	return append(rows, ref,
		actionRow{id: "note-delete", key: "d", label: i18n.T("Delete note"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.confirmFileNoteDelete(d, n)
		}},
		actionRow{id: "notes-delete-all", label: i18n.T("Delete all notes on this file"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.confirmFileNotesDelete(d)
		}})
}

// noteHint is the bottom bar's hint for the files view's focused preview
// while d carries notes: the note keys, then the exits, then the rest — in
// place of hint, so the exits are never pushed off the line.
func (m Model) noteHint(d *openFile, hint string) string {
	if d == nil || len(d.shownNotes()) == 0 {
		return hint
	}
	return i18n.T("file: [}/{] notes  [d] delete  [r] reference  [esc] background  [X] close  [enter] full note  [alt+↑↓] line  [/] find  [←/tab] back to tree")
}

// remarkLines are a review document's remark lines per file: the new side's
// first lines, ascending (an old-side remark lives in the D diff only).
func remarkLines(files []notebatch.ReviewFile) map[string][]int {
	out := map[string][]int{}
	for _, f := range files {
		p := strings.TrimPrefix(filepath.ToSlash(f.Path), "./")
		for _, n := range f.Notes {
			if n.Side == "old" || n.Range[0] <= 0 {
				continue
			}
			out[p] = append(out[p], n.Range[0])
		}
	}
	for _, ls := range out {
		sort.Ints(ls)
	}
	return out
}

// overviewRemarkLines are the lines of path where overview ov's remarks (a
// stored overview's) or notes (an agent's, on the working tree) sit.
func (m Model) overviewRemarkLines(ov *openFile, path string) []int {
	if ov.ov.remarks != nil {
		return ov.ov.remarks[path]
	}
	if ov.src.kind != srcOverview || m.docs == nil {
		return nil
	}
	ns, _ := m.docs.Notes(domain.CheckoutKey(m.currentWorktree), path)
	var out []int
	for _, n := range ns {
		out = append(out, n.Start)
	}
	sort.Ints(out)
	return out
}

// stepSplitNote is } / { past the last (first) note of the split's file
// pane: the overview's next (previous) file with notes, in the stacked
// view's file order, comes into the pane on its first (last) one — the
// same walk the stack makes through every file's notes. false when no
// file further on has any.
func (m Model) stepSplitNote(sv *splitView, d *openFile, dir int) (Model, tea.Cmd, bool) {
	r, single := sv.right.(*docPane)
	ov := sv.left.doc()
	if !single || r.d != d || ov == nil || ov.ov == nil {
		return m, nil, false
	}
	files, _, _ := m.stackFiles(ov)
	at := -1
	for i, f := range files {
		if f.path == d.path {
			at = i
		}
	}
	if at < 0 && dir < 0 {
		at = len(files)
	}
	for i := at + dir; i >= 0 && i < len(files); i += dir {
		lines := m.overviewRemarkLines(ov, files[i].path)
		if len(lines) == 0 {
			continue
		}
		line := lines[0]
		if dir < 0 {
			line = lines[len(lines)-1]
		}
		nm, cmd := m.docSteer(sv, r, ov, anchor{target: anchorTarget(files[i].path, line)})
		return nm, cmd, true
	}
	return m, nil, false
}
