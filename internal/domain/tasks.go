package domain

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/taskhist"
)

type TaskID string

type TaskState string

const (
	TaskQueued      TaskState = "queued"
	TaskRunning     TaskState = "running"
	TaskResultReady TaskState = "result-ready" // interactive, ≥1 result, still running
	TaskDone        TaskState = "done"
	TaskFailed      TaskState = "failed"
	TaskCancelled   TaskState = "cancelled"
)

// Live reports a task that is queued or holds a slot.
func (s TaskState) Live() bool { return s == TaskQueued || s == TaskRunning || s == TaskResultReady }

func (s TaskState) running() bool { return s == TaskRunning || s == TaskResultReady }

// TaskRecord is a finished task in the history (frontends may not import
// internal/taskhist).
type TaskRecord = taskhist.Record

// TaskInfo is a task's live view. Results counts results so far: a
// frontend applies each new one exactly once by remembering the count it
// last saw.
type TaskInfo struct {
	ID        TaskID
	Key       string
	Kind      exttool.Category
	Agent     string
	Repo      string
	Worktree  string
	Mode      TaskMode
	State     TaskState
	Submitted time.Time
	Started   time.Time
	Ended     time.Time
	Session   SessionID // interactive: the task's session once started
	Result    string    // the latest result
	Results   int
	ExitCode  int
	Err       string
	Tail      string // headless output tail (≤ taskhist.MaxTail)
}

type task struct {
	info      TaskInfo
	spec      TaskSpec
	cancel    context.CancelFunc
	cancelled bool
}

type taskEnd struct {
	state TaskState
	exit  int
	err   string
	tail  string
}

// TaskManager runs AI tasks: FIFO per key, at most max running at once
// across every mode (ruling 5/6). Live tasks are in memory; a task that
// ends is written to the history.
type TaskManager struct {
	mu       sync.Mutex
	max      int
	tasks    []*task // submit order; ended ones trimmed to taskhist.Max
	changed  chan struct{}
	wg       sync.WaitGroup
	sessions *agentsession.Manager // nil = Sessions()

	histMu   sync.Mutex
	hist     taskhist.Store
	fallback *taskhist.MemStore // set once the store failed a write
	problem  error              // that failure, until TakeStoreProblem
}

// NewTaskManager returns a manager recording into hist, cap 3.
func NewTaskManager(hist taskhist.Store) *TaskManager {
	return &TaskManager{max: 3, hist: hist, changed: make(chan struct{}, 1)}
}

// SetMaxParallel sets the cap, clamped to 1..config.MaxParallelCap, and
// starts whatever now fits.
func (m *TaskManager) SetMaxParallel(n int) {
	n = min(max(n, 1), config.MaxParallelCap)
	m.mu.Lock()
	m.max = n
	m.pumpLocked()
	m.mu.Unlock()
	m.signal()
}

func (m *TaskManager) Max() int { m.mu.Lock(); defer m.mu.Unlock(); return m.max }

// Changed is a coalesced "something changed" signal (the Sessions pattern).
func (m *TaskManager) Changed() <-chan struct{} { return m.changed }

func (m *TaskManager) signal() {
	select {
	case m.changed <- struct{}{}:
	default:
	}
}

func (m *TaskManager) sessionMgr() *agentsession.Manager {
	if m.sessions != nil {
		return m.sessions
	}
	return Sessions()
}

// Submit queues spec and starts it when its key is free and a slot is.
func (m *TaskManager) Submit(spec TaskSpec) TaskID {
	now := time.Now()
	t := &task{spec: spec, info: TaskInfo{
		ID: TaskID(taskhist.NewID(now)), Key: spec.Key, Kind: spec.Kind, Agent: spec.Agent,
		Repo: spec.Repo, Worktree: spec.Worktree, Mode: spec.Mode, State: TaskQueued, Submitted: now,
	}}
	m.mu.Lock()
	m.tasks = append(m.tasks, t)
	m.pumpLocked()
	m.mu.Unlock()
	m.signal()
	return t.info.ID
}

// pumpLocked starts queued tasks in submit order while slots remain; a key
// that is running (or has an earlier queued task) waits.
func (m *TaskManager) pumpLocked() {
	running := 0
	busy := map[string]bool{}
	for _, t := range m.tasks {
		if t.info.State.running() {
			running++
			busy[t.spec.Key] = true
		}
	}
	for _, t := range m.tasks {
		if t.info.State != TaskQueued {
			continue
		}
		if busy[t.spec.Key] {
			continue
		}
		busy[t.spec.Key] = true // later same-key tasks queue behind this one
		if running >= m.max {
			continue
		}
		running++
		m.startLocked(t)
	}
}

