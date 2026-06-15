# Interactive Rebase — Slice 1: gitexec per-command env — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a single git invocation carry extra environment (e.g. `GIT_SEQUENCE_EDITOR`), so a later slice can drive `git rebase -i` non-interactively. The existing `Run`/`Stream` stay as the no-env fast path.

**Architecture:** Add one method `RunEnv(ctx, name, argv, env []string)` to the `gitexec.Runner` interface; `Run` becomes a thin wrapper that calls `RunEnv(..., nil)`. `ExecRunner` sets `cmd.Env = append(os.Environ(), env...)` when `env != nil`; `FakeRunner` records the env for assertions; `LimitRunner` forwards it; the two test-only mock runners delegate. `env` entries are `"KEY=VALUE"` strings, appended onto the inherited process environment.

**Tech Stack:** Go 1.26, `os/exec`, `internal/gitexec`.

**Spec:** `docs/superpowers/specs/2026-06-15-interactive-rebase-design.md` (Slice 1).

**Conventions:** TDD; tests use a real `git` binary (the env test invokes `git var`); gate `./test.sh race`; commits end `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

**Why one method, not two:** only the driven rebase needs env, and it uses a `Run`-style call (it captures output; it does not stream). `StreamEnv` is YAGNI — add it if a streaming-with-env need ever appears.

**Why all implementers move in one task:** adding a method to the `Runner` interface makes the `gitexec` package (and `internal/domain` test mock) fail to compile until every implementer has it. They must change together to keep the build green. Implementers: `ExecRunner` (`exec.go`), `FakeRunner` (`fake.go`), `LimitRunner` (`limit.go`), `countingRunner` (`internal/gitexec/limit_test.go`), `pagingRunner` (`internal/domain/commitfeed_test.go`).

---

### Task 1: Add `RunEnv` to the `Runner` interface and every implementer

**Files:**
- Modify: `internal/gitexec/runner.go` (interface)
- Modify: `internal/gitexec/exec.go` (real env impl + `Run` delegates)
- Modify: `internal/gitexec/fake.go` (record env + `Run` delegates)
- Modify: `internal/gitexec/limit.go` (forward env)
- Modify: `internal/gitexec/limit_test.go` (mock `countingRunner` delegates)
- Modify: `internal/domain/commitfeed_test.go` (mock `pagingRunner` delegates)
- Test: `internal/gitexec/exec_test.go` (env reaches the subprocess)

- [ ] **Step 1: Write the failing real-subprocess test**

Add to `internal/gitexec/exec_test.go`:

```go
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
```

Confirm `exec_test.go` imports `context` and `strings` (add them if missing).

- [ ] **Step 2: Run to verify it fails (does not compile)**

Run: `go test ./internal/gitexec/ -run TestExecRunnerRunEnv -v`
Expected: build failure — `r.RunEnv undefined (type *ExecRunner has no field or method RunEnv)`.

- [ ] **Step 3: Add `RunEnv` to the `Runner` interface**

In `internal/gitexec/runner.go`, add the method to the interface and document `env`:

```go
// Runner executes git commands. name is the human label recorded as a span;
// argv is the full git argument vector (from gitcmd.Builder.ToArgv()).
type Runner interface {
	Run(ctx context.Context, name string, argv []string) (Result, error)
	// RunEnv is Run with extra environment for this one invocation. Each env
	// entry is "KEY=VALUE" and is appended onto the inherited process
	// environment. A nil env behaves exactly like Run.
	RunEnv(ctx context.Context, name string, argv, env []string) (Result, error)
	Stream(ctx context.Context, name string, argv []string, onLine func(string)) (Result, error)
}
```

- [ ] **Step 4: Implement `RunEnv` in `ExecRunner`; make `Run` delegate**

In `internal/gitexec/exec.go`, replace the existing `Run` method with a delegating wrapper plus the real `RunEnv`:

```go
func (r *ExecRunner) Run(ctx context.Context, name string, argv []string) (Result, error) {
	return r.RunEnv(ctx, name, argv, nil)
}

