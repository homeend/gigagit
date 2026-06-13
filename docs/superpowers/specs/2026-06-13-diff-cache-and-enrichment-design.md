# Diff cache + intraline enrichment — design (Spec A)

> Sibling spec (deferred): **Spec B — word-wrap in the diff view** (pure TUI,
> no dependency on this work). Not covered here.

**Goal:** Add a generic, injected, in-memory LRU cache to gg, and use it to
serve *enriched* (GitHub-style intraline) side-by-side diffs for commits
without recomputing or re-reading immutable blobs.

**Architecture:** Three layers. A reusable `internal/cache` LRU factory
(injected at the app composition root). A pure `internal/textdiff` that gains
an `Enhanced` option producing word-level intraline spans. A `domain.Differ`
abstraction (plain + cached decorator) that holds *lazy* byte-sources so a
cache hit skips both the git reads and the diff computation.

**Tech stack:** Go 1.26, stdlib only (`container/list`, `sync`) — hand-rolled
LRU, matching the project's existing "hand-roll over x/sync" taste (cf.
`domain.flightGroup`). lipgloss for render-time emphasis styling.

---

## Motivation

The diff viewer today fetches both blobs (`git show` / file read) and runs a
Myers alignment **every time** a file is opened. Two pressures make that
wasteful:

1. **Enrichment cost.** Intraline emphasis adds a *second* word-level diff
   inside every `Changed` row. For a many-hunk file that is real extra work.
2. **Monorepo read cost.** In a ~100 GB monorepo the dominant cost is the
   `git show <hash>:<path>` process spawn + object read, not the alignment.

A commit diff is **immutable**: `parent(hash)→hash` for a path always yields
the same bytes and therefore the same aligned result. That makes it perfectly
cacheable by a content-addressed key — and the cache should skip the git
reads, not just the computation.

## Scope

In scope (Spec A):

- A generic in-memory LRU cache + injected factory (`internal/cache`).
- Intraline word-level enrichment in `internal/textdiff` (`Enhanced` option).
- A `domain.Differ` abstraction: `plain` + `cached` decorator, composed by a
  factory; lazy byte-sources so a hit avoids fetching.
- Wiring: build one cache factory at the app composition root, inject it down
  to a cached + enhanced `Differ` the TUI/CLI diff loaders call.

Out of scope:

- Word-wrap / horizontal handling of long lines (Spec B).
- Caching anything other than commit diffs (working-tree diffs are **not**
  cached — see below). The cache is *built generic* for future consumers
  (commit lists, files-in-commits) but wires only the diff consumer now.
- Routing diff blob reads through the repogate Read reservation. The lazy
  byte-sources keep today's fetch behaviour (`repo.ShowFile` / `os.ReadFile`)
  unchanged on a miss; gating diff reads is separate CQRS work.
- Disk/persistent caching, byte-size eviction, cross-process sharing.

## Decisions (locked during brainstorming)

- **Cache hit skips fetch *and* compute.** The cached unit holds lazy
  byte-sources (closures); on a hit they are never invoked. Cached value is
  the final `textdiff.Result`.
- **Commit diffs only are cached.** The working-tree diff (HEAD→disk) changes
  on every edit and is hard to invalidate; it always recomputes (cache key
  `""`). It is opened far less often than commit diffs.
- **In-memory LRU**, entry-count bounded, so it cannot grow unbounded. The
  upstream `maxDiffBytes` (10 MiB) guard already keeps any single cached
  result small.
- **Factory is injected, not a package singleton.** Better for testing (fresh
  cache per test, fake/no-op cache injectable) and consistent with the
  codebase rule: inject by default; go process-global only when correctness
  demands it (the sole singleton, `repogate`, exists because cross-worktree
  reservations *must* share one gate; a cache has no such need).
- **Word-level** intraline granularity (not rune-level) — matches GitHub and
  avoids per-character noise.
- **Differ lives in `internal/domain`**, the frontend-facing read layer,
  alongside the `Snapshot`/`Status` queries.

## Components

### 1. `internal/cache` — generic injected LRU factory

A reusable, concurrency-safe, in-memory key→value store. Keys are
caller-chosen strings (typically content hashes); the caller owns key
construction ("providing the hash is up to the user").