func (m *TaskManager) startLocked(t *task) {
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	t.info.State = TaskRunning
	t.info.Started = time.Now()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer cancel()
		var end taskEnd
		if t.spec.Mode == TaskInteractive {
			end = m.runInteractive(ctx, t)
		} else {
			end = m.runHeadless(ctx, t)
		}
		m.finish(t, end)
	}()
}

// parse applies the spec's Parse (TrimSpace when nil).
func (t *task) parse(captured string) (string, error) {
	if t.spec.Parse == nil {
		return strings.TrimSpace(captured), nil
	}
	return t.spec.Parse(captured)
}

// setResult records a new result and signals.
func (m *TaskManager) setResult(t *task, result string) {
	m.mu.Lock()
	t.info.Result = result
	t.info.Results++
	if t.spec.Mode == TaskInteractive && t.info.State == TaskRunning {
		t.info.State = TaskResultReady
	}
	m.mu.Unlock()
	m.signal()
}

func (m *TaskManager) runHeadless(ctx context.Context, t *task) taskEnd {
	events := make(chan engine.Event, 64)
	var tail strings.Builder
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for ev := range events {
			if gl, ok := ev.(engine.GitLine); ok {
				tail.WriteString(gl.Raw + "\n")
			}
		}
	}()
	res, err := t.spec.Svc.Execute(ctx, t.spec.Op, events, nil)
	close(events)
	<-drained
	if strings.TrimSpace(res.Captured) != "" {
		tail.WriteString(res.Captured)
	}
	out := taskhist.TrimTail(tail.String())
	if ctx.Err() != nil {
		return taskEnd{state: TaskCancelled, exit: -1, tail: out}
	}
	if err != nil {
		return taskEnd{state: TaskFailed, exit: exitCodeOf(err), err: err.Error(), tail: out}
	}
	result, perr := t.parse(res.Captured)
	if perr == nil && strings.TrimSpace(result) != "" {
		m.setResult(t, result)
		return taskEnd{state: TaskDone}
	}
	if t.spec.ResultOptional && perr == nil {
		return taskEnd{state: TaskDone}
	}
	msg := "the agent produced no result"
	if perr != nil {
		msg = perr.Error()
	}
	return taskEnd{state: TaskFailed, err: msg, tail: out}
}

func exitCodeOf(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// finish ends t. The record is written BEFORE the end state becomes
// visible, so a frontend that sees the task end also finds its history.
func (m *TaskManager) finish(t *task, end taskEnd) {
	m.mu.Lock()
	info := t.info // the runner is done: no setResult can race this copy
	m.mu.Unlock()
	info.State, info.Ended = end.state, time.Now()
	info.ExitCode, info.Err, info.Tail = end.exit, end.err, end.tail
	m.record(recordOf(info), info.Result, end.tail)
	m.mu.Lock()
	t.info = info
	m.trimEndedLocked()
	m.pumpLocked()
	m.mu.Unlock()
	m.signal()
}

func recordOf(info TaskInfo) TaskRecord {
	started := info.Started
	if started.IsZero() {
		started = info.Submitted
	}
	return TaskRecord{
		ID: string(info.ID), Key: info.Key, Kind: string(info.Kind), Agent: info.Agent,
		Repo: info.Repo, Worktree: info.Worktree, Mode: string(info.Mode), State: string(info.State),
		Started: started.UTC(), Ended: info.Ended.UTC(), ExitCode: info.ExitCode, Err: info.Err,
	}
}

// trimEndedLocked keeps at most taskhist.Max ended tasks in memory.
func (m *TaskManager) trimEndedLocked() {
	ended := 0
	for _, t := range m.tasks {
		if !t.info.State.Live() {
			ended++
		}
	}
	if ended <= taskhist.Max {
		return
	}
	out := m.tasks[:0]
	for _, t := range m.tasks {
		if !t.info.State.Live() && ended > taskhist.Max {
			ended--
			continue
		}
		out = append(out, t)
	}
	m.tasks = out
}

func (m *TaskManager) find(id TaskID) *task {
	for _, t := range m.tasks {
		if t.info.ID == id {
			return t
		}
	}
	return nil
}

// Get returns a task's current view.
func (m *TaskManager) Get(id TaskID) (TaskInfo, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t := m.find(id); t != nil {
		return t.info, true
	}
	return TaskInfo{}, false
}

// List returns live tasks in submit order, then this process's ended tasks
// newest first.
func (m *TaskManager) List() []TaskInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	var live, ended []TaskInfo
	for _, t := range m.tasks {
		if t.info.State.Live() {
			live = append(live, t.info)
		} else {
			ended = append([]TaskInfo{t.info}, ended...)
		}
	}
	return append(live, ended...)
}

