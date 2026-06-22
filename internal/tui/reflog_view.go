package tui

import "github.com/gigagit/gg/internal/model"

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
