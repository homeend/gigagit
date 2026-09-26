package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
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
	lp, ok := mm.(Model).topLayer().(*taskLaunchPopup)
	if !ok {
		t.Fatalf("running the row must open the launch dialog, got %T (status %q)", mm.(Model).topLayer(), mm.(Model).statusMsg)
	}
	lane := struct{ target domain.ReviewTarget }{lp.launch.review}
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

// A working-changes review (empty range) titles the viewer sensibly.
func TestReviewTitleWorkingChanges(t *testing.T) {
	if got := reviewTitle(""); got != "Review: working changes" {
		t.Fatalf("reviewTitle(\"\") = %q", got)
	}
	if got := reviewTitle("HEAD~1..HEAD"); got != "Review: HEAD~1..HEAD" {
		t.Fatalf("reviewTitle(range) = %q", got)
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

	// reviewTitle must recognize the literal "working changes" label (the
	// always-non-empty label a working-changes review actually carries) and
	// route it through the translated sibling key, not the generic
	// "Review: %s" format — which would silently degrade to untranslated
	// English since the label itself is never translated.
	if title := reviewTitle("working changes"); title != "XX-Review-working" {
		t.Fatalf("reviewTitle(\"working changes\") = %q, want the translated sibling key", title)
	}
}

func TestReviewRowOpensLaunchDialog(t *testing.T) {
	m := launchTestModel(t)
	m.loading = false
	m.focus = panelFiles
	row, ok := m.workingReviewRow()
	if !ok {
		t.Fatal("row hidden with a review agent configured")
	}
	nm, _ := row.run(m)
	if layerOf[*taskLaunchPopup](nm.(Model)) == nil {
		t.Fatal("review must open the launch dialog")
	}
}

func TestReviewResultOpensViewerAndSavesReport(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := launchTestModel(t)
	m.cfg.Tools.Command = []config.ToolCommand{captureCmd(exttool.CatReview, "echo LGTM")}
	spec, err := m.svc.ReviewTask(context.Background(), m.cfg.Tools.Command[0], domain.WorkingReviewTarget(), "")
	if err != nil {
		t.Fatal(err)
	}
	id := domain.Tasks().Submit(spec)
	waitTaskState(t, id, taskEndedFn)
	m, _ = m.onTasksChanged()
	v := layerOf[*fileViewer](m)
	if v == nil || v.src.kind != srcExternal || !strings.HasSuffix(v.path, ".md") || !strings.HasPrefix(v.title(), "Review: ") {
		t.Fatalf("viewer %+v (status %q)", v, m.statusMsg)
	}
}

func TestReviewResultWhileConsoleFocusedIsANotice(t *testing.T) {
	m := launchTestModel(t)
	m.console = &consoleState{focused: true}
	info := domain.TaskInfo{ID: "x", Key: "review — a..b", Kind: exttool.CatReview, Worktree: m.currentWorktree, Result: "ok", Results: 1}
	m, _ = m.applyTaskResult(info)
	if layerOf[*fileViewer](m) != nil || !strings.Contains(m.statusMsg, "ready") {
		t.Fatalf("status %q", m.statusMsg)
	}
}

func TestTaskSegmentCountsLiveTasks(t *testing.T) {
	m := launchTestModel(t)
	if seg := m.taskSegment(); seg != "" {
		t.Fatalf("idle segment %q", seg)
	}
	m.cfg.Tools.Command = []config.ToolCommand{captureCmd(exttool.CatReview, "sleep 5")}
	spec, _ := m.svc.ReviewTask(context.Background(), m.cfg.Tools.Command[0], domain.WorkingReviewTarget(), "")
	domain.Tasks().Submit(spec)
	if seg := m.taskSegment(); !strings.Contains(seg, "1") {
		t.Fatalf("segment %q", seg)
	}
}
