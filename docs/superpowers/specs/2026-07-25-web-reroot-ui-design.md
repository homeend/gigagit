# Web re-root UI + MRU recording — design

Date: 2026-07-25 · Branch: `feat/web-reroot-ui` off `web-dev` · Status: awaiting review

## Goal

Make the shipped `POST /api/reroot` reachable from the UI (worktree
right-click → "switch here") and close its known reachability gap: record
served/switched repos into the MRU registry so you can always navigate
back. The MRU repo *picker* UI waits for wave 3 (it needs the popup
infra); this increment ships its data endpoint.

## Changes

### 1. MRU recording (server)

- `web.Serve` records the served repo after a successful preflight:
  a small `touchMRU(ctx, svc, statePath)` helper (resolves
  `svc.TopLevel`, calls `repos.Touch(statePath, top, time.Now())`,
  best-effort — an error is ignored; recording must never block serving),
  called with `repos.DefaultStatePath()`. Extracted so it is unit-testable
  with a temp statePath; `New()` must NOT touch the registry (tests
  construct servers freely).
- `handleReroot` records the NEW root the same way after a successful
  swap (via the existing `s.reposStatePath()` seam). Combined effect: any
  root that was ever active is in the MRU, so re-rooting away is always
  reversible.

### 2. `GET /api/repos` (server)

Returns the MRU list: `{"repos": [{"path": ..., "name": ...}]}` —
`repos.Load(s.reposStatePath())` mapped through `repos.Name`. Read-only,
no guard beyond hostGuard (same standing as /api/worktrees). Feeds wave
3's picker; harmless to ship now.

### 3. Worktree ctx-menu "switch here" (client)

`showWorktreeMenu` gains a row — on every row except the currently served
worktree (`w.path === state.worktree`, the same exemption the remove row
uses): label `switch here`, action `POST /api/reroot {path: w.path}`;
on 200 → `location.reload()` (the whole client state is repo-scoped; a
clean reload re-reads everything, and localStorage prefs survive); on
error → the red status strip. No confirm — switching is non-destructive.
Help updated (worktree row: click still nothing / right-click: switch
here, copy path, remove).

### Stability contract (parallel-track safety)

This track only touches `showWorktreeMenu`, one new client function, the
help's worktree row, and server files the popup track never opens. It
consumes `showCtxMenu`/status-strip APIs as-is — the popup-infra track
guarantees those signatures stay stable.

## Testing

Go (real-git):
- `touchMRU` writes the served repo's toplevel into a temp statePath.
- After a successful `/api/reroot`, the statePath registry contains the
  new root (and the old one, pre-seeded by the test as `touchMRU` would).
- `GET /api/repos` returns the seeded registry, pruned of dead paths.

Playwright: a repo with a second worktree — right-click it in the
sidebar → "switch here" → the header shows the new worktree path after
the reload; `/api/repos` then lists both roots (fetched in-page); the
served row itself offers no "switch here".

## Out of scope

The MRU picker popup (wave 3); switching to a non-worktree repo from the
UI (needs the picker); any engine/domain change.
