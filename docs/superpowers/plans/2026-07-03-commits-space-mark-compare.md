# Commits Space-Mark & Auto-Compare Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `space` on the Commits panel marks/unmarks a commit in the existing ◉ compare selection (max 2); the second mark opens the two-commit comparison immediately; the `.` menu gains Unmark labels; re-opening the same comparison never reloads it.

**Architecture:** A new `handleCommitSpaceKey` (state machine over the existing `commitCompareSet` + `validCompareKeys` + `compareSelectionEndpoints`) branched from the global space dispatch; a same-tag guard inside `openCompareFiles`; relabel/visibility changes on the two existing `.`-menu rows; footer + help advertising.

**Tech Stack:** Go 1.26, Bubble Tea TUI (`internal/tui`), real-git test helpers (`loadedModelLinearCommits`).

**Spec:** `docs/superpowers/specs/2026-07-03-commits-space-mark-compare-design.md`

## Global Constraints

- Work in worktree `.claude/worktrees/commit-space-compare`, branch `feat/commit-space-compare`. All paths below are relative to the worktree root.
- TUI `Model` is a value receiver; map fields (like `commitCompareSet`) mutate shared state — the same pattern `handleMarkKey` already uses.
- `internal/tui` never imports `internal/git` (archtest-guarded). Nothing in this plan needs it.
- Count marks via `len(m.validCompareKeys())` in the *handler* (stale-tolerant set); footer predicates may use raw `len(m.commitCompareSet)` (O(1) per frame — no O(commits) work in render-path predicates).
- Refusal message, exact text: `2 commits already marked — space a marked one to unmark, or . → Unmark all`
- Run tests from the worktree root: `go test ./internal/tui/ -run <Name> -v` (needs a real `git` on PATH — all helpers build throwaway repos in `t.TempDir()`).
- Every commit message ends with the two project trailers (Co-Authored-By + Claude-Session) used on this branch.

---

### Task 1: The space gesture (`handleCommitSpaceKey`)

**Files:**
- Create: `internal/tui/commit_space.go`
- Create: `internal/tui/commit_space_test.go`
- Modify: `internal/tui/model.go` (the space dispatch, currently `if msg.Type == tea.KeySpace { return m.handleStageKey() }` at ~line 1029)

**Interfaces:**
- Consumes: `m.selectedKey(panelCommits) (string, bool)` (wip_rows.go), `m.validCompareKeys() []string`, `m.compareSelectionEndpoints() (left, right model.Endpoint, note string, ok bool)` (commit_scope.go), `m.openCompareFiles(left, right model.Endpoint) (Model, tea.Cmd)` (files_view.go), `m.commitCompareSet map[string]bool` (model.go).
- Produces: `func (m Model) handleCommitSpaceKey() (tea.Model, tea.Cmd)` — Task 4's footer predicates advertise the same states this implements.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/commit_space_test.go`. Helpers used: `loadedModelLinearCommits` (compare_select_test.go, newest-first commits), `keyMsg` (model_test.go — `"space"` → `tea.KeyMsg{Type: tea.KeySpace}`), `dirtyStatus`/`deriveWipRows` (WIP-row tests).

