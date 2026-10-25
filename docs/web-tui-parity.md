# Web ↔ TUI parity

What the browser frontend (`gg web`) can do compared with the TUI, and what it
still cannot. Snapshot refreshed **2026-10-23** against `main` `305c3570`
(workspaces and both probe rounds merged). The previous snapshot (2026-08-15)
is superseded: almost every gap it listed is closed — see *Closed since the
2026-08-15 snapshot* below. Updated **2026-10-24** against `main` `5834f3bc`
after the F1, F2 and F3 merges, against `572ab7a0` after E, against `0d9e900b` after G, against `827a5e35` after F4, against `b8d87e03` after D, and against `f611c530` after H (multi-branch commit view, all files).

**How this was derived** (repeat it to refresh the list rather than trusting the
prose). The `engine.X{}` operations each frontend constructs are the ground
truth for *what can be run*:

```bash
comm -3 <(grep -rhoE "engine\.[A-Z][A-Za-z]+\{" internal/tui/*.go --exclude='*_test.go' | sort -u) \
        <(grep -rhoE "engine\.[A-Z][A-Za-z]+\{" internal/web/*.go --exclude='*_test.go' | sort -u)
```

On 2026-10-23 that leaves no runnable op unported (only `ExportFile`, which the
web reaches as a browser download, and the non-op types `DecisionRequest` /
`Msg`). So the remaining gap is in **surfaces**: menu rows, keys, popups and
domain queries the web never calls. Those were found by walking the TUI's `?`
help (`internal/tui/help.go`), the palette and the CHANGELOG, and grepping
`internal/web` (Go routes + `static/*.js`) for each counterpart.

**Blind spot of that method — side effects on screen.** Both steps find what
a key or a menu row *runs*. Neither finds what the TUI *shows* as a side
effect of it, when the help text does not say so: the cursor landing on a
row, the tab that comes forward, a mark, a title, a status line. Example,
found 2026-10-25 by the user, not by this method: every agent walk
(`alt+a`/`alt+t`/`alt+A`/`alt+T`, `internal/tui/console.go` →
`selectSessionRow`) and every `alt+q` stop (`worktree_walk.go`) puts the
Branches cursor on the shown session's `└ ●` row (or the worktree's branch)
and brings the Branches tab forward — that is how the TUI tells you WHICH
agent is on screen. The `alt+a` help never says it, the stage-D spec never
listed it, and the web, whose sidebar has no cursor, silently dropped it.

So a refresh, and every stage's spec, adds a **screen-effect pass**: for each
key or row being ported, capture the TUI before and after
(`./tui-capture.sh`) and list EVERY panel that changed (cursor row, shown
tab, marks, titles, status line, footer). Each change the web does not
reproduce is either ported, or written down here as a row or as a
"Design differences" line — a "not applicable in a browser" is a decision
recorded in this file, never a silent drop. Grep the TUI handler for
`select*Row` / `activateTab` / `statusMsg` calls too: those are the
side effects help text tends to leave out.

Sizes: **S** < 1 day · **M** 1–3 days · **L** more.

## Shared surface

