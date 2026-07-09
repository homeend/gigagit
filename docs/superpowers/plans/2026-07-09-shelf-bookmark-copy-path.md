# Copy File Path/Name from Shelf & Bookmark Switchers — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Pressing `y` on a single-file entry in the shelf (`G`) or bookmark (`g`) quick-switcher opens a small Copy-file-path / Copy-file-name / Cancel chooser that writes to the system clipboard.

**Architecture:** A pure option→payload mapper (`copyFileChoice`) plus a shared modal builder (`copyFilePrompt`) live beside `copyToClipboardCmd` in `internal/tui/clipboard_cmd.go`; each switcher popup adds a `case "y":` that guards (compare mode → inert; commit entry → existing notice) and calls the shared builder with its entry's repo-relative path. No engine op, no domain query, no CLI or store change — paths are already structured fields (`ShelfEntry.Origin.Path`, `Bookmark.Path`).

**Tech Stack:** Go 1.26, Bubble Tea (Elm-style value-receiver `Model`), existing `internal/clipboard` writer. Spec: `docs/superpowers/specs/2026-07-09-shelf-bookmark-copy-path-design.md`.

## Global Constraints

- Modal: `ID: "copy-file"`, `Prompt: "Copy — " + p`, `Options: []string{"Copy file path", "Copy file name", "Cancel"}` — **Cancel must be the LAST option** (the TUI modal maps esc to the option named "abort" if present, else the last option; Cancel last makes esc a genuine cancel).
- Status strings exactly match the Files-panel copy rows (`fileCopyPathName`, `internal/tui/action_menu.go:462`): `"Copied path: " + p` and `"Copied file name: " + path.Base(p)`.
- Use the `path` stdlib package (slash-separated git paths), NOT `path/filepath`.
- `y` acts on file entries only: shelved commits go through `commitShelfNotice`, commit bookmarks through `commitBookmarkNotice` (existing helpers — do not duplicate their message strings). In compare-picker mode (`p.compareRef != nil`) `y` is inert. While filtering, `y` stays query text (the new case goes in the non-filtering rune switch only).
- Tests must NEVER run the returned clipboard cmd (it writes the real clipboard); assert on the `decisionState` fields and on cmd nil-ness only. `clipboardCopiedMsg` handling is already covered by `clipboard_cmd_test.go`.
- Every code file gofmt-clean; run tests from the worktree root `/mnt/t/others/gigagit/.claude/worktrees/shelf-bookmark-copy` on branch `feat/shelf-bookmark-copy-path`.

---

### Task 1: `copyFileChoice` + `copyFilePrompt` (shared chooser)

**Files:**
- Modify: `internal/tui/clipboard_cmd.go`
- Test: `internal/tui/clipboard_cmd_test.go`

**Interfaces:**
- Consumes: existing `copyToClipboardCmd(ok, text string) tea.Cmd` (same file), `decisionState` (`internal/tui/op.go:154` — fields `req engine.DecisionRequest`, `onResolve func(m Model, opt string) (tea.Model, tea.Cmd)`), `engine.DecisionRequest{ID, Prompt, Options}`.
- Produces: `copyFileChoice(option, p string) (okMsg, text string, ok bool)` and `(m Model) copyFilePrompt(p string) (Model, tea.Cmd)` — Tasks 2 and 3 call `copyFilePrompt`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/clipboard_cmd_test.go`:

```go
func TestCopyFileChoice(t *testing.T) {
	okMsg, text, ok := copyFileChoice("Copy file path", "dir/f.txt")
	if !ok || okMsg != "Copied path: dir/f.txt" || text != "dir/f.txt" {
		t.Errorf("path choice = (%q, %q, %v)", okMsg, text, ok)
	}
	okMsg, text, ok = copyFileChoice("Copy file name", "dir/f.txt")
	if !ok || okMsg != "Copied file name: f.txt" || text != "f.txt" {
		t.Errorf("name choice = (%q, %q, %v)", okMsg, text, ok)
	}
	for _, opt := range []string{"Cancel", "bogus"} {
		if _, _, ok := copyFileChoice(opt, "dir/f.txt"); ok {
			t.Errorf("%q must not map to a copy", opt)
		}
	}
}

