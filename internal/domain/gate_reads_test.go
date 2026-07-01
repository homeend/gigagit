package domain

import (
	"context"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repogate"
)

// holdTreeWrite acquires an exclusive reservation on svc's gate so a test can
// assert that a domain read queues behind it (i.e. is actually gated).
func holdTreeWrite(t *testing.T, svc *Service) *repogate.Reservation {
	t.Helper()
	res, err := svc.gateFor(context.Background()).Acquire(context.Background(), repogate.TreeWrite, "test hold")
	if err != nil {
		t.Fatalf("acquire TreeWrite: %v", err)
	}
	return res
}

// assertBlocksUntilRelease runs fn in a goroutine and asserts it does NOT
// complete while the reservation is held, then completes once released.
func assertBlocksUntilRelease(t *testing.T, res *repogate.Reservation, name string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { fn(); close(done) }()
	select {
	case <-done:
		res.Release()
		t.Fatalf("%s completed while a TreeWrite reservation was held — the read is not gated", name)
	case <-time.After(75 * time.Millisecond):
	}
	res.Release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not complete after the reservation was released", name)
	}
}

func TestConflictRunsUnderReadReservation(t *testing.T) {
	svc := New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	st := model.WorkingTreeStatus{
		Branch: "main",
		Files:  []model.FileStatus{{Path: "x.txt", Kind: model.KindUnmerged}},
	}
	res := holdTreeWrite(t, svc)
	assertBlocksUntilRelease(t, res, "Conflict", func() {
		svc.Conflict(context.Background(), st)
	})
}

func TestConflictCleanStatusSkipsGate(t *testing.T) {
	svc := New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	res := holdTreeWrite(t, svc)
	defer res.Release()

	done := make(chan ConflictState, 1)
	go func() { done <- svc.Conflict(context.Background(), model.WorkingTreeStatus{Branch: "main"}) }()
	select {
	case cs := <-done:
		if cs != (ConflictState{}) {
			t.Fatalf("clean status Conflict = %+v, want zero", cs)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Conflict on a clean status blocked on the gate — the no-probe short-circuit must not reserve")
	}
}

func TestBookmarkBytesRunsUnderReadReservation(t *testing.T) {
	svc, f := bmSvc(t)
	f.SetResponse("git cat-file blob", gitexec.Result{Stdout: "frozen\n"})
	res := holdTreeWrite(t, svc)
	assertBlocksUntilRelease(t, res, "BookmarkBytes", func() {
		// Path must be set: a path-less committed bookmark is a commit
		// pointer (IsCommit) and errors out before any git read.
		_, _ = svc.BookmarkBytes(context.Background(), model.Bookmark{
			State: model.StateCommitted, SHA: "abc123", Path: "a/b.go",
		})
	})
}

func TestBookmarkAddRunsUnderReadReservation(t *testing.T) {
	svc, f := bmSvc(t)
	f.SetResponse("git rev-parse blob", gitexec.Result{Stdout: "abc123sha\n"})
	res := holdTreeWrite(t, svc)
	assertBlocksUntilRelease(t, res, "BookmarkAdd", func() {
		_, _ = svc.BookmarkAdd(context.Background(), model.Bookmark{
			State: model.StateCommitted, Commit: "c0ffee", Path: "a/b.go",
		})
	})
}
