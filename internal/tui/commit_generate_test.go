package tui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/promptstate"
)

// errKilled stands in for the *exec.ExitError a ctx-cancelled subprocess
// returns from svc.Execute ("signal: killed") — NOT context.Canceled, which
// is why the gen-guard (not an errors.Is check) is what must drop it.
var errKilled = errors.New("signal: killed")

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
	m, _ = m.startGenerate(p) // generating=true, genGen bumped, spinFrame=0
	gen := p.genGen

	// A matching in-flight tick advances the frame and reschedules.
	if _, cmd := m.tickGenSpinner(genSpinMsg{gen: gen}); cmd == nil || p.spinFrame != 1 {
		t.Fatalf("in-flight tick: spinFrame=%d cmd==nil=%v", p.spinFrame, cmd == nil)
	}
	// A stale-gen tick self-stops (no reschedule).
	if _, cmd := m.tickGenSpinner(genSpinMsg{gen: gen - 1}); cmd != nil {
		t.Fatal("stale-gen tick must not reschedule")
	}
	// Once the run ends, a tick self-stops.
	p.generating = false
	if _, cmd := m.tickGenSpinner(genSpinMsg{gen: gen}); cmd != nil {
		t.Fatal("finished run: tick must not reschedule")
	}
}

func TestGenerateFillsFieldsGenGuarded(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	m := commitGenTestModel(t)
	p := &commitPopup{genGen: 1}
	m = m.pushLayer(p)
	tm, _ := m.Update(genMessageMsg{gen: 1, subject: "S", body: "B"})
	m = tm.(Model)
	if layerOf[*commitPopup](m).title.Value() != "S" {
		t.Fatal("Update must route genMessageMsg to applyGeneratedMessage")
	}
}

// --- Task 7: chooser / first-run approval / confirm-replace gates ---

// TestGenerateChooserWhenMultipleTools covers gate 1: with >1 commit_message
// tool configured, startGenerate must populate p.choosing and NOT dispatch;
// selecting by digit resolves that specific tool and advances past the
// chooser (both tools are pre-approved here so the test isolates the
// chooser mechanic from the approval gate covered separately below).
func TestGenerateChooserWhenMultipleTools(t *testing.T) {
	t.Parallel()
	m := commitGenTestModel(t)
	second := config.ToolCommand{Category: "commit_message", Name: "Junie", Mode: "capture", Command: "echo bye"}
	m.cfg.Tools.Command = append(m.cfg.Tools.Command, second)
	m.rememberToolApproval(second.Command)

	p := &commitPopup{}
	m = m.pushLayer(p)
	m, cmd := m.startGenerate(p)
	if cmd != nil {
		t.Fatal("chooser must not dispatch before a tool is picked")
	}
	if p.generating {
		t.Fatal("chooser must not start generating")
	}
	if len(p.choosing) != 2 {
		t.Fatalf("want 2 choices, got %d", len(p.choosing))
	}

	// Digit "2" selects the second tool (Junie), not the first.
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	m = tm.(Model)
	p = layerOf[*commitPopup](m)
	if p.choosing != nil {
		t.Fatal("choosing must clear after a selection")
	}
	if p.genCmd.Name != "Junie" {
		t.Fatalf("want Junie chosen by digit 2, got %q", p.genCmd.Name)
	}
	if !p.generating {
		t.Fatal("an already-approved tool with empty fields must dispatch immediately")
	}
	if cmd == nil {
		t.Fatal("want a dispatch cmd")
	}
}

// TestGenerateChooserEscCancels covers the chooser's esc: it clears choosing
// without picking any tool or dispatching.
func TestGenerateChooserEscCancels(t *testing.T) {
	t.Parallel()
	m := commitGenTestModel(t)
	m.cfg.Tools.Command = append(m.cfg.Tools.Command,
		config.ToolCommand{Category: "commit_message", Name: "Junie", Mode: "capture", Command: "echo bye"})
	p := &commitPopup{}
	m = m.pushLayer(p)
	m, _ = m.startGenerate(p)
	if len(p.choosing) != 2 {
		t.Fatalf("want 2 choices, got %d", len(p.choosing))
	}

	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = tm.(Model)
	p = layerOf[*commitPopup](m)
	if p.choosing != nil {
		t.Fatal("esc must clear choosing")
	}
	if cmd != nil || p.generating {
		t.Fatal("esc must not dispatch")
	}
}

