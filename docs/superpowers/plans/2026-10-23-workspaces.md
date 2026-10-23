# Workspaces Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **This repo's rule (CLAUDE.md):** no implementer subagents — the session that wrote this plan executes it sequentially (superpowers:executing-plans). Review subagents are allowed (read-only).

**Goal:** Several repositories open in one gg TUI as a named workspace: a settings popup to define them, a launch dialog, grouped Worktrees/Branches tabs, workspace-wide alt+w / alt+a walks, every member watched, and repository switches that keep windows (slots, not re-roots).

**Architecture:** A DAG-leaf `internal/workspace` store (TOML under XDG state) holds the definitions. The TUI adds a **repository slot** (`repoView`) above today's per-worktree slots: the repository-wide Model fields `reRoot` resets today are saved/loaded per member the way `saveView`/`loadView` do per worktree; each member keeps its own `domain.Service`, config overlay and repository-level `gitwatch` watcher alive. The Worktrees and Branches entry lists gain heading rows per member and one `worktreeRing()` drives alt+w, alt+a and the tab targets.

**Tech Stack:** Go 1.26, Bubble Tea, go-toml/v2, `internal/filelock`, `internal/gitwatch`, the e2e TOML harness.

**Spec:** `docs/superpowers/specs/2026-10-22-workspaces-design.md`

## Global Constraints

