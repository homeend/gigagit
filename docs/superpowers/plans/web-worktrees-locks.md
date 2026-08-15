# Task 2 — worktrees you can move, and locks you can clear

**Depends on task 0.** Read `README-web-parallel-tasks.md` first.

The browser can create and remove a worktree and nothing else: it cannot
rename one, cannot move one, and cannot cut one that keeps your current
changes. And when git leaves a stale `index.lock` behind, the browser shows
the failure with no way out — the TUI has a whole cleanup surface.

## Your files

| Server | Client |
|--------|--------|
| `internal/web/op_worktree_extra.go` (new) | `static/locks.js` (new) |
| `internal/web/locks.go` (new) | |
| `internal/web/opworktreeextra_test.go`, `locks_test.go` (new) | |

Worktree menu rows go in via `registerRows("worktree", …)`; the locks notice
mounts its own surface with `mountOverlay` (task 0). Do not edit `sidebar.js`,
`ophttp.go`, `server.go` or `index.html`.

## What already exists

- `engine.MoveWorktree{Path, Dest string}` — both absolute. It refuses the
  main worktree, refuses a destination inside the source, refuses when the
  parent is missing, and when the worktree is **locked** it parks a decision
  offering *unlock and move* or *abort*. gg follows the move if it is standing
  in that worktree.
- `engine.CreateWorktree{StartPoint, Branch, Path, Keep, PostCreateHook}` —
  the web only ever builds `CreateWorktreeForBranch` and (since the commit-menu
  work) plain `CreateWorktree` from a commit. `Keep` is the unused half:
  `KeepStaged` / `KeepUnstaged` land the new branch on the start point's
  **parent** with the commit's diff staged or unstaged in the new worktree.
  It refuses a root or merge commit for those modes, with a typed error.
- `engine.RemoveGitLocks{Paths []string}` — deletes lockfiles, and validates
  every path before removing anything: a path outside the repo's git dir is
  refused, so the wire cannot aim it elsewhere.
- `domain.Service.StaleLocks(ctx) ([]model.GitLock, error)` — a stat-level
  probe, no git invocation. This is what the notice should be built on.
- The TUI equivalents: `gg unlock` (CLI) and the TUI's lock notice.

## Work

1. **Rename a worktree.** Row: *rename worktree…*, prompt prefilled with the
   basename only (the TUI's `e`), destination resolved next to the current
   parent directory. Underneath it is `MoveWorktree`.
2. **Move a worktree.** Row: *move worktree…*, prompt prefilled with the full
   path. Same op, different prefill — keep them two rows, as the TUI does,
   because the two intentions are different.
3. **The locked case.** Both rows must let the engine's *unlock and move* /
   *abort* decision park in the modal. Do not pre-empt it client-side.
4. **Keep-modes when creating a worktree from a commit.** Extend the
   create-worktree lane you own with the two keep modes, offered only where
   they can work (not a root or merge commit — the engine refuses, and the row
   should not pretend otherwise). Wire value → `Keep` must be an allowlist, not
   an integer off the wire.
5. **Stale locks.** `GET /api/locks` → `StaleLocks`, and
   `POST /api/op {op:"remove-locks", paths:[…]}` → `RemoveGitLocks`. Show a
   notice bar when the list is non-empty, listing each lock with its age, with
   one button to clear them. Poll it where the client already refreshes rather
   than on a timer of its own.
6. Re-read `StaleLocks` after clearing and hide the notice when empty.

## Acceptance

- Go tests: move refuses the main worktree; move refuses a nested destination;
  a locked worktree's decision reaches the client (assert the parked decision
  in the SSE stream, the `opdecide` tests show the pattern); `/api/locks`
  lists a lock you create by touching `index.lock` and clearing removes it;
  a lock path outside the git dir is refused.
- Browser, control run first: the worktree menu gains both rows; renaming one
  in a fixture with two worktrees changes the sidebar row and the row's title
  attribute holds the new path; the lock notice appears after you `touch
  .git/index.lock`, and disappears after clicking clear.
- `./test.sh race` green. CHANGELOG bullet. `registerHelp` row.

## Notes

- Worktree paths are absolute and cross-platform: on Windows git reports `/`
  where `filepath` produces `\`. Compare with care and never string-compare a
  path you have not normalised — that bug has shipped here before.
- The served worktree is the one gg is standing in. Moving it means the server
  re-roots; make sure the client reloads rather than silently pointing at a
  path that no longer exists.
- A worktree whose `.git` file was written by the other environment (WSL vs
  Windows) cannot be read locally; the existing re-root path already explains
  that, so route failures through the same message rather than inventing one.
