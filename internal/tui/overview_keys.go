package tui

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/i18n"
)

// overviewKey gives an overview's own keys their shot: tab / shift+tab walk
// the anchors, enter opens the selected one, r copies a reference to it, y
// the whole text. Any other document, or any other key: not ours.
func (m Model) overviewKey(d *openFile, msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if d == nil || d.ov == nil {
		return m, nil, false
	}
	rows, _ := m.viewerGeom()
	switch msg.String() {
	case "tab":
		d.stepAnchor(1, rows)
		return m, nil, true
	case "shift+tab":
		d.stepAnchor(-1, rows)
		return m, nil, true
	case "enter":
		if d.ov.sel < 0 {
			return m, nil, true
		}
		nm, cmd := m.openAnchor(d, d.ov.sel)
		return nm, cmd, true
	case "r":
		if d.ov.sel < 0 || d.src.kind.storedOverview() { // a stored overview has no store reference (A9)
			return m, nil, true
		}
		return m, m.copyToClipboardCmd(i18n.T("Copied anchor reference"), anchorReference(d, d.ov.anchors[d.ov.sel])), true
	case "y":
		return m, m.copyToClipboardCmd(i18n.T("copied"), d.ov.text), true
	}
	return m, nil, false
}

// stepAnchor selects the next (dir > 0) or previous anchor that is on screen
// somewhere, wrapping at the ends. With none selected, the first step picks
// the first anchor at or below the top of the window (the last one above it
// going back).
func (d *openFile) stepAnchor(dir, rows int) {
	as := d.ov.anchors
	n := len(as)
	if n == 0 {
		return
	}
	i := d.ov.sel
	if i < 0 {
		top := d.p.clampTop(d.p.sel, rows)
		i = -1
		if dir < 0 {
			i = 0
		}
		for k, a := range as {
			if len(a.spans) > 0 && !a.plain && a.spans[0].line >= top {
				i = k - 1
				if dir < 0 {
					i = k
				}
				break
			}
		}
	}
	for range n {
		i = ((i+dir)%n + n) % n
		if len(as[i].spans) > 0 && !as[i].plain {
			d.selectAnchor(i, rows)
			return
		}
	}
}

// pageAtEnd pages the overview where the anchor walk would wrap: a step
// down from the last anchor with text still below it scrolls a page down, a
// step up from the first with text above it a page up — the text past the
// ends is read before the walk starts over. It reports whether it paged.
func (d *openFile) pageAtEnd(dir, rows int) bool {
	as, sel := d.ov.anchors, d.ov.sel
	if sel < 0 || sel >= len(as) {
		return false
	}
	for i := sel + dir; i >= 0 && i < len(as); i += dir {
		if len(as[i].spans) > 0 && !as[i].plain {
			return false // a next anchor short of the end: no wrap
		}
	}
	top, last := d.p.clampTop(d.p.sel, rows), d.p.lastVisible(rows)
	// Paging reads on past the anchor: from a window at or past it. One the
	// reader scrolled back over (home, pgup) walks the anchors as before.
	if len(as[sel].spans) == 0 {
		return false
	}
	if line := as[sel].spans[0].line; (dir > 0 && line > last) || (dir < 0 && line < top) {
		return false
	}
	if (dir > 0 && last >= len(d.p.lines)-1) || (dir < 0 && top == 0) { // the end shows: wrap
		return false
	}
	d.p.scrollBy(dir*rows, rows)
	return true
}

// markFileAnchors records whether overview ov's file anchors on path found
// their file: on the anchors, in the store for an agent's overview (the page
// shows the mark, and a sync brings it back), and in the overview's paint.
// A note anchor has no path: it keeps its own mark. changed reports whether
// any anchor's mark flipped.
func (m Model) markFileAnchors(ov *openFile, path string, missing bool) (changed bool) {
	for i := range ov.ov.anchors {
		a := &ov.ov.anchors[i]
		if a.target.Path != path || a.missing == missing {
			continue
		}
		a.missing, changed = missing, true
		if ov.src.kind == srcOverview && m.docs != nil {
			m.docs.SetAnchorMissingIn(ov.root, ov.id(), i, missing)
		}
	}
	if changed {
		ov.ov.paint(ov.p.lines)
	}
	return changed
}

