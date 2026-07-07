package tui

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/promptstate"
)

// reviewTestModel builds a Model with two configured, valid review capture
// tools and a temp promptstate store. Individual tests re-approve, trim the
// command list, or set focus/selection to engage one gate at a time.
func reviewTestModel(t *testing.T) Model {
	t.Helper()
	m := New(nil)
	m.loading = false // New() starts loading=true; the . -menu rows gate on opsIdle
	m.currentWorktree = t.TempDir()
	m.cfg.Tools.Command = []config.ToolCommand{
		{Category: "review", Name: "A", Mode: "capture", Command: "echo hi"},
		{Category: "review", Name: "B", Mode: "capture", Command: "echo ho"},
	}
	m.promptStore = promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	return m
}

// (c) toolUsable now accepts a review capture block (Task 7 un-inert).
func TestToolUsableAllowsReviewCapture(t *testing.T) {
	m := New(nil)
	tc := config.ToolCommand{Category: "review", Name: "X", Mode: "capture", Command: "echo hi"}
	if err := m.toolUsable(tc); err != nil {
		t.Fatalf("review capture must be usable: %v", err)
	}
	// A capture block for an unsupported category stays inert.
	bad := config.ToolCommand{Category: "conflict", Name: "Y", Mode: "capture", Command: "echo hi"}
	if err := m.toolUsable(bad); err == nil {
		t.Fatal("conflict capture must remain inert")
	}
}

// (a) A focused non-root commit reviews its own change: sha^..sha.
func TestReviewTargetForCommitNonRoot(t *testing.T) {
	tgt := reviewTargetForCommit(model.Commit{Hash: "abc123", Parents: []string{"p0"}})
	if tgt.Range != "abc123^..abc123" {
		t.Fatalf("Range = %q, want abc123^..abc123", tgt.Range)
	}
	if tgt.Diff.Rev != "abc123^..abc123" {
		t.Fatalf("Diff.Rev = %q, want abc123^..abc123", tgt.Diff.Rev)
	}
	if tgt.Kind != domain.ReviewRange {
		t.Fatalf("Kind = %v, want ReviewRange", tgt.Kind)
	}
}

// (a') A root commit has no parent, so ^.. would fail — review the tip alone.
func TestReviewTargetForCommitRoot(t *testing.T) {
	tgt := reviewTargetForCommit(model.Commit{Hash: "root0"})
	if tgt.Range != "root0" {
		t.Fatalf("Range = %q, want root0", tgt.Range)
	}
	if tgt.Diff.Rev != "root0" {
		t.Fatalf("Diff.Rev = %q, want root0", tgt.Diff.Rev)
	}
}

// focusedCommitReviewRow wires the target from the Commits panel selection.
func TestFocusedCommitReviewRow(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.cfg.Tools.Command = []config.ToolCommand{
		{Category: "review", Name: "A", Mode: "capture", Command: "echo hi"},
	}
	m.focus = panelCommits
	m.sel[panelCommits] = 0 // newest — has a parent
	r, ok := m.focusedCommitReviewRow()
	if !ok || r.id != "review-commit" || r.run == nil {
		t.Fatalf("row not offered: ok=%v r=%+v", ok, r)
	}
	// Off the Commits panel it is absent.
	m.focus = panelBranches
	if _, ok := m.focusedCommitReviewRow(); ok {
		t.Fatal("must not be offered off the Commits panel")
	}
}

// No review tool configured → no rows at all.
func TestReviewRowsAbsentWithoutTool(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m.cfg.Tools.Command = nil
	m.focus = panelCommits
	if _, ok := m.focusedCommitReviewRow(); ok {
		t.Fatal("no review tool → no commit review row")
	}
	m.focus = panelFiles
	if _, ok := m.workingReviewRow(); ok {
		t.Fatal("no review tool → no working review row")
	}
	m.focus = panelBranches
	if _, ok := m.branchReviewRow(); ok {
		t.Fatal("no review tool → no branch review row")
	}
}

// (d) The working-changes row targets the zero DiffSpec / empty range.
func TestWorkingReviewRowTarget(t *testing.T) {
	m := reviewTestModel(t)
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "a.go", Unstaged: 'M'}}}
	m.focus = panelFiles
	r, ok := m.workingReviewRow()
	if !ok || r.id != "review-working" {
		t.Fatalf("working row not offered: ok=%v r=%+v", ok, r)
	}
}

// Chooser gate: two tools open the numbered chooser before anything runs.
func TestStartReviewLaneOpensChooser(t *testing.T) {
	m := reviewTestModel(t)
	m, _ = m.startReviewLane(domain.ReviewTarget{Kind: domain.ReviewWorking})
	lane, _ := m.topLayer().(*reviewLane)
	if lane == nil {
		t.Fatal("lane not pushed")
	}
	if !lane.choosing {
		t.Fatal("two tools must open the chooser")
	}
	if lane.running {
		t.Fatal("nothing should run before a tool is chosen")
	}
}

// A single approved tool skips both the chooser and the approval gate and
// dispatches straight to the running spinner, bumping the Model-level gen.
func TestStartReviewLaneSingleApprovedDispatches(t *testing.T) {
	m := reviewTestModel(t)
	m.cfg.Tools.Command = m.cfg.Tools.Command[:1] // one tool
	m.rememberToolApproval(m.cfg.Tools.Command[0].Command)
	before := m.reviewGen
	m, cmd := m.startReviewLane(domain.ReviewTarget{Kind: domain.ReviewWorking})
	lane, _ := m.topLayer().(*reviewLane)
	if lane == nil || !lane.running {
		t.Fatalf("expected a running lane, got %+v", lane)
	}
	if m.reviewGen != before+1 {
		t.Fatalf("reviewGen = %d, want %d", m.reviewGen, before+1)
	}
	if cmd == nil {
		t.Fatal("dispatch must return a run+tick batch")
	}
}

