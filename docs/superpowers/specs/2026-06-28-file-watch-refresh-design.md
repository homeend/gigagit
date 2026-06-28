# Phase D — File-watch auto-refresh — Design

**Status:** approved (brainstorm), ready for planning
**Date:** 2026-06-28
**Branch:** `feat/file-watch-refresh`

## Goal

Refresh a panel the moment its underlying git state changes — instead of waiting
for the next poll tick — by watching a **fixed, small set of `.git` internal
files** with `fsnotify`. The watcher is an opt-in, per-source trigger that feeds
the **existing** single-lane background-refresh queue. Interval polling remains
the fallback and is the sole mechanism where file-watching is not viable
(notably WSL2 `/mnt` drvfs/9p mounts).

This is Phase D of the refresh feature (A: per-source registry; B: background
scheduler; C: fixed-interval editor). It adds no new refresh machinery — only a
new *trigger* into the lane built in B/C.

## Non-goals

- **No whole-worktree watching.** A 100GB monorepo has too many files; recursive
  worktree watches exhaust inotify limits and contend with other tools. Any
  window whose state depends on the working tree is therefore excluded.
- **`status` is excluded.** Its modified/untracked counts require watching every
  project file. It stays interval/manual only. (This was the user's explicit
  counter-example.)
- **`tags` and `feed` are deferred.** They ride the same refs trees as
  branches/remotes and can be added later with the identical machinery.
- **`identity` is excluded.** It is not auto-refreshed today (changes only via
  the `SetIdentity` op).
- No replacement of the heartbeat, the lane, or the interval-editor from Phase C.

## v1 watch set

Four windows get a file-watch toggle: **worktrees, branches, reflog, remotes.**

Per-window analysis of the file footprint (`$C` = git common dir =
`git rev-parse --git-common-dir`; `$W` = per-worktree git dir =
`git rev-parse --git-dir`):

| Window     | Watch footprint                                                        | Notes |
|------------|-----------------------------------------------------------------------|-------|
| reflog     | `$W/logs/` (filter `HEAD`) — one file                                  | tiniest |
| worktrees  | `$C/worktrees/` — one small dir                                        | tiny |
| branches   | `$C/refs/heads/**` (recursive dirs) + `$C` (filter `packed-refs`) + `$W` (filter `HEAD`) | bounded by ref *directories*, not ref count |
| remotes    | `$C/refs/remotes/**` (recursive dirs) + `$C` (filter `packed-refs`, `FETCH_HEAD`, `config`) | same refs-tree shape |

`reflog` and `worktrees` are trivially small (one file / one dir). `branches`
and `remotes` ride `refs/*` trees: we watch **directories** (not individual
refs), so the watch count grows with ref-namespace *directories*, not with the
number of refs — and `packed-refs` (a single file) catches the bulk in a packed
monorepo.

A single directory event can map to several sources: e.g. `$C/packed-refs`
affects both branches and remotes; `$C/config` affects remotes. The path→source
mapping therefore returns a **set** of sources per event.

## Architecture

```
fsnotify (inotify/kqueue/RDCW)
        │  raw fs events on watched .git dirs
        ▼
internal/gitwatch.Watcher  ── debounce per-source ──▶ Events() <-chan Source
        ▲                                                     │
        │ Plan(commonDir, worktreeDir, enabled)               │  gitwatch.Source
        │ (pure: dirs to watch + path→Source predicate)       ▼
        │                                          tui: blocking tea.Cmd →
   gitwatch.Supported(commonDir)  (statfs 9P gate)   watchEventMsg{sourceKey}
                                                              │
                                                  enqueueDue(bgQueue, …)  ◀── existing lane
                                                              │
                                                  refreshTick drains one item ≤1s
```

The watcher is purely a new *trigger*. When an event for source `S` arrives, the
TUI enqueues `S` into `bgQueue` exactly as a due timer would. The Phase B/C lane
then drains it under all its existing guards (single-lane, dedup-by-type,
suppression during ops/modals, op-preemption via `bgCancel`).

## Package: `internal/gitwatch`

Pure of git/TUI/domain imports; depends only on `fsnotify` + stdlib. Mirrors the
boundary of `commitgraph`/`textdiff`/`fuzzy`.

### Types and functions

