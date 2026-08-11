# Worktree rename & move

**Date:** 2026-08-11 · **Branch:** `feat/worktree-rename-move`

Git has no `git worktree rename`; the underlying capability is `git worktree
move` (2.17+). gg grows both faces: **move** (any destination path) and
**rename** (new directory name, same parent) — one engine op underneath.

## Engine

New op `engine.MoveWorktree`:

```go
type MoveWorktree struct {
    Path string // worktree to move (absolute path)
    Dest string // destination path (absolute)
}
```

- `LockMode() = TreeWrite`.
- Runs the new git verb `Repo.MoveWorktree(ctx, path, dest, onLine)` —
  one invocation: `git worktree move <path> <dest>`, argv via `gitcmd`,
  streamed so `GitLine` events flow.
- **Rename is not a second op.** Callers compute
  `Dest = filepath.Join(filepath.Dir(Path), newName)` and run `MoveWorktree`.
- Up-front validation (before touching git):
  - refuse moving the **main** worktree (git also refuses; we produce the
    friendly error first),
  - refuse empty/`.`/`..` names and a `Dest` that already exists,
  - refuse `Dest` inside the worktree being moved.
- The op does not create parent directories; if `filepath.Dir(Dest)` does not
  exist the op fails with a clear error naming the missing parent.

### Decision ladder (mirrors `RemoveWorktree`)

If git refuses because the worktree is **locked**, emit
`DecisionNeeded{ID: "move-worktree-locked"}` with options
`unlock and move` / `abort`. On `unlock and move`: run the existing
`UnlockWorktree` verb, retry the move once. Any other git refusal (submodules,
races) surfaces as a clean error carrying git's reason. Option values stay
English (agent-facing protocol).

## TUI

Worktrees panel:

- Two `.`-menu rows: **Rename worktree…** and **Move worktree…**.
- Direct key **`e`** = rename (menu row and key share one code path, house
  style). Move stays menu-only.
- Both open the existing text-prompt pattern:
  - rename prefills the current directory **basename**; on submit the new name
    is joined onto the old parent,
  - move prefills the full **absolute path**.
- Esc cancels; submitting an unchanged value is a no-op.
- **Follow the move:** if the moved worktree is the one gg is rooted in, chain
  a `guardedReRoot` onto the new path after `Done` — the same
  `pendingRepairSwitch` mechanism `RepairWorktree` uses in `opFinishedMsg`.
- The MRU repos registry (`internal/repos`) entry for the old path is
  rewritten to the new path so the switcher doesn't grow a dead row.
- New op mapped in `opAffectedSources` → worktrees + branches sources.
- All new strings through `i18n.T` with literal keys in all four bundles
  (ja/ko/zh/ru). Help overlay and footer advertise the `e` key.

## CLI

```
gg worktree rename <worktree> <new-name>
gg worktree move   <worktree> <new-path>
```

- `<worktree>` resolves by path or branch name, matching `worktree remove`.
- `rename` rejects a `<new-name>` containing a path separator (that's `move`).
- The locked fork answers through the standard `cliDecider` policy flags /
  stdin; `abort` is the non-interactive default.
- `internal/agentskill/using-gg.md` documents both verbs;
  `agentskill.Version` bumps.

## Out of scope

- Web sidebar row (web work lives on `web-dev`; follow-up there).
- MCP mutation surface.
- Renaming the *branch* together with the directory (branch rename already
  exists on the Branches panel).

## Testing (TDD)

- Engine: real-git `t.TempDir()` tests — happy move, rename face, moving the
  current worktree, locked → `unlock and move` ladder, locked → `abort`,
  target-exists and main-worktree refusals.
- Verb: `FakeRunner` argv assertion for `worktree move`.
- TUI: menu rows present/gated, `e` opens the prefilled prompt, submit runs the op,
  reRoot chained when moving the current worktree.
- CLI: unit tests for arg resolution + e2e scenario
  `e2e/scenarios/worktree-rename.toml` (rename via CLI, assert new path listed,
  branch checkout preserved).

## Docs

`CHANGELOG.md` entry; README worktree section gains the two verbs; this file.
