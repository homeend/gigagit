package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

// openReflogFiles opens the commit files-view for the reflog row under the
// cursor, reusing the commit files-view path with a synthesized model.Commit.
// Anchors on the panelReflog cursor, never on Commits-panel selection.
func (m Model) openReflogFiles() (Model, tea.Cmd) {
	bi, ok := m.backingIndex(panelReflog)
	if !ok {
		return m, nil
	}
	e := m.reflog[bi]
	c := model.Commit{Hash: e.Hash, Subject: e.Subject}
	m.filesView = &contentPopup{lines: []contentLine{{text: "(loading…)"}}}
	m.filesTitle = "Files " + shortHash(c.Hash) + " " + c.Subject
	m.filesHash = c.Hash
	m.filesTreeFocused = false
	m.filesAllFiles = false
	m.filesPreview = nil
	m.filesPreviewTag = ""
	m.filesReadInflight = true
	return m, m.loadCommitFilesCmd(c)
}

// reflogRows renders the HEAD reflog entries for the panel body.
func (m Model) reflogRows() []string {
	rows := make([]string, len(m.reflog))
	for i, e := range m.reflog {
		rows[i] = e.ShortHash + "  " + e.Subject + "  (" + e.Rel + ")"
	}
	return rows
}

// reflogList adapts the reflog entries to the panelList contract.
type reflogList struct {
	items []model.ReflogEntry
	rows  []string
}

func (l reflogList) Len() int          { return len(l.items) }
func (l reflogList) Row(i int) string  { return l.rows[i] }
func (l reflogList) Name(i int) string { return l.items[i].Subject }
func (l reflogList) Date(i int) int64  { return 0 } // git default order is newest-first; no per-entry timestamp
func (l reflogList) Key(i int) string  { return l.items[i].Selector }

// Haystack lets the filter match the full SHA and selector, not just the row.
func (l reflogList) Haystack(i int) string {
	e := l.items[i]
	return e.Hash + " " + e.Selector + " " + e.Subject
}
