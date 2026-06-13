# Domain layer + repo gate (CQRS stage 1) — design

Date: 2026-06-13
Status: approved

## Context: the target architecture (six stages)

gg is growing a CQRS-shaped core: frontends issue **commands** (the existing
engine `Operation`s) and **queries** (read-services) through a single
**domain layer** that synchronizes them per repository, so concurrent
activity on a gigantic monorepo is deterministic — writes serialize, reads
stay consistent, nothing dies on `index.lock`.

```
   TUI            CLI            MCP (future)
     \             |              /
      ───── internal/domain ─────          ← commands + queries + sync ("the proxy")
      ├─ Execute(ctx, op, …)   command side: reservation + engine op
      ├─ Snapshot(ctx)         query side (stage 2)
      ├─ commit feed           query side (stage 3)
      └─ internal/repogate     reservations: Read / RefWrite / TreeWrite
                |
        internal/engine  (Operations — contract unchanged)
                |
        internal/git verbs → gitcmd → gitexec
```

Stages (each its own spec → plan → feature cycle):

1. **`repogate` + `domain.Execute`** — THIS SPEC.
2. `domain.Snapshot` query service (parallel fan-out + singleflight; ships
   the measured 2.3–3.1 s → ~max(reads) startup win).
3. `commitfeed` paged history query (design already approved separately).
4. OpDeps narrowing to an interface + frontend import guard + `LimitRunner`
   subprocess bound.
5. Concurrent commands (queued ops, op-ID-tagged events, TUI queue display)
   — the enabler for workspace group sync and MCP.
6. Snapshot cache (TTL/fs-watch, stale-while-revalidate) — only if span
   measurements after 1–3 justify it.

Architectural findings this rests on (from the 2026-06-13 architecture
review):

- **The synchronization unit is the operation, not the git invocation.**
  `SmartPull.checkoutPull` runs stash → switch → fetch → pull → switch back
  → stash pop, possibly blocked on a Decider mid-sequence; the repo is in a
  deliberately wrong state between steps. Per-invocation locking (e.g. a
  scheduling `Runner` decorator) would serialize subprocesses while leaving
  interleaved transactions — the actual corruption — wide open. The lock is
  held across Decider blocks: an op mid-decision holds no subprocess but
  logically holds the repo.
- **Argv-based read/write classification is unreliable**
  (`FastForwardRef` is a `git fetch` that writes a ref; `git status`
  opportunistically writes the index). Classification is by op identity,
  which is exact.
- **Engine ops calling git verbs stays.** "No high-level op on low-level
  ops" is a layering rule: frontends never touch `internal/git`
  (enforced fully in stage 4) and ops never construct their own
  Runner/Repo. Verbs are the engine's vocabulary; a mid-tier between ops
  and verbs would recreate the verb layer under another name.
- **In-process honesty:** the gate synchronizes one gg process. Two `gg`
  processes or raw git in another terminal still fall back to git's own
  `index.lock`. Documented, not oversold.

## What stage 1 delivers

1. New package **`internal/repogate`**: per-repo reservations with three
   modes, writer-preferring fairness, FIFO writers, context-aware acquire,
   first-class escalation, queue introspection, wait spans.
2. New package **`internal/domain`**: `Open` (one place that constructs the
   Repo/Runner) and `Execute` (the one command path both frontends use —
   absorbs the duplicated OpDeps-assembly + op-span shell in `tui/op.go`
   and `cli/core.go`).
3. `engine.OpDeps` gains an optional `Escalate` hook; `SmartPull` declares
   `RefWrite` for background intent and escalates at its safe boundary.
4. TUI op context becomes cancellable (today `startOp` hardcodes
   `context.Background()`).

**No user-visible behavior change.** The TUI still runs one op at a time
(`opsIdle`); reads still go through `m.repo` directly until stage 2. Stage 1
is the foundation, verified by tests rather than by new UI.

## `internal/repogate`

```go
// Package repogate serializes access to a git repository within this
// process. The unit of exclusion is a whole high-level operation (which may
// span many git invocations and block on user decisions), not a single git
// call. Gates are keyed by the repo's git common dir, so all linked
// worktrees of a repository share one gate.
package repogate

// Mode is the kind of access a reservation grants.
type Mode int

const (
	// Read is repo-state observation (status, branches, log…).
	Read Mode = iota
	// RefWrite moves refs only, never index/worktree/HEAD (background
	// fast-forward). Per-ref updates are atomic, so reads may overlap.
	RefWrite
	// TreeWrite may touch index, worktree, or HEAD. Exclusive.
	TreeWrite
)

// For returns the process-wide gate for the given git common dir
// (absolute path), creating it on first use.
func For(commonDir string) *Gate

// Acquire blocks until the reservation is granted or ctx is cancelled.
// label names the holder in Queue() and in wait spans (e.g. "op SmartPull").
func (g *Gate) Acquire(ctx context.Context, mode Mode, label string) (*Reservation, error)

// Release ends the reservation. Releasing twice panics (programming error).
func (r *Reservation) Release()

// Escalate trades the held reservation for a TreeWrite one: it RELEASES the
// current reservation and joins the writer queue (no atomic upgrade — that
// is the classic deadlock). The caller must therefore only escalate at a
// boundary where the repo holds no partial state from its own work.
// A reservation that is already TreeWrite returns immediately.
func (r *Reservation) Escalate(ctx context.Context) error

// Queue snapshots current holders and waiters, FIFO, for frontends to
// render ("queued: smart pull (2nd)").
func (g *Gate) Queue() []Entry

type Entry struct {
	Label   string
	Mode    Mode
	Waiting bool // false = currently holding
}
```

