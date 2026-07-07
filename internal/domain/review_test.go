package domain

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

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

func TestReviewReportPersistsAndReturns(t *testing.T) {
	dir, svc := newRealRepo(t) // domain test helper (compare_test.go)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	commitFile(t, dir, "a.txt", "one\n", "c1")
	commitFile(t, dir, "a.txt", "one\ntwo\n", "c2")

	target := ReviewTarget{Kind: ReviewRange, Range: "HEAD~1..HEAD", Diff: model.DiffSpec{Rev: "HEAD~1..HEAD"}}
	// A resolved command that just echoes a fixed report to stdout.
	cmd := `printf 'REPORT: one finding\n'`
	when := time.Date(2026, 7, 7, 1, 30, 0, 0, time.UTC)
	res, err := svc.ReviewReport(context.Background(), target, cmd, []string{"GG_TASK=review"}, when)
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if !strings.Contains(res.Content, "REPORT: one finding") {
		t.Fatalf("content=%q", res.Content)
	}
	if !strings.Contains(res.Path, "reviews") || !strings.HasSuffix(res.Path, ".md") {
		t.Fatalf("path=%q, want a reviews/*.md file", res.Path)
	}
	persisted, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatalf("read persisted report: %v", err)
	}
	if got := string(persisted); !strings.Contains(got, "REPORT: one finding") {
		t.Fatalf("persisted file content=%q", got)
	}
	// New layout: a per-day folder (YYYY-MM-DD) holding <HH-MM>-<label>.md.
	// Label is unset here, so DisplayLabel falls back to the Range.
	if !strings.Contains(res.Path, "2026-07-07") || !strings.Contains(res.Path, "01-30-HEAD~1..HEAD.md") {
		t.Fatalf("path should be <date>/<HH-MM>-<label>.md: %q", res.Path)
	}
	if res.Label != "HEAD~1..HEAD" {
		t.Fatalf("Label = %q, want the range fallback HEAD~1..HEAD", res.Label)
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
	res, err := svc.ReviewReport(context.Background(), target, cmd, nil, time.Now())
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

func TestSanitizeRangeForFilename(t *testing.T) {
	cases := map[string]string{
		"main..HEAD":      "main..HEAD",
		"feature/x..main": "feature-x..main",
		"":                "working-changes",
		"a b:c":           "a-b-c",
	}
	for in, want := range cases {
		if got := sanitizeRangeForFilename(in); got != want {
			t.Fatalf("sanitize(%q)=%q, want %q", in, got, want)
		}
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
// fallback reviews the tip commit's OWN change (tip^..tip), not an empty
// "working tree vs tip" diff, and that the range is the tip's resolved SHA
// (not the branch name). An orphan branch shares no history with main, so
// MergeBase(main, orphan) fails and there's no configured upstream either.
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
	wantRange := wantTip + "^.." + wantTip
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
	_, err := svc.ReviewReport(context.Background(), target, "true", nil, time.Now())
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

// TestReviewReportFolderedByDate proves the persisted path is
// <repoKey>/<YYYY-MM-DD>/<HH-MM>-<label>.md and that the human Label (not the
// hex Range) names the file.
func TestReviewReportFolderedByDate(t *testing.T) {
	_, svc := newRealRepo(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	target := ReviewTarget{Kind: ReviewBranch, Range: "aaaaaaa..bbbbbbb", Label: "feat/my-branch", Diff: model.DiffSpec{Rev: "HEAD"}}
	when := time.Date(2026, 7, 7, 14, 5, 0, 0, time.UTC)
	res, err := svc.ReviewReport(context.Background(), target, `printf 'ok\n'`, nil, when)
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if !strings.Contains(res.Path, "2026-07-07") {
		t.Fatalf("path %q should be under a YYYY-MM-DD folder", res.Path)
	}
	if !strings.Contains(res.Path, "14-05-feat-my-branch.md") {
		t.Fatalf("path %q should be <HH-MM>-<sanitized-label>.md (branch name, not the SHA range)", res.Path)
	}
}
