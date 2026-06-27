# Design — Unified data-source registry (window-by-window async refresh)

Date: 2026-06-27
Status: approved (Phase A)
Branch: `feat/source-registry`

## Motivation

Today the gg TUI refreshes through a single monolithic path: `loadCmd`
(`internal/tui/load.go`) runs `domain.Snapshot` (status, branches, remote
branches, tags, reflog, worktrees, head-times, conflict — parallel internally
under one Read reservation) concurrently with the commit-feed walk and returns
one `dataLoadedMsg`. Any change — even one that touches a single panel — pays a
full reload, including the expensive working-tree status walk and the commit
re-walk. On a ~100GB monorepo that is slow and disruptive.

Different panels also change at very different rates: the working tree (Files)
changes constantly because the user edits outside gg, while remotes barely
change except on an explicit fetch. A single refresh cadence serves neither.

The goal is a **reactive data-source layer**: each kind of data is read
independently and asynchronously; reading it emits a "this data is now
available" event; every window that depends on that data refreshes from it.
This is delivered in three phases. **This spec covers Phase A only**; B and C
are described as the boundary A must not foreclose.

- **Phase A — partial / per-source reload (this spec).** Replace the monolithic
  reload with a source registry. Actions refresh only the sources they affect.
  Manual refresh shows a per-window spinner; the path to silent automatic
  refresh is built in but not yet driven.
- **Phase B — background async auto-refresh.** Per-source timers read sources on
  their own cadence, silently, with suppression rules (no auto-refresh while an
  op runs, a popup/modal/decider is open, a filter is being typed, or a diff
  layer is up). Not in A.
- **Phase C — adaptive intervals.** Measure each source's read duration (via the
  `observ` spans these queries already emit) and tune its interval. Not in A.

## Architecture (Phase A)

A **source registry** in `internal/tui` becomes the single refresh mechanism.

### Sources

A source is a keyed, independently-readable unit:

```
sourceKey: status | branches | remotes | tags | reflog | worktrees | feed | identity
```

Each source carries:

- a **loader** — calls the existing gated `domain` query (no new domain queries
  needed; see table below),
- the **consumer windows** — used for spinner targeting (manual) and, in Phase
  B, for deciding which sources a timer polls,
- a **per-source generation counter** — supersession, replacing the single
  global `loadGen`,
- an **in-flight flag** — coalescing (avoid stacking a second read of a source
  already loading).

### The data-available message

A read produces one generic message:

```go
type dataAvailableMsg struct {
    source sourceKey
    gen    int
    value  any   // typed per source; type-asserted in the handler
    manual bool
    err    error
}
```

Handler logic:

1. If `gen` is stale for that source (a newer read superseded it), drop it.
2. Store `value` into the model's per-source slot.
3. Recompute any derived caches owned by that source (see table).
4. If `manual`, clear that source's spinner / loading flag.
5. Clear the source's in-flight flag.

Windows re-render naturally in `View` from the stored slots — the Elm-idiomatic
realization of "publish to listeners." There is no literal listener list; the
source→window dependency map exists to target spinners and (Phase B) timers.

### Source set, queries, consumers, derived data

All loaders map to **existing** gated domain queries. Derived data travels with
its parent source, matching how `loadSnapshot` already composes it.

| Source      | Domain query              | Consumer windows              | Derived with it           |
|-------------|---------------------------|-------------------------------|---------------------------|
| `status`    | `Status`                  | Files, Staged                 | conflict state, EOL reconcile |
| `branches`  | `Branches`                | Branches, Commits (branch col)| —                         |
| `remotes`   | `RemoteBranches`          | Remotes                       | —                         |
| `tags`      | `Tags`                    | Tags                          | —                         |
| `reflog`    | `Reflog` (configured limit)| Reflog                       | —                         |
| `worktrees` | `Worktrees`               | Worktrees, Branches (path col)| head-times (`CommitTimes`)|
| `feed`      | `CommitFeed.LoadInitial`  | Commits                       | commit-graph rows         |
| `identity`  | `Identity`                | Settings                      | —                         |

Notes:

- `status` recomputes conflict state (`s.conflictState` is cheap and short-
  circuits unless Status has unmerged files) and applies the EOL-only reconcile
  already wired into `statusFiltered`.
- `worktrees` recomputes head-times (`CommitTimes` over worktree heads) and the
  Branches panel's worktree-path column.
- `feed` keeps its existing `CommitFeed` paging/generation machinery internally;
  the registry wraps it as a source so it participates uniformly. The per-source
  generation here composes with (does not replace) the feed's own gen.
