# Snapshot query service (CQRS stage 2) — design

Date: 2026-06-13
Status: approved

## Context

Stage 2 of the CQRS refactor (stage 1 = `internal/repogate` + `internal/domain.Execute`,
merged `7cacd8d`). Stage 1 routed every **command** (engine operation)
through `domain.Execute` under an operation-granularity reservation. Stage 2
does the same for **reads**: it introduces domain query methods so frontends
stop calling `internal/git` verbs directly, runs the TUI's 7-read startup
snapshot in parallel under one Read reservation, and coalesces concurrent
identical reads with singleflight.

The win is measured (`docs/perf/2026-06-13-startup-timing.md`): `loadCmd`
runs its 7 git reads strictly sequentially, ~2.3–3.1 s on a slow filesystem.
Most of that is the `git status` long pole (1–3 s); the other six reads add
up to roughly a second of avoidable serial latency. Running the independent
reads concurrently collapses wall time to ≈ the `git status` cost alone.

Roadmap reminder (stages 3–6 unchanged): 3 = `commitfeed` paged history;
4 = OpDeps→`engine.GitOps` interface + frontend `internal/git` import guard +
`LimitRunner`; 5 = concurrent commands (queue UI, op-ID-tagged events); 6 =
snapshot cache (TTL/fswatch, measurement-gated).

## The read dependency graph

`loadCmd` (`internal/tui/load.go`) issues seven git reads plus two non-git
steps. Only three reads depend on another's result:

- **Independent** (fire concurrently): `Status`, `Branches`, `Log`,
  `Worktrees`, `TopLevel`, `GitCommonDir` — six git reads.
- **Dependent**: `CommitTimes(shas)` needs the worktree HEAD shas, so it
  runs after `Worktrees` returns.
- **Not git, stays in the TUI**: `config.Load(top)` and `repos.Touch(top)`.
  They are not git reads and (per the stage-1 spec) do not belong under a
  repo reservation. The TUI performs them itself after `Snapshot` returns,
  using the `top` the snapshot reports.

Resulting wall time ≈ `max(Status, Worktrees+CommitTimes, Branches, Log,
TopLevel, GitCommonDir)` ≈ `Status` (CommitTimes ~80 ms waiting on Worktrees
~95 ms is far under Status's 1–3 s).

## API (`internal/domain`)

```go
// Snapshot is the git-read half of a TUI load — everything loadCmd needs
// from git in one parallel, gated, coalesced fetch. config.Load and
// repos.Touch are NOT here: they aren't git reads and stay in the frontend.
type Snapshot struct {
	Status          model.WorkingTreeStatus
	Branches        []model.Branch
	Commits         []model.Commit
	Worktrees       []model.Worktree
	CurrentWorktree string // git toplevel ("" if TopLevel failed)
	GitCommonDir    string // "" if it failed
	HeadTimes       map[string]int64
}

// Snapshot runs the seven git reads under ONE Read reservation: the six
// independent reads concurrently, CommitTimes after Worktrees. Coalesced on
// the key "snapshot" so concurrent callers share one fetch. Returns the
// first error from a fatal read (Status/Branches/Log/Worktrees); best-effort
// reads (CommitTimes/TopLevel/GitCommonDir) leave zero values on failure.
func (s *Service) Snapshot(ctx context.Context) (Snapshot, error)

// Status / Worktrees are single gated reads for the CLI commands that need
// just one value. Each takes a Read reservation; coalesced on its own key.
func (s *Service) Status(ctx context.Context) (model.WorkingTreeStatus, error)
func (s *Service) Worktrees(ctx context.Context) ([]model.Worktree, error)
```

`Commit`/`Log` count (50) stays exactly as loadCmd uses today; paging is
stage 3.

### The gated-read helper

Go forbids generic methods, so a package-level generic carries the gate +
singleflight boilerplate:

```go
// query runs fn under a Read reservation on s's gate, coalescing concurrent
// calls with the same key. Reservation is OUTERMOST-via-singleflight: the
// leader acquires the reservation and runs fn; followers share the leader's
// result without acquiring.
func query[T any](ctx context.Context, s *Service, key string,
	fn func(context.Context) (T, error)) (T, error)
```

- `Snapshot`'s `fn` is the parallel fan-out function (fires the six
  independents as goroutines, then `CommitTimes`). Inside it, verbs run on
  `s.repo` directly — they are already under the single Read reservation
  `query` acquired; they must NOT re-acquire.
- `Status`/`Worktrees` `fn` is just the verb call.

### Singleflight

Hand-rolled (no new dependency — the project keeps deps to go-toml +
bubbletea), per-`Service`:

```go
type flightGroup struct {
	mu sync.Mutex
	m  map[string]*flightCall
}
type flightCall struct {
	wg  sync.WaitGroup
	val any
	err error
}
func (g *flightGroup) Do(key string, fn func() (any, error)) (any, error)
```

