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
