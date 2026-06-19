# Overlay Stack + Paste-Dest Prefill Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the popup layer a stack — self-contained popups that own their state, with one orchestrator deciding what's on top, compositing, and key routing — so a child popup returns to its parent on close; and prefill the bookmark paste destination.

**Architecture:** A new `overlayStack` mirrors the existing full-screen `viewStack` (`internal/tui/stack.go`). Each migrated popup struct implements an `overlay` interface (`update(m, msg)` + `render(m, below)`), holds its own state, and lives on the stack — no `Model` field. Dispatch/render/mouse collapse to a single `overlayTop()` check, placed above the full-screen `stackTop` (overlays float over surfaces). `menuBackground()` already renders everything beneath the popup layer and is passed as `below`. Return-to-parent falls out of push/pop: opening a child pushes it over the still-present parent; closing pops back to the parent.

**Tech Stack:** Go 1.26, Bubble Tea (Elm-style value-receiver `Model` with pointer fields), `internal/tui`. Tests: real `git` in `t.TempDir()` or `FakeRunner`; `go test ./internal/tui/`.

## Global Constraints

- Migrated this round: **bookmark switcher, shelf switcher, bookmark paste, shelf restore** only. The other ~13 popups stay as legacy `Model` fields.
- `internal/tui` and `internal/cli` never import `internal/git` (archtest-guarded). Reach git via `internal/domain`.
- `Model` is a value receiver; the overlay stack is a **pointer** field (`*overlayStack`) so pushes persist across the value copy (same rationale as `modal`/`popup`/`stack`).
- TDD: failing test first, minimal code, green, commit. Run `gofmt -w` on touched files. Run `go test ./internal/tui/ -count=1` per task; the final task runs `./test.sh race`.
- New keybindings/behaviors must stay reflected in help/footer where applicable (here: no new keys — behavior change only).
- `_RESTORED` rule (verbatim): dotfile (basename starts `.`) → append `_RESTORED`; else if the basename has an extension (last `.` at index > 0) → insert before it; else append.

---

## File map

| File | Change |
|------|--------|
| `internal/tui/overlay_stack.go` | **new** — `overlay` interface, `overlayStack`, `pushOverlay`/`popOverlay`/`overlayTop`/`clearOverlays`, `bookmarkSwitcher()`/`shelfSwitcher()` accessors |
| `internal/tui/overlay_stack_test.go` | **new** — stack unit tests |
| `internal/tui/restore_path.go` | **new** — pure `restoredPath` helper |
| `internal/tui/restore_path_test.go` | **new** — `restoredPath` cases |
| `internal/tui/model.go` | remove 4 fields; rewire dispatch (`overlayTop()`), bookmarks/shelf loaded-msg push overlays; cheat-sheet gate |
| `internal/tui/view.go` | render via `overlayTop().render(m, menuBackground())`; remove 4 popup render blocks; cheat-sheet gate |
| `internal/tui/mouse.go` | overlay guard via `overlayTop()`; cheat-sheet gate |
| `internal/tui/bookmark_popup.go` | `bookmarkPopup`/`bookmarkPastePopup` implement `overlay`; open=push, close=pop; paste prefill; `openPickerDiff` clears overlays |
| `internal/tui/shelf_popup.go` | `shelfPopup` implements `overlay`; open=push, close=pop |
| `internal/tui/shelf_actions.go` | `shelfRestorePopup` implements `overlay`; open=push, close=pop |
| test files (6) | `bookmark_test.go`, `bookmark_compare_test.go`, `bookmark_global_key_test.go`, `popup_help_test.go`, `shelf_popup_test.go`, `shelf_test.go` — construct via `pushOverlay`, read via accessors |
| `CHANGELOG.md` | Unreleased entry |

---

### Task 1: Overlay stack infrastructure

**Files:**
- Create: `internal/tui/overlay_stack.go`
- Create: `internal/tui/overlay_stack_test.go`
- Modify: `internal/tui/model.go` (add the `overlays *overlayStack` field)

**Interfaces:**
- Produces:
  - `type overlay interface { update(m Model, msg tea.KeyMsg) (Model, tea.Cmd); render(m Model, below string) string }`
  - `type overlayStack struct{ entries []overlay }`
  - `func (m Model) overlayTop() overlay`
  - `func (m Model) pushOverlay(o overlay) Model`
  - `func (m Model) popOverlay() Model`
  - `func (m Model) clearOverlays() Model`
  - `func (m Model) bookmarkSwitcher() *bookmarkPopup` (topmost `*bookmarkPopup` on the stack, else nil)
  - `func (m Model) shelfSwitcher() *shelfPopup`

- [ ] **Step 1: Write the failing test**

