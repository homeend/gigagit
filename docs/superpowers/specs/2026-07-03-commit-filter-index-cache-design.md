# Commit-filter index cache — design

**Date:** 2026-07-03
**Branch:** `fix/commit-filter-index-cache`
**Status:** approved (root cause benchmarked and confirmed with user)

## Problem

With a large feed loaded (linux repo, ~600k commits), activating the `/` filter
on the Commits panel makes every keypress take seconds — even after the filter
has settled on ~10 matching rows.

### Root cause (measured)

`displayIndices(panelCommits)` (`internal/tui/viewstate.go`) has an O(1) fast
path **only** for the unfiltered case (`m.commitsIdx`, added by the held-End
fix `e51fb29`). The guard is `!m.filterActive(p)`: the moment a filter is
active, every call re-scans the entire backing feed, and per row it

1. builds the haystack string via `commitHaystackAt` (ident derivation + string
   concats),
2. `strings.ToLower`s it (a full copy),
3. runs `strings.Contains`.

Nothing caches the filtered result, and `displayIndices`/`panelLen` fire many
times per keypress: nav clamping in Update, `commitBody`, panel labels,
tooltip, footer predicates, decorators.

Benchmarked on the dev machine (Ryzen 7 8840HS, synthetic 600k-commit feed):

| Scenario | Cost |
|---|---|
| One filtered `displayIndices` scan | 381 ms, 1.2 M allocs |
| One down-arrow Update+View cycle, committed filter | **5.6 s**, 16.6 M allocs (≈15 scans) |
| Same while still in `/`-typing mode | 2.3 s (≈6 scans) |

The match count is irrelevant — the cost is rescanning the 600k backing slice
on every call.

## Fix

Two layers, both Commits-panel-only (other panels are small; the file panels
already have their own membership-split cache):

### 1. Memoize the filtered index (the essential fix)

A new pointer field on `Model`:

```go
filterMemo *commitFilterMemo
```

```go
// commitFilterMemo caches the filtered Commits display-index slice between
// displayIndices calls. Shared across Model value copies via pointer — safe
// because Bubble Tea runs Update/View on one goroutine (same discipline as
// statusList's shared mtime map).
type commitFilterMemo struct {
    query    string   // lowercased query idx was built for; "" = invalid
    wipRows  []wipRow // wip pseudo-rows when built — keyed by CONTENT, not count
    feedLen  int      // len(m.commits) when built
    baseHash string   // m.commits[0].Hash when built
    sort     sortMode // m.sortModes[panelCommits] when built
    idx      []int    // the cached display→backing indices (shared, read-only)
}
```

`displayIndices(panelCommits)` with `filterActive`: if the memo matches
`(query, wipRows, feedLen, baseHash, sort)`, return `memo.idx` in O(1).
Otherwise compute (see §2), store, return.

**Invalidation** mirrors the commit-graph layer's own invariant exactly:

- query / sort / wipRows / feedLen / baseHash are part of the key — any
  change misses naturally. The wip term compares the rows' **content**
  (`slices.Equal`), not their count: a wip row's haystack embeds its
  dirty-file count ("Working tree (2)"), which a status refresh can change
  while the row count stays the same — a count-only key served a stale
  filtered result (caught in the Task-1 review).
- A **same-tip, same-length replacement** is not detectable from the key (the
  documented `graphLayerReset` hole). `graphLayerReset()` therefore ALSO
  invalidates the memo (sets `query = ""` through the pointer). Every
  replacement site already calls it — that invariant is established and
  tested.
- The returned slice is shared and read-only — the same contract as
  `m.commitsIdx`. `sortIndices` is applied at build time, never on the cached
  slice.

**Nil tolerance:** tests (and `benchModel`) construct `Model{}` directly, and a
value receiver cannot persist a lazily-allocated pointer. A nil `filterMemo`
falls through to the plain full scan (today's behavior, correct but
unmemoized). The real constructor allocates it; `reRoot` re-allocates (fresh
repo ⇒ fresh cache).

### 2. Incremental scans on memo miss

When the memo is stale but structurally related, avoid the full O(feed) scan:

- **Narrowing (typing):** same `(wip, feedLen, baseHash, sort)` and the old
  query is a non-empty prefix of the new query ⇒ matches can only shrink
  (`Contains(h, q+s) ⇒ Contains(h, q)`), so scan only `memo.idx` —
  O(previous matches) per typed character instead of O(600k).
- **Append extension (paging):** same `(query, wip, baseHash, sort)` and
  `feedLen` grew ⇒ paging appended older commits; existing indices are stable,
  so scan only the appended tail `[wip+oldFeedLen, wip+newFeedLen)` and append
  matches.
- Anything else (first character, backspace, history recall replacing the
  query, wip change, replacement) ⇒ full scan. One ~381 ms hit on the first
  typed character and per backspace at 600k is acceptable; no snapshot stack
  in v1.

### Expected outcome

- Arrow/page movement with an active filter: O(1) per keypress (memo hit) —
  from 5.6 s to sub-millisecond at 600k.
- Typing: first char one full scan, each further char O(matches).
- Paging while filtered: O(new page), not O(total).

## Non-goals

- Filter performance of non-Commits panels (Files@40k is served by the
  membership split; filter scans there are Contains on short strings, no
  haystack build).
- Pre-lowering / interning haystacks (memo makes full scans rare).
- The separate known issue of ~18 s startup on linux from `--date-order`.
- Eager search (`commit_eager.go`) and `@`-highlight — they call
  `commitHaystackAt` directly for their own purposes and are unchanged.

## Tests

1. **Correctness oracle:** memoized `displayIndices` equals a brute-force
   reference filter across a mutation script — activate filter, extend query,
   backspace, append a feed page, same-length replace (via the
   `graphLayerReset` path), change wip count, change sort, clear filter.
2. **O(1) repeat guard (the user-facing bug):** with a warm memo,
   `testing.AllocsPerRun` of a repeated `displayIndices` call is ~0 and does
   not scale with feed size (ratio test at n and 2n, decorator-test style).
3. **Narrowing unit test:** extract the scan into a helper taking an optional
   base index slice; assert extending the query scans the base slice (test the
   helper directly with a crafted fixture).
4. **Append-extension unit test:** growing the feed with a warm memo appends
   only tail matches and keeps prior indices.
5. Full `./test.sh` (and `race` before merge).
