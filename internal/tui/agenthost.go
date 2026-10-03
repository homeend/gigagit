package tui

import (
	"context"
	"errors"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// AgentHost is the agent MCP channel served from THIS process (cmd/gg sets
// NewAgentHost to internal/mcp's host; the frontends never import each
// other). nil = unavailable: sessions then get no GG_MCP_URL.
type AgentHost interface {
	Start(starter func(context.Context, domain.AgentStartRequest) (domain.AgentStartResult, error)) (string, error)
	Close()
}

var NewAgentHost func() AgentHost

// agentSpawn is a test seam over domain.SpawnAgent.
var agentSpawn = domain.SpawnAgent

type agentHostState struct {
	host     AgentHost
	url      string
	requests chan agentSpawnRequestMsg
	stop     chan struct{}
	once     sync.Once // closeAgentHost runs at most once
}

type agentSpawnReply struct {
	res domain.AgentStartResult
	err error
}

type agentSpawnRequestMsg struct {
	req   domain.AgentStartRequest
	reply chan agentSpawnReply
}

type agentSpawnedMsg struct {
	res   domain.AgentStartResult
	id    domain.SessionID
	err   error
	reply chan agentSpawnReply
	inbox string
}

func newAgentHostState() *agentHostState {
	return &agentHostState{requests: make(chan agentSpawnRequestMsg), stop: make(chan struct{})}
}

// starterFor hands an agent_start into Update and waits for the answer.
func starterFor(st *agentHostState) func(context.Context, domain.AgentStartRequest) (domain.AgentStartResult, error) {
	return func(ctx context.Context, req domain.AgentStartRequest) (domain.AgentStartResult, error) {
		// A wedged TUI must not hang the agent's call (spec §7); a late
		// spawn still records itself — reply is buffered, the send never blocks.
		ctx, cancel := context.WithTimeout(ctx, agentStartTimeout)
		defer cancel()
		reply := make(chan agentSpawnReply, 1)
		select {
		case st.requests <- agentSpawnRequestMsg{req: req, reply: reply}:
		case <-st.stop:
			return domain.AgentStartResult{}, errors.New("gg is closing")
		case <-ctx.Done():
			return domain.AgentStartResult{}, ctx.Err()
		}
		select {
		case r := <-reply:
			return r.res, r.err
		case <-st.stop:
			return domain.AgentStartResult{}, errors.New("gg is closing")
		case <-ctx.Done():
			return domain.AgentStartResult{}, errors.New("gg did not answer in time; agent_list shows whether the worker started")
		}
	}
}

// agentStartTimeout bounds one agent_start round trip through Update.
const agentStartTimeout = 30 * time.Second

// agentSpawnTimeout bounds the spawn itself (git, claim, session start).
const agentSpawnTimeout = 5 * time.Minute

func waitAgentSpawnCmd(st *agentHostState) tea.Cmd {
	if st == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case r := <-st.requests:
			return r
		case <-st.stop:
			return nil
		}
	}
}

// startAgentHost serves the channel before the program starts (Run).
func (m Model) startAgentHost() Model {
	if NewAgentHost == nil {
		return m
	}
	st := newAgentHostState()
	h := NewAgentHost()
	url, err := h.Start(starterFor(st))
	if err != nil {
		m.statusMsg = i18n.T("agent channel unavailable: %s", err.Error())
		return m
	}
	st.host, st.url = h, url
	m.agentHost = st
	return m
}

func (m Model) closeAgentHost() {
	st := m.agentHost
	if st == nil {
		return
	}
	st.once.Do(func() {
		close(st.stop)
		if st.host != nil {
			st.host.Close()
		}
	})
}

func (m Model) agentURL() string {
	if m.agentHost == nil {
		return ""
	}
	return m.agentHost.url
}

// onAgentSpawnRequest supplies what only the TUI knows — the console size,
// its child env, the approval store — and runs the spawn off the UI thread;
// the wait re-arms at once so a second request is never stuck behind this one.
func (m Model) onAgentSpawnRequest(msg agentSpawnRequestMsg) (Model, tea.Cmd) {
	g := m.layout()
	cols, rows := consoleInner(g.rightW, g.boxH[panelCommits])
	store := m.promptStore
	sp := domain.SpawnSpec{Req: msg.req, Cols: cols, Rows: rows, Env: m.childEnv(), MCPURL: m.agentURL(),
		Approved: func(repoKey, command string) bool {
			return store != nil && store.ApprovedToolCommands(repoKey)[toolCommandHash(command)]
		}}
	inbox := m.childInboxDir()
	spawn := func() tea.Msg {
		// Bounded: a git call hung on a slow mount must not hold a cap slot
		// forever; longer than agentStartTimeout so a late start still records.
		ctx, cancel := context.WithTimeout(context.Background(), agentSpawnTimeout)
		defer cancel()
		res, sess, err := agentSpawn(ctx, sp)
		out := agentSpawnedMsg{res: res, err: err, reply: msg.reply, inbox: inbox}
		if sess != nil {
			out.id = sess.Info().ID
		}
		return out
	}
	return m, tea.Batch(spawn, waitAgentSpawnCmd(m.agentHost))
}

// onAgentSpawned answers the agent, keeps the child's inbox answered, and
// says so on the status line — never opening the console (ruling 11).
func (m Model) onAgentSpawned(msg agentSpawnedMsg) (Model, tea.Cmd) {
	msg.reply <- agentSpawnReply{res: msg.res, err: msg.err}
	if msg.err != nil {
		return m, nil
	}
	if msg.inbox != "" && msg.id != "" {
		if m.childInbox == nil {
			m.childInbox = map[domain.SessionID]string{}
		}
		m.childInbox[msg.id] = msg.inbox
	}
	m.statusMsg = i18n.T("%s started in %s by an agent", msg.res.Tool, shortWorktreeName(msg.res.Worktree))
	if msg.res.Warning != "" {
		m.statusMsg += " — " + i18n.T("its worktree claim stayed with the agent that started it")
	}
	m = m.fileBriefTour(msg.id) // its brief as a tour in the worker's worktree, in the background
	var cmd tea.Cmd
	m, cmd = m.reloadSourcesCmd([]sourceKey{srcWorktrees, srcBranches}, reloadOpts{})
	return m, cmd
}
