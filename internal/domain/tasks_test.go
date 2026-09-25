package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/repogate"
	"github.com/homeend/gigagit/internal/taskhist"
)

// blockOp is a CaptureTask whose headless Run waits for release (or its
// ctx) and returns out/err — the scheduler's fake workload.
type blockOp struct {
	started chan string // receives key when Run begins (buffered)
	release chan struct{}
	key     string
	out     string
	err     error
}

func (blockOp) LockMode() repogate.Mode { return repogate.Read }
func (blockOp) Prepare(context.Context, engine.OpDeps) (engine.TaskInputs, error) {
	return engine.TaskInputs{Cleanup: func() {}}, nil
}
func (blockOp) Collect(_ engine.TaskInputs, stdout []byte) (engine.Result, error) {
	return engine.Result{Captured: string(stdout)}, nil
}
func (o blockOp) Run(ctx context.Context, _ engine.OpDeps) (engine.Result, error) {
	o.started <- o.key
	select {
	case <-o.release:
	case <-ctx.Done():
		return engine.Result{}, ctx.Err()
	}
	return engine.Result{Captured: o.out}, o.err
}

func newTestTasks(t *testing.T) (*TaskManager, *Service) {
	t.Helper()
	_, svc := newRealRepo(t)
	m := NewTaskManager(taskhist.NewFileStore(t.TempDir()))
	m.sessions = agentsession.NewManager()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		m.KillAll(ctx)
		m.sessions.KillAll(ctx)
	})
	return m, svc
}

func headlessSpec(svc *Service, key string, op engine.CaptureTask) TaskSpec {
	return TaskSpec{Key: key, Kind: exttool.CatReview, Agent: "Fake", Mode: TaskHeadless, Svc: svc, Op: op}
}

func waitInfo(t *testing.T, m *TaskManager, id TaskID, what string, ok func(TaskInfo) bool) TaskInfo {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if info, found := m.Get(id); found && ok(info) {
			return info
		}
		time.Sleep(10 * time.Millisecond)
	}
	info, _ := m.Get(id)
	t.Fatalf("timed out waiting for %s: %+v", what, info)
	return info
}

func stateIs(s TaskState) func(TaskInfo) bool { return func(i TaskInfo) bool { return i.State == s } }

func recv(t *testing.T, ch chan string) string {
	t.Helper()
	select {
	case k := <-ch:
		return k
	case <-time.After(10 * time.Second):
		t.Fatal("no task started")
		return ""
	}
}

