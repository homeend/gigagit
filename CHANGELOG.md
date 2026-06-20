# Changelog

All notable changes to gigagit (`gg`) are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
No tagged release has been cut yet; everything lives under **Unreleased**.

## [Unreleased]

### Added
- **Add untracked files to `.gitignore` from the Files panel.** The `.` action
  menu on an untracked file now offers **Add to .gitignore** (ignores that exact
  file via an anchored, glob-escaped `/path` line) and **Add extension to
  .gitignore** (`*.ext`, offered only when the file has an extension). Both
  append to the repo-root `.gitignore`, skip a pattern that is already present,
  and leave the change unstaged; the now-ignored file drops out of the panel on
  refresh. The actions appear only on untracked files, since `.gitignore` has no
  effect on tracked ones.

### Changed
- **Truncated-row reveal now renders inline, on the row itself.** When a
  selected row is too long for its panel, the full-text reveal is drawn over
  that row's own line and overflows the panel border to use the whole screen
  width — spilling right, or **left over the adjacent panels** when the row sits
  in a right-hand panel with little room to its right — instead of floating as a
  strip above the row. Only a row wider than the entire screen clips with `…`.
  The reveal fills the whole selected row, so the row's highlight never peeks
  out beside it. The old strip covered the panel's title bar whenever the top
  row was selected, which hid the very context it sat in; the inline reveal
  never does.
- **The commit files tree reveals its truncated rows too.** A long directory
  heading or file row selected in the files tree (`l`) now reveals its full path
  inline, the same way the panels do — previously the tree had no reveal at all.
- **Commit files tree — directory headings elide from the left.** A nested
  directory heading (e.g. `…/api/v3/ApiObject/`) is shortened from the front, so
  the leaf directory stays visible instead of tail-truncating to look identical
  to its parent (`…/api/v3/`) and reading as a bogus duplicate row.
- **Commits panel — the left column shows the branch, not the commit id.** Each
  commit row leads with the branch it belongs to: **bright** when the commit is
  that branch's tip ("the last commit for a given branch"), **grayed** when the
  commit is only that branch's lineage. Long names trim with `…` (select the row
  to reveal the full name in a tooltip). The commit id moves to the **status
  bar** for the selected commit (`⎇ <branch> · # <id>`); copying the full id is
  still on the `.` menu. Filtering the list still matches the full commit id and
  the full branch name even though neither is shown verbatim in the row.
- Internal: unified the popup overlay stack and the full-screen surface stack
  into **one `layer` stack**. `overlay` + `surface` became one `layer` interface
  (`render(m, below)`), the two stacks merged into one push-ordered pile, and the
  three routing sites (dispatch / render / mouse) collapsed their
  `overlayTop`/`stackTop` pair into a single `topLayer()` check; `render` is now a
  bottom-up walk over the open diff (else the panels). No user-facing change. Only
  the decision modal, the conflict process, the action menu, and the full-screen
  diff remain off the stack.
- Internal: the last three centered pop-ups (the help / `?` cheat-sheet viewer,
  reword-commit, rename-branch) now live on the unified overlay stack like the
  rest; the special `?`-cheat-sheet routing collapsed into the one overlay path
  (the cheat-sheet is simply pushed over the switcher, and esc returns to it).
  No user-facing change. Only the decision modal and the action menu remain off
  the stack.
- **Action menu (`.`) wraps around.** Up-arrow on the first row jumps to the last,
  and down-arrow on the last jumps to the first.
- **Conflict resolution is now a process, not a pop-up.** While you resolve a
  merge/rebase, the interface is locked to the resolution flow: it shows the
  conflicted-file list, hands off to the full-screen line editor for both-modified
  files, runs each resolve/continue/abort with a progress indicator, and reports
  failures in place — every other command is inert (no stale key hints) so you
  can't half-do something mid-resolve. It **always** offers a clean exit: **esc**
  cancels the in-flight step, **L** leaves the whole flow (the repo is left as-is;
  resume from the **[x]** notice). The resolver is no longer a window that
  re-opens itself; it is started/resumed with `x` and survives relaunch into a
  half-finished rebase. (Internally: a new TUI `process` slot that owns input,
  drawing, and the jobs it runs — the first of a general mechanism.)

### Added
- **Delete a tag.** The `.` menu on the **Tags** tab offers **Delete tag** —
  behind a confirm modal (Cancel always available). Also on the CLI: `gg tag rm
  <name>` (alias `delete`).
- **Create a tag.** The `.` menu on a commit offers **Create tag here** — a popup
  takes a name and an optional message (`tab` between fields); an empty message
  makes a **lightweight** tag, a message makes it **annotated**. Also on the CLI:
  `gg tag create [-m <msg>] <name> [<commit>]` (commit defaults to HEAD).
