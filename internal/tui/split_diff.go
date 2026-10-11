package tui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// An open file's REVIEW DIFF (spec 2026-10-11-overview-split-diff §4–§6):
// the overview split's right column — or the narrow viewer — shows, in
// place of the file, the change the review covered for it as one column of
// rows (split_diff_rows.go).
//
// d.p is ALWAYS the file's popup: its lines are the file's, its cursor a
// file line, so every reader of a line (links, notes, the overview's mark,
// a reload) keeps reading the file. d.diff holds a SECOND popup over the
// rows; while it is set, the frames draw it and the keys move it (shownP).
// After every key or click the frame handles, mirrorDiffCursor puts d.p.cur
// on the cursor row's new line (a deleted row: the next new line below);
// whatever moves d.p.cur while the diff shows (a steer, n / p) calls
// followIntoDiff — the cursor's two homes never disagree.

// docDiff is the diff shown in place of d's file: the rows as lines (p),
// the rows they are (rows; folded when partial), every row (all), the
// read's answer (res) or what the column says instead (notice), the folds
// enter opened (by their first row's at), the review's remarks on both
// sides, and the number column's width.
type docDiff struct {
	p       *contentPopup
	rows    []diffRow
	all     []diffRow
	res     domain.ReviewFileDiff
	notice  string
	agent   bool
	partial bool
	open    map[int]bool
	remarks []rowNote
	numW    int
}

// rowNote is a remark of the review on one side of the diff.
type rowNote struct {
	n   agentdocs.Note
	old bool
}

// shownP is the popup a frame draws and the keys move: the diff's rows
// while they show, the file otherwise.
func (d *openFile) shownP() *contentPopup {
	if d.diff != nil {
		return d.diff.p
	}
	return d.p
}

// showDiff puts res on screen in place of the file (or, with err, nothing
// changed or nothing drawable, its notice), the cursor on the row of the
// line it is on now, at the same distance from the top when that fits.
// A diff already showing is replaced the same way (a reload).
func (d *openFile) showDiff(res domain.ReviewFileDiff, err error, agent, partial bool, rows int) {
	line, off, mode, open := d.p.cur+1, d.p.cur-d.p.sel, d.p.mode, map[int]bool{}
	if old := d.diff; old != nil {
		if old.notice == "" {
			line, off = rowNewLine(old.rows, old.p.cur), old.p.cur-old.p.sel
		}
		mode, open = old.p.mode, old.open
		if old.p.cs.on {
			old.p.previewCharLeave()
		}
	}
	if d.p.cs.on {
		d.p.previewCharLeave()
	}
	dd := &docDiff{res: res, agent: agent, partial: partial, notice: diffNoticeText(res, err, agent), open: open}
	dd.p = &contentPopup{title: d.path, charWrap: true, mode: mode}
	d.diff = dd
	if dd.notice != "" {
		dd.p.lines = []contentLine{{text: dd.notice}}
		dd.p.noCursor = true
		d.syncNoteRows()
		return
	}
	dd.all = unifiedDiffRows(res.Diff)
	for _, n := range res.Notes {
		dd.remarks = append(dd.remarks, rowNote{n: remarkBoxes([]domain.ResolvedNote{n})[0], old: n.Note.Side == model.NoteSideOld})
	}
	d.layDiff()
	r := diffRowNear(dd.rows, line)
	dd.p.cur = r
	dd.p.sel = dd.p.clampTop(r-off, rows)
	if r < dd.p.sel || r > dd.p.lastVisible(rows) {
		dd.p.sel = dd.p.clampTop(r-rows/2, rows)
	}
}

// diffRowNear is the row of new line line, else of the first line after it
// that has one, else the last row (a deleted file: the top).
func diffRowNear(rows []diffRow, line int) int {
	best := -1
	for i, r := range rows {
		if r.kind == diffFold || r.nu == 0 {
			continue
		}
		if r.nu >= line {
			return i
		}
		best = i
	}
	return max(best, 0)
}