func (r *ExecRunner) RunEnv(ctx context.Context, name string, argv, env []string) (Result, error) {
	start := r.now()
	cmd := exec.CommandContext(ctx, r.gitPath, argv...)
	cmd.Dir = r.workDir
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	dur := r.now().Sub(start)
	exit := exitCodeOf(runErr)
	r.record(name, argv, exit, dur, start, runErr)

	res := Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exit, Duration: dur}
	if runErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return res, fmt.Errorf("%s cancelled: %w", name, ctxErr)
		}
		return res, fmt.Errorf("%s failed (exit %d): %s", name, exit, strings.TrimSpace(stderr.String()))
	}
	return res, nil
}
```

Add `"os"` to the `exec.go` import block.

- [ ] **Step 5: Record env in `FakeRunner`; make `Run` delegate**

In `internal/gitexec/fake.go`, add `Env` to `FakeCall`, and split `Run`/`RunEnv`:

```go
// FakeCall records one invocation of the fake runner.
type FakeCall struct {
	Name string
	Argv []string
	Env  []string
}
```

```go
func (f *FakeRunner) Run(ctx context.Context, name string, argv []string) (Result, error) {
	return f.RunEnv(ctx, name, argv, nil)
}

func (f *FakeRunner) RunEnv(_ context.Context, name string, argv, env []string) (Result, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, FakeCall{Name: name, Argv: argv, Env: env})
	err := f.errs[name]
	r, ok := f.responses[name]
	f.mu.Unlock()
	if err != nil {
		return r, err
	}
	if !ok {
		return Result{}, fmt.Errorf("fake: no response configured for %q", name)
	}
	return r, nil
}
```

`Stream` is unchanged (it already calls `f.Run`).

- [ ] **Step 6: Forward env in `LimitRunner`**

In `internal/gitexec/limit.go`, add `RunEnv` next to `Run`:

```go
func (l *LimitRunner) Run(ctx context.Context, name string, argv []string) (Result, error) {
	return l.RunEnv(ctx, name, argv, nil)
}

func (l *LimitRunner) RunEnv(ctx context.Context, name string, argv, env []string) (Result, error) {
	gitSem <- struct{}{}
	defer func() { <-gitSem }()
	return l.inner.RunEnv(ctx, name, argv, env)
}
```

- [ ] **Step 7: Make the two test-only mock runners satisfy the interface**

In `internal/gitexec/limit_test.go`, the `countingRunner` needs `RunEnv`. Add (delegating to its existing `Run`, which already tracks concurrency):

```go
func (r *countingRunner) RunEnv(ctx context.Context, name string, argv, env []string) (gitexec.Result, error) {
	return r.Run(ctx, name, argv)
}
```

(If `countingRunner` is declared in the `gitexec` package — same dir — use `Result` not `gitexec.Result`; match the file's existing return type on its `Run` method.)

In `internal/domain/commitfeed_test.go`, the `pagingRunner` needs `RunEnv`:

```go
func (r *pagingRunner) RunEnv(ctx context.Context, name string, argv, env []string) (gitexec.Result, error) {
	return r.Run(ctx, name, argv)
}
```

- [ ] **Step 8: Run the env tests and the affected packages**

Run: `go build ./... && go test ./internal/gitexec/ ./internal/domain/ -run 'RunEnv|Env|Paging|Limit' -v`
Expected: build clean; `TestExecRunnerRunEnvPassesEnvToSubprocess` and `TestExecRunnerRunStillInheritsEnv` PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/gitexec/runner.go internal/gitexec/exec.go internal/gitexec/fake.go internal/gitexec/limit.go internal/gitexec/exec_test.go internal/gitexec/limit_test.go internal/domain/commitfeed_test.go
git commit -m "feat(gitexec): RunEnv carries per-command environment

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: FakeRunner env-assertion coverage

**Files:**
- Test: `internal/gitexec/fake_test.go`

Slice 3 will assert that the rebase op sets `GIT_SEQUENCE_EDITOR`. Lock the
`FakeRunner.Env` recording behavior so those assertions rest on tested ground.

- [ ] **Step 1: Write the failing test**

Add to `internal/gitexec/fake_test.go`:

```go
func TestFakeRunnerRecordsEnv(t *testing.T) {
	f := NewFakeRunner()
	f.SetResponse("git rebase", Result{})
	env := []string{"GIT_SEQUENCE_EDITOR=gg __rebase-seq /tmp/plan.json"}
	if _, err := f.RunEnv(context.Background(), "git rebase", []string{"rebase", "-i", "base"}, env); err != nil {
		t.Fatalf("RunEnv: %v", err)
	}
	if len(f.Calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(f.Calls))
	}
	if !reflect.DeepEqual(f.Calls[0].Env, env) {
		t.Fatalf("recorded env = %v, want %v", f.Calls[0].Env, env)
	}
}

