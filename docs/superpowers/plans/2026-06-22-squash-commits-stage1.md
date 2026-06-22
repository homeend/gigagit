# Squash Selected Commits — Stage 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On the Commits panel, marking commits builds one selection set, and the `.` context menu offers Compare (existing) and Squash (new) — where Squash combines adjacent selected commits into one, concatenating their messages.

**Architecture:** A new pure `rebaseplan.BuildSquash` turns an oldest-first commit range + a set of target SHAs into a `Pick`/`Squash` plan. The TUI loads the range off-thread (`domain.CommitRange`, like move/drop), builds the plan, and dispatches the existing generic `engine.InteractiveRebase`. The `m` key on the Commits panel is repurposed to toggle the existing `commitCompareSet` (the `◉` selection), replacing the old single-mark-then-auto-diff gesture; "Compare with marked" is removed from the Commits panel.

**Tech Stack:** Go 1.26, Bubble Tea TUI, existing `rebaseplan` + `engine.InteractiveRebase` + `domain.CommitRange`.

## Global Constraints

- Module `github.com/gigagit/gg`; a git verb is one invocation; frontends reach git only through `internal/domain` (never `internal/git`).
- `rebaseplan` is pure: no git, no os/exec, no TUI imports.
- TUI `Model` is a value receiver; mutate the copy and return it.
- Tests use a real `git` in a `t.TempDir()` (`newRepo`/`loadedModel` helpers) or `FakeRunner`; follow TDD.
- Squash order and adjacency are derived from the loaded `onto..HEAD` range (`model.RangeCommit` list), **never** from the Commits feed / `compareKeyRank`. The feed is multi-branch and date/plain-ordered.
- Stage 1 only: adjacent selections squash; non-adjacent selections are refused with a note (reorder is Stage 2).

---

### Task 1: `rebaseplan.BuildSquash` (pure builder)

**Files:**
- Create: `internal/rebaseplan/squash.go`
- Test: `internal/rebaseplan/squash_test.go`

**Interfaces:**
- Consumes: `model.RangeCommit` (`{Hash, Subject, Message string}`); `rebaseplan.{Plan, Entry, Pick, Squash}`.
- Produces: `func BuildSquash(commits []model.RangeCommit, targets []string) (Plan, error)`.
  - `commits` is the `onto..HEAD` range, **oldest-first** (git todo order).
  - `targets` is the set of commit SHAs to squash together (order irrelevant).
  - On success: the oldest target (smallest range index) is `Pick`; every other target is `Squash`; all non-target commits keep `Pick`. Entries are in `commits` order with `Orig` carried from `RangeCommit.Message`.
  - Errors: fewer than 2 targets; a target not found in `commits`; targets not adjacent (`max-min+1 != len(targets)`).

- [ ] **Step 1: Write the failing tests**

