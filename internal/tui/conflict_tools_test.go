package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// NOTE: keyRunes(s) already exists in this package (irebase_view_test.go) —
// use it, do NOT redeclare it.

func conflictModelWithTools(t *testing.T, cmds ...config.ToolCommand) (Model, *conflictProcess) {
	t.Helper()
	m := toolCfg(cmds...)
	m.conflict = domain.ConflictState{Op: "merge", Source: "feature", Target: "main"}
	m.status.Files = []model.FileStatus{{Path: "a.go", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'}}
	m2, _ := startConflictProcess(m)
	p, ok := m2.proc.(*conflictProcess)
	if !ok {
		t.Fatal("conflict process did not open")
	}
	return m2, p
}

func TestConflictTKeyOpensPicker(t *testing.T) {
	m, p := conflictModelWithTools(t,
		config.ToolCommand{Category: "conflict", Name: "Agent", Mode: "terminal", Command: "a <op>"})
	m, _ = p.update(m, keyRunes("t"))
	if p.st != confToolPick {
		t.Fatalf("state = %v, want confToolPick", p.st)
	}
	if len(p.toolChoices) != 1 || p.toolChoices[0].Name != "Agent" {
		t.Errorf("choices = %+v", p.toolChoices)
	}
	// esc returns to the list.
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if p.st != confListing {
		t.Errorf("esc: state = %v, want confListing", p.st)
	}
	_ = m
}

func TestConflictTKeyNoCommandsIsNoop(t *testing.T) {
	m, p := conflictModelWithTools(t) // no commands configured
	m, _ = p.update(m, keyRunes("t"))
	if p.st != confListing {
		t.Errorf("t with zero commands must stay in listing, got %v", p.st)
	}
	if m.statusMsg == "" {
		t.Error("expected a status hint about configuring tools")
	}
}

func TestConflictHintsAdvertiseTools(t *testing.T) {
	files := []model.FileStatus{{Path: "a.go", Staged: 'U', Unstaged: 'U'}}
	withTools := conflictHints(files, 0, "merge", 1)
	found := false
	for _, h := range withTools {
		if h == "[t] tools" {
			found = true
		}
	}
	if !found {
		t.Errorf("hints missing [t] tools: %v", withTools)
	}
	for _, h := range conflictHints(files, 0, "merge", 0) {
		if h == "[t] tools" {
			t.Error("[t] shown with zero commands")
		}
	}
}