```go
package cache

// Cache is a concurrency-safe bounded store of computed values. Keys are
// caller-chosen (usually content hashes). Values are any; callers cast or
// use the Load helper.
type Cache interface {
	// GetOrLoad returns the value for key, computing it via load on a miss
	// and storing the result. load runs OUTSIDE the lock, so a concurrent
	// miss on the same key may compute twice (harmless — the diff is pure);
	// the cache never serializes unrelated keys behind one slow load.
	// A load error is returned and nothing is stored.
	GetOrLoad(key string, load func() (any, error)) (any, error)

	// Get peeks without loading. ok=false on a miss.
	Get(key string) (val any, ok bool)

	// Len reports the current entry count (tests / observability).
	Len() int
}

// Factory vends named caches. The same name returns the same instance, so
// independent consumers ("diff", later "commits", "files") each get their
// own bounded LRU. New names are created lazily at the configured capacity.
type Factory interface {
	Cache(name string) Cache
}

// NewFactory builds an in-memory factory. capacity is the per-cache entry
// cap (LRU eviction once exceeded). capacity <= 0 uses defaultCapacity.
func NewFactory(capacity int) Factory
```

- **Implementation:** `memFactory` holds `map[string]Cache` under a mutex.
  Each `Cache` is an `lru` — a `container/list` (recency order, MRU front) +
  `map[string]*list.Element` + its own `sync.Mutex`. `GetOrLoad`:
  lock→check→unlock; on miss, `load()` outside the lock; lock→store
  (evicting the LRU tail if over capacity)→unlock. On a store race for the
  same key, last writer wins; both callers get a correct value.
- **Capacity:** entry-count. Default `defaultCapacity = 256`. The diff cache
  is created at the default. (Byte-size bounding is a future refinement; the
  10 MiB per-entry guard upstream bounds worst case to ~cap × 10 MiB only in
  a pathological all-huge-diffs session, and realistic diffs are KBs.)
- **Typed helper** (avoids casts at call sites, reusable by any consumer):

```go
// Load is a typed view over GetOrLoad for value type V. A stored value of a
// different type is a programming error and panics (fail fast in tests).
func Load[V any](c Cache, key string, load func() (V, error)) (V, error)
```

- **Thread-safety:** required — TUI diff loaders run in `tea.Cmd` goroutines
  and a future MCP server is multi-caller.
- **Future (not v1):** a cache that *also* coalesces concurrent misses can be
  composed from this `Cache` + `domain.flightGroup`. Noted, not built — the
  single-caller TUI does not need it.

### 2. `internal/textdiff` — intraline enrichment

`textdiff` stays pure (no git/TUI imports). It gains an option and span data.

```go
// Options tunes a comparison. The zero value is the plain line-level diff
// (current behaviour). Enhanced additionally computes intraline spans on
// Changed rows.
type Options struct {
	Enhanced bool
}

// Span is a half-open rune range [Start, End) into a row's raw Left/Right
// string, marking text that differs from the other side. Rune offsets, not
// bytes — the renderer maps them to display columns.
type Span struct {
	Start, End int
}

// Row gains (populated only for Changed rows under Enhanced; nil otherwise):
//   LeftSpans  []Span // differing ranges in Left
//   RightSpans []Span // differing ranges in Right

// Compare aligns old and new line-by-line per opts.
func Compare(old, newB []byte, opts Options) Result
```

- **Signature change:** `Compare` gains an `opts` parameter. There is exactly
  one production caller (the new `plainDiffer`) plus tests; all are updated.
  No `Options`-less convenience wrapper — explicit is fine for an internal
  package with one caller.
- **Enrichment pass:** for each `Changed` row, tokenize `Left` and `Right`
  into **words** (a token is a maximal run of word characters, or a single
  run of separators) and run the existing `myers` over the token sequences.
  Tokens covered by `opDel` become `LeftSpans`; `opAdd` become `RightSpans`;
  adjacent differing tokens merge into one span. Equal leading/trailing
  tokens carry no span. The token→rune mapping yields the rune offsets.
  Reuse `myers`; its `maxEditD` budget bounds a pathological line (on
  give-up, leave spans nil — the row still renders, just without emphasis).
- **Only `Changed` rows** get spans. `Del`/`Add`/`Same` rows never do (a
  whole-line add/delete needs no intraline emphasis; its cell background
  already conveys it).
- **Purity preserved:** spans are plain data; styling is the TUI's job.

### 3. `internal/domain` — the `Differ` abstraction

```go
// ByteSource lazily yields one side's content. Invoked only on a cache miss.
type ByteSource func(context.Context) ([]byte, error)

// Request is one diff to compute. Key is the cache key; "" disables caching
// for this call (e.g. working-tree diffs). Old/New are the two sides; a nil
// source means an absent side (new/deleted file), matching Compare(nil, x).
type Request struct {
	Key      string
	Old, New ByteSource
}

// Diff is the cacheable outcome of one comparison. For a commit diff every
// field is immutable, so the whole struct is cached — including the
// binary/too-large verdicts, which avoids re-reading the blob to re-detect
// them on a later open. Exactly one of {Binary, TooLarge, Result-populated}
// holds.
type Diff struct {
	Result   textdiff.Result // valid unless Binary or TooLarge
	Binary   bool
	TooLarge bool
}

// Differ computes an aligned diff, possibly from cache.
type Differ interface {
	Diff(ctx context.Context, req Request) (Diff, error)
}

// DifferOptions selects quality and caching at construction.
type DifferOptions struct {
	Enhanced bool // produce intraline spans
	Cached   bool // wrap in the caching decorator
}

// NewDiffer composes a Differ: a plainDiffer(Enhanced), optionally wrapped by
// a cachedDiffer over c. c may be nil when Cached is false.
func NewDiffer(opts DifferOptions, c cache.Cache) Differ
```

