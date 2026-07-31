# Dirty-tree branch-switch prompt Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When the Branches-panel switch (`s`) targets a local branch while the working tree is dirty, ask what to do with the changes — worktree for the branch / carry changes (today's autostash) / cancel — instead of silently stashing.

**Architecture:** Pure TUI pre-empt at the existing dispatch site in `internal/tui/model.go` (the `checkoutCurrentBranchModal` / switch-to-worktree precedent): a `decisionState` modal replaces the plain confirm when dirty; clean trees keep today's flow. Engine and CLI untouched.

**Tech Stack:** Go 1.26, Bubble Tea, `internal/tui` AST i18n gates.

**Spec:** `docs/superpowers/specs/2026-07-31-switch-dirty-prompt-design.md`

## Global Constraints

- **All work happens in the feature worktree** `/mnt/t/others/gigagit.worktrees/feat-switch-dirty-prompt` (branch `feat/switch-dirty-prompt`). Every Write/Edit uses that absolute path prefix; every command is prefixed `cd /mnt/t/others/gigagit.worktrees/feat-switch-dirty-prompt && `. Verify `git branch --show-current` prints `feat/switch-dirty-prompt` before touching anything.
- Stage specific paths only (`gg add <paths>`), NEVER `add -A`.
- Every user-visible string via `i18n.T("<literal>")` with the key in ALL FOUR bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`); decision option VALUES stay English protocol and each needs an `optionDisplayName` case (`internal/tui/i18n_display.go`) — `options_vocab_test.go` enforces this.
- Dirty predicate mirrors what SmartSwitch stashes on: `m.status.Counts()` → `Staged+Unstaged+Conflicted > 0` (untracked excluded).
- The modal must appear even when slow-op confirmation is disabled (it replaces `confirmOp` on the dirty path, it does not stack with it).
- Commit messages end with:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`
  `Claude-Session: https://claude.ai/code/session_01F6vE15h5QeiTe9BuvrJTCg`

---

### Task 1: the switch-dirty modal

**Files:**
- Modify: `internal/tui/model.go` (~line 1346, the `canSwitchBranch()` branch of the `s` handler)
- Modify: `internal/tui/i18n_display.go` (two `optionDisplayName` cases)
- Modify: `internal/i18n/lang/ja.toml`, `ko.toml`, `zh.toml`, `ru.toml`
- Test: `internal/tui/model_test.go` or the file where existing switch-modal tests live (`grep -rn "switch-to-worktree" internal/tui/*_test.go` and put these beside them)

**Interfaces:**
- Consumes: `engine.SmartSwitch{Branch}`, `m.openWorktreePopup(true)` (existing-branch create-worktree popup with create-&-switch semantics, keyed off the Branches-panel selection — the same selection this handler acted on), `decisionState`/`engine.DecisionRequest` (mirror the `"switch-to-worktree"` modal at model.go:1349-1366), `abortOption` esc mapping (esc resolves to the LAST option when none is named `"abort"` — so `"cancel"` must stay last).
- Produces: modal id `"switch-dirty"` with options `["worktree", "carry changes", "cancel"]` (order fixed: worktree first).

- [ ] **Step 1: Write the failing tests.** Find the existing tests around the Branches-panel switch/`"switch-to-worktree"` modal and mirror their Model construction (focus on `panelBranches`, a selectable local branch, `m.sel` set). Add:

```go
// TestSwitchDirtyPromptAppears: dirty tree + s on a switchable branch pushes
// the switch-dirty modal instead of dispatching/confirming.
func TestSwitchDirtyPromptAppears(t *testing.T) {
	m := /* mirror the existing switch-test model builder */
	m.status.Files = []model.FileStatus{{Path: "a.txt", Unstaged: 'M'}} // Counts().Unstaged > 0
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = mm.(Model)
	if m.modal == nil || m.modal.req.ID != "switch-dirty" {
		t.Fatalf("expected switch-dirty modal, got %+v", m.modal)
	}
	want := []string{"worktree", "carry changes", "cancel"}
	if !reflect.DeepEqual(m.modal.req.Options, want) {
		t.Fatalf("options = %v, want %v", m.modal.req.Options, want)
	}
}

// TestSwitchCleanTreeSkipsDirtyPrompt: a clean tree keeps today's flow (no
// switch-dirty modal; the confirm/dispatch path is reached).
func TestSwitchCleanTreeSkipsDirtyPrompt(t *testing.T) {
	m := /* same builder, no dirty files */
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = mm.(Model)
	if m.modal != nil && m.modal.req.ID == "switch-dirty" {
		t.Fatal("clean tree must not get the switch-dirty modal")
	}
}

// TestSwitchDirtyPromptResolutions: "worktree" opens the create-worktree
// popup for the selection; "carry changes" dispatches SmartSwitch.
func TestSwitchDirtyPromptResolutions(t *testing.T) {
	// worktree lane
	m := /* dirty builder as above */
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = mm.(Model)
	mm, _ = m.modal.onResolve(m, "worktree")
	m = mm.(Model)
	if _, ok := m.topLayer().(*worktreePopup); !ok {
		t.Fatalf("worktree option must open the worktree popup, top = %T", m.topLayer())
	}
	// carry lane: assert the op dispatch (mirror however sibling tests observe
	// startOp — e.g. a non-nil returned tea.Cmd plus m.opRunning/status, or the
	// pattern the neighboring switch tests use).
}
```