```go
// Source is a watch-eligible data source. gitwatch owns this enum so the
// package stays decoupled from internal/tui's sourceKey; the TUI maps
// gitwatch.Source → sourceKey.
type Source int
const (
    Branches Source = iota
    Remotes
    Reflog
    Worktrees
)

// Group is one watched directory plus the predicate deciding which sources a
// change to a given basename affects.
type Group struct {
    Dir       string                 // directory to watch (events are non-recursive)
    Recursive bool                   // true for refs/heads, refs/remotes subtrees
    Match     func(base string) []Source // basename → affected sources ([] = ignore)
}

// Plan returns the directories to watch for the enabled sources, with each
// dir's path→source predicate. Pure and fully unit-testable: all .git-layout
// knowledge lives here. commonDir and worktreeDir are absolute, cleaned paths.
func Plan(commonDir, worktreeDir string, enabled []Source) []Group

// Supported reports whether file-watching is viable for the repo whose git
// common dir is commonDir. On Linux it returns false for 9P/v9fs mounts
// (WSL2 drvfs, fs magic 0x01021997); true otherwise. On non-Linux it returns
// true (kqueue/ReadDirectoryChangesW are assumed viable).
func Supported(commonDir string) bool

// Watcher watches the planned groups and emits a debounced Source on change.
type Watcher struct { /* … */ }

// New builds a Watcher over the groups, walking recursive groups to add a watch
// per existing subdirectory and arranging to watch newly-created subdirectories.
// debounce coalesces a burst of events per source into one emission.
func New(groups []Group, debounce time.Duration) (*Watcher, error)

func (w *Watcher) Events() <-chan Source // closed on Close
func (w *Watcher) Close() error
```

### Watcher behavior

- **Watch directories, never files.** git writes a ref by creating `<ref>.lock`
  and renaming it over `<ref>`; an atomic rename replaces the target inode, so a
  watch on the file's inode misses the update. Watching the parent dir and
  filtering by basename (via `Group.Match`) catches create/write/rename/remove.
- **Recursive groups** (`refs/heads`, `refs/remotes`): at `New`, walk the subtree
  and `Add` a watch for every existing directory. On a directory-create event
  inside a recursive group, `Add` the new directory (and any children created
  before we caught up). Files within are matched by `Group.Match`.
- **Debounce per source** (default 200ms): a fetch/checkout rewrites many refs in
  a burst (lockfile churn). Each incoming event resolves to a source set; a
  pending timer per source coalesces the burst into a single emission. Emission
  is non-blocking-safe: if no reader is draining, the latest pending source is
  retained (a dropped duplicate is harmless — the lane dedups anyway).
- **Missing dirs are skipped, not fatal.** A repo with no `refs/remotes` yet, or
  no `logs/`, simply has no watch there; the dir is picked up if later created
  (its parent is watched where practical) or on the next watcher rebuild.
- **`Close`** stops the fsnotify watcher, drains/halts timers, and closes
  `Events()`.

### `Supported` build-tag split

- `supported_linux.go`: `syscall.Statfs(commonDir, &st)`; return
  `st.Type != v9fsMagic` where `const v9fsMagic = 0x01021997`. On statfs error,
  return `true` (fail open to the normal watch path; a genuinely broken path
  surfaces elsewhere).
- `supported_other.go` (`//go:build !linux`): `return true`.

## Config

New per-source booleans in the `[refresh]` section (default `false`):

```
[refresh]
worktrees_watch = false
branches_watch  = false
reflog_watch    = false
remotes_watch   = false
```

- `config.RefreshConfig` gains `WorktreesWatch, BranchesWatch, ReflogWatch,
  RemotesWatch bool`.
- `overlayRefresh` overlays them (a bool is overlaid when the source key is
  present; follow the existing inverted-vs-normal pattern used for `Enabled` —
  these are normal-polarity, default false).
- `config.SetRefreshWatch(path, source string, on bool) error` — a non-destructive
  line-edit writer mirroring `SetRefreshInterval`, writing
  `[refresh] <source>_watch = <bool>` to the repo `.gg.toml`.
- `settingDocs` (in `template.go`) gains the four `*_watch` keys so
  `gg config init`/`populate` document them.

Watch keys are independent of interval keys; a source can have both an interval
and a watch toggle. The interval is what the source falls back to when watching
is off or unsupported (see Semantics).

