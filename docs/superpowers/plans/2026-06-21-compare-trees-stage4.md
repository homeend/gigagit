# Compare Trees — Stage 4 Implementation Plan (Multi-select set + range squash)

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans. Steps use `- [ ]`.

**Goal:** Toggle commits into a `◉` selection set, then `.`-menu **Compare selection** — 2 commits = the difference between them (`git diff A B`), 3+ = the combined diff of the range (`git diff oldest^ newest`). Plus shift+↑/↓ to grow a contiguous run.

**Architecture:** Reuse Stage 1 `openCompareFiles` and Stage 2 `orderByFeed`. The squash falls out of the existing commit↔commit `DiffTreeFiles` by passing `oldest^` as the left endpoint's hash. A new `◉` marker path (distinct from the `m`-mark `◆`) renders set membership.

**Tech Stack:** Go 1.26, Bubble Tea.

## Global Constraints

- Reuse `openCompareFiles(left, right model.Endpoint)` (Stage 1) and `orderByFeed` (Stage 2). No new git/domain/engine code.
- **`◉` = "in the compare set"** (matches the branch-scope set convention, view.go:649); **never reuse `◆`** (the transient `m`-mark, view.go:452) — they must stay visually distinct, and both are compare entry points.
- **3+ squash is a range approximation.** `git diff oldest^ newest` equals "the combined changes of the selection" only when the feed-span is a topological chain; the default feed is multi-branch date-order, so label it honestly ("combined diff of the **range**", not "of the selected commits"). The 2-commit case is exact (a tree-to-tree diff is GitKraken's semantic; no ancestry needed).
- **Root squash base: refuse with a status notice** ("can't squash from the root commit") — do NOT hardcode the empty-tree SHA (wrong on SHA-256 repos). Detect via `len(oldest.Parents) == 0`.
- **Avoid the vacuous-test trap (3rd time):** `newRepo`/`loadedModel` make ONE commit. Build `loadedModelLinearCommits(t, n)` and drive the squash diff **end-to-end** on a real linear repo (file list + `len(view.full) > 0`).
- TDD, real git. Verify test exit explicitly (no `| tail`). Branch `compare-multiselect`; human merges.

---

### Task 1: Selection set state + `◉` marker + toggle/clear rows

**Files:**
- Modify: `internal/tui/model.go` (Model field `commitCompareSet map[string]bool`)
- Modify: `internal/tui/view.go` (render block ~448–455: `◉` prefix before `◆`)
- Modify: `internal/tui/commit_scope.go` (add `compareSetDisplayIndices`, `commitCompareToggleRow`, `commitCompareClearRow`)
- Modify: `internal/tui/action_menu.go` (register the two rows)
- Test: `internal/tui/compare_select_test.go`

**Interfaces:**
- Produces: `m.commitCompareSet`; `compareSetDisplayIndices(p panel) map[int]bool`; `commitCompareToggleRow`, `commitCompareClearRow` `(actionRow, bool)`.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/compare_select_test.go`:

```go
package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

// loadedModelLinearCommits builds a real repo with n linear commits (commit k
// adds fileK.txt) and returns a loaded model. m.commits is newest-first, so
// m.commits[0] is the tip and m.commits[n-1] is the root.
func loadedModelLinearCommits(t *testing.T, n int) Model {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	for k := 0; k < n; k++ {
		os.WriteFile(filepath.Join(dir, "file"+strconv.Itoa(k)+".txt"), []byte("v\n"), 0o644)
		run("add", ".")
		run("commit", "-m", "c"+strconv.Itoa(k))
	}
	repo := &git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}
	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	mm := loaded.(Model)
	if len(mm.commits) < n {
		t.Fatalf("expected ≥%d commits, got %d", n, len(mm.commits))
	}
	return mm
}

