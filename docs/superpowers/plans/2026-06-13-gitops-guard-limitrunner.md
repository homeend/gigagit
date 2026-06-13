# GitOps Interface + Frontend Git-Guard + LimitRunner (CQRS Stage 4) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `OpDeps.Repo` an injectable `GitOps` interface, route the last two frontend read paths through domain so `internal/tui` and `internal/cli` no longer import `internal/git` (guarded by a test), and bound concurrent git subprocesses.

**Architecture:** `engine.GitOps` (the verbs ops use) replaces `*git.Repo` in `OpDeps`; `*git.Repo` satisfies it structurally. New gated `Service.ShowFile`/`CommitFiles`/`TopLevel` queries let the TUI and CLI drop their `internal/git` imports; `cmd/gg`/`internal/app` remain the composition root that builds concrete types. A process-global `LimitRunner` semaphore caps git subprocesses at 8.

**Tech Stack:** Go 1.26, stdlib. Spec: `docs/superpowers/specs/2026-06-13-gitops-guard-limitrunner-design.md` — read it first. Builds on stages 1–3 (`repogate`, `domain.Service`/`Snapshot`/`query`/`CommitFeed`, all on `main`; note `domain.Service` also already has a `cache.Factory`/`Differ` from the merged diff-cache feature).

**Branch:** `feat/gitops-guard` off `main`, developed in a worktree at `/mnt/t/others/gigagit.worktrees/gitops-guard`.

**Conventions:** tests first (TDD); `gofmt -w`; comments state constraints not narration; commits end with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

---

### Task 1: engine.GitOps interface + OpDeps.Repo retype

**Files:**
- Create: `internal/engine/gitops.go`
- Modify: `internal/engine/operation.go` (`OpDeps.Repo` field type)

- [ ] **Step 1: Write the failing test (compile assertion)**

Create `internal/engine/gitops.go` with ONLY the assertion first, to drive the interface out:

Actually, write the interface and assertion together (an interface has no behavior to TDD; the compile assertion IS the test). Create `internal/engine/gitops.go`:

```go
package engine

import (
	"context"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/model"
)

// GitOps is the set of git verbs operations use. *git.Repo satisfies it.
// OpDeps.Repo is this interface so operations are decoratable and mockable,
// and a new verb an op needs becomes a visible addition here.
type GitOps interface {
	Status(ctx context.Context) (model.WorkingTreeStatus, error)
	Branches(ctx context.Context) ([]model.Branch, error)
	CurrentBranch(ctx context.Context) (string, error)
	RemoteForBranch(ctx context.Context, branch string) (string, error)
	IsDirty(ctx context.Context) (bool, error)
	LastReflogSubject(ctx context.Context) (string, error)
	TopLevel(ctx context.Context) (string, error)
	Worktrees(ctx context.Context) ([]model.Worktree, error)
	WorktreeForBranch(ctx context.Context, branch string) (*model.Worktree, error)

	Fetch(ctx context.Context, remote string) error
	Pull(ctx context.Context, remote, branch string, strategy git.PullStrategy) error
	PullInWorktree(ctx context.Context, worktreePath, remote, branch string) error
	FastForwardRef(ctx context.Context, remote, branch string) error
	Push(ctx context.Context, remote, branch string, setUpstream bool) error
	Switch(ctx context.Context, branch string) error
	Commit(ctx context.Context, message string, all bool) error
	ResetSoft(ctx context.Context, ref string) error

	StashList(ctx context.Context) ([]string, error)
	StashPush(ctx context.Context, message string) error
	StashPop(ctx context.Context) error

	CheckRefFormatBranch(ctx context.Context, name string) error
	CreateBranch(ctx context.Context, name, startPoint string) error
	DeleteBranch(ctx context.Context, name string, force bool) error

	AddWorktree(ctx context.Context, path, branch, startPoint string, onLine func(string)) error
	AddWorktreeForBranch(ctx context.Context, path, branch string, onLine func(string)) error
	RemoveWorktree(ctx context.Context, path string, force bool, onLine func(string)) error

	Merge(ctx context.Context, dir, branch string) error
	MergeAbort(ctx context.Context, dir string) error
	MergeInProgress(ctx context.Context, dir string) (bool, error)
}

// Compile-time proof the concrete repo implements the interface; a drift
// fails the build.
var _ GitOps = (*git.Repo)(nil)
```

