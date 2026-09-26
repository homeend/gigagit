package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/promptstate"
)

// commitGenTestModel builds a Model with a staged change and one configured,
// approved commit_message capture tool — the precondition for startGenerate
// to reach dispatch straight through the Task 7 gates (a single tool skips
// the chooser, a pre-approved command skips the approval gate, and empty
// title/desc fields skip the confirm-replace gate). Individual Task 7 tests
// override cfg.Tools.Command / promptStore / popup fields to re-engage one
// gate at a time.
func commitGenTestModel(t *testing.T) Model {
	t.Helper()
	m := New(nil)
	m.status = model.WorkingTreeStatus{
		Files: []model.FileStatus{{Path: "a.go", Staged: 'M', Unstaged: '.'}},
	}
	m.cfg.Tools.Command = []config.ToolCommand{
		{Category: "commit_message", Name: "Claude", Mode: "capture", Command: "echo hi"},
	}
	// A temp promptstate store — never the real machine file (see
	// tool_approval_test.go's promptTestModel) — with the sole tool
	// pre-approved so the Task-6 dispatch tests below are unaffected by the
	// Task-7 approval gate.
	m.promptStore = promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	m.rememberToolApproval(m.cfg.Tools.Command[0].Command)
	return m
}

func TestCommitNormalWidthAndMaximize(t *testing.T) {
	t.Parallel()
	// The wider-than-standard default (B ii).
	if got := commitNormalWidth(120); got != 96 { // capped
		t.Fatalf("commitNormalWidth(120) = %d, want 96", got)
	}
	if got := commitNormalWidth(60); got != 52 { // termW-8
		t.Fatalf("commitNormalWidth(60) = %d, want 52", got)
	}
	// ctrl+t maximizing goes through the shared popupMax mechanism: the commit
	// popup embeds popupMax (so it is a maximizableLayer the central ctrl+t
	// handler drives) and resolves its width via popupResolveWidth.
	p := &commitPopup{}
	if p.maxed() {
		t.Fatal("default not maximized")
	}
	p.toggleMaximize()
	if !p.maxed() {
		t.Fatal("toggleMaximize must maximize")
	}
	// Maximized width exceeds the normal (capped) width on a wide terminal.
	if popupResolveWidth(120, p.maximized, commitNormalWidth(120)) <= commitNormalWidth(120) {
		t.Fatal("maximized width should exceed the normal capped width")
	}
}

func TestGenSpinnerAdvancesAndSelfStops(t *testing.T) {
	t.Parallel()
	m := commitGenTestModel(t)
	p := &commitPopup{}
	m = m.pushLayer(p)
	p.generating, p.genGen = true, 1

	// A matching in-flight tick advances the frame and reschedules.
	if _, cmd := m.tickGenSpinner(genSpinMsg{gen: 1}); cmd == nil || p.spinFrame != 1 {
		t.Fatalf("in-flight tick: spinFrame=%d cmd==nil=%v", p.spinFrame, cmd == nil)
	}
	// A stale-gen tick self-stops (no reschedule).
	if _, cmd := m.tickGenSpinner(genSpinMsg{gen: 0}); cmd != nil {
		t.Fatal("stale-gen tick must not reschedule")
	}
	// Once the run ends, a tick self-stops.
	p.generating = false
	if _, cmd := m.tickGenSpinner(genSpinMsg{gen: 1}); cmd != nil {
		t.Fatal("finished run: tick must not reschedule")
	}
}

func TestGenerateNoOpGuardsNothingStaged(t *testing.T) {
	t.Parallel()
	m := commitGenTestModel(t)
	m.status = model.WorkingTreeStatus{} // nothing staged
	p := &commitPopup{}
	m = m.pushLayer(p)
	m, cmd := m.startGenerate(p)
	if cmd != nil || p.generating || layerOf[*taskLaunchPopup](m) != nil {
		t.Fatal("nothing-staged must no-op")
	}
	if m.statusMsg == "" {
		t.Fatal("want a hint")
	}
}

// commitBoxModel: a real repo with a staged file, one approved capture
// commit_message command running line, and an empty commit box on top.
func commitBoxModel(t *testing.T, line string) Model {
	t.Helper()
	m := launchTestModel(t)
	stageTestFile(t, m)
	m.cfg.Tools.Command = []config.ToolCommand{captureCmd(exttool.CatCommitMessage, line)}
	approveAll(m)
	return m.pushLayer(&commitPopup{})
}

