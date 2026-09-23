# In-view search inside a stacked diff — Implementation Plan (4b)

> **For agentic workers:** this plan is executed INLINE by the session that wrote
> it (project rule: never subagents). Steps use checkbox (`- [ ]`) syntax.

**Goal:** `/ @ ] [` search a whole stacked diff — every loaded file is searched
and tinted, and `]`/`[` reach a hit in a file that is folded or has never been
fetched by unfolding / loading it on the step.

**Architecture:** the TUI already searches every loaded, expanded file (one
`v.lines` stream, `searchLines()` skips only non-body lines), so its half is
three narrow additions: re-find AFTER the late-load cursor remap, a "hunt" —
a parked step into a file whose lines do not exist yet — and a badge/title that
says how much of the stack has been searched. The web refuses search in a stack
today; its half gives the ONE `diffSearch` a stack-wide line list by keying each
slot's rows with a composite row number (`slot * STACK_ROW_SPAN + rowIndex`), so
the pure engine, the bar, the hit indices and `data-h` stay exactly what they
are single-file, and only the host verbs (lines, render, step, goTo, here)
become stack-aware.

**Tech Stack:** Go 1.26 + Bubble Tea (`internal/tui`), ES modules
(`internal/web/static`), Go source-grep + node function-harness tests for the
JS, Playwright probes (chromium + firefox), `tui-capture.sh` under tmux.

