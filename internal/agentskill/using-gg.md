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

### Review notes

```bash
gg diff --hunks [--json] [--cached] [<commit>] [-- <paths>...]   # numbered git @@ hunks per file
gg note add   --file <path> (--hunk N | --new-line N | --old-line N) [--cached | --rev <c>] \
              --summary "…" [--rationale "…"] [--author <name>] [--source user|agent] [--json]
gg note reply [<repo-link>] <note-id> --summary "…" [--json]
gg note apply [<repo-link>] --stdin [--cached | --rev <c>] [--author <name>] [--json]   # agent-context v1 or a comments batch
gg note list  [<link> | --file <path>] [--type user|agent|all] [--cached | --rev <c>] [--json]
gg note rm    [<repo-link>] <note-id>
gg note clear [<link>] (--file <path> | --all) [--type user|agent|all] --yes
gg review --notes [--tool <name>] [--working] [<rev>|<A..B>]     # also import the tool's anchored notes
gg skill path [review|using-gg]                                  # print the bundled skill's path
```

Notes are machine-local review remarks anchored to a line range on one side of
one file; the user reads them inline in `gg` and `gg web`. A note targets ONE
diff: no flag = the unstaged working tree, `--cached` = the staged diff,
`--rev <commit>` = that commit's own change (a range is refused). Line numbers
are 1-based. Full guidance: `gg skill path` (the reviewing-with-gg skill).

Three gotchas worth knowing up front (the reviewing-with-gg skill covers
them in full): a path with BOTH a staged and an unstaged note needs
`gg note list --file <path>` with `--cached` (or without) to resolve each
against its own diff — a bare `note list` can show a misleading verdict; a
batch error's `item N` is 0-based; `gg note clear --all` removes this
checkout's own working-tree notes plus EVERY commit note in the store
(commit notes are not worktree-scoped).

- `gg session status` — is a gg TUI or web page open on this worktree? Exit 1
  when none is.
- `gg session navigate --file <path> (--hunk N | --new-line N | --old-line N)
  [--cached | --rev <sha>]` — put the open window on that line; `--rev <sha>`
  alone reveals a commit, `--next-comment` / `--prev-comment` step the open
  diff. Add `--no-wait` to skip the 2s wait for the window's answer.
- `gg session reload [notes|status|all]`, `gg session focus <panel>`,
  `gg session highlight add|clear` — refresh, switch panel, or paint an
  attention band. See the `reviewing-with-gg` skill for when to use them.

### gg links

A `gg://` link names one place in one repository — a file, a line on one side
of one diff, a hunk, or a commit — in a form a human can paste into a chat and
you can hand straight back to gg:

```text
gg://<repo>/<path>[@<target>][:<line>]     <target> = a full/short sha, "staged", or absent = the working tree
gg://<repo>/<path>[@<target>]#<hunk>       hunk numbers are `gg diff --hunks`'s
gg://<repo>/<path>@<sha>:old:<n>           the old side of that diff
gg://<repo>@<sha>                          a commit, no file
gg://<repo>@ref:<branch|tag>               a branch or tag TIP: the whole tree there
gg://<repo>@<a>..<b>                       a CHANGE-SET: only what differs between a and b
gg://<repo>@<target>...<source>            a merge preview: the Previews tab entry
gg://<repo>/<path>@<target>...<source>[:<line>]   a file (or new-side line) in that preview
gg://<repo>/<path>@<target>...<source>#<hunk>     a hunk of that preview's patch
gg:///abs/checkout/path/file.go:12         a repo with no remote: its absolute path
gg://<repo>@<sha>?bookmark=<id>            a trailing ?<kind>=<id> hint: bookmark | shelf | stash
```

