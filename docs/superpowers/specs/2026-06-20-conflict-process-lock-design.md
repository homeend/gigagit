# The process abstraction + conflict resolution as the first process — design

**Date:** 2026-06-20
**Status:** Approved (design)
**Related:** follows the overlay-stack popup migration
([[overlay-stack-feature]], [[overlay-stack-simple-popups-feature]]). This spec
deliberately does **not** put the conflict popup on the popup stack — conflict
resolution is a process, not a popup.

## Problem

Merge / rebase conflict resolution is a **long-running task with side effects**
that proceeds in stages over time, and whose state lives on disk between stages
(and across app restarts). Today it is a popup that **re-summons itself**: after
each resolve it closes, fires one small git command, and a flag re-opens it on
the next refresh. Meanwhile the rest of the interface is only loosely held back
by a `running` flag checked by hand in ~a dozen key handlers and missed in
others, so unrelated popups and commands can still slip in.

This is the "windows opening windows" tangle: the conflict window manages its
own lifecycle, hands off to a full-screen editor, and reappears on its own,
while the interface stays open underneath.

## The model: two levels

Separate **showing a window** (a passive utility) from **deciding when a window
is shown** (a task's logic), with two named levels:

- **Job** — one operation that does a single thing and either succeeds or
  fails. It may take real time in a big repo, so it reports progress and can be
  cancelled, but it is *one unit with one outcome*: resolve this file, mark
  resolved, continue the rebase, create a worktree, refresh, commit. This is
  essentially what an engine operation already is.
- **Process** — a *set of jobs* with its own problem domain, rules, error
  handling, and resolution logic. It is the coordinator: it runs jobs one after
  another, reacts to each one's success or failure, owns which window is shown,
  and owns the interface while it is active. The **conflict resolution process**
  is the first one.

### Responsibility split

Each concern lives in exactly one place:

- **Jobs / the queue own mechanics:** serialize, supersede, coalesce, report
  progress, cancel, and block *conflicting* jobs. (The per-repo reservation gate
  already does most of this at the data layer.) "A refresh must block other
  operations that would act on the about-to-change state" is a job-relationship
  rule here — not an interface takeover.
- **A process owns judgment:** the rules, the error handling, the resolution,
  and ownership of the interface + which window is shown while it runs.

Consequence: **the interface lock is a property of a process, not of a long
job.** A giant-repo refresh is *just a long job* — progress, cancel, blocks
conflicting jobs — but it does **not** take over the screen; you can still
scroll and read. Only a *process* routes input to itself and drives windows.
(We deliberately do not taxonomize jobs as "fast" vs "slow"; a job being slow
does not change what it is.)

## The process abstraction

### The slot (= the lock)

The app gains a **process slot** alongside the popup pile and the full-screen
pile. Empty = the app behaves normally. Filled = the app is "in a process":

- **All key input routes to the process** (one gate, replacing the scattered
  `!running` checks). The process interprets keys in the context of the window
  it is currently showing.
- **The process owns what is drawn** (its current window).
- **Every other command is inert** — opening a switcher / the action menu,
  starting an op, mutating a panel are no-ops while the slot is filled. A
  blocked command never performs a partial action; at most it sets a transient
  "busy" status.
- **An indicator** shows the active process and its live keys.
- **Quit (ctrl+c) always works.**

### The state machine

A process advances through explicit states; every transition is the process's
decision, never a window reopening itself. For conflict resolution:

- **Listing** — showing the conflicted-files window; waiting for a file + action.
- **Picking** — handed off to the full-screen line editor for a both-sides file.
- **Working** — a job is running; show progress; the only live key is cancel
  (where cancelling is safe).
- **Reporting** — a job failed; show the error; wait for acknowledge; return to
  Listing.
- **Finishing** — continue/abort succeeded; tear down and release the slot.

### Window orchestration

The process **presents** a window and **consumes its outcome**; the window is
passive. The process holds "the window I am showing now," routes input to that
window's logic, reads back the *intent* the window reports — "keep-ours on file
X", "editor finished with this resolved content", "error acknowledged" — and
decides the next window. Windows never open each other and never reopen
themselves. This is the orchestration the process exists to provide, and it is
shaped so future processes (interactive rebase, an interrupted pull) can reuse
it.

### Lifecycle — start by detection

The process is started by the app **noticing the repo is in a conflicted
merge/rebase**, not by the merge/rebase job "handing off." The merge/rebase job
just leaves the repo in that state; the app detects it (as it already detects
in-progress state today) and fills the slot. This also covers relaunching the
app into a half-finished rebase — a real monorepo case. The slot is released
when continue completes the merge/rebase or abort unwinds it.

## Conflict resolution as the first process

The concrete first instance, fulfilling exactly today's requirements.

- **State it carries:** the in-progress kind (merge/rebase), the current list of
  conflicted files, the selection, and the state-machine state above.
- **Jobs it runs:** resolve one file with a chosen action (keep ours / keep
  theirs / mark resolved / keep the modified side / delete / keep base, gated by
  conflict class); mark all resolved; continue; abort; plus the read that
  refreshes the conflicted-file list after each. The line editor, on completion,
  produces a resolve job. (These are the same small git commands as today —
  driven by the process instead of a self-reopening popup.)
- **Rules:** continue only when nothing is left unresolved; both-sides files must
  go through the editor; per-file actions gated by conflict class.
- **Errors:** a failed job → Reporting (show why) → back to Listing with a
  refreshed list; the process is never lost.
- **Resolution:** the slot releases only when continue completes the merge/rebase
  or abort unwinds it.

The conflicted-files window and the full-screen line editor become **passive
surfaces the process shows** — they no longer decide when they appear.

## What this changes / removes

- The hand-checked `!m.running (&& !m.loading)` conditions across key handlers
  collapse into the single slot gate (per-handler checks that drive
  footer/menu *enablement* may remain; input *correctness* no longer depends on
  them).
- The conflict popup leaves the popup pile; its self-reopen flag and
  per-keystroke open/close lifecycle are removed; the process owns the flow.
- The conflicted-file list and the line editor become passive windows.

## Behavior (acceptance)

- Entering a conflicted merge/rebase fills the slot: opening the bookmark/shelf
  switcher, the action menu, starting another op, and state-mutating panel keys
  are all no-ops; the indicator shows the process and its live keys.
- The file list, the line-editor hand-off, and progress all appear because the
  process shows them — never because a window reopened itself.
- A failed resolve shows the error and returns to the list with a refreshed set,
  with the process still active.
- Resolving the last file and continuing (or aborting) releases the slot and the
  panels behave normally again.
- Quit always works; blocked commands never perform a partial action.

## Testing

- **Slot gate:** with the slot filled, representative commands (open a switcher,
  open the action menu, start an op) are no-ops and the active process is
  unchanged; the indicator/allowed-keys are present; the process's own keys are
  delivered.
- **State machine:** drive a scripted conflicted state through the process —
  Listing → resolve a file (Working → job done → Listing with the shorter list)
  → a both-sides file (Picking → editor returns → Listing) → last file resolved
  + continue (Finishing → slot released). Use the existing real-git / fake-runner
  conflict scaffolding.
- **Error path:** a failed resolve → Reporting → Listing, slot still filled.
- **Abort** releases the slot.
- **Detection start:** a model already in a conflicted state fills the slot on
  load (covers relaunch into a half-finished rebase).
- Regression: existing conflict-resolution tests are ported to drive the process
  rather than the old self-reopening popup.

## Scope & build order

This spec covers **Stage 1**: the process abstraction (slot + lock + state
machine + window orchestration + detection-start) and conflict resolution as its
first instance.

Two further stages follow as their own spec/plan cycles (out of scope here):

- **Stage 2 — finish the simple popups:** move the plain help/cheat-sheet,
  reword, and rename popups onto the popup pile (the Branch-1 pattern). Unrelated
  to the process model.
- **Stage 3 — unify the UI stacks:** fold the popup pile and the full-screen pile
  into one window system; the process then drives windows through it as just
  another client.

**Unchanged:** the decision modal and the action menu stay as they are; the
engine's operation/decision contract and the per-repo gate are reused, not
replaced.
