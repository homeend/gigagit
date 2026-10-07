package tui

import (
	"context"
	"sort"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// An interrupted send (spec §3.4): gg stopped after GitHub opened its pending
// review. Each refresh of a PR asks; the notice offers to finish or discard.

type interruptedSend struct {
	review string
	keys   []string
	joined bool
}

type interruptedMsg struct {
	gen    int
	pr     int
	review string
	keys   []string
	joined bool
}

func (m Model) interruptedCmd(pr int) tea.Cmd {
	svc, gen := m.svc, m.noticeGen
	if svc == nil || pr == 0 {
		return nil
	}
	return func() tea.Msg {
		rev, keys, joined := svc.PRInterrupted(context.Background(), pr)
		return interruptedMsg{gen: gen, pr: pr, review: rev, keys: keys, joined: joined}
	}
}

func (m Model) handleInterrupted(msg interruptedMsg) (Model, tea.Cmd) {
	if msg.gen != m.noticeGen {
		return m, nil
	}
	prev := m.noticeIDs()
	if msg.review == "" {
		delete(m.interrupted, msg.pr)
	} else {
		if m.interrupted == nil {
			m.interrupted = map[int]interruptedSend{}
		}
		m.interrupted[msg.pr] = interruptedSend{review: msg.review, keys: msg.keys, joined: msg.joined}
	}
	m = m.rebuildNotices()
	return m.armBlinkForNew(prev)
}

func interruptedSendNotices(m Model) []notice {
	prs := make([]int, 0, len(m.interrupted))
	for n := range m.interrupted {
		prs = append(prs, n)
	}
	sort.Ints(prs)
	var out []notice
	for _, n := range prs {
		n, s := n, m.interrupted[n]
		title := i18n.T("A send to #%d was interrupted: 1 comment waits in a pending review on GitHub", n)
		if len(s.keys) != 1 {
			title = i18n.T("A send to #%d was interrupted: %d comments wait in a pending review on GitHub", n, len(s.keys))
		}
		acts := []noticeAction{{label: i18n.T("Finish sending"), sourced: true, run: func(m Model) (Model, tea.Cmd) {
			return m.forgeSendCmd(domain.PRSendRequest{PR: n, Finish: true})
		}}}
		if !s.joined {
			acts = append(acts, noticeAction{label: i18n.T("Discard"), sourced: true, run: func(m Model) (Model, tea.Cmd) {
				return m.forgeSendCmd(domain.PRSendRequest{PR: n, Discard: true})
			}})
		}
		acts = append(acts, noticeAction{label: i18n.T("Later"), sourced: true})
		out = append(out, notice{id: "interrupted_send_" + strconv.Itoa(n), repoKey: m.repoHealth.GitCommonDir,
			title: title, detail: []string{i18n.T("Nothing else of it is visible on GitHub until it is submitted.")}, actions: acts})
	}
	return out
}