func TestCopyFilePromptOpensModal(t *testing.T) {
	m := footerModel()
	m, _ = m.copyFilePrompt("dir/f.txt")
	if m.modal == nil {
		t.Fatal("copyFilePrompt should set the chooser modal")
	}
	if m.modal.req.ID != "copy-file" {
		t.Errorf("modal ID = %q, want copy-file", m.modal.req.ID)
	}
	if m.modal.req.Prompt != "Copy — dir/f.txt" {
		t.Errorf("prompt = %q", m.modal.req.Prompt)
	}
	if got := strings.Join(m.modal.req.Options, "|"); got != "Copy file path|Copy file name|Cancel" {
		t.Errorf("options = %q (Cancel must be last: esc maps to the last option)", got)
	}
	// The copy options resolve to a clipboard cmd; Cancel resolves to nothing.
	// Never RUN the cmd — it would write the real clipboard.
	for _, opt := range []string{"Copy file path", "Copy file name"} {
		if _, cmd := m.modal.onResolve(m, opt); cmd == nil {
			t.Errorf("%q should return a clipboard cmd", opt)
		}
	}
	if _, cmd := m.modal.onResolve(m, "Cancel"); cmd != nil {
		t.Error("Cancel should return no cmd")
	}
}
```

Add `"strings"` to the test file's imports (alongside the existing `"errors"` and `"testing"`).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestCopyFileChoice|TestCopyFilePromptOpensModal' -v`
Expected: FAIL — `undefined: copyFileChoice` / `undefined: (Model).copyFilePrompt` (compile error).

- [ ] **Step 3: Implement**

In `internal/tui/clipboard_cmd.go`, extend the import block with `"path"` and the engine package so it reads:

```go
import (
	"io"
	"os"
	"path"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"

	"github.com/homeend/gigagit/internal/clipboard"
	"github.com/homeend/gigagit/internal/engine"
)
```

Append at the end of the file:

```go
// copyFileChoice maps a copy-chooser option to its status line and clipboard
// text. ok is false for Cancel or an unknown option. The strings match the
// Files-panel copy rows (fileCopyPathName) so both surfaces speak alike.
func copyFileChoice(option, p string) (okMsg, text string, ok bool) {
	switch option {
	case "Copy file path":
		return "Copied path: " + p, p, true
	case "Copy file name":
		return "Copied file name: " + path.Base(p), path.Base(p), true
	}
	return "", "", false
}

// copyFilePrompt opens the path/name copy chooser for a repo-relative file
// path. The modal renders above the calling popup (which stays on the layer
// stack); Cancel — kept last so esc maps to it — reveals it unchanged.
func (m Model) copyFilePrompt(p string) (Model, tea.Cmd) {
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "copy-file",
			Prompt:  "Copy — " + p,
			Options: []string{"Copy file path", "Copy file name", "Cancel"},
		},
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if okMsg, text, ok := copyFileChoice(opt, p); ok {
				return m, m.copyToClipboardCmd(okMsg, text)
			}
			return m, nil
		},
	}
	return m, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestCopyFileChoice|TestCopyFilePromptOpensModal' -v`
Expected: PASS (both).

- [ ] **Step 5: Vet, format, commit**

```bash
gofmt -l internal/tui/ && go vet ./internal/tui/
gg add internal/tui/clipboard_cmd.go internal/tui/clipboard_cmd_test.go
gg commit -m "feat(tui): shared copy-file-path/name chooser modal"
```

Expected: `gofmt -l` prints nothing; vet clean.

---

### Task 2: `y` in the shelf switcher (G)

**Files:**
- Modify: `internal/tui/shelf_popup.go` (rune switch in `(*shelfPopup).update`, ~line 300; hint slice in `renderShelfPopupBox`, ~line 130)
- Modify: `internal/tui/popup_help.go` (`shelfSwitcherHelp`, non-compare list)
- Modify: `internal/tui/help.go` (the `r("G", …)` switcher line, ~line 22)
- Test: `internal/tui/shelf_popup_test.go`

**Interfaces:**
- Consumes: `(m Model) copyFilePrompt(p string) (Model, tea.Cmd)` from Task 1; existing `commitShelfNotice(p *shelfPopup) (Model, bool)` (`shelf_popup.go:320`, message `"not available for a shelved commit — [t] copies it to a temp dir"`); `(*shelfPopup).selected() (model.ShelfEntry, bool)`; test helpers `shelfPopModel`, `shEntry`, `keyMsg`, `runeKey` (all existing).
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/shelf_popup_test.go` (the file already imports `strings`, `testing`, `model`):

```go
func TestShelfPopupYOpensCopyChooser(t *testing.T) {
	m := shelfPopModel(shEntry("a", "dir/x.go"))
	mm, _ := m.Update(keyMsg("y"))
	m = mm.(Model)
	if m.modal == nil || m.modal.req.ID != "copy-file" {
		t.Fatalf("y should open the copy chooser modal, modal=%+v", m.modal)
	}
	if !strings.Contains(m.modal.req.Prompt, "dir/x.go") {
		t.Errorf("prompt should name the entry's path, got %q", m.modal.req.Prompt)
	}
	if m.shelfSwitcher() == nil {
		t.Error("the switcher must stay on the stack beneath the modal")
	}
}