The leader runs `fn`; followers block on `wg` and share `val`/`err`. The
leader's ctx governs the underlying work (a leader cancel affects followers —
same semantics as `golang.org/x/sync/singleflight`; a non-issue for the
single-caller TUI). Per-Service scope is correct for the TUI (one Service)
and a future shared MCP Service (one per repo); the CLI is process-per-command
so coalescing is inert there but harmless. The `flightGroup` lives on the
`Service` (pointer field), so the value-receiver concern doesn't apply (the
TUI holds `*domain.Service`).

### Fan-out error handling

Preserve today's `loadCmd` semantics exactly:

- **Fatal**: `Status`, `Branches`, `Log`, `Worktrees`. The first to fail is
  the error `Snapshot` returns.
- **Best-effort**: `CommitTimes`, `TopLevel`, `GitCommonDir`. Failures leave
  the zero value; the date sort degrades and the current-worktree marker
  hides, exactly as today.

Implementation: a `sync.WaitGroup` over the read goroutines plus a
mutex-guarded `firstErr` written only by the fatal reads. All reads run to
completion; there is no cancel-on-first-error (it would save at most ~1 s on
a rare error path and complicates the ctx/singleflight interaction — YAGNI).

## Frontend wiring

**TUI** (`internal/tui/load.go`, `model.go`):

- `loadCmd` replaces its seven inline verb calls with `m.svc.Snapshot(ctx)`.
  On success it fills `dataLoadedMsg` from the snapshot fields **and** runs
  `config.Load(filepath.Join(top, ".gg.toml"))` + `repos.Touch(statePath,
  top, now)` itself (unchanged logic, relocated to after the snapshot, gated
  on a non-empty `top`).
- **Generation-superseding** (the one new behavior): `Model` gains
  `loadGen int`; `loadCmd` increments it and captures the value;
  `dataLoadedMsg` carries `gen`; the `dataLoadedMsg` handler drops a result
  whose `gen != m.loadGen`. This stops a stale in-flight load (mash `r`, or a
  reRoot mid-load) from painting an outdated snapshot over a newer one.
  `reRoot` bumps `loadGen` so a load in flight against the old repo is
  discarded.

**CLI** (`internal/cli`): `cmdStatus` → `svc.Status(ctx)`; the worktree
`list`/`remove` reads of `repo.Worktrees` → `svc.Worktrees(ctx)`. Each
acquires a Read reservation (and resolves the common dir once) per command —
accepted cost; the CLI is process-per-command so there is no concurrency to
coalesce, but routing through domain completes the "frontends don't touch
`internal/git` for reads" goal early for these call sites. Commands construct
a Service via `domain.New(openRepo(workdir))` (or reuse the Service if the
command already built one for an operation).

`domain.Service` keeps `Repo()` for the read call sites NOT converted in this
stage (branches/log/commit-files in the TUI still go through verbs inside the
Snapshot or other code paths); the full removal of `Repo()` and the import
guard are stage 4.

## Testing

`internal/domain` (all `-race`):

- **Singleflight (headline)**: two goroutines call `Snapshot` concurrently
  against a FakeRunner whose `git status` blocks on a channel; assert the
  underlying `git status` ran exactly once and both callers got the same
  result.
- **Parallel fan-out**: `Snapshot` issues all seven git spans (FakeRunner
  `Calls`); the Read reservation is held during the fan-out and released
  after (observed via `gateFor(...).Queue()` from inside a blocked fake read).
- **Dependency**: `CommitTimes` is called with the shas from the `Worktrees`
  result (FakeRunner returns worktrees with known HEADs; assert the
  `git log (commit times)` argv carries them).
- **Fatal vs best-effort**: a `Status`/`Branches`/`Log`/`Worktrees` error
  makes `Snapshot` return it; a `CommitTimes`/`TopLevel`/`GitCommonDir` error
  yields a successful Snapshot with the corresponding field zero.
- **Single reads**: `Status` and `Worktrees` each acquire a Read reservation
  (Queue() shows Read held) and return the verb result; coalesced on distinct
  keys.

`internal/tui`:

- **Stale-generation drop**: feed two `dataLoadedMsg` with an older `gen`
  after a newer one; assert the older is ignored.
- Existing load/model tests stay green (Snapshot is behavior-preserving for
  the success path; config/Touch still happen).

`internal/cli` + `e2e`: behavior unchanged; suites green. (FakeRunner-based
CLI tests that assert call sequences gain a leading `git rev-parse
(common-dir)` from the first gated read — update those expectations; real-git
CLI/e2e tests are unaffected.)

## Docs

- `CHANGELOG.md`: a "Parallel startup snapshot" entry under the existing
  "Domain layer & repo gate" lineage (stage 2).
- `CLAUDE.md`: the `domain` package-map row gains "and `Snapshot`/single
  gated queries"; one convention line that frontend reads go through domain
  queries (Snapshot for the TUI load, `Status`/`Worktrees` for CLI).
- No README change (no user-facing surface), no agent-skill bump (CLI
  behavior unchanged).

## Not doing (YAGNI)

Cache (stage 6); cancel-on-first-fatal-read; per-gate (cross-Service)
singleflight; converting every TUI read path to a domain query (stage 4 does
the sweep + import guard); making reads non-blocking differently than the
existing `tea.Cmd`; paging commits (stage 3).
