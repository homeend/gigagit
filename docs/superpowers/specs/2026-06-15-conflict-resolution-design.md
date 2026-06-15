# Conflict resolution (whole-file) — design

**Date:** 2026-06-15
**Status:** draft (brainstorm)
**Roadmap:** F4 (conflict resolution), first slice — whole-file only.

## Goal

Tell the user, in the status bar, when the repository is in a conflicted state,
and let them press a key to open a popup that resolves each conflicted file at
the **whole-file** level (keep a side / delete / keep base / mark resolved), and
— when a merge or rebase is in progress — continue or abort it.

Partial (hunk/line-level) conflict editing is explicitly a **later** feature;
v1 never edits file contents.

## Why detection is status-driven, not op-driven

The conflict that motivated this (test repo `test-1`) came from a **stash-pop**
(`gg-autostash:zzz`): the index holds unmerged stages (base + theirs, no ours →
"deleted by us") but there is **no `MERGE_HEAD`** and no rebase dir. So gg's
existing `MergeInProgress`/`RebaseInProgress` probes do not see it.

**Conflict presence is therefore derived purely from `git status`** — any file
with `Kind == KindUnmerged`. The in-progress-op probes are used only to decide
whether to offer *continue/abort* (step 4), never to decide whether conflicts
exist.

## What exists today

- `model.FileStatus{Path, Staged, Unstaged byte, Kind}`; the porcelain-v2 parser
  (`internal/git/status_parse.go`) tags unmerged entries `Kind: KindUnmerged`
  but **drops the unmerged XY code** (the `u <XY>` field), so the conflict
  *type* is currently unknown to the model.
- Staging (`space`) and the stash popup already no-op on `KindUnmerged`
  ("resolve conflicts first").
- `MergeInProgress`/`MergeAbort` and `RebaseInProgress`/`RebaseAbort` verbs
  exist (per-dir); there is **no** `merge --continue`/`rebase --continue`.
- The decision modal, the centered-popup pattern (`worktreePopup`/the stash
  popups), and `domain.Execute` write path are all available to reuse.

## 1 — Capture the conflict type (parser)

Extend the `u` branch of the parser to record the unmerged XY code into
`Staged`/`Unstaged`, exactly like the ordinary changed-file branch:

```
u <XY> <sub> <m1> <m2> <m3> <mW> <h1> <h2> <h3> <path>
```

`FileStatus{Path, Kind: KindUnmerged, Staged: XY[0], Unstaged: XY[1]}`. The
relevant codes:

| Code | Meaning | "present" side |
|------|---------|----------------|
| `UU` | both modified | both (stage 2 + 3) |
| `AA` | both added | both |
| `DU` | deleted by us, modified by them | theirs (stage 3) |
| `UD` | modified by us, deleted by them | ours (stage 2) |
| `AU` | added by us, (theirs absent) | ours |
| `UA` | (ours absent), added by them | theirs |
| `DD` | both deleted | neither |

A pure classifier `model.ConflictKind(f FileStatus) conflictClass` collapses
these into the two action sets the popup offers:
- **bothSides** (`UU`,`AA`): ours / theirs / mark-resolved.
- **modifyDelete** (`DU`,`UD`,`AU`,`UA`,`DD`): keep-modified / delete /
  keep-base.

`WorkingTreeStatus.Conflicts() []FileStatus` returns the unmerged files; the
existing `Counts().Conflicted` already counts them.

## 2 — Status-bar notification

When `len(status.Conflicts()) > 0`, the status line shows a persistent,
high-visibility prefix (ahead of any transient `statusMsg`):

> `⚠ 1 conflict — press [x] to resolve`

Pluralized; cleared automatically once the conflict count reaches zero (the
op-finished reload recomputes it). `x` is the open key (free in the dispatch;
confirm at review).

## 3 — The resolution popup (`x`)

A centered `conflictPopup` (pointer field `m.conflictPopup`), modeled on the
stash popups:

```go
type conflictPopup struct {
	files []model.FileStatus // the conflicted files, refreshed after each action
	sel   int                // selected file
}
```

- **List:** each row shows the path + a plain-language type
  (`both modified`, `modified vs deleted`, `added by both`, `both deleted`),
  newest-first is irrelevant — git's path order.
