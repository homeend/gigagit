# F1 Staging Surface Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stage/unstage changed files with `space` in the TUI Status panel, with a status-only refresh, plus the index git-verb foundation (`add` / `restore --staged`) that F2/F3/F4 reuse.

**Architecture:** Two new dir-less git verbs (`StagePaths`/`UnstagePaths`) added to the `engine.GitOps` interface; an `engine.Stage{Paths, Unstage}` operation run via `domain.Execute` (default TreeWrite reservation); a dedicated TUI `stageCmd` that runs the op and refreshes **only** `domain.Status` (not the full snapshot) so repeated staging stays snappy on huge repos; `space` wired in the Status panel with a `canStage` predicate, footer hint, and help row.

**Tech Stack:** Go 1.26, `internal/gitcmd` argv builder, `internal/gitexec` FakeRunner, Bubble Tea TUI.

**Spec:** `docs/superpowers/specs/2026-06-14-staging-surface-design.md`

**Conventions reminder:**
- A git verb is one invocation built with `gitcmd`, run via `r.Runner.Run`.
- Writes run as engine `Operation`s through `domain.Execute`; `internal/tui` never imports `internal/git`.
- Tests use a real `git` in a `t.TempDir()` (`newRepo`/`newTestRepo`/`newRepoDir`) or `FakeRunner` for argv.
- Gate before merge: `./test.sh race`.
- Commits end with `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

**Toggle rule (from the spec):** for the selected Status file — if it has any unstaged content (untracked, or `Unstaged` byte is not `.`/0) → **stage** (`git add`); if it is fully staged (nothing unstaged) → **unstage** (`git restore --staged`); if conflicted (`Kind == KindUnmerged`) → **no-op** with a statusMsg (mark-resolved is F4).

---

### Task 1: git stage/unstage verbs

**Files:**
- Create: `internal/git/stage.go`
- Test: `internal/git/stage_test.go`

Mirrors the `gitcmd` + `Runner.Run` pattern of `internal/git/merge.go`. `--`
guards path args that could look like flags. Both take a slice so F3 / a future
stage-all can reuse them.

- [ ] **Step 1: Write the failing argv + real-repo tests**

Create `internal/git/stage_test.go`:

```go
package git

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gigagit/gg/internal/gitexec"
)

func TestStagePathsArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git add", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.StagePaths(context.Background(), []string{"a.go", "b.go"}); err != nil {
		t.Fatalf("stage: %v", err)
	}
	want := []string{"add", "--", "a.go", "b.go"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestUnstagePathsArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git restore --staged", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.UnstagePaths(context.Background(), []string{"a.go"}); err != nil {
		t.Fatalf("unstage: %v", err)
	}
	want := []string{"restore", "--staged", "--", "a.go"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

// Real-git: stage an unstaged modification, then unstage it, asserting the
// porcelain transitions via the existing Status verb.
func TestStageUnstageReal(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	// README.md exists from newTestRepo; modify it (unstaged).
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)

	if err := repo.StagePaths(context.Background(), []string{"README.md"}); err != nil {
		t.Fatalf("stage: %v", err)
	}
	st, _ := repo.Status(context.Background())
	if findStaged(st, "README.md") == '.' {
		t.Fatal("README.md should be staged after StagePaths")
	}

	if err := repo.UnstagePaths(context.Background(), []string{"README.md"}); err != nil {
		t.Fatalf("unstage: %v", err)
	}
	st, _ = repo.Status(context.Background())
	if findStaged(st, "README.md") != '.' {
		t.Fatal("README.md should be unstaged after UnstagePaths")
	}
}

// findStaged returns the Staged porcelain byte for path ('.' if absent/clean).
func findStaged(st model.WorkingTreeStatus, path string) byte {
	for _, f := range st.Files {
		if f.Path == path {
			if f.Staged == 0 {
				return '.'
			}
			return f.Staged
		}
	}
	return '.'
}
```

Add the import `"github.com/gigagit/gg/internal/model"` to the test file (used
by `findStaged`).

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/git/ -run 'TestStage|TestUnstage' -v`
Expected: FAIL — `r.StagePaths`/`r.UnstagePaths` undefined.

