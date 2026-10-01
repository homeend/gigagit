package tui

import (
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
		if d.ov.sel < 0 {
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
			if len(a.spans) > 0 && a.spans[0].line >= top {
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
		if len(as[i].spans) > 0 {
			d.selectAnchor(i, rows)
			return
		}
	}
}

// openAnchor opens overview ov's anchor i in the foreground: a file at its
// line (a range selected), or the file a note sits on at the note. A target
// that is gone is marked and named in the status line instead.
func (m Model) openAnchor(ov *openFile, i int) (Model, tea.Cmd) {
	a := &ov.ov.anchors[i]
	// mark records what the open found, here and in the store (the page).
	mark := func(missing bool) {
		a.missing = missing
		m.docs.SetAnchorMissing(ov.id(), i, missing)
		ov.ov.paint(ov.p.lines)
	}
	gone := func(msg string) (Model, tea.Cmd) {
		mark(true)
		m.statusMsg = msg
		return m, nil
	}
	if id := a.target.Note; id != "" {
		d, n := m.findFileNote(id)
		if n == nil {
			return gone(i18n.T("note %s is gone", id))
		}
		mark(false)
		d.pendingLine, d.pendingEnd = n.Start, 0
		d.from, d.backgrounded = ov, true
		return m.bringToFront(d)
	}
	t := a.target
	if st, err := os.Stat(filepath.Join(m.currentWorktree, filepath.FromSlash(t.Path))); err != nil || st.IsDir() {
		return gone(i18n.T("no file %s", t.Path))
	}
	mark(false)
	m, cmd, _ := m.openFileViewerEv(t.Path, t.Start)
	if d := topDoc(m); d != nil {
		if t.End > t.Start {
			d.pendingEnd = t.End
		}
		d.from, d.backgrounded = ov, true
	}
	return m, cmd
}

// topDoc is the document of the full-screen viewer on top, or nil.
func topDoc(m Model) *openFile {
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

// overviewClick is a left click in the viewer showing overview fv: on an
// anchor it selects it, and a second click on the same anchor (a double
// click) opens it. gg laid the rows out itself, so row y is a line and
// column x a rune — no wrapping to undo.
func (m Model) overviewClick(fv *fileViewer, x, y int) (Model, tea.Cmd) {
	d := fv.openFile
	w, _ := m.overlayDims()
	rows, _ := m.viewerGeom()
	_, margin := readingColumn(w-4, m.readingWidth())
	row, col := y-2, x-2-margin // border + title line; border + padding + margin
	if row < 0 || row >= rows || col < 0 {
		return m, nil
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
	for i, a := range d.ov.anchors {
		for _, s := range a.spans {
			if s.line == line && at >= s.from && at < s.to {
				var double bool
				// row carries the anchor: a click on another anchor is no double.
				if m, double = m.registerClick(clickTarget{zone: zoneLayer, layer: fv, row: i}); double {
					return m.openAnchor(d, i)
				}
				d.selectAnchor(i, rows)
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
			ref := m.copyRow("overview-ref", i18n.T("Copy anchor reference"), i18n.T("Copied anchor reference"), anchorReference(d, d.ov.anchors[i]))
			ref.key = "r"
			rows = append(rows, ref)
		}
	}
	if d.from != nil {
		rows = append(rows, actionRow{id: "overview-back", key: "bksp", label: i18n.T("Back to overview"), run: func(m Model) (tea.Model, tea.Cmd) {
			nm, cmd, _ := m.anchorBack(d)
			return nm, cmd
		}})
	}
	return rows
}
