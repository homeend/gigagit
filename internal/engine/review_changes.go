package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/git"
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
	// Working: a review of uncommitted changes. The input gains the
	// untracked files as new-file patches, and Prepare fingerprints every
	// reviewed file (TaskInputs.ReviewFiles).
	Working bool
}

var _ Operation = ReviewChanges{}

func (op ReviewChanges) LockMode() repogate.Mode { return repogate.Read }

func (op ReviewChanges) Prepare(ctx context.Context, deps OpDeps) (TaskInputs, error) {
	diff, err := deps.Repo.DiffPatch(ctx, op.Diff)
	if err != nil {
		return TaskInputs{}, err
	}
	stat, _ := deps.Repo.DiffNumstat(ctx, op.Diff)
	var files []model.NoteFile
	overCap := false
	if op.Working {
		w, werr := reviewWorkingInput(ctx, deps, git.ParseNumstat(stat), len(diff))
		if werr != nil {
			return TaskInputs{}, werr
		}
		files, overCap = w.files, w.overCap
		diff += w.patch
		stat += w.numstat
	}
	truncated := overCap || len(diff) > MaxDiffBytes
	diffBody := diff
	if truncated {
		diffBody = fmt.Sprintf("(diff truncated: %d bytes exceeds the %d KiB cap — inspect specific files with git)\n",
			len(diff), MaxDiffBytes>>10)
	}
	tmp := &tempSet{}
	fail := func(err error) (TaskInputs, error) { tmp.cleanup(); return TaskInputs{}, err }
	diffPath, err := tmp.write("gg-review-*.diff", diffBody)
	if err != nil {
		return fail(err)
	}
	ctxPath, err := tmp.write("gg-review-ctx-*.txt", op.reviewSummary(diffPath, stat, truncated))
	if err != nil {
		return fail(err)
	}
	msgPath, err := tmp.write("gg-review-msg-*.md", "")
	if err != nil {
		return fail(err)
	}
	env := append(append([]string{}, op.Env...),
		"GG_CONTEXT_FILE="+ctxPath,
		"GG_REVIEW_DIFF="+diffPath,
		"GG_MESSAGE_FILE="+msgPath,
		"GG_REPO="+op.Dir,
	)
	return TaskInputs{Command: op.Command, Dir: op.Dir, Env: env, MessageFile: msgPath, Cleanup: tmp.cleanup, ReviewFiles: files}, nil
}

func (op ReviewChanges) Collect(in TaskInputs, stdout []byte) (Result, error) {
	return Result{Captured: collectCaptured(in.MessageFile, stdout), ReviewFiles: in.ReviewFiles}.
		WithSummary("reviewed %s", op.RangeLabel), nil
}

func (op ReviewChanges) Run(ctx context.Context, deps OpDeps) (Result, error) {
	return runCaptureTask(ctx, deps, op)
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
	heading := "\n\n## Files changed (git diff --numstat)\n"
	if op.Working {
		heading = "\n\n## Files changed (git diff --numstat; untracked files as added)\n"
	}
	b.WriteString(heading)
	stat = strings.ReplaceAll(stat, "\x00", "\n") // -z is NUL-delimited
	if strings.TrimSpace(stat) == "" {
		b.WriteString("(no changes)\n")
	} else {
		b.WriteString(strings.TrimRight(stat, "\n") + "\n")
	}
	b.WriteString(ReviewOutputInstruction())
	return b.String()
}

// ReviewOutputInstruction is the "Review output" section of every review
// context document: the one shape a review agent replies in — agent-context v1
// whose top-level summary is the markdown overview, with free-form "meta" at
// every level. notebatch.ParseReview reads it back; the built-in review
// templates point at this section by name.
func ReviewOutputInstruction() string {
	return "\n## Review output\n" +
		"Write ONLY this JSON document (no prose around it) to the file named by\n" +
		"$GG_MESSAGE_FILE — it replaces a free-form report:\n\n" +
		"{\n  \"version\": 1,\n  \"summary\": \"<markdown: the overall review — what changed, what matters, the verdict>\",\n" +
		"  \"meta\": { \"verdict\": \"approve | comment | request changes\" },\n" +
		"  \"files\": [\n    { \"path\": \"<repo-relative path>\", \"summary\": \"<optional one line about this file>\",\n" +
		"      \"annotations\": [\n        { \"newRange\": [<first>, <last>], \"summary\": \"<one line>\",\n" +
		"          \"rationale\": \"<why it matters / how it fails>\",\n" +
		"          \"meta\": { \"severity\": \"bug | risk | design | nit\", \"confidence\": \"low | medium | high\" } }\n" +
		"      ] }\n  ]\n}\n\n" +
		"\"summary\" is rendered as markdown in the review's overview, so structure it:\n" +
		"  ## Summary   — 1-3 sentences: what the change does.\n" +
		"  ## Findings  — one bullet (\"- \") per finding, most important first, naming\n" +
		"                 files and symbols as `code` spans (e.g. `src/app.go:42`);\n" +
		"                 write \"- None.\" when there is nothing to report.\n" +
		"  ## Verdict   — approve, comment or request changes, with a one-line reason.\n" +
		"Short paragraphs, no walls of text. It is a JSON string: write each line\n" +
		"break as \\n.\n\n" +
		"Rules: line numbers are 1-based and inclusive; use \"newRange\" for a line in the\n" +
		"new version of the file and \"oldRange\" for a removed line. \"meta\" is optional\n" +
		"and free-form (string values). Annotate what a reader would not spot; leave\n" +
		"\"files\" empty when there is nothing line-specific to say. Do not modify the\n" +
		"repository and do not commit.\n"
}
