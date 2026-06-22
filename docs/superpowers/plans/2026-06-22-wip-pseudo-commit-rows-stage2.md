# WIP pseudo-commit rows — Stage 2 (◉ compare integration) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the WIP pseudo-rows (`◇ Working tree` / `◇ Staged`) participate in the two-row compare flows — `m`-mark + *Compare with marked* and the `◉` selection + *Compare selection* — so you can diff a commit against your working copy / index by picking the two rows. Also fix a latent Stage 1 bug where marking a commit while the tree is dirty marks the wrong row.

**Architecture:** The mark and the `◉` set are keyed by a row's `panelList.Key` (a commit hash, or a WIP sentinel `\x00wip-<label>`). Stage 2 adds two pure resolvers over those keys — `compareKeyEndpoint` (key → `model.Endpoint`) and `compareKeyRank` (key → newest-first order rank, WIP rows newest) — and routes the compare flows through them. Marking/toggling is fixed to use the **unified list index** (the space `Key` lives in), which both repairs the dirty-commit-mark bug and lets a WIP row be marked/selected.

**Tech Stack:** Go 1.26, Bubble Tea (value-receiver `Model`), `model.Endpoint`, the Stage 1 `wip_rows.go` accessors.

## Global Constraints

- TUI-only: no changes to `internal/engine`, `internal/domain`, `internal/git`, `internal/cli`, or `internal/agentskill` (no `agentskill.Version` bump).
- `m.commits` stays the pure feed; WIP rows live only in the display/unified layer (Stage 1 invariant). HEAD is `m.commits[0]`.
- The mark (`markState.key`) and the `◉` set (`m.commitCompareSet`) are keyed by `panelList.Key` — a commit hash for a commit row, the WIP sentinel `\x00wip-<label>` for a pseudo-row. Single-source the sentinel via `wipKey(wipRow)`.
- A WIP row maps to `model.EndpointWorkTree` (Working tree) or `model.EndpointIndex` (Staged); a commit to `model.EndpointCommit{Hash}`.
- Two-row compare orders older→newer: WIP rows are newest (Working tree newer than Staged), commits ordered by feed position (larger feed index = older).
- A WIP row is refused in a 3+ range squash compare (a range `oldest^..newest` is meaningless for a non-commit) — with an explanatory note.
- The standalone Commits `.`-menu *Compare against working tree* / *Compare against staged* rows are KEPT (a one-step shortcut the selection flow doesn't cheaply replace); this is a deliberate deviation from the spec's "retire them" line.
- Commit messages end with the trailers:
  `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`
  `Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro`
- Run `go test ./internal/tui/` after each task; `./test.sh race` before merge.

---

### Task 1: Key resolvers + mark fix + markable/selectable WIP rows

**Files:**
- Modify: `internal/tui/wip_rows.go` — `wipKey`, `compareKeyEndpoint`, `compareKeyRank`, `compareKeyLabel`.
- Modify: `internal/tui/viewstate.go` — `commitList.Key` uses `wipKey` (single-source).
- Modify: `internal/tui/mark.go` — `handleMarkKey` keys off the unified list index.
- Modify: `internal/tui/avail.go` — `canMark` admits a WIP row on the Commits panel.
- Modify: `internal/tui/commit_scope.go` — `commitCompareToggleRow` keys off the unified selection (supports WIP).
- Test: `internal/tui/wip_compare_test.go` (new).

**Interfaces:**
- Consumes: Stage 1 `wipRow`/`wipRowAt`/`isWipRow`/`commitSelUnified`/`commitAtUnified`; `model.Endpoint`.
- Produces:
  - `func wipKey(r wipRow) string` — `"\x00wip-" + r.label()`.
  - `func (m Model) compareKeyEndpoint(key string) model.Endpoint` — WIP sentinel → WorkTree/Index, else `EndpointCommit{Hash: key}`.
  - `func (m Model) compareKeyRank(key string) int` — `wipWorktree`→-2, `wipStaged`→-1, a commit→its feed index, unknown→a large number (treated oldest).
  - `func (m Model) compareKeyLabel(key string) string` — `"working tree"` / `"staged"` / `shortHash(key)`.

- [ ] **Step 1: Write the failing test**

```go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestCompareKeyResolvers(t *testing.T) {
	m := loadedModelLinearCommits(t, 3) // commits[0]=tip
	wt := wipKey(wipRow{kind: wipWorktree})
	st := wipKey(wipRow{kind: wipStaged})

	if e := m.compareKeyEndpoint(wt); e.Kind != model.EndpointWorkTree {
		t.Fatalf("worktree key → %v", e.Kind)
	}
	if e := m.compareKeyEndpoint(st); e.Kind != model.EndpointIndex {
		t.Fatalf("staged key → %v", e.Kind)
	}
	h := m.commits[1].Hash
	if e := m.compareKeyEndpoint(h); e.Kind != model.EndpointCommit || e.Hash != h {
		t.Fatalf("commit key → %+v", e)
	}
	// rank: working tree newest (-2) < staged (-1) < tip commit (0) < older (1)
	if !(m.compareKeyRank(wt) < m.compareKeyRank(st) &&
		m.compareKeyRank(st) < m.compareKeyRank(m.commits[0].Hash) &&
		m.compareKeyRank(m.commits[0].Hash) < m.compareKeyRank(m.commits[1].Hash)) {
		t.Fatal("rank ordering wrong (want wt < staged < tip < older)")
	}
}

func TestMarkCommitUnderDirtyTreeHitsRightRow(t *testing.T) {
	m := loadedModelLinearCommits(t, 4)
	m.focus = panelCommits
	m.status = dirtyStatus()
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()

	// Select the tip commit: unified index = wipCount (display row just below the
	// WIP rows). Mark it.
	m.sel[panelCommits] = m.wipCount()
	u, _ := m.Update(keyMsg("m"))
	m = u.(Model)
	if m.mark == nil {
		t.Fatal("m must mark the commit row")
	}
	if m.mark.key != m.commits[0].Hash {
		t.Fatalf("mark key = %q, want tip hash %q (off-by-wipCount bug)", m.mark.key, m.commits[0].Hash)
	}
	// The ◆ marker must render on that same display row, not a shifted one.
	if md := m.markDisplayIndex(panelCommits); md != m.wipCount() {
		t.Fatalf("mark display row = %d, want %d", md, m.wipCount())
	}
}

func TestMarkAndToggleWipRow(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.status = dirtyStatus()
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()

	// Mark the Working tree row (unified index 0).
	m.sel[panelCommits] = 0
	if !m.canMark() {
		t.Fatal("canMark must allow a wip row")
	}
	u, _ := m.Update(keyMsg("m"))
	m = u.(Model)
	if m.mark == nil || m.mark.key != wipKey(wipRow{kind: wipWorktree}) {
		t.Fatalf("mark key = %v, want worktree sentinel", m.mark)
	}

	// Toggle the Staged row (unified index 1) into the ◉ set.
	m.sel[panelCommits] = 1
	r, ok := m.commitCompareToggleRow()
	if !ok {
		t.Fatal("compare-toggle must be available on a wip row")
	}
	uu, _ := r.run(m)
	m = uu.(Model)
	if !m.commitCompareSet[wipKey(wipRow{kind: wipStaged})] {
		t.Fatalf("staged sentinel not in compare set: %v", m.commitCompareSet)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestCompareKeyResolvers|TestMarkCommitUnderDirtyTree|TestMarkAndToggleWipRow' -v`
Expected: FAIL — `wipKey`/`compareKeyEndpoint` undefined; mark keys off-by-wipCount; toggle/canMark refuse wip.

- [ ] **Step 3: Add the resolvers (wip_rows.go) and single-source `wipKey` in commitList.Key**

In `internal/tui/wip_rows.go` add:

```go
// wipKey is the panelList.Key identity of a pseudo-row: a git-invalid sentinel
// (NUL) that can't collide with a commit hash. Single-sourced here and used by
// commitList.Key, the mark, and the ◉ set.
func wipKey(r wipRow) string { return "\x00wip-" + r.label() }

// compareKeyEndpoint maps a mark/◉ key (a commit hash or a wip sentinel) to the
// compare endpoint it denotes.
func (m Model) compareKeyEndpoint(key string) model.Endpoint {
	switch key {
	case wipKey(wipRow{kind: wipWorktree}):
		return model.Endpoint{Kind: model.EndpointWorkTree}
	case wipKey(wipRow{kind: wipStaged}):
		return model.Endpoint{Kind: model.EndpointIndex}
	default:
		return model.Endpoint{Kind: model.EndpointCommit, Hash: key}
	}
}

// compareKeyRank orders keys newest→oldest for older→newer pairing: the working
// tree is newest (-2), then staged (-1), then commits by feed position (a larger
// feed index is older). An unknown key sorts oldest.
func (m Model) compareKeyRank(key string) int {
	switch key {
	case wipKey(wipRow{kind: wipWorktree}):
		return -2
	case wipKey(wipRow{kind: wipStaged}):
		return -1
	}
	for i := range m.commits {
		if m.commits[i].Hash == key {
			return i
		}
	}
	return 1 << 30
}

// compareKeyLabel is a short human label for a key (status bar / menu text).
func (m Model) compareKeyLabel(key string) string {
	switch key {
	case wipKey(wipRow{kind: wipWorktree}):
		return "working tree"
	case wipKey(wipRow{kind: wipStaged}):
		return "staged"
	default:
		return shortHash(key)
	}
}
```

In `internal/tui/viewstate.go`, change `commitList.Key`'s wip branch to use `wipKey`:

```go
func (l commitList) Key(i int) string {
	if r, ok := l.wipAt(i); ok {
		return wipKey(r)
	}
	return l.items[i-l.wip()].Hash
}
```

- [ ] **Step 4: Fix `handleMarkKey` to key off the unified list index (mark.go)**

Replace the `bi`/`Key(bi)` lookup at the top of `handleMarkKey`:

```go
func (m Model) handleMarkKey() (tea.Model, tea.Cmd) {
	idx := m.displayIndices(m.focus)
	s := m.sel[m.focus]
	if s < 0 || s >= len(idx) {
		return m, nil
	}
	li := idx[s] // the panelList index space (unified for Commits); Key lives here
	key := m.listFor(m.focus).Key(li)
	// ... unchanged from the file-panel branch onward (uses `key`).
```

(Everything after the `key :=` line is unchanged; the file-panel branch still keys files the same way.)

- [ ] **Step 5: Let `canMark` admit a WIP row (avail.go)**

```go
func (m Model) canMark() bool {
	if !m.opsIdle() {
		return false
	}
	if m.focus == panelCommits && m.isWipRow(m.commitSelUnified()) {
		return true
	}
	_, ok := m.backingIndex(m.focus)
	return ok
}
```

- [ ] **Step 6: Let `commitCompareToggleRow` key off the unified selection (commit_scope.go)**

```go
func (m Model) commitCompareToggleRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	u := m.commitSelUnified()
	if u < 0 {
		return actionRow{}, false
	}
	key := m.listFor(panelCommits).Key(u)
	in := m.commitCompareSet[key]
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
				delete(m.commitCompareSet, key)
			} else {
				m.commitCompareSet[key] = true
			}
			return m, nil
		},
	}, true
}
```

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/tui/ -run 'TestCompareKeyResolvers|TestMarkCommitUnderDirtyTree|TestMarkAndToggleWipRow' -v`
Expected: PASS.

- [ ] **Step 8: Run the whole package**

Run: `go test ./internal/tui/`
Expected: ok (clean-tree behavior unchanged; existing mark/compare tests still pass).

- [ ] **Step 9: Commit**

```bash
gofmt -w internal/tui/*.go
git add internal/tui/ && git commit -m "feat(tui): mark/select WIP rows for compare; fix dirty-tree commit mark

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 2: Two-row compare resolves WIP↔commit

**Files:**
- Modify: `internal/tui/commit_scope.go` — `commitCompareMarkedRow` and `compareSelectionEndpoints` resolve via the key helpers; `commitCompareMarkedRow` available when the selection is a WIP row.
- Test: `internal/tui/wip_compare_test.go` (append).

**Interfaces:**
- Consumes: `compareKeyEndpoint`, `compareKeyRank`, `compareKeyLabel`, `commitSelUnified`, `listFor(panelCommits).Key`.
- Produces: `commitCompareMarkedRow` and `compareSelectionEndpoints` that accept WIP keys.

- [ ] **Step 1: Write the failing test**

```go
func TestCompareMarkedWipVsCommit(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.status = dirtyStatus()
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()

	// Mark the Working tree row, then select an old commit.
	m.mark = &markState{panel: panelCommits, key: wipKey(wipRow{kind: wipWorktree}), display: "working tree"}
	m.sel[panelCommits] = m.wipCount() + 1 // second commit from the tip

	r, ok := m.commitCompareMarkedRow()
	if !ok {
		t.Fatal("compare-with-marked must be available with a wip mark + commit selection")
	}
	u, _ := r.run(m)
	mm := u.(Model)
	if mm.filesView == nil {
		t.Fatal("compare must open the files view")
	}
	// Older (commit) → newer (working tree): left=commit, right=working tree.
	if mm.filesLeft.Kind != model.EndpointCommit || mm.filesRight.Kind != model.EndpointWorkTree {
		t.Fatalf("endpoints = %v↔%v, want Commit↔WorkTree", mm.filesLeft.Kind, mm.filesRight.Kind)
	}
}

func TestCompareSelectionWithWipTwo(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.status = dirtyStatus()
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()
	m.commitCompareSet = map[string]bool{
		wipKey(wipRow{kind: wipWorktree}): true,
		wipKey(wipRow{kind: wipStaged}):   true,
	}
	left, right, note, ok := m.compareSelectionEndpoints()
	if !ok {
		t.Fatalf("two wip rows must compare: %s", note)
	}
	// staged (older) → working tree (newer)
	if left.Kind != model.EndpointIndex || right.Kind != model.EndpointWorkTree {
		t.Fatalf("endpoints = %v↔%v, want Index↔WorkTree", left.Kind, right.Kind)
	}
}

func TestCompareSelectionRangeRefusesWip(t *testing.T) {
	m := loadedModelLinearCommits(t, 4)
	m.status = dirtyStatus()
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()
	m.commitCompareSet = map[string]bool{
		wipKey(wipRow{kind: wipWorktree}): true,
		m.commits[0].Hash:                 true,
		m.commits[1].Hash:                 true,
	}
	if _, _, _, ok := m.compareSelectionEndpoints(); ok {
		t.Fatal("a 3+ range containing a wip row must be refused")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestCompareMarkedWipVsCommit|TestCompareSelectionWithWip|TestCompareSelectionRangeRefusesWip' -v`
Expected: FAIL — compare-marked refuses wip selection / endpoints come out as commit hashes.

- [ ] **Step 3: Rewrite `commitCompareMarkedRow` to use keys**

```go
func (m Model) commitCompareMarkedRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	if m.mark == nil || m.mark.panel != panelCommits || !m.markAlive() {
		return actionRow{}, false
	}
	u := m.commitSelUnified()
	if u < 0 {
		return actionRow{}, false
	}
	selKey := m.listFor(panelCommits).Key(u)
	marked := m.mark.key
	if selKey == marked {
		return actionRow{}, false
	}
	// older→newer by rank (higher rank = older).
	a, b := marked, selKey
	if m.compareKeyRank(a) < m.compareKeyRank(b) {
		a, b = b, a // a = older
	}
	older, newer := a, b
	return actionRow{
		id:    "commit-compare-marked",
		label: "Compare with marked (" + m.compareKeyLabel(marked) + ")",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openCompareFiles(m.compareKeyEndpoint(older), m.compareKeyEndpoint(newer))
		},
	}, true
}
```

(`markAlive` already matches `m.mark.key` against `listFor(panelCommits).Key` over the display indices, so a WIP mark stays alive while its row exists. Verify in Step 5.)

- [ ] **Step 4: Rewrite `compareSelectionEndpoints` to use keys**

```go
func (m Model) compareSelectionEndpoints() (left, right model.Endpoint, note string, ok bool) {
	// Collect selected keys present in the unified list, with their rank.
	type sk struct {
		key  string
		rank int
	}
	var sel []sk
	l := m.listFor(panelCommits)
	idx := m.displayIndices(panelCommits)
	seen := map[string]bool{}
	for _, i := range idx {
		k := l.Key(i)
		if m.commitCompareSet[k] && !seen[k] {
			seen[k] = true
			sel = append(sel, sk{k, m.compareKeyRank(k)})
		}
	}
	if len(sel) < 2 {
		return left, right, "select at least 2 rows to compare", false
	}
	// older = max rank, newer = min rank.
	oldest, newest := sel[0], sel[0]
	hasWip := false
	for _, s := range sel {
		if s.rank < 0 {
			hasWip = true
		}
		if s.rank > oldest.rank {
			oldest = s
		}
		if s.rank < newest.rank {
			newest = s
		}
	}
	if len(sel) == 2 {
		return m.compareKeyEndpoint(oldest.key), m.compareKeyEndpoint(newest.key), "", true
	}
	if hasWip {
		return left, right, "range compare (3+) is commits-only; remove the working tree / staged row", false
	}
	// 3+ commits: squash from oldest^. Refuse if the oldest is a root commit.
	oi := oldest.rank // feed index for a commit key
	if oi >= 0 && oi < len(m.commits) && len(m.commits[oi].Parents) == 0 {
		return left, right, "can't squash a range from the root commit", false
	}
	return model.Endpoint{Kind: model.EndpointCommit, Hash: oldest.key + "^"},
		model.Endpoint{Kind: model.EndpointCommit, Hash: newest.key}, "", true
}
```

- [ ] **Step 5: Run the tests + the existing compare tests (regression)**

Run: `go test ./internal/tui/ -run 'TestCompare' -v`
Expected: PASS — new wip cases plus the existing `TestCompareSelection*` / `TestCompareMark*`.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/tui/*.go
git add internal/tui/ && git commit -m "feat(tui): two-row compare resolves WIP↔commit (mark + ◉ selection)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 3: Docs + full race

**Files:**
- Modify: `internal/tui/help.go` — note WIP rows participate in mark / ◉ compare.
- Modify: `CHANGELOG.md` — `[Unreleased]` → `### Added`.

- [ ] **Step 1: Help update**

In the `h("Commits panel")` section, extend the WIP-rows help line (added in Stage 1) or add a follow-on row:

```go
		r("", "the ◇ Working tree / ◇ Staged rows can be marked (m) or added to the ◉ compare selection like commits — Compare with marked / Compare selection then diffs a commit against your working copy / index"),
```

- [ ] **Step 2: CHANGELOG entry**

Under `## [Unreleased]` → `### Added`:

```markdown
- **Compare a commit against your working copy via the WIP rows.** The
  `◇ Working tree` / `◇ Staged` rows now join the two-row compare flows: mark one
  (`m`) or add it to the `◉` selection, then *Compare with marked* / *Compare
  selection* diffs it against a commit (commit ↔ working tree, commit ↔ index,
  or staged ↔ working tree). A 3+ range stays commits-only. Also fixes marking a
  commit while the tree was dirty landing on the wrong row. (Compare-trees Stage 3
  follow-up.)
```

- [ ] **Step 3: Full race suite**

Run: `./test.sh race`
Expected: `all green`.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/help.go CHANGELOG.md
git commit -m "docs(tui): WIP-row compare integration — help + changelog

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

## Notes for the implementer

- **The latent bug fixed here:** Stage 1's `handleMarkKey` did `key := Key(backingIndex(focus))`. For Commits, `backingIndex` returns a *pure* feed index but `Key` expects a *unified* index, so on a dirty tree marking a commit produced the wrong key (the first `wipCount` commits even got a WIP sentinel). Task 1 keys off `displayIndices[sel]` (the list space) for every panel — correct for non-Commits panels too (there `backingIndex == list index`).
- **Keys are the single identity:** mark + ◉ set both store `panelList.Key`. The resolvers (`compareKeyEndpoint`/`compareKeyRank`) are the only place that knows a key can be a WIP sentinel — everything else just passes keys around.
- **markAlive / markDisplayIndex / compareSetDisplayIndices** already match on `Key` over `displayIndices`, so WIP marks/selections render their `◆`/`◉` correctly with no change.
- **Don't retire** the standalone *Compare against working tree / staged* `.`-menu rows — they remain a one-step shortcut.
- **Value-receiver `Model`:** helpers return the modified copy.
