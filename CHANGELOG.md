# Changelog

All notable changes to gigagit (`gg`) are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
No tagged release has been cut yet; everything lives under **Unreleased**.

## [Unreleased]

### Added
- TUI: the hunk picker (conflict resolve / `H` staging) now reads long lines —
  **`z`** cycles the display mode (**scroll** default → wrap → cutoff) and
  **shift+←/→** pans in scroll mode, matching the rest of the app's windows. The
  picker also scrolls vertically now, so large hunks no longer run off-screen.
- TUI: **hunk/line staging** — press `H` on a file in the Files panel to open a
  GitKraken-style staging picker (the same surface as the conflict resolver).
  Per hunk: stage the whole working-tree side (`i`), keep the index side (`c`),
  or `space` to stage individual lines (in pick order); `C`/`I` apply to all
  hunks; `enter` stages the selection — the working tree is never modified (only
  the index). `space` still stages the whole file.

#### TUI action menu (.)
- **`.`** opens an action menu listing every action available in the current
  context; press the action's key to run it, or `↑`/`↓` + `enter` (`/` filters,
  `z` cycles display mode). New `[ui] footer_actions` and `[ui] menu_actions`
  config lists (action ids; unset/empty = show all) choose which actions appear
  in the footer bar versus only in the menu.

- TUI: **conflict hunk picker** — press `enter` on a both-modified file in the
  `x` conflict resolver to open a GitKraken-style region/line editor. Per
  conflict region: take the whole **current** or **incoming** side (`c`/`i`),
  or `space` to pick individual lines from either side (they land in the result
  in pick order); `C`/`I` take all regions one way; `←/→` switch side, `↑/↓`
  move the line cursor, `n`/`p` jump regions, `enter` applies once every region
  is resolved. The whole-file resolver's labels are now **current/incoming**
  (clearer than ours/theirs, which invert during a rebase).

#### TUI Files / Staged split
- The Status panel is now two panels: **Files** (working-tree changes —
  unstaged, untracked, and conflicts) and **Staged** (the index). `space` stages
  from Files and unstages from Staged; a partially-staged file shows in both.
  Each row shows a **single status letter for that panel's own side** (not git's
  two-byte code): Files shows the working-tree state (a new untracked file → `A`,
  a conflict → `U`, otherwise `M`/`D`), Staged shows the index state
  (`A`/`M`/`D`/…). So a partially-staged `MM` file reads `M` in Files and `M` in
  Staged — each letter describing only that window.
  `enter` on a Files row diffs index → working tree (the unstaged delta); on a
  Staged row it diffs HEAD → index (the staged delta), so the two panels show
  the unstaged and staged halves of a partially-staged file as disjoint diffs
  that match each row's status letter. Each panel header shows its file count. On
  a short terminal the Staged panel is dropped (Branches/Worktrees tab over
  Files).

#### TUI tabbed Branches/Worktrees
- The Branches and Worktrees panels are now one tabbed left-column slot —
  **`ctrl+←/→`** switches the active tab (and focuses it); each tab keeps its
  own selection, sort, and filter. The active tab is shown in the slot's header
  (`[Branches] Worktrees` / `Branches [Worktrees]`).

#### TUI window display modes
- Every list/tree/text window (panels, stash list, files tree, file history,
  blame) **and every list popup** (repo switcher, help, conflict resolver,
  settings, pair-op, stash actions) now shares one rendering primitive with
  three switchable text display modes — **cutoff** (default; long rows truncate
  to one line), **wrap** (rows wrap onto multiple lines), and **scroll** (rows
  stay full, panned horizontally). Press **`z`** to cycle the focused window's
  mode; in scroll mode **`shift+←/→`** pans. Each window remembers its own mode.
- The diff view's existing long-line cycle moved from `w` to **`z`** so one key
  means "display mode" everywhere. The two worktree-create keys (`w`
  existing-branch, `W` new-branch) are unchanged.