// readGone reports whether a failed read means the file is not there: any
// error reading a commit (git names no other kind), only "does not exist"
// in the working tree — a file it cannot read (permissions) is there, as the
// store's stat check finds it. (Odd paths — a directory now there, a parent
// turned file — read as there here while that check says gone.)
func readGone(err error, worktree bool) bool {
	return err != nil && (!worktree || errors.Is(err, fs.ErrNotExist))
}

// openAnchor opens overview ov's anchor i in the foreground: a file at its
// line (a range selected), or the file a note sits on at the note. A target
// that is gone is marked and named in the status line instead.
func (m Model) openAnchor(ov *openFile, i int) (Model, tea.Cmd) {
	a := &ov.ov.anchors[i]
	// mark records what the open found, here and in the store (the page).
	mark := func(missing bool) {
		a.missing = missing
		if ov.src.kind == srcOverview {
			m.docs.SetAnchorMissingIn(ov.root, ov.id(), i, missing)
		}
		ov.ov.paint(ov.p.lines)
	}
	gone := func(msg string) (Model, tea.Cmd) {
		mark(true)
		m.statusMsg = msg
		return m, nil
	}
	if ov.src.kind.storedOverview() { // a stored overview: its commit's files, nothing else
		if a.plain {
			if ov.src.kind == srcCommitOverview {
				m.statusMsg = i18n.T("anchor %s does not resolve at the overview's commit", a.dest)
			} else {
				m.statusMsg = i18n.T("anchor %s does not resolve at the reviewed commit", a.dest)
			}
			return m, nil
		}
		var cmd tea.Cmd
		if ov.ov.tip != "" {
			m, cmd = m.openFileAtCommitLine(ov.ov.tip, a.target.Path, a.target.Start, 0)
		} else { // a working review: its files are the working tree's
			m, cmd, _ = m.openFileViewerEv(a.target.Path, a.target.Start)
		}
		if d := topDoc(m); d != nil {
			d.from, d.backgrounded, d.anchorCur = ov, true, a.dest
			ov.ov.beginStep(d)
			m.markStepViewer(d)
		}
		return m, cmd
	}
	if id := a.target.Note; id != "" {
		d, n := m.findFileNote(id)
		if n == nil {
			return gone(i18n.T("note %s is gone", id))
		}
		mark(false)
		d.pendingLine, d.pendingEnd = n.Start, 0
		d.anchorCur = "" // a note is no band: none is current
		d.from, d.backgrounded = ov, true
		nm, cmd := m.bringToFront(d)
		nm.markStepViewer(d)
		return nm, cmd
	}
	// The file is stat'ed off the UI thread (a slow mount must not freeze the
	// screen); anchorStatted opens it if the overview is still in front.
	path, tag, dest := filepath.Join(m.currentWorktree, filepath.FromSlash(a.target.Path)), ov.tag, a.dest
	return m, func() tea.Msg {
		st, err := os.Stat(path)
		return anchorStatMsg{tag: tag, i: i, dest: dest, ok: err == nil && !st.IsDir()}
	}
}

// anchorStatMsg is openAnchor's file check: anchor i (dest) of the overview
// tagged tag is a file (ok) or is gone.
type anchorStatMsg struct {
	tag  string
	i    int
	dest string
	ok   bool
}

// anchorStatted finishes openAnchor once its file check is in: nothing when
// the overview left the front or its anchors changed meanwhile.
func (m Model) anchorStatted(msg anchorStatMsg) (Model, tea.Cmd) {
	ov := topDoc(m)
	if ov == nil || ov.tag != msg.tag || ov.ov == nil || msg.i >= len(ov.ov.anchors) || ov.ov.anchors[msg.i].dest != msg.dest {
		return m, nil
	}
	a := &ov.ov.anchors[msg.i]
	a.missing = !msg.ok
	if ov.src.kind == srcOverview {
		m.docs.SetAnchorMissingIn(ov.root, ov.id(), msg.i, a.missing)
	}
	ov.ov.paint(ov.p.lines)
	t := a.target
	if !msg.ok {
		m.statusMsg = i18n.T("no file %s", t.Path)
		return m, nil
	}
	m, cmd, _ := m.openFileViewerEv(t.Path, t.Start)
	if d := topDoc(m); d != nil {
		// The range is drawn as the current band (anchor_bands.go), not
		// selected: the reader's own marking stays theirs.
		d.from, d.backgrounded, d.anchorCur = ov, true, msg.dest
		ov.ov.beginStep(d)
		m.markStepViewer(d)
	}
	return m, cmd
}

