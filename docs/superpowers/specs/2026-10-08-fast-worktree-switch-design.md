# Fast worktree switch — design

Date: 2026-10-08. Status: agreed in conversation, awaiting written review.

## Purpose

Two agents work in two worktrees of the repository while the user works in a
third. The user wants to hop between them with one key and see, each time,
what that worktree looks like: its modified files, its branch, its conflicts,
as if they had switched to it. Today a worktree switch inside one repository
is a full teardown and reload (`reRoot`): the screen blanks, every cursor is
lost, the file watcher and the steering inbox close, and the parked view the
alt+a cycle must return to is forgotten. On a large monorepo that is seconds
per press.

This feature makes a switch between worktrees of ONE repository instant and
state-keeping, and gives it two triggers:

1. **A console shown.** alt+a / alt+t (and ctrl+\ enter on a session of this
   repository) show the session's console AND the panels of the worktree it
   runs in. Stepping out of the console with tab, or focusing it with enter,
   needs no confirmation: the interface is already that worktree's. Closing
   the console brings the user's own worktree back.
2. **The user's own switch.** enter on a Worktrees row, a `gg session`
   switch ask, the hosted web page's switch, a `gg://` link's checkout, an
   agent tour: the same slot swap, plus gg's identity moves to the target.

User rulings (2026-10-08): looking equals switching (operations launched
while a console is shown run in the console's worktree); the swap happens
when the console is SHOWN, never on a confirm; both triggers ship together so
one repository never has two switch behaviours.

## Not in scope

- A session or a switch target in ANOTHER repository keeps today's full
  reload (`reRoot`). The console repo-scope rulings of 2026-10-01 stand.
- gg web's agent switcher and its own worktree switch keep their current
  shape; the hosted page follows the terminal as it does today.
- Steering: `gg session` commands from agent A keep targeting A's inbox.
  Showing A's console does not make this TUI A's steering target.

## The mechanism: worktree slots

### What is worktree-scoped

Worktrees of one repository share the common git dir, so Branches, Remotes,
Commits, Worktrees, Stashes, Tags and Reflog are the same list in every one
of them. What differs per worktree is small and already grouped in the
model:

| field (Model today)                                   | role |
|-------------------------------------------------------|------|
| `svc` (`*domain.Service` rooted at the worktree)       | every read and op of that tree |
| `status`, `filesIdx`, `filesIdxReview`, `stagedIdx`    | the Status panel |
| `conflict`                                             | its merge/rebase parties |
| `sel[panelFiles]`, `sel[panelStaged]`, `fileMarks`     | cursors and marks in Status |
| `workingReviews`, `workingReviewsGen`                  | the ✎ rows of that tree |
| `openFiles`, `docWatch`                                | the F window list and its poll |
| `currentWorktree`                                      | the header path |
| `watcher`, `watchGen`, `watchSupported`                | the git/file watcher |
| the per-source refresh bookkeeping of `srcStatus`      | generation, last run, measured cost |

These move into one value, `worktreeView`, held per worktree path:

```go
type worktreeView struct {
    path   string
    svc    *domain.Service
    status statusState     // status + conflict + the derived index slices
    cursor statusCursor    // sel for Files/Staged, fileMarks
    reviews workingReviewsState
    files  *openFilesReg
    docWatch docWatchState
    watch  watchState      // watcher, gen, supported
    refresh statusRefresh  // srcGen/srcLast/srcCost for srcStatus
    loaded bool            // false until its first status landed
}
```

The Model keeps `views map[string]*worktreeView` (keyed by cleaned path) and
`viewed string` (the path on screen). The fields above stay on the Model as
the LIVE copy — every reader and renderer is untouched — and a swap copies
them out to the leaving slot and in from the arriving one. The accessor
`m.view()` returns the live slot; the only writers of the map are the swap
and the slot's lifecycle.

### `switchView(path)`

The one primitive, same repository only:

1. Refuse (status message) if `path` is not in `m.worktrees` or fails
   `checkSwitchTarget` (the cross-environment notation guard; the repair
   offer stays with the Worktrees-row site).
2. Save the live fields into `views[viewed]` and put that slot to sleep:
   close its git watcher and its open-files watch (`closeDocWatch`), cancel
   nothing else — in-flight status reads carry the slot's generation and are
   dropped on arrival if the slot is not live.
3. Take `views[path]`, creating it on first use with
   `domain.OpenTUI(path)`, empty status, `loaded=false`.
4. Copy its fields in, set `viewed = path`, bump `srcGen[srcStatus]` so a
   read launched for the previous slot cannot land here.
5. Kick a status refresh for the new slot (manual, uncancellable) and start
   its watcher (`startWatchCmd`) and its docs sync (`syncAgentDocs`).

A slot that is not loaded yet renders the Status panel with its loading
marker; a loaded one renders instantly and refreshes underneath
(stale-while-revalidate). A swap to the slot already on screen is a no-op.

### Lifecycle of a slot

- Created on first use by either trigger; home is created at startup from
  the model's initial state (the first snapshot) — it is just `views[gg's
  worktree]`.
- Kept while the worktree exists in `m.worktrees`. The worktrees source
  handler drops slots whose path left the list (a removed or recycled
  worktree); a dropped slot that is on screen falls back to home.
- Dropped wholesale by `reRoot` (another repository).
- Never more than one slot awake: the git watcher and the docs watch run
  only for the live one. Hidden slots cost memory only.

### Freshness

The live slot gets exactly what the current worktree gets today: the
auto-refresh lane, the file watcher, status reloads after operations
(`opAffectedSources`). Hidden slots are not refreshed. On swap-in a slot is
refreshed immediately, so "switch to A, see what it changed" is one key;
the remembered state covers the interval until the read lands.

## Trigger 1: a console shown

Rule: **a shown console ⇔ the viewed worktree is the console's.**

- `showConsole(id, …)`: after the console is attached, if the session's dir
  is a worktree of this repository and differs from `viewed`,
  `switchView(dir)`. The console's `consoleReturn` records the path the
  cycle started from (`ret.view`), carried over when a console replaces a
  console, like the parked views.
- Every console close (`closeConsole`: esc, the cycle's return stop, `x`,
  the quit guard, settle after a repo switch) ends with
  `switchView(ret.view)` when it differs — the user's own worktree comes
  back with its cursors.
- A console in the viewed worktree swaps nothing. A console whose dir is not
  a worktree of this repository is not shown here (unchanged repo-scope
  rule).
- `cycleSessions` is unchanged in shape: its stops are consoles, and each
  console show now carries the view. The status message gains nothing: the
  header path already shows the viewed worktree.
- `openSessionAnywhere` for a session in this repository goes through
  `openConsole` and therefore the same rule; the other-repository branch
  stays a `reRoot` with `consoleSwitch.open`.
- Agent tours (`agent_tours_open.go`) in this repository use `switchView`
  then `showTour`; another repository's keep `guardedReRoot`.

The console does NOT move identity: tab out of A's console and commit — the
commit runs in A (its slot's `svc`), but `gg` quitting still lands the shell
in the user's own worktree and `gg session` still steers the user's own.

## Trigger 2: the user's own switch

`guardedReRoot(path, offerRepair)` gains a fast path: when `path` is a
worktree of the current repository, it runs `switchView(path)` and then
`adoptView()` — gg's identity moves to the viewed slot:

- `switchTarget` (the `--cwd-file` exit directory) and `publishedWT` (the
  session registry's worktree for `gg agent`/`gg session`).
- The session snapshot file: `removeSnapshotFile` for the old, then
  `snapshotTargetCmd(svc)` re-resolves for the new, exactly as `reRoot`
  does today.
- The steering inbox: `closeSteerInbox` + `closePendingWatch` for the old
  worktree, re-opened by the same path `reRoot`'s follow-ups use
  (`snapshotTargetMsg` → `reconcileSteer`), so a `gg session` switch ask
  from an agent keeps working.
- The web host is told the worktree changed (`webRerootCmd` equivalent,
  the hosted page re-reads its repo; its repo switch endpoint already
  routes through this model).
- `home` becomes the adopted path: a later console show/close returns
  there.

Everything `reRoot` drops for a DIFFERENT repository — selections of the
repo-scoped panels, marks, compare sets, feed scope, filters, notices,
parked views, the diff layer — is kept, because the repository did not
change. The in-repo switch keeps a diff open on a commit and the Commits
cursor where it was.

Callers that reach `guardedReRoot` and benefit: the Worktrees-row enter
(`model.go` three sites), the move-worktree chain (`pendingSwitch`), the
repair chain (`pendingRepairSwitch`), `steer_switch_ask`, `goto_link`'s
checkout switch, `repo_popup`/`repo_path_popup` when the chosen repository
is this one, the web page's switch request. Targets in another repository
fall through to `reRoot` unchanged.

A switch while a console is shown: the console's worktree stays the viewed
one until the console closes; a switch asked meanwhile adopts the target
and closes the console's return to it (`ret.view = target`), so the user
lands where they asked.

## On screen

- Header path: the viewed worktree, as today's `currentWorktree` (the field
  is part of the slot).
- Worktrees panel: gg's own checkout keeps its current marker; the viewed
  worktree, when it differs, gets a distinct "shown" marker and the row's
  `.` menu offers "Switch here" (adopt) as it offers switch today.
- Status row hint (`consoleWorktreeHint`): while the view follows the
  console the path is already in the header, so the hint reads
  `showing <path>` only when the console's worktree differs from gg's OWN
  (the 2026-10-05 ruling: "current worktree = gg's own"), and is dropped
  when the console runs in gg's own worktree. The half-row reservation and
  middle elision stay.
- Bottom-bar help and the `?` help list alt+a/alt+t as "show agent and its
  worktree".
- All new strings go through `i18n.T` with all four bundles.

## Errors and edge cases

- `domain.OpenTUI(path)` on a path whose git is broken: the slot is
  created, its first status read fails, the panel shows the error line as
  any failed status does; the swap itself never fails.
- A worktree removed while viewed: the worktrees source handler sees it
  gone, swaps home in and drops the slot, with a status message.
- Status read in flight across a swap: carries `srcGen`; dropped by the
  existing generation check.
- An operation running (`m.op != nil`) when a swap is asked: refused with
  the same message a repo switch gives (the op's `svc` is the slot it
  started in; results route by op gen, so even a late result is safe, but
  the user must not see A's panels while an op on B is reported).
- A focused console taking keys: alt+a/alt+t are gg's even there
  (unchanged); the swap happens inside the console path.
- Repo switch (`reRoot`) with slots: drops the map, closes every slot's
  watcher, and the existing `consoleSwitch` settle applies.
- Windows / cross-environment worktree: `checkSwitchTarget` refuses or
  offers repair exactly as today before any slot is touched.

## Testing

Unit tests in `internal/tui` with a real repo of two worktrees (the
`testRepo` helper + `git worktree add`):

- show a console of worktree A → Status lists A's files, header shows A;
  tab out and stage a file → `git -C A status` shows it staged; esc closes
  the console → home's files and Files cursor are back.
- alt+a over two agents and the return stop: three distinct views, the
  parked diff restored at the return (the 2026-10-06 ruling still holds).
- a hidden slot receives no refresh: run the auto-refresh lane with A
  hidden, assert no status read for A (FakeRunner argv count, or the
  srcLast bookkeeping).
- a stale read: launch a status read for home, swap to A, deliver the
  read → dropped, A's status untouched.
- Worktrees-row enter on A → view is A, `switchTarget` is A, Commits
  cursor and an open diff survive; enter on a worktree of ANOTHER repo →
  `reRoot` as before (existing tests).
- a worktree removed while viewed → home shown, slot gone.
- `consoleWorktreeHint`: present when the console's worktree ≠ gg's own,
  absent when equal (existing tests adjusted).
- i18n AST gates for the new strings.

One e2e TUI scenario (golden screens): two worktrees, a terminal session in
B, alt+t shows B's status, alt+t again returns to A's screen.

## Documentation to update at the end

`CHANGELOG.md`, `README.md` (worktree switch is instant; alt+a shows the
agent's worktree), `docs/CLAUDE-details.md` (the slot model, the two
triggers, the identity list), `CLAUDE.md`'s `tui` row by one clause, and
the console-related memory notes.