```go
package rebaseplan

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func rc(hash, msg string) model.RangeCommit {
	return model.RangeCommit{Hash: hash, Subject: msg, Message: msg}
}

func actions(p Plan) map[string]Action {
	out := map[string]Action{}
	for _, e := range p.Entries {
		out[e.Sha] = e.Action
	}
	return out
}

func TestBuildSquashAdjacentPair(t *testing.T) {
	commits := []model.RangeCommit{rc("a", "A"), rc("b", "B"), rc("c", "C")}
	p, err := BuildSquash(commits, []string{"a", "b"})
	if err != nil {
		t.Fatalf("BuildSquash: %v", err)
	}
	if len(p.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(p.Entries))
	}
	got := actions(p)
	if got["a"] != Pick || got["b"] != Squash || got["c"] != Pick {
		t.Fatalf("actions = %v, want a:pick b:squash c:pick", got)
	}
	// Orig carried for message composition.
	if p.Entries[0].Orig != "A" {
		t.Fatalf("Orig[0] = %q, want A", p.Entries[0].Orig)
	}
}

func TestBuildSquashAdjacentTriple(t *testing.T) {
	commits := []model.RangeCommit{rc("a", "A"), rc("b", "B"), rc("c", "C"), rc("d", "D")}
	p, err := BuildSquash(commits, []string{"b", "c", "d"})
	if err != nil {
		t.Fatalf("BuildSquash: %v", err)
	}
	got := actions(p)
	if got["a"] != Pick || got["b"] != Pick || got["c"] != Squash || got["d"] != Squash {
		t.Fatalf("actions = %v, want a:pick b:pick c:squash d:squash", got)
	}
}

func TestBuildSquashNonAdjacent(t *testing.T) {
	commits := []model.RangeCommit{rc("a", "A"), rc("b", "B"), rc("c", "C")}
	_, err := BuildSquash(commits, []string{"a", "c"})
	if err == nil {
		t.Fatal("want error for non-adjacent targets")
	}
}

func TestBuildSquashMissingTarget(t *testing.T) {
	commits := []model.RangeCommit{rc("a", "A"), rc("b", "B")}
	_, err := BuildSquash(commits, []string{"a", "z"})
	if err == nil {
		t.Fatal("want error for target not in range")
	}
}

func TestBuildSquashTooFew(t *testing.T) {
	commits := []model.RangeCommit{rc("a", "A"), rc("b", "B")}
	_, err := BuildSquash(commits, []string{"a"})
	if err == nil {
		t.Fatal("want error for fewer than 2 targets")
	}
}

func TestBuildSquashMessageConcatenates(t *testing.T) {
	commits := []model.RangeCommit{rc("a", "A subject"), rc("b", "B subject"), rc("c", "C subject")}
	p, err := BuildSquash(commits, []string{"a", "b"})
	if err != nil {
		t.Fatalf("BuildSquash: %v", err)
	}
	// Group target is index 0 (a); its melded message includes b's.
	msg := p.Message(0)
	if msg != "A subject\n\nB subject" {
		t.Fatalf("Message = %q, want concatenation of A and B", msg)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd internal/rebaseplan && go test ./... -run TestBuildSquash`
Expected: FAIL — `undefined: BuildSquash`.

- [ ] **Step 3: Implement `BuildSquash`**

```go
package rebaseplan

import (
	"fmt"

	"github.com/gigagit/gg/internal/model"
)

// BuildSquash builds the rebase plan that squashes the target commits into one,
// over an oldest-first commit range (git todo order). The oldest target (the
// smallest range index) stays Pick; every other target becomes Squash; all
// other commits keep Pick, with Orig carried from the range message. The targets
// must be adjacent in the range (no unselected commit between the oldest and
// newest target) — Stage 1 refuses gaps; reordering is a later stage.
//
// Errors when fewer than 2 targets are given, a target is not in the range, or
// the targets are not adjacent.
func BuildSquash(commits []model.RangeCommit, targets []string) (Plan, error) {
	if len(targets) < 2 {
		return Plan{}, fmt.Errorf("select at least 2 commits to squash")
	}
	pos := make(map[string]int, len(commits))
	for i, c := range commits {
		pos[c.Hash] = i
	}
	min, max := len(commits), -1
	isTarget := make(map[string]bool, len(targets))
	for _, t := range targets {
		i, ok := pos[t]
		if !ok {
			return Plan{}, fmt.Errorf("commit %s is not on the current branch", short(t))
		}
		isTarget[t] = true
		if i < min {
			min = i
		}
		if i > max {
			max = i
		}
	}
	if max-min+1 != len(targets) {
		return Plan{}, fmt.Errorf("selected commits are not adjacent")
	}
	entries := make([]Entry, len(commits))
	for i, c := range commits {
		action := Pick
		if isTarget[c.Hash] && i != min {
			action = Squash
		}
		entries[i] = Entry{Sha: c.Hash, Action: action, Orig: c.Message}
	}
	return Plan{Entries: entries}, nil
}

// short trims a SHA for error messages without importing the TUI's helper.
func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd internal/rebaseplan && go test ./...`
Expected: PASS (all `TestBuildSquash*` plus the existing suite).

- [ ] **Step 5: Commit**

```bash
git add internal/rebaseplan/squash.go internal/rebaseplan/squash_test.go
git commit -m "feat(rebaseplan): BuildSquash pure plan builder for adjacent commits"
```

---

### Task 2: `m` toggles the Commits selection set; remove "Compare with marked"

