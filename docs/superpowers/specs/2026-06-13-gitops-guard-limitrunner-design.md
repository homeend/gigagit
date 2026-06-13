# GitOps interface, frontend git-removal + guard, LimitRunner (CQRS stage 4) — design

Date: 2026-06-13
Status: approved

## Context

Stage 4 of the CQRS refactor (1 = `repogate`+`domain.Execute`; 2 =
`domain.Snapshot`/`Status`/`Worktrees`; 3 = `domain.CommitFeed`). A parallel
feature also landed a diff cache: `domain.Service` now holds a `cache.Factory`
and a `Differ`, and the TUI computes diffs through `m.svc.Differ()` — but the
raw blob reads (`ShowFile`) and the commit file list (`CommitFiles`) still
call git verbs directly on `m.repo`.

This is a hardening stage. It (A) makes `OpDeps.Repo` an interface so engine
operations are decoratable/mockable and new verb usage is visible; (B) routes
the last two frontend read paths through domain and removes `internal/git`
from the frontend packages, guarded by a test; (C) bounds concurrent git
subprocesses with a decorator. No user-visible behavior change.

## A. `engine.GitOps` interface

The consumer defines the interface it needs. `internal/engine` gains:

```go
// GitOps is the set of git verbs operations use. *git.Repo satisfies it.
// OpDeps.Repo is this interface so ops are decoratable and mockable, and a
// new verb an op needs becomes a visible addition here.
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
```

(29 verbs — exactly those ops call today, verified by enumerating
`deps.Repo.X(` across `internal/engine`.) `OpDeps.Repo` changes from
`*git.Repo` to `GitOps`. `*git.Repo` already implements every method, so no
operation body changes. Engine keeps importing `git` for `git.PullStrategy`
(the `PullFF/PullRebase/PullMerge` consts) — that is fine; the goal is the
injectable interface, not severing engine→git.

A compile-time assertion `var _ GitOps = (*git.Repo)(nil)` lives in the engine
package so a drift between the interface and the concrete repo fails the
build.

Value: ops become decoratable (future per-op instrumentation), unit-testable
against a fake GitOps, and a new verb is a visible interface line.

## B. Frontend `internal/git` removal + import guard

### New gated queries (domain)

Two reads, same `query[T]` machinery as `Snapshot`/`logPage` (Read
reservation + singleflight):

```go
// ShowFile returns the raw blob of path at rev (git show rev:path).
func (s *Service) ShowFile(ctx context.Context, rev, path string) ([]byte, error)
//   = query(ctx, s, "showfile:"+rev+":"+path, func(ctx) { return s.repo.ShowFile(ctx, rev, path) })

// CommitFiles returns the files changed by commit hash.
func (s *Service) CommitFiles(ctx context.Context, hash string) ([]model.CommitFile, error)
//   = query(ctx, s, "commit-files:"+hash, func(ctx) { return s.repo.CommitFiles(ctx, hash) })
```

These gate the raw reads (the computed diff already caches via the Differ;
adding a blob cache here is stage 6, not now).

### TUI changes

- The diff loaders (`loadStatusDiffCmd`, `loadCommitDiffCmd` in
  `diff_view.go`): the `oldSrc`/`newSrc` `domain.ByteSource` closures call
  `svc.ShowFile(ctx, rev, p)` instead of `repo.ShowFile`.
- `loadCommitFilesCmd` (`files_view.go`) calls `svc.CommitFiles(ctx, c.Hash)`.
- **Drop `m.repo *git.Repo` from the Model.** It holds only `svc
  *domain.Service`; the loader closures capture `svc := m.svc`. `m.svc.Repo()`
  is no longer called from the TUI.
- `tui.New(svc *domain.Service)` and `tui.Run(svc *domain.Service)` take a
  Service instead of `*git.Repo`. `reRoot` already builds via `domain.Open`;
  it just stops setting `m.repo`.

### CLI changes

- `cli.Run` builds `svc := domain.Open(workdir)` once and threads
  `*domain.Service` to the command funcs (replacing the `*repoT` parameter).
  `openRepo` is removed.
- `cmdStatus`/`cmdWorktree*` already call `domain.New(repo)` internally; they
  now take `svc` directly. `runOperation(ctx, repo, …)` →
  `runOperation(ctx, svc, …)`.
- The best-effort `repos.Touch` toplevel read in `cli.Run` (currently
  `repo.TopLevel`) routes through a new gated `Service.TopLevel(ctx)
  (string, error)` query (added alongside `Status`/`Worktrees`).
- `internal/cli` no longer imports `internal/git`; the `repoT = git.Repo`
  alias is removed.

### cmd/gg (composition root — NOT guarded)

`cmd/gg/main.go` legitimately constructs concrete types (it is `main`). It
builds the ring + `repo := &git.Repo{Runner: gitexec.NewExecRunner("git",
".", ring)}`, wraps `svc := domain.New(repo)`, passes `svc` to `tui.Run(svc)`,
and keeps `repo`+`ring` for the panic `app.DumpRepo`. The gate keys on the
common dir (resolved on first use), so `domain.New(repo)` (no workdir) is
correct for the single-repo TUI launch. `internal/app` (the wiring layer,
home of `DumpRepo`) also keeps its `git.Repo` parameter.

