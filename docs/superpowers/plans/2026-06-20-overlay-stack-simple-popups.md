# Overlay-Stack Migration — Simple Popups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate the 8 surface-exclusive legacy TUI popups off their dedicated `Model` pointer fields onto the existing `overlayStack`, collapsing each one's dispatch/render/mouse routing into the single `overlayTop()` path.

**Architecture:** Each popup struct gains the two `overlay`-interface methods (`update(m, msg) (Model, tea.Cmd)` and `render(m, below) string`), its `Model` field is deleted, its open-sites become `pushOverlay`, and its three legacy routing checks are removed. A new generic `overlayOf[T]` accessor lets production code and tests reach a live popup by type. Pure structural refactor — no behavior change except one documented mouse-swallow alignment.

**Tech Stack:** Go 1.26, Bubble Tea (Elm-style value-receiver `Model`), the existing `internal/tui/overlay_stack.go` infrastructure.

## Global Constraints

- **No behavior change** beyond the one documented exception below. Keybindings, rendering, op semantics, and popup state machines stay identical.
- **Documented exception (mouse-swallow alignment):** `commitPopup`, `stashPopup`, and `stashAction` are currently absent from the mouse-swallow OR-chain (`mouse.go:64-65`), so today a mouse event while one of them is open falls through to panel hit-testing. After migration they are overlays, and the existing `if m.overlayTop() != nil { return m, nil }` at `mouse.go:38` swallows mouse for every overlay — so these three join the other 7 popups in swallowing mouse. This is intentional and strictly more correct; it is the only behavior delta in this branch.
- **No B1 `running` guard** on any of these 8 popups. Each nils its field *before* calling `startOp` (verified per-popup), so the popup is already off the stack during the async op. (The bookmark/shelf switchers need the guard because they stay visible during their write; these do not.)
- **Background:** overlays render over `below = m.menuBackground()`. For all 8, `menuBackground()` returns exactly `renderInterface()` whenever the popup is visible (they are surface-exclusive), so the background is unchanged.
- **The overlay interface** (already in tree, `overlay_stack.go`): `update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)` and `render(m Model, below string) string`.
- **Per-task verification:** `go build ./cmd/gg` then `./test.sh unit` after each popup; `./test.sh race` before the final merge.
- **Template to copy:** `internal/tui/bookmark_popup.go` (`bookmarkPopup.update` / `bookmarkPopup.render`) — the already-merged reference implementation.

---

## Migration Recipe (the mechanical shape — applies to every popup task)

Every popup task performs the same six transformations. Tasks 3–9 enumerate their popup-specific identifiers; this section is the shape.

For a popup with field `m.X *xType`, source file `x_popup.go`, current handlers `updateXPopupKey(msg) (tea.Model, tea.Cmd)` and `renderXPopup() string`:

**1. Add the two `overlay` methods on `*xType`** (in `x_popup.go`):

```go
// update handles one key while the popup is open. Body is the former
// updateXPopupKey, with: receiver `p` replaces the `p := m.X` local; every
// `m.X = nil` becomes `m = m.popOverlay()`; the return type is Model (not
// tea.Model).
func (p *xType) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
    // ... former body ...
}

// render composites the popup box over the layer beneath.
func (p *xType) render(m Model, below string) string {
    w, h := m.overlayDims()
    return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}
```

`p.box(m)` is the former `renderXPopup` body renamed to a method `func (p *xType) box(m Model) string` (it already returns just the modal box; `overlayCenter` was applied by the old `view.go` caller). Inside `box`, replace the `p := m.X` local with the receiver.

**2. Delete the field** `X *xType` from the `Model` struct in `model.go`.

**3. Convert open-sites** `m.X = &xType{...}` → `m = m.pushOverlay(&xType{...})` (every site, including cross-file ones).

**4. Convert any helper that read `m.X`** (e.g. `popupVisible`, `openAgentPicker`, `startCreateFromPopup`) to take the popup as a parameter or fetch it via `overlayOf[*xType](m)`. Each task names its helpers.

