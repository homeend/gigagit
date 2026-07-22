# gigagit (`gg`)

A fast terminal git client for very large monorepos — GitKraken's one-key smart
operations with lazygit's keyboard-driven TUI. Cross-platform, shells out to the
system `git`.

> **Status:** actively developed and ready for daily use. The worktree-aware
> engine, one-key smart operations (pull / switch / merge / rebase / commit /
> stash / undo), interactive rebase, conflict resolution, a rich keyboard TUI,
> and a fully scriptable CLI are all in place. No `1.0` tag has been cut yet. An
> MCP server (`gg mcp`) has shipped, stage 1 (read-only) and stage 2 (mutating
> tools, gated by client consent); heavy-ops MCP surfaces remain on the
> roadmap. See [`CHANGELOG.md`](CHANGELOG.md) for the full feature list and
> [`CLAUDE.md`](CLAUDE.md) for the architecture.

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
panel and selected row right now. When the terminal is too narrow for all of
them, whole entries are dropped from the end and the line ends with
`… [?] help` — the dropped keys are listed at the top of the `?` help window
("More keys"), so nothing is ever silently hidden. `?` opens the full
searchable reference.

| Key | Action |
|-----|--------|
| `p` / `P` | pull / push. If a push is **rejected because the remote moved ahead**, a modal offers *rebase onto the remote and push*, *force-push* (chaining the force-with-lease / force confirm), or *abort* (`esc`). `P` pushes the **checked-out** branch; to push a **different** branch, highlight it on the Branches panel and choose **Push `<branch>`** from the `.` menu — it pushes that branch and sets its upstream (works for any local branch, including one never pushed before, without checking it out). To **force-push** directly (after a rebase/amend/reword rewrites history), select the current branch on the Branches panel and choose **Force push `<branch>`** from the `.` menu: a modal offers *force-with-lease* (refuses if the remote moved under you) or *force* (overwrites the remote unconditionally); `esc` aborts. **Branch-tip tag prompt:** if the tip commit of the checked-out branch has any local tags not yet on the remote, `P` runs a fresh `git ls-remote --tags` (5-second budget) to confirm which are missing, then shows a modal — *Push branch + tags* (default) / *Push branch only* / *Cancel*. Choosing *Push branch + tags* pushes the branch first (rejection recovery still applies to the branch push), then immediately chains one `git push origin refs/tags/…` call for all the tip tags; the `▲` pushed-state markers update on success. If the 5s check times out or the remote is unreachable, the prompt is skipped and the branch pushes normally — `P` never hangs. Only the **tip commit's** tags are considered; tags further back in history are not affected |
| `s` | on the Branches panel: smart-switch to the selected branch (if it's already checked out in another worktree, a modal offers to jump to that worktree instead); on the Files panel: open the stash-create popup (name defaults to `WIP on <branch>`, a checklist of unstaged/untracked files, `space` toggles, `ctrl+s` stashes). Slow working-tree ops (switch, pull, merge, rebase, fast-forward, reset, and remote checkout) ask a `y`/`n` confirmation before running (default **No**); disable with `[ui] disable_slow_op_confirm = true` |
| `b` | create a branch off the selected one (popup); `B` create **and** switch to it. Inside the popup, `ctrl+p` picks a saved **branch prefix** (Settings → Branch prefixes) and seeds the name |
| `S` | open the stash window (lists all stashes in the right column): `↑`/`↓` move, `l` shows the selected stash's files in the tree (diff / `h` history / `b` blame, like commit files), `enter` opens an action popup (apply / pop / drop, drop confirms), `esc`/`S` close |
| `u` | undo last commit (ref-only, soft reset) |
| `w` | create a worktree **for the selected branch** (popup); `W` worktree on a **new** templated branch. Inside the popup: `w`/`enter` create, `W` create **and** switch, `e` edit the name, `p` pick a saved **branch prefix** (fills any `<user:…>` labels, then edit) |
| `enter` | on the Branches panel: jump to the selected branch's **tip commit** in the Commits panel (deep-searching unloaded history if needed — the same machinery as `ctrl+f`); on the Worktrees panel: switch into the selected worktree; on the Files panel: full-screen side-by-side diff of the unstaged change (index → working tree); on the Staged panel: the staged diff (HEAD → index); on the files-view tree: diff of the file in the viewed commit. Inside the diff: `↑`/`↓` scroll, `pgup`/`pgdn` page, `n`/`p` (or `ctrl+↑`/`ctrl+↓`) jump between changes, `home`/`end` jump to the top/bottom of the file, then at the edge prime a step to the previous/next file in the list — a bottom-left cue appears and the next press moves to that file, announced by a bottom-left notice naming it (the tree or Status/Staged panel selection follows), `f` toggles full file ↔ changed-lines-only, `z` cycles the text display mode (scroll/wrap/truncate), `←`/`→`/`0` pan in scroll mode, `esc` closes. Changed lines highlight the exact words that differ; commit diffs are cached for instant re-open |
| `ctrl+g` | on the Branches panel: **Solo this branch + go to its tip** — scopes the Commits feed to the branch (same toggle as the `.`-menu Solo: press again to un-solo) and lands the cursor on the tip once the reload finishes; in the **commit popup** (`c`/`C`): **generate a commit message** from the staged diff using a configured `commit_message` external agent (Settings → External tools), run headless — fills the title/description fields for you to review, nothing commits until `ctrl+s`. More than one tool configured shows a numbered chooser; an unapproved command shows a first-run approval (remembered per repo); existing title/description text asks before being replaced; `esc` cancels an in-flight run |
| `z` | cycle the focused window's **text display mode** — cutoff (truncate, default) → wrap → scroll — for any list/tree/text window (panels, stash list, files tree, history, blame) and every list popup (repo switcher, help, conflict resolver, settings, pair-op, stash actions); `shift+←/→` pans horizontally in scroll mode |
| `space` | on the **Files** panel: stage the selected working-tree file (`git add`); on the **Staged** panel: unstage it (`git restore --staged`); conflicted files are skipped; on the **Commits** panel: mark/unmark the selected commit for compare (same ◉ set as `m`, max 2) — marking the second commit opens the two-commit comparison immediately; `esc` clears all marks |
| `H` | on the **Files** panel: open the region/line **staging picker** for the selected tracked file (the same surface as the conflict resolver) — `←`/`→` switch side (index ↔ working), `↑`/`↓` move the line cursor, `space` stages a line (result follows pick order), `c`/`i` take the whole hunk from index/working, `C`/`I` all hunks, `n`/`p` jump, `enter` applies (only the index changes — the working tree is untouched), `esc` cancels. Long lines are readable: `z` cycles the display mode (**scroll** default / wrap / cutoff) and `shift+←/→` pans in scroll mode; the picker scrolls vertically to keep the cursor in view |
| `c` | commit the staged index: opens a popup with a title + multi-line description (`tab` switches fields, `enter` is newline/next, `ctrl+s` commits, `esc` cancels). Like every editable popup field (branch/tag/stash names, worktree fields, …), the text has a visible cursor with full line editing — `←`/`→`, `Home`/`End`, insert/delete at the cursor, word-jumps (`alt`/`ctrl`+`←`/`→`), `ctrl+w`; in the description `↑`/`↓` move between lines |
| `C` | amend the last commit: the same popup opens pre-filled with its message — `ctrl+s` rewrites the message and folds in whatever is currently staged |
| `d` | on the Worktrees panel: delete the selected worktree; on the Branches panel: delete the selected branch; on the **Files** panel: **discard** the marked files (or, with nothing marked, the cursor row) — reverts tracked edits (keeping any staged hunks) and deletes new untracked files, after a confirmation modal; conflicted files are skipped |
| `D` | on the **Files** panel: **discard all** unstaged changes (revert every edit + delete every new file), after a confirmation modal; refuses while the repo is conflicted |
| `m` | mark the selected row; press `m` on a second row of the same panel to open the pair-operation picker (Branches: Merge, Rebase, **Interactive rebase** — the last opens a GitKraken-style editor: per-row `p`/`r`/`s`/`d` = pick/reword/squash/drop, `ctrl+↑/↓` reorder, `enter` start, `R` reset, `esc` cancel; `esc` from the picker clears the mark before clearing the filter). Marking two branches also offers **Compare A ↔ B** — the whole-tree diff between the tips, with an `f`-key filter to show only the files either branch changed since they diverged |
| `l` | on the Commits panel: show the selected commit's files as a directory tree in the left column (`←`/`→`/`tab` switch focus between the tree and the commit list; movement keys act on the focused side — the commits side reloads the tree; `ctrl+↑`/`ctrl+↓` always scroll the tree; `/` searches paths; `esc`/`l` close) |
| `h` | file **history**: on a Files/Staged-panel file, a files-view tree row, or inside the diff view — opens the commits that touched the file (left, newest first) with the file's diff at the selected commit (right); `↑`/`↓` move between commits, `esc`/`h` go back |
| `b` | file **blame**: same entry points as `h` — opens the file with each line tagged by the commit that last changed it (consecutive same-commit lines grouped); `enter` opens that commit's history, `esc`/`b` go back |
| `x` | resolve conflicts, or resume a paused op whose conflicts were already resolved (e.g. outside gg): shown when the repo is conflicted — a `⚠ N conflict` notice appears in the status bar, naming the source: `merging <branch> into <branch>` / `rebasing <branch> onto <branch>`, also shown as the popup subtitle — **or** when a merge/rebase/cherry-pick/revert is paused with nothing left unmerged, in which case the status bar instead shows a persistent `⏸ <op> paused — press [x] to continue or abort` segment (with the source in parens when known, e.g. `⏸ rebase paused (rebasing feature onto main) — press [x] to continue or abort`) and `x` opens straight into the continue/abort step. The first status refresh (`r`, background, file-watch, or startup) that observes that paused-and-resolved state also pushes a **one-shot** popup — **Continue `<op>`** / **Abort `<op>`** / **Not now** (`↑`/`↓` select, `enter` chooses, `c`/`a` direct shortcuts, `esc` = Not now) — so nothing is forced; declining leaves the `⏸` segment and `x` as the way back in, and it won't re-prompt until the paused state actually changes (op continued/aborted, or a fresh conflict appears). Otherwise `x` opens a popup listing each unmerged file. Whole-file actions adapt to the conflict type — both-modified: `enter` opens the **hunk picker** (region/line resolution), `C` keep current / `i` keep incoming / `m` mark resolved; modify-delete: `k` keep modified / `d` delete / `b` keep base. `A` marks all resolved; `↑`/`↓` move; when a merge or rebase is paused, `c` continues (once clean) and `a` aborts; `esc` closes. The popup reopens after each action until the tree is clean. In the hunk picker: a column header labels which side is `current` / `incoming` and highlights the active one; `←`/`→` switch side (current ↔ incoming), `↑`/`↓` move the line cursor, `space` picks a line (result follows pick order), `c`/`i` take the whole region, `C`/`I` take all regions, `n`/`p` jump regions, `enter` applies once every region is resolved, `esc` cancels; `z` cycles the display mode (**scroll** default / wrap / cutoff) and `shift+←/→` pans long lines (the action hint wraps across lines so no command is cut off) |
| `tab` | move focus between panels |
| `shift+tab` | move focus backwards |
| `←`/`→` | focus the left column / the Commits panel (inside the files view: switch between the file tree and the commit list) |
| Commits panel | shows commits from **all local branches** by default, in date order, with branch/HEAD labels (`‹*current›‹branch›`). The `.` menu on the Branches panel offers **Solo this branch** (scope the list to one branch; re-run to un-solo) and **Show all branches** (also on the Commits menu) to clear it; the header shows `Commits (all)` / `Commits (solo: <branch>)`. A single-line **commit graph** (`●─╮`/`│`/`╯` …) is drawn to the left of each commit, showing forks and merges across branches — visible in natural order, hidden while the panel is filtered or re-sorted. `■` marks a local branch's tip; `▲` marks the tip of that branch's tracked remote (`■▲` together = local and remote in sync). When ≥2 local branches tip a commit, `■` gains a **superscript count badge** (`■²`, `■³`, …; dropped when both `■▲` are present). A **decoration group** is rendered **before the subject** listing all extra refs at the commit beyond its primary identity: extra local-branch tips in default color, then tags as `⊙<name>` in **yellow** — including on non-tip lineage rows where the tag actually lives. When the group is too wide for the panel it collapses to `(+N)` (N = extras + tags). Tags are searchable via the Commits `/` filter and `@` highlight. *v1: remote-tracking refs appear only as `▲`, not inside the group; in wrap mode the group still collapses by panel width.* Loads in pages as you scroll |
| `ctrl+←/→` | cycle the **focused** tab slot (and focus it); you can also **click a tab name** in a slot's header to switch directly to it. The **top** slot holds **Branches / Remotes / Worktrees** (active tab spelled out and bracketed, the others as single-letter markers `B`/`R`/`W`); the **middle** (Files) box holds **Files / Tags**; the **bottom** (Staged) box holds **Staged / Reflog**. The **Remotes** tab lists remote-tracking branches (`refs/remotes`); on it, `c` checks out the selected remote branch as a local tracking branch (stay) and `s` checks out and switches to it — both fast-forward-safe (a diverged local branch offers **check out as different name…**, pre-filled with a free `-2`/`-3` suggestion, instead of a dead-end refusal); on the **current** branch's own remote, `c`/`s` prompt instead of erroring — **pull now** (only when actually behind) or **check out as different name…**; `f` fetches all remotes and the `.` menu offers **Check out `<remote>` as…** / **Switch to `<remote>` as…** (a name popup pre-filled with the branch name — materialize the remote branch under a local name you choose), **Prune** (drop tracking refs for branches deleted upstream), **Delete `<remote>/<branch>`** (push `--delete` with a confirm prompt), **Create worktree from** the remote branch, **Merge** it into the current branch, **Rebase** the current branch onto it (merge/rebase hidden on a detached HEAD), and **Copy commit id** / **Copy commit sha** for the branch tip. The **Tags** tab lists tags (`●` annotated / `○` lightweight) with their target and subject; a trailing `▲` marks tags that exist on the default remote (origin if configured, else the first remote) — a tag that is local-only **or that has not been checked this session** shows no marker (deliberately indistinguishable before a lookup runs); `enter` goes to a tag's target commit — jumping the Commits cursor to it when it's in the loaded feed, otherwise opening that commit's files view directly by hash (so it works for old tags far back in history); the `.` menu offers **Check out** / **Push** / **Delete** the tag, **Delete `<tag>` from remote** (confirm prompt), **Annotate `<tag>`** (a message dialog prefilled with the tag's current subject — turns a lightweight tag annotated or updates an annotated tag's message, keeping its target), **Merge `<tag>` into current** and **Rebase current onto `<tag>`** (hidden on a detached HEAD), **Solo this tag** (scope the Commits list to the tag's history), **Refresh remote status** (one-shot `git ls-remote --tags`; annotates every visible tag with `▲` or not; see also `[refresh] remote_tags` for background auto-refresh), plus **Copy tag name** / **Copy commit id** / **Copy commit sha** (the tag's target commit). The **Reflog** tab lists the HEAD reflog (read-only, newest first, per-worktree, capped by `[ui] reflog_limit` — default 200); `enter`/`l` opens an entry's commit in the files view (even dangling commits), and the `.` menu offers **Copy SHA**, **Bookmark this commit**, **Shelf this commit** (freeze its changed files, content-only, into the shelf — see `G`), **Reset to this entry** (soft/mixed/hard), and **Check out this entry** (detached or as a new branch you switch to) — recovery that works on dangling commits. **Bookmark this commit** and **Shelf this commit** open a one-line name popup pre-filled with the commit's subject — edit it, `ctrl+s` inserts the commit's short sha at the cursor, `enter` creates with that name, `esc` cancels; an empty name falls back to the subject for a bookmark, or leaves the shelf entry unlabeled |
| `.` (on a file) | **Add to shelf** — wherever a file is focused (Files, Staged, a commit's file tree, file history), the `.` menu can freeze a copy onto the **shelf**: a non-git, per-file store of frozen copies that survive even permanent deletion of the source. Restore them later to any path as an unstaged change. The same menu offers **Bookmark this file** — a *live* reference (see `g`) rather than a frozen copy — and **Compare against bookmark** / **Compare against shelf** (pick one, then diff the focused file against it). On the Commits panel the `.` menu additionally offers **Shelf this commit** — freeze the selected commit's **changed files** (content only, via `git archive`; no message/author) as one durable shelf entry, so it survives `git gc`/history rewrites the same way a file entry survives deletion of its source (capped at 200MiB); it appears in the `G` switcher, path-less like a commit bookmark. Both **Shelf this commit** and **Bookmark this commit** first prompt for a name (pre-filled with the commit's subject; `ctrl+s` inserts the short sha) — see the Commits panel row above |
| `g` | **bookmark quick-switcher** — a centered, filterable list of bookmarks (richly-addressed references to files anywhere: a worktree's working/index file, a commit/branch file, a shelf entry). Navigation-first: `↑↓/jk` move, `enter` diffs the bookmark against the current working-tree file, `e` opens the bookmarked file in your external editor (`$VISUAL`/`$EDITOR`, read-only), `m`+`m` compares two bookmarks, `c` compares the highlighted bookmark against a shelf entry, `p` pastes its contents to a path you type, `t` **copies it to a temp dir** (see below), `y` copies the bookmarked file's path or name to the clipboard, `x` removes (confirms), `/` filters, `ctrl+t` maximizes the popup to a near-fullscreen box, `esc` closes. A bookmark to a working file is *live* (reflects later edits); to freeze bytes, use the shelf. You can also bookmark a **commit** itself — the Commits panel `.` menu has **Bookmark this commit** — which appears here as a path-less entry showing the name you gave it at creation (or the commit's subject if you left the name blank), e.g. `feat / a1b2c3d — Fix the parser crash`; `enter` on it whole-tree-compares it (base) against the commit selected in the Commits panel, and `a` **cherry-picks it onto the current branch** (confirm first; a bookmark stores no snapshot, so if the commit no longer exists you get a clear notice — shelve commits to keep them applyable) (paste / vs-shelf / mark are file-only; `t` works on it too — see below). The `.` menu on **any file reference** (file tree, a history row, blame, diff, stash files, the Files/Staged panels) offers **Bookmark this file** and **Compare against bookmark** (pick a bookmark, then diff the focused file against it); in history each row is that commit's version of the file |
| `G` | **shelf quick-switcher** — a centered, filterable list of shelved files (frozen copies) and shelved commits (frozen changed-file sets, see `.` → **Shelf this commit**), the counterpart to `g`. A shelved commit shows the name you gave it at creation, if any (` — <name>` after the address), same as `gg shelf list`. Navigation-first: `↑↓/jk` move, `enter` diffs the entry against the working-tree file — on a **shelved commit** it instead opens the **files view** listing every frozen file (the files added/modified at shelve time; deletions carry no content and aren't stored): `enter` on a row diffs the frozen version against the working tree, and the `.` menu offers **View file**, **Open in external editor**, and — the restore path — **Copy to working dir** (writes that file's frozen bytes back to its own path as an unstaged change, overwrite-confirmed), so cherry-picking files out of a shelved commit is browse → diff → copy. `e` opens the shelved copy in your external editor (`$VISUAL`/`$EDITOR`, read-only), `m`+`m` compares two entries, `c` compares the highlighted entry against a bookmark, `p` restores it to a path (prefilled with the original path — enter puts the copy back in place, with an overwrite confirm; edit it to restore elsewhere, `ctrl+r` re-fills the original) — `e`/`m`/`c`/`p` are file-entry keys — `a` on a **shelved commit** cherry-picks it onto the current branch (confirm first): a true `git cherry-pick` while the commit still exists, and if it's been gc'd or the history rewritten, gg re-applies it from the patch snapshot (`git format-patch` mailbox) frozen alongside the files at shelve time (`git am --3way`, atomic; an entry shelved before patch support, or a merge commit, gets a clear notice instead) (CLI: `gg shelf cherry-pick <entry-id>`), `t` **copies it to a temp dir** (see below), `y` copies the file's path or name to the clipboard, `x` removes (confirms), `/` filters, `z` cycles display mode, `ctrl+t` maximizes the popup to a near-fullscreen box, `esc` closes |
| `t` (in `g`/`G`) | **copy to temp dir** — writes the highlighted bookmark's or shelf entry's files to a fixed sibling directory next to the repo, `<repo>.tmp/` (e.g. `/a/x/repo` → `/a/x/repo.tmp`), anchored on the **main** worktree even from a linked one. An editable popup shows the destination, prefilled with a per-type subdirectory name — `commit-<7-char-sha>` for a shelved or bookmarked commit, the entry's id for a shelf file, `bookmark-<label-or-id>` for a bookmarked file — edit it if you want, then `enter` writes (`esc` cancels). An existing target directory prompts overwrite/cancel. Equivalent to `gg shelf export` for a shelf entry |
| mouse | click focuses the window under the cursor and selects the clicked row; **clicking a tab name in a left-column header switches to that tab** (the same slots `ctrl+←/→` cycles); the wheel scrolls the hovered list (`[ui] wheel_step` rows per tick) |
| `j`/`k` or `↑`/`↓` | move selection |
| `pgup`/`pgdn` | move selection by 25% of the panel viewport |
| `o` | cycle the focused panel's sort order (name/date, asc/desc) |
| `/` | filter the focused panel (type, then `enter` to keep, `esc` to clear). The search starts **from the cursor**, not from the top: each keystroke lands on the nearest match at/after the current row (wrapping to the top when every match is above, like `@`), and leaving the filter (`esc`, `ctrl+r`, or switching to `@`) keeps the cursor on the same row in the full list |
| `\` | on the Commits panel: open the **commit feed filter** popup — type a path, author, message substring, and/or date range (`since` / `until`, passed verbatim to `git log`) to narrow the commit list; filters compose with any active branch scope; the commit-graph hides while a filter is active; clear via the `.` menu **Clear filter** row or by opening the popup and erasing all fields. "Commits touching this" in the fuzzy file finder (`F`) and in the files view `.` menu seeds the path field automatically |
| `ctrl+l` | on the Commits panel: load the next batch of history on demand (without waiting to scroll to the bottom) |
| `Home`/`End` | jump to the top / bottom of any navigable list; **End** on the Commits panel also loads the next history batch — press again to walk deeper |
| `ctrl+f` | on the Commits panel: **eager search** — pages unloaded history for the next match of the active `/` filter or `@` highlight query and jumps to it. Every press digs past the already-loaded commits (a hit already on screen doesn't stop it), and gg asks before loading many more pages; the `/` filter stays engaged just like `@` (the query stays visible in the bar), and the last query is remembered so `ctrl+f` keeps digging even after you esc-clear the search |
| `R` | switch repository (popup: type to filter, `enter` to switch, `ctrl+d` to forget, `ctrl+t` maximizes the popup to a near-fullscreen box) |
| `ctrl+o` | **shell escape** — the emergency hatch: suspends gg into an interactive `$SHELL` (`%COMSPEC%` on Windows) in the current worktree, from **any** surface, including mid conflict-resolve or any other window — `exit` returns to gg with a full reload. Never swallowed by whatever window is open, but it waits for a running gg operation to finish first |
| `ctrl+p` | **command palette** — a searchable launcher for commands that don't have (or don't need) their own dedicated key: **Show commit** (`#`), **File history** / **File blame** (type a path — relative, absolute, or `./`-prefixed, normalized to repo-relative — then opens the same `h`/`b` view), **Find** (`F`, the fuzzy file finder), **Open repo** (type a path to a repo not already open; `~` expands to home; an invalid path shows an inline error instead of switching), **Apply patch…** (an editable path popup — applies a patch file to the working tree as unstaged changes, conflicts landing as markers for `x`; a `git format-patch` mailbox offers to recreate its commits instead — see `gg apply` below), **Git config explorer**, **Set up agent skills**, **Open shell** (same as `ctrl+o`), and **Run shell command…** (type one command, run it in the worktree with a press-enter-to-return pause so the output stays on screen; `alt+↓`/`alt+↑` recalls previous commands); `↑`/`↓` select, `enter` runs, `esc` closes |
| `,` | settings: **Identity & profiles** — view/edit the git `user.name`/`user.email` (global vs repo-local, kept distinct) and manage named identity **profiles** (global or per-repo presets; `enter`/`e` prompts *apply to this repo or globally*) — **Branch prefixes**, the **Operation log** toggle, and **Session errors**: a viewer of this session's failed git operations (also written to an always-on `errors.log` in the gg state dir). (Set up agent skills and the Git config explorer moved to the command palette — see `ctrl+p`.) |
| `.` | open the **action menu** (works in every navigable window — panels, the file tree, diff, history, blame, stash): lists context actions for what's in view (row actions first, then panel/window actions; whole-app actions stay in the footer); press an action's key to run it, or `↑`/`↓` + `enter`; `/` filters, `z` cycles display mode, `esc` closes. Includes **Copy commit id** / **Copy commit title** (Commits), **Copy file path** / **Copy file name** (and **Copy stash ref** on the stash list) for whatever the active window shows — copied to the system clipboard (native OS clipboard command, with an OSC 52 fallback for remote/SSH sessions) — plus context write actions: **Copy branch name** / **Copy commit id** / **Copy commit sha** / **Rename branch** on the Branches panel (Copy branch name / commit id / commit sha on Remotes too), and **Rename commit** (reword via a pre-filled message popup) on the Commits panel. On the Commits panel it also offers **Fast-forward `<branch>` to here** when the selected commit is ahead of the current branch's tip (advance the branch with no merge commit), and **Export commit as patch** (hidden for merge commits) — writes a `git am`-able `git format-patch -1 --binary` patch for the whole commit. Inside the diff view, when viewing a commit-vs-parent file diff, the `.` menu offers **Export this file's diff as patch** — the same patch, scoped to that one file. Both open an editable full-path popup pre-filled with `<parent-of-repo>/<shortsha>.patch` (or `<shortsha>-<basename>.patch` for a file); `enter` writes, `esc` cancels — see also `gg commit export-patch` below. To import a patch back, use the command palette's **"Apply patch…"** entry (`ctrl+p`) — see also `gg apply` below. |
| `r` / `q` | reload / quit |
| `?` | help: searchable list of all key bindings (`/` to search; `↑`/`↓` or `j`/`k`, `ctrl+↑`/`ctrl+↓`, `pgup`/`pgdn`, mouse wheel to scroll; `ctrl+t` maximizes the window to a near-fullscreen box; `q` closes) |

When an operation hits a fork (e.g. a diverged branch, or a worktree with
uncommitted changes), a modal asks you to choose; `↑`/`↓` + `enter` to pick,
`esc` to take the safe default.

### CLI

Every smart operation is also scriptable:

```bash
gg status
gg batch [--keep-going]       # run a script of gg commands from stdin against ONE process; framed #<idx> ok/!<exit> sections + a #done trailer; stops on first failure unless --keep-going
gg log [-n N] [<rev>|<A..B>]  # terse "<short-sha> <subject>" history, newest first (default -n 10)
gg diff [--stat|--name-only] [--cached] [<rev>|<A..B>] [-- <paths>...]
                                      # full patch by default; --stat = terse per-file +A -D; --name-only = bare paths
gg show <commit> [--patch] [-- <file>...]   # "<short-sha> <subject>" header + terse stat (default) or full patch
gg review [--tool <name>] [--working] [<rev>|<A..B>]
                                      # AI code review; flags MUST precede the positional (like gg log -n). No positional
                                      # reviews the current branch's work; a single <rev> reviews just that commit's own
                                      # change; --working reviews uncommitted changes. Prints the report to stdout and
                                      # persists it under the gg state dir; --tool picks among configured review commands
gg add (-A | <path>...)       # stage paths, or everything incl. untracked with -A
gg unstage <path>...          # remove paths from the index, keeping working-tree content
gg commit -m "msg"            # add -a to stage tracked changes; --amend rewrites the last commit
gg commit reword <commit> -m "msg"   # change a commit's message (HEAD=amend; older=in-place rebase)
gg commit export-patch <sha> [--out <path>] [--force] [-- <file>]
                                      # write a git am-able patch (git format-patch -1 --binary); with -- <file>, scope to that file; refuses merge commits; default path = parent-of-repo/<name>.patch
gg apply [--am | --working] <path>   # import a patch file (the inverse of export-patch); default = working tree (lands unstaged, conflicts left as markers for [x], exit 1); --am recreates commits from a format-patch mailbox (atomic: rolls back whole on any conflict, exit 1); flags precede the positional
gg pull [--background] [--on-conflict rebase|merge|reset|abort]   # reset = hard-reset to the remote tip, discarding local work
gg push [--force | --force-with-lease] [--on-reject rebase|force|force-with-lease|abort] [<branch>]
                                         # push the current branch, or a named one by ref (no checkout); --on-reject recovers a rejected push (default: fail/prompt)
gg switch <branch>
gg checkout <remote>/<branch> [-s] [--as <local>]   # local tracking branch from a remote ref (ff-safe); -s switches to it; --as names it, and is offered as a hint when a diverged local branch refuses
gg remote ls | fetch | prune         # list remote branches / fetch all / prune deleted
gg remote rm <remote>/<branch>       # delete a remote branch (git push --delete)
gg tag ls                            # list tags (newest first)
gg tag create [-m <msg>] <name> [<commit>]  # create a tag (annotated with -m, else lightweight)
gg tag rm [--remote] <name> [<remote>]  # delete a tag locally; --remote pushes a remote delete (alias: delete)
gg tag checkout [--branch <name>] <tag>  # check out a tag (detached, or onto a new branch)
gg tag push <name> [<remote>]        # push a tag to a remote (auto when only one)
gg tag annotate -m <message> <name>  # set or update a tag's annotation message (turns lightweight → annotated)
gg branch current               # just the branch name (HEAD's short sha when detached)
gg branch ls                    # local branches, "* " marks HEAD, "↑a ↓b" when an upstream exists
gg branch create <name> [<start-point>]
gg branch rename <old> <new>
gg branch delete [--force] <name>
gg versions [<branch>]                 # list a branch's recorded pre-operation snapshots, newest first (default: current branch)
gg versions restore [--discard] <branch> <id|latest>  # restore a branch to a recorded version; --discard answers the dirty-tree prompt
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
gg shelf cherry-pick [--patch] [--on-conflict=keep|abort] <entry-id>  # re-apply a shelved commit: live cherry-pick, or atomic patch replay (git am --3way) once gc'd; --patch forces the replay
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
gg worktree prune                     # drop stale worktree administrative entries
gg repo list
gg repo switch <query>
gg init [--all | --update | --agents <ids> | --list | --to <path>]
                                      # --to: install the skill at a custom path for an
                                      # unsupported agent (file → managed block; directory →
                                      # <dir>/using-gg/SKILL.md); remembered, so --update refreshes it
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

## MCP server (`gg mcp`)

`gg mcp` serves gg's non-git value to AI agents over the Model Context
Protocol (stdio). It deliberately does NOT expose normal git operations —
agents already have the `gg` CLI for those — but the things only gg knows:

- **`gg_ui_state`** — what the gg TUI is showing right now: focused panel,
  cursor commit/branch/tag/worktree, ◉-marked commits, marked files, the open
  diff/compare view and its selected file, the highlighted bookmark/shelf
  entry in an open `g`/`G` switcher, active filters, conflict/paused-op state.
  (The TUI publishes a snapshot file under your XDG state dir; no TUI running
  → `session: null`.)
- **Bookmarks** — `gg_bookmarks_list`, `gg_bookmark_get`, `gg_bookmark_read`.
- **Shelves** — `gg_shelf_buckets`, `gg_shelf_list`, `gg_shelf_commit_files`,
  `gg_shelf_read`.
- **Compare** — `gg_compare_trees` (changed files between worktree/index/any
  commit), `gg_compare_file` (unified diff between any two file versions,
  including bookmarks and shelved-commit members).
- **Export** — `gg_export` copies a bookmark or shelf entry into a local
  directory.
- **Mutating tools (stage 2)** — `gg_cherry_pick` re-applies a shelved or
  bookmarked commit onto the current branch (falling back to the shelved
  commit's stored patch when the original was gc'd), and
  `gg_write_to_worktree` restores/pastes a stored file version as an unstaged
  change. Both are annotated destructive, so your MCP client asks before
  running them; `on_conflict` / `overwrite` parameters control the risky
  paths explicitly.

Stage 1 tools are read-only; stage 2 tools mutate the repository only behind
your MCP client's consent prompt.

Register it with Claude Code from your repo directory:

```sh
claude mcp add gg -- gg mcp
```

## Configuration

Optional `.gg.toml` in the repo (overlaid on a global config) configures worktree
branch/path templates with tokens like `<parent-branch>`, `<repo>`,
`<date:yyyy-MM-dd>` (bare `<date>` defaults to `yyyy-MM-dd`), `<seq:NAME:N>`, and `<user:LABEL>`. Per-repo `<seq>` counters
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

### Config precedence

gg merges configuration field-by-field, later wins:

1. Built-in defaults
2. Global — `~/.config/gg/config.toml`
3. Active per-repo file — **one** file, whichever exists:
   - `~/.config/gg/projects/<encoded-repo-path>/config.toml` (machine-local
     private file; used when present), else
   - `<repo>/.gg.toml` (committed; tracked and shared with everyone who clones)

The private per-repo file lets you keep personal preferences on a shared repo
without committing them. It is keyed on the repo's main-worktree path, so every
linked worktree shares one private config, and when it exists it *replaces* the
committed `.gg.toml` for that repo (per-repo Settings writes also target it).
Settings (`,`) → **Repo settings location** copies or moves the whole config
between the committed and private locations.

On a shared repo the committed `.gg.toml` is git-tracked, so prefer **Copy to
private** — it keeps the committed team baseline in place while your private
file takes effect. **Move to private** deletes `.gg.toml`, which leaves a pending
git deletion in a shared repo.

`[ui] wheel_step` sets the mouse-wheel scroll step in rows (default 3);
`[ui] hscroll_step` sets the diff scroll-mode pan step in columns (default 8);
`[ui] search_history_size` sets how many phrases each search-history ring keeps
(default 20, hard max 1000) — recall them while typing a search with `alt+↑/↓`;
`[ui] reflog_limit` caps how many HEAD reflog entries the Reflog tab loads
(default 200, no upper clamp; git's own `gc.reflogExpire` is the real ceiling);
`[ui] commit_initial_count` sets how many commits are loaded on first paint
(default 300); `[ui] commit_batch_size` sets how many more are loaded per page
(default 300); `[ui] commit_search_max_pages` sets how many extra pages
`ctrl+f` eager search will scan before asking permission to go deeper (default 50);
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
the prompt names — remove the id from the array (or delete the file) to get
prompts back.
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

### Languages

The TUI speaks English (default), 日本語, 한국어, 中文, and Русский: Settings
(`,`) → **Language**, or set `[ui] language = "ja"` directly (the picker
persists the choice to the **global** config). Custom languages or
per-string overrides live in `$XDG_CONFIG_HOME/gg/lang/<code>.toml`:

```toml
[meta]
name = "My language"

[strings]
"Commit" = "…"
"committed %s %s" = "%[2]s — %[1]s …"   # printf verbs may be reordered
```

A new code adds a language; reusing a built-in code (`ja`/`ko`/`zh`/`ru`)
overlays it per-key — fix just the strings you disagree with. Anything
untranslated falls back to English. Operation status lines, progress steps,
and confirmation prompts localize too: the engine always emits them in
English (so the CLI, logs, and agents stay stable), and the TUI renders the
localized form alongside. CLI output is always English — it's the
agent-facing, script-stable surface.

### Notifications

gg checks repo health in the background on every load. When it finds
something worth fixing, a red **`! N notice`** segment blinks in the status
bar; press **`!`** to open the notification center, pick a notice, and choose
an action. The first check targets big repos (≥ 100 MB of packs) without a
commit-graph file: writing one (`git commit-graph write --reachable`) makes
ordered commit browsing roughly 10× faster, and enabling
`fetch.writeCommitGraph` keeps it fresh from then on. Actions: write + keep
fresh, enable only, *Not now* (asks again next load), or *Never for this
repo* (remembered in `<state>/gg/prompts.toml`). The Settings (`,`) →
"Commit-graph" row shows the current state and applies the same fix.

### Git config explorer

The command palette (`ctrl+p`) → **"Git config explorer"** lists every config key git knows
with what's set where: **key | local | global | default**, unset scopes shown
as an explicit `(unset)`. Around 64 common keys are curated — they show git's
real default and a one-line description, and can be edited right there:
**`l`** sets the repo-local value, **`g`** the global one, **`u`** unsets
(you pick which set scope); boolean and enum keys offer a picker, the rest a
text field. Everything else is read-only browsing (use `git config` for
exotic keys). `/` filters as you type; `ctrl+t` maximizes the popup to a near-fullscreen box; `esc` closes.

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

### Branch versions (operations history)

Before any operation that rewrites, replaces, or deletes a branch's
history, gg records the branch's current tip as a hidden git ref
(`refs/gg/versions/<branch>/<unix-ts>-<op>`) — a full-history snapshot
(every commit, message, and author, not a squash) at zero storage cost
that also pins those commits against `git gc`. This is a safety net, not
an opt-in feature: it runs for every branch automatically. What triggers
a snapshot: merging **into** a branch, rebasing it (including an
interactive rebase's squash/move/drop), `--amend`ing the last commit,
undoing the last commit, resetting a branch to its remote tip, deleting a
branch, and `gg pull`'s rebase/merge/reset-to-remote lanes. A plain commit,
a fast-forward pull, cherry-pick, push, stash, and switching branches are
**not** triggers — the old tip stays reachable as an ordinary ancestor, so
nothing needs recording. A snapshot failure never blocks the real
operation (best-effort by design).

Browse a branch's versions from the Branches panel's `.` menu → **"Previous
versions…"**. From the command palette (`ctrl+p`) → **"Branch versions…"**
picks any branch that has recorded versions — including a **deleted** one
— which is how you recover a deleted branch's history. In the popup:
`enter` whole-tree-compares a version against the branch's current tip,
`r` restores it (reset the branch in place, or start a new branch at that
version instead — a non-destructive alternative), `d` deletes just that
snapshot, `y` copies its sha. Settings (`,`) → **"Operations history"**
shows and edits the retention window and toggles recording on/off.

Scriptable: `gg versions [<branch>]` lists a branch's recorded versions,
newest first (default: current branch); `gg versions restore [--discard]
<branch> <id|latest>` restores one (`--discard` answers the "the working
tree has uncommitted changes" prompt for a current-branch restore).

`[versions] disabled` (default `false`) is a kill-switch — set `true` to
stop recording entirely. `[versions] max_age_days` (default `90`) prunes
snapshots older than this on the branch's next write; `-1` keeps them
forever.

**Where it lives, and what pushes.** Versions are ordinary git refs inside
your repo's own `.git` directory (loose files under `.git/refs/gg/versions/…`,
or lines in `.git/packed-refs` once git packs them) — the ref name carries
the metadata, the commits live in the normal object database, and no file
outside the repository is involved. They sit in the repo's *common* git dir,
so every linked worktree sees the same history. Because `refs/gg/*` is
outside `refs/heads`/`refs/tags`, **a normal push or fetch never transfers
them** — the remote and your teammates never see your versions, and a fresh
clone starts with an empty history. The only ways they travel are explicit:
a `--mirror` clone/push (which copies *all* refs), or a hand-written refspec
(`git push origin 'refs/gg/versions/*:refs/gg/versions/*'`).

### External tools

Settings (`,`) → **"External tools…"** probes PATH (plus a few known install
locations) for supported agents/mergetools — currently Claude Code, Junie,
Meld, OpenAI Codex, Antigravity, and Kimi Code — and lets you check off
which ones to write as default commands into the **global** config
(`~/.config/gg/config.toml`); rows already configured are shown checked and
skipped, so the wizard never overwrites an edited command. Manual commands
use the same shape, in either the global config or the repo `.gg.toml`.

In the conflict window (`x`), press **`t`** (shown only when at least one
`conflict` command is configured) to pick one: repo-level agent commands
(Claude Code, Junie, Kimi Code) are always listed while an op is paused, get a per-run
temp **context file** — the paused op, source, target, and the conflicted
paths, one per line — exposed as `<context-file>`/`GG_CONTEXT_FILE` plus ten
more `GG_*` env vars, and either hand over the terminal (interactive agents
like Claude Code and Junie) or run **headless in the background** (`mode =
"capture"` — Kimi Code, whose `kimi -p` draws no terminal UI of its own)
while gg keeps its TUI up with a "Running … [esc] cancel" box; a failed
capture run shows the tail of the agent's output in the error box. Per-file
commands (Meld) are listed when the focused file is a both-sides conflict and
get the
**LOCAL/BASE/REMOTE/MERGED** quartet; if the merged file changes, gg offers
to mark it resolved. The first run of each command shows the fully resolved
text for approval — Run / Cancel — remembered per repo until the command
text changes.

```toml
[[tools.command]]
category = "conflict"
name     = "Meld"
mode     = "terminal"
per_file = true
command  = "meld --auto-merge --output=<merged> <local> <base> <remote>"
```

Tokens for hand-written commands: `<repo>` `<file>` `<local>` `<base>`
`<remote>` `<merged>` `<context-file>` (shell-quoted, path-valued) and
`<op>` `<source>` `<target>` `<conflicted-files>` `<user:LABEL>` (substituted
literally — prefer `"$GG_*"`/`<context-file>` when a value might carry shell
metacharacters).

**Commit-message generation.** Press **`ctrl+g`** in the commit popup (`c`/`C`)
to draft a message from the staged diff with a configured `commit_message`
command — these run **headless** (`mode =
"capture"`, no terminal handover, like a capture-mode conflict command): the staged diff is written to two per-run
files instead of a positional token — `$GG_CONTEXT_FILE` (a labeled summary:
files changed, recent-commit style) and `$GG_STAGED_DIFF` (the full `git diff
--cached`, truncated past a size cap with a note) — and the command's captured
stdout is parsed into a subject + body pair that fills the popup's editable
fields; nothing commits until you press `ctrl+s` yourself. The same chooser,
first-run approval, and per-repo remembering as conflict commands apply; a
confirm-replace prompt also guards against overwriting text already typed
into the fields. Catalog defaults ship for Claude Code, Junie, and Kimi Code
— Junie's is best-effort, since its `--output-format json` `.result` is a
markdown report rather than a clean message, and the parser/editable fields
absorb whatever comes back; Kimi's print-mode stdout is likewise a report,
so its default returns the message by writing `$GG_MESSAGE_FILE`.

```toml
[[tools.command]]
category = "commit_message"
name     = "Claude"
mode     = "capture"
command  = '''claude -p "Write a git commit message for the staged changes. Read the summary at ${GG_CONTEXT_FILE} and the full diff at ${GG_STAGED_DIFF}. Output ONLY the commit message." \
  --output-format json \
  --allowedTools "Read" "Bash(git diff *)" "Bash(git log *)" "Bash(git show *)" "Bash(git status *)"'''
```

**Code review (AI).** The `.` menu offers **"Review this commit"** (Commits
panel — the focused commit's own change), **"Review branch `<name>`"**
(Branches panel — the branch's work since it diverged from `main`, falling
back to its upstream), **"Review working changes"** (Files panel), and
**"Review marked range (AI)"** (Commits panel, when two or more commits are
◉-marked — it reviews the same range **"Compare selection"** shows), each
running a configured `review` command — also `mode = "capture"`, headless —
over the target's diff. The same chooser/first-run-approval gates apply; the
review runs in the background (a blinking `⟳ reviewing <label>…` status names
the scope by **branch name / commit title / range**, not a raw SHA) and on
success the agent's report auto-opens in a new full-screen, read-only viewer
(`↑↓`/`pgup`/`pgdn`/`home`/`end` scroll, `z` wrap mode, `/` search, **`e`**
opens the report file in `$EDITOR`, `esc` closes); a failed or empty run
reports the error in the status line instead. Every report is also written
durably to `<state>/gg/reviews/<repo-key>/<YYYY-MM-DD>/<HH-MM>-<label>.md` (a
per-day folder; the label is the branch name / `<short-sha> <subject>` /
range), so past reviews stay on disk and reopenable. The same pipeline is
scriptable as `gg review`
(see the CLI section above). Catalog defaults ship for Claude Code
(`/code-review <range>`), Junie, and Kimi Code — the Junie and Kimi reports
likewise come back through `$GG_MESSAGE_FILE`, fed the diff via a new
`$GG_REVIEW_DIFF` file (Junie's own `--review` flag can't take a range;
Kimi's print-mode stdout is a report, not the review).

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
