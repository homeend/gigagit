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
- `avg` = arithmetic mean of the **last up-to-10** measured durations for that
  item (a small ring buffer). Fewer than 10 samples → mean of what exists.
- The configured value is always a **floor**: adaptation only ever *lengthens*
  the interval, never polls faster than the user asked.

Worked example: `[refresh] status = 10`, adaptive on, status reads average 4s →
effective interval `max(10, 10×4) = 40s`. If they average 12s → `DISABLED`
(over the 10s cutoff), so status only refreshes on manual `r`.

### Single-lane background scheduler

- At most **one** background read is in flight at any time. When multiple items
  are due, the scheduler runs the **single most-overdue** one, marks the lane
  busy, and waits for it to complete before picking the next.
- **Fetch shares this one lane.** On fetch completion, the handler marks
  `remotes` as due-now (resets its `lastRun`) and lets the scheduler pick it via
  the queue — replacing Phase B's direct post-fetch remotes fire (which violated
  the single-lane invariant).
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
  - `bgBusy bool` + `bgActiveItem refreshItem` — the single-lane tracker.
  - (existing `refreshLastRun`, `bgCtx`, `bgCancel` stay.)

- **Pure decision functions (table-tested, no `Model`/IO):**
  - `meanDuration(samples []time.Duration) time.Duration`
  - `effectiveInterval(cfg RefreshConfig, it refreshItem, avg time.Duration,
    haveSample bool) (secs int, state intervalState)` — returns the effective
    seconds and one of `stateOff/stateFixed/stateAdaptive/stateAdaptiveFloor/stateDisabled`.
    Encodes the full rule above.
  - `nextDueItem(now, lastRun, durs, cfg, suppressed) (refreshItem, bool)` —
    returns the single most-overdue due item (or false). Replaces the
    "fire all due in parallel" `dueItems`.

- **`refreshTick(now)`** rewritten: if suppressed or `bgBusy` → nothing.
  Otherwise call `nextDueItem`; if one is returned, run exactly that single
  read (set `bgBusy`/`bgActiveItem`, bump gen/inflight, stamp `lastRun`).

- **Lane freeing:** the `dataAvailableMsg` handler, when the message is for
  `bgActiveItem`, clears `bgBusy` (even on error/cancel, so `startOp`
  preemption never strands the lane). `startOp` also resets `bgBusy` when it
  cancels `bgCancel`.

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
- `nextDueItem` — picks the single most-overdue; returns false when none due,
  when suppressed, and respects per-source off/disabled.
- Single-lane invariant — `refreshTick` returns no command while `bgBusy`;
  fires exactly one when free.
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

- **Disabled source never re-measures on its own** — by design: once `avg >
  cutoff`, only manual `r` runs and re-measures it (which can re-enable it if it
  got fast). Documented as intended behavior, surfaced in the viewer.
- **Single sample noise** — mitigated by averaging up to 10; early on (1–2
  samples) a fluke can mis-classify, self-corrects as the ring fills.
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
