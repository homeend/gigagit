# Web ↔ TUI parity

What the browser frontend (`gg web`) can do compared with the TUI, and what it
still cannot. Snapshot refreshed **2026-10-23** against `main` `305c3570`
(workspaces and both probe rounds merged). The previous snapshot (2026-08-15)
is superseded: almost every gap it listed is closed — see *Closed since the
2026-08-15 snapshot* below.

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
| AI launch dialog | every AI action opens one dialog: agent (←/→), mode interactive-background / -foreground / headless (↑/↓), queue verdicts, `[tasks] max_parallel`; an interactive agent's `$GG_MESSAGE_FILE` is applied | DIFFERENT: review, conflict-complete and commit message run as one headless op each through `startRun`, holding the op lane, and never enter `domain.Tasks()` (so they never show in the AI tasks tab) | `Tasks().Submit(TaskSpec{Mode})`, `Tasks().Load` | M–L |
| Agent / screen walks | `alt+a`/`alt+t` walk agents/terminals, `alt+A`/`alt+T` the shown worktree's; `alt+q`/`alt+Q` walk one worktree's screens with windows set aside | ABSENT: consoles are reached from the `ctrl+\` list or a sidebar sub-row | `Sessions().List()` | M |
| Console scrollback + mouse | wheel / `alt+pgup` scrollback; mouse passed to programs that ask (fullscreen Claude Code, mc); `alt+f` docked/maximised, `alt+b` keys-to-session | WEAKER: no scrollback, a click only focuses; maximize + step-out exist | `agentsession` History/Extent | M |
| Worktree claims + reservations | `.` Reserve / Unreserve (⊘), Release claim (⚑ agent, asks first); the bar says since when and why | ABSENT: no markers, no rows | `ClaimWorktree`, `ReleaseWorktree`, `SetWorktreeReserved`, `WorktreeInfo.Claim()` | S–M |
| Agent channel in standalone `gg web` | the TUI hosts `AgentHost`, so its agents can spawn workers, `agent_wait`, report | DIFFERENT: a hosted page uses the TUI's channel; a standalone `gg web` starts sessions with no `GG_MCP_URL`, so gg-delegate cannot run from it | `domain.SpawnAgent` + the `internal/mcp` host | M–L |
| Quit guard | quitting with live sessions asks first, then kills tasks, then sessions, within a grace | ABSENT: shutdown only closes the event streams (what happens to children is unchecked) | `Tasks().KillAll`, `Sessions().KillAll` | S |
| Conflicts: external tool + agent resolve | conflict process `[t] tools` (Meld, Claude, Junie) then "The tool changed X"; "Resolve with an agent…" and "Resolve & complete with an agent…" through the launch dialog | WEAKER: only the headless "AI resolve" (`conflict_complete`) | exttool conflict categories, `Tasks().Submit` | M |
| External tools: add / edit | Settings → External tools wizard writes `[[tools.command]]` blocks | WEAKER: read-only inventory; only answers a template-update offer | exttool catalog, config writers | M |
| "Overviews & notes" tree: stored overviews | lists stored review overviews + commit/branch permanent overviews (`@ <sha>` files) | WEAKER: agent session overviews only (intended in the CHANGELOG) | notes overview counts + `agentdocs` | S–M |
| Related-option prompts | one follow-up when a related Settings toggle flips (graph off → plain sort) | DIFFERENT: no prompt registry; the `web_graph_off_suggest` notice partly stands in | `promptstate` + a registry | S |

### Commits, marks and compare

| Feature | TUI | Web | Domain | Size |
|---|---|---|---|---|
| ◇ Working tree / ◇ Staged feed rows | dirty-tree rows on top of the graph, markable into the compare set | ABSENT | `CompareFiles` with a working-tree / index `model.Endpoint` | M |
| Commit vs working tree / staged | whole-tree diff of a commit against the working copy or index | ABSENT | `CompareFiles(Endpoint)` | S (after ◇) |
| One file vs working dir / copy to working dir | on a commit / stash / staged file: diff against the working-tree path, or write it there | ABSENT (`/api/entry-diff` already takes a worktree side) | `/api/entry-diff`, restore op | S |
| Multi-branch "Add to commit view" | `.` Add / Remove from commit view, Show all branches; the feed scoped to a set | ABSENT: solo stores ONE ref (territory dots colour only that) | `LogScope` with several refs | M |
| `@` highlight search in Commits | dims non-matches, ctrl+↑/↓ steps, works with ctrl+f | ABSENT: only the `/` filter | client only | S |
| Search recall | alt+↑/↓ in `/`, `@`, in-view search, finders | ABSENT | `RecordSearch` / `SearchHistoryAll` | S |
| `a` all files in a commit's files view | the commit's whole tree vs only changed files | ABSENT | `TreeFiles` | M |
| File history paging | streams newest first; ↓ past the end loads 200 older | WEAKER: one fetch of up to 200 | `FileLog` + skip | S |
| In-view search in the conflict picker | `/ @ ] [` inside the hunk picker | ABSENT: the diff search turns itself off while the picker is up | client (`inviewsearch.js`) | S |

### Branches, tags, worktrees, stash

| Feature | TUI | Web | Domain | Size |
|---|---|---|---|---|
| Worktree create options | path from the config template; `e` new branch name; `p` prefix seed; `W` create & switch | WEAKER: one path prompt, path guessed client-side as `<main>-<branch>` | template resolve + `CreateWorktree` | M |
| Tag → new worktree | Check out tag: detached / new branch / new worktree | WEAKER: no worktree option | `CreateWorktree` at the tag | S |
| Tags "Refresh remote status" | one-shot ls-remote that updates ▲ | ABSENT as a row (the interval lane and ▲ exist) | remote-tags list | S |
| Branch → its worktree (ctrl+s) | jump to the worktree the branch is checked out in | ABSENT on branch rows | re-root | S |
| Stash window extras | `/` filter, copy stash ref | ABSENT | client only | S |
| Find current (`f`) | Branches / Worktrees jump to the checked-out row | ABSENT | client only | S |
| Shelve N marked files as one set | "Add N marked files to shelf…" names one set | ABSENT: shelf add takes one path or one sha (browsing a set works) | `ShelfAddFiles(addrs, label)` | S–M |

### Settings and editing

| Feature | TUI | Web | Domain | Size |
|---|---|---|---|---|
| Theme, Theme colours… | pick a theme; live colour-role editor with defaults / reset | ABSENT | `theme.Lookup` / `theme.Merge` | M |
| Language | UI language switch | ABSENT: the web is English only | i18n | L |
| Repo settings location | move the repo config between `.gg.toml` and machine-local | WEAKER: read-only row "move it from the TUI" | the TUI's location op | S |
| Settings filter | type to filter the Settings list | ABSENT | client only | S |
| Open in external editor | `e` / `I` / Edit in editor / staged version / history `e` / blame `e`; bookmark/shelf `e` read-only copy | ABSENT: no editor launch | a server-side (loopback) editor launch | S–M |
| Branch prefixes: copy + cheat sheet | `y` copies a template; ctrl+d token / date-format cheat sheet | ABSENT: a one-line token list | `/api/prefixes` + `copyText` | S |
| Text templates: token help | ctrl+d cheat sheet with live examples; alt+x from over a console | WEAKER: an inline footnote; palette only | `TextTemplates`, `/api/text-templates/render` | S |
| Bookmark / shelf `y` | copy path, absolute path or name | WEAKER: copy path only | client only | S |

### Repos, pull requests, links, shell

| Feature | TUI | Web | Domain | Size |
|---|---|---|---|---|
| **Workspaces** | several repos open at once: the Workspaces… popup, launch question, Branches / Worktrees grouped per member, walks across members, Leave workspace | ABSENT **by design** (spec 2026-10-22 §non-goals: the hosted page follows the terminal's repository). A hosted page follows member switches both ways; it never shows the grouped lists or the R Workspaces group | `internal/workspace`, `domain.WorkspacesPath` | L if wanted |
| Repo switcher: forget, copy path | R: ctrl+d forgets, ctrl+p copies the path, Workspaces group on top | WEAKER: neither (group-by-project + slow-fs details are web-only) | `repos.Remove` | S |
| `#` prompt link history | `# ↓` picks a copied `gg://` link, newest first | WEAKER: plain prompt (the picker exists only in Compare with link…) | `GET /api/linkhist` | S |
| PR details overlay | `i`: `/` filter, `L` copy gg link, `r` reload, `s`/`v` Send to GitHub… | WEAKER: read-only, read once; copy URL only | `/api/pr/details`, `/api/pr/link`, `openSendPanel` | S |
| "List notes…" in a diff | `.` lists every thread of the open diff, jumps to one | ABSENT (UNSURE: View all notes covers part) | notes of the diff | S |
| Notification centre coverage | every notice in one `!` centre, blink, "Not now" (session) and "Never" | WEAKER: the bell holds 3 notices, only "Never", no unread / blink, refreshed only after an op; the rest live on their own surfaces (locks, drift, interrupted sends, tool offers). `featureDisabledNotices` and the steer switch ask are not covered — a command for another worktree is refused, where the TUI asks whether to switch | `RegisterNotice` per notice | S each |
| Operation log in standalone `gg web` | `[debug] log_operations` writes every op + git span | ABSENT: `gg web` installs only the failure sink (a hosted page is logged by its TUI) | `observ.SetSpanSink` | S |
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

- **Marked commits** (merged 6d1976d1): drop N, reorder & squash, cherry-pick
  the marked commits, compare a 3+ range, shift+click / shift+↑↓ mark a run,
  save 2 to Previews (and reversed), review the marked range (AI), rename /
  delete a branch from its tip commit.
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
`/` either. The hunk-picker search does not apply — the web has no picker.)

## Neither frontend

Sparse-checkout (roadmapped alongside the remaining MCP surface).

## Working on this

Pick a surface from the tables, port it by reading the TUI surface (the
`driving-tui-headless` skill), one `feat/` branch per surface. Update this
document after the merge, not from the feature branch, so the snapshot never
appears in a merge conflict.
