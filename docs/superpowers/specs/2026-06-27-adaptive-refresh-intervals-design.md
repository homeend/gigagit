# Adaptive Refresh Intervals (Phase C) — Design

**Date:** 2026-06-27
**Status:** Design approved, pending implementation
**Feature branch:** `feat/adaptive-refresh`

This is **Phase C** of the data-source refresh feature. It builds directly on:

- **Phase A** — per-source data-source registry (`internal/tui/source.go`),
  merged `8d5ce1f`.
- **Phase B** — background auto-refresh scheduler (`internal/tui/refresh.go`),
  merged `612a782`.

Read those specs/plans (`docs/superpowers/{specs,plans}/2026-06-27-data-source-registry*`
and `*-background-auto-refresh*`) for the surrounding architecture.

---

## Goal

Make the background auto-refresh lane **cost-aware and self-throttling** instead
of firing every due source in parallel on a fixed interval. A source whose reads
are expensive on *this* repo automatically backs off (or drops out of
auto-refresh entirely); cheap sources keep their configured rate. Background
reads run **one at a time** so they never bog down a large monorepo, while the
manual `r` refresh stays fully parallel and unchanged.

The optimization target is **cost back-off / performance protection**, not
change-rate freshness. (Change-rate detection was explicitly out of scope.)

## Non-goals

- No change-detection / freshness-driven polling (only read-cost drives
  adaptation).
- No new persisted state — all measurement is session-only, in-memory,
  re-seeded each session from the startup load.
