# commitfeed: on-demand paged commit history (CQRS stage 3) — design

Date: 2026-06-13
Status: approved

Diagrams: `docs/superpowers/specs/2026-06-13-commitfeed-diagrams.md` (Mermaid
class + startup/paging sequence + Feed state).

## Context & problem

The Commits panel is hard-capped at 50: stage 2's `domain.Snapshot` calls
`repo.Log(ctx, 50)` and the TUI has no "load more" path — scrolling to the
bottom just clamps the selection. In a gigantic monorepo loading all history
up front is wasteful, so commits must load on demand.

This is stage 3 of the CQRS refactor (1 = `repogate` + `domain.Execute`;
2 = `domain.Snapshot`/`Status`/`Worktrees` gated parallel queries). It adds a
**stateful domain read-model** that is the single source of truth for the
commit list. The UI signals intent (selection nearing the end) and subscribes
to the model's snapshots; it never accumulates commits itself. This is the
"domain owns the data, propagates to the UI" principle the user set for the
whole effort.

Decided (Option 2): the Feed owns **all** commit loading — `Snapshot` no
longer reads commits at all. "Load commits" is one concept (the Feed), not
split between Snapshot's page 0 and the Feed's pages 1+.

## Scope

`internal/git` (Log gains skip), `internal/domain` (new `CommitFeed`,
`Snapshot` loses commits), `internal/tui` (loadCmd coordinates two reads;
paging; label). No CLI/engine/agent-skill surface change. No config (sizes are
constants). No cache (stage 6).

## git verb change

`Log` gains a `skip` offset:

```go
// Log returns up to limit commits reachable from HEAD, newest first, skipping
// the first skip. skip=0 is the head of history.
func (r *Repo) Log(ctx context.Context, limit, skip int) ([]model.Commit, error)
```

Argv: `git log -n <limit> --skip=<skip> --format=<logFormat>` (`--skip` is
omitted when skip==0 via `gitcmd` `.ArgIf(skip > 0, ...)`, keeping the page-0
invocation byte-identical to today). The only non-test caller is
`domain` (below); the `log_test.go` caller passes `skip=0`.

**Paging model — `--skip` with dedupe, not anchoring.** Pages are
`Log(limit, skip=len(loaded))`. `git log` from HEAD is a stable newest-first
walk, so consecutive `--skip` windows tile it correctly. If HEAD moves between
pages (a commit/pull during browsing) offsets can shift; this is handled by
(a) hash-deduping on append and (b) the Feed's generation counter — any
operation triggers a full reload anyway. Anchoring on the oldest loaded commit
(`Log(<oldest>~1)`) was rejected: in a date-ordered walk it silently skips
side-branch commits that aren't ancestors of the anchor.

## domain.CommitFeed

A stateful read-model in `internal/domain`, vended by the Service and held by
the TUI as a pointer field. It reads through the same gate + singleflight as
`Snapshot` (via an internal gated `logPage`), so it never touches `git`
directly.

```go
// CommitFeed is the single source of truth for the commit list: an
// incrementally loaded, newest-first view of HEAD history. Goroutine-safe;
// Snapshot returns a copy so the TUI can render while a page loads.
type CommitFeed struct {
	svc *Service
	mu        sync.Mutex
	commits   []model.Commit
	skip      int   // next --skip offset (== len(commits) absent dedupe drops)
	exhausted bool  // a page returned fewer than requested
	gen       int   // bumped by Reset; tags pages so stale ones drop
	inFlight  bool  // at most one page request outstanding
}

// CommitFeed returns a fresh feed for this Service's repo.
func (s *Service) CommitFeed() *CommitFeed

// logPage is the gated, singleflighted page read the feed uses.
func (s *Service) logPage(ctx context.Context, limit, skip int) ([]model.Commit, error)
```

Page constants (in `domain`, unexported): `commitInitialPage = 50`,
`commitPageSize = 200`, `commitNearEnd = 10`.

### Methods

