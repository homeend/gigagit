# Overlay-Stack — Remaining Popups (Stage 2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Migrate the last three plain centered popups — `renameBranchPopup`, `rewordPopup`, `contentPopup` (help + `?` cheat-sheet) — onto the existing `overlayStack`, finishing the popup migration. Migrating `contentPopup` collapses the special `?`-cheat-sheet gate.

**Architecture:** Identical recipe to the merged 8-popup migration ([[overlay-stack-simple-popups-feature]], spec `docs/superpowers/specs/2026-06-20-overlay-stack-simple-popups-design.md`): each struct implements `overlay` (`update(m,msg)(Model,tea.Cmd)` + `render(m,below)string` = `overlayCenter(clipToHeight(below,h), p.box(m), w, h)`); delete its `Model` field; open-sites → `m.pushOverlay(...)`; the three routing checks (dispatch/render/mouse) collapse into the single `overlayTop()` path; reads/tests via `overlayOf[*T]`.

**Tech Stack:** Go 1.26, Bubble Tea. `overlayOf[T]` + `pushOverlay`/`popOverlay`/`overlayTop` already exist on `main`.

## Global Constraints

- **No behavior change.** Keybindings, rendering, op semantics identical.
- All three are **surface-exclusive** (rename/reword are offered only outside content windows — `availableActions` returns early when `inContentWindow()`; the help/cheat-sheet `?` is panel/switcher-context) and **pop before their op**, so **no B1 `running` guard**.
- `below = m.menuBackground()` (== `renderInterface()` when no surface is up; == the dimmed background the cheat-sheet already uses) — behavior-preserving.
- **`filesView` is a SEPARATE `*contentPopup` field** rendered as a left column; it does NOT use `updateContentPopupKey`/`renderContentPopup` (verified) and is **left untouched**. Only the centered `m.contentPopup` migrates.
- Per popup: `go build ./cmd/gg` + `./test.sh unit`; `./test.sh race` before merge.

## Task 1: `renameBranchPopup`

**Files:** `internal/tui/rename_branch_popup.go` (+`_test.go`), `model.go`, `view.go`, `mouse.go`.

- Handlers `updateRenameBranchPopupKey`→`(p *renameBranchPopup) update`, `renderRenameBranchPopup`→`render`+`box`.
- Open-site `rename_branch_popup.go:26` `m.renameBranchPopup = &renameBranchPopup{...}` → `pushOverlay`.
- nil-sites (59, 62, 66) → `m = m.popOverlay()`; the create path: `m.renameBranchPopup = nil` (66) then `return m.startOp(op)` (67) — pop before op.
- Routing: dispatch (model.go:408), render (view.go:186), mouse (in the `mouse.go:68` OR-chain `m.renameBranchPopup != nil || m.rewordPopup != nil` — drop this term).

- [ ] **Step 1:** Migrate `rename_branch_popup_test.go` (+ grep `m.renameBranchPopup` across `internal/tui/*_test.go` and migrate every fixture): assignments → `pushOverlay`, reads → `overlayOf[*renameBranchPopup](m)`, direct `updateRenameBranchPopupKey` calls → `m.Update`.
- [ ] **Step 2:** `go test ./internal/tui/ -run RenameBranch` → FAIL (compile).
- [ ] **Step 3:** Add `update`/`render`/`box` methods (recipe); delete the Model-methods + field.
- [ ] **Step 4:** Convert open-site + remove the 3 routing checks.
- [ ] **Step 5:** `go build ./cmd/gg && ./test.sh unit` → PASS.
- [ ] **Step 6:** Commit `refactor(tui): migrate renameBranchPopup onto the overlay stack`.

## Task 2: `rewordPopup`

**Files:** `internal/tui/reword_popup.go` (+`_test.go`), `model.go`, `view.go`, `mouse.go`.

- Handlers `updateRewordPopupKey`→`update`, `renderRewordPopup`→`render`+`box`.
- **`rewordPopup` embeds a `commitPopup` value** (`reword_popup.go:39` `popup: commitPopup{...}`); its `update` drives `rp.popup.applyEditKey` — leave that intact.
- Open-site `reword_popup.go:39` (inside `openRewordPopup`) → `pushOverlay`.
- nil-sites (71, 78) → `popOverlay`; create path: nil@78 → `startOp`@79 (pop before op).
- Routing: dispatch (model.go:411), render (view.go:190), mouse (the `mouse.go:68` OR-chain `m.rewordPopup != nil` — drop, with rename also dropped the chain becomes empty: delete the whole `if` block).

