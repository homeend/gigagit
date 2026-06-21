# Commit highlight-search (`@`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a second Commits-panel search bound to `@` that keeps every commit visible, dims non-matching rows, and steps between matches with `ctrl+↑/↓` — complementary to the existing `/` filter (which hides non-matches).

**Architecture:** TUI-only. New `Model` fields (`highlightQuery`, `highlightTyping`) kept separate from the `/` filter fields so `filterActive`/`displayIndices`/`commitGraphOn` are untouched (highlight never filters or reorders). A pure match/scan core (new `internal/tui/commit_highlight.go`) is the single source of truth for "does row i match" and "next/prev match with wrap". Dimming is a render-time `rowDecorator` over visible rows only (O(visible), preserving the recent render-perf win). `ctrl+↑/↓` reuses the scan helper.

**Tech Stack:** Go 1.26, Bubble Tea (Elm-style value-receiver `Model`), lipgloss.

## Global Constraints

- TUI-only: no changes to `internal/engine`, `internal/domain`, `internal/git`, `internal/cli`, or `internal/agentskill` (no `agentskill.Version` bump).
- `internal/tui` must not import `internal/git` (archtest-guarded).
- Highlight rendering and navigation stay O(visible rows) / O(distance-to-match) — never an O(feed) scan per keystroke or per frame.
- Match test reuses the existing commit haystack: `m.commitHaystackAt(i)` (full hash + branch/ref names + subject), case-insensitive.
- `@` and highlight-search apply only to the Commits panel (`panelCommits`).
- Highlight and the `/` filter are mutually exclusive: activating one clears the other.
- Commit messages end with the trailers:
  `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`
  `Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro`
- Run `go test ./internal/tui/` after each task; `./test.sh race` before merge.

---

### Task 1: Match/scan core + state fields

**Files:**
- Modify: `internal/tui/model.go` — add two fields to the `Model` struct (after the filter fields at `model.go:109-111`).
- Create: `internal/tui/commit_highlight.go` — the match predicate, the scan helper, and `highlightActive`.
- Test: `internal/tui/commit_highlight_test.go` (new).

**Interfaces:**
- Consumes: `m.commits []model.Commit`, `m.commitHaystackAt(i int) string` (view.go:1004).
- Produces:
  - `Model.highlightQuery string`, `Model.highlightTyping bool` (struct fields).
  - `func (m Model) highlightActive() bool` — `m.highlightTyping || m.highlightQuery != ""`.
  - `func (m Model) commitMatchesHighlight(i int) bool` — false when query empty or `i` out of range; else case-insensitive `strings.Contains` of `commitHaystackAt(i)`.
  - `func (m Model) scanHighlightMatch(from, dir int, inclusive bool) (int, bool)` — from index `from`, step by `dir` (+1 / -1), wrapping once over `[0,len(commits))`, return the first matching index and true; `(from, false)` if none or query empty. `inclusive` includes `from` itself as a candidate (used by the type-time snap); exclusive (used by ctrl-nav) starts one step away.

- [ ] **Step 1: Write the failing test**

```go
package tui

import "testing"

func TestCommitMatchesHighlight(t *testing.T) {
	m := loadedModelLinearCommits(t, 6) // subjects c0..c5, newest-first
	m.highlightQuery = "c3"
	// exactly one commit has subject "c3"
	matches := 0
	for i := range m.commits {
		if m.commitMatchesHighlight(i) {
			matches++
		}
	}
	if matches != 1 {
		t.Fatalf("query c3 should match exactly 1 of c0..c5, got %d", matches)
	}
	// empty query matches nothing
	m.highlightQuery = ""
	for i := range m.commits {
		if m.commitMatchesHighlight(i) {
			t.Fatalf("empty query must match nothing (row %d)", i)
		}
	}
}

func TestScanHighlightMatchWrap(t *testing.T) {
	m := loadedModelLinearCommits(t, 6) // commits[0]=c5 (tip) .. commits[5]=c0
	m.highlightQuery = "c" // every row matches

	// exclusive forward from 0 -> 1
	if got, ok := m.scanHighlightMatch(0, +1, false); !ok || got != 1 {
		t.Fatalf("forward from 0 => (%d,%v), want (1,true)", got, ok)
	}
	// exclusive forward from last wraps to 0
	last := len(m.commits) - 1
	if got, ok := m.scanHighlightMatch(last, +1, false); !ok || got != 0 {
		t.Fatalf("forward wrap from %d => (%d,%v), want (0,true)", last, got, ok)
	}
	// exclusive backward from 0 wraps to last
	if got, ok := m.scanHighlightMatch(0, -1, false); !ok || got != last {
		t.Fatalf("backward wrap from 0 => (%d,%v), want (%d,true)", got, ok, last)
	}

	// inclusive: a matching `from` stays put
	if got, ok := m.scanHighlightMatch(2, +1, true); !ok || got != 2 {
		t.Fatalf("inclusive from matching 2 => (%d,%v), want (2,true)", got, ok)
	}

	// a query matching only c0 (commits[5]); exclusive forward from 0 lands on 5
	m.highlightQuery = "c0"
	if got, ok := m.scanHighlightMatch(0, +1, false); !ok || got != len(m.commits)-1 {
		t.Fatalf("c0 forward from 0 => (%d,%v), want (%d,true)", got, ok, len(m.commits)-1)
	}

	// no-match query returns (from,false)
	m.highlightQuery = "zzz"
	if got, ok := m.scanHighlightMatch(3, +1, false); ok || got != 3 {
		t.Fatalf("no-match => (%d,%v), want (3,false)", got, ok)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestCommitMatchesHighlight|TestScanHighlightMatchWrap' -v`
