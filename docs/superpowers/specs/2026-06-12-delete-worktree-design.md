# Delete Worktree — Design Spec

**Date:** 2026-06-12
**Status:** Approved
**Milestone:** M2 (worktree management) — follows create/switch (A1–A3c).

## Goal

Let a user remove a linked worktree from gigagit, choosing whether to also
delete its branch, and — only when git refuses because the tree is dirty or the
branch is unmerged — choosing whether to force. Exposed in both the TUI (a key
on the Worktrees panel) and the CLI (`gg worktree remove`).

## Motivation

Creating worktrees (A2/A3) leaves no in-app way to clean them up; users drop to
raw `git worktree remove`. Worse, the natural cleanup ("remove the worktree and
its branch") is a two-command dance with ordering and force subtleties that are
easy to get wrong. gigagit should make the safe path one keystroke and surface
the destructive path only when it's actually required.

## Approach: reactive force

The user picks **worktree-only vs. worktree+branch** up front. We then try the
*safe* removal. Force is offered **reactively** — only if git refuses — never
pre-authorized. This mirrors the SmartPull "try the safe thing, fork when it
isn't safe" pattern and means the user never authorizes destroying uncommitted
work that may not even exist.

We deliberately **do not parse git's stderr** to classify failures (dirty vs.
unmerged vs. checked-out-elsewhere). Instead: run the safe command; on *any*
failure, offer force; run the force command; if force *also* fails, propagate
git's real error. Simple and robust. (A *locked* worktree, for instance, may
need force and still fail — that error simply propagates, which is correct.)

## Engine: `RemoveWorktree` operation

Fully Decider-driven, so the TUI's existing modal, the CLI's policy decider, and
a future MCP `MapDecider` all answer the same forks without bespoke per-frontend
flow.

```go
// RemoveWorktree removes a linked worktree, optionally deleting its branch.
// Force is resolved reactively (only when git refuses the safe command).
type RemoveWorktree struct {
    Path   string // absolute path of the worktree to remove
    Branch string // its short branch name; "" if detached (no branch to delete)
}
```

### Guards (fail fast, before any decision)

Returned as ordinary errors (`Result{}, err`):

