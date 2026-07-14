# Copy absolute file path — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Copy absolute file path" clipboard action alongside every existing "Copy file path" surface in the TUI.

**Architecture:** Additive, TUI-only. A single shared `Model.absFilePath(base, rel)` helper joins a repo-relative path onto the file's worktree root (empty `base` → current worktree). Three existing copy-path surfaces each gain an absolute sibling: the `.` action menu (`fileCopyPathName`), the fuzzy file finder (`ff-copy-path`), and the `y` copy chooser (`copyFilePrompt`/`copyFileChoice`) used by the `g`/`G` switchers. No engine op, no CLI, no `domain` query.

**Tech Stack:** Go 1.26, Bubble Tea TUI. `filepath.Join` for the OS-correct absolute path; `clipboard.Copy` (native / OSC 52) already backs every copy row.

## Global Constraints

- Absolute path = `filepath.Join(<worktree-root>, <repo-relative-path>)`; no symlink resolution.
- The existing repo-relative "Copy file path" and "Copy file name" actions are unchanged.
- `.`-menu row order: `Copy file path` · **`Copy absolute file path`** · `Copy file name`.
- `y` chooser option order (Cancel last so `esc` maps to Cancel): `Copy file path` · **`Copy absolute file path`** · `Copy file name` · `Cancel`.
- File finder label is the shorter **`Copy absolute path`** (matches its existing `Copy path`).
- New status line: `Copied absolute path: <abs>`.
- Tests use `footerModel()` (sets `currentWorktree: "/repo"`); build/test via `go test ./internal/tui/...` and finally `./test.sh`.
- `internal/tui` must not import `internal/git` (archtest-guarded) — this change touches none of that.

---

### Task 1: Shared `absFilePath` helper + `.`-menu absolute row

**Files:**
- Modify: `internal/tui/clipboard_cmd.go` — add `import "path/filepath"` and the `absFilePath` method.
- Modify: `internal/tui/action_menu.go:461-467` — `fileCopyPathName` inserts the abs row.
- Test: `internal/tui/action_menu_copyrows_test.go`

**Interfaces:**
- Produces: `func (m Model) absFilePath(base, rel string) string` — `filepath.Join(base, rel)`, with `base` defaulting to `m.currentWorktree` when empty. Consumed by Tasks 2 and 3.
- Produces: `fileCopyPathName` now returns 3 rows; the new one has id `copy-file-abspath`.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/action_menu_copyrows_test.go`:

```go
func TestFileCopyPathNameIncludesAbsolute(t *testing.T) {
	m := footerModel() // currentWorktree == "/repo"
	rows := m.fileCopyPathName("dir/f.go")
	got := ids(rows)
	if !got["copy-file-path"] || !got["copy-file-abspath"] || !got["copy-file-name"] {
		t.Fatalf("rows = %v, want path/abspath/name", got)
	}
	if r, _ := findRow(rows, "copy-file-abspath"); r.copyText != "/repo/dir/f.go" {
		t.Errorf("abspath copyText = %q, want /repo/dir/f.go", r.copyText)
	}
	// The repo-relative row is unchanged.
	if r, _ := findRow(rows, "copy-file-path"); r.copyText != "dir/f.go" {
		t.Errorf("path copyText = %q, want dir/f.go", r.copyText)
	}
}

