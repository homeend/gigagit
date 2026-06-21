# Commit Bookmarks + Compare-Against-Bookmarked-Commit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user bookmark a *commit* (a path-less pointer that persists in the `g` switcher) and, with `enter` on it, whole-tree-compare it (base) against the currently-selected Commits-panel commit (subject).

**Architecture:** A commit bookmark is a path-less `StateCommitted` `model.Bookmark` stored in the existing `bookmarks.toml` registry and rendered in the existing `g` switcher. Creation is a Commits-panel `.` menu action; comparison reuses the existing `Endpoint`/`domain.CompareFiles`/`openCompareFiles` machinery. No new store, type, or popup.

**Tech Stack:** Go 1.26, Bubble Tea TUI (value-receiver `Model`), `internal/model`, `internal/bookmark`, `internal/domain`, `internal/tui`.

## Global Constraints

- A commit bookmark is identified by `b.Path == "" && b.State == StateCommitted` (`Bookmark.IsCommit()`), `Commit` = full sha, `SHA` = **empty** (do NOT set `SHA = Commit`).
- Compare direction is fixed: **bookmarked = left/base, selected = right/subject** (additions read as present in the selected commit, not the bookmark). Do not order by ancestry/feed.
- `internal/tui` and `internal/cli` never import `internal/git`; reach git via `internal/domain` (archtest-guarded).
- TUI `Model` is a value receiver; mutating helpers return `Model`.
- Tests use a real `git` in `t.TempDir()` (`newRepo`/`loadedModelLinearCommits`) or `gitexec.FakeRunner` for argv assertions. Follow TDD: write the failing test, see it fail, implement, see it pass, commit.
- Run `go test ./internal/tui/ ./internal/model/ ./internal/domain/ ./internal/bookmark/` after each task; `./test.sh race` before finishing.

---

## Stage 1 — model, storage, create, safe switcher

### Task 1: `Bookmark.IsCommit()` + path-less `FileAddress.Display()`

**Files:**
- Modify: `internal/model/model.go` (`FileAddress.Display`, add `Bookmark.IsCommit`)
- Test: `internal/model/fileaddress_test.go`

**Interfaces:**
- Produces: `func (b model.Bookmark) IsCommit() bool`; `FileAddress.Display()` returns `"<container> / <mid>"` (no trailing `/path`) when `Path == ""`.

- [ ] **Step 1: Write the failing tests** — add to `internal/model/fileaddress_test.go`:

```go
func TestFileAddressDisplayCommitPointer(t *testing.T) {
	cases := []struct {
		name string
		a    FileAddress
		want string
	}{
		{"branch", FileAddress{State: StateCommitted, Branch: "feat", Commit: "a1b2c3d4e5", Path: ""}, "feat / a1b2c3d"},
		{"no-branch", FileAddress{State: StateCommitted, Commit: "a1b2c3d4e5", Path: ""}, "commit / a1b2c3d"},
		{"file-unchanged", FileAddress{State: StateCommitted, Branch: "feat", Commit: "a1b2c3d4e5", Path: "src/x.go"}, "feat / a1b2c3d / src/x.go"},
	}
	for _, c := range cases {
		if got := c.a.Display(); got != c.want {
			t.Errorf("%s: Display()=%q want %q", c.name, got, c.want)
		}
	}
}

func TestBookmarkIsCommit(t *testing.T) {
	if !(Bookmark{State: StateCommitted, Commit: "abc", Path: ""}).IsCommit() {
		t.Error("path-less committed bookmark should be a commit bookmark")
	}
	if (Bookmark{State: StateCommitted, Commit: "abc", Path: "x.go"}).IsCommit() {
		t.Error("a committed FILE bookmark is not a commit bookmark")
	}
	if (Bookmark{State: StateUnstaged, Worktree: "/wt", Path: ""}).IsCommit() {
		t.Error("a non-committed state is not a commit bookmark")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/model/ -run 'TestFileAddressDisplayCommitPointer|TestBookmarkIsCommit'`
Expected: FAIL (`IsCommit` undefined; commit-pointer Display has a trailing ` / `).

- [ ] **Step 3: Implement** — in `internal/model/model.go`, change the final return of `FileAddress.Display()`:

```go
	if a.Path == "" {
		return fmt.Sprintf("%s / %s", container, mid)
	}
	return fmt.Sprintf("%s / %s / %s", container, mid, a.Path)
```

and add, next to `Bookmark.Address()`:

