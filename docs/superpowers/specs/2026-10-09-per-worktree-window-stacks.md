# Per-worktree window stacks — design

Date: 2026-10-09. Status: agreed in conversation, awaiting written review.
Builds on `2026-10-08-fast-worktree-switch-design.md` (the slots) and the
three audit rounds merged into `feat/fast-worktree-switch` (last `7cbbcb5c`).

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
  results").
- Top-level, never parked: an operation's decision modal, an interactive
  process, the agent console, notices.
- Agent-triggered swaps (a console show, a steer ask, a gone slot) still
  wait while the user types in a popup; the user's own alt+w / alt+a may
  swap over a parkable popup.
- Memory: the commit-diff cache is per REPOSITORY, not per service; the F
  window drops its on-disk file list when its worktree sleeps.

## Not in scope

- Per-worktree copies of the repository lists or the commit feed. They
  stay shared and `markHead` / the feed re-root keep marking them for the
  viewed worktree.
- Another repository: `reRoot` stays the full reload and drops every slot.
- gg web: the hosted page keeps following gg's own worktree.
- The decision modal and `m.proc`: a swap is refused while an operation
  runs or a process owns the screen, as today.

## The mechanism

### What moves into the slot

The Model keeps its fields as the LIVE copy (no reader changes); the slot
gains a `windows` group that `saveView` / `loadView` copy out and in, as
the status group is copied today. The group is one struct so the copy is
one assignment and a new window field cannot be forgotten half-way: it is
either in `windowState` (per worktree) or it is process state.

| Model field(s) today | role | per worktree? |
|---|---|---|
| `layers` (full-screen views AND centred popups) | the window pile | yes |
| `files*` (mode, view, title, context, commit, hash, left/right, compareTag, comparePair, sets, linkCompareWant, stashTag, shelf*, returnFocus, returnLayers, treeFocused, readInflight, preview) | the files view | yes |
| `wtFiles` | the F window's on-disk list | yes (list dropped on sleep, see below) |
| `stashView` | the stash list over Commits | yes |
| `diffTag`, `compareTag`, `filesHash`, `linkCompareWant`, `filesStashTag` | pair an async result with its window | yes (they travel with the window) |
| `attention` | `gg session highlight` bands | yes |
| `pendingSteer`, `startAtCmd`/`startAtPending` | a navigate waiting on a load | yes |
| `consoleSwitch.tour` | a tour waiting on the slot's status | yes |
| `focus`, `lastLeftPanel`, `activeLeftTab`, `fullMax`, `fullMaxed` | where the keyboard is, the pin | yes |
| `eager` | the ctrl+f deep search | yes |
| `resumePromptShown` | the paused-op prompt's one-shot | yes (already) |
| `modal`, `proc`, `console`, `notices*`, `actionMenu`, `recall*`, `filterTyping`, `highlightTyping` | the operation's surfaces, the console, process-wide input | no |
| `sel[panelCommits]`, `sel[panelBranches]`, … (non-Status cursors) | cursors on shared lists | no (shared, as today) |

`sleepView` stops dropping anything: no `dropWorkingTreeWindows`, no
attention pruning, no tour clearing. `switchView` becomes: refuse → save
the status group AND the window group → sleep (watchers, gens) → load
both groups → kick. The leaving worktree's windows are exactly where it
left them when it comes back.

`workingTreeWindow`, `dropWorkingLayers`, `dropWorkingTreeWindows`,
`filesViewIsWorkingTree`, `workingSide`, `parkedLayersFor`,
`consoleReturn.over`, the attention pruning and `pendingSteer.svc` are
deleted. Their tests become "waits in its worktree and is back on return"
tests (below).

### Results addressed to a parked window

Every async result that targets a window reaches the window object, not
"the window on screen":

- A result found by window TYPE (`layerOf[*versionsPopup](m)`,
  `layerOf[*remoteHeadsPopup]`, …) searches the live stack first, then
  every sleeping slot's stack. The window is a pointer; writing to a
  parked one is safe since it is neither drawn nor keyed.
- A result gated by a window FIELD on the Model (`diffTag`,
  `compareTag`, `filesHash`, `linkCompareWant`, `filesStashTag`) resolves
  the owning window group first: the live one when the result's worktree
  is the viewed one, else the sleeping slot's. The worktree-scoped
  results already carry `svc` (round 1); the repository-scoped ones (a
  commit diff, a compare) carry the slot key they were asked from. A
  helper `windowsFor(key) *windowState` returns the live group or the
  slot's; handlers write through it instead of `m.filesX = …`.
- Generations (`versionsGen`, `loadGen`, `srcGen`, …) stay Model-global
  and monotonic (round-1 ruling): a result for a reopened popup is still
  dropped by its gen.