**5. Remove the three legacy routing checks:**
- **dispatch** — delete the `if m.X != nil { return m.updateXPopupKey(msg) }` block in `model.go` `Update`.
- **render** — delete the `if m.X != nil { ... return overlayCenter(bg, m.renderXPopup(), w, h) }` block in `view.go` `render`, and remove `m.X == nil &&` from the tooltip-suppression condition (`view.go:172`).
- **mouse** — if `m.X` appears in the swallow OR-chain (`mouse.go:64-65`), remove it. (`commitPopup`/`stashPopup`/`stashAction` are not in the chain — nothing to remove; they gain swallow via `mouse.go:38`, per the documented exception.)

The single `overlayTop()` checks already present in all three files (`model.go:391`, `view.go:157`, `mouse.go:38`) now route this popup.

**6. Migrate `x_popup_test.go`:**
- assignments `m.X = &xType{...}` (or `newXType(...)`) → `m = m.pushOverlay(&xType{...})`.
- reads `m.X` → `overlayOf[*xType](m)` (nil when closed).
- direct handler calls `m.updateXPopupKey(key)` → route through `m.Update(key)`:
  `tm, cmd := m.Update(key); m = tm.(Model)` (stronger end-to-end coverage).
- render-assertion calls `m.renderXPopup()` → `overlayOf[*xType](m).box(m)` (the box only) or `m.render()` for the full composite.
- "popup closed" assertions `m.X == nil` → `overlayOf[*xType](m) == nil`.

---

## File Structure

| File | Change |
|---|---|
| `internal/tui/overlay_stack.go` | Add the generic `overlayOf[T overlay](m) T` accessor (Task 1). |
| `internal/tui/overlay_stack_test.go` | Add `overlayOf` unit test (Task 1). |
| `internal/tui/model.go` | Delete the 8 fields; remove their 8 dispatch blocks; convert the `commitPopup` open-sites. |
| `internal/tui/view.go` | Remove the 8 render blocks; trim the tooltip-suppression condition. |
| `internal/tui/mouse.go` | Remove `popup`/`repoPopup`/`settings`/`branchPopup`/`pairPopup` from the swallow chain. |
| `internal/tui/commit_popup.go` (+`_test.go`) | Task 2. |
| `internal/tui/repo_popup.go` (+`_test.go`) | Task 3. |
| `internal/tui/settings_popup.go` (+`_test.go`) | Task 4. |
| `internal/tui/branch_popup.go` (+`_test.go`) | Task 5. |
| `internal/tui/pairop_popup.go` (+`_test.go`) | Task 6. |
| `internal/tui/stash_popup.go` (+`_test.go`) | Task 7. |
| `internal/tui/stash_action.go` (+`_test.go`) | Task 8. |
| `internal/tui/worktree_popup.go` (+`_test.go`) | Task 9. |
| `internal/tui/mark.go`, `files_view.go`, `stash_view.go` | Convert cross-file open-sites (Tasks 6/8/9). |
| `CHANGELOG.md` | Note the migration (Task 9). |

---

### Task 1: Generic `overlayOf[T]` accessor

**Files:**
- Modify: `internal/tui/overlay_stack.go`
- Test: `internal/tui/overlay_stack_test.go`