`@ref:<name>` keeps the NAME on purpose: it addresses the branch, not
whichever commit it sits on today. `@<a>..<b>` is git's two-dot range and is
the one link form that names a SET OF FILES rather than a whole tree — each
half may be a sha or a refname. The `?<hint>` suffix records which UI surface
a link was copied from; `gg compare` ignores it, so a bookmarked commit and
the same commit off the log are one endpoint. The only exception is a
`?shelf=` hint that is the sole surviving source of the BYTES: a link with no
address at all (`gg://<repo>?shelf=<id>`, a shelved working-tree file) reads
the shelved blob, and `gg://<repo>@<sha>?shelf=<id>` falls back to the frozen
tar once that sha has been gc'd. Because `?` is the hint separator, a path, a
checkout path or a ref name containing `?` cannot appear in a link at all —
`gg link` refuses to print one rather than emit something that reparses
differently.
`gg link resolve` takes both: a `@ref:` link answers with `ref <name>` plus
the tip it resolves to HERE, a `@a..b` link with `pair <a>..<b>`, and
`--json` carries `ref` / `pair_a` + `pair_b` beside the address fields. The
verbs that CONSUME a link are stricter, and differ from each other:

| link shape | `gg diff`, `gg open`, `gg session navigate` | `gg show`, every `gg note` verb, `gg session highlight` |
|---|---|---|
| `@ref:<name>` — a whole tree | takes it | takes it |
| `@<a>..<b>` — a change-set | takes it | **refuses it, exit 2** |

A change-set's only single commit is its newer end, and anchoring a note or a
`gg show` there would silently widen a bounded set of files into a whole
tree — so those verbs refuse rather than guess. `gg compare` evaluates either
shape directly.

A preview link spells the branch PAIR, never a sha: the names travel between
machines, the machine-local preview id does not. Every verb that takes a link
treats a preview link exactly as `--preview` — `gg diff`, `gg note
add|list|apply|clear`, `gg session navigate`, `gg session highlight add`,
`gg link resolve`. `gg show` refuses one (use `gg diff`), and the preview's old
side is the merge base, so `:old:` is refused at parse time; a `#<hunk>`
naming a delete-only hunk (no new side) is instead refused at RUN time, on
`gg note add`, `gg session navigate` and `gg session highlight add`. Build one
with `gg link --preview <id|label|<target>...<source>> [<path>[:<line>]]`.

