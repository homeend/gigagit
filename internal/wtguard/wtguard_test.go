package wtguard

import (
	"context"
	"errors"
	"slices"
	"testing"
)

type fake struct {
	reason, key string
	cheap       bool
	block, hard bool
	fact        any
	err         error
	ran         *int
}

func (f fake) Reason() string  { return f.reason }
func (f fake) FactKey() string { return f.key }
func (f fake) Cheap() bool     { return f.cheap }
func (f fake) Check(context.Context, Target) (Result, error) {
	if f.ran != nil {
		*f.ran++
	}
	if f.err != nil {
		return Result{}, f.err
	}
	r := Result{Fact: f.fact}
	if f.block {
		r.Blocker = &Blocker{Reason: f.reason, Hard: f.hard}
	}
	return r, nil
}

func TestRunCheapBlocksSkipExpensive(t *testing.T) {
	t.Parallel()
	ran := 0
	r := Run(context.Background(), []Guard{
		fake{reason: "claimed", key: "claim", cheap: true, block: true, fact: "c"},
		fake{reason: "dirty-recent", key: "dirty", cheap: false, ran: &ran},
	}, Target{Dir: "/x"})
	if ran != 0 {
		t.Fatal("expensive guard ran although a cheap one blocked")
	}
	if !slices.Equal(r.Reasons(), []string{"claimed"}) || r.Facts["claim"] != "c" {
		t.Fatalf("report = %+v", r)
	}
	if v, ok := r.Facts["dirty"]; !ok || v != nil {
		t.Fatalf("a skipped guard's fact must be present and nil, got %v %v", v, ok)
	}
}

func TestRunAllCheapThenExpensive(t *testing.T) {
	t.Parallel()
	ran := 0
	r := Run(context.Background(), []Guard{
		fake{reason: "a", cheap: true},
		fake{reason: "dirty-recent", key: "dirty", ran: &ran, block: true, fact: 3},
	}, Target{})
	if ran != 1 || !slices.Equal(r.Reasons(), []string{"dirty-recent"}) || r.Facts["dirty"] != 3 {
		t.Fatalf("ran=%d report=%+v", ran, r)
	}
}

func TestRunErrorIsHardCheckFailed(t *testing.T) {
	t.Parallel()
	r := Run(context.Background(), []Guard{fake{reason: "x", cheap: true, err: errors.New("boom")}}, Target{})
	if len(r.Hard()) != 1 || r.Hard()[0].Reason != "check-failed" {
		t.Fatalf("report = %+v", r)
	}
}

func TestReportSplitsHardAndOverridable(t *testing.T) {
	t.Parallel()
	r := Run(context.Background(), []Guard{
		fake{reason: "missing", cheap: true, block: true, hard: true},
		fake{reason: "reserved", cheap: true, block: true},
	}, Target{})
	if len(r.Hard()) != 1 || len(r.Overridable()) != 1 || r.Overridable()[0].Reason != "reserved" {
		t.Fatalf("report = %+v", r)
	}
}
