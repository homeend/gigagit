# Context ops: copy branch name · rename branch · rename (reword) commit

Date: 2026-06-19
Status: design (approved verbally; pending written review)

## Goal

Add three GitKraken-style **context operations**, reachable from the TUI `.`
action menu and from the CLI:

1. **Copy branch name** — copy the selected branch's name to the clipboard.
2. **Rename branch** — `git branch -m <old> <new>`.
3. **Rename (reword) commit** — change a commit's message, reusing the
   interactive-rebase machinery.

These are small, additive operations that thread through the existing
engine → domain → frontend layers. No new architectural surface.

## 1. Copy branch name

Pure render + clipboard, no git. Mirrors the existing Copy commit-id / file-path
rows (`internal/tui/action_menu.go` `contextCopyRows`, OSC-52 via
`internal/clipboard`).

- Add a case to `contextCopyRows` for the **Branches** panel: when a local
  branch row is selected, return one `copyRow("copy-branch-name", "Copy branch
  name", …, branch.Name)`.
- Add a case for the **Remotes** panel: copy the **full** remote-tracking name
  (`origin/foo`), matching how a user would paste it into `gg checkout`.
- `.`-menu-only (no dedicated keybinding), consistent with the other copy rows.

No engine/domain/CLI changes. Help text: add the rows under the Branches /
Remotes panel sections in `help.go`.

## 2. Rename branch

`git branch -m <old> <new>` renames a local branch. Verified behaviour
(temp-repo probe):

- Renaming a branch **checked out in another worktree** succeeds and updates
  that worktree's HEAD — **no worktree-aware refusal is needed**.
- Renaming to an **existing** name is refused by git itself (`fatal: a branch
  named 'x' already exists`).

### Layers

- **git verbs** — `Repo.RenameBranch(ctx, old, new string) error` in
  `internal/git/mutate.go`: `git branch -m <old> <new>`. One invocation. (The
  reword HEAD path reuses the existing `Repo.Commit(…, amend=true)` verb — no
  new amend verb.)
