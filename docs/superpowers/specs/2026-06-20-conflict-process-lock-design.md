# Long-running process lock + conflict resolution as a process — design

**Date:** 2026-06-20
**Status:** Approved (design)
**Related:** follows the overlay-stack popup migration ([[overlay-stack-feature]],
[[overlay-stack-simple-popups-feature]]). This spec deliberately *does not* put
the conflict popup on the overlay stack — see "Why not just stack it."

## Problem

Merge / rebase conflict resolution is not a popup. It is a **long-running task
with side effects** that proceeds in stages over time (resolve a file, resolve
another, continue, or abort), and its state lives on disk between stages (and
across app restarts).

Today it is implemented as a popup that **re-summons itself**: after each
resolve it closes, fires one small git command, and a flag re-opens it on the
next data refresh. Meanwhile the rest of the interface is only loosely held
back — a `running` flag is checked by hand in roughly a dozen key handlers and
missed in others, so popups and commands can still sneak in during the flow.

This produces the "windows opening windows" tangle: the conflict window manages
its own lifecycle, hands off to a full-screen editor, and reappears on its own,
while unrelated popups remain reachable.

## The design principle

Separate **showing a window** (a passive display utility) from **deciding when a
window is shown** (the task's logic). A long task is the boss: it orchestrates
which window appears at each stage; windows never open other windows.

While such a task runs, the interface is **locked**: every command that is not
part of the task is rejected, the keys the user might *think* they can press are
switched off, and a clear indicator shows that something is in progress. When
the task ends or fails, the lock lifts and the interface returns to normal.

This is the grain of the codebase: underneath, a merge/rebase is already a task
that runs, reports progress, and pauses to ask the user a question. Two gaps
this spec closes: the conflict-resolution stage bypasses that model, and there
is no single authoritative "interface locked" mode.

## Architecture

Two parts. Part A is a general capability; Part B is its first real consumer.

### Part A — the interface lock ("active process" slot)

A single, authoritative gate at the top of input handling, backed by one piece
of state: the **active process** slot (empty = interface behaves normally).

While the slot is occupied:

- **All key input is routed to the active process.** The process handles the
  keys it cares about and ignores the rest, so every other command is a no-op
  by construction — nothing has to remember to check a flag. This is the same
  shape as the existing decision modal, generalized to a task that runs over
  time and can change which window it shows.
- **An app-wide indicator** shows that a process is running and which keys are
  live (e.g. "Resolving conflicts — [c]ontinue / [a]bort"). For a brief
  one-shot process this is just a spinner/label; for an interactive process the
  process supplies the hint.
- **Global escapes still work** (quit). The process defines any in-flow escape
  (e.g. abort).

Two kinds of process occupy the slot:

1. **One-shot op** (pull, push, commit, …): start → indicator → auto-finish.
   This already exists as the `running` flag; the change is that it now flows
   through the one gate, so input is uniformly blocked (closing the
   "some-handlers-forgot-to-check" gap) instead of relying on scattered
   per-handler `!running` tests.
2. **Interactive session** (Part B): spans multiple ops and user input, owns
   which window is shown.

The gate **replaces** the scattered `!m.running && !m.loading` checks with one
decision made before per-handler dispatch. Per-handler enablement (footer hints,
the `.` menu's available-actions list) continues to reflect what is runnable, but
correctness no longer depends on each handler remembering to re-check.

**Command rejection** is observable: an attempt to run a blocked command while a
process is active is a no-op with the indicator still showing (optionally a brief
"busy" status note), never a silent half-action.

### Part B — conflict / merge / rebase resolution as a process

A **conflict-resolution process** (a UI-side controller) occupies the active
slot whenever the repository is in a conflicted merge or rebase. It is the thing
that was previously spread across a self-reopening popup, a reopen flag, and
per-keystroke commands.

Responsibilities:

- **Owns which window is shown.** When waiting for input it shows the conflict
  **file-list window** (the same content as today's conflict popup, now a passive
  surface it drives). For a file conflicted on both sides it hands off to the
  full-screen **line picker**; when that returns, the process shows the file list
  again. While a resolve/continue/abort runs it shows **progress**.
- **Drives the small git commands** (resolve one file with a chosen action, mark
  all resolved, continue, abort) and waits for each to finish before deciding the
  next window. There is no "reopen myself" flag — the process re-shows its window
  because it owns the flow.
- **Lifecycle:** the process starts when the repo enters a conflicted
  merge/rebase (detected as today), and ends — releasing the lock — when the
  merge/rebase completes or is aborted. If conflicts remain after a step, the
  process simply shows the file list again.

**Where the controller lives: the UI side.** It owns rich windows *and* drives
the engine's small, UI-agnostic commands. This is what resolves the hard part —
"how does a running task show a rich window (not just a one-line choice)?" — the
task is a UI citizen, so rich windows are native to it. The engine's
pick-one-from-a-list decision channel is untouched and remains for *single-op*
forks (e.g. a smart-pull decision); the conflict session is a distinct,
multi-op, UI-owned mechanism and does not go through that channel.

### Why not just stack it

Putting the conflict popup on the overlay (popup) stack would keep treating it as
a popup that manages its own appearance and hands off to other windows — the very
framing this redesign rejects. The simple popups (help/reword/rename) *are* plain
popups and belong on the stack; conflict resolution is a process and belongs in
the lock/orchestration model. So the conflict popup is **removed from the popup
layer** and re-homed under the process controller.

## What this changes / removes

- The hand-checked `!m.running (&& !m.loading)` conditions scattered across key
  handlers collapse into one gate (the per-handler checks that drive footer/menu
  *enablement* may stay, but input *correctness* no longer depends on them).
- The conflict popup's self-reopen flag and per-keystroke open/close lifecycle
  are removed; the process controller owns the flow.
- The conflict file list and the full-screen line picker become passive surfaces
  the process shows; they no longer decide when they appear.

## Behavior (acceptance)

- Starting a process (e.g. a conflicted merge) locks the interface: opening the
  bookmark/shelf switcher, the action menu, panel navigation that mutates state,
  and other ops are no-ops; the indicator shows the process and its live keys.
- The conflict file list, line-picker hand-off, and progress all appear because
  the process shows them — never because a window reopened itself.
- Resolving the last file and continuing (or aborting) ends the process and
  unlocks the interface; the panels behave normally again.
- A blocked command never performs a partial action; at most it sets a transient
  "busy" status.
- Quit (ctrl+c) always works.

## Testing

- **Lock gate:** with a process active, assert representative commands (open a
  switcher, open the action menu, start another op) are no-ops and the active
  process is unchanged; assert the indicator/allowed-keys are present; assert the
  process's own keys are delivered.
- **One-shot op through the gate:** a running op blocks input uniformly and
  auto-releases on completion.
- **Conflict process flow:** drive a scripted conflicted state through the
  process — file list shown → resolve a file (op runs, file list re-shown with
  the shorter list) → both-sides file hands off to the picker and returns to the
  file list → last file resolved + continue ends the process and unlocks. Use the
  existing real-git/fake-runner conflict test scaffolding.
- **Abort path** ends the process and unlocks.
- Regression: existing conflict-resolution tests are ported to drive the process
  rather than the old self-reopening popup.

## Scope

**In scope:** Part A (the lock + indicator + command rejection) and Part B
(conflict/merge/rebase resolution rebuilt as a process). Build order: A first
(mechanism + tests), then B on top of it.

**Out of scope (separate, trivial follow-up):** moving the plain help/cheat-sheet,
reword, and rename popups onto the overlay stack — unrelated to the process model,
parked for a small later branch.

**Unchanged:** the decision modal and the action menu stay as they are; the
engine's operation/decision contract and the per-repo lock are reused, not
replaced; the eventual overlay/full-screen stack unification remains a later
effort.