```go
package tui

import (
	"strings"
	"testing"
)

func TestSpaceMarksAndUnmarksCommit(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.sel[panelCommits] = 0

	u, _ := m.Update(keyMsg("space"))
	m = u.(Model)
	if !m.commitCompareSet[m.commits[0].Hash] {
		t.Fatalf("space must mark the cursor commit, set=%v", m.commitCompareSet)
	}
	u, _ = m.Update(keyMsg("space"))
	m = u.(Model)
	if len(m.commitCompareSet) != 0 {
		t.Fatalf("space on a marked commit must unmark it, set=%v", m.commitCompareSet)
	}
}

func TestSecondSpaceOpensCompareAndKeepsMarks(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.sel[panelCommits] = 0
	u, _ := m.Update(keyMsg("space")) // first mark: the tip
	m = u.(Model)
	m.sel[panelCommits] = 1
	u, cmd := m.Update(keyMsg("space")) // second mark → compare opens
	m = u.(Model)
	if m.filesView == nil || !m.inCompareMode() {
		t.Fatal("second space-mark must open the compare files view")
	}
	if m.filesLeft.Hash != m.commits[1].Hash || m.filesRight.Hash != m.commits[0].Hash {
		t.Fatalf("endpoints %s ↔ %s, want older %s ↔ newer %s",
			m.filesLeft.Hash, m.filesRight.Hash, m.commits[1].Hash, m.commits[0].Hash)
	}
	if !m.commitCompareSet[m.commits[0].Hash] || !m.commitCompareSet[m.commits[1].Hash] {
		t.Fatalf("marks must persist after the compare opens, set=%v", m.commitCompareSet)
	}
	if cmd == nil {
		t.Fatal("compare open must start the file-list load")
	}
}

func TestThirdSpaceRefusedAtTwoMarks(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.commitCompareSet = map[string]bool{m.commits[0].Hash: true, m.commits[1].Hash: true}
	m.sel[panelCommits] = 2
	u, _ := m.Update(keyMsg("space"))
	m = u.(Model)
	if m.commitCompareSet[m.commits[2].Hash] || len(m.commitCompareSet) != 2 {
		t.Fatalf("space must not grow a 2-mark set, set=%v", m.commitCompareSet)
	}
	if !strings.Contains(m.statusMsg, "2 commits already marked") {
		t.Fatalf("refusal must set the status hint, got %q", m.statusMsg)
	}
}

func TestStaleMarkDoesNotEatASpaceSlot(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.commitCompareSet = map[string]bool{
		m.commits[0].Hash: true,
		"deadbeefdeadbeefdeadbeefdeadbeefdeadbeef": true, // not in the feed
	}
	m.sel[panelCommits] = 1
	u, _ := m.Update(keyMsg("space"))
	m = u.(Model)
	if !m.commitCompareSet[m.commits[1].Hash] {
		t.Fatalf("1 valid + 1 stale mark must still leave a slot, set=%v", m.commitCompareSet)
	}
	if !m.inCompareMode() {
		t.Fatal("the second VALID mark must open the compare")
	}
}

func TestSpaceTogglesWipRow(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.status = dirtyStatus()
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()
	m.sel[panelCommits] = 0 // ◇ Working tree pseudo-row
	u, _ := m.Update(keyMsg("space"))
	m = u.(Model)
	if !m.commitCompareSet[wipKey(wipRow{kind: wipWorktree})] {
		t.Fatalf("space must mark the WIP row's sentinel key, set=%v", m.commitCompareSet)
	}
	m.sel[panelCommits] = m.wipCount() // the tip commit's unified row
	u, _ = m.Update(keyMsg("space"))
	m = u.(Model)
	if !m.inCompareMode() {
		t.Fatal("commit + WIP pair must open the compare")
	}
}
```

Note: if `dirtyStatus` is named differently, copy the WIP setup from `TestMarkCommitUnderDirtyTreeHitsRightRow` in `internal/tui/wip_compare_test.go` — it is the canonical dirty-tree pattern.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestSpace|TestSecondSpace|TestThirdSpace|TestStaleMark' -v`
Expected: FAIL — space currently routes to `handleStageKey`, whose `canStage()` requires a files panel, so the set never changes.

- [ ] **Step 3: Implement the handler and the dispatch branch**

Create `internal/tui/commit_space.go`:

```go
package tui

import tea "github.com/charmbracelet/bubbletea"

// handleCommitSpaceKey is the space gesture on the Commits panel: a fast path
// over the same ◉ compare selection the m key toggles (commitCompareSet).
// Space toggles the cursor row's membership, refuses to grow the set past two
// marks, and opens the comparison the moment the second mark lands. WIP
// pseudo-rows (◇ Working tree / ◇ Staged) participate exactly as with m.
func (m Model) handleCommitSpaceKey() (tea.Model, tea.Cmd) {
	key, ok := m.selectedKey(panelCommits)
	if !ok {
		return m, nil
	}
	if m.commitCompareSet[key] { // marked row: space always toggles off
		delete(m.commitCompareSet, key)
		return m, nil
	}
	// Count only valid marks: the set is stale-tolerant (keys survive scope
	// changes and history rewrites), and a ghost mark must not eat a slot.
	valid := len(m.validCompareKeys())
	if valid >= 2 {
		m.statusMsg = "2 commits already marked — space a marked one to unmark, or . → Unmark all"
		return m, nil
	}
	if m.commitCompareSet == nil {
		m.commitCompareSet = map[string]bool{}
	}
	m.commitCompareSet[key] = true
	if valid == 0 {
		return m, nil
	}
	// Second mark: open the comparison immediately, resolving endpoints the
	// same way the .-menu Compare row does. Marks persist so esc returns to
	// the Commits panel with both ◉ still set.
	left, right, note, ok := m.compareSelectionEndpoints()
	if !ok {
		m.statusMsg = note
		return m, nil
	}
	return m.openCompareFiles(left, right)
}
```

In `internal/tui/model.go`, change the space dispatch (~line 1029):

```go
	if msg.Type == tea.KeySpace {
		if m.focus == panelCommits {
			return m.handleCommitSpaceKey()
		}
		return m.handleStageKey()
	}
```

