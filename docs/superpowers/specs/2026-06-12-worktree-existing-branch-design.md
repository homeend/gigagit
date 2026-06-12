# Worktree for an Existing Branch (`w` / `W` rework) — Design Spec

**Date:** 2026-06-12
**Status:** Approved (design agreed in chat; this document records it)
**Scope:** Split the worktree-create entry points: `w` creates a worktree that
**checks out the selected (existing) branch**; `W` keeps today's behavior —
new branch (from templates) + worktree. New engine op, new git verb, a popup
mode, a CLI flag, agent skill v3.

## Goal

Today `w` always creates a *new* branch with the worktree. The common monorepo
flow "give me a worktree for the branch I already have" requires leaving the
TUI. After this change:

- **`w`** (Branches panel selection) → worktree **for** the selected branch
  (no new branch). Popup shows the fixed branch + a live path preview.
- **`W`** → exactly today's `w` popup (new branch from the branch template,
  selected branch = `<parent-branch>`).
- CLI: `gg worktree add --branch <name>` uses that existing branch.

## 1. Git verb (`internal/git/worktree.go`)

New verb (one invocation; existing `AddWorktree` keeps `-b`):

```go
// AddWorktreeForBranch creates a linked worktree at path that checks out the
// existing branch (`git worktree add <path> <branch>`). Output lines are
// forwarded to onLine (nil allowed); the checkout is cancellable via ctx.
func (r *Repo) AddWorktreeForBranch(ctx context.Context, path, branch string, onLine func(string)) error
```

Note: bare `git worktree add <path> <branch>` has remote-DWIM (a missing local
branch with a matching `origin/<branch>` is silently created with tracking).
The engine guards existence first, so the verb only ever runs against a known
local branch and the DWIM can't trigger.

## 2. Engine (`internal/engine/create_worktree_for_branch.go`)

A separate operation — not a flag on `CreateWorktree` — because the guards
differ (existing-must-exist vs new-must-not-exist):

```go
// CreateWorktreeForBranch creates a linked worktree that checks out an
// EXISTING branch. A relative Path resolves against the repo root.
type CreateWorktreeForBranch struct {
    Branch string
    Path   string
}
```

Run (no decisions):

1. Guard `Branch != ""` and `Path != ""`.
2. Guard: branch exists locally — scan `deps.Repo.Branches(ctx)`; missing →
   `"create worktree: no local branch %q"` (this also forecloses git's
   remote-DWIM).
