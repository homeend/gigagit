package tui

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

func TestAutowrapOffBracketsTheBody(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	want := errors.New("done")
	err := autowrapOff(&buf, func() error {
		buf.WriteString("frame")
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want the body's", err)
	}
	if got := buf.String(); got != "\x1b[?7lframe\x1b[?7h" {
		t.Fatalf("stream = %q: wrap must be off around the body and on again after it", got)
	}
}

func TestAutowrapOffRestoresWrapOnPanic(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	func() {
		defer func() {
			if recover() == nil {
				t.Errorf("the body's panic must propagate")
			}
		}()
		_ = autowrapOff(&buf, func() error { panic("boom") })
	}()
	if got := buf.String(); got != "\x1b[?7l\x1b[?7h" {
		t.Fatalf("stream = %q: wrap must be restored even when the body panics", got)
	}
}

// TestHandoverCmdWrapsOnAroundTheChild pins the handover contract: the
// terminal a child inherits must wrap again (CSI ?7 h) before the child
// starts, and wrap must be off again (CSI ?7 l) once it exits — on the same
// writer Bubble Tea handed the command, so the bytes land on the terminal
// the child paints, not on the child's own captured stdout.
func TestHandoverCmdWrapsOnAroundTheChild(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only: pins the child's exact stdout via sh -c printf")
	}
	var buf bytes.Buffer
	c := &handoverCmd{Cmd: exec.Command("sh", "-c", "printf child")}
	c.SetStdin(strings.NewReader(""))
	c.SetStdout(&buf)
	c.SetStderr(io.Discard)
	if err := c.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := buf.String(); got != "\x1b[?7hchild\x1b[?7l" {
		t.Fatalf("stream = %q: wrap must be on around the child and off again after it", got)
	}
}

// TestHandoverCmdRestoresWrapOffOnFailure: a child that exits non-zero (or
// never starts) must still leave the TUI's wrap-off in place, else the
// wide-glyph clip guard is lost for the rest of the session.
func TestHandoverCmdRestoresWrapOffOnFailure(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		cmd  *exec.Cmd
	}{
		{"non-zero exit", exitCmd(3)},
		{"never starts", exec.Command("/nonexistent/gg-no-such-binary")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			c := &handoverCmd{Cmd: tc.cmd}
			c.SetStdout(&buf)
			c.SetStderr(io.Discard)
			if err := c.Run(); err == nil {
				t.Fatalf("Run: want the child's error")
			}
			if got := buf.String(); got != "\x1b[?7h\x1b[?7l" {
				t.Fatalf("stream = %q: wrap must be off again even when the child fails", got)
			}
		})
	}
}

// TestHandoverCmdKeepsPresetStdio mirrors Bubble Tea's own wrapper: a
// command that already owns a stdio stream keeps it, so a caller that
// captured stdout still captures it — but the wrap bytes go to the
// terminal writer regardless, because that is the screen the child draws on.
func TestHandoverCmdKeepsPresetStdio(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only: pins the child's exact stdout via sh -c printf")
	}
	var term, captured bytes.Buffer
	cmd := exec.Command("sh", "-c", "printf child")
	cmd.Stdout = &captured
	c := &handoverCmd{Cmd: cmd}
	c.SetStdout(&term)
	c.SetStderr(io.Discard)
	if err := c.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if captured.String() != "child" {
		t.Fatalf("captured = %q, want the child's stdout kept", captured.String())
	}
	if got := term.String(); got != "\x1b[?7h\x1b[?7l" {
		t.Fatalf("terminal = %q: wrap bytes must reach the terminal, never the capture", got)
	}
}

// exitCmd is a child that exits with code on either shell family.
func exitCmd(code int) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/C", "exit "+strconv.Itoa(code))
	}
	return exec.Command("sh", "-c", "exit "+strconv.Itoa(code))
}

// TestNoRawExecProcessInTUI is the gate: every terminal handover must ride
// handover(), which brackets the child with wrap-on/wrap-off. A raw
// tea.ExecProcess call hands the child a non-wrapping terminal (the
// 2026-09-18 Junie "one line at the top" report).
func TestNoRawExecProcessInTUI(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "autowrap.go" {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "//") {
				continue
			}
			if strings.Contains(line, "tea.ExecProcess(") || strings.Contains(line, "tea.Exec(") {
				t.Errorf("%s:%d: raw Bubble Tea exec — use handover() so the child gets a wrapping terminal", f, i+1)
			}
		}
	}
}

// TestHandoverDoneWrapsTheCallback: the exec callback's own result rides
// inside handoverDoneMsg, so Update can tell "the terminal is ours again"
// apart from the child's result. A nil callback still yields the wrapper.
func TestHandoverDoneWrapsTheCallback(t *testing.T) {
	t.Parallel()
	want := errors.New("child failed")
	got := handoverDone(func(err error) tea.Msg { return shellDoneMsg{err: err} })(want)
	hd, ok := got.(handoverDoneMsg)
	if !ok {
		t.Fatalf("msg = %T, want handoverDoneMsg", got)
	}
	if inner, ok := hd.inner.(shellDoneMsg); !ok || inner.err != want {
		t.Fatalf("inner = %#v, want the callback's own shellDoneMsg", hd.inner)
	}
	if got := handoverDone(nil)(nil); got != (handoverDoneMsg{}) {
		t.Fatalf("nil callback: msg = %#v, want an empty handoverDoneMsg", got)
	}
}

// TestHandoverDoneReenablesMouse pins the fix for the dead mouse after the
// first editor / subshell / terminal-mode tool of a session: Bubble Tea's
// ReleaseTerminal switches mouse tracking off and RestoreTerminal never
// switches it back on, so Update must issue EnableMouseCellMotion when the
// handover returns — and still deliver the child's own result.
func TestHandoverDoneReenablesMouse(t *testing.T) {
	t.Parallel()
	m := Model{}
	nm, cmd := m.Update(handoverDoneMsg{inner: shellDoneMsg{err: errors.New("boom")}})
	if got := nm.(Model).statusMsg; got != i18n.T("shell: %s", "boom") {
		t.Fatalf("statusMsg = %q: the inner message must still be dispatched", got)
	}
	wantType := reflect.TypeOf(tea.EnableMouseCellMotion())
	for _, msg := range runCmdMsgs(cmd) {
		if reflect.TypeOf(msg) == wantType {
			return
		}
	}
	t.Fatalf("no EnableMouseCellMotion in the handover-done command: the mouse stays dead after a handover")
}