**Spec:** `docs/superpowers/specs/2026-09-22-stacked-diff-view-design.md` — §11
item 2 ("in-view search across loaded slots; a hit in an unloaded or collapsed
file loads/expands it on step"), §5.1 (slot), §5.2 (stackFile), §6 (loading).

## Global Constraints

- **Never subagents.** One session, sequential, TDD.
- Work in `.claude/worktrees/stacked-search` on `feat/stacked-search`; the user
  merges. Two merges: TUI (tasks 1–7), web (tasks 8–14).
- Every user-visible TUI string goes through `i18n.T` with a **literal** key
  present in all four bundles (`en`, `ja`, `ko`, `zh`, `ru` as shipped);
  **insert in place, never re-sort**; remove orphaned keys from every bundle.
  The search **badge** is exempt (it is built without `i18n.T` — existing
  ruling, in-view-search memory).
- `internal/tui` never imports `internal/git`; frontends go through `domain`.
- The diff footer is **exactly 140 columns** (`TestDiffHintFitsTheBudget`).
- Race gate (`./test.sh race`) before each delivery; a verify binary (Linux +
  Windows, plus `gg-web-new.exe` for the web half) unprompted.
- Probes live in the session scratchpad
  `/tmp/claude-1000/-mnt-t-others-gigagit/9ec3cca2-ba50-4bc2-8475-5c9d5f80f797/scratchpad/stack-probe/`
  (they survived the compaction — reuse, do not rewrite).

## Decisions (D1–D6) — settled at plan review, do not re-ask afterwards

- **D1 — the count spans the whole stack**, over the files SEARCHED so far
  (loaded and expanded). While any searchable file is still folded or unfetched
  the badge carries a `+`: `/foo  3/17+`. The `+` disappears when every
  searchable file has been searched. (Files that can never be searched — binary,
  too large, errored, conflicted, "no content difference" — do not hold the `+`.)
- **D2 — unloaded files are searched ON DEMAND, never eagerly.** `/` loads
  nothing. `]` / `[` at the edge of the searched region unfold / fetch the NEXT
  file in that direction, re-find, and land on its first (last) hit; a file with
  no hit hands the step on to the next candidate, one file per round trip, until
  a hit is found or the stack ends.
- **D3 — wrap only when the whole stack has been searched.** Stacked, `]` is
  strict first: if stepping would wrap while unsearched files remain, hunt
  instead. With nothing left to search, `]` wraps as it does today.
- **D4 — a file the hunt unfolds stays unfolded.** It is the file being read now.
- **D5 — a hunt is cancelled by any other key.** A pending hunt is dropped by
  every key that is not the same-direction `]`/`[`; a repeat while one is
  pending is a no-op (the round trip is already out).
- **D6 (NON-GOAL) — `S` drops the query**, on both frontends. Single-file
  already behaves that way ("a new file is a new search"); leaving a stack is
  leaving the view.

---

## File structure

**TUI (merge 1)**

| File | Change |
|---|---|
| `internal/tui/diff_view.go` | split `rebuild()` into `rebuildLines()` + refind; `searchBadge()` |
| `internal/tui/diff_stack.go` | `applyStackFile` re-finds AFTER the cursor remap; `diffStack.hunt` |
| `internal/tui/diff_stack_search.go` | **new** — `stackHitStep`, `huntTarget`, `drainStackHunt`, `searchableUnsearched`, `hitIn` |
| `internal/tui/diff_stack_keys.go` | `]`/`[` route through `stackHitStep`; other keys clear the hunt |
| `internal/tui/diff_render.go` | header badge reads `v.searchBadge()` |
| `internal/tui/help.go` + 4 bundles | the stack's search row |
| `internal/tui/diff_stack_search_test.go` | **new** — the whole half's tests |

**Web (merge 2)**

| File | Change |
|---|---|
| `internal/web/static/inviewsearch.js` | `stepHitStrict` (pure, node-testable) |
| `internal/web/static/files.js` | `diffItems()` factored out of `diffHTML`; `diffHTML(..., hctx)`; `diffSearchKey` gate; stack-aware host verbs |
| `internal/web/static/stackview.js` | `STACK_ROW_SPAN`, `slotSearchLines`, `refindStack`, `stackHitStep`, `stackSearchHere`, refind sites in `load`/`rerenderStack` |
| `internal/web/static/searchbar.js` | optional `host.step` / `host.count` overrides |
| `internal/web/static/keys.js` | delete the "search works in the single-file view" toast branch |
| `internal/web/stackviewjs_test.go`, `filesjs_test.go`, `inviewsearchjs_test.go` | grep guards + node harnesses |

---

## Task 1: Pin the baseline — a stack already searches its loaded files

**Files:**
- Test: `internal/tui/diff_stack_search_test.go` (create)

**Interfaces:**
- Consumes: `newStackedTestView` / the stack helpers already used by
  `internal/tui/diff_stack_test.go` and `diff_stack_notes_test.go` — read them
  first and reuse the same builder rather than inventing one.
- Produces: nothing; this task states a fact the rest of the plan rests on.

- [ ] **Step 1: Read the existing stack test helpers**

Run: `sed -n '1,80p' internal/tui/diff_stack_test.go` and
`sed -n '1,60p' internal/tui/diff_stack_notes_test.go`.
Note the exact helper that builds a Model with a loaded 3-file stack; every
test below uses it.

- [ ] **Step 2: Write the baseline test**

```go
// A stack is ONE line stream, so the in-view search already spans every file
// whose diff has arrived and is unfolded. This test is the baseline the rest
// of plan 4b rests on: it passes before any of it is written.
func TestStackSearchAlreadySpansLoadedFiles(t *testing.T) {
	t.Parallel()
	m, v := stackedModel(t) // three loaded files: alpha.go, beta.go, gamma.go
	v.search.query = "needle"
	v.search.refindFrom(v.searchLines(), v.searchPos())
	files := map[int]bool{}
	for _, h := range v.search.hits {
		files[v.lines[h.row].file] = true
	}
	if !files[0] || !files[2] {
		t.Fatalf("a stacked search must find hits in the first AND the last file, got files %v (%d hits)", files, len(v.search.hits))
	}
	_ = m
}
```

Give the fixture a line containing `needle` in file 0 and in file 2 (extend the
helper's content if it has none — keep the change additive so the other stack
tests still pass).

- [ ] **Step 3: Run it — it must PASS on the unchanged tree**

Run: `go test ./internal/tui/ -run TestStackSearchAlreadySpansLoadedFiles -v`
Expected: PASS. If it fails, STOP: the premise of this plan is wrong and the
scope changes — report before writing anything else.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/diff_stack_search_test.go
git commit -m "test(tui): pin that a stacked diff already searches its loaded files"
```

---

## Task 2: Re-find AFTER the late-load cursor remap

A file loading ABOVE the viewport lengthens the stream. `applyStackFile` today
calls `v.rebuild()` — whose tail is `refindAfterRebuild()` → `searchPos()` →
`v.curLine` — and only THEN remaps `v.curLine` through the stack anchor. So the
current hit is re-snapped against a stale line index and the highlight jumps off
the line the reader is on. (Same shape as plan 3's `reanchorAfterRebuild`.)

**Files:**
- Modify: `internal/tui/diff_view.go` (`rebuild`)
- Modify: `internal/tui/diff_stack.go` (`applyStackFile`)
- Test: `internal/tui/diff_stack_search_test.go`

**Interfaces:**
- Produces: `func (v *diffView) rebuildLines()` — everything `rebuild()` does
  except the search re-find; `rebuild()` = `rebuildLines()` + `refindAfterRebuild()`.

- [ ] **Step 1: Write the failing test**

```go
// A file arriving ABOVE the reader shifts every line index below it. The
// search must be re-found against the cursor's NEW index, not the one it had
// in the old stream, or the current hit jumps to another file's hit.
func TestStackSearchKeepsTheCurrentHitWhenAFileLoadsAbove(t *testing.T) {
	t.Parallel()
	m, v := stackedModelPending(t, 0) // file 0 not loaded yet; 1 and 2 loaded
	v.search.query = "needle"
	v.search.refindFrom(v.searchLines(), v.searchPos())
	// put the cursor on the hit inside the LAST file
	last := len(v.search.hits) - 1
	v.goToHit(last, 20)
	want := v.lines[v.search.hits[v.search.cur].row].file
	wantNo := v.lines[v.curLine].Row.RightNo

	m, _ = m.applyStackFile(loadedStackFileMsg(t, v, 0)) // file 0 arrives

	v = m.diffLayer()
	got := v.search.hits[v.search.cur]
	if v.lines[got.row].file != want {
		t.Fatalf("the current hit moved to file %d, want %d", v.lines[got.row].file, want)
	}
	if v.lines[v.curLine].Row.RightNo != wantNo {
		t.Fatalf("the cursor left line %d for %d", wantNo, v.lines[v.curLine].Row.RightNo)
	}
	if got.row != v.curLine {
		t.Fatalf("the current hit (%d) parted from the cursor (%d)", got.row, v.curLine)
	}
}
```

`stackedModelPending(t, idx)` and `loadedStackFileMsg(t, v, idx)`: build them
next to the existing helpers — the first leaves file `idx` `stackIdle` with no
`d`, the second returns the `stackFileMsg{gen: v.stk.gen, idx: idx, view: …}`
the ordinary loader would have produced for that file (reuse whatever
`diff_stack_test.go` already does to fabricate a loaded file).

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/tui/ -run TestStackSearchKeepsTheCurrentHit -v`
Expected: FAIL — the current hit lands in the wrong file (the re-find measured
from the pre-remap `curLine`).

- [ ] **Step 3: Split rebuild, re-find after the remap**

In `diff_view.go`:

```go
// rebuild recomputes the logical (mode) stream, then the display stream, and
// re-finds a committed search over it.
func (v *diffView) rebuild() {
	v.rebuildLines()
	v.refindAfterRebuild()
}

// rebuildLines is rebuild WITHOUT the search re-find: the stack's loader
// rebuilds the stream first and remaps the cursor second, and a re-find in
// between would measure from a line index the new stream no longer means.
func (v *diffView) rebuildLines() {
	v.sanLeft, v.sanRight = nil, nil // the line stream is about to change
	v.lsel.clear()                   // …and so do the line indexes it holds
	if v.stk != nil {
		v.spliceStack()
		v.relayout(v.width)
		return
	}
	if v.partial {
		lines, blocks := textdiff.Collapse(v.full, v.fullBlocks, diffContext)
		v.lines, v.blocks = wrapLines(lines), blocks
	} else {
		v.lines = wrapLines(textdiff.Expand(v.full))
		v.blocks = v.fullBlocks
	}
	v.relayout(v.width)
}
```

In `diff_stack.go`'s `applyStackFile`, replace `v.rebuild()` with
`v.rebuildLines()` and add the re-find after the remap:

```go
	v.rebuildLines()
	v.curLine = v.lineAt(cur)
	tl := v.lineAt(top)
	if tl < len(v.lineStart) {
		v.offset = v.lineStart[tl] + top.sub
	}
	v.refindAfterRebuild() // AFTER the remap: searchPos reads v.curLine
	v.scroll(0, body)
	v.syncStackTitle()
```

- [ ] **Step 4: Run the test and the stack suite**

Run: `go test ./internal/tui/ -run 'TestStack' -v 2>&1 | tail -30`
Expected: PASS, and every existing `TestStack*` still green.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/diff_view.go internal/tui/diff_stack.go internal/tui/diff_stack_search_test.go
git commit -m "fix(tui): a late stack load re-finds the search after the cursor remap"
```

---

## Task 3: What is still unsearched — the badge's `+`

**Files:**
- Create: `internal/tui/diff_stack_search.go`
- Modify: `internal/tui/diff_render.go:353`
- Test: `internal/tui/diff_stack_search_test.go`

**Interfaces:**
- Produces:
  - `func (v *diffView) searchableFile(i int) bool` — file i can hold a hit at
    all (not conflicted, not binary/too large/errored, not "no difference").
  - `func (v *diffView) unsearchedFiles() int` — searchable files that are
    folded or not loaded.
  - `func (v *diffView) searchBadge() string` — `v.search.badge()` plus `+`.

- [ ] **Step 1: Write the failing tests**

```go
func TestStackSearchBadgeMarksUnsearchedFiles(t *testing.T) {
	t.Parallel()
	_, v := stackedModelPending(t, 2) // file 2 never fetched
	v.search.query = "needle"
	v.search.refindFrom(v.searchLines(), v.searchPos())
	if got := v.searchBadge(); !strings.HasSuffix(got, "+") {
		t.Fatalf("badge %q must end in + while a file is unsearched", got)
	}
}

func TestStackSearchBadgeDropsThePlusWhenEverythingIsSearched(t *testing.T) {
	t.Parallel()
	_, v := stackedModel(t) // all three loaded, none folded
	v.search.query = "needle"
	v.search.refindFrom(v.searchLines(), v.searchPos())
	if got := v.searchBadge(); strings.HasSuffix(got, "+") {
		t.Fatalf("badge %q must not claim unsearched files when every file is loaded", got)
	}
}

func TestStackSearchBadgeIgnoresFilesThatCanNeverHoldAHit(t *testing.T) {
	t.Parallel()
	_, v := stackedModel(t)
	v.stk.files[1].collapsed = true
	v.stk.files[1].conflict = true // header + resolver: nothing to search
	v.rebuild()
	v.search.query = "needle"
	v.search.refindFrom(v.searchLines(), v.searchPos())
	if got := v.searchBadge(); strings.HasSuffix(got, "+") {
		t.Fatalf("badge %q counted a conflicted file as unsearched", got)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/tui/ -run TestStackSearchBadge -v`
Expected: FAIL with `v.searchBadge undefined`.

- [ ] **Step 3: Write `diff_stack_search.go`'s first half**

```go
package tui

// In-view search inside a STACK (design §11 item 2). The stream a stack
// splices is one document, so `/`, `@`, `]` and `[` already search every file
// whose diff has arrived and is unfolded — nothing here re-implements the
// search. What a stack adds is the part of the document that is NOT in the
// stream: a folded file's rows, and a file that has never been fetched. Those
// are reached the way a }/{ note step reaches them (diff_stack_notes.go) — the
// file is unfolded, sent for, and the landing is PARKED until its lines exist.

// searchableFile reports whether file i could hold a hit at all. A conflicted
// file is a header and a resolver line; a binary, too-large, errored or
// content-identical file has no rows of its own. None of them is ever hunted,
// and none of them holds the badge's "+".
func (v *diffView) searchableFile(i int) bool {
	if v.stk == nil || i < 0 || i >= len(v.stk.files) {
		return false
	}
	f := v.stk.files[i]
	if f.conflict || f.bin {
		return false
	}
	if d := f.d; d != nil {
		return d.err == nil && !d.binary && !d.tooLarge && len(d.full) > 0
	}
	return true // not fetched: assume it has rows until it says otherwise
}

// unsearchedFiles counts the searchable files whose rows are not in the stream:
// folded, or never fetched. It is what the badge's "+" and the hunt read.
func (v *diffView) unsearchedFiles() int {
	if v.stk == nil {
		return 0
	}
	n := 0
	for i := range v.stk.files {
		if !v.searchableFile(i) {
			continue
		}
		if v.stk.files[i].collapsed || v.stk.files[i].d == nil {
			n++
		}
	}
	return n
}

// searchBadge is the header's search status. Stacked, a trailing "+" says the
// count is over the files searched SO FAR — more may be found when ] steps
// into a folded or unfetched file. Like badge() it carries no i18n.T: it is
// punctuation around the user's own query (in-view-search ruling).
func (v *diffView) searchBadge() string {
	bd := v.search.badge()
	if bd == "" || v.stk == nil || v.search.query == "" || v.unsearchedFiles() == 0 {
		return bd
	}
	return bd + "+"
}
```

- [ ] **Step 4: Point the renderer at it**

`internal/tui/diff_render.go:353`: `if bd := v.search.badge(); bd != "" {` →
`if bd := v.searchBadge(); bd != "" {`.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/tui/ -run 'TestStackSearchBadge|TestDiffHint' -v`
Expected: PASS (the badge is right-aligned status, not part of the 140-column
footer, but run the footer test too — it is cheap insurance).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/diff_stack_search.go internal/tui/diff_render.go internal/tui/diff_stack_search_test.go
git commit -m "feat(tui): the stacked search badge marks files it has not searched yet"
```

---

## Task 4: The hunt — `]` / `[` step into a folded or unfetched file

**Files:**
- Modify: `internal/tui/diff_stack.go` (`diffStack.hunt`, `stackHunt`)
- Modify: `internal/tui/diff_stack_search.go`
- Test: `internal/tui/diff_stack_search_test.go`

**Interfaces:**
- Produces:
  - `type stackHunt struct{ file, dir int }` — file `file` was unfolded/sent for
    on behalf of a `]`(dir>0) / `[`(dir<0); on arrival land on its first/last hit
    or hand the step on.
  - `func (m Model) stackHitStep(v *diffView, dir, body int) (Model, tea.Cmd, bool)`
  - `func (v *diffView) hitIn(lo, hi, dir int) (int, bool)` — index into
    `v.search.hits` of the first (dir>0) / last (dir<0) hit inside a line range.
  - `func (v *diffView) huntTarget(from, dir int) (int, bool)` — the next
    searchable, unsearched file in `dir`.

- [ ] **Step 1: Write the failing tests**

```go
// ] at the last hit of the searched region must not wrap while a file it has
// never looked at lies ahead: it unfolds that file, sends for it, and parks.
func TestStackHitStepHuntsIntoAnUnfetchedFile(t *testing.T) {
	t.Parallel()
	m, v := stackedModelPending(t, 2) // file 2 has never been fetched
	v.search.query = "needle"
	v.search.refindFrom(v.searchLines(), v.searchPos())
	v.goToHit(len(v.search.hits)-1, 20) // the last hit in the searched region
	curBefore := v.search.cur

	m, cmd, ok := m.stackHitStep(v, 1, 20)
	if !ok {
		t.Fatal("] must be handled by the stack")
	}
	v = m.diffLayer()
	if v.stk.hunt == nil || v.stk.hunt.file != 2 || v.stk.hunt.dir != 1 {
		t.Fatalf("] must park a hunt on file 2, got %+v", v.stk.hunt)
	}
	if v.search.cur != curBefore {
		t.Fatalf("] must not wrap to hit %d while file 2 is unsearched", v.search.cur)
	}
	if cmd == nil {
		t.Fatal("] must send for the file it hunts")
	}
}

// A folded file is unfolded by the step and stays unfolded (D4); its rows are
// already here, so the landing needs no round trip.
func TestStackHitStepUnfoldsAFoldedFileAndLandsAtOnce(t *testing.T) {
	t.Parallel()
	m, v := stackedModel(t)
	v.stk.files[2].collapsed = true
	v.rebuild()
	v.search.query = "needle"
	v.search.refindFrom(v.searchLines(), v.searchPos())
	v.goToHit(len(v.search.hits)-1, 20)

	m, _, ok := m.stackHitStep(v, 1, 20)
	if !ok {
		t.Fatal("] must be handled")
	}
	v = m.diffLayer()
	if v.stk.files[2].collapsed {
		t.Fatal("] must unfold the file it steps into, and leave it unfolded")
	}
	if v.curFile() != 2 {
		t.Fatalf("] landed in file %d, want the unfolded file 2", v.curFile())
	}
	if h := v.search.hits[v.search.cur]; v.lines[h.row].file != 2 {
		t.Fatalf("the current hit is in file %d, want 2", v.lines[h.row].file)
	}
	if v.stk.hunt != nil {
		t.Fatalf("a landing that needed no round trip must not park a hunt: %+v", v.stk.hunt)
	}
}

// With every file searched, ] wraps exactly as it does single-file (D3).
func TestStackHitStepWrapsOnceEverythingIsSearched(t *testing.T) {
	t.Parallel()
	m, v := stackedModel(t)
	v.search.query = "needle"
	v.search.refindFrom(v.searchLines(), v.searchPos())
	v.goToHit(len(v.search.hits)-1, 20)

	m, _, ok := m.stackHitStep(v, 1, 20)
	if !ok {
		t.Fatal("] must be handled")
	}
	v = m.diffLayer()
	if v.search.cur != 0 {
		t.Fatalf("] at the last hit of a fully searched stack must wrap to hit 0, got %d", v.search.cur)
	}
	if v.stk.hunt != nil {
		t.Fatal("a wrap must not park a hunt")
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/tui/ -run TestStackHitStep -v`
Expected: FAIL with `m.stackHitStep undefined`.

- [ ] **Step 3: Add the hunt pointer**

In `diff_stack.go`, beside `land`:

```go
	// hunt is the ] / [ step a file owes once it arrives: the search stepped
	// past the end of what has been searched, so the next unsearched file was
	// unfolded and sent for. Separate from `land` because it waits on the
	// DIFF alone (a note landing also waits on stackNotesMsg), and because the
	// two are mutually exclusive — setting one clears the other.
	hunt *stackHunt
```

```go
// stackHunt is a parked search step: file `file` was unfolded (and sent for if
// it had never been fetched) on behalf of a ] (dir>0) / [ (dir<0). When its
// lines arrive the search is re-found and the cursor lands on the file's first
// (dir>0) / last (dir<0) hit — and when it has none, the step hands on to the
// next unsearched file, one round trip at a time (design D2).
type stackHunt struct{ file, dir int }
```

- [ ] **Step 4: Implement the step**

Append to `diff_stack_search.go`:

```go
// hitIn is the index into v.search.hits of the first (dir>0) / last (dir<0)
// hit whose row lies in [lo, hi] — one file's edge hit.
func (v *diffView) hitIn(lo, hi, dir int) (int, bool) {
	best, found := -1, false
	for i, h := range v.search.hits {
		if h.row < lo || h.row > hi {
			continue
		}
		if !found || (dir > 0 && i < best) || (dir < 0 && i > best) {
			best, found = i, true
		}
	}
	return best, found
}

// huntTarget is the next file in dir that is searchable but not searched — the
// file a ] / [ must open to keep going. It starts from the file AFTER `from`.
func (v *diffView) huntTarget(from, dir int) (int, bool) {
	for i := from + dir; i >= 0 && i < len(v.stk.files); i += dir {
		if !v.searchableFile(i) {
			continue
		}
		if v.stk.files[i].collapsed || v.stk.files[i].d == nil {
			return i, true
		}
	}
	return 0, false
}

// stackHitStep is ] / [ in a stack. It is STRICT first: a hit further on in
// the searched region wins outright. Only when the step would WRAP does the
// stack look for a file it has not searched — the wrap is the signal that the
// searched region has run out in this direction (design D3).
func (m Model) stackHitStep(v *diffView, dir, body int) (Model, tea.Cmd, bool) {
	if v.stk == nil || v.search.query == "" {
		return m, nil, false
	}
	pos := v.searchPos()
	if i := stepHit(v.search.hits, pos, dir); i >= 0 {
		h := v.search.hits[i]
		strict := (dir > 0 && searchPosLess(pos, searchPos{row: h.row, side: h.side, col: h.start})) ||
			(dir < 0 && searchPosLess(searchPos{row: h.row, side: h.side, col: h.start}, pos))
		if strict {
			v.goToHit(i, body)
			v.syncStackTitle()
			v.stk.hunt = nil
			return m, nil, true
		}
	}
	// The step wrapped (or there is nothing to step to): hunt.
	from := v.curFile()
	if len(v.search.hits) > 0 && v.search.cur >= 0 && v.search.cur < len(v.search.hits) {
		from = v.lines[v.search.hits[v.search.cur].row].file
	}
	if i, ok := v.huntTarget(from, dir); ok {
		return m.openHunt(v, i, dir, body)
	}
	// Nothing left to search: the wrap stands.
	if i := stepHit(v.search.hits, pos, dir); i >= 0 {
		v.goToHit(i, body)
		v.syncStackTitle()
	}
	v.stk.hunt = nil
	return m, nil, true
}

// openHunt unfolds file i, moves the cursor to its header (which is what puts
// it inside the loader's window — wantLoads only picks files near the reading
// position), and either lands at once (its rows are already here) or parks the
// hunt and pumps the queue.
func (m Model) openHunt(v *diffView, i, dir, body int) (Model, tea.Cmd, bool) {
	f := &v.stk.files[i]
	f.collapsed = false
	v.rebuild()
	v.setCursorLine(f.hdr, body)
	v.syncStackTitle()
	v.stk.land = nil
	v.stk.hunt = &stackHunt{file: i, dir: dir}
	if f.d != nil {
		nm, _ := m.drainStackHunt(i, body)
		return nm, nil, true
	}
	nm, cmd := m.pumpStack()
	return nm, cmd, true
}

// drainStackHunt consumes a hunt parked on file idx once its lines are in the
// stream: it lands on that file's edge hit, or — when the file turned out to
// hold none — hands the step on to the next unsearched file (one round trip
// per file, never a bulk load).
func (m Model) drainStackHunt(idx, body int) (Model, tea.Cmd) {
	v := m.diffLayer()
	if v == nil || v.stk == nil || v.stk.hunt == nil || v.stk.hunt.file != idx {
		return m, nil
	}
	dir := v.stk.hunt.dir
	v.stk.hunt = nil
	lo, hi := v.fileLineRange(idx)
	if i, ok := v.hitIn(lo, hi, dir); ok {
		v.goToHit(i, body)
		v.syncStackTitle()
		return m, nil
	}
	if next, ok := v.huntTarget(idx, dir); ok {
		nm, cmd, _ := m.openHunt(v, next, dir, body)
		return nm, cmd
	}
	return m, nil // the stack ran out: the reader stays on this file's header
}
```

`searchPosLess` is `textsearch.go`'s document-order comparison — check its real
name with `rtk grep -n "func before\|func posLess\|func lessPos" internal/tui/textsearch.go`
and use it; do not add a second one.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/tui/ -run TestStackHitStep -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/diff_stack.go internal/tui/diff_stack_search.go internal/tui/diff_stack_search_test.go
git commit -m "feat(tui): ] and [ step into a folded or unfetched file of a stack"
```

---

## Task 5: Wire the keys and drain the hunt on arrival

**Files:**
- Modify: `internal/tui/diff_view.go` (`diffSearchKey`'s `searchNext`/`searchPrev`)
- Modify: `internal/tui/diff_stack.go` (`applyStackFile` drains the hunt)
- Modify: `internal/tui/diff_stack_keys.go` (any other key clears a hunt)
- Test: `internal/tui/diff_stack_search_test.go`

**Interfaces:**
- Consumes: `stackHitStep`, `drainStackHunt` (task 4).

- [ ] **Step 1: Write the failing tests**

```go
// The whole gesture end to end: ] on the last searched hit sends for the next
// file, and its arrival lands the cursor on that file's first hit.
func TestStackHuntLandsWhenTheFileArrives(t *testing.T) {
	t.Parallel()
	m, v := stackedModelPending(t, 2)
	v.search.query = "needle"
	v.search.refindFrom(v.searchLines(), v.searchPos())
	v.goToHit(len(v.search.hits)-1, 20)

	m, _ = keyPress(m, "]") // through the real key path, not stackHitStep
	m, _ = m.applyStackFile(loadedStackFileMsg(t, m.diffLayer(), 2))

	v = m.diffLayer()
	if v.curFile() != 2 {
		t.Fatalf("the hunt landed in file %d, want 2", v.curFile())
	}
	h := v.search.hits[v.search.cur]
	if v.lines[h.row].file != 2 || h.row != v.curLine {
		t.Fatalf("the cursor (%d) must sit on file 2's first hit (row %d, file %d)", v.curLine, h.row, v.lines[h.row].file)
	}
	if v.stk.hunt != nil {
		t.Fatalf("a landed hunt must be cleared, got %+v", v.stk.hunt)
	}
}

// A file that turns out to hold no hit hands the step on — one file per round
// trip, never a bulk load (D2).
func TestStackHuntPassesOnAFileWithNoHit(t *testing.T) {
	t.Parallel()
	m, v := stackedModelPending(t, 1, 2) // 1 and 2 unfetched; only 2 has "needle"
	v.search.query = "needle"
	v.search.refindFrom(v.searchLines(), v.searchPos())
	v.goToHit(len(v.search.hits)-1, 20)

	m, _ = keyPress(m, "]")
	m, _ = m.applyStackFile(loadedStackFileMsg(t, m.diffLayer(), 1)) // no hit here
	v = m.diffLayer()
	if v.stk.hunt == nil || v.stk.hunt.file != 2 {
		t.Fatalf("a hitless file must hand the hunt to file 2, got %+v", v.stk.hunt)
	}
	if v.stk.files[2].d != nil {
		t.Fatal("the next file must not have been loaded before its turn")
	}
	m, _ = m.applyStackFile(loadedStackFileMsg(t, m.diffLayer(), 2))
	v = m.diffLayer()
	if v.curFile() != 2 || v.stk.hunt != nil {
		t.Fatalf("the hunt must land in file 2 and clear, file=%d hunt=%+v", v.curFile(), v.stk.hunt)
	}
}

// Any other key abandons a pending hunt (D5): the answer still arrives, and it
// must not yank the cursor away from wherever the reader went.
func TestStackHuntIsCancelledByAnyOtherKey(t *testing.T) {
	t.Parallel()
	m, v := stackedModelPending(t, 2)
	v.search.query = "needle"
	v.search.refindFrom(v.searchLines(), v.searchPos())
	v.goToHit(len(v.search.hits)-1, 20)
	m, _ = keyPress(m, "]")
	m, _ = keyPress(m, "g") // the reader goes to the top
	if m.diffLayer().stk.hunt != nil {
		t.Fatal("another key must drop a pending hunt")
	}
	at := m.diffLayer().curLine
	m, _ = m.applyStackFile(loadedStackFileMsg(t, m.diffLayer(), 2))
	if got := m.diffLayer().curLine; got != at {
		t.Fatalf("a cancelled hunt must not move the cursor (%d → %d)", at, got)
	}
}
```

`keyPress(m, "]")` — reuse the helper the tui tests already use to feed a
`tea.KeyMsg` through `Update`; grep `diff_stack_keys_test.go` for its name.
(The cancelled-hunt assertion tolerates the remap: `g` puts the cursor on line
0 of file 0, which no arrival below it shifts.)

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/tui/ -run TestStackHunt -v`
Expected: FAIL — `]` still wraps inside the loaded region.

- [ ] **Step 3: Route the keys**

`diff_view.go`, in `diffSearchKey`:

```go
	case searchNext, searchPrev:
		dir := 1
		if searchCommandKey(...) == searchPrev { … } // keep the existing two cases if that reads cleaner
		if v.stk != nil {
			if nm, cmd, ok := m.stackHitStep(v, dir, body); ok {
				return nm, cmd, true
			}
		}
		v.goToHit(stepHit(v.search.hits, v.searchPos(), dir), body)
		return m, nil, true
```

Keep the two existing `case searchNext:` / `case searchPrev:` arms and give each
the stacked branch with its own `dir` — do not restructure the switch.

`diff_stack.go`, at the tail of `applyStackFile`, after `v.syncStackTitle()`:

```go
	// A ] / [ that stepped into this file lands now that its rows exist.
	nm, hcmd := m.drainStackHunt(msg.idx, body)
	m = nm
	return m, tea.Batch(hcmd, m.stackNotesCmd(v.stk.gen, msg.idx, f.d))
```

(`v` is still the layer pointer — `drainStackHunt` re-reads it through
`m.diffLayer()`, which is the same object; keep the existing `v` use for the
notes cmd.)

`diff_stack_keys.go`, in the stacked key entry point, beside the `wrapDir`
reset at the top of the handler:

```go
	// D5: a pending ] / [ hunt belongs to that gesture alone. Any other key
	// abandons it, so the answer that is already out cannot yank the cursor
	// away from wherever the reader has since gone.
	if v.stk != nil && v.stk.hunt != nil && !isHitStepKey(msg) {
		v.stk.hunt = nil
	}
```

with `func isHitStepKey(msg tea.KeyMsg) bool { s := msg.String(); return s == "]" || s == "[" }`
in `diff_stack_search.go`. Put the reset where the existing `wrapDir` reset is
(the same "every key clears this" position) — grep for `wrapNone` to find it.

- [ ] **Step 4: Run the tests, then the whole package**

Run: `go test ./internal/tui/ -run TestStack -v 2>&1 | tail -20`
then `timeout 900 go test ./internal/tui/ 2>&1 | tail -20` (the package takes
~5.5 min — FOREGROUND, never backgrounded).
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/diff_view.go internal/tui/diff_stack.go internal/tui/diff_stack_keys.go internal/tui/diff_stack_search.go internal/tui/diff_stack_search_test.go
git commit -m "feat(tui): ] and [ in a stack hunt file by file, cancelled by any other key"
```

---

## Task 6: Help, footer and i18n

**Files:**
- Modify: `internal/tui/help.go`
- Modify: `internal/i18n/bundles/*.toml` (all four)
- Test: `internal/tui/i18n_scan_test.go` (existing gate; no new test file)

**Interfaces:** none.

- [ ] **Step 1: Read what plan 4a added**

Run: `rtk grep -n "note\|stack" internal/tui/help.go | head -30` and find the
stacked-diff help rows plan 3 and 4a wrote. The new row goes beside them.

- [ ] **Step 2: Add the row**

One row, in the diff-view help section next to the existing `/ @ ] [` row:

```go
		{i18n.T("] / ["), i18n.T("next / previous hit — steps into a folded or unread file of a stack")},
```

Match the surrounding rows' exact shape (some are `helpRow{...}` literals) —
copy a neighbour rather than guessing.

- [ ] **Step 3: Add the key to all four bundles**

Insert `"next / previous hit — steps into a folded or unread file of a stack"`
**in place** (alphabetically where the file is alphabetical, else beside the
neighbouring diff keys) in each of the four non-English bundles with a real
translation. **Never re-sort a bundle.** Follow the `adding-translations` skill.

Do NOT change the diff footer: the stacked footer is exactly 140 columns and
already advertises `[/] find`.

- [ ] **Step 4: Run the gates**

Run: `go test ./internal/tui/ -run 'TestI18n|TestHelp|TestDiffHint' -v`
Expected: PASS (missing-key and orphan-key gates green).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/help.go internal/i18n
git commit -m "docs(tui): help says ] and [ step into a stack's unread files"
```

---

## Task 7: Verify headlessly, document, deliver (TUI merge)

**Files:**
- Modify: `CHANGELOG.md`, `docs/CLAUDE-details.md`, `README.md` (if the key's
  user-facing description changes), `docs/web-tui-parity.md` (search row).

- [ ] **Step 1: Build and capture**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/stacked-search
go build -o /tmp/claude-1000/.../scratchpad/gg-search ./cmd/gg
./tui-capture.sh --state /tmp/claude-1000/.../scratchpad/state-search1 \
  --gg /tmp/claude-1000/.../scratchpad/gg-search \
  "enter,enter,enter,S,/,n,e,e,d,l,e,enter,],]"
```

Remember: the capture starts on BRANCHES (hence `enter,enter,enter`), each
`--state` dir is single-use (a re-run inherits the pref the previous `S` wrote),
and `--state` isolates `XDG_STATE_HOME` by itself.

- [ ] **Step 2: Read the snapshots**

Expected in the last screens: the title names the file the last `]` landed in,
the badge reads `/needle  N/M` (with `+` while files are unread), and the hit is
visibly highlighted. Check the isolated state dir, not the user's real
`~/.local/state/gg/prompts.toml`:
`md5sum ~/.local/state/gg/prompts.toml` before and after must match.

- [ ] **Step 3: Race gate**

Run: `./test.sh race 2>&1 | tail -20` — start it immediately, do not wait for a
quiet machine.
Expected: all stages green.

- [ ] **Step 4: Docs**

- `CHANGELOG.md`: "Search a whole stacked diff: `/` and `@` search every file
  that is loaded, `]`/`[` step into a folded or unread file to reach a hit, and
  the badge marks with `+` that files remain unsearched."
- `docs/CLAUDE-details.md`: a subsection "In-view search inside a stack (plan
  4b, 2026-09-23)" under the stacked-diff heading — the strict-then-hunt rule,
  the `rebuildLines` split and why, `searchableFile`, D1–D6.
- `docs/web-tui-parity.md`: the in-view-search row now says stacked works in
  the TUI (the web half follows in the second merge).

- [ ] **Step 5: Verify binaries + hand over**

```bash
./build.sh linux && ./build.sh windows
```
Send both with SendUserFile and their absolute paths, unprompted, then report
the evidence (capture excerpt, race gate result) and stop for the merge.

---

## Task 8: `stepHitStrict` — a step that reports "nothing in this direction"

**Files:**
- Modify: `internal/web/static/inviewsearch.js`
- Test: `internal/web/inviewsearchjs_test.go` (node harness, existing pattern)

**Interfaces:**
- Produces: `stepHitStrict(hits, pos, delta) → index | -1` — `stepHit` without
  the wrap. Exported beside `stepHit`.

- [ ] **Step 1: Write the failing harness test**

In `internal/web/inviewsearchjs_test.go`, following the existing `jsFunc`
pattern in that file (read it first):

```go
func TestStepHitStrictDoesNotWrap(t *testing.T) {
	t.Parallel()
	fn := jsFunc(t, "inviewsearch.js", "stepHitStrict", "before", "hitPos")
	hits := `[{"row":1,"side":1,"start":0,"end":3},{"row":5,"side":1,"start":0,"end":3}]`
	if got := fn(t, hits, `{"row":5,"side":1,"col":0}`, "1"); got != "-1" {
		t.Fatalf("a forward strict step past the last hit must report -1, got %s", got)
	}
	if got := fn(t, hits, `{"row":1,"side":1,"col":0}`, "-1"); got != "-1" {
		t.Fatalf("a backward strict step before the first hit must report -1, got %s", got)
	}
	if got := fn(t, hits, `{"row":1,"side":1,"col":0}`, "1"); got != "1" {
		t.Fatalf("a forward strict step must find the next hit, got %s", got)
	}
}
```

Adapt the call shape to whatever `jsFunc` in this repo actually takes (it
extracts named functions from a static file and runs them under node) — read
`internal/web/notesjs_test.go` for a worked example.

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/web/ -run TestStepHitStrict -v`
Expected: FAIL — `stepHitStrict` is not in the file.

- [ ] **Step 3: Implement**

```js
// stepHitStrict is stepHit without the wrap: -1 when there is no hit strictly
// after (delta >= 0) / before (delta < 0) pos. A stacked diff needs the
// difference — a wrap there means "the loaded files have run out", which is
// where ] goes looking for a file it has not searched yet.
function stepHitStrict(hits, pos, delta) {
  if (delta >= 0) {
    for (let i = 0; i < hits.length; i++) if (before(pos, hitPos(hits[i]))) return i;
    return -1;
  }
  for (let i = hits.length - 1; i >= 0; i--) if (before(hitPos(hits[i]), pos)) return i;
  return -1;
}
```

and add it to the export list.

- [ ] **Step 4: Run the test**

Run: `go test ./internal/web/ -run TestStepHit -v`
Expected: PASS, and the existing `stepHit` tests still green.

- [ ] **Step 5: Commit**

```bash
git add internal/web/static/inviewsearch.js internal/web/inviewsearchjs_test.go
git commit -m "feat(web): stepHitStrict — a hit step that reports when a direction is exhausted"
```

---

## Task 9: One collapse per slot — `diffItems` factored out of `diffHTML`

The render is what re-finds today, because the fold set lives inside `diffHTML`
(web-inview-search gotcha 1). A stack must re-find ONCE over every slot before
any slot paints, so the item list has to be computable without painting — and
computed only once, or the fold set the search used and the fold set the paint
used could drift.

**Files:**
- Modify: `internal/web/static/files.js`
- Test: `internal/web/filesjs_test.go` (grep guard) + a node harness if `diffItems`
  is extractable standalone

**Interfaces:**
- Produces:
  - `function diffItems(d, open, notesOn, nctx) → {items, ri}` — the collapsed
    row list and the row-index function `diffHTML` builds today.
  - `diffHTML(d, paneWidth, notesOn, open, nctx, hctx)` — `hctx` is
    `{search, base, items, ri, refind}`; null/absent = today's behaviour.

- [ ] **Step 1: Read the current body of `diffHTML` around the `hs` block**

Run: `sed -n '1495,1560p' internal/web/static/files.js`.
Identify exactly where `items` and `ri` are built and where `hs.refind` runs.

- [ ] **Step 2: Extract**

```js
// diffItems is the row list a render paints: the fold collapse, plus the row
// index function every mask is keyed by. It is factored out of diffHTML so a
// STACK can compute one slot's searchable lines without painting it — and so
// that the search and the paint provably use the SAME fold set (computing it
// twice is how the two drift).
function diffItems(d, open, notesOn, nctx) {
  const rows = d.rows || [];
  const ri = (r) => rows.indexOf(r); // keep whatever diffHTML does today
  const items = collapseDiffRows(rows, open, notesOn ? pinnedRowsFor(nctx) : null);
  return { items, ri };
}
```

Use the REAL expressions from `diffHTML` (the `ri` there is an index map, not
`indexOf` — copy it verbatim), then make `diffHTML` call `diffItems` unless
`hctx` already carries `items`/`ri`:

```js
function diffHTML(d, paneWidth, notesOn = false, open = state.diffFolds, nctx = null, hctx = null) {
  …
  const { items, ri } = hctx && hctx.items ? hctx : diffItems(d, open, notesOn, nc);
  …
  // The search mask. Single-file the RENDER re-finds, over the rows it is
  // about to paint. Stacked the stack has already re-found over every slot's
  // lines at once (one document), so this render only PAINTS — re-finding here
  // would throw away every other slot's hits.
  const hs = hctx ? (hctx.search.query ? hctx.search : null) : notesOn && diffSearch.query ? diffSearch : null;
  if (hs && (!hctx || hctx.refind)) hs.refind(diffSearchLines(items, ri));
  const base = hctx ? hctx.base : 0;
  const hitsL = (r) => (hs ? hs.hitsOn(base + ri(r), r.kind === "same" ? 1 : 0) : null);
  const hitsR = (r) => (hs ? hs.hitsOn(base + ri(r), 1) : null);
```

Note the second change in the `hs` line: stacked, hits are NOT gated on
`notesOn` (a stack paints notes only where the file is note-addressable, but it
searches every file).

- [ ] **Step 3: Guard it**

In `internal/web/filesjs_test.go` (or `stackviewjs_test.go`, wherever the diff
guards live — read both):

```go
func TestDiffHTMLTakesAnExplicitSearchContext(t *testing.T) {
	t.Parallel()
	src := staticFile(t, "files.js")
	if !strings.Contains(src, "function diffHTML(d, paneWidth, notesOn = false, open = state.diffFolds, nctx = null, hctx = null)") {
		t.Fatal("diffHTML must take an explicit search context (hctx) — a stack paints one slot while the search spans them all")
	}
	if !strings.Contains(src, "hctx && hctx.items ? hctx : diffItems(") {
		t.Fatal("diffHTML must reuse the caller's items so the search and the paint share one fold set")
	}
}
```

- [ ] **Step 4: Run the web tests**

Run: `go test ./internal/web/ 2>&1 | tail -20`
Expected: PASS — including every existing `diffHTML` harness (the default
arguments keep single-file behaviour identical).

- [ ] **Step 5: Commit**

```bash
git add internal/web/static/files.js internal/web/filesjs_test.go
git commit -m "refactor(web): diffHTML takes an explicit search context and a shared item list"
```

---

## Task 10: One search over the whole stack — the composite row key

**Files:**
- Modify: `internal/web/static/stackview.js`
- Test: `internal/web/stackviewjs_test.go`

**Interfaces:**
- Produces:
  - `const STACK_ROW_SPAN = 1e9` — a slot's rows are keyed
    `k * STACK_ROW_SPAN + rowIndex`, so one flat hit list stays in document
    order across files, `data-h` stays globally unique, and the pure engine
    (`before`, `stepHit`, `nearestHit`) is untouched.
  - `function slotItems(st, k) → {items, ri}` (cached on the slot as `s.items`)
  - `function refindStack()` — re-find `diffSearch` over every loaded, expanded
    slot's lines and repaint the slots whose hits changed.
  - `function stackSearchHere() → {row, side, col}` — the first row on screen,
    with its composite key.

- [ ] **Step 1: Write the failing guards**

```go
func TestStackSearchKeysRowsPerSlot(t *testing.T) {
	t.Parallel()
	src := staticFile(t, "stackview.js")
	if !strings.Contains(src, "STACK_ROW_SPAN") {
		t.Fatal("a stacked search must key each slot's rows into one document order — every file has a row 12")
	}
	if !strings.Contains(src, "function refindStack") {
		t.Fatal("the stack must re-find ONCE over every loaded slot; a per-slot re-find would clobber the others' hits")
	}
	for _, site := range []string{"async function load(", "function rerenderStack("} {
		i := strings.Index(src, site)
		if i < 0 {
			t.Fatalf("%s vanished — re-point this guard", site)
		}
		if !strings.Contains(src[i:min(i+1600, len(src))], "refindStack()") {
			t.Fatalf("%s must re-find: a slot arriving (or a fold change) changes what the query matches", site)
		}
	}
}
```

- [ ] **Step 2: Run to see it fail**

Run: `go test ./internal/web/ -run TestStackSearchKeys -v`
Expected: FAIL.

- [ ] **Step 3: Implement in `stackview.js`**

```js
// --- in-view search -------------------------------------------------------
//
// A stack is many diffs but ONE document: `/` searches every file whose rows
// are here, ] and [ walk the whole stream, and the count is over the stack.
// The pure engine orders hits by (row, side, col) with a NUMERIC row, so each
// slot's rows are keyed into one space: k * STACK_ROW_SPAN + the row's index
// in its own diff. That keeps document order, keeps `data-h` unique across the
// pane (one Search, so hit indices are global), and leaves inviewsearch.js and
// searchbar.js exactly as the single-file view uses them.
const STACK_ROW_SPAN = 1e9; // far past any diff's row count; k * 1e9 stays exact

// slotItems is one slot's collapsed row list, computed once per paint and kept
// on the slot so the search and the render share the same fold set.
function slotItems(st, k) {
  const s = st.slots[k];
  if (!s.diff || s.collapsed) return null;
  const nc = { ctx: s.ctx || null, notes: s.notes || [], row: s.row || null };
  s.items = diffItems(s.diff, s.folds, notesArmed(nc.ctx), nc);
  return s.items;
}

// refindStack re-runs the query over EVERY loaded, expanded slot at once and
// repaints the slots whose tint changed. Re-finding per slot would leave the
// hit list holding only the last slot's hits.
function refindStack() {
  const st = state.stack;
  if (!st) return;
  if (!diffSearch.query) return;
  const lines = [];
  st.slots.forEach((s, k) => {
    const it = slotItems(st, k);
    if (!it) return;
    for (const l of diffSearchLines(it.items, it.ri)) lines.push({ row: k * STACK_ROW_SPAN + l.row, side: l.side, text: l.text });
  });
  diffSearch.refind(lines);
  const hit = new Set(diffSearch.hits.map((h) => Math.floor(h.row / STACK_ROW_SPAN)));
  st.slots.forEach((s, k) => {
    const now = hit.has(k);
    if (!now && !s.hadHits) return; // nothing to paint and nothing to unpaint
    s.hadHits = now;
    if (s.diff && !s.collapsed) repaintSlot(st, k);
  });
}

// stackSearchHere is where the reader is with no hit current: the first row on
// screen anywhere in the pane, in the composite key space.
function stackSearchHere() {
  const st = state.stack;
  const pane = $("diff-pane").getBoundingClientRect();
  for (const tr of $("diff-body").querySelectorAll(".stk-file table.diff tr[data-i]")) {
    if (tr.getBoundingClientRect().bottom < pane.top) continue;
    const sec = tr.closest(".stk-file");
    const k = Number(sec.dataset.k);
    return { row: k * STACK_ROW_SPAN + Number(tr.dataset.i), side: 0, col: -1 };
  }
  return { row: (st ? st.anchor : 0) * STACK_ROW_SPAN, side: 0, col: -1 };
}
```

`bodyHTML` passes the slot's search context:

```js
  if (s.diff) {
    const nc = { ctx: s.ctx || null, notes: s.notes || [], row: s.row || null };
    const k = state.stack ? state.stack.slots.indexOf(s) : 0;
    const it = s.items || null;
    const hctx = { search: diffSearch, base: k * STACK_ROW_SPAN, items: it && it.items, ri: it && it.ri, refind: false };
    return diffHTML(s.diff, $("diff-pane").clientWidth, notesArmed(nc.ctx), s.folds, nc, hctx);
  }
```

(when `s.items` is null — no query has ever been typed — `hctx.items` is null
and `diffHTML` collapses as before; the `refind: false` flag only matters when
there IS a query, and `refindStack` has always filled `s.items` by then.)

Re-find sites, both flagged in the guard:
- `load()`, after `repaintSlot(st, k)`: `if (diffSearch.query) { refindStack(); diffSearchBar.paint(); }`
- `rerenderStack()`, at the end: the same two lines (the `f` fold toggle and a
  resize change which rows exist).
- `teardownStack()`: `diffSearchBar.reset()` (D6 — leaving the stack is leaving
  the view).

- [ ] **Step 4: Run the guards**

Run: `go test ./internal/web/ -run TestStackSearch -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web/static/stackview.js internal/web/stackviewjs_test.go
git commit -m "feat(web): one in-view search over every loaded slot of a stack"
```

---

## Task 11: The bar, the keys and the gate

**Files:**
- Modify: `internal/web/static/searchbar.js` (optional `host.step`/`host.count`)
- Modify: `internal/web/static/files.js` (`diffSearchKey` gate, host verbs)
- Modify: `internal/web/static/keys.js` (delete the toast branch)
- Modify: `internal/web/static/stackview.js` (`stackHitStep`)
- Test: `internal/web/stackviewjs_test.go`

**Interfaces:**
- Produces: `async function stackHitStep(delta)` in stackview.js — the web twin
  of the TUI's `stackHitStep`: strict step, else unfold/load the next slot and
  land, one slot per round trip.

- [ ] **Step 1: Write the failing guards**

```go
func TestStackSearchIsNotRefusedInAStack(t *testing.T) {
	t.Parallel()
	keys := staticFile(t, "keys.js")
	if strings.Contains(keys, "search works in the single-file view") {
		t.Fatal("a stack searches now: the refusal toast must be gone")
	}
	files := staticFile(t, "files.js")
	i := strings.Index(files, "function diffSearchKey(")
	if i < 0 {
		t.Fatal("diffSearchKey vanished")
	}
	body := files[i:min(i+900, len(files))]
	if !strings.Contains(body, "state.stack") {
		t.Fatal("diffSearchKey's gate reads state.lastDiff, which is null in a stack — it must admit a stack too")
	}
}

func TestStackHitStepsAcrossSlots(t *testing.T) {
	t.Parallel()
	src := staticFile(t, "stackview.js")
	if !strings.Contains(src, "stepHitStrict") {
		t.Fatal("] in a stack must know when the loaded slots have run out, not silently wrap")
	}
	i := strings.Index(src, "function stackHitStep")
	if i < 0 {
		t.Fatal("stackHitStep missing")
	}
	if !strings.Contains(src[i:min(i+1800, len(src))], "collapsed") {
		t.Fatal("] must unfold the slot it steps into")
	}
}
```

- [ ] **Step 2: Run to see them fail**

Run: `go test ./internal/web/ -run 'TestStackSearchIsNot|TestStackHitSteps' -v`
Expected: FAIL.

- [ ] **Step 3: The bar's two overrides**

`searchbar.js`:

```js
  const paint = () => {
    lead.textContent = s.backward ? "@" : "/";
    count.textContent = host.count ? host.count() : s.count();
  };
  …
  const step = (delta) => {
    if (s.query === "") return;
    if (host.step) {
      host.step(delta); // a host whose document spans several views steps itself
      paint();
      return;
    }
    const i = stepHit(s.hits, s.pos(host.here()), delta);
    if (i < 0) return;
    host.goTo(i);
    paint();
  };
```

`host.step` must call `paint()` itself once an async landing finishes — pass the
bar's `paint` out by returning it in the object bindSearchBar already returns
(it exposes `paint()` — use `diffSearchBar.paint()` from the host, as the
existing re-render sites do).

- [ ] **Step 4: The host verbs in files.js**

```js
const diffSearchBar = bindSearchBar("diff-search", {
  search: diffSearch,
  here: () => (state.stack ? stackSearchHere() : diffSearchHere()),
  count: () => diffSearchCount(),
  step: (delta) => { if (state.stack) { stackHitStep(delta); return; } … },
  …
  render: () => (state.stack ? refindStack() : rerenderDiffKeepingPlace(true)),
  goTo: goToDiffHit,
  focus: focusDiff,
});
```

`diffSearchCount()` is D1's `+`:

```js
// diffSearchCount is the bar's "i/n". In a stack a trailing + says the count
// is over the files searched SO FAR — ] steps into the rest (design D1).
function diffSearchCount() {
  const c = diffSearch.count();
  if (!c || !state.stack) return c;
  return unsearchedSlots() > 0 ? c + "+" : c;
}
```

`unsearchedSlots()` in stackview.js: slots that are `collapsed` or whose `load`
is not `ok`, excluding `none` / `error` / binary.

`origin()`/`restore()` and `goToDiffHit`'s pan: in a stack the bars are the
section's own (`el.closest(".stk-file").querySelector(".stk-hbars")`), not
`#diff-hbars` (which is hidden). Change `goToDiffHit`'s bar lookup to

```js
  const sec = el.closest(".stk-file");
  const bars = sec ? sec.querySelector(".stk-hbars") : $("diff-hbars");
```

and make `origin()`/`restore()` walk `#diff-body .hbars` (every bar in the pane,
keyed by section + side) when a stack is up.

`diffSearchKey`'s gate:

```js
  if (state.layout !== "diff" || (!state.lastDiff && !state.stack) || conflictPick) return false;
```

`keys.js`: delete the `(e.key === "/" || e.key === "@") && state.stack` branch
and its toast entirely (`diffSearchKey` runs before it and now handles both).

- [ ] **Step 5: `stackHitStep` in stackview.js**

```js
// stackHitStep is ] / [ in a stack. Strict first: a hit further on in the
// loaded slots wins outright. When the direction runs out, the next slot that
// is folded or unread is unfolded, fetched and re-found — one slot per round
// trip, never a bulk load (design D2) — and the step lands on its edge hit, or
// hands on when it holds none. Only when nothing is left to search does ]
// wrap, exactly as it does single-file (D3).
async function stackHitStep(delta) {
  const st = state.stack;
  if (!st || !diffSearch.query) return;
  const pos = diffSearch.pos(stackSearchHere());
  const i = stepHitStrict(diffSearch.hits, pos, delta);
  if (i >= 0) {
    goToDiffHit(i);
    diffSearchBar.paint();
    return;
  }
  let from = Math.floor((diffSearch.cur >= 0 ? diffSearch.hits[diffSearch.cur].row : pos.row) / STACK_ROW_SPAN);
  for (let k = from + (delta >= 0 ? 1 : -1); k >= 0 && k < st.slots.length; k += delta >= 0 ? 1 : -1) {
    const s = st.slots[k];
    if (!searchableSlot(s)) continue;
    if (!s.collapsed && s.load === "ok") continue; // already searched
    if (s.collapsed) { s.collapsed = false; repaintSlot(st, k); }
    scrollToFile(st, k);
    if (!(await awaitSlot(st, s))) return;
    refindStack();
    diffSearchBar.paint();
    const lo = k * STACK_ROW_SPAN;
    const hits = diffSearch.hits
      .map((h, j) => [h, j])
      .filter(([h]) => h.row >= lo && h.row < lo + STACK_ROW_SPAN);
    if (hits.length) {
      goToDiffHit(delta >= 0 ? hits[0][1] : hits[hits.length - 1][1]);
      diffSearchBar.paint();
      return;
    }
  }
  // Nothing left to search: the wrap stands.
  const w = stepHit(diffSearch.hits, pos, delta);
  if (w >= 0) goToDiffHit(w);
  diffSearchBar.paint();
}
```

`awaitSlot(st, s)` is `landStackLine`'s existing wait loop, lifted into its own
function and reused by both (pump + 100 ms, ≤80 turns, bail when the stack
changed). Lift it — do not copy it.

- [ ] **Step 6: Run the guards and the whole web package**

Run: `go test ./internal/web/ 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/web/static internal/web/stackviewjs_test.go
git commit -m "feat(web): / @ ] [ search a stacked diff, stepping into unread files"
```

---

## Task 12: The working-tree reconcile edge

A status refresh inserts and drops slots, so `k` shifts under a live query and a
composite `row` in `diffSearch.anchor()` points into the wrong file.

**Files:**
- Modify: `internal/web/static/stackview.js` (the reconcile path)
- Test: `internal/web/stackviewjs_test.go`

- [ ] **Step 1: Write the guard**

```go
func TestStackReconcileResnapsTheSearch(t *testing.T) {
	t.Parallel()
	src := staticFile(t, "stackview.js")
	i := strings.Index(src, "function reconcileStack")
	if i < 0 {
		t.Skip("no reconcileStack in this build — re-point this guard")
	}
	if !strings.Contains(src[i:min(i+1600, len(src))], "refindStack") {
		t.Fatal("a reconcile renumbers the slots: a live query's anchor would point into another file")
	}
}
```

Find the real reconcile entry point first (`rtk grep -n "reconcile" internal/web/static/stackview.js`)
and point the guard at its actual name.

- [ ] **Step 2: Implement**

At the end of the reconcile, when `diffSearch.query` is non-empty:

```js
  // The slots were renumbered: a composite anchor now names another file.
  // Re-anchor from where the reader IS, then re-find.
  diffSearch.origin = stackSearchHere();
  diffSearch.cur = -1;
  refindStack();
  diffSearchBar.paint();
```

- [ ] **Step 3: Run + commit**

```bash
go test ./internal/web/ -run TestStackReconcile -v
git add internal/web/static/stackview.js internal/web/stackviewjs_test.go
git commit -m "fix(web): a working-tree reconcile re-anchors a live stacked search"
```

---

## Task 13: Playwright probe — unfixed build first, chromium and firefox

**Files:**
- Create: `<scratchpad>/stack-probe/search.mjs`

- [ ] **Step 1: Write the probe**

Model it on `notes.mjs` (same scratchpad). Fixture: reuse `mknotes.sh`'s repo
(a.txt / b.txt / c.txt) or extend it so a word appears in TWO files and in a
third that is far enough down not to be loaded eagerly.

Assertions, all on VISIBILITY:
1. fingerprint the binary (md5 + a `/static/stackview.js` fetch);
2. drop `ui-state.json`, open a commit, press `S`, and re-press it if the first
   press unstacked (the pref is stored — the 4a trap);
3. type `/` + the word: `.hit` elements exist inside **two different**
   `.stk-file` sections at once, and `#diff-search-count` reads `2/…`;
4. the count carries `+` while the last file is unloaded, and loses it after the
   `]` that loads it;
5. `]` from the last hit of the loaded slots: the third file's section gains
   `.hit.cur` and its bounding box is inside `#diff-pane`;
6. `f` (changes-only) then the count is still right (memory gotcha 3);
7. esc clears the tint in every section.

- [ ] **Step 2: Run it against the INSTALLED (unfixed) gg first**

```bash
node search.mjs "$(command -v gg)" <repo> <port> <shots>
```
Expected: FAILS at step 3 (the stack refuses `/` today). Record the output.

- [ ] **Step 3: Run it against the worktree build, chromium AND firefox**

Expected: every assertion passes in both. The user reads Firefox screenshots.

- [ ] **Step 4: Re-run the plan 1/2/4a probes**

```bash
node probe.mjs …; node wt.mjs …; node mixed.mjs …; ./runsym.sh; node notes.mjs …; node land.mjs …
```
Expected: all green (plan 2's lesson: only the previous plan's probe catches a
shared-path regression).

---

## Task 14: Docs, race gate, deliver (web merge)

- [ ] **Step 1: Race gate** — `./test.sh race 2>&1 | tail -20`, started at once.
- [ ] **Step 2: Docs**
  - `CHANGELOG.md`: a web bullet for the stacked search.
  - `docs/CLAUDE-details.md`: the web half under the 4b subsection — the
    composite row key and why (`before()` compares rows numerically), the single
    `diffSearch` over the whole stack, the re-find sites, `+`.
  - `docs/web-tui-parity.md`: the in-view-search row now reads "both, stacked
    too".
- [ ] **Step 3: Binaries** — `./build.sh linux && ./build.sh windows && ./build.sh web`,
  sent with SendUserFile and absolute paths, unprompted.
- [ ] **Step 4:** report evidence (probe output on the unfixed build vs the new
  one, both browsers; race gate) and stop for the merge.
- [ ] **Step 5 (after the user merges both halves):** `./build.sh install`,
  check `command gg --version` matches the merge sha, remove the worktree and
  branch, update memory `stacked-diff-view-feature.md` + `MEMORY.md`.

---

## Self-review

**Spec coverage.** §11 item 2 has two clauses: "across loaded slots" (TUI task
1 pins it as already true; web tasks 9–11 build it) and "a hit in an unloaded or
collapsed file loads/expands it on step" (TUI tasks 4–5, web task 11). §5.1's
`activeDiff()` is untouched — search is not per-slot state, it is one document.
§6's lazy loading is preserved by D2 (no eager load).

**Placeholders.** None: every step carries the code or the exact command. Three
places say "read the real name first" (`searchPosLess`, `keyPress`, the
reconcile entry point) — those are lookups, not decisions, and each names the
grep that resolves it.

**Type consistency.** `stackHunt{file, dir}` is used identically in tasks 4 and
5. `hctx = {search, base, items, ri, refind}` is built in task 10's `bodyHTML`
and consumed in task 9's `diffHTML`. `STACK_ROW_SPAN` is defined once (task 10)
and read in tasks 10 and 11. `searchableFile` (Go) and `searchableSlot` (JS) are
deliberately different names for the two frontends' own predicates.