- **engine op** — `engine.RenameBranch{Old, New string}`
  (`internal/engine/rename_branch.go`), modelled on `engine.CreateBranch`:
  - require both fields non-empty;
  - validate the new name via the existing `CheckRefFormatBranch` verb (clear
    message ahead of git's terser ref error);
  - emit `Progress{Step:"renaming branch", Detail: old+" → "+new}`;
  - call `RenameBranch`, wrapping git's error (covers "already exists");
  - `Result{Summary:"renamed branch "+old+" → "+new, Changed:true}` + `Done`.
  - **Lock mode:** default `TreeWrite`, matching its `CreateBranch` /
    `DeleteBranch` siblings (they declare no `LockMode`).
- **domain** — no new method; frontends build the op and call `domain.Execute`,
  exactly as branch create/delete do.
- **CLI** — `gg branch rename <old> <new>` in `internal/cli/branch.go`
  (new `case "rename"` in `cmdBranch`, a `cmdBranchRename` mirroring
  `cmdBranchDelete`'s shape). Both args required; usage error otherwise.
- **TUI** — `.`-menu row **Rename branch** scoped to the Branches panel. It
  opens a **text-input popup** prefilled with the selected branch's current
  name (reusing the `branchPopup` input pattern in
  `internal/tui/branch_popup.go`); submitting runs `engine.RenameBranch{Old:
  selected, New: typed}` via `startOp`. Footer + help entries.

## 3. Rename (reword) commit

Change the message of a commit on the **current branch**. The commit is selected
in the Commits panel.

### Why a dedicated op (not a direct `InteractiveRebase` call)

`engine.InteractiveRebase.Run` validates that **both `Branch` and `Onto` are
existing branch names** (`interactive_rebase.go:49-52`). Rewording commit `C`
means rebasing `C^..branch` onto `C^` — a **parent SHA, not a branch**. That
guard protects the rebase-onto-branch use case and must not be loosened. So
reword is a thin new op that **reuses `InteractiveRebase`'s internals**
(`wrapped` / `irebaseAt`), bypassing only the top-level branch validation.

Verified empirically: `git rebase -i C^` with a plan of `reword C` + `pick` for
every later commit rewrites the current branch **in place**, preserves all later
commits' content, and leaves **no temporary branch**. git does the
reset-to-`C^` + cherry-pick-forward internally — we neither create a temp branch
nor cherry-pick by hand.

### The op

`engine.Reword{Commit, NewMsg string; GGBin string}`
(`internal/engine/reword.go`):

1. Validate: `Commit` and `NewMsg` non-empty; resolve the current branch.
2. Resolve whether `Commit` is **HEAD** of the current branch.
   - **C == HEAD** → reword via the **existing** `Repo.Commit(ctx, NewMsg,
     all=false, amend=true)` verb (= `git commit --amend -m <NewMsg>`, no `-a`).
     No rebase. The common case. **Message-only guard:** `--amend` folds any
     **staged** index into HEAD, so to keep reword message-only the op
     stash-wraps when the index has staged changes (the same stash approach the
     rebase path uses), restoring after — making the HEAD and mid-branch paths
     behave identically w.r.t. a dirty worktree. Unstaged-only changes are
     untouched by `--amend` and need no stash.
   - **C != HEAD, has a parent** → build a single-reword
     `rebaseplan.Plan` covering the full `C^..HEAD` range (the target marked
     `Reword` with `NewMsg`, every other entry `Pick` — **the whole range, or
     later commits drop**), then run it through the rebase internals with
     `Onto = C^`. Reuse `InteractiveRebase{Branch: cur, Onto: parentSHA, Plan,
     GGBin}` and invoke its unexported `wrapped(ctx, deps, "", env)` directly
     (same package). A pure reword cannot conflict (identical trees replay), so
     the conflict fork is dead-but-harmless.
   - **C == root** (no parent) → **refuse** with a clear message: rewording the
     repository's root commit needs `git rebase -i --root`, which this machinery
     does not implement (v1 scope). Note: a single-commit repo's only commit
     *is* HEAD, so it takes the cheap amend path; this refusal only fires for a
     *non-HEAD root in a multi-commit repo*.
3. Emit progress + `Done`; `Result{Summary:"reworded "+short(commit), Changed:true}`.

Range construction reuses the same `rebaseplan` builder the `i` editor uses
(`internal/tui/irebase_view.go`), so the todo list is always the full
oldest-first range with one entry flipped to `reword`.

`GGBin` is threaded via `os.Executable()` at the frontend edge, the same way
`irebase_view.go` and `cli/rebase.go` obtain it.

### Frontends

- **CLI** — `gg commit reword <commit> -m <message>`
  (`internal/cli/commit.go` or a `commit` dispatcher). `-m` required for v1 (no
  editor/stdin yet — keeps the CLI scriptable and the spec tight). Resolves the
  commit, builds `engine.Reword`, runs via `runOperation`.
- **TUI** — `.`-menu row **Rename commit** scoped to the Commits panel; opens a
  **message popup** prefilled with the commit's current message (reuse the
  commit-message popup, `internal/tui/commit_popup.go`), submitting runs
  `engine.Reword`. Footer + help entries.

## Edge cases

| Case | Behaviour |
|------|-----------|
| Rename branch to existing name | git refuses; error surfaced |
| Rename branch checked out elsewhere | succeeds (git retargets that worktree's HEAD) |
| Reword HEAD | `git commit --amend -m` (no rebase) |
| Reword mid-branch commit | rebase `C^..HEAD` in place, later commits preserved |
| Reword non-HEAD root commit | **refused** (needs `rebase -i --root`; out of v1 scope) |
| Reword with dirty worktree | handled by the rebase internals' stash-wrap (reused) |
| Copy on an empty/zero selection | no copy row emitted |

## Testing (TDD throughout)

- **git verbs** — argv assertions with `FakeRunner` (`RenameBranch`,
  `AmendMessage`).
- **engine ops** — against a real temp repo: `RenameBranch` (success,
  existing-name failure, invalid name); `Reword` (HEAD amend, mid-branch reword
  preserving later commits, root refusal).
- **TUI** — copy rows present for Branches/Remotes; rename-branch popup → op;
  reword popup → op (table/keypress tests like the existing menu tests).
- **CLI** — `gg branch rename`, `gg commit reword` happy paths + arg errors.
- **e2e** — scenarios for `gg branch rename` (assert `branches` / `branch`) and
  `gg commit reword` (assert `log` message changed; later commits intact).

## Out of scope (v1)

- Rewording the root commit (`rebase -i --root`).
- Reword message from `$EDITOR` / stdin in the CLI (only `-m`).
- Renaming remote branches (push/delete dance) — local only.
- Multi-commit batch reword (the `i` editor already covers richer edits).

## Docs

- `CHANGELOG.md` (always); `README.md` CLI verbs + Branches/Commits sections;
  `internal/agentskill/using-gg.md` + bump `agentskill.Version`, then
  `gg init --update` to refresh installed copies; `CLAUDE.md` only if the
  package map changes (it does not).