- **Tags (read).** A read-only **Tags** view lives as a tab in the middle (Files)
  window: `ctrl+←/→` switches **Files ⇄ Tags** while that box is focused (the top
  Branches/Remotes/Worktrees slot still cycles when *it* is focused). Each row
  shows the tag (`●` annotated / `○` lightweight), its short target, and the
  subject; `enter` jumps the Commits cursor to the tag's target commit. New
  `gg tag ls` CLI command lists tags newest-first. First stage of staged tag
  support (create/delete/checkout/push to follow).
- **Reset to this commit (soft / mixed / hard).** The `.` menu on a commit
  offers **Reset to this commit** — moves the current branch to it. A modal asks
  the mode: **soft** keeps the changes since staged, **mixed** keeps them
  unstaged (the default), **hard** discards uncommitted tracked changes (untracked
  files survive; the commits you reset past stay recoverable via `git reflog`).
  Because the Commits panel spans all branches, resetting to a commit that is
  **not on the current branch** asks one more confirmation before moving the
  branch onto it. Also on the CLI: `gg reset [--soft|--mixed|--hard] [--force]
  <commit>`.
- **Revert a commit.** The `.` menu on a commit offers **Revert this commit** —
  creates a new commit on the current branch that undoes the selected one. A
  dirty working tree is autostashed and restored. A conflict drops into the
  existing conflict resolver (resolve, then continue or abort). Reverting a merge
  commit is refused (it needs `-m <parent>`, out of scope for v1). Also on the
  CLI: `gg revert [--on-conflict=keep|abort] <commit>`.
- **Cherry-pick a commit.** The `.` menu on a commit offers **Cherry-pick
  here** — applies that commit onto the current branch as a new commit. A dirty
  working tree is autostashed and restored. A conflict drops into the existing
  conflict resolver (resolve the files, then continue or abort), the same flow
  as a merge/rebase conflict. Also on the CLI: `gg cherry-pick
  [--on-conflict=keep|abort] <commit>`.
- **Create a worktree from a commit.** The `.` menu on a commit offers **Create
  worktree here** — opens the create-worktree dialog based at that commit; you
  type the new branch name (it is not templated) and a worktree is created on that
  branch rooted at the commit.
- **Create a branch from a commit.** The `.` menu on a commit offers **Create
  branch here** — opens the create-branch dialog with the selected commit as the
  start point. (`gg branch create <name> <start-point>` already does this on the
  CLI.)
- **Go to a branch's tip in the commit list.** The `.` menu on a Branches row
  offers **Go to tip in commits** — it moves the Commits cursor to that branch's
  tip commit and focuses the Commits panel.
- **Commits panel — branch in the status line.** When a commit is selected, the
  status line shows `⎇ <branch>` — which branch the commit is from (`git log
  --source`/`%S`, computed in the existing feed walk, no per-hover git call) —
  always visible without occluding any commit row.
- **Compare against working dir.** The `.` menu on any focused commit, staged, or
  shelf file offers **Compare against working dir** — a direct side-by-side diff of
  that version against the same path in the working tree (no second pick).
- **Branches panel — indicators moved to a dynamic left gutter.** The set/solo
  `◉` and head `*` markers now render in a left gutter (width adapts to how many
  indicator types are active) so the set marker is no longer truncated in a
  narrow panel.
- **Commits panel — lane color + list mode.** Each commit's `●` node is colored
  by its graph lane (recycled palette, the standard git-client convention). A new
  `.`-menu toggle **Show as list / Show as graph** renders the feed as a flat
  `●`-gutter list (no connectors) that keeps its per-commit lane color even when
  filtered or re-sorted (where the connected graph is suppressed).
- **Commits panel — multi-branch selected set.** The `.` menu on a Branches row
  now offers **Add to commit view** / **Remove from commit view** to scope the
  Commits feed to a custom set of branches (alongside one-tap Solo and Show all).
  Every branch in the set is marked `◉`.
- TUI: the Commits panel now draws a single-line **commit graph** (Unicode
  rounded box-drawing, monochrome) to the left of each commit — lane layout over
  the all-branches date-ordered feed, so forks and merges are visible. Shown only
  in natural feed order; hidden while the Commits panel is filtered (`/`) or
  re-sorted (`o`), where the topology would be incoherent. Backed by the new pure
  `internal/commitgraph` engine.