- [ ] **Step 2: Retype OpDeps.Repo**

In `internal/engine/operation.go`, change the `Repo` field:

```go
type OpDeps struct {
	Repo    GitOps
	Events  chan<- Event
	Decider Decider
	Escalate func(ctx context.Context) error
}
```

(Leave the doc comment and the rest of the struct unchanged. `operation.go` already imports `git`; if after this change `git` is unused there, drop the import — but `git` is likely still referenced; let the compiler decide.)

- [ ] **Step 3: Verify it compiles and ops still pass**

Run: `go build ./internal/engine/ && go test ./internal/engine/`
Expected: PASS. Every op body calls verbs through `deps.Repo` which is now `GitOps`; `*git.Repo` satisfies it, and op tests construct `OpDeps{Repo: someRepo}` where `someRepo` is a `*git.Repo` — still assignable. If any op references a verb NOT in `GitOps`, the build fails there — add that verb to the interface (and report it; the enumeration should be complete).

- [ ] **Step 4: Whole-build check**

Run: `go build ./...`
Expected: clean (domain passes `*git.Repo` into `OpDeps.Repo`, still assignable).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/engine && git add internal/engine && git commit -m "refactor(engine): OpDeps.Repo is a GitOps interface

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: Service.ShowFile + CommitFiles + TopLevel gated queries

**Files:**
- Modify: `internal/domain/query.go` (add three queries)
- Modify: `internal/domain/query_test.go` (tests)

- [ ] **Step 1: Write the failing tests**

Append to `internal/domain/query_test.go`:

```go
func TestShowFileGatedQuery(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git show", gitexec.Result{Stdout: "blob-bytes"})
	svc := New(&git.Repo{Runner: f})
	b, err := svc.ShowFile(context.Background(), "HEAD", "a.txt")
	if err != nil || string(b) != "blob-bytes" {
		t.Fatalf("ShowFile = %q, %v", b, err)
	}
}

func TestCommitFilesGatedQuery(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git diff-tree", gitexec.Result{Stdout: "M\ta.txt\n"})
	svc := New(&git.Repo{Runner: f})
	files, err := svc.CommitFiles(context.Background(), "abc123")
	if err != nil || len(files) != 1 || files[0].Path != "a.txt" {
		t.Fatalf("CommitFiles = %+v, %v", files, err)
	}
}

func TestTopLevelGatedQuery(t *testing.T) {
	f := fakeReads() // stage-2 helper; configures rev-parse (toplevel)
	svc := New(&git.Repo{Runner: f})
	top, err := svc.TopLevel(context.Background())
	if err != nil || top != "/repo" {
		t.Fatalf("TopLevel = %q, %v", top, err)
	}
}

func TestShowFileHoldsReadReservation(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git show", gitexec.Result{Stdout: "x"})
	svc := New(&git.Repo{Runner: f})
	var held []repogate.Entry
	// Probe the gate from inside a concurrent query is overkill here; instead
	// assert the reservation is released afterward (no leak): a TreeWrite must
	// be grantable immediately after.
	if _, err := svc.ShowFile(context.Background(), "HEAD", "a.txt"); err != nil {
		t.Fatal(err)
	}
	if q := svc.gateFor(context.Background()).Queue(); len(q) != 0 {
		t.Fatalf("reservation leaked after ShowFile: %+v", q)
	}
	_ = held
}
```