// layDiff lays the rows out as the popup's lines: all, or f's folds of it
// (a band's or a remark's line never folds).
func (d *openFile) layDiff() {
	dd := d.diff
	dd.rows = dd.all
	if dd.partial {
		dd.rows = foldDiffRows(dd.all, d.diffKeep(), dd.open)
	}
	lines := make([]contentLine, len(dd.rows))
	top := 1
	for i, r := range dd.rows {
		if r.kind == diffFold {
			lines[i] = contentLine{text: i18n.T("⋯ %d unchanged lines", r.hidden)}
			continue
		}
		lines[i] = contentLine{text: r.text, raw: r.raw, src: true, cls: r.cls}
		top = max(top, r.old, r.nu)
	}
	dd.p.lines = lines
	dd.numW = len(strconv.Itoa(top)) + 3 // "12 − "
	d.syncNoteRows()
}

// diffKeep is f's keep rule over dd.all: a line inside one of the
// overview's bands, or under a note or a remark (on its side).
func (d *openFile) diffKeep() func(int) bool {
	dd := d.diff
	bs := d.bands()
	return func(i int) bool {
		r := dd.all[i]
		for _, b := range bs {
			if r.nu > 0 && b.start <= r.nu && r.nu <= b.end {
				return true
			}
		}
		for _, n := range d.notes {
			if r.nu > 0 && n.Start <= r.nu && r.nu <= n.End {
				return true
			}
		}
		for _, rn := range dd.remarks {
			no := r.nu
			if rn.old {
				no = r.old
			}
			if no > 0 && rn.n.Start <= no && no <= rn.n.End {
				return true
			}
		}
		return false
	}
}

// refoldDiff lays the rows out again (f, a fold opened), the cursor kept on
// its new line.
func (d *openFile) refoldDiff(rows int) {
	dd := d.diff
	if dd == nil || dd.notice != "" {
		return
	}
	line, off := rowNewLine(dd.rows, dd.p.cur), dd.p.cur-dd.p.sel
	if dd.p.cs.on {
		dd.p.previewCharLeave()
	}
	dd.p.lsel.clear()
	d.layDiff()
	r := diffRowNear(dd.rows, line)
	dd.p.cur = r
	dd.p.sel = dd.p.clampTop(r-off, rows)
	dd.p.ensureCursorVisible(rows)
}

// dropDiff puts the file back, its cursor on the diff cursor's new line on
// the same screen row.
func (d *openFile) dropDiff(rows int) {
	dd := d.diff
	if dd == nil {
		return
	}
	if dd.p.cs.on {
		dd.p.previewCharLeave()
	}
	off := dd.p.cur - dd.p.sel
	d.mirrorDiffCursor()
	d.diff = nil
	d.p.mode = dd.p.mode
	if dd.notice == "" {
		d.p.sel = d.p.clampTop(d.p.cur-off, rows)
	}
	d.p.ensureCursorVisible(rows)
	d.syncNoteRows()
}

// mirrorDiffCursor keeps d.p.cur on the diff cursor's new line, so every
// reader of d.p.cur reads a file line (gg web's view.cur rule), and keeps
// it in the file's window so a flip back lands where it was.
func (d *openFile) mirrorDiffCursor() {
	dd := d.diff
	if dd == nil || dd.notice != "" || !docLoaded(d) {
		return
	}
	if line := rowNewLine(dd.rows, dd.p.cur); line > 0 && line <= len(d.p.lines) {
		d.p.cur = line - 1
	}
}

// followIntoDiff puts the diff cursor on d.p.cur's row, centred — after
// something moved the file cursor (a steer, n / p).
func (d *openFile) followIntoDiff(rows int) {
	dd := d.diff
	if dd == nil || dd.notice != "" {
		return
	}
	r := diffRowNear(dd.rows, d.p.cur+1)
	dd.p.cur = r
	dd.p.sel = dd.p.clampTop(r-rows/2, rows)
	dd.p.ensureCursorVisible(rows)
}

// rowBands are d's bands (bands, on new lines) on the diff's rows: a band
// covers the rows from its first line's to its last line's, the deleted
// rows between them included.
func (d *openFile) rowBands() []anchorBand {
	dd := d.diff
	if dd == nil || dd.notice != "" {
		return nil
	}
	var out []anchorBand
	for _, b := range d.bands() {
		s, e := rowOfNew(dd.rows, b.start), rowOfNew(dd.rows, b.end)
		if s < 0 || e < 0 {
			continue
		}
		out = append(out, anchorBand{start: s + 1, end: e + 1, i: b.i})
	}
	return out
}

