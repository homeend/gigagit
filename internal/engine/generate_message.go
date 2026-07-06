package engine

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repogate"
)

// MaxDiffBytes caps the staged diff handed to the agent; a larger diff is
// replaced by a stat + truncation note (the stat still lists every file).
const MaxDiffBytes = 200 << 10

// GenerateMessage runs a commit_message agent headless and returns its captured
// stdout (Result.Captured). It first writes two context artifacts from the
// staged diff — a labeled summary ($GG_CONTEXT_FILE) and the full unified diff
// ($GG_STAGED_DIFF) — then runs the (resolved, approved) command via the
// CaptureRunner, then removes them. LockMode Read: git reads only; no ref/tree
// writes by gg. Approval is the caller's (TUI) responsibility, not the op's.
type GenerateMessage struct {
	Command string   // resolved, approved shell command line
	Dir     string   // repo/worktree root
	Env     []string // caller env additions (e.g. GG_TASK=commit_message)
}

var _ Operation = GenerateMessage{}

func (op GenerateMessage) LockMode() repogate.Mode { return repogate.Read }

func (op GenerateMessage) Run(ctx context.Context, deps OpDeps) (Result, error) {
	diff, err := deps.Repo.DiffPatch(ctx, model.DiffSpec{Cached: true})
	if err != nil {
		return Result{}, err
	}
	stat, _ := deps.Repo.DiffNumstat(ctx, model.DiffSpec{Cached: true})
	log, _ := deps.Repo.LogLines(ctx, "HEAD", 20)

	truncated := len(diff) > MaxDiffBytes
	diffBody := diff
	if truncated {
		diffBody = fmt.Sprintf("(diff truncated: %d bytes exceeds the %d KiB cap — inspect specific files with git)\n",
			len(diff), MaxDiffBytes>>10)
	}
	diffPath, err := writeTempFile("gg-staged-*.diff", diffBody)
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(diffPath)
	ctxPath, err := writeTempFile("gg-ctx-*.txt", buildSummary(diffPath, stat, log, truncated))
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(ctxPath)

	env := append(append([]string{}, os.Environ()...), op.Env...)
	env = append(env,
		"GG_CONTEXT_FILE="+ctxPath,
		"GG_STAGED_DIFF="+diffPath,
		"GG_REPO="+op.Dir,
	)
	stdout, runErr := deps.captureRunner().Capture(ctx,
		CaptureSpec{Dir: op.Dir, Env: env, Command: op.Command},
		func(line string) { deps.emit(ctx, GitLine{Raw: line}) })
	if runErr != nil {
		return Result{Captured: string(stdout)}, runErr
	}
	return Result{Captured: string(stdout), Summary: "generated commit message"}, nil
}

func buildSummary(diffPath, stat string, log []model.LogLine, truncated bool) string {
	var b strings.Builder
	b.WriteString("# gg — write a commit message for the staged changes.\n")
	b.WriteString("# Output ONLY the message (subject, blank line, body). Do not commit or edit files.\n")
	b.WriteString("# Full unified diff: " + diffPath)
	if truncated {
		b.WriteString("  (truncated — inspect files with git)")
	}
	b.WriteString("\n\n## Files changed (git diff --cached --numstat)\n")
	stat = strings.ReplaceAll(stat, "\x00", "\n")
	if strings.TrimSpace(stat) == "" {
		b.WriteString("(no staged changes)\n")
	} else {
		// git diff --numstat -z NUL-terminates every record, including the
		// last, so stat already ends with a newline after the replace above;
		// TrimRight+append guarantees exactly one trailing newline instead
		// of doubling it into a stray blank line before the next section.
		b.WriteString(strings.TrimRight(stat, "\n") + "\n")
	}
	b.WriteString("\n## Recent commit subjects (match this style)\n")
	for _, l := range log {
		b.WriteString(l.Subject + "\n")
	}
	return b.String()
}

func writeTempFile(pattern, content string) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
