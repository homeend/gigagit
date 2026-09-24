package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// maxOpenFiles is how many files one worktree keeps open (spec ruling 4).
const maxOpenFiles = 20

// openFilesReg is the open-files list: per worktree, the documents the user
// or an agent opened, most recently shown first. A document leaves it only
// when it is closed on purpose (esc on it, x in the switcher) or evicted over
// the cap — every other teardown leaves it in the background.
type openFilesReg struct {
	byWT map[string][]*openFile
}

// list is wt's open files, most recently shown first. A nil registry is empty.
func (r *openFilesReg) list(wt string) []*openFile {
	if r == nil {
		return nil
	}
	return r.byWT[wt]
}

// find is wt's open document with key, or nil.
func (r *openFilesReg) find(wt, key string) *openFile {
	for _, d := range r.list(wt) {
		if d.key() == key {
			return d
		}
	}
	return nil
}

// touch puts d first in wt's list, adding it when new. Over the cap it drops
// the least recently shown document that is not on screen (shown reports
// that) and returns it; nil when nothing was dropped.
func (r *openFilesReg) touch(wt string, d *openFile, shown func(*openFile) bool) (evicted *openFile) {
	if r.byWT == nil {
		r.byWT = map[string][]*openFile{}
	}
	l := []*openFile{d}
	for _, e := range r.byWT[wt] {
		if e != d {
			l = append(l, e)
		}
	}
	if len(l) > maxOpenFiles {
		for i := len(l) - 1; i > 0; i-- {
			if !shown(l[i]) {
				evicted = l[i]
				l = append(l[:i], l[i+1:]...)
				break
			}
		}
	}
	r.byWT[wt] = l
	return evicted
}

// remove drops d from wt's list.
func (r *openFilesReg) remove(wt string, d *openFile) {
	if r == nil {
		return
	}
	l := r.byWT[wt][:0:0]
	for _, e := range r.byWT[wt] {
		if e != d {
			l = append(l, e)
		}
	}
	r.byWT[wt] = l
}

// docShown reports whether d is in a frame: the files view's preview or a
// full-screen viewer anywhere on the stack (a covered one is still shown —
// it comes back when the window above it closes).
func (m Model) docShown(d *openFile) bool {
	if m.filesPreview == d {
		return true
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

// detachDoc takes d out of whatever frame shows it, so it can be shown in
// another: a document is in one frame at a time.
func (m Model) detachDoc(d *openFile) Model {
	if m.filesPreview == d {
		m.filesPreview = nil
		m = m.focusTree()
	}
	if m.layers != nil {
		for _, l := range m.layers.entries {
			if fv, ok := l.(*fileViewer); ok && fv.openFile == d {
				return m.removeLayer(fv)
			}
		}
	}
	return m
}

// registerDoc puts d first in the current worktree's open files — call it
// once d is in its frame. A file dropped over the cap is named in the status.
func (m Model) registerDoc(d *openFile) Model {
	if m.openFiles == nil {
		return m
	}
	if ev := m.openFiles.touch(m.currentWorktree, d, m.docShown); ev != nil {
		m.statusMsg = i18n.T("closed %s (%d files open)", ev.path, maxOpenFiles)
	}
	return m
}

// docLoaded reports whether d holds its file's lines (not a placeholder).
func docLoaded(d *openFile) bool { return len(d.p.lines) > 0 && d.p.lines[0].src }

// backgroundDoc takes d off the screen and keeps it open (ctrl+]): the screen
// returns to whatever was beneath it.
func (m Model) backgroundDoc(d *openFile) Model {
	m = m.detachDoc(d)
	m.statusMsg = i18n.T("%s is in the background — ctrl+\\ lists open files", d.path)
	return m
}

// closeDoc closes d for good (esc on it, x in the switcher): off the screen
// and out of the list.
func (m Model) closeDoc(d *openFile) Model {
	m = m.detachDoc(d)
	m.openFiles.remove(m.currentWorktree, d)
	return m
}

// focusedDoc is the open file the keyboard is on: a full-screen viewer on
// top, else the files view's focused preview.
func (m Model) focusedDoc() (*openFile, bool) {
	if fv, ok := m.topLayer().(*fileViewer); ok {
		return fv.openFile, true
	}
	return m.focusedFilesPreview()
}

// backgroundRow is the . menu's "Send to background" for the focused open
// file — ctrl+] for the mouse.
func (m Model) backgroundRow() (actionRow, bool) {
	d, ok := m.focusedDoc()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "file-background",
		label: i18n.T("Send to background"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.backgroundDoc(d), nil
		},
	}, true
}

// docLoader is how d's bytes are read again: the disk, the commit, the shelf.
func (m Model) docLoader(d *openFile) func(context.Context) ([]byte, error) {
	svc, src, path := m.svc, d.src, d.path
	switch src.kind {
	case srcCommit:
		return func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, src.rev, path) }
	case srcShelf:
		ref := model.FileRef{Source: model.SourceShelf, Locator: src.rev, Path: path}
		return func(ctx context.Context) ([]byte, error) { return svc.ResolveBytes(ctx, ref) }
	}
	return func(ctx context.Context) ([]byte, error) { return svc.WorktreeFile(ctx, path) }
}

// bringToFront shows open file d (the switcher's enter): the files view's
// preview when it is the preview (focus moves there), else a full-screen
// viewer on top — a covered viewer's frame is moved up, a background file
// gets a new one (spec ruling 9). A working-tree file, or one whose load
// never landed, is read again at the reader's place.
func (m Model) bringToFront(d *openFile) (Model, tea.Cmd) {
	if m.filesPreview == d && m.topLayer() == nil {
		m.filesTreeFocused = false
		return m.registerDoc(d), nil
	}
	m = m.detachDoc(d)
	m = m.pushLayer(&fileViewer{d})
	m = m.registerDoc(d)
	if d.src.kind != srcWorktree && docLoaded(d) {
		return m, nil
	}
	d.keepPlace()
	return m, loadFileContentSrcCmd(d.tag, d.path, m.cfg.UI.SyntaxOn(), m.docLoader(d))
}