#### Interactive rebase (engine + scriptable CLI + TUI editor)
- `gg rebase -i --plan <file> <newbase>` drives an interactive rebase from a
  plan (pick/reword/squash/drop + reorder), executed via git's interactive
  rebase with gg acting as the sequence editor. Squash composes a combined
  message (target subject + each squashed commit's message line-by-line). The
  working tree is preserved across the rebase (stash-wrap); conflicts pause for
  `git rebase --continue` or `--on-conflict=abort`.
- TUI: mark two branches (`m`, `m`) on the Branches panel and choose
  **Interactive rebase {marked} onto {selected}** to open a GitKraken-style
  editor — per-row pick/reword/squash/drop, reorder with `ctrl+↑/↓`, `enter`
  starts, `R` resets, `esc` cancels. Reword opens an inline title+description
  editor; squash composes the combined message; the working tree is preserved.

- TUI: a conflicted repo shows a status-bar notice (`⚠ N conflict — press [x]
  to resolve`) that names the source of the conflict — `⚠ N conflict merging
  <branch> into <branch>` or `… rebasing <branch> onto <branch>` (the resolve
  popup shows the same phrase as a subtitle); a stash-pop or other untracked
  source shows the bare notice. `x` opens a resolution popup that resolves each
  unmerged file at the whole-file level — keep ours/theirs/base, keep-modified,
  delete, or mark-resolved (the offered keys adapt to the conflict type), plus
  `A` to mark all resolved and `c`/`a` to continue/abort an in-progress merge or
  rebase. The popup reopens after each action until the tree is clean. Partial
  (hunk/line) resolution is a later feature.
- TUI: `m` multi-marks files in the Status panel; `s` opens a stash-create popup
  (name defaults to `WIP on <branch>`, a checklist of unstaged/untracked files,
  `ctrl+s` stashes the checked ones).
- TUI: `S` opens a stash list window in the right column; `l` shows the selected
  stash's files in the tree (diff / history `h` / blame `b`, like commit files);
  `enter` opens an action popup to apply, pop, or drop the selected stash.
- CLI: `gg stash` gains `list`, `apply [<ref>]`, `pop [<ref>]`, `drop [<ref>]`
  subcommands, plus `-u` and `-- <paths>` on push. A conflicting apply/pop exits
  non-zero and keeps the stash. (agentskill v8)

### Fixed
- TUI: the repo switcher (`R`) no longer wraps long repository paths onto
  multiple lines — entries render as clean single lines (cutoff), with `z` to
  switch to wrap/scroll when you want the full path.
- Diff view: a working copy with CRLF line endings (e.g. a Windows checkout
  under `core.autocrlf`) no longer shows the whole file as changed. The diff
  compared `git show` (LF) against the raw on-disk bytes (CRLF), so every line
  differed by a non-printing carriage return while rendering identically; the
  line-alignment engine now treats a trailing `\r` as not part of line identity,
  matching `git diff` and how the rows are drawn.
- Status bar: failures (e.g. a stash apply that would overwrite local changes)
  now render as a bold white-on-red bar instead of the same plain text as the
  key hints, so an error is no longer easy to miss.
- Stash file tree: a stash's files are now listed once. A stash commit is a
  merge of its HEAD and index parents, and the old `diff-tree -m --first-parent`
  double-listed a file that differs from both parents; the commit-files query
  now takes the first-parent diff via `git log` (also covers any merge commit).
- Panel filter (`/`): `↑`/`↓` (and `pgup`/`pgdn`) now move the selection through
  the filtered rows while still typing the query — an incremental picker, like
  the repo switcher. Previously arrows were ignored until you pressed `enter`,
  so the cursor appeared stuck on the top match.
- Blame view: source rows are now tab/control-sanitized and padded to full
  width, fixing scroll artifacts (stale text and orphaned highlight bars left
  behind when moving up/down).

### Changed

#### Diff view change counter
- The diff view header now shows `change <n>/<total>` — which difference is
  currently displayed and how many there are in the file.

#### `q` quits only from the home screen
- Pressing **`q`** now quits the app **only on the base (home) layout**. In the
  diff, file-history, blame, and commit-files views `q` is inert — **`esc`** is
  the back key (it closes the surface and returns to the previous layout) and
  **`ctrl+c`** remains the universal quit. This prevents accidentally quitting
  the whole app while reading a diff or browsing history.

### Added

#### Commit & amend (`c` / `C`)
- `c` opens a commit popup with a **title** and a multi-line **description**,
  and commits the staged index. `tab` switches fields, `enter` moves title →
  description (and inserts newlines in the body), `ctrl+s` commits, `esc`
  cancels. Empty title is refused; `c` with nothing staged is a no-op.
- `C` **amends** the last commit: the same popup opens pre-filled with HEAD's
  message, so `ctrl+s` rewrites the message and folds in whatever is currently
  staged (stage with `space`, then `C`). No-op when there is no commit yet.
- CLI: `gg commit --amend` rewrites the last commit; with no `-m` it reuses the
  existing message.

#### Staging (`space` in the Status panel)
- `space` on a Status-panel file stages it (`git add`), or unstages it
  (`git restore --staged`) when it is already fully staged; conflicted files
  are skipped (resolution lands with the conflict feature). The Status panel
  refreshes on its own without a full reload.

#### Rebase (`gg rebase` + the TUI pair-op)
- `gg rebase [--branch <b>] [--on-conflict=keep|abort] <newbase>` and the TUI
  Branches mark-and-pair **"Rebase {marked} onto {selected}"** operation replay
  a branch's commits onto a new base. Worktree-aware: rebases in place, in the
  worktree that has the branch checked out (you stay put), or autostashes and
  switches to it. A conflict pauses the rebase — `keep` leaves it for
  `git rebase --continue` (exit 1), `abort` runs `git rebase --abort` (exit 0).

#### Diff view: long-line modes (scroll / wrap / truncate)
- The diff view now opens in **horizontal-scroll** mode by default: long lines
  pan with `←`/`→` (`0` resets; `‹`/`›` mark off-screen text), step from the new
  `[ui] hscroll_step` (default 8). `w` cycles scroll → wrap → truncate; the mode
  shows in the hint and is remembered for the session.

#### File blame view (`b`)
- Press `b` on a Status-panel file, a files-view tree row, or inside the diff
  view to open a **Blame** view: the file's content with each line tagged by the
  commit that last changed it. Consecutive lines from the same commit collapse
  into one gutter remark (short hash, author, compact age); uncommitted lines
  show `(uncommitted)`. `↑`/`↓` move the line cursor; `enter` opens that commit's
  file history; `esc`/`b` go back. From the History view, `b` blames the file at
  the selected commit — closing the history↔blame cross-link.
- The second consumer of the TUI **view stack** (after history).

#### File history view (`h`)
- Press `h` on a Status-panel file, a files-view tree row, or inside the diff
  view to open a **History** view: the commits that touched the file on the left
  (newest first), the file's diff at the selected commit on the right. `↑`/`↓`
  move between commits (the diff reloads); `esc`/`h` go back. Rename-following
  via `git log --follow`; history depth caps at 200 commits for large repos.
- Built on a new full-screen **view stack** — the first piece of the TUI layout
  layer. Surfaces pushed on the stack own the screen and input; `esc` pops one
  level, revealing the surface beneath. (Migrating the existing surfaces onto
  the stack is a follow-up.)

#### Diff view: partial mode + change navigation
- The full-screen diff view gains a **partial mode** (`f` toggles): show only
  changed lines plus 3 lines of context, collapsing long unchanged runs into a
  fold marker — GitHub's split-diff style. The choice is remembered for the
  session.
- `n` / `p` jump to the next / previous change (aliases of `ctrl+↓` / `ctrl+↑`).
- A diff now opens scrolled to the first change instead of the top.

#### Diff view: intraline emphasis + commit-diff cache
- Changed lines now show **GitHub-style intraline word emphasis** — the exact
  words that differ are highlighted within a changed line, not just the whole
  line.
- **Commit diffs are cached**: re-opening the same file in a commit issues no
  further `git show` and skips re-alignment — the diff for an immutable commit
  is computed once. Working-tree diffs still reflect live edits (never cached).
- New `internal/cache` package: a generic, injected, two-bound in-memory LRU
  cache factory (entry-count **and** byte-budget eviction, so it can't eat
  memory), reusable for future heavy reads. Wired today only for diffs.

#### Diff view: word-wrap toggle
- `w` in the diff view word-wraps long lines across multiple rows (the two
  panes stay aligned) instead of truncating them with `…`. Default off,
  remembered for the session, and the view re-wraps when the terminal resizes.

#### Domain layer & repo gate
- New `internal/domain` command layer: both frontends now run engine
  operations through `domain.Execute`, which serializes them per repository
  on a three-mode reservation gate (`internal/repogate`: Read / RefWrite /
  TreeWrite, writer-preferring FIFO, keyed by git common dir so linked
  worktrees share one gate). Reservations are held across user decisions —
  the exclusion unit is the whole operation, not one git call. Background
  pulls hold only a ref-write reservation and escalate at a safe boundary
  when the user chooses checkout-and-resolve. Queued reservations emit
  "gate wait" spans, and a TUI op's context is now cancelled when the
  program exits. Foundation for concurrent operations (workspace group
  sync, MCP); no behavior change for single operations.
- Stage 2: the TUI startup load and the CLI's status/worktree reads now run
  as **domain queries** (`Snapshot`, `Status`, `Worktrees`) under a Read
  reservation. `Snapshot` fetches the seven startup reads in parallel
  (collapsing sequential startup latency to the `git status` long pole) and
  coalesces concurrent identical reads with a singleflight group; a load
  generation counter drops a stale in-flight snapshot so a superseded load
  can't paint over a newer one.
- Stage 3: the Commits panel is backed by a domain `CommitFeed` read-model and
  loads history **on demand** — no more 50-commit cap. Scrolling toward the
  end pages in more (50 initial, 200 per page) through the gate; the panel
  label shows `Commits N+` until history is exhausted, then `Commits N`. The
  feed is the single source of truth for commits (`Snapshot` no longer reads
  them); `git log` gained a `--skip` offset.
- Stage 4: `engine.OpDeps.Repo` is now a `GitOps` interface (operations are
  decoratable and mockable). The TUI and CLI no longer import `internal/git` —
  the last raw reads (`ShowFile`, `CommitFiles`, `TopLevel`, `CurrentBranch`,
  `GitCommonDir`) go through gated domain queries, enforced by an import-guard
  test. Concurrent git subprocesses are bounded (process-global ceiling of 8).

#### Side-by-side diff view
- TUI: `enter` on a Status-panel file opens a full-screen dual-pane diff
  (old version left, new right, aligned and highlighted): HEAD → working
  tree. Untracked files render all-added; deleted files all-removed.
- `enter` on a file in the commit files view (tree side focused) shows that
  file's change in the viewed commit (first parent → commit; renames diff
  old path against new).
