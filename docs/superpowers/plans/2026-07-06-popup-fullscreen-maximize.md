# Popup fullscreen-maximize (`T`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a content-heavy popup be toggled to a near-fullscreen bordered box with capital `T`, the same key that fullscreens a focused panel.

**Architecture:** One embeddable `popupMax` (a `maximized` flag + a `handleMaxKey` toggle helper) and two shared resolvers (`popupResolveWidth`, `popupMaxRowCap`). Each opt-in popup embeds `popupMax`, calls `handleMaxKey` at the top of its *navigation* branch (so capital `T` stays typeable in filter/text fields), and honors `p.maximized` in its `render`/`box` — widening to `popupFullInnerWidth`, and (for the four fixed-cap popups) lifting its row cap.

**Tech Stack:** Go 1.26, Bubble Tea, lipgloss. TUI package `internal/tui`. Tests use a plain `Model{}` with `width`/`height` set; no git needed.

## Global Constraints

- `T` is handled in each opt-in popup's **navigation** branch, never as a global intercept before `topLayer().update` — capital `T` is a legal character in a branch name / commit message / tag name / `/`-filter query, so a global swallow would make those un-typeable.
- Maximized inner width = `popupFullInnerWidth(w)` (w−8). A maximized popup keeps its border, title, and hint footer (it is NOT a borderless edge-to-edge takeover).
- `esc` closes a popup outright as today. `maximized` is transient per-instance view state; it resets when the popup closes. It is never a separate step on the exit ladder.
- The four fixed-cap popups (`bookmarkPopup`/`shelfPopup`/`repoPopup` cap 12, `fileFinderPopup` cap 16) also lift their row cap to `popupMaxRowCap(termH)` when maximized. `contentPopup` (`h-7`) and `gitConfigPopup` (`termH-12`) already derive rows from the terminal and stay width-only.
- Each opt-in popup adds `[T] full` to its hint line; `help.go` notes that `T` also maximizes popups. (Advertise in help AND footer.)
- Tests assert box **geometry** — `lipgloss.Width` of the box, and body-line counts — never substring presence (a 0-width box truncates labels to `…`, so substring assertions are toothless).
- The files-view (`m.filesView`, a `contentPopup`) is routed by its own `updateFilesViewKey` and rendered by `renderFilesView`, NOT by `contentPopup.update`/`box`. Converting `contentPopup` therefore affects only the layer-context contentPopups (the `?` help window and the switcher cheat-sheets) — leave `updateFilesViewKey` and `renderFilesView` untouched.

---

### Task 1: Shared maximize mechanism

**Files:**
- Create: `internal/tui/popup_max.go`
- Test: `internal/tui/popup_max_test.go`

**Interfaces:**
- Consumes: `popupFullInnerWidth(w int) int` (existing, `view.go:290`).
- Produces:
  - `type popupMax struct{ maximized bool }`
  - `func (p *popupMax) maxed() bool`
  - `func (p *popupMax) handleMaxKey(msg tea.KeyMsg) bool` — toggles `maximized` on `"T"`, returns whether it consumed the key.
  - `func popupResolveWidth(w int, maximized bool, normal int) int`
  - `func popupMaxRowCap(termH int) int`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/popup_max_test.go`:

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func runeKey(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestHandleMaxKeyTogglesOnT(t *testing.T) {
	var p popupMax
	if !p.handleMaxKey(runeKey("T")) {
		t.Fatal(`"T" must be consumed`)
	}
	if !p.maxed() {
		t.Fatal(`"T" must set maximized`)
	}
	if !p.handleMaxKey(runeKey("T")) || p.maxed() {
		t.Fatal(`second "T" must toggle back off`)
	}
}

func TestHandleMaxKeyIgnoresOtherKeys(t *testing.T) {
	var p popupMax
	if p.handleMaxKey(runeKey("x")) {
		t.Fatal(`"x" must NOT be consumed`)
	}
	if p.maxed() {
		t.Fatal(`"x" must not set maximized`)
	}
}

func TestPopupResolveWidth(t *testing.T) {
	if got := popupResolveWidth(200, false, 56); got != 56 {
		t.Fatalf("normal: got %d, want 56", got)
	}
	if got := popupResolveWidth(200, true, 56); got != popupFullInnerWidth(200) {
		t.Fatalf("maximized: got %d, want %d", got, popupFullInnerWidth(200))
	}
}

func TestPopupMaxRowCap(t *testing.T) {
	if got := popupMaxRowCap(50); got != 38 {
		t.Fatalf("tall: got %d, want 38", got)
	}
	if got := popupMaxRowCap(5); got != 3 {
		t.Fatalf("floor: got %d, want 3", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd internal/tui && go test -run 'TestHandleMaxKey|TestPopupResolveWidth|TestPopupMaxRowCap' ./...`
