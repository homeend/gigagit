package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

// focusedBookmark builds a Bookmark for the file under focus, mirroring the
// shelf-capture precedence. The two-sided diff view is excluded.
func (m Model) focusedBookmark() (model.Bookmark, bool) {
	switch s := m.stackTop().(type) {
	case *historyView:
		if s.ctx.path == "" || s.ctx.rev == "" {
			return model.Bookmark{}, false
		}
		return model.Bookmark{State: model.StateCommitted, Commit: s.ctx.rev, Path: s.ctx.path}, true
	case *blameView:
		if s.ctx.path == "" || s.ctx.rev == "" {
			return model.Bookmark{}, false
		}
		return model.Bookmark{State: model.StateCommitted, Commit: s.ctx.rev, Path: s.ctx.path}, true
	}
	if m.diffView != nil {
		return model.Bookmark{}, false
	}
	if v := m.filesView; v != nil {
		if m.filesTreeFocused && m.filesHash != "" {
			if vis := v.visible(); v.sel >= 0 && v.sel < len(vis) && vis[v.sel].path != "" {
				return model.Bookmark{State: model.StateCommitted, Commit: m.filesHash, Path: vis[v.sel].path}, true
			}
		}
		return model.Bookmark{}, false
	}
	switch m.focus {
	case panelFiles:
		if bi, ok := m.backingIndex(panelFiles); ok {
			f := m.status.Files[bi]
			st := model.StateUnstaged
			if f.Kind == model.KindUntracked {
				st = model.StateUntracked
			}
			return model.Bookmark{State: st, Worktree: m.currentWorktree, Branch: m.status.Branch, Path: f.Path}, true
		}
	case panelStaged:
		if bi, ok := m.backingIndex(panelStaged); ok {
			return model.Bookmark{State: model.StateStaged, Worktree: m.currentWorktree, Branch: m.status.Branch, Path: m.status.Files[bi].Path}, true
		}
	case panelShelf:
		if bi, ok := m.backingIndex(panelShelf); ok {
			e := m.shelfEntries[bi]
			return model.Bookmark{State: model.StateShelf, ShelfID: e.ID, SHA: e.SHA, Path: e.Path}, true
		}
	}
	return model.Bookmark{}, false
}

type bookmarkAddedMsg struct {
	bm  model.Bookmark
	err error
}

func (m Model) bookmarkAddCmd(b model.Bookmark) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		stored, err := svc.BookmarkAdd(context.Background(), b)
		return bookmarkAddedMsg{bm: stored, err: err}
	}
}

// bookmarkAddRow is the menu-only "Bookmark this file" action, present wherever
// a single file is focused. The resolved address is captured at build time.
func (m Model) bookmarkAddRow() (actionRow, bool) {
	b, ok := m.focusedBookmark()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "bookmark-add",
		label: "Bookmark this file",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.bookmarkAddCmd(b)
		},
	}, true
}
