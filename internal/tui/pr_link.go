package tui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// A pull request's gg:// link: <base>...refs/gg/pr/<n>, the merge-preview
// link of the pair its view opens on. Pasted to an agent (/gg-review <link>)
// it reviews the whole PR; opened (gg open, #) it lands in the PR's view.

// prLinkMsg is PR n's link pair, resolved off the UI thread (git resolves
// the base; a PR this session never saw is one forge read).
type prLinkMsg struct {
	n              int
	source, target string
	err            error
	gen            int // m.forgeGen when asked: a repo switch drops the answer
}

// prLinkCmd resolves PR n's link pair.
func (m Model) prLinkCmd(n int) tea.Cmd {
	svc, gen := m.svc, m.forgeGen
	if svc == nil || n == 0 {
		return nil
	}
	return func() tea.Msg {
		p, err := svc.PRLinkPair(context.Background(), n)
		return prLinkMsg{n: n, source: p.Head, target: p.Base, err: err, gen: gen}
	}
}

// handlePRLinkMsg copies the link the pair names.
func (m Model) handlePRLinkMsg(msg prLinkMsg) (Model, tea.Cmd) {
	if msg.gen != m.forgeGen {
		return m, nil // asked in a repository the TUI has left
	}
	switch {
	case errors.Is(msg.err, domain.ErrPRNotFetched):
		m.statusMsg = i18n.T("PR #%d is not fetched: press enter to open it first", msg.n)
		return m, nil
	case msg.err != nil:
		m.statusMsg = i18n.T("error: %s", firstLine(msg.err.Error()))
		return m, nil
	}
	link, ok := m.previewLinkFor(msg.source, msg.target, "", 0)
	if !ok {
		m.statusMsg = i18n.T("▸ no gg link for this place")
		return m, nil
	}
	return m, m.copyToClipboardCmd(i18n.T("copied link to PR #%d", msg.n), link)
}

// copySelectedPRLink is the Pull requests panel's L.
func (m Model) copySelectedPRLink() (Model, tea.Cmd) {
	p, ok := m.selectedPR()
	if !ok {
		return m, nil
	}
	return m, m.prLinkCmd(p.Number)
}

// openPRLinkRow is the open PR's "Copy pull request link": the pair on
// screen IS the link's, so no round trip.
func (m Model) openPRLinkRow() (actionRow, bool) {
	po, n := m.previewOpen, m.openPRNumber()
	if n == 0 || po == nil {
		return actionRow{}, false
	}
	return actionRow{id: "pr-link-open", label: i18n.T("Copy pull request link"), run: func(m Model) (tea.Model, tea.Cmd) {
		link, ok := m.previewLinkFor(po.source, po.target, "", 0)
		if !ok {
			m.statusMsg = i18n.T("▸ no gg link for this place")
			return m, nil
		}
		return m, m.copyToClipboardCmd(i18n.T("copied link to PR #%d", n), link)
	}}, true
}

// listedPR is PR n's row in the Pull requests list.
func (m Model) listedPR(n int) (model.PullRequest, bool) {
	for _, p := range m.prs {
		if p.Number == n {
			return p, true
		}
	}
	return model.PullRequest{}, false
}

// steerNavigatePR lands a PR link: the PR's view opens on its own pair (the
// link's base spelling may differ — the pending stage is gated by number),
// then the file and line when the link names one.
func (m Model) steerNavigatePR(c steer.Command, p model.PullRequest) (Model, tea.Cmd) {
	if c.File != "" && m.width > 0 && m.width < 60 {
		return m, m.answerSteer(c, steerFail(c, "the terminal is too narrow for the diff view"))
	}
	m = m.steerToPanels()
	if c.File == "" {
		if startAtOrigin(c) {
			m = m.steerNotice(i18n.T("▸ opened PR #%d", p.Number))
		} else {
			m = m.steerNotice(i18n.T("▸ agent moved the focus"))
		}
		nm, reply := m.navigateLanded(c, fmt.Sprintf("opened pull request #%d", p.Number))
		return nm, tea.Batch(reply, nm.openPRLandingCmd(p))
	}
	m.pendingSteer = &pendingSteer{cmd: c, stage: steerStagePreview, prNumber: p.Number, at: time.Now()}
	return m, m.openPRLandingCmd(p)
}

// openPRLandingCmd opens PR p for a link as a row click would: a head that
// is already local opens at once; otherwise the enter path fetches it first
// (prFetchReadyMsg → the fetch op → openPRPreviewCmd). An unrelated op does
// not block the first: only the fetch needs idle ops (handlePRFetchReady).
func (m Model) openPRLandingCmd(p model.PullRequest) tea.Cmd {
	svc, open := m.svc, m.openPRPreviewCmd(p)
	return func() tea.Msg {
		ctx := context.Background()
		if svc.PRFetched(ctx)[p.Number] {
			return open()
		}
		op, err := svc.PRFetchOp(ctx, p.Number)
		return prFetchReadyMsg{pr: p, op: op, err: err}
	}
}

// failPRLanding answers a PR link's parked landing when its open failed —
// never left to expire in silence.
func (m Model) failPRLanding(n int, reason string) (Model, tea.Cmd) {
	if ps := m.pendingSteer; ps == nil || ps.prNumber != n {
		return m, nil
	}
	return m.failPending(reason)
}

// restartPRLandingClock restarts PR n's parked landing's TTL: a fetch's
// network time must not count against steerPendingTTL.
func (m Model) restartPRLandingClock(n int) {
	if ps := m.pendingSteer; ps != nil && ps.prNumber == n {
		ps.at = time.Now()
	}
}
