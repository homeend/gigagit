package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/wtguard"
)

func guardsReturning(bs ...wtguard.Blocker) func(context.Context, wtguard.Target) (wtguard.Report, error) {
	return func(context.Context, wtguard.Target) (wtguard.Report, error) {
		return wtguard.Report{Blockers: bs, Facts: map[string]any{}}, nil
	}
}

func TestRecycleHardBlockerFailsUntouched(t *testing.T) {
	t.Parallel()
	_, deps, wt := recycleFixture(t)
	deps.Guards = guardsReturning(wtguard.Blocker{Reason: "missing", Hard: true})
	_, err := RecycleWorktree{Dir: wt, Branch: "target", Now: fixedNow}.Run(context.Background(), deps)
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("err = %v", err)
	}
	if got := wtHead(t, wt); got != "a" {
		t.Fatalf("HEAD = %q, want a (untouched)", got)
	}
}

func TestRecycleOverridableBlockersAskOnce(t *testing.T) {
	t.Parallel()
	_, deps, wt := recycleFixture(t)
	deps.Guards = guardsReturning(
		wtguard.Blocker{Reason: "claimed", Detail: "claude"},
		wtguard.Blocker{Reason: "reserved"},
	)
	deps.Decider = MapDecider{RecycleBlockedDecisionID: "abort"}
	res, err := RecycleWorktree{Dir: wt, Branch: "target", Now: fixedNow}.Run(context.Background(), deps)
	if err != nil || !strings.Contains(res.Summary, "cancelled") || wtHead(t, wt) != "a" {
		t.Fatalf("abort: res=%+v err=%v head=%s", res, err, wtHead(t, wt))
	}
	deps.Decider = MapDecider{RecycleBlockedDecisionID: "recycle anyway"}
	if _, err := (RecycleWorktree{Dir: wt, Branch: "target", Now: fixedNow}).Run(context.Background(), deps); err != nil {
		t.Fatal(err)
	}
	if wtHead(t, wt) != "target" {
		t.Fatal("recycle anyway must proceed")
	}
}

func TestRecycleDirtyRecentIsLeftToTheDirtyQuestion(t *testing.T) {
	t.Parallel()
	_, deps, wt := recycleFixture(t)
	deps.Guards = guardsReturning(wtguard.Blocker{Reason: "dirty-recent"})
	deps.Decider = MapDecider{} // any decision would fail with ErrDecisionRequired
	if _, err := (RecycleWorktree{Dir: wt, Branch: "target", Now: fixedNow}).Run(context.Background(), deps); errors.Is(err, ErrDecisionRequired) {
		t.Fatal("dirty-recent alone must not raise recycle.blocked")
	}
}

func TestRecycleDetachedIsNotAsked(t *testing.T) {
	t.Parallel()
	_, deps, wt := recycleFixture(t)
	deps.Guards = guardsReturning(wtguard.Blocker{Reason: "detached"})
	deps.Decider = MapDecider{}
	if _, err := (RecycleWorktree{Dir: wt, Branch: "target", Now: fixedNow}).Run(context.Background(), deps); errors.Is(err, ErrDecisionRequired) {
		t.Fatal("detached alone must not raise recycle.blocked")
	}
}

func TestRecyclePassesCallerSession(t *testing.T) {
	t.Parallel()
	_, deps, wt := recycleFixture(t)
	var got wtguard.Target
	deps.Guards = func(_ context.Context, tg wtguard.Target) (wtguard.Report, error) {
		got = tg
		return wtguard.Report{Facts: map[string]any{}}, nil
	}
	RecycleWorktree{Dir: wt, Branch: "target", CallerSession: "p/s1", Now: fixedNow}.Run(context.Background(), deps)
	if got.CallerSession != "p/s1" || got.Dir == "" {
		t.Fatalf("target = %+v", got)
	}
}
