# AI tasks — Plan 2: the task core (no UI)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans
> (this repo forbids subagents) to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** every AI task (commit message, review, conflict resolve, resolve &
complete) can be submitted to one process-global scheduler that runs it
headless (queued, capped, recorded in a 50-record history) or interactively
(an agent session whose every `$GG_MESSAGE_FILE` write becomes the task's
latest result). No TUI surface yet — plan 3 adds the launch dialog, the
Headless tab and the ◆ rows on top of this API.

**Architecture:** the three engine AI ops split into `Prepare` / `Collect`
(`Run` = prepare → capture → collect, so web and CLI are untouched) and gain a
sibling `ResolveConflict`. A new leaf `taskhist` keeps the last 50 records
plus their result/tail files. `domain.Tasks()` is a `*TaskManager`
(process-global like `Sessions()`): per-key FIFO, a global cap, headless runs
through `Service.Execute`, interactive runs through `Service.PrepareTask` +
an agent session + a result watcher. Kind builders
(`CommitMessageTask`/`ReviewTask`/`ConflictTask`) turn a configured tool
command into a `TaskSpec`. exttool gains `mode = "interactive"` and
catalogue rows for the four agents that start interactively with a prompt.

**Tech Stack:** Go 1.26, internal/engine, internal/domain, internal/agentsession,
internal/filewatch, internal/filelock, go-toml v2.

**Spec:** `docs/superpowers/specs/2026-09-24-ai-tasks-in-sessions-design.md`
(rulings 4–6, Architecture §B–§E, Error handling).

## Global Constraints

- `[tasks] max_parallel` default **3**, clamped in code to **1..10**; the cap
  counts AI tasks of every mode; user-started agent sessions and terminals are
  NOT counted.
- Task key `<kind> — <target>` (em dash, spaces) is identity and title; same
  key runs in sequence, different keys in parallel within the cap.
- History: the last **50** records, all repos, one store; result texts kept;
  output tail ≤ **64 KiB**.
- States: `queued → running → result-ready* → done | failed | cancelled`.
- `internal/tui` and `internal/cli` never import `internal/git`,
  `internal/taskhist` or `internal/agentsession` (archtest).
- Engine/domain/CLI prose stays English; no new TUI strings in this plan.
- New tests call `t.Parallel()` unless they set env or a global seam; tests
  that run `sh` scripts skip on Windows with `runtime.GOOS == "windows"` (never
  a `_windows_test.go`/`_unix_test.go` suffix).
- No subagents. Worktree `.claude/worktrees/ai-tasks-core`, branch
  `feat/ai-tasks-core`. `./test.sh race` green before asking to merge.

## Rulings made while planning (review these)

1. **Output tail lives in a file** (`<id>.tail`) beside `<id>.result`, not in
   the TOML index: 50 × 64 KiB would make every index rewrite ~3 MB. The
   record carries `TailFile`; the spec's `OutputTail` is read via `Tail(id)`.
2. **Records are written when a task ends** (done/failed/cancelled), never
   while queued/running — a crashed gg leaves no orphaned "running" rows.
   Live tasks are in memory only (`Tasks().List()`).
3. **Conflict kinds are `ResultOptional`**: an interactive/headless conflict
   run that exits 0 without writing a result ends `done`, not `failed` — the
   result of a conflict run is the repository state (today's "reported no
   overview" stance). Commit message and review keep the spec's rule (no
   result = failed).
4. **Per-file conflict commands stay mergetools**: `mode = "interactive"` is
   rejected with `per_file = true`, and a per-file `terminal` command is not an
   AI task (it keeps the handover). No catalogue agent row is per-file.
   `TaskSpec` still allows an op with no `MessageFile`, so plan 3 can add a
   per-file task later without changing the scheduler.
5. **What counts as interactive:** `mode = "interactive"`, or
   `mode = "terminal"` with `per_file = false` (today's whole-operation agent
   rows — spec §E: "an existing terminal-mode command is interactive
   already"). `capture` = headless.
6. **Agent identity** = the exttool tool id from the command's program
   (`agentIDFor`), display name = the catalogue `Label` ("Claude Code"); a
   custom command groups by its own `Name`. No new config field.
7. **Review results are not archived under `reviews/`** by the task path:
   the history keeps the text; plan 3 decides whether the lane also saves a
   report file.
8. **Conflict key sha** = HEAD's sha7 (moves every rebase round, fixed for a
   merge — so re-running the same paused state serialises).
9. **Key `<worktree>`** = the worktree directory's base name (what the
   Worktrees panel shows); a cross-repo name clash only serialises two
   tasks, never breaks them.
10. **`EnsureInteractiveCommands`** (first-run append, the
    `EnsureSessionCommands` shape) appends the safe interactive rows for a
    category only when the config has no interactive row for it at all. Plan
    3 calls it when the launch dialog opens.
11. **Agent probe:** `--help` checked 2026-09-25 — claude 2.1.282 (positional
    prompt, interactive), codex 0.153.4 (positional, interactive), junie
    26.9.7 (`--prompt=` "start interactive mode with an initial prompt"),
    agy 1.1.28 (`--prompt-interactive`), kimi 0.41.0 (only `-p`,
    non-interactive → headless only). A live smoke of each interactive row is
    plan 3's manual check on the real TUI.

## Review Focus

1. A result file rewritten with the SAME content must not raise a second
   result; a half-written file caught by the poll must not become a result
   (the stable-read check) — Task 7 pins both.
2. `TaskInputs.Env` is a delta: `os.Environ()` in it would re-add `TMUX` to a
   session child (`agentsession.childEnv` filters only the host env it builds
   itself) — Task 1 asserts no `PATH=` entry in a Prepare env.
3. Today's commit-message chooser, review lane and `gg review` must not pick
   up an interactive row and run it headless (it would wait forever for a
   human) — Task 4 pins all three.
4. Cancel in every state: queued (never starts, recorded cancelled), running
   headless (cancelled), interactive before any result (cancelled), after a
   result (done) — Tasks 6 and 7.
5. A task keeps the Service it was submitted with: a commit message
   submitted from worktree A runs in A even after the TUI switches to B —
   Task 6 runs a real `GenerateMessage` and checks the directory it ran in.

---

### Task 1: engine — Prepare / Collect split + `ResolveConflict`

**Files:**
- Create: `internal/engine/capture_task.go`
- Modify: `internal/engine/generate_message.go`, `internal/engine/review_changes.go`, `internal/engine/complete_conflict.go`
- Test: `internal/engine/capture_task_test.go`

**Interfaces:**
- Produces:
  - `type TaskInputs struct { Command, Dir string; Env []string; MessageFile string; Cleanup func() }`
  - `type CaptureTask interface { Operation; Prepare(ctx context.Context, deps OpDeps) (TaskInputs, error); Collect(in TaskInputs, stdout []byte) (Result, error) }`
  - `GenerateMessage`, `ReviewChanges`, `CompleteConflict` and the new
    `ResolveConflict` (same fields as `CompleteConflict`) implement `CaptureTask`.
  - `Env` holds ONLY additions (op.Env + `GG_*`); `Run` prepends `os.Environ()`.
  - `Cleanup` is idempotent and non-nil on success.

- [ ] **Step 1: Write the failing tests**

`internal/engine/capture_task_test.go`:

```go
package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hasEnvKey(env []string, key string) bool {
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			return true
		}
	}
	return false
}

func TestGenerateMessagePrepareCollect(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	stageFile(t, dir, repo, "a.txt", "one\n")
	op := GenerateMessage{Command: "true", Dir: dir, Env: []string{"GG_TASK=commit_message"}}
	in, err := op.Prepare(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	env := envMap(in.Env)
	if env["GG_TASK"] != "commit_message" || env["GG_MESSAGE_FILE"] != in.MessageFile || in.MessageFile == "" {
		t.Fatalf("env = %v, MessageFile = %q", in.Env, in.MessageFile)
	}
	if hasEnvKey(in.Env, "PATH") {
		t.Fatalf("Prepare env must be additions only, got os.Environ entries: %v", in.Env)
	}
	if in.Command != "true" || in.Dir != dir {
		t.Fatalf("Command/Dir = %q/%q", in.Command, in.Dir)
	}
	// stdout is used while the file is empty; CRLF is normalised.
	res, _ := op.Collect(in, []byte("subj\r\n\r\nbody\r\n"))
	if res.Captured != "subj\n\nbody\n" {
		t.Fatalf("captured = %q", res.Captured)
	}
	// Non-empty file content wins over stdout.
	if err := os.WriteFile(in.MessageFile, []byte("from file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ = op.Collect(in, []byte("stdout"))
	if res.Captured != "from file\n" {
		t.Fatalf("captured = %q, want the file content", res.Captured)
	}
	in.Cleanup()
	in.Cleanup() // idempotent
	for _, k := range []string{"GG_MESSAGE_FILE", "GG_CONTEXT_FILE", "GG_STAGED_DIFF"} {
		if _, err := os.Stat(env[k]); !os.IsNotExist(err) {
			t.Fatalf("%s not removed by Cleanup: %s", k, env[k])
		}
	}
}

func TestReviewChangesPrepareCollect(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	op := ReviewChanges{Command: "true", Dir: dir, RangeLabel: "working changes"}
	in, err := op.Prepare(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Cleanup()
	env := envMap(in.Env)
	if env["GG_REVIEW_DIFF"] == "" || env["GG_MESSAGE_FILE"] != in.MessageFile {
		t.Fatalf("env = %v", in.Env)
	}
	if hasEnvKey(in.Env, "PATH") {
		t.Fatalf("Prepare env must be additions only: %v", in.Env)
	}
	res, _ := op.Collect(in, []byte("report"))
	if res.Captured != "report" {
		t.Fatalf("captured = %q", res.Captured)
	}
}

func TestConflictOpsPrepareResolveTheTemplate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		op   CaptureTask
		task string
	}{
		{CompleteConflict{Command: "agent <context-file>", Dir: t.TempDir(), Op: "merge", ConflictedFiles: []string{"f.txt"}}, "conflict_complete"},
		{ResolveConflict{Command: "agent <context-file>", Dir: t.TempDir(), Op: "merge", ConflictedFiles: []string{"f.txt"}}, "conflict"},
	} {
		in, err := tc.op.Prepare(context.Background(), OpDeps{})
		if err != nil {
			t.Fatal(err)
		}
		env := envMap(in.Env)
		if env["GG_TASK"] != tc.task {
			t.Errorf("%T: GG_TASK = %q, want %q", tc.op, env["GG_TASK"], tc.task)
		}
		ctxFile := env["GG_CONTEXT_FILE"]
		if ctxFile == "" || !strings.Contains(in.Command, filepath.Base(ctxFile)) {
			t.Errorf("%T: command %q does not name the context file %q", tc.op, in.Command, ctxFile)
		}
		if in.MessageFile == "" || env["GG_MESSAGE_FILE"] != in.MessageFile {
			t.Errorf("%T: MessageFile %q / env %v", tc.op, in.MessageFile, in.Env)
		}
		in.Cleanup()
		if _, err := os.Stat(ctxFile); !os.IsNotExist(err) {
			t.Errorf("%T: context file survived Cleanup", tc.op)
		}
	}
}

func TestConflictOpPrepareCleansUpOnBadTemplate(t *testing.T) {
	t.Parallel()
	before, _ := filepath.Glob(filepath.Join(os.TempDir(), "gg-context-*.txt"))
	_, err := CompleteConflict{Command: "agent <bogus>", Dir: t.TempDir(), Op: "merge"}.Prepare(context.Background(), OpDeps{})
	if err == nil {
		t.Fatal("want a template error")
	}
	after, _ := filepath.Glob(filepath.Join(os.TempDir(), "gg-context-*.txt"))
	if len(after) > len(before) {
		t.Fatalf("Prepare leaked a context file on error: %d → %d", len(before), len(after))
	}
}

func TestResolveConflictRunCaptures(t *testing.T) {
	t.Parallel()
	fc := &fakeCapture{writeMsgFile: "summary"}
	res, err := ResolveConflict{Command: "agent", Dir: t.TempDir(), Op: "merge"}.Run(context.Background(), OpDeps{CaptureRunner: fc})
	if err != nil {
		t.Fatal(err)
	}
	if res.Captured != "summary" {
		t.Fatalf("captured = %q", res.Captured)
	}
	if envMap(fc.spec.Env)["PATH"] == "" {
		t.Fatalf("Run must hand the capture runner the full environment")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `rtk go test ./internal/engine/ -run 'PrepareCollect|ConflictOpsPrepare|ConflictOpPrepare|ResolveConflictRun'`
Expected: FAIL to compile — `op.Prepare undefined`, `undefined: ResolveConflict`, `undefined: CaptureTask`.

- [ ] **Step 3: Implement**

`internal/engine/capture_task.go`:

```go
package engine

import (
	"context"
	"os"
	"strings"
	"sync"
)

// TaskInputs is what an AI task's Prepare made for one run: the resolved
// command line and the directory it runs in, the environment ADDITIONS
// (never os.Environ — an agent session builds the child's environment
// itself and filters gg's host-terminal variables out of it), the
// $GG_MESSAGE_FILE path ("" when the kind has no file channel) and a
// Cleanup that removes every temp file Prepare created. Cleanup is
// idempotent.
type TaskInputs struct {
	Command     string
	Dir         string
	Env         []string
	MessageFile string
	Cleanup     func()
}

// CaptureTask is an AI-task operation split in three so the same inputs can
// feed a headless capture or an interactive session: Prepare builds the
// inputs, the caller runs the command, Collect turns what it produced into
// a Result (the output-channel contract: non-empty $GG_MESSAGE_FILE wins
// over stdout). Run is prepare → capture → collect.
type CaptureTask interface {
	Operation
	Prepare(ctx context.Context, deps OpDeps) (TaskInputs, error)
	Collect(in TaskInputs, stdout []byte) (Result, error)
}

// runCaptureTask is the Run body every CaptureTask shares.
func runCaptureTask(ctx context.Context, deps OpDeps, t CaptureTask) (Result, error) {
	in, err := t.Prepare(ctx, deps)
	if err != nil {
		return Result{}, err
	}
	defer in.Cleanup()
	env := append(append([]string{}, os.Environ()...), in.Env...)
	stdout, runErr := deps.captureRunner().Capture(ctx,
		CaptureSpec{Dir: in.Dir, Env: env, Command: in.Command},
		func(line string) { deps.emit(ctx, GitLine{Raw: line}) })
	res, _ := t.Collect(in, stdout)
	if runErr != nil {
		return Result{Captured: res.Captured}, runErr
	}
	return res, nil
}

// collectCaptured applies the output-channel contract: non-empty
// messageFile content wins over stdout. A Windows agent (cmd.exe echo, a
// CRLF-writing editor) emits \r\n; every consumer splits on \n.
func collectCaptured(messageFile string, stdout []byte) string {
	captured := string(stdout)
	if messageFile != "" {
		if b, err := os.ReadFile(messageFile); err == nil && strings.TrimSpace(string(b)) != "" {
			captured = string(b)
		}
	}
	return strings.ReplaceAll(captured, "\r\n", "\n")
}

// tempSet gathers the temp files of one Prepare for its Cleanup.
type tempSet struct {
	once  sync.Once
	paths []string
}

func (t *tempSet) write(pattern, content string) (string, error) {
	p, err := writeTempFile(pattern, content)
	if err == nil {
		t.paths = append(t.paths, p)
	}
	return p, err
}