- `↑/↓` scroll, `pgup/pgdn` page, `ctrl+↑/↓` jump between change blocks,
  mouse wheel scrolls, `esc` closes back to where you were.
- New pure package `internal/textdiff` (Myers line alignment with size
  guards) — the comparison engine the M3 conflict editor will reuse.

#### Mouse focus & wheel
- TUI: left-click focuses the window under the cursor and selects the
  clicked row (files view included: a tree click moves the tree cursor, a
  commits click moves the commit selection through the follow-live reload).
  The mouse wheel scrolls the list under the cursor — focus untouched —
  stepping by the new `[ui] wheel_step` config entry (default 3,
  defaults→global→repo overlay). Mouse input is ignored under popups and
  the decision modal. New project skill `adding-config-entries` documents
  the config system.

#### Arrow-key window focus
- TUI: `←`/`→` switch focus horizontally — `→` from a left panel focuses
  Commits, `←` returns to the last-focused left panel. Inside the commit
  files view, `←`/`→`/`tab` switch between the file tree and the commit
  list; vertical movement follows the focused side and `ctrl+↑`/`ctrl+↓`
  always scroll the tree. Focus is visible: the border and row highlight
  follow it.

#### Commit files view
- TUI: `l` on the Commits panel shows the selected commit's changed files as
  a directory-grouped tree in the left column (replacing the three left
  panels while open). Follow-live: j/k keeps moving through commits and the
  tree reloads for each one. `/` searches file paths, ctrl+↑/↓ / pgup/pgdn /
  mouse wheel scroll, esc/`l` close. Merge commits show their first-parent
  diff; renames render as `R old → new`.

