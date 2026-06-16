package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/model"
)

// opEventMsg carries one engine event (progress/done/gitline) to the UI.
type opEventMsg struct{ event engine.Event }

// statusRefreshedMsg carries the result of a staging op plus a fresh Status
// read. It refreshes ONLY the Status panel (not the full snapshot) so repeated
// staging stays snappy on huge repos.
type statusRefreshedMsg struct {
	summary string
	status  model.WorkingTreeStatus
	err     error
}

// stageCmd runs a staging op through the domain layer, then re-reads only the
// working-tree status. It runs synchronously inside the returned tea.Cmd
// (staging is fast and has no decisions), yielding a single statusRefreshedMsg.
func (m Model) stageCmd(op engine.Operation) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		res, err := svc.Execute(context.Background(), op, nil, nil)
		if err != nil {
			return statusRefreshedMsg{err: err}
		}
		st, serr := svc.Status(context.Background())
		return statusRefreshedMsg{summary: res.Summary, status: st, err: serr}
	}
}

// amendPrefillMsg carries HEAD's message for the amend popup.
type amendPrefillMsg struct {
	msg string
	err error
}

// amendPrefillCmd fetches HEAD's message off the UI thread, to pre-fill the
// amend popup.
func (m Model) amendPrefillCmd() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		s, err := svc.LastCommitMessage(context.Background())
		return amendPrefillMsg{msg: s, err: err}
	}
}

// opDecisionMsg asks the UI to resolve a fork; the op goroutine blocks on reply.
type opDecisionMsg struct {
	req   engine.DecisionRequest
	reply chan engine.DecisionResponse
}

// opFinishedMsg is sent once when the operation returns.
type opFinishedMsg struct {
	res engine.Result
	err error
}

// decisionState holds an in-flight modal decision. Engine-driven decisions
// answer over reply; frontend-only decisions (e.g. jump-to-worktree) set
// onResolve instead — the modal key handler calls it with the live,
// modal-cleared model and the chosen option, and returns its result.
type decisionState struct {
	req       engine.DecisionRequest
	reply     chan engine.DecisionResponse
	sel       int
	onResolve func(m Model, opt string) (tea.Model, tea.Cmd)
}

// uiDecider bridges engine decisions to the UI over the msgs channel.
type uiDecider struct{ msgs chan tea.Msg }

func (d uiDecider) Decide(ctx context.Context, req engine.DecisionRequest) (engine.DecisionResponse, error) {
	reply := make(chan engine.DecisionResponse, 1)
	select {
	case d.msgs <- opDecisionMsg{req: req, reply: reply}:
	case <-ctx.Done():
		return engine.DecisionResponse{}, ctx.Err()
	}
	select {
	case resp := <-reply:
		return resp, nil
	case <-ctx.Done():
		return engine.DecisionResponse{}, ctx.Err()
	}
}

// startOp launches op through the domain layer in a goroutine, forwarding
// its events and completion onto a fresh message channel, and returns the
// command that waits for the next msg. The op context is cancelled when the
// program exits (run.go) so an op can never outlive the UI silently.
func (m Model) startOp(op engine.Operation) (Model, tea.Cmd) {
	msgs := make(chan tea.Msg, 32)
	events := make(chan engine.Event, 32)
	svc := m.svc
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		res, err := svc.Execute(ctx, op, events, uiDecider{msgs: msgs})
		close(events)
		msgs <- opFinishedMsg{res: res, err: err}
	}()
	go func() {
		for e := range events {
			msgs <- opEventMsg{event: e}
		}
	}()
	m.running = true
	m.statusMsg = "working…"
	m.opMsgs = msgs
	m.opCancel = cancel
	return m, waitForOp(msgs)
}

// waitForOp blocks (off the UI thread) for the next op message.
func waitForOp(msgs chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-msgs }
}

// irebaseLoadedMsg carries the range commits for the interactive-rebase editor.
type irebaseLoadedMsg struct {
	branch, onto string
	commits      []model.RangeCommit
	err          error
}

// loadIrebaseCmd fetches branch's commits since onto (oldest-first) off the UI
// thread; the resulting msg opens the editor.
func (m Model) loadIrebaseCmd(branch, onto string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		cs, err := svc.CommitRange(context.Background(), onto, branch)
		return irebaseLoadedMsg{branch: branch, onto: onto, commits: cs, err: err}
	}
}

// conflictFileLoadedMsg carries a conflicted file's marker text for the picker.
type conflictFileLoadedMsg struct {
	path    string
	content []byte
	err     error
}

// loadConflictFileCmd reads a conflicted file's working-tree bytes off the UI
// thread; the resulting msg parses + pushes the picker.
func (m Model) loadConflictFileCmd(path string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		c, err := svc.WorktreeFile(context.Background(), path)
		return conflictFileLoadedMsg{path: path, content: c, err: err}
	}
}

// stageHunksLoadedMsg carries the two sides for the staging picker.
type stageHunksLoadedMsg struct {
	path        string
	index, work []byte
	err         error
}

// loadStageHunksCmd reads the index blob and the working-tree bytes off the UI
// thread; the resulting msg builds the diff and pushes the staging picker.
func (m Model) loadStageHunksCmd(path string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		idx, err := svc.ShowFile(context.Background(), "", path)
		if err != nil {
			return stageHunksLoadedMsg{path: path, err: err}
		}
		work, werr := svc.WorktreeFile(context.Background(), path)
		if werr != nil {
			return stageHunksLoadedMsg{path: path, err: werr}
		}
		return stageHunksLoadedMsg{path: path, index: idx, work: work}
	}
}
