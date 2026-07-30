package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/model"
)

func toolCfg(cmds ...config.ToolCommand) Model {
	m := New(nil) // the zero-service constructor pattern (see commit_branch_ops_test.go)
	m.cfg.Tools.Command = cmds
	return m
}

func TestToolCommandsFiltersInvalid(t *testing.T) {
	m := toolCfg(
		config.ToolCommand{Category: "conflict", Name: "OK", Mode: "terminal", Command: "x <op>"},
		config.ToolCommand{Category: "conflict", Name: "Cap", Mode: "capture", Command: "x"},
		config.ToolCommand{Category: "conflict", Name: "BadTok", Mode: "terminal", Command: "x <bogus>"},
		config.ToolCommand{Category: "bogus", Name: "BadCat", Mode: "terminal", Command: "x"},
		config.ToolCommand{Category: "commit_message", Name: "Other", Mode: "terminal", Command: "x"},
	)
	got := m.toolCommands("conflict")
	if len(got) != 2 || got[0].Name != "OK" || got[1].Name != "Cap" {
		t.Fatalf("toolCommands = %+v, want OK and Cap (conflict capture is live)", got)
	}
	if len(m.toolNoted) != 2 { // BadTok, BadCat noted once each; Other is valid, different category
		t.Errorf("noted %d blocks, want 2: %v", len(m.toolNoted), m.toolNoted)
	}
	m.toolCommands("conflict") // second call must not re-note
	if len(m.toolNoted) != 2 {
		t.Errorf("re-noting on repeat call: %v", m.toolNoted)
	}
}

func TestToolCommandsCommitMessageCaptureLive(t *testing.T) {
	m := toolCfg(
		config.ToolCommand{Category: "commit_message", Name: "Claude", Mode: "capture", Command: "claude -p <op>"},
	)
	got := m.toolCommands("commit_message")
	if len(got) != 1 || got[0].Name != "Claude" {
		t.Fatalf("toolCommands(commit_message) = %+v, want the capture command live", got)
	}
	if len(m.toolNoted) != 0 {
		t.Errorf("a valid commit_message capture block must not be noted: %v", m.toolNoted)
	}
}

func TestToolCommandsReviewCaptureLive(t *testing.T) {
	// Stage 3 un-inerts review capture: a valid review capture block is now a
	// live command (backing the . -menu review lanes), not noted.
	m := toolCfg(
		config.ToolCommand{Category: "review", Name: "Claude", Mode: "capture", Command: "claude -p <op>"},
	)
	got := m.toolCommands("review")
	if len(got) != 1 || got[0].Name != "Claude" {
		t.Fatalf("toolCommands(review) = %+v, want the capture command live", got)
	}
	if len(m.toolNoted) != 0 {
		t.Errorf("a valid review capture block must not be noted: %v", m.toolNoted)
	}
}

func TestConflictToolChoices(t *testing.T) {
	repoLevel := config.ToolCommand{Category: "conflict", Name: "Agent", Mode: "terminal", Command: "a"}
	mergeOnly := config.ToolCommand{Category: "conflict", Name: "JM", Mode: "terminal", WhenOp: "merge", Command: "j"}
	perFile := config.ToolCommand{Category: "conflict", Name: "Meld", Mode: "terminal", PerFile: true, Command: "m"}
	all := []config.ToolCommand{repoLevel, mergeOnly, perFile}

	both := &model.FileStatus{Path: "a.go", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'} // UU = both modified
	names := func(cs []config.ToolCommand) []string {
		var out []string
		for _, c := range cs {
			out = append(out, c.Name)
		}
		return out
	}
	if got := names(conflictToolChoices(all, "merge", both)); len(got) != 3 {
		t.Errorf("merge+both-sides: %v, want all 3", got)
	}
	if got := names(conflictToolChoices(all, "rebase", both)); len(got) != 2 || got[0] != "Agent" || got[1] != "Meld" {
		t.Errorf("rebase filters when_op=merge: %v", got)
	}
	if got := names(conflictToolChoices(all, "merge", nil)); len(got) != 2 {
		t.Errorf("no focused file drops per_file: %v", got)
	}
}

func TestCompleteToolChoices(t *testing.T) {
	cmds := []config.ToolCommand{
		{Category: "conflict_complete", Name: "A", Mode: "terminal", Command: "x"},
		{Category: "conflict_complete", Name: "B", Mode: "terminal", WhenOp: "rebase", Command: "x"},
	}
	if got := completeToolChoices(cmds, ""); got != nil {
		t.Fatalf("no paused op: nothing to complete, got %v", got)
	}
	if got := completeToolChoices(cmds, "merge"); len(got) != 1 || got[0].Name != "A" {
		t.Fatalf("merge: want [A] (B is when_op=rebase), got %v", got)
	}
	if got := completeToolChoices(cmds, "rebase"); len(got) != 2 {
		t.Fatalf("rebase: want both rows, got %v", got)
	}
}