#### Contextual footer
- TUI: the footer now shows only the keys that work right now — panel/row
  specific actions first (`[s]witch` hides on the checked-out branch,
  `[enter] switch`/`[d]elete` hide on the current worktree, the `m` hint
  reads mark/unmark/pair to match the mark state), then the global tail.
  While an operation runs it collapses to `[tab] [?] [q]`; filter input
  shows its own line. Availability predicates are shared between the key
  dispatch and the footer, so a shown key always works — and `s`/`d` on the
  checked-out branch or `d` on the current worktree are now clean no-ops
  instead of operations git rejects.

#### Mark-and-pair operations + SmartMerge
- TUI: `m` marks a row; `m` on a second row of the same panel opens a
  pair-operation picker (generic per-panel registry). Branches offer
  **Merge** (worktree-aware SmartMerge: merges in place, in the worktree
  that has the target checked out, or autostash+switch+stay) and a
  disabled Rebase placeholder. `esc` clears the mark before the filter.
- CLI: `gg merge [--into <target>] [--on-conflict=keep|abort] <source>`.
  New `merge-conflict` decision (`keep-conflicts`/`abort`) shared across
  frontends. Embedded using-gg agent skill v5.

#### Help window
- **Help window**: `?` opens a searchable list of every key binding, grouped
  by context. `/` searches (panel-consistent); ↑/↓ or j/k, ctrl+↑/↓,
  pgup/pgdn, and the mouse wheel scroll; `q`/esc close. Built on a new generic read-only content popup (`contentPopup`)
  reusable for future viewers. Mouse reporting is now enabled while gg runs
  (shift+drag for native terminal text selection).

