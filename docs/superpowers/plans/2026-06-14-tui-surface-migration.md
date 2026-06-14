# TUI Surface Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every TUI surface a view-stack entry and delete the three hand-maintained render/key/mouse `if`-chains, so the routing invariant (input owner == stack top) is structural.

**Architecture:** `surface` gains a compositing `kind()` (replace / overlay / decoration) plus optional `positioned`/`mouseHandler` interfaces. `render`/key/`handleMouse` each become a single stack walk. Existing overlay structs become surfaces (thin forwarders to today's `Model` render/update methods). The base grid becomes `gridSurface` **early** (right after the walk machinery) so every later overlay always has a replace base beneath it. Named overlay fields (`m.diffView`, `m.popup`, …) are deleted; reads go through a generic `surfaceOf[T]`. Staged so the suite stays green after every task — **deleting each field makes the compiler enumerate the sites to fix.**

**Tech Stack:** Go 1.26, Bubble Tea (Elm value-receiver `Model`), lipgloss.

**Spec:** `docs/superpowers/specs/2026-06-14-tui-surface-migration-design.md`. Read §2, §3.1, §5 before starting.

**Pure refactor:** no surface changes behaviour. The existing TUI suite is the safety net; `./test.sh` after every task, `./test.sh race` before merge.

---

## Transition model (read once before Task 1)

Today's render/key/mouse precedence is **modal → stack(history/blame) → diffView → popups → filesView → base grid**; the stack already sits *above* the legacy field-surfaces.

**Why the grid converts early, not last.** An `overlay` surface only renders if `renderStack` finds a `replace` surface *beneath* it to use as the base. If the grid were still a legacy field while we convert the popups, a popup pushed onto the stack would have no replace base — `renderStack` would find none and the popup would draw nowhere. So we make the grid the permanent **stack[0]** immediately after the walk machinery (Task 4). From then on every overlay always composites over the grid, and "an overlay is up" ⟺ `len(stack.entries) > 1` — a single consistent invariant for both compositing and tooltip suppression.

**The hybrid window (Tasks 4–13).** After Task 4 the grid is on the stack but the other surfaces are still legacy fields. During this window:
- **render** = legacy replace short-circuits (`modal`/`diffView`, until they convert) → `renderStack()` (grid base + any converted overlays) → legacy overlay composites for *unconverted* popup fields → tooltip (in `gridSurface`). Each conversion deletes one legacy term.
- **key** = if `stackTop()` is **not** the grid, dispatch to it (history/blame/converted surfaces); else check the remaining legacy fields; else `gridSurface.update`.
- **mouse** = symmetric.

When the last overlay (filesView, Task 13) converts, every legacy term is gone and all three collapse to single walks.

**Conversion order:** **grid (Task 4)** → modal → diffView → worktree/repo/settings/branch/pair-op popups → help window → filesView (last).

---

## File structure

| File | Responsibility after this plan |
|---|---|
| `internal/tui/stack.go` | `surface` + `kind()`; `compositeKind`; `positioned`/`mouseHandler`; `renderStack`/`routeMouse`; `surfaceOf[T]` |
| `internal/tui/grid_surface.go` (new) | `gridSurface` — base 3+1 panels, tooltip, mark, base key switch (`updateGridKey`), panel mouse (`mouseInGrid`) |
| `internal/tui/view.go` | `render()` → the stack walk |
| `internal/tui/model.go` | overlay fields deleted; `KeyMsg` arm → stack dispatch; async handlers use `surfaceOf` |
| `internal/tui/mouse.go` | `handleMouse` → `routeMouse` + residual `mouseInGrid` |
| `internal/tui/{diff_view,worktree_popup,repo_popup,settings_popup,branch_popup,content_popup,pairop_popup,files_view}.go` | each gains its `surface` methods; `content_popup.go` adds `helpWindow` + `commitFilesView` wrappers |
| `internal/tui/*_test.go` | new dispatch/compositing tests; existing tests touched only where they poke a deleted field |

---

## Task 1: Compositing kinds + `surfaceOf` (TDD)

**Files:** Modify `internal/tui/stack.go`, `history_view.go`, `blame_view.go`; Test `internal/tui/stack_test.go`

- [ ] **Step 1: Write the failing test** (append to `stack_test.go`)

```go
type fakeReplace struct{ name string }

func (fakeReplace) render(m Model) string                         { return "R" }
func (fakeReplace) update(m Model, _ tea.KeyMsg) (Model, tea.Cmd) { return m, nil }
func (fakeReplace) kind() compositeKind                           { return kindReplace }

func TestSurfaceOfFindsTopmostOfType(t *testing.T) {
	var m Model
	m = m.pushSurface(fakeReplace{"a"})
	b := &historyView{}
	m = m.pushSurface(b)
	if got, ok := surfaceOf[*historyView](m); !ok || got != b {
		t.Fatalf("surfaceOf[*historyView] = %v,%v want %v", got, ok, b)
	}
	if _, ok := surfaceOf[*blameView](m); ok {
		t.Fatal("surfaceOf[*blameView] should miss")
	}
}

func TestKindsAreReplaceForFullScreenSurfaces(t *testing.T) {
	if (&historyView{}).kind() != kindReplace || (&blameView{}).kind() != kindReplace {
		t.Fatal("history/blame must be replace surfaces")
	}
}
```

- [ ] **Step 2: Run — verify it fails**

Run: `go test ./internal/tui -run 'TestSurfaceOf|TestKindsAre' -v`
Expected: FAIL — `compositeKind`/`kindReplace`/`surfaceOf`/`kind` undefined.

- [ ] **Step 3: Add the machinery to `stack.go`**

```go
// compositeKind says how a surface draws relative to those beneath it.
type compositeKind int

const (
	kindReplace    compositeKind = iota // owns the whole screen
	kindOverlay                         // a box composited over what's beneath; owns input
	kindDecoration                      // render-only; never owns input (reserved; no stack consumer yet)
)
```

Add `kind() compositeKind` to the `surface` interface:

```go
type surface interface {
	render(m Model) string
	update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
	kind() compositeKind
}
```

Add the optional interfaces and the lookup:

```go
// positioned: top-left where an overlay/decoration box is composited. A surface
// that does not implement it is centered.
type positioned interface {
	at(m Model, boxW, boxH, scrW, scrH int) (x, y int)
}

// mouseHandler: a surface that consumes mouse events. One that does not SWALLOWS
// mouse while it owns input.
type mouseHandler interface {
	mouse(m Model, msg tea.MouseMsg) (Model, tea.Cmd)
}

// surfaceOf returns the topmost stack entry of concrete type T, if any.
func surfaceOf[T surface](m Model) (T, bool) {
	var zero T
	if m.stack == nil {
		return zero, false
	}
	for i := len(m.stack.entries) - 1; i >= 0; i-- {
		if s, ok := m.stack.entries[i].(T); ok {
			return s, true
		}
	}
	return zero, false
}
```

- [ ] **Step 4: Add `kind()` to the two existing surfaces**

`history_view.go`: `func (h *historyView) kind() compositeKind { return kindReplace }`
`blame_view.go`: `func (b *blameView) kind() compositeKind { return kindReplace }`

- [ ] **Step 5: Run — verify pass + build**

Run: `go test ./internal/tui -run 'TestSurfaceOf|TestKindsAre' -v && go build ./internal/tui`
Expected: PASS; build OK.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/stack.go internal/tui/stack_test.go internal/tui/history_view.go internal/tui/blame_view.go
git commit -m "tui: add compositeKind, positioned/mouseHandler, surfaceOf to the view stack"
```

---

## Task 2: The render walk `renderStack` (TDD)

**Files:** Modify `internal/tui/view.go`; Test `internal/tui/stack_test.go`

- [ ] **Step 1: Write the failing test**

```go
type fakeOverlay struct{ body string }

func (f fakeOverlay) render(m Model) string                         { return f.body }
func (f fakeOverlay) update(m Model, _ tea.KeyMsg) (Model, tea.Cmd) { return m, nil }
func (f fakeOverlay) kind() compositeKind                           { return kindOverlay }

func TestRenderStackCompositesOverlayOverReplace(t *testing.T) {
	m := Model{width: 40, height: 12}
	m = m.pushSurface(&blameView{ctx: navContext{path: "x"}, loading: true}) // replace base
	m = m.pushSurface(fakeOverlay{body: "POPUP"})
	out := m.renderStack()
	if !contains(out, "POPUP") {
		t.Fatalf("overlay not composited:\n%s", out)
	}
}

func TestRenderStackEmptyWithoutReplace(t *testing.T) {
	m := Model{width: 40, height: 12}
	m = m.pushSurface(fakeOverlay{body: "X"}) // overlay with no replace base
	if m.renderStack() != "" {
		t.Fatal("renderStack must return \"\" when the stack has no replace base")
	}
}
```

- [ ] **Step 2: Run — verify it fails**

Run: `go test ./internal/tui -run TestRenderStack -v`
Expected: FAIL — `m.renderStack` undefined.

- [ ] **Step 3: Implement `renderStack`**

In `internal/tui/view.go`:

```go
// renderStack renders the stack as one image: the topmost replace surface is
// the base, every surface above it is composited over it in stack order.
// Returns "" when the stack holds no replace surface (the caller falls back to
// the legacy layer during the migration; from Task 4 on the grid is always a
// replace base, so this never returns "" in production).
func (m Model) renderStack() string {
	if m.stack == nil || len(m.stack.entries) == 0 {
		return ""
	}
	es := m.stack.entries
	base := -1
	for i := len(es) - 1; i >= 0; i-- {
		if es[i].kind() == kindReplace {
			base = i
			break
		}
	}
	if base < 0 {
		return ""
	}
	w, h := m.overlayDims()
	out := es[base].render(m)
	for i := base + 1; i < len(es); i++ {
		s := es[i]
		box := s.render(m)
		if p, ok := s.(positioned); ok {
			x, y := p.at(m, lipgloss.Width(box), countLines(box), w, h)
			out = overlayAt(out, box, x, y, w, h)
		} else {
			out = overlayCenter(out, box, w, h)
		}
	}
	return out
}

// countLines counts display rows in s (1 + number of '\n').
func countLines(s string) int { return strings.Count(s, "\n") + 1 }
```

> `lipgloss`/`strings` are already imported in view.go. If a line-count helper already exists, reuse it instead of `countLines`.

- [ ] **Step 4: Wire `render()` to prefer the stack image**

Replace the existing stack branch in `render()`:

```go
	if s := m.stackTop(); s != nil {
		_, h := m.overlayDims()
		return clipToHeight(s.render(m), h)
	}
```

with:

```go
	if img := m.renderStack(); img != "" {
		_, h := m.overlayDims()
		return clipToHeight(img, h)
	}
```

Leave the legacy `if m.modal` (above) and `if m.diffView` / popup composites (below) unchanged — the legacy layer until each converts.

- [ ] **Step 5: Run — verify pass + package**

Run: `go test ./internal/tui -run TestRenderStack -v && go test ./internal/tui`
Expected: PASS; package ok (history/blame still render byte-identically).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/view.go internal/tui/stack_test.go
git commit -m "tui: renderStack composites the view stack (replace base + overlays)"
```

---

## Task 3: `routeMouse` (TDD)

**Files:** Modify `internal/tui/mouse.go`; Test `internal/tui/stack_test.go`

- [ ] **Step 1: Write the failing test**

```go
type fakeMouse struct{ hit *bool }

func (fakeMouse) render(m Model) string                         { return "" }
func (fakeMouse) update(m Model, _ tea.KeyMsg) (Model, tea.Cmd) { return m, nil }
func (fakeMouse) kind() compositeKind                           { return kindReplace }
func (f fakeMouse) mouse(m Model, _ tea.MouseMsg) (Model, tea.Cmd) {
	*f.hit = true
	return m, nil
}

func TestRouteMouseDispatchesToTopHandler(t *testing.T) {
	hit := false
	var m Model
	m = m.pushSurface(fakeMouse{hit: &hit})
	m.routeMouse(tea.MouseMsg{})
	if !hit {
		t.Fatal("routeMouse must dispatch to the top mouseHandler")
	}
}

func TestRouteMouseSwallowsWhenNoHandler(t *testing.T) {
	var m Model
	m = m.pushSurface(&blameView{}) // no mouse() ⇒ swallow
	if _, cmd := m.routeMouse(tea.MouseMsg{}); cmd != nil {
		t.Fatal("a surface without mouse() must swallow (cmd nil)")
	}
}
```

- [ ] **Step 2: Run — verify it fails**

Run: `go test ./internal/tui -run TestRouteMouse -v`
Expected: FAIL — `m.routeMouse` undefined.

- [ ] **Step 3: Add `routeMouse` and use it for the stack branch in `handleMouse`**

```go
// routeMouse sends a mouse event to the top surface if it handles mouse,
// otherwise swallows it (the top owns input exclusively).
func (m Model) routeMouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	if mh, ok := m.stackTop().(mouseHandler); ok {
		return mh.mouse(m, msg)
	}
	return m, nil
}
```

Replace the legacy stack branch in `handleMouse`:

```go
	if m.stackTop() != nil {
		return m, nil // history/blame are keyboard-only (v1)
	}
```

with:

```go
	if m.stackTop() != nil {
		return m.routeMouse(msg)
	}
```

(history/blame have no `mouse()`, so they still swallow — identical behaviour.)

- [ ] **Step 4: Run + commit**

Run: `go test ./internal/tui -run TestRouteMouse -v && go test ./internal/tui`
Expected: PASS; package ok.

```bash
git add internal/tui/mouse.go internal/tui/stack_test.go
git commit -m "tui: routeMouse dispatches mouse to the top surface (swallow otherwise)"
```

---

## Task 4: Base grid → `gridSurface` (stack[0]) — the permanent base

This must land before any overlay converts (it provides their replace base) and before the replace surfaces (so the hybrid render/key/mouse below is in place).

**Files:** Create `internal/tui/grid_surface.go`; Modify `internal/tui/view.go`, `model.go`, `mouse.go`; Test `internal/tui/stack_test.go`

- [ ] **Step 1: Extract the base key switch and panel mouse into methods**

In `model.go`, move the base `switch msg.String()` (the panel keys + the `filterTyping` branch currently inline at the bottom of the `KeyMsg` arm — everything after the legacy `if m.modal/...` chain) into:

```go
func (m Model) updateGridKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) { /* the moved base switch */ }
```

> Keep its return type `(tea.Model, tea.Cmd)` so `return m.reRoot(path)` (which returns `(tea.Model, tea.Cmd)`) and the other `return m, cmd` statements move verbatim. `gridSurface.update` adapts the type (Step 2).

In `mouse.go`, move the residual panel click/wheel logic (everything after the overlay precedence checks) into `func (m Model) mouseInGrid(msg tea.MouseMsg) (Model, tea.Cmd)`.

- [ ] **Step 2: Create `gridSurface`** (`grid_surface.go`)

```go
package tui

import tea "github.com/charmbracelet/bubbletea"

// gridSurface is the base 3+1 panel layout: the always-present floor (stack[0])
// of the view stack. It owns the panels, the contextual footer, the tooltip and
// the mark.
type gridSurface struct{}

func (gridSurface) kind() compositeKind { return kindReplace }

func (gridSurface) render(m Model) string {
	bg := m.renderInterface()
	// Tooltip shows only when nothing is above the grid. During the migration
	// that means: no converted overlay (len==1) AND no unconverted legacy popup
	// field set. Drop each legacy term as that popup converts; after Task 13
	// only `len(m.stack.entries) == 1` remains.
	if len(m.stack.entries) == 1 && !m.anyLegacyOverlay() {
		if lines, x, y, ok := m.tooltip(); ok {
			w, h := m.overlayDims()
			bg = overlayAt(bg, strings.Join(lines, "\n"), x, y, w, h)
		}
	}
	return bg
}

func (gridSurface) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	mm, cmd := m.updateGridKey(msg)
	return mm.(Model), cmd
}

func (gridSurface) mouse(m Model, msg tea.MouseMsg) (Model, tea.Cmd) { return m.mouseInGrid(msg) }
```

Add the shrinking helper (delete each term as the popup converts in Tasks 7–13; remove the whole function at Task 13):

```go
// anyLegacyOverlay reports whether an unconverted overlay field is open. Each
// term is deleted when its surface converts; the function is removed at Task 13.
func (m Model) anyLegacyOverlay() bool {
	return m.popup != nil || m.repoPopup != nil || m.settings != nil ||
		m.branchPopup != nil || m.contentPopup != nil || m.pairPopup != nil || m.filesView != nil
}
```

> `strings` is already imported by view.go; `grid_surface.go` needs its own import if it uses `strings.Join` — put the tooltip composite in a helper in view.go if cleaner, or import `strings` here.

- [ ] **Step 3: Push the grid as stack[0]**

Find the constructor/`Init` where the `Model` is first built and `reRoot` where it rebuilds. Ensure the grid is the bottom entry:
```go
m = m.pushSurface(gridSurface{}) // permanent stack[0]
```
In `reRoot`, after resetting `m.stack`, push `gridSurface{}` before any reload. (The stack now always has ≥1 entry.)

- [ ] **Step 4: Rewire the hybrid dispatch**

`render()` — the stack now always has the grid as a replace base, so `renderStack()` never returns "". The grid renders the panels+tooltip; legacy replace surfaces still short-circuit above; legacy overlay fields composite after:

```go
func (m Model) render() string {
	_, scrH := m.overlayDims()
	// modal and diffView are still legacy fields at Task 4; they short-circuit
	// above the stack image and convert to surfaces in Tasks 5–6.
	if m.modal != nil {
		return clipToHeight(m.renderModal(), scrH)
	}
	if m.diffView != nil {
		return clipToHeight(m.renderDiffView(), scrH)
	}
	img := m.renderStack() // grid base (+ any converted overlays)
	w, h := m.overlayDims()
	// Legacy overlay composites for unconverted popups (deleted as each converts):
	if m.popup != nil {
		img = overlayCenter(img, m.renderWorktreePopup(), w, h)
	}
	if m.repoPopup != nil {
		img = overlayCenter(img, m.renderRepoPopup(), w, h)
	}
	if m.settings != nil {
		img = overlayCenter(img, m.renderSettingsPopup(), w, h)
	}
	if m.branchPopup != nil {
		img = overlayCenter(img, m.renderBranchPopup(), w, h)
	}
	if m.contentPopup != nil {
		img = overlayCenter(img, m.renderContentPopup(), w, h)
	}
	if m.pairPopup != nil {
		img = overlayCenter(img, m.renderPairOpPopup(), w, h)
	}
	// filesView renders inside renderInterface today; it converts at Task 13.
	return clipToHeight(img, scrH)
}
```

Key arm in `Update` — dispatch to a converted surface on top, else legacy fields, else the grid:

```go
		top := m.stackTop()
		if _, isGrid := top.(gridSurface); !isGrid {
			if msg.Type == tea.KeyCtrlC {
				return m, tea.Quit
			}
			return top.update(m, msg)
		}
		// top is the grid → legacy overlay fields take precedence over it:
		if m.modal != nil { /* existing modal key block */ }
		if m.diffView != nil { return m.updateDiffViewKey(msg) }
		if m.popup != nil { return m.updatePopupKey(msg) }
		if m.repoPopup != nil { return m.updateRepoPopupKey(msg) }
		if m.settings != nil { return m.updateSettingsKey(msg) }
		if m.branchPopup != nil { return m.updateBranchPopupKey(msg) }
		if m.contentPopup != nil { return m.updateContentPopupKey(msg) }
		if m.pairPopup != nil { return m.updatePairPopupKey(msg) }
		if m.filesView != nil { return m.updateFilesViewKey(msg) }
		return top.update(m, msg) // gridSurface.update == updateGridKey
```

`handleMouse` — after the existing repo-switch guard, mirror the key precedence: if `stackTop()` isn't the grid, `routeMouse`; else the existing legacy overlay mouse branches; else `mouseInGrid` (which the grid's `mouse()` already calls when it's top via `routeMouse`). Simplest: keep the legacy overlay mouse branches as-is for now and add, before them, `if _, isGrid := m.stackTop().(gridSurface); !isGrid { return m.routeMouse(msg) }`.

- [ ] **Step 5: Test the grid-as-base invariant**

```go
func TestGridIsAlwaysStackBase(t *testing.T) {
	m := newTestModelWithGrid(t) // however the constructor builds it; or: var m Model; m = m.pushSurface(gridSurface{})
	if _, ok := m.stackTop().(gridSurface); !ok {
		t.Fatal("with nothing pushed, the grid is the top (and base)")
	}
	m = m.pushSurface(&blameView{})
	if m.renderStack() == "" {
		t.Fatal("an overlay/replace always has the grid as a base now")
	}
}
```

- [ ] **Step 6: Run + commit**

Run: `gofmt -l internal/tui/grid_surface.go && go test ./internal/tui`
Expected: gofmt clean; package ok (behaviour unchanged — the grid just moved onto the stack).

```bash
git add internal/tui/grid_surface.go internal/tui/view.go internal/tui/model.go internal/tui/mouse.go internal/tui/stack_test.go
git commit -m "tui: base grid becomes gridSurface (stack[0]); hybrid dispatch over it"
```

---

## Conversion tasks (5–13): the template

Each converts one legacy surface. **Five moves; per-surface specifics tabulated in each task.**

1. **Add `surface` methods** to the struct: `kind()`, `render` (→ `m.renderXxx`), `update` (→ `m.updateXxxKey`), plus `at(...)` if positioned and `mouse(...)` if it handles mouse.
2. **Delete the named field** from `Model`.
3. **Fix every compiler error**: `m.X = &X{…}` (open) → `pushSurface`; `m.X = nil` (close) → `popSurface`; `m.X` / `m.X != nil` (read) → `surfaceOf[T](m)`.
4. **Delete the legacy branches** for this surface in `render()`, the key arm, `handleMouse`, and its term in `anyLegacyOverlay()`.
5. **Run** `go test ./internal/tui`; `gofmt -l`; commit `tui: convert <surface> to a view-stack surface`.

> The compiler is the checklist: after move 2 the build lists exactly the sites. Don't hand-hunt.

---

### Task 5: Convert the **decision modal** (replace)

First extract the inline modal key block from the `KeyMsg` arm into `func (m Model) updateModalKey(msg tea.KeyMsg) (Model, tea.Cmd)`. Surface methods (`modal_surface.go`):
```go
func (d *decisionState) render(m Model) string                          { return m.renderModal() }
func (d *decisionState) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) { return m.updateModalKey(msg) }
func (d *decisionState) kind() compositeKind                            { return kindReplace }
```
Field `modal`. Open: `opDecisionMsg` `m.modal = &decisionState{…}` → `pushSurface`. Close (`m.modal = nil`) → `popSurface`. Reads (`m.modal != nil` in footer/mouse/view/key) → `surfaceOf[*decisionState](m)`. Delete legacy: `render()` `if m.modal` short-circuit; key arm modal block; `handleMouse` `if m.modal { return m,nil }` (modal now swallows via no `mouse()`). Modal is `kindReplace` → renders full-screen + swallows exactly as before.

### Task 6: Convert the **diff view** (replace)

Methods (append to `diff_view.go`):
```go
func (v *diffView) render(m Model) string                          { return m.renderDiffView() }
func (v *diffView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) { return m.updateDiffViewKey(msg) }
func (v *diffView) kind() compositeKind                            { return kindReplace }
func (v *diffView) mouse(m Model, msg tea.MouseMsg) (Model, tea.Cmd) {
	if wheel := wheelOf(msg, m); wheel != 0 {
		v.scroll(wheel, m.diffBodyRows())
	}
	return m, nil
}
```
> `wheelOf(msg, m)` = the exact wheel-delta expression `handleMouse` computes today before `m.diffView.scroll(...)`. Reuse it verbatim (inline if there's no named helper).

Move `diffTag` onto `diffView`. Fields `diffView`, `diffTag`. Opens (status-enter, files-enter, `loadStatusDiffCmd`, `loadCommitDiffCmd`) build `&diffView{…, tag: …}` then `pushSurface`. Closes (esc, narrow resize) → `popSurface`. Reads (`diffMsg` handler `v.tag == msg.tag`, `WindowSizeMsg`, render) → `surfaceOf[*diffView](m)`. Delete legacy: `render()` `if m.diffView` short-circuit; key arm `if m.diffView`; `handleMouse` diff wheel branch.

### Tasks 7–11: Convert the **centered popups** (one task each)

`kindOverlay`, centered (no `positioned`), swallow mouse (no `mouse()`).

| Task | Struct | field | render fwd | update fwd | open assigns |
|---|---|---|---|---|---|
| 7 | `*worktreePopup` | `popup` | `m.renderWorktreePopup()` | `m.updatePopupKey(msg)` | `m.popup = &worktreePopup{…}` |
| 8 | `*repoPopup` | `repoPopup` | `m.renderRepoPopup()` | `m.updateRepoPopupKey(msg)` | `m.repoPopup = &repoPopup{…}` |
| 9 | `*settingsPopup` | `settings` | `m.renderSettingsPopup()` | `m.updateSettingsKey(msg)` | `m.settings = &settingsPopup{…}` |
| 10 | `*branchPopup` | `branchPopup` | `m.renderBranchPopup()` | `m.updateBranchPopupKey(msg)` | `m.branchPopup = &branchPopup{…}` |
| 11 | `*pairOpPopup` | `pairPopup` | `m.renderPairOpPopup()` | `m.updatePairPopupKey(msg)` | `m.pairPopup = &pairOpPopup{…}` |

Per task — example (Task 7), the rest identical with the row's names. Methods (append to the popup's file):
```go
func (p *worktreePopup) render(m Model) string                          { return m.renderWorktreePopup() }
func (p *worktreePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) { return m.updatePopupKey(msg) }
func (p *worktreePopup) kind() compositeKind                            { return kindOverlay }
```
Delete the field; push on open / pop on close; `surfaceOf[*worktreePopup](m)` for reads. Delete legacy: its `if m.popup` composite in `render()`, its `if m.popup` key branch, its disjunct in the `handleMouse` swallow group, and its term in `anyLegacyOverlay()`.

### Task 12: Convert the **help window** (`helpWindow` wrapper)

`m.contentPopup` is a `*contentPopup` (also the files-view type), so wrap it. New `content_surface.go`:
```go
type helpWindow struct{ *contentPopup }

func (h helpWindow) render(m Model) string                          { return m.renderContentPopup() }
func (h helpWindow) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) { return m.updateContentPopupKey(msg) }
func (h helpWindow) kind() compositeKind                            { return kindOverlay }
func (h helpWindow) mouse(m Model, msg tea.MouseMsg) (Model, tea.Cmd) {
	if wheel := wheelOf(msg, m); wheel != 0 {
		h.contentPopup.move(wheel)
	}
	return m, nil
}
```
> Use the exact wheel logic from `handleMouse`'s current `if m.contentPopup != nil` branch.

Delete the `contentPopup` field. Open (`?`): `m.contentPopup = &contentPopup{…}` → `pushSurface(helpWindow{&contentPopup{…}})`. Close → `popSurface`. Reads → `surfaceOf[helpWindow](m)` (then `.contentPopup`). Delete legacy: its `render()` composite, key branch, the dedicated `handleMouse` wheel branch, and its `anyLegacyOverlay()` term.

### Task 13: Convert the **files view** (`commitFilesView`, left-column overlay) — last overlay

New in `content_surface.go`:
```go
type commitFilesView struct {
	*contentPopup
	hash, title string // were m.filesHash / m.filesTitle
}

func (f *commitFilesView) render(m Model) string                          { return m.renderFilesView() }
func (f *commitFilesView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) { return m.updateFilesViewKey(msg) }
func (f *commitFilesView) kind() compositeKind                            { return kindOverlay }
func (f *commitFilesView) mouse(m Model, msg tea.MouseMsg) (Model, tea.Cmd) { return m.mouseInFilesView(msg, wheelOf(msg, m)) }
func (f *commitFilesView) at(m Model, boxW, boxH, scrW, scrH int) (int, int) { return 0, 0 } // left column at top-left
```
- Repoint `renderFilesView`/`updateFilesViewKey`/`mouseInFilesView`/`moveCommitUnderFilesView` from `m.filesView`/`m.filesHash`/`m.filesTitle` to `surfaceOf[*commitFilesView](m)` and the struct's `hash`/`title`.
- **renderInterface coupling:** `renderFilesView` is drawn inside `renderInterface` today. Drop that `if m.filesView` branch from `renderInterface` so the grid always draws its normal left panels; the `commitFilesView` overlay (via `renderStack`'s `at`) now covers the left column (it pads to `leftW×bodyH`).
- Delete `filesView`, `filesHash`, `filesTitle`. Open (`l`) → `pushSurface(&commitFilesView{contentPopup: …, hash: …, title: …})`. Close (`l`/esc/narrow-resize) → `popSurface`. Reads (incl. the blame/history `b`/`h` entry points that read `m.filesHash`) → `surfaceOf[*commitFilesView](m)` then `.hash`.
- This is the last overlay: after it, delete `anyLegacyOverlay()` entirely, and collapse `render()`/`Update` key arm/`handleMouse` to the single walks (Step below).

**Collapse to single walks** (in this same task, after the suite is green with the overlay converted):
```go
func (m Model) render() string {
	_, h := m.overlayDims()
	return clipToHeight(m.renderStack(), h)
}
```
Key arm: `top := m.stackTop(); if msg.Type == tea.KeyCtrlC && replaceOwnsQuit(top) { return m, tea.Quit }; return top.update(m, msg)` (keep the ctrl+c handling exactly as the surfaces expect — each surface already guards ctrl+c). `handleMouse`: after the repo-switch guard, `return m.routeMouse(msg)`. `gridSurface.render` tooltip check becomes just `if len(m.stack.entries) == 1`.

Run, gofmt, commit.

---

## Task 14: Guard test + full verification

**Files:** Test `internal/tui/stack_test.go`; docs

- [ ] **Step 1: Field-absence guard**

```go
func TestNoOverlayFieldsRemain(t *testing.T) {
	banned := map[string]bool{
		"modal": true, "popup": true, "repoPopup": true, "settings": true,
		"branchPopup": true, "contentPopup": true, "pairPopup": true,
		"diffView": true, "filesView": true,
	}
	tp := reflect.TypeOf(Model{})
	for i := 0; i < tp.NumField(); i++ {
		if banned[tp.Field(i).Name] {
			t.Errorf("Model.%s should be a view-stack surface, not a field", tp.Field(i).Name)
		}
	}
}

func TestInputOwnerIsStackTop(t *testing.T) {
	var m Model
	m = m.pushSurface(gridSurface{})
	m = m.pushSurface(&blameView{})
	if _, ok := m.stackTop().(*blameView); !ok {
		t.Fatal("top of stack must own input")
	}
}
```
> `reflect` import in the test file.

- [ ] **Step 2: Run the guard tests**

Run: `go test ./internal/tui -run 'TestNoOverlayFieldsRemain|TestInputOwnerIsStackTop' -v`
Expected: PASS.

- [ ] **Step 3: Full staged verification**

Run: `./test.sh`
Expected: vet+gofmt clean, unit + e2e pass.

- [ ] **Step 4: Race pass**

Run: `./test.sh race`
Expected: PASS with `-race`.

- [ ] **Step 5: Docs + commit**

Update `CLAUDE.md` (tui row: "value-receiver Model with a view stack of typed surfaces; one render/key/mouse walk, input owner == stack top") and a terse CHANGELOG Unreleased line (internal refactor, no user-facing change). Then:

```bash
git add internal/tui/stack_test.go CLAUDE.md CHANGELOG.md
git commit -m "tui: guard the single-walk dispatch; document the surface model"
```

---

## Follow-ups (out of scope)
- MCP / background-op routing that *uses* the now-structural "input owner == top surface" invariant.
- Converting the tooltip into a real `kindDecoration` stack entry (today it stays inside `gridSurface`).

---

## Self-review

**Spec coverage:** §2 model → Tasks 1–3; §3 inventory → Tasks 4–13 (every row); §3.1 contentPopup dual-role → `helpWindow` (12) + `commitFilesView` (13); §4 field deletion + `surfaceOf` → conversion template + Task 14 guard; §5 gridSurface/tooltip/mark/filesView/modal → Tasks 4/5/13; §6 sequencing — **corrected: grid converts EARLY (Task 4), not last**, because an overlay needs a replace base beneath it (a grid-last order would leave converted popups with no base — render hole); §7 non-goals → unchanged suite asserts no behaviour change; §8 timing → flagged for execution scheduling.

**Placeholder scan:** No "TBD"/"handle later". The conversion template (5–13) gives complete per-surface method bodies + exact field/branch deltas; "the compiler enumerates the sites" is a method, not a placeholder. The `>` notes ask the engineer to reuse an existing wheel expression verbatim (verification, not missing content). The `wheelOf(msg, m)` referenced in Tasks 6/12/13 is "the wheel-delta expression `handleMouse` uses today" — if it isn't already a named helper, Task 6 introduces it as one (extract the current inline expression) and 12/13 reuse it.

**Type consistency:** `compositeKind`/`kindReplace`/`kindOverlay`/`kindDecoration`, `positioned.at`, `mouseHandler.mouse`, `surfaceOf[T]`, `renderStack`, `routeMouse`, `gridSurface`, `updateGridKey`/`updateModalKey`, `anyLegacyOverlay`, `helpWindow`, `commitFilesView{hash,title}`, `wheelOf` are used consistently. Each converted surface forwards to the verified existing methods (`renderWorktreePopup`/`updatePopupKey`, `renderRepoPopup`/`updateRepoPopupKey`, `renderSettingsPopup`/`updateSettingsKey`, `renderBranchPopup`/`updateBranchPopupKey`, `renderContentPopup`/`updateContentPopupKey`, `renderPairOpPopup`/`updatePairPopupKey`, `renderFilesView`/`updateFilesViewKey`, `renderDiffView`/`updateDiffViewKey`, `renderModal`).