func (t *tempSet) cleanup() {
	t.once.Do(func() {
		for _, p := range t.paths {
			os.Remove(p)
		}
	})
}
```

`internal/engine/generate_message.go` — replace `Run` with Prepare / Collect / Run (keep `buildSummary` and `writeTempFile`; update the type comment's "then removes them" to "Run = Prepare → capture → Collect"):

```go
func (op GenerateMessage) Prepare(ctx context.Context, deps OpDeps) (TaskInputs, error) {
	diff, err := deps.Repo.DiffPatch(ctx, model.DiffSpec{Cached: true})
	if err != nil {
		return TaskInputs{}, err
	}
	stat, _ := deps.Repo.DiffNumstat(ctx, model.DiffSpec{Cached: true})
	log, _ := deps.Repo.LogLines(ctx, "HEAD", 20)

	truncated := len(diff) > MaxDiffBytes
	diffBody := diff
	if truncated {
		diffBody = fmt.Sprintf("(diff truncated: %d bytes exceeds the %d KiB cap — inspect specific files with git)\n",
			len(diff), MaxDiffBytes>>10)
	}
	tmp := &tempSet{}
	fail := func(err error) (TaskInputs, error) { tmp.cleanup(); return TaskInputs{}, err }
	diffPath, err := tmp.write("gg-staged-*.diff", diffBody)
	if err != nil {
		return fail(err)
	}
	ctxPath, err := tmp.write("gg-ctx-*.txt", buildSummary(diffPath, stat, log, truncated))
	if err != nil {
		return fail(err)
	}
	// Empty output file: a task-agent tool writes the message here (see the
	// contract on GenerateMessage); a stdout tool leaves it empty. Lives in the
	// OS temp dir, outside the repo, so it never pollutes the working tree.
	msgPath, err := tmp.write("gg-msg-*.txt", "")
	if err != nil {
		return fail(err)
	}
	env := append(append([]string{}, op.Env...),
		"GG_CONTEXT_FILE="+ctxPath,
		"GG_STAGED_DIFF="+diffPath,
		"GG_MESSAGE_FILE="+msgPath,
		"GG_REPO="+op.Dir,
	)
	return TaskInputs{Command: op.Command, Dir: op.Dir, Env: env, MessageFile: msgPath, Cleanup: tmp.cleanup}, nil
}

func (op GenerateMessage) Collect(in TaskInputs, stdout []byte) (Result, error) {
	return Result{Captured: collectCaptured(in.MessageFile, stdout)}.WithSummary("generated commit message"), nil
}

func (op GenerateMessage) Run(ctx context.Context, deps OpDeps) (Result, error) {
	return runCaptureTask(ctx, deps, op)
}
```

`internal/engine/review_changes.go` — same shape (keep `reviewSummary`, `notesInstruction`):

```go
func (op ReviewChanges) Prepare(ctx context.Context, deps OpDeps) (TaskInputs, error) {
	diff, err := deps.Repo.DiffPatch(ctx, op.Diff)
	if err != nil {
		return TaskInputs{}, err
	}
	stat, _ := deps.Repo.DiffNumstat(ctx, op.Diff)
	truncated := len(diff) > MaxDiffBytes
	diffBody := diff
	if truncated {
		diffBody = fmt.Sprintf("(diff truncated: %d bytes exceeds the %d KiB cap — inspect specific files with git)\n",
			len(diff), MaxDiffBytes>>10)
	}
	tmp := &tempSet{}
	fail := func(err error) (TaskInputs, error) { tmp.cleanup(); return TaskInputs{}, err }
	diffPath, err := tmp.write("gg-review-*.diff", diffBody)
	if err != nil {
		return fail(err)
	}
	ctxPath, err := tmp.write("gg-review-ctx-*.txt", op.reviewSummary(diffPath, stat, truncated))
	if err != nil {
		return fail(err)
	}
	msgPath, err := tmp.write("gg-review-msg-*.md", "")
	if err != nil {
		return fail(err)
	}
	env := append(append([]string{}, op.Env...),
		"GG_CONTEXT_FILE="+ctxPath,
		"GG_REVIEW_DIFF="+diffPath,
		"GG_MESSAGE_FILE="+msgPath,
		"GG_REPO="+op.Dir,
	)
	if op.NotesFile != "" {
		// The CALLER owns this file: Cleanup removes only what Prepare made,
		// and the caller must still read the notes after the run.
		env = append(env, "GG_NOTES_FILE="+op.NotesFile)
	}
	return TaskInputs{Command: op.Command, Dir: op.Dir, Env: env, MessageFile: msgPath, Cleanup: tmp.cleanup}, nil
}

func (op ReviewChanges) Collect(in TaskInputs, stdout []byte) (Result, error) {
	return Result{Captured: collectCaptured(in.MessageFile, stdout)}.WithSummary("reviewed %s", op.RangeLabel), nil
}

func (op ReviewChanges) Run(ctx context.Context, deps OpDeps) (Result, error) {
	return runCaptureTask(ctx, deps, op)
}
```

`internal/engine/complete_conflict.go` — shared prepare, `CompleteConflict` keeps its fields and doc, `ResolveConflict` is new:

```go
// ResolveConflict runs a whole-operation conflict agent under the
// CatConflict contract — resolve and stage, never --continue (gg's
// ContinueOp owns the sequencer). Inputs and LockMode as CompleteConflict;
// GG_TASK=conflict. Its result (a summary the agent may write to
// $GG_MESSAGE_FILE) is informational: the outcome is the repository state.
type ResolveConflict struct {
	Command         string   // command TEMPLATE text (config); resolved by Prepare
	Dir             string   // worktree root the agent runs in
	Env             []string // caller env additions
	Op              string   // paused op: merge|rebase|cherry-pick|revert
	Source          string
	Target          string
	ConflictedFiles []string
}

var (
	_ CaptureTask = CompleteConflict{}
	_ CaptureTask = ResolveConflict{}
)

func (op CompleteConflict) LockMode() repogate.Mode { return repogate.Read }
func (op ResolveConflict) LockMode() repogate.Mode  { return repogate.Read }

func (op CompleteConflict) Prepare(_ context.Context, _ OpDeps) (TaskInputs, error) {
	return prepareConflictAgent(op.Command, op.Dir, op.Env, op.Op, op.Source, op.Target, op.ConflictedFiles, "conflict_complete")
}

func (op ResolveConflict) Prepare(_ context.Context, _ OpDeps) (TaskInputs, error) {
	return prepareConflictAgent(op.Command, op.Dir, op.Env, op.Op, op.Source, op.Target, op.ConflictedFiles, "conflict")
}

func (op CompleteConflict) Collect(in TaskInputs, stdout []byte) (Result, error) {
	return Result{Captured: collectCaptured(in.MessageFile, stdout)}.WithSummary("conflict agent finished (%s)", op.Op), nil
}

func (op ResolveConflict) Collect(in TaskInputs, stdout []byte) (Result, error) {
	return Result{Captured: collectCaptured(in.MessageFile, stdout)}.WithSummary("conflict agent finished (%s)", op.Op), nil
}

func (op CompleteConflict) Run(ctx context.Context, deps OpDeps) (Result, error) {
	return runCaptureTask(ctx, deps, op)
}

func (op ResolveConflict) Run(ctx context.Context, deps OpDeps) (Result, error) {
	return runCaptureTask(ctx, deps, op)
}

// prepareConflictAgent writes the context doc (op/source/target + the
// C-quoted conflicted paths — the bytes the TUI's tool runs write) and an
// empty $GG_MESSAGE_FILE, then resolves the command template against them:
// a custom <context-file> token needs the real temp path, which exists only
// here.
func prepareConflictAgent(command, dir string, extra []string, opName, source, target string, files []string, task string) (TaskInputs, error) {
	tmp := &tempSet{}
	fail := func(err error) (TaskInputs, error) { tmp.cleanup(); return TaskInputs{}, err }
	ctxPath, err := tmp.write("gg-context-*.txt", template.ConflictContextDoc(opName, source, target, files))
	if err != nil {
		return fail(err)
	}
	msgPath, err := tmp.write("gg-overview-*.md", "")
	if err != nil {
		return fail(err)
	}
	resolved, err := template.ResolveCommand(command, nil, template.CmdCtx{
		Op: opName, Source: source, Target: target,
		ConflictedFiles: files, Repo: dir, ContextFile: ctxPath,
	})
	if err != nil {
		return fail(err)
	}
	env := append(append([]string{}, extra...),
		"GG_OP="+opName,
		"GG_SOURCE="+source,
		"GG_TARGET="+target,
		"GG_CONFLICTED_FILES="+strings.Join(files, " "),
		"GG_REPO="+dir,
		"GG_FILE=", "GG_LOCAL=", "GG_BASE=", "GG_REMOTE=", "GG_MERGED=",
		"GG_CONTEXT_FILE="+ctxPath,
		"GG_MESSAGE_FILE="+msgPath,
		"GG_TASK="+task,
	)
	return TaskInputs{Command: resolved, Dir: dir, Env: env, MessageFile: msgPath, Cleanup: tmp.cleanup}, nil
}
```

Delete the old `CompleteConflict.Run` body and its `var _ Operation = CompleteConflict{}` (the `var (...)` block above replaces it); drop the now-unused `os` import if the compiler says so.

- [ ] **Step 4: Run the engine tests**

Run: `rtk go test ./internal/engine/`
Expected: PASS — the new tests and every existing `GenerateMessage`/`ReviewChanges`/`CompleteConflict` test (their fakes read `CaptureSpec.Env`, which `Run` still builds as `os.Environ()` + additions).

- [ ] **Step 5: Run the callers' packages**

Run: `rtk go test ./internal/domain/ ./internal/web/ ./internal/tui/ -run 'Review|Complete|Generate|Commit'`
Expected: PASS (web/CLI/TUI unchanged).

- [ ] **Step 6: Commit**

```bash
rtk git add internal/engine/capture_task.go internal/engine/capture_task_test.go internal/engine/generate_message.go internal/engine/review_changes.go internal/engine/complete_conflict.go
rtk git commit -m "feat(engine): split the AI capture ops into Prepare/Collect; add ResolveConflict"
```

---

### Task 2: `[tasks] max_parallel`

**Files:**
- Modify: `internal/config/config.go` (Config, Defaults, Load overlay, new TasksConfig)
- Modify: `internal/config/template.go` (settingDocs row, section list)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.TasksConfig{MaxParallel int}` (`toml:"max_parallel"`), `Config.Tasks`, `config.MaxParallelCap = 10`,
  `func (TasksConfig) Parallel() (n int, warning string)`.

- [ ] **Step 1: Write the failing test**

```go
func TestTasksMaxParallel(t *testing.T) {
	t.Parallel()
	if n, w := Defaults().Tasks.Parallel(); n != 3 || w != "" {
		t.Fatalf("default = %d %q, want 3 and no warning", n, w)
	}
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	if err := os.WriteFile(global, []byte("[tasks]\nmax_parallel = 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(global, "")
	if err != nil {
		t.Fatal(err)
	}
	if n, w := cfg.Tasks.Parallel(); n != 5 || w != "" {
		t.Fatalf("configured = %d %q, want 5", n, w)
	}
	for _, tc := range []struct{ in, want int }{{-2, 1}, {11, 10}, {99, 10}} {
		n, w := TasksConfig{MaxParallel: tc.in}.Parallel()
		if n != tc.want || w == "" {
			t.Errorf("Parallel(%d) = %d %q, want %d with a warning", tc.in, n, w, tc.want)
		}
	}
	if !strings.Contains(Template(), "max_parallel") {
		t.Error("the config template does not document [tasks] max_parallel")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `rtk go test ./internal/config/ -run TestTasksMaxParallel`
Expected: FAIL to compile — `Defaults().Tasks undefined`.

- [ ] **Step 3: Implement**

In `config.go` add the field to `Config` after `Console`:

```go
	Tasks    TasksConfig    `toml:"tasks"`
```

in `Defaults()` add `Tasks: TasksConfig{MaxParallel: 3},`, in `Load`'s overlay list add `overlayTasks(&cfg.Tasks, layer.Tasks)`, and after `overlayConsole`:

```go
// MaxParallelCap is the hard ceiling on AI tasks running at once, whatever
// the config says (ruling 5: clamped in code to 1..10).
const MaxParallelCap = 10

// TasksConfig is the [tasks] section: AI tasks (commit message, review,
// conflict agents) in every mode. User-started agent sessions and
// terminals are never counted.
type TasksConfig struct {
	MaxParallel int `toml:"max_parallel"` // tasks running at once; clamped 1..MaxParallelCap
}

// Parallel is the effective cap and, when the configured value was out of
// range, the warning to show once.
func (t TasksConfig) Parallel() (int, string) {
	switch {
	case t.MaxParallel < 1:
		return 1, fmt.Sprintf("[tasks] max_parallel = %d is below 1; using 1", t.MaxParallel)
	case t.MaxParallel > MaxParallelCap:
		return MaxParallelCap, fmt.Sprintf("[tasks] max_parallel = %d is above %d; using %d", t.MaxParallel, MaxParallelCap, MaxParallelCap)
	}
	return t.MaxParallel, ""
}

// overlayTasks copies the set (non-zero) [tasks] fields.
func overlayTasks(dst *TasksConfig, src TasksConfig) {
	if src.MaxParallel != 0 {
		dst.MaxParallel = src.MaxParallel
	}
}
```

In `template.go` add after the `console` rows:

```go
	{"tasks", "max_parallel", 3, "AI tasks (commit message, review, conflict agents) running at once, headless and interactive counted together; clamped to 1..10; agent sessions you start yourself and terminals are not counted"},
```

and add `"tasks"` to `Template()`'s section list after `"console"`.

- [ ] **Step 4: Run the config tests**

Run: `rtk go test ./internal/config/`
Expected: PASS (including any settingDocs-coverage test).

- [ ] **Step 5: Commit**

```bash
rtk git add internal/config/config.go internal/config/template.go internal/config/config_test.go
rtk git commit -m "feat(config): [tasks] max_parallel, clamped 1..10"
```

---

### Task 3: `taskhist` — the 50-record history

**Files:**
- Create: `internal/taskhist/taskhist.go`, `internal/taskhist/file_store.go`, `internal/taskhist/mem_store.go`
- Test: `internal/taskhist/file_store_test.go`
- Modify: `internal/archtest/import_guard_test.go` (frontends forbidden; leaf budget)

**Interfaces:**
- Produces:
  - `const Max = 50`, `const MaxTail = 64 << 10`
  - `type Record struct { ID, Key, Kind, Agent, Repo, Worktree, Mode, State string; Started, Ended time.Time; ExitCode int; Err, ResultFile, TailFile string }`
  - `type Store interface { List() ([]Record, error); Add(r Record, result, tail string) error; Result(id string) (string, error); Tail(id string) (string, error); Remove(id string) error }`
  - `func NewFileStore(root string) *FileStore`, `func NewMemStore() *MemStore`, `func NewID(now time.Time) string`, `func TrimTail(s string) string`

- [ ] **Step 1: Write the failing tests**

`internal/taskhist/file_store_test.go`:

```go
package taskhist

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func rec(id string) Record {
	return Record{ID: id, Key: "review — " + id, Kind: "review", State: "done", Started: time.Unix(1, 0).UTC(), Ended: time.Unix(2, 0).UTC()}
}

func stores(t *testing.T) map[string]Store {
	return map[string]Store{"file": NewFileStore(t.TempDir()), "mem": NewMemStore()}
}

