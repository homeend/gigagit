package tui

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/promptstate"
)

// updateKey feeds one key through Update.
func updateKey(m Model, s string) (Model, tea.Cmd) {
	var k tea.KeyMsg
	switch s {
	case "ctrl+g":
		k = tea.KeyMsg{Type: tea.KeyCtrlG}
	case "b", "y", "k", "x", "a":
		k = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	default:
		k = keyMsg(s)
	}
	nm, cmd := m.Update(k)
	return nm.(Model), cmd
}

// deliver runs cmd and feeds every non-tick message it yields (batches
// unwrapped) back through Update — the dialog's async hops.
func deliver(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	switch v := msg.(type) {
	case tea.BatchMsg:
		for _, c := range v {
			m = deliver(t, m, c)
		}
		return m
	case genSpinMsg, tasksChangedMsg, stickyExpiredMsg:
		return m
	case nil:
		return m
	}
	nm, _ := m.Update(msg)
	return nm.(Model)
}

// approveAll records a first-run approval for every configured command.
func approveAll(m Model) {
	for _, tc := range m.cfg.Tools.Command {
		m.rememberToolApproval(tc.Command)
	}
}

func launchTestModel(t *testing.T) Model {
	t.Helper()
	useTestTasks(t)
	m := newTestModel(t)
	top, err := m.svc.TopLevel(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	m.currentWorktree = top
	m.promptStore = promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	m.cfg.Tools.Command = []config.ToolCommand{
		{Category: "review", Name: "Claude Code", Mode: "capture", Command: "claude -p x"},
		{Category: "review", Name: "Claude Code (interactive)", Mode: "interactive", Command: "claude x"},
		{Category: "review", Name: "Kimi", Mode: "capture", Command: "kimi x"},
	}
	prevDetect, prevLook := agentDetect, taskLookPath
	agentDetect = func() []exttool.Detection { return nil } // no first-run writes
	taskLookPath = func(string) (string, error) { return "/bin/true", nil }
	t.Cleanup(func() { agentDetect, taskLookPath = prevDetect, prevLook })
	return m
}

// readyLaunch opens the dialog and delivers its async prep.
func readyLaunch(t *testing.T, m Model, l taskLaunch) (Model, *taskLaunchPopup) {
	t.Helper()
	m, cmd := m.openTaskLaunch(l)
	m = deliver(t, m, cmd)
	p := layerOf[*taskLaunchPopup](m)
	if p == nil || p.stage != launchChoose {
		t.Fatalf("dialog not ready: %+v (status %q)", p, m.statusMsg)
	}
	return m, p
}

func reviewLaunch() taskLaunch {
	return taskLaunch{kind: exttool.CatReview, review: domain.WorkingReviewTarget()}
}

func TestLaunchDialogRowsAndGreyedModes(t *testing.T) {
	m := launchTestModel(t)
	m, p := readyLaunch(t, m, reviewLaunch())
	if !strings.HasPrefix(p.key, "review — ") {
		t.Fatalf("key %q", p.key)
	}
	if got := len(p.rows); got != 3 { // bg, fg, headless for Claude
		t.Fatalf("claude rows = %d", got)
	}
	m, _ = updateKey(m, "right") // Kimi: headless only
	p = layerOf[*taskLaunchPopup](m)
	if !p.rows[0].none || !p.rows[1].none || p.rows[2].none {
		t.Fatalf("kimi rows: %+v", p.rows)
	}
	if p.rows[p.sel].none {
		t.Fatal("cursor sits on a greyed row")
	}
	if !strings.Contains(p.render(m, ""), "Kimi") {
		t.Fatal("render must name the agent")
	}
}

func TestLaunchDialogRemembersChoice(t *testing.T) {
	m := launchTestModel(t)
	approveAll(m)
	m, _ = readyLaunch(t, m, reviewLaunch())
	m, _ = updateKey(m, "down") // Claude fg
	m, cmd := updateKey(m, "enter")
	if cmd == nil {
		t.Fatal("enter must submit")
	}
	got, ok := m.promptStore.TaskLaunchChoice("review")
	if !ok || got.Mode != "foreground" || got.Command != "Claude Code (interactive)" {
		t.Fatalf("remembered %+v %v", got, ok)
	}
	_, p := readyLaunch(t, m, reviewLaunch())
	if r := p.rows[p.sel]; r.mode != launchForeground || r.tc.Name != "Claude Code (interactive)" {
		t.Fatalf("preselected %+v", r)
	}
}

func TestLaunchDialogApprovalBeforeRun(t *testing.T) {
	m := launchTestModel(t)
	m, _ = readyLaunch(t, m, reviewLaunch())
	m, cmd := updateKey(m, "enter")
	p := layerOf[*taskLaunchPopup](m)
	if cmd != nil || p == nil || p.stage != launchApprove {
		t.Fatal("unapproved command must show the approval box first")
	}
	m, cmd = updateKey(m, "enter")
	if cmd == nil || !m.toolCommandApproved("claude x") {
		t.Fatal("enter approves and submits")
	}
}

func TestLaunchDialogNotFoundRefuses(t *testing.T) {
	m := launchTestModel(t)
	approveAll(m)
	taskLookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	m, _ = readyLaunch(t, m, reviewLaunch())
	m, cmd := updateKey(m, "enter")
	if cmd != nil || !strings.Contains(m.statusMsg, "not found") {
		t.Fatalf("status %q", m.statusMsg)
	}
}

func TestLaunchWaitLine(t *testing.T) {
	t.Parallel()
	if got := launchWaitLine(domain.TaskLoad{SameKey: 1, Running: 1, Max: 3}); !strings.Contains(got, "will wait") {
		t.Fatalf("same key: %q", got)
	}
	if got := launchWaitLine(domain.TaskLoad{Running: 3, Max: 3}); !strings.Contains(got, "when one finishes") {
		t.Fatalf("cap: %q", got)
	}
	if got := launchWaitLine(domain.TaskLoad{Running: 1, Max: 3}); got != "" {
		t.Fatalf("free slot: %q", got)
	}
}

func TestLaunchDialogSubmitsHeadlessTask(t *testing.T) {
	m := launchTestModel(t)
	m.cfg.Tools.Command = []config.ToolCommand{captureCmd(exttool.CatReview, "echo looks good")}
	approveAll(m)
	m, _ = readyLaunch(t, m, reviewLaunch())
	m, cmd := updateKey(m, "enter")
	m = deliver(t, m, cmd)
	if layerOf[*taskLaunchPopup](m) != nil {
		t.Fatal("dialog must close on submit")
	}
	list := domain.Tasks().List()
	if len(list) != 1 || list[0].Mode != domain.TaskHeadless || !strings.HasPrefix(list[0].Key, "review — ") {
		t.Fatalf("tasks %+v", list)
	}
}

func TestLaunchDialogNoAgentConfigured(t *testing.T) {
	m := launchTestModel(t)
	m.cfg.Tools.Command = nil
	m, cmd := m.openTaskLaunch(reviewLaunch())
	m = deliver(t, m, cmd)
	if layerOf[*taskLaunchPopup](m) != nil || !strings.Contains(m.statusMsg, "no review agent") {
		t.Fatalf("status %q", m.statusMsg)
	}
}

func TestCommandProgram(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"claude -p x":            "claude",
		`"/opt/my tool/bin" --x`: "/opt/my tool/bin",
		"'kimi' run":             "kimi",
		"  codex":                "codex",
	} {
		if got := commandProgram(in); got != want {
			t.Errorf("commandProgram(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLaunchApprovalShowsOneHint(t *testing.T) {
	m := launchTestModel(t)
	m, _ = readyLaunch(t, m, reviewLaunch())
	m, _ = updateKey(m, "enter")
	p := layerOf[*taskLaunchPopup](m)
	if got := strings.Count(p.render(m, ""), "[esc]"); got != 1 {
		t.Fatalf("approval box shows %d esc hints, want 1", got)
	}
}