- [ ] **Step 3: Implement the verbs**

Create `internal/git/stage.go`:

```go
package git

import (
	"context"

	"github.com/gigagit/gg/internal/gitcmd"
)

// StagePaths stages the given paths into the index (git add). The "--" guard
// keeps a path that looks like a flag from being parsed as one.
func (r *Repo) StagePaths(ctx context.Context, paths []string) error {
	b := gitcmd.New("add").Arg("--").Arg(paths...)
	_, err := r.Runner.Run(ctx, "git add", b.ToArgv())
	return err
}

// UnstagePaths removes the given paths from the index, keeping working-tree
// content (git restore --staged).
func (r *Repo) UnstagePaths(ctx context.Context, paths []string) error {
	b := gitcmd.New("restore").Arg("--staged", "--").Arg(paths...)
	_, err := r.Runner.Run(ctx, "git restore --staged", b.ToArgv())
	return err
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/git/ -run 'TestStage|TestUnstage' -v`
Expected: PASS (all three).

- [ ] **Step 5: Commit**

```bash
git add internal/git/stage.go internal/git/stage_test.go
git commit -m "feat(git): StagePaths and UnstagePaths verbs

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: add the verbs to the GitOps interface

**Files:**
- Modify: `internal/engine/gitops.go`

- [ ] **Step 1: Add the methods**

In `internal/engine/gitops.go`, after the rebase verbs (the
`RebaseInProgress` line, last methods before the closing brace), add:

```go
	StagePaths(ctx context.Context, paths []string) error
	UnstagePaths(ctx context.Context, paths []string) error
```

- [ ] **Step 2: Verify the build (the compile assertion is the test)**

Run: `go build ./internal/engine/`
Expected: clean — `*git.Repo` has the methods from Task 1, so
`var _ GitOps = (*git.Repo)(nil)` holds.

- [ ] **Step 3: Run engine tests**

Run: `go test ./internal/engine/`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/engine/gitops.go
git commit -m "feat(engine): expose stage/unstage verbs on GitOps

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: the Stage operation

**Files:**
- Create: `internal/engine/stage.go`
- Test: `internal/engine/stage_test.go`

A minimal `Operation`: no decisions, no events beyond a Progress. It uses the
default TreeWrite reservation (no `LockMode` method needed). Reuses the engine
test helpers `newRepo`, `gitE`, `gitOut`.

- [ ] **Step 1: Write the failing test**

Create `internal/engine/stage_test.go`:

```go
package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageStagesFile(t *testing.T) {
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)

	res, err := Stage{Paths: []string{"new.txt"}}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "staged new.txt") {
		t.Fatalf("result = %+v", res)
	}
	// new.txt is now in the index (added).
	if out := gitOut(t, dir, "diff", "--cached", "--name-only"); !strings.Contains(out, "new.txt") {
		t.Fatalf("new.txt not staged; cached names = %q", out)
	}
}

