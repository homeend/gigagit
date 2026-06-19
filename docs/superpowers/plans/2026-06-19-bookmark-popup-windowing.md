# Bookmark Popup Windowed Display Modes — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render the bookmark quick-switcher list through the shared `renderWindow` primitive so it gets cutoff/wrap/scroll via `z`, `shift+←/→` horizontal pan, and a capped scrolling viewport — exactly like the repo switcher.

**Architecture:** Mirror `internal/tui/repo_popup.go`. Add `mode dispMode` + `hscroll int` to `bookmarkPopup`, rebuild `renderBookmarkPopup` to emit `[]winRow` through `renderWindow`/`popupBox`, and add `z` / `shift+←/→` handling to `updateBookmarkPopupKey` in navigation mode only.

**Tech Stack:** Go 1.26, Bubble Tea, lipgloss. Existing TUI primitives: `renderWindow`/`winRow`/`winOpts` (`window.go`), `popupBox`/`popupInnerWidth`/`popupTextWidth`/`padRight`/`selectedRow` (`view.go`), `dispMode`/`modeCutoff`/`modeScroll`/`(dispMode).next()` (`window.go`), `m.hscrollStep()` (`model.go`).

## Global Constraints

- All work happens in the existing worktree on branch `worktree-bookmark-popup-window` (off `main` tip `4fa94e6`). Use worktree-relative paths only — absolute paths land in the shared checkout.
- `internal/tui` must not import `internal/git` / `internal/shelf` / `internal/bookmark` (archtest-guarded). This change touches none of them.
- The paste-destination popup (`renderBookmarkPastePopup`) is OUT OF SCOPE — it stays a plain `modalStyle` modal.
- Run `./test.sh race` before declaring done.
- Commit messages end with the repo's Co-Authored-By / Claude-Session trailers.

---

### Task 1: Window the bookmark list render

Replace the hand-rolled `strings.Builder` + `modalStyle.Width(...)` render with the repo-popup `renderWindow` shape, and add the two state fields it needs.

**Files:**
- Modify: `internal/tui/bookmark_popup.go` (struct `bookmarkPopup` ~line 21; `renderBookmarkPopup` ~lines 92-119)
- Test: `internal/tui/bookmark_test.go`

