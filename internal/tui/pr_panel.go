package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// prsLoadedMsg is the one event a pull-request list read produces. It carries
// the forge status too: the FIRST read is also the session's one forge probe
// (domain caches it), and an available status is what reveals the tab.
type prsLoadedMsg struct {
	gen    int // ties the result to the read that issued it; a stale gen is dropped
	status domain.ForgeStatus
	prs    []model.PullRequest
	err    error // the list read failed although a forge is usable
	dur    time.Duration
	bg     bool // issued by the background lane (its arrival frees the lane)
	manual bool // the user asked (r): a failure is worth the status line
}

// readPRsCmd reads the forge status and, when a forge is usable, the
// pull-request list — off the UI thread, and outside the source registry: a gh
// call may take its whole 30 s timeout, and must never hold m.loading or the r
// key hostage (see prsItem). It marks the read in flight; the prsLoadedMsg arm
// clears it. nil cmd when there is no service or a read is already running.
func (m Model) readPRsCmd(ctx context.Context, bg, manual bool) (Model, tea.Cmd) {
	if m.svc == nil || m.prsInflight {
		return m, nil
	}
	m.prsGen++
	m.prsInflight = true
	svc, gen := m.svc, m.prsGen
	return m, func() tea.Msg {
		start := time.Now()
		out := prsLoadedMsg{gen: gen, bg: bg, manual: manual}
		out.status = svc.ForgeStatus(ctx)
		if out.status.Available() {
			out.prs, out.err = svc.PullRequests(ctx)
		}
		out.dur = time.Since(start)
		return out
	}
}

// kickForgeProbe dispatches this repo session's one startup read: the forge
// probe plus, when it succeeds, the first list. Idempotent per repo (reRoot
// re-arms it); silent whatever the outcome.
func (m Model) kickForgeProbe() (Model, tea.Cmd) {
	if m.forgeProbeKicked || m.svc == nil || domain.ForgeDisabled {
		return m, nil
	}
	m.forgeProbeKicked = true
	if m.refreshLastRun == nil {
		m.refreshLastRun = map[refreshItem]time.Time{}
	}
	m.refreshLastRun[prsItem] = time.Now()
	return m.readPRsCmd(context.Background(), false, false)
}

// handlePRsLoaded stores a pull-request read. The tab appears on the first
// usable status and then stays for the repo session: a later failure keeps the
// previous list and becomes an error row, never a vanishing tab.
func (m Model) handlePRsLoaded(msg prsLoadedMsg) (Model, tea.Cmd) {
	if msg.gen != m.prsGen {
		return m, nil // superseded (a repo switch, a newer read)
	}
	m.prsInflight = false
	if msg.bg && m.bgBusy && m.bgActiveItem.isPRs {
		m.bgBusy = false
	}
	cancelled := errors.Is(msg.err, context.Canceled) || errors.Is(msg.status.Err, context.Canceled)
	if cancelled {
		return m, nil // pre-empted by a user op: not a failure, and nothing new to show
	}
	if !msg.status.Available() {
		if m.forgeShown { // the probe is cached per session, so this is defensive
			m.prsErr = firstLine(errText(msg.status.Err))
		}
		return m, nil // no usable forge: the feature simply is not there (no tab, no notice)
	}
	m.forgeShown = true
	m.forgeProvider = msg.status.Provider
	if msg.err != nil {
		m.prsErr = firstLine(msg.err.Error())
		if msg.manual {
			m.statusMsg = m.forgeProvider + ": " + m.prsErr
		}
		return m, nil
	}
	if msg.bg {
		m = m.recordDuration(prsItem, msg.dur)
	}
	key := m.panelSelKey(panelPRs)
	m.prs, m.prsErr, m.prsLoaded = msg.prs, "", true
	m = m.restorePanelSel(panelPRs, key)
	return m, nil
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// firstLine is the first non-empty line of s, trimmed: gh's failures are
// multi-line and a row holds one.
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}
