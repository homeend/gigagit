package gitexec

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gigagit/gg/internal/observ"
)

// compile-time assertion that ExecRunner satisfies Runner.
var _ Runner = (*ExecRunner)(nil)

// ExecRunner runs the real git binary and records a span per invocation.
type ExecRunner struct {
	gitPath  string
	workDir  string
	recorder observ.Recorder
	now      func() time.Time
}

// NewExecRunner returns a runner that invokes gitPath in workDir, recording
// spans to rec (may be nil to disable recording).
func NewExecRunner(gitPath, workDir string, rec observ.Recorder) *ExecRunner {
	if gitPath == "" {
		gitPath = "git"
	}
	return &ExecRunner{gitPath: gitPath, workDir: workDir, recorder: rec, now: time.Now}
}

func (r *ExecRunner) record(name string, argv []string, exit int, dur time.Duration, start time.Time, runErr error) {
	if r.recorder == nil {
		return
	}
	errStr := ""
	if runErr != nil {
		errStr = runErr.Error()
	}
	r.recorder.Record(observ.Span{
		Name:     name,
		Args:     observ.Redact(argv),
		ExitCode: exit,
		Err:      errStr,
		Start:    start,
		Duration: dur,
	})
}

func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if asExit(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func asExit(err error, target **exec.ExitError) bool {
	for err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			*target = ee
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// gitEnv builds the environment for a git subprocess: the inherited process
// environment, any caller-supplied vars, then GIT_TERMINAL_PROMPT=0 appended
// last so it always wins. gg runs inside a TUI that owns the terminal in raw
// mode, so git must never fall back to an interactive credential/terminal
// prompt (e.g. "Username for 'https://github.com':") — that opens /dev/tty and
// blocks the process forever, freezing the UI. With prompting disabled git
// fails fast with a clear error instead, leaving the UI responsive. Credential
// helpers and ssh-agent are unaffected.
func gitEnv(extra []string) []string {
	env := os.Environ()
	env = append(env, extra...)
	return append(env, "GIT_TERMINAL_PROMPT=0")
}

func (r *ExecRunner) Run(ctx context.Context, name string, argv []string) (Result, error) {
	return r.RunEnv(ctx, name, argv, nil)
}

func (r *ExecRunner) RunEnv(ctx context.Context, name string, argv, env []string) (Result, error) {
	start := r.now()
	cmd := exec.CommandContext(ctx, r.gitPath, argv...)
	cmd.Dir = r.workDir
	cmd.Env = gitEnv(env)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	dur := r.now().Sub(start)
	exit := exitCodeOf(runErr)
	r.record(name, argv, exit, dur, start, runErr)

	res := Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exit, Duration: dur}
	if runErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return res, fmt.Errorf("%s cancelled: %w", name, ctxErr)
		}
		return res, fmt.Errorf("%s failed (exit %d): %s", name, exit, strings.TrimSpace(stderr.String()))
	}
	return res, nil
}

func (r *ExecRunner) Stream(ctx context.Context, name string, argv []string, onLine func(string)) (Result, error) {
	start := r.now()
	cmd := exec.CommandContext(ctx, r.gitPath, argv...)
	cmd.Dir = r.workDir
	cmd.Env = gitEnv(nil)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}
	var all strings.Builder
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		all.WriteString(line)
		all.WriteByte('\n')
		onLine(line)
	}
	scanErr := scanner.Err()
	runErr := cmd.Wait()
	dur := r.now().Sub(start)
	exit := exitCodeOf(runErr)
	r.record(name, argv, exit, dur, start, runErr)

	res := Result{Stdout: all.String(), Stderr: stderr.String(), ExitCode: exit, Duration: dur}
	if runErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return res, fmt.Errorf("%s cancelled: %w", name, ctxErr)
		}
		return res, fmt.Errorf("%s failed (exit %d): %s", name, exit, strings.TrimSpace(stderr.String()))
	}
	if scanErr != nil {
		return res, fmt.Errorf("%s: reading output: %w", name, scanErr)
	}
	return res, nil
}