3. Guard: branch not already checked out — `deps.Repo.WorktreeForBranch(ctx,
   branch)`; non-nil → `"branch %s is already checked out in worktree %s"`
   (cleaner than git's refusal, the DeleteBranch-guard precedent).
4. Resolve relative Path against `TopLevel` and refuse an existing path
   (byte-for-byte the CreateWorktree logic — extract the shared helper
   `resolveNewWorktreePath(ctx, deps, path) (string, error)` into
   create_worktree.go and use it from both ops).
5. `Progress{Step: "creating worktree", Detail: branch + " → " + abs}`;
   `AddWorktreeForBranch` streaming `GitLine`s.
6. `Result{Summary: "worktree created: " + abs, Changed: true, Path: abs}` +
   `Done` (Path set so the TUI's create-and-switch re-root works unchanged).

## 3. TUI (`internal/tui/worktree_popup.go`, `model.go`)

One popup with a mode flag — the rendering/keys/preview machinery is shared:

- `worktreePopup` gains `existing bool`.
- **`openWorktreePopup(existing bool)`**: in existing mode the branch template
  is bypassed — `worktree.Resolve(tm, /*fixedBranch=*/ selected, …)` (the
  mechanism already shipped for hand-edits), so the preview shows
  `branch: <selected>` fixed and the path resolved with `ctx.Branch` set.
  Labels = the **path** template's `<user:…>` labels only; seq consumption =
  path template's `SeqNames()` only (the `consumedSeqNames` precedent —
  in existing mode it always takes the branchOverride-style arm).
  The `e` (edit branch) action is hidden/inert in existing mode — the branch
  is the point of the flow. Title: `Create worktree for <branch>` (vs today's
  `Create worktree from <branch>`).
- **Key wiring (model.go):** `w` → `openWorktreePopup(true)` (existing),
  `W` → `openWorktreePopup(false)` (new-branch, today's behavior). Both keep
  working from any panel (they act on the Branches selection via
  `backingIndex`); empty/no selection keeps the popup closed.
- **Inside the popup nothing changes:** `enter`/`w` create, `W` create **and**
  switch (pendingSwitch + Result.Path re-root), `esc` cancel. In existing
  mode create launches `CreateWorktreeForBranch`; otherwise `CreateWorktree`.
  `createOp()` returns `engine.Operation` choosing by mode.
- Footer: `[w]orktree` hint unchanged (one word covers both); README explains
  the split.

## 4. CLI (`internal/cli/worktree.go`)

`gg worktree add` gains `--branch <name>`:

```
gg worktree add [<start-point>]          # today: new branch from templates
gg worktree add --branch <name>          # NEW: worktree for existing branch
```

- With `--branch`: a positional start-point is a usage error (exit 2) — the
  branch *is* the source. Path resolves from the path template with
  `fixedBranch` (prompting for the path template's `<user:…>` fields only);
  seq bumps limited to the path template's names. Runs
  `CreateWorktreeForBranch` via `runOperation` + `finish`; `--cwd-file`
  behavior unchanged.
- Flags precede positionals (the existing worktree-remove convention).

## 5. Agent skill v3

1. `internal/agentskill/using-gg.md`: extend the worktree bullet with
   `add --branch <name>` ("create a worktree for an existing branch").
   Verify the flag against the code before writing.
2. `agentskill.Version` 2 → 3; `gg init --update`; commit the regenerated
   `.claude/skills/using-gg/SKILL.md` (the drift-guard test enforces sync).

## 6. Files touched

| File | Change |
|------|--------|
| `internal/git/worktree.go` | `AddWorktreeForBranch` verb. |
| `internal/engine/create_worktree.go` | Extract `resolveNewWorktreePath` helper. |
| `internal/engine/create_worktree_for_branch.go` (new) | The op (guards, no decisions). |
| `internal/tui/worktree_popup.go` | `existing` mode (fixed branch, labels/seqs/title/keys). |
| `internal/tui/model.go` | `w` → existing mode, `W` → new-branch mode. |
| `internal/cli/worktree.go` | `--branch` flag on `add`. |
| `internal/agentskill/{using-gg.md, agentskill.go}` | Bullet + `Version = 3`. |
| `.claude/skills/using-gg/SKILL.md` | Regenerated v3 (drift-guard enforced). |
| `CHANGELOG.md`, `README.md` | Entry; key table (`w`/`W`) + CLI block. |

## 7. Testing

- **git verb:** real repo — `AddWorktreeForBranch` checks out an existing
  branch at the path; HEAD of the new worktree == the branch.
- **engine:** happy path (existing branch → worktree created, `Result.Path`
  set, Done emitted); missing-branch guard (and no worktree dir created);
  already-checked-out guard (message names the worktree path); existing-path
  guard; relative path resolves against repo root. Existing `CreateWorktree`
  tests must stay green after the helper extraction.
- **TUI:** `w` opens the popup in existing mode (branch fixed to the
  selection, no branch-template labels prompted, `e` inert); enter creates a
  worktree checking out the selected branch (drive the op, assert
  `git -C <path> symbolic-ref` == branch); `W` opens today's popup
  (new-branch mode — existing popup tests keep passing, retargeted to `W`
  where they press the open key); create-and-switch still re-roots in both
  modes; seq counters: existing mode bumps only path-template seqs.
- **CLI:** `--branch` creates a worktree for the branch (HEAD check);
  `--branch` + positional start-point exits 2; missing branch exits 1 with
  the guard message; plain `add` (no flag) behavior unchanged (existing tests
  green).
- **agentskill:** body mentions `--branch`; drift-guard keeps the committed
  copy in sync.

## 8. Out of scope (YAGNI)

- Checking out remote-only branches (`origin/x` DWIM) — guard refuses;
  create the local branch first (`gg branch create`).
- A path-edit field in the popup; `--force` checkout of an already-checked-out
  branch; `gg worktree checkout` subcommand; detached-HEAD worktrees.
