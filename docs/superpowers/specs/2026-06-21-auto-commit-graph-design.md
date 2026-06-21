# Auto-maintain the commit-graph (fast startup on huge repos)

**Date:** 2026-06-21
**Status:** Design approved (brainstorm). One feature, one plan.

## Motivation

On a large repo (`/home/homeend/others/linux`: 6.4 GB .git, 1,458,349 commits,
no commit-graph file) the TUI is blank for ~18 s on launch, and pays it again on
every page. Measured root cause: gg's commit-feed verb (`LogScoped`) uses
`git log --date-order`, which must load and topologically sort the **entire**
1.46M-commit graph before emitting the first row — because there is no
commit-graph file to provide generation numbers.

Empirically verified on that repo:

| command | time |
|---|---|
| `git log -n 500` (plain order) | 0.02 s |
| `git log -n 500 --date-order` (no commit-graph) | 18.17 s |
| `git commit-graph write --reachable` (one-time) | 26 s → 84 MB cache |
| `git log -n 500 --date-order` (**with** commit-graph) | **0.09 s** (~200×) |

The fix is **not** to drop `--date-order` (it guarantees a parent is never drawn
before its children — required by the single-line commit-graph lane renderer),
but to make it cheap by ensuring a commit-graph exists.

## Order policy (domain) — decided once per feed generation

The log order is chosen by whether a commit-graph exists:

- **commit-graph present** → `--date-order` (topologically correct *and* fast).
- **commit-graph absent** → **plain order** (git's default lazy newest-first
  walk; instant; still interleaves multiple branch tips by date). The graph
  lanes are usually still correct because children normally have ≥ their
  parents' commit dates; only unusual history (clock skew, cross-branch
  interleave) can briefly mis-draw.

**The order is captured ONCE per feed generation, not per page.** `LoadInitial`
stats for the graph once and stores `dateOrder` on the feed; every `LoadMore`
in that generation reuses the stored value and passes it to `LogScoped`. This is
a correctness requirement, not an optimization: the feed pages with `--skip=N`,
and `N` is computed against a particular walk order — if a later `LoadMore`
flipped to `--date-order` mid-generation it would skip `N` rows of a *different*
ordering and silently drop commits (gaps that hash-dedup cannot detect, applied
to the current generation so the gen-drop guard would not catch them). The
**only** order transition is the explicit reload on `commitGraphWrittenMsg`,
which bumps the generation and re-stats → the whole new walk is `--date-order`.

Rationale: gg only pays for topo-correct ordering when it is cheap (a graph
exists). The feed is never blocked for 18 s, regardless of auto-write.

## Auto-write (TUI, on open)

When the repo opens, if the commit-graph file is **missing** and config
`auto_commit_graph` is on (default), gg:

1. shows a one-time status notice: *"indexing history for faster loads (one-time)…"*;
2. runs `git commit-graph write --reachable` in the **background** (non-blocking
   `tea.Cmd`, mirroring `reloadRefsCmd` → `refsRefreshedMsg`);
