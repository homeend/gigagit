package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
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
	// A capture block for any supported category is usable (conflict included —
	// headless agents like `kimi -p` run in the background capture lane).
	cap := config.ToolCommand{Category: "conflict", Name: "Y", Mode: "capture", Command: "echo hi"}
	if err := m.toolUsable(cap); err != nil {
		t.Fatalf("conflict capture must be usable: %v", err)
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

// The commit-review Label is the human "<short> <subject>" (the commit title),
// while Range stays the hex-ish sha^..sha executed by the tool.
func TestReviewTargetForCommitLabel(t *testing.T) {
	tgt := reviewTargetForCommit(model.Commit{Hash: "0123456789abcdef", Parents: []string{"p0"}, Subject: "fix: wrap modal"})
	if want := "0123456 fix: wrap modal"; tgt.Label != want {
		t.Fatalf("Label = %q, want %q", tgt.Label, want)
	}
}

// Task B: two ◉-marked commits offer "Review marked range (AI)" scoped to
// older..newer (the same range "Compare selection" shows), Range hex and Label
// human. loadedModelLinearCommits builds c0(oldest)…; git log puts the newest
// first, so commits[0]="c2" and commits[2]="c0".
func TestMarkedRangeReviewRow(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.loading = false
	m.cfg.Tools.Command = []config.ToolCommand{
		{Category: "review", Name: "A", Mode: "capture", Command: "echo hi"},
	}
	m.promptStore = promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))

	if _, ok := m.markedRangeReviewRow(); ok {
		t.Fatal("marked-range review row must be absent with no selection")
	}

	m.commitCompareSet = selectionSet(m.commits[0].Hash, m.commits[2].Hash)
	row, ok := m.markedRangeReviewRow()
	if !ok {
		t.Fatal("marked-range review row must appear with 2 commits marked")
	}
	if row.label != "Review marked range (AI)" {
		t.Fatalf("label = %q", row.label)
	}

	mm, _ := row.run(m)
	lane, ok := mm.(Model).topLayer().(*reviewLane)
	if !ok {
		t.Fatalf("running the row must open the review lane, got %T", mm.(Model).topLayer())
	}
	wantRange := m.commits[2].Hash + ".." + m.commits[0].Hash // older..newer
	if lane.target.Range != wantRange {
		t.Fatalf("Range = %q, want %q", lane.target.Range, wantRange)
	}
	if lane.target.Diff.Rev != wantRange {
		t.Fatalf("Diff.Rev = %q, want %q", lane.target.Diff.Rev, wantRange)
	}
	wantLabel := shortHash(m.commits[2].Hash) + ".." + shortHash(m.commits[0].Hash) + " — c2"
	if lane.target.Label != wantLabel {
		t.Fatalf("Label = %q, want %q", lane.target.Label, wantLabel)
	}
}

