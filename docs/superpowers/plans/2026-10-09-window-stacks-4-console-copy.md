# Per-worktree window stacks — phase 4: the console's parked copy on the slot

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (this repo's CLAUDE.md forbids implementer subagents; the one session that wrote the plan executes it). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** What a console displaces (the full-screen layers it covers, the stash list, the files preview) is stored on the worktree slot it belongs to, not on the console's return point. A console's return to another worktree then brings that worktree's own displaced windows back and never A's over B; `parkedLayersFor`, `consoleReturn.over`, `workingTreeWindow` and `dropWorkingLayers` go.

**Architecture:** `windowState` gains `consoleParked *consoleParked` (`layers []layer`, `stashView`, `filesView`, `filesPreview`). `captureReturn` fills the LIVE slot's `consoleParked` (and `consoleReturn` keeps only `view`, `focus`, `full`, `fullMaxed`, `fullMax`). Since the group swaps with the slot, a console shown over A and returned to B finds B's `consoleParked` (usually nil) on close, and A's copy waits in A. `dispatchParkedAware` puts the live slot's parked layers back beneath the stack for a non-key message, as today, reading `m.consoleParked`. `historyLive` counts the live and every sleeping slot's parked copy (`windowState.holds` includes it). `steerRefusal`'s "top of the parked stack" reads the live copy. `forgetConsoleReturn` (a repo switch) clears the live slot's copy; `reRoot` drops the slots anyway.

**Spec:** `docs/superpowers/specs/2026-10-09-per-worktree-window-stacks.md` ("The console over a slot").

## Global Constraints

- Same worktree, same gates as the earlier phases (`-2` branch, TDD, `gg add` + `git commit -F`, race gate, never push).
- `showConsole` keeps nil-ing `m.stashView`/`m.filesPreview` and dropping the pin while the console shows; only WHERE the displaced windows are kept changes.

## Review Focus

1. A console replacing a console (`ret = m.console.ret`) must not capture again: the first console's copy is already on the slot; the second show finds `m.consoleParked != nil` and leaves it.
2. `dispatchParkedAware` restores and re-parks the LIVE slot's copy around a non-key dispatch; if the handler swapped the view (a steer ask landing), what is "still there" belongs to the slot that left: re-park onto `m.views[was].windows.consoleParked`, not onto the new live slot.
3. `closeConsole` after `returnView` restores from the slot on screen THEN; when the return is refused and queued (`pendingReturnView`), the close restores B's (nil) copy and A's waits — the queued return later loads A's group, whose `consoleParked` must then be restored too: `takeQueuedReturn`/`loadView` must put a slot's `consoleParked` back on the pile when NO console shows (`restoreConsoleParked` in `loadView` when `m.console == nil`).
4. `TestConsoleCloseDropsParkedWorkingLayersOverAnotherWorktree` becomes "the console's return to B shows nothing of A; back in A the diff is there".

---

### Task 1: `consoleParked` on the slot

**Files:** `internal/tui/window_state.go`, `internal/tui/console.go` (`consoleReturn`, `captureReturn`, `forgetConsoleReturn`, `dispatchParkedAware`, `closeConsole`, `parkedLayersFor` deleted), `internal/tui/history_view.go` (`historyLive`), `internal/tui/steer.go` (~224), `internal/tui/worktree_view.go` (`workingTreeWindow`/`dropWorkingLayers` deleted; `loadView` restores a slot's copy when no console shows), tests `switch_audit_test.go`, `alt_cycle_test.go` (426/432/490), `history_stream_test.go` (301).

- [ ] **Step 1: Failing tests** — `TestConsoleReturnToAnotherWorktreeShowsNothingOfTheFirst` (home's working diff parked under a console shown for a session in B; `ret.view = B`; close: top == nil, B's stack empty; switch back home: the diff is there). `TestParkedCopyFollowsAQueuedReturn` (console over A's diff, return to B refused by an op, close → B shows nothing; op ends → the queued return loads A and A's diff is back on the pile). `TestDispatchParkedAwareReparksOntoTheSlotThatLeft` (a non-key message whose handler swaps the view).
- [ ] **Step 2: Watch them fail. Step 3: Implement. Step 4: Green, probe, package, commit** `feat(tui): a console's displaced windows wait on their worktree's slot`.

### Task 2: Docs, race gate, merge

- [ ] CHANGELOG (the phase-2 paragraph's "alt+w's first hit" sentence → "a console's return brings back only that worktree's own windows"), `docs/CLAUDE-details.md` (the "console's process-wide parked copy" sentence replaced), `./test.sh race` → "all green", bin/gg, memory, merge into `feat/fast-worktree-switch`.
