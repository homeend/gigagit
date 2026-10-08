package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
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
}

// prLinkCmd resolves PR n's link pair.
func (m Model) prLinkCmd(n int) tea.Cmd {
	svc := m.svc
	if svc == nil || n == 0 {
		return nil
	}
	return func() tea.Msg {
		p, err := svc.PRLinkPair(context.Background(), n)
		return prLinkMsg{n: n, source: p.Head, target: p.Base, err: err}
	}
}

// handlePRLinkMsg copies the link the pair names.
func (m Model) handlePRLinkMsg(msg prLinkMsg) (Model, tea.Cmd) {
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