Expected: FAIL — `m.highlightQuery` undefined / `commitMatchesHighlight` undefined.

- [ ] **Step 3: Add the struct fields**

In `internal/tui/model.go`, immediately after the filter fields (after line `filterTyping bool   // true while /-input mode is capturing keys`):

```go
	highlightQuery  string // Commits-panel @-highlight: case-insensitive substring; "" = no committed query
	highlightTyping bool   // true while @-input mode is capturing keys
```

- [ ] **Step 4: Create the core helper file**

Create `internal/tui/commit_highlight.go`:

```go
package tui

import "strings"

// highlightActive reports whether @-highlight search is engaged on the Commits
// panel — either mid-entry (typing) or with a committed query. Drives keybinding
// gating (ctrl+↑/↓ match nav, esc) and the label/footer display. Whether any row
// is actually dimmed depends additionally on highlightQuery != "".
func (m Model) highlightActive() bool {
	return m.highlightTyping || m.highlightQuery != ""
}

// commitMatchesHighlight reports whether commit i matches the current highlight
// query. An empty query matches nothing (so navigation no-ops and — combined
// with the dim gate — nothing is dimmed). Reuses the filter haystack.
func (m Model) commitMatchesHighlight(i int) bool {
	if m.highlightQuery == "" || i < 0 || i >= len(m.commits) {
		return false
	}
	return strings.Contains(
		strings.ToLower(m.commitHaystackAt(i)),
		strings.ToLower(m.highlightQuery),
	)
}

// scanHighlightMatch finds the next matching commit from index `from` stepping by
// dir (+1 forward/newer→older down the feed, -1 backward), wrapping once over the
// whole loaded feed. inclusive lets `from` itself match (used by the type-time
// cursor snap); exclusive starts one step away (used by ctrl+↑/↓ nav). Returns
// (from, false) when there is no match or the query is empty. Cost is O(distance
// to the next match), never a full-feed scan unless there is no match.
func (m Model) scanHighlightMatch(from, dir int, inclusive bool) (int, bool) {
	n := len(m.commits)
	if n == 0 || m.highlightQuery == "" {
		return from, false
	}
	start := from
	if !inclusive {
		start = from + dir
	}
	for k := 0; k < n; k++ {
		i := ((start+dir*k)%n + n) % n
		if m.commitMatchesHighlight(i) {
			return i, true
		}
	}
	return from, false
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestCommitMatchesHighlight|TestScanHighlightMatchWrap' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/commit_highlight.go internal/tui/commit_highlight_test.go
git commit -m "feat(tui): commit highlight-search match/scan core + state

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 2: Entry, input capture, navigation, mutual exclusivity

**Files:**
- Modify: `internal/tui/model.go` — `@` entry case; `/` entry clears highlight; new highlight-typing capture branch; committed-state `ctrl+up`/`ctrl+down` case.
- Test: `internal/tui/commit_highlight_test.go` (append).

**Interfaces:**
- Consumes: `m.highlightQuery`, `m.highlightTyping`, `m.highlightActive()`, `m.scanHighlightMatch(...)` (Task 1); `m.filterQuery`/`m.filterTyping`/`m.filterPanel`; `m.sel[panelCommits]`; `m.pageStep()`; `m.panelLen(panelCommits)`; `m.maybeLoadMoreCommits()`.
- Produces: keyboard behavior; no new exported symbols.

**Context — dispatch order (do not break it):** the key handler in `model.go` checks, in order: diff view (`m.diffView`), the `m.filterTyping` capture loop (`model.go:445`), `m.filesView`, the stash view, then the main panel `switch msg.String()`. The new highlight-typing capture branch must sit **immediately after** the `filterTyping` branch (so it has the same precedence over filesView/stash), and the `@` / `ctrl+up` / `ctrl+down` cases go in the main panel switch alongside `/` (`model.go:842`).

- [ ] **Step 1: Write the failing test**

```go
package tui

