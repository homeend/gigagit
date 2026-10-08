package domain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// runGitIn runs a git command against dir, failing the test on error — the
// commitfeed_upstream_test.go helper pattern.
func runGitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// commitFile writes content to name under dir and commits it.
func commitFile(t *testing.T, dir, name, content, msg string) {
	t.Helper()
	if err := os.WriteFile(dir+"/"+name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "add", name)
	runGitIn(t, dir, "commit", "-m", msg)
}

func TestReviewReportSavesANote(t *testing.T) {
	dir, svc := newRealRepo(t) // domain test helper (compare_test.go)
	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)
	svc.UseNotesDir(t.TempDir())

	commitFile(t, dir, "a.txt", "one\n", "c1")
	commitFile(t, dir, "a.txt", "one\ntwo\n", "c2")

	target := ReviewTarget{Kind: ReviewRange, Range: "HEAD~1..HEAD", Diff: model.DiffSpec{Rev: "HEAD~1..HEAD"}}
	// A resolved command that just echoes a fixed report to stdout.
	cmd := `printf 'REPORT: one finding\n'`
	res, err := svc.ReviewReport(context.Background(), target, "Fake", cmd, []string{"GG_TASK=review"})
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if !strings.Contains(res.Content, "REPORT: one finding") {
		t.Fatalf("content=%q", res.Content)
	}
	r, err := svc.Review(context.Background(), res.NoteID)
	if err != nil {
		t.Fatalf("the review was not stored as a note: %v", err)
	}
	if r.Text != res.Content || r.Agent != "Fake" || r.Commit != revParse(t, dir, "HEAD") {
		t.Fatalf("stored review = %+v", r)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "gg", "reviews")); !os.IsNotExist(err) {
		t.Fatalf("a reviews/ report dir was written (stat err %v)", err)
	}
	if res.Label != "HEAD~1..HEAD" {
		t.Fatalf("Label = %q, want the range fallback HEAD~1..HEAD", res.Label)
	}
}

