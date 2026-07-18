package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// reflogResetRow offers "Reset to this entry" on the reflog panel: moves the
// current branch to the entry's commit via engine.Reset (soft/mixed/hard modal +
// non-ancestor confirm). Anchored on the panelReflog cursor.
func (m Model) reflogResetRow() (actionRow, bool) {
	if m.focus != panelReflog || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelReflog)
	if !ok {
		return actionRow{}, false
	}
	hash := m.reflog[bi].Hash // full SHA → unambiguous
	return actionRow{
		id:    "reflog-reset",
		label: i18n.T("Reset to this entry"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.confirmOp(engine.Reset{Commit: hash}, i18n.T("Reset to %s? This moves the current branch ref.", shortHash(hash)))
		},
	}, true
}

// reflogCheckoutRow offers "Check out this entry…" on the reflog panel: a modal
// (Detached / Create branch… / Cancel) mirroring the tag-checkout flow, on the
// panelReflog cursor entry.
func (m Model) reflogCheckoutRow() (actionRow, bool) {
	if m.focus != panelReflog || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelReflog)
	if !ok {
		return actionRow{}, false
	}
	ref := m.reflog[bi].Hash // full SHA
	return actionRow{
		id:    "reflog-checkout",
		label: i18n.T("Check out this entry…"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.modal = &decisionState{
				req: engine.DecisionRequest{
					ID:      "reflog-checkout",
					Prompt:  i18n.T("Check out %s:", shortHash(ref)),
					Options: []string{"Detached", "Create branch…", "Cancel"},
				},
				onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
					switch opt {
					case "Detached":
						return m.startOp(engine.Checkout{Ref: ref})
					case "Create branch…":
						return m.pushLayer(&reflogCheckoutPopup{ref: ref, name: newTextField("")}), nil
					}
					return m, nil
				},
			}
			return m, nil
		},
	}, true
}

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
	m, cmd := m.openChangedFiles(c)
	// Open on the TREE, not the commit-list side: the right column is the Commits
	// feed (not the reflog you came from), so flipping it with up/down would be
	// disorienting — you want to walk this entry's files. focus is the Commits
	// panel (the files-view commit-list side); leaving it panelReflog would make
	// tooltip() reveal the reflog row mis-placed over the tree.
	m.focus = panelCommits
	m = m.focusTree()
	return m, cmd
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