// rowCurBand is the index in rb (rowBands) of d's current anchor, or -1.
func (d *openFile) rowCurBand(rb []anchorBand) int {
	if d.from == nil || d.from.ov == nil || d.anchorCur == "" {
		return -1
	}
	for k, b := range rb {
		if b.i < len(d.from.ov.anchors) && d.from.ov.anchors[b.i].dest == d.anchorCur {
			return k
		}
	}
	return -1
}

// rowNotes are the notes shown in the diff on its rows (Start / End = the
// first / last row + 1): the agent's notes on new lines, the review's
// remarks on their side. A note no row shows is left out.
func (d *openFile) rowNotes() []agentdocs.Note {
	dd := d.diff
	if dd == nil || dd.notice != "" {
		return nil
	}
	var out []agentdocs.Note
	place := func(n agentdocs.Note, old bool) {
		first, last := -1, -1
		for i, r := range dd.rows {
			no := r.nu
			if old {
				no = r.old
			}
			if r.kind == diffFold || no == 0 || no < n.Start || no > n.End {
				continue
			}
			if first < 0 {
				first = i
			}
			last = i
		}
		if first >= 0 {
			n.Start, n.End = first+1, last+1
			out = append(out, n)
		}
	}
	for _, n := range d.notes {
		place(n, false)
	}
	for _, rn := range dd.remarks {
		place(rn.n, rn.old)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Start < out[b].Start })
	return out
}

// rowNoteOrigin is note id as the file names it (its own lines), for its
// box's title and its full view.
func (d *openFile) rowNoteOrigin(id string) (agentdocs.Note, bool) {
	for _, n := range d.notes {
		if n.ID == id {
			return n, true
		}
	}
	if d.diff != nil {
		for _, rn := range d.diff.remarks {
			if rn.n.ID == id {
				return rn.n, true
			}
		}
	}
	return agentdocs.Note{}, false
}

// diffNoteRowsUnder is the diff popup's extraRows: the box rows hanging
// under rows [from, to).
func (d *openFile) diffNoteRowsUnder(from, to int) int {
	rows := 0
	for _, n := range d.rowNotes() {
		if i := n.End - 1; i >= from && i < to {
			rows += len(noteBoxLines(d.boxNote(n), d.noteW-noteBoxFrame, d.noteH))
		}
	}
	return rows
}

// diffNum is a row's number column: the new line's number (a deleted row's
// old one) and its sign, w wide.
func diffNum(r diffRow, w int) string {
	if r.kind == diffFold {
		return strings.Repeat(" ", w)
	}
	no, sign := r.nu, " "
	switch r.kind {
	case diffDel:
		no, sign = r.old, "−"
	case diffAdd:
		sign = "+"
	}
	return fmt.Sprintf("%*d %s ", w-3, no, sign)
}

// diffRowStyle is a diff row's look: a deleted or added row on the diff
// view's tint (the cursor's own tint on the cursor row, the selection
// stripe over it); an unchanged row as the file's (its band, the cursor);
// a fold dimmed.
func diffRowStyle(p *contentPopup, row int, cursorOff bool, l contentLine, r diffRow, band bandKind) (lipgloss.Style, bool) {
	var tint, cur lipgloss.Style
	switch r.kind {
	case diffDel:
		tint, cur = st().diffDelCell, st().diffDelCursor
	case diffAdd:
		tint, cur = st().diffAddCell, st().diffAddCursor
	case diffFold:
		if s, marked := previewRowMark(p, row, cursorOff, l, bandNone); marked {
			return s, true
		}
		return st().dim, true
	default:
		return previewRowMark(p, row, cursorOff, l, band)
	}
	style := tint
	if row == p.cur && !cursorOff {
		style = cur
	}
	if p.lsel.contains(row, p.cur) {
		style = st().selectionStyle(style)
	}
	return style, true
}