### The guard

A test (in a neutral package, e.g. `internal/archtest` or
`cmd/gg`-adjacent — placed where it can `go list`) that fails if the
transitive import graph of `internal/tui` or `internal/cli` contains
`github.com/gigagit/gg/internal/git`:

Direct-import granularity (not transitive) is the intent — `internal/tui`
importing `internal/domain`, which imports `internal/git`, is fine; only a
*direct* frontend→git import is forbidden. The test lists each package's OWN
imports:

```go
func TestFrontendsDoNotImportGit(t *testing.T) {
	const forbidden = "github.com/gigagit/gg/internal/git"
	for _, pkg := range []string{"internal/tui", "internal/cli"} {
		out, err := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, "./"+pkg).Output()
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

Scope: **`internal/tui` and `internal/cli` only.** `cmd/gg` and
`internal/app` are the composition root and wiring layer; they construct
concrete `git`/`observ` types by design (standard composition-root pattern).

## C. LimitRunner — process-global subprocess bound

A `gitexec.Runner` decorator:

```go
// LimitRunner caps concurrent git subprocesses across the process. The
// semaphore is package-global so every ExecRunner (current and future
// group-sync Services) shares one ceiling — a slow 100GB repo must not face
// 20 simultaneous git processes.
func NewLimitRunner(inner Runner) Runner

// gitConcurrency is the ceiling. 8 leaves the ~6-subprocess startup fan-out
// (Snapshot + feed) unthrottled while capping future fan-out.
const gitConcurrency = 8
```

`Run`/`Stream` acquire a token from a package-level `chan struct{}` (size 8)
before delegating to `inner`, release after. `domain.Open` wraps its
`ExecRunner`: `NewLimitRunner(gitexec.NewExecRunner("git", workdir, ring))`.
`FakeRunner` is untouched (tests use the fake, not the decorator); the bound
only applies to real subprocesses.

**No write-assertion.** The bundled debug assert (mutating argv never runs
without a TreeWrite reservation) would require threading the reservation mode
through `context` from `domain.Execute` → op → verb → `Runner.Run`. That is
real plumbing for a check already covered belt-and-suspenders by the gate
(`LockMode` reservations) and by "ops are only built inside domain". Deferred;
revisit at stage 5 where concurrent ops make it catch something.

## Testing

- `engine`: `var _ GitOps = (*git.Repo)(nil)` compile assertion; existing op
  tests pass unchanged (they pass a `*git.Repo`, which satisfies `GitOps`). A
  small test that a fake `GitOps` can drive an op (e.g. a trivial op with a
  stub) — optional; the compile assertion + green suite is the core proof.
- `domain`: `ShowFile`/`CommitFiles`/`TopLevel` each take a Read reservation
  (Queue() observed mid-call) and return the verb result; coalesce on their
  keys. (Same style as stage-2 query tests.)
- `gitexec`: `LimitRunner` never lets more than `gitConcurrency` inner `Run`s
  run at once (a counting inner runner + N>8 concurrent calls asserts the peak
  ≤ 8); it delegates results/errors faithfully.
- `internal/tui` + `internal/cli`: existing suites pass after the rewrite
  (behavior-preserving); the new import-guard test passes (and would fail if a
  frontend re-imported `internal/git`).
- Full `./test.sh race` green.

## Docs

- `CHANGELOG.md`: a stage-4 bullet under "Domain layer & repo gate" —
  `OpDeps` now an interface; frontend reads fully behind domain (guarded);
  concurrent git subprocesses bounded.
- `CLAUDE.md`: package-map `engine` row notes `GitOps`; `gitexec` row notes
  `LimitRunner`; a convention line that `internal/tui`/`internal/cli` never
  import `internal/git` (guarded by test). `domain` row: `ShowFile`/
  `CommitFiles`/`TopLevel` added to the query list; drop the stage-2 "Repo()
  removed at stage 4" note's "transitional" framing if `Repo()` is removed.
- `Service.Repo()`: keep it (the composition root / app DumpRepo path and
  tests use a repo), but it is no longer called from `internal/tui`/`cli`.

## Decomposition (5 tasks)

1. `engine.GitOps` interface + `OpDeps.Repo` retype + compile assertion.
2. `Service.ShowFile` + `Service.CommitFiles` + `Service.TopLevel` gated
   queries (+ tests).
3. TUI: route diff/files loaders through the new queries; drop `m.repo`;
   `New`/`Run` take `*domain.Service`.
4. CLI: `openService` + thread `*domain.Service`; remove the `git` import +
   `repoT` alias; `cmd/gg` wires `domain.New(repo)` and keeps the dump path.
5. `LimitRunner` (+ test) wired in `domain.Open`; import-guard test; docs;
   full gate.

## Not doing (YAGNI)

The LimitRunner write-assertion (stage 5); a blob cache for `ShowFile`
(stage 6); role-split GitOps interfaces (one fat interface is simplest);
guarding `cmd/gg`/`internal/app` (composition root); removing `Service.Repo()`
(still used by the wiring/test paths).
