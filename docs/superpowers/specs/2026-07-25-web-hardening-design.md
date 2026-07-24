# Web transport hardening — design

Date: 2026-07-25 · Branch: `feat/web-hardening` off `web-dev` · Status: approved

## Goal

Close the known reliability gaps in the web op transport and modal, plus
three reviewed-and-deferred minors. No new ops, no new endpoints.

## Background (verified against the code)

- The SSE stream (`GET /api/op/{id}/events`) sends no `id:` fields and
  ignores `Last-Event-ID`; every (re)subscribe replays the **full history**
  (`opRun.subscribe` clones `history`). Client rendering is almost
  replay-idempotent: `progress` overwrites the single op line, `gitline` is
  ignored, `done` is terminal.
- The one replay hole: an already-**answered `decision` re-opens the modal**
  on replay — nothing in the history marks it consumed. The same root cause
  leaves a **second tab's modal open forever** after the first tab answers
  (`decide` clears `pending` server-side but publishes nothing).
- `es.onerror` is a no-op. EventSource semantics: `readyState CONNECTING`
  in `onerror` = the browser is auto-retrying (transient network drop);
  `readyState CLOSED` = permanent failure (HTTP error status — e.g. the
  server restarted and the op id is gone, 404). Today a permanent failure
  leaves the op line stuck, pull/push disabled, and `state.op` set — every
  future op silently refuses.
- Decision options are English protocol values (the i18n layer never
  translates them), so a client-side destructive-option set is reliable.
- Web-reachable destructive options today: `force`, `force-with-lease`
  (push-force), `force-delete` (branch-unmerged), `reset` (pull's diverged
  fork), `unlock-and-remove` (worktree-locked), and the local confirms'
  `delete` (delete-tag) and `drop` (stash-drop).

## Changes

### 1. `resolved` wire event (server, `oprun.go`)

When `opRun.decide` accepts an answer, publish `{"type":"resolved"}` into
the history and fan-out (inside the same `r.mu` critical section that
consumes the pending decision — note `publish` also locks `r.mu`, so the
append+fanout must run inline or via an unexported locked helper, not by
calling `publish` under the lock).

Client (`handleOpEvent`): on `resolved`, `hideModal()`.

Effects: replay becomes genuinely idempotent (`decision` → `resolved`
re-shows then re-hides), and a second tab's modal closes live when the
first tab answers. A decision answered by nobody (timeout) already closes
via `done`.

### 2. SSE-drop recovery (client, `startOp`)

Replace the no-op `onerror`:

- `readyState === EventSource.CONNECTING`: transient. Show
  `⟳ reconnecting…` on the op line and increment a consecutive-error
  counter. `onopen` resets the counter (and the op line falls back to the
  next `progress`/replay event).
- `readyState === EventSource.CLOSED`, **or** the consecutive-error counter
  reaches 5: the op is lost. Close the source, clear `state.op`, re-enable
  `pull-btn`/`push-btn`, `hideModal()`, show a red op line
  `error: lost connection to operation — repo state refreshed`, and run
  `refreshAfterOp()` so panels reflect whatever the op actually did.

Rejected alternatives: `Last-Event-ID` incremental replay (machinery, no
benefit at probe-tier event volumes); a polling `GET /api/op/{id}` status
endpoint (cannot help in the only uncovered case — the server process is
gone; EventSource retry + full replay covers every case where the server
is alive).

### 3. Destructive-option styling in the decision modal (client)

A `DANGER_OPTIONS` set: `force`, `force-with-lease`, `force-delete`,
`reset`, `delete`, `drop`, `unlock-and-remove`, plus unambiguous
future-proofs `discard`, `overwrite`, `hard`. `showModal` adds class
`danger` to matching option buttons; CSS colors them like the ctx-menu
danger rows (`#f27a6a`). Covers server decisions and local confirms alike
(they share the modal).

### 4. Persist sidebar state (client, localStorage)

- Whole-sidebar visibility (`b` key): key `gg.sidebar.hidden` (`"1"` when
  hidden). Applied at startup to `state.sidebar` + the `nosb` class.
- Per-section collapse (dblclick on header): key `gg.sidebar.collapsed`,
  a JSON array of section names (`branches`/`worktrees`/`tags`/`stashes`).
  `toggleSection` writes it; startup re-applies via the same
  `toggleSection` logic so header arrows stay in sync. The collapsed class
  lives on the persistent `<ul>` containers, so a one-time boot restore
  survives every re-render.
- localStorage reads/writes wrapped in try/catch (private-mode safety);
  failure degrades to today's non-persistent behavior.

### 5. Minors (all reviewed-and-deferred pickups)

- **Stash ref+sha match** (server, `ophttp.go`): `opStartRequest` gains
  `Sha string`. In the stash-apply/pop/drop allowlist loop, when
  `req.Sha != ""`, resolve `svc.StashCommit(ctx, e.Ref)` and 409
  `stash list changed — refresh` on mismatch. A resolve **error** does not
  block (best-effort guard, the row may legitimately be sha-less). Client
  sends `sha: st.sha` from the stash menu actions.
- **Detail-open generation counter** (client): `state.detailGen`;
  `openCommitByHash`, `openStashDetail`, and `openCommit` capture
  `const gen = ++state.detailGen` at entry and bail after each `await` if
  `gen !== state.detailGen`. `drillOut` also bumps it so an in-flight open
  cannot resurrect a closed detail view.
- **Diff-nav on notice paths** (client): the five direct
  `$("diff-body").innerHTML = <notice>` writes (loading ×2, error ×2,
  conflicted) are followed by `updateDiffNav()` so the change arrows
  disable when no diff table is present.

## Testing

Go (`internal/web`, real-git fixtures per existing op tests):
- `decide` publishes `resolved`: after answering a parked decision, the
  run's history contains `decision` then `resolved` (assert via a fresh
  `subscribe` replay).
- Stash sha guard: matching sha dispatches; mismatched sha → 409 with
  `stash list changed` in the body; empty sha keeps today's behavior.

Playwright (scratch repos in `/tmp`, per the established shoot*.js loop):
- Two tabs on the same parked decision; answering in tab A closes tab B's
  modal (exercises `resolved` live fan-out).
- Server killed while a decision is parked: the client gives up cleanly —
  op line shows the lost-connection error, pull/push re-enabled, modal
  closed, a subsequent op can start (after server restart).
- Danger styling: a parked `push-force` (or local delete-tag confirm)
  renders its destructive buttons red (computed style).
- Sidebar persistence: collapse a section + hide the sidebar, reload,
  both restored.

## Out of scope

Engine/TUI changes (the TUI's own `^3` stash blind spot stays a separate
candidate); event ids / incremental replay; op status polling endpoint;
multi-op concurrency.

## Docs

CHANGELOG entry; CLAUDE.md `web` package-map row updated (resolved event,
onerror recovery, danger modal styling, sidebar persistence, sha guard).
