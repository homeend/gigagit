package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// staticDecider answers engine forks from a fixed policy (the CLI flag-policy
// pattern). Stage 1's only reachable decision is ExportToDir's "overwrite";
// anything else fails loud — an MCP tool must never wedge on a question.
type staticDecider struct {
	policy map[string]string
}

func (d staticDecider) Decide(_ context.Context, req engine.DecisionRequest) (engine.DecisionResponse, error) {
	if opt, ok := d.policy[req.ID]; ok {
		return engine.DecisionResponse{Option: opt}, nil
	}
	return engine.DecisionResponse{}, fmt.Errorf(
		"unexpected decision %q (options: %s)", req.ID, strings.Join(req.Options, ", "))
}

// runOp executes op via domain.Execute, draining events (the MCP reply
// carries the outcome; per-line progress has no channel here).
func runOp(ctx context.Context, svc *domain.Service, op engine.Operation, dec engine.Decider) (engine.Result, error) {
	events := make(chan engine.Event, 32)
	done := make(chan struct{})
	var (
		res engine.Result
		err error
	)
	go func() {
		res, err = svc.Execute(ctx, op, events, dec)
		close(events)
		close(done)
	}()
	for range events {
	}
	<-done
	return res, err
}