**Interfaces:**
- Produces: `func overlayOf[T overlay](m Model) T` — returns the topmost overlay of concrete type `T`, or the zero value (`nil` for a pointer type) when none is present. Used by every later task in production code and tests.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/overlay_stack_test.go`. It uses an existing overlay type already in the tree (`*bookmarkPastePopup`) plus a second type to prove type discrimination; if a second concrete overlay is inconvenient, assert presence/absence of `*bookmarkPastePopup` only.

```go
func TestOverlayOfReturnsTypedTopOrNil(t *testing.T) {
	var m Model
	// empty stack → nil
	if got := overlayOf[*bookmarkPastePopup](m); got != nil {
		t.Fatalf("empty stack: want nil, got %v", got)
	}
	want := &bookmarkPastePopup{origin: "a.go"}
	m = m.pushOverlay(want)
	if got := overlayOf[*bookmarkPastePopup](m); got != want {
		t.Fatalf("after push: want %p, got %p", want, got)
	}
	// a different concrete type is not matched
	if got := overlayOf[*bookmarkPopup](m); got != nil {
		t.Fatalf("wrong type: want nil, got %v", got)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run TestOverlayOfReturnsTypedTopOrNil`
Expected: FAIL — `undefined: overlayOf`.

- [ ] **Step 3: Implement `overlayOf`**

Add to `internal/tui/overlay_stack.go`:

```go
// overlayOf returns the topmost overlay of concrete type T on the stack, or the
// zero value (nil for a pointer type) when none is present. Lets production code
// and tests reach a live popup by type without a dedicated Model field.
func overlayOf[T overlay](m Model) T {
	var zero T
	if m.overlays == nil {
		return zero
	}
	for i := len(m.overlays.entries) - 1; i >= 0; i-- {
		if p, ok := m.overlays.entries[i].(T); ok {
			return p
		}
	}
	return zero
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/tui/ -run TestOverlayOfReturnsTypedTopOrNil`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/overlay_stack.go internal/tui/overlay_stack_test.go
git commit -m "feat(tui): generic overlayOf[T] accessor for the overlay stack"
```

---

### Task 2: Migrate `commitPopup` (canonical worked example)

**Files:**
- Modify: `internal/tui/commit_popup.go`
- Modify: `internal/tui/model.go` (delete field; remove dispatch block; convert 2 open-sites)
- Modify: `internal/tui/view.go` (remove render block; trim tooltip condition)
- Test: `internal/tui/commit_popup_test.go`

**Interfaces:**
- Consumes: `overlayOf[*commitPopup]`, `pushOverlay`, `popOverlay` (Task 1 + existing).
- Produces: `*commitPopup` implements `overlay`. `applyEditKey`, `message`, `splitMessage` are unchanged and still reused by `rewordPopup` (Branch 2) and the rebase editor.

**Context:** `applyEditKey` (a `*commitPopup` method) is reused by the interactive-rebase editor and by `rewordPopup`; do **not** touch it. Only the `m.commitPopup` field, the `updateCommitPopupKey`/`renderCommitPopup` Model-methods, and the routing change. The amend open-site (`model.go:978`) is an async-message handler — convert it to `pushOverlay` exactly like the keypress open-site.

- [ ] **Step 1: Migrate the test first**

In `internal/tui/commit_popup_test.go`, apply the recipe's test transforms. Representative changes (apply to every occurrence):
- `m.commitPopup = &commitPopup{...}` → `m = m.pushOverlay(&commitPopup{...})`
- a read like `if m.commitPopup.title != ...` → `if overlayOf[*commitPopup](m).title != ...`
- a direct `m2, cmd := m.updateCommitPopupKey(key)` → `tm, cmd := m.Update(key); m := tm.(Model)`
- a closed-assertion `if m.commitPopup != nil` → `if overlayOf[*commitPopup](m) != nil`

- [ ] **Step 2: Run the test to verify it fails to compile**

Run: `go test ./internal/tui/ -run Commit`
Expected: FAIL — `m.commitPopup undefined` / `m.updateCommitPopupKey undefined` once production changes land; at this step it fails to compile because `overlayOf`-based reads reference methods not yet present. (Compile failure is the red state.)

- [ ] **Step 3: Add the `overlay` methods; delete the old Model-methods**

In `internal/tui/commit_popup.go`, replace `updateCommitPopupKey` and `renderCommitPopup` with:

```go
// update handles one key while the commit popup is open. It swallows every key:
// esc cancels, ctrl+c quits, ctrl+s commits.
func (p *commitPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	submit, cancel := p.applyEditKey(msg)
	switch {
	case cancel:
		m = m.popOverlay()
	case submit:
		if strings.TrimSpace(p.title) == "" {
			m.statusMsg = "title required"
			return m, nil
		}
		op := engine.Commit{Message: p.message(), Amend: p.amend}
		m = m.popOverlay()
		return m.startOp(op)
	}
	return m, nil
}

// render composites the commit dialog over the layer beneath.
func (p *commitPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

// box draws the two-field commit dialog (modal box only).
func (p *commitPopup) box(m Model) string {
	var b strings.Builder
	heading := "Commit"
	if p.amend {
		heading = "Amend last commit"
	}
	b.WriteString(heading + "\n\n")
	b.WriteString(renderCommitFields(p))
	b.WriteString("\n[tab] switch field  [enter] newline/next  [ctrl+s] commit  [esc] cancel")

	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	return modalStyle.Width(inner).Render(b.String()) + "\n"
}
```

Note `m.startOp` returns `(tea.Model, tea.Cmd)`; `update` must return `(Model, tea.Cmd)`. Confirm `startOp`'s signature — the bookmark popups already `return m.startOp(...)` from a `(Model, tea.Cmd)` method, so `startOp` returns concrete `Model`. If the compiler reports a `tea.Model` mismatch, `startOp` returns `tea.Model`; in that case write `tm, cmd := m.startOp(op); return tm.(Model), cmd`.

- [ ] **Step 4: Delete the field and routing in `model.go`/`view.go`**

In `internal/tui/model.go`:
- Delete the struct field `commitPopup *commitPopup` (line ~36).
- Delete the dispatch block:
  ```go
  if m.commitPopup != nil {
      return m.updateCommitPopupKey(msg)
  }
  ```
- Convert both open-sites:
  - `m.commitPopup = &commitPopup{}` → `m = m.pushOverlay(&commitPopup{})` (the `c` key, ~line 541)
  - `m.commitPopup = &commitPopup{title: title, desc: desc, amend: true}` → `m = m.pushOverlay(&commitPopup{title: title, desc: desc, amend: true})` (amend msg handler, ~line 978)

In `internal/tui/view.go`:
- Delete the render block:
  ```go
  if m.commitPopup != nil {
      w, h := m.overlayDims()
      return overlayCenter(bg, m.renderCommitPopup(), w, h)
  }
  ```
- Remove `m.commitPopup == nil &&` from the tooltip-suppression condition (`view.go:172`).

(`commitPopup` is not in the `mouse.go` swallow chain — no mouse edit; it gains swallow via `mouse.go:38` per the documented exception.)

- [ ] **Step 5: Build and test**

Run: `go build ./cmd/gg && go test ./internal/tui/ -run Commit`
Expected: build OK; tests PASS.

- [ ] **Step 6: Full unit suite**

Run: `./test.sh unit`
Expected: PASS (catches any cross-file reference you missed).

- [ ] **Step 7: Commit**

```bash
git add internal/tui/commit_popup.go internal/tui/commit_popup_test.go internal/tui/model.go internal/tui/view.go
git commit -m "refactor(tui): migrate commitPopup onto the overlay stack"
```

---

### Task 3: Migrate `repoPopup`

**Files:** `internal/tui/repo_popup.go` (+`_test.go`), `model.go`, `view.go`, `mouse.go`.

**Popup-specific data:**
- Field: `repoPopup *repoPopup` (`model.go:37`).
- Handlers: `updateRepoPopupKey` → `(p *repoPopup) update`; `renderRepoPopup` → `(p *repoPopup) render` + `(p *repoPopup) box(m)`.
- **Helper to convert:** `func (m Model) popupVisible() []repos.Entry` reads `m.repoPopup`. Convert to a method `func (p *repoPopup) visible() []repos.Entry` (drop the `p == nil` guard — the receiver is always non-nil). Update its callers (`update`, `box`) to `p.visible()`.
- Open-site: `repo_popup.go:33` `m.repoPopup = &repoPopup{...}` (inside `openRepoPopup`, which returns `(Model, bool)`) → `m = m.pushOverlay(&repoPopup{...})`.
- nil-sites in `update`: lines 85, 99 → `m = m.popOverlay()`.
- Dispatch block (`model.go`): `if m.repoPopup != nil { return m.updateRepoPopupKey(msg) }`.
- Render block (`view.go`): the `if m.repoPopup != nil { ... renderRepoPopup() ... }` block + the `m.repoPopup == nil &&` clause in the tooltip condition.
- Mouse: remove `m.repoPopup != nil ||` from the `mouse.go:64` OR-chain.

- [ ] **Step 1:** Migrate `repo_popup_test.go` per the recipe (`m.repoPopup` → `overlayOf[*repoPopup](m)`; assignments → `pushOverlay`; direct `updateRepoPopupKey` calls → `m.Update`; `m.popupVisible()` reads → `overlayOf[*repoPopup](m).visible()`).
- [ ] **Step 2:** Run `go test ./internal/tui/ -run Repo` → FAIL (compile).
- [ ] **Step 3:** Add `update`/`render`/`box`/`visible` methods; delete `updateRepoPopupKey`/`renderRepoPopup`/`popupVisible`. Apply the recipe (`m.repoPopup`→`p`, nil→`popOverlay`, return `Model`).
- [ ] **Step 4:** Delete the field + dispatch/render/mouse routing + tooltip clause; convert the open-site.
- [ ] **Step 5:** `go build ./cmd/gg && go test ./internal/tui/ -run Repo` → PASS.
- [ ] **Step 6:** `./test.sh unit` → PASS.
- [ ] **Step 7:** Commit:
```bash
git add internal/tui/repo_popup.go internal/tui/repo_popup_test.go internal/tui/model.go internal/tui/view.go internal/tui/mouse.go
git commit -m "refactor(tui): migrate repoPopup onto the overlay stack"
```

---

### Task 4: Migrate `settings` (`settingsPopup`)

**Files:** `internal/tui/settings_popup.go` (+`_test.go`), `model.go`, `view.go`, `mouse.go`.

**Popup-specific data:**
- Field: `settings *settingsPopup` (`model.go:38`).
- Handlers: `updateSettingsKey` → `(p *settingsPopup) update`; `renderSettingsPopup` → `(p *settingsPopup) render` + `(p *settingsPopup) box(m)`.
- **Helpers to convert:**
  - `func (m Model) openSettings() Model` (`settings_popup.go:28`) sets `m.settings = &settingsPopup{}` → `m = m.pushOverlay(&settingsPopup{})`. This is the open-site; it stays a `func (m Model) ... Model`.
  - `func (m Model) openAgentPicker() Model` (`settings_popup.go:36`) reads `m.settings`. Convert its first line `p := m.settings` → `p := overlayOf[*settingsPopup](m)`. It is called from `update` (former line 83, `return m.openAgentPicker(), nil`) — that call still works.
- nil-sites in `update`: lines 59, 116 → `m = m.popOverlay()`. (Line 55-57 `p.picker = false` is a state change, NOT a close — leave it.)
- Dispatch block: `if m.settings != nil { return m.updateSettingsKey(msg) }`.
- Render block + tooltip clause `m.settings == nil &&`.
- Mouse: remove `m.settings != nil ||` from the `mouse.go:65` OR-chain.

- [ ] **Step 1:** Migrate `settings_popup_test.go` per the recipe.
- [ ] **Step 2:** `go test ./internal/tui/ -run Settings` → FAIL (compile).
- [ ] **Step 3:** Add `update`/`render`/`box`; convert `openAgentPicker` to fetch via `overlayOf`; delete old Model-methods. Recipe applied.
- [ ] **Step 4:** Delete field + routing + tooltip clause; convert the `openSettings` open-site.
- [ ] **Step 5:** `go build ./cmd/gg && go test ./internal/tui/ -run Settings` → PASS.
- [ ] **Step 6:** `./test.sh unit` → PASS.
- [ ] **Step 7:** Commit:
```bash
git add internal/tui/settings_popup.go internal/tui/settings_popup_test.go internal/tui/model.go internal/tui/view.go internal/tui/mouse.go
git commit -m "refactor(tui): migrate settings popup onto the overlay stack"
```

---

### Task 5: Migrate `branchPopup`

**Files:** `internal/tui/branch_popup.go` (+`_test.go`), `model.go`, `view.go`, `mouse.go`.

**Popup-specific data:**
- Field: `branchPopup *branchPopup` (`model.go:44`).
- Handlers: `updateBranchPopupKey` → `(p *branchPopup) update`; `renderBranchPopup` → `(p *branchPopup) render` + `(p *branchPopup) box(m)`. No other helpers read the field.
- Open-site: `branch_popup.go:25` inside `openBranchPopup` (returns `(Model, bool)`) → `m = m.pushOverlay(&branchPopup{...})`.
- nil-sites in `update`: lines 38, 47 → `m = m.popOverlay()`.
- Routing: dispatch `if m.branchPopup != nil {...}`; render block + tooltip clause `m.branchPopup == nil &&`; mouse — remove `m.branchPopup != nil ||` from the `mouse.go:65` chain.

- [ ] **Step 1:** Migrate `branch_popup_test.go` per the recipe.
- [ ] **Step 2:** `go test ./internal/tui/ -run Branch` → FAIL (compile).
- [ ] **Step 3:** Add `update`/`render`/`box`; delete old Model-methods. Recipe applied.
- [ ] **Step 4:** Delete field + routing + tooltip clause; convert the open-site.
- [ ] **Step 5:** `go build ./cmd/gg && go test ./internal/tui/ -run Branch` → PASS.
- [ ] **Step 6:** `./test.sh unit` → PASS.
- [ ] **Step 7:** Commit:
```bash
git add internal/tui/branch_popup.go internal/tui/branch_popup_test.go internal/tui/model.go internal/tui/view.go internal/tui/mouse.go
git commit -m "refactor(tui): migrate branchPopup onto the overlay stack"
```

---

### Task 6: Migrate `pairPopup` (`pairOpPopup`)

**Files:** `internal/tui/pairop_popup.go` (+`_test.go`), `internal/tui/mark.go` (open-site), `model.go`, `view.go`, `mouse.go`.

**Popup-specific data:**
- Field: `pairPopup *pairOpPopup` (`model.go:53`).
- Handlers: `updatePairPopupKey` → `(p *pairOpPopup) update`; `renderPairOpPopup` → `(p *pairOpPopup) render` + `(p *pairOpPopup) box(m)`. No helper reads the field.
- Open-site is cross-file: `mark.go:95` `m.pairPopup = &pairOpPopup{...}` → `m = m.pushOverlay(&pairOpPopup{...})`.
- nil-sites in `update`: lines 41, 57 → `m = m.popOverlay()`.
- **Return-type note:** the enter branch ends with `return op.open(m, marked, selected)` or `return m.startOp(...)`. `pairOp.open` is already `func(Model, string, string) (Model, tea.Cmd)` (`mark.go:25`), so both return `(Model, tea.Cmd)` — no friction.
- Routing: dispatch `if m.pairPopup != nil {...}`; render block + tooltip clause; mouse — remove `m.pairPopup != nil` from the `mouse.go:65` chain (it is the last term; fix the trailing `||`).

- [ ] **Step 1:** Migrate `pairop_popup_test.go` per the recipe.
- [ ] **Step 2:** `go test ./internal/tui/ -run Pair` → FAIL (compile).
- [ ] **Step 3:** Add `update`/`render`/`box`; delete old Model-methods. Recipe applied.
- [ ] **Step 4:** Delete field + routing + tooltip clause; convert the `mark.go` open-site.
- [ ] **Step 5:** `go build ./cmd/gg && go test ./internal/tui/ -run Pair` → PASS.
- [ ] **Step 6:** `./test.sh unit` → PASS.
- [ ] **Step 7:** Commit:
```bash
git add internal/tui/pairop_popup.go internal/tui/pairop_popup_test.go internal/tui/mark.go internal/tui/model.go internal/tui/view.go internal/tui/mouse.go
git commit -m "refactor(tui): migrate pairPopup onto the overlay stack"
```

---

### Task 7: Migrate `stashPopup`

**Files:** `internal/tui/stash_popup.go` (+`_test.go`), `model.go`, `view.go`.

**Popup-specific data:**
- Field: `stashPopup *stashPopup` (`model.go:54`).
- Handlers: `updateStashPopupKey` → `(p *stashPopup) update`; `renderStashPopup` → `(p *stashPopup) render` + `(p *stashPopup) box(m)`. The `op()` method (`stash_popup.go:30`) is already on `*stashPopup` — unchanged. `stashCandidates` is a free function — unchanged.
- Open-site: `stash_popup.go:72` inside `openStashPopup` (returns `(Model, bool)`) → `m = m.pushOverlay(&stashPopup{...})`.
- nil-sites in `update`: lines 85, 99 → `m = m.popOverlay()`.
- Routing: dispatch `if m.stashPopup != nil {...}`; render block + tooltip clause. **Mouse:** `stashPopup` is NOT in the swallow chain — no `mouse.go` edit; it gains swallow via `mouse.go:38` (documented exception).

- [ ] **Step 1:** Migrate `stash_popup_test.go` per the recipe.
- [ ] **Step 2:** `go test ./internal/tui/ -run Stash` → FAIL (compile). (`-run Stash` also runs stash_action/stash_view tests — fine.)
- [ ] **Step 3:** Add `update`/`render`/`box`; delete old Model-methods. Recipe applied.
- [ ] **Step 4:** Delete field + dispatch/render routing + tooltip clause; convert the open-site.
- [ ] **Step 5:** `go build ./cmd/gg && go test ./internal/tui/ -run Stash` → PASS.
- [ ] **Step 6:** `./test.sh unit` → PASS.
- [ ] **Step 7:** Commit:
```bash
git add internal/tui/stash_popup.go internal/tui/stash_popup_test.go internal/tui/model.go internal/tui/view.go
git commit -m "refactor(tui): migrate stashPopup onto the overlay stack"
```

---

### Task 8: Migrate `stashAction` (`stashActionPopup`)

**Files:** `internal/tui/stash_action.go` (+`_test.go`), `internal/tui/files_view.go` + `internal/tui/stash_view.go` (open-sites), `model.go`, `view.go`.

**Popup-specific data:**
- Field: `stashAction *stashActionPopup` (`model.go:55`).
- Handlers: `updateStashActionKey` → `(a *stashActionPopup) update`; `renderStashActionPopup` → `(a *stashActionPopup) render` + `(a *stashActionPopup) box(m)`. (The existing local is named `a`, not `p` — keep `a` as the receiver name for a minimal diff.)
- Open-sites are cross-file (both `m.stashAction = &stashActionPopup{ref: e.Ref, subject: e.Subject}`):
  - `files_view.go:211` → `m = m.pushOverlay(&stashActionPopup{...})`
  - `stash_view.go:143` → `m = m.pushOverlay(&stashActionPopup{...})`
- nil-sites in `update`: lines 32, 57, 70, 73 → `m = m.popOverlay()`.
- Routing: dispatch `if m.stashAction != nil {...}`; render block + tooltip clause. **Mouse:** not in the swallow chain — no `mouse.go` edit; gains swallow via `mouse.go:38` (documented exception).
- **Surface-exclusivity note (already established):** `stashAction` opens from inside the `filesView`/`stashView` column views, which are drawn within `renderInterface()` and are not `stackTop` surfaces, so `menuBackground()` still draws them under the popup.

- [ ] **Step 1:** Migrate `stash_action_test.go` per the recipe (`m.stashAction` → `overlayOf[*stashActionPopup](m)`).
- [ ] **Step 2:** `go test ./internal/tui/ -run StashAction` → FAIL (compile).
- [ ] **Step 3:** Add `update`/`render`/`box` (receiver `a`); delete old Model-methods. Recipe applied.
- [ ] **Step 4:** Delete field + dispatch/render routing + tooltip clause; convert both cross-file open-sites.
- [ ] **Step 5:** `go build ./cmd/gg && go test ./internal/tui/ -run Stash` → PASS.
- [ ] **Step 6:** `./test.sh unit` → PASS.
- [ ] **Step 7:** Commit:
```bash
git add internal/tui/stash_action.go internal/tui/stash_action_test.go internal/tui/files_view.go internal/tui/stash_view.go internal/tui/model.go internal/tui/view.go
git commit -m "refactor(tui): migrate stashAction onto the overlay stack"
```

---

### Task 9: Migrate `popup` (worktree) + changelog

**Files:** `internal/tui/worktree_popup.go` (+`_test.go`), `model.go`, `view.go`, `mouse.go`, `CHANGELOG.md`.

**Popup-specific data (the largest — most state):**
- Field: `popup *worktreePopup` (`model.go:35`).
- Handlers: `updatePopupKey` → `(p *worktreePopup) update`; `renderWorktreePopup` → `(p *worktreePopup) render` + `(p *worktreePopup) box(m)`.
- **Methods already on `*worktreePopup`** (`tctx`, `fixedBranch`, `recompute`, `createOp`, `consumedSeqNames`) — unchanged.
- **Helper to convert:** `func (m Model) startCreateFromPopup(switchAfter bool) (tea.Model, tea.Cmd)` (`worktree_popup.go:213`) reads `m.popup` (line 214) and nils it (line 221). It is called from `update` (former lines 202, 204). Convert to take the popup explicitly:
  - Signature → `func (m Model) startCreateFromPopup(p *worktreePopup, switchAfter bool) (Model, tea.Cmd)`.
  - Remove its `p := m.popup` line; use the parameter.
  - Its nil-site (line 221) → `m = m.popOverlay()`.
  - Callers in `update`: `return m.startCreateFromPopup(false)` → `return m.startCreateFromPopup(p, false)`; `return m.startCreateFromPopup(true)` → `return m.startCreateFromPopup(p, true)`.
- Open-site: `worktree_popup.go:130` `m.popup = p` (end of `openWorktreePopup`, returns `(Model, bool)`) → `m = m.pushOverlay(p)`.
- nil-sites in `update`: lines 146, 193 → `m = m.popOverlay()`.
- Routing: dispatch `if m.popup != nil { return m.updatePopupKey(msg) }`; render block + the leading `m.popup == nil &&` in the tooltip condition; mouse — remove `m.popup != nil ||` from the `mouse.go:64` chain (it is the first term).

- [ ] **Step 1:** Migrate `worktree_popup_test.go` per the recipe (assignments → `pushOverlay`; `m.popup` reads → `overlayOf[*worktreePopup](m)`; direct `updatePopupKey`/`startCreateFromPopup` calls updated to the new signatures / routed through `m.Update`).
- [ ] **Step 2:** `go test ./internal/tui/ -run Worktree` → FAIL (compile).
- [ ] **Step 3:** Add `update`/`render`/`box`; convert `startCreateFromPopup` signature + body; delete old Model-methods. Recipe applied.
- [ ] **Step 4:** Delete field + dispatch/render/mouse routing + tooltip clause; convert the open-site.
- [ ] **Step 5:** `go build ./cmd/gg && go test ./internal/tui/ -run Worktree` → PASS.
- [ ] **Step 6:** Full race suite (last popup — verify the whole branch):

Run: `./test.sh race`
Expected: PASS.

- [ ] **Step 7:** Update `CHANGELOG.md`

Add under the unreleased/Changed section:
```
- Internal: migrated the worktree, commit, repo, settings, branch, pair-op,
  stash, and stash-action popups onto the unified overlay stack (no
  user-facing change; commit/stash/stash-action popups now also swallow mouse
  like the other dialogs).
```

- [ ] **Step 8:** Commit

```bash
git add internal/tui/worktree_popup.go internal/tui/worktree_popup_test.go internal/tui/model.go internal/tui/view.go internal/tui/mouse.go CHANGELOG.md
git commit -m "refactor(tui): migrate worktree popup onto the overlay stack; changelog"
```

---

## Post-migration state (for the final whole-branch review)

After Task 9:
- The `Model` struct has 8 fewer popup fields. Remaining legacy popup fields: `renameBranchPopup`, `rewordPopup`, `contentPopup`, `conflictPopup` (+ `modal`, `actionMenu`, and the column views `filesView`/`stashView`) — all Branch 2 / out of scope.
- `model.go` dispatch: 8 `if m.X != nil` blocks removed; the 4 deferred popup blocks remain, plus the already-separate `modal`/`actionMenu`/cheat-sheet/`overlayTop()` checks.
- `view.go` render: 8 blocks removed; tooltip-suppression condition trimmed to the 4 remaining fields.
- `mouse.go`: swallow chain trimmed to `renameBranchPopup`/`rewordPopup`; `contentPopup` wheel block unchanged.
- One documented behavior delta: `commitPopup`/`stashPopup`/`stashAction` now swallow mouse.

## Out of scope

- `rewordPopup`, `renameBranchPopup`, `contentPopup`, `conflictPopup`, `actionMenu`, `modal` (Branch 2).
- Unifying `overlayStack` + `viewStack` (later branch).
