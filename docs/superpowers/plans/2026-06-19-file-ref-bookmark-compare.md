# File-reference bookmark + compare-against-bookmark Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Compare against bookmark" action to the `.` menu on every file reference, pick a bookmark and diff the focused file against it; and make "Bookmark this file" target the right version in history (per-row) and the diff view.

**Architecture:** Pure TUI layer over existing domain reads. `focusedBookmark()` stays the single per-context file resolver; a new `bookmarkToFileRef` maps its address to a `model.FileRef`. The focused (left) side resolves via `domain.ResolveBytes` (by address, no blob SHA); the picked-bookmark (right) side via `domain.BookmarkBytes`; both feed the existing `Differ`. The global bookmark popup gains a "compare mode" carried across the async load on a Model field.

**Tech Stack:** Go 1.26, Bubble Tea (Elm-style value-receiver `Model`), `internal/tui`, `internal/domain`, `internal/model`.

## Global Constraints

- A git verb is one invocation; frontends never import `internal/git` — reach git through `internal/domain` (archtest-guarded). This feature adds NO git verb and NO engine op — all reads exist (`ResolveBytes`, `BookmarkBytes`, `Differ`).
- TUI `Model` is a value receiver with pointer fields for state that must persist across the value copy.
- Tests use `footerModel()` (FakeRunner-backed) or a real `git` temp repo. Follow TDD.
- `main` is the trunk; work happens in the `bookmark-global-key` worktree (already created).
- Run `./test.sh race` before declaring done.

---

### Task 1: History per-row + diff-view in `focusedBookmark`

Make "Bookmark this file" target the selected history row's version and the diff view's NEW-side file. Align the history copy-rows to the selected row.

**Files:**
- Modify: `internal/tui/bookmark.go` (`focusedBookmark`, history + diff-view cases)
- Modify: `internal/tui/action_menu.go` (`contextCopyRows`, history case ~line 102-107)
- Test: `internal/tui/bookmark_test.go`

**Interfaces:**
- Consumes: `model.Bookmark{State, Commit, Path, Worktree, Branch}`, `model.FileCommit{Hash, Path}`, `historyView{commits []model.FileCommit, sel int, ctx navContext}`, `diffView{title, rev string}`.
- Produces: `focusedBookmark()` now resolves history (selected row) and diff view.

- [ ] **Step 1: Write the failing tests**

```go
// internal/tui/bookmark_test.go  (add)
func TestFocusedBookmarkHistoryUsesSelectedRow(t *testing.T) {
	m := footerModel()
	h := newHistoryView(navContext{path: "old.go", rev: "starthash"})
	h.commits = []model.FileCommit{
		{Hash: "aaaa1111", Path: "old.go"},
		{Hash: "bbbb2222", Path: "renamed.go"},
	}
	h.sel = 1
	m = m.pushSurface(h)
	b, ok := m.focusedBookmark()
	if !ok || b.State != model.StateCommitted || b.Commit != "bbbb2222" || b.Path != "renamed.go" {
		t.Fatalf("history focusedBookmark = %+v ok=%v; want committed bbbb2222 renamed.go", b, ok)
	}
}

func TestFocusedBookmarkDiffViewCommit(t *testing.T) {
	m := footerModel()
	m.diffView = &diffView{title: "dir/a.go", rev: "cafe9999"}
	b, ok := m.focusedBookmark()
	if !ok || b.State != model.StateCommitted || b.Commit != "cafe9999" || b.Path != "dir/a.go" {
		t.Fatalf("diff focusedBookmark = %+v ok=%v; want committed cafe9999 dir/a.go", b, ok)
	}
}

func TestFocusedBookmarkDiffViewWorkingTree(t *testing.T) {
	m := footerModel()
	m.diffView = &diffView{title: "a.go", rev: ""} // working-tree diff
	b, ok := m.focusedBookmark()
	if !ok || b.State != model.StateUnstaged || b.Path != "a.go" {
		t.Fatalf("working-tree diff focusedBookmark = %+v ok=%v; want unstaged a.go", b, ok)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run TestFocusedBookmark`
Expected: FAIL — history uses `ctx.rev` ("starthash"); diff view returns `ok=false`.

- [ ] **Step 3: Implement**

In `internal/tui/bookmark.go`, replace the history case and the diff-view early-return:

