package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// eagerSearch is the state of a /-search that pages unloaded history looking for
// a match. budget is the number of pages left to scan before re-prompting.
type eagerSearch struct {
	active bool
	query  string
	budget int
}

// commitSearchMaxPages is the configured per-pass page cap for eager search.
func (m Model) commitSearchMaxPages() int {
	if n := m.cfg.UI.CommitSearchMaxPages; n > 0 {
		return n
	}
	return 5
}

// firstCommitMatch returns the DISPLAY position (same space as m.sel[panelCommits])
// of the first Commits row whose haystack contains query (case-insensitive), or
// (0, false). Mirrors scanHighlightMatch's display→backing mapping so it is correct
// under a non-default sort.
func (m Model) firstCommitMatch(query string) (int, bool) {
	if query == "" {
		return 0, false
	}
	idx := m.displayIndices(panelCommits)
	q := strings.ToLower(query)
	for d, bi := range idx {
		if strings.Contains(strings.ToLower(m.commitHaystackAt(bi)), q) {
			return d, true
		}
	}
	return 0, false
}

// startEagerSearch begins scanning history for query (up to commitSearchMaxPages
// pages before asking to go deeper). "Go to" semantics: it CLEARS a Commits-panel
// /-filter so the cursor lands on the found commit within the full list (with
// surrounding history), not in a filtered-down view. Reused by the @-highlight
// trigger too, for which clearing filterQuery is a no-op and highlightQuery is
// left set (the found commit lands highlighted). The query lives in eager state
// regardless. A filter belonging to ANOTHER panel (e.g. Branches, when the
// goto-tip fallback starts the search from there) is preserved — "go to"
// semantics only ever concern the Commits list.
func (m Model) startEagerSearch(query string) (Model, tea.Cmd) {
	if query == "" {
		return m, nil
	}
	if m.filterPanel == panelCommits {
		m.filterTyping = false
		m.filterQuery = "" // no sticky filter — land the cursor in the full list
	}
	m.eager = eagerSearch{active: true, query: query, budget: m.commitSearchMaxPages()}
	return m.eagerAdvance()
}

// eagerAdvance is the step function, re-entered after each loaded page: jump on a
// match; page while the budget allows and the feed can load; prompt at the cap;
// report exhaustion.
func (m Model) eagerAdvance() (Model, tea.Cmd) {
	if !m.eager.active {
		return m, nil
	}
	if d, ok := m.firstCommitMatch(m.eager.query); ok {
		m.sel[panelCommits] = d
		m = m.focusCommitsPanel()
		m.eager = eagerSearch{}
		return m, nil
	}
	if m.feed == nil || !m.feed.CanLoadMore() {
		m.statusMsg = "'" + m.eager.query + "' not found in full history"
		m.eager = eagerSearch{}
		return m, nil
	}
	if m.eager.budget <= 0 {
		// Cap reached with no match: pause and ask (Task D3 supplies eagerPrompt).
		q := m.eager.query
		m.eager.active = false
		return m.pushLayer(&eagerPrompt{query: q, scanned: m.commitsTotal()}), nil
	}
	m.eager.budget--
	m.commitsLoading = true
	return m, m.loadMoreCmd()
}

// eagerPrompt is the "search deeper?" dialog shown when an eager /-search reaches
// its page cap with no match. enter on "Search N more" resumes with a fresh
// budget; Cancel/esc stops the scan on the loaded set.
type eagerPrompt struct {
	query   string
	scanned int
	sel     int // 0 = search more, 1 = cancel
}

func (p *eagerPrompt) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.String() {
	case "esc":
		return m.popLayer(), nil
	case "up", "k":
		p.sel = 0
	case "down", "j":
		p.sel = 1
	case "enter":
		if p.sel == 1 {
			return m.popLayer(), nil
		}
		m = m.popLayer()
		m.eager = eagerSearch{active: true, query: p.query, budget: m.commitSearchMaxPages()}
		return m.eagerAdvance()
	}
	return m, nil
}

func (p *eagerPrompt) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *eagerPrompt) box(m Model) string {
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	textW := popupTextWidth(inner)
	parts := []string{
		"Search deeper?",
		"",
		"Searched " + strconv.Itoa(p.scanned) + " commits, no match for \"" + p.query + "\".",
		"",
	}
	opts := []string{"Search " + strconv.Itoa(m.commitSearchMaxPages()) + " more pages", "Cancel"}
	for i, o := range opts {
		prefix, st := "  ", lipgloss.NewStyle()
		if i == p.sel {
			prefix, st = "> ", selectedRow
		}
		parts = append(parts, st.Render(padRight(prefix+o, textW)))
	}
	parts = append(parts, "", "[enter] choose  [esc] cancel")
	return popupBox(inner, strings.Join(parts, "\n"))
}
