# Changelog

All notable changes to gigagit (`gg`) are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
No tagged release has been cut yet; everything lives under **Unreleased**.

## [Unreleased]

- **Web: staged detail layout (the GitKraken flow).** Clicking a commit no
  longer jumps straight to a diff: the commits pane shrinks to the left
  and the changed-file list appears as a fixed column on the RIGHT, with
  nothing auto-opened — click other commits to browse their files in
  place. Clicking a file then opens its diff in the space the commits
  occupied, file list staying right; esc steps one stage back (diff →
  file list → full-width commit list). The same staged flow applies to
  the working-tree row, stash drill-ins, sidebar tags, and branch
  compare. The files/diff drag handle now resizes the right-hand file
  list column (width persisted as before).

- **Web: version ↔ tip compare and the all-branches versions picker.** The
  two pieces deliberately left out of the web's "previous versions…" popup.
  A version row's menu gains *compare against current tip* — the whole-tree
  compare of a recorded snapshot against the branch's live tip, in the
  existing compare view with readable labels (`<branch>@<short> ↔ <branch>
  (tip)`); absent for a deleted branch (restore it to compare, the TUI's
  rule). A new *branch versions…* entry (palette + ☰ menu) opens a picker
  over every branch with recorded snapshots — deleted branches tagged red —
  and drills into the versions popup, which is the deleted-branch recovery
  path: restore there recreates the ref. Transport: `/api/compare` gains an
  explicit rev form (`revs=1`, both sides plain hex ids, no branch-list
  resolution — the name allowlist's empty-vs-unknown ambiguity doesn't
  exist for hashes, which fail loudly), `/api/diff`'s `left`/`right` now
  enforce the hex-only contract their doc always promised, and
  `GET /api/version-branches` lists every branch with snapshots.

- **fix: external tools ran with none of their flags on Windows.** Selecting
  *Claude (yolo)* for a conflict launched Claude in normal permission mode —
  `--dangerously-skip-permissions` never reached it. gg writes the configured
  command to a temp `.bat` and runs it through cmd.exe, which cannot express
  two things a POSIX shell accepts and gg's own templates use: a line ending
  in a backslash (POSIX continuation) and a double-quoted string spanning
  lines. cmd.exe ran the tool from the FIRST line — truncated prompt, no flags
  — and treated the rest as separate commands. Both shapes are now joined
  before the script is written; a genuine multi-line batch script (a worktree
  post-create hook) still runs line by line. Affects every Windows lane that
  runs a configured command: the conflict picker, `ctrl+g` commit-message
  generation, AI review (TUI, CLI and web) and the post-create hook. **No
  config change is needed** — existing `[[tools.command]]` blocks are repaired
  as they run. New blocks are also **written** single-line on Windows, so the
  config says what will actually run — it is what the approval popup shows
  before a command's first run, and gg should not execute text you were not
  shown.

- **fix: interactive rebase and reword failed on Windows.** git runs
  `GIT_SEQUENCE_EDITOR` and every rebase `exec` line through its own bundled
  POSIX sh — not cmd.exe — and gg passed the gg binary's path unquoted, so a
  Windows path's backslashes were eaten as escapes: `t:\others\…\gg.exe`
  reached the shell as `t:othersgg.exe` and git reported *"there was a problem
  with the editor"*. Both paths are now converted to forward slashes and
  single-quoted. The same defect broke any path containing a **space** on
  every platform, which is what the new regression test exercises. Affects the
  TUI's interactive rebase, reword and single-commit move/drop as well as the
  web editor.

- web: **an interactive-rebase plan editor.** Drag a branch onto another and
  pick *interactive rebase A onto B…*: every commit A has that B does not is
  listed newest-first, and each row can be **pick**, **reword** (the message
  editor opens straight away, prefilled with the real message, body included),
  **squash** (meld into the commit below it) or **drop**, with ↑/↓ to reorder.
  **Nothing runs until you press start** — cancel, esc and clicking outside all
  leave the branch exactly as it was. The oldest row cannot squash (there is
  nothing older to meld into) and a row moved into that slot stops being one.
  The browser may reorder and annotate the plan but never invent it: the server
  rebuilds it against a freshly read range and refuses (409, nothing run) a
  plan that names a commit outside the range, names one twice, or no longer
  covers the branch — which is also the staleness guard when a commit lands
  while the editor is open. A message likewise only comes off the wire for a
  row you marked as a reword.

- web: **right-clicking a commit now opens gg's own menu** — previously it
  fell through to the browser's. It carries *show this commit*, *copy commit
  id*, *copy subject*, and the single-commit history edits: **move up
  (newer)** / **move down (older)** swap the commit with its neighbour, and
  **drop this commit** removes it (confirmed first, and shown red). All three
  rewrite the checked-out branch from that commit up, through the same
  `engine.InteractiveRebase` the TUI's commit menu uses, so the previous tip
  is recorded and a branch's *previous versions…* can put it back. They are
  offered only on an ordinary commit — a merge and the root show the copy
  rows alone — and the branch is left untouched when the commit is not on it,
  when it has no neighbour in that direction, or when the range spans a
  merge, each refused with a message on the status line. The wire carries a
  commit id and one of three verbs; the rebase plan is built on the server
  from a range it reads itself.
- web: **clicking the "review running in the background" line now reopens the
  review** instead of doing nothing — and double-clicking it no longer makes
  it vanish. That line is the most visible mention of a backgrounded run, so
  it is what you click; the status strip's dismiss-on-double-click is only
  advertised in the header shown for *errors*, which made it invisible here.
  Every other status message keeps its old dismiss behaviour.
- web: the **"review running in the background" line now clears when the run
  ends** instead of being replaced by another line you had to dismiss by
  hand — the chip in the top bar is what announces the result. (A failure
  still says so: an error that vanished silently would be worse.) The line
  also expires normally while the run is going, rather than sitting there for
  the whole run.
- web: fixed **double-clicking the ready chip discarding the report**. The
  first click opened it and the second landed on the report's own backdrop,
  closing it again — with the chip already cleared, so there was nothing left
  to click. The report viewer no longer closes on a backdrop click at all: it
  is a document to read, not a picker, and closing stays the button or esc.
- web: an AI review can now be **put in the background**. *run in background*
  (or esc, or clicking outside the box) hides the waiting dialog and leaves
  the agent running; a chip appears in the top bar, and clicking it brings the
  run back so you can watch or cancel it. When the report is ready the chip
  fills and blinks, and the report **waits there** until you click it —
  finishing a run no longer interrupts what you were doing.

  Backgrounding never cancels: only the labelled *cancel the run* button does.
  While a run is live you can read the repo normally, but a second operation
  still waits for it — and now says so instead of doing nothing, which
  previously only worked because the dialog was on screen to explain itself.
- web: fixed **switch repo** offering the repository already open — picking it
  re-rooted onto the same repo. The list is filtered by the server now,
  because the two sides of "is this the one I'm serving?" were spelled
  differently: git reports a top-level with forward slashes even on Windows,
  while the recent-repos registry stores platform-cleaned paths, so on
  Windows the two never matched. Linux was unaffected, which is why it took a
  report to find.
- web: **review with an AI agent** — right-click a branch for *review … (AI)*,
  or reach *review working changes (AI)* from the ☰ menu and the command
  palette. gg runs a review agent you have configured (a `[[tools.command]]`
  block with `category = "review"`, the same ones `gg review` and the TUI
  use), then shows the report and saves it to a file.

  The browser names a **tool**, never a command: the command text comes from
  your own config, is resolved server-side, and is shown to you in full
  before its first run. Approving it is remembered per repository until you
  edit the command — and the memory is shared with the TUI, so approving in
  either covers both. A run holds the operation lane and can take minutes, so
  it can be **cancelled** at any point, including while it is starting.
- web: fixed the **previous versions** rows being dead to the mouse. A left
  click opened the row menu and the same click immediately closed it again;
  a right click fell through to the browser's own menu. Both buttons now open
  the row menu and it stays up. It was then still invisible, because the
  right-click menu (and the confirm dialog it raises) painted *underneath*
  the popup — both are now stacked above every panel overlay, so a menu or a
  confirmation opened from inside a popup is always on top of it.
- web: **force push** — a new row on a branch's right-click menu. It does not
  force anything on its own: it opens the same force-mode prompt an ordinary
  rejected push shows — *force-with-lease* (refuses if the remote moved since
  your last fetch), plain *force*, or abort, the two force options in red.
  The row's only effect is reaching that prompt without waiting for a
  rejection first; a silent force push is not expressible from the browser.
- web: **previous versions** — right-click a branch for the operations
  history: what it pointed at before each recent merge, rebase, amend, reset
  or delete, newest first with how long ago. Click one to restore the branch
  to it (asked for first), copy its commit id, or delete the snapshot.
  Restoring is itself recorded, so it can be undone the same way.
- web: **compare two branches** — drag one branch onto another and pick
  *compare A ↔ B* (the third row, below merge and rebase). The whole
  tip-to-tip changed-file list opens in the existing detail screen, and a bar
  above it filters by **origin**: all, or only the files one side changed
  since the two diverged (a file both sides touched stays in either view —
  the TUI's rule). Unrelated histories still list their differences; only the
  filter is unavailable, and says why.
- web: **create branch…** in the ☰ menu and the command palette. Every way to
  make a branch used to hang off right-clicking an existing one; this starts
  from the current HEAD.
- web: in the working-tree view, **Staged is now the last section** rather
  than the first. Staging a hunk used to move the file up to the top and push
  the remaining work down, so the list seemed to reorder itself under you
  mid-task. Order is Changes, Untracked, Conflicts, Staged.
- web: **rename a branch**, **create a branch from one**, and **create a
  worktree for one** — three new rows on a branch's right-click menu, each
  asking for the name or path in a shared one-line prompt (enter confirms,
  esc cancels). The worktree row appears only when the branch has no worktree
  yet, and its path is pre-filled with a sibling of the main worktree named
  `<repo>-<branch>`. A configured worktree post-create hook is honoured
  rather than skipped: the engine shows the script in the decision modal and
  runs it only if you say so.
- web: **solo a branch** — right-click a branch → *solo this branch* narrows
  the commit list to its history, with a `solo: <name> ✕` chip in the top bar
  to leave again. The scope lives on the server (there is one commit feed, so
  every tab shares it) and survives commits, merges and pulls until cleared;
  a branch that does not exist is refused rather than entered, since a scope
  that cannot render would take the exit affordance with it.
- web: **fetch all remotes** in the ☰ menu and command palette, and per-branch
  **pull** / **push** on a branch's right-click menu. Pulling a branch you are
  not standing on updates it in place and leaves you where you are — no
  checkout, so nothing to confirm; pulling the branch you *are* on keeps the
  existing confirm, since it can rewrite the working tree. `pull` and `push`
  now take an optional `branch` on the wire; omitting it means the current
  branch, exactly as before.
- web: a branch's right-click menu gains **copy branch name**, **copy commit
  id** (its tip), and **copy worktree absolute path** — the last only when
  some worktree has that branch checked out. A copy now reports what it
  copied on the status line; it used to report only failures, so a silent
  clipboard write was indistinguishable from nothing happening.
- web: the pane dividers are draggable — the sidebar in the commit list and
  the file list in a commit's detail. Double-click a divider to reset it.
  Each width persists (`gg.sidebar.width`, `gg.panes.files-width`) and is
  clamped to the window, so a stored width squeezes rather than pushing the
  neighbouring pane off-screen and comes back when the window widens again.
- web: drag a branch onto another in the sidebar to merge or rebase —
  the drop opens a menu offering "merge A into B" / "rebase A onto B",
  dispatching `engine.SmartMerge` / `engine.SmartRebase` over the existing
  op transport (conflicts park in the decision modal).
- web: the line-mode commit dot now aligns with the graph's leftmost
  lane; the working-tree row's dot aligns in both modes.

- tui: **the `[E]` message viewer reads as one block, on one margin.** The window
  used to indent its three kinds of line by three different amounts — the title
  and the key hints at the box padding, each message line two columns further
  in, and a *wrapped* message line all the way back at the padding — so a single
  wrapped error came out as a ragged staircase, with the hints running straight
  on from git's stderr. Now the message is a faint band: every line, wrap
  continuations included, starts in one column and keeps an equal margin on the
  right (the indent moved outside the window rather than into the row text,
  which is what carries it onto a continuation), the band covers the text
  columns only — no tinted gutter down either side — and a blank line below it
  sets what follows apart from the message. Only the background changes, so the
  text keeps the terminal's own foreground and stays readable inside the red
  danger frame. The `saved to …` note now sits between one blank line above and
  one below rather than two above and none. Fixed alongside it: a blank line in the
  message wrapped to *no* segments while still occupying a display line, so the
  window came up one row short per blank and clipped the tail off the very
  message the viewer exists to show — git separates its stderr paragraphs with
  blank lines, so the common `Please make sure you have the correct access
  rights / and the repository exists.` block lost its last line.

- tui: **`s` saves a popup's text to a temp file** and reports the path — in the
  notification dialog and in every `contentPopup`-backed window (the `[E]` full
  message viewer, help, …). Reading a fix instruction out of a popup and then
  getting it into a shell was a chore: a terminal multiplexer selects whole
  terminal-width *lines*, so a centred box comes with the panels either side of
  it, and the box's own wrapping breaks a long command across rows. The RAW
  lines are written, never the wrapped render, so a `sudo …` block comes back as
  one pasteable line. Deliberately a file rather than the clipboard: gg's
  clipboard path can silently no-op (a WSL `clip.exe` that can't exec, an OSC 52
  escape the terminal drops) while still reporting success — and the reason
  someone is reading a fix instruction out of a popup may be that copying is
  what broke.

- tui: `[E]` now reaches every status message the one-line bar had to cut, not
  just the seven prefixes `statusIsError` recognises. That predicate gated both
  the red styling *and* the capture into `lastError`, while the TUI sets ~40
  distinct error-prefixed messages — including the per-source refresh failures
  (`branches: <git stderr>`, `worktrees: …`), of which only `commits:` happened
  to be listed. A repo switch that failed while reloading branches truncated a
  paragraph of git stderr into one line with no way to read the rest. Capture is
  now structural (`statusNeedsFull`): anything too wide for the bar is kept,
  error or not, so a message added anywhere can never be missed again. The
  pointer renders for those too, and the viewer titles itself "Full message"
  with no red frame when the message is not a failure. `[E]` relabelled from
  "last error" to "full message".
- tui: a snapshot load that fails **after** the UI is up — a repo switch into an
  unreadable repo — no longer replaces the whole screen with a bare `error: …`
  line that has no status bar, no footer and nothing to press. Once one load has
  succeeded (`loadedOK`), a later failure is reported like any other, keeping
  the interface and making the full text readable via `[E]`. A first-load
  failure still shows the bare error: there is no interface to preserve, and an
  empty frame would read as a working UI onto a broken repo.

- clipboard: detect a WSL kernel that cannot execute Windows binaries, and stop
  handing copies to a `clip.exe` that will fail. `exec.LookPath` finds
  `clip.exe` whether or not WSL interop is registered, so gg used to choose it,
  fail at exec time with `exec format error`, fall through to the OSC 52 escape
  (whose write always "succeeds"), and report a green `Copied …` while the
  clipboard never changed. `nativeCopyCmd` now gates `clip.exe` on a
  `binfmt_misc` probe: with interop dead it falls through to `wl-copy` / `xclip`
  / `xsel`, which under WSLg reaches the Windows clipboard anyway. An
  unreadable `binfmt_misc` is treated as working, so a machine gg cannot
  inspect (WSL1, a sandboxed `/proc`) keeps its existing behavior. A new
  `wsl_interop_broken` notice fires in the `!` center — only when no fallback
  tool covers it, superseding the generic "install a clipboard tool" notice so
  one problem yields one notice — explaining why the green status lied and
  offering both remedies: a persistent `/etc/binfmt.d/WSLInterop.conf` or
  installing `wl-clipboard`. Dismiss-only, and it self-clears once either fix
  lands. New `debugging-clipboard-copy` project skill documents the whole
  triage.


- web: command palette (`ctrl+k` / `ctrl+p`) — pull/push/refresh, sidebar and
  graph toggles, help, and a **switch repo…** mode listing previously-opened
  repos (the MRU registry) that re-roots the server in place. A global ☰
  menu in the top bar offers the same actions by mouse.

- web: hunk staging, inline in the diff — an unstaged file's diff highlights
  its change blocks in place (full context and line numbers stay visible,
  TUI-style); click a block to select it and *stage selected* in the diff
  header stages just those hunks. The diff reloads after every round (picks
  are positional against a freshness hash; a concurrent edit surfaces as
  "file changed; refresh" and reloads the blocks).
- web: right-click a changed file → **discard changes** (untracked →
  **delete untracked file**), behind a red confirm. The op resolves the
  path server-side against a fresh status read; conflicted files are
  refused (resolve instead).

- Hunk staging no longer rewrites CRLF files to LF: `hunkpick` now re-applies
  the file's own line terminator on resolve, so the TUI `H` picker and the
  web hunk view round-trip pure-CRLF files byte-faithfully. The web guard
  narrows to refusing only mixed-EOL files.

- web: error messages in the status strip carry a header bar — "Problem"
  on the left, "(double-click anywhere to hide)" on the right — and
  double-clicking anywhere in the strip dismisses it immediately (the 30s
  auto-hide stays).

- Push now detects a narrowed fetch refspec (single-branch/`--depth` monorepo
  clones): after a successful push of a branch the refspec doesn't cover, gg
  offers to add a per-branch tracking mapping and fetch just that branch, so
  the Commits panel's ↓↑ tip markers and ahead/behind can follow it. A
  notification-center notice (`!`) fixes already-affected branches the same
  way, in one batch. New CLI flags: `gg push --map` / `--no-map`.

- TUI: press **[E]** to read the last failure in full. The status bar stays
  one line, but an error now leads with an `[E] full details` pointer (it
  leads so truncation, which cuts from the back, can never eat it), and E
  opens the whole message wrapped in a red box — `/` searches it, `z` cycles
  the display mode, `ctrl+t` maximizes, esc closes. git's line structure is
  preserved, and cursor-moving control bytes are stripped: ssh ends its lines
  with CRLF, and a surviving `\r` would send the terminal back to column 0
  mid-line and overwrite the box's own border. Every failure this session is
  still kept in Settings `,` → Session errors.

- tui: the stash drill-in now shows a -u stash's untracked files (the ^3
  third parent, previously invisible); their diff, preview, history/blame,
  and bookmarks resolve against that parent via a per-line sha override.

- web: hunk-staging backend — `GET /api/hunks?path=` lists a file's
  unstaged change blocks with a freshness hash; `POST /api/stage-hunks`
  stages a picked subset through the TUI's own hunkpick → StageHunks
  machinery (409 when the file changed under the client; untracked,
  binary, and CRLF files refused with clear messages). UI arrives with
  the diff-pane work.

- web: right-click a worktree → "switch here" re-points the running server
  at it (the page reloads into the new root). Served and switched-to repos
  are recorded in the MRU registry (`GET /api/repos` lists it), so
  re-rooting away is always reversible.

- web: overlay surfaces (decision modal, help, context menu) now share one
  layer stack with a single keyboard-routing rule — groundwork for the
  command palette and menus; behavior unchanged.

- TUI: an error too long for the status bar now temporarily (30s) takes the
  footer row too, ending with a pointer to the full text in Settings `,` →
  Session errors. A newer status message collapses it back to one line
  immediately.

- web: right-click on a working-tree status file offers stage / unstage
  (per its section), stage all / unstage all, and copy path.

- web: the Working-tree row is taller (30px, larger type) with breathing
  room around the dot and label; the virtualized list accounts for its
  extra height exactly.

- web: the status/error strip wraps long messages (git's own newlines
  preserved, scrollable past 30% of the screen) instead of cutting them
  off with an ellipsis.

- web: the status/error line moved into its own strip ABOVE the bottom bar
  (it no longer covers the key hints) and auto-hides after 30s (never while
  an operation is still running); the bottom-bar entries are now clickable
  chips that run their action (pull, push, refresh, stage/unstage, graph,
  sidebar, back, help).

