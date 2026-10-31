# Web Workspaces Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task, in this session (repo rule: no implementer subagents). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The gg web page, hosted and standalone, shows and works an open workspace the way the TUI does, including `alt+s` and `alt+S`.

**Architecture:** The page reads and asks through one set of server seams and never holds what is open. Under `gg` / `gg --web` the TUI `Model` is the owner behind those seams. Under standalone `gg web` a small owner in `internal/web` fills them. Shared rules move to `internal/workspace` and `internal/domain`.

**Tech Stack:** Go 1.26, Bubble Tea, vanilla JS modules, `runPureJS` tests (`internal/web/*js_test.go`), Playwright browser probes (scratchpad, not committed).

**Spec:** `docs/superpowers/specs/2026-10-31-web-workspaces-design.md`. Sample of the screen: `docs/superpowers/specs/2026-10-31-web-workspaces-mock.html`.

## Global Constraints

- `tui` does not import `web`; `web` is domain-only. New host calls go through the `WebHost` interface in `internal/tui/webhost.go`, wired by `cmd/gg`.
- Outside a workspace, nothing on the page or in the TUI changes, including the row gutter.
- Exact strings: `compare works within one repository`, `left workspace <name>`, `Open <repo> alone`, ` (missing)`.
- A member switch never reloads and never loses the console, open files or commit draft.
- Page strings and help are English. New TUI strings go through `i18n.T` in all four bundles.
- A refresh of the member on screen reads no other member.
- In a destructive question the first option is the safe one; esc cancels.
- There is no global `.hidden` rule: every hideable element needs its own `#id.hidden`.
- Every new route sits behind `uiGate` and the existing write and host guards; `path` values resolve against the member allowlist.
- New Go tests call `t.Parallel()` unless they touch process-global state. Commit messages end with the session's attribution lines.

## Review Focus

Each line has its test in the named task.

1. A member's path disappears between identity push and read: the route answers `missing` and the heading says so, with no 500 (Task 2, `TestWorkspaceMemberMissingOutranksPush`).
2. A `workspace_member` event is lost while a switch replaces the hub: the next `hello` re-reads only members whose counter moved (Task 2, `TestWorkspaceCountersSurviveHubRestart`; Task 5, `TestMembersToReread`).
3. Two worktrees in different members with the same directory name: rows, rings and `data-p` stay distinct because they key on full paths (Task 6, `TestRingAcrossMembersSameDirName`).
4. A row action on another member while an operation runs: the switch is refused and the action never runs in the repository on screen (Task 6, `TestRowActionPlanRefusedSwitchStops`).
5. A second `gg web` deletes or renames the workspace this process has open: a save reports the gone workspace, as the popup's `gone` does, and the page stays usable (Task 9, `TestOwnerEditWorkspaceGoneElsewhere`).

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/workspace/normalize.go` (new) | `NormalizeEntry`, `NormalizeEntries`, `HeadingLabels`, moved from `tui` |
| `internal/domain/workspace_info.go` (new) | `WorkspaceInfo`, `WorkspaceRequest`, `WorkspaceReply`, `WorkspaceQuestion`, `DefaultWorktree`, `RunningSessionsIn` |
| `internal/web/workspace.go` (new) | Host seams, identity and member routes, change counters, actor route |
| `internal/web/workspace_owner.go` (new) | Standalone owner |
| `internal/tui/webhost_workspace.go` (new) | Identity push, actor channel, launch question held for the page |
| `internal/tui/workspace_edit.go` (new) | Edit logic moved out of `workspacePopup` |
| `internal/web/static/workspace.js` (new) | Page state, pure grouping and row plan, member reads |
| `internal/web/static/workspacewin.js` (new) | Workspaces window, launch and session questions |

---

### Task 1: Shared rules and types

**Files:**
- Create: `internal/workspace/normalize.go`, `internal/workspace/normalize_test.go`, `internal/domain/workspace_info.go`, `internal/domain/workspace_info_test.go`
- Modify: `internal/tui/workspace_open.go`, `internal/tui/workspace_rows.go`, `internal/tui/workspace_popup.go`, `internal/tui/repo_view.go`

**Interfaces:**
- Produces, in `workspace`: `NormalizeEntry(Entry) Entry`, `NormalizeEntries([]Entry) []Entry`, `HeadingLabels(names, paths []string) []string`.
- Produces, in `domain`:

```go
type WorkspaceInfo struct {
	Name         string
	Live         int
	SettingsRepo string
	Members      []WorkspaceMember
}
type WorkspaceMember struct {
	Path, Label string
	Missing     bool
	LastViewed  string
	Svc         *Service
}
type WorkspaceRequest struct {
	Kind string   // "open", "leave", "edit", "launch"
	Edit string   // Kind "edit": "new", "rename", "delete", "add", "remove", "move", "settings"
	Name string   // "" on launch = open alone
	Args []string
	Drop string   // "", "keep", "stop"
}
type WorkspaceReply struct {
	DropSessions []SessionInfo // non-empty: nothing happened, ask and repeat with Drop
	Status       string
}
type WorkspaceQuestion struct {
	Repo string
	Rows []WorkspaceQuestionRow // one per containing workspace
}
type WorkspaceQuestionRow struct{ Name, Label string }

