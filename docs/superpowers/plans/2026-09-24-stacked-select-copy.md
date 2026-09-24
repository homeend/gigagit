# Stacked Line Select / Copy (plan 4d) Implementation Plan

> Executed by the session that wrote it (never subagents — CLAUDE.md). Steps use checkbox syntax.

**Goal:** line select/copy works inside a stacked diff in both frontends: the TUI
selection survives the stack's own re-splices, and a web mouse drag copies ONE
version's lines with no header or fold text.

**Architecture:** TUI — the stack's re-splice holds the selection as
(file path, line in file) anchors and restores it, the way the cursor already
is; only a change of what a file's lines ARE (`f`, a fold expanded for a note)
still clears it. Web — a pure `sideCopyText(cells, side)` decides the copy
payload from the cells a selection touches; a thin DOM layer feeds it from
`#diff-body`, stamps the drag's side for CSS (`user-select: none` on the other
side + headers + folds, so the highlight shows what will be copied), and serves
both the browser `copy` event and the right-click "copy" row.

**Tech stack:** Go (Bubble Tea TUI), vanilla ES-module SPA, node-driven JS
guard tests, Playwright probes (chromium + firefox).

**Spec:** `docs/superpowers/specs/2026-09-22-stacked-diff-view-design.md` §11
item 4, R4. Rulings from the 4d brainstorm (2026-09-24, user: "go with the
recommendations"):

- D1 `f` is ALREADY stack-wide and remembered in both frontends (web
  `toggleDiffView` → `rerenderStack`, `diff_view` in /api/uistate; TUI
  `spliceStack` reads `v.partial`, `m.diffPartial`). `-` is the per-file fold.
  No `f` work in this plan.
- D2 Web keeps mouse drag + right-click "copy"; NO keyboard line selection.
  The copy is ONE side: the side the drag started on (left cell = old, right =
  new; a unified context row = new). Headers, fold rows, note rows and absent
  cells never ride along.
- D3 A range across files copies raw lines back to back — no path lines
  (the TUI already does this: `TestStackSearchAndSelectionSkipNonBody`).
- D4 A text drag keeps the working-tree staging row marks (it is not an
  outside click). A double-click outside the rows still clears them.

## Global constraints

- Every TUI string via `i18n.T` in all four bundles (this plan adds none).
- Web must not import `internal/git` (archtest).
- Probes: explicit ports, `/api/repo` check, isolated `XDG_STATE_HOME`, run
  against the UNFIXED build first, chromium AND firefox.
- Never edit the worktree while `./test.sh race` runs.

## Premise (checked on main `ee165997` before planning)

- TUI throwaway test: a 41-line range spanning two stacked files → 0 after a
  `stackFileMsg` for a file above; `f` also clears it.
- Web drag (probe `stack-probe/dragcopy.mjs`, both browsers) from alpha
  line 20 to beta line 6 copied `line 21 of alpha\tBOTTOM EDIT in alpha` (both
  sides tab-joined), the header chrome `▾ M beta.txt +2 −2`, and the drag
  cleared the one marked staging row (1 → 0).

---

### Task 1: TUI — the selection survives a re-splice

**Files:**
- Modify: `internal/tui/diff_stack.go` (selHold, holdSel, restoreSel next to stackAnchor)
- Modify: `internal/tui/diff_view.go` (`rebuildLines` stack branch; `f` case clears after rebuild)
- Modify: `internal/tui/diff_stack_keys.go` (`reconcileStatusStack` holds before the swap)
- Modify: `internal/tui/note_keys.go` (`expandFoldFor` clears after its rebuild)
- Test: `internal/tui/diff_stack_select_test.go` (new)

**Interfaces:**
- Produces: `type selHold struct{ on, fixed bool; a, e stackAnchor; ap, ep string }`,
  `func (v *diffView) holdSel() selHold`, `func (v *diffView) restoreSel(h selHold)`.

- [ ] **Step 1: failing tests** — in `diff_stack_select_test.go`:
  - `TestStackSelectionSurvivesALateLoad`: stack (nil, 40×5, 40×5 rows),
    cursor in file 1 line +3, `lsel.press`, cursor into file 2, snapshot
    `selectedLines()`; deliver `stackFileMsg{idx:0}`; want `lsel.on` and the
    same lines.
  - `TestStackSelectionSurvivesFoldingAnotherFile`: fixed range inside file 2,
    `-` on file 0 (foldFile) → same `selectedLines()`.
  - `TestStackSelectionSurvivesNotesArriving`: fixed range, `stackNotesMsg` for
    file 1 → same lines.
  - `TestStackSelectionFollowsItsFileAcrossAReconcile`: status stack, fixed
    range in the second file; reconcile with a new file inserted FIRST → the
    same lines; reconcile with the selection's file gone → `lsel.on == false`.
  - `TestStackFStillClearsTheSelection`: fixed range, key `f` → `!lsel.on`
    (the file's lines change meaning — single-file parity).
- [ ] **Step 2:** `go test ./internal/tui -run 'TestStackSelection|TestStackFStill'` → the survive tests FAIL (`on=false`).
- [ ] **Step 3: implement.**

```go
// selHold is a line selection that survives a re-splice: each end as a
// (file, line in file) anchor plus the file's PATH, because a working-tree
// reconcile renumbers the files.
type selHold struct {
	on, fixed bool
	a, e      stackAnchor
	ap, ep    string
}

func (v *diffView) holdSel() selHold {
	if v.stk == nil || !v.lsel.on {
		return selHold{}
	}
	h := selHold{on: true, fixed: v.lsel.fixed, a: v.anchorAt(v.lsel.anchor), e: v.anchorAt(v.lsel.end)}
	h.ap, h.ep = v.stackPathOf(h.a.file), v.stackPathOf(h.e.file)
	return h
}

func (v *diffView) restoreSel(h selHold) {
	v.lsel.clear()
	if !h.on || v.stk == nil {
		return
	}
	var ok1, ok2 bool
	if h.a.file, ok1 = v.stackFileIdx(h.ap); !ok1 { return }
	if h.e.file, ok2 = v.stackFileIdx(h.ep); !ok2 { return }
	v.lsel = lineSel{on: true, anchor: v.lineAt(h.a), end: v.lineAt(h.e), fixed: h.fixed}
}
```

  `rebuildLines` stack branch: `h := v.holdSel()` before the clear,
  `v.restoreSel(h)` after `relayout`. `f`: `v.lsel.clear()` after
  `v.rebuild()`. `expandFoldFor`: `v.lsel.clear()` after its rebuild.
  `reconcileStatusStack`: `h := v.holdSel()` before `v.stk.files = fresh`,
  `v.restoreSel(h)` after `v.rebuild()` (the rebuild's own hold read a stream
  whose file indexes no longer match the list — the explicit one wins).
- [ ] **Step 4:** tests pass; `go test ./internal/tui` green.
- [ ] **Step 5:** commit `fix(tui): a stacked line selection survives loads, folds and refreshes`.

### Task 2: Web — the pure copy payload

**Files:**
- Modify: `internal/web/static/files.js` (new guarded section `// --- side copy (pure; guarded against Go) ---`)
- Test: `internal/web/sidecopyjs_test.go` (new, node-driven like `rowseljs_test.go`)

**Interfaces:**
- Produces: `sideCopyText(cells, side) → string`; `cells` = document-order
  `[{ side: "l"|"r"|"", kind: "same"|"add"|"del"|"mod"|…, text }]`
  (`text` already clipped to the selection), `side` = "l"|"r".

```js
// sideCopyText joins the lines of ONE version: a cell of the other side is
// dropped, and so is a cell whose row has no line on this side (the empty
// left half of an added row, the right half of a removed one). A cell with
// no side class (a unified context row, a one-pane row) belongs to both.
function sideCopyText(cells, side) {
  const out = [];
  for (const c of cells) {
    if (c.side && c.side !== side) continue;
    if (side === "l" && c.kind === "add") continue;
    if (side === "r" && c.kind === "del") continue;
    out.push(c.text);
  }
  return out.join("\n");
}
```

- [ ] Step 1: test cases — side-by-side rows (same/mod/add/del) for "l" and
  "r"; unified rows (same without side, del .l, add .r); a one-pane pure-add
  file whose cells are side "r"; empty input → "".
- [ ] Step 2: run → FAIL (function missing). Step 3: add the function. Step 4: PASS. Step 5: commit.

### Task 3: Web — wire the drag, the copy, the marks

**Files:** `internal/web/static/files.js`, `internal/web/static/style.css`.

- `mousedown` on `#diff-body` (button 0, no modifiers): `td.side` under the
  pointer → `$("diff-body").dataset.selside = td.classList.contains("l") ? "l" : "r"`;
  elsewhere → delete it.
- CSS: `#diff-body[data-selside="r"] td.side.l, #diff-body[data-selside="l"] td.side.r,
  #diff-body[data-selside] .stk-head, #diff-body[data-selside] tr.fold td,
  #diff-body[data-selside] tr:not([data-i]) { user-select: none; }` — the
  highlight shows what will be copied.
- `diffSelectionText()`: every `tr[data-i]:not(.fold) td.side` in
  `#diff-body` that intersects a range of `getSelection()` (ALL ranges —
  Firefox splits a selection around `user-select: none`), text clipped to the
  range, fed to `sideCopyText(cells, selside || "r")`.
- `document` `copy` listener: selection inside `#diff-body` → set
  `text/plain` to `diffSelectionText()`, `preventDefault`.
- The diff context menu's "copy" row uses `diffSelectionText()`.
- The capture-phase outside-click listener returns early when
  `e.detail <= 1 && !getSelection().isCollapsed` and the target is inside
  `#diff-body` (a drag's release, D4).

- [ ] Step 1: extend `stack-probe/dragcopy.mjs` into assertions: copied text
  (read via the "copy" row's `copyText` → intercept `navigator.clipboard` /
  read the ctx action text) has no `\t`, no `beta.txt` header, equals the new
  side's lines; a drag started in the LEFT cell copies old lines; the staging
  mark count is unchanged after the drag; a double-click on a context row
  still clears marks. Run against the main build → FAIL (recorded above).
- [ ] Step 2: implement. Step 3: probe PASS in chromium and firefox; re-run
  `gkselect.mjs`, `gkstack.mjs`, `gkstage.mjs` (staging rulings unchanged).
- [ ] Step 4: `go test ./internal/web` green. Commit.

### Task 4: Docs, gate, verify binary

- `CHANGELOG.md`, `docs/CLAUDE-details.md` (stack selection hold; web side
  copy), `docs/web-tui-parity.md` (web copy = drag, one side — the deliberate
  twin of space/space/enter), spec §11 item 4 marked done.
- `./test.sh race` (no edits while it runs), then verify binaries (Linux
  stripped + `gg-web-new.exe`). The user merges.