Pull · push · force-push · smart switch · merge · rebase · fast-forward ·
interactive rebase · branch create/rename/delete · remote checkout (stay or
switch) · browse & checkout unfetched remote branches (per-branch fetch
mapping) · prune · delete remote branch · worktree create + remove · tags
(create, annotate, push, delete, delete from remote) · stash create/apply/pop/
drop · commit · staging incl. the region/line hunk picker · discard ·
`.gitignore` rows · reset (incl. reset-to-remote-tip) · conflict resolution
through the hunk picker · AI conflict-complete · diff (incl. the `f` changed-lines-only fold) · file history · blame ·
compare two branches · solo scope · branch versions · AI review · identity &
profiles · branch prefixes · external tools · session errors · repo switcher ·
refresh settings · live refresh (file watch + intervals) · commit graph · command palette · cherry-pick · revert ·
reword · undo last commit · worktree from a commit · fast-forward to a commit ·
reset to a commit · annotated tags from a commit · review one commit (AI) ·
**bookmarks and the shelf** (list, add a file or commit, name, remove, open,
restore to a path, restore one file from a shelved commit, cherry-pick a
shelved commit — live or from its frozen patch) · **merge previews** (saved
`source → target` branch-name pairs, previewed as the merge-base..source
diff rather than a tip-to-tip compare: list, add, rename, remove, save the
reversed pair, open, and live re-open when a tracked tip moves) · **commit
pairs and saved comparisons** in the same Previews section (list, open, rename,
remove, save reversed, copy gg link — either half of a comparison; a commit
pair carries its **review notes**: the ◆N total on its row, badges, note rows,
`c`/`E`/`R` on the new side, the line copy row as a `@a..b` link; its files'
right-click menu has history / blame / bookmark / shelf at `b`) ·
**compare with link…** (two `gg://` links through `domain.CompareLinks`: the
copied-link history, side swap, per-side errors, the base row, save
comparison…; a `@a..b` link lands through the same door; **web only:** the
⇄ symmetric view of two sets — two row-aligned file lists around one diff, a
flippable direction arrow, differ / all / one side / same filters; and drag a
Previews row onto another to start the comparison — the TUI's twin is space /
`m` marks on two Previews rows; and the **stacked diff** (`S`) — every file
of a change set in one scroll, header + diff per file, the symmetric
comparison included — which the TUI now has too, with its own keys: `N`/`P`
step file to file where the web uses `j`/`k`, `n`/`p` step change to change
across the whole stack as one document where the web uses `.`/`,` (or the
‹ change / change › buttons) — both step from the viewport after a free
scroll and open a folded or not-yet-loaded file on the way, the web
without the TUI's wrap — home/end are the whole stack's
ends, `J` lists the files, and `-`/`_` fold, as on the web; review notes work per file
inside a stack in BOTH: `c`/`E`/`R` act on the file under the cursor (the
clicked row, on the web), `}`/`{` walk note to note across the whole stack
(unfolding and loading a file to reach one), and a link or a `gg session
navigate` with a line lands on that LINE rather than the file header —
**TUI only:** `ctrl+↑`/`↓` step change to change inside one stacked file) · **pull
requests** (list, fetch + open the PR diff, copy URL, forget,
`[refresh] prs` poll and ⟳, review threads inside the diff as read-only note
boxes, the details overlay — the TUI's PR hub — the comment re-poll, and the
any-state **search** — the TUI's `A` — as a field + state chip in the section;
PR text is rendered as markdown in both — the web paints the parsed tree
as HTML, the TUI lays it out in rows) ·
**note folding** (every note box: click the title, `z` / `Z` — the TUI's
`o` / `O`).

## Missing from the web

### Agents and AI tasks

| Feature | TUI | Web | Domain | Size |
|---|---|---|---|---|
| Agent channel in standalone `gg web` | the TUI hosts `AgentHost`, so its agents can spawn workers, `agent_wait`, report | DIFFERENT: a hosted page uses the TUI's channel; a standalone `gg web` starts sessions with no `GG_MCP_URL`, so gg-delegate cannot run from it | `domain.SpawnAgent` + the `internal/mcp` host | M–L |
| "Overviews & notes" tree: stored overviews | lists stored review overviews + commit/branch permanent overviews (`@ <sha>` files) | WEAKER: agent session overviews only (intended in the CHANGELOG) | notes overview counts + `agentdocs` | S–M |
| Related-option prompts | one follow-up when a related Settings toggle flips (graph off → plain sort) | DIFFERENT: no prompt registry; the `web_graph_off_suggest` notice partly stands in | `promptstate` + a registry | S |

### Branches, tags, worktrees, stash