- web: the flat list's gutter shows one dot per commit in its lane color
  (was monochrome white); `r` soft-reloads everything, and
  the working-tree status auto-refreshes when the tab regains focus — files
  edited while the page was in the background now surface without a manual
  reload.

- web: `?` opens a help overlay listing every implemented key and mouse
  action (grouped keys / mouse / notes; esc, `?`, or a backdrop click
  closes); the footer advertises it.

- web: `g` now toggles the commit graph on/off — off is a flat ●-gutter
  list with the lane column's space going to subjects (TUI show_graph
  parity), persisted across reloads. The near-identical text-glyph
  renderer (the old second `g` state) is gone.

- web: `POST /api/reroot` — the running server can switch to another
  worktree of the repo or a previously-opened repo (MRU registry);
  allowlist-resolved, preflighted before the swap, refused while an
  operation runs. No client UI yet.

- web: transport hardening — SSE-drop recovery (reconnecting hint; a lost
  op unlocks the UI and refreshes), `resolved` wire event closes answered
  decision modals on replay and in second tabs, destructive modal options
  render red, sidebar collapse/visibility persist across reloads, stash
  apply/pop/drop guard ref+sha (409 when the list changed), gen-guarded
  detail opens, diff arrows disable on notice panes.

- `gg web`: diff-pane navigation + stash untracked files. The diff header
  gains ‹/› arrows stepping between files and between change blocks within
  a diff. A stash's untracked files (stored in its `^3` parent, invisible
  to a first-parent diff — an untracked-only stash listed NO files) now
  appear in the stash drill-in, diffed as added content.

- `gg web`: stashes. A 4th sidebar section lists stash entries (left-click
  opens the stash's changes in the commit detail); right-click offers
  apply / pop / drop (drop confirms — the engine op is decision-free). A
  `stash` button beside commit stashes all working-tree changes incl.
  untracked, taking the message box's text as the optional stash message.
  Apply/pop/drop refs are resolved against the server's own stash list
  (allowlist); a pop/apply conflict surfaces in the status pane as usual.

- `gg web`: right-click context menus on all three sidebar sections. Branches
  gain **delete branch** (engine confirm + unmerged force fork in the modal;
  the tip is snapshotted to `refs/gg/versions` first, so it's recoverable via
  `gg versions`). Worktrees gain **copy path** and **remove worktree** (scope /
  locked / dirty forks in the modal; the served worktree's row is exempt).
  Tags gain **show commit**, **copy name**, and **delete tag** (client-side
  confirm — the engine op is decision-free). Destructive rows render red; the
  remove-worktree path is resolved against the server's own worktree list, so
  no client string reaches git argv.

- `gg web`: push. A `⇫ push` header button (`P`) pushes the current branch to
  origin (`engine.Push`, set-upstream). The engine's full rejection recovery
  rides the existing modal: a non-fast-forward rejection offers
  rebase / force / abort, and force chains a force-with-lease-first confirm.
  Force is never settable from the wire; detached HEAD is refused up front.

- `gg web` fails fast at startup (before binding a port or opening the
  browser) when the served directory isn't a usable repository — including
  a friendly hint for the WSL↔Windows case where a linked worktree's .git
  file points at the other environment's path notation. Previously the
  server started and every request 500ed with a raw rev-parse error.

- `gg web`: SmartPull — a `⟳ pull` header button (`p`) runs the hero op on
  the transport; a diverged branch parks the first live decision modal
  (rebase / merge / reset / abort). Conflicted pulls surface in the status
  pane.

- `gg web`: a `← back` button atop the detail screen (mouse alternative to
  esc), and sidebar section headers (branches/worktrees/tags) collapse on
  double-click — long lists no longer force constant sidebar scrolling.

- `gg web`: clicking a branch no longer switches — a left-click jumps the
  commit list to the branch tip (the TUI's enter-on-branch behavior) and
  mutations moved behind a right-click context menu (go to tip / switch).
  A stray click can no longer start an operation.

### Added
- `gg web`: commit from the working-tree screen — a message box + commit
  button (Ctrl+Enter) on the status pane, wired as the op transport's second
  operation (`op:"commit"` → `engine.Commit`; empty message → 400). Typing
  in form fields no longer triggers the j/k/s/u keyboard shortcuts.
- `gg web`: the sidebar grows worktrees and tags sections (read-only; tags
  capped at 100 with a truncation marker). Clicking a tag opens that
  commit's detail screen (`GET /api/worktrees`, `GET /api/tags`).
- `gg web` gains the op transport — the streaming spine for web write
  operations: `POST /api/op` starts an engine op (`switch` →
  `engine.SmartSwitch` first), `GET /api/op/{id}/events` streams its
  progress/decision/done events over SSE (stdlib, no WebSocket dep), and
  `POST /api/op/{id}/decide` answers forks parked on a channel-based web
  Decider (5-min timeout so an abandoned modal can't wedge the repo gate).
  The SPA adds a branches sidebar (click to switch, `b` toggles), a live op
  status line, and the decision modal (esc = abort, the TUI rule). A
  successful op resets the server's commit-feed cache so `/api/commits`
  reflects the new HEAD.
- `gg web` (probe) gains its first write pathway: a "● Working tree" row atop
  the commit list opens a sectioned status pane (Staged / Changes / Untracked /
  Conflicts) with whole-file stage/unstage (per-row `s`/`u` + bulk buttons) via
  `POST /api/stage` → `engine.Stage`, working-tree diffs (`/api/diff?wt=`,
  uncached), and `GET /api/status`. Mutating routes sit behind a CSRF write
  guard (JSON content type required, loopback-Origin check) on top of the
  existing Host guard.
- `gg web` (probe): read-only browser UI — a loopback-only embedded server
  serving commits + lane graph (text and SVG modes), commit files, and
  side-by-side diffs straight from the domain read-model; `--open` launches
  the system browser. Evaluation probe — may be removed after review.
- `gg --record <file>` — record a TUI session's keystrokes to a file in the
  `tui-capture.sh` keyscript format (one step per key, terminating quit
  excluded, a `#` header naming the repo), so a human can author a scenario
  for headless replay + screen capture.
- **Session-errors viewer sizes itself to its content.** Settings →
  "Session errors" already rendered wider than a standard popup, but long
  git stderr rows still truncated until a manual `ctrl+t`. Entering the
  viewer now opens it maximized automatically when its widest row wouldn't
  fit even the wide box (the pair-op popup's auto-maximize precedent,
  decided once at entry so it never fights a later `ctrl+t`), and `esc`
  back to the menu restores the normal size — unless the maximize was the
  user's own earlier `ctrl+t`, which is left alone.
- **Branch versions (operations history): pre-operation snapshots as hidden
  refs; list/compare/restore/delete via Branches `.` menu, command palette,
  Settings, and `gg versions`.** Before any operation that rewrites,
  replaces, or deletes a branch's history, gg now records the branch's
  current tip as a hidden, gg-owned git ref
  (`refs/gg/versions/<branch>/<unix-ts>-<op>`) — a full-history snapshot at
  zero storage cost that also pins the old commits against `git gc`.
  Triggers: `SmartMerge` (the branch merged INTO), `SmartRebase`,
  `InteractiveRebase` (squash/move/drop, snapshotted only after the plan
  passes full validation), `Commit --amend` (a plain commit is not a
  trigger), `UndoLastCommit`, `Reset` (any reset moving the branch ref,
  incl. reset-to-remote-tip), `DeleteBranch` (so a deleted branch's history
  is recoverable), and
  `SmartPull`'s rebase/merge/reset-to-remote lanes (its fast-forward and
  background checkout-pull lanes are untouched). Always-on for every branch;
  best-effort by contract — a snapshot failure never blocks the real
  operation. Retention is time-based (default 90 days, configurable via the
  new `[versions]` config section: `disabled` to turn recording off,
  `max_age_days` to change the window, `-1` to keep forever), pruned lazily
  on the branch's next snapshot. Two new engine ops back restore/delete:
  `RestoreBranchVersion` (current branch → hard reset with a
  proceed/cancel fork on a dirty tree; a branch checked out in another
  worktree is refused by name; any other branch — including a deleted one —
  is moved via `update-ref`; the pre-restore tip is snapshotted first, so a
  restore is itself undoable) and `DeleteBranchVersion` (removes one
  snapshot ref, refusing anything outside `refs/gg/versions/`). TUI: the
  Branches panel's `.` menu gains "Previous versions…" (opens a popup of
  that branch's recorded snapshots: `enter` whole-tree-compares a version
  against the branch's current tip, `r` restores — reset in place or start
  a new branch at that version — `d` deletes the snapshot, `y` copies its
  sha); the command palette gains "Branch versions…", a branch picker over
  every branch with recorded versions (deleted branches marked) — the
  recovery path for a deleted branch's history; Settings (`,`) → "Operations
  history" edits the retention window and toggles recording, writing the
  active repo `.gg.toml`. CLI: `gg versions [<branch>]` lists a branch's
  recorded versions (default: current branch), and `gg versions restore
  [--discard] <branch> <id|latest>` restores one (`--discard` pre-answers
  the dirty-tree prompt). The commit feed's `%D` decorations now exclude
  `refs/gg/*` so a merge's old tip doesn't show a version ref as a
  decoration. `agentskill.Version` 52 → 53; `gg init --update` refreshes
  installed copies.
- **Kimi Code joins the known-agent roster.** `gg init` now detects Kimi
  Code (project `.kimi-code/`, global `~/.kimi-code/`) and installs the
  using-gg skill as `skills/using-gg/SKILL.md` under either — Kimi discovers
  the same directory-form, frontmatter SKILL.md as Claude Code. The
  external-tools catalog (`internal/exttool`, Settings → "External tools…")
  gains a Kimi entry probed as `kimi` on PATH: `commit_message` and `review`
  capture defaults built on `kimi -p` that return through `$GG_MESSAGE_FILE`
  (print-mode stdout is a work report, never the clean answer), plus a
  `conflict` command of the same `kimi -p` shape under terminal handover
  (kimi has no interactive pre-seeded-prompt flag; print mode approves the
  conflict edits itself). All templates verified against a live kimi 0.27.0
  (2026-07-20), including an end-to-end headless conflict resolution.
- **Copy a worktree's absolute path from the `.` menu.** The Worktrees
  panel's context menu gains "Copy absolute path" (the selected worktree's
  root), and the Branches panel's copy rows gain "Copy worktree absolute
  path" whenever the selected branch is checked out in a worktree —
  including the current one.
- **`gg init --to <path>` — install the agent skill anywhere, for agents gg
  doesn't know.** The hardcoded registry covers the common agents; `--to`
  is the fallback for everything else: point it at a shared instruction
  FILE and the skill lands as the same marker-delimited managed block the
  AGENTS.md-style targets get (idempotent, version-stamped, surrounding
  content untouched, file created if missing), or at a DIRECTORY and it
  writes a Claude-style `<dir>/using-gg/SKILL.md`. The target is recorded
  in a machine-local registry (`agent-targets.toml` beside `repos.toml`),
  shows up in the `gg init` listing as a `Custom` row with the usual
  new/outdated/up-to-date status, and is refreshed by `gg init --update`
  like any supported agent — so an unsupported agent's copy never silently
  goes stale when the skill version bumps. `agentskill.Version` → 52.
- **The "using-gg" agent skill now teaches agents to register THEMSELVES as
  gg tools.** A new "Registering yourself as a gg tool" section documents
  the `[[tools.command]]` block (schema, config locations, the global+repo
  concatenation rule, inert-on-invalid loading, the `'''` restriction, the
  first-run approval gate) and the per-category contracts an agent must
  honor: `commit_message` (read `$GG_CONTEXT_FILE`/`$GG_STAGED_DIFF`, answer
  via stdout or `$GG_MESSAGE_FILE` — non-empty file wins), `review` (the
  `<range>` token, `$GG_REVIEW_DIFF`, free-form markdown report), and
  `conflict` (terminal handover, `GG_*` env, per_file mergetool tokens).
  A hardening pass makes self-registration reliably land: the
  active-repo-config rule (the private per-repo file, when it exists,
  replaces `.gg.toml` entirely — write the global config by default), one
  complete worked example block per category, the `GG_TASK` env and
  capture-run cwd, and a headless verification path (`gg review --tool
  <name> HEAD`; the CLI run is its own consent — the TUI approval popup
  still guards `ctrl+g`/`t`). using-gg also gains an **"Interacting over
  MCP"** section (the server, one-repo-per-cwd rule, the tool roster, and
  when to prefer MCP over the CLI) — MCP is an agent-facing interaction
  channel, so it belongs in the interaction skill. A new PROJECT skill,
  **`defining-agentic-tasks`**, owns the other side of the boundary: what
  an agent is expected to DO per task category (the conflict /
  commit_message / review contracts, the catalog prompts that encode them,
  detection gating which agents the wizard offers, and the using-gg sync
  rule), referring to `adding-external-tools` for process-running
  mechanics without duplicating them; `adding-features` 7b's trigger
  widened to the whole agent-facing surface. `agentskill.Version` 48 → 51;
  run `gg init --update` to refresh installed copies.