Expected: build failure — `undefined: popupMax`, `undefined: popupResolveWidth`, `undefined: popupMaxRowCap`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/tui/popup_max.go`:

```go
package tui

import tea "github.com/charmbracelet/bubbletea"

// popupMax is embedded by any popup that supports T-to-fullscreen. The layer
// stack holds the popup *pointer*, so the flag persists across Model value
// copies (same rationale as the modal/popup pointer fields). maximized is
// transient view state: it resets when the popup instance is dropped.
type popupMax struct{ maximized bool }

// maxed reports whether the popup is currently maximized.
func (p *popupMax) maxed() bool { return p.maximized }

// handleMaxKey toggles maximize on "T" and reports whether it consumed the key.
// Opted-in popups call this at the top of their NAVIGATION branch so that "T"
// stays a literal character while a filter / text field is capturing.
func (p *popupMax) handleMaxKey(msg tea.KeyMsg) bool {
	if msg.String() == "T" {
		p.maximized = !p.maximized
		return true
	}
	return false
}

// popupResolveWidth returns the near-fullscreen inner width when maximized,
// else the popup's normal width. popupFullInnerWidth (w-8) is the same width
// the external-tools wizard renders at permanently.
func popupResolveWidth(w int, maximized bool, normal int) int {
	if maximized {
		return popupFullInnerWidth(w)
	}
	return normal
}

