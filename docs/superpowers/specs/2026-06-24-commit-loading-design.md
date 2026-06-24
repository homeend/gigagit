# Commit-loading improvements — design

**Date:** 2026-06-24
**Status:** approved (brainstorm)
**Feature branch:** `feat/commit-loading`

## Problem

The Commits panel loads only 50 commits on first paint and pages more only when
the cursor scrolls near the end. Three pain points follow:

1. Searching for a commit that sits below the loaded window fails silently — the
   user must manually scroll down to force more pages to load before `/` can find it.
2. The initial and per-page counts are hardcoded constants with no way to tune
   them per repo or globally.
3. Applying then clearing a `\` commit filter re-walks the feed from page 0,
   discarding the deep accumulation the user had scrolled/loaded — so a filter
   round-trip resets you to the initial count.

## Goal

Make the Commits feed load deeper, on demand, and survive filtering, so the user
stops hitting the 50-commit wall.

## Scope

Four independent, individually-testable parts, all built on the existing
`CommitFeed` paged read-model (`internal/domain/commitfeed.go`) and its async
single-flight / generation / cancel machinery. No new git verbs beyond the
existing `LogScoped` walk. TUI + domain + config only; no CLI surface.

---

## Part 1 — Configurable page sizes (per-repo + global)

Today `commitInitialPage = 50` and `commitPageSize = 200` are package constants
in `internal/domain/commitfeed.go`. Move the *values* into config, keep the
constants as the built-in fallback.

**Config keys** — three new keys under the existing `[ui]` section (matching
the `commit_graph_*` precedent — all TUI-behavior settings live in `UIConfig`),
overlaid the usual way (defaults → global `~/.config/gg/.gg.toml` → repo
`.gg.toml`):

| Key (`[ui]`)              | Meaning                                  | Default |
|---------------------------|------------------------------------------|---------|
| `commit_initial_count`    | commits walked on first paint            | `300`   |
| `commit_batch_size`       | commits per later page (scroll / ctrl+l) | `300`   |
| `commit_search_max_pages` | eager-search page cap (Part 4)           | `5`     |

A larger initial count is cheap: the default `plain` pager is git's lazy
newest-first walk, so `git log -n 300` is near-instant even on linux-kernel-sized
repos. (The `date-order` pager remains opt-in via `GG_COMMIT_PAGER` and is the
only slow path; these settings do not change that.)

**Plumbing.** `domain` stays config-free (no `internal/config` import). The
`CommitFeed` gains `initialPage`/`pageSize` fields and a
`SetPageSizes(initial, batch int)` setter (mirroring `SetScope`); a value `<= 0`
leaves the existing constant fallback (`commitInitialPage` / `commitPageSize`).
`commitNearEnd` (the auto-page trigger distance) stays a constant. The TUI reads
`m.cfg.UI.CommitInitialCount` / `CommitBatchSize` and injects them via
`SetPageSizes`; `CommitSearchMaxPages` is read by the TUI for Part 4.

**First-paint ordering (load.go).** Today `loadCmd` runs the snapshot and the
first `feed.LoadInitial` *in parallel*, and only loads config *afterward* — so a
configured `initial_count` could not affect the very first paint (the one that
matters most). Restructure `loadCmd` to first resolve the worktree toplevel
(`svc.TopLevel`) and `config.Load` it, call `feed.SetPageSizes(...)`, **then**
run the parallel snapshot + `LoadInitial`. The added sequential `rev-parse
--show-toplevel` + small TOML read is sub-10ms and keeps the existing snapshot∥feed
parallelism. (The repo-switch path at `model.go:1856` recreates the feed; it
reloads through `loadCmd`, so `SetPageSizes` is re-applied.)

**Config registry.** Add the `UIConfig` fields, the `overlayUI` `> 0` guards, the
`Defaults()` values (300/300/5), and a `settingDoc` per key in
`internal/config/template.go` so `gg config init` documents them (see the
`config-settings-registry` convention — hand-sync those literal sites).

**Tests.** Config overlay precedence (repo overrides global overrides default)
for each key; `CommitFeed` constructed with injected sizes issues
`git log -n <initial_count>` on first page and `-n <batch_size>` on later pages
(FakeRunner argv assert); zero/unset falls back to the constant.

---

## Part 2 — `ctrl+l` loads the next batch

`ctrl+l` is unbound today. Add it as a key handled when the Commits panel is
focused: force one `LoadMore` regardless of cursor position, instead of waiting
for the cursor to reach `commitNearEnd` of the end.

- No-op (with the existing in-flight/exhausted guards) when the feed is already
  loading or fully walked.
- Sets the `commitsLoading` ⏳ indicator like the auto-page path.
- Reuses the existing `feed.LoadMore` + `commitsReloaded`/page-applied message
  path; the only new thing is an *unconditional* entry point (bypassing
  `NeedsMore`'s "near end" check, but not its exhausted/in-flight guards).

**Discoverability.** Advertise in both `help.go` and the footer (per the
`advertise-features-in-help-and-footer` convention; footer text kept tight).

**Tests.** With a non-exhausted feed and the cursor at the top, `ctrl+l` on the
Commits panel dispatches a load and sets `commitsLoading`; on an exhausted feed
it is a no-op.

---

## Part 3 — Clearing a filter restores the loaded list (no re-walk)

Applying or clearing the `\` filter currently calls `SetScope` + `LoadInitial`,
re-walking from page 0 and discarding the prior accumulation. Add a
**scope-keyed accumulation cache** inside `CommitFeed`.

**Prerequisite — fix `scopeKey`.** The existing `scopeKey`
(`internal/domain/query.go`) folds only `Branches` + `Upstreams`, **not** the
filter axes — so an unfiltered scope and a `\`-filtered scope on the same
branches collapse to the same key. Extend it to also fold `Paths` / `Author` /
`Grep` / `Since` / `Until` (stable order). This is required for the cache key to
distinguish filtered from unfiltered, and it also tightens the existing domain
read-model singleflight/cache key (today two different filters on the same
branches share a key, disambiguated only by the generation counter). `scopeKey`
is unexported, but `commitfeed.go` and `query.go` are the same `package domain`,
so `CommitFeed` calls it directly — no accessor needed.

**Design.**
- The feed keeps a bounded LRU (default 4 entries) of
  `map[string]cachedScope` keyed by the (now filter-complete) scope key, where
  `cachedScope` holds the accumulated `commits`, the `hashes` dedupe set,
  `skip`, and `exhausted`. The bound is by entry *count* (number of remembered
  scopes), not bytes — one large base accumulation dominates; this caps how many
  scopes are remembered, it is not a memory budget.
- New method `ApplyScope(ctx, scope) (FeedState, error)`:
  1. Stash the *current* scope's accumulation into the cache (so the user can
     come back to it).
  2. If the *new* scope has a cached accumulation → **restore** it (bump gen so
     stale in-flight pages drop; cancel any in-flight walk), return that state
     with **no git call**.
  3. Otherwise walk page 0 fresh (today's `LoadInitial` behavior) and cache it.
  Filter / solo / show-all toggles (the TUI `reloadFeedCmd`) call `ApplyScope`.
- `LoadInitial` remains the **hard refresh** used by startup and post-operation
  reloads: it re-walks the current scope and **clears the entire scope cache**
  (every entry, not just the current one). This is the staleness fix: a write op
  reloads through `LoadInitial`, so any *other* cached scope (e.g. the base list
  stashed when a `\` filter was applied) is dropped rather than restored stale
  later. (The distinction: scope *change* with nothing changed underneath →
  `ApplyScope` restores from cache; any data *refresh* → `LoadInitial` re-walks
  and invalidates all cached scopes.)

**Why a cache and not a single snapshot.** A scope-keyed LRU also makes solo /
show-all toggles instant, not just the `\` filter round-trip, with the same
mechanism.

**Staleness — the load-bearing rule.** Cached accumulations are reused **only**
for a scope toggle with no intervening data change. Walk: base S0 cached → apply
`\` filter S1 → *commit something* → clear filter back to S0. The commit's post-op
`LoadInitial` cleared the whole cache, so clearing the filter re-walks S0 fresh
and shows the new commit / moved tips — never the stale pre-commit list. Without
an intervening op, S0 is restored instantly. No background invalidation needed.

**Note.** `/` search is display-only and already never reset the feed; this part
fixes the `\`/solo round-trip specifically.

**Tests (real git).**
- *Instant restore:* walk a base scope to N commits; apply a `\` filter (narrower
  set); clear it → the restored feed equals the pre-filter accumulation and **no
  additional `git log` ran** (assert via a counting runner or that `skip` is
  unchanged).
- *Cross-scope staleness fix (the blocking case):* walk base → apply filter →
  add a new commit + call `LoadInitial` (the post-op hard refresh) → clear the
  filter → the base feed now **contains the new commit** (re-walked), proving the
  whole-cache invalidation, not a stale restore.

(No `/`-search test here — `/` is display-only and never touched the feed; Part 3
is about the `\`/solo round-trip only.)

---

## Part 4 — Eager `/` search across unloaded history

`/` matches loaded commits only (haystack = full hash + branch name(s) +
subject). When it finds nothing in the loaded window, an explicit trigger pages
the feed forward, re-running the match, and jumps to the first hit.

**Trigger key.** `ctrl+f` ("find further"), usable both while typing a `/` query
and on an already-committed `/` filter, on the Commits panel. (`ctrl+enter` is
kept as an alias where the terminal reports it distinctly, but is not relied on —
most terminals collapse it to plain Enter.) Plain Enter keeps its current
behavior (commit/close the filter).

**Behavior.**
1. Commit the current `/` query (if still typing) and start eager search with a
   budget of `commits.search_max_pages` pages.
2. Load one more page (`feed.LoadMore`); after it applies, re-evaluate the `/`
   match against the now-larger loaded set. On a match → move the Commits
   selection to the first matching row, focus the panel, stop.
3. No match and budget remains and feed not exhausted → load the next page
   (repeat step 2).
4. Budget exhausted, still no match, feed **not** exhausted → open a confirm
   dialog: *"Searched N commits, no match for '‹q›' — search deeper?"* with
   options **Search ‹max_pages› more** / **Cancel**. Confirm → reset the budget
   and continue at step 2. Cancel → stop, leave the `/` filter active on the
   loaded set.
5. Feed exhausted with no match → status notice *"'‹q›' not found in full
   history"*; stop.

**Implementation.** An iterative Bubble Tea command chain, not a blocking loop:
small `eagerSearch` model state `{query string, panel panel, budget int,
scanned int, active bool}`. Each `LoadMore` returns its page-applied message; the
`Update` handler, when `eagerSearch.active`, re-checks the match and either
dispatches the next page-load, opens the dialog, or finishes. Respects the
feed's single-flight guard (one page in flight at a time). The ⏳ indicator shows
while it pages.

**Composition.** Eagerly loaded pages stay in the feed and survive a later filter
toggle via Part 3's cache. The match reuses the existing `commitHaystackAt`
matcher — no second notion of "matches".

**Tests (FakeRunner).** Pages served so the match appears on page 3 of a 5-page
budget → search stops with the selection on the matching row and `commitsLoading`
cleared. Budget exhausted with no match and feed not exhausted → the dialog
opens (assert the layer/modal is present). Feed exhausted with no match → status
notice set, no dialog. A query already matching a loaded commit → no paging
(eager search is a no-op beyond the existing display filter).

---

## Out of scope (YAGNI)

- No ahead/behind counts, no background pre-fetching of the whole history.
- No CLI surface (`gg log` paging) — TUI-only.
- No change to the `date-order` pager's cost profile.
- No fuzzy/regex search — eager search reuses the existing case-insensitive
  substring match.

## Affected packages

- `internal/config` — three new `[ui]` settings (`UIConfig` fields + `Defaults`
  + `overlayUI` guards) + `template.go` docs.
- `internal/domain` — `CommitFeed` page-size fields + `SetPageSizes`,
  `ApplyScope`, the scope cache, and the `scopeKey` filter-axis fix (+ accessor).
- `internal/tui` — `ctrl+l` load-more, `ctrl+f` eager search + dialog, wiring
  `reloadFeedCmd` to `ApplyScope`, `loadCmd` reorder + `SetPageSizes` injection,
  reading the new config, help/footer.

## Decomposition

One feature, four task groups (A: config+sizes, B: ctrl+l, C: scope cache /
ApplyScope, D: eager search). B and D depend on nothing beyond A's config read;
C is independent of B/D. Suitable for subagent-driven execution.
