package tui

import tea "github.com/charmbracelet/bubbletea"

// previewJumpKey gives the focused file preview's go-to-line prompt first
// refusal on a key — the file viewer, the files view's View-file preview and
// a split's file pane (activePreview's hosts). While the prompt is typed it
// owns every key; otherwise : opens it on a loaded FILE (an overview's prose
// and a review stack's stream have no line of a file to go to), unless the
// character selection or a search being typed has the keys.
func (m Model) previewJumpKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	p, rows, _, ok := m.activePreview()
	if !ok {
		return m, nil, false
	}
	d, ok := m.focusedDoc()
	if !ok || d.shownP() != p {
		return m, nil, false
	}
	if p.jump.typing {
		ev, n := p.jump.key(msg)
		if ev == jumpGo {
			nm, cmd := m.jumpDoc(d, n, rows)
			return nm, cmd, true
		}
		return m, nil, true
	}
	if !jumpOpenKey(msg) || !d.isFile() || !docLoaded(d) || p.cs.on || p.search.typing {
		return m, nil, false
	}
	p.jump.open()
	return m, nil, true
}

// jumpDoc lands line n of d's file. With the review's diff showing, a line
// the diff shows is landed in place; one it folds away brings the file back
// (D) at that line — the pane already holds the file the diff is drawn over.
// Past the end lands on the last line with landPendingLine's notice.
func (m Model) jumpDoc(d *openFile, n, rows int) (Model, tea.Cmd) {
	var cmd tea.Cmd
	if dd := d.diff; dd != nil && dd.notice == "" {
		if rowOfNew(dd.rows, n) >= 0 {
			d.p.cur = n - 1
			d.followIntoDiff(rows)
			return m, nil
		}
		m, cmd = m.flipDiff(d)
	}
	d.pendingLine = n
	m.statusMsg = d.landPendingLine(rows)
	return m, cmd
}