// A WIP row (working tree / staged) in the marked set hides the marked-range
// review row: a review needs a commit-to-commit range (also keeps Range hex).
func TestMarkedRangeReviewRefusesWip(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.loading = false
	m.cfg.Tools.Command = []config.ToolCommand{
		{Category: "review", Name: "A", Mode: "capture", Command: "echo hi"},
	}
	m.promptStore = promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	m.wipRows = []wipRow{{wipWorktree, 1}}
	m.commitCompareSet = selectionSet(m.commits[0].Hash, wipKey(wipRow{kind: wipWorktree}))

	if _, ok := m.markedRangeReviewRow(); ok {
		t.Fatal("marked-range review row must be absent when a WIP row is marked")
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

// (d) The working-changes row is offered (its target's Diff.Rev is asserted
// against "HEAD" in TestWorkingReviewTargetDiffsAgainstHEAD, domain package).
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
	if m.reviewRunning {
		t.Fatal("nothing should run before a tool is chosen")
	}
}

// A single approved tool skips both the chooser and the approval gate and
// dispatches straight to a BACKGROUND run: the lane is popped, m.reviewRunning
// goes true, and the Model-level gen is bumped.
func TestStartReviewLaneSingleApprovedDispatches(t *testing.T) {
	m := reviewTestModel(t)
	m.cfg.Tools.Command = m.cfg.Tools.Command[:1] // one tool
	m.rememberToolApproval(m.cfg.Tools.Command[0].Command)
	before := m.reviewGen
	m, cmd := m.startReviewLane(domain.ReviewTarget{Kind: domain.ReviewWorking})
	if !m.reviewRunning {
		t.Fatal("a single approved tool must background a run (reviewRunning)")
	}
	if layerOf[*reviewLane](m) != nil {
		t.Fatal("dispatch must pop the lane (the run is backgrounded)")
	}
	if m.reviewGen != before+1 {
		t.Fatalf("reviewGen = %d, want %d", m.reviewGen, before+1)
	}
	if cmd == nil {
		t.Fatal("dispatch must return a run+blink batch")
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
	if m.reviewRunning {
		t.Fatal("must not run before approval")
	}
	// y approves → records the config text and backgrounds the run (lane popped).
	m, cmd := lane.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if !m.reviewRunning || cmd == nil {
		t.Fatal("approve must background the run")
	}
	if layerOf[*reviewLane](m) != nil {
		t.Fatal("approve must pop the lane (the run is backgrounded)")
	}
	if !m.toolCommandApproved("echo hi") {
		t.Fatal("approval must be remembered on the config command text")
	}
}

// esc on the foreground lane (chooser/approval) pops it and starts nothing.
// The run only exists once dispatched, and it dispatches to the background —
// so there is no in-lane run to cancel.
func TestReviewEscOnLanePopsWithoutRunning(t *testing.T) {
	m := reviewTestModel(t)
	m.cfg.Tools.Command = m.cfg.Tools.Command[:1] // one tool, NOT approved → approval gate
	m, _ = m.startReviewLane(domain.ReviewTarget{Kind: domain.ReviewWorking})
	lane, _ := m.topLayer().(*reviewLane)
	if lane == nil {
		t.Fatal("expected the approval-gate lane")
	}
	m, _ = lane.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if _, ok := m.topLayer().(*reviewLane); ok {
		t.Fatal("esc must pop the lane")
	}
	if m.reviewRunning {
		t.Fatal("esc on the foreground lane must not leave a run in flight")
	}
}

// cancelReview (reachable via reRoot) cancels an in-flight background run,
// clears the running flag, and bumps the gen so a late killed result is
// dropped — never surfaced.
func TestReviewCancelDropsBackgroundRun(t *testing.T) {
	m := reviewTestModel(t)
	m.cfg.Tools.Command = m.cfg.Tools.Command[:1]
	m.rememberToolApproval(m.cfg.Tools.Command[0].Command)
	m, _ = m.startReviewLane(domain.ReviewTarget{Kind: domain.ReviewWorking})
	if !m.reviewRunning {
		t.Fatal("expected a backgrounded run")
	}
	genBefore := m.reviewGen
	m = m.cancelReview()
	if m.reviewRunning {
		t.Fatal("cancelReview must clear reviewRunning (kills the blink)")
	}
	if m.reviewGen == genBefore {
		t.Fatal("cancelReview must bump reviewGen (drops the killed result)")
	}
	// A late result carrying the pre-cancel gen is dropped, not surfaced.
	m2, _ := m.applyReviewDone(reviewDoneMsg{gen: genBefore, err: errKilled})
	if m2.statusMsg != "" {
		t.Fatalf("stale killed result must be dropped, got statusMsg=%q", m2.statusMsg)
	}
}

// The success path: dispatch backgrounds the run (lane popped, reviewRunning),
// then a matching-gen, error-free result clears the flag and auto-pops the
// full-screen report viewer titled by the range.
func TestReviewDoneSuccessOpensViewer(t *testing.T) {
	m := reviewTestModel(t)
	m.cfg.Tools.Command = m.cfg.Tools.Command[:1]
	m.rememberToolApproval(m.cfg.Tools.Command[0].Command)
	m, _ = m.startReviewLane(domain.ReviewTarget{Kind: domain.ReviewRange, Range: "a..b"})
	if !m.reviewRunning {
		t.Fatal("expected a backgrounded run")
	}
	if m.reviewRunningLabel != "a..b" {
		t.Fatalf("reviewRunningLabel = %q, want a..b", m.reviewRunningLabel)
	}
	m, _ = m.applyReviewDone(reviewDoneMsg{gen: m.reviewGen, res: domain.ReviewResult{Path: "/x/r.md", Content: "hi\n", Range: "a..b", Label: "a..b"}})
	rv, ok := m.topLayer().(*reviewView)
	if !ok {
		t.Fatalf("success must push the report viewer, got %T", m.topLayer())
	}
	if rv.title != "Review: a..b" {
		t.Fatalf("title = %q, want Review: a..b", rv.title)
	}
	if m.reviewRunning {
		t.Fatal("reviewRunning must be cleared on success")
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

// A stale done result from run A must not disturb a later run B (the cross-run
// gen guard the Model-level gen provides).
func TestReviewDoneCrossRunGuard(t *testing.T) {
	m := reviewTestModel(t)
	m.cfg.Tools.Command = m.cfg.Tools.Command[:1]
	m.rememberToolApproval(m.cfg.Tools.Command[0].Command)
	// Run A backgrounds, then is cancelled (cancelReview bumps the gen).
	m, _ = m.startReviewLane(domain.ReviewTarget{Kind: domain.ReviewWorking})
	genA := m.reviewGen
	m = m.cancelReview()
	// Run B starts and is live.
	m, _ = m.startReviewLane(domain.ReviewTarget{Kind: domain.ReviewWorking})
	if !m.reviewRunning {
		t.Fatal("run B must be live")
	}
	// A's killed run returns with A's gen — must be dropped, B untouched.
	m, _ = m.applyReviewDone(reviewDoneMsg{gen: genA, err: errKilled})
	if !m.reviewRunning {
		t.Fatal("stale run-A result must not clear live run B")
	}
	if m.statusMsg != "" {
		t.Fatalf("stale run-A error must not surface, got %q", m.statusMsg)
	}
}

// While a review runs in the background, none of the three review rows offer a
// second review (you can't start a 2nd one).
func TestReviewRowsGateOffWhileRunning(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.cfg.Tools.Command = []config.ToolCommand{
		{Category: "review", Name: "A", Mode: "capture", Command: "echo hi"},
	}
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "a.go", Unstaged: 'M'}}}
	m.reviewRunning = true

	m.focus = panelCommits
	m.sel[panelCommits] = 0
	if _, ok := m.focusedCommitReviewRow(); ok {
		t.Fatal("commit review row must gate off while a review runs")
	}
	m.focus = panelFiles
	if _, ok := m.workingReviewRow(); ok {
		t.Fatal("working review row must gate off while a review runs")
	}
	m.focus = panelBranches
	if _, ok := m.branchReviewRow(); ok {
		t.Fatal("branch review row must gate off while a review runs")
	}
}

// The commit-message generate lane refuses to start while a review runs.
func TestStartGenerateRefusesWhileReviewing(t *testing.T) {
	m := reviewTestModel(t)
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "a.go", Staged: 'M', Unstaged: '.'}}}
	m.cfg.Tools.Command = append(m.cfg.Tools.Command,
		config.ToolCommand{Category: "commit_message", Name: "Claude", Mode: "capture", Command: "echo hi"})
	m.rememberToolApproval("echo hi")
	m.reviewRunning = true
	p := &commitPopup{}
	m = m.pushLayer(p)
	m, cmd := m.startGenerate(p)
	if cmd != nil {
		t.Fatal("startGenerate must not dispatch while a review runs")
	}
	if m.statusMsg == "" {
		t.Fatal("startGenerate must surface a status message when refusing")
	}
}

