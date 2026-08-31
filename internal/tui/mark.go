package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
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
	// open, when non-nil, is used instead of build+startOp: the picker calls it
	// to open a view (e.g. the interactive-rebase editor) for (marked, selected).
	open func(m Model, marked, selected string) (Model, tea.Cmd)
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
			label: func(marked, selected string) string { return i18n.T("Merge %s into %s", marked, selected) },
			build: func(marked, selected string) engine.Operation {
				return engine.SmartMerge{Source: marked, Target: selected}
			},
			enabled: true,
		},
		{
			label: func(marked, selected string) string { return i18n.T("Rebase %s onto %s", marked, selected) },
			build: func(marked, selected string) engine.Operation {
				return engine.SmartRebase{Branch: marked, Onto: selected}
			},
			enabled: true,
		},
		{
			label:   func(marked, selected string) string { return i18n.T("Interactive rebase %s onto %s", marked, selected) },
			enabled: true,
			open: func(m Model, marked, selected string) (Model, tea.Cmd) {
				return m, m.loadIrebaseCmd(marked, selected)
			},
		},
		{
			label:   func(marked, selected string) string { return i18n.T("Compare %s ↔ %s", marked, selected) },
			enabled: true,
			open: func(m Model, marked, selected string) (Model, tea.Cmd) {
				return m.openBranchCompare(marked, selected)
			},
		},
	}
}

// handleMarkKey implements the m-key state machine: mark, toggle off,
// move across panels, or pair with the marked row (opening the popup).
func (m Model) handleMarkKey() (tea.Model, tea.Cmd) {
	// Key off the unified list index (the space Key lives in), not backingIndex —
	// for Commits backingIndex is a pure feed index, so Key(backingIndex) would
	// mis-key once WIP rows shift the list (and refuse a WIP row outright).
	key, ok := m.selectedKey(m.focus)
	if !ok {
		return m, nil
	}
	// File panels: m toggles a multi-select set of files (for stashing), kept
	// separate from the single-mark/pair-op machinery used on other panels.
	if m.isFilesPanel(m.focus) {
		if m.fileMarks == nil {
			m.fileMarks = map[string]bool{}
		}
		if m.fileMarks[key] {
			delete(m.fileMarks, key)
		} else {
			m.fileMarks[key] = true
		}
		return m, nil
	}
	// Commits panel: m toggles membership in the compare selection set (◉). The
	// `.` menu then drives Compare or Squash on the selection; the m key itself
	// has no auto-diff — space (handleCommitSpaceKey) is the auto-compare fast
	// path over the same set. A WIP pseudo-row's sentinel key toggles into the
	// same set.
	if m.focus == panelCommits {
		if m.commitCompareSet == nil {
			m.commitCompareSet = map[string]bool{}
		}
		if m.commitCompareSet[key] {
			delete(m.commitCompareSet, key)
		} else {
			m.commitCompareSet[key] = true
		}
		return m, nil
	}
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
		m.statusMsg = i18n.T("no pair operations for this panel")
		return m, nil
	}
	// Branches: probe whether one branch can fast-forward to the other before
	// opening, so the popup can offer the row only when it applies (and in the
	// right direction). The probe is two ref reads + at most two ancestry
	// checks — cheap even on a huge repo.
	if m.focus == panelBranches && m.svc != nil {
		m.pairProbe = &pairProbeReq{marked: m.mark.display, selected: key}
		return m, m.loadPairOpsCmd(m.mark.display, key)
	}
	w, _ := m.overlayDims()
	m = m.pushLayer(newPairOpPopup(w, m.mark.display, key, ops))
	return m, nil
}

// pairProbeReq names the branch pair whose fast-forward probe is in flight;
// only a msg matching Model.pairProbe may open the popup, so a re-pair while
// an older probe runs supersedes it.
type pairProbeReq struct {
	marked, selected string
}

// pairOpsMsg delivers the fast-forward probe for a branch pair; the popup
// opens when it arrives.
type pairOpsMsg struct {
	marked, selected string
	ff               domain.FFPair
}

// loadPairOpsCmd probes the (marked, selected) fast-forward relation off the
// UI thread. A probe error fails open: the popup still opens, just without
// the fast-forward row (the standard ops carry their own guards).
func (m Model) loadPairOpsCmd(marked, selected string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ff, err := svc.FastForwardPair(context.Background(), marked, selected)
		if err != nil {
			ff = domain.FFPair{}
		}
		return pairOpsMsg{marked: marked, selected: selected, ff: ff}
	}
}

// ffPairOp is the conditional fast-forward row. The probe fixed the
// direction, so the closures ignore the (marked, selected) order.
func ffPairOp(behind, ahead string) pairOp {
	return pairOp{
		label: func(_, _ string) string { return i18n.T("Fast-forward %s to %s", behind, ahead) },
		build: func(_, _ string) engine.Operation {
			return engine.FastForward{Branch: behind, Commit: ahead}
		},
		enabled: true,
	}
}

// markedDisplayIndices returns the set of display-row indices in panel p that
// carry a marker: the single mark, plus every Status fileMark.
func (m Model) markedDisplayIndices(p panel) map[int]bool {
	out := map[int]bool{}
	if md := m.markDisplayIndex(p); md >= 0 {
		out[md] = true
	}
	if m.isFilesPanel(p) && len(m.fileMarks) > 0 {
		l := m.listFor(p)
		idx := m.displayIndices(p)
		for n, i := range idx {
			if m.fileMarks[l.Key(i)] {
				out[n] = true
			}
		}
	}
	return out
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
	idx := m.displayIndices(p)
	for n, i := range idx {
		if l.Key(i) == m.mark.key {
			return n
		}
	}
	return -1
}
