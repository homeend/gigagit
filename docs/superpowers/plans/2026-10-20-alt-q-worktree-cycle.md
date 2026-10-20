# alt+q / alt+Q worktree walk Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (this repo forbids implementer subagents — CLAUDE.md). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** alt+q / alt+Q walk ONE worktree's screens — your windows → its agents → its terminals → the plain worktree — forward / back, wrapping, without losing the windows.

**Architecture:** A new `internal/tui/worktree_walk.go` holds the walk and the *cycle park*. The park exists only at the plain stop: the viewed worktree's whole `windowState` group moves into a `*cyclePark` hung on the (fresh) live group, so a slot swap carries it. Session stops are plain `showConsoleBy` (alt+a's screen); leaving the plain stop first unparks. Late window results for parked windows queue on the park (the sleeping-slot mechanism) and replay on unpark.

**Tech Stack:** Go 1.26, Bubble Tea, `internal/tui`, i18n TOML bundles (`internal/i18n/lang/`).

**Spec:** `docs/superpowers/specs/2026-10-20-alt-q-worktree-cycle-design.md`

## Global Constraints

- Keys: `alt+q` forward, `alt+Q` back (Bubble Tea strings).
- Ring order: `[your windows]? ++ sessionRingIn(false, W) ++ sessionRingIn(true, W) ++ [plain]`, wrapping.
- W = the viewed worktree (`m.viewPath(m.viewed)`), except: Branches has the keyboard with no window open and the cursor on a row naming another worktree → that worktree (switch to it first, `userSwitchView`); a row naming no worktree → nothing at all — unless a walk is in progress (park present), which wins.
- Refusals as alt+w: `altCycleBlocked()` → ignored; `!opsIdle()` → status "an operation is running — switch once it has finished", nothing moves.
- Every user-visible string through `i18n.T` with a literal key present in all four bundles (ja/ko/zh/ru). Run the i18n AST gates.
- TUI only. No implementer subagents. Tests use `loadedModel`, `addWorktree`, `installSessionManager`, `startSessionIn`, `startTerminalIn`, `pressAlt`.

## Review Focus

