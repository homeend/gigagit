# Time-Track Performance Log Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A global `--time-track <path>` flag that appends one redacted JSON span per process start, git subprocess, and engine operation to a log file, for the TUI and every CLI subcommand.

**Architecture:** A package-level span sink in `internal/observ` (`SetSpanSink`/`EmitSpan`, nil = disabled — the project's seam pattern); `Ring.Record` mirrors every git span to it, so all four existing recorder construction sites stay untouched. Frontends wrap `op.Run` in synthetic `op <Type>` spans named via a new `engine.OpName`. `cmd/gg/main.go` extracts the flag (the `--cwd-file` precedent), opens the file in append mode, and emits a run-delimiting `gg start` span.

**Tech Stack:** Go 1.26 stdlib only. Spec: `docs/superpowers/specs/2026-06-12-time-track-design.md`.

**Working branch:** `feat/time-track` off `main`.

**Quality gates per task:** `go test ./...`, `go vet ./...`, `gofmt -l internal cmd` (empty output). `go test ./... -race` once at the end. Commit messages end with `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

**Hermeticity rule for every test that sets the sink:** the sink is process-wide state — always register `t.Cleanup(func() { observ.SetSpanSink(nil) })` (or the package-local equivalent) immediately after setting it, or parallel tests will cross-contaminate.

---

### Task 1: observ — span sink (`SetSpanSink`, `EmitSpan`, ring mirroring)

**Files:**
- Create: `internal/observ/sink.go`
- Create: `internal/observ/sink_test.go`
- Modify: `internal/observ/ring.go` (`Record`, lines 41-52)
- Modify: `internal/observ/trace.go` (`Record` reuses the shared write helper)

- [ ] **Step 1: Write the failing tests**

Create `internal/observ/sink_test.go`:

```go
package observ

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRingRecordMirrorsToSink(t *testing.T) {
	var buf bytes.Buffer
	SetSpanSink(&buf)
	t.Cleanup(func() { SetSpanSink(nil) })

	r := NewRing(4)
	r.Record(Span{Name: "git status", Args: []string{"status", "--porcelain"}, Start: time.Now(), Duration: time.Millisecond})

	line := strings.TrimSpace(buf.String())
	var got Span
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("sink line is not valid JSON: %v\n%s", err, line)
	}
	if got.ID != 1 {
		t.Errorf("ID = %d, want the ring-assigned 1", got.ID)
	}
	if got.Name != "git status" {
		t.Errorf("Name = %q, want git status", got.Name)
	}
	// The ring itself still retains the span (mirroring, not rerouting).
	if snap := r.Snapshot(); len(snap) != 1 {
		t.Fatalf("ring snapshot = %d spans, want 1", len(snap))
	}
}

func TestEmitSpanWritesToSink(t *testing.T) {
	var buf bytes.Buffer
	SetSpanSink(&buf)
	t.Cleanup(func() { SetSpanSink(nil) })

	EmitSpan(Span{Name: "op SmartPull", Duration: 2 * time.Second})
	var got Span
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.Name != "op SmartPull" || got.Duration != 2*time.Second {
		t.Errorf("got %+v", got)
	}
}

