# gigagit (`gg`)

[https://gigagit.code-drill.eu/](https://gigagit.code-drill.eu/)

A fast terminal git client for very large monorepos — GitKraken's one-key smart
operations with lazygit's keyboard-driven TUI. Cross-platform, shells out to the
system `git`.

## Why

Huge repos make ordinary git slow and stateful operations error-prone. `gg`
turns multi-step flows (pull-with-divergence, switch-with-local-changes,
worktree create-and-cd) into single keystrokes that ask you a focused question
only when there's a real decision to make.

## Install

Requires a `git` binary on `PATH` (and Go 1.26 for the `go install` / source
routes).

```bash
# Homebrew (macOS / Linux)
brew install homeend/tap/gg

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
searchable reference. An operation error leads with an `[E] full
details` pointer; **`E`** opens the whole message wrapped in a red box (the
status bar is one line and git writes a paragraph of stderr). Every failure
this session is also kept in Settings `,` → Session errors.

| Key | Action |
|-----|--------|
| `p` / `P` | pull / push. If a push is **rejected as non-fast-forward**, gg asks the remote WHY first (one `ls-remote`, nothing is fetched) — git reports the same rejection for two opposite situations. If the remote genuinely **moved ahead**, the modal leads with *rebase onto the remote and push*, then *force-push* (chaining the force-with-lease / force confirm), then *abort* (`esc`). If instead **you rewrote the branch locally** (rebase/amend/squash) and the remote holds nothing but the *old copies* of your own commits, the modal says so and leads with *force-push* — rebasing onto those copies would restore them and replay your rewrite away. A remote carrying **both** (new work plus stale copies) keeps *rebase* first and names both. Every option stays available in all three cases. A push always targets the remote branch of the **same name** — gg spells the destination out, so a `push.default = upstream` or `remote.<name>.push` setting can't silently redirect "push `<branch>`" onto a differently-named remote branch. `P` pushes the **checked-out** branch; to push a **different** branch, highlight it on the Branches panel and choose **Push `<branch>`** from the `.` menu — it pushes that branch and sets its upstream (works for any local branch, including one never pushed before, without checking it out). To **force-push** directly (after a rebase/amend/reword rewrites history), select the current branch on the Branches panel and choose **Force push `<branch>`** from the `.` menu: a modal offers *force-with-lease* (refuses if the remote moved under you) or *force* (overwrites the remote unconditionally); `esc` aborts. **Branch-tip tag prompt:** if the tip commit of the checked-out branch has any local tags not yet on the remote, `P` runs a fresh `git ls-remote --tags` (5-second budget) to confirm which are missing, then shows a modal — *Push branch + tags* (default) / *Push branch only* / *Cancel*. Choosing *Push branch + tags* pushes the branch first (rejection recovery still applies to the branch push), then immediately chains one `git push origin refs/tags/…` call for all the tip tags; the `▲` pushed-state markers update on success. If the 5s check times out or the remote is unreachable, the prompt is skipped and the branch pushes normally — `P` never hangs. Only the **tip commit's** tags are considered; tags further back in history are not affected. **Fetch-refspec mapping:** on a single-branch or shallow (`--depth`) clone, a push can succeed while the branch's remote-tracking ref never moves — the clone's fetch refspec simply doesn't cover it, so the ↓↑ tip markers and ahead/behind can't follow it. When that happens, gg offers to add a mapping for just that branch and fetch it (declining changes nothing else); see also the notification center (`!`), which offers the same fix in one batch for every branch already affected |
| `s` | on the Branches panel: smart-switch to the selected branch (if it's already checked out in another worktree, a modal offers to jump to that worktree instead; if the working tree has staged, unstaged, or conflicted changes, a modal offers to create a worktree for the branch instead, carry the changes over via autostash, or cancel); on the Files panel: open the stash-create popup (name defaults to `WIP on <branch>`, a checklist of unstaged/untracked files, `space` toggles, `ctrl+s` stashes). Slow working-tree ops (switch, pull, merge, rebase, fast-forward, reset, and remote checkout) ask a `y`/`n` confirmation before running (default **No**); disable with `[ui] disable_slow_op_confirm = true` |
| `b` | create a branch off the selected one (popup); `B` create **and** switch to it. Inside the popup, `ctrl+p` picks a saved **branch prefix** (Settings → Branch prefixes) and seeds the name |
| `S` | open the stash window (lists all stashes in the right column): `↑`/`↓` move, `enter` drills into the selected stash's file tree with focus on the tree (`l` opens it focused on the list side; diff / `h` history / `b` blame, like commit files), `.` opens the stash actions menu (apply / pop / drop — drop confirms — plus Copy stash ref), `/` filters the list as you type (ref + subject, case-insensitive; `enter` keeps the filter, `esc` clears it, `ctrl+r` clears a kept one — the same gesture as the Commits panel), `tab`/`←` move focus to the left panels and back (the window stays open), `esc`/`S` close |
| `u` | undo last commit (ref-only, soft reset) |
| `w` | create a worktree **for the selected branch** (popup); `W` opens the same popup with **create & switch** as enter's default — one flow onto the branch's own clean directory (`<repo>.worktrees/<branch>`). The branch name starts as the selection (never templated); `e` edits it — confirming a **different** name creates a **new** branch cut from the selection — and `p` seeds it from a saved **branch prefix** (fills any `<user:…>` labels, then edit). Inside the popup: `enter` runs the popup's default, `w` create only, `W` create **and** switch. `.` on a branch that is checked out nowhere offers **Recycle a worktree…**: check it out in an existing worktree, committing, shelving (one shelf set `WIP on <branch>`; deletions and renames go into a note on the entry, ◆ in the shelf list, `n` reads it) or discarding that worktree's changes first (`gg worktree recycle` from the CLI). The same row on a **remote** branch (Remotes tab) checks it out first — creating or fast-forwarding the local branch, as `c` does — then recycles onto it. The browser UI (`gg web`) has the same **recycle a worktree…** row on its branch and remote right-click menus: a worktree picker, the same commit / shelve / discard / abort prompt, and for a remote branch a local-name prompt first |
| `enter` | on the Branches panel: jump to the selected branch's **tip commit** in the Commits panel (deep-searching unloaded history if needed — the same machinery as `ctrl+f`); on the Worktrees panel: switch into the selected worktree; on the Files panel: full-screen side-by-side diff of the unstaged change (index → working tree) — on a **conflicted** row it instead opens the **hunk picker** for that file directly (the same region/line resolver `x` reaches; `esc`/`ctrl+s` return to the panel; a modify-delete conflict has no regions, so the status line points at `x`); on the Staged panel: the staged diff (HEAD → index); on the files-view tree: diff of the file in the viewed commit. Inside the diff: `↑`/`↓` scroll, `pgup`/`pgdn` page, `n`/`p` (or `ctrl+↑`/`ctrl+↓`) jump between changes, `home`/`end` jump to the top/bottom of the file, then at the edge prime a step to the previous/next file in the list — a bottom-left cue appears and the next press moves to that file, announced by a bottom-left notice naming it (the tree or Status/Staged panel selection follows), `f` toggles full file ↔ changed-lines-only, `ctrl+w` cycles the text display mode (scroll/wrap/truncate), `←`/`→`/`0` pan in scroll mode, `j`/`k` (or `alt+↑/↓`) move the **line cursor** (`↑`/`↓` scroll without moving it; `z` cycles its position center/top/bottom; a click places it), `alt+←`/`alt+→` move it to the **other pane of the same row** — the cursor sits on ONE side, only that side's cell is marked (a `·` gap cell included, so you can see where you are even where the side has no line), the header names it (`old line N` on the left), and the copy, the review-note anchor and the `gg://` link all follow it — `space` starts a **line selection** at the cursor and a second `space` freezes its end (the cursor is then free; a third starts a new range), `enter` copies the selected lines — the cursor side's source text, skipping cells that side hasn't got and collapsed folds — and `esc` unmarks (the side is locked while a selection is live), `e` opens the file in your editor at that line, `c` adds a **review note** at the cursor line, `E`/`R` edit or reply to the nearest one, `a` hides agent notes, `}`/`{` jump between annotated lines, and the `.` menu carries **List notes…** (every thread on the file, type to filter, `enter` jumps to one) and **Remove all notes…** (a typed `remove all` clears this file's notes and their replies) wherever the cursor sits (notes belong to the diff that created them — unstaged, staged, untracked or commit — so a two-sided comparison, or a diff against a shelf entry or bookmark, carries none), `/` searches the text in view (`@` searches backwards) — type to search incrementally from the cursor line, `enter` keeps the query, `esc` cancels it, `]`/`[` step to the next/previous hit (wrapping), `alt+↑`/`alt+↓` recall previous searches; hits are highlighted like word differences and the current one is painted with the `search_current_bg` theme role (a background patch; the `terminal` theme leaves it unset and inverts the hit against its row instead), while a line selection paints its lines with the `selection_bg` role the same two ways, with a hit on an unchanged line lighting up in both columns, `esc` closes. Changed lines highlight the exact words that differ; commit diffs are cached for instant re-open. A diff of **images** (a modified, added or deleted PNG/JPEG/GIF) draws the two thumbnails instead of `(binary file)`: `ctrl+w` cycles side by side → stacked (old above new) → one at a time, where `tab` flips old/new in place; each side has an info line with its format, pixel and byte size. The stacked view (`S`) and the file history show an image file as a small side-by-side thumbnail. Code is syntax-coloured by file type (see `[ui] diff_syntax`) |
| `ctrl+g` | on the Branches panel: **Solo this branch + go to its tip** — scopes the Commits feed to the branch (same toggle as the `.`-menu Solo: press again to un-solo) and lands the cursor on the tip once the reload finishes; on the **Commits panel**: **Solo from the selected commit** — scopes the feed to the history reachable from the commit under the cursor, the commit tree that starts there (press again on the same commit to un-solo; also a `.`-menu row, **Solo from this commit**), with the cursor landing back on the commit after the reload and the header showing `Commits (solo: <short-sha>)`; in the **commit popup** (`c`/`C`): **generate a commit message** from the staged diff using a configured `commit_message` external agent (Settings → External tools), run headless — fills the title/description fields for you to review, nothing commits until `ctrl+s`. More than one tool configured shows a numbered chooser; an unapproved command shows a first-run approval (remembered per repo); existing title/description text asks before being replaced; `esc` cancels an in-flight run |
| `ctrl+\` | the **agent-sessions** popup, from anywhere — even inside a focused agent console: every agent this gg runs, grouped repo → worktree (`enter` opens one, switching to its repository first when another repository owns it; `k` kills, `x` removes an exited one, `/` filters). See [Agent sessions](#agent-sessions-embedded-consoles) |
| `alt+a` / `alt+t` | cycle this repository's running **agent sessions** (`alt+a`) or **terminals** (`alt+t`) by last use, like alt-tab: the first press docks the most recently used one in the Commits column **unfocused**, each further press shows the one used before it (wrapping; exited ones are skipped; the status line says `2 of 5`). `enter` focuses the shown one, which makes it the most recent. "Used" = you focused it in the TUI or typed into it (TUI or web). Inside a focused console the keys go to its program (step out with `ctrl+]` first). A console shows only the repository's own sessions: switching to a repository that does not own the shown one closes its screen (the session keeps running) |
| `alt+x` | **text templates** — reusable multi-line texts (a PR description, a bug report), global or for this repo. The window lists the titles, the selected text and the variables it asks for. `enter` asks for each `<user:LABEL>` value and shows the rendered text; `y` copies it and closes. `n` adds, `e` edits (the title in the window, the text in `$EDITOR`), `d` deletes. Tokens are the branch-prefix ones plus `<branch>`; any other `<…>` stays as written. Also `ctrl+p` → **Text templates…**, `gg template …` from the CLI, and the same view in `gg web` |
| `ctrl+w` | cycle the focused window's **text display mode** — cutoff (truncate, default) → wrap (on words; blame and file previews wrap column-exact) → scroll — for any list/tree/text window (panels, stash list, files tree, history, blame) and every list popup (repo switcher, help, conflict resolver, settings, pair-op); `shift+←/→` pans horizontally in scroll mode |
| `space` | on the **Files** panel: stage the selected working-tree file (`git add`); on the **Staged** panel: unstage it (`git restore --staged`); conflicted files are skipped; the `.` menu on either file panel also offers **Stage all files** (right after Stage file) — every working-tree change into the index in one op, untracked files included — and **Unstage all** — every staged file back out of the index in one op (both hidden while the repo is conflicted or a merge/rebase sits paused); on the **Commits** panel: mark/unmark the selected commit for compare (same ◉ set as `m`, max 2) — marking the second commit opens the two-commit comparison immediately; `esc` clears all marks; on the **Staged** panel, `H` opens the region/line **unstage picker** over the staged change (staged ↔ HEAD, the same picker surface as staging) — taking the HEAD side reverts that region of the index, the working tree is untouched (`git reset -p` style); a newly added file is refused (`space` unstages it whole) |
| `H` | on the **Files** panel: open the region/line **staging picker** for the selected tracked file (the same surface as the conflict resolver) (on the **Staged** panel, `H` opens the mirror **unstage picker**, staged ↔ HEAD) — `←`/`→` switch side (index ↔ working), `↑`/`↓` move the line cursor, `space` stages a line (result follows pick order), `c`/`i` toggle a whole side of the hunk (index/working — left, right, or both can be on; toggle order = result order), `C`/`I` toggle a side across all hunks, `s` resets the hunk to its default and steps on, `n`/`p` jump (wrapping; `enter` = next hunk), `ctrl+s` applies (only the index changes — the working tree is untouched), `esc` cancels. Long lines are readable: `ctrl+w` cycles the display mode (**scroll** default / wrap / cutoff) and `shift+←/→` pans in scroll mode; the picker scrolls vertically to keep the cursor in view, and `alt+↑/↓` scrolls the view freely without moving the cursor (the first plain `↑`/`↓` snaps back); `pgup`/`pgdn` page the cursor / the output pane; `tab` focuses the bottom **output** pane so `↑`/`↓` scroll the assembled result (`tab` again returns to the grid); `ctrl+t` zooms the focused half (grid or output) to the whole box — Tab swaps which half is zoomed, `ctrl+t`/`esc` restores the split |
| `c` | commit the staged index: opens a popup with a title + multi-line description (`tab` switches fields, `enter` is newline/next, `ctrl+s` commits, `esc` cancels). Like every editable popup field (branch/tag/stash names, worktree fields, …), the text has a visible cursor with full line editing — `←`/`→`, `Home`/`End`, insert/delete at the cursor, word-jumps (`alt`/`ctrl`+`←`/`→`), `ctrl+w`; in the description `↑`/`↓` move between lines |
| `C` | amend the last commit: the same popup opens pre-filled with its message — `ctrl+s` rewrites the message and folds in whatever is currently staged |
| `d` | on the Worktrees panel: delete the selected worktree; on the Branches panel: delete the selected branch; on the **Files** panel: **discard** the marked files (or, with nothing marked, the cursor row) — reverts tracked edits (keeping any staged hunks) and deletes new untracked files, after a confirmation modal; conflicted files are skipped |
| `e` | on the Worktrees panel: **rename** the selected worktree's directory — a popup prefilled with just the basename (`git worktree move` under the hood); if it's the worktree gg is standing in, gg follows the rename. The `.` menu adds **Move worktree…** for relocating it to any path (full path in the popup). Both refuse the main worktree; a **locked** worktree asks to unlock and move, or abort |
| `D` | on the **Files** panel: **discard all** unstaged changes (revert every edit + delete every new file), after a confirmation modal; refuses while the repo is conflicted |
| `f` | on the **Branches** panel: jump the cursor to the **checked-out branch** (the list scrolls to it; honors the current sort, and tells you when a `/` filter hides it or HEAD is detached); on the **Worktrees** panel: jump to the worktree gg is running in; on the **Remotes** panel: fetch all remotes; inside a diff: toggle full file ↔ changed lines (the browser UI has the same `f` and a **changes only** toolbar button; its folded runs read `⋯ N unchanged lines` and unfold on click, and the choice is remembered per machine; the browser's `w` — or the toolbar chip beside it — cycles the long-line display **scroll / wrap / cutoff** like the TUI's `ctrl+w`, for the diff, the file-history overlay and blame alike; in the blame overlay `d`/`D` highlight lines by commit age (`-7d`, `+30d`, `+1d -7d`) / turn it off, as in the TUI; in an open diff `↑`/`↓`, `PgUp`/`PgDn` and `Home`/`End` scroll the diff (`j`/`k` still walk the file list behind it) and the blame overlay scrolls for the same keys — the content takes the focus when it opens; in an open diff, in blame and in the `?` help popup, `/` (`@` backward) is the TUI's **in-view search** — a bar under the toolbar, live hit tinting with a `hit/total` count, `enter` keeps the query, `]`/`[` step with wrap, `esc` clears (typing: restores where you were; kept: only the next `esc` leaves); the browser's `S` — or the **stacked** toolbar chip — is the **stacked diff**: every file of the open commit, comparison, preview, PR or working-tree section in one scroll, a sticky header per file (status, path, `+added −deleted`) above its diff, files loading as they scroll near, more than 100 files opening folded, `-` / `_` fold the current / every file, the file list following what you read, and review notes working per file inside it — click a row to mark it, then `c`/`E`/`R` act on that file, `}`/`{` step through the whole stack's notes, and a `gg://` link with a line scrolls to that line in its own file (remembered per machine; the TUI has it too — `S` in the full-screen diff view, where `n`/`p` step file to file, `home`/`end` are the stack's ends, `J` lists its files and `-`/`_` fold, with its own remembered choice — and where review notes work per file: `c`/`E`/`R` act on the file under the cursor, `}`/`{` walk note to note across the whole stack, a `gg://` link lands on the LINE inside it, and `ctrl+↑`/`ctrl+↓` step change to change within one file); inside the file-history overlay, `m` — or the `»` chip in its title — goes **fullscreen**: the commit list folds away and the diff takes the whole screen, `j`/`k` still walk the commits, `m` or `«` brings the list back) |
| `m` | on the **Branches** panel: mark the selected branch; press `m` on a second branch to open the pair-operation picker (Merge, Rebase, **Fast-forward** when one branch is strictly behind the other — direction auto-detected, no merge commit, **Interactive rebase** — the last opens a GitKraken-style editor: per-row `p`/`r`/`s`/`d` = pick/reword/squash/drop, `ctrl+↑/↓` reorder, `enter` start, `R` reset, `esc` cancel; `esc` from the picker clears the mark before clearing the filter). Marking two branches also offers **Compare A ↔ B** — the whole-tree diff between the tips, with an `f`-key filter to show only the files either branch changed since they diverged. On the **Files**/**Staged** panels `m` multi-selects files (for staging, stashing, discarding — `esc` drops every file mark at once); on **Commits** it toggles the ◉ compare set (see `space`). On other panels `m` does nothing |
| `l` | on the Commits panel: show the selected commit's files as a directory tree in the left column. A line under the title carries the commit's **date and author** (`YYYY-MM-DD HH:MM · <author>`, the author date — the same stamp `i` shows), so you can see when a commit landed without opening its message; walking the commit list keeps it in step, and it is fetched even for a commit opened by hash from **Tags**, **goto-SHA** (`#`) or the **Reflog**. A comparison (two endpoints) has no single date and shows no such line. The browser UI (`gg web`) draws the same line under its file-list title, in the same format and the viewer's own timezone — including for a commit opened from a sidebar tag, which has no feed row to take a date from. (`←`/`→`/`tab` switch focus between the tree and the commit list; movement keys act on the focused side — the commits side reloads the tree; `ctrl+↑`/`ctrl+↓` always scroll the tree; `/` searches paths — with the **View file** preview focused it searches the previewed content instead (`@` backwards, `]`/`[` step the hits and land the preview's **line cursor** on them, `esc` clears a live search first, then a line selection, then a kept search, then closes the preview); in that preview `alt+↑`/`alt+↓` move the line cursor (plain `↑`/`↓` keep scrolling the viewport), `space`/`space` select a range of lines and `enter` copies them as the file's own text (tabs intact), and the `.` menu carries **Copy line** and **Copy selected lines**; `esc`/`l` close) |
| `h` | file **history**: on a Files/Staged-panel file, a files-view tree row, or inside the diff view — opens the commits that touched the file (left, newest first) with the file's diff at the selected commit (right); `↑`/`↓` move between commits, `esc`/`h` go back |
| `b` | file **blame**: same entry points as `h` — opens the file with each line tagged by the commit that last changed it (consecutive same-commit lines grouped, the code syntax-coloured by file type — the same lexers and `[ui] diff_syntax` switch as the diff views); `enter` opens that commit's history, `space`/`space` select a range of lines and `enter` then copies them instead (the `.` menu offers **Copy line** and **Copy selected lines** too), `/` (or `@`) finds text in the file with `]`/`[` stepping the hits, `d` **highlights lines by commit age**: type `-7d` (younger than a week), `+30d` (older than a month) or `+1d -7d` (between; both inclusive, order-free) — a bare span like `7d` or `1d 3h 5m` means younger than, a bare number is days, a bare unit is one of it (`+w`); measured from now, uncommitted lines are age 0; the header shows the filter; `D` turns it off, `d` edits it; off whenever blame opens, the last text prefills the dialog; tint = theme role `blame_recent_bg`, bold under the `terminal` theme), `esc`/`b` go back (a live search takes the first `esc`, then a line selection, then a kept search; `b` always goes back) |
| `x` | resolve conflicts, or resume a paused op whose conflicts were already resolved (e.g. outside gg): shown when the repo is conflicted — a `⚠ N conflict` notice appears in the status bar, naming the source: `merging <branch> into <branch>` / `rebasing <branch> onto <branch>`, also shown as the popup subtitle — **or** when a merge/rebase/cherry-pick/revert is paused with nothing left unmerged, in which case the status bar instead shows a persistent `⏸ <op> paused — press [x] to continue or abort` segment (with the source in parens when known, e.g. `⏸ rebase paused (rebasing feature onto main) — press [x] to continue or abort`) and `x` opens straight into the continue/abort step. The first status refresh (`r`, background, file-watch, or startup) that observes that paused-and-resolved state also pushes a **one-shot** popup — **Continue `<op>`** / **Abort `<op>`** / **Not now** (`↑`/`↓` select, `enter` chooses, `c`/`a` direct shortcuts, `esc` = Not now) — so nothing is forced; declining leaves the `⏸` segment and `x` as the way back in, and it won't re-prompt until the paused state actually changes (op continued/aborted, or a fresh conflict appears). Otherwise `x` opens a popup listing each unmerged file. Whole-file actions adapt to the conflict type — both-modified: `enter` opens the **hunk picker** (region/line resolution), `C` keep current / `i` keep incoming / `m` mark resolved; modify-delete: `k` keep modified / `d` delete / `b` keep base. `A` marks all resolved; `↑`/`↓` move; when a merge or rebase is paused, `c` continues (once clean) and `a` aborts; `esc` closes. The popup reopens after each action until the tree is clean. In the hunk picker: a column header labels which side is `current` / `incoming` and highlights the active one; `←`/`→` switch side (current ↔ incoming), `↑`/`↓` move the line cursor, `space` picks a line (result follows pick order), `c`/`i` toggle a whole side of the region (left, right, or both — toggle order = result order; checkboxes at the side/region/line levels show the state), `C`/`I` toggle a side across all regions, `s` **skips** the region (resolved with no lines from either side, then on to the next undecided; `s` again un-skips), a bottom **output** pane previews the assembled result live (`o` collapses it), `n`/`p` jump regions (wrapping), `enter` jumps to the next region still undecided (wrapping from the last to the first), `ctrl+s` applies once every region is resolved, `/` (or `@`) finds text across the candidate lines — the 2D cursor jumps to the hit, `]`/`[` step (the literal context and the output pane are not searched) —, `esc` cancels (clearing a live search first); `ctrl+w` cycles the display mode (**scroll** default / wrap / cutoff) and `shift+←/→` pans long lines (the action hint wraps across lines so no command is cut off); `alt+↑/↓` scrolls the view without moving the line cursor, and the first plain `↑`/`↓` snaps the view back to the selection; `pgup`/`pgdn` move a viewport page at a time (the line cursor in the grid, the scroll in the output pane); `tab` switches focus to the **output** pane (`▶` on its rule) where `↑`/`↓` scroll the whole result end to end — selection keys wait until `tab` returns to the grid, a collapsed pane expands on `tab`, and leaving the pane resumes cursor-following; `ctrl+t` zooms the focused half (grid or output) to the whole box — Tab swaps which half is zoomed, `ctrl+t`/`esc` restores the split |
| `tab` | move focus between panels |
| `shift+tab` | move focus backwards |
| `←`/`→` | focus the left column / the Commits panel (inside the files view: switch between the file tree and the commit list) |
| Commits panel | shows commits from **all local branches** by default, in date order, with branch/HEAD labels (`‹*current›‹branch›`). The `.` menu on the Branches panel offers **Solo this branch** (scope the list to one branch; re-run to un-solo) and **Show all branches** (also on the Commits menu) to clear it; the Commits `.` menu adds **Solo from this commit** (`ctrl+g`) — scope the feed to the history reachable from the selected commit; the header shows `Commits (all)` / `Commits (solo: <branch or short-sha>)`. A single-line **commit graph** (`●─╮`/`│`/`╯` …) is drawn to the left of each commit, showing forks and merges across branches — visible in natural order, hidden while the panel is filtered or re-sorted. `■` marks a local branch's tip; `▲` marks the tip of that branch's tracked remote (`■▲` together = local and remote in sync). When ≥2 local branches tip a commit, `■` gains a **superscript count badge** (`■²`, `■³`, …; dropped when both `■▲` are present). A **decoration group** is rendered **before the subject** listing all extra refs at the commit beyond its primary identity: extra local-branch tips in default color, then tags as `⊙<name>` in **yellow** — including on non-tip lineage rows where the tag actually lives. When the group is too wide for the panel it collapses to `(+N)` (N = extras + tags). Tags are searchable via the Commits `/` filter and `@` highlight. *v1: remote-tracking refs appear only as `▲`, not inside the group; in wrap mode the group still collapses by panel width.* Loads in pages as you scroll |
| `ctrl+←/→` | cycle the **focused** tab slot (and focus it); you can also **click a tab name** in a slot's header to switch directly to it. The **top** slot holds **Branches / Remotes / Worktrees / Previews** (active tab spelled out and bracketed, the others as single-letter markers `B`/`R`/`W`/`P`); the **middle** (Files) box holds **Files / Tags**; the **bottom** (Staged) box holds **Staged / Reflog**. The **Previews** tab lists saved merge previews — a GitHub-PR-style diff of `source → target` (`git diff target...source`, merge-base to source tip) rather than a tip-to-tip compare — as `label  source → target  N files ↑M` rows (or `merged` / `missing: <name>` / `no common base` when a state prevents the diff; saved pairs store branch NAMES and recompute from the live tips on every refresh). `enter` opens the pair in the compare view (titled `Merge preview: source → target`; the `f` origin filter is inert there); `a` adds a pair via a two-field form with fuzzy completion over local and remote-tracking branch names (`tab` accepts/moves fields, `ctrl+s` swaps source/target, `enter` on the target field saves and opens it); `e` renames the selected pair; `d` removes it (confirms); `s` saves the reversed pair (target → source) as a new entry. The tab also lists **saved commit pairs** — the diff between two FROZEN commits, as `label  <a7>..<b7>  N files` (or `missing commit: <sha7>`): save one from the Commits panel by marking exactly two commits with `m` and picking **Save to previews** / **Save reversed to previews** in the `.` menu (older → newer, the direction *Compare selection* uses; the entry never follows a branch); `enter` opens it titled `Saved diff: <label>`, and `e` / `d` / `s` / copy-link work as on a preview row. A saved diff carries **review notes** like a preview does — they sit on the newer commit (new side only), `◆N` counts them on the row and per file, `c` adds one and `}` / `{` step between them; a pair opened from a `gg://…@a..b` link shows them too. A recorded branch version copies as its preview link (`?version=`), so a "looks fishy" rebase can be handed to an agent as the pre-operation change set. The Branches panel's pair picker (`m`+`m`) also offers **Merge preview A → B…**, a dialog with *show once* / *show and save* / *swap direction* / *abort*. It also offers **Symmetric merge preview A, B…**: type a base (the field starts empty — there is no default; `esc` returns to the picker) and gg saves ONE comparison of what A and what B would each bring into that base, opens it behind a *comparing…* popup, and lists it on the Previews tab as a `sym` row (`A vs B  base: <base>`); its `.` menu also opens either side's merge preview. `enter` on any saved comparison shows the same *comparing…* popup while it loads. An already-open preview re-opens itself when either side's tip moves (a bottom-left notice names which side), silently reconciles when only the target moved without changing the diff, and closes with a notice when the pair becomes merged, a side goes missing, or the pair itself is removed. A preview's diff rows also carry **review notes**, gathered along the whole branch from the merge base to the source tip (not just the tip's own change), so a note survives the agent pushing more commits; a note whose lines a later commit moved is listed **outdated** rather than dropped. The **Remotes** tab lists remote-tracking branches (`refs/remotes`); on it, `c` checks out the selected remote branch as a local tracking branch (stay) and `s` checks out and switches to it — both fast-forward-safe (a diverged local branch offers **check out as different name…**, pre-filled with a free `-2`/`-3` suggestion, instead of a dead-end refusal); on the **current** branch's own remote, `c`/`s` prompt instead of erroring — **pull now** (only when actually behind) or **check out as different name…**; `f` fetches all remotes and the `.` menu offers **Check out `<remote>` as…** / **Switch to `<remote>` as…** (a name popup pre-filled with the branch name — materialize the remote branch under a local name you choose), **Prune** (drop tracking refs for branches deleted upstream), **Delete `<remote>/<branch>`** (push `--delete` with a confirm prompt), **Create worktree from** the remote branch, **Merge** it into the current branch, **Rebase** the current branch onto it (merge/rebase hidden on a detached HEAD), and **Copy commit id** / **Copy commit sha** for the branch tip. The **Tags** tab lists tags (`●` annotated / `○` lightweight) with their target and subject; a trailing `▲` marks tags that exist on the default remote (origin if configured, else the first remote) — a tag that is local-only **or that has not been checked this session** shows no marker (deliberately indistinguishable before a lookup runs); the browser UI (`gg web`) marks its sidebar tags the same way, adding a dim `▵` for a tag the remote is known **not** to have — and hides **delete … from remote** for such a tag; `enter` goes to a tag's target commit — jumping the Commits cursor to it when it's in the loaded feed, otherwise opening that commit's files view directly by hash (so it works for old tags far back in history); the `.` menu offers **Check out** / **Push** / **Delete** the tag, **Delete `<tag>` from remote** (confirm prompt), **Annotate `<tag>`** (a message dialog prefilled with the tag's current subject — turns a lightweight tag annotated or updates an annotated tag's message, keeping its target), **Merge `<tag>` into current** and **Rebase current onto `<tag>`** (hidden on a detached HEAD), **Solo this tag** (scope the Commits list to the tag's history), **Refresh remote status** (one-shot `git ls-remote --tags`; annotates every visible tag with `▲` or not; see also `[refresh] remote_tags` for background auto-refresh), plus **Copy tag name** / **Copy commit id** / **Copy commit sha** (the tag's target commit). The **Reflog** tab lists the HEAD reflog (read-only, newest first, per-worktree, capped by `[ui] reflog_limit` — default 200); `enter`/`l` opens an entry's commit in the files view (even dangling commits), and the `.` menu offers **Copy SHA**, **Bookmark this commit**, **Shelf this commit** (freeze its changed files, content-only, into the shelf — see `G`), **Reset to this entry** (soft/mixed/hard), and **Check out this entry** (detached or as a new branch you switch to) — recovery that works on dangling commits. **Bookmark this commit** and **Shelf this commit** open a one-line name popup pre-filled with the commit's subject — edit it, `ctrl+s` inserts the commit's short sha at the cursor, `enter` creates with that name, `esc` cancels; an empty name falls back to the subject for a bookmark, or leaves the shelf entry unlabeled |
| `.` (on a file) | **Add to shelf** — wherever a file is focused (Files, Staged, a commit's file tree, file history), the `.` menu can freeze a copy onto the **shelf**: a non-git, per-file store of frozen copies that survive even permanent deletion of the source. Restore them later to any path as an unstaged change. The same menu offers **Bookmark this file** — a *live* reference (see `g`) rather than a frozen copy — and **Compare against bookmark** / **Compare against shelf** (pick one, then diff the focused file against it). On the Commits panel the `.` menu additionally offers **Shelf this commit** — freeze the selected commit's **changed files** (content only, via `git archive`; no message/author) as one durable shelf entry, so it survives `git gc`/history rewrites the same way a file entry survives deletion of its source (capped at 200MiB); it appears in the `G` switcher, path-less like a commit bookmark. Both **Shelf this commit** and **Bookmark this commit** first prompt for a name (pre-filled with the commit's subject; `ctrl+s` inserts the short sha) — see the Commits panel row above |
| `F` | **files in the working tree** — a window over the whole left column listing every file on disk (tracked files minus deleted ones, plus untracked ones; no extra git scan — the untracked list is the Status panel's) as the commit view's tree: root files first, then one bold `dir/` heading per directory with its files indented beneath — the list's first line always names the directory of the row under it (`.` for root files), so a long directory scrolled past its own heading still says where you are — each row leading with its status letter (`M`, `A`, … from the Status panel, `?` for an untracked file, blank when clean). `/` then a query filters (`enter` keeps it, `esc` clears it) in fzf's syntax, scored by fzf's own matcher: a plain term is fuzzy (`fvgo` → `files_view.go`), `'darwin` needs the literal substring, `^docs` a prefix, `.md$` a suffix, `!test` excludes, and space-separated terms must all match (a capital letter makes a term case-sensitive); the filter line is an editable field: `←`/`→` move the cursor, `ctrl+←`/`ctrl+→` (or `alt+`) by word, `home`/`end`, `backspace`/`delete` erase around it, `ctrl+w` a word — the matches stay grouped under their directories and the cursor lands on the best one; `↑↓/jk/pgup/pgdn` move and the right column **previews** the file on disk (it follows the cursor once it rests, and reloads when the file changes); a PNG/JPEG/GIF is drawn as half-block cells scaled to the pane (an info line gives its format, pixel and byte size; a colourless terminal gets a glyph ramp), any other binary file shows a one-line placeholder instead of its bytes; `→` scrolls the preview, `←` returns. `enter` or `.` opens the file's actions: **View file content** (the full-screen viewer — an open file, see *Open files*), **Diff (HEAD ↔ working tree)**, **History**, **Blame**, **Edit in editor**, **Copy file name / path / absolute path**, **Copy file link**, **Commits touching this** (the git rows are for tracked files only). `ctrl+t` spreads the list over the whole screen; `esc` closes, back to where you were (over a diff, history or blame, back to it) |
| `g` | **bookmark quick-switcher** — a centered, filterable list of bookmarks (richly-addressed references to files anywhere: a worktree's working/index file, a commit/branch file, a shelf entry). Navigation-first: `↑↓/jk` move, `enter` diffs the bookmark against the current working-tree file, `e` opens the bookmarked file in your external editor (`$VISUAL`/`$EDITOR`, read-only), `m`+`m` compares two bookmarks, `c` compares the highlighted bookmark against a shelf entry, `p` pastes its contents to a path you type, `t` **copies it to a temp dir** (see below), `y` copies the bookmarked file's path or name to the clipboard, `x` removes (confirms), `/` filters, `ctrl+t` maximizes the popup to a near-fullscreen box, `esc` closes. A bookmark to a working file is *live* (reflects later edits); to freeze bytes, use the shelf. You can also bookmark a **commit** itself — the Commits panel `.` menu has **Bookmark this commit** — which appears here as a path-less entry showing the name you gave it at creation (or the commit's subject if you left the name blank), e.g. `feat / a1b2c3d — Fix the parser crash`; `enter` on it whole-tree-compares it (base) against the commit selected in the Commits panel, and `a` **cherry-picks it onto the current branch** (confirm first; a bookmark stores no snapshot, so if the commit no longer exists you get a clear notice — shelve commits to keep them applyable). `m`+`m` and `c` also work on a commit bookmark — a whole-tree compare against another marked commit bookmark, or (via `c`) against a shelved commit — live sha-vs-sha while both commits still exist, falling back to a gc'd shelved side's frozen snapshot (the compare title marks it "frozen"); paste stays file-only, `t` works on it too — see below. The `.` menu on **any file reference** (file tree, a history row, blame, diff, stash files, the Files/Staged panels) offers **Bookmark this file** and **Compare against bookmark** (pick a bookmark, then diff the focused file against it); in history each row is that commit's version of the file |
| `G` | **shelf quick-switcher** — a centered, filterable list of shelved files (frozen copies) and shelved commits (frozen changed-file sets, see `.` → **Shelf this commit**), the counterpart to `g`. A shelved commit shows the name you gave it at creation, if any (` — <name>` after the address), same as `gg shelf list`. Navigation-first: `↑↓/jk` move, `enter` diffs the entry against the working-tree file — on a **shelved commit** it instead opens the **files view** listing every frozen file (the files added/modified at shelve time; deletions carry no content and aren't stored): `enter` on a row diffs the frozen version against the working tree, and the `.` menu offers **View file** (the right-column content preview, syntax-coloured by file type), **Open in external editor**, and — the restore path — **Copy to working dir** (writes that file's frozen bytes back to its own path as an unstaged change, overwrite-confirmed), so cherry-picking files out of a shelved commit is browse → diff → copy. `e` opens the shelved copy in your external editor (`$VISUAL`/`$EDITOR`, read-only), `m`+`m` compares two entries, `c` compares the highlighted entry against a bookmark, `p` restores it to a path (prefilled with the original path — enter puts the copy back in place, with an overwrite confirm; edit it to restore elsewhere, `ctrl+r` re-fills the original) — `e`/`p` are file-entry keys, while `m`+`m` and `c` also work on a **shelved commit**: a whole-tree compare against another marked shelved commit, or (via `c`) against a commit bookmark — live sha-vs-sha while both commits still exist, falling back to this or the other side's frozen tar after a gc (the compare title marks a frozen side "frozen"; the fallback scopes to the frozen side's members) — `a` on a **shelved commit** cherry-picks it onto the current branch (confirm first): a true `git cherry-pick` while the commit still exists, and if it's been gc'd or the history rewritten, gg re-applies it from the patch snapshot (`git format-patch` mailbox) frozen alongside the files at shelve time (`git am --3way`, atomic; an entry shelved before patch support, or a merge commit, gets a clear notice instead) (CLI: `gg shelf cherry-pick <entry-id>`), `t` **copies it to a temp dir** (see below), `y` copies the file's path or name to the clipboard, `x` removes (confirms), `/` filters, `ctrl+w` cycles display mode, `ctrl+t` maximizes the popup to a near-fullscreen box, `esc` closes |
| `t` (in `g`/`G`) | **copy to temp dir** — writes the highlighted bookmark's or shelf entry's files to a fixed sibling directory next to the repo, `<repo>.tmp/` (e.g. `/a/x/repo` → `/a/x/repo.tmp`), anchored on the **main** worktree even from a linked one. An editable popup shows the destination, prefilled with a per-type subdirectory name — `commit-<7-char-sha>` for a shelved or bookmarked commit, the entry's id for a shelf file, `bookmark-<label-or-id>` for a bookmarked file — edit it if you want, then `enter` writes (`esc` cancels). An existing target directory prompts overwrite/cancel. Equivalent to `gg shelf export` for a shelf entry |
| mouse | click focuses the window under the cursor and selects the clicked row; **double-click acts as enter** on that row (drill into a commit, jump to a branch tip, run the selected action-menu or list-popup row); **right-click selects the row and opens its `.` action menu** (panels, files view, and the diff/history/blame views — a shortcut for `.`, never the only path); **middle-click acts as esc** (close the hovered reader, list popup, menu, or files view — text-entry popups are deliberately untouched, so an accidental wheel-press can't discard typed input); **clicking a tab name in a left-column header switches to that tab** (the same slots `ctrl+←/→` cycles); the wheel scrolls the hovered list (`[ui] wheel_step` rows per tick); text-entry popups and decision modals deliberately ignore clicks |
| `j`/`k` or `↑`/`↓` | move selection |
| `pgup`/`pgdn` | move selection by 25% of the panel viewport |
| `o` | cycle the focused panel's sort order (name/date, asc/desc) |
| `alt+1`…`alt+5` | on the focused **Branches** or **Remotes** panel: toggle one of five configurable **branch filters** (`.gg.toml` `[[branches.filter]]` — hide, or show only, branches by tip age and name; edit them from Settings `,` → *Branch filters…*). Pressing the already-active slot's key clears it (one active slot per list, independent between Branches and Remotes); the header gains a `▾N name · M hidden` decoration and stacks with an active `/` filter. HEAD, a branch checked out in a worktree, and the current branch's upstream are never hidden — a dim `∗` marks a row the rule would otherwise hide. The active slot is remembered per repo and shared with `gg web`, where the same rule shows as a `▾` chip on each list header (click for a menu, or `alt+1…5` / `alt+shift+1…5` for Branches/Remotes) |
| `/` | filter the focused panel (type, then `enter` to keep, `esc` to clear). The search starts **from the cursor**, not from the top: each keystroke lands on the nearest match at/after the current row (wrapping to the top when every match is above, like `@`), and leaving the filter (`esc`, `ctrl+r`, or switching to `@`) keeps the cursor on the same row in the full list |
| `\` | on the Commits panel: open the **commit feed filter** popup — type a path, author, message substring, and/or date range (`since` / `until`, passed verbatim to `git log`) to narrow the commit list; filters compose with any active branch scope; the commit-graph hides while a filter is active; clear via the `.` menu **Clear filter** row or by opening the popup and erasing all fields. "Commits touching this" in the working-tree files window (`F`) and in the files view `.` menu seeds the path field automatically |
| `ctrl+l` | on the Commits panel: load the next batch of history on demand (without waiting to scroll to the bottom) |
| `Home`/`End` | jump to the top / bottom of any navigable list; **End** on the Commits panel also loads the next history batch — press again to walk deeper |
| `ctrl+f` | on the Commits panel: **eager search** — pages unloaded history for the next match of the active `/` filter or `@` highlight query and jumps to it. Every press digs past the already-loaded commits (a hit already on screen doesn't stop it), and gg asks before loading many more pages; the `/` filter stays engaged just like `@` (the query stays visible in the bar), and the last query is remembered so `ctrl+f` keeps digging even after you esc-clear the search |
| `R` | switch repository — a fullscreen table of known repos (name, slow-fs marker, path, last opened, each in its own aligned column; over-long paths middle-elide so both ends stay readable): type to filter by name or path (no `/` needed; `esc` clears the filter first, closes on the next press), `enter` switches, `ctrl+d` forgets, `ctrl+p` copies the selected row's absolute path, and `ctrl+g` groups the list by project — a checkout with its linked worktrees (they share one git common dir), plus any clone of the same remote repository; each project's most recently opened checkout leads under the project name (the remote's, else the main checkout's directory), with its other checkouts and worktrees beneath it; a project with a single row keeps its directory name (press again for the flat list; the choice is remembered across sessions). The browser UI (`gg web`, ☰ → *repositories* → *switch repo*, or the palette's **switch repo…**) shows the same table with a **branch** column after the name (name · branch · slow-fs · path · last opened); the box grows to fit the longest path, and when even the viewport is too narrow the path is cut from the **middle** by the same rule (leaf, its parent and the root survive; hover shows the full path), never from the right. The branch and slow-fs verdicts arrive a moment after the list — a checkout on a hung network mount never delays the others — and typing filters on branch, name and path; `ctrl+g` (or the clickable hint under the table) groups it by project exactly as the TUI does, remembered in the web's own layout state |
| `ctrl+o` | **shell escape** — the emergency hatch: suspends gg into an interactive `$SHELL` (`%COMSPEC%` on Windows) in the current worktree, from **any** surface, including mid conflict-resolve or any other window — `exit` returns to gg with a full reload. Never swallowed by whatever window is open, but it waits for a running gg operation to finish first |
| `ctrl+p` | **command palette** — a launcher for commands that don't have (or don't need) their own dedicated key; just start typing to filter the list (`↑`/`↓` move, `enter` runs, `esc` clears the filter, then closes): **Show commit** (`#`), **File history** / **File blame** (type a path — relative, absolute, or `./`-prefixed, normalized to repo-relative — then opens the same `h`/`b` view), **Find** (`F`, the working-tree files window), **Open repo** (type a path to a repo not already open; `~` expands to home; an invalid path shows an inline error instead of switching), **Apply patch…** (an editable path popup — applies a patch file to the working tree as unstaged changes, conflicts landing as markers for `x`; a `git format-patch` mailbox offers to recreate its commits instead — see `gg apply` below), **Browse remote branches** (list branches on the remote that a narrowed fetch refspec left unfetched — one `ls-remote`, `/` filters — and check one out, staying put or switching, adding a per-branch fetch mapping first), **Git config explorer**, **Set up agent skills**, **Open shell** (same as `ctrl+o`), and **Run shell command…** (type one command, run it in the worktree with a press-enter-to-return pause so the output stays on screen; `alt+↓`/`alt+↑` recalls previous commands); `↑`/`↓` select, `enter` runs, `esc` closes |
| `,` | settings: **Identity & profiles** — view/edit the git `user.name`/`user.email` (global vs repo-local, kept distinct) and manage named identity **profiles** (global or per-repo presets; `enter`/`e` prompts *apply to this repo or globally*) — **Branch prefixes**, **Branch filters…** (browse/edit/remove the five `alt+1…5` slots, global or per-repo), the **Operation log** toggle, and **Session errors**: a viewer of this session's failed git operations (also written to an always-on `errors.log` in the gg state dir). The menu is type-to-filter like `ctrl+p`: any character narrows the list, `↑`/`↓` + `enter` opens/toggles, `esc` clears the filter first and closes on the next press. (Set up agent skills and the Git config explorer moved to the command palette — see `ctrl+p`.) |
| `.` | open the **action menu** (works in every navigable window — panels, the file tree, diff, history, blame, stash): lists context actions for what's in view (row actions first, then panel/window actions; whole-app actions stay in the footer); type to filter the list (any character narrows it — no `/` needed), `↑`/`↓` + `enter` runs the highlighted action (keys never run rows directly), `ctrl+w` cycles display mode, `esc` clears the filter first and closes on the next press. Includes **Copy commit id** / **Copy commit title** (Commits), **Copy file path** / **Copy file name** (and **Copy stash ref** on the stash list) for whatever the active window shows — copied to the system clipboard (native OS clipboard command, with an OSC 52 fallback for remote/SSH sessions) — plus context write actions: **Copy branch name** / **Copy commit id** / **Copy commit sha** / **Rename branch** on the Branches panel (Copy branch name / commit id / commit sha on Remotes too), and **Edit commit message** (reword via a pre-filled message popup) on the Commits panel; the Remotes panel also offers **Browse remote branches…** (same picker as the `ctrl+p` palette entry; the browser UI (`gg web`) has the same picker under its ☰ menu). On the Commits panel it also offers **Fast-forward `<branch>` to here** when the selected commit is ahead of the current branch's tip (advance the branch with no merge commit), and **Export commit as patch** (hidden for merge commits) — writes a `git am`-able `git format-patch -1 --binary` patch for the whole commit. Inside the diff view, when viewing a commit-vs-parent file diff, the `.` menu offers **Export this file's diff as patch** — the same patch, scoped to that one file. Both open an editable full-path popup pre-filled with `<parent-of-repo>/<shortsha>.patch` (or `<shortsha>-<basename>.patch` for a file); `enter` writes, `esc` cancels — see also `gg commit export-patch` below. To import a patch back, use the command palette's **"Apply patch…"** entry (`ctrl+p`) — see also `gg apply` below. |
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
gg compare [--patch] <left> [<right>]   # changed-file list (or --patch: unified diffs of exactly those files) between two endpoints or gg:// links
                                      # endpoints: a gg:// link, a commit-ish, @staged, @worktree, bookmark:<id>,
                                      # shelf:<id> (a stored commit entry — hybrid: live sha while it exists, frozen
                                      # tar once gc'd, noted on stderr); <right> defaults to @worktree
                                      # order is free (@worktree first just inverts the statuses); rows sorted by path
gg compare --save <label> <left> [<right>]  # run it AND keep it; both sides stored as gg:// links whatever you typed
                                      # stdout stays the changed-file list (pipeable); the id goes to stderr
gg compare --saved <id|label>           # re-run a stored comparison; prints exactly what the original printed
gg compare --list                       # <id>\t<label>\t<left>\t<right> per row; empty right = a saved merge preview
gg compare --rename <id|label> <label>  # relabel a stored comparison (the id does not change)
gg compare --remove <id|label>          # delete one (a saved merge preview is a row of the same store)
                                      # saved comparisons and saved previews share one store
                                      # a link with a /<path>, or an @<a>..<b> target, scopes the answer to those files
gg preview list                       # id  label  source  target  state  files  ahead, one row per saved pair
gg preview add [--label <text>] <source> <target>   # save a pair; state/counts recompute from the live tips on every read
gg preview add [--label <text>] <a>..<b>            # save a COMMIT PAIR: the two-dot diff between two commits, frozen to full shas (listed with state "pair")
gg preview add --symmetric --base <base> <a> <b>   # save a SYMMETRIC MERGE PREVIEW: one live comparison of @<base>...<a> vs @<base>...<b> (no default base; see gg compare --list / --saved)
gg preview rm <id|label>              # remove a saved pair
gg preview rename <id|label> <text>   # change a saved pair's label
gg preview show [--patch] <id|label>  # changed-file list (or --patch) for a saved pair's current merge-base..source diff
gg preview diff [--patch] <source> <target>   # the same diff, one-off (no saved pair)
gg preview diff [--patch] <pair id|label>     # a saved COMMIT PAIR's diff
gg note add --preview <a>..<b> --file F --new-line N --summary "…"   # review notes on a commit pair: stored on b, new side (also via the gg://repo/F@a..b:N link)
                                      # show/diff: a non-ok state (merged/missing/no common base) prints to stderr, exit 1
gg review [--tool <name>] [--working] [<rev>|<A..B>]
                                      # AI code review; flags MUST precede the positional (like gg log -n). No positional
                                      # reviews the current branch's work; a single <rev> reviews just that commit's own
                                      # change; --working reviews uncommitted changes. Prints the report to stdout and
                                      # persists it under the gg state dir; --tool picks among configured review commands
gg review --notes [--tool <name>] [--working] [<rev>|<A..B>]
                                      # also ask the tool for anchored notes (agent-context v1) and import them
gg diff --hunks [--json] [--cached] [<commit>] [-- <paths>...]
                                      # numbered git @@ hunks per file, over the same patch a note anchors to
gg note add   --file <path> (--hunk N | --new-line N | --old-line N) [--cached | --rev <c>]
              --summary "…" [--rationale "…"] [--author <name>] [--source user|agent] [--json]
gg note reply [<repo-link>] <note-id> --summary "…" [--json]
gg note apply [<repo-link>] --stdin [--cached | --rev <c>] [--author <name>] [--json]
                                      # agent-context v1 or a comments batch; validated whole before the first write
gg note list  [<link> | --file <path>] [--type user|agent|all] [--cached | --rev <c>] [--json]
gg note rm    [<repo-link>] <note-id>
gg note clear [<link>] (--file <path> | --all) [--type user|agent|all] --yes
gg add [-f] (-A | <path>...)  # stage paths (-f forces gitignored ones), or everything with -A
gg unstage <path>...          # remove paths from the index, keeping working-tree content
gg commit -m "msg"            # add -a to stage tracked changes; --amend rewrites the last commit
gg commit reword <commit> -m "msg"   # change a commit's message (HEAD=amend; older=in-place rebase)
gg commit export-patch <sha> [--out <path>] [--force] [-- <file>]
                                      # write a git am-able patch (git format-patch -1 --binary); with -- <file>, scope to that file; refuses merge commits; default path = parent-of-repo/<name>.patch
gg apply [--am | --working] <path>   # import a patch file (the inverse of export-patch); default = working tree (lands unstaged, conflicts left as markers for [x], exit 1); --am recreates commits from a format-patch mailbox (atomic: rolls back whole on any conflict, exit 1); flags precede the positional
gg pull [--background] [--on-conflict rebase|merge|reset|abort] [--on-stale-mapping remove|abort]
                                      # reset = hard-reset to the remote tip, discarding local work; --on-stale-mapping=remove drops a per-branch fetch refspec whose branch was deleted on the remote (it blocks every fetch) and retries
gg push [--force | --force-with-lease] [--on-reject rebase|force|force-with-lease|abort] [--map | --no-map] [<branch>]
                                         # push the current branch, or a named one by ref (no checkout); --on-reject recovers a rejected push (default: fail/prompt)
                                         # --map/--no-map answer the post-push fetch-refspec-mapping prompt; neither + non-interactive = skip
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
gg versions show <branch> <id|latest>  # print the frozen change set a two-branch version recorded (Base...Ours)
gg versions restore [--discard] <branch> <id|latest>  # restore a branch to a recorded version; --discard answers the dirty-tree prompt
gg link --version <branch> <id|latest> # print one version's preview link (gg://…?version=<id>)
gg unlock [--yes]                      # list (or with --yes remove) stranded .git/*.lock files; exit 1 while locks are present
gg migrate [--yes]                     # list pending store migrations and what they'd discard; changes nothing without --yes
gg merge [--into <target>] [--on-conflict=keep|abort] [--no-ff] [-m <msg> | -F <file>] <source>  # -m/-F (- = stdin) set the merge commit message and imply --no-ff
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
gg prefix resolve <id> [--set label=value]... [--parent <branch>] [--bump]
                                          # print a prefix resolved as the TUI's picker does (--template <value> for a raw one)
gg template list                          # list text templates: id, scope, title
gg template show <id>                     # a template's text and its variables
gg template render <id> --set name=value  # print the resolved text (consumes <seq>; --peek does not)
gg template add --title <t> -F <file|->   # store a text template (for every repo; --repo = this repo only)
gg template edit <id> [--title <t>] [-F <file|->]
gg template rm <id>                       # remove a text template
gg undo
gg worktree list
gg worktree list --json | --free [--json]
                                      # for orchestrating agents: every worktree's facts (dirty + last change,
                                      # claim, sessions, tui, reserved, paused op, git lock), a free verdict and
                                      # blocked_by reasons; --free = only free ones, clean first, then stale-dirty
gg worktree claim [--note <text>] <path>   # an agent running inside gg ($GG_SESSION_ID) takes a free worktree;
                                      # atomic, ends with the session; outside gg it exits 2
gg worktree release [--force] <path>  # give a claim back (exit 0 with no claim)
gg worktree reserve|unreserve <path>  # keep a worktree away from agents ([agents] reserved)
gg worktree add [<start-point>]
gg worktree add --branch <name> [<path>]  # existing branch; <path> (cwd-relative) overrides the path template
gg worktree add --from <commit> [--keep staged|unstaged] [<branch-name>]
                                      # new branch at <commit> (default name <current-branch>_<short-sha>,
                                      # a trailing positional overrides it); --keep staged|unstaged instead
                                      # lands the branch on the commit's PARENT with the commit's own diff
                                      # left staged/unstaged in the new worktree; refuses a root/merge commit
gg worktree remove [--with-branch] [--force] <path>
gg worktree rename [--force] <worktree> <new-name>
                                      # <worktree> resolves by path (like remove) or by branch name;
                                      # --force unlocks a locked worktree and renames it
gg worktree move [--force] <worktree> <new-path>
                                      # relative <new-path> resolves against the invocation directory;
                                      # moving the worktree your shell sits in updates the cd-on-switch handoff
gg worktree prune                     # drop stale worktree administrative entries
gg repo list
gg repo switch [--exact] <query>     # --exact: whole name or path, not a substring
gg init [--all | --update | --agents <ids> | --list | --to <path>]
                                      # installs BOTH embedded skills (using-gg, reviewing-with-gg) per agent
                                      # --to: install both at a custom path for an unsupported agent
                                      # (file → managed block, one per skill; directory →
                                      # <dir>/using-gg/SKILL.md + <dir>/reviewing-with-gg/SKILL.md);
                                      # remembered, so --update refreshes it
gg skill path [review|using-gg]      # materialise an embedded skill under the user cache dir and print its path
gg inspect [--debug-dump <path>] [--trace]
gg version                    # (also --version / -v) print build version + commit
```

**Review notes.** `gg note add --file <path> --new-line 42 --summary "…"` pins a
remark to a line; `gg note list`, `gg note reply`, `gg note rm` and
`gg note clear` manage them, and `gg note apply --stdin` imports a whole batch
of agent annotations at once. Every note verb takes a `gg://` link as its
first argument: a file link for `add`/`list`/`clear`, the repository's link
(`gg://<repo>`) for `reply`/`rm`/`apply`, since note ids are per repository
and the link picks the checkout whose store holds them. `gg diff --hunks` numbers each file's `@@` hunks
so `--hunk N` can address one. Notes are machine-local and expire (see
`[notes]` under Configuration); they render inline in the TUI diff view and in
`gg web`. In the TUI, **View all notes…** (command palette) lists every note
this checkout can see as one tree — working tree (unstaged / staged /
untracked), then commits newest first, then shelf entries, each down to its
directory and file — with a status column (active, stale, orphaned, or missing
when the commit is gone); type to filter, `enter` on a note opens its diff on
that line and `esc` comes back to the list. A shelf entry's own notes (a
recycled worktree's "deleted …" / "renamed …") sit right under the entry,
before its files; `enter` on one reads it. `gg web` has the same list under
**view all notes…** in its command palette (`ctrl+k`) — same tree, columns,
filter, `←`/`→` folds, a click or `enter` on a note opens its diff on it, on a
review the review view (`esc` returns), `ctrl+d` deletes the thread or review under the cursor after asking. `gg review --notes` does not replace the text report — it still
prints and saves that — but ALSO asks the tool for anchored notes and imports
them. `--preview <id|label|<target>...<source>>` targets a saved merge
preview instead of a single commit (on `gg diff --hunks`, `gg note
add|list|apply` and `gg review`): a preview note is an ordinary committed
note stored on the **source tip**, addressable only on the new side — the
merge base is not addressable, so `--old-line` is refused.

An agent can also steer the window you already have open: `gg session status`
says whether one is running, and `gg session navigate --file <path> --hunk N`
puts your cursor on the hunk it is about to annotate. Every `gg note` mutation
posts a refresh on its own, so notes an agent writes appear without a manual
`r`. Turn the whole channel off with `[ui] agent_steering = "off"`.

Forks are answered by flags (e.g. `--on-conflict`, `--with-branch`/`--force`);
without a flag, an interactive terminal prompts, and a non-interactive run errors
asking for the flag.

Every command (and the TUI) accepts a global `--time-track <file>` flag that
appends one JSON span per process start, git subprocess, and operation —
`jq . gg-perf.log` shows where the time went.

#### gg links

A `gg://` link is a portable address for one place in one repository:

```text
gg://<repo>/<path>[@<target>][:<line>]     # <target>: a sha, "staged", or absent = the working tree
gg://<repo>/<path>[@<target>]#<hunk>       # hunk numbers are `gg diff --hunks`'s
gg://<repo>/<path>@<sha>:old:<n>           # the old side of that diff
gg://<repo>@<sha>                          # a commit
gg://<repo>@ref:<branch|tag>               # a branch/tag TIP: the whole tree there (a NAME, not today's sha)
gg://<repo>@<a>..<b>                       # a CHANGE-SET: only the files that differ between a and b
gg://<repo>@<target>...<source>            # a merge preview (branch names, never shas)
gg://<repo>/<path>@<target>...<source>:<n> # a file / new-side line in it
gg:///abs/checkout/file.go:12              # a repo with no remote
gg://<repo>@<sha>?bookmark=<id>            # a trailing ?<kind>=<id> hint: where the link was copied from
gg://<repo>@<a>..<b>?preview=<id>          # …a saved Previews entry: lands the same, then reveals its row
```

A `?` is the hint separator now, so it cannot appear in a path, in a checkout
path or in a ref name carried by a link. `gg link`, the TUI and `gg web`'s
copy-link buttons all refuse to print such a link rather than emit one that
reparses as something else.

**Copy file link** (the TUI `.` menu and the web file-row menu, next to Copy
link) copies `gg://<repo>/<path>?view=content`: the file as it is on disk in
this worktree, with no commit — from a commit's file list too. gg checks the
file exists first and says `<path> is not in the working tree` when it does
not. `gg open` on such a link (or an agent's `gg session navigate`) opens the
file's content viewer in the TUI — and, with `gg web` running (`gg open
--web`, or a link pasted into its `#` prompt), in the page's viewer.
Inside that viewer (and in the files view's View-file preview, when the disk
still shows the previewed text), Copy file link carries the cursor line
(`…/<path>:<line>?view=content`), and opening such a link lands the cursor on
that line. `gg link --content <path>:<line>` prints one (exit 1 when the file
has fewer lines).

With lines marked (`space`, move, `space`) in a diff, the file viewer or a
preview, `L` / Copy link copies a link to that **range** — `…:42-57`, or
`…:old:42-57` on the old side. Opening it marks the same lines on the same
side again. Paste it to an agent with a question; the agent reads the lines
with `gg link text <link>`. A range link to uncommitted lines is strict: once
any of those lines changed (or the block moved) the link is refused as no
longer valid rather than pointed at whatever sits there now. A note written on
a range link (`gg note add <link>`) sits under the last line and marks the
lines it is about with a bar in the gutter.

```bash
gg link internal/tui/steer.go:42        # print the link for a place here
gg link --rev HEAD README.md            # …at a commit (always the full sha)
gg link --ref main                      # …the branch tip, kept as a name
gg link --pair HEAD~3..HEAD             # …the last 3 commits' change-set (both halves = full shas)
gg link --ref main --bookmark b1        # …with a landing hint appended
gg link --content README.md             # …the file's CONTENT on disk (?view=content), no commit
gg link --version main latest           # …a branch version's frozen preview (?version=<id>)
gg link internal/tui/steer.go:42-57     # …a RANGE of lines (uncommitted: + a fingerprint of the block)
gg link resolve gg://gigagit/a.go:3     # which checkout on this machine?
gg link text gg://gigagit/a.go:42-57    # print exactly the lines a line or range link names
gg links                                # the links copied here, newest first

gg compare gg://gigagit@ref:main gg://gigagit@ref:v1.2         # links on either side of a compare
gg compare gg://gigagit/internal/tui/model.go@ref:main main    # one file, one row

gg diff  gg://gigagit/a.go@abc1234      # every verb takes a link as its first positional
gg show  gg://gigagit@abc1234
gg note add gg://gigagit/a.go:42 --summary "…"
gg session navigate gg://gigagit/a.go:42

gg link --preview login a.go:12         # a link to a place in a merge preview
gg open  gg://gigagit/a.go@main...feat/login:12   # show it in the user's gg
gg open  gg://gigagit                             # just open gg in that checkout
gg open --web gg://gigagit/a.go@main...feat/login:12   # …in the browser: steer a live page, ask a live TUI to serve one, or start gg web (foreground)
```

A **change-set** link (`@<a>..<b>`) names a bounded set of files, so the verbs
that need one commit refuse it rather than guess: `gg diff`, `gg open` and
`gg session navigate` take it, while `gg show`, the `gg note` verbs and
`gg session highlight` take a `@ref:` tip but exit 2 on a change-set —
anchoring on its newer end would quietly widen the set into a whole tree.

Every copy records into a per-repo history that `gg links` prints and the
`gg_link_list` MCP tool serves; copying the same link again moves it to the
top. The browser records through it too, so a link copied in `gg web` shows
up in `gg links`.

**Compare with link…** (the `ctrl+p` palette) compares any two links inside the
TUI: type or paste them, or press `↓` to pick from the links you copied;
`ctrl+s` swaps the sides. Each field says what its link is underneath
(`pair: …`, `branch: …`, and `· copied from a saved preview` for a hinted
link). A link naming a whole branch, tag or commit gets a
**base row** under it — the branch's upstream or the trunk, a commit's parent.
`enter` on that row rewrites the link into the bounded one (`@<base>...<branch>`
or `@<parent>..<sha>`), so the comparison lists only what it changed; leave the
row alone to compare the whole tree. `.` → **Save comparison…** keeps it in the
**Previews** tab (the same entries `gg compare --save` / `--list` use): `enter`
runs it again, `e`/`d`/`s` rename, remove and save it reversed, and the `.`
menu copies either link. In the bookmark and shelf switchers, comparing across
the two (`c`) now also accepts a commit against a single file.

Two **Previews** rows compare against each other without copying anything:
`space` marks a merge preview or a commit pair (◉), and the second `space`
opens the comparison of the two rows' own links — the upper row on the left.
`m` marks without opening (`.` → **Compare the 2 marked previews**), `esc`
clears the marks. A saved comparison row is already two links, so it cannot be
marked.

`gg web` has the same dialog — **compare with link…** in the command palette
or the ☰ menu — with the same history picks (`↓` or ▾), swap (`ctrl+s` or ⇅)
and base row (`enter` on it, or **bound**). **save comparison…** sits on the
open comparison's bar, and the sidebar's **Previews** section lists merge
previews, commit pairs and comparisons together: click to open, right-click to
rename, save reversed, copy a link or remove. A commit pair opened there — or
landed by a `@a..b` link — carries its **review notes** as in the TUI: `◆N` on
its files, the notes inside each diff, `c` on a new-side line.

A comparison of two **change-sets** (two previews, a pair against a preview,
two agents' attempts) also offers **⇄ symmetric** (`v`): each set's file list on
its own side, row for row, with one diff between them — a `· · ·` gap where a
set does not touch the file, a struck-through row where it *deletes* it, a
dimmed `=` row where both changed it identically. The arrow over the diff says
which set is old and which is new, and `x` (or a click) flips it while the
lists stay put; `1`–`4` filter *differ · all · one side · same*. Web only,
remembered, and off below a 1200px window.

The quick way to start one: **drag** a merge preview or a commit pair onto
another row of the section. The drop opens a menu with both directions and
**compare and save…**; **compare with…** in a row's right-click menu opens the
compare dialog with that row's link already in the left field.

`<repo>` is the repository name of the repo's remote, resolved through the
repository history behind the `R` switcher — so a link made on one machine
finds the matching checkout here. In the TUI, `#` (or the palette's
**Open gg:// link…**) takes a pasted link and lands on it — a link into
another checkout asks first, then switches the repo and lands there; `L` in
the diff view copies the cursor line's link and the `.` menu's **Copy link**
works in the Files, Staged and Commits panels and a commit's files view; in
`gg web`, **shift+click a line number** marks a range of lines (diff, stacked
diff, file viewer — the right-click menu then copies the range's link, a
range link that opens marks its lines again, and in a diff `c` writes one
note over the marked lines), and right-click
a diff line, a review note, a file row or a commit row — a note has no address
of its own (its id is machine-local), so **copy gg link to this note** hands
back the note's ANCHOR line, the same link the line under it copies, and a
reply copies its thread's anchor — inside an open merge preview the
diff line and the file row copy the preview forms
(`…@<target>...<source>[:<line>]`, new side only; a deletion row copies the
file), and a Previews row copies the pair's own link. Quote a link that carries `#<hunk>` — an unquoted `#` starts a shell
comment.

The browser UI copies the same things the TUI's `.` menu does, from where they
are on screen. In an open commit, the file list's top bar stays on screen while
the list scrolls, and its **»** control folds the list to a slim strip so the
diff gets the width on a small screen (**«** brings it back; the ☰ menu's
*toggle file list* is the same switch, remembered per machine). The **file
list cuts long paths from the
middle** the way the TUI does — whole directories drop out around a `…`, so
the file name and the head of the path both survive a narrow pane (the row's
tooltip carries the path in full). The **diff header's file path** copies on a
click, and right-clicks into *copy full path* / *copy file name* / *copy parent
dir* — plus, below a separator, *copy absolute file path*, *copy absolute
parent dir* and *copy repo absolute path* (the checkout root alone, `\`-separated
on Windows). Opening a file diff lands on its **first changed line** rather
than the top of the file. Right-clicking **inside a diff** offers *copy* for
the selected text, or *copy line* when nothing is selected (alongside *copy gg
link to this line*). And the file list's header copies
everything it shows: right-click anywhere in it for *copy short commit id* /
*copy commit id* / *copy commit title* / *copy date* / *copy author* — or, if
text in the header is selected, a plain *copy* of the selection.

### Pull requests (read-only)

With the GitHub CLI installed and logged in (`gh auth login`), gg reads the
repository's pull requests — and only reads: nothing is ever posted, edited or
submitted.

```bash
gg pr list [--json]           # open PRs, newest-updated first; known closed/merged ones stay, marked
gg pr list --state merged --search "login author:kim"   # SEARCH the forge: any state (all|open|closed|merged,
                              # default all), GitHub search syntax, 50 newest-updated (--limit 1…200);
                              # --search 123 (or '#123') looks that PR up directly
gg pr view 123 [--json]       # description, conversation + review verdicts, outdated inline threads
gg pr comments 123 [--json]   # inline / file-level threads that still have a position
gg pr fetch 123               # PR head (forks too) → private ref refs/gg/pr/123; prints the ref
gg diff main...refs/gg/pr/123 # read the change
gg pr forget 123              # drop the ref (and a closed PR's row)
```

The plain list holds the open pull requests (plus the ones gg already knows).
A closed or merged PR you never fetched is found by **searching**: in the TUI
`A` on the Pull requests tab opens a search popup — type text (it goes to the
forge's own search, so `author:kim` or `head:fix/login` work), `tab` cycles
*all / closed / merged / open*, `enter` searches; a bare number opens that PR.
The result rows act like rows of the tab (`enter` diff, `i` details, `y` copy
URL), closing a diff or the details returns to the results, and the last search
is kept for the session. Results are a transient set: a found PR joins the tab
once you open it (its fetched ref makes it known), until you forget it (`d`).

In the TUI the same data is a **Pull requests** tab — the fifth tab of the
top-left box (`PR` in its header), present only when a usable `gh` was found at
startup. Rows lead with their status (`✓` approved · `✗` changes requested ·
`…` review required · `draft`; `merged` / `closed` / `unavailable` for a PR that
is no longer open — those stay listed, dimmed). `enter` fetches the head and
opens the PR's diff (`base…head`, titled `PR #123 · title`) in the same view a
merge preview uses; `i` opens the PR hub (description, conversation with review
verdicts, outdated threads; `y` copies the URL, `r` reloads); `y` copies the PR
URL; `d` forgets a PR that is no longer open. `r` re-reads the list, and gg
re-reads it in the background every `[refresh] prs` seconds (default `300`,
`0` = off) — independently of the `[refresh] enabled` master switch.

Inside a PR's diff its **review threads** show as read-only note boxes on
their lines (`review · author · age · path R12`, left pane for a comment on the
old side, a whole-file comment at the top of the file, `◆N` badges in the file
list); `}` / `{`, search and *List notes…* include them, and `E` / `R` / delete
refuse — gg never writes to the forge. `o` collapses the note at the cursor to
one row and `O` all of them (this works for your own notes too); a resolved
thread starts collapsed. Comments are re-read every `[refresh] prs` seconds
while the diff is open and on `r`; `i` opens the PR hub from the diff.

In **`gg web`** the list is a *pull requests* section of the sidebar (under
*previews*), shown only with a usable `gh`: click a row to fetch and open its
diff, right-click for *copy URL* and *forget*, **⟳** on the header to re-read.
The section has a **search** field (`A` focuses it): it asks the forge for pull
requests of any state — text in the forge's own syntax, a chip cycling *all /
closed / merged / open*, a bare number for a direct lookup, `Enter` to search.
The 50 newest-updated matches show under the field and act like list rows;
opening one makes it a row of the list, and the last search is shown again after
a reload.
It follows `[refresh] prs` too, in every open tab. A pull request you opened
before shows at once from the local head and is checked against the forge in
the background (new commits are fetched and the diff re-opened); a first open
masks the panes with a loading notice. Review threads show inside the diff as
read-only boxes (resolved ones folded; a whole-file comment leads the file) and
are re-read on every `[refresh] prs` tick; every note box folds from its title
(click, `z`, `Z` for all). **details…** in the row menu — or the `details` chip
on the open diff — shows the description, the conversation with the review
verdicts, and the outdated threads with their hunks.

Pull-request text is **rendered as markdown** in `gg web` and in the TUI (the
PR hub popup and the review-thread boxes) — the description,
comments, verdicts and review threads: headings, emphasis, lists and task
lists, quotes, tables, syntax-coloured code blocks, captioned *suggestion*
blocks (shown, never applied). Links open in a new tab and only when they are
`http(s)`; images are never fetched (they become `[image: alt]` links) and raw
HTML is shown as text. Your own review notes stay exactly as typed, and the
CLI (`gg pr view`, `gg pr comments`) keeps the raw markdown for agents. In the
TUI a link reads `text (url)`, code blocks and tables are clipped rather than
reflowed, and the styles are terminal attributes, so every theme shows them.

`refs/gg/pr/<n>` is local only: never pushed, never shown in the commit graph.
The fetch goes through whichever of your remotes names the base repository, so
it uses your own ssh/https setup. `gh` is detected once per run; without a
usable `gh` the verbs exit 1 and say why. Per PR, gg reads up to 100 review
threads (50 comments each), 100 conversation comments and 100 reviews, and says
so when a PR has more. `GG_GH_BIN` points gg at a different `gh` binary.

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
- **Links and saved comparisons** — `gg_link_resolve`, `gg_link_list` (the
  copied-link ring), `gg_compare_links` (the changed files between two
  `gg://` links) and `gg_compare_list` (every saved merge preview, commit
  pair and two-link comparison, with the ids the note tools take).
- **Export** — `gg_export` copies a bookmark or shelf entry into a local
  directory.
- **Review notes** — `gg_notes_list` (read-only) plus the consent-gated
  `gg_note_add` (leave one anchored note), `gg_notes_apply` (import a batch)
  and `gg_note_rm` (remove one). The same store `gg note` writes on the CLI.
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
`[ui] reading_width` caps the text column of free-text popups — the review
overview, AI results, PR descriptions and comments, commit messages, View all
notes — in columns (default 120, min 40): text wraps inside it and the column
is centred, so a fullscreen (`ctrl+t`) popup never runs lines across the whole
screen; code, diffs and file trees keep the full width;
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
`[ui] theme` selects the TUI's colour scheme: `terminal` (the default;
inherit the terminal's own scheme, unchanged from before this setting
existed) or `dark` (the Windows Terminal "Campbell" look, pinned everywhere)
or `light` (a neutral light grey: charcoal text on an off-white grey ground).
Cycle it live from the `,` Settings menu
("Theme"), which applies immediately and persists the choice to the
**global** config (a theme is per-human, like `[ui] language`, not per-repo).
Under a 256-colour terminal profile the theme's hex colours snap to the
nearest colour-cube entry; with only 16 colours available they snap onto the
terminal's own remapped basic slots — exactly what the theme exists to
override — so it's effectively off.

**Recolouring a theme** — a `[themes.<name>]` table overrides individual
colour roles of any built-in theme, so you can keep `light` but darken its
text, or paint the `terminal` theme's background without adopting a whole
scheme:

```toml
[themes.light]
bg = "#E9E9E5"   # frame background
fg = "#2A2F34"   # frame foreground (default text)
```

Values are `"#rrggbb"` or a `"0"`–`"255"` colour-cube index. Every role is
optional — an omitted key keeps the theme's own colour — and the table names
a theme (`light`, `dark`, `terminal`), so it only applies while that theme is
active. Run **`gg config populate`** to write the full commented list of
roles (every key with the theme's current value and what it paints) into your
config; the two list roles, `lanes` (7 graph-lane colours) and `syntax` (11
syntax classes), take arrays, where an `""` entry keeps the built-in colour.
The global and repo files layer per key, like every other setting, so a repo
can retune one role. An invalid value is ignored — the rest of the table
still applies and the status bar names what it dropped, e.g. `theme light:
ignored invalid bg=#12`.

You don't have to hand-edit the file, though: the `,` Settings menu's
**Theme colours…** row (right under Theme) opens an editor for every role of
the theme that is currently active. Each row shows a sample painted in the
colour itself, the effective value, the built-in default, and what the role
paints; `/` filters over the key and the description, `↑`/`↓` move, `enter`
edits the value in place, `d` restores the built-in default, `D` resets the
whole theme after a yes/no confirm (it removes the global `[themes.<name>]`
table; the roles your repo's `.gg.toml` pins keep their value and are named in
the status), `t` cycles the theme, `ctrl+t` makes the popup fullscreen, and
`esc` clears a committed filter before it closes the window. The wheel scrolls it, a double-click is `enter` and
a middle-click is `esc`, as in the other list popups. **While you type, the whole
screen already renders in the new colour**, so you judge it in place — `esc`
puts it back, `enter` saves. Saving writes exactly one line into the
**global** config's `[themes.<name>]` (for a lane or a syntax class, the whole
array line) and re-applies the theme, leaving every other line and comment of
your config alone — including the commented blocks `gg config populate`
generated, whose lines it uncomments in place. A role your repo's `.gg.toml`
pins is shown tagged `(repo)` and cannot be edited here — the repo file would
shadow whatever the editor wrote — though `d` still removes a global override
left underneath one, and says the repo value goes on applying. In this and
every other gg text field, the Delete key erases the character behind the
cursor once nothing is left ahead of it, so a keyboard that sends Delete for
its erase key never gets stuck on the last character.

`[ui] agent_steering` (default `"on"`) controls whether the TUI and `gg web`
accept live-steering commands from `gg session` — an AI agent putting your
window on the line it just annotated. Set `"off"` to disable the inbox:
neither frontend writes a session presence, and `gg session` reports there is
no gg session for the worktree.

`[ui] show_eol_only_changes` (default `false`) controls whether a file whose
only unstaged change is its line endings (CRLF↔LF) is shown as modified — by
default such files are hidden from the Files panel and its count badge as noise;
set it `true` to surface them (e.g. when deliberately renormalizing line
endings). The scriptable `gg status` is unaffected (faithful to `git status`).
Like every entry, the repo's `.gg.toml` overrides the global config per field.

`[ui] diff_syntax` (default `"auto"`) colours code in the diff views by file
type — keywords, types, strings, numbers and comments each get a colour, on
top of the add/del backgrounds and the word-level emphasis. The language is
picked from the file name (about 300 lexers, via chroma); a file with no
known lexer, or a side larger than 1 MB, renders plain. The same switch and
lexers colour the **hunk picker** (TUI) — the conflict resolver's two
candidate columns and its live output pane, and the hunk staging/unstaging
pickers — where each column is lexed with its own side's version of the file;
the cursor row there stays plain. Set `"off"` to disable everywhere (TUI and
`gg web`).

`[ui] diff_cursor` (default `"row"`) picks the current-line marker: `"row"`
paints a background under the cursor line, `"number"` highlights only its
gutter numbers, `"off"` hides it (the cursor still drives `e`, the review-note
anchor and the line selection). In the diff the marker lands on the **cursor
side's cell only** — `alt+←`/`alt+→` move it across. It governs the **View
file** preview's line cursor as well, where `"number"` falls back to the band
(the preview has no gutter to number). The `.` menu's **Cursor marker** row
switches it for the session.

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
prs = 300            # re-read the pull-request list every 5 min (the default; 0 = off). NOT gated by `enabled`

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

A `commits` refresh **keeps the history you already paged in**. It re-walks only
the first page (as it always has) and reconciles it into the loaded list: new
commits are prepended, a tip that vanished (amend, `reset`) is trimmed, and every
page below — everything a long scroll, `ctrl+l` or a deep `ctrl+f` search pulled
in — stays put, filter and cursor included. The same holds after an operation
completes (a fresh commit simply appears on top). When the new page can't be
aligned with the loaded list — history was rewritten, or more commits arrived
than one page holds — the list falls back to a clean walk from the top. Manual
`r`, a commit-sort or page-size change, and returning from the shell escape
(`ctrl+o`) always start clean. The browser UI (`gg web`) has the same reload on
`r`, on a **↻ refresh** button next to pull/push, and under its ☰ menu, and it
reloads the same way:
finishing an operation keeps the rows you scrolled in, leaves the scroll where
it was, and re-anchors the cursor to the same commit instead of jumping to the
top; a solo-scope change, a commit-sort change and a re-root still start clean.
The browser UI also **refreshes itself on repo changes** — the same
`[refresh]` file-watch and interval settings the TUI uses drive a server-side
watcher whose pushes (`GET /api/events`) make every open tab re-fetch just the
sources that changed; the settings panel carries the per-source file-watch
toggles.

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

### Stranded git locks

Git creates a `.lock` file while it rewrites the file beside it (`index.lock`
for `add`/`commit`/`status`, `HEAD.lock` for a checkout, and so on) and removes
it on the way out — including when it is interrupted, since it cleans up from
its own signal handler. A lock that outlives its process therefore means a git
was killed before it could tidy up: a `kill -9`, a crash, a lost power. Until
it's gone, every git command fails with *"Another git process seems to be
running in this repository"* and git's own advice is to delete the file by hand.

gg surfaces that as a notice (it also appears the moment an operation fails
this way, not just on the next load), listing each lock with **how old it is**,
and offers to remove them. Scriptable: `gg unlock` lists what's there and exits
1 while any lock is present, so it works as a precondition check; `gg unlock
--yes` removes them.

gg won't decide a lock is stale for you — it can't see git processes it didn't
start, and a git running right now legitimately holds one, so deleting it would
corrupt that write. Check nothing else is running, then confirm.

(gg used to *cause* this: cancelling a git subprocess killed it outright, and a
user action cancels the background refresh. gg now stops git gracefully so it
cleans up after itself. Windows has no such signal, so recovery still matters
there.)

### Git config explorer

The command palette (`ctrl+p`) → **"Git config explorer"** lists every config key git knows
with what's set where: **key | local | global | default**, unset scopes shown
as an explicit `(unset)`. Around 64 common keys are curated — they show git's
real default and a one-line description, and can be edited right there:
**`l`** sets the repo-local value, **`g`** the global one, **`u`** unsets
(you pick which set scope); boolean and enum keys offer a picker, the rest a
text field. Everything else is read-only browsing (use `git config` for
exotic keys). `/` filters as you type; `ctrl+t` maximizes the popup to a near-fullscreen box; `esc` closes.

### Agents and worktrees

`[agents]` governs which worktrees an orchestrating agent may take
(`gg worktree list --free`, `gg worktree claim`, and the guard every
`gg worktree recycle` asks):

```toml
[agents]
reserved = ["../gigagit-main-work"]  # never handed to an agent; REPO file only, relative to the main worktree
stale_after = "14d"                  # a dirty worktree untouched this long may be recycled (its changes shelved)
allow_main = false                   # let agents take the main checkout
```

A worktree is free when it is not the main checkout, has a branch checked
out, no paused merge/rebase, no git lock, is not reserved or claimed, has no
running agent session and no gg TUI open in it, and is clean or dirty but
untouched for `stale_after`. Recycling one that is in use (claimed by another
agent, reserved, a running session, the main checkout…) asks first — the TUI
shows the question, the CLI needs `--force`. In the TUI Worktrees tab ⚑
marks a claimed row (the bottom bar says who, since when, and why) and ⊘ a
reserved one; the `.` menu reserves, unreserves and releases claims.

#### Agents starting agents

An agent running in a gg console (started from Start agent) can start a
worker agent in another worktree and hand it a task. Allow it in the
**global** config (a repo `.gg.toml` cannot):

```toml
[agents]
spawn = ["Claude (yolo)"]  # session command names agents may start; empty = off
max_spawned = 4            # live agent-started sessions per TUI (1..16)
```

Then, once and in this order: accept that command's update in Settings →
External tools if it offers one (the command needs `<prompt>`; the update
changes its text), start it yourself from Start agent (the one-time approval
of that text), and run `gg init --mcp` so Claude Code loads gg's MCP
server. The overseer then calls the `agent_start` tool — or
`gg agent start --worktree <path|name> --tool "Claude (yolo)" --prompt-file
brief.md` — and the worker appears as a sub-row with a status line; your
focus stays where it is. The worker reads its task with `agent_task`. The
overseer's worktree claim moves to the worker and comes back when it ends. A
worker cannot start workers, and `agent_send` / `agent_kill` (`gg agent send`
/ `kill`) reach only the agents you started; `agent_list` / `agent_screen`
read every session of the TUI.

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

### Repository preflight (`gg migrate`)

gg features declare what they need from a repository — a git version floor,
or a data-format range for the store they own — and a criticality: **core**
git-version support is Required (gg refuses to start without it), while
things like branch versions are Optional. On every launch gg resolves each
feature's requirements against the repository and gets back one of three
verdicts: satisfied, repairable (a migration would fix it), or unsatisfiable.
**A disabled feature means gg keeps running normally, just without that one
capability** — the affected commands report `ErrFeatureDisabled` instead of
doing anything partial or corrupting state, and a standing notice in the
notification center (`!`) names which feature is off and why. A Required
feature that's unsatisfiable is the only case that stops gg from starting at
all, and it explains exactly what's missing when it does.

When a migration exists and would help, the TUI asks about it as a
pre-launch **Migrate / Skip / Quit** prompt on *every* start — deliberately
never suppressed, because a feature silently left disabled is the exact
failure this gate exists to prevent — and `gg web` offers the same choice
through a consent panel. Scriptable: `gg migrate` lists every pending
migration and what applying it would discard, changing nothing; `gg migrate
--yes` applies them. Store formats live in the repository itself as marker
refs (`refs/gg/meta/<store>/<N>`, pointing at the empty tree), stamped by
each store's own writer the first time it writes — never by gg just opening
the repo — so switching repositories or checking one out fresh never costs
an extra write and two `gg` processes starting at once never race.

The `versions` store's own migration (format 1 → 2, format 2 adding the
frozen-preview fields — see **Branch versions** below) is a straight
**discard**: a format-1 record carries no merge-base, so it can never be
converted into a preview, only thrown away. **A repo that still holds
format-1 data records no new versions at all until you migrate** — the
branch-version writer is gated on the same format check, so a
rebase/merge/pull there runs with no safety net rather than
silently mixing formats.

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
branch, and **every** `gg pull` — the fast-forward lane included. A plain
commit, cherry-pick, push, stash, and switching branches are **not**
triggers — the old tip stays reachable as an ordinary ancestor, so nothing
needs recording. A snapshot failure never blocks the real operation
(best-effort by design).

A fast-forward pull records nothing *interesting* — the branch contributed
nothing on top of the tip it fast-forwarded to, so the record carries
`Base == Ours` and the drift check stays silent — but it does record. It
has to: drift is always measured against the branch's **newest** version,
so a pull that wrote no record would leave the next check comparing against
some older rebase or merge and reporting the commits the pull just brought
down as drift. Every `gg pull` therefore writes a version ref; the 90-day
prune policy below keeps the volume in hand.

A version records more than a tip: for a two-branch op it also carries the
tip it landed on/against and their merge-base, so it can open later as a
**frozen PR-style preview** — the change set exactly as it stood at
operation time, not a live diff against wherever the branch has moved to
since. A one-branch op's record (amend, reset, undo-commit, delete-branch,
restore) carries no such endpoints and opens as the plain commit view
instead.

Browse a branch's versions from the Branches panel's `.` menu → **"Previous
versions…"**. From the command palette (`ctrl+p`) → **"Branch versions…"**
picks any branch that has recorded versions — including a **deleted** one
— which is how you recover a deleted branch's history. In the popup:
`enter` opens that frozen preview (or the commit view for a one-branch
record), `r` restores it (reset the branch in place, or start a new branch
at that version instead — a non-destructive alternative), `d` deletes just
that snapshot, `y` copies its sha, `L` copies its preview link
(`gg://<repo>@<base>..<ours>?version=<id>`), and `.` lists all of these as an
action menu. Settings (`,`) → **"Operations history"**
shows and edits the retention window and toggles recording on/off.

After a rebase, merge, or pull completes, gg compares what the branch
contributed going in against what it contributes coming out, against the
same other side, and flags anything the operation itself changed — a path
that's newly there, or one whose status flipped (`M` → `A` is a
**resurrected** file: the classic case is a modify/delete conflict resolved
by keeping a file upstream had deleted). A path that only *disappeared*
from the branch's contribution is noted quietly as absorbed upstream (an
identical fix, a cherry-pick landed it already) — never alarmed, and silent
when nothing changed. The CLI prints this summary right after `gg
rebase`/`gg merge`/`gg pull`; the TUI raises a notice on drift (or when an
operation completed after pausing for conflicts); `gg web` shows it in a
drift panel. The notice and the panel both copy the compared version's
preview link (**Copy preview link** / **copy preview link**).

Scriptable: `gg versions [<branch>]` lists a branch's recorded versions,
newest first (default: current branch); `gg versions show <branch>
<id|latest>` prints the frozen change set a version recorded (or says so
plainly for a one-branch record with no preview); `gg versions restore
[--discard] <branch> <id|latest>` restores one (`--discard` answers the
"the working tree has uncommitted changes" prompt for a current-branch
restore).

`[versions] disabled` (default `false`) is a kill-switch — set `true` to
stop recording entirely. `[versions] max_age_days` (default `90`) prunes
snapshots older than this on the branch's next write; `-1` keeps them
forever.

`[notes] max_age_days` (default `30`, `-1` keeps forever) and
`[notes] max_entries` (default `2000`, `-1` uncapped) bound the review-note
store. Notes live outside git, per repo, under
your XDG state dir — they never travel with a branch. A background sweep at
every gg start drops notes past the age limit and notes whose anchored lines
are gone; `-1` in either key means "keep forever" / "uncapped".

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

Blocks the wizard writes carry a **template stamp** (`template_version`,
`agent_range`, `fingerprint`). When a newer gg ships a better template — or
your agent's own version now needs a different one — gg offers it: a notice
("External-tool template updates available"), an **(update available — u)**
mark on the row in Settings → External tools, and the web page's external
tools overlay. **`u`** shows why and the new block's full text; **take new**
rewrites just that block in the file that holds it, **keep mine** leaves it
and stops offering that update until the template changes again, **edit**
opens the config in your editor. Nothing is ever rewritten without your
answer; formatting-only edits (spaces, comments) don't count as edits, and
blocks you wrote yourself are never touched. gg reads each agent's version
with `<agent> --version` in the background; if an installed agent is outside
every version range a template supports, a notice says so and the block
keeps running as is.

In the conflict window (`x`), press **`t`** (shown only when at least one
`conflict` command is configured) to pick one: repo-level agent commands
(Claude Code, Junie, Kimi Code) are always listed while an op is paused, get a per-run
temp **context file** — the paused op, source, target, and the conflicted
paths, one per line — exposed as `<context-file>`/`GG_CONTEXT_FILE` plus ten
more `GG_*` env vars, and either hand over the terminal (interactive agents
like Claude Code and Junie) or run **headless in the background** (`mode =
"capture"` — Kimi Code, whose `kimi -p` draws no terminal UI of its own, and
the opt-in "Claude (yolo, headless)" and "Junie (headless)" rows)
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

**Resolve & complete.** The same `t` picker also lists any configured
`conflict_complete` commands (shown whenever an op is paused): an agent in
this category doesn't just resolve the conflicts, it stages them and drives
the operation to completion itself — the matching `--continue`, repeating
through further rebase rounds, never `--abort` — then reports an overview
via `$GG_MESSAGE_FILE`, which gg opens in a read-only report viewer on a
clean exit (the conflict window closes first). Catalog defaults ship for
Claude Code, Junie, Codex, and Antigravity (terminal handover with their
bypass-permissions flag) and Kimi Code (headless capture, since `kimi -p`
has no bypass flag to combine with). Claude Code and Junie also ship a
**headless** resolve-and-complete row for the TUI and web (`claude -p …
--dangerously-skip-permissions`, `junie --task …`); Codex and Antigravity's
headless rows are web-only. Every row is deliberately **yolo-only
and unchecked by default** in the wizard — completing your paused operation
autonomously is an explicit opt-in, checked or not.

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
(`↑↓`/`pgup`/`pgdn`/`home`/`end` scroll, `ctrl+w` wrap mode, `/` search, **`e`**
opens the report file in `$EDITOR`, `esc` closes); a failed or empty run
reports the error in the status line instead. Every commit, range or branch
review is stored as a **note on the reviewed commit** (a range's last commit; a
branch review's tip, carrying the branch name) — never as a file. You find it
from the thing it is about: an `@notes/` entry at the top of the commit's file
list (and at the top of the stacked diff), a `◆n` marker and review rows under
the branch in the Branches tab (only reviews of the branch's current tip; `.` →
**Show review** opens the newest), under the commit in **View all notes…**, and
from the run in the `ctrl+\` AI-tasks tab. Deleting or renaming a branch in gg
takes its reviews along; review notes never expire. A review of uncommitted
changes has no commit, so it stays in the AI-tasks history only. The same pipeline is
scriptable as `gg review`
(see the CLI section above).

A review agent replies with ONE JSON document (the brief at
`$GG_CONTEXT_FILE` ends in a "Review output" section describing it): a
markdown overview, a one-line summary per file, line notes, and free-form
`meta` (a verdict, a severity, a confidence) at every level. Opening such a
review shows the **review view** — the reviewed commit's files (a branch
review: its range's files) with `≡ Overview` first, `◆n` and the file's
summary on each file the review notes (`n` / `p` step between them); enter on
the Overview renders the overview as markdown with its meta and the notes on
files the commit does not change; stacked, the overview is the first element.
`o` in the overview lists those other notes to open one. Its diffs show only
the review's notes, read-only — `gg review --notes` keeps them as your own.
"Delete review" in the `.` menu (in the review view or on a Branches review
row) or `ctrl+d` in View all notes deletes a review, after asking. A
reviewed commit carries `✎` in the Commits list (after its tip markers). The
browser UI (`gg web`) shows the same reviews: a commit's under **Reviews**
above its files, a branch's under its row; clicking one opens the review
view (Overview in the diff pane, read-only review notes in the diffs), and a
right-click offers **Delete review**. A reply that is not the document is kept as text and opens
rendered as markdown. Catalog defaults ship for Claude Code, Codex, Junie,
Kimi Code and Antigravity; their documents come back through
`$GG_MESSAGE_FILE`, fed the diff via `$GG_REVIEW_DIFF`. Review commands an
older gg wrote into your config are upgraded by a one-time migration that
asks first (edited commands are left alone).

### AI tasks (launch dialog, Headless tab)

Every AI action — `ctrl+g` in the commit box, the Review … rows, the conflict
window's "Resolve with an agent…" / "Resolve & complete with an agent…" —
opens a launch dialog titled with the task key. Pick the agent (`←/→`) and
how it runs (`↑/↓`): **interactive in the background** (an agent session you
can open later), **interactive in the foreground** (its console opens), or
**headless** (queued and captured). An interactive agent keeps running after
it writes a result, so you can ask for changes: each write of
`$GG_MESSAGE_FILE` is applied again.

Tasks with the same key run one after another; at most `[tasks]
max_parallel` (default 3, clamped to 1..10) run at once. `ctrl+\` → `tab`
lists them — live ones first, then the last 50 finished ones across all
repos — and ◆ rows under Worktrees show a worktree's queued and running
headless tasks.

```toml
[tasks]
max_parallel = 3
```

### Image diffs in `gg web`

A diff of **images** (a modified, added or deleted PNG/JPEG/GIF) shows the
pictures instead of `binary file`, at full resolution, fitted to the pane and
never enlarged. `w` (or the chips above them) cycles **side by side** →
**stacked** → **one at a time**, where `tab` or a click on the image flips
old ↔ new; each side's info line names its format, pixel and byte size. An
added or deleted image shows alone, unmarked. The stacked view (`S`) and the
file-history overlay show each pair small and side by side. Every diff source
works alike — a commit, the working tree or index, a branch or link
comparison.

### The file viewer in `gg web`

Right-click (or `.`) a file in the browser's working-tree list, a commit's
file list or a shelved file and pick **view file**: the file opens full-page —
the bytes on disk for a working-tree row, that commit's version for a commit
row, the frozen bytes for a shelved file — with line numbers and syntax
colour. `↑↓` / `j k` move a line cursor (click a line too), `/` finds (`]` `[`
step), `w` cycles long lines, and `.` (or right-click) offers **copy file
link** at the cursor line, **copy line**, the file's **diff**, **file
history** and **blame**; `esc` closes. The keys show in the page's bottom bar
while the viewer is open. A content link lands here too — an agent's `gg
session navigate`, `gg open --web`, or a link pasted into the `#` prompt
(which now takes any `gg://` link as well as a commit). A commit or shelf
version copies a content link only while the file on disk still holds the
lines shown.

Viewed files stay open, one list per worktree shared by every tab of that
`gg web`: `ctrl+]` sends the viewer to the background and `ctrl+\` lists the
open files (`●` the one this tab shows) — `enter` brings one back where this
tab left it, `x` closes it in every tab. A working-tree file follows the disk:
an edit reloads it in place, a deleted file shows `(file deleted on disk)`
until it comes back.

`F` turns the file list into every file in the working tree (grouped under
directory headings like a commit's files, with a status letter — `?` marks an
untracked one), over whatever screen you are on (an image is shown as a picture scaled to the pane, any other binary as a one-line placeholder): `/` filters (fuzzy, or `'exact` `^prefix` `suffix$` `!not`),
the diff pane previews the file under the cursor (following it on disk), and `enter` / `.` (or
right-click) offers **view file**, **diff**, **file history**, **blame** and
the copy rows — a double-click views the file. `ctrl+]` opens the row in the
background; `esc` gives the panes back as they were.

### Open files (background file viewers)

A file you view — **View file** in a commit's file tree, a pasted `gg://`
content link (`#`), an agent's `gg open` — stays **open** when you step away
from it: `ctrl+]` sends it to the background (the `.` menu's **Send to
background** does the same), `esc` closes it. `ctrl+\` lists this
worktree's open files under the agent sessions (`●` on screen, `○` in the
background, with the cursor line and the version: `working tree`,
`@ <sha>` or `shelf`); `enter` brings one back exactly as you left it
(cursor, scroll, selection, search), full-screen, and `x` closes it.
Opening a file that is already open reuses it; up to 20 stay open per
worktree, and the 21st closes the one you looked at least recently (the
bottom bar names it). The list lives as long as gg runs.

An open **working-tree** file follows the disk: when it changes (an agent's
edit, a save in your editor) it reloads by itself, keeping your cursor line,
scroll and search — the file on screen within about a second, background
files within five. A file deleted on disk shows `(file deleted on disk)` and
returns, at your line, when the file does. Commit and shelf versions never
change.

An **agent** can use the list too, without touching your screen: `gg open
<content-link> --background` loads a file into it (answering `opened <path>
in the background [at line N]`; a file you are looking at is left alone),
`gg session files [--json]` lists the open files (id, path, version, cursor
line, on screen or in the background — also `open_files` in the MCP
`gg_ui_state`), and `gg session files focus <id|path>[:<line>]` brings one to
the front. So an agent walking you through a change can open A, B and C in
the background first, then bring each up in turn as it explains it. These
work against a running TUI or `gg web` (they never start one): the web page's
server answers from its shared list, and a focus brings the file up in every
open tab.

An agent can also **explain itself on the code**: `gg session note add
<path>:<start>[-<end>] --summary "…" [--rationale "…"]` puts a temporary note
on those lines of an open file (opening it in the background first when
needed). You read it in a box under the lines; `}` / `{` step through the
notes (and on to the next open file that has any), `d` dismisses the one under
the cursor, and `r` copies a reference — `gg note t7 <path>:<lines>` — to paste
back to the agent when you want to ask about that remark. A note taller than
half the window is cut in place; `enter` opens it in full. `gg session note
list|show|rm|clear` are the agent's other verbs. The notes are temporary: they
follow their lines as the file changes, a file carrying them stays open on esc,
and closing it (X) drops them. The gg web page shows them too: a box under
the lines in its file viewer, with `d` / `r` / `}` / `{` and **dismiss** and
**copy reference** buttons, and `· N notes` on the file's switcher row. A TUI
that serves its own web page (`[web]`, `gg --web`) shows the same notes in
both — dismiss one in the browser and it leaves the terminal. A standalone
`gg web` answers `gg session note` itself when no TUI is running.

For a **guided tour** the agent writes an overview: `gg session overview add
--title "…" < tour.md` shows a markdown document that lives in memory,
whose links are anchors — `[the loader](internal/tui/open_files.go:220)`, a
range `path:120-140`, or one of its notes `note:t7`. `tab` / `shift+tab`
select the next / previous anchor, `enter` (or a double click) opens it — the
file comes to the front at that line, a range selected — and `backspace` in
that file brings you back to the overview on the same anchor; the file stays
open in the background. `r` copies a reference to the anchor for the agent
(`gg overview f12 "Tour" → internal/tui/open_files.go:220`),
`y` the whole text. `gg session overview set|list|show|rm` are the agent's
other verbs; an overview is an open file (esc backgrounds it, X closes it, the
ctrl+\\ switcher lists it). gg web shows overviews too: the viewer opens one
as a document, `tab` / `shift+tab` / `enter` / `r` / `y` / `backspace` work
the same, and a single click opens an anchor. A TUI serving its own page
shares one set of overviews with it (same ids; close one in either and it
closes in both), and a standalone `gg web` answers `gg session overview`
itself. With both running, a `gg session` verb goes to the TUI (navigate, reload,
focus, highlight and background opens reach the browser too); `--to web` or `--to tui` after the verb picks one
side — `gg session overview add --to web …` puts the tour in the browser and
leaves the terminal where it is.

### Agent sessions (embedded consoles)

On a **Worktrees** row, the `.` menu's **Start agent** runs an interactive
agent (Claude Code, Codex, Junie, Antigravity, Kimi Code — or any command you
configure) **inside gg**, in that worktree, in a live console that takes the
Commits column. Type to it as in any terminal: while the console is
**focused, every key goes to the agent** — `esc`, `ctrl+t`, `ctrl+o`, `q`
included — except two:

| Key | Focused console | Unfocused console (its column focused) |
|-----|-----------------|-----------------------------------------|
| `ctrl+]` | step out (a maximised console shrinks back to the Commits column) | close the console, as `esc` does — so `ctrl+]` twice puts a focused console in the background |
| `ctrl+\` | the agent-sessions popup | the agent-sessions popup |
| `alt+a` / `alt+t` | → agent | show the next agent / terminal by last use (see the key table) |
| `enter` | → agent | focus the console (type again) |
| `ctrl+t` | → agent | maximise over the whole body, focused |
| `esc` | → agent | close the console — **the agent keeps running** |

A running or exited session shows as a sub-row under its worktree
(`└ ● Claude  running 12m` / `└ ○ Codex  exited (0)`); `enter` on it (or its
`.` menu **Open session**) brings the console back, and the menu also offers
**Kill session** / **Kill and remove session** / **Remove session**; on an
exited session's sub-row, `x` removes it without opening the menu (a running
one is refused), and on a running one `X` kills it (after a Kill / Cancel
confirm) and drops the row once it has exited; the footer says `[enter] open` / `[x] remove` / `[X] kill+remove`
there. A `/` filter matches a
worktree together with its sub-rows, so typing a session's label keeps the
worktree it runs in. The same menu's **Open terminal** starts
an interactive shell in that worktree in the same console (`$SHELL`, or
pwsh → powershell → cmd on Windows; `[console] shell` overrides). Both rows
also sit on a **Branches** row whose branch is checked out in a worktree (the
rows that show a worktree path), as **Start agent in \<worktree\>** /
**Open terminal in \<worktree\>** — they run in that worktree; a branch
checked out nowhere has nothing on disk to run in and gets neither. Such a
branch also lists its worktree's sessions as the same sub-rows, right under
the branch (through any sort or filter), with the same `enter` / `x` / `.`
menu. Sessions belong to the gg process, not
to a repository: switching worktree or repository (`R`) keeps them running,
and `ctrl+\` reaches every one. Quitting gg with live sessions opens that
popup in quit mode — `Q` kills them all and quits, `esc` cancels. A worktree
with a running agent cannot be deleted until the agent is killed.

Every session gg starts gets `GG_INBOX` (gg's steering inbox), so
`gg session …` run inside it reaches the gg that started it, whichever
worktree gg shows by then. When an agent asks gg to show a file in a worktree
gg is not showing, gg does not move: a notice (`!`) offers **Switch to <b> and
show** or **Ignore**, and the agent is told it asked you.

The commands come from the `session` external-tool category. The first
**Start agent** with none configured detects the installed agents and writes
their plain interactive commands to the global config (yolo variants stay
opt-in in Settings → External tools); a custom one is

```toml
[[tools.command]]
category = "session"
name = "Claude (opus)"
mode = "session"
command = '''claude --model opus'''
```

and runs through the usual first-run approval. The two reserved keys are
configurable:

```toml
[console]
step_out_key = "ctrl+]"   # a Bubble Tea key name
sessions_key = "ctrl+\\"
```

### Agent consoles in `gg web`

The web page shows the agent sessions of the process that serves it — so
serve it FROM THE TUI: **Open in browser** in the command palette
(`ctrl+p`) starts the page inside the running gg and opens the browser; the
sessions you started in the terminal are the ones the page lists. (A
standalone `gg web` is its own process and shows only its own.) `ctrl+\` is
a tabbed switcher — **Agents** (every repo, grouped repo → worktree), **AI
tasks** (read-only for now) and **Open files** — and `enter` on a session
opens it as a live console over the panes, painted from the server's screen
(nothing to install in the browser, nothing can desync). Type as in the TUI:
a focused console sends every key to the agent except `ctrl+]` (step out)
and `ctrl+\`; the browser keeps `ctrl+w`, `ctrl+t`, `ctrl+n` and friends
(the bottom bar says so). Unfocused: `enter` focus, `m` maximize over the
sidebar, `esc` or `ctrl+]` again close — the session keeps running. Whichever viewer has the
console focused — this tab, another tab, or the TUI — sets the session's
size; the others scroll. Every worktree row in the sidebar lists its
sessions beneath it (`└ ● claude running 12m`, `○` when exited); a click
opens one, and an exit raises a toast. Starting, killing and removing
sessions from the web is not built yet.

The hosted page and the terminal switch together: switch repo or worktree
in the TUI and the page re-roots with it; switch from the page (palette
`switch repo…`/`open repo (path)…`, a worktree's `switch here`) and the TUI
switches too. While the terminal is busy (an operation running, a popup or
prompt open, text being typed) the page's switch is refused with the
reason; quitting gg ends the page and open
tabs show a server-down veil. Serve at startup with `[web] serve = true`
or `gg --web`, pin the address with `[web] addr` or `gg --web-addr
127.0.0.1:7777` (default: a random loopback port each run — a fixed one
keeps the browser's per-page preferences). Settings (`,`) → **Web page**
shows the URL and edits both. `gg open --web <link>` with a TUI live in
that checkout asks it to serve instead of starting a second server.

```toml
[web]
serve = false            # serve the page when the TUI starts (browser not opened)
addr  = "127.0.0.1:0"    # loopback only; :0 = a random port each run
```

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

See [`CLAUDE.md`](CLAUDE.md) for the architecture and the conventions the
code follows. The project does not take outside code — see
[`CONTRIBUTING.md`](CONTRIBUTING.md) for what is welcome instead.

## License

gigagit is source-available under the
[PolyForm Noncommercial License 1.0.0](LICENSE): free for personal, hobby,
research, educational and non-profit use; **commercial use needs a license
from the author** — see [`COMMERCIAL.md`](COMMERCIAL.md). Everything
published before this change — the releases through v0.3.0 and the commits
up to `0b844a31` — was under MIT and remains so. The bundled open-source Go
modules keep their own licenses, listed in
[`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md).
