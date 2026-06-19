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
		if s.sel < 0 || s.sel >= len(s.commits) {
			return model.Bookmark{}, false
		}
		fc := s.commits[s.sel]
		return model.Bookmark{State: model.StateCommitted, Commit: fc.Hash, Path: fc.Path}, true
	case *blameView:
		if s.ctx.path == "" {
			return model.Bookmark{}, false
		}
		if s.ctx.rev == "" { // working-tree blame → the current worktree's working file
			return model.Bookmark{State: model.StateUnstaged, Worktree: m.currentWorktree, Branch: m.status.Branch, Path: s.ctx.path}, true
		}
		return model.Bookmark{State: model.StateCommitted, Commit: s.ctx.rev, Path: s.ctx.path}, true
	}
	if v := m.diffView; v != nil {
		if v.title == "" {
			return model.Bookmark{}, false
		}
		if v.rev != "" {
			return model.Bookmark{State: model.StateCommitted, Commit: v.rev, Path: v.title}, true
		}
		return model.Bookmark{State: model.StateUnstaged, Worktree: m.currentWorktree, Branch: m.status.Branch, Path: v.title}, true
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
	}
	return model.Bookmark{}, false
}

// bookmarkToFileRef maps a bookmark's address to a FileRef so the focused
// (left) compare side resolves via domain.ResolveBytes — by address, with no
// pre-resolved blob SHA. A committed bookmark's Commit may be a stash commit.
func bookmarkToFileRef(b model.Bookmark) model.FileRef {
	switch b.State {
	case model.StateShelf:
		return model.FileRef{Source: model.SourceShelf, Locator: b.ShelfID, Path: b.Path}
	case model.StateStaged:
		return model.FileRef{Source: model.SourceStaged, Path: b.Path}
	case model.StateCommitted:
		return model.FileRef{Source: model.SourceCommit, Locator: b.Commit, Path: b.Path}
	default: // unstaged / untracked
		return model.FileRef{Source: model.SourceUnstaged, Path: b.Path}
	}
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

// compareAgainstBookmarkRow is the menu-only "Compare against bookmark" action,
// present wherever a single file is focused. The focused file is frozen at build
// time; running it stashes that ref on the Model and opens the bookmark picker
// in compare mode.
func (m Model) compareAgainstBookmarkRow() (actionRow, bool) {
	b, ok := m.focusedBookmark()
	if !ok {
		return actionRow{}, false
	}
	ref := bookmarkToFileRef(b)
	label := bookmarkDisplay(b)
	return actionRow{
		id:    "bookmark-compare",
		label: "Compare against bookmark",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.pendingCompare = &pendingCompare{ref: ref, label: label}
			return m, m.loadBookmarksCmd()
		},
	}, true
}
