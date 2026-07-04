# Branches enter = go-to-tip, ctrl+g = solo+tip — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On the Branches panel, enter jumps the Commits cursor to the selected branch's tip (deep-searching unloaded history when needed), and ctrl+g solos the branch then jumps to its tip.

**Architecture:** Extract the existing `.`-menu "Go to tip in commits" jump loop into a shared `gotoCommitByHash` helper whose miss-path falls back to the ctrl+f eager search (`startEagerSearch`). The enter key runs the existing action row; ctrl+g runs the existing "Solo this branch" row with a new `pendingGotoTip` model field that the `commitsReloadedMsg` handler drains to finish the jump after the async scope reload (the `pendingPushTags` pattern).

**Tech Stack:** Go 1.26, Bubble Tea v1.3.10, value-receiver Elm-style `Model`.

**Spec:** `docs/superpowers/specs/2026-07-03-branch-enter-goto-tip-design.md`

## Global Constraints

- Work in the worktree `.claude/worktrees/branch-enter-goto-tip` on branch `feat/branch-enter-goto-tip`. Verify with `git branch --show-current` before ANY edit. Use worktree-RELATIVE paths with Write/Edit tools.
- `internal/tui` never imports `internal/git` (archtest-guarded); everything here stays inside `internal/tui` + docs.
- TUI `Model` is a value receiver; return the modified copy.
- All tests run from the worktree root: `go test ./internal/tui/ -run <Name> -v`.
- Before each commit: `gofmt -l internal/ && go vet ./internal/tui/` must be clean.
- Footer/help drift guard: every footer-binding key must appear in a help row (`TestHelpFooterCoverage`).
- New footer bindings use `id: ""` — a non-empty id would fold them into the `.` action menu, duplicating the existing "Go to tip in commits" / "Solo this branch" rows.

---

### Task 1: `gotoCommitByHash` helper + eager-search fallback in the goto-tip row

**Files:**
- Modify: `internal/tui/commit_scope.go:262-293` (the `commitGotoTipRow` func)
- Test: `internal/tui/commit_scope_test.go` (around the existing goto-tip tests, lines 406-462)

**Interfaces:**
- Consumes: `m.displayIndices(panelCommits)`, `m.commitAtUnified(bi)`, `commitIsHash(c, hash)`, `m.focusCommitsPanel()`, `m.startEagerSearch(query)` (all existing).
- Produces: `func (m Model) gotoCommitByHash(hash string) (Model, tea.Cmd)` — jump to the loaded commit matching `hash` and focus Commits; on miss, start an eager search for `hash`. Tasks 2 and 3 call this.

- [ ] **Step 1: Rewrite the not-loaded test and add two fallback tests**

In `internal/tui/commit_scope_test.go`, REPLACE the body of `TestCommitGotoTipNotLoadedNotifies` (line 449) — the old assertion ("tip not loaded" message with focus staying put) now only holds when the feed cannot load more (nil feed = exhausted path through `eagerAdvance`):

```go
// TestCommitGotoTipNotLoadedNotifies: with no feed to page (nil = cannot load
// more), the eager fallback reports exhaustion instead of silently stopping.
func TestCommitGotoTipNotLoadedNotifies(t *testing.T) {
	m := branchesPanelModel("feat", "main")
	m.branches[0].Hash = "t1deadbeef"
	m.commits = []model.Commit{{Hash: "b0aaaaaaaaaa", Subject: "base"}} // no feat tip loaded
	r, _ := findRow(availableActions(m), "commits-goto-tip")
	mm, _ := r.run(m)
	m = mm.(Model)
	if m.focus != panelBranches {
		t.Fatalf("focus should stay on Branches, got %v", m.focus)
	}
	if !strings.Contains(m.statusMsg, "not found in full history") {
		t.Fatalf("statusMsg = %q, want the eager 'not found in full history' report", m.statusMsg)
	}
}
```

Then ADD below it:

```go
// TestCommitGotoTipFallsBackToEagerSearch: a tip missing from the loaded page
// with a loadable feed starts the ctrl+f deep search on the tip hash.
func TestCommitGotoTipFallsBackToEagerSearch(t *testing.T) {
	m := newTestModelForReload(t) // Branches focused ("main" selected), real FakeRunner feed
	m.branches[0].Hash = "t1deadbeef"
	m.commits = []model.Commit{{Hash: "b0aaaaaaaaaa", Subject: "base"}}
	r, ok := findRow(availableActions(m), "commits-goto-tip")
	if !ok {
		t.Fatal("go-to-tip row missing on Branches panel")
	}
	mm, cmd := r.run(m)
	m = mm.(Model)
	if !m.eager.active || m.eager.query != "t1deadbeef" {
		t.Fatalf("eager = %+v, want active search for the tip hash", m.eager)
	}
	if !m.commitsLoading || cmd == nil {
		t.Fatalf("loading=%v cmd=%v, want a page load dispatched", m.commitsLoading, cmd != nil)
	}
}

// TestCommitGotoTipFindsFilteredTip: a /-filter hiding an already-loaded tip no
// longer dead-ends — the eager fallback clears the filter and lands on the tip.
func TestCommitGotoTipFindsFilteredTip(t *testing.T) {
	m := branchesPanelModel("feat", "main")
	m.branches[0].Hash = "t1deadbeef"
	m.commits = []model.Commit{
		{Hash: "b0aaaaaaaaaa", Subject: "base"},
		{Hash: "t1deadbeefcafe", Subject: "tip"},
	}
	m.filterPanel = panelCommits
	m.filterQuery = "zzz" // hides every row from displayIndices
	r, _ := findRow(availableActions(m), "commits-goto-tip")
	mm, _ := r.run(m)
	m = mm.(Model)
	if m.filterQuery != "" {
		t.Fatalf("filterQuery = %q, want cleared (go-to semantics)", m.filterQuery)
	}
	if m.focus != panelCommits || m.sel[panelCommits] != 1 {
		t.Fatalf("focus=%v sel=%d, want panelCommits/1", m.focus, m.sel[panelCommits])
	}
}
```

Add `"strings"` to the test file's imports if not already there.

- [ ] **Step 2: Run the three tests, expect the two new/changed ones to FAIL**

Run: `go test ./internal/tui/ -run 'TestCommitGotoTip' -v`
Expected: `TestCommitGotoTipNotLoadedNotifies` FAILS (statusMsg is the old "tip not in the loaded commits"), `TestCommitGotoTipFallsBackToEagerSearch` FAILS (eager not active), `TestCommitGotoTipFindsFilteredTip` FAILS (focus stays Branches). `TestCommitGotoTipJumpsAndFocuses` and `TestCommitGotoTipSlashBranchByHash` still PASS.

- [ ] **Step 3: Extract the helper and switch the fallback**

In `internal/tui/commit_scope.go`, replace `commitGotoTipRow`'s `run` body and add the helper directly below the func. The row (lines 262-293) becomes:

```go
// commitGotoTipRow offers "Go to tip in commits" on the Branches panel: move the
// Commits cursor to the selected branch's tip commit (matched by tip HASH, so it
// works regardless of how %D decorated the row) and focus the Commits panel.
// A tip that isn't loaded falls back to the ctrl+f eager deep-search.
// Mirrors commitSoloRow's gating.
func (m Model) commitGotoTipRow() (actionRow, bool) {
	if m.focus != panelBranches {
		return actionRow{}, false
	}
	b, ok := m.selectedBranch()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "commits-goto-tip",
		label: "Go to tip in commits",
		run: func(m Model) (tea.Model, tea.Cmd) {
			nm, cmd := m.gotoCommitByHash(b.Hash)
			return nm, cmd
		},
	}, true
}

// gotoCommitByHash moves the Commits cursor to the loaded row matching hash
// (display-index space; hash compare, not decoration parsing) and focuses the
// Commits panel. A miss falls back to the ctrl+f eager deep-search — it clears
// any /-filter ("go to" semantics), pages history under the search budget, and
// prompts before scanning deeper. Shared by the goto-tip row (enter / .-menu)
// and the ctrl+g pendingGotoTip drain in the commitsReloadedMsg handler.
func (m Model) gotoCommitByHash(hash string) (Model, tea.Cmd) {
	idx := m.displayIndices(panelCommits)
	for di, bi := range idx {
		if c, ok := m.commitAtUnified(bi); ok && commitIsHash(c, hash) {
			m.sel[panelCommits] = di
			m = m.focusCommitsPanel()
			return m, nil
		}
	}
	return m.startEagerSearch(hash)
}
```