func TestAbsFilePathDefaultsToCurrentWorktree(t *testing.T) {
	m := footerModel() // currentWorktree == "/repo"
	if got := m.absFilePath("", "a/b.go"); got != "/repo/a/b.go" {
		t.Errorf("empty base = %q, want /repo/a/b.go", got)
	}
	if got := m.absFilePath("/wt", "a/b.go"); got != "/wt/a/b.go" {
		t.Errorf("explicit base = %q, want /wt/a/b.go", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestFileCopyPathNameIncludesAbsolute|TestAbsFilePathDefaultsToCurrentWorktree' -v`
Expected: FAIL — `m.absFilePath` undefined; `copy-file-abspath` missing.

- [ ] **Step 3: Add the helper**

In `internal/tui/clipboard_cmd.go`, add `"path/filepath"` to the std import block (after `"path"`), and add near the top of the file:

```go
// absFilePath joins a repo-relative path onto base, defaulting to the current
// worktree when base is empty. It is the single source of truth for the
// "Copy absolute file path" actions so every surface agrees byte-for-byte.
func (m Model) absFilePath(base, rel string) string {
	if base == "" {
		base = m.currentWorktree
	}
	return filepath.Join(base, rel)
}
```

- [ ] **Step 4: Add the `.`-menu row**

In `internal/tui/action_menu.go`, replace `fileCopyPathName` (currently lines 461-467):

```go
// fileCopyPathName returns the repo-relative path, absolute path, and basename
// copy rows for a file. The absolute path is anchored on the current worktree.
func (m Model) fileCopyPathName(p string) []actionRow {
	abs := m.absFilePath("", p)
	return []actionRow{
		m.copyRow("copy-file-path", "Copy file path", "Copied path: "+p, p),
		m.copyRow("copy-file-abspath", "Copy absolute file path", "Copied absolute path: "+abs, abs),
		m.copyRow("copy-file-name", "Copy file name", "Copied file name: "+path.Base(p), path.Base(p)),
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestFileCopyPathName|TestAbsFilePath|TestContextCopyRows' -v`
Expected: PASS (the existing `TestContextCopyRows*` still pass — they only assert presence of `copy-file-path`/`copy-file-name`, both retained).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/clipboard_cmd.go internal/tui/action_menu.go internal/tui/action_menu_copyrows_test.go
git commit -m "feat(tui): Copy absolute file path in the . menu"
```

---

### Task 2: File finder absolute-path row

**Files:**
- Modify: `internal/tui/file_finder.go:361-368` — add `ff-copy-abspath` after `ff-copy-path`.
- Test: `internal/tui/file_finder_actions_test.go`

**Interfaces:**
- Consumes: `Model.absFilePath` (Task 1).
- Produces: a new finder row id `ff-copy-abspath`.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/file_finder_actions_test.go`:

```go
func TestFileFinderCopyAbsPathRow(t *testing.T) {
	m, rows := finderSetup(t, "a/b.go") // finderSetup uses loadedModelLinearCommits
	run := finderRow(t, rows, "ff-copy-abspath")
	nm, cmd := run(m)
	if cmd == nil {
		t.Fatal("ff-copy-abspath should return a non-nil tea.Cmd")
	}
	if layerOf[*fileFinderPopup](nm.(Model)) != nil {
		t.Fatal("the finder must be popped by ff-copy-abspath")
	}
}
```

Also extend the id list in the existing `TestFileFinderEnterOpensActionMenu` (add `"ff-copy-abspath"` to the checked ids slice at line 25).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestFileFinderCopyAbsPathRow|TestFileFinderEnterOpensActionMenu' -v`
Expected: FAIL — `fileFinderActionRows` has no `ff-copy-abspath`.

- [ ] **Step 3: Add the finder row**

In `internal/tui/file_finder.go`, immediately after the `ff-copy-path` row block (which ends at line 368 with `},`), insert:

```go
		{
			id:    "ff-copy-abspath",
			label: "Copy absolute path",
			run: func(m Model) (tea.Model, tea.Cmd) {
				m = m.popLayer()
				abs := m.absFilePath("", path)
				return m, m.copyToClipboardCmd("Copied "+abs, abs)
			},
		},
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestFileFinder' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/file_finder.go internal/tui/file_finder_actions_test.go
git commit -m "feat(tui): Copy absolute path in the fuzzy file finder"
```

---

### Task 3: `y` copy chooser absolute option + switcher call sites

**Files:**
- Modify: `internal/tui/clipboard_cmd.go` — `copyFileChoice` gains an `abs` param + the abs case; `copyFilePrompt` gains a `base` param and the abs option.
- Modify: `internal/tui/bookmark_popup.go:369` — pass `b.Worktree` as base.
- Modify: `internal/tui/shelf_popup.go:339` — pass `e.Origin.Worktree` as base.
- Test: `internal/tui/clipboard_cmd_test.go`, `internal/tui/bookmark_popup_test.go`, `internal/tui/shelf_popup_test.go`

**Interfaces:**
- Consumes: `Model.absFilePath` (Task 1).
- Produces:
  - `func copyFileChoice(option, p, abs string) (okMsg, text string, ok bool)`
  - `func (m Model) copyFilePrompt(base, p string) (Model, tea.Cmd)`

- [ ] **Step 1: Update existing chooser tests to the new signatures + option**

Replace `TestCopyFileChoice` and `TestCopyFilePromptOpensModal` in `internal/tui/clipboard_cmd_test.go` with:

```go
func TestCopyFileChoice(t *testing.T) {
	const p, abs = "dir/f.txt", "/repo/dir/f.txt"
	okMsg, text, ok := copyFileChoice("Copy file path", p, abs)
	if !ok || okMsg != "Copied path: dir/f.txt" || text != "dir/f.txt" {
		t.Errorf("path choice = (%q, %q, %v)", okMsg, text, ok)
	}
	okMsg, text, ok = copyFileChoice("Copy absolute file path", p, abs)
	if !ok || okMsg != "Copied absolute path: /repo/dir/f.txt" || text != "/repo/dir/f.txt" {
		t.Errorf("abs choice = (%q, %q, %v)", okMsg, text, ok)
	}
	okMsg, text, ok = copyFileChoice("Copy file name", p, abs)
	if !ok || okMsg != "Copied file name: f.txt" || text != "f.txt" {
		t.Errorf("name choice = (%q, %q, %v)", okMsg, text, ok)
	}
	for _, opt := range []string{"Cancel", "bogus"} {
		if _, _, ok := copyFileChoice(opt, p, abs); ok {
			t.Errorf("%q must not map to a copy", opt)
		}
	}
}

func TestCopyFilePromptOpensModal(t *testing.T) {
	m := footerModel() // currentWorktree == "/repo"
	m, _ = m.copyFilePrompt("", "dir/f.txt")
	if m.modal == nil {
		t.Fatal("copyFilePrompt should set the chooser modal")
	}
	if m.modal.req.ID != "copy-file" {
		t.Errorf("modal ID = %q, want copy-file", m.modal.req.ID)
	}
	if m.modal.req.Prompt != "Copy — dir/f.txt" {
		t.Errorf("prompt = %q", m.modal.req.Prompt)
	}
	const want = "Copy file path|Copy absolute file path|Copy file name|Cancel"
	if got := strings.Join(m.modal.req.Options, "|"); got != want {
		t.Errorf("options = %q, want %q (Cancel last: esc maps to it)", got, want)
	}
	for _, opt := range []string{"Copy file path", "Copy absolute file path", "Copy file name"} {
		if _, cmd := m.modal.onResolve(m, opt); cmd == nil {
			t.Errorf("%q should return a clipboard cmd", opt)
		}
	}
	if _, cmd := m.modal.onResolve(m, "Cancel"); cmd != nil {
		t.Error("Cancel should return no cmd")
	}
}

func TestCopyFilePromptBaseFallback(t *testing.T) {
	// An empty base resolves the absolute option against the current worktree;
	// a non-empty base (a cross-worktree bookmark) resolves against that base.
	m := footerModel() // currentWorktree == "/repo"
	m, _ = m.copyFilePrompt("/wt", "dir/f.txt")
	// The absolute row's text is captured in the modal closure; exercise it via
	// copyFileChoice with the same abs the prompt computed.
	if got := m.absFilePath("/wt", "dir/f.txt"); got != "/wt/dir/f.txt" {
		t.Errorf("abs = %q, want /wt/dir/f.txt", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestCopyFileChoice|TestCopyFilePrompt' -v`
Expected: FAIL — `copyFileChoice` takes 2 args, `copyFilePrompt` takes 1 arg; abs option absent.

- [ ] **Step 3: Update `copyFileChoice` and `copyFilePrompt`**

In `internal/tui/clipboard_cmd.go`, replace `copyFileChoice` and `copyFilePrompt`:

```go
// copyFileChoice maps a copy-chooser option to its status line and clipboard
// text. p is the repo-relative path; abs is the absolute path (precomputed by
// copyFilePrompt). ok is false for Cancel or an unknown option.
func copyFileChoice(option, p, abs string) (okMsg, text string, ok bool) {
	switch option {
	case "Copy file path":
		return "Copied path: " + p, p, true
	case "Copy absolute file path":
		return "Copied absolute path: " + abs, abs, true
	case "Copy file name":
		return "Copied file name: " + path.Base(p), path.Base(p), true
	}
	return "", "", false
}

// copyFilePrompt opens the path/name copy chooser for a repo-relative file
// path. base is the worktree the file belongs to (empty → current worktree),
// used to build the absolute-path option. The modal renders above the calling
// popup; Cancel — kept last so esc maps to it — reveals it unchanged.
func (m Model) copyFilePrompt(base, p string) (Model, tea.Cmd) {
	abs := m.absFilePath(base, p)
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "copy-file",
			Prompt:  "Copy — " + p,
			Options: []string{"Copy file path", "Copy absolute file path", "Copy file name", "Cancel"},
		},
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if okMsg, text, ok := copyFileChoice(opt, p, abs); ok {
				return m, m.copyToClipboardCmd(okMsg, text)
			}
			return m, nil
		},
	}
	return m, nil
}
```

- [ ] **Step 4: Update the two switcher call sites**

`internal/tui/bookmark_popup.go:369` — change `return m.copyFilePrompt(b.Path)` to:

```go
			return m.copyFilePrompt(b.Worktree, b.Path)
```

`internal/tui/shelf_popup.go:339` — change `return m.copyFilePrompt(e.Origin.Path)` to:

```go
			return m.copyFilePrompt(e.Origin.Worktree, e.Origin.Path)
```

- [ ] **Step 5: Add switcher coverage for the abs option + base**

Add to `internal/tui/bookmark_popup_test.go`:

```go
func TestBookmarkPopupYChooserHasAbsoluteOnOriginWorktree(t *testing.T) {
	m := bookmarkCopyModel(model.Bookmark{ID: "b1", State: model.StateUnstaged, Worktree: "/wt", Path: "dir/y.go"})
	mm, _ := m.Update(keyMsg("y"))
	m = mm.(Model)
	if m.modal == nil {
		t.Fatal("y should open the copy chooser")
	}
	const want = "Copy file path|Copy absolute file path|Copy file name|Cancel"
	if got := strings.Join(m.modal.req.Options, "|"); got != want {
		t.Errorf("options = %q, want %q", got, want)
	}
	// The absolute option resolves against the bookmark's OWN worktree (/wt),
	// not the current worktree (/repo).
	_, cmd := m.modal.onResolve(m, "Copy absolute file path")
	if cmd == nil {
		t.Fatal("absolute option should return a clipboard cmd")
	}
	if got := m.absFilePath("/wt", "dir/y.go"); got != "/wt/dir/y.go" {
		t.Errorf("abs = %q, want /wt/dir/y.go", got)
	}
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestCopyFile|TestBookmarkPopupY|TestShelfPopup' -v`
Expected: PASS. (`TestClipboardCopiedMsgSetsStatus` uses `clipboardCopiedMsg` directly and is unaffected.)

- [ ] **Step 7: Full package build + test**

Run: `go build ./cmd/gg && go test ./internal/tui/...`
Expected: builds, all pass.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/clipboard_cmd.go internal/tui/clipboard_cmd_test.go internal/tui/bookmark_popup.go internal/tui/shelf_popup.go internal/tui/bookmark_popup_test.go
git commit -m "feat(tui): Copy absolute file path in the g/G copy chooser"
```

---

### Task 4: Docs (help + cheat rows + CHANGELOG)

**Files:**
- Modify: `internal/tui/help.go:255` — copy-summary line.
- Modify: `internal/tui/popup_help.go:41,71` — `y` cheat rows for the switchers.
- Modify: `CHANGELOG.md`.

**Interfaces:** none (documentation only).

- [ ] **Step 1: Update help.go copy summary**

In `internal/tui/help.go`, line 255, change the copy summary to mention the absolute path:

```go
		r("copy", "Copy commit id / commit title (Commits); Copy file path / absolute file path / file name (Files/Staged) — OSC 52"),
```

- [ ] **Step 2: Update popup_help.go `y` cheat rows**

In `internal/tui/popup_help.go`, update both `y` cheat rows (lines 41 and 71) to name the absolute path:

```go
		cheatRow("y", "copy the bookmarked file's path, absolute path, or name to the clipboard (file bookmarks only)"),
```

```go
		cheatRow("y", "copy the file's path, absolute path, or name to the clipboard (file entries only)"),
```

- [ ] **Step 3: Update CHANGELOG.md**

Add an entry under the current unreleased/top section of `CHANGELOG.md` (match the existing bullet style):

```markdown
- **Copy absolute file path** — every "Copy file path" surface (the `.` action
  menu, the fuzzy file finder, and the `y` copy chooser in the `g`/`G`
  bookmark & shelf switchers) now also offers copying the file's absolute
  filesystem path. In the switchers the absolute path is anchored on the
  entry's own origin worktree.
```

- [ ] **Step 4: Verify no test regressions and formatting**

Run: `gofmt -l internal/tui/ && go test ./internal/tui/...`
Expected: `gofmt -l` prints nothing; tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/help.go internal/tui/popup_help.go CHANGELOG.md
git commit -m "docs: Copy absolute file path (help, cheat rows, changelog)"
```

---

### Final verification (after all tasks)

- [ ] Run the full staged suite: `./test.sh` (vet+gofmt → unit → e2e).
- [ ] Run with race before delivering: `./test.sh race`.
- [ ] Build the binary for manual testing: `go build -o ./gg ./cmd/gg`.

## Self-Review

- **Spec coverage:** `.` menu → Task 1; file finder → Task 2; `y` chooser + bookmark/shelf origin-worktree base → Task 3; help/cheat/CHANGELOG → Task 4. All spec sections covered.
- **Placeholder scan:** none — every step has concrete code + commands.
- **Type consistency:** `absFilePath(base, rel string) string` defined in Task 1, consumed unchanged in Tasks 2–3; `copyFileChoice(option, p, abs string)` and `copyFilePrompt(base, p string)` defined and called consistently across Task 3's steps and the two switcher call sites; new row ids (`copy-file-abspath`, `ff-copy-abspath`) used consistently in impl + tests.
