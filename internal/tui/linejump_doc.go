package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

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
	if !ok || d.shownP() != p || !d.isFile() {
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
	if !jumpOpenKey(msg) || !docLoaded(d) || p.cs.on || p.search.typing {
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

// blameJumpKey is the go-to-line prompt's first refusal in blame: the line
// cursor lands on line n of the blamed file.
func (m Model) blameJumpKey(b *blameView, msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if b.jump.typing {
		if ev, n := b.jump.key(msg); ev == jumpGo {
			if n > len(b.lines) {
				m.statusMsg = i18n.T("line %d is past the end of %s (%d lines)", n, b.ctx.path, len(b.lines))
				n = len(b.lines)
			}
			b.sel = n - 1
		}
		return m, nil, true
	}
	if !jumpOpenKey(msg) || b.loading || b.err != nil || len(b.lines) == 0 || b.cs.on || b.search.typing {
		return m, nil, false
	}
	b.jump.open()
	return m, nil, true
}

// stackJumpKey is the prompt's first refusal on a review's stacked stream:
// line n of the file the cursor is in (every file has a line n), centred. A
// folded or unloaded file has no lines: the cursor rests on its header.
func (m Model) stackJumpKey(sp *stackPane, msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	p := sp.d.p
	if p.jump.typing {
		ev, n := p.jump.key(msg)
		if ev != jumpGo {
			return m, nil, true
		}
		fi := sp.stream.fileAt(p.cur)
		if fi < 0 {
			return m, nil, true
		}
		f := sp.stream.files[fi]
		if f.shows() && n > len(f.lines) {
			m.statusMsg = i18n.T("line %d is past the end of %s (%d lines)", n, f.path, len(f.lines))
		}
		rows, _ := sp.sv.geomOf(m, sp)
		p.cur = sp.stream.streamLine(fi, n)
		p.sel = p.clampTop(p.cur-rows/2, rows)
		p.ensureCursorVisible(rows)
		return m, nil, true
	}
	if !jumpOpenKey(msg) || !docLoaded(sp.d) || p.cs.on || p.search.typing {
		return m, nil, false
	}
	p.jump.open()
	return m, nil, true
}
