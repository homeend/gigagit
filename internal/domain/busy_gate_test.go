package domain

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/repogate"
)

// blockingTask is a CaptureTask whose Run parks until released — the shape
// of a headless agent run, minus the agent.
type blockingTask struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingTask) LockMode() repogate.Mode { return repogate.Read }
func (b *blockingTask) Prepare(context.Context, engine.OpDeps) (engine.TaskInputs, error) {
	return engine.TaskInputs{Cleanup: func() {}}, nil
}
func (b *blockingTask) Collect(engine.TaskInputs, []byte) (engine.Result, error) {
	return engine.Result{}, nil
}
func (b *blockingTask) Run(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
	b.once.Do(func() { close(b.started) })
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return engine.Result{}, nil
}

var _ engine.CaptureTask = (*blockingTask)(nil)

// A running headless task refuses an exclusive op immediately with a
// BusyError naming it, lets a ref-only op through, and is ordinary again
// once it ends.
func TestExecuteRefusesExclusiveOpWhileCaptureTaskRuns(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git check-ref-format", gitexec.Result{})
	f.SetResponse("git branch", gitexec.Result{})
	svc := New(&git.Repo{Runner: f})
	ctx := context.Background()
	task := &blockingTask{started: make(chan struct{}), release: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = svc.Execute(ctx, task, nil, nil)
	}()
	<-task.started

	start := time.Now()
	_, err := svc.Execute(ctx, engine.Commit{Message: "x"}, nil, nil)
	var busy *repogate.BusyError
	if !errors.As(err, &busy) {
		t.Fatalf("expected BusyError, got %v", err)
	}
	if busy.Holder != "op blockingTask" {
		t.Fatalf("BusyError must name the task: %+v", busy)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("the refusal must not wait")
	}

	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := svc.Execute(c, engine.CreateBranch{Name: "feat/x"}, nil, nil); err != nil {
		t.Fatalf("a ref-only op must run beside the task: %v", err)
	}

	close(task.release)
	<-done
	// The commit now queues/runs normally (the fake has no "git commit"
	// response, so the op fails inside Run — but not with BusyError).
	_, err = svc.Execute(ctx, engine.Commit{Message: "x"}, nil, nil)
	if errors.As(err, &busy) {
		t.Fatalf("after the task ends nothing is busy: %v", err)
	}
}