`internal/tui/overlay_stack_test.go`:
```go
package tui

import "testing"

// a trivial overlay for stack tests.
type fakeOverlay struct{ id string }

func (fakeOverlay) update(m Model, msg keyLike) (Model, tea.Cmd) { return m, nil }
func (o fakeOverlay) render(m Model, below string) string       { return below + o.id }

func TestOverlayStackPushPopTop(t *testing.T) {
	var m Model
	if m.overlayTop() != nil {
		t.Fatal("empty stack must have nil top")
	}
	m = m.pushOverlay(fakeOverlay{id: "a"})
	m = m.pushOverlay(fakeOverlay{id: "b"})
	if got := m.overlayTop().(fakeOverlay).id; got != "b" {
		t.Fatalf("top = %q, want b", got)
	}
	m = m.popOverlay()
	if got := m.overlayTop().(fakeOverlay).id; got != "a" {
		t.Fatalf("after pop, top = %q, want a", got)
	}
	m = m.clearOverlays()
	if m.overlayTop() != nil {
		t.Fatal("clearOverlays must empty the stack")
	}
}

func TestPopOverlayEmptyIsNoOp(t *testing.T) {
	var m Model
	m = m.popOverlay() // must not panic
	if m.overlayTop() != nil {
		t.Fatal("pop on empty stack must stay empty")
	}
}
```

Note: the `update` signature in the real interface is `update(m Model, msg tea.KeyMsg)`. For the test's fake, import `tea "github.com/charmbracelet/bubbletea"` and use `tea.KeyMsg`; drop the `keyLike` placeholder. Final fake:
```go
import tea "github.com/charmbracelet/bubbletea"
type fakeOverlay struct{ id string }
func (fakeOverlay) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) { return m, nil }
func (o fakeOverlay) render(m Model, below string) string          { return below + o.id }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestOverlayStack|TestPopOverlayEmpty' -count=1`
Expected: FAIL — `undefined: overlayTop`, `pushOverlay`, etc.

- [ ] **Step 3: Implement `overlay_stack.go`**

`internal/tui/overlay_stack.go`:
```go
package tui

import tea "github.com/charmbracelet/bubbletea"

// overlay is a centered popup layered above the panel interface (and any
// full-screen surface). It owns its own state and is the single source of
// truth for what it shows; the overlayStack orchestrates which overlay is on
// top, compositing, and key routing. Mirrors the full-screen `surface`
// interface — the only difference is render takes `below` (the screen beneath)
// and composites onto it instead of owning the whole screen. The two stacks
// merge into one compositor when overlays and surfaces unify.
type overlay interface {
	update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
	render(m Model, below string) string
}

type overlayStack struct{ entries []overlay }

// overlayTop returns the active overlay, or nil when the stack is empty.
func (m Model) overlayTop() overlay {
	if m.overlays == nil || len(m.overlays.entries) == 0 {
		return nil
	}
	return m.overlays.entries[len(m.overlays.entries)-1]
}

// pushOverlay puts o on top. overlays is a pointer field so the push persists
// across Model value copies (same rationale as modal/popup/stack).
func (m Model) pushOverlay(o overlay) Model {
	if m.overlays == nil {
		m.overlays = &overlayStack{}
	}
	m.overlays.entries = append(m.overlays.entries, o)
	return m
}

// popOverlay drops the top overlay; a no-op on an empty stack. Popping reveals
// the overlay beneath (or, when empty, the surface/panel layers), whose state
// was never torn down.
func (m Model) popOverlay() Model {
	if m.overlays != nil && len(m.overlays.entries) > 0 {
		m.overlays.entries = m.overlays.entries[:len(m.overlays.entries)-1]
	}
	return m
}

// clearOverlays removes every overlay. Used when a popup hands off to a
// full-screen diff that must own the screen: the diff view is checked AFTER the
// overlay layer in render/dispatch/mouse, so a lingering overlay would hide it.
func (m Model) clearOverlays() Model {
	if m.overlays != nil {
		m.overlays.entries = nil
	}
	return m
}

// bookmarkSwitcher returns the topmost bookmark switcher on the overlay stack,
// or nil when none is open. Lets code and tests reach the live switcher without
// a Model field.
func (m Model) bookmarkSwitcher() *bookmarkPopup {
	if m.overlays == nil {
		return nil
	}
	for i := len(m.overlays.entries) - 1; i >= 0; i-- {
		if p, ok := m.overlays.entries[i].(*bookmarkPopup); ok {
			return p
		}
	}
	return nil
}

// shelfSwitcher returns the topmost shelf switcher on the overlay stack, else nil.
func (m Model) shelfSwitcher() *shelfPopup {
	if m.overlays == nil {
		return nil
	}
	for i := len(m.overlays.entries) - 1; i >= 0; i-- {
		if p, ok := m.overlays.entries[i].(*shelfPopup); ok {
			return p
		}
	}
	return nil
}
```

Add the field to `Model` in `internal/tui/model.go` (near the existing `stack *viewStack` at line 79):
```go
	overlays *overlayStack // top-of-everything centered popups; nil/empty = none
```

