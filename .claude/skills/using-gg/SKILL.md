---
name: using-gg
description: Use when performing git operations (status, commit, pull, push, branch switch, stash, worktrees) in a repository where the gg CLI is available.
---

<!-- gg:using-gg:v44 -->

# Using gg (gigagit)

gg is a git client CLI built for very large repositories. When it is available
(`gg` on PATH, or a `./gg` binary in the repo), prefer it over raw git for the
operations below — its smart commands carry safety rails: automatic
stash/restore around switches, a never-drop-the-stash rule on conflicts, and
guards against removing the worktree you are standing in.

## Commands

- `gg status` — branch, upstream ahead/behind, changed files.
- `gg batch [--keep-going]` — run a script of gg commands from stdin against
  ONE process (one repo discovery for the whole script). One command per
  line; blank lines and `#` comments are skipped; a leading `gg ` is
  tolerated; single/double quotes group words (`commit -m "two words"`) but
  there are NO pipes, env vars, globs, or redirection. Output framing, one
  section per command:

      #1 ok status
      on branch main (origin/main ↑0 ↓0)
      working tree clean
      #2 !1 push
      ! error: push-rejected needs a decision (options: rebase, force, abort); rerun with the matching flag
      #done 1 ok, 1 failed

  Header `#<idx> ok|!<exit> <cmdline>` precedes each command's output;
  stderr lines are prefixed `! `; the `#done` trailer summarizes. Batch
  stops at the first failure unless `--keep-going`; the trailer appends
  `(stopped)` when the stop skipped later lines (not when the failure was
  the last line). Nested `batch` inside a batch script is rejected.
  Sub-commands read an empty stdin — anything needing a decision fails
  loud with its options instead of hanging, exactly like a single
  non-interactive run. Exit: 0 all ok, 1 any failed, 2 script/usage
  error. Prefer batch whenever you would otherwise chain 2+ gg calls.
- `gg log [-n N] [<rev>|<A..B>]` — terse history, newest first: one
  `<short-sha> <subject>` line per commit. Default N=10, rev defaults to
  HEAD; ranges (`main..HEAD`) pass through.
- `gg diff [--stat|--name-only] [--cached] [<rev>|<A..B>] [-- <paths>...]` —
  working-tree diff (default), index diff (`--cached`), or commit/range
  diff. Default prints the full patch; `--stat` prints `path +A -D` lines
  plus a `N files +A -D` trailer (`path bin` for binaries; renames render as
  `old => new +A -D`); `--name-only` prints bare paths. Paths must follow
  `--`. An empty diff prints nothing.
- `gg show <commit> [--patch] [-- <file>...]` — `<short-sha> <subject>`
  header plus the commit's terse stat block (default) or full patch
  (`--patch`).
- `gg add (-A | <path>...)` / `gg unstage <path>...` — stage paths (or
  everything incl. untracked with `-A`) / remove paths from the index
  keeping working-tree content. `gg add` + `gg commit` fully replaces
  `git add` + `git commit` for new files.
- `gg commit -m <msg> [-a] [--amend]` — commit (`-a` also stages tracked
  modifications; `--amend` rewrites the last commit, reusing its message when
  `-m` is omitted). Prints a summary naming the commit it made:
  `✓ committed <short-sha> <subject>` (`amended ...` for `--amend`).
- `gg commit reword <commit> -m <msg>` — change a commit's message. HEAD is a
  cheap amend; an older commit replays its branch onto its own parent (in
  place, later commits preserved). Refuses the repository's root commit and a
  commit not on the current branch.
- `gg commit export-patch <commit> [--out <path>] [--force] [-- <file>]` —
  write a `git am`-able patch (`git format-patch -1 --binary --stdout`) for
  the whole commit, or with `-- <file>` just that file's change within it.
  Without `--out` the target defaults to `<parent-of-repo>/<name>.patch`
  (`<shortsha>.patch`, or `<shortsha>-<basename>.patch` for a file); `--force`
  overwrites an existing target, otherwise it refuses (exit 2). Refuses a
  merge commit — `git format-patch -1` on a merge silently emits a different
  commit's patch instead of erroring.