// shows reports whether p is one of d's popups (the file's or the diff's).
func (d *openFile) shows(p *contentPopup) bool {
	return d.p == p || d.diff != nil && d.diff.p == p
}

// mirrorPanes runs mirrorDiffCursor on both panes' documents of split sv.
func mirrorPanes(sv *splitView) {
	for _, p := range []pane{sv.left, sv.right} {
		if d := p.doc(); d != nil {
			d.mirrorDiffCursor()
		}
	}
}

// canFlip reports whether D acts on d here (spec §6): a file an open
// overview's link opened, shown in that overview's split (its single file
// side) or in a full-screen viewer (the narrow fallback). Anywhere else D
// keeps doing nothing.
func (m Model) canFlip(d *openFile) bool {
	if d == nil || d.stream != nil || d.from == nil || d.from.ov == nil || d.from.ov.closed {
		return false
	}
	if sv := m.splitShowing(d); sv != nil {
		r, ok := sv.right.(*docPane)
		return ok && r.d == d && sv.left.doc() == d.from
	}
	if m.layers != nil {
		for _, l := range m.layers.entries {
			if fv, ok := l.(*fileViewer); ok && fv.openFile == d {
				return true
			}
		}
	}
	return false
}

// diffWanted reports whether d should show its diff now: D can act on it
// and its overview's choice says diff. Read when a load or a read LANDS, so
// a D pressed meanwhile wins.
func (m Model) diffWanted(d *openFile) bool { return m.canFlip(d) && d.from.ov.diff }

// flipDiff is D on d: the overview's choice flips; the file comes back at
// once, the diff is read (or, while the file still loads, read when it
// lands). A diff left showing where D no longer acts (its overview gone)
// goes away.
func (m Model) flipDiff(d *openFile) (Model, tea.Cmd) {
	if !m.canFlip(d) {
		if d != nil && d.diff != nil {
			d.dropDiff(m.docRows(d))
		}
		return m, nil
	}
	ov := d.from.ov
	ov.diff = !ov.diff
	if !ov.diff {
		d.dropDiff(m.docRows(d))
		return m, nil
	}
	if d.loading {
		return m, nil // the fill reads it (diffOnLoad)
	}
	return m, m.diffReadCmd(d)
}

// diffKey is f (the changed lines only ⇄ the whole file) and enter on a ⋯
// fold, while d's diff shows. Declined otherwise.
func (m Model) diffKey(d *openFile, msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if d == nil || d.diff == nil || d.diff.notice != "" {
		return m, nil, false
	}
	dd, rows := d.diff, m.docRows(d)
	switch msg.String() {
	case "f":
		dd.partial = !dd.partial
		if d.from != nil && d.from.ov != nil {
			d.from.ov.diffPartial = dd.partial
		}
		d.refoldDiff(rows)
		return m, nil, true
	case "enter":
		if r := dd.p.cur; r >= 0 && r < len(dd.rows) && dd.rows[r].kind == diffFold && !dd.p.lsel.on {
			at := dd.rows[r].at
			dd.open[at] = true
			d.refoldDiff(rows)
			for i, x := range dd.rows {
				if x.at == at {
					dd.p.cur = i
					break
				}
			}
			dd.p.ensureCursorVisible(rows)
			return m, nil, true
		}
	}
	return m, nil, false
}

// reviewDiffMsg is a read of the diff of the document tagged tag (read
// number seq): a stored review's ReviewFileDiff, or an agent overview's
// HEAD → disk (agent).
type reviewDiffMsg struct {
	slotStamp
	tag   string
	seq   int
	agent bool
	res   domain.ReviewFileDiff
	err   error
}

// diffReadCmd reads d's diff off the UI thread: the review's stored range
// for a stored overview's file, HEAD → the file on disk for an agent's.
func (m Model) diffReadCmd(d *openFile) tea.Cmd {
	if m.svc == nil || d.from == nil {
		return nil
	}
	d.diffSeq++
	svc, tag, seq, path, slot := m.svc, d.tag, d.diffSeq, d.path, m.stamp()
	agent, id := d.from.src.kind != srcReviewOverview, d.from.src.rev
	return func() tea.Msg {
		ctx := context.Background()
		var res domain.ReviewFileDiff
		var err error
		if agent {
			res, err = svc.WorkingFileDiff(ctx, path)
		} else {
			res, err = svc.ReviewFileDiff(ctx, id, path)
		}
		return reviewDiffMsg{slotStamp: slot, tag: tag, seq: seq, agent: agent, res: res, err: err}
	}
}