- **Worktree switch guard + cross-environment repair.** Every TUI switch —
  the Worktrees panel, the "go to worktree" branch prompt, the repo
  switcher and "Open repo" popups, and the chained post-operation switches
  — now verifies the target directory is reachable *before* tearing down
  the current session, and refuses an unreachable target with a status
  message instead of the raw `chdir` crash that used to follow a moved or
  deleted worktree. The two worktree-targeting switches — entering a
  worktree from the Worktrees panel, and the "go to worktree" prompt when
  switching to a branch checked out elsewhere — go one step
  further for the one case that's actually fixable: a linked worktree
  created on the other side of a WSL↔Windows path-notation boundary
  (`/mnt/t/…` vs `T:\…` on the same shared disk) — its admin gitdir file and
  `.git` back-link still point at the other environment's notation, so it
  stats as unreachable here even though the files are right there. Both
  routes now offer a repair/cancel modal; choosing repair runs
  `git worktree repair` on the translated path and, on success, switches
  straight into the repaired worktree. New: the `git.WorktreeRepair` verb
  (`git worktree repair <path>`) and the `engine.RepairWorktree{Path}` op
  that runs it (default `TreeWrite`; the confirm prompt is TUI-side, not
  the op's).
- `gg mcp` — an MCP (Model Context Protocol) stdio server exposing gg's
  non-git value to AI agents: `gg_ui_state` (the live TUI session snapshot —
  focus, cursor selections, ◉-marked commits, marked files, open diff/compare
  view, open bookmark/shelf switcher, filters, conflict/running-op state),
  bookmark tools (`gg_bookmarks_list`/`gg_bookmark_get`/`gg_bookmark_read`),
  shelf tools (`gg_shelf_buckets`/`gg_shelf_list`/`gg_shelf_commit_files`/
  `gg_shelf_read`), compare tools (`gg_compare_trees`/`gg_compare_file`), and
  `gg_export` (copy a bookmark/shelf entry into a local directory). Stage 1 is
  the safe surface only — no repo mutation. Register with
  `claude mcp add gg -- gg mcp` from the repo directory.
- The TUI now publishes a per-repo session snapshot
  (`<state>/gg/sessions/<repo-key>/ui-state.json`, atomic write-on-change from
  the 1s heartbeat, removed on exit/repo-switch) that backs `gg_ui_state`.
- `gg mcp` stage 2 — the first mutating MCP tools, each gated by the MCP
  client's destructive-tool consent prompt: `gg_cherry_pick` re-applies a
  shelved or bookmarked commit onto the current branch (live cherry-pick while
  the commit exists; a shelved commit's stored patch replays atomically via
  `git am --3way` after a gc, or with `mode:"patch"`; `on_conflict:"abort"`
  rolls back, `"keep"` leaves the conflicts and reports the conflicted files),
  and `gg_write_to_worktree` writes a shelf file entry, a shelved-commit
  member, or a file bookmark into the working tree as an unstaged change
  (destination defaults to the entry's own path; `overwrite` guard; identical
  content is a reported no-op). All 13 tools now carry read-only/destructive
  annotations.
- External-tools catalog: OpenAI Codex and Antigravity (`agy`) entries for
  conflict resolution, commit-message generation, and review — both shapes
  verified against the real binaries (codex-cli 0.144.6, agy 1.1.4). Codex
  captures via its native `--output-last-message` file channel;
  Antigravity delivers through `$GG_MESSAGE_FILE`. Antigravity's
  commit/review rows are **opt-in** (unchecked in the wizard): headless
  `agy -p` auto-denies every permission-gated tool, so those templates must
  carry `--dangerously-skip-permissions`. Consequently the catalog's OptIn
  rule generalized from "(yolo)"-named rows to "the command carries a
  permission-bypass flag". The wizard never overwrites existing
  `[[tools.command]]` blocks — already-configured categories pick up the
  new tools by checking their new rows.
- Agent-skills picker: detects Antigravity (`~/.gemini/antigravity-cli` —
  the agy home, not a bare `~/.gemini` left by gemini-cli) and installs the
  using-gg skill at `~/.gemini/config/skills/using-gg/SKILL.md`, agy's
  documented global customization root.
- External-tool detection now finds Kimi Code's standard install
  (`~/.kimi-code/bin/kimi`) even when the shell PATH export is missing
  (`ExtraProbes` gained `~/` expansion).

### Fixed
- **ssh host-key failures now explain themselves.** gg runs ssh in
  BatchMode (a prompt must never freeze the TUI), so first contact with a
  host missing from `~/.ssh/known_hosts` failed with the raw `Host key
  verification failed.` plus git's misleading "check your access rights"
  tail. The TUI status line now says ssh doesn't trust the host yet and
  points at the `ctrl+o` shell escape to run the push/pull once
  interactively and accept the key. A changed key (possible MITM —
  ssh's "REMOTE HOST IDENTIFICATION HAS CHANGED" warning) gets its own
  message that deliberately does *not* advise accepting; it says to verify
  with the host, then update known_hosts. Both are translated (ja/ko/zh/ru);
  CLI output stays raw English by design.
- **Running gg from a deleted directory now explains itself.** A shell
  sitting in a directory that was deleted (or deleted and recreated — the
  shell keeps the old inode) used to surface git's raw `fatal: Unable to
  read current working directory: No such file or directory`. gg now checks
  its working directory up front, before any repo-touching command or the
  TUI, and prints a friendly two-line message advising `cd "$PWD"` to
  re-enter the path (exit 1); `friendlyGitError` also recognizes the git
  message as a backstop. `gg version` and `gg shell-init` still work from a
  dead cwd.
- **File-stepping through a comparison no longer shows the wrong file's
  diff.** In a compare files view (marked commits, branch compare, full-tree
  mode, or the finder's HEAD ↔ working tree diff), stepping between files
  with N/P/Home/End could leave the diff view showing a *different* file's
  content under the new file's title — stepping then looked stuck ("no
  next/previous file" cues while the tree clearly had more, n/p walking the
  wrong change blocks). The compare loader mutated the live diff view from
  its async closure with no staleness gate, so a superseded load landing
  late (easy: already-viewed commit↔commit neighbors answer instantly from
  the diff cache while the abandoned load is still reading) overwrote the
  file the view had since stepped to. It now fills a private view applied
  only through the tag-gated result path, like every other diff loader.
- **Deleting a remote branch no longer triggers a needless network probe.**
  `DeleteRemoteBranch` was unmapped in the post-op refresh table, so it fell
  through to "reload all sources" — and the tags reload auto-fired the
  remote-tags `ls-remote` lookup (the ▲ pushed-state indicator) after every
  delete. It now refreshes only what changed: the Branches and Remotes
  panels and the commit feed's decorations.

### Changed
- **Default worktree branch template is now
  `<parent-branch>-<date:yyyy-MM-dd_HH-mm>`** (e.g. `main-2026-07-22_22-11`)
  instead of the day-one `b/from-<parent-branch>-<random-alpha:4>`
  placeholder. After the w/W popup fold below, the template only drives the
  CLI's `gg worktree add` no-argument lane; a minute-resolution timestamp
  names the branch after when it was cut rather than four random letters.
  Date tokens are lowercase `yyyy`/`MM`/`dd`/`HH`/`mm`/`ss`.
- **One worktree popup for `w` and `W` — the branch starts as the selected
  branch, and `W` switches to the new worktree.** `W` used to open the popup
  in new-templated-branch mode, so its directory carried the default branch
  template's `b/from-` prefix and random 4-letter suffix
  (`b/from-<parent-branch>-<random-alpha:4>`; the path template mirrored the
  templated name). Now both keys open the same popup on the selected branch
  (clean `<repo>.worktrees/<branch>` directory); `W` makes enter default to
  create & switch (`[w]` still creates without switching). The branch
  template is no longer applied at all: `e` edits the name (seeded with the
  selection — confirming a different name creates a NEW branch cut from it)
  and `p` seeds it from a saved branch prefix, so the new-branch flow lives
  inside the same popup instead of a separate mode.
- **Pull/push dialogs name the branch they act on.** The `p` slow-op confirm
  now reads "Pull main? This may rewrite the working tree." (or "Pull
  feat/x (stay here)?" for a Branches-panel background pull) instead of the
  branch-less "Pull?", and the `P` unpushed-tip-tags modal now leads with
  the branch being pushed ("Push main: branch tip has tag v1.2 not on the
  remote. Push too?"). A detached HEAD falls back to the old branch-less
  pull wording. New keys translated in all four bundles.
- **Multilanguage TUI (stage 5): operation status, progress, and prompts,
  localized.** The last English-only surface inside the TUI — the busy line
  while an operation runs, the after-op status summary, and the decision-
  modal prompt sentence — now renders in the active language (~200 new keys
  ×4 bundles: ja/ko/zh/ru, ~1,180 → ~1,385 keys each). The mechanism is a
  **dual-channel engine contract**: every operation still emits its English
  `Result.Summary` / `Progress.Detail` / `DecisionRequest.Prompt` byte-for-
  byte unchanged (so the CLI, `operations.log`, e2e scenarios, and agents
  are entirely unaffected), and additionally carries the unformatted
  `Msg{Format, Args}` pair — built only through the `WithSummary` /
  `AppendSummary` / `Progressf` / `PromptReq` helpers so the two channels
  cannot drift. A single TUI render seam (`internal/tui/i18n_engine.go`)
  translates the localizable channel and falls back to the English string
  when it is absent, so any un-migrated site degrades to English rather than
  breaking. Two AST gates enforce it: `engine_prose_test.go` requires every
  engine format/step literal to exist in all four bundles and forbids
  hand-built `Summary:`/`Prompt:` strings (helpers only), and the
  options-vocab gate learned the `PromptReq` pass-through. Engine error
  prose stays English (it renders inside the already-translated
  `friendlyOpError` frame); CLI/agent output stays English by design.
- **Multilanguage TUI (stage 4): the last remaining chrome, translated.**
  Closes the "declared stage-4 remainder" list from stage 3. Pair-op picker
  labels and footer (mark.go's Merge/Rebase/Interactive rebase/Compare
  closures, restructured from string concatenation to `i18n.T` format keys)
  are translated, plus browse/switcher chrome across the fuzzy file finder,
  files view, bookmark and shelf popups, the command palette, and the repo
  popup. The conflict process/picker and review-tool chooser are translated,
  alongside the prefix picker/settings, tool-approval, shell-escape,
  checkout-as, and commit eager-search popups. All 18 textfield-style popups
  (annotate tag, apply patch, commit filter, `ctrl+g` generate, export patch,
  goto-commit, interactive-rebase edit, reflog checkout, related-prompt,
  rename branch, repo path, reword, shelf actions, stash action/popup, tag
  checkout/popup, temp export) are translated too. Roughly 100 action-menu
  row labels across the package are wrapped, including commit scope, tags,
  remotes, and the rebase commit-move/drop rows. A new AST gate,
  `internal/tui/menu_labels_test.go` (`TestActionMenuLabelsTranslated`), sits
  alongside `options_vocab_test.go`: it walks every `actionRow` composite
  literal's `label:` field AND every call-site argument at a same-package
  function or method's `label string` parameter position (the positional
  blind spot a helper like `commitEditRow` can hide a raw literal behind),
  requiring every reached literal to route through `i18n.T` and every found
  key to exist in all four bundles. Running it for the first time caught two
  genuine untranslated panel titles outside any wave's file list
  (`stash_view.go`'s "Stashes" and `view.go`'s "Commits (%s)"), fixed in the
  same pass. Three related fixes round this out: notices are now
  rebuilt on a language switch instead of keeping titles baked in at
  construction time; the review lane's "working changes" status-bar argument
  is translated; and label-column pad widths (git-config explorer, identity
  & profiles, repo-config location) are now computed from the *translated*
  label set via a new `maxLabelWidth` helper in `i18n_display.go`, next to
  `padCell`, instead of a fixed English-length floor. Closed with four
  per-language QA passes (ja 8 fixes / ko 18 / zh 20 / ru 21 — terminology
  drift, genitive-count and particle/case fixes, and field-tag width
  alignment). With this wave, **the TUI is fully translated**; only
  engine/CLI prose stays English by design (the agent-facing, script-stable
  surface). ~405 new keys land in all four bundles, which now run just under
  1,200 lines each.
- **`/` search starts from the cursor, not from the top.** Engaging the `/`
  filter (in any panel, and on the files view's commit-list side) used to
  reset the cursor to row 0, and every typed character reset it again — with
  several pages of commits loaded and the cursor mid-list, each search
  restarted from the very beginning. Now the cursor stays put when `/` opens,
  each query edit re-seats it on the nearest match at or after it (wrapping
  to the top only when every match is above — the same rule the `@` highlight
  snap already followed), and leaving the filter (`esc`, `ctrl+r`, or
  switching to `@`) keeps the cursor on the same row in the full list instead
  of teleporting it to an unrelated display position.
- **Multilanguage TUI (stage 3): decision options, six popups, and the
  statusMsg tail.** Decision-modal option LABELS now translate at the
  single render site (`optionDisplayName` in the new
  `internal/tui/i18n_display.go`, which also now hosts `padCell`) — option
  VALUES (`Options` lists, decider/`onResolve` comparisons, the esc→`abort`
  mapping) stay English protocol, only the rendered label changes, and the
  modal footer is translated too. A new `internal/tui/options_vocab_test.go`
  AST scan enforces this going forward: every statically declared
  `Options: []string{…}` value across `engine`+`tui` must have an
  `optionDisplayName` case and exist in all four bundles (it caught a
  missing `"overwrite"` case on its first run). Six more popups are fully
  translated: identity & profiles (byte-width `%-9s`/`%-10s` column pads
  replaced by the shared, display-width-aware `padCell`), the git-config
  explorer (`configRowDecorator` rewritten to display-column math, with a
  new CJK regression test), the notification center, the repo-config
  location popup (`slotDisplay` gets the same `padCell` fix), the review
  viewer chrome (report *content* stays whatever the agent produced), and
  the worktree post-create hook editor. The `statusMsg` tail — roughly 110
  call sites across `model.go` and two dozen other files — is translated in
  three waves, including a handful missed by grep the first time round
  (`compareSelectionEndpoints` notices) and picked up in review. Red error
  styling survives all of this: a new `i18n.ActiveTranslations()` accessor
  lets `statusIsError` derive its error-prefix set from the active
  catalog's own translated, `%`-verb-bearing, error-prefixed keys (retroactive
  to stage-1/stage-2 keys too), guarded against a verb-less key sharing the
  same prefix (e.g. a footer label) and against a translation that reorders
  its topic word past the verb — either case just renders unstyled instead
  of mis-styled. Carried polish: `config.FileUILanguage` now delegates to
  the shared `decodeFile` helper, the Language-picker repo-override hint
  gained failure-path tests, and `toolConfiguredSuffixDecorator` moved to
  display-column math (`dimSpanRunes`) instead of byte slicing. Closed with
  a per-language QA delta pass across all four bundles (ja 6 fixes / ko 4 /
  zh 2 / ru 8 — terminology drift and punctuation-width cleanup) that also
  re-audited every error-prefixed key against the topic-head-leads-
  translation rule `statusIsError` depends on (zero violations found). With
  this, the TUI's decision modals, popups, and statusMsg tail are translated;
  engine/CLI prose stays English by design (the agent-facing surface), and a
  declared stage-4 remainder of chrome is still English: pair-op picker
  labels and footer, footer/hint strips in the fuzzy file finder, files view,
  bookmark/shelf popups, command palette, prefix picker/settings, tool
  approval, shell escape, checkout-as, the commit eager-search popup, and the
  conflict-process list box, plus the review tool chooser, conflict picker,
  and repo popup — roughly 220 new keys across all four bundles, which now
  run close to 790 lines each.
- **ctrl+f always digs deeper.** The Commits eager search (ctrl+f on a `/`
  filter or `@` highlight) used to stop for good once any match was in the
  loaded commits — pressing ctrl+f again jumped back to that match (or did
  nothing after a `/`-sourced jump cleared the filter) instead of loading
  more history. Now every ctrl+f press restarts the cycle past the
  already-loaded commits: it pages new batches, jumps to the next match when
  one appears, and re-asks with the "Search deeper?" prompt when the budgeted
  batches come up empty. The `/` filter now stays engaged through the search
  and the jump, exactly like the `@` highlight — the query no longer vanishes
  from the commit bar (only the Branches goto-tip fallback still clears a
  filter, since there the searched hash is unrelated to the filter text and a
  kept filter could hide the target). The query from the last eager search is
  also remembered, so ctrl+f keeps digging even after esc cleared the search
  (the repo switcher forgets it; a status line shows which query is searched).
  The default per-pass scan budget (`[ui] commit_search_max_pages`) is raised
  from 5 to 50 pages, so one pass covers ~15k commits before re-asking.
- **Multilanguage TUI (stage 2): fuller coverage + hardening.** The status
  line, the resume-paused-op prompt, the push-tip-tags prompt, every
  footer-override mode, and the conflict-process indicator are now
  translated, alongside the Settings sub-screens that were still English
  (operation log, commit-graph, external tools, agent skills, session
  errors, refresh-rates editor — ~40 more keys). Op/source names and
  decision values stay English (protocol); only display prose is localized,
  centralized in a new `internal/tui/i18n_display.go`
  (`opDisplayName`/`sourceDisplayName`/`describeConflict`/`conflictNotice`/
  `pausedNotice`). Multi-count messages (e.g. unpushed tip tags) now use a
  two-key singular/plural convention instead of baking a count into one
  string. `CheckVerbs` — the load-time guard that a translation's `%`-verbs
  match its English key — is stricter: a `*` width/precision now counts as
  its own consumed argument, and an explicit `%[n]` index is checked against
  the key's argument count; this also caught a help string ("a quarter of
  the viewport", previously worded as "25%" and misread as a `%o` verb,
  making it untranslatable). `SetLanguage` now surfaces an
  existing-but-unreadable custom bundle file (e.g. permission-denied) as a
  real error instead of silently falling back to English with no clue why —
  a genuinely missing file is unaffected. The Language picker shows a dim
  hint when the active repo config sets `[ui] language` itself, since that
  overrides the global choice you're about to pick. Rounded out with a
  per-language bundle review across all four bundles (terminology
  unification — e.g. distinct Chinese verbs for "fetch" vs "pull" —
  punctuation width, and particle/case grammar fixes) and lipgloss-width-aware
  column padding in the refresh-rates editor (was byte-width, misaligning
  wide-character translations).

### Added
- **Multilanguage TUI (stage 1).** New `[ui] language` setting: the TUI
  renders in Japanese (`ja`), Korean (`ko`), Chinese (`zh`), Russian (`ru`),
  or English (default). Pick it from Settings (`,`) → **Language** (persists
  to the global config) or set `[ui] language` directly. Custom languages:
  drop a `<code>.toml` into `$XDG_CONFIG_HOME/gg/lang/` — a new code adds a
  language, reusing a built-in code overlays it per-key (fix just the
  strings you disagree with); missing strings fall back to English. Covered
  so far: the footer, help, Settings, command palette, `.` menus, confirm
  prompts, the commit/create-branch/create-worktree popups, and status
  hints — CLI output, git output, and engine messages stay English by
  design (the agent-facing, script-stable surface). Fail-soft throughout: an
  unknown language code or a malformed bundle keeps English and shows a
  one-line notice, never a startup error.

### Changed
- **`gg version` now reports the real version for `go install` builds.**
  When the `-ldflags` values from `build.sh` are absent, `internal/buildinfo`
  falls back to Go's embedded build info (`runtime/debug.ReadBuildInfo`): a
  `go install github.com/homeend/gigagit/cmd/gg@latest` binary prints its
  module version (e.g. `gg v0.1.16 (none) windows/amd64`) instead of
  `gg dev (none)`, and a plain `go build` from a checkout prints the commit
  (`vcs.revision`, `-dirty` when the tree is modified). Explicit `-ldflags`
  values still win.
- **Shelf restore (`G` switcher, `p`) prefills the destination** with the
  entry's original path — enter puts the copy straight back in place (the
  existing overwrite/cancel confirm still guards a clobber); edit the path to
  restore elsewhere, and `ctrl+r` re-fills the original path after an edit.
  Previously the field started empty and the origin was shown only as a hint.

### Added
- `gg shelf cherry-pick [--patch] [--on-conflict=keep|abort] <entry-id>` — re-apply a
  shelved commit from the command line: a live `git cherry-pick` while the commit
  exists, or an atomic replay of the shelve-time patch snapshot (`git am --3way`)
  once it's gc'd; `--patch` forces the replay lane. Exit codes per `gg apply`
  (0 applied — or a clean `--on-conflict=abort` — 1 failure/conflicts, 2 usage);
  works under `gg batch`. The CLI twin
  of the `a` key in the TUI's `g`/`G` switchers.
- TUI hardening: async probe results (cherry-pick commit probe, pre-push tag check)
  no longer clobber an open dialog — they drop with a visible status notice; the
  `g`/`G` switchers invalidate an in-flight cherry-pick probe on every close path,
  not just esc.
- **Copy absolute file path** — every "Copy file path" surface now also offers
  copying the file's absolute filesystem path: the `.` action menu (Files,
  Staged, files view, history/blame/diff), the fuzzy file finder (`Copy
  absolute path`), and the `y` copy chooser in the `g`/`G` bookmark & shelf
  switchers. In the switchers the absolute path is anchored on the entry's own
  origin worktree. The existing repo-relative "Copy file path" is unchanged.
- Shell escape: `ctrl+o` anywhere in the TUI (even over a failed conflict
  resolve) suspends gg into an interactive `$SHELL` in the worktree — run
  whatever git needs (`git cherry-pick --skip`, …), `exit` returns to gg
  with a full reload. The `ctrl+p` palette gains **Open shell** and **Run
  shell command…** (one-off command with a press-enter-to-return pause and
  `alt+↓` history recall).
- Cherry-pick a bookmarked or shelved commit: `a` in the `g`/`G` switchers
  applies the highlighted commit entry onto the current branch (confirm
  modal). While the commit exists it is a true `git cherry-pick`; a shelved
  commit whose object was gc'd is re-applied from a patch snapshot
  (`git format-patch` mailbox) now stored alongside the tar at shelve time
  (`git am --3way`, atomic). A bookmark or a pre-patch/merge shelf entry
  whose commit is gone gets a clear notice instead of a git error.
- TUI: the footer no longer hard-truncates on narrow terminals — whole
  shortcut labels are dropped from the end, the line ends with a protected
  `… [?] help` tail, and the `?` help window lists the dropped keys in a
  leading "More keys (not shown in the footer)" section.
- **Branch-prefix editing niceties (Settings → Branch prefixes).** An invalid
  prefix no longer closes the add form and buries the error in the status
  bar: the form stays open with the typed value intact and the error shown
  inline, ready to fix. `ctrl+d` in the form opens a cheat sheet of every
  template token and the `<date:FMT>` format letters, with live examples.
  And a bare `<date>` token (no format) now defaults to `yyyy-MM-dd`
  everywhere templates resolve — prefixes and the worktree `path_template`.
- **Browse a shelved commit's files (`G` switcher, `enter`).** Enter on a
  shelved-commit entry now opens the files view populated with every file
  frozen in the entry (the commit's added/modified files at shelve time;
  deletions carry no content and aren't stored). Each row works like any
  files-view row: `enter` diffs the frozen version against the working tree,
  and the `.` menu offers **View file** / **Open in external editor** (both
  read the frozen bytes, durable even after `git gc`) and **Copy to working
  dir** — the restore path: it writes that one file back to its own
  repo-relative path as an unstaged change (overwrite-confirmed), so
  cherry-picking files out of a shelved commit is browse → diff → copy.
  Previously enter on a commit entry was refused with a notice. Under the
  hood, a shelf file-reference is now member-aware: a shelved commit's ref
  with a path resolves to that one file's bytes from the stored tar, which
  also makes **Compare against working dir / bookmark / shelf** and
  **Bookmark this file** work on the members.
- **Copy a file's path or name from the `g`/`G` switchers**: pressing `y` on a
  single-file bookmark or shelf entry opens a small chooser — *Copy file path*
  (the repo-relative path) / *Copy file name* (just the basename) / *Cancel* —
  and writes the pick to the system clipboard, matching the Files-panel
  `.`-menu copy rows. On a shelved commit or a commit bookmark `y` shows the
  usual "not available" notice instead.
- **Compare branches (Branches panel)**: mark a branch with `m`, `m` on a second —
  the pair-op picker now offers *Compare A ↔ B*: the full tip-to-tip diff in the
  compare files view (full branch names in the title). `f` cycles an origin
  filter — all differences / only files A changed / only files B changed —
  computed from the merge base (`no common ancestor` disables it). TUI-only;
  `gg compare A B` already covers the CLI.
- **Import/apply a patch.** `gg apply [--am | --working] <path>` and a TUI
  command-palette **"Apply patch…"** entry (`ctrl+p`) import a patch file —
  the inverse of `gg commit export-patch` / the `.`-menu "Export commit as
  patch", and round-trips them. Working-tree mode (the default) lands the
  diff as *unstaged* changes for you to review/stage/commit; a hunk that
  doesn't apply cleanly falls back to a 3-way merge, leaving standard
  conflict markers + unmerged entries in the tree for the existing conflict
  process (`x`) to resolve — no new machinery. Recreate-commits mode
  (`--am`) replays a `git format-patch` mailbox as real commits, preserving
  author/date/message, and is atomic: any failure rolls back completely
  (`git am --abort`), nothing half-applied; it refuses a plain diff
  (`--am` needs a mailbox, `ErrNotMailbox`). In the TUI, a mailbox patch
  forks a working-tree/recreate-commits choice; the CLI always takes an
  explicit mode and never forks. `gg apply` exits 0 on a clean apply, 1 on
  failure or applied-with-conflicts (conflicts left in the tree, the
  `gg merge --on-conflict=keep` convention), 2 on a usage error.
- **Command palette (`ctrl+p`) gains six commands.** File history and File
  blame prompt for a path (typed relative, absolute, or `./`-prefixed —
  normalized to repo-relative) and open the existing history/blame view;
  Find launches the fuzzy file finder; Open repo prompts for a path
  (`~` expands to home) and switches to it after validating it's a real repo,
  with an inline error on failure. Git config explorer and Set up agent
  skills (using-gg) both move out of the Settings (`,`) menu into the
  palette — the agent-skills picker still opens through Settings internally
  (so its screens are unchanged) but now returns straight to the palette on
  `esc` instead of the Settings menu.
- **Review a marked commit range (AI).** With two (or more) commits ◉-marked
  in the Commits panel, the `.` menu offers **"Review marked range (AI)"** —
  it reviews exactly the same changes **"Compare selection"** would show
  (`older..newer` for two marks, `oldest^..newest` for three or more), so
  marking-then-reviewing and marking-then-comparing scope identically. A
  working-tree / staged mark in the selection hides the row (a review needs a
  commit-to-commit range). Routes through the same review lane as the other
  `.`-menu review entries.
- **Human-friendly review labels + date-foldered reports.** The blinking
  status indicator, the report-viewer title, and the report filename now show
  a readable label — a **branch name** for a branch review, the **commit
  title** (`<short-sha> <subject>`) for a commit review, the **range** you
  typed for `gg review <A..B>`, or **"working changes"** — instead of the raw
  hex SHA range. (The SHA range is still what's fed to the tool's `<range>`
  token; only the *display* changed. The branch-name → hex-SHA resolution that
  closed the earlier `<range>` command-injection is unaffected — the name is
  display-only and never executed.) Reports are now filed under a per-day
  folder: `<state>/gg/reviews/<repo-key>/<YYYY-MM-DD>/<HH-MM>-<label>.md`,
  keeping the archive browsable.
- **External tools (stage 3: AI review).** Three `.`-menu entries run a
  configured `review` agent headless and open its report in a new full-screen
  viewer: Commits panel **"Review this commit"** (the focused commit's own
  change, `<sha>^..<sha>`; a root commit reviews just itself), Branches panel
  **"Review branch `<name>`"** (range `<base>..<tip>`, base = merge-base with
  `main`, falling back to the branch's `@{upstream}`, then the tip's own
  change when neither exists), and Files panel **"Review working changes"**.
  Runs share the stage-2 capture-lane machinery: a numbered chooser when more
  than one `review` command is configured, first-run approval of the resolved
  command (remembered per repo), and an animated spinner while the agent
  works; a failed or empty run surfaces the error in the status line instead
  of opening an empty viewer. The report viewer is read-only and scrollable
  (`↑↓`/`pgup`/`pgdn`/`home`/`end`), supports `/` search, **`e`** opens the
  same report file in `$EDITOR`, and `esc` closes. Every report is also
  written durably to `<state>/gg/reviews/<repo-key>/<YYYY-MM-DD>/<HH-MM>-<label>.md`
  so it survives past the session — reports accumulate; there's no
  history-browser UI yet, but the files are on disk and reopenable. A new
  scriptable **`gg review [--tool <name>] [--working] [<rev>|<A..B>]`** CLI
  verb runs the same pipeline non-interactively: it prints the report to
  stdout and persists it to the same path. Flags must precede the positional
  (like `gg log -n`); a single-commit positional reviews that commit's own
  change, an `A..B` positional reviews the range, `--working` reviews
  uncommitted changes, and no positional reviews the current branch's work
  (the same base-resolution as the Branches-panel entry). Exits 0 on a
  produced report, 1 on tool failure/empty report/no configured review tool,
  2 on a usage error; drivable under `gg batch`. Catalog defaults ship for
  Claude Code (`/code-review <range>`, verified headless under `-p`) and
  Junie — a task-agent whose report comes back through the
  `$GG_MESSAGE_FILE` channel introduced in stage 2, since its own `--review`
  flag can't take a range and is instead pointed at the diff via a new
  `$GG_REVIEW_DIFF` file.
- **"Install a clipboard tool" notice.** The notification center (`!`) now
  warns when a local X11/Wayland session is present but no clipboard helper is
  installed — the case where copy actions fall back to an OSC 52 terminal
  escape that many terminals (and tmux without extra config) don't honour, so
  copies silently do nothing. The notice names the exact package to install
  (`xclip` for X11, `wl-clipboard` for Wayland) with per-distro commands, and
  self-clears on the next load once a tool is present. A headless/SSH session
  (where OSC 52 is the expected path) never triggers it.

### Fixed
- **Deleting a remote branch no longer flashes a `git log … unknown revision`
  error.** `git push --delete` removes the local remote-tracking ref
  immediately, but the Commits feed's next re-walk still listed that ref in
  its applied scope (the remote-branches list refreshes concurrently, so the
  `feedUpstreams` filter ran against a stale snapshot) — `git log` failed
  with exit 128 and the error hit the status line and `errors.log`, even
  though the delete itself succeeded. The feed walk now passes
  `--ignore-missing`, so a scope ref that vanished between scope application
  and the walk is skipped instead of failing; the same protects any ref
  deleted externally (e.g. `fetch --prune`) mid-session.
- **A shelved commit no longer errors in the shelf switcher's per-file actions.**
  Pressing `enter` (diff vs working tree) on a `G`-switcher entry created by
  "Shelf this commit" tried to read the entry's empty origin path as a file —
  the diff opened onto `error: read <repo>: is a directory`. The per-file keys
  (`enter` diff, `p` restore, `e` editor, `m` mark/compare, `c` vs bookmark)
  now explain they don't apply to a shelved commit (a frozen tar of the
  commit's changed files) and point at `[t]`, which copies it to a temp dir;
  `[x]` remove still works. Mirrors the commit-bookmark guard in the `g`
  switcher.
- **The branch pair-op popup opens full-size when a branch name is too long to
  fit.** Marking one branch and picking merge/rebase/interactive-rebase against
  another opens a picker whose rows spell out *both* branch names
  (`Merge <a> into <b>`) — the essential content. It was capped at the default
  popup width, so long names truncated to `Merge fix/… into feat/…`. The popup
  now opens maximized whenever its rows would be clipped at the default width,
  showing the full names; `ctrl+t` still toggles it and `z` still cycles
  wrap/scroll. (Groundwork — `autoMaxForContent` — for extending the same
  "open full-size when content is clipped" default to other maximizable popups.)
- **Decision/confirm popups no longer clip long text.** Every confirm and
  decision dialog (merge/rebase/pull/switch/checkout confirms, the SmartPull
  worktree fork, delete-branch/tag prompts, "export path exists", the
  post-create-hook approval, …) renders through one modal. It was the only
  popup with no width bound, so a long branch name — the essential thing you
  need to read to know what you're confirming — made the box wider than the
  terminal and the terminal clipped the edges. The modal now wraps its prompt
  and options to the terminal width (hard-wrapping a single unbreakable token so
  even a space-free name stays visible); short dialogs are unchanged. Reported
  when merging a branch with a very long, multi-author name into `main`.
- **Clipboard copy now works on a Wayland session inside tmux.** tmux does not
  propagate `WAYLAND_DISPLAY` into its environment, so gg skipped `wl-copy`
  (its `WAYLAND_DISPLAY`-gated Wayland tool) and fell back to an OSC 52 escape
  that didn't reach the clipboard. gg now recovers the display by probing
  `$XDG_RUNTIME_DIR` for the live `wayland-N` socket and runs `wl-copy` with it
  injected — mirroring why WSL detection reads the kernel osrelease rather than
  the tmux-stripped `$WSL_DISTRO_NAME`. (On an X11 session the fix is to install
  `xclip`/`xsel`; the new notice above surfaces that.)
- **`/`-filter navigation on the Commits panel is now O(1) per keypress at
  any feed size.** The filtered display index is memoized
  (`commitFilterMemo`) and rebuilt incrementally — typing narrows the cached
  matches, paging scans only the appended tail. Previously every keypress
  rescanned the whole feed ~15×: ~5.6s per arrow key at 600k commits (linux
  repo), now sub-millisecond.

### Added
- **External tools (stage 2: AI-generated commit messages).** `ctrl+g` in the
  commit popup (`c`/`C`) drafts a commit message from the staged diff using a
  configured `commit_message` agent, run **headless** (no terminal handover):
  the staged diff is written to two per-run context files — a labeled
  summary (`$GG_CONTEXT_FILE`: files changed, recent-commit style) and the
  full `git diff --cached` (`$GG_STAGED_DIFF`, truncated past a size cap with
  a note) — the agent's captured stdout is parsed into a subject + body pair
  (format-agnostic: plain text, Claude's `--output-format json` `.result`, or
  its `--json-schema` envelope), and the result fills the popup's editable
  title/description fields. Nothing commits automatically — review and
  `ctrl+s` as usual. The same gates as the conflict lane apply: a numbered
  chooser when more than one `commit_message` tool is configured, first-run
  approval of the resolved command (remembered per repo until the config
  text changes), and a confirm-replace prompt when the fields already hold
  text; `esc` cancels an in-flight run. Catalog defaults ship for Claude Code
  and Junie (`mode = "capture"`). A tool may return the message either on
  stdout (Claude's `--output-format json` `.result`) or by writing it to the
  file at `$GG_MESSAGE_FILE` — non-empty file content wins. That file channel
  is what makes Junie work: it is a task-agent whose stdout is only a
  `### Summary / ### Changes / ### Verification` work report, never the
  message, so it writes the commit message to `$GG_MESSAGE_FILE` and gg reads
  it back as a clean subject + body.
- **Fullscreen popups (`ctrl+t`).** `ctrl+t` now toggles ANY popup to a
  near-fullscreen bordered box — every switcher, picker, list, viewer, table,
  wizard, editor, and prompt — via one central handler on the popup layer
  stack. `ctrl+t` also fullscreens the focused panel. `esc` still closes a popup. It uses `ctrl+t` rather than bare
  `T` so it never collides with typing a capital T into a branch name, commit
  message, filter, or tag text. (`ctrl+shift+t` is intentionally not used —
  most terminals send the same control byte for both and can't distinguish
  them.)
- **External tools (stage 1: conflicts).** Run a configured agent or
  mergetool on a paused merge/rebase/cherry-pick/revert from the conflict
  window (`t`): repo-level agents (Claude Code, Junie) get a per-run temp
  context file (op/source/target header plus the conflicted paths,
  C-quoted against control-byte forgery) exposed as `<context-file>` and
  `GG_CONTEXT_FILE`, plus ten more `GG_*` env vars, and hand over the
  terminal; per-file tools (Meld) get the LOCAL/BASE/REMOTE/MERGED quartet
  and an after-run mark-resolved offer. Commands live in
  `[[tools.command]]` config blocks (global+repo lists concatenate, repo
  wins name collisions); catalog defaults are built from generation-time
  `<env:NAME>` tokens (rendered `${NAME}`/`%NAME%` per OS) and the context
  file/env channels only — no default template substitutes a raw prose
  value. Settings → "External tools" detects installed tools and writes
  editable defaults to the global config; the first run of each command
  shows it for approval (remembered per repo until the text changes). Each
  agent also ships an opt-in yolo variant — Claude via
  `--dangerously-skip-permissions`, Junie via `--brave` — shown unchecked
  in the wizard by default (first-run approval still applies).
- **Relocatable per-repo settings.** Per-repo settings can now live in a private
  machine-local file (`~/.config/gg/projects/<encoded-repo-path>/config.toml`)
  instead of the committed `.gg.toml`, so personal preferences on a shared repo
  are never committed. gg reads ONE active per-repo file — the private file when
  it exists, else the committed `.gg.toml` — layered over global
  (`defaults → global → active repo file`); per-repo Settings writes target the
  same active file. Settings → **"Repo settings location"** copies or moves the
  whole config between the two locations.
- **Smart prompt when checking out the current branch's remote.** `c`/`s` on
  the remote counterpart of the checked-out branch no longer dead-ends with
  "use pull to update it": a state-aware prompt offers "pull now" (only when
  the branch is actually behind its upstream), "check out as different
  name…" (the checkout-as popup with a free `-2/-3` suggestion), or cancel.
  The CLI's diverged `--as` hint now also fires for the current-branch
  refusal.
- **Resume a paused rebase/merge after external conflict resolution.** When a
  merge/rebase/cherry-pick/revert is paused and its conflicts were resolved
  outside gg, the next status refresh (`r`, background, watcher, or startup)
  shows a one-shot prompt — Continue / Abort / Not now — backed by the
  existing continue/abort ops. A persistent `⏸ <op> paused` status segment
  stays visible while the op is paused, and `x` now opens the conflict
  process even with zero conflicted files (straight into its continue/abort
  state). Detection is a stat-level probe (cached git dir → pure file
  stats), so a clean repo still pays zero extra git invocations.
- **Check out a remote branch under a different local name.** The Remotes
  `.`-menu gains "Check out <remote> as…" and "Switch to <remote> as…" (a name
  popup pre-filled with the branch name), and `gg checkout` gains `--as
  <local>`. When a same-name checkout refuses because the local branch has
  diverged, the TUI now offers "check out as different name…" with a free
  `-2/-3` suggestion instead of a dead-end error; the CLI prints a `--as` hint.
- **Branches panel: `enter` jumps to the branch tip, `ctrl+g` solos + jumps.**
  `enter` on a selected branch runs "Go to tip in commits": the Commits cursor
  lands on the branch's tip and the panel focuses. A tip that isn't in the
  loaded page no longer dead-ends — it falls back to the `ctrl+f` deep search
  (clears the `/` filter, pages history under the search budget, asks before
  scanning deeper, reports "not found in full history" on exhaustion); the
  `.`-menu row gained the same fallback. `ctrl+g` runs "Solo this branch"
  first (toggle semantics preserved — a second press un-solos) and finishes
  the tip jump once the scope reload lands. Both keys advertised in the
  footer (`[enter] tip`, `[ctrl+g] solo+tip`) and `?` help.
- **Space-mark & compare on the Commits panel.** `space` toggles the selected
  commit (or ◇ Working tree / ◇ Staged row) in the ◉ compare selection — the
  same set as `m`, capped at two marks — and the moment the second mark lands
  the two-commit comparison opens. With two already marked, space refuses with
  a hint. `esc` on the focused Commits panel clears ALL marks in one press
  (before it falls through to clearing highlight/filter). The `.` menu now
  reads "Unmark commit" / "Unmark all commits (N)" / "Unmark the marked
  commit", and re-opening a comparison already on screen keeps it instead of
  reloading.
- **`gg batch [--keep-going]`.** Runs a script of `gg` commands from stdin
  against ONE shared process (one repo discovery for the whole script) —
  much cheaper than spawning `gg` once per command when an agent is chaining
  several calls. One command per line; blank lines and `#` comments are
  skipped; a leading `gg ` token is tolerated; single/double quotes group
  words (`commit -m "two words"`) but there are no pipes, env vars, globs, or
  redirection — a batch script is not a shell. Each command's output is
  framed with a header naming its outcome, e.g.:
  ```
  #1 ok add new.txt
  #2 ok commit -m "batch commit"
  #3 !2 bogus
  ! unknown command "bogus"
  #done 2 ok, 1 failed
  ```
  (`#<idx> ok <cmdline>` or `#<idx> !<exit> <cmdline>`, stdout verbatim,
  stderr lines prefixed `! `). Batch stops at the first failure unless
  `--keep-going`; the `#done` trailer notes `(stopped)` when the stop
  skipped later lines (not when the failure was the last line).
  Sub-commands read an empty stdin, so anything needing a decision fails
  loud with its options instead of hanging — same non-interactive contract
  as a single `gg` run. Exit codes: 0 all ok, 1 any command failed, 2
  script/usage error (nothing framed). New internal seams: `runOne`
  (extracted from `cli.Run`'s dispatch, shared by both single-command and
  batch execution) and `tokenizeBatchLine` (the quote-aware line splitter).
  The e2e harness's `[[run]]` gained an optional `stdin` field (multi-line
  TOML string fed to the command's stdin; empty = prior no-stdin behavior)
  to exercise it end to end (`s79_cli_batch.toml`).
- **TUI: `T` fullscreen.** `T` fullscreens the focused panel (any left-column
  panel or Commits) to fill the whole terminal; `t` still maximizes to the
  left column only. `esc` or `T` restores the prior layout; `t` (while
  fullscreen) drops back to column-maximized. Tab switching while fullscreen
  transfers the pin to the newly shown tab. Deliberate jump-to-Commits actions
  (solo tag, go-to-tip, commits-touching-file) also transfer an active
  fullscreen pin to Commits instead of stranding focus. New predicates:
  `canFullMaximize()`/`fullMaxActive()`, new Model fields `fullMax`/`fullMaxed`.
- **Agent-facing CLI verbs: `gg log`, `gg diff`, `gg show`, `gg add` /
  `gg unstage`, `gg branch current` / `gg branch ls`, `gg worktree prune`.**
  A batch of terse, scriptable read/write commands aimed at AI agents driving
  `gg` non-interactively. `gg log [-n N] [<rev>|<A..B>]` prints one
  `<short-sha> <subject>` line per commit, newest first (default `-n 10`, rev
  defaults to `HEAD`; a range like `main..HEAD` passes through unchanged).
  `gg diff [--stat|--name-only] [--cached] [<rev>|<A..B>] [-- <paths>...]`
  diffs the working tree (default), the index (`--cached`), or a commit/range;
  plain invocation prints the full patch, `--stat` prints `path +A -D` lines
  plus an `N files +A -D` trailer (`path bin` for binaries), `--name-only`
  prints bare paths; paths must follow a `--` separator so they're never
  confused with a rev, and an empty diff prints nothing. `gg show <commit>
  [--patch] [-- <file>...]` prints a `<short-sha> <subject>` header followed
  by the terse stat block (default) or the full patch (`--patch`). `gg add
  (-A | <path>...)` / `gg unstage <path>...` stage (including untracked files
  via `-A`) or unstage paths — closing the long-standing hole where `gg`
  had no way to stage a brand-new file before `gg commit`. `gg branch
  current` prints just the branch name (HEAD's short sha when detached); `gg
  branch ls` lists local branches, `* ` marking HEAD and `↑a ↓b` when an
  upstream exists. `gg worktree prune` drops stale worktree administrative
  entries (`git worktree prune`). `gg commit`'s summary now names the commit
  it made: `✓ committed <short-sha> <subject>` (`amended ...` for
  `--amend`). New engine primitives `engine.Stage{All}` and
  `engine.PruneWorktrees`; new git verbs `LogLines`, `CommitLine`,
  `DiffNumstat`, `DiffPatch`, `ShowNumstat`, `ShowPatch`, `StageAll`,
  `PruneWorktrees`; new e2e scenarios `s77` (log/show) and `s78`
  (add→commit).
- **Git config explorer.** Settings (`,`) → "Git config explorer" opens a
  searchable, full-height view of every config key git knows (`git help -c`,
  ~870 keys) with columns key | local | global | default — unset scopes say
  `(unset)` explicitly. ~64 curated keys (`internal/gitconfdocs`) show git's
  real default plus a one-line description and edit in place: `l` sets local,
  `g` sets global, `u` unsets (choosing among set scopes); bools/enums get an
  option picker, strings/ints a text field. Writes run through the same
  `engine.SetGitConfig` op as the notification center (now with `Unset`);
  non-curated keys are read-only. `/` filters as you type, `z` cycles display
  modes.
- **Notification center.** On repo load gg runs cheap health checks; findings
  show as a blinking red `! N notice` status segment and a **`!`** dialog.
  First check: a big repo (packs ≥ 100 MB) with no commit-graph file and
  `fetch.writeCommitGraph` unset gets "Commit browsing can be ~10× faster in
  this repo" with one-keystroke fixes — *Write commit-graph now + keep it
  fresh* (runs `git commit-graph write --reachable`, then sets
  `fetch.writeCommitGraph=true` locally), *Enable auto-refresh only*, *Not
  now* (asks again next load), or *Never for this repo* (persisted in
  `<state>/gg/prompts.toml`). Settings gains a **"Commit-graph"** row showing
  the same state with the same one-key fix. New engine ops
  `WriteCommitGraph` + `SetGitConfig` back it (the generic config write that
  stage 3's explorer will reuse).
- **Related-option prompts.** Flipping a Settings option can now ask one
  follow-up about a related option: turning "Show graph" off offers to set
  Commit sort to `plain` (ordering only matters for graph lanes — plain is
  much faster on big repos); turning it back on offers `date-order` back.
  Options are Yes / Not now / **No — don't ask again**; the last is persisted
  machine-globally in `<state>/gg/prompts.toml` (named in the popup; remove
  an id from its array — or delete the file — to bring prompts back). The
  registry is generic (`internal/tui/related_prompts.go`); the new
  `internal/promptstate` store also carries per-repo notice dismissals for
  the upcoming notification center.
- **`[ui] show_graph` — persistent Commits render mode.** A per-repo setting
  (Settings `,` → "Show graph") choosing how the Commits panel renders on
  startup: `on` (default when unset; the lane graph) or `off` (the flat
  `●`-gutter list, exactly what the `.` menu's "Show as list" shows). The
  Settings toggle applies immediately and persists the choice to the repo's
  `.gg.toml`; any explicitly set value is remembered. A string key (not a
  bool) on purpose: the overlay's zero-is-unset rule would make a bool's
  `false` unwritable over a global `true`, so `"off"`/`"on"` keep the repo
  layer able to override in both directions. The `.` menu toggle stays
  session-only. New `config.SetShowGraph` writer (eighth runtime writer).
- **Export a commit — or a single file's change within a commit — as a git
  patch.** The Commits panel `.` menu offers **"Export commit as patch"**
  (hidden for merge commits); drilling into a commit's file and opening its
  diff, the diff view's `.` menu offers **"Export this file's diff as
  patch"** (only for a commit-vs-parent diff — not working-tree or compare
  diffs). Both open an editable single-line full-path popup pre-filled with
  `<parent-of-repo>/<shortsha>.patch` (file: `<shortsha>-<basename>.patch`);
  `enter` writes, `esc` cancels. The patch is `git format-patch -1 --binary
  --stdout` output — mailbox format, `git am`-able, carrying the commit's
  author/date/message (`--binary` keeps binary changes appliable). **Merge
  commits are refused**: `git format-patch -1` on a merge doesn't error, it
  silently emits a *different* commit's patch, so gg checks the parent count
  up front and blocks it instead. The default destination is the parent
  directory of the repo root (e.g. `/a/x/repo` → `/a/x`), anchored on the
  main worktree even from a linked one. Also scriptable from the CLI: `gg
  commit export-patch <sha> [--out <path>] [--force] [-- <file>]` — omit
  `-- <file>` for the whole commit, add it to scope to one file; `--out`
  overrides the default path; `--force` overwrites an existing target
  (otherwise it refuses, exit 2). New engine primitive: `engine.ExportFile`
  — the file-grained sibling of `WriteFile`, and the second op (after
  `ExportToDir`) that writes outside the working tree.
- **`gg version`** (also `gg --version` / `gg -v`) prints the build identifier
  — version, commit, and platform — from `internal/buildinfo` and exits. It is
  intercepted in `cmd/gg` before any repo is opened, so it works from anywhere,
  including outside a git repository.

### Fixed
- **Wrap mode (`z`) made cursor movement O(feed) per keystroke.** With the
  Commits panel in wrap display mode on a deeply paged feed (a few thousand
  commits loaded), every frame rebuilt, decorated, and ANSI-wrapped *every*
  loaded row just to place the window — ≈47ms per keystroke at 5k commits,
  ≈84ms at 10k, so movement crawled and key-repeat built a backlog. The
  "wrap needs every row for exact windowing" assumption was false: every row
  occupies at least one display line, so an h-line window can never show rows
  more than h away from the anchor, and slicing to `[anchor-h, anchor+h]`
  before wrapping is output-identical (pinned by an equivalence test against
  the full layout for every anchor). All four wrap fallbacks — `renderWindow`,
  `renderPanel`, `commitBody`, `panelViewWindowed`, plus the files view's
  copy of the gate — now window before building, making wrap-mode frames
  ~flat in feed size (≈1.8ms at 10k commits, same as cutoff).
- **`gg status` printed a blank status pair for untracked files instead of
  git's `??`.** `ParseStatusV2` left `Staged`/`Unstaged` zero-valued for the
  untracked branch of the porcelain-v2 parse, which `cmdStatus`'s
  '.'-and-zero-are-blank rule then rendered as two spaces; the untracked
  branch now sets `?`/`?` so the CLI output matches `git status`. (The
  ignored branch stays zero-valued: `gg` never runs `git status --ignored`,
  and setting `!` there would double-count in `model.Counts()`.)
- **Solo view painted the previous scope's lane graph.** Soloing a branch
  (Branches `.` → "Solo this branch") reloaded the commit rows but could keep
  the lanes laid for the earlier all-branches walk, drawing phantom forks
  through what is actually linear history. The incremental graph fast path
  (built for paging appends) keys on the first commit's hash and the row
  count, so a scope switch that keeps the same newest commit — soloing the
  checked-out branch whose own tip is the newest commit — looked like a no-op.
  Every path that *replaces* the commit rows (scope reload, feed re-read, full
  load) now discards the incremental lane fold and re-lays from scratch;
  paging keeps the O(new commits) append.
  - **A paste/restore destination could escape the working tree.**
    `WriteWorktreeFile` (backing `gg bookmark paste`, `gg shelf restore`, and
    the TUI paste text fields) resolved its destination with `filepath.Join`,
    whose `Clean` collapses `../` upward instead of rejecting it — a crafted
    destination wrote (or read, via `ReadWorktreeFile`) outside the repo. Both
    now reject any path that resolves outside the working tree; a `../` that
    still lands inside stays legal.
  - **Three domain reads ran outside the repo gate.** `Conflict` (hit on every
    TUI status refresh) probed `MERGE_HEAD`/rebase state files that a
    concurrent tree-writing op actively mutates, and
    `BookmarkAdd`/`BookmarkBytes` resolved blobs unguarded. All three now run
    under the same Read reservation (+ singleflight + failure seam) as every
    other domain read; a clean status still skips the gate entirely. The
    layering is now also enforced by a new archtest table
    (`TestLayeringDAG`) covering the whole package DAG, not just the
    frontend→git edges.
  - **A cancelled CLI operation stayed wedged on an interactive decision
    prompt.** `cliDecider` ignored its context while blocking on stdin,
    holding the repo-gate reservation until the user typed; the prompt read
    now unblocks on ctx cancellation (mirroring the TUI decider).
  - **The initial TUI session duplicated `domain`'s runner wiring inline in
    `cmd/gg`.** It now goes through `domain.OpenTUIWithRing`, so the
    LimitRunner/ssh-BatchMode stack is built in exactly one place for both
    startup and the repo switcher's reRoot.
- **After switching repos (`R`), per-repo Settings writes landed in the
  previous repo's `.gg.toml`.** The repo switch reloads through the legacy
  load path, which updated the in-memory config but never rebound the
  Settings write target (`repoConfigPath`) — only app startup set it. Every
  per-repo write after a switch ("Show graph", "Commit sort", "Refresh
  rates", file-watch toggles, the worktree post-create hook editor) therefore
  edited the repo you came FROM. The load result now carries the new repo's
  `.gg.toml` path and rebinds the target alongside the config.
- **Holding `End` (or `PgDn` / `j`) on the Commits panel kept loading pages
  long after the key was released.** Root cause (profiled at 100k loaded
  commits): every keystroke's frame cost O(feed) — `commitIdentWidth` ran a
  `lipgloss.Width` scan over all loaded commits per frame, `displayIndices`
  refilled an n-int identity index per call, and each landed page re-laid the
  whole commit graph. A frame took ~160 ms at 100k, ~5× slower than terminal
  key auto-repeat, so held keys piled up in the tty and the backlog kept
  pulling pages after release. Fixed by making the whole paging cycle
  O(page)/O(visible): the commit-graph lane fold is now incremental
  (`commitgraph.Layer` preserves open-lane state so paging appends rows
  instead of re-laying history), and the ident width + identity display-index
  are cached at the `rebuildCommitGraph` chokepoint (same pattern as the
  Files-panel `filesIdx` fix), invalidated on branches/scope changes. A frame
  at 100k commits now renders in ~1.9 ms (was 160 ms) and stays flat as the
  feed grows, so loading tracks the held key and stops on release. The load
  dispatch also gained a synchronous `commitsLoading` guard closing the
  back-to-back double-dispatch race (End/PgDn/`j`/`k`, `ctrl+l`, mouse wheel).
- **The full-text reveal (the yellow highlight) for a truncated selected row is
  now sized and anchored to the text instead of running edge-to-edge with stray
  blank padding (the "odd effect").** `revealLine` (`internal/tui/tooltip.go`) was
  rewritten to a clear geometry: the strip is the text plus a small blank margin
  on each side (never flush against the text it overflows onto), floored at the
  panel's inner width so a short reveal still covers the selected-row highlight.
  It anchors where the original text sits — a right-hand Commits reveal pins its
  right edge where the text ends and grows left; a left-panel/files-tree reveal
  pins its left edge where the text starts and grows right — and never exceeds the
  viewport. Only a subject wider than the whole viewport is clipped: it then fills
  the full width and is marked with `…` at the edge (no margin, since nothing shows
  underneath).
- **Pressing `l` or `enter` on the "Working tree" or "Staged" pseudo-commit
  row (Commits panel) opened the files view with a "compare: DiffTreeFiles:
  unsupported endpoint pair" error, and the view then wedged — only `esc`
  could close it (`up`/`down`/`j`/`k` were silent no-ops on the 1-line
  error popup).** `wipEndpoints` (`internal/tui/wip_rows.go`) built the
  compare-endpoint pair backwards: `DiffTreeFiles` only implements the four
  *older → newer* pairs (documented left = older, right = newer), but every
  WIP-row branch returned *newer, older*, which matches none of them and
  always errors. Fixed by swapping the return order in all three branches;
  the pre-existing `wip_rows_diff_test.go` assertions had encoded the same
  reversed order (introduced together with the WIP-row `l`/`enter` routing
  in the same commit) and are corrected alongside the fix.
- **Creating a tag (or anything else typed/pasted into a text field) could
  crash with a cryptic `fork/exec ...git.exe: invalid argument` on Windows.**
  Pasting into a Windows console can synthesize a stray NUL (U+0000) key
  event at the end of the paste burst; nothing filtered it out of the field,
  so it survived into the tag name/message and reached `git tag`'s argv
  unfiltered (`internal/git/mutate.go`). Go's Windows exec layer rejects any
  argv item containing a NUL with `syscall.EINVAL`, which surfaces as that
  opaque error — reproduced via copy-pasting a (correctly-copied, post the
  clip.exe fix below) tag name into the create-tag popup. `textfield.insert`
  now drops NUL runes at the field's own input boundary; no legitimate
  keystroke or paste ever needs one.
- **Copying a short value (tag name, branch name) on WSL no longer pastes as
  CJK mojibake.** `clip.exe` guesses whether stdin is already UTF-16 using a
  length-sensitive heuristic, and misdetects short pure-ASCII payloads like a
  git tag name (`v0.1.9`) as UTF-16, storing them verbatim — pasted back, each
  ASCII byte pair reads as one CJK-range UTF-16 code unit. A 40-char SHA
  carries enough signal to be detected correctly, which is why only short
  copies showed the bug. `internal/clipboard` now encodes stdin to UTF-16LE
  explicitly for `clip.exe`/`clip`, removing the ambiguity the heuristic acts
  on; every other native command (`pbcopy`, `wl-copy`, `xclip`, `xsel`)
  is unaffected.
- **Scrolling / switching panels no longer lags for seconds on a repo with a
  huge untracked set.** Every keystroke rebuilt the Files/Staged panel's row
  strings and membership indices from scratch — several times over, since
  `displayIndices` (and everything through it: `panelLen`, the labels, the mark
  overlays, the mouse hit-test) rescanned all of `status.Files` on each call. On
  a 40k-file tree a single arrow key did millions of allocations. Now the
  Files/Staged membership split is derived **once when the status is written**
  (`withStatus`) and returned in O(1); the render builds and elides only the
  visible window's rows; and the per-frame `[]winRow` buffer is sized to the
  visible window instead of the full row count (it embeds a `lipgloss.Style`, so
  a full-length allocation zeroed megabytes per frame). CPU during a fast scroll
  of the 40k-file panel dropped ~3× and the row work is now O(visible).
- **The whole UI no longer burns CPU (and eventually freezes) on a repo with a
  huge untracked or commit set.** `renderWindow` — the shared list/panel window
  primitive behind every panel and popup — used to build a display line for
  **every** row on **every** frame before discarding all but the visible ~dozen.
  Combined with gg's perpetual 1-second heartbeat (which re-renders the whole UI
  even when idle), a 40k-file/36k-commit repo sat pegged at ~20 % CPU with heavy
  GC churn and locked up under any extra event traffic. In the single-line
  display modes (cutoff/scroll) each row is exactly one line, so the window is
  now sliced to the visible rows **before** any per-row work — O(visible) instead
  of O(total). Idle CPU on the TypeScript repo dropped ~20 % → ~6 %. Wrap mode
  (multi-line rows) keeps the full pass.
- **The Files/Staged panels no longer freeze the UI on a repo with a huge
  untracked set** (e.g. a 40k-file `graphify-out/` directory). The left-panel
  render only builds and elides the rows the window will actually show
  (`[start,end)` around the selection) instead of middle-eliding **every** file
  on **every** frame. Because each long path hit an `O(len²)` width-eliding
  loop, a 40k-file panel cost ~1.5 s per frame — and Bubble Tea re-renders on
  every message (startup source fan-out, spinner ticks, keystrokes), so the
  render queue never drained and the UI locked up. Post-fix the same panel
  renders in ~40 ms (a ~35× drop; per-row allocations fall 174 → ~1). Wrap mode
  still builds every row so its multi-line windowing stays exact.

### Changed
- **The Commits-panel tip markers are now arrows: `↓` for a local branch's tip
  and `↑` for its tracked-remote tip** (previously `■` and `▲`). A commit that is
  both shows `↓↑`. The Tags panel's separate `▲` "pushed to remote" indicator is
  unchanged. Both glyphs still occupy one terminal cell, so the fixed 3-cell
  marker layout is unchanged.

### Added
- **Name a commit when shelving or bookmarking it.** "Shelf this commit" and
  "Bookmark this commit" (on both the Commits and Reflog panels' `.` menu) now
  open a one-line name popup pre-filled with the commit's subject before
  creating the entry: edit the text, press `ctrl+s` to insert the commit's
  short sha at the cursor, `enter` to create with that name, or `esc` to
  cancel. An empty name falls back to the commit subject for a bookmark
  (matching the previous behavior); for a shelf entry it just leaves the label
  unset. The name is display-only — captured once at creation time, not part
  of the entry's identity, and not editable afterward. Named entries show
  ` — <name>` in `gg shelf list` and in the shelf (`G`) quick-switcher
  (mirroring how the bookmark switcher already showed its label). From the
  CLI: `gg shelf commit --name <name> <sha>`.
- **Shelf a commit, and copy shelf/bookmark items (including commits) to a
  temp dir outside the repo.** Two additions to the shelf:
  - **Shelf this commit** — the Commits and Reflog panels' `.` menu now offers
    **"Shelf this commit"** alongside "Bookmark this commit": it freezes the
    selected commit's **changed files** (content only, via `git archive`, no
    message/author) as one durable shelf entry (`ShelfKind = Commit`), capped
    at 200MiB. Because the content is stored (not just a commit reference),
    it survives `git gc` and history rewrites the same way a file shelf entry
    survives deletion of its source. `gg shelf commit <sha>` does the same
    from the CLI, printing the new entry's id.
  - **`[t]` Copy to temp dir** — the shelf (`G`) and bookmark (`g`)
    quick-switchers gain a `[t]` action that copies the highlighted entry's
    files to a fixed sibling directory next to the repo: `<repo>.tmp/` (e.g.
    `/a/x/repo` → `/a/x/repo.tmp`), anchored on the **main** worktree even
    when run from a linked one. Each entry type gets its own subdirectory
    name — `commit-<7-char-sha>` for a shelved or bookmarked commit, the
    entry's id for a shelf file, `bookmark-<label-or-id>` for a bookmarked
    file — and the destination is shown in an editable popup before writing
    (prefilled, but you can change it). Writing outside the worktree is new
    territory for gg's engine (`engine.ExportToDir`, the first op that writes
    outside the working tree); an existing target directory prompts
    overwrite/cancel. `[t]` works the same for a commit bookmark (exports its
    changed-files tree) as for a file bookmark. Also available from the CLI:
    `gg shelf export [--dir <path>] [--force] <entry-id>` (default target
    `<repo>.tmp/<name>` when `--dir` is omitted).
- **Commit sort order (`[ui] commit_sort`)**: choose how the Commits panel and its graph are ordered — `date-order` (the new default; `git --date-order`, a global topological sort so branch **forks always render correctly**) or `plain` (git's lazy newest-first order, much faster on very large repos but the graph can draw a disconnected lane stub when commit dates disagree with topology, e.g. right after a squash). Cycle it live from Settings (`,` → "Commit sort"); it re-walks the feed immediately and saves the choice **per-repo** to `.gg.toml` (so a huge monorepo can opt down to `plain`). Missing key ⇒ `date-order`. `GG_COMMIT_PAGER` still overrides. Fixes the case where a squashed branch's graph looked like it never forked from its base.
- **Reset a diverged branch to the remote tip, discarding local work.** When a
  pull of the current branch can't fast-forward, the divergence prompt now offers
  a **`reset`** choice alongside `rebase`/`merge`/`abort`: it hard-resets the
  branch to the fetched remote tip, throwing away local commits and uncommitted
  changes (the `--ff-only` pull leaves no in-progress merge, so no abort is
  needed first). The same is available non-interactively as
  `gg pull --on-conflict=reset`. Additionally, the Remotes-tab `.` menu now offers
  **"Reset current (<branch>) to <remote> tip"** when the selected remote branch
  is the counterpart of the checked-out branch — a one-step way to snap your local
  branch onto its origin tip. It **always asks for confirmation first**, even when
  slow-op confirms are disabled (`[ui] disable_slow_op_confirm`), since it is a
  one-click hard reset. (git `reset` only moves HEAD's branch, so the action is
  hidden when the remote's local branch isn't the one checked out.)
- **Post-worktree-create hook**: configure a per-repo shell script (`[worktree] post_create_hook`) that runs inside a newly created worktree — e.g. to copy gitignored files (`.env`, local config) from the main checkout. Available for both the TUI and `gg worktree add`. Edit it in Settings (`,` → "Worktree post-create hook", a wide multi-line editor). **The hook requires approval before it runs** — because `.gg.toml` travels on clone, gg never runs a clone-borne script silently. In the TUI a modal shows the script and asks run/skip; the `[h]` toggle in the create-worktree popup is a pre-skip that suppresses even the prompt. On the CLI: `--hook` runs without prompting, `--no-hook` skips, otherwise gg prompts interactively and skips when stdin is not a terminal. The script runs with `cwd` = the new worktree and env `GG_MAIN_WORKTREE` / `GG_WORKTREE_PATH` / `GG_BRANCH` / `GG_REPO`; output streams into the busy log, and a hook failure is reported without rolling back the worktree.
- **`N` / `P` step to the next / previous file from the diff boundary.** In the
  diff view (opened from the files tree, the Status panel, or the Staged panel),
  sitting on the **last** change now shows a proactive bottom-left cue
  `▸ nn → top · NN → next file`; pressing `N` twice steps to the next file in the
  list (the first press primes, mirroring the existing `n`/`p` wrap double-press).
  On the **first** change the cue reads `▸ pp → bottom · PP → prev file` and `PP`
  steps to the previous file. `N`/`P` are boundary-gated (inert on any other
  change) and share the same prime and arrival notice as `Home`/`End`
  file-stepping. A single-change file offers both steps; a picker/bookmark
  compare (no source list) advertises only the wrap.
- **`enter` on a commit drills into its files (and focuses the tree).** On the
  Commits panel, pressing `enter` on a commit now opens the changed-files view
  *and* moves focus to the file tree — `l` followed by switching to the tree, in
  one keystroke. `l` is unchanged (it opens the same view on the commit-list
  side, where ↑/↓ keeps reloading the tree as you move). A WIP pseudo-row
  (◇ Working tree / ◇ Staged) opens its node-vs-parent compare and likewise
  lands on the tree. With the files view already open on the commit-list side,
  `enter` drills the rest of the way in — it moves focus to the file tree.
- **`i` shows a commit's message even while its files view is open.** Pressing
  `i` with the files view open now opens the same scrollable commit-message
  popup as `i` on the Commits panel; the popup layers over the tree and `esc`
  returns to it. Only meaningful for a real commit files view — it is inert for
  a stash or compare view (no single commit behind them).
- **Clickable left-column tabs.** A left-click on a tab name in any left-column
  slot header now switches to that tab (and focuses it) — the same three slots
  `ctrl+←/→` cycles: the top **Branches / Remotes / Worktrees** slot (the
  single-letter `B`/`R`/`W` markers are clickable too), the middle **Files /
  Tags** box, and the bottom **Staged / Reflog** box. Mouse and keyboard share
  one activation path, so a maximized column re-pins to the clicked tab exactly
  as it does when cycled. Clicks off the tab names (a data row, the sort/filter
  decoration) keep their previous focus/select behaviour. The header string and
  the click hit-test derive from one shared tab-segment list, so they cannot
  drift.
- **`P` offers to push an unpushed branch-tip tag with the branch.** When the
  branch's tip commit has one or more local tags not on the remote, `P` first
  runs a fresh `git ls-remote --tags` with a **5-second timeout** to check
  which are missing; if the check times out or fails the tag check is skipped
  and the branch pushes immediately (P never hangs). If unpushed tip tags are
  found, a modal prompts: *Push branch + tags* (default) / *Push branch only* /
  *Cancel*. Choosing *Push branch + tags* pushes the branch first (keeping the
  existing rejected-push recovery — rebase/force/abort), then chains one
  `git push origin refs/tags/…` call for all the tags in a single invocation;
  on success the `▲` tag-pushed-state markers update immediately (optimistic).
  Only the **tip commit's** tags are considered; tags further back in history
  are unaffected.
- **Commits-panel ref decorations (tags + multi-tip group).** The Commits panel
  now renders a `git log --decorate`-style decoration group **before the subject**:
  extra local-branch tips appear in default foreground, and tags appear as
  `⊙<name>` in **yellow** (color 220), including on non-tip lineage rows where the
  tag actually lives. When ≥2 local branches tip a commit the `■` marker gains a
  **superscript count badge** (`■²`, `■³`, … `■⁺` for ≥10); the badge is dropped
  when both `■` and `▲` are shown (no room). When the full group would leave too
  little room for the subject it collapses to `(+N)` where N = extras + tags. Tags
  are now searchable via the Commits `/` filter and `@` highlight. The old
  after-subject `‹name›` pills are removed; all ref info is now in the
  before-subject group and the marker badge. *v1 note: remote-tracking refs still
  appear via `▲` only, not inside the group; in wrap mode the group still collapses
  by panel width.*
- **File-watch auto-refresh (Phase D):** worktrees, reflog, branches, and remotes
  panels refresh the moment their `.git` files change (fsnotify) instead of on a
  timer. Toggle per source with `w` in Settings → Refresh rates. Disabled
  automatically on WSL2 `/mnt` (9p) mounts, where the source falls back to
  interval polling. New `internal/gitwatch` package (recursive ref-tree watching
  for branches/remotes); new `[refresh] *_watch` keys. The Refresh-rates editor
  has a labelled **file-watch** column with a `[x]`/`[ ]` checkbox on watch-capable
  rows (toggle with `space` or `w`). A ref change refreshes its dependent panels,
  not just the watched one (`watchAffectedSources`): a branch/remote change also
  reloads the commit feed (Commits panel `%D` decorations + ■/▲ tip markers); a
  remote change also reloads branches (Upstream/Ahead/Behind); a worktree change
  also reloads branches (new branch + worktree-path) and the feed.
- **Auto remote-tag refresh on tag-list changes.** The `▲` tag-pushed-state
  indicator now auto-refreshes **by default** whenever the Tags panel's contents
  change: once on app load (when the tag list first populates) and again after any
  tag add, remove, push, or delete-from-remote (the operation reloads the tag
  list, which re-enqueues the remote lookup). The refresh uses the same silent
  single-lane background machinery as the opt-in `[refresh] remote_tags` timer.
  It is **independent of the `[refresh] enabled` master switch** — the auto-trigger
  is always active unless explicitly disabled. To disable:
  - **Settings (`,`) → "Auto remote-tag refresh"** — toggles live and persists to
    the global config; the label shows the current state.
  - `[refresh] disable_remote_tags_auto = true` in `.gg.toml` — raw config key;
    a repo can disable it independently of the global setting.
- **Tag pushed-state indicator (`▲`).** Tags that exist on the default remote
  (origin if configured, else the first configured remote) now show a trailing
  `▲` in the Tags panel, mirroring the Commits-panel convention (`■`/`▲` for
  local/remote branch tips). A tag that is local-only — **or that has not been
  checked yet this session** — renders no marker; the two are deliberately
  indistinguishable (no false "local-only" claim before a lookup runs). Two
  opt-in triggers share one lookup command (`git ls-remote --tags`):
  - **Tags `.`-menu action "Refresh remote status"** — a one-shot lookup that
    annotates every visible tag; manual network/auth errors surface on the
    status line.
  - **`[refresh] remote_tags`** (seconds, default `0` = off) — runs the same
    lookup silently in the existing single-lane background scheduler, exactly
    like `[refresh] fetch`; offline/auth failures are discarded silently and
    never written to `errors.log`.
  After a successful **Push tag** the tag immediately gains `▲`; after a
  successful **Delete tag from remote** it loses it (optimistic updates).
  Comparison is by tag name (v1; no hash-mismatch detection). No new CLI
  command.
- `gg config populate (--repo | --global)` — tops up an existing config file
  with every supported setting not yet present, added as commented `[populated]`
  lines; never touches existing overrides; idempotent. Complements
  `gg config init`.
- **Refresh rates editor (Phase C rework).** Background auto-refresh now runs on
  fixed, user-set per-source intervals (floored at `[refresh] min_seconds`,
  default 10) over the single-lane queue — the adaptive engine
  (`disable_adaptive`/`max_read_seconds`/`backoff_factor`) is removed. Settings →
  "Refresh rates" is now an inline editor: select a source, press enter, type the
  seconds (0 = off); it writes `[refresh] <source>` to the repo `.gg.toml`. Read
  durations are still measured and shown there as stats.
- **Background auto-refresh (Phase B).** The TUI can now silently refresh any
  data source in the background on a configurable per-source timer. Everything
  is **off by default** — opt in via the new `[refresh]` config section.
  `[refresh] enabled` (default `false`) is the master switch; each source has
  its own interval in seconds (`status`, `branches`, `remotes`, `worktrees`,
  `tags`, `reflog`, `feed`; all default 0 = that source stays manual-only).
  `[refresh] fetch` (default 0) runs a quiet `git fetch` in the background on
  its own timer, refreshing remote branches on success and silently swallowing
  any network/auth error. Background reads are fully **silent** — no spinner,
  no status-line change, no cursor/selection disturbance (selections survive via
  identity-based reconciliation from Phase A). Auto-refresh is **suppressed**
  while an operation is running, a popup/modal/overlay is open, or a
  filter/search is being typed. A **user operation preempts** any in-flight
  background read: `startOp` cancels the background context so the git
  concurrency slots are freed immediately (enabled by fixing `LimitRunner` to
  honour the context while acquiring the semaphore — bug #4). Background fetch
  runs off the foreground op slot so it cannot block an interactive op. The
  master switch can also be toggled live from **Settings (`,`)** — the choice
  is persisted to `[refresh] enabled` in the global config file so it survives
  restarts. Phase C (background-refresh config editor) followed.
- **Per-source async refresh (Phase A).** The TUI now reloads data
  source-by-source rather than all-at-once: status, branches, remote branches,
  tags, reflog, worktrees, commit feed, and identity each load independently
  via their existing `domain` query and emit a `dataAvailableMsg` when ready.
  After an action, only the sources that operation touched are re-fetched — a
  commit refreshes only status and the feed; a push refreshes only remote
  branches. Unmapped operations default to refreshing all sources (safe
  fallback; never a correctness regression). Manual `r` and startup fan out all
  sources in parallel with per-panel ⏳ spinners indicating which panels are
  still loading. Repo-switch (`reRoot`) and the conflict-process flow still use
  the previous monolithic load; porting those is future work. This is the
  foundation for Phase B (silent background auto-refresh on per-source timers)
  and Phase C (per-source interval config and stats editor).
- **Session error log.** Every git operation that fails (any operation or read
  query that returns an error to a frontend) is now recorded to an always-on
  `errors.log` in the gg state dir (beside `operations.log`), and a new
  **Settings (`,`) → "Session errors"** entry shows the current session's
  failures in a read-only viewer. Control-flow probes that exit non-zero by
  design (e.g. `git merge-base --is-ancestor`) and user cancellations are not
  recorded — only genuine failures.
- **Push a selected branch.** The `.` menu on the Branches panel now offers
  **Push \<branch\>**, which pushes the highlighted branch and sets its upstream
  (`git push -u origin <branch>`) — for any local branch, current or not,
  including one that was never pushed before, without first checking it out. The
  CLI gains the matching `gg push [<branch>]` positional. Previously the only
  push was the `P` key, which always pushed the checked-out branch, so a
  highlighted non-current branch could not be pushed. If the push is rejected
  because the remote moved ahead, a non-current branch offers only force-push or
  abort (rebasing would rewrite the wrong branch); the checked-out branch keeps
  the full rebase / force / abort recovery.
- **Branch prefixes.** A writable two-scope (global + per-repo) registry of
  reusable, templated branch-name skeletons. Press `ctrl+p` in the create-branch
  (`b`/`B`) popup, or `p` in the create-worktree popup, to pick one; interactive
  `<user:…>` labels are collected, the template is resolved, and the result seeds
  the branch name for you to complete. Prefixes accept the usual gg tokens
  (`<user:LABEL>`, `<seq:NAME:N>`, `<date:…>`, `<parent-branch>`, `<repo>`,
  `<random-*>`; `<branch>` is rejected). Manage them in Settings (`,`) → Branch
  prefixes, or via `gg prefix ls | add [--global] <value> | rm [--global] <value>`.
- **Copy to working dir.** The `.` menu on a focused non-working file (a stash
  file, an old commit's file, or a staged file) now offers **Copy to working
  dir**, which writes that file's content into the working tree at its own path
  (with an overwrite prompt if a differing working file already exists). It is
  the write-sibling of **Compare against working dir**.
- **Confirm slow operations.** The TUI now asks a yes/no confirmation (default
  **No**; `y`/`n`/`esc`) before slow working-tree operations — switch, remote
  checkout, pull, merge, rebase, fast-forward, and reset. On by default; opt out
  with `[ui] disable_slow_op_confirm = true`.
- **Smart push recovery.** When a plain push is rejected because the remote has
  moved ahead, gg no longer dead-ends on an error: from the single push action
  it offers **rebase onto the remote and push**, **force-push** (routing through
  the existing force-with-lease / force confirm), or **abort**. In the CLI,
  `gg push --on-reject=rebase|force|force-with-lease|abort` drives the same
  recovery; with the flag unset a rejected push fails fast (non-interactive) or
  prompts (interactive), so a script never silently no-ops.
- **Soft reload.** Pressing `r` no longer blanks the screen on large repos: the
  panels stay visible (showing the previous data) with a ⏳ in each panel title
  and a `reloading…` status line until the fresh data swaps in. Repo switches and
  initial startup keep the brief `loading…` screen. While a reload (or a repo
  switch) is already in flight, `r` is inert and its footer hint is hidden, so a
  second `r` can't restart the walk or flash the outgoing repo's stale panels.
- **Drop multiple selected commits at once.** With 2+ commits in the Commits
  panel's `◉` compare selection, the `.` menu now offers **Drop N selected
  commits** — deleting them all in a single interactive rebase. Unlike squash
  there is no adjacency requirement; non-contiguous commits can be dropped and
  the gaps are preserved. (The existing single-cursor **Drop commit** is
  unchanged.) Working-tree / staged rows and off-branch selections are refused;
  conflicts pause for `git rebase --continue`.
- **Busy line shows elapsed time.** While an operation runs, the status line's
  `⏳` indicator now counts up (`⏳ creating worktree… 2m14s`), driven by a
  once-a-second heartbeat. A long op on a huge repo — e.g. a 20GB `git worktree
  add` checkout that emits no progress — visibly advances instead of looking
  frozen.
- **Operation debug log.** Optional diagnostic log that mirrors every operation
  and git invocation (redacted) as JSON lines to `operations.log` in the gg
  state dir, leaving a trace of a hung or slow op. Toggle it live from the `,`
  Settings menu (which shows the on/off state and the log's full path); the
  choice persists to the global config's `[debug] log_operations`, so it also
  survives restarts. Off by default.
- **Files panel ignores line-ending-only changes.** A tracked file whose only
  unstaged difference is its line endings (CRLF↔LF) — common on Windows/WSL — no
  longer shows up as modified in the Files panel or its count badge. Detection
  uses `git diff --ignore-cr-at-eol` (one extra diff, only when there are
  modified files, scoped to just those paths); a file's staged change, if any,
  is preserved. Set `[ui] show_eol_only_changes = true` to surface them again
  (e.g. when you are deliberately renormalizing endings). The scriptable
  `gg status` is unaffected — it stays faithful to `git status`.

### Fixed
- **The Reflog panel no longer shows a literal `(%gr)` after every entry.** The
  reflog rows meant to end with a relative time (e.g. "2 minutes ago"), but the
  format used `%gr`, which git does not recognize and prints verbatim. The time
  is now read from the reflog selector under `--date=relative`, so each row shows
  its real relative time and the numeric `HEAD@{N}` selector is preserved.
- **A diverged/ahead `origin/main` (and other tracked remote tips) now shows in
  the Commits panel on startup, not only after a manual refresh.** The commit
  feed only walks in a branch's remote tip when its upstream is added to the
  scope, and that upstream is dropped until the *remote branches* source has
  loaded (a configured-but-unfetched ref would make `git log` error). The one
  place that re-walked the feed once the tracked upstreams became known fired on
  the *branches* and *feed* sources arriving but **not** on the *remotes* source
  — so whenever remotes was the last of the three to land during the parallel
  startup fan-out, the upstream re-walk never happened and the remote tip's
  ahead commits stayed hidden (looking "not loaded") until you pressed `r`. The
  remotes arrival now re-checks the same latch, so whichever of the three lands
  last triggers the walk.
- **A failed squash now unmarks the off-branch commits it complained about.**
  Squashing a ◉ selection that includes a commit not on the current branch
  (easy to do from the multi-branch commit feed) fails the membership check with
  `commit <sha> is not on the current branch`. Previously the stray marks stayed
  set, so retrying hit the same error until you hunted them down by hand. The
  off-branch commits are now cleared from the selection (the on-branch marks are
  kept), and the status line reports how many were unmarked.
- **The repository path in the top-right header now appears on startup**, not
  only after the first repo switch (`R`). The header renders the open worktree's
  full path, but only the legacy repo-switch load set the backing
  `currentWorktree` field; the app-start fan-out (the per-source refresh
  registry) never did, so the path stayed blank until you switched repos. The
  startup bootstrap now seeds it from the git working-tree root it already
  resolves, so the path shows from first paint.
- **The Commits panel didn't refresh after a push (or force push).** A push
  moves the remote-tracking ref, which changes the feed's `%D` ref decorations
  and the `■`/`▲` local/remote tip markers — but `engine.Push` mapped only to
  the Branches and Remotes panels, so the Commits feed went stale until the next
  manual `r`, watcher event, or interval refresh. `engine.Push` now also
  refreshes the feed (`srcFeed`). Tags are deliberately left out: pushing a
  branch doesn't push tags, and a tags reload would auto-fire a needless
  background `ls-remote` (the `▲` pushed-state lookup).
- **"Go to tip in commits" (and the `■` tip marker) failed for slash-named
  local branches.** The commit feed parsed `%D` decorations with a "name
  contains `/` ⇒ remote-tracking" heuristic, so a local branch like `feat/foo`
  was misclassified as a remote ref. The Branches-panel "Go to tip in commits"
  action — and `commitIdentOf`'s `■` local-tip marker — both key off ref kind,
  so any slash-named branch silently didn't work, even when two branches sharing
  one commit differed only by whether the name had a slash. The feed now runs
  `git log --decorate=full` and classifies refs by namespace
  (`refs/heads/` → local, `refs/remotes/` → remote, `refs/tags/` → tag), so a
  slashed branch name is unambiguous. Additionally, "Go to tip in commits" now
  matches the branch's tip **hash** rather than its decoration name, so it finds
  the tip regardless of how (or whether) git decorated that row.
- **Branch delete/rename triggered a needless remote-tag network lookup.**
  `engine.DeleteBranch` and `engine.RenameBranch` were not listed in
  `opAffectedSources`, so they fell through to the conservative "refresh all
  sources" default. That reloaded the Tags panel, whose arrival auto-enqueues a
  background `git ls-remote --tags` (the `▲` pushed-state lookup) — a network
  round-trip on every branch delete/rename even though neither can change tags.
  Both ops now map to `{branches, feed}` (the feed for the moved `%D` ref
  decorations and tip markers), so no tags reload and no `ls-remote` fire.
- **Flaky `git worktree add`/`remove` failures (`reading output: read |0: file
  already closed`).** `gitexec.Stream` read stdout with `StdoutPipe()` + a manual
  reader goroutine running concurrently with `cmd.Wait()`. On a clean exit `Wait()`
  closes the pipe's read end as part of its own cleanup; if the reader was still
  blocked (common for commands that emit little/no stdout, like `git worktree add`)
  it got `os.ErrClosed`, which was surfaced as a command failure — so worktree ops
  failed intermittently under load. `Stream` now streams stdout through a
  line-splitting `io.Writer` (`cmd.Stdout = lineWriter`); os/exec runs its own
  copier and `Wait()` blocks until it drains, so the read can never lose the race
  to `Wait`'s close. `WaitDelay`'s grandchild-held-pipe handling is preserved.
- **Space key now works in the TUI on Windows.** Staging/unstaging a file
  (`space`), and every other space action (picker toggles, the settings
  agent-skill checklist, file-finder selection), did nothing on Windows: Bubble
  Tea's Windows input driver delivers a space keypress as a rune rather than the
  `KeySpace` event that Unix produces and that gg's handlers keyed off. gg now
  normalizes a lone space rune to `KeySpace` at the top of key handling, so the
  space key behaves identically on Windows, Linux, and macOS.
- **Creating a worktree from inside another worktree no longer nests it.**
  `gg worktree add` (and the TUI popup) now anchor the new worktree on the
  repository's **main** worktree — both the `<repo>` template token and the
  relative `../` path base — instead of whichever (linked) worktree gg was
  invoked from. Previously, running it from a nested worktree produced a doubled
  `.worktrees` path such as
  `…/repo.worktrees/feature-x.worktrees/new-branch`; it now lands beside the
  main repo at `…/repo.worktrees/new-branch` regardless of where you run it.
  `gg worktree remove <relative-path>` resolves against the same main-worktree
  base so the two round-trip.
- **Worktree creation no longer hangs forever on huge repos.** On a large repo,
  `git worktree add` (and other streamed git commands) can spawn a long-lived
  background daemon — fsmonitor, `gc --auto`, `git maintenance` — that inherits
  the subprocess's stdout pipe and outlives it. The git process itself exited
  (the worktree was fully written to disk), but the reader looped on stdout until
  EOF, which the lingering daemon never delivered, so the operation blocked
  forever and the TUI sat in a permanent loading state. The git runner now sets
  `Cmd.WaitDelay` and reads stdout concurrently with `Wait`, so a clean exit
  completes even when a detached child still holds the pipe.
- **`space` now stages every marked file, not just the cursor row.** Marking
  files with `m` in the Files (or Staged) panel then pressing `space` stages
  (unstages) all of them in a single `git add` / `git restore --staged`,
  matching how `d` discards the marked set. The marks are cleared by the op, so
  the staged files no longer keep their `◆` marker after moving to the Staged
  panel. With no marks, `space` still acts on the cursor row.
- **A locked worktree can now be removed.** An interrupted `git worktree add`
  (e.g. quitting gg mid-checkout on a huge repo) leaves the worktree locked with
  reason "initializing", which git refuses to remove even with `--force`. Remove
  now detects the lock and offers an explicit **Unlock and remove** decision
  (TUI modal; `gg worktree remove --force` answers it non-interactively, also
  cleaning up a stale entry whose directory was already deleted). A new
  `git worktree unlock` verb backs it.
- **The commit `◉` selection count no longer over-counts stale rows.** After an
  operation rewrote history (e.g. a drop/squash rebase), keys for commits that
  no longer exist lingered in the selection and inflated the `.` menu labels
  ("Squash N commits" / "Compare range of N commits") — selecting 3 more after a
  4-commit selection could read "7". The labels and size gates now count only
  keys that still resolve to a live row.

- **A git command that fails to start now reports the real cause instead of a
  bare `exit -1`.** When git is killed by a signal or never starts (fork/exec
  under resource pressure on a huge repo, etc.) it produces no stderr and an
  exit code of -1; the error previously rendered as an empty `… failed (exit
  -1): ` with nothing actionable (seen as a transient "error -1" when creating a
  tag right after a push). The underlying OS error is now preserved in the
  message so the failure is diagnosable.

### Changed
- **Readable push-rejection messages.** A rejected push no longer dumps git's
  raw multi-line stderr into the one-line status bar. The common reasons are
  rewritten into a single actionable sentence: a **non-fast-forward** rejection
  → "the remote has commits you don't have; pull (or fetch + rebase) first, or
  force-push to overwrite"; a `--force-with-lease` **stale-info** refusal → "the
  remote moved since your last fetch; fetch & review, then retry" (so the lease
  safety net is explained rather than mistaken for a defect); a server-side
  **protected-branch / pre-receive hook** rejection gets its own message.
- **Module path is now `github.com/homeend/gigagit`** (was
  `github.com/gigagit/gg`). The repo is hosted at
  <https://github.com/homeend/gigagit>, so the tool installs with
  `go install github.com/homeend/gigagit/cmd/gg@latest` (binary `gg`). No CLI,
  config, or behaviour change — import paths only.

### Added
- **Deeper, on-demand commit loading.** The Commits panel now loads more history
  before you hit the bottom and lets you reach it directly. New `[ui]` settings
  set the counts: `commit_initial_count` (first paint, default 300, up from 50),
  `commit_batch_size` (per later page, default 300), and
  `commit_search_max_pages` (default 5). `ctrl+l` loads the next batch on demand;
  **Home**/**End** jump to the top/bottom of any list, and End on Commits also
  loads the next batch (press again to walk deeper). Applying then clearing a `\`
  commit filter now restores the commits you had already loaded instead of
  re-walking from the top. Eager search: when a `/` filter or `@` highlight query
  isn't among the loaded commits, `ctrl+f` pages history to find it and jumps to
  the first hit (asking before it scans deeper).
- **Remotes tab: tracked branches float to the top.** Remote branches whose
  short name matches a local branch (one present in the Branches tab) now sort
  ahead of the rest in the Remotes tab, so the branches you're actually working
  with sit at the top. Within each group git's original order is preserved.
- **Files panel keeps the filename when paths are too long.** In the Files panel
  (the middle window of the left column), a path wider than the column is now
  middle-elided (`internal/tu…/view.go`) instead of tail-truncated: the path's
  beginning and the filename — the parts you actually need — stay visible, and
  the directories nearest the file are dropped to fill the column. The leading
  status glyph is preserved. Applies in the default cutoff display mode (`w`
  still cycles to wrap/scroll for the full path); the Staged panel is unchanged.
- **Filtered commit log** — `\` on the Commits panel opens a filter popup (path,
  author, message, date range) that narrows the feed via `git log` flags; filters
  compose with branch scope. "Commits touching this" seeds a path filter from the
  fuzzy file finder and the files view. The commit-graph hides while a filter is
  active (the filtered feed is a non-contiguous subset). `ctrl+r` clears the
  focused window's filtering — its `/` filter, or on the Commits panel the `@`
  highlight and the `\` commit filter — leaving other windows' filters intact
  (the "Clear filter" `.`-menu row clears the commit filter alone).
- **Show any commit by SHA (`#`) + a command palette (`ctrl+p`).** Press `#` to open a small input, type a commit SHA (or any commit-ish ref — `HEAD~3`, a branch, a tag), and `enter` opens that commit's files in the files-view — no need to scroll the Commits feed to find it. An unknown ref shows an inline error and keeps the prompt open. `ctrl+p` opens a generic command palette (a launcher for global commands); for now it holds a single entry, **Show commit**, which runs the same flow.
- **Commits panel — local/remote tip markers.** Each commit that is the tip of a
  local branch shows `■`, and a commit that is the tip of that branch's tracked
  remote shows `▲`; when local and remote point at the same commit both markers
  appear together. Tracked-remote tips are walked into the feed so the marker
  shows even when the local branch is behind its upstream. No ahead/behind
  numbers; the divergence reads from the graph.
- **Repository path in the top bar.** The header's top-left title now shows the open repo/worktree's directory name (the path's final segment) instead of a fixed "gigagit" brand, with the branch beside it; the repository's full path is right-aligned on the right. When the path is too long for the space between them it is elided in the middle (`…`), always keeping the directory name visible. (The brand is still shown as a fallback before the first repo loads.)
- **Read a commit's full message.** On the Commits panel, `i` opens the selected commit's full message in a scrollable popup with a `git show`-style metadata header — full hash, author, date, ref decorations (branches/tags), and merge parents — above the subject + body, plus a compact author · date line in the footer. Handy for long descriptions, trailers, and multi-paragraph messages that the one-line subject hides. `I` opens the same message in your external editor (`$EDITOR`, read-only). Both are also in the commit `.` menu as **View message** / **Open message in editor**.
- **Solo this tag.** The Tags panel `.` menu now offers **Solo this tag** — it scopes the Commits panel to the tag's history (`git log <tag>`: the tag's commit on top, everything in that release below, lazily paged) and focuses the Commits panel, so you can browse a release's commits even on a huge repo where the tag's commit is far outside the loaded window. Clear it with **Show all branches** (or re-run Solo this tag). Same scoping mechanism as "Solo this branch".
- **Fuzzy file finder.** `F` opens a finder over every tracked file; fuzzy-type a
  path (`fvgo` → `files_view.go`) and `enter` opens a per-file menu: View content,
  Diff, History, Blame, Open in editor, Copy path. Built for tens of thousands of
  files in a monorepo.
- **Annotate an existing tag.** The Tags panel `.` menu now offers **Annotate `<tag>`** — a message dialog (prefilled with the tag's current subject) that turns a lightweight tag annotated, or updates an annotated tag's message, keeping its target commit. The CLI adds `gg tag annotate -m <message> <name>`.
- **Merge / rebase a local branch from the Branches menu.** The Branches panel
  `.` menu now offers **Merge `<branch>` into current** and **Rebase current onto
  `<branch>`** for the selected branch (hidden on the checked-out branch itself and
  on a detached HEAD) — the same one-click actions the Remotes and Tags panels
  already had. Conflicts/dirty trees resolve through the usual modal.
- **Delete a tag from a remote.** The Tags panel `.` menu now offers **Delete `<tag>` from remote** (with a confirm prompt), and the CLI extends `gg tag rm <tag> --remote [<remote>]` — `git push <remote> --delete refs/tags/<tag>`. The local `gg tag rm <tag>` is unchanged.
- **Force push.** After a rebase/amend/reword rewrites history, the current branch can now be force-pushed. On the Branches panel, the `.` menu offers **Force push `<branch>`** for the checked-out branch: a modal lets you pick **force-with-lease** (refuses if the remote moved under you) or **force** (overwrites the remote unconditionally), and `esc` aborts — the choice doubles as the confirmation for this history-overwriting push. The CLI adds `gg push --force` (plain `--force`, no lease) and `gg push --force-with-lease`; with no flag `gg push` stays a plain push. First gg capability to overwrite published remote history.
- **More Tags menu actions.** The Tags panel `.` menu now offers **Copy tag name**, **Copy commit id** / **Copy commit sha** (the tag's target commit, full SHA resolved on demand), plus one-click **Merge `<tag>` into current** and **Rebase current onto `<tag>`** (reusing SmartMerge/SmartRebase; merge/rebase hidden on a detached HEAD).
- **Delete a remote branch.** The Remotes panel `.` menu now offers **Delete `<remote>/<branch>`** (with a confirm prompt), and the CLI adds `gg remote rm <remote>/<branch>` — `git push <remote> --delete`. Destructive and outward-facing, so the TUI confirms; the CLI command is the confirmation.
- **More Remotes & Branches menu actions.** The `.` menu on the Branches and
  Remotes panels now offers **Copy commit id** (the short tip hash) and **Copy
  commit sha** (the full 40-char hash, resolved on demand). The Remotes menu
  additionally offers **Create worktree from** the remote branch, **Merge** it
  into the current branch, and **Rebase** the current branch onto it (reusing
  SmartMerge/SmartRebase; conflicts resolve through the usual modal). Merge and
  rebase are hidden on a detached HEAD.
- **`gg config init`.** Scaffolds a documented config file (`--repo` for
  `.gg.toml` at the repo root, `--global` for `~/.config/gg/config.toml`) with
  every setting commented-out alongside its default and a one-line description.
  Refuses to overwrite without `--force`.
- **Squash selected commits.** On the Commits panel, `m` now toggles a commit
  into a selection set (the `◉` markers); the `.` menu then offers **Compare**
  (unchanged) and **Squash N commits**. Squash combines the selected commits
  into one, concatenating their messages (reword afterward if you like). When the
  selected commits aren't adjacent, a prompt offers **Reorder & squash** — the
  skipped in-between commits move to just after the squashed commit. Off-branch
  selections are refused; conflicts pause for `git rebase --continue`. "Compare
  with marked" is removed from the Commits panel — the selection set replaces it.
- **Fast-forward the current branch to a commit.** When another branch is built
  on top of your current branch, the Commits panel `.` menu now offers
  **Fast-forward `<branch>` to here** on any commit ahead of your branch's tip —
  advancing the branch with no merge commit (`git merge --ff-only`). Also
  available as `gg fast-forward <commit>`. The menu action only appears when the
  selected commit is actually ahead (decided in-memory from the loaded commit
  graph, no extra git call), and the operation refuses with a clear message if
  the target is not a fast-forward.
- **Git identity & app profiles.** Settings (`,`) → **Identity & profiles**
  shows the current git identity with global, repo-local, and effective values
  kept distinct ("not set — inherits global" reads differently from a real
  local value), and a list of named **profiles** — `{name, email}` presets you
  define, scoped either **global** (every repo) or **this repo only**. `enter`
  on a profile (or `e` to edit the live identity) prompts **apply to this repo
  or globally**, then writes git's `user.name`/`user.email`; `n`/`r`/`d` create,
  rename, and delete profiles. Profiles live in a new writable side-store under
  the XDG state dir (this is gg's first feature to write git config). CLI
  (`gg identity` / `gg profile`) is not wired yet.
- **Reflog recovery actions.** The Reflog tab's `.` menu now offers
  **Reset to this entry** (soft/mixed/hard, with a confirm when the entry is off
  the current branch) and **Check out this entry…** (detached HEAD, or create a
  new branch at it and switch) — the "rescue lost work" half of the reflog,
  working even on dangling commits.
- **Reflog window.** The bottom-left panel is now a tab group: `ctrl+←/→`
  toggles **Staged ⇄ Reflog**. The Reflog tab lists the HEAD reflog (read-only,
  newest first, per-worktree, capped by `[ui] reflog_limit` — default 200);
  `enter`/`l` opens an entry's commit in the files view (works for dangling
  commits too), and the `.` menu offers *Copy SHA* and *Bookmark this commit*
  (compare via the `g` switcher).
- **Editable text fields now have a real cursor.** Every editable popup field —
  commit title/description, branch name, rename branch, tag name + message,
  tag-checkout name, stash name, shelf restore + bookmark paste destinations,
  and the create-worktree fields — shows a visible cursor and supports full in-place
  line editing: `←`/`→` to move, `Home`/`End`, insert and delete at the cursor
  (`Backspace`/`Delete`), word-jumps (`Alt`+`←`/`→` and `Ctrl`+`←`/`→`), and
  `Ctrl`+`W` to delete the previous word. The commit description edits as a
  multi-line buffer (`Enter` inserts a newline; `↑`/`↓` move between lines). The
  quick-switcher *search/filter* inputs are intentionally unchanged. All fields
  share one small `textfield` component.
- **`enter` in the file-history view opens the diff full-screen.** Instead of
  only the cramped right-pane preview, `enter` on the selected commit promotes
  its diff to the full diff surface — with the usual `n`/`p` change navigation,
  scrolling, and `h`/`b` to reopen history/blame. `esc` returns to whatever was
  beneath the history view.
- **Open any file in your external editor (read-only).** New *Open in external
  editor* action that materializes a file's content to a throwaway temp file and
  opens it in `$VISUAL`/`$EDITOR` (the temp keeps the real extension so syntax
  highlighting works, is marked read-only, and is deleted on exit — no
  working-tree status reload). Available wherever a file is addressable: the
  commit files view (`l`, tree side, `.`-menu — the file's content at that
  commit); the file-history (`e`) and blame (`e`) views — that commit's /
  revision's version; the bookmark (`g`) and shelf (`G`) quick-switchers (`e`);
  and the Staged panel `.`-menu (*Open staged version…* — the index blob, which
  differs from the working file). Working-tree files keep their existing live
  *Edit in editor*. Path-less commit bookmarks and staged deletions (no content)
  are skipped.
- **Step between files from inside the diff view.** In the full-screen diff,
  `home` jumps to the top of the current file and `end` to the bottom; at the
  edge the key *primes* a file step — a bottom-left cue appears (`▸ end again →
  next file` / `▸ home again → previous file`) and the next same-direction press
  moves to the previous/next file, announced by a bottom-left notice naming the
  newly shown file (so a silent file swap is never missed). Any other key cancels
  the primed step. At the first/last file a `▸ no previous/next file` notice
  shows instead. Works for diffs opened from the file tree (commit files /
  full-tree / compare) and from the Status / Staged panels; the tree/panel
  selection follows, so `esc` lands on the last-viewed file. Conflicted rows (no
  plain diff) are skipped.
- **Copy commit title.** The Commits panel `.`-menu now offers *Copy commit
  title* alongside *Copy commit id*, putting the selected commit's subject line
  on the system clipboard.
- **Wrap-around change navigation in the diff view.** When `n` (next change)
  reaches the last change — or `p` (previous) reaches the first — the press no
  longer just stops. A header cue (`↻ n again → top` / `↻ p again → bottom`)
  appears, and pressing the same key once more wraps to the other end. Any other
  key cancels the primed wrap, so a single overshoot never jumps you across the
  file. Also applies to the `ctrl+↓`/`ctrl+↑` aliases.
- **Pull another branch without leaving yours.** In the Branches panel, `p` on a
  non-current branch (or the `.`-menu *Pull <branch> (stay here)*) now pulls that
  branch in the background: it fast-forwards its ref when it can, pulls in the
  branch's own worktree if it has one, or otherwise stashes → switches → pulls →
  switches back — so you end up where you started. (This is SmartPull's
  background intent, previously only reachable via `gg pull <branch> --background`.)
  Also fixes a misleading "cannot fast-forward" prompt when the target branch
  lived in a worktree.
- **Mark two rows to compare them.** In the Commits panel, mark one row (`m`) and
  mark a second — gg opens their whole-tree compare directly (the GitKraken
  "select two, see the diff" gesture), instead of the old "no pair operations"
  dead-end. Works for any pair of a commit, the `◇ Working tree` row, or the
  `◇ Staged` row (ordered older→newer); the `.` menu *Compare with marked* still
  works too.
- **See every file at a commit, not just what it changed (`a`).** In the commit
  files view (`l`), pressing `a` toggles between the commit's *changed* files and
  the **full tree** — every file that would exist if you checked the commit out
  (`git ls-tree -r`, which walks tree objects and is cheap even on a huge repo).
  `a` again returns to the changed set; the mode sticks as you move between
  commits. In full-tree mode `enter` diffs the file's version at the commit
  against your working tree (so untouched files still show something useful), and
  the `.` menu's **View file** shows that file's content at the commit (no diff)
  in a right-pane preview while the tree stays on the left (`↑/↓` scroll, `z`
  display mode, `esc` closes the preview, `←` returns to the tree). **View file
  works in the changed-files view too** (any non-deleted file row), not just the
  full tree.
- **Search history.** Enter-confirmed searches are remembered per window — the
  panel `/` filter and the `@` highlight share one ring, while the bookmark (`g`)
  and shelf (`G`) switchers and the files-view tree search each keep their own.
  While typing a search, **alt+↓** opens a scrollable dropdown of past phrases
  (newest first, up to 10 rows with `↑N`/`↓N` scroll markers); **alt+↓/↑** move
  the highlight and preview it into the query, **enter** accepts, **esc** (or
  alt+↑ above the newest) restores your draft, and typing resumes live editing.
  History is stored per-repo; `[ui] search_history_size` sets the per-ring cap
  (default 20, hard max 1000).
- **Uncommitted work shows in the Commits graph (WIP rows).** When the tree is
  dirty, the Commits panel shows `◇ Working tree (N)` and/or `◇ Staged (N)` rows
  chained above HEAD. `l`/`enter` opens their whole-tree diff (working tree vs
  index, index vs HEAD); commit-only operations are unavailable on them. (Stage 3
  of the compare-trees arc.)
- **Compare a commit against your working copy via the WIP rows.** The
  `◇ Working tree` / `◇ Staged` rows now join the two-row compare flows: mark one
  (`m`) or add it to the `◉` selection, then *Compare with marked* / *Compare
  selection* diffs it against a commit (commit ↔ working tree, commit ↔ index, or
  staged ↔ working tree). A 3+ range stays commits-only. Also fixes marking a
  commit while the tree was dirty landing on the wrong row.
- **Instant Commits feed on huge repos (plain-order loading).** The feed now uses
  git's lazy newest-first order, which parses only the page on screen — on a
  1.4M-commit repo the commit walk drops from ~18 s to ~40 ms (`--date-order` was
  forcing a global topological sort of the whole history). First paint is now
  bounded by `git status` on the worktree (a separate cost), not the commit walk.
  Plain order is topologically correct for all practical viewing; only very deep,
  merge-heavy multi-branch history can show a rare cosmetic lane stub. Opt into a
  guaranteed-perfect (but slow on big repos) graph with
  `GG_COMMIT_PAGER=date-order`.
- **Highlight search in the Commits panel (`@`).** A second search that
  complements `/`: instead of filtering the feed, `@` keeps every commit visible,
  dims non-matching rows, and leaves the commit graph drawn. `ctrl+↑/↓` jump to
  the previous / next match (wrapping); `enter` keeps the highlight, `esc` clears
  it. `@` and `/` are mutually exclusive.
- **Bookmark a commit and compare against it.** The Commits panel `.` menu now
  has **Bookmark this commit**, storing a path-less pointer in the same `g`
  switcher as file bookmarks; the switcher row shows the commit's subject
  (e.g. `feat / a1b2c3d — Fix the parser crash`) so you can tell them apart, and
  you can filter by it. In the switcher, `enter` on a bookmarked commit opens a
  whole-tree compare of it (base) against the currently-selected Commits-panel
  commit (subject). File-only actions (paste / vs-shelf / mark) are not offered
  for a commit bookmark.
- **Commits panel shows a loading indicator (⏳) while the feed is loading.** On
  a large repo a scope change (Solo / Show all / selection) or paging in older
  commits can take a few seconds; the Commits title now shows ⏳ next to the
  count while that walk is in flight, so the panel no longer looks frozen.

### Changed
- **TUI: every `/` filter now lets you move the selection while you type**, so
  search feels the same everywhere — like the commit filter always has. In the
  file finder (`F`), bookmark (`g`) / shelf (`G`) switchers, the repo switcher
  (`R`), the file-tree filter, the content viewer, and the `.` action menu, plain
  `↑/↓` and `pgup/pgdn` move through the filtered rows without leaving the input or
  resetting the cursor; `enter` still locks the filter and a second `enter` opens;
  `j`/`k` stay query text. Backed by one shared `filterMotion` contract so the
  surfaces can't drift apart again. `pgup/pgdn` also now page in plain navigation
  mode in the switchers/menu. (Also fixes bookmark/shelf paging stopping short of
  the last row.)
- **TUI: the `R` repo switcher is now navigation-first too.** It opens showing the
  repo list; press `/` to filter (so `z` is a literal query character instead of
  cycling the display mode), `↑↓/jk`/`pgup/pgdn` to move, `enter` to switch — while
  filtering, the first `enter` locks the filter (same lock-then-act flow as the
  other switchers) and the next `enter` switches. Brings the last quick-switcher in
  line with the finder and bookmark/shelf popups.
- **TUI: the `F` fuzzy file finder is now navigation-first, like every other gg
  list.** It opens showing the file list; press `/` to start filtering and type a
  query (so `/` and `z` are literal query characters — fixing the bug where typing
  a query containing `z`, e.g. `zdata`, cycled the display mode and showed nothing,
  and the confusion around a literal `/` in the query). `esc` clears the filter and
  keeps the finder open; a second `esc` closes it. `pgup`/`pgdn` page the selection,
  and `alt+↑/↓` recalls previous file searches (shared with the file-tree filter).
- **TUI internals: the commit/compare/stash files view is now a small state
  machine.** A `filesMode` plus a set of transition methods (the single close
  chokepoint `closeFilesView`, plus `openChangedFiles`/`openCompareFiles`/
  `openStashFiles`/`toggleFullTree`/`openPreview`/`closePreview`/focus) are the
  only mutators of its state, so mode switches can no longer leave stale fields
  behind. No user-visible change — structurally prevents the class of "half-reset"
  bugs that the full-tree and file-preview features each hit during development.
- **TUI: the full-screen diff is now a member of the layer stack.** `esc` from a
  diff opened over history or a bookmark/shelf picker returns to that surface
  instead of the base layout; the diff's `.` menu and mouse wheel always target
  the diff. Retires the internal `clearLayers` workaround for diffs.
- **Editable popup fields now show their editable area.** Every text field in
  the popups (commit/amend title & description, reword, branch/tag/worktree
  names, paste/restore destinations, stash name, …) is drawn on a subtle
  background that fills to the box edge, so you can see the slot — and that it's
  empty — at a glance. The focus cursor is a light block that stays visible
  against it.

### Fixed
- **Closing a files view returns focus to the panel that opened it.** Opening a commit's files view (e.g. `enter` on a tag or a reflog entry) runs on the Commits column, but `esc`/`l` used to always leave you on the Commits panel — not the Tags (or Reflog) panel you came from. The view now remembers its source panel and restores focus to it on close.
- **`enter` on a tag now always opens its commit.** Previously it only moved the Commits cursor to the tag's target *if that commit was already in the loaded feed* — so on a large repo (e.g. babel: 922 tags pointing at old releases, ~50 commits loaded) it almost always dead-ended with "tag … target not in the loaded commits". Now, when the target isn't in the loaded feed, `enter` opens that commit's files view directly by hash (like `enter` on a reflog entry); when it *is* loaded, it still jumps the cursor in the graph.
- **SSH remotes no longer freeze the TUI on a host-key or passphrase prompt.** The earlier credential-prompt fix (`GIT_TERMINAL_PROMPT=0`) only covered HTTPS; an unknown ssh host key (`Are you sure you want to continue connecting?`) or a passphrase not held by an agent still read `/dev/tty` and hung the raw-mode UI. The TUI's git runner now also sets `GIT_SSH_COMMAND="ssh -o BatchMode=yes"` (preserving any custom `GIT_SSH_COMMAND` you already set), so ssh fails fast with a clear error (`Host key verification failed` / `Permission denied (publickey)`) instead of blocking. The scriptable CLI is unchanged — a real terminal can still service an ssh prompt.
- **Remotes menu actions no longer leak onto the Branches/Worktrees tabs.** The
  `.` menu offered remote-branch actions (Create worktree from / Merge / Rebase
  onto / Delete `<remote>/<branch>`) referencing the Remotes panel's stored
  selection even when another left tab was focused. The rows now require the
  Remotes tab to be active (matching the keyboard/footer gating).
- **Amend/commit description lines now align in one column.** The wrapped/extra
  lines of a multi-line commit description started two columns to the left of
  the first line; every value line now begins in the same column as the field's
  first line.
- **Opening a reflog entry's files now lands on the file tree.** `enter`/`l` on
  a Reflog row opened the files view on the commit-list side (the Commits feed,
  not the reflog), so `↑`/`↓` flipped unrelated feed commits instead of walking
  the entry's files, and the row-reveal tooltip was drawn over the tree. It now
  opens tree-focused with the commit-list side correctly bound to the Commits
  panel.
- **Push/pull no longer freezes when a remote needs credentials.** When a
  remote required authentication and no credential helper was configured, git
  fell back to an interactive terminal prompt (`Username for
  'https://github.com':`). Because the TUI owns the terminal in raw mode, that
  prompt blocked forever and gg appeared hung. Every git subprocess now runs
  with `GIT_TERMINAL_PROMPT=0`, so git fails fast instead of prompting, and the
  status bar shows *"remote needs credentials — configure a git credential
  helper"*. Credential helpers and ssh-agent are unaffected. (This also makes
  the scriptable `gg` CLI fail fast rather than hang; configure a credential
  helper for non-interactive auth.) Note: SSH remotes can still hang on a
  first-time host-key or passphrase prompt — keep the host known and the key
  loaded in an agent.
- **Diffing a file with a non-ASCII name no longer fails.** A committed file
  whose path contains a non-ASCII byte (e.g. an em-dash `—`) was listed with
  git's quoted, octal-escaped form (`"timing \342\200\224 kopia.log"`), so the
  follow-up `git show <rev>:<path>` failed with `fatal: path … does not exist`
  (exit 128) and the diff/preview never opened. Every path producer that feeds
  `git show` is fixed: commit-file listing (`CommitFiles`) and whole-tree
  comparison (`DiffTreeFiles`) now use `-z` (NUL-separated, unquoted), and the
  file-history view (`FileLog`) uses `core.quotepath=false` — so paths arrive as
  raw UTF-8 throughout, matching the rest of the codebase's path-safe verbs.
- **The history/blame/diff `.` menu no longer shows Commits-panel actions.** When
  the file-history, blame, or diff view was opened from the files view's
  commit-list side, the files view stayed live underneath and the whole Commits
  action set (cherry-pick / revert / reset / create worktree·branch·tag / the
  graph pan·widen controls — plus *View file* / *Open in external editor* on the
  tree side) leaked into its `.` menu, acting on the hidden files view rather than
  the file you were looking at. These surfaces now show only their own file
  actions: copy path/name, bookmark / shelf / compare (which already targeted the
  right file), and — for history/blame — *Open in external editor* (also making
  the editor action reachable from the menu, not just the `e` key).
- **Closing the file-content preview returns focus to the file tree.** In the
  full-tree *View file* preview, `esc` closed the preview but left focus on the
  commit-list side (the preview had taken focus on open) — so the cursor was no
  longer on the tree that launched the action. `esc` now restores tree focus.
- **Clipboard copy now works in tmux and WSL.** `.`-menu copy actions
  previously emitted only the tmux-passthrough-wrapped OSC 52 escape, which
  tmux silently drops unless `allow-passthrough on` is set — so nothing reached
  the clipboard under tmux (notably tmux on WSL). gg now prefers a native OS
  clipboard command — `clip.exe` on WSL/Windows, `pbcopy` on macOS,
  `wl-copy`/`xclip`/`xsel` on Linux — and falls back to the OSC 52 escape for
  remote/SSH sessions (where it correctly reaches the local terminal) or when no
  native command is available. WSL is detected via the kernel osrelease (immune
  to tmux env propagation) so `clip.exe` wins even when WSLg also exposes
  `wl-copy`.
- **File-content preview no longer leaks a stray commit tooltip.** While the
  full-tree *View file* preview owned the right column, the hidden Commits panel
  behind it still surfaced its selected row's reveal — for a file whose origin
  commit had a long subject, that wide tooltip shifted left and landed over the
  file tree. The reveal is now suppressed whenever the preview owns the right
  column (it's a content pager with no truncated-row reveal of its own).
- **Diff view: the `change X/N` counter now reaches the last change, and `n`/`p`
  wrap from it.** When the final change(s) sat in the last screenful — or the
  whole diff was shorter than the viewport — they couldn't be scrolled to the
  top, so the counter capped below `N/N` and `n`/`p` never registered the
  boundary (the wrap-around never armed). The focused change is now tracked
  explicitly instead of being inferred from the scroll position, so every change
  is reachable and counted regardless of where it falls; free-scrolling still
  resyncs the counter to the change under the viewport.
- **Comparing against the working tree now shows untracked (new) files.** A
  brand-new, never-added file was missing from *Compare against working tree*,
  the `◇ Working tree` diff, and any compare whose newer side is the working tree
  (also `gg compare … @worktree`), because `git diff` omits untracked files. They
  now appear as added (`A`) entries — paths with spaces or non-ASCII bytes
  included (listed via `ls-files -z`).
- **The file-content preview now scrolls like a pager.** In the full-tree
  *View file* preview, `↑/↓`/`pgup`/`pgdn` (and the mouse wheel) moved an
  invisible cursor that the viewport only followed once it reached the middle of
  the window — so the first half-screen of presses appeared to do nothing. The
  preview now scrolls the viewport on every press (the top line moves
  immediately), and wheeling over an open preview scrolls it instead of reloading
  a commit underneath it.
- **A whole-tree compare no longer gets discarded by an arrow key.** The compare
  files view (*Compare against working tree / staged*, *Compare with marked*, the
  WIP-row diff) opened with the commit list focused, so pressing up/down moved the
  commit selection and reloaded a plain commit view — throwing away the
  comparison. It now opens with the file tree focused and locks out the
  (meaningless) commit-list side, so up/down navigates the compared files.
- **The bookmark (`g`) and shelf (`G`) switchers no longer cut off file paths
  or hide the `[z] mode` hint.** The popups were fixed at 56 columns, so a long
  file path (which sits at the end of each row) was truncated, and the footer —
  including the `[z] mode` toggle that cycles cutoff/wrap/scroll — was itself
  cut off, leaving the long-text handling undiscoverable. The switchers now
  widen with the terminal (up to 96 columns) and wrap the footer hint onto
  multiple lines, so paths fit and every key (incl. `[z] mode`) stays visible at
  any width.
- **A file deleted in a commit no longer offers "Add to shelf" / "Bookmark this
  file" / compare actions in the commit files view.** A `D` (deleted) entry has
  no content at that commit, so those actions could only fail; they're now
  withheld for deleted files (the deletion is still viewable with `enter`).
- **Comparing a commit against the working tree (or staged) no longer leaves the
  file diff stuck on "(loading…)".** Opening a file from a *Compare against
  working tree* / *Compare against staged* view showed a correct header (the
  change counts and row range) but a body frozen on "(loading…)" forever. The
  compare path reuses the pre-built loading diff view, and nothing cleared its
  `loading` flag when the result landed (the plain commit-diff path only worked
  because it builds a fresh view). The diff-result handler now clears `loading`
  for every path — a result message means the load completed.
- **The commit files view (`l`) no longer hides the commit's context menu and
  graph keys.** Opening a commit's files with `l` left the commit still
  selected, but the `.` menu collapsed to copy-only and the commit-graph window
  keys (`<` / `>` / `=` / `shift+←/→`) stopped working. While focused on the
  commit-list side, the `.` menu now offers the full Commits-panel actions
  (cherry-pick, revert, reset, create branch/worktree/tag, compare, scope
  toggles, …) and the graph keys behave exactly as in the Commits panel. The
  file-tree side stays file-scoped (copy path/name, history, blame).
- **Creating a worktree now refreshes the Branches and Worktrees panels
  immediately.** After creating a worktree (and branch) the panels still showed
  the old lists until a manual reload (`r`), which on a huge repo took ages
  because it re-walked `git status` over the whole working tree and reloaded the
  commit feed — neither of which a worktree-create changes. The create now does
  a targeted refresh of just the branches and worktrees, so the new rows appear
  right away.
- **Commits panel navigation is no longer crippled on large repositories.** Each
  frame was rebuilding the entire loaded commit feed — styling every row and,
  worse, recomputing the identity-column width once per row inside the decorator
  loop, making rendering O(commits²) per keystroke (≈1.9 s/frame at 5 000 loaded
  commits, ≈28 s at 20 000). Holding an arrow or Page-Up then queued a backlog
  the UI ground through long after the key was released, and widening the graph
  made it worse. Rendering now styles only the rows actually visible
  (window-then-style), computes the column width once, and builds the
  tooltip/filter strings per row on demand instead of for the whole feed — the
  per-frame cost is ~1.5 ms at 5 000 commits and flat as the feed grows. The
  commit files view (`l`) also stops issuing a file-list read for every held
  `j`/`k`; it loads where navigation settles.
- **Switching to a branch whose name collides with a tag no longer fails.**
  After creating a worktree (and branch) at a tag, the branch and tag share a
  name, and git's `%(refname:short)` disambiguated the branch to
  `heads/<name>` — which is not a valid `git switch` argument and never matched
  the worktree, so pressing `s` skipped the "go to worktree?" prompt and died
  with `fatal: a branch is expected, got 'refs/heads/<name>'`. The branch,
  tag, and remote listings now use `%(refname:lstrip=2)`, which yields the bare
  unambiguous name regardless of collisions, restoring both the switch and the
  worktree prompt. A real-git test creates a branch and tag of the same name
  and asserts both listings report the bare name.
- **`gg remote` and `gg checkout` are recognized by the real binary again.**
  Both subcommands had working handlers and were dispatched correctly when
  invoked in-process, but were missing from the CLI command registry that
  `gg` uses to choose between running a command and launching the TUI — so the
  installed binary printed `unknown command "remote"` / `"checkout"`. They are
  now registered. A new guard test parses every `case` arm in the CLI
  dispatcher and asserts each is in the registry, so a future command can't
  drift out of it unnoticed (the in-process tests never exercised that gate).

### Added
- **Compare a commit against your working tree or staged changes.** In the
  Commits panel, the `.` menu now offers *Compare against working tree* and
  *Compare against staged*, opening the files view as a whole-tree diff
  (commit ↔ working copy / index); `enter` on a file diffs that path. First
  slice of GitKraken-style commit comparison; closes commit-ops backlog #2b.
- **Compare two commits.** Mark a commit with `m`, move to another, and the
  Commits `.` menu offers *Compare with marked commit* — a whole-tree diff
  between the two (ordered older→newer). Stage 2 of commit comparison.
- **Compare a selection of commits.** Toggle commits into a `◉` set (Commits
  `.` menu *Add to compare selection*, or `shift+↑/↓`) and pick *Compare
  selection*: two commits show the diff between them, three or more show the
  combined diff of the range (`oldest^..newest`). Stage 4 of commit comparison.
- **`gg compare <left> [<right>]` (CLI).** Print the files that changed between
  two endpoints — a commit-ish, `@staged`, or `@worktree` (right defaults to the
  working tree), one `<status>\t<path>` line each. Stage 5 (final) of commit
  comparison.
- **Maximize a left-column panel (`t`).** Pressing `t` while a small
  left-column panel is focused — the Branches/Remotes/Worktrees tab slot, the
  Files/Tags slot, or Staged — grows that panel to fill the whole left column
  height, hiding the other two, so more of its content is visible; `t` again
  restores the normal split. The pin is sticky (like `z`). While maximized,
  `tab`/`←`/`→` move only between the pinned panel and Commits, and `ctrl+←/→`
  still cycles within the pinned slot's own tab group, re-pinning the new tab.
  `t` on the Commits panel, on a narrow terminal, or while the files view is
  open is a no-op.
- **Move or drop a commit from the Commits panel.** The `.` action menu on a
  non-merge commit of the **checked-out** branch now offers **Move commit up**
  (one step newer), **Move commit down** (one step older), and **Drop commit** —
  each a one-step interactive rebase performed immediately (no editor). The
  rebase bases onto the commit's parent (`sha~1`) or grandparent (`sha~2`),
  reuses the existing dirty-worktree stash, and pauses on a conflict for
  `git rebase --continue`. `engine.InteractiveRebase` now also accepts a
  commit-ish (not just a branch) as its rebase base.
- **Rename or delete a branch from its tip commit.** When the selected
  Commits-panel commit is the tip of one or more local branches, the `.` action
  menu now offers **Rename branch ‹name›** (opens the rename dialog) and
  **Delete branch ‹name›** (with the usual confirm + force-delete prompt) for
  each — bringing branch rename/delete to the commit graph, not just the
  Branches panel. Delete is hidden for any branch that is checked out (the
  current branch or one in another worktree), since git refuses those.
- **Windowed commit graph for deep histories.** The Commits panel now renders a
  fixed-width horizontal *window* of the commit graph (default 8 lanes,
  configurable via `[ui] commit_graph_lanes`) instead of the full lane plane, so
  a repo with a deep merge history (e.g. the Linux kernel, ~300 concurrent
  lanes) no longer pushes the commit text off-screen. A `⋯` marker shows lanes
  beyond each edge of the window. Controls (Commits panel): `<`/`>` narrow/widen,
  `shift+←/→` pan, `=` snap to the selected commit's node — all also in the `.`
  menu. Tunables: `[ui] commit_graph_min_lanes`, `commit_graph_step`,
  `commit_graph_pan_step`, `commit_graph_max_lanes` (clamped to a 320-lane
  ceiling).
- **Edit a file in your editor from the Files panel.** The `.` action menu on a
  Files-panel file now offers **Edit in editor** — it suspends the TUI, opens
  the file in `$VISUAL`/`$EDITOR` (falling back to `vi` on unix / `notepad` on
  Windows), and refreshes the working-tree status when the editor exits. Works
  on any working-tree file (modified, untracked, or conflicted).
- **Add untracked files to `.gitignore` from the Files panel.** The `.` action
  menu on an untracked file now offers **Add to .gitignore** (ignores that exact
  file via an anchored, glob-escaped `/path` line) and **Add extension to
  .gitignore** (`*.ext`, offered only when the file has an extension). Both
  append to the repo-root `.gitignore`, skip a pattern that is already present,
  and leave the change unstaged; the now-ignored file drops out of the panel on
  refresh. The actions appear only on untracked files, since `.gitignore` has no
  effect on tracked ones.

### Changed
- **Friendly startup errors.** Launching `gg` outside a git repository (or with
  `git` missing from `PATH`, or a repo git refuses for "dubious ownership") now
  prints a short, human-readable message — e.g. "this folder is not a git
  repository. Run gg from inside a git repository, or create one here with `git
  init`." — instead of the raw `error: git status failed (exit 128): fatal: …`
  dump. A pre-flight check catches it before the TUI even launches; other git
  failures fall back to git's own message with the runner noise stripped.
- **Commits panel — the branch column fits the longest name now.** The
  branch-identity column sizes to the widest branch label currently loaded
  (capped at 16 chars) instead of a fixed 16, so a short common name like
  `master` no longer leaves a padding gap before the subject; subjects still
  align within the feed, and a longer name paging in grows the column up to the
  cap.
- **The Commits-row reveal shows just the text now.** The inline reveal for a
  truncated commit row drops the graph lanes and the fixed-width identity
  padding, showing only the readable `branch  subject` — the graph is positional,
  so revealing its glyphs in a horizontal strip was meaningless, and the padding
  left an ugly gap between a short branch name and the subject.
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
- **Push a tag.** The `.` menu on the **Tags** tab offers **Push tag** — it pushes
  the tag to a remote, choosing the only configured remote automatically or
  asking which one when there are several. Also on the CLI: `gg tag push <name>
  [<remote>]`. This completes tag support (view, create, delete, checkout, push).
- **Check out a tag.** The `.` menu on the **Tags** tab offers **Check out tag** —
  it asks whether to land on a **detached HEAD** at the tag's commit, create a
  **new branch** at the tag and switch to it, or create a **new worktree** at the
  tag. The branch name (and the worktree directory) are **prefilled from the tag
  name** — the directory is sanitized into a single, OS-safe path segment
  (slashes/reserved characters/Windows device names handled), so worktree paths
  are now safer cross-platform generally. Also on the CLI: `gg tag checkout
  [--branch <name>] <tag>` (the worktree option is TUI-only). Dirty-tree handling
  is git's native `switch` behavior (it carries non-conflicting changes, else
  refuses), not an autostash.
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