3. on success, **if the feed is still near the top**, triggers a feed reload
   (`startFeedReload`) which finds the graph and uses `--date-order` — correct
   ordering, self-healed. If the user has scrolled deep during the build, the
   reload is **skipped** (it would yank them back to page 1 ~26 s after launch,
   itself a glitch); the next *natural* reload — a scope change or manual
   refresh — picks up `--date-order`. The feed already shows the correct commits
   in plain order; the reload only upgrades lane-drawing, so skipping it when
   disruptive is the right trade. ("Near the top" = the current selection /
   first page is still in view — exact predicate decided in the plan against
   `startFeedReload`'s selection/scroll behavior.)

So the first launch shows commits **instantly** (plain order), then quietly
converges to topo-correct ordering ~26 s later. Subsequent launches are fast
because the graph already exists.

Failure (git < 2.18, read-only `.git`, write error) is **non-fatal**: the
notice clears and the feed keeps using plain order.

## Components and boundaries

| Layer | Change |
|---|---|
| `git` | New verb `WriteCommitGraph(ctx)` = `git commit-graph write --reachable`. `LogScoped` gains a `dateOrder bool` param (omit `--date-order` when false). |
| `domain` | `HasCommitGraph(ctx) (bool, error)` — stat `<commonDir>/objects/info/commit-graph` **or** the split-chain `<commonDir>/objects/info/commit-graphs/commit-graph-chain`. `WriteCommitGraph(ctx) error` — the verb under a Read reservation (it writes only a cache, takes git's own lock, never touches refs/tree). `logPage` applies the order policy via `HasCommitGraph`. |
| `tui` | On startup (after the snapshot loads), if `!HasCommitGraph && cfg.autoCommitGraph`: set the notice + dispatch `writeCommitGraphCmd`. `commitGraphWrittenMsg` clears the notice and calls `startFeedReload`. |
| `config` | `auto_commit_graph` (default **on**) under the existing config; field-level overlay defaults→global→repo. |
| `cli` | None — the CLI has no feed/graph. |

The order decision lives in the feed: `LoadInitial` stats once and stores
`dateOrder` for the generation, `LoadMore` reuses it, and `logPage`/`LogScoped`
just receive the boolean (see "Order policy — decided once per feed
generation"). The TUI owns only the write trigger + notice + conditional reload.

## Detection details

- A commit-graph may be a single file (`objects/info/commit-graph`) or a chain
  (`objects/info/commit-graphs/commit-graph-chain`). `HasCommitGraph` returns
  true if **either** exists.
- git writes the graph atomically (temp + rename), so the stat never sees a
  torn/partial file.
- Write only when **missing** (MVP). No staleness detection: a stale graph still
  accelerates `--date-order` (only commits added since the last write are
  parsed), and git's own `maintenance`/`fetch.writeCommitGraph` refreshes it.

## Gate: plain order must not break the lane renderer

Plain order is now a **first-class** mode — every first launch, and *permanent*
when auto-write is off — so the single-line graph engine (`internal/commitgraph`
`Lay`) must degrade gracefully on non-topological input (a parent row appearing
*before* a child). **Verification step (do this first, before relying on
plain-order default):** add a `Lay` test that feeds a parent-before-child
sequence and asserts it produces a stub/disconnected lane — **never a panic or
index-out-of-range**. Use the Phase-2 real-git graph oracle. If `Lay` *can*
break on non-topo input, plain-order-as-default is unsafe: fall back to dropping
`--date-order` **only for single-branch scope** (where the walk is effectively
topological), and keep `--date-order` for the multi-branch feed (accepting the
one-time 18 s there until the graph is written). The plan must run this check
before wiring the order policy.

## Testing

- **git verb:** real-repo `WriteCommitGraph` then assert `objects/info/commit-graph`
  exists; `LogScoped(dateOrder=false)` omits `--date-order` (FakeRunner argv) and
  `=true` includes it.
- **commitgraph:** `Lay` on a non-topo (parent-before-child) sequence does not
  panic and yields a drawable result (the gate above).
- **domain:** `HasCommitGraph` false on a fresh repo, true after `WriteCommitGraph`;
  `logPage` order policy — no graph → argv without `--date-order`, graph → with it
  (FakeRunner asserting the `git log` argv; or a real-repo behavioral check).
- **tui:** startup with no graph + config on dispatches `writeCommitGraphCmd` and
  sets the notice; `commitGraphWrittenMsg` clears it and yields a feed-reload cmd;
  startup with a graph present (or config off) dispatches nothing.
- Gate: `gofmt`, `go vet`, `./test.sh race`.

## Known limitations (note, don't handle)

- **`core.commitGraph=false`**: such a user has the file but git ignores it, so
  `HasCommitGraph` returns true, we pick `--date-order`, and it is slow again.
  Rare; documented, not handled.
- **Quit during the ~26 s write** is safe with no cancellation logic: git writes
  the graph atomically (temp + rename), so a killed mid-write leaves no
  corruption — just an absent graph that the next launch rebuilds. Treat a
  killed/failed write as non-fatal (clear the notice, keep plain order); never
  fatal.

## Non-goals (v1)

- Refreshing/rewriting an existing (possibly stale) commit-graph.
- `--changed-paths` Bloom filters (pathspec speedups) — `--reachable` only.
- Progress percentage for the write (a single notice suffices).
- Touching the CLI or non-feed surfaces.
