package domain

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/repogate"
)

// scriptOp is a CaptureTask whose Prepare hands out a shell line and a
// fresh $GG_MESSAGE_FILE — an interactive "agent" that is really sh.
type scriptOp struct{ line string }

func (scriptOp) LockMode() repogate.Mode { return repogate.Read }
func (o scriptOp) Prepare(context.Context, engine.OpDeps) (engine.TaskInputs, error) {
	f, err := os.CreateTemp("", "gg-task-test-*.txt")
	if err != nil {
		return engine.TaskInputs{}, err
	}
	f.Close()
	return engine.TaskInputs{
		Command: o.line, Env: []string{"GG_MESSAGE_FILE=" + f.Name()}, MessageFile: f.Name(),
		Cleanup: func() { os.Remove(f.Name()) },
	}, nil
}
func (scriptOp) Collect(in engine.TaskInputs, _ []byte) (engine.Result, error) {
	b, _ := os.ReadFile(in.MessageFile)
	return engine.Result{Captured: string(b)}, nil
}
func (o scriptOp) Run(context.Context, engine.OpDeps) (engine.Result, error) {
	return engine.Result{}, nil
}

func interactiveSpec(svc *Service, key, line string) TaskSpec {
	top, _ := svc.TopLevel(context.Background())
	return TaskSpec{Key: key, Kind: exttool.CatCommitMessage, Agent: "Fake", Mode: TaskInteractive,
		Svc: svc, Worktree: top, Cols: 80, Rows: 24, Op: scriptOp{line: line}}
}

// lowerTaskPoll shortens the result poll for this test. Registered before
// newTestTasks, its restore runs after the manager's cleanup has ended every
// task (t.Cleanup is LIFO).
func lowerTaskPoll(t *testing.T) {
	prev := taskResultPoll
	taskResultPoll = 50 * time.Millisecond
	t.Cleanup(func() { taskResultPoll = prev })
}

func skipOnWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell scripts")
	}
}

func TestInteractiveTaskPicksUpEachDistinctWrite(t *testing.T) {
	skipOnWindows(t) // not parallel: it lowers the package poll interval
	lowerTaskPoll(t)
	m, svc := newTestTasks(t)
	// "one", then "two", then "two" again (same content: no new result),
	// then a long wait — the agent stays up.
	id := m.Submit(interactiveSpec(svc, "k", `printf one > "$GG_MESSAGE_FILE"; sleep 1; printf two > "$GG_MESSAGE_FILE"; sleep 1; printf two > "$GG_MESSAGE_FILE"; sleep 30`))
	info := waitInfo(t, m, id, "two results", func(i TaskInfo) bool { return i.Results == 2 })
	if info.State != TaskResultReady || info.Result != "two" || info.Session == "" {
		t.Fatalf("info = %+v", info)
	}
	time.Sleep(1500 * time.Millisecond)
	if info, _ = m.Get(id); info.Results != 2 {
		t.Fatalf("an identical rewrite raised another result: %+v", info)
	}
	if err := m.Cancel(id); err != nil {
		t.Fatal(err)
	}
	waitInfo(t, m, id, "done after a result", stateIs(TaskDone))
}

func TestInteractiveTaskEndStates(t *testing.T) {
	skipOnWindows(t)
	lowerTaskPoll(t)
	m, svc := newTestTasks(t)
	noResult := m.Submit(interactiveSpec(svc, "a", `exit 0`))
	waitInfo(t, m, noResult, "failed without a result", stateIs(TaskFailed))

	spec := interactiveSpec(svc, "b", `exit 0`)
	spec.ResultOptional = true
	waitInfo(t, m, m.Submit(spec), "optional: done", stateIs(TaskDone))

	lastWrite := m.Submit(interactiveSpec(svc, "c", `printf final > "$GG_MESSAGE_FILE"; exit 0`))
	if info := waitInfo(t, m, lastWrite, "done", stateIs(TaskDone)); info.Result != "final" {
		t.Fatalf("a write just before exit was lost: %+v", info)
	}

	killed := m.Submit(interactiveSpec(svc, "d", `sleep 30`))
	waitInfo(t, m, killed, "running", func(i TaskInfo) bool { return i.Session != "" })
	if err := m.Cancel(killed); err != nil {
		t.Fatal(err)
	}
	waitInfo(t, m, killed, "cancelled before a result", stateIs(TaskCancelled))

	crashed := m.Submit(interactiveSpec(svc, "e", `exit 3`))
	if info := waitInfo(t, m, crashed, "failed", stateIs(TaskFailed)); info.ExitCode != 3 {
		t.Fatalf("exit code = %d", info.ExitCode)
	}
}

func TestResultWatcherStableRead(t *testing.T) {
	t.Parallel()
	f, err := os.CreateTemp(t.TempDir(), "msg-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	w := newResultWatcher(f.Name())
	defer w.close()
	if w.changed() {
		t.Fatal("an empty file is not a result")
	}
	os.WriteFile(f.Name(), []byte("a"), 0o644)
	if !w.changed() {
		t.Fatal("new content must be a result")
	}
	if w.changed() {
		t.Fatal("unchanged content must not be a second result")
	}
	if newResultWatcher("").wake() != nil {
		t.Fatal("no message file = a watcher that never wakes")
	}
}
