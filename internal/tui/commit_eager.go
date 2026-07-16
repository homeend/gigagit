package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/i18n"
)

// eagerSearch is the state of a /-search that pages unloaded history looking for
// a match. budget is the number of pages left to scan before re-prompting. from
// is the feed-index floor: only commits at m.commits[from:] count as matches
// (0 = the whole list, WIP rows included). Each ctrl+f press sets it to the
// loaded count so the scan always digs past what is already on screen, even
// when an earlier match was found. After a search ends, query stays behind
// (active=false) so the next ctrl+f can restart the cycle deeper even once the
// / filter or @ highlight is gone (e.g. esc-cleared, or the goto-tip fallback
// which clears a filter on start).
type eagerSearch struct {
	active bool
	query  string
	budget int
	from   int
}

// commitSearchMaxPages is the configured per-pass page cap for eager search.
// The fallback mirrors config.Defaults() for a Model whose cfg never arrived.
func (m Model) commitSearchMaxPages() int {
	if n := m.cfg.UI.CommitSearchMaxPages; n > 0 {
		return n
	}
	return 50
}

// firstCommitMatch returns the DISPLAY position (same space as m.sel[panelCommits])
// of the first Commits row whose haystack contains query (case-insensitive), or
// (0, false). Mirrors scanHighlightMatch's display→backing mapping so it is correct
// under a non-default sort. A from > 0 restricts matches to feed indices >= from
// (commits loaded after an eager restart); it also excludes WIP pseudo-rows,
// which are never "newly loaded history".
func (m Model) firstCommitMatch(query string, from int) (int, bool) {
	if query == "" {
		return 0, false
	}
	idx := m.displayIndices(panelCommits)
	q := strings.ToLower(query)
	for d, bi := range idx {
		if from > 0 && bi-m.wipCount() < from {
			continue
		}
		if strings.Contains(strings.ToLower(m.commitHaystackAt(bi)), q) {
			return d, true
		}
	}
	return 0, false
}

// startEagerSearch begins scanning history for query (up to commitSearchMaxPages
// pages before asking to go deeper). This is the goto-tip fallback entry: "go to"
// semantics CLEAR a Commits-panel /-filter, because the searched hash is
// unrelated to the filter text — a kept filter could hide the target row from
// displayIndices entirely. A filter belonging to ANOTHER panel (e.g. Branches,
// when the goto-tip fallback starts the search from there) is preserved — "go
// to" semantics only ever concern the Commits list.
func (m Model) startEagerSearch(query string) (Model, tea.Cmd) {
	if query == "" {
		return m, nil
	}
	if m.filterPanel == panelCommits {
		m.filterTyping = false
		m.filterQuery = "" // no sticky filter — land the cursor in the full list
	}
	return m.startEagerSearchFrom(query, 0)
}

// startEagerSearchDeeper is the ctrl+f entry point: it restarts the eager cycle
// past the already-loaded commits, so every press pages new history even when an
// earlier match is already on screen. If the fresh batches come up empty the
// usual "search deeper?" prompt re-asks, and a later press redoes the whole
// cycle from the then-current end. Unlike the goto-tip entry above, a Commits
// /-filter STAYS engaged (same standing as the @ highlight — the query IS the
// filter text, so every hit is visible in the filtered list); ctrl+f only
// commits an in-progress typing session.
func (m Model) startEagerSearchDeeper(query string) (Model, tea.Cmd) {
	if query == "" {
		return m, nil
	}
	if m.filterPanel == panelCommits {
		m.filterTyping = false
	}
	m.statusMsg = i18n.T("searching deeper for '%s'…", query)
	return m.startEagerSearchFrom(query, len(m.commits))
}

func (m Model) startEagerSearchFrom(query string, from int) (Model, tea.Cmd) {
	if query == "" {
		return m, nil
	}
	m.eager = eagerSearch{active: true, query: query, budget: m.commitSearchMaxPages(), from: from}
	return m.eagerAdvance()
}

// eagerAdvance is the step function, re-entered after each loaded page: jump on a
// match; page while the budget allows and the feed can load; prompt at the cap;
// report exhaustion.
func (m Model) eagerAdvance() (Model, tea.Cmd) {
	if !m.eager.active {
		return m, nil
	}
	if d, ok := m.firstCommitMatch(m.eager.query, m.eager.from); ok {
		m.sel[panelCommits] = d
		m = m.focusCommitsPanel()
		if m.eager.from > 0 {
			m.statusMsg = i18n.T("found '%s'", m.eager.query)
		}
		m.eager = eagerSearch{query: m.eager.query} // keep the query for a repeat ctrl+f
		return m, nil
	}
	if m.feed == nil || !m.feed.CanLoadMore() {
		if m.eager.from > 0 {
			m.statusMsg = i18n.T("no further match for '%s' in history", m.eager.query)
		} else {
			m.statusMsg = i18n.T("'%s' not found in full history", m.eager.query)
		}
		m.eager = eagerSearch{query: m.eager.query}
		return m, nil
	}
	if m.eager.budget <= 0 {
		// Cap reached with no match: pause and ask (Task D3 supplies eagerPrompt).
		q, from := m.eager.query, m.eager.from
		m.eager.active = false
		scanned := m.commitsTotal()
		if from > 0 {
			scanned = len(m.commits) - from // only what this pass actually scanned
		}
		return m.pushLayer(&eagerPrompt{query: q, scanned: scanned, from: from}), nil
	}
	m.eager.budget--
	m.commitsLoading = true
	return m, m.loadMoreCmd()
}

// eagerPrompt is the "search deeper?" dialog shown when an eager /-search reaches
// its page cap with no match. enter on "Search N more" resumes with a fresh
// budget; Cancel/esc stops the scan on the loaded set.
type eagerPrompt struct {
	popupMax
	query   string
	scanned int
	from    int // >0: a deeper pass — resume keeps the floor, wording says "further"
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
		m.eager = eagerSearch{active: true, query: p.query, budget: m.commitSearchMaxPages(), from: p.from}
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
	inner := popupResolveWidth(w, p.maximized, popupInnerWidth(w))
	textW := popupTextWidth(inner)
	report := i18n.T("Searched %d commits, no match for \"%s\".", p.scanned, p.query)
	if p.from > 0 {
		report = i18n.T("Searched %d more commits, no further match for \"%s\".", p.scanned, p.query)
	}
	parts := []string{
		i18n.T("Search deeper?"),
		"",
		report,
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
	parts = append(parts, "", i18n.T("[enter] choose  [esc] cancel"))
	return popupBox(inner, strings.Join(parts, "\n"))
}