(The eager haystack `commitHaystackAt` begins with the full `c.Hash`, so a short tip hash matches by `strings.Contains` — same prefix semantics as `commitIsHash`.)

- [ ] **Step 4: Run the goto-tip and eager suites, expect PASS**

Run: `go test ./internal/tui/ -run 'TestCommitGotoTip|TestEager|TestCtrlF' -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/ && go vet ./internal/tui/
git add internal/tui/commit_scope.go internal/tui/commit_scope_test.go
git commit -m "feat(tui): goto-tip falls back to eager deep-search; extract gotoCommitByHash"
```

---

### Task 2: enter on the Branches panel runs the goto-tip row

**Files:**
- Modify: `internal/tui/model.go:1290-1297` (the `case "enter":` block in the main key switch)
- Test: `internal/tui/commit_scope_test.go`

**Interfaces:**
- Consumes: `m.commitGotoTipRow()` (existing row; Task 1 refined its run body).
- Produces: enter-on-Branches key behavior; no new symbols.

- [ ] **Step 1: Write the failing key tests**

Append to `internal/tui/commit_scope_test.go` (needs `tea "github.com/charmbracelet/bubbletea"` in imports):

```go
// TestBranchesEnterJumpsToTip: enter on the Branches panel = the .-menu
// "Go to tip in commits" (same code path, so they cannot drift).
func TestBranchesEnterJumpsToTip(t *testing.T) {
	m := branchesPanelModel("feat", "main")
	m.branches[0].Hash = "t1deadbeef"
	m.commits = []model.Commit{
		{Hash: "b0aaaaaaaaaa", Subject: "base"},
		{Hash: "t1deadbeefcafe", Subject: "tip"},
	}
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if m.focus != panelCommits || m.sel[panelCommits] != 1 {
		t.Fatalf("enter: focus=%v sel=%d, want panelCommits/1", m.focus, m.sel[panelCommits])
	}
}

// TestBranchesEnterNoBranchNoOp: enter with nothing selectable must not panic
// or fall through to another panel's enter behavior.
func TestBranchesEnterNoBranchNoOp(t *testing.T) {
	m := branchesPanelModel() // empty Branches list
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if m.focus != panelBranches || cmd != nil {
		t.Fatalf("enter on empty Branches: focus=%v cmd=%v, want no-op", m.focus, cmd != nil)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./internal/tui/ -run 'TestBranchesEnter' -v`
Expected: `TestBranchesEnterJumpsToTip` FAILS (focus stays panelBranches — enter is unbound there today). `TestBranchesEnterNoBranchNoOp` may already pass; that's fine.

- [ ] **Step 3: Add the dispatch**

In `internal/tui/model.go`, at the TOP of the `case "enter":` block (line 1290, before the `panelTags` check), insert:

```go
		case "enter":
			// Branches: enter = the .-menu "Go to tip in commits" row (shared
			// code path, so the key and the menu can never drift apart).
			if m.focus == panelBranches {
				if r, ok := m.commitGotoTipRow(); ok {
					return r.run(m)
				}
				return m, nil
			}
			if m.focus == panelTags {
```

- [ ] **Step 4: Run, expect PASS**

Run: `go test ./internal/tui/ -run 'TestBranchesEnter|TestCommitGotoTip|TestEnterNoOp' -v`
Expected: all PASS (including the existing worktree-enter no-op test).

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/ && go vet ./internal/tui/
git add internal/tui/model.go internal/tui/commit_scope_test.go
git commit -m "feat(tui): enter on Branches jumps to the branch tip in Commits"
```

---

### Task 3: ctrl+g = solo + goto tip via pendingGotoTip

**Files:**
- Modify: `internal/tui/model.go` — Model struct (field near `pendingPushTags`, line ~51), the key switch (new `case "ctrl+g":` right after the `case "enter":` block), the `commitsReloadedMsg` handler (lines 445-459), and `reRoot` (the clear block at line ~2724).
- Test: `internal/tui/commit_scope_test.go`

**Interfaces:**
- Consumes: `m.commitSoloRow()` (existing; gates `opsIdle`), `m.selectedBranch()`, `m.gotoCommitByHash(hash)` from Task 1.
- Produces: `Model.pendingGotoTip string` — tip hash to jump to once the solo-triggered scope reload lands; drained (captured-and-cleared) by the `commitsReloadedMsg` handler; cleared by `reRoot`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/commit_scope_test.go`:

```go
// TestCtrlGSoloSetsPendingAndReloads: ctrl+g on Branches solos the branch and
// remembers its tip for the post-reload jump.
func TestCtrlGSoloSetsPendingAndReloads(t *testing.T) {
	m := newTestModelForReload(t) // Branches focused, "main" selected
	m.branches[0].Hash = "t1deadbeef"
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = nm.(Model)
	if len(m.commitScopeBranches) != 1 || m.commitScopeBranches[0] != "main" {
		t.Fatalf("scope = %v, want [main]", m.commitScopeBranches)
	}
	if m.pendingGotoTip != "t1deadbeef" {
		t.Fatalf("pendingGotoTip = %q, want the tip hash", m.pendingGotoTip)
	}
	if !m.commitsLoading || cmd == nil {
		t.Fatalf("loading=%v cmd=%v, want a scope reload dispatched", m.commitsLoading, cmd != nil)
	}
}

// TestReloadedMsgDrainsPendingGotoTip: the scope reload landing finishes the
// ctrl+g gesture — cursor on the tip, Commits focused, pending cleared.
func TestReloadedMsgDrainsPendingGotoTip(t *testing.T) {
	m := newTestModelForReload(t)
	m.branches[0].Hash = "t1deadbeef"
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = nm.(Model)
	msg := commitsReloadedMsg{gen: m.feed.Gen(), state: domain.FeedState{Commits: []model.Commit{
		{Hash: "t1deadbeefcafe", Subject: "tip"},
		{Hash: "b0aaaaaaaaaa", Subject: "base"},
	}}}
	nm, _ = m.Update(msg)
	m = nm.(Model)
	if m.pendingGotoTip != "" {
		t.Fatalf("pendingGotoTip = %q, want drained", m.pendingGotoTip)
	}
	if m.focus != panelCommits || m.sel[panelCommits] != 0 {
		t.Fatalf("focus=%v sel=%d, want panelCommits/0 (the tip row)", m.focus, m.sel[panelCommits])
	}
}

// TestCtrlGOnSoloedBranchUnsolos: ctrl+g preserves solo's toggle — a second
// press un-solos, and the pending jump still chains.
func TestCtrlGOnSoloedBranchUnsolos(t *testing.T) {
	m := newTestModelForReload(t)
	m.branches[0].Hash = "t1deadbeef"
	m.commitScopeBranches = []string{"main"} // already soloed to the selected branch
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = nm.(Model)
	if len(m.commitScopeBranches) != 0 {
		t.Fatalf("scope = %v, want cleared (un-solo)", m.commitScopeBranches)
	}
	if m.pendingGotoTip != "t1deadbeef" {
		t.Fatalf("pendingGotoTip = %q, want the tip hash even on un-solo", m.pendingGotoTip)
	}
}

// TestCtrlGBusyNoOp: ctrl+g inherits solo's opsIdle gate — nothing mutates
// while an operation runs.
func TestCtrlGBusyNoOp(t *testing.T) {
	m := newTestModelForReload(t)
	m.branches[0].Hash = "t1deadbeef"
	m.running = true // opsIdle() == false
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = nm.(Model)
	if len(m.commitScopeBranches) != 0 || m.pendingGotoTip != "" || cmd != nil {
		t.Fatalf("busy ctrl+g mutated state: scope=%v pending=%q cmd=%v", m.commitScopeBranches, m.pendingGotoTip, cmd != nil)
	}
}
```

`domain` is already imported by this test file (used in `newTestModelForReload`).

- [ ] **Step 2: Run, expect compile FAIL**

Run: `go test ./internal/tui/ -run 'TestCtrlG|TestReloadedMsgDrains' -v`
Expected: compile error — `m.pendingGotoTip` undefined.

- [ ] **Step 3: Implement**

(a) `internal/tui/model.go` Model struct, directly under the `pendingPushTags` field (line ~51):

```go
	pendingGotoTip        string              // branch tip to jump to once the ctrl+g solo reload lands (drained by commitsReloadedMsg)
```

