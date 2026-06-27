# Design — Background auto-refresh (Phase B)

Date: 2026-06-27
Status: approved
Branch: `feat/refresh-scheduler`

## Motivation

Phase A (the data-source registry, merged `8d5ce1f`) made every panel's data an
independently refreshable **source** read via a domain query and delivered as a
`dataAvailableMsg`, with a `manual bool` distinguishing user-initiated reads
(which show a per-panel ⏳) from silent ones. Phase A only ever issues `manual`
reads; the silent path exists but nothing drives it.

Phase B drives it: gg periodically refreshes sources **on their own**, silently,
so panels stay current without the user pressing `r`. Different sources change at
very different rates, so each is scheduled **independently** with its **own
interval**, **individually configurable and individually toggleable**.

Because the tool targets ~100GB monorepos where git operations are assumed
expensive and user-initiated, the feature is **off by default** and entirely
opt-in. A user who never edits config sees Phase A behavior unchanged.

This is Phase B of three. Phase A (per-source manual refresh) shipped. **Phase C**
(adaptive intervals derived from measured read durations) is deferred; its seam
is the scheduler's due-table.

## Decisions (locked during brainstorming)

- **Polling, not watching.** There is no file-watcher infrastructure in the repo;
  cross-platform fsmonitor is a separate future feature. B uses timers.
- **Off by default, opt-in** via a new `[refresh]` config section.
- **Global master toggle.** A single `[refresh] enabled` switch gates the whole
  scheduler; when off, nothing auto-refreshes regardless of per-source intervals.
  It is also a **runtime toggle** in Settings (a quick pause/resume that does not
  require zeroing per-source intervals), persisted via a non-destructive config
  line-edit — the same pattern as the existing operation-log toggle
  (`config.SetGlobalDebugLogOperations`).
- **Per-source interval + per-source toggle.** Every source has its own seconds
  interval; `0` = off.
- **Auto-refresh is silent** — `manual=false`: no spinner, no focus change, no
  cursor movement (Phase A already specifies this).
- **Both local re-reads and background `fetch`** are scheduled, as items of the
  same scheduler; `fetch` is a special task (run `git fetch`, then refresh the
  `remotes` source).
- **User always wins:** a starting user op cancels in-flight background reads.
- **Bug #4 is a hard prerequisite** (see Components → #4 fix).

## Architecture

Phase B is a thin **scheduler** over Phase A. It adds no new refresh logic; it
decides *when* to call Phase A's existing `reloadSourcesCmd(sources, manual=false)`.

```
heartbeat tick (~1s, already exists)
        │
        ▼
  refreshTick: consult the due-table
        │  for each schedulable item:
        │    enabled (interval > 0) AND
        │    now - lastRun >= interval AND
        │    not suppressed AND
        │    not already in-flight
        ▼
  fire a SILENT refresh  →  readSourceCmd(source, manual=false)
                            (or the quiet fetch task)
```

One global tick, an in-memory per-item due-table (`lastRun` timestamps), no
per-source goroutines. The due-table is also the Phase-C seam: adaptive intervals
just change the comparison delta per item.

### Components

| Unit | Responsibility |
|------|----------------|
| `LimitRunner` ctx fix (**#4**, first task) | `gitexec/limit.go` `gitSem <- struct{}{}` becomes a `select` on `ctx.Done()` vs the send, so a blocked git call observes cancellation. Prerequisite for "user op cancels background read." |
| `[refresh]` config | New `internal/config` section: a master `enabled` bool (default `true`) plus per-item interval seconds (`status`/`branches`/`remotes`/`worktrees`/`tags`/`reflog`/`feed`/`fetch`), all default `0` (off). With `enabled=true` and all intervals `0`, the feature is still off — `enabled` is the master gate, intervals are the opt-in. Read at startup like the rest of config. |
| refresh scheduler (TUI) | `internal/tui` — the due-table + the `refreshTick` handler that fires silent reads. The master `enabled` gate short-circuits the whole tick when off. Reuses the heartbeat cadence (no new ticker). |
| master runtime toggle | A Settings (`,`) action flips `[refresh] enabled` at runtime — instant pause/resume of ALL auto-refresh without zeroing per-source intervals. Persisted via a non-destructive config line-edit (`config.SetGlobal…` mirroring `SetGlobalDebugLogOperations`, the operation-log toggle). The in-memory `m.cfg.Refresh.Enabled` flips immediately so the next tick honors it. |
| quiet `fetch` task | Run `engine.Fetch` WITHOUT the normal `startOp` (no op slot, no `m.running`, no busy line, no modal); on success refresh the `remotes` source; on failure stay silent (record to the session error log only). The riskiest component; implemented last. |
| background cancel ctx | Background reads run under a cancellable context the model holds (`bgCancel`); `startOp` cancels it so a user op preempts in-flight background work. |
| Settings surface | Settings (`,`) hosts the master runtime toggle (above) and shows the active per-source `[refresh]` schedule (which items are on and their intervals); per-source intervals are config-edited, not runtime-toggled. |

### Suppression — first-class

A scheduled refresh is skipped (not queued) when ANY of these hold:

- the master toggle is off (`!m.cfg.Refresh.Enabled`) — short-circuits the whole tick,
- an op is running (`m.running`),
- a popup / modal / decider / diff layer is open (`m.layers` non-empty or
  `m.modal != nil`),
- a filter or search is being typed (`m.filterTyping` / search-typing),
- that source is already in-flight (`m.srcInflight[s]`).

Silent means silent: the refresh never steals focus, never moves the cursor.
Phase A's selection-by-identity restore already preserves selection; B must hold
it even when focus is **on** the panel being refreshed.

### Precedence & cancellation

- Background reads run under `bgCancel`'s context. When the user starts an op
  (`startOp`), it cancels `bgCancel` so in-flight background reads abort and free
  the git semaphore for the user's work. (Requires the #4 fix to actually
  interrupt a read blocked on the semaphore.)