func TestStageUnstagesFile(t *testing.T) {
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)
	gitE(t, dir, "add", "new.txt")

	res, err := Stage{Paths: []string{"new.txt"}, Unstage: true}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("unstage: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "unstaged new.txt") {
		t.Fatalf("result = %+v", res)
	}
	if out := gitOut(t, dir, "diff", "--cached", "--name-only"); strings.Contains(out, "new.txt") {
		t.Fatalf("new.txt still staged; cached names = %q", out)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/engine/ -run TestStage -v`
Expected: FAIL — `Stage` undefined.

- [ ] **Step 3: Implement the operation**

Create `internal/engine/stage.go`:

```go
package engine

import (
	"context"
	"fmt"
	"strings"
)

// Stage stages (or, with Unstage, unstages) the given paths in the index. It
// takes no decisions and emits a single Progress; the default TreeWrite
// reservation applies (it writes .git/index).
type Stage struct {
	Paths   []string
	Unstage bool
}

var _ Operation = Stage{}

func (op Stage) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if len(op.Paths) == 0 {
		return Result{}, fmt.Errorf("stage: no paths")
	}
	verb := "staged"
	if op.Unstage {
		verb = "unstaged"
	}
	deps.emit(ctx, Progress{Step: verb, Detail: strings.Join(op.Paths, " ")})
	var err error
	if op.Unstage {
		err = deps.Repo.UnstagePaths(ctx, op.Paths)
	} else {
		err = deps.Repo.StagePaths(ctx, op.Paths)
	}
	if err != nil {
		return Result{}, fmt.Errorf("stage: %w", err)
	}
	return Result{Summary: verb + " " + strings.Join(op.Paths, " "), Changed: true}, nil
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/engine/ -run TestStage -v`
Expected: PASS (both).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/stage.go internal/engine/stage_test.go
git commit -m "feat(engine): Stage operation (stage/unstage paths)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: TUI wiring — `space` stages/unstages with a status-only refresh

**Files:**
- Modify: `internal/tui/avail.go` (add `canStage`)
- Modify: `internal/tui/op.go` (add `stageCmd` + `statusRefreshedMsg`)
- Modify: `internal/tui/model.go` (space dispatch + `statusRefreshedMsg` handler)
- Modify: `internal/tui/footer.go` (hint), `internal/tui/help.go` (help row)
- Test: `internal/tui/stage_test.go`

- [ ] **Step 1: Write the failing TUI test**

Create `internal/tui/stage_test.go`:

```go
package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/domain"
)

// stageTestModel: a loaded model on a repo with one unstaged modification,
// focused on the Status panel with that file selected.
func stageTestModel(t *testing.T) (Model, string) {
	t.Helper()
	dir, repo := newRepoDir(t)
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
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644)
	run("status") // ensure the worktree is observable

	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelStatus
	m.sel[panelStatus] = 0
	return m, dir
}

// drive a stageCmd to completion (it returns a single statusRefreshedMsg).
func driveStage(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a stage command")
	}
	msg := cmd()
	updated, _ := m.Update(msg)
	return updated.(Model)
}

func TestSpaceStagesSelectedFile(t *testing.T) {
	m, _ := stageTestModel(t)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	m = driveStage(t, m, cmd)
	// README.md should now report a staged byte.
	var staged byte = '.'
	for _, f := range m.status.Files {
		if f.Path == "README.md" {
			staged = f.Staged
		}
	}
	if staged == '.' || staged == 0 {
		t.Fatalf("README.md not staged after space; staged byte = %q", staged)
	}
	if m.running {
		t.Fatal("running must be cleared after the status refresh")
	}
}

func TestSpaceUnstagesFullyStagedFile(t *testing.T) {
	m, dir := stageTestModel(t)
	// stage it first (outside the model) and reload so the model sees it staged.
	cmd := exec.Command("git", "add", "README.md")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	cmd.Run()
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelStatus
	m.sel[panelStatus] = 0

	updated, c := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = driveStage(t, updated.(Model), c)
	for _, f := range m.status.Files {
		if f.Path == "README.md" && f.Staged != '.' && f.Staged != 0 {
			t.Fatalf("README.md should be unstaged; staged byte = %q", f.Staged)
		}
	}
}
```

(If `newRepoDir(t)` in the tui package returns only a dir or a different
signature than `(dir, repo)`, match the signature used by
`TestPairPopupEnterRunsSmartMerge` in `mark_test.go` — it is the canonical
helper and returns `(dir, repo)`.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run 'TestSpaceStages|TestSpaceUnstages' -v`
Expected: FAIL — `canStage`/`stageCmd`/`statusRefreshedMsg` undefined or space does nothing.

- [ ] **Step 3: Add the `canStage` predicate**

