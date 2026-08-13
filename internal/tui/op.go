package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/rebaseplan"
)

// opEventMsg carries one engine event (progress/done/gitline) to the UI.
type opEventMsg struct{ event engine.Event }

// rebaseRangeLoadedMsg carries the commit range for a single-commit move/drop,
// loaded off the UI thread; the handler builds the plan and runs the rebase.
type rebaseRangeLoadedMsg struct {
	branch, onto, target string
	edit                 rebaseplan.Edit
	commits              []model.RangeCommit
	err                  error
}

// loadRebaseRangeCmd reads onto..branch off the UI thread for a single-commit edit.
func (m Model) loadRebaseRangeCmd(branch, onto, target string, e rebaseplan.Edit) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		cs, err := svc.CommitRange(context.Background(), onto, branch)
		return rebaseRangeLoadedMsg{branch: branch, onto: onto, target: target, edit: e, commits: cs, err: err}
	}
}

// squashRangeLoadedMsg carries the onto..branch range for a squash, loaded off
// the UI thread; the handler builds the squash plan and runs the rebase.
type squashRangeLoadedMsg struct {
	branch, onto string
	targets      []string
	commits      []model.RangeCommit
	err          error
}

// loadSquashRangeCmd reads onto..branch off the UI thread for a squash.
func (m Model) loadSquashRangeCmd(branch, onto string, targets []string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		cs, err := svc.CommitRange(context.Background(), onto, branch)
		return squashRangeLoadedMsg{branch: branch, onto: onto, targets: targets, commits: cs, err: err}
	}
}

// dropRangeLoadedMsg carries the onto..branch range for a multi-commit drop,
// loaded off the UI thread; the handler builds the drop plan and runs the rebase.
type dropRangeLoadedMsg struct {
	branch, onto string
	targets      []string
	commits      []model.RangeCommit
	err          error
}

// loadDropRangeCmd reads onto..branch off the UI thread for a multi-commit drop.
func (m Model) loadDropRangeCmd(branch, onto string, targets []string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		cs, err := svc.CommitRange(context.Background(), onto, branch)
		return dropRangeLoadedMsg{branch: branch, onto: onto, targets: targets, commits: cs, err: err}
	}
}

// editorFinishedMsg signals the external editor exited (path is the edited
// repo-relative path).
type editorFinishedMsg struct {
	path string
	err  error
}

// reloadStatusCmd re-reads only the working-tree status off the UI thread,
// yielding a statusRefreshedMsg (the panels-only refresh).
func (m Model) reloadStatusCmd(summary string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		st, err := svc.Status(context.Background())
		return statusRefreshedMsg{summary: summary, status: st, err: err}
	}
}

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
		return statusRefreshedMsg{summary: renderSummary(res), status: st, err: serr}
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

// heartbeatMsg fires ~once a second while an op runs. Its only job is to wake
// Update so View re-renders the busy line with a fresh elapsed time; the model
// stops re-arming it once the op finishes.
type heartbeatMsg struct{}

// heartbeatCmd schedules the next heartbeat tick.
func heartbeatCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return heartbeatMsg{} })
}

// decisionState holds an in-flight modal decision. Engine-driven decisions
// answer over reply; frontend-only decisions (e.g. jump-to-worktree) set
// onResolve instead — the modal key handler calls it with the live,
// modal-cleared model and the chosen option, and returns its result.
type decisionState struct {
	req       engine.DecisionRequest
	reply     chan engine.DecisionResponse
	sel       int
	confirm   bool              // yes/no confirm modal: enables y/n accelerators (frontend-only)
	copyTexts map[string]string // copy-file chooser: option label → resolved clipboard text (test-inspectable; nil for other modals)
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
	if m.bgCancel != nil {
		m.bgCancel() // preempt in-flight background reads so the user's op gets the slot
		m.bgCancel = nil
	}
	// A user op preempts the entire background lane; still-due items re-enqueue on
	// the next post-op tick. bgActiveItem is left as-is (meaningless when !bgBusy).
	m.bgBusy = false
	m.bgQueue = nil
	m.pendingSources = opAffectedSources(op)
	// A checkout-family op moves HEAD to a different branch/commit, which
	// obsoletes the solo/multi commit scope the same way a worktree switch
	// does (reRoot). Armed here — the single dispatch path — so every site
	// (direct key, confirm modal, dirty-switch chain) is covered; consumed
	// on a Changed success in opFinishedMsg.
	switch op.(type) {
	case engine.SmartSwitch, engine.SmartCheckout, engine.Checkout:
		m.pendingScopeClear = true
	default:
		m.pendingScopeClear = false
	}
	_, m.opIsFetch = op.(engine.Fetch) // a foreground fetch records its duration into the fetch row
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
	m.opName = engine.OpName(op)
	m.opStart = time.Now() // the perpetual heartbeat (Init) reads this to show elapsed time
	m.statusMsg = i18n.T("working…")
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

// conflictFileLoadedMsg carries a conflicted file's marker text for the picker,
// with the marker size it must be parsed at (0 clamps to git's default 7).
type conflictFileLoadedMsg struct {
	path       string
	content    []byte
	markerSize int
	err        error
}

// loadConflictFileCmd fetches a conflicted file's picker text off the UI
// thread — regenerated from the index stages with oversized markers, so file
// content that itself looks like conflict markers stays parseable; the
// resulting msg parses + pushes the picker.
func (m Model) loadConflictFileCmd(path string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		c, size, err := svc.ConflictPickerFile(context.Background(), path)
		return conflictFileLoadedMsg{path: path, content: c, markerSize: size, err: err}
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
