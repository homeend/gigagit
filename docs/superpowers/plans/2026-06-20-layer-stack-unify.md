# Layer-Stack Unification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (inline) or superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fold `overlayStack` (popups) + `viewStack` (surfaces) into one
push-ordered `layer` stack, collapsing the duplicated interface/stack/routing —
behavior identical.

**Architecture:** `overlay` + `surface` → one `layer` interface
(`render(m, below)`); `overlayStack` + `viewStack` → one `layerStack` (`layers`
field); the three routing sites' adjacent `overlayTop`/`stackTop` pair →
one `topLayer()`; render becomes a bottom-up walk over a base that is
`renderDiffView()` (when a diff is open) else `renderInterface()`.

**Tech Stack:** Go 1.26, Bubble Tea (value-receiver `Model`, pointer fields).

## Global Constraints

- `internal/tui` must not import `internal/git` (archtest-guarded). No new imports needed.
- Behavior is identical at every step; this is a structural refactor.
- The four single-slot pointer fields — `modal`, `proc`, `actionMenu`, `diffView` — are NOT touched.
- `cross_compare_return_test.go` (bookmark→shelf→esc returns to bookmark, and symmetric) is the regression north star — must stay green.
- Build + `go test ./internal/tui/` green after every task; full `./test.sh race` before merge.
- Work in the worktree at `/mnt/t/others/gg-layer-stack` (branch `worktree-layer-stack-unify`). Use worktree-absolute paths.

---

### Task 1: Surfaces adopt the `below` parameter

Converge `surface.render(m)` to `render(m, below string)` (surfaces own the
screen → ignore `below`), so `surface` and `overlay` become the same shape.
Pure signature change; behavior identical.

**Files:**
- Modify: `internal/tui/stack.go` (the `surface` interface)
- Modify: `internal/tui/history_view.go:118`, `internal/tui/blame_view.go:117`, `internal/tui/irebase_view.go:125`, `internal/tui/conflict_picker.go:275` (the 4 `render(m Model) string` methods — `hunkPicker` covers both conflict + stage pickers)
- Modify callers: `internal/tui/view.go:121` (`menuBackground`), `internal/tui/view.go:164` (`stackTop` render path)
- Test: existing surface render tests (`history_view_test.go`, `blame_view_test.go`, `irebase_view_test.go`, `conflict_picker_test.go`)

- [ ] **Step 1: Change the `surface` interface signature**

In `internal/tui/stack.go`:
```go
type surface interface {
	render(m Model, below string) string
	update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
}
```

- [ ] **Step 2: Update the 4 surface render methods to accept and ignore `below`**

Each becomes `func (x *T) render(m Model, _ string) string {` — body unchanged:
- `internal/tui/history_view.go:118` `func (h *historyView) render(m Model, _ string) string`
- `internal/tui/blame_view.go:117` `func (b *blameView) render(m Model, _ string) string`
- `internal/tui/irebase_view.go:125` `func (e *irebaseEditor) render(m Model, _ string) string`
- `internal/tui/conflict_picker.go:275` `func (e *hunkPicker) render(m Model, _ string) string`

- [ ] **Step 3: Update the two callers**

`internal/tui/view.go` — `menuBackground` (line ~121) and the `stackTop` render
path (line ~164). A surface ignores `below`, so pass the panel base:
```go
// menuBackground:
if s := m.stackTop(); s != nil {
	return s.render(m, m.renderInterface())
}
// render() stackTop path:
if s := m.stackTop(); s != nil {
	_, h := m.overlayDims()
	return clipToHeight(s.render(m, m.renderInterface()), h)
}
```

- [ ] **Step 4: Update surface render tests**

Any test calling `view.render(m)` directly (e.g. `historyView.render(m)`) now
passes a `below` string — use `""` (surfaces ignore it). Grep the four
`*_test.go` for `.render(` on a surface and add the arg.

- [ ] **Step 5: Build + test**

