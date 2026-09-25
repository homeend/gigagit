# AI tasks — plan 3: the TUI — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans (this repo: NEVER subagents — the session that wrote the plan executes it). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every AI action in the TUI (commit-box generate, review, conflict resolve, resolve & complete) opens a launch dialog that runs it as an interactive agent (background or foreground) or as a queued headless task. Results are applied as they arrive, and tasks are listed in a Headless tab of the `ctrl+\` popup and as ◆ rows under Worktrees.

**Architecture:** The TUI consumes plan 2's process-global `domain.Tasks()`: one `waitTasksCmd` consumer re-armed on each `tasksChangedMsg`, and a per-process tracker (`taskTrack`) that remembers what it has applied. One dialog layer (`taskLaunchPopup`) serves all four kinds: it prepares asynchronously (the task key, plus `EnsureInteractiveCommands`), picks agent × mode × variant, runs the existing hash approval, and submits a kind-built `TaskSpec`. The old headless lanes on the TUI side are retired (numbered choosers, `genMessageCmd`, `reviewRunCmd`, `reviewRunning` and the blink, the conflict capture branch). The CLI and web keep `domain.ReviewReport` and `CompleteConflictReport`.

**Tech Stack:** Go 1.26, Bubble Tea v1 with a value-receiver `Model`, lipgloss, gg's `i18n` (four bundles), `promptstate`.

**Spec:** `docs/superpowers/specs/2026-09-24-ai-tasks-in-sessions-design.md` (rulings 1–9, Architecture §F). Plan 2 (the API used here): `docs/superpowers/plans/2026-09-25-ai-tasks-plan-2-task-core.md`.

## Decisions for your review (the plan's own rulings — veto any before execution)

1. **Retired on the TUI side:**
   - `laneToolCommands`;
   - the numbered choosers (commit box, review lane);
   - the commit box's pre-dispatch `confirming` gate (ask-before-replace moves to when the result is applied, as the spec says);
   - `genMessageCmd` and `genCancel`;
   - `reviewLane`, `reviewRunCmd`, `reviewRunning`, `reviewGen`, the review blink and its "a review is in progress" refusals;
   - `cancelReview` on a repo switch (tasks are process-global and keep their `Svc`, so a switch no longer cancels them).

   The blinking `⟳ reviewing …` segment becomes a `⟳ N AI tasks` segment for live tasks.