// stageTestFile writes and stages one file and refreshes m.status.
func stageTestFile(t *testing.T, m Model) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(m.currentWorktree, "gen.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", m.currentWorktree, "add", "gen.txt").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
}

// submitCommitTask submits the configured commit-message command headless.
func submitCommitTask(t *testing.T, m Model) domain.TaskID {
	t.Helper()
	spec, err := m.svc.CommitMessageTask(context.Background(), m.cfg.Tools.Command[0])
	if err != nil {
		t.Fatal(err)
	}
	return domain.Tasks().Submit(spec)
}

// startBoxGenerate drives ctrl+g → dialog → enter (the only, headless row).
func startBoxGenerate(t *testing.T, m Model) Model {
	t.Helper()
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "gen.txt", Staged: 'A', Unstaged: '.'}}}
	m, cmd := updateKey(m, "ctrl+g")
	m = deliver(t, m, cmd)
	if layerOf[*taskLaunchPopup](m) == nil {
		t.Fatalf("ctrl+g must open the launch dialog (status %q)", m.statusMsg)
	}
	m, cmd = updateKey(m, "enter")
	return deliver(t, m, cmd)
}

func TestCommitBoxGenerateHeadlessFillsBox(t *testing.T) {
	m := commitBoxModel(t, `printf 'feat: x\n\nbody\n'`)
	m = startBoxGenerate(t, m)
	p := m.topCommitPopup()
	if p == nil || !p.generating || p.genTask == "" {
		t.Fatalf("box not waiting on a task: %+v", p)
	}
	waitTaskState(t, p.genTask, taskEndedFn)
	m, _ = m.onTasksChanged()
	p = m.topCommitPopup()
	if p.generating || p.title.Value() != "feat: x" || p.desc.Value() != "body" {
		t.Fatalf("title %q desc %q generating %v", p.title.Value(), p.desc.Value(), p.generating)
	}
}

func TestCommitBoxResultAsksBeforeReplacing(t *testing.T) {
	m := commitBoxModel(t, `echo "feat: new"`)
	p := m.topCommitPopup()
	p.title = newTextField("mine")
	id := submitCommitTask(t, m)
	waitTaskState(t, id, taskEndedFn)
	m, _ = m.onTasksChanged()
	if p.title.Value() != "mine" || p.offer == "" {
		t.Fatalf("must ask first: title %q offer %q", p.title.Value(), p.offer)
	}
	m, _ = updateKey(m, "y")
	if p.title.Value() != "feat: new" || p.offer != "" {
		t.Fatalf("after y: %q", p.title.Value())
	}
}

func TestCommitMessageWithBoxClosedBecomesPending(t *testing.T) {
	m := commitBoxModel(t, `echo "feat: later"`)
	m = m.popLayer()
	id := submitCommitTask(t, m)
	waitTaskState(t, id, taskEndedFn)
	m, _ = m.onTasksChanged()
	if !strings.Contains(m.statusMsg, "press c") {
		t.Fatalf("status %q", m.statusMsg)
	}
	m = m.openCommitBox()
	if p := m.topCommitPopup(); p == nil || p.title.Value() != "feat: later" {
		t.Fatalf("c must open the box filled: %+v", p)
	}
	if _, left := m.pendingCommitMsg[filepath.Clean(m.currentWorktree)]; left {
		t.Fatal("the pending message is consumed")
	}
}

func TestCommitBoxEscCancelsItsTask(t *testing.T) {
	m := commitBoxModel(t, "sleep 5; echo late")
	m = startBoxGenerate(t, m)
	id := m.topCommitPopup().genTask
	m, _ = updateKey(m, "esc")
	info := waitTaskState(t, id, taskEndedFn)
	if info.State != domain.TaskCancelled || m.topCommitPopup() == nil || m.topCommitPopup().generating {
		t.Fatalf("state %s", info.State)
	}
}

func TestCommitBoxCtrlBSendsToBackground(t *testing.T) {
	m := commitBoxModel(t, "sleep 0.3; echo \"feat: bg\"")
	m = startBoxGenerate(t, m)
	id := m.topCommitPopup().genTask
	m, _ = updateKey(m, "ctrl+b")
	if m.topCommitPopup() != nil {
		t.Fatal("ctrl+b closes the box")
	}
	waitTaskState(t, id, taskEndedFn)
	m, _ = m.onTasksChanged()
	if pm, ok := m.pendingCommitMsg[filepath.Clean(m.currentWorktree)]; !ok || pm.text != "feat: bg" {
		t.Fatalf("pending %+v %v", pm, ok)
	}
}
