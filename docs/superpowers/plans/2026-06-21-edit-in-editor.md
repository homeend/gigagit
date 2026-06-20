# "Edit in editor" Files-panel Action — Implementation Plan

> **For agentic workers:** TDD — failing test → run-fails → minimal code →
> run-passes → commit. Steps use `- [ ]`.

**Goal:** A `.`-menu action on the selected Files-panel file that suspends the
TUI, opens `$VISUAL`/`$EDITOR` (fallback vi/notepad), and refreshes status on
return.

**Architecture:** Pure TUI — no engine/domain/git/CLI. `tea.ExecProcess`
suspends the program; pure `resolveEditor`/`editorCommand` helpers carry the
testable logic; the post-edit refresh reuses `statusRefreshedMsg`.

## Global Constraints

- TUI-only; `internal/tui` may import `os`/`os/exec` (not `internal/git`).
- Editor: `$VISUAL` → `$EDITOR` → `vi` (unix) / `notepad` (Windows).
- Files panel only, menu-only (no keybind).
- Absolute path = `filepath.Join(m.currentWorktree, relPath)`; `cmd.Dir` = the
  worktree root.
- Commit trailers: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` +
  the `Claude-Session:` line.

---

### Task 1: Editor resolution + command helpers (pure)

**Files:** Create `internal/tui/edit_actions.go`; Test `internal/tui/edit_actions_test.go`.

**Produces:** `resolveEditor() string`, `defaultEditor() string`,
`editorCommand(editor, absPath string) *exec.Cmd`.

- [ ] **Step 1: Failing tests:**

```go
func TestResolveEditorPrecedence(t *testing.T) {
	t.Setenv("VISUAL", "myvis")
	t.Setenv("EDITOR", "myed")
	if got := resolveEditor(); got != "myvis" {
		t.Fatalf("VISUAL should win, got %q", got)
	}
	t.Setenv("VISUAL", "")
	if got := resolveEditor(); got != "myed" {
		t.Fatalf("EDITOR fallback, got %q", got)
	}
	t.Setenv("EDITOR", "")
	if got := resolveEditor(); got != defaultEditor() {
		t.Fatalf("default fallback, got %q", got)
	}
}

func TestDefaultEditorNonEmpty(t *testing.T) {
	if defaultEditor() == "" {
		t.Fatal("default editor must be non-empty")
	}
}

func TestEditorCommandArgv(t *testing.T) {
	cmd := editorCommand("code -w", "/wt/a/b.go")
	want := []string{"code", "-w", "/wt/a/b.go"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("args = %v, want %v", cmd.Args, want)
	}
	cmd2 := editorCommand("vim", "/wt/x")
	if !reflect.DeepEqual(cmd2.Args, []string{"vim", "/wt/x"}) {
		t.Fatalf("args = %v", cmd2.Args)
	}
}
```

- [ ] **Step 2: Run — fail.** `go test ./internal/tui/ -run 'TestResolveEditor|TestDefaultEditor|TestEditorCommand'` → undefined.

- [ ] **Step 3: Implement** in `edit_actions.go`:

```go
package tui

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func defaultEditor() string {
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	return "vi"
}

// resolveEditor picks the editor: $VISUAL, then $EDITOR, then a platform default.
func resolveEditor() string {
	if e := os.Getenv("VISUAL"); e != "" {
		return e
	}
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	return defaultEditor()
}

// editorCommand builds the editor invocation: the editor string is split on
// whitespace (binary + leading flags) and absPath is appended. No shell-quote
// parsing (v1) — sufficient for "vim", "code -w", "emacs -nw", etc.
func editorCommand(editor, absPath string) *exec.Cmd {
	fields := strings.Fields(editor)
	args := append(fields[1:], absPath)
	return exec.Command(fields[0], args...)
}
```

- [ ] **Step 4: Run — pass.** Same command → PASS.

- [ ] **Step 5: Commit.** `feat(tui): editor resolution + command helpers`

---

### Task 2: editFileCmd + editorFinishedMsg + reloadStatusCmd + row + wiring

**Files:** Modify `internal/tui/edit_actions.go`, `internal/tui/op.go`,
`internal/tui/action_menu.go`, `internal/tui/model.go`;
Test `internal/tui/edit_actions_test.go`.

**Consumes:** Task 1 helpers; `actionRow`; `m.backingIndex`; `m.status.Files`;
`m.currentWorktree`; `statusRefreshedMsg` (model.go:917); `availableActions`.
**Produces:** `func (m Model) fileEditRow() (actionRow, bool)`,
`func (m Model) editFileCmd(rel string) tea.Cmd`,
`func (m Model) reloadStatusCmd(summary string) tea.Cmd`,
`type editorFinishedMsg struct{ path string; err error }`.

- [ ] **Step 1: Failing tests** (append to `edit_actions_test.go`). Reuse the
  Files-panel fixture pattern (see `ignoreModel`/`discardModel`): a sized, idle
  Model with `m.focus = panelFiles`, `m.status.Files` populated, `m.sel`.

```go
func editModel(files []model.FileStatus, sel int) Model {
	m := New(nil)
	m.width, m.height = 80, 30
	m.loading = false
	m.focus = panelFiles
	m.currentWorktree = "/wt"
	m.status = model.WorkingTreeStatus{Files: files}
	m.sel[panelFiles] = sel
	return m
}

