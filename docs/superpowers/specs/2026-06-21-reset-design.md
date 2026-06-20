# Reset to this commit — soft / mixed / hard (pipeline #9) — design

## Goal

Move the current branch to a selected commit, choosing how much of the working
state to keep: **soft** (changes since stay staged), **mixed** (changes stay
unstaged — git's default), **hard** (uncommitted tracked changes discarded). The
last and most destructive pipeline item; deliberately NOT shaped like
cherry-pick/revert (reset never conflicts, so no conflict integration).

## Scope

- Reset the **current branch** to the **selected commit** (Commits panel `.`
  menu) / `<commit>` (CLI), in the current worktree.
- Mode is an option-list **Decider** (`reset-mode`): `soft | mixed | hard |
  cancel`. In the TUI that modal is the deliberate confirmation step (user
  choice: picking `hard` is the confirm — no second prompt for hard+dirty).
- **Non-ancestor guard** (all modes): the Commits panel is the multi-branch feed,
  so the target may be on another branch. If the target is NOT an ancestor of
  HEAD, a second `reset-confirm` decision (`proceed | cancel`) warns that the
  branch will move to a commit not on it. (Resetting backward along the current
  branch — the common case — skips this.)

## Behavior of each mode (informs the option labels)

- `soft`  — `git reset --soft`:  branch moves; index + working tree untouched →
  the diff since the target is left **staged**. Nothing lost.
- `mixed` — `git reset --mixed`: branch moves; index reset, working tree kept →
  the diff is left **unstaged**. Nothing lost.
- `hard`  — `git reset --hard`:  branch moves; index + working tree reset →
  **uncommitted tracked changes are discarded**. Untracked files survive; the
  commits reset past remain recoverable via `git reflog`.

## Git verb (`internal/git/reflog.go`, next to ResetSoft)

`Reset(ctx, mode, ref string) error` → `git reset --<mode> <ref>` for mode ∈
{soft, mixed, hard}. (`ResetSoft` stays as-is for `UndoLastCommit`.) Add to the
engine `GitOps` interface, plus `IsAncestor` (already on `*git.Repo`, just needs
listing in `GitOps`).

## Engine op (`internal/engine/reset.go`)

`type Reset struct{ Commit string }`. `Run`:
1. require `Commit != ""`.
2. `decide("reset-mode", [soft, mixed, hard, cancel])`. `cancel`/empty →
   `Result{Summary:"reset cancelled", Changed:false}`. Validate the option.
3. `IsAncestor(Commit, "HEAD")`; if **false**, `decide("reset-confirm",
   [proceed, cancel])` with a non-ancestor warning; non-`proceed` → cancelled.
4. emit `Progress{Step:"resetting", Detail:mode+" → "+Commit}`; `Reset(mode,
   Commit)`; return `Result{Summary:"reset ("+mode+") to "+Commit, Changed:true}`.

No autostash, no conflict path (reset doesn't conflict). `LockMode` — a reset is
a tree+ref write; default (exclusive) lock is correct.

## TUI (`internal/tui/commit_scope.go`)

`commitResetRow() (actionRow, bool)`: gated `m.focus == panelCommits &&
m.opsIdle()`, commit via `backingIndex` → `m.commits[bi].Hash`. `run` →
`m.startOp(engine.Reset{Commit: hash})`. The op's two decisions surface as the
existing modal Decider (soft/mixed/hard/cancel, then proceed/cancel if
non-ancestor). Wired into `availableActions` after Revert; advertised in
`help.go`. (The modal already provides esc/cancel; the explicit `cancel` option
keeps the CLI and never-trap symmetric.)

## CLI (`internal/cli/reset.go`)

`gg reset [--soft|--mixed|--hard] [--force] <commit>`:
- mode flags map to `policy["reset-mode"]`; **no flag → `mixed`** (matches git's
  default, and it's non-destructive). At most one mode flag.
- `--force`/`-f` maps `policy["reset-confirm"]="proceed"` (answers the
  non-ancestor guard). Without it, a non-ancestor reset on a non-TTY exits 1 with
  the options on stderr (same as the other ops); on a TTY it prompts via stdin.
- bump `agentskill.Version`, document in `using-gg.md`, refresh dogfood SKILL.md
  (`gg init --update` — `TestDogfoodSkillCopyInSync` fails otherwise).

## e2e (`e2e/scenarios/`)

- `reset_mixed.toml`: 3 commits, `gg reset HEAD~1` (no flag → mixed) → branch at
  HEAD~1, the undone change shows as an unstaged modification, file content kept.
- `reset_soft.toml`: `gg reset --soft HEAD~1` → branch moved, the change staged.
- `reset_hard.toml`: dirty edit + `gg reset --hard HEAD~1` → branch moved, the
  change and the dirty edit gone (file back to the target's content).
- `reset_nonancestor_refused.toml`: a side branch; `gg reset <side-tip>` (no
  `--force`, non-TTY) → exit 1, branch unchanged.
- `reset_nonancestor_force.toml`: same with `--force` → exit 0, branch moved.

## Out of scope (v1)

`--keep`/`--merge` modes, resetting a different branch than the current one,
reset of specific paths (`git reset <paths>`), undo-of-reset UI (reflog already
recovers it). After this, only #2b (whole-commit compare) remains in the backlog.