**Files:**
- Modify: `internal/tui/mark.go` (the `panelCommits` branch of `handleMarkKey`)
- Modify: `internal/tui/action_menu.go` (drop the `commitCompareMarkedRow` call)
- Modify: `internal/tui/commit_scope.go` (delete `commitCompareMarkedRow`)
- Test: `internal/tui/compare_mark_test.go` (update expectation), `internal/tui/mark.go` behavior via a new test in `internal/tui/squash_test.go`

**Interfaces:**
- Consumes: `m.commitCompareSet map[string]bool`, `m.selectedKey(panelCommits)`.
- Produces: after `m` on a Commits row, that row's key is toggled in `m.commitCompareSet`; no `filesView`/compare is opened.

- [ ] **Step 1: Write the failing test**

Add `internal/tui/squash_test.go`:

```go
package tui

import "testing"

// m on the Commits panel toggles the compare selection set; a second m on a
// different commit adds it to the set rather than opening a diff.
func TestMarkOnCommitsTogglesSelectionSet(t *testing.T) {
	m := loadedModel(t, linearCommits) // multi-commit linear history helper
	m.focus = panelCommits
	m.sel[panelCommits] = 0

	// First m: select the top commit.
	mm, _ := m.handleMarkKey()
	m = mm.(Model)
	k0, _ := m.selectedKey(panelCommits)
	if !m.commitCompareSet[k0] {
		t.Fatalf("first m did not add %s to the selection set", k0)
	}
	if m.filesView != nil {
		t.Fatal("m must not open a compare/files view")
	}

	// Move down, second m: the set now has two commits, still no view.
	m.sel[panelCommits] = 1
	mm, _ = m.handleMarkKey()
	m = mm.(Model)
	k1, _ := m.selectedKey(panelCommits)
	if !m.commitCompareSet[k0] || !m.commitCompareSet[k1] {
		t.Fatalf("second m did not keep both commits selected: %v", m.commitCompareSet)
	}
	if m.filesView != nil {
		t.Fatal("second m must not open a compare view")
	}

	// m again on the same row toggles it off.
	mm, _ = m.handleMarkKey()
	m = mm.(Model)
	if m.commitCompareSet[k1] {
		t.Fatalf("re-marking %s did not toggle it off", k1)
	}
}
```

If a `linearCommits`/`loadedModel` helper with N commits does not already exist, reuse the existing real-repo model helper used by the compare tests (grep `loadedModel(` in `internal/tui/*_test.go`) and its multi-commit fixture; match that helper's name in this test.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd internal/tui && go test ./... -run TestMarkOnCommitsTogglesSelectionSet`
Expected: FAIL — second `m` opens a compare view (`m.filesView != nil`) or set not populated.

- [ ] **Step 3: Rewrite the `panelCommits` branch of `handleMarkKey`**

In `internal/tui/mark.go`, replace the Commits-specific second-mark compare block. After the file-panel block (the `if m.isFilesPanel(m.focus)` return), insert a Commits branch that toggles the set and returns, so the single-mark logic below runs only for non-Commits panels:

```go
	// Commits panel: m toggles membership in the compare selection set (◉). The
	// `.` menu then drives Compare or Squash; there is no single-mark gesture here.
	if m.focus == panelCommits {
		if m.commitCompareSet == nil {
			m.commitCompareSet = map[string]bool{}
		}
		if m.commitCompareSet[key] {
			delete(m.commitCompareSet, key)
		} else {
			m.commitCompareSet[key] = true
		}
		return m, nil
	}
```

Then delete the now-dead Commits block later in the function (the `if m.focus == panelCommits { older, newer := ... openCompareFiles ... }` second-mark block).

- [ ] **Step 4: Remove "Compare with marked" from the Commits menu**

In `internal/tui/action_menu.go`, delete the block:

```go
	if r, ok := m.commitCompareMarkedRow(); ok {
		rows = append(rows, r)
	}
```

In `internal/tui/commit_scope.go`, delete the `commitCompareMarkedRow` function (lines for `// commitCompareMarkedRow ...` through its closing brace).

- [ ] **Step 5: Update the stale compare-mark test**

In `internal/tui/compare_mark_test.go`, find the test that marks two Commits rows and asserts a compare/files view opens via `m`. Update it to assert the two rows land in `m.commitCompareSet` and no view opens (the second-`m`-opens-diff behavior is gone). If the test exclusively covered the removed gesture, replace its body with the selection-set assertion; keep its name or rename to match.

