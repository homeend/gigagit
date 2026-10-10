# Overview split for every overview — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every overview (stored review overview and agent overview) opens as the split; the review's remarks show as read-only boxes beside the code; the right side marks, never moves, the overview's anchor; `S` stacks the anchored files when there is no review tree.

**Architecture:** The split layer (`splitView` + `pane`) stays as is. `openReviewOverviewSplit` splits into a review caller and a general `openOverviewSplit(ov)` reached through `showOverview` / `bringToFront`. Anchor targets go through one resolver (`anchorPlace`) so note anchors steer. Notes drawn in a file go through `d.shownNotes()` = the agent's store notes + the review's remarks (`d.reviewNotes`, fetched by a new domain query). The overview gains `here` (the anchor the right cursor sits in), painted with a new class.

**Tech Stack:** Go 1.26, Bubble Tea, gg's `internal/tui`, `internal/domain`.

**Spec:** `docs/superpowers/specs/2026-10-10-overview-split-all-design.md`

## Global Constraints

- Every user-visible TUI string goes through `i18n.T` with a literal key present in ja/ko/zh/ru (AST-gate tests).
- `internal/tui` never imports `internal/git`; reads go through `internal/domain` off the UI thread (a `tea.Cmd`).
- Split min width: `splitMinWidth = 60`; narrower opens the full-screen viewer.
- R1: a remark on a DELETED line is left out of the file pane entirely (no count, no box).
- R2: only overview actions (↑/↓, enter, click on an anchor) and `N`/`P` on the single file pane move the overview's selection; `n`/`p` (single and stacked), alt+↑↓, clicks, search, paging never do. The full-screen viewer keeps today's n/p behaviour.
- R3: every overview opens the split; agent overview right side = working tree; a note anchor steers to the note's line.
- R4: no review tree → `S` stacks the files the anchors name, first-anchored order.
- New tests call `t.Parallel()`. Copy tests use `captureClip`. Race gate = `./test.sh race`, green only on "all green" in the log; run with `GG_*` env vars unset (`env -u GG_INBOX -u GG_MCP_URL -u GG_SESSION_TOKEN -u GG_SESSION_ID`).

## Review Focus