Run: `cd /mnt/t/others/gg-layer-stack && go build ./... && go test ./internal/tui/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui && git commit -m "refactor(tui): surfaces take below param (converge with overlay)"
```

---

### Task 2: One stack behind compatibility shims + collapse routing

Introduce the unified `layer`/`layerStack`/`layers` field and the canonical
`pushLayer`/`popLayer`/`topLayer`/`clearLayers`/`layerOf` accessors. Keep the old
names (`pushSurface`/`pushOverlay`/`popSurface`/`popOverlay`/`stackTop`/
`overlayTop`/`clearStack`/`clearOverlays`/`overlayOf`) as thin shims over the one
stack so the ~50 existing call sites and tests stay untouched this task. Collapse
the three routing sites (which would otherwise double-fire) to one `topLayer()`,
and turn render into the bottom-up walk. Delete `viewStack`/`overlayStack` types
and the `stack`/`overlays` fields.

**Interfaces:**
- Produces: `layer` interface; `layers *layerStack` field; `pushLayer`/`popLayer`/`topLayer`/`clearLayers`/`layerOf[T]`; shims for the old names.
- Consumes: Task 1's converged `render(m, below)` shape.

**Files:**
- Create: `internal/tui/layer_stack.go`
- Delete: `internal/tui/overlay_stack.go`, `internal/tui/stack.go`
- Modify: `internal/tui/model.go` (field decl + dispatch routing + typed lookups), `internal/tui/view.go` (render walk, drop `menuBackground`), `internal/tui/mouse.go` (routing)

- [ ] **Step 1: Write `layer_stack.go` — unified type, accessors, and shims**

```go
package tui

import tea "github.com/charmbracelet/bubbletea"

// layer is a window on the layer stack: a full-screen surface (history, blame,
// rebase/conflict/stage editors) or a centered popup (bookmark/shelf switchers,
// content/help, reword, …). The top owns the keyboard; popping reveals the layer
// beneath, whose state was never torn down. render composites onto `below` (the
// accumulated render of everything beneath): a surface ignores `below` and owns
// the screen; a popup composites its centered box onto `below`.
type layer interface {
	update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
	render(m Model, below string) string
}

// surface and overlay are retained as aliases during migration; Task 3 removes them.
type surface = layer
type overlay = layer

type layerStack struct{ entries []layer }

func (m Model) topLayer() layer {
	if m.layers == nil || len(m.layers.entries) == 0 {
		return nil
	}
	return m.layers.entries[len(m.layers.entries)-1]
}

func (m Model) pushLayer(l layer) Model {
	if m.layers == nil {
		m.layers = &layerStack{}
	}
	m.layers.entries = append(m.layers.entries, l)
	return m
}

func (m Model) popLayer() Model {
	if m.layers != nil && len(m.layers.entries) > 0 {
		m.layers.entries = m.layers.entries[:len(m.layers.entries)-1]
	}
	return m
}

func (m Model) clearLayers() Model {
	if m.layers != nil {
		m.layers.entries = nil
	}
	return m
}

// layerOf returns the topmost layer of concrete type T, or the zero value when none.
func layerOf[T layer](m Model) T {
	var zero T
	if m.layers == nil {
		return zero
	}
	for i := len(m.layers.entries) - 1; i >= 0; i-- {
		if p, ok := m.layers.entries[i].(T); ok {
			return p
		}
	}
	return zero
}

func (m Model) bookmarkSwitcher() *bookmarkPopup { return layerOf[*bookmarkPopup](m) }
func (m Model) shelfSwitcher() *shelfPopup       { return layerOf[*shelfPopup](m) }

// --- migration shims (Task 3 removes; all delegate to the one stack) ---
func (m Model) pushOverlay(o layer) Model { return m.pushLayer(o) }
func (m Model) pushSurface(s layer) Model { return m.pushLayer(s) }
func (m Model) popOverlay() Model         { return m.popLayer() }
func (m Model) popSurface() Model         { return m.popLayer() }
func (m Model) clearOverlays() Model      { return m.clearLayers() }
func (m Model) clearStack() Model         { return m.clearLayers() }
func (m Model) overlayTop() layer         { return m.topLayer() }
func (m Model) stackTop() layer           { return m.topLayer() }
func overlayOf[T layer](m Model) T        { return layerOf[T](m) }
```

