# Stacked diff view — TUI (plan 3) Implementation Plan

> **For agentic workers:** executed INLINE by the session that wrote it (repo rule: never subagents), task by task with superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** `S` in the TUI's full-screen diff view switches to a stacked view: every file of the list the diff was opened from, in ONE scroll. Each file gets a header line (fold mark, status, path or `old → new`, `+a −d`), with its body below. Files load lazily and fold with `-`/`_`. `n`/`p` step header to header.

**Architecture:** `diffView` gains `stk *diffStack`. `nil` means single-file mode and changes nothing today.

- **Stream.** `v.lines` becomes `[]diffLine`, where `diffLine` embeds `textdiff.Line` and adds `file int` and `kind lineKind` (body | header | placeholder). The pure `textdiff` leaf is never touched. Every `.Row`/`.Fold` read compiles unchanged. When stacked, `rebuild()` splices the stream from `v.stk.files[i].d`, the per-file `*diffView` a loader built. `v.blocks` = the header line indices, so the existing `n`/`p`/wrap/`focusBlock`/`deriveOrdinal` machinery steps header to header with no new code. Tokens and the gutter resolve per line's file.
- **Loading.** Files load by wrapping the EXISTING single-file loader Cmds. Their `diffMsg` is rewrapped as `stackFileMsg{gen, idx, view}`, gated on the stack generation. The `diffMsg` handler (`*dv = *msg.view`) never sees a stack.
- **Per-file actions.** `v.title` is kept synced to the cursor's file. Every per-file action that reads `v.title`/`v.rev` then works unchanged: h, b, e, copy, bookmark, export, L.
- **Notes.** Inert on a stack in v1: `noteAddr` stays zero, the existing "no address" convention.

**Tech Stack:** Go 1.26, Bubble Tea, lipgloss; `internal/tui`, `internal/promptstate`, `internal/i18n/lang/*.toml`.

**Spec:** `docs/superpowers/specs/2026-09-22-stacked-diff-view-design.md` (§4, §5.2, §6.2, §6.3, §7, §8 TUI, §9, §12 TUI). Rulings R1–R10 are settled.

## Global Constraints

