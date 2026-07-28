---
name: using-gg
description: Use when performing git operations (status, commit, pull, push, branch switch, stash, worktrees) in a repository where the gg CLI is available.
---

<!-- gg:using-gg:v55 -->

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
- `gg review [--tool <name>] [--working] [<rev>|<A..B>]` — runs a configured
  AI review agent headless and prints its report to stdout (also persisted
  under the gg state dir). Flags must precede the positional (like `gg log
  -n`). No positional reviews the current branch's work; a single `<rev>`
  reviews just that commit's own change (`rev^..rev`); an `A..B` positional
  is used as a range; `--working` reviews uncommitted changes. `--tool`
  picks among configured `review` commands when more than one is set up.
  Exit 0 on a produced report, 1 on tool failure/empty report/no review tool
  configured, 2 on a usage error.
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
- `gg apply [--am | --working] <path>` — apply a patch file (the inverse of
  `gg commit export-patch`; round-trips it). Default = working-tree mode:
  lands the diff as unstaged changes, nothing committed; a hunk that
  doesn't apply cleanly falls back to a 3-way merge, and conflicts stay in
  the tree as markers (exit 1) for you to resolve and commit. `--am`
  recreates commits from a `git format-patch` mailbox (author/date/message
  preserved) and is atomic: a conflicting mailbox is rolled back completely
  (exit 1, nothing changed). `--am` on a plain diff is refused.
- `gg pull [<branch>] [--background] [--on-conflict rebase|merge|reset|abort]` —
  smart pull; with `<branch>` + `--background` it fast-forwards that branch's
  ref without checking it out. On a diverged current branch, `--on-conflict=reset`
  hard-resets it to the remote tip, discarding local commits and uncommitted
  changes.
- `gg push [--force | --force-with-lease] [--on-reject ...] [--map | --no-map] [<branch>]`
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
  a non-current `<branch>` offers only force/abort. `--map` adds a per-branch
  fetch-refspec mapping when the clone's refspec doesn't cover the pushed branch
  (single-branch/depth clones); `--no-map` declines; with neither, non-interactive
  runs skip.
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
- `gg versions [<branch>]` — list a branch's recorded pre-operation
  snapshots (taken automatically before merges, rebases, resets, amends,
  and branch deletion), newest first: `<id> <short-sha> <time> <subject>`.
- `gg versions restore [--discard] <branch> <id|latest>` — move the branch
  back to a recorded version (its own pre-restore state is snapshotted
  first). Restoring the current branch hard-resets; `--discard` answers the
  dirty-tree prompt. Also recreates a deleted branch. Exit 0 restored,
  1 failure/unknown id, 2 usage.
