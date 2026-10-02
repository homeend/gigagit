package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// A commit's Range review row.
//
// Notes written in a merge preview or a commit pair all sit on the range's
// newest commit, mostly on files that commit itself does not change. The
// commit's files view cannot put them on a file row, so it lists the review
// as one row (withScopeLines) and enter opens the range the notes were
// written in — frozen at this commit (domain.ScopeAtCommit) — as a commit
// pair's compare view, where every note is on its file. esc comes back.

// scopeBack is the way back from such a range: the commit whose files view
// opened it and the row to land on.
type scopeBack struct {
	commit model.Commit
	scope  string
}

// scopeOpenMsg is a Range review row's range, resolved off the UI thread. It
// carries the previewGen it was dispatched under, like pairOpenMsg.
type scopeOpenMsg struct {
	scope string
	back  model.Commit
	a, b  string
	eps   domain.PairEndpoints
	gen   int
	err   error
}

// scopeTitle names an opened review: a commit pair's is a range review, a
// merge preview's (reached from View all notes) a preview review.
func scopeTitle(scope string) string {
	if domain.IsPreviewScope(scope) {
		return i18n.T("Preview review: %s", scopeLabel(scope))
	}
	return i18n.T("Range review: %s", scopeLabel(scope))
}

// openScopeRow is enter on a Range review row of the commit on screen.
func (m Model) openScopeRow(scope string) (Model, tea.Cmd) {
	svc, gen := m.svc, m.previewGen
	if svc == nil {
		return m, nil
	}
	back := m.filesCommit
	if back.Hash == "" {
		back.Hash = m.filesHash
	}
	return m, func() tea.Msg {
		ctx := context.Background()
		msg := scopeOpenMsg{scope: scope, back: back, gen: gen}
		if msg.a, msg.b, msg.err = svc.ScopeAtCommit(ctx, scope, back.Hash); msg.err != nil {
			return msg
		}
		msg.eps, msg.err = svc.PairOpen(ctx, msg.a, msg.b)
		return msg
	}
}

// handleScopeOpenMsg opens the files view over the range, as a saved commit
// pair opens (handlePairOpenMsg): the note scope arrives by pairNotesMsg. A
// range that cannot be worked out — the target is gone, the branch was merged
// — says so and leaves the commit's files as they were.
func (m Model) handleScopeOpenMsg(msg scopeOpenMsg) (Model, tea.Cmd) {
	if msg.gen != m.previewGen {
		return m, nil // the view was closed or replaced while this resolved
	}
	if msg.err != nil {
		m.statusMsg = i18n.T("range review: %s", msg.err.Error())
		return m, nil
	}
	if st := msg.eps.Summary.State; st != domain.PairOK {
		m.statusMsg = pairMissingNotice(domain.CommitPair{A: msg.a, B: msg.b}, st)
		return m, nil
	}
	m.compareTag = "" // as a pair: never inherit a compare of the same two commits
	var cmd tea.Cmd
	m, cmd = m.openCompareFiles(msg.eps.Left, msg.eps.Right)
	m.filesTitle = scopeTitle(msg.scope)
	m.filesContext = m.filesTitle
	// After openCompareFiles: it tore the commit's view down (and with it any
	// earlier way back), and bumped previewGen, which the command stamps.
	m.filesBack = &scopeBack{commit: msg.back, scope: msg.scope}
	if nc := m.pairNotesCmd(msg.a, msg.b, msg.scope); nc != nil {
		cmd = tea.Batch(cmd, nc)
	}
	return m, cmd
}