```go
// IsCommit reports whether the bookmark points at a commit itself (a path-less
// committed pointer) rather than a file within a commit.
func (b Bookmark) IsCommit() bool {
	return b.Path == "" && b.State == StateCommitted
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/model/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/model/model.go internal/model/fileaddress_test.go
git commit -m "feat(model): commit bookmarks — IsCommit + path-less address Display"
```

---

### Task 2: `BookmarkAdd` BlobSHA gate + central `BookmarkBytes` guard

**Files:**
- Modify: `internal/domain/bookmark.go` (`BookmarkAdd`, `BookmarkBytes`)
- Test: `internal/domain/bookmark_test.go`

**Interfaces:**
- Consumes: `model.Bookmark.IsCommit()` (Task 1).
- Produces: `BookmarkAdd` of a commit bookmark stores empty `SHA` without calling `BlobSHA`; `BookmarkBytes` of a commit bookmark returns an error and never runs `cat-file`.

- [ ] **Step 1: Write the failing tests** — add to `internal/domain/bookmark_test.go`:

```go
func TestBookmarkAddCommitSkipsBlobSHA(t *testing.T) {
	svc, f := bmSvc(t) // FakeRunner has NO rev-parse response; calling it would not fill SHA
	b, err := svc.BookmarkAdd(context.Background(), model.Bookmark{
		State: model.StateCommitted, Commit: "c0ffee", Path: "",
	})
	if err != nil {
		t.Fatalf("BookmarkAdd(commit): %v", err)
	}
	if b.SHA != "" {
		t.Fatalf("commit bookmark must carry empty SHA, got %q", b.SHA)
	}
	if sawArg(f, "git rev-parse", "c0ffee") {
		t.Fatalf("BlobSHA must not be called for a commit bookmark: %+v", f.Calls)
	}
	if b.ID == "" {
		t.Fatal("no id assigned")
	}
}

func TestBookmarkBytesCommitErrors(t *testing.T) {
	svc, f := bmSvc(t)
	_, err := svc.BookmarkBytes(context.Background(), model.Bookmark{
		State: model.StateCommitted, Commit: "c0ffee", Path: "", // commit bookmark
	})
	if err == nil {
		t.Fatal("BookmarkBytes of a commit bookmark must error")
	}
	if len(f.Calls) != 0 {
		t.Fatalf("must not shell out for a commit bookmark: %+v", f.Calls)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/domain/ -run 'TestBookmarkAddCommitSkipsBlobSHA|TestBookmarkBytesCommitErrors'`
Expected: FAIL (`BookmarkAdd` errors or calls `rev-parse`; `BookmarkBytes` runs `cat-file blob ""`).

- [ ] **Step 3: Implement** — in `internal/domain/bookmark.go`:

In `BookmarkAdd`, gate the freeze on a non-empty path:

```go
	if b.State == model.StateCommitted && b.SHA == "" && b.Path != "" {
		sha, err := s.repo.BlobSHA(ctx, b.Commit, b.Path)
		if err != nil {
			return model.Bookmark{}, err
		}
		b.SHA = sha
	}
```

At the top of `BookmarkBytes`, before the switch:

```go
	if b.IsCommit() {
		return nil, errors.New("bookmark: commit bookmark has no file bytes")
	}
```

