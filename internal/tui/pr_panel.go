package tui

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
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

// prList is the panelList behind the Pull requests tab: Key is the PR number
// (stable across reorders and state changes), Name the title, Date the last
// update.
type prList struct {
	items []model.PullRequest
	text  []string
}

func (l prList) Len() int          { return len(l.items) }
func (l prList) Row(i int) string  { return l.text[i] }
func (l prList) Name(i int) string { return l.items[i].Title }
func (l prList) Date(i int) int64  { return l.items[i].Updated.Unix() }
func (l prList) Key(i int) string  { return strconv.Itoa(l.items[i].Number) }

// prReviewMark is the one-cell review verdict of an open PR. Narrow glyphs
// only: a wide one overflows the row in tmux (the ☰ lesson).
func prReviewMark(state string) string {
	switch state { // model.PullRequest.ReviewState is the forge's word, lower-cased
	case "approved":
		return "✓"
	case "changes_requested":
		return "✗"
	case "review_required":
		return "…"
	}
	return ""
}

// prStateWord is the translated state of a PR that is no longer open ("" for
// an open one). A known PR never leaves the list when it closes — it is
// re-marked with this word instead.
func prStateWord(state string) string {
	switch state {
	case model.PRStateMerged:
		return i18n.T("merged")
	case model.PRStateClosed:
		return i18n.T("closed")
	case model.PRStateUnavailable:
		return i18n.T("unavailable")
	}
	return ""
}

// prBranches is "source → target", a fork's head spelled owner:branch the way
// the forge does.
func prBranches(p model.PullRequest) string {
	src := p.Source
	if p.SourceRepo != "" {
		if owner, _, ok := strings.Cut(p.SourceRepo, "/"); ok {
			src = owner + ":" + src
		}
	}
	if src == "" && p.Target == "" {
		return ""
	}
	return src + " → " + p.Target
}

// prStatusCell is a row's status: the state word of a PR that is no longer
// open, else the review mark and "draft".
func prStatusCell(p model.PullRequest) string {
	if !p.IsOpen() {
		return prStateWord(p.State)
	}
	cell := prReviewMark(p.ReviewState)
	if p.Draft {
		if cell != "" {
			cell += " "
		}
		cell += i18n.T("draft")
	}
	return cell
}

// prRows renders "#N  <status>  title  author  source → target", the number
// and status columns padded to their widest cell. The status LEADS: the left
// column is narrow and cuts a row's tail, and "merged" or a review verdict is
// what a list row must never lose. Forge text is never translated; only
// "draft" and the state word are.
func (m Model) prRows() []string {
	nums := make([]string, len(m.prs))
	for i, p := range m.prs {
		nums[i] = "#" + strconv.Itoa(p.Number)
	}
	status := make([]string, len(m.prs))
	for i, p := range m.prs {
		status[i] = prStatusCell(p)
	}
	w, sw := maxLabelWidth(2, nums...), maxLabelWidth(0, status...)
	out := make([]string, 0, len(m.prs))
	for i, p := range m.prs {
		cells := []string{padCell(nums[i], w)}
		if sw > 0 {
			cells = append(cells, padCell(status[i], sw))
		}
		cells = append(cells, sanitizeRowText(p.Title))
		for _, c := range []string{p.Author, prBranches(p)} {
			if c != "" {
				cells = append(cells, c)
			}
		}
		out = append(out, strings.TrimRight(strings.Join(cells, "  "), " "))
	}
	return out
}

// sanitizeRowText keeps forge-provided text on one clean row: control
// characters (a title can hold a tab or an escape) become spaces.
func sanitizeRowText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}

// selectedPR is the focused row when the Pull requests tab has one.
func (m Model) selectedPR() (model.PullRequest, bool) {
	i, ok := m.backingIndex(panelPRs)
	if !ok || i >= len(m.prs) {
		return model.PullRequest{}, false
	}
	return m.prs[i], true
}

// prDecorators dims every row whose PR is no longer open; idx is the display
// → backing map panelViewWindowed returned.
func (m Model) prDecorators(idx []int) []rowDecorator {
	decos := make([]rowDecorator, len(idx))
	for j, i := range idx {
		if i >= 0 && i < len(m.prs) && !m.prs[i].IsOpen() {
			decos[j] = dimRowDecorator()
		}
	}
	return decos
}

// prErrText is the list failure as the user reads it: "github: <first line>".
func (m Model) prErrText() string {
	if m.prsErr == "" {
		return ""
	}
	return m.forgeProvider + ": " + m.prsErr
}

// emptyPanelText is what a panel with no rows says. Every panel says
// "(none)"; the Pull requests tab distinguishes a list still on its way, a
// failed read (with the key that retries it) and a repository with no open
// pull request.
func (m Model) emptyPanelText(p panel) string {
	if p == panelPRs {
		switch {
		case m.prsErr != "":
			return "  " + i18n.T("%s — [r] retry", m.prErrText())
		case !m.prsLoaded:
			return "  " + i18n.T("(loading…)")
		default:
			return "  " + i18n.T("(no open pull requests)")
		}
	}
	return i18n.T("  (none)")
}