#### Performance log
- Global `--time-track <file>` flag (TUI + every CLI command): appends one
  redacted JSON span per process start, git subprocess, and engine operation.
  Same span schema as `gg inspect --trace` and the panic dump. Embedded
  using-gg agent skill v4.

#### Truncation tooltip
- TUI: when the focused panel's selected row is too wide and ellipsized, a
  floating strip directly above the row shows its full text (all four list
  panels; wraps up to 3 lines; suppressed under modals/popups).

#### Worktree for an existing branch
- `w` on the Branches panel now creates a worktree that checks out the
  **selected existing branch** (no new branch); `W` opens the previous
  template-driven popup (new branch + worktree). CLI:
  `gg worktree add --branch <name>`. Embedded using-gg agent skill v3.

#### Branch management
- Branch management: `b` (create-branch popup) / `B` (create and smart-switch)
  / `d` (delete with confirm + unmerged force fork) on the TUI Branches panel,
  and `gg branch create <name> [<start>]` / `gg branch delete [--force] <name>`
  in the CLI. The embedded using-gg agent skill is now v2.

#### E2E harness
- **E2E scenario harness** (`e2e/`): declarative TOML scenarios build real git
  repos (optionally served over real HTTP via `git http-backend`), run gg CLI
  commands in-process, and assert user-visible state — files, branches, stashes
  and their content, sync state, history shape. 17 scenarios cover SmartSwitch,
  SmartPull (ff/rebase/merge/abort/conflict/background/worktree), stash,
  commit+push, undo, and worktree add/remove. New agent skill:
  `.claude/skills/writing-e2e-scenarios/`.
- 8 more scenarios (s18–s25) covering `gg branch create/delete` (start points,
  guards, the unmerged fork with and without `--force`) and
  `gg worktree add --branch` (existing-branch checkout, guards, usage errors);
  the skill's contract table gained the three new command rows.
- **Fix** `gg worktree remove <relative-path>`: a repo-top-relative path now
  matches regardless of process cwd (found by the e2e corpus; matters for
  in-process frontends like the future MCP server).
- **Staged test runner** `./test.sh` / `test.cmd`: quality gates (vet+gofmt)
  → unit tests → e2e scenarios last; `race` target for the pre-merge gate,
  `unit`/`e2e` to run one stage.

