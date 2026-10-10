# Review Overview Split Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (this repo's CLAUDE.md forbids implementer subagents). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a review's stored overview beside the files it points at — overview fixed on the left, the file(s) on the right, single or stacked — the overview steering the right side.

**Architecture:** A new generic full-screen layer `splitView` holds two `pane`s and a focused side; a `docPane` wraps an `openFile` and draws it with the existing preview box, so every viewer feature (cursor, selection, search, bands, `.` menu) arrives unchanged. An `overviewPane` adds anchor stepping and steering; a `stackPane` is one unregistered document whose line stream is every review file's content with a header, loaded lazily. The diff view is untouched. Four "which document is the keyboard on" seams gain one case each through `m.splitFocus()`.

**Tech Stack:** Go 1.26, Bubble Tea + lipgloss, the TUI's `openFile`/`contentPopup`/`renderPreviewBox`, promptstate, the e2e harness (`e2e/scenarios/*.toml`, `tui.Headless`).

**Spec:** `docs/superpowers/specs/2026-10-10-review-overview-split-design.md`

**Spec deviation (decided at plan time, told to the user):** §4.2 says the stacked stream shows the review's note boxes per file. The single-mode file viewer shows no review notes today (review notes live in the diff view only), so v1 of the stack shows the `◆n` count on each header and no boxes — single and stacked stay at parity. Note boxes in both are a follow-up. Likewise §4.1 lists `h`/`b` (history, blame) for the file pane: the file viewer has no such keys today, so the pane has none either.

## Global Constraints

- Work only in the worktree `/work/gigagit/.claude/worktrees/review-overview-split` on branch `feat/review-overview-split`; commit per task; never `git add -A`; `bin/` is ignored.
- `internal/tui` never imports `internal/git` (archtest). Reads go through `m.svc` (domain).
- Every user-visible TUI string is an `i18n.T` literal present in all four bundles `internal/i18n/lang/{ja,ko,zh,ru}.toml` (AST gate tests fail otherwise). Keys are added in place, never re-sorted; removing a string removes its key from all four.
- `Model` is a value receiver; state that must survive the copy lives behind pointers (`m.layers`, the layer structs themselves).
- Tests call `t.Parallel()`, build repos with `stackRepoModel(t)` / `testRepo`, pin the prompt store with `tempPromptStore(t, m)`, and drain commands with `drainCmds(t, m, cmd)`.
- Run `go test ./internal/tui -run <Name> -count=1` per step; `./test.sh race` before the final review (green only on "all green").
- Minimum width for the split: 60 columns (`splitMinWidth`); below it today's full-screen viewer opens.
- The stacked choice is remembered under its own promptstate key `tui_stacked_review_files`, never the diff's.

## Review Focus

1. A steer to a file that is gone at the reviewed tip (a deleted path, or a working review whose file was removed): the right pane must show the load error placeholder and the status line name the file, never a blank pane or a stale file. → Task 4 test `TestSplitSteerToMissingFileShowsPlaceholder`.
2. A load result arriving after the split closed, or for the other pane while the focused one changed: it must land in the pane that asked (by tag), never in the focused one. → Task 3 test `TestSplitLiveDocFindsEitherPane`.
3. A stacked file loading ABOVE the cursor's file: the cursor must stay on the same (file, line) and the viewport on the same screen row. → Task 5 test `TestStackLoadAboveCursorKeepsPlace`.
4. The window shrinking below 60 columns while the split is open: the split must keep drawing (clamped), never panic on a 1-column pane. → Task 3 test `TestSplitRendersAtTinyWidth`.
5. Esc while a search is being typed in a pane, or while a line selection is live: the pane must take it (close the search / unmark), the split must stay open. → Task 3 test `TestSplitEscGoesToATypingSearchFirst`.

---

### Task 1: The stacked-review-files preference

**Files:**
- Modify: `internal/promptstate/store.go` (the `Store` interface, after `SetStackedDiff`)
- Modify: `internal/promptstate/file_store.go:27-28` (the record)
- Modify: `internal/promptstate/stacked.go`
- Modify: `internal/tui/model.go:182` (field beside `diffStacked`), `internal/tui/model.go:494-500` (`loadPrefs`)
- Create: `internal/tui/split_prefs.go`
- Test: `internal/promptstate/stacked_test.go`, `internal/tui/split_prefs_test.go`

**Interfaces:**
- Produces: `Store.StackedReviewFiles() bool`, `Store.SetStackedReviewFiles(on bool) error`; `Model.reviewStacked bool`; `func (m Model) setReviewStackedPref(on bool) Model`.

- [ ] **Step 1: Write the failing store test**

Append to `internal/promptstate/stacked_test.go`:

```go
// The review split's own stacked pref round-trips and never touches the
// diff view's (spec §4.3: two keys, two postures).
func TestStackedReviewFilesIsItsOwnPref(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	if fs.StackedReviewFiles() {
		t.Fatal("a fresh store must read false")
	}
	if err := fs.SetStackedReviewFiles(true); err != nil {
		t.Fatal(err)
	}
	if !fs.StackedReviewFiles() || fs.StackedDiff() {
		t.Fatalf("review %v diff %v: the review pref must not set the diff's", fs.StackedReviewFiles(), fs.StackedDiff())
	}
	if err := fs.SetStackedDiff(true); err != nil {
		t.Fatal(err)
	}
	if err := fs.SetStackedReviewFiles(false); err != nil {
		t.Fatal(err)
	}
	if fs.StackedReviewFiles() || !fs.StackedDiff() {
		t.Fatal("clearing the review pref must leave the diff's alone")
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/promptstate -run TestStackedReviewFilesIsItsOwnPref -count=1`
Expected: FAIL — `fs.StackedReviewFiles undefined`.

- [ ] **Step 3: Implement the pref**

In `internal/promptstate/store.go`, after `SetStackedDiff(on bool) error`:

```go
	// StackedReviewFiles reports whether a review's overview split opens its
	// files stacked (the S key there). Its own key, independent of StackedDiff.
	StackedReviewFiles() bool
	// SetStackedReviewFiles persists it.
	SetStackedReviewFiles(on bool) error
```

In `internal/promptstate/file_store.go`, after the `StackedDiff` field:

```go
	// StackedReviewFiles is the review overview split's stacked mode (see stacked.go).
	StackedReviewFiles bool `toml:"tui_stacked_review_files,omitempty"`
```

Append to `internal/promptstate/stacked.go`:

```go
// StackedReviewFiles reports whether the review overview split opens its
// files stacked — its own record, so the diff view and the split are read
// in their own postures (review-overview-split design §4.3).
func (fs *FileStore) StackedReviewFiles() bool { return fs.read().StackedReviewFiles }

// SetStackedReviewFiles persists the review split's stacked preference.
func (fs *FileStore) SetStackedReviewFiles(on bool) error {
	r := fs.read()
	r.StackedReviewFiles = on
	return fs.write(r)
}
```

Run `grep -rn 'SetStackedDiff(' --include='*.go' . | grep -v promptstate/` — every other implementer of `promptstate.Store` (if any) gets the two methods too (none exist at plan time; the grep proves it).

- [ ] **Step 4: Run the store test**

Run: `go test ./internal/promptstate -count=1`
Expected: PASS.

- [ ] **Step 5: Write the failing TUI pref test**

Create `internal/tui/split_prefs_test.go`:

```go
package tui

import "testing"

// The split's stacked choice is read at startup and written on every flip,
// under its own key — flipping it never moves the diff view's.
func TestReviewStackedPrefRoundTrips(t *testing.T) {
	t.Parallel()
	m := tempPromptStore(t, newTestModel(t))
	if m.reviewStacked {
		t.Fatal("a fresh store reads false")
	}
	m = m.setReviewStackedPref(true)
	if !m.reviewStacked || !m.promptStore.StackedReviewFiles() || m.promptStore.StackedDiff() {
		t.Fatalf("flag %v store %v diff %v", m.reviewStacked, m.promptStore.StackedReviewFiles(), m.promptStore.StackedDiff())
	}
	if !m.loadPrefs().reviewStacked {
		t.Fatal("loadPrefs must read the stored choice")
	}
}
```

- [ ] **Step 6: Run it to see it fail**

Run: `go test ./internal/tui -run TestReviewStackedPrefRoundTrips -count=1`
Expected: FAIL — `m.reviewStacked undefined`.

- [ ] **Step 7: Implement the Model side**

In `internal/tui/model.go`, directly after the `diffStacked bool` field:

```go
	// reviewStacked is the review overview split's stacked mode (S there):
	// its own promptstate key (split_prefs.go), never the diff view's.
	reviewStacked bool
```

In `loadPrefs`, inside the `if m.promptStore != nil` block, add `m.reviewStacked = m.promptStore.StackedReviewFiles()`.

Create `internal/tui/split_prefs.go`:

```go
package tui

import "github.com/homeend/gigagit/internal/i18n"

// setReviewStackedPref flips the review split's stacked mode for the session
// and remembers it (machine-local, promptstate). A store that cannot be
// written keeps the session flag and says so in the status line.
func (m Model) setReviewStackedPref(on bool) Model {
	m.reviewStacked = on
	if m.promptStore != nil {
		if err := m.promptStore.SetStackedReviewFiles(on); err != nil {
			m.statusMsg = i18n.T("could not remember the stacked-files choice: %s", err.Error())
		}
	}
	return m
}
```