- TUI: the popup layer now has a stack (`overlayStack`, mirroring the full-screen
  `viewStack`). A child popup opened from the bookmark (`g`) / shelf (`G`)
  switcher — paste, restore, or the remove-confirm — returns to the switcher when
  closed or after the action succeeds, with the switcher's filter/selection
  intact. The "Paste bookmarked file to a new path" destination is now prefilled
  from the bookmark's path with a `_RESTORED` marker (`config.go` →
  `config_RESTORED.go`; `.gitignore` → `.gitignore_RESTORED`).
- TUI: pressing `?` while the bookmark (`g`) or shelf (`G`) quick-switcher is open
  now shows a **compact cheat sheet** of that switcher's keys, overlaid on the
  still-open switcher; `esc` closes it and returns you to the switcher with its
  filter/mark intact. (Other popups still pass `?` through unchanged.)
- TUI: **Compare against bookmark** — the `.` menu on any file reference (file
  tree, history row, blame, diff, stash files, the Files/Staged/Shelf panels)
  offers **Bookmark this file** and **Compare against bookmark**: pick a bookmark
  from the switcher and the focused file is diffed against it. Bookmarking in the
  history view now targets the **selected commit's** version of the file.
- **Context ops** — branch + commit. `gg branch rename <old> <new>` renames a
  local branch (`git branch -m`; the TUI Branches panel `.` menu offers **Rename
  branch** + **Copy branch name**, and the Remotes panel **Copy branch name**).
  `gg commit reword <commit> -m <msg>` changes a commit's message — HEAD is a
  cheap `git commit --amend`, an older commit replays its branch onto its own
  parent in place (later commits preserved), and the root / off-branch commits
  are refused; the TUI Commits panel `.` menu offers **Rename commit** (a popup
  pre-filled with the full message). Reword reuses the interactive-rebase engine.
- CLI/TUI: **fetch + prune** for remote-tracking refs. `gg remote fetch`
  (`git fetch --all`) updates every remote's tracking refs; `gg remote prune`
  drops tracking refs for branches deleted upstream. On the TUI Remotes tab, `f`
  fetches and the `.` menu offers **Prune**. Both refresh the Remotes list and
  the Branches `(↓N)` behind-counts. (The e2e harness gained a `stdout_excludes`
  assertion to prove a ref was pruned.)
- **Bookmarks** — a persistent registry of richly-addressed file references
  (the live-pointer counterpart to the Shelf's frozen copies). A bookmark stores
  the full **address** it was taken from (worktree/branch/commit/shelf-id/path +
  state — its identity *and* its human display) plus a **content determinator**:
  a blob SHA for permanent content (a committed file, a shelf entry → frozen), or
  live-by-address for a worktree's working/index file (re-read on access). The
  address is the identity, not the SHA — a checksum names content, not origin
  (every empty `.gitignore` collides). Backed by the new `internal/bookmark`
  store (a fixed `Store` interface over a `bookmarks.toml` record registry) and a
  `domain.BookmarkBytes` resolver; jump/compare/paste reuse the existing diff
  engine + `engine.WriteFile`.
  - TUI: a **quick-switcher popup** (`g`) — a type-to-filter list; `enter` diffs a
    bookmark against the current working-tree file, `m`+`m` compares two
    bookmarks, `p` pastes to a typed path, `x` removes (confirms). **Bookmark this
    file** is a `.`-menu action wherever a file is focused (Files → unstaged,
    Staged → index, a commit's file tree / history → committed, the Shelf tab →
    shelf entry).
  - CLI: `gg bookmark add [--rev <commit>] [--staged] [--worktree <path>] <path>...`,
    `gg bookmark list`, `gg bookmark rm <id>`, and
    `gg bookmark paste [--force] <id> <dest>` (`<dest>` required).
- CLI: `gg checkout <remote>/<branch> [-s]` checks out a remote-tracking branch
  as a local tracking branch — fast-forward-safe via the same `SmartCheckout`
  engine op as the TUI's Remotes-tab `c`/`s` (reuses an existing local branch
  only if it fast-forwards; refuses a diverged one); `-s` switches to it. `gg
  remote ls` lists remote-tracking branches. The e2e harness gained a
  `stdout_contains` assertion on `[[run]]` steps to cover command output.