| Feature | TUI | Web | Domain | Size |
|---|---|---|---|---|
| Shelve N marked files as one set | "Add N marked files to shelf…" names one set | ABSENT: shelf add takes one path or one sha (browsing a set works) | `ShelfAddFiles(addrs, label)` | S–M |

### Settings and editing

| Feature | TUI | Web | Domain | Size |
|---|---|---|---|---|
| Theme, Theme colours… | pick a theme; live colour-role editor with defaults / reset | ABSENT | `theme.Lookup` / `theme.Merge` | M |
| Language | UI language switch | ABSENT: the web is English only | i18n | L |

### Repos, pull requests, links, shell

| Feature | TUI | Web | Domain | Size |
|---|---|---|---|---|
| **Workspaces** | several repos open at once: the Workspaces… popup, launch question, Branches / Worktrees grouped per member, walks across members, Leave workspace | ABSENT **by design** (spec 2026-10-22 §non-goals: the hosted page follows the terminal's repository). A hosted page follows member switches both ways; it never shows the grouped lists or the R Workspaces group | `internal/workspace`, `domain.WorkspacesPath` | L if wanted |
| State dump (alt+U) | writes a state dump + forced unlock | DIFFERENT: an op watchdog unlocks the UI; no dump to share | `domain.WriteProcessState` | S |
| Fullscreen popups | `ctrl+t` fullscreens any panel or popup; `t` maximizes a left panel | DIFFERENT: sidebar toggle, drag-resize panes, console maximize; no popup fullscreen | client only | S, low value |

### Design differences (not gaps)

- Staging: the web selects ROWS in the diff and stages them from the context
  menu (GitKraken), the TUI's `H` opens the region/line picker. Both use
  `hunkpick`; the TUI picker can also take one side of a line.
- Line select / copy: browser drag + ctrl+c, one side, no headers — the TUI's
  space / `v` one-sided cursor. No keyboard line cursor in the web diff (`z`,
  alt+←/→, Align rows).
- Commit marks: ctrl+click on the web, space / `m` in the TUI.
- Export: the web downloads a patch; the TUI writes it to a path.
- Apply patch: the web takes a typed server path, not an upload.
- Footer and help: the web has a per-surface chip stack and a searchable help;
  the TUI one key table.
- Quitting: a standalone `gg web` quits on Ctrl+C in its terminal (a tab
  closing is not a quit); the page only answers the quit question the
  server asks. The worktree claim line rides the row's tooltip, not a bar.
- All files (`a`): the web lists a commit's tree up to 20 000 files with a
  line saying where it stops (its file list is not windowed); the TUI lists
  the whole tree in a viewport.
- The commit-list scope (solo / commit view) is the web server's and
  survives a worktree switch; the TUI clears it on a re-root and keeps one
  per worktree slot.
- Walk landing mark: the web has no sidebar cursor, so the walks' landing
  (the TUI's Branches cursor after `alt+a`/`alt+t`/`alt+A`/`alt+T`/`alt+q`/
  `alt+w`) is a mark on one Branches row, drawn the same whether the console
  has the keys or not. A click on another Branches row clears it (the TUI
  cursor moves there); a console opened any other way clears a mark naming
  another session (the list is on screen beside it); a row the filter hides
  shows no mark (the TUI keeps its cursor where it was).
- External editor: the server's `$VISUAL` / `$EDITOR` runs in a page console
  (the TUI hands it the whole terminal). The page stays usable beside it,
  and the session is listed, and walked by `alt+t`, until the editor exits.
  A GUI editor that returns at once opens and closes the console, and a
  read-only copy is gone before it reads it (`code` without `--wait`; the
  TUI is the same). The bookmark and shelf switchers' `e` is a menu row on
  the sidebar entries. A shelved commit's frozen files (a link compare)
  have no editor row.
- A file shown alone (the F preview, the viewer with one file) takes half
  the width, centred (never narrower than 760px); the TUI shows it over the
  whole screen. The split, `m` in F and the TUI's ctrl+t lift the half width.
- `[ui] file_width` (default 200 characters, `-1` = unlimited) caps the
  text column of those views and of blame: the box narrows to the gutter
  plus that many characters, `m` in F lifts the half width but not the cap.
  The TUI takes the same cap (a centred column in its full-screen frame).
  A width saved in the TUI's Settings reaches an open page at its next
  repo switch or reload; one saved on a hosted page reaches the terminal
  at once.
- Overview search rows: the TUI searches the rows it wraps an overview
  into; the web searches its text blocks (a paragraph, a heading, a table
  cell, a code block), so a query may match across a wrap the TUI breaks.
  A stack header is searched as drawn (its path cut in the middle), not as
  the TUI's `━━ path  (status)` line.
- Not applicable in a browser: `r` repaint, clipboard / WSL notices, the
  Settings → Web page row.

## Web only

- Drag a Previews row onto another to compare; the ⇄ symmetric two-set view;
  "compare and save…" on link compares.
- Drag-resizable panes; server-down bar with reconnect; a "switching" overlay;
  help search; the `?classic` look.
- A console steps aside on a pane click; an agent's navigate to a review or
  overview closes its own console; activity toasts on every open tab.
- Notices the TUI has no auto-notice for: `web_graph_off_suggest`,
  `web_push_mapping_offer`; refspec repair rows on the branch menu.
- Agent setup lists the user's custom install targets; the External tools
  overlay is the TUI's wizard plus one inventory across all categories and
  frontends (no `ctrl+w` display modes: the browser wraps; a write closes
  the overlay rather than returning to a Settings menu).
- Repo picker: group-by-project, slow-fs and age columns.
- The interrupted-send bar right in the PR section; an overview's HTML export
  downloads straight to the browser.

## Closed since the 2026-10-23 refresh

- **Overview search** (feat/web-overview-search): `/ @ ] [` in an overview
  opened on its own, in the split's left column (its own bar) and in the
  stacked file column (every header and shown line across the stack, the
  cursor landing on each hit). It was absent: the stack said "this acts on
  a single file". The stack reads every unfolded file once a search opens
  (the TUI's stack reads them all anyway).

- **External tools wizard + Repo settings location (J2)**
  (feat/web-tools-wizard): the external tools overlay opens on the TUI's
  wizard — detected tool × template rows, its default checks (yolo and every
  resolve-&-complete unchecked), `(configured)` / `(update available — u)` /
  `(agent version unsupported)`, the focused row's destination + command,
  ↑/↓, space, `u`, enter → `POST /api/exttools/add` (rows by key, resolved
  against a fresh detection, appended to the global config); settings →
  repo settings location → the TUI popup (present / absent, the moves that
  apply, `Overwrite <dst> ?`) over `POST /api/settings/repo-location`. A
  hosting terminal follows both writes (`Host.SetConfigChanged`).

- **Open in external editor (J1)** (feat/web-editor): `POST /api/editor-open`
  runs the server's editor in a console session: **edit in editor** on a
  working-tree file (live, the list reloads), read-only copies for the
  staged version, a commit's file, a bookmark, a shelved file, `I` (a
  commit's message), diff `e` at the clicked line (vi `+N`, code `--goto`,
  hx `path:N`), history `e`, blame `e`; the op line says `edited` /
  `viewed`, as the TUI's status line.

- **Walk landing marker** (fix/web-walk-marker): every walk marks the
  Branches row it landed on — the shown session's `└ ●` sub-row (`alt+a` /
  `alt+t` / `alt+A` / `alt+T`, `alt+q`'s session stops, a Branches sub-row
  click) or the worktree's branch (`alt+w`, `alt+q`'s page stop); a landing
  unfolds the branches section and scrolls the row into view; the mark
  survives repaints and the console's close.