func TestFileEditRowGating(t *testing.T) {
	f := model.FileStatus{Path: "a.go", Kind: model.KindTracked, Unstaged: 'M'}
	m := editModel([]model.FileStatus{f}, 0)
	r, ok := m.fileEditRow()
	if !ok || r.id != "edit-file" || r.label != "Edit in editor" {
		t.Fatalf("want edit row, got %+v ok=%v", r, ok)
	}
	// In availableActions.
	var found bool
	for _, a := range availableActions(m) {
		if a.id == "edit-file" {
			found = true
		}
	}
	if !found {
		t.Fatal("availableActions missing edit-file")
	}
	// Staged panel / running → absent.
	m.focus = panelStaged
	if _, ok := m.fileEditRow(); ok {
		t.Fatal("no edit row on Staged panel")
	}
	m.focus = panelFiles
	m.running = true
	if _, ok := m.fileEditRow(); ok {
		t.Fatal("no edit row while running")
	}
}

func TestEditorFinishedMsgRefreshes(t *testing.T) {
	m := editModel([]model.FileStatus{{Path: "a.go", Kind: model.KindTracked}}, 0)
	// Error path sets the edit: status and still returns a refresh cmd.
	m2, cmd := m.Update(editorFinishedMsg{path: "a.go", err: errors.New("boom")})
	if cmd == nil {
		t.Fatal("want a refresh cmd")
	}
	if !strings.Contains(m2.(Model).statusMsg, "boom") {
		t.Fatalf("status = %q", m2.(Model).statusMsg)
	}
	// Success path returns a refresh cmd.
	_, cmd2 := m.Update(editorFinishedMsg{path: "a.go"})
	if cmd2 == nil {
		t.Fatal("want a refresh cmd on success")
	}
}
```

- [ ] **Step 2: Run — fail.** `go test ./internal/tui/ -run 'TestFileEditRow|TestEditorFinished'` → undefined.

- [ ] **Step 3a: Implement the row + editFileCmd** in `edit_actions.go`:

```go
// fileEditRow offers "Edit in editor" on the selected Files-panel file.
func (m Model) fileEditRow() (actionRow, bool) {
	if m.focus != panelFiles || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelFiles)
	if !ok {
		return actionRow{}, false
	}
	p := m.status.Files[bi].Path
	return actionRow{
		id:    "edit-file",
		label: "Edit in editor",
		run:   func(m Model) (tea.Model, tea.Cmd) { return m, m.editFileCmd(p) },
	}, true
}

// editFileCmd suspends the TUI and opens rel in the user's editor; on exit it
// yields an editorFinishedMsg.
func (m Model) editFileCmd(rel string) tea.Cmd {
	abs := filepath.Join(m.currentWorktree, rel)
	cmd := editorCommand(resolveEditor(), abs)
	cmd.Dir = m.currentWorktree
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorFinishedMsg{path: rel, err: err}
	})
}
```

- [ ] **Step 3b: Add the msg + reload cmd** to `op.go`:

```go
// editorFinishedMsg signals the external editor exited (path is the edited
// repo-relative path).
type editorFinishedMsg struct {
	path string
	err  error
}

// reloadStatusCmd re-reads only the working-tree status off the UI thread.
func (m Model) reloadStatusCmd(summary string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		st, err := svc.Status(context.Background())
		return statusRefreshedMsg{summary: summary, status: st, err: err}
	}
}
```

- [ ] **Step 3c: Handle the msg** in `model.go` Update (near the other op
  message cases, e.g. after `statusRefreshedMsg`):

```go
case editorFinishedMsg:
	if msg.err != nil {
		m.statusMsg = "edit: " + msg.err.Error()
		return m, m.reloadStatusCmd("")
	}
	return m, m.reloadStatusCmd("edited " + path.Base(msg.path))
```

  (Ensure `path` is imported in model.go; if not, use the already-imported
  basename helper or add the import.)

- [ ] **Step 3d: Wire the row** into `availableActions` (action_menu.go),
  alongside `fileIgnoreRow`:

```go
	if r, ok := m.fileEditRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 4: Run — pass.** `go test ./internal/tui/ -run 'TestFileEditRow|TestEditorFinished'` → PASS; then full `go test ./internal/tui/`.

- [ ] **Step 5: Commit.** `feat(tui): Edit in editor action on Files-panel files`

---

### Task 3: Docs

**Files:** Modify `internal/tui/help.go`, `CHANGELOG.md`.

- [ ] **Step 1: help.go** — add a Files-panel `.`-menu entry:

```go
	r(".", "Edit in editor: open the selected file in $VISUAL/$EDITOR (vi/notepad fallback), then refresh"),
```
  (Place near the other Files-panel `.`-menu entries; if a help test asserts
  text, update it.)

- [ ] **Step 2: CHANGELOG** — `### Added` bullet: Edit in editor on the Files
  panel via the `.` menu; suspends the TUI, opens `$VISUAL`/`$EDITOR`
  (vi/notepad fallback), refreshes status on return; TUI-only.

- [ ] **Step 3: Run help tests.** `go test ./internal/tui/ -run TestHelp` → PASS.

- [ ] **Step 4: Commit.** `docs: changelog + help for Edit in editor action`

---

## Final verification

- [ ] `./test.sh race` green.
- [ ] `gofmt -l internal/ | head` empty.
- [ ] Manual smoke (optional): in a real repo, `.` → Edit in editor on a file
  opens `$EDITOR` and returns.
- [ ] Merge to main; verify on the merged tree; clean up worktree; update memory.

## Self-review notes

- Spec coverage: resolution+command (T1), row+suspend+refresh (T2), docs (T3).
- Type consistency: `resolveEditor`/`defaultEditor`/`editorCommand`/`fileEditRow`/
  `editFileCmd`/`reloadStatusCmd`/`editorFinishedMsg` identical across tasks.
- `ExecProcess` not unit-tested (suspends program); helpers + handler are.