(`errors` is already imported.)

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/domain/`
Expected: PASS (existing `TestBookmarkAddFillsCommittedSHA` / `TestBookmarkBytesCommittedUsesCatFile` still pass — they use `Path: "a/b.go"`).

- [ ] **Step 5: Commit**

```bash
git add internal/domain/bookmark.go internal/domain/bookmark_test.go
git commit -m "feat(domain): commit bookmarks — skip BlobSHA freeze, guard BookmarkBytes"
```

---

### Task 3: stable, distinct `AddressID` for a commit bookmark

**Files:**
- Test only: `internal/bookmark/file_store_test.go`

**Interfaces:**
- Consumes: `bookmark.AddressID`, `bookmark.NewFileStore` (existing).

- [ ] **Step 1: Write the test** — add to `internal/bookmark/file_store_test.go`:

```go
func TestAddressIDCommitBookmark(t *testing.T) {
	commit := model.Bookmark{State: model.StateCommitted, Commit: "c0ffee", Path: ""}
	id1 := AddressID(commit)
	if id1 == "" || id1 != AddressID(commit) {
		t.Fatalf("commit bookmark id not stable: %q", id1)
	}
	// Distinct from a FILE bookmark at the same commit.
	file := model.Bookmark{State: model.StateCommitted, Commit: "c0ffee", Path: "a.go"}
	if AddressID(file) == id1 {
		t.Fatal("commit and file bookmark at the same commit must have distinct ids")
	}
	// Distinct from another commit.
	other := model.Bookmark{State: model.StateCommitted, Commit: "deadbee", Path: ""}
	if AddressID(other) == id1 {
		t.Fatal("different commits must have distinct ids")
	}
	// Round-trips through the store.
	fs := NewFileStore(t.TempDir())
	stored, err := fs.Add(commit)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fs.List(0, 100)
	if err != nil || len(got) != 1 || got[0].ID != stored.ID {
		t.Fatalf("commit bookmark did not round-trip: %+v err %v", got, err)
	}
}
```

- [ ] **Step 2: Run**

Run: `go test ./internal/bookmark/ -run TestAddressIDCommitBookmark`
Expected: PASS immediately (no production change — this is a guard locking the behavior in).

- [ ] **Step 3: Commit**

```bash
git add internal/bookmark/file_store_test.go
git commit -m "test(bookmark): lock commit-bookmark AddressID stability + distinctness"
```

---

### Task 4: "Bookmark this commit" Commits-panel action

**Files:**
- Modify: `internal/tui/bookmark.go` (add `commitBookmarkRow` + `firstLocalRef`)
- Modify: `internal/tui/action_menu.go` (register in `appendCommitContextRows`)
- Test: `internal/tui/bookmark_commit_test.go` (new)

**Interfaces:**
- Consumes: `m.backingIndex(panelCommits)`, `m.commits`, `m.bookmarkAddCmd`, `model.Bookmark`, `model.Ref`/`RefLocal`.
- Produces: `func (m Model) commitBookmarkRow() (actionRow, bool)` with id `"commit-bookmark"`.

- [ ] **Step 1: Write the failing test** — create `internal/tui/bookmark_commit_test.go`:

```go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestCommitBookmarkRowPresentOnCommits(t *testing.T) {
	m := loadedModelLinearCommits(t, 2) // real-git, focus lands per helper
	m.focus = panelCommits
	r, ok := m.commitBookmarkRow()
	if !ok {
		t.Fatal("Bookmark this commit should be offered on the Commits panel")
	}
	if r.id != "commit-bookmark" || r.run == nil {
		t.Fatalf("bad row: %+v", r)
	}
	// Off the Commits panel: absent.
	m.focus = panelBranches
	if _, ok := m.commitBookmarkRow(); ok {
		t.Fatal("must not be offered off the Commits panel")
	}
}

func TestCommitBookmarkRowInMenu(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m.focus = panelCommits
	found := false
	for _, r := range availableActions(m) {
		if r.id == "commit-bookmark" {
			found = true
		}
	}
	if !found {
		t.Fatal("commit-bookmark must appear in the Commits . menu")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run 'TestCommitBookmarkRow'`
Expected: FAIL (`commitBookmarkRow` undefined).

- [ ] **Step 3: Implement** — in `internal/tui/bookmark.go` add:

```go
// commitBookmarkRow offers "Bookmark this commit" on the Commits panel: persist a
// path-less pointer to the selected commit in the bookmark registry. Mirrors
// bookmarkAddRow ("Bookmark this file").
func (m Model) commitBookmarkRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	c := m.commits[bi]
	b := model.Bookmark{State: model.StateCommitted, Commit: c.Hash, Branch: firstLocalRef(c), Path: ""}
	return actionRow{
		id:    "commit-bookmark",
		label: "Bookmark this commit",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.bookmarkAddCmd(b)
		},
	}, true
}

// firstLocalRef returns the name of the first local-branch ref decorating c, for
// display sugar on a commit bookmark; "" when the commit is no branch's tip.
func firstLocalRef(c model.Commit) string {
	for _, r := range c.Refs {
		if r.Kind == model.RefLocal {
			return r.Name
		}
	}
	return ""
}
```

In `internal/tui/action_menu.go`, register it inside `appendCommitContextRows` (next to the other commit rows, e.g. right after the `rewordRow` block):

```go
	if r, ok := m.commitBookmarkRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/tui/ -run 'TestCommitBookmarkRow'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/bookmark.go internal/tui/action_menu.go internal/tui/bookmark_commit_test.go