func TestAddListResultTail(t *testing.T) {
	t.Parallel()
	for name, st := range stores(t) {
		if err := st.Add(rec("a"), "result a", "tail a"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := st.Add(rec("b"), "", ""); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		list, err := st.List()
		if err != nil || len(list) != 2 || list[0].ID != "b" || list[1].ID != "a" {
			t.Fatalf("%s: List = %+v, %v (want newest first)", name, list, err)
		}
		if list[1].Key != "review — a" || !list[1].Started.Equal(time.Unix(1, 0)) {
			t.Fatalf("%s: record did not round-trip: %+v", name, list[1])
		}
		if r, err := st.Result("a"); err != nil || r != "result a" {
			t.Fatalf("%s: Result = %q, %v", name, r, err)
		}
		if r, err := st.Tail("a"); err != nil || r != "tail a" {
			t.Fatalf("%s: Tail = %q, %v", name, r, err)
		}
		if r, err := st.Result("b"); err != nil || r != "" {
			t.Fatalf("%s: a record with no result reads %q, %v", name, r, err)
		}
	}
}

func TestPruneDropsOldestAndItsFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	st := NewFileStore(root)
	for i := 0; i < Max+1; i++ {
		if err := st.Add(rec(string(rune('A'+i%26))+strings.Repeat("x", i/26)), "r", "t"); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := st.List()
	if len(list) != Max {
		t.Fatalf("kept %d records, want %d", len(list), Max)
	}
	if _, err := os.Stat(filepath.Join(root, "A.result")); !os.IsNotExist(err) {
		t.Fatal("the pruned record's result file survived")
	}
	if _, err := os.Stat(filepath.Join(root, "A.tail")); !os.IsNotExist(err) {
		t.Fatal("the pruned record's tail file survived")
	}
}

func TestRemoveDeletesRecordAndFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	st := NewFileStore(root)
	_ = st.Add(rec("a"), "r", "t")
	if err := st.Remove("a"); err != nil {
		t.Fatal(err)
	}
	if list, _ := st.List(); len(list) != 0 {
		t.Fatalf("List = %+v after Remove", list)
	}
	if _, err := os.Stat(filepath.Join(root, "a.result")); !os.IsNotExist(err) {
		t.Fatal("result file survived Remove")
	}
}

func TestCorruptIndexIsAnErrorNotAWipe(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tasks.toml"), []byte("not = [toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := NewFileStore(root)
	if _, err := st.List(); err == nil {
		t.Fatal("List of a corrupt index must fail")
	}
	if err := st.Add(rec("a"), "", ""); err == nil {
		t.Fatal("Add over a corrupt index must fail, not rewrite it from empty")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "tasks.toml")); string(b) != "not = [toml" {
		t.Fatal("the corrupt index was overwritten")
	}
}

func TestUnwritableRootFails(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "plain")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewFileStore(filepath.Join(file, "sub")).Add(rec("a"), "r", ""); err == nil {
		t.Fatal("Add under a regular file must fail")
	}
}

func TestTrimTailKeepsTheEnd(t *testing.T) {
	t.Parallel()
	s := strings.Repeat("é", MaxTail) // 2 bytes per rune
	got := TrimTail(s)
	if len(got) > MaxTail || !strings.HasSuffix(s, got) || !strings.HasPrefix(got, "é") {
		t.Fatalf("TrimTail kept %d bytes, prefix %q", len(got), got[:4])
	}
}

func TestNewIDIsUniqueAndSafe(t *testing.T) {
	t.Parallel()
	now := time.Now()
	a, b := NewID(now), NewID(now)
	if a == b || strings.ContainsAny(a, `/\: `) {
		t.Fatalf("NewID = %q, %q", a, b)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `rtk go test ./internal/taskhist/`
Expected: FAIL — package has no Go files / undefined symbols.

- [ ] **Step 3: Implement**

`internal/taskhist/taskhist.go`:

```go
// Package taskhist is gigagit's machine-local history of AI tasks (commit
// message, review, conflict agents): the last Max records across every
// repository, each with its result text and output tail in files beside
// the index. Owned by internal/domain — frontends reach it only through
// domain.Tasks().
package taskhist

import (
	"crypto/rand"
	"encoding/hex"
	"time"
	"unicode/utf8"
)

// Max is how many records are kept (spec ruling 4).
const Max = 50

// MaxTail caps a record's kept output tail.
const MaxTail = 64 << 10

// Record is one finished task. Times are UTC. ResultFile and TailFile are
// base names under the store root, "" when there is none.
type Record struct {
	ID         string    `toml:"id"`
	Key        string    `toml:"key"`
	Kind       string    `toml:"kind"`
	Agent      string    `toml:"agent"`
	Repo       string    `toml:"repo"`
	Worktree   string    `toml:"worktree"`
	Mode       string    `toml:"mode"`
	State      string    `toml:"state"`
	Started    time.Time `toml:"started"`
	Ended      time.Time `toml:"ended"`
	ExitCode   int       `toml:"exit_code"`
	Err        string    `toml:"err"`
	ResultFile string    `toml:"result_file"`
	TailFile   string    `toml:"tail_file"`
}

// Store persists records newest first.
type Store interface {
	List() ([]Record, error)
	// Add stores r with its result and tail texts ("" = none), prepends it
	// and prunes to Max, deleting the pruned records' files.
	Add(r Record, result, tail string) error
	Result(id string) (string, error)
	Tail(id string) (string, error)
	Remove(id string) error
}

// NewID is a sortable, filename-safe unique id.
func NewID(now time.Time) string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return now.UTC().Format("20060102-150405.000000") + "-" + hex.EncodeToString(b[:])
}

// TrimTail keeps the last MaxTail bytes of s, starting on a rune boundary.
func TrimTail(s string) string {
	if len(s) <= MaxTail {
		return s
	}
	s = s[len(s)-MaxTail:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}
```

`internal/taskhist/file_store.go`:

```go
package taskhist

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/filelock"
)

// FileStore keeps tasks.toml plus <id>.result / <id>.tail under root,
// rewritten atomically (temp+rename) under a process mutex and the shared
// cross-process lock: every gg process on the machine records its tasks
// here.
type FileStore struct {
	root string
	mu   sync.Mutex
}

// NewFileStore roots a store at root (XDG resolution is domain's job).
func NewFileStore(root string) *FileStore { return &FileStore{root: root} }

type index struct {
	Tasks []Record `toml:"tasks"`
}

func (fs *FileStore) path() string { return filepath.Join(fs.root, "tasks.toml") }

// read parses the index. A missing file is empty; a corrupt one is an
// error — treating it as empty would let the next Add wipe the history.
func (fs *FileStore) read() ([]Record, error) {
	data, err := os.ReadFile(fs.path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var idx index
	if err := toml.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("taskhist: %s is corrupt: %w", fs.path(), err)
	}
	return idx.Tasks, nil
}

func (fs *FileStore) List() ([]Record, error) { return fs.read() }

func (fs *FileStore) write(rs []Record) error {
	data, err := toml.Marshal(index{Tasks: rs})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(fs.root, "tasks-*.toml")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, fs.path()); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// locked runs f under the process mutex and the file lock.
func (fs *FileStore) locked(f func() error) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if err := os.MkdirAll(fs.root, 0o700); err != nil {
		return err
	}
	unlock, err := filelock.Acquire(fs.path() + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	return f()
}

func (fs *FileStore) Add(r Record, result, tail string) error {
	if r.ID == "" {
		return errors.New("taskhist: a record needs an id")
	}
	return fs.locked(func() error {
		before, err := fs.read()
		if err != nil {
			return err
		}
		if result != "" {
			r.ResultFile = r.ID + ".result"
			if err := os.WriteFile(filepath.Join(fs.root, r.ResultFile), []byte(result), 0o600); err != nil {
				return err
			}
		}
		if tail = TrimTail(tail); tail != "" {
			r.TailFile = r.ID + ".tail"
			if err := os.WriteFile(filepath.Join(fs.root, r.TailFile), []byte(tail), 0o600); err != nil {
				return err
			}
		}
		out := make([]Record, 0, len(before)+1)
		out = append(out, r)
		for _, x := range before {
			if x.ID != r.ID {
				out = append(out, x)
			}
		}
		for _, x := range out[min(len(out), Max):] {
			fs.removeFiles(x)
		}
		return fs.write(out[:min(len(out), Max)])
	})
}

func (fs *FileStore) removeFiles(r Record) {
	for _, f := range []string{r.ResultFile, r.TailFile} {
		if f != "" {
			os.Remove(filepath.Join(fs.root, f))
		}
	}
}

func (fs *FileStore) text(id string, pick func(Record) string) (string, error) {
	rs, err := fs.read()
	if err != nil {
		return "", err
	}
	for _, r := range rs {
		if r.ID != id {
			continue
		}
		name := pick(r)
		if name == "" {
			return "", nil
		}
		b, err := os.ReadFile(filepath.Join(fs.root, name))
		return string(b), err
	}
	return "", fmt.Errorf("taskhist: no task %s", id)
}

func (fs *FileStore) Result(id string) (string, error) {
	return fs.text(id, func(r Record) string { return r.ResultFile })
}

func (fs *FileStore) Tail(id string) (string, error) {
	return fs.text(id, func(r Record) string { return r.TailFile })
}

func (fs *FileStore) Remove(id string) error {
	return fs.locked(func() error {
		rs, err := fs.read()
		if err != nil {
			return err
		}
		out := rs[:0:0]
		for _, r := range rs {
			if r.ID == id {
				fs.removeFiles(r)
				continue
			}
			out = append(out, r)
		}
		return fs.write(out)
	})
}
```

`internal/taskhist/mem_store.go`:

```go
package taskhist

import (
	"fmt"
	"sync"
)

// MemStore is the in-memory Store: the fallback when the file store cannot
// be written (spec: tasks still run, one notice, records in memory).
type MemStore struct {
	mu      sync.Mutex
	recs    []Record
	results map[string]string
	tails   map[string]string
}

func NewMemStore() *MemStore {
	return &MemStore{results: map[string]string{}, tails: map[string]string{}}
}

func (m *MemStore) List() ([]Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Record(nil), m.recs...), nil
}

func (m *MemStore) Add(r Record, result, tail string) error {
	if r.ID == "" {
		return fmt.Errorf("taskhist: a record needs an id")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[r.ID], m.tails[r.ID] = result, TrimTail(tail)
	out := []Record{r}
	for _, x := range m.recs {
		if x.ID != r.ID {
			out = append(out, x)
		}
	}
	for _, x := range out[min(len(out), Max):] {
		delete(m.results, x.ID)
		delete(m.tails, x.ID)
	}
	m.recs = out[:min(len(out), Max)]
	return nil
}

func (m *MemStore) Result(id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.results[id], nil
}

func (m *MemStore) Tail(id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tails[id], nil
}

func (m *MemStore) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.recs[:0:0]
	for _, r := range m.recs {
		if r.ID != id {
			out = append(out, r)
		}
	}
	m.recs = out
	delete(m.results, id)
	delete(m.tails, id)
	return nil
}
```

In `internal/archtest/import_guard_test.go` add to `forbidden`:

```go
		"github.com/homeend/gigagit/internal/taskhist":     "frontends must reach the AI-task history through internal/domain",
```

and after `TestLinkhistIsALeaf`:

```go
// TestTaskhistIsALeaf pins internal/taskhist's budget: records + text files
// under an explicit root, stdlib, the shared file lock and go-toml only.
func TestTaskhistIsALeaf(t *testing.T) {
	t.Parallel()
	allowed := map[string]bool{
		"github.com/homeend/gigagit/internal/filelock": true,
		"github.com/pelletier/go-toml/v2":              true,
	}
	for _, imp := range directImports(t, "github.com/homeend/gigagit/internal/taskhist") {
		if allowed[imp] {
			continue
		}
		if first := strings.SplitN(imp, "/", 2)[0]; strings.Contains(first, ".") {
			t.Errorf("internal/taskhist imports %s — only stdlib, internal/filelock and go-toml are allowed", imp)
		}
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `rtk go test ./internal/taskhist/ ./internal/archtest/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/taskhist internal/archtest/import_guard_test.go
rtk git commit -m "feat(taskhist): 50-record AI-task history with result and tail files"
```

---

### Task 4: exttool `mode = "interactive"` — validation, catalogue rows, picker filters, task modes

**Files:**
- Modify: `internal/exttool/exttool.go` (ModeInteractive, prompts, catalogue rows)
- Modify: `internal/config/tools.go` (ValidateToolCommand)
- Modify: `internal/tui/tools.go`, `internal/tui/commit_generate.go:50`, `internal/tui/review.go:103,239`
- Modify: `internal/cli/review.go:260`
- Create: `internal/domain/task_modes.go`
- Test: `internal/config/tools_test.go`, `internal/tui/tools_test.go`, `internal/cli/review_test.go`, `internal/domain/task_modes_test.go`

**Interfaces:**
- Produces:
  - `exttool.ModeInteractive Mode = "interactive"`
  - `type domain.TaskMode string`; `domain.TaskHeadless = "headless"`, `domain.TaskInteractive = "interactive"`
  - `func domain.TaskModeOf(tc config.ToolCommand) (TaskMode, bool)`
  - `type domain.TaskChoice struct { AgentID, Agent string; Headless, Interactive []config.ToolCommand }`
  - `func domain.TaskChoices(cfg config.Config, kind exttool.Category, frontend string) []TaskChoice`
  - `func domain.AgentName(tc config.ToolCommand) string`
  - `func domain.EnsureInteractiveCommands(cfg config.Config, globalPath string, detect func() []exttool.Detection) ([]string, error)`
  - `func (m Model) laneToolCommands(category string) []config.ToolCommand` (tui)

- [ ] **Step 1: Write the failing tests**

`internal/config/tools_test.go` (append):

```go
func TestValidateInteractiveMode(t *testing.T) {
	t.Parallel()
	ok := func(cat string, perFile bool) ToolCommand {
		return ToolCommand{Category: cat, Name: "A", Mode: "interactive", PerFile: perFile, Command: "agent"}
	}
	for _, cat := range []string{"commit_message", "review", "conflict", "conflict_complete"} {
		if err := ValidateToolCommand(ok(cat, false)); err != nil {
			t.Errorf("%s interactive: %v", cat, err)
		}
	}
	if err := ValidateToolCommand(ok("session", false)); err == nil {
		t.Error("a session command cannot be interactive-mode")
	}
	if err := ValidateToolCommand(ok("conflict", true)); err == nil {
		t.Error("per_file + interactive must be rejected (per-file commands are mergetools)")
	}
}
```

`internal/tui/tools_test.go` (append):

```go
func TestLaneToolCommandsSkipInteractive(t *testing.T) {
	t.Parallel()
	m := toolCfg(
		config.ToolCommand{Category: "commit_message", Name: "Claude", Mode: "capture", Command: "claude -p x"},
		config.ToolCommand{Category: "commit_message", Name: "Claude (interactive)", Mode: "interactive", Command: "claude x"},
		config.ToolCommand{Category: "review", Name: "Claude (interactive)", Mode: "interactive", Command: "claude x"},
	)
	if got := m.laneToolCommands("commit_message"); len(got) != 1 || got[0].Name != "Claude" {
		t.Fatalf("commit lane = %+v, want only the capture row", got)
	}
	if got := m.laneToolCommands("review"); len(got) != 0 {
		t.Fatalf("review lane = %+v, want none (the only row is interactive)", got)
	}
	if m.hasReviewTool() {
		t.Fatal("hasReviewTool must not count an interactive row")
	}
}
```

`internal/cli/review_test.go` (append; follow the file's existing config-fixture helper — if it writes a repo `.gg.toml`, reuse it with these two blocks):

```go
func TestSelectReviewCommandSkipsInteractive(t *testing.T) {
	// Not parallel if the file's fixtures set XDG/HOME env — match them.
	svc, cleanup := reviewCfgService(t, `
[[tools.command]]
category = "review"
name = "Claude (interactive)"
mode = "interactive"
command = "claude x"
`)
	defer cleanup()
	var stderr bytes.Buffer
	if _, err := selectReviewCommand(svc, "", &stderr); err == nil {
		t.Fatal("an interactive review row must not be selectable by gg review")
	}
}
```

If `review_test.go` has no such helper, add `reviewCfgService(t, toml string) (*domain.Service, func())` next to the test: create a repo with `gittest.BasicRepo`, write `toml` to `<dir>/.gg.toml`, return `domain.Open(dir)` and a no-op cleanup; point the global config away from the user's file the way the file's other tests do (`t.Setenv("XDG_CONFIG_HOME", t.TempDir())`).

`internal/domain/task_modes_test.go`:

```go
package domain

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/template"
)

func TestTaskModeOf(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode    string
		perFile bool
		want    TaskMode
		ok      bool
	}{
		{"capture", false, TaskHeadless, true},
		{"interactive", false, TaskInteractive, true},
		{"terminal", false, TaskInteractive, true},
		{"terminal", true, "", false},
		{"session", false, "", false},
	} {
		got, ok := TaskModeOf(config.ToolCommand{Mode: tc.mode, PerFile: tc.perFile})
		if got != tc.want || ok != tc.ok {
			t.Errorf("TaskModeOf(%s, perFile=%v) = %q %v", tc.mode, tc.perFile, got, ok)
		}
	}
}

