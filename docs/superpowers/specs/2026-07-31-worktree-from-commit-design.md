# Worktree from a commit, with keep-changes modes — design

**Date:** 2026-07-31
**Status:** approved (brainstorm complete)
**Branch:** `feat/worktree-from-commit`

## What

Create a linked worktree anchored at a specific commit selected in the Commits
panel (or named on the CLI), with three variants:

1. **At this commit** — the new worktree's new branch points at the selected
   commit. Commits after it are simply not on the new branch. This already
   exists ("Create worktree here"); it gains a new label and a branch-name
   prefill.
2. **At parent, changes staged** — the new branch points at the *parent* of the
   selected commit; the commit's own diff sits in the new worktree staged
   (`git reset --soft <sha>^` after creation).
3. **At parent, changes unstaged** — same, but unstaged
   (`git reset --mixed <sha>^`).

The current worktree and the original branch are never touched — the operation
is purely additive, so there is **no dirty-tree guard** on the current
checkout. The only guards are technical, on modes 2–3: a **root commit** (no
parent) and a **merge commit** (ambiguous diff) are refused up front, before
anything is created.

## Engine (`internal/engine`)

Extend `CreateWorktree`:

```go
type WorktreeKeep int

const (
    KeepNone     WorktreeKeep = iota // zero value = today's behavior
    KeepStaged                       // reset --soft to StartPoint^
    KeepUnstaged                     // reset --mixed to StartPoint^
)

type CreateWorktree struct {
    StartPoint     string
    Branch         string
    Path           string
    PostCreateHook string
    Keep           WorktreeKeep
}
```

Behavior when `Keep != KeepNone`:

- **Pre-validate** `StartPoint` via the existing `git.ParentCount` BEFORE
  creating anything: 0 parents (root) or ≥2 (merge) → typed error
  (`WorktreeKeepParentError{Sha, Parents}` naming the reason), nothing created.
- Create the worktree at `StartPoint` exactly as today (branch validation,
  path resolution, `AddWorktree`).
- Run one extra verb in the new worktree's directory:
  `ResetInDir(ctx, abs, StartPoint+"^", soft)`.
- Because the parent was pre-validated, a reset failure is a
  near-impossibility; if it happens anyway, return an error that names the
  created worktree path so the user knows what exists on disk. No rollback
  machinery.
- The post-create hook runs AFTER the reset (the hook must see the final
  state).
- Summary gains a suffix via the lockstep i18n helpers:
  `worktree created: <path> (commit's changes staged)` /
  `… (commit's changes unstaged)`.

New git verb (`internal/git`): `ResetInDir(ctx, dir, ref string, soft bool)` —
one invocation, `git -C <dir> reset --soft|--mixed <ref>` (dir-scoped verb per
the `ShowFileInDir` precedent).

`opAffectedSources` needs no change: `CreateWorktree` already maps to
`{branches, worktrees}`, and modes 2–3 only affect the NEW worktree's status,
which no panel shows.

## TUI (`internal/tui`)

- The existing Commits-panel `.`-menu row is relabelled **"Create worktree
  from this commit…"**. It opens the existing `fromCommit` popup
  (`openWorktreeAt`), now pre-filling the branch field with
  `<current-branch>_<short-sha>` (`wt_<short-sha>` when HEAD is detached).
  A branch name containing `/` keeps it; the path derives from the branch
  sanitized per-OS as today. Fully editable as before.
- The popup gains a **mode line** (only when opened from the Commits panel),
  cycled with a key shown in the popup footer:
  `at this commit` → `at parent, changes staged` → `at parent, changes
  unstaged`. Default `at this commit`. For a root or merge commit the mode is
  locked to `at this commit` with a dim note; the parent count comes from the
  feed's own `model.Commit` data — no extra git call.
- Tag and remote-branch entrances to `openWorktreeAt` keep their current
  prefills and do NOT get the mode line (mode 2 is a commit-rework tool).
- All new strings go through `i18n.T` with keys in all four bundles
  (ja/ko/zh/ru); the AST gates enforce this.

## CLI (`internal/cli`)

`gg worktree add --from <commit> [--keep staged|unstaged] [<branch-name>]`:

- `--from <commit>` = from-commit mode: start point is the commit; the branch
  name defaults to `<current-branch>_<short-sha>`, overridable with the
  optional positional `<branch-name>` (flags precede positionals, the
  `gg review` convention); the path derives from the branch as usual.
- `--keep staged|unstaged` selects modes 2–3 (omitted = at-commit). Root or
  merge commit fails loud with the engine's typed error, exit 1.
- `--keep` without `--from` is a usage error (exit 2). Any other `--keep`
  value is a usage error.
- Hook flags (`--hook`/`--no-hook`) and exit conventions unchanged.
- `--from` is incompatible with `--branch` (which checks out an EXISTING
  branch — from-commit always creates a new one): usage error.

## Tests

- **Engine** (real git, TDD): all three modes (branch tip, staged vs unstaged
  status in the new worktree), root refusal, merge refusal, refusal happens
  before any worktree/branch is created, hook-after-reset ordering.
- **git verb**: `ResetInDir` soft/mixed against a scratch repo.
- **TUI**: popup mode-cycle, prefill (`<branch>_<short-sha>`, detached
  fallback), mode lock on root/merge commits.
- **CLI**: flag parsing/usage errors (unit), plus an e2e scenario:
  `gg worktree add --from <sha> --keep staged` asserting the new branch tip
  is the parent and the new worktree's status shows the commit's files staged.

## Docs (after implementation)

CHANGELOG (always), README (user-facing surface), CLAUDE.md (engine field +
verb + CLI flags in the package map), `internal/agentskill/using-gg.md` +
`agentskill.Version` bump + `gg init --update` (CLI surface changed).