- **Shelf** — a non-git, per-file content store of frozen, content-addressed
  copies that survive even permanent deletion of the source (unlike `git
  stash`). Entries persist per-repo under the machine-local state dir, organized
  into named buckets (the `default` bucket is implicit). Backed by the new
  `internal/shelf` store (a fixed `Store` interface over a content-addressed
  file store) and a shared `model.FileRef` ("a file located somewhere") that
  domain resolves to bytes — so comparing any two file versions and writing a
  copy anywhere as unstaged fall out of the existing diff engine plus the new
  `engine.WriteFile` op.
  - TUI: a fourth left-column tab **Shelf** (`B·R·W·S`, `ctrl+←/→` cycles it)
    lists the default bucket. **Add to shelf** is a `.`-menu action available
    wherever a file is focused (Files → unstaged, Staged → index, a commit's
    file tree / file history → that commit). On the tab, `enter` diffs a shelved
    copy against the current working-tree file; the `.` menu offers **Restore
    to…** (writes the copy to a path you type — a destination is mandatory, with
    an Overwrite/Cancel confirm) and **Remove from shelf**.
  - CLI: `gg shelf add [--staged|--rev <commit>] [--bucket <name>] <path>...`,
    `gg shelf list [--bucket]`, `gg shelf rm <entry>`, and
    `gg shelf restore [--force] <entry> <dest>` (`<dest>` required; `--force`
    overwrites an existing differing file, else it refuses).
- CLI: `gg discard [--yes] (--all | <path>...)` discards unstaged changes —
  reverting tracked edits (staged hunks kept) and deleting untracked files —
  through the same engine operation as the TUI's `d`/`D`. Requires `--yes` (or a
  y/N prompt on a terminal); `--all` refuses while the repo is conflicted, and a
  named path must appear in `gg status` (a conflicted path is rejected).
- TUI: new **Remotes** tab — a third tab in the shared left-column slot
  (`Branches · Remotes · Worktrees`) listing remote-tracking branches
  (`refs/remotes`, with the per-remote `HEAD` symref filtered out). `ctrl+←/→`
  now cycles all three tabs; the active tab is spelled out and bracketed in the
  slot header while the inactive tabs show as single-letter markers (`B`/`R`/`W`)
  so all three fit the narrow column. Local **Branches** rows now show a `(↓N)`
  indicator when the branch is behind its upstream. (Commit preview and
  fetch/prune land in follow-ups.)
- TUI: **checkout from the Remotes tab** — `c` materializes the selected
  remote-tracking branch as a local tracking branch (staying on the current
  branch); `s` does the same and switches to it. Both are fast-forward-safe: an
  existing local branch is reused only when it fast-forwards to the remote ref,
  and a diverged branch is refused (never clobbered). `s` autostashes like a
  normal switch.
- TUI: **discard unstaged changes** on the Files panel. `d` discards the marked
  files (or, with nothing marked, the cursor row) — reverting tracked edits
  (keeping any staged hunks) and deleting new untracked files; `D` discards
  **all** unstaged changes. Both prompt for confirmation first. Conflicted files
  are excluded from `d`, and `D` refuses while the repo is conflicted.
- TUI: pressing `s` on a branch that's already checked out in **another
  worktree** now opens a modal offering to **jump to that worktree** (re-root
  the UI and `cd` there on exit), instead of failing with git's "already checked
  out" error. Choosing *cancel* / `esc` does nothing; branches not checked out
  elsewhere still `SmartSwitch` as before.

### Fixed
- TUI: the `/`-search input now rides its **own line beneath the title** in the
  commit files view and in every list pop-up, instead of being appended after the
  title. A long commit subject used to push the search box off the right edge, so
  you couldn't see what you were typing; the query now stays visible regardless of
  title length.
- TUI: cross-store **compare** now stacks instead of vanishing. Pressing `c` in
  the bookmark switcher (compare against a shelf entry) — or `c` in the shelf
  switcher (compare against a bookmark) — opens the other switcher *on top of*
  the first, so `esc` in it returns you to the switcher you started from. It
  used to close the originating switcher before opening the picker, dropping you
  to nothing on `esc`.
- TUI: in the commit files view, `/` now searches the **focused** column. With
  the commit list focused it filters the commits (it used to always filter the
  file tree, regardless of focus); with the tree focused it filters the files as
  before. Committing a commit search (`enter`) reloads the tree for the now-
  selected commit, so "search commits → see its files" needs no extra keypress.
- TUI: the bookmark quick-switcher (`g`) is now truly **global** — it opens from
  every navigable window (the file tree, diff, history, blame, and stash views),
  not just the base panels. Previously each of those windows swallowed `g`
  before it reached the switcher. Once open, the popup is rendered and receives
  keys above whatever window it was launched from.
- TUI: the hunk picker (conflict resolve / `H` staging) now shows a **column
  header** labelling which side is `current`/`incoming` (or `index`/`working`),
  with the active side — the column the cursor edits — highlighted, so the two
  panes are no longer ambiguous. Its action hint also **wraps** across lines
  instead of being truncated at the screen edge, so no command is ever cut off.
