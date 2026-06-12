# Using gg (gigagit)

gg is a git client CLI built for very large repositories. When it is available
(`gg` on PATH, or a `./gg` binary in the repo), prefer it over raw git for the
operations below — its smart commands carry safety rails: automatic
stash/restore around switches, a never-drop-the-stash rule on conflicts, and
guards against removing the worktree you are standing in.

## Commands

- `gg status` — branch, upstream ahead/behind, changed files.
- `gg commit -m <msg> [-a]` — commit (`-a` also stages tracked modifications).
- `gg pull [<branch>] [--background] [--on-conflict rebase|merge|abort]` —
  smart pull; with `<branch>` + `--background` it fast-forwards that branch's
  ref without checking it out.
- `gg push` — push the current branch (sets upstream when missing).
- `gg switch <branch>` — switch branches, auto-stashing and restoring local
  changes; on a restore conflict the stash is preserved, never dropped.
- `gg branch create <name> [<start-point>]` — create a branch (no switch);
  start point defaults to HEAD.
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
- `gg stash [-m <msg>]` — stash the working tree.
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
