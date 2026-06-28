# Auto remote-tag refresh on tag-window changes — design

**Date:** 2026-06-28
**Status:** approved (questions answered)
**Builds on:** the tag pushed-state indicator (`▲`) feature.

## Problem

The `▲` "tag is on the remote" marker is currently populated only on demand
(Tags `.`-menu "Refresh remote status") or via an opt-in timed `[refresh]
remote_tags` interval (default off). So out of the box the marker stays blank
until the user explicitly asks. We want it to populate and stay current
automatically whenever the tag list changes.

## Desired behavior

Automatically run a background remote-tag lookup whenever the **tag window's
contents change**:

- **On app load** — once the tags panel is first populated.
- **After any tag add / remove / push / delete-from-remote** — once the tags
  panel re-populates following the operation.

This is **on by default**, with a setting to disable it.

## Key insight — one trigger covers all cases

The tag window updates exactly when the `srcTags` data source delivers data
(`dataAvailableMsg{source: srcTags}`). That single event already fires for every
case above:

- App-start fan-out reads `srcTags` → fires.
- Tag-mutating ops (CreateTag, DeleteTag, PushTag, DeleteRemoteTag, annotate)
  are unmapped in `opAffectedSources`, so they reload **all** sources including
  `srcTags` → fires. (Non-tag ops like Commit map to specific non-tag sources,
  so they do NOT reload `srcTags` and correctly do NOT trigger this.)
- Manual `r` reloads all sources → `srcTags` → fires (a user-requested refresh;
  acceptable to refresh remote state then too).

So the trigger is: **on `srcTags` arrival, if enabled and there is at least one
tag, enqueue a background remote-tags lookup.**

## Architecture

Reuse the existing single-lane background machinery from the `▲` feature:

- In the `srcTags` arm of the `dataAvailableMsg` handler (`model.go:627`), after
  applying `m.tags`, enqueue the synthetic `remoteTagsItem` onto `m.bgQueue` via
  the existing `enqueueDue` (dedup-by-type). `refreshTick` drains it on the next
  heartbeat through the established path (`remoteTagsCmd(bgCtx, false)` → silent
  failures, lane-free on completion, op preemption via `bgCancel`).
- Gate the enqueue on `m.autoRemoteTagsEnabled()` (= `!cfg.Refresh.DisableRemoteTagsAuto`)
  and `len(m.tags) > 0`.

This is **independent of the `[refresh] enabled` master switch**: the master
switch gates only the timed `dueItems` path; a directly-enqueued item drains
regardless. That matches "default on" without forcing the whole timed-refresh
system on.

No new lifecycle, message, or lane is introduced — only a new enqueue site and a
config gate.

## Setting

A default-ON behavior. Because the config overlay uses the zero-is-unset rule
(a zero/false value reads as "unset"), a default-true bool cannot be expressed
directly — so we store the **inverted** flag, matching the established
`[ui] disable_slow_op_confirm` / `[refresh] enabled` polarity convention:

- `config.RefreshConfig.DisableRemoteTagsAuto bool` (`toml:"disable_remote_tags_auto"`),
  default `false` → auto-refresh is ON.
- `overlayRefresh`: inverted polarity (`if src.DisableRemoteTagsAuto { dst… = true }`),
  exactly like `Enabled`. Consequence (accepted, matches all inverted bools): a
  higher layer can disable a lower layer's default, but cannot re-enable over a
  layer that disabled it. The normal defaults→global→repo overlay still applies,
  so a **repo `.gg.toml` can disable** even when global leaves it on.
- A `settingDoc` in `template.go` (auto-feeds `gg config init` / `populate`).

### Exposure

- **Settings `,` menu toggle** — new entry "Auto remote-tag refresh: on/off",
  presented positively (on = `!DisableRemoteTagsAuto`), placed adjacent to the
  existing "Auto-refresh" entry. Toggling flips the in-memory value and persists
  to the **global** config via a new `config.SetGlobalDisableRemoteTagsAuto`
  writer (mirrors `SetGlobalRefreshEnabled` / `toggleAutoRefresh`).
- The raw config key, editable in `.gg.toml`.

## Out of scope

- Changing the manual `.`-menu action or the timed `[refresh] remote_tags`
  interval (both remain; the interval is now largely redundant for local changes
  but still catches remote-side changes by others).
- Diffing old vs new tag lists to suppress no-op refreshes (the over-trigger on a
  few unrelated all-source-reloading ops is one cheap, silent `ls-remote`;
  not worth the complexity).

## Testing

- `internal/config`: overlay (inverted polarity) + settingDoc coverage
  (`TestSettingDocsCoverAllFields`) + the global writer.
- `internal/tui`: `srcTags` arrival enqueues `remoteTagsItem` when enabled and
  tags exist; does NOT enqueue when disabled or when there are no tags; the
  Settings toggle flips state and the menu label reflects it.
