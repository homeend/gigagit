# Per-worktree window stacks — design

Date: 2026-10-09. Status: IMPLEMENTED — phases 1–6 merged into
`feat/fast-worktree-switch` the same day (plans
`docs/superpowers/plans/2026-10-09-window-stacks-{1..6}-*.md`). Agreed in
conversation; reviewed read-only by a second model (verdict "reasonable
with the listed changes") and revised with every required change before
building. Differences from the text below, decided while building: the
whitelist is one function (`parkableLayer`) rather than a method; the
generic `contentPopup` is not parkable; `switchView(path, byUser)` is
`switchViewBy` with `switchView`/`userSwitchView` wrappers. Builds on
`2026-10-08-fast-worktree-switch-design.md` (the slots) and the three
audit rounds merged into `feat/fast-worktree-switch` (last `7cbbcb5c`).

## Purpose

The fast switch keeps one `Model` and swaps the worktree-scoped DATA
(status, cursors, marks, reviews) between slots. Everything else — the
window stack, the popups, the files and stash views, steering leftovers —
stays on the Model and crosses the swap. Three review rounds found the
same class of bug nine times: a window or a parked command of worktree A
acting on worktree B's file of the same name. Each was fixed with a rule
(`workingTreeWindow`, `dropWorkingTreeWindows`, `parkedLayersFor`, the
attention-band pruning, the `pendingSteer` service tag, the per-slot
resume flag, `filesViewIsWorkingTree`). The rules are correct today and
wrong the next time someone adds a window.

This design removes the class: **a worktree owns its windows.** A swap
saves the leaving worktree's whole window state into its slot and restores
the arriving worktree's. Nothing is filtered, nothing crosses. The
repository-wide lists (branches, commits, stashes, tags, reflog, previews,
PRs, the commit feed) stay shared, so the swap stays instant.

User rulings (2026-10-09):

- A commit diff, a history, a compare do NOT follow a swap. They wait in
  the worktree they were opened in and are back when it returns. (This
  reverses the round-1 promise "an open commit diff survives" — it still
  survives, in its worktree.)
- Popups are part of the worktree's windows: an add-worktree popup half
  typed in A waits while B is shown and submits in A when A returns.
- A parked popup keeps receiving the results it asked for ("if an
  interface exists we don't have to worry about where to route the
  results"): the result is kept for the slot and applied when the
  worktree returns (see "Results addressed to a sleeping slot").
- Top-level, never parked: an operation's decision modal, an interactive
  process, the agent console, notices.
- Agent-triggered swaps (a console show, a steer ask, a gone slot) still
  wait while the user types in a popup; the user's own alt+w / alt+a may
  swap over a parkable popup.
- Memory: the diff caches are per REPOSITORY, not per service; the F
  window drops its on-disk file list when its worktree sleeps.

## Not in scope

- Per-worktree copies of the repository lists or the commit feed. They
  stay shared and `markHead` / the feed re-root keep marking them for the
  viewed worktree.
- Another repository: `reRoot` stays the full reload and drops every slot.
- gg web: the hosted page keeps following gg's own worktree.
- The decision modal and `m.proc`: a swap is refused while an operation
  runs or a process owns the screen, as today.
- The session snapshot (`gg session status`, the web page's state): it
  reads the live Model and keeps describing what is on screen.

## The mechanism

### What moves into the slot

The Model EMBEDS a `windowState` struct holding the window fields, so
every reader keeps spelling `m.filesView`, `m.layers`, `m.diffTag` (Go
promotes the fields); the slot gains a `windows windowState` that
`saveView` / `loadView` copy out and in with one assignment, as the status
group is copied today. One struct means a new window field cannot be
forgotten half-way: it is either in `windowState` (per worktree) or it is
process state. A fresh slot's group starts empty (`layers` is seeded with
an empty stack as the Model's is). The checklist for "is it a window field" is `closeFilesView`
(`files_view.go`): everything that function resets belongs to the group,
plus the layer stack, the popups' pending state and the steer leftovers.

| Model field(s) today | role | per worktree? |
|---|---|---|
| `layers` (full-screen views AND centred popups) | the window pile | yes |
| `files*` (mode, view, title, context, commit, hash, left/right, compareTag, comparePair, sets, linkCompareWant, stashTag, shelf*, returnFocus, returnLayers, treeFocused, readInflight, preview, full, landNote, landScope, back, pairLabel, review, previewSet/Counts/Groups/Reviews) | the files view | yes |
| `wtFiles` | the F window's on-disk list | yes (`all` and `filesView.lines` dropped on sleep, see Memory) |
| `previewOpen`, `previewFull`, `wtPreviewGen` | the preview beside the files view | yes |
| `pendingCompare` | the compare-mode picker's focused file | yes |
| `noteLand`, `diffLand`, `hunkReload`, `diffNav`, `diffNotice` | what an open diff owes its reader | yes |
| `pendingHint`, `hintGen` | a review hint waiting on a load | yes |
| `stashView` | the stash list over Commits | yes |
| `diffTag`, `compareTag`, `filesHash`, `linkCompareWant`, `filesStashTag` | pair an async result with its window | yes (they travel with the window) |
| window generations: `versionsGen`, `previewGen`, `wtPreviewGen`, `reviewsFollowGen`, `reviewOpenGen`, `entryCompareGen`, `gitConfigGen`, `hintGen` | "is this result for the window that is open" | yes (see Generations) |
| `histWalks` | the streaming file-history walks | process-wide (one set), but `historyLive` scans every slot |
| `attention` | `gg session highlight` bands | working-file bands yes; commit bands stay repo-wide |
| `pendingSteer` | a navigate waiting on a load | yes |
| `consoleSwitch.tour` | a tour waiting on the slot's status | yes |
| `resumePromptShown` | the paused-op prompt's one-shot | yes (already) |
| `focus`, `lastLeftPanel`, `activeLeftTab`, `fullMax`, `fullMaxed` | where the keyboard is, the pin | **no** — alt+w's first-hit rule and `showConsole`'s Commits-column invariants read them process-wide |
| `eager` | the ctrl+f deep search | **no** — it walks the shared feed |
| `startAtCmd`, `startAtPending` | a `--at` navigate | **no** — fires within one Update, never outlives a swap |
| `modal`, `proc`, `console`, `notices*`, `actionMenu`, `recall*`, `filterTyping`, `highlightTyping` | the operation's surfaces, the console, process-wide input | no |
| `sel[panelCommits]`, `sel[panelBranches]`, … (non-Status cursors) | cursors on shared lists | no (shared, as today) |

`sleepView` stops dropping anything window-shaped: no
`dropWorkingTreeWindows`, no attention pruning of working-file bands, no
tour clearing. `switchView` becomes: refuse → save the status group AND
the window group → sleep (watchers, slot-data gens) → load both groups →
kick. The leaving worktree's windows are exactly where it left them when
it comes back.

`dropWorkingLayers`, `dropWorkingTreeWindows`, `filesViewIsWorkingTree`,
`workingSide`, the attention pruning and `pendingSteer.svc` are deleted in
phase 2. `workingTreeWindow`, `parkedLayersFor` and `consoleReturn.over`
guard the console's process-wide parked copy and go in phase 4, when that
copy moves onto the slot. Their tests become "waits in its worktree and is
back on return" tests (below).

### Generations

Two kinds of generation exist today and they must stay apart:

- **Slot-data generations** (`loadGen`, `srcGen`, `watchGen`,
  `docWatch.gen`, `workingReviewsGen`) stay Model-global and monotonic.
  `sleepView` bumps them so a status/feed/load read started for the
  leaving slot cannot land in the arriving one (round-1 ruling, kept).
- **Window generations** (`versionsGen`, `previewGen`, `wtPreviewGen`,
  `reviewsFollowGen`, `reviewOpenGen`, `entryCompareGen`, `gitConfigGen`,
  `hintGen`) answer "is this result for the window that is open". They
  move INTO `windowState` with the windows they guard. If they stayed
  global, a swap would not bump them (fine) but a popup reopened in B
  would bump the gen A's parked popup is waiting on, and A's result would
  be dropped on return.
- Two popups key on `loadGen` today although it is a slot-data gen: the
  all-notes read (`model.go` ~3532) and the remote-heads popup
  (`remoteHeadNamesMsg` / `remoteHeadsMsg`, ~1715/1738). `sleepView`
  bumps `loadGen`, so a parked one would stay "loading" forever. They get
  their own window gen (`allNotesGen`, `remoteHeadsGen`) in the group;
  `reRoot` still drops them because it drops every slot.

### Results addressed to a sleeping slot

A result that lands while its worktree sleeps must NOT be routed by
window TYPE across slots: `layerOf[*versionsPopup]` over every slot's
stack is the original bug again (A's result fills B's same-type popup).
And a handler is rarely a pure write to the window: about forty of them
also open views, move focus, set `statusMsg` or drain a `pending*` field
on the Model (`linkCompareLoadedMsg`, `handlePreviewOpenMsg`,
`commitFilesMsg`, `diffMsg` → `drainPendingDiff` / `loadNotesCmd`, the
stage/unstage-hunk handlers, `amendPrefill`, `conflictFile`,
`gotoLinkResolvedMsg`, `onAllNotes` writing `currentWorktree`, …).
Rewriting each to write "through the right group" is the blast radius
the review measured and rejected.

The mechanism is a **per-slot replay queue**:

- Every window-addressed message carries the slot key it was asked from
  (`for model.CheckoutKey`): the worktree-scoped ones already carry `svc`
  (round 1) and derive it; the repository-scoped ones (a commit diff, a
  compare, a versions read, remote heads, all notes, git config) gain the
  field at their command's creation (`m.viewed` at dispatch time).
- `Update` checks `for` first: `for != "" && for != m.viewed` → the
  message is appended to `m.views[for].windows.queue` and nothing else
  happens. A slot that is gone (pruned, abandoned) drops its queue.
- `loadView` drains the queue in order through the ordinary `Update`
  path, after the window group is live and before the kick. The handlers
  run exactly as they would have, over the Model state they expect.
- Until the worktree returns, the parked window shows what it showed when
  it was parked (a loading row, an empty popup). Nothing is re-requested.
- Generation checks still apply at replay: a result for a window that was
  closed before the swap (its gen moved) is dropped by its own handler.
- The queue is bounded (64 messages, oldest dropped) so a slot nobody
  returns to cannot grow without end; a dropped message leaves the window
  in its loading state, which its close discards.

The console's parked copy (below) does not use the queue: the console
shows over the LIVE slot, so `dispatchParkedAware` (`console.go`) keeps
putting the parked layers back for the handler, as today.

### The console over a slot

Today `captureReturn` copies full-screen views off the live stack while a
console shows (they would draw over it and take the keys), and
`closeConsole` restores them, filtered by `parkedLayersFor`. The copy
stays — it is what lets `dispatchParkedAware` route results to a parked
view and what keeps the keys off the hidden stack — but it moves onto
the slot: `windowState.consoleParked` holds the layers, the stash view,
the files view and the preview the console displaced. A swap under a
console (the console's own return to another worktree, alt+w while a
console shows) saves and restores the parked copy with the rest of the
group, so A's displaced windows never reappear over B.

`consoleReturn` keeps `view`, `focus`, `full`, `fullMaxed`, `fullMax`;
`layers`, `stashView`, `filesView`, `filesPreview` and `over` move to the
slot's `consoleParked`. `closeConsole` restores from the live slot's
`consoleParked`; the filter `parkedLayersFor` goes.

### Swapping over a popup

Popups have no "typing" flag in general (a text-field popup is always
being typed into when it has the keyboard), so `cycleReachable` cannot
decide by a flag. Instead the window group knows which popups are
**parkable**: a `parkable()` method on the popup layer, default false,
true for the popups whose pending work is either none or a plain
read that the replay queue can hold (versions, remote heads, all notes,
git config, the add-worktree / branch / stash / note / template editors).
Not parkable — the swap is refused as today: a review loading
(`reviewOpenGen` in flight with a running task), the agent-start popup
while detecting, a link compare while busy, a PR / forge send in flight,
the notices dialog, the sessions popup (incl. its quit mode) and the
command palette.

`switchView` gains a caller-intent flag: `switchView(path, byUser bool)`.
With `byUser` (alt+w, alt+a, alt+t, the user's own worktree row) a
parkable popup on top is parked with the stack; without it (a console
show, a steer ask, a gone slot's return) any popup on top refuses as
today and the move queues through `pendingReturnView`. `steerRefusal`
(agent-triggered moves) is unchanged.

### Memory

- **One set of caches per repository.** `domain.Service.factory` vends
  six caches (diff, blame, sha-file, commit-files, compare-files,
  preview), each with its own 64 MiB budget, all keyed by content
  (commit + path, pair + path, rev + path) and all safe to share between
  worktrees of one repository. Working-tree diffs are never cached (they
  ask with `Key: ""`, `diff_view.go`; `compareDiffKey` returns "" for a
  working side) so nothing worktree-dependent can enter a shared cache.
  Sharing is explicit, not global: `domain.OpenTUISharing(path, home)`
  builds the slot's service over HOME's factory, and `ensureView` calls
  it. No process-wide map keyed by common dir: a repo switch drops the
  slots and their services, and the caches go with them. Less memory
  than now (the same commit diff viewed from two worktrees is one entry)
  and a diff cached through worktree A is a hit from B.
- **The F window's list is not parked.** On sleep, `wtFiles` keeps the
  window, its filter text (`query`, `field`) and its cursor path, and
  drops the on-disk list: `wtFiles.all = nil`, `wtFiles.untracked = nil`,
  `wtFiles.letters = nil`, `wtFiles.loading = true` and
  `filesView.lines = nil` (the rendered tree is as large as the list).
  `loadView` re-issues `loadLsFilesCmd` when the F window is open. ~100
  bytes per path on a million-file tree, twice (list + lines), is the one
  item worth not keeping six times.
- Everything else parked is bounded by what the user opened: a diff is
  capped at `MaxDiffBytes` (10 MiB) and usually far smaller, a blame or
  viewer is one file, a history is one page. Six large worktrees with
  windows left open cost tens of MB.

### Errors and edge cases

- A slot pruned or abandoned (its worktree gone) drops its windows and
  its replay queue with it; the queued return home carries no windows.
- `reRoot` drops every slot and so every parked window and queue, as it
  does today.
- A parked popup whose result FAILED shows the failure when its worktree
  returns: the failure message replays like any other.
- `historyLive` (`history_view.go`) scans the live stack, the live slot's
  `consoleParked`, `filesReturnLayers` AND every sleeping slot's group;
  otherwise `sweepHistoryWalks` (run after every Update) would stop a
  parked history's git the moment its worktree sleeps.
- A parked `pendingSteer` whose wait expires (the 5 s TTL) is failed by
  the existing TTL sweep; the sweep looks at every slot's pending, not
  only the live one.
- The e2e goldens of `tui_worktree_switch_fast.toml` change where a
  window used to follow the swap.

## Phases

Each phase is one plan, built on `feat/fast-worktree-switch-2`, race
gate green, merged separately into `feat/fast-worktree-switch` after the
user says so.

1. **Shared caches** (domain only): `OpenTUIWithFactory` / the sharing
   constructor; two services of one repository hit the same diff entry.
2. **The window group + swap** (popups still refused): `windowState`,
   `saveView`/`loadView` copy the group, the window gens move in (incl.
   the two new ones), `historyLive` over sleeping slots, the survival
   rules deleted, the survival tests reworked into "waits in its
   worktree". The `for` key is stamped on messages but a result for a
   sleeping slot is dropped with a loading state, not yet queued.
3. **The replay queue**: messages for a sleeping slot are queued and
   replayed on `loadView`.
4. **The console's parked copy on the slot**: `consoleParked`,
   `consoleReturn` slimmed, `parkedLayersFor` deleted.
5. **Parkable popups**: `parkable()`, `switchView(path, byUser)`,
   `cycleReachable` over a parkable popup.
6. **F-window memory**: the list and lines dropped on sleep, re-read on
   return.

## Testing

Replace, in `switch_audit_test.go`, `switch_audit2_test.go`,
`review_fixes_test.go`, `round2*_switch_test.go`:

- "a working diff / blame / viewer / F window / compare-with-working-side
  closes on the swap" → "waits in its worktree: absent over B, back over A
  with its cursor".
- "a commit diff survives the swap" → "waits in its worktree".
- "the console's parked working layers are dropped over another
  worktree" → "the console's return loads the slot and its parked copy
  comes back with it; nothing of A shows over B".
- attention bands, parked tour, parked status retry: "wait in their
  worktree; the arriving worktree's status does not drain A's retry".

New, per phase:

1. Two services of one repository share the diff cache (domain): a diff
   loaded through A is a hit through B; a plain `OpenTUI` does not share.
2. A popup (versions) opened in A, swap to B: B shows no popup, the window
   gens of B start fresh, a versions popup opened in B gets its own
   result; back in A the popup is there. A history parked in A keeps its
   walk running (`sweepHistoryWalks` leaves it). The all-notes and
   remote-heads popups parked in A are not stuck by the `loadGen` bump.
3. A result for A landing while B is shown is queued; back in A it is
   applied and the popup shows loaded. A result for a closed window is
   dropped by its gen at replay. The queue caps at 64.
4. A console shown over A's parked diff, return to B: B shows no diff;
   `dispatchParkedAware` still routes a result to the parked diff.
5. alt+w over a parkable popup swaps; over the palette it refuses; a
   console show over a parkable popup still queues.
6. F window: `all` and `lines` dropped on sleep, re-read on return,
   filter and cursor kept.

## Documentation to update at the end

CHANGELOG (the "Fast worktree switch" section: the survival bullets become
one paragraph on worktree-owned windows; the round-1 "an open commit diff
survives" is reworded), README (alt+w / alt+a rows: "each worktree keeps
its own windows"), `docs/CLAUDE-details.md` (the slot section: the
`windows` group, the window gens, the replay queue, `consoleParked`,
`parkable()`, the shared factory), `CLAUDE.md` `tui` row only if the
layer-stack convention sentence changes.