// A working-changes review is stored, with the untracked file fingerprinted.
func TestReviewReportStoresAWorkingReview(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	dir, svc, _ := reviewRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	res, err := svc.ReviewReport(ctx, WorkingReviewTarget(), "Echo",
		`printf '{"version":1,"summary":"ok","files":[]}' > "$GG_MESSAGE_FILE"`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.NoteID == "" {
		t.Fatal("a working review must be stored")
	}
	r, err := svc.Review(ctx, res.NoteID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range r.Files {
		found = found || (f.Path == "new.txt" && f.Blob != "")
	}
	if !found || r.Kind != ReviewOnWorktree {
		t.Fatalf("review = %+v, want a worktree review with the untracked new.txt fingerprinted", r)
	}
}

// A review the store cannot keep still hands back its report: the caller
// prints it beside the save error instead of losing the agent's work.
func TestReviewReportKeepsTheReportWhenTheSaveFails(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	dir, svc, _ := reviewRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc.disableNotesForTest()
	res, err := svc.ReviewReport(context.Background(), WorkingReviewTarget(), "Echo",
		`printf 'REPORT: kept\n'`, nil)
	if err == nil || !strings.Contains(err.Error(), "review not saved") {
		t.Fatalf("err = %v, want the save error", err)
	}
	if !strings.Contains(res.Content, "REPORT: kept") || res.NoteID != "" {
		t.Fatalf("result = %+v, want the report and no note id", res)
	}
}

// TestWorkingReviewTargetDiffsAgainstHEAD proves the working-changes target
// diffs against HEAD (git diff HEAD, which includes staged changes), NOT the
// zero DiffSpec (bare git diff = working tree vs index only, which silently
// omits anything already staged).
func TestWorkingReviewTargetDiffsAgainstHEAD(t *testing.T) {
	target := WorkingReviewTarget()
	if target.Diff.Rev != "HEAD" || target.Diff.Cached || len(target.Diff.Paths) != 0 {
		t.Fatalf("Diff = %+v, want {Rev: HEAD}", target.Diff)
	}
	if target.Kind != ReviewWorking {
		t.Fatalf("Kind = %v, want ReviewWorking", target.Kind)
	}
	if target.Range != "" {
		t.Fatalf("Range = %q, want empty (working-changes target)", target.Range)
	}
}

// TestWorkingReviewReportIncludesStagedChanges is the integration-level proof:
// a file staged (but not committed) must appear in the review's captured
// diff. Before the fix, WorkingReviewTarget's zero DiffSpec produced a bare
// `git diff` (working tree vs index), which is EMPTY for a fully-staged
// change — the review would silently see nothing.
func TestWorkingReviewReportIncludesStagedChanges(t *testing.T) {
	dir, svc := newRealRepo(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	if err := os.WriteFile(dir+"/staged.txt", []byte("staged content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "add", "staged.txt")

	target := WorkingReviewTarget()
	// Echo the review diff file's contents straight to stdout so the report
	// content proves what the diff actually contained.
	cmd := `cat "$GG_REVIEW_DIFF"`
	if runtime.GOOS == "windows" { // the capture runs as a .bat via cmd.exe
		cmd = `@type "%GG_REVIEW_DIFF%"`
	}
	res, err := svc.ReviewReport(context.Background(), target, "Fake", cmd, nil)
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if !strings.Contains(res.Content, "staged content") {
		t.Fatalf("review content = %q, want it to include the staged file's content", res.Content)
	}
	if !strings.Contains(res.Content, "staged.txt") {
		t.Fatalf("review content = %q, want it to mention staged.txt", res.Content)
	}
}

// hexRangeRE matches a "<hex>..<hex>" or "<hex>^..<hex>" range: proof that
// BranchReviewTarget's Range never carries a raw ref name (see
// TestBranchReviewTargetResolvesToSha for the injection-closed proof).
var hexRangeRE = regexp.MustCompile(`^[0-9a-f]{7,40}(\^)?\.\.[0-9a-f]{7,40}$`)

// TestBranchReviewTarget proves BranchReviewTarget resolves <merge-base with
// main>..<tip> for a feature branch created off main with one extra commit,
// and that both endpoints are resolved to hex SHAs (not the branch name) —
// closing the <range> command-injection vector (see
// TestBranchReviewTargetResolvesToSha).
func TestBranchReviewTarget(t *testing.T) {
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	runGitIn(t, dir, "checkout", "-b", "feature")
	commitFile(t, dir, "feature.txt", "hello\n", "feature commit")

	// Expected base: merge-base of main and feature (main's tip, since feature
	// branched off main with no further main commits).
	out, err := exec.Command("git", "-C", dir, "rev-parse", "main").Output()
	if err != nil {
		t.Fatal(err)
	}
	wantBase := strings.TrimSpace(string(out))
	out, err = exec.Command("git", "-C", dir, "rev-parse", "feature").Output()
	if err != nil {
		t.Fatal(err)
	}
	wantTip := strings.TrimSpace(string(out))

	target, err := svc.BranchReviewTarget(ctx, "feature")
	if err != nil {
		t.Fatalf("BranchReviewTarget: %v", err)
	}
	wantRange := wantBase + ".." + wantTip
	if target.Range != wantRange {
		t.Fatalf("Range = %q, want %q", target.Range, wantRange)
	}
	if target.Diff.Rev != wantRange {
		t.Fatalf("Diff.Rev = %q, want %q", target.Diff.Rev, wantRange)
	}
	if target.Kind != ReviewBranch {
		t.Fatalf("Kind = %v, want ReviewBranch", target.Kind)
	}
	if !hexRangeRE.MatchString(target.Range) {
		t.Fatalf("Range = %q, does not look like a pure-hex range", target.Range)
	}
	if strings.Contains(target.Range, "feature") {
		t.Fatalf("Range = %q, must not contain the branch name", target.Range)
	}
	// The branch NAME lives in Label (display only), never in the executed Range.
	if target.Label != "feature" {
		t.Fatalf("Label = %q, want the branch name \"feature\"", target.Label)
	}
}

// TestBranchReviewTargetTipAloneFallback proves the no-base, no-upstream
// fallback reviews the tip commit's OWN change, not a "working tree vs tip"
// diff, and that the range is the tip's resolved SHA (not the branch name).
// An orphan branch shares no history with main, so MergeBase(main, orphan)
// fails and there's no configured upstream either; its tip is a root
// commit, so its own change is everything it adds (tip^..tip does not exist).
func TestBranchReviewTargetTipAloneFallback(t *testing.T) {
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	runGitIn(t, dir, "checkout", "--orphan", "orphan")
	runGitIn(t, dir, "commit", "-m", "orphan root")

	out, err := exec.Command("git", "-C", dir, "rev-parse", "orphan").Output()
	if err != nil {
		t.Fatal(err)
	}
	wantTip := strings.TrimSpace(string(out))

	target, err := svc.BranchReviewTarget(ctx, "orphan")
	if err != nil {
		t.Fatalf("BranchReviewTarget: %v", err)
	}
	// The orphan's tip is a root commit: tip^..tip does not exist, so its own
	// change is everything it adds (against the empty tree).
	if target.Range != wantTip {
		t.Fatalf("Range = %q, want %q", target.Range, wantTip)
	}
	if target.Diff.Rev != wantTip || !target.Diff.Root {
		t.Fatalf("Diff = %+v, want the root commit's own change", target.Diff)
	}
	if target.Kind != ReviewBranch {
		t.Fatalf("Kind = %v, want ReviewBranch", target.Kind)
	}
	if !hexSHA.MatchString(target.Range) { // a sha: no ref name reaches the tool
		t.Fatalf("Range = %q, does not look like a pure-hex sha", target.Range)
	}
	if target.Label != "orphan" {
		t.Fatalf("Label = %q, want the branch name \"orphan\"", target.Label)
	}
}

// TestBranchReviewTargetResolvesToSha is the injection-closed proof: a branch
// whose name contains a shell metacharacter that git allows in ref names
// must NOT leak into Range, because Range is later substituted as unquoted
// prose into an external-tool command (`claude -p "/code-review <range>"`),
// and command substitution executes inside double quotes.
func TestBranchReviewTargetResolvesToSha(t *testing.T) {
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	// Prefer a branch name containing "$" (command substitution); git rejects
	// some ref-name characters (space, ~, ^, :, ?, *, [, \), so fall back to
	// another shell metacharacter git does allow if "$" is somehow rejected.
	name := "ev$il"
	cmd := exec.Command("git", "-C", dir, "branch", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("git branch %q rejected (%v: %s), falling back to a;b", name, err, out)
		name = "a;b"
		runGitIn(t, dir, "branch", name)
	}

	target, err := svc.BranchReviewTarget(ctx, name)
	if err != nil {
		t.Fatalf("BranchReviewTarget: %v", err)
	}
	for _, bad := range []string{"$", "`", ";"} {
		if strings.Contains(target.Range, bad) {
			t.Fatalf("Range = %q, contains injectable char %q — <range> is not pure hex", target.Range, bad)
		}
	}
	if !hexRangeRE.MatchString(target.Range) {
		t.Fatalf("Range = %q, does not look like a pure-hex range", target.Range)
	}
}

// TestReviewReportEmptyReportErrors proves a resolved command that prints
// nothing is treated as a failure, and that no report file is written in
// that case.
func TestReviewReportEmptyReportErrors(t *testing.T) {
	_, svc := newRealRepo(t)
	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)

	target := WorkingReviewTarget()
	empty := "true"
	if runtime.GOOS == "windows" {
		empty = "@rem" // exit 0, no output — cmd.exe's "true"
	}
	_, err := svc.ReviewReport(context.Background(), target, "Fake", empty, nil)
	if err == nil {
		t.Fatal("ReviewReport: want error for an empty report, got nil")
	}

	reviewsDir := stateDir + "/gg/reviews"
	entries, statErr := os.ReadDir(reviewsDir)
	if statErr == nil && len(entries) != 0 {
		t.Fatalf("expected no report file written, found %v under %s", entries, reviewsDir)
	}
}

// TestReviewDisplayLabelFallback: the display chain is Label → Range → "working
// changes", so a construction site that forgets Label degrades to the visible
// hex range, never a silent "working changes" mislabel.
func TestReviewDisplayLabelFallback(t *testing.T) {
	if got := (ReviewTarget{Label: "feat/foo", Range: "aaa..bbb"}).DisplayLabel(); got != "feat/foo" {
		t.Fatalf("DisplayLabel = %q, want the Label", got)
	}
	if got := (ReviewTarget{Range: "aaa..bbb"}).DisplayLabel(); got != "aaa..bbb" {
		t.Fatalf("DisplayLabel = %q, want the Range fallback", got)
	}
	if got := (ReviewTarget{}).DisplayLabel(); got != "working changes" {
		t.Fatalf("DisplayLabel = %q, want \"working changes\"", got)
	}
}

// TestBranchReviewTargetMasterTrunk: a repository whose trunk is `master`
// (no `main` at all) reviews EVERY commit the branch adds since master, not
// just its tip commit — the user's bug: a five-commit branch reviewed as one.
func TestBranchReviewTargetMasterTrunk(t *testing.T) {
	dir, svc := newRealRepo(t)
	ctx := context.Background()
	runGitIn(t, dir, "branch", "-m", "main", "master")
	base := revParse(t, dir, "master")
	runGitIn(t, dir, "checkout", "-b", "feature")
	for i, f := range []string{"a.txt", "b.txt", "c.txt"} {
		commitFile(t, dir, f, "x\n", "feature commit "+string(rune('1'+i)))
	}
	target, err := svc.BranchReviewTarget(ctx, "feature")
	if err != nil {
		t.Fatalf("BranchReviewTarget: %v", err)
	}
	if want := base + ".." + revParse(t, dir, "feature"); target.Range != want {
		t.Fatalf("Range = %q, want %q (everything since master)", target.Range, want)
	}
}

// TestBranchReviewTargetTrunkBeatsOwnUpstream: a branch tracking its own
// pushed copy is still reviewed from the trunk, not from what was last
// pushed (user ruling 2026-10-06: a branch review covers everything since
// trunk).
func TestBranchReviewTargetTrunkBeatsOwnUpstream(t *testing.T) {
	dir, svc := newRealRepo(t)
	ctx := context.Background()
	runGitIn(t, dir, "branch", "-m", "main", "master")
	base := revParse(t, dir, "master")
	remote := t.TempDir()
	runGitIn(t, remote, "init", "--bare", "-q")
	runGitIn(t, dir, "remote", "add", "origin", remote)
	runGitIn(t, dir, "checkout", "-b", "feature")
	commitFile(t, dir, "a.txt", "x\n", "pushed commit")
	runGitIn(t, dir, "push", "-q", "-u", "origin", "feature")
	commitFile(t, dir, "b.txt", "x\n", "unpushed commit")

	target, err := svc.BranchReviewTarget(ctx, "feature")
	if err != nil {
		t.Fatalf("BranchReviewTarget: %v", err)
	}
	if want := base + ".." + revParse(t, dir, "feature"); target.Range != want {
		t.Fatalf("Range = %q, want %q (since master, not since origin/feature)", target.Range, want)
	}
}

// TestBranchReviewTargetOriginDefaultBranch: with no local main or master,
// origin's default branch (refs/remotes/origin/HEAD) is the trunk.
func TestBranchReviewTargetOriginDefaultBranch(t *testing.T) {
	dir, svc := newRealRepo(t)
	ctx := context.Background()
	base := revParse(t, dir, "main")
	runGitIn(t, dir, "update-ref", "refs/remotes/origin/develop", base)
	runGitIn(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/develop")
	runGitIn(t, dir, "checkout", "-b", "feature")
	runGitIn(t, dir, "branch", "-D", "main")
	commitFile(t, dir, "a.txt", "x\n", "one")
	commitFile(t, dir, "b.txt", "x\n", "two")

	target, err := svc.BranchReviewTarget(ctx, "feature")
	if err != nil {
		t.Fatalf("BranchReviewTarget: %v", err)
	}
	if want := base + ".." + revParse(t, dir, "feature"); target.Range != want {
		t.Fatalf("Range = %q, want %q (since origin/develop)", target.Range, want)
	}
}