// topDoc is the document of the full-screen viewer on top, or nil.
func topDoc(m Model) *openFile {
	if _, d, _, _, ok := m.splitFocus(); ok {
		return d
	}
	if fv, ok := m.topLayer().(*fileViewer); ok {
		return fv.openFile
	}
	return nil
}

// anchorBack is backspace in a file an anchor opened: the file steps aside
// (it stays open) and the overview comes back, its anchor still selected.
// false: d was not opened from an overview — not our key.
func (m Model) anchorBack(d *openFile) (Model, tea.Cmd, bool) {
	if d == nil || d.from == nil {
		return m, nil, false
	}
	for _, e := range m.openFiles.list(m.currentWorktree) {
		if e == d.from {
			m = m.backgroundDoc(d)
			nm, cmd := m.bringToFront(e)
			return nm, cmd, true
		}
	}
	d.from = nil // the way back is gone for good: stop offering it
	m.statusMsg = i18n.T("the overview was closed")
	return m, nil, true
}

// anchorReference is what r copies: enough for the agent to know which
// overview and which step the user means.
func anchorReference(ov *openFile, a anchor) string {
	return agentdocs.AnchorReference(ov.overviewCopy(), agentdocs.Anchor{Dest: a.dest})
}

// overviewClick is a left click in the full-screen viewer showing overview
// fv: its content sits in the reading column under the border and title.
func (m Model) overviewClick(fv *fileViewer, x, y int) (Model, tea.Cmd) {
	w, _ := m.overlayDims()
	rows, _ := m.viewerGeom()
	_, margin := readingColumn(w-4, m.readingWidth())
	return m.overviewClickIn(fv.openFile, fv, x, y, rows, margin)
}

// overviewClickIn is a left click at (x, y) in the frame l showing overview
// d, its rows starting at row 2 and column 2+margin (border + title; border
// + padding + margin): on an anchor it selects it, and a second click on the
// same anchor (a double click) opens it. gg laid the rows out itself, so row
// y is a line and column x a rune — no wrapping to undo, only the horizontal
// scroll's pan. In a split, selecting steers the file pane and opening moves
// the focus to it.
func (m Model) overviewClickIn(d *openFile, l layer, x, y, rows, margin int) (Model, tea.Cmd) {
	row, col := y-2, x-2-margin
	if row < 0 || row >= rows || col < 0 || d.p.cs.on { // the copy mode is modal: a click waits
		return m, nil
	}
	if d.p.mode == modeScroll {
		col += d.p.hscroll
	}
	line := d.p.clampTop(d.p.sel, rows) + row
	if line >= len(d.p.lines) {
		return m, nil
	}
	at, cw := -1, 0
	for k, r := range []rune(d.p.lines[line].text) {
		cw += lipgloss.Width(string(r))
		if cw > col {
			at = k
			break
		}
	}
	sv, inSplit := l.(*splitView)
	for i, a := range d.ov.anchors {
		for _, s := range a.spans {
			if s.line == line && at >= s.from && at < s.to {
				var double bool
				// row carries the anchor: a click on another anchor is no double.
				m, double = m.registerClick(clickTarget{zone: zoneLayer, layer: l, row: i})
				switch {
				case inSplit:
					d.selectAnchorAt(i, line, rows)
					nm, cmd := m.steerSplit(sv)
					if double {
						sv.focus(false)
					}
					return nm, cmd
				case double:
					return m.openAnchor(d, i)
				}
				d.selectAnchorAt(i, line, rows) // where it was clicked: no scroll under the second click
				return m, nil
			}
		}
	}
	return m, nil
}