- No coupling to `internal/observ` — the read is timed directly in the TUI.
- The whole feature stays **off by default** (inherits Phase B's master gate).

---

## User-facing behavior

### Two independent switches

| Key | Default | Meaning |
|-----|---------|---------|
| `[refresh] enabled` | `false` | Master auto-refresh on/off (Phase B). |
| `[refresh] disable_adaptive` | `false` | When false ⇒ adaptation **ON** (the normal mode). Set true for **fixed** configured intervals. |

`disable_adaptive` uses the inverted `disable_*` polarity (mirrors
`[ui] disable_slow_op_confirm`): the default behavior (adaptation on) is the
false/zero value, and only a `true` in a higher config layer overlays. The
Settings menu presents it positively as **"Adaptive intervals: on/off"**.

### Effective-interval rule (per scheduled item, evaluated each tick)

```
cfg = configured [refresh] seconds for the item (status/branches/.../fetch)

cfg == 0                          → OFF        (per-source off switch, unchanged)
adaptive OFF                      → cfg        (fixed: what you set is what runs)
no measurement yet                → cfg        (startup load seeds the first one)
avg(last ≤10 reads) > cutoff      → DISABLED   (auto off; only manual r runs it,
                                                 which re-measures and can re-enable)
otherwise                         → max(cfg, backoff_factor × avg)
```

- `cutoff` = `[refresh] max_read_seconds`, default **10**.
- `backoff_factor` = `[refresh] backoff_factor`, default **10**.
- `min_seconds` = `[refresh] min_seconds`, default **10** — a floor on every
  scheduled interval, so a very cheap source (sub-second read) doesn't poll every
  ~1s. The effective interval is `max(min_seconds, max(configured, factor × avg))`.
- `avg` = arithmetic mean of the **last up-to-10** measured durations for that
  item (a small ring buffer). Fewer than 10 samples → mean of what exists.
- The configured value is always a **floor**: adaptation only ever *lengthens*
  the interval, never polls faster than the user asked.

Worked example: `[refresh] status = 10`, adaptive on, status reads average 4s →
effective interval `max(10, 10×4) = 40s`. If they average 12s → `DISABLED`
(over the 10s cutoff), so status only refreshes on manual `r`.

### Single-lane background scheduler

- At most **one** background read is in flight at any time. Due items wait in a
  **FIFO queue** and drain one at a time; the running item plus the queue is the
  lane.
- **Dedup by type.** A given item type is never enqueued while an instance of
  the same type is already pending in the queue **or** currently running. So if
  A is queued and then B is queued, neither A nor B can be enqueued again until
  *both* have executed and left the lane. The queue therefore holds at most one
  of each type, and no type starves another by re-queuing ahead of it.
- **FIFO order.** Items run in the order they were enqueued (the order they
  first became due), not most-overdue-first — A enqueued before B runs before B.
- **Fetch shares this one lane.** On fetch completion, the handler marks
  `remotes` as due-now (resets its `lastRun`); `remotes` then enters the queue
  through the normal dedup path on the next tick — replacing Phase B's direct
  post-fetch remotes fire (which violated the single-lane invariant).
- **Manual `r` is untouched** — separate code path, fires all sources in
  parallel, as today.

### Indicator

While the single background read runs, show an unobtrusive **`⟳ <source>…`**
hint in the bottom status line. It clears when the read finishes. No countdown,
and no per-panel spinners (those stay reserved for manual `r`). This is a
deliberate, minimal relaxation of Phase B's "auto-refresh is fully silent" rule.

### Settings (`,`) surfaces

1. **"Adaptive intervals" toggle** — flips `disable_adaptive` in memory (next
   tick honors it) and persists via a new non-destructive writer
   `config.SetGlobalRefreshDisableAdaptive` (the 3rd runtime config writer, after
   `SetGlobalDebugLogOperations` and `SetGlobalRefreshEnabled`).
2. **"Refresh rates" viewer** — a read-only popup modeled on the existing
   "Session errors" viewer / operation-log surfaces. One row per scheduled item:

   ```
   source      configured   avg (n)     effective   state
   status      10s          4.1s (10)   41s         adaptive
   branches    15s          0.3s (10)   15s         adaptive (floor)
   remotes     30s          —           30s         fixed
   tags        20s          12.4s (7)   —           disabled (too slow)
   feed        10s          —           —           off
   fetch       60s          1.2s (4)    60s         adaptive (floor)
   ```

   State is derived from the effective-interval rule: `off` (cfg 0),
   `fixed` (adaptive off), `adaptive` / `adaptive (floor)` (running cfg as the
   floor), `disabled (too slow)` (over cutoff).

---

## Architecture & components

All changes are confined to `internal/tui` (scheduler + view + settings) and
`internal/config` (3 new keys + 1 writer). No engine/domain/git changes.

### `internal/tui/refresh.go` (rewrite of the scheduler core)

- **Measurement:** `dataAvailableMsg` gains a `dur time.Duration` field.
  `readSourceCmd` times the domain query around its call and sets `dur` (both
  manual and background reads measure). The `dataAvailableMsg` handler appends
  `dur` to the item's ring (capped at 10) — for *every* completed read, so the
  viewer and adaptation share one measurement source.

- **State (new `Model` fields, all in-memory / session-only):**
  - `refreshDur map[refreshItem][]time.Duration` — the per-item ring of the last
    ≤10 measured durations.
  - `bgQueue []refreshItem` — the FIFO of pending background items.
  - `bgBusy bool` + `bgActiveItem refreshItem` — the running-item tracker.
  - (existing `refreshLastRun`, `bgCtx`, `bgCancel` stay.)

- **Pure decision functions (table-tested, no `Model`/IO):**
  - `meanDuration(samples []time.Duration) time.Duration`
  - `effectiveInterval(cfg RefreshConfig, it refreshItem, avg time.Duration,
    haveSample bool) (secs int, state intervalState)` — returns the effective
    seconds and one of `stateOff/stateFixed/stateAdaptive/stateAdaptiveFloor/stateDisabled`.
    Encodes the full rule above.
  - `dueItems(now, lastRun, durs, cfg, suppressed) []refreshItem` — the items
    whose effective interval has elapsed this tick (per the rule above; `OFF`
    and `DISABLED` items excluded). Pure.
  - `enqueueDue(queue []refreshItem, active refreshItem, busy bool, due
    []refreshItem) []refreshItem` — appends each due item that is not already in
    `queue` and not the currently-running `active` (when `busy`). This is the
    dedup-by-type gate. Pure.

- **`refreshTick(now)`** rewritten:
  1. If suppressed → return (do not even enqueue).
  2. `queue = enqueueDue(queue, bgActiveItem, bgBusy, dueItems(...))` — add
     newly-due items, deduped by type.
  3. If `bgBusy` or `len(queue) == 0` → return (wait / nothing to do).
  4. Pop the front of `queue` into `bgActiveItem`, set `bgBusy`, bump
     gen/inflight, stamp `lastRun`, and run that single read.

- **Lane freeing:** the `dataAvailableMsg` handler, when the message is for
  `bgActiveItem`, clears `bgBusy` (even on error/cancel, so `startOp`
  preemption never strands the lane); the next tick drains the next queued item.
  `startOp` resets `bgBusy` **and clears `bgQueue`** when it cancels `bgCancel`
  (a user op preempts the whole background lane; still-due items re-enqueue
  naturally on the next post-op tick).

- **Fetch:** `bgFetchDoneMsg` clears the lane and resets `remotes`' `lastRun`
  to "due now" instead of firing the remotes read directly.

### `internal/config`

- `RefreshConfig` gains: `DisableAdaptive bool` (`disable_adaptive`),
  `MaxReadSeconds int` (`max_read_seconds`), `BackoffFactor int`
  (`backoff_factor`).
- `overlayRefresh` overlays them: `DisableAdaptive` with inverted polarity
  (true overlays), the two ints with the zero-is-unset rule. Effective defaults
  (10 / 10) are applied at read time in the TUI when the value is 0, matching how
  other "0 = unset → default" fields work.
- `write.go` gains `SetGlobalRefreshDisableAdaptive(disable bool)` — a
  non-destructive line edit of the global file's `[refresh] disable_adaptive`
  (writes the explicit `true`/`false`, since this is a default-on toggle the user
  can flip both ways at runtime).
- `template.go` gains a `settingDoc` per new key so `gg config init` emits them
  commented with defaults (hand-synced per the config-settings-registry rule).

### `internal/tui` (Settings + view)

- Settings menu: add the "Adaptive intervals" toggle row and the "Refresh
  rates" viewer entry, alongside the existing Phase B "auto-refresh" master
  toggle.
- The "Refresh rates" viewer composes its rows from `effectiveInterval` +
  `meanDuration` over the current `refreshDur`/config — pure formatting over
  already-held state.
- Status line: render `⟳ <source>…` when `bgBusy`.

---

## Testing

Pure, table-driven unit tests carry the logic; TUI-level tests cover plumbing.

- `meanDuration` — empty, partial (<10), full (10), and >10 (only last 10
  counted) sample sets.
- `effectiveInterval` — every state branch: off (cfg 0), fixed (adaptive off),
  no-sample fallback, disabled (avg > cutoff), floor (cfg wins), backed-off
  (factor×avg wins); default cutoff/factor applied when config is 0.
- `dueItems` — returns due items per the rule; excludes off/disabled; empty
  when suppressed.
- `enqueueDue` — dedup invariant: a type already in the queue, or the
  currently-running `active` type, is not re-appended; a new type is appended in
  FIFO order.
- Single-lane invariant — `refreshTick` runs no read while `bgBusy`; runs
  exactly one (the queue front) when free.
- FIFO / fairness — A enqueued before B runs first; once A and B are both
  queued, A is not re-enqueued or re-run until both have executed and the lane
  has drained.
- `startOp` clears `bgQueue` and `bgBusy`; items re-enqueue on the next tick.
- Lane freeing — `dataAvailableMsg` for the active item clears `bgBusy`,
  including the error/cancel path; `startOp` preemption clears it.
- Fetch — `bgFetchDoneMsg` clears the lane and marks remotes due (no direct
  fire).
- Measurement plumbing — `readSourceCmd` sets `dur`; the handler appends to the
  ring and caps at 10.
- Indicator — status line shows `⟳ <source>…` iff `bgBusy`.
- Config — overlay of the 3 new keys (inverted `disable_adaptive`,
  zero-is-unset ints); `SetGlobalRefreshDisableAdaptive` round-trips both
  values without disturbing the rest of the file.
- "Refresh rates" viewer — row rendering for representative item states.

Reuses Phase B's `refresh_test.go` patterns (fake clock via explicit `now`,
in-memory `Model`).

