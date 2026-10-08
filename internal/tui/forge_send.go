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
// — a note, a group, a verdict, a reply — is
// planned by domain.PRSendOp OFF the Update goroutine (it reads under the
// repo gate, which is not re-entrant), then run with startOp; the op's one
// question (forge.send) is the confirm, which the TUI renders from the plan
// it holds (T4).

// forgeSendState is the send the TUI is running: the plan its confirm shows
// and the PR it writes to. Agents never send (user ruling 2026-10-08): every
// send here is the user's own.
type forgeSendState struct {
	pr   int
	plan engine.SendPlan
}

// forgeSendReadyMsg carries the planned op (or why there is none) back.
type forgeSendReadyMsg struct {
	gen int // m.forgeSendGen when the plan started: a repo switch drops it
	req domain.PRSendRequest
	op  engine.SendToForge
	err error
}

// forgeSendCmd plans req off the UI thread.
func (m Model) forgeSendCmd(req domain.PRSendRequest) (Model, tea.Cmd) {
	svc := m.svc
	if svc == nil {
		return m, nil
	}
	if !m.opsIdle() {
		m = m.sayInDiff(i18n.T("another operation is running — send again when it ends"))
		return m, nil
	}
	m = m.sayInDiff(i18n.T("preparing the send to #%d…", req.PR))
	gen := m.forgeSendGen
	return m, func() tea.Msg {
		op, err := svc.PRSendOp(context.Background(), req)
		return forgeSendReadyMsg{gen: gen, req: req, op: op, err: err}
	}
}

// handleForgeSendReady starts the planned op, or says why it cannot.
func (m Model) handleForgeSendReady(msg forgeSendReadyMsg) (Model, tea.Cmd) {
	if msg.gen != m.forgeSendGen {
		return m, nil // planned in the repository before R
	}
	if m.modal != nil { // its op's question would replace the open dialog
		return m.sendDialogBusy(), nil
	}
	if msg.err != nil {
		if k := m.keptSendBody; k != nil && k.pr == msg.req.PR {
			return m.sayInDiff(i18n.T("send: %s — the text you typed is kept", firstLine(msg.err.Error()))), nil
		}
		return m.sayInDiff(i18n.T("send: %s", firstLine(msg.err.Error()))), nil
	}
	if !m.opsIdle() {
		return m.sayInDiff(i18n.T("another operation is running — send again when it ends")), nil
	}
	m.forgeSend = &forgeSendState{pr: msg.req.PR, plan: msg.op.Plan}
	return m.startOp(msg.op)
}

