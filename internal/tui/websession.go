package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/i18n"
)

// A session started from the hosted web page: the page's server validates
// and approves the request, then hands it here so the child is the
// terminal's own — its inbox, its agent channel, its kept-inbox tracking.
// The terminal says so on the status line and never opens its console (the
// page shows it).

type webSessionReply struct {
	id  domain.SessionID
	err error
}

// webSessionRequestMsg is the page asking the terminal to start a session.
// reply is buffered: Update never blocks answering it.
type webSessionRequestMsg struct {
	req   domain.SessionStartRequest
	reply chan webSessionReply
}

type webSessionStartedMsg struct {
	id    domain.SessionID
	name  string
	dir   string
	inbox string
	err   error
	reply chan webSessionReply
}

// sessionStarterFor is the function the host calls for a page start: it
// hands the request to Update and waits for the answer (bounded by the
// page's request context).
func sessionStarterFor(w *webHostState) func(ctx context.Context, req domain.SessionStartRequest) (domain.SessionID, error) {
	reqs, stop := w.sessions, w.stop
	return func(ctx context.Context, req domain.SessionStartRequest) (domain.SessionID, error) {
		reply := make(chan webSessionReply, 1)
		select {
		case reqs <- webSessionRequestMsg{req: req, reply: reply}:
		case <-stop:
			return "", errors.New("the terminal is closing")
		case <-ctx.Done():
			return "", ctx.Err()
		}
		select {
		case r := <-reply:
			return r.id, r.err
		case <-stop:
			return "", errors.New("the terminal is closing")
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

// waitWebSessionCmd waits for the page's next start request; nil once the
// page is closed.
func waitWebSessionCmd(w *webHostState) tea.Cmd {
	reqs, stop := w.sessions, w.stop
	return func() tea.Msg {
		select {
		case req := <-reqs:
			return req
		case <-stop:
			return nil
		}
	}
}

// onWebSessionRequest starts the session off the UI thread with what only
// the terminal knows (child env, agent channel, shell), at the size the page
// measured; the wait re-arms at once.
func (m Model) onWebSessionRequest(msg webSessionRequestMsg) (Model, tea.Cmd) {
	var rearm tea.Cmd
	if m.web != nil {
		rearm = waitWebSessionCmd(m.web)
	}
	m, start, why := m.webSessionStartLeaf(msg)
	if why != "" {
		msg.reply <- webSessionReply{err: errors.New(why)}
		return m, rearm
	}
	return m, tea.Batch(start, rearm)
}

// hasSessionCommand: EnsureSessionCommands' own "already configured" test.
func hasSessionCommand(cfg config.Config) bool {
	for _, tc := range cfg.Tools.Command {
		if tc.Category == string(exttool.CatSession) {
			return true
		}
	}
	return false
}

// webSessionStartLeaf builds the start itself (no wait attached — tests run
// it directly); why is the English refusal for the page. The page's first
// start on a machine may have just WRITTEN the session commands (its
// first-run detect runs in the web request): a terminal that still sees
// none reloads its config here, or its own Start agent would detect and
// append them a second time.
func (m Model) webSessionStartLeaf(msg webSessionRequestMsg) (Model, tea.Cmd, string) {
	req := msg.req
	if !hasSessionCommand(m.cfg) {
		if nc, err := config.Load(agentGlobalConfigPath(), m.repoConfigPath); err == nil {
			m.cfg = nc
		}
	}
	cwd, _, refusal := sessionPlace(req.Worktree)
	if refusal != "" {
		return m, nil, "cannot start here: " + req.Worktree + " is not reachable from here"
	}
	svc, shell, env, inbox, url := m.svc, m.cfg.Console.Shell, m.childEnv(), m.childInboxDir(), m.agentURL()
	name := req.Command.Name
	if req.Terminal {
		name = i18n.T("Terminal")
	}
	start := func() tea.Msg {
		out := webSessionStartedMsg{name: name, dir: req.Worktree, inbox: inbox, reply: msg.reply}
		if req.Terminal {
			s, err := svc.StartTerminal(context.Background(), shell, req.Worktree, cwd, req.Cols, req.Rows, env)
			if err != nil {
				out.err = err
				return out
			}
			out.id = s.Info().ID
			return out
		}
		s, _, err := svc.StartAgentSession(context.Background(), req.Command, req.Worktree, cwd, req.Cols, req.Rows, env, url, domain.SpawnRecord{}, "")
		if err != nil {
			out.err = err
			return out
		}
		out.id = s.Info().ID
		return out
	}
	return m, start, ""
}

// onWebSessionStarted answers the page and keeps the child's inbox answered.
func (m Model) onWebSessionStarted(msg webSessionStartedMsg) (Model, tea.Cmd) {
	msg.reply <- webSessionReply{id: msg.id, err: msg.err}
	if msg.err != nil {
		m.statusMsg = i18n.T("could not start %s: %s", msg.name, msg.err.Error())
		return m, nil
	}
	if msg.inbox != "" {
		if m.childInbox == nil {
			m.childInbox = map[domain.SessionID]string{}
		}
		m.childInbox[msg.id] = msg.inbox
	}
	m.statusMsg = i18n.T("%s started in %s from the web page", msg.name, shortWorktreeName(msg.dir))
	return m, nil
}