- **Multi-branch commit view and all files (H)** (merged f611c530): branch
  menu add / remove from commit view and show all branches (commit menu
  too), ◉ on member rows, the chip `solo: X ✕` / `N branches ✕`, the walk
  includes the in-scope upstreams (every local branch's when unscoped, as the
  TUI's `feedUpstreams`), territory colours over the set, review visibility
  follows the set; `a` in a commit's file list toggles the whole tree
  (`(all files)`, sticks while the list is open, a tree file diffs against
  the working tree, alone, link and go to line on the right sides).
- **Agent walks, console scrollback and mouse (D)** (merged b8d87e03):
  `alt+a` / `alt+t` / `alt+A` / `alt+T` walk the running agents and
  terminals in the Worktrees list's order, `alt+q` / `alt+Q` the viewed
  worktree's screens (the page, its agents, its terminals — the page is the
  TUI's windows and plain stops in one), even inside a focused console;
  `alt+f` maximise ↔ dock, `alt+b` keys to the session or the page; the
  wheel (`alt+pgup` focused, `pgup` unfocused) opens a frozen scrollback
  with line and drag copy; a program that asks for the mouse gets clicks,
  drags and the wheel, a full-screen one without it ↑/↓.

- **Small rows (F4)** (merged 827a5e35): the Stashes header's `/` filter
  (enter keeps, esc drops, × clears); ⌖ find current on Worktrees too, with
  the TUI's words; the Settings type-to-filter (first esc clears); the
  `ctrl+d` token / date-format sheets in the prefix and text-template forms,
  `alt+x` over an unfocused console; List notes… in a diff (a stack lists
  its loaded files); the ◇ Staged feed row (HEAD ↔ index, live, markable).
  The sidebar keys are header buttons (no sidebar cursor).

- **The create-worktree dialog** (merged 0d9e900b): every new-worktree
  entrance (branch, branch & switch, commit, tag, and a new remote-branch
  row) opens the TUI popup's dialog — path from `[worktree] path_template`
  with its `<user:…>` fields, `<random>`/`<date>` fixed while open, `<seq>`
  counters bumped after a successful create; `e` / `E` / `p` (resolved
  against the start point) / `w` / `W` / `h` / `m` / `esc`. The commit
  menu's keep rows are the dialog's `m`.

- **Worktree claims and reservations, the quit guard** (merged 572ab7a0):
  the Worktrees rows draw ⚑ <agent> / ⊘ (the bar's since-when and why in
  the tooltip); the worktree menu adds Release claim (asks first) and
  Reserve (no agents) / Unreserve after Start agent / Open terminal. A
  standalone `gg web` holds the first Ctrl+C while agents or AI tasks run:
  every tab's ctrl+\ switcher shows the TUI's quit question (Agents and AI
  tasks tabs only, Q kill all and quit, esc cancel); Q or a second Ctrl+C
  kills tasks, then sessions, within 5 s; any exit runs the safety-net
  kill. `domain.LiveWork` is the count both frontends share.

- **PR hub keys, notification centre, operation log** (merged 6d2863db): the
  PR details overlay answers `y` / `L` / `r` / `s` / `v` / `/` (block filter);
  every notice offers Not now (a server-side session, shared by every tab,
  reset by a repo switch) and Never; the bell blinks while unread; the centre
  carries the stale-lock, commit-graph, feature-disabled, tool-template and
  steer-switch notices (a navigate for another worktree asks instead of
  landing in the shown one); a lock failure points at `!`; a standalone
  `gg web` writes `operations.log`. Not ported by design: the clipboard / WSL
  notices; drift and interrupted sends keep their own panels.
- **Search parity** (merged 0d36ad75): file history pages past 200 commits,
  the `#` prompt's link history (↓ / ↑, ↑ on the first link gives back the
  draft), search recall (alt+↓ / alt+↑, the TUI's per-scope rings, shared
  with it) in the commit filter, the in-view searches and the file finder,
  the `@` highlight on the commit list (dims non-matches, ctrl+↑ / ctrl+↓
  step, type snaps the cursor), `/ @ ] [` inside the conflict picker.
