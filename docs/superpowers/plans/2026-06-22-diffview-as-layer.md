# diffView-as-layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Promote the standalone full-screen diff from the `m.diffView` `Model` field onto the single layer stack, so one structure owns its z-order/keyboard/mouse/render/return.

**Architecture:** `*diffView` gains `update`/`render` so it satisfies the `layer` interface; its big key-handler body is reused unchanged via a thin delegating wrapper. Read-sites go through an idiomatic `m.diffLayer()` accessor (a `layerOf[*diffView]` wrapper, like `bookmarkSwitcher()`); a single accessor flip + open/close-site conversion moves the diff onto the stack and deletes the field. Single-focus surface (lockstep scroll) — no tiling/pane machinery.

**Tech Stack:** Go 1.26, Bubble Tea (Elm value-receiver `Model`), the existing `internal/tui` layer stack.

## Global Constraints

- `internal/tui` MUST NOT import `internal/git` (archtest-guarded). No new git access here.
- `Model` is a value receiver; stack state persists via the `*layerStack` pointer field. Never store per-window state on `Model` by value when it must survive a copy.
- A diff handles `esc` only when it is the **top** layer (dispatch routes to it via `topLayer()`), so `popLayer()` is correct there. A diff may **not** be top on resize/repo-switch/`diffMsg` — use `m.diffLayer()` / `removeLayer` there, never `popLayer`.
- `historyView.diff` (`history_view.go:34`) is the diff TYPE reused as history's embedded pane — **do not touch it**. This plan promotes only the standalone `m.diffView` instance.
- Keep `diffTag`, `diffNav`, `diffPartial`, `diffLong` on `Model` (async-gate + session state). `diffNav` stays.
- Run `./test.sh` (vet+gofmt → unit → e2e) after each task; `./test.sh race` before the final commit.

---

### Task 1: `*diffView` satisfies `layer` (no behavior change)

**Files:**
- Modify: `internal/tui/diff_view.go` (add two methods near `updateDiffViewKey`, ~`:577`)
- Modify: `internal/tui/layer_stack.go:22-28` (`isFullScreenLayer`)
- Test: `internal/tui/diff_layer_test.go` (new)

**Interfaces:**
- Consumes: existing `func (m Model) updateDiffViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd)`, `func (m Model) renderDiffView() string`, the `layer` interface.
- Produces: `func (v *diffView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)`, `func (v *diffView) render(m Model, below string) string`. `isFullScreenLayer(&diffView{}) == true`.

- [ ] **Step 1: Write the failing test**

```go
// internal/tui/diff_layer_test.go
package tui

import "testing"

// Compile-time: *diffView must satisfy the layer interface.
var _ layer = (*diffView)(nil)

func TestDiffViewIsFullScreenLayer(t *testing.T) {
	if !isFullScreenLayer(&diffView{}) {
		t.Fatal("a diffView must be a full-screen surface so it folds into a popup backdrop")
	}
}
```

- [ ] **Step 2: Run it — expect compile failure**

Run: `go test ./internal/tui/ -run TestDiffViewIsFullScreenLayer`
Expected: build error — `*diffView does not implement layer (missing update/render)`, and `isFullScreenLayer` returns false.

- [ ] **Step 3: Add the wrapper methods**

In `internal/tui/diff_view.go`, immediately above `func (m Model) updateDiffViewKey`:

```go
// update lets a diffView live on the layer stack: it delegates to the existing
// Model-side key handler (which finds this diff via m.diffLayer()) and adapts the
// tea.Model return to the layer interface's Model.
func (v *diffView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	nm, cmd := m.updateDiffViewKey(msg)
	return nm.(Model), cmd
}

// render draws the full-screen diff. Like the other surfaces it owns the screen
// and ignores the backdrop.
func (v *diffView) render(m Model, below string) string {
	return m.renderDiffView()
}
```

- [ ] **Step 4: Add `*diffView` to `isFullScreenLayer`**

`internal/tui/layer_stack.go`:

```go
func isFullScreenLayer(l layer) bool {
	switch l.(type) {
	case *historyView, *blameView, *irebaseEditor, *hunkPicker, *diffView:
		return true
	}
	return false
}
```

- [ ] **Step 5: Run the test + package**