```go
	case *historyView:
		if s.sel < 0 || s.sel >= len(s.commits) {
			return model.Bookmark{}, false
		}
		fc := s.commits[s.sel]
		return model.Bookmark{State: model.StateCommitted, Commit: fc.Hash, Path: fc.Path}, true
```

```go
	if v := m.diffView; v != nil {
		if v.title == "" {
			return model.Bookmark{}, false
		}
		if v.rev != "" {
			return model.Bookmark{State: model.StateCommitted, Commit: v.rev, Path: v.title}, true
		}
		return model.Bookmark{State: model.StateUnstaged, Worktree: m.currentWorktree, Branch: m.status.Branch, Path: v.title}, true
	}
```

(The `blameView` case is unchanged — blame has no per-row file version, it stays `ctx.rev`/`ctx.path`.)

In `internal/tui/action_menu.go` `contextCopyRows`, align the history case:

```go
	case *historyView:
		if s.sel >= 0 && s.sel < len(s.commits) {
			fc := s.commits[s.sel]
			return m.fileCopyRows(fc.Path, fc.Hash)
		}
		return m.fileCopyRows(s.ctx.path, s.ctx.rev)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestFocusedBookmark|TestContextCopyRows'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/bookmark.go internal/tui/action_menu.go internal/tui/bookmark_test.go
git commit -m "feat(tui): bookmark history per-row version + diff-view file"
```

---

### Task 2: `bookmarkToFileRef` mapper

Map a bookmark's address to a `model.FileRef` for address-based resolution of the focused (left) compare side.

**Files:**
- Modify: `internal/tui/bookmark.go` (add `bookmarkToFileRef`)
- Test: `internal/tui/bookmark_test.go`

**Interfaces:**
- Consumes: `model.Bookmark`, `model.FileRef{Source, Locator, Path}`, `model.FileSource` consts (`SourceUnstaged`, `SourceStaged`, `SourceCommit`, `SourceShelf`).
- Produces: `func bookmarkToFileRef(b model.Bookmark) model.FileRef`.

- [ ] **Step 1: Write the failing test**

```go
func TestBookmarkToFileRef(t *testing.T) {
	cases := []struct {
		name string
		in   model.Bookmark
		want model.FileRef
	}{
		{"committed", model.Bookmark{State: model.StateCommitted, Commit: "deadbeef", Path: "a.go"},
			model.FileRef{Source: model.SourceCommit, Locator: "deadbeef", Path: "a.go"}},
		{"shelf", model.Bookmark{State: model.StateShelf, ShelfID: "sh1", Path: "b.go"},
			model.FileRef{Source: model.SourceShelf, Locator: "sh1", Path: "b.go"}},
		{"staged", model.Bookmark{State: model.StateStaged, Path: "c.go"},
			model.FileRef{Source: model.SourceStaged, Path: "c.go"}},
		{"unstaged", model.Bookmark{State: model.StateUnstaged, Path: "d.go"},
			model.FileRef{Source: model.SourceUnstaged, Path: "d.go"}},
		{"untracked", model.Bookmark{State: model.StateUntracked, Path: "e.go"},
			model.FileRef{Source: model.SourceUnstaged, Path: "e.go"}},
	}
	for _, c := range cases {
		if got := bookmarkToFileRef(c.in); got != c.want {
			t.Errorf("%s: bookmarkToFileRef = %+v, want %+v", c.name, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestBookmarkToFileRef`
Expected: FAIL — `bookmarkToFileRef` undefined.

- [ ] **Step 3: Implement**

In `internal/tui/bookmark.go`:

```go
// bookmarkToFileRef maps a bookmark's address to a FileRef so the focused
// (left) compare side resolves via domain.ResolveBytes — by address, with no
// pre-resolved blob SHA. A committed bookmark's Commit may be a stash commit.
func bookmarkToFileRef(b model.Bookmark) model.FileRef {
	switch b.State {
	case model.StateShelf:
		return model.FileRef{Source: model.SourceShelf, Locator: b.ShelfID, Path: b.Path}
	case model.StateStaged:
		return model.FileRef{Source: model.SourceStaged, Path: b.Path}
	case model.StateCommitted:
		return model.FileRef{Source: model.SourceCommit, Locator: b.Commit, Path: b.Path}
	default: // unstaged / untracked
		return model.FileRef{Source: model.SourceUnstaged, Path: b.Path}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestBookmarkToFileRef`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/bookmark.go internal/tui/bookmark_test.go
