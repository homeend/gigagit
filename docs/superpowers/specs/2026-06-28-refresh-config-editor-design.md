# Refresh Config Editor (Phase C rework) — Design

**Date:** 2026-06-28
**Status:** Design approved, pending implementation
**Feature branch:** `feat/adaptive-refresh` (continues; the branch name is now a
slight misnomer — the adaptive engine is being removed)

This **reworks** the adaptive-interval design
(`2026-06-27-adaptive-refresh-intervals-design.md`). It removes the adaptive
engine, keeps fixed-interval scheduling over the single-lane queue, keeps timing
measurements as **statistics only**, and turns the "Refresh rates" screen into
an **inline editor** for the `[refresh]` per-source intervals.

## Goal

Background auto-refresh is driven purely by user-configured intervals. Each
source polls on a fixed interval (floored at a minimum), through the existing
single-lane FIFO queue. The "Refresh rates" Settings screen lets the user **edit
those intervals in place** (writing the repo `.gg.toml`) and shows the **measured
read time** per source as informational statistics.

## Non-goals

- No adaptive/derived intervals, no back-off, no read-cost cutoff, no
  auto-disable. The user sets the rates.
- No general-purpose config editor — the screen edits only the `[refresh]`
  per-source intervals.
- No change to the single-lane queue / lane lifecycle (kept verbatim).

---

## Scheduling

Per scheduled item (the 8 sources: status, branches, remotes, worktrees, tags,
reflog, feed, fetch):

```
configured = [refresh] <source> seconds (0 = off)
min        = [refresh] min_seconds (config-file only; 0/unset → default 10)

configured <= 0  → OFF (never auto-refreshes)
otherwise        → effective = max(min, configured)
```

A pure helper replaces the adaptive `effectiveInterval`:

```go
// scheduledInterval returns an item's fixed poll interval in seconds and whether
// it is scheduled at all. Measurements do NOT affect it.
func scheduledInterval(cfg config.RefreshConfig, it refreshItem) (secs int, on bool)
```

`dueItems(now, lastRun, cfg, suppressed)` uses `scheduledInterval`: an item is
due when `on && (unseen || now - lastRun >= effective)`. The `durs` (duration
ring) parameter is dropped from `dueItems` — scheduling no longer reads it.

Everything downstream of `dueItems` is unchanged: `refreshTick` enqueues due
items into `bgQueue` (deduped), drains one at a time under `bgBusy`/`bgActiveItem`,
the lane-clear in the `dataAvailableMsg` handler, `startOp` clearing, the
srcInflight drop guard, the fetch path, and the `bgFetchDoneMsg` rewrite all
stay. Fetch is just a normal row: it runs only when `[refresh] fetch > 0` (the
old adaptive "config-gated" special case becomes the universal rule and
disappears).

## Statistics (measurement kept, scheduling-independent)

The duration ring (`refreshDur`, ≤10 samples per item) and `recordDuration` stay.
Reads still record their wall-clock: manual `r` and single-lane background reads
record; the app-start fan-out does not (`dataAvailableMsg.startup`); a foreground
`git fetch` records via `opIsFetch`. This now feeds **only** the stats column in
the editor. The `⟳ <source>…` status hint stays, still suppressed when the
active item's rolling average is < 1s.

## "Refresh rates" → inline editor

A Settings (`,`) → "Refresh rates" screen (replaces the read-only viewer),
modeled on the existing editable-popup patterns. One row per scheduled source:

```
  status      every 30s     avg 120ms (10)
> branches    off           avg —
  remotes     every 10s     avg 47ms (10)
  …
  fetch       off           avg 0.6s (2)
```

- **Columns:** name · interval (`every Ns` / `off`) · avg stat (`Xms (n)` /
  `X.Xs (n)` / `—`). When `configured` is below `min_seconds` the interval shows
  the effective floored value with a marker (e.g. `every 10s (min)`), so the user
  sees what actually runs.