import "testing"

func commitsModel(t *testing.T, n int) Model {
	m := loadedModelLinearCommits(t, n)
	m.focus = panelCommits
	m.sel[panelCommits] = 0
	return m
}

func TestHighlightEntryClearsFilter(t *testing.T) {
	m := commitsModel(t, 6)
	// an active / filter
	m.filterPanel = panelCommits
	m.filterQuery = "c1"
	u, _ := m.Update(keyMsg("@"))
	mm := u.(Model)
	if !mm.highlightTyping {
		t.Fatal("@ must start highlight typing")
	}
	if mm.filterQuery != "" || mm.filterTyping {
		t.Fatalf("@ must clear the / filter, got query=%q typing=%v", mm.filterQuery, mm.filterTyping)
	}
}

func TestFilterEntryClearsHighlight(t *testing.T) {
	m := commitsModel(t, 6)
	m.highlightQuery = "c1"
	u, _ := m.Update(keyMsg("/"))
	mm := u.(Model)
	if !mm.filterTyping {
		t.Fatal("/ must start filter typing")
	}
	if mm.highlightQuery != "" || mm.highlightTyping {
		t.Fatalf("/ must clear the highlight, got query=%q typing=%v", mm.highlightQuery, mm.highlightTyping)
	}
}

func TestHighlightTypingSnapsCursorAndEnterKeeps(t *testing.T) {
	m := commitsModel(t, 6) // commits[0]=c5 .. commits[5]=c0
	u, _ := m.Update(keyMsg("@"))
	m = u.(Model)
	// type "c0" — matches only commits[5]; cursor should snap there
	for _, r := range "c0" {
		u, _ = m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	if m.highlightQuery != "c0" {
		t.Fatalf("query=%q want c0", m.highlightQuery)
	}
	if m.sel[panelCommits] != len(m.commits)-1 {
		t.Fatalf("cursor=%d want %d (snap to the c0 match)", m.sel[panelCommits], len(m.commits)-1)
	}
	// enter keeps the query, ends typing
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	if m.highlightTyping || m.highlightQuery != "c0" {
		t.Fatalf("enter should keep query and stop typing, got typing=%v query=%q", m.highlightTyping, m.highlightQuery)
	}
	// esc clears
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	if m.highlightActive() {
		t.Fatalf("esc must clear highlight")
	}
}

func TestHighlightCtrlNavMovesBetweenMatches(t *testing.T) {
	m := commitsModel(t, 6)
	m.highlightQuery = "c" // every row matches; committed (not typing)
	m.sel[panelCommits] = 0
	u, _ := m.Update(keyMsg("ctrl+down"))
	m = u.(Model)
	if m.sel[panelCommits] != 1 {
		t.Fatalf("ctrl+down => %d, want 1", m.sel[panelCommits])
	}
	u, _ = m.Update(keyMsg("ctrl+up"))
	m = u.(Model)
	if m.sel[panelCommits] != 0 {
		t.Fatalf("ctrl+up => %d, want 0", m.sel[panelCommits])
	}
	// ctrl+up wraps from 0 to last
	u, _ = m.Update(keyMsg("ctrl+up"))
	m = u.(Model)
	if m.sel[panelCommits] != len(m.commits)-1 {
		t.Fatalf("ctrl+up wrap => %d, want %d", m.sel[panelCommits], len(m.commits)-1)
	}
}
```

(`keyMsg` is the existing test helper used across the package, e.g. `compare_diff_test.go`.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestHighlight|TestFilterEntryClearsHighlight' -v`
Expected: FAIL — `@` does nothing (no highlightTyping), ctrl+down doesn't move between matches.

- [ ] **Step 3: Add the highlight-typing capture branch**

In `internal/tui/model.go`, immediately after the `if m.filterTyping { ... return m, nil }` block (ends at the line `return m, nil` near `model.go:492`), insert:

```go
		// @-highlight typing captures every key (the panel label shows the query).
		// Mirrors the filter loop, but: ctrl+↑/↓ jump matches, and a query edit
		// snaps the cursor to the nearest match instead of resetting it to row 0.
		if m.highlightTyping {
			switch msg.Type {
			case tea.KeyCtrlC:
				return m, tea.Quit
			case tea.KeyEsc:
				m.highlightTyping = false
				m.highlightQuery = ""
			case tea.KeyEnter:
				m.highlightTyping = false // commit: highlight stays active
			case tea.KeyUp:
				if m.sel[panelCommits] > 0 {
					m.sel[panelCommits]--
				}
			case tea.KeyDown:
				if m.sel[panelCommits] < m.panelLen(panelCommits)-1 {
					m.sel[panelCommits]++
				}
			case tea.KeyCtrlUp:
				if i, ok := m.scanHighlightMatch(m.sel[panelCommits], -1, false); ok {
					m.sel[panelCommits] = i
				}
			case tea.KeyCtrlDown:
				if i, ok := m.scanHighlightMatch(m.sel[panelCommits], +1, false); ok {
					m.sel[panelCommits] = i
				}
			case tea.KeyBackspace, tea.KeyCtrlH:
				if r := []rune(m.highlightQuery); len(r) > 0 {
					m.highlightQuery = string(r[:len(r)-1])
				}
				m = m.snapToHighlightMatch()
			case tea.KeySpace:
				m.highlightQuery += " "
				m = m.snapToHighlightMatch()
			case tea.KeyRunes:
				m.highlightQuery += string(msg.Runes)
				m = m.snapToHighlightMatch()
			}
			return m, nil
		}
```

- [ ] **Step 4: Add the snap helper to `commit_highlight.go`**

Append to `internal/tui/commit_highlight.go`:

```go
// snapToHighlightMatch moves the Commits cursor to the nearest match at or after
// its current position (wrapping). No-op when there is no match (or empty query),
// leaving the cursor where it is.
func (m Model) snapToHighlightMatch() Model {
	if i, ok := m.scanHighlightMatch(m.sel[panelCommits], +1, true); ok {
		m.sel[panelCommits] = i
	}
	return m
}
```

- [ ] **Step 5: Add the `@` entry case and committed ctrl-nav, and clear-on-`/`**

In `internal/tui/model.go`, change the `/` case (`model.go:842-848`) to also clear any highlight:

```go
		case "/":
			if !m.running && !m.loading {
				m.highlightTyping = false // mutually exclusive with @-highlight
				m.highlightQuery = ""
				m.filterPanel = m.focus
				m.filterQuery = ""
				m.filterTyping = true
				m.sel[m.focus] = 0
			}
```

Immediately after that `/` case, add:

```go
		case "@":
			if !m.running && !m.loading && m.focus == panelCommits {
				m.filterTyping = false // mutually exclusive with the / filter
				if m.filterPanel == panelCommits {
					m.filterQuery = ""
				}
				m.highlightQuery = ""
				m.highlightTyping = true
			}
		case "ctrl+up":
			if m.highlightActive() && m.focus == panelCommits {
				if i, ok := m.scanHighlightMatch(m.sel[panelCommits], -1, false); ok {
					m.sel[panelCommits] = i
				}
				return m, nil
			}
		case "ctrl+down":
			if m.highlightActive() && m.focus == panelCommits {
				if i, ok := m.scanHighlightMatch(m.sel[panelCommits], +1, false); ok {
					m.sel[panelCommits] = i
				}
				return m, nil
			}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestHighlight|TestFilterEntryClearsHighlight' -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/model.go internal/tui/commit_highlight.go internal/tui/commit_highlight_test.go
git commit -m "feat(tui): @-highlight entry, input capture, ctrl+arrow match nav

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 3: Dim non-matches + `@query` label

**Files:**
- Modify: `internal/tui/commit_ident.go` — add a whole-row dim style + decorator constructor.
- Modify: `internal/tui/view.go` — apply the dim decorator in `commitDecoratorsRange`.
- Modify: `internal/tui/viewstate.go` — `panelLabel` shows `@query` when highlight active.
- Test: `internal/tui/commit_highlight_render_test.go` (new).

**Interfaces:**
- Consumes: `m.highlightActive()`, `m.commitMatchesHighlight(i)`, `m.highlightQuery`; `rowDecorator` (window.go:29); the existing `commitDecoratorsRange` loop (view.go:931).
- Produces: `func dimRowDecorator() rowDecorator`; visible-row dimming; `@query` label text.

**Note on precedence:** `renderPanel` (view.go:476-481) does NOT apply a row's decorator to the focused selected row (selection style wins). So the cursor row never gets dimmed — correct. Non-matching rows get a whole-row dim; matching rows keep the normal lane-color/lineage decoration.

- [ ] **Step 1: Write the failing test**

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestHighlightDimsNonMatchesInRender(t *testing.T) {
	prev := lipgloss.ColorProfile() // force color in non-TTY tests (lipgloss emits none otherwise)
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	m := loadedModelLinearCommits(t, 6)
	u, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = u.(Model)
	m.loading = false
	m.focus = panelCommits
	m.sel[panelCommits] = 0 // tip = c5 (a non-match for "c0"), but selected row is never dimmed
	m.highlightQuery = "c0" // matches only commits[5]

	raw := m.View()
	out := ansi.Strip(raw)

	// All commits remain visible (highlight never filters).
	for _, sub := range []string{"c0", "c1", "c5"} {
		if !strings.Contains(out, sub) {
			t.Fatalf("highlight must keep all commits visible; missing %q", sub)
		}
	}
	// A non-matching, non-selected row carries ANSI styling (it is dimmed); the
	// stripped text still shows it. Assert the raw output contains the dim color
	// code (240) somewhere, proving the decorator ran.
	if !strings.Contains(raw, "240") {
		t.Fatal("expected a dim (color 240) styled row in the highlighted render")
	}
}

func TestHighlightLabelShowsQuery(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.highlightQuery = "fix"
	label := m.panelLabel(panelCommits, "Commits")
	if !strings.Contains(label, "@fix") {
		t.Fatalf("label = %q, want it to contain @fix", label)
	}
}
```

(Imports: this file uses `lipgloss` — already imported package-wide via other files; add the explicit import if `go vet` requires it in this file. `keyMsg`/`loadedModelLinearCommits` are existing helpers.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestHighlightDimsNonMatchesInRender|TestHighlightLabelShowsQuery' -v`
Expected: FAIL — no dim styling applied; label has no `@fix`.

- [ ] **Step 3: Add the dim style + decorator**

In `internal/tui/commit_ident.go`, near `dimIdentStyle` (line 18), add:

```go
// dimRowStyle grays an entire commit row that does NOT match the active
// @-highlight query, de-emphasizing it while keeping it visible.
var dimRowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

// dimRowDecorator dims a whole visible line (all visual lines, including wrap
// continuations). Width is preserved (a foreground style adds no cells).
func dimRowDecorator() rowDecorator {
	return func(visible string, hscroll, visualLine int) string {
		return dimRowStyle.Render(visible)
	}
}
```

- [ ] **Step 4: Apply it in `commitDecoratorsRange`**

In `internal/tui/view.go`, inside the `for j := lo; j < hi; j++` loop of `commitDecoratorsRange`, right after the `ci`/range guard (after the `if ci < 0 || ci >= len(m.commits) { continue }` block, ~view.go:946), insert:

```go
		// @-highlight: a non-matching row is dimmed whole; matching rows keep the
		// normal lane/lineage decoration below. Selection style still wins in
		// renderPanel, so the cursor row is never dimmed.
		if m.highlightActive() && m.highlightQuery != "" && !m.commitMatchesHighlight(ci) {
			decos[j] = dimRowDecorator()
			continue
		}
```

- [ ] **Step 5: Show the query in the label**

In `internal/tui/viewstate.go`, in `panelLabel` after the existing filter block (after `model.go`'s `} else if m.filterActive(p) { base += " /" + m.filterQuery }`, viewstate.go:487), add:

```go
	if p == panelCommits {
		if m.highlightTyping {
			base += " @" + m.highlightQuery + "█"
		} else if m.highlightQuery != "" {
			base += " @" + m.highlightQuery
		}
	}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestHighlightDimsNonMatchesInRender|TestHighlightLabelShowsQuery' -v`
Expected: PASS.

- [ ] **Step 7: Run the whole tui package**

Run: `go test ./internal/tui/`
Expected: ok (no golden render regressions; highlight is inert when `highlightQuery == ""`).

- [ ] **Step 8: Commit**

```bash
git add internal/tui/commit_ident.go internal/tui/view.go internal/tui/viewstate.go internal/tui/commit_highlight_render_test.go
git commit -m "feat(tui): dim non-matching commit rows + @query label for highlight-search

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 4: Footer, help, CHANGELOG

**Files:**
- Modify: `internal/tui/footer.go` — highlight-typing footer hint.
- Modify: `internal/tui/help.go` — Commits-panel help entries for `@` and `ctrl+↑/↓`.
- Modify: `CHANGELOG.md` — "Added" entry under `[Unreleased]`.
- Test: `internal/tui/commit_highlight_test.go` (append a footer assertion).

**Interfaces:**
- Consumes: `m.highlightTyping`, `m.footerLine()` (footer.go:108).
- Produces: footer/help strings.

- [ ] **Step 1: Write the failing test**

```go
func TestHighlightFooterWhileTyping(t *testing.T) {
	m := commitsModel(t, 3)
	u, _ := m.Update(keyMsg("@"))
	m = u.(Model)
	if got := m.footerLine(); !strings.Contains(got, "highlight") {
		t.Fatalf("footer while @-typing = %q, want it to mention highlight", got)
	}
}
```

(Add `"strings"` to the test file imports if not already present.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run TestHighlightFooterWhileTyping -v`
Expected: FAIL — footer shows the normal panel line.

- [ ] **Step 3: Add the footer hint**

In `internal/tui/footer.go`, immediately after the `if m.filterTyping { ... }` block (footer.go:114-116), add:

```go
	if m.highlightTyping {
		return "highlight: type to search  [↑↓] move  [ctrl+↑/↓] prev/next match  [enter] keep  [esc] clear"
	}
```

- [ ] **Step 4: Add help entries**

In `internal/tui/help.go`, in the `h("Commits panel")` section, directly after the
`r("l", "show the selected commit's files in the left column")` line (help.go:111), add:

```go
		r("@", "highlight search: keep all commits visible, dim non-matches (graph stays); ctrl+↑/↓ jump prev/next match; enter keeps, esc clears"),
```

(The existing `h("Filter mode (/)")` subsection documents `/`; this `@` line is the Commits-panel highlight counterpart. Leave the `/` docs unchanged.)

- [ ] **Step 5: Add the CHANGELOG entry**

In `CHANGELOG.md`, under `## [Unreleased]` → `### Added`, add:

```markdown
- **Highlight search in the Commits panel (`@`).** A second search that
  complements `/`: instead of filtering the feed, `@` keeps every commit visible,
  dims non-matching rows, and leaves the commit graph drawn. `ctrl+↑/↓` jump to
  the previous / next match (wrapping); `enter` keeps the highlight, `esc` clears
  it. `@` and `/` are mutually exclusive.
```

If there is no `### Added` under `## [Unreleased]`, create it directly under the `## [Unreleased]` heading.

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/tui/ -run TestHighlightFooterWhileTyping -v`
Expected: PASS.

- [ ] **Step 7: Full race suite**

Run: `./test.sh race`
Expected: `all green`.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/footer.go internal/tui/help.go internal/tui/commit_highlight_test.go CHANGELOG.md
git commit -m "docs(tui): footer/help + changelog for @ highlight-search

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

## Notes for the implementer

- **Value-receiver `Model`:** every handler returns the modified copy. The
  highlight-typing branch mutates `m` and `return m, nil` — never store state in
  package globals.
- **`keyMsg` helper:** the package already has a `keyMsg(string) tea.KeyMsg`
  test helper; `keyMsg("ctrl+down")` produces the right `tea.KeyMsg`. If a
  specific key type isn't produced by the string form, construct the
  `tea.KeyMsg{Type: tea.KeyCtrlDown}` directly.
- **Don't touch `displayIndices`/`filterActive`/`commitGraphOn`:** highlight must
  never filter or reorder; keeping the state separate guarantees the graph stays
  on and rows stay contiguous.
- **Perf:** the dim decorator is built only for the windowed `[lo,hi)` rows
  (Task 3 sits inside the existing windowed loop), and nav scans stop at the
  first match — no per-keystroke/per-frame O(feed) work.