- `Path == ""` → error.
- `Path` equals the **primary worktree** root → error ("cannot remove the main
  worktree"). git refuses this anyway; we give a clean message first.
- `Path` equals the **current worktree** (`deps.Repo.TopLevel(ctx)`) → error
  ("cannot remove the worktree you are currently in"). This lives in the engine
  (not just the TUI) so `gg worktree remove <path>` run *from inside* that
  worktree is also caught — otherwise the reactive flow would pointlessly offer
  "force?", which git also refuses for the cwd worktree, then surface a
  confusing error.

### Decision flow

1. **`remove-scope`** — Prompt: `Remove worktree at <path>?`
   - branch present → options `["worktree-only", "worktree-and-branch", "abort"]`
   - detached (`Branch == ""`) → `["worktree-only", "abort"]`
   - `abort` → no-op, `Result{Summary: "cancelled", Changed: false}`, nil error.

2. Run `git worktree remove <path>` (no force).
   - On **any failure** → **`worktree-dirty`**: options `["force", "abort"]`.
     - `force` → `git worktree remove --force <path>`. If *that* errors,
       propagate it.
     - `abort` → no-op (nothing removed yet), `Result{Changed: false}`.
   - `abort` is meaningful here because nothing has been deleted yet.

3. If scope is `worktree-and-branch` **and** branch present: run
   `git branch -d <branch>`.
   - On **any failure** → **`branch-unmerged`**: options `["force-delete", "keep"]`.
     - `force-delete` → `git branch -D <branch>`. If that errors, propagate it.
     - `keep` → leave the branch; success.
   - **No `abort` option here, by design:** the worktree is already removed and
     cannot be un-removed, so `abort` and `keep` would produce the identical
     outcome (worktree gone, branch retained). Collapsing them avoids implying
     the removal is reversible. esc in the TUI maps via `abortOption` to the
     **last** option (`keep`), the safe default.
   - The prompt text must state that **force-delete discards unmerged commits**
     — this is the one irreversible branch action.

### Ordering note

Worktree removal **must** precede branch deletion: git refuses to delete a
branch that is still checked out in a worktree. Step 2 always runs before
step 3.

### Result

- Success: `Result{Summary: "removed worktree <path>" [+ "and branch <branch>" |
  " (branch kept)"], Changed: true}`.
- Abort at scope, or abort at `worktree-dirty`: `Result{Changed: false}`.
- Emits `Progress` steps ("removing worktree", "deleting branch") and `GitLine`
  for streamed output, like `CreateWorktree`.

## git verbs (`internal/git/worktree.go`)

```go
// RemoveWorktree removes the linked worktree at path
// (`git worktree remove [--force] <path>`), forwarding output lines to onLine.
func (r *Repo) RemoveWorktree(ctx context.Context, path string, force bool, onLine func(string)) error

// DeleteBranch deletes a local branch (`git branch -d|-D <name>`).
func (r *Repo) DeleteBranch(ctx context.Context, name string, force bool) error
```

`RemoveWorktree` streams (like `AddWorktree`); `DeleteBranch` is a plain `Run`.
Argv built via `gitcmd.New(...)`; `--force` / `-D` added only when `force`.

## TUI

- **`d`** on the focused Worktrees panel, when not running/loading and a row is
  selected → `m.startOp(engine.RemoveWorktree{Path, Branch})` from the selected
  `model.Worktree`.
- The engine is the single source of truth for guards. The TUI does **not**
  pre-check current/primary worktree; it simply starts the op, and the engine's
  guard errors surface in the status line like any other op error.
- The three decisions render through the **existing modal** (`m.modal`, ↑/↓ to
  move, enter to choose, esc → `abortOption`). Every decision that needs a true
  cancel includes `"abort"`; `branch-unmerged` intentionally does not (esc →
  `keep`).
- **Selection clamp after delete (panic guard):** the delete path finishes via
  `loadCmd` (a reload), **not** `reRoot`, so `m.sel` is *not* cleared. After the
  worktree list shrinks, `sel[panelWorktrees]` can point past the new end; the
  next `m.worktrees[m.sel[panelWorktrees]]` access (model.go indexes exactly
  this way in the `d`/`enter` handlers) would panic. Fix: after a reload clamp
  every `sel[p]` to `max(0, panelLen(p)-1)` (general clamp on load covers all
  panels and is the robust choice). Verify with a test that deletes the last row.
- Footer/hint on the Worktrees panel gains `[d]elete`.

## CLI

`gg worktree remove <path>`:

- Positional `<path>` (required). Resolve relative paths against cwd; match
  against the worktree list to recover the `model.Worktree` (for `Branch`). If
  no worktree matches `<path>`, error.
- `--with-branch` → policy `remove-scope = "worktree-and-branch"` (default
  `"worktree-only"`).
- `--force` → policy `worktree-dirty = "force"` **and**
  `branch-unmerged = "force-delete"`.
- Build the `cliDecider` policy map from the flags; unanswered decisions fall
  back to interactive stdin prompt (existing `cliDecider` behavior) or error
  when non-interactive — unchanged from `gg pull --on-conflict`.
- Add a `"remove"` case to `cmdWorktree` dispatch (alongside `list`/`add`).

## Testing

**Engine** (`FakeRunner`, assert exact argv sequences and decision IDs):
- worktree-only, clean → only `worktree remove`.
- worktree-and-branch, clean → `worktree remove` then `branch -d`.
- `worktree remove` fails → `worktree-dirty`=force → `worktree remove --force`.
- `worktree remove` fails → `worktree-dirty`=abort → no force, `Changed:false`.
- `branch -d` fails → `branch-unmerged`=force-delete → `branch -D`.
- `branch -d` fails → `branch-unmerged`=keep → no `-D`, branch kept, success.
- detached (`Branch==""`) → `remove-scope` offers only `["worktree-only","abort"]`.
- abort at `remove-scope` → no git calls, `Changed:false`.
- guard: `Path` == primary worktree → error, no decision.
- guard: `Path` == current worktree (`TopLevel`) → error, no decision.

**git verbs** (`FakeRunner` argv assertions):
- `RemoveWorktree` with/without `--force`.
- `DeleteBranch` `-d` vs `-D`.

**TUI:**
- `d` starts `RemoveWorktree` with the selected row's `Path`/`Branch`.
- Full modal handshake: pick `worktree-and-branch`, op completes, reload.
- **Selection clamp:** delete the last worktree in the list; assert no
  out-of-range access and `sel[panelWorktrees]` is clamped.

**CLI:**
- flag→policy mapping (`--with-branch`, `--force`, both, neither).
- clean remove prints a summary.
- non-interactive + unanswered decision → error mentioning the missing flag.
- unknown `<path>` → error.

## Scope

One cohesive feature on a single branch off `main` (no A-style split). Builds
directly on the merged `internal/worktree`, `internal/git/worktree.go`,
`engine`, TUI Worktrees panel, and `gg worktree` CLI.

## Out of scope (YAGNI)

- Removing the worktree you're currently in by auto-switching elsewhere first
  (engine simply refuses).
- `git worktree prune` of stale registrations (separate concern).
- Multi-select / bulk delete.
- Parsing git stderr to pre-classify failures.
