# Multi-branch commit feed with branch labels (Phase 1) — design

**Date:** 2026-06-20
**Status:** approved (brainstorm)
**Scope:** `internal/git`, `internal/model`, `internal/domain`, `internal/tui`
(+ docs). No CLI/agentskill change. **Phase 1 of a 4-part GitKraken-style commit
graph effort** (later phases: graph lanes/topology, then branch-aware navigation
+ multi-branch "selected" mode + jump-to-divergence).

## Goal

The Commits panel shows commits from **all local branches by default**, in
**date order** (newest first), with **branch/HEAD labels** on the commits.
A branch can be **soloed** (show only its commits) and un-soloed — all via the
`.` context menu, with no global key.

## Background (what exists)

- `model.Commit{Hash, Parents, Author, Subject, UnixTime}` — already carries
  `Parents`, so the data is graph-ready.
- `git.Log(ctx, limit, skip)` runs `git log -n <limit> [--skip <skip>]
  --format=<logFormat>` from **HEAD only**, newest-first. `logFormat =
  "%H%x1f%P%x1f%an%x1f%at%x1f%s"`.
- `domain.CommitFeed` is the incrementally-paged, gen-tagged, dedupe-by-hash
  read-model for the panel; `svc.logPage(ctx, limit, skip)` →
  `repo.Log`, singleflight-coalesced by a `commits:<limit>:<skip>` key.
- TUI: `m.feed *domain.CommitFeed` (rebuilt in `New`/`reRoot`); `LoadInitial`
  drives the first paint (`load.go`), `commitRows()` renders rows; the `.`
  action menu is context-scoped per panel; `selectedBranch()` reads the
  Branches-panel selection.

## Decisions (locked in brainstorm)

1. **Default = all local branches**, date-ordered. (Today's HEAD-only walk is no
   longer the default.)
2. **Scope state = a list of branch names** `commitScopeBranches` (empty = all).
   Phase 1 holds 0 or 1 (solo or all); list-shaped so multi-select is a trivial
   later add.
3. **No global key.** Scope changes happen only through `.` menu actions.
4. **Multi-branch "selected" mode is deferred** (Phase 3).

## Design

### Git walk (`internal/git`)

Extend the log verb to take a **scope** and emit decorations:

```go
// LogScope selects which refs the walk covers. Empty Branches = all local
// branches (--branches); otherwise the listed branch names.
type LogScope struct {
    Branches []string // nil/empty → all local branches
}

func (r *Repo) LogScoped(ctx context.Context, limit, skip int, scope LogScope) ([]model.Commit, error)
```

Argv: `git log -n <limit> [--skip <skip>] --date-order --format=<logFormat>`
then `--branches` (empty scope) **or** the explicit `<branch>…` names (non-empty).
`logFormat` gains a trailing `%x1f%D` field (ref decorations). The existing
HEAD-only `Log` is replaced by `LogScoped` and its callers move over (the panel
is the only consumer). `--date-order` + `--skip` is deterministic for a fixed
repo state, so paging still composes exactly as today.

`ParseLog` parses the extra `%D` field into `[]model.Ref` (see below). The `%D`
value looks like `HEAD -> main, feature, tag: v1.0, origin/main`; split on `, `,
classify each token:
- `HEAD -> <name>` → the current branch: a `Ref{Name, Kind: RefHead}` plus the
  named local branch it points to.
- `HEAD` (detached) → `Ref{Name: "HEAD", Kind: RefHead}`.
- `tag: <name>` → `Ref{Name, Kind: RefTag}`.
- `<remote>/<name>` (matches a known remote prefix) → `Ref{Kind: RefRemote}`.
- otherwise → `Ref{Kind: RefLocal}`.

(Remote-prefix detection: a name containing `/` whose first segment is a remote
is remote; Phase 1 may simplify to "contains `/` ⇒ remote" since only local
labels render — exact remote-vs-slashy-branch disambiguation is deferred. Local
branches with `/` are rare; documented as a known Phase-1 simplification.)