### Compatibility and fairness

| holder ↓ / requester → | Read | RefWrite | TreeWrite |
|---|---|---|---|
| Read | ✓ | ✓ | — |
| RefWrite | ✓ | — | — |
| TreeWrite | — | — | — |

- **Writers (RefWrite + TreeWrite) are FIFO among themselves** in arrival
  order.
- **Writer preference:** the moment any writer waits, newly arriving Reads
  queue behind it (already-granted Reads finish naturally). Rationale: the
  TUI refreshes constantly with seconds-long `git status` calls on slow
  filesystems; reader preference would starve a pull indefinitely, and the
  TUI wants the post-write snapshot anyway.
- Reads are granted together whenever compatible; no ordering promise among
  concurrent reads.
- A cancelled waiter (ctx) leaves the queue and `Acquire` returns
  `ctx.Err()`.

### Implementation sketch

One mutex per gate guarding: current holders (mode + count), a FIFO waiter
list (`{mode, label, ready chan struct{}}`). Every Release/arrival runs a
grant pass: grant the head writer when no incompatible holder remains;
grant a prefix of compatible Reads when no writer waits. `For` is a
package-level `map[string]*Gate` behind its own mutex. No goroutines owned
by the package.

### Observability

When a granted reservation waited longer than zero, the gate emits
`observ.EmitSpan(Span{Name: "gate wait", Args: [mode, label], Start: …,
Duration: waited})`. Zero-wait acquisitions emit nothing (the common
single-user case stays noise-free). This keeps `--time-track`/`inspect`
truthful once concurrency exists: "git was slow" and "queued behind a
write" become distinguishable.

## `internal/domain`

```go
// Package domain is the frontend-facing layer: commands (engine operations)
// and, in later stages, queries. Frontends call domain; nothing above the
// engine acquires gates or assembles OpDeps by hand.
package domain

type Service struct { /* repo *git.Repo; commonDir string (lazy); … */ }

// Open builds a Service rooted at workdir with the standard runner
// (NewExecRunner + a 200-span ring) — collapsing today's three
// per-frontend `git.Repo{Runner: …}` construction sites.
func Open(workdir string) *Service

// New wraps an existing repo (tests; FakeRunner).
func New(repo *git.Repo) *Service

// Repo exposes the underlying repo for READ verbs. Transitional: stage 2
// moves frontend reads into domain queries and stage 4 removes this.
func (s *Service) Repo() *git.Repo

// Execute runs one command under its reservation:
//  1. mode = op.LockMode() if it implements it, else TreeWrite;
//  2. acquire the gate (label "op <OpName>"), honoring ctx while waiting;
//  3. run op.Run with OpDeps{Repo, Events, Decider, Escalate};
//  4. release; emit the "op <OpName>" span (success/error), absorbing the
//     span code currently duplicated in tui/op.go and cli/core.go.
// Execute is synchronous; frontends keep their own goroutine/pumping
// structure (tea.Msg channel in the TUI, progress printing in the CLI).
func (s *Service) Execute(ctx context.Context, op engine.Operation,
	events chan<- engine.Event, dec engine.Decider) (engine.Result, error)
```

- The gate key (git common dir) is resolved lazily on first `Execute` via
  `repo.GitCommonDir` and cached — `Open` itself runs no git command, so
  CLI startup pays nothing new. If resolution fails or returns empty,
  `Execute` falls back to the workdir `Open` recorded, and a `New`-built
  Service (no workdir) falls back to a per-Service unique key — sound for
  everything except cross-worktree races in a broken repo, where the verb
  error surfaces anyway.
- `LockMode` is an optional interface:
  `interface{ LockMode() repogate.Mode }`. Every existing op defaults to
  TreeWrite. `SmartPull` implements it: `RefWrite` when
  `Intent == PullInBackground`, else `TreeWrite`.

## Engine changes

`OpDeps` gains one optional field, mirroring the nil-safe style of
`Events`/`Decider`:

```go
type OpDeps struct {
	Repo    *git.Repo
	Events  chan<- Event
	Decider Decider
	// Escalate trades the operation's reservation for an exclusive
	// (TreeWrite) one. Nil (direct engine use, tests) is a no-op. Call it
	// only at a boundary where the operation holds no partial state.
	Escalate func(ctx context.Context) error
}

// escalate is the nil-safe helper (like emit/decide).
func (d OpDeps) escalate(ctx context.Context) error
```

