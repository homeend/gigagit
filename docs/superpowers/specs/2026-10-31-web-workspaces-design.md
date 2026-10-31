# Web workspaces — Design Spec

**Date:** 2026-10-31
**Status:** Draft, for review
**Scope:** The gg web page, hosted and standalone, shows and works an open
workspace the way the TUI does (spec 2026-10-22-workspaces-design). Builds on one active
interface (spec 2026-10-31-one-active-interface-design).

---

## Intent

The user works on systems split over several repositories and now picks one
interface at a time. A workspace opened in gg must stay a workspace when the
web page is the active side: every member's worktrees, branches and sessions
in the same lists, the walks running across all of them, and one keystroke
moving between repositories without a reload. Today the page shows only the
member on screen and `alt+S` is the same as `alt+s`.

The TUI is the spec. Where this document is silent, the page does what the
TUI does on screen; where the page already differs from the TUI for one
repository (§Page differences), it keeps that difference.

## Goals

- `gg --web --workspace shop` and `gg --web` in a member repository open the
  workspace with the page active. `gg web --workspace shop` and `gg web` in a
  member repository do the same with no terminal.
- The Worktrees and Branches sections group every member under a heading.
- `alt+w`, `alt+a`, `alt+t`, `alt+q`, `alt+s` run over every member; `alt+s`
  and `alt+S` become different walks.
- A switch between members happens in place, with no reload.
- `R` lists workspaces, and the palette offers **Workspaces…** and **Leave
  workspace**, with the same edits as the TUI.
- Sleeping members' lists stay fresh while the page is active.
- Switching between the TUI and the page keeps the same open workspace.

## Non-goals

- Several repositories side by side, group sync, or any operation or compare
  across two members (as in the TUI spec).
- Changes to how workspaces work in the TUI, or to `workspaces.toml`.

## Anti-goals (a failure even if it works)

- A switch between members that reloads the page or loses the console, the
  open-files list, or a half-typed commit message.
- A question only the blocked terminal can answer: nothing may wait on a
  prompt behind the block.
- A page whose first paint waits for every member. The member on screen
  paints as it does today; the other groups arrive as they are read.
- Git calls per member on every refresh of the member on screen.
- A row action that silently runs in the wrong repository.

---

## 1. What the user sees

### Grouped lists

With a workspace open, `#worktrees-list` and `#branches-list` hold, per
member in workspace order:

- a heading row `[shop-api]` (the TUI's `headingLabel`, which disambiguates
  equal directory names), with ` (missing)` when the member's path is gone;
- that member's rows, each under its own sort and its own branch filter slot,
  with the session sub-rows and ◆ rows the page shows today.

