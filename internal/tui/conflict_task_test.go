package tui

import (
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
)

// conflictModelWithAgents: a paused merge with one whole-op conflict agent,
// one conflict_complete agent and one per-file mergetool configured.
func conflictModelWithAgents(t *testing.T) (Model, *conflictProcess) {
	t.Helper()
	m, p := conflictModelWithTools(t,
		config.ToolCommand{Category: "conflict", Name: "Meld", Mode: "terminal", PerFile: true, Command: "meld <merged>"},
		config.ToolCommand{Category: "conflict", Name: "Agent", Mode: "terminal", Command: "agent <op>"},
		config.ToolCommand{Category: "conflict_complete", Name: "Finisher", Mode: "capture", Command: "finish"})
	m.currentWorktree = t.TempDir()
	return m, p
}

func TestConflictPickerOffersAgentRows(t *testing.T) {
	t.Parallel()
	m, p := conflictModelWithAgents(t)
	m, _ = p.update(m, keyRunes("t"))
	if p.st != confToolPick {
		t.Fatalf("st = %v", p.st)
	}
	if len(p.toolChoices) != 1 || p.toolChoices[0].Name != "Meld" {
		t.Fatalf("in-place rows %+v: only the mergetool stays", p.toolChoices)
	}
	if !slices.Equal(p.toolAgents, []exttool.Category{exttool.CatConflict, exttool.CatConflictComplete}) {
		t.Fatalf("agent rows %v", p.toolAgents)
	}
	_ = m
}

func TestConflictPickerNoAgentRowsWithoutPausedOp(t *testing.T) {
	t.Parallel()
	m, p := conflictModelWithAgents(t)
	p.src.Op = ""
	m, _ = p.update(m, keyRunes("t"))
	if len(p.toolAgents) != 0 {
		t.Fatalf("agent rows %v need a paused op", p.toolAgents)
	}
	_ = m
}

func TestConflictAgentRowClosesProcessAndOpensDialog(t *testing.T) {
	t.Parallel()
	m, p := conflictModelWithAgents(t)
	m, _ = p.update(m, keyRunes("t"))
	p.toolSel = len(p.toolChoices) // the first agent row
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	lp := layerOf[*taskLaunchPopup](m)
	if m.proc != nil || lp == nil || lp.launch.kind != exttool.CatConflict || lp.launch.whenOp != "merge" {
		t.Fatalf("agent row must leave the process and open the dialog: proc=%v dialog=%+v status=%q", m.proc, lp, m.statusMsg)
	}
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if nm.(Model).proc == nil {
		t.Fatal("esc in the dialog must reopen the conflict window")
	}
}

func TestConflictResultOpensOverview(t *testing.T) {
	t.Parallel()
	m, _ := conflictModelWithAgents(t)
	m.proc = nil
	info := domain.TaskInfo{ID: "c", Key: "resolve & complete — x merge 1234567", Kind: exttool.CatConflictComplete,
		Agent: "Claude Code", Worktree: m.currentWorktree, Result: "resolved 2 files", Results: 1}
	m, cmd := m.applyTaskResult(info)
	if v := layerOf[*fileViewer](m); v == nil || v.src.kind != srcExternal || cmd == nil {
		t.Fatal("overview viewer + status reload expected")
	}
}

func TestConflictResultWhileProcessOpenIsANotice(t *testing.T) {
	t.Parallel()
	m, _ := conflictModelWithAgents(t)
	info := domain.TaskInfo{ID: "c", Key: "resolve conflict — x merge 1234567", Kind: exttool.CatConflict,
		Agent: "Claude Code", Worktree: m.currentWorktree, Result: "done", Results: 1}
	m, cmd := m.applyTaskResult(info)
	if layerOf[*fileViewer](m) != nil || cmd == nil {
		t.Fatal("the conflict window owns the screen: notice + reload, no viewer")
	}
}
