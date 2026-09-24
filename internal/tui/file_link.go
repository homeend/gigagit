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
	err        error
}

// fileRowPath is the path of the file ROW under the cursor, plus the line to
// focus (1-based; 0 = the whole file — every surface but the viewer) — a Files/Staged
// panel row or a files-view tree row (commit, stash, shelf) — or false on
// anything else (a diff, history or blame on top, a directory heading, a
// commit row). A files-view row deleted in its commit still counts: its
// "Copy file link" then says the file is not in the working tree, which is
// the answer the user asked for.
func (m Model) fileRowPath() (string, int, bool) {
	switch s := m.topLayer().(type) {
	case *fileViewer: // the viewed file itself, at the cursor line
		line := 0
		if p := s.p; p.cur >= 0 && p.cur < len(p.lines) && p.lines[p.cur].src {
			line = p.cur + 1
		}
		return s.p.title, line, true
	case *historyView, *blameView:
		return "", 0, false
	}
	path, ok := m.fileListRowPath()
	return path, 0, ok
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
	svc := m.svc
	return actionRow{
		id:    "copy-file-link",
		label: i18n.T("Copy file link"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, func() tea.Msg {
				present, err := svc.WorktreeFilesPresent(context.Background(), []string{path})
				return fileLinkCheckedMsg{path: path, text: text, present: err == nil && present[path], err: err}
			}
		},
	}, true
}