---

## Risks / edge cases

- **The app-start fan-out is NOT measured.** The startup load reads all sources
  in parallel (contending for the git-subprocess semaphore), so its durations are
  systematically inflated and unrepresentative. Those reads carry a `startup`
  flag and never feed the ring. **Manual `r` and single-lane background reads DO
  feed it** — `r` is the user's chosen way to seed measurements.
- **Adaptive derives the interval from measurements; the configured interval is
  an optional floor.** With adaptive on, a source polls at
  `max(configured, backoff_factor × avg)`. If no per-source interval is
  configured (floor 0), the source still auto-refreshes once measured, polling
  purely at `backoff_factor × avg`. Before its first measurement a floor-less
  source is **`pending`** (it begins after the first manual `r` or background
  read measures it). So enabling auto-refresh and pressing `r` once is enough to
  make every source self-tune — no per-source interval config required. With
  adaptive **off**, the configured interval is used verbatim and 0 = off.
- **Disabled source re-measures via `r`.** A `disabled (too slow)` source is
  excluded from background reads, so it gets no further background samples; but a
  manual `r` re-measures it (manual reads feed the ring) and can re-enable it if
  it has become fast.
- **Single sample noise** — averaging up to 10 samples damps a one-off slow
  read; an early fluke (1–2 samples) self-corrects as the ring fills.
- **Fetch is opt-in (network).** The `fetch` row is the periodic background
  `git fetch`. Unlike local source reads it does **not** floor-less auto-start
  and is never `pending`: with `[refresh] fetch` unset it is `off`. It runs only
  when `[refresh] fetch = N` is set. A foreground fetch (Remotes menu) records
  its duration into the fetch row for visibility (and refines the interval when
  the background fetch is enabled) but never turns the background task on.
- **Fetch self-disabling** — with `max_read_seconds = 10`, a `git fetch` on a
  huge repo can routinely exceed the cutoff and self-disable. This is the rule
  working as specified, not a bug; the viewer's `disabled (too slow)` state
  makes it legible.
- **Indicator vs. Phase B silence** — a deliberate, scoped relaxation; only the
  active `⟳` hint, nothing per-panel, no countdown.
- **Lane starvation** — `nextDueItem`'s most-overdue selection ensures every due
  source eventually runs; a single slow source can't monopolize the lane because
  it backs off after each read.

---

## Documentation to update at the end

- `CHANGELOG.md` (always).
- `README.md` — the `[refresh]` config keys + Settings surfaces.
- `CLAUDE.md` — the `internal/tui` refresh.go and `internal/config` entries
  (3rd runtime writer; adaptive scheduler).
- Memory: a new Phase C feature file + update `data-source-registry-feature.md`
  / `background-auto-refresh-feature.md` cross-links.
