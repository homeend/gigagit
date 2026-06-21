---
name: using-gg
description: Use when performing git operations (status, commit, pull, push, branch switch, stash, worktrees) in a repository where the gg CLI is available.
---

<!-- gg:using-gg:v22 -->

# Using gg (gigagit)

gg is a git client CLI built for very large repositories. When it is available
(`gg` on PATH, or a `./gg` binary in the repo), prefer it over raw git for the
operations below — its smart commands carry safety rails: automatic
stash/restore around switches, a never-drop-the-stash rule on conflicts, and
guards against removing the worktree you are standing in.

## Commands

- `gg status` — branch, upstream ahead/behind, changed files.
- `gg commit -m <msg> [-a] [--amend]` — commit (`-a` also stages tracked
  modifications; `--amend` rewrites the last commit, reusing its message when
  `-m` is omitted).
- `gg commit reword <commit> -m <msg>` — change a commit's message. HEAD is a
  cheap amend; an older commit replays its branch onto its own parent (in
  place, later commits preserved). Refuses the repository's root commit and a
  commit not on the current branch.
- `gg pull [<branch>] [--background] [--on-conflict rebase|merge|abort]` —
  smart pull; with `<branch>` + `--background` it fast-forwards that branch's
  ref without checking it out.
- `gg push` — push the current branch (sets upstream when missing).
- `gg switch <branch>` — switch branches, auto-stashing and restoring local
  changes; on a restore conflict the stash is preserved, never dropped.
- `gg checkout <remote>/<branch> [-s|--switch]` — check out a remote-tracking
  branch as a local tracking branch (fast-forward-safe: reuses an existing local
  branch only if it fast-forwards to the remote ref, and refuses a diverged
  one). `-s` also switches to it.
- `gg remote ls | fetch | prune` — `ls` lists remote-tracking branches (one
  `remote/branch` per line); `fetch` updates tracking refs for all remotes
  (`git fetch --all`); `prune` drops tracking refs for branches deleted upstream.
- `gg tag ls | create | rm | checkout | push` — `ls` lists tags newest-first
  (one name per line); `create [-m <msg>] <name> [<commit>]` creates a tag at
  `<commit>` (default HEAD): lightweight, or annotated when `-m` is given;
  `rm <name>` (alias `delete`) deletes a tag; `checkout [--branch <name>] <tag>`
  checks out a tag — detached, or onto a new branch created at the tag when
  `--branch` is given; `push <name> [<remote>]` pushes a tag to a remote (the
  remote defaults to the only configured one; specify it when there are several).
- `gg branch create <name> [<start-point>]` — create a branch (no switch);
  start point defaults to HEAD.
- `gg branch rename <old> <new>` — rename a local branch (`git branch -m`);
  refuses an existing target name.
- `gg branch delete [--force] <name>` — delete a branch; refuses the
  checked-out branch and branches checked out in a worktree. An unmerged
  branch is a `branch-unmerged` fork (`force-delete`/`keep`): pass `--force`
  to pre-answer it.
- `gg merge [--into <target>] [--on-conflict=keep|abort] <source>` — merge one
  branch into another (default target: the current branch; worktree-aware —
  merges in the worktree that has the target checked out, autostashes when it
  must switch). `--on-conflict=keep` leaves conflicts in the tree (exit 1),
  `--on-conflict=abort` restores the tree (exit 0); with neither and no TTY, a
  conflict exits 1 with the options on stderr.
- `gg rebase [--branch <b>] [--on-conflict=keep|abort] <newbase>` — replay a
  branch's commits onto `<newbase>` (default branch: the current one; `--branch`
  rebases another branch, switching to it). Worktree-aware — rebases in place,
  in the worktree that has the branch checked out (you stay put), or autostashes
  and switches. A conflict pauses the rebase: `--on-conflict=keep` leaves it
  paused for `git rebase --continue` (exit 1), `--on-conflict=abort` runs
  `git rebase --abort` (exit 0); with neither and no TTY, a conflict exits 1
  with the options on stderr.
- `gg rebase -i --plan <file> <newbase>` — **interactive** rebase from a plan
  file (a gg rebase-plan JSON: ordered `{sha, action: pick|reword|squash|drop,
  orig, new_msg}`); the TUI builds this plan interactively. Squash composes a
  combined message (target subject + each squashed commit's message
  line-by-line). The working tree is preserved across the rebase; conflicts
  answer to `--on-conflict`.
- `gg cherry-pick [--on-conflict=keep|abort] <commit>` — apply `<commit>` onto
  the current branch as a new commit. A dirty tree is autostashed and restored.
  A conflict: `--on-conflict=keep` leaves it paused for `git cherry-pick
  --continue` (exit 1), `--on-conflict=abort` runs `git cherry-pick --abort`
  (exit 0); with neither and no TTY, a conflict exits 1 with the options on
  stderr.