```go
// LoadInitial loads page 0 (skip=0, commitInitialPage). Idempotent-ish: it
// resets state first, so it doubles as the reload primitive.
func (f *CommitFeed) LoadInitial(ctx context.Context) (FeedState, error)

// LoadMore loads the next page if warranted. Returns (state, true) when a
// page was loaded; (state, false) when it was a no-op (exhausted, already
// in-flight, or nothing new). Single-flight: concurrent calls collapse.
func (f *CommitFeed) LoadMore(ctx context.Context) (FeedState, bool, error)

// NeedsMore reports whether selection index sel is close enough to the end to
// warrant a page, and the feed can serve one. Filter-suppression is the
// caller's concern (the TUI gates on no-active-filter) — the feed does not
// know about filters.
func (f *CommitFeed) NeedsMore(sel int) bool
//   = !exhausted && !inFlight && sel >= len(commits)-commitNearEnd

// Snapshot returns a copy of the current state for rendering.
func (f *CommitFeed) Snapshot() FeedState

// Reset clears the feed and bumps gen (reload / 'r' / post-op).
func (f *CommitFeed) Reset()

// Gen returns the current generation (for the TUI's stale-page check).
func (f *CommitFeed) Gen() int

// FeedState is an immutable view handed to the frontend.
type FeedState struct {
	Commits   []model.Commit
	Exhausted bool
	Gen       int
}
```

### Behaviour

- **LoadInitial**: `Reset()` (bump gen, clear), then read `logPage(50, 0)`
  under the gen captured after reset; set `commits`, `skip=len`,
  `exhausted = len < 50`. A page whose captured gen != current gen on return
  is dropped (a reset raced it).