In `internal/tui/avail.go`, add:

```go
// canStage reports whether the selected Status row can be staged/unstaged:
// the Status panel is focused, a row is selected, and no op is running.
func (m Model) canStage() bool {
	if m.focus != panelStatus || !m.opsIdle() {
		return false
	}
	_, ok := m.backingIndex(panelStatus)
	return ok
}
```

- [ ] **Step 4: Add `stageCmd` + `statusRefreshedMsg`**

In `internal/tui/op.go`, add (keep the existing imports; add `"github.com/gigagit/gg/internal/model"` and `"github.com/gigagit/gg/internal/domain"` only if not already imported — `domain` is used via `m.svc`'s type, which is `*domain.Service`; check the file's imports):

```go
// statusRefreshedMsg carries the result of a staging op plus a fresh Status
// read. It refreshes ONLY the Status panel (not the full snapshot) so repeated
// staging stays snappy on huge repos.
type statusRefreshedMsg struct {
	summary string
	status  model.WorkingTreeStatus
	err     error
}

// stageCmd runs a staging op through the domain layer, then re-reads only the
// working-tree status. It is synchronous inside the returned tea.Cmd (staging
// is fast and has no decisions), so the result is a single statusRefreshedMsg.
func (m Model) stageCmd(op engine.Operation) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		res, err := svc.Execute(context.Background(), op, nil, nil)
		if err != nil {
			return statusRefreshedMsg{err: err}
		}
		st, serr := svc.Status(context.Background())
		return statusRefreshedMsg{summary: res.Summary, status: st, err: serr}
	}
}
```

Add the needed imports to `op.go`: `"github.com/gigagit/gg/internal/model"`
(for `model.WorkingTreeStatus`). `context`, `engine`, and `tea` are already
imported.

- [ ] **Step 5: Wire the `space` dispatch + the refresh handler in `model.go`**

In `internal/tui/model.go`, in the `tea.KeyMsg` case, **immediately before**
the `switch msg.String()` line (currently `switch msg.String() {` after the
filter-typing block), add the space guard:

```go
		if msg.Type == tea.KeySpace {
			return m.handleStageKey()
		}
		switch msg.String() {
```

Then add the `handleStageKey` method (near the other key helpers in
`model.go`, e.g. after `reRoot`):

```go
// handleStageKey toggles staging of the selected Status row: stage if the file
// has any unstaged content (untracked, or an unstaged porcelain byte), else
// unstage. Conflicted files are a no-op here (mark-resolved is F4).
func (m Model) handleStageKey() (tea.Model, tea.Cmd) {
	if !m.canStage() {
		return m, nil
	}
	bi, _ := m.backingIndex(panelStatus)
	f := m.status.Files[bi]
	if f.Kind == model.KindUnmerged {
		m.statusMsg = "resolve conflicts first"
		return m, nil
	}
	hasUnstaged := f.Kind == model.KindUntracked || (f.Unstaged != '.' && f.Unstaged != 0)
	m.running = true
	m.statusMsg = "working…"
	return m, m.stageCmd(engine.Stage{Paths: []string{f.Path}, Unstage: !hasUnstaged})
}
```

In the same file's message `switch` (alongside `case opFinishedMsg:`), add:

```go
	case statusRefreshedMsg:
		m.running = false
		if msg.err != nil {
			m.statusMsg = "error: " + msg.err.Error()
			return m, nil
		}
		m.status = msg.status
		if msg.summary != "" {
			m.statusMsg = msg.summary
		}
		if n := m.panelLen(panelStatus); n > 0 && m.sel[panelStatus] >= n {
			m.sel[panelStatus] = n - 1
		}
		return m, nil
```

(`model` is already imported in `model.go`.)

- [ ] **Step 6: Run the TUI staging tests**

Run: `go test ./internal/tui/ -run 'TestSpaceStages|TestSpaceUnstages' -v`
Expected: PASS.

