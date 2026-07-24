# Web repo/worktree switch (server re-root) — design

Date: 2026-07-25 · Branch: `feat/web-reroot` off `web-dev` · Status: awaiting review

## Goal

`POST /api/reroot` lets the running `gg web` server point itself at a
different worktree of the current repo, or at a previously-opened repo,
resetting all repo-scoped state. **No client UI this increment** — the
sidebar "switch here" row and repo picker are a later, thin client change.

## Background (verified against the code)

- `Server` (server.go:24-39): `svc *domain.Service` and `feed
  *domain.CommitFeed` are the repo-scoped fields (`feed` guarded by `s.mu`,
  reset via `resetFeed`); `cur *opRun`/`opSeq` under `opMu`;
  `pageInitial`/`pageBatch`/`decideTimeout` are process-scoped test seams.
- `s.svc` is read **unguarded** by every handler today (treated as
  immutable for the server's lifetime) — a runtime swap needs
  synchronization.
- `web.Serve` (serve.go:22-49): `domain.Open(workdir)` → `preflight(ctx,
  svc, workdir)` (svc.TopLevel under a 15s timeout, with the friendly
  cross-env worktree-link error) → loopback listen → `New(svc).Handler()`.
- `domain.Service` has no Close; repogate gates are process-global keyed by
  git common dir — dropping an old Service leaks nothing.
- A live `opRun`'s goroutine captured the old service at `Execute` time; an
  in-flight op keeps running against the repo it started on.
- archtest: `internal/repos` (the MRU registry behind the TUI/CLI repo
  switcher) is in neither the frontends' forbidden-import list nor the
  layering DAG — `internal/web` may import it directly, as cli/tui do.
  Its API: `repos.Load(statePath) []Entry` (Entry carries `.Path`),
  `repos.DefaultStatePath()`, `repos.Touch(statePath, repoPath, now)`.

## Changes

### 1. Swappable service

`Server.svc` moves behind `atomic.Pointer[domain.Service]`; every direct
`s.svc` read becomes `s.service()` (mechanical sweep across the package —
handlers, oprun, stage). `New(svc)` stores the initial pointer. The op
transport's existing behavior is untouched: a live op finishes against the
service it started with.

### 2. `POST /api/reroot`

Behind `writeGuard` (JSON content type + loopback Origin), like `/api/op`.
Body: `{"path": "..."}`.

The client string is an **identifier resolved by allowlist** (never
sanitized, never passed to git/domain directly). It must exactly match:

- (a) a `.Path` of `s.service().Worktrees(ctx)` — switch among the current
  repo's worktrees, or
- (b) an `Entry.Path` of `repos.Load(statePath)` — the machine's MRU
  registry of previously-opened repos, read fresh per request.

No match → 404 `unknown target`. Only the matched server-owned string is
opened. `statePath` is a new Server field defaulting to
`repos.DefaultStatePath()` with a test override (the
`pageInitial`/`decideTimeout` seam precedent).

### 3. Re-root sequence (on an allowlisted match)

1. Refuse while an op is live: the `startOp` liveness check under `opMu` →
   409 `an operation is already running`.
2. `cand := domain.Open(path)`; run the existing startup `preflight`
   against it (the func moves/exports within the package so the handler
   can call it). Failure → 409 with the preflight error; the current
   service keeps serving untouched.
3. On success, atomically: swap the service pointer; `s.feed = nil` under
   `s.mu`; clear the finished `s.cur` record under `opMu` (late SSE reads
   of a previous repo's op get 404 — correct, that op belongs to the old
   root).
4. Respond 200 with the same payload `GET /api/repo` returns, built
   against the new service (extract a shared helper from `handleRepo` so
   the two cannot drift).

Rejected alternatives: process restart semantics (loses the loopback
port and any browser state for no benefit); serving multiple repos
concurrently (a different, much bigger feature — one server, one active
repo stays the model).

### 4. Security posture (unchanged)

hostGuard still wraps the whole mux; writeGuard wraps the new route; the
loopback-only bind is untouched. No client-sent string reaches
`domain.Open` — only allowlist-matched server-owned values do.

## Out of scope

Client UI (later increment: worktree ctx-menu "switch here" + MRU repo
picker); recording the new repo into the MRU registry via `repos.Touch`;
multi-repo concurrent serving; op continuation across a re-root.

## Testing (Go, real-git fixtures per the existing web tests)

- Re-root to a second worktree of the same repo (on another branch): 200;
  `/api/repo` reflects the new worktree; the commit feed rebuilt (the
  `headBranchOf` observable changes).
- Re-root to an MRU-registered repo: seed a temp state file via
  `repos.Touch`, point the Server's statePath seam at it; 200 and
  `/api/repo` shows the other repo.
- Unknown path → 404; the old service still serves.
- Op live (a parked delete-branch decision) → 409; decide + finish still
  work afterwards.
- Allowlisted-but-broken target (an MRU entry whose directory was
  deleted after Load — create, register, remove) → 409 preflight error;
  old service still serves.
- After a successful re-root, `GET /api/op/{old-id}/events` → 404.
- writeGuard on the route: non-JSON content type → 415, cross-origin → 403
  (matching the existing `/api/op` guard tests).

## Docs

CHANGELOG entry; CLAUDE.md `web` row gains the re-root sentence (swappable
service pointer, allowlist = own worktrees + MRU registry, preflight
before swap, feed/op-record reset).
