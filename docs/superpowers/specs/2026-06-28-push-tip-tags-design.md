# Push branch-tip tags with the branch (P) — design

**Date:** 2026-06-28
**Status:** approved (verify-with-5s-timeout chosen)

## Problem

Pressing `P` pushes the current branch (`engine.Push{Remote:"origin", Branch,
SetUpstream}`) but never offers to push tags. A user who tagged the tip commit
(e.g. a release tag) routinely forgets to push the tag — it stays local.

## Behavior

When the user presses `P` and the **current branch's tip commit** carries one or
more local tags that are **not on the remote**, prompt:

> Branch tip has tag `v1.0.0` not on the remote. Push it too?
> `[Push branch + tag]`  `[Push branch only]`  `[Cancel]`

- Default highlight: **Push branch + tag** (the helpful nudge). `Cancel` never
  traps.
- Multiple unpushed tip tags → list them (truncated if many); push all.
- No unpushed tip tags → `P` behaves exactly as today (no modal, immediate push).

## Determining "unpushed" — verify with a 5s budget

The check runs ONLY when the tip has local tags (the common no-tag case stays
instant, no network):

1. Compute the tip commit hash = `branch.Hash` for the branch matching
   `m.status.Branch` (the commit feed is scope-filtered, so `m.commits[0]` is not
   reliable; `model.Branch.Hash` is).
2. Tip tags = `m.tags` where `Target == tipHash`. If none → push immediately.
3. Otherwise run a **fresh `git ls-remote --tags`** with a **5-second timeout**.
   - Completes in time → unpushed = tip tags whose name is NOT in the remote set
     → if any, show the modal; else push directly.
   - Times out (>5s) or errors → **skip the tag check**, push the branch as today
     (never hang `P`).

The fresh lookup uses a new **non-coalescing** domain method
`RemoteTagsFresh(ctx)` so the caller's 5s-timeout context fully governs it
(the existing `RemoteTags` coalesces via singleflight onto the background
auto-refresh lookup, whose leader a follower's context cannot cancel — which
would defeat the 5s guarantee). `RemoteTagsFresh` still runs under a Read
reservation and does NOT record failures (a timeout/offline must not spam
`errors.log`). Its result also refreshes the in-memory `m.remoteTagNames` cache
(so `▲` markers benefit).

A generation counter (`pushCheckGen`) drops a late check result if the user has
moved on (started another op, switched repo, pressed `P` again).

## Execution — push branch, then chain the tags

Chosen over a single combined push so the existing branch push-rejection
recovery (`internal/pusherr`, rebase/force/abort) stays branch-only and clean:

1. The modal's "Push branch + tag" sets `m.pendingPushTags = <unpushed tip tags>`
   and starts the normal `engine.Push` (unchanged — keeps rejection recovery).
2. On the branch push's success (`opFinishedMsg`), if `pendingPushTags` is set,
   chain a new `engine.PushTags{Remote:"origin", Names}` op — one
   `git push origin refs/tags/a refs/tags/b` (a single invocation for all tags).
3. On the chained `PushTags` success, **optimistically** add those names to
   `m.remoteTagNames` (so `▲` updates and a later `P` won't re-prompt).
4. `pendingPushTags` is cleared on the branch push's error path (a failed/cancelled
   branch push must not later trigger a tag push).

## New surfaces

- `engine.PushTags{Remote string; Names []string}` + `git.Repo.PushTags(ctx,
  remote, names []string)` — one `git push <remote> refs/tags/<n>…` invocation.
  (The existing single-tag `engine.PushTag` / `.`-menu action is unchanged.)
- `domain.RemoteTagsFresh(ctx) (map[string]bool, error)` — non-coalescing,
  no-failure-recording remote-tag lookup, governed by the caller's context.
- TUI: `pendingPushTags []string`, `pushCheckGen int`, an async `pushTagCheckMsg`,
  the modal, and the `opFinishedMsg` chain.

## Out of scope (v1)

- Tags NOT at the branch tip (only the tip commit's tags are considered, per the
  request).
- Branch `.`-menu push and tag `.`-menu push (only the `P` current-branch push).
- A config toggle (the prompt is a low-cost, helpful nudge; always on. The 5s
  check only fires when the tip has tags).
- Pushing to a non-default remote (uses `origin`, like the existing `P`).

## Testing

- `engine.PushTags`: argv assertion via FakeRunner (`git push origin
  refs/tags/a refs/tags/b`); empty Names is a no-op (no push); real-git
  two-repo push of multiple tags.
- `domain.RemoteTagsFresh`: returns the remote set; honors a cancelled/expired
  context (returns promptly, records no failure); no-remote → empty set.
- TUI: `unpushedTipTags` helper (filter by tip hash + remote set); the `P` flow —
  no tip tags → straight push; tip tags all pushed → straight push; unpushed tip
  tag → modal; modal "branch+tag" sets pendingPushTags and pushes; timeout →
  straight push; stale `pushCheckGen` result dropped; chain pushes tags on branch
  success; optimistic add; clears on branch-push error.