#### Foundation & engine (M1)
- Frontend-agnostic core **engine**: the `Operation` contract with a streamed
  `Event` union (`Progress`/`GitLine`/`DecisionNeeded`/`Timing`/`Done`) and a
  `Decider` for mid-flight, option-list-only forks.
- Thin git verb layer (`internal/git`) over a `gitcmd` argv builder and a
  `gitexec` process runner (with a `FakeRunner` for tests).
- Smart operations: `SmartPull` (worktree-aware divergence decision tree),
  `SmartSwitch` (auto-stash/restore), `Commit`, `Push`, `Stash`, and a
  ref-only `UndoLastCommit`.
- Observability: span ring buffer, tracing, redaction, and a panic debug dump
  (`gg inspect`, `--debug-dump`, `--trace`).
- Interactive **TUI** (Bubble Tea): size-aware multi-panel layout, a modal
  Decider, and a panic-safe dump on crash.

#### CLI (M2)
- Scriptable commands: `gg status`, `commit`, `pull` (`--background`,
  `--on-conflict`), `push`, `switch`, `stash`, `undo`.
- Forks resolved by flags, an interactive stdin prompt, or a clear error when
  neither is available; unknown-subcommand guard.

#### Worktree management (M2)
- TOML config (`.gg.toml`) with field-level overlay and worktree branch/path
  **templates** (`<parent-branch>`, `<repo>`, `<date:…>`, `<seq:…>`, `<user:…>`);
  per-repo `<seq>` counters in `<git-common-dir>/gg/state.toml`.
- **Create**: TUI `w` create popup (live preview, editable name) and `W`
  create-and-switch; `gg worktree add [<start-point>]`; shared resolution in
  `internal/worktree`.
- **Switch**: `enter` on the Worktrees panel re-roots the TUI into the selected
  worktree; `gg shell-init [bash|zsh|fish]` follows the switch with a real `cd`
  via `--cwd-file`.
- **Delete**: TUI `d` on the Worktrees panel and
  `gg worktree remove [--with-branch] [--force] <path>`. Reactive-force model —
  choose worktree-only vs. worktree+branch up front, then force is offered only
  when git refuses (dirty tree or unmerged branch). Engine guards refuse
  removing the current or primary worktree.

#### TUI list UX
- `shift+tab` cycles panel focus backwards; `pgup`/`pgdn` move the selection by
  25% of the focused panel's viewport.
- `o` cycles a panel's sort order (`default → name ↑/↓ → date ↑/↓`) — each
  panel defines its own name/date semantics (branches: committer date;
  worktrees: HEAD commit date; status files: mtime; commits: commit time).
  Branches default to newest-first.
- `/` filters any list panel by case-insensitive substring (`enter` keeps the
  filter, `esc` clears it); selection, paging, and all action keys operate on
  the filtered, sorted view.

#### Repo switcher
- gg auto-records every repository it opens in a machine-local MRU registry
  (`~/.local/state/gg/repos.toml`); dead paths are pruned automatically.
- TUI: `R` opens a switcher popup — filter as you type, `enter` re-roots into
  the chosen repo (the shell follows via `--cwd-file`), `ctrl+d` forgets an
  entry.
- CLI: `gg repo list` and `gg repo switch <query>` (unique substring match
  prints the path and writes `--cwd-file` so a wrapped shell `cd`s there).

#### Agent init
- `gg init` detects installed AI coding agents (Claude Code, Junie, Codex,
  OpenCode, Cursor, AGENTS.md, …) and installs an embedded "using-gg" skill
  teaching them to drive git through the gg CLI. Already-installed targets are
  checked by default (applying refreshes them); new agents are explicit
  opt-in. `--all`, `--update`, `--agents <ids>`, `--list` for scripting.
- TUI: `,` opens a Settings popup with the same agent-skill picker.
- The skill ships inside the gg binary (version-marked); installed copies
  change only when a newer binary's init runs.

#### Developer tooling
- Project skills in `.claude/skills/`: `adding-features` (engine→TUI→CLI wiring
  checklist for new operations/commands) and `adding-tui-windows` (panel vs
  popup vs modal taxonomy and wiring), both validated against baseline runs.

[Unreleased]: https://github.com/gigagit/gg
