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
