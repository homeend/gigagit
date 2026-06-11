package gitexec

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gigagit/gg/internal/observ"
)

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
	_ = time.Now
}
