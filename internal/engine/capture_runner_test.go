package engine

import (
	"context"
	"runtime"
	"testing"
)

func TestShellCaptureRunnerCapturesStdoutStreamsStderr(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	var lines []string
	out, err := ShellCaptureRunner{}.Capture(context.Background(),
		CaptureSpec{Command: "printf 'hello\\nworld'; printf 'progress\\n' 1>&2"},
		func(l string) { lines = append(lines, l) })
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "hello\nworld" {
		t.Fatalf("stdout=%q", out)
	}
	if len(lines) != 1 || lines[0] != "progress" {
		t.Fatalf("stderr lines=%v", lines)
	}
}

func TestShellCaptureRunnerNonZeroExitReturnsErr(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/exit")
	}
	_, err := ShellCaptureRunner{}.Capture(context.Background(),
		CaptureSpec{Command: "exit 3"}, func(string) {})
	if err == nil {
		t.Fatal("want error on non-zero exit")
	}
}

// TestShellCaptureRunnerCleanExitPastWaitDelaySucceeds reproduces a
// grandchild (e.g. an MCP subprocess spawned by an agent CLI) that outlives
// the shell script's own exit 0 and holds the inherited stdout/stderr pipes
// open past WaitDelay (3s). cmd.Run returns exec.ErrWaitDelay in that case
// even though stdout was fully captured beforehand — Capture must treat a
// clean (exit 0) ErrWaitDelay as success, not discard the good output as a
// failure. Takes ~3s (the real WaitDelay), hence -short gating.
func TestShellCaptureRunnerCleanExitPastWaitDelaySucceeds(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf/sleep")
	}
	out, err := ShellCaptureRunner{}.Capture(context.Background(),
		CaptureSpec{Command: "printf hi; sleep 5 & exit 0"}, func(string) {})
	if err != nil {
		t.Fatalf("err = %v, want nil (clean exit-0 must not be discarded as a failure)", err)
	}
	if string(out) != "hi" {
		t.Fatalf("stdout=%q, want %q", out, "hi")
	}
}
