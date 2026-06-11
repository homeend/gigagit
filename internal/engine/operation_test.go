package engine

import (
	"context"
	"testing"
)

type fakeOp struct{}

func (fakeOp) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(Progress{Step: "working"})
	resp, err := deps.decide(ctx, DecisionRequest{ID: "go?", Options: []string{"yes", "no"}})
	if err != nil {
		return Result{}, err
	}
	res := Result{Summary: "did " + resp.Option, Changed: resp.Option == "yes"}
	deps.emit(Done{Result: res})
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
	deps.emit(Progress{Step: "x"})
	_, err := deps.decide(context.Background(), DecisionRequest{ID: "y"})
	if err == nil {
		t.Fatal("decide with nil Decider should return an error")
	}
}
