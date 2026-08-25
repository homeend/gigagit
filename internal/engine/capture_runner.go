package engine

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/homeend/gigagit/internal/template"
)

// CaptureSpec is one headless capture invocation. Command is a shell command
// line, run via a temp script + the platform shell (the ShellHookRunner path).
type CaptureSpec struct {
	Dir     string
	Env     []string
	Command string
}

// CaptureRunner runs a command headless and returns its full stdout, streaming
// stderr lines to onLine. Unlike HookRunner a non-zero exit is an error (a
// failed capture has no usable output). stdin is /dev/null; ctx kills it.
type CaptureRunner interface {
	Capture(ctx context.Context, spec CaptureSpec, onLine func(string)) (stdout []byte, err error)
}

// ShellCaptureRunner is the production CaptureRunner: temp script + $SHELL/cmd,
// stdout to a buffer, stderr streamed as lines.
type ShellCaptureRunner struct{}

func (ShellCaptureRunner) Capture(ctx context.Context, spec CaptureSpec, onLine func(string)) ([]byte, error) {
	ext := ".sh"
	if runtime.GOOS == "windows" {
		ext = ".bat"
	}
	f, err := os.CreateTemp("", "gg-capture-*"+ext)
	if err != nil {
		return nil, err
	}
	name := f.Name()
	defer os.Remove(name)
	body := spec.Command
	if runtime.GOOS == "windows" {
		// @echo off: cmd.exe otherwise echoes each command line into stdout,
		// which would pollute the captured output (a generated commit message
		// would start with the prompt line). The flattened command stays one
		// line — the .bat itself may have this one preamble line.
		body = "@echo off\r\n" + template.FlattenForCmd(body) // see FlattenForCmd: a .bat cannot span lines
	}
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}

	shell, args := hookShellArgv(name)
	cmd := exec.CommandContext(ctx, shell, args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.Stdin = nil // /dev/null: a prompting agent gets EOF, never hangs.
	var out bytes.Buffer
	cmd.Stdout = &out
	lw := &hookLineWriter{onLine: onLine}
	cmd.Stderr = lw
	cmd.WaitDelay = 3 * time.Second // Stage-1 grandchild-pipe guard

	err = cmd.Run()
	lw.flush()
	// A clean exit whose pipes a detached grandchild held open past WaitDelay
	// returns ErrWaitDelay even though stdout is fully captured; mirror
	// gitexec.RunEnv's handling so a successful agent's output isn't discarded.
	if errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 0 {
		return out.Bytes(), nil
	}
	return out.Bytes(), err // err is *exec.ExitError on non-zero exit
}