// Cancel removes a queued task, or stops a running one (headless: its ctx;
// interactive: its session is killed). Ended tasks are left alone.
func (m *TaskManager) Cancel(id TaskID) error {
	m.mu.Lock()
	t := m.find(id)
	if t == nil {
		m.mu.Unlock()
		return fmt.Errorf("no task %s", id)
	}
	switch {
	case t.info.State == TaskQueued:
		t.info.State = TaskCancelled
		t.info.Ended = time.Now()
		rec := recordOf(t.info)
		m.pumpLocked()
		m.mu.Unlock()
		m.record(rec, "", "")
		m.signal()
		return nil
	case t.info.State.running():
		t.cancelled = true
		cancel := t.cancel
		m.mu.Unlock()
		cancel()
		return nil
	}
	m.mu.Unlock()
	return nil
}

// KillAll cancels every queued and running task and waits (bounded by
// ctx) for the running ones to end — gg's quit path.
func (m *TaskManager) KillAll(ctx context.Context) {
	m.mu.Lock()
	var ids []TaskID
	for _, t := range m.tasks {
		if t.info.State == TaskQueued {
			ids = append(ids, t.info.ID)
		}
	}
	for _, t := range m.tasks {
		if t.info.State.running() {
			ids = append(ids, t.info.ID)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Cancel(id)
	}
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// record writes a finished task to the history; the first failed write
// switches this process to an in-memory history (spec: tasks still run,
// one notice).
func (m *TaskManager) record(r TaskRecord, result, tail string) {
	m.histMu.Lock()
	defer m.histMu.Unlock()
	if m.fallback == nil {
		err := errors.New("no history store")
		if m.hist != nil {
			if err = m.hist.Add(r, result, tail); err == nil {
				return
			}
		}
		m.fallback = taskhist.NewMemStore()
		if m.hist != nil {
			m.problem = err
		}
	}
	_ = m.fallback.Add(r, result, tail)
}

// History lists finished tasks newest first (in-memory fallback records,
// then the store's).
func (m *TaskManager) History() []TaskRecord {
	m.histMu.Lock()
	defer m.histMu.Unlock()
	var out []TaskRecord
	seen := map[string]bool{}
	if m.fallback != nil {
		mem, _ := m.fallback.List()
		for _, r := range mem {
			seen[r.ID] = true
			out = append(out, r)
		}
	}
	if m.hist != nil {
		disk, _ := m.hist.List()
		for _, r := range disk {
			if !seen[r.ID] {
				out = append(out, r)
			}
		}
	}
	return out
}

func (m *TaskManager) historyText(id string, get func(taskhist.Store, string) (string, error)) (string, error) {
	m.histMu.Lock()
	defer m.histMu.Unlock()
	if m.fallback != nil {
		if s, _ := get(m.fallback, id); s != "" {
			return s, nil
		}
	}
	if m.hist == nil {
		return "", nil
	}
	return get(m.hist, id)
}

func (m *TaskManager) HistoryResult(id string) (string, error) {
	return m.historyText(id, taskhist.Store.Result)
}

func (m *TaskManager) HistoryTail(id string) (string, error) {
	return m.historyText(id, taskhist.Store.Tail)
}

// RemoveHistory deletes a finished record (the Headless tab's x).
func (m *TaskManager) RemoveHistory(id string) error {
	m.histMu.Lock()
	defer m.histMu.Unlock()
	if m.fallback != nil {
		_ = m.fallback.Remove(id)
	}
	if m.hist == nil {
		return nil
	}
	return m.hist.Remove(id)
}

func (m *TaskManager) runInteractive(ctx context.Context, t *task) taskEnd {
	return taskEnd{state: TaskFailed, exit: -1, err: "interactive tasks are not implemented yet"}
}
