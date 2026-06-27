package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

// failOp returns a plain error so engine.OpName(failOp{}) == "failOp".
type failOp struct{}

func (failOp) Run(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
	return engine.Result{}, errors.New("boom")
}

func TestDefaultErrLogPathName(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p := defaultErrLogPath()
	if p == "" {
		t.Fatal("expected a path with XDG_STATE_HOME set")
	}
	if filepath.Base(p) != "errors.log" {
		t.Fatalf("want basename errors.log, got %q", p)
	}
}

func TestOpenErrorLogCreatesAppendable(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	f, path, err := OpenErrorLog()
	if err != nil {
		t.Fatalf("OpenErrorLog: %v", err)
	}
	if f == nil {
		t.Fatal("expected a file handle with a state dir set")
	}
	defer f.Close()
	if _, err := f.WriteString("x\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("errors.log not created: %v", err)
	}
}

// TestErrorLogSeamEndToEnd exercises the exact chain cmd/gg wires at startup:
// OpenErrorLog -> SetFailureSink -> a real failing op via domain.Execute ->
// a line lands in errors.log AND the session viewer sees it.
func TestErrorLogSeamEndToEnd(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	observ.ResetFailures()

	f, path, err := OpenErrorLog()
	if err != nil || f == nil {
		t.Fatalf("OpenErrorLog: f=%v err=%v", f, err)
	}
	observ.SetFailureSink(f)
	defer func() { observ.SetFailureSink(nil); _ = f.Close() }()

	svc := domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	if _, err := svc.Execute(context.Background(), failOp{}, nil, nil); err == nil {
		t.Fatal("expected Execute(failOp) to fail")
	}

	// (a) The session viewer sees it.
	if fs := observ.SessionFailures(); len(fs) != 1 || fs[0].Source != "op failOp" {
		t.Fatalf("session ring missing the failure: %+v", fs)
	}
	// (b) The durable file actually received a line.
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("read errors.log: %v", rerr)
	}
	if !strings.Contains(string(data), "op failOp") || !strings.Contains(string(data), "boom") {
		t.Fatalf("errors.log missing the failure line:\n%s", data)
	}
}