- [ ] **Step 2: Delete the old stack files**

```bash
git rm internal/tui/overlay_stack.go internal/tui/stack.go
```
(Their `bookmarkSwitcher`/`shelfSwitcher`/`overlayOf` now live in `layer_stack.go`.)

- [ ] **Step 3: Replace the `Model` fields**

`internal/tui/model.go` — replace the two fields (lines ~63–64):
```go
	layers *layerStack // ordered window pile (surfaces + popups); nil/empty = none
```
Remove the `stack *viewStack` and `overlays *overlayStack` lines.

- [ ] **Step 4: Collapse dispatch routing (model.go ~386–394)**

Replace the adjacent `overlayTop` + `stackTop` checks with one:
```go
	if l := m.topLayer(); l != nil {
		return l.update(m, msg)
	}
```
NOTE on ctrl+c: the old `stackTop` branch had an early `if msg.Type ==
tea.KeyCtrlC { return m, tea.Quit }` before `s.update`. It is **redundant** —
every surface's own `update` already quits on ctrl+c (blame_view.go:182,
history_view.go:228, irebase_view.go:55, conflict_picker.go:138), as does every
popup. Dropping the early check and calling `l.update(m, msg)` for any top layer
is therefore behavior-identical. Verified: all 4 surface types + all popups
handle `KeyCtrlC` themselves.

- [ ] **Step 5: Re-point the typed message lookups (model.go ~210/221/226)**

```go
	if h := layerOf[*historyView](m); h != nil && h.listTag == msg.tag { … }
	if h := layerOf[*historyView](m); h != nil && h.diffTag == msg.tag { … }
	if b := layerOf[*blameView](m); b != nil && b.tag == msg.tag { … }
```

- [ ] **Step 6: Render walk (view.go) — drop `menuBackground`, walk the stack**

Replace `menuBackground()` body and the overlay/stack render paths with one walk.
The base is the diff (if open) else the panels; fold the stack bottom→top:
```go
// layerBase is what the layer stack composites over: the open diff, else the panels.
func (m Model) layerBase() string {
	if m.diffView != nil {
		return m.renderDiffView()
	}
	return m.renderInterface()
}

// renderLayers folds the stack bottom→top over layerBase. Empty stack → just the base.
func (m Model) renderLayers() string {
	below := m.layerBase()
	if m.layers != nil {
		for _, l := range m.layers.entries {
			below = l.render(m, below)
		}
	}
	return below
}
```
In `render()`:
- `actionMenu` path: `overlayCenter(clipToHeight(m.renderLayers(), h), m.renderActionMenu(), w, h)` (was `menuBackground`).
- Replace the `overlayTop` path AND the `stackTop` path AND the standalone
  `diffView` path with a single:
  ```go
  if m.layers != nil && len(m.layers.entries) > 0 || m.diffView != nil {
  	_, h := m.overlayDims()
  	return clipToHeight(m.renderLayers(), h)
  }
  ```
  (`renderLayers` already returns the diff base when the stack is empty but a
  diff is open, and composites popups over a surface or the diff as needed.)
- The base/tooltip tail stays: when no layer and no diff, render panels + tooltip.

Delete the now-unused `menuBackground` func.

- [ ] **Step 7: Collapse mouse routing (mouse.go ~35–43)**

Replace the `overlayTop` block + the `stackTop` swallow with one:
```go
	if l := m.topLayer(); l != nil {
		if cp, ok := l.(*contentPopup); ok && wheel != 0 {
			cp.move(wheel)
		}
		return m, nil // surfaces are keyboard-only; popups swallow (except contentPopup wheel)
	}
```
Leave the `diffView` and `actionMenu` mouse branches exactly where they are
(pre-existing order; not in scope).

