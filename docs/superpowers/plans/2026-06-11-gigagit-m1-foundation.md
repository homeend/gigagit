# gigagit M1 — Plan 1: Foundation & Read-Only Inspection — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the bottom layers of gigagit — process exec, command builder, data models, observability (ring buffer / tracing / debug dump), and read-only git loaders — wired into a temporary `gg inspect` command that prints repo status/branches/worktrees and can emit a debug dump and timing trace.

**Architecture:** Pure-Go layers that shell out to the system `git` binary. Layering mirrors lazygit's proven shape: `gitexec` (process layer + fake) → `gitcmd` (fluent builder) → `git` (thin verbs + pure parsers) over `model` (shared types), with `observ` (ring buffer + spans + tracing + dump) as a leaf dependency of `gitexec`. No TUI/engine yet; this plan delivers a testable foundation.

**Tech Stack:** Go 1.26, standard library only (`os/exec`, `context`, `encoding/json`, `testing`). Module path `github.com/gigagit/gg`. Tests run against real throwaway git repos in temp dirs plus table-driven parser tests over captured porcelain.

---

## Shared interfaces (defined once, referenced by later tasks)

These signatures are fixed across the plan. Later tasks must match them exactly.

```go
// internal/observ
type Span struct {
    ID       int64
    ParentID int64         // 0 = root
    Name     string        // "git status", "op:smart-pull/stash", ...
    Args     []string      // already redacted
    ExitCode int
    Err      string        // "" if none
    Start    time.Time
    Duration time.Duration
}
type Recorder interface{ Record(s Span) }   // Ring implements this
func Redact(args []string) []string

// internal/gitcmd
type Builder struct{ /* args []string */ }
func New(subcommand string) *Builder
func (b *Builder) Arg(a ...string) *Builder
func (b *Builder) ArgIf(cond bool, a ...string) *Builder
func (b *Builder) Config(kv string) *Builder   // prepends "-c" "kv"
func (b *Builder) Dir(path string) *Builder    // prepends "-C" "path"
func (b *Builder) ToArgv() []string

// internal/gitexec
type Result struct {
    Stdout, Stderr string
    ExitCode       int
    Duration       time.Duration
}
type Runner interface {
    Run(ctx context.Context, name string, argv []string) (Result, error)
    Stream(ctx context.Context, name string, argv []string, onLine func(string)) (Result, error)
}
```

`name` is the human label recorded as the span name (e.g. `"git status"`); `argv` is the full git argument vector from `Builder.ToArgv()`.

---

## Task 1: Scaffold the Go module

**Files:**
- Create: `go.mod`
- Create: `cmd/gg/main.go`
- Create: `internal/buildinfo/buildinfo.go`
- Test: `internal/buildinfo/buildinfo_test.go`

- [ ] **Step 1: Initialize the module**

Run:
```bash
cd /mnt/t/others/gigagit
go mod init github.com/gigagit/gg
```
Expected: creates `go.mod` containing `module github.com/gigagit/gg`. Then edit `go.mod` so the version line reads `go 1.26`.

- [ ] **Step 2: Write the failing test**

Create `internal/buildinfo/buildinfo_test.go`:
```go
package buildinfo

import "testing"

func TestStringIncludesVersion(t *testing.T) {
	Version = "9.9.9"
	if got := String(); got == "" {
		t.Fatal("String() returned empty")
	}
	if want := "9.9.9"; !contains(String(), want) {
		t.Fatalf("String() = %q, want it to contain %q", String(), want)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/buildinfo/`
Expected: FAIL — `buildinfo.go` does not exist (build error: undefined `Version`, `String`).

- [ ] **Step 4: Write minimal implementation**

Create `internal/buildinfo/buildinfo.go`:
```go
// Package buildinfo exposes version and build metadata, set via -ldflags at build time.
package buildinfo

import (
	"fmt"
	"runtime"
)

// Version is overridden at build time with -ldflags "-X .../buildinfo.Version=...".
var Version = "dev"

// Commit is overridden at build time.
var Commit = "none"

// String returns a one-line human-readable build identifier.
func String() string {
	return fmt.Sprintf("gg %s (%s) %s/%s", Version, Commit, runtime.GOOS, runtime.GOARCH)
}
```

Create `cmd/gg/main.go`:
```go
package main

import (
	"fmt"

	"github.com/gigagit/gg/internal/buildinfo"
)

func main() {
	fmt.Println(buildinfo.String())
}
```

- [ ] **Step 5: Run test + build to verify pass**

Run: `go test ./internal/buildinfo/ && go build ./...`
Expected: PASS; build succeeds.

- [ ] **Step 6: Commit**

```bash
git add go.mod cmd/gg internal/buildinfo
git commit -m "feat: scaffold gg module with buildinfo"
```

---

## Task 2: Observability ring buffer + Span

**Files:**
- Create: `internal/observ/ring.go`
- Test: `internal/observ/ring_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/observ/ring_test.go`:
```go
package observ

import (
	"testing"
	"time"
)

func span(name string) Span {
	return Span{Name: name, Start: time.Now(), Duration: time.Millisecond}
}

func TestRingRecordsAndSnapshots(t *testing.T) {
	r := NewRing(3)
	r.Record(span("a"))
	r.Record(span("b"))
	got := r.Snapshot()
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("order wrong: %q, %q", got[0].Name, got[1].Name)
	}
}

func TestRingEvictsOldestBeyondCapacity(t *testing.T) {
	r := NewRing(2)
	r.Record(span("a"))
	r.Record(span("b"))
	r.Record(span("c"))
	got := r.Snapshot()
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (capped)", len(got))
	}
	if got[0].Name != "b" || got[1].Name != "c" {
		t.Fatalf("eviction wrong: %q, %q (want b, c)", got[0].Name, got[1].Name)
	}
}

func TestRingAssignsIncrementingIDs(t *testing.T) {
	r := NewRing(10)
	r.Record(span("a"))
	r.Record(span("b"))
	got := r.Snapshot()
	if got[0].ID == 0 || got[1].ID == 0 {
		t.Fatal("IDs should be non-zero")
	}
	if got[1].ID <= got[0].ID {
		t.Fatalf("IDs should increment: %d then %d", got[0].ID, got[1].ID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/observ/`
Expected: FAIL — undefined `NewRing`, `Span`, `Record`, `Snapshot`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/observ/ring.go`:
```go
// Package observ provides always-on lightweight observability: a bounded ring
// buffer of recent spans, opt-in tracing, and debug-dump serialization.
package observ