Every group has its own head star. The branch checked out in the worktree on
screen is marked `*:`, and the star in Worktrees is that worktree too (the
TUI's rule). A sleeping member's branches have no review or overview sub-rows,
as in the TUI. Outside a workspace nothing changes, including the row gutter.

Only these two sections group. Tags, remotes, stashes, reflog, bookmarks,
shelf, commits, files and pull requests show the member on screen.

### Headings and rows of another member

- Moving the list cursor never switches, and it can rest on a heading.
- A click on a heading, or `enter` with the cursor on one, adopts into the
  member's last-viewed worktree, else its main checkout. On the member already
  on screen it does nothing. A missing member says why on the status line.
- An action on a row of another member (the TUI's `memberSwitchKey` keys,
  a click that acts, the `.` menu, the row's context menu, the branch filter
  keys `alt+1…5`) first switches to that member, landing on the worktree the
  row names, else the member's default worktree. Then it runs there. A refused
  switch stops the action and its reason stays on the status line.
- `m` with a mark in the member on screen, on a row of another member, is
  refused with `compare works within one repository`. So is a drag from a row
  of one member onto a row of another.
- Global keys act on the member on screen and never switch.

### Switching between members

A switch is an adopt, as every page switch is today: the terminal's own
worktree moves, and with it the snapshot, the steer inbox and the recents. It
goes through the same path as a worktree switch within one repository: same
veil-free status line, same commit-message draft swap, same console rule. In
addition the repository-wide lists are re-read (tags, remotes, stashes,
bookmarks, shelf, pull requests, health, notices), and commit marks and
compare marks are cleared. An open file stays in the list; a window that the
in-repository switch would close closes, and one it would keep is kept.

A switch to a path outside every member leaves the workspace, as `R` does:
the page reloads and then says `left workspace shop`, carried across the
reload the way a switch's other state is. As in the TUI, nothing asks first:
members' sessions keep running.

### Walks and the help

The rings already follow the Worktrees list, so grouping extends them:

| Key | In a workspace |
|---|---|
| `alt+w` | every member's worktrees, group by group |
| `alt+a` / `alt+t` | every member's agents / terminals |
| `alt+A` / `alt+T` | the viewed worktree's own, unchanged |
| `alt+q` / `alt+Q` | the viewed worktree's screens, unchanged |
| `alt+s` | every worktree with a running session, all members |
| `alt+S` | the same, only the member on screen |

Outside a workspace `alt+S` is `alt+s`. A session of another member shows in
its worktree, by the switch above. Worktrees marked unreachable are skipped in
every member. Missing members have no stops. The page's help overlay gets the
TUI's Workspaces section and the walk sentences, in English.

### `R`, the palette and the Workspaces window

- **`R`:** when any workspace is defined, workspaces head the list. `●` marks
  the open workspace and its members. Enter on a workspace opens it, on this
  repository when it is a member, else on its first repository. Enter on a
  member switches in place. Enter on a repository outside leaves.
- **Palette:** **Workspaces…** always, **Leave workspace** with one
  open.
- **Workspaces window:** the TUI popup's screens and keys. List: `n` new, `r`
  rename, `d` delete, `o` open now, `enter` members. Members: `a` add (the
  repositories open now, then recents, then Path…), `d` remove, `K`/`J` move,
  `s` settings repository (✎), `esc` back. Rows are clickable too, and every
  edit saves at once. Name refusals use the store's reasons. Missing members
  show `(missing)`.

### Questions the terminal would have asked

Each appears on the page while the page is active, and the terminal does not
show it:

- **Launch question:** `gg --web` or `gg web` in a member repository with no
  `--workspace` asks on the page which to open, one row per workspace plus
  `Open <repo> alone`; esc opens it alone. With no tab connected it waits for
  the first tab. The repository alone is served until it is answered. It is one
  pending question held by the owner (§2): under `gg --web` it shows on
  whichever side is active and moves with a flip. It ends when answered and is
  dropped if the repository changes. It is asked once per process, as in the TUI.
- **Running sessions in a member being dropped** (Leave workspace, removing a
  member, deleting the open workspace, opening a workspace that lacks the
  member, exactly where the TUI's `askBeforeDrop` asks): leave them running,
  stop them, or cancel. The first option is the safe one, and esc cancels.
  Cancel leaves `workspaces.toml` untouched.

---

## 2. How it fits together

The page never holds what is open: it reads and asks an owner behind the seams
below. Under `gg` and `gg --web` the owner is the terminal, which owns `m.ws`,
its member services and every transition. Under standalone `gg web` it is a
small owner in `internal/web` (§Standalone owner). The page, the routes and the
wire shapes are the same for both. Where this section says "the terminal", the
standalone owner does the same job unless §Standalone owner says otherwise.

**Identity, pushed by the terminal.** A new host seam
`SetWorkspace(*domain.WorkspaceInfo)`, called on open, leave, edit, adopt and
member load. The type lives in `domain` because `tui` does not import `web`:

```go
type WorkspaceInfo struct {
	Name         string
	Live         int // index of the member on screen
	SettingsRepo string
	Members      []WorkspaceMember
}
type WorkspaceMember struct {
	Path, Label string
	Missing     bool
	LastViewed  string
	Svc         *domain.Service // the terminal's own: caches and gate are shared
}
```

`nil` means no workspace. Each push fans out a `workspace` live event.

**Reads.** All behind `uiGate`, so a blocked page reads nothing:

- `GET /api/workspace`: identity, plus the pending launch question.
- `GET /api/workspace/member?path=<allowlisted member path>`: that member's
  worktree and branch rows in today's wire shapes, built by the same code as
  `/api/worktrees` and `/api/branches` over the member's service, with its
  branch filter applied. Its git cost is that of those two routes. It stats the
  path first and answers `missing`, which outranks the pushed `Missing`.
- `workspaces.toml` listing for the window and for `R`, read-only through
  `internal/workspace`, as `repos` is used.

The member on screen keeps coming from today's routes and today's live
refreshes. The page keeps each sleeping member's last rows, so a refresh of
the member on screen reads no other member, and a switch re-reads only the
member that became live.

**Freshness while the page is active.** The terminal's member watchers and
polls already exist and are ignored while it is blocked. While blocked,
`onMemberWatchEvent`, `onMemberPollTick` and `memberFetchDone` forward the
member's path through `Host.MemberChanged(path)` instead of loading it. The
terminal does not read the member's rows itself; activation reloads them, as it
does today. The page reads and the terminal reads are never both spent on one
change.

The host bumps a per-member change counter and fans out `workspace_member`
through `fanOut`, not `emit`, whose gate drops events during an operation. The
page re-reads that member, coalesced per member. `GET /api/workspace` carries
the counters. On every `hello` the page re-reads identity (no git) and only the
members whose counter moved, so an event lost while a switch replaces the hub
costs nothing extra and is not lost.

A missing member has no watcher, so it revives as in the TUI: on a click or a
switch to it. While the terminal is active the page is blocked and hears
nothing.

**What a blocked terminal still does.** Two carve-outs from "a blocked terminal
reads nothing":

- `refreshTick` runs only `memberFetchesDue` while blocked, so sleeping members
  keep being fetched, one at a time, by the existing ruling, since no one else
  would. Everything else on that tick stays suppressed.
- `applyRepoHealth`'s blocked early return moves below the parts that establish
  identity and the launch question (`setHomeRepoMain`, `startWorkspaceIfAsked`,
  the launch check). The health result itself stays unapplied.

The Update tail's `viewKick` and `memberFirstLive` kicks do not run while
blocked: the page does its own reads. Their flags stay set, so a member first
visited through the page gets its first-visit load, health and forge probes on
activation.

**Actions.** A new seam, `SetWorkspaceActor(fn(ctx, WorkspaceRequest)
(WorkspaceReply, error))`, over the same request-and-reply channel pattern as
the switcher and the activator. The terminal owns every write to
`workspaces.toml`, so the order is the TUI's: ask, switch away if needed, then
save. The page sends the edit it wants:

| Request | Terminal does |
|---|---|
| `open{name, drop}` | `openWorkspace` / `reopenWorkspace` |
| `leave{drop}` | `leaveWorkspace` |
| `edit{op, name, args, drop}` | what the popup does for that `op`: new, rename, delete, add, remove, move, settings |
| `launch{name or ""}` | answers the launch question |

The popup's edit logic moves out of `workspacePopup` into `Model` methods that
both the popup and the actor call, so rename updates the open workspace's name
and deleting the open workspace leaves it, in both. A refused switch refuses the
edit, and the file stays as it was. The new routes run the same "an operation is
running" check as `/api/reroot` before asking the terminal, since the terminal
cannot see the page's operations.

Switching keeps using `POST /api/reroot` and the existing switcher. A reachable
target inside a member is already a member switch in `guardedReRoot`. The repair
leg (`repair: true`) runs on the service of the member holding the target, not
the service on screen.

**Questions in place of terminal prompts.** When an actor request would call
`askBeforeDrop`, the terminal pushes no popup. It replies with the sessions
concerned, the server answers 409 `{drop_sessions: [...]}`, the page asks in the
tab that made the request, and it repeats the request with `drop: keep|stop`.
The launch question is read through `/api/workspace`.

**In-place decision.** The page's "listed" test (`listedPaths`, `inPlace`,
`wtOrder`) covers every member's worktrees. `followSwitch` and `softReroot` then
take the in-place lane across members. The server's `adoptService` still swaps
the service and restarts its live hub, as it does for any reroot. A `member`
flag on the `switched` message tells the page to re-read the repository-wide
lists.

**Settings.** Reads and writes split as in the TUI. Process-wide sections come
from and save to the settings repository (`settingsRepo()`), the rest to the
member on screen. The page's Settings window names the repository each section
writes to, so `handleSettingsPost` gets per-section targets and
`onWebConfigChanged` follows.

### Standalone owner

`gg web` has no `Model`, so `internal/web/workspace_owner.go` fills the same
seams: `SetWorkspace`, the actor and `MemberChanged`. `internal/tui`'s workspace
code is written around `Model` and its slots, so it is not reused directly. What
must not differ moves into a shared place both owners call:

- **To `internal/workspace`:** member path rules (`normalizeEntries`,
  `resolveMemberPaths`, `dedupeRealPaths`) and heading labels.
- **To `domain`:** the choice of a member's default worktree and the "which
  sessions does this change drop" rule behind `askBeforeDrop`, because sessions
  are a domain type and `workspace` is a DAG leaf.

The standalone owner holds:

- the open entry, its member services (`domain.Open`, shared caches and gate as
  in the TUI) and each member's last-viewed worktree;
- a `gitwatch.Watcher` and poll per sleeping member, forwarding to
  `MemberChanged`, with the watcher closed for the member on screen;
- sleeping members' background fetches, one at a time, by the same ruling as
  `memberFetchesDue`;
- the launch question, decided at start exactly as `maybeAskWorkspace` decides
  it, with no check of the terminal's screen.

Differences from the terminal owner:

- **Switching:** `/api/reroot` already swaps the service itself, with no
  switcher. After a swap the owner updates `Live` and the last-viewed worktree,
  or leaves the workspace when the target is outside every member.
- **Blocking:** there is no blocked state, so the carve-outs in §What a blocked
  terminal still does do not apply.
- **Quit:** the existing quit guard already counts every session in the process,
  members' included.
- **`--workspace`:** `gg web --workspace <name>` is parsed by `gg web` itself. An
  unknown name fails before a port is bound, like `gg --workspace`.
  `workspaceFlagMisuse` still refuses `gg --workspace <name> web`.

## 3. Page differences that stay

- Every page switch is an adopt. The page has no "look", so `alt+w` moves the
  terminal's own worktree, and coming back to the TUI shows that worktree.
- The page's rings follow the Worktrees list order, as they do today.
- Help text and page strings stay English. A refusal the terminal words, such
  as a busy reason, arrives in the terminal's language, as it does today.

## 4. Edge cases

- **A member fails to read:** its heading stays, with the error as its title
  and no rows; the next `workspace_member` retries. Other groups are unaffected.
- **A member appears or disappears (remount, `worktree add`):** the terminal
  pushes identity and the watcher event refreshes the rows.
- **An operation is running:** switches are refused as today and the status
  line says so; reads go on.
- **Page blocked, terminal changes the workspace:** the page re-reads
  `/api/workspace` on activation (`resumeActive`).
- **Terminal busy while the page asks:** the same reasons as `steerRefusal`
  reach the status line. The block's rule applies: windows behind it close and
  nothing refuses because of them.
- **A second tab:** identity and events reach every tab. The launch question
  appears in each and the first answer wins; the others hear the new state and
  close it. The session-drop question appears only in the tab that asked.
- **`gg://` link or `gg session navigate` into a member:** the in-place lane, no
  "switch repository?" ask; outside every member, the ask as today.
- **`gg open --web <link>` starting a standalone page in a member:** the page
  lands on the link, then asks the launch question.
- **Ten members with a slow one:** groups paint independently, and git
  subprocesses stay under the existing limit.

## 5. Verification

- Go and JS unit tests in the existing patterns, for the routes, seams,
  grouping, rings and row switching. Each guard is shown failing without it.
- A browser probe in Firefox and Chromium, against a real `gg --web --workspace`
  in tmux with three repositories: grouped lists, each walk, in-place switch with
  no reload and the console and draft kept, a sleeping member's new branch
  appearing, the window's edits, leave, the launch question, the session-drop
  question, flip to the TUI and back.
- The same probe's workspace flows against standalone `gg web --workspace` and
  `gg web` in a member, including quit with a member's session running. Route
  tests run against both owners.
- Screenshots of the page beside `./tui-capture.sh` captures of the same
  workspace, checking on-screen effects and absences, not state.
- Git calls counted for page open and for a refresh of the member on screen.
- `./test.sh race` green before asking to merge.
- Docs: CHANGELOG, README, `docs/web-tui-parity.md` row, CLAUDE-details section,
  and the CLAUDE.md `web` row.