(b) New case right after the `case "enter":` block in the same key switch:

```go
		case "ctrl+g":
			// Solo the selected branch AND land on its tip: run the .-menu
			// "Solo this branch" row (its toggle semantics included), remembering
			// the tip so the commitsReloadedMsg handler can finish the jump once
			// the scope reload lands.
			if m.focus == panelBranches {
				if b, ok := m.selectedBranch(); ok {
					if r, rowOK := m.commitSoloRow(); rowOK { // gates opsIdle
						m.pendingGotoTip = b.Hash
						return r.run(m)
					}
				}
			}
```

(c) `commitsReloadedMsg` handler (model.go:445-459) — replace the final `return m, nil` with the drain (AFTER the sel clamp and the eager-clear):

```go
		if m.eager.active {
			m.eager = eagerSearch{}
		}
		if tip := m.pendingGotoTip; tip != "" {
			m.pendingGotoTip = ""
			nm, cmd := m.gotoCommitByHash(tip)
			return nm, cmd
		}
		return m, nil
```

(d) `reRoot` — next to `m.pendingPushTags = nil` (line ~2724):

```go
	m.pendingGotoTip = "" // a repo switch must not fire a stale tip jump
```

- [ ] **Step 4: Run, expect PASS**

Run: `go test ./internal/tui/ -run 'TestCtrlG|TestReloadedMsgDrains|TestEagerClearedOnExternalReload' -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/ && go vet ./internal/tui/
git add internal/tui/model.go internal/tui/commit_scope_test.go
git commit -m "feat(tui): ctrl+g on Branches solos the branch and jumps to its tip"
```

---

### Task 4: advertise — footer bindings + help rows

**Files:**
- Modify: `internal/tui/footer.go` (contextBindings, after the `delete-branch` row at line 39)
- Modify: `internal/tui/help.go` (Branches-panel rows, lines 60-61; new enter/ctrl+g rows)
- Test: `internal/tui/footer_test.go`

**Interfaces:**
- Consumes: `m.selectedBranch()`, `m.opsIdle()`.
- Produces: footer labels `[enter] tip`, `[ctrl+g] solo+tip` on the Branches panel. Both bindings use `id: ""` so the `.` menu (which folds id-carrying contextBindings in) does not grow duplicates of its existing "Go to tip in commits" / "Solo this branch" rows.

- [ ] **Step 1: Write the failing footer test**

Append to `internal/tui/footer_test.go`:

```go
// bindingByLabel finds a context binding by its (unique) rendered label.
func bindingByLabel(t *testing.T, label string) footerBinding {
	t.Helper()
	for _, b := range contextBindings {
		if b.label == label {
			return b
		}
	}
	t.Fatalf("binding %q not found in contextBindings", label)
	return footerBinding{}
}

// TestBranchesFooterAdvertisesTipKeys: the Branches footer shows the enter and
// ctrl+g tip-jump keys when a branch is selected. Busy gating is asserted on
// the predicates directly — while an op runs the footer swaps to the heartbeat
// line wholesale, so the rendered line can't distinguish per-binding gating.
func TestBranchesFooterAdvertisesTipKeys(t *testing.T) {
	m := footerModel()
	m.loading = false
	m.focus = panelBranches
	line := m.footerLine()
	if !strings.Contains(line, "[enter] tip") || !strings.Contains(line, "[ctrl+g] solo+tip") {
		t.Fatalf("Branches footer missing tip keys: %q", line)
	}
	m.running = true // opsIdle() == false
	if bindingByLabel(t, "[ctrl+g] solo+tip").when(m) {
		t.Fatal("ctrl+g predicate must be false while busy (it mutates the feed scope)")
	}
	if !bindingByLabel(t, "[enter] tip").when(m) {
		t.Fatal("enter tip predicate should ignore busy (pure navigation)")
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./internal/tui/ -run 'TestBranchesFooterAdvertisesTipKeys' -v`
Expected: FAIL — labels absent.

- [ ] **Step 3: Add the bindings and help rows**

(a) `internal/tui/footer.go`, in `contextBindings` directly after the `delete-branch` row (line 39):