// TestGenerateApprovalGateFirstRun covers gate 2: an unapproved command sets
// p.approving instead of dispatching; esc cancels without recording approval
// or dispatching; y both records the approval (keyed by the CONFIG command
// text) and proceeds (skipping confirm — fields are empty).
func TestGenerateApprovalGateFirstRun(t *testing.T) {
	t.Parallel()
	m := commitGenTestModel(t)
	m.cfg.Tools.Command[0].Command = "echo unapproved" // a fresh, unapproved template
	p := &commitPopup{}
	m = m.pushLayer(p)

	m, cmd := m.startGenerate(p)
	if cmd != nil {
		t.Fatal("an unapproved command must not dispatch")
	}
	if p.generating {
		t.Fatal("must not be generating before approval")
	}
	if p.approving != "echo unapproved" {
		t.Fatalf("want approving to hold the resolved command, got %q", p.approving)
	}

	// esc cancels: no dispatch, no recorded approval.
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = tm.(Model)
	p = layerOf[*commitPopup](m)
	if p.approving != "" {
		t.Fatal("esc must clear approving")
	}
	if cmd != nil || p.generating {
		t.Fatal("esc must not dispatch")
	}
	if m.toolCommandApproved("echo unapproved") {
		t.Fatal("esc must not record approval")
	}

	// Re-trigger, then approve with "y".
	m, _ = m.startGenerate(p)
	tm, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = tm.(Model)
	p = layerOf[*commitPopup](m)
	if p.approving != "" {
		t.Fatal("y must clear approving")
	}
	if !p.generating {
		t.Fatal("y must dispatch (empty fields skip confirm)")
	}
	if cmd == nil {
		t.Fatal("want a dispatch cmd")
	}
	if !m.toolCommandApproved("echo unapproved") {
		t.Fatal("y must record the approval for future runs")
	}
}

// TestGenerateApprovalGateEnterAlsoRuns proves enter is an equally valid
// accept key alongside "y" (the shared approvalBoxView hint says "[enter]
// run").
func TestGenerateApprovalGateEnterAlsoRuns(t *testing.T) {
	t.Parallel()
	m := commitGenTestModel(t)
	m.cfg.Tools.Command[0].Command = "echo unapproved2"
	p := &commitPopup{}
	m = m.pushLayer(p)
	m, _ = m.startGenerate(p)

	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)
	p = layerOf[*commitPopup](m)
	if p.approving != "" {
		t.Fatal("enter must clear approving")
	}
	if !p.generating || cmd == nil {
		t.Fatal("enter must dispatch")
	}
	if !m.toolCommandApproved("echo unapproved2") {
		t.Fatal("enter must record the approval")
	}
}

// TestGenerateConfirmReplaceGate covers gate 3: non-empty title/desc sets
// p.confirming before the run; Cancel (esc) leaves the existing text
// untouched and does not dispatch; Replace (y) dispatches.
func TestGenerateConfirmReplaceGate(t *testing.T) {
	t.Parallel()
	m := commitGenTestModel(t)
	p := &commitPopup{title: newTextField("existing subject")}
	m = m.pushLayer(p)

	m, cmd := m.startGenerate(p)
	if cmd != nil {
		t.Fatal("must not dispatch before the confirm-replace decision")
	}
	if p.generating {
		t.Fatal("must not be generating before confirm")
	}
	if p.confirming == "" {
		t.Fatal("want confirming set when the title is non-empty")
	}

	// esc cancels: existing text untouched, no dispatch.
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = tm.(Model)
	p = layerOf[*commitPopup](m)
	if p.confirming != "" {
		t.Fatal("esc must clear confirming")
	}
	if cmd != nil || p.generating {
		t.Fatal("esc must not dispatch")
	}
	if p.title.Value() != "existing subject" {
		t.Fatal("cancel must leave the existing title untouched")
	}

	// Re-trigger, then replace with "y".
	m, _ = m.startGenerate(p)
	tm, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = tm.(Model)
	p = layerOf[*commitPopup](m)
	if p.confirming != "" {
		t.Fatal("y must clear confirming")
	}
	if !p.generating {
		t.Fatal("y must dispatch the run")
	}
	if cmd == nil {
		t.Fatal("want a dispatch cmd")
	}
}