Run: `go test ./internal/tui/ -run TestDiffViewIsFullScreenLayer` → PASS.
Run: `go test ./internal/tui/` → PASS (methods are unused by production yet; field still drives everything).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/diff_view.go internal/tui/layer_stack.go internal/tui/diff_layer_test.go
git commit -m "refactor(tui): diffView satisfies the layer interface (unused yet)"
```

---

### Task 2: Route every diff READ through `m.diffLayer()` (pure refactor)

Insulates the ~15 read-sites from Task 3's flip. Writes (`m.diffView = …`) stay as the field for now.

**Files:**
- Modify: `internal/tui/layer_stack.go` (add accessor near `bookmarkSwitcher`/`shelfSwitcher`, ~`:85`)
- Modify (read-sites → `m.diffLayer()`): `internal/tui/bookmark.go:30`, `internal/tui/action_menu.go:54,255,282`, `internal/tui/diff_render.go:126,134,153`, `internal/tui/mouse.go:43-45`, `internal/tui/view.go:136,199`, `internal/tui/model.go:193,201,216,220-221`
- Test: existing suite (no behavior change).

**Interfaces:**
- Produces: `func (m Model) diffLayer() *diffView` — for Task 2 its body returns the field; Task 3 flips it to `layerOf`.

- [ ] **Step 1: Add the accessor (body returns the field for now)**

`internal/tui/layer_stack.go`, beside the other switchers:

```go
// diffLayer returns the open standalone diff, else nil. (Body flips to
// layerOf[*diffView] when the diff moves onto the stack in the next task.)
func (m Model) diffLayer() *diffView { return m.diffView }
```

- [ ] **Step 2: Mechanically convert every READ of `m.diffView` to `m.diffLayer()`**

Leave assignments (`m.diffView = …`, `m.diffView = nil`) untouched — those convert in Task 3. Convert reads only, e.g.:
- `if v := m.diffView; v != nil {` → `if v := m.diffLayer(); v != nil {`
- `if m.diffView != nil {` → `if m.diffLayer() != nil {`
- `if m.diffView == nil || msg.tag != m.diffTag {` → `if m.diffLayer() == nil || msg.tag != m.diffTag {`
- `v := m.diffView` (relayout branch, `model.go:201`) → `v := m.diffLayer()`
- the `frontIsFilesView := !onStackFile && m.diffView == nil` (`action_menu.go:54`) → `… && m.diffLayer() == nil` (semantics preserved exactly).

Update the two comment mentions in `history_view.go:103,306` (`m.diffView` → "the standalone diff layer") while here.

- [ ] **Step 3: Verify no stray reads remain & it builds**

Run: `grep -n "m\.diffView" internal/tui/*.go | grep -v "_test" | grep -v "m\.diffView = " | grep -v "func (m Model) diffLayer"`
Expected: no lines (every remaining `m.diffView` is an assignment or the accessor body).
Run: `go build ./...` → success.

- [ ] **Step 4: Run the suite**

Run: `./test.sh unit`
Expected: PASS — pure refactor, no behavior change.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/
git commit -m "refactor(tui): read the diff through m.diffLayer() accessor"
```

---

### Task 3: The flip — push the diff onto the stack and delete the field

Atomic compile-unit change: flip the accessor to the stack, convert open/close/async sites, remove the diff's bespoke rungs, delete the field.

**Files:**
- Modify: `internal/tui/layer_stack.go` (flip `diffLayer` body; add `removeLayer`)
- Modify: `internal/tui/bookmark_popup.go:436-441` (`openPickerDiff`)
- Modify: `internal/tui/diff_view.go:380` (`openStatusDiff`), `:603-605` (`esc`)
- Modify: `internal/tui/files_view.go:426` (file-tree `enter`)
- Modify: `internal/tui/model.go:76` (delete field), `:193-197` (narrow-close), `:216-221` (`diffMsg`), `:497` (remove keyboard rung), `:1769` (repo-switch)
- Modify: `internal/tui/mouse.go:35-48` (add `*diffView` to topLayer branch; remove diff rung)
- Modify: `internal/tui/view.go:135-138` (drop `layerBase` diff case), `:199` (drop `|| m.diffView != nil`)
- Test: `internal/tui/diff_layer_flip_test.go` (new)

**Interfaces:**
- Consumes: `pushLayer`, `popLayer`, `m.diffLayer()`, `layerOf[*diffView]`, `m.diffTag`.
- Produces: `func (m Model) removeLayer(l layer) Model` (removes the matching entry wherever it sits). No more `m.diffView` field.

- [ ] **Step 1: Write the failing behavior tests**

```go
// internal/tui/diff_layer_flip_test.go
package tui

import (
	"testing"
	tea "github.com/charmbracelet/bubbletea"
)

// esc from a picker-opened compare diff returns to the picker beneath it.
func TestEscFromPickerDiffReturnsToPicker(t *testing.T) {
	m := loadedModelLinearCommits(t, 2) // existing helper; a repo with ≥2 commits
	m, _ = m.openBookmarkSwitcher()
	if m.bookmarkSwitcher() == nil {
		t.Fatal("precondition: bookmark switcher should be open")
	}
	// Open a compare diff from the switcher (pushes the diff over the switcher).
	v := &diffView{title: "a ↔ b", loading: true}
	m, _ = m.openPickerDiff(v, "test:diff", nil)
	if m.diffLayer() == nil {
		t.Fatal("diff should be on the stack after openPickerDiff")
	}
	// esc closes the diff and reveals the switcher, still live.
	m2, _ := m.diffLayer().update(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = m2
	if m.diffLayer() != nil {
		t.Fatal("esc should pop the diff")
	}
	if m.bookmarkSwitcher() == nil {
		t.Fatal("esc from a picker diff must return to the picker, not base")
	}
}

// diffMsg populates the on-stack diff in place even when it is not the top layer.
func TestDiffMsgPopulatesInPlaceUnderOverlay(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	v := &diffView{title: "f", loading: true}
	m, _ = m.openPickerDiff(v, "tag1", nil)
	m, _ = m.openBookmarkSwitcher() // push a popup OVER the loading diff
	loaded := &diffView{title: "f", loading: true, lines: []textdiff.Line{{}}}
	nm, _ := m.Update(diffMsg{tag: "tag1", view: loaded})
	m = nm.(Model)
	dv := m.diffLayer()
	if dv == nil || dv.loading {
		t.Fatal("diffMsg must clear loading on the on-stack diff")
	}
	if m.bookmarkSwitcher() == nil {
		t.Fatal("the overlay popup must remain on top after diffMsg")
	}
}
```

(Adjust `loadedModel`/`twoCommitsFixture` to the actual helpers in `*_test.go`; add the `textdiff` import as used.)

- [ ] **Step 2: Run — expect failure**

Run: `go test ./internal/tui/ -run 'TestEscFromPickerDiff|TestDiffMsgPopulates'`
Expected: FAIL/compile error (openPickerDiff still uses the field; diffMsg replaces wholesale; field still present).

- [ ] **Step 3: Add `removeLayer` and flip `diffLayer`**

`internal/tui/layer_stack.go`:

```go
func (m Model) diffLayer() *diffView { return layerOf[*diffView](m) }

// removeLayer drops the first matching entry wherever it sits (not only the top).
// Used to close a window that may have a popup above it (e.g. the diff on resize).
func (m Model) removeLayer(target layer) Model {
	if m.layers == nil {
		return m
	}
	for i, l := range m.layers.entries {
		if l == target {
			m.layers.entries = append(m.layers.entries[:i], m.layers.entries[i+1:]...)
			break
		}
	}
	return m
}
```

- [ ] **Step 4: Convert the open sites to `pushLayer`**

`bookmark_popup.go` `openPickerDiff`:

```go
func (m Model) openPickerDiff(v *diffView, tag string, load tea.Cmd) (Model, tea.Cmd) {
	m = m.pushLayer(v)
	m.diffTag = tag
	m.diffNav = diffNavNone
	return m, load
}
```

`diff_view.go:380` (`openStatusDiff`) and `files_view.go:426` (file-tree `enter`): replace `m.diffView = &diffView{…}` with `m = m.pushLayer(&diffView{…})`. Where the old code then set `m.diffView.context = …` (e.g. `files_view.go:440,445`), build the struct with those fields first, then `pushLayer`, OR mutate the returned pointer via `m.diffLayer()` after the push.

- [ ] **Step 5: `diffMsg` in place; `esc`/close → pop/remove**

`model.go` diffMsg handler:

```go
case diffMsg:
	dv := m.diffLayer()
	if dv == nil || msg.tag != m.diffTag {
		return m, nil // closed, or stale
	}
	*dv = *msg.view
	dv.loading = false
	return m, nil
```

`diff_view.go:603` esc:

```go
case "esc":
	m = m.popLayer()
	m.diffTag = ""
	return m, nil
```

`model.go:193-197` narrow-close and `:1769` repo-switch:

```go
// narrow-close (≤ replaces m.diffView = nil; diff may sit under a popup on resize)
if dv := m.diffLayer(); dv != nil && msg.Width > 0 {
	if msg.Width < 60 {
		m = m.removeLayer(dv)
		m.diffTag = ""
		m.statusMsg = "diff closed: terminal too narrow"
	} else {
		v := dv
		// …existing relayout body unchanged…
	}
}
// repo-switch (model.go:1769)
if dv := m.diffLayer(); dv != nil {
	m = m.removeLayer(dv)
}
```

- [ ] **Step 6: Remove the bespoke rungs and the field**

- `model.go:497`: delete the whole `if m.diffView != nil { return m.updateDiffViewKey(msg) }` keyboard rung (the diff now routes via `topLayer()` at `:491`).
- `mouse.go`: in the `topLayer()` branch (`:35-40`) add a diff case; delete the standalone diff rung (`:43-48`):

```go
if l := m.topLayer(); l != nil {
	if cp, ok := l.(*contentPopup); ok && wheel != 0 {
		cp.move(wheel)
	}
	if dv, ok := l.(*diffView); ok && wheel != 0 {
		dv.scrollBy(wheel, m.diffBodyRows())
	}
	return m, nil
}
```

- `view.go:135-138`: `layerBase` becomes `func (m Model) layerBase() string { return m.renderInterface() }`.
- `view.go:199`: drop `|| m.diffView != nil` (an open diff is now a stack entry, covered by the `len(entries) > 0` check).
- `model.go:76`: delete the `diffView *diffView` field.

- [ ] **Step 7: Build, vet, run the suite**

Run: `go build ./... && grep -n "m\.diffView" internal/tui/*.go | grep -v _test`
Expected: build success; the only remaining `m.diffView` references are none (field gone).
Run: `./test.sh unit` → PASS, including the two new tests.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/
git commit -m "refactor(tui): move the standalone diff onto the layer stack

esc from a picker-opened diff now returns to its picker; retires the diff's
clearLayers/layerBase special-case and its keyboard+mouse rungs. diffMsg
populates the on-stack diff in place (it may not be top)."
```

---

### Task 4: Mouse-wheel coverage + docs + race

**Files:**
- Test: `internal/tui/diff_layer_flip_test.go` (add wheel test)
- Modify: `CHANGELOG.md`; `docs/superpowers/specs/2026-06-22-windowing-zorder-root-cause.md` (fix contradiction-#1 framing)

- [ ] **Step 1: Wheel test**

```go
func TestDiffWheelRoutesToTopLayer(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	v := &diffView{title: "f"}
	for i := 0; i < 50; i++ { // give it scrollable content
		v.disp = append(v.disp, dRow{})
	}
	m = m.pushLayer(v)
	before := v.offset
	nm, _ := m.Update(tea.MouseMsg{Type: tea.MouseWheelDown})
	_ = nm
	if v.offset == before {
		t.Fatal("wheel over a diff layer should scroll the diff")
	}
}
```

Run: `go test ./internal/tui/ -run TestDiffWheelRoutesToTopLayer` → PASS. (Adjust `dRow{}`/scroll setup to the real fields if needed.)

- [ ] **Step 2: CHANGELOG**

Add under the current unreleased section:

```markdown
### Changed
- TUI: the full-screen diff is now a member of the layer stack. `esc` from a
  diff opened over history or a bookmark/shelf picker returns to that surface
  instead of the base layout; the diff's `.` menu and mouse wheel always target
  the diff. Retires the internal `clearLayers` workaround for diffs.
```

- [ ] **Step 3: Correct the z-order doc**

In `docs/superpowers/specs/2026-06-22-windowing-zorder-root-cause.md`, replace "contradiction #1" (claims render draws diffView in front of the stack) with the accurate framing: the diff is `layerBase` — the backdrop the stack composites over (`view.go` `layerBase`), so render AGREES with keyboard/mouse (stack in front of diff); the real root cause is the diff being a base **outside** the stack, which is what this change fixes. Keep contradiction #2 (actionMenu vs stack, mouse vs keyboard/render).

- [ ] **Step 4: Race + commit**

Run: `./test.sh race`
Expected: PASS.

```bash
git add CHANGELOG.md docs/ internal/tui/
git commit -m "docs+test(tui): diff-as-layer changelog, wheel test, z-order doc fix"
```

---

## Self-Review

**Spec coverage:** `*diffView` implements `layer` (T1) ✓; field dropped, reads via `layerOf` (T2/T3) ✓; `layerBase` special-case removed + added to `isFullScreenLayer` (T1/T3) ✓; `openPickerDiff`→`pushLayer` seam + 2 direct installers (T3) ✓; `diffMsg` in place (T3) ✓; `esc`→`popLayer`, resize/repo→`removeLayer` (T3) ✓; keep `diffNav` (Global Constraints) ✓; mouse rung (T3) + wheel test (T4) ✓; esc-to-picker behavior + test (T3) ✓; untouched `filesView`/`stashView`/`historyView.diff`/identity `clearLayers` (Global Constraints + not in any task) ✓; CHANGELOG + doc fix (T4) ✓. **Note vs spec:** `frontIsFilesView` needed no rederivation — the `m.diffLayer()` accessor swap preserves its semantics (diff-open ⟺ diff-on-stack), so it folds into T2's mechanical swap rather than its own task.

**Placeholder scan:** none — every code step shows the code; test-helper names (`loadedModel`/`twoCommitsFixture`) are flagged to match the real helpers.

**Type consistency:** `diffLayer() *diffView`, `removeLayer(l layer) Model`, `update(m Model, msg) (Model, tea.Cmd)`, `render(m Model, below string) string` consistent across tasks; `pushLayer`/`popLayer` signatures match `layer_stack.go`.