func TestFakeRunnerRunRecordsNilEnv(t *testing.T) {
	f := NewFakeRunner()
	f.SetResponse("git status", Result{})
	if _, err := f.Run(context.Background(), "git status", []string{"status"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if f.Calls[0].Env != nil {
		t.Fatalf("Run should record nil env, got %v", f.Calls[0].Env)
	}
}
```

Confirm `fake_test.go` imports `context` and `reflect` (add if missing).

- [ ] **Step 2: Run to verify it passes**

Run: `go test ./internal/gitexec/ -run TestFakeRunner -v`
Expected: PASS (the behavior was implemented in Task 1; this test pins it).

> If `TestFakeRunnerRecordsEnv` had been written before Task 1 it would have
> failed to compile (`RunEnv`/`FakeCall.Env` undefined). Authored here, after
> Task 1, it is a characterization test that guards the contract slice 3 relies
> on.

- [ ] **Step 3: Commit**

```bash
git add internal/gitexec/fake_test.go
git commit -m "test(gitexec): FakeRunner records per-command env

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Final verification (after all tasks)

- [ ] `./test.sh race` — vet+gofmt clean, all unit + e2e green.
- [ ] `superpowers:finishing-a-development-branch`.
- [ ] **After merge, RE-RUN `./test.sh race` on merged `main`** — drift discipline (main is moving fast; expect to rebase).

---

## Self-Review

**1. Spec coverage (Slice 1 only):**
- "A git invocation can carry extra environment (`GIT_SEQUENCE_EDITOR`, …)" → Task 1 `RunEnv` (`ExecRunner` sets `cmd.Env`). ✓
- "keeping the existing `Run`/`Stream` as the no-env fast path" → `Run` delegates to `RunEnv(..., nil)`; `Stream` untouched. ✓
- "`ExecRunner` sets `cmd.Env = append(os.Environ(), env...)`" → Task 1 Step 4. ✓
- "`FakeRunner` records the env passed" → Task 1 Step 5 + Task 2 tests. ✓
- "`LimitRunner` forwards env unchanged" → Task 1 Step 6. ✓

**2. Placeholder scan:** every code step shows complete code; the one conditional ("if `countingRunner` is in the `gitexec` package, use `Result` not `gitexec.Result`") is a concrete either/or tied to the file's existing return type, not a vague "handle it." ✓

**3. Type consistency:** `RunEnv(ctx context.Context, name string, argv, env []string) (Result, error)` is identical across the interface (runner.go), `ExecRunner`, `FakeRunner`, `LimitRunner`, and both test mocks. `FakeCall.Env` (`[]string`) is consistent between fake.go and the Task 2 assertions. `env` semantics (nil = inherit; non-nil = appended to `os.Environ()`) are stated in the interface doc and matched by `ExecRunner.RunEnv`. ✓

**Scope note:** `StreamEnv` deliberately omitted (YAGNI — the driven rebase uses a `Run`-style call). Documented in the header.
