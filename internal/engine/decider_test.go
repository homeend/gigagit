package engine

import (
	"context"
	"errors"
	"testing"
)

func TestMapDeciderAnswersByID(t *testing.T) {
	d := MapDecider{"non-fast-forward": "rebase"}
	resp, err := d.Decide(context.Background(), DecisionRequest{ID: "non-fast-forward"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Option != "rebase" {
		t.Fatalf("option = %q, want rebase", resp.Option)
	}
}

func TestMapDeciderUnansweredReturnsErrDecisionRequired(t *testing.T) {
	d := MapDecider{}
	_, err := d.Decide(context.Background(), DecisionRequest{ID: "x"})
	if !errors.Is(err, ErrDecisionRequired) {
		t.Fatalf("err = %v, want ErrDecisionRequired", err)
	}
}

func TestDeciderFuncAdapts(t *testing.T) {
	d := DeciderFunc(func(_ context.Context, req DecisionRequest) (DecisionResponse, error) {
		return DecisionResponse{Option: req.Options[0]}, nil
	})
	resp, err := d.Decide(context.Background(), DecisionRequest{Options: []string{"abort", "merge"}})
	if err != nil || resp.Option != "abort" {
		t.Fatalf("resp=%v err=%v, want option abort", resp, err)
	}
}