git commit -m "feat(tui): bookmarkToFileRef address mapper"
```

---

### Task 3: Compare command + opener (focused vs picked bookmark)

The diff command and the `diffView` opener that drive the comparison.

**Files:**
- Create: `internal/tui/bookmark_compare.go`
- Test: `internal/tui/bookmark_compare_test.go`

**Interfaces:**
- Consumes: `bookmarkToFileRef` (Task 2); `m.svc` (`*domain.Service`) with `ResolveBytes(ctx, model.FileRef)` + `BookmarkBytes(ctx, model.Bookmark)`; `m.diffDiffer() domain.Differ`; `domain.Request{Key, Old, New}`; `applyDiff(v *diffView, out domain.Diff, body int)`; `diffMsg{tag string, view *diffView}`; `bookmarkDisplay(model.Bookmark) string`; `m.overlayDims()`, `m.diffBodyRows()`, `m.diffPartial`, `m.diffLong`.
- Produces:
  - `func (m Model) openCompareFocusedVsBookmark(ref model.FileRef, label string, bm model.Bookmark) (Model, tea.Cmd)`
  - `func (m Model) loadCompareFocusedVsBookmarkCmd(ref model.FileRef, label string, bm model.Bookmark) tea.Cmd`

- [ ] **Step 1: Write the failing test**

```go
// internal/tui/bookmark_compare_test.go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestOpenCompareFocusedVsBookmark(t *testing.T) {
	m := footerModel()
	ref := model.FileRef{Source: model.SourceCommit, Locator: "aaaa1111", Path: "a.go"}
	bm := model.Bookmark{ID: "bm9", State: model.StateCommitted, Commit: "bbbb2222", SHA: "blob22", Path: "b.go"}
	mm, cmd := m.openCompareFocusedVsBookmark(ref, "commit a.go", bm)
	u := mm.(Model)
	if u.diffView == nil {
		t.Fatal("openCompareFocusedVsBookmark must open a diff view")
	}
	if u.diffView.title != "a.go ↔ b.go" {
		t.Errorf("diff title = %q, want \"a.go ↔ b.go\"", u.diffView.title)
	}
	if u.diffTag != "cmpbm:a.go:bm9" {
		t.Errorf("diffTag = %q, want cmpbm:a.go:bm9", u.diffTag)
	}
	if cmd == nil {
		t.Error("expected a load command")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestOpenCompareFocusedVsBookmark`
Expected: FAIL — `openCompareFocusedVsBookmark` undefined.

- [ ] **Step 3: Implement**

```go
// internal/tui/bookmark_compare.go
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/model"
)

// openCompareFocusedVsBookmark diffs the focused file (ref, left/old) against a
// picked bookmark (bm, right/new) in the full-screen diff view.
func (m Model) openCompareFocusedVsBookmark(ref model.FileRef, label string, bm model.Bookmark) (Model, tea.Cmd) {
	m.bookmarkPopup = nil
	width, _ := m.overlayDims()
	m.diffView = &diffView{
		title:   ref.Path + " ↔ " + bm.Path,
		context: label + " → " + bookmarkDisplay(bm),
		loading: true,
		partial: m.diffPartial,
		long:    m.diffLong,
		width:   width,
	}
	m.diffTag = "cmpbm:" + ref.Path + ":" + bm.ID
	return m, m.loadCompareFocusedVsBookmarkCmd(ref, label, bm)
}