- `gg pull [<branch>] [--background] [--on-conflict rebase|merge|reset|abort]` —
  smart pull; with `<branch>` + `--background` it fast-forwards that branch's
  ref without checking it out. On a diverged current branch, `--on-conflict=reset`
  hard-resets it to the remote tip, discarding local commits and uncommitted
  changes.
- `gg push [--force | --force-with-lease] [--on-reject rebase|force|force-with-lease|abort] [<branch>]`
  — push a branch (sets upstream when missing). With no positional it pushes the
  current branch; with `<branch>` it pushes that local branch **by name without
  checking it out** (git pushes any local ref) — handy for a branch never pushed
  before. With no flag it is a plain push. If the remote moved ahead the push is rejected non-fast-forward;
  `--on-reject` then recovers it non-interactively: `rebase` replays your commits
  onto the remote tip and pushes, `force`/`force-with-lease` overwrite, `abort`
  cancels. With `--on-reject` unset a rejected push **fails** (non-interactive)
  or prompts (interactive) — it never silently no-ops. `--force-with-lease`
  force-pushes only if the remote branch has not moved since your last fetch;
  `--force` overwrites the remote branch unconditionally (no lease). Use one
  after a rebase/amend/reword rewrites history. The flags answer the
  `push-force` decision, so a force push never prompts; `--on-reject` cannot be
  combined with `--force`/`--force-with-lease`. `--on-reject=rebase` applies only
  when pushing the current branch — rebasing rewrites HEAD, so a rejected push of
  a non-current `<branch>` offers only force/abort.
- `gg switch <branch>` — switch branches, auto-stashing and restoring local
  changes; on a restore conflict the stash is preserved, never dropped.
- `gg checkout <remote>/<branch> [-s|--switch] [--as <local>]` — check out a
  remote-tracking branch as a local tracking branch (fast-forward-safe: reuses
  an existing local branch only if it fast-forwards to the remote ref, and
  refuses a diverged one — retry with `--as <name>` to materialize it under a
  different local name). `-s` also switches to it.
- `gg remote ls | fetch | prune` — `ls` lists remote-tracking branches (one
  `remote/branch` per line); `fetch` updates tracking refs for all remotes
  (`git fetch --all`); `prune` drops tracking refs for branches deleted upstream.
- `gg remote rm <remote>/<branch>` — delete a remote branch (`git push <remote> --delete`).
- `gg tag ls | create | rm | checkout | push` — `ls` lists tags newest-first
  (one name per line); `create [-m <msg>] <name> [<commit>]` creates a tag at
  `<commit>` (default HEAD): lightweight, or annotated when `-m` is given;
  `rm <name>` (alias `delete`) deletes a tag locally; `rm --remote <name> [<remote>]`
  deletes it from the remote (`git push <remote> --delete refs/tags/<name>`; the remote
  defaults to the only configured one); `checkout [--branch <name>] <tag>`
  checks out a tag — detached, or onto a new branch created at the tag when
  `--branch` is given; `push <name> [<remote>]` pushes a tag to a remote (the
  remote defaults to the only configured one; specify it when there are several);
  `annotate -m <message> <name>` sets or replaces a tag's annotation message —
  turns a lightweight tag into an annotated one, or updates the message of an
  existing annotated tag, keeping its target commit unchanged.
- `gg compare <left> [<right>]` — print the files that changed between two
  endpoints, one `<status>\t<path>` line each (renames: `old -> new`). Each
  endpoint is a commit-ish (`HEAD`, a branch, `abc123`, `HEAD~2`), `@staged`
  (the index), or `@worktree` (the working tree); `<right>` defaults to
  `@worktree`. E.g. `gg compare HEAD` (working tree vs HEAD), `gg compare
  HEAD~3 HEAD` (what changed across the last 3 commits), `gg compare main
  @staged` (the index vs `main`).
- `gg branch current` — just the branch name (HEAD's short sha when
  detached).