`gg open <link>` shows it to the user in their gg: a link with no `:<line>`
opens the file and leaves the cursor alone (the plain `gg link <path>` form
is exactly this, so a link gg itself printed is always openable). It steers
whatever gg session is live in the link's checkout (`--no-wait` skips waiting for that
session's answer), or starts the TUI there positioned on the link; a bare
repository link (`gg://<repo>`) just opens the TUI in that checkout. `--web`
names the browser instead: a live `gg web` page is steered (only the page),
otherwise `gg web` is started in that checkout with the browser opened,
landing on the link — and like the TUI launch it then RUNS IN THE FOREGROUND
until the user stops it, so do not wait on its exit (steer a page the user
already has open, or start it detached). Refused inside `gg batch` (exit 2 —
unlike every other batch line it might launch the TUI or a server). Use it
instead of launching `gg` yourself. (A human can also paste any link into the
TUI's `#` prompt.)

`<repo>` is the repository name of the repo's remote (`gigagit`), resolved
through gg's machine-local repository history — so a link made on one checkout
finds the right one here.

- `gg link [<path>[:<line>]] [--cached | --rev <commit> | --preview <id> |
  --ref <branch|tag> | --pair <a>..<b>] [--bookmark <id> | --shelf <id>]` —
  print the link for a place in the current repo. Those five target flags name
  the same thing, so pass at most one (exit 2 otherwise); `--bookmark` and
  `--shelf` attach the landing hint and are mutually exclusive. `--pair`
  resolves both halves to FULL shas so the link travels and still means the
  same thing tomorrow; `--ref` keeps the name. E.g.:

  ```bash
  gg link --pair HEAD~3..HEAD        # gg://repo@<sha40>..<sha40> — the last 3 commits' change-set
  gg link --ref main                 # gg://repo@ref:main — the branch tip, as a name
  gg link --ref main --bookmark b1   # …with the hint that names where it was copied from
  ```

  `gg link resolve <link> [--json]` says which checkout it names here and the
  address inside it; exit 1 when the repository is unknown or ambiguous,
  exit 2 when the link itself is malformed.
- A STASH is linked as the change-set it holds: `gg://<repo>@<parent>..<stash-sha>`
  (both full shas — never `stash@{N}`, which is renumbered by every push and
  drop). For a stash made with untracked files (`git stash -u`) the set
  INCLUDES those files, as additions, although `git diff <parent> <stash>`
  does not show them: the link means "what was stashed". `gg links` describes
  such a row as `stash: <subject>`.
- `gg links [--json]` — the links copied in this repository, newest first,
  one `<desc>\t<link>` row per line. Every "copy gg link" action records here:
  `gg link`, `gg compare` on a link argument, the browser's own copy rows, and
  every link copied in the TUI (Copy link on a file, commit, branch, tag,
  preview or stash row; `L` in the bookmark and shelf switchers).
  **Use it to pick up a place the user just copied instead of asking them to
  paste it again.** Twenty rows are kept; re-copying a link moves it to the
  top rather than adding a second row. Nothing copied yet prints nothing and
  exits 0.
- **A link the user pastes is enough.** Pass it as the FIRST positional to
  `gg diff <link>`, `gg show <link>`, every `gg note` verb (`gg note add
  <link> --summary "…"`, `gg note list <link>`, `gg note reply <repo-link>
  <id> …`), `gg session navigate <link>` and
  `gg session highlight add <link>[-<end>]`. Each verb's link replaces the
  flags that name the same thing, and passing both is a usage error (exit 2),
  never a silent override:
  - `gg diff <link>` / `gg show <link>` — replaces `--cached`, the `<rev>`
    positional and `-- <paths>`. (`gg diff` uses the link's file and target
    only: to see the hunk a `#<hunk>` link names, add `--hunks`. `gg show`
    needs a link to a COMMIT and exits 2 otherwise.)
  - `gg note add <link>` — replaces `--file`, `--cached`, `--rev`, `--hunk`,
    `--new-line` and `--old-line`; the link must carry `:<line>` or `#<hunk>`.
  - `gg note list <link>` — replaces `--file`, `--cached` and `--rev`; the
    link's own line or hunk is ignored (it lists that FILE's threads). A BARE
    preview link (no path) instead lists every path the preview covers — the
    same as `--preview <P>` with no `--file`.
  - `gg note reply <repo-link> <id> …` / `gg note rm <repo-link> <id>` — note
    ids are per REPOSITORY, so from another directory put the repository's
    link first (`gg://<repo>` or `gg:///abs/path`, no file or `@target`
    part): it only picks the checkout whose store holds the id. A bare id the
    current repository does not hold exits 1 and says which store it searched.
  - `gg note clear <link>` — a file link replaces `--file`, `--cached` and
    `--rev`; a repository link picks the checkout and `--file`/`--all` apply
    as usual.
  - `gg note apply <repo-link> --stdin` — the link's `@staged`/`@<sha>`
    replaces `--cached`/`--rev`; a link carrying a file is refused, since each
    batch item names its own path.
  - `gg session navigate <link>` — the same six, plus `--next-comment` and
    `--prev-comment`.
  - `gg session highlight add <link>[-<end>]` — replaces `--file`, `--cached`,
    `--rev`, `--start` and `--side`; `--end` is still yours to pass (but not
    together with the link's own `-<end>`, and never on a `#<hunk>` link,
    which already names a range).
- The verb runs against the checkout the link names even when your working
  directory is somewhere else — including posting into that worktree's session.
- **Quote a link that carries `#<hunk>`**: `#` starts a shell comment, so
  `gg diff gg://r/a.go#3` silently loses the hunk. gg applies no heuristic.
