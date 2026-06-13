package engine

import (
	"context"
	"errors"
	"testing"
)

type fakeOp struct{}

func (fakeOp) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "working"})
	resp, err := deps.decide(ctx, DecisionRequest{ID: "go?", Options: []string{"yes", "no"}})
	if err != nil {
		return Result{}, err
	}
	res := Result{Summary: "did " + resp.Option, Changed: resp.Option == "yes"}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

func TestOpDepsEmitAndDecide(t *testing.T) {
	ch := make(chan Event, 8)
	deps := OpDeps{
		Events:  ch,
		Decider: MapDecider{"go?": "yes"},
	}
	res, err := fakeOp{}.Run(context.Background(), deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Summary != "did yes" || !res.Changed {
		t.Fatalf("result = %+v, want did yes / changed", res)
	}
	close(ch)
	var kinds []string
	for e := range ch {
		switch e.(type) {
		case Progress:
			kinds = append(kinds, "progress")
		case Done:
			kinds = append(kinds, "done")
		}
	}
	if len(kinds) != 2 || kinds[0] != "progress" || kinds[1] != "done" {
		t.Fatalf("events = %v, want [progress done]", kinds)
	}
}

func TestOpDepsEmitNilChannelDoesNotPanic(t *testing.T) {
	deps := OpDeps{}
	deps.emit(context.Background(), Progress{Step: "x"})
	_, err := deps.decide(context.Background(), DecisionRequest{ID: "y"})
	if err == nil {
		t.Fatal("decide with nil Decider should return an error")
	}
}

func TestDecideEmitsDecisionNeededThenErrsWithoutDecider(t *testing.T) {
	ch := make(chan Event, 4)
	deps := OpDeps{Events: ch} // nil Decider
	_, err := deps.decide(context.Background(), DecisionRequest{
		ID:      "non-fast-forward",
		Options: []string{"rebase", "abort"},
	})
	if !errors.Is(err, ErrDecisionRequired) {
		t.Fatalf("err = %v, want ErrDecisionRequired", err)
	}
	close(ch)
	var sawDecision bool
	for e := range ch {
		if dn, ok := e.(DecisionNeeded); ok && dn.Request.ID == "non-fast-forward" {
			sawDecision = true
		}
	}
	if !sawDecision {
		t.Fatal("expected a DecisionNeeded event emitted before erroring")
	}
}

// TestOpDepsEscalateNilSafe: direct engine users (tests, future callers)
// configure no Escalate; the helper must be a successful no-op, matching the
// nil-channel/nil-decider style of emit/decide.
func TestOpDepsEscalateNilSafe(t *testing.T) {
	if err := (OpDeps{}).escalate(context.Background()); err != nil {
		t.Fatalf("nil escalate = %v, want nil", err)
	}
}
