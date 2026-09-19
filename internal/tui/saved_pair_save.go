package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// pairSaveShas is the commit pair "Save to previews" stores: old side, new
// side. It does NOT rank the marks itself — it takes the two endpoints
// compareSelectionEndpoints already chose, so the saved direction IS the
// direction "Compare selection" diffs in, by construction rather than by two
// arms agreeing.
//
// Exactly two COMMITS: a ◇ Working tree / ◇ Staged row cannot be frozen, and
// a 3+ range is a different change-set (oldest^..newest) this row does not
// claim to save.
func (m Model) pairSaveShas() (a, b string, ok bool) {
	if len(m.validCompareKeys()) != 2 {
		return "", "", false
	}
	left, right, _, ok := m.compareSelectionEndpoints()
	if !ok || left.Kind() != model.EndpointCommit || right.Kind() != model.EndpointCommit {
		return "", "", false
	}
	return left.CacheTag(), right.CacheTag(), true // a commit endpoint's tag is its sha
}

// commitSavePairRows offers "Save to previews" and its reverse while exactly
// two commits are ◉-marked. Saving leaves the marks alone: the usual next
// step is to look at the diff just saved (space / Compare selection).
func (m Model) commitSavePairRows() []actionRow {
	if m.focus != panelCommits || !m.opsIdle() {
		return nil
	}
	a, b, ok := m.pairSaveShas()
	if !ok {
		return nil
	}
	// The entry is named after the NEWER commit's subject — the row already
	// shows <a7>..<b7> in its own column, so the sha default would say it
	// twice. Both directions take the same name: it says what the diff is
	// about, and the column says which way it runs.
	name := m.commitSubject(b)
	row := func(id, label, a, b string) actionRow {
		return actionRow{id: id, label: label, run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.pairAddCmd(a, b, name, false)
		}}
	}
	return []actionRow{
		row("commit-save-pair", i18n.T("Save to previews"), a, b),
		row("commit-save-pair-reversed", i18n.T("Save reversed to previews"), b, a),
	}
}

// commitSubject is the loaded feed's subject for hash, "" when it is not
// paged in (domain then falls back to the <a7>..<b7> label).
func (m Model) commitSubject(hash string) string {
	for _, c := range m.commits {
		if c.Hash == hash {
			return c.Subject
		}
	}
	return ""
}