## Semantics (per watch-eligible source)

Let `watchOn` = the source's `*_watch` config bool, and
`watchSupported` = `gitwatch.Supported(commonDir)` (computed once at startup /
repo switch, stored as `m.watchSupported`).

| watchOn | watchSupported | Behavior |
|---------|----------------|----------|
| false   | (any)          | Interval polling, exactly as today (0 = off). |
| true    | true           | **Event-driven.** The source is **skipped in `dueItems`** (interval not polled); the watcher triggers its refresh. |
| true    | false (drvfs)  | Watch unavailable → **interval polling** at the source's configured interval (0 = manual-only). |

So a source is **watch-active** iff `watchOn && watchSupported`. Watch-active
sources are excluded from interval polling; everything else polls as in Phase C.
The interval field thus doubles as the drvfs/off fallback — "polling remains the
fallback" with no extra config.

### Scheduler change

`dueItems` (and `refreshTick`) must skip watch-active sources. Implementation:
add a pure predicate

```go
func watchActive(cfg config.RefreshConfig, watchSupported bool, it refreshItem) bool
```

(returns false for `fetch` and for non-watch-eligible sources), and have
`dueItems` skip an item when `watchActive(...)` is true. `dueItems` gains a
`watchSupported bool` parameter; `refreshTick` passes `m.watchSupported`.
`fetch` is never watch-eligible and is unaffected.

## TUI wiring

- **Model fields:** `watcher *gitwatch.Watcher`, `watchSupported bool`,
  `watchCommonDir string` (to detect when a repo switch needs a rebuild).
- **`gitwatch.Source → sourceKey` map:** `Branches→srcBranches`,
  `Remotes→srcRemotes`, `Reflog→srcReflog`, `Worktrees→srcWorktrees`.
- **Enabled set helper:** `enabledWatchSources(cfg) []gitwatch.Source` from the
  four config bools.
- **Startup:** in the `configReadyMsg` handler, once `repoTOML`/common dir are
  known, set `m.watchSupported = gitwatch.Supported(commonDir)`; if supported and
  at least one source is watch-on, build the watcher (`startWatchCmd`) and start
  the blocking `watchListenCmd`. (Common dir is obtained via `svc.GitCommonDir`;
  `configReadyMsg` is extended to carry it, computed in `bootstrapCmd` alongside
  `TopLevel`.)
- **`watchEventMsg{source sourceKey}` + `watchListenCmd`:** a `tea.Cmd` that
  receives one `gitwatch.Source` from `Events()`, maps it to `sourceKey`, and
  returns `watchEventMsg`. Its handler:
  1. If the source is no longer watch-active (toggled off / unsupported), ignore.
  2. `m.bgQueue = enqueueDue(m.bgQueue, m.bgActiveItem, m.bgBusy,
     []refreshItem{{source: s}})`.
  3. Re-issue `watchListenCmd` to keep listening (channel-blocking loop).
  The next `refreshTick` (≤1s) drains the lane under existing guards. (Latency is
  bounded by the 1s heartbeat — acceptable for v1; instant drain is a possible
  later refinement.)
- **Repo switch (`reRoot`):** `Close()` the old watcher, recompute
  `watchSupported` for the new common dir, rebuild if applicable.
- **Editor toggle rebuild:** flipping a source's watch bool in the editor
  rebuilds the watcher plan (close + rebuild with the new enabled set) and
  reseeds `refreshLastRun` for that source.
- **Quit:** `Close()` the watcher.

The blocking-channel `tea.Cmd` loop is the standard Bubble Tea pattern for
external event sources; the watcher goroutine lives inside `gitwatch`.

## Refresh-rates editor

The Phase C inline editor (`ratesView`) gains a **watch toggle** on the four
watch-eligible rows:

- A row shows its interval and, for watch-eligible sources, a watch indicator:
  - watch off → interval as today.
  - watch on + supported → `watch`.
  - watch on + unsupported → `watch (9p → Ns)` (annotates the drvfs fallback to
    the interval).
- A key (e.g. `w`, advertised in the editor's help/footer) toggles the focused
  watch-eligible row's watch bool, persists via `config.SetRefreshWatch` to the
  repo `.gg.toml`, updates in-memory cfg, rebuilds the watcher, reseeds that
  source's `refreshLastRun`. Non-watch-eligible rows (status, fetch, tags,
  commits) ignore `w`.

