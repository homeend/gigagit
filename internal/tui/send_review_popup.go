package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// sendReviewPopup is the review body before a send (spec §3.5): Send review
// (a group's notes as one GitHub review) or Verdict (no threads). The verdict
// itself is the op's confirm.
type sendReviewPopup struct {
	popupMax
	pr      int
	group   string // domain.GroupMine or "review:<id>"; "" with verdict
	verdict bool
	body    textfield
	scroll  int
}

// sendGroupsMsg is a PR's groups with something to send.
type sendGroupsMsg struct {
	pr     int
	groups []domain.SendGroup
	err    error
}

// sendBodyMsg is an AI review's summary, read to prefill the body.
type sendBodyMsg struct {
	pr          int
	group, body string
	err         error
}

func (m Model) openSendReview(pr int) (Model, tea.Cmd) {
	svc := m.svc
	if svc == nil || pr == 0 {
		return m, nil
	}
	return m, func() tea.Msg {
		gs, err := svc.PRSendGroups(context.Background(), pr)
		return sendGroupsMsg{pr: pr, groups: gs, err: err}
	}
}

func (m Model) handleSendGroups(msg sendGroupsMsg) (Model, tea.Cmd) {
	switch {
	case msg.err != nil:
		return m.sayInDiff(i18n.T("send: %s", firstLine(msg.err.Error()))), nil
	case len(msg.groups) == 0:
		return m.sayInDiff(i18n.T("nothing to send to #%d", msg.pr)), nil
	case len(msg.groups) == 1:
		return m.openSendReviewBody(msg.pr, msg.groups[0].ID)
	}
	groups := msg.groups
	opts := sendGroupOptions(groups)
	m.modal = &decisionState{
		req: engine.DecisionRequest{ID: "pr-send-group", Prompt: i18n.T("Send which review to #%d?", msg.pr),
			Options: sendGroupOptions(groups)}, // dynamic by nature: the groups' own words
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			for i, g := range groups {
				if opt == opts[i] {
					return m.openSendReviewBody(msg.pr, g.ID)
				}
			}
			return m, nil
		},
	}
	return m, nil
}

// sendGroupOptions are the chooser's rows plus the trailing Cancel esc maps to.
func sendGroupOptions(groups []domain.SendGroup) []string {
	opts := make([]string, 0, len(groups)+1)
	for _, g := range groups {
		opts = append(opts, sendGroupLabel(g))
	}
	return append(opts, "Cancel")
}

// sendGroupLabel is one chooser row: "my draft review · 2 notes" or
// "claude: two nits · 3 remarks".
func sendGroupLabel(g domain.SendGroup) string {
	if g.ID == domain.GroupMine {
		if g.Count == 1 {
			return i18n.T("my draft review · 1 note")
		}
		return i18n.T("my draft review · %d notes", g.Count)
	}
	sum := truncate(sanitizeLine(g.Summary), 40)
	if g.Count == 1 {
		return i18n.T("%s: %s · 1 remark", g.Agent, sum)
	}
	return i18n.T("%s: %s · %d remarks", g.Agent, sum, g.Count)
}

func (m Model) openSendReviewBody(pr int, group string) (Model, tea.Cmd) {
	id, isReview := strings.CutPrefix(group, "review:")
	if !isReview || m.svc == nil {
		return m.pushLayer(&sendReviewPopup{pr: pr, group: group, body: newTextField("")}), nil
	}
	svc := m.svc
	return m, func() tea.Msg {
		body, err := svc.ReviewBodyText(context.Background(), id)
		return sendBodyMsg{pr: pr, group: group, body: body, err: err}
	}
}

func (m Model) handleSendBody(msg sendBodyMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		return m.sayInDiff(i18n.T("send: %s", firstLine(msg.err.Error()))), nil
	}
	return m.pushLayer(&sendReviewPopup{pr: msg.pr, group: msg.group, body: newTextField(msg.body)}), nil
}

func (m Model) openVerdict(pr int) (Model, tea.Cmd) {
	if pr == 0 {
		return m, nil
	}
	return m.pushLayer(&sendReviewPopup{pr: pr, verdict: true, body: newTextField("")}), nil
}

// request is what ctrl+s sends.
func (p *sendReviewPopup) request() domain.PRSendRequest {
	req := domain.PRSendRequest{PR: p.pr, Body: strings.TrimSpace(p.body.Value())}
	switch {
	case p.verdict:
		req.Verdict = true
	case p.group == domain.GroupMine:
		req.Mine = true
	default:
		req.Review = strings.TrimPrefix(p.group, "review:")
	}
	return req
}

func (p *sendReviewPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyCtrlS:
		m = m.popLayer()
		return m.forgeSendCmd(p.request(), "")
	case tea.KeyEnter:
		p.body.InsertNewline()
		return m, nil
	case tea.KeyUp:
		p.body.Up()
		return m, nil
	case tea.KeyDown:
		p.body.Down()
		return m, nil
	}
	p.body.HandleEditKey(msg)
	return m, nil
}

func (p *sendReviewPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *sendReviewPopup) box(m Model) string {
	w, _ := m.overlayDims()
	innerW := popupResolveWidth(w, p.maximized, commitNormalWidth(w))
	contentW := popupTextWidth(innerW)
	heading := i18n.T("Send review to #%d", p.pr)
	what := i18n.T("my draft review")
	switch {
	case p.verdict:
		heading, what = i18n.T("Verdict on #%d", p.pr), i18n.T("no comments, only the verdict")
	case p.group != domain.GroupMine:
		what = i18n.T("an AI review: its summary is the review body")
	}
	var b strings.Builder
	b.WriteString(heading + "\n" + what + "\n\n")
	b.WriteString(viewFieldWindow("> "+i18n.T("body:")+" ", p.body, true, contentW, 10, &p.scroll) + "\n")
	b.WriteString(i18n.T("the next step asks for the verdict: comment, approve or request changes") + "\n")
	b.WriteString("\n" + packHints([]string{
		i18n.T("[enter] newline"),
		i18n.T("[ctrl+s] continue"),
		i18n.T("[ctrl+t] fullscreen"),
		i18n.T("[esc] cancel"),
	}, contentW))
	return popupBox(innerW, b.String())
}