- **Navigation:** `↑/↓` select a row.
- **Edit:** `enter` (or `e`) on a row opens an **inline numeric field** (reusing
  gg's cursor-aware `textfield`); type seconds, `enter` saves, `esc` cancels.
  Only digits accepted; empty/`0` ⇒ off.
- **Save:** writes `[refresh] <source> = N` to the **repo `.gg.toml`**, updates
  `m.cfg.Refresh` in memory (next tick honors it), and re-seeds that item's
  `refreshLastRun = now` (no enable-burst). On write error, a status message and
  the in-memory value still applies.
- If `[refresh] enabled` is false (master off), the editor still edits values
  and shows a one-line hint that auto-refresh is off (toggle it in Settings).
- `esc` from the screen returns to the Settings menu (never traps).

## Config changes

`internal/config`:
- **Remove** from `RefreshConfig`: `DisableAdaptive`, `MaxReadSeconds`,
  `BackoffFactor` (+ their `overlayRefresh` blocks + their `settingDoc` rows).
- **Remove** `SetGlobalRefreshDisableAdaptive`.
- **Keep** `MinSeconds` (default applied at read time = 10) + its settingDoc.
- **Add** `SetRefreshInterval(path, source string, secs int) error` — a thin
  wrapper over `setScalarLine(path, "refresh", source, strconv.Itoa(secs))` that
  writes a per-source interval to the given file (the repo `.gg.toml`).
- The TUI captures the repo `.gg.toml` path (`<repo-top>/.gg.toml`) at bootstrap
  so the editor knows where to write (the model stores it alongside `cfg`).

## TUI changes

- **Remove:** `effectiveInterval`/`effectiveIntervalRaw`, `intervalState` + its
  constants + `stateLabel`; the `backoff_factor`/`max_read_seconds` consts;
  `refreshRateRows` (replaced by the editor's row rendering); the Settings
  "Adaptive intervals" entry + `toggleAdaptive`.
- **Add:** `scheduledInterval`; the editor screen (a `settingsPopup.ratesEdit`
  mode with a selection index + an inline edit field), its key handling and
  render; the repo-config-path capture; a save action calling `SetRefreshInterval`.
- **Keep:** the single-lane scheduler, measurement plumbing, `min_seconds` floor,
  `bgRefreshHint`.

## Testing

- `scheduledInterval`: off when configured 0; floored to min when below; passthrough
  when ≥ min; custom `min_seconds`.
- `dueItems` (new signature, no durs): due/not-due across fixed intervals; off
  items never due; suppressed → none.
- Editor: row rendering (interval + avg formatting, the `(min)` marker); enter →
  edit field opens; typing digits then enter saves → `SetRefreshInterval` called,
  `m.cfg.Refresh` updated, `lastRun` reseeded; esc cancels (no write); `0`/empty ⇒
  off; non-digit input rejected.
- `SetRefreshInterval` round-trips a value and preserves unrelated lines/comments
  in the repo `.gg.toml`.
- Measurement still recorded for manual/background/foreground-fetch, not startup
  (retained tests); `bgRefreshHint` sub-1s suppression (retained).
- Config: `RefreshConfig` no longer has the removed fields (compile-level); the
  `settingDocs` guard passes after removing the three rows and keeping `min_seconds`.

## Risks / edge cases

- **Editor write target** — the repo `.gg.toml` may not exist yet; `setScalarLine`
  already creates the file/section if absent (atomic write). The repo-top path is
  resolved once at bootstrap (same path `config.Load` reads), so editor writes and
  config reads agree.
- **Master off** — editing intervals while `enabled=false` writes config but
  nothing polls until enabled; the screen states this.
- **Removed keys in an existing config file** — a user's `.gg.toml` may still
  contain `disable_adaptive`/`max_read_seconds`/`backoff_factor`; with the fields
  gone they are simply ignored by the TOML decoder (no error). Documented.

## Documentation to update

- `CHANGELOG.md` (rework entry), `README.md` (drop adaptive keys; document the
  editor + fixed intervals + `min_seconds` floor), `CLAUDE.md` (tui + config
  rows), memory.