- Read `gg session status` (or `gg_ui_state`'s `cursor.link`) to see WHERE THE
  USER IS LOOKING right now, as a link you can pass to any of the verbs above.

- `gg add [-f] (-A | <path>...)` / `gg unstage <path>...` — stage paths (or
  everything incl. untracked with `-A`) / remove paths from the index
  keeping working-tree content. `gg add` + `gg commit` fully replaces
  `git add` + `git commit` for new files. When git refuses a path because
  .gitignore excludes it, the `stage.ignored` decision offers
  `force-add`/`abort`; `-f` pre-answers it with `force-add` (git add -f), so
  a non-interactive add of an ignored path needs `-f` or it fails with git's
  refusal.
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
- `gg pull [<branch>] [--background] [--on-conflict rebase|merge|reset|abort] [--on-stale-mapping remove|abort]` —
  smart pull; with `<branch>` + `--background` it fast-forwards that branch's
  ref without checking it out. On a diverged current branch, `--on-conflict=reset`
  hard-resets it to the remote tip, discarding local commits and uncommitted
  changes. If a per-branch fetch refspec references a branch deleted on the
  remote, every fetch exits 128 and blocks the pull; `--on-stale-mapping=remove`
  answers the `fetch_mapping.stale` fork by removing the stale mapping(s) (and
  their dangling tracking refs) and retrying. Without the flag a piped run
  fails with the original fetch error (config is never mutated unseen).
  After a successful pull, if the branch's own recorded change set (against
  what it pulled) shows a path the pull itself added or resurrected (a
  status flip such as `M` → `A`), gg prints `! this operation changed
  <branch>'s change set:` followed by one `<status> <path>` line per
  finding; a path that only dropped out (absorbed upstream) is noted
  quietly underneath as `(absorbed upstream: <status> <path>)`. Silent when
  nothing changed, or when no version was recorded to compare against.
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
  @staged` (the index vs `main`). Rows are sorted by path, and **the order of
  the two endpoints is free**: `gg compare @worktree main` compares just as
  `gg compare main @worktree` does, with the statuses turned round (what reads
  `A` forward reads `D` reversed).
- `gg compare [--patch] <spec> [<spec>]` — specs may also be `bookmark:<id>`
  or `shelf:<id>` (a stored commit entry; find ids via `gg bookmark list` /
  `gg shelf list`). While the entry's commit exists this is a live tree
  compare; a gc'd shelved commit falls back to its frozen snapshot (noted on
  stderr, scoped to the files that commit changed) — and a frozen entry
  compares against `@staged`/`@worktree` too, which answers "is my shelved work
  already in my tree?". `--patch` prints unified diffs instead of the file
  list — the patch of EXACTLY the rows the listing shows, read left → right as
  typed, for every comparison the listing answers: a link with a `/<path>`
  renders that one file, an `@<a>..<b>` change-set its members, a reversed
  pair (`@worktree` then a commit) the same diff the other way round. (A file
  set renders one file at a time, so a very large one is slower than two whole
  endpoints.)
- **Either side of `gg compare` may be a `gg://` link**, mixed freely with the
  rest. A link with a `/<path>`, or a `@<a>..<b>` change-set target, names a
  SET OF FILES; comparing it against a whole tree projects the tree onto that
  set, so the answer is scoped to those paths and never grows to the tree's
  size. E.g.:

  ```bash
  gg compare gg://repo@ref:main gg://repo@ref:v1.2   # two tips, whole trees
  gg compare gg://repo@abc1234..def5678 gg://repo@ref:main
                                # did main already absorb that change-set?
  gg compare gg://repo/internal/tui/model.go@ref:main main
                                # one file, one row
  ```

  A link naming a DIFFERENT checkout is refused (exit 2): cross-repository
  compare is not supported yet.
- `gg compare --save <label> <left> [<right>]` — run the comparison AND keep
  it. Both sides are stored as `gg://` links whatever you typed, so
  `bookmark:<id>`, `shelf:<id>`, `@staged`, `@worktree` and a bare commit-ish
  all become links. A token that cannot be expressed as one is refused
  (exit 2) and nothing is stored; a comparison that FAILS stores nothing
  either.

  **stdout is unchanged** — still the `<status>\t<path>` list, so it stays
  pipeable. The saved id is reported on stderr as `# saved: <id>\t<label>`.
- `gg compare --saved <id|label>` — re-run a stored comparison. It prints
  exactly what the original invocation printed.
- `gg compare --list` — the stored comparisons, one
  `<id>\t<label>\t<left>\t<right>` line each. A saved MERGE PREVIEW is a
  one-sided entry, so its `<right>` column is EMPTY — the column count is
  fixed either way, for `cut`. Nothing is printed when none are stored.
