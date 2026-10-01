package domain

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/sessionreg"
	"github.com/homeend/gigagit/internal/wtclaim"
	"github.com/homeend/gigagit/internal/wtguard"
)

func TestInventoryUsesTheGuardSetHook(t *testing.T) {
	// Not parallel: swaps the process-wide composition hook. Parallel tests
	// resume only after every sequential top-level test has finished.
	main, svc, _ := inventoryRepo(t)
	wt := addWT(t, main, "hooked")
	prev := WorktreeGuardSet
	WorktreeGuardSet = func(GuardSources) []wtguard.Guard { return []wtguard.Guard{alwaysBlocks{}} }
	defer func() { WorktreeGuardSet = prev }()
	infos, _ := svc.WorktreeInventory(context.Background(), pol(), false)
	if got := find(t, infos, wt).BlockedBy; !slices.Equal(got, []string{"test-block"}) {
		t.Fatalf("blocked_by = %v", got)
	}
}

type alwaysBlocks struct{}

func (alwaysBlocks) Reason() string  { return "test-block" }
func (alwaysBlocks) FactKey() string { return "" }
func (alwaysBlocks) Cheap() bool     { return true }
func (alwaysBlocks) Check(context.Context, wtguard.Target) (wtguard.Result, error) {
	return wtguard.Result{Blocker: &wtguard.Blocker{Reason: "test-block"}}, nil
}

func TestClaimGuardExemptsHolder(t *testing.T) {
	t.Parallel()
	main, svc, reg := inventoryRepo(t)
	wt := addWT(t, main, "held")
	sessionreg.Write(reg, "p", sessionreg.Registry{PID: 1, Sessions: []sessionreg.Entry{{ID: "p/s1", Dir: main, State: "running"}}})
	wtclaim.Create(git.GitDirAt(wt), wtclaim.Claim{Session: "p/s1", Since: time.Now()})
	ctx := context.Background()
	holder, _ := svc.GuardReport(ctx, wtguard.Target{Dir: wt, Branch: "held", CallerSession: "p/s1"})
	other, _ := svc.GuardReport(ctx, wtguard.Target{Dir: wt, Branch: "held", CallerSession: "p/s2"})
	if slices.Contains(holder.Reasons(), "claimed") || !slices.Contains(other.Reasons(), "claimed") {
		t.Fatalf("holder=%v other=%v", holder.Reasons(), other.Reasons())
	}
}