// reviewSegment blinks (alternates style) while running, and is empty otherwise.
func TestReviewSegmentBlinks(t *testing.T) {
	// Force TrueColor so lipgloss emits ANSI escapes in the non-TTY test env
	// (the SetColorProfile idiom the diff-render tests use); otherwise both blink
	// phases render byte-identical plain text.
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	m := reviewTestModel(t)
	if seg := m.reviewSegment(); seg != "" {
		t.Fatalf("no running review → empty segment, got %q", seg)
	}
	m.reviewRunning = true
	m.reviewRunningLabel = "main..HEAD"
	m.reviewBlink = false
	off := m.reviewSegment()
	m.reviewBlink = true
	on := m.reviewSegment()
	if off == "" || on == "" {
		t.Fatal("a running review must render a segment")
	}
	if off == on {
		t.Fatal("the segment must alternate style on the blink phase")
	}
	if !strings.Contains(off, "main..HEAD") {
		t.Fatalf("segment must name the scope, got %q", off)
	}
}

// The reviewBlinkMsg handler flips the phase while a run is live and self-stops
// on a stale gen (finished / cancelled / superseded run).
func TestReviewBlinkTickTogglesAndSelfStops(t *testing.T) {
	m := reviewTestModel(t)
	m.reviewRunning = true
	m.reviewGen = 7
	m.reviewBlink = false
	nm, cmd := m.Update(reviewBlinkMsg{gen: 7})
	m = nm.(Model)
	if !m.reviewBlink || cmd == nil {
		t.Fatal("a live-gen tick must flip the phase and re-arm")
	}
	// A stale gen must not flip and must not re-arm.
	m.reviewBlink = false
	nm, cmd = m.Update(reviewBlinkMsg{gen: 6})
	m = nm.(Model)
	if m.reviewBlink || cmd != nil {
		t.Fatal("a stale-gen tick must be dropped (no flip, no re-arm)")
	}
	// A finished run (reviewRunning=false) also stops the tick.
	m.reviewRunning = false
	if _, cmd := m.Update(reviewBlinkMsg{gen: 7}); cmd != nil {
		t.Fatal("a tick after the run finished must not re-arm")
	}
}

// reviewScopeLabel must translate the "working changes" fallback from
// domain.ReviewTarget.DisplayLabel().
func TestReviewScopeLabelTranslatesWorkingChanges(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "xx.toml"), []byte("[meta]\nname=\"xx\"\n[strings]\n"+
		"\"working changes\" = \"XX-working\"\n"+
		"\"Review: working changes\" = \"XX-Review-working\"\n"), 0o644)
	if err := i18n.SetLanguage("xx", dir); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}
	t.Cleanup(func() { _ = i18n.SetLanguage("", "") })

	got := reviewScopeLabel(domain.ReviewTarget{Kind: domain.ReviewWorking})
	if got != "XX-working" {
		t.Fatalf("reviewScopeLabel = %q, want translated fallback", got)
	}

	// reviewTitle must recognize the literal "working changes" label (the
	// always-non-empty label a working-changes review actually carries) and
	// route it through the translated sibling key, not the generic
	// "Review: %s" format — which would silently degrade to untranslated
	// English since the label itself is never translated.
	if title := reviewTitle("working changes"); title != "XX-Review-working" {
		t.Fatalf("reviewTitle(\"working changes\") = %q, want the translated sibling key", title)
	}
}
