package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// Review comments of the OPEN pull-request diff. domain.PRCommentsRefresh is
// the only network call on this path; the note loaders read its cache. It is
// fetched right after a PR's files view opens, on a heartbeat tick of its own
// (the diff view is a layer, and refreshTick is suppressed under any layer, so
// the list poll could never carry it), and on r inside a PR diff.

// prCommentsMsg lands when a comment read finishes. changed says the cached
// comments now differ, i.e. something on screen may be stale.
type prCommentsMsg struct {
	number  int
	changed bool
	manual  bool // the user pressed r: a failure is worth the status line
	err     error
}

// prCountsMsg carries the PR file list's refreshed per-path note counts.
type prCountsMsg struct {
	gen    int
	counts map[string]int
}

// openPRNumber is the pull request whose diff the files view shows (0 = none).
func (m Model) openPRNumber() int {
	if m.previewOpen == nil || m.filesView == nil {
		return 0
	}
	return m.previewOpen.prNumber
}

// prCommentsCmd re-reads the open PR's comments off the UI thread. One at a
// time; nil when no PR diff is open.
func (m Model) prCommentsCmd(manual bool) (Model, tea.Cmd) {
	n := m.openPRNumber()
	if n == 0 || m.svc == nil || m.prCommentsInflight {
		return m, nil
	}
	m.prCommentsInflight = true
	m.prCommentsLast = time.Now()
	svc := m.svc
	return m, func() tea.Msg {
		changed, err := svc.PRCommentsRefresh(context.Background(), n)
		return prCommentsMsg{number: n, changed: changed, manual: manual, err: err}
	}
}

// prCommentsTick is the comment poll: every [refresh] prs seconds (the PR
// list's own switch, 0 = off, min_seconds floor) while a PR diff is open. It
// is NOT refreshTick: that one stands down under any layer, and a diff IS a
// layer — which is exactly when these comments are on screen.
func (m Model) prCommentsTick(now time.Time) (Model, tea.Cmd) {
	if m.openPRNumber() == 0 || m.running || m.loading || m.modal != nil || m.prCommentsInflight {
		return m, nil
	}
	secs, on := scheduledInterval(m.cfg.Refresh, prsItem)
	if !on || now.Sub(m.prCommentsLast) < time.Duration(secs)*time.Second {
		return m, nil
	}
	m, cmd := m.prCommentsCmd(false)
	m.prCommentsLast = now
	return m, cmd
}

// handlePRCommentsMsg reloads what the comments feed — the file list's badges
// and, when a diff is up, its note boxes — but only when they changed. The
// notes arrival keeps the cursor, the scroll and the collapse state.
func (m Model) handlePRCommentsMsg(msg prCommentsMsg) (Model, tea.Cmd) {
	// Cleared FIRST, whatever the arrival is about: a read whose view closed
	// under it would otherwise leave the flag set and no later PR diff could
	// ever fetch its comments.
	m.prCommentsInflight = false
	if msg.number != m.openPRNumber() {
		return m, nil // that PR's diff is no longer the one on screen
	}
	if msg.err != nil {
		if msg.manual {
			m.statusMsg = i18n.T("PR comments: %s", firstLine(msg.err.Error()))
			m = m.noticeInDiff()
		}
		return m, nil
	}
	if !msg.changed {
		if msg.manual {
			m.statusMsg = i18n.T("PR comments are up to date")
			m = m.noticeInDiff()
		}
		return m, nil
	}
	cmds := []tea.Cmd{m.prCountsCmd()}
	if m.diffLayer() != nil {
		cmds = append(cmds, m.loadNotesCmd())
	}
	return m, tea.Batch(cmds...)
}

// noticeInDiff mirrors the status line into the diff view's own notice box —
// the full-screen diff hides the status bar, and r pressed there must answer.
func (m Model) noticeInDiff() Model {
	if m.diffLayer() != nil {
		m.diffNotice = m.statusMsg
	}
	return m
}

// prCountsCmd recounts the open preview's notes per path (store + forge).
func (m Model) prCountsCmd() tea.Cmd {
	if m.filesPreviewSet == nil || m.svc == nil {
		return nil
	}
	svc, set, gen := m.svc, *m.filesPreviewSet, m.previewGen
	return func() tea.Msg {
		counts, _, err := svc.PreviewNoteCounts(context.Background(), set)
		if err != nil {
			return nil
		}
		return prCountsMsg{gen: gen, counts: counts}
	}
}

// openPRHubFromDiff opens the hub for the PR whose diff is on screen: the row
// the list knows, else a bare number (the hub re-reads the PR anyway).
func (m Model) openPRHubFromDiff() (Model, tea.Cmd) {
	n := m.openPRNumber()
	if n == 0 {
		return m, nil
	}
	pr := model.PullRequest{Number: n}
	for _, p := range m.prs {
		if p.Number == n {
			pr = p
		}
	}
	return m.openPRHub(pr)
}
