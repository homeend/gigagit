# F1 — Staging surface + index/commit verb foundation — design

**Date:** 2026-06-14
**Status:** draft (brainstorm)
**Roadmap:** `docs/superpowers/2026-06-14-feature-roadmap.md` (F1)

## Goal

Let the user stage and unstage changed files directly from the TUI's Status
panel with the `space` key, and lay the index-manipulation git-verb foundation
that F2 (commit/amend), F3 (hunk staging), and F4 (conflict resolution) extend.

## What already exists (so we build only the gap)

- `model.StatusFile{Path, Staged byte, Unstaged byte, Kind}` already carries the
  porcelain XY state per file; `domain.Status` parses `git status --porcelain`.
- The **Status panel** (left-bottom: Branches / Worktrees / Status) already
  renders each file as `XY path` (`statusRows()` in `view.go`).
- `space` (`tea.KeySpace`) is **unbound** in the main key dispatch (used only
  inside popups).
- Only `commit -a` and `ResetSoft` exist as write verbs — **index
  manipulation (`add` / `restore --staged`) is greenfield.**

The gap is purely the **action** + the **verbs** + a **targeted refresh**.

## Surface decision (settled)

**No new window.** Staging happens in place in the existing Status panel.
`space` on the selected Status row toggles its staged state. `enter` keeps its
current meaning (open the file diff). This is the cheapest path and matches
lazygit muscle memory.

## Toggle semantics (the one nuance the porcelain model forces)

A single file row can be staged *and* unstaged at once (`XY = MM`). With one
line per file, `space` uses an unambiguous whole-file rule:

| Row state | `space` action |
|-----------|----------------|
| Untracked (`Kind == KindUntracked`, `??`) | `git add -- <path>` (stage the new file) |
| Has any unstaged content (`Unstaged != '.'`, incl. `MM`) | `git add -- <path>` (stage everything for the file) |
| Fully staged (`Unstaged == '.'`/0 and `Staged != '.'`/0) | `git restore --staged -- <path>` (unstage it) |
| Conflicted (`Kind == KindUnmerged`, `UU`) | **no-op in F1**; `statusMsg`: "resolve conflicts first" — "mark resolved" belongs to F4 |

Rule in one line: **if there is anything unstaged, stage the file; otherwise
unstage it.** A mixed `MM` file takes one `space` to fully stage, another to
fully unstage — never a half-state you can't reason about. Hunk-level partial
staging is **F3**.

## New git verbs (`internal/git/stage.go`)

Built with `gitcmd`, run via `r.Runner.Run`, added to the `engine.GitOps`
interface (next to `Commit`/`ResetSoft`):

- `StagePaths(ctx, paths []string) error` — `git add -- <paths...>`.
- `UnstagePaths(ctx, paths []string) error` — `git restore --staged -- <paths...>`.

`--` guards against paths that look like flags. Both take a slice so F3/“stage
all” can reuse them, but F1 calls them with a single path.

## Engine op (`internal/engine/stage.go`)

```
engine.Stage{ Paths []string, Unstage bool }
```

An `Operation` run through `domain.Execute` (uniform write path, gate, future
MCP). No `Decider` interaction — it emits a `Progress` and returns. Reservation:
**TreeWrite** (it writes `.git/index`). On `Unstage == false` it calls
`StagePaths`; otherwise `UnstagePaths`. `Result.Summary` = e.g. `staged
internal/foo.go` / `unstaged internal/foo.go`; `Changed: true`.

*(Decision to confirm: op-vs-domain-command. Recommended: a real engine
`Operation` for write-path uniformity and MCP-readiness, even though staging is
a fast local mutation with no decisions.)*

## Refresh strategy (the perf-relevant decision)

The generic op-finished path reloads the **whole** snapshot via `loadCmd()`
(branches + worktrees + commit-times + status). On a ~100GB repo that full
reload — dominated by `git status` — would make repeated `space` presses
sluggish.

**F1 refreshes only the Status panel.** Pressing `space` runs a dedicated
`stageCmd` that, on completion, re-runs **only** `domain.Status` and updates
`m.status` (and the derived counts) — it does **not** trigger `loadCmd()` or
touch branches/worktrees/commits. This is a contained, F1-scoped instance of
the F7 surgical-refresh idea; generalized affected-window refresh stays F7.

The Status read still costs one `git status` (unavoidable to reflect the new XY
state); we just stop paying for the other three reads on every keypress.

## TUI wiring

- `space` arm in `Update`'s key dispatch, gated by a new `avail.go` predicate
  `canStage` (`m.focus == panelStatus && m.opsIdle()` and a row is selected).
- Resolve the selected `StatusFile` via `backingIndex(panelStatus)`, decide
  stage vs unstage from the toggle table, dispatch `stageCmd`.
- Conflicted row → `m.statusMsg = "resolve conflicts first"`, no op.
- Footer: a `space`/`[space] stage` binding in `footer.go` gated by `canStage`;
  a help row in `help.go` (`TestHelpFooterCoverage`).
- While the stage op runs, `opsIdle()` already blocks other ops; selection is
  preserved by stable index.

## Out of scope (explicit — these are later roadmap features)

- **Commit / amend** the staged index → **F2** (there is no TUI commit yet;
  until F2, commit via `gg commit` CLI).
- **Hunk / line-level** partial staging → **F3**.
- **"Mark resolved"** on conflicted files → **F4**.
- **Stage-all / unstage-all** key → possible later addition; F1 is per-file.
- CLI `gg stage`/`gg unstage` → not in F1 (TUI-only); add later if wanted.

## Tests

- `internal/git/stage_test.go`: argv (`FakeRunner`) for `StagePaths`/
  `UnstagePaths` (incl. the `--` guard and multi-path); a real-repo test
  staging then unstaging a file, asserting porcelain XY transitions.
- `internal/engine/stage_test.go`: `Stage{Paths, Unstage}` stages/unstages in a
  real `newRepo`, asserting `Status` reflects it; `Summary` text.
- `internal/tui/`: `space` on an unstaged row stages it (status refreshes,
  no full reload — assert branches/worktrees untouched via a load counter or
  by asserting only status changed); `space` on a fully-staged row unstages;
  `space` on a conflicted row sets the statusMsg and runs no op; key-swallow
  not relevant (panel key). A fit/selection-preserved check.

## Files touched

| File | Change |
|------|--------|
| `internal/git/stage.go` (+test) | Create: `StagePaths`, `UnstagePaths` |
| `internal/engine/gitops.go` | Add the two verbs to `GitOps` |
| `internal/engine/stage.go` (+test) | Create: `engine.Stage` operation |
| `internal/tui/avail.go` | Add `canStage` predicate |
| `internal/tui/model.go` | `space` dispatch + `stageCmd` + status-only refresh msg |
| `internal/tui/load.go` (or a new `stage.go`) | `stageCmd` + `statusRefreshedMsg` handling |
| `internal/tui/footer.go`, `help.go` | `[space] stage` hint + help row |
| `internal/tui/*_test.go` | staging key tests |
| `CHANGELOG.md` | entry |

## Open decisions to confirm at review

1. **Op vs domain-command shape** for staging (recommended: engine `Operation`).
2. **Status-only refresh** approach (recommended; confirm we don't want full
   reload for simplicity).
3. Whether to include a **stage-all** key in F1 or defer it.
