package tui

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/model"
)

// errKilled stands in for the *exec.ExitError a ctx-cancelled subprocess
// returns from svc.Execute ("signal: killed") — NOT context.Canceled, which
// is why the gen-guard (not an errors.Is check) is what must drop it.
var errKilled = errors.New("signal: killed")

// commitGenTestModel builds a Model with a staged change and one configured,
// approved commit_message capture tool — the precondition for startGenerate
// to reach dispatch (Task 7 adds the chooser/approval/confirm gates in front
// of this same dispatch for the >1-tool and first-run cases).
func commitGenTestModel(t *testing.T) Model {
	t.Helper()
	m := New(nil)
	m.status = model.WorkingTreeStatus{
		Files: []model.FileStatus{{Path: "a.go", Staged: 'M', Unstaged: '.'}},
	}
	m.cfg.Tools.Command = []config.ToolCommand{
		{Category: "commit_message", Name: "Claude", Mode: "capture", Command: "echo hi"},
	}
	return m
}

func TestGenerateFillsFieldsGenGuarded(t *testing.T) {
	m := commitGenTestModel(t)
	p := &commitPopup{}
	m = m.pushLayer(p)
	m, cmd := m.startGenerate(p)
	if !p.generating {
		t.Fatal("want generating")
	}
	if cmd == nil {
		t.Fatal("want a dispatch cmd")
	}
	gen := p.genGen

	// A stale result (wrong gen) is dropped.
	m = m.applyGeneratedMessage(genMessageMsg{gen: gen - 1, subject: "stale"})
	if p.title.Value() == "stale" {
		t.Fatal("stale result must be dropped")
	}
	if !p.generating {
		t.Fatal("a stale result must not clear generating")
	}

	// The live result fills subject/body and clears generating.
	m = m.applyGeneratedMessage(genMessageMsg{gen: gen, subject: "Add cap", body: "Bound diff."})
	if p.title.Value() != "Add cap" || p.desc.Value() != "Bound diff." {
		t.Fatal("fields not filled")
	}
	if p.generating {
		t.Fatal("still generating")
	}
	if m.genCancel != nil {
		t.Fatal("genCancel must be cleared on a live result")
	}
}

func TestGenerateNoOpGuardsNothingStaged(t *testing.T) {
	m := commitGenTestModel(t)
	m.status = model.WorkingTreeStatus{} // nothing staged
	p := &commitPopup{}
	m = m.pushLayer(p)
	m, cmd := m.startGenerate(p)
	if cmd != nil || p.generating {
		t.Fatal("nothing-staged must no-op")
	}
	if m.statusMsg == "" {
		t.Fatal("want a hint")
	}
}

func TestGenerateNoOpGuardsNoTool(t *testing.T) {
	m := commitGenTestModel(t)
	m.cfg.Tools.Command = nil // no commit_message tool configured
	p := &commitPopup{}
	m = m.pushLayer(p)
	m, cmd := m.startGenerate(p)
	if cmd != nil || p.generating {
		t.Fatal("no-tool must no-op")
	}
	if m.statusMsg == "" {
		t.Fatal("want a hint")
	}
}

func TestGenerateEscCancelDropsLateResult(t *testing.T) {
	m := commitGenTestModel(t)
	p := &commitPopup{}
	m = m.pushLayer(p)
	m, _ = m.startGenerate(p)
	gen := p.genGen
	m = m.escGenerate(p) // the esc-while-generating handler
	if p.generating {
		t.Fatal("esc must stop generating")
	}
	if m.genCancel != nil {
		t.Fatal("esc must clear genCancel")
	}
	// A result from the cancelled run (with the old gen + a killed error) is
	// DROPPED silently — no spurious statusMsg, no field change.
	m = m.applyGeneratedMessage(genMessageMsg{gen: gen, err: errKilled})
	if m.statusMsg != "" {
		t.Fatalf("cancel must not surface an error: %q", m.statusMsg)
	}
	if p.title.Value() != "" {
		t.Fatal("fields must be untouched")
	}
}

// TestCommitPopupCtrlGWiring drives the mechanic through commitPopup.update
// (not a direct startGenerate/escGenerate call) to prove the key routing
// itself is correct: ctrl+g dispatches, and while generating every other key
// is swallowed except esc.
func TestCommitPopupCtrlGWiring(t *testing.T) {
	m := commitGenTestModel(t)
	m = m.pushLayer(&commitPopup{})

	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = tm.(Model)
	if !layerOf[*commitPopup](m).generating {
		t.Fatal("ctrl+g must start generating")
	}
	if cmd == nil {
		t.Fatal("ctrl+g must dispatch a cmd")
	}
	if m.genCancel == nil {
		t.Fatal("ctrl+g through Update must arm m.genCancel")
	}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = tm.(Model)
	if layerOf[*commitPopup](m).title.Value() != "" {
		t.Fatal("keys must be swallowed while generating")
	}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = tm.(Model)
	if layerOf[*commitPopup](m).generating {
		t.Fatal("esc must cancel an in-flight generate")
	}
	if m.genCancel != nil {
		t.Fatal("esc through Update must clear m.genCancel")
	}
}

// TestModelUpdateHandlesGenMessageMsg proves genMessageMsg is routed through
// Model.Update (not just callable via applyGeneratedMessage directly).
func TestModelUpdateHandlesGenMessageMsg(t *testing.T) {
	m := commitGenTestModel(t)
	p := &commitPopup{genGen: 1}
	m = m.pushLayer(p)
	tm, _ := m.Update(genMessageMsg{gen: 1, subject: "S", body: "B"})
	m = tm.(Model)
	if layerOf[*commitPopup](m).title.Value() != "S" {
		t.Fatal("Update must route genMessageMsg to applyGeneratedMessage")
	}
}
