# Tag pushed-state indicator — design

**Date:** 2026-06-28
**Status:** approved (pending spec review)

## Problem

The Tags panel shows local tags only. A user cannot tell whether a given tag
has been pushed to the remote. There is no visual cue, and no way to ask.

## Root constraint

gg currently has **no knowledge of remote tags**. Local tags (`refs/tags`) are
cached for free by git, but — unlike remote *branches* (which live in
`refs/remotes` after any fetch) — there is no local record of which tags exist
on a remote. Determining "pushed or not" requires a **network call**
(`git ls-remote --tags <remote>`).

Everything below follows from that: the marker is trivial; the design work is
about *when* we pay for the network lookup and how we keep that cost opt-in and
non-disruptive.

## User-facing behavior

- A tag known to exist on the remote renders a trailing `▲` in the Tags panel,
  mirroring the Commits panel's local/remote tip convention (`■`/`▲`).
- A tag that is local-only — **or that has not been checked yet this session** —
  renders no marker. The two are deliberately indistinguishable (chosen
  tradeoff: simplest cue, no false "local-only" claim before a lookup runs).
- The `●`/`○` annotated/lightweight kind glyph (existing) is unchanged; `▲` is
  an additional trailing marker.

Example panel rows:

```
● v1.2.0  a1b3c9d  release   ▲     (on remote)
● v1.4.0  9c4d2e1  wip             (local-only OR not yet checked)
```

## Driving the lookup — two paths, one command

Both paths converge on a single TUI command (`remoteTagsCmd`) that calls one
domain query (`RemoteTags`) and produces one message (`remoteTagsMsg`) whose
handler replaces the stored set.

1. **Manual** — a **Tags `.`-menu action "Refresh remote status"**. Runs the
   lookup once, annotates every visible tag. Chosen over a global keypress
   because the global single-letter keyspace is saturated (`u` is
   `UndoLastCommit`) and because every other tag operation (push, delete from
   remote, annotate, solo) is already exposed via the `.` menu — this matches
   the established pattern and naturally operates on the whole list rather than
   one row. A manual failure (e.g. offline) shows on the status line.

2. **Scheduler** — an opt-in `[refresh] remote_tags` interval (seconds, default
   `0` = off), driven by the existing single-lane background refresh queue,
   exactly like the `fetch` row. Background failures are **silent** (see Failure
   seam below).

A lookup **replaces** the whole set (it is a snapshot of the remote at that
instant), so a tag deleted on the remote loses its `▲` on the next refresh.

## Architecture

### Why synthetic, not a `sourceKey`

Remote-tags is modeled as a **synthetic `refreshItem{isRemoteTags: true}`**
(mirroring `fetchItem`/`isFetch`), **not** a new `sourceKey`.

Reason: `reloadAllCmd`/`reloadSourcesCmd` sweep every source `0..srcCount`, so
*any* real `sourceKey` is fired by both the app-start fan-out and manual `r`.
A network call must never be triggered unsolicited by startup or `r`. `fetch`
is kept synthetic for exactly this reason, and there is no existing precedent
for a `sourceKey` that reload-all skips (`srcIdentity` is *in* the sweep).
Following the `fetch` precedent keeps the network call off the automatic
all-source paths without inventing a fragile exclusion every future reload
helper must remember.

### Layers

**`internal/git`** — new verb `RemoteTags(ctx, remote string) (map[string]bool, error)`:
one invocation of `git ls-remote --tags <remote>`. Parse each line's
`refs/tags/<name>` ref, returning the set of bare tag names. Drop `^{}` peeled
rows (annotated-tag dereferences). One verb = one git invocation, per
convention. A dedicated parser (`ParseRemoteTags`) is unit-tested against
`ls-remote` output fixtures.

**`internal/domain`** —
- New `queryQuiet[T]` helper: identical to `query` (Read reservation +
  singleflight coalescing) **except it does not call `observ.NoteFailure`**.
- New `RemoteTags(ctx) (map[string]bool, error)`: resolves the remote itself
  (calls `RemoteNames`; picks `origin` if present, else the first remote;
  returns an empty set + nil error if there are no remotes), then calls the git
  verb under `queryQuiet`. Remote resolution lives here, not in the TUI, so the
  frontend never threads a remote name in and the two-invocation detail does not
  leak.

**`internal/tui`** —
- New model field `remoteTagNames map[string]bool` (nil until first lookup).
- `tagRows()` appends `" ▲"` when `m.remoteTagNames[t.Name]`.
- `remoteTagsCmd(ctx, manual bool)` → `remoteTagsMsg{names, err, manual}`.
  Handler stores the set; on manual error sets the status line; background
  completion clears the bg lane like other synthetic items.
- Tags `.`-menu action "Refresh remote status" → `remoteTagsCmd(ctx, true)`.
- Scheduler: `refreshItem{isRemoteTags: true}` added to `scheduledItems`, with
  cases in `refreshIntervalFor`, `scheduledInterval`, `refreshTomlKey`
  (→ `"remote_tags"`), and `setRefreshIntervalField`. The lane runs it via
  `remoteTagsCmd(ctx, false)`.

**`internal/config`** —
- `RefreshConfig.RemoteTags int` (`toml:"remote_tags"`), default `0`.
- `overlayRefresh` copies it; `Defaults` leaves it `0`.
- A `settingDoc` `{"refresh", "remote_tags", 0, "seconds between background
  remote-tag (ls-remote) lookups; 0 = off"}` in `template.go` — this single
  registry auto-feeds both `gg config init` and `gg config populate`.
- The existing `min_seconds` floor applies via the unchanged `scheduledInterval`
  logic.

### Optimistic updates (symmetric)

To keep the marker honest between lookups:
- After a successful **Push tag** (`tagPushRow` op completion): **add** the tag
  name to `remoteTagNames` → `▲` appears immediately.
- After a successful **Delete tag from remote** (`tagDeleteRemoteRow` op
  completion): **remove** the tag name → `▲` disappears immediately.

### Failure seam (critical)

Per the domain failure seam, `query()` records every non-cancel error to the
always-on `errors.log` + session ring. An opt-in background poll while offline
would otherwise append to `errors.log` every interval — the exact flood the
seam exists to avoid. Therefore `RemoteTags` uses `queryQuiet` (no
`NoteFailure`). The error is still *returned*, so the manual path can surface it
on the status line; the background path discards it silently.

## Which remote

Check `origin` if it exists, else the first configured remote. A tag pushed
only to a non-default remote shows blank. Documented limitation; checking every
remote would be N network calls.

## Comparison semantics (v1)

A tag is "on the remote" iff a tag of the **same name** exists on the chosen
remote. Hash-mismatch detection ("pushed, but the remote tag points at a
different commit") is **out of scope** for v1.

## Out of scope (v1)

- Hash-mismatch detection.
- Per-remote breakdown / multi-remote aggregation.
- CLI surface (no new `gg` subcommand → no `agentskill` version bump).

## Testing strategy

- `internal/git`: `ParseRemoteTags` unit tests (peeled-row dropping, empty
  output); `RemoteTags` verb against a real two-repo push fixture or FakeRunner
  argv assertion.
- `internal/domain`: `RemoteTags` resolves origin-or-first; empty-remotes →
  empty set, nil error; `queryQuiet` does not record a failure (assert via the
  observ failure ring).
- `internal/tui`: `tagRows` renders `▲` only for names in the set; manual-action
  wiring stores the set and reports on error; optimistic add/remove on
  push/delete completion; scheduler `refreshTomlKey`/interval cases.
- `internal/config`: overlay + settingDoc coverage (existing
  `TestSettingDocsCoverAllFields` guards the new field).