- `gg revert [--on-conflict=keep|abort] <commit>` — create a new commit on the
  current branch that undoes `<commit>`. A dirty tree is autostashed and
  restored. A conflict: `--on-conflict=keep` leaves it paused for `git revert
  --continue` (exit 1), `--on-conflict=abort` runs `git revert --abort` (exit 0);
  with neither and no TTY, a conflict exits 1 with the options on stderr.
  Reverting a merge commit is refused (it needs `-m <parent>`, out of scope).
- `gg reset [--soft|--mixed|--hard] [--force] <commit>` — move the current branch
  to `<commit>`. `--soft` keeps the changes staged, `--mixed` (the default) keeps
  them unstaged, `--hard` discards uncommitted tracked changes (untracked files
  survive; the commits reset past stay recoverable via `git reflog`). If
  `<commit>` is not on the current branch the reset is refused unless `--force`
  is given (a non-TTY run lists the options and exits 1).
- `gg stash [-m <msg>] [-u] [-- <paths>...]` — stash the working tree (or only
  the named paths; `-u` includes untracked files).
- `gg stash list` — list stashes (`stash@{N}: <subject>`, newest first).
- `gg stash apply [<ref>]` / `gg stash pop [<ref>]` / `gg stash drop [<ref>]` —
  apply (keep), pop (apply + drop), or drop a stash; `<ref>` defaults to the
  newest. A conflicting apply/pop exits non-zero and keeps the stash.
- `gg discard [--yes|-y] (--all | <path>...)` — throw away unstaged changes:
  tracked edits are reverted (staged hunks kept), untracked files deleted.
  Destructive, so `--yes` is required (or a y/N prompt on a TTY). `--all`
  discards everything unstaged and refuses while the repo is conflicted; named
  paths must appear in `gg status` and a conflicted path is rejected.
- `gg shelf add [--staged|--rev <commit>] [--bucket <name>] <path>...` /
  `gg shelf list [--bucket <name>]` / `gg shelf rm <entry>` /
  `gg shelf restore [--force] <entry> <dest>` — the **shelf**: a non-git,
  per-file content store of frozen copies that survive even permanent deletion
  of the source. `add` freezes the unstaged (default), `--staged`, or `--rev`
  version of each path and prints its entry id. `restore` writes a stored copy
  to a **required** `<dest>` as an unstaged change (`--force` to overwrite an
  existing differing file, else it refuses). Entries persist per-repo under the
  machine-local state dir; the default bucket is implicit. `list` shows each
  entry's id and a bookmark-style origin (`<container> / <state-or-commit> /
  <path>`).
- `gg bookmark add [--rev <commit>] [--staged] [--worktree <path>] <path>...` /
  `gg bookmark list` / `gg bookmark rm <id>` /
  `gg bookmark paste [--force] <id> <dest>` — **bookmarks**: a persistent registry
  of richly-addressed file references (vs the shelf's frozen copies). `add` stores
  a live pointer — default = this worktree's working file; `--staged` the index
  side; `--worktree <path>` another worktree; `--rev <commit>` a committed file
  (frozen by blob sha). `paste` resolves the bookmark's **current** bytes (live
  for working/index, the pinned blob for committed) and writes them to a
  **required** `<dest>` as an unstaged change (`--force` to overwrite). A bookmark
  to a working file reflects later edits; to freeze bytes, shelf instead.
- `gg undo` — undo the last commit, keeping its changes (ref-only soft reset).
- `gg worktree list` / `gg worktree add [<start-point>]` /
  `gg worktree add --branch <name>` /
  `gg worktree remove [--with-branch] [--force] <path>` — linked worktrees;
  `add` resolves branch/path templates from `.gg.toml` and may prompt on stdin
  for `<user:...>` fields; `add --branch` checks out the EXISTING branch in
  the new worktree (no new branch; refuses a branch already checked out).
- `gg repo list` / `gg repo switch <query>` — the known-repository registry
  (MRU); `switch` prints the path of the unique match.
- `gg inspect` — one-shot repo summary (scriptable health check).
- `gg init` — install/refresh this skill for detected AI agents.
- `--time-track <file>` (global; combine with any command) — append one JSON
  span per process start, git subprocess, and operation to `<file>` for
  performance analysis.

## The rule that matters for agents

gg never hangs waiting for input mid-operation. When an operation hits a fork
(diverged branch, dirty worktree, unmerged branch), it needs a decision:

- Interactive terminals get a prompt; **non-interactive runs fail with exit 1
  and print the decision and its options to stderr** instead of blocking.
- Pre-answer decisions with the matching flag: `--on-conflict` for pull
  divergence; `--with-branch` / `--force` for worktree removal; `--force`
  for unmerged branch deletion.
- On a non-zero exit, read stderr: it names the decision and the valid
  options; re-run with the matching flag.

Exit codes: 0 = success, 1 = operation failed or needs a decision,
2 = usage error.

## Shell following

Worktree and repo switches write the target directory to `--cwd-file <path>`
(human shells follow via `gg shell-init`). As an agent, just `cd` to the path
printed on stdout.
