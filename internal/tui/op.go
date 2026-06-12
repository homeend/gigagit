package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/observ"
)

// opEventMsg carries one engine event (progress/done/gitline) to the UI.
type opEventMsg struct{ event engine.Event }

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

// decisionState holds an in-flight modal decision (used by the modal in Task 2).
type decisionState struct {
	req   engine.DecisionRequest
	reply chan engine.DecisionResponse
	sel   int
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

// startOp launches op in a goroutine, forwarding its events and completion onto
// a fresh message channel, and returns the command that waits for the next msg.
func (m Model) startOp(op engine.Operation) (Model, tea.Cmd) {
	msgs := make(chan tea.Msg, 32)
	events := make(chan engine.Event, 32)
	repo := m.repo
	go func() {
		opStart := time.Now()
		res, err := op.Run(context.Background(), engine.OpDeps{
			Repo:    repo,
			Events:  events,
			Decider: uiDecider{msgs: msgs},
		})
		span := observ.Span{Name: "op " + engine.OpName(op), Start: opStart, Duration: time.Since(opStart)}
		if err != nil {
			span.ExitCode = 1
			span.Err = err.Error()
		}
		observ.EmitSpan(span)
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
	return m, waitForOp(msgs)
}

// waitForOp blocks (off the UI thread) for the next op message.
func waitForOp(msgs chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-msgs }
}
