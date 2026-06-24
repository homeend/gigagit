# Local vs remote branch-tip markers in the Commits graph

**Status:** approved (design)
**Date:** 2026-06-24

## Problem

In the Commits panel you cannot tell, at a glance, how a local branch stands
relative to its tracked remote. The branch name decorates its tip row, but
there is no visual cue that distinguishes "this is the local tip" from "this is
where the remote (origin) tip sits" — and when the local branch is *behind* its
remote, the remote tip is not even walked into the feed, so the divergence is
invisible.

The user wants a **purely visual** reading of the gap (no ahead/behind numbers):
distinct icons for the tip of a local branch and the tip of its tracked remote.
When both point at the same commit, that row shows both icons; when they have
diverged, each icon appears on its own commit row.

## Goal

For every commit row in the Commits graph, show up to two **tip markers** in a
fixed prefix on the identity column, immediately left of the branch name:

- **Local-branch tip** → `■` (filled square)
- **Tracked-remote tip** → `▲` (filled triangle), where "tracked" means the
  remote ref is the configured `Upstream` of some local branch (i.e. the remote
  branch has a local copy)
- **In sync** (local tip == remote tip, same commit) → both markers: `■▲`
- **Diverged** → each marker on its own commit row

Glyphs (`■` / `▲`) are tunable during implementation; the square/triangle
distinction is the contract.

### Row format

```
[graph]  ■▲ main      in sync (local tip == remote tip)
[graph]  ■  feat-x    local tip, ahead of / no remote
[graph]  │  …         lineage row (no marker, unchanged)
[graph]   ▲ main      remote tip on its own row (local is behind/diverged)
```

- Markers occupy a **fixed ≤2-cell prefix** (local slot, remote slot) followed by
  a separator space, so subjects stay column-aligned. Present markers
  left-pack into the prefix.
- The existing **dim-lineage name** (`c.Source` on non-tip rows) and the **`*`
  head prefix** are preserved. Head + in-sync reads `■▲ *main`.
- On a remote-tip-only row (behind case), the name shown is the **local branch
  name** (`main`), not `origin/main` — it reads as "this is where main's remote
  is."

## Non-goals

- No ahead/behind **numbers** anywhere (explicitly rejected by the user).
- No CLI surface — the CLI does not render the commit graph.
- The `commitgraph` lane engine and the lane `●`/`◇` glyphs are untouched.
- Remotes that are **not** the upstream of any local branch get no marker.

## Design

### Detection (no new git data for markers)

The marker logic joins data already in memory:

- `model.Commit.Refs` already carries `RefLocal` and `RefRemote` kinds
  (parsed from `git log %D`).
- `model.Branch` already carries `Upstream` (e.g. `origin/main`), loaded for all
  local branches and held in `m.branches`.

Per render, build the set `U = { b.Upstream : b ∈ m.branches, b.Upstream != "" }`.
For each commit:

- a `RefLocal` ⇒ local-tip marker `■`
- a `RefRemote` whose short name ∈ `U` ⇒ remote-tip marker `▲`
- both ⇒ `■▲`

Cost: building `U` is O(branches) once per render; the per-row check is O(refs on
that commit). Negligible.

### Stage A — markers (pure TUI)

Touches `internal/tui` only. Covers the **in-sync** and **local-ahead** cases
with **zero change to the feed** (in those cases the remote tip is already an
ancestor of, or equal to, the local tip and is therefore already walked in).

1. Extend `commitIdentOf` (`commit_ident.go`) — or add a sibling that the row
   builder calls — to compute the marker prefix. `commitIdentOf` currently
   inspects `RefLocal` only; it must now also inspect `RefRemote` against `U`.
   Since `U` is Model state, the marker computation needs access to the upstream
   set: pass it in (e.g. `commitIdentOf(c, upstreams)` or a small
   `Model.commitIdentOf(c)` method that closes over `m.branches`).
2. Emit a fixed-width marker prefix (`■`/`▲`/`■▲`/spaces) ahead of the label in
   the identity token. Account for the prefix in `commitIdentWidth` so subjects
   stay aligned and the column does not reflow as commits page in.
3. On a row that is a tracked-remote tip but **not** a local tip, derive the
   displayed name from the matching local branch (strip the remote prefix /
   look up the branch whose `Upstream` equals this ref) so the row reads `▲ main`.
4. Keep `*` head prefix and dim-lineage behavior intact.
5. `commitTextRevealAt` (tooltip text) should reflect the same markers/label so
   the reveal matches the row.

### Stage B — walk tracked upstreams into the feed

Makes the **behind / diverged** remote tip appear as its own row. Always on.

1. `git.LogScope` (`internal/git/log.go`) gains `Upstreams []string`. In
   `LogScoped`, append these refs to the walk argv (in addition to
   `--branches HEAD` or the named branches). De-duplicate.
2. Only include upstreams that **resolve** to an existing remote-tracking ref
   (an upstream can be configured but never fetched). Validate against the known
   remote-tracking refs (e.g. the `RemoteBranches` set) so `git log` is never
   handed a missing ref.
3. `domain.scopeKey` (`internal/domain/query.go`) must fold `Upstreams` into the
   cache / singleflight discriminator, so a scope that differs only by its
   upstream set is not coalesced with one that doesn't.
4. The TUI populates `LogScope.Upstreams` from `m.branches` upstreams (filtered
   per #2) whenever it sets/reloads the feed scope. When branches change (fetch
   updates tracking, ahead/behind, or a new upstream appears), the upstream set
   changes ⇒ the feed reloads (same mechanism that already reloads on scope
   change).
5. Stage B is opt-in at the consumer level: only the TUI populates `Upstreams`.
   The CLI and any other `CommitFeed` consumer leave it empty and are unaffected.

## Testing

- **Stage A (TUI, table-driven over `commitIdentOf`/row builder):**
  - local tip only ⇒ `■` + name
  - tracked-remote tip only ⇒ `▲` + local branch name
  - both at same commit ⇒ `■▲`
  - lineage row ⇒ no marker, dim name unchanged
  - head + in-sync ⇒ `■▲ *main`
  - untracked remote ref (not any branch's upstream) ⇒ no `▲`
  - identity-column width accounts for the marker prefix (subjects aligned)
- **Stage B (git + domain):**
  - `LogScoped` appends `Upstreams` to argv (FakeRunner argv assertion);
    de-dupes; omits non-resolving upstreams.
  - `scopeKey` differs when `Upstreams` differ.
  - Real-repo: local branch behind its remote ⇒ the remote tip commit is present
    in the walked feed and renders the `▲` marker on its own row.

## Open / deferred

- Exact glyphs may be tuned live (square/triangle distinction is fixed).
- Multi-remote: if a branch's upstream is on a non-`origin` remote it still works
  (matching is by the configured `Upstream` short name, not a hard-coded
  `origin/`).