1. A history still streaming when parked must keep streaming and come back filled (sweep must see the park) — Task 1 test `TestCycleParkKeepsAHistoryWalk`.
2. A diff/blame still loading when parked must not stay "loading" forever — Task 1 test `TestCycleParkQueuesAndReplaysWindowResults`.
3. alt+a (or an agent's task console) from the plain stop must not lose the windows — Task 1 test `TestConsoleShownAtThePlainStopTakesTheWindows`.
4. alt+w from the plain stop moves on (no first-hit close) and the windows come back with W — Task 1 test `TestAltWFromThePlainStopKeepsThePark`.
5. alt+q and alt+Q land on the same screen for the same stop (full-screen console over a diff either way) — Task 2 test `TestAltQAndAltShiftQShowTheSameScreenPerStop`.

---

### Task 1: The cycle park

**Files:**
- Create: `internal/tui/worktree_walk.go` (park half)
- Modify: `internal/tui/window_state.go` (field + `holds`), `internal/tui/slot_msg.go` or `slot_replay.go` (`routeSlotMsg`), `internal/tui/console.go` (`showConsoleBy` unparks first), `internal/tui/worktree_view.go` (`loadView` unparks), `internal/tui/model.go` (Update tail: loss detection)
- Test: `internal/tui/worktree_walk_test.go`

**Interfaces:**
- Produces:
  - `type cyclePark struct { windows windowState; queued []tea.Msg; focus, lastLeftPanel, activeLeftTab, fullMax panel; fullMaxed bool }`
  - `windowState.cyclePark *cyclePark` (nil = nothing put aside)
  - `func (m Model) parkCycle() (Model, tea.Cmd)` — fails the group's parked steer (`failParkedSteer`, reason `"the view was put aside (alt+q)"`), moves `m.windowState` + focus/tabs/pin into a park, `m.windowState = old.fresh()` with `.cyclePark` set, `fullMaxed = false`; no-op (nil park) when the group holds no window (`windowsOpen()` false).
  - `func (m Model) unparkCycle() Model` — no-op without a park; else live group's layers (a popup opened at the plain stop) go ON TOP of the parked pile, `m.windowState = park.windows` (its `cyclePark` nil), focus/tabs/pin restored, `m.replay = append(m.replay, park.queued...)`, `reconcileFullscreenFocus`.
  - `func (m Model) dropCyclePark() Model` — tears the parked windows down as their esc would: `*historyView` → `stop()`, `*splitView` → `leaveChars(); retire()`, `*fileViewer` with `openFile` → `openFile.backgrounded = true`; over `layers.entries` and `filesReturnLayers`; queue dropped; park nil.
  - `func (m Model) windowsOpen() bool` — a window on the live group: `filesView != nil || stashView != nil || filesPreview != nil` or a layer of type `*diffView, *historyView, *blameView, *fileViewer, *splitView`.

- [ ] **Step 1: Write the failing tests** (`worktree_walk_test.go`, each `t.Parallel()` where the neighbours do)

```go
// park → unpark gives back the very same windows, focus and pin.
func TestCycleParkRoundTrip(t *testing.T) {
	m := loadedModel(t); m.width, m.height = 160, 40
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv); m.focus = panelFiles; m.fullMaxed, m.fullMax = true, panelFiles
	m, _ = m.parkCycle()
	// assert: m.topLayer()==nil, m.windowState.cyclePark!=nil, !m.fullMaxed
	m = m.unparkCycle()
	// assert: m.topLayer()==dv, m.focus==panelFiles, m.fullMaxed && m.fullMax==panelFiles, cyclePark==nil
}
func TestCycleParkKeepsAHistoryWalk(t *testing.T)            // h.cancel set, pushLayer(h), histWalks{h}; parkCycle; sweepHistoryWalks → not stopped
func TestCycleParkQueuesAndReplaysWindowResults(t *testing.T) // blameView loading; parkCycle; Update(blameMsg{slotStamp: m.stamp(), tag: <its tag>, lines: 1 line}) → still loading; unparkCycle + Update(any no-op msg) → blame has the line
func TestOpeningAWindowAtThePlainStopDropsThePark(t *testing.T) // park with a history h (cancel recorded); pushLayer(new diff) then Update(nil-ish msg) → cyclePark==nil, h stopped, new diff on top
func TestAPopupAtThePlainStopKeepsThePark(t *testing.T)     // park; pushLayer(&helpPopup{} or any popup type); Update → cyclePark still set
func TestConsoleShownAtThePlainStopTakesTheWindows(t *testing.T) // diff parked; startSessionIn(home); pressAlt 'a' → console shown full-screen (consoleFull), park nil; esc/closeConsole → diff on top
func TestAltWFromThePlainStopKeepsThePark(t *testing.T)     // two worktrees; diff in home; parkCycle + activateTab(Branches)+selectWorktreeBranch(home); pressAlt 'w' → viewed==other (moved, not first-hit); switchView(home) → diff live again (loadView unparked)
```

- [ ] **Step 2: Run** `go test ./internal/tui -run 'CyclePark|PlainStop' -count=1` — Expected: FAIL (undefined `parkCycle`).

- [ ] **Step 3: Implement**
  - `window_state.go`: add the field; `holds` also returns `w.cyclePark != nil && w.cyclePark.windows.holds(l)`.
  - `worktree_walk.go`: the four functions above.
  - `routeSlotMsg`: before the `k == m.viewed` early return — when `k == m.viewed && m.windowState.cyclePark != nil && m.topLayer() == nil`, append to `park.queued` via `trimQueue` (respect `staleFor` against the viewed slot's `worktreeView`, `sharedWriter` as for sleeping slots) and report taken.
  - `showConsoleBy`: `m = m.unparkCycle()` as its first line after the session lookup.
  - `loadView`: `m = m.unparkCycle()` right after `m.windowState = v.windows` (+ the nil-layers guard).
  - `reRoot` (repo switch): `m = m.dropCyclePark()` where it drops the other slots — the parked group is the old repository's. Test `TestRepoSwitchDropsThePark` (park a history, `reRoot` the way `TestDroppedSlotStopsItsParkedWalks`' neighbours do → park nil, walk stopped).
  - Update tail (`model.go`, before `sweepHistoryWalks`): `if next.windowState.cyclePark != nil && next.windowsOpen() { next = next.dropCyclePark() }`.

- [ ] **Step 4: Run** the same command — Expected: PASS. Then `go test ./internal/tui -count=1` — Expected: PASS.

- [ ] **Step 5: Commit** `tui: the alt+q cycle park — windows put aside whole, results queued, unparked on a console or a return`

---

### Task 2: The walk + keys

**Files:**
- Modify: `internal/tui/worktree_walk.go` (walk half), `internal/tui/model.go` (global dispatch next to alt+w), `internal/tui/console.go` (focused-console router next to alt+w; `consolePassthrough`, `consoleFullPassthrough` get `alt+q`, `alt+Q`)
- Test: `internal/tui/worktree_walk_test.go`

**Interfaces:**
- Consumes: Task 1's park API.
- Produces:
  - `func (m Model) walkWorktree(back bool) (Model, tea.Cmd)` — the alt+q / alt+Q handler.
  - `func (m Model) cursorWorktree() (path string, named bool)` — Branches cursor row (`branchEntries()[displayIndices(panelBranches)[sel]].br`) → `worktreePathOf(m.branches[br].Name)`; sub-rows count as their branch.
  - `func (m Model) hideConsoleHere() Model` — `ret.view = m.viewed` (when `ret != nil`), then `closeConsole()`.
  - stop kinds: `walkWindows`, `walkSession` (with id), `walkPlain`.

**Algorithm (walkWorktree):**
1. `altCycleBlocked` → return; `!opsIdle` → status, return.
2. Position: park present → `walkPlain`. Else console shown: its session in W's ring (dir == viewed) → `walkSession`; otherwise (foreign / exited) `hideConsoleHere()` then fall through. Else Branches has the keyboard and `!windowsOpen()`: `cursorWorktree()` — not named → return (no status); names another worktree → `userSwitchView` (refused → return its model); then if `windowsOpen()` → `walkWindows`, else `walkPlain` when `worktreeShownInBranches(W)`. Else → `walkWindows`.
3. Stops: windows stop present iff position is `walkWindows`, or park present, or (position `walkSession` and the console's return carries something: `consoleParked` non-empty, `filesView != nil`, or `ret.fullMaxed`).
4. Ring of one stop → status "no running agent or terminal in this worktree", return.
5. Next = position ± 1 (wrap). Leaving plain → `unparkCycle()` first.
6. Arrive: session → `showConsoleBy(id, true, true)` (or bind the shown one), `selectSessionRow`; plain → `hideConsoleHere()` if a console, `parkCycle()`, `activateTab(panelBranches).selectWorktreeBranch(W)`; windows → `hideConsoleHere()` if a console.
7. Status: session `"%s in %s — %d of %d"` (title, worktree, stop index, stops); plain `"%s — plain worktree, %d of %d"`; windows `"%s — your windows, %d of %d"`.

- [ ] **Step 1: Write the failing tests**

```go
func TestAltQWalksWindowsAgentsTerminalsPlain(t *testing.T)   // diff pushed; agent A1, terminal T1 in home; q→A1 bound (consoleFull), q→T1, q→plain (no layer, Branches focused, cursor on home row), q→diff back on top
func TestAltShiftQWalksBack(t *testing.T)                     // same setup; Q→plain, Q→T1, Q→A1, Q→diff
func TestAltQAndAltShiftQShowTheSameScreenPerStop(t *testing.T) // T1 via q twice vs via Q twice from the diff: both consoleFull(), same id
func TestAltQWithoutSessionsTogglesWindowsAndPlain(t *testing.T)
func TestAltQFromPlainPanelsHasNoWindowsStop(t *testing.T)    // Branches focused on home row, one agent: q→A1, q→plain, q→A1
func TestAltQOnABranchWithoutWorktreeDoesNothing(t *testing.T)
func TestAltQOnAnotherWorktreesRowWalksThatWorktree(t *testing.T) // cursor on wt2's row, agent in wt2: q → viewed==wt2, console = wt2's agent
func TestAltQWalkInProgressWinsOverTheCursor(t *testing.T)   // at plain with park; move cursor to a non-worktree branch; q → windows back
func TestAltQFromAForeignDockedConsoleWalksTheViewedWorktree(t *testing.T)
func TestAltQIsRefusedDuringAnOperation(t *testing.T)        // m.running = true → nothing moves, status set
func TestAltQReachesGGFromABoundConsole(t *testing.T)        // bound console of home; pressAlt 'q' → the next stop, the program never got it
```

- [ ] **Step 2: Run** `go test ./internal/tui -run 'AltQ|AltShiftQ' -count=1` — Expected: FAIL.
- [ ] **Step 3: Implement** `walkWorktree`, `cursorWorktree`, `hideConsoleHere`; wire `alt+q`/`alt+Q` in `model.go` (after alt+w: `if k == "alt+q" || k == "alt+Q"` → `m.walkWorktree(k == "alt+Q")`), in `updateConsoleKey`'s focused branch, and both passthrough maps.
- [ ] **Step 4: Run** the same, then `go test ./internal/tui -count=1` — Expected: PASS (incl. i18n gates once Task 3's bundle entries exist — add them here if the gate fails now).
- [ ] **Step 5: Commit** `tui: alt+q / alt+Q walk the viewed worktree's windows, sessions and plain panels`

---

### Task 3: Hints, help, translations, docs

**Files:**
- Modify: `internal/tui/footer.go` (binding `{"walk-worktree", "alt+q", i18n.T("[alt+q] this worktree"), <gate>, scopeGlobal}` — gate: `!m.altCycleBlocked()` and the viewed worktree has a session or a window open/parked), `internal/tui/help.go` (rows for `alt+q` and `alt+Q` next to alt+w), `internal/i18n/lang/*.toml` (every new key, four bundles — follow the `adding-translations` skill), `CHANGELOG.md`, `README.md` (key table), `docs/CLAUDE-details.md` (fast-switch section: the cycle park), spec header → IMPLEMENTED.

- [ ] **Step 1:** footer + help + bundles; run `go test ./internal/tui -run 'I18n|Vocab|MenuLabels|EngineProse|Footer|Help' -count=1` — Expected: PASS.
- [ ] **Step 2:** docs.
- [ ] **Step 3:** `./test.sh` then `./test.sh race` — Expected: "all green" in the log (not just exit 0).
- [ ] **Step 4:** `./build.sh` into `bin/gg` of the worktree for the user's manual check; commit `docs: alt+q / alt+Q`.
- [ ] **Step 5:** Final read-only whole-branch review (fresh subagent, most capable model); fix findings; ask the user before merging.
