package engine

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"runtime"
)

// HookSpec is one hook invocation: a working directory, a full environment
// (caller already merged the inherited env + GG_* vars), and the script body.
type HookSpec struct {
	Dir    string
	Env    []string
	Script string
}

// HookRunner runs a post-create hook script. A non-zero script exit is returned
// via exitCode (not err); err is non-nil only for a setup/exec failure. The hook
// is non-interactive: stdin is the null device. ctx cancellation kills it.
type HookRunner interface {
	Run(ctx context.Context, spec HookSpec, onLine func(string)) (exitCode int, err error)
}

// ShellHookRunner runs the script via the user's $SHELL (POSIX) or cmd.exe
// (Windows) by writing it to a temp file and executing that file — uniform
// across platforms and free of arg-length / newline-quoting limits.
type ShellHookRunner struct{}

func (ShellHookRunner) Run(ctx context.Context, spec HookSpec, onLine func(string)) (int, error) {
	ext := ".sh"
	if runtime.GOOS == "windows" {
		ext = ".bat"
	}
	f, err := os.CreateTemp("", "gg-hook-*"+ext)
	if err != nil {
		return -1, err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.WriteString(spec.Script); err != nil {
		f.Close()
		return -1, err
	}
	if err := f.Close(); err != nil {
		return -1, err
	}

	shell, args := hookShellArgv(name)
	cmd := exec.CommandContext(ctx, shell, args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.Stdin = nil // nil ⇒ /dev/null: a prompting hook gets EOF, never hangs.
	lw := &hookLineWriter{onLine: onLine}
	cmd.Stdout = lw
	cmd.Stderr = lw

	err = cmd.Run()
	lw.flush()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), nil // script failure: report code, not err
		}
		return -1, err
	}
	return 0, nil
}

// hookShellArgv chooses the interpreter for the temp script file.
func hookShellArgv(path string) (string, []string) {
	if runtime.GOOS == "windows" {
		comspec := os.Getenv("COMSPEC")
		if comspec == "" {
			comspec = "cmd"
		}
		return comspec, []string{"/C", path}
	}
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	return sh, []string{path}
}

// hookLineWriter splits streamed bytes into lines, calling onLine per complete
// line; flush emits any trailing partial line.
type hookLineWriter struct {
	onLine func(string)
	buf    bytes.Buffer
}

func (w *hookLineWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	for {
		line, err := w.buf.ReadString('\n')
		if err != nil { // no full line yet; keep the remainder
			w.buf.Reset()
			w.buf.WriteString(line)
			break
		}
		w.onLine(line[:len(line)-1])
	}
	return len(p), nil
}

func (w *hookLineWriter) flush() {
	if rest := w.buf.String(); rest != "" {
		w.onLine(rest)
	}
}
