package engine

import (
	"context"
	"os"
	"strings"
	"sync"
)

// TaskInputs is what an AI task's Prepare made for one run: the resolved
// command line and the directory it runs in, the environment ADDITIONS
// (never os.Environ — an agent session builds the child's environment
// itself and filters gg's host-terminal variables out of it), the
// $GG_MESSAGE_FILE path ("" when the kind has no file channel) and a
// Cleanup that removes every temp file Prepare created. Cleanup is
// idempotent.
type TaskInputs struct {
	Command     string
	Dir         string
	Env         []string
	MessageFile string
	Cleanup     func()
}

// CaptureTask is an AI-task operation split in three so the same inputs can
// feed a headless capture or an interactive session: Prepare builds the
// inputs, the caller runs the command, Collect turns what it produced into
// a Result (the output-channel contract: non-empty $GG_MESSAGE_FILE wins
// over stdout). Run is prepare → capture → collect.
type CaptureTask interface {
	Operation
	Prepare(ctx context.Context, deps OpDeps) (TaskInputs, error)
	Collect(in TaskInputs, stdout []byte) (Result, error)
}

// runCaptureTask is the Run body every CaptureTask shares.
func runCaptureTask(ctx context.Context, deps OpDeps, t CaptureTask) (Result, error) {
	in, err := t.Prepare(ctx, deps)
	if err != nil {
		return Result{}, err
	}
	defer in.Cleanup()
	env := append(append([]string{}, os.Environ()...), in.Env...)
	stdout, runErr := deps.captureRunner().Capture(ctx,
		CaptureSpec{Dir: in.Dir, Env: env, Command: in.Command},
		func(line string) { deps.emit(ctx, GitLine{Raw: line}) })
	res, _ := t.Collect(in, stdout)
	if runErr != nil {
		return Result{Captured: res.Captured}, runErr
	}
	return res, nil
}

// collectCaptured applies the output-channel contract: non-empty
// messageFile content wins over stdout. A Windows agent (cmd.exe echo, a
// CRLF-writing editor) emits \r\n; every consumer splits on \n.
func collectCaptured(messageFile string, stdout []byte) string {
	captured := string(stdout)
	if messageFile != "" {
		if b, err := os.ReadFile(messageFile); err == nil && strings.TrimSpace(string(b)) != "" {
			captured = string(b)
		}
	}
	return strings.ReplaceAll(captured, "\r\n", "\n")
}

// tempSet gathers the temp files of one Prepare for its Cleanup.
type tempSet struct {
	once  sync.Once
	paths []string
}

func (t *tempSet) write(pattern, content string) (string, error) {
	p, err := writeTempFile(pattern, content)
	if err == nil {
		t.paths = append(t.paths, p)
	}
	return p, err
}

func (t *tempSet) cleanup() {
	t.once.Do(func() {
		for _, p := range t.paths {
			os.Remove(p)
		}
	})
}
