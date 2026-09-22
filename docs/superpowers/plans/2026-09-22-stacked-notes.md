# Stacked diff — review notes + line cursor (plan 4a) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement
> this plan task-by-task (this project forbids subagents — CLAUDE.md). Steps use checkbox
> (`- [ ]`) syntax for tracking.

**Goal:** Make review notes work inside a stacked diff — each file of the stack carries its
own note address, notes and note rows — and make a link / steering landing (and leaving the
stack with `S`) land on the exact LINE rather than the file's header.

**Architecture:** In the TUI every `stackFile` already holds `d *diffView`, the per-file view
its ordinary single-file loader built — so the per-file note ADDRESS (`d.noteAddr`),
preview scope (`d.previewSet`) and rows (`d.full`) already exist. 4a therefore adds no new
data plumbing: it (a) makes the note anchoring functions file-scoped, (b) resolves and stores
each file's notes on its own `d`, (c) routes the note key handlers through the cursor's file,
and (d) gives navigation a parked line landing. On the web the same shape arrives as the
`activeDiff()` accessor the design reserved (§5.1): an explicit note context
(`{ctx, notes, collapsed, row}`) threaded into `diffHTML`, per slot, defaulting to today's
globals in single-file mode.

**Tech Stack:** Go 1.26, Bubble Tea (value-receiver `Model`), `internal/domain` note queries,
`internal/i18n` TOML bundles (ja/ko/zh/ru); vanilla ES modules for `gg web`; Playwright
(chromium + firefox) for browser probes; `./tui-capture.sh` (tmux PTY) for TUI snapshots.

