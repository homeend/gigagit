package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// prTitle is the files-view title of an open pull-request diff.
func prTitle(p model.PullRequest) string {
	return i18n.T("PR #%d · %s", p.Number, sanitizeRowText(p.Title))
}

// canOpenPR gates enter / i / y on the Pull requests tab: a focused row and no
// op in the way.
func (m Model) canOpenPR() bool {
	_, ok := m.selectedPR()
	return m.focus == panelPRs && ok && m.opsIdle()
}

// prFetchReadyMsg carries the resolved fetch op back to the UI thread.
// Resolving it asks the forge which repository the PR targets (a network
// call), so it never runs on the Update goroutine.
type prFetchReadyMsg struct {
	pr  model.PullRequest
	op  engine.FetchPRHead
	err error
}

// openPRCmd starts enter's chain: resolve the fetch op off-thread → run it
// (prFetchReadyMsg) → open the pair on the preview surface (opFinishedMsg).
func (m Model) openPRCmd(p model.PullRequest) (Model, tea.Cmd) {
	svc := m.svc
	if svc == nil {
		return m, nil
	}
	m.statusMsg = i18n.T("fetching PR #%d…", p.Number)
	return m, func() tea.Msg {
		op, err := svc.PRFetchOp(context.Background(), p.Number)
		return prFetchReadyMsg{pr: p, op: op, err: err}
	}
}

// handlePRFetchReady runs the fetch and arms the open that follows it.
func (m Model) handlePRFetchReady(msg prFetchReadyMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = i18n.T("error: %s", firstLine(msg.err.Error()))
		return m, nil
	}
	if !m.opsIdle() {
		return m, nil // an op started while the resolve was in flight; the user can press enter again
	}
	pr := msg.pr
	m.pendingPROpen = &pr
	return m.startOp(msg.op)
}

// openPRPreviewCmd opens the fetched PR as a one-off ("show once") preview:
// head = gg's private ref, base = the PR's base sha when the forge gave one —
// a merged PR compared against its target BRANCH would be empty.
func (m Model) openPRPreviewCmd(p model.PullRequest) tea.Cmd {
	svc, gen, title := m.svc, m.previewGen, prTitle(p)
	return func() tea.Msg {
		ctx := context.Background()
		pair := svc.PRPair(ctx, p)
		eps, err := svc.PreviewOpen(ctx, pair.Head, pair.Base)
		msg := previewOpenMsg{source: pair.Head, target: pair.Base, gen: gen, eps: eps, err: err, title: title, prNumber: p.Number}
		if err == nil && eps.Summary.State == domain.PreviewOK {
			if set, serr := svc.PreviewNotes(ctx, pair.Head, pair.Base); serr == nil {
				msg.set = set
				msg.counts, _, _ = svc.PreviewNoteCounts(ctx, set)
			}
		}
		return msg
	}
}

// canForgetPR gates Forget: only a PR that is no longer open can be dropped —
// an open one is listed by the forge and would come straight back.
func (m Model) canForgetPR() bool {
	p, ok := m.selectedPR()
	return m.focus == panelPRs && ok && !p.IsOpen() && m.opsIdle()
}

// forgetPR drops gg's private ref for the selected PR and, with it, the row.
// Confirm-free: nothing of the user's is lost (the ref is refetchable, the
// forge is untouched). The list re-reads once the op lands.
func (m Model) forgetPR() (Model, tea.Cmd) {
	p, ok := m.selectedPR()
	if !ok || m.svc == nil {
		return m, nil
	}
	m.pendingPRsReload = true
	return m.startOp(m.svc.PRForgetOp(p.Number))
}

// copyPRURL puts the selected PR's web URL on the clipboard.
func (m Model) copyPRURL() (Model, tea.Cmd) {
	p, ok := m.selectedPR()
	if !ok {
		return m, nil
	}
	if p.URL == "" {
		m.statusMsg = i18n.T("this pull request has no URL")
		return m, nil
	}
	return m, m.copyToClipboardCmd(i18n.T("copied PR URL"), p.URL)
}