- **`plainDiffer{enhanced bool}`** — fetches `Old`/`New` (nil source ⇒ nil
  bytes), then owns the size/binary verdicts that today live in the TUI's
  `fillDiff`: either side over the size limit ⇒ `Diff{TooLarge:true}`;
  `textdiff.IsBinary` on either side ⇒ `Diff{Binary:true}`; otherwise
  `Diff{Result: textdiff.Compare(old, new, Options{Enhanced: d.enhanced})}`.
  The size constant (today `maxDiffBytes` in `internal/tui`) moves to the
  domain layer next to `plainDiffer`; the TUI keeps using the same value via
  the returned `TooLarge`. The TUI's *display* of these states is unchanged.
- **`cachedDiffer{inner Differ; cache cache.Cache; enhanced bool}`** — if
  `req.Key == ""`, delegate straight to `inner`. Otherwise
  `cache.Load[Diff](c, qkey, func() { return inner.Diff(ctx, req) })`
  where `qkey` is `req.Key` prefixed by a quality marker (`"e:"`/`"p:"`) so
  enhanced and plain outcomes never collide under the same caller key. On a
  hit, `inner.Diff` (and thus the `Old`/`New` sources) never runs — binary
  and too-large commit blobs are detected once and never re-read.
- **Construction:** the production Differ is `NewDiffer({Enhanced:true,
  Cached:true}, factory.Cache("diff"))`. Tests can build
  `{Enhanced:false, Cached:false}` etc.

### 4. Wiring (composition root + frontends)

```
app
 └─ cache.NewFactory(0)                       // one per running app
      └─ domain.Open(workdir, factory)        // Service stores the factory
           └─ Service.Differ() Differ         // NewDiffer({Enhanced,Cached:true}, factory.Cache("diff"))
                └─ TUI Model holds the Differ (via the Service it already holds)
```

- `domain.Open` gains a `factory cache.Factory` parameter; `Service` stores
  it and exposes the production `Differ` (built once, lazily). Existing
  `domain.New(repo)` (test constructor) defaults the factory to a no-op /
  small in-memory one so existing tests are unaffected.
- The TUI/CLI loaders no longer call `textdiff.Compare` directly; they build
  a `Request` and call the `Differ`.

## Data flow — opening a commit diff (the hot path)

1. User hits Enter on a file in the commit files tree. `model.go` installs the
   placeholder `diffView{loading:true}` and `diffTag`, returns
   `loadCommitDiffCmd` (unchanged shape).
2. The `tea.Cmd` builds:
   - `key := hash + "^.." + hash + ":" + path` (immutable ⇒ cacheable).
   - `old := func(ctx){ return repo.ShowFile(ctx, hash+"^", oldPath) }`
     (nil source when `status == "A"`).
   - `new := func(ctx){ return repo.ShowFile(ctx, hash, path) }`
     (nil source when `status == "D"`).
3. `out, err := m.differ.Diff(ctx, Request{Key: key, Old: old, New: new})`
   (returns a `domain.Diff`).
   - **Cache hit:** the sources never run — no `git show` — and `out` is the
     stored outcome (enriched `Result`, or the `Binary`/`TooLarge` verdict).
   - **Miss:** sources fetch; `plainDiffer` runs the size/binary verdicts and,
     if neither, `Compare(..., Options{Enhanced:true})`; the whole `Diff` is
     stored under `e:`+key.
4. The loader maps the outcome onto the view: `out.TooLarge` ⇒ `v.tooLarge`,
   `out.Binary` ⇒ `v.binary`, else `v.full = out.Result.Rows` /
   `v.fullBlocks = out.Result.Blocks`, `v.rebuild()`, jump to the first
   block; returns `diffMsg`. **Working-tree diffs** (`loadStatusDiffCmd`) do
   the same with `Key: ""` — always recompute (never cached).

Because `domain.Diff` carries the binary/too-large verdicts, they are cached
alongside successful results: an immutable commit blob that is binary or
oversized is detected on the first open and never re-read. The TUI's
`fillDiff` shrinks to "map a `domain.Diff` onto the view"; the size/binary
*detection* moves down into `plainDiffer`.

## Rendering enriched spans (the one real risk)

