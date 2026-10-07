package tui

import (
	"context"
	"time"

	"github.com/homeend/gigagit/internal/clock"

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
	n               int
	gen             int // m.prsGen when the read started: a repo switch drops it
	pr              model.PullRequest
	moved           bool
	commentsChanged bool
	manual          bool      // the user pressed r: answer on the status line
	readAt          time.Time // when the PR was last read (the offline mark's age)
	err             error
}

// prRevalidateCmd asks the forge about PR n off the UI thread. The reopen a
// revalidation itself caused consumes prRevalidateSkip instead of asking
// again: a fetch that cannot reach the forge's head must not loop.
func (m Model) prRevalidateCmd(n int) (Model, tea.Cmd) {
	if m.prRevalidateSkip == n {
		m.prRevalidateSkip = 0
		return m, nil
	}
	return m.prRefreshCmd(n, false)
}

// prRefreshCmd is THE forge read of an open PR: one call answers whether its
// head moved AND re-reads its comments (domain.PRRevalidate). One at a time;
// it marks the view "refreshing…" until the answer lands.
func (m Model) prRefreshCmd(n int, manual bool) (Model, tea.Cmd) {
	if n == 0 || m.svc == nil || m.prRevalidateInflight || m.prCommentsInflight {
		return m, nil
	}
	m.prRevalidateInflight, m.prCommentsInflight, m.prRefreshing = true, true, true
	m.prCommentsLast = time.Now()
	svc, gen := m.svc, m.prsGen
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), prRevalidateBudget)
		defer cancel()
		rv, err := svc.PRRevalidate(ctx, n)
		msg := prRevalidatedMsg{n: n, gen: gen, pr: rv.PR, moved: rv.Moved, commentsChanged: rv.CommentsChanged,
			manual: manual, readAt: rv.ReadAt, err: err}
		if err != nil {
			msg.readAt, _ = svc.PRCacheReadAt(n)
		}
		return msg
	}
}

func (m Model) handlePRRevalidatedMsg(msg prRevalidatedMsg) (Model, tea.Cmd) {
	// Cleared BEFORE the "still my view" checks: a read whose view closed
	// under it must not block every later one.
	m.prRevalidateInflight, m.prRefreshing = false, false
	if msg.gen != m.prsGen {
		m.prCommentsInflight = false
		return m, nil // another repository's answer
	}
	if msg.err != nil {
		m.prOfflineSince = msg.readAt
		if m.prOfflineSince.IsZero() {
			m.prOfflineSince = clock.Now()
		}
	} else {
		m.prOfflineSince = time.Time{}
	}
	// The comment half (reloads notes and counts on a change; answers r).
	m, cmd := m.handlePRCommentsMsg(prCommentsMsg{number: msg.n, changed: msg.commentsChanged, manual: msg.manual, err: msg.err})
	if msg.err != nil {
		return m, cmd // offline, rate-limited: the diff on screen stands
	}
	for i := range m.prs { // the row follows the forge (merged, closed, retitled)
		if m.prs[i].Number == msg.n {
			m.prs[i] = msg.pr
		}
	}
	// "Moved" is judged against the head ON SCREEN too: a background prefetch
	// may already have fetched the new head into the local ref, and then the
	// domain (which compares with that ref) says nothing moved.
	moved := msg.moved
	if po := m.previewOpen; po != nil && po.prNumber == msg.n && msg.pr.HeadSHA != "" &&
		po.srcHash != "" && po.srcHash != msg.pr.HeadSHA {
		moved = true
	}
	if moved || msg.commentsChanged {
		m.prUpdated = msg.n
	} else if m.prUpdated == msg.n {
		m.prUpdated = 0 // nothing new: the mark clears (spec §2.4)
	}
	if !moved || m.openPRNumber() != msg.n || !m.opsIdle() {
		return m, cmd // unchanged, or the user moved on: the next enter fetches
	}
	m.prRevalidateSkip = msg.n
	r := &prReland{n: msg.n, path: m.previewSelectedPath()}
	if v := m.diffLayer(); v != nil {
		r.diff, r.land = true, v.cursorLineLanding()
	}
	m.prReland = r
	var open tea.Cmd
	m, open = m.openPRCmd(msg.pr)
	m.statusMsg = i18n.T("PR #%d has new commits — updating…", msg.n)
	return m.noticeInDiff(), tea.Batch(cmd, open)
}

// prFreshnessSuffix is the open PR's title tail: "refreshing…" while the
// forge is asked, the cached copy's age while the forge cannot be reached.
func (m Model) prFreshnessSuffix() string {
	if m.openPRNumber() == 0 {
		return ""
	}
	switch {
	case m.prRefreshing:
		return " · " + i18n.T("refreshing…")
	case !m.prOfflineSince.IsZero():
		return " · " + i18n.T("offline · read %s", ageString(clock.Now(), m.prOfflineSince))
	case m.prUpdated == m.openPRNumber():
		return " · " + i18n.T("updated")
	}
	return ""
}

// prReland is the place a moved-head reopen gives back: the file under the
// files cursor and, when a diff was open, that diff at its line.
type prReland struct {
	n    int
	path string
	diff bool
	land *lineLanding // nil when the diff cursor had no numbered line
}

// relandPR consumes the open PR's prReland once its reopened file list is in:
// the cursor already sits on path (previewOpenState.keepPath); an open diff
// reopens there, at its line. ok reports that it opened the diff.
func (m Model) relandPR() (tea.Model, tea.Cmd, bool) {
	r := m.prReland
	if r == nil || m.openPRNumber() != r.n {
		return m, nil, false
	}
	m.prReland = nil
	vis := m.filesView.visible()
	found := false
	for i, l := range vis {
		if l.path == r.path && r.path != "" {
			m.filesView.sel, found = i, true
			break
		}
	}
	if !r.diff || !found {
		return m, nil, false
	}
	tm, cmd := m.openDiffForFileLine(vis[m.filesView.sel])
	tm, cmd = m.withLineLanding(r.land, tm, cmd)
	return tm, cmd, true
}
