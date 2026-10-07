package tui

import (
	"context"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// Sending to GitHub from the TUI (spec 2026-10-07 §3.3, plan 3). Every send
// — a note, a group, a verdict, a reply, an agent's queued request — is
// planned by domain.PRSendOp OFF the Update goroutine (it reads under the
// repo gate, which is not re-entrant), then run with startOp; the op's one
// question (forge.send) is the confirm, which the TUI renders from the plan
// it holds (T4).

// forgeSendState is the send the TUI is running: the plan its confirm shows,
// the PR it writes to, the queued request it answers ("" = the user's own),
// and the verdict an agent asked for (preselected in the confirm).
type forgeSendState struct {
	pr        int
	plan      engine.SendPlan
	pendingID string
	event     string
}

// forgeSendReadyMsg carries the planned op (or why there is none) back.
type forgeSendReadyMsg struct {
	req       domain.PRSendRequest
	pendingID string
	op        engine.SendToForge
	err       error
}

// forgeSendCmd plans req off the UI thread.
func (m Model) forgeSendCmd(req domain.PRSendRequest, pendingID string) (Model, tea.Cmd) {
	svc := m.svc
	if svc == nil {
		return m, nil
	}
	if !m.opsIdle() {
		m = m.sayInDiff(i18n.T("another operation is running — send again when it ends"))
		return m, nil
	}
	m = m.sayInDiff(i18n.T("preparing the send to #%d…", req.PR))
	return m, func() tea.Msg {
		op, err := svc.PRSendOp(context.Background(), req)
		return forgeSendReadyMsg{req: req, pendingID: pendingID, op: op, err: err}
	}
}

// handleForgeSendReady starts the planned op, or says why it cannot. A
// queued request whose plan fails stays queued (it may work once the PR is
// fetched): only the op's outcome finishes it.
func (m Model) handleForgeSendReady(msg forgeSendReadyMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		return m.sayInDiff(i18n.T("send: %s", firstLine(msg.err.Error()))), nil
	}
	if !m.opsIdle() {
		return m.sayInDiff(i18n.T("another operation is running — send again when it ends")), nil
	}
	m.forgeSend = &forgeSendState{pr: msg.req.PR, plan: msg.op.Plan, pendingID: msg.pendingID, event: msg.req.Event}
	return m.startOp(msg.op)
}

// sayInDiff puts msg on the status line and, while a diff is on top (it has
// no status bar), in the diff's notice box.
func (m Model) sayInDiff(msg string) Model {
	m.statusMsg = msg
	if m.diffLayer() != nil {
		m.diffNotice = "▸ " + msg
	}
	return m
}

// The confirm's caps (Review Focus 4): body lines, then item + skip rows.
const (
	sendConfirmBodyRows = 6
	sendConfirmRows     = 12
)

// sendConfirmText is the forge.send prompt in the user's language: target,
// body, every item, every skipped item with its reason — what will be posted.
func sendConfirmText(p engine.SendPlan) string {
	var b []string
	switch p.Mode {
	case engine.SendFinish:
		b = append(b, i18n.T("Finish sending to %s:", p.Target))
	case engine.SendDiscard:
		b = append(b, i18n.T("Discard gg's pending review on %s:", p.Target))
	default:
		b = append(b, i18n.T("Send to %s:", p.Target))
	}
	if p.Pending != "" && p.Mode == engine.SendReview {
		b = append(b, i18n.T("You have a review pending on GitHub: these comments join it and it is submitted."))
	}
	if body := p.BodyText(); body != "" {
		b = append(b, i18n.T("review body:"))
		for i, l := range strings.Split(body, "\n") {
			if i == sendConfirmBodyRows {
				b = append(b, "    …")
				break
			}
			b = append(b, "    "+l)
		}
	}
	var rows []string
	for _, it := range p.Items {
		rows = append(rows, "  + "+sendItemText(p.Mode, it))
	}
	for _, sk := range p.Skipped {
		rows = append(rows, "  - "+sendSkipText(sk))
	}
	if len(rows) > sendConfirmRows {
		more := len(rows) - sendConfirmRows
		rows = append(rows[:sendConfirmRows], "  "+i18n.T("+ %d more", more))
	}
	return strings.Join(append(b, rows...), "\n")
}

// sendWhere is "path:line" or "path (file)": data plus one translated word.
func sendWhere(path string, line int) string {
	if line == 0 {
		return path + " " + i18n.T("(file)")
	}
	return path + ":" + strconv.Itoa(line)
}

func sendItemText(mode engine.SendMode, it engine.SendItem) string {
	if mode == engine.SendFinish || mode == engine.SendDiscard {
		return i18n.T("%s (waiting in the pending review)", it.Key)
	}
	switch it.Kind {
	case engine.SendReply:
		return i18n.T("reply: %s", it.Summary)
	case engine.SendResolve:
		return i18n.T("resolve thread %s", it.ThreadID)
	case engine.SendUnresolve:
		return i18n.T("reopen thread %s", it.ThreadID)
	}
	line := it.Thread.StartLine
	if line == 0 {
		line = it.Thread.Line
	}
	s := sendWhere(it.Thread.Path, line) + " " + it.Summary
	switch n := len(it.Replies); {
	case n == 1:
		s += " " + i18n.T("(1 reply)")
	case n > 1:
		s += " " + i18n.T("(%d replies)", n)
	}
	if it.Resolve {
		s += " · " + i18n.T("resolved after sending")
	}
	return s
}

func sendSkipText(sk engine.SendSkip) string {
	what := sk.Summary
	switch {
	case sk.Path != "":
		what = sendWhere(sk.Path, sk.Line) + " " + sk.Summary
	case sk.Reason == domain.SkipOnGitHub:
		what = i18n.T("review summary")
	}
	return what + " " + sendSkipReasonText(sk.Reason)
}

// sendSkipReasonText is one reason code in words; an unknown code (a newer
// domain) shows as data in a generic frame.
func sendSkipReasonText(reason string) string {
	switch reason {
	case domain.SkipNotInPR:
		return i18n.T("(skipped: not in this PR)")
	case domain.SkipLinesChanged:
		return i18n.T("(skipped: its lines changed)")
	case domain.SkipBeingSent:
		return i18n.T("(skipped: already being sent)")
	case domain.SkipOnGitHub:
		return i18n.T("(skipped: already on GitHub)")
	case domain.SkipGone:
		return i18n.T("(skipped: it no longer exists)")
	case domain.SkipThreadNotInPR:
		return i18n.T("(skipped: its thread is not in this PR)")
	}
	return i18n.T("(skipped: %s)", reason)
}

// forgeSendFinished is the op's follow-up (opFinishedMsg): answer the queued
// request, re-read the PR (its threads now hold what was sent) and recount
// the badges. The notes themselves reload through srcNotes (the op's
// opAffectedSources), whose arrival re-resolves the open diff's boxes.
func (m Model) forgeSendFinished(fs *forgeSendState, res engine.Result, err error) (Model, tea.Cmd) {
	var cmds []tea.Cmd
	if fs.pendingID != "" {
		cmds = append(cmds, m.pendingFinishCmd(fs.pendingID, res, err))
	}
	if fs.pr != 0 && fs.pr == m.openPRNumber() {
		var c tea.Cmd
		m, c = m.prRefreshCmd(fs.pr, false)
		cmds = append(cmds, c, m.prCountsCmd())
	}
	return m, tea.Batch(cmds...)
}
