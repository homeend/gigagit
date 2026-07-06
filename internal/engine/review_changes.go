package engine

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repogate"
)

// ReviewChanges runs a review agent headless over op.Diff and returns its
// captured report (Result.Captured). It writes three temp files — a labeled
// summary ($GG_CONTEXT_FILE, naming the range), the full unified diff of
// op.Diff ($GG_REVIEW_DIFF, capped at MaxDiffBytes), and an empty output file
// ($GG_MESSAGE_FILE) — runs the (resolved, approved) command via the
// CaptureRunner, then removes them.
//
// It shares the Stage-2 output-channel contract: a task-agent MAY write its
// report to $GG_MESSAGE_FILE and non-empty file content WINS over stdout; a
// stdout tool (Claude's --output-format json .result) leaves it empty and
// stdout is used. LockMode Read: git reads only; approval is the frontend's job.
type ReviewChanges struct {
	Command    string         // resolved, approved shell command line
	Dir        string         // repo/worktree root
	Env        []string       // caller env additions (e.g. GG_TASK=review)
	Diff       model.DiffSpec // the range/working diff to review
	RangeLabel string         // human range label for the summary (e.g. "main..HEAD")
}

var _ Operation = ReviewChanges{}

func (op ReviewChanges) LockMode() repogate.Mode { return repogate.Read }

func (op ReviewChanges) Run(ctx context.Context, deps OpDeps) (Result, error) {
	diff, err := deps.Repo.DiffPatch(ctx, op.Diff)
	if err != nil {
		return Result{}, err
	}
	stat, _ := deps.Repo.DiffNumstat(ctx, op.Diff)

	truncated := len(diff) > MaxDiffBytes
	diffBody := diff
	if truncated {
		diffBody = fmt.Sprintf("(diff truncated: %d bytes exceeds the %d KiB cap — inspect specific files with git)\n",
			len(diff), MaxDiffBytes>>10)
	}
	diffPath, err := writeTempFile("gg-review-*.diff", diffBody)
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(diffPath)
	ctxPath, err := writeTempFile("gg-review-ctx-*.txt", op.reviewSummary(diffPath, stat, truncated))
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(ctxPath)
	msgPath, err := writeTempFile("gg-review-msg-*.md", "")
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(msgPath)

	env := append(append([]string{}, os.Environ()...), op.Env...)
	env = append(env,
		"GG_CONTEXT_FILE="+ctxPath,
		"GG_REVIEW_DIFF="+diffPath,
		"GG_MESSAGE_FILE="+msgPath,
		"GG_REPO="+op.Dir,
	)
	stdout, runErr := deps.captureRunner().Capture(ctx,
		CaptureSpec{Dir: op.Dir, Env: env, Command: op.Command},
		func(line string) { deps.emit(ctx, GitLine{Raw: line}) })
	captured := string(stdout)
	if fileMsg, rerr := os.ReadFile(msgPath); rerr == nil && strings.TrimSpace(string(fileMsg)) != "" {
		captured = string(fileMsg)
	}
	if runErr != nil {
		return Result{Captured: captured}, runErr
	}
	return Result{Captured: captured, Summary: "reviewed " + op.RangeLabel}, nil
}

func (op ReviewChanges) reviewSummary(diffPath, stat string, truncated bool) string {
	var b strings.Builder
	b.WriteString("# gg — review the changes below.\n")
	rangeLabel := op.RangeLabel
	if rangeLabel == "" {
		rangeLabel = "(working changes)"
	}
	b.WriteString("# Range: " + rangeLabel + "\n")
	b.WriteString("# Full unified diff: " + diffPath)
	if truncated {
		b.WriteString("  (truncated — inspect files with git)")
	}
	b.WriteString("\n\n## Files changed (git diff --numstat)\n")
	stat = strings.ReplaceAll(stat, "\x00", "\n") // -z is NUL-delimited
	if strings.TrimSpace(stat) == "" {
		b.WriteString("(no changes)\n")
	} else {
		b.WriteString(strings.TrimRight(stat, "\n") + "\n")
	}
	return b.String()
}