func TestCompareSetToggleAndClear(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.sel[panelCommits] = 0

	// No set → toggle row labeled "Add", clear row absent.
	r, ok := m.commitCompareToggleRow()
	if !ok {
		t.Fatal("toggle row must be present on a commit")
	}
	if _, ok := m.commitCompareClearRow(); ok {
		t.Fatal("clear row must be absent with an empty set")
	}
	m = r.run(m).(Model) // add commit[0]
	if !m.commitCompareSet[m.commits[0].Hash] {
		t.Fatal("commit[0] must be in the set after add")
	}
	// Display indices include the member.
	if !m.compareSetDisplayIndices(panelCommits)[0] {
		t.Fatal("display index 0 must be marked")
	}
	// Clear row now present; clearing empties the set.
	cr, ok := m.commitCompareClearRow()
	if !ok {
		t.Fatal("clear row must appear with a non-empty set")
	}
	m = cr.run(m).(Model)
	if len(m.commitCompareSet) != 0 {
		t.Fatalf("set must be empty after clear, got %d", len(m.commitCompareSet))
	}
}
```

(`run` on an `actionRow` returns `(tea.Model, tea.Cmd)`; the helper rows here return `nil` cmd, so `r.run(m).(Model)` works after discarding the cmd — write `mm, _ := r.run(m); m = mm.(Model)` if a lint flags the single-value form.)

- [ ] **Step 2: Run, verify it fails**

Run: `cd /mnt/t/others/gg-compare-multi && go test ./internal/tui/ -run TestCompareSetToggleAndClear -v`
Expected: FAIL — `commitCompareToggleRow`/`commitCompareClearRow`/`compareSetDisplayIndices`/`commitCompareSet` undefined.

- [ ] **Step 3a: Model field**

In `internal/tui/model.go`, near `fileMarks` (the other multi-select set), add:

```go
	commitCompareSet map[string]bool // commits toggled into the ◉ compare selection (keyed by hash)
```

- [ ] **Step 3b: Display-index helper + rows**

In `internal/tui/commit_scope.go` add:

```go
// compareSetDisplayIndices returns the display-row indices in panel p that are
// in the commit compare selection (◉). Empty unless p is the Commits panel and
// the set is non-empty.
func (m Model) compareSetDisplayIndices(p panel) map[int]bool {
	out := map[int]bool{}
	if p != panelCommits || len(m.commitCompareSet) == 0 {
		return out
	}
	l := m.listFor(p)
	_, idx := m.panelView(p)
	for n, i := range idx {
		if m.commitCompareSet[l.Key(i)] {
			out[n] = true
		}
	}
	return out
}

// commitCompareToggleRow adds/removes the selected commit to the ◉ compare
// selection (the multi-commit analog of marking).
func (m Model) commitCompareToggleRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	hash := m.commits[bi].Hash
	in := m.commitCompareSet[hash]
	label := "Add to compare selection"
	if in {
		label = "Remove from compare selection"
	}
	return actionRow{
		id:    "commit-compare-toggle",
		label: label,
		run: func(m Model) (tea.Model, tea.Cmd) {
			if m.commitCompareSet == nil {
				m.commitCompareSet = map[string]bool{}
			}
			if in {
				delete(m.commitCompareSet, hash)
			} else {
				m.commitCompareSet[hash] = true
			}
			return m, nil
		},
	}, true
}

