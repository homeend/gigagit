# Per-worktree window stacks — phase 3: the replay queue

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (this repo's CLAUDE.md forbids implementer subagents; the one session that wrote the plan executes it). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A window-addressed result that lands while its worktree sleeps is no longer dropped: it waits in that slot and is applied, through the ordinary handler, when the worktree returns. The parked window shows what it showed when parked until then.

**Architecture:** Phase 2's gate (`gateSlotMsg` in `Update`) already identifies a message addressed to a slot not on screen. Phase 3 appends such a message to `worktreeView.queued` (bounded, oldest dropped) when the slot exists; a gone slot's message stays dropped. `loadView` moves the arriving slot's queue into `Model.replay`; the Update tail (where `viewKick` launches the slot's wake-up) drains `replay` by dispatching each message through `Update` itself (the slot is on screen now, so the gate passes and the handler runs over the state it expects) and batches the commands they return. Tests reach the drain through `replayQueued()` directly.

**Tech Stack:** Go 1.26, Bubble Tea, `internal/tui` test helpers (`loadedModel`, `addWorktree`, `forceSwitch`), real `git` in `t.TempDir()`.

**Spec:** `docs/superpowers/specs/2026-10-09-per-worktree-window-stacks.md` ("Results addressed to a sleeping slot").

## Global Constraints

- Work only in `/work/gigagit/.claude/worktrees/fast-worktree-switch-2` (branch `feat/fast-worktree-switch-2`).
- The queue holds `tea.Msg` values as they arrived; nothing is re-requested and nothing is rewritten.
- `replayQueued` dispatches through `Update` (not `dispatch`): the slot gate, the parked-aware console routing and the Update tail's invariants all apply to a replayed message as to a live one.
- Bound: 64 messages per slot; the oldest is dropped. A dropped message leaves its window in its loading state, which the window's own close discards.
- TDD per task; a test that passes at once is probed. `gg add` + `git commit -F`; attribution lines; never push. `./test.sh race` before asking to merge.

## Review Focus

1. Re-entrancy: `replayQueued` calls `m.Update(msg)` for each queued message, and `Update`'s tail is where `replayQueued` is called from. The tail must take `replay` out of the Model BEFORE dispatching (`q := m.replay; m.replay = nil`) so a nested Update sees an empty queue and cannot loop.
2. A replayed message whose handler swaps the view (a `gotoLinkResolvedMsg` landing in another worktree, a console show): the remaining queue entries are for the slot that just left. The drain checks `m.viewed` after each dispatch and, if it moved, puts the rest back on THAT slot's `queued` (the leaving slot) rather than applying them to the new one.
3. The gone slot: `gateSlotMsg` must keep dropping a message whose slot has no entry in `m.views`; only an existing sleeping slot queues.
4. `reRoot` and `pruneViews`/`abandonGoneView` drop the slot and its queue together (nothing to do); `adoptView` keeps the slot (the queue survives an adopt).

---

### Task 1: Queue on the slot, drain on return

**Files:**
- Modify: `internal/tui/slot_msg.go` (`gateSlotMsg` → `routeSlotMsg` that queues or drops), `internal/tui/worktree_view.go` (`worktreeView.queued`, `loadView` moves it to `m.replay`), `internal/tui/model.go` (`replay []tea.Msg` field; the tail's `viewKick` arm calls `replayQueued`)
- Create: `internal/tui/slot_replay.go` (`replayQueued`, `queueCap = 64`)
- Test: `internal/tui/slot_replay_test.go`

**Interfaces:**
- `worktreeView.queued []tea.Msg` — results that landed while the slot slept, oldest first.
- `Model.replay []tea.Msg` — the arriving slot's queue, taken by `loadView`, drained by the Update tail.
- `func (m Model) routeSlotMsg(msg tea.Msg) (Model, bool)` — true when msg was queued or dropped (addressed elsewhere).
- `func (m Model) replayQueued() (Model, tea.Cmd)` — dispatches `m.replay` in order through `Update`, batches the commands.

- [ ] **Step 1: Failing tests**

```go
// A versions read asked from A lands while B is shown: nothing changes on
// screen; back in A the popup shows loaded.
func TestResultForASleepingSlotReplaysOnReturn(t *testing.T) {
	m := loadedModel(t)
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	m, cmdA := m.openBranchVersions("main", false, false)
	m = forceSwitch(t, m, other)
	nm, _ := m.Update(cmdA())
	m = nm.(Model)
	if layerOf[*versionsPopup](m) != nil {
		t.Fatal("A's popup shows over B")
	}
	m = forceSwitch(t, m, home)
	m, _ = m.replayQueued()
	if p := layerOf[*versionsPopup](m); p == nil || p.loading {
		t.Fatal("A's result was not replayed on return")
	}
}

// A result for a window closed before the swap is dropped at replay by
// its own generation.
func TestReplayDropsAResultForAClosedWindow(t *testing.T) { /* open, pop, swap, deliver, swap back, replay: no popup, no statusMsg */ }

// The queue is bounded: the oldest goes.
func TestSlotQueueIsBounded(t *testing.T) { /* 70 stamped messages for a sleeping slot → len(v.queued) == 64, the first kept is the 7th */ }

// A message for a slot that is gone is dropped, never queued.
func TestResultForAGoneSlotIsNotQueued(t *testing.T) { /* stamp with an unknown key; no slot gains a queue */ }

// A replayed message that moves the view leaves the rest on the slot that
// left: they are not applied to the new worktree.
func TestReplayStopsWhenTheViewMoves(t *testing.T) { /* queue [a message whose handler switches away (a consoleShow-like), then a versions msg]; after replayQueued the versions msg is back on the leaving slot's queued */ }
```

- [ ] **Step 2: Watch them fail** (today: dropped at the gate; `replayQueued` undefined).
- [ ] **Step 3: Implement** — `routeSlotMsg`: if `k != "" && k != m.viewed`: `if v := m.views[k]; v != nil { v.queued = append(v.queued, msg); if len > queueCap { v.queued = v.queued[len-queueCap:] } }`; return true. `loadView`: `m.replay = append(m.replay, v.queued...); v.queued = nil`. `replayQueued`: `q := m.replay; m.replay = nil; for i, msg := range q { was := m.viewed; nm, c := m.Update(msg); m = nm.(Model); cmds = append(cmds, c); if m.viewed != was { if v := m.views[was]; v != nil { v.queued = append(q[i+1:], v.queued...) }; break } }`. The Update tail: where `m.viewKick` is consumed, `if len(m.replay) > 0 { m, c = m.replayQueued(); cmds = append(cmds, c) }`.
- [ ] **Step 4: Green; probe** (make `routeSlotMsg` drop instead of queue → the first test fails); **Step 5: package green; commit** `feat(tui): a result for a sleeping worktree waits in its slot and replays on return`.

### Task 2: Docs, race gate, merge request

- [ ] CHANGELOG (the phase-2 paragraph: "kept off the one shown" → "and applied when that worktree returns"), `docs/CLAUDE-details.md` (the gate paragraph: queue + replay, the re-entrancy rule, the view-moved rule), the spec's phase list unchanged.
- [ ] `./test.sh race` → "all green"; `go build -o bin/gg ./cmd/gg`; memory; ASK before the merge into `feat/fast-worktree-switch`.