func TestTaskChoicesGroupByAgent(t *testing.T) {
	t.Parallel()
	var cfg config.Config
	cfg.Tools.Command = []config.ToolCommand{
		{Category: "commit_message", Name: "Claude", Mode: "capture", Command: "claude -p x"},
		{Category: "commit_message", Name: "Claude (interactive)", Mode: "interactive", Command: "claude x"},
		{Category: "commit_message", Name: "Kimi", Mode: "capture", Command: "kimi -p x"},
		{Category: "commit_message", Name: "Mine", Mode: "capture", Command: "./my-tool"},
		{Category: "review", Name: "Claude", Mode: "capture", Command: "claude -p x"},
	}
	got := TaskChoices(cfg, exttool.CatCommitMessage, "tui")
	if len(got) != 3 {
		t.Fatalf("choices = %+v, want claude, kimi, Mine", got)
	}
	if got[0].AgentID != "claude" || got[0].Agent != "Claude Code" || len(got[0].Headless) != 1 || len(got[0].Interactive) != 1 {
		t.Errorf("claude choice = %+v", got[0])
	}
	if got[1].AgentID != "kimi" || len(got[1].Interactive) != 0 {
		t.Errorf("kimi choice = %+v", got[1])
	}
	if got[2].AgentID != "" || got[2].Agent != "Mine" {
		t.Errorf("custom choice = %+v", got[2])
	}
}

func TestCatalogueInteractiveRows(t *testing.T) {
	t.Parallel()
	want := map[string]bool{"claude": true, "codex": true, "junie": true, "antigravity": true}
	for _, tl := range exttool.Builtins() {
		for _, ct := range tl.Commands {
			if ct.Mode != exttool.ModeInteractive {
				continue
			}
			if !want[tl.ID] {
				t.Errorf("%s ships an interactive row, but it has no interactive-with-prompt mode", tl.ID)
			}
			gen := exttool.GenerateCommand(ct, tl.Bins[0])
			tc := config.ToolCommand{Category: string(ct.Category), Name: ct.Name, Mode: string(ct.Mode), Command: gen}
			if err := config.ValidateToolCommand(tc); err != nil {
				t.Errorf("%s/%s: %v", tl.ID, ct.Name, err)
			}
			if err := template.ValidateCommandTokens(gen, false); err != nil {
				t.Errorf("%s/%s tokens: %v", tl.ID, ct.Name, err)
			}
			if !strings.Contains(ct.Command, "<env:GG_MESSAGE_FILE>") || !strings.Contains(ct.Command, "wait for further instructions") {
				t.Errorf("%s/%s: an interactive prompt must write $GG_MESSAGE_FILE and then wait", tl.ID, ct.Name)
			}
		}
	}
	for id := range want {
		for _, cat := range []exttool.Category{exttool.CatCommitMessage, exttool.CatReview} {
			if !catalogueHas(id, cat, exttool.ModeInteractive, false) {
				t.Errorf("%s: no safe interactive %s row", id, cat)
			}
		}
	}
}

func catalogueHas(id string, cat exttool.Category, mode exttool.Mode, optIn bool) bool {
	for _, tl := range exttool.Builtins() {
		if tl.ID != id {
			continue
		}
		for _, ct := range tl.Commands {
			if ct.Category == cat && ct.Mode == mode && ct.OptIn == optIn {
				return true
			}
		}
	}
	return false
}

