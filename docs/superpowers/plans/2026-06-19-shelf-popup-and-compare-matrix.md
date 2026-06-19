# Shelf Popup + Compare Matrix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Shelf left-column tab with a global `G` quick-switcher popup (mirroring the bookmark `g` popup), and add a cross-store compare matrix (files-menu "Compare against shelf" + `c` cross-compare between the two popups).

**Architecture:** A new `shelfPopup` mirrors `bookmarkPopup` and drives the existing shelf action cores (refactored to be entry-parameterized). The `panelShelf` tab and its wiring are removed. Compare reuses the existing `compareRef`/`pendingCompare` machinery, generalized with a `target` so either popup can open in compare mode; right-side bytes resolve per popup (shelf via `ResolveBytes(SourceShelf)`, bookmark via `BookmarkBytes`).

**Tech Stack:** Go 1.26, Bubble Tea, lipgloss. No engine/domain/CLI changes.

## Global Constraints

- Work in the existing worktree on branch `worktree-shelf-popup-compare` (off `main` tip `c1063b1`). Worktree-relative paths only.
- `internal/tui` must not import `internal/git`/`internal/shelf`/`internal/bookmark` (archtest). Untouched here.
- Open key for the shelf popup is **`G`** (verified free).
- Diff handoff from any picker popup MUST close the popup AND `clearStack()` (so the full-screen diff isn't hidden under a history/blame surface) — use the shared `openPickerDiff`.
- Every new keybinding lands in both `help.go` and the footer. Run `./test.sh race` before done. Commit trailers as in the repo.

---

### Task 1: Refactor shelf action cores to be entry-parameterized

Pure refactor (no behavior change, tab still works): make the compare/restore/remove cores take an explicit entry/ID so both the tab (now) and the popup (Task 2) can call them. Also introduce the shared `openPickerDiff`.

**Files:**
- Modify: `internal/tui/shelf_actions.go`
- Modify: `internal/tui/bookmark_popup.go` (rename `openBookmarkDiff` → `openPickerDiff`)
- Modify: `internal/tui/bookmark_compare.go`, `internal/tui/bookmark.go` (callers of the rename, if any beyond bookmark_popup.go)
- Test: `internal/tui/shelf_test.go` (existing tests must stay green)

**Interfaces:**
- Produces: `openPickerDiff(v *diffView, tag string, load tea.Cmd) (Model, tea.Cmd)`; `openShelfCompareEntry(e model.ShelfEntry) (Model, tea.Cmd)`; `openShelfCompareTwoEntries(a, b model.ShelfEntry) (Model, tea.Cmd)`; restore opener `openShelfRestore(e model.ShelfEntry) (Model, tea.Cmd)`.

- [ ] **Step 1: Rename `openBookmarkDiff` → `openPickerDiff`**

In `internal/tui/bookmark_popup.go`, rename the method and update its doc + all call sites (`openBookmarkCompareTwo`, `bookmarkJump`, `openCompareFocusedVsBookmark` in bookmark_compare.go):

```go
// openPickerDiff hands off from a picker popup (bookmark or shelf) to a
// full-screen diff: it closes BOTH popups and clears the surface stack, so the
// diff view owns the screen even when the popup was opened over a history/blame
// surface.
func (m Model) openPickerDiff(v *diffView, tag string, load tea.Cmd) (Model, tea.Cmd) {
	m.bookmarkPopup = nil
	m.shelfPopup = nil
	m = m.clearStack()
	m.diffView = v
	m.diffTag = tag
	return m, load
}
```

(`m.shelfPopup` is declared in Task 2; for Task 1 add the field now — see Task 2 Step 1 — or reference it only after Task 2. To keep Task 1 self-contained, add the `shelfPopup *shelfPopup` field to Model in this step too, defined as an empty struct placeholder; Task 2 fills it. Simpler: do the Model field + struct stub here.)

Add to `internal/tui/model.go` Model struct (near `bookmarkPopup`):
```go
	shelfPopup *shelfPopup // Shelf quick-switcher (G); nil = closed
```
And a stub in a new file `internal/tui/shelf_popup.go`:
```go
package tui

import "github.com/gigagit/gg/internal/model"

// shelfPopup is the centered shelf quick-switcher (G): a type-to-filter list of
// the repo's shelved files. Mirrors bookmarkPopup.
type shelfPopup struct {
	items     []model.ShelfEntry
	rows      []string // e.Origin.Display(), parallel to items
	sel       int
	filter    string
	filtering bool
	markID    string
	mode      dispMode
	hscroll   int
	compareRef   *model.FileRef
	compareLabel string
}
```

- [ ] **Step 2: Verify existing tests still build/pass after the rename**

Run: `go build ./... && go test ./internal/tui/ 2>&1 | tail -3`
Expected: PASS (rename only; `shelfPopup` stub compiles, field unused yet — Go allows unused struct fields).

- [ ] **Step 3: Entry-parameterize the shelf cores**

In `internal/tui/shelf_actions.go`, split the tab-bound openers into entry cores and thin tab wrappers. Replace `openShelfCompare` and `openShelfCompareTwo` bodies:

```go
// openShelfCompare diffs the tab-selected entry vs the working tree.
func (m Model) openShelfCompare() (Model, tea.Cmd) {
	e, ok := m.selectedShelfEntry()
	if !ok {
		return m, nil
	}
	return m.openShelfCompareEntry(e)
}

// openShelfCompareEntry diffs entry e (old) against the current working-tree
// file at its origin path (new). Routed through openPickerDiff so it works when
// invoked from the popup over a stacked surface.
func (m Model) openShelfCompareEntry(e model.ShelfEntry) (Model, tea.Cmd) {
	width, _ := m.overlayDims()
	v := &diffView{title: e.Origin.Path, context: "shelf #" + shortShelf(e) + " → working tree", rev: "", loading: true, partial: m.diffPartial, long: m.diffLong, width: width}
	return m.openPickerDiff(v, "shelf:"+e.ID, m.loadShelfCompareCmd(e))
}

// openShelfCompareTwo diffs two entries by id (pair-op mark path).
func (m Model) openShelfCompareTwo(markedID, selectedID string) (Model, tea.Cmd) {
	a, okA := m.shelfEntryByID(markedID)
	b, okB := m.shelfEntryByID(selectedID)
	if !okA || !okB {
		return m, nil
	}
	m.mark = nil // consume the mark
	return m.openShelfCompareTwoEntries(a, b)
}

// openShelfCompareTwoEntries diffs entries a (old) and b (new).
func (m Model) openShelfCompareTwoEntries(a, b model.ShelfEntry) (Model, tea.Cmd) {
	title := a.Origin.Path
	if a.Origin.Path != b.Origin.Path {
		title = a.Origin.Path + " ↔ " + b.Origin.Path
	}
	ctx := "shelf #" + shortShelf(a) + " → shelf #" + shortShelf(b)
	width, _ := m.overlayDims()
	v := &diffView{title: title, context: ctx, rev: "", loading: true, partial: m.diffPartial, long: m.diffLong, width: width}
	return m.openPickerDiff(v, "shelf2:"+a.ID+":"+b.ID, m.loadShelfCompareTwoCmd(a, b, title, ctx))
}
```

Add a restore opener (used by both tab menu and popup):
```go
// openShelfRestore opens the mandatory-dest restore popup for entry e.
func (m Model) openShelfRestore(e model.ShelfEntry) (Model, tea.Cmd) {
	m.shelfRestorePopup = &shelfRestorePopup{entryID: e.ID, origin: e.Origin.Path}
	return m, nil
}
```

Update `shelfTabRows`' restore handler to call `openShelfRestore(e)`:
```go
			run: func(m Model) (tea.Model, tea.Cmd) {
				return m.openShelfRestore(e)
			},
```

- [ ] **Step 4: Run tests**

Run: `go build ./... && go test ./internal/tui/ 2>&1 | tail -3`
Expected: PASS — existing shelf tab tests (`TestShelfCompareOpensDiff`, `TestShelfCompareTwoOpensDiff`, `TestShelfRestorePopupRequiresDest`, `TestShelfTabMenuRows`) still green.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/shelf_actions.go internal/tui/bookmark_popup.go internal/tui/bookmark_compare.go internal/tui/model.go internal/tui/shelf_popup.go
git commit -m "refactor(tui): entry-parameterize shelf cores + shared openPickerDiff

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 2: The `shelfPopup` (G) — list, keys, render, open wiring

Fill the popup: render, keys (enter=jump, p=restore, m=mark/compare-two, x=remove, /, z), the global `G` open key in every surface, and `shelfLoadedMsg` building the popup. The tab still exists (coexists); removed in Task 3.

**Files:**
- Modify: `internal/tui/shelf_popup.go` (fill the stub)
- Modify: `internal/tui/model.go` (`shelfLoadedMsg` builds popup; `G` key; popup key routing + render hoist; restore-popup routing already exists)
- Modify: `internal/tui/{blame_view,diff_view,files_view,history_view,stash_view}.go` (add `G` case mirroring `g`)
- Modify: `internal/tui/view.go` (render the popup overlay)
- Test: `internal/tui/shelf_popup_test.go` (create)

**Interfaces:**
- Consumes: `openShelfCompareEntry`, `openShelfCompareTwoEntries`, `openShelfRestore`, `shelfRemoveCmd`, `loadShelfCmd`, `openPickerDiff`, `renderWindow`/`winOpts`/`popupBox`/`popupInnerWidth`/`popupTextWidth`/`padRight`/`selectedRow`.
- Produces: `openShelfSwitcher() (Model, tea.Cmd)`; `(*shelfPopup).visibleIdx()`; `popupSelectedShelfEntry() (model.ShelfEntry, bool)`; `updateShelfPopupKey`; `renderShelfPopup`.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/shelf_popup_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func shelfPopModel(entries ...model.ShelfEntry) Model {
	m := footerModel()
	m.width, m.height = 100, 30
	m.shelfEntries = entries
	m.shelfPopup = newShelfPopup(entries)
	return m
}

func shEntry(id, path string) model.ShelfEntry {
	return model.ShelfEntry{ID: id, Origin: model.FileAddress{State: model.StateUnstaged, Worktree: "/wt", Path: path}, SHA: id + "0000"}
}

func TestShelfPopupRendersOrigin(t *testing.T) {
	m := shelfPopModel(shEntry("a", "dir/x.go"))
	out := m.renderShelfPopup()
	if !strings.Contains(out, "dir/x.go") || !strings.Contains(out, "Shelf") {
		t.Fatalf("popup missing content:\n%s", out)
	}
}

func TestShelfPopupZCyclesMode(t *testing.T) {
	m := shelfPopModel(shEntry("a", "x.go"))
	mm, _ := m.updateShelfPopupKey(keyMsg("z"))
	m = mm.(Model)
	if m.shelfPopup.mode != modeWrap {
		t.Fatalf("z should cycle to wrap, got %v", m.shelfPopup.mode)
	}
}

func TestShelfPopupEnterJumps(t *testing.T) {
	m := shelfPopModel(shEntry("a", "x.go"))
	mm, cmd := m.updateShelfPopupKey(keyMsg("enter"))
	m = mm.(Model)
	if m.shelfPopup != nil {
		t.Fatalf("enter should close the popup (jump to diff)")
	}
	if m.diffView == nil || m.diffTag != "shelf:a" {
		t.Fatalf("enter should open the shelf-vs-worktree diff, tag=%q", m.diffTag)
	}
	_ = cmd
}

func TestShelfPopupRemoveConfirms(t *testing.T) {
	m := shelfPopModel(shEntry("a", "x.go"))
	mm, _ := m.updateShelfPopupKey(keyMsg("x"))
	m = mm.(Model)
	if m.modal == nil {
		t.Fatalf("x should open a remove-confirm modal")
	}
}

func TestShelfPopupRestoreOpensDest(t *testing.T) {
	m := shelfPopModel(shEntry("a", "x.go"))
	mm, _ := m.updateShelfPopupKey(keyMsg("p"))
	m = mm.(Model)
	if m.shelfRestorePopup == nil || m.shelfRestorePopup.entryID != "a" {
		t.Fatalf("p should open the restore-destination popup")
	}
}

func TestShelfPopupMarkThenCompare(t *testing.T) {
	m := shelfPopModel(shEntry("a", "a.go"), shEntry("b", "b.go"))
	mm, _ := m.updateShelfPopupKey(keyMsg("m"))
	m = mm.(Model)
	if m.shelfPopup == nil || m.shelfPopup.markID != "a" {
		t.Fatalf("first m should mark entry a")
	}
	m.shelfPopup.sel = 1
	mm, _ = m.updateShelfPopupKey(keyMsg("m"))
	m = mm.(Model)
	if m.diffView == nil || m.diffTag != "shelf2:a:b" {
		t.Fatalf("second m should open the two-entry diff, tag=%q", m.diffTag)
	}
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `go test ./internal/tui/ -run TestShelfPopup 2>&1 | tail -8`
Expected: FAIL — build error (`newShelfPopup`, `renderShelfPopup`, `updateShelfPopupKey` undefined).

- [ ] **Step 3: Implement the popup**

Fill `internal/tui/shelf_popup.go` (keep the struct from Task 1):

```go
import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/model"
)

// openShelfSwitcher opens the global shelf quick-switcher (G). Wired into every
// navigable surface like g; render+routing hoisted above content surfaces.
func (m Model) openShelfSwitcher() (Model, tea.Cmd) {
	if m.opsIdle() && m.shelfPopup == nil {
		return m, m.loadShelfCmd()
	}
	return m, nil
}

func newShelfPopup(items []model.ShelfEntry) *shelfPopup {
	p := &shelfPopup{items: items}
	for _, e := range items {
		p.rows = append(p.rows, e.Origin.Display())
	}
	return p
}

func (p *shelfPopup) visibleIdx() []int {
	var idx []int
	q := strings.ToLower(p.filter)
	for i, row := range p.rows {
		if q == "" || strings.Contains(strings.ToLower(row), q) {
			idx = append(idx, i)
		}
	}
	return idx
}

func (m Model) popupSelectedShelfEntry() (model.ShelfEntry, bool) {
	p := m.shelfPopup
	vis := p.visibleIdx()
	if p.sel < 0 || p.sel >= len(vis) {
		return model.ShelfEntry{}, false
	}
	return p.items[vis[p.sel]], true
}

func (m Model) renderShelfPopup() string {
	p := m.shelfPopup
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	textW := popupTextWidth(inner)

	header := "Shelf"
	if p.compareRef != nil {
		header = "Compare " + p.compareRef.Path + " against:"
	}
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
	parts = append(parts, "", "[enter] diff  [p] restore  [m] mark/compare  [x] remove  [c] vs bookmark  [/] filter  [z] mode  [esc] close")
	return popupBox(inner, strings.Join(parts, "\n"))
}

func (m Model) shelfPopupMoveSel(d int) {
	p := m.shelfPopup
	if n := p.sel + d; n >= 0 && n < len(p.visibleIdx()) {
		p.sel = n
	}
}

func (m Model) updateShelfPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	p := m.shelfPopup
	if p.filtering {
		switch msg.Type {
		case tea.KeyEsc:
			p.filtering, p.filter, p.sel = false, "", 0
		case tea.KeyEnter:
			p.filtering = false
		case tea.KeyBackspace, tea.KeyCtrlH:
			if r := []rune(p.filter); len(r) > 0 {
				p.filter = string(r[:len(r)-1])
				p.sel = 0
			}
		case tea.KeyRunes:
			p.filter += string(msg.Runes)
			p.sel = 0
		}
		return m, nil
	}
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
	switch msg.Type {
	case tea.KeyEsc:
		m.shelfPopup = nil
	case tea.KeyEnter:
		e, ok := m.popupSelectedShelfEntry()
		if !ok {
			return m, nil
		}
		if p.compareRef != nil {
			return m.openCompareFocusedVsShelf(*p.compareRef, p.compareLabel, e)
		}
		return m.openShelfCompareEntry(e)
	case tea.KeyUp:
		m.shelfPopupMoveSel(-1)
	case tea.KeyDown:
		m.shelfPopupMoveSel(1)
	case tea.KeyRunes:
		switch msg.String() {
		case "/":
			p.filtering = true
		case "k":
			m.shelfPopupMoveSel(-1)
		case "j":
			m.shelfPopupMoveSel(1)
		case "x":
			if p.compareRef != nil {
				return m, nil
			}
			return m.shelfPopupRemovePrompt()
		case "p":
			if p.compareRef != nil {
				return m, nil
			}
			e, ok := m.popupSelectedShelfEntry()
			if !ok {
				return m, nil
			}
			m.shelfPopup = nil
			return m.openShelfRestore(e)
		case "m":
			if p.compareRef != nil {
				return m, nil
			}
			return m.shelfPopupMark()
		case "c":
			if p.compareRef != nil {
				return m, nil
			}
			return m.shelfCompareAgainstBookmark() // Task 4
		}
	}
	return m, nil
}

func (m Model) shelfPopupRemovePrompt() (tea.Model, tea.Cmd) {
	e, ok := m.popupSelectedShelfEntry()
	if !ok {
		return m, nil
	}
	m.shelfPopup = nil
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "shelf-remove",
			Prompt:  "Remove " + e.Origin.Path + " from the shelf? (the frozen copy is destroyed)",
			Options: []string{"Remove", "Cancel"},
		},
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt == "Remove" {
				return m, m.shelfRemoveCmd(e.ID)
			}
			return m, nil
		},
	}
	return m, nil
}

func (m Model) shelfPopupMark() (tea.Model, tea.Cmd) {
	e, ok := m.popupSelectedShelfEntry()
	if !ok {
		return m, nil
	}
	p := m.shelfPopup
	if p.markID == "" || p.markID == e.ID {
		if p.markID == e.ID {
			p.markID = ""
		} else {
			p.markID = e.ID
		}
		return m, nil
	}
	a, okA := m.shelfEntryByID(p.markID)
	b, okB := m.shelfEntryByID(e.ID)
	if !okA || !okB {
		return m, nil
	}
	return m.openShelfCompareTwoEntries(a, b)
}
```

Note `shelfCompareAgainstBookmark` and `openCompareFocusedVsShelf` are added in Task 4; to keep Task 2 compiling, add minimal stubs now and flesh out in Task 4:
```go
// stubs filled in Task 4
func (m Model) shelfCompareAgainstBookmark() (tea.Model, tea.Cmd) { return m, nil }
func (m Model) openCompareFocusedVsShelf(ref model.FileRef, label string, e model.ShelfEntry) (Model, tea.Cmd) {
	return m.openShelfCompareEntry(e) // placeholder; Task 4 replaces with focused-vs-shelf
}
```

- [ ] **Step 4: Wire the `G` open key + popup routing + render**

In `internal/tui/model.go`:
- The top-level key switch — add after the `g` case:
```go
		case "G": // open the shelf quick-switcher (global; see openShelfSwitcher)
			return m.openShelfSwitcher()
```
- `shelfLoadedMsg` handler (currently sets `m.shelfEntries` + clamps `m.sel[panelShelf]`): also build the popup. Replace the clamp block with:
```go
		m.shelfEntries = msg.entries
		if m.shelfPopup != nil || m.opsIdle() {
			m.shelfPopup = newShelfPopup(msg.entries)
			if pc := m.pendingCompare; pc != nil && pc.target == compareShelf { // Task 4
				m.shelfPopup.compareRef = &pc.ref
				m.shelfPopup.compareLabel = pc.label
				m.pendingCompare = nil
			}
		}
		return m, nil
```
(For Task 2, before Task 4 adds `target`, use the simpler `m.shelfPopup = newShelfPopup(msg.entries)` and add the pendingCompare arm in Task 4. Keep the `m.sel[panelShelf]` clamp until Task 3 removes the panel.)
- Popup key routing: in the `tea.KeyMsg` dispatch, add a branch next to the bookmark popup branch:
```go
	if m.shelfPopup != nil {
		return m.updateShelfPopupKey(msg)
	}
```
Place it at the same precedence as `if m.bookmarkPopup != nil { return m.updateBookmarkPopupKey(msg) }` (after modal, before content surfaces) — find that line and add this immediately after.

In `internal/tui/view.go` `render()`: add the overlay next to the bookmark popup render (mirror line 189-style):
```go
	if m.shelfPopup != nil {
		return overlayCenter(bg, m.renderShelfPopup(), w, h)
	}
```
Place it at the same precedence the bookmark popup render uses (hoisted above content surfaces).

- [ ] **Step 5: Add `G` to the five content surfaces**

In each of `blame_view.go`, `diff_view.go`, `files_view.go`, `history_view.go`, `stash_view.go`, immediately after the `case "g": return m.openBookmarkSwitcher()` line, add:
```go
	case "G": // global shelf quick-switcher
		return m.openShelfSwitcher()
```

- [ ] **Step 6: Run tests**

Run: `go build ./... && go test ./internal/tui/ -run TestShelfPopup -v 2>&1 | tail -12 && go test ./internal/tui/ 2>&1 | tail -3`
Expected: PASS — popup tests green; existing tests green (tab still present).

- [ ] **Step 7: Commit**

```bash
git add internal/tui/shelf_popup.go internal/tui/shelf_popup_test.go internal/tui/model.go internal/tui/view.go internal/tui/blame_view.go internal/tui/diff_view.go internal/tui/files_view.go internal/tui/history_view.go internal/tui/stash_view.go
git commit -m "feat(tui): shelf quick-switcher popup (G), coexisting with the tab

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 3: Remove the `panelShelf` tab

Now the popup covers every shelf action; remove the tab and all its wiring.

**Files:**
- Modify: `internal/tui/model.go` (enum, `leftTabs`, clamp, enter-on-shelf, ctrl-arrow lazy-load), `mark.go`, `footer.go`, `view.go`, `viewstate.go`, `bookmark.go` (focusedBookmark panelShelf arm), `shelf_actions.go` (`selectedShelfEntry`/`shelfTabRows`), `action_menu.go` (`shelfTabRows` injection)
- Test: `internal/tui/shelf_test.go` (drop/convert tab-only tests)

- [ ] **Step 1: Remove the enum + leftTabs entry**

`internal/tui/model.go`: delete `panelShelf` from the `const` block and from `leftTabs`:
```go
var leftTabs = []panel{panelBranches, panelRemotes, panelWorktrees}
```

- [ ] **Step 2: Remove tab dispatch + lazy-load + clamp**

`internal/tui/model.go`:
- Delete the `if m.focus == panelShelf && m.canShelfCompare() { return m.openShelfCompare() }` block in the `enter` case.
- Delete the `if m.activeLeftTab == panelShelf { return m, m.loadShelfCmd() }` block in the ctrl-arrow case.
- Delete the `if m.sel[panelShelf] >= len(m.shelfEntries) { m.sel[panelShelf] = 0 }` clamp in `shelfLoadedMsg` (the popup owns selection now).

- [ ] **Step 3: Remove panelShelf from the remaining sites**

- `mark.go`: delete the entire `if p == panelShelf { return []pairOp{...} }` block.
- `footer.go`: delete the four `shelf-*` bindings (`shelf-compare`, `shelf-mark`, `shelf-unmark`, `shelf-pair`).
- `view.go`: in the tab bar, drop the `mark(panelShelf, "Shelf", "S")` term (and the trailing `+ " "` on the Worktrees line):
```go
	return mark(panelBranches, "Branches", "B") + " " +
		mark(panelRemotes, "Remotes", "R") + " " +
		mark(panelWorktrees, "Worktrees", "W")
```
- `viewstate.go`: delete the `case panelShelf:` arm in `listFor`.
- `bookmark.go`: delete the `case panelShelf:` arm in `focusedBookmark` (TUI shelf-entry bookmarking dropped with the tab; the cross-compare covers comparing a shelf entry. `model.StateShelf` bookmarks remain resolvable/displayable, just not created from a removed tab).

- [ ] **Step 4: Retarget the remaining shelf_actions helpers**

`shelf_actions.go`:
- `selectedShelfEntry` is now only the tab-menu path which is gone; `shelfTabRows` is removed (its actions live in the popup). Delete `shelfTabRows`, `canShelfRestore`, `canShelfRemove`, `canShelfCompare`, and `selectedShelfEntry`.
- `action_menu.go`: remove the `m.shelfTabRows()` injection line in `availableActions`.

- [ ] **Step 5: Fix tests for the removed tab**

In `internal/tui/shelf_test.go`: delete tab-only tests that reference `panelShelf`/`shelfTabModel`/`selectedShelfEntry`/`shelfTabRows` (`TestShelfTabMenuRows`, `TestShelfCompareOpensDiff` (tab enter), `TestShelfCompareTwoOpensDiff` (tab mark path — now covered by `TestShelfPopupMarkThenCompare`), `TestShelfPairOpCompare`, `TestShelfMarkThenCompareOpensPicker`, `TestTabBarLabelIncludesShelf`, `shelfTabModel`). Keep `TestShelfRowsContent` only if `shelfRows` is still used by the popup — it is (the popup builds rows from `Origin.Display()`; `shelfRows` itself may now be unused: if so, delete `shelfRows` from `shelf.go` and that test). Keep `TestShelfRestorePopupRequiresDest` (restore popup unchanged). Keep `TestShelfAddCaptureFromBlame`, `TestAddToShelfRow*` (capture path unchanged).

After edits, search for leftover references:
```bash
grep -rn 'panelShelf\|shelfTabRows\|shelfTabModel\|selectedShelfEntry\|canShelfCompare' internal/tui
```
Expected: no matches (or only inside deleted-then-confirmed-gone files).

- [ ] **Step 6: Build, vet, test**

Run: `go build ./... 2>&1 | tail; go vet ./internal/tui/ 2>&1 | tail; go test ./internal/tui/ 2>&1 | tail -3`
Expected: PASS. Use the compiler output as the authoritative list of any missed `panelShelf` site and fix each.

- [ ] **Step 7: Commit**

```bash
git add -A internal/tui
git commit -m "feat(tui): remove the Shelf tab; the G popup is the only shelf surface

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 4: Compare matrix (Stage B)

Files-menu "Compare against shelf", and `c` cross-compare in both popups, on the generalized `pendingCompare`.

**Files:**
- Modify: `internal/tui/bookmark_compare.go` (add `comparePopupKind` + `target`)
- Modify: `internal/tui/shelf_popup.go` (flesh out `shelfCompareAgainstBookmark`, `openCompareFocusedVsShelf`)
- Modify: `internal/tui/bookmark_popup.go` (bookmark popup `c` → compare against shelf)
- Modify: `internal/tui/bookmark.go` (add `compareAgainstShelfRow`; factor `focusedCompareRef`)
- Modify: `internal/tui/model.go` (`bookmarksLoadedMsg` guards `target == compareBookmark`; `shelfLoadedMsg` arm for `compareShelf`)
- Modify: `internal/tui/action_menu.go` (inject `compareAgainstShelfRow`)
- Test: `internal/tui/shelf_popup_test.go`, `internal/tui/bookmark_test.go`

**Interfaces:**
- Produces: `comparePopupKind` (`compareBookmark`/`compareShelf`); `pendingCompare.target`; `openCompareFocusedVsShelf`; `compareAgainstShelfRow`.

- [ ] **Step 1: Write failing tests**

In `internal/tui/shelf_popup_test.go`:
```go
func TestComdareAgainstShelfMenuRow(t *testing.T) {
	m := filesMenuModel()
	m.currentWorktree = "/wt"
	r, ok := findRow(availableActions(m), "shelf-compare-against")
	if !ok {
		t.Fatalf("Compare against shelf missing on Files panel")
	}
	mm, _ := r.run(m)
	m = mm.(Model)
	if m.pendingCompare == nil || m.pendingCompare.target != compareShelf {
		t.Fatalf("running it should set a shelf-targeted pendingCompare")
	}
}

func TestShelfPopupCAgainstBookmark(t *testing.T) {
	m := shelfPopModel(shEntry("a", "x.go"))
	mm, cmd := m.updateShelfPopupKey(keyMsg("c"))
	m = mm.(Model)
	if m.shelfPopup != nil {
		t.Fatalf("c should close the shelf popup")
	}
	if m.pendingCompare == nil || m.pendingCompare.target != compareBookmark {
		t.Fatalf("c should set a bookmark-targeted pendingCompare")
	}
	if cmd == nil {
		t.Fatalf("c should load bookmarks")
	}
}

func TestShelfCompareModeEnterDiffs(t *testing.T) {
	m := shelfPopModel(shEntry("a", "x.go"))
	m.shelfPopup.compareRef = &model.FileRef{Source: model.SourceUnstaged, Path: "focused.go"}
	m.shelfPopup.compareLabel = "wt:wt / unstaged / focused.go"
	mm, _ := m.updateShelfPopupKey(keyMsg("enter"))
	m = mm.(Model)
	if m.diffView == nil || !strings.HasPrefix(m.diffTag, "cmpsh:") {
		t.Fatalf("enter in compare mode should diff focused vs shelf, tag=%q", m.diffTag)
	}
}
```

In `internal/tui/bookmark_test.go`:
```go
func TestBookmarkPopupCAgainstShelf(t *testing.T) {
	m := bmPopupModel(model.Bookmark{ID: "b1", State: model.StateUnstaged, Worktree: "/wt", Path: "a.go"})
	mm, cmd := m.updateBookmarkPopupKey(keyMsg("c"))
	m = mm.(Model)
	if m.bookmarkPopup != nil {
		t.Fatalf("c should close the bookmark popup")
	}
	if m.pendingCompare == nil || m.pendingCompare.target != compareShelf {
		t.Fatalf("c should set a shelf-targeted pendingCompare")
	}
	if cmd == nil {
		t.Fatalf("c should load the shelf")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/tui/ -run 'CAgainst|CompareMode|CompareAgainstShelf' 2>&1 | tail -8`
Expected: FAIL (undefined `compareShelf`/`compareBookmark`, row id missing, `c` no-ops).

- [ ] **Step 3: Generalize `pendingCompare`**

`internal/tui/bookmark_compare.go`:
```go
type comparePopupKind int

const (
	compareBookmark comparePopupKind = iota
	compareShelf
)

type pendingCompare struct {
	ref    model.FileRef
	label  string
	target comparePopupKind
}
```
Add `openCompareFocusedVsShelf` (mirror `openCompareFocusedVsBookmark`, right side = `ResolveBytes(FileRef{SourceShelf, e.ID})`):
```go
func (m Model) openCompareFocusedVsShelf(ref model.FileRef, label string, e model.ShelfEntry) (Model, tea.Cmd) {
	width, _ := m.overlayDims()
	right := model.FileRef{Source: model.SourceShelf, Locator: e.ID, Path: e.Origin.Path}
	v := &diffView{title: ref.Path + " ↔ " + e.Origin.Path, context: label + " → shelf #" + shortShelf(e), loading: true, partial: m.diffPartial, long: m.diffLong, width: width}
	return m.openPickerDiff(v, "cmpsh:"+ref.Path+":"+e.ID, m.loadCompareTwoRefsCmd(ref, right, label, "shelf #"+shortShelf(e), ref.Path+" ↔ "+e.Origin.Path, "cmpsh:"+ref.Path+":"+e.ID))
}

// loadCompareTwoRefsCmd resolves both sides via ResolveBytes (generic).
func (m Model) loadCompareTwoRefsCmd(left, right model.FileRef, leftLabel, rightLabel, title, tag string) tea.Cmd {
	svc := m.svc
	differ := m.diffDiffer()
	body := m.diffBodyRows()
	v := &diffView{title: title, context: leftLabel + " → " + rightLabel, partial: m.diffPartial, long: m.diffLong}
	v.width, _ = m.overlayDims()
	return func() tea.Msg {
		oldSrc := func(ctx context.Context) ([]byte, error) { return svc.ResolveBytes(ctx, left) }
		newSrc := func(ctx context.Context) ([]byte, error) { return svc.ResolveBytes(ctx, right) }
		out, err := differ.Diff(context.Background(), domain.Request{Key: "", Old: oldSrc, New: newSrc})
		if err != nil {
			v.err = err
			return diffMsg{tag: tag, view: v}
		}
		applyDiff(v, out, body)
		return diffMsg{tag: tag, view: v}
	}
}
```

- [ ] **Step 4: Replace the Task-2 stubs in `shelf_popup.go`**

```go
// shelfCompareAgainstBookmark: the highlighted entry becomes the left side,
// then the bookmark popup opens in compare mode.
func (m Model) shelfCompareAgainstBookmark() (tea.Model, tea.Cmd) {
	e, ok := m.popupSelectedShelfEntry()
	if !ok {
		return m, nil
	}
	ref := model.FileRef{Source: model.SourceShelf, Locator: e.ID, Path: e.Origin.Path}
	m.shelfPopup = nil
	m.pendingCompare = &pendingCompare{ref: ref, label: "shelf #" + shortShelf(e), target: compareBookmark}
	return m, m.loadBookmarksCmd()
}
```
Delete the `openCompareFocusedVsShelf` placeholder stub (the real one now lives in bookmark_compare.go).

- [ ] **Step 5: Bookmark popup `c` → compare against shelf**

In `internal/tui/bookmark_popup.go` `updateBookmarkPopupKey`, add a `c` case in the `tea.KeyRunes` switch (after `m`):
```go
		case "c":
			if p.compareRef != nil {
				return m, nil
			}
			b, ok := m.selectedBookmark()
			if !ok {
				return m, nil
			}
			m.bookmarkPopup = nil
			m.pendingCompare = &pendingCompare{ref: bookmarkToFileRef(b), label: bookmarkDisplay(b), target: compareShelf}
			return m, m.loadShelfCmd()
```
Update the bookmark popup footer hint to include `[c] vs shelf`.

- [ ] **Step 6: Files-menu "Compare against shelf" + DRY the focused ref**

In `internal/tui/bookmark.go`, factor the focused-ref helper and add the shelf row:
```go
func (m Model) focusedCompareRef() (model.FileRef, string, bool) {
	b, ok := m.focusedBookmark()
	if !ok {
		return model.FileRef{}, "", false
	}
	return bookmarkToFileRef(b), bookmarkDisplay(b), true
}

func (m Model) compareAgainstShelfRow() (actionRow, bool) {
	ref, label, ok := m.focusedCompareRef()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "shelf-compare-against",
		label: "Compare against shelf",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.pendingCompare = &pendingCompare{ref: ref, label: label, target: compareShelf}
			return m, m.loadShelfCmd()
		},
	}, true
}
```
Refactor `compareAgainstBookmarkRow` to use `focusedCompareRef` and set `target: compareBookmark`. In `internal/tui/action_menu.go`, inject `compareAgainstShelfRow` wherever `compareAgainstBookmarkRow` is injected (both `availableActions` branches).

- [ ] **Step 7: Loaded-msg compare wiring**

`internal/tui/model.go`:
- `bookmarksLoadedMsg`: guard the compare stamp with `pc.target == compareBookmark`:
```go
		if pc := m.pendingCompare; pc != nil && pc.target == compareBookmark {
			m.bookmarkPopup.compareRef = &pc.ref
			m.bookmarkPopup.compareLabel = pc.label
			m.pendingCompare = nil
		}
```
- `shelfLoadedMsg`: stamp the shelf arm (added in Task 2 Step 4, now real):
```go
		if pc := m.pendingCompare; pc != nil && pc.target == compareShelf {
			m.shelfPopup.compareRef = &pc.ref
			m.shelfPopup.compareLabel = pc.label
			m.pendingCompare = nil
		}
```

- [ ] **Step 8: Run tests**

Run: `go build ./... && go test ./internal/tui/ -run 'Compare|CAgainst|Shelf|Bookmark' 2>&1 | tail -12 && go test ./internal/tui/ 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add -A internal/tui
git commit -m "feat(tui): compare matrix — files-menu vs shelf + c cross-compare

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 5: Docs, help, footer, race gate

**Files:**
- Modify: `internal/tui/help.go`, `internal/tui/footer.go`, `CHANGELOG.md`, `README.md`, `CLAUDE.md`

- [ ] **Step 1: help.go**

Add a `G` row (mirror the `g` row) describing the shelf switcher (enter=diff vs working tree, p=restore, m+m=compare two, x=remove, c=compare against bookmark, /filter, z mode), and extend the `g` row to mention `c` (compare against shelf) + the `.`-menu "Compare against shelf".

- [ ] **Step 2: footer.go**

Add a global binding near `bookmarks`:
```go
	{"shelf", "G", "[G] shelf", Model.opsIdle, scopeGlobal},
```
(The `shelf-*` tab bindings were already removed in Task 3.)

- [ ] **Step 3: CHANGELOG / README / CLAUDE**

- CHANGELOG `### Changed`: the Shelf is now a global `G` quick-switcher popup (replacing the Shelf tab), with enter/restore/mark-compare/remove; new `.`-menu **Compare against shelf** and `c` cross-compare between the bookmark and shelf popups.
- README: replace the Shelf-tab description with the `G` popup; note compare actions.
- CLAUDE.md: update the `tui` notes / any tab list — the shelf is a popup (`G`), not a left tab; the compare matrix reuses `pendingCompare`/`openPickerDiff`.

- [ ] **Step 4: Full race gate**

Run: `./test.sh race`
Expected: vet+gofmt clean, all unit tests pass, e2e green.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "docs(tui): shelf popup (G) + compare matrix; help/footer

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

## Self-Review

**Spec coverage:**
- Shelf popup mirroring bookmark popup (list/filter/z/jump/restore/mark/remove) → Tasks 1-2. ✓
- Open key `G` in all surfaces → Task 2 Steps 4-5. ✓
- Tab removal (enum/leftTabs/mark/footer/view/viewstate/focusedBookmark/action_menu) → Task 3. ✓
- Files-menu "Compare against shelf" → Task 4 Step 6. ✓
- Cross-compare `c` (bookmark→shelf, shelf→bookmark) on generalized `pendingCompare` → Task 4 Steps 3-7. ✓
- Right-side resolvers (shelf via ResolveBytes(SourceShelf), bookmark via BookmarkBytes) → Task 4 Steps 3-4. ✓
- Shared diff handoff (clearStack) `openPickerDiff` → Task 1 Step 1. ✓
- help/footer/docs → Task 5. ✓

**Placeholder scan:** Task 2 intentionally adds two named stubs (`shelfCompareAgainstBookmark`, `openCompareFocusedVsShelf`) replaced in Task 4 — explicitly flagged, not silent TODOs. No other placeholders.

**Type consistency:** `shelfPopup{…compareRef *model.FileRef, compareLabel string}`, `popupSelectedShelfEntry`, `openShelfCompareEntry`, `openShelfCompareTwoEntries`, `openShelfRestore`, `openPickerDiff`, `pendingCompare{ref,label,target}`, `comparePopupKind`/`compareBookmark`/`compareShelf`, `openCompareFocusedVsShelf`, `compareAgainstShelfRow` (id `shelf-compare-against`), `loadCompareTwoRefsCmd` — consistent across tasks. Test typo `TestComdareAgainstShelfMenuRow` is just a test name; rename to `TestCompareAgainstShelfMenuRow` when writing.