func TestShelfPopupYOnCommitEntryNotices(t *testing.T) {
	e := model.ShelfEntry{ID: "c1", Kind: model.ShelfKindCommit,
		Origin: model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5"}}
	m := shelfPopModel(e)
	mm, _ := m.Update(keyMsg("y"))
	m = mm.(Model)
	if m.modal != nil {
		t.Fatal("y must not open the chooser for a shelved commit")
	}
	if !strings.Contains(m.statusMsg, "shelved commit") {
		t.Errorf("statusMsg = %q, want the shelved-commit notice", m.statusMsg)
	}
}

func TestShelfPopupYInertInCompareMode(t *testing.T) {
	m := shelfPopModel(shEntry("a", "x.go"))
	m.shelfSwitcher().compareRef = &model.FileRef{Source: model.SourceUnstaged, Path: "focused.go"}
	mm, _ := m.Update(keyMsg("y"))
	m = mm.(Model)
	if m.modal != nil {
		t.Fatal("y must be inert in compare-picker mode")
	}
}

func TestShelfPopupYEmptyListNoop(t *testing.T) {
	m := shelfPopModel()
	mm, _ := m.Update(keyMsg("y"))
	m = mm.(Model)
	if m.modal != nil {
		t.Fatal("y on an empty list must be a no-op")
	}
}

func TestShelfPopupYWhileFilteringIsText(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &shelfPopup{filtering: true}
	p.update(m, runeKey("y"))
	if p.filter != "y" {
		t.Fatalf(`"y" while filtering must be a literal char; filter=%q`, p.filter)
	}
}

func TestShelfPopupAdvertisesCopy(t *testing.T) {
	m := shelfPopModel(shEntry("a", "x.go"))
	if out := m.renderShelfPopupBox(m.shelfSwitcher()); !strings.Contains(out, "[y] copy") {
		t.Errorf("hint line missing [y] copy:\n%s", out)
	}
	found := false
	for _, l := range shelfSwitcherHelp(false) {
		if strings.HasPrefix(l.text, "y ") {
			found = true
		}
	}
	if !found {
		t.Error("shelfSwitcherHelp(false) missing the y row")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestShelfPopupY|TestShelfPopupAdvertisesCopy' -v`
Expected: FAIL — the chooser tests fail with `m.modal == nil` (`y` is currently unhandled), the advertise test fails on both asserts. (`TestShelfPopupYWhileFilteringIsText` and `TestShelfPopupYEmptyListNoop` pass already — they pin current behavior.)

- [ ] **Step 3: Implement**

In `internal/tui/shelf_popup.go`, inside `(*shelfPopup).update`'s `case tea.KeyRunes:` switch, add after the `case "t":` block (which ends `return m.startTempExportShelf(e)`):

```go
		case "y":
			if p.compareRef != nil {
				return m, nil
			}
			if nm, blocked := m.commitShelfNotice(p); blocked {
				return nm, nil
			}
			e, ok := p.selected()
			if !ok {
				return m, nil
			}
			return m.copyFilePrompt(e.Origin.Path)
```

In `renderShelfPopupBox`, change the hint slice — insert `"[y] copy"` after `"[t] temp dir"`:

```go
	hint := []string{"[?] keys", "[enter] diff", "[e] editor", "[p] restore", "[t] temp dir", "[y] copy", "[m] mark/compare", "[x] remove", "[c] vs bookmark", "[/] filter", "[z] mode", "[ctrl+t] full", "[esc] close"}
```

In `internal/tui/popup_help.go`, in `shelfSwitcherHelp`'s **non-compare** return list, insert after the `cheatRow("t", …)` line:

```go
		cheatRow("y", "copy the file's path or name to the clipboard (file entries only)"),
```

In `internal/tui/help.go`, in the `r("G", …)` line, insert after `t copies to a new dir under <repo>.tmp,`:

```
y copies the file's path or name to the clipboard,
```

so the fragment reads `… t copies to a new dir under <repo>.tmp, y copies the file's path or name to the clipboard, m marks one then a second …`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestShelfPopupY|TestShelfPopupAdvertisesCopy' -v`
Expected: PASS (all six).

Run: `go test ./internal/tui/`
Expected: PASS (no regressions — some render tests assert hint content).

- [ ] **Step 5: Vet, format, commit**

```bash
gofmt -l internal/tui/ && go vet ./internal/tui/
gg add internal/tui/shelf_popup.go internal/tui/popup_help.go internal/tui/help.go internal/tui/shelf_popup_test.go
gg commit -m "feat(tui): y copies file path/name from the shelf switcher"
```

---

### Task 3: `y` in the bookmark switcher (g)

**Files:**
- Modify: `internal/tui/bookmark_popup.go` (rune switch in `(*bookmarkPopup).update`, ~line 332; hint slice in `renderBookmarkPopupBox`, ~line 149)
- Modify: `internal/tui/popup_help.go` (`bookmarkSwitcherHelp`, non-compare list)
- Modify: `internal/tui/help.go` (the `r("g", …)` switcher line, ~line 21)
- Test: `internal/tui/bookmark_popup_test.go`

**Interfaces:**
- Consumes: `(m Model) copyFilePrompt(p string) (Model, tea.Cmd)` from Task 1; existing `commitBookmarkNotice(p *bookmarkPopup) (Model, bool)` (`bookmark_popup.go:549`, message `"not available for a commit bookmark"`); `(*bookmarkPopup).selected() (model.Bookmark, bool)`; `newBookmarkPopup`; test helpers `footerModel`, `keyMsg`, `runeKey` (all existing).
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/bookmark_popup_test.go`. The file currently imports `fmt`, `testing`, `lipgloss`, `model` — add `"strings"`:

```go
func bookmarkCopyModel(items ...model.Bookmark) Model {
	m := footerModel()
	m.width, m.height = 100, 30
	m = m.pushLayer(newBookmarkPopup(items))
	return m
}

func TestBookmarkPopupYOpensCopyChooser(t *testing.T) {
	m := bookmarkCopyModel(model.Bookmark{ID: "b1", State: model.StateUnstaged, Worktree: "/wt", Path: "dir/y.go"})
	mm, _ := m.Update(keyMsg("y"))
	m = mm.(Model)
	if m.modal == nil || m.modal.req.ID != "copy-file" {
		t.Fatalf("y should open the copy chooser modal, modal=%+v", m.modal)
	}
	if !strings.Contains(m.modal.req.Prompt, "dir/y.go") {
		t.Errorf("prompt should name the bookmark's path, got %q", m.modal.req.Prompt)
	}
	if m.bookmarkSwitcher() == nil {
		t.Error("the switcher must stay on the stack beneath the modal")
	}
}

func TestBookmarkPopupYOnCommitBookmarkNotices(t *testing.T) {
	m := bookmarkCopyModel(model.Bookmark{ID: "cb", State: model.StateCommitted, Commit: "a1b2c3d4e5"})
	mm, _ := m.Update(keyMsg("y"))
	m = mm.(Model)
	if m.modal != nil {
		t.Fatal("y must not open the chooser for a commit bookmark")
	}
	if !strings.Contains(m.statusMsg, "commit bookmark") {
		t.Errorf("statusMsg = %q, want the commit-bookmark notice", m.statusMsg)
	}
}

func TestBookmarkPopupYInertInCompareMode(t *testing.T) {
	m := bookmarkCopyModel(model.Bookmark{ID: "b1", State: model.StateUnstaged, Worktree: "/wt", Path: "y.go"})
	m.bookmarkSwitcher().compareRef = &model.FileRef{Source: model.SourceUnstaged, Path: "focused.go"}
	mm, _ := m.Update(keyMsg("y"))
	m = mm.(Model)
	if m.modal != nil {
		t.Fatal("y must be inert in compare-picker mode")
	}
}

func TestBookmarkPopupYWhileFilteringIsText(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &bookmarkPopup{filtering: true}
	p.update(m, runeKey("y"))
	if p.filter != "y" {
		t.Fatalf(`"y" while filtering must be a literal char; filter=%q`, p.filter)
	}
}

func TestBookmarkPopupAdvertisesCopy(t *testing.T) {
	m := bookmarkCopyModel(model.Bookmark{ID: "b1", State: model.StateUnstaged, Worktree: "/wt", Path: "y.go"})
	if out := m.renderBookmarkPopupBox(m.bookmarkSwitcher()); !strings.Contains(out, "[y] copy") {
		t.Errorf("hint line missing [y] copy:\n%s", out)
	}
	found := false
	for _, l := range bookmarkSwitcherHelp(false) {
		if strings.HasPrefix(l.text, "y ") {
			found = true
		}
	}
	if !found {
		t.Error("bookmarkSwitcherHelp(false) missing the y row")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestBookmarkPopupY|TestBookmarkPopupAdvertisesCopy' -v`
Expected: FAIL — chooser/notice/advertise tests fail (`y` unhandled); the filtering test passes already (pins current behavior).

- [ ] **Step 3: Implement**

In `internal/tui/bookmark_popup.go`, inside `(*bookmarkPopup).update`'s `case tea.KeyRunes:` switch, add after the `case "t":` block (which ends `return m.startTempExportBookmark(b)`):

```go
		case "y":
			if p.compareRef != nil {
				return m, nil
			}
			if mm, yes := m.commitBookmarkNotice(p); yes {
				return mm, nil
			}
			b, ok := p.selected()
			if !ok {
				return m, nil
			}
			return m.copyFilePrompt(b.Path)
```

In `renderBookmarkPopupBox`, change the hint slice — insert `"[y] copy"` after `"[t] temp dir"`:

```go
	hint := []string{"[?] keys", "[enter] jump", "[e] editor", "[p] paste", "[t] temp dir", "[y] copy", "[m] mark/compare", "[x] remove", "[c] vs shelf", "[/] filter", "[z] mode", "[ctrl+t] full", "[esc] close"}
```

In `internal/tui/popup_help.go`, in `bookmarkSwitcherHelp`'s **non-compare** return list, insert after the `cheatRow("t", …)` line:

```go
		cheatRow("y", "copy the bookmarked file's path or name to the clipboard (file bookmarks only)"),
```

In `internal/tui/help.go`, in the `r("g", …)` line, insert after `t copies to a new dir under <repo>.tmp (works for file AND commit bookmarks),`:

```
y copies the file's path or name to the clipboard,
```

so the fragment reads `… (works for file AND commit bookmarks), y copies the file's path or name to the clipboard, m marks one then a second to compare …`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestBookmarkPopupY|TestBookmarkPopupAdvertisesCopy' -v`
Expected: PASS (all five).

Run: `go test ./internal/tui/`
Expected: PASS.

- [ ] **Step 5: Vet, format, commit**

```bash
gofmt -l internal/tui/ && go vet ./internal/tui/
gg add internal/tui/bookmark_popup.go internal/tui/popup_help.go internal/tui/help.go internal/tui/bookmark_popup_test.go
gg commit -m "feat(tui): y copies file path/name from the bookmark switcher"
```

---

### Task 4: User docs (CHANGELOG, README)

**Files:**
- Modify: `CHANGELOG.md` (top of the `## [Unreleased]` → `### Added` section)
- Modify: `README.md` (the `g` and `G` key-table rows, lines ~72–73)

**Interfaces:**
- Consumes: the shipped behavior from Tasks 1–3 (key `y`, chooser options, file-entries-only gating).
- Produces: nothing.

- [ ] **Step 1: CHANGELOG entry**

In `CHANGELOG.md`, add as the FIRST bullet under `## [Unreleased]` → `### Added`:

```markdown
- **Copy a file's path or name from the `g`/`G` switchers**: pressing `y` on a
  single-file bookmark or shelf entry opens a small chooser — *Copy file path*
  (the repo-relative path) / *Copy file name* (just the basename) / *Cancel* —
  and writes the pick to the system clipboard, matching the Files-panel
  `.`-menu copy rows. On a shelved commit or a commit bookmark `y` shows the
  usual "not available" notice instead.
```

- [ ] **Step 2: README key-table rows**

In `README.md`, in the `g` row (line ~72), insert after `` `t` **copies it to a temp dir** (see below), ``:

```
`y` copies the bookmarked file's path or name to the clipboard,
```

In the `G` row (line ~73), insert after `` `t` **copies it to a temp dir** (see below), ``:

```
`y` copies the file's path or name to the clipboard,
```

Both rows keep the rest of their text unchanged.

- [ ] **Step 3: Verify and commit**

Run: `go build ./cmd/gg && go test ./internal/tui/`
Expected: build ok, tests PASS (docs-only change; this is the cheap final sanity pass).

```bash
gg add CHANGELOG.md README.md
gg commit -m "docs: copy file path/name from the g/G switchers"
```

---

## Final verification (after all tasks)

Run from the worktree root:

```bash
./test.sh race
```

Expected: vet+gofmt clean, unit tests pass, e2e pass (no e2e touches this TUI-only feature, but the full race suite is the merge gate).