- [ ] **Step 8: Build + full test**

Run: `cd /mnt/t/others/gg-layer-stack && go build ./... && go test ./internal/tui/`
Expected: PASS — especially `cross_compare_return_test.go`, `history_view_test.go`,
`blame_view_test.go`, `mouse_test.go`, `nav_test.go`.

- [ ] **Step 9: Commit**

```bash
git add -A && git commit -m "refactor(tui): one layer stack (merge overlay+view, collapse routing)"
```

---

### Task 3: Rename to canonical `layer*` names, drop shims, docs

Mechanically rename the old accessor names to the canonical `layer*` set across
production + tests, delete the shims and the `surface`/`overlay` aliases, merge
the two stack test files, and update CHANGELOG + CLAUDE.md.

**Files:**
- Modify: all `internal/tui/*.go` (production + tests) using the old names
- Modify: `internal/tui/layer_stack.go` (delete shims + aliases)
- Merge: `internal/tui/overlay_stack_test.go` + `internal/tui/stack_test.go` → `internal/tui/layer_stack_test.go`
- Modify: `CHANGELOG.md`, `CLAUDE.md` (if it names the stacks)

- [ ] **Step 1: Rename call sites (mechanical)**

Replace, across `internal/tui/`:
- `pushOverlay(` → `pushLayer(`, `pushSurface(` → `pushLayer(`
- `popOverlay(` → `popLayer(`, `popSurface(` → `popLayer(`
- `clearOverlays(` → `clearLayers(`, `clearStack(` → `clearLayers(`
- `overlayTop(` → `topLayer(`, `stackTop(` → `topLayer(`
- `overlayOf[` → `layerOf[`
Use `gofmt`-safe edits. After: `grep -rn 'pushOverlay\|pushSurface\|popOverlay\|popSurface\|clearOverlays\|clearStack\|overlayTop\|stackTop\|overlayOf' internal/tui` returns nothing.

- [ ] **Step 2: Delete the shims and aliases from `layer_stack.go`**

Remove the `--- migration shims ---` block and the `type surface = layer` /
`type overlay = layer` aliases. Replace remaining `surface`/`overlay` type uses
(none should remain after Step 1 except possibly in comments) with `layer`.

- [ ] **Step 3: Merge the stack test files**

Combine `overlay_stack_test.go` + `stack_test.go` into `layer_stack_test.go`
(push/pop/top/clear/`layerOf` over the one stack; drop duplicate cases). Delete
the two originals.

- [ ] **Step 4: Build + full race gate**

Run: `cd /mnt/t/others/gg-layer-stack && go build ./... && ./test.sh race`
Expected: all green (unit + e2e).

- [ ] **Step 5: Update docs**

- `CHANGELOG.md` — under a new "Changed" entry: "TUI: unified the popup overlay
  stack and full-screen surface stack into one `layer` stack (internal refactor;
  no behavior change)."
- `CLAUDE.md` — if the `tui` package note names `overlayStack`/`viewStack`,
  update to the single `layer` stack. (Grep first; only edit if present.)

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "refactor(tui): rename to layer* + drop migration shims; docs"
```

---

## Self-Review

- **Spec coverage:** interface merge (T1+T2), stack merge (T2), routing collapse
  (T2), render walk / `menuBackground` removal (T2), typed-lookup re-point (T2),
  `clear*` collapse (T2), accessor + test re-point + rename (T2 shims, T3 clean),
  four slots untouched (all tasks). ✓
- **Placeholder scan:** none — every step names files/lines and shows code. The
  one judgement call (ctrl+c on any top layer) is documented with its rationale
  in T2 Step 4.
- **Type consistency:** `layer`, `layerStack`, `layers`, `topLayer`, `pushLayer`,
  `popLayer`, `clearLayers`, `layerOf[T]` used consistently across tasks.
- **Green at each task:** T1 (signature only), T2 (behavior change behind shims —
  full tui tests), T3 (mechanical rename — race gate). ✓
