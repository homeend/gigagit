package domain

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/repogate"
)

// fakeOp runs body under whatever OpDeps Execute assembled.
type fakeOp struct {
	mode *repogate.Mode // used by lockedOp; nil on plain fakeOp
	body func(ctx context.Context, deps engine.OpDeps) (engine.Result, error)
}

func (o fakeOp) Run(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
	return o.body(ctx, deps)
}

// lockedOp is a fakeOp that declares a LockMode.
type lockedOp struct{ fakeOp }

func (o lockedOp) LockMode() repogate.Mode { return *o.mode }

func modePtr(m repogate.Mode) *repogate.Mode { return &m }

// svcWithKey builds a Service over a FakeRunner that resolves the common dir
// to key — each test uses a unique key because the gate registry is global.
func svcWithKey(key string) (*Service, *gitexec.FakeRunner) {
	fr := gitexec.NewFakeRunner()
	fr.SetResponse("git rev-parse (common-dir)", gitexec.Result{Stdout: key + "\n"})
	return New(&git.Repo{Runner: fr}), fr
}

func TestExecuteHoldsTreeWriteByDefault(t *testing.T) {
	svc, _ := svcWithKey("/domain-test-default")
	var seen []repogate.Entry
	op := fakeOp{body: func(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
		seen = repogate.For("/domain-test-default").Queue()
		return engine.Result{Summary: "ok"}, nil
	}}
	res, err := svc.Execute(context.Background(), op, nil, nil)
	if err != nil || res.Summary != "ok" {
		t.Fatalf("execute: %v %+v", err, res)
	}
	if len(seen) != 1 || seen[0].Mode != repogate.TreeWrite || seen[0].Waiting {
		t.Fatalf("mid-op gate state = %+v, want one held TreeWrite", seen)
	}
	if q := repogate.For("/domain-test-default").Queue(); len(q) != 0 {
		t.Fatalf("gate not released after Execute: %+v", q)
	}
}

func TestExecuteRespectsLockMode(t *testing.T) {
	svc, _ := svcWithKey("/domain-test-mode")
	var seen []repogate.Entry
	op := lockedOp{fakeOp{mode: modePtr(repogate.Read), body: func(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
		seen = repogate.For("/domain-test-mode").Queue()
		return engine.Result{}, nil
	}}}
	if _, err := svc.Execute(context.Background(), op, nil, nil); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(seen) != 1 || seen[0].Mode != repogate.Read {
		t.Fatalf("mid-op gate state = %+v, want one held Read", seen)
	}
}

func TestExecuteWiresEscalate(t *testing.T) {
	svc, _ := svcWithKey("/domain-test-escalate")
	var after []repogate.Entry
	op := lockedOp{fakeOp{mode: modePtr(repogate.RefWrite), body: func(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
		if deps.Escalate == nil {
			t.Fatal("Execute did not wire Escalate")
		}
		if err := deps.Escalate(ctx); err != nil {
			return engine.Result{}, err
		}
		after = repogate.For("/domain-test-escalate").Queue()
		return engine.Result{}, nil
	}}}
	if _, err := svc.Execute(context.Background(), op, nil, nil); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(after) != 1 || after[0].Mode != repogate.TreeWrite {
		t.Fatalf("post-escalate gate state = %+v, want one held TreeWrite", after)
	}
	if q := repogate.For("/domain-test-escalate").Queue(); len(q) != 0 {
		t.Fatalf("gate not released after escalated Execute: %+v", q)
	}
}

func TestExecuteEscalateCancelledReleasesCleanly(t *testing.T) {
	svc, _ := svcWithKey("/domain-test-esc-cancel")
	blocker, err := repogate.For("/domain-test-esc-cancel").Acquire(context.Background(), repogate.Read, "blocker")
	if err != nil {
		t.Fatal(err)
	}
	op := lockedOp{fakeOp{mode: modePtr(repogate.RefWrite), body: func(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
		ictx, icancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer icancel()
		return engine.Result{}, deps.Escalate(ictx) // blocked by the Read holder
	}}}
	if _, err := svc.Execute(context.Background(), op, nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded from the failed escalation", err)
	}
	blocker.Release()
	// No panic from the deferred release, and the gate is free.
	if q := repogate.For("/domain-test-esc-cancel").Queue(); len(q) != 0 {
		t.Fatalf("gate state after failed escalation = %+v, want empty", q)
	}
}

func TestExecuteCancelledWhileQueued(t *testing.T) {
	svc, _ := svcWithKey("/domain-test-cancel")
	hold, err := repogate.For("/domain-test-cancel").Acquire(context.Background(), repogate.TreeWrite, "blocker")
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Release()
	ran := false
	op := fakeOp{body: func(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
		ran = true
		return engine.Result{}, nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := svc.Execute(ctx, op, nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
	if ran {
		t.Fatal("op ran despite the gate never granting")
	}
}

func TestExecuteSameGateAcrossCalls(t *testing.T) {
	// Two Executes on one Service must contend on the same gate even though
	// the common dir resolves only once (it is cached).
	svc, fr := svcWithKey("/domain-test-shared")
	started := make(chan struct{})
	release := make(chan struct{})
	go svc.Execute(context.Background(), fakeOp{body: func(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
		close(started)
		<-release
		return engine.Result{}, nil
	}}, nil, nil)
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := svc.Execute(ctx, fakeOp{body: func(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
		return engine.Result{}, nil
	}}, nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("second Execute did not contend with the first")
	}
	close(release)
	// The common dir was resolved exactly once.
	n := 0
	for _, c := range fr.Calls {
		if c.Name == "git rev-parse (common-dir)" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("common-dir resolved %d times, want 1 (cached)", n)
	}
}

func TestExecuteFallbackKeyWhenCommonDirFails(t *testing.T) {
	fr := gitexec.NewFakeRunner()
	fr.SetError("git rev-parse (common-dir)", errors.New("not a repo"))
	svc := New(&git.Repo{Runner: fr})
	ran := false
	if _, err := svc.Execute(context.Background(), fakeOp{body: func(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
		ran = true
		return engine.Result{}, nil
	}}, nil, nil); err != nil || !ran {
		t.Fatalf("execute with fallback key: err=%v ran=%v", err, ran)
	}
}

func TestRepoExposesUnderlyingRepo(t *testing.T) {
	r := &git.Repo{Runner: gitexec.NewFakeRunner()}
	if New(r).Repo() != r {
		t.Fatal("Repo() must hand back the wrapped repo")
	}
}

// TestExecuteEmitsOpSpan: the "op <Name>" span (with error fields on
// failure) moved from the frontend shells into Execute — pin it here.
func TestExecuteEmitsOpSpan(t *testing.T) {
	var buf syncBuffer
	observ.SetSpanSink(&buf)
	defer observ.SetSpanSink(nil)

	svc, _ := svcWithKey("/domain-test-span")
	boom := errors.New("boom")
	op := fakeOp{body: func(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
		return engine.Result{}, boom
	}}
	if _, err := svc.Execute(context.Background(), op, nil, nil); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	s := buf.String()
	if !strings.Contains(s, "op fakeOp") || !strings.Contains(s, "boom") {
		t.Fatalf("op span missing or lacks error fields: %s", s)
	}
}

// syncBuffer is a goroutine-safe bytes.Buffer for span-sink capture.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