**Interfaces:**
- Consumes: `renderWindow(rows []winRow, o winOpts) []string`; `winRow{text string; style lipgloss.Style}`; `winOpts{w, h int; mode dispMode; anchor int; hscroll int}`; `popupInnerWidth(int) int`; `popupTextWidth(int) int`; `popupBox(inner int, content string) string`; `padRight(s string, n int) string`; `selectedRow lipgloss.Style`; `bookmarkDisplay(model.Bookmark) string`; `(*bookmarkPopup).visibleIdx() []int`.
- Produces: `bookmarkPopup.mode dispMode` and `bookmarkPopup.hscroll int` (consumed by Task 2's key handler).

- [ ] **Step 1: Write the failing viewport test**

Add to `internal/tui/bookmark_test.go`:

```go
func TestBookmarkPopupWindowsLongList(t *testing.T) {
	var items []model.Bookmark
	for i := 0; i < 30; i++ {
		items = append(items, model.Bookmark{
			ID: fmt.Sprintf("b%02d", i), State: model.StateUnstaged,
			Worktree: "/wt", Path: fmt.Sprintf("f%02d.go", i),
		})
	}
	m := bmPopupModel(items...)
	m.width, m.height = 80, 30
	m.bookmarkPopup.sel = 29 // selection at the bottom
	out := m.renderBookmarkPopup()
	if !strings.Contains(out, "f29.go") {
		t.Fatalf("selected (bottom) row must be visible:\n%s", out)
	}
	if strings.Contains(out, "f00.go") {
		t.Fatalf("top row must scroll out of the capped viewport:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Fatalf("line wider than terminal (%d > %d): %q", w, m.width, line)
		}
	}
}
```

Add `"fmt"` and `"github.com/charmbracelet/lipgloss"` to the test file's imports if absent.

- [ ] **Step 2: Run it to confirm it fails**

Run: `go test ./internal/tui/ -run TestBookmarkPopupWindowsLongList -v`
Expected: FAIL — the old render lists every row, so `f00.go` is present (and there is no viewport cap).

- [ ] **Step 3: Add the state fields**

In `internal/tui/bookmark_popup.go`, extend the struct:

```go
type bookmarkPopup struct {
	items     []model.Bookmark
	rows      []string // display strings, parallel to items
	sel       int
	filter    string
	filtering bool     // true while `/` filter sub-mode captures runes
	markID    string   // first mark for a two-bookmark compare ("" = none)
	mode      dispMode // text display mode; z cycles (cutoff default)
	hscroll   int      // modeScroll horizontal offset
}
```

- [ ] **Step 4: Rewrite `renderBookmarkPopup` to use `renderWindow`**

Replace the whole `renderBookmarkPopup` body with:

```go
func (m Model) renderBookmarkPopup() string {
	p := m.bookmarkPopup
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	textW := popupTextWidth(inner)

	header := "Bookmarks"
	if p.filtering {
		header += "  /" + p.filter + "█"
	} else if p.filter != "" {
		header += "  /" + p.filter
	}

	vis := p.visibleIdx()
	var bodyLines []string
	if len(vis) == 0 {
		bodyLines = []string{padRight("  (none)", textW)}
	} else {
		wr := make([]winRow, len(vis))
		for n, i := range vis {
			prefix := "  "
			var st lipgloss.Style
			if n == p.sel {
				prefix, st = "> ", selectedRow
			}
			mark := " "
			if p.items[i].ID == p.markID {
				mark = "•"
			}
			wr[n] = winRow{text: prefix + mark + " " + p.rows[i], style: st}
		}
		h := len(vis)
		if h > 12 {
			h = 12
		}
		bodyLines = renderWindow(wr, winOpts{w: textW, h: h, mode: p.mode, anchor: p.sel, hscroll: p.hscroll})
	}

	parts := []string{header, ""}
	parts = append(parts, bodyLines...)
	parts = append(parts, "", "[enter] jump  [p] paste  [m] mark/compare  [x] remove  [/] filter  [z] mode  [esc] close")
	return popupBox(inner, strings.Join(parts, "\n"))
}
```

Add `"github.com/charmbracelet/lipgloss"` to the file imports (the struct field is `lipgloss.Style`).

- [ ] **Step 5: Run the viewport test + the existing popup tests**

Run: `go test ./internal/tui/ -run 'TestBookmarkPopup|TestBookmark' -v`
Expected: PASS — including the unchanged `TestBookmarkPopupOpenAndRenderFullPath` (still contains `a/b.go`).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/bookmark_popup.go internal/tui/bookmark_test.go
git commit -m "feat(tui): window the bookmark popup list via renderWindow

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 2: Display-mode + pan keys (`z`, `shift+←/→`)

Add the mode-cycle and horizontal-pan keys to the bookmark popup, in navigation mode only, taking precedence over the navigation key switch — mirroring `repo_popup.go` lines 63-80.

**Files:**
- Modify: `internal/tui/bookmark_popup.go` (`updateBookmarkPopupKey` ~lines 143-191)
- Test: `internal/tui/bookmark_test.go`

**Interfaces:**
- Consumes: `bookmarkPopup.mode`/`.hscroll` (from Task 1); `(dispMode).next()`; `m.hscrollStep() int`; `modeScroll`, `modeWrap`, `modeCutoff`.
- Produces: nothing new.

- [ ] **Step 1: Write the failing key tests**

Add to `internal/tui/bookmark_test.go`:

```go
func TestBookmarkPopupZCyclesMode(t *testing.T) {
	m := bmPopupModel(model.Bookmark{ID: "a", State: model.StateUnstaged, Worktree: "/wt", Path: "a.go"})
	m.bookmarkPopup.hscroll = 5
	mm, _ := m.updateBookmarkPopupKey(keyMsg("z"))
	m = mm.(Model)
	if m.bookmarkPopup.mode != modeWrap {
		t.Fatalf("z should cycle cutoff→wrap, got %v", m.bookmarkPopup.mode)
	}
	if m.bookmarkPopup.hscroll != 0 {
		t.Fatalf("z should reset hscroll, got %d", m.bookmarkPopup.hscroll)
	}
	mm, _ = m.updateBookmarkPopupKey(keyMsg("z"))
	m = mm.(Model)
	if m.bookmarkPopup.mode != modeScroll {
		t.Fatalf("second z should reach scroll, got %v", m.bookmarkPopup.mode)
	}
}

func TestBookmarkPopupPanOnlyInScroll(t *testing.T) {
	m := bmPopupModel(model.Bookmark{ID: "a", State: model.StateUnstaged, Worktree: "/wt", Path: "a.go"})
	// cutoff (default): shift+right is a no-op
	mm, _ := m.updateBookmarkPopupKey(keyMsg("shift+right"))
	m = mm.(Model)
	if m.bookmarkPopup.hscroll != 0 {
		t.Fatalf("shift+right in cutoff must not pan, got %d", m.bookmarkPopup.hscroll)
	}
	// scroll mode: shift+right pans by one step, shift+left clamps at 0
	m.bookmarkPopup.mode = modeScroll
	mm, _ = m.updateBookmarkPopupKey(keyMsg("shift+right"))
	m = mm.(Model)
	if m.bookmarkPopup.hscroll != m.hscrollStep() {
		t.Fatalf("shift+right in scroll → hscroll=%d, want %d", m.bookmarkPopup.hscroll, m.hscrollStep())
	}
	mm, _ = m.updateBookmarkPopupKey(keyMsg("shift+left"))
	m = mm.(Model)
	if m.bookmarkPopup.hscroll != 0 {
		t.Fatalf("shift+left should clamp to 0, got %d", m.bookmarkPopup.hscroll)
	}
}

func TestBookmarkPopupZTypesWhileFiltering(t *testing.T) {
	m := bmPopupModel(model.Bookmark{ID: "a", State: model.StateUnstaged, Worktree: "/wt", Path: "zebra.go"})
	mm, _ := m.updateBookmarkPopupKey(keyMsg("/")) // enter filter mode
	m = mm.(Model)
	mm, _ = m.updateBookmarkPopupKey(keyMsg("z")) // a query char, not a mode cycle
	m = mm.(Model)
	if m.bookmarkPopup.mode != modeCutoff {
		t.Fatalf("z while filtering must not cycle mode, got %v", m.bookmarkPopup.mode)
	}
	if m.bookmarkPopup.filter != "z" {
		t.Fatalf("z while filtering should type, filter=%q", m.bookmarkPopup.filter)
	}
}
```

- [ ] **Step 2: Run them to confirm they fail**

Run: `go test ./internal/tui/ -run 'TestBookmarkPopupZCyclesMode|TestBookmarkPopupPanOnlyInScroll|TestBookmarkPopupZTypesWhileFiltering' -v`
Expected: FAIL — `z`/`shift+right` currently fall through the navigation switch and do nothing (mode stays `modeCutoff`, hscroll stays 0); `TestBookmarkPopupZTypesWhileFiltering` already passes the filtering branch but is harmless to keep.

- [ ] **Step 3: Add the mode/pan switch in navigation mode**

In `updateBookmarkPopupKey`, the `p.filtering` branch is unchanged. Immediately AFTER the `if p.filtering { ... }` block and BEFORE `switch msg.Type {`, insert (mirrors `repo_popup.go`):

```go
	// Display-mode + pan keys take precedence over the navigation switch and
	// only act in navigation mode (while filtering they are query characters).
	switch msg.String() {
	case "z":
		p.mode = p.mode.next()
		p.hscroll = 0
		return m, nil
	case "shift+left":
		if p.mode == modeScroll && p.hscroll > 0 {
			if p.hscroll -= m.hscrollStep(); p.hscroll < 0 {
				p.hscroll = 0
			}
		}
		return m, nil
	case "shift+right":
		if p.mode == modeScroll {
			p.hscroll += m.hscrollStep()
		}
		return m, nil
	}
```

(The existing `case tea.KeyRunes: switch msg.String() { ... }` no longer needs a `z` case — it never had one. Leave `j`/`k`/`x`/`p`/`m`/`/` as they are.)

- [ ] **Step 4: Run the key tests + full tui package**

Run: `go test ./internal/tui/ -run 'TestBookmark' -v && go test ./internal/tui/`
Expected: PASS — new key tests green, all existing bookmark tests still green.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/bookmark_popup.go internal/tui/bookmark_test.go
git commit -m "feat(tui): bookmark popup z mode-cycle + shift pan keys

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 3: Skill doc note + changelog + full gate

Document the "list popups use `renderWindow`" rule so future popups don't repeat the gap, record the change, and run the race gate.

**Files:**
- Modify: `.claude/skills/adding-tui-windows/SKILL.md` (Popup checklist section, after the table ~line 55)
- Modify: `CHANGELOG.md` (top "Unreleased"/latest section)

- [ ] **Step 1: Add the List-popups note to the skill**

In `.claude/skills/adding-tui-windows/SKILL.md`, immediately after the Popup checklist table (after the line for item 5, before `## Tests`), insert:

```markdown
### List popups (scrollable rows) — use `renderWindow`, not `modalStyle.Width`

Item 3 above (`modalStyle.Width(inner)`) is the rule only for **single-line /
fixed input or confirm** popups. A popup that shows a **scrollable list of
rows** (repo switcher, bookmark switcher, action menu, …) MUST render through
the shared `renderWindow` primitive so it inherits cutoff/wrap/scroll for free.
Exemplar: `internal/tui/repo_popup.go`. Checklist:

- Give the state struct `mode dispMode` and `hscroll int`.
- Build one `winRow{text, style}` per visible row; **fold any cursor/mark
  prefix into `text`** (the primitive adds none) and set `style = selectedRow`
  on the selected row.
- Cap the visible height and call `renderWindow(rows, winOpts{w: textW, h: h,
  mode: p.mode, anchor: p.sel, hscroll: p.hscroll})`; emit via `popupBox(inner,
  …)`, not `modalStyle.Width(...)`.
- In the key handler, before the navigation switch, handle `z`
  (`p.mode = p.mode.next()`, reset `hscroll`) and `shift+←/→`
  (pan by `m.hscrollStep()`, only in `modeScroll`). Gate these to navigation
  mode if the popup has a `/` filter sub-mode, so `z` stays a query character
  while typing.
- Add `[z] mode` to the popup's footer hint line.
```

- [ ] **Step 2: Add the changelog entry**

In `CHANGELOG.md`, add under the latest/Unreleased section's TUI bullets:

```markdown
- Bookmark switcher popup (`g`) now renders through the windowed list
  primitive: `z` cycles cutoff/wrap/scroll, `shift+←/→` pans in scroll mode,
  and long rows no longer wrap uncontrolled (matches the repo switcher).
```

- [ ] **Step 3: Run the full race gate**

Run: `./test.sh race`
Expected: vet + gofmt clean, all unit tests pass (incl. the new bookmark tests), e2e green.

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md .claude/skills/adding-tui-windows/SKILL.md
git commit -m "docs(tui): list-popup windowing skill note + changelog

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

## Self-Review

**Spec coverage:**
- Struct `mode`/`hscroll` fields → Task 1 Step 3. ✓
- `renderBookmarkPopup` via `renderWindow`/`popupBox`, folded indicators, `(none)` line, height cap 12, `[z] mode` footer, `/filter` header caret → Task 1 Step 4. ✓
- `z` / `shift+←/→` in navigation mode only, precedence → Task 2 Step 3. ✓
- Paste popup untouched → not modified by any task. ✓
- Skill update → Task 3 Step 1. ✓
- Tests (z cycles, pan only in scroll, z-while-filtering types, long-row no-overflow / viewport) → Task 1 Step 1 + Task 2 Step 1. ✓
- CHANGELOG, no README/CLAUDE/agentskill → Task 3 Steps 2-4. ✓

**Placeholder scan:** none — every code step shows full code.

**Type consistency:** `mode dispMode`, `hscroll int`, `winRow{text,style}`, `winOpts{w,h,mode,anchor,hscroll}`, `p.mode.next()`, `m.hscrollStep()`, `selectedRow`, `popupBox(inner, …)`, `popupTextWidth(inner)` — all match the primitives in `window.go`/`view.go`/`model.go` and the repo_popup usage. ✓
