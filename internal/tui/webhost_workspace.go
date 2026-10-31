package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// workspaceInfo is the open workspace as the hosted page is told it.
func (m Model) workspaceInfo() *domain.WorkspaceInfo {
	if m.ws == nil {
		return nil
	}
	info := &domain.WorkspaceInfo{Name: m.ws.name, Live: m.ws.live, SettingsRepo: m.ws.settings, Members: make([]domain.WorkspaceMember, 0, len(m.ws.members))}
	for i, r := range m.ws.members {
		info.Members = append(info.Members, domain.WorkspaceMember{
			Path: r.mainPath, Label: headingLabel(m.ws.members, i), Missing: r.missing,
			LastViewed: r.lastViewed, Svc: r.svc,
		})
	}
	return info
}

// workspacePrint is info's identity: a push goes out when it moves.
func workspacePrint(info *domain.WorkspaceInfo) string {
	if info == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%q %d %q", info.Name, info.Live, info.SettingsRepo)
	for _, r := range info.Members {
		fmt.Fprintf(&b, " %q %q %v %q %p", r.Path, r.Label, r.Missing, r.LastViewed, r.Svc)
	}
	return b.String()
}

// pushWebWorkspace tells a serving page what is open when that changed: the
// Update tail calls it, so every open, leave, edit, adopt and member load goes
// out without each of them having to remember. No git.
func (m Model) pushWebWorkspace() {
	if !m.webServing() {
		return
	}
	info := m.workspaceInfo()
	if p := workspacePrint(info); p != m.web.wsPrint {
		m.web.wsPrint = p
		m.web.host.SetWorkspace(info)
	}
	q := m.launchQuestion
	if q != nil && (m.ws != nil || q.repoKey != m.homeRepoKey()) {
		q = nil // the repository changed under it, or a workspace opened: over
	}
	if qp := questionPrint(q); qp != m.web.qPrint {
		m.web.qPrint = qp
		m.web.host.SetWorkspaceQuestion(q.wire())
	}
}

func (m Model) homeRepoKey() model.CheckoutKey {
	if r := m.homeRepo(); r != nil {
		return r.key
	}
	return ""
}

func questionPrint(q *workspaceLaunchPopup) string {
	if q == nil {
		return ""
	}
	return string(q.repoKey) + " " + fmt.Sprint(len(q.rows))
}

// wire is the question as the page shows it: the workspace rows; the page adds
// Open <repo> alone from Repo.
func (p *workspaceLaunchPopup) wire() *domain.WorkspaceQuestion {
	if p == nil {
		return nil
	}
	q := &domain.WorkspaceQuestion{Repo: p.repoName}
	for _, r := range p.rows {
		if r.entry != nil {
			q.Rows = append(q.Rows, domain.WorkspaceQuestionRow{Name: r.entry.Name, Label: r.label})
		}
	}
	return q
}

// launchPopupToQuestion moves the launch popup behind a flip to the page.
func (m Model) launchPopupToQuestion() Model {
	if p, ok := m.topLayer().(*workspaceLaunchPopup); ok {
		m = m.popLayer()
		m.launchQuestion = p
	}
	return m
}

// launchQuestionToPopup moves a pending question to the terminal's popup.
func (m Model) launchQuestionToPopup() Model {
	if q := m.launchQuestion; q != nil {
		m.launchQuestion = nil
		if m.ws == nil && q.repoKey == m.homeRepoKey() {
			q.sel = 0
			m = m.pushLayer(q)
		}
	}
	return m
}

// actorDropState answers askBeforeDrop while a page's request runs: "" reports
// the sessions and stops the change, keep goes on, stop kills them and goes on.
type actorDropState struct {
	drop  string
	asked []domain.SessionInfo
}

// decide reports whether the change must stop to ask.
func (s *actorDropState) decide(m Model, gone []*repoView) bool {
	var sessions []domain.SessionInfo
	for _, r := range gone {
		sessions = append(sessions, m.runningSessionsIn(r)...)
	}
	if len(sessions) == 0 || s.drop == "keep" {
		return false
	}
	if s.drop == "stop" {
		for _, info := range sessions {
			_ = domain.Sessions().Kill(info.ID)
		}
		return false
	}
	s.asked = sessions
	return true
}

// webWorkspaceRequestMsg is the page asking the terminal for a workspace change.
type webWorkspaceRequestMsg struct {
	req   domain.WorkspaceRequest
	reply chan webWorkspaceReply
}

type webWorkspaceReply struct {
	reply domain.WorkspaceReply
	err   error
}

// workspaceActorFor is the function the host calls for a workspace request: it
// hands the request to Update and waits for the answer.
func workspaceActorFor(w *webHostState) func(ctx context.Context, req domain.WorkspaceRequest) (domain.WorkspaceReply, error) {
	workspaces, stop := w.workspaces, w.stop
	closing := errors.New("the terminal is closing")
	return func(ctx context.Context, req domain.WorkspaceRequest) (domain.WorkspaceReply, error) {
		reply := make(chan webWorkspaceReply, 1)
		select {
		case workspaces <- webWorkspaceRequestMsg{req: req, reply: reply}:
		case <-stop:
			return domain.WorkspaceReply{}, closing
		case <-ctx.Done():
			return domain.WorkspaceReply{}, ctx.Err()
		}
		select {
		case r := <-reply:
			return r.reply, r.err
		case <-stop:
			return domain.WorkspaceReply{}, closing
		case <-ctx.Done():
			return domain.WorkspaceReply{}, ctx.Err()
		}
	}
}

// waitWebWorkspaceCmd waits for the page's next workspace request.
func waitWebWorkspaceCmd(w *webHostState) tea.Cmd {
	workspaces, stop := w.workspaces, w.stop
	return func() tea.Msg {
		select {
		case req := <-workspaces:
			return req
		case <-stop:
			return nil
		}
	}
}

// onWebWorkspaceRequest runs a page's request. A terminal busy with something
// the page cannot see refuses as a switch does.
func (m Model) onWebWorkspaceRequest(msg webWorkspaceRequestMsg) (Model, tea.Cmd) {
	rearm := waitWebWorkspaceCmd(m.web)
	nm, cmd, reply, err := m.workspaceEditApply(msg.req)
	msg.reply <- webWorkspaceReply{reply: reply, err: err}
	return nm, tea.Batch(cmd, rearm)
}

// forwardMemberChange is a blocked terminal's answer to a sleeping member
// moving: the page re-reads it. The terminal reads it on activation.
func (m Model) forwardMemberChange(r *repoView) {
	if m.webServing() {
		m.web.host.MemberChanged(r.mainPath)
	}
}