- Max members per workspace: `MaxRepos = 5`. Name: unique case-insensitively, 1–40 runes, no `/`, `?` or control characters.
- State file: `workspaces.toml` beside `repos.toml`, `format = 1`, temp+rename under `filelock`, corrupt → empty, missing member paths kept (never pruned).
- Member identity = main checkout path (parent of the common git dir; the bare dir for a bare main repo), compared with `model.KeyOf`.
- One repository on screen at a time; a member switch is a slot swap, never `reRoot`. Every switch (look or adopt) is refused while an operation runs, with today's message `an operation is running — switch once it has finished`.
- Look (alt+w, alt+a, alt+q) never moves home; adopt (enter on a worktree row, `R`, a link, ctrl+\, a heading click) moves home. The hosted web page follows adopts only.
- Process-wide config (`[ui]`, `[console]`, `[tasks]`, `[agents]`, `[web]`, `[forge]`, `[debug]`, `[themes]`) comes from the workspace's settings repository and is fixed while the workspace is open; per-member config (`[worktree]`, `[branches]`, `[tools]`, `[refresh]`, `[versions]`, `[notes]`) comes from each member's `.gg.toml`.
- Every user-visible TUI string goes through `i18n.T` with a literal key present in `internal/i18n/lang/{ja,ko,zh,ru}.toml`; every new key is in `?` (`help.go`) and the footer (`footer.go`). New glyphs East-Asian-width neutral.
- `internal/tui` never imports `internal/git`; it reads through `domain`. `tui` may import `internal/workspace` for its types and Load/Save exactly as it imports `internal/repos` today (deviation from the spec's "frontends never import" line, taken for parity with `repos`; `archtest` is updated to allow it).
- Scriptable verbs are untouched. Only `tui.Run` asks the launch question.
- TDD; new tests call `t.Parallel()` unless they touch process-global state; commit after each task; `./test.sh race` green before merge.

## Review Focus

1. **A member whose path is on an unmounted drive at launch** → the dialog still lists the workspace; opening it loads the other members and the missing one shows a greyed heading "(missing)" with no rows; nothing hangs. (Task 10 test `TestOpenWorkspaceSkipsMissingMember`.)
2. **The same repository reached through a linked worktree** (gg launched in `/work/shop-api/.claude/worktrees/x`) → counts as member `/work/shop-api`; the dialog appears. (Task 3 test `TestRepoMainPathFromLinkedWorktree`.)
3. **A result for a sleeping member landing during a switch** (branches read of member B arrives while A is live) → lands in B's `repoView`, never in the live lists. (Task 9 test `TestSleepingMemberBranchesRouteToSlot`.)
4. **Two members with the same directory name** → headings read `api (shop)` / `api (billing)`; the ring still has every worktree once. (Task 11 test `TestHeadingsDisambiguated`.)
5. **Removing the viewed member from the open workspace in the settings popup** → the view stays on it as a single repository only if it was the last member; otherwise the view moves to the first remaining member before the slot is dropped, and running sessions were asked about first. (Task 13 test `TestRemoveViewedMemberMovesView`.)

---

## File structure

| Path | Responsibility |
|------|----------------|
| `internal/workspace/workspace.go` (+`_test.go`) | `Entry`, validation, cap, `Load`/`Save`, `Containing`, pure edits |
| `internal/domain/workspaces.go` (+`_test.go`) | `WorkspacesPath`, `RepoMainPath` (identity of a service's repository) |
| `internal/config/workspace.go` (+`_test.go`) | `MergeMember(processWide, member Config) Config` — the section split |
| `internal/tui/repo_view.go` (+`_test.go`) | `repoView` slot, `saveRepo`/`loadRepo`, `switchRepo`, member lookup |
| `internal/tui/workspace_open.go` (+`_test.go`) | `openWorkspace`, `leaveWorkspace`, member load/drop, `--workspace` |
| `internal/tui/workspace_rows.go` (+`_test.go`) | grouped `wtEntry`/`brEntry` with headings, `worktreeRing()` |
| `internal/tui/workspace_launch_popup.go` (+`_test.go`) | the launch dialog |
| `internal/tui/workspace_popup.go` (+`_test.go`) | Workspaces settings popup, member list, add picker |
| `internal/tui/member_watch.go` (+`_test.go`) | per-member repository-level watcher + routed Branches/Worktrees reads |
| Modify | `worktree_sessions.go`, `view.go` (row render), `console.go` (`worktreeOrder`), `worktree_view.go` (`switchViewBy`, `cycleWorktrees`), `switch_guard.go`, `goto_link.go`, `agent_tours_open.go`, `repo_popup.go`, `command_palette.go`, `settings.go` title, `run.go`, `headless.go`, `cmd/gg/main.go`, `footer.go`, `help.go`, `i18n/lang/*.toml`, `e2e/scenario.go` + `builder.go`, docs |

---

### Task 1: `internal/workspace` store

**Files:**
- Create: `internal/workspace/workspace.go`, `internal/workspace/workspace_test.go`
- Modify: `internal/archtest/*_test.go` (allow `tui` → `workspace`, like `repos`)

**Interfaces:**
- Produces:
  ```go
  const MaxRepos = 5
  const Format = 1
  type Entry struct {
      Name         string   `toml:"name"`
      Repos        []string `toml:"repos"`                   // absolute main-checkout paths, ordered
      SettingsRepo string   `toml:"settings_repo,omitempty"` // one of Repos or ""
  }
  func ValidName(name string) error                 // rules in Global Constraints
  func Load(path string) []Entry                    // missing/corrupt → nil; unknown newer format → nil
  func Save(path string, ws []Entry) error          // filelock + temp/rename, creates parents
  func Containing(ws []Entry, key model.CheckoutKey) []Entry
  func (e Entry) Has(path string) bool              // by model.KeyOf
  func (e *Entry) Add(path string) error            // ErrFull / ErrDuplicate
  func (e *Entry) Remove(path string)               // also clears SettingsRepo if it was that path
  func (e *Entry) Move(i, delta int)                // clamps
  func Find(ws []Entry, name string) (Entry, bool)  // case-insensitive
  func Upsert(ws []Entry, e Entry) []Entry          // by name, case-insensitive; appends when new
  func Delete(ws []Entry, name string) []Entry
  var ErrFull, ErrDuplicate error
  ```

- [ ] **Step 1: Write the failing tests** in `workspace_test.go` (all `t.Parallel()`, temp dirs):
  `TestLoadMissingIsNil`, `TestSaveLoadRoundTrip` (two entries, order and `SettingsRepo` kept, file starts with `format = 1`), `TestLoadCorruptIsNil`, `TestValidName` (table: "" / 41 runes / "a/b" / "a?b" / "\x01" rejected; "shop", "Shop 2" accepted), `TestAddCapAndDuplicate` (6th add → `ErrFull`; same path in another case on a folding OS → `ErrDuplicate` via `model.KeyOf`), `TestRemoveClearsSettingsRepo`, `TestMoveClamps`, `TestContainingByKey`, `TestUpsertCaseInsensitive`, `TestSaveLeavesNoTempLitter`.
- [ ] **Step 2: Run** `go test ./internal/workspace/ -run . -v` → FAIL (package missing).
- [ ] **Step 3: Implement** `workspace.go` per the interface. `Save` uses `filelock` the way `internal/linkhist` does (same lock name pattern, `<path>.lock`); the TOML shape is `type file struct { Format int; Workspaces []Entry `toml:"workspace"` }`.
- [ ] **Step 4: Run** the tests → PASS. Run `go test ./internal/archtest/` → PASS after adding `workspace` next to `repos` in the TUI allowlist.
- [ ] **Step 5: Commit** `feat(workspace): machine-local workspace store`.

---

### Task 2: config section split

**Files:**
- Create: `internal/config/workspace.go`, `internal/config/workspace_test.go`

**Interfaces:**
- Produces: `func MergeMember(processWide, member Config) Config` — returns `processWide` with the per-member sections (`Worktree`, `Branches`, `Tools`, `Refresh`, `Versions`, `Notes`) copied from `member`. Everything else (`UI`, `Console`, `Tasks`, `Agents`, `Web`, `Forge`, `Debug`, `Themes`) stays `processWide`'s.

- [ ] **Step 1: Write** `TestMergeMemberSplit`: two `Defaults()` copies differing in every section; assert each per-member field equals `member`'s and each process-wide field equals `processWide`'s (compare section structs whole with `reflect.DeepEqual`).
- [ ] **Step 2: Run** → FAIL (undefined). **Step 3: Implement.** **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(config): MergeMember splits per-member from process-wide sections`.

---

### Task 3: domain identity + path helpers

**Files:**
- Create: `internal/domain/workspaces.go`, `internal/domain/workspaces_test.go`

**Interfaces:**
- Produces:
  ```go
  func WorkspacesPath(reposStatePath string) string          // "" → ""; else filepath.Join(filepath.Dir(p), "workspaces.toml")
  func (s *Service) RepoMainPath(ctx context.Context) (string, error) // parent of common dir; the common dir itself when it is bare (no ".git" basename)
  ```
  `RepoMainPath` uses the existing common-dir verb the health probe uses (`s.CommonDir`/the snapshot's commonDir read — whichever `applyRepoHealth`'s probe calls); one git invocation, cleaned absolute path.

- [ ] **Step 1: Write** `TestWorkspacesPath`, `TestRepoMainPathFromLinkedWorktree` (real git: `newTestRepo` + `git worktree add`; a service opened in the linked worktree reports the main checkout), `TestRepoMainPathBare` (`git init --bare` → the bare dir).
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(domain): RepoMainPath + WorkspacesPath`.

---

### Task 4: `repoView` slot — save/load of the repository-wide state

**Files:**
- Create: `internal/tui/repo_view.go`, `internal/tui/repo_view_test.go`
- Modify: `internal/tui/model.go` (fields), `internal/tui/worktree_view.go:183` (`seedHome` also seeds the home `repoView`)

**Interfaces:**
- Produces:
  ```go
  type repoView struct {
      key       model.CheckoutKey   // of mainPath
      mainPath  string
      name      string              // directory name; headingLabel() adds the parent when ambiguous (Task 11)
      svc       *domain.Service     // rooted at mainPath (domain.OpenTUISharing(mainPath, m.homeSvc()))
      cfg       config.Config       // config.MergeMember(m.wsCfg, <member overlay>)
      views     map[model.CheckoutKey]*worktreeView
      viewed, home model.CheckoutKey
      lastViewed string             // path, for "its last-viewed worktree"
      // the repository-wide Model fields: everything reRoot (model.go:5084–5200) resets —
      // branches, remotes, tags, stashes, worktrees, feed, commit scope/filter/memos, sel, sortModes? (NO: per panel, stays live),
      // branchFilterSlot/bfSlotsLoaded/bfCfgApplied/bfMemo, notices/driftNotices, repo health, forge/PR state, snapshot fields,
      // stashView, remoteTagNames, pending* one-shots, watcher state (Task 9)
      loaded    bool
      missing   bool
  }
  type workspaceState struct {
      name     string
      members  []*repoView              // workspace order
      live     int                      // index of the member on screen
      settings string                   // settings repo main path ("" = the launch member)
  }
  func (m Model) inWorkspace() bool
  func (m Model) liveRepo() *repoView
  func (m Model) memberAt(path string) (*repoView, bool)   // which member a worktree path belongs to (by its listed worktrees, else by prefix of mainPath)
  func (m Model) saveRepo() Model                          // live fields → liveRepo()
  func (m Model) loadRepo(r *repoView) Model               // r → live fields, m.cfg = r.cfg, m.svc = r.svc, m.views = r.views …
  ```
- Inventory step: the field list is derived, not guessed — copy every assignment target of `reRoot` (`awk 'NR>=5084&&NR<=5200' internal/tui/model.go | grep 'm\.'`) into the struct, dropping those the spec keeps Model-global: generations (`loadGen`, `srcGen`, `watchGen`, `noticeGen`, `pushCheckGen`, `gitConfigGen`, `steerGen`), `sortModes`, the text filters, focus/tab/pin state, `switchTarget`, `loading/ready`.

- [ ] **Step 1: Write** `TestSaveLoadRepoRoundTrip`: a loaded test model (the `newTestModel`/fixture the worktree_view tests use); set `m.branches`, `m.worktrees`, `m.feed`, `m.notices`, `m.branchFilterSlot`; `saveRepo` into member A, build member B with different values, `loadRepo(B)` → live fields are B's; `loadRepo(A)` → A's again, and `m.svc == A.svc`, `m.cfg == A.cfg`. `TestSeedHomeSeedsRepo`: after the first load, `len(m.ws.members)==0` (no workspace) but `m.homeRepo()` is non-nil with `views == m.views`. `TestReRootDropsWorkspace`: `reRoot` sets `m.ws = nil`.
- [ ] **Step 2: Run** `go test ./internal/tui/ -run 'TestSaveLoadRepo|TestSeedHome|TestReRootDrops' -v` → FAIL.
- [ ] **Step 3: Implement** `repo_view.go`; add `ws *workspaceState`, `wsCfg config.Config` (process-wide source) and `homeRepo()` to Model; `seedHome` seeds a one-member `repoView` for home (no workspace yet: `m.ws == nil`, `m.homeRepoView` holds it); `reRoot` sets `m.ws = nil` and closes member watchers (Task 9 adds them; leave a `closeMembers()` hook now).
- [ ] **Step 4: Run** → PASS; `go test ./internal/tui/` whole package still PASS.
- [ ] **Step 5: Commit** `feat(tui): repoView slot for repository-wide state`.

---

### Task 5: member switch — `switchRepo` and the switch guards

**Files:**
- Modify: `internal/tui/worktree_view.go:472` (`switchViewBy`), `:524` (`switchRefusalBy`), `:443` (`homeSvc`), `internal/tui/switch_guard.go:33` (`guardedReRoot`), `internal/tui/goto_link.go:158`, `internal/tui/agent_tours_open.go:71`, `internal/tui/console.go` (`openSessionAnywhere`)
- Create: tests in `internal/tui/repo_view_test.go`

**Interfaces:**
- Produces: `func (m Model) switchRepo(r *repoView, worktree string, byUser bool) (Model, bool)` — refuses with today's message when `!m.opsIdle()`; `parkView()` + `saveRepo()` + `loadRepo(r)` + `switchViewBy(worktree, byUser)`; returns the kick (the existing `viewKickCmd` path runs from the Update tail as it does for a worktree switch). `worktree == ""` → `r.lastViewed`, else the main worktree.
- `switchViewBy(path, byUser)`: when `path` is not a worktree of the live member but `memberAt(path)` finds another member → `switchRepo(member, path, byUser)`; unchanged otherwise.
- `guardedReRoot(path, …)`: before re-rooting, `if r, ok := m.memberAt(path); ok && m.inWorkspace() { return m.adoptInto(r, path) }`. `adoptInto` = `switchRepo` then **the body of today's `adoptView` (worktree_view.go:758) and never `reRoot`**: `m.home` moves to the target slot, the snapshot target re-resolves (`snapshotTargetCmd`), the steer inbox re-homes, `repos.Touch` records the member, `m.switchTarget = path` (what `--cwd-file` writes on exit), the web page is handed `m.homeSvc()` (Task 14). Config is NOT re-read (`m.wsCfg` stays the settings repository's). A non-member target: `m.ws = nil` + status `left workspace <name>` + today's `reRoot`. A test that passes with a re-root is wrong: `TestEnterOnOtherMemberWorktreeAdopts` asserts `m.ws != nil` and that A's slot still holds its windows.
- `homeSvc()` returns the home member's service (home may be in any member).

- [ ] **Step 1: Write** `TestSwitchRepoKeepsWindows` (member A has a diff view open; `switchRepo(B)`; A's slot `windows` holds the diff; `switchRepo(A)` restores it), `TestSwitchRepoRefusedWhileRunning` (`m.running = true` → `(m, false)` and the status message), `TestEnterOnOtherMemberWorktreeAdopts` (`guardedReRoot(bPath)` with B a member → `m.home` is B's worktree key, `m.ws != nil`, `m.svc == B.svc`, no `loading`), `TestGuardedReRootNonMemberLeaves` (`m.ws == nil`, status `left workspace shop`), `TestLinkIntoMemberStaysInWorkspace` (goto_link path), `TestOpenSessionAnywhereIntoMember`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** the new tests and `go test ./internal/tui/ -run 'Switch|ReRoot|Link|Session'` → PASS.
- [ ] **Step 5: Commit** `feat(tui): member switch over repository slots; adopts stay in the workspace`.

---

### Task 6: grouped entries and the one ring

**Files:**
- Create: `internal/tui/workspace_rows.go`, `internal/tui/workspace_rows_test.go`
- Modify: `internal/tui/worktree_sessions.go:12,64` (`wtEntry`/`brEntry` gain `member int` and `heading bool`), `:24` (`worktreeEntries`), `:79` (`branchEntries`), `internal/tui/view.go:1132,1249` (render a heading row), `internal/tui/console.go:727,754,768` (`worktreeOrder`, `worktreeIndex`, `sessionRing` over the ring)

**Interfaces:**
- Produces:
  ```go
  type ringStop struct{ member int; path string }           // member index in m.ws.members (0 when no workspace)
  func (m Model) worktreeRing() []ringStop                   // Branches-tab checkout rows, group by group in workspace order; no workspace → today's worktreeOrder() mapped
  func (m Model) ringIndex(path string) int                  // -1 when absent
  func (m Model) memberBranchEntries(i int) []brEntry        // today's branchEntries computed against member i's slot (its branches, sort, filter slot, sessions)
  func (m Model) memberWorktreeEntries(i int) []wtEntry
  func headingLabel(members []*repoView, i int) string       // dir name; "+ (parent)" when another member has the same dir name
  ```
- `worktreeEntries()`/`branchEntries()`: no workspace → unchanged; in a workspace → for each member: one heading entry, then that member's entries with `member = i`. The cursor skips `heading` entries (the same skip the sub-row logic uses for non-selectable rows — if none exists, `moveSel` in the two panels steps over headings).
- `worktreeOrder()` and `worktreeIndex` become thin wrappers over `worktreeRing()` for the live member; `sessionRing(terminal)` walks `worktreeRing()` stops in order and collects each stop's sessions (`domain.Sessions().List()` filtered by worktree path as today).

- [ ] **Step 1: Write** `TestRingSingleRepoMatchesWorktreeOrder` (no workspace: ring paths == today's `worktreeOrder()` paths), `TestRingSpansMembersInOrder` (two members, 2+1 worktrees → 3 stops, member indexes 0,0,1), `TestEntriesHaveHeadings` (first entry of each group is a heading; cursor `moveSel` from the last row of A lands on the first worktree row of B, not the heading), `TestHeadingsDisambiguated` (Review Focus 4), `TestSessionRingCrossesMembers`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement** (heading render: bold `theme` title style, the member name, full width, no gutter marks; a sleeping member's rows come from its `repoView` fields). The live panel's **sort mode and `/` text filter apply to every group** (`memberBranchEntries(i)` runs `displayIndicesBase`'s sort+filter over member i's branches with the live `sortModes[panelBranches]` and the live filter text); the branch-filter SLOT is member i's own. **Sub-rows of a sleeping member show sessions and ◆ tasks only** (process-global `domain.Sessions()`/`Tasks()`); review/overview sub-rows (`srcNotes`) appear once the member is live, since that source refreshes on wake (Task 9 routes only Branches and Worktrees). **Step 4: Run** → PASS, plus `go test ./internal/tui/ -run 'Worktree|Branch|Session|Cycle'` → PASS.
- [ ] **Step 5: Commit** `feat(tui): grouped Worktrees/Branches entries and the workspace ring`.

---

### Task 7: the walks over the ring

**Files:**
- Modify: `internal/tui/worktree_view.go:803` (`cycleWorktrees`), `internal/tui/console.go:815` (`cycleSessionsIn`), `internal/tui/worktree_walk.go` (unchanged logic; verify it uses the shown worktree), `internal/tui/footer.go:243-249`, `internal/tui/help.go:92-99`

- [ ] **Step 1: Write** `TestAltWCrossesMember` (viewed = last worktree of A → alt+w shows first worktree of B: `m.liveRepo()==B`, `m.home` unchanged (a look), cursor on that branch row inside B's group), `TestAltWWrapsToFirstMember`, `TestAltAWalksAcrossMembers` (sessions in A and B; from A's bound console alt+a binds B's), `TestAltShiftAStaysInViewedWorktree`, `TestAltQUnchangedAcrossMembers` (alt+q after an alt+w into B walks B's worktree screens).
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement:** `cycleWorktrees` iterates `worktreeRing()` and calls `switchRepo` when the next stop's member differs (then the existing `userSwitchView`); the `n < 2` messages count the ring; `cycleSessionsIn` (unscoped) starts from `ringIndex(viewed)`. Footer `next-worktree` predicate uses `len(m.worktreeRing()) > 1`. Help rows for alt+a/alt+w gain the sentence "In a workspace the walk runs over every repository's worktrees, group by group" (one new i18n key each, four bundles).
- [ ] **Step 4: Run** → PASS; `go test ./internal/tui/ -run 'i18n|Vocab|Menu|Prose'` (the AST gates) → PASS.
- [ ] **Step 5: Commit** `feat(tui): alt+w / alt+a walk the whole workspace`.

---

### Task 8: opening, leaving and loading members

**Files:**
- Create: `internal/tui/workspace_open.go`, `internal/tui/workspace_open_test.go`
- Modify: `internal/tui/run.go` (`RunOptions.Workspace string`, state path), `internal/tui/headless.go` (`HeadlessOptions` gets `WorkspacesPath string`), `internal/tui/notify.go:122` (`applyRepoHealth` → `m.afterRepoHealth()` hook calls the launch check of Task 10), `cmd/gg/main.go` (`extractWorkspaceFlag`)

**Interfaces:**
- Produces:
  ```go
  func (m Model) openWorkspace(e workspace.Entry) (Model, tea.Cmd)   // home member must be in e, else re-root into e.Repos[0] via guardedReRoot and open after its first load
  func (m Model) leaveWorkspace() (Model, tea.Cmd)                   // keeps the live member; drops the rest (Task 13's prompt gates it)
  type memberLoadedMsg struct{ svc *domain.Service; data dataLoadedPayload }  // whatever loadCmd returns today, addressed by svc
  func memberLoadCmd(r *repoView) tea.Cmd                            // the first full read for a member: the same reads loadCmd does, into the slot
  func (m Model) applyWorkspaceConfig() Model                        // m.wsCfg from the settings repo (config.Load(global, <settings>/.gg.toml)); each member's cfg = MergeMember(m.wsCfg, member overlay); theme via setTheme/applyTheme once
  ```
- `openWorkspace` builds a `repoView` per member (`missing = true` when `os.Stat` fails), the home member reusing the live slot; sets `m.ws`; dispatches `memberLoadCmd` for each non-home member in order (eager, Branches/Worktrees sources first, commits last — in the single cmd, order the reads that way); status `workspace <name>: <n> repositories`. Loads never set `m.loading`.
- `--workspace <name>`: `extractWorkspaceFlag(args) (name string, rest []string)` (both `--workspace x` and `--workspace=x`); `Run` stores it in `m.startWorkspace`. **Synchronously in `Run`, before `tea.NewProgram`**, when `opts.Workspace != ""`: `main, err := svc.RepoMainPath(ctx)` (one git call, Task 3), `workspace.Find(Load(path), name)`; not found or `!e.Has(main)` → `Run` returns the error `--workspace: <name>: this repository is not a member` (cmd/gg prints `gg: ` + it, exit 1) and nothing opens. Found → `m.startWorkspace = e`; the first health probe's success path calls `openWorkspace(e)` instead of `maybeAskWorkspace`.

- [ ] **Step 1: Write** `TestOpenWorkspaceLoadsMembers` (two real repos in temp dirs; after settling, both `repoView.loaded`, the live one is home), `TestOpenWorkspaceDoesNotSetLoading`, `TestOpenWorkspaceSkipsMissingMember` (Review Focus 1), `TestLeaveWorkspaceKeepsLive`, `TestWorkspaceFlagRefusedForNonMember` (Run-level: a fake `svc` whose main path is not in the entry → error string), `TestExtractWorkspaceFlag` (cmd/gg, table), `TestApplyWorkspaceConfigUsesSettingsRepo` (member B is `settings_repo` with `[ui] theme = "light"`, A is home → active theme light; switching to A keeps it).
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(tui): open/leave a workspace, member loads, --workspace`.

---

### Task 9: per-member watchers and routed reads

**Files:**
- Create: `internal/tui/member_watch.go`, `internal/tui/member_watch_test.go`
- Modify: `internal/tui/watch.go:106` (`startWatchCmd` takes the service/common dir it watches), `internal/tui/model.go:3606-3639` (`watchReadyMsg`/`watchEventMsg`/`watchClosedMsg` carry `svc`), `internal/tui/source.go:176,189` (the `srcBranches`/`srcWorktrees` result arms route by `svc`), `internal/tui/worktree_view.go:551-563` (sleep closes only the WORKTREE-level pieces; the member's repository watcher stays), `internal/tui/refresh.go` (background lane entries per member for `srcBranches`/`srcWorktrees`)

**Interfaces:**
- Produces: `repoView.watcher *gitwatch.Watcher`, `repoView.watchGen int`; `func (m Model) startMemberWatch(r *repoView) tea.Cmd`; `func (m Model) sleepingRepo(svc *domain.Service) *repoView` (nil when `svc` is the live member's); `func (r *repoView) applyBranches(...)` / `applyWorktrees(...)` — the same derivations the live arms do (`rebuildCommitGraph` is NOT needed for a sleeping member).
- Routing rule, in the two source arms and the watch messages: `if r := m.sleepingRepo(msg.svc); r != nil { apply into r; return m, nil }`. Every other source of a sleeping member is refreshed on wake by the existing kick (unchanged).
- A member's watcher starts with its first load and closes on drop/leave/`reRoot`; unsupported → that member's two sources poll on the background lane cadence.

- [ ] **Step 1: Write** `TestSleepingMemberBranchesRouteToSlot` (Review Focus 3: a `srcBranches` result with B's svc while A is live → `B.branches` updated, `m.branches` untouched), `TestMemberWatcherSurvivesSwitch` (switch A→B→A: B's `watcher != nil` throughout; with real fsnotify, create a branch in B's repo while A is live and settle → B's group shows it), `TestLeaveClosesMemberWatchers`, `TestUnsupportedMemberPolls`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** → PASS; `go test -race ./internal/tui/ -run 'Member|Watch'` → PASS.
- [ ] **Step 5: Commit** `feat(tui): every member watched; Branches/Worktrees reads route to sleeping slots`.

---

### Task 10: the launch dialog

**Files:**
- Create: `internal/tui/workspace_launch_popup.go`, `internal/tui/workspace_launch_popup_test.go`
- Modify: `internal/tui/notify.go:122` (call site), `internal/tui/workspace_open.go` (`m.maybeAskWorkspace()`)

**Interfaces:**
- Produces: `type workspaceLaunchPopup struct { rows []launchRow; sel int }` (`launchRow{entry *workspace.Entry /*nil = alone*/, label string}`), `func (m Model) maybeAskWorkspace() (Model, tea.Cmd)` — once per process (`m.workspaceAsked`), only when `m.startWorkspace == ""` and `workspace.Containing(Load(path), key)` is non-empty; the popup is a layer (`pushLayer`), enter opens, esc = alone. Row text: `Open workspace <name> — <dir>, <dir>, <dir>` and `Open <dir> alone` (i18n keys with `%s`).
- Ordering: after a repo-health blocker (the call sits at the end of `applyRepoHealth`'s success path), before `maybeResumePrompt` runs for a worktree choice (the resume prompt is per worktree; it still fires for home after the dialog closes).

- [ ] **Step 1: Write** `TestLaunchDialogOnlyForMembers` (state file with a workspace containing home → popup on top; a file without it → no popup), `TestLaunchDialogEscOpensAlone` (`m.ws == nil`, no popup, status unchanged), `TestLaunchDialogEnterOpensWorkspace` (`m.ws.name == "shop"`), `TestLaunchDialogListsEveryWorkspace` (two containing → two rows + alone), `TestNoDialogWithWorkspaceFlag`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(tui): launch dialog for repositories that belong to a workspace`.

---

### Task 11: tab interactions — headings, cross-member actions, status refresh

**Files:**
- Modify: `internal/tui/model.go` (Branches/Worktrees key handlers: enter/click on another member's row; the mouse click on a heading), `internal/tui/view.go` (heading render), `internal/tui/branch_compare.go` (`f` across members refused), `internal/tui/branch_filter.go:193,257` (slot per member: keyed by the cursor's group)

- [ ] **Step 1: Write** `TestEnterOnOtherMemberWorktreeRow` (adopt into B at that worktree), `TestHeadingClickAdoptsLastViewed` (B's `lastViewed` set → that worktree; unset → main), `TestBranchActionOnOtherMemberSwitchesFirst` (cursor on B's branch row while A live; `s` switch → `m.liveRepo()==B` before the op dispatch; assert via the dispatched op's svc), `TestCompareAcrossMembersRefused` (status `compare works within one repository`), `TestAltDigitActsOnCursorGroup` (alt+1 with the cursor in B's group changes B's `branchFilterSlot`, not A's), `TestSortAppliesToEveryGroup`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(tui): grouped tabs act across members`.

---

### Task 12: `R` popup — workspaces group; palette entries

**Files:**
- Modify: `internal/tui/repo_popup.go:24,103,115,196,324` (a `workspaces []workspace.Entry` group above the entries; rows render `name — dir, dir`; ● on the open one; fuzzy filter over names + member dir names; enter on a workspace row → `openWorkspace`), `internal/tui/command_palette.go:68-79` (entries `Workspaces…` → Task 13's popup; `Leave workspace` visible only when `m.inWorkspace()`), `internal/tui/footer.go`, `internal/tui/help.go` (R row + palette rows)

- [ ] **Step 1: Write** `TestRepoPopupListsWorkspaces` (group present only when the state file has entries; rows ordered as the file), `TestRepoPopupEnterOpensWorkspace`, `TestRepoPopupEnterOnOpenWorkspaceNoop`, `TestRepoPopupFilterMatchesMemberName`, `TestRepoPopupMemberMarked`, `TestPaletteLeaveWorkspaceVisibility`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** → PASS (incl. the overlay fit test pattern used by repo_popup tests).
- [ ] **Step 5: Commit** `feat(tui): R switches workspaces; palette entries`.

---

### Task 13: Workspaces settings popup (two levels, add picker, drop prompt)

**Files:**
- Create: `internal/tui/workspace_popup.go`, `internal/tui/workspace_popup_test.go`
- Modify: `internal/tui/workspace_open.go` (`dropMember(r)` with the sessions prompt; `applyWorkspaceEdit`), `internal/tui/repo_path_popup.go` (reusable for the `Path…` row — a callback for the resolved path instead of `guardedReRoot`), `internal/tui/settings.go` (title names the settings repository inside a workspace)

**Interfaces:**
- Produces: `type workspacePopup struct { level int /*0 list,1 members,2 add picker*/; ws []workspace.Entry; sel int; cur int /*edited workspace*/; msel int; input *textfield; …}` embedding `popupMax`. Keys exactly as spec §2: list `n r d o enter esc`; members `a d K J s esc`; picker: three sources top to bottom (open now, recents, `Path…`), `/` fuzzy filter, enter.
- `dropMember(r)`: running sessions in r → a three-option prompt (`leave them running (hidden)` first, `stop them`, `cancel`) via the existing option-prompt popup pattern (`relatedPromptPopup`/`resumePromptPopup` shape); then close r's watcher, drop its slots, keep sessions in the manager when "leave".
- Live edits: add → `memberLoadCmd`; remove the live member → move the view to the first remaining member first (Review Focus 5); reorder → `m.ws.members` re-sorted; `s` → `applyWorkspaceConfig()`.

- [ ] **Step 1: Write** `TestWorkspacePopupCreateRenameDelete` (state file after each), `TestWorkspacePopupNameValidationInline`, `TestWorkspacePopupAddCapRefused`, `TestWorkspacePopupAddDuplicateRefused`, `TestWorkspacePopupPickerSources` (open-now rows first, then recents, then `Path…`), `TestWorkspacePopupMoveReordersGroups`, `TestWorkspacePopupSettingsRepoMarker`, `TestRemoveViewedMemberMovesView`, `TestDropMemberWithSessionsAsks` (first option is "leave running"; choosing it keeps the session in `domain.Sessions()`), `TestSettingsTitleNamesSettingsRepo`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** → PASS; AST gates PASS.
- [ ] **Step 5: Commit** `feat(tui): Workspaces settings popup`.

---

### Task 14: presence, web follow, i18n sweep

**Files:**
- Modify: `internal/tui/run.go` (`publishedWT`/`publishedView` already follow home/viewed — verify across members), `internal/tui/webhost.go:329` (`rerootWebCmd` on adopt into another member uses `m.homeSvc()`), `internal/i18n/lang/*.toml` (every key added in Tasks 7–13)

- [ ] **Step 1: Write** `TestAdoptIntoMemberMovesWebPage` (a fake `WebHost` records the service handed on adopt; a look hands nothing), `TestPublishedWorktreeFollowsAdopt`.
- [ ] **Step 2: Run** → FAIL where the follow is missing. **Step 3: Implement.** **Step 4: Run** → PASS; `go test ./internal/tui/ -run 'i18n|Vocab|Menu|Prose'` → PASS with all four bundles complete.
- [ ] **Step 5: Commit** `feat(tui): web page and presence follow member adopts; translations`.

---

### Task 15: e2e scenarios and golden screens

**Files:**
- Modify: `e2e/scenario.go` (`Input.Extra []ExtraRepo{Name string; Steps []Step}` — sibling repos under the sandbox root, `git init` + the same step vocabulary), `e2e/builder.go`, `e2e/tui.go:31` (`HeadlessOptions.WorkspacesPath`), `e2e/scenario.go` `TUI` (`Workspaces string` — TOML text written to that path before the model starts; `WorkspaceFlag string` → `startWorkspace`)
- Create: `e2e/scenarios/s1xx_workspace_launch.toml` (dialog with one and with two workspaces; esc = alone), `s1xx_workspace_tabs.toml` (grouped Worktrees + Branches, disambiguated headings), `s1xx_workspace_alt_w.toml` (alt+w crossing a heading), `s1xx_workspace_popup.toml` (both levels). Follow the `writing-e2e-scenarios` skill for the golden workflow (`-update`).

- [ ] **Step 1: Write** the harness tests for `Extra` (`e2e/builder_test.go`: two sibling repos exist with their steps applied) → run → FAIL → implement → PASS.
- [ ] **Step 2: Write** the four scenarios with `screen_contains` assertions (the heading text, `Open workspace shop — …`, the status `workspace shop: 2 repositories`); run `./test.sh e2e` → FAIL on missing goldens → `-update` → inspect the screens → PASS.
- [ ] **Step 3: Commit** `test(e2e): workspace scenarios`.

---

### Task 16: docs and the gate

**Files:**
- Modify: `CHANGELOG.md` (Added: Workspaces — one paragraph per surface), `README.md` (Workspaces section: palette entry, launch dialog, `--workspace`, R group, walks), `CLAUDE.md` (one package-map row for `workspace`; `tui` row mentions repository slots), `docs/CLAUDE-details.md` (new section "Workspaces — repository slots over worktree slots": the inventory, look/adopt across members, routing rule, config split, the ring)

- [ ] **Step 1: Write the docs.**
- [ ] **Step 2: Run** `./test.sh race > /tmp/claude-1000/-work-gigagit/9a5cac4c-9c2a-480a-a992-2cb0238bfb55/scratchpad/race.log 2>&1`; read the verdict in a separate call; proceed only on `all green`.
- [ ] **Step 3: Build** `cd <worktree> && go build -o bin/gg ./cmd/gg` and report the absolute path.
- [ ] **Step 4: Commit** `docs: workspaces`.
- [ ] **Step 5: Final review** — a fresh read-only review subagent on the most capable model over `git diff main...feat/workspaces`; fix findings; re-run the gate; then ASK before merging.

---

## Self-review notes

- Spec §1 → T1; §2 → T13; §3 → T8, T10; §4 → T5, T8, T12; §5 → T6, T11; §6 → T4, T5, T8, T9, T13, T14; §7 → T7; §8 → T14, T15, T16; §9 covered by each task's tests; §10's builder items: inventory (T4 step), eager load (T8), prompt ordering (T10), the move-home open item (T5's `adoptInto` bypasses `reRoot`).
- Names used across tasks: `repoView`, `workspaceState`, `m.ws`, `m.wsCfg`, `switchRepo`, `adoptInto`, `memberAt`, `liveRepo`, `sleepingRepo`, `worktreeRing`, `ringStop`, `ringIndex`, `openWorkspace`, `leaveWorkspace`, `dropMember`, `memberLoadCmd`, `applyWorkspaceConfig`, `maybeAskWorkspace`, `startWorkspace`, `workspaceLaunchPopup`, `workspacePopup`, `headingLabel`, `config.MergeMember`, `domain.RepoMainPath`, `domain.WorkspacesPath`.