// sendDialogBusy drops a send step that answered while another dialog was
// open: replacing that dialog could leave its op waiting forever.
func (m Model) sendDialogBusy() Model {
	return m.sayInDiff(i18n.T("send cancelled (another dialog opened) — send again"))
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
func sendConfirmText(p engine.SendPlan) string { return sendConfirmTextFit(p, 0, 0) }

// sendConfirmTextFit is sendConfirmText laid out for a modal w columns wide
// with room for maxRows prompt rows (0 = no limit): the body and the rows are
// counted AFTER wrapping, so a long AI overview never pushes the target or
// the options off the screen (Review Focus 4). Item rows are cut, not wrapped.
func sendConfirmTextFit(p engine.SendPlan, w, maxRows int) string {
	wrap := func(s string) []string {
		if w <= 0 {
			return []string{s}
		}
		if ws := wrapWords(s, w); len(ws) > 0 {
			return ws
		}
		return []string{""}
	}
	var out []string
	switch p.Mode {
	case engine.SendFinish:
		out = append(out, wrap(i18n.T("Finish sending to %s:", p.Target))...)
	case engine.SendDiscard:
		out = append(out, wrap(i18n.T("Discard gg's pending review on %s:", p.Target))...)
	default:
		out = append(out, wrap(i18n.T("Send to %s:", p.Target))...)
	}
	if p.Pending != "" && p.Mode == engine.SendReview {
		out = append(out, wrap(i18n.T("You have a review pending on GitHub: these comments join it and it is submitted."))...)
	}
	budget := maxRows - len(out)
	if maxRows <= 0 {
		budget = 1 << 30
	}
	if body := p.BodyText(); body != "" && budget > 3 {
		out = append(out, i18n.T("review body:"))
		limit := min(sendConfirmBodyRows, max(1, budget/3))
		var rows []string
		for _, l := range strings.Split(body, "\n") {
			bw := w - 4
			if w <= 0 {
				bw = 0
			}
			if bw > 0 {
				ws := wrapWords(l, bw)
				if len(ws) == 0 {
					ws = []string{""}
				}
				for _, x := range ws {
					rows = append(rows, "    "+x)
				}
			} else {
				rows = append(rows, "    "+l)
			}
		}
		if len(rows) > limit {
			rows = append(rows[:limit], "    …")
		}
		out = append(out, rows...)
		budget -= len(rows) + 1
	}
	var rows []string
	for _, it := range p.Items {
		rows = append(rows, "  + "+sendItemText(p.Mode, it))
	}
	for _, sk := range p.Skipped {
		rows = append(rows, "  - "+sendSkipText(sk))
	}
	limit := min(sendConfirmRows, max(1, budget-1))
	if len(rows) > limit {
		more := len(rows) - limit
		rows = append(rows[:limit], "  "+i18n.T("+ %d more", more))
	}
	for _, r := range rows {
		if w > 0 {
			r = truncate(r, w)
		}
		out = append(out, r)
	}
	return strings.Join(out, "\n")
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
		what := it.Summary
		if what == "" {
			what = it.Key
		}
		return i18n.T("%s (waiting in the pending review)", what)
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
		s = i18n.T("%s · resolved after sending", s)
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
	return sendSkipReasonText(what, sk.Reason)
}

// sendSkipReasonText is one skipped row: what, then why — one format per
// reason, so a translation orders the whole row (F10). An unknown code (a
// newer domain) shows as data in a generic frame.
func sendSkipReasonText(what, reason string) string {
	switch reason {
	case domain.SkipNotInPR:
		return i18n.T("%s (skipped: not in this PR)", what)
	case domain.SkipLinesChanged:
		return i18n.T("%s (skipped: its lines changed)", what)
	case domain.SkipBeingSent:
		return i18n.T("%s (skipped: already being sent)", what)
	case domain.SkipOnGitHub:
		return i18n.T("%s (skipped: already on GitHub)", what)
	case domain.SkipGone:
		return i18n.T("%s (skipped: it no longer exists)", what)
	case domain.SkipThreadNotInPR:
		return i18n.T("%s (skipped: its thread is not in this PR)", what)
	}
	return i18n.T("%[1]s (skipped: %[2]s)", what, reason)
}

// forgeSendFinished is the op's follow-up (opFinishedMsg): re-read the PR (its threads now hold what was sent) and recount
// the badges. The notes themselves reload through srcNotes (the op's
// opAffectedSources), whose arrival re-resolves the open diff's boxes.
func (m Model) forgeSendFinished(fs *forgeSendState, res engine.Result, err error) (Model, tea.Cmd) {
	var cmds []tea.Cmd
	if err == nil && res.Changed && fs.pr != 0 { // my own change (F1): not "updated"
		m.prOwnSend, m.prOwnSendSeq = fs.pr, m.prReadSeq
		m.keptSendBody = nil // the typed body reached GitHub
	}
	if fs.pr != 0 && fs.pr == m.openPRNumber() {
		var c tea.Cmd
		m, c = m.prRefreshCmd(fs.pr, false)
		if c == nil && (m.prRevalidateInflight || m.prCommentsInflight) {
			m.prRefreshAgain = fs.pr // one read at a time: ask again when it lands
		}
		cmds = append(cmds, c, m.prCountsCmd())
	}
	// Whatever PR is open: a finished or discarded interrupted send must
	// take its notice along (it is only re-asked on that PR's refresh).
	cmds = append(cmds, m.interruptedCmd(fs.pr))
	return m, tea.Batch(cmds...)
}
