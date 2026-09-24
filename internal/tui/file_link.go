package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// fileLinkCheckedMsg carries the "Copy file link" presence check: the link
// is copied only when the file is on disk in this worktree.
type fileLinkCheckedMsg struct {
	path, text string
	present    bool
	// changed: the link came from a preview of ANOTHER version whose lines
	// differ from the disk's, so its line would point at other text.
	changed bool
	err     error
}

// fileRowPath is the path of the file ROW under the cursor, plus the line to
// focus (1-based; 0 = the whole file — every surface but the viewer) — a Files/Staged
// panel row or a files-view tree row (commit, stash, shelf) — or false on
// anything else (a diff, history or blame on top, a directory heading, a
// commit row). A files-view row deleted in its commit still counts: its
// "Copy file link" then says the file is not in the working tree, which is
// the answer the user asked for.
func (m Model) fileRowPath() (string, int, bool) {
	if d, ok := m.focusedDoc(); ok {
		p := d.p
		if p.cur < 0 || p.cur >= len(p.lines) || !p.lines[p.cur].src {
			// Still loading, or a placeholder: no line to name. The disk file
			// itself is still linkable; another version cannot be checked.
			return d.path, 0, d.src.kind == srcWorktree
		}
		return d.path, p.cur + 1, true
	}
	switch m.topLayer().(type) {
	case *historyView, *blameView:
		return "", 0, false
	}
	if m.diffLayer() != nil {
		return "", 0, false
	}
	path, ok := m.fileListRowPath()
	return path, 0, ok
}

// focusedFilesPreview is the files view's View-file preview when it holds
// the focus (and no full-screen viewer sits above it). It shows a version of
// the file — a commit's, a shelf's — that the disk may no longer match.
func (m Model) focusedFilesPreview() (*openFile, bool) {
	if m.filesPreview == nil || m.filesTreeFocused || m.filesView == nil {
		return nil, false
	}
	if m.topLayer() != nil { // a viewer, a diff, history or blame on top has the keys
		return nil, false
	}
	return m.filesPreview, true
}

// sameContentLines reports whether the disk's bytes split into exactly the
// raw lines the preview shows — the one case where the preview's line
// numbers are the disk file's.
func sameContentLines(disk []byte, shown []contentLine) bool {
	got := fileContentLinesTok(disk, nil)
	if len(got) != len(shown) {
		return false
	}
	for i := range got {
		if got[i].src != shown[i].src || got[i].raw != shown[i].raw {
			return false
		}
	}
	return true
}

// fileListRowPath is fileRowPath for the file LISTS (the files-view tree, the
// Files/Staged panels): a row names a whole file, never a line.
func (m Model) fileListRowPath() (string, bool) {
	if m.diffLayer() != nil {
		return "", false
	}
	if v := m.filesView; v != nil {
		if !m.filesTreeFocused {
			return "", false
		}
		vis := v.visible()
		if v.sel < 0 || v.sel >= len(vis) || vis[v.sel].path == "" {
			return "", false
		}
		return vis[v.sel].path, true
	}
	if m.stashView != nil && m.focus == panelCommits {
		return "", false
	}
	switch m.focus {
	case panelFiles, panelStaged:
		if b, ok := m.focusedBookmark(); ok && b.Path != "" {
			return b.Path, true
		}
	}
	return "", false
}

// contextFileLinkRow is the . menu's "Copy file link": the file's CONTENT
// link (?view=content) — no commit, the file as it is on disk — copied only
// after a stat proves the file is there.
func (m Model) contextFileLinkRow() (actionRow, bool) {
	path, line, ok := m.fileRowPath()
	if !ok {
		return actionRow{}, false
	}
	text, ok := m.buildLinkFor(model.FileAddress{State: model.StateUnstaged, Worktree: m.currentWorktree, Path: path}, model.NoteSideNew, line, 0, model.ContentHint)
	if !ok {
		return actionRow{}, false
	}
	// From a preview, the line is only right if the disk shows the same
	// text: snapshot the shown lines now, compare off the UI thread.
	var shown []contentLine
	if d, ok := m.focusedDoc(); ok && d.src.kind != srcWorktree {
		shown = append([]contentLine(nil), d.p.lines...)
	}
	svc := m.svc
	return actionRow{
		id:    "copy-file-link",
		label: i18n.T("Copy file link"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, func() tea.Msg {
				ctx := context.Background()
				present, err := svc.WorktreeFilesPresent(ctx, []string{path})
				msg := fileLinkCheckedMsg{path: path, text: text, present: err == nil && present[path], err: err}
				if msg.present && shown != nil {
					disk, rerr := svc.WorktreeFile(ctx, path)
					if rerr != nil {
						msg.err = rerr
					} else {
						msg.changed = !sameContentLines(disk, shown)
					}
				}
				return msg
			}
		},
	}, true
}