`SmartPull` (smart_pull.go:68): in the background-intent path, after the
user picks `checkout-and-resolve` and BEFORE `checkoutPull` runs, call
`deps.escalate(ctx)`. This boundary is safe by construction: the failed
`FastForwardRef` left no partial state. On escalation error (ctx cancelled
while queued), return the error.

## Frontend wiring

**TUI** — `Model` gains `svc *domain.Service` (pointer field, the `m.repo`
pattern): `tui.New(repo)` wraps the handed-in repo via `domain.New`, and
`reRoot` rebuilds via `domain.Open(path)` with `m.repo = m.svc.Repo()`, so
every read call site stays untouched.
`startOp` becomes: create `ctx, cancel := context.WithCancel(context.Background())`,
store `cancel` on the Model (`opCancel context.CancelFunc`, set once per op
— safe across value copies), and run `svc.Execute(ctx, op, events,
uiDecider{…})` in the existing goroutine; the span code moves out (now in
Execute). `opCancel` is invoked when the program quits while an op runs
(tea.Quit path) and cleared on `opFinishedMsg`. A user-facing cancel key is
stage 5, but from stage 1 an op can no longer outlive the UI silently.

**CLI** — `openRepo` and every command signature stay unchanged;
`runOperation(ctx, repo, …)` keeps its signature and its body shrinks to
the event-printing pump around `domain.New(repo).Execute(…)`. A fresh
Service per call is equivalent to a shared one because gates live in the
process-global registry keyed by common dir — the full
frontends-hold-a-Service narrowing lands with stage 4. The CLI's real,
cancellable command ctx flows through unchanged. (Side effect: each CLI op
now runs one extra `git rev-parse --git-common-dir`; FakeRunner-based CLI
tests that assert call sequences gain that leading call.)

**`cmd/gg/main.go`** — unchanged: it keeps building the repo with its own
ring (the panic dump references it); the TUI wraps that repo via
`domain.New` internally.

## What stage 1 explicitly does NOT do

- No reads through the gate (stage 2) — `loadCmd` and CLI reads still call
  verbs directly via `Repo()`.
- No concurrent ops in the TUI (`opsIdle` unchanged; stage 5).
- No cache, no singleflight (stage 2/6).
- No `engine.GitOps` interface narrowing (stage 4).
- No queue UI (stage 5) — but `Gate.Queue()` exists and is tested, so
  stage 5 is rendering, not plumbing.

## Testing

`internal/repogate` (all with `-race`):

- Compatibility matrix: each pair of modes, concurrent goroutines,
  assert overlap/exclusion (Read‖Read and Read‖RefWrite overlap;
  RefWrite‖RefWrite, anything‖TreeWrite exclude).
- Writers FIFO: three TreeWrites resolve in submission order.
- Writer preference: holder Read + waiting TreeWrite + new Read → the new
  Read waits until after the writer.
- Acquire honors ctx cancel while queued (returns ctx.Err(), leaves queue,
  later grants unaffected).
- Escalate: RefWrite holder escalates while a TreeWrite waits → the earlier
  waiter wins (FIFO), then the escalator; already-TreeWrite returns
  immediately; ctx cancel during escalate.
- Queue(): labels, modes, holding/waiting flags, FIFO order.
- Double Release panics.
- `For`: same key → same gate; different keys → independent gates (the e2e
  in-process invariant: scenarios build distinct temp repos, hence distinct
  common dirs).
- Wait span emitted only when waiting occurred.

`internal/domain`:

- Execute acquires/releases around op.Run (fake op observes gate state via
  Queue()).
- Default TreeWrite vs declared LockMode (fake op with LockMode() Read).
- Escalate hook wired: fake op calls deps.escalate, observes exclusivity.
- Op span emitted with error fields on failure (FakeRunner).
- Ctx cancelled while queued → op never runs, error returned.
- Open/New: lazy common-dir key; fallback key on resolution failure.

`internal/engine`:

- OpDeps.escalate nil-safe (no Escalate set → no-op, op still works).
- SmartPull: LockMode table (background → RefWrite, stay → TreeWrite);
  background + checkout-and-resolve calls Escalate before any checkout verb
  runs (FakeRunner argv order vs a recording Escalate func).

TUI/CLI: existing op tests stay green (wiring is behavior-preserving);
one new TUI test that quitting mid-op cancels the op ctx.

## Documentation

- `CHANGELOG.md`: new "Domain layer & repo gate" subsection under Added.
- `CLAUDE.md`: package map gains `domain` and `repogate`; the architecture
  diagram gains the domain layer; conventions gain "frontends run commands
  via domain.Execute; ops declare LockMode when not TreeWrite".
- No README change (no user-facing surface), no agent-skill bump (CLI
  surface unchanged).

## Not doing (YAGNI)

Lock upgrade (escalate = release + re-queue, by design); per-worktree
sub-locks; cross-process locking; retry-on-index.lock niceties; persistent
job queue; read gating; op priorities beyond writer preference.