func TestEnsureInteractiveCommands(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	detect := func() []exttool.Detection {
		for _, tl := range exttool.Builtins() {
			if tl.ID == "claude" {
				return []exttool.Detection{{Tool: tl, Bin: "claude"}}
			}
		}
		return nil
	}
	var cfg config.Config
	added, err := EnsureInteractiveCommands(cfg, path, detect)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 2 { // commit_message + review, never the OptIn yolo rows
		t.Fatalf("added = %v", added)
	}
	cfg, err = config.Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := EnsureInteractiveCommands(cfg, path, detect)
	if len(again) != 0 {
		t.Fatalf("second run added %v, want nothing", again)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `rtk go test ./internal/config/ ./internal/domain/ ./internal/tui/ ./internal/cli/ -run 'InteractiveMode|TaskModeOf|TaskChoices|CatalogueInteractive|EnsureInteractive|LaneToolCommands|SkipsInteractive'`
Expected: FAIL to compile — `undefined: TaskModeOf`, `exttool.ModeInteractive`, `m.laneToolCommands`.

- [ ] **Step 3: Implement**

`internal/exttool/exttool.go` — the mode:

```go
	// ModeInteractive runs an AI task as an agent session that starts with
	// the task prompt and stays open (commit_message, review, conflict,
	// conflict_complete; never per-file). Its prompt writes the result to
	// $GG_MESSAGE_FILE — each write is the task's latest result — then
	// waits for further instructions.
	ModeInteractive Mode = "interactive"
```

The prompts (after the kimi constants):

```go
// Interactive task prompts: the capture contract's file channel (an
// interactive session has no captured stdout), rewritten on every
// revision, then the agent waits — the user keeps talking to it in gg's
// console. <range> is the injection-safe hex range (see ReviewTarget).
//
// Probed 2026-09-25 (--help of the installed binaries): claude 2.1.282 and
// codex 0.153.4 take a positional prompt and stay interactive; junie
// 26.9.7 `--prompt=<text>` "Start interactive mode with an initial prompt
// already submitted"; agy 1.1.28 `--prompt-interactive` "Run an initial
// prompt interactively and continue the session". kimi 0.41.0 has only
// `-p` (non-interactive) — no interactive rows for it.
const interactiveCommitPrompt = `"Write a git commit message for the staged changes. The change summary is at <env:GG_CONTEXT_FILE> (files changed, recent-commit style) and the full diff at <env:GG_STAGED_DIFF>. Write ONLY the commit message - a concise imperative subject line (max ~72 chars), a blank line, then a short body explaining what changed and why - into the file at <env:GG_MESSAGE_FILE> (an absolute path outside the repository), overwriting it each time you revise the message. Do not run git commit and do not modify any other files. Then wait for further instructions."`

const interactiveReviewPrompt = `"You are reviewing a code change. The summary is at <env:GG_CONTEXT_FILE> and the full diff at <env:GG_REVIEW_DIFF> (range <range>). Write a concise code review - findings with severity and a short summary - into the file at <env:GG_MESSAGE_FILE> (an absolute path outside the repository), overwriting it each time you revise the review. Do NOT modify any repository files and do NOT run git commit. Then wait for further instructions."`
```

Catalogue rows — add to each tool's `Commands`, after its `CatReview` capture row:

```go
// claude
{Category: CatCommitMessage, Name: "Claude (interactive)", Mode: ModeInteractive, Command: `<bin> ` + interactiveCommitPrompt},
{Category: CatCommitMessage, Name: "Claude (interactive, yolo)", Mode: ModeInteractive, OptIn: true, Command: `<bin> ` + interactiveCommitPrompt + ` --dangerously-skip-permissions`},
{Category: CatReview, Name: "Claude (interactive)", Mode: ModeInteractive, Command: `<bin> ` + interactiveReviewPrompt},
{Category: CatReview, Name: "Claude (interactive, yolo)", Mode: ModeInteractive, OptIn: true, Command: `<bin> ` + interactiveReviewPrompt + ` --dangerously-skip-permissions`},
// junie
{Category: CatCommitMessage, Name: "Junie (interactive)", Mode: ModeInteractive, Command: `<bin> --prompt ` + interactiveCommitPrompt},
{Category: CatCommitMessage, Name: "Junie (interactive, yolo)", Mode: ModeInteractive, OptIn: true, Command: `<bin> --prompt ` + interactiveCommitPrompt + ` --brave`},
{Category: CatReview, Name: "Junie (interactive)", Mode: ModeInteractive, Command: `<bin> --prompt ` + interactiveReviewPrompt},
{Category: CatReview, Name: "Junie (interactive, yolo)", Mode: ModeInteractive, OptIn: true, Command: `<bin> --prompt ` + interactiveReviewPrompt + ` --brave`},
// codex
{Category: CatCommitMessage, Name: "Codex (interactive)", Mode: ModeInteractive, Command: `<bin> ` + interactiveCommitPrompt},
{Category: CatCommitMessage, Name: "Codex (interactive, yolo)", Mode: ModeInteractive, OptIn: true, Command: `<bin> ` + interactiveCommitPrompt + ` --dangerously-bypass-approvals-and-sandbox`},
{Category: CatReview, Name: "Codex (interactive)", Mode: ModeInteractive, Command: `<bin> ` + interactiveReviewPrompt},
{Category: CatReview, Name: "Codex (interactive, yolo)", Mode: ModeInteractive, OptIn: true, Command: `<bin> ` + interactiveReviewPrompt + ` --dangerously-bypass-approvals-and-sandbox`},
// antigravity
{Category: CatCommitMessage, Name: "Antigravity (interactive)", Mode: ModeInteractive, Command: `<bin> --prompt-interactive ` + interactiveCommitPrompt},
{Category: CatCommitMessage, Name: "Antigravity (interactive, yolo)", Mode: ModeInteractive, OptIn: true, Command: `<bin> --prompt-interactive ` + interactiveCommitPrompt + ` --dangerously-skip-permissions`},
{Category: CatReview, Name: "Antigravity (interactive)", Mode: ModeInteractive, Command: `<bin> --prompt-interactive ` + interactiveReviewPrompt},
{Category: CatReview, Name: "Antigravity (interactive, yolo)", Mode: ModeInteractive, OptIn: true, Command: `<bin> --prompt-interactive ` + interactiveReviewPrompt + ` --dangerously-skip-permissions`},
```

`internal/config/tools.go` — `ValidateToolCommand`: the mode switch becomes

```go
	switch tc.Mode {
	case "terminal", "capture", "session", "interactive":
	default:
		return fmt.Errorf("tools: %s: unknown mode %q (want terminal|capture|interactive|session)", tc.Name, tc.Mode)
	}
```

and after the `per_file` category check add

```go
	if tc.PerFile && tc.Mode == "interactive" {
		return fmt.Errorf("tools: %s: per_file commands are mergetools (mode = \"terminal\"); interactive is whole-operation only", tc.Name)
	}
```

(the existing session↔session rule already rejects `category = "session"` with `mode = "interactive"`). Update the `Mode` field comment to `// terminal | capture | interactive | session (session: category = "session" only)`.

`internal/tui/tools.go` — after `toolCommands`:

```go
// laneToolCommands is toolCommands minus interactive rows: the headless
// commit-message and review lanes cannot run a command that waits for a
// human. Plan 3 routes interactive rows through the task launch dialog.
func (m Model) laneToolCommands(category string) []config.ToolCommand {
	var out []config.ToolCommand
	for _, tc := range m.toolCommands(category) {
		if tc.Mode != string(exttool.ModeInteractive) {
			out = append(out, tc)
		}
	}
	return out
}
```

Replace `m.toolCommands(string(exttool.CatCommitMessage))` at `commit_generate.go:50` and both `m.toolCommands(string(exttool.CatReview))` in `review.go` (`hasReviewTool`, `startReviewLane`) with `m.laneToolCommands(...)`.

`internal/cli/review.go` `selectReviewCommand` — after the frontend check:

```go
		if tc.Mode == string(exttool.ModeInteractive) {
			continue // waits for a human; gg review runs headless
		}
```

`internal/domain/task_modes.go`:

```go
package domain

import (
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/template"
)

// TaskMode is how an AI task runs.
type TaskMode string

const (
	TaskHeadless    TaskMode = "headless"    // queued, captured, no console
	TaskInteractive TaskMode = "interactive" // an agent session; each $GG_MESSAGE_FILE write is a result
)

// TaskModeOf says how tc runs as an AI task: capture → headless;
// interactive, or a whole-operation terminal command (today's conflict
// agent rows) → interactive. A per-file terminal command is a mergetool and
// a session command is not a task: ok = false.
func TaskModeOf(tc config.ToolCommand) (TaskMode, bool) {
	switch tc.Mode {
	case string(exttool.ModeCapture):
		return TaskHeadless, true
	case string(exttool.ModeInteractive):
		return TaskInteractive, true
	case string(exttool.ModeTerminal):
		if !tc.PerFile {
			return TaskInteractive, true
		}
	}
	return "", false
}

// AgentName is the display name of tc's agent: the catalogue label of the
// program it runs ("Claude Code"), else the command's own name.
func AgentName(tc config.ToolCommand) string {
	if id := agentIDFor(tc); id != "" {
		for _, tl := range exttool.Builtins() {
			if tl.ID == id {
				return tl.Label
			}
		}
	}
	return tc.Name
}

// TaskChoice is one agent the launch dialog offers for a kind, with its
// commands per mode (config order; more than one = variants, e.g. yolo).
type TaskChoice struct {
	AgentID     string // exttool tool id; "" for a custom command
	Agent       string // display name
	Headless    []config.ToolCommand
	Interactive []config.ToolCommand
}

// TaskChoices groups the valid kind commands offered in frontend by agent,
// in first-appearance order. A custom command (no catalogue program) is its
// own agent, keyed by name.
func TaskChoices(cfg config.Config, kind exttool.Category, frontend string) []TaskChoice {
	var out []TaskChoice
	index := map[string]int{}
	for _, tc := range cfg.Tools.Command {
		if tc.Category != string(kind) || !config.ToolVisibleIn(tc, frontend) {
			continue
		}
		if config.ValidateToolCommand(tc) != nil || template.ValidateCommandTokens(tc.Command, tc.PerFile) != nil {
			continue
		}
		mode, ok := TaskModeOf(tc)
		if !ok {
			continue
		}
		id := agentIDFor(tc)
		key := "id:" + id
		if id == "" {
			key = "name:" + tc.Name
		}
		i, seen := index[key]
		if !seen {
			i = len(out)
			index[key] = i
			out = append(out, TaskChoice{AgentID: id, Agent: AgentName(tc)})
		}
		if mode == TaskHeadless {
			out[i].Headless = append(out[i].Headless, tc)
		} else {
			out[i].Interactive = append(out[i].Interactive, tc)
		}
	}
	return out
}

// EnsureInteractiveCommands is the first-run append for interactive task
// commands (the EnsureSessionCommands shape): for each of commit_message
// and review with NO interactive-capable row in cfg, append the detected
// agents' safe (never OptIn) interactive rows to the global config.
// Conflict kinds already have interactive rows (their terminal agents).
func EnsureInteractiveCommands(cfg config.Config, globalPath string, detect func() []exttool.Detection) ([]string, error) {
	need := map[exttool.Category]bool{exttool.CatCommitMessage: true, exttool.CatReview: true}
	for _, tc := range cfg.Tools.Command {
		if mode, ok := TaskModeOf(tc); ok && mode == TaskInteractive {
			delete(need, exttool.Category(tc.Category))
		}
	}
	if len(need) == 0 {
		return nil, nil
	}
	var blocks []config.ToolCommand
	var names []string
	for _, det := range detect() {
		for _, ct := range det.Tool.Commands {
			if !need[ct.Category] || ct.Mode != exttool.ModeInteractive || ct.OptIn {
				continue
			}
			blocks = append(blocks, config.ToolCommand{
				Category: string(ct.Category), Name: ct.Name, Mode: string(ct.Mode),
				Frontends: ct.Frontends, Command: exttool.GenerateCommand(ct, det.Bin),
			})
			names = append(names, string(ct.Category)+"/"+ct.Name)
		}
	}
	if len(blocks) == 0 {
		return nil, nil
	}
	if err := config.AppendToolCommands(globalPath, blocks); err != nil {
		return nil, err
	}
	return names, nil
}
```

- [ ] **Step 4: Run the affected packages**

Run: `rtk go test ./internal/exttool/ ./internal/config/ ./internal/domain/ ./internal/cli/ ./internal/tui/ ./internal/web/`
Expected: PASS. If an existing exttool/settings test pins the catalogue's row count or a per-tool list, update its expectation to include the new rows (ledger it).

- [ ] **Step 5: Commit**

```bash
rtk git add internal/exttool internal/config/tools.go internal/config/tools_test.go internal/tui/tools.go internal/tui/tools_test.go internal/tui/commit_generate.go internal/tui/review.go internal/cli/review.go internal/cli/review_test.go internal/domain/task_modes.go internal/domain/task_modes_test.go
rtk git commit -m "feat(exttool): interactive mode for AI tasks — catalogue rows, validation, task modes"
```

---

### Task 5: domain — task keys, kind builders, PrepareTask, resolved session start

**Files:**
- Create: `internal/domain/task_kinds.go`
- Modify: `internal/domain/sessions.go` (extract `startLine`)
- Test: `internal/domain/task_kinds_test.go`

**Interfaces:**
- Consumes: `engine.CaptureTask`, `engine.TaskInputs`, `engine.ResolveConflict` (Task 1); `TaskMode`, `TaskModeOf`, `AgentName` (Task 4).
- Produces:
  - `type TaskSpec struct { Key string; Kind exttool.Category; Agent, AgentID string; Mode TaskMode; Repo, Worktree, Cwd string; Env []string; Cols, Rows int; Svc *Service; Op engine.CaptureTask; Parse func(captured string) (string, error); ResultOptional bool }`
  - `func CommitMessageKey(worktree, head string) string`, `ReviewKey(worktree string, t ReviewTarget) string`, `ConflictKey(worktree, op, head string) string`, `CompleteKey(worktree, op, head string) string`
  - `func (s *Service) CommitMessageTask(ctx, tc) (TaskSpec, error)`, `ReviewTask(ctx, tc, target ReviewTarget, notesFile string) (TaskSpec, error)`, `ConflictTask(ctx, tc, complete bool) (TaskSpec, error)`
  - `func (s *Service) PrepareTask(ctx context.Context, op engine.CaptureTask) (engine.TaskInputs, error)`
  - `func (s *Service) startLine(ctx context.Context, mgr *agentsession.Manager, label, agentID, line, worktreeDir, cwd string, cols, rows int, env []string) (*AgentSession, error)`

- [ ] **Step 1: Write the failing tests**

`internal/domain/task_kinds_test.go`:

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/model"
)

func TestTaskKeys(t *testing.T) {
	t.Parallel()
	const head = "0123456789abcdef0123456789abcdef01234567"
	if got := CommitMessageKey("/w/repo.worktrees/feat-x", head); got != "commit message — feat-x @ 0123456" {
		t.Errorf("commit key = %q", got)
	}
	if got := CommitMessageKey(`C:\w\feat-x`, head); got != "commit message — feat-x @ 0123456" {
		t.Errorf("windows commit key = %q", got)
	}
	r := ReviewTarget{Range: "aaaaaaaaaaaa..bbbbbbbbbbbb", Label: "feat"}
	if got := ReviewKey("/w/main", r); got != "review — aaaaaaa..bbbbbbb" {
		t.Errorf("review key = %q", got)
	}
	if got := ReviewKey("/w/main", WorkingReviewTarget()); got != "review — main working changes" {
		t.Errorf("working review key = %q", got)
	}
	if got := ConflictKey("/w/main", "rebase", head); got != "resolve conflict — main rebase 0123456" {
		t.Errorf("conflict key = %q", got)
	}
	if got := CompleteKey("/w/main", "merge", head); got != "resolve & complete — main merge 0123456" {
		t.Errorf("complete key = %q", got)
	}
}

func TestCommitMessageTaskSpec(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	tc := config.ToolCommand{Category: "commit_message", Name: "Claude (interactive)", Mode: "interactive", Command: "claude write <repo>"}
	spec, err := svc.CommitMessageTask(context.Background(), tc)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Mode != TaskInteractive || spec.Kind != exttool.CatCommitMessage || spec.Agent != "Claude Code" || spec.AgentID != "claude" {
		t.Fatalf("spec = %+v", spec)
	}
	if spec.Key != CommitMessageKey(dir, headHash(t, dir)) || spec.Svc != svc || spec.Parse == nil || spec.ResultOptional {
		t.Fatalf("spec = %+v", spec)
	}
	op, ok := spec.Op.(engine.GenerateMessage)
	if !ok || !strings.Contains(op.Command, filepath.Base(dir)) || strings.Contains(op.Command, "<repo>") {
		t.Fatalf("op = %#v — want the command resolved against the worktree", spec.Op)
	}
	if _, err := svc.CommitMessageTask(context.Background(), config.ToolCommand{Category: "conflict", Name: "Meld", Mode: "terminal", PerFile: true, Command: "meld"}); err == nil {
		t.Fatal("a mergetool is not an AI task")
	}
}

func TestParseCommitMessageResult(t *testing.T) {
	t.Parallel()
	got, err := parseCommitMessage("Subject\n\nBody line\n")
	if err != nil || got != "Subject\n\nBody line" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := parseCommitMessage("  \n"); err == nil {
		t.Fatal("an empty message must be an error")
	}
}

func TestReviewTaskSpec(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	tc := config.ToolCommand{Category: "review", Name: "Claude", Mode: "capture", Command: "claude -p /code-review <range>"}
	target := ReviewTarget{Kind: ReviewRange, Range: "aaaaaaaaaaaa..bbbbbbbbbbbb", Label: "x", Diff: model.DiffSpec{Rev: "HEAD"}}
	spec, err := svc.ReviewTask(context.Background(), tc, target, "")
	if err != nil {
		t.Fatal(err)
	}
	op := spec.Op.(engine.ReviewChanges)
	if spec.Mode != TaskHeadless || !strings.Contains(op.Command, "aaaaaaaaaaaa..bbbbbbbbbbbb") || op.Dir != dir {
		t.Fatalf("spec = %+v op = %+v", spec, op)
	}
	if got, err := spec.Parse(`{"result":"## Findings"}`); err != nil || got != "## Findings" {
		t.Fatalf("Parse = %q, %v", got, err)
	}
	if _, err := spec.Parse("   "); err == nil {
		t.Fatal("an empty review must be an error")
	}
}

func TestConflictTaskSpec(t *testing.T) {
	t.Parallel()
	dir, svc := conflictedMergeRepo(t)
	tc := config.ToolCommand{Category: "conflict_complete", Name: "Claude — resolve & complete (yolo)", Mode: "terminal", Command: "claude go"}
	spec, err := svc.ConflictTask(context.Background(), tc, true)
	if err != nil {
		t.Fatal(err)
	}
	op, ok := spec.Op.(engine.CompleteConflict)
	if !ok || op.Op != "merge" || len(op.ConflictedFiles) != 1 || op.ConflictedFiles[0] != "f.txt" {
		t.Fatalf("op = %#v", spec.Op)
	}
	if spec.Key != CompleteKey(dir, "merge", headHash(t, dir)) || !spec.ResultOptional || spec.Mode != TaskInteractive {
		t.Fatalf("spec = %+v", spec)
	}
	spec, err = svc.ConflictTask(context.Background(), config.ToolCommand{Category: "conflict", Name: "Kimi", Mode: "capture", Command: "kimi -p x"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := spec.Op.(engine.ResolveConflict); !ok || spec.Key != ConflictKey(dir, "merge", headHash(t, dir)) {
		t.Fatalf("resolve spec = %+v", spec)
	}
	_, clean := newRealRepo(t)
	if _, err := clean.ConflictTask(context.Background(), tc, true); err == nil {
		t.Fatal("no paused operation must refuse")
	}
}

func TestPrepareTaskUnderTheGate(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	in, err := svc.PrepareTask(context.Background(), engine.GenerateMessage{Command: "true", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Cleanup()
	if _, err := os.Stat(in.MessageFile); err != nil {
		t.Fatalf("message file: %v", err)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `rtk go test ./internal/domain/ -run 'TaskKeys|MessageTaskSpec|ParseCommitMessage|ReviewTaskSpec|ConflictTaskSpec|PrepareTask'`
Expected: FAIL to compile — `undefined: CommitMessageKey`, `svc.CommitMessageTask`, …

- [ ] **Step 3: Implement**

`internal/domain/sessions.go` — `StartSession` resolves then delegates:

```go
func (s *Service) StartSession(ctx context.Context, tc config.ToolCommand, worktreeDir, cwd string, cols, rows int, env []string) (*AgentSession, error) {
	resolved, err := template.ResolveCommand(tc.Command, nil, template.CmdCtx{Repo: worktreeDir})
	if err != nil {
		return nil, err
	}
	return s.startLine(ctx, Sessions(), tc.Name, agentIDFor(tc), resolved, worktreeDir, cwd, cols, rows, env)
}

// startLine runs an already-resolved command line as a session on mgr (the
// AI-task path hands it a line Prepare resolved against its temp files —
// resolving it again would misread any <…> in a path).
func (s *Service) startLine(ctx context.Context, mgr *agentsession.Manager, label, agentID, line, worktreeDir, cwd string, cols, rows int, env []string) (*AgentSession, error) {
	repo, err := s.RepoName(ctx)
	if err != nil || repo == "" {
		repo = filepath.Base(worktreeDir)
	}
	argv, cmdline := sessionShell(line, runtime.GOOS, os.Getenv)
	return mgr.Start(agentsession.StartSpec{
		Label: label, AgentID: agentID, Repo: repo, Dir: worktreeDir,
		Cwd: cwd, Argv: argv, CmdLine: cmdline, Env: env, Cols: cols, Rows: rows,
		TracePath: sessionTracePath(os.Getenv("GG_SESSION_TRACE"), label, time.Now()),
	})
}
```

`internal/domain/task_kinds.go`:

```go
package domain

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/repogate"
	"github.com/homeend/gigagit/internal/template"
)

// TaskSpec is one AI task to submit to Tasks(). A kind builder
// (CommitMessageTask, ReviewTask, ConflictTask) fills the identity, the op
// and the result hooks; the frontend adds Env (its GG_INBOX), Cwd (a
// translated worktree path) and the console size.
type TaskSpec struct {
	Key      string           // "<kind> — <target>": identity and title (ruling 6)
	Kind     exttool.Category // commit_message | review | conflict | conflict_complete
	Agent    string           // display name, e.g. "Claude Code"
	AgentID  string           // exttool tool id, "" for a custom command
	Mode     TaskMode
	Repo     string // repository display name
	Worktree string // worktree directory: the session's identity
	Cwd      string // where the process runs when it differs from Worktree
	Env      []string
	Cols     int
	Rows     int
	// Svc is the Service the task was submitted from. It runs (headless) or
	// prepares (interactive) the op even after the frontend has switched to
	// another worktree.
	Svc *Service
	Op  engine.CaptureTask
	// Parse shapes a collected capture into the result text; an error or an
	// empty text is "no result". nil = strings.TrimSpace.
	Parse func(captured string) (string, error)
	// ResultOptional: a run that exits 0 without a result is done, not
	// failed — a conflict run's outcome is the repository state.
	ResultOptional bool
}

var longHex = regexp.MustCompile(`\b[0-9a-f]{8,40}\b`)

func sha7(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// worktreeName is the directory's base name in either notation.
func worktreeName(dir string) string {
	return path.Base(strings.ReplaceAll(dir, `\`, "/"))
}

func CommitMessageKey(worktree, head string) string {
	return "commit message — " + worktreeName(worktree) + " @ " + sha7(head)
}

func ReviewKey(worktree string, t ReviewTarget) string {
	if strings.TrimSpace(t.Range) == "" {
		return "review — " + worktreeName(worktree) + " working changes"
	}
	return "review — " + longHex.ReplaceAllStringFunc(t.Range, sha7)
}

func ConflictKey(worktree, op, head string) string {
	return "resolve conflict — " + worktreeName(worktree) + " " + op + " " + sha7(head)
}

func CompleteKey(worktree, op, head string) string {
	return "resolve & complete — " + worktreeName(worktree) + " " + op + " " + sha7(head)
}

// taskBase fills the fields every kind shares.
func (s *Service) taskBase(ctx context.Context, tc config.ToolCommand, kind exttool.Category) (TaskSpec, string, error) {
	mode, ok := TaskModeOf(tc)
	if !ok || tc.Category != string(kind) {
		return TaskSpec{}, "", fmt.Errorf("%s cannot run as a %s task", tc.Name, kind)
	}
	top, err := s.TopLevel(ctx)
	if err != nil {
		return TaskSpec{}, "", err
	}
	repo, err := s.RepoName(ctx)
	if err != nil || repo == "" {
		repo = worktreeName(top)
	}
	return TaskSpec{
		Kind: kind, Agent: AgentName(tc), AgentID: agentIDFor(tc), Mode: mode,
		Repo: repo, Worktree: top, Svc: s,
	}, top, nil
}

// CommitMessageTask builds a commit-message task over the staged changes.
func (s *Service) CommitMessageTask(ctx context.Context, tc config.ToolCommand) (TaskSpec, error) {
	spec, top, err := s.taskBase(ctx, tc, exttool.CatCommitMessage)
	if err != nil {
		return TaskSpec{}, err
	}
	head, err := s.RevParse(ctx, "HEAD")
	if err != nil {
		return TaskSpec{}, err
	}
	resolved, err := template.ResolveCommand(tc.Command, nil, template.CmdCtx{Repo: top})
	if err != nil {
		return TaskSpec{}, err
	}
	spec.Key = CommitMessageKey(top, head)
	spec.Op = engine.GenerateMessage{Command: resolved, Dir: top, Env: []string{"GG_TASK=commit_message"}}
	spec.Parse = parseCommitMessage
	return spec, nil
}

// parseCommitMessage normalises any catalogue tool's output to
// "subject\n\nbody" (or just the subject).
func parseCommitMessage(captured string) (string, error) {
	subject, body, err := exttool.ParseCaptureMessage([]byte(captured))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(subject) == "" {
		return "", errors.New("empty commit message")
	}
	if strings.TrimSpace(body) == "" {
		return subject, nil
	}
	return subject + "\n\n" + body, nil
}

// ReviewTask builds a review task over target. notesFile as ReviewReportNotes.
func (s *Service) ReviewTask(ctx context.Context, tc config.ToolCommand, target ReviewTarget, notesFile string) (TaskSpec, error) {
	spec, top, err := s.taskBase(ctx, tc, exttool.CatReview)
	if err != nil {
		return TaskSpec{}, err
	}
	resolved, err := template.ResolveCommand(tc.Command, nil, template.CmdCtx{Range: target.Range, Repo: top})
	if err != nil {
		return TaskSpec{}, err
	}
	spec.Key = ReviewKey(top, target)
	spec.Op = engine.ReviewChanges{
		Command: resolved, Dir: top, Env: []string{"GG_TASK=review"},
		Diff: target.Diff, RangeLabel: target.DisplayLabel(), NotesFile: notesFile,
	}
	spec.Parse = parseReport
	return spec, nil
}

// parseReport unwraps a JSON-enveloped report; an empty one is an error.
func parseReport(captured string) (string, error) {
	report, err := exttool.ParseCaptureReport(captured)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(report) == "" {
		return "", errors.New("empty report")
	}
	return strings.TrimSpace(report), nil
}

// ConflictTask builds a whole-operation conflict task against the paused
// operation: resolve & complete when complete, else resolve (never
// --continue). Refuses when nothing is paused.
func (s *Service) ConflictTask(ctx context.Context, tc config.ToolCommand, complete bool) (TaskSpec, error) {
	kind := exttool.CatConflict
	if complete {
		kind = exttool.CatConflictComplete
	}
	spec, top, err := s.taskBase(ctx, tc, kind)
	if err != nil {
		return TaskSpec{}, err
	}
	st, err := s.Status(ctx)
	if err != nil {
		return TaskSpec{}, err
	}
	cs := s.Conflict(ctx, st)
	if cs.Op == "" {
		return TaskSpec{}, errors.New("no paused operation to resolve")
	}
	head, err := s.RevParse(ctx, "HEAD")
	if err != nil {
		return TaskSpec{}, err
	}
	files := unmergedPaths(st)
	if complete {
		spec.Key = CompleteKey(top, cs.Op, head)
		spec.Op = engine.CompleteConflict{Command: tc.Command, Dir: top, Op: cs.Op, Source: cs.Source, Target: cs.Target, ConflictedFiles: files}
	} else {
		spec.Key = ConflictKey(top, cs.Op, head)
		spec.Op = engine.ResolveConflict{Command: tc.Command, Dir: top, Op: cs.Op, Source: cs.Source, Target: cs.Target, ConflictedFiles: files}
	}
	spec.Parse = parseReport
	spec.ResultOptional = true
	return spec, nil
}

// PrepareTask runs op.Prepare under the op's reservation (the gate Execute
// uses), so the inputs are a consistent read of the repository. The temp
// files outlive the reservation; the caller runs in.Cleanup.
func (s *Service) PrepareTask(ctx context.Context, op engine.CaptureTask) (engine.TaskInputs, error) {
	mode := repogate.Read
	if lm, ok := op.(lockModer); ok {
		mode = lm.LockMode()
	}
	res, err := s.gateFor(ctx).Acquire(ctx, mode, "prepare "+engine.OpName(op))
	if err != nil {
		return engine.TaskInputs{}, err
	}
	defer res.Release()
	return op.Prepare(ctx, engine.OpDeps{Repo: s.repo})
}
```

- [ ] **Step 4: Run the domain tests**

Run: `rtk go test ./internal/domain/`
Expected: PASS (the session tests still pass through `StartSession`).

- [ ] **Step 5: Commit**

```bash
rtk git add internal/domain/task_kinds.go internal/domain/task_kinds_test.go internal/domain/sessions.go
rtk git commit -m "feat(domain): AI task keys, kind builders and PrepareTask"
```

---

### Task 6: `domain.Tasks()` — scheduler, headless runs, cancel, history

**Files:**
- Create: `internal/domain/tasks.go`
- Test: `internal/domain/tasks_test.go`

**Interfaces:**
- Consumes: `TaskSpec` (Task 5), `taskhist.Store`/`NewID`/`TrimTail`/`MaxTail` (Task 3), `engine.GitLine`.
- Produces:
  - `type TaskID string`; `type TaskState string` with `TaskQueued`, `TaskRunning`, `TaskResultReady`, `TaskDone`, `TaskFailed`, `TaskCancelled`; `func (TaskState) Live() bool`
  - `type TaskInfo struct { ID TaskID; Key, Agent, Repo, Worktree string; Kind exttool.Category; Mode TaskMode; State TaskState; Submitted, Started, Ended time.Time; Session SessionID; Result string; Results, ExitCode int; Err, Tail string }`
  - `type TaskRecord = taskhist.Record`
  - `func NewTaskManager(hist taskhist.Store) *TaskManager`
  - methods: `SetMaxParallel(n int)`, `Max() int`, `Submit(TaskSpec) TaskID`, `Get(TaskID) (TaskInfo, bool)`, `List() []TaskInfo`, `Cancel(TaskID) error`, `KillAll(ctx context.Context)`, `Changed() <-chan struct{}`, `History() []TaskRecord`, `HistoryResult(id string) (string, error)`, `HistoryTail(id string) (string, error)`, `RemoveHistory(id string) error`
  - unexported: `m.sessions *agentsession.Manager` (Task 7 uses it), `m.setResult(t *task, result string)`, `type taskEnd struct { state TaskState; exit int; err, tail string }`, `recordOf(TaskInfo) TaskRecord`
  - Ordering guarantee: a task's history record is written before its end state is visible through `Get`/`List`.

- [ ] **Step 1: Write the failing tests**

`internal/domain/tasks_test.go`:

```go
package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/repogate"
	"github.com/homeend/gigagit/internal/taskhist"
)

// blockOp is a CaptureTask whose headless Run waits for release (or its
// ctx) and returns out/err — the scheduler's fake workload.
type blockOp struct {
	started chan string // receives key when Run begins (buffered)
	release chan struct{}
	key     string
	out     string
	err     error
}

func (blockOp) LockMode() repogate.Mode { return repogate.Read }
func (blockOp) Prepare(context.Context, engine.OpDeps) (engine.TaskInputs, error) {
	return engine.TaskInputs{Cleanup: func() {}}, nil
}
func (blockOp) Collect(_ engine.TaskInputs, stdout []byte) (engine.Result, error) {
	return engine.Result{Captured: string(stdout)}, nil
}
func (o blockOp) Run(ctx context.Context, _ engine.OpDeps) (engine.Result, error) {
	o.started <- o.key
	select {
	case <-o.release:
	case <-ctx.Done():
		return engine.Result{}, ctx.Err()
	}
	return engine.Result{Captured: o.out}, o.err
}

func newTestTasks(t *testing.T) (*TaskManager, *Service) {
	t.Helper()
	_, svc := newRealRepo(t)
	m := NewTaskManager(taskhist.NewFileStore(t.TempDir()))
	m.sessions = agentsession.NewManager()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		m.KillAll(ctx)
		m.sessions.KillAll(ctx)
	})
	return m, svc
}

func headlessSpec(svc *Service, key string, op engine.CaptureTask) TaskSpec {
	return TaskSpec{Key: key, Kind: exttool.CatReview, Agent: "Fake", Mode: TaskHeadless, Svc: svc, Op: op}
}

func waitInfo(t *testing.T, m *TaskManager, id TaskID, what string, ok func(TaskInfo) bool) TaskInfo {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if info, found := m.Get(id); found && ok(info) {
			return info
		}
		time.Sleep(10 * time.Millisecond)
	}
	info, _ := m.Get(id)
	t.Fatalf("timed out waiting for %s: %+v", what, info)
	return info
}

func stateIs(s TaskState) func(TaskInfo) bool { return func(i TaskInfo) bool { return i.State == s } }

func recv(t *testing.T, ch chan string) string {
	t.Helper()
	select {
	case k := <-ch:
		return k
	case <-time.After(10 * time.Second):
		t.Fatal("no task started")
		return ""
	}
}

func notRecv(t *testing.T, ch chan string) {
	t.Helper()
	select {
	case k := <-ch:
		t.Fatalf("task %q started, want it queued", k)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestTasksSameKeyRunInSequence(t *testing.T) {
	t.Parallel()
	m, svc := newTestTasks(t)
	started := make(chan string, 4)
	r1, r2 := make(chan struct{}), make(chan struct{})
	a := m.Submit(headlessSpec(svc, "review — k", blockOp{started: started, release: r1, key: "a", out: "ra"}))
	b := m.Submit(headlessSpec(svc, "review — k", blockOp{started: started, release: r2, key: "b", out: "rb"}))
	if k := recv(t, started); k != "a" {
		t.Fatalf("first started = %q", k)
	}
	notRecv(t, started)
	if info, _ := m.Get(b); info.State != TaskQueued {
		t.Fatalf("b = %s, want queued behind a", info.State)
	}
	close(r1)
	waitInfo(t, m, a, "a done", stateIs(TaskDone))
	if k := recv(t, started); k != "b" {
		t.Fatalf("second started = %q", k)
	}
	close(r2)
	if info := waitInfo(t, m, b, "b done", stateIs(TaskDone)); info.Result != "rb" || info.Results != 1 {
		t.Fatalf("b = %+v", info)
	}
}

func TestTasksDifferentKeysRunInParallelWithinTheCap(t *testing.T) {
	t.Parallel()
	m, svc := newTestTasks(t)
	m.SetMaxParallel(2)
	started := make(chan string, 4)
	rel := make(chan struct{})
	defer close(rel)
	m.Submit(headlessSpec(svc, "k1", blockOp{started: started, release: rel, key: "1"}))
	m.Submit(headlessSpec(svc, "k2", blockOp{started: started, release: rel, key: "2"}))
	c := m.Submit(headlessSpec(svc, "k3", blockOp{started: started, release: rel, key: "3"}))
	recv(t, started)
	recv(t, started)
	notRecv(t, started)
	if info, _ := m.Get(c); info.State != TaskQueued {
		t.Fatalf("third = %s, want queued at the cap", info.State)
	}
}

func TestTasksMaxParallelClamp(t *testing.T) {
	t.Parallel()
	m := NewTaskManager(taskhist.NewMemStore())
	if m.Max() != 3 {
		t.Fatalf("default = %d", m.Max())
	}
	for in, want := range map[int]int{0: 1, -4: 1, 7: 7, 11: 10, 99: 10} {
		m.SetMaxParallel(in)
		if m.Max() != want {
			t.Errorf("SetMaxParallel(%d) → %d, want %d", in, m.Max(), want)
		}
	}
}

func TestTasksCancelQueuedAndRunning(t *testing.T) {
	t.Parallel()
	m, svc := newTestTasks(t)
	m.SetMaxParallel(1)
	started := make(chan string, 4)
	rel := make(chan struct{})
	defer close(rel)
	run := m.Submit(headlessSpec(svc, "k1", blockOp{started: started, release: rel, key: "run"}))
	queued := m.Submit(headlessSpec(svc, "k2", blockOp{started: started, release: rel, key: "queued"}))
	recv(t, started)
	if err := m.Cancel(queued); err != nil {
		t.Fatal(err)
	}
	waitInfo(t, m, queued, "queued cancelled", stateIs(TaskCancelled))
	if err := m.Cancel(run); err != nil {
		t.Fatal(err)
	}
	waitInfo(t, m, run, "running cancelled", stateIs(TaskCancelled))
	notRecv(t, started) // the cancelled queued task never starts
	h := m.History() // the running task's record lands before its end state shows
	if len(h) != 2 || h[0].State != "cancelled" || h[1].State != "cancelled" {
		t.Fatalf("history = %+v", h)
	}
}

func TestTasksHeadlessFailuresKeepTheTail(t *testing.T) {
	t.Parallel()
	m, svc := newTestTasks(t)
	started := make(chan string, 2)
	rel := make(chan struct{})
	close(rel)
	id := m.Submit(headlessSpec(svc, "k", blockOp{started: started, release: rel, key: "x", out: "partial output", err: errors.New("boom")}))
	info := waitInfo(t, m, id, "failed", stateIs(TaskFailed))
	if info.Err != "boom" || !strings.Contains(info.Tail, "partial output") {
		t.Fatalf("info = %+v", info)
	}
	if tail, _ := m.HistoryTail(string(id)); !strings.Contains(tail, "partial output") {
		t.Fatalf("history tail = %q", tail)
	}
	empty := m.Submit(headlessSpec(svc, "k2", blockOp{started: started, release: rel, key: "y", out: "  "}))
	waitInfo(t, m, empty, "empty result fails", stateIs(TaskFailed))
	spec := headlessSpec(svc, "k3", blockOp{started: started, release: rel, key: "z", out: ""})
	spec.ResultOptional = true
	waitInfo(t, m, m.Submit(spec), "optional result done", stateIs(TaskDone))
}

func TestTasksHeadlessRealCommandInTheSubmittingWorktree(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell command")
	}
	m, _ := newTestTasks(t)
	dirA, svcA := newRealRepo(t)
	_, _ = newRealRepo(t) // the "other" worktree the frontend moved to
	if err := os.WriteFile(filepath.Join(dirA, "n.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dirA, "add", "n.txt")
	spec := TaskSpec{
		Key: "commit message — a", Kind: exttool.CatCommitMessage, Mode: TaskHeadless, Svc: svcA,
		Op:    engine.GenerateMessage{Command: `printf 'Subject in %s\n' "$(pwd)"; echo warn >&2`, Dir: dirA},
		Parse: parseCommitMessage,
	}
	info := waitInfo(t, m, m.Submit(spec), "done", stateIs(TaskDone))
	if !strings.Contains(info.Result, filepath.Base(dirA)) {
		t.Fatalf("result = %q, want it produced in %s", info.Result, dirA)
	}
	if r, _ := m.HistoryResult(string(info.ID)); r != info.Result {
		t.Fatalf("history result = %q", r)
	}
	spec.Op = engine.GenerateMessage{Command: `echo oops >&2; exit 3`, Dir: dirA}
	failed := waitInfo(t, m, m.Submit(spec), "failed", stateIs(TaskFailed))
	if failed.ExitCode != 3 || !strings.Contains(failed.Tail, "oops") {
		t.Fatalf("failed = %+v", failed)
	}
}

func TestTasksChangedSignals(t *testing.T) {
	t.Parallel()
	m, svc := newTestTasks(t)
	started := make(chan string, 1)
	rel := make(chan struct{})
	close(rel)
	id := m.Submit(headlessSpec(svc, "k", blockOp{started: started, release: rel, key: "x", out: "r"}))
	select {
	case <-m.Changed():
	case <-time.After(5 * time.Second):
		t.Fatal("Submit did not signal Changed")
	}
	waitInfo(t, m, id, "done", stateIs(TaskDone))
	if l := m.List(); len(l) != 1 || l[0].ID != id {
		t.Fatalf("List = %+v", l)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `rtk go test ./internal/domain/ -run 'TestTasks'`
Expected: FAIL to compile — `undefined: NewTaskManager`.

- [ ] **Step 3: Implement**

`internal/domain/tasks.go`:

```go
package domain

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/taskhist"
)

type TaskID string

type TaskState string

const (
	TaskQueued      TaskState = "queued"
	TaskRunning     TaskState = "running"
	TaskResultReady TaskState = "result-ready" // interactive, ≥1 result, still running
	TaskDone        TaskState = "done"
	TaskFailed      TaskState = "failed"
	TaskCancelled   TaskState = "cancelled"
)

// Live reports a task that is queued or holds a slot.
func (s TaskState) Live() bool { return s == TaskQueued || s == TaskRunning || s == TaskResultReady }

func (s TaskState) running() bool { return s == TaskRunning || s == TaskResultReady }

// TaskRecord is a finished task in the history (frontends may not import
// internal/taskhist).
type TaskRecord = taskhist.Record

// TaskInfo is a task's live view. Results counts results so far: a
// frontend applies each new one exactly once by remembering the count it
// last saw.
type TaskInfo struct {
	ID        TaskID
	Key       string
	Kind      exttool.Category
	Agent     string
	Repo      string
	Worktree  string
	Mode      TaskMode
	State     TaskState
	Submitted time.Time
	Started   time.Time
	Ended     time.Time
	Session   SessionID // interactive: the task's session once started
	Result    string    // the latest result
	Results   int
	ExitCode  int
	Err       string
	Tail      string // headless output tail (≤ taskhist.MaxTail)
}

type task struct {
	info      TaskInfo
	spec      TaskSpec
	cancel    context.CancelFunc
	cancelled bool
}

type taskEnd struct {
	state TaskState
	exit  int
	err   string
	tail  string
}

// TaskManager runs AI tasks: FIFO per key, at most max running at once
// across every mode (ruling 5/6). Live tasks are in memory; a task that
// ends is written to the history.
type TaskManager struct {
	mu       sync.Mutex
	max      int
	tasks    []*task // submit order; ended ones trimmed to taskhist.Max
	changed  chan struct{}
	wg       sync.WaitGroup
	sessions *agentsession.Manager // nil = Sessions()

	histMu   sync.Mutex
	hist     taskhist.Store
	fallback *taskhist.MemStore // set once the store failed a write
	problem  error              // that failure, until TakeStoreProblem
}

// NewTaskManager returns a manager recording into hist, cap 3.
func NewTaskManager(hist taskhist.Store) *TaskManager {
	return &TaskManager{max: 3, hist: hist, changed: make(chan struct{}, 1)}
}

// SetMaxParallel sets the cap, clamped to 1..config.MaxParallelCap, and
// starts whatever now fits.
func (m *TaskManager) SetMaxParallel(n int) {
	n = min(max(n, 1), config.MaxParallelCap)
	m.mu.Lock()
	m.max = n
	m.pumpLocked()
	m.mu.Unlock()
	m.signal()
}

func (m *TaskManager) Max() int { m.mu.Lock(); defer m.mu.Unlock(); return m.max }

// Changed is a coalesced "something changed" signal (the Sessions pattern).
func (m *TaskManager) Changed() <-chan struct{} { return m.changed }

func (m *TaskManager) signal() {
	select {
	case m.changed <- struct{}{}:
	default:
	}
}

func (m *TaskManager) sessionMgr() *agentsession.Manager {
	if m.sessions != nil {
		return m.sessions
	}
	return Sessions()
}

// Submit queues spec and starts it when its key is free and a slot is.
func (m *TaskManager) Submit(spec TaskSpec) TaskID {
	now := time.Now()
	t := &task{spec: spec, info: TaskInfo{
		ID: TaskID(taskhist.NewID(now)), Key: spec.Key, Kind: spec.Kind, Agent: spec.Agent,
		Repo: spec.Repo, Worktree: spec.Worktree, Mode: spec.Mode, State: TaskQueued, Submitted: now,
	}}
	m.mu.Lock()
	m.tasks = append(m.tasks, t)
	m.pumpLocked()
	m.mu.Unlock()
	m.signal()
	return t.info.ID
}

// pumpLocked starts queued tasks in submit order while slots remain; a key
// that is running (or has an earlier queued task) waits.
func (m *TaskManager) pumpLocked() {
	running := 0
	busy := map[string]bool{}
	for _, t := range m.tasks {
		if t.info.State.running() {
			running++
			busy[t.spec.Key] = true
		}
	}
	for _, t := range m.tasks {
		if t.info.State != TaskQueued {
			continue
		}
		if busy[t.spec.Key] {
			continue
		}
		busy[t.spec.Key] = true // later same-key tasks queue behind this one
		if running >= m.max {
			continue
		}
		running++
		m.startLocked(t)
	}
}

func (m *TaskManager) startLocked(t *task) {
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	t.info.State = TaskRunning
	t.info.Started = time.Now()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer cancel()
		var end taskEnd
		if t.spec.Mode == TaskInteractive {
			end = m.runInteractive(ctx, t)
		} else {
			end = m.runHeadless(ctx, t)
		}
		m.finish(t, end)
	}()
}

// parse applies the spec's Parse (TrimSpace when nil).
func (t *task) parse(captured string) (string, error) {
	if t.spec.Parse == nil {
		return strings.TrimSpace(captured), nil
	}
	return t.spec.Parse(captured)
}

// setResult records a new result and signals.
func (m *TaskManager) setResult(t *task, result string) {
	m.mu.Lock()
	t.info.Result = result
	t.info.Results++
	if t.spec.Mode == TaskInteractive && t.info.State == TaskRunning {
		t.info.State = TaskResultReady
	}
	m.mu.Unlock()
	m.signal()
}

func (m *TaskManager) runHeadless(ctx context.Context, t *task) taskEnd {
	events := make(chan engine.Event, 64)
	var tail strings.Builder
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for ev := range events {
			if gl, ok := ev.(engine.GitLine); ok {
				tail.WriteString(gl.Raw + "\n")
			}
		}
	}()
	res, err := t.spec.Svc.Execute(ctx, t.spec.Op, events, nil)
	close(events)
	<-drained
	if strings.TrimSpace(res.Captured) != "" {
		tail.WriteString(res.Captured)
	}
	out := taskhist.TrimTail(tail.String())
	if ctx.Err() != nil {
		return taskEnd{state: TaskCancelled, exit: -1, tail: out}
	}
	if err != nil {
		return taskEnd{state: TaskFailed, exit: exitCodeOf(err), err: err.Error(), tail: out}
	}
	result, perr := t.parse(res.Captured)
	if perr == nil && strings.TrimSpace(result) != "" {
		m.setResult(t, result)
		return taskEnd{state: TaskDone}
	}
	if t.spec.ResultOptional && perr == nil {
		return taskEnd{state: TaskDone}
	}
	msg := "the agent produced no result"
	if perr != nil {
		msg = perr.Error()
	}
	return taskEnd{state: TaskFailed, err: msg, tail: out}
}

func exitCodeOf(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// finish ends t. The record is written BEFORE the end state becomes
// visible, so a frontend that sees the task end also finds its history.
func (m *TaskManager) finish(t *task, end taskEnd) {
	m.mu.Lock()
	info := t.info // the runner is done: no setResult can race this copy
	m.mu.Unlock()
	info.State, info.Ended = end.state, time.Now()
	info.ExitCode, info.Err, info.Tail = end.exit, end.err, end.tail
	m.record(recordOf(info), info.Result, end.tail)
	m.mu.Lock()
	t.info = info
	m.trimEndedLocked()
	m.pumpLocked()
	m.mu.Unlock()
	m.signal()
}

func recordOf(info TaskInfo) TaskRecord {
	started := info.Started
	if started.IsZero() {
		started = info.Submitted
	}
	return TaskRecord{
		ID: string(info.ID), Key: info.Key, Kind: string(info.Kind), Agent: info.Agent,
		Repo: info.Repo, Worktree: info.Worktree, Mode: string(info.Mode), State: string(info.State),
		Started: started.UTC(), Ended: info.Ended.UTC(), ExitCode: info.ExitCode, Err: info.Err,
	}
}

// trimEndedLocked keeps at most taskhist.Max ended tasks in memory.
func (m *TaskManager) trimEndedLocked() {
	ended := 0
	for _, t := range m.tasks {
		if !t.info.State.Live() {
			ended++
		}
	}
	if ended <= taskhist.Max {
		return
	}
	out := m.tasks[:0]
	for _, t := range m.tasks {
		if !t.info.State.Live() && ended > taskhist.Max {
			ended--
			continue
		}
		out = append(out, t)
	}
	m.tasks = out
}

func (m *TaskManager) find(id TaskID) *task {
	for _, t := range m.tasks {
		if t.info.ID == id {
			return t
		}
	}
	return nil
}

// Get returns a task's current view.
func (m *TaskManager) Get(id TaskID) (TaskInfo, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t := m.find(id); t != nil {
		return t.info, true
	}
	return TaskInfo{}, false
}

// List returns live tasks in submit order, then this process's ended tasks
// newest first.
func (m *TaskManager) List() []TaskInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	var live, ended []TaskInfo
	for _, t := range m.tasks {
		if t.info.State.Live() {
			live = append(live, t.info)
		} else {
			ended = append([]TaskInfo{t.info}, ended...)
		}
	}
	return append(live, ended...)
}

// Cancel removes a queued task, or stops a running one (headless: its ctx;
// interactive: its session is killed). Ended tasks are left alone.
func (m *TaskManager) Cancel(id TaskID) error {
	m.mu.Lock()
	t := m.find(id)
	if t == nil {
		m.mu.Unlock()
		return fmt.Errorf("no task %s", id)
	}
	switch {
	case t.info.State == TaskQueued:
		t.info.State = TaskCancelled
		t.info.Ended = time.Now()
		rec := recordOf(t.info)
		m.pumpLocked()
		m.mu.Unlock()
		m.record(rec, "", "")
		m.signal()
		return nil
	case t.info.State.running():
		t.cancelled = true
		cancel := t.cancel
		m.mu.Unlock()
		cancel()
		return nil
	}
	m.mu.Unlock()
	return nil
}

// KillAll cancels every queued and running task and waits (bounded by
// ctx) for the running ones to end — gg's quit path.
func (m *TaskManager) KillAll(ctx context.Context) {
	m.mu.Lock()
	var ids []TaskID
	for _, t := range m.tasks {
		if t.info.State == TaskQueued {
			ids = append(ids, t.info.ID)
		}
	}
	for _, t := range m.tasks {
		if t.info.State.running() {
			ids = append(ids, t.info.ID)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Cancel(id)
	}
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// record writes a finished task to the history; the first failed write
// switches this process to an in-memory history (spec: tasks still run,
// one notice).
func (m *TaskManager) record(r TaskRecord, result, tail string) {
	m.histMu.Lock()
	defer m.histMu.Unlock()
	if m.fallback == nil {
		err := errors.New("no history store")
		if m.hist != nil {
			if err = m.hist.Add(r, result, tail); err == nil {
				return
			}
		}
		m.fallback = taskhist.NewMemStore()
		if m.hist != nil {
			m.problem = err
		}
	}
	_ = m.fallback.Add(r, result, tail)
}

// History lists finished tasks newest first (in-memory fallback records,
// then the store's).
func (m *TaskManager) History() []TaskRecord {
	m.histMu.Lock()
	defer m.histMu.Unlock()
	var out []TaskRecord
	seen := map[string]bool{}
	if m.fallback != nil {
		mem, _ := m.fallback.List()
		for _, r := range mem {
			seen[r.ID] = true
			out = append(out, r)
		}
	}
	if m.hist != nil {
		disk, _ := m.hist.List()
		for _, r := range disk {
			if !seen[r.ID] {
				out = append(out, r)
			}
		}
	}
	return out
}

func (m *TaskManager) historyText(id string, get func(taskhist.Store, string) (string, error)) (string, error) {
	m.histMu.Lock()
	defer m.histMu.Unlock()
	if m.fallback != nil {
		if s, _ := get(m.fallback, id); s != "" {
			return s, nil
		}
	}
	if m.hist == nil {
		return "", nil
	}
	return get(m.hist, id)
}

func (m *TaskManager) HistoryResult(id string) (string, error) {
	return m.historyText(id, taskhist.Store.Result)
}

func (m *TaskManager) HistoryTail(id string) (string, error) {
	return m.historyText(id, taskhist.Store.Tail)
}

// RemoveHistory deletes a finished record (the Headless tab's x).
func (m *TaskManager) RemoveHistory(id string) error {
	m.histMu.Lock()
	defer m.histMu.Unlock()
	if m.fallback != nil {
		_ = m.fallback.Remove(id)
	}
	if m.hist == nil {
		return nil
	}
	return m.hist.Remove(id)
}
```

Add a temporary stub in the same file so it compiles before Task 7 (Task 7 replaces it):

```go
func (m *TaskManager) runInteractive(ctx context.Context, t *task) taskEnd {
	return taskEnd{state: TaskFailed, exit: -1, err: "interactive tasks are not implemented yet"}
}
```

- [ ] **Step 4: Run the tests**

Run: `rtk go test -race ./internal/domain/ -run 'TestTasks'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/domain/tasks.go internal/domain/tasks_test.go
rtk git commit -m "feat(domain): AI task scheduler — per-key FIFO, cap, headless runs, cancel, history"
```

---

### Task 7: interactive runs — session + result watcher

**Files:**
- Create: `internal/domain/task_watch.go`
- Modify: `internal/domain/tasks.go` (replace the `runInteractive` stub)
- Test: `internal/domain/task_watch_test.go`

**Interfaces:**
- Consumes: `Service.PrepareTask`, `Service.startLine` (Task 5); `m.sessionMgr()`, `m.setResult`, `taskEnd` (Task 6); `filewatch.New/Set/Events/Close`.
- Produces: `var taskResultPoll = 500 * time.Millisecond` (tests lower it), `type resultWatcher`, `func newResultWatcher(path string) *resultWatcher`.

- [ ] **Step 1: Write the failing tests**

`internal/domain/task_watch_test.go`:

```go
package domain

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/repogate"
)

// scriptOp is a CaptureTask whose Prepare hands out a shell line and a
// fresh $GG_MESSAGE_FILE — an interactive "agent" that is really sh.
type scriptOp struct{ line string }

func (scriptOp) LockMode() repogate.Mode { return repogate.Read }
func (o scriptOp) Prepare(context.Context, engine.OpDeps) (engine.TaskInputs, error) {
	f, err := os.CreateTemp("", "gg-task-test-*.txt")
	if err != nil {
		return engine.TaskInputs{}, err
	}
	f.Close()
	return engine.TaskInputs{
		Command: o.line, Env: []string{"GG_MESSAGE_FILE=" + f.Name()}, MessageFile: f.Name(),
		Cleanup: func() { os.Remove(f.Name()) },
	}, nil
}
func (scriptOp) Collect(in engine.TaskInputs, _ []byte) (engine.Result, error) {
	b, _ := os.ReadFile(in.MessageFile)
	return engine.Result{Captured: string(b)}, nil
}
func (o scriptOp) Run(context.Context, engine.OpDeps) (engine.Result, error) {
	return engine.Result{}, nil
}

func interactiveSpec(svc *Service, key, line string) TaskSpec {
	top, _ := svc.TopLevel(context.Background())
	return TaskSpec{Key: key, Kind: exttool.CatCommitMessage, Agent: "Fake", Mode: TaskInteractive,
		Svc: svc, Worktree: top, Cols: 80, Rows: 24, Op: scriptOp{line: line}}
}

func skipOnWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell scripts")
	}
}

func TestInteractiveTaskPicksUpEachDistinctWrite(t *testing.T) {
	skipOnWindows(t) // not parallel: it lowers the package poll interval
	defer func(p time.Duration) { taskResultPoll = p }(taskResultPoll)
	taskResultPoll = 50 * time.Millisecond
	m, svc := newTestTasks(t)
	// "one", then "two", then "two" again (same content: no new result),
	// then a long wait — the agent stays up.
	id := m.Submit(interactiveSpec(svc, "k", `printf one > "$GG_MESSAGE_FILE"; sleep 1; printf two > "$GG_MESSAGE_FILE"; sleep 1; printf two > "$GG_MESSAGE_FILE"; sleep 30`))
	info := waitInfo(t, m, id, "two results", func(i TaskInfo) bool { return i.Results == 2 })
	if info.State != TaskResultReady || info.Result != "two" || info.Session == "" {
		t.Fatalf("info = %+v", info)
	}
	time.Sleep(1500 * time.Millisecond)
	if info, _ = m.Get(id); info.Results != 2 {
		t.Fatalf("an identical rewrite raised another result: %+v", info)
	}
	if err := m.Cancel(id); err != nil {
		t.Fatal(err)
	}
	waitInfo(t, m, id, "done after a result", stateIs(TaskDone))
}

func TestInteractiveTaskEndStates(t *testing.T) {
	skipOnWindows(t)
	defer func(p time.Duration) { taskResultPoll = p }(taskResultPoll)
	taskResultPoll = 50 * time.Millisecond
	m, svc := newTestTasks(t)
	noResult := m.Submit(interactiveSpec(svc, "a", `exit 0`))
	waitInfo(t, m, noResult, "failed without a result", stateIs(TaskFailed))

	spec := interactiveSpec(svc, "b", `exit 0`)
	spec.ResultOptional = true
	waitInfo(t, m, m.Submit(spec), "optional: done", stateIs(TaskDone))

	lastWrite := m.Submit(interactiveSpec(svc, "c", `printf final > "$GG_MESSAGE_FILE"; exit 0`))
	if info := waitInfo(t, m, lastWrite, "done", stateIs(TaskDone)); info.Result != "final" {
		t.Fatalf("a write just before exit was lost: %+v", info)
	}

	killed := m.Submit(interactiveSpec(svc, "d", `sleep 30`))
	waitInfo(t, m, killed, "running", func(i TaskInfo) bool { return i.Session != "" })
	if err := m.Cancel(killed); err != nil {
		t.Fatal(err)
	}
	waitInfo(t, m, killed, "cancelled before a result", stateIs(TaskCancelled))

	crashed := m.Submit(interactiveSpec(svc, "e", `exit 3`))
	if info := waitInfo(t, m, crashed, "failed", stateIs(TaskFailed)); info.ExitCode != 3 {
		t.Fatalf("exit code = %d", info.ExitCode)
	}
}

func TestResultWatcherStableRead(t *testing.T) {
	t.Parallel()
	f, err := os.CreateTemp(t.TempDir(), "msg-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	w := newResultWatcher(f.Name())
	defer w.close()
	if w.changed() {
		t.Fatal("an empty file is not a result")
	}
	os.WriteFile(f.Name(), []byte("a"), 0o644)
	if !w.changed() {
		t.Fatal("new content must be a result")
	}
	if w.changed() {
		t.Fatal("unchanged content must not be a second result")
	}
	if newResultWatcher("").wake() != nil {
		t.Fatal("no message file = a watcher that never wakes")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `rtk go test ./internal/domain/ -run 'InteractiveTask|ResultWatcher'`
Expected: FAIL to compile — `undefined: taskResultPoll`, `newResultWatcher`.

- [ ] **Step 3: Implement**

`internal/domain/task_watch.go`:

```go
package domain

import (
	"bytes"
	"crypto/sha256"
	"os"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/filewatch"
)

// taskResultPoll is the watcher's polling period: fsnotify is only a
// wake-up (it misses writes on some mounts), the poll is the truth.
var taskResultPoll = 500 * time.Millisecond

// resultStableDelay is how long a new content must stay the same before it
// counts: a poll can land mid-write.
const resultStableDelay = 100 * time.Millisecond

// resultWatcher reports when one file holds new, non-empty content.
type resultWatcher struct {
	path  string
	wakeC chan struct{}
	stop  chan struct{}
	once  sync.Once
	fw    *filewatch.Watcher
	last  [32]byte
	seen  bool
}

// newResultWatcher watches path; "" gives a watcher that never wakes.
func newResultWatcher(path string) *resultWatcher {
	w := &resultWatcher{path: path, stop: make(chan struct{})}
	if path == "" {
		return w
	}
	w.wakeC = make(chan struct{}, 1)
	var fsEvents <-chan string
	if fw, err := filewatch.New(50 * time.Millisecond); err == nil {
		fw.Set([]string{path})
		w.fw = fw
		fsEvents = fw.Events()
	}
	go func() {
		tick := time.NewTicker(taskResultPoll)
		defer tick.Stop()
		for {
			select {
			case <-w.stop:
				return
			case <-tick.C:
			case _, ok := <-fsEvents:
				if !ok {
					fsEvents = nil
					continue
				}
			}
			select {
			case w.wakeC <- struct{}{}:
			default:
			}
		}
	}()
	return w
}

// wake fires when the file may have changed (nil when there is no file).
func (w *resultWatcher) wake() <-chan struct{} { return w.wakeC }

// changed reports new non-empty content, stable across resultStableDelay,
// different from the last reported content.
func (w *resultWatcher) changed() bool {
	if w.path == "" {
		return false
	}
	b, err := os.ReadFile(w.path)
	if err != nil || len(bytes.TrimSpace(b)) == 0 {
		return false
	}
	sum := sha256.Sum256(b)
	if w.seen && sum == w.last {
		return false
	}
	time.Sleep(resultStableDelay)
	if b2, err := os.ReadFile(w.path); err != nil || !bytes.Equal(b, b2) {
		return false // still being written; the next wake re-reads
	}
	w.last, w.seen = sum, true
	return true
}

func (w *resultWatcher) close() {
	w.once.Do(func() {
		close(w.stop)
		if w.fw != nil {
			w.fw.Close()
		}
	})
}
```

In `internal/domain/tasks.go` replace the stub:

```go
// kindLabel names a kind in a session label ("Claude Code · commit message").
func kindLabel(k exttool.Category) string {
	switch k {
	case exttool.CatCommitMessage:
		return "commit message"
	case exttool.CatConflict:
		return "resolve conflict"
	case exttool.CatConflictComplete:
		return "resolve & complete"
	}
	return string(k)
}

// runInteractive prepares the inputs, starts the agent session and turns
// every distinct write of $GG_MESSAGE_FILE into a result until the session
// ends. Cancel kills the session. End: ≥1 result → done; killed before a
// result → cancelled; exit 0 without a result → done only when
// ResultOptional, else failed.
func (m *TaskManager) runInteractive(ctx context.Context, t *task) taskEnd {
	in, err := t.spec.Svc.PrepareTask(ctx, t.spec.Op)
	if err != nil {
		return taskEnd{state: TaskFailed, exit: -1, err: err.Error()}
	}
	defer in.Cleanup()
	env := append(append([]string{}, t.spec.Env...), in.Env...)
	label := t.spec.Agent + " · " + kindLabel(t.spec.Kind)
	sess, err := t.spec.Svc.startLine(ctx, m.sessionMgr(), label, t.spec.AgentID, in.Command,
		t.spec.Worktree, t.spec.Cwd, t.spec.Cols, t.spec.Rows, env)
	if err != nil {
		return taskEnd{state: TaskFailed, exit: -1, err: err.Error()}
	}
	m.mu.Lock()
	t.info.Session = sess.Info().ID
	m.mu.Unlock()
	m.signal()

	w := newResultWatcher(in.MessageFile)
	defer w.close()
	results := 0
	check := func() {
		if !w.changed() {
			return
		}
		res, _ := t.spec.Op.Collect(in, nil)
		out, perr := t.parse(res.Captured)
		if perr != nil || strings.TrimSpace(out) == "" {
			return
		}
		results++
		m.setResult(t, out)
	}
	stop := ctx.Done()
	for {
		select {
		case <-w.wake():
			check()
		case <-stop:
			stop = nil
			_ = m.sessionMgr().Kill(sess.Info().ID)
		case <-sess.Done():
			check() // a write just before exit
			info := sess.Info()
			switch {
			case results > 0:
				return taskEnd{state: TaskDone, exit: info.ExitCode}
			case ctx.Err() != nil:
				return taskEnd{state: TaskCancelled, exit: info.ExitCode}
			case t.spec.ResultOptional && info.ExitCode == 0:
				return taskEnd{state: TaskDone}
			}
			return taskEnd{state: TaskFailed, exit: info.ExitCode, err: "the agent ended without a result"}
		}
	}
}
```

Delete the `t.cancelled` field and its assignment in `Cancel` if the compiler reports it unused (cancellation is read from `ctx.Err()`).

- [ ] **Step 4: Run the tests**

Run: `rtk go test -race ./internal/domain/ -run 'InteractiveTask|ResultWatcher|TestTasks'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/domain/task_watch.go internal/domain/task_watch_test.go internal/domain/tasks.go
rtk git commit -m "feat(domain): interactive AI tasks — agent session plus a result watcher"
```

---

### Task 8: the global manager, live counts, history fallback, docs

**Files:**
- Modify: `internal/domain/tasks.go`
- Test: `internal/domain/tasks_test.go` (append)
- Modify: `CLAUDE.md` (package map row), `docs/CLAUDE-details.md`, `CHANGELOG.md`

**Interfaces:**
- Produces: `func Tasks() *TaskManager`, `func UseTaskManager(m *TaskManager) func()`,
  `func (m *TaskManager) Live() int`, `type TaskLoad struct { SameKey, Running, Max int }`,
  `func (m *TaskManager) Load(key string) TaskLoad`, `func (m *TaskManager) TakeStoreProblem() error`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/domain/tasks_test.go`:

```go
func TestTasksLiveAndLoad(t *testing.T) {
	t.Parallel()
	m, svc := newTestTasks(t)
	m.SetMaxParallel(1)
	started := make(chan string, 4)
	rel := make(chan struct{})
	defer close(rel)
	m.Submit(headlessSpec(svc, "k", blockOp{started: started, release: rel, key: "1"}))
	m.Submit(headlessSpec(svc, "k", blockOp{started: started, release: rel, key: "2"}))
	recv(t, started)
	if m.Live() != 2 {
		t.Fatalf("Live = %d, want 2", m.Live())
	}
	if l := m.Load("k"); l.SameKey != 2 || l.Running != 1 || l.Max != 1 {
		t.Fatalf("Load(k) = %+v", l)
	}
	if l := m.Load("other"); l.SameKey != 0 || l.Running != 1 {
		t.Fatalf("Load(other) = %+v", l)
	}
}

func TestTasksHistoryFallsBackToMemory(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "plain")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, svc := newRealRepo(t)
	m := NewTaskManager(taskhist.NewFileStore(filepath.Join(file, "sub"))) // unwritable
	started := make(chan string, 2)
	rel := make(chan struct{})
	close(rel)
	id := m.Submit(headlessSpec(svc, "k", blockOp{started: started, release: rel, key: "x", out: "r"}))
	waitInfo(t, m, id, "done", stateIs(TaskDone))
	deadline := time.Now().Add(5 * time.Second)
	for len(m.History()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if h := m.History(); len(h) != 1 || h[0].ID != string(id) {
		t.Fatalf("history = %+v", h)
	}
	if r, _ := m.HistoryResult(string(id)); r != "r" {
		t.Fatalf("result = %q", r)
	}
	if m.TakeStoreProblem() == nil {
		t.Fatal("the failed store must be reported once")
	}
	if m.TakeStoreProblem() != nil {
		t.Fatal("…and only once")
	}
}

func TestTasksGlobalManager(t *testing.T) {
	m := NewTaskManager(taskhist.NewMemStore())
	restore := UseTaskManager(m)
	if Tasks() != m {
		t.Fatal("UseTaskManager did not install m")
	}
	restore()
	if Tasks() == m {
		t.Fatal("restore did not put the previous manager back")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `rtk go test ./internal/domain/ -run 'LiveAndLoad|FallsBackToMemory|GlobalManager'`
Expected: FAIL to compile — `m.Live undefined`, `Tasks` undefined.

- [ ] **Step 3: Implement** (append to `internal/domain/tasks.go`)

```go
var (
	tasksMu  sync.Mutex
	tasksMgr *TaskManager
)

// Tasks is the process-global AI-task manager. Like Sessions() it lives
// outside every Service: the TUI reopens its Service on each worktree/repo
// switch while tasks keep running.
func Tasks() *TaskManager {
	tasksMu.Lock()
	defer tasksMu.Unlock()
	if tasksMgr == nil {
		var hist taskhist.Store
		if root := stateBaseDir("tasks"); root != "" {
			hist = taskhist.NewFileStore(root)
		}
		tasksMgr = NewTaskManager(hist)
	}
	return tasksMgr
}

// UseTaskManager installs m as the global manager (tests) and returns a
// func restoring the previous one.
func UseTaskManager(m *TaskManager) func() {
	tasksMu.Lock()
	prev := tasksMgr
	tasksMgr = m
	tasksMu.Unlock()
	return func() {
		tasksMu.Lock()
		tasksMgr = prev
		tasksMu.Unlock()
	}
}

// Live counts queued and running tasks (the quit guard's count).
func (m *TaskManager) Live() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, t := range m.tasks {
		if t.info.State.Live() {
			n++
		}
	}
	return n
}

// TaskLoad is what a new task with a key would wait for: SameKey live
// tasks with that key ahead of it, Running tasks holding slots, of Max.
type TaskLoad struct {
	SameKey int
	Running int
	Max     int
}

// Load backs the launch dialog's wait line.
func (m *TaskManager) Load(key string) TaskLoad {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := TaskLoad{Max: m.max}
	for _, t := range m.tasks {
		if t.info.State.running() {
			l.Running++
		}
		if t.info.State.Live() && t.spec.Key == key {
			l.SameKey++
		}
	}
	return l
}

// TakeStoreProblem returns the history store's failure once (the frontend
// shows one notice), nil afterwards or when there was none.
func (m *TaskManager) TakeStoreProblem() error {
	m.histMu.Lock()
	defer m.histMu.Unlock()
	err := m.problem
	m.problem = nil
	return err
}
```

- [ ] **Step 4: Run the domain package and the archtest**

Run: `rtk go test -race ./internal/domain/ ./internal/archtest/`
Expected: PASS.

- [ ] **Step 5: Docs**

- `CLAUDE.md` package map — add after the `linkhist` row:
  `| \`taskhist\`   | Machine-local history of AI tasks: the last 50 records across all repos + one result and one output-tail file each (TOML + the shared file lock under XDG state). Owned by \`domain\` (\`Tasks()\`); frontends never import it. |`
  and extend the `domain` row's sentence list with "the process-global AI-task scheduler `Tasks()` (per-key FIFO, `[tasks] max_parallel` cap, headless via `Execute`, interactive via a session + `$GG_MESSAGE_FILE` watcher)".
  Extend the `engine` row: "AI capture ops (`GenerateMessage`, `ReviewChanges`, `CompleteConflict`, `ResolveConflict`) are `CaptureTask`s — `Prepare`/`Collect`, `Run` = prepare → capture → collect".
  Extend the `exttool` row's category/mode mention with `interactive`.
- `docs/CLAUDE-details.md` — new section "AI tasks — the task core (plan 2)": `TaskInputs.Env` is a delta; `CaptureTask`; keys table (ruling 6 + the working-changes review key); scheduler rules (FIFO per key, cap counts every mode, `SetMaxParallel` clamp); states and the end table incl. `ResultOptional` for conflict kinds; interactive watcher (fsnotify wake + poll, stable read, content-hash dedup, a last read on exit); history (written at end only, `<id>.result`/`<id>.tail`, in-memory fallback + `TakeStoreProblem`); what counts as interactive (`TaskModeOf`); `EnsureInteractiveCommands`; the laneToolCommands filter until plan 3.
- `CHANGELOG.md` — under Unreleased, "Added": "AI task core (no UI yet): a scheduler for commit-message, review and conflict agents — headless (queued, `[tasks] max_parallel`, default 3, max 10) or interactive (an agent session whose every `$GG_MESSAGE_FILE` write is the latest result); a 50-record task history; interactive catalogue rows for Claude, Codex, Junie and Antigravity (`mode = \"interactive\"`)."

- [ ] **Step 6: Commit**

```bash
rtk git add internal/domain/tasks.go internal/domain/tasks_test.go CLAUDE.md docs/CLAUDE-details.md CHANGELOG.md
rtk git commit -m "feat(domain): global task manager, live counts, history fallback; docs"
```

- [ ] **Step 7: Race gate**

Run: `./test.sh race > /tmp/claude-1000/race.log 2>&1; tail -30 /tmp/claude-1000/race.log`
Expected: every stage green. Then ask the user before merging.
