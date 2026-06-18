package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

// shelfRows formats one "[source] path  #sha8" line per shelf entry.
func (m Model) shelfRows() []string {
	rows := make([]string, len(m.shelfEntries))
	for i, e := range m.shelfEntries {
		sha := e.SHA
		if len(sha) > 8 {
			sha = sha[:8]
		}
		rows[i] = fmt.Sprintf("[%s] %s  #%s", e.Source, e.Path, sha)
	}
	return rows
}

type shelfLoadedMsg struct {
	entries []model.ShelfEntry
	err     error
}

// loadShelfCmd loads the default bucket's entries (all of them — the shelf is a
// local read; the Store API stays paged for a future backend). A disabled shelf
// (no state dir) yields an empty list, not an error modal.
func (m Model) loadShelfCmd() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		es, err := svc.ShelfList(context.Background(), "", 0, 0)
		return shelfLoadedMsg{entries: es, err: err}
	}
}

// commitOrWorktreeRef maps a (rev, path) pair to a FileRef: a real commit is a
// SourceCommit; an empty rev (working-tree blame) is the unstaged file.
func commitOrWorktreeRef(rev, path string) model.FileRef {
	if rev == "" {
		return model.FileRef{Source: model.SourceUnstaged, Path: path}
	}
	return model.FileRef{Source: model.SourceCommit, Locator: rev, Path: path}
}

// focusedShelfRef resolves the file under focus to a FileRef for "Add to shelf",
// mirroring contextCopyRows' precedence. The two-sided diff view is deliberately
// excluded (which side?). Returns false where no single file is focused.
func (m Model) focusedShelfRef() (model.FileRef, bool) {
	switch s := m.stackTop().(type) {
	case *historyView:
		return commitOrWorktreeRef(s.ctx.rev, s.ctx.path), s.ctx.path != ""
	case *blameView:
		return commitOrWorktreeRef(s.ctx.rev, s.ctx.path), s.ctx.path != ""
	}
	if m.diffView != nil {
		return model.FileRef{}, false // two-sided; deferred
	}
	if v := m.filesView; v != nil {
		if m.filesTreeFocused && m.filesHash != "" { // a commit's file tree (not a stash)
			if vis := v.visible(); v.sel >= 0 && v.sel < len(vis) && vis[v.sel].path != "" {
				return model.FileRef{Source: model.SourceCommit, Locator: m.filesHash, Path: vis[v.sel].path}, true
			}
		}
		return model.FileRef{}, false
	}
	switch m.focus {
	case panelFiles:
		if bi, ok := m.backingIndex(panelFiles); ok {
			return model.FileRef{Source: model.SourceUnstaged, Path: m.status.Files[bi].Path}, true
		}
	case panelStaged:
		if bi, ok := m.backingIndex(panelStaged); ok {
			return model.FileRef{Source: model.SourceStaged, Path: m.status.Files[bi].Path}, true
		}
	}
	return model.FileRef{}, false
}

type shelfAddedMsg struct {
	entry model.ShelfEntry
	err   error
}

// shelfAddCmd freezes ref's bytes into the default bucket off the UI thread.
func (m Model) shelfAddCmd(ref model.FileRef) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		e, err := svc.ShelfAdd(context.Background(), ref, "")
		return shelfAddedMsg{entry: e, err: err}
	}
}

// shelfAddRow is the menu-only "Add to shelf" action, present wherever a single
// file is focused. Its run handler captures the resolved ref at build time.
func (m Model) shelfAddRow() (actionRow, bool) {
	ref, ok := m.focusedShelfRef()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "shelf-add",
		label: "Add to shelf",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.shelfAddCmd(ref)
		},
	}, true
}
