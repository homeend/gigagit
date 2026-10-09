# Per-worktree window stacks — phase 2: the window group and the swap

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (this repo's CLAUDE.md forbids implementer subagents; the one session that wrote the plan executes it). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A worktree owns its windows. A swap between two worktrees of one repository saves the leaving worktree's layer stack, files/stash views, preview, pending diff/steer state and window generations into its slot and restores the arriving worktree's; the survival rules (`dropWorkingTreeWindows` and friends) are deleted. Popups still refuse the swap (phase 5); a result landing for a sleeping slot is dropped (phase 3 queues it).

**Architecture:** The `Model` embeds a new `windowState` struct holding every window field (readers keep `m.filesView`, `m.layers`, … by promotion); `worktreeView` gains `windows windowState`; `saveView`/`loadView` copy it in one assignment. Window generations move into the group; two popups that keyed on the slot-data `loadGen` get their own window gens. `historyLive` scans sleeping slots so `sweepHistoryWalks` leaves a parked history's walk running. Window-addressed messages carry the slot key they were asked from (`for`), and `Update` drops one addressed to a sleeping slot (the hook phase 3 turns into a queue). The console's process-wide parked copy (`captureReturn`, `parkedLayersFor`, `ret.over`) is untouched until phase 4.

**Tech Stack:** Go 1.26, Bubble Tea, `internal/tui` test helpers (`loadedModel`, `addWorktree`, `viewedOther`, `landView`, `startSessionIn`), real `git` in `t.TempDir()`, the e2e golden harness (`e2e/scenarios/tui_worktree_switch_fast.toml`, `-update`).

**Spec:** `docs/superpowers/specs/2026-10-09-per-worktree-window-stacks.md` ("What moves into the slot", "Generations", "Results addressed to a sleeping slot" — the drop half, "Errors and edge cases", "Phases" 2).

## Global Constraints

- Work only in `/work/gigagit/.claude/worktrees/fast-worktree-switch-2` (branch `feat/fast-worktree-switch-2`).
- `internal/tui` never imports `internal/git`. Every new user-visible string goes through `i18n.T` with keys in all four bundles (none are expected in this phase).
- `focus`, `lastLeftPanel`, `activeLeftTab`, `fullMax`, `fullMaxed`, `eager`, `startAtCmd`/`startAtPending`, the console, the modal, `proc`, notices, the action menu, the recall and typing flags stay on the Model.
- Slot-data gens (`loadGen`, `srcGen`, `watchGen`, `docWatch.gen`, `workingReviewsGen`) stay on the Model and keep being bumped by `sleepView`.
- TDD per task; a test that passes at once is probed by removing the change.
- `gg add <files>` then `git commit -F <msgfile>`; attribution lines; never `git add -A`, never push. `./test.sh race` before asking to merge.

## Review Focus

1. The embed must not leave a window field behind on the Model: Task 1 ends with a grep over `closeFilesView`'s assignments and the field table of the spec — every name resolves to `windowState`. A field left on the Model crosses the swap silently.
2. A fresh slot's `layers` is nil: `pushLayer` seeds it (`layer_stack.go:70`), but any reader doing `m.layers.entries` without a nil check panics on the first key over a fresh slot. Task 2's test opens a diff in B right after the first swap.
3. `sleepView` must keep bumping `loadGen`/`srcGen`; `loadView` must NOT touch the window gens (they are the slot's). The remote-heads and all-notes popups move from `loadGen` to their own gens — `reRoot` still has to drop them (it drops the slots).
4. `historyLive` over sleeping slots: a walk whose view is parked in a sleeping slot keeps its git running until that slot is dropped (`pruneViews`/`abandonGoneView`/`reRoot` must stop the walks of a dropped slot: Task 4 adds `stopSlotWalks`).
5. The console: `captureReturn` moves layers from the LIVE stack into `console.ret.layers`; after a swap under the console (`returnView`), `closeConsole` restores them through `parkedLayersFor` over the slot now live. That filter stays in this phase; `TestConsoleCloseDropsParkedWorkingLayersOverAnotherWorktree` keeps passing unchanged.

---

### Task 1: `windowState`, embedded in the Model

**Files:**
- Create: `internal/tui/window_state.go`
- Modify: `internal/tui/model.go` (the field block ~lines 75–345: move the fields; the embed)
- Modify: `internal/tui/worktree_view.go` (`worktreeView` gains `windows windowState`)
- Create: `internal/tui/window_state_test.go`

**Interfaces:**
- `type windowState struct { … }` — the per-worktree window fields (list below).
- `Model` embeds `windowState` (an unnamed field), so `m.filesView` etc. keep compiling.
- `worktreeView.windows windowState`.

Fields that move (from the Model's declarations; the comments move with them):

- the pile: `layers *layerStack`
- the files view: `filesMode`, `filesView`, `filesTitle`, `filesContext`, `filesCommit`, `filesHash`, `filesLeft`, `filesRight`, `compareTag`, `comparePair`, `filesSets`, `linkCompareWant`, `filesStashTag`, `filesShelfID`, `filesShelfLabel`, `filesShelfNotes`, `filesReturnFocus`, `filesReturnLayers`, `filesTreeFocused`, `filesReadInflight`, `filesPreview`, `filesFull`, `filesLandNote`, `filesLandScope`, `filesBack`, `filesPairLabel`, `filesReview`, `filesPreviewSet`, `filesPreviewCounts`, `filesPreviewGroups`, `filesPreviewReviews`, `wtFiles`, `previewOpen`, `previewFull`, `wtPreviewGen`, `previewGen`, `pendingCompare`
- the diff's leftovers: `diffTag`, `diffNav`, `diffNotice`, `noteLand`, `diffLand`, `hunkReload`
- the stash list: `stashView`
- steering leftovers: `pendingSteer`, `pendingHint`, `hintGen`, `tour` (moved OUT of `consoleSwitch` — `consoleSwitch.tour` becomes `m.tour`; `consoleSwitch` keeps `armed`, `open`, `gen`)
- window gens: `versionsGen`, `reviewsFollowGen`, `reviewOpenGen`, `entryCompareGen`, `gitConfigGen`, plus NEW `allNotesGen`, `remoteHeadsGen` (Task 3)
- bands: `attention map[attentionKey][]steerMark` stays on the Model (commit bands are repo-wide); the group gains `workingAttention map[attentionKey][]steerMark` and `saveView`/`loadView` move the `commit == ""` entries between the two (Task 2). Readers stay on `m.attention`.

NOT moved: `diffPartial`, `diffLong`, `diffImgLayout`, `diffCursor`, `diffStacked` (session preferences), `resumePromptShown` (already a slot field), everything in the Global Constraints.

- [ ] **Step 1: Write the failing test**

```go
// internal/tui/window_state_test.go
package tui

import "testing"

// The window fields live in one group: saving the live group into a slot
// and loading an empty one leaves no window on the Model, and loading the
// saved one brings every field back. The test touches one field of each
// family; the compile-time guarantee is the embed itself.
func TestWindowStateRoundTrip(t *testing.T) {
	m := loadedModel(t)
	m = m.pushLayer(&diffView{title: "f"})
	m.filesView = &contentPopup{}
	m.filesMode = filesModeWorktree
	m.wtFiles = &worktreeFiles{query: "q"}
	m.stashView = &stashView{}
	m.diffTag = "tag"
	m.pendingSteer = &pendingSteer{}
	m.versionsGen = 7
	saved := m.windowState
	m.windowState = windowState{}
	if m.topLayer() != nil || m.filesView != nil || m.wtFiles != nil || m.stashView != nil || m.diffTag != "" || m.pendingSteer != nil || m.versionsGen != 0 {
		t.Fatal("a window field survived an empty group")
	}
	m.windowState = saved
	if m.topLayer() == nil || m.filesView == nil || m.wtFiles == nil || m.wtFiles.query != "q" || m.stashView == nil || m.diffTag != "tag" || m.pendingSteer == nil || m.versionsGen != 7 {
		t.Fatal("a window field did not come back")
	}
}
```

- [ ] **Step 2: Watch it fail to compile** (`m.windowState` undefined).

- [ ] **Step 3: Grep `Model{` in `internal/tui` (tests, `Headless`, the snapshot) for window-field names: every keyed literal that names one must move it under `windowState: windowState{…}`.** Then **create `window_state.go`** with the struct and its doc comment (why: a worktree owns its windows; the checklist = `closeFilesView`; what stays out and why), move the field declarations out of `model.go` into it, and add `windowState` as the first unnamed field of `Model`. Move `tour` out of `consoleSwitch`; fix its three readers (`console_scope.go`, `sleepView`, the tour filing in `agent_tours*.go`) to `m.tour`. `go build ./... && go vet ./internal/tui`.

- [ ] **Step 4: Field audit** — `grep -o 'm\.\w*' internal/tui/files_view.go` over `closeFilesView`'s body and the spec's table; each name must be declared in `window_state.go` (a shell loop over `grep -c "^\s*<name> " internal/tui/window_state.go`). List the result in the commit message.

- [ ] **Step 5: Tests green**: `go test ./internal/tui -run 'TestWindowStateRoundTrip' && go test ./internal/tui 2>&1 | tail -3`.

- [ ] **Step 6: Commit** `refactor(tui): the window fields of the Model form one embedded windowState group`.

### Task 2: The swap copies the group; the survival rules go

**Files:**
- Modify: `internal/tui/worktree_view.go` (`saveView` ~172, `loadView` ~192, `switchView` ~298, `sleepView` ~374, delete `workingTreeWindow`'s companions ~404–600: `dropWorkingLayers`, `dropWorkingTreeWindows`, `filesViewIsWorkingTree`, `workingSide`; keep `workingTreeWindow` for `parkedLayersFor`)
- Modify: `internal/tui/worktree_view.go` `ensureView` (seed `windows.layers = &layerStack{}`)
- Modify: `internal/tui/steer_nav.go` (`pendingSteer.svc` and the `drainPendingStatus` service check go: the pending is the slot's)
- Modify: `internal/tui/steer_attn.go` (no change to readers; the pruning in `sleepView` goes)
- Modify tests: `internal/tui/switch_audit_test.go` (`TestSwitchViewDropsWorkingTreeLayers`), `round2_switch_test.go` (`TestSwitchViewKeepsACommitsFilesWindow`, `TestSwitchViewClosesACompareWithAWorkingSide`), `round2c_switch_test.go` (`TestSwitchViewDropsAParkedTour`, `TestSwitchViewDropsWorkingTreeAttentionMarks`, `TestParkedStatusRetryDoesNotRunAgainstAnotherWorktree`)

- [ ] **Step 1: Write the failing tests** (replace the three survival tests; the new names say what is pinned):

```go
// A working diff, a commit diff, a viewer and the F window opened in A wait
// in A: nothing of them shows over B; back in A they are all there.
func TestSwitchViewParksTheWindowsInTheirWorktree(t *testing.T) {
	m := loadedModel(t)
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	m = m.pushLayer(&diffView{title: "f", rev: ""})
	m = m.pushLayer(&diffView{title: "g", rev: "abc1"})
	m = m.pushLayer(&fileViewer{openFile: &openFile{}})
	m.filesView = &contentPopup{}
	m.filesMode = filesModeWorktree
	m.wtFiles = &worktreeFiles{query: "needle"}
	m, ok := m.switchView(other)
	if !ok {
		t.Fatalf("refused: %s", m.statusMsg)
	}
	if m.topLayer() != nil || m.filesView != nil || m.wtFiles != nil {
		t.Fatalf("A's windows show over B: top=%T files=%v", m.topLayer(), m.filesView != nil)
	}
	m = m.pushLayer(&diffView{title: "b", rev: ""}) // B's own window on B's fresh stack
	m, ok = m.switchView(home)
	if !ok {
		t.Fatalf("refused: %s", m.statusMsg)
	}
	if n := len(m.layers.entries); n != 3 {
		t.Fatalf("%d layers back in A, want 3", n)
	}
	if m.filesView == nil || m.wtFiles == nil || m.wtFiles.query != "needle" {
		t.Fatal("A's F window did not come back with its filter")
	}
	m, _ = m.switchView(other)
	if d, _ := m.topLayer().(*diffView); d == nil || d.title != "b" {
		t.Fatalf("B's window = %T, want its own diff", m.topLayer())
	}
}

// A compare with a working-tree side waits in its worktree too.
func TestSwitchViewParksACompareWithAWorkingSide(t *testing.T) { /* as the closed test, asserting absent over B and back over A */ }

// A parked tour, A's working-file bands and A's parked status retry wait
// in A: B sees none of them and B's status does not drain A's retry.
func TestSwitchViewParksTheSteerLeftovers(t *testing.T) { /* from the three round2c tests: assert absent over B, present back in A; a status landing in B leaves A's pendingSteer parked */ }
```

- [ ] **Step 2: Watch them fail** (the swap still drops).

- [ ] **Step 3: Implement**
  - `saveView`: `v.windows = m.windowState` + move `commit == ""` attention keys from `m.attention` into `v.windows.workingAttention`.
  - `loadView`: `m.windowState = v.windows` (seed `layers` when nil) + merge `v.windows.workingAttention` back into `m.attention`; the tour: `m.tour` is now in the group, so `consoleSwitch` readers need nothing.
  - `switchView`: drop the `dropWorkingTreeWindows()` call.
  - `sleepView`: drop the tour clearing and the attention pruning (keep every gen bump).
  - `ensureView`: `v.windows.layers = &layerStack{}`.
  - delete `dropWorkingLayers`, `dropWorkingTreeWindows`, `filesViewIsWorkingTree`, `workingSide`; `pendingSteer.svc` and its check in `drainPendingStatus`.
  - `pruneViews` / `abandonGoneView` / `reRoot`: a dropped slot's windows go with it (nothing to do beyond Task 4's walk stop).

- [ ] **Step 4: Green + the package**; **Step 5: Probe** (restore the `dropWorkingTreeWindows` call → the first test fails) ; **Step 6: Commit** `feat(tui): a swap parks the leaving worktree's windows in its slot and restores the arriving one's`.

### Task 3: Window generations in the group

**Files:**
- Modify: `internal/tui/model.go` (`remoteHeadNamesMsg` ~1715, `remoteHeadsMsg` ~1738, the all-notes landing ~3532 and their cmds' stamping sites; `remoteHeadsPopup` open, all-notes open)
- Modify: `internal/tui/window_state.go` (`allNotesGen`, `remoteHeadsGen`)
- Test: `internal/tui/window_state_test.go`

- [ ] **Step 1: Failing tests**

```go
// A remote-heads popup parked in A is not stuck by the slot-data gen bump:
// its read, stamped before the round trip, still lands on return.
func TestParkedRemoteHeadsPopupStillReceivesItsRead(t *testing.T) {
	m := loadedModel(t)
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	m, _ = m.openRemoteHeads("origin") // or the key path that opens it; capture the gen the cmd was stamped with
	gen := m.remoteHeadsGen
	m, _ = m.switchView(other)
	m, _ = m.switchView(home)
	nm, _ := m.Update(remoteHeadsMsg{gen: gen, remote: "origin", heads: []string{"main"}})
	m = nm.(Model)
	if p := layerOf[*remoteHeadsPopup](m); p == nil || len(p.heads) == 0 {
		t.Fatal("the parked popup dropped its read: it keyed on loadGen, which the swap bumped")
	}
}
// Same shape for the all-notes popup (TestParkedAllNotesPopupStillReceivesItsRead)
// and for the versions popup across a round trip in which B opens and closes
// its OWN versions popup (B's bump must not touch A's gen):
func TestVersionsGenIsTheSlotsOwn(t *testing.T) { /* open in A (gen gA), swap to B, open+close versions in B, back to A, deliver gA → lands */ }
```

- [ ] **Step 2: Watch them fail** (dropped by `msg.gen != m.loadGen`, or by B's bump).
- [ ] **Step 3: Implement**: stamp `m.remoteHeadsGen`/`m.allNotesGen` (pre-incremented at open) into those cmds and compare against them in the handlers; `reRoot` needs no change (slots dropped). The other window gens already moved with Task 1; the versions test pins that B's open bumps B's gen only.
- [ ] **Step 4: Green, probe (compare against `loadGen` again → fail), commit** `fix(tui): window generations are the slot's; remote heads and all notes stop keying on loadGen`.

### Task 4: `historyLive` over sleeping slots; a dropped slot stops its walks

**Files:**
- Modify: `internal/tui/history_view.go` (`historyLive` ~283, `sweepHistoryWalks` ~260)
- Modify: `internal/tui/worktree_view.go` (`pruneViews`, `abandonGoneView`), `internal/tui/model.go` (`reRoot` ~5053: before `m.views = …`)
- Test: `internal/tui/history_view_test.go` (or `window_state_test.go`)

- [ ] **Step 1: Failing tests**

```go
// A history parked in a sleeping slot keeps its walk: the sweep leaves it.
func TestParkedHistoryKeepsItsWalk(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	h := &historyView{}
	stopped := false
	h.cancel = func() { stopped = true }
	m = m.pushLayer(h)
	m.histWalks = &historyWalks{views: []*historyView{h}}
	m, _ = m.switchView(other)
	m.sweepHistoryWalks()
	if stopped {
		t.Fatal("the sweep stopped a walk whose history waits in its worktree")
	}
}

// A slot dropped with its worktree stops the walks parked in it.
func TestDroppedSlotStopsItsParkedWalks(t *testing.T) { /* same setup, then dropWorktreeFromList + the worktrees reload (pruneViews); stopped must be true */ }
```

- [ ] **Step 2: Fail; Step 3: Implement** — `historyLive` also walks `m.views[k].windows.layers.entries` and `.filesReturnLayers` for every `k != m.viewed`; `stopSlotWalks(v *worktreeView)` called by `pruneViews`, `abandonGoneView` and `reRoot` stops walks whose view is in that slot's group. **Step 4: Green, probe, commit** `fix(tui): a history parked in a sleeping worktree keeps its walk; a dropped slot stops its own`.

### Task 5: Results addressed to a sleeping slot are dropped at the gate

**Files:**
- Create: `internal/tui/slot_msg.go` (`slotMsg` interface, `forSlot` helper)
- Modify: `internal/tui/model.go` (`Update` head: the gate; the window-addressed cmds stamp `for`)
- Test: `internal/tui/window_state_test.go`

**Interfaces:**
- `type slotMsg interface{ slotKey() model.CheckoutKey }` — a message addressed to the slot it was asked from.
- `func (m Model) forSlot() model.CheckoutKey { return m.viewed }` — stamped by the cmd constructors.
- The gate in `Update`, before the switch: `if sm, ok := msg.(slotMsg); ok && sm.slotKey() != "" && sm.slotKey() != m.viewed { return m, nil /* phase 3: queue when the slot exists */ }`. It drops whether or not the slot still exists: a message for a pruned slot must never fall through to a handler that would act on the worktree on screen.
- NOT gated: a message whose handler writes only through the window's own pointer and touches no Model state (`historyChunkMsg` / the walk's done message through `h *historyView`): a parked history keeps streaming into its view (Task 4 keeps its walk alive for exactly that). Verify `onHistoryChunk` has no Model side effect before leaving it unstamped; the same test applies to any other candidate.

Messages to stamp (handlers that mutate Model state beyond the window — the review's list; confirm each by grep at execution): `diffMsg` (→ `drainPendingDiff`, `loadNotesCmd`), `compareFilesMsg`, `commitFilesMsg`, `linkCompareLoadedMsg`, `previewOpenMsg`, `versionsMsg`/the versions popup's loads, `remoteHeadNamesMsg`/`remoteHeadsMsg`, the all-notes landing, `gitConfigRowsMsg`, `entryCompareMsg`, `historyChunkMsg`/`historyDoneMsg`, the blame load, the file viewer's load, `stashFilesMsg`, `shelfFilesMsg`, the stage/unstage-hunk results, `amendPrefillMsg`, `conflictFileMsg`, `gotoLinkResolvedMsg`. A message already keyed by a slot-scoped gen or tag that is now in the group needs the stamp too: the gate is what keeps its handler's SIDE effects (focus, a view opened, `statusMsg`) off the other worktree.

- [ ] **Step 1: Failing test**

```go
// A commit diff asked from A lands while B is shown: B's screen does not
// change (no diff opens, no status message), and A's stack is untouched.
func TestResultForASleepingSlotIsDroppedAtTheGate(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m = m.pushLayer(&diffView{title: "g", rev: "abc1"})
	m.diffTag = "t1"
	m, _ = m.switchView(other)
	nm, _ := m.Update(diffMsg{tag: "t1", for: model.KeyOf(m.home), /* a loaded diff */})
	m = nm.(Model)
	if m.topLayer() != nil || m.statusMsg != "" {
		t.Fatalf("A's diff result acted on B: top=%T msg=%q", m.topLayer(), m.statusMsg)
	}
}
```

(`for` is a keyword: name the field `slot`.)

- [ ] **Step 2: Fail (the handler runs: B shows a diff or a status message); Step 3: Implement the gate and stamp the listed messages; Step 4: Green, probe (remove the gate), commit** `feat(tui): window-addressed results carry their slot; one for a sleeping slot is dropped at the gate`.

### Task 6: E2E goldens, docs, race gate

- [ ] Run `go test ./e2e -run TestScenarios/tui_worktree_switch_fast` (name per the harness); where a screen changed because a window used to follow the swap, inspect the diff, then `-update` and read the new golden.
- [ ] `CHANGELOG.md` "Fast worktree switch (TUI)": the survival bullets become one paragraph ("each worktree keeps its own windows: a diff, a history, a compare, the F window and the stash list wait in the worktree they were opened in and are back when it returns"); reword the round-1 "an open commit diff survives". `README.md` alt+w / alt+a rows. `docs/CLAUDE-details.md` fast-switch section: `windowState`, the window gens, `slotMsg`, `historyLive` over slots, what phases 3–6 add.
- [ ] `./test.sh race > <scratchpad>/race-p2.log 2>&1; grep -n "all green\|FAIL" …`; `go build -o bin/gg ./cmd/gg`.
- [ ] Memory: `fast-switch-per-worktree-stacks.md` (phase 2 done, hash, what phases 3–6 still owe).
- [ ] ASK before `gg merge -F <msg> --into feat/fast-worktree-switch feat/fast-worktree-switch-2` from the parent worktree; then ff -2, rebuild the parent's bin/gg.