// popupMaxRowCap is the visible-row budget for a maximized list popup whose
// normal budget is a small fixed constant: terminal height minus box chrome,
// floored so a tiny terminal still shows a few rows. Mirrors gitConfigPopup's
// existing capRows (termH - 12).
func popupMaxRowCap(termH int) int {
	n := termH - 12
	if n < 3 {
		n = 3
	}
	return n
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd internal/tui && go test -run 'TestHandleMaxKey|TestPopupResolveWidth|TestPopupMaxRowCap' ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/popup_max.go internal/tui/popup_max_test.go
git commit -m "feat(tui): shared popup maximize mechanism (popupMax + resolvers)"
```

---

### Task 2: Convert `contentPopup` (width-only; reference; covers `?` help + switcher cheat-sheets)

**Files:**
- Modify: `internal/tui/content_popup.go` (struct ~31, `update` ~151, `box` ~243, hint ~289)
- Test: `internal/tui/content_popup_test.go`

**Interfaces:**
- Consumes: `popupMax`, `popupResolveWidth` (Task 1); `contentPopupWidth` (existing, `content_popup.go:206`).
- Produces: a maximizable `contentPopup`. No new exported names.

Note: `contentPopup` already derives its visible-row count from the terminal (`contentPageRows() = h - 7`), so this task is width-only — no row-cap change.

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/content_popup_test.go`:

```go
func TestContentPopupMaximizeWidens(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := newContentPopup("Title", contentLines(4))

	normal := lipgloss.Width(p.box(m))
	p.maximized = true
	maxed := lipgloss.Width(p.box(m))
	if maxed <= normal {
		t.Fatalf("maximized width %d must exceed normal %d", maxed, normal)
	}
}

func TestContentPopupTKeyDoesNotMaximizeWhileTyping(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := newContentPopup("Title", contentLines(4))
	p.typing = true // /-filter input mode

	p.update(m, runeKey("T"))
	if p.maximized {
		t.Fatal(`"T" while typing must not maximize`)
	}
	if p.query != "T" {
		t.Fatalf(`"T" while typing must be a literal char; query=%q`, p.query)
	}
}

func TestContentPopupTKeyMaximizesInNavMode(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := newContentPopup("Title", contentLines(4))

	p.update(m, runeKey("T"))
	if !p.maximized {
		t.Fatal(`"T" in nav mode must maximize`)
	}
}

func TestContentPopupEscClosesWhileMaximized(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := newContentPopup("Title", contentLines(4))
	m = m.pushLayer(p)
	p.maximized = true

	mm, _ := p.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if mm.topLayer() != nil {
		t.Fatal("esc must close the maximized popup outright (stack not popped)")
	}
}
```

(`contentLines` is an existing helper in `content_popup_test.go`; `lipgloss` is already imported there.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd internal/tui && go test -run TestContentPopup ./...`
Expected: FAIL — `p.maximized undefined` (field not yet embedded).

- [ ] **Step 3: Write minimal implementation**

In `internal/tui/content_popup.go`, embed `popupMax` as the first field of the struct (line ~31):

```go
type contentPopup struct {
	popupMax
	title   string
	lines   []contentLine // full, unfiltered content
	query   string        // case-insensitive substring over non-heading lines
	typing  bool          // true while /-input mode is capturing keys
	sel     int           // cursor index into the FILTERED view
	mode    dispMode      // text display mode; z cycles
	hscroll int           // modeScroll horizontal offset
	footer  string        // optional line above the hint (e.g. commit author · date); "" = none
}
```

In `update`, insert the maximize toggle at the top of the navigation branch — immediately after the `if p.typing { … return … }` block closes and BEFORE `switch msg.String()` (line ~151):

```go
	if p.handleMaxKey(msg) { // "T" toggles fullscreen (nav mode only; typing returns above)
		return m, nil
	}
	switch msg.String() {
	case "z": // cycle the text display mode (cutoff / wrap / scroll)
```

In `box`, route the width through the resolver (line ~243):

```go
	w, _ := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, contentPopupWidth(w))
```

Update the hint (line ~289) to advertise `T`:

```go
	hint := "[/] search  [z] mode  [T] full  [q] close"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd internal/tui && go test -run TestContentPopup ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/content_popup.go internal/tui/content_popup_test.go
git commit -m "feat(tui): T maximizes the content viewer (and ? help / switcher cheat-sheets)"
```

---

### Task 3: Convert `gitConfigPopup` (width-only + reload-survival)

**Files:**
- Modify: `internal/tui/gitconfig_popup.go` (struct ~23, `update` ~144, `box` ~464, hint ~541)
- Test: `internal/tui/gitconfig_popup_test.go`

**Interfaces:**
- Consumes: `popupMax`, `popupResolveWidth` (Task 1); `popupWideInnerWidth` (existing).
- Produces: a maximizable `gitConfigPopup`. Its rows already cap at `termH-12`, so width-only.

Reload-survival note: `gitConfigRowsMsg` mutates the existing stack instance in place (`layerOf[*gitConfigPopup](m)` → `p.loading = false`, `model.go:333`), so `maximized` set on that instance survives a post-write row re-read. The test locks this in.

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/gitconfig_popup_test.go`:

```go
func TestGitConfigPopupMaximizeWidens(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &gitConfigPopup{}

	normal := lipgloss.Width(p.box(m))
	p.maximized = true
	maxed := lipgloss.Width(p.box(m))
	if maxed <= normal {
		t.Fatalf("maximized width %d must exceed normal %d", maxed, normal)
	}
}

func TestGitConfigPopupTKeyDoesNotMaximizeWhileFiltering(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &gitConfigPopup{filtering: true}

	p.update(m, runeKey("T"))
	if p.maximized {
		t.Fatal(`"T" while filtering must not maximize`)
	}
	if p.query != "T" {
		t.Fatalf(`"T" while filtering must be a literal char; query=%q`, p.query)
	}
}

func TestGitConfigPopupMaximizeSurvivesRowReload(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	m = m.pushLayer(&gitConfigPopup{loading: true})
	p := layerOf[*gitConfigPopup](m)
	p.maximized = true

	// A post-write row re-read lands on the same instance.
	mm, _ := m.Update(gitConfigRowsMsg{gen: m.gitConfigGen, rows: nil})
	if !layerOf[*gitConfigPopup](mm.(Model)).maxed() {
		t.Fatal("maximized must survive a gitConfigRowsMsg reload")
	}
}
```

(`lipgloss` is already imported in `gitconfig_popup_test.go`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd internal/tui && go test -run TestGitConfigPopup ./...`
Expected: FAIL — `p.maximized undefined`.

- [ ] **Step 3: Write minimal implementation**

Embed `popupMax` as the first field of the struct (line ~23):

```go
type gitConfigPopup struct {
	popupMax
	// … existing fields unchanged …
```

In `update`, insert the toggle after the `if p.edit != nil { … }` and `if p.filtering { … }` guards return, and BEFORE the `// Navigation mode.` comment / `switch msg.String()` at line ~144:

```go
	if p.handleMaxKey(msg) { // "T" toggles fullscreen (nav mode only)
		return m, nil
	}
	// Navigation mode. Display-mode + pan keys act here (query chars while filtering).
	switch msg.String() {
```

In `box`, route the width through the resolver (line ~464):

```go
	w, termH := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupWideInnerWidth(w))
```

Add `[T] full` to the hint slice (line ~541), before `[esc] close`:

```go
	hint := []string{"[l] set local", "[g] set global", "[u] unset", "[/] filter", "[z] mode", "[T] full", "[esc] close"}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd internal/tui && go test -run TestGitConfigPopup ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/gitconfig_popup.go internal/tui/gitconfig_popup_test.go
git commit -m "feat(tui): T maximizes the git-config explorer"
```

---

### Task 4: Convert `bookmarkPopup` + `shelfPopup` (width + row-cap; twins)

**Files:**
- Modify: `internal/tui/bookmark_popup.go` (struct ~21, `update` ~218, `renderBookmarkPopupBox` ~102, hint ~146)
- Modify: `internal/tui/shelf_popup.go` (struct ~16, `update` ~190, `renderShelfPopupBox` ~83, hint ~128)
- Test: `internal/tui/bookmark_popup_test.go`, `internal/tui/shelf_popup_test.go`

**Interfaces:**
- Consumes: `popupMax`, `popupResolveWidth`, `popupMaxRowCap` (Task 1); `popupWideInnerWidth` (existing).
- Produces: maximizable `bookmarkPopup` and `shelfPopup`. Both cap rows at a fixed 12, so both widen AND lift the cap.

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/bookmark_popup_test.go`:

```go
func TestBookmarkPopupMaximizeWidensAndLiftsRowCap(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &bookmarkPopup{}
	for i := 0; i < 30; i++ { // more than the fixed cap of 12
		p.items = append(p.items, model.Bookmark{ID: fmt.Sprintf("b%d", i), Path: fmt.Sprintf("path/to/file%d", i)})
		p.rows = append(p.rows, fmt.Sprintf("path/to/file%d", i))
	}

	normal := m.renderBookmarkPopupBox(p)
	p.maximized = true
	maxed := m.renderBookmarkPopupBox(p)

	if lipgloss.Width(maxed) <= lipgloss.Width(normal) {
		t.Fatalf("maximized width %d must exceed normal %d", lipgloss.Width(maxed), lipgloss.Width(normal))
	}
	if lipgloss.Height(maxed) <= lipgloss.Height(normal) {
		t.Fatalf("maximized must show more rows: height %d vs %d", lipgloss.Height(maxed), lipgloss.Height(normal))
	}
}

func TestBookmarkPopupTKeyDoesNotMaximizeWhileFiltering(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &bookmarkPopup{filtering: true}
	p.update(m, runeKey("T"))
	if p.maximized {
		t.Fatal(`"T" while filtering must not maximize`)
	}
	if p.filter != "T" {
		t.Fatalf(`"T" while filtering must be a literal char; filter=%q`, p.filter)
	}
}
```

Append to `internal/tui/shelf_popup_test.go`:

```go
func TestShelfPopupMaximizeWidensAndLiftsRowCap(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &shelfPopup{}
	for i := 0; i < 30; i++ { // more than the fixed cap of 12
		p.items = append(p.items, model.ShelfEntry{})
		p.rows = append(p.rows, fmt.Sprintf("some/long/origin/path/file%d", i))
	}

	normal := m.renderShelfPopupBox(p)
	p.maximized = true
	maxed := m.renderShelfPopupBox(p)

	if lipgloss.Width(maxed) <= lipgloss.Width(normal) {
		t.Fatalf("maximized width %d must exceed normal %d", lipgloss.Width(maxed), lipgloss.Width(normal))
	}
	if lipgloss.Height(maxed) <= lipgloss.Height(normal) {
		t.Fatalf("maximized must show more rows: height %d vs %d", lipgloss.Height(maxed), lipgloss.Height(normal))
	}
}

func TestShelfPopupTKeyDoesNotMaximizeWhileFiltering(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &shelfPopup{filtering: true}
	p.update(m, runeKey("T"))
	if p.maximized {
		t.Fatal(`"T" while filtering must not maximize`)
	}
	if p.filter != "T" {
		t.Fatalf(`"T" while filtering must be a literal char; filter=%q`, p.filter)
	}
}
```

(Confirm `model`, `fmt`, and `lipgloss` imports exist in each test file; add any that are missing.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd internal/tui && go test -run 'TestBookmarkPopupMaximize|TestBookmarkPopupTKey|TestShelfPopupMaximize|TestShelfPopupTKey' ./...`
Expected: FAIL — `p.maximized undefined`.

- [ ] **Step 3: Write minimal implementation**

In `bookmark_popup.go`, embed `popupMax` as the first struct field (line ~21):

```go
type bookmarkPopup struct {
	popupMax
	// … existing fields unchanged …
```

In `bookmarkPopup.update`, insert the toggle after the `if p.filtering { … }` block returns and BEFORE the display-mode `switch msg.String()` at line ~218:

```go
	if p.handleMaxKey(msg) { // "T" toggles fullscreen (nav mode only)
		return m, nil
	}
	// Display-mode + pan keys take precedence over the navigation switch …
	switch msg.String() {
	case "z":
```

In `renderBookmarkPopupBox`, capture `termH`, route the width, and lift the row cap (lines ~103–138):

```go
	w, termH := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupWideInnerWidth(w))
```

and replace the fixed cap:

```go
		capRows := 12
		if p.maximized {
			capRows = popupMaxRowCap(termH)
		}
		h := len(vis)
		if h > capRows {
			h = capRows
		}
```

Add `[T] full` to the hint (line ~146), before `[esc] close`:

```go
	hint := []string{"[?] keys", "[enter] jump", "[e] editor", "[p] paste", "[t] temp dir", "[m] mark/compare", "[x] remove", "[c] vs shelf", "[/] filter", "[z] mode", "[T] full", "[esc] close"}
```

Apply the identical four edits to `shelf_popup.go`: embed `popupMax` (struct ~16); `handleMaxKey` before the nav `switch msg.String()` (line ~190); `w, termH := m.overlayDims()` + `popupResolveWidth(w, p.maximized, popupWideInnerWidth(w))` in `renderShelfPopupBox` (line ~84–85); the `capRows` lift replacing `if h > 12` (line ~116–119); and `[T] full` before `[esc] close` in the hint (line ~128):

```go
	hint := []string{"[?] keys", "[enter] diff", "[e] editor", "[p] restore", "[t] temp dir", "[m] mark/compare", "[x] remove", "[c] vs bookmark", "[/] filter", "[z] mode", "[T] full", "[esc] close"}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd internal/tui && go test -run 'TestBookmarkPopup|TestShelfPopup' ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/bookmark_popup.go internal/tui/bookmark_popup_test.go internal/tui/shelf_popup.go internal/tui/shelf_popup_test.go
git commit -m "feat(tui): T maximizes the bookmark and shelf switchers"
```

---

### Task 5: Convert `repoPopup` + `fileFinderPopup` (width + row-cap)

**Files:**
- Modify: `internal/tui/repo_popup.go` (struct ~16, `update` ~100, `box` ~186, cap ~222, hint ~228)
- Modify: `internal/tui/file_finder.go` (struct ~24, `update` ~139, `box` ~204, cap ~233, hint ~252)
- Test: `internal/tui/repo_popup_test.go`, `internal/tui/file_finder_test.go`

**Interfaces:**
- Consumes: `popupMax`, `popupResolveWidth`, `popupMaxRowCap` (Task 1); `popupInnerWidth` (repo), `popupWideInnerWidth` (finder).
- Produces: maximizable `repoPopup` (fixed cap 12, narrow 56-col normal width) and `fileFinderPopup` (fixed cap 16, nav-first).

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/repo_popup_test.go` (imports `github.com/homeend/gigagit/internal/repos`, `fmt`, `lipgloss` — add any missing):

```go
func TestRepoPopupMaximizeWidensAndLiftsRowCap(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &repoPopup{}
	for i := 0; i < 30; i++ { // more than the fixed cap of 12
		p.entries = append(p.entries, repos.Entry{Path: fmt.Sprintf("/home/user/repos/project-number-%d", i)})
	}

	normal := p.box(m)
	p.maximized = true
	maxed := p.box(m)

	if lipgloss.Width(maxed) <= lipgloss.Width(normal) {
		t.Fatalf("maximized width %d must exceed normal %d", lipgloss.Width(maxed), lipgloss.Width(normal))
	}
	if lipgloss.Height(maxed) <= lipgloss.Height(normal) {
		t.Fatalf("maximized must show more rows: height %d vs %d", lipgloss.Height(maxed), lipgloss.Height(normal))
	}
}

func TestRepoPopupTKeyDoesNotMaximizeWhileFiltering(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &repoPopup{filtering: true}
	p.update(m, runeKey("T"))
	if p.maximized {
		t.Fatal(`"T" while filtering must not maximize`)
	}
	if p.query != "T" {
		t.Fatalf(`"T" while filtering must be a literal char; query=%q`, p.query)
	}
}
```

Append to `internal/tui/file_finder_test.go` (imports `github.com/homeend/gigagit/internal/fuzzy`, `fmt`, `lipgloss` — add any missing):

```go
func TestFileFinderMaximizeWidensAndLiftsRowCap(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &fileFinderPopup{}
	for i := 0; i < 30; i++ { // more than the fixed cap of 16
		p.matches = append(p.matches, fuzzy.Match{S: fmt.Sprintf("some/long/path/to/file-number-%d.go", i)})
	}

	normal := p.box(m)
	p.maximized = true
	maxed := p.box(m)

	if lipgloss.Width(maxed) <= lipgloss.Width(normal) {
		t.Fatalf("maximized width %d must exceed normal %d", lipgloss.Width(maxed), lipgloss.Width(normal))
	}
	if lipgloss.Height(maxed) <= lipgloss.Height(normal) {
		t.Fatalf("maximized must show more rows: height %d vs %d", lipgloss.Height(maxed), lipgloss.Height(normal))
	}
}

func TestFileFinderTKeyDoesNotMaximizeWhileFiltering(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &fileFinderPopup{filtering: true}
	p.update(m, runeKey("T"))
	if p.maximized {
		t.Fatal(`"T" while filtering must not maximize`)
	}
	if p.query != "T" {
		t.Fatalf(`"T" while filtering must be a literal char; query=%q`, p.query)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd internal/tui && go test -run 'TestRepoPopupMaximize|TestRepoPopupTKey|TestFileFinderMaximize|TestFileFinderTKey' ./...`
Expected: FAIL — `p.maximized undefined`.

- [ ] **Step 3: Write minimal implementation**

In `repo_popup.go`: embed `popupMax` (struct ~16); insert `handleMaxKey` before the nav `switch msg.String()` at line ~100:

```go
	if p.handleMaxKey(msg) { // "T" toggles fullscreen (nav mode only)
		return m, nil
	}
	switch msg.String() {
```

In `repoPopup.box`, capture `termH`, route the width, lift the cap (lines ~186–224):

```go
	w, termH := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupInnerWidth(w))
```

```go
		capRows := 12
		if p.maximized {
			capRows = popupMaxRowCap(termH)
		}
		h := len(vis)
		if h > capRows {
			h = capRows
		}
```

Add `[T] full` to the hint (line ~228), before `[esc] close`:

```go
	hint := []string{"[enter] switch", "[ctrl+d] forget", "[/] filter", "[z] mode", "[T] full", "[esc] close"}
```

In `file_finder.go`: embed `popupMax` (struct ~24); insert `handleMaxKey` at the top of the navigation `switch msg.String()` at line ~139 (after the `if p.filtering { … return … }` block):

```go
	// Navigation mode. Display-mode + pan keys act here …
	if p.handleMaxKey(msg) { // "T" toggles fullscreen (nav mode only)
		return m, nil
	}
	switch msg.String() {
	case "z":
```

In `fileFinderPopup.box`, capture `termH`, route the width (line ~203–204):

```go
	w, termH := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupWideInnerWidth(w))
```

and lift the fixed cap of 16 (line ~232–235):

```go
		visH := len(p.matches)
		capRows := 16
		if p.maximized {
			capRows = popupMaxRowCap(termH)
		}
		if visH > capRows {
			visH = capRows
		}
```

Add `[T] full` to the hint (line ~252), before `[esc] close`:

```go
	hint := []string{"[enter] open", "[↑↓ pgup/pgdn] nav", "[/] filter", "[z] mode", "[T] full", "[esc] close"}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd internal/tui && go test -run 'TestRepoPopup|TestFileFinder' ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/repo_popup.go internal/tui/repo_popup_test.go internal/tui/file_finder.go internal/tui/file_finder_test.go
git commit -m "feat(tui): T maximizes the repo switcher and fuzzy file finder"
```

---

### Task 6: Docs + full verification

**Files:**
- Modify: `internal/tui/help.go` (the `?` cheat-sheet rows; existing `T` row ~37)
- Modify: `CHANGELOG.md`
- Modify: `README.md` (if a keybinding/feature table lists popup keys)
- Modify: `CLAUDE.md` (the `tui` package-map row, if the popup-maximize mechanism warrants a sentence)

**Interfaces:** none (docs only).

- [ ] **Step 1: Extend the help cheat-sheet**

`internal/tui/help.go` line ~37 currently reads:

```go
	r("T", "fullscreen the focused panel — any left-column panel or Commits — to …"),
```

Extend that description so it also states the popup behavior, e.g. append `; also maximizes a content popup (help / switchers / config explorer / finder)`. Keep it one row — do not add a second `T` row (a duplicate key row reads as a conflict).

- [ ] **Step 2: Verify the help text renders**

Run: `cd internal/tui && go test -run 'TestHelp|Help' ./...`
Expected: PASS (or no help-specific test — then just confirm the package builds in Step 5).

- [ ] **Step 3: Update CHANGELOG.md**

Add an entry under the current unreleased section:

```markdown
- **Fullscreen popups (`T`).** Capital `T` now toggles a content-heavy popup to
  a near-fullscreen bordered box — the content viewer and `?` help, the
  git-config explorer, the bookmark/shelf/repo switchers, and the fuzzy file
  finder. Mirrors the panel `T`; `esc` still closes. `T` stays a literal
  character while a filter or text field is active.
```

- [ ] **Step 4: Update README.md and CLAUDE.md**

If `README.md` has a keys/shortcuts section mentioning popup navigation, add `T` = maximize popup. In `CLAUDE.md`, if the `tui` row documents popup infrastructure, add one sentence: a shared `popupMax` (embeddable flag + `handleMaxKey`) plus `popupResolveWidth`/`popupMaxRowCap` let a popup toggle to a near-fullscreen box with `T`; opted-in popups call `handleMaxKey` in their nav branch (never a global intercept — capital `T` must stay typeable). Keep edits minimal and factual.

- [ ] **Step 5: Run the full race suite**

Run: `./test.sh race`
Expected: all stages green (vet+gofmt → unit → e2e).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/help.go CHANGELOG.md README.md CLAUDE.md
git commit -m "docs(tui): document popup fullscreen-maximize (T)"
```

---

## Notes for the implementer

- **Struct embedding:** embed `popupMax` as a value field (`popupMax` on its own line, first field). Because every popup lives on the layer stack as a pointer (`*contentPopup`, etc.), the promoted pointer-receiver methods (`handleMaxKey`, `maxed`) and the `p.maximized` field are addressable and mutations persist — the same reason the layer stack holds pointers.
- **Do not** add a global `T` intercept before `topLayer().update` in `model.go`. Every conversion handles `T` inside the popup's own nav branch by design (see Global Constraints).
- **Do not** touch `updateFilesViewKey` or `renderFilesView` — the files-view is a `contentPopup` but has its own router and renderer; the maximize path never reaches it.
- When a test needs the popup's list/filter field names (Tasks 4–5), read the struct first and write concrete assertions; never leave a placeholder.
