# Web push — design

Date: 2026-07-23 · Branch: `feat/web-push` (off `web-dev`) · Status: approved

## Goal

Add **push** to the web client: a header button (and `P` key) that pushes the
current branch to `origin` over the existing op transport, with the engine's
full rejection recovery rendered by the existing decision modal.

## Key finding that set the scope

`engine.Push` (`internal/engine/ops_basic.go`) already contains the complete
smart-push recovery — the web gets it for free through the generic transport:

- Plain push, success → `Done{Changed:true}`, summary `pushed`.
- Non-fast-forward rejection → parks **`push-rejected`**
  (`rebase`/`force`/`abort`; when the pushed branch is not the current branch,
  `force`/`abort` only). `rebase` = `pull --rebase` + one re-push, with a
  conflict forking the existing **`rebase-conflict`** (`keep-conflicts`/`abort`)
  the pull modal already renders. `force` chains **`push-force`**
  (`force-with-lease`/`force`/`abort`, lease first).
- Credentials / hook / network failures surface unchanged as op errors.

So the originally sketched "plain push + clear rejected error, recovery later"
is dropped: suppressing recovery would be *more* code for less behavior.
Decision: wire `engine.Push` as-is (user-approved 2026-07-23).

## Server (`internal/web/ophttp.go`)

New `case "push"` in `handleOpStart`:

1. Resolve the branch server-side: `svc.CurrentBranch(ctx)`. Empty branch
   (detached HEAD) → HTTP 409 with a clear message
   (`"push: no current branch (detached HEAD?)"`); a read *error* → HTTP 500.
   An unborn branch is NOT detectable here (`symbolic-ref` resolves it), so it
   dispatches and surfaces git's own refspec error through the op — acceptable,
   a zero-commit repo isn't usable in the web UI anyway. Nothing client-sent
   ever reaches git argv.
2. Dispatch `engine.Push{Remote: "origin", Branch: cur, SetUpstream: true}` —
   byte-for-byte the TUI's `P` dispatch (`internal/tui/remote_tags.go`).
3. `Force` is **never** settable from the wire. The only route to a force push
   is choosing `force` in the parked `push-rejected` modal and then confirming
   in the chained `push-force` modal — two explicit user decisions.

No other transport changes: SSE events, replay buffer, decide route, and the
done-synthesized-from-Execute rule are untouched.

## Client (`internal/web/static/`)

- `index.html`: `⇫ push` button (`#push-btn`) in the header next to
  `#pull-btn`.
- `app.js`: `doPush()` → `startOp({op:"push"}, "pushing")`. **No pre-flight
  confirm** — the TUI pushes straight on `P`, push never rewrites the working
  tree, and the destructive lanes are already decision-gated in-op.
- Key: `P` (shift+p) in the keydown chain (`p` stays pull). Same guard order:
  modal → ctx-menu → form-field → chain.
- Button disabled while any op runs, re-enabled on done — the exact
  `#pull-btn` pattern (`startOp` disables, `handleOpEvent` done re-enables).
- `style.css`: reuse the pull button's class; no new styles unless the glyph
  needs width.

## Error handling

- Detached HEAD → 409 before an op starts; client shows the error on the op
  line (existing startOp error path).
- Rejection → the modal (not an error). Abort → `done{changed:false}`,
  summary `push cancelled`.
- Rebase-conflict `keep-conflicts` → op ends `changed:true` **with** an error
  (the SmartMerge convention); the client's done handler refreshes status,
  which shows the conflicted working tree — same behavior the pull increment
  already established.
- Credentials/network → op error event, shown on the op line verbatim.

## Testing (`internal/web/oppush_test.go`)

Reuse `oppull_test.go`'s `cloneWithOrigin` / `pushRemoteCommit` fixtures.
Scenarios:

1. **Clean push**: local commit ahead → `op:"push"` → done `changed:true`;
   origin's `main` tip equals the clone's tip.
2. **Rejected → abort**: `pushRemoteCommit` moves origin + local commit →
   SSE surfaces `push-rejected` → decide `abort` → done `changed:false`;
   origin tip unchanged.
3. **Rejected → rebase → re-push**: same divergence (non-conflicting files) →
   decide `rebase` → done `changed:true`; origin history contains both
   commits, remote commit first.
4. **Rejected → force → force (plain)**: decide `force`, then `force` →
   done `changed:true`; origin tip is the local commit (remote commit gone).
   (`force-with-lease` would *refuse* here — a failed push never updates the
   remote-tracking ref, so the lease is stale until a fetch; that refusal is
   the lease working as designed, not a gg bug.)
5. **Rejected → force → force-with-lease refused**: decide `force`, then
   `force-with-lease` → op ends in an error mentioning stale info; origin tip
   unchanged — pins the safety property.
6. **Detached HEAD**: `git checkout --detach` → POST `op:"push"` → 409, no op
   started.

## Out of scope (deliberate)

- Tip-tags push check (`pushTagCheckCmd`) — TUI-only nicety for now.
- Destructive-option styling in the modal — transport-hardening stage.
- `pusherr`-based friendlier error text — the raw git message is acceptable.
- Pushing a non-current branch from the branches sidebar — a later menu row.