(The two earlier `tea.KeySpace` cases in the filter-typing and highlight-typing loops capture space first and stay unchanged; the `m.filesView != nil` routing above the dispatch means space never reaches this branch while a compare view is open.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestSpace|TestSecondSpace|TestThirdSpace|TestStaleMark' -v`
Expected: all 5 PASS

- [ ] **Step 5: Run the full tui package to catch regressions (space in other panels must still stage)**

Run: `go test ./internal/tui/`
Expected: ok (notably `stage_test.go` still passes)

- [ ] **Step 6: Commit**

```bash
git add internal/tui/commit_space.go internal/tui/commit_space_test.go internal/tui/model.go
git commit -m "feat(tui): space marks commits for compare, second mark opens it"
```

---

### Task 2: Same-pair no-refresh guard in `openCompareFiles`

**Files:**
- Modify: `internal/tui/files_view.go` (`openCompareFiles`, ~line 247)
- Modify: `internal/tui/model.go` (`case compareFilesMsg:` error branch, ~line 412)
- Create: `internal/tui/compare_reopen_test.go`

**Interfaces:**
- Consumes: `model.Endpoint.CacheTag()`, `m.inCompareMode()`, `m.compareTag`.
- Produces: `openCompareFiles` keeps its exact signature; new behavior only — same-tag re-open returns `(m, nil)` unchanged; `compareFilesMsg` with `err != nil` clears `m.compareTag`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/compare_reopen_test.go`:

```go
package tui

import (
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestReopenSamePairIsNoop(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	left := model.Endpoint{Kind: model.EndpointCommit, Hash: m.commits[1].Hash}
	right := model.Endpoint{Kind: model.EndpointCommit, Hash: m.commits[0].Hash}
	m, cmd := m.openCompareFiles(left, right)
	if cmd == nil {
		t.Fatal("first open must start a load")
	}
	view := m.filesView
	m2, cmd2 := m.openCompareFiles(left, right)
	if cmd2 != nil {
		t.Fatal("re-opening the same pair must not reload")
	}
	if m2.filesView != view {
		t.Fatal("re-opening the same pair must keep the existing view")
	}
}

func TestReopenDifferentPairReloads(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	left := model.Endpoint{Kind: model.EndpointCommit, Hash: m.commits[1].Hash}
	right := model.Endpoint{Kind: model.EndpointCommit, Hash: m.commits[0].Hash}
	m, _ = m.openCompareFiles(left, right)
	firstTag := m.compareTag
	other := model.Endpoint{Kind: model.EndpointCommit, Hash: m.commits[2].Hash}
	m2, cmd := m.openCompareFiles(other, right)
	if cmd == nil {
		t.Fatal("a different pair must reload")
	}
	if m2.compareTag == firstTag {
		t.Fatal("the tag must change for a different pair")
	}
}

func TestFailedCompareLoadIsRetryable(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	left := model.Endpoint{Kind: model.EndpointCommit, Hash: m.commits[1].Hash}
	right := model.Endpoint{Kind: model.EndpointCommit, Hash: m.commits[0].Hash}
	m, _ = m.openCompareFiles(left, right)
	u, _ := m.Update(compareFilesMsg{tag: m.compareTag, err: errors.New("boom")})
	m = u.(Model)
	if m.compareTag != "" {
		t.Fatalf("a failed load must clear the tag, got %q", m.compareTag)
	}
	m2, cmd := m.openCompareFiles(left, right)
	if cmd == nil || m2.compareTag == "" {
		t.Fatal("the same pair must re-open after a failure")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestReopen|TestFailedCompare' -v`
Expected: `TestReopenSamePairIsNoop` FAILS (cmd2 non-nil today); `TestFailedCompareLoadIsRetryable` FAILS (tag not cleared); `TestReopenDifferentPairReloads` passes already (it pins current behavior against regressions).

- [ ] **Step 3: Implement the guard and the error-clears-tag**

In `internal/tui/files_view.go`, rework the head of `openCompareFiles` — compute the tag first, add the guard, and reuse the local `tag` (delete the later `m.compareTag = "cmp:" + ...` line):

```go
func (m Model) openCompareFiles(left, right model.Endpoint) (Model, tea.Cmd) {
	tag := "cmp:" + left.CacheTag() + ":" + right.CacheTag()
	// Already showing (or loading) this exact comparison: keep it — re-running
	// the load would only blank and repaint identical content. Each caller
	// orders its endpoints deterministically, so the same pair from the same
	// gesture always builds the same tag.
	if m.filesView != nil && m.inCompareMode() && m.compareTag == tag {
		return m, nil
	}
	if m.filesView == nil { // fresh open: remember the source panel for esc/l to restore
		m.filesReturnFocus = m.focus
	}
	m = m.closeFilesView() // clean slate: clears any prior changed/stash/fullTree/preview state
	m.filesView = &contentPopup{lines: []contentLine{{text: "(loading…)"}}}
	m.filesTitle = left.Display() + " ↔ " + right.Display()
	m.filesMode = filesModeCompare
	m.filesLeft = left
	m.filesRight = right
	// h/b (history/blame) context: prefer a commit side; "" means working tree.
	switch {
	case right.Kind == model.EndpointCommit:
		m.filesHash = right.Hash
	case left.Kind == model.EndpointCommit:
		m.filesHash = left.Hash
	default:
		m.filesHash = ""
	}
	m.compareTag = tag
	// Focus the tree: compare mode has no live commit list, and moving the commit
	// selection would discard the comparison. The focus-switch keys are inert here.
	m.filesTreeFocused = true
	return m, m.loadCompareFilesCmd(left, right, tag)
}
```

In `internal/tui/model.go`, extend the `compareFilesMsg` error branch (~line 412):

```go
		if msg.err != nil {
			m.statusMsg = "compare: " + msg.err.Error()
			// A failed compare must be retryable: clear the tag so re-opening the
			// SAME pair isn't swallowed by the openCompareFiles same-tag guard.
			m.compareTag = ""
			if len(m.filesView.lines) == 1 && m.filesView.lines[0].text == "(loading…)" {
				m.filesView.lines = []contentLine{{text: "(load failed)"}}
			}
			return m, nil
		}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestReopen|TestFailedCompare' -v`
Expected: all 3 PASS

- [ ] **Step 5: Run the full tui package (bookmark/shelf/WIP compare flows share this path)**

Run: `go test ./internal/tui/`
Expected: ok

- [ ] **Step 6: Commit**

```bash
git add internal/tui/files_view.go internal/tui/model.go internal/tui/compare_reopen_test.go
git commit -m "feat(tui): re-opening the same comparison keeps the view (no reload)"
```

---

### Task 3: `.`-menu Unmark rows

**Files:**
- Modify: `internal/tui/commit_scope.go` (`commitCompareToggleRow` ~line 401, `commitCompareClearRow` ~line 432)
- Modify: `internal/tui/compare_select_test.go` (`TestCompareSetToggleAndClear` — its clear-row expectation changes)

**Interfaces:**
- Consumes: `m.selectedKey(panelCommits)`, `m.validCompareKeys()`, `strconv` (already imported by commit_scope.go).
- Produces: row ids `commit-compare-toggle` / `commit-compare-clear` unchanged (action_menu.go wiring untouched). New labels: `"Unmark commit"`, `"Add to compare selection (space)"`, `"Unmark all commits (N)"`, `"Unmark the marked commit"`.

- [ ] **Step 1: Write the failing test and update the outdated one**

Append to `internal/tui/compare_select_test.go`:

```go
// TestUnmarkRowsVisibility pins the three .-menu unmark states: cursor-on-mark
// → "Unmark commit"; ≥2 marks → "Unmark all commits (N)"; exactly one mark
// with the cursor elsewhere → "Unmark the marked commit".
func TestUnmarkRowsVisibility(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits

	// Cursor on a marked commit → toggle row reads "Unmark commit".
	m.commitCompareSet = map[string]bool{m.commits[0].Hash: true}
	m.sel[panelCommits] = 0
	r, ok := m.commitCompareToggleRow()
	if !ok || r.label != "Unmark commit" {
		t.Fatalf("cursor-on-mark toggle row = %q ok=%v, want \"Unmark commit\"", r.label, ok)
	}
	// ...and the clear row is hidden (the toggle row covers this state).
	if _, ok := m.commitCompareClearRow(); ok {
		t.Fatal("clear row must hide when the single mark is under the cursor")
	}

	// Cursor elsewhere with the single mark → "Unmark the marked commit".
	m.sel[panelCommits] = 1
	cr, ok := m.commitCompareClearRow()
	if !ok || cr.label != "Unmark the marked commit" {
		t.Fatalf("single off-cursor mark row = %q ok=%v", cr.label, ok)
	}

	// Two marks → "Unmark all commits (2)" regardless of the cursor.
	m.commitCompareSet[m.commits[1].Hash] = true
	cr, ok = m.commitCompareClearRow()
	if !ok || cr.label != "Unmark all commits (2)" {
		t.Fatalf("two-mark row = %q ok=%v, want \"Unmark all commits (2)\"", cr.label, ok)
	}
	mm, _ := cr.run(m)
	if n := len(mm.(Model).commitCompareSet); n != 0 {
		t.Fatalf("running unmark-all must empty the set, got %d", n)
	}

	// Unmarked cursor row advertises the space gesture.
	m.commitCompareSet = nil
	m.sel[panelCommits] = 0
	r, ok = m.commitCompareToggleRow()
	if !ok || r.label != "Add to compare selection (space)" {
		t.Fatalf("unmarked toggle row = %q ok=%v", r.label, ok)
	}
}
```

In `TestCompareSetToggleAndClear` (same file, ~line 49): the cursor sits on row 0 and marks commit[0], so under the new rules the clear row hides there. Replace the clear-row block:

```go
	// Clear row hides while the cursor sits ON the single mark ("Unmark
	// commit" covers it); move the cursor away and it appears.
	if _, ok := m.commitCompareClearRow(); ok {
		t.Fatal("clear row must hide when the cursor is on the only mark")
	}
	m.sel[panelCommits] = 1
	cr, ok := m.commitCompareClearRow()
	if !ok {
		t.Fatal("clear row must appear for an off-cursor mark")
	}
	mm, _ = cr.run(m)
	m = mm.(Model)
	if len(m.commitCompareSet) != 0 {
		t.Fatalf("set must be empty after clear, got %d", len(m.commitCompareSet))
	}
```

- [ ] **Step 2: Run tests to verify the new one fails**

Run: `go test ./internal/tui/ -run 'TestUnmarkRowsVisibility|TestCompareSetToggleAndClear' -v`
Expected: both FAIL (old labels, old visibility)

- [ ] **Step 3: Implement the row changes**

In `internal/tui/commit_scope.go`, `commitCompareToggleRow` (~line 410) — labels only:

```go
	in := m.commitCompareSet[key]
	label := "Add to compare selection (space)"
	if in {
		label = "Unmark commit"
	}
```

Replace `commitCompareClearRow` (~line 432) wholesale:

```go
// commitCompareClearRow unmarks the ◉ compare selection: "Unmark all commits
// (N)" with 2+ in the set; with exactly one mark it shows "Unmark the marked
// commit" ONLY when the cursor is elsewhere — the toggle row's "Unmark
// commit" covers cursor-on-mark, and without this row a lone off-cursor or
// stale mark would be menu-unreachable.
func (m Model) commitCompareClearRow() (actionRow, bool) {
	if m.focus != panelCommits || len(m.commitCompareSet) == 0 {
		return actionRow{}, false
	}
	label := "Unmark all commits (" + strconv.Itoa(len(m.validCompareKeys())) + ")"
	if len(m.commitCompareSet) < 2 {
		key, ok := m.selectedKey(panelCommits)
		if ok && m.commitCompareSet[key] {
			return actionRow{}, false
		}
		label = "Unmark the marked commit"
	}
	return actionRow{
		id:    "commit-compare-clear",
		label: label,
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitCompareSet = nil
			return m, nil
		},
	}, true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestUnmarkRowsVisibility|TestCompareSetToggleAndClear' -v`
Expected: both PASS

- [ ] **Step 5: Run the full tui package**

Run: `go test ./internal/tui/`
Expected: ok

- [ ] **Step 6: Commit**

```bash
git add internal/tui/commit_scope.go internal/tui/compare_select_test.go
git commit -m "feat(tui): .-menu Unmark commit / Unmark all commits rows"
```

---

### Task 4: Advertise — footer, help, README, CHANGELOG

**Files:**
- Modify: `internal/tui/footer.go` (contextBindings, after the `commit-files` entry ~line 70)
- Modify: `internal/tui/help.go` (Commits section, after the `m` row ~line 139)
- Modify: `internal/tui/commit_space_test.go` (footer-state test)
- Modify: `README.md` (the `space` key-table row, ~line 54)
- Modify: `CHANGELOG.md` (Unreleased → Added)

**Interfaces:**
- Consumes: `footerBinding` registry (footer.go), `m.footerLine()`, Task 1's gesture states.
- Produces: three mutually-exclusive `space` footer bindings with `id: ""` — the empty id keeps them OUT of the `.` action menu (Task 3's dedicated rows live there; a non-empty id would duplicate them as key-replay rows).

- [ ] **Step 1: Write the failing footer test**

Append to `internal/tui/commit_space_test.go`:

```go
// TestFooterAdvertisesSpaceStates pins the three mutually-exclusive footer
// hints; with 2 marked and the cursor unmarked, no space hint shows (space
// would refuse — the footer never advertises a dead key).
func TestFooterAdvertisesSpaceStates(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.sel[panelCommits] = 0

	if f := m.footerLine(); !strings.Contains(f, "[space] mark") {
		t.Fatalf("empty set must advertise [space] mark: %q", f)
	}
	m.commitCompareSet = map[string]bool{m.commits[1].Hash: true}
	if f := m.footerLine(); !strings.Contains(f, "[space] compare with marked") {
		t.Fatalf("one mark + unmarked cursor must advertise the compare: %q", f)
	}
	m.sel[panelCommits] = 1 // cursor onto the mark
	if f := m.footerLine(); !strings.Contains(f, "[space] unmark") {
		t.Fatalf("marked cursor must advertise [space] unmark: %q", f)
	}
	m.commitCompareSet[m.commits[2].Hash] = true
	m.sel[panelCommits] = 0 // 2 marks, cursor unmarked → space refuses → no hint
	if f := m.footerLine(); strings.Contains(f, "[space]") {
		t.Fatalf("2 marks + unmarked cursor must advertise no space key: %q", f)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run TestFooterAdvertisesSpaceStates -v`
Expected: FAIL (no space bindings for the Commits panel yet)

- [ ] **Step 3: Add the footer bindings and help row**

In `internal/tui/footer.go`, insert into `contextBindings` directly after the `commit-files` entry (the three predicates are mutually exclusive, like the m-trio above; `id: ""` keeps them out of the `.` menu, which has dedicated Unmark rows):

```go
	{"", "space", "[space] mark", func(m Model) bool {
		if m.focus != panelCommits || len(m.commitCompareSet) != 0 {
			return false
		}
		_, ok := m.selectedKey(panelCommits)
		return ok
	}, scopeRow},
	{"", "space", "[space] compare with marked", func(m Model) bool {
		if m.focus != panelCommits || len(m.commitCompareSet) != 1 {
			return false
		}
		key, ok := m.selectedKey(panelCommits)
		return ok && !m.commitCompareSet[key]
	}, scopeRow},
	{"", "space", "[space] unmark", func(m Model) bool {
		if m.focus != panelCommits {
			return false
		}
		key, ok := m.selectedKey(panelCommits)
		return ok && m.commitCompareSet[key]
	}, scopeRow},
```

In `internal/tui/help.go`, add after the `m` row of the Commits section (~line 139):

```go
		r("space", "mark / unmark the selected commit for compare — the same ◉ set as m but capped at two: the second space-mark opens the comparison immediately (with 2 already marked, space a marked one to unmark first, or . → Unmark all commits). Re-opening the comparison already on screen keeps it (no reload)"),
```

- [ ] **Step 4: Run the footer test and the drift guards**

Run: `go test ./internal/tui/ -run 'TestFooterAdvertisesSpaceStates|TestHelpFooterCoverage|TestFooterBindingIDs' -v`
Expected: PASS (the space key already appears in help via the staging row, and the new Commits row also carries it; empty ids are exempt from the id-uniqueness guard — the existing `tab` binding proves it)

- [ ] **Step 5: Update README and CHANGELOG**

`README.md` — extend the existing `space` row of the key table (~line 54) by appending before its closing `|`:

```
; on the **Commits** panel: mark/unmark the selected commit for compare (same ◉ set as `m`, max 2) — marking the second commit opens the two-commit comparison immediately
```

`CHANGELOG.md` — add under `## [Unreleased]` → `### Added`, above the `gg batch` entry:

```markdown
- **Space-mark & compare on the Commits panel.** `space` toggles the selected
  commit (or ◇ Working tree / ◇ Staged row) in the ◉ compare selection — the
  same set as `m`, capped at two marks — and the moment the second mark lands
  the two-commit comparison opens. With two already marked, space refuses with
  a hint. The `.` menu now reads "Unmark commit" / "Unmark all commits (N)" /
  "Unmark the marked commit", and re-opening a comparison already on screen
  keeps it instead of reloading.
```

- [ ] **Step 6: Full verification**

Run: `go build ./cmd/gg && ./test.sh`
Expected: build ok; vet+gofmt clean; unit tests pass; e2e pass (no CLI surface changed)

- [ ] **Step 7: Commit**

```bash
git add internal/tui/footer.go internal/tui/help.go internal/tui/commit_space_test.go README.md CHANGELOG.md
git commit -m "docs(tui): advertise the space mark/compare gesture (footer, help, README, changelog)"
```

---

### Task 5: Race suite & wrap-up

- [ ] **Step 1: Run the race suite (required before merge)**

Run: `./test.sh race`
Expected: all stages pass with `-race`

- [ ] **Step 2: Verify the branch is clean and report**

Run: `git status --short && git log --oneline main..HEAD`
Expected: clean tree; the spec/plan commits plus the four feature commits. The human merges (never merge unprompted).