- `gg compare --remove <id|label>` — delete ONE stored comparison (a saved
  merge preview is a row of the same store, so this removes one too).
  `gg compare --rename <id|label> <new-label>` relabels one; the id is derived
  from the two links, so it does not change. Both take no endpoints, print
  nothing on stdout, and confirm on stderr (`# removed: <id>\t<label>` /
  `# renamed: <id>\t<label>`). An unknown `<id|label>` is exit 2.

  ```bash
  gg compare --save "auth refactor" main feat/auth
  gg compare --list | cut -f1,2        # ids and labels
  gg compare --saved "auth refactor"   # run it again later
  gg compare --rename "auth refactor" "auth refactor v2"
  gg compare --remove "auth refactor v2"
  ```

  Saved comparisons and saved merge previews share ONE store, so
  `gg compare --list` shows both and `gg preview list` shows the previews
  among them.
- `gg preview add [--label <text>] <source> <target>` — save a MERGE PREVIEW:
  "what would <source> bring into <target>", i.e. the GitHub pull-request
  files-changed diff (`git diff target...source`, from their merge base to
  the source tip — NOT the tip-to-tip diff `gg compare` prints). Names are
  stored, not hashes, so every later `show` reflects the current tips.
  `gg preview list` prints `<id>\t<label>\t<source>\t<target>\t<state>\t<files>\t<ahead>`
  (state: `ok`, `merged`, `missing-source`, `missing-target`, `no-base`, or
  `error` with zero counts for a row whose summary failed — the reason goes to
  stderr and the other rows still print; exit 1 only if EVERY row failed);
  `gg preview show [--patch] <id|label>` prints the file list (or unified
  diff; a non-ok state goes to stderr with exit 1); `gg preview diff
  [--patch] <source> <target>` is the one-off form with no record, and
  `gg preview diff <id|label> --hunks [--json]` numbers a saved preview's
  hunks (the same numbering as `gg diff --preview P --hunks`);
  `gg preview rename <id|label> <text>`; `gg preview rm <id|label>`.