- [ ] **Step 7: Add the footer hint + help row**

In `internal/tui/footer.go`, add to `contextBindings` (the focus-specific
slice, next to the `enter`/`[enter] diff` entry):

```go
	{"space", "[space] stage", Model.canStage},
```

In `internal/tui/help.go`, add a row in the Status-panel section of
`helpContent()` (next to the file-diff/history rows):

```go
		r("space", "stage / unstage the selected file"),
```

- [ ] **Step 8: Run the full TUI package**

Run: `go test ./internal/tui/`
Expected: PASS, including `TestHelpFooterCoverage` (the new global/contextual
key now has a help row).

- [ ] **Step 9: Commit**

```bash
git add internal/tui/avail.go internal/tui/op.go internal/tui/model.go internal/tui/footer.go internal/tui/help.go internal/tui/stage_test.go
git commit -m "feat(tui): space stages/unstages in the Status panel (status-only refresh)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: docs

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `README.md` (only if it documents TUI keybindings)

- [ ] **Step 1: CHANGELOG entry**

Add under `## [Unreleased]` → `### Added` (top of the list), matching the
existing format:

```markdown
#### Staging (`space` in the Status panel)
- `space` on a Status-panel file stages it (`git add`), or unstages it
  (`git restore --staged`) when it is already fully staged; conflicted files
  are skipped (resolution lands with the conflict feature). The Status panel
  refreshes on its own without a full reload.
```

- [ ] **Step 2: README keybindings (conditional)**

Run: `grep -n "enter.*diff\|Keys\|keybinding\|^| *\`" README.md`
If README has a TUI key list/table, add a `space` → "stage / unstage selected
file" row in the same format. If it does not enumerate TUI keys, skip this step.

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md README.md
git commit -m "docs: staging (space) in CHANGELOG/README

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

> No `agentskill.Version` bump: F1 is TUI-only and changes no CLI surface.

---

### Final verification (after all tasks)

- [ ] **Run the staged suite with the race detector**

Run: `./test.sh race`
Expected: vet+gofmt clean, all unit tests pass, all e2e scenarios pass.

- [ ] **Dispatch the final holistic code review**, then use
  `superpowers:finishing-a-development-branch`. **After merge, RE-RUN
  `./test.sh race` ON MERGED MAIN** — parallel sessions land features and a
  clean textual merge can still break the build or surface a flaky test.

---

## Self-Review

**1. Spec coverage:**
- `space` toggle in Status panel → Task 4 (`handleStageKey`). ✓
- Toggle rule (untracked/unstaged → stage; fully staged → unstage; conflicted → no-op) → Task 4 `handleStageKey`. ✓
- New verbs `StagePaths`/`UnstagePaths` → Task 1; on `GitOps` → Task 2. ✓
- `engine.Stage{Paths, Unstage}` via Execute (TreeWrite default) → Task 3. ✓
- Status-only refresh (not full snapshot) → Task 4 (`stageCmd` + `statusRefreshedMsg`, no `loadCmd`). ✓
- Footer hint + help row → Task 4 Step 7. ✓
- Scope fences (commit/amend F2, hunks F3, mark-resolved F4) → respected; conflicted is a no-op, no commit path added. ✓
- Docs, no agentskill bump → Task 5. ✓

**2. Placeholder scan:** No TBD/TODO; every code step shows complete code; the one conditional (README) gives a concrete grep-and-decide rule. ✓

**3. Type consistency:** `StagePaths(ctx, []string)` / `UnstagePaths(ctx, []string)` identical across Task 1 impl, Task 2 interface, and Task 3's `deps.Repo` calls. `engine.Stage{Paths []string, Unstage bool}` consistent across Task 3 and Task 4. `statusRefreshedMsg{summary, status, err}` consistent between `op.go` (Task 4 Step 4) and the `model.go` handler (Step 5). `canStage` defined in Task 4 Step 3 and used by `handleStageKey` (Step 5) and the footer (Step 7). ✓