func TestSinkRedactsArgs(t *testing.T) {
	var buf bytes.Buffer
	SetSpanSink(&buf)
	t.Cleanup(func() { SetSpanSink(nil) })

	EmitSpan(Span{Name: "gg start", Args: []string{"pull", "https://user:secret@host/repo"}})
	if strings.Contains(buf.String(), "secret") {
		t.Fatalf("sink line leaked a credential: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "<redacted>") {
		t.Fatalf("expected redaction marker in: %s", buf.String())
	}
}

func TestEmitSpanWithoutSinkIsNoOp(t *testing.T) {
	SetSpanSink(nil)
	EmitSpan(Span{Name: "op X"}) // must not panic, must not block
}

func TestSinkConcurrentWriters(t *testing.T) {
	var buf bytes.Buffer
	SetSpanSink(&buf)
	t.Cleanup(func() { SetSpanSink(nil) })

	r := NewRing(8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Record(Span{Name: "git x"})
			EmitSpan(Span{Name: "op y"})
		}()
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 16 {
		t.Fatalf("lines = %d, want 16 (8 ring + 8 emit)", len(lines))
	}
	for i, ln := range lines {
		if !json.Valid([]byte(ln)) {
			t.Fatalf("line %d is not valid JSON (interleaved write?): %q", i, ln)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/observ/ -run 'TestRingRecordMirrors|TestEmitSpan|TestSink'`
Expected: COMPILE FAIL — `undefined: SetSpanSink`.

- [ ] **Step 3: Implement the sink**

Create `internal/observ/sink.go`:

```go
package observ

import (
	"encoding/json"
	"io"
	"sync"
)

// The process-wide span sink. nil (the default) disables mirroring entirely —
// the cli.RepoStatePath seam pattern: wired only by cmd/gg, so tests and
// library consumers see no side effects unless they opt in.
var (
	sinkMu sync.Mutex
	sink   io.Writer
)

// SetSpanSink routes a copy of every recorded span (ring-recorded and
// EmitSpan-emitted alike) to w as redacted JSON lines. nil disables. Call once
// at startup, before recorders run on other goroutines.
func SetSpanSink(w io.Writer) {
	sinkMu.Lock()
	defer sinkMu.Unlock()
	sink = w
}

// EmitSpan writes a synthetic span (process start, an engine operation) to the
// sink. It does not enter any ring. No-op when no sink is set.
func EmitSpan(s Span) { sinkWrite(s) }

// sinkWrite appends s to the sink as one JSON line. The single package mutex
// serializes all writers — every ring and every EmitSpan caller shares it.
func sinkWrite(s Span) {
	sinkMu.Lock()
	defer sinkMu.Unlock()
	if sink == nil {
		return
	}
	writeSpanLine(sink, s)
}

// writeSpanLine is the shared redact+marshal+write step used by the sink and
// TraceRecorder. Callers hold their own locks.
func writeSpanLine(w io.Writer, s Span) {
	s.Args = Redact(s.Args)
	data, err := json.Marshal(s)
	if err != nil {
		return
	}
	_, _ = w.Write(append(data, '\n'))
}
```

- [ ] **Step 4: Mirror from the ring and dedupe TraceRecorder**

In `internal/observ/ring.go`, replace `Record`:

```go
// Record stores a span, assigning it a monotonic ID and evicting the oldest
// span when capacity is exceeded. A copy of the span (with its assigned ID)
// is mirrored to the process-wide span sink when one is set.
func (r *Ring) Record(s Span) {
	r.mu.Lock()
	r.nextID++
	s.ID = r.nextID
	r.buf = append(r.buf, s)
	if len(r.buf) > r.cap {
		r.buf = r.buf[len(r.buf)-r.cap:]
	}
	r.mu.Unlock() // sink I/O must not run under the ring lock
	sinkWrite(s)
}
```

In `internal/observ/trace.go`, replace the tail of `Record` so the write step
is shared (behavior identical):

```go
func (t *TraceRecorder) Record(s Span) {
	if t.inner != nil {
		t.inner.Record(s)
	}
	if t.w == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	writeSpanLine(t.w, s)
}
```

(`encoding/json` may become unused in trace.go — remove the import if the
compiler says so.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/observ/ -race`
Expected: PASS — the 5 new tests plus all existing ring/trace/redact/dump tests (TraceRecorder behavior is unchanged).

- [ ] **Step 6: Gate + commit**

```bash
go test ./... && go vet ./... && gofmt -l internal cmd
git add internal/observ/sink.go internal/observ/sink_test.go internal/observ/ring.go internal/observ/trace.go
git commit -m "feat(observ): process-wide span sink with ring mirroring

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: engine — `OpName`

**Files:**
- Create: `internal/engine/opname.go`
- Create: `internal/engine/opname_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/engine/opname_test.go`:

```go
package engine

import "testing"

func TestOpNameStripsPackagePrefix(t *testing.T) {
	cases := []struct {
		op   Operation
		want string
	}{
		{SmartPull{}, "SmartPull"},
		{Commit{}, "Commit"},
		{CreateWorktreeForBranch{}, "CreateWorktreeForBranch"},
		{DeleteBranch{}, "DeleteBranch"},
	}
	for _, c := range cases {
		if got := OpName(c.op); got != c.want {
			t.Errorf("OpName(%T) = %q, want %q", c.op, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestOpName`
Expected: COMPILE FAIL — `undefined: OpName`.

- [ ] **Step 3: Implement**

Create `internal/engine/opname.go`:

```go
package engine

import (
	"fmt"
	"strings"
)

// OpName returns a stable, human-readable name for an operation (its type
// name without the package prefix, e.g. "SmartPull"). Frontends use it to
// label timing spans.
func OpName(op Operation) string {
	name := fmt.Sprintf("%T", op)
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return name
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ -run TestOpName`
Expected: PASS.

- [ ] **Step 5: Gate + commit**

```bash
go test ./... && go vet ./... && gofmt -l internal cmd
git add internal/engine/opname.go internal/engine/opname_test.go
git commit -m "feat(engine): OpName for span labeling

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: frontends — `op <Type>` spans around every operation

**Files:**
- Modify: `internal/cli/core.go` (`runOperation`, lines ~71-94)
- Modify: `internal/tui/op.go` (`startOp`'s op goroutine, lines ~53-65)
- Create: `internal/cli/timetrack_test.go`
- Create: `internal/tui/timetrack_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/timetrack_test.go` (helpers `newRepoDir(t) string` exists in core_test.go):

```go
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/observ"
)

// sinkLines parses every JSON span line the sink captured.
func sinkLines(t *testing.T, buf *bytes.Buffer) []observ.Span {
	t.Helper()
	var out []observ.Span
	for _, ln := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if ln == "" {
			continue
		}
		var s observ.Span
		if err := json.Unmarshal([]byte(ln), &s); err != nil {
			t.Fatalf("bad span line %q: %v", ln, err)
		}
		out = append(out, s)
	}
	return out
}

func TestRunOperationEmitsOpSpan(t *testing.T) {
	var buf bytes.Buffer
	observ.SetSpanSink(&buf)
	t.Cleanup(func() { observ.SetSpanSink(nil) })

	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	repo := openRepo(dir)

	var prog bytes.Buffer
	if _, err := runOperation(context.Background(), repo, engine.Commit{Message: "x", All: true}, cliDecider{}, &prog); err != nil {
		t.Fatalf("runOperation: %v", err)
	}

	spans := sinkLines(t, &buf)
	var opSpan, gitSpan bool
	for _, s := range spans {
		if s.Name == "op Commit" && s.ExitCode == 0 && s.Duration > 0 {
			opSpan = true
		}
		if strings.HasPrefix(s.Name, "git commit") {
			gitSpan = true
		}
	}
	if !opSpan {
		t.Errorf("missing successful 'op Commit' span in %+v", spans)
	}
	if !gitSpan {
		t.Errorf("missing mirrored git subprocess span in %+v", spans)
	}
}

func TestRunOperationFailureSpanCarriesError(t *testing.T) {
	var buf bytes.Buffer
	observ.SetSpanSink(&buf)
	t.Cleanup(func() { observ.SetSpanSink(nil) })

	dir := newRepoDir(t) // clean tree: commit -a has nothing to commit -> error
	repo := openRepo(dir)
	var prog bytes.Buffer
	if _, err := runOperation(context.Background(), repo, engine.Commit{Message: "x", All: true}, cliDecider{}, &prog); err == nil {
		t.Fatal("expected the empty commit to fail")
	}

	for _, s := range sinkLines(t, &buf) {
		if s.Name == "op Commit" {
			if s.ExitCode != 1 || s.Err == "" {
				t.Fatalf("failure span = %+v, want ExitCode 1 with Err set", s)
			}
			return
		}
	}
	t.Fatal("no 'op Commit' span found")
}
```

Create `internal/tui/timetrack_test.go` (helpers `newRepoDir`, `runGit`, `loadModel`, `driveOp`, `keyMsg` exist in the tui test package):

```go
package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/observ"
)

// TestStartOpEmitsOpSpan drives a real op through the TUI plumbing and
// asserts the sink got an "op …" span (emitted from the op goroutine — the
// -race gate covers the concurrency).
func TestStartOpEmitsOpSpan(t *testing.T) {
	var buf bytes.Buffer
	observ.SetSpanSink(&buf)
	t.Cleanup(func() { observ.SetSpanSink(nil) })

	dir, repo := newRepoDir(t)
	runGit(t, dir, "branch", "feat/spanned")
	m := loadModel(t, repo)
	m.focus = panelBranches
	for vi := 0; vi < m.panelLen(panelBranches); vi++ {
		m.sel[panelBranches] = vi
		if bi, ok := m.backingIndex(panelBranches); ok && m.branches[bi].Name == "feat/spanned" {
			break
		}
	}

	// d on the Branches panel -> DeleteBranch op; answer the confirm modal.
	updated, cmd := m.Update(keyMsg("d"))
	m = updated.(Model)
	for i := 0; i < 100 && m.running; i++ {
		if m.modal != nil {
			u, _ := m.Update(keyMsg("enter")) // option 0 = "delete"
			m = u.(Model)
			continue
		}
		if cmd == nil {
			t.Fatal("no command but op still running")
		}
		u, next := m.Update(cmd())
		m = u.(Model)
		cmd = next
	}

	if !strings.Contains(buf.String(), `"name":"op DeleteBranch"`) {
		t.Fatalf("sink missing the op span:\n%s", buf.String())
	}
}
```

(If the JSON field order makes the literal `"name":"op DeleteBranch"` brittle,
parse the lines instead — `encoding/json` marshals struct fields in
declaration order, so the literal is stable; keep it simple.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run TestRunOperation && go test ./internal/tui/ -run TestStartOpEmits`
Expected: FAIL — no `op …` spans are emitted yet (the git span assertions alone would pass; the op-span assertions fail).

- [ ] **Step 3: Wrap the CLI runner**

In `internal/cli/core.go`, add `"time"` and `"github.com/gigagit/gg/internal/observ"` to the imports (observ is already imported), and change `runOperation` to time the run:

```go
func runOperation(ctx context.Context, repo *git.Repo, op engine.Operation, dec engine.Decider, progress io.Writer) (engine.Result, error) {
	opStart := time.Now()
	events := make(chan engine.Event, 32)
	var (
		res engine.Result
		err error
	)
	done := make(chan struct{})
	go func() {
		res, err = op.Run(ctx, engine.OpDeps{Repo: repo, Events: events, Decider: dec})
		close(events)
		close(done)
	}()
	for e := range events {
		if p, ok := e.(engine.Progress); ok {
			if p.Detail != "" {
				fmt.Fprintf(progress, "→ %s: %s\n", p.Step, p.Detail)
			} else {
				fmt.Fprintf(progress, "→ %s\n", p.Step)
			}
		}
	}
	<-done
	span := observ.Span{Name: "op " + engine.OpName(op), Start: opStart, Duration: time.Since(opStart)}
	if err != nil {
		span.ExitCode = 1
		span.Err = err.Error()
	}
	observ.EmitSpan(span)
	return res, err
}
```

- [ ] **Step 4: Wrap the TUI runner**

In `internal/tui/op.go`, add `"time"` and `"github.com/gigagit/gg/internal/observ"` to the imports, and change `startOp`'s first goroutine:

```go
	go func() {
		opStart := time.Now()
		res, err := op.Run(context.Background(), engine.OpDeps{
			Repo:    repo,
			Events:  events,
			Decider: uiDecider{msgs: msgs},
		})
		span := observ.Span{Name: "op " + engine.OpName(op), Start: opStart, Duration: time.Since(opStart)}
		if err != nil {
			span.ExitCode = 1
			span.Err = err.Error()
		}
		observ.EmitSpan(span)
		close(events)
		msgs <- opFinishedMsg{res: res, err: err}
	}()
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/ ./internal/tui/ -race`
Expected: PASS — the 3 new tests plus everything existing.

- [ ] **Step 6: Gate + commit**

```bash
go test ./... && go vet ./... && gofmt -l internal cmd
git add internal/cli/core.go internal/cli/timetrack_test.go internal/tui/op.go internal/tui/timetrack_test.go
git commit -m "feat(cli,tui): emit op spans around every engine operation

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: cmd/gg — `--time-track` flag, append-mode file, `gg start` span

**Files:**
- Modify: `cmd/gg/main.go` (flag extraction + setup; imports gain `buildinfo`)
- Create: `cmd/gg/timetrack_test.go`

- [ ] **Step 1: Write the failing tests**

Create `cmd/gg/timetrack_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/observ"
)

func TestExtractTimeTrack(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantPath string
		wantRest []string
	}{
		{"absent", []string{"status"}, "", []string{"status"}},
		{"space form", []string{"--time-track", "/tmp/x.log", "status"}, "/tmp/x.log", []string{"status"}},
		{"equals form", []string{"--time-track=/tmp/y.log"}, "/tmp/y.log", []string{}},
		{"after subcommand", []string{"pull", "--time-track", "/tmp/a.log"}, "/tmp/a.log", []string{"pull"}},
		{"no value is dropped safely", []string{"--time-track"}, "", []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path, rest := extractTimeTrack(tc.args)
			if path != tc.wantPath {
				t.Errorf("path = %q, want %q", path, tc.wantPath)
			}
			if !reflect.DeepEqual(rest, tc.wantRest) {
				t.Errorf("rest = %v, want %v", rest, tc.wantRest)
			}
		})
	}
}

func TestSetupTimeTrackAppendsAcrossRuns(t *testing.T) {
	t.Cleanup(func() { observ.SetSpanSink(nil) })
	logPath := filepath.Join(t.TempDir(), "perf.log")

	if err := setupTimeTrack(logPath, []string{"status"}); err != nil {
		t.Fatalf("first setup: %v", err)
	}
	if err := setupTimeTrack(logPath, []string{"pull"}); err != nil {
		t.Fatalf("second setup: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), `"name":"gg start"`); got != 2 {
		t.Fatalf("gg start lines = %d, want 2 (append, not truncate):\n%s", got, data)
	}
	if !strings.Contains(string(data), "version=") {
		t.Fatalf("start span missing the version element:\n%s", data)
	}
}

func TestSetupTimeTrackBadPathErrors(t *testing.T) {
	t.Cleanup(func() { observ.SetSpanSink(nil) })
	bad := filepath.Join(t.TempDir(), "no-such-dir", "perf.log")
	if err := setupTimeTrack(bad, nil); err == nil {
		t.Fatal("opening a path in a missing directory must error")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/gg/ -run 'TestExtractTimeTrack|TestSetupTimeTrack'`
Expected: COMPILE FAIL — `undefined: extractTimeTrack`.

- [ ] **Step 3: Implement extraction + setup**

In `cmd/gg/main.go`:

**3a.** Add `"github.com/gigagit/gg/internal/buildinfo"` to the imports.

**3b.** After the `cwdFile, args := extractCwdFile(os.Args[1:])` line, insert:

```go
	timeTrack, args := extractTimeTrack(args)
	if timeTrack != "" {
		if err := setupTimeTrack(timeTrack, args); err != nil {
			fmt.Fprintln(os.Stderr, "gg: --time-track:", err)
			os.Exit(2)
		}
	}
```

(Note this shadows nothing: `args` is reassigned, keeping the later
shell-init/inspect/CLI routing unchanged.)

**3c.** Append next to `extractCwdFile`:

```go
// extractTimeTrack pulls the global --time-track flag (in either
// "--time-track path" or "--time-track=path" form) out of args, returning its
// value and the remaining args. A trailing "--time-track" with no value is
// dropped.
func extractTimeTrack(args []string) (string, []string) {
	path := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--time-track":
			if i+1 < len(args) {
				path = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--time-track="):
			path = strings.TrimPrefix(a, "--time-track=")
		default:
			rest = append(rest, a)
		}
	}
	return path, rest
}

// setupTimeTrack opens path for appending (creating it if missing), routes
// every span there, and emits the run-delimiting "gg start" span carrying the
// (redacted) argv and the build version. The file is never explicitly closed:
// each span is one unbuffered line, and process exit releases the handle.
func setupTimeTrack(path string, argv []string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	observ.SetSpanSink(f)
	observ.EmitSpan(observ.Span{
		Name:  "gg start",
		Args:  append(append([]string{}, argv...), "version="+buildinfo.Version),
		Start: time.Now(),
	})
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/gg/`
Expected: PASS (3 new tests + the existing cwd-file table test).

- [ ] **Step 5: Manual smoke**

```bash
go build -o ./gg ./cmd/gg
cd /tmp && rm -rf tt-smoke && mkdir tt-smoke && cd tt-smoke && git init -qb main
git -c user.email=t@t -c user.name=t commit --allow-empty -qm init
/path/to/gg --time-track /tmp/tt.log status
/path/to/gg branch create t1 --time-track /tmp/tt.log
cat /tmp/tt.log
```

Expected: `gg start` lines (one per run), `git …` spans, and `op CreateBranch` — all valid JSONL (`jq . /tmp/tt.log` parses). A bad path (`--time-track /nope/x.log`) exits 2 with a message.

- [ ] **Step 6: Gate + commit**

```bash
go test ./... && go vet ./... && gofmt -l internal cmd
git add cmd/gg/main.go cmd/gg/timetrack_test.go
git commit -m "feat(cmd): global --time-track flag streams spans to a log file

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: Agent skill v4 + docs

**Files:**
- Modify: `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go` (`const Version = 3` → `4`), `internal/agentskill/agentskill_test.go`
- Regenerate: `.claude/skills/using-gg/SKILL.md` (drift-guard `TestDogfoodSkillCopyInSync` enforces sync)
- Modify: `README.md`, `CHANGELOG.md`

- [ ] **Step 1: Write the failing test**

In `internal/agentskill/agentskill_test.go`, `TestBodyCoversTheCLISurface`'s expected-substrings list gains `"--time-track"`.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/agentskill/`
Expected: FAIL — body missing `--time-track`.

- [ ] **Step 3: Update the embedded skill + version**

In `internal/agentskill/using-gg.md`, add to the end of the Commands list
(after the `gg init` bullet):

```markdown
- `--time-track <file>` (global; combine with any command) — append one JSON
  span per process start, git subprocess, and operation to `<file>` for
  performance analysis.
```

In `internal/agentskill/agentskill.go`, change `const Version = 3` to `const Version = 4`.

- [ ] **Step 4: Regenerate the committed dogfood skill**

```bash
go build -o ./gg ./cmd/gg
./gg init --update
go test ./internal/agentskill/   # drift-guard must pass
```

(Do not commit the `./gg` binary.)

- [ ] **Step 5: Update README.md and CHANGELOG.md**

`README.md` — in the CLI section, after the command block (near the
`--cwd-file` / shell-integration text), add:

```markdown
Every command (and the TUI) accepts a global `--time-track <file>` flag that
appends one JSON span per process start, git subprocess, and operation —
`jq . gg-perf.log` shows where the time went.
```

`CHANGELOG.md` — under `[Unreleased]` → `### Added`, add a new subsection
above the existing ones, matching their style:

```markdown
#### Performance log
- Global `--time-track <file>` flag (TUI + every CLI command): appends one
  redacted JSON span per process start, git subprocess, and engine operation.
  Same span schema as `gg inspect --trace` and the panic dump. Embedded
  using-gg agent skill v4.
```

- [ ] **Step 6: Full gate + commit**

```bash
go test ./... -race && go vet ./... && gofmt -l internal cmd
git add internal/agentskill/using-gg.md internal/agentskill/agentskill.go internal/agentskill/agentskill_test.go .claude/skills/using-gg/SKILL.md README.md CHANGELOG.md
git commit -m "docs(agentskill): document --time-track, bump skill to v4

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Final verification (after all tasks)

- [ ] `go test ./... -race && go vet ./... && gofmt -l internal cmd` — clean.
- [ ] Manual smoke per Task 4 Step 5 if not already done.
- [ ] Dispatch the final code reviewer over the whole branch diff (`git diff main...HEAD`).
- [ ] Present finishing-a-development-branch options (the user merges manually).
