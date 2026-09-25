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

// GenerateMessage runs a commit_message agent headless and returns the captured
// message (Result.Captured). It first writes context artifacts from the staged
// diff — a labeled summary ($GG_CONTEXT_FILE), the full unified diff
// ($GG_STAGED_DIFF), and an empty output file ($GG_MESSAGE_FILE) — then runs the
// (resolved, approved) command via the CaptureRunner, then removes them.
//
// Output channel contract: any capture tool MAY write the commit message to the
// file at $GG_MESSAGE_FILE instead of stdout; non-empty file content wins over
// stdout. This exists because a task-agent (e.g. Junie) treats "write a commit
// message" as work-to-do and emits only a status report on stdout — the message
// itself never reaches stdout, so it must come back through a file. A tool whose
// stdout already IS the message (e.g. Claude's --output-format json .result)
// leaves the file empty and its stdout is used. The contract generalizes to any
// future capture lane (e.g. stage 3's review).
//
// LockMode Read: git reads only; no ref/tree writes by gg. Approval is the
// caller's (TUI) responsibility, not the op's.
type GenerateMessage struct {
	Command string   // resolved, approved shell command line
	Dir     string   // repo/worktree root
	Env     []string // caller env additions (e.g. GG_TASK=commit_message)
}

var _ Operation = GenerateMessage{}

func (op GenerateMessage) LockMode() repogate.Mode { return repogate.Read }

func (op GenerateMessage) Prepare(ctx context.Context, deps OpDeps) (TaskInputs, error) {
	diff, err := deps.Repo.DiffPatch(ctx, model.DiffSpec{Cached: true})
	if err != nil {
		return TaskInputs{}, err
	}
	stat, _ := deps.Repo.DiffNumstat(ctx, model.DiffSpec{Cached: true})
	log, _ := deps.Repo.LogLines(ctx, "HEAD", 20)

	truncated := len(diff) > MaxDiffBytes
	diffBody := diff
	if truncated {
		diffBody = fmt.Sprintf("(diff truncated: %d bytes exceeds the %d KiB cap — inspect specific files with git)\n",
			len(diff), MaxDiffBytes>>10)
	}
	tmp := &tempSet{}
	fail := func(err error) (TaskInputs, error) { tmp.cleanup(); return TaskInputs{}, err }
	diffPath, err := tmp.write("gg-staged-*.diff", diffBody)
	if err != nil {
		return fail(err)
	}
	ctxPath, err := tmp.write("gg-ctx-*.txt", buildSummary(diffPath, stat, log, truncated))
	if err != nil {
		return fail(err)
	}
	// Empty output file: a task-agent tool writes the message here (see the
	// contract on GenerateMessage); a stdout tool leaves it empty. Lives in the
	// OS temp dir, outside the repo, so it never pollutes the working tree.
	msgPath, err := tmp.write("gg-msg-*.txt", "")
	if err != nil {
		return fail(err)
	}
	env := append(append([]string{}, op.Env...),
		"GG_CONTEXT_FILE="+ctxPath,
		"GG_STAGED_DIFF="+diffPath,
		"GG_MESSAGE_FILE="+msgPath,
		"GG_REPO="+op.Dir,
	)
	return TaskInputs{Command: op.Command, Dir: op.Dir, Env: env, MessageFile: msgPath, Cleanup: tmp.cleanup}, nil
}

func (op GenerateMessage) Collect(in TaskInputs, stdout []byte) (Result, error) {
	return Result{Captured: collectCaptured(in.MessageFile, stdout)}.WithSummary("generated commit message"), nil
}

func (op GenerateMessage) Run(ctx context.Context, deps OpDeps) (Result, error) {
	return runCaptureTask(ctx, deps, op)
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
