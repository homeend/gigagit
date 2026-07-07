package domain

import (
	"context"
	"os"
	"os/exec"
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
	// filename carries the sanitized range + the stamped time
	if !strings.Contains(res.Path, "20260707-0130") || !strings.Contains(res.Path, "HEAD~1..HEAD") {
		t.Fatalf("filename should carry timestamp+range: %q", res.Path)
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

// TestBranchReviewTarget proves BranchReviewTarget resolves <merge-base with
// main>..<tip> for a feature branch created off main with one extra commit.
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

	target, err := svc.BranchReviewTarget(ctx, "feature")
	if err != nil {
		t.Fatalf("BranchReviewTarget: %v", err)
	}
	wantRange := wantBase + "..feature"
	if target.Range != wantRange {
		t.Fatalf("Range = %q, want %q", target.Range, wantRange)
	}
	if target.Diff.Rev != wantRange {
		t.Fatalf("Diff.Rev = %q, want %q", target.Diff.Rev, wantRange)
	}
	if target.Kind != ReviewBranch {
		t.Fatalf("Kind = %v, want ReviewBranch", target.Kind)
	}
}

// TestBranchReviewTargetTipAloneFallback proves the no-base, no-upstream
// fallback reviews the tip commit's OWN change (tip^..tip), not an empty
// "working tree vs tip" diff. An orphan branch shares no history with main,
// so MergeBase(main, orphan) fails and there's no configured upstream either.
func TestBranchReviewTargetTipAloneFallback(t *testing.T) {
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	runGitIn(t, dir, "checkout", "--orphan", "orphan")
	runGitIn(t, dir, "commit", "-m", "orphan root")

	target, err := svc.BranchReviewTarget(ctx, "orphan")
	if err != nil {
		t.Fatalf("BranchReviewTarget: %v", err)
	}
	wantRange := "orphan^..orphan"
	if target.Range != wantRange {
		t.Fatalf("Range = %q, want %q", target.Range, wantRange)
	}
	if target.Diff.Rev != wantRange {
		t.Fatalf("Diff.Rev = %q, want %q", target.Diff.Rev, wantRange)
	}
	if target.Kind != ReviewBranch {
		t.Fatalf("Kind = %v, want ReviewBranch", target.Kind)
	}
}

// TestReviewReportEmptyReportErrors proves a resolved command that prints
// nothing is treated as a failure, and that no report file is written in
// that case.
func TestReviewReportEmptyReportErrors(t *testing.T) {
	_, svc := newRealRepo(t)
	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)

	target := ReviewTarget{Kind: ReviewWorking, Range: "", Diff: model.DiffSpec{}}
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