- **Per-file actions** (context-aware on the selected file's class), shown as a
  hint line and bound to keys:

  | Class | Keys |
  |-------|------|
  | bothSides | `[o]` keep ours · `[t]` keep theirs · `[m]` mark resolved |
  | modifyDelete | `[k]` keep modified · `[d]` delete the file · `[b]` keep base |

  Matching the GitKraken modal for `timing3.log` (Keep Modified / Delete /
  Keep Base).
- **Global keys:** `↑↓`/`j`/`k` move; `[A]` mark **all** resolved; `esc` close.
  When a merge/rebase is in progress: `[c]` continue (enabled only when zero
  conflicts remain), `[a]` abort.
- Each action dispatches an op (step 5), then refreshes the conflict list from a
  fresh `Status`; the resolved file drops off. When the list empties:
  - op in progress → popup shows "all resolved — [c] continue / [a] abort".
  - no op → popup shows "all resolved — commit with `c`" and closing returns to
    the normal flow (the resolved changes are staged, ready for `c`).

## 4 — Action → git semantics

Resolution marks the file resolved by staging it (`git add`) or removing it
(`git rm`), which is what clears the unmerged index entry:

| Action | Class | git |
|--------|-------|-----|
| keep ours | bothSides | `checkout --ours -- <p>` then `add -- <p>` |
| keep theirs | bothSides | `checkout --theirs -- <p>` then `add -- <p>` |
| mark resolved | bothSides | `add -- <p>` (user edited it by hand outside gg) |
| keep modified | modifyDelete | check out the **present** side (`--ours` for `UD`/`AU`, `--theirs` for `DU`/`UA`) then `add -- <p>` |
| delete the file | modifyDelete | `rm -- <p>` |
| keep base | modifyDelete | `git checkout :1:<p>` (stage-1 blob) then `add -- <p>`; if no stage 1 (e.g. `AA`/`DD` has none) the key is hidden |
| mark all resolved | any | `add -- <all conflicted paths>` (assumes the user resolved markers by hand) |
| continue | — | `merge --continue` or `rebase --continue` (whichever is in progress) |
| abort | — | `merge --abort` or `rebase --abort` |

"Keep modified" picks the side that actually has content, derived from the
conflict code (so we never run `--ours` on a file with no stage 2).

## 5 — Engine & verbs

New git verbs (`internal/git/conflict.go`):
- `CheckoutSide(ctx, path, side)` — `git checkout --ours|--theirs -- <path>`.
- `CheckoutBase(ctx, path)` — `git checkout :1:<path>` (stage-1 content).
- `RemoveFile(ctx, path)` — `git rm -- <path>`.
- Reuse `StagePaths` for `add` (mark resolved).
- `MergeContinue(ctx, dir)` / `RebaseContinue(ctx, dir)` (mirror the existing
  abort verbs). Continue uses `--no-edit` for merge.

Engine op `engine.ResolveConflict{Path string, Action conflictAction}` (a small
enum: `keepOurs`/`keepTheirs`/`markResolved`/`keepModified`/`delete`/`keepBase`)
runs the right verb sequence under a **TreeWrite** reservation via
`domain.Execute`, `Summary` e.g. `resolved <path> (kept theirs)`. Plus
`engine.MarkAllResolved{Paths}`, `engine.MergeOrRebaseContinue{}`,
`engine.MergeOrRebaseAbort{}` (these last two detect which op is in progress and
dispatch accordingly; abort also works for a pure `--abort` when applicable).
Add the new verbs to the `engine.GitOps` interface.

## 6 — TUI wiring

- `x` in the normal dispatch, gated by `m.status` having conflicts, opens the
  popup. Routed in the popup chain (before the stash handlers).
- The status-bar prefix in `view.go`'s status-line assembly.
- Footer: a `[x] resolve` binding gated on conflicts; help.go a "Conflicts (x)"
  section.
- After each `ResolveConflict`/continue/abort op, the standard full reload runs
  (status + commits change), and the popup re-reads `status.Conflicts()`; the
  popup closes automatically when an abort/continue ends the conflicted state.

## Out of scope (later)

- **Partial / hunk / line-level** conflict resolution and any in-TUI or
  `$EDITOR`/mergetool content editing — the explicit next feature.
- Three-way visual merge view.
- A `gg resolve` CLI surface (v1 is TUI-only; add later for scriptability/e2e,
  the same gap the stash CLI just closed).
- Conflict resolution inside a *linked worktree* other than the current one
  (v1 resolves the current working tree).

## Testing

- `internal/git`: parser test — a `u DU`/`u UU`/`u AA` line yields
  `KindUnmerged` with the right `Staged`/`Unstaged`; argv tests for
  `CheckoutSide`/`CheckoutBase`/`RemoveFile`/`MergeContinue`/`RebaseContinue`;
  a real-repo modify/delete + both-modified conflict resolved each way, asserting
  the porcelain state clears.
- `internal/model`: `ConflictKind` classifier table; `Conflicts()`.
- `internal/engine`: `ResolveConflict` each action against a real conflicted
  repo; continue/abort against an in-progress merge and rebase.
- `internal/tui`: the status-bar prefix appears with conflicts; `x` opens the
  popup with the right per-file action set; `o`/`t`/`k`/`d`/`b` dispatch the
  right op and the file leaves the list on refresh; `[c]` is disabled until zero
  conflicts remain; no-op when there are no conflicts.

## Files touched

| File | Change |
|------|--------|
| `internal/git/status_parse.go` (+test) | capture unmerged XY |
| `internal/git/conflict.go` (+test) | checkout-side/base, rm, continue verbs |
| `internal/model/model.go` (+test) | `ConflictKind`, `Conflicts()` |
| `internal/engine/gitops.go` | new verbs on the interface |
| `internal/engine/conflict.go` (+test) | `ResolveConflict`, continue/abort ops |
| `internal/tui/conflict_popup.go` (+test) | the popup + actions |
| `internal/tui/model.go`, `view.go`, `footer.go`, `help.go` | `x` dispatch, status-bar notice, routing, hints |
| `CHANGELOG.md`, `README.md` | entries |

## Open decisions to confirm at review

1. Open key — `x` (recommended), vs `!`/`X`.
2. Whether "mark all resolved" should refuse files that still contain conflict
   markers (safer) or stage unconditionally like GitKraken (simpler). Recommend
   simpler for v1.
3. Decomposition for the plan: (a) parser + model classifier; (b) verbs + ops;
   (c) the popup + status-bar notice + continue/abort.