// overviewRows are the . menu's rows for an overview on screen (its keys for
// the mouse) and, in a file an anchor opened, the way back.
func (m Model) overviewRows() []actionRow {
	d, ok := m.focusedDoc()
	if !ok {
		return nil
	}
	if sv, _, _, _, inSplit := m.splitFocus(); inSplit && sv.left.doc() == d && d.ov != nil {
		return m.splitOverviewRows(sv, d)
	}
	var rows []actionRow
	if d.ov != nil {
		rows = append(rows, actionRow{id: "overview-next", key: "tab", label: i18n.T("Next anchor"), run: func(m Model) (tea.Model, tea.Cmd) {
			rows, _ := m.viewerGeom()
			d.stepAnchor(1, rows)
			return m, nil
		}})
		if i := d.ov.sel; i >= 0 {
			rows = append(rows, actionRow{id: "overview-open", key: "enter", label: i18n.T("Open anchor"), run: func(m Model) (tea.Model, tea.Cmd) {
				return m.openAnchor(d, i)
			}})
			if !d.src.kind.storedOverview() { // a stored overview has no store reference (A9)
				ref := m.copyRow("overview-ref", i18n.T("Copy anchor reference"), i18n.T("Copied anchor reference"), anchorReference(d, d.ov.anchors[i]))
				ref.key = "r"
				rows = append(rows, ref)
			}
		}
		rows = append(rows, m.overviewExportRow(d), m.overviewLinkRow(d))
		if m.heldOverview(d) { // an agent's document: the file goes only on purpose
			rows = append(rows, actionRow{id: "overview-delete", label: i18n.T("Delete overview"), run: func(m Model) (tea.Model, tea.Cmd) {
				return m.confirmSessionOverviewDelete(d)
			}})
		}
	}
	rows = append(rows, m.bandRows(d)...)
	if d.from != nil && m.splitShowing(d) == nil { // in a split the overview is on screen: backspace is the split's
		rows = append(rows, actionRow{id: "overview-back", key: "bksp", label: i18n.T("Back to overview"), run: func(m Model) (tea.Model, tea.Cmd) {
			nm, cmd, _ := m.anchorBack(d)
			return nm, cmd
		}})
	}
	return rows
}

// splitOverviewRows are the . menu's rows for the overview pane of split
// sv: the walk by the split's keys (each step steers the file pane), Open
// anchor (steer and focus the file pane), and Copy overview.
func (m Model) splitOverviewRows(sv *splitView, d *openFile) []actionRow {
	step := func(dir int) func(Model) (tea.Model, tea.Cmd) {
		return func(m Model) (tea.Model, tea.Cmd) {
			rows, _ := sv.geomOf(m, sv.left)
			d.stepAnchor(dir, rows)
			return m.steerSplit(sv)
		}
	}
	rows := []actionRow{
		{id: "overview-next", key: "↓", label: i18n.T("Next anchor"), run: step(1)},
		{id: "overview-prev", key: "↑", label: i18n.T("Previous anchor"), run: step(-1)},
	}
	if d.ov.sel >= 0 {
		rows = append(rows, actionRow{id: "overview-open", key: "enter", label: i18n.T("Open anchor"), run: func(m Model) (tea.Model, tea.Cmd) {
			nm, cmd := m.steerSplit(sv)
			sv.focus(false)
			return nm, cmd
		}})
	}
	if i := d.ov.sel; i >= 0 && !d.src.kind.storedOverview() { // a stored overview has no store reference (A9)
		ref := m.copyRow("overview-ref", i18n.T("Copy anchor reference"), i18n.T("Copied anchor reference"), anchorReference(d, d.ov.anchors[i]))
		ref.key = "r"
		rows = append(rows, ref)
	}
	cp := m.copyRow("overview-copy", i18n.T("Copy overview"), i18n.T("copied"), d.ov.text)
	cp.key = "y"
	rows = append(rows, m.overviewExportRow(d), m.overviewLinkRow(d), cp)
	if m.heldOverview(d) { // an agent's document: the file goes only on purpose (parity with the viewer's menu)
		rows = append(rows, actionRow{id: "overview-delete", label: i18n.T("Delete overview"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.confirmSessionOverviewDelete(d)
		}})
	}
	return rows
}