**Spec:** `docs/superpowers/specs/2026-09-22-stacked-diff-view-design.md` — §11 item 1 is this
plan; §5.1/§5.2 are the slot / `stackFile` the notes hang off; §8 ("landing on the exact line
arrives with the notes + cursor follow-up") is the link half.

## Global Constraints

- **NEVER use sub agents.** Every task runs inline in this session (CLAUDE.md).
- Work in the worktree `/mnt/t/others/gigagit/.claude/worktrees/stacked-notes`
  (branch `feat/stacked-notes`); never commit feature work in the main checkout.
- `internal/tui` never imports `internal/git`; it reaches git through `internal/domain`.
- Every new user-visible TUI string goes through `i18n.T` with a **literal** key present in
  all four bundles (ja/ko/zh/ru). **Never re-sort a bundle** — insert the key in place
  (skill `adding-translations`). A key that stops being used must be REMOVED from all four
  (the AST gate fails on orphans).
- TDD: write the failing test, watch it fail, implement, watch it pass, commit.
- Tests that need a notes store use `svc.UseNotesDir(t.TempDir())`; `internal/tui` tests build
  repos with `testRepo(t, dir)` and never construct a raw `NewExecRunner`.
- New `internal/tui` tests call `t.Parallel()` unless they touch global state.
- The TUI footer for a stacked diff already spends its 140-column budget: nothing new may be
  added to it (advertise in the context help and the `.` menu instead).
- `./test.sh race` before delivery; a verify binary (Linux + Windows) is delivered
  unprompted; the USER merges.

---

## Decisions this plan makes (tell the user at review; they were not re-asked)

| # | Decision |
|---|----------|
| D1 | **Notes are inert in a stack exactly where they are inert single-file** — a file whose loader stamped no `noteAddr` (every two-sided compare: branch/entry/link/full-tree). No new rule, no new refusal text: the stack inherits each file's own addressability. |
| D2 | **`}`/`{` crosses the whole stack.** Inside loaded files it walks note to note; when nothing is left in that direction it uses `NoteCounts` (the same predicate the single-file file step uses — no store read) to find the next file that CARRIES notes, expands it, loads it, and parks the landing until its notes arrive. |
| D3 | **In-file change stepping = `ctrl+↓` / `ctrl+↑` while stacked.** They duplicate `n`/`p` today; stacked, `n`/`p` keep stepping files (v.blocks = headers) and ctrl-arrows step change to change inside the cursor's file, stopping at its ends (no wrap into the neighbouring file). |
| D4 | **Leaving with `S` keeps the line being read**, not the file's first change: `unstack` parks a `{side, no}` landing consumed when the single-file diff arrives. |
| D5 | **A conflicted file's header shows no counts** (its body is the resolver line, not a diff). |
| D6 | **`o` folds the thread at the cursor; `O` folds every thread in the WHOLE stack** — `O` is a view-wide key today and the collapse set is keyed by (globally unique) root id. |
| D7 | **The notes-list popup lists the whole stack**, one group per file, path shown; jumping from it scrolls to that file's line (expanding the file when folded). |
| D8 | **Web: a slot fetches its notes when its DIFF lands** — one `/api/notes` per loaded file, never for an unloaded one. Notes are inert on a web stack wherever they are inert single-file (D1). |
| D9 | **Merge order:** the TUI half (tasks 1–8) is self-contained and can be merged and tested before the web half (tasks 9–12) — say so at review; the user may want the binary earlier. |

---

## File structure

**TUI (existing files, no new production file except the test files):**

| File | Change |
|------|--------|
| `internal/tui/diff_notes.go` | `lineAnchorIn`, file-scoped `noteRowIndex` / `noteAnchorLine`, `setNotesFor`, `notesOf`, per-file `noteBoxTitle` path |
| `internal/tui/note_keys.go` | `diffNoteAddress` / `previewNoteSet` / `loadNotesCmd` read the cursor's file; `}`/`{` stack walk + `stackNoteLanding` |
| `internal/tui/diff_stack.go` | `stackNotesMsg`, `stackNotesCmd`, `land` on `diffStack`, notes load in `applyStackFile` |
| `internal/tui/diff_stack_keys.go` | drop the "press S" gate; `unstack` parks the line landing; ctrl-arrow in-file change steps |
| `internal/tui/diff_stack_render.go` | conflicted header shows no counts |
| `internal/tui/steer_nav.go` | `landSteer` resolves the file inside a stack, then the line |
| `internal/tui/notes_list_popup.go` | whole-stack listing + jump |
| `internal/tui/model.go` | `case stackNotesMsg`; notes reload after a mutation covers every loaded file |
| `internal/i18n/lang/*.toml` | remove the orphaned "press S" key; add the new ones |
| `tui-capture.sh` | `--state <dir>` |

**Tests:** `internal/tui/diff_stack_notes_test.go` (new), plus additions to
`internal/tui/diff_stack_test.go`, `internal/tui/note_keys_test.go`,
`internal/tui/steer_nav_test.go`.

**Web:**

| File | Change |
|------|--------|
| `internal/web/static/stack.js` | slot gains `ctx` / `notes` / `noteCollapsed` / `row`; `slotNoteCtx(slot)` pure builder |
| `internal/web/static/stackview.js` | fetch a slot's notes on load, repaint, `activeDiff()`, cursor row, line landing |
| `internal/web/static/files.js` | `diffHTML(..., nctx)` threading; note helpers take the context; note keys/menus read `activeDiff()` |
| `internal/web/static/*_test.mjs` (node unit) | slot note-context build + collapse seeding |

---

## Task 1: File-scoped note anchoring

The single lever. `noteRowIndex` today walks `v.notes` and anchors each note by LINE NUMBER
over the whole stream — in a stack every file has a line 12, so every note would pile onto
file 0. Scope the anchor search to one file's line range, and read the notes from each file's
own `d`.

**Files:**
- Modify: `internal/tui/diff_notes.go` (`noteRowIndex` ~86, `noteAnchorLine` ~129, `lineAnchor` ~154)
- Test: `internal/tui/diff_stack_notes_test.go` (create)

**Interfaces:**
- Produces: `func (v *diffView) lineAnchorIn(lo, hi, no int, old bool) (int, bool)` —
  `lineAnchor` becomes `lineAnchorIn(0, len(v.lines)-1, …)`.
- Produces: `func (v *diffView) notesOf(i int) []domain.ResolvedNote` — file i's resolved
  notes stacked (`v.stk.files[i].d.notes`), `v.notes` when not stacked.
- Consumes: `fileLineRange(i)` (diff_stack.go), `stackFile.d` (diff_stack.go).

- [ ] **Step 1: Write the failing test** — two files, a note on each, anchored in its OWN file.

```go
// internal/tui/diff_stack_notes_test.go
package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// notesOn stamps file i of the stack with one note on the new side at line no.
func notesOn(v *diffView, i, no int, body string) {
	v.stk.files[i].d.notes = append(v.stk.files[i].d.notes, domain.ResolvedNote{
		Note:  model.Note{ID: body, Body: body, Side: model.NoteSideNew},
		Range: [2]int{no, no},
	})
}

func TestStackAnchorsEachFilesNotesInItsOwnFile(t *testing.T) {
	t.Parallel()
	v := stackFixture(t, 2) // helper in diff_stack_test.go: two loaded files, 6 rows each
	notesOn(v, 0, 2, "on A")
	notesOn(v, 1, 2, "on B")
	v.rebuild()
	byLine, _ := v.noteRowIndex()
	if len(byLine) != 2 {
		t.Fatalf("want a note row set on 2 lines, got %d", len(byLine))
	}
	loA, hiA := v.fileLineRange(0)
	loB, hiB := v.fileLineRange(1)
	for li := range byLine {
		inA, inB := li >= loA && li <= hiA, li >= loB && li <= hiB
		if !inA && !inB {
			t.Fatalf("note anchored on line %d, outside both files", li)
		}
	}
	// the decisive one: B's note must NOT sit in A
	var bLine = -1
	for li, rows := range byLine {
		for _, r := range rows {
			if containsText(r, "on B") {
				bLine = li
			}
		}
	}
	if bLine < loB || bLine > hiB {
		t.Fatalf("B's note anchored at %d, want inside file B [%d,%d]", bLine, loB, hiB)
	}
}
```

(`stackFixture` and `containsText` are written in this step too if they do not exist —
check `diff_stack_test.go` for the existing stack fixture helper and reuse it.)

- [ ] **Step 2: Run it and watch it fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/stacked-notes && go test ./internal/tui -run TestStackAnchorsEachFilesNotes -v`
Expected: FAIL — `byLine` is empty (`v.notes` is nil in a stack), i.e. `want … 2, got 0`.

- [ ] **Step 3: Implement**

```go
// lineAnchor finds the logical line carrying number no … (doc comment unchanged)
func (v *diffView) lineAnchor(no int, old bool) (int, bool) {
	return v.lineAnchorIn(0, len(v.lines)-1, no, old)
}

// lineAnchorIn is lineAnchor restricted to the logical lines [lo, hi] — one
// file of a stack. Line NUMBERS repeat across a stack's files (every file has
// a line 12), so every anchor a stacked view resolves must name its file's
// range; the single-file view passes the whole stream and reads unchanged.
func (v *diffView) lineAnchorIn(lo, hi, no int, old bool) (int, bool) {
	// …the body of today's lineAnchor, with its two loops bounded by lo/hi…
}
```

```go
// notesOf are the resolved notes of file i — stacked, the file's OWN, resolved
// against its OWN address (stackFile.d is the single-file view its ordinary
// loader built, so d.noteAddr and d.previewSet are the loader's stamps, never
// derived from Model state at key time).
func (v *diffView) notesOf(i int) []domain.ResolvedNote {
	if v.stk == nil {
		return v.notes
	}
	if i < 0 || i >= len(v.stk.files) {
		return nil
	}
	if d := v.stk.files[i].d; d != nil {
		return d.notes
	}
	return nil
}
```

`noteRowIndex` grows a stacked branch that runs the same body once per file:

```go
func (v *diffView) noteRowIndex() (map[int][]noteLine, map[int]bool) {
	byLine, foldMark := map[int][]noteLine{}, map[int]bool{}
	innerW := v.noteInnerWidth()
	add := func(file int, ns []domain.ResolvedNote) {
		lo, hi := 0, len(v.lines)-1
		if v.stk != nil {
			lo, hi = v.fileLineRange(file)
		}
		for _, r := range ns {
			rows := v.noteBoxLines(r, innerW)
			if v.hideAgent {
				rows = dropAgentRows(rows)
			}
			if !hasNoteContent(rows) {
				continue
			}
			li, visible := v.noteAnchorLineIn(lo, hi, r)
			if li < 0 {
				continue
			}
			if !visible {
				foldMark[li] = true
				continue
			}
			byLine[li] = append(byLine[li], rows...)
		}
	}
	if v.stk == nil {
		add(0, v.notes)
	} else {
		for i := range v.stk.files {
			add(i, v.notesOf(i))
		}
	}
	if len(byLine) == 0 && len(foldMark) == 0 {
		return nil, nil // keep today's contract: no notes → nil maps
	}
	return byLine, foldMark
}
```

`noteAnchorLine(r)` becomes `noteAnchorLineIn(lo, hi, r)`; the file-level (forge) case
anchors on the file's **first line in [lo, hi] that is a body line**, not `v.lines[0]`:

```go
func (v *diffView) noteAnchorLineIn(lo, hi int, r domain.ResolvedNote) (int, bool) {
	if isFileLevelNote(r) {
		for li := lo; li <= hi && li < len(v.lines); li++ {
			if v.lines[li].kind == lineBody {
				return li, v.lines[li].Fold == 0
			}
		}
		return -1, false
	}
	return v.lineAnchorIn(lo, hi, r.Range[1], r.Note.Side == model.NoteSideOld)
}
```

Keep `noteAnchorLine(r)` as the single-file wrapper (`noteAnchorLineIn(0, len(v.lines)-1, r)`)
so existing callers and tests compile.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui -run 'TestStackAnchors|TestNote|TestDiffNote' -v`
Expected: PASS, and no existing note test regresses.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/diff_notes.go internal/tui/diff_stack_notes_test.go
git commit -m "feat(tui): note anchors are scoped to one file of a stack"
```

---

## Task 2: Each stacked file resolves its own notes

**Files:**
- Modify: `internal/tui/diff_stack.go` (msg + cmd + `applyStackFile`), `internal/tui/model.go`
  (the new case, and the srcNotes re-resolve), `internal/tui/note_keys.go` (`loadNotesCmd`)
- Test: `internal/tui/diff_stack_notes_test.go`

**Interfaces:**
- Produces: `type stackNotesMsg struct { gen, idx int; notes []domain.ResolvedNote; err error }`
- Produces: `func (m Model) stackNotesCmd(gen, idx int, d *diffView) tea.Cmd`
- Produces: `func (v *diffView) setNotesFor(i int, ns []domain.ResolvedNote)`
- Consumes: `applyStackFile` (diff_stack.go), `domain.Service.NotesFor` / `PreviewNotesFor`.

Why a NEW message: `notesLoadedMsg` is gated on `m.diffTag` and replaces the VIEW's notes.
A stack has one tag and many files — reusing it would drop 90 % of the answers and write the
last one over the whole view. (Plan 3 learned the same lesson with `diffMsg`.)

- [ ] **Step 1: Write the failing test**

```go
func TestStackLoadsEachFilesNotes(t *testing.T) {
	t.Parallel()
	// a real repo + a real notes dir: one note on file B, none on A
	m, dir := stackRepoModel(t) // diff_stack_load_test.go
	m.svc.UseNotesDir(t.TempDir())
	// …write a note on the second file of the commit through m.svc.AddNote…
	// open the stack, drain every Cmd (drainCmds flattens tea.BatchMsg)
	// assert: file 1's d.notes has 1 entry, file 0's has none,
	//         and a note ROW is spliced inside file 1's range.
	_ = dir
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `go test ./internal/tui -run TestStackLoadsEachFilesNotes -v`
Expected: FAIL — `d.notes` is empty (nothing resolves a stacked file's notes yet).

- [ ] **Step 3: Implement**

`diff_stack.go`:

```go
// stackNotesMsg delivers ONE file's resolved review notes into the stack that
// asked for them. It is deliberately not notesLoadedMsg: that one is gated on
// the view's single diffTag and replaces the WHOLE view's notes, which in a
// stack would drop every answer but the last and write it over file 0.
type stackNotesMsg struct {
	gen, idx int
	notes    []domain.ResolvedNote
	err      error
}

// stackNotesCmd resolves file idx's notes off the UI thread, against the
// address ITS OWN loader stamped (d.noteAddr / d.previewSet) and its own rows.
func (m Model) stackNotesCmd(gen, idx int, d *diffView) tea.Cmd {
	if m.svc == nil || d == nil || d.noteAddr.Path == "" {
		return nil // inert exactly where notes are inert single-file (D1)
	}
	svc, addr, set, rows := m.svc, d.noteAddr, d.previewSet, d.full
	return func() tea.Msg {
		dd := domain.Diff{Result: textdiff.Result{Rows: rows}}
		if set != nil {
			ns, err := svc.PreviewNotesFor(context.Background(), *set, addr.Path, dd)
			return stackNotesMsg{gen: gen, idx: idx, notes: ns, err: err}
		}
		ns, err := svc.NotesFor(context.Background(), addr, dd)
		return stackNotesMsg{gen: gen, idx: idx, notes: ns, err: err}
	}
}
```

`applyStackFile` returns the notes command alongside (change its signature to
`(Model, tea.Cmd)` and update its caller in `model.go`), issued only when the file
loaded cleanly:

```go
	// …after f.d, f.load = msg.view, stackLoaded and the re-anchor…
	ncmd := m.stackNotesCmd(v.stk.gen, msg.idx, f.d)
	return m, ncmd
```

`setNotesFor` + the handler in `model.go`:

```go
// setNotesFor stores one stacked file's resolved notes. The collapse seeding
// stays VIEW-wide: a thread's root id is unique across files, so one set holds
// the whole stack (D6).
func (v *diffView) setNotesFor(i int, ns []domain.ResolvedNote) {
	if v.stk == nil || i < 0 || i >= len(v.stk.files) || v.stk.files[i].d == nil {
		return
	}
	v.stk.files[i].d.notes = ns
	v.seedCollapsed(ns) // the same seeding setNotes does, factored out
}
```

```go
	case stackNotesMsg:
		dv := m.diffLayer()
		if dv == nil || dv.stk == nil || msg.gen != dv.stk.gen || msg.err != nil {
			return m, nil // stale generation, or best-effort failure
		}
		body := m.diffBodyRows()
		hold := dv.anchorAt(dv.curLine)
		dv.stackHold = hold
		dv.setNotesFor(msg.idx, msg.notes)
		dv.rebuild() // note rows are DISPLAY rows: relayout is enough, but rebuild keeps one path
		dv.curLine = dv.lineAt(hold)
		dv.scroll(0, body)
		return m.drainStackNoteLanding(msg.idx, body)
```

(`drainStackNoteLanding` is Task 4's; in this task it is a stub returning `m, nil`.)

Finally `loadNotesCmd` must not fire for a stacked view (its `v.noteAddr` is the SEED's
address — a single file's) and instead re-resolve every LOADED file:

```go
func (m Model) loadNotesCmd() tea.Cmd {
	v := m.diffLayer()
	if v == nil || m.svc == nil {
		return nil
	}
	if v.stk != nil {
		var cmds []tea.Cmd
		for i := range v.stk.files {
			if f := &v.stk.files[i]; f.load == stackLoaded || f.load == stackStale {
				if c := m.stackNotesCmd(v.stk.gen, i, f.d); c != nil {
					cmds = append(cmds, c)
				}
			}
		}
		return tea.Batch(cmds...)
	}
	// …today's single-file body…
}
```

That one change carries every existing refresh site (the srcNotes arrival at
`model.go:1795`, the failed-count path at `1619`, PR comments, saved-pair notes) into the
stack for free — a mutation refreshes every file on screen.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui -run 'TestStack' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git commit -am "feat(tui): every file of a stack resolves its own review notes"
```

---

## Task 3: The note KEYS act on the cursor's file

**Files:**
- Modify: `internal/tui/note_keys.go` (`diffNoteAddress`, `previewNoteSet`),
  `internal/tui/diff_stack_keys.go` (drop the gate), `internal/i18n/lang/*.toml`
- Test: `internal/tui/diff_stack_notes_test.go`

**Interfaces:**
- Produces: `func (v *diffView) curNoteView() *diffView` — the view whose note stamps apply:
  the cursor's `stk.files[curFile()].d` when stacked, `v` otherwise.

- [ ] **Step 1: Write the failing test** — `c` on file B's line opens the note prompt with
  B's address, and the `▸ notes: press S …` notice is gone.

```go
func TestStackNoteKeysUseTheCursorsFile(t *testing.T) {
	t.Parallel()
	m := /* stack with two addressable files, cursor put inside file 1 */
	addr, ok := m.diffNoteAddress()
	if !ok || addr.Path != "b.txt" {
		t.Fatalf("want b.txt's address under the cursor, got %q ok=%v", addr.Path, ok)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `go test ./internal/tui -run TestStackNoteKeysUseTheCursorsFile -v`
Expected: FAIL — the seed's path (file 0) comes back, or `ok` is false.

- [ ] **Step 3: Implement**

```go
// curNoteView is the view whose note stamps (noteAddr, previewSet, notes)
// apply right now: stacked, the file the CURSOR is in — every file of a stack
// has its own address, stamped by the ordinary loader that built it.
func (v *diffView) curNoteView() *diffView {
	if v == nil || v.stk == nil {
		return v
	}
	if d := v.stk.files[v.curFile()].d; d != nil {
		return d
	}
	return nil
}
```

`diffNoteAddress` and `previewNoteSet` read `m.diffLayer().curNoteView()`. In
`diff_stack_keys.go`, delete the whole

```go
	case "c", "E", "R", "}", "{", "o", "O":
```

case so the keys fall through to their ordinary handlers, and **remove the key
`"▸ notes: press S for the single-file view"` from all four bundles** (the i18n gate fails
on an orphan) — editing each file IN PLACE, never re-sorting it.

`notesAtCursor` / `noteAnchorsAtCursor` / `cursorRow` already work: they read `v.cursorRow()`,
which is stacked-aware since plan 3.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui -run 'TestStack|TestNote|TestI18n|Test.*Bundle' -v`
Expected: PASS (the i18n AST gate included).

- [ ] **Step 5: Commit**

```bash
git commit -am "feat(tui): c/E/R act on the file under the cursor in a stack"
```

---

## Task 4: `}` / `{` across the whole stack

**Files:**
- Modify: `internal/tui/note_keys.go` (`jumpNote`, new `stackNoteStep`),
  `internal/tui/diff_stack.go` (`diffStack.land`, `drainStackNoteLanding`)
- Test: `internal/tui/diff_stack_notes_test.go`

**Interfaces:**
- Produces: `type stackLanding struct { file, dir int; side model.NoteSide; no int }`
  on `diffStack.land *stackLanding` — one parked landing, consumed when that file's notes
  (or diff) arrive. `dir != 0` = "land on this file's first/last note"; `no > 0` = "land on
  this exact line" (Task 5 reuses it).

- [ ] **Step 1: Write the failing test** — a stack of three files, a note only in file 2
  (which is still `stackIdle`); `}` from file 0 expands + loads file 2 and lands on the note.

- [ ] **Step 2: Run it and watch it fail** — today `}` finds nothing and does not move.

- [ ] **Step 3: Implement**

`jumpNote` keeps its in-view walk (Task 1 already made `noteLineFrom` see the whole stack's
loaded notes). When it finds nothing and the view is stacked, step files:

```go
// stackNoteStep is the }/{ file step inside a stack: the next file in dir that
// CARRIES notes (NoteCounts — no store read, and it answers for a file whose
// diff has not been fetched), expanded, queued for loading, with the landing
// parked until its notes arrive.
func (m Model) stackNoteStep(v *diffView, dir int) (Model, tea.Cmd, bool) {
	cur := v.curFile()
	for i := cur + dir; i >= 0 && i < len(v.stk.files); i += dir {
		f := &v.stk.files[i]
		if !m.notedStackFile(v, f) {
			continue
		}
		f.collapsed = false
		v.rebuild()
		v.setCursorLine(f.hdr, m.diffBodyRows())
		v.syncStackTitle()
		v.stk.land = &stackLanding{file: i, dir: dir}
		if len(v.notesOf(i)) > 0 {
			return m.drainStackNoteLanding(i, m.diffBodyRows())
		}
		nm, cmd := m.pumpStack() // loads it; its notes follow (Task 2)
		return nm, cmd, true
	}
	return m, nil, false
}
```

`notedStackFile` is `notedFilePath`'s stack twin: the preview scope comes from the FILE
(`f.d.previewSet`, falling back to the stack's seed), the commit key from `v.rev`:

```go
func (m Model) notedStackFile(v *diffView, f *stackFile) bool {
	if len(v.notesOf(indexOf(v, f))) > 0 {
		return true
	}
	if m.previewNoteScope() != nil {
		return m.filesPreviewCounts[f.path] > 0 && !m.previewPathGoneAtTip(f.path)
	}
	if v.rev != "" {
		return m.noteCounts.ByCommitPath[v.rev+":"+f.path] > 0
	}
	return m.noteCounts.ByPath[f.path] > 0
}
```

`drainStackNoteLanding(idx, body)` (the stub from Task 2) consumes a parked landing whose
`file == idx`: `dir != 0` → the file's first/last annotated line
(`noteLineFrom` bounded by `fileLineRange(idx)`); `no > 0` → `lineAnchorIn` + `expandFoldFor`.
It sets `v.noteVisited = true`, centres, and clears `v.stk.land`.

- [ ] **Step 4: Run the tests** — `go test ./internal/tui -run 'TestStackNote' -v`

- [ ] **Step 5: Commit**

```bash
git commit -am "feat(tui): }/{ step note to note across a whole stack"
```

---

## Task 5: A link / steering navigate lands on the LINE

**Files:**
- Modify: `internal/tui/steer_nav.go` (`landSteer`, and wherever the stack branch parks today),
  `internal/tui/diff_stack_keys.go` (`unstack` — D4)
- Test: `internal/tui/steer_nav_test.go`, `internal/tui/diff_stack_notes_test.go`

- [ ] **Step 1: Write the failing tests**
  1. `landSteer` with `{File: "b.txt", Line: {new, 7}}` on a stacked view puts the cursor on
     b.txt's line 7 (not on its header).
  2. `S` out of a stack with the cursor on b.txt line 7 reopens the single view with the
     cursor on line 7 (not on the first change block).

- [ ] **Step 2: Run them and watch them fail** — today both land on a header / first change.

- [ ] **Step 3: Implement**

`landSteer` gains a stacked preamble: find the file by path in `v.stk.files`; if it is
collapsed, expand; if it is not loaded, park `stackLanding{file: i, side, no}` and pump —
the landing is drained by `applyStackFile`; otherwise bound `find` to that file's range:

```go
	lo, hi := 0, len(v.lines)-1
	if v.stk != nil {
		i, ok := v.stackFileIdx(c.File)
		if !ok {
			return m, m.answerSteer(c, steerFail(c, c.File+" is not in this stack"))
		}
		// expand / load / park, then:
		lo, hi = v.fileLineRange(i)
	}
	find := func() (int, bool) { return v.lineAnchorIn(lo, hi, no, old) }
```

`lastLineNo(old)` (the clamp) gets the same bounds treatment — a stacked clamp must clamp to
the FILE's last line, not the stack's.

`unstack` parks the line landing on the reopened single diff. The existing parked-navigation
shape is `m.noteLand` / `drainPendingDiff`; add the smallest twin:

```go
	// Leaving the stack keeps the LINE being read, not the file's first change
	// (D4): the single view is loaded asynchronously, so the landing is parked
	// and drained where the diff arrives.
	if r, ok := v.cursorRow(); ok {
		side, no := model.NoteSideNew, r.RightNo
		if v.onOld || no == 0 {
			side, no = model.NoteSideOld, r.LeftNo
		}
		if no > 0 {
			m.diffLand = &lineLanding{side: side, no: no}
		}
	}
```

drained in the `diffMsg` handler right after `drainPendingDiff` (tag-gated the same way).

- [ ] **Step 4: Run the tests** — `go test ./internal/tui -run 'TestSteer|TestStack' -v`

- [ ] **Step 5: Commit**

```bash
git commit -am "feat(tui): a link, a steer and leaving a stack all land on the line"
```

---

## Task 6: The notes-list popup and the ◆ badges read the stack

**Files:**
- Modify: `internal/tui/notes_list_popup.go`, `internal/tui/diff_notes.go` (badge count)
- Test: `internal/tui/diff_stack_notes_test.go`

- [ ] **Step 1: Write the failing test** — "List notes…" on a stack lists both files' notes,
  each row prefixed with its path, and picking file B's row moves the cursor into file B.
- [ ] **Step 2: Run it and watch it fail.**
- [ ] **Step 3: Implement** — the popup builds its rows from `v.notesOf(i)` over every file
  (D7), carrying `(file, note id)`; its jump expands the file, sets the cursor via
  `lineAnchorIn`, and centres. `Remove all notes…` stays the CURSOR FILE's address (it is a
  per-address operation; say so in its typed-confirm text).
- [ ] **Step 4: Run the tests.**
- [ ] **Step 5: Commit**

```bash
git commit -am "feat(tui): the notes list covers the whole stack"
```

---

## Task 7: The small items

**Files:**
- Modify: `internal/tui/diff_stack_render.go` (D5), `internal/tui/diff_stack_keys.go` (D3),
  `tui-capture.sh`
- Test: `internal/tui/diff_stack_test.go`

- [ ] **Step 1: Write the failing tests**
  1. a conflicted file's header row contains no `+`/`−` counts;
  2. `ctrl+down` inside a loaded file with three change blocks moves to the NEXT CHANGE in
     that file (not to the next file's header), and stops at the file's last change.
- [ ] **Step 2: Run them and watch them fail.**
- [ ] **Step 3: Implement**
  - `stackRow`: `if f.conflict { counts = "" }`.
  - `stackKey`: `case "ctrl+down", "ctrl+up":` walk `v.lines` from the cursor inside
    `fileLineRange(curFile())` to the next line that starts a change run
    (`Row.Kind != textdiff.KindSame` after a Same line, matching the single-file block rule),
    no wrap; a file with no next change keeps the cursor and posts nothing (silent, like the
    single-file boundary before its wrap cue).
  - `tui-capture.sh --state <dir>`: the flag must reach the binary INSIDE tmux — an exported
    variable does not (the tmux server hands new sessions the env it was started with). Build
    the command as `tmux new-session … "env XDG_STATE_HOME=<dir> <gg> …"`, and print the dir
    in the capture header so a snapshot says which state it used.
- [ ] **Step 4: Run the tests** and one capture with `--state` to prove the real
  `~/.local/state/gg/prompts.toml` is untouched:

```bash
cp ~/.local/state/gg/prompts.toml /tmp/prompts.before
./tui-capture.sh --state /tmp/capstate "enter,S" >/dev/null
diff ~/.local/state/gg/prompts.toml /tmp/prompts.before && grep -r stacked /tmp/capstate
```

- [ ] **Step 5: Commit**

```bash
git commit -am "fix(tui): conflicted headers show no counts; ctrl-arrows step changes in a file; tui-capture --state"
```

---

## Task 8: TUI verification + docs (the TUI half is mergeable here)

- [ ] **Step 1** `go test ./internal/tui/...` all green.
- [ ] **Step 2** `./tui-capture.sh --state <tmp>` snapshots: a stacked commit diff with a note
  box under a line of the SECOND file; `}` from the first file landing on it; `c` opening the
  note prompt with the right path in its title; `S` back to single keeping the line.
  Re-run any snapshot that looks like "the key did nothing" before believing it (plan-3 gotcha).
- [ ] **Step 3** Docs: `CHANGELOG.md`, `docs/CLAUDE-details.md` (a "Review notes in a stacked
  diff (plan 4a)" section incl. the gotchas found), `README.md` if the user-facing sentence
  changes, `docs/web-tui-parity.md`.
- [ ] **Step 4** `./test.sh race` (start it immediately — the machine handles parallel runs).
- [ ] **Step 5** Commit; build Linux + Windows verify binaries and send both with absolute paths.

---

## Task 9: Web — the note context becomes explicit

**Files:**
- Modify: `internal/web/static/files.js` (`diffHTML` and the note helpers it calls),
  `internal/web/static/stack.js` (`slotNoteCtx`)
- Test: the node-imported unit test next to the existing `stack` tests

**Interfaces:**
- Produces: `nctx = {ctx, notes, collapsed, row}` — `ctx` is the `state.diffCtx` twin for this
  file, `notes` its resolved notes, `collapsed` its Set, `row` its cursor row (`{side, no}`).
- Produces: `function activeDiff()` — the cursor slot's `nctx` while stacked, else an object
  backed by today's globals.

- [ ] **Step 1: Write the failing test** — `diffHTML(d, w, true, folds, nctx)` renders the
  note box from `nctx.notes`, ignoring `state.notes`.
- [ ] **Step 2: Run it and watch it fail.**
- [ ] **Step 3: Implement** — thread `nctx` (defaulting to the globals object) through
  `diffHTML` → `noteRowsHTML`, `fileNoteRowsHTML`, `curCls`, the `noted` pin of the
  changes-only fold, and `attnKey(nctx.ctx)`. **Pass it explicitly; never a module-level
  "current slot" variable** — a slot's notes arrive asynchronously and would race it.
  Convert only the NOTE-path readers of `state.diffCtx`, not all 47 sites.
- [ ] **Step 4: Run** `node --test internal/web/static/*_test.mjs` (or the repo's usual web
  unit runner) — PASS.
- [ ] **Step 5: Commit**

```bash
git commit -am "refactor(web): the diff renderer takes its note context explicitly"
```

---

## Task 10: Web — a slot fetches, paints and seeds its own notes

**Files:** `internal/web/static/stackview.js`, `stack.js`

- [ ] **Step 1: Write the failing unit test** — `slotNoteCtx(slot)` builds the same shape the
  single-file openers build for `state.diffCtx` (commit: `{rev, path}`; working tree:
  `{state: sectionNoteState(group), path}`; preview/pair/PR: the preview descriptor).
- [ ] **Step 2: Run it and watch it fail.**
- [ ] **Step 3: Implement** — in `load(st, s)`, after the diff lands: build `s.ctx`, fetch
  `/api/notes?…` for it (best-effort, generation-gated on `state.stack !== st`), store
  `s.notes`, seed `s.noteCollapsed` ONCE, then `repaintSlot(st, k)` so the reader's pin
  logic keeps their place. `bodyHTML(s)` passes `notesOn = true` and `s`'s `nctx`.
  Notes are skipped entirely for a slot whose source has no note address (D1/D8).
- [ ] **Step 4: Run the unit tests + the browser probe** (Task 12).
- [ ] **Step 5: Commit**

```bash
git commit -am "feat(web): each stacked file loads and paints its own review notes"
```

---

## Task 11: Web — keys, the row cursor and the line landing

**Files:** `internal/web/static/stackview.js`, `files.js`, `keys.js`

- [ ] **Step 1: Write the failing test / probe expectation** — clicking a row inside file B
  marks it `cur`, makes B the active slot, and `c` adds a note against B's context;
  `}` / `{` step across slots; a `gg://…?line=` landing scrolls to the row, not the header.
- [ ] **Step 2: Run the probe against the UNFIXED build and watch it fail** (memory rule:
  the probe must be able to see its subject fail).
- [ ] **Step 3: Implement** — the row click handler (files.js ~2459-2505) sets the slot's
  `row` and `st.anchor`; note mutations post through `activeDiff().ctx`; `}`/`{` walk the
  slots in order, expanding and loading as the TUI does; the link landing parks
  `{key, side, no}` on the stack and consumes it after that slot's diff paints
  (`scrollIntoView` on the `tr`).
- [ ] **Step 4: Run the probes** (chromium AND firefox — the user reads Firefox screenshots),
  asserting VISIBILITY, against the fixed build.
- [ ] **Step 5: Commit**

```bash
git commit -am "feat(web): note keys, the row cursor and link landings work inside a stack"
```

---

## Task 12: Full verification, docs, delivery

- [ ] **Step 1** Re-run plan 1's and plan 2's probes (`stack-probe/probe.mjs`, `wt.mjs`,
  `sym.mjs`, `mixed.mjs`) — plan 2's gotcha was that only the PREVIOUS plan's probe caught a
  regression. Isolated `XDG_STATE_HOME`, `stdio: "ignore"`, kill the server in a `finally`.
- [ ] **Step 2** `go test ./...`, then `./test.sh race` (started early).
- [ ] **Step 3** Docs: CHANGELOG, README, `docs/CLAUDE-details.md`, `docs/web-tui-parity.md`;
  `internal/agentskill/using-gg.md` only if a CLI surface changed (it should not).
- [ ] **Step 4** Build and send the verify binaries (`gg` Linux, `gg.exe` Windows, and
  `gg-web-new.exe` since the web half changed — memory rule `windows-web-exe-delivery`).
- [ ] **Step 5** Update memory (`stacked-diff-view-feature.md` + any new gotcha file), hand
  the branch to the user to merge, and after their merge run `./build.sh install`.

---

## Self-review

**Spec coverage.** §11 item 1 has four clauses — per-slot `ctx`/`notes`/`row` (Tasks 2, 9, 10),
`activeDiff()` everywhere notes read the globals (Task 9), ◆ rows per file (Tasks 1, 10),
link line-landing (Tasks 5, 11). §5.1's slot fields: `ctx`, `notes`, `row` land here; `hunks`
and `fold` stay for 4c/4d. §5.2's "line-index consumers skip header and placeholder lines"
already holds from plan 3 and is re-tested for note rows in Task 1.

**Placeholders.** The web tasks name behaviours and the exact functions to change but do not
quote whole JS bodies (files.js is 150 KB and the exact lines must be read at execution time);
each web step still names the file, the function and the assertion, and every TUI step carries
its code. Flag at review if the user wants the web tasks expanded before execution.

**Type consistency.** `stackNotesMsg{gen, idx, notes, err}`, `stackLanding{file, dir, side, no}`,
`lineAnchorIn(lo, hi, no, old)`, `noteAnchorLineIn(lo, hi, r)`, `notesOf(i)`, `setNotesFor(i, ns)`,
`curNoteView()`, `stackNotesCmd(gen, idx, d)`, `drainStackNoteLanding(idx, body)` — one spelling
each, used the same way in every task.
