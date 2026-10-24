# Web ↔ TUI parity

What the browser frontend (`gg web`) can do compared with the TUI, and what it
still cannot. Snapshot refreshed **2026-10-23** against `main` `305c3570`
(workspaces and both probe rounds merged). The previous snapshot (2026-08-15)
is superseded: almost every gap it listed is closed — see *Closed since the
2026-08-15 snapshot* below. Updated **2026-10-24** against `main` `5834f3bc`
after the F1, F2 and F3 merges, against `572ab7a0` after E, and against `0d9e900b` after G.

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
| Agent / screen walks | `alt+a`/`alt+t` walk agents/terminals, `alt+A`/`alt+T` the shown worktree's; `alt+q`/`alt+Q` walk one worktree's screens with windows set aside | ABSENT: consoles are reached from the `ctrl+\` list or a sidebar sub-row | `Sessions().List()` | M |
| Console scrollback + mouse | wheel / `alt+pgup` scrollback; mouse passed to programs that ask (fullscreen Claude Code, mc); `alt+f` docked/maximised, `alt+b` keys-to-session | WEAKER: no scrollback, a click only focuses; maximize + step-out exist | `agentsession` History/Extent | M |
| Agent channel in standalone `gg web` | the TUI hosts `AgentHost`, so its agents can spawn workers, `agent_wait`, report | DIFFERENT: a hosted page uses the TUI's channel; a standalone `gg web` starts sessions with no `GG_MCP_URL`, so gg-delegate cannot run from it | `domain.SpawnAgent` + the `internal/mcp` host | M–L |
| External tools: add / edit | Settings → External tools wizard writes `[[tools.command]]` blocks | WEAKER: read-only inventory; only answers a template-update offer | exttool catalog, config writers | M |
| "Overviews & notes" tree: stored overviews | lists stored review overviews + commit/branch permanent overviews (`@ <sha>` files) | WEAKER: agent session overviews only (intended in the CHANGELOG) | notes overview counts + `agentdocs` | S–M |
| Related-option prompts | one follow-up when a related Settings toggle flips (graph off → plain sort) | DIFFERENT: no prompt registry; the `web_graph_off_suggest` notice partly stands in | `promptstate` + a registry | S |

### Commits, marks and compare

| Feature | TUI | Web | Domain | Size |
|---|---|---|---|---|
| ◇ Staged feed row | the TUI has ◇ Working tree AND ◇ Staged rows, both markable | DIFFERENT: one working-tree row (markable); commit ↔ staged only from the commit menu, no staged mark | `CompareFiles(IndexEndpoint)` | S |
| Multi-branch "Add to commit view" | `.` Add / Remove from commit view, Show all branches; the feed scoped to a set | ABSENT: solo stores ONE ref (territory dots colour only that) | `LogScope` with several refs | M |
| `a` all files in a commit's files view | the commit's whole tree vs only changed files | ABSENT | `TreeFiles` | M |

### Branches, tags, worktrees, stash

| Feature | TUI | Web | Domain | Size |
|---|---|---|---|---|
| Stash window filter | `/` filter | ABSENT (copy stash ref exists) | client only | S |
| Find current (`f`) | Branches / Worktrees jump to the checked-out row | WEAKER: only the palette's go to current worktree | client only | S |
| Shelve N marked files as one set | "Add N marked files to shelf…" names one set | ABSENT: shelf add takes one path or one sha (browsing a set works) | `ShelfAddFiles(addrs, label)` | S–M |

### Settings and editing

| Feature | TUI | Web | Domain | Size |
|---|---|---|---|---|
| Theme, Theme colours… | pick a theme; live colour-role editor with defaults / reset | ABSENT | `theme.Lookup` / `theme.Merge` | M |
| Language | UI language switch | ABSENT: the web is English only | i18n | L |
| Repo settings location | move the repo config between `.gg.toml` and machine-local | WEAKER: read-only row "move it from the TUI" | the TUI's location op | S |
| Settings filter | type to filter the Settings list | ABSENT | client only | S |
| Open in external editor | `e` / `I` / Edit in editor / staged version / history `e` / blame `e`; bookmark/shelf `e` read-only copy | ABSENT: no editor launch | a server-side (loopback) editor launch | S–M |
| Branch prefixes: cheat sheet | ctrl+d token / date-format cheat sheet | ABSENT: a one-line token list (copy exists) | `/api/prefixes` | S |
| Text templates: token help | ctrl+d cheat sheet with live examples; alt+x from over a console | WEAKER: an inline footnote; palette only | `TextTemplates`, `/api/text-templates/render` | S |

### Repos, pull requests, links, shell

| Feature | TUI | Web | Domain | Size |
|---|---|---|---|---|
| **Workspaces** | several repos open at once: the Workspaces… popup, launch question, Branches / Worktrees grouped per member, walks across members, Leave workspace | ABSENT **by design** (spec 2026-10-22 §non-goals: the hosted page follows the terminal's repository). A hosted page follows member switches both ways; it never shows the grouped lists or the R Workspaces group | `internal/workspace`, `domain.WorkspacesPath` | L if wanted |
| "List notes…" in a diff | `.` lists every thread of the open diff, jumps to one | ABSENT (UNSURE: View all notes covers part) | notes of the diff | S |
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
  overlay is one inventory across all categories and frontends.
- Repo picker: group-by-project, slow-fs and age columns.
- The interrupted-send bar right in the PR section; an overview's HTML export
  downloads straight to the browser.

## Closed since the 2026-10-23 refresh

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
permanent overviews (open, export, delete), note meta.

(The old doc's "file-history search" gap was wrong: the TUI history view has no
`/` either.)

## Neither frontend

Sparse-checkout (roadmapped alongside the remaining MCP surface).

## Working on this

Pick a surface from the tables, port it by reading the TUI surface (the
`driving-tui-headless` skill), one `feat/` branch per surface. Update this
document after the merge, not from the feature branch, so the snapshot never
appears in a merge conflict.