func DefaultWorktree(wts []model.Worktree, lastViewed string) string
func RunningSessionsIn(worktrees []model.Worktree) []SessionInfo
```

- [ ] **Step 1: Write failing tests.** `TestNormalizeEntryLinkedWorktreeToMain`, `TestNormalizeEntryDedupesSymlink`, `TestHeadingLabelsDisambiguateEqualNames` (the moved TUI cases, same expectations); `TestDefaultWorktreeLastViewedThenMain` (last viewed if listed, else first entry, else first non-bare when the main is bare); `TestRunningSessionsInOnlyListedWorktrees`.
- [ ] **Step 2: Run them.** `go test ./internal/workspace ./internal/domain -run 'Normalize|HeadingLabels|DefaultWorktree|RunningSessionsIn'`. Expected: build failure on the undefined names.
- [ ] **Step 3: Move the code.** `normalizeEntry`, `normalizeEntries`, `resolveMemberPaths`, `dedupeRealPaths` and their `.git` file helpers go to `workspace`. `headingLabel`'s rule goes to `HeadingLabels`. `repoView.defaultWorktree` and `Model.runningSessionsIn` become thin calls. No TUI behaviour changes.
- [ ] **Step 4: Run.** `go test ./internal/workspace ./internal/domain ./internal/tui ./internal/archtest`. Expected: PASS, including the DAG-leaf check on `workspace`.
- [ ] **Step 5: Commit.** `refactor: share workspace member rules with the web page`

### Task 2: Server identity, member reads and change events

**Files:**
- Create: `internal/web/workspace.go`, `internal/web/workspace_test.go`
- Modify: `internal/web/host.go`, `internal/web/server.go`, `internal/web/live.go`, `internal/web/worktrees.go`, `internal/web/branches.go`

**Interfaces:**
- Consumes: `domain.WorkspaceInfo`, `domain.WorkspaceQuestion`.
- Produces: `Host.SetWorkspace(*domain.WorkspaceInfo)`, `Host.SetWorkspaceQuestion(*domain.WorkspaceQuestion)`, `Host.MemberChanged(path string)`, `GET /api/workspace`, `GET /api/workspace/member?path=`, live reasons `workspace` and `workspace_member` (with `Path`), and `member: true` on `switched` when old and new roots are both inside the identity.
- Wire `/api/workspace`: `{"open": null|{"name","live","settings_repo","members":[{"path","label","missing","last_viewed","changes"}]},"question":null|{"repo","rows":[{"name","label"}]},"defined":["shop",…]}`. `defined` comes from `workspace.Load`; no git.
- Wire `/api/workspace/member`: `{"missing":bool,"worktrees":[…],"branches":[…],"filter":…,"error":""}` in today's row shapes. A read failure answers 200 with `error` set.
- Split `handleWorktrees` into `worktreeRows(ctx, svc) ([]worktreeRow, error)` and `handleBranches` into `branchRows(ctx, svc) ([]branchRow, any, error)`; the routes call them.

- [ ] **Step 1: Write failing tests.** `TestWorkspaceRouteNilWhenClosed`; `TestWorkspaceRouteListsMembersAndCounters`; `TestWorkspaceMemberRowsMatchPlainRoutes` (rows equal `/api/worktrees` and `/api/branches` served from that repository); `TestWorkspaceMemberRefusesPathOutsideMembers` (400); `TestWorkspaceMemberMissingOutranksPush`; `TestWorkspaceMemberReadErrorIs200WithError`; `TestMemberChangedFansOutDuringOp` (the event arrives while an op is running, proving `fanOut` over `emit`); `TestWorkspaceCountersSurviveHubRestart`; `TestWorkspaceRoutesBlockedByUIGate`; `TestSwitchedCarriesMemberFlag`.
- [ ] **Step 2: Run.** `go test ./internal/web -run 'Workspace|MemberChanged|SwitchedCarries'`. Expected: build failure on the undefined names.
- [ ] **Step 3: Implement** in `internal/web/workspace.go`, with identity behind `s.mu` and counters keyed by `model.KeyOf`. `Host.MemberChanged` bumps and calls `h.fanOut` directly.
- [ ] **Step 4: Run.** Same command, then `go test ./internal/web`. Expected: PASS.
- [ ] **Step 5: Commit.** `feat: web routes for the open workspace and its members`

### Task 3: The terminal pushes identity and keeps members fresh

**Files:**
- Create: `internal/tui/webhost_workspace.go`, `internal/tui/webhost_workspace_test.go`
- Modify: `internal/tui/webhost.go`, `internal/tui/member_watch.go`, `internal/tui/refresh.go`, `internal/tui/notify.go`, `internal/tui/model.go`, fake hosts in `internal/tui/*_test.go`

**Interfaces:**
- Consumes: Task 2's Host methods, added to `WebHost`.
- Produces: `func (m Model) workspaceInfo() *domain.WorkspaceInfo`, `func (m Model) pushWebWorkspace() tea.Cmd`, called from the Update tail when the value changes (compare a fingerprint on `Model`, not the pointer).

- [ ] **Step 1: Write failing tests.** `TestPushWorkspaceOnOpenLeaveEditAdoptLoad`; `TestBlockedMemberWatchForwardsNotLoads` (`webActive`: `MemberChanged(path)` called, no `memberLoadCmd`); `TestBlockedPollForwards`; `TestBlockedFetchDoneForwards`; `TestBlockedRefreshTickRunsOnlyMemberFetches`; `TestBlockedHealthStillEstablishesIdentity` (`setHomeRepoMain`, `startWorkspaceIfAsked` and the launch check run, health stays unapplied); `TestBlockedKeepsFirstVisitFlags` (`viewKick`, `memberFirstLive` stay set and fire on activation).
- [ ] **Step 2: Run.** `go test ./internal/tui -run 'PushWorkspace|Blocked'`. Expected: FAIL on the new behaviour.
- [ ] **Step 3: Implement.** Replace the `m.webActive` early returns in `onMemberWatchEvent`, `onMemberPollTick` and `memberFetchDone` with the forward, re-arming the listener as today. Move `applyRepoHealth`'s blocked return below the identity parts. Run only `memberFetchesDue` on a blocked `refreshTick`.
- [ ] **Step 4: Run.** `go test ./internal/tui`. Expected: PASS.
- [ ] **Step 5: Commit.** `feat: a blocked terminal keeps the page's workspace identity and members current`

### Task 4: Workspace actions through the terminal

**Files:**
- Create: `internal/tui/workspace_edit.go`, `internal/tui/workspace_edit_test.go`
- Modify: `internal/tui/workspace_popup.go`, `internal/tui/workspace_open.go`, `internal/tui/workspace_launch_popup.go`, `internal/tui/webhost_workspace.go`, `internal/tui/model.go`, `internal/web/workspace.go`, `internal/web/host.go`, `internal/web/reroot.go`

**Interfaces:**
- Consumes: `domain.WorkspaceRequest`, `domain.WorkspaceReply`.
- Produces: `Host.SetWorkspaceActor(fn func(context.Context, domain.WorkspaceRequest) (domain.WorkspaceReply, error))`; `POST /api/workspace` taking the request as JSON (`kind`, `edit`, `name`, `args`, `drop`). It answers 200 with `{"status"}`, 409 `{drop_sessions:[…]}` to ask, 409 `{error}` on a refusal, and 409 `errOpBusy` before asking the owner while an operation runs.
- Produces on `Model`: `workspaceEditApply(req domain.WorkspaceRequest) (Model, tea.Cmd, domain.WorkspaceReply, error)`, called by the actor; the popup's `n r d a x K J s` paths call the same underlying methods.
- `askBeforeDrop` gains an actor path: with `req.Drop == ""` and running sessions in the members, it returns `DropSessions` and pushes no popup; with `keep` or `stop` it proceeds, stopping them on `stop`.

- [ ] **Step 1: Write failing tests.** `TestPopupAndActorRenameUpdateOpenName`; `TestDeleteOpenWorkspaceLeavesBothPaths`; `TestActorDropAsksWithoutPopup`; `TestActorDropCancelLeavesFileUntouched` (no repeated request, so `workspaces.toml` bytes are unchanged); `TestActorDropStopStopsSessions`; `TestActorRefusedSwitchKeepsFile`; `TestLaunchQuestionHeldWhileWebActive` (no popup, `SetWorkspaceQuestion` called, cleared on answer and on repository change, popup shown after a flip to the TUI); `TestWebWorkspacePostBusyRefusedBeforeActor`; `TestRerootRepairRunsOnMemberService`.
- [ ] **Step 2: Run.** `go test ./internal/tui ./internal/web -run 'PopupAndActor|DeleteOpen|ActorDrop|ActorRefused|LaunchQuestion|WebWorkspacePost|RerootRepair'`. Expected: FAIL.
- [ ] **Step 3: Implement.** Use the channel pattern of `switcherFor` and `waitWebSwitchCmd`. Keep existing popup behaviour; the existing popup tests must pass unchanged. In `handleReroot`, run `repair` on the service of the member holding the target.
- [ ] **Step 4: Run.** `go test ./internal/tui ./internal/web`. Expected: PASS.
- [ ] **Step 5: Commit.** `feat: the page edits, opens and leaves workspaces through the terminal`

### Task 5: Grouped lists on the page

**Files:**
- Create: `internal/web/static/workspace.js`, `internal/web/workspacejs_test.go`
- Modify: `internal/web/static/sidebar.js`, `internal/web/static/live.js`, `internal/web/static/polish.css`, `internal/web/static/style.css`, `internal/web/embed_test.go` if it lists modules

**Interfaces:**
- Consumes: Task 2's routes and events.
- Produces in `workspace.js`, pure part between `// --- workspace model (pure; guarded against Go) ---` markers: `groupedRows(members, live, liveRows, memberRows, sortFn)` returning `[{heading, path, missing, error, rows}]`; `membersToReread(oldCounts, newCounts)`; `markViewed(rows, viewedWorktree)`.
- Produces, impure: `workspaceOpen()`, `memberOfPath(path)`, `loadWorkspace()`, `loadMember(path)` (coalesced per member), `allWorktreePaths()`.
- `renderBranches` and `renderWorktrees` render a heading `li.ws-heading[data-member]` and rows through today's row code when `workspaceOpen()`. Otherwise they take today's path unchanged. The rows carry `data-member`.

- [ ] **Step 1: Write failing tests.** `TestGroupedRowsWorkspaceOrderOwnSort`; `TestGroupedRowsMissingAndErrorHeadings` (labels `[shop-docs] (missing)`; an error becomes the heading title with no rows); `TestMembersToReread` (only moved counters); `TestMarkViewedOnlyOnScreenWorktree` (the `*:` mark, and every group keeps its own head star).
- [ ] **Step 2: Run.** `go test ./internal/web -run 'GroupedRows|MembersToReread|MarkViewed'`. Expected: FAIL, markers not found.
- [ ] **Step 3: Implement.** First paint uses the member on screen through today's fetches; other groups render when they arrive. `live.js` handles `workspace`, `workspace_member` and the re-read on `hello`. A refresh of the member on screen reads only today's routes.
- [ ] **Step 4: Run.** `go test ./internal/web`. Expected: PASS.
- [ ] **Step 5: Commit.** `feat: the page groups worktrees and branches by member`

### Task 6: Switching, row actions and walks

**Files:**
- Modify: `internal/web/static/workspace.js`, `ops.js`, `live.js`, `console.js`, `walk.js`, `sidebar.js`, `keys.js`, `internal/web/workspacejs_test.go`, the walk's existing JS test file

**Interfaces:**
- Consumes: `memberOfPath`, `allWorktreePaths`, the `switched.member` flag.
- Produces: `rowActionPlan(rowMember, liveMember, rowWorktree, defaultWorktree)` returning `{switchTo}` or `{}` (pure); `compareAllowed(markMember, rowMember)` (pure); `sessionStops(sessions, order, onlyDirs)` gains the optional `onlyDirs` for `alt+S`; `async function actOnRow(li, run)` in `workspace.js`, used by click, `.` menu, context menu, `alt+1…5` and each `memberSwitchKey` key.
- `listedPaths` in `ops.js` and `inPlace` in `live.js` use `allWorktreePaths()`. `softSwitch`, on `member`, re-reads tags, remotes, stashes, bookmarks, shelf, pull requests, health and notices and clears commit and compare marks.

- [ ] **Step 1: Write failing tests.** `TestRowActionPlan` (same member: no switch; other member: row's worktree, else default); `TestRowActionPlanRefusedSwitchStops` (`actOnRow`'s pure core does not run the action when the switch answers false); `TestCompareAllowed`; `TestAltSAllMembersAltCapitalSLiveOnly`; `TestRingAcrossMembersSameDirName`; `TestRingSkipsUnreachableAndMissing`; `TestSwitchTargetAcrossMembersInPlace`.
- [ ] **Step 2: Run.** `go test ./internal/web -run 'RowAction|CompareAllowed|AltS|RingAcross|RingSkips|SwitchTargetAcross'`. Expected: FAIL.
- [ ] **Step 3: Implement.** Heading click and `enter` adopt the default worktree, do nothing on the live member and report a missing member on the status line. Cursor moves never switch and can rest on a heading. A drag between members and `m` across members refuse with `compare works within one repository`. In `console.js`, `KeyS` with shift passes the live member's directories; outside a workspace `alt+S` stays `alt+s`.
- [ ] **Step 4: Run.** `go test ./internal/web`. Expected: PASS.
- [ ] **Step 5: Browser check.** Three repositories in tmux under `gg --web --workspace`: walk with `alt+w`, `alt+s` and `alt+S`, and confirm no reload (a `window` marker survives), with console and draft kept. Expected: no page errors in Firefox and Chromium.
- [ ] **Step 6: Commit.** `feat: page switches, row actions and walks across members`

### Task 7: `R`, palette, Workspaces window, questions and help

**Files:**
- Create: `internal/web/static/workspacewin.js`
- Modify: `internal/web/static/palette.js`, `index.html`, `style.css`, `polish.css`, `app.js`, `live.js`, the help overlay's module, `internal/web/workspacejs_test.go`

**Interfaces:**
- Consumes: `POST /api/workspace`, `loadWorkspace`.
- Produces: `openWorkspacesWindow()`; `postWorkspace(req)`, which turns a 409 `drop_sessions` into the leave-running / stop / cancel question and repeats the request; `repoListRows(defined, open, repos)` (pure); `showLaunchQuestion(q)`.
- Window keys, as in the spec: list `n r d o enter`; members `a d K J s esc`. Add order: repositories open now, recents, then Path…. Rows are clickable.

- [ ] **Step 1: Write failing tests.** `TestRepoListWorkspacesFirstWithDots` (`●` on the open workspace and members); `TestRepoListNoWorkspacesUnchanged`; `TestRepoEnterPlan` (workspace: open on this repository when a member, else its first; member: switch in place; outside: leaves); `TestDropQuestionSafeFirst` (order keep, stop, cancel; esc cancels); `TestLaunchEscOpensAlone`.
- [ ] **Step 2: Run.** `go test ./internal/web -run 'RepoList|RepoEnter|DropQuestion|LaunchEsc'`. Expected: FAIL.
- [ ] **Step 3: Implement.** The palette shows **Workspaces…** always and **Leave workspace** with one open. A leave across a reload carries `left workspace <name>` through the existing carry. A second tab closes the launch question on the `workspace` event. The help overlay gets the TUI's Workspaces section and walk sentences.
- [ ] **Step 4: Run.** `go test ./internal/web`. Expected: PASS.
- [ ] **Step 5: Commit.** `feat: page Workspaces window, repository list and questions`

### Task 8: Settings follow the section's repository

**Files:**
- Modify: `internal/web/settings.go`, `internal/web/static/settings.js`, `internal/web/settings_test.go`, `internal/tui/webhost.go`

**Interfaces:**
- Consumes: `WorkspaceInfo.SettingsRepo`.
- Produces: `func (s *Server) settingsTarget(ctx, section string) (svc *domain.Service, repo string)`, using the TUI's process-wide section list; `/api/settings` sections gain `repo`; `onWebConfigChanged` re-reads the repository that was written.

- [ ] **Step 1: Write failing tests.** `TestSettingsProcessWideGoesToSettingsRepo`; `TestSettingsMemberSectionGoesToLive`; `TestSettingsNoWorkspaceUnchanged`; `TestSettingsSectionsNameRepo`.
- [ ] **Step 2: Run.** `go test ./internal/web -run TestSettings`. Expected: new cases FAIL.
- [ ] **Step 3: Implement.** Move the section list to `internal/workspace` if `tui` holds it, so both share one copy.
- [ ] **Step 4: Run.** `go test ./internal/web ./internal/tui`. Expected: PASS.
- [ ] **Step 5: Commit.** `feat: page settings write to the repository each section belongs to`

### Task 9: Standalone `gg web`

**Files:**
- Create: `internal/web/workspace_owner.go`, `internal/web/workspace_owner_test.go`
- Modify: `internal/web/serve.go`, `internal/web/reroot.go`, `cmd/gg/main.go`, `cmd/gg/main_test.go`

**Interfaces:**
- Consumes: every Task 1 and Task 2 name, and the actor seam.
- Produces: `newWorkspaceOwner(s *Server, statePath string) *workspaceOwner`; `(*workspaceOwner).start(ctx, name string) error` (`name == ""` decides the launch question, a known name opens, an unknown or non-member name fails with the same wording as `resolveStartWorkspace`); `(*workspaceOwner).afterAdopt(ctx, root string)`, called at the end of `adoptService`; `(*workspaceOwner).close()`; `(*workspaceOwner).act(ctx, req) (domain.WorkspaceReply, error)`.
- `Serve` gains a parameter: `Serve(ctx, workdir, addr, launch, startAt, workspaceName string)`. The name is validated before a port is bound.

- [ ] **Step 1: Write failing tests.** `TestOwnerLaunchQuestionOnlyForMember`; `TestOwnerStartUnknownNameFailsBeforeBind`; `TestOwnerAfterAdoptUpdatesLiveAndLastViewed`; `TestOwnerAdoptOutsideLeaves`; `TestOwnerWatchesOnlySleepingMembers`; `TestOwnerFetchesOneAtATime`; `TestOwnerDropAsksAndStops`; `TestOwnerEditWorkspaceGoneElsewhere`; `TestStartAtLandsThenAsks`; `TestWebWorkspaceFlagParsed`; `TestWorkspaceFlagBeforeWebStillRefused`; and the route tests from Tasks 2 and 4, run against both owners through a `forEachOwner(t, fn)` helper.
- [ ] **Step 2: Run.** `go test ./internal/web ./cmd/gg -run 'Owner|StartAt|WebWorkspaceFlag|WorkspaceFlagBefore'`. Expected: FAIL.
- [ ] **Step 3: Implement.** Member services come from `s.open`. Each sleeping member gets a watcher and poll built as `startMemberWatchCmd` builds them. Fetches follow `memberFetchesDue`'s ruling. Edits use `workspace.Update`/`Edit`. `gg web --workspace` parses in the `web` subcommand.
- [ ] **Step 4: Run.** `go test ./internal/web ./cmd/gg`. Expected: PASS.
- [ ] **Step 5: Commit.** `feat: standalone gg web opens workspaces`

### Task 10: Probe, docs and gate

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `docs/web-tui-parity.md`, `docs/CLAUDE-details.md`, `CLAUDE.md` (`web` row), `internal/agentskill/using-gg.md` plus `agentskill.Version` for the `gg web --workspace` flag, the dogfood skill copy
- Create: `e2e/scenarios/tui_web_workspace.toml` and its goldens (the blocked terminal with a workspace open)

- [ ] **Step 1: Full browser probe, hosted.** Firefox and Chromium against real `gg --web --workspace` with three repositories: grouped lists, each walk, in-place switch, a sleeping member's new branch appearing, the window's edits, leave, launch question, session-drop question, flip to the TUI and back. Expected: every check passes with no page errors.
- [ ] **Step 2: Full browser probe, standalone.** The same flows against `gg web --workspace` and `gg web` in a member, plus quit with a member's session running. Expected: every check passes.
- [ ] **Step 3: Screenshots beside `./tui-capture.sh`.** Read each pair for on-screen effects and absences. Expected: matches, or a difference §3 of the spec names.
- [ ] **Step 4: Count git calls.** For page open and a refresh of the member on screen, with and without a workspace. Expected: the refresh is equal; page open grows by one member read per sleeping member.
- [ ] **Step 5: Docs and e2e.** Read every new golden in full. `gg init --update` for the dogfood copy runs with a scratch `HOME`.
- [ ] **Step 6: Whole-branch review.** A read-only subagent on the most capable model, given the spec, plan and Review Focus; one fix pass with a failing test for each Critical or Important finding.
- [ ] **Step 7: Sync and gate.** Merge `main` into the branch if it moved, then `./test.sh race > <log>`. Expected: the log says "all green".
- [ ] **Step 8: Commit, build `bin/gg`, and ask to merge.**