Note: `bookmarkSwitcher()`/`shelfSwitcher()` reference `*bookmarkPopup`/`*shelfPopup` which already exist as types; they compile now and gain `overlay`-interface methods in Tasks 3–4.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestOverlayStack|TestPopOverlayEmpty' -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/tui/overlay_stack.go internal/tui/overlay_stack_test.go
git add internal/tui/overlay_stack.go internal/tui/overlay_stack_test.go internal/tui/model.go
git commit -m "feat(tui): overlay stack scaffolding (mirrors viewStack)"
```

---

### Task 2: `restoredPath` prefill helper

**Files:**
- Create: `internal/tui/restore_path.go`
- Create: `internal/tui/restore_path_test.go`

**Interfaces:**
- Produces: `func restoredPath(p string) string`

- [ ] **Step 1: Write the failing test**

`internal/tui/restore_path_test.go`:
```go
package tui

import "testing"

func TestRestoredPath(t *testing.T) {
	cases := map[string]string{
		"config.go":       "config_RESTORED.go",
		"a/b/config.go":   "a/b/config_RESTORED.go",
		"Makefile":        "Makefile_RESTORED",
		"a/b/Makefile":    "a/b/Makefile_RESTORED",
		".gitignore":      ".gitignore_RESTORED",
		"a/.gitignore":    "a/.gitignore_RESTORED",
		".env.local":      ".env.local_RESTORED",
		"":                "_RESTORED",
	}
	for in, want := range cases {
		if got := restoredPath(in); got != want {
			t.Errorf("restoredPath(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestRestoredPath -count=1`
Expected: FAIL — `undefined: restoredPath`

- [ ] **Step 3: Implement `restore_path.go`**

`internal/tui/restore_path.go`:
```go
package tui

import (
	"path"
	"strings"
)

// restoredPath inserts a _RESTORED marker into a repo-relative path for the
// paste/restore destination prefill:
//   - dotfile (basename starts "."): append          .gitignore -> .gitignore_RESTORED
//   - has an extension (last "." > 0): insert before config.go -> config_RESTORED.go
//   - no extension:                    append        Makefile  -> Makefile_RESTORED
func restoredPath(p string) string {
	const marker = "_RESTORED"
	dir, base := path.Split(p) // dir keeps its trailing "/"
	if base == "" || strings.HasPrefix(base, ".") {
		return p + marker
	}
	if i := strings.LastIndex(base, "."); i > 0 {
		return dir + base[:i] + marker + base[i:]
	}
	return p + marker
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestRestoredPath -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/tui/restore_path.go internal/tui/restore_path_test.go
git add internal/tui/restore_path.go internal/tui/restore_path_test.go
git commit -m "feat(tui): restoredPath helper for paste-dest prefill"
```

---

### Task 3: Migrate the bookmark switcher + bookmark paste onto the stack

This is the core task. `bookmarkPopup` and `bookmarkPastePopup` become `overlay`s; their `Model` fields are removed; dispatch/render/mouse gain the `overlayTop()` check; paste returns to the switcher; the paste dest is prefilled; `openPickerDiff` clears overlays.

**Files:**
- Modify: `internal/tui/bookmark_popup.go`
- Modify: `internal/tui/model.go` (remove `bookmarkPopup`/`bookmarkPastePopup` fields; dispatch; `bookmarksLoadedMsg`; cheat-sheet gate)
- Modify: `internal/tui/view.go` (render; cheat-sheet gate)
- Modify: `internal/tui/mouse.go` (overlay guard; cheat-sheet gate)
- Modify (tests): `bookmark_test.go`, `bookmark_compare_test.go`, `bookmark_global_key_test.go`, `popup_help_test.go`

**Interfaces:**
- Consumes: `pushOverlay`/`popOverlay`/`overlayTop`/`clearOverlays`/`bookmarkSwitcher` (Task 1); `restoredPath` (Task 2).
- Produces:
  - `func (p *bookmarkPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)`
  - `func (p *bookmarkPopup) render(m Model, below string) string`
  - `func (p *bookmarkPastePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)`
  - `func (p *bookmarkPastePopup) render(m Model, below string) string`
  - `func (p *bookmarkPopup) selected() (model.Bookmark, bool)`, `(p *bookmarkPopup) byID(string) (model.Bookmark, bool)`, `(p *bookmarkPopup) moveSel(int)`

- [ ] **Step 1: Write the failing tests** (`internal/tui/bookmark_return_test.go`, new)

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gigagit/gg/internal/model"
)

// switcherModel opens the bookmark switcher with one bookmark, on the stack.
func switcherModel() Model {
	m := Model{width: 80, height: 24, sel: map[panel]int{}, sortModes: map[panel]sortMode{}}
	return m.pushOverlay(newBookmarkPopup([]model.Bookmark{{ID: "b1", Path: "src/app.go"}}))
}

func TestPasteEscReturnsToSwitcher(t *testing.T) {
	m := switcherModel()
	m.bookmarkSwitcher().filter = "ap" // a state we expect to survive
	// open paste (p)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = u.(Model)
	if _, ok := m.overlayTop().(*bookmarkPastePopup); !ok {
		t.Fatal("p must push the paste popup on top")
	}
	// esc the paste popup
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	sw := m.bookmarkSwitcher()
	if sw == nil || m.overlayTop() != sw {
		t.Fatal("esc must return to the bookmark switcher")
	}
	if sw.filter != "ap" {
		t.Fatalf("switcher filter = %q, must survive the round trip", sw.filter)
	}
}

func TestPasteDestPrefilled(t *testing.T) {
	m := switcherModel()
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = u.(Model)
	pp, ok := m.overlayTop().(*bookmarkPastePopup)
	if !ok {
		t.Fatal("paste popup must be open")
	}
	if pp.dest != "src/app_RESTORED.go" {
		t.Fatalf("dest = %q, want the prefilled _RESTORED path", pp.dest)
	}
}
```

Note: `newBookmarkPopup` builds the popup from items (existing). The paste flow needs the bookmark's bytes; in the test the `svc` is nil, so `bookmarkPastePrompt` must read the path for the prefill *before* fetching bytes, and the byte fetch is exercised in existing paste tests with a fake `svc`. If the nil `svc` panics on fetch, give `switcherModel` a `FakeRunner`-backed `svc` (see `filesModel` in `files_view_test.go` for the pattern: `domain.New(&git.Repo{Runner: f})`) and a fake `BookmarkBytes` response. Prefer setting a fake svc so both tests run the real code path.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tui/ -run 'TestPasteEscReturnsToSwitcher|TestPasteDestPrefilled' -count=1`
Expected: FAIL — `p` currently routes to the old field handler (no overlay) / dest empty.

- [ ] **Step 3: Convert `bookmarkPopup` to an overlay** (`internal/tui/bookmark_popup.go`)

3a. Rename the key handler to a method and drop the field read. Change:
```go
func (m Model) updateBookmarkPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	p := m.bookmarkPopup
```
to:
```go
func (p *bookmarkPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
```
The rest of the body is unchanged **except**: every `m.bookmarkPopup = nil` (the esc close, line ~203) becomes `m = m.popOverlay()`; calls to `m.selectedBookmark()` become `p.selected()`; `m.bookmarkMoveSel(±1)` become `p.moveSel(±1)`; `m.bookmarkByID(id)` become `p.byID(id)`; the `?` cheat-sheet case is unchanged (it sets `m.contentPopup`). The compare-mode `enter` and `c`/`m`/`p`/`x` branches keep calling the same `m.openCompareFocusedVsBookmark`, `m.bookmarkRemovePrompt`, `m.bookmarkPastePrompt`, `m.bookmarkMark` (updated below).

3b. Convert the helpers to methods:
```go
func (p *bookmarkPopup) selected() (model.Bookmark, bool) {
	vis := p.visibleIdx()
	if p.sel < 0 || p.sel >= len(vis) {
		return model.Bookmark{}, false
	}
	return p.items[vis[p.sel]], true
}
func (p *bookmarkPopup) byID(id string) (model.Bookmark, bool) {
	for _, b := range p.items {
		if b.ID == id {
			return b, true
		}
	}
	return model.Bookmark{}, false
}
func (p *bookmarkPopup) moveSel(d int) {
	if n := p.sel + d; n >= 0 && n < len(p.visibleIdx()) {
		p.sel = n
	}
}
```
Delete the old `m.selectedBookmark`, `m.bookmarkByID`, `m.bookmarkMoveSel`. Update their other callers (`bookmarkMark`, `bookmarkJump`, `bookmarkRemovePrompt`, `bookmarkPastePrompt`, `openBookmarkCompareTwo`) to take/derive `p` from `m.bookmarkSwitcher()`. Concretely, each of those `func (m Model) ...` helpers starts by reading the switcher:
```go
	p := m.bookmarkSwitcher()
	if p == nil {
		return m, nil
	}
	b, ok := p.selected()
```

3c. Add the render method (replaces the old `renderBookmarkPopup` body, now composited over `below`):
```go
func (p *bookmarkPopup) render(m Model, below string) string {
	_, h := m.overlayDims()
	box := m.renderBookmarkPopupBox(p) // the existing renderBookmarkPopup body, taking p
	w, _ := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), box, w, h)
}
```
Rename the existing `func (m Model) renderBookmarkPopup() string` to `func (m Model) renderBookmarkPopupBox(p *bookmarkPopup) string` and replace its first line `p := m.bookmarkPopup` with the parameter. (Keeps the box-drawing logic intact; only the data source changes.)

3d. Opening the switcher. `openBookmarkSwitcher` (line ~55) gates on `m.bookmarkPopup == nil`; change to gate on no switcher present:
```go
func (m Model) openBookmarkSwitcher() (Model, tea.Cmd) {
	if m.opsIdle() && m.bookmarkSwitcher() == nil {
		return m, m.loadBookmarksCmd()
	}
	return m, nil
}
```

3e. `bookmarkPastePrompt` (line ~300): push the paste overlay (don't nil anything) and prefill the dest:
```go
func (m Model) bookmarkPastePrompt() (tea.Model, tea.Cmd) {
	p := m.bookmarkSwitcher()
	if p == nil {
		return m, nil
	}
	b, ok := p.selected()
	if !ok {
		return m, nil
	}
	data, err := m.svc.BookmarkBytes(context.Background(), b)
	if err != nil {
		m.statusMsg = "bookmark paste: " + err.Error()
		return m, nil
	}
	return m.pushOverlay(&bookmarkPastePopup{origin: b.Path, data: data, dest: restoredPath(b.Path)}), nil
}
```

3f. Convert `bookmarkPastePopup` to an overlay. Rename `updateBookmarkPasteKey` → `(p *bookmarkPastePopup) update(m Model, msg tea.KeyMsg)`, drop `p := m.bookmarkPastePopup`. The esc close `m.bookmarkPastePopup = nil` becomes `m = m.popOverlay()` (reveals the switcher beneath). The enter-success path:
```go
	case tea.KeyEnter:
		dest := strings.TrimSpace(p.dest)
		if dest == "" {
			return m, nil
		}
		data := p.data
		m = m.popOverlay() // back to the switcher; it stays visible during the write
		return m.startOp(engine.WriteFile{Path: dest, Data: data})
```
Rename `renderBookmarkPastePopup` → `(p *bookmarkPastePopup) render(m Model, below string)`:
```go
func (p *bookmarkPastePopup) render(m Model, below string) string {
	var b strings.Builder
	b.WriteString("Paste bookmarked file to a new path\n\n")
	b.WriteString("from: " + p.origin + "  (resolved now)\n")
	b.WriteString("dest: " + p.dest + "\n\n")
	b.WriteString("[type] path  [enter] paste  [esc] cancel")
	w, h := m.overlayDims()
	box := modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}
```

3g. `bookmarkJump`, `openBookmarkCompareTwo` call `openPickerDiff`. Update `openPickerDiff` (line ~352) to clear overlays instead of niling fields:
```go
func (m Model) openPickerDiff(v *diffView, tag string, load tea.Cmd) (Model, tea.Cmd) {
	m = m.clearOverlays()
	m = m.clearStack()
	m.diffView = v
	m.diffTag = tag
	return m, load
}
```

3h. `bookmarkMark`/`openBookmarkCompareTwo` read the switcher: replace `p := m.bookmarkPopup` with `p := m.bookmarkSwitcher()` (+ nil guard) and `m.bookmarkByID` → `p.byID`.

3i. The remove path. `bookmarkRemovePrompt` (line ~265) currently nils `m.bookmarkPopup`; instead leave the switcher on the stack (the modal renders above it) and on **Cancel** the switcher is revealed automatically. Change the body to not pop, and the modal `onResolve` only acts on "Remove":
```go
func (m Model) bookmarkRemovePrompt() (tea.Model, tea.Cmd) {
	p := m.bookmarkSwitcher()
	if p == nil {
		return m, nil
	}
	b, ok := p.selected()
	if !ok {
		return m, nil
	}
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "bookmark-remove",
			Prompt:  "Remove bookmark " + b.Path + "?",
			Options: []string{"Remove", "Cancel"},
		},
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt == "Remove" {
				return m, m.bookmarkRemoveCmd(b.ID)
			}
			return m, nil // Cancel: modal closes, switcher (still on the stack) is revealed
		},
	}
	return m, nil
}
```

- [ ] **Step 4: Rewire routing**

4a. `internal/tui/model.go` dispatch — replace the two checks (lines ~375–382):
```go
		if m.bookmarkPastePopup != nil {
			return m.updateBookmarkPasteKey(msg)
		}
		if m.bookmarkPopup != nil {
			return m.updateBookmarkPopupKey(msg)
		}
		if m.shelfPopup != nil {
			return m.updateShelfPopupKey(msg)
		}
```
with a single overlay check **plus** the still-legacy shelf check (shelf migrates in Task 4):
```go
		if o := m.overlayTop(); o != nil {
			return o.update(m, msg)
		}
		if m.shelfPopup != nil {
			return m.updateShelfPopupKey(msg)
		}
```
Update the cheat-sheet gate just above it (line ~368) from `(m.bookmarkPopup != nil || m.shelfPopup != nil)` to `(m.overlayTop() != nil || m.shelfPopup != nil)`.

4b. `bookmarksLoadedMsg` handler (model.go ~270) — push or refresh the overlay instead of setting a field:
```go
	case bookmarksLoadedMsg:
		if msg.err != nil {
			m.statusMsg = "bookmarks: " + msg.err.Error()
			m.pendingCompare = nil
			return m, nil
		}
		p := newBookmarkPopup(msg.items)
		if pc := m.pendingCompare; pc != nil && pc.target == compareBookmark {
			p.compareRef = &pc.ref
			p.compareLabel = pc.label
		}
		m.pendingCompare = nil
		if existing := m.bookmarkSwitcher(); existing != nil {
			// reopen after a remove: refresh the live switcher in place.
			*existing = *p
			return m, nil
		}
		return m.pushOverlay(p), nil
```
(Use the actual current field/branch names in that handler; the key change is `m.bookmarkPopup = …` → push/refresh on the stack. Preserve the existing compare-mode assignment and error handling.)

4c. `internal/tui/view.go` render — replace the two bookmark blocks (lines ~157–164) and the cheat-sheet gate. Insert one overlay render above the surface/diff early returns:
```go
	if m.contentPopup != nil && (m.overlayTop() != nil || m.shelfPopup != nil) {
		w, h := m.overlayDims()
		return overlayCenter(clipToHeight(m.menuBackground(), h), m.renderContentPopup(), w, h)
	}
	if o := m.overlayTop(); o != nil {
		return o.render(m, m.menuBackground())
	}
	if m.shelfPopup != nil { // migrates in Task 4
		w, h := m.overlayDims()
		return overlayCenter(clipToHeight(m.menuBackground(), h), m.renderShelfPopup(), w, h)
	}
```
Remove the old `if m.bookmarkPastePopup != nil` and `if m.bookmarkPopup != nil` render blocks.

4d. `internal/tui/mouse.go` — replace the swallow guard (line ~38) and update the cheat-sheet gate (line ~29):
```go
	if m.contentPopup != nil && (m.overlayTop() != nil || m.shelfPopup != nil) {
		if wheel != 0 {
			m.contentPopup.move(wheel)
		}
		return m, nil
	}
	if m.overlayTop() != nil || m.shelfPopup != nil {
		return m, nil // centered overlays swallow mouse
	}
```
Remove `m.bookmarkPopup`/`m.bookmarkPastePopup` from the old guard.

4e. Remove the fields from `Model` (model.go ~49–50): delete `bookmarkPopup` and `bookmarkPastePopup`. Delete the now-dead `func (m Model) renderBookmarkPopup`/`updateBookmarkPopupKey`/`updateBookmarkPasteKey`/`renderBookmarkPastePopup` names (they were renamed in Step 3). Fix compile errors.

- [ ] **Step 5: Migrate the bookmark tests**

In `bookmark_test.go`, `bookmark_compare_test.go`, `bookmark_global_key_test.go`, `popup_help_test.go`: replace `m.bookmarkPopup = newBookmarkPopup(...)` with `m = m.pushOverlay(newBookmarkPopup(...))`; replace reads/assertions of `m.bookmarkPopup` with `m.bookmarkSwitcher()` (e.g., `if m.bookmarkSwitcher() == nil` for "open", `!= nil` ⇒ still open). For `bookmarkPastePopup`, assert `m.overlayTop().(*bookmarkPastePopup)`. In `popup_help_test.go`, `bookmarkPopupModel()` becomes:
```go
func bookmarkPopupModel() Model {
	m := Model{width: 80, height: 24, sel: map[panel]int{}, sortModes: map[panel]sortMode{}}
	return m.pushOverlay(newBookmarkPopup([]model.Bookmark{{ID: "b1", Path: "a.go"}}))
}
```
and `m.bookmarkPopup.filter`/`.markID` → `m.bookmarkSwitcher().filter`/`.markID`; the esc-return assertion uses `m.bookmarkSwitcher() != nil`.

- [ ] **Step 6: Run tests**

Run: `go test ./internal/tui/ -count=1`
Expected: PASS (bookmark + paste + cheat-sheet + return + prefill green; shelf still via legacy field).

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/tui/
git add -A
git commit -m "feat(tui): migrate bookmark switcher + paste onto the overlay stack

Switcher/paste are self-contained overlays that own their state; opening
paste pushes over the switcher, so esc/success returns to it. Paste dest
prefilled via restoredPath. openPickerDiff now clears overlays."
```

---

### Task 4: Migrate the shelf switcher + shelf restore onto the stack

Mirror Task 3 for `shelfPopup` (`shelf_popup.go`) and `shelfRestorePopup` (`shelf_actions.go`), and finish the routing (remove the transitional `|| m.shelfPopup != nil` clauses).

**Files:**
- Modify: `internal/tui/shelf_popup.go`, `internal/tui/shelf_actions.go`
- Modify: `internal/tui/model.go` (remove `shelfPopup`/`shelfRestorePopup` fields; `shelf entries loaded` msg pushes overlay; finalize gates), `internal/tui/view.go`, `internal/tui/mouse.go`
- Modify (tests): `shelf_popup_test.go`, `shelf_test.go`, `popup_help_test.go` (`shelfPopupModel`)

**Interfaces:**
- Consumes: Task 1 + Task 3 routing.
- Produces: `(p *shelfPopup) update/render`, `(p *shelfRestorePopup) update/render`, `(p *shelfPopup) selected()/moveSel()`.

- [ ] **Step 1: Write the failing tests** (`internal/tui/shelf_return_test.go`, new)

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gigagit/gg/internal/model"
)

func shelfSwitcherModel() Model {
	m := Model{width: 80, height: 24, sel: map[panel]int{}, sortModes: map[panel]sortMode{}}
	return m.pushOverlay(newShelfPopup([]model.ShelfEntry{{ID: "s1"}}))
}

func TestRestoreEscReturnsToShelfSwitcher(t *testing.T) {
	m := shelfSwitcherModel()
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")}) // restore
	m = u.(Model)
	if _, ok := m.overlayTop().(*shelfRestorePopup); !ok {
		t.Fatal("p must push the restore popup")
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	if m.shelfSwitcher() == nil || m.overlayTop() != m.shelfSwitcher() {
		t.Fatal("esc must return to the shelf switcher")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestRestoreEscReturnsToShelfSwitcher -count=1`
Expected: FAIL.

- [ ] **Step 3: Convert `shelfPopup` to an overlay** (`shelf_popup.go`)

Apply the Task-3 shape:
- `updateShelfPopupKey` → `(p *shelfPopup) update(m Model, msg tea.KeyMsg)`; drop `p := m.shelfPopup`; esc close `m.shelfPopup = nil` → `m = m.popOverlay()`.
- `m.popupSelectedShelfEntry()` → `(p *shelfPopup) selected() (model.ShelfEntry, bool)`; `m.shelfPopupMoveSel(±1)` → `p.moveSel(±1)`. Update callers (`shelfPopupMark`, `shelfPopupRemovePrompt`, `shelfCompareAgainstBookmark`, the `p` restore branch) to read `p := m.shelfSwitcher()` with a nil guard.
- `renderShelfPopup` → split: keep box logic as `func (m Model) renderShelfPopupBox(p *shelfPopup) string` (replace `p := m.shelfPopup` with the param), add `func (p *shelfPopup) render(m Model, below string) string` returning `overlayCenter(clipToHeight(below, h), m.renderShelfPopupBox(p), w, h)`.
- `openShelfSwitcher` gate: `m.shelfSwitcher() == nil`.
- The `p` (restore) branch: `m.shelfPopup = nil; return m.openShelfRestore(e)` → just `return m.openShelfRestore(e)` (openShelfRestore now pushes; see 4-Step-4).
- `shelfPopupRemovePrompt`: don't nil; modal `onResolve` Cancel returns `m, nil` (switcher revealed); Remove → `m.shelfRemoveCmd(e.ID)`.
- `shelfCompareAgainstBookmark`: replace `m.shelfPopup = nil` with `m = m.popOverlay()` before setting `pendingCompare` + `loadBookmarksCmd` (the bookmark switcher will push on load).

- [ ] **Step 4: Convert `shelfRestorePopup` to an overlay** (`shelf_actions.go`)

- `updateShelfRestoreKey` → `(p *shelfRestorePopup) update(m Model, msg tea.KeyMsg)`; drop `p := m.shelfRestorePopup`; esc `m.shelfRestorePopup = nil` → `m = m.popOverlay()`; enter-success: `m = m.popOverlay()` before `m.startOp(engine.WriteFile{...})`.
- `renderShelfRestorePopup` → `(p *shelfRestorePopup) render(m Model, below string)` returning `overlayCenter(clipToHeight(below, h), <existing box>, w, h)`.
- `openShelfRestore`: `return m.pushOverlay(&shelfRestorePopup{entryID: e.ID, origin: e.Origin.Path}), nil`. (Keep the deliberate empty `dest` — shelf restore is NOT prefilled.)

- [ ] **Step 5: Finalize routing**

- `model.go` dispatch: delete the transitional `if m.shelfPopup != nil { return m.updateShelfPopupKey(msg) }` and the `if m.shelfRestorePopup != nil { … }` block; the single `if o := m.overlayTop(); o != nil { return o.update(m, msg) }` now covers all four. Cheat-sheet gate → `m.contentPopup != nil && m.overlayTop() != nil`.
- `view.go`: delete the transitional shelf render block and the `if m.shelfRestorePopup != nil` block; cheat-sheet gate → `m.contentPopup != nil && m.overlayTop() != nil`.
- `mouse.go`: guard → `if m.overlayTop() != nil { return m, nil }`; cheat-sheet gate → `m.overlayTop() != nil`.
- The shelf-entries-loaded handler (model.go ~242): push/refresh the shelf overlay (mirror Task 3 Step 4b), preserving compare-mode (`compareShelf`) assignment.
- Remove the `shelfPopup` and `shelfRestorePopup` fields from `Model` (model.go ~47–48). Delete dead renamed funcs. Fix compile errors.

- [ ] **Step 6: Migrate shelf tests + `shelfPopupModel`**

`shelf_popup_test.go`, `shelf_test.go`, `popup_help_test.go`: `m.shelfPopup = newShelfPopup(...)` → `m = m.pushOverlay(...)`; `m.shelfRestorePopup` reads → `m.overlayTop().(*shelfRestorePopup)`; `m.shelfPopup` reads → `m.shelfSwitcher()`. `shelfPopupModel()` mirrors `bookmarkPopupModel()`.

- [ ] **Step 7: Run tests**

Run: `go test ./internal/tui/ -count=1`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
gofmt -w internal/tui/
git add -A
git commit -m "feat(tui): migrate shelf switcher + restore onto the overlay stack

Finishes the overlay-stack migration of the 4 feature popups; dispatch/
render/mouse now route the popup layer through overlayTop()."
```

---

### Task 5: Remove-success refresh + full integration

Verify remove-success refreshes the switcher in place (not a stale list), run the full race gate, and update docs.

**Files:**
- Modify: `CHANGELOG.md`
- Test: `internal/tui/bookmark_return_test.go` (add remove cases)

**Interfaces:** Consumes everything above.

- [ ] **Step 1: Write the failing test** (append to `bookmark_return_test.go`)

```go
func TestRemoveSuccessRefreshesSwitcher(t *testing.T) {
	// Two bookmarks; remove the first; the loaded msg (one bookmark) must
	// refresh the SAME switcher overlay in place, not push a second one.
	m := Model{width: 80, height: 24, sel: map[panel]int{}, sortModes: map[panel]sortMode{}}
	m = m.pushOverlay(newBookmarkPopup([]model.Bookmark{{ID: "b1", Path: "a.go"}, {ID: "b2", Path: "b.go"}}))
	depth := len(m.overlays.entries)
	u, _ := m.Update(bookmarksLoadedMsg{items: []model.Bookmark{{ID: "b2", Path: "b.go"}}})
	m = u.(Model)
	if len(m.overlays.entries) != depth {
		t.Fatalf("overlay depth = %d, want %d (refresh in place, not push)", len(m.overlays.entries), depth)
	}
	sw := m.bookmarkSwitcher()
	if sw == nil || len(sw.items) != 1 || sw.items[0].ID != "b2" {
		t.Fatalf("switcher must be refreshed to the new list, got %+v", sw)
	}
}
```

- [ ] **Step 2: Run to verify it fails or passes**

Run: `go test ./internal/tui/ -run TestRemoveSuccessRefreshesSwitcher -count=1`
Expected: PASS if Task 3 Step 4b implemented the in-place refresh; if it FAILS (a second overlay was pushed), fix the `bookmarksLoadedMsg` handler to refresh `existing` in place per Task 3 Step 4b.

- [ ] **Step 3: CHANGELOG entry** (`CHANGELOG.md`, under `## [Unreleased]` → `### Added`)

```markdown
- TUI: popup layer now has a stack (`overlayStack`, mirroring the full-screen
  `viewStack`). A child popup opened from the bookmark (`g`) / shelf (`G`)
  switcher — paste, restore, or the remove-confirm — returns to the switcher
  when closed or after the action succeeds, with the switcher's filter/selection
  intact. The "Paste bookmarked file to a new path" destination is prefilled
  from the bookmark's path with a `_RESTORED` marker (`config.go` →
  `config_RESTORED.go`; `.gitignore` → `.gitignore_RESTORED`).
```

- [ ] **Step 4: Full race gate**

Run: `./test.sh race`
Expected: `all green`.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "test(tui): remove-success refreshes the switcher in place; changelog"
```

---

## Self-review

**Spec coverage:** overlay interface/stack (Task 1) ✓; data-on-stack, fields removed (Tasks 3–4) ✓; `menuBackground()` as `below` (Task 3 Step 4c) ✓; routing collapse (Tasks 3–4) ✓; return-to-parent esc/success (Tasks 3–4 tests) ✓; remove-cancel reveal + remove-success refresh (Task 3 Step 3i/4b, Task 5) ✓; `openPickerDiff` clearOverlays (Task 3 Step 3g) ✓; cheat-sheet gate (Tasks 3–4) ✓; `restoredPath` + prefill (Tasks 2–3) ✓; shelf restore NOT prefilled (Task 4 Step 4) ✓; out-of-scope popups untouched ✓.

**Placeholder scan:** the only "use the actual current names" notes (Task 3 Step 4b, Task 4 Step 5) point at the live `bookmarksLoadedMsg`/shelf-loaded handlers; the transformation (assign-field → push/refresh) and full replacement code are given. No TBD/TODO.

**Type consistency:** `overlay.update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)` and `render(m Model, below string) string` are used identically in Tasks 1/3/4; accessors `bookmarkSwitcher()`/`shelfSwitcher()` returning `*bookmarkPopup`/`*shelfPopup` are consistent throughout; `restoredPath(string) string` consistent (Tasks 2–3).