git commit -m "feat(tui): 'Bookmark this commit' action on the Commits panel"
```

---

### Task 5: switcher safety guards for commit bookmarks

A commit bookmark can now exist in the `g` list. Make every file-only key
safe before wiring compare. `enter` on a commit bookmark shows a temporary
notice (Task 6 replaces it with the compare).

**Files:**
- Modify: `internal/tui/bookmark_popup.go` (`enter`, `p`, `c`, `m` handlers)
- Test: `internal/tui/bookmark_commit_test.go`

**Interfaces:**
- Consumes: `bookmarkPopup.selected()`, `model.Bookmark.IsCommit()`, `m.statusMsg`.
- Produces: file-only keys no-op with a notice on a commit bookmark; `enter` on a commit bookmark sets a notice (interim).

- [ ] **Step 1: Write the failing tests** — add to `internal/tui/bookmark_commit_test.go`:

```go
func commitBmPopupModel(t *testing.T) Model {
	m := loadedModelLinearCommits(t, 2)
	m.focus = panelCommits
	cb := model.Bookmark{State: model.StateCommitted, Commit: m.commits[0].Hash, Path: "", ID: "cb1"}
	return m.pushLayer(newBookmarkPopup([]model.Bookmark{cb}))
}

func TestCommitBookmarkPasteIsNoop(t *testing.T) {
	m := commitBmPopupModel(t)
	mm, _ := m.Update(keyMsg("p"))
	m = mm.(Model)
	if m.bookmarkSwitcher() == nil {
		t.Fatal("paste on a commit bookmark must not leave/replace the switcher")
	}
	if m.statusMsg == "" {
		t.Fatal("expected a notice that paste is unavailable for a commit bookmark")
	}
}

func TestCommitBookmarkMarkIsNoop(t *testing.T) {
	m := commitBmPopupModel(t)
	mm, _ := m.Update(keyMsg("m"))
	m = mm.(Model)
	if p := m.bookmarkSwitcher(); p == nil || p.markID != "" {
		t.Fatal("m on a commit bookmark must not record a mark")
	}
	if m.statusMsg == "" {
		t.Fatal("expected a notice for m on a commit bookmark")
	}
}
```

(`keyMsg` and `loadedModelLinearCommits` already exist in the tui test package.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run 'TestCommitBookmarkPaste|TestCommitBookmarkMark'`
Expected: FAIL (paste tries to resolve bytes / mark gets recorded; no notice).

- [ ] **Step 3: Implement** — in `internal/tui/bookmark_popup.go`, add a guard helper and use it. Near the top of the file (after the type), add:

```go
// commitBookmarkNotice sets a "not for a commit bookmark" status and reports
// true when the highlighted bookmark is a commit pointer (so the caller can
// no-op the file-only key).
func (m *Model) commitBookmarkNotice(p *bookmarkPopup) bool {
	if b, ok := p.selected(); ok && b.IsCommit() {
		m.statusMsg = "not available for a commit bookmark"
		return true
	}
	return false
}
```

In `update`, guard the file-only cases. For `p`:

```go
		case "p":
			if p.compareRef != nil {
				return m, nil
			}
			if m.commitBookmarkNotice(p) {
				return m, nil
			}
			return m.bookmarkPastePrompt()
```

For `m` (mark):

```go
		case "m":
			if p.compareRef != nil {
				return m, nil
			}
			if m.commitBookmarkNotice(p) {
				return m, nil
			}
			return m.bookmarkMark()
```

For `c` (vs shelf):

```go
		case "c":
			if p.compareRef != nil {
				return m, nil
			}
			if m.commitBookmarkNotice(p) {
				return m, nil
			}
			b, ok := p.selected()
			// ... existing body unchanged ...
```

For `enter` (interim notice — replaced in Task 6): in the `tea.KeyEnter` case, before `return m.bookmarkJump()`:

```go
	case tea.KeyEnter:
		if p.compareRef != nil {
			b, ok := p.selected()
			if !ok {
				return m, nil
			}
			if b.IsCommit() {
				m.statusMsg = "cannot compare a file against a commit bookmark"
				return m, nil
			}
			return m.openCompareFocusedVsBookmark(*p.compareRef, p.compareLabel, b)
		}
		if m.commitBookmarkNotice(p) { // interim — Task 6 wires the compare
			return m, nil
		}
		return m.bookmarkJump()
```