- Background reads never hold more than a `Read` reservation. `fetch` is a
  `RefWrite`, run quietly; it must not escalate to or block a user `TreeWrite`
  beyond the normal gate semantics.
- A superseded background read drops via Phase A's per-source generation
  (`srcGen`) exactly as a manual one does.

### The `fetch` task in detail

- Scheduled like any item (`[refresh] fetch = N`).
- Runs `engine.Fetch{}` through a **quiet execution path** (via `domain.Execute`
  under `RefWrite`) that does NOT touch `m.running`, the op slot (`m.proc`), the
  busy line, or the modal — a background fetch must never block the UI or
  intercept the single foreground-op slot.
- On success → refresh the `remotes` source (silent). On any error (network,
  auth, rate) → swallow; record to the always-on session error log; never a
  modal or status-line message.
- This overlaps the roadmap's "workspace group sync / parallel background-pull."
  B implements only single-repo scheduled fetch; group sync remains separate.

## What Phase B explicitly does NOT include

- No adaptive/measured intervals (Phase C).
- No event-driven / fsmonitor / inotify watching.
- No multi-repo / group background-pull (roadmap item).
- The ONLY runtime config *write* is the master `enabled` toggle (a
  non-destructive line-edit, exactly like the existing operation-log toggle).
  Per-source intervals stay read-at-startup; a live per-source runtime editor is
  a possible later addition, not in B.

## Testing

- **#4 fix:** a `Run`/`Stream` blocked on a full `gitSem` returns promptly when
  its context is cancelled (no leak, correct error).
- **Due-table:** an item fires only after its interval elapses and only when
  `interval > 0`; `lastRun` advances; two items with different intervals fire on
  their own cadence.
- **Suppression:** every gated condition (op running, layer/modal open, filter
  typing, source already in-flight) blocks a scheduled fire.
- **Silence:** a scheduled refresh sets no `srcLoading` and shows no spinner;
  cursor/selection survive a silent refresh of the focused panel.
- **Precedence:** starting an op cancels in-flight background reads (with #4).
- **Quiet fetch:** on success refreshes `remotes` and never sets `m.running` or a
  modal; on failure is silent and logged.
- **Master toggle:** with `enabled=false`, no item fires even when intervals are
  set; flipping it back on resumes; the runtime toggle persists via the
  non-destructive line-edit and flips the in-memory flag immediately.
- **Config:** `[refresh]` parses; all-zero default means the scheduler fires
  nothing (feature off).

## Risks

- **Bug #4 / cancellation correctness.** Getting "user op preempts background
  read" right is the core safety property; the #4 fix and the `bgCancel` wiring
  must be verified, including under git saturation (the 8-subprocess ceiling).
- **Quiet fetch.** Running an op outside the normal `startOp`/op-slot machinery
  risks two writers (a background fetch overlapping a user op). Mitigation: the
  quiet path takes only `RefWrite`, the gate serializes it against a user
  `TreeWrite`, and suppression skips scheduling fetch while an op runs. Implement
  it last, behind the scheduler that the local sources already exercise.
- **Background git load on a huge repo.** Mitigated by off-by-default + per-source
  opt-in + suppression; documented so users tune intervals to their repo.
- **Status pollability varies with `core.fsmonitor`.** Documented: on a repo
  where `git status` is slow, leave `status = 0` (or a long interval). A runtime
  cheap/expensive auto-detect is explicitly out of scope for B.