- **Small list rows** (merged 8f83030d): repo switcher ctrl+p copy path /
  ctrl+d forget, branch menu go to its worktree, tag check out as a new
  worktree + refresh remote status, copy stash ref, bookmark / shelf copy
  absolute path and name, branch prefix copy, palette go to current worktree.
- **Marked commits** (merged 6d1976d1): drop N, reorder & squash, cherry-pick
  the marked commits, compare a 3+ range, shift+click / shift+↑↓ mark a run,
  save 2 to Previews (and reversed), review the marked range (AI), rename /
  delete a branch from its tip commit.
- **The working tree as a compare side** (merged 14df2d13): Compare against
  working tree / staged on a commit, the working-tree row markable into the
  compare set, Compare against working dir / Copy to working dir on a file.
- **Conflict tools** (merged 8094a263): the banner's tools… is the TUI's `t`
  picker — in-place tools (per-file mergetools on the focused file, commands
  asking for input) run in a console session and end with the TUI's
  mark-resolved question; Resolve with an agent… / Resolve & complete with an
  agent… open the launch dialog.
- **AI actions through the task scheduler** (merged 4edf19d1): review,
  commit message and AI resolve open the TUI's launch dialog (agent ×
  interactive background / foreground / headless, the wait line, the
  remembered pick, approval) and run in `domain.Tasks()`; results land in the
  commit box, the review view or the report viewer. The single-lane runs and
  their park chip are gone.