- `gg preview add [--label <text>] <a>..<b>` — save a COMMIT PAIR: the plain
  two-dot diff between two commits, as a named entry beside the merge
  previews. ONE argument holding `..` (three dots is a merge preview, not a
  pair). Any rev works and both sides FREEZE to full shas at save time, so
  `gg preview add main..feat/x` stores today's two tips and never follows the
  branches. Prints the id; an already-saved pair exits 1 naming the existing
  id; an unknown rev or `a == b` exits 2. `gg preview list` prints a pair in
  the same seven columns — `<id>\t<label>\t<a-full-sha>\t<b-full-sha>\tpair\t<files>\t0`
  (state `missing` when a commit is gone) — after the previews;
  `show [--patch]`, `rename` and `rm` take a pair's id or label too. Its link
  is `gg://<repo>@<a>..<b>` (see `gg compare --list`), which `gg diff`,
  `gg open` and `gg compare` accept. A pair is also a NOTE SCOPE, exactly like
  a merge preview: `--preview` takes `<a>..<b>` or a saved pair's id/label on
  `gg diff`, `gg note add|list|apply`, `gg review` and `gg link`, and every
  `gg note` verb plus `gg session highlight add` accept the pair link itself
  (`gg note add gg://<repo>/<path>@<a>..<b>:<line> --summary …`, `#<hunk>`
  numbered over the pair's patch). Notes land on `b`, NEW side only — they are
  ordinary notes on that commit and outlive the saved entry; `:old:` or a
  delete-only hunk exits 2, and `gg show <pair link>` still exits 2.
  `gg preview diff [--patch] <pair id|label>` prints a saved pair.
  Notes live inside a preview: `gg diff --preview <id|label|<target>...<source>>
  [--hunks [--json]]` prints the preview's own patch and numbers its hunks,
  and `gg note add --preview P --file F (--new-line N | --hunk H) --summary …`
  anchors a note on it. A preview note is stored on the SOURCE TIP and shows
  on that commit's own view too; the old side (the merge base) is not
  addressable, so `--old-line` is refused. `gg note list --preview P [--file F]
  [--json]` lists the notes gathered along the whole branch — a note written
  against an earlier commit whose lines a later commit changed is reported
  `outdated` rather than dropped. `gg note apply --preview P --stdin` imports
  a batch onto the tip (old-side items skipped), and `gg review --preview P
  [--notes]` reviews the pair.
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
- `gg pr list|view|comments|fetch|forget` — READ-ONLY pull requests through the
  forge's own CLI (GitHub's `gh` today; nothing is ever posted, edited or
  submitted). Needs `gh` installed, logged in and able to read this repo;
  otherwise every verb exits 1 with `gg pr: no forge CLI can read this
  repository's pull requests: <why>`. `--json` may sit anywhere.
  `gg pr list [--json]` prints `#<n> <state> <author>  <source> → <target>
  [draft] [<review_state>]  <title>` — open PRs first, newest-updated first; a
  fork head prints `owner:branch`. PRs gg already knows stay listed after they
  close, marked `closed`/`merged` (or `unavailable` when the forge no longer
  answers): known = listed open earlier in this process, or fetched.
  `gg pr list --state all|open|closed|merged --search <text> --limit <n>`
  (any one of the three flags) SEARCHES the forge instead — the way to a
  closed/merged PR gg never fetched. Same row format and the same bare JSON
  array; state defaults to `all`, limit to 50 (1…200), newest-updated first.
  `<text>` is the forge's own search syntax (`author:kim`, `head:fix/x`,
  `-label:bug` — a value may start with a dash; `--search=<text>` works too);
  a text that is only a number (`123`, `#123`) looks that PR up directly and
  ignores `--state` (unknown number = empty list, exit 0). When the forge has
  more rows than the limit, `more results — narrow the search` goes to STDERR.
  A bad state or limit exits 2. Search results never join the plain list.
  `gg pr view <n> [--json]` prints the row, the URL, the description, then
  `── conversation ──` (general comments and review verdicts, oldest first,
  `carol [changes_requested]: …`) and `── outdated ──` (inline threads whose
  lines later pushes changed, with their original line and hunk tail).
  `gg pr comments <n> [--json]` prints the inline threads that still have a
  position: `a.go:10-12 (new) carol [resolved]: body`, replies indented two
  spaces, file-level threads as `a.go (file)`, body continuation lines indented
  four. JSON: `{inline, hub, outdated, truncated}` of `{id, parent_id, kind
  (inline|file|general|review), author, body, path, side (old|new), line,
  start_line, outdated, resolved, hunk, verdict, created, updated}`;
  `truncated` = the PR has more than gg reads (100 threads × 50 comments, 100
  conversation comments, 100 reviews — per PR).
  `gg pr fetch <n>` fetches the PR head into the private ref `refs/gg/pr/<n>`
  (a local ref only — never pushed, never shown in the graph; a no-op when
  already current) and prints the ref, so the change itself reads with
  `gg diff <target>...refs/gg/pr/<n>`. Fork PRs work. `gg pr forget <n>`
  deletes that ref and drops a closed row.
- `gg versions [<branch>]` — list a branch's recorded pre-operation
  snapshots (taken automatically before merges, rebases, resets, amends,
  and branch deletion), newest first: `<id> <short-sha> <time> <subject>`.
- `gg versions show <branch> <id|latest>` — print the frozen change set a
  two-branch version recorded (`<status>\t<path>` lines, its own
  Base...Ours diff at operation time — never a live diff against whatever
  the branch is at now). A one-branch record (amend, reset, undo-commit,
  delete-branch, restore) prints `<id>: records no preview (a one-branch
  operation)` instead (exit 0, not an error).
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
- `gg migrate [--yes]` — list pending store migrations (a feature whose data
  format is behind what this build writes, but repairable) and what applying
  each would discard. Without `--yes` this changes NOTHING — it is safe to
  run just to check. With `--yes`, applies every listed migration. A feature
  gg can't satisfy and can't repair keeps running with that one capability
  disabled rather than failing the whole command; only a Required feature
  (e.g. the git version floor) blocks gg from starting at all. The
  `versions` store's migration is a straight discard: a format-1 branch
  version carries no merge-base, so it can never become a preview, only be
  thrown away. A repo that still holds format-1 data **records no new
  versions at all** until it runs — rebases/merges/pulls there proceed with
  no safety net rather than mixing formats.