- [ ] **Step 6: Run the tests**

Run: `cd internal/tui && go test ./... -run 'TestMark|TestCompare'`
Expected: PASS. Then `go build ./...` from repo root: PASS (no remaining references to `commitCompareMarkedRow`).

- [ ] **Step 7: Commit**

```bash
git add internal/tui/mark.go internal/tui/action_menu.go internal/tui/commit_scope.go internal/tui/compare_mark_test.go internal/tui/squash_test.go
git commit -m "feat(tui): m toggles Commits selection set; drop Compare-with-marked"
```

---

### Task 3: "Squash N commits" menu row + range load + dispatch

**Files:**
- Modify: `internal/tui/commit_scope.go` (new `commitSquashRow`)
- Modify: `internal/tui/op.go` (new `squashRangeLoadedMsg` + `loadSquashRangeCmd`)
- Modify: `internal/tui/model.go` (handle `squashRangeLoadedMsg`)
- Modify: `internal/tui/action_menu.go` (append the squash row)
- Test: `internal/tui/squash_test.go`

**Interfaces:**
- Consumes: `m.commitCompareSet`, `m.status.Branch`, `m.svc.CommitRange`, `m.compareKeyRank`, `m.isWipRow`/WIP keys via `wipKey(...)`, `rebaseplan.BuildSquash`, `engine.InteractiveRebase`, `os.Executable`.
- Produces:
  - `func (m Model) commitSquashRow() (actionRow, bool)` — visible when `focus==panelCommits`, ops idle, `status.Branch != ""`, and the selection set has ≥2 entries.
  - `squashRangeLoadedMsg{branch, onto string; targets []string; commits []model.RangeCommit; err error}`.
  - `func (m Model) loadSquashRangeCmd(branch, onto string, targets []string) tea.Cmd`.

- [ ] **Step 1: Write the failing test (row availability + refusals)**

Append to `internal/tui/squash_test.go`:

```go
import (
	"strings"
	"testing"
)

func selectionSet(keys ...string) map[string]bool {
	s := map[string]bool{}
	for _, k := range keys {
		s[k] = true
	}
	return s
}

func TestSquashRowVisibleWith2Commits(t *testing.T) {
	m := loadedModel(t, linearCommits)
	m.focus = panelCommits
	if _, ok := m.commitSquashRow(); ok {
		t.Fatal("squash row should be hidden with <2 selected")
	}
	// Select the two newest commits from the feed.
	m.commitCompareSet = selectionSet(m.commits[0].Hash, m.commits[1].Hash)
	row, ok := m.commitSquashRow()
	if !ok {
		t.Fatal("squash row should be visible with 2 commits selected")
	}
	if !strings.Contains(row.label, "Squash") {
		t.Fatalf("label = %q, want it to mention Squash", row.label)
	}
}

func TestSquashRefusesWipInSelection(t *testing.T) {
	m := loadedModel(t, linearCommits)
	m.focus = panelCommits
	m.commitCompareSet = selectionSet(m.commits[0].Hash, wipKey(wipRow{kind: wipWorktree}))
	row, ok := m.commitSquashRow()
	if !ok {
		t.Fatal("row should still appear; refusal happens on run")
	}
	mm, _ := row.run(m)
	if got := mm.(Model).statusMsg; !strings.Contains(got, "commits-only") {
		t.Fatalf("statusMsg = %q, want a commits-only refusal", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd internal/tui && go test ./... -run TestSquash`
Expected: FAIL — `m.commitSquashRow undefined`.

- [ ] **Step 3: Implement `commitSquashRow`**

In `internal/tui/commit_scope.go`:

```go
// commitSquashRow offers "Squash N commits" when 2+ commits are in the ◉
// selection and a branch is checked out. The run validates the selection
// (commits-only, on the current branch, adjacent) after loading the range; the
// oldest selected commit (by feed rank) seeds the rebase base onto..HEAD.
func (m Model) commitSquashRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() || m.status.Branch == "" {
		return actionRow{}, false
	}
	if len(m.commitCompareSet) < 2 {
		return actionRow{}, false
	}
	n := len(m.commitCompareSet)
	return actionRow{
		id:    "commit-squash",
		label: "Squash " + strconv.Itoa(n) + " commits",
		run: func(m Model) (tea.Model, tea.Cmd) {
			var targets []string
			oldest, oldestRank := "", -1
			for k := range m.commitCompareSet {
				switch k {
				case wipKey(wipRow{kind: wipWorktree}), wipKey(wipRow{kind: wipStaged}):
					m.statusMsg = "squash is commits-only; remove the working tree / staged row"
					return m, nil
				}
				targets = append(targets, k)
				if r := m.compareKeyRank(k); r > oldestRank {
					oldest, oldestRank = k, r
				}
			}
			if oldest == "" {
				m.statusMsg = "select at least 2 commits to squash"
				return m, nil
			}
			// Root guard: the oldest commit needs a parent to rebase onto.
			if oldestRank >= 0 && oldestRank < len(m.commits) && len(m.commits[oldestRank].Parents) == 0 {
				m.statusMsg = "can't squash from the root commit"
				return m, nil
			}
			return m, m.loadSquashRangeCmd(m.status.Branch, oldest+"^", targets)
		},
	}, true
}
```

Ensure `strconv` is imported in `commit_scope.go` (it already is — `commitCompareSelectionRow` uses it).

- [ ] **Step 4: Implement the range load message + command**

In `internal/tui/op.go`, alongside `rebaseRangeLoadedMsg`/`loadRebaseRangeCmd`:

```go
// squashRangeLoadedMsg carries the onto..branch range for a squash, loaded off
// the UI thread; the handler builds the squash plan and runs the rebase.
type squashRangeLoadedMsg struct {
	branch, onto string
	targets      []string
	commits      []model.RangeCommit
	err          error
}

// loadSquashRangeCmd reads onto..branch off the UI thread for a squash.
func (m Model) loadSquashRangeCmd(branch, onto string, targets []string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		cs, err := svc.CommitRange(context.Background(), onto, branch)
		return squashRangeLoadedMsg{branch: branch, onto: onto, targets: targets, commits: cs, err: err}
	}
}
```

- [ ] **Step 5: Handle the message in `model.go`**

In `internal/tui/model.go`, next to the `rebaseRangeLoadedMsg` case:

```go
	case squashRangeLoadedMsg:
		if msg.err != nil {
			m.statusMsg = "squash: " + msg.err.Error()
			return m, nil
		}
		plan, perr := rebaseplan.BuildSquash(msg.commits, msg.targets)
		if perr != nil {
			// Stage 1: adjacency / membership failures refuse with a note.
			m.statusMsg = "squash: " + perr.Error()
			return m, nil
		}
		ggBin, err := os.Executable()
		if err != nil {
			m.statusMsg = "squash: " + err.Error()
			return m, nil
		}
		m.commitCompareSet = nil // the squash consumes the selection
		return m.startOp(engine.InteractiveRebase{Branch: msg.branch, Onto: msg.onto, Plan: plan, GGBin: ggBin})
```

- [ ] **Step 6: Append the squash row to the menu**

In `internal/tui/action_menu.go`, in `appendCommitContextRows`, right after the `commitCompareSelectionRow` block:

```go
	if r, ok := m.commitSquashRow(); ok {
		rows = append(rows, r)
	}
```

(Match the local accumulator variable name used in that function — `rows` or `out`.)

- [ ] **Step 7: Run the tests**

