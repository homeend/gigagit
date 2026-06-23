package gitexec

import (
	"context"
	"strings"
	"testing"
)

// gscArgv runs a git alias whose body is `!echo ssh=$GIT_SSH_COMMAND`, so the
// printed value is exactly the GIT_SSH_COMMAND the git subprocess received (same
// `!`-alias trick as gtpArgv for GIT_TERMINAL_PROMPT).
var gscArgv = []string{"-c", "alias.gsc=!echo ssh=$GIT_SSH_COMMAND", "gsc"}

func runGSC(t *testing.T, r *ExecRunner) string {
	t.Helper()
	res, err := r.Run(context.Background(), "gsc", gscArgv)
	if err != nil {
		t.Fatalf("run gsc: %v", err)
	}
	return strings.TrimSpace(res.Stdout)
}

// The default runner (the scriptable CLI's surface) must NOT impose
// GIT_SSH_COMMAND: a real terminal can answer an ssh prompt, and we must not
// clobber the user's ssh configuration.
func TestExecRunnerDefaultLeavesSSHCommandUntouched(t *testing.T) {
	t.Setenv("GIT_SSH_COMMAND", "")
	r := NewExecRunner("git", t.TempDir(), nil)
	if got := runGSC(t, r); got != "ssh=" {
		t.Fatalf("default runner GIT_SSH_COMMAND = %q, want empty (ssh=)", got)
	}
}

// The TUI runner forces ssh BatchMode so an ssh host-key or passphrase prompt
// fails fast instead of opening /dev/tty and freezing the raw-mode UI.
func TestExecRunnerBatchModeSetsSSHCommand(t *testing.T) {
	t.Setenv("GIT_SSH_COMMAND", "")
	r := NewExecRunner("git", t.TempDir(), nil).WithSSHBatchMode()
	if got := runGSC(t, r); got != "ssh=ssh -o BatchMode=yes" {
		t.Fatalf("batch runner GIT_SSH_COMMAND = %q, want ssh=ssh -o BatchMode=yes", got)
	}
}

// A user's custom GIT_SSH_COMMAND wrapper is preserved — BatchMode is appended,
// not replaced (so e.g. a `-i <key>` survives in the TUI).
func TestExecRunnerBatchModePreservesUserSSHCommand(t *testing.T) {
	t.Setenv("GIT_SSH_COMMAND", "ssh -i /tmp/key")
	r := NewExecRunner("git", t.TempDir(), nil).WithSSHBatchMode()
	if got := runGSC(t, r); got != "ssh=ssh -i /tmp/key -o BatchMode=yes" {
		t.Fatalf("batch runner GIT_SSH_COMMAND = %q, want the user's wrapper + BatchMode", got)
	}
}

// Forcing ssh BatchMode must not disturb the always-on HTTPS prompt disable.
func TestExecRunnerBatchModeKeepsTerminalPromptDisabled(t *testing.T) {
	r := NewExecRunner("git", t.TempDir(), nil).WithSSHBatchMode()
	res, err := r.Run(context.Background(), "gtp", gtpArgv)
	if err != nil {
		t.Fatalf("run gtp: %v", err)
	}
	if got := strings.TrimSpace(res.Stdout); got != "prompt=0" {
		t.Fatalf("GIT_TERMINAL_PROMPT with batch mode = %q, want prompt=0", got)
	}
}

func TestSSHBatchCommand(t *testing.T) {
	for _, tc := range []struct{ base, want string }{
		{"", "ssh -o BatchMode=yes"},
		{"   ", "ssh -o BatchMode=yes"},
		{"ssh", "ssh -o BatchMode=yes"},
		{"ssh -i /tmp/key", "ssh -i /tmp/key -o BatchMode=yes"},
		{"  ssh -F /dev/null  ", "ssh -F /dev/null -o BatchMode=yes"},
	} {
		if got := sshBatchCommand(tc.base); got != tc.want {
			t.Errorf("sshBatchCommand(%q) = %q, want %q", tc.base, got, tc.want)
		}
	}
}