- **LoadMore**: if `exhausted || inFlight` → return `(Snapshot(), false, nil)`.
  Else set `inFlight`, capture `gen0` and `skip`, release the lock, read
  `logPage(200, skip)`, re-take the lock: if `gen != gen0` drop the page
  (reset raced) and return `(Snapshot(), false, nil)`; else append with
  hash-dedupe (skip commits whose hash is already present — guards HEAD-moved
  overlap), `skip += len(appended-before-dedupe)` (advance by the raw page
  length so offsets stay aligned with git's walk), `exhausted = rawLen < 200`,
  clear `inFlight`. Single-flight at two levels: the `inFlight` flag (one page
  at a time per feed) and the Service `logPage` query (identical concurrent
  reads coalesce).
- **NeedsMore**: pure predicate, no I/O, no lock side effects beyond a read.
- Errors: a failed page read clears `inFlight`, leaves state unchanged, and
  returns the error; the TUI surfaces it as a transient `statusMsg` and the
  next movement retries. LoadInitial failure propagates like the old load
  error (the TUI shows it).

## domain.Snapshot change

`Snapshot` drops commits entirely:

- `Snapshot` struct loses the `Commits` field.
- `loadSnapshot` removes the `Log` read goroutine (six git reads now: Status,
  Branches, Worktrees, CommitTimes-after-Worktrees, TopLevel, GitCommonDir).
- Fatal/best-effort split is otherwise unchanged.

## TUI integration

**Model fields** (pointer + mirrors):
- `feed *domain.CommitFeed` (pointer field; the value-receiver pattern).
- `commits []model.Commit` stays as the rendered mirror of `feed.Snapshot()`.
- `commitsExhausted bool` and an implicit count from `len(commits)` drive the
  label.

**Startup `loadCmd`** fires two gated reads concurrently and joins for the
first paint (see the startup sequence diagram). Concretely, `loadCmd`'s
`tea.Cmd` runs `svc.Snapshot(ctx)` and `feed.LoadInitial(ctx)` in two
goroutines, waits for both, and returns one `dataLoadedMsg` carrying the
snapshot fields plus `commits`/`commitsExhausted`/the feed gen. On error from
either, the message carries the error (snapshot error takes precedence; a feed
error alone still shows status/branches with an empty commit list and the
error in `statusMsg`). The generation guard from stage 2 (`loadGen`) is
unchanged and still drops stale whole-loads.

**Paging** (see the paging sequence diagram): after any selection move on the
Commits panel — the normal `down`/`j`/`pgdown` arms, **and**
`moveCommitUnderFilesView` (files-view follow-live) — the TUI calls
`m.feed.NeedsMore(sel)` and, when true **and no filter is active on the
commits panel**, returns a `loadMoreCmd(m.feed)`. That cmd calls
`feed.LoadMore(ctx)` and returns `commitsPagedMsg{gen}`. The handler, if
`msg.gen == m.feed.Gen()`, mirrors `feed.Snapshot()` into
`m.commits`/`m.commitsExhausted`; selection is untouched (the cursor stays
under the user). A stale gen (a reload happened) is dropped.

Filter suppression: auto-paging is gated on `!m.filterActive(panelCommits) &&
!(m.filterTyping && m.filterPanel == panelCommits)`. Typing `/` over commits
must not cascade through all history. `o` sort and `/` filter operate on the
loaded slice only (their existing behaviour); the label signals more exist.

**reRoot / reload**: `reRoot` sets `m.feed = m.svc.CommitFeed()` (fresh feed
for the new repo) alongside the existing `m.svc`/`m.repo` reset. The `r`
reload and post-op reload paths already call `loadCmd`, whose `LoadInitial`
does the `Reset()` — so no separate reset call is needed there, but `loadCmd`
must call `LoadInitial` (which resets) rather than assuming a fresh feed.

**Label**: `panelLabel(panelCommits, "Commits")` appends the count —
`Commits 250+` while `!commitsExhausted` (more may exist), `Commits 437` once
exhausted. Implemented by extending `panelLabel` (or its commits branch) to
append ` N` / ` N+` for the commits panel before the sort/filter suffixes.
Narrow layout shows the same label.

## Testing

`internal/git`: `Log` with skip>0 emits `--skip=N` in argv; skip==0 omits it
(FakeRunner argv assertion); existing log parse test updated to the new
signature.

`internal/domain` (`-race`):
- **LoadInitial** sets commits/skip/exhausted; `exhausted` true when the page
  is short (FakeRunner returns < 50).
- **LoadMore** appends the next page, advances skip, flips exhausted on a
  short page; a second LoadMore after exhaustion is a no-op `(.,false,.)`.
- **Hash-dedupe**: a page overlapping the loaded set (shared hashes) appends
  only the new commits.
- **Single-flight**: two concurrent LoadMore calls issue one `logPage`
  (blocking-runner pattern from stage 2's `TestSnapshotCoalesces`); the second
  returns the same state.
- **Generation drop**: a page that returns after a `Reset` is discarded
  (state reflects only the post-reset load).
- **NeedsMore**: true within `commitNearEnd` of the end, false when exhausted
  / in-flight / far from the end.
- **logPage holds a Read reservation** (Queue() observed mid-call), released
  after — same assertion style as stage 2.
- **Snapshot returns a copy** (mutating the returned slice doesn't affect the
  feed).
- `Snapshot` (the query) no longer issues `git log` (FakeRunner Calls has no
  `git log`).

`internal/tui`:
- Near-end movement fires a `loadMoreCmd` (cmd non-nil); movement far from the
  end does not.
- An active commits filter suppresses paging (no cmd even near the end).
- `commitsPagedMsg` mirrors the feed into `m.commits`; a stale-gen paged msg
  is dropped.
- Label shows `Commits N+` vs `Commits N` per `commitsExhausted`.
- `moveCommitUnderFilesView` near the end also triggers paging.
- Existing startup/load tests updated for the two-read loadCmd.

No e2e (TUI + read-model only; the CLI surface is unchanged).

## Docs

- `CHANGELOG.md`: a stage-3 bullet under "Domain layer & repo gate" — on-
  demand paged commit history; Commits panel no longer capped at 50.
- `CLAUDE.md`: `domain` package-map row gains `CommitFeed`; one line that the
  Commits panel is backed by the domain `CommitFeed` read-model.
- `help.go`: the Commits section notes that more commits load automatically as
  you scroll (no new key — it's automatic). Reference the diagrams file from
  this spec.
- No README change (no user-facing key/flag), no agent-skill bump.

## Not doing (YAGNI)

Cache (stage 6); explicit "load more" key (auto on scroll, as agreed);
configurable page sizes (constants); filter/sort across unloaded history
(loaded-only, label signals more); anchored paging; cancel-in-flight-on-move
(a page is cheap and the gen guard handles reloads).
