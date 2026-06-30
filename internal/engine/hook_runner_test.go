package engine

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestShellHookRunnerStreamsAndEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script")
	}
	dir := t.TempDir()
	var got []string
	code, err := ShellHookRunner{}.Run(context.Background(),
		HookSpec{
			Dir:    dir,
			Env:    append([]string{}, "GG_BRANCH=feat/x"),
			Script: "printf 'a\\nb\\n'\necho \"branch=$GG_BRANCH\"\npwd\n",
		},
		func(line string) { got = append(got, line) })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"a", "b", "branch=feat/x"} {
		if !contains(got, want) {
			t.Fatalf("missing %q in output:\n%s", want, joined)
		}
	}
}

func TestShellHookRunnerNonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script")
	}
	code, err := ShellHookRunner{}.Run(context.Background(),
		HookSpec{Dir: t.TempDir(), Script: "exit 3\n"},
		func(string) {})
	if err != nil {
		t.Fatalf("non-zero exit must not be a Run error: %v", err)
	}
	if code != 3 {
		t.Fatalf("exit = %d, want 3", code)
	}
}

// TestHookLineWriterCRLF verifies that Windows-style CRLF hook output is
// stripped to clean lines (no trailing \r in the emitted strings).
func TestHookLineWriterCRLF(t *testing.T) {
	var got []string
	lw := &hookLineWriter{onLine: func(line string) { got = append(got, line) }}
	lw.Write([]byte("a\r\nb\r\n"))
	lw.flush()
	want := []string{"a", "b"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("line[%d] = %q, want %q", i, g, want[i])
		}
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if strings.TrimRight(s, "\r") == want {
			return true
		}
	}
	return false
}
