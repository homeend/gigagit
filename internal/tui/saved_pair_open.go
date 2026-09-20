package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// pairOpenMsg is a saved commit pair resolved for the files view. It carries
// the previewGen it was dispatched under, like previewOpenMsg: a second enter
// or a closed view in the meantime makes this one stale, and it is dropped.
type pairOpenMsg struct {
	pair domain.CommitPair
	eps  domain.PairEndpoints
	gen  int
	err  error
}

func pairTitle(label string) string { return i18n.T("Saved diff: %s", label) }

// pairMissingNotice is the status line for a pair that cannot open: one of
// its two commits is not in this repository (a gc, or another clone).
func pairMissingNotice(p domain.CommitPair, st domain.PairState) string {
	if st == domain.PairMissingA {
		return i18n.T("missing commit: %s", shortHash(p.A))
	}
	return i18n.T("missing commit: %s", shortHash(p.B))
}

// openPairRow is enter on a pair row. The row's own summary already says when
// a commit is gone, so that refusal costs no git call.
func (m Model) openPairRow(r previewRow) (Model, tea.Cmd) {
	if r.err != nil {
		m.statusMsg = i18n.T("error: %s", r.err.Error())
		return m, nil
	}
	if r.psum.State != domain.PairOK {
		m.statusMsg = pairMissingNotice(r.pair, r.psum.State)
		return m, nil
	}
	svc, gen, p := m.svc, m.previewGen, r.pair
	return m, func() tea.Msg {
		eps, err := svc.PairOpen(context.Background(), p.A, p.B)
		return pairOpenMsg{pair: p, eps: eps, gen: gen, err: err}
	}
}

// handlePairOpenMsg opens the files view over the pair's two commits.
// previewOpen stays UNSET — a frozen pair has no tips to follow — and the note
// scope arrives by its own message (pairNotesMsg), gated on the two commits
// the view shows rather than on this opener.
func (m Model) handlePairOpenMsg(msg pairOpenMsg) (Model, tea.Cmd) {
	if msg.gen != m.previewGen {
		return m, nil
	}
	if msg.err != nil {
		m.statusMsg = i18n.T("error: %s", msg.err.Error())
		return m, nil
	}
	if msg.eps.Summary.State != domain.PairOK {
		m.statusMsg = pairMissingNotice(msg.pair, msg.eps.Summary.State)
		return m, nil
	}
	// Defeat openCompareFiles' same-tag guard, as a merge preview does: the
	// view already showing these two commits may be a branch compare whose
	// origin-filtered list a saved diff must not inherit.
	m.compareTag = ""
	var cmd tea.Cmd
	m, cmd = m.openCompareFiles(msg.eps.Left, msg.eps.Right)
	m.filesTitle = pairTitle(msg.pair.Label)
	m.filesContext = m.filesTitle
	// After openCompareFiles: it bumped previewGen, which the command stamps.
	if nc := m.pairNotesCmd(msg.pair.A, msg.pair.B); nc != nil {
		cmd = tea.Batch(cmd, nc)
	}
	return m, cmd
}