- **AI tasks tab + ◆ rows** (merged 57aadcae): the ctrl+\ AI tasks tab lists
  the history and acts (enter result / console / output, apply a commit
  message, k k cancel, x remove, s retry save, / filter, live); a worktree's
  headless tasks as ◆ rows. x in either frontend removes the row from both.

## Closed since the 2026-08-15 snapshot

`ApplyPatch` (+ mode, abort) · `ExportFile` / `ExportToDir` and the `t`
copy-to-directory lane · `MoveWorktree` (rename, move) · worktree keep-changes
modes · `RemoveGitLocks` · `AddFetchMappings` · the `PushTags` prompt ·
`GenerateMessage` (headless only — see the launch dialog) · commit amend ·
`MarkAllResolved` · whole-file keep current / incoming (+ keep / delete / base
for modify-delete) · the git config explorer · agent-skill setup · the
notification centre (surface; coverage above) · the stash per-file checklist ·
commit menu: squash (adjacent), export patch, solo from this commit, compare 2
marked commits · ctrl+f eager paging · the `\` server-side feed filter · the `F`
fuzzy file finder · in-view search in the file viewer · compare against a
bookmark or shelf entry · branch filters (five slots, alt+1…5, slot editor) ·
blame age highlight · the remote-tag ▲ marker · pull requests are no longer
read-only (Send to GitHub…, reply, resolve, the review send panel, interrupted
sends, the ✎ local-review mark) · session docs, the "Overviews & notes" tree,
permanent overviews (open, export, delete), note meta · bookmark and shelf
rows as the TUI switchers (F / C / S kind letter, the display text cut in the
middle with the file name kept, re-cut on resize; menus in the `g` / `G`
order and words, left click = enter) · worktree switches in place (no
reload: the header, marks and per-worktree commit draft at once, then only
what a worktree changes) · a console of another worktree switches the page
there and its own close switches back (the TUI's showConsoleBy /
returnView) · alt+w (the worktree ring, unreachable rows skipped and named) ·
a Windows-created worktree seen from WSL offers the repair (web and TUI).

(The old doc's "file-history search" gap was wrong: the TUI history view has no
`/` either.)

## Neither frontend

Sparse-checkout (roadmapped alongside the remaining MCP surface).

## Working on this

Pick a surface from the tables, port it by reading the TUI surface (the
`driving-tui-headless` skill), one `feat/` branch per surface. Update this
document after the merge, not from the feature branch, so the snapshot never
appears in a merge conflict.