- `gg unlock [--yes]` — list git lock files (`.git/index.lock` and friends)
  stranded by a git process that was killed before it could clean up. While
  one exists every git command fails with "Another git process seems to be
  running in this repository". Without `--yes` nothing is removed: exit 0 when
  clean, **exit 1 when locks are present** (so it works as a precondition
  check), 2 usage. Pass `--yes` to remove them — but only once you are sure no
  other git is running, since deleting a live git's lock corrupts its write.
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
- `gg shelf cherry-pick [--patch] [--on-conflict=keep|abort] <entry-id>` —
  re-apply a **shelved commit** onto the current branch. While the original
  commit object still exists this is a live `git cherry-pick`
  (`--on-conflict` pre-answers a conflict: `keep` leaves conflict markers in
  the tree and exits 1, `abort` rolls back cleanly and exits 0). Once the commit is
  gc'd or the history rewritten, gg replays the patch snapshot frozen at
  shelve time (`git am --3way`, atomic — all-or-nothing); `--patch` forces
  that lane even while the commit exists. An entry shelved before patch
  support, or a merge commit, has no snapshot: the gc'd case then exits 1
  with a clear message. Exit 0 = commit created, or a clean
  `--on-conflict=abort`; 1 = failure or conflicts left; 2 = usage.
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
  (`<user:LABEL>`, `<seq:NAME:N>`, `<date>`/`<date:…>` (bare `<date>` = today as `yyyy-MM-dd`), `<parent-branch>`, `<repo>`,
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
- `gg init` — install/refresh this skill for detected AI agents. An agent gg
doesn't know can still get it: `gg init --to <path>` installs at a custom
location (a file receives a marker-delimited managed block, surrounding
content preserved; a directory receives `<dir>/using-gg/SKILL.md`) and
remembers the target, so `gg init --update` refreshes it too.
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

## Registering yourself as a gg tool

gg can call an external AI agent for three tasks: **resolving conflicts**
(the TUI conflict window's `t` picker), **generating commit messages** (the
commit popup's `ctrl+g`), and **reviewing changes** (`gg review` and the TUI
review rows). Each tool is a `[[tools.command]]` block in gg's config. The
easiest path is the human running Settings → External tools, which detects
installed agents and writes these blocks; write a block yourself only when
asked to, or for a tool the catalog doesn't know.

Where to write it — **default to the global config**
(`$XDG_CONFIG_HOME/gg/config.toml`, default `~/.config/gg/config.toml`); it
is always active and applies to every repo. A repo-level block is trickier:
per-repo settings live in ONE of two files, and gg reads whichever is
active — the machine-local private file
`$XDG_CONFIG_HOME/gg/projects/<repo-key>/config.toml` **if it exists**
(`<repo-key>` = the main worktree's absolute path with `/`, `\`, and `:`
replaced by `-`), else the committed `<repo>/.gg.toml`. Appending to
`.gg.toml` while the private file exists does nothing — check for the
private file first. Global and the active repo file concatenate, and a
repo block wins a `(category, name)` collision.

```toml
[[tools.command]]
category = "commit_message"   # conflict | commit_message | review
name     = "MyAgent"          # picker label; unique per category
mode     = "capture"          # capture = headless | terminal = takes the TTY
command  = '''myagent --do-the-task'''
# per_file = true             # conflict only: run once per conflicted file
# when_op  = "merge"          # conflict only: merge|rebase|cherry-pick|revert
```

A structurally invalid block (unknown category/mode, empty name/command,
`per_file` outside conflict, unknown `when_op`) is silently ignored at load.
The command body cannot contain `'''`. **First run is approval-gated**: gg
shows the human the fully resolved command (Run/Cancel), remembered per repo
keyed on the block's text — editing the block re-prompts.

Per-category contract (env vars are absolute paths; stdin is /dev/null;
capture commands run with the repo worktree as working directory; capture
runs also get `GG_TASK=commit_message` / `GG_TASK=review`, so one command
can branch on the task):

- **commit_message** (`mode = "capture"`): read `$GG_CONTEXT_FILE` (change
  summary + recent commit subjects) and `$GG_STAGED_DIFF` (full staged diff;
  replaced by a stat-only note when huge). Return the message EITHER as
  stdout — plain text, or Claude-style `--output-format json` (`.result`) —
  OR by writing it to `$GG_MESSAGE_FILE`; **non-empty file content wins over
  stdout** (use the file if your stdout is a status report, not the answer).
  Format: subject line, blank line, body. Do not run `git commit`.
- **review** (`mode = "capture"`): the `<range>` token in the command text
  resolves to the injection-safe range under review (e.g. `abc12..def34`;
  empty for a working-changes review). Read `$GG_CONTEXT_FILE` (range label
  + numstat) and `$GG_REVIEW_DIFF` (the full diff). Return a free-form
  markdown report via stdout or `$GG_MESSAGE_FILE` (same file-wins rule);
  gg persists it under the state dir and shows it in a viewer. Do not
  modify repository files.
- **conflict** (`mode = "terminal"` — gg suspends and hands you the real
  terminal in the worktree): a paused merge/rebase/cherry-pick/revert has
  conflicts. Env: `GG_OP`, `GG_SOURCE`, `GG_TARGET`, `GG_CONFLICTED_FILES`
  (space-joined), `GG_REPO`, and `GG_CONTEXT_FILE` (op header + one
  conflicted path per line). Resolve the files and stage them; do NOT
  continue or abort the operation unless asked — gg re-reads status when
  you exit and offers the continue step itself. With `per_file = true`
  (mergetool style) the command instead runs for one file with the
  `<local>`/`<base>`/`<remote>`/`<merged>` path tokens (also as `GG_LOCAL`
  etc.); editing `<merged>` offers mark-resolved on return.

Tokens usable in `command`: path tokens `<repo>` `<file>` `<local>` `<base>`
`<remote>` `<merged>` `<context-file>` (gg shell-quotes them), prose tokens
`<op>` `<source>` `<target>` `<conflicted-files>` `<range>` `<user:LABEL>`
(`<user:…>` prompts the human at run time). Plain `${GG_*}` env references
work too — the vars are always exported. `<bin>`/`<env:NAME>` are
wizard-only; in a hand-written block name the binary and env vars directly.

Worked examples (adapt the binary and prompt; `$GG_*` expand at run time):

```toml
[[tools.command]]
category = "commit_message"
name     = "MyAgent"
mode     = "capture"
command  = '''myagent run -q "Write a git commit message for the staged changes. Read the summary at $GG_CONTEXT_FILE and the full diff at $GG_STAGED_DIFF. Output ONLY the message: an imperative subject (max ~72 chars), a blank line, a short body."'''

[[tools.command]]
category = "review"
name     = "MyAgent"
mode     = "capture"
command  = '''myagent run -q "Review the change <range>. The full diff is at $GG_REVIEW_DIFF, a summary at $GG_CONTEXT_FILE. Write a markdown review (findings with severity, then a summary) into the file at $GG_MESSAGE_FILE (an absolute path outside the repository). Do not modify repository files."'''

[[tools.command]]
category = "conflict"
name     = "MyAgent"
mode     = "terminal"
command  = '''myagent "A git $GG_OP is paused with conflicts. The conflicted paths are listed in $GG_CONTEXT_FILE. Resolve the conflict markers and stage the results with git add. Do not continue or abort the operation."'''
```

If your tool refuses to write outside its project root, the phrase "an
absolute path outside the repository" in the `$GG_MESSAGE_FILE` instruction
is load-bearing — keep it.

Verify a registration headlessly with the review lane: `gg review --tool
MyAgent HEAD` — exit 0 with a report on stdout proves the block loaded,
the command resolved, and your output channel works (running the CLI
yourself is its own consent; the TUI's first-run approval popup still
guards `ctrl+g`/`t`). Plain `gg review` with a wrong/missing `--tool`
errors listing the configured names — a cheap existence check. The
commit_message and conflict lanes have no headless test path; they surface
in the TUI (`ctrl+g` chooser, conflict `t` picker, Settings → External
tools).

Once configured: commit messages come from `ctrl+g` in the commit popup,
reviews from `gg review [--tool <name>]` or the TUI review rows, conflict
tools from `t` in the conflict window.

## Interacting over MCP

Besides the CLI, gg ships an MCP server: `gg mcp` (stdio) serves the repo
it starts in — one server per repo, discovered from the working directory;
every reply carries `repo{common_dir, worktree}`. Register it from the
repo directory, e.g. for Claude Code: `claude mcp add gg -- gg mcp`.

Use the CLI for git operations — that is what it is for. Use MCP for what
only gg knows: `gg_ui_state` (the live TUI session — focused panel, cursor,
◉-marked commits, open compare, conflict/running-op state; `session: null`
when no TUI is running for the repo), bookmark and shelf listing/reading,
`gg_compare_trees`/`gg_compare_file` (diff any two versions, including
shelved-commit members), `gg_export` (copy a bookmark/shelf entry to a
directory), and two consent-gated mutations — `gg_cherry_pick` (re-apply a
shelved/bookmarked commit; falls back to the shelf's stored patch when the
original was gc'd) and `gg_write_to_worktree` (restore a stored file
version as an unstaged change). The mutating tools are annotated
destructive, so your MCP client prompts before running them.

## Shell following

Worktree and repo switches write the target directory to `--cwd-file <path>`
(human shells follow via `gg shell-init`). As an agent, just `cd` to the path
printed on stdout.
