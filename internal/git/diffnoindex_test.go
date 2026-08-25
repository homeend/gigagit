package git

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

// Exit 1 from `git diff --no-index` means "files differ" — the normal outcome,
// not an error (the ConfigUnset exit-5 pattern).
func TestDiffNoIndexExitOneIsDiff(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git diff --no-index", gitexec.Result{Stdout: "--- a\n+++ b\n@@\n", ExitCode: 1})
	f.SetError("git diff --no-index", errors.New("exit status 1"))
	r := &Repo{Runner: f}
	out, err := r.DiffNoIndex(context.Background(), "/tmp/a", "/tmp/b")
	if err != nil {
		t.Fatalf("exit 1 must not be an error: %v", err)
	}
	if !strings.Contains(out, "+++ b") {
		t.Fatalf("diff output lost: %q", out)
	}
	call := f.Calls[len(f.Calls)-1]
	want := []string{"diff", "--no-index", "--", "/tmp/a", "/tmp/b"}
	if len(call.Argv) != len(want) {
		t.Fatalf("argv = %v, want %v", call.Argv, want)
	}
	for i := range want {
		if call.Argv[i] != want[i] {
			t.Fatalf("argv = %v, want %v", call.Argv, want)
		}
	}
}

func TestDiffNoIndexIdentical(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git diff --no-index", gitexec.Result{Stdout: "", ExitCode: 0})
	r := &Repo{Runner: f}
	out, err := r.DiffNoIndex(context.Background(), "/tmp/a", "/tmp/a")
	if err != nil || out != "" {
		t.Fatalf("identical files: out=%q err=%v", out, err)
	}
}

func TestDiffNoIndexRealError(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git diff --no-index", gitexec.Result{ExitCode: 128, Stderr: "fatal: bad"})
	f.SetError("git diff --no-index", errors.New("exit status 128"))
	r := &Repo{Runner: f}
	if _, err := r.DiffNoIndex(context.Background(), "/nope", "/nope2"); err == nil {
		t.Fatal("exit 128 must surface as an error")
	}
}
