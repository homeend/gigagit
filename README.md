# gigagit (`gg`)

A fast terminal git client for very large monorepos — GitKraken's one-key smart
operations with lazygit's keyboard-driven TUI. Cross-platform, shells out to the
system `git`.

> **Status:** actively developed and ready for daily use. The worktree-aware
> engine, one-key smart operations (pull / switch / merge / rebase / commit /
> stash / undo), interactive rebase, conflict resolution, a rich keyboard TUI,
> and a fully scriptable CLI are all in place. No `1.0` tag has been cut yet, and
> an MCP server for AI agents is on the roadmap. See [`CHANGELOG.md`](CHANGELOG.md)
> for the full feature list and [`CLAUDE.md`](CLAUDE.md) for the architecture.

## Why

Huge repos make ordinary git slow and stateful operations error-prone. `gg`
turns multi-step flows (pull-with-divergence, switch-with-local-changes,
worktree create-and-cd) into single keystrokes that ask you a focused question
only when there's a real decision to make.

## Install

Requires Go 1.26 and a `git` binary on `PATH`.

```bash
# install the latest from GitHub (binary lands in $GOBIN / $GOPATH/bin as `gg`)
go install github.com/homeend/gigagit/cmd/gg@latest

# or, from a checkout
go build ./cmd/gg            # produces ./gg
# or cross-compile both targets
./build.sh all               # ./gg (linux) and ./gg.exe (windows)
```

## Usage

### TUI

Run `gg` with no arguments to open the interactive UI.

The footer is contextual: it lists only the keys that apply to the focused
panel and selected row right now; `?` opens the full searchable reference.

