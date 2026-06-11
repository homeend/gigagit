package gitexec

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
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

func (r *ExecRunner) Run(ctx context.Context, name string, argv []string) (Result, error) {
	start := r.now()
	cmd := exec.CommandContext(ctx, r.gitPath, argv...)
	cmd.Dir = r.workDir
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