Nothing is re-requested on return; the window shows what landed while it
waited.

### The console over a slot

Today `captureReturn` copies full-screen views off the live stack while a
console shows (they would draw over it and take the keys) and `closeConsole`
restores them, filtered by `parkedLayersFor`. With slot-owned stacks the
copy goes: the slot's stack keeps its entries and the console marks them
DORMANT (`windowState.dormantBelow`: the stack height when the console was
shown). Dormant entries are not drawn and not keyed; a popup opened while
the console shows goes above the mark and behaves as today. `closeConsole`
clears the mark. A console's return (`consoleReturn.view`) is then only
"load that slot": the slot's windows come back by themselves, in the
worktree they belong to.

`consoleReturn` keeps `view`, `focus`, `full`; `layers`, `stashView`,
`filesView`, `filesPreview`, `over` go.

### Swapping over a popup

`cycleReachable` (alt+w, alt+a, alt+t) is extended: reachable when the top
layer is a centred popup that is not being typed into, as it already is
over a diff whose search is not being typed into. The popup is parked
with the stack. `switchRefusal` keeps refusing for an operation, a
process, a decision modal and the typing states; the surfaces that used
to refuse ONLY because they would submit through the wrong service (a
popup over the panels) no longer refuse — the service they submit through
is their worktree's, which is live again when they are.

`steerRefusal` (agent-triggered moves) is unchanged: an agent never moves
the screen under a popup the user is filling.

### Memory

- **One diff cache per repository.** `domain.Service.factory` (the
  `cache.Factory` vending the "diff" cache) becomes per git common dir:
  `factoryFor(commonDir)` in a process-global map, resolved like
  `gateFor`. Every service of one repository then shares one 64 MiB
  budget, keyed by commit + path as today. Less memory than now (the same
  commit diff viewed from two worktrees is one entry), and a diff cached
  through worktree A is a hit from B. Working-tree diffs are keyed by
  content hash already, so they do not collide across worktrees.
- **The F window's list is not parked.** On sleep, `wtFiles` keeps the
  window, its filter text and its cursor path, and drops the entries
  (`wtFiles.entries = nil; wtFiles.loading = true`); `loadView` re-issues
  the list read when the window is open. ~100 bytes per path on a
  million-file tree is the one item worth not keeping six times.
- Everything else parked is bounded by what the user opened: a diff is
  capped at `MaxDiffBytes` (10 MiB) and usually far smaller, a blame or
  viewer is one file, a history is one page. Six large worktrees with
  windows left open cost tens of MB.

### Errors and edge cases

- A slot pruned or abandoned (its worktree gone) drops its windows with
  it; the queued return home carries no windows.
- `reRoot` drops every slot and so every parked window, as it does today.
- A parked popup whose result FAILED shows the failure when its worktree
  returns, as it would have shown it live.
- A parked `pendingSteer` whose wait expires (the 5 s TTL) is failed by
  the existing TTL sweep; the sweep looks at every slot's pending, not
  only the live one.
- The e2e goldens of `tui_worktree_switch_fast.toml` change where a
  window used to follow the swap.

## Testing

Replace, in `switch_audit_test.go`, `switch_audit2_test.go`,
`review_fixes_test.go`, `round2*_switch_test.go`:

- "a working diff / blame / viewer / F window / compare-with-working-side
  closes on the swap" → "waits in its worktree: absent over B, back over A
  with its cursor".
- "a commit diff survives the swap" → "waits in its worktree".
- "the console's parked working layers are dropped over another
  worktree" → "the console's return loads the slot and its windows are
  back; a popup opened over the console sits above the dormant mark".
- attention bands, parked tour, parked status retry: "wait in their
  worktree; the arriving worktree's status does not drain A's retry".

New:

- A popup (versions) opened in A, swap to B, its result lands: the parked
  popup has the data; back in A it is shown loaded.
- A result gated by a window field (a commit diff asked from A, landing
  while B is shown) fills A's diff view, not B's state.
- alt+w over a popup swaps; typing in the popup's field refuses.
- Two services of one repository share the diff cache (domain).
- F window: entries dropped on sleep, re-read on return, filter kept.
- The TTL sweep fails a parked pending steer.

## Documentation to update at the end

CHANGELOG (the "Fast worktree switch" section: the survival bullets become
one paragraph on worktree-owned windows; the round-1 "an open commit diff
survives" is reworded), README (alt+w / alt+a rows: "each worktree keeps
its own windows"), `docs/CLAUDE-details.md` (the slot section: the
`windows` group, `windowsFor`, the dormant mark, the per-repository
factory), `CLAUDE.md` `tui` row only if the layer-stack convention
sentence changes.
