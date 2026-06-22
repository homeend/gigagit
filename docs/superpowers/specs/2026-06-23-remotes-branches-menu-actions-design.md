# Remotes/Branches Menu Actions (GitKraken parity, Bucket A) — Design

**Date:** 2026-06-23
**Status:** Approved, ready for plan

## Goal

Bring `gg`'s Remotes- and Branches-panel `.` (action) menus closer to GitKraken's
remote-branch context menu, by surfacing capabilities that **already exist in the
engine** on the relevant row. This is the "easy wiring wins" bucket (Bucket A)
from the GitKraken-menu gap analysis.

## Background

GitKraken's graph row is *both a ref and a commit*, so its remote-branch menu
blends ref-ops (checkout, delete, worktree) with commit-ops (reset, revert, tag,
edit message). `gg` deliberately splits these: commit-ops live on the **Commits**
panel, ref-ops on the **Remotes**/**Branches** panels. As a result most of
GitKraken's menu is not missing — it is one panel over. This feature closes the
small set of ref-level actions that belong on a remote/branch row and are not yet
surfaced there.

Out of scope (separate features / Bucket B): delete remote branch (`push
--delete`), copy web links, hide-branch-from-graph.

## Scope

Two parts.

### Part 1 — Copy commit id + sha (Branches & Remotes rows)

Today the Branches and Remotes rows offer only **Copy branch name**. Add two more
copy actions to each, for the row's tip commit:

- **Copy commit id** — the short hash already carried in the model
  (`model.Branch.Hash` / `model.RemoteBranch.Hash`). Instant, static copy.
- **Copy commit sha** — the full 40-char SHA, **resolved on invoke** (not at
  menu-build time) by running `git rev-parse` against the ref name
  (`branch.Name` / `rb.Name`). Falls back to the short hash if resolution errors.

The full-SHA resolution is lazy by design: the menu builds without a git call;
the resolve happens only when the user actually picks the action.

Commits and Reflog panels already offer a sha copy and are unchanged. Worktrees
and Tags are explicitly out of scope for this feature.

### Part 2 — Remotes-row operations

Add three operations to the Remotes-panel `.` menu, acting on the selected
remote-branch row (`rb`, e.g. `origin/feat/x`):

- **Create worktree from `origin/x`** — opens the existing "worktree from
  commit/tag" popup seeded with the remote ref as start-point and the
  de-prefixed branch name (`rb.Branch`) as the branch prefill. Reuses
  `Model.openWorktreeAt(startPoint, prefillBranch)`.
- **Merge `origin/x` into current (`<cur>`)** — `engine.SmartMerge{Source:
  rb.Name}` (empty `Target` defaults to the current branch).
- **Rebase current (`<cur>`) onto `origin/x`** — `engine.SmartRebase{Onto:
  rb.Name}` (empty `Branch` defaults to the current branch).

Branches already has the worktree popup (`w`) and the mark-pair merge/rebase
gesture, so Part 2 applies to Remotes only.

## Architecture & components

Everything reuses existing engine ops, popups, and git verbs. The only new
non-TUI code is one thin domain query.

### New: `domain.Service.RevParse`

`internal/domain/query.go`:

```go
// RevParse resolves rev to a full object id, under a Read reservation.
func (s *Service) RevParse(ctx context.Context, rev string) (string, error) {
	return query(ctx, s, "revparse:"+rev, func(ctx context.Context) (string, error) {
		return s.repo.RevParse(ctx, rev)
	})
}
```

Wraps the existing `git.Repo.RevParse(ctx, rev)` verb (`internal/git/query2.go`,
`git rev-parse --verify --quiet`). `GitOps` already has access; `*git.Repo`
satisfies the call.

### TUI wiring

`internal/tui/remote_actions.go`:

- `copyShaRow(ref, fallbackShort string) actionRow` — a run-handler copy row.
  On invoke it resolves `ref` to a full SHA via `m.svc.RevParse` (nil-svc and
  error both fall back to `fallbackShort`), then copies via
  `m.copyToClipboardCmd`. Reused by both the Branches and Remotes copy cases.
- `remoteCreateWorktreeRow()` — gated on a selected remote row + `opsIdle`; runs
  `m.openWorktreeAt(rb.Name, rb.Branch)`.
- `remoteMergeRow()` — gated on selected remote + `opsIdle` + non-detached
  current branch + `rb.Name != cur`; runs `m.startOp(engine.SmartMerge{Source:
  rb.Name})`.
- `remoteRebaseRow()` — same gating; runs `m.startOp(engine.SmartRebase{Onto:
  rb.Name})`.

`internal/tui/action_menu.go`:

- In `contextCopyRows`, extend the `case m.focus == panelBranches` and `case
  m.focus == panelRemotes` branches: after the existing **Copy branch name**
  row, append **Copy commit id** (`copyRow` with the short hash) and **Copy
  commit sha** (`copyShaRow(name, shortHash)`).
- In `availableActions`, append `remoteCreateWorktreeRow`, `remoteMergeRow`,
  `remoteRebaseRow` (each self-gating, mirroring the existing `remotePruneRow`
  append).

### Current-branch detection

Merge/rebase need the current branch name and must detect detached HEAD. The
Commits-panel fast-forward feature already established this requires guarding
both the engine view (`""` = detached) and the porcelain v2 view
(`"(detached)"`). Use `m.status.Branch` (porcelain) for gating/labels and treat
`""` and `"(detached)"` as detached → row hidden. The engine ops re-read the
current branch themselves; the TUI only needs it for the label and the
`rb.Name != cur` guard.

## Error handling

- **Copy sha resolve failure** — fall back to the short hash; the copy still
  succeeds with a usable (abbreviated) value. No error surfaced.
- **Merge/rebase conflicts / dirty tree** — handled entirely by the existing
  `SmartMerge`/`SmartRebase` Decider ladder (autostash / switch / conflict
  pause), which the TUI already maps to its modal. No new confirm modal: these
  are local and reversible (reflog), matching GitKraken's one-click offering.
- **Worktree creation** — the existing popup owns its own validation and
  per-OS path sanitization.

## Testing

`internal/tui/remote_actions_test.go` (new):

- Copy rows: Branches and Remotes each expose Copy branch name + Copy commit id
  (short) + Copy commit sha; assert ids/labels present and the commit-id row
  carries the short hash.
- `copyShaRow` dispatch: with a fake `svc` whose `FakeRunner` returns a full SHA
  for `git rev-parse`, invoking the row copies the full SHA; with a resolve
  error it copies the fallback short hash.
- Remote op rows present/gated: `remoteCreateWorktreeRow`/`remoteMergeRow`/
  `remoteRebaseRow` appear when a remote row is selected and `opsIdle`; merge and
  rebase are hidden on detached HEAD and when `rb.Name == cur`.
- Dispatch wiring: selecting merge/rebase starts the expected op (assert via the
  `startOp` path with a fake svc, as in existing menu-dispatch tests).

A fake `svc` (`domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})`) is
required because `startOp` spawns a goroutine that calls `svc.Execute`, and the
copy-sha row calls `svc.RevParse`.

`internal/domain` gets a `RevParse` test against a real git temp repo (resolves a
ref to its full 40-char SHA; errors on a bogus ref).

## Files

- Modify: `internal/domain/query.go` — add `RevParse`.
- Create: `internal/domain/revparse_test.go` — `RevParse` test.
- Modify: `internal/tui/remote_actions.go` — `copyShaRow` + 3 remote op rows.
- Modify: `internal/tui/action_menu.go` — copy cases + append the 3 rows.
- Create: `internal/tui/remote_actions_test.go` — gating + dispatch tests.
- Modify: `CHANGELOG.md`, `README.md` (Remotes/Branches menu surface).

No engine, git-verb, CLI, or agentskill changes (all reused).

## Non-goals

- Delete remote branch, copy web links, hide branch (Bucket B / later).
- Worktrees/Tags copy actions.
- New CLI surface (`gg merge`/`gg rebase`/worktree-from-ref already accept refs).
