package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// markState identifies a marked row by stable identity (panelList.Key), not
// index, so it survives reloads, re-sorts, and filtering.
type markState struct {
	panel   panel
	key     string
	display string // human label for the status bar / popup (Key for now)
}

// pairOp is one two-argument operation a panel offers on (marked, selected).
type pairOp struct {
	label   func(marked, selected string) string
	build   func(marked, selected string) engine.Operation // nil when !enabled
	enabled bool
	note    string // shown for disabled entries
}

// pairOpsFor returns panel p's pair-operations. Only Branches has any; the
// labels spell out the direction so marked-vs-selected never carries
// implicit meaning.
func pairOpsFor(p panel) []pairOp {
	if p != panelBranches {
		return nil
	}
	return []pairOp{
		{
			label: func(marked, selected string) string { return "Merge " + marked + " into " + selected },
			build: func(marked, selected string) engine.Operation {
				return engine.SmartMerge{Source: marked, Target: selected}
			},
			enabled: true,
		},
		{
			label: func(marked, selected string) string { return "Rebase " + selected + " onto " + marked },
			note:  "not implemented yet",
		},
	}
}

// handleMarkKey implements the m-key state machine: mark, toggle off,
// move across panels, or pair with the marked row (opening the popup).
func (m Model) handleMarkKey() (tea.Model, tea.Cmd) {
	bi, ok := m.backingIndex(m.focus)
	if !ok {
		return m, nil
	}
	key := m.listFor(m.focus).Key(bi)
	// No mark, a mark in another panel, or a dead mark: (re-)mark here.
	if m.mark == nil || m.mark.panel != m.focus || !m.markAlive() {
		m.mark = &markState{panel: m.focus, key: key, display: key}
		return m, nil
	}
	if m.mark.key == key { // same row: toggle off
		m.mark = nil
		return m, nil
	}
	ops := pairOpsFor(m.focus)
	if len(ops) == 0 {
		m.statusMsg = "no pair operations for this panel"
		return m, nil
	}
	m.pairPopup = &pairOpPopup{marked: m.mark.display, selected: key, ops: ops}
	return m, nil
}

// markAlive reports whether the marked row still exists in its panel's
// backing list.
func (m Model) markAlive() bool {
	if m.mark == nil {
		return false
	}
	l := m.listFor(m.mark.panel)
	for i := 0; i < l.Len(); i++ {
		if l.Key(i) == m.mark.key {
			return true
		}
	}
	return false
}

// markDisplayIndex returns the display-row index of the mark in panel p, or
// -1 when p holds no living mark (or it is filtered out of view).
func (m Model) markDisplayIndex(p panel) int {
	if m.mark == nil || m.mark.panel != p {
		return -1
	}
	l := m.listFor(p)
	_, idx := m.panelView(p)
	for n, i := range idx {
		if l.Key(i) == m.mark.key {
			return n
		}
	}
	return -1
}