- `gg merge [--into <target>] [--on-conflict=keep|abort] <source>` — merge one
  branch into another (default target: the current branch; worktree-aware —
  merges in the worktree that has the target checked out, autostashes when it
  must switch). `--on-conflict=keep` leaves conflicts in the tree (exit 1),
  `--on-conflict=abort` restores the tree (exit 0); with neither and no TTY, a
  conflict exits 1 with the options on stderr. A successful merge prints the
  same change-set-drift summary as `gg pull` (see above) for the branch it
  moved.
- `gg rebase [--branch <b>] [--on-conflict=keep|abort] <newbase>` — replay a
  branch's commits onto `<newbase>` (default branch: the current one; `--branch`
  rebases another branch, switching to it). Worktree-aware — rebases in place,
  in the worktree that has the branch checked out (you stay put), or autostashes
  and switches. A conflict pauses the rebase: `--on-conflict=keep` leaves it
  paused for `git rebase --continue` (exit 1), `--on-conflict=abort` runs
  `git rebase --abort` (exit 0); with neither and no TTY, a conflict exits 1
  with the options on stderr. A successful rebase prints the same
  change-set-drift summary as `gg pull` (see above) for the branch it moved.
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
  `gg worktree add --branch <name> [<path>]` /
  `gg worktree remove [--with-branch] [--force] <path>` — linked worktrees;
  `add` resolves branch/path templates from `.gg.toml` and may prompt on stdin
  for `<user:...>` fields; `add --branch` checks out the EXISTING branch in
  the new worktree (no new branch; refuses a branch already checked out).
  With `--branch` an optional `<path>` is the destination (relative to the
  current directory, like `git worktree add`), bypassing the path template —
  e.g. `gg worktree add --branch feat/x .claude/worktrees/feat-x`.
  `remove` refuses a dirty or **locked** worktree (an interrupted `add` can
  leave one locked); `--force` removes a dirty tree and unlocks a locked one.