- [ ] **Step 1:** Migrate `reword_popup_test.go` (+ grep other test fixtures) per the recipe.
- [ ] **Step 2:** `go test ./internal/tui/ -run Reword` → FAIL (compile).
- [ ] **Step 3:** Add `update`/`render`/`box`; delete Model-methods + field.
- [ ] **Step 4:** Convert open-site + remove the 3 routing checks (the mouse OR-chain is now empty — remove the whole block).
- [ ] **Step 5:** `go build ./cmd/gg && ./test.sh unit` → PASS.
- [ ] **Step 6:** Commit `refactor(tui): migrate rewordPopup onto the overlay stack`.

## Task 3: `contentPopup` (help + `?` cheat-sheet) — collapses the gate

**Files:** `internal/tui/content_popup.go` (+`_test.go`), `model.go`, `view.go`, `mouse.go`, `bookmark_popup.go`, `shelf_popup.go`, `CHANGELOG.md`.

- Handlers `updateContentPopupKey`→`update` (preserve the `p.typing` /-filter sub-mode), `renderContentPopup`→`render`+`box`. Keep the existing `(p *contentPopup)` methods `visible`/`move` (used by `filesView` too).
- **Delete only `m.contentPopup`**; KEEP `m.filesView *contentPopup` (the column tree — untouched).
- Open-sites → `pushOverlay`: the base help (model.go:795), the bookmark cheat-sheet (bookmark_popup.go:230), the shelf cheat-sheet (shelf_popup.go:191). nil-sites (content_popup.go:147/155/158) → `popOverlay`.
- **Remove BOTH the cheat-sheet gate AND the base checks** at all three sites — they all become `overlayTop()`:
  - dispatch: the gate `if m.contentPopup != nil && m.overlayTop() != nil` (model.go:387) AND the base `if m.contentPopup != nil` (model.go:414).
  - render: the gate (view.go:161, composites over `menuBackground()`) AND the base (view.go:194). The migrated `render(m, menuBackground())` reproduces both (a cheat-sheet pushed over a switcher overlay composites over `menuBackground()` just as the gate did; the base help over the panels likewise).
  - mouse: the gate wheel block (mouse.go:33) AND the base wheel block (mouse.go:62). The migrated overlay's mouse is swallowed by the existing `overlayTop()` check; if wheel-scroll of the viewer must be preserved, route the wheel to `overlayOf[*contentPopup](m).move(...)` from the single `overlayTop()` mouse branch (check the existing bookmark/shelf overlays' wheel handling and mirror it).
- **esc-returns-to-switcher falls out for free:** the cheat-sheet is a `contentPopup` pushed over the switcher overlay; `popOverlay` reveals the switcher. The `?`-cheat-sheet feature's tests ([[popup-help-cheatsheet-feature]]) must still pass (esc returns with the switcher's filter/mark intact).

- [ ] **Step 1:** Migrate `content_popup_test.go` + the cheat-sheet tests (grep `m.contentPopup` across `*_test.go`; the `?`-over-switcher tests in the bookmark/shelf return tests) per the recipe.
- [ ] **Step 2:** `go test ./internal/tui/ -run 'Content|CheatSheet|Help'` → FAIL (compile).
- [ ] **Step 3:** Add `update`/`render`/`box` on `*contentPopup`; delete the two Model-methods + the `m.contentPopup` field (KEEP `filesView`, `visible`, `move`, `newContentPopup`).
- [ ] **Step 4:** Convert the 3 open-sites; remove the 6 routing checks (gate + base × dispatch/render/mouse), wiring the viewer wheel through the single `overlayTop()` mouse branch.
- [ ] **Step 5:** `go build ./cmd/gg && ./test.sh unit` → PASS. Then `./test.sh race`.
- [ ] **Step 6:** `CHANGELOG.md` (internal note): the help/cheat-sheet, reword, and rename popups now live on the overlay stack; the `?` cheat-sheet gate collapsed into the uniform overlay path. Commit `refactor(tui): migrate contentPopup onto the overlay stack; collapse the cheat-sheet gate; changelog`.

## Post-migration state

The only non-overlay popups left are `modal` (async decision) and `actionMenu` (key-replay), deliberately special until Stage 3 (unify the overlay pile + the full-screen surface pile). After this task the dispatch chain is: `modal → actionMenu → overlayTop → stackTop → diffView → panels` — every centered popup on one path.

## Out of scope

- `modal`, `actionMenu` (Stage 3).
- Unifying `overlayStack` + `viewStack` (Stage 3).
