package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// fileViewer is the full-screen View-file window over a WORKING-TREE file —
// where a content link (gg://<repo>/<path>?view=content) lands. It is the
// files view's right-column preview lifted onto the layer stack: the same
// popup state, the same loader (syntax colouring per [ui] diff_syntax), the
// same box (renderPreviewBox) and the same keys (activePreview routes the
// line cursor, the selection, the in-view search and the . menu's line rows
// here), so the two viewers cannot drift apart.
type fileViewer struct {
	*openFile // the document shown: its lines, cursor, selection, search
}

// openFileViewer pushes the viewer for path and starts its load off the UI
// thread. The bytes are the file ON DISK, uncommitted edits included. line
// (1-based, 0 = none) is where the cursor lands once the load arrives.
func (m Model) openFileViewer(path string, line int) (Model, tea.Cmd) {
	src := fileSource{kind: srcWorktree}
	d := m.openFiles.find(m.currentWorktree, docKey(src, path))
	if d == nil {
		d = newOpenFile(src, path)
	} else {
		// Already open: this is the same document, brought to the front and
		// reloaded (the disk may have moved on) at the reader's place.
		m = m.detachDoc(d)
		if line == 0 {
			d.keepPlace()
		}
	}
	d.pendingLine = line
	fv := &fileViewer{d}
	m = m.pushLayer(fv)
	m = m.registerDoc(d)
	return m, m.loadDoc(d)
}

// geom is the viewer's content size: the whole screen as one bordered box
// (border 2) holding a title and a hint line (2) — renderPreviewBox's math.
func (fv *fileViewer) geom(m Model) (rows, innerW int) { return m.viewerGeom() }

// viewerGeom is a full-screen viewer's content size — also the size a
// background document fills at, since it comes back in one.
func (m Model) viewerGeom() (rows, innerW int) {
	w, h := m.overlayDims()
	rows, innerW = h-4, w-4
	if rows < 1 {
		rows = 1
	}
	if innerW < 1 {
		innerW = 1
	}
	return rows, innerW
}

// update swallows every key. The shared preview hooks go first in the files
// view's order — selection, then search — and the pager/cursor keys follow.
func (fv *fileViewer) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if nm, cmd, ok := m.previewSelectKey(msg); ok {
		return nm, cmd
	}
	if nm, cmd, ok := m.previewSearchKey(msg); ok {
		return nm, cmd
	}
	p := fv.p
	rows, _ := fv.geom(m)
	scroll := func(delta int) { p.sel = previewClamp(p.sel+delta, len(p.lines), rows, p.mode) }
	switch msg.String() {
	case "esc":
		return m.closeDoc(fv.openFile), nil
	case "ctrl+]":
		return m.backgroundDoc(fv.openFile), nil
	case ".":
		return m.openActionMenu(), nil
	case "alt+up":
		m.movePreviewCursor(-1)
	case "alt+down":
		m.movePreviewCursor(1)
	case "up", "k":
		scroll(-1)
	case "down", "j":
		scroll(1)
	case "pgup":
		scroll(-rows)
	case "pgdown", " ":
		scroll(rows)
	case "home":
		p.sel = 0
	case "end":
		p.sel = previewClamp(len(p.lines), len(p.lines), rows, p.mode)
	case "ctrl+w":
		p.mode = p.mode.next()
		p.hscroll = 0
	case "shift+left":
		if p.mode == modeScroll && p.hscroll > 0 {
			if p.hscroll -= m.hscrollStep(); p.hscroll < 0 {
				p.hscroll = 0
			}
		}
	case "shift+right":
		if p.mode == modeScroll {
			p.hscroll += m.hscrollStep()
		}
	}
	return m, nil
}

// render owns the screen: one preview box the size of the terminal.
func (fv *fileViewer) render(m Model, _ string) string {
	w, h := m.overlayDims()
	return m.renderPreviewBox(fv.p, fv.title(), w, h, true, true)
}

// title names the version on screen: the working tree, a commit or a shelf.
func (fv *fileViewer) title() string {
	switch fv.src.kind {
	case srcCommit:
		return i18n.T("View %s @ %s", fv.path, shortHash(fv.src.rev))
	case srcShelf:
		return i18n.T("View %s (shelf)", fv.path)
	}
	return i18n.T("View %s (working tree)", fv.path)
}

// liveDoc finds the document a load result belongs to, and the content size
// of the frame showing it: any full-screen viewer on the stack (covered ones
// too — two files opened in a row must both fill), then the files view's
// preview, then the open-files list (a document in the background). A
// document closed since is not found: its result is stale.
func (m Model) liveDoc(tag string) (d *openFile, rows, innerW int, ok bool) {
	if m.layers != nil {
		for i := len(m.layers.entries) - 1; i >= 0; i-- {
			if fv, isViewer := m.layers.entries[i].(*fileViewer); isViewer && fv.tag == tag {
				rows, innerW = fv.geom(m)
				return fv.openFile, rows, innerW, true
			}
		}
	}
	if d := m.filesPreview; d != nil && d.tag == tag {
		return d, m.filePreviewRowsCap(), m.filePreviewInnerW(), true
	}
	if d := m.openFiles.findTag(tag); d != nil {
		rows, innerW = m.viewerGeom() // a background document returns in the viewer
		return d, rows, innerW, true
	}
	return nil, 0, 0, false
}
