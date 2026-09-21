package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// The Previews tab's ◉ compare selection: m toggles a row's membership, space
// does the same but is capped at two and opens the comparison the moment the
// second mark lands — the Commits panel's gesture (commit_space.go). The
// comparison is each row's own gg:// link handed to startLinkCompare, so it is
// exactly what copying the two rows' links and comparing them by hand gives.

// previewRowLink is the ONE gg:// link a Previews row stands for: a merge
// preview's <target>...<source>, a commit pair's @<a>..<b>. A saved comparison
// is two links and has none; neither has a row whose names the grammar cannot
// carry.
func (m Model) previewRowLink(r previewRow) (string, bool) {
	if _, isCmp := r.compare(); isCmp {
		return "", false
	}
	if rec, isMerge := r.merge(); isMerge {
		return m.previewLinkFor(rec.Source, rec.Target, "", 0)
	}
	return m.pairLinkFor(r.pair.A, r.pair.B)
}

// previewMarkedLinks are the marked rows' links in DISPLAY order (upper row
// first), whichever was marked first. Marks whose row is gone, filtered out or
// linkless are skipped: the set is stale-tolerant, like commitCompareSet.
func (m Model) previewMarkedLinks() []string {
	if len(m.previewCompareSet) == 0 {
		return nil
	}
	var out []string
	for _, i := range m.displayIndices(panelPreviews) {
		if i >= len(m.previews) || !m.previewCompareSet[m.previews[i].id()] {
			continue
		}
		if l, ok := m.previewRowLink(m.previews[i]); ok {
			out = append(out, l)
		}
	}
	return out
}

// togglePreviewMark flips the cursor row's membership. limit > 0 refuses to grow
// the set past that many live marks (space); 0 is uncapped (m). marked reports
// that a mark was ADDED — the only case space may open a comparison after.
func (m Model) togglePreviewMark(limit int) (_ Model, marked bool) {
	if !m.opsIdle() {
		return m, false
	}
	r, ok := m.selectedPreview()
	if !ok {
		return m, false
	}
	if m.previewCompareSet[r.id()] {
		delete(m.previewCompareSet, r.id())
		return m, false
	}
	if _, isCmp := r.compare(); isCmp {
		m.statusMsg = i18n.T("a saved comparison is already two links — it cannot be compared with another row (enter opens it)")
		return m, false
	}
	if _, ok := m.previewRowLink(r); !ok {
		m.statusMsg = i18n.T("this row has no gg:// link to compare")
		return m, false
	}
	if limit > 0 && len(m.previewMarkedLinks()) >= limit {
		m.statusMsg = i18n.T("2 previews already marked — space a marked one to unmark, esc to unmark all")
		return m, false
	}
	if m.previewCompareSet == nil {
		m.previewCompareSet = map[string]bool{}
	}
	m.previewCompareSet[r.id()] = true
	return m, true
}

// handlePreviewSpaceKey is space on the Previews tab.
func (m Model) handlePreviewSpaceKey() (tea.Model, tea.Cmd) {
	m, marked := m.togglePreviewMark(2)
	if !marked {
		return m, nil
	}
	return m.compareMarkedPreviews()
}

// compareMarkedPreviews opens the two marked rows' comparison; with any other
// count it does nothing. Marks persist, so esc returns to both ◉ still set.
func (m Model) compareMarkedPreviews() (Model, tea.Cmd) {
	links := m.previewMarkedLinks()
	if len(links) != 2 {
		return m, nil
	}
	return m.startLinkCompare(links[0], links[1])
}

// previewCompareMarkedRow is the `.` menu's consumer of the m-marked pair.
func (m Model) previewCompareMarkedRow() (actionRow, bool) {
	if m.focus != panelPreviews || !m.opsIdle() || len(m.previewMarkedLinks()) != 2 {
		return actionRow{}, false
	}
	return actionRow{
		id:    "preview-compare-marked",
		label: i18n.T("Compare the 2 marked previews"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.compareMarkedPreviews()
		},
	}, true
}

// previewMarksClearRow keeps a lone off-cursor or stale mark menu-reachable.
func (m Model) previewMarksClearRow() (actionRow, bool) {
	if m.focus != panelPreviews || len(m.previewCompareSet) == 0 {
		return actionRow{}, false
	}
	return actionRow{
		id:    "preview-marks-clear",
		label: i18n.T("Unmark all previews"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.previewCompareSet = nil
			return m, nil
		},
	}, true
}