- `gg worktree add --from <commit> [--keep staged|unstaged] [<branch-name>]`
  — create a worktree on a NEW branch cut from `<commit>` rather than a
  branch tip. Default name `<current-branch>_<short-sha>` (a trailing
  positional overrides it); mutually exclusive with `--branch` (`--from`
  always makes a new branch). `--keep staged`/`--keep unstaged` instead
  land the new branch on the commit's PARENT, with the commit's own diff
  left staged (`git reset --soft`) or unstaged (`git reset --mixed`) in the
  fresh worktree — redo a commit differently without touching the branch
  it came from. `--keep` requires `--from` (exit 2 without it, or on an
  unrecognized value); a root or merge commit under `--keep` is refused
  (exit 1) before anything is created — there is no single parent to keep
  the diff against.
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
- `gg worktree rename [--force] <worktree> <new-name>` / `gg worktree move
  [--force] <worktree> <new-path>` — relocate a linked worktree's directory
  (`git worktree move`); `rename` is a same-parent move computed from just
  the new directory name (refuses a name containing `/` or `\` — use `move`
  for that). `<worktree>` resolves by path (absolute, cwd-relative, or
  main-worktree-relative, exactly like `worktree remove`) or by branch name.
  A relative `<new-path>` resolves against the invocation directory. The
  **main worktree is refused**. A **locked** worktree (an interrupted `add`
  can leave one locked) forks the `move-worktree-locked` decision —
  `unlock-and-move` / `abort`; `--force` pre-answers `unlock-and-move`.
  Moving/renaming the worktree your shell is sitting in still succeeds: gg
  leaves the tree before the git call (required on Windows, which cannot
  rename a directory a process holds as cwd) and updates the shell-init
  cwd-handoff file so a wrapped shell's `cd` follows to the new path.
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
  for a locked worktree under `worktree rename`/`worktree move`; `--force`
  for unmerged branch deletion.
- On a non-zero exit, read stderr: it names the decision and the valid
  options; re-run with the matching flag.

Exit codes: 0 = success, 1 = operation failed or needs a decision,
2 = usage error.

**Paths.** Everything gg *prints* — `gg status`, `gg diff`, `gg show --patch` —
is relative to the **worktree root**, wherever you run it from. A pathspec you
*type* is relative to **your cwd**, exactly as it is for git: in `src/`,
`gg add .` stages `src/` and `gg add xxx.txt` stages `src/xxx.txt`. So a path
copied out of `gg status` only feeds straight back in when you are at the
root — the simplest habit is to run gg from the worktree root, where the two
are the same thing. (`gg note --file` is the one exception: it is always
repo-relative, because a `gg://` link can point it at another checkout.)

## Registering yourself as a gg tool

gg can call an external AI agent for four tasks: **resolving conflicts**
(the TUI conflict window's `t` picker), **resolving-and-completing a paused
operation** (the same `t` picker — the agent resolves, stages, runs the
matching `--continue` through further rebase rounds, and writes an overview
to `$GG_MESSAGE_FILE`; never `--abort`), **generating commit messages** (the
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
category = "commit_message"   # conflict | commit_message | review | conflict_complete
name     = "MyAgent"          # picker label; unique per category
mode     = "capture"          # capture = headless | terminal = takes the TTY
command  = '''myagent --do-the-task'''
# per_file = true             # conflict only: run once per conflicted file
# when_op  = "merge"          # conflict/conflict_complete only: merge|rebase|cherry-pick|revert
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
- **conflict_complete** (`mode = "terminal"` or `"capture"`): same env as
  `conflict`, plus an empty `$GG_MESSAGE_FILE` and `GG_TASK=conflict_complete`.
  Unlike `conflict`, you OWN the sequencer for this run: resolve every
  conflict, stage it, run the matching continue command (`git merge
  --continue`, `git rebase --continue`, `git cherry-pick --continue`, or
  `git revert --continue`), and repeat through further rebase rounds until
  the operation finishes. Never run any `--abort` command and never push —
  if a conflict cannot be resolved safely, stop and leave the operation
  paused instead. Finally write an overview (which operation, each file and
  how you resolved it, how many continue rounds ran, the final state) to
  `$GG_MESSAGE_FILE`; gg opens it in a report viewer on a clean exit.

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

MCP also carries the `gg://` link surface, so a link a human pastes into the
chat needs no shell round-trip:

- `gg_link_resolve {link}` — take the link apart: `checkout`, `path`,
  `state`, `commit`, plus `ref` for a branch-tip link, `pair_a`/`pair_b` for
  a change-set, `preview_source`/`preview_target` for a merge preview,
  and `line`/`side`/`hunk`/`hint_kind`/`hint_id` when the link carries them.
  A field is OMITTED when the link does not name it, so an absent `line`
  means "no line", never line 0.
- `gg_link_list {}` — this repo's copied-link ring, newest first, the same
  rows `gg links` prints.
- `gg_compare_links {left, right}` — the changed files between the two places
  two links name, in either order. Two links naming different repositories
  are refused.
- `gg_compare_list {}` — the SAVED comparisons `gg compare --list` prints:
  each row has `id`, `label`, `kind` (`preview` = a merge preview, `pair` = a
  frozen commit pair, `comparison` = two links), `left`/`left_desc` and — for
  a comparison only — `right`/`right_desc`. A preview's or pair's `id` or
  `label` is what the note tools' `preview` argument takes; a comparison's
  `left` and `right` go straight into `gg_compare_links`.
- `gg_compare_file` takes `{"source":"link","link":"gg://…"}` as either side:
  a link carries its own path and state, so it needs neither `path` nor
  `locator`.

All four resolve against the repository that server was started in; a link
naming a different checkout is refused rather than answered from elsewhere.

MCP also carries the review-notes surface — the same store `gg note` writes:

- `gg_notes_list` — review notes, resolved (read-only)
- `gg_note_add` — leave one anchored note (mutates)
- `gg_notes_apply` — import a batch of notes (mutates)
- `gg_note_rm` — remove one note (mutates)

## Shell following

Worktree and repo switches write the target directory to `--cwd-file <path>`
(human shells follow via `gg shell-init`). As an agent, just `cd` to the path
printed on stdout.
