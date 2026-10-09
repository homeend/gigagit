# Per-worktree window stacks — phase 5: parkable popups

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (this repo's CLAUDE.md forbids implementer subagents; the one session that wrote the plan executes it). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The user's own swap (`alt+w`, `alt+a`/`alt+t`, `enter` on a worktree row) may happen over a popup that can wait: the popup is parked with its worktree's windows (half-typed text included) and submits in that worktree when it returns. An agent-triggered swap (a console show asked by an agent, a steer ask, a console's return, a gone slot) still refuses under any popup, as today.

**Architecture:** One function, `parkableLayer(l layer) bool` (`avail.go`), is the whitelist: a popup whose pending work is none or a plain read the slot queue can hold. `switchView(path)` keeps its agent semantics; a new `userSwitchView(path)` allows a parkable popup on top. `cycleReachable` (what alt+w / alt+a / alt+t accept) gains the same clause. The refusal text for a non-parkable popup is unchanged.

**Spec:** `docs/superpowers/specs/2026-10-09-per-worktree-window-stacks.md` ("Swapping over a popup").

## Whitelist (parkable)

`versionsPopup`, `remoteHeadsPopup`, `allNotesPopup`, `gitConfigPopup`, `branchPopup`, `commitPopup`, `notePopup`, `annotateTagPopup`, `renameBranchPopup`, `rewordPopup`, `commitNamePopup`, `filePathPopup`, `bookmarkPopup`, `bookmarkPastePopup`, `notesListPopup`, `exportPatchPopup`, `applyPatchPopup`, `hookEditorPopup`, `languagePickerPopup`, `repoPathPopup`, `gotoCommitPopup` (not while `pending` — a resolve in flight switches by itself), `linkHistPicker`, `previewRenamePopup`, `pairOpPopup`, `reflogCheckoutPopup`, `shelfRestorePopup`, `shellCmdPopup`, `blameRecentPopup`, `commitFilterPopup`, `checkoutAsPopup`, `hunkPicker`, `relatedPromptPopup`.

NOT parkable (the swap refuses): `compareLoadingPopup` / a review loading, `agentStartPopup`, `linkComparePopup`, `sendReviewPopup`, `prHubPopup`, `prSearchPopup`, `noticePopup`, `sessionsPopup` (incl. quit mode), `commandPalette`, `moveWorktreePopup` (it switches views itself), `irebaseEditor`, `repoPopup` (a repo switch in the making), `eagerPrompt` (the shared feed's search), the generic `contentPopup` (too many surfaces draw with it; the existing alt+a gate test pins it), anything not listed.

## Caller intent

- User: `cycleWorktrees` (alt+w), the alt+a / alt+t key handlers → `showConsole` (add `byUser bool` to `showConsole`; the key handlers and the sessions popup's enter pass true), `guardedReRoot` from a key handler (enter on a worktree row, the Worktrees-panel `.` rows) — pass `byUser` through `guardedReRoot(path, offerRepair, byUser)`.
- Agent (refuses under any popup, queues through `pendingReturnView` where it does today): `returnView` (a console's close), `abandonGoneView` / `pruneViews`, steer asks (`steer_switch_ask.go`), agent tours (`agent_tours_open.go`), a console opened for a task / after a repo switch (`console_scope.go`), the hosted web page's switch (`webhost.go`: arrives async while the user may be typing), `gg://` link checkouts resolved off-thread (`goto_link.go`).

## Global Constraints

- Same worktree and gates as the earlier phases. `steerRefusal` is unchanged.
- No new user-visible strings expected; if one is, all four bundles.

## Review Focus

1. A popup parked with a half-typed field: the field's textfield state is a pointer or value inside the popup struct — nothing on the Model (`filterTyping`, `highlightTyping`, `recallOpen` are process-wide typing flags and must be false for the swap anyway).
2. A parked `commitPopup`'s submit on return runs `startOp` through `m.svc` = its worktree's: pinned by `TestParkedCommitPopupSubmitsInItsWorktree` (type a message in A, alt+w to B, alt+w back, enter → the commit lands in A, B's HEAD unchanged).
3. `gotoCommitPopup` with a resolve pending: NOT parkable (its result swaps the view by itself; parking it would replay that swap on return).
4. `cycleReachable` must keep refusing a diff/blame/viewer whose search is being typed.

---

### Task 1: `parkableLayer`, `userSwitchView`, `cycleReachable`

**Files:** `internal/tui/avail.go`, `internal/tui/worktree_view.go` (`switchView`/`userSwitchView`/`cycleWorktrees`), `internal/tui/console.go` (`showConsole` intent), `internal/tui/switch_guard.go` (`guardedReRoot` intent), the alt+a/alt+t/enter key handlers in `model.go`, `internal/tui/sessions_popup.go` (enter), tests `internal/tui/parkable_test.go`.

- [ ] **Step 1: Failing tests** — `TestAltWParksAParkableHalfTypedPopup` (a `branchPopup` with text typed, alt+w swaps; back in A the popup is there with its text), `TestAltWRefusesOverThePalette`, `TestConsoleShowStillQueuesOverAParkablePopup` (an agent's `showConsole(id, false, false)` under a versions popup: refused, `pendingReturnView` set), `TestParkedCommitPopupSubmitsInItsWorktree`, `TestGotoPopupWithAResolvePendingIsNotParkable`.
- [ ] **Step 2: Fail. Step 3: Implement. Step 4: Green, probe, package, commit** `feat(tui): the user's own swap parks a popup that can wait; it submits in its worktree on return`.

### Task 2: Docs, race gate, merge

- [ ] CHANGELOG (Added: "alt+w / alt+a work over a popup you are filling in: it waits in its worktree and submits there when you return; a swap asked by an agent still waits"), README (alt+w row), `docs/CLAUDE-details.md` (`parkableLayer`, the intent split), `./test.sh race` → "all green", bin/gg, memory, merge.