Add the key `"could not remember the stacked-files choice: %s"` to all four bundles next to `"could not remember the stacked-diff choice: %s"` (same place in each file; translate by that neighbour's pattern).

- [ ] **Step 8: Run the tests and the i18n gate**

Run: `go test ./internal/tui -run 'TestReviewStackedPrefRoundTrips|TestI18n|TestTranslation' -count=1 && go test ./internal/i18n -count=1`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/promptstate/store.go internal/promptstate/file_store.go internal/promptstate/stacked.go internal/promptstate/stacked_test.go internal/tui/model.go internal/tui/split_prefs.go internal/tui/split_prefs_test.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml
git commit -m "promptstate+tui: the review split's own stacked-files preference"
```

---

### Task 2: A preview box that can hide its cursor

**Files:**
- Modify: `internal/tui/file_preview.go:477` (`renderPreviewBox`)
- Test: `internal/tui/split_box_test.go`

**Interfaces:**
- Produces: `type previewBoxOpts struct{ focused, viewer, cursorOff bool }`; `func (m Model) renderPreviewBoxOpts(p *contentPopup, title string, boxW, boxH int, o previewBoxOpts) string`. `renderPreviewBox` keeps its signature and becomes a wrapper.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/split_box_test.go`:

```go
package tui

import "testing"

// An unfocused pane draws no cursor row: the box rendered with cursorOff is
// byte-identical to one whose cursor is off-screen, and differs from the
// focused render (which paints the cursor band).
func TestPreviewBoxCursorOffHidesTheCursor(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.width, m.height = 120, 30
	p := &contentPopup{lines: []contentLine{{text: "one", src: true}, {text: "two", src: true}, {text: "three", src: true}}}
	p.cur = 1
	with := m.renderPreviewBoxOpts(p, "t", 40, 10, previewBoxOpts{focused: true})
	off := m.renderPreviewBoxOpts(p, "t", 40, 10, previewBoxOpts{focused: true, cursorOff: true})
	if with == off {
		t.Fatal("cursorOff must change the render (the cursor band is gone)")
	}
	p.cur = -1
	none := m.renderPreviewBoxOpts(p, "t", 40, 10, previewBoxOpts{focused: true})
	if off != none {
		t.Fatalf("cursorOff must render exactly as a box with no cursor row:\n%s\n---\n%s", off, none)
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/tui -run TestPreviewBoxCursorOffHidesTheCursor -count=1`
Expected: FAIL — `renderPreviewBoxOpts undefined`.

- [ ] **Step 3: Implement the opts wrapper**

In `internal/tui/file_preview.go`, above `renderPreviewBox`:

```go
// previewBoxOpts is how a preview box is drawn: focused (the lit border),
// viewer (the full-screen viewer's reading column, hint row and title-line
// status) and cursorOff (no cursor row — an unfocused split pane).
type previewBoxOpts struct{ focused, viewer, cursorOff bool }

// renderPreviewBox draws one file preview as a bordered box: title, the
// windowed lines (cursor band, selection stripe, search emphasis, syntax
// classes) and the hint line. The files view's right column and the
// full-screen fileViewer both draw through it, so they cannot drift apart.
func (m Model) renderPreviewBox(p *contentPopup, title string, boxW, boxH int, focused, viewer bool) string {
	return m.renderPreviewBoxOpts(p, title, boxW, boxH, previewBoxOpts{focused: focused, viewer: viewer})
}
```

Rename the existing function body to `renderPreviewBoxOpts(p *contentPopup, title string, boxW, boxH int, o previewBoxOpts) string`, replace every `focused` read inside with `o.focused`, every `viewer` with `o.viewer`, and change the line `cursorOff := m.cursorStyle() == "off"` to `cursorOff := m.cursorStyle() == "off" || o.cursorOff`. Move the old doc comment onto the wrapper (above) and give the opts function a one-line comment: `// renderPreviewBoxOpts is renderPreviewBox with every drawing choice explicit.`

- [ ] **Step 4: Run the test and the preview tests**

Run: `go test ./internal/tui -run 'TestPreviewBox|TestFilePreview|TestFileViewer' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/file_preview.go internal/tui/split_box_test.go
git commit -m "tui: renderPreviewBoxOpts — a preview box that can hide its cursor"
```

---

### Task 3: The split layer, the document pane and the four seams

**Files:**
- Create: `internal/tui/split_view.go`, `internal/tui/split_doc_pane.go`
- Modify: `internal/tui/layer_stack.go:36-41` (`isFullScreenLayer`), `internal/tui/mouse.go:71-77` (`rightClickMenuLayer`)
- Modify: `internal/tui/preview_select.go:16-26` (`activePreview`), `internal/tui/open_files.go:125-139` (`docShown`), `:144-156` (`detachDoc`), `:229-235` (`focusedDoc`), `internal/tui/file_viewer.go:177-196` (`liveDoc`), `internal/tui/overview_keys.go:173-179` (`topDoc`), `internal/tui/open_file_notes.go:240-252` (`previewDoc`), `internal/tui/model.go:1096-1108` (the `fileContentMsg` case)
- Test: `internal/tui/split_view_test.go`

**Interfaces:**
- Produces:
  ```go
  type pane interface {
      update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
      render(m Model, w, h int, focused bool) string
      title() string
      hint(m Model) string
      captures(msg tea.KeyMsg) bool
      doc() *openFile
  }
  type splitView struct { left, right pane; onLeft bool; onClose func(Model) Model }
  const splitMinWidth = 60
  func (m Model) splitGeom() (leftW, rightW, boxH int)
  func paneGeom(boxW, boxH int) (rows, innerW int)
  func (sv *splitView) focused() pane
  func (sv *splitView) geomOf(m Model, p pane) (rows, innerW int)
  func (sv *splitView) holds(d *openFile) bool
  func (sv *splitView) paneOf(d *openFile) pane
  func (m Model) splitLayer() *splitView            // the topmost split, or nil
  func (m Model) splitFocus() (sv *splitView, d *openFile, rows, innerW int, ok bool) // only when the split is the TOP layer and its focused pane shows a document
  type docPane struct { d *openFile; empty string }
  func (d *openFile) frameTitle() string             // lifted from fileViewer.title
  ```

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/split_view_test.go`:

```go
package tui

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// splitDoc is a loaded commit document of n lines "<prefix>1".."<prefix>n" —
// more lines than a pane shows, so scrolling has somewhere to go.
func splitDoc(path, prefix string, n int) *openFile {
	d := &openFile{src: fileSource{kind: srcCommit, rev: "abc"}, path: path, p: &contentPopup{}, tag: path}
	for i := 1; i <= n; i++ {
		l := prefix + strconv.Itoa(i)
		d.p.lines = append(d.p.lines, contentLine{text: l, raw: l, src: true})
	}
	return d
}

// A split over two documents: left/right halves, each a bordered box with
// its own title, one hint line under them.
func testSplit(t *testing.T) (Model, *splitView) {
	t.Helper()
	m := newTestModel(t)
	m.width, m.height = 120, 30
	sv := &splitView{onLeft: true}
	sv.left = &docPane{d: splitDoc("left.md", "l", 60)}
	sv.right = &docPane{d: splitDoc("right.go", "r", 60)}
	m = m.pushLayer(sv)
	return m, sv
}

func TestSplitGeometryIsTwoHalves(t *testing.T) {
	t.Parallel()
	m, sv := testSplit(t)
	lw, rw, bh := m.splitGeom()
	if lw != 60 || rw != 60 || bh != 29 {
		t.Fatalf("geom = %d %d %d", lw, rw, bh)
	}
	rows, inner := sv.geomOf(m, sv.left)
	if rows != 26 || inner != 56 {
		t.Fatalf("pane geom = %d×%d, want 26×56 (box 60×29 less border, title, padding)", rows, inner)
	}
	m.width = 121
	lw, rw, _ = m.splitGeom()
	if lw != 60 || rw != 61 {
		t.Fatalf("odd width: %d %d", lw, rw)
	}
}

func TestSplitRendersAtTinyWidth(t *testing.T) {
	t.Parallel()
	m, _ := testSplit(t)
	m.width, m.height = 9, 5
	if v := m.View(); v == "" {
		t.Fatal("a tiny split must still draw")
	}
}

func TestSplitFocusKeys(t *testing.T) {
	t.Parallel()
	m, sv := testSplit(t)
	step := func(k string) {
		t.Helper()
		u, _ := m.Update(synthKey(k))
		m = u.(Model)
	}
	for _, k := range []string{"tab", "shift+tab", "alt+right", "alt+left"} {
		before := sv.onLeft
		step(k)
		if sv.onLeft == before {
			t.Fatalf("%s did not switch sides", k)
		}
	}
	sv.onLeft = false
	step("backspace")
	if !sv.onLeft {
		t.Fatal("backspace focuses the left side")
	}
	step("backspace")
	if !sv.onLeft {
		t.Fatal("backspace on the left is inert")
	}
}

func TestSplitKeysReachOnlyTheFocusedPane(t *testing.T) {
	t.Parallel()
	m, sv := testSplit(t)
	u, _ := m.Update(synthKey("down"))
	m = u.(Model)
	if sv.left.doc().p.sel != 1 || sv.right.doc().p.sel != 0 {
		t.Fatalf("left %d right %d: only the focused pane scrolls", sv.left.doc().p.sel, sv.right.doc().p.sel)
	}
	u, _ = m.Update(synthKey("tab"))
	u, _ = u.(Model).Update(synthKey("down"))
	m = u.(Model)
	if sv.right.doc().p.sel != 1 {
		t.Fatal("after tab the right pane scrolls")
	}
}

func TestSplitEscPopsAndRunsOnClose(t *testing.T) {
	t.Parallel()
	m, sv := testSplit(t)
	closed := false
	sv.onClose = func(m Model) Model { closed = true; m.statusMsg = "closed"; return m }
	u, _ := m.Update(synthKey("esc"))
	m = u.(Model)
	if m.topLayer() != nil || !closed || m.statusMsg != "closed" {
		t.Fatalf("top %T closed %v msg %q", m.topLayer(), closed, m.statusMsg)
	}
}

func TestSplitEscGoesToATypingSearchFirst(t *testing.T) {
	t.Parallel()
	m, sv := testSplit(t)
	u, _ := m.Update(synthKey("/"))
	m = u.(Model)
	if !sv.left.doc().p.search.typing {
		t.Fatal("/ opens the focused pane's search")
	}
	u, _ = m.Update(synthKey("esc"))
	m = u.(Model)
	if m.topLayer() != sv || sv.left.doc().p.search.typing {
		t.Fatalf("esc closes the search, not the split: top %T typing %v", m.topLayer(), sv.left.doc().p.search.typing)
	}
	// A live selection owns esc too.
	u, _ = m.Update(synthKey(" "))
	m = u.(Model)
	if !sv.left.doc().p.lsel.on {
		t.Fatal("space starts a selection")
	}
	u, _ = m.Update(synthKey("esc"))
	m = u.(Model)
	if m.topLayer() != sv || sv.left.doc().p.lsel.on {
		t.Fatal("esc unmarks the selection, the split stays")
	}
}

func TestSplitSeamsAnswerForTheFocusedPane(t *testing.T) {
	t.Parallel()
	m, sv := testSplit(t)
	if d, ok := m.focusedDoc(); !ok || d != sv.left.doc() {
		t.Fatalf("focusedDoc = %v %v", d, ok)
	}
	if p, rows, _, ok := m.activePreview(); !ok || p != sv.left.doc().p || rows != 26 {
		t.Fatalf("activePreview = %v rows %d ok %v", p, rows, ok)
	}
	if topDoc(m) != sv.left.doc() {
		t.Fatal("topDoc is the focused pane's document")
	}
	sv.onLeft = false
	if d, _ := m.focusedDoc(); d != sv.right.doc() {
		t.Fatal("after a switch the right pane answers")
	}
	if !m.docShown(sv.left.doc()) || !m.docShown(sv.right.doc()) {
		t.Fatal("both pane documents are shown")
	}
	if m.previewDoc(sv.left.doc().p) != sv.left.doc() {
		t.Fatal("previewDoc finds a pane's document by its popup")
	}
}

func TestSplitLiveDocFindsEitherPane(t *testing.T) {
	t.Parallel()
	m, sv := testSplit(t)
	for _, p := range []pane{sv.left, sv.right} {
		d, rows, inner, ok := m.liveDoc(p.doc().tag)
		if !ok || d != p.doc() || rows != 26 || inner != 56 {
			t.Fatalf("liveDoc(%s) = %v %d %d %v", p.doc().tag, d, rows, inner, ok)
		}
	}
	// A load landing for the unfocused pane fills that pane, not the focused one.
	u, _ := m.Update(fileContentMsg{tag: "right.go", lines: []contentLine{{text: "new", src: true}}})
	m = u.(Model)
	if sv.right.doc().p.lines[0].text != "new" || sv.left.doc().p.lines[0].text != "l1" {
		t.Fatal("the result landed in the wrong pane")
	}
}

func TestSplitDetachingAPaneDocumentClosesTheSplit(t *testing.T) {
	t.Parallel()
	m, sv := testSplit(t)
	m = m.detachDoc(sv.right.doc())
	if m.hasLayer(sv) {
		t.Fatal("a document taken to another frame takes the split down")
	}
	if !sv.left.doc().backgrounded {
		t.Fatal("the other pane's document stays open in the background")
	}
}

func TestSplitRenderHasOneLitBorderAndOneHint(t *testing.T) {
	t.Parallel()
	m, sv := testSplit(t)
	v := m.View()
	plain := ansi.Strip(v)
	if !strings.Contains(plain, "left.md") || !strings.Contains(plain, "right.go") {
		t.Fatalf("both titles drawn:\n%s", plain)
	}
	if strings.Count(plain, "[tab] other side") != 1 {
		t.Fatalf("one hint line:\n%s", plain)
	}
	lit := strings.Count(v, st().focusedPanel.GetBorderStyle().TopLeft)
	if lit != 2 {
		t.Fatalf("two top-left corners drawn, got %d", lit)
	}
	left := sv.left.render(m, 60, 29, true)
	unlit := sv.left.render(m, 60, 29, false)
	if left == unlit {
		t.Fatal("the focused border differs from the unfocused one")
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/tui -run 'TestSplit' -count=1`
Expected: FAIL to compile — `splitView undefined`.

- [ ] **Step 3: Write the split layer**

Create `internal/tui/split_view.go`:

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/i18n"
)

// The SPLIT layer (spec 2026-10-10-review-overview-split §1): two panes side
// by side, one focused. The split owns the keys that move between the sides
// and the one that closes it; every other key goes to the focused pane. It
// is a generic full-screen surface — the review's overview beside its files
// is its first use — and knows nothing about what a pane shows.

// pane is one side of a split: a surface with its own content, cursor and
// keys, drawn as a bordered box w×h. captures says the pane must see a key
// before the split reads it (a search being typed takes every key, a live
// line selection takes esc). doc is the document shown (nil = none), for
// the seams that ask "which document is the keyboard on".
type pane interface {
	update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
	render(m Model, w, h int, focused bool) string
	title() string
	hint(m Model) string
	captures(msg tea.KeyMsg) bool
	doc() *openFile
}

// splitView is the layer: the two panes, the focused side, and what esc
// does once the split is gone (nil = nothing).
type splitView struct {
	left, right pane
	onLeft      bool
	onClose     func(Model) Model
}

// splitMinWidth is the narrowest terminal a split is opened on: two
// 30-column boxes. Narrower, callers open their full-screen fallback.
const splitMinWidth = 60

// splitGeom is the two boxes' sizes: the left half (rounded down), the rest
// on the right, both one row short of the screen (the hint line).
func (m Model) splitGeom() (leftW, rightW, boxH int) {
	w, h := m.overlayDims()
	leftW = w / 2
	rightW = w - leftW
	boxH = h - 1
	if boxH < 4 {
		boxH = 4
	}
	return leftW, rightW, boxH
}

// paneGeom is a box's content size: the preview box's math (border 2,
// padding 2, a title row) with no hint row of its own.
func paneGeom(boxW, boxH int) (rows, innerW int) {
	rows, innerW = boxH-3, boxW-4
	if rows < 1 {
		rows = 1
	}
	if innerW < 1 {
		innerW = 1
	}
	return rows, innerW
}

// focused is the pane the keyboard is on.
func (sv *splitView) focused() pane {
	if sv.onLeft {
		return sv.left
	}
	return sv.right
}

// geomOf is pane p's content size in this split.
func (sv *splitView) geomOf(m Model, p pane) (rows, innerW int) {
	lw, rw, bh := m.splitGeom()
	if p == sv.left {
		return paneGeom(lw, bh)
	}
	return paneGeom(rw, bh)
}

// holds reports whether either pane shows d.
func (sv *splitView) holds(d *openFile) bool { return d != nil && sv.paneOf(d) != nil }

// paneOf is the pane showing d, or nil.
func (sv *splitView) paneOf(d *openFile) pane {
	if d == nil {
		return nil
	}
	if sv.left.doc() == d {
		return sv.left
	}
	if sv.right.doc() == d {
		return sv.right
	}
	return nil
}

func (sv *splitView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	f := sv.focused()
	if f.captures(msg) {
		return f.update(m, msg)
	}
	switch msg.String() {
	case "tab", "shift+tab":
		sv.onLeft = !sv.onLeft
		return m, nil
	case "alt+left":
		sv.onLeft = true
		return m, nil
	case "alt+right":
		sv.onLeft = false
		return m, nil
	case "backspace":
		sv.onLeft = true
		return m, nil
	case "esc":
		m = m.removeLayer(sv)
		if sv.onClose != nil {
			m = sv.onClose(m)
		}
		return m, nil
	}
	return f.update(m, msg)
}

// render owns the screen: the two boxes joined, one hint line under them —
// the focused pane's keys, then the split's own. A status message leads the
// hint (the split covers the status bar), as the viewer's title line does.
func (sv *splitView) render(m Model, _ string) string {
	lw, rw, bh := m.splitGeom()
	w, _ := m.overlayDims()
	boxes := lipgloss.JoinHorizontal(lipgloss.Top, sv.left.render(m, lw, bh, sv.onLeft), sv.right.render(m, rw, bh, !sv.onLeft))
	hint := sv.focused().hint(m) + "  " + i18n.T("[tab] other side  [bksp] left side  [esc] close")
	if m.statusMsg != "" && !m.running && m.statusMsg != m.stickyMsg {
		hint = m.statusMsg + "  " + hint
	}
	return strings.TrimRight(boxes, "\n") + "\n" + padRight(truncate(hint, w), w)
}

// splitLayer is the topmost split on the stack, or nil.
func (m Model) splitLayer() *splitView { return layerOf[*splitView](m) }

// splitFocus answers "which document is the keyboard on" for a split ON
// TOP of the stack whose focused pane shows a document: that document and
// its content size. A split under a popup is not on the keyboard.
func (m Model) splitFocus() (sv *splitView, d *openFile, rows, innerW int, ok bool) {
	sv, isSplit := m.topLayer().(*splitView)
	if !isSplit {
		return nil, nil, 0, 0, false
	}
	p := sv.focused()
	d = p.doc()
	if d == nil {
		return sv, nil, 0, 0, false
	}
	rows, innerW = sv.geomOf(m, p)
	return sv, d, rows, innerW, true
}

// splitShowing is the split anywhere on the stack showing d, or nil.
func (m Model) splitShowing(d *openFile) *splitView {
	if m.layers == nil {
		return nil
	}
	for i := len(m.layers.entries) - 1; i >= 0; i-- {
		if sv, ok := m.layers.entries[i].(*splitView); ok && sv.holds(d) {
			return sv
		}
	}
	return nil
}
```

- [ ] **Step 4: Write the document pane**

Create `internal/tui/split_doc_pane.go`:

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// docPane is a split pane over an open-file document: the file viewer's
// box, cursor, selection, search, note boxes, bands and keys, in a half of
// the screen. d nil = nothing shown yet (empty is the title then).
type docPane struct {
	d     *openFile
	empty string
}

func (p *docPane) doc() *openFile { return p.d }

func (p *docPane) title() string {
	if p.d == nil {
		if p.empty != "" {
			return p.empty
		}
		return i18n.T("(no file)")
	}
	return p.d.frameTitle()
}

// captures: a search being typed takes every key; a live selection takes
// esc (it unmarks); nothing else is the pane's before the split's.
func (p *docPane) captures(msg tea.KeyMsg) bool {
	if p.d == nil {
		return false
	}
	if p.d.p.search.typing {
		return true
	}
	return p.d.p.lsel.on && msg.String() == "esc"
}

func (p *docPane) render(m Model, w, h int, focused bool) string {
	if p.d == nil {
		return m.renderPreviewBoxOpts(&contentPopup{}, p.title(), w, h, previewBoxOpts{focused: focused, cursorOff: true})
	}
	return m.renderPreviewBoxOpts(p.d.p, p.title(), w, h, previewBoxOpts{focused: focused, cursorOff: !focused})
}

// hint is the viewer's everyday keys; a file with bands leads with n/p.
func (p *docPane) hint(m Model) string {
	if p.d == nil {
		return ""
	}
	h := i18n.T("[alt+↑↓] line  [spc] mark  [/] find  [X] close  [↑/↓] scroll  [ctrl+w] view")
	if p.d.bands() != nil {
		h = i18n.T("[n/p] anchors") + "  " + h
	}
	return h
}

// update is the file viewer's key order without the keys the split owns
// (esc, backspace, tab): selection, search, note keys, bands, then the
// pager. X closes the document and empties the pane; ctrl+] backgrounds it.
func (p *docPane) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if p.d == nil {
		if msg.String() == "." {
			return m.openActionMenu(), nil
		}
		return m, nil
	}
	if nm, cmd, ok := m.previewSelectKey(msg); ok {
		return nm, cmd
	}
	if nm, cmd, ok := m.previewSearchKey(msg); ok {
		return nm, cmd
	}
	if nm, cmd, ok := m.previewNoteKey(msg); ok {
		return nm, cmd
	}
	if nm, cmd, ok := m.bandKey(msg); ok {
		return nm, cmd
	}
	d := p.d
	pp := d.p
	_, rows, inner, _ := m.activePreview()
	scroll := func(delta int) { pp.scrollBy(delta, rows) }
	switch msg.String() {
	case "X":
		p.d = nil // out of the pane first: closeDoc's detach must not find it here
		return m.closeDoc(d), nil
	case "ctrl+]":
		p.d = nil
		return m.backgroundDoc(d), nil
	case ".":
		return m.openActionMenu(), nil
	case "y":
		if d.result {
			return m, m.copyToClipboardCmd(i18n.T("copied"), docText(d))
		}
	case "alt+up":
		m.movePreviewCursor(-1)
	case "alt+down":
		m.movePreviewCursor(1)
	case "up", "k":
		scroll(-1)
	case "down", "j":
		scroll(1)
	case "pgup":
		scroll(-rows)
	case "pgdown", " ":
		scroll(rows)
	case "home":
		pp.sel = 0
	case "end":
		pp.sel = pp.clampTop(len(pp.lines), rows)
	case "ctrl+w":
		pp.mode = pp.mode.next()
		pp.hscroll = 0
		if d.ov != nil {
			d.layOut(rows, inner)
		}
	case "shift+left":
		if pp.mode == modeScroll && pp.hscroll > 0 {
			if pp.hscroll -= m.hscrollStep(); pp.hscroll < 0 {
				pp.hscroll = 0
			}
		}
	case "shift+right":
		if pp.mode == modeScroll {
			pp.hscroll += m.hscrollStep()
		}
	}
	return m, nil
}
```

In `internal/tui/file_viewer.go`, replace the body of `fileViewer.title()` with `return fv.openFile.frameTitle()` and add to `internal/tui/open_file.go`:

```go
// frameTitle names the version a frame shows: the working tree, a commit,
// a shelf, or a titled document (a result, an overview).
func (d *openFile) frameTitle() string {
	switch d.src.kind {
	case srcCommit:
		return i18n.T("View %s @ %s", d.path, shortHash(d.src.rev))
	case srcShelf:
		return i18n.T("View %s (shelf)", d.path)
	case srcExternal, srcNote, srcOverview, srcReviewOverview:
		if d.title != "" {
			return d.title
		}
		return d.path
	}
	return i18n.T("View %s (working tree)", d.path)
}
```

- [ ] **Step 5: Teach the seams about the split**

`internal/tui/layer_stack.go` `isFullScreenLayer`: add `*splitView` to the case list. `internal/tui/mouse.go` `rightClickMenuLayer`: add `*splitView`.

`internal/tui/preview_select.go` `activePreview`, as the first check:

```go
	if _, d, rows, innerW, ok := m.splitFocus(); ok {
		return d.p, rows, max(innerW-d.gutterW(), 1), true
	}
```

`internal/tui/open_files.go` `focusedDoc`, first:

```go
	if _, d, _, _, ok := m.splitFocus(); ok {
		return d, true
	}
```

`docShown`: inside the layers loop add `if sv, ok := l.(*splitView); ok && sv.holds(d) { return true }`.

`detachDoc`, before the layers loop:

```go
	// A document shown in a split pane leaves for another frame: the split
	// comes down (a document is in one frame at a time) and the other pane's
	// document stays open in the background.
	if sv := m.splitShowing(d); sv != nil {
		if o := sv.left.doc(); o != nil && o != d {
			o.backgrounded = true
		}
		if o := sv.right.doc(); o != nil && o != d {
			o.backgrounded = true
		}
		return m.removeLayer(sv)
	}
```

`internal/tui/file_viewer.go` `liveDoc`, inside the layers loop (before the fileViewer check):

```go
			if sv, ok := m.layers.entries[i].(*splitView); ok {
				for _, p := range []pane{sv.left, sv.right} {
					if d := p.doc(); d != nil && d.tag == tag {
						rows, innerW = sv.geomOf(m, p)
						return d, rows, innerW, true
					}
				}
			}
```

`internal/tui/overview_keys.go` `topDoc`, first:

```go
	if _, d, _, _, ok := m.splitFocus(); ok {
		return d
	}
```

`internal/tui/open_file_notes.go` `previewDoc`, inside the layers loop add:

```go
			if sv, ok := l.(*splitView); ok {
				for _, pn := range []pane{sv.left, sv.right} {
					if d := pn.doc(); d != nil && d.p == p {
						return d
					}
				}
			}
```

`internal/tui/model.go` `fileContentMsg` case: an overview in a split lays out at the pane's width, not the reading column:

```go
			if d.ov != nil {
				w := m.overviewWidth(inner)
				if m.splitShowing(d) != nil {
					w = inner
				}
				d.layOut(rows, w)
				return m, nil
			}
```

Add the i18n keys (all four bundles): `"[tab] other side  [bksp] left side  [esc] close"`, `"(no file)"`, `"[alt+↑↓] line  [spc] mark  [/] find  [X] close  [↑/↓] scroll  [ctrl+w] view"`.

- [ ] **Step 6: Run the split tests, the viewer tests and the gates**

Run: `go test ./internal/tui -run 'TestSplit|TestFileViewer|TestOpenFiles|TestI18n|TestMenu|TestOptions|TestEngineProse' -count=1`
Expected: PASS. If `TestSplitRenderHasOneLitBorderAndOneHint` fails on the corner count, print `v` and count the rounded corner `╭` instead of the style getter.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/split_view.go internal/tui/split_doc_pane.go internal/tui/split_view_test.go internal/tui/layer_stack.go internal/tui/mouse.go internal/tui/preview_select.go internal/tui/open_files.go internal/tui/file_viewer.go internal/tui/open_file.go internal/tui/overview_keys.go internal/tui/open_file_notes.go internal/tui/model.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml
git commit -m "tui: the split layer — two panes, one focused side, a document pane"
```

---

### Task 4: The overview pane, steering in single mode, the review tree's door

**Files:**
- Create: `internal/tui/split_overview.go`
- Modify: `internal/tui/review_view.go:516-548` (`openReviewOverviewDoc` → factor `reviewOverviewDoc`), `internal/tui/files_view.go:1260-1262` (the `l.overviewDoc` branch), `internal/tui/overview_keys.go:251-283` (`overviewRows`: the split's rows)
- Test: `internal/tui/split_overview_test.go`; update `internal/tui/review_overview_test.go:41-63` (`TestStoredOverviewRowOpensTheViewer`)

**Interfaces:**
- Consumes: `splitView`, `docPane`, `paneGeom`, `splitFocus` (Task 3); `reviewStacked` (Task 1).
- Produces:
  ```go
  type overviewPane struct { docPane; sv *splitView }
  func (m Model) reviewOverviewDoc(st *reviewViewState, rows, width int) *openFile // find-or-build, laid out, plain anchors marked, first anchor selected on a fresh build
  func (m Model) openReviewOverviewSplit() (Model, tea.Cmd)
  func (m Model) steerSplit(sv *splitView) (Model, tea.Cmd)        // show the selected anchor on the right
  func (m Model) docSteer(sv *splitView, r *docPane, ov *openFile, a anchor) (Model, tea.Cmd)
  func (m Model) landOnOverviewRow() Model
  func anchorSource(ov *openFile) fileSource                          // srcCommit at the tip, srcWorktree for a working review
  ```
  Task 5 replaces the `case *stackPane` placeholder in `steerSplit`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/split_overview_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// A review of stackRepoModel's tip (a.go, b.go, c.go each gain a func)
// with an overview of three resolvable anchors across two files, one plain
// (zzz.go) and one note: anchor — opened as the split.
func reviewSplitModel(t *testing.T) (Model, *splitView) {
	t.Helper()
	doc := `{"version":1,"summary":"## Summary\nfine","overview":"Start at [A](a.go:3), then [B](b.go:3), [gone](zzz.go:1), [a note](note:t1), last [A again](a.go:1-3).","files":[{"path":"a.go","annotations":[{"newRange":[3,3],"summary":"A"}]},{"path":"b.go","annotations":[{"newRange":[3,3],"summary":"B"}]}]}`
	m, id := reviewViewModel(t, doc)
	m, cmd := m.openReview(id, "review")
	m = drainCmds(t, m, cmd)
	u, cmd := m.openDiffForFileLine(overviewRow(t, m))
	m = drainCmds(t, u.(Model), cmd)
	sv := m.splitLayer()
	if sv == nil || m.topLayer() != sv {
		t.Fatalf("top = %T, want the split", m.topLayer())
	}
	return m, sv
}

func key(t *testing.T, m Model, k string) Model {
	t.Helper()
	u, cmd := m.Update(synthKey(k))
	return drainCmds(t, u.(Model), cmd)
}

func TestOverviewSplitOpensWithTheFirstAnchorShown(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	ov := sv.left.doc()
	if ov == nil || ov.ov == nil || ov.src.kind != srcReviewOverview || ov.ov.sel != 0 || !sv.onLeft {
		t.Fatalf("left = %+v onLeft %v", ov, sv.onLeft)
	}
	r := sv.right.doc()
	if r == nil || r.path != "a.go" || r.src.kind != srcCommit || r.src.rev != ov.ov.tip || !docLoaded(r) || r.p.cur != 2 {
		t.Fatalf("right = %+v", r)
	}
	if r.from != ov || r.anchorCur != "a.go:3" {
		t.Fatalf("from %v anchorCur %q: the bands need the way back", r.from, r.anchorCur)
	}
	if !m.openFiles.has(m.currentWorktree, r) {
		t.Fatal("a single-mode document is a registered open file")
	}
}

func TestOverviewSplitDownStepsAnchorsAndSteers(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	ov := sv.left.doc()
	m = key(t, m, "down")
	if ov.ov.sel != 1 || sv.right.doc().path != "b.go" || sv.right.doc().p.cur != 2 || !sv.onLeft {
		t.Fatalf("sel %d right %s cur %d onLeft %v", ov.ov.sel, sv.right.doc().path, sv.right.doc().p.cur, sv.onLeft)
	}
	m = key(t, m, "down") // zzz.go and note:t1 are plain: skipped
	if ov.ov.sel != 4 || sv.right.doc().path != "a.go" || sv.right.doc().p.cur != 0 || sv.right.doc().anchorCur != "a.go:1-3" {
		t.Fatalf("sel %d right %s cur %d anchor %q", ov.ov.sel, sv.right.doc().path, sv.right.doc().p.cur, sv.right.doc().anchorCur)
	}
	if sv.right.doc().p.lsel.on {
		t.Fatal("a range anchor is a band, not a selection")
	}
	m = key(t, m, "down") // wraps
	if ov.ov.sel != 0 {
		t.Fatalf("wrap: sel %d", ov.ov.sel)
	}
	m = key(t, m, "up")
	if ov.ov.sel != 4 {
		t.Fatalf("up wraps back: sel %d", ov.ov.sel)
	}
	// The same file again: the document is reused, not reopened.
	before := sv.right.doc()
	m = key(t, m, "up") // b.go
	m = key(t, m, "down")
	if sv.right.doc() != before {
		t.Fatal("steering back to a file reuses its open document")
	}
}

func TestOverviewSplitEnterFocusesRightAndBackspaceReturns(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	m = key(t, m, "enter")
	if sv.onLeft {
		t.Fatal("enter focuses the right pane")
	}
	m = key(t, m, "n") // the band walk moves the overview's selection
	if sv.left.doc().ov.sel != 4 || sv.right.doc().anchorCur != "a.go:1-3" {
		t.Fatalf("sel %d anchor %q", sv.left.doc().ov.sel, sv.right.doc().anchorCur)
	}
	m = key(t, m, "backspace")
	if !sv.onLeft || m.topLayer() != sv {
		t.Fatal("backspace focuses the overview, the split stays")
	}
}

func TestOverviewSplitPageKeysKeepTheSelection(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	ov := sv.left.doc()
	for _, k := range []string{"pgdown", "pgup", "end", "home"} {
		m = key(t, m, k)
		if ov.ov.sel != 0 {
			t.Fatalf("%s changed the selection to %d", k, ov.ov.sel)
		}
	}
}

func TestOverviewSplitEscLandsOnTheOverviewRow(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	ov, r := sv.left.doc(), sv.right.doc()
	m = key(t, m, "tab")
	m = key(t, m, "down")
	m = key(t, m, "esc")
	if m.topLayer() != nil || m.filesReview == nil {
		t.Fatalf("top %T review %v: esc returns to the review tree", m.topLayer(), m.filesReview != nil)
	}
	if !m.filesView.visible()[m.filesView.sel].overviewDoc {
		t.Fatalf("cursor on row %d, want the ≡ Overview row", m.filesView.sel)
	}
	if !ov.backgrounded || !r.backgrounded || !m.openFiles.has(m.currentWorktree, ov) || !m.openFiles.has(m.currentWorktree, r) {
		t.Fatal("both documents stay open in the background")
	}
	// Enter again finds the same overview document, selection kept.
	u, cmd := m.openDiffForFileLine(overviewRow(t, m))
	m = drainCmds(t, u.(Model), cmd)
	if sv2 := m.splitLayer(); sv2 == nil || sv2.left.doc() != ov {
		t.Fatal("reopening reuses the overview document")
	}
}

func TestOverviewSplitNarrowTerminalOpensTheViewer(t *testing.T) {
	t.Parallel()
	m, _ := reviewWithOverview(t)
	m.width = 59
	u, cmd := m.openDiffForFileLine(overviewRow(t, m))
	m = drainCmds(t, u.(Model), cmd)
	if _, ok := m.topLayer().(*fileViewer); !ok || m.splitLayer() != nil {
		t.Fatalf("top %T: below %d columns today's viewer opens", m.topLayer(), splitMinWidth)
	}
}

func TestSplitSteerToMissingFileShowsPlaceholder(t *testing.T) {
	t.Parallel()
	doc := `{"version":1,"summary":"s","overview":"See [gone](a.go:1).","files":[{"path":"a.go","annotations":[{"newRange":[1,1],"summary":"x"}]}]}`
	m, id := reviewViewModel(t, doc)
	m, cmd := m.openReview(id, "review")
	m = drainCmds(t, m, cmd)
	// The review resolved a.go at the tip, but the object store answers for
	// a tip that no longer has it: point the overview at a bogus tip.
	u, cmd := m.openDiffForFileLine(overviewRow(t, m))
	m = drainCmds(t, u.(Model), cmd)
	sv := m.splitLayer()
	sv.left.doc().ov.tip = "0000000000000000000000000000000000000000"
	sv.right.(*docPane).d = nil
	m = key(t, m, "down")
	r := sv.right.doc()
	if r == nil || docLoaded(r) || !strings.Contains(ansi.Strip(m.View()), "load failed") {
		t.Fatalf("right %+v: a file gone at the tip shows the load placeholder\n%s", r, ansi.Strip(m.View()))
	}
}

func TestOverviewSplitRenderShowsBothSides(t *testing.T) {
	t.Parallel()
	m, _ := reviewSplitModel(t)
	v := ansi.Strip(m.View())
	for _, want := range []string{"Overview: ", "Start at", "View a.go @", "func A()", "[↑↓] anchor"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q:\n%s", want, v)
		}
	}
}

var _ = tea.KeyMsg{}
```

Add to `internal/tui/open_files.go` (the registry) if missing:

```go
// has reports whether d is in wt's open files.
func (r *openFilesReg) has(wt string, d *openFile) bool {
	for _, e := range r.list(wt) {
		if e == d {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/tui -run 'TestOverviewSplit|TestSplitSteer' -count=1`
Expected: FAIL to compile — `openReviewOverviewSplit`/`overviewPane` undefined.

- [ ] **Step 3: Write the overview pane and the steer**

Create `internal/tui/split_overview.go`:

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// The overview pane (spec §3): the review's stored overview on the left of
// a split. Up/down step anchor to anchor and STEER the right pane to the
// anchor's target; enter steers and moves the focus right. Page keys and
// home/end scroll the text and leave the selection alone.

// overviewPane is a docPane over an overview document plus the split it
// steers.
type overviewPane struct {
	docPane
	sv *splitView
}

func (p *overviewPane) hint(m Model) string {
	return i18n.T("[↑↓] anchor  [enter] open  [/] find  [y] copy  [.] menu")
}

func (p *overviewPane) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	d := p.d
	if d == nil || d.ov == nil {
		return p.docPane.update(m, msg)
	}
	if nm, cmd, ok := m.previewSearchKey(msg); ok { // a search being typed, or /, ], [ — before the walk
		return nm, cmd
	}
	_, rows, _, _ := m.activePreview()
	switch msg.String() {
	case "down", "j":
		d.stepAnchor(1, rows)
		return m.steerSplit(p.sv)
	case "up", "k":
		d.stepAnchor(-1, rows)
		return m.steerSplit(p.sv)
	case "enter":
		nm, cmd := m.steerSplit(p.sv)
		p.sv.onLeft = false
		return nm, cmd
	case "y":
		return m, m.copyToClipboardCmd(i18n.T("copied"), d.ov.text)
	case "r":
		return m, nil // a stored overview has no store reference (A9)
	}
	return p.docPane.update(m, msg)
}

// anchorSource is where an overview's anchors open: the reviewed tip, or
// the working tree for a working review.
func anchorSource(ov *openFile) fileSource {
	if ov.ov.tip == "" {
		return fileSource{kind: srcWorktree}
	}
	return fileSource{kind: srcCommit, rev: ov.ov.tip}
}

// steerSplit shows the overview's selected anchor on the right pane. A
// plain anchor is never selected, so there is always a target.
func (m Model) steerSplit(sv *splitView) (Model, tea.Cmd) {
	ov := sv.left.doc()
	if ov == nil || ov.ov == nil || ov.ov.sel < 0 || ov.ov.sel >= len(ov.ov.anchors) {
		return m, nil
	}
	a := ov.ov.anchors[ov.ov.sel]
	if a.plain || a.target.Note != "" {
		return m, nil
	}
	switch r := sv.right.(type) {
	case *docPane:
		return m.docSteer(sv, r, ov, a)
	}
	return m, nil
}

// docSteer is the single-mode steer: the anchor's file at the tip in the
// right pane, the cursor on its line, the anchor's band current. The same
// file lands without a reload; another file replaces the pane's document,
// which stays open in the background. A file never opened is read off the
// UI thread; liveDoc finds the pane when the lines arrive.
func (m Model) docSteer(sv *splitView, r *docPane, ov *openFile, a anchor) (Model, tea.Cmd) {
	src, path := anchorSource(ov), a.target.Path
	rows, _ := sv.geomOf(m, r)
	land := func(d *openFile) {
		d.from, d.anchorCur = ov, a.dest
		d.pendingLine, d.pendingEnd = a.target.Start, 0 // a band, never a selection
	}
	if d := r.d; d != nil && d.key() == docKey(src, path) {
		land(d)
		if docLoaded(d) {
			d.landPendingLine(rows)
			return m, nil
		}
		return m, m.loadDoc(d)
	}
	d := m.openFiles.find(m.currentWorktree, docKey(src, path))
	if d == nil {
		d = m.newOpenFile(src, path)
	} else {
		m = m.detachDoc(d) // out of any other frame; never this split (the key differs)
	}
	if old := r.d; old != nil {
		old.backgrounded = true
	}
	d.backgrounded = true // esc on the split steps aside; only X closes
	land(d)
	r.d = d
	m = m.registerDoc(d)
	if docLoaded(d) && !d.onDisk() {
		d.landPendingLine(rows)
		return m, nil
	}
	return m, m.loadDoc(d)
}

// landOnOverviewRow puts the review tree's cursor on its ≡ Overview row —
// where the split was opened from.
func (m Model) landOnOverviewRow() Model {
	if m.filesView == nil {
		return m
	}
	for i, l := range m.filesView.visible() {
		if l.overviewDoc {
			m.filesView.sel = i
			break
		}
	}
	return m
}

// openReviewOverviewSplit is enter on the review tree's ≡ Overview row: the
// overview on the left, its first anchor's file on the right (stacked when
// the user chose so), the focus on the overview. Too narrow a terminal
// opens the full-screen overview viewer instead.
func (m Model) openReviewOverviewSplit() (Model, tea.Cmd) {
	st := m.filesReview
	if st == nil || st.overview == nil {
		return m, nil
	}
	if w, _ := m.overlayDims(); w < splitMinWidth {
		return m.openReviewOverviewDoc()
	}
	lw, _, bh := m.splitGeom()
	rows, inner := paneGeom(lw, bh)
	d := m.reviewOverviewDoc(st, rows, inner)
	sv := &splitView{onLeft: true}
	sv.left = &overviewPane{docPane: docPane{d: d}, sv: sv}
	sv.right = &docPane{}
	if m.reviewStacked {
		sv.right = m.newStackPane(sv, st) // Task 5
	}
	sv.onClose = func(m Model) Model {
		d.backgrounded = true
		if rd := sv.right.doc(); rd != nil {
			rd.backgrounded = true
		}
		return m.landOnOverviewRow()
	}
	m = m.detachDoc(d)
	m = m.pushLayer(sv)
	m = m.registerDoc(d)
	return m.steerSplit(sv)
}
```

Until Task 5 exists, write `newStackPane` as a stub in `split_overview.go` so the package compiles: `func (m Model) newStackPane(sv *splitView, st *reviewViewState) pane { return &docPane{} }` with the comment `// newStackPane is Task 5's; single mode until then.` (Task 5 deletes this stub.)

In `internal/tui/review_view.go`, split `openReviewOverviewDoc` into the builder and the viewer opener:

```go
// reviewOverviewDoc is the review's stored overview as an open document
// (R12, R5): found among the open files, or built — laid out at rows×width,
// its anchors the review could not resolve marked plain, the first resolved
// one current. Registered by the caller once it is in its frame, under its
// own source kind so the agent-docs sync never touches it.
func (m Model) reviewOverviewDoc(st *reviewViewState, rows, width int) *openFile {
	src := fileSource{kind: srcReviewOverview, rev: st.id}
	path := "overview-" + st.id + ".md"
	if d := m.openFiles.find(m.currentWorktree, docKey(src, path)); d != nil {
		return d
	}
	d := m.newOpenFile(src, path)
	d.title = i18n.T("Overview: %s", reviewLabel(st.review))
	d.p.prose, d.p.mode = true, modeScroll
	d.ov = &overview{text: st.overview.Text, sel: -1, tip: st.tip}
	d.layOut(rows, width)
	if len(d.ov.anchors) == len(st.overview.Anchors) { // the same parser, the same order
		for i := range d.ov.anchors {
			d.ov.anchors[i].plain = !st.overview.Anchors[i].OK
		}
		d.ov.paint(d.p.lines)
	}
	d.stepAnchor(1, rows) // the first resolved anchor is current on open
	return d
}

// openReviewOverviewDoc opens the review's stored overview in the
// full-screen overview viewer — the narrow-terminal door (the review tree's
// enter opens the split, openReviewOverviewSplit).
func (m Model) openReviewOverviewDoc() (Model, tea.Cmd) {
	st := m.filesReview
	if st == nil || st.overview == nil {
		return m, nil
	}
	rows, inner := m.viewerGeom()
	d := m.reviewOverviewDoc(st, rows, m.overviewWidth(inner))
	if m.docShown(d) {
		return m.bringToFront(d)
	}
	m = m.pushLayer(&fileViewer{d})
	m = m.registerDoc(d)
	return m, nil
}
```

In `internal/tui/files_view.go` `openDiffForFileLine`, change `return m.openReviewOverviewDoc()` to `return m.openReviewOverviewSplit()`.

In `internal/tui/overview_keys.go` `overviewRows`, the row list gets the split's words: where `d.ov != nil`, when `m.splitLayer() != nil && m.topLayer() == m.splitLayer()` use key labels `"down"`/`"up"` for Next/Previous anchor (`d.stepAnchor(±1, rows)` then `m.steerSplit(sv)`), an "Open anchor" row that runs `m.steerSplit(sv)` and sets `sv.onLeft = false`, and a `"Copy overview"` copy row (`m.copyRow("overview-copy", i18n.T("Copy overview"), i18n.T("copied"), d.ov.text)`); otherwise the rows as today. Drop the "Back to overview" row when the document is in a split (backspace is the split's).

i18n keys (four bundles): `"[↑↓] anchor  [enter] open  [/] find  [y] copy  [.] menu"`, `"Previous anchor"`, `"Copy overview"`.

- [ ] **Step 4: Update the existing viewer test**

In `internal/tui/review_overview_test.go` rename `TestStoredOverviewRowOpensTheViewer` to `TestStoredOverviewRowOpensTheSplit` and make its assertion: `sv := m.splitLayer(); d := sv.left.doc()` with the same checks on `d` (kind, tip, `sel == 0`) and the view containing `"Overview: "` and `"the change"`. The other tests in that file open the viewer through `openReviewOverviewDoc()` directly where they test viewer behaviour; where one goes through `openDiffForFileLine`, switch it to `m.openReviewOverviewDoc()` so it keeps testing the viewer.

- [ ] **Step 5: Run the tests and gates**

Run: `go test ./internal/tui -run 'TestOverviewSplit|TestSplit|TestStoredOverview|TestI18n|TestMenu' -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/split_overview.go internal/tui/split_overview_test.go internal/tui/review_view.go internal/tui/files_view.go internal/tui/overview_keys.go internal/tui/open_files.go internal/tui/review_overview_test.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml
git commit -m "tui: the review overview opens as a split — anchors steer the file pane"
```

---

### Task 5: The stacked file pane

**Files:**
- Create: `internal/tui/split_stack.go` (the pure stream model), `internal/tui/split_stack_pane.go` (the pane, loading, keys, S)
- Modify: `internal/tui/split_overview.go` (`steerSplit`: the `*stackPane` case; delete the `newStackPane` stub), `internal/tui/split_doc_pane.go` (`S` in single mode), `internal/tui/anchor_bands.go:100-113` (`bands`/`curBand` over a stream), `internal/tui/open_file.go` (the `stream` field), `internal/tui/model.go` (the `stackPaneMsg` case)
- Test: `internal/tui/split_stack_test.go`, `internal/tui/split_stack_pane_test.go`

**Interfaces:**
- Consumes: `splitView`, `docPane`, `overviewPane`, `steerSplit`, `anchorSource`, `reviewStacked`, `setReviewStackedPref`.
- Produces:
  ```go
  // split_stack.go — pure
  type streamFile struct { path, status string; notes int; lines []contentLine; folded, loaded bool; err error; start, hdr, body int }
  type fileStream struct { files []streamFile }
  const streamFoldOver = 100
  func (s *fileStream) build() []contentLine                       // stamps start/hdr/body, returns the stream
  func (s *fileStream) fileAt(line int) int                        // the file index a stream line belongs to; -1 above the first
  func (s *fileStream) streamLine(file, line int) int              // 1-based file line → stream index (header when the file has no lines yet)
  func (s *fileStream) nextHeader(from, dir int) int               // the next/previous header index, -1 at the ends
  // split_stack_pane.go
  type stackPane struct { docPane; sv *splitView; st *reviewViewState; stream *fileStream; gen int; inflight int; park *stackPark }
  type stackPark struct { anchor int }                              // the overview anchor owed once its file loads
  type stackPaneMsg struct { slotStamp; tag string; gen, file int; lines []contentLine; err error }
  func (m Model) newStackPane(sv *splitView, st *reviewViewState) pane
  func (m Model) stackSteer(sv *splitView, r *stackPane, a anchor) (Model, tea.Cmd)
  func (m Model) toggleReviewStacked(sv *splitView) (Model, tea.Cmd)
  // open_file.go
  openFile.stream *fileStream   // set only on a stack document: bands and curBand are per file through it
  ```
  `srcReviewStack` is a new `fileSourceKind` (rev = review id, path = "files-<id>").

- [ ] **Step 1: Write the failing pure-stream tests**

Create `internal/tui/split_stack_test.go`:

```go
package tui

import "testing"

func lines(n int) []contentLine {
	out := make([]contentLine, n)
	for i := range out {
		out[i] = contentLine{text: "x", raw: "x", src: true}
	}
	return out
}

// The stream: blank (not on the first file), header, rule, then the
// content or one placeholder; start/hdr/body stamped per file.
func TestFileStreamBuildStampsOffsets(t *testing.T) {
	t.Parallel()
	s := &fileStream{files: []streamFile{
		{path: "a.go", status: "M", notes: 2, lines: lines(3), loaded: true},
		{path: "b.go", status: "A"},                      // not loaded: (loading…)
		{path: "c.go", status: "D", folded: true, loaded: true, lines: lines(5)},
	}}
	got := s.build()
	a, b, c := s.files[0], s.files[1], s.files[2]
	if a.start != 0 || a.hdr != 0 || a.body != 2 {
		t.Fatalf("a = %+v", a)
	}
	if b.start != 5 || b.hdr != 6 || b.body != 8 {
		t.Fatalf("b = %+v (blank 5, header 6, rule 7, placeholder 8)", b)
	}
	if c.start != 9 || c.hdr != 10 || c.body != 12 || len(got) != 13 {
		t.Fatalf("c = %+v len %d", c, len(got))
	}
	if got[a.hdr].text != "━━ a.go  (M)  ◆2" || !got[a.hdr].stackHeader {
		t.Fatalf("header = %q", got[a.hdr].text)
	}
	if got[b.body].text != "  (loading…)" || got[c.body].text != "  (folded)" || got[a.body].src != true || got[b.body].src {
		t.Fatalf("placeholders: %q %q", got[b.body].text, got[c.body].text)
	}
}

func TestFileStreamMapsLinesBothWays(t *testing.T) {
	t.Parallel()
	s := &fileStream{files: []streamFile{
		{path: "a.go", lines: lines(3), loaded: true},
		{path: "b.go", lines: lines(2), loaded: true},
	}}
	s.build()
	for line, want := range map[int]int{0: 0, 2: 0, 4: 0, 5: 1, 6: 1, 8: 1, 9: 1} {
		if got := s.fileAt(line); got != want {
			t.Fatalf("fileAt(%d) = %d, want %d", line, got, want)
		}
	}
	if s.streamLine(1, 2) != 9 || s.streamLine(0, 1) != 2 {
		t.Fatalf("streamLine: %d %d", s.streamLine(1, 2), s.streamLine(0, 1))
	}
	if s.streamLine(1, 9) != 9 { // past the file's end: clamped to its last line
		t.Fatalf("clamp: %d", s.streamLine(1, 9))
	}
	s.files[1].loaded = false
	s.build()
	if s.streamLine(1, 2) != s.files[1].hdr {
		t.Fatal("an unloaded file's line is its header")
	}
	if s.nextHeader(0, 1) != s.files[1].hdr || s.nextHeader(s.files[1].hdr, 1) != -1 || s.nextHeader(s.files[1].hdr, -1) != 0 {
		t.Fatal("nextHeader walks header to header")
	}
}

func TestFileStreamBandsArePerFile(t *testing.T) {
	t.Parallel()
	ov := &openFile{p: &contentPopup{}, ov: &overview{anchors: []anchor{
		{dest: "a.go:2", target: agentdocsAnchor("a.go", 2, 0)},
		{dest: "b.go:1-2", target: agentdocsAnchor("b.go", 1, 2)},
		{dest: "b.go:9", target: agentdocsAnchor("b.go", 9, 0)}, // past b's end: dropped
	}}}
	s := &fileStream{files: []streamFile{
		{path: "a.go", lines: lines(3), loaded: true},
		{path: "b.go", lines: lines(2), loaded: true},
	}}
	d := &openFile{p: &contentPopup{lines: s.build()}, stream: s, from: ov, anchorCur: "b.go:1-2"}
	d.p.lines[0].src = true
	bs := d.bands()
	if len(bs) != 2 || bs[0].start != 3 || bs[0].end != 3 || bs[1].start != 9 || bs[1].end != 10 {
		t.Fatalf("bands = %+v (stream lines, 1-based)", bs)
	}
	if d.curBand(bs) != 1 {
		t.Fatalf("curBand = %d", d.curBand(bs))
	}
}
```

Add the helper to the same file:

```go
func agentdocsAnchor(path string, start, end int) agentdocs.Anchor {
	return agentdocs.Anchor{Path: path, Start: start, End: end}
}
```

(import `github.com/homeend/gigagit/internal/agentdocs`). Check `agentdocs.Anchor`'s field names with `grep -n 'type Anchor struct' -A12 internal/agentdocs/*.go` and adjust the helper to them.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/tui -run TestFileStream -count=1`
Expected: FAIL to compile — `fileStream undefined`.

- [ ] **Step 3: Write the pure stream**

Add to `internal/tui/content_popup.go`'s `contentLine` struct a field `stackHeader bool // a split stack's file header row (split_stack.go)`.

Create `internal/tui/split_stack.go`:

```go
package tui

import (
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/i18n"
)

// The stacked file pane's STREAM (spec §4.2): every review file, one after
// another — a blank line (not on the first), a header, a rule, then the
// file's lines or one placeholder. Pure: no loading, no keys. Every
// (file, line) ↔ stream mapping goes through this table, never through a
// line number alone — line numbers repeat across files.

// streamFile is one file of the stream and where its block sits.
type streamFile struct {
	path, status string
	notes        int // the review's notes on it (◆n on the header)
	lines        []contentLine
	folded       bool
	loaded       bool  // lines (or err) are final
	loading      bool  // a load is out for it
	err          error // the load failed
	start        int   // the block's first stream line (the blank, or the header on the first file)
	hdr          int   // the header line
	body         int   // the first content line, or the placeholder
}

// fileStream is the table the pane rebuilds the document's lines from.
type fileStream struct{ files []streamFile }

// streamFoldOver is the file count above which a stream opens folded
// (every file but the target), as the stacked diff does.
const streamFoldOver = 100

// headerText is a file's header row: path, status, note count.
func headerText(f streamFile) string {
	t := "━━ " + f.path
	if f.status != "" {
		t += "  (" + f.status + ")"
	}
	if f.notes > 0 {
		t += "  ◆" + strconv.Itoa(f.notes)
	}
	return t
}

// build lays the stream out and stamps each file's start/hdr/body.
func (s *fileStream) build() []contentLine {
	var out []contentLine
	for i := range s.files {
		f := &s.files[i]
		if i > 0 {
			out = append(out, contentLine{})
		}
		f.start = len(out)
		if i == 0 {
			f.start = 0
		}
		f.hdr = len(out)
		out = append(out, contentLine{text: headerText(*f), raw: headerText(*f), elide: true, stackHeader: true})
		out = append(out, contentLine{text: strings.Repeat("─", 200), dim: true, noWrap: true})
		f.body = len(out)
		switch {
		case f.folded:
			out = append(out, contentLine{text: "  " + i18n.T("(folded)"), dim: true})
		case !f.loaded:
			out = append(out, contentLine{text: "  " + i18n.T("(loading…)"), dim: true})
		case f.err != nil:
			out = append(out, contentLine{text: "  " + i18n.T("(load failed: %s)", f.err.Error()), dim: true})
		case len(f.lines) == 0:
			out = append(out, contentLine{text: "  " + i18n.T("(empty)"), dim: true})
		default:
			out = append(out, f.lines...)
		}
	}
	return out
}

// fileAt is the file whose block holds stream line line; -1 before the first.
func (s *fileStream) fileAt(line int) int {
	at := -1
	for i, f := range s.files {
		if line >= f.start {
			at = i
		}
	}
	return at
}

// streamLine is file's 1-based line as a stream index: clamped to the
// file's last line; the header when the file shows no lines (unloaded,
// folded, failed, empty).
func (s *fileStream) streamLine(file, line int) int {
	f := s.files[file]
	n := len(f.lines)
	if f.folded || !f.loaded || f.err != nil || n == 0 {
		return f.hdr
	}
	if line < 1 {
		line = 1
	}
	if line > n {
		line = n
	}
	return f.body + line - 1
}

// nextHeader is the header after (dir 1) / before (dir -1) stream line from,
// or -1 at the ends.
func (s *fileStream) nextHeader(from, dir int) int {
	at := s.fileAt(from)
	if dir > 0 {
		if at+1 < len(s.files) {
			return s.files[at+1].hdr
		}
		return -1
	}
	if at > 0 && from == s.files[at].hdr {
		return s.files[at-1].hdr
	}
	if at > 0 || (at == 0 && from != s.files[0].hdr) {
		return s.files[max(at, 0)].hdr
	}
	return -1
}
```

Check `contentLine.dim` exists (it does: `reviewTreeLines` uses it).

In `internal/tui/open_file.go` add the field after `pendingEnd`:

```go
	// stream is set on a split stack's document only (split_stack.go): its
	// lines are several files', so bands and the current band are found
	// per file through it.
	stream *fileStream
```

In `internal/tui/anchor_bands.go` replace `bands` and `curBand`:

```go
func (d *openFile) bands() []anchorBand {
	if d.from == nil || d.from.ov == nil || d.from.ov.closed || !docLoaded(d) || d.p.img != nil {
		return nil
	}
	if d.stream == nil {
		return anchorBands(d.from.ov.anchors, d.path, len(d.p.lines))
	}
	var out []anchorBand
	for _, f := range d.stream.files {
		if f.folded || !f.loaded || f.err != nil {
			continue
		}
		for _, b := range anchorBands(d.from.ov.anchors, f.path, len(f.lines)) {
			out = append(out, anchorBand{start: f.body + b.start, end: f.body + b.end, i: b.i})
		}
	}
	return out
}

// curBand is the index in bands of d's current anchor, or -1.
func (d *openFile) curBand(bands []anchorBand) int {
	if d.from == nil || d.from.ov == nil {
		return -1
	}
	if d.stream == nil {
		return bandOf(bands, d.from.ov.anchors, d.anchorCur, len(d.p.lines))
	}
	for k, b := range bands {
		if d.from.ov.anchors[b.i].dest == d.anchorCur {
			return k
		}
	}
	return -1
}
```

(A stream band's `start`/`end` are stream lines 1-based: `f.body + b.start` because `b.start` is the file line 1-based and `f.body` the 0-based stream index of file line 1 — so stream line 1-based = `f.body + b.start`.) Note `docLoaded(d)` needs `d.p.lines[0].src`: the pane marks its header line `src: true` after build (Step 5) so the stack document counts as loaded.

- [ ] **Step 4: Run the pure tests**

Run: `go test ./internal/tui -run 'TestFileStream' -count=1`
Expected: PASS.

- [ ] **Step 5: Write the failing pane tests**

Create `internal/tui/split_stack_pane_test.go`:

```go
package tui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func stackedSplitModel(t *testing.T) (Model, *splitView, *stackPane) {
	t.Helper()
	m, sv := reviewSplitModel(t)
	m = key(t, m, "S")
	sp, ok := sv.right.(*stackPane)
	if !ok {
		t.Fatalf("right = %T after S", sv.right)
	}
	return m, sv, sp
}

func TestStackPaneOpensEveryReviewFileAndLandsTheAnchor(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	if len(sp.stream.files) != 3 || sp.stream.files[0].path != "a.go" {
		t.Fatalf("files = %+v", sp.stream.files)
	}
	for _, f := range sp.stream.files {
		if !f.loaded || f.err != nil || f.folded {
			t.Fatalf("every file of a small stack loads unfolded: %+v", f)
		}
	}
	if sp.inflight != 0 || !m.reviewStacked || !m.promptStore.StackedReviewFiles() {
		t.Fatalf("inflight %d pref %v/%v", sp.inflight, m.reviewStacked, m.promptStore.StackedReviewFiles())
	}
	d := sv.right.doc()
	if d.p.cur != sp.stream.streamLine(0, 3) || d.anchorCur != "a.go:3" {
		t.Fatalf("cur %d anchor %q: S keeps the cursor's file and line", d.p.cur, d.anchorCur)
	}
	if m.openFiles.has(m.currentWorktree, d) {
		t.Fatal("the stream document is not an open file")
	}
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "━━ a.go") || !strings.Contains(v, "◆1") || !strings.Contains(v, "Files: ") {
		t.Fatalf("render:\n%s", v)
	}
}

func TestStackPaneSteerScrollsTheStream(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	m = key(t, m, "down") // b.go:3
	d := sv.right.doc()
	if d.p.cur != sp.stream.streamLine(1, 3) || d.anchorCur != "b.go:3" {
		t.Fatalf("cur %d anchor %q", d.p.cur, d.anchorCur)
	}
	if sv.right != sp {
		t.Fatal("a stacked steer never replaces the pane")
	}
}

func TestStackPaneFoldKeysAndFileStep(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	m = key(t, m, "tab")
	m = key(t, m, "-")
	d := sv.right.doc()
	if !sp.stream.files[0].folded || d.p.cur != sp.stream.files[0].hdr {
		t.Fatalf("- folds the cursor's file, the cursor on its header: folded %v cur %d", sp.stream.files[0].folded, d.p.cur)
	}
	m = key(t, m, "-")
	if sp.stream.files[0].folded {
		t.Fatal("- again unfolds")
	}
	m = key(t, m, "_")
	for _, f := range sp.stream.files {
		if !f.folded {
			t.Fatal("_ folds all")
		}
	}
	m = key(t, m, "_")
	m = key(t, m, "N")
	if d.p.cur != sp.stream.files[1].hdr {
		t.Fatalf("N steps to the next header: cur %d", d.p.cur)
	}
	m = key(t, m, "P")
	if d.p.cur != sp.stream.files[0].hdr {
		t.Fatalf("P steps back: cur %d", d.p.cur)
	}
}

func TestStackPaneBandWalkCrossesFiles(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	m = key(t, m, "tab")
	d, ov := sv.right.doc(), sv.left.doc()
	m = key(t, m, "n")
	if d.anchorCur != "b.go:3" || ov.ov.sel != 1 || d.p.cur != sp.stream.streamLine(1, 3) {
		t.Fatalf("n: anchor %q sel %d cur %d (a.go:1-3 sorts before a.go:3 — the walk is in LINE order: a.go:1-3, a.go:3, b.go:3)", d.anchorCur, ov.ov.sel, d.p.cur)
	}
	m = key(t, m, "n")
	if d.anchorCur != "a.go:1-3" || ov.ov.sel != 4 {
		t.Fatalf("n wraps to the first band: %q %d", d.anchorCur, ov.ov.sel)
	}
	m = key(t, m, "p")
	if d.anchorCur != "b.go:3" {
		t.Fatalf("p: %q", d.anchorCur)
	}
}

func TestStackPaneBandWalkOpensAFoldedFile(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	m = key(t, m, "tab")
	sp.stream.files[1].folded = true
	sp.rebuild(m, sv)
	m = key(t, m, "n") // the next band is in b.go, folded: it unfolds and lands
	d := sv.right.doc()
	if sp.stream.files[1].folded || d.anchorCur != "b.go:3" || d.p.cur != sp.stream.streamLine(1, 3) {
		t.Fatalf("folded %v anchor %q cur %d", sp.stream.files[1].folded, d.anchorCur, d.p.cur)
	}
}

func TestStackLoadAboveCursorKeepsPlace(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	d := sv.right.doc()
	// A long tail below the reader, so a top that keeps the screen row is
	// never clamped away by the stream's end.
	u, _ := m.Update(stackPaneMsg{slotStamp: m.stamp(), tag: d.tag, gen: sp.gen, file: 2, lines: lines(80)})
	m = u.(Model)
	m = key(t, m, "down") // b.go:3
	cur, top := d.p.cur, d.p.sel
	screenRow := cur - top
	// a.go loads again, longer: the stream grows above the cursor.
	sp.stream.files[0].loaded = false
	sp.rebuild(m, sv)
	u, _ = m.Update(stackPaneMsg{slotStamp: m.stamp(), tag: d.tag, gen: sp.gen, file: 0, lines: lines(40)})
	m = u.(Model)
	if sp.stream.fileAt(d.p.cur) != 1 || d.p.cur-sp.stream.files[1].body != 2 {
		t.Fatalf("cursor left b.go:3: cur %d", d.p.cur)
	}
	if d.p.cur-d.p.sel != screenRow {
		t.Fatalf("screen row moved: %d → %d", screenRow, d.p.cur-d.p.sel)
	}
}

func TestStackStaleLoadIsDropped(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	d := sv.right.doc()
	before := len(d.p.lines)
	u, _ := m.Update(stackPaneMsg{slotStamp: m.stamp(), tag: d.tag, gen: sp.gen - 1, file: 0, lines: lines(40)})
	m = u.(Model)
	if len(d.p.lines) != before {
		t.Fatal("an old generation's load must not land")
	}
}

func TestStackPaneSToggleRoundTripsKeepingTheFile(t *testing.T) {
	t.Parallel()
	m, sv, sp := stackedSplitModel(t)
	m = key(t, m, "tab")
	m = key(t, m, "N") // b.go header
	m = key(t, m, "alt+down")
	m = key(t, m, "alt+down") // b.go line 1 (rule, then body)
	m = key(t, m, "S")
	dp, ok := sv.right.(*docPane)
	if !ok || dp.d == nil || dp.d.path != "b.go" || m.reviewStacked {
		t.Fatalf("right = %T %v stacked %v", sv.right, dp, m.reviewStacked)
	}
	m = key(t, m, "S")
	sp2, ok := sv.right.(*stackPane)
	if !ok || sp2 == sp || sp2.stream.fileAt(sv.right.doc().p.cur) != 1 {
		t.Fatalf("back to a new stack on b.go: %T", sv.right)
	}
}

func TestStackOverAHundredFilesOpensFolded(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	st := m.filesReview
	var many []contentLine
	for i := 0; i < 101; i++ {
		many = append(many, contentLine{path: "f" + strconv.Itoa(i) + ".go", status: "M"})
	}
	m.filesView.lines = append(m.filesView.lines[:2], append(many, contentLine{path: "a.go", status: "M"})...)
	sv.left.doc().ov.sel = 0 // a.go:3
	sp := m.newStackPane(sv, st).(*stackPane)
	folded := 0
	for _, f := range sp.stream.files {
		if f.folded {
			folded++
		}
	}
	if folded != 101 || sp.stream.files[len(sp.stream.files)-1].folded {
		t.Fatalf("folded %d of %d; the target stays open", folded, len(sp.stream.files))
	}
}
```

- [ ] **Step 6: Run them to see them fail**

Run: `go test ./internal/tui -run 'TestStackPane|TestStackLoad|TestStackStale|TestStackOver' -count=1`
Expected: FAIL to compile — `stackPane undefined`.

- [ ] **Step 7: Write the pane**

Add to `internal/tui/open_file.go`'s `fileSourceKind` consts: `srcReviewStack // a review split's stacked stream (openFile.stream); rev = review id, path = "files-<id>"`. Add it to the `frameTitle` switch's titled branch (`case srcExternal, srcNote, srcOverview, srcReviewOverview, srcReviewStack:`) and to `docLoader` as a no-op loader (`case srcReviewStack: return func(context.Context) ([]byte, error) { return nil, nil }` — the pane loads per file). Check `open_files.go:313`'s switch over source kinds (`case srcOverview, srcReviewOverview:`) and add `srcReviewStack` where the overview kinds are excluded from disk polling.

Create `internal/tui/split_stack_pane.go`:

```go
package tui

import (
	"context"
	"errors"
	"sort"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// The STACKED file pane (spec §4.2): one unregistered document whose lines
// are every review file's content at the reviewed tip, headers between
// them, loaded lazily. Bands, the cursor and every anchor are per file
// through the stream table.

// stackPane is the pane: the document, its stream, the loads in flight
// and the anchor a step owes once its file arrives.
type stackPane struct {
	docPane
	sv       *splitView
	st       *reviewViewState
	stream   *fileStream
	gen      int // tags every load; a rebuilt pane drops the old one's answers
	inflight int
	park     *stackPark
	// want is a (file, 1-based line) owed once that file loads — the place
	// S keeps when single turns stacked.
	want struct {
		file, line int
		set        bool
	}
}

// stackPark is a step parked on a load: overview anchor `anchor` lands once
// its file's lines are in.
type stackPark struct{ anchor int }

// stackPaneMsg is one file's load for the stream of the document tagged tag.
type stackPaneMsg struct {
	slotStamp
	tag       string
	gen, file int
	lines     []contentLine
	err       error
}

const stackMaxLoads = 3

// newStackPane builds the pane over the review tree's files (its rows with
// a path, in tree order); over streamFoldOver files all but the overview's
// selected anchor's file open folded. Nothing is loaded yet: the first
// steer (or pump) starts the loads.
func (m Model) newStackPane(sv *splitView, st *reviewViewState) pane {
	ov := sv.left.doc()
	target := ""
	if ov != nil && ov.ov != nil && ov.ov.sel >= 0 && ov.ov.sel < len(ov.ov.anchors) {
		target = ov.ov.anchors[ov.ov.sel].target.Path
	}
	s := &fileStream{}
	if m.filesView != nil {
		for _, l := range m.filesView.lines {
			if l.path == "" {
				continue
			}
			s.files = append(s.files, streamFile{path: l.path, status: l.status, notes: st.counts[l.path]})
		}
	}
	if len(s.files) > streamFoldOver {
		for i := range s.files {
			s.files[i].folded = s.files[i].path != target
		}
	}
	d := m.newOpenFile(fileSource{kind: srcReviewStack, rev: st.id}, "files-"+st.id)
	d.title = i18n.T("Files: %s", reviewLabel(st.review))
	d.stream, d.from = s, ov
	m.stackSeq++
	p := &stackPane{docPane: docPane{d: d}, sv: sv, st: st, stream: s, gen: m.stackSeq}
	p.rebuild(m, sv)
	return p
}

// rebuild lays the stream out again keeping the cursor's (file, line) and
// its screen row — a file loading above the reader must not move them.
func (p *stackPane) rebuild(m Model, sv *splitView) {
	d, s := p.d, p.stream
	rows, _ := sv.geomOf(m, p)
	file, off := -1, 0
	if len(d.p.lines) > 0 {
		file = s.fileAt(d.p.cur)
		if file >= 0 {
			off = d.p.cur - s.files[file].body
		}
	}
	screenRow := d.p.cur - d.p.sel
	d.p.lines = s.build()
	if len(d.p.lines) > 0 {
		d.p.lines[0].src = true // docLoaded: the stream is a loaded document
	}
	if file >= 0 {
		d.p.cur = s.streamLine(file, off+1)
		if screenRow >= 0 && screenRow < rows {
			d.p.sel = d.p.clampTop(d.p.cur-screenRow, rows)
		} else {
			d.p.ensureCursorVisible(rows)
		}
	}
	d.p.lsel.clear()
}

func (p *stackPane) title() string {
	k := p.stream.fileAt(p.d.p.cur) + 1
	return p.d.title + "  " + i18n.T("file %d/%d", max(k, 1), len(p.stream.files))
}

// render: the embedded docPane's render would call ITS title (Go has no
// virtual dispatch through an embedded struct), so the stack draws itself.
func (p *stackPane) render(m Model, w, h int, focused bool) string {
	return m.renderPreviewBoxOpts(p.d.p, p.title(), w, h, previewBoxOpts{focused: focused, cursorOff: !focused})
}

func (p *stackPane) hint(m Model) string {
	return i18n.T("[n/p] anchor  [N/P] file  [-] fold  [_] fold all  [S] single  [alt+↑↓] line  [spc] mark  [/] find")
}

// loadCmd reads file i of the stream off the UI thread.
func (p *stackPane) loadCmd(m Model, i int) tea.Cmd {
	f := p.stream.files[i]
	src := anchorSource(p.sv.left.doc())
	loader := func(ctx context.Context) ([]byte, error) { return m.svc.ShowFile(ctx, src.rev, f.path) }
	if src.kind == srcWorktree {
		loader = func(ctx context.Context) ([]byte, error) { return m.svc.WorktreeFile(ctx, f.path) }
	}
	read := loadFileContentSrcCmd(p.d.tag, f.path, m.cfg.UI.SyntaxOn(), loader)
	slot, tag, gen := m.stamp(), p.d.tag, p.gen
	return func() tea.Msg {
		r := read().(fileContentMsg)
		return stackPaneMsg{slotStamp: slot, tag: tag, gen: gen, file: i, lines: r.lines, err: r.err}
	}
}

// pump starts loads for unfolded files not yet read, the cursor's file
// first, at most stackMaxLoads at once.
func (p *stackPane) pump(m Model) tea.Cmd {
	var cmds []tea.Cmd
	n := len(p.stream.files)
	at := max(p.stream.fileAt(p.d.p.cur), 0)
	for k := 0; k < n && p.inflight < stackMaxLoads; k++ {
		i := (at + k) % n
		f := &p.stream.files[i]
		if f.folded || f.loaded || f.err != nil || f.loading {
			continue
		}
		f.loading = true
		p.inflight++
		cmds = append(cmds, p.loadCmd(m, i))
	}
	return tea.Batch(cmds...)
}

// stackPaneLanded is a load's arrival: the lines go into their file, the
// stream is rebuilt around the reader, a parked anchor lands, the next
// loads start.
func (m Model) stackPaneLanded(msg stackPaneMsg) (Model, tea.Cmd) {
	sv, p := m.stackPaneByTag(msg.tag)
	if p == nil || p.gen != msg.gen || msg.file >= len(p.stream.files) {
		return m, nil
	}
	f := &p.stream.files[msg.file]
	f.loading, f.loaded, f.lines, f.err = false, true, msg.lines, msg.err
	if msg.err == nil && (len(msg.lines) == 0 || !msg.lines[0].src) {
		f.lines = nil
		if len(msg.lines) > 0 { // a placeholder the loader named (too large, binary)
			f.err = errPlaceholder(msg.lines[0].text)
		}
	}
	p.inflight--
	p.rebuild(m, sv)
	if p.want.set && p.want.file == msg.file {
		p.want.set = false
		rows, _ := sv.geomOf(m, p)
		p.d.p.cur = p.stream.streamLine(msg.file, p.want.line)
		p.d.p.sel = p.d.p.clampTop(p.d.p.cur-rows/2, rows)
	}
	if pk := p.park; pk != nil && pk.anchor < len(sv.left.doc().ov.anchors) && sv.left.doc().ov.anchors[pk.anchor].target.Path == f.path {
		p.park = nil
		m = m.stackLand(sv, p, pk.anchor)
	}
	return m, p.pump(m)
}

// stackPaneByTag finds the split and stack pane whose document is tagged tag.
func (m Model) stackPaneByTag(tag string) (*splitView, *stackPane) {
	if m.layers == nil {
		return nil, nil
	}
	for _, l := range m.layers.entries {
		if sv, ok := l.(*splitView); ok {
			if p, ok := sv.right.(*stackPane); ok && p.d.tag == tag {
				return sv, p
			}
		}
	}
	return nil, nil
}

// stackLand puts the cursor on overview anchor i's line in the stream and
// makes it the current band; the overview's selection follows.
func (m Model) stackLand(sv *splitView, p *stackPane, i int) Model {
	ov := sv.left.doc()
	a := ov.ov.anchors[i]
	fi := p.fileIndex(a.target.Path)
	if fi < 0 {
		m.statusMsg = i18n.T("no file %s", a.target.Path)
		return m
	}
	rows, _ := sv.geomOf(m, p)
	p.d.anchorCur = a.dest
	p.d.p.cur = p.stream.streamLine(fi, a.target.Start)
	p.d.p.sel = p.d.p.clampTop(p.d.p.cur-rows/2, rows)
	vr, _ := sv.geomOf(m, sv.left)
	ov.selectAnchor(i, vr)
	return m
}

func (p *stackPane) fileIndex(path string) int {
	for i, f := range p.stream.files {
		if f.path == path {
			return i
		}
	}
	return -1
}

// stackSteer shows anchor a (the overview's selection) in the stream: its
// file unfolded and, once loaded, the cursor on its line.
func (m Model) stackSteer(sv *splitView, p *stackPane, a anchor) (Model, tea.Cmd) {
	i := sv.left.doc().ov.sel
	fi := p.fileIndex(a.target.Path)
	if fi < 0 {
		m.statusMsg = i18n.T("no file %s", a.target.Path)
		return m, nil
	}
	f := &p.stream.files[fi]
	if f.folded {
		f.folded = false
		p.rebuild(m, sv)
	}
	if f.loaded {
		return m.stackLand(sv, p, i), nil
	}
	p.park = &stackPark{anchor: i}
	p.d.p.cur = f.hdr
	rows, _ := sv.geomOf(m, p)
	p.d.p.ensureCursorVisible(rows)
	return m, p.pump(m)
}

// stepStreamAnchor is n/p in the stream: the next/previous overview anchor
// in stream order (file order, then line) across every file, wrapping; an
// anchor in a folded or unread file opens it and parks the landing.
func (m Model) stepStreamAnchor(sv *splitView, p *stackPane, dir int) (Model, tea.Cmd) {
	ov := sv.left.doc()
	type at struct{ i, line int }
	var order []at
	for fi, f := range p.stream.files {
		for i, a := range ov.ov.anchors {
			if a.plain || a.target.Note != "" || a.target.Path != f.path || a.target.Start <= 0 {
				continue
			}
			order = append(order, at{i, fi*1_000_000 + a.target.Start})
		}
	}
	if len(order) == 0 {
		return m, nil
	}
	sort.Slice(order, func(a, b int) bool { return order[a].line < order[b].line })
	cur := -1
	for k, o := range order {
		if ov.ov.anchors[o.i].dest == p.d.anchorCur {
			cur = k
		}
	}
	next := ((cur+dir)%len(order) + len(order)) % len(order)
	if cur < 0 && dir < 0 {
		next = len(order) - 1
	}
	ov.ov.sel = order[next].i
	wrapped := (dir > 0 && next <= cur) || (dir < 0 && next >= cur && cur >= 0)
	nm, cmd := m.stackSteer(sv, p, ov.ov.anchors[order[next].i])
	label := ov.ov.anchors[order[next].i].target.Label
	if label == "" {
		label = ov.ov.anchors[order[next].i].dest
	}
	if wrapped {
		nm.statusMsg = i18n.T("anchor %d/%d · %s · wrapped", next+1, len(order), label)
	} else {
		nm.statusMsg = i18n.T("anchor %d/%d · %s", next+1, len(order), label)
	}
	return nm, cmd
}

func (p *stackPane) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	d := p.d
	if nm, cmd, ok := m.previewSelectKey(msg); ok {
		return nm, cmd
	}
	if nm, cmd, ok := m.previewSearchKey(msg); ok {
		return nm, cmd
	}
	rows, _ := p.sv.geomOf(m, p)
	switch msg.String() {
	case "n":
		return m.stepStreamAnchor(p.sv, p, 1)
	case "p":
		return m.stepStreamAnchor(p.sv, p, -1)
	case "-":
		if fi := p.stream.fileAt(d.p.cur); fi >= 0 {
			p.stream.files[fi].folded = !p.stream.files[fi].folded
			p.rebuild(m, p.sv)
			d.p.cur = p.stream.files[fi].hdr
			d.p.ensureCursorVisible(rows)
			return m, p.pump(m)
		}
		return m, nil
	case "_":
		all := true
		for _, f := range p.stream.files {
			all = all && f.folded
		}
		for i := range p.stream.files {
			p.stream.files[i].folded = !all
		}
		p.rebuild(m, p.sv)
		return m, p.pump(m)
	case "N", "P":
		dir := 1
		if msg.String() == "P" {
			dir = -1
		}
		if h := p.stream.nextHeader(d.p.cur, dir); h >= 0 {
			d.p.cur = h
			d.p.ensureCursorVisible(rows)
		}
		return m, nil
	case "S":
		return m.toggleReviewStacked(p.sv)
	case "X", "ctrl+]":
		return m, nil // the stream is no open file: nothing to close or background
	}
	return p.docPane.update(m, msg)
}

// toggleReviewStacked is S on either pane: single ⇄ stacked, keeping the
// cursor's file and line.
func (m Model) toggleReviewStacked(sv *splitView) (Model, tea.Cmd) {
	ov := sv.left.doc()
	switch r := sv.right.(type) {
	case *stackPane:
		m = m.setReviewStackedPref(false)
		fi := r.stream.fileAt(r.d.p.cur)
		dp := &docPane{}
		sv.right = dp
		if fi < 0 || ov == nil {
			return m, nil
		}
		f := r.stream.files[fi]
		line := r.d.p.cur - f.body + 1
		if line < 1 || !f.loaded || f.folded {
			line = 0
		}
		return m.docSteer(sv, dp, ov, anchor{dest: r.d.anchorCur, target: agentdocsAnchorAt(f.path, line)})
	case *docPane:
		m = m.setReviewStackedPref(true)
		path, line, cur := "", 0, ""
		if r.d != nil {
			path, line, cur = r.d.path, r.d.p.cur+1, r.d.anchorCur
			r.d.backgrounded = true
		}
		p := m.newStackPane(sv, m.filesReview).(*stackPane)
		sv.right = p
		p.d.anchorCur = cur
		if fi := p.fileIndex(path); fi >= 0 {
			p.stream.files[fi].folded = false
			p.rebuild(m, sv)
			p.want.file, p.want.line, p.want.set = fi, line, true
		}
		return m, p.pump(m)
	}
	return m, nil
}
```

Define `agentdocsAnchorAt(path string, line int) agentdocs.Anchor` next to `anchorSource` in `split_overview.go` as `agentdocs.Anchor{Path: path, Start: line}` (field names per `grep -n 'type Anchor struct' -A12 internal/agentdocs/*.go`). `errPlaceholder(text)` is `errors.New(text)`. The pane file imports `context`, `errors`, `sort`, `strconv`, bubbletea and `i18n`.

In `internal/tui/split_doc_pane.go` `docPane.update`, add before `"X"`: `case "S": if sv := m.splitLayer(); sv != nil && m.topLayer() == sv { return m.toggleReviewStacked(sv) }` — S toggles from EITHER pane (the overview pane reaches it through its embedded docPane).

In `internal/tui/split_overview.go` `steerSplit`, add `case *stackPane: return m.stackSteer(sv, r, a)` and delete the `newStackPane` stub.

In `internal/tui/model.go` `Update`, next to the `fileContentMsg` case: `case stackPaneMsg: if m.staleSlot(msg.slotStamp) { return m, nil }; return m.stackPaneLanded(msg)` — check how other slot-stamped messages are gated (`grep -n 'slotStamp' internal/tui/slot_msg.go`) and use that exact guard.

i18n keys (four bundles): `"Files: %s"`, `"file %d/%d"` (exists — reuse), `"(folded)"`, `"[n/p] anchor  [N/P] file  [-] fold  [_] fold all  [S] single  [alt+↑↓] line  [spc] mark  [/] find"`, `"anchor %d/%d · %s"`, `"anchor %d/%d · %s · wrapped"`. In `docPane.hint` add `[S] stacked` after `[/] find` (edit that key in all four bundles).

- [ ] **Step 8: Run the pane tests, then the whole package**

Run: `go test ./internal/tui -run 'TestStack|TestFileStream|TestOverviewSplit|TestSplit' -count=1` then `go test ./internal/tui -count=1`
Expected: PASS. A failing `TestStackPaneBandWalkCrossesFiles` ordering means the stream order sort is wrong: `a.go:1-3` (start 1) sorts before `a.go:3`.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/split_stack.go internal/tui/split_stack_pane.go internal/tui/split_stack_test.go internal/tui/split_stack_pane_test.go internal/tui/split_overview.go internal/tui/split_doc_pane.go internal/tui/anchor_bands.go internal/tui/open_file.go internal/tui/open_files.go internal/tui/content_popup.go internal/tui/model.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml
git commit -m "tui: the stacked file pane — every review file in one lazy stream"
```

---

### Task 6: The mouse in a split

**Files:**
- Modify: `internal/tui/mouse.go:153-165` (the layer branch), `internal/tui/overview_keys.go:209-249` (`overviewClick` → geometry parameters)
- Test: `internal/tui/split_mouse_test.go`

**Interfaces:**
- Produces: `func (m Model) overviewClickIn(d *openFile, l layer, x, y, rows, innerW, margin int) (Model, tea.Cmd)`; `overviewClick(fv, x, y)` becomes a wrapper computing the viewer's geometry.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/split_mouse_test.go`:

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func click(t *testing.T, m Model, x, y int) Model {
	t.Helper()
	u, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	return drainCmds(t, u.(Model), cmd)
}

func TestSplitClickFocusesTheSideUnderThePointer(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	lw, _, _ := m.splitGeom()
	m = click(t, m, lw+5, 5)
	if sv.onLeft {
		t.Fatal("a click in the right box focuses it")
	}
	m = click(t, m, 5, 5)
	if !sv.onLeft {
		t.Fatal("a click in the left box focuses it")
	}
}

func TestSplitClickOnAnAnchorSelectsAndSteersDoubleOpens(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	ov := sv.left.doc()
	// Row 0 of the overview holds "Start at [A], then [B], …": find B's span.
	var bx, by int
	for _, s := range ov.ov.anchors[1].spans {
		bx, by = 2+s.from, 2+s.line // border + padding, border + title
		break
	}
	m = click(t, m, bx, by)
	if ov.ov.sel != 1 || sv.right.doc().path != "b.go" || !sv.onLeft {
		t.Fatalf("sel %d right %s onLeft %v", ov.ov.sel, sv.right.doc().path, sv.onLeft)
	}
	m = click(t, m, bx, by)
	if sv.onLeft {
		t.Fatal("a second click on the anchor opens it: focus right")
	}
}

func TestSplitWheelScrollsTheSideUnderThePointerWithoutFocus(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	lw, _, _ := m.splitGeom()
	u, _ := m.Update(tea.MouseMsg{X: lw + 5, Y: 5, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	m = u.(Model)
	if sv.right.doc().p.sel == 0 || !sv.onLeft {
		t.Fatalf("right sel %d onLeft %v", sv.right.doc().p.sel, sv.onLeft)
	}
}
```

Check how the mouse tests in `mouse_test.go` build a wheel message (`grep -n 'WheelDown' internal/tui/mouse_test.go | head -2`) and match that shape.

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/tui -run 'TestSplitClick|TestSplitWheel' -count=1`
Expected: FAIL — focus does not change / sel stays 0.

- [ ] **Step 3: Implement**

In `internal/tui/overview_keys.go`, rename `overviewClick`'s body into:

```go
// overviewClickIn is a left click at (x, y) in a box showing overview d
// whose content is rows×innerW starting at column 2+margin, row 2: on an
// anchor it selects it, a second click on the same anchor opens it.
func (m Model) overviewClickIn(d *openFile, l layer, x, y, rows, innerW, margin int) (Model, tea.Cmd) {
	row, col := y-2, x-2-margin
	if row < 0 || row >= rows || col < 0 {
		return m, nil
	}
	if d.p.mode == modeScroll {
		col += d.p.hscroll
	}
	line := d.p.clampTop(d.p.sel, rows) + row
	if line >= len(d.p.lines) {
		return m, nil
	}
	at, cw := -1, 0
	for k, r := range []rune(d.p.lines[line].text) {
		cw += lipgloss.Width(string(r))
		if cw > col {
			at = k
			break
		}
	}
	for i, a := range d.ov.anchors {
		for _, s := range a.spans {
			if s.line == line && at >= s.from && at < s.to {
				var double bool
				if m, double = m.registerClick(clickTarget{zone: zoneLayer, layer: l, row: i}); double {
					if sv, ok := l.(*splitView); ok {
						d.selectAnchorAt(i, line, rows)
						nm, cmd := m.steerSplit(sv)
						sv.onLeft = false
						return nm, cmd
					}
					return m.openAnchor(d, i)
				}
				d.selectAnchorAt(i, line, rows)
				if sv, ok := l.(*splitView); ok {
					return m.steerSplit(sv)
				}
				return m, nil
			}
		}
	}
	return m, nil
}

// overviewClick is a left click in the full-screen viewer showing overview fv.
func (m Model) overviewClick(fv *fileViewer, x, y int) (Model, tea.Cmd) {
	w, _ := m.overlayDims()
	rows, _ := m.viewerGeom()
	_, margin := readingColumn(w-4, m.readingWidth())
	return m.overviewClickIn(fv.openFile, fv, x, y, rows, w-4, margin)
}
```

In `internal/tui/mouse.go`, inside `if l := m.topLayer(); l != nil {`, before the `fileViewer` checks:

```go
		if sv, ok := l.(*splitView); ok {
			lw, _, _ := m.splitGeom()
			side := sv.right
			x := msg.X - lw
			if msg.X < lw {
				side, x = sv.left, msg.X
			}
			rows, inner := sv.geomOf(m, side)
			if wheel != 0 { // the wheel scrolls the side under the pointer, never the focus
				if d := side.doc(); d != nil {
					d.p.scrollBy(wheel, rows)
				}
				return m, nil
			}
			if msg.Button == tea.MouseButtonLeft && m.actionMenu == nil {
				sv.onLeft = side == sv.left
				if d := side.doc(); d != nil && d.ov != nil {
					return m.overviewClickIn(d, sv, x, msg.Y, rows, inner, 0)
				}
				if d := side.doc(); d != nil && msg.Y >= 2 && msg.Y < 2+rows {
					if line := d.p.clampTop(d.p.sel, rows) + msg.Y - 2; line < len(d.p.lines) {
						d.p.cur = line
					}
				}
				return m, nil
			}
		}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui -run 'TestSplitClick|TestSplitWheel|TestOverviewClick|TestMouse' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/mouse.go internal/tui/overview_keys.go internal/tui/split_mouse_test.go
git commit -m "tui: the mouse in a split — click focuses a side, anchors steer, the wheel scrolls under the pointer"
```

---

### Task 7: The e2e screens, docs, and the race gate

**Files:**
- Modify: `e2e/scenarios/tui_stored_overview.toml` + its `tui_stored_overview.screens/` goldens (regenerated)
- Create: `e2e/scenarios/tui_stored_overview_narrow.toml` + goldens
- Modify: `CHANGELOG.md` (top entry), `README.md` (the review section — find it with `grep -n -i 'overview' README.md`), `internal/agentskill/gg-overview.md:46-49`, `internal/agentskill/agentskill.go:38` (`Version` +1), `docs/CLAUDE-details.md` (a "Split layer" paragraph under the TUI section)

- [ ] **Step 1: Rewrite the stored-overview scenario**

Replace the `[tui]` part of `e2e/scenarios/tui_stored_overview.toml` (keep `[input]`, `[[run]]`, `[expect]`); the review document gains a second file so stacking shows two headers — change the input and the stdin to:

```toml
[input]
steps = [
  { write = "a.txt", content = "one\ntwo\nthree\nfour\nfive\n" },
  { write = "b.txt", content = "alpha\nbeta\ngamma\n" },
  { commit = "add a and b" },
]

[[run]]
cmd   = ["review", "save", "gg://{{cwd}}@{{rev:HEAD}}", "--agent", "Claude Code", "--stdin"]
stdin = '{"version":1,"summary":"## Summary\nfine","overview":"Start at [line two](a.txt:2), then [beta](b.txt:2), then [outside](zzz.go:1).","files":[{"path":"a.txt","annotations":[{"newRange":[2,2],"summary":"why two?"}]},{"path":"b.txt","annotations":[{"newRange":[2,2],"summary":"beta?"}]}]}'
exit  = 0

[tui]
size = "160x40"

[[tui.step]]
name = "files"
keys = ["right", "enter"]
wait = true
screen_contains = ["Reviews", "Claude Code"]

[[tui.step]]
name = "review"
keys = ["down", "enter"]
wait = true
screen_contains = ["≡ Summary", "≡ Overview", "a.txt", "b.txt"]

[[tui.step]]
name = "split"
keys = ["down", "enter"]
wait = true
screen_contains = ["Overview: ", "line two", "View a.txt @", "two", "[↑↓] anchor"]

[[tui.step]]
name = "steer"
keys = ["down"]
wait = true
screen_contains = ["View b.txt @", "beta"]

[[tui.step]]
name = "focus-right"
keys = ["enter"]
screen_contains = ["[n/p] anchors"]

[[tui.step]]
name = "back-left"
keys = ["bspace"]
screen_contains = ["[↑↓] anchor"]

[[tui.step]]
name = "stacked"
keys = ["S"]
wait = true
screen_contains = ["━━ a.txt", "━━ b.txt", "Files: ", "file 2/2"]

[[tui.step]]
name = "close"
keys = ["esc"]
screen_contains = ["≡ Overview"]
```

Update the scenario's `name` to `"tui: a review's stored overview opens as a split — the overview steers the file pane, S stacks every file, esc returns to the ≡ Overview row"` and its comment accordingly.

- [ ] **Step 2: Write the narrow scenario**

Create `e2e/scenarios/tui_stored_overview_narrow.toml` with the same `[input]`/`[[run]]`/`[expect]` and:

```toml
[tui]
size = "58x30"

[[tui.step]]
name = "files"
keys = ["right", "enter"]
wait = true
screen_contains = ["Reviews"]

[[tui.step]]
name = "review"
keys = ["down", "enter"]
wait = true
screen_contains = ["≡ Overview"]

[[tui.step]]
name = "viewer"
keys = ["down", "enter"]
screen_contains = ["Overview: ", "line two"]

[[tui.step]]
name = "anchor"
keys = ["enter"]
wait = true
screen_contains = ["View a.txt @", "┃"]

[[tui.step]]
name = "back"
keys = ["bspace"]
screen_contains = ["Overview: "]
```

- [ ] **Step 3: Generate and inspect the goldens**

Run: `go test ./e2e -run 'TestScenarios/tui_stored_overview' -count=1 -update` then `cat e2e/scenarios/tui_stored_overview.screens/*.txt | less` — read every screen: two boxes side by side, one lit border, the hint line, the stack headers. Then `go test ./e2e -run 'TestScenarios/tui_stored_overview' -count=1` (no `-update`).
Expected: PASS, and the screens look right (if a screen is wrong, fix the code, not the golden).

- [ ] **Step 4: Docs**

`CHANGELOG.md`: a new top section:

```markdown
## The review overview beside its files

### Added
- Enter on a review's "≡ Overview" row now opens a SPLIT: the overview on
  the left, the file it points at on the right. ↑/↓ on the overview step
  anchor to anchor and the right side follows; enter focuses the file;
  tab / shift+tab / alt+←/→ switch sides, backspace focuses the overview,
  esc closes the split back to the review tree. The file pane has every
  viewer key (cursor, selection, search, anchor bands with n/p, `.` menu).
- `S` in the split stacks every file of the review into one lazy stream
  (headers with the note count, `-`/`_` fold, N/P step files, n/p walk the
  anchors across files); the choice is remembered on its own, apart from
  the diff view's stacked mode.
- Below 60 columns the full-screen overview viewer opens as before.

### Internal
- `internal/tui/split_view.go`: a generic two-pane full-screen layer
  (`splitView` + the `pane` interface); `docPane` wraps an open file in the
  preview box; the four "focused document" seams answer through
  `splitFocus`. The diff view is untouched.
```

`README.md`: in the paragraph describing reviews' overview (find with `grep -n -i 'overview' README.md`), replace the sentence about the overview opening in the viewer with: "Enter on ≡ Overview opens the overview beside the file it points at; ↑/↓ walk the anchors and the file pane follows, enter focuses it, `S` stacks every reviewed file, esc returns to the tree."

`internal/agentskill/gg-overview.md` lines 46-49: after "tab walks the anchors top to bottom:" add the parenthetical "(in a review's split view ↑/↓ walk them and the file shows beside the overview; tab switches sides)". Bump `agentskill.Version` by one in `internal/agentskill/agentskill.go` and run `go test ./internal/agentskill -count=1` (a golden version guard may ask for an `-update`; follow its message).

`docs/CLAUDE-details.md`: under the TUI section add a paragraph "Split layer (`split_view.go`)": the `pane` contract, the split's own keys, that `splitFocus` is the one seam, that a document taken into another frame pops the split, and the stacked stream's rule (every (file, line) through `fileStream`, never a bare line number).

- [ ] **Step 5: The full gate**

Run: `./test.sh race 2>&1 | tee /tmp/claude-1000/-work-gigagit/49de9605-880e-4785-8c17-48d4c834a409/scratchpad/race.log; grep -n -E 'all green|FAIL' /tmp/claude-1000/-work-gigagit/49de9605-880e-4785-8c17-48d4c834a409/scratchpad/race.log`
Expected: "all green" and no FAIL lines. Also `go build ./cmd/gg && go build -o bin/gg ./cmd/gg` for the verify binary at `/work/gigagit/.claude/worktrees/review-overview-split/bin/gg`.

- [ ] **Step 6: Commit**

```bash
git add e2e/scenarios/tui_stored_overview.toml e2e/scenarios/tui_stored_overview.screens e2e/scenarios/tui_stored_overview_narrow.toml e2e/scenarios/tui_stored_overview_narrow.screens CHANGELOG.md README.md internal/agentskill/gg-overview.md internal/agentskill/agentskill.go docs/CLAUDE-details.md
git commit -m "docs+e2e: the review overview split — golden screens, changelog, skill text"
```

Then a read-only whole-branch review by a fresh subagent on the most capable model (CLAUDE.md allows review subagents), fixes committed here, and hand the branch to the user for the merge (never merge or push unasked).
