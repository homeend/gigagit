package domain_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

// failOp returns a plain error; engine.OpName(failOp{}) == "failOp".
type failOp struct{}

func (failOp) Run(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
	return engine.Result{}, errors.New("kaboom")
}

// cancelOp returns context.Canceled (a user abort, not a failure).
type cancelOp struct{}

func (cancelOp) Run(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
	return engine.Result{}, context.Canceled
}

func newFakeService() *domain.Service {
	// A bare FakeRunner errors for every span ("fake: no response configured"),
	// so any read query fails.
	return domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})
}

func TestQueryFailureRecorded(t *testing.T) {
	observ.ResetFailures()
	s := newFakeService()
	if _, err := s.Status(context.Background()); err == nil {
		t.Fatal("expected Status to fail against a bare fake runner")
	}
	fs := observ.SessionFailures()
	if len(fs) != 1 || fs[0].Source != "query status" {
		t.Fatalf("want one 'query status' failure, got %+v", fs)
	}
}

func TestExecuteFailureRecorded(t *testing.T) {
	observ.ResetFailures()
	s := newFakeService()
	if _, err := s.Execute(context.Background(), failOp{}, nil, nil); err == nil {
		t.Fatal("expected Execute(failOp) to return the op error")
	}
	fs := observ.SessionFailures()
	if len(fs) != 1 || fs[0].Source != "op failOp" {
		t.Fatalf("want one 'op failOp' failure, got %+v", fs)
	}
}

func TestCommitFeedFailureRecorded(t *testing.T) {
	observ.ResetFailures()
	s := newFakeService()
	// A bare fake runner fails the underlying `git log`. The commit walk routes
	// through the shared query() helper (key "commits:..."), so the failure is
	// captured there — no separate CommitFeed hook needed.
	if _, err := s.CommitFeed().LoadInitial(context.Background()); err == nil {
		t.Fatal("expected LoadInitial to fail against a bare fake runner")
	}
	fs := observ.SessionFailures()
	if len(fs) != 1 || !strings.HasPrefix(fs[0].Source, "query commits") {
		t.Fatalf("want one 'query commits…' failure, got %+v", fs)
	}
}

func TestExecuteCancellationNotRecorded(t *testing.T) {
	observ.ResetFailures()
	s := newFakeService()
	_, _ = s.Execute(context.Background(), cancelOp{}, nil, nil)
	if fs := observ.SessionFailures(); len(fs) != 0 {
		t.Fatalf("cancellation must not be recorded, got %+v", fs)
	}
}
