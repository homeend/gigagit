package engine

import (
	"context"
	"errors"
	"fmt"
)

// DecisionRequest describes a fork an operation cannot resolve on its own.
type DecisionRequest struct {
	ID      string
	Prompt  string
	Options []string
}

// DecisionResponse is the chosen option.
type DecisionResponse struct{ Option string }

// Decider resolves a DecisionRequest. A TUI prompts a human; a headless/MCP
// caller uses a MapDecider seeded with pre-answers.
type Decider interface {
	Decide(ctx context.Context, req DecisionRequest) (DecisionResponse, error)
}

// ErrDecisionRequired is returned when a fork has no available answer.
var ErrDecisionRequired = errors.New("engine: decision required but no decider/answer available")

// MapDecider answers decisions from a fixed ID->option map (the "policy").
type MapDecider map[string]string

func (m MapDecider) Decide(_ context.Context, req DecisionRequest) (DecisionResponse, error) {
	if opt, ok := m[req.ID]; ok {
		return DecisionResponse{Option: opt}, nil
	}
	return DecisionResponse{}, fmt.Errorf("%w: %s", ErrDecisionRequired, req.ID)
}

// DeciderFunc adapts a function to the Decider interface.
type DeciderFunc func(ctx context.Context, req DecisionRequest) (DecisionResponse, error)

func (f DeciderFunc) Decide(ctx context.Context, req DecisionRequest) (DecisionResponse, error) {
	return f(ctx, req)
}
