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

func TestToolCommandsFiltersInvalidAndCapture(t *testing.T) {
	m := toolCfg(
		config.ToolCommand{Category: "conflict", Name: "OK", Mode: "terminal", Command: "x <op>"},
		config.ToolCommand{Category: "conflict", Name: "Cap", Mode: "capture", Command: "x"},
		config.ToolCommand{Category: "conflict", Name: "BadTok", Mode: "terminal", Command: "x <bogus>"},
		config.ToolCommand{Category: "bogus", Name: "BadCat", Mode: "terminal", Command: "x"},
		config.ToolCommand{Category: "commit_message", Name: "Other", Mode: "terminal", Command: "x"},
	)
	got := m.toolCommands("conflict")
	if len(got) != 1 || got[0].Name != "OK" {
		t.Fatalf("toolCommands = %+v, want just OK", got)
	}
	if len(m.toolNoted) != 3 { // Cap, BadTok, BadCat noted once each; Other is valid, different category
		t.Errorf("noted %d blocks, want 3: %v", len(m.toolNoted), m.toolNoted)
	}
	m.toolCommands("conflict") // second call must not re-note
	if len(m.toolNoted) != 3 {
		t.Errorf("re-noting on repeat call: %v", m.toolNoted)
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