(Exact field spellings for `model.FileStatus`/`Counts` and the modal accessors: read the neighboring tests first and match them; the FACTS asserted must stay as written. If the existing modal struct exposes `req` differently, adapt the accessor, not the assertion.)

- [ ] **Step 2: Run them, verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-switch-dirty-prompt && go test ./internal/tui/ -run 'TestSwitchDirty|TestSwitchClean' -v`
Expected: FAIL — no `switch-dirty` modal exists yet.

- [ ] **Step 3: Implement.** In `model.go`, inside `if m.canSwitchBranch() {`, AFTER the existing `worktreeForBranch` go-to-worktree block and BEFORE the `confirmOp(engine.SmartSwitch{...})` line:

```go
			if c := m.status.Counts(); c.Staged+c.Unstaged+c.Conflicted > 0 {
				branch := b.Name
				m.modal = &decisionState{
					req: engine.DecisionRequest{
						ID:      "switch-dirty",
						Prompt:  i18n.T("You have uncommitted changes. Switch to %s?", branch),
						Options: []string{"worktree", "carry changes", "cancel"},
					},
					onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
						switch opt {
						case "worktree":
							if mm, ok := m.openWorktreePopup(true); ok {
								return mm, nil
							}
							return m, nil
						case "carry changes":
							return m.startOp(engine.SmartSwitch{Branch: branch})
						}
						return m, nil
					},
				}
				return m, nil
			}
```

In `i18n_display.go`, add to `optionDisplayName` (alphabetical placement among the cases):

```go
	case "carry changes":
		return i18n.T("carry changes")
	case "worktree":
		return i18n.T("worktree")
```

- [ ] **Step 4: Bundle entries** — add to all four bundles (insert near similar keys):

| key | ja | ko | zh | ru |
|---|---|---|---|---|
| `"You have uncommitted changes. Switch to %s?"` | `"未コミットの変更があります。%s に切り替えますか？"` | `"커밋되지 않은 변경 사항이 있습니다. %s(으)로 전환하시겠습니까?"` | `"有未提交的更改。切换到 %s 吗？"` | `"Есть незафиксированные изменения. Переключиться на %s?"` |
| `"carry changes"` | `"変更を持ち越す"` | `"변경 사항 가져가기"` | `"携带更改"` | `"перенести изменения"` |
| `"worktree"` | `"ワークツリー"` | `"워크트리"` | `"工作树"` | `"рабочее дерево"` |

- [ ] **Step 5: Run the full TUI gate**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-switch-dirty-prompt && go test ./internal/tui/ && go build ./...`
Expected: PASS including `options_vocab_test` and the i18n scans.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-switch-dirty-prompt
gg add internal/tui/model.go internal/tui/i18n_display.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml <test file>
gg commit -m "feat(tui): ask before carrying changes on a dirty branch switch

The Branches-panel switch on a dirty tree now forks a modal — worktree
for the branch (create-worktree popup, dirty tree untouched) / carry
changes (today's autostash SmartSwitch) / cancel — instead of silently
stashing. Clean trees keep the existing confirm flow; engine/CLI
unchanged.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01F6vE15h5QeiTe9BuvrJTCg"
```

---

### Task 2: docs + gates

**Files:**
- Modify: `CHANGELOG.md`, `CLAUDE.md` (`tui` package-map row: one sentence on the switch-dirty pre-empt), `README.md` ONLY if it describes the switch behavior (check; likely no change).

**Interfaces:** none — prose.

- [ ] **Step 1: CHANGELOG entry** under the unreleased/top section, in-voice: dirty-tree branch switch now asks worktree/carry/cancel instead of silently autostashing.
- [ ] **Step 2: CLAUDE.md `tui` row** — append one sentence describing the `"switch-dirty"` pre-dispatch modal and its three options (the `checkoutCurrentBranchModal` precedent).
- [ ] **Step 3: README check** — grep README for the switch/`s` key documentation; extend only if it exists.
- [ ] **Step 4: Full gates**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-switch-dirty-prompt && ./test.sh 2>&1 | tail -5`
Expected: all green.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-switch-dirty-prompt
gg add CHANGELOG.md CLAUDE.md
gg commit -m "docs: dirty-switch prompt surface

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01F6vE15h5QeiTe9BuvrJTCg"
```

(Include README.md in the `gg add` only if Step 3 changed it.)

---

### Task 3: final gates

- [ ] **Step 1:** `./test.sh` full (already run in Task 2 — re-run only if Task 2 changed code, which it must not).
- [ ] **Step 2:** Race gate detached: `cd <worktree> && ./test.sh race > <scratchpad>/race-switch.log 2>&1 &`, poll; quiet machine caveat applies.
- [ ] **Step 3:** Stop and report — the human owns merging.