// A single un-approved tool stops at the approval gate; approving dispatches.
func TestReviewApprovalGate(t *testing.T) {
	m := reviewTestModel(t)
	m.cfg.Tools.Command = m.cfg.Tools.Command[:1] // one tool, NOT approved
	m, _ = m.startReviewLane(domain.ReviewTarget{Kind: domain.ReviewWorking})
	lane, _ := m.topLayer().(*reviewLane)
	if lane == nil || lane.approving == "" {
		t.Fatalf("expected the approval gate, got %+v", lane)
	}
	if lane.running {
		t.Fatal("must not run before approval")
	}
	// y approves → records the config text and dispatches.
	m, cmd := lane.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if !lane.running || cmd == nil {
		t.Fatal("approve must dispatch the run")
	}
	if !m.toolCommandApproved("echo hi") {
		t.Fatal("approval must be remembered on the config command text")
	}
}

// esc while running cancels the run (bumps the Model gen) and pops the lane,
// so a late killed-run result is dropped by the gen guard — never surfaced.
func TestReviewEscWhileRunningCancels(t *testing.T) {
	m := reviewTestModel(t)
	m.cfg.Tools.Command = m.cfg.Tools.Command[:1]
	m.rememberToolApproval(m.cfg.Tools.Command[0].Command)
	m, _ = m.startReviewLane(domain.ReviewTarget{Kind: domain.ReviewWorking})
	lane, _ := m.topLayer().(*reviewLane)
	genBefore := m.reviewGen
	m, _ = lane.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.reviewGen == genBefore {
		t.Fatal("esc while running must bump reviewGen (drops the killed result)")
	}
	if _, ok := m.topLayer().(*reviewLane); ok {
		t.Fatal("esc must pop the lane")
	}
	// A late result carrying the pre-cancel gen is dropped, not surfaced.
	m2, _ := m.applyReviewDone(reviewDoneMsg{gen: genBefore, err: errKilled})
	if m2.statusMsg != "" {
		t.Fatalf("stale killed result must be dropped, got statusMsg=%q", m2.statusMsg)
	}
}

// The success path: a matching-gen, error-free result pops the lane and pushes
// the full-screen report viewer titled by the range.
func TestReviewDoneSuccessOpensViewer(t *testing.T) {
	m := reviewTestModel(t)
	m.cfg.Tools.Command = m.cfg.Tools.Command[:1]
	m.rememberToolApproval(m.cfg.Tools.Command[0].Command)
	m, _ = m.startReviewLane(domain.ReviewTarget{Kind: domain.ReviewRange, Range: "a..b"})
	if _, ok := m.topLayer().(*reviewLane); !ok {
		t.Fatal("expected a running lane")
	}
	m, _ = m.applyReviewDone(reviewDoneMsg{gen: m.reviewGen, res: domain.ReviewResult{Path: "/x/r.md", Content: "hi\n", Range: "a..b"}})
	rv, ok := m.topLayer().(*reviewView)
	if !ok {
		t.Fatalf("success must push the report viewer, got %T", m.topLayer())
	}
	if rv.title != "Review: a..b" {
		t.Fatalf("title = %q, want Review: a..b", rv.title)
	}
	if layerOf[*reviewLane](m) != nil {
		t.Fatal("the lane must be removed on success")
	}
	if m.reviewCancel != nil {
		t.Fatal("reviewCancel must be cleared on success")
	}
}

// A working-changes review (empty range) titles the viewer sensibly.
func TestReviewTitleWorkingChanges(t *testing.T) {
	if got := reviewTitle(""); got != "Review: working changes" {
		t.Fatalf("reviewTitle(\"\") = %q", got)
	}
	if got := reviewTitle("HEAD~1..HEAD"); got != "Review: HEAD~1..HEAD" {
		t.Fatalf("reviewTitle(range) = %q", got)
	}
}

// A stale done result from lane A must not pop the current lane B (the
// cross-lane gen guard the Model-level gen provides).
func TestReviewDoneCrossLaneGuard(t *testing.T) {
	m := reviewTestModel(t)
	m.cfg.Tools.Command = m.cfg.Tools.Command[:1]
	m.rememberToolApproval(m.cfg.Tools.Command[0].Command)
	// Lane A runs, then is cancelled (esc pops it, bumps gen).
	m, _ = m.startReviewLane(domain.ReviewTarget{Kind: domain.ReviewWorking})
	laneA, _ := m.topLayer().(*reviewLane)
	genA := m.reviewGen
	m, _ = laneA.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	// Lane B starts and is live.
	m, _ = m.startReviewLane(domain.ReviewTarget{Kind: domain.ReviewWorking})
	if _, ok := m.topLayer().(*reviewLane); !ok {
		t.Fatal("lane B must be live")
	}
	// A's killed run returns with A's gen — must be dropped, B untouched.
	m, _ = m.applyReviewDone(reviewDoneMsg{gen: genA, err: errKilled})
	if _, ok := m.topLayer().(*reviewLane); !ok {
		t.Fatal("stale lane-A result must not pop live lane B")
	}
}
