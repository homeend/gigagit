package gitexec

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/observ"
)

func TestExecRunnerRunEnvPassesEnvToSubprocess(t *testing.T) {
	r := NewExecRunner("git", t.TempDir(), nil)
	// `git var GIT_EDITOR` prints the editor git would use, which honors the
	// GIT_EDITOR environment variable. If our env reaches the subprocess, the
	// output is exactly what we set.
	res, err := r.RunEnv(context.Background(), "git var", []string{"var", "GIT_EDITOR"},
		[]string{"GIT_EDITOR=gg-test-editor"})
	if err != nil {
		t.Fatalf("RunEnv: %v", err)
	}
	if got := strings.TrimSpace(res.Stdout); got != "gg-test-editor" {
		t.Fatalf("GIT_EDITOR = %q, want gg-test-editor (env did not reach the subprocess)", got)
	}
}

func TestExecRunnerRunStillInheritsEnv(t *testing.T) {
	// Run (nil env) must keep working: it inherits the process environment.
	t.Setenv("GIT_EDITOR", "inherited-editor")
	r := NewExecRunner("git", t.TempDir(), nil)
	res, err := r.Run(context.Background(), "git var", []string{"var", "GIT_EDITOR"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := strings.TrimSpace(res.Stdout); got != "inherited-editor" {
		t.Fatalf("GIT_EDITOR = %q, want inherited-editor", got)
	}
}

// gtpArgv runs a git alias whose body is `!echo prompt=$GIT_TERMINAL_PROMPT`.
// A `!`-prefixed alias runs through git's own shell on every platform (git for
// Windows bundles one), so the printed value is exactly what the git subprocess
// received in its environment.
var gtpArgv = []string{"-c", "alias.gtp=!echo prompt=$GIT_TERMINAL_PROMPT", "gtp"}

// A TUI owns the terminal in raw mode; if git ever falls back to an interactive
// prompt (e.g. "Username for 'https://github.com':" when no credential helper is
// configured), it opens /dev/tty and blocks forever, freezing gg. Every git
// subprocess must therefore see GIT_TERMINAL_PROMPT=0 so it fails fast instead.
func TestExecRunnerDisablesTerminalPrompt(t *testing.T) {
	r := NewExecRunner("git", t.TempDir(), nil)

	t.Run("Run", func(t *testing.T) {
		res, err := r.Run(context.Background(), "gtp", gtpArgv)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := strings.TrimSpace(res.Stdout); got != "prompt=0" {
			t.Fatalf("GIT_TERMINAL_PROMPT in subprocess = %q, want prompt=0", got)
		}
	})

	t.Run("RunEnv", func(t *testing.T) {
		res, err := r.RunEnv(context.Background(), "gtp", gtpArgv, []string{"FOO=bar"})
		if err != nil {
			t.Fatalf("RunEnv: %v", err)
		}
		if got := strings.TrimSpace(res.Stdout); got != "prompt=0" {
			t.Fatalf("GIT_TERMINAL_PROMPT in subprocess = %q, want prompt=0", got)
		}
	})

	t.Run("Stream", func(t *testing.T) {
		var lines []string
		_, err := r.Stream(context.Background(), "gtp", gtpArgv, func(line string) {
			lines = append(lines, line)
		})
		if err != nil {
			t.Fatalf("Stream: %v", err)
		}
		if got := strings.TrimSpace(strings.Join(lines, "\n")); got != "prompt=0" {
			t.Fatalf("GIT_TERMINAL_PROMPT in subprocess = %q, want prompt=0", got)
		}
	})
}

// The disabled prompt must win even when the surrounding environment explicitly
// re-enables it: the TUI can never service a prompt it cannot type into.
func TestExecRunnerForcesTerminalPromptOverInheritedEnv(t *testing.T) {
	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	r := NewExecRunner("git", t.TempDir(), nil)

	res, err := r.Run(context.Background(), "gtp", gtpArgv)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := strings.TrimSpace(res.Stdout); got != "prompt=0" {
		t.Fatalf("GIT_TERMINAL_PROMPT in subprocess = %q, want prompt=0 (must override inherited =1)", got)
	}
}

func TestExecRunnerRunsGitVersion(t *testing.T) {
	rec := observ.NewRing(10)
	r := NewExecRunner("git", ".", rec)

	res, err := r.Run(context.Background(), "git version", []string{"version"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(res.Stdout, "git version") {
		t.Fatalf("stdout = %q, want it to start with 'git version'", res.Stdout)
	}
	spans := rec.Snapshot()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}
	if spans[0].Name != "git version" || spans[0].Duration <= 0 {
		t.Fatalf("span not recorded properly: %+v", spans[0])
	}
}

func TestExecRunnerNonZeroExitReturnsError(t *testing.T) {
	rec := observ.NewRing(10)
	r := NewExecRunner("git", ".", rec)

	res, err := r.Run(context.Background(), "git bogus", []string{"bogus-subcommand-xyz"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
	if res.ExitCode == 0 {
		t.Fatalf("exit code = 0, want non-zero")
	}
	spans := rec.Snapshot()
	if len(spans) != 1 || spans[0].ExitCode == 0 {
		t.Fatalf("span should record non-zero exit: %+v", spans)
	}
}

func TestExecRunnerHonorsContextCancellation(t *testing.T) {
	rec := observ.NewRing(10)
	r := NewExecRunner("git", ".", rec)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	_, err := r.Run(ctx, "git version", []string{"version"})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error should wrap context.Canceled, got %v", err)
	}
}