2. **Foreground from the commit box closes the box** (the docked console only gets keys when no layer is on top). A commit-message result that arrives while the box is closed is kept as the worktree's **pending message**: the next `c` opens the box already filled, with a notice saying where the text came from. Background mode keeps the box open and fills it as results arrive.
3. **A result is applied only where the user can see it; otherwise a notice.**
   - Commit message → the open commit box of the same checkout: filled at once when the box is empty, with ask-before-replace when it has text.
   - Review → the report viewer (saved in the reviews dir as today).
   - Conflict kinds → the overview viewer, plus a status refresh.

   The result waits behind a sticky notice (`… ready — ctrl+\`) when:
   - an agent console is focused;
   - the conflict process owns the screen;
   - the task belongs to another checkout.

   A result for another checkout is never applied here (`domain.SameCheckout`).
4. **Headless from the commit box:** the box shows its spinner tied to the task (`queued…` / `generating…`). `esc` cancels the task (the same meaning esc has today); `ctrl+b` sends the run to the background (the box closes and the result becomes the pending message).
5. **Conflict window `t`:** the picker keeps the per-file mergetool rows and gains two rows, "Resolve with an agent…" and "Resolve & complete with an agent…". Either one closes the conflict process first (`m.proc` preempts layer keys) and opens the dialog; `esc` in the dialog reopens the conflict window. After the run starts, the ⏸ segment and `x` lead back in, as after `L`.
6. **The dialog's variants axis:** `←/→` picks the agent; the rows are mode × command (e.g. *Interactive, background — Claude Code (interactive, yolo)*), and `↑/↓` moves between them. A mode the agent has no command for shows one greyed row. A command whose program is not on `PATH` is marked "not found", and enter refuses it. Commands with `<user:…>` tokens are not offered, and conflict kinds honour `when_op`.
7. **The Headless tab lists every task** (headless and interactive), so an interactive task's latest result is reachable too:
   - live tasks first, then the history;
   - `enter` opens the result viewer when there is a result;
   - otherwise `enter` opens a running interactive task's console, or shows a failed task's output tail.

   The viewer's `a` (apply) is offered only for a commit message, and only in its own checkout. `y` copies.
8. **◆ rows under Worktrees = live headless tasks only.** A finished task leaves the Worktrees list; its result is in the tab, and its notice has already been raised.
9. **Test seam:** TUI task tests swap the global manager with `domain.UseTaskManager(domain.NewTaskManager(nil))` in **serial** tests (the `startTestSession` pattern). Parallel tests run only after every serial top-level test has finished.
10. **Dialog and Headless-tab hints stay inside the popup**, like every other overlay popup (`sessionsPopup`, `agentStartPopup`). The 2026-09-25 bottom-bar rule applies to boxes docked in the main layout.

## Global Constraints

- **NEVER use subagents.** Work only in `.claude/worktrees/ai-tasks-tui` (branch `feat/ai-tasks-tui`). Never `git push`. Merge only when the user asks.
- `internal/tui` never imports `internal/git`, `internal/taskhist` or `internal/engine` for new code (archtest). Everything goes through `domain`.
- Every user-visible TUI string goes through `i18n.T` with a literal key, in all four bundles (`internal/i18n/lang/{ja,ko,ru,zh}.toml`), added in the SAME task that introduces the key (the AST gates fail otherwise; see the `adding-translations` skill).
- Paths in rows and titles are middle-elided (`elidePath` / `winRow{elide:true}`). Task keys hold worktree names, not paths, so `truncate` is right for them.
- A new footer binding needs a help row (`TestHelpFooterCoverage`).
- Tests use real git temp repos (`newTestModel`) and `sh` children. Tests that swap `domain.UseTaskManager` or `UseSessionManager` are serial and skip on Windows.
- Spec ruling 5: the task cap is `clamp([tasks] max_parallel, 1, 10)`. Ruling 6: task key = identity, and same key = sequential.
- Commit messages end with the two trailers from the session's attribution reminder.

## Review Focus

1. **A result arrives while the commit box is in a sub-state** (the approval box or ask-before-replace): it must not clobber the open question. The newest result queues as the box's `offer`.
2. **A task's worktree is removed, or gg switches repo, mid-run.** The task finishes with its own `Svc`, and the result shows as a notice, never applied to the new checkout.
3. **`k k` on a queued task while its same-key predecessor finishes:** the cancelled task must not start (the pump skips cancelled tasks). Tested in the tab task.
4. **The history store on a read-only directory:** tasks still run, and `TakeStoreProblem` shows ONE sticky notice.
5. **80-column terminal with a long review key and agent name:** the dialog title and tab rows truncate and never wrap the frame.

---

## File structure

| File | Responsibility |
|------|----------------|
| `internal/domain/tasks.go` (modify) | cancel during start → cancelled |
| `internal/domain/task_kinds.go` (modify) | `(*Service).TaskKey` |
| `internal/domain/review.go` (modify) | `(*Service).SaveReviewReport` |
| `internal/promptstate/tasklaunch.go` (create) | last dialog choice per kind |
| `internal/tui/task_track.go` (create) | `taskTrack`, `waitTasksCmd`, `onTasksChanged`, `applyTasksConfig`, notices |
| `internal/tui/task_launch_popup.go` (create) | the launch dialog + submit |
| `internal/tui/task_apply.go` (create) | per-kind result application |
| `internal/tui/commit_generate.go` (rewrite) | ctrl+g → dialog; box spinner tied to a task; pending message |
| `internal/tui/review.go` (modify) | menu rows → dialog; lane removed |
| `internal/tui/conflict_process.go` (modify) | `t` picker agent rows → dialog; capture branch removed |
| `internal/tui/sessions_popup.go` (modify) + `task_tab.go` (create) | the Headless tab |
| `internal/tui/review_view.go` (modify) | `y` copy, optional `a` apply, `e` only with a path |
| `internal/tui/worktree_sessions.go`, `view.go` (modify) | ◆ rows |
| `internal/tui/quit_guard.go`, `run.go` (modify) | tasks in the quit guard |
| `internal/tui/tools.go` (modify) | drop `laneToolCommands` |
| `internal/tui/help.go`, `footer.go` (modify) | rows/bindings |
| docs | CHANGELOG, README, CLAUDE-details, CLAUDE.md `tui` row |

---

### Task 1: Domain additions — cancel-during-start, `TaskKey`, `SaveReviewReport`

**Files:**
- Modify: `internal/domain/tasks.go` (`runInteractive`)
- Modify: `internal/domain/task_kinds.go` (add `TaskKey`)
- Modify: `internal/domain/review.go` (add `SaveReviewReport`)
- Test: `internal/domain/tasks_test.go`, `internal/domain/task_kinds_test.go`, `internal/domain/review_test.go`

**Interfaces:**
- Produces: `func (s *Service) TaskKey(ctx context.Context, kind exttool.Category, target ReviewTarget) (string, error)`. `target` is used only for `CatReview`.
- Produces: `func (s *Service) SaveReviewReport(ctx context.Context, label, content string, now time.Time) (string, error)`.

- [ ] **Step 1: Failing tests**

In `tasks_test.go`:

```go
// prepBlockOp's Prepare waits for its ctx: a cancel that lands while an
// interactive task is still preparing.
type prepBlockOp struct{ entered chan struct{} }

func (prepBlockOp) LockMode() repogate.Mode { return repogate.Read }
func (o prepBlockOp) Prepare(ctx context.Context, _ engine.OpDeps) (engine.TaskInputs, error) {
	close(o.entered)
	<-ctx.Done()
	return engine.TaskInputs{}, ctx.Err()
}
func (prepBlockOp) Collect(engine.TaskInputs, []byte) (engine.Result, error) { return engine.Result{}, nil }
func (prepBlockOp) Run(context.Context, engine.OpDeps) (engine.Result, error) { return engine.Result{}, nil }

func TestTasksCancelDuringStartIsCancelled(t *testing.T) {
	m, svc := newTestTasks(t)
	op := prepBlockOp{entered: make(chan struct{})}
	id := m.Submit(TaskSpec{Key: "k", Kind: exttool.CatCommitMessage, Mode: TaskInteractive, Svc: svc, Op: op})
	<-op.entered
	if err := m.Cancel(id); err != nil {
		t.Fatal(err)
	}
	info := waitTaskEnd(t, m, id)
	if info.State != TaskCancelled {
		t.Fatalf("state = %s, want cancelled (err %q)", info.State, info.Err)
	}
}
```

(`waitTaskEnd` exists in `tasks_test.go` from plan 2. If it has another name, use the existing poll-until-not-Live helper.)

In `task_kinds_test.go`:

```go
func TestTaskKeyMatchesBuilders(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	ctx := context.Background()
	top, _ := svc.TopLevel(ctx)
	head, _ := svc.RevParse(ctx, "HEAD")
	got, err := svc.TaskKey(ctx, exttool.CatCommitMessage, ReviewTarget{})
	if err != nil || got != CommitMessageKey(top, head) {
		t.Fatalf("commit key = %q, %v", got, err)
	}
	rt := WorkingReviewTarget()
	if got, _ := svc.TaskKey(ctx, exttool.CatReview, rt); got != ReviewKey(top, rt) {
		t.Fatalf("review key = %q", got)
	}
	if _, err := svc.TaskKey(ctx, exttool.CatConflict, ReviewTarget{}); err == nil {
		t.Fatal("conflict key without a paused op must fail")
	}
}
```

In `review_test.go`:

```go
func TestSaveReviewReportWritesUnderReviews(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	_, svc := newRealRepo(t)
	p, err := svc.SaveReviewReport(context.Background(), "working changes", "# ok\n", time.Date(2026, 9, 25, 10, 4, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "# ok\n" || !strings.Contains(p, "reviews") || !strings.HasSuffix(p, "10-04-working-changes.md") {
		t.Fatalf("path %q content %q", p, b)
	}
}
```

(Check how the existing review tests isolate the state dir, and copy that: `t.Setenv` forbids `t.Parallel`.)

- [ ] **Step 2: Run them and watch them fail**

Run: `rtk go test ./internal/domain -run 'TestTasksCancelDuringStartIsCancelled|TestTaskKeyMatchesBuilders|TestSaveReviewReportWritesUnderReviews' -count=1`
Expected: the cancel test FAILS with `state = failed`; the other two fail to compile (`svc.TaskKey` / `svc.SaveReviewReport` undefined).

- [ ] **Step 3: Implement**

In `runInteractive`, change both early returns:

```go
	in, err := t.spec.Svc.PrepareTask(ctx, t.spec.Op)
	if err != nil {
		if ctx.Err() != nil {
			return taskEnd{state: TaskCancelled, exit: -1}
		}
		return taskEnd{state: TaskFailed, exit: -1, err: err.Error()}
	}
	…
	if err != nil { // startLine
		if ctx.Err() != nil {
			return taskEnd{state: TaskCancelled, exit: -1}
		}
		return taskEnd{state: TaskFailed, exit: -1, err: err.Error()}
	}
```

In `task_kinds.go`:

```go
// TaskKey is the key a task of kind would get now (the launch dialog's
// title and wait line) — the same computation the kind builders use.
func (s *Service) TaskKey(ctx context.Context, kind exttool.Category, target ReviewTarget) (string, error) {
	top, err := s.TopLevel(ctx)
	if err != nil {
		return "", err
	}
	if kind == exttool.CatReview {
		return ReviewKey(top, target), nil
	}
	head, err := s.RevParse(ctx, "HEAD")
	if err != nil {
		return "", err
	}
	switch kind {
	case exttool.CatCommitMessage:
		return CommitMessageKey(top, head), nil
	case exttool.CatConflict, exttool.CatConflictComplete:
		st, err := s.Status(ctx)
		if err != nil {
			return "", err
		}
		cs := s.Conflict(ctx, st)
		if cs.Op == "" {
			return "", errors.New("no paused operation to resolve")
		}
		if kind == exttool.CatConflictComplete {
			return CompleteKey(top, cs.Op, head), nil
		}
		return ConflictKey(top, cs.Op, head), nil
	}
	return "", fmt.Errorf("no task kind %q", kind)
}
```

In `review.go`:

```go
// SaveReviewReport persists a review produced outside ReviewReport (an AI
// task's result) where ReviewReport keeps its reports, and returns the path.
func (s *Service) SaveReviewReport(ctx context.Context, label, content string, now time.Time) (string, error) {
	return s.writeReviewReport(ctx, label, content, now)
}
```

- [ ] **Step 4: Run them and watch them pass**

Run: the Step 2 command. Expected: PASS.
Run: `rtk go test ./internal/domain -count=1`. Expected: ok.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/domain && rtk git commit -m "feat(domain): TaskKey, SaveReviewReport; a cancel during task start ends cancelled"
```

---

### Task 2: promptstate — the last launch choice per kind

**Files:**
- Create: `internal/promptstate/tasklaunch.go`
- Modify: `internal/promptstate/file_store.go` (`records` field), `internal/promptstate/store.go` (interface)
- Test: `internal/promptstate/tasklaunch_test.go`

**Interfaces:**
- Produces:

```go
type TaskLaunch struct {
	Agent   string `toml:"agent"`   // TaskChoice identity: "id:<tool id>" or "name:<command name>"
	Mode    string `toml:"mode"`    // "background" | "foreground" | "headless"
	Command string `toml:"command"` // the command's Name (picks the variant)
}
func (fs *FileStore) TaskLaunchChoice(kind string) (TaskLaunch, bool)
func (fs *FileStore) SetTaskLaunchChoice(kind string, c TaskLaunch) error
```

  Both are also added to the `Store` interface. `FileStore` is its only implementer (checked with grep).

- [ ] **Step 1: Failing test**

```go
package promptstate

import (
	"path/filepath"
	"testing"
)

func TestTaskLaunchChoiceRoundTrip(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	if _, ok := fs.TaskLaunchChoice("review"); ok {
		t.Fatal("empty store must have no choice")
	}
	want := TaskLaunch{Agent: "id:claude", Mode: "background", Command: "Claude Code (interactive)"}
	if err := fs.SetTaskLaunchChoice("review", want); err != nil {
		t.Fatal(err)
	}
	if err := fs.SuppressPrompt("x"); err != nil { // a sibling write keeps it
		t.Fatal(err)
	}
	got, ok := fs.TaskLaunchChoice("review")
	if !ok || got != want {
		t.Fatalf("got %+v %v", got, ok)
	}
	if _, ok := fs.TaskLaunchChoice("commit_message"); ok {
		t.Fatal("choices are per kind")
	}
}
```

- [ ] **Step 2: Run it and watch it fail** — `rtk go test ./internal/promptstate -run TestTaskLaunchChoiceRoundTrip -count=1`. Expected: compile error, `TaskLaunchChoice` undefined.

- [ ] **Step 3: Implement**

`records` gains `TaskLaunch map[string]TaskLaunch \`toml:"task_launch,omitempty"\``. Add `tasklaunch.go`:

```go
package promptstate

// TaskLaunch is the launch dialog's last choice for one AI task kind
// (spec ruling 1: the dialog preselects it; enter repeats it).
type TaskLaunch struct {
	Agent   string `toml:"agent"`
	Mode    string `toml:"mode"`
	Command string `toml:"command"`
}

// TaskLaunchChoice returns the remembered choice for kind.
func (fs *FileStore) TaskLaunchChoice(kind string) (TaskLaunch, bool) {
	c, ok := fs.read().TaskLaunch[kind]
	return c, ok
}

// SetTaskLaunchChoice remembers c for kind.
func (fs *FileStore) SetTaskLaunchChoice(kind string, c TaskLaunch) error {
	r := fs.read()
	if r.TaskLaunch == nil {
		r.TaskLaunch = map[string]TaskLaunch{}
	}
	r.TaskLaunch[kind] = c
	return fs.write(r)
}
```

Add both methods to `Store` in `store.go`, with one-line doc comments.

- [ ] **Step 4: Pass** — the same command, then `rtk go test ./internal/promptstate ./internal/tui -run XXX -count=1` (build check). Expected: ok.

- [ ] **Step 5: Commit** — `rtk git add internal/promptstate && rtk git commit -m "feat(promptstate): remember the AI-task launch choice per kind"`

---

### Task 3: TUI task plumbing — tracker, change loop, config cap, notices

**Files:**
- Create: `internal/tui/task_track.go`
- Modify: `internal/tui/model.go`:
  - the `taskTrack *taskTrack` field + init in `New`;
  - `Init()` adds `waitTasksCmd()`;
  - dispatch `tasksChangedMsg`;
  - call `applyTasksConfig` beside the two `applyBranchFilterConfig` calls in the `configReadyMsg` / `dataLoadedMsg` cases.
- Modify: `internal/tui/run.go` (call `applyTasksConfig` after `applyBranchFilterConfig`)
- Test: `internal/tui/task_track_test.go`

**Interfaces:**
- Consumes: `domain.Tasks()`, `TaskInfo{ID, Key, Kind, Worktree, Mode, State, Session, Result, Results, Err}`, `TakeStoreProblem`, `SetMaxParallel`, `config.TasksConfig.Parallel()`.
- Produces:

```go
type taskTrack struct {
	seen    map[domain.TaskID]int    // results applied so far
	ended   map[domain.TaskID]bool   // end already reported
	inbox   map[domain.TaskID]string // GG_INBOX handed to an interactive task
	fg      map[domain.TaskID]bool   // foreground: open its console once the session exists
	removed map[domain.TaskID]bool   // x'd in the Headless tab (Task 8)
	capWarn string                   // the last max_parallel warning shown ("" = none)
}
type tasksChangedMsg struct{}
func waitTasksCmd() tea.Cmd
func (m Model) onTasksChanged() (Model, tea.Cmd)
func (m Model) applyTasksConfig() (Model, tea.Cmd)
func (m Model) applyTaskResult(info domain.TaskInfo) (Model, tea.Cmd) // Tasks 5–7 fill the per-kind cases
func (m Model) taskEnded(info domain.TaskInfo) (Model, tea.Cmd)
func taskKindLabel(k exttool.Category) string // translated: "commit message", "review", …
func useTestTasks(t *testing.T) *domain.TaskManager // in task_track_test.go
func waitTaskState(t *testing.T, id domain.TaskID, ok func(domain.TaskInfo) bool) domain.TaskInfo // test helper
```

- [ ] **Step 1: Failing tests** (`task_track_test.go`, serial, skipped on Windows)

```go
package tui

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
)

// useTestTasks installs a fresh task manager with no history store (records
// fall back to memory) for one SERIAL test.
func useTestTasks(t *testing.T) *domain.TaskManager {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	mgr := domain.NewTaskManager(nil)
	restore := domain.UseTaskManager(mgr)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		mgr.KillAll(ctx)
		restore()
	})
	return mgr
}

func waitTaskState(t *testing.T, id domain.TaskID, ok func(domain.TaskInfo) bool) domain.TaskInfo {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		info, found := domain.Tasks().Get(id)
		if found && ok(info) {
			return info
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %s: timed out, last %+v", id, info)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestApplyTasksConfigSetsCapAndWarnsOnce(t *testing.T) {
	mgr := useTestTasks(t)
	m := newTestModel(t)
	m.cfg.Tasks.MaxParallel = 42
	m, _ = m.applyTasksConfig()
	if mgr.Max() != 10 {
		t.Fatalf("cap = %d, want 10", mgr.Max())
	}
	if !strings.Contains(m.statusMsg, "max_parallel") {
		t.Fatalf("status %q: want the clamp warning", m.statusMsg)
	}
	m.statusMsg = ""
	m, _ = m.applyTasksConfig()
	if m.statusMsg != "" {
		t.Fatalf("warning repeated: %q", m.statusMsg)
	}
	m.cfg.Tasks.MaxParallel = 2
	m, _ = m.applyTasksConfig()
	if mgr.Max() != 2 || m.statusMsg != "" {
		t.Fatalf("cap %d status %q", mgr.Max(), m.statusMsg)
	}
}

func TestOnTasksChangedReportsFailureOnce(t *testing.T) {
	useTestTasks(t)
	m := newTestModel(t)
	spec, err := m.svc.CommitMessageTask(context.Background(), captureCmd(exttool.CatCommitMessage, "exit 3"))
	if err != nil {
		t.Fatal(err)
	}
	id := domain.Tasks().Submit(spec)
	waitTaskState(t, id, func(i domain.TaskInfo) bool { return !i.State.Live() })
	m, _ = m.onTasksChanged()
	if !strings.Contains(m.statusMsg, "failed") {
		t.Fatalf("status %q: want a failure notice", m.statusMsg)
	}
	m.statusMsg = ""
	m, _ = m.onTasksChanged()
	if m.statusMsg != "" {
		t.Fatalf("failure reported twice: %q", m.statusMsg)
	}
}
```

Also add a `captureCmd` test helper in `task_track_test.go`:

```go
// captureCmd is a headless command block of kind running script under sh.
func captureCmd(kind exttool.Category, script string) config.ToolCommand {
	return config.ToolCommand{Category: string(kind), Name: "Test agent", Mode: "capture", Command: "sh -c '" + script + "'"}
}
```

(Check that `template.ResolveCommand` passes `sh -c '…'` through. If the resolver quotes tokens differently, write a two-line script file in `t.TempDir()` and run `sh <file>` instead.)

- [ ] **Step 2: Run them and watch them fail** — `rtk go test ./internal/tui -run 'TestApplyTasksConfig|TestOnTasksChanged' -count=1`. Expected: compile errors (`applyTasksConfig`, `onTasksChanged` undefined).

- [ ] **Step 3: Implement `task_track.go`**

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/i18n"
)

// taskTrack is what this TUI remembers about the process-global AI tasks
// (domain.Tasks()): which results it applied, which ends it reported, and
// the per-task wiring a launch set up. Maps, so value copies share them.
type taskTrack struct {
	seen    map[domain.TaskID]int
	ended   map[domain.TaskID]bool
	inbox   map[domain.TaskID]string
	fg      map[domain.TaskID]bool
	capWarn string
}

func newTaskTrack() *taskTrack {
	return &taskTrack{seen: map[domain.TaskID]int{}, ended: map[domain.TaskID]bool{},
		inbox: map[domain.TaskID]string{}, fg: map[domain.TaskID]bool{}, removed: map[domain.TaskID]bool{}}
}

// tasksChangedMsg: something in Tasks() changed. waitTasksCmd is the ONE
// reader of the coalesced Changed channel; onTasksChanged re-arms it.
type tasksChangedMsg struct{}

func waitTasksCmd() tea.Cmd {
	ch := domain.Tasks().Changed()
	return func() tea.Msg {
		<-ch
		return tasksChangedMsg{}
	}
}

// onTasksChanged hands each task's new wiring and results to the TUI once.
func (m Model) onTasksChanged() (Model, tea.Cmd) {
	var cmds []tea.Cmd
	tr := m.taskTrack
	for _, info := range domain.Tasks().List() {
		if info.Session != "" {
			if dir := tr.inbox[info.ID]; dir != "" {
				if _, ok := m.childInbox[info.Session]; !ok {
					m.childInbox[info.Session] = dir // steer_kept keeps it answered
				}
			}
			if tr.fg[info.ID] {
				delete(tr.fg, info.ID)
				var c tea.Cmd
				m, c = m.openConsole(info.Session)
				cmds = append(cmds, c)
			}
		}
		if info.Results > tr.seen[info.ID] {
			tr.seen[info.ID] = info.Results
			var c tea.Cmd
			m, c = m.applyTaskResult(info)
			cmds = append(cmds, c)
		}
		if !info.State.Live() && !tr.ended[info.ID] {
			tr.ended[info.ID] = true
			var c tea.Cmd
			m, c = m.taskEnded(info)
			cmds = append(cmds, c)
		}
	}
	if err := domain.Tasks().TakeStoreProblem(); err != nil {
		var c tea.Cmd
		m, c = m.stickyNotice(i18n.T("AI task history could not be saved (%s) — this session keeps it in memory", err.Error()))
		cmds = append(cmds, c)
	}
	return m, tea.Batch(append(cmds, waitTasksCmd())...)
}

// taskEnded reports an end that has no result of its own to show: a
// failure, a cancel, or a conflict run that reported nothing.
func (m Model) taskEnded(info domain.TaskInfo) (Model, tea.Cmd) {
	switch info.State {
	case domain.TaskFailed:
		return m.stickyNotice(i18n.T("%s failed — %s  (ctrl+\\ for details)", info.Key, info.Err))
	case domain.TaskDone:
		if info.Results == 0 && m.taskHere(info) && isConflictKind(info.Kind) {
			m.statusMsg = i18n.T("%s finished", info.Key)
			return m, m.loadCmd()
		}
	}
	return m, nil
}

// applyTaskResult routes a new result to its kind (Tasks 5–7 add cases).
func (m Model) applyTaskResult(info domain.TaskInfo) (Model, tea.Cmd) {
	return m.stickyNotice(i18n.T("%s ready — ctrl+\\", info.Key))
}

// taskHere: the task ran in the checkout the TUI shows.
func (m Model) taskHere(info domain.TaskInfo) bool {
	return domain.SameCheckout(info.Worktree, m.currentWorktree)
}

func isConflictKind(k exttool.Category) bool {
	return k == exttool.CatConflict || k == exttool.CatConflictComplete
}

// applyTasksConfig sets the cap from [tasks] max_parallel; an out-of-range
// value is clamped (domain) and warned about once per distinct value.
func (m Model) applyTasksConfig() (Model, tea.Cmd) {
	n, warn := m.cfg.Tasks.Parallel()
	domain.Tasks().SetMaxParallel(n)
	if warn == "" || warn == m.taskTrack.capWarn {
		return m, nil
	}
	m.taskTrack.capWarn = warn
	return m.stickyNotice(i18n.T("[tasks] max_parallel = %d is out of range; using %d", m.cfg.Tasks.MaxParallel, n))
}

// taskKindLabel names a kind for rows and titles.
func taskKindLabel(k exttool.Category) string {
	switch k {
	case exttool.CatCommitMessage:
		return i18n.T("commit message")
	case exttool.CatReview:
		return i18n.T("review")
	case exttool.CatConflict:
		return i18n.T("resolve conflict")
	case exttool.CatConflictComplete:
		return i18n.T("resolve & complete")
	}
	return string(k)
}

```


Wiring:
- `New`: `taskTrack: newTaskTrack(),`.
- `Init()` batch: add `waitTasksCmd()`.
- `dispatch`: `case tasksChangedMsg: return m.onTasksChanged()`.
- After each `m = m.applyBranchFilterConfig()` in `configReadyMsg` and `dataLoadedMsg`: `var tcmd tea.Cmd; m, tcmd = m.applyTasksConfig()`, and add `tcmd` to that case's returned batch.
- In `run.go`, after `m = m.applyBranchFilterConfig()`: `m, _ = m.applyTasksConfig()` (a startup warning comes back via `configReadyMsg`).

i18n: add every new key in this task to the four bundles.

- [ ] **Step 4: Pass** — the Step 2 command. Expected: PASS. Then `rtk go test ./internal/tui -count=1 > $WS/t3.log 2>&1; tail -5 $WS/t3.log`. Expected: ok (the i18n gates are green).

- [ ] **Step 5: Commit** — `rtk git add internal/tui internal/i18n && rtk git commit -m "feat(tui): track AI tasks — change loop, [tasks] max_parallel, history-store notice"`

---

### Task 4: The launch dialog

**Files:**
- Create: `internal/tui/task_launch_popup.go`
- Test: `internal/tui/task_launch_popup_test.go`

**Interfaces:**
- Consumes:
  - `domain.TaskChoices`, `EnsureInteractiveCommands`, `(*Service).TaskKey`;
  - the kind builders `CommitMessageTask` / `ReviewTask(ctx, tc, target, "")` / `ConflictTask(ctx, tc, complete)`;
  - `Tasks().Load/Submit`, `promptstate.TaskLaunch`;
  - `agentDetect` / `agentGlobalConfigPath` (the `agent_start_popup.go` seams);
  - `sessionPlace`, `consoleInner`, `m.childEnv()`, `m.childInboxDir()`, `m.toolCommandApproved`, `m.rememberToolApproval`, `approvalBoxView`, `newTemplateFill`.
- Produces:

```go
type launchMode string
const (
	launchBackground launchMode = "background"
	launchForeground launchMode = "foreground"
	launchHeadless   launchMode = "headless"
)
// taskLaunch is what a caller asks the dialog to launch.
type taskLaunch struct {
	kind      exttool.Category
	review    domain.ReviewTarget // CatReview only
	whenOp    string              // conflict kinds: the paused op (when_op filter)
	commitBox bool                // opened from the commit box (Task 5)
}
func (m Model) openTaskLaunch(l taskLaunch) (Model, tea.Cmd)
type taskSubmittedMsg struct { id domain.TaskID; launch taskLaunch; mode launchMode; key string; err error }
func (m Model) applyTaskSubmitted(msg taskSubmittedMsg) (Model, tea.Cmd)
var taskLookPath = exec.LookPath // test seam: "not found" marking
```

  `openTaskLaunch` checks `sessionPlace(m.currentWorktree)` first: a refusal (an unreachable checkout) shows its reason and opens nothing.

Dialog, as drawn (80 columns):

```
 review — main working changes
 Agent  ◀ Claude Code ▶                          1/2

 > Interactive, background   Claude Code (interactive)
   Interactive, background   Claude Code (interactive, yolo)
   Interactive, foreground   Claude Code (interactive)
   Interactive, foreground   Claude Code (interactive, yolo)
   Headless                  Claude Code

 1 task with this key is running — this one will wait
 [←/→] agent  [↑/↓] mode  [enter] run  [esc] cancel
```

- [ ] **Step 1: Failing tests** (serial; they use `useTestTasks` and a temp promptstate)

```go
func launchTestModel(t *testing.T) Model {
	t.Helper()
	useTestTasks(t)
	m := newTestModel(t)
	m.promptStore = promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	m.cfg.Tools.Command = []config.ToolCommand{
		{Category: "review", Name: "Claude Code", Mode: "capture", Command: "claude -p x"},
		{Category: "review", Name: "Claude Code (interactive)", Mode: "interactive", Command: "claude x"},
		{Category: "review", Name: "Kimi", Mode: "capture", Command: "kimi x"},
	}
	agentDetect = func() []exttool.Detection { return nil } // no first-run writes
	taskLookPath = func(string) (string, error) { return "/bin/true", nil }
	return m
}

// readyLaunch opens the dialog and delivers its async prep.
func readyLaunch(t *testing.T, m Model, l taskLaunch) (Model, *taskLaunchPopup) {
	t.Helper()
	m, cmd := m.openTaskLaunch(l)
	nm, _ := m.Update(cmd())
	m = nm.(Model)
	p := layerOf[*taskLaunchPopup](m)
	if p == nil || p.stage != launchChoose {
		t.Fatalf("dialog not ready: %+v", p)
	}
	return m, p
}

func TestLaunchDialogRowsAndGreyedModes(t *testing.T) {
	m := launchTestModel(t)
	m, p := readyLaunch(t, m, taskLaunch{kind: exttool.CatReview, review: domain.WorkingReviewTarget()})
	if !strings.HasPrefix(p.key, "review — ") {
		t.Fatalf("key %q", p.key)
	}
	if got := len(p.rows); got != 3 { // bg, fg, headless for Claude
		t.Fatalf("claude rows = %d", got)
	}
	m, _ = updateKey(m, "right") // Kimi: headless only
	p = layerOf[*taskLaunchPopup](m)
	if !p.rows[0].none || !p.rows[1].none || p.rows[2].none {
		t.Fatalf("kimi rows: %+v", p.rows)
	}
	if p.rows[p.sel].none {
		t.Fatal("cursor sits on a greyed row")
	}
}

func TestLaunchDialogRemembersChoice(t *testing.T) {
	m := launchTestModel(t)
	approveAll(m) // record approval for every configured command
	m, _ = readyLaunch(t, m, taskLaunch{kind: exttool.CatReview, review: domain.WorkingReviewTarget()})
	m, _ = updateKey(m, "down") // Claude fg
	m, cmd := updateKey(m, "enter")
	if cmd == nil {
		t.Fatal("enter must submit")
	}
	got, ok := m.promptStore.TaskLaunchChoice("review")
	if !ok || got.Mode != "foreground" || got.Command != "Claude Code (interactive)" {
		t.Fatalf("remembered %+v %v", got, ok)
	}
	m, p := readyLaunch(t, m, taskLaunch{kind: exttool.CatReview, review: domain.WorkingReviewTarget()})
	if r := p.rows[p.sel]; r.mode != launchForeground || r.tc.Name != "Claude Code (interactive)" {
		t.Fatalf("preselected %+v", r)
	}
}

func TestLaunchDialogApprovalBeforeRun(t *testing.T) {
	m := launchTestModel(t)
	m, _ = readyLaunch(t, m, taskLaunch{kind: exttool.CatReview, review: domain.WorkingReviewTarget()})
	m, cmd := updateKey(m, "enter")
	p := layerOf[*taskLaunchPopup](m)
	if cmd != nil || p == nil || p.stage != launchApprove {
		t.Fatal("unapproved command must show the approval box first")
	}
	m, cmd = updateKey(m, "enter")
	if cmd == nil || !m.toolCommandApproved("claude x") {
		t.Fatal("enter approves and submits")
	}
}

func TestLaunchDialogNotFoundRefuses(t *testing.T) {
	m := launchTestModel(t)
	approveAll(m)
	taskLookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	m, _ = readyLaunch(t, m, taskLaunch{kind: exttool.CatReview, review: domain.WorkingReviewTarget()})
	m, cmd := updateKey(m, "enter")
	if cmd != nil || !strings.Contains(m.statusMsg, "not found") {
		t.Fatalf("status %q", m.statusMsg)
	}
}

func TestLaunchDialogWaitLine(t *testing.T) {
	m := launchTestModel(t)
	_, p := readyLaunch(t, m, taskLaunch{kind: exttool.CatReview, review: domain.WorkingReviewTarget()})
	if got := launchWaitLine(domain.TaskLoad{SameKey: 1, Running: 1, Max: 3}); !strings.Contains(got, "will wait") {
		t.Fatalf("same key: %q", got)
	}
	if got := launchWaitLine(domain.TaskLoad{Running: 3, Max: 3}); !strings.Contains(got, "when one finishes") {
		t.Fatalf("cap: %q", got)
	}
	if got := launchWaitLine(domain.TaskLoad{Running: 1, Max: 3}); got != "" {
		t.Fatalf("free slot: %q", got)
	}
	_ = p
}

func TestLaunchDialogSubmitsHeadlessTask(t *testing.T) {
	m := launchTestModel(t)
	m.cfg.Tools.Command = []config.ToolCommand{captureCmd(exttool.CatReview, "echo looks good")}
	approveAll(m)
	m, _ = readyLaunch(t, m, taskLaunch{kind: exttool.CatReview, review: domain.WorkingReviewTarget()})
	m, cmd := updateKey(m, "enter")
	nm, _ := m.Update(cmd())
	m = nm.(Model)
	if layerOf[*taskLaunchPopup](m) != nil {
		t.Fatal("dialog must close on submit")
	}
	list := domain.Tasks().List()
	if len(list) != 1 || list[0].Mode != domain.TaskHeadless || !strings.HasPrefix(list[0].Key, "review — ") {
		t.Fatalf("tasks %+v", list)
	}
}
```

Helpers, in the same test file: `updateKey(m, s)` (`m.Update(keyMsg(s))` and cast; reuse one if a helper with this shape exists), and `approveAll(m)`, which calls `m.rememberToolApproval(tc.Command)` for each `m.cfg.Tools.Command`.

- [ ] **Step 2: Run them and watch them fail** — `rtk go test ./internal/tui -run TestLaunchDialog -count=1`. Expected: compile errors.

- [ ] **Step 3: Implement `task_launch_popup.go`**

Core types and flow:

```go
type launchStage int

const (
	launchPreparing launchStage = iota // key + first-run interactive commands (async)
	launchChoose
	launchApprove
)

// launchRow is one mode × command row; none = the agent has no command for
// that mode (greyed, never selectable).
type launchRow struct {
	mode    launchMode
	tc      config.ToolCommand
	none    bool
	missing bool // program not on PATH
}

type taskLaunchPopup struct {
	launch  taskLaunch
	stage   launchStage
	key     string
	choices []domain.TaskChoice
	agent   int
	rows    []launchRow
	sel     int
}

type taskLaunchReadyMsg struct {
	key   string
	cfg   *config.Config // non-nil when first-run commands were added
	added []string
	path  string
	err   error
}
```

`openTaskLaunch(l)`:
1. Push `&taskLaunchPopup{launch: l, stage: launchPreparing}`.
2. Return a cmd that runs `svc.TaskKey(ctx, l.kind, l.review)`. For kinds commit/review it also runs `domain.EnsureInteractiveCommands(cfg, agentGlobalConfigPath(), agentDetect)`; if names were added, it reloads with `config.Load(path, m.repoConfigPath)` (the `ensureAgentsCmd` shape).
3. Return the result as `taskLaunchReadyMsg`.

`applyTaskLaunchReady(msg)` (dispatched from `model.go`):
1. Drop the message if the popup is gone or not preparing.
2. On `err`: pop the popup and set the status `i18n.T("%s: %s", taskKindLabel(kind), err)`.
3. If `msg.cfg != nil`: `m.cfg = *msg.cfg`, and set the status to the `Added %s to %s …` text (an existing key).
4. Build `choices := m.launchChoices(l)`:
   - `domain.TaskChoices(m.cfg, l.kind, "tui")`;
   - within each choice, drop commands where `newTemplateFill(tc.Command).needsInput()`, or where `tc.WhenOp != "" && tc.WhenOp != l.whenOp`;
   - drop choices left empty.
5. With no choices: pop, and set the status `i18n.T("no %s agent configured (Settings → External tools)", taskKindLabel(kind))`.
6. Preselect from `m.promptStore.TaskLaunchChoice(string(kind))` by agent identity (`choiceIdentity(c)` = `"id:"+AgentID` or `"name:"+Agent`), then by mode + command name. Otherwise: agent 0, the first selectable row.
7. Set `stage = launchChoose`.

`buildRows(c domain.TaskChoice) []launchRow`: for each mode in bg, fg, headless, use `c.Interactive` (bg and fg) or `c.Headless`. Empty → `launchRow{mode, none: true}`. Otherwise one row per command, with `missing` set when `taskLookPath(commandProgram(tc.Command))` fails.

`commandProgram(cmd string) string`: the first shell word. A leading `"…"` or `'…'` is unquoted; otherwise the text up to the first space.

Keys:
- **choose:**
  - `left`/`right` cycle the agent (rows rebuilt; the cursor moves to the first selectable row, keeping the same mode if possible);
  - `up`/`down` move the cursor over selectable rows;
  - `enter` → `m.launchPick(p)`;
  - `esc` → `m.cancelTaskLaunch(p)` (pop; for conflict kinds, `startConflictProcess(m)` reopens the window);
  - `ctrl+c` → `tea.Quit`.
- **approve:** `enter` → `m.rememberToolApproval(row.tc.Command)`, then submit. `esc` → back to choose.
- **preparing:** `esc` cancels.

`launchPick`:
1. A missing row: set the status `i18n.T("%s: program not found on PATH", row.tc.Name)` and stay.
2. Remember the choice (`SetTaskLaunchChoice`).
3. Not approved → `stage = launchApprove`.
4. Otherwise `m.submitTask(p, row)`.

`submitTask(p, row)`:
1. Pop the dialog.
2. For `launchForeground` with `launch.commitBox`, also pop the commit popup if it is now on top (decision 2).
3. Compute:
   - `g := m.layout(); cols, rows := consoleInner(g.rightW, g.boxH[panelCommits])`;
   - `cwd, _, _ := sessionPlace(m.currentWorktree)`;
   - `env, inbox := m.childEnv(), m.childInboxDir()`.
4. Return a cmd that builds the spec off the UI thread. By kind:
   - `m.svc.CommitMessageTask(ctx, tc)`;
   - `ReviewTask(ctx, tc, l.review, "")`;
   - `ConflictTask(ctx, tc, kind == CatConflictComplete)`.
5. On a build error, return `taskSubmittedMsg{err}`.
6. Otherwise set `spec.Env, spec.Cwd, spec.Cols, spec.Rows` and `id := domain.Tasks().Submit(spec)`. Return `taskSubmittedMsg{id, launch, mode, key: spec.Key}`, with the inbox carried in an extra field `inbox string`.

`applyTaskSubmitted`:
1. On `err`: set the status `i18n.T("could not start %s: %s", …)`.
2. Otherwise:
   - `m.taskTrack.inbox[id] = inbox` for interactive modes;
   - `m.taskTrack.fg[id] = true` for foreground;
   - status `i18n.T("started %s", key)`, or `i18n.T("queued %s", key)` when `Tasks().Get(id)` says queued;
   - return `onTasksChanged()`'s cmd only if the session already exists (usually not; the change loop handles it).
3. Task 5 adds the commit-box hand-off here.

`launchWaitLine(l domain.TaskLoad) string`:
- `SameKey > 0` → `i18n.T("%d task(s) with this key ahead — this one will wait", l.SameKey)`;
- `Running >= Max` → `i18n.T("%d tasks running — it will start when one finishes", l.Running)`;
- otherwise `""`.

Render (`popupBox(popupInnerWidth(w), …)`, overlay centred):
- **title:** `truncate(p.key, textW)`, or `taskKindLabel` + `…` while preparing;
- **agent line:** `i18n.T("Agent")` + `  ◀ %s ▶` + right-aligned `n/N`;
- **one line per row:** `modeLabel(mode)` padded to the longest mode label, then the command name. A greyed row is faint and reads `i18n.T("(no command for this agent)")`. A missing row gets `  ` + `i18n.T("not found")`. The selected row is highlighted with `selectedRow`;
- **wait line:** `launchWaitLine(domain.Tasks().Load(p.key))` when it is non-empty;
- **hint:** `i18n.T("[←/→] agent  [↑/↓] mode  [enter] run  [esc] cancel")`.

Approve stage:
- the header is `i18n.T("Run this command?  (%s)", tc.Name)`, then `approvalBoxView(shown, textW)`;
- `shown` = `template.ResolveCommand(tc.Command, nil, template.CmdCtx{Repo: m.currentWorktree, Range: l.review.Range})` for commit/review; for conflict kinds it is the template text itself, since the engine resolves it after it writes its context file;
- the hint: `i18n.T("[enter] approve and run  [esc] back")`.

`modeLabel`:
- `launchBackground` → `i18n.T("Interactive, background")`;
- `launchForeground` → `i18n.T("Interactive, foreground")`;
- `launchHeadless` → `i18n.T("Headless")`.

Dispatch in `model.go`: `case taskLaunchReadyMsg: return m.applyTaskLaunchReady(msg)`, and `case taskSubmittedMsg: return m.applyTaskSubmitted(msg)`.

i18n for every new key.

- [ ] **Step 4: Pass** — `rtk go test ./internal/tui -run TestLaunchDialog -count=1`, then the whole tui package into the workspace log. Expected: ok.

- [ ] **Step 5: Commit** — `rtk git commit -m "feat(tui): AI task launch dialog — agent × mode × variant, wait line, remembered choice"` (stage `internal/tui internal/i18n`).

---

### Task 5: Commit box → dialog; commit-message results

**Files:**
- Rewrite: `internal/tui/commit_generate.go`
- Modify: `internal/tui/commit_popup.go` (sub-states, footer, esc and ctrl+b while generating, opening with a pending message)
- Modify: `internal/tui/model.go`:
  - the `c` handler at ~2220 consumes the pending message;
  - the `pendingCommitMsg` field;
  - drop `genCancel` and its `reRoot` block.
- Modify: `internal/tui/task_track.go` (`applyTaskResult` commit case)
- Modify: `internal/tui/task_launch_popup.go` (`applyTaskSubmitted` commit-box hand-off)
- Test: rewrite `internal/tui/commit_generate_test.go`

**Interfaces:**
- Produces:
  - `commitPopup` fields `genTask domain.TaskID`, `offer string` (a result awaiting ask-before-replace) and `offerFrom string`;
  - `Model.pendingCommitMsg map[string]pendingMessage`, keyed by `filepath.Clean(worktree)`, where `type pendingMessage struct{ text, from string }`;
  - `func (m Model) applyCommitMessage(info domain.TaskInfo) (Model, tea.Cmd)`.
- Removed: `choosing`, `approving`, `confirming`, `genCmd`, `genMessageMsg`, `genMessageCmd`, `applyGeneratedMessage`, `updateChoosing`, `chooseBox`, `updateApproving`, `approveBox`, `updateConfirming`, `confirmBox`, `Model.genCancel`.

Behaviour:
- `ctrl+g` (idle box, something staged) → `m.openTaskLaunch(taskLaunch{kind: CatCommitMessage, commitBox: true})`. The dialog is pushed OVER the box, and `esc` returns to it (the layer stack does this).
- **Headless submit from the box:** `applyTaskSubmitted` finds the commit popup on top (`m.topCommitPopup()`), sets `p.genTask = id`, `p.generating = true`, `p.genStart = now`, `p.genGen++`, and returns `spinTickCmd(p.genGen)`.
- **While generating:**
  - the footer shows `%c queued…  ([esc] cancel  [ctrl+b] background)` while the task is queued, and `%c generating message… %ds  ([esc] cancel  [ctrl+b] background)` while it runs;
  - `esc` → `domain.Tasks().Cancel(p.genTask)`, `p.generating = false`, `p.genTask = ""`;
  - `ctrl+b` → the box closes (`popLayer`) and the task keeps running; its result becomes the pending message, with a notice.
  - `tickGenSpinner` stops when the task is no longer live.
- **`applyCommitMessage(info)`**, called from `applyTaskResult` for `CatCommitMessage`, with `from := info.Agent`:
  1. The task is not here (`!m.taskHere(info)`) → sticky `%s ready — ctrl+\`.
  2. The commit popup is on top and not amend:
     - generating on this task → fill the title/desc from `info.Result` (split on the first blank line), `generating = false`, status `i18n.T("message from %s", from)`;
     - fields empty → fill, same status;
     - otherwise → `p.offer = info.Result`, `p.offerFrom = from`. The box shows the confirm-replace sub-screen (`i18n.T("Replace current message with %s's?", from)`, `[y]es / [enter]  [esc] no`): `y`/`enter` fills, `esc` keeps the old text.
  3. Otherwise → `m.pendingCommitMsg[clean(worktree)] = pendingMessage{…}` and sticky `i18n.T("commit message from %s ready — press c", from)`.
- **`c` with a pending message for `currentWorktree`:** push `&commitPopup{title, desc}` filled from it, delete the entry, status `i18n.T("message from %s", from)`.
- **Failure of the box's own task:** `taskEnded` also clears `p.generating` when `info.ID == p.genTask`. The failure notice fires as usual.

- [ ] **Step 1: Failing tests** (rewrite `commit_generate_test.go`; delete the tests for removed choosers and gates)

```go
func commitBoxModel(t *testing.T, script string) Model {
	t.Helper()
	useTestTasks(t)
	m := newTestModel(t)
	stageAFile(t, m) // helper: write + git add a file via m.svc (reuse the existing one in commit tests)
	m.promptStore = promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	m.cfg.Tools.Command = []config.ToolCommand{captureCmd(exttool.CatCommitMessage, script)}
	approveAll(m)
	taskLookPath = func(string) (string, error) { return "/bin/sh", nil }
	agentDetect = func() []exttool.Detection { return nil }
	m = m.pushLayer(&commitPopup{})
	return m
}

func TestCommitBoxGenerateHeadlessFillsBox(t *testing.T) {
	m := commitBoxModel(t, "printf \"feat: x\\n\\nbody\\n\"")
	m, cmd := updateKey(m, "ctrl+g")
	m = deliver(t, m, cmd)            // taskLaunchReadyMsg
	m, cmd = updateKey(m, "enter")    // headless is the only mode
	m = deliver(t, m, cmd)            // taskSubmittedMsg
	p := m.topCommitPopup()
	if p == nil || !p.generating || p.genTask == "" {
		t.Fatalf("box not waiting on a task: %+v", p)
	}
	waitTaskState(t, p.genTask, func(i domain.TaskInfo) bool { return !i.State.Live() })
	m, _ = m.onTasksChanged()
	p = m.topCommitPopup()
	if p.generating || p.title.Value() != "feat: x" || p.desc.Value() != "body" {
		t.Fatalf("title %q desc %q generating %v", p.title.Value(), p.desc.Value(), p.generating)
	}
}

func TestCommitBoxResultAsksBeforeReplacing(t *testing.T) {
	m := commitBoxModel(t, "echo \"feat: new\"")
	p := m.topCommitPopup()
	p.title = newTextField("mine")
	id := submitCommitTask(t, m) // builds CommitMessageTask with the configured command and submits it
	waitTaskState(t, id, func(i domain.TaskInfo) bool { return !i.State.Live() })
	m, _ = m.onTasksChanged()
	if p.title.Value() != "mine" || p.offer == "" {
		t.Fatalf("must ask first: title %q offer %q", p.title.Value(), p.offer)
	}
	m, _ = updateKey(m, "y")
	if p.title.Value() != "feat: new" || p.offer != "" {
		t.Fatalf("after y: %q", p.title.Value())
	}
}

func TestCommitMessageWithBoxClosedBecomesPending(t *testing.T) {
	m := commitBoxModel(t, "echo \"feat: later\"")
	m = m.popLayer()
	id := submitCommitTask(t, m)
	waitTaskState(t, id, func(i domain.TaskInfo) bool { return !i.State.Live() })
	m, _ = m.onTasksChanged()
	if !strings.Contains(m.statusMsg, "press c") {
		t.Fatalf("status %q", m.statusMsg)
	}
	m, _ = updateKey(m, "c")
	if p := m.topCommitPopup(); p == nil || p.title.Value() != "feat: later" {
		t.Fatalf("c must open the box filled: %+v", p)
	}
}

func TestCommitBoxEscCancelsItsTask(t *testing.T) {
	m := commitBoxModel(t, "sleep 5; echo late")
	m, cmd := updateKey(m, "ctrl+g")
	m = deliver(t, m, cmd)
	m, cmd = updateKey(m, "enter")
	m = deliver(t, m, cmd)
	id := m.topCommitPopup().genTask
	m, _ = updateKey(m, "esc")
	info := waitTaskState(t, id, func(i domain.TaskInfo) bool { return !i.State.Live() })
	if info.State != domain.TaskCancelled || m.topCommitPopup() == nil || m.topCommitPopup().generating {
		t.Fatalf("state %s", info.State)
	}
}
```

`deliver(t, m, cmd)` runs `cmd()` and feeds the resulting message to `Update`. It must skip spinner ticks: batch messages are unwrapped, and only non-tick messages are fed. Put it in `task_track_test.go`.

`submitCommitTask(t, m)` builds a spec with `m.svc.CommitMessageTask(ctx, m.cfg.Tools.Command[0])`, submits it, and returns its id.

- [ ] **Step 2: Run them and watch them fail** — `rtk go test ./internal/tui -run 'TestCommitBox|TestCommitMessageWithBoxClosed' -count=1`. Expected: compile errors (`genTask` / `offer` undefined).

- [ ] **Step 3: Implement** the behaviour above. Keep `genSpinMsg` and `spinTickCmd`. Remove `laneToolCommands` use from this file. Update the `ctrl+g` help text (`help.go:242`) to:

> generate a commit message with an AI agent: the launch dialog picks the agent and how it runs (interactive in the background or foreground, or headless); results fill the box, asking first when it has text; esc cancels a headless run, ctrl+b sends it to the background

That is a new key, in all four bundles; drop the old key from the bundles only if no other code references it. Add the new keys to the bundles.

- [ ] **Step 4: Pass** — the Step 2 command, then the whole tui package. Expected: ok.

- [ ] **Step 5: Commit** — `rtk git commit -m "feat(tui): commit box generates through the task launch dialog; results fill it or wait for c"`

---

### Task 6: Review → dialog; review results; the task status segment

**Files:**
- Modify: `internal/tui/review.go`:
  - delete `reviewLane`, `reviewDoneMsg`, `reviewBlinkMsg`/`Cmd`, `reviewDispatch`, `reviewRunCmd`, `applyReviewDone`, `cancelReview`, and the lane layer methods;
  - `startReviewLane` becomes `startReview(target)`;
  - `hasReviewTool` checks `domain.TaskChoices(m.cfg, CatReview, "tui")`;
  - drop the `m.reviewRunning` gates.
- Modify: `internal/tui/model.go`:
  - drop the fields `reviewGen`, `reviewRunning`, `reviewRunningLabel`, `reviewBlink`;
  - drop the dispatch cases for the removed messages;
  - `reviewTargetReadyMsg` loses its gen (no gen: a late target opens the dialog in the current repo only if `m.svc` is unchanged, so keep a `svc` pointer in the msg and compare it);
  - `reRoot` drops `cancelReview`.
- Modify: `internal/tui/view.go` — the review segment is replaced by `taskSegment()`: `⟳ N AI tasks` when `domain.Tasks().Live() > 0` and `m.proc == nil`; no blink.
- Modify: `internal/tui/task_track.go` — the `applyTaskResult` review case.
- Modify: `internal/tui/conflict_process.go:201` — drop the `reviewRunning` refusal.
- Modify: `internal/tui/tools.go` — delete `laneToolCommands`.
- Test: rewrite `internal/tui/review_test.go` (delete the lane, blink and gen tests); update `tools_test.go` to drop the `laneToolCommands` tests.

**Interfaces:**
- Produces: `func (m Model) startReview(target domain.ReviewTarget) (Model, tea.Cmd)` → `openTaskLaunch(taskLaunch{kind: CatReview, review: target})`.
- Produces: `func (m Model) applyReviewResult(info domain.TaskInfo) (Model, tea.Cmd)`:
  1. The report path comes from `m.svc.SaveReviewReport(ctx, label, info.Result, time.Now())`, where `label` = `strings.TrimPrefix(info.Key, "review — ")`. This runs synchronously: one file write. On error the status is `review: %s` and the path is empty.
  2. If `m.canShowResult(info)`, push `newReviewView(reviewTitle(label), path, info.Result)`; otherwise sticky `%s ready — ctrl+\`.
- Produces: `func (m Model) canShowResult(info domain.TaskInfo) bool` = `m.taskHere(info) && m.proc == nil && !(m.console != nil && m.console.focused) && m.modal == nil`.

- [ ] **Step 1: Failing tests**

```go
func TestReviewRowOpensLaunchDialog(t *testing.T) {
	m := launchTestModel(t)
	m.focus = panelFiles
	row, ok := m.workingReviewRow()
	if !ok {
		t.Fatal("row hidden with a review agent configured")
	}
	nm, _ := row.run(m)
	if layerOf[*taskLaunchPopup](nm.(Model)) == nil {
		t.Fatal("review must open the launch dialog")
	}
}

func TestReviewResultOpensViewerAndSavesReport(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := launchTestModel(t)
	m.cfg.Tools.Command = []config.ToolCommand{captureCmd(exttool.CatReview, "echo LGTM")}
	spec, err := m.svc.ReviewTask(context.Background(), m.cfg.Tools.Command[0], domain.WorkingReviewTarget(), "")
	if err != nil {
		t.Fatal(err)
	}
	id := domain.Tasks().Submit(spec)
	waitTaskState(t, id, func(i domain.TaskInfo) bool { return !i.State.Live() })
	m, _ = m.onTasksChanged()
	v := layerOf[*reviewView](m)
	if v == nil || v.path == "" || !strings.Contains(strings.Join(v.lines, "\n"), "LGTM") {
		t.Fatalf("viewer %+v", v)
	}
}

func TestReviewResultWhileConsoleFocusedIsANotice(t *testing.T) {
	m := launchTestModel(t)
	m.console = &consoleState{focused: true}
	info := domain.TaskInfo{ID: "x", Key: "review — a..b", Kind: exttool.CatReview, Worktree: m.currentWorktree, Result: "ok", Results: 1}
	m, _ = m.applyTaskResult(info)
	if layerOf[*reviewView](m) != nil || !strings.Contains(m.statusMsg, "ready") {
		t.Fatalf("status %q", m.statusMsg)
	}
}

func TestTaskSegmentCountsLiveTasks(t *testing.T) {
	m := launchTestModel(t)
	m.cfg.Tools.Command = []config.ToolCommand{captureCmd(exttool.CatReview, "sleep 5")}
	spec, _ := m.svc.ReviewTask(context.Background(), m.cfg.Tools.Command[0], domain.WorkingReviewTarget(), "")
	domain.Tasks().Submit(spec)
	if seg := m.taskSegment(); !strings.Contains(seg, "1") {
		t.Fatalf("segment %q", seg)
	}
}
```

- [ ] **Step 2: Run them and watch them fail** — `rtk go test ./internal/tui -run 'TestReviewRow|TestReviewResult|TestTaskSegment' -count=1`. Expected: compile errors.

- [ ] **Step 3: Implement** as above. `applyTaskResult` becomes a switch:

```go
func (m Model) applyTaskResult(info domain.TaskInfo) (Model, tea.Cmd) {
	switch info.Kind {
	case exttool.CatCommitMessage:
		return m.applyCommitMessage(info)
	case exttool.CatReview:
		return m.applyReviewResult(info)
	}
	return m.stickyNotice(i18n.T("%s ready — ctrl+\\", info.Key))
}
```

i18n: `⟳ %d AI task(s)` and the other new keys. Remove the bundle entries for deleted keys only when the i18n "unused key" gate requires it (run the gates to see).

- [ ] **Step 4: Pass** — the package tests into the workspace log. Expected: ok.

- [ ] **Step 5: Commit** — `rtk git commit -m "feat(tui): reviews run as AI tasks through the launch dialog; ⟳ N AI tasks segment"`

---

### Task 7: Conflict window → dialog; conflict results

**Files:**
- Modify: `internal/tui/conflict_process.go`:
  - the `t` picker rows;
  - `startToolRun` on an agent row;
  - drop the capture branch of `runPending` and the `messageFile` / overview code in `buildToolRun` and `toolFinished`;
  - `pendingToolRun.messageFile` goes.
- Modify: `internal/tui/tools.go` (`pendingToolRun`; `completeToolChoices` stays for the row gating)
- Modify: `internal/tui/task_track.go` (the `applyTaskResult` conflict cases)
- Test: `internal/tui/conflict_tools_test.go` (update the picker tests; delete the capture and overview tests); new `conflict_task_test.go`

**Interfaces:**
- The picker rows become `[]conflictToolRow{tc config.ToolCommand; agent exttool.Category}`:
  - `agent == ""` → a per-file mergetool row (today's handover path);
  - `agent == CatConflict` → "Resolve with an agent…", present when `TaskChoices(m.cfg, CatConflict, "tui")` has any command whose `WhenOp` fits;
  - `agent == CatConflictComplete` → "Resolve & complete with an agent…", same test over `completeToolChoices`.
- Choosing an agent row: `op := p.src.Op; m.proc = nil; return m.openTaskLaunch(taskLaunch{kind: row.agent, whenOp: op})`. `cancelTaskLaunch` for conflict kinds calls `startConflictProcess(m)`.
- Produces: `func (m Model) applyConflictResult(info domain.TaskInfo) (Model, tea.Cmd)`:
  - when `m.canShowResult(info)`, push `newReviewView(i18n.T("Resolution overview — %s", info.Agent), "", info.Result)` and return `m.loadCmd()`;
  - otherwise sticky, plus `m.loadCmd()` when `m.taskHere(info)`.
- Every conflict-kind task end in this checkout also reloads the status (`taskEnded`, from Task 3), so ⏸ and the conflict list stay current.

- [ ] **Step 1: Failing tests**

```go
func TestConflictPickerOffersAgentRows(t *testing.T) {
	m := conflictModelWithAgents(t) // an existing merge-conflict fixture + one interactive conflict row + one conflict_complete row
	m, _ = updateKey(m, "t")
	p := m.proc.(*conflictProcess)
	var kinds []exttool.Category
	for _, r := range p.toolChoices {
		kinds = append(kinds, r.agent)
	}
	if !slices.Contains(kinds, exttool.CatConflict) || !slices.Contains(kinds, exttool.CatConflictComplete) {
		t.Fatalf("rows %v", kinds)
	}
}

func TestConflictAgentRowClosesProcessAndOpensDialog(t *testing.T) {
	m := conflictModelWithAgents(t)
	m, _ = updateKey(m, "t")
	p := m.proc.(*conflictProcess)
	p.toolSel = indexOfAgentRow(p, exttool.CatConflict)
	m, _ = updateKey(m, "enter")
	if m.proc != nil || layerOf[*taskLaunchPopup](m) == nil {
		t.Fatal("agent row must leave the process and open the dialog")
	}
	m, _ = updateKey(m, "esc")
	if m.proc == nil {
		t.Fatal("esc in the dialog must reopen the conflict window")
	}
}

func TestConflictResultOpensOverview(t *testing.T) {
	m := launchTestModel(t)
	info := domain.TaskInfo{ID: "c", Key: "resolve & complete — x merge 1234567", Kind: exttool.CatConflictComplete,
		Agent: "Claude Code", Worktree: m.currentWorktree, Result: "resolved 2 files", Results: 1}
	m, cmd := m.applyTaskResult(info)
	if v := layerOf[*reviewView](m); v == nil || cmd == nil {
		t.Fatal("overview viewer + status reload expected")
	}
}
```

Write `conflictModelWithAgents` from the existing conflict fixture helper in `conflict_tools_test.go` (the one that builds a real merge conflict), with `m.cfg.Tools.Command` set to one `conflict` interactive row (`sh -c true`) and one `conflict_complete` capture row. `indexOfAgentRow` scans `p.toolChoices`.

- [ ] **Step 2: Run them and watch them fail** — `rtk go test ./internal/tui -run 'TestConflictPicker|TestConflictAgentRow|TestConflictResult' -count=1`. Expected: compile errors.

- [ ] **Step 3: Implement** as above.
  - Render the agent rows with the labels `i18n.T("Resolve with an agent…")` and `i18n.T("Resolve & complete with an agent…")`.
  - `conflict_process.go:747` (`nTools`) counts mergetool rows plus agent rows.
  - Add the `applyTaskResult` cases `CatConflict, CatConflictComplete → applyConflictResult`.

- [ ] **Step 4: Pass** — the package tests into the log. Expected: ok.

- [ ] **Step 5: Commit** — `rtk git commit -m "feat(tui): conflict agents run as AI tasks from the t picker; overview on result"`

---

### Task 8: The Headless tab in `ctrl+\`

**Files:**
- Create: `internal/tui/task_tab.go` (rows, keys and render of the tab)
- Modify: `internal/tui/sessions_popup.go`:
  - the `tab` field (`tabSessions` / `tabTasks`);
  - `tab` switches;
  - `update`/`render` delegate when `tabTasks`;
  - `openSessionsPopup` also opens when tasks or history exist, and `openSessionsPopupOn(tabTasks, id)` for ◆ rows;
  - quit mode stays on the sessions tab.
- Modify: `internal/tui/review_view.go`:
  - fields `apply func(Model) (Model, tea.Cmd)` and `copyText string`;
  - `y` copies (`m.copyToClipboardCmd(i18n.T("copied"), content)`);
  - `a` runs `apply` when non-nil;
  - `e` only when `path != ""`;
  - the hint row adds `[y] copy` and `[a] apply` when available.
- Modify: `internal/tui/footer.go:204` — the `agent-sessions` binding shows when sessions OR tasks/history exist; label `[ctrl+\] agents & tasks` when there are tasks.
- Test: `internal/tui/task_tab_test.go`

**Interfaces:**
- Consumes: `Tasks().List()`, `History()`, `HistoryResult(id)`, `HistoryTail(id)`, `RemoveHistory(id)`, `Cancel(id)`.
- Produces:

```go
type taskRow struct {
	live   *domain.TaskInfo    // a live (or this-process ended) task
	record *domain.TaskRecord  // a history record not in List()
}
func taskRows(live []domain.TaskInfo, hist []domain.TaskRecord, query string) []taskRow
func taskRowText(r taskRow, now time.Time) string // "key · agent · state · age"
```

  The history slice is cached on the popup (`p.hist`), refreshed on tab open and in `onTasksChanged` when a sessions popup is on top. It is never refreshed per render (it is TOML I/O).

Rows and keys:
1. `taskRows` order:
   - `List()` live tasks (submit order);
   - then this process's ended tasks (the List tail);
   - then history records whose ID is not in `List()`.
2. `taskRowText`:
   - `key · agent · state · age`;
   - state is translated (`queued`, `running`, `result ready`, `done`, `failed`, `cancelled`);
   - age is `formatElapsed(now − Submitted/Started/Ended)` + `i18n.T(" ago")` for ended ones, or elapsed for running ones.
3. `enter`:
   - a result exists (live `Results > 0`, or `HistoryResult(id) != ""`) → push `newReviewView(key, "", result)`. `copyText = result`. `apply` is set only when the kind is commit message and the task is here (it does what `applyCommitMessage` would do). Review and conflict kinds re-open as-is (no `a`);
   - else a live interactive task with a session → pop the popup and `openConsole(session)`;
   - else a failed task → viewer over `HistoryTail(id)` (or `info.Tail`), titled `i18n.T("%s — output", key)`;
   - else the status `i18n.T("no result yet")`.
4. `k`: first press → `confirmCancel = id` and the status `press k again to cancel %s`; second press → `Tasks().Cancel(id)`. Only live tasks.
5. `x`: an ended task → `Tasks().RemoveHistory(id)` and a refresh of `p.hist`. On a live task: status `i18n.T("cancel it first (k k)")`.
6. `/` filters, as in the sessions tab.
7. Title: `i18n.T("AI tasks")` + `[tab] sessions`. The sessions tab's title gains `[tab] tasks`.
8. Hint: `i18n.T("[enter] result/console  [k k] cancel  [x] remove  [/] filter  [tab] sessions  [esc] close")`.

- [ ] **Step 1: Failing tests**

```go
func TestTaskRowsOrderLiveThenHistory(t *testing.T) {
	t.Parallel()
	now := time.Now()
	live := []domain.TaskInfo{{ID: "b", Key: "review — a..b", State: domain.TaskRunning, Started: now}}
	hist := []domain.TaskRecord{{ID: "a", Key: "commit message — w @ 1234567", State: "done", Ended: now}, {ID: "b"}}
	rows := taskRows(live, hist, "")
	if len(rows) != 2 || rows[0].live == nil || rows[1].record == nil || rows[1].record.ID != "a" {
		t.Fatalf("rows %+v", rows)
	}
	if got := taskRows(live, hist, "commit"); len(got) != 1 {
		t.Fatalf("filter: %+v", got)
	}
}

func TestTaskTabEnterShowsResultAndCopy(t *testing.T) {
	m := launchTestModel(t)
	m.cfg.Tools.Command = []config.ToolCommand{captureCmd(exttool.CatReview, "echo fine")}
	spec, _ := m.svc.ReviewTask(context.Background(), m.cfg.Tools.Command[0], domain.WorkingReviewTarget(), "")
	id := domain.Tasks().Submit(spec)
	waitTaskState(t, id, func(i domain.TaskInfo) bool { return !i.State.Live() })
	m, _ = m.openSessionsPopupOn(tabTasks, id)
	m, _ = updateKey(m, "enter")
	v := layerOf[*reviewView](m)
	if v == nil || v.copyText != "fine" || v.apply != nil {
		t.Fatalf("viewer %+v", v)
	}
}

func TestTaskTabKKCancelsQueued(t *testing.T) {
	m := launchTestModel(t)
	m.cfg.Tools.Command = []config.ToolCommand{captureCmd(exttool.CatReview, "sleep 5")}
	spec, _ := m.svc.ReviewTask(context.Background(), m.cfg.Tools.Command[0], domain.WorkingReviewTarget(), "")
	first := domain.Tasks().Submit(spec)
	second := domain.Tasks().Submit(spec) // same key: queued behind first
	m, _ = m.openSessionsPopupOn(tabTasks, second)
	m, _ = updateKey(m, "k")
	m, _ = updateKey(m, "k")
	if info, _ := domain.Tasks().Get(second); info.State != domain.TaskCancelled {
		t.Fatalf("second = %s", info.State)
	}
	_ = domain.Tasks().Cancel(first)
	info := waitTaskState(t, first, func(i domain.TaskInfo) bool { return !i.State.Live() })
	if s, _ := domain.Tasks().Get(second); s.State != domain.TaskCancelled || info.State != domain.TaskCancelled {
		t.Fatal("a cancelled queued task must never start")
	}
}

func TestTaskTabXRemovesEnded(t *testing.T) {
	m := launchTestModel(t)
	m.cfg.Tools.Command = []config.ToolCommand{captureCmd(exttool.CatReview, "echo ok")}
	spec, _ := m.svc.ReviewTask(context.Background(), m.cfg.Tools.Command[0], domain.WorkingReviewTarget(), "")
	id := domain.Tasks().Submit(spec)
	waitTaskState(t, id, func(i domain.TaskInfo) bool { return !i.State.Live() })
	m, _ = m.openSessionsPopupOn(tabTasks, id)
	m, _ = updateKey(m, "x")
	for _, r := range domain.Tasks().History() {
		if r.ID == string(id) {
			t.Fatal("record still in history")
		}
	}
}
```

Note for `x`: `RemoveHistory` removes the record. The in-memory ended task still shows in `List()`, so the tab also hides removed IDs: `m.taskTrack.removed[id] = true`, a new map in `taskTrack`, filtered in `taskRows`' caller.

- [ ] **Step 2: Run them and watch them fail** — `rtk go test ./internal/tui -run 'TestTaskRows|TestTaskTab' -count=1`. Expected: compile errors.

- [ ] **Step 3: Implement** as above. Add the i18n keys.

- [ ] **Step 4: Pass** — the package tests into the log. Expected: ok.

- [ ] **Step 5: Commit** — `rtk git commit -m "feat(tui): Headless tab in ctrl+\\ — live tasks, history, result viewer, cancel, remove"`

---

### Task 9: ◆ rows under Worktrees

**Files:**
- Modify: `internal/tui/worktree_sessions.go` (`wtEntry.task domain.TaskID`; `worktreeEntries` appends live headless tasks, matched by `domain.SameCheckout(info.Worktree, w.Path)`, after that worktree's sessions; `selectedTask()`)
- Modify: `internal/tui/view.go` (`worktreeRows`: a task entry renders `taskSubRowText(info)`)
- Modify: `internal/tui/agent_start_popup.go` (`sessionMenuRows`: task rows)
- Modify: `internal/tui/model.go` (enter on a Worktrees ◆ row → `openSessionsPopupOn(tabTasks, id)`, next to the existing session-row enter at ~2547)
- Test: `internal/tui/worktree_tasks_test.go`

**Interfaces:**
- Produces: `func taskSubRowText(info domain.TaskInfo) string` → `"  └ ◆ " + taskKindLabel(kind) + " " + target + "  " + state(+elapsed)`, where target = the key after ` — `.
- Produces: `func (m Model) selectedTask() (domain.TaskInfo, bool)`.
- `.` menu on a ◆ row:
  - `task-cancel` "Cancel task", while it is live;
  - `task-result` "Show result", which opens the tab on it.

  (A live headless task has no result yet; "Show result" is offered only when `Results > 0`. That is effectively never for headless, but it keeps the spec's row.)

- [ ] **Step 1: Failing tests**

```go
func TestWorktreesShowLiveHeadlessTasks(t *testing.T) {
	m := launchTestModel(t)
	m.cfg.Tools.Command = []config.ToolCommand{captureCmd(exttool.CatReview, "sleep 5")}
	spec, _ := m.svc.ReviewTask(context.Background(), m.cfg.Tools.Command[0], domain.WorkingReviewTarget(), "")
	id := domain.Tasks().Submit(spec)
	m = withWorktrees(t, m) // load m.worktrees for the test repo (reuse the existing loader helper)
	var found bool
	for i, e := range m.worktreeEntries() {
		if e.task == id {
			found = true
			if row := m.worktreeRows(m.worktreeEntries())[i]; !strings.Contains(row, "◆") {
				t.Fatalf("row %q", row)
			}
		}
	}
	if !found {
		t.Fatal("no ◆ row for the live headless task")
	}
	_ = domain.Tasks().Cancel(id)
	waitTaskState(t, id, func(i domain.TaskInfo) bool { return !i.State.Live() })
	for _, e := range m.worktreeEntries() {
		if e.task == id {
			t.Fatal("ended task must leave Worktrees")
		}
	}
}

func TestTaskRowMenuCancels(t *testing.T) {
	m := launchTestModel(t)
	m.cfg.Tools.Command = []config.ToolCommand{captureCmd(exttool.CatReview, "sleep 5")}
	spec, _ := m.svc.ReviewTask(context.Background(), m.cfg.Tools.Command[0], domain.WorkingReviewTarget(), "")
	id := domain.Tasks().Submit(spec)
	m = withWorktrees(t, m)
	m = selectWorktreeTask(t, m, id) // set focus + sel to the ◆ entry
	rows := m.sessionMenuRows()
	if len(rows) == 0 || rows[0].id != "task-cancel" {
		t.Fatalf("menu %+v", rows)
	}
	nm, _ := rows[0].run(m)
	_ = nm
	info := waitTaskState(t, id, func(i domain.TaskInfo) bool { return !i.State.Live() })
	if info.State != domain.TaskCancelled {
		t.Fatalf("state %s", info.State)
	}
}
```

- [ ] **Step 2: Run them and watch them fail** — `rtk go test ./internal/tui -run 'TestWorktreesShowLiveHeadless|TestTaskRowMenu' -count=1`. Expected: compile errors.

- [ ] **Step 3: Implement.** The Worktrees list re-renders on `tasksChangedMsg` for free, because every message re-renders. Check that `displayIndices(panelWorktrees)` counts entries via `worktreeEntries()`: it should, since sessions already work this way.

- [ ] **Step 4: Pass** — the package tests into the log. Expected: ok.

- [ ] **Step 5: Commit** — `rtk git commit -m "feat(tui): ◆ rows for live headless AI tasks under Worktrees"`

---

### Task 10: Quit guard counts tasks; kill on quit

**Files:**
- Modify: `internal/tui/quit_guard.go`, `internal/tui/run.go`, `internal/tui/sessions_popup.go` (quit-mode title and `Q`)
- Test: `internal/tui/quit_guard_test.go` (extend)

**Interfaces:**
- Produces: `func liveWork() int` = `domain.Sessions().LiveCount()` + the number of live tasks without a running session (queued, or headless), counted from `Tasks().List()`. An interactive task with a session is already counted as a session.

Changes:
- `quitFilter` holds the quit when `liveWork() > 0`.
- `killAllAndQuitCmd` runs `domain.Tasks().KillAll(ctx)` BEFORE `domain.Sessions().KillAll(ctx)`, so the records say cancelled.
- `run.go`'s safety net does the same.
- The quit-mode title becomes `i18n.T("agents and AI tasks still running: %d — quit gg?", liveWork())`.
- The quit-mode popup opens even with zero sessions (tasks only): `openSessionsPopup(true)` already skips the early return in quit mode.

- [ ] **Step 1: Failing test**

```go
func TestQuitGuardHoldsForHeadlessTask(t *testing.T) {
	m := launchTestModel(t)
	m.cfg.Tools.Command = []config.ToolCommand{captureCmd(exttool.CatReview, "sleep 5")}
	spec, _ := m.svc.ReviewTask(context.Background(), m.cfg.Tools.Command[0], domain.WorkingReviewTarget(), "")
	id := domain.Tasks().Submit(spec)
	if _, held := quitFilter(m, tea.QuitMsg{}).(quitHeldMsg); !held {
		t.Fatal("quit must be held while a task runs")
	}
	msg := killAllAndQuitCmd()()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("got %T", msg)
	}
	if info, _ := domain.Tasks().Get(id); info.State != domain.TaskCancelled {
		t.Fatalf("state %s", info.State)
	}
}
```

- [ ] **Step 2: Run it and watch it fail** — `rtk go test ./internal/tui -run TestQuitGuardHoldsForHeadlessTask -count=1`. Expected: FAIL, "quit must be held".

- [ ] **Step 3: Implement** as above, with the i18n key.

- [ ] **Step 4: Pass** — the package tests into the log. Expected: ok.

- [ ] **Step 5: Commit** — `rtk git commit -m "feat(tui): the quit guard counts AI tasks; quitting cancels them"`

---

### Task 11: Help, footer, docs, gates, live smoke

**Files:**
- Modify: `internal/tui/help.go`:
  - update the `ctrl+\` row to describe the Headless tab (`tab` switches tabs; enter shows a result, or opens a running agent's console; k k cancels; x removes);
  - add rows for the launch dialog (`←/→`, `↑/↓`, enter, esc) under an "AI tasks" heading;
  - update the Worktrees `.` row with ◆ rows.
- Modify: `CHANGELOG.md`, `README.md` (AI tasks section: the launch dialog, the Headless tab, ◆ rows, `[tasks] max_parallel`), `docs/CLAUDE-details.md` (a new section "AI tasks — the TUI (plan 3, 2026-09-25)": the rulings above, `taskTrack`, the apply rules, the retired lanes), `CLAUDE.md` (append to the `tui` row: "AI-task launch dialog + Headless tab over `domain.Tasks()`"; one line)
- Memory: `agent-sessions-feature.md` + the `MEMORY.md` line.

- [ ] **Step 1:** `rtk go test ./internal/tui -run 'TestHelpFooterCoverage|I18n|Vocab|MenuLabels|EngineProse' -count=1`. Expected: ok. If not, add the missing rows and keys.
- [ ] **Step 2:** Race gate: `./test.sh race > $WS/race.log 2>&1; tail -30 $WS/race.log`. Expected: all stages ok. A failure goes through systematic-debugging, never a retry-until-green.
- [ ] **Step 3: Live smoke on the real TUI** (`driving-tui-headless` skill; `./tui-capture.sh`, run from the worktree binary `go build -o /tmp/claude-1000/.../gg-plan3 ./cmd/gg`). Each check below runs in a throwaway repo copy (`test-1` fixture or a temp repo) in a named tmux session `claude-plan3`:
  1. Stage a file, `c`, `ctrl+g`: the dialog lists the detected agents, and the title shows `commit message — <wt> @ <sha7>`.
  2. **Headless** with a configured capture row: the spinner, then the box fills.
  3. For **each interactive row `exttool.Detect` finds on this machine** (claude, codex, junie, agy, where installed), run:
     - foreground: the console opens, the agent starts with the prompt, and writing `$GG_MESSAGE_FILE` fills the pending message or the box;
     - background: the ● row appears under Worktrees, and a notice arrives when the result lands.

     Record per agent: starts ✓/✗, result picked up ✓/✗. An agent that fails to start interactively is reported to the user; its catalogue row is not changed without asking.
  4. Review of working changes, headless: the ◆ row appears, then the report viewer opens.
  5. `ctrl+\` → `tab`: the row states and ages; `enter` on a finished task shows its result; `k k` on a running task; `x`.
  6. `q` with a running headless task: the quit popup counts it, and `Q` ends it (a history record says cancelled).
  7. Conflict: create a merge conflict, `t` → "Resolve with an agent…" → the dialog; `esc` reopens the window.
- [ ] **Step 4: Docs + commit** — `rtk git commit -m "docs: AI tasks TUI (plan 3) — help, CHANGELOG, README, CLAUDE-details"`
- [ ] **Step 5:** Final review per executing-plans (a self-review, since there are no subagents; ledger it), fixes RED→GREEN, then ask the user before merging.
