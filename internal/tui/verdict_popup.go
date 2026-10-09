package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// verdictPopup is Verdict… (spec §3.5, R7): a review with no threads — the
// body box, then the op's confirm picks comment / approve / request
// changes. Everything with comments goes through the send panel
// (send_panel.go).
type verdictPopup struct {
	popupMax
	pr     int
	body   textfield
	scroll int
}

func (m Model) openVerdict(pr int) (Model, tea.Cmd) {
	if pr == 0 {
		return m, nil
	}
	kept, _ := m.keptBodyFor(pr, "", true)
	return m.pushLayer(&verdictPopup{pr: pr, body: newTextField(kept)}), nil
}

// request is what ctrl+s sends.
func (p *verdictPopup) request() domain.PRSendRequest {
	return domain.PRSendRequest{PR: p.pr, Verdict: true, Body: strings.TrimSpace(p.body.Value()), BodySet: true}
}

func (p *verdictPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyCtrlS:
		if !m.opsIdle() { // refused at once: keep the popup and what was typed
			return m.sayInDiff(i18n.T("another operation is running — send again when it ends")), nil
		}
		m.keptSendBody = &keptSendBody{pr: p.pr, verdict: true, text: p.body.Value()}
		m = m.popLayer()
		return m.forgeSendCmd(p.request())
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

func (p *verdictPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *verdictPopup) box(m Model) string {
	w, _ := m.overlayDims()
	innerW := popupResolveWidth(w, p.maximized, commitNormalWidth(w))
	contentW := popupTextWidth(innerW)
	var b strings.Builder
	b.WriteString(i18n.T("Verdict on #%d", p.pr) + "\n" + i18n.T("no comments, only the verdict") + "\n\n")
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

// keptSendBody is the text of a body popup whose send has not reached GitHub.
type keptSendBody struct {
	pr      int
	group   string // sendGroupPanel for the send panel; "" with verdict
	verdict bool
	text    string
}

// sendGroupPanel keys the send panel's typed body in keptSendBody: one per
// PR, whatever is ticked (A8).
const sendGroupPanel = "panel"

// from reports whether req is the send this kept body's box made (C8): the
// same PR and the same box — the send panel (a Notes send) or Verdict….
func (k *keptSendBody) from(req domain.PRSendRequest) bool {
	if k == nil || !req.BodySet || k.pr != req.PR {
		return false
	}
	switch {
	case len(req.Notes) > 0:
		return k.group == sendGroupPanel && !k.verdict
	case req.Verdict:
		return k.verdict
	}
	return false
}

// keptBodyFor is the text a failed send of the same PR and box left (F12):
// the next body box starts from it.
func (m Model) keptBodyFor(pr int, group string, verdict bool) (string, bool) {
	k := m.keptSendBody
	if k == nil || k.pr != pr || k.group != group || k.verdict != verdict {
		return "", false
	}
	return k.text, true
}