- `identity` is included now (it already has a precedent in `reloadIdentityCmd`);
  folding it into the registry retires that bespoke path.

## Triggers

### Reload-all and startup

`r` and startup become "refresh every source" through the same path
(`reloadAll`). The monolithic `loadCmd` / `dataLoadedMsg` is **retired** in
favor of fanning out all sources; there is one mechanism, not two. Today's
`softReload` keep-panels-visible behavior becomes per-source: panels stay
rendered and show their own ⏳ until their source returns.

### Op completion → affected sources

Each engine operation declares an **affected source set** via a TUI-side lookup
table (a generalization of today's `pendingRefsReload`/`reloadRefsCmd`). On op
completion, refresh exactly those sources with `manual=true`.

**The default for any unmapped op is all sources**, so correctness never
regresses — only explicitly-mapped ops gain the speedup. Initial mappings:

| Operation        | Affected sources              |
|------------------|-------------------------------|
| `Commit`         | status, feed, branches        |
| `Push`           | branches, remotes             |
| `CreateWorktree` | branches, worktrees           |
| `RemoveWorktree` | branches, worktrees           |
| `SetIdentity`    | identity                      |
| `Stash`          | status (+ stash list, existing path) |
| `SmartPull`      | all                           |
| `SmartMerge`/`SmartRebase` | status, feed, branches |
| (unmapped)       | all (safe default)            |

The table lives in `internal/tui` (the engine stays frontend-agnostic; the TUI
owns the mapping from op to the sources its panels care about).

## Manual vs automatic

- A read carries `manual bool`.
- **Manual** (Phase A: every read — `r`, op completion, panel-local refresh)
  sets the consuming windows' loading flags, which render ⏳ (reusing
  `softReload` / `commitsLoadingGlyph`), cleared on arrival.
- **Automatic** (Phase B) sets nothing — silent, no attention pull. Phase A only
  adds the flag the Phase-B scheduler will set to `false`; it does not add the
  scheduler.

## Selection stability

A partial refresh can land while the cursor is in another panel, or in the
refreshed panel itself. Panels must **preserve selection by identity** (branch
name / file path / commit hash), not by index, and clamp gracefully when the
selected item has vanished. This is required for A (partial refreshes already
happen while focus is elsewhere) and de-risks B (refreshes firing while the user
reads).

## Generations & coalescing

- **Per-source generation** replaces the single global `loadGen`. An in-flight
  `branches` read neither clobbers nor is clobbered by a `status` read; each
  source drops its own stale results on arrival.
- **In-flight coalescing**: the per-source in-flight flag prevents stacking a
  second read of a source already loading. The domain layer also
  singleflight-coalesces underneath (belt and suspenders).

## Observability

Every per-source loader routes through the gated `domain.query()` path, which
already emits an `observ` span per read. No new instrumentation is needed in A;
Phase C reads these spans for per-source durations.

## What Phase A explicitly does NOT include

- No background timers, no per-source intervals, no suppression rules
  (Phase B).
- No duration-driven adaptive intervals (Phase C).
- No new domain queries (all needed queries already exist).
- No change to engine operations or the `Operation`/`Decider`/`Event`
  contract — only the TUI's refresh wiring changes.

## Testing (TDD)

- `reloadAll` fans out one read per source and emits one `dataAvailableMsg` per
  source.
- A stale-gen `dataAvailableMsg` is dropped (older gen than the source's
  current).
- A `manual=true` read sets exactly the consuming windows' spinners and clears
  them on arrival; a `manual=false` read sets none.
- The op→source table fires only the mapped sources for a mapped op, and all
  sources for an unmapped op.
- Selection survives a partial refresh of an unfocused panel (selection tracked
  by identity, clamped when the item disappears).
- `feed` source refresh continues to honor `CommitFeed`'s existing generation /
  paging semantics (no regression of scope reload, paging, or graph rows).

## Risks

- **Conversion scope.** Retiring `loadCmd`/`dataLoadedMsg` touches every op's
  post-completion refresh and startup. Mitigation: the "unmapped op → all
  sources" default makes the conversion safe panel-by-panel and op-by-op.
- **Derived-data drift.** Derived caches (commit-graph rows, head-times,
  worktree-path column, conflict state) must recompute with their parent source,
  not on the retired full-reload. The source table makes ownership explicit;
  tests guard each.
- **Feed coupling.** The feed's own generation machinery must compose with the
  registry's per-source gen rather than fight it. Treated as a wrapped source,
  not a reimplementation.