// loadCompareFocusedVsBookmarkCmd resolves the focused side by address
// (ResolveBytes) and the bookmark side via BookmarkBytes, then runs the Differ.
func (m Model) loadCompareFocusedVsBookmarkCmd(ref model.FileRef, label string, bm model.Bookmark) tea.Cmd {
	svc := m.svc
	differ := m.diffDiffer()
	body := m.diffBodyRows()
	tag := "cmpbm:" + ref.Path + ":" + bm.ID
	v := &diffView{
		title:   ref.Path + " ↔ " + bm.Path,
		context: label + " → " + bookmarkDisplay(bm),
		partial: m.diffPartial,
		long:    m.diffLong,
	}
	v.width, _ = m.overlayDims()
	return func() tea.Msg {
		oldSrc := func(ctx context.Context) ([]byte, error) { return svc.ResolveBytes(ctx, ref) }
		newSrc := func(ctx context.Context) ([]byte, error) { return svc.BookmarkBytes(ctx, bm) }
		out, err := differ.Diff(context.Background(), domain.Request{Key: "", Old: oldSrc, New: newSrc})
		if err != nil {
			v.err = err
			return diffMsg{tag: tag, view: v}
		}
		applyDiff(v, out, body)
		return diffMsg{tag: tag, view: v}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestOpenCompareFocusedVsBookmark`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/bookmark_compare.go internal/tui/bookmark_compare_test.go
git commit -m "feat(tui): focused-vs-bookmark compare command"
```

---

### Task 4: Compare-mode picker wiring

Carry the compare target across the async load on the Model, put the popup in compare mode, branch `enter` to compare, make `m`/`p`/`x` inert, and retitle the header.

**Files:**
- Modify: `internal/tui/model.go` (add `pendingCompare` field; `bookmarksLoadedMsg` handler ~line 257-263)
- Modify: `internal/tui/bookmark_popup.go` (struct fields; `updateBookmarkPopupKey` enter + letter cases; `renderBookmarkPopup` header)
- Modify: `internal/tui/bookmark_compare.go` (add `pendingCompare` type)
- Test: `internal/tui/bookmark_compare_test.go`

**Interfaces:**
- Consumes: `openCompareFocusedVsBookmark` (Task 3), `m.selectedBookmark()`, `newBookmarkPopup`.
- Produces: `type pendingCompare struct { ref model.FileRef; label string }`; `Model.pendingCompare *pendingCompare`; `bookmarkPopup.compareRef *model.FileRef`; `bookmarkPopup.compareLabel string`.

- [ ] **Step 1: Write the failing tests**

```go
func twoBookmarkItems() []model.Bookmark {
	return []model.Bookmark{
		{ID: "b1", State: model.StateCommitted, Commit: "c1", SHA: "s1", Path: "a.go"},
		{ID: "b2", State: model.StateCommitted, Commit: "c2", SHA: "s2", Path: "b.go"},
	}
}

func TestPendingCompareSurvivesLoad(t *testing.T) {
	m := footerModel()
	m.pendingCompare = &pendingCompare{ref: model.FileRef{Source: model.SourceCommit, Locator: "x", Path: "a.go"}, label: "commit a.go"}
	u, _ := m.Update(bookmarksLoadedMsg{items: twoBookmarkItems()})
	mm := u.(Model)
	if mm.bookmarkPopup == nil || mm.bookmarkPopup.compareRef == nil {
		t.Fatal("popup must open in compare mode")
	}
	if mm.pendingCompare != nil {
		t.Error("pendingCompare must be cleared once consumed")
	}
}

func TestCompareModeEnterRunsCompare(t *testing.T) {
	m := footerModel()
	m.bookmarkPopup = newBookmarkPopup(twoBookmarkItems())
	ref := model.FileRef{Source: model.SourceCommit, Locator: "x", Path: "a.go"}
	m.bookmarkPopup.compareRef = &ref
	m.bookmarkPopup.compareLabel = "commit a.go"
	u, _ := m.Update(keyMsg("enter"))
	mm := u.(Model)
	if mm.diffView == nil {
		t.Fatal("enter in compare mode must open the comparison diff")
	}
	if mm.bookmarkPopup != nil {
		t.Error("popup should close after launching the compare")
	}
}

func TestCompareModeMutatorsInert(t *testing.T) {
	m := footerModel()
	m.bookmarkPopup = newBookmarkPopup(twoBookmarkItems())
	ref := model.FileRef{Source: model.SourceCommit, Locator: "x", Path: "a.go"}
	m.bookmarkPopup.compareRef = &ref
	for _, k := range []string{"x", "p", "m"} {
		u, _ := m.Update(keyMsg(k))
		mm := u.(Model)
		if mm.bookmarkPopup == nil || mm.diffView != nil || mm.modal != nil || mm.bookmarkPastePopup != nil {
			t.Errorf("%q must be inert in compare mode", k)
		}
		if mm.bookmarkPopup != nil && mm.bookmarkPopup.markID != "" {
			t.Errorf("%q must not set a compare mark in compare mode", k)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestPendingCompare|TestCompareMode'`
Expected: FAIL — `pendingCompare`/`compareRef` undefined.

- [ ] **Step 3: Implement**

In `internal/tui/bookmark_compare.go` add:

```go
// pendingCompare carries the focused file (frozen at menu time) across the
// async bookmark load, so the popup it produces opens in compare mode. It lives
// on the Model, never on the not-yet-built popup (mirrors the reword-prefill fix).
type pendingCompare struct {
	ref   model.FileRef
	label string
}
```

In `internal/tui/model.go`, add the field to the Model struct (near `bookmarkPopup`):

```go
	pendingCompare *pendingCompare
```

And update the `bookmarksLoadedMsg` handler:

```go
	case bookmarksLoadedMsg:
		if msg.err != nil {
			m.statusMsg = "bookmarks: " + msg.err.Error()
			return m, nil
		}
		m.bookmarkPopup = newBookmarkPopup(msg.items)
		if pc := m.pendingCompare; pc != nil {
			m.bookmarkPopup.compareRef = &pc.ref
			m.bookmarkPopup.compareLabel = pc.label
			m.pendingCompare = nil
		}
		return m, nil
```

In `internal/tui/bookmark_popup.go`, add to the `bookmarkPopup` struct:

```go
	compareRef   *model.FileRef // non-nil → compare mode (enter diffs against the highlighted bookmark)
	compareLabel string         // human label for the focused side, shown in the header
```

In `updateBookmarkPopupKey`, change the `enter` case:

```go
	case tea.KeyEnter:
		if p.compareRef != nil {
			b, ok := m.selectedBookmark()
			if !ok {
				return m, nil
			}
			return m.openCompareFocusedVsBookmark(*p.compareRef, p.compareLabel, b)
		}
		return m.bookmarkJump()
```

Guard the three mutators in the `tea.KeyRunes` letter switch (each `case "x"`, `case "p"`, `case "m"` gets this as its first line):

```go
		case "x":
			if p.compareRef != nil {
				return m, nil
			}
			return m.bookmarkRemovePrompt()
		case "p":
			if p.compareRef != nil {
				return m, nil
			}
			return m.bookmarkPastePrompt()
		case "m":
			if p.compareRef != nil {
				return m, nil
			}
			return m.bookmarkMark()
```

In `renderBookmarkPopup`, change the header line:

```go
	header := "Bookmarks"
	if p.compareRef != nil {
		header = "Compare " + p.compareRef.Path + " against:"
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestPendingCompare|TestCompareMode'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/bookmark_popup.go internal/tui/bookmark_compare.go internal/tui/bookmark_compare_test.go
git commit -m "feat(tui): bookmark popup compare mode"
```

---

### Task 5: "Compare against bookmark" menu row

The menu-only action that captures the focused file and opens the picker in compare mode, wired into `availableActions` beside "Bookmark this file".

**Files:**
- Modify: `internal/tui/bookmark.go` (add `compareAgainstBookmarkRow`)
- Modify: `internal/tui/action_menu.go` (`availableActions`, both the content-window branch ~line 33-41 and the panel branch ~line 64-66)
- Test: `internal/tui/bookmark_compare_test.go`

**Interfaces:**
- Consumes: `focusedBookmark()`, `bookmarkToFileRef` (Task 2), `bookmarkDisplay`, `loadBookmarksCmd`, `pendingCompare` (Task 4).
- Produces: `func (m Model) compareAgainstBookmarkRow() (actionRow, bool)` with id `"bookmark-compare"`.

- [ ] **Step 1: Write the failing tests**

```go
func TestCompareRowRunSetsPendingAndLoads(t *testing.T) {
	m := footerModel()
	m.diffView = &diffView{title: "a.go", rev: "cafe9999"} // a resolvable focused file
	row, ok := m.compareAgainstBookmarkRow()
	if !ok {
		t.Fatal("compare row must be present when a file is focused")
	}
	u, cmd := row.run(m)
	mm := u.(Model)
	if mm.pendingCompare == nil || mm.pendingCompare.ref.Path != "a.go" {
		t.Fatalf("run must set pendingCompare for the focused file, got %+v", mm.pendingCompare)
	}
	if cmd == nil {
		t.Error("run must kick off the bookmark load")
	}
}

func TestCompareRowAccompaniesAddRow(t *testing.T) {
	// Wherever "Bookmark this file" appears, so must "Compare against bookmark".
	m := footerModel()
	m = m.pushSurface(newBlameView(navContext{path: "a.go", rev: "abc123"}))
	got := ids(availableActions(m))
	if !got["bookmark-add"] {
		t.Fatal("precondition: bookmark-add expected in blame view")
	}
	if !got["bookmark-compare"] {
		t.Error("bookmark-compare must accompany bookmark-add")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestCompareRow'`
Expected: FAIL — `compareAgainstBookmarkRow` undefined.

- [ ] **Step 3: Implement**

In `internal/tui/bookmark.go`:

```go
// compareAgainstBookmarkRow is the menu-only "Compare against bookmark" action,
// present wherever a single file is focused. The focused file is frozen at build
// time; running it stashes that ref on the Model and opens the bookmark picker
// in compare mode.
func (m Model) compareAgainstBookmarkRow() (actionRow, bool) {
	b, ok := m.focusedBookmark()
	if !ok {
		return actionRow{}, false
	}
	ref := bookmarkToFileRef(b)
	label := bookmarkDisplay(b)
	return actionRow{
		id:    "bookmark-compare",
		label: "Compare against bookmark",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.pendingCompare = &pendingCompare{ref: ref, label: label}
			return m, m.loadBookmarksCmd()
		},
	}, true
}
```

In `internal/tui/action_menu.go`, append the row right after each `bookmarkAddRow` block. The content-window branch:

```go
		if r, ok := m.bookmarkAddRow(); ok {
			rows = append(rows, r)
		}
		if r, ok := m.compareAgainstBookmarkRow(); ok {
			rows = append(rows, r)
		}
		return rows
```

The panel branch:

```go
	if r, ok := m.bookmarkAddRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.compareAgainstBookmarkRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestCompareRow'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/bookmark.go internal/tui/action_menu.go internal/tui/bookmark_compare_test.go
git commit -m "feat(tui): Compare against bookmark menu action"
```

---

### Task 6: Help text, docs, and full gate

Advertise the new action and document it; run the full race gate.

**Files:**
- Modify: `internal/tui/help.go` (the bookmark/`g` help line)
- Modify: `CHANGELOG.md`
- Modify: `README.md` (the `g` / bookmark row in the keybindings table)

**Interfaces:** none (docs + help only).

- [ ] **Step 1: Update help.go**

In `internal/tui/help.go`, extend the bookmark help line (the `r("g", ...)` entry) to mention the menu action, e.g. append:
`" The . menu on any file offers Bookmark this file and Compare against bookmark (pick a bookmark, then diff the focused file against it)."`

- [ ] **Step 2: Update CHANGELOG.md**

Under `## [Unreleased]` → `### Added`, add:

```markdown
- TUI: **Compare against bookmark** — the `.` menu on any file reference (file
  tree, history row, blame, diff, stash files, the Files/Staged/Shelf panels)
  offers **Bookmark this file** and **Compare against bookmark**: pick a bookmark
  from the switcher and the focused file is diffed against it. Bookmarking in the
  history view now targets the **selected commit's** version of the file.
```

- [ ] **Step 3: Update README.md**

In the keybindings table, extend the `g` / bookmark row (or add a sentence near it) noting the `.`-menu **Compare against bookmark** action and per-row history bookmarking. Keep it to one line.

- [ ] **Step 4: Run the full race gate**

Run: `gofmt -l internal/tui/ && go vet ./internal/tui/ && ./test.sh race`
Expected: gofmt prints nothing; vet clean; `all green`.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/help.go CHANGELOG.md README.md
git commit -m "docs(tui): document Compare against bookmark"
```

---

## Notes for the implementer

- `keyMsg`, `footerModel`, `ids`, `newBlameView`, `newHistoryView`, `pushSurface` are existing test helpers in `internal/tui`.
- The `diffMsg` handler already matches `msg.tag == m.diffTag` and swaps in `msg.view`; the `cmpbm:` tag follows the same pattern as the existing `bookmark2:` compare, so no new message plumbing is needed.
- Do NOT mutate `domain.BookmarkBytes`; the asymmetric resolution (left via `ResolveBytes`, right via `BookmarkBytes`) is deliberate.
- No engine op, no git verb, no CLI, no `agentskill` bump — this is TUI-only.