// commitCompareClearRow clears the ◉ compare selection.
func (m Model) commitCompareClearRow() (actionRow, bool) {
	if m.focus != panelCommits || len(m.commitCompareSet) == 0 {
		return actionRow{}, false
	}
	return actionRow{
		id:    "commit-compare-clear",
		label: "Clear compare selection",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitCompareSet = nil
			return m, nil
		},
	}, true
}
```

- [ ] **Step 3c: Render `◉` (distinct from `◆`)**

In `internal/tui/view.go`, the panel render loop (~448), add a set check that takes precedence over the mark:

```go
		marked := m.markedDisplayIndices(p)
		cmpSet := m.compareSetDisplayIndices(p)
		sel := m.sel[p]
		isFocused := m.panelFocused(p)
		wr := make([]winRow, len(rows))
		for i, row := range rows {
			prefix := "  "
			var st lipgloss.Style
			if cmpSet[i] {
				prefix = "◉ "
			} else if marked[i] {
				prefix = "◆ "
			} else if i == sel && isFocused {
				prefix = "> "
			}
			if i == sel && isFocused {
				st = selectedRow
			}
```

(The cursor stays visible on a `◉` row via the `selectedRow` style set just below — independent of the prefix.)

- [ ] **Step 3d: Register the rows**

In `internal/tui/action_menu.go`, after the `commitCompareMarkedRow` registration:

```go
	if r, ok := m.commitCompareToggleRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.commitCompareClearRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 4: Run, verify it passes**

Run: `cd /mnt/t/others/gg-compare-multi && go test ./internal/tui/ -run TestCompareSetToggleAndClear -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gg-compare-multi
git add internal/tui/model.go internal/tui/view.go internal/tui/commit_scope.go internal/tui/action_menu.go internal/tui/compare_select_test.go
git commit -m "feat(tui): commit compare selection set (◉) — toggle/clear + marker

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 2: Compare selection (2 = diff, 3+ = range squash)

**Files:**
- Modify: `internal/tui/commit_scope.go` (`compareSelectionEndpoints` + `commitCompareSelectionRow`)
- Modify: `internal/tui/action_menu.go` (register the row)
- Test: `internal/tui/compare_select_test.go` (extend)

**Interfaces:**
- Consumes: `m.commitCompareSet`, `m.commits`, `orderByFeed` (Stage 2), `openCompareFiles`.
- Produces: `func (m Model) compareSelectionEndpoints() (left, right model.Endpoint, note string, ok bool)`; `commitCompareSelectionRow`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/compare_select_test.go`:

```go
func TestCompareSelectionTwoCommits(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.commitCompareSet = map[string]bool{
		m.commits[0].Hash: true, // newer
		m.commits[1].Hash: true, // older
	}
	left, right, note, ok := m.compareSelectionEndpoints()
	if !ok {
		t.Fatalf("two commits must be comparable: %q", note)
	}
	// 2 commits → tree-diff older↔newer, no ^.
	if left.Hash != m.commits[1].Hash || right.Hash != m.commits[0].Hash {
		t.Fatalf("endpoints = %s ↔ %s, want %s ↔ %s", left.Hash, right.Hash, m.commits[1].Hash, m.commits[0].Hash)
	}
}

func TestCompareSelectionThreeCommitsSquash(t *testing.T) {
	m := loadedModelLinearCommits(t, 4) // c0(root)..c3(tip); select the top 3 (oldest selected = c1, has a parent)
	m.focus = panelCommits
	m.commitCompareSet = map[string]bool{
		m.commits[0].Hash: true,
		m.commits[1].Hash: true,
		m.commits[2].Hash: true, // oldest selected; its parent is c0
	}
	left, right, note, ok := m.compareSelectionEndpoints()
	if !ok {
		t.Fatalf("three non-root commits must squash: %q", note)
	}
	if left.Hash != m.commits[2].Hash+"^" {
		t.Fatalf("squash base = %q, want %q", left.Hash, m.commits[2].Hash+"^")
	}
	if right.Hash != m.commits[0].Hash {
		t.Fatalf("squash tip = %q, want %q", right.Hash, m.commits[0].Hash)
	}
}

func TestCompareSelectionRootSquashRefused(t *testing.T) {
	m := loadedModelLinearCommits(t, 3) // select all 3 → oldest selected is the root
	m.focus = panelCommits
	m.commitCompareSet = map[string]bool{
		m.commits[0].Hash: true,
		m.commits[1].Hash: true,
		m.commits[2].Hash: true, // root
	}
	_, _, note, ok := m.compareSelectionEndpoints()
	if ok {
		t.Fatal("squashing from the root commit must be refused")
	}
	if note == "" {
		t.Fatal("refusal must carry a status note")
	}
}

func TestCompareSelectionRowRunsRealSquashDiff(t *testing.T) {
	m := loadedModelLinearCommits(t, 4)
	m.focus = panelCommits
	m.commitCompareSet = map[string]bool{
		m.commits[0].Hash: true,
		m.commits[1].Hash: true,
		m.commits[2].Hash: true,
	}
	r, ok := m.commitCompareSelectionRow()
	if !ok {
		t.Fatal("Compare selection row must be present with 3 in the set")
	}
	u, cmd := r.run(m)
	mm := u.(Model)
	if !mm.filesCompare {
		t.Fatal("running must open compare mode")
	}
	// Drive the file list: the squash (c1^..c3 = c1,c2,c3) added file1/2/3.
	cm, ok := cmd().(compareFilesMsg)
	if !ok || cm.err != nil {
		t.Fatalf("compareFilesMsg=%v err=%v", ok, cm.err)
	}
	u, _ = mm.Update(cm)
	mm = u.(Model)
	var pick contentLine
	for _, l := range mm.filesView.lines {
		if l.path == "file2.txt" && l.status == "A" {
			pick = l
		}
	}
	if pick.path == "" {
		t.Fatalf("file2.txt (A) missing from squash list: %+v", mm.filesView.lines)
	}
	// Drive that file's diff: real rows through the cached commit↔commit key.
	sel := -1
	for i, l := range mm.filesView.lines {
		if l.path == "file2.txt" {
			sel = i
		}
	}
	mm.filesView.sel = sel
	mm.filesTreeFocused = true
	u, dcmd := mm.Update(keyMsg("enter"))
	mm = u.(Model)
	dmsg := dcmd().(diffMsg)
	if dmsg.view.err != nil || len(dmsg.view.full) == 0 {
		t.Fatalf("squash file diff: err=%v rows=%d", dmsg.view.err, len(dmsg.view.full))
	}
}

func TestCompareSelectionRowAbsentUnderTwo(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.commitCompareSet = map[string]bool{m.commits[0].Hash: true}
	if _, ok := m.commitCompareSelectionRow(); ok {
		t.Fatal("Compare selection row must be absent with fewer than 2 selected")
	}
}
```

- [ ] **Step 2: Run, verify it fails**

Run: `cd /mnt/t/others/gg-compare-multi && go test ./internal/tui/ -run TestCompareSelection -v`
Expected: FAIL — `compareSelectionEndpoints`/`commitCompareSelectionRow` undefined.

- [ ] **Step 3: Implement**

In `internal/tui/commit_scope.go`:

```go
// compareSelectionEndpoints resolves the ◉ selection into a (left, right)
// endpoint pair, ordered older→newer by feed position:
//   - exactly 2  → a tree-to-tree diff of the two commits (GitKraken's exact
//     2-commit semantic; no ancestry needed).
//   - 3 or more  → the combined diff of the RANGE: git diff oldest^ newest.
//     This is a range approximation — exact only on a topological chain — and
//     is refused when the oldest selected commit is a root (no parent).
// ok is false (with a note) when fewer than 2 are selected or the root guard
// trips.
func (m Model) compareSelectionEndpoints() (left, right model.Endpoint, note string, ok bool) {
	// Collect selected commits with their feed indices (newest-first feed).
	type sc struct {
		hash string
		idx  int
	}
	var sel []sc
	for i := range m.commits {
		if m.commitCompareSet[m.commits[i].Hash] {
			sel = append(sel, sc{m.commits[i].Hash, i})
		}
	}
	if len(sel) < 2 {
		return left, right, "select at least 2 commits to compare", false
	}
	// Oldest = largest feed index; newest = smallest.
	oldest, newest := sel[0], sel[0]
	for _, s := range sel {
		if s.idx > oldest.idx {
			oldest = s
		}
		if s.idx < newest.idx {
			newest = s
		}
	}
	if len(sel) == 2 {
		return model.Endpoint{Kind: model.EndpointCommit, Hash: oldest.hash},
			model.Endpoint{Kind: model.EndpointCommit, Hash: newest.hash}, "", true
	}
	// 3+: squash from oldest^. Refuse if oldest is a root commit.
	if len(m.commits[oldest.idx].Parents) == 0 {
		return left, right, "can't squash a range from the root commit", false
	}
	return model.Endpoint{Kind: model.EndpointCommit, Hash: oldest.hash + "^"},
		model.Endpoint{Kind: model.EndpointCommit, Hash: newest.hash}, "", true
}

// commitCompareSelectionRow offers "Compare selection" when 2+ commits are in
// the ◉ set. The label is honest about the 3+ range semantic.
func (m Model) commitCompareSelectionRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	n := len(m.commitCompareSet)
	if n < 2 {
		return actionRow{}, false
	}
	label := "Compare the 2 selected commits"
	if n >= 3 {
		label = "Compare range of " + strconv.Itoa(n) + " commits (combined diff)"
	}
	return actionRow{
		id:    "commit-compare-selection",
		label: label,
		run: func(m Model) (tea.Model, tea.Cmd) {
			left, right, note, ok := m.compareSelectionEndpoints()
			if !ok {
				m.statusMsg = note
				return m, nil
			}
			return m.openCompareFiles(left, right)
		},
	}, true
}
```

(`commit_scope.go` already imports `strconv`? If not, add it. It imports `slices`, `model`, etc.; check and add `strconv` to the import block.)

- [ ] **Step 4: Register the row**

In `internal/tui/action_menu.go`, after `commitCompareClearRow`'s registration:

```go
	if r, ok := m.commitCompareSelectionRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 5: Run, verify it passes**

Run: `cd /mnt/t/others/gg-compare-multi && go test ./internal/tui/ -run TestCompareSelection -v`
Expected: PASS (all five).

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gg-compare-multi
git add internal/tui/commit_scope.go internal/tui/action_menu.go internal/tui/compare_select_test.go
git commit -m "feat(tui): . menu — Compare selection (2 = diff, 3+ = range squash)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 3: shift+↑/↓ grows the selection (isolated; droppable)

**Files:**
- Modify: `internal/tui/model.go` (Commits-panel normal-key switch: add `shift+up`/`shift+down`)
- Test: `internal/tui/compare_select_test.go` (extend)

**Interfaces:**
- Consumes: `m.commitCompareSet`, `m.backingIndex(panelCommits)`, the existing selection-move.

- [ ] **Step 1: Write the failing test**

Append:

```go
func TestShiftDownGrowsCompareSelection(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.sel[panelCommits] = 0

	u, _ := m.Update(keyMsg("shift+down"))
	mm := u.(Model)
	// Both the start row and the landed row are in the set.
	if !mm.commitCompareSet[m.commits[0].Hash] || !mm.commitCompareSet[m.commits[1].Hash] {
		t.Fatalf("shift+down must add the start and the landed commit: %v", mm.commitCompareSet)
	}
	if mm.sel[panelCommits] != 1 {
		t.Fatalf("cursor must move to row 1, got %d", mm.sel[panelCommits])
	}
}
```

- [ ] **Step 2: Run, verify it fails**

Run: `cd /mnt/t/others/gg-compare-multi && go test ./internal/tui/ -run TestShiftDownGrowsCompareSelection -v`
Expected: FAIL — shift+down is unhandled (no set change).

- [ ] **Step 3: Implement**

In `internal/tui/model.go`, find the Commits-panel normal-key handling (the `switch msg.String()` that has `case "m":` ~line 869). Add:

```go
		case "shift+down", "shift+up":
			if m.focus != panelCommits || !m.opsIdle() {
				break
			}
			if bi, ok := m.backingIndex(panelCommits); ok {
				if m.commitCompareSet == nil {
					m.commitCompareSet = map[string]bool{}
				}
				m.commitCompareSet[m.commits[bi].Hash] = true // add the current row
			}
			delta := 1
			if msg.String() == "shift+up" {
				delta = -1
			}
			m = m.moveSelection(panelCommits, delta)
			if bi, ok := m.backingIndex(panelCommits); ok {
				m.commitCompareSet[m.commits[bi].Hash] = true // add the landed row
			}
			return m, nil
```

> Adapt the move call to the actual selection-mover used by `j`/`k` on the Commits panel (grep the `case "j":`/`case "down":` arm — it may be `m.moveSel(...)`, `m.move(panelCommits, …)`, or set `m.sel[...]` with a clamp). Use that same function so wrap/clamp behavior matches. The test asserts the cursor lands on row 1 and both rows are in the set.

- [ ] **Step 4: Run, verify it passes**

Run: `cd /mnt/t/others/gg-compare-multi && go test ./internal/tui/ -run TestShiftDownGrowsCompareSelection -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gg-compare-multi
git add internal/tui/model.go internal/tui/compare_select_test.go
git commit -m "feat(tui): shift+up/down grows the commit compare selection

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 4: Help + CHANGELOG + gate

**Files:**
- Modify: `internal/tui/help.go`, `CHANGELOG.md`

- [ ] **Step 1: Help**

In `internal/tui/help.go`, extend the Commits `.`-menu summary line to include `Add to compare selection / Compare selection`, and add:

```go
		r("", "Compare selection (.-menu): toggle commits into the ◉ set (Add to compare selection / shift+↑↓), then Compare selection — 2 = the diff between them, 3+ = the combined diff of the range (oldest^..newest, refused from a root commit); Clear compare selection empties it"),
```

- [ ] **Step 2: CHANGELOG**

Under `## [Unreleased]` → `### Added`, after the Stage 2 line:

```markdown
- **Compare a selection of commits.** Toggle commits into a `◉` set (Commits
  `.` menu *Add to compare selection*, or `shift+↑/↓`) and pick *Compare
  selection*: two commits show the diff between them, three or more show the
  combined diff of the range (`oldest^..newest`). Stage 4 of commit comparison.
```

- [ ] **Step 3: Format + vet + full race gate**

```bash
cd /mnt/t/others/gg-compare-multi
gofmt -l internal/ cmd/
go vet ./...
./test.sh race
```
Expected: `gofmt` silent, `vet` exit 0, `./test.sh race` → `all green` exit 0 (read the status directly).

- [ ] **Step 4: Commit**

```bash
cd /mnt/t/others/gg-compare-multi
git add internal/tui/help.go CHANGELOG.md
git commit -m "docs: compare a selection of commits (multi-select + range squash)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

## Self-Review

- Spec entry point D (multi-select set + shift-range; 2=diff, 3+=squash) → Tasks 1–3. ✅
- `◉` distinct from `◆` (advisor #2) → Task 1 Step 3c. ✅
- 3+ squash is a range approximation, labeled honestly + linear end-to-end test (advisor #1) → Task 2 (`TestCompareSelectionThreeCommitsSquash` + `…RunsRealSquashDiff` on a linear repo) + help/changelog wording. ✅
- Root squash refused with a note, no empty-tree constant (advisor #3) → `compareSelectionEndpoints` + `TestCompareSelectionRootSquashRefused`. ✅
- Vacuous-test trap dodged with `loadedModelLinearCommits` driving a real squash diff (advisor #4) → Task 2. ✅
- shift-range isolated as Task 3, droppable. ✅
- Names consistent: `commitCompareSet`, `compareSetDisplayIndices`, `commitCompareToggleRow`, `commitCompareClearRow`, `compareSelectionEndpoints`, `commitCompareSelectionRow`, `openCompareFiles`, `orderByFeed`. ✅