1. An overview re-laid out (an agent's `overview set`, a width change, ctrl+w) while `here` is set: `here` must not point at a stale index — expect it reset by the next right-side event, never an out-of-range paint.
2. A remark fetch that lands after the file was steered to another overview (or closed): the stale answer must be dropped, never drawn on the wrong file.
3. `bringToFront` on an overview already shown in a split under a popup: raise that split, never build a second split over the same document.
4. A narrow terminal (<60): every way in falls back to the full-screen viewer, never a squashed split.
5. Agent notes on a stacked working-tree file whose content changed since the notes were aligned (fingerprint mismatch): not drawn at wrong lines.

---

## File map

| File | Change |
|---|---|
| `internal/tui/overview.go` | `here` field; `paint` paints `mdAnchorHere`; `layOut` resets `here` |
| `internal/tui/md_render.go` | `mdAnchorHere` class + style |
| `internal/tui/split_here.go` (new) | `syncOverviewHere(sv)` |
| `internal/tui/split_view.go` | call `syncOverviewHere` after keys and mouse |
| `internal/tui/anchor_bands.go` | `stepAnchorBand` in a split: no `selectAnchor` |
| `internal/tui/split_stack_pane.go` | `stackSteer(sv,p,i)`, `stackLand` no select, `stepStreamAnchor` no select, `newStackPane(sv)` source, toggle without review, stack notes |
| `internal/tui/split_overview.go` | `openOverviewSplit`, `showOverview`, `reviewOf`, `anchorPlace`, note steering |
| `internal/tui/open_files.go` | `bringToFront` routes overviews |
| `internal/tui/steer_overview.go` | `overview add` shows via `showOverview` |
| `internal/tui/overview_keys.go` | `anchorBack` (unchanged call, now reaches the split through `bringToFront`) |
| `internal/domain/review_doc.go` | `ReviewFileNotes` |
| `internal/tui/review_remarks.go` (new) | `reviewNotesCmd`, `reviewNotesMsg`, `remarkBoxes`, `shownNotes`, `readOnlyNote` |
| `internal/tui/open_file_notes.go`, `open_file_note_keys.go`, `file_preview.go`, `open_file.go` | readers go through `shownNotes`; read-only refusals |
| `internal/tui/model.go` | dispatch `reviewNotesMsg`, `stackNotesMsg`; fetch on fill |
| `internal/tui/help.go`, `internal/i18n/lang/*.toml`, `internal/agentskill/gg-overview.md`, `agentskill.go`, `.claude/skills/...`, `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`, e2e goldens | text |

---

### Task 1: The right side marks the overview, never moves it (R2)

**Files:**
- Modify: `internal/tui/overview.go`, `internal/tui/md_render.go`, `internal/tui/split_view.go`, `internal/tui/anchor_bands.go`, `internal/tui/split_stack_pane.go`
- Create: `internal/tui/split_here.go`
- Test: `internal/tui/split_here_test.go`

**Interfaces:**
- Produces: `overview.here int` (-1 none); `mdAnchorHere` class; `func (m Model) syncOverviewHere(sv *splitView)`; `func (m Model) stackSteer(sv *splitView, p *stackPane, i int) (Model, tea.Cmd)` (anchor index explicit).

- [ ] **Step 1: Write the failing tests** (`split_here_test.go`)

```go
package tui

import "testing"

// alt+↓ into another anchor's band marks that anchor on the overview and
// leaves the selection alone.
func TestSplitRightCursorInAnotherBandMarksNotSelects(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t) // a.go:3 selected; "A again" = a.go:1-3 is another band in a.go
	ov := sv.left.doc()
	m = pressDrain(t, m, "tab") // focus right
	r := sv.right.doc()
	r.p.cur = 0 // line 1: only the a.go:1-3 band covers it
	m = pressDrain(t, m, "alt+down") // line 2, still in a.go:1-3 only
	if ov.ov.sel != 0 {
		t.Fatalf("sel %d: the right side never moves the selection", ov.ov.sel)
	}
	if ov.ov.here != 4 {
		t.Fatalf("here %d, want 4 (A again)", ov.ov.here)
	}
}

// n/p in the single pane step the bands and mark, never select.
func TestSplitNPMarkTheOverviewOnly(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	ov := sv.left.doc()
	m = pressDrain(t, m, "tab")
	m = pressDrain(t, m, "n")
	if ov.ov.sel != 0 {
		t.Fatalf("sel %d after n: R2", ov.ov.sel)
	}
	if ov.ov.here < 0 || ov.ov.here == ov.ov.sel {
		t.Fatalf("here %d: n lands in another band, which is marked", ov.ov.here)
	}
}

// N/P (overview anchors) still move the selection; the mark clears when the
// cursor sits in the selected anchor's band.
func TestSplitNPCapitalStillSelects(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	ov := sv.left.doc()
	m = pressDrain(t, m, "tab")
	m = pressDrain(t, m, "N")
	if ov.ov.sel != 1 || ov.ov.here != -1 {
		t.Fatalf("sel %d here %d", ov.ov.sel, ov.ov.here)
	}
}

// The mark paints its own class, the selection keeps reverse video.
func TestOverviewPaintsHereApartFromSel(t *testing.T) {
	t.Parallel()
	_, sv := reviewSplitModel(t)
	ov := sv.left.doc()
	ov.ov.here = 1
	ov.ov.paint(ov.p.lines)
	s := ov.ov.anchors[1].spans[0]
	if c := ov.p.lines[s.line].cls[s.from]; c != mdAnchorHere {
		t.Fatalf("class %v, want mdAnchorHere", c)
	}
	s0 := ov.ov.anchors[0].spans[0]
	if c := ov.p.lines[s0.line].cls[s0.from]; c != mdAnchorSel {
		t.Fatalf("selected class %v", c)
	}
}
```

Add a stacked test in `split_stack_pane_test.go` following its existing setup (`S` from `reviewSplitModel`, drain loads): after `n`, `ov.ov.sel` is unchanged and `ov.ov.here` is the stepped anchor.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tui/ -run 'TestSplitRightCursor|TestSplitNPMark|TestSplitNPCapital|TestOverviewPaintsHere|TestStack.*Mark' -count=1`
Expected: compile error (`here`, `mdAnchorHere` undefined).

- [ ] **Step 3: Implement**

`md_render.go`: add `mdAnchorHere // the anchor the split's right-hand cursor sits in` after `mdAnchorGone`, and in the style switch:

```go
	case mdAnchorHere:
		return st().anchorBand.Inherit(base).Underline(true)
```

(If `anchorBand` carries only a background, `Inherit` keeps the base's foreground.)

`overview.go`: field `here int // the anchor the split's right-hand cursor is in, apart from sel; -1 = none`; every constructor sets `here: -1` (`newOverviewDocFrom`, `reviewOverviewDoc` in review_view.go); `layOut` sets `ov.here = -1` before painting; `paint`:

```go
		case i == ov.sel:
			c = mdAnchorSel
		case i == ov.here:
			c = mdAnchorHere
		case a.missing:
```

`split_here.go`:

```go
package tui

// syncOverviewHere marks on the overview the anchor whose band the right
// pane's cursor sits in (spec 2026-10-10-overview-split-all §7): the
// current band first, else the first band covering the line; none, or the
// selected anchor itself, clears the mark. Nothing on the left moves.
func (m Model) syncOverviewHere(sv *splitView) {
	ov := sv.left.doc()
	if ov == nil || ov.ov == nil {
		return
	}
	here := -1
	if d := sv.right.doc(); d != nil && d.from == ov {
		bs := d.bands()
		line, cur := d.p.cur+1, d.curBand(bs)
		if cur >= 0 && bs[cur].start <= line && line <= bs[cur].end {
			here = bs[cur].i
		} else {
			for _, b := range bs {
				if b.start <= line && line <= b.end {
					here = b.i
					break
				}
			}
		}
	}
	if here == ov.ov.sel || here >= len(ov.ov.anchors) {
		here = -1
	}
	if here != ov.ov.here {
		ov.ov.here = here
		ov.ov.paint(ov.p.lines)
	}
}
```

`split_view.go`: `update` becomes a wrapper — rename the body to `updateKey` and

```go
func (sv *splitView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	nm, cmd := sv.updateKey(m, msg)
	if nm.splitLayer() == sv {
		nm.syncOverviewHere(sv)
	}
	return nm, cmd
}
```

and at the end of `splitMouse` (every return path after the press is handled) call `m.syncOverviewHere(sv)` — restructure as `nm, cmd := m.splitMouseKey(sv, msg, wheel); nm.syncOverviewHere(sv); return nm, cmd`.

`anchor_bands.go` `stepAnchorBand`: keep the selection only outside a split:

```go
	if m.splitShowing(d) == nil { // a split's overview moves only by its own keys (R2)
		ov.selectAnchor(b.i, m.docRows(ov))
	}
```

`split_stack_pane.go`:
- `stackSteer(sv, p, i int)` takes the anchor index (callers: `steerSplit` passes `ov.ov.sel`; `stepStreamAnchor` passes `o.i`); `park = i` as today.
- `stackLand`: delete the `if ov.ov.sel != i { … selectAnchor … }` block (a steer has already selected; n/p must not).
- `stepStreamAnchor`: delete `lr, _ := sv.geomOf(m, sv.left)` and `ov.selectAnchor(o.i, lr)`; call `m.stackSteer(sv, p, o.i)`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run 'Split|Stack|Overview|Band' -count=1`
Expected: PASS. Existing tests that asserted the overview selection follows n/p in a split (search `split_overview_test.go`, `split_stack_pane_test.go` for `sel` after `"n"`) are updated to assert `here` instead — R2 reverses that ruling; note each change in the ledger.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/overview.go internal/tui/md_render.go internal/tui/split_here.go internal/tui/split_here_test.go internal/tui/split_view.go internal/tui/anchor_bands.go internal/tui/split_stack_pane.go internal/tui/review_view.go internal/tui/*_test.go
git commit -m "tui: the split's right side marks the overview's anchor, never moves its selection"
```

---

### Task 2: Every overview opens as the split (R3)

**Files:**
- Modify: `internal/tui/split_overview.go`, `internal/tui/open_files.go`, `internal/tui/steer_overview.go`, `internal/tui/split_stack_pane.go` (`newStackPane` signature only)
- Test: `internal/tui/split_agent_overview_test.go` (new)

**Interfaces:**
- Consumes: `stackSteer(sv, p, i)` (Task 1).
- Produces: `func (m Model) openOverviewSplit(ov *openFile) (Model, tea.Cmd)`; `func (m Model) showOverview(d *openFile) (Model, tea.Cmd)`; `func (m Model) reviewOf(ov *openFile) *reviewViewState`; `func (m Model) anchorPlace(ov *openFile, i int) (path string, line int, dest string, ok bool)`; `func (m Model) newStackPane(sv *splitView) pane` (source picked inside; Task 3 fills the no-review branch).

- [ ] **Step 1: Write the failing tests**

A helper builds an agent overview over working-tree files: a test repo (`newTestModel` + files written into `m.currentWorktree`), an agent note added through the store (`m.docs.AddNote(...)` / the open-file path the existing `steer_file_notes` tests use), then `steerOverviewAdd` with text `"Look at [the loop](a.go:2) and [my note](note:t1)."`.

```go
func TestAgentOverviewAddOpensTheSplit(t *testing.T) {
	t.Parallel()
	m := agentOverviewModel(t, 120) // width 120
	sv := m.splitLayer()
	if sv == nil || m.topLayer() != sv {
		t.Fatalf("top %T, want the split", m.topLayer())
	}
	r := sv.right.doc()
	if r == nil || r.path != "a.go" || r.src.kind != srcWorktree || r.p.cur != 1 {
		t.Fatalf("right %+v", r)
	}
}

func TestAgentOverviewNarrowOpensTheViewer(t *testing.T) {
	t.Parallel()
	m := agentOverviewModel(t, 50)
	if _, ok := m.topLayer().(*fileViewer); !ok {
		t.Fatalf("top %T, want the full-screen viewer below 60 columns", m.topLayer())
	}
}

func TestSplitNoteAnchorSteersToTheNoteLine(t *testing.T) {
	t.Parallel()
	m := agentOverviewModel(t, 120)
	sv := m.splitLayer()
	m = pressDrain(t, m, "down") // the note anchor
	r := sv.right.doc()
	if r == nil || r.path != "b.go" || r.p.cur != 2 || r.anchorCur != "" {
		t.Fatalf("right %+v: a note anchor lands on the note's line, no band current", r)
	}
}

func TestSwitcherOnAnOverviewOpensTheSplit(t *testing.T) {
	t.Parallel()
	m := agentOverviewModel(t, 120)
	m = pressDrain(t, m, "esc") // the split steps aside, both documents in the background
	ov := findOverviewDoc(t, m)
	m, _ = m.bringToFront(ov)  // the switcher's enter and file_focus both come here
	if sv := m.splitLayer(); sv == nil || sv.left.doc() != ov {
		t.Fatal("bringToFront on an overview opens the split")
	}
}

func TestBringToFrontRaisesTheSplitAlreadyShowingIt(t *testing.T) {
	t.Parallel()
	m := agentOverviewModel(t, 120)
	sv := m.splitLayer()
	m = m.pushLayer(newContentPopup("x", nil)) // something over it
	m, _ = m.bringToFront(sv.left.doc())
	if m.topLayer() != sv || countSplits(m) != 1 {
		t.Fatalf("top %T splits %d: raise, never a second split", m.topLayer(), countSplits(m))
	}
}
```

(`countSplits` counts `*splitView` entries in `m.layers.entries`; `findOverviewDoc` returns the open file with `src.kind == srcOverview`.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tui/ -run 'TestAgentOverview|TestSplitNoteAnchor|TestSwitcherOnAnOverview|TestBringToFrontRaises' -count=1`
Expected: FAIL (top is `*fileViewer`).

- [ ] **Step 3: Implement**

`split_overview.go`:

```go
// reviewOf is the open review view whose stored overview ov is, or nil.
func (m Model) reviewOf(ov *openFile) *reviewViewState {
	if st := m.filesReview; st != nil && ov != nil && ov.src.kind == srcReviewOverview && ov.src.rev == st.id {
		return st
	}
	return nil
}

// showOverview is the one door an overview comes to the front through:
// the split on a terminal 60 columns or wider, the full-screen viewer below.
func (m Model) showOverview(d *openFile) (Model, tea.Cmd) {
	if w, _ := m.overlayDims(); w >= splitMinWidth {
		return m.openOverviewSplit(d)
	}
	m = m.detachDoc(d)
	m = m.pushLayer(&fileViewer{d})
	return m.registerDoc(d), nil
}

// openOverviewSplit puts overview ov on the left of a split, its selected
// anchor's place on the right (stacked when the user chose so). A split
// already showing ov is raised instead.
func (m Model) openOverviewSplit(ov *openFile) (Model, tea.Cmd) {
	if sv := m.splitShowing(ov); sv != nil && sv.left.doc() == ov {
		m = m.removeLayer(sv)
		sv.onLeft = true
		return m.pushLayer(sv), nil
	}
	lw, _, bh := m.splitGeom()
	rows, inner := paneGeom(lw, bh)
	m = m.detachDoc(ov) // a viewer still showing it gives it up
	ov.layOut(rows, inner)
	sv := &splitView{onLeft: true}
	sv.left = &overviewPane{docPane: docPane{d: ov, stackable: true}, sv: sv}
	sv.right = &docPane{stackable: true}
	if m.reviewStacked {
		sv.right = m.newStackPane(sv)
	}
	sv.onClose = func(m Model) Model {
		ov.backgrounded = true
		if rd := sv.right.doc(); rd != nil {
			rd.backgrounded = true
		}
		if m.reviewOf(ov) != nil {
			return m.landOnOverviewRow()
		}
		return m
	}
	m = m.pushLayer(sv)
	m = m.registerDoc(ov)
	if ov.ov.sel < 0 {
		ov.stepAnchor(1, rows)
	}
	return m.steerSplit(sv)
}
```

`openReviewOverviewSplit` keeps its narrow check, builds `d := m.reviewOverviewDoc(st, rows, inner)` and returns `m.openOverviewSplit(d)`.

`anchorPlace` (in `split_overview.go`):

```go
// anchorPlace is where overview anchor i opens in a split: a file anchor's
// path and line with its band (dest), or a note anchor's file and first
// line with no band (dest ""). ok false: a plain anchor, a note of a stored
// overview, or a note that is gone — marked missing, the status line says
// which.
func (m Model) anchorPlace(ov *openFile, i int) (path string, line int, dest string, ok bool) {
	a := &ov.ov.anchors[i]
	if a.plain {
		return "", 0, "", false
	}
	if id := a.target.Note; id != "" {
		if ov.src.kind != srcOverview {
			return "", 0, "", false
		}
		d, n := m.findFileNote(id)
		if n == nil {
			a.missing = true
			m.docs.SetAnchorMissing(ov.id(), i, true)
			ov.ov.paint(ov.p.lines)
			return "", 0, "", false
		}
		return d.path, n.Start, "", true
	}
	if a.target.Path == "" {
		return "", 0, "", false
	}
	return a.target.Path, a.target.Start, a.dest, true
}
```

`steerSplit`: replace the `a.plain || a.target.Note != "" || a.target.Path == ""` guard with `path, line, dest, ok := m.anchorPlace(ov, ov.ov.sel)`; on `!ok` with a gone note set `m.statusMsg = i18n.T("note %s is gone", a.target.Note)` and return. Single mode calls `m.docSteer(sv, r, ov, anchor{dest: dest, target: anchorTarget(path, line)})`; stacked calls `m.stackSteer(sv, r, ov.ov.sel)`. The k/n count loop counts anchors with `!x.plain && (x.target.Path != "" || x.target.Note != "" && ov.src.kind == srcOverview)`. `stackSteer`/`stackLand` take the path and line from `anchorPlace` instead of `a.target`.

`open_files.go` `bringToFront`, first lines:

```go
	if d.ov != nil {
		if w, _ := m.overlayDims(); w >= splitMinWidth {
			return m.openOverviewSplit(d)
		}
	}
```

`steer_overview.go` `steerOverviewAdd` default branch: `m, _ = m.showOverview(d)` in place of `m = m.pushLayer(&fileViewer{d})`; the right pane's load command joins the returned batch (`tea.Batch(showCmd, m.checkAnchorsCmd(...))`). Keep `registerDocEv` for the eviction detail — call it before `showOverview` and let `showOverview`'s `registerDoc` be the second (idempotent) registration.

`newStackPane(sv *splitView) pane`: for now `st := m.reviewOf(sv.left.doc())`; if `st == nil` return `&docPane{stackable: true}` (Task 3 replaces this); key `files-`+`st.id` as today.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run 'Split|Overview|Steer|Switcher|OpenFile' -count=1`
Expected: PASS. Tests that expected a `*fileViewer` for an overview at 120 columns (grep `fileViewer` in `overview_*_test.go`, `steer_overview_test.go`) set the model's width below 60 or assert the split — ledger each.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/split_overview.go internal/tui/open_files.go internal/tui/steer_overview.go internal/tui/split_stack_pane.go internal/tui/split_agent_overview_test.go internal/tui/*_test.go
git commit -m "tui: every overview opens as the split; a note anchor steers to its note"
```

---

### Task 3: `S` stacks the anchored files without a review tree (R4)

**Files:**
- Modify: `internal/tui/split_stack_pane.go`
- Test: `internal/tui/split_agent_overview_test.go`

**Interfaces:**
- Consumes: `reviewOf`, `anchorPlace` (Task 2).
- Produces: `func (m Model) stackFiles(ov *openFile) (files []streamFile, key, title string)`.

- [ ] **Step 1: Write the failing tests**

```go
func TestStackOnAnAgentOverviewListsTheAnchoredFiles(t *testing.T) {
	t.Parallel()
	m := agentOverviewModel(t, 120) // anchors: a.go:2, note t1 on b.go
	m = pressDrain(t, m, "S")
	sp, ok := m.splitLayer().right.(*stackPane)
	if !ok {
		t.Fatalf("right %T, want the stack", m.splitLayer().right)
	}
	var got []string
	for _, f := range sp.stream.files {
		got = append(got, f.path)
	}
	if strings.Join(got, ",") != "a.go,b.go" {
		t.Fatalf("files %v, want the anchored files in first-anchored order", got)
	}
}

func TestStackOnAReviewOverviewAfterTheReviewClosed(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	ov := sv.left.doc()
	m = pressDrain(t, m, "esc")
	m.filesReview = nil // the review view is gone
	m, _ = m.bringToFront(ov)
	m = pressDrain(t, m, "S")
	sp, ok := m.splitLayer().right.(*stackPane)
	if !ok || len(sp.stream.files) != 2 || sp.stream.files[0].path != "a.go" || sp.stream.files[1].path != "b.go" {
		t.Fatalf("right %T: the anchored files a.go, b.go (zzz.go has no file at the tip but is anchored: see below)", m.splitLayer().right)
	}
}
```

(The review fixture also anchors `zzz.go:1`, which is `plain` — it does not resolve at the commit — so it is not listed.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tui/ -run 'TestStackOn' -count=1`
Expected: FAIL (`the review is no longer open` / right is `*docPane`).

- [ ] **Step 3: Implement**

```go
// stackFiles is the stack's file list for overview ov: its review tree's
// files when that review view is open, else the files ov's anchors name —
// a file anchor's path, a note anchor's file — each once, first-anchored
// order (spec §5); with the document key and title that go with it.
func (m Model) stackFiles(ov *openFile) (files []streamFile, key, title string) {
	if st := m.reviewOf(ov); st != nil && m.filesView != nil {
		for _, l := range m.filesView.lines {
			if l.path == "" {
				continue
			}
			f := streamFile{path: l.path, status: l.status, notes: st.counts[l.path]}
			if l.status == "D" {
				f.loaded, f.err = true, errors.New(i18n.T("(not at this commit)"))
			}
			files = append(files, f)
		}
		return files, "files-" + st.id, i18n.T("Files: %s", reviewLabel(st.review))
	}
	seen := map[string]bool{}
	for i := range ov.ov.anchors {
		path, _, _, ok := m.anchorPlace(ov, i)
		if !ok || seen[path] {
			continue
		}
		seen[path] = true
		files = append(files, streamFile{path: path})
	}
	return files, "files-" + ov.id(), i18n.T("Files: %s", ov.title)
}
```

`newStackPane(sv)` uses it (the source kind stays `srcReviewStack`, `rev` = the key's id). `toggleReviewStacked` drops the `m.filesReview == nil` refusal.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run 'Stack|Split' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/split_stack_pane.go internal/tui/split_agent_overview_test.go
git commit -m "tui: S stacks the overview's anchored files when no review tree lists them"
```

---

### Task 4: The review's remarks beside the code, single pane (R1)

**Files:**
- Modify: `internal/domain/review_doc.go`, `internal/tui/open_file.go`, `internal/tui/open_file_notes.go`, `internal/tui/open_file_note_keys.go`, `internal/tui/file_preview.go`, `internal/tui/model.go`, `internal/tui/split_overview.go`
- Create: `internal/tui/review_remarks.go`
- Test: `internal/domain/review_doc_test.go`, `internal/tui/review_remarks_test.go`

**Interfaces:**
- Produces: `func (s *Service) ReviewFileNotes(ctx context.Context, reviewID, path string, lines []string) ([]ResolvedNote, error)`; on `openFile`: `reviewNotes []agentdocs.Note`, `reviewNotesOf string` (review id); `func (d *openFile) shownNotes() []agentdocs.Note`; `func (d *openFile) readOnlyNote(n *agentdocs.Note) bool`; `func remarkBoxes(ns []domain.ResolvedNote) []agentdocs.Note`; `func (m Model) reviewNotesCmd(d *openFile) tea.Cmd`; `type reviewNotesMsg struct{ tag, review string; notes []domain.ResolvedNote; err error }`.

- [ ] **Step 1: Write the failing tests**

Domain (`review_doc_test.go`, alongside the existing `ReviewNotesFor` tests, same fixture helpers): a review whose doc has a new-side remark on `[3,3]` and an old-side remark on `[2,2]` of `a.go`:

```go
func TestReviewFileNotesKeepsOnlyLinesAtTheTip(t *testing.T) {
	t.Parallel()
	s, id := reviewWithRemarks(t) // new [3,3] "kept", old [2,2] "deleted line"
	ns, err := s.ReviewFileNotes(context.Background(), id, "a.go", []string{"l1", "l2", "l3"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 1 || ns[0].Note.Summary != "kept" || ns[0].Range != [2]int{3, 3} {
		t.Fatalf("notes %+v: a remark on a deleted line is left out (R1)", ns)
	}
}
```

TUI (`review_remarks_test.go`):

```go
func TestSplitFilePaneDrawsTheReviewRemark(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t) // a.go has remark "A" on [3,3]
	r := sv.right.doc()
	if len(r.shownNotes()) != 1 || r.shownNotes()[0].Summary != "A" || r.shownNotes()[0].Start != 3 {
		t.Fatalf("shown %+v", r.shownNotes())
	}
	if !strings.Contains(m.View(), "A") {
		t.Fatal("the remark's box is drawn")
	}
}

func TestReviewRemarkIsReadOnly(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	m = pressDrain(t, m, "tab")
	r := sv.right.doc()
	r.p.cur = 2 // line 3, under the remark
	m = pressDrain(t, m, "d")
	if len(r.shownNotes()) != 1 || !strings.Contains(m.statusMsg, "read-only") {
		t.Fatalf("notes %d status %q", len(r.shownNotes()), m.statusMsg)
	}
}

func TestRemarksLeaveWithTheOverview(t *testing.T) {
	t.Parallel()
	_, sv := reviewSplitModel(t)
	r := sv.right.doc()
	r.from = nil // stepped back, or opened from elsewhere
	if len(r.shownNotes()) != 0 {
		t.Fatal("a review's remarks show only while its overview is the file's way back")
	}
}

func TestStaleRemarksAnswerIsDropped(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	r := sv.right.doc()
	r.reviewNotes, r.reviewNotesOf = nil, ""
	nm, _ := m.Update(reviewNotesMsg{tag: r.tag, review: "some-other-review", notes: []domain.ResolvedNote{{Range: [2]int{1, 1}}}})
	_ = nm
	if len(r.reviewNotes) != 0 {
		t.Fatal("an answer for another review is dropped")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/domain/ -run TestReviewFileNotes -count=1; go test ./internal/tui/ -run 'TestSplitFilePaneDraws|TestReviewRemark|TestRemarksLeave|TestStaleRemarks' -count=1`
Expected: compile errors (undefined).

- [ ] **Step 3: Implement**

`review_doc.go`:

```go
// ReviewFileNotes is the review's remarks on path placed on the file at the
// reviewed tip, whose text is lines: new-side remarks only — a remark on a
// line the commit deleted has no line there and is left out (spec
// 2026-10-10-overview-split-all R1). A working review has none on a file
// that no longer matches.
func (s *Service) ReviewFileNotes(ctx context.Context, reviewID, path string, lines []string) ([]ResolvedNote, error) {
	r, err := s.Review(ctx, reviewID)
	if err != nil || r.Doc == nil {
		return nil, err
	}
	addr := model.FileAddress{State: model.StateCommitted, Commit: r.Commit, Path: path}
	if r.Kind == ReviewOnWorktree {
		if WorkingReviewState(r.Worktree, r.Files).States[reviewPath(path)] != WorkingFileMatches {
			return nil, nil
		}
		addr = model.FileAddress{State: model.StateUnstaged, Worktree: r.Worktree, Path: path}
	}
	return reviewDocNotes(r, path, addr, nil, lines, true), nil
}
```

`review_remarks.go`:

```go
package tui

// A review's remarks beside the code (spec 2026-10-10-overview-split-all §6):
// a file an overview of a review steered to carries that review's remarks,
// drawn as read-only note boxes, for as long as the overview is its way
// back.

type reviewNotesMsg struct {
	tag, review string
	notes       []domain.ResolvedNote
	err         error
}

// remarkBoxes are a review's remarks as note boxes: their range, words and
// author; the id keeps the review prefix that makes them read-only.
func remarkBoxes(ns []domain.ResolvedNote) []agentdocs.Note {
	out := make([]agentdocs.Note, 0, len(ns))
	for _, n := range ns {
		out = append(out, agentdocs.Note{ID: n.Note.ID, Path: n.Note.Address.Path, Start: n.Range[0], End: max(n.Range[1], n.Range[0]),
			Summary: n.Note.Summary, Rationale: n.Note.Rationale, Author: n.Note.Author})
	}
	return out
}

// reviewOfFrom is the review id whose remarks d may carry: its way back is
// a review's stored overview that is still open; "" otherwise.
func (d *openFile) reviewOfFrom() string {
	if f := d.from; f != nil && f.ov != nil && !f.ov.closed && f.src.kind == srcReviewOverview {
		return f.src.rev
	}
	return ""
}

// shownNotes is every note drawn in d: the agent's (the store's copy) then
// the review's remarks while they are d's, by start line. A stack's are its
// files' notes at their stream lines.
func (d *openFile) shownNotes() []agentdocs.Note {
	if d.stream != nil {
		return d.reviewNotes
	}
	var rn []agentdocs.Note
	if id := d.reviewOfFrom(); id != "" && id == d.reviewNotesOf {
		rn = d.reviewNotes
	}
	if len(rn) == 0 {
		return d.notes
	}
	if len(d.notes) == 0 {
		return rn
	}
	out := append(append([]agentdocs.Note(nil), d.notes...), rn...)
	sort.SliceStable(out, func(a, b int) bool { return out[a].Start < out[b].Start })
	return out
}

// readOnlyNote: a review's remark, or any note in a stack.
func (d *openFile) readOnlyNote(n *agentdocs.Note) bool {
	return d.stream != nil || strings.HasPrefix(n.ID, model.ReviewNoteIDPrefix)
}

// reviewNotesCmd fetches d's review remarks off the UI thread once its
// lines are in; nil when d carries none to fetch.
func (m Model) reviewNotesCmd(d *openFile) tea.Cmd {
	id := d.reviewOfFrom()
	if id == "" || d.stream != nil || !docLoaded(d) || d.p.img != nil || m.svc == nil {
		return nil
	}
	svc, tag, path, lines := m.svc, d.tag, d.path, rawOf(d.p.lines)
	return func() tea.Msg {
		ns, err := svc.ReviewFileNotes(context.Background(), id, path, lines)
		return reviewNotesMsg{tag: tag, review: id, notes: ns, err: err}
	}
}

// reviewNotesLanded adopts an answer still meant for its file.
func (m Model) reviewNotesLanded(msg reviewNotesMsg) Model {
	d := m.openFiles.findTag(msg.tag)
	if d == nil || msg.err != nil || d.reviewOfFrom() != msg.review {
		return m
	}
	d.reviewNotes, d.reviewNotesOf = remarkBoxes(msg.notes), msg.review
	d.syncNoteRows()
	return m
}
```

Readers switch to `shownNotes()`: `file_preview.go` (the `notes, gut =` line and the annotated-hint `len(...) > 0`), `open_file_notes.go` (`noteAt`, `gutterW`, `noteRowsUnder`, `syncNoteRows` — install `extraRows` while `len(d.shownNotes()) > 0`), `open_file_note_keys.go` (`anyFileNotes`, `stepFileNote` both loops and `e.notes[0]`/last, `fileNoteRows`, `noteHint`), `open_file.go:263` (landing). Store-facing ones stay on `d.notes` (`noteByID`, `hasNotes`, `removeNote`, `openFilesProto`, sessions popup count, `steer_file_notes`).

Refusals in `previewNoteKey`:

```go
	case "d", "r":
		if n := d.noteAt(d.p.cur + 1); n != nil && d.readOnlyNote(n) {
			m.statusMsg = d.readOnlyText()
			return m, nil, true
		}
```

placed before the existing `"d"`/`"r"` cases (Go `switch` picks the first matching case: fold the check into each existing case's top instead). `readOnlyText`: `i18n.T("▸ notes are read-only in the stack — S opens the file")` when `d.stream != nil`, else `i18n.T("▸ review notes are read-only")`. `fileNoteRows` leaves out the reference and dismiss rows for a read-only note.

Fetch points: `docSteer` returns `tea.Batch(cmd, m.reviewNotesCmd(d))` where it lands a loaded document; `model.go`'s `fileContentMsg` branch, after `d.fill`, adds `m.reviewNotesCmd(d)`; `model.go` dispatches `case reviewNotesMsg: return m.reviewNotesLanded(msg), nil`. `reviewNotesMsg` carries no `slotStamp` — check how `stackPaneMsg` is stamped and whether the dispatcher drops unstamped messages across worktree switches; stamp it the same way (`slotStamp: m.stamp()`) if so.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/domain/ -count=1 && go test ./internal/tui/ -count=1`
Expected: PASS (the i18n gate covers the new string once Task 6 adds it to the bundles — if it runs first, add the bundle entries in this task).

- [ ] **Step 5: Commit**

```bash
git add internal/domain/review_doc.go internal/domain/review_doc_test.go internal/tui/review_remarks.go internal/tui/review_remarks_test.go internal/tui/open_file*.go internal/tui/file_preview.go internal/tui/model.go internal/tui/split_overview.go internal/i18n/lang/*.toml
git commit -m "tui+domain: a review's remarks show as read-only boxes in the split's file pane"
```

---

### Task 5: Notes in the stacked pane

**Files:**
- Modify: `internal/tui/split_stack.go` (`streamFile.boxes`), `internal/tui/split_stack_pane.go`, `internal/tui/model.go`
- Test: `internal/tui/review_remarks_test.go`

**Interfaces:**
- Consumes: `remarkBoxes`, `ReviewFileNotes`, `shownNotes` (Task 4).
- Produces: `streamFile.boxes []agentdocs.Note` (file lines, 1-based); `type stackNotesMsg struct{ slotStamp; tag string; gen int64; file int; notes []domain.ResolvedNote; err error }`; `func (p *stackPane) notesCmd(m Model, i int) tea.Cmd`.

- [ ] **Step 1: Write the failing tests**

```go
func TestStackDrawsEachFilesRemarksAtTheirStreamLines(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	m = pressDrain(t, m, "S")
	sp := sv.right.(*stackPane)
	ns := sp.d.shownNotes()
	if len(ns) != 2 {
		t.Fatalf("notes %+v: a.go's and b.go's remarks", ns)
	}
	for _, n := range ns {
		fi := sp.stream.fileAt(n.Start - 1)
		f := sp.stream.files[fi]
		if n.Start-1-f.body != 2 { // line 3 of its file
			t.Fatalf("note %q at stream %d, file %s body %d", n.Summary, n.Start, f.path, f.body)
		}
	}
}

func TestStackNotesAreReadOnly(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	m = pressDrain(t, m, "S")
	sp := sv.right.(*stackPane)
	sp.d.p.cur = sp.d.shownNotes()[0].Start - 1
	m = pressDrain(t, m, "tab") // focus right if not already
	m = pressDrain(t, m, "d")
	if !strings.Contains(m.statusMsg, "S opens the file") {
		t.Fatalf("status %q", m.statusMsg)
	}
}
```

Add one agent-overview case: `agentOverviewModel` + `S` shows `b.go`'s agent note `t1` at its stream line; a note whose store fingerprint does not match the stacked lines is not drawn (edit the file on disk before `S`, so the store's alignment is stale).

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tui/ -run 'TestStackDraws|TestStackNotes' -count=1`
Expected: FAIL (no notes).

- [ ] **Step 3: Implement**

- `streamFile` gains `boxes []agentdocs.Note`.
- `stackPaneLanded`: after the lines go in, for a review stack (`m.reviewOf(ov) != nil` or `ov.src.kind == srcReviewOverview`) return `tea.Batch(p.pump(m), p.notesCmd(m, msg.file))`; for an agent overview set `f.boxes` from the store at once when its fingerprint matches: `ns, fp := m.docs.Notes(domain.CheckoutKey(m.currentWorktree), f.path); if fp == agentdocs.Print(rawOf(f.lines)) { f.boxes = ns }`.
- `notesCmd` = `ReviewFileNotes(ctx, ov.src.rev, f.path, rawOf(f.lines))` wrapped as `stackNotesMsg` with the pane's tag and gen; `stackNotesLanded` drops an answer for another tag/gen, sets `f.boxes = remarkBoxes(msg.notes)` and rebuilds.
- `rebuild` ends by mapping: `d.reviewNotes = nil; for each f that shows: for each b in f.boxes: b.Start += f.body; b.End += f.body; append`; then `d.syncNoteRows()`. (Note `f.body` is the 0-based stream index of the file's first line, so file line `k` is stream line `f.body + k` 1-based — the bands' mapping.)

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run 'Stack|Split|Remark' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/split_stack.go internal/tui/split_stack_pane.go internal/tui/model.go internal/tui/review_remarks_test.go
git commit -m "tui: the stacked file pane draws each file's remarks and notes, read-only"
```

---

### Task 6: Help, skill, docs, goldens

**Files:**
- Modify: `internal/tui/help.go`, `internal/i18n/lang/{ja,ko,zh,ru}.toml`, `internal/agentskill/gg-overview.md`, `internal/agentskill/agentskill.go` (`GGOverviewVersion` 3→4), `.claude/skills/gg-overview/SKILL.md` (regenerated), `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`, `e2e/scenarios/tui_stored_overview.screens/*`

- [ ] **Step 1:** Help rows (English + the four bundles):
  - `enter (≡ Overview)` row: append "every overview opens this way — an agent's too (its working-tree files, its notes) — when the window is 60 columns or wider; the review's remarks show as read-only boxes; moving on the file never moves the overview, it marks the anchor whose block the cursor is in".
  - the agent-overview `tab / enter` row: "…opens beside the file it points to when the window is 60 columns or wider".
- [ ] **Step 2:** `gg-overview.md`: where it describes the TUI (lines ~47–48), say the TUI shows the overview beside the file each anchor points to (↑/↓ walk, the right side follows; a note anchor shows the note). Bump `GGOverviewVersion`; regenerate the dogfood copy through the embedded renderer (NOT `gg init --update` on the branch — it rewrites the global installs): reuse the throwaway-test approach from the N/P follow-up (render `agentskill` skill to `.claude/skills/gg-overview/SKILL.md`), then delete the throwaway test.
- [ ] **Step 3:** CHANGELOG entry "Every overview opens beside its code"; README's overview paragraph; `docs/CLAUDE-details.md` "Review overview split" section: `showOverview`/`openOverviewSplit`, `anchorPlace`, `here`, `shownNotes`/`reviewNotes`, `stackFiles`.
- [ ] **Step 4:** `go test ./e2e/ -run TestScenarios -update -count=1` with `GG_*` unset; review every changed golden by eye (the overview marks and hint text only); then `./test.sh race` (GG_* unset) → "all green".
- [ ] **Step 5: Commit**

```bash
git add internal/tui/help.go internal/i18n/lang/*.toml internal/agentskill/gg-overview.md internal/agentskill/agentskill.go .claude/skills/gg-overview/SKILL.md CHANGELOG.md README.md docs/CLAUDE-details.md e2e/scenarios/tui_stored_overview.screens
git commit -m "docs+skill: every overview opens beside its code"
```
