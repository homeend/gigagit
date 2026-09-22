package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

// Review notes inside a STACK (design §11 item 1). Each file carries the
// single-file view its own loader built, so its address, preview scope and
// resolved notes are that loader's stamps — the note keys simply read the file
// under the cursor (curNoteView). What needs new code is the WALK: } / { cross
// the whole stack, and the file they land in may be folded, or not fetched at
// all, in which case the landing is parked until its lines exist.

// stackNoteStep is the }/{ step in a stack: the next file in dir that CARRIES
// notes, unfolded, fetched if need be, with the landing parked. Unlike the
// single-file view there is no file to step TO — every file of the list is
// already in this stream — so nothing is armed and nothing is re-opened.
func (m Model) stackNoteStep(v *diffView, dir int) (Model, tea.Cmd, bool) {
	body := m.diffBodyRows()
	for i := v.curFile() + dir; i >= 0 && i < len(v.stk.files); i += dir {
		if !m.notedStackFile(v, i) {
			continue
		}
		f := &v.stk.files[i]
		f.collapsed = false
		v.rebuild()
		v.setCursorLine(f.hdr, body)
		v.syncStackTitle()
		v.stk.land = &stackLanding{file: i, dir: dir}
		if len(v.notesOf(i)) > 0 {
			// Its notes are already resolved: land now, no round trip.
			nm, _ := m.drainStackLanding(i, body)
			return nm, nil, true
		}
		nm, cmd := m.pumpStack() // fetch it; its notes follow the diff
		return nm, cmd, true
	}
	return m, nil, false
}

// notedStackFile reports whether file i carries review notes — from the notes
// already resolved for it, else from the badge COUNTS (no store read, and they
// answer for a file whose diff has never been fetched). It mirrors
// notedFilePath, keyed on the stack's own provenance.
func (m Model) notedStackFile(v *diffView, i int) bool {
	if len(v.notesOf(i)) > 0 {
		return true
	}
	f := v.stk.files[i]
	if f.conflict {
		return false // header + resolver line: no lines to anchor on
	}
	if m.previewNoteScope() != nil {
		return m.filesPreviewCounts[f.path] > 0 && !m.previewPathGoneAtTip(f.path)
	}
	if v.rev != "" {
		return m.noteCounts.ByCommitPath[v.rev+":"+f.path] > 0
	}
	return m.noteCounts.ByPath[f.path] > 0
}

// drainStackLanding consumes a landing parked on file idx, once that file's
// lines (and, for a note landing, its notes) are in the stream. It reports
// whether it moved the cursor.
func (m Model) drainStackLanding(idx, body int) (Model, bool) {
	v := m.diffLayer()
	if v == nil || v.stk == nil || v.stk.land == nil || v.stk.land.file != idx {
		return m, false
	}
	land := *v.stk.land
	lo, hi := v.fileLineRange(idx)
	find := func() (int, bool) {
		if land.dir != 0 {
			return v.noteLineIn(lo, hi, land.dir)
		}
		return v.lineAnchorIn(lo, hi, land.no, land.side == model.NoteSideOld)
	}
	li, ok := find()
	if !ok {
		if land.dir != 0 {
			return m, false // its notes have not arrived yet: keep the landing
		}
		v.stk.land = nil
		return m, false
	}
	v.stk.land = nil
	m, li, ok = m.expandFoldFor(v, li, find)
	if !ok {
		return m, false
	}
	v.setCursorLine(li, body)
	v.revealCursorNotes(body)
	v.noteVisited = true
	v.syncStackTitle()
	return m, true
}

// noteLineIn is the FIRST (dir>0) / LAST (dir<0) annotated logical line inside
// [lo, hi] — one file's edge note, the landing a }/{ step owes.
func (v *diffView) noteLineIn(lo, hi, dir int) (int, bool) {
	byLine, foldMark := v.noteRowIndex()
	best, found := -1, false
	consider := func(li int) {
		if li < lo || li > hi {
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

// stackFileIdx finds the stack's file with this path.
func (v *diffView) stackFileIdx(path string) (int, bool) {
	if v.stk == nil {
		return 0, false
	}
	for i := range v.stk.files {
		if v.stk.files[i].path == path {
			return i, true
		}
	}
	return 0, false
}

// lineLanding is a cursor placement owed to a diff that is still loading: the
// line the reader was on when they left a stack (S), named by side and number
// because the view they are going back to has not been built yet.
type lineLanding struct {
	tag  string // the diffTag the re-open set; a mismatch drops the landing
	side model.NoteSide
	no   int
}

// cursorLineLanding names the line under the cursor, or nil when the cursor
// has no numbered row (a header, a placeholder).
func (v *diffView) cursorLineLanding() *lineLanding {
	r, ok := v.cursorRow()
	if !ok {
		return nil
	}
	side, no := model.NoteSideNew, r.RightNo
	if v.onOld || no == 0 {
		side, no = model.NoteSideOld, r.LeftNo
	}
	if no == 0 {
		return nil
	}
	return &lineLanding{side: side, no: no}
}

// withLineLanding parks land on the diff the caller just re-opened, tagged
// with the tag that open set.
func (m Model) withLineLanding(land *lineLanding, tm tea.Model, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	mm, ok := tm.(Model)
	if !ok || land == nil {
		return tm, cmd
	}
	land.tag = mm.diffTag
	mm.diffLand = land
	return mm, cmd
}

// drainLineLanding puts the cursor on the parked line once the diff carrying
// it has arrived. It expands a fold hiding the line, exactly as a note jump
// and a steer landing do.
func (m Model) drainLineLanding(v *diffView, tag string) Model {
	if m.diffLand == nil || m.diffLand.tag != tag {
		return m
	}
	land := *m.diffLand
	m.diffLand = nil
	old := land.side == model.NoteSideOld
	find := func() (int, bool) { return v.lineAnchor(land.no, old) }
	li, ok := find()
	if !ok || li < 0 {
		return m
	}
	body := m.diffBodyRows()
	if m, li, ok = m.expandFoldFor(v, li, find); !ok {
		return m
	}
	v.setCursorLine(li, body)
	v.alignCursor(alignCenter, body)
	return m
}
