# Swappable commit pager + auto commit-graph

**Date:** 2026-06-21
**Status:** Design approved (brainstorm, revised to a swappable-strategy shape).
Built in two stages, each its own plan/branch; the human merges per stage.

## Motivation

On a large repo (`/home/homeend/others/linux`: 6.4 GB .git, 1,458,349 commits,
no commit-graph) the TUI is blank ~18 s on launch and pays it again per page.
Root cause: the commit-feed verb (`LogScoped`) uses `git log --date-order`,
which must load + topologically sort the **entire** 1.46M-commit graph before
emitting a row — there is no commit-graph file to provide generation numbers.

Measured on that repo:

| command | time |
|---|---|
| `git log -n 500` (plain order) | 0.02 s |
| `git log -n 500 --date-order` (no commit-graph) | 18.17 s |
| `git commit-graph write --reachable` (one-time) | 26 s → 84 MB |
| `git log -n 500 --date-order` (**with** commit-graph) | **0.09 s** (~200×) |

We are not fully certain how much each refinement matters in the real TUI, so
the design makes the commit-loading strategy a **swappable seam**: the current
behavior and the new behavior become interchangeable implementations selected by
one config switch, so they can be A/B-tested on the real repo with the identical
UI.

## Architecture: the `CommitPager` seam

The feed's only git interaction is page-fetching (two `f.svc.logPage(...)`
calls). Extract that behind a small interface the feed delegates to:

```go
// CommitPager fetches one page of commits for a generation. Implementations
// decide ordering and any acceleration (e.g. ensuring a commit-graph).
type CommitPager interface {
    Page(ctx context.Context, limit, skip, gen int, scope LogScope) ([]model.Commit, error)
    Name() string // "date-order" | "graph"
}
```

`CommitFeed` holds one `pager` and calls `pager.Page(...)` instead of `logPage`
directly. `svc.CommitFeed()` picks the implementation. The feed's generation /
dedup / cancellation logic is unchanged.

Why this shape: the *only* thing that differs between old and new is how a page
is fetched, so that is the one swappable unit — not flags scattered across the
code. Adding/altering a strategy never touches the feed.

---

## Stage 1 — refactor to the pager seam (behavior-identical)

Introduce `CommitPager`, extract today's behavior into `dateOrderPager`, and
make the feed delegate to it. **No behavior change**: `dateOrderPager.Page`
calls the existing `logPage` (which keeps `--date-order` hardcoded in
`LogScoped`), and `CommitFeed()` constructs a `dateOrderPager`. No config, no
new git verbs, no TUI change.

- `dateOrderPager{svc}` — `Page` → `svc.logPage(ctx, limit, skip, scope, gen)`;
  `Name()` → `"date-order"`.
- `CommitFeed` gains a `pager CommitPager` field, set in `CommitFeed()`.
- The two `f.svc.logPage(...)` calls in `LoadInitial`/`LoadMore` become
  `f.pager.Page(ctx, limit, skip, gen0, scope)`.
- Tests: the feed loads pages exactly as before (a real-repo behavioral test
  + a FakeRunner argv check that `--date-order` is still used).

Deliverable: a clean seam, provably no behavior change, mergeable on its own.

---

## Stage 2 — `graphPager` (async build) + the v1/v2 switch

Add the new strategy and the switch so the two can be compared.

### git / domain primitives
- `git`: verb `WriteCommitGraph(ctx)` = `git commit-graph write --reachable`;
  `LogScoped` gains a `dateOrder bool` param (omit `--date-order` when false).
  `dateOrderPager` now passes `true` (still behavior-identical).
- `domain`: `HasCommitGraph(ctx) (bool, error)` (stat `<commonDir>/objects/info/commit-graph`
  or the split-chain `.../commit-graphs/commit-graph-chain`); `WriteCommitGraph(ctx) error`
  (verb under a Read reservation). `Snapshot` gains `HasCommitGraph bool`.

### `graphPager` — order policy, captured once per generation
`graphPager.Page` decides `--date-order` by commit-graph presence, **once per
generation** (keyed on `gen`):

- the first call for a new `gen` stats `HasCommitGraph` and caches the boolean;
- later pages of that `gen` reuse the cached boolean (never re-stat).

This per-generation capture is a **correctness requirement**, not an
optimization: the feed pages with `--skip=N` against a particular walk order;
flipping order mid-generation with the same `N` silently drops commits (gaps
hash-dedup cannot detect). The only order transition is the explicit reload
(new `gen`) after the background write. Living inside the pager (keyed on `gen`),
this keeps the feed ignorant of ordering.

- graph present → `--date-order` (fast + correct).
- graph absent → plain order (instant; lanes usually still correct).

### Async build + notice (TUI, gated on v2)
When the active strategy is `graphPager` and the commit-graph is missing
(`!Snapshot.HasCommitGraph`):

1. show a one-time title notice *"(indexing…)"*;
2. run `git commit-graph write --reachable` in the **background**
   (`writeCommitGraphCmd` → `commitGraphWrittenMsg`, mirroring `reloadRefsCmd`);
3. on success, clear the notice and — **only if the feed is still near the top**
   (`!commitsPaged`) — trigger a feed reload (new generation → `graphPager`
   re-stats → `--date-order`). If the user has scrolled deep, skip the reload
   (it would yank them to page 1 ~26 s in); the next natural reload upgrades.

Failure/killed write is **non-fatal** (clear notice, keep plain order; git
writes atomically so a killed write leaves no corruption).

### The switch
Config `[ui] commit_pager = "graph" | "date-order"` (string), default `"graph"`.
`svc.CommitFeed()` reads it and constructs the matching pager. Flipping it and
relaunching the same repo is the whole A/B. (`date-order` = exactly today's
behavior = v1; `graph` = v2.)

### Lay-renderer gate (verify before relying on plain order)
Plain order is a real mode in v2 (every first launch, and whenever the graph is
absent). Before relying on it, confirm `internal/commitgraph.Lay` degrades
gracefully on non-topological input (a parent row before its child): a `Lay`
test that feeds parent-before-child must **not panic / index-out-of-range** — at
worst a stub lane. If `Lay` can break, add a minimal missing-parent guard in
`Lay`; if that is infeasible, narrow the policy to drop `--date-order` only for
single-branch scope.

## Testing (per stage)

- **Stage 1:** feed behavioral equivalence (real-repo load) + FakeRunner argv
  shows `--date-order` still present; `gofmt`/`vet`/`./test.sh race`.
- **Stage 2:** verb (`WriteCommitGraph` writes the file; `LogScoped` toggle);
  `HasCommitGraph` false→true; `graphPager` order captured once per generation
  (FakeRunner: common-dir → a temp dir; create the graph file mid-generation and
  assert `LoadMore` still omits `--date-order`); `Lay` non-topo gate; config
  default + override; TUI trigger/notice/conditional-reload/non-fatal-failure;
  `./test.sh race`.

## Known limitations (note, don't handle)

- `core.commitGraph=false`: the file exists but git ignores it → `HasCommitGraph`
  true, `--date-order` slow again. Rare; documented.
- No staleness refresh: write only when the file is **missing**; a stale graph
  still accelerates `--date-order`, and git's own maintenance refreshes it.

## Non-goals (v1)

- `--changed-paths` Bloom filters (pathspec speedups) — `--reachable` only.
- Per-write progress percentage (one notice suffices).
- Touching the CLI (no feed there).