import (
	"sync"
	"time"
)

// Span is one recorded unit of work: a git subprocess or an operation step.
type Span struct {
	ID       int64         `json:"id"`
	ParentID int64         `json:"parent_id,omitempty"`
	Name     string        `json:"name"`
	Args     []string      `json:"args,omitempty"`
	ExitCode int           `json:"exit_code"`
	Err      string        `json:"err,omitempty"`
	Start    time.Time     `json:"start"`
	Duration time.Duration `json:"duration_ns"`
}

// Recorder receives completed spans.
type Recorder interface{ Record(s Span) }

// Ring is a bounded, concurrency-safe Recorder retaining the last N spans.
type Ring struct {
	mu     sync.Mutex
	buf    []Span
	cap    int
	nextID int64
}

// NewRing returns a Ring retaining up to capacity spans.
func NewRing(capacity int) *Ring {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring{cap: capacity}
}

// Record stores a span, assigning it a monotonic ID and evicting the oldest
// span when capacity is exceeded.
func (r *Ring) Record(s Span) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	s.ID = r.nextID
	r.buf = append(r.buf, s)
	if len(r.buf) > r.cap {
		r.buf = r.buf[len(r.buf)-r.cap:]
	}
}

// Snapshot returns a copy of the retained spans, oldest first.
func (r *Ring) Snapshot() []Span {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Span, len(r.buf))
	copy(out, r.buf)
	return out
}

// NextID exposes the next ID the ring will assign; used by callers that need a
// parent span ID before recording children. It does not advance the counter.
func (r *Ring) PeekNextID() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.nextID + 1
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/observ/`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/observ/ring.go internal/observ/ring_test.go
git commit -m "feat: add observ ring buffer and Span"
```

---

## Task 3: Secret redaction for recorded args

**Files:**
- Create: `internal/observ/redact.go`
- Test: `internal/observ/redact_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/observ/redact_test.go`:
```go
package observ

import (
	"reflect"
	"testing"
)

func TestRedactStripsCredentialConfig(t *testing.T) {
	in := []string{"-c", "credential.helper=store --file=/tmp/x", "push"}
	got := Redact(in)
	want := []string{"-c", "credential.helper=<redacted>", "push"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRedactStripsUserinfoInURLs(t *testing.T) {
	in := []string{"clone", "https://alice:secrettoken@example.com/repo.git"}
	got := Redact(in)
	want := []string{"clone", "https://alice:<redacted>@example.com/repo.git"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRedactLeavesPlainArgsUntouched(t *testing.T) {
	in := []string{"status", "--porcelain=v2", "-z"}
	got := Redact(in)
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("got %v, want unchanged %v", got, in)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/observ/ -run TestRedact`
Expected: FAIL — undefined `Redact`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/observ/redact.go`:
```go
package observ

import "regexp"

// userinfoURL matches the "user:password@" portion of a URL, capturing the user.
var userinfoURL = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^:/@\s]+):[^@/\s]+@`)

// credentialConfig matches a "credential.*=value" git -c config string.
var credentialConfig = regexp.MustCompile(`^(credential\.[^=]*)=.*$`)

// Redact returns a copy of args with secrets masked: URL passwords and
// credential.* config values are replaced with "<redacted>".
func Redact(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		a = userinfoURL.ReplaceAllString(a, `$1:<redacted>@`)
		a = credentialConfig.ReplaceAllString(a, `$1=<redacted>`)
		out[i] = a
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/observ/ -run TestRedact`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/observ/redact.go internal/observ/redact_test.go
git commit -m "feat: add secret redaction for recorded git args"
```

---

## Task 4: Fluent git command builder

**Files:**
- Create: `internal/gitcmd/builder.go`
- Test: `internal/gitcmd/builder_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/gitcmd/builder_test.go`:
```go
package gitcmd

import (
	"reflect"
	"testing"
)

