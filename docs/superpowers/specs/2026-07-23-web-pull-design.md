# Web: SmartPull on the transport — Design

**Date:** 2026-07-23 · **Branch:** `feat/web-pull` off `web-dev` (merges back
into web-dev; main only when the user says merge).

## Goal

The hero op on the web: `engine.SmartPull` — its `non-fast-forward` fork
(rebase / merge / reset / abort) is the first **live, parking** decision the
browser modal renders in production.

## A. Server (one switch case in `internal/web/ophttp.go`)

- `op:"pull"` → `engine.SmartPull{}` — zero params: current branch, its
  configured remote (`RemoteForBranch`), `PullAndStay`. No new fields on
  `opStartRequest`, no new routes; the transport (SSE, decide, writeGuard,
  feed reset on `changed`) is untouched.
- Engine semantics the web inherits verbatim (verified in source):
  - fast-forwardable → `done{ok:true, changed:true, summary:"pulled <b>"}`.
  - diverged → the op PARKS on `non-fast-forward` (options
    `rebase|merge|reset|abort`; the prompt itself carries the reset
    warning). `abort` → `done{ok:true, changed:false,
    summary:"aborted: <b> diverged"}`. The other three snapshot a branch
    version, act, and return `changed:true`.
  - a rebase/merge that hits conflicts → `done{ok:false}` (error), with the
    conflicts visible in `/api/status` (the status pane's Conflicts section
    and the WT row pick them up on the client's existing refresh paths).
  - no upstream/remote → `done{ok:false}` with the engine error. Detached
    HEAD → `done{ok:false, error:"smart pull: no target branch…"}`.

## B. SPA

- A `⟳ pull` button in the top header (`#top`, after `#repo-branch`) +
  the **`p`** key (TUI parity) — both call `startOp({op:"pull"}, "pulling")`
  (the `if (state.op) return` guard already debounces). No new modal code:
  the existing decision modal renders the fork; esc = `abort` (present in
  the option list); done-closes-modal covers the notify-only decisions
  (`worktree-pull-failed`, `stash-pop-conflict`).
- Button dims while an op is live (same rendering hook as `commit-btn` is
  NOT available in the header — instead toggle a `disabled` attribute in
  `startOp`/`handleOpEvent.done`, the two places `state.op` flips).

## C. Testing (Go, real git; file remotes — no network)

Fixture helper `cloneWithOrigin(t)`: bare origin in a `t.TempDir()`, a clone
with `origin/main` upstream (git clone wires it), returning (originDir,
cloneDir). Scenarios:
1. **Fast-forward:** commit in a second clone → push → pull from the first →
   SSE ends `done{ok:true,changed:true}`, summary `pulled main`, local tip ==
   origin tip (git-verified).
2. **Diverged + rebase over HTTP:** local commit + remote commit → POST pull
   → SSE yields `decision{id:"non-fast-forward", options:[rebase,merge,
   reset,abort]}` and PARKS → `POST decide "rebase"` → `done{ok:true,
   changed:true}`, `git log` linear (local commit atop remote's).
3. **Abort:** same divergence → decide `abort` → `done{ok:true,
   changed:false}`, summary contains `aborted`, tips unchanged.
4. **Conflicted rebase:** divergent edits to the same file → decide
   `rebase` → `done{ok:false}`; `/api/status` reports the file conflicted.
5. **No remote:** repo without origin → `done{ok:false}` (error present).

Playwright (scratch clone pair under /tmp): click `⟳ pull` on a diverged
clone → screenshot the LIVE parked modal (the first production one) → click
`rebase` → op line shows the summary. Read the PNGs.

## D. Out of scope

Pulling a non-current branch (background pull lane + its
`not-fast-forwardable` fork), choosing the remote, fetch-only, push,
conflict RESOLUTION on the web (visible in status only), auto-pull.
