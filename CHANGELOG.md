# Changelog

All notable changes to gigagit (`gg`) are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
No tagged release has been cut yet; everything lives under **Unreleased**.

## [Unreleased]

### Added
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
- **Architecture-review hardening batch** (whole-codebase review, findings
  verified against the tree before fixing):
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