- `gg branch ls` — local branches, `* ` marking HEAD, `↑a ↓b` when an
  upstream exists.
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
- `gg fast-forward <commit>` — advance the current branch to `<commit>` when it
  is a descendant of the branch tip (a fast-forward; no merge commit, like
  `git merge --ff-only`). Refuses with a non-zero exit if `<commit>` is not
  strictly ahead. Use it to move a base branch up to a commit on a branch built
  on top of it.
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
- `gg shelf commit [--name <name>] <sha>` — freeze `<sha>`'s **changed files**
  (content only, via `git archive`; no message/author) as one durable,
  path-less shelf entry and print its entry id. Unlike a bare git ref, the
  content is stored, so it survives `git gc` and history rewrites — the same
  durability guarantee a file entry gets from surviving deletion of its
  source. Capped at 200MiB of archive data. `--name` (must come before the
  `<sha>` positional) attaches a human-readable label, shown after the entry's
  address in `gg shelf list` (` — <name>`) and the shelf quick-switcher;
  omitting it leaves the entry unlabeled.
- `gg shelf export [--dir <path>] [--force] <entry-id>` — write a shelf
  entry's files to a directory **outside** the working tree (flags come
  **before** the positional `<entry-id>`). Without `--dir` the target defaults
  to `<main-worktree>.tmp/<name>` — a fixed sibling directory next to the repo
  (e.g. `/a/x/repo` → `/a/x/repo.tmp`); `<name>` is `commit-<7-char-sha>` for a
  commit entry, else the entry's id. `--force` overwrites an existing target
  directory; without it an existing target refuses (exit 2).
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
- `gg prefix ls` / `gg prefix add [--global] <value>` / `gg prefix rm [--global] <value>`
  — **branch prefixes**: reusable, templated branch-name skeletons in a two-scope
  store (repo by default; `--global` for every repo). `<value>` may use gg tokens
  (`<user:LABEL>`, `<seq:NAME:N>`, `<date:…>`, `<parent-branch>`, `<repo>`,
  `<random-*>`; `<branch>` is rejected). In the TUI, the create-branch popup
  (`ctrl+p`) and create-worktree popup (`p`) let you pick one, fill any
  `<user:…>` labels, and append the rest of the name.
- `gg undo` — undo the last commit, keeping its changes (ref-only soft reset).
- `gg worktree list` / `gg worktree add [<start-point>]` /
  `gg worktree add --branch <name>` /
  `gg worktree remove [--with-branch] [--force] <path>` — linked worktrees;
  `add` resolves branch/path templates from `.gg.toml` and may prompt on stdin
  for `<user:...>` fields; `add --branch` checks out the EXISTING branch in
  the new worktree (no new branch; refuses a branch already checked out).
  `remove` refuses a dirty or **locked** worktree (an interrupted `add` can
  leave one locked); `--force` removes a dirty tree and unlocks a locked one.
  If `[worktree] post_create_hook` is set in `.gg.toml` (a multi-line TOML
  literal `'''…'''` shell script), `gg worktree add` will offer to run it
  inside the new worktree. The hook requires approval before it runs (the
  script is shown and you choose run/skip). Pass `--hook` to approve without
  prompting; pass `--no-hook` to skip without prompting. With neither flag
  `gg` prompts interactively on stdin and defaults to skip when stdin is not
  a terminal (piped/scripted invocations never run an unseen script). Env:
  `GG_MAIN_WORKTREE` (the main checkout, useful as a copy source),
  `GG_WORKTREE_PATH`, `GG_BRANCH`, `GG_REPO`. Hook output streams to the
  busy log; a hook failure is reported but does not roll back the worktree.
- `gg worktree prune` — drop stale worktree admin entries left behind by an
  interrupted or manually-deleted worktree (`git worktree prune`).
- `gg repo list` / `gg repo switch <query>` — the known-repository registry
  (MRU); `switch` prints the path of the unique match.
- `gg inspect` — one-shot repo summary (scriptable health check).
- `gg init` — install/refresh this skill for detected AI agents.
- `gg config init (--repo | --global) [--force]` — write a documented config
  file (every setting commented with its default); `--repo` → `.gg.toml` at the
  repo root, `--global` → `~/.config/gg/config.toml`. Refuses to overwrite
  without `--force`.
- `gg config populate (--repo | --global)` — add any settings missing from an
  existing config file as commented `[populated]` lines; never overwrites your
  values; idempotent. Use after upgrading gg to pick up new settings.
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