Run: `cd internal/tui && go test ./... -run TestSquash`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/commit_scope.go internal/tui/op.go internal/tui/model.go internal/tui/action_menu.go internal/tui/squash_test.go
git commit -m "feat(tui): Squash N commits menu row, range load, and dispatch"
```

---

### Task 4: Real-repo integration test (dispatched Onto + plan)

**Files:**
- Test: `internal/tui/squash_test.go`

**Interfaces:**
- Consumes: the real-repo `loadedModel` helper + a multi-commit linear fixture; `engine.InteractiveRebase`.
- Produces: a test asserting that running the squash row over a 3-commit linear selection produces a plan with the oldest target `Pick` and the rest `Squash`, and `Onto == oldest+"^"`.

- [ ] **Step 1: Write the integration test**

The squash dispatch is async (range load → plan build → `startOp`). Drive it through `Update` by feeding the `squashRangeLoadedMsg` directly with a real range, OR assert the plan by capturing the op. Prefer asserting the pure boundary: load the range via the real service, then build the plan and check it (this mirrors the move/drop range tests and avoids running a live rebase in a unit test).

```go
func TestSquashBuildsAdjacentPlanFromRange(t *testing.T) {
	m := loadedModel(t, linearCommits) // newest-first feed: m.commits[0] newest
	m.focus = panelCommits

	// Select the two newest commits (adjacent in a linear history).
	newer, older := m.commits[0].Hash, m.commits[1].Hash
	m.commitCompareSet = selectionSet(newer, older)

	// Oldest by feed rank seeds the base.
	onto := older + "^"
	cs, err := m.svc.CommitRange(t.Context(), onto, m.status.Branch)
	if err != nil {
		t.Fatalf("CommitRange: %v", err)
	}
	plan, err := rebaseplan.BuildSquash(cs, []string{newer, older})
	if err != nil {
		t.Fatalf("BuildSquash: %v", err)
	}
	// Range is oldest-first: older=Pick, newer=Squash.
	got := map[string]rebaseplan.Action{}
	for _, e := range plan.Entries {
		got[e.Sha] = e.Action
	}
	if got[older] != rebaseplan.Pick || got[newer] != rebaseplan.Squash {
		t.Fatalf("plan = %v, want older:pick newer:squash", got)
	}
}
```

If `t.Context()` is unavailable on the Go version in use, use `context.Background()`.

- [ ] **Step 2: Run the test**

Run: `cd internal/tui && go test ./... -run TestSquashBuildsAdjacentPlanFromRange`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/squash_test.go
git commit -m "test(tui): squash builds adjacent plan from the real range"
```

---

### Task 5: Help text + CHANGELOG

**Files:**
- Modify: `internal/tui/help.go`
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Update help text**

In `internal/tui/help.go`, find the `m` entry and the Commits-panel help. Change the `m` description so that on the Commits panel it reads as toggling the compare selection (not "mark"), and add a line that the `.` menu offers Compare and Squash on the selection. Keep the Branches-panel `m` (mark for merge/rebase) description intact.

- [ ] **Step 2: Add a CHANGELOG entry**

In `CHANGELOG.md`, under `## [Unreleased]` → `### Added`:

```markdown
- **Squash selected commits.** On the Commits panel, `m` now toggles a commit
  into a selection set (the `◉` markers); the `.` menu then offers **Compare**
  (unchanged) and **Squash N commits**. Squash combines the selected commits
  into one, concatenating their messages (reword afterward if you like). Stage 1
  squashes commits that are already adjacent; non-adjacent selections are refused
  with a note (reordering them first is coming next). "Compare with marked" is
  removed from the Commits panel — the selection set replaces it.
```

- [ ] **Step 3: Verify build + full unit suite**

Run (repo root): `go build ./... && ./test.sh unit`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/help.go CHANGELOG.md
git commit -m "docs: help + changelog for squash selected commits"
```

---

## Self-Review Notes

- **Spec coverage:** §Mark semantics → Task 2; §menu (Compare/Squash) → Tasks 2–3; §Squash mechanics (range-derived order, off-HEAD guard, adjacency, plan, dispatch, message) → Tasks 1 & 3; §non-adjacent refusal → Task 3 (Step 5 handler) + Task 1 error; §testing → Tasks 1–4; help/changelog → Task 5. Stage 2 (reorder) intentionally absent.
- **Range-vs-feed:** adjacency + Pick/Squash order come from `BuildSquash` over `msg.commits` (the range). The feed `compareKeyRank` is used only to pick the *oldest candidate* that seeds `Onto`; `BuildSquash` then validates membership/adjacency from the range, and the off-HEAD case surfaces as the "not on the current branch" error.
- **Type consistency:** `BuildSquash(commits []model.RangeCommit, targets []string) (Plan, error)`; `squashRangeLoadedMsg`/`loadSquashRangeCmd` mirror the `rebaseRangeLoadedMsg`/`loadRebaseRangeCmd` shapes; `engine.InteractiveRebase{Branch, Onto, Plan, GGBin}` matches the existing struct.
- **Helper names:** the test fixture helper (`loadedModel` + a multi-commit history) must match the actual names in `internal/tui/*_test.go` — confirm via grep at execution time and adjust the test calls accordingly.