| Key | Action |
|-----|--------|
| `p` / `P` | pull / push. If a push is **rejected because the remote moved ahead**, a modal offers *rebase onto the remote and push*, *force-push* (chaining the force-with-lease / force confirm), or *abort* (`esc`). `P` pushes the **checked-out** branch; to push a **different** branch, highlight it on the Branches panel and choose **Push `<branch>`** from the `.` menu — it pushes that branch and sets its upstream (works for any local branch, including one never pushed before, without checking it out). To **force-push** directly (after a rebase/amend/reword rewrites history), select the current branch on the Branches panel and choose **Force push `<branch>`** from the `.` menu: a modal offers *force-with-lease* (refuses if the remote moved under you) or *force* (overwrites the remote unconditionally); `esc` aborts. **Branch-tip tag prompt:** if the tip commit of the checked-out branch has any local tags not yet on the remote, `P` runs a fresh `git ls-remote --tags` (5-second budget) to confirm which are missing, then shows a modal — *Push branch + tags* (default) / *Push branch only* / *Cancel*. Choosing *Push branch + tags* pushes the branch first (rejection recovery still applies to the branch push), then immediately chains one `git push origin refs/tags/…` call for all the tip tags; the `▲` pushed-state markers update on success. If the 5s check times out or the remote is unreachable, the prompt is skipped and the branch pushes normally — `P` never hangs. Only the **tip commit's** tags are considered; tags further back in history are not affected |
| `s` | on the Branches panel: smart-switch to the selected branch (if it's already checked out in another worktree, a modal offers to jump to that worktree instead); on the Files panel: open the stash-create popup (name defaults to `WIP on <branch>`, a checklist of unstaged/untracked files, `space` toggles, `ctrl+s` stashes). Slow working-tree ops (switch, pull, merge, rebase, fast-forward, reset, and remote checkout) ask a `y`/`n` confirmation before running (default **No**); disable with `[ui] disable_slow_op_confirm = true` |
| `b` | create a branch off the selected one (popup); `B` create **and** switch to it. Inside the popup, `ctrl+p` picks a saved **branch prefix** (Settings → Branch prefixes) and seeds the name |
| `S` | open the stash window (lists all stashes in the right column): `↑`/`↓` move, `l` shows the selected stash's files in the tree (diff / `h` history / `b` blame, like commit files), `enter` opens an action popup (apply / pop / drop, drop confirms), `esc`/`S` close |
| `u` | undo last commit (ref-only, soft reset) |
| `w` | create a worktree **for the selected branch** (popup); `W` worktree on a **new** templated branch. Inside the popup: `w`/`enter` create, `W` create **and** switch, `e` edit the name, `p` pick a saved **branch prefix** (fills any `<user:…>` labels, then edit) |
| `enter` | on the Worktrees panel: switch into the selected worktree; on the Files panel: full-screen side-by-side diff of the unstaged change (index → working tree); on the Staged panel: the staged diff (HEAD → index); on the files-view tree: diff of the file in the viewed commit. Inside the diff: `↑`/`↓` scroll, `pgup`/`pgdn` page, `n`/`p` (or `ctrl+↑`/`ctrl+↓`) jump between changes, `home`/`end` jump to the top/bottom of the file, then at the edge prime a step to the previous/next file in the list — a bottom-left cue appears and the next press moves to that file, announced by a bottom-left notice naming it (the tree or Status/Staged panel selection follows), `f` toggles full file ↔ changed-lines-only, `z` cycles the text display mode (scroll/wrap/truncate), `←`/`→`/`0` pan in scroll mode, `esc` closes. Changed lines highlight the exact words that differ; commit diffs are cached for instant re-open |
| `z` | cycle the focused window's **text display mode** — cutoff (truncate, default) → wrap → scroll — for any list/tree/text window (panels, stash list, files tree, history, blame) and every list popup (repo switcher, help, conflict resolver, settings, pair-op, stash actions); `shift+←/→` pans horizontally in scroll mode |
| `space` | on the **Files** panel: stage the selected working-tree file (`git add`); on the **Staged** panel: unstage it (`git restore --staged`); conflicted files are skipped |
| `H` | on the **Files** panel: open the region/line **staging picker** for the selected tracked file (the same surface as the conflict resolver) — `←`/`→` switch side (index ↔ working), `↑`/`↓` move the line cursor, `space` stages a line (result follows pick order), `c`/`i` take the whole hunk from index/working, `C`/`I` all hunks, `n`/`p` jump, `enter` applies (only the index changes — the working tree is untouched), `esc` cancels. Long lines are readable: `z` cycles the display mode (**scroll** default / wrap / cutoff) and `shift+←/→` pans in scroll mode; the picker scrolls vertically to keep the cursor in view |
| `c` | commit the staged index: opens a popup with a title + multi-line description (`tab` switches fields, `enter` is newline/next, `ctrl+s` commits, `esc` cancels). Like every editable popup field (branch/tag/stash names, worktree fields, …), the text has a visible cursor with full line editing — `←`/`→`, `Home`/`End`, insert/delete at the cursor, word-jumps (`alt`/`ctrl`+`←`/`→`), `ctrl+w`; in the description `↑`/`↓` move between lines |
| `C` | amend the last commit: the same popup opens pre-filled with its message — `ctrl+s` rewrites the message and folds in whatever is currently staged |
| `d` | on the Worktrees panel: delete the selected worktree; on the Branches panel: delete the selected branch; on the **Files** panel: **discard** the marked files (or, with nothing marked, the cursor row) — reverts tracked edits (keeping any staged hunks) and deletes new untracked files, after a confirmation modal; conflicted files are skipped |
| `D` | on the **Files** panel: **discard all** unstaged changes (revert every edit + delete every new file), after a confirmation modal; refuses while the repo is conflicted |
| `m` | mark the selected row; press `m` on a second row of the same panel to open the pair-operation picker (Branches: Merge, Rebase, **Interactive rebase** — the last opens a GitKraken-style editor: per-row `p`/`r`/`s`/`d` = pick/reword/squash/drop, `ctrl+↑/↓` reorder, `enter` start, `R` reset, `esc` cancel; `esc` from the picker clears the mark before clearing the filter) |
| `l` | on the Commits panel: show the selected commit's files as a directory tree in the left column (`←`/`→`/`tab` switch focus between the tree and the commit list; movement keys act on the focused side — the commits side reloads the tree; `ctrl+↑`/`ctrl+↓` always scroll the tree; `/` searches paths; `esc`/`l` close) |
| `h` | file **history**: on a Files/Staged-panel file, a files-view tree row, or inside the diff view — opens the commits that touched the file (left, newest first) with the file's diff at the selected commit (right); `↑`/`↓` move between commits, `esc`/`h` go back |
| `b` | file **blame**: same entry points as `h` — opens the file with each line tagged by the commit that last changed it (consecutive same-commit lines grouped); `enter` opens that commit's history, `esc`/`b` go back |
| `x` | resolve conflicts (shown when the repo is conflicted — a `⚠ N conflict` notice appears in the status bar, naming the source: `merging <branch> into <branch>` / `rebasing <branch> onto <branch>`, also shown as the popup subtitle): opens a popup listing each unmerged file. Whole-file actions adapt to the conflict type — both-modified: `enter` opens the **hunk picker** (region/line resolution), `C` keep current / `i` keep incoming / `m` mark resolved; modify-delete: `k` keep modified / `d` delete / `b` keep base. `A` marks all resolved; `↑`/`↓` move; when a merge or rebase is paused, `c` continues (once clean) and `a` aborts; `esc` closes. The popup reopens after each action until the tree is clean. In the hunk picker: a column header labels which side is `current` / `incoming` and highlights the active one; `←`/`→` switch side (current ↔ incoming), `↑`/`↓` move the line cursor, `space` picks a line (result follows pick order), `c`/`i` take the whole region, `C`/`I` take all regions, `n`/`p` jump regions, `enter` applies once every region is resolved, `esc` cancels; `z` cycles the display mode (**scroll** default / wrap / cutoff) and `shift+←/→` pans long lines (the action hint wraps across lines so no command is cut off) |
| `tab` | move focus between panels |
| `shift+tab` | move focus backwards |
| `←`/`→` | focus the left column / the Commits panel (inside the files view: switch between the file tree and the commit list) |
| Commits panel | shows commits from **all local branches** by default, in date order, with branch/HEAD labels (`‹*current›‹branch›`). The `.` menu on the Branches panel offers **Solo this branch** (scope the list to one branch; re-run to un-solo) and **Show all branches** (also on the Commits menu) to clear it; the header shows `Commits (all)` / `Commits (solo: <branch>)`. A single-line **commit graph** (`●─╮`/`│`/`╯` …) is drawn to the left of each commit, showing forks and merges across branches — visible in natural order, hidden while the panel is filtered or re-sorted. `■` marks a local branch's tip; `▲` marks the tip of that branch's tracked remote (`■▲` together = local and remote in sync). When ≥2 local branches tip a commit, `■` gains a **superscript count badge** (`■²`, `■³`, …; dropped when both `■▲` are present). A **decoration group** is rendered **before the subject** listing all extra refs at the commit beyond its primary identity: extra local-branch tips in default color, then tags as `⊙<name>` in **yellow** — including on non-tip lineage rows where the tag actually lives. When the group is too wide for the panel it collapses to `(+N)` (N = extras + tags). Tags are searchable via the Commits `/` filter and `@` highlight. *v1: remote-tracking refs appear only as `▲`, not inside the group; in wrap mode the group still collapses by panel width.* Loads in pages as you scroll |
| `ctrl+←/→` | cycle the **focused** tab slot (and focus it); you can also **click a tab name** in a slot's header to switch directly to it. The **top** slot holds **Branches / Remotes / Worktrees** (active tab spelled out and bracketed, the others as single-letter markers `B`/`R`/`W`); the **middle** (Files) box holds **Files / Tags**; the **bottom** (Staged) box holds **Staged / Reflog**. The **Remotes** tab lists remote-tracking branches (`refs/remotes`); on it, `c` checks out the selected remote branch as a local tracking branch (stay) and `s` checks out and switches to it — both fast-forward-safe (a diverged local branch is refused); `f` fetches all remotes and the `.` menu offers **Prune** (drop tracking refs for branches deleted upstream), **Delete `<remote>/<branch>`** (push `--delete` with a confirm prompt), **Create worktree from** the remote branch, **Merge** it into the current branch, **Rebase** the current branch onto it (merge/rebase hidden on a detached HEAD), and **Copy commit id** / **Copy commit sha** for the branch tip. The **Tags** tab lists tags (`●` annotated / `○` lightweight) with their target and subject; a trailing `▲` marks tags that exist on the default remote (origin if configured, else the first remote) — a tag that is local-only **or that has not been checked this session** shows no marker (deliberately indistinguishable before a lookup runs); `enter` goes to a tag's target commit — jumping the Commits cursor to it when it's in the loaded feed, otherwise opening that commit's files view directly by hash (so it works for old tags far back in history); the `.` menu offers **Check out** / **Push** / **Delete** the tag, **Delete `<tag>` from remote** (confirm prompt), **Annotate `<tag>`** (a message dialog prefilled with the tag's current subject — turns a lightweight tag annotated or updates an annotated tag's message, keeping its target), **Merge `<tag>` into current** and **Rebase current onto `<tag>`** (hidden on a detached HEAD), **Solo this tag** (scope the Commits list to the tag's history), **Refresh remote status** (one-shot `git ls-remote --tags`; annotates every visible tag with `▲` or not; see also `[refresh] remote_tags` for background auto-refresh), plus **Copy tag name** / **Copy commit id** / **Copy commit sha** (the tag's target commit). The **Reflog** tab lists the HEAD reflog (read-only, newest first, per-worktree, capped by `[ui] reflog_limit` — default 200); `enter`/`l` opens an entry's commit in the files view (even dangling commits), and the `.` menu offers **Copy SHA**, **Bookmark this commit**, **Shelf this commit** (freeze its changed files, content-only, into the shelf — see `G`), **Reset to this entry** (soft/mixed/hard), and **Check out this entry** (detached or as a new branch you switch to) — recovery that works on dangling commits. **Bookmark this commit** and **Shelf this commit** open a one-line name popup pre-filled with the commit's subject — edit it, `ctrl+s` inserts the commit's short sha at the cursor, `enter` creates with that name, `esc` cancels; an empty name falls back to the subject for a bookmark, or leaves the shelf entry unlabeled |
| `.` (on a file) | **Add to shelf** — wherever a file is focused (Files, Staged, a commit's file tree, file history), the `.` menu can freeze a copy onto the **shelf**: a non-git, per-file store of frozen copies that survive even permanent deletion of the source. Restore them later to any path as an unstaged change. The same menu offers **Bookmark this file** — a *live* reference (see `g`) rather than a frozen copy — and **Compare against bookmark** / **Compare against shelf** (pick one, then diff the focused file against it). On the Commits panel the `.` menu additionally offers **Shelf this commit** — freeze the selected commit's **changed files** (content only, via `git archive`; no message/author) as one durable shelf entry, so it survives `git gc`/history rewrites the same way a file entry survives deletion of its source (capped at 200MiB); it appears in the `G` switcher, path-less like a commit bookmark. Both **Shelf this commit** and **Bookmark this commit** first prompt for a name (pre-filled with the commit's subject; `ctrl+s` inserts the short sha) — see the Commits panel row above |
| `g` | **bookmark quick-switcher** — a centered, filterable list of bookmarks (richly-addressed references to files anywhere: a worktree's working/index file, a commit/branch file, a shelf entry). Navigation-first: `↑↓/jk` move, `enter` diffs the bookmark against the current working-tree file, `e` opens the bookmarked file in your external editor (`$VISUAL`/`$EDITOR`, read-only), `m`+`m` compares two bookmarks, `c` compares the highlighted bookmark against a shelf entry, `p` pastes its contents to a path you type, `t` **copies it to a temp dir** (see below), `x` removes (confirms), `/` filters, `esc` closes. A bookmark to a working file is *live* (reflects later edits); to freeze bytes, use the shelf. You can also bookmark a **commit** itself — the Commits panel `.` menu has **Bookmark this commit** — which appears here as a path-less entry showing the name you gave it at creation (or the commit's subject if you left the name blank), e.g. `feat / a1b2c3d — Fix the parser crash`; `enter` on it whole-tree-compares it (base) against the commit selected in the Commits panel (paste / vs-shelf / mark are file-only; `t` works on it too — see below). The `.` menu on **any file reference** (file tree, a history row, blame, diff, stash files, the Files/Staged panels) offers **Bookmark this file** and **Compare against bookmark** (pick a bookmark, then diff the focused file against it); in history each row is that commit's version of the file |
| `G` | **shelf quick-switcher** — a centered, filterable list of shelved files (frozen copies) and shelved commits (frozen changed-file sets, see `.` → **Shelf this commit**), the counterpart to `g`. A shelved commit shows the name you gave it at creation, if any (` — <name>` after the address), same as `gg shelf list`. Navigation-first: `↑↓/jk` move, `enter` diffs the entry against the working-tree file, `e` opens the shelved copy in your external editor (`$VISUAL`/`$EDITOR`, read-only), `m`+`m` compares two entries, `c` compares the highlighted entry against a bookmark, `p` restores it to a path you type, `t` **copies it to a temp dir** (see below), `x` removes (confirms), `/` filters, `z` cycles display mode, `esc` closes |
| `t` (in `g`/`G`) | **copy to temp dir** — writes the highlighted bookmark's or shelf entry's files to a fixed sibling directory next to the repo, `<repo>.tmp/` (e.g. `/a/x/repo` → `/a/x/repo.tmp`), anchored on the **main** worktree even from a linked one. An editable popup shows the destination, prefilled with a per-type subdirectory name — `commit-<7-char-sha>` for a shelved or bookmarked commit, the entry's id for a shelf file, `bookmark-<label-or-id>` for a bookmarked file — edit it if you want, then `enter` writes (`esc` cancels). An existing target directory prompts overwrite/cancel. Equivalent to `gg shelf export` for a shelf entry |
| mouse | click focuses the window under the cursor and selects the clicked row; **clicking a tab name in a left-column header switches to that tab** (the same slots `ctrl+←/→` cycles); the wheel scrolls the hovered list (`[ui] wheel_step` rows per tick) |
| `j`/`k` or `↑`/`↓` | move selection |
| `pgup`/`pgdn` | move selection by 25% of the panel viewport |
| `o` | cycle the focused panel's sort order (name/date, asc/desc) |
| `/` | filter the focused panel (type, then `enter` to keep, `esc` to clear) |
| `\` | on the Commits panel: open the **commit feed filter** popup — type a path, author, message substring, and/or date range (`since` / `until`, passed verbatim to `git log`) to narrow the commit list; filters compose with any active branch scope; the commit-graph hides while a filter is active; clear via the `.` menu **Clear filter** row or by opening the popup and erasing all fields. "Commits touching this" in the fuzzy file finder (`F`) and in the files view `.` menu seeds the path field automatically |
| `ctrl+l` | on the Commits panel: load the next batch of history on demand (without waiting to scroll to the bottom) |
| `Home`/`End` | jump to the top / bottom of any navigable list; **End** on the Commits panel also loads the next history batch — press again to walk deeper |
| `ctrl+f` | on the Commits panel: **eager search** — when the active `/` filter or `@` highlight query has no match in the already-loaded commits, `ctrl+f` pages history until it finds the first hit and jumps to it; if that would load many more pages gg asks first |
| `R` | switch repository (popup: type to filter, `enter` to switch, `ctrl+d` to forget) |
| `,` | settings: **set up agent skills**, **Identity & profiles** — view/edit the git `user.name`/`user.email` (global vs repo-local, kept distinct) and manage named identity **profiles** (global or per-repo presets; `enter`/`e` prompts *apply to this repo or globally*) — **Branch prefixes**, the **Operation log** toggle, and **Session errors**: a viewer of this session's failed git operations (also written to an always-on `errors.log` in the gg state dir) |
| `.` | open the **action menu** (works in every navigable window — panels, the file tree, diff, history, blame, stash): lists context actions for what's in view (row actions first, then panel/window actions; whole-app actions stay in the footer); press an action's key to run it, or `↑`/`↓` + `enter`; `/` filters, `z` cycles display mode, `esc` closes. Includes **Copy commit id** / **Copy commit title** (Commits), **Copy file path** / **Copy file name** (and **Copy stash ref** on the stash list) for whatever the active window shows — copied to the system clipboard (native OS clipboard command, with an OSC 52 fallback for remote/SSH sessions) — plus context write actions: **Copy branch name** / **Copy commit id** / **Copy commit sha** / **Rename branch** on the Branches panel (Copy branch name / commit id / commit sha on Remotes too), and **Rename commit** (reword via a pre-filled message popup) on the Commits panel. On the Commits panel it also offers **Fast-forward `<branch>` to here** when the selected commit is ahead of the current branch's tip (advance the branch with no merge commit), and **Export commit as patch** (hidden for merge commits) — writes a `git am`-able `git format-patch -1 --binary` patch for the whole commit. Inside the diff view, when viewing a commit-vs-parent file diff, the `.` menu offers **Export this file's diff as patch** — the same patch, scoped to that one file. Both open an editable full-path popup pre-filled with `<parent-of-repo>/<shortsha>.patch` (or `<shortsha>-<basename>.patch` for a file); `enter` writes, `esc` cancels — see also `gg commit export-patch` below. |
| `r` / `q` | reload / quit |
| `?` | help: searchable list of all key bindings (`/` to search; `↑`/`↓` or `j`/`k`, `ctrl+↑`/`ctrl+↓`, `pgup`/`pgdn`, mouse wheel to scroll; `q` closes) |

When an operation hits a fork (e.g. a diverged branch, or a worktree with
uncommitted changes), a modal asks you to choose; `↑`/`↓` + `enter` to pick,
`esc` to take the safe default.

### CLI

Every smart operation is also scriptable:

```bash
gg status
gg commit -m "msg"            # add -a to stage tracked changes; --amend rewrites the last commit
gg commit reword <commit> -m "msg"   # change a commit's message (HEAD=amend; older=in-place rebase)
gg commit export-patch <sha> [--out <path>] [--force] [-- <file>]
                                      # write a git am-able patch (git format-patch -1 --binary); with -- <file>, scope to that file; refuses merge commits; default path = parent-of-repo/<name>.patch
gg pull [--background] [--on-conflict rebase|merge|reset|abort]   # reset = hard-reset to the remote tip, discarding local work
gg push [--force | --force-with-lease] [--on-reject rebase|force|force-with-lease|abort] [<branch>]
                                         # push the current branch, or a named one by ref (no checkout); --on-reject recovers a rejected push (default: fail/prompt)
gg switch <branch>
gg checkout <remote>/<branch> [-s]   # local tracking branch from a remote ref (ff-safe); -s switches to it
gg remote ls | fetch | prune         # list remote branches / fetch all / prune deleted
gg remote rm <remote>/<branch>       # delete a remote branch (git push --delete)
gg tag ls                            # list tags (newest first)
gg tag create [-m <msg>] <name> [<commit>]  # create a tag (annotated with -m, else lightweight)
gg tag rm [--remote] <name> [<remote>]  # delete a tag locally; --remote pushes a remote delete (alias: delete)
gg tag checkout [--branch <name>] <tag>  # check out a tag (detached, or onto a new branch)
gg tag push <name> [<remote>]        # push a tag to a remote (auto when only one)
gg tag annotate -m <message> <name>  # set or update a tag's annotation message (turns lightweight → annotated)
gg branch create <name> [<start-point>]
gg branch rename <old> <new>
gg branch delete [--force] <name>
gg merge [--into <target>] [--on-conflict=keep|abort] <source>
gg fast-forward <commit>               # advance the current branch to a descendant commit (no merge commit)
gg rebase [--branch <b>] [--on-conflict=keep|abort] <newbase>
gg rebase -i --plan <file> <newbase>   # interactive rebase from a plan (pick/reword/squash/drop)
gg stash [-m msg] [-u] [-- <paths>...]
gg stash list | apply [<ref>] | pop [<ref>] | drop [<ref>]
gg discard --yes (--all | <path>...)   # discard unstaged: revert edits, delete new files (--all refuses on conflict)
gg shelf add [--staged|--rev <commit>] [--bucket <name>] <path>...  # freeze a non-git copy (survives deletion)
gg shelf commit [--name <name>] <sha>       # freeze a commit's changed files (content only) as one durable entry; --name labels it (shown in list/switcher)
gg shelf list [--bucket <name>] | rm <entry>
gg shelf restore [--force] <entry> <dest>   # write a shelved copy to <dest> as unstaged (dest required)
gg shelf export [--dir <path>] [--force] <entry-id>  # copy an entry's files to a dir outside the repo (default: <repo>.tmp/<name>)
gg bookmark add [--rev <commit>] [--staged] [--worktree <path>] <path>...  # a live reference (default: this worktree's working file)
gg bookmark list | rm <id>
gg bookmark paste [--force] <id> <dest>   # write the bookmark's CURRENT bytes to <dest> as unstaged (dest required)
gg prefix ls                              # list branch prefixes (global + repo)
gg prefix add [--global] <value>          # add a branch prefix (default scope: this repo)
gg prefix rm [--global] <value>           # remove a branch prefix
gg undo
gg worktree list
gg worktree add [<start-point>]
gg worktree add --branch <name>
gg worktree remove [--with-branch] [--force] <path>
gg repo list
gg repo switch <query>
gg init [--all | --update | --agents <ids> | --list]
gg inspect [--debug-dump <path>] [--trace]
gg version                    # (also --version / -v) print build version + commit
```

Forks are answered by flags (e.g. `--on-conflict`, `--with-branch`/`--force`);
without a flag, an interactive terminal prompts, and a non-interactive run errors
asking for the flag.

Every command (and the TUI) accepts a global `--time-track <file>` flag that
appends one JSON span per process start, git subprocess, and operation —
`jq . gg-perf.log` shows where the time went.

### Shell integration (cd-on-switch)

So switching/creating a worktree can move your shell into it:

```bash
# bash/zsh
eval "$(gg shell-init bash)"
# fish
gg shell-init fish | source
```

## Configuration

Optional `.gg.toml` in the repo (overlaid on a global config) configures worktree
branch/path templates with tokens like `<parent-branch>`, `<repo>`,
`<date:YYYY-MM-DD>`, `<seq:NAME:N>`, and `<user:LABEL>`. Per-repo `<seq>` counters
live in `<git-common-dir>/gg/state.toml`.

Run `gg config init --repo` (writes `.gg.toml` at the repo root) or `gg config
init --global` (writes `~/.config/gg/config.toml`) to scaffold a config file
listing every setting commented-out with its default and a description —
uncomment what you want to change. It refuses to overwrite an existing file
without `--force`.

Run `gg config populate (--repo | --global)` to top up an existing config file
with settings added in newer gg versions. Unlike `init`, it never overwrites:
it only inserts the keys you don't have yet, as commented lines marked
`[populated]`, leaving your existing values and comments intact. Safe to re-run.

`[ui] wheel_step` sets the mouse-wheel scroll step in rows (default 3);
`[ui] hscroll_step` sets the diff scroll-mode pan step in columns (default 8);
`[ui] search_history_size` sets how many phrases each search-history ring keeps
(default 20, hard max 1000) — recall them while typing a search with `alt+↑/↓`;
`[ui] reflog_limit` caps how many HEAD reflog entries the Reflog tab loads
(default 200, no upper clamp; git's own `gc.reflogExpire` is the real ceiling);
`[ui] commit_initial_count` sets how many commits are loaded on first paint
(default 300); `[ui] commit_batch_size` sets how many more are loaded per page
(default 300); `[ui] commit_search_max_pages` sets how many extra pages
`ctrl+f` eager search will scan before asking permission to go deeper (default 5);
`[ui] commit_sort` selects commit ordering for the Commits panel and its graph:
`date-order` (the default; `git --date-order`, a global topological sort so the
graph's branch forks always draw correctly) or `plain` (git's lazy newest-first
order — much faster on very large repos, but the graph can draw a disconnected
lane stub when commit dates disagree with topology, e.g. right after a squash).
Cycle it live from the `,` Settings menu ("Commit sort"), which re-walks the feed
and persists the choice to the repo's `.gg.toml` (per-repo on purpose, so a huge
monorepo can opt down to `plain`); the `GG_COMMIT_PAGER` env var still overrides.
`[ui] show_graph` selects how the Commits panel renders on startup: `on` (the
default when the key is missing; the lane graph) or `off` (the flat `●`-gutter
list — the same view as the `.` menu's "Show as list"). Toggle it live from the
`,` Settings menu ("Show graph"), which applies immediately and persists the
choice to the repo's `.gg.toml`; any explicitly set value is remembered per
repo. The `.` menu's "Show as list"/"Show as graph" remains a session-only
flip that doesn't touch the config.
The two settings know about each other: toggling "Show graph" asks (once)
whether to align "Commit sort" with it — `plain` when the graph goes off
(ordering only matters for lanes; plain is much faster on big repos),
`date-order` when it comes back. Answer "No — don't ask again" to silence a
prompt permanently; those choices live in `<state>/gg/prompts.toml`, which
the prompt names — delete the line (or the file) to get prompts back.
`[ui] show_eol_only_changes` (default `false`) controls whether a file whose
only unstaged change is its line endings (CRLF↔LF) is shown as modified — by
default such files are hidden from the Files panel and its count badge as noise;
set it `true` to surface them (e.g. when deliberately renormalizing line
endings). The scriptable `gg status` is unaffected (faithful to `git status`).
Like every entry, the repo's `.gg.toml` overrides the global config per field.

`[debug] log_operations` (default `false`) turns on the **operation log**: a
diagnostic that mirrors every operation and git invocation (argument-redacted) as
JSON lines to `operations.log` in the gg state dir (`$XDG_STATE_HOME/gg/`, else
`~/.local/state/gg/`, `%LocalAppData%\gg\` on Windows). It leaves a trace when an
op hangs or runs slowly. You can also toggle it live from the `,` Settings menu,
which shows the on/off state and the log's full path; toggling there persists the
choice to this key in the global config so it survives restarts.

The `[refresh]` section configures **background auto-refresh** — entirely off by
default. `[refresh] enabled` (default `false`) is the master switch; setting it
`true` (or toggling it live from the `,` Settings menu, which persists the choice)
activates the scheduler. Individual per-source intervals are seconds between silent
background reads; 0 (the default for every key) means that source is never
auto-refreshed:

```toml
[refresh]
enabled     = true   # master switch
status      = 30     # re-read working-tree status every 30 s
branches    = 60
remotes     = 60
worktrees   = 60
tags        = 120
reflog      = 120
feed        = 120    # commit feed
fetch       = 300    # run `git fetch` every 5 min (network; errors swallowed)
remote_tags = 300    # check which tags exist on the remote every 5 min (network; errors swallowed)

min_seconds = 10     # floor on any interval (no source polls faster than this)

# disable_remote_tags_auto = false  # set true to turn off the default auto-refresh (see below)

# Phase D — file-watch: react to .git changes instead of polling
worktrees_watch = false  # true = watch .git/worktrees; falls back to interval on 9p/drvfs
reflog_watch    = false  # true = watch .git/logs/HEAD
branches_watch  = false  # true = watch .git/refs/heads (recursive)
remotes_watch   = false  # true = watch .git/refs/remotes (recursive)
```

Each per-source value is the poll interval in seconds; 0 (the default) means that
source never auto-refreshes. Intervals are floored at `min_seconds` (default 10)
so cheap sources don't hammer the repo. The `fetch` and `remote_tags` rows are
opt-in only (network): each runs solely when set to a non-zero value. A manual
`git fetch` from the Remotes menu does not enable `fetch`; the Tags `.`-menu
**Refresh remote status** action does not enable `remote_tags`. The `remote_tags`
row drives the `▲` tag-pushed-state indicator: it runs `git ls-remote --tags` on
the default remote (origin if present, else the first remote) and updates the `▲`
markers for every visible tag; comparison is by name only (v1).

The `▲` indicator also **auto-refreshes by default** whenever the tag list
changes — on app load and after any tag add/remove/push/delete-from-remote — via
a silent background lookup that is independent of the `[refresh] enabled` master
switch. To disable it: toggle **Settings (`,`) → "Auto remote-tag refresh"**
(persists to the global config), or set `[refresh] disable_remote_tags_auto = true`
in `.gg.toml` (a repo can disable independently of the global setting).

**File-watch mode** (`worktrees_watch`, `reflog_watch`, `branches_watch`,
`remotes_watch`): when enabled, gg watches the relevant `.git` layout paths with
fsnotify and triggers a refresh the moment a change is detected — no polling delay.
Branches and remotes use recursive ref-tree watching (`.git/refs/heads`,
`.git/refs/remotes`). On WSL2 `/mnt` (9p/drvfs) mounts fsnotify cannot watch
Windows filesystem events, so gg automatically falls back to interval polling for
those sources; the "Refresh rates" editor shows `watch (9p→…)` for a watch-enabled
source on such a mount.

Background reads run **one at a time** (FIFO, deduped by type) to cap git
subprocess pressure; manual `r` stays parallel and is unaffected. A small
`⟳ <source>…` hint appears in the status line while the single background lane is
busy — suppressed for reads whose rolling average is under 1 s so quick sources
don't flicker the status bar. The scheduler is suppressed while an operation is
running, a popup/modal is open, or you are typing a search/filter, and a user
action immediately preempts any in-flight background read.

The **Settings (`,`) → "Refresh rates"** entry is an **inline editor**: ↑/↓ selects
a source row, enter opens a numeric field (type the seconds, enter saves, esc
cancels, 0 = off), and `w` toggles **file-watch mode** for sources that support it
(worktrees, reflog, branches, remotes). Saving writes `[refresh] <source>` (or `[refresh] <source>_watch`)
to the **repo `.gg.toml`** and takes effect immediately. Read durations (mean of the
last 10 reads from manual `r` and background reads; app-start load is excluded) are
shown in the `avg` column as informational stats — they do not affect scheduling.

`[ui] footer_actions` and `[ui] menu_actions` are lists of action **ids** that
choose which actions appear in the footer bar and in the `.` menu respectively;
each is unset/empty by default (show everything). Ids: `pull push commit amend
stashes undo order view filter repo settings resolve reload help quit` (globals)
and `switch branch worktree delete-branch delete-worktree mark unmark pair stage
unstage file-diff stash mark-file commit-files switch-worktree` (context). For
example, `footer_actions = ["pull", "commit", "filter"]` shrinks the footer to
those (plus `[.] actions`), leaving everything else one keypress away in the `.`
menu.

### Post-worktree hook

After `gg` creates a worktree it can run a per-repo shell script — handy for
copying gitignored files the new worktree won't have. Set it in `.gg.toml`:

```toml
[worktree]
post_create_hook = '''
cp "$GG_MAIN_WORKTREE/.env" .
make setup
'''
```

Runs with `cwd` = the new worktree. Env: `GG_MAIN_WORKTREE` (the main checkout),
`GG_WORKTREE_PATH`, `GG_BRANCH`, `GG_REPO`. Edit it from Settings (`,`).

**Security:** `.gg.toml` is committable and travels on clone, so `gg` never runs
the hook without showing it and asking first. In the TUI a modal displays the
script (choose run or skip); `h` in the create popup is a pre-skip that
suppresses even the prompt. On the CLI: pass `--hook` to approve without
prompting, `--no-hook` to skip, or omit both to be asked interactively (`gg`
skips automatically when stdin is not a terminal).

### Environment

`GG_COMMIT_PAGER` selects the commit-feed loading strategy: `plain` (default) is
git's lazy newest-first order, which parses only the page on screen — instant
startup even on a multi-million-commit repo. `date-order` opts into
`git log --date-order`, a global topological sort that guarantees a parent never
appears above its child (perfect graph lanes) at the cost of loading the whole
history's ordering (slow on a large repo).

## Development

```bash
go test ./...                # -race before merging
go vet ./... && gofmt -l internal/ cmd/
```

The `e2e/` directory contains a declarative scenario harness: TOML files in
`e2e/scenarios/` describe a starting repo state, a sequence of `gg` CLI
commands, and the expected user-visible outcome (files, branches, stashes, sync
state, history shape). Scenarios are run as standard Go tests and cover
SmartSwitch, SmartPull, stash, commit+push, undo, and worktree add/remove.

See [`CLAUDE.md`](CLAUDE.md) for architecture and contributor conventions.