- Every user-visible TUI string goes through `i18n.T` with a literal key, and the key is added to ALL FOUR bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`) **in the same task**. The AST gates run in `go test ./internal/tui`.
- New tests call `t.Parallel()`. Repos are built via `testRepo(t, dir)`, never a raw `NewExecRunner`.
- Single-file mode must stay byte-identical: every existing `internal/tui` test stays green in every task.
- The diff footer stays ≤ 140 columns (`diff_search_test.go:449`); the stacked footer also stays ≤ 140.
- The TUI pref is independent of the web's `stacked_diff` (R7): its own promptstate field.
- `internal/tui` never imports `internal/git`.
- Commit per task in the worktree `.claude/worktrees/stacked-diff-tui`, branch `feat/stacked-diff-tui`.
- **"Watch it fail" = guard removed, not feature removed** (memory `assertion-cannot-see-its-subject`).

## Decisions this plan makes: spec collisions, for your ruling at review

1. **`g`/`G` are taken in the diff view** (the global bookmark / shelf switchers, `diff_view.go` `case "g"/"G"`), so they stay as they are.
   - Stack top / bottom = **home / end**. They already mean top / bottom of the file.
   - In a stack, home/end never arm a file-step: the whole set is on screen, so there is no "next file" to step to.
   - **N/P** are inert in a stack, because n/p already step files.
2. **The cursor may rest on a header line** (not on a placeholder).
   - The spec says line consumers "skip" headers. Taken literally, a folded file (header only) could never be the cursor's file, so `-` could not unfold it with the cursor.
   - So: `j`/`k` stop on headers. `cursorRow()` returns `(Row{}, false)` on a header or placeholder. Through the existing `hadRow` convention, the notes anchor, copy line, `e`, the selection text and search then all skip headers, as the spec intends.
   - Placeholders are skipped by every mover (like folds).
3. **`n`/`p` in a stack step headers.** Change blocks inside a file (`ctrl+↓/↑` alias included) are not separately steppable in v1. `v.blocks` = headers.
4. **`S` lives in the diff view only.** In the files view and base layout, `S` is already the stash window toggle.
   - With the pref on, `enter` on a file opens the stack scrolled to that file. That is how "S works from the file list" is met.
   - Also a `.` menu row in the diff view.
5. **Auto-collapse above 100 files folds every file EXCEPT the one you opened**, so `enter` always shows what you picked.
6. **Sources that stack:** the files view (commit, compare / entry / bookmark / shelf / link / preview) and the Status / Staged panels.
   - The **full-tree** mode (every file of a commit vs the working tree) is not a change set and does not stack.
   - Neither does a picker compare with no list (`diffNavNone`). There, `S` posts "▸ nothing to stack here".
7. **Notes are inert on a stack** (follow-up 4a). `c`/`E`/`R`/`}`/`{`/`o`/`O` post "▸ notes: press S for the single-file view". Selection copy skips header and placeholder lines; a selection may span files (per-file clamp is 4d).
8. **Footer trade:** in single mode, `[e] edit` leaves the footer for `[S] stack`. `e` stays in help and in the `.` menu's editor row. The stacked footer is its own line.
9. **Working-tree refresh:** a status refresh marks loaded files *stale*. Their old body stays on screen, and the lazy queue refetches stale files near the viewport. Nothing flickers, and a background refresh costs ≤ 3 reads.
   - Gone files drop out; new files are inserted in list order.
   - If the section empties, the view closes back to the list.
10. **Lexing:** already inside `differ.Diff`, i.e. inside the loader Cmd, so off the UI thread. No separate lex command is needed.

---

## File map

| File | Responsibility |
|------|----------------|
| `internal/promptstate/{file_store,store}.go` + new `stacked.go` | `StackedDiff() bool` / `SetStackedDiff(bool) error` |
| `internal/tui/diff_stack.go` (new) | `diffStack`, `stackFile`, `diffLine`/`lineKind`, `spliceStack`, `wantLoads`, anchor save/restore, stack builders, `openStack`, `stackFileMsg`, pump, reconcile, counts |
| `internal/tui/diff_stack_keys.go` (new) | `stackKey`: S / - / _ / J / enter / home / end / N / P / notes-inert; the `.` menu rows; `toggleStacked` |
| `internal/tui/diff_stack_render.go` (new) | header + placeholder row painting; stacked footer |
| `internal/tui/diff_view.go` | `lines []diffLine`; `rebuild`/`relayout` branch on `stk`; `toksFor`, `gutter()`; key-handler hook |
| `internal/tui/diff_render.go` | render uses `v.gutter()`/`toksFor`; stacked rows; header status "file %d/%d"; footer `[S]` |
| `internal/tui/diff_cursor.go`, `diff_select.go`, `diff_edit.go` | `isStop`/`isBody` instead of `Fold > 0` |
| `internal/tui/files_view.go` | `treeFileLoad(l)` factored out of `openDiffForFileLine`; pref-on → `openStack` |
| `internal/tui/diff_view.go` (`openStatusDiff`) | pref-on → `openStack` |
| `internal/tui/model.go` | `diffStacked` field + load at start; `stackFileMsg`/`stackStatMsg` cases; status-arrival reconcile + pump |
| `internal/tui/help.go`, `action_menu.go` | help rows, `.` menu rows |
| `internal/i18n/lang/*.toml` | every new key |
| docs | CHANGELOG, README, `docs/CLAUDE-details.md`, `docs/web-tui-parity.md` |

---

### Task 1: The pref — promptstate + Model field

**Files:**
- Create: `internal/promptstate/stacked.go`, `internal/promptstate/stacked_test.go`
- Modify: `internal/promptstate/file_store.go` (records field), `internal/promptstate/store.go` (interface), `internal/tui/model.go` (field + init)

**Interfaces:**
- Produces:
  - `(*FileStore).StackedDiff() bool`, `(*FileStore).SetStackedDiff(on bool) error`; both are on `promptstate.Store`.
  - `Model.diffStacked bool`, read once in `New` from `m.promptStore` (nil store → false).

- [ ] **Step 1: Failing test**

```go
// internal/promptstate/stacked_test.go
func TestStackedDiffRoundTripsAndIsIndependentOfWebUI(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	if fs.StackedDiff() {
		t.Fatal("fresh store must read false")
	}
	if err := fs.SetStackedDiff(true); err != nil {
		t.Fatal(err)
	}
	if !fs.StackedDiff() {
		t.Fatal("set true must read back true")
	}
	if _, ok := fs.WebUIState(); ok {
		t.Fatal("the TUI pref must not create a web_ui record (R7)")
	}
	_ = fs.SuppressPrompt("x") // a sibling write must keep it (read-merge)
	if !fs.StackedDiff() {
		t.Fatal("a sibling write dropped the pref")
	}
}
```

- [ ] **Step 2:** `go test ./internal/promptstate -run StackedDiff`. Expected: compile FAIL.
- [ ] **Step 3: Implement**

```go
// file_store.go records:
	// StackedDiff is the TUI diff view's stacked mode (S). Independent of the
	// web's web_ui.stacked_diff (spec R7).
	StackedDiff bool `toml:"tui_stacked_diff,omitempty"`

// stacked.go
package promptstate

// StackedDiff reports whether the TUI diff view opens stacked (spec R1/R7).
func (fs *FileStore) StackedDiff() bool { return fs.read().StackedDiff }

// SetStackedDiff persists the TUI stacked-diff pref (read-merge-write).
func (fs *FileStore) SetStackedDiff(on bool) error {
	r := fs.read()
	r.StackedDiff = on
	return fs.write(r)
}
```

Add both methods to the `Store` interface, with doc lines. In `tui.New`, after `promptStore` is set: `if m.promptStore != nil { m.diffStacked = m.promptStore.StackedDiff() }`. Field in `Model` next to `diffPartial`:

```go
	diffStacked bool // S: the diff view opens stacked (every file of the list in one scroll); promptstate-backed
```

- [ ] **Step 4:** `go test ./internal/promptstate ./internal/tui` — PASS. Check that tui tests never read the user's real prompts file: grep `defaultPromptStore` and confirm the tui `TestMain` isolates `XDG_STATE_HOME`. If it does not, the Model init reads through a nil-safe path only when the store is non-nil, and tests that need the pref set a temp `FileStore`.
- [ ] **Step 5: Commit** `feat(promptstate): TUI stacked-diff pref, independent of the web's`

---

### Task 2: `diffLine`, a stream type that can carry headers (mechanical refactor)

**Files:**
- Create: `internal/tui/diff_stack.go` (types only for now)
- Modify: `internal/tui/diff_view.go` (`lines` type, `rebuild`), `diff_render.go` (`maxCellWidth` signature), the tests that build `lines` literally (`diff_layer_flip_test.go:63`, `diff_render_test.go:363`, `steer_nav_test.go:24`)

**Interfaces:**
- Produces:

```go
// lineKind says what a v.lines entry is. The zero value is a body line, so the
// single-file stream is unchanged.
type lineKind uint8

const (
	lineBody   lineKind = iota // a diff row or a fold (textdiff.Line as before)
	lineHeader                 // a stacked file's header (file = its index)
	linePlace                  // a stacked file's one-line state: loading / binary / too large / error / no difference / conflict
)

// diffLine is one logical line of the diff stream. It embeds textdiff.Line so
// every .Row/.Fold read is unchanged; file and kind only mean something when
// the view is stacked.
type diffLine struct {
	textdiff.Line
	file int
	kind lineKind
}

func (l diffLine) isBody() bool { return l.kind == lineBody && l.Fold == 0 }

func wrapLines(ls []textdiff.Line) []diffLine // file 0, lineBody
```

- `maxCellWidth(lines []diffLine) int` skips non-body lines.

- [ ] **Step 1:** Change `lines []textdiff.Line` → `lines []diffLine`. In `rebuild()`, wrap `textdiff.Collapse`/`Expand` output with `wrapLines`. Fix the three test literals: `[]textdiff.Line{…}` → `wrapLines([]textdiff.Line{…})`.
- [ ] **Step 2:** `go build ./... && go test ./internal/tui` — PASS. It is a pure refactor, so every existing test is the gate.
- [ ] **Step 3: Commit** `refactor(tui): diff stream lines carry a file index and a kind`

---

### Task 3: The stack model + splice + render (pure; no loading yet)

**Files:**
- Modify: `internal/tui/diff_stack.go`, `diff_view.go` (`stk` field, `rebuild`/`relayout` branch, `toksFor`, `gutter`, `textWidth`), `diff_render.go` (use `v.gutter()`/`toksFor`, stacked header/placeholder rows, top-line "file i/N", skip the whole-body loading switch when stacked)
- Create: `internal/tui/diff_stack_render.go`, `internal/tui/diff_stack_test.go`
- i18n (all four bundles): `"  (not loaded yet)"`, `"  conflict — enter opens the resolver"`, `"+%d −%d"`, `"bin"`, `"file %d/%d"`. The existing `"  (loading…)"`, `"  (binary file)"`, `"  (file too large)"`, `"  error: %s"`, `"  (no content difference)"` are reused.

**Interfaces:**
- Produces:

```go
type stackLoad uint8

const (
	stackIdle    stackLoad = iota // never requested
	stackLoading                  // a loader Cmd is out
	stackLoaded                   // d holds the result
	stackStale                    // loaded, but a status refresh says re-read (d still shown)
)

// stackFile is one file of a stack — the TUI twin of the web slot (spec §5.2).
// d is the per-file view a single-file loader built (rows, tokens, binary,
// tooLarge, err, truncated); the stack never renders d directly, it splices d's
// rows into its own stream.
type stackFile struct {
	path, oldPath, status string
	line      contentLine      // tree source: the files-view row (loader input)
	fs        model.FileStatus // status source: the panel entry (loader input)
	conflict  bool             // unmerged: header + one resolver line, never fetched
	collapsed bool
	load      stackLoad
	d         *diffView
	add, del  int
	counted   bool // add/del known (numstat or rows)
	bin       bool // numstat says binary
	start     int  // index of this file's header in v.lines (set by spliceStack)
}

type diffStack struct {
	gen      int         // tags stackFileMsg / stackStatMsg; a new stack = a new gen
	src      diffNavKind // diffNavTree | diffNavStatus | diffNavStaged
	files    []stackFile
	inflight int
}

func (v *diffView) spliceStack()                          // lines + blocks from files; sets files[i].start
func (v *diffView) curFile() int                          // lines[curLine].file (0 when empty)
func (v *diffView) toksFor(li int) (old, nw [][]syntax.Tok)
func (v *diffView) gutter() int                           // max gutterWidth over loaded files when stacked
func stackCollapseAll(files []stackFile, keep int)        // >100 rule helper
const stackCollapseOver = 100
```

- [ ] **Step 1: Failing tests** (`diff_stack_test.go`). Use a helper that builds a stacked view from fake loaded files:

```go
// stackViewOf builds a stacked view over files whose d is preset from rows
// (nil rows = not loaded). width 120.
func stackViewOf(t *testing.T, rows ...[]textdiff.Row) *diffView {
	t.Helper()
	st := &diffStack{gen: 1, src: diffNavTree}
	for i, r := range rows {
		f := stackFile{path: fmt.Sprintf("f%d.go", i), status: "M"}
		if r != nil {
			f.d, f.load = diffViewWith(r, blocksOf(r)), stackLoaded
		}
		st.files = append(st.files, f)
	}
	v := &diffView{stk: st, width: 120}
	v.rebuild()
	return v
}

func TestStackSpliceHeadersThenBodies(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, sameRowsTUI(3, 1), nil, sameRowsTUI(2, 0))
	kinds := []lineKind{}
	for _, l := range v.lines {
		kinds = append(kinds, l.kind)
	}
	want := []lineKind{lineHeader, lineBody, lineBody, lineBody, lineHeader, linePlace, lineHeader, lineBody, lineBody}
	if !slices.Equal(kinds, want) {
		t.Fatalf("stream kinds = %v, want %v", kinds, want)
	}
	if !slices.Equal(v.blocks, []int{0, 4, 6}) {
		t.Fatalf("blocks must be the header indices, got %v", v.blocks)
	}
	if v.lines[7].file != 2 || v.stk.files[2].start != 6 {
		t.Fatal("file index / start not stamped")
	}
}

func TestStackCollapsedFileIsHeaderOnly(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, sameRowsTUI(3, 1), sameRowsTUI(2, 0))
	v.stk.files[0].collapsed = true
	v.rebuild()
	if len(v.lines) != 1+1+2 || v.lines[1].kind != lineHeader {
		t.Fatalf("a folded file must contribute its header only: %d lines", len(v.lines))
	}
}

// Tokens are keyed by SOURCE line number: line 1 of file 1 must paint file 1's
// runs, never file 0's (the trap a shared v.oldTok/newTok would spring).
func TestStackTokensResolvePerFile(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, sameRowsTUI(1), sameRowsTUI(1))
	v.stk.files[0].d.newTok = [][]syntax.Tok{{{Start: 0, End: 1, Class: syntax.Keyword}}}
	v.stk.files[1].d.newTok = [][]syntax.Tok{{{Start: 0, End: 1, Class: syntax.String}}}
	_, nw := v.toksFor(3) // file 1's body line
	if got := tokAt(nw, 1); len(got) != 1 || got[0].Class != syntax.String {
		t.Fatalf("file 1 line 1 painted with %v", got)
	}
}

func TestStackRenderPaintsHeaderAndPlaceholder(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.height, m.width = 20, 120
	v := stackViewOf(t, sameRowsTUI(2, 0), nil)
	v.stk.files[1].oldPath, v.stk.files[1].status = "old.go", "R"
	*m.diffLayer() = *v
	out := m.renderDiffView()
	for _, want := range []string{"▾ M  f0.go", "▾ R  old.go → f1.go", "(not loaded yet)", "file 1/2"} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
}
```

(`blocksOf(rows)` is a small test helper returning the indices of non-Same rows that start a run. Put it in `diff_stack_test.go`.)

- [ ] **Step 2:** Run `go test ./internal/tui -run 'TestStack'`. Expected: compile FAIL.
- [ ] **Step 3: Implement**
  - `rebuild()`: `if v.stk != nil { v.sanLeft, v.sanRight = nil, nil; v.lsel.clear(); v.spliceStack(); v.relayout(v.width); v.refindAfterRebuild(); return }`.
  - `spliceStack` (per file: header; skip if collapsed; conflict → placeholder; `d == nil`, or `d.err`/`binary`/`tooLarge` → placeholder; else `Collapse` when partial or `Expand`, each wrapped with `file: i`; empty body → placeholder):

```go
func (v *diffView) spliceStack() {
	v.lines, v.blocks = v.lines[:0], v.blocks[:0]
	for i := range v.stk.files {
		f := &v.stk.files[i]
		f.start = len(v.lines)
		v.blocks = append(v.blocks, f.start)
		v.lines = append(v.lines, diffLine{file: i, kind: lineHeader})
		if f.collapsed {
			continue
		}
		var body []textdiff.Line
		if d := f.d; !f.conflict && d != nil && d.err == nil && !d.binary && !d.tooLarge {
			if v.partial {
				body, _ = textdiff.Collapse(d.full, d.fullBlocks, diffContext)
			} else {
				body = textdiff.Expand(d.full)
			}
		}
		if len(body) == 0 {
			v.lines = append(v.lines, diffLine{file: i, kind: linePlace})
			continue
		}
		for _, l := range body {
			v.lines = append(v.lines, diffLine{Line: l, file: i})
		}
	}
	if v.stk.files == nil {
		v.blocks = nil
	}
}
```

  - `relayout`: header and placeholder lines → one `dRow{line: li, first: true, kind: k, file: f}`. Add `kind lineKind; file int` to `dRow`. Replace `tokAt(v.oldTok…)`/`tokAt(v.newTok…)` with `ot, nt := v.toksFor(li)`. Replace `gutterWidth(v.full)` with `v.gutter()` here, in `textWidth` and in `diffPaneLines`.
  - `diffPaneLines`: `if dr.kind != lineBody { out = append(out, m.stackRow(v, dr, w, i >= curStart && i < curEnd)); continue }`.
  - `renderDiffView`: when `v.stk != nil`, skip the `loading/err/binary/tooLarge` switch arm (always paint panes). Drop the `(no content difference)` note. `right` starts with `i18n.T("file %d/%d", v.curFile()+1, len(v.stk.files))` instead of `change`.
  - `diff_stack_render.go`:

```go
// stackRow paints a header or placeholder row full width. The header reads
// "▾ M  path  +a −d" (▸ when folded; "old → new" on a rename); a cursor on it
// renders it in the cursor-row style so j/k onto a header is visible.
func (m Model) stackRow(v *diffView, dr dRow, w int, onCursor bool) string {
	f := v.stk.files[dr.file]
	s := st()
	if dr.kind == linePlace {
		return truncate(s.diffFold.Render(v.placeText(f)), w)
	}
	mark := "▾"
	if f.collapsed {
		mark = "▸"
	}
	name := f.path
	if f.oldPath != "" {
		name = f.oldPath + " → " + f.path
	}
	text := mark + " " + f.status + "  " + name
	switch {
	case f.bin || (f.d != nil && f.d.binary):
		text += "  " + i18n.T("bin")
	case f.counted:
		text += "  " + i18n.T("+%d −%d", f.add, f.del)
	}
	style := s.pickerLabel.Bold(true)
	if onCursor {
		style = s.diffCursorRow.Bold(true)
	}
	return style.Render(padRight(truncate(text, w), w))
}

// placeText is a file's one-line body when it has no rows to show.
func (v *diffView) placeText(f stackFile) string {
	switch {
	case f.conflict:
		return i18n.T("  conflict — enter opens the resolver")
	case f.load == stackLoading && f.d == nil:
		return i18n.T("  (loading…)")
	case f.d == nil:
		return i18n.T("  (not loaded yet)")
	case f.d.err != nil:
		return i18n.T("  error: %s", f.d.err.Error())
	case f.d.binary:
		return i18n.T("  (binary file)")
	case f.d.tooLarge:
		return i18n.T("  (file too large)")
	}
	return i18n.T("  (no content difference)")
}
```

  - `toksFor(li)`: stacked and in range with `d != nil` → `d.oldTok, d.newTok`; stacked otherwise → `nil, nil`; single → `v.oldTok, v.newTok`.
  - `gutter()`: single → `gutterWidth(v.full)`; stacked → max of `gutterWidth(f.d.full)` over loaded files (min 3).
- [ ] **Step 4:** `go test ./internal/tui` — PASS (the i18n gates included).
- [ ] **Step 5: Watch-it-fail:** make `toksFor` return `v.oldTok, v.newTok` unconditionally. `TestStackTokensResolvePerFile` must go RED. Restore.
- [ ] **Step 6: Commit** `feat(tui): stacked diff stream — header and placeholder lines per file`

---

### Task 4: Cursor, search and selection over a stacked stream

**Files:**
- Modify: `diff_cursor.go` (`cursorRow`, `snapOffFold`, `moveCursor`, `setCursorDisp`, `pageCursor`, `selectedLines`), `diff_view.go` (`searchLines`), `diff_edit.go` (`editLine` stays in the file)
- Test: `diff_stack_test.go`

**Interfaces:**
- Produces:
  - `func (l diffLine) isStop() bool { return l.Fold == 0 && l.kind != linePlace }` — a line the cursor may rest on: body rows and headers.
  - `cursorRow()` returns `false` unless `lines[curLine].isBody()`.

- [ ] **Step 1: Failing tests**

```go
func TestStackCursorStopsOnHeadersSkipsPlaceholders(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, sameRowsTUI(1), nil, sameRowsTUI(1))
	// lines: H0 B H1 P H2 B
	v.setCursorLine(1, 10)
	v.moveCursor(1, 10)
	if v.curLine != 2 || v.lines[2].kind != lineHeader {
		t.Fatalf("j from a body row must land on the next header, got %d", v.curLine)
	}
	if _, ok := v.cursorRow(); ok {
		t.Fatal("cursorRow must be false on a header")
	}
	v.moveCursor(1, 10)
	if v.curLine != 4 {
		t.Fatalf("j must skip the placeholder to header 2, got %d", v.curLine)
	}
}

func TestStackSearchAndSelectionSkipNonBody(t *testing.T) {
	t.Parallel()
	a := []textdiff.Row{{Kind: textdiff.Same, Left: "needle", Right: "needle", LeftNo: 1, RightNo: 1}}
	v := stackViewOf(t, a, nil, a)
	for _, sl := range v.searchLines() {
		if v.lines[sl.row].kind != lineBody {
			t.Fatalf("search indexed a %v line", v.lines[sl.row].kind)
		}
	}
	v.lsel.start(0)
	v.curLine = len(v.lines) - 1
	if got := v.selectedLines(); !slices.Equal(got, []string{"needle", "needle"}) {
		t.Fatalf("selection copied %q, want the two body lines only", got)
	}
}
```

- [ ] **Step 2:** Run the tests. Expected FAIL.
- [ ] **Step 3: Implement.** Every `v.lines[j].Fold > 0` / `Fold == 0` test in the movers becomes `!v.lines[j].isStop()` / `isStop()`. `cursorRow` checks `isBody()`. `searchLines` and `selectedLines` skip `!isBody()`. `setCursorDisp` refuses `linePlace` rows (`dr.kind == linePlace`). `editLine` only scans lines with the cursor's `file`.
- [ ] **Step 4:** `go test ./internal/tui` — PASS.
- [ ] **Step 5: Commit** `feat(tui): the diff cursor stops on stack headers; search and selection read body lines only`

---

### Task 5: Loading — loader factoring, the queue, arrival + remap

**Files:**
- Modify: `files_view.go` (extract `treeFileLoad`), `diff_stack.go` (`stackFileMsg`, `wantLoads`, `pumpStack`, anchors), `model.go` (`case stackFileMsg`), `diff_view.go` (`(v *diffView) update` pumps after keys), resize/mouse paths pump
- Test: `diff_stack_test.go`, plus a real-repo loader test

**Interfaces:**
- Produces:

```go
// treeFileLoad is openDiffForFileLine's loader choice without the layer
// bookkeeping: the Cmd (yielding a diffMsg), its tag, and the context line.
// ok is false for full-tree mode (not a change set — never stacked).
func (m Model) treeFileLoad(l contentLine) (cmd tea.Cmd, tag, context string, ok bool)

type stackFileMsg struct {
	gen, idx int
	view     *diffView
}

// wantLoads picks which files to request now: not collapsed, not conflict,
// idle or stale, whose header lies within [lo, hi] (logical-line window
// = the viewport ± 2 screens); nearest-first from `at` (the viewport's top
// line), at most max-inflight. Pure.
func wantLoads(files []stackFile, lo, hi, at, inflight int) []int

const stackMaxInflight = 3

func (m Model) pumpStack() (Model, tea.Cmd) // marks picked files loading, returns the batch

// stackAnchor is a stream position that survives a re-splice.
type stackAnchor struct{ file, inFile, sub int } // inFile -1 = the header; sub = display rows below the line's first
func (v *diffView) anchorAt(li int) stackAnchor
func (v *diffView) lineAt(a stackAnchor) int
```

- [ ] **Step 1: Failing tests**

```go
func TestWantLoadsNearestFirstCappedAtThree(t *testing.T) {
	t.Parallel()
	files := make([]stackFile, 8)
	for i := range files {
		files[i].start = i * 10
	}
	files[2].collapsed = true
	files[3].conflict = true
	got := wantLoads(files, 0, 60, 30, 0)
	if !slices.Equal(got, []int{4, 5, 1}) { // distance from line 30 (2 folded, 3 conflict skipped): 4→10, 5→20, 1→20; a tie goes below-first
		t.Fatalf("got %v", got)
	}
	if got := wantLoads(files, 0, 60, 30, 2); len(got) != 1 {
		t.Fatalf("2 in flight leaves room for 1, got %v", got)
	}
}

// A file above the viewport loading must not move what the user reads: the
// cursor's row and its screen position are unchanged (spec §6.2).
func TestStackLateLoadAboveViewportKeepsScreen(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.height, m.width = 12, 120
	v := stackViewOf(t, nil, sameRowsTUI(40, 5), sameRowsTUI(40, 5))
	*m.diffLayer() = *v
	dv := m.diffLayer()
	body := m.diffBodyRows()
	dv.setCursorLine(dv.stk.files[2].start+10, body)
	dv.alignCursor(alignCenter, body)
	row, _ := dv.cursorRow()
	screen := dv.lineStart[dv.curLine] - dv.offset

	late := diffViewWith(sameRowsTUI(30, 3), []int{3})
	u, _ := m.Update(stackFileMsg{gen: dv.stk.gen, idx: 0, view: late})
	dv = u.(Model).diffLayer()
	if r2, _ := dv.cursorRow(); r2 != row || dv.curFile() != 2 {
		t.Fatalf("cursor moved rows: %+v → %+v (file %d)", row, r2, dv.curFile())
	}
	if s2 := dv.lineStart[dv.curLine] - dv.offset; s2 != screen {
		t.Fatalf("screen position %d → %d", screen, s2)
	}
}

func TestStackStaleGenIsDropped(t *testing.T) {
	t.Parallel()
	m := diffModel()
	*m.diffLayer() = *stackViewOf(t, nil)
	u, _ := m.Update(stackFileMsg{gen: 99, idx: 0, view: diffViewWith(sameRowsTUI(2, 0), []int{0})})
	if u.(Model).diffLayer().stk.files[0].d != nil {
		t.Fatal("a stale generation must not land")
	}
}
```

Plus `TestStackLoadsARealCommit` (real repo via `testRepo`): a commit touching 3 files → `openStack` from a files view → run the returned Cmds (`drainCmds` helper: execute a batch, feed each msg back through `Update`, to a fixed point, max 20 rounds). Assert every file ends `stackLoaded` with rows, and `inflight == 0`.

- [ ] **Step 2:** Run. Expected FAIL.
- [ ] **Step 3: Implement**
  - `treeFileLoad`: move the three branches of `openDiffForFileLine` (compare / shelf / commit) into it. Full-tree → `ok=false`. `openDiffForFileLine` calls it, keeping its layer and context writes, so its behaviour is identical.
  - `stackCmd(gen, idx int, c tea.Cmd) tea.Cmd` wraps: `if d, ok := c().(diffMsg); ok { return stackFileMsg{gen, idx, d.view} }; return nil`.
  - `pumpStack`:
    - `lo/hi` = the logical lines of `disp[offset]` and of `disp[min(offset+body, len)-1]`, ±2 × body.
    - `at` = the top line.
    - For each picked file: `load = stackLoading`, `inflight++`, build its Cmd:
      - tree source → `treeFileLoad(f.line)`;
      - status source → `m.loadStatusDiffCmd(f.fs, src == diffNavStaged)`.
  - `stackFileMsg` handler:
    - gate on `dv != nil && dv.stk != nil && msg.gen == dv.stk.gen`;
    - set `f.d`, `f.load = stackLoaded`, `inflight--`;
    - if `!f.counted`, count Add/Del/Changed rows into add/del and set `counted`;
    - then `a := dv.anchorAt(dv.curLine); top := dv.anchorAt(dv.disp[dv.offset].line)` with `top.sub = dv.offset - dv.lineStart[that line]`, then `rebuild`;
    - then `curLine = lineAt(a)`, `offset = lineStart[lineAt(top)] + top.sub`, `scroll(0, body)`;
    - then `syncStackTitle`, then `pumpStack`.
  - `anchorAt(li)`: `file = lines[li].file`; `inFile = li - files[file].start - 1` (−1 for the header).
  - `lineAt(a)`: clamps `inFile` into the file's current span. A folded file → its header.
  - `(v *diffView) update`: after `updateDiffViewKey`, if the layer is still stacked, `pumpStack` and batch. Mouse wheel over a diff and `tea.WindowSizeMsg` do the same (grep the diff relayout on resize in `model.go`).
- [ ] **Step 4:** `go test ./internal/tui` — PASS.
- [ ] **Step 5: Watch-it-fail:** in the handler, skip the anchor restore. `TestStackLateLoadAboveViewportKeepsScreen` must go RED. Restore.
- [ ] **Step 6: Commit** `feat(tui): stacked files load lazily near the viewport, at most three at a time, without moving the screen`

---

### Task 6: Opening, toggling and the stack keys

**Files:**
- Create: `internal/tui/diff_stack_keys.go`
- Modify: `diff_stack.go` (`openStack`, `buildTreeStack`, `buildStatusStack`, `syncStackTitle`), `files_view.go` (`openDiffForFileLine` pref branch), `diff_view.go` (`openStatusDiff` pref branch; `updateDiffViewKey` calls `stackKey` first when stacked), `diff_filenav.go` (`boundaryCue` → "" when stacked)
- i18n: `"▸ nothing to stack here"`, `"▸ notes: press S for the single-file view"`, `"Stacked diff: on"`, `"Stacked diff: off"`, `"Fold / unfold this file"`, `"Fold / unfold all files"`, `"Jump to file…"`, `"▸ stacked: %d files"`, `"▸ single file: %s"`

**Interfaces:**
- Produces:

```go
// openStack opens (or converts the open diff into) a stack over the list the
// diff comes from, scrolled to focusPath. seed, when non-nil and loaded, is the
// single view of focusPath — its rows are reused, and the cursor keeps its line.
func (m Model) openStack(src diffNavKind, focusPath string, seed *diffView) (Model, tea.Cmd)
func (m Model) buildTreeStack() []stackFile                  // filesView.visible() rows with a path
func (m Model) buildStatusStack(staged bool) []stackFile     // displayIndices(panel), unmerged as conflict
func (m Model) toggleStacked() (tea.Model, tea.Cmd)          // S
func (m Model) stackKey(v *diffView, msg tea.KeyMsg, body int) (tea.Model, tea.Cmd, bool)
func (v *diffView) syncStackTitle()                          // v.title = the cursor file's path
```

- `Model.stackSeq int`: the gen counter (each `openStack` bumps it).

- [ ] **Step 1: Failing tests** (use `treeDiffModel` / `statusDiffModelMulti` from `diff_filenav_test.go`; set `m.promptStore` to a temp `FileStore`):

```go
func TestSFlipsSingleToStackKeepingTheFile(t *testing.T) {
	t.Parallel()
	m := treeDiffModelBlocks(2) // single view on b.go
	m.promptStore = promptstate.NewFileStore(filepath.Join(t.TempDir(), "p.toml"))
	m.diffLayer().title = "b.go"
	m.diffLayer().setCursorLine(20, m.diffBodyRows())
	u, cmd := m.Update(keyMsg("S"))
	mm := u.(Model)
	v := mm.diffLayer()
	if v.stk == nil || len(v.stk.files) != 3 {
		t.Fatal("S must open a stack of the three tree files")
	}
	if v.curFile() != 1 || v.title != "b.go" {
		t.Fatalf("stack must sit on b.go, file %d title %q", v.curFile(), v.title)
	}
	if r, ok := v.cursorRow(); !ok || r.RightNo != 21 {
		t.Fatalf("the seeded file keeps its cursor line, got %+v", r)
	}
	if !mm.diffStacked || !mm.promptStore.StackedDiff() {
		t.Fatal("S must set and persist the pref")
	}
	if cmd == nil {
		t.Fatal("opening a stack must start loading its neighbours")
	}
	u2, _ := mm.Update(keyMsg("S"))
	if v2 := u2.(Model).diffLayer(); v2.stk != nil || v2.title != "b.go" {
		t.Fatal("S again must return to the single view on the same file")
	}
}

func TestStackNPStepHeadersAndFoldKeys(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.height = 30
	*m.diffLayer() = *stackViewOf(t, sameRowsTUI(3, 1), sameRowsTUI(3, 1), sameRowsTUI(3, 1))
	for _, want := range []int{1, 2} {
		u, _ := m.Update(keyMsg("n"))
		m = u.(Model)
		if got := m.diffLayer().curFile(); got != want || m.diffLayer().lines[m.diffLayer().curLine].kind != lineHeader {
			t.Fatalf("n → file %d header, got file %d", want, got)
		}
	}
	u, _ := m.Update(keyMsg("-"))
	m = u.(Model)
	if !m.diffLayer().stk.files[2].collapsed {
		t.Fatal("- must fold the cursor's file")
	}
	u, _ = m.Update(keyMsg("_"))
	m = u.(Model)
	for _, f := range m.diffLayer().stk.files {
		if !f.collapsed {
			t.Fatal("_ with some unfolded must fold all")
		}
	}
	u, _ = m.Update(keyMsg("_"))
	for _, f := range u.(Model).diffLayer().stk.files {
		if f.collapsed {
			t.Fatal("_ with all folded must unfold all")
		}
	}
}

func TestStackOver100OpensFoldedExceptTheOpenedFile(t *testing.T) { … 101 tree rows, enter on row 50 with pref on; every file collapsed except 50; cursor on header 50 … }
func TestStackNotesKeysAreInert(t *testing.T) { … c, }, { on a stack → no popup, diffNotice == "▸ notes: press S for the single-file view", filesView.sel unchanged … }
func TestStackHomeEndNeverArmAFileStep(t *testing.T) { … end, end → cursor on last line, fileArm none, diffTag unchanged … }
func TestSInertWithoutAList(t *testing.T) { … diffNav = diffNavNone → notice "▸ nothing to stack here", stk nil … }
```

- [ ] **Step 2:** Run. Expected FAIL.
- [ ] **Step 3: Implement**
  - **`openStack`:**
    - `m.stackSeq++`.
    - Files from the builder (full-tree / `diffNavNone` → notice, no stack).
    - `> stackCollapseOver` → fold all but the focus file.
    - The seed (a loaded single view of `focusPath`) becomes the focus file's `d` (`stackLoaded`, counted from rows).
    - New `diffView{stk, context, rev, partial, long, width}`: `context`/`rev` come from the current layer, or from `filesContext`/`filesHash` (tree) / `statusDiffContext(staged)` (status).
    - Replace the layer in place or push it. `m.diffTag = ""`, so any in-flight single load is dropped by the `diffMsg` tag gate.
    - `rebuild`.
    - Cursor: with a seed → the seed's `curLine` mapped by `inFile`, with the same screen row; without → the focus header, jumped to with `diffLead`.
    - `syncStackTitle`, then `pumpStack`, and in Task 8 also batch `stackStatCmd`.
  - **Pref branches.**
    - `openDiffForFileLine`: `if m.diffStacked && !m.inFullTree() { return m.openStack(diffNavTree, l.path, nil) }`, placed after the width guard.
    - `openStatusDiff`: `if m.diffStacked { return m.openStack(nav, f.Path, nil) }`, where `nav` is status / staged.
    - `stepDiffFile*` only runs from single mode, so it is unaffected.
  - **`toggleStacked`:**
    - Flip `m.diffStacked` and persist it (`SetStackedDiff`; an error → `statusMsg`, keep the flip).
    - Going ON: `openStack(m.diffNav, v.title, v)` (seed = the current view if `!v.loading`).
    - Going OFF: `f := v.stk.files[v.curFile()]`.
      - Point the source list's selection at `f` (tree: the visible row index whose path == `f.path`; status: `m.sel[p]` = the display index of `f.fs.Path`).
      - If `f.d != nil`: a new single `diffView` from `*f.d`, with `context`/`rev` inherited and `noteAddr` stamped as `openStatusDiff` / `openDiffForFileLine` do. Cursor at the same `inFile` line; a header → the first change. Then `loadNotesCmd`.
      - Else call the ordinary `openDiffForFileLine` / `openStatusDiff` (pref now off).
  - **`stackKey`** (runs first in `updateDiffViewKey` when `v.stk != nil`, after the selection/search hooks):
    - `S`: `toggleStacked`.
    - `-`: toggle the cursor file's fold; re-splice; put the cursor on that file's header.
    - `_`: all folded ? unfold all : fold all; re-splice; cursor on the current file's header.
    - `J`: an `actionMenu` with one row per file. The label is `status + "  " + path`; `run` puts the cursor on the header (`jumpTo` with `diffLead`), unfolds the file if folded, and pumps.
    - `enter` on a conflict file's header or placeholder: `conflictPickable(f.fs)` → `statusMsg`, else `loadConflictFileCmd(f.path)`.
    - `home`/`end`: cursor to line 0 / the last stop, and scroll to the top / bottom. No arming.
    - `N`/`P`: inert.
    - `c`/`E`/`R`/`}`/`{`/`o`/`O`: the notes notice.
    - Every other key falls through to the normal switch: `n`/`p` work via the header blocks; `f`/`ctrl+w` re-anchor through `focusBlock(ord)`, where `ord` = file index.
    - In a stack, `f` and `ctrl+w` also save and restore `anchorAt(curLine)` around the rebuild instead of `reanchorAfterRebuild` (line numbers are ambiguous across files).
  - `syncStackTitle` is called at the end of every stacked key and every arrival.
  - `.` menu rows (from `m.stackMenuRows()`, appended in `availableActions`' content-window branch while a diff layer is open and its list stacks):
    - `stack-toggle` (key `S`; label "Stacked diff: on" / "off" by the pref);
    - `stack-fold` (`-`) and `stack-fold-all` (`_`), stacked only;
    - `stack-jump` (`J`), stacked only.
- [ ] **Step 4:** `go test ./internal/tui` — PASS (the menu_labels / options_vocab / i18n gates included).
- [ ] **Step 5: Commit** `feat(tui): S stacks the diff view — n/p step files, -/_ fold, J jumps, the pref is remembered`

---

### Task 7: Working tree — sections, conflicts, live reconcile

**Files:**
- Modify: `diff_stack.go` (`reconcileStatusStack`), `model.go` (the three `withStatus` call sites: after each, `m = m.reconcileStatusStack()` and batch `pumpStack`'s Cmd)
- Test: `diff_stack_test.go` (`statusDiffModelMulti`-style fixtures)

**Interfaces:**
- Produces: `func (m Model) reconcileStatusStack() (Model, tea.Cmd)`

- [ ] **Step 1: Failing tests**
  - `TestStatusStackIsTheCursorSection`: Files panel with 2 unstaged + 1 staged-only entry. Enter with the pref on → the stack holds the 2 unstaged files. From the Staged panel → the 1 staged file.
  - `TestStatusStackConflictIsHeaderPlusResolverLine`: a `KindUnmerged` entry → `conflict` true; its placeholder reads "conflict — enter opens the resolver"; it is never in `wantLoads`; `enter` on it returns a Cmd (`loadConflictFileCmd`) for a both-modified conflict.
  - `TestStatusStackReconcile`: a stack over a.txt, b.txt with both loaded, cursor in b.txt. A new status drops a.txt and adds c.txt:
    - files become [b.txt, c.txt];
    - b.txt is `stackStale` with its `d` still set (its body still shows);
    - the cursor is still on the same b.txt row.
    - An emptied section closes the layer (`diffLayer() == nil`).
- [ ] **Step 2:** Run. FAIL.
- [ ] **Step 3: Implement**
  - `buildStatusStack`: include unmerged rows as `conflict: true`.
  - `reconcileStatusStack`:
    - No-op unless the diff layer is stacked with a status source.
    - Rebuild the list from the section. Keep old entries by path (their `d`, `collapsed`, `add`/`del`); a kept loaded file → `stackStale`.
    - An empty list → `popLayer`, `diffTag = ""`.
    - Anchor/restore around the re-splice; the anchor's file is looked up by PATH (its index may shift). If the anchor's file is gone, use the next surviving file.
    - Then `pumpStack`. `wantLoads` already picks `stackStale` files.
    - A stale file's arrival replaces `d` like any load.
  - The counts refresh (Task 8) is also re-fired from here.
- [ ] **Step 4:** `go test ./internal/tui` — PASS.
- [ ] **Step 5: Commit** `feat(tui): working-tree stacks follow the section live; conflicts are header + resolver`

---

### Task 8: Header counts from numstat

**Files:**
- Modify: `diff_stack.go` (`stackStatCmd`, `stackStatMsg`), `model.go` (case), `openStack` + `reconcileStatusStack` batch it
- Test: real repo

**Interfaces:**
- Produces:

```go
type stackStatMsg struct {
	gen   int
	stats []model.DiffStat
}

// stackStatCmd asks git for the stack's +/− counts in ONE numstat when git has
// a tree pair: a plain commit (CommitStat), a commit↔commit compare
// (DiffStat{Rev: a..b}), a working-tree section (DiffStat{Cached: staged}).
// Entry / shelf / preview / link / live-endpoint stacks return nil — their
// counts come from rows on load (spec §7).
func (m Model) stackStatCmd(st *diffStack) tea.Cmd
```

- [ ] **Step 1: Failing test** `TestStackCountsArriveBeforeBodies`: a real repo commit (a: +2 −1, bin.dat binary, c: +5). Open a stack with the pref on and run ONLY the stat Cmd, then feed the message → `counted` with the right `add`/`del`, `bin` on bin.dat. The rendered header contains `+2 −1` and `bin` while the bodies are not loaded. Plus a stale gen dropped.
- [ ] **Step 2:** FAIL.
- [ ] **Step 3: Implement**
  - Match stats by `Path`. `Binary` → `bin`.
  - Numstat never overwrites a file already `counted` from rows? No: the numstat counts are authoritative, rows only fill in the files numstat did not name.
  - Untracked files are absent from `DiffStat` and fill in from rows.
  - Endpoint check: compare mode with both `m.filesLeft`/`m.filesRight` of kind `EndpointCommit` → `DiffStat{Rev: l.Hash() + ".." + r.Hash()}`. Use the real accessor name; grep `func (e Endpoint)` in `model.go`.
- [ ] **Step 4:** `go test ./internal/tui` — PASS.
- [ ] **Step 5: Commit** `feat(tui): stack headers show +/− counts from one numstat`

---

### Task 9: Advertising — footer, help, `.` menu

**Files:**
- Modify: `diff_render.go` (`diffHintFor` gets a `stacked bool` parameter; single footer `[e] edit` → `[S] stack`; the stacked footer), `diff_search_test.go:449` loop over `stacked ∈ {false, true}`, `diff_cursor_test.go:771`, `help.go` (Diff view section rows), i18n bundles
- i18n: `"[↑↓/jk] scroll  [/] find  [spc] mark  [alt↔] side  [n/p] chg  [c}{] notes  [S] stack  [f] part  [^w] %s"` (replaces the current single literal). The stacked footer: `"[↑↓/jk] scroll  [/] find  [n/p] file  [-/_] fold  [J] files  [S] single  [f] part  [^w] %s"`. Help rows (below).

- [ ] **Step 1: Failing tests**
  - Extend the 140-column test to both variants.
  - `TestDiffFooterAdvertisesStack`: the single footer contains `[S] stack`; the stacked footer contains `[n/p] file`, `[-/_] fold`, `[J] files` and `[S] single`.
  - `TestHelpAdvertisesStack`: the `?` help from a diff view has rows for `S`, `-/_`, `J`.
  - `TestDotMenuHasStackRows`: the `.` menu on a tree diff has `stack-toggle`; on a stack it also has `stack-fold`, `stack-fold-all`, `stack-jump`.
- [ ] **Step 2:** FAIL.
- [ ] **Step 3: Implement** the footer (`renderDiffView` passes `v.stk != nil`) and the help rows under "Diff view (enter)":

```go
		r("S", i18n.T("stacked view: every file of the list in one scroll, a header per file (the choice is remembered; S again returns to the single file under the cursor)")),
		r("n/p (stacked)", i18n.T("next / previous file header; home/end go to the top / bottom of the whole stack")),
		r("-/_", i18n.T("stacked: fold / unfold the cursor's file; _ folds all (or unfolds all when all are folded) — more than 100 files open folded")),
		r("J", i18n.T("stacked: jump to a file (type to filter)")),
		r("enter (stacked)", i18n.T("on a conflicted file's header: open the conflict resolver")),
```

  Update the doc comment above `diffHintFor` with the new trade (the `[e] edit` → `[S] stack` swap, and the stacked variant's budget).
- [ ] **Step 4:** `go test ./internal/tui` — PASS.
- [ ] **Step 5: Commit** `feat(tui): advertise the stacked diff in the footer, help and . menu`

---

### Task 10: Verify, document, deliver

- [ ] **Step 1: tui-capture snapshots** (driving-tui-headless skill):
  - Fixture repo with a commit of 5 files (one rename, one binary) plus 120 small files in a second commit.
  - Script: open the commit's files, `enter` → `S` → screen; `n` → screen; `-` → screen; `J` → screen.
  - Also the second commit: `enter` → "every file folded except the opened one".
  - Also the Files panel with 2 unstaged files and 1 conflict → `enter` (pref on) → screen.
  - Read every snapshot: headers present, bodies painted, counts visible, folded `▸`.
  - Run with an isolated `XDG_STATE_HOME` (S writes the pref).
  - Run the SAME script against the main-branch binary first. `S` must do nothing there. This is the unfixed-build baseline.
- [ ] **Step 2: Docs.**
  - `CHANGELOG.md` (an Unreleased entry).
  - `README.md` (the diff view keys).
  - `docs/CLAUDE-details.md`: a new "Stacked diff view in the TUI" section with the data model, loading, and the decisions list above.
  - `docs/web-tui-parity.md`: the stacked row now has TUI = yes, with the key differences — TUI n/p vs web j/k, home/end vs web.
  - Memory: `stacked-diff-view-feature.md` status.
- [ ] **Step 3:**
  - `./test.sh race`. It must be green; paste the failures if not.
  - The web probes are NOT re-run: nothing under `internal/web` or `internal/domain` changes. Verify with `git diff --stat main -- internal/web internal/domain` = empty.
- [ ] **Step 4: Deliver.**
  - `./build.sh all` in the worktree.
  - Send the Linux `gg` + the Windows `gg.exe` with absolute paths and the version string.
  - The user merges.