Spans are **raw-rune** coordinates, but the renderer transforms each line via
`sanitizeLine` (expand tabs to 4-col stops, control chars → `·`) before
truncate/pad. A raw-rune range therefore does **not** equal a display-column
range.

**Approach:** a span-aware cell renderer walks the raw line once, tracking
both the raw-rune index and the emitted display column, and wraps display
runs whose source rune index falls in a `Changed` span with an emphasis
style. Emphasis is a brighter **foreground** layered *on top of* the existing
cell background (`diffDelCell` dark-red / `diffAddCell` dark-green) — lipgloss
styles compose, so the cell background still reads as add/del while the
changed words stand out. `Del`/`Add`/`Same` cells render exactly as today.

This replaces today's `sanitizeLine` + `truncate` + optional `hotStyle.Render`
in `diffCell` with a single pass that interleaves sanitize, span styling, and
width accounting. Plain mode (`Enhanced:false`, nil spans) must render
**byte-identical** to today.

## Error handling

- **Cache:** `GetOrLoad` returns load errors un-stored; a diff fetch/compute
  error propagates to the loader's existing `v.err` path. No partial caching.
- **Source errors:** a `ByteSource` error (e.g. `ShowFile` fails) returns from
  `plainDiffer.Diff` exactly as today's loader error path.
- **Enrichment give-up:** a `Changed` line exceeding the `myers` budget yields
  nil spans; the row renders without emphasis (graceful degradation).
- **Stale results:** unchanged — the `diffTag`/`diffMsg` guard still drops
  results for a diff the user has since closed or navigated away from.

## Testing

- **`internal/cache`:** GetOrLoad hit/miss; eviction at capacity (LRU order —
  least-recently-*used* evicted, `Get` and `GetOrLoad` both refresh
  recency); `Get` peek; concurrent access under `-race` (N goroutines, shared
  keys); `Load[V]` typed round-trip and type-mismatch panic; `Factory` same
  name ⇒ same instance, different names ⇒ isolated/independently bounded.
- **`internal/textdiff`:** word-tokenization span cases — single changed
  word mid-line, leading/trailing change, multiple disjoint changes,
  punctuation boundaries, all-different line, identical-after-trim; spans
  only on `Changed` rows; `Enhanced:false` produces nil spans and an
  otherwise-identical `Result`; budget give-up ⇒ nil spans, row intact.
- **`domain.Differ`:** plain path runs sources and Compares; cached path
  stores then serves without re-invoking sources (assert source call count
  == 1 across two `Diff`s with the same key); `Key:""` never caches (count
  == 2); enhanced/plain key namespacing (same caller key, different quality ⇒
  two entries); nil source ⇒ nil-side diff.
- **TUI render:** enriched `Changed` row shows emphasis spans at the right
  display columns *after* tab expansion (a line with a leading tab + a
  mid-line word change); plain mode renders byte-identical to the current
  golden output; fold/gutter/separator unchanged.
- **Whole stack:** opening a commit diff twice issues `git show` once
  (source-call-count assertion through the loader); opening a working-tree
  diff twice issues reads twice.

## File structure

- Create: `internal/cache/cache.go` (Cache, Factory, lru, memFactory, Load).
- Create: `internal/cache/cache_test.go`.
- Modify: `internal/textdiff/textdiff.go` (Options, Span, Row spans, Compare
  signature, enrichment pass).
- Modify: `internal/textdiff/textdiff_test.go`.
- Create: `internal/domain/differ.go` (ByteSource, Request, Differ,
  DifferOptions, NewDiffer, plainDiffer, cachedDiffer).
- Create: `internal/domain/differ_test.go`.
- Modify: `internal/domain/service.go` (Open takes a Factory; Service holds
  it; `Differ()` accessor). `internal/domain/service_test.go`.
- Modify: `internal/tui/diff_view.go` (loaders build Requests, call the
  Differ; size/binary guard relocation), `internal/tui/diff_render.go`
  (span-aware cell renderer), and the Model/app wiring for the Differ.
  Matching `_test.go` updates.
- Modify: `cmd/gg/main.go` / `internal/app` (construct the factory, thread it
  through `domain.Open`).
- Docs: `CHANGELOG.md` (always); `CLAUDE.md` package map (`cache` added;
  `domain` gains the Differ); `README.md` only if the user-facing diff
  surface changes (enrichment is visual — note it). No CLI surface change ⇒
  no agentskill bump.

## Success criteria

- A generic, injected, concurrency-safe LRU cache exists and is unit-tested,
  with one wired consumer (diff) and headroom for more.
- Re-opening the same commit diff serves from cache with **zero** `git show`
  invocations and no re-computation.
- Commit diffs render GitHub-style intraline word emphasis; working-tree
  diffs still update live (never stale).
- Plain (non-enhanced) rendering is byte-identical to today.
- `./test.sh race` is green.
