package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// Stale-while-revalidate for a pull request's diff. domain's PR cache lets
// enter open a PR from what is local — the listing carried its head sha, so
// an unchanged PR costs no forge call and no network fetch. The price is a
// head that moved since the last listing; prRevalidateCmd pays it AFTER the
// view is up: one forge read, and only a moved head re-runs the open chain.

// prRevalidateBudget bounds that one forge read.
const prRevalidateBudget = 30 * time.Second

type prRevalidatedMsg struct {
	n     int
	pr    model.PullRequest
	moved bool
	err   error
}

// prRevalidateCmd asks the forge about PR n off the UI thread. The reopen a
// revalidation itself caused consumes prRevalidateSkip instead of asking
// again: a fetch that cannot reach the forge's head must not loop.
func (m Model) prRevalidateCmd(n int) (Model, tea.Cmd) {
	if m.prRevalidateSkip == n {
		m.prRevalidateSkip = 0
		return m, nil
	}
	if m.svc == nil || m.prRevalidateInflight {
		return m, nil
	}
	m.prRevalidateInflight = true
	svc := m.svc
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), prRevalidateBudget)
		defer cancel()
		rv, err := svc.PRRevalidate(ctx, n)
		return prRevalidatedMsg{n: n, pr: rv.PR, moved: rv.Moved, err: err}
	}
}

func (m Model) handlePRRevalidatedMsg(msg prRevalidatedMsg) (Model, tea.Cmd) {
	m.prRevalidateInflight = false // cleared BEFORE the "still my view" checks
	if msg.err != nil {
		return m, nil // offline, rate-limited: the diff on screen stands
	}
	for i := range m.prs { // the row follows the forge (merged, closed, retitled)
		if m.prs[i].Number == msg.n {
			m.prs[i] = msg.pr
		}
	}
	if !msg.moved || m.openPRNumber() != msg.n || !m.opsIdle() {
		return m, nil // unchanged, or the user moved on: the next enter fetches
	}
	m.prRevalidateSkip = msg.n
	var cmd tea.Cmd
	m, cmd = m.openPRCmd(msg.pr)
	m.statusMsg = i18n.T("PR #%d has new commits — updating…", msg.n)
	return m.noticeInDiff(), cmd
}
