# Fast-Forward Current Branch to a Commit — Design

**Date:** 2026-06-22
**Status:** Approved (brainstorm)

## Goal

When another branch is built on top of your current branch — so its commits are
**descendants** of your branch's tip — let the user advance the current branch
straight to one of those commits with no merge commit. This is git's
**fast-forward** (`git merge --ff-only <commit>`): it moves the current branch
ref forward and updates the worktree, and refuses if the target is not a
descendant.

Concretely: you are on `main`; `feat` is based on `main` and ahead of it. In the
Commits panel you select a commit on `feat` and choose **Fast-forward `main` to
here** — `main` jumps to that commit.

This is the *safe*, intent-revealing complement to the existing reset op (which
is destructive and unguarded as to direction). Fast-forward never rewrites
history, never discards committed work, and preserves uncommitted changes when
git can.

## Scope (settled in brainstorm)

- **Branch moved:** always the **current** (checked-out) branch.
- **Target:** a single commit selected in the **Commits** panel. (Not the
  Branches panel; not a branch tip shortcut.)
- **Surfaces:** TUI Commits `.`-menu action **and** a CLI command
  (`gg fast-forward <commit>`), per gg's engine→TUI→CLI parity convention.
- **No interactive forks / confirm.** Fast-forward is non-destructive, so the op
  has no `Decider` prompt.

## Architecture

A new engine `Operation` over a new thin git verb, wired to the TUI `.`-menu and
a CLI command — the standard `adding-features` path. The only novel piece is the
TUI menu **gating**, which is computed in-memory from the already-loaded commit
feed rather than with a git call.

### git verb — `internal/git/merge.go`

```go
// MergeFFOnly fast-forwards the branch checked out at dir ("" = this repo's own
// worktree) to commit, refusing (non-zero exit) if commit is not a descendant
// of HEAD. --no-edit keeps it non-interactive. One invocation.
func (r *Repo) MergeFFOnly(ctx context.Context, dir, commit string) error
```

Argv: `git merge --ff-only --no-edit <commit>` (with `-C dir` when `dir != ""`),
mirroring the existing `Merge` verb. `git merge --ff-only` is itself the
authority on fast-forward-ability — the engine guard below is for a friendly
message, not correctness.

### engine op — `internal/engine/fast_forward.go`

```go
type FastForward struct {
	Commit string // commit-ish to advance the current branch to
}
```

- `LockMode() = TreeWrite` (it updates the worktree), like reset/merge.
- `Name()` registered in `opname.go`.
- `Run(ctx, deps)`:
  1. Resolve the current branch. If detached HEAD (no branch) →
     error `"fast-forward needs a checked-out branch"`.
  2. Resolve the target to a full SHA (`deps.Repo.RevParse` or equivalent). If
     `target == HEAD` → `Result{Summary: "already up to date", Changed: false}`,
     no error (friendly no-op).
  3. Guard: `anc, _ := deps.Repo.IsAncestor(ctx, "HEAD", target)`. If `!anc` →
     error `"cannot fast-forward <branch>: <short> is not ahead of it"`.
  4. `deps.emit(Progress{Step: "fast-forwarding", Detail: branch})`, then
     `deps.Repo.MergeFFOnly(ctx, "", target)`.
  5. `Result{Summary: "fast-forwarded <branch> to <short>", Changed: true}`.

  No `decide` calls. The `GitOps` interface already exposes `IsAncestor`; add
  `MergeFFOnly` to it (mirrors how other verbs are surfaced to the engine).

### domain

Runs via `domain.Execute` like every other op (no new query needed for the op
itself). The TUI gating uses data already in the model — no new domain call.

### TUI — Commits `.`-menu row (`internal/tui/commit_scope.go`)

A new context row, appended where the other commit-row actions are built
(`appendCommitContextRows`):

- **Label:** `"Fast-forward <branch> to here"` (`<branch>` = current branch
  name; falls back to a generic label if detached, though the row is then
  hidden — see gating).
- **Availability:** `focus == panelCommits`, a commit is selected, ops idle,
  **and** a current branch exists (not detached).
- **`run`:** `m.startOp(engine.FastForward{Commit: selectedHash})`. A normal
  `opFinishedMsg` refresh follows (branch tip + worktree changed).

**Gating (in-memory, no git call).** Determine whether the selected commit is a
descendant of the current branch tip from the loaded feed:

- Find the **tip hash**: the loaded `model.Commit` whose `Refs` contains a ref
  with `Head == true` (the code already keys on `r.Head` in `commit_ident.go`).
- Build a transient `map[string]int` (hash → feed index) over `m.commits` once.
- Walk: from the selected commit, follow `Parents` pointers; prune any parent
  with `UnixTime < tipTime` (a descendant of the tip is newer-or-equal, so older
  commits can't lead back to it). Stop when the tip hash is reached (→
  **descendant**, show the row) or the frontier is exhausted / leaves the loaded
  set (→ **inconclusive**).
- **Show the row** when the walk concludes *descendant*, OR when it is
  *inconclusive* (the chain ran past the windowed/paged feed — rare, since both
  the tip and a slightly-ahead child commit sit near the top). **Hide** it only
  when the walk conclusively shows the commit is **not** a descendant (it's the
  tip itself, an ancestor/behind, or on a divergent line within the loaded set).

  This keeps the menu clean in the common case with zero git calls, and never
  wrongly hides a valid action at the window edge — the op's `IsAncestor` guard
  is the correctness backstop for the inconclusive case.

  The walk lives in a small pure helper (e.g. `feedDescendant(commits, byHash,
  selHash, tipHash, tipTime) (descendant, conclusive bool)`) so it is unit-
  testable without a model.

### CLI — `gg fast-forward <commit>` (`internal/cli`)

- One positional arg: the commit-ish. No flags, no forks → the `cliDecider` is
  never consulted.
- Register in **both** the `commands` map **and** the dispatch switch (the known
  CLI-routing gotcha: a missing `commands`-map entry makes the built binary say
  "unknown command" even though `cli.Run` works).
- Resolve to `engine.FastForward{Commit: arg}` and run via `domain.Execute`.
- Print the `Result.Summary` (or the friendly "already up to date").

### agentskill

Add a `gg fast-forward` entry to `internal/agentskill/using-gg.md`, bump
`agentskill.Version`, and run `gg init --update` (else `TestDogfoodSkillCopyInSync`
fails).

## Error / edge handling

| Situation | Behavior |
|-----------|----------|
| Detached HEAD | Op errors `"fast-forward needs a checked-out branch"`; TUI row hidden (no current branch). |
| Target == current tip | `"already up to date"`, no error, no change. |
| Target not a descendant (behind / divergent) | Op errors `"cannot fast-forward <branch>: <short> is not ahead of it"`; surfaced as the TUI status notice / CLI stderr. |
| Uncommitted changes that conflict with the ff | `git merge --ff-only` aborts and changes nothing; its error is surfaced verbatim. (Non-conflicting local changes are preserved — git's normal ff behavior.) |
| Current branch checked out in another worktree | N/A — we always operate on *this* worktree's HEAD. |

## Testing (TDD, real `git`)

- **git verb** (`merge_test.go`): real repo where `feat` is ahead of `main`;
  `MergeFFOnly("", featTip)` advances `main` and updates the worktree; a
  non-descendant target returns an error and leaves the ref unchanged.
- **engine op** (`fast_forward_test.go`): descendant → branch tip + worktree
  advance, `Changed:true`; non-descendant → error, ref unchanged; detached HEAD
  → error; `target == HEAD` → friendly no-op (`Changed:false`).
- **feed-walk helper** (pure unit test): tip reached → `(true,true)`; ancestor /
  divergent within the loaded set → `(false,true)`; chain leaves the loaded set
  → `(_,false)`; `UnixTime` pruning stops the walk early.
- **TUI** (`commit_scope` test): a loaded model with a child branch ahead — the
  row is present on the ahead commit and absent on the tip / a behind commit;
  selecting it starts `engine.FastForward`.
- **CLI**: argv/registration (`TestEverySwitchCaseIsRegistered` stays green) +
  an e2e scenario under `e2e/scenarios/` that builds `main` + an ahead `feat`,
  runs `gg fast-forward <featTip>`, and asserts `main` now points at it.

## Process

Work in the worktree `.claude/worktrees/fast-forward` (branch
`feat/fast-forward` off `main`); the human merges. Follow the `adding-features`
checklist for the engine→TUI→CLI wiring. Update `CHANGELOG.md`, `README.md` (new
`.`-menu action + CLI command), and the agentskill as noted.