(Per the project memory: advertise the new key in both the editor help and the
footer.)

## Error handling

- `gitwatch.New` error → log via the existing status line, leave the source on
  interval polling (no watcher). Never fatal.
- fsnotify error channel → drained and ignored per-event; a persistent error
  does not crash the TUI (worst case: that source falls back to its interval on
  the next manual `r`/tick — but note watch-active sources are not interval-
  polled, so a dead watcher means stale-until-`r`; acceptable for v1 and
  mitigated because the lane still serves manual `r`).
- Dropped/overflowed fsnotify events: harmless — the lane dedups, and a missed
  event only delays one refresh until the next relevant event or manual `r`.
- drvfs (9p): no watcher; interval polling is the mechanism.

## Testing

- **`gitwatch.Plan` (pure):** table tests asserting the watched dirs and the
  path→source predicate for each enabled-set combination, including the
  multi-source cases (`packed-refs` → {Branches, Remotes}, `config` → {Remotes},
  `HEAD` → {Branches}). Worktree-split case: `$C != $W`.
- **`gitwatch.Supported`:** assert `true` for an ext4 temp dir (`t.TempDir()` =
  `/tmp`, ext4 here) and assert the magic constant value (`v9fsMagic ==
  0x01021997`); the 9P branch can't be forced in CI.
- **Live watcher integration (Linux, native FS):** in a `t.TempDir()` git repo,
  start a `Watcher`, create/update a loose ref under `refs/heads/`, and assert a
  `Branches` event arrives within a timeout; likewise touch `worktrees/` and
  `logs/HEAD`. This is the "test it in WSL2" requirement — `/tmp` is ext4, so the
  working watch path is exercised; the `/mnt` (9p) path is covered by `Supported`
  returning false.
- **Scheduler:** `watchActive` truth-table test; `dueItems` skips watch-active
  sources and still polls watch-on-but-unsupported sources at their interval.
- **Config:** `SetRefreshWatch` round-trip (write → reload → bool set), additive
  and non-destructive (other keys preserved); `overlayRefresh` overlays the four
  bools.
- **TUI:** `watchEventMsg` handler enqueues the mapped source and ignores a
  no-longer-watch-active source; editor `w` toggle flips + persists + rebuilds.
- **Manual WSL2 check (documented, not automated):** run `gg` on a `~`-hosted
  (ext4) repo → watch fires; run on `/mnt/t` (9p) → watcher disabled, interval
  fallback. Recorded in the PR/changelog notes.

## Phasing

One spec, plan ordered in two phases:

- **D1 — infrastructure + trivial sources:** `internal/gitwatch` (`Source`,
  `Plan` for reflog+worktrees, `Supported`, `Watcher` for non-recursive groups),
  config bools + `SetRefreshWatch` + settingDocs, scheduler `watchActive` skip,
  TUI wiring (model fields, common-dir in `configReadyMsg`, `watchEventMsg` +
  listen loop, lifecycle), editor watch toggle — wired for **worktrees** and
  **reflog**. End state: file-watch works for the two trivial sources on a native
  FS and is disabled on 9p.
- **D2 — refs-tree sources:** extend `Plan` + `Watcher` with recursive subtree
  watching (walk + new-dir handling) and enable **branches** and **remotes**.

## Dependencies

- Add `github.com/fsnotify/fsnotify` to `go.mod` (the de-facto Go file-watch
  library; cross-platform inotify/kqueue/ReadDirectoryChangesW).

## Affected files (anticipated)

- New: `internal/gitwatch/{gitwatch.go, plan.go, watcher.go, supported_linux.go,
  supported_other.go, *_test.go}`
- `internal/config/{config.go (RefreshConfig + overlay), write.go
  (SetRefreshWatch), template.go (settingDocs)}`
- `internal/tui/{model.go (fields, configReadyMsg handler, watchEventMsg,
  lifecycle), source.go (configReadyMsg common dir, source map),
  refresh.go (watchActive, dueItems signature), settings_popup.go (editor
  toggle), help.go/footer (advertise `w`)}`
- `go.mod`, `go.sum`
- Docs: `CHANGELOG.md`, `README.md` (refresh section), `CLAUDE.md` (package map +
  `[refresh]` keys).