Note: `commitBookmarkNotice` takes `*Model`; call it as `m.commitBookmarkNotice(p)` — `m` is an addressable local in `update`, and the method mutates `m.statusMsg` on the same value the function returns. Verify by running the tests.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/tui/ -run 'TestCommitBookmark'`
Expected: PASS

- [ ] **Step 5: Run the whole tui + model + domain + bookmark suites**

Run: `go test ./internal/tui/ ./internal/model/ ./internal/domain/ ./internal/bookmark/`
Expected: PASS (no regression to file-bookmark jump/paste/mark/compare).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/bookmark_popup.go internal/tui/bookmark_commit_test.go
git commit -m "feat(tui): guard file-only switcher keys against commit bookmarks"
```

---

## Stage 2 — compare against a bookmarked commit

### Task 6: `enter` on a commit bookmark → whole-tree compare vs selected commit

**Files:**
- Modify: `internal/tui/bookmark_popup.go` (`enter` non-compare branch; add `compareCommitBookmark`)
- Test: `internal/tui/bookmark_commit_test.go`

**Interfaces:**
- Consumes: `m.backingIndex(panelCommits)`, `m.commits`, `m.clearLayers()`, `m.openCompareFiles(left, right model.Endpoint)`, `model.Endpoint{Kind: model.EndpointCommit, Hash}`.
- Produces: `func (m Model) compareCommitBookmark(b model.Bookmark) (Model, tea.Cmd)`.

- [ ] **Step 1: Write the failing tests** — add to `internal/tui/bookmark_commit_test.go`:

```go
func TestCommitBookmarkEnterComparesVsSelected(t *testing.T) {
	m := loadedModelLinearCommits(t, 3) // newest-first feed: index 0 newest
	m.focus = panelCommits
	base := m.commits[2].Hash // an older commit — the bookmark
	m.sel[panelCommits] = 0   // select the newest as the subject
	cb := model.Bookmark{State: model.StateCommitted, Commit: base, Path: "", ID: "cb1"}
	m = m.pushLayer(newBookmarkPopup([]model.Bookmark{cb}))

	mm, cmd := m.Update(keyMsg("enter"))
	m = mm.(Model)
	if m.bookmarkSwitcher() != nil {
		t.Fatal("enter should close the switcher (cleared layers)")
	}
	if !m.filesCompare || m.filesView == nil {
		t.Fatal("enter on a commit bookmark should open the compare files view")
	}
	if m.filesLeft.Hash != base {
		t.Fatalf("left/base must be the bookmark commit, got %q want %q", m.filesLeft.Hash, base)
	}
	if m.filesRight.Hash != m.commits[0].Hash {
		t.Fatalf("right/subject must be the selected commit, got %q", m.filesRight.Hash)
	}
	if cmd == nil {
		t.Fatal("expected a load command for the compare")
	}
}

func TestCommitBookmarkEnterSelfCompareNoop(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m.focus = panelCommits
	m.sel[panelCommits] = 0
	same := m.commits[0].Hash
	cb := model.Bookmark{State: model.StateCommitted, Commit: same, Path: "", ID: "cb1"}
	m = m.pushLayer(newBookmarkPopup([]model.Bookmark{cb}))

	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)
	if m.filesView != nil {
		t.Fatal("comparing a commit against itself must not open a compare")
	}
	if m.bookmarkSwitcher() == nil || m.statusMsg == "" {
		t.Fatal("expected the switcher to stay open with a notice")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run 'TestCommitBookmarkEnter'`
Expected: FAIL (interim notice path; no compare opened).

- [ ] **Step 3: Implement** — in `internal/tui/bookmark_popup.go`, add:

```go
// compareCommitBookmark opens a whole-tree compare of a bookmarked commit
// (left/base) against the currently-selected Commits-panel commit (right/
// subject), closing the switcher first. A self-compare or no loaded commit is a
// notice, not a compare.
func (m Model) compareCommitBookmark(b model.Bookmark) (Model, tea.Cmd) {
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		m.statusMsg = "no commit selected to compare against"
		return m, nil
	}
	subject := m.commits[bi].Hash
	if subject == b.Commit {
		m.statusMsg = "select a different commit to compare against"
		return m, nil
	}
	m = m.clearLayers() // close the switcher so the files view is not drawn under it
	return m.openCompareFiles(
		model.Endpoint{Kind: model.EndpointCommit, Hash: b.Commit},   // base
		model.Endpoint{Kind: model.EndpointCommit, Hash: subject})    // subject
}
```

Replace the interim `enter` non-compare branch from Task 5:

```go
		if b, ok := p.selected(); ok && b.IsCommit() {
			return m.compareCommitBookmark(b)
		}
		return m.bookmarkJump()
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/tui/ -run 'TestCommitBookmark'`
Expected: PASS (the interim-notice expectation is gone; compare + self-compare-notice pass).

- [ ] **Step 5: Run the full tui suite**

Run: `go test ./internal/tui/`
Expected: PASS (file-bookmark `enter` jump still works).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/bookmark_popup.go internal/tui/bookmark_commit_test.go
git commit -m "feat(tui): enter on a commit bookmark compares it against the selected commit"
```

---

### Task 7: docs + cheat sheet + full verification

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `README.md` (bookmark feature description)
- Modify: `CLAUDE.md` (bookmark package line)
- Modify: `internal/tui/popup_help.go` (`bookmarkSwitcherHelp`)

- [ ] **Step 1: CHANGELOG** — under `## [Unreleased] → ### Added`:

```markdown
- **Bookmark a commit and compare against it.** The Commits panel `.` menu now
  has **Bookmark this commit**, storing a path-less pointer in the same `g`
  switcher as file bookmarks. In the switcher, `enter` on a bookmarked commit
  opens a whole-tree compare of it (base) against the currently-selected
  Commits-panel commit (subject). File-only actions (paste / vs-shelf / mark)
  are not offered for a commit bookmark.
```

- [ ] **Step 2: README** — in the bookmark section, add one sentence that a commit (not just a file) can be bookmarked via the Commits `.` menu and compared against the selected commit from the `g` switcher. (Match the surrounding wording; keep it to a sentence.)

- [ ] **Step 3: CLAUDE.md** — in the `bookmark` package row, note that bookmarks now also include **path-less commit pointers** (identity = the commit; no blob SHA; `enter` in the switcher whole-tree-compares against the selected Commits-panel commit).

- [ ] **Step 4: Cheat sheet** — in `internal/tui/popup_help.go`, in `bookmarkSwitcherHelp`, add one line (non-compare mode) documenting: `enter on a commit bookmark → compare it against the selected commit; file/paste/shelf/mark keys are file-only`. Match the existing `contentLine`/`r(...)` row style in that function.

- [ ] **Step 5: Verify docs render and nothing else broke**

Run: `./test.sh race`
Expected: `all green`

- [ ] **Step 6: Commit**

```bash
git add CHANGELOG.md README.md CLAUDE.md internal/tui/popup_help.go
git commit -m "docs(tui): commit bookmarks + compare in CHANGELOG/README/CLAUDE/cheat sheet"
```

---

## Resolution-path audit checklist (verify during Task 5 & 6)

Every site that resolves a *selected* bookmark to bytes, and how a commit bookmark is handled:

- [ ] `enter` non-compare → `compareCommitBookmark` (routed away from `bookmarkJump`/`BookmarkBytes`) — Task 6
- [ ] `enter` compare-mode (`compareRef != nil`) → `IsCommit` notice, no `openCompareFocusedVsBookmark` — Task 5
- [ ] `p` paste (`bookmarkPastePrompt` → `BookmarkBytes`) → `IsCommit` notice — Task 5
- [ ] `c` vs shelf → `IsCommit` notice — Task 5
- [ ] `m` mark (`bookmarkMark` → `loadBookmarkCompareTwoCmd` → `BookmarkBytes`) → `IsCommit` notice (no mark recorded) — Task 5
- [ ] `x` remove (`bookmarkRemoveCmd(b.ID)`) → id-only, safe, no change
- [ ] `domain.BookmarkBytes` central guard → clean error for any commit bookmark, backstops the CLI (`internal/cli/bookmark.go`) — Task 2

## Self-Review notes

- Spec coverage: Task 1 (model `Display`/`IsCommit`), Task 2 (BlobSHA gate + central guard), Task 3 (AddressID), Task 4 (create), Task 5 (file-only guards + compare-mode guard), Task 6 (enter compare + direction + self-compare guard), Task 7 (docs/cheat sheet). The resolution-path audit is the checklist above.
- Type consistency: `IsCommit()` (Task 1) used in Tasks 2/5/6; `commitBookmarkRow`/`compareCommitBookmark`/`firstLocalRef`/`commitBookmarkNotice` are the only new TUI symbols; `openCompareFiles(left, right model.Endpoint)` and `clearLayers()` are existing.
- CLI is out of scope; the central `BookmarkBytes` guard ensures it fails cleanly rather than emitting a broken diff. No `agentskill.Version` bump (CLI surface unchanged).