```go
	{"", "enter", "[enter] tip", func(m Model) bool {
		_, ok := m.selectedBranch()
		return m.focus == panelBranches && ok
	}, scopeRow},
	{"", "ctrl+g", "[ctrl+g] solo+tip", func(m Model) bool {
		_, ok := m.selectedBranch()
		return m.focus == panelBranches && m.opsIdle() && ok
	}, scopeRow},
```

(b) `internal/tui/help.go`, in the Branches-panel section (lines 58-61): add two rows after the `p` row and update the two existing lines:

```go
		r("enter", "go to the selected branch's tip in the Commits panel — jumps if the tip is loaded, otherwise deep-searches history for it (the ctrl+f machinery: clears the / filter, pages with a budget, asks before scanning deeper)"),
		r("ctrl+g", "Solo the selected branch AND go to its tip: scopes the Commits feed to the branch (press again to un-solo) and lands the cursor on the tip once the reload finishes"),
		r(".", "rename branch / copy branch name / Pull <branch> (stay here) / Solo this branch / Add to commit view / Go to tip in commits (.-menu)"),
		r("", "Solo this branch (.-menu): scope the Commits panel to this branch (re-run to un-solo); Add/Remove from commit view builds a multi-branch set; Show all branches clears it; Go to tip in commits jumps the Commits cursor to this branch's tip (enter does the same; both deep-search unloaded history when needed)"),
```

- [ ] **Step 4: Run footer + drift-guard tests, expect PASS**

Run: `go test ./internal/tui/ -run 'TestBranchesFooterAdvertisesTipKeys|TestHelpFooterCoverage|TestFooter' -v`
Expected: all PASS. (`TestHelpFooterCoverage` needs the "enter" and "ctrl+g" keys present in help rows — step 3b provides both.)

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/ && go vet ./internal/tui/
git add internal/tui/footer.go internal/tui/help.go internal/tui/footer_test.go
git commit -m "feat(tui): advertise Branches enter=tip and ctrl+g=solo+tip in footer and help"
```

---

### Task 5: docs + full verification

**Files:**
- Modify: `CHANGELOG.md` (Unreleased → Added, top)
- Modify: `README.md` (the `enter` key row, line ~52; the `.`-menu row, line ~85)

**Interfaces:** none (docs only), but the full test suite gates the task.

- [ ] **Step 1: CHANGELOG entry**

Add at the top of `## [Unreleased]` → `### Added` in `CHANGELOG.md`:

```markdown
- **Branches panel: `enter` jumps to the branch tip, `ctrl+g` solos + jumps.**
  `enter` on a selected branch runs "Go to tip in commits": the Commits cursor
  lands on the branch's tip and the panel focuses. A tip that isn't in the
  loaded page no longer dead-ends — it falls back to the `ctrl+f` deep search
  (clears the `/` filter, pages history under the search budget, asks before
  scanning deeper, reports "not found in full history" on exhaustion); the
  `.`-menu row gained the same fallback. `ctrl+g` runs "Solo this branch"
  first (toggle semantics preserved — a second press un-solos) and finishes
  the tip jump once the scope reload lands. Both keys advertised in the
  footer (`[enter] tip`, `[ctrl+g] solo+tip`) and `?` help.
```

- [ ] **Step 2: README key rows**

In `README.md`:
(a) The `enter` row (line ~52) starts with `| \`enter\` | on the Worktrees panel: …`. Prepend the Branches case so it begins:

```markdown
| `enter` | on the Branches panel: jump to the selected branch's **tip commit** in the Commits panel (deep-searching unloaded history if needed — the same machinery as `ctrl+f`); on the Worktrees panel: switch into the selected worktree; …
```

(b) Add a `ctrl+g` row directly below the `enter` row:

```markdown
| `ctrl+g` | on the Branches panel: **Solo this branch + go to its tip** — scopes the Commits feed to the branch (same toggle as the `.`-menu Solo: press again to un-solo) and lands the cursor on the tip once the reload finishes |
```

- [ ] **Step 3: Full suite**

Run: `./test.sh`
Expected: vet+gofmt clean, unit tests pass, e2e pass (no CLI surface changed, so e2e is unaffected).

- [ ] **Step 4: Race pass (pre-merge gate)**

Run: `./test.sh race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md README.md
git commit -m "docs: changelog + README for Branches enter/ctrl+g tip jump"
```