(Check the exact span names: `ShowFile` uses span `"git show"`, `CommitFiles` uses `"git diff-tree"`, `TopLevel` uses `"git rev-parse (toplevel)"`. Verify against `internal/git/*.go` and adjust the `SetResponse` keys if they differ. `fakeReads()` already configures `git rev-parse (toplevel)` → `/repo`. `model.CommitFile` has a `Path` field. `repogate` import for the reservation check.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/domain/ -run 'TestShowFile|TestCommitFiles|TestTopLevel' 2>&1 | head`
Expected: compile errors (`svc.ShowFile`/`CommitFiles`/`TopLevel` undefined).

- [ ] **Step 3: Implement**

In `internal/domain/query.go`, add three queries next to `Snapshot`/`logPage` (import `model` is already present):

```go
// ShowFile returns the raw blob of path at rev (git show rev:path), under a
// Read reservation, coalesced per (rev, path).
func (s *Service) ShowFile(ctx context.Context, rev, path string) ([]byte, error) {
	return query(ctx, s, "showfile:"+rev+":"+path, func(ctx context.Context) ([]byte, error) {
		return s.repo.ShowFile(ctx, rev, path)
	})
}

// CommitFiles returns the files changed by commit hash, under a Read
// reservation, coalesced per hash.
func (s *Service) CommitFiles(ctx context.Context, hash string) ([]model.CommitFile, error) {
	return query(ctx, s, "commit-files:"+hash, func(ctx context.Context) ([]model.CommitFile, error) {
		return s.repo.CommitFiles(ctx, hash)
	})
}

// TopLevel returns the repo's working-tree root, under a Read reservation.
func (s *Service) TopLevel(ctx context.Context) (string, error) {
	return query(ctx, s, "toplevel", func(ctx context.Context) (string, error) {
		return s.repo.TopLevel(ctx)
	})
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race ./internal/domain/` and `go vet ./internal/domain/`.
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/domain && git add internal/domain && git commit -m "feat(domain): ShowFile/CommitFiles/TopLevel gated queries

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: TUI drops internal/git — route loaders through domain

**Files:**
- Modify: `internal/tui/model.go` (`Model.repo` field, `New`, `reRoot`)
- Modify: `internal/tui/run.go` (`Run` signature)
- Modify: `internal/tui/diff_view.go` (`loadStatusDiffCmd`, `loadCommitDiffCmd` closures)
- Modify: `internal/tui/files_view.go` (`loadCommitFilesCmd`)
- Modify: any TUI test that calls `tui.New(&git.Repo{…})` or `tui.Run`

- [ ] **Step 1: Update the diff + files loaders to use svc**

In `internal/tui/diff_view.go`, both loaders currently do `repo := m.repo` and the `oldSrc`/`newSrc` closures call `repo.ShowFile(...)`. Change each to capture `svc := m.svc` and call `svc.ShowFile(...)`:

`loadStatusDiffCmd` — replace `repo := m.repo` with `svc := m.svc`, and the closure(s):
```go
		oldSrc = func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, "HEAD", p) }
```

`loadCommitDiffCmd` — same: `svc := m.svc`, and:
```go
		oldSrc = func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, hash+"^", p) }
```
```go
		newSrc = func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, hash, line.path) }