// TestGenerateConfirmReplaceGateNonEmptyDescOnly proves a non-empty
// description alone (title still blank) also engages the confirm gate.
func TestGenerateConfirmReplaceGateNonEmptyDescOnly(t *testing.T) {
	t.Parallel()
	m := commitGenTestModel(t)
	p := &commitPopup{desc: newTextField("existing body")}
	m = m.pushLayer(p)
	m, cmd := m.startGenerate(p)
	if cmd != nil || p.confirming == "" {
		t.Fatal("a non-empty description alone must engage the confirm gate")
	}
}

// TestGenerateApprovalKeyedOnConfigTextNotResolved proves the approval hash
// is stable across runs whose RESOLVED command differs (here, via <repo>)
// as long as the CONFIG template text is unchanged — the exact property
// gateGenerate/approveAndProceed rely on (m.toolCommandApproved(chosen.Command)
// / m.rememberToolApproval(p.genCmd.Command), never the resolved text).
func TestGenerateApprovalKeyedOnConfigTextNotResolved(t *testing.T) {
	t.Parallel()
	m := commitGenTestModel(t)
	m.cfg.Tools.Command[0].Command = "echo <repo>" // resolved text varies with currentWorktree
	// toolRepoKey falls back to currentWorktree when repoHealth hasn't
	// resolved a git common dir; pin it so the approval-store BUCKET stays
	// fixed while currentWorktree (and so the resolved text) varies below —
	// otherwise this test would conflate "different repo key" with "same
	// repo key, different resolved text".
	m.repoHealth.GitCommonDir = "/repo/.git"
	m.currentWorktree = "/repo/a"
	p := &commitPopup{}
	m = m.pushLayer(p)

	m, cmd := m.startGenerate(p)
	if cmd != nil || p.approving == "" {
		t.Fatal("a fresh template must require approval")
	}
	if !strings.Contains(p.approving, "/repo/a") {
		t.Fatalf("resolved command should embed the repo path, got %q", p.approving)
	}
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = tm.(Model)
	p = layerOf[*commitPopup](m)
	if p.approving != "" || cmd == nil {
		t.Fatal("y must approve and dispatch")
	}

	// A second popup, a different resolved command (different repo path),
	// but the SAME config template text: the approval gate must be skipped.
	m.currentWorktree = "/repo/b"
	p2 := &commitPopup{}
	m = m.pushLayer(p2)
	m, cmd = m.startGenerate(p2)
	if p2.approving != "" {
		t.Fatal("approval keyed on config text must survive a different resolved command")
	}
	if !p2.generating || cmd == nil {
		t.Fatal("an already-approved template must dispatch straight through")
	}
}

// TestGenerateSkipsConfirmWhenFieldsEmpty covers gate 3's else branch: empty
// title/desc skip the confirm gate and (with an already-approved single
// tool) dispatch straight through.
func TestGenerateSkipsConfirmWhenFieldsEmpty(t *testing.T) {
	t.Parallel()
	m := commitGenTestModel(t)
	p := &commitPopup{}
	m = m.pushLayer(p)
	m, cmd := m.startGenerate(p)
	if p.confirming != "" {
		t.Fatal("empty fields must skip the confirm gate")
	}
	if !p.generating {
		t.Fatal("empty fields must dispatch straight through")
	}
	if cmd == nil {
		t.Fatal("want a dispatch cmd")
	}
}
