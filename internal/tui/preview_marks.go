package tui

import (
	"sort"
	"strings"

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
// first), whichever was marked first. It walks the ROWS, not the visible ones
// — a mark hidden by the / filter still counts, exactly as on Commits
// (compareSelectionEndpoints) — and only ORDERS by display position, hidden
// rows after the visible ones. Marks whose row is gone or linkless are
// skipped: the set is stale-tolerant, like commitCompareSet.
func (m Model) previewMarkedLinks() []string {
	if len(m.previewCompareSet) == 0 {
		return nil
	}
	pos := make(map[int]int, len(m.previews))
	for n, i := range m.displayIndices(panelPreviews) {
		pos[i] = n
	}
	type marked struct {
		at   int
		link string
	}
	var rows []marked
	for i, r := range m.previews {
		if !m.previewCompareSet[r.id()] {
			continue
		}
		l, ok := m.previewRowLink(r)
		if !ok {
			continue
		}
		at, visible := pos[i]
		if !visible {
			at = len(m.previews) + i
		}
		rows = append(rows, marked{at, l})
	}
	sort.Slice(rows, func(a, b int) bool { return rows[a].at < rows[b].at })
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.link
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
	// A comparison is loading (a link compare is ~50 git calls — seconds on a
	// slow mount). Swallow the key: a second space here would UNMARK the row
	// the user just marked, and the view would then open over a set that no
	// longer says why. esc cancels (it drops the marks and the load).
	if m.linkCompareWant != "" {
		m.statusMsg = i18n.T("comparing…")
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
	subject := m.previewMarkedSubjects()
	m, cmd := m.startLinkCompare(links[0], links[1])
	if cmd == nil {
		return m, nil
	}
	// A link compare is ~50 git calls — seconds on a slow mount — and the view
	// opens only when the load lands. Until then a small popup says so (and
	// owns the keyboard, so a second space cannot unmark the row just marked).
	// Not a placeholder files view: compare mode with no endpoints is a state
	// other readers (the session snapshot) rightly refuse.
	m = m.pushLayer(&compareLoadingPopup{tag: m.linkCompareWant, subject: subject})
	return m, cmd
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

// previewMarkedSubjects names the marked rows for the loading popup, in the
// order the comparison takes them: "<label> ↔ <label>".
func (m Model) previewMarkedSubjects() string {
	var names []string
	for _, i := range m.displayIndices(panelPreviews) {
		if i < len(m.previews) && m.previewCompareSet[m.previews[i].id()] {
			names = append(names, m.previews[i].label())
		}
	}
	return strings.Join(names, " ↔ ")
}

// compareLoadingPopup sits over the Previews tab while the marked rows'
// comparison loads. esc cancels the load (the result is dropped on arrival);
// every other key is swallowed. loadedLinkCompare closes it — see
// closeCompareLoading — BEFORE opening the view, so the view is not "opened
// from a popup" and esc on it returns to the panel, not to this.
type compareLoadingPopup struct {
	tag     string
	subject string
}

func (p *compareLoadingPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		if m.linkCompareWant == p.tag {
			m.linkCompareWant = ""
		}
		m.statusMsg = ""
		return m.popLayer(), nil
	}
	return m, nil
}

func (p *compareLoadingPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	body := i18n.T("comparing…") + "\n\n" + p.subject + "\n\n" + i18n.T("[esc] cancel")
	return overlayCenter(clipToHeight(below, h), popupBox(popupInnerWidth(w), body), w, h)
}

// closeCompareLoading pops the loading popup when it waits for tag.
func (m Model) closeCompareLoading(tag string) Model {
	if p, ok := m.topLayer().(*compareLoadingPopup); ok && p.tag == tag {
		return m.popLayer()
	}
	return m
}
