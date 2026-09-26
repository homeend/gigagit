package tui

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
)

// useTestTasks installs a fresh task manager with no history store (records
// fall back to memory) for one SERIAL test — the manager is process-global.
func useTestTasks(t *testing.T) *domain.TaskManager {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	mgr := domain.NewTaskManager(nil)
	restore := domain.UseTaskManager(mgr)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		mgr.KillAll(ctx)
		restore()
	})
	return mgr
}

// waitTaskState polls the global manager until ok holds for task id.
func waitTaskState(t *testing.T, id domain.TaskID, ok func(domain.TaskInfo) bool) domain.TaskInfo {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		info, found := domain.Tasks().Get(id)
		if found && ok(info) {
			return info
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %s: timed out, last %+v", id, info)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func taskEndedFn(i domain.TaskInfo) bool { return !i.State.Live() }

// captureCmd is a headless command block of kind running a shell line.
func captureCmd(kind exttool.Category, line string) config.ToolCommand {
	return config.ToolCommand{Category: string(kind), Name: "Test agent", Mode: "capture", Command: line}
}

func TestApplyTasksConfigSetsCapAndWarnsOnce(t *testing.T) {
	mgr := useTestTasks(t)
	m := newTestModel(t)
	m.cfg.Tasks.MaxParallel = 42
	m, _ = m.applyTasksConfig()
	if mgr.Max() != 10 {
		t.Fatalf("cap = %d, want 10", mgr.Max())
	}
	if !strings.Contains(m.statusMsg, "max_parallel") {
		t.Fatalf("status %q: want the clamp warning", m.statusMsg)
	}
	m.statusMsg = ""
	m, _ = m.applyTasksConfig()
	if m.statusMsg != "" {
		t.Fatalf("warning repeated: %q", m.statusMsg)
	}
	m.cfg.Tasks.MaxParallel = 2
	m, _ = m.applyTasksConfig()
	if mgr.Max() != 2 || m.statusMsg != "" {
		t.Fatalf("cap %d status %q", mgr.Max(), m.statusMsg)
	}
}

func TestOnTasksChangedReportsFailureOnce(t *testing.T) {
	useTestTasks(t)
	m := newTestModel(t)
	spec, err := m.svc.CommitMessageTask(context.Background(), captureCmd(exttool.CatCommitMessage, "exit 3"))
	if err != nil {
		t.Fatal(err)
	}
	id := domain.Tasks().Submit(spec)
	waitTaskState(t, id, taskEndedFn)
	m, _ = m.onTasksChanged()
	if !strings.Contains(m.statusMsg, "failed") {
		t.Fatalf("status %q: want a failure notice", m.statusMsg)
	}
	m.statusMsg = ""
	m, _ = m.onTasksChanged()
	if m.statusMsg != "" {
		t.Fatalf("failure reported twice: %q", m.statusMsg)
	}
}
