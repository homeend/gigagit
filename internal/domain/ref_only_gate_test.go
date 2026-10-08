package domain

import (
	"context"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/repogate"
)

// The user's report: a headless conflict agent holds the gate in Read mode
// for its whole run; `b` (create branch) then "did nothing". The create must
// run alongside that holder instead of queuing behind it.
func TestCreateBranchRunsBesideReadHolder(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git check-ref-format", gitexec.Result{})
	f.SetResponse("git branch", gitexec.Result{})
	svc := New(&git.Repo{Runner: f})
	ctx := context.Background()
	hold, err := svc.gateFor(ctx).Acquire(ctx, repogate.Read, "conflict agent")
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Release()
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	res, err := svc.Execute(c, engine.CreateBranch{Name: "feat/x"}, nil, nil)
	if err != nil {
		t.Fatalf("create branch while a Read holder is active: %v", err)
	}
	if !res.Changed {
		t.Fatalf("expected Changed, got %+v", res)
	}
	for _, e := range svc.gateFor(ctx).Queue() {
		if e.Waiting {
			t.Fatalf("nothing may be left queued: %+v", e)
		}
	}
}