```

In `internal/tui/files_view.go`, `loadCommitFilesCmd`:
```go
func (m Model) loadCommitFilesCmd(c model.Commit) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		files, err := svc.CommitFiles(context.Background(), c.Hash)
		return commitFilesMsg{hash: c.Hash, subject: c.Subject, files: files, err: err}
	}
}
```

- [ ] **Step 2: Drop Model.repo; New/Run take *domain.Service**

In `internal/tui/model.go`:
- Remove the `repo *git.Repo` field from the `Model` struct.
- `New` takes a Service:
```go
func New(svc *domain.Service) Model {
	return Model{
		svc:       svc,
		feed:      svc.CommitFeed(),
		loading:   true,
		sel:       map[panel]int{},
		sortModes: map[panel]sortMode{panelBranches: sortDateDesc},
	}
}
```
- In `reRoot`, remove the `m.repo = m.svc.Repo()` line (keep `m.svc = domain.Open(path)` and `m.feed = m.svc.CommitFeed()`).
- Remove the now-unused `git` and (if unused) `gitexec`/`observ` imports from model.go — let the compiler guide you.

In `internal/tui/run.go`:
```go
func Run(svc *domain.Service) (string, error) {
	m := New(svc)
	// …rest unchanged…
}
```
Remove the `git` import from run.go if it becomes unused.

- [ ] **Step 3: Fix TUI tests that construct via git.Repo**

Search: `grep -rn "New(&git.Repo\|New(repo)\|Run(&git.Repo\|tui.New\|\.repo" internal/tui/*_test.go`. Every `New(&git.Repo{Runner: r})` becomes `New(domain.New(&git.Repo{Runner: r}))`. Test files MAY still import `internal/git` (the guard covers non-test package imports only — `go list .Imports` excludes test-only imports... verify: `go list` `.Imports` does NOT include test imports, `.TestImports` does; the guard checks `.Imports`, so test files importing git are fine). Any test reading `m.repo` must instead use `m.svc` / `m.svc.Repo()`.

- [ ] **Step 4: Verify**

Run: `go build ./... && go test -race ./internal/tui/ && go vet ./internal/tui/`
Expected: PASS. (`cmd/gg` will not compile yet because `tui.Run` now takes a Service — that is fixed in Task 4. If `go build ./...` fails ONLY on `cmd/gg`'s `tui.Run(repo)` call, that is expected; confirm `go build ./internal/tui/` and `go test ./internal/tui/` are green and proceed. To keep `go build ./...` green at this commit, you may apply Task 4's one-line `cmd/gg` change now — `tui.Run(domain.New(repo))` — and fold it in; otherwise note the expected cmd/gg break.)

To keep the whole build green: in `cmd/gg/main.go`, change `cwd, err := tui.Run(repo)` to `cwd, err := tui.Run(domain.New(repo))` and add the `domain` import. Include this in this commit.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/tui cmd/gg && git add internal/tui cmd/gg && git commit -m "refactor(tui): drop internal/git; loaders and entry points use domain.Service

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: CLI drops internal/git — thread *domain.Service

**Files:**
- Modify: `internal/cli/core.go` (`openRepo` → removed; `runOperation` signature)
- Modify: `internal/cli/cli.go` (`Run`, `cmdStatus`, `cmdCommit`, the `repoT` alias, dispatch)
- Modify: `internal/cli/ops.go`, `internal/cli/branch.go`, `internal/cli/merge.go`, `internal/cli/worktree.go` (command func signatures `*repoT` → `*domain.Service`)

- [ ] **Step 1: Convert the entry + helper**

In `internal/cli/cli.go`, `Run`: replace `repo := openRepo(workdir)` with `svc := domain.Open(workdir)`. The `repos.Touch` block currently does `repo.TopLevel(...)`; change to `svc.TopLevel(...)`. Thread `svc` into every `cmd*` call (replacing `repo`). Remove the `repoT = git.Repo` type alias. Remove the `internal/git` import from cli.go.

In `internal/cli/core.go`: delete `openRepo`. `runOperation` becomes:
```go
func runOperation(ctx context.Context, svc *domain.Service, op engine.Operation, dec engine.Decider, progress io.Writer) (engine.Result, error) {
	events := make(chan engine.Event, 32)
	var (
		res engine.Result
		err error
	)
	done := make(chan struct{})
	go func() {
		res, err = svc.Execute(ctx, op, events, dec)
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
	return res, err
}
```
Remove the `git` import from core.go if now unused (note: `core.go` may still reference `git` via `openRepo`'s old body — once deleted, drop the import). `cliDecider` etc. stay.

- [ ] **Step 2: Convert every command func signature**

In `branch.go`, `cli.go`, `merge.go`, `ops.go`, `worktree.go`: change each `func cmdX(repo *repoT, …)` to `func cmdX(svc *domain.Service, …)` and update the body:
- Calls `runOperation(ctx, repo, op, …)` → `runOperation(ctx, svc, op, …)`.
- `domain.New(repo).Status(…)` (cmdStatus) → `svc.Status(…)`.
- `domain.New(repo).Worktrees(…)` (cmdWorktreeList, cmdWorktreeRemove) → `svc.Worktrees(…)`.
- Internal dispatch calls (`cmdBranch` → `cmdBranchCreate(repo,…)`, `cmdWorktree` → `cmdWorktreeList(repo,…)`, etc.) pass `svc`.
- Any other direct `repo.Verb()` read in these files routes through a `svc.*` query (there should be none beyond Status/Worktrees — grep `repo\.` in cli to confirm; if a verb has no domain query, add one like Task 2, but the spec expects only Status/Worktrees/TopLevel needed).

Remove the `internal/git` import from every cli file that no longer needs it.

- [ ] **Step 3: Verify the CLI is git-free and green**

Run: `grep -rn '"github.com/gigagit/gg/internal/git"' internal/cli/*.go | grep -v _test` → expect NO output (test files may still import git).
Run: `go build ./... && go test ./internal/cli/ ./e2e/ && go vet ./internal/cli/`
Expected: PASS. (e2e drives the in-process CLI; behavior is unchanged.)

- [ ] **Step 4: Commit**

```bash
gofmt -w internal/cli && git add internal/cli && git commit -m "refactor(cli): thread domain.Service; drop internal/git

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: LimitRunner + import guard + docs

**Files:**
- Create: `internal/gitexec/limit.go`
- Create: `internal/gitexec/limit_test.go`
- Modify: `internal/domain/service.go` (`Open` wraps the runner)
- Create: `internal/archtest/import_guard_test.go` (or place in an existing neutral test package)
- Modify: `CHANGELOG.md`, `CLAUDE.md`

- [ ] **Step 1: Write the failing LimitRunner test**

Create `internal/gitexec/limit_test.go`:

```go
package gitexec

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingRunner tracks concurrent Run calls and records the peak.
type countingRunner struct {
	cur, peak int32
	release   chan struct{}
}

func (r *countingRunner) Run(ctx context.Context, name string, argv []string) (Result, error) {
	n := atomic.AddInt32(&r.cur, 1)
	for {
		p := atomic.LoadInt32(&r.peak)
		if n <= p || atomic.CompareAndSwapInt32(&r.peak, p, n) {
			break
		}
	}
	<-r.release // hold so concurrency builds up
	atomic.AddInt32(&r.cur, -1)
	return Result{}, nil
}

func (r *countingRunner) Stream(ctx context.Context, name string, argv []string, onLine func(string)) (Result, error) {
	return r.Run(ctx, name, argv)
}

func TestLimitRunnerCapsConcurrency(t *testing.T) {
	inner := &countingRunner{release: make(chan struct{})}
	lr := NewLimitRunner(inner)

	const n = gitConcurrency + 12
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); lr.Run(context.Background(), "git x", nil) }()
	}
	// Let goroutines pile up against the semaphore, then release them.
	time.Sleep(50 * time.Millisecond)
	close(inner.release)
	wg.Wait()

	if peak := atomic.LoadInt32(&inner.peak); peak > gitConcurrency {
		t.Fatalf("peak concurrency = %d, want <= %d", peak, gitConcurrency)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/gitexec/ -run TestLimitRunner 2>&1 | head`
Expected: compile error (`NewLimitRunner`, `gitConcurrency` undefined).

- [ ] **Step 3: Implement LimitRunner**

Create `internal/gitexec/limit.go`:

```go
package gitexec

import "context"

// gitConcurrency caps git subprocesses in flight across the process. 8 leaves
// the ~6-subprocess startup fan-out unthrottled while capping future fan-out
// (concurrent ops, group sync) so a slow 100GB repo is not hit by dozens of
// simultaneous git processes.
const gitConcurrency = 8

// gitSem is process-global so every ExecRunner shares one ceiling.
var gitSem = make(chan struct{}, gitConcurrency)

// LimitRunner wraps a Runner, bounding concurrent Run/Stream calls by the
// process-global git subprocess ceiling.
type LimitRunner struct{ inner Runner }

// NewLimitRunner returns inner wrapped with the concurrency bound.
func NewLimitRunner(inner Runner) Runner { return &LimitRunner{inner: inner} }

func (l *LimitRunner) Run(ctx context.Context, name string, argv []string) (Result, error) {
	gitSem <- struct{}{}
	defer func() { <-gitSem }()
	return l.inner.Run(ctx, name, argv)
}

func (l *LimitRunner) Stream(ctx context.Context, name string, argv []string, onLine func(string)) (Result, error) {
	gitSem <- struct{}{}
	defer func() { <-gitSem }()
	return l.inner.Stream(ctx, name, argv, onLine)
}
```

- [ ] **Step 4: Wire it in domain.Open**

In `internal/domain/service.go`, `Open` wraps the ExecRunner:

```go
func Open(workdir string) *Service {
	s := New(&git.Repo{Runner: gitexec.NewLimitRunner(gitexec.NewExecRunner("git", workdir, observ.NewRing(200)))})
	s.workdir = workdir
	return s
}
```

- [ ] **Step 5: Write the import guard**

Create `internal/archtest/import_guard_test.go`:

```go
package archtest

import (
	"os/exec"
	"strings"
	"testing"
)

// TestFrontendsDoNotImportGit: internal/tui and internal/cli must reach git
// only through internal/domain, never by a direct import. cmd/gg and
// internal/app are the composition root / wiring layer and are exempt.
func TestFrontendsDoNotImportGit(t *testing.T) {
	const forbidden = "github.com/gigagit/gg/internal/git"
	for _, pkg := range []string{
		"github.com/gigagit/gg/internal/tui",
		"github.com/gigagit/gg/internal/cli",
	} {
		out, err := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, pkg).Output()
		if err != nil {
			t.Fatalf("go list %s: %v", pkg, err)
		}
		for _, imp := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if imp == forbidden {
				t.Errorf("%s directly imports %s — frontends must reach git through internal/domain", pkg, forbidden)
			}
		}
	}
}
```

- [ ] **Step 6: Run the new tests + whole suite**

Run: `go test -race ./internal/gitexec/ ./internal/archtest/`
Expected: PASS (LimitRunner caps at 8; neither frontend imports git).
Run: `./test.sh race`
Expected: `all green`.

- [ ] **Step 7: Docs**

`CHANGELOG.md` — append to the `#### Domain layer & repo gate` subsection:

```markdown
- Stage 4: `engine.OpDeps.Repo` is now a `GitOps` interface (operations are
  decoratable and mockable). The TUI and CLI no longer import `internal/git` —
  the last raw reads (`ShowFile`, `CommitFiles`, `TopLevel`) go through gated
  domain queries, enforced by an import-guard test. Concurrent git
  subprocesses are bounded (process-global ceiling of 8).
```

`CLAUDE.md`:
- `engine` package-map row: append "Ops act on a `GitOps` interface (`*git.Repo` satisfies it)."
- `gitexec` row: append "`LimitRunner` bounds concurrent git subprocesses."
- `domain` row: add `ShowFile`/`CommitFiles`/`TopLevel` to the query list.
- Conventions: add a bullet — "**`internal/tui` and `internal/cli` never import `internal/git`** — they reach git through `internal/domain` (guarded by `internal/archtest`). `cmd/gg` and `internal/app` are the composition root and may construct concrete git types."

- [ ] **Step 8: Commit**

```bash
gofmt -w internal/gitexec internal/domain internal/archtest && git add internal/gitexec internal/domain internal/archtest CHANGELOG.md CLAUDE.md && git commit -m "feat(gitexec): LimitRunner subprocess bound; add frontend git import guard; docs

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Plan self-review notes

- **Spec coverage:** GitOps interface + retype + compile assertion (Task 1); ShowFile/CommitFiles/TopLevel queries (Task 2); TUI git-removal + loaders + New/Run (Task 3); CLI git-removal + threading (Task 4); LimitRunner + import guard + docs (Task 5). Spec non-goals (write-assert, blob cache, role-split interfaces, removing Repo()) → no tasks.
- **Type consistency:** `GitOps` (Task 1) ↔ `*git.Repo` satisfies (asserted); `Service.ShowFile/CommitFiles/TopLevel` (Task 2) ↔ consumed by TUI loaders (Task 3) and CLI (Task 4); `NewLimitRunner`/`gitConcurrency` (Task 5) ↔ test + `domain.Open`. `tui.New(svc)`/`tui.Run(svc)` (Task 3) ↔ `cmd/gg` call (Task 3/4).
- **Green-build ordering:** Task 1 keeps `go build ./...` green (`*git.Repo` satisfies the interface). Task 3 folds the one-line `cmd/gg` `tui.Run(domain.New(repo))` fix to stay green. Task 4 finishes the CLI. Each task's package tests pass at its commit.
- **Guard granularity:** uses `go list .Imports` (direct, non-test) so `internal/tui`→`internal/domain`→`internal/git` is allowed and test files importing git are fine; only a direct frontend→git import fails.
- **Verify-before-implement:** Task 2 flags confirming the span names (`git show`, `git diff-tree`, `git rev-parse (toplevel)`) against `internal/git`. Task 3/4 flag grepping for stray `repo.` reads and dropping now-unused imports compiler-guided.