### Model (`internal/model`)

```go
type RefKind int
const (
    RefLocal  RefKind = iota // local branch
    RefRemote                // remote-tracking branch
    RefTag
    RefHead                  // HEAD (current-branch marker)
)

type Ref struct {
    Name string
    Kind RefKind
}
```

`model.Commit` gains `Refs []Ref` (parsed from `%D`; nil when undecorated).

### Read-model (`internal/domain`)

- `CommitFeed` gains a `scope LogScope` field. `Service.logPage` becomes
  `logPage(ctx, limit, skip, scope LogScope)`; the singleflight key includes the
  scope (`commits:<branchesKey>:<limit>:<skip>`) so all-mode and solo-mode pages
  never collide.
- `CommitFeed.SetScope(scope LogScope)` sets the scope for subsequent loads;
  callers then `LoadInitial` to re-walk (gen bump drops stale pages). `LoadInitial`
  / `LoadMore` pass `f.scope` to `logPage`.
- `Service.CommitFeed()` starts with an empty scope (all branches).

### TUI (`internal/tui`)

- **State:** `commitScopeBranches []string` on Model (empty = all). A helper
  `commitScopeLabel()` → `"all"` or `"solo: <branch>"`.
- **Reload command:** `reloadFeedCmd()` calls `m.feed.SetScope(...)` then
  `LoadInitial`, returning the feed state via the existing message (mirrors the
  startup load); it bumps gen so a stale in-flight page is dropped.
- **Branch `.` menu actions** (Branches panel, on a selected branch `b`):
  - `commits-solo` **"Solo this branch"** — if `commitScopeBranches == [b]` set
    it to `nil` (un-solo) else set `[]string{b}`; then reload.
  - `commits-showall` **"Show all branches"** — present **only when**
    `len(commitScopeBranches) > 0`; sets `nil`; reload.
- **Commit `.` menu action** (Commits panel):
  - `commits-showall` **"Show all branches"** — present only when scoped; sets
    `nil`; reload.
- **Rendering:**
  - `commitRows()` appends branch label pills from `c.Refs`: local branches as
    `‹name›`, the HEAD branch emphasized (e.g. bracketed/▸ or styled). The labels
    join the single-sourced row string → filterable (`/`) and tooltip-revealed,
    consistent with the Branches-path precedent.
  - The Commits panel header shows the mode: `Commits (all)` /
    `Commits (solo: <branch>)`.
  - The Branches panel marks the soloed branch row (a `◉`/`(solo)` marker via the
    branch row builder; note the single-sourced-row gotcha — the marker becomes
    part of the filter haystack, acceptable).
- **Footer/help:** the two menu actions get help rows (menu-only actions need a
  help entry, not a footer key — mirror the existing menu-only copy actions).

## Testing

- **git:** `LogScoped` argv per scope — empty → `--branches --date-order`;
  `{Branches:["feat"]}` → `… --date-order feat` (FakeRunner). `ParseLog` parses
  `%D` into `Refs` incl. `HEAD -> main` (RefHead + RefLocal), `tag:`, remote.
- **domain:** `logPage` threads scope + distinct singleflight keys; `SetScope` +
  `LoadInitial` re-walks with the new refspec.
- **tui:** `commitRows` renders branch labels + HEAD emphasis; `commits-solo`
  sets/clears scope and triggers a reload; `commits-showall` is absent in all
  mode and present when scoped (both Branches and Commits menus);
  `commitScopeLabel`; the Branches solo marker.

## Docs

CHANGELOG (always); README (Commits panel now all-branches + solo via `.` menu,
branch labels); CLAUDE.md (Commits panel / CommitFeed scope note). No CLI surface
change → no agentskill bump.

## Out of scope (later phases)

- **Phase 2:** the visual graph — pure lane/topology engine (`internal/commitgraph`)
  + column rendering.
- **Phase 3:** branch-aware navigation (jump to a branch's divergence point,
  highlight a branch's commits) + the multi-branch **selected** set (toggle
  several branches into `commitScopeBranches`).