func notRecv(t *testing.T, ch chan string) {
	t.Helper()
	select {
	case k := <-ch:
		t.Fatalf("task %q started, want it queued", k)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestTasksSameKeyRunInSequence(t *testing.T) {
	t.Parallel()
	m, svc := newTestTasks(t)
	started := make(chan string, 4)
	r1, r2 := make(chan struct{}), make(chan struct{})
	a := m.Submit(headlessSpec(svc, "review — k", blockOp{started: started, release: r1, key: "a", out: "ra"}))
	b := m.Submit(headlessSpec(svc, "review — k", blockOp{started: started, release: r2, key: "b", out: "rb"}))
	if k := recv(t, started); k != "a" {
		t.Fatalf("first started = %q", k)
	}
	notRecv(t, started)
	if info, _ := m.Get(b); info.State != TaskQueued {
		t.Fatalf("b = %s, want queued behind a", info.State)
	}
	close(r1)
	waitInfo(t, m, a, "a done", stateIs(TaskDone))
	if k := recv(t, started); k != "b" {
		t.Fatalf("second started = %q", k)
	}
	close(r2)
	if info := waitInfo(t, m, b, "b done", stateIs(TaskDone)); info.Result != "rb" || info.Results != 1 {
		t.Fatalf("b = %+v", info)
	}
}

func TestTasksDifferentKeysRunInParallelWithinTheCap(t *testing.T) {
	t.Parallel()
	m, svc := newTestTasks(t)
	m.SetMaxParallel(2)
	started := make(chan string, 4)
	rel := make(chan struct{})
	defer close(rel)
	m.Submit(headlessSpec(svc, "k1", blockOp{started: started, release: rel, key: "1"}))
	m.Submit(headlessSpec(svc, "k2", blockOp{started: started, release: rel, key: "2"}))
	c := m.Submit(headlessSpec(svc, "k3", blockOp{started: started, release: rel, key: "3"}))
	recv(t, started)
	recv(t, started)
	notRecv(t, started)
	if info, _ := m.Get(c); info.State != TaskQueued {
		t.Fatalf("third = %s, want queued at the cap", info.State)
	}
}

func TestTasksMaxParallelClamp(t *testing.T) {
	t.Parallel()
	m := NewTaskManager(taskhist.NewMemStore())
	if m.Max() != 3 {
		t.Fatalf("default = %d", m.Max())
	}
	for in, want := range map[int]int{0: 1, -4: 1, 7: 7, 11: 10, 99: 10} {
		m.SetMaxParallel(in)
		if m.Max() != want {
			t.Errorf("SetMaxParallel(%d) → %d, want %d", in, m.Max(), want)
		}
	}
}

func TestTasksCancelQueuedAndRunning(t *testing.T) {
	t.Parallel()
	m, svc := newTestTasks(t)
	m.SetMaxParallel(1)
	started := make(chan string, 4)
	rel := make(chan struct{})
	defer close(rel)
	run := m.Submit(headlessSpec(svc, "k1", blockOp{started: started, release: rel, key: "run"}))
	queued := m.Submit(headlessSpec(svc, "k2", blockOp{started: started, release: rel, key: "queued"}))
	recv(t, started)
	if err := m.Cancel(queued); err != nil {
		t.Fatal(err)
	}
	waitInfo(t, m, queued, "queued cancelled", stateIs(TaskCancelled))
	if err := m.Cancel(run); err != nil {
		t.Fatal(err)
	}
	waitInfo(t, m, run, "running cancelled", stateIs(TaskCancelled))
	notRecv(t, started) // the cancelled queued task never starts
	h := m.History()    // the running task's record lands before its end state shows
	if len(h) != 2 || h[0].State != "cancelled" || h[1].State != "cancelled" {
		t.Fatalf("history = %+v", h)
	}
}

func TestTasksHeadlessFailuresKeepTheTail(t *testing.T) {
	t.Parallel()
	m, svc := newTestTasks(t)
	started := make(chan string, 4)
	rel := make(chan struct{})
	close(rel)
	id := m.Submit(headlessSpec(svc, "k", blockOp{started: started, release: rel, key: "x", out: "partial output", err: errors.New("boom")}))
	info := waitInfo(t, m, id, "failed", stateIs(TaskFailed))
	if info.Err != "boom" || !strings.Contains(info.Tail, "partial output") {
		t.Fatalf("info = %+v", info)
	}
	if tail, _ := m.HistoryTail(string(id)); !strings.Contains(tail, "partial output") {
		t.Fatalf("history tail = %q", tail)
	}
	empty := m.Submit(headlessSpec(svc, "k2", blockOp{started: started, release: rel, key: "y", out: "  "}))
	waitInfo(t, m, empty, "empty result fails", stateIs(TaskFailed))
	spec := headlessSpec(svc, "k3", blockOp{started: started, release: rel, key: "z", out: ""})
	spec.ResultOptional = true
	waitInfo(t, m, m.Submit(spec), "optional result done", stateIs(TaskDone))
}

func TestTasksHeadlessRealCommandInTheSubmittingWorktree(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell command")
	}
	m, _ := newTestTasks(t)
	dirA, svcA := newRealRepo(t)
	_, _ = newRealRepo(t) // the "other" worktree the frontend moved to
	if err := os.WriteFile(filepath.Join(dirA, "n.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dirA, "add", "n.txt")
	spec := TaskSpec{
		Key: "commit message — a", Kind: exttool.CatCommitMessage, Mode: TaskHeadless, Svc: svcA,
		Op:    engine.GenerateMessage{Command: `printf 'Subject in %s\n' "$(pwd)"; echo warn >&2`, Dir: dirA},
		Parse: parseCommitMessage,
	}
	info := waitInfo(t, m, m.Submit(spec), "done", stateIs(TaskDone))
	if !strings.Contains(info.Result, filepath.Base(dirA)) {
		t.Fatalf("result = %q, want it produced in %s", info.Result, dirA)
	}
	if r, _ := m.HistoryResult(string(info.ID)); r != info.Result {
		t.Fatalf("history result = %q", r)
	}
	spec.Op = engine.GenerateMessage{Command: `echo oops >&2; exit 3`, Dir: dirA}
	failed := waitInfo(t, m, m.Submit(spec), "failed", stateIs(TaskFailed))
	if failed.ExitCode != 3 || !strings.Contains(failed.Tail, "oops") {
		t.Fatalf("failed = %+v", failed)
	}
}

func TestTasksChangedSignals(t *testing.T) {
	t.Parallel()
	m, svc := newTestTasks(t)
	started := make(chan string, 1)
	rel := make(chan struct{})
	close(rel)
	id := m.Submit(headlessSpec(svc, "k", blockOp{started: started, release: rel, key: "x", out: "r"}))
	select {
	case <-m.Changed():
	case <-time.After(5 * time.Second):
		t.Fatal("Submit did not signal Changed")
	}
	waitInfo(t, m, id, "done", stateIs(TaskDone))
	if l := m.List(); len(l) != 1 || l[0].ID != id {
		t.Fatalf("List = %+v", l)
	}
}
