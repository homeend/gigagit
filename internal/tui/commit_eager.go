package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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
// pages before asking to go deeper). "Go to" semantics: it CLEARS the /-filter so
// the cursor lands on the found commit within the full list (with surrounding
// history), not in a filtered-down view. Reused by the @-highlight trigger too,
// for which clearing filterQuery is a no-op and highlightQuery is left set (the
// found commit lands highlighted). The query lives in eager state regardless.
func (m Model) startEagerSearch(query string) (Model, tea.Cmd) {
	if query == "" {
		return m, nil
	}
	m.filterTyping = false
	m.filterQuery = "" // no sticky filter — land the cursor in the full list
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
		m.focus = panelCommits
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

// eagerPrompt is the "search deeper?" dialog (Task D3 implements update/render).
type eagerPrompt struct {
	query   string
	scanned int
	sel     int
}

func (p *eagerPrompt) update(m Model, _ tea.KeyMsg) (Model, tea.Cmd) { return m.popLayer(), nil }
func (p *eagerPrompt) render(m Model, below string) string           { return below }
