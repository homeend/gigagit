package tui

import (
	"context"

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
	p   *contentPopup
	tag string // "worktree:<path>" — gates a stale fileContentMsg
}

// openFileViewer pushes the viewer for path and starts its load off the UI
// thread. The bytes are the file ON DISK, uncommitted edits included.
func (m Model) openFileViewer(path string) (Model, tea.Cmd) {
	fv := &fileViewer{
		p:   &contentPopup{title: path, lines: []contentLine{{text: i18n.T("(loading…)")}}},
		tag: "worktree:" + path,
	}
	m = m.pushLayer(fv)
	svc := m.svc
	return m, loadFileContentSrcCmd(fv.tag, path, m.cfg.UI.SyntaxOn(), func(ctx context.Context) ([]byte, error) {
		return svc.WorktreeFile(ctx, path)
	})
}

// geom is the viewer's content size: the whole screen as one bordered box
// (border 2) holding a title and a hint line (2) — renderPreviewBox's math.
func (fv *fileViewer) geom(m Model) (rows, innerW int) {
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
		return m.popLayer(), nil
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
	return m.renderPreviewBox(fv.p, i18n.T("View %s (working tree)", fv.p.title), w, h, true)
}