- TUI: blaming a commit from a file's history (`b` in the history view) now
  blames the file under the name it had **at that commit**, following renames
  and copies. Previously it always passed the file's current name, so blaming a
  commit that predated a rename failed with `git blame` exit 128 (`fatal: no
  such path … in <sha>`).
- TUI: in the blame view the commit/author/age gutter is now a **frozen left
  column**: `z` wrap mode wraps only the code body (long lines no longer bleed
  across the author column), and scroll mode pans the content while the gutter
  stays put (it used to slide the whole row, sweeping the gutter off-screen).
- TUI: a status-bar message (notably an error like switching to a branch already
  checked out in another worktree) no longer lingers forever — it now clears on
  the next key interaction when idle, so it reflects the most recent action
  instead of persisting across navigation and reloads.

### Changed
- Internal: migrated the worktree, commit, repo, settings, branch, pair-op,
  stash, and stash-action popups onto the unified overlay stack (no
  user-facing change; commit/stash/stash-action popups now also swallow mouse
  like the other dialogs, and the async amend-prefill now stacks the commit
  popup on the overlay layer instead of relying on dispatch shadowing).
- TUI: the **Commits panel now shows all local branches by default**, in date
  order, with branch/HEAD labels on commits (‹*current›‹branch›). The `.` menu on
  the Branches panel offers **Solo this branch** (scope the feed to one branch;
  re-run to un-solo), and **Show all branches** (from the Branches *or* Commits
  menu, when scoped) clears it. The panel header shows the mode —
  `Commits (all)` / `Commits (solo: <branch>)`. The walk is `git log --branches
  --date-order` (paged), supersede-cancellable on giant repos; commits carry
  `%D` ref decorations.
- TUI: the **shelf is now a global `G` quick-switcher popup** (mirroring the
  bookmark `g` popup), replacing the Shelf left-column tab. In it: `enter` diffs
  the entry vs the working-tree file, `p` restores to a typed path, `m`+`m`
  compares two entries, `x` removes (confirms), `/` filters, `z` cycles display
  mode. New cross-store compares: the `.` menu on any file offers **Compare
  against shelf** (alongside Compare against bookmark), and `c` in either popup
  compares the highlighted item against one picked from the other popup
  (bookmark↔shelf). The diff handoff (`openPickerDiff`) clears the surface stack
  so the diff shows even when opened over a history/blame view.
- TUI: the Branches panel now shows each worktree-backed branch's **worktree
  path** in `()` (replacing the `◫` glyph), so you can see where a branch is
  checked out — including the current worktree.
- TUI: **Bookmark this file** / **Add to shelf** now work from a working-tree
  blame/history view (`b`/`h` on a working-tree file). `focusedBookmark` derives
  the worktree/branch from the Model for the `rev==""` case instead of bailing
  out — matching the working-tree diff-view capture.
- Shelf entries now read like bookmarks. Each entry stores a structured
  **origin** (the shared `model.FileAddress` — worktree/branch/commit/state)
  captured at shelve-time and renders `<container> / <state-or-commit> / <path>`
  in the TUI Shelf tab and `gg shelf list`, instead of the terse
  `[source] path #sha`. `gg shelf add` records the worktree/branch it was taken
  from. (`BookmarkState` is renamed `model.FileState`, now shared by both.)
- TUI: the bookmark switcher popup (`g`) now renders through the windowed list
  primitive like the repo switcher — `z` cycles cutoff/wrap/scroll, `shift+←/→`
  pans in scroll mode, the viewport scrolls to keep the selection in view, and
  long rows no longer wrap uncontrolled.
- TUI: the decision modal (e.g. "merge … hit conflicts") now renders centered
  over the interface instead of standalone in the top-left corner.
- TUI: in the conflict resolver (`x`), the whole-file conflict keys are now
  **`C`** keep current / **`i`** keep incoming (were `o`/`t`), matching the hunk
  picker's `current`/`incoming` mnemonic.

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
- TUI `.` action menu: **Copy commit id** (Commits panel), **Copy file path** and **Copy file name** (Files/Staged panels) — written to the clipboard via OSC 52.
- The `.` action menu now lists only **context actions** (the selected row's actions first, then panel/window actions); whole-app actions are no longer included (they remain in the footer with their own hotkeys). The `[ui] menu_actions` allowlist now selects/orders among context actions only.
- The `.` action menu now opens **inside every navigable window** — the commit/stash file tree, the diff view, file history, and blame — not just the panel layout. In a window it offers that window's copy actions (Copy file path / file name, and Copy commit id where a commit is in view); the stash list adds **Copy stash ref**.

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