// diffOnLoad is the read a file's load starts: its diff again when one
// shows (a reload: the shown diff stays until the new one lands), or the
// first when its overview's choice says diff.
func (m Model) diffOnLoad(d *openFile) tea.Cmd {
	if d.diff != nil || m.diffWanted(d) {
		return m.diffReadCmd(d)
	}
	return nil
}

// diffOnSteer is the read a steer to an already loaded file starts.
func (m Model) diffOnSteer(d *openFile) tea.Cmd {
	if d.diff != nil || d.loading || !m.diffWanted(d) {
		return nil
	}
	return m.diffReadCmd(d)
}

// diffLanded shows a read's diff — when it is the document's latest read
// and the choice still says diff.
func (m Model) diffLanded(msg reviewDiffMsg) Model {
	d := m.openFiles.findTag(msg.tag)
	if d == nil || msg.seq != d.diffSeq || !m.diffWanted(d) {
		return m
	}
	d.showDiff(msg.res, msg.err, msg.agent, d.from.ov.diffPartial, m.docRows(d))
	return m
}

// diffSwitch is the title's switch: file | diff, the half in force in
// reverse video.
func diffSwitch(on bool) string {
	f, d := i18n.T("file"), i18n.T("diff")
	rev := lipgloss.NewStyle().Reverse(true)
	if on {
		d = rev.Render(d)
	} else {
		f = rev.Render(f)
	}
	return f + " | " + d
}

// switchHalf is the switch's half under column col of a title innerW wide
// (0-based, the switch at its right end): 1 file, 2 diff, 0 neither.
func switchHalf(col, innerW int) int {
	f, d := lipgloss.Width(i18n.T("file")), lipgloss.Width(i18n.T("diff"))
	start := innerW - f - 3 - d
	switch {
	case col >= start && col < start+f:
		return 1
	case col >= innerW-d && col < innerW:
		return 2
	}
	return 0
}

// noteAtCursor is the note under the cursor, as the file names it: in a
// diff the box under the cursor's row (an old-side remark too), else the
// note on the cursor's line.
func (d *openFile) noteAtCursor() *agentdocs.Note {
	if dd := d.diff; dd != nil {
		if dd.notice != "" {
			return nil
		}
		row := dd.p.cur + 1
		for _, n := range d.rowNotes() {
			if n.Start <= row && row <= n.End {
				if o, ok := d.rowNoteOrigin(n.ID); ok {
					return &o
				}
			}
		}
		return nil
	}
	return d.noteAt(d.p.cur + 1)
}

// diffMarkedLines is the new lines a marking in the diff covers: the first
// and the last row inside it with a new line. ok false: it covers deleted
// rows (or folds) only — no link in the reviewed version.
func (d *openFile) diffMarkedLines() (line, last int, ok bool) {
	dd := d.diff
	if dd == nil || dd.notice != "" {
		return 0, 0, false
	}
	lo, hi, on := dd.p.lsel.bounds(dd.p.cur)
	if !on {
		lo, hi = dd.p.cur, dd.p.cur
	}
	for i := max(lo, 0); i <= hi && i < len(dd.rows); i++ {
		if r := dd.rows[i]; r.nu > 0 && r.kind != diffFold {
			if line == 0 {
				line = r.nu
			}
			last = r.nu
		}
	}
	return line, last, line > 0
}

// diffRowNumber is the number a row's own line has: its new line, a
// deleted row's old one; 0 for a fold.
func (dd *docDiff) rowNumber(i int) int {
	if i < 0 || i >= len(dd.rows) {
		return 0
	}
	if r := dd.rows[i]; r.kind == diffDel {
		return r.old
	} else if r.kind != diffFold {
		return r.nu
	}
	return 0
}
