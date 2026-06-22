# Reflog window — design

**Date:** 2026-06-22
**Status:** Approved (brainstorm)
**Feature:** A read-only HEAD-reflog viewer surfaced as a bottom-left TUI tab
sharing the Staged slot.

## Goal

Give users a way to inspect and recover recent work directly inside `gg` by
listing the HEAD reflog. lazygit has a real reflog tab (toggle `[`/`]` inside
the Commits panel, `space` to reset); GitKraken has no visual reflog view and a
long-standing open feature request for one. We follow lazygit's model, scoped
read-only for v1.

## Scope (decided in brainstorm)

- **Read-only viewer first.** No reset/checkout/recover actions in v1.
- **HEAD reflog only** (`git reflog`, i.e. `HEAD@{n}` entries) — every action:
  commit, checkout, reset, rebase, merge.
- **Row action = Both:** `enter`/`l` opens the entry's commit in the existing
  commit files-view; the `.` menu offers read-only actions.

## Placement & navigation

- New panel constant `panelReflog`, appended **before** `panelCount` so existing
  iota values do not shift.
- The bottom-left slot (currently the Staged panel) becomes a tab group:
  `bottomTabs = []panel{panelStaged, panelReflog}`, mirroring the existing
  `filesTabs = []panel{panelFiles, panelTags}` pattern.
- `ctrl+←/→` toggles Staged ⇄ Reflog when the bottom slot is focused, exactly
  like the middle slot's Files ⇄ Tags. The panel title shows the active tab and
  its row count.
- The active bottom tab is tracked by a new Model field
  `activeBottomTab panel` (zero value resolves to `panelStaged` via a
  `bottomTab()` helper, paralleling `middleTab()`).
- **Short terminal:** the whole bottom slot already drops at `bodyH < 12` in
  `leftColumnPanels`. Reflog lives in that slot, so it drops together with
  Staged — no special-casing.

## Data

### Git verb

`internal/git/reflog.go` gains:

```go
// ReflogEntries returns up to limit HEAD reflog entries, newest first.
func (r *Repo) ReflogEntries(ctx context.Context, limit int) ([]model.ReflogEntry, error)
```

Implementation: `git reflog --format=<fmt> -n <limit>` with a record format
carrying selector, full hash, short hash, subject, and relative date. Parsed
with a unit-stable separator (NUL-delimited fields, newline-delimited records)
to survive arbitrary subjects.

The verb runs against the active repo's worktree dir (the same `*git.Repo` the
rest of the TUI uses), so it tracks `reRoot` — HEAD reflog is **per-worktree**
and must follow the active worktree.

### Model type

`internal/model` gains:

```go
type ReflogEntry struct {
    Selector  string // "HEAD@{0}"
    Hash      string // full SHA
    ShortHash string // abbreviated SHA
    Subject   string // %gs, e.g. "commit: add foo" / "checkout: moving from main to dev"
    Rel       string // %gr, e.g. "2 hours ago"
}
```

### Domain query

`internal/domain` gains:

```go
func (s *Service) Reflog(ctx context.Context) ([]model.ReflogEntry, error)
```

Runs under a Read reservation (like `Branches`/`Worktrees`), bounded by config.

### Config

`internal/config` gains `[ui] reflog_limit` (`ReflogLimit int`, `<=0` = unset →
default 200), matching the existing `search_history_size` / `commit_graph_max_lanes`
field-level pattern.

### Refresh policy

The reflog changes on every HEAD-changing op, and showing what just happened is
the feature's entire point, so it must never go stale:

- Loaded as part of the startup `Snapshot` (new field on the snapshot struct),
  stored as `m.reflog []model.ReflogEntry`.
- Re-read on `opFinishedMsg` (the post-op full refresh path). It is a single
  cheap bounded file read, so refreshing after every op is acceptable and the
  simplest correct choice. No separate targeted-refresh machinery needed.

## Row interaction

All reflog interactions anchor on the **reflog row under the cursor**, resolved
through `displayIndices(panelReflog)` / `backingIndex` — never on `panelCommits`
selection state. (Display-vs-backing is this codebase's recurring bug class;
see wip-rows and commit-branch-column.)

### `enter` / `l`

Synthesize `model.Commit{Hash: e.Hash, Subject: e.Subject}` from the entry and
reuse the existing commit files-view open path (`loadCommitFilesCmd`,
`filesView`/`filesTitle`/`filesHash`). This shows the entry's commit vs its
parent in changed-files mode, with the `a` full-tree toggle and per-file diffs —
identical to opening a commit from the Commits panel.

Reflog commits may be **dangling** (not on any branch). The reflog keeps them
reachable, so `git`'s show/diff-tree resolves them. (Verify empirically during
implementation that `loadCommitFilesCmd` diffs a SHA not present in `m.commits`
and not on any ref.)

### `.` context menu (reflog-anchored)

Built fresh from the reflog cursor row — **not** via `appendCommitContextRows`
(which reads commit-panel state). Rows:

- **Copy SHA** — copy the entry's full hash (reuses the clipboard copy action).
- **Bookmark this commit** — reuses the path-less commit-bookmark path
  (commit `84d994d`).

### Compare (scope pin)

v1 compare = "bookmark the entry, then compare via the `g` switcher." Bringing
reflog rows into the `compareKeyEndpoint` / `Rank` mark system (mark an entry,
"Compare with marked") is **deferred** — that is the invasive path the wip-rows
feature showed bites hard, and bookmark-then-`g` already delivers the compare
without new wiring.

## Panel-enum sweep (touch list)

Adding `panelReflog` requires updating every panel-dispatch site. The plan must
visit each:

- `displayIndices(panel)` — reflog rows from `m.reflog`.
- `panelLen(panel)` — falls out of `displayIndices`.
- `focusOrder()` — include the active bottom tab.
- `leftColumnPanels()` — Reflog shares Staged's slot membership (present iff the
  bottom slot is present).
- Panel title rendering (around `view.go:402`, the `filesTabLabel` area) — a
  `bottomTabLabel` analog showing the active tab + count.
- `ctrl+←/→` handler in `model.go` — cycle `bottomTabs` when the bottom slot is
  focused.
- Body rendering for the bottom slot — render reflog rows when `activeBottomTab`
  is `panelReflog`.

## Out of scope (v1)

- Reset / checkout / create-branch from a reflog entry (read-only first).
- `gg reflog` CLI subcommand (`git reflog` already exists; the value here is the
  TUI surface).
- Paging / lazy windowing (reflog is bounded by `gc.reflogExpire`; one capped
  read is enough).
- Per-branch reflogs / branch picker (HEAD only).

## Testing

- `git` verb: real-repo test (`newRepo` + a few HEAD-moving ops) asserting
  parsed entries (selector, hash, subject) and limit bound; NUL parsing of a
  subject containing odd characters.
- domain: `Reflog` returns entries under the Read reservation.
- TUI: `ctrl+←/→` toggles Staged ⇄ Reflog; `enter`/`l` on a reflog row opens
  the files-view with the entry's hash (anchored on the cursor row, verified
  with a non-zero cursor); `.` menu lists reflog-anchored Copy SHA + Bookmark
  rows and nothing leaked from the commit menu; short-terminal drop; refresh
  after `opFinishedMsg`.
- config: `reflog_limit` overlay/default/clamp.

## Docs to update on completion

`CHANGELOG.md` (always); `README.md` (new TUI surface); `CLAUDE.md` package map
if the panel taxonomy description needs it. No `agentskill` bump (no CLI surface
change).