func TestBuilderBasicArgv(t *testing.T) {
	got := New("status").Arg("--porcelain=v2", "-z").ToArgv()
	want := []string{"status", "--porcelain=v2", "-z"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuilderArgIf(t *testing.T) {
	got := New("fetch").ArgIf(true, "--all").ArgIf(false, "--prune").ToArgv()
	want := []string{"fetch", "--all"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuilderDirAndConfigPrepend(t *testing.T) {
	// -C and -c must come BEFORE the subcommand.
	got := New("pull").Dir("/repo").Config("pull.ff=only").ToArgv()
	want := []string{"-c", "pull.ff=only", "-C", "/repo", "pull"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
```

Note: `Config` is applied after `Dir` in the test, and each prepends to the front, so the last-applied (`Config`) ends up first. This is the intended, documented ordering.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gitcmd/`
Expected: FAIL — undefined `New`, builder methods.

- [ ] **Step 3: Write minimal implementation**

Create `internal/gitcmd/builder.go`:
```go
// Package gitcmd builds git argument vectors fluently. Global options (-C, -c)
// are prepended so they precede the subcommand, as git requires.
package gitcmd

// Builder accumulates a git argument vector.
type Builder struct {
	args []string
}

// New starts a builder for the given git subcommand (e.g. "status").
func New(subcommand string) *Builder {
	return &Builder{args: []string{subcommand}}
}

// Arg appends positional arguments/flags.
func (b *Builder) Arg(a ...string) *Builder {
	b.args = append(b.args, a...)
	return b
}

// ArgIf appends arguments only when cond is true.
func (b *Builder) ArgIf(cond bool, a ...string) *Builder {
	if cond {
		b.args = append(b.args, a...)
	}
	return b
}

// Config prepends "-c kv" before the subcommand.
func (b *Builder) Config(kv string) *Builder {
	b.args = append([]string{"-c", kv}, b.args...)
	return b
}

// Dir prepends "-C path" before the subcommand, making git operate in path.
func (b *Builder) Dir(path string) *Builder {
	b.args = append([]string{"-C", path}, b.args...)
	return b
}

// ToArgv returns the assembled argument vector.
func (b *Builder) ToArgv() []string {
	out := make([]string, len(b.args))
	copy(out, b.args)
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/gitcmd/`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/gitcmd
git commit -m "feat: add fluent git command builder"
```

---

## Task 5: gitexec Runner — fake first

**Files:**
- Create: `internal/gitexec/runner.go`
- Create: `internal/gitexec/fake.go`
- Test: `internal/gitexec/fake_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/gitexec/fake_test.go`:
```go
package gitexec

import (
	"context"
	"testing"
)

func TestFakeRunnerReturnsConfiguredResult(t *testing.T) {
	f := NewFakeRunner()
	f.SetResponse("git status", Result{Stdout: "clean", ExitCode: 0})

	res, err := f.Run(context.Background(), "git status", []string{"status"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Stdout != "clean" {
		t.Fatalf("stdout = %q, want %q", res.Stdout, "clean")
	}
	if len(f.Calls) != 1 || f.Calls[0].Name != "git status" {
		t.Fatalf("call not recorded: %+v", f.Calls)
	}
}

func TestFakeRunnerStreamsLines(t *testing.T) {
	f := NewFakeRunner()
	f.SetResponse("git log", Result{Stdout: "line1\nline2\n"})

	var got []string
	_, err := f.Stream(context.Background(), "git log", []string{"log"}, func(l string) {
		got = append(got, l)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "line1" || got[1] != "line2" {
		t.Fatalf("streamed lines = %v, want [line1 line2]", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gitexec/`
Expected: FAIL — undefined `NewFakeRunner`, `Result`, `Run`, `Stream`.

- [ ] **Step 3: Write the interface + fake**

Create `internal/gitexec/runner.go`:
```go
// Package gitexec runs the system git binary, records timing spans, and exposes
// a Runner interface with a fake for tests.
package gitexec

import (
	"context"
	"time"
)

// Result is the outcome of one git invocation.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// Runner executes git commands. name is the human label recorded as a span;
// argv is the full git argument vector (from gitcmd.Builder.ToArgv()).
type Runner interface {
	Run(ctx context.Context, name string, argv []string) (Result, error)
	Stream(ctx context.Context, name string, argv []string, onLine func(string)) (Result, error)
}
```

Create `internal/gitexec/fake.go`:
```go
package gitexec

import (
	"context"
	"fmt"
	"strings"
)

// FakeCall records one invocation of the fake runner.
type FakeCall struct {
	Name string
	Argv []string
}

// FakeRunner is an in-memory Runner for tests.
type FakeRunner struct {
	responses map[string]Result
	errs      map[string]error
	Calls     []FakeCall
}

// NewFakeRunner returns an empty fake runner.
func NewFakeRunner() *FakeRunner {
	return &FakeRunner{responses: map[string]Result{}, errs: map[string]error{}}
}

// SetResponse configures the Result returned for a given span name.
func (f *FakeRunner) SetResponse(name string, r Result) { f.responses[name] = r }

// SetError configures an error returned for a given span name.
func (f *FakeRunner) SetError(name string, err error) { f.errs[name] = err }

func (f *FakeRunner) Run(_ context.Context, name string, argv []string) (Result, error) {
	f.Calls = append(f.Calls, FakeCall{Name: name, Argv: argv})
	if err := f.errs[name]; err != nil {
		return f.responses[name], err
	}
	r, ok := f.responses[name]
	if !ok {
		return Result{}, fmt.Errorf("fake: no response configured for %q", name)
	}
	return r, nil
}

func (f *FakeRunner) Stream(ctx context.Context, name string, argv []string, onLine func(string)) (Result, error) {
	r, err := f.Run(ctx, name, argv)
	if err != nil {
		return r, err
	}
	for _, line := range strings.Split(strings.TrimRight(r.Stdout, "\n"), "\n") {
		if line != "" || r.Stdout != "" {
			onLine(line)
		}
	}
	return r, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/gitexec/`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/gitexec/runner.go internal/gitexec/fake.go internal/gitexec/fake_test.go
git commit -m "feat: add gitexec Runner interface and fake"
```

---

## Task 6: gitexec real runner (executes git, records spans, cancellable)

**Files:**
- Create: `internal/gitexec/exec.go`
- Test: `internal/gitexec/exec_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/gitexec/exec_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gitexec/ -run TestExecRunner`
Expected: FAIL — undefined `NewExecRunner`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/gitexec/exec.go`:
```go
package gitexec

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/gigagit/gg/internal/observ"
)

// ExecRunner runs the real git binary and records a span per invocation.
type ExecRunner struct {
	gitPath  string
	workDir  string
	recorder observ.Recorder
	now      func() time.Time
}

// NewExecRunner returns a runner that invokes gitPath in workDir, recording
// spans to rec (may be nil to disable recording).
func NewExecRunner(gitPath, workDir string, rec observ.Recorder) *ExecRunner {
	if gitPath == "" {
		gitPath = "git"
	}
	return &ExecRunner{gitPath: gitPath, workDir: workDir, recorder: rec, now: time.Now}
}

func (r *ExecRunner) record(name string, argv []string, exit int, dur time.Duration, start time.Time, runErr error) {
	if r.recorder == nil {
		return
	}
	errStr := ""
	if runErr != nil {
		errStr = runErr.Error()
	}
	r.recorder.Record(observ.Span{
		Name:     name,
		Args:     observ.Redact(argv),
		ExitCode: exit,
		Err:      errStr,
		Start:    start,
		Duration: dur,
	})
}

func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if asExit(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func (r *ExecRunner) Run(ctx context.Context, name string, argv []string) (Result, error) {
	start := r.now()
	cmd := exec.CommandContext(ctx, r.gitPath, argv...)
	cmd.Dir = r.workDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	dur := r.now().Sub(start)
	exit := exitCodeOf(runErr)
	r.record(name, argv, exit, dur, start, runErr)

	res := Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exit, Duration: dur}
	if runErr != nil {
		return res, fmt.Errorf("%s failed (exit %d): %s", name, exit, strings.TrimSpace(stderr.String()))
	}
	return res, nil
}

func (r *ExecRunner) Stream(ctx context.Context, name string, argv []string, onLine func(string)) (Result, error) {
	start := r.now()
	cmd := exec.CommandContext(ctx, r.gitPath, argv...)
	cmd.Dir = r.workDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}
	var all strings.Builder
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		all.WriteString(line)
		all.WriteByte('\n')
		onLine(line)
	}
	runErr := cmd.Wait()
	dur := r.now().Sub(start)
	exit := exitCodeOf(runErr)
	r.record(name, argv, exit, dur, start, runErr)

	res := Result{Stdout: all.String(), Stderr: stderr.String(), ExitCode: exit, Duration: dur}
	if runErr != nil {
		return res, fmt.Errorf("%s failed (exit %d): %s", name, exit, strings.TrimSpace(stderr.String()))
	}
	return res, nil
}
```

Create the small `asExit` helper in the same file (kept separate so the cast is testable/clear):
```go
func asExit(err error, target **exec.ExitError) bool {
	for err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			*target = ee
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/gitexec/`
Expected: PASS (all tests, including the fake tests). Requires `git` on PATH.

- [ ] **Step 5: Commit**

```bash
git add internal/gitexec/exec.go internal/gitexec/exec_test.go
git commit -m "feat: add real gitexec runner with span recording and cancellation"
```

---

## Task 7: Shared data models

**Files:**
- Create: `internal/model/model.go`
- Test: `internal/model/model_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/model/model_test.go`:
```go
package model

import "testing"

func TestCountsFromStatus(t *testing.T) {
	st := WorkingTreeStatus{
		Files: []FileStatus{
			{Path: "a.go", Staged: 'M', Unstaged: '.'},
			{Path: "b.go", Staged: '.', Unstaged: 'M'},
			{Path: "c.go", Kind: KindUntracked},
			{Path: "d.go", Kind: KindUnmerged},
		},
	}
	c := st.Counts()
	if c.Staged != 1 {
		t.Errorf("Staged = %d, want 1", c.Staged)
	}
	if c.Unstaged != 1 {
		t.Errorf("Unstaged = %d, want 1", c.Unstaged)
	}
	if c.Untracked != 1 {
		t.Errorf("Untracked = %d, want 1", c.Untracked)
	}
	if c.Conflicted != 1 {
		t.Errorf("Conflicted = %d, want 1", c.Conflicted)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/model/`
Expected: FAIL — undefined types.

- [ ] **Step 3: Write minimal implementation**

Create `internal/model/model.go`:
```go
// Package model holds shared git data types used across the engine and frontends.
package model

// FileKind classifies a changed path.
type FileKind int

const (
	KindTracked FileKind = iota
	KindUntracked
	KindIgnored
	KindUnmerged
)

// FileStatus is one entry from `git status --porcelain=v2`.
// Staged/Unstaged hold the porcelain XY status bytes ('.' = unmodified).
type FileStatus struct {
	Path     string
	OrigPath string // populated for renames/copies
	Staged   byte
	Unstaged byte
	Kind     FileKind
}

// WorkingTreeStatus is a snapshot of the working tree and branch position.
type WorkingTreeStatus struct {
	Branch   string
	Upstream string
	Ahead    int
	Behind   int
	Files    []FileStatus
}

// Counts summarises a WorkingTreeStatus.
type Counts struct {
	Staged     int
	Unstaged   int
	Untracked  int
	Conflicted int
}

// Counts tallies file states. A conflicted (unmerged) file is counted only as
// Conflicted, never as Staged/Unstaged.
func (w WorkingTreeStatus) Counts() Counts {
	var c Counts
	for _, f := range w.Files {
		switch f.Kind {
		case KindUntracked:
			c.Untracked++
		case KindUnmerged:
			c.Conflicted++
		default:
			if f.Staged != '.' && f.Staged != 0 {
				c.Staged++
			}
			if f.Unstaged != '.' && f.Unstaged != 0 {
				c.Unstaged++
			}
		}
	}
	return c
}

// Branch is a local branch ref.
type Branch struct {
	Name     string
	Upstream string
	Ahead    int
	Behind   int
	IsHead   bool
	Hash     string
}

// Worktree is one entry from `git worktree list --porcelain`.
type Worktree struct {
	Path     string
	Branch   string // short branch name, "" if detached/bare
	Head     string
	Detached bool
	Bare     bool
}

// Commit is one entry from the commit log.
type Commit struct {
	Hash    string
	Parents []string
	Author  string
	Subject string
	UnixTime int64
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/model/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/model
git commit -m "feat: add shared git data models"
```

---

## Task 8: Status parser (porcelain v2, -z)

**Files:**
- Create: `internal/git/status_parse.go`
- Test: `internal/git/status_parse_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/git/status_parse_test.go`:
```go
package git

import "testing"

func TestParseStatusV2(t *testing.T) {
	// Fields within an entry are space-separated; entries are NUL-terminated.
	entries := []string{
		"# branch.oid abc123",
		"# branch.head main",
		"# branch.upstream origin/main",
		"# branch.ab +2 -1",
		"1 M. N... 100644 100644 100644 hhh iii staged.go",
		"1 .M N... 100644 100644 100644 hhh iii unstaged.go",
		"u UU N... 100644 100644 100644 000000 h1 h2 h3 conflict.go",
		"? untracked.txt",
	}
	var data []byte
	for _, e := range entries {
		data = append(data, []byte(e)...)
		data = append(data, 0)
	}

	st, err := ParseStatusV2(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.Branch != "main" {
		t.Errorf("branch = %q, want main", st.Branch)
	}
	if st.Upstream != "origin/main" {
		t.Errorf("upstream = %q, want origin/main", st.Upstream)
	}
	if st.Ahead != 2 || st.Behind != 1 {
		t.Errorf("ahead/behind = %d/%d, want 2/1", st.Ahead, st.Behind)
	}
	c := st.Counts()
	if c.Staged != 1 || c.Unstaged != 1 || c.Conflicted != 1 || c.Untracked != 1 {
		t.Fatalf("counts = %+v, want staged1 unstaged1 conflicted1 untracked1", c)
	}
}

func TestParseStatusV2Rename(t *testing.T) {
	// A "2" entry encodes a rename; with -z the original path is the NEXT token.
	entries := []string{
		"# branch.head main",
		"2 R. N... 100644 100644 100644 hhh iii R100 new.go",
		"old.go", // original path follows immediately for the rename entry
	}
	var data []byte
	for _, e := range entries {
		data = append(data, []byte(e)...)
		data = append(data, 0)
	}
	st, err := ParseStatusV2(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(st.Files) != 1 {
		t.Fatalf("files = %d, want 1", len(st.Files))
	}
	f := st.Files[0]
	if f.Path != "new.go" || f.OrigPath != "old.go" {
		t.Fatalf("rename parse: path=%q orig=%q, want new.go/old.go", f.Path, f.OrigPath)
	}
	if f.Staged != 'R' {
		t.Fatalf("staged code = %q, want R", string(f.Staged))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run TestParseStatusV2`
Expected: FAIL — undefined `ParseStatusV2`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/git/status_parse.go`:
```go
// Package git contains thin git verb wrappers and pure output parsers.
package git

import (
	"strconv"
	"strings"

	"github.com/gigagit/gg/internal/model"
)

// ParseStatusV2 parses `git status --porcelain=v2 -z --branch` output.
func ParseStatusV2(data []byte) (model.WorkingTreeStatus, error) {
	var st model.WorkingTreeStatus
	tokens := splitNUL(data)
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok == "" {
			continue
		}
		switch tok[0] {
		case '#':
			parseBranchHeader(tok, &st)
		case '1':
			fields := strings.SplitN(tok, " ", 9)
			if len(fields) >= 9 {
				xy := fields[1]
				st.Files = append(st.Files, model.FileStatus{
					Path:     fields[8],
					Staged:   xy[0],
					Unstaged: xy[1],
					Kind:     model.KindTracked,
				})
			}
		case '2':
			// Rename/copy: original path is the next NUL-separated token.
			fields := strings.SplitN(tok, " ", 10)
			orig := ""
			if i+1 < len(tokens) {
				orig = tokens[i+1]
				i++
			}
			if len(fields) >= 10 {
				xy := fields[1]
				st.Files = append(st.Files, model.FileStatus{
					Path:     fields[9],
					OrigPath: orig,
					Staged:   xy[0],
					Unstaged: xy[1],
					Kind:     model.KindTracked,
				})
			}
		case 'u':
			fields := strings.Fields(tok)
			path := ""
			if len(fields) > 0 {
				path = fields[len(fields)-1]
			}
			st.Files = append(st.Files, model.FileStatus{Path: path, Kind: model.KindUnmerged})
		case '?':
			st.Files = append(st.Files, model.FileStatus{Path: strings.TrimSpace(tok[1:]), Kind: model.KindUntracked})
		case '!':
			st.Files = append(st.Files, model.FileStatus{Path: strings.TrimSpace(tok[1:]), Kind: model.KindIgnored})
		}
	}
	return st, nil
}

func parseBranchHeader(tok string, st *model.WorkingTreeStatus) {
	switch {
	case strings.HasPrefix(tok, "# branch.head "):
		st.Branch = strings.TrimPrefix(tok, "# branch.head ")
	case strings.HasPrefix(tok, "# branch.upstream "):
		st.Upstream = strings.TrimPrefix(tok, "# branch.upstream ")
	case strings.HasPrefix(tok, "# branch.ab "):
		parts := strings.Fields(strings.TrimPrefix(tok, "# branch.ab "))
		for _, p := range parts {
			if len(p) < 2 {
				continue
			}
			n, _ := strconv.Atoi(p[1:])
			if p[0] == '+' {
				st.Ahead = n
			} else if p[0] == '-' {
				st.Behind = n
			}
		}
	}
}

func splitNUL(data []byte) []string {
	parts := strings.Split(string(data), "\x00")
	// Trailing NUL produces an empty final element; drop it.
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/git/ -run TestParseStatusV2`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/git/status_parse.go internal/git/status_parse_test.go
git commit -m "feat: add porcelain v2 status parser"
```

---

## Task 9: Branch + worktree parsers

**Files:**
- Create: `internal/git/branch_parse.go`
- Create: `internal/git/worktree_parse.go`
- Test: `internal/git/branch_parse_test.go`
- Test: `internal/git/worktree_parse_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/git/branch_parse_test.go`:
```go
package git

import "testing"

func TestParseBranches(t *testing.T) {
	// Format: %(HEAD)\x00%(refname:short)\x00%(upstream:short)\x00%(objectname:short)\x00%(upstream:track)
	lines := "*\x00main\x00origin/main\x00abc1234\x00[ahead 2, behind 1]\n" +
		" \x00feature\x00\x00def5678\x00\n"
	got, err := ParseBranches([]byte(lines))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("branches = %d, want 2", len(got))
	}
	if !got[0].IsHead || got[0].Name != "main" || got[0].Upstream != "origin/main" {
		t.Errorf("branch0 = %+v", got[0])
	}
	if got[0].Ahead != 2 || got[0].Behind != 1 {
		t.Errorf("branch0 ahead/behind = %d/%d, want 2/1", got[0].Ahead, got[0].Behind)
	}
	if got[1].IsHead || got[1].Name != "feature" || got[1].Upstream != "" {
		t.Errorf("branch1 = %+v", got[1])
	}
}
```

Create `internal/git/worktree_parse_test.go`:
```go
package git

import "testing"

func TestParseWorktrees(t *testing.T) {
	out := "worktree /repo\nHEAD abc123\nbranch refs/heads/main\n\n" +
		"worktree /repo/wt-feature\nHEAD def456\nbranch refs/heads/feature\n\n" +
		"worktree /repo/wt-detached\nHEAD aaa111\ndetached\n\n"
	got, err := ParseWorktrees([]byte(out))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("worktrees = %d, want 3", len(got))
	}
	if got[0].Path != "/repo" || got[0].Branch != "main" {
		t.Errorf("wt0 = %+v", got[0])
	}
	if got[2].Branch != "" || !got[2].Detached {
		t.Errorf("wt2 should be detached: %+v", got[2])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/git/ -run 'TestParseBranches|TestParseWorktrees'`
Expected: FAIL — undefined `ParseBranches`, `ParseWorktrees`.

- [ ] **Step 3: Write minimal implementations**

Create `internal/git/branch_parse.go`:
```go
package git

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/gigagit/gg/internal/model"
)

var trackRe = regexp.MustCompile(`ahead (\d+)|behind (\d+)`)

// ParseBranches parses for-each-ref output formatted as:
//
//	%(HEAD)\x00%(refname:short)\x00%(upstream:short)\x00%(objectname:short)\x00%(upstream:track)
//
// one ref per line.
func ParseBranches(data []byte) ([]model.Branch, error) {
	var out []model.Branch
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\x00")
		if len(f) < 4 {
			continue
		}
		b := model.Branch{
			IsHead:   strings.TrimSpace(f[0]) == "*",
			Name:     f[1],
			Upstream: f[2],
			Hash:     f[3],
		}
		if len(f) >= 5 {
			for _, m := range trackRe.FindAllStringSubmatch(f[4], -1) {
				if m[1] != "" {
					b.Ahead, _ = strconv.Atoi(m[1])
				}
				if m[2] != "" {
					b.Behind, _ = strconv.Atoi(m[2])
				}
			}
		}
		out = append(out, b)
	}
	return out, nil
}
```

Create `internal/git/worktree_parse.go`:
```go
package git

import (
	"strings"

	"github.com/gigagit/gg/internal/model"
)

// ParseWorktrees parses `git worktree list --porcelain` output. Records are
// separated by blank lines; each record has worktree/HEAD/branch|detached|bare.
func ParseWorktrees(data []byte) ([]model.Worktree, error) {
	var out []model.Worktree
	var cur *model.Worktree
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur = &model.Worktree{Path: strings.TrimPrefix(line, "worktree ")}
		case cur == nil:
			// ignore stray lines
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			cur.Detached = true
		case line == "bare":
			cur.Bare = true
		}
	}
	flush()
	return out, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/git/`
Expected: PASS (all parser tests).

- [ ] **Step 5: Commit**

```bash
git add internal/git/branch_parse.go internal/git/worktree_parse.go internal/git/branch_parse_test.go internal/git/worktree_parse_test.go
git commit -m "feat: add branch and worktree parsers"
```

---

## Task 10: Repo verbs (open + status/branches/worktrees via runner)

**Files:**
- Create: `internal/git/repo.go`
- Test: `internal/git/repo_test.go`

This task wires parsers to the runner and is tested end-to-end against a **real throwaway git repo** built with a shared helper.

- [ ] **Step 1: Write the test helper**

Create `internal/git/repo_test.go`:
```go
package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

// newTestRepo creates a temp git repo with one commit and returns its path and
// a runner scoped to it.
func newTestRepo(t *testing.T) (string, gitexec.Runner) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-m", "initial")
	return dir, gitexec.NewExecRunner("git", dir, observ.NewRing(50))
}

func TestRepoStatusCleanThenDirty(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	st, err := repo.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Branch != "main" {
		t.Errorf("branch = %q, want main", st.Branch)
	}
	if c := st.Counts(); c.Staged+c.Unstaged+c.Untracked+c.Conflicted != 0 {
		t.Errorf("expected clean tree, got %+v", c)
	}

	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, _ = repo.Status(context.Background())
	if st.Counts().Untracked != 1 {
		t.Errorf("expected 1 untracked, got %+v", st.Counts())
	}
}

func TestRepoBranches(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	bs, err := repo.Branches(context.Background())
	if err != nil {
		t.Fatalf("branches: %v", err)
	}
	if len(bs) != 1 || bs[0].Name != "main" || !bs[0].IsHead {
		t.Fatalf("branches = %+v, want one head branch 'main'", bs)
	}
}

func TestRepoWorktrees(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	wts, err := repo.Worktrees(context.Background())
	if err != nil {
		t.Fatalf("worktrees: %v", err)
	}
	if len(wts) != 1 || wts[0].Branch != "main" {
		t.Fatalf("worktrees = %+v, want one on 'main'", wts)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run TestRepo`
Expected: FAIL — undefined `Repo`, `Status`, `Branches`, `Worktrees`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/git/repo.go`:
```go
package git

import (
	"context"

	"github.com/gigagit/gg/internal/gitcmd"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
)

// Repo provides read-only git operations through a Runner.
type Repo struct {
	Runner gitexec.Runner
}

// Status returns the working-tree status.
func (r *Repo) Status(ctx context.Context) (model.WorkingTreeStatus, error) {
	argv := gitcmd.New("status").Arg("--porcelain=v2", "-z", "--branch").ToArgv()
	res, err := r.Runner.Run(ctx, "git status", argv)
	if err != nil {
		return model.WorkingTreeStatus{}, err
	}
	return ParseStatusV2([]byte(res.Stdout))
}

// Branches returns local branches.
func (r *Repo) Branches(ctx context.Context) ([]model.Branch, error) {
	const format = "%(HEAD)%00%(refname:short)%00%(upstream:short)%00%(objectname:short)%00%(upstream:track)"
	argv := gitcmd.New("for-each-ref").Arg("--format="+format, "refs/heads").ToArgv()
	res, err := r.Runner.Run(ctx, "git for-each-ref", argv)
	if err != nil {
		return nil, err
	}
	return ParseBranches([]byte(res.Stdout))
}

// Worktrees returns the repository's worktrees.
func (r *Repo) Worktrees(ctx context.Context) ([]model.Worktree, error) {
	argv := gitcmd.New("worktree").Arg("list", "--porcelain").ToArgv()
	res, err := r.Runner.Run(ctx, "git worktree list", argv)
	if err != nil {
		return nil, err
	}
	return ParseWorktrees([]byte(res.Stdout))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/git/`
Expected: PASS (parser + repo integration tests).

- [ ] **Step 5: Commit**

```bash
git add internal/git/repo.go internal/git/repo_test.go
git commit -m "feat: add repo verbs for status/branches/worktrees"
```

---

## Task 11: Debug dump assembly + serialization

**Files:**
- Create: `internal/observ/dump.go`
- Test: `internal/observ/dump_test.go`

The `Dump` type and `WriteDump` live in `observ` (serialization + safety). The struct uses only primitive fields and `[]Span`, so `observ` stays a leaf. Higher layers fill it in (Task 12).

- [ ] **Step 1: Write the failing test**

Create `internal/observ/dump_test.go`:
```go
package observ

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteDumpRedactsAndOmitsContent(t *testing.T) {
	d := Dump{
		GeneratedAt: time.Now(),
		GGVersion:   "9.9.9",
		GitVersion:  "git version 2.45.0",
		Repo: RepoInfo{
			WorktreePath: "/repo",
			Branch:       "main",
			Head:         "abc123",
		},
		WorkingTree: DumpCounts{Untracked: 2},
		Recent: []Span{
			{Name: "git push", Args: []string{"push", "https://u:secrettoken@h/r.git"}},
		},
		Errors: []string{"boom"},
	}
	path := filepath.Join(t.TempDir(), "dump.json")
	if err := WriteDump(path, d); err != nil {
		t.Fatalf("WriteDump: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secrettoken") {
		t.Fatal("dump leaked a secret token")
	}
	// Must be valid JSON.
	var back Dump
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("dump is not valid JSON: %v", err)
	}
	if back.GGVersion != "9.9.9" {
		t.Fatalf("round-trip lost version: %+v", back)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/observ/ -run TestWriteDump`
Expected: FAIL — undefined `Dump`, `WriteDump`, `RepoInfo`, `DumpCounts`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/observ/dump.go`:
```go
package observ

import (
	"encoding/json"
	"os"
	"time"
)

// RepoInfo captures non-secret repository identity for a debug dump.
type RepoInfo struct {
	WorktreePath string `json:"worktree_path"`
	GitDir       string `json:"git_dir,omitempty"`
	Branch       string `json:"branch"`
	Upstream     string `json:"upstream,omitempty"`
	Head         string `json:"head"`
	Sparse       bool   `json:"sparse"`
	PartialClone bool   `json:"partial_clone"`
}

// DumpCounts mirrors model.Counts without importing model (keeps observ a leaf).
type DumpCounts struct {
	Staged     int `json:"staged"`
	Unstaged   int `json:"unstaged"`
	Untracked  int `json:"untracked"`
	Conflicted int `json:"conflicted"`
}

// Dump is the serialisable diagnostic snapshot. It deliberately contains NO
// file contents or diffs — only counts, identity, and the recent-span buffer.
type Dump struct {
	GeneratedAt time.Time  `json:"generated_at"`
	GGVersion   string     `json:"gg_version"`
	GitVersion  string     `json:"git_version"`
	OS          string     `json:"os,omitempty"`
	Arch        string     `json:"arch,omitempty"`
	Repo        RepoInfo   `json:"repo"`
	WorkingTree DumpCounts `json:"working_tree"`
	Recent      []Span     `json:"recent"`
	Errors      []string   `json:"errors,omitempty"`
}

// WriteDump redacts span args and writes the dump as indented JSON to path.
func WriteDump(path string, d Dump) error {
	for i := range d.Recent {
		d.Recent[i].Args = Redact(d.Recent[i].Args)
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/observ/`
Expected: PASS (all observ tests).

- [ ] **Step 5: Commit**

```bash
git add internal/observ/dump.go internal/observ/dump_test.go
git commit -m "feat: add debug dump struct and safe serialization"
```

---

## Task 12: Trace logger (opt-in JSON-lines)

**Files:**
- Create: `internal/observ/trace.go`
- Test: `internal/observ/trace_test.go`

A `TraceRecorder` wraps another `Recorder` (the ring) and, when enabled, also appends each span as a JSON line to a writer. Enablement is decided by the caller (env/flag) in Task 13.

- [ ] **Step 1: Write the failing test**

Create `internal/observ/trace_test.go`:
```go
package observ

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestTraceRecorderWritesJSONLinesAndForwards(t *testing.T) {
	var buf bytes.Buffer
	ring := NewRing(10)
	tr := NewTraceRecorder(ring, &buf)

	tr.Record(Span{Name: "git status", Duration: 5 * time.Millisecond})

	// Forwarded to the underlying ring.
	if len(ring.Snapshot()) != 1 {
		t.Fatalf("expected span forwarded to ring")
	}
	// One JSON line written.
	line := bytes.TrimSpace(buf.Bytes())
	var s Span
	if err := json.Unmarshal(line, &s); err != nil {
		t.Fatalf("trace line not valid JSON: %v (%q)", err, line)
	}
	if s.Name != "git status" {
		t.Fatalf("trace line name = %q, want 'git status'", s.Name)
	}
}

func TestTraceRecorderNilWriterStillForwards(t *testing.T) {
	ring := NewRing(10)
	tr := NewTraceRecorder(ring, nil)
	tr.Record(Span{Name: "x"})
	if len(ring.Snapshot()) != 1 {
		t.Fatal("nil-writer trace recorder must still forward to ring")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/observ/ -run TestTraceRecorder`
Expected: FAIL — undefined `NewTraceRecorder`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/observ/trace.go`:
```go
package observ

import (
	"encoding/json"
	"io"
	"sync"
)

// TraceRecorder forwards spans to an inner Recorder and, when w is non-nil,
// also appends each span as a JSON line to w. Use it to enable verbose tracing
// while keeping the always-on ring buffer populated.
type TraceRecorder struct {
	inner Recorder
	mu    sync.Mutex
	w     io.Writer
}

// NewTraceRecorder wraps inner; if w is nil, only forwarding occurs.
func NewTraceRecorder(inner Recorder, w io.Writer) *TraceRecorder {
	return &TraceRecorder{inner: inner, w: w}
}

func (t *TraceRecorder) Record(s Span) {
	if t.inner != nil {
		t.inner.Record(s)
	}
	if t.w == nil {
		return
	}
	s.Args = Redact(s.Args)
	data, err := json.Marshal(s)
	if err != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_, _ = t.w.Write(append(data, '\n'))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/observ/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/observ/trace.go internal/observ/trace_test.go
git commit -m "feat: add opt-in JSON-lines trace recorder"
```

---

## Task 13: Wire `gg inspect` command (+ `--debug-dump`, `--trace`)

**Files:**
- Create: `internal/app/inspect.go`
- Modify: `cmd/gg/main.go` (replace body)
- Test: `internal/app/inspect_test.go`

This assembles the foundation into a runnable command: it opens the repo, prints status/branch/worktree summary, and supports a debug dump and verbose trace. The dump assembler lives here (the layer that has both repo access and the ring).

- [ ] **Step 1: Write the failing test**

Create `internal/app/inspect_test.go`:
```go
package app

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "initial")
	return dir
}

func TestInspectPrintsSummary(t *testing.T) {
	dir := newRepo(t)
	var out bytes.Buffer
	opts := Options{WorkDir: dir, Stdout: &out}
	if err := Inspect(context.Background(), opts); err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "branch: main") {
		t.Fatalf("output missing branch line:\n%s", s)
	}
	if !strings.Contains(s, "worktrees: 1") {
		t.Fatalf("output missing worktree count:\n%s", s)
	}
}

func TestInspectWritesDebugDump(t *testing.T) {
	dir := newRepo(t)
	dumpPath := filepath.Join(t.TempDir(), "dump.json")
	var out bytes.Buffer
	opts := Options{WorkDir: dir, Stdout: &out, DumpPath: dumpPath}
	if err := Inspect(context.Background(), opts); err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if _, err := os.Stat(dumpPath); err != nil {
		t.Fatalf("debug dump not written: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/`
Expected: FAIL — undefined `Options`, `Inspect`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/app/inspect.go`:
```go
// Package app wires the foundation layers into runnable commands. The `inspect`
// command is a temporary M1 surface; the TUI (Plan 3) will replace it.
package app

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"

	"github.com/gigagit/gg/internal/buildinfo"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitcmd"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

// Options configures an Inspect run.
type Options struct {
	WorkDir  string
	Stdout   io.Writer
	DumpPath string    // if non-empty, write a debug dump here
	Trace    io.Writer // if non-nil, enable verbose JSON-lines tracing
}

// Inspect opens the repo, prints a summary, and optionally writes a debug dump.
func Inspect(ctx context.Context, opts Options) error {
	ring := observ.NewRing(200)
	var rec observ.Recorder = ring
	if opts.Trace != nil {
		rec = observ.NewTraceRecorder(ring, opts.Trace)
	}
	runner := gitexec.NewExecRunner("git", opts.WorkDir, rec)
	repo := &git.Repo{Runner: runner}

	var errs []string
	st, err := repo.Status(ctx)
	if err != nil {
		errs = append(errs, err.Error())
	}
	branches, err := repo.Branches(ctx)
	if err != nil {
		errs = append(errs, err.Error())
	}
	wts, err := repo.Worktrees(ctx)
	if err != nil {
		errs = append(errs, err.Error())
	}

	c := st.Counts()
	fmt.Fprintf(opts.Stdout, "%s\n", buildinfo.String())
	fmt.Fprintf(opts.Stdout, "branch: %s", st.Branch)
	if st.Upstream != "" {
		fmt.Fprintf(opts.Stdout, " (upstream %s, ahead %d, behind %d)", st.Upstream, st.Ahead, st.Behind)
	}
	fmt.Fprintln(opts.Stdout)
	fmt.Fprintf(opts.Stdout, "changes: staged %d, unstaged %d, untracked %d, conflicted %d\n",
		c.Staged, c.Unstaged, c.Untracked, c.Conflicted)
	fmt.Fprintf(opts.Stdout, "branches: %d\n", len(branches))
	fmt.Fprintf(opts.Stdout, "worktrees: %d\n", len(wts))

	if opts.DumpPath != "" {
		if derr := writeDump(ctx, opts.DumpPath, runner, ring, st, errs); derr != nil {
			return fmt.Errorf("write debug dump: %w", derr)
		}
		fmt.Fprintf(opts.Stdout, "debug dump written: %s\n", opts.DumpPath)
	}
	return nil
}

func writeDump(ctx context.Context, path string, runner gitexec.Runner, ring *observ.Ring, st gitStatus, errs []string) error {
	gitVer := ""
	if res, err := runner.Run(ctx, "git version", gitcmd.New("version").ToArgv()); err == nil {
		gitVer = strings.TrimSpace(res.Stdout)
	}
	c := st.Counts()
	d := observ.Dump{
		GeneratedAt: time.Now(),
		GGVersion:   buildinfo.Version,
		GitVersion:  gitVer,
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		Repo: observ.RepoInfo{
			Branch:   st.Branch,
			Upstream: st.Upstream,
		},
		WorkingTree: observ.DumpCounts{
			Staged: c.Staged, Unstaged: c.Unstaged,
			Untracked: c.Untracked, Conflicted: c.Conflicted,
		},
		Recent: ring.Snapshot(),
		Errors: errs,
	}
	return observ.WriteDump(path, d)
}
```

Note: `gitStatus` is a local alias to keep the helper signature short. Add at the top of the file, after imports:
```go
type gitStatus = model.WorkingTreeStatus
```
and add `"github.com/gigagit/gg/internal/model"` to the import block.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/`
Expected: PASS (2 tests).

- [ ] **Step 5: Replace main.go to expose the command**

Replace the body of `cmd/gg/main.go`:
```go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/gigagit/gg/internal/app"
)

func main() {
	var (
		dumpPath = flag.String("debug-dump", "", "write a debug dump JSON file to this path")
		trace    = flag.Bool("trace", false, "enable verbose timing trace to stderr")
	)
	flag.Parse()

	opts := app.Options{WorkDir: ".", Stdout: os.Stdout, DumpPath: *dumpPath}
	if *trace || os.Getenv("GG_TRACE") == "1" {
		opts.Trace = os.Stderr
	}
	if err := app.Inspect(context.Background(), opts); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 6: Verify the whole module builds and tests pass**

Run: `go build ./... && go test ./...`
Expected: build succeeds; all tests PASS.

- [ ] **Step 7: Manual smoke test**

Run:
```bash
go run ./cmd/gg --debug-dump /tmp/gg-dump.json
cat /tmp/gg-dump.json | head -20
GG_TRACE=1 go run ./cmd/gg 2>&1 | grep -i 'git status' | head -1
```
Expected: prints branch/changes/branches/worktrees summary for the gigagit repo itself; dump file is valid JSON with no secrets; trace mode emits a JSON line for the status span.

- [ ] **Step 8: Commit**

```bash
git add internal/app cmd/gg/main.go
git commit -m "feat: wire gg inspect command with debug dump and trace"
```

---

## Self-Review

**Spec coverage (against `2026-06-11-gigagit-m1-design.md`):**
- §3 module layout (`gitexec`, `gitcmd`, `git`, `model`, `observ`) → Tasks 2–11. (`engine`, `tui` are Plans 2–3.)
- §3.1 runner-with-fake, command builder, loader/command split → Tasks 4, 5, 6, 8–10.
- §4 contract `Timing{Span}` substrate → Span + Recorder (Tasks 2, 6). Full `Event`/`Operation`/`Decider` is Plan 2.
- §6 non-blocking/cancellation → context-aware runner (Task 6). Sparse/fsmonitor status flags refined in Plan 2 when the engine drives status live.
- §9.1 always-on ring buffer + spans → Tasks 2, 6.
- §9.2 opt-in tracing → Task 12, wired in Task 13.
- §9.3 debug dump (counts only, redaction, no diffs) → Tasks 11, 13.
- §10 testing: real throwaway repos + table-driven parsers → Tasks 8–10, 13.

**Out of scope here (later plans):** smart pull/switch/commit/push/stash, undo, TUI, panic-triggered dump (added in Plan 3 when there is a long-lived process), CLI subcommand for dump (M2).

**Placeholder scan:** none — every code step contains complete, compilable code.

**Type consistency:** `Runner.Run/Stream(ctx, name, argv)` consistent across Tasks 5/6/10/13; `Span`/`Recorder`/`Ring` consistent across Tasks 2/6/11/12; `model.WorkingTreeStatus.Counts()` consistent across Tasks 7/8/10/13; `observ.Dump`/`DumpCounts`/`RepoInfo` consistent across Tasks 11/13.

---

## Plan sequence (M1)

1. **Plan 1 — Foundation & read-only inspection** (this document).
2. **Plan 2 — Engine & smart operations:** `Operation`/`Decider`/`Event`/`Policy`, smart pull (§5 tree), smart switch, commit, push, stash, ref-only undo; headless tests via fixtures.
3. **Plan 3 — TUI:** Bubble Tea panels/update/presentation, modal `Decider`, keybindings, debug-dump key, panic-triggered dump.
