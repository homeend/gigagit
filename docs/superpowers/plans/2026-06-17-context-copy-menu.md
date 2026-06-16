# Context-scoped action menu + clipboard copy actions — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the `.` action menu show only row- then window-scoped actions (excluding whole-app actions), and add three menu-only clipboard copy actions (commit id, file path, file name) via OSC 52.

**Architecture:** A new `bindingScope` field tags every footer binding; the menu builder filters to non-global scopes and orders row-before-window. Menu rows gain an optional `run` handler so copy actions invoke a closure (copying a value captured at menu-build time) instead of replaying a keypress. A new pure `internal/clipboard` package builds the OSC 52 escape; the TUI emits it in a single write to stderr (isatty-guarded) so it can't interleave with the renderer's frames.

**Tech Stack:** Go 1.26, Bubble Tea (value-receiver Model), `github.com/aymanbagabas/go-osc52/v2` (promoted to a direct dep), `github.com/mattn/go-isatty` (promoted to a direct dep).

**Spec:** `docs/superpowers/specs/2026-06-17-context-copy-menu-design.md`

**Working location:** the `feat/context-copy-menu` worktree at `.claude/worktrees/context-copy-menu`. All commands below run from its root. Never edit/commit this feature in the shared checkout.

---

## File structure

| File | Responsibility |
|---|---|
| `internal/clipboard/clipboard.go` | **New.** Pure OSC 52 sequence builder + single-write `Copy` + multiplexer detection. No TUI/git deps. |
| `internal/clipboard/clipboard_test.go` | **New.** Exact-bytes + single-write tests. |
| `internal/tui/footer.go` | Add `bindingScope` type + `scope` field on `footerBinding`; set scope on every binding. |
| `internal/tui/action_menu.go` | `actionRow` gains `copyText`/`run`; `availableActions` becomes context-only, row-before-window; `runVisibleRow` honors `run`; add `contextCopyRows`/`copyRow`. |
| `internal/tui/clipboard_cmd.go` | **New.** `copyToClipboardCmd` + `clipboardCopiedMsg` (keeps the os/isatty wiring out of model.go). |
| `internal/tui/model.go` | Handle `clipboardCopiedMsg` in the Update switch. |
| `internal/tui/action_menu_test.go` | Update tests that assumed globals appear in the menu; add scope/copy tests. |
| `internal/tui/clipboard_cmd_test.go` | **New.** `clipboardCopiedMsg` → statusMsg mapping. |
| `internal/tui/help.go` | Note the `.` menu's context copy actions. |
| `CHANGELOG.md`, `README.md`, `CLAUDE.md` | Docs. |

---

## Task 1: `internal/clipboard` package (pure OSC 52)

**Files:**
- Create: `internal/clipboard/clipboard.go`
- Test: `internal/clipboard/clipboard_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/clipboard/clipboard_test.go`:

```go
package clipboard

import (
	"strings"
	"testing"
)

func TestSequencePlain(t *testing.T) {
	// base64("hello") == "aGVsbG8=", default clipboard buffer 'c', BEL terminator.
	got := Sequence("hello", NoMux)
	want := "\x1b]52;c;aGVsbG8=\x07"
	if got != want {
		t.Errorf("Sequence(plain) = %q, want %q", got, want)
	}
}

func TestSequenceTmuxWrapped(t *testing.T) {
	got := Sequence("hello", Tmux)
	if !strings.HasPrefix(got, "\x1bPtmux;") {
		t.Errorf("tmux sequence must start with the tmux DCS passthrough, got %q", got)
	}
	if !strings.HasSuffix(got, "\x1b\\") {
		t.Errorf("tmux sequence must end with ST (\\x1b\\\\), got %q", got)
	}
}

// countWriter records how many Write calls it received.
type countWriter struct {
	n   int
	buf strings.Builder
}

func (c *countWriter) Write(p []byte) (int, error) {
	c.n++
	return c.buf.Write(p)
}

func TestCopyWritesSequenceInOneWrite(t *testing.T) {
	var w countWriter
	if err := Copy(&w, "hi"); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if w.n != 1 {
		t.Errorf("Copy made %d Write calls, want exactly 1 (contiguous OSC 52)", w.n)
	}
	if got, want := w.buf.String(), Sequence("hi", detectMux()); got != want {
		t.Errorf("Copy wrote %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/clipboard/ -run 'TestSequence|TestCopy' -v`
Expected: build failure / FAIL — `Sequence`, `Copy`, `NoMux`, `Tmux`, `detectMux` are undefined.

- [ ] **Step 3: Implement the package**

Create `internal/clipboard/clipboard.go`:

```go
// Package clipboard writes text to the terminal clipboard using the OSC 52
// escape sequence. It has no TUI or git dependencies so the sequence bytes are
// unit-testable; the caller decides which writer (and tty) to emit to.
package clipboard

import (
	"io"
	"os"
	"strings"

	"github.com/aymanbagabas/go-osc52/v2"
)

// Mux is the terminal multiplexer the sequence must traverse, if any. Plain
// OSC 52 does not reach the outer terminal through tmux/screen without that
// multiplexer's passthrough wrapping.
type Mux int

const (
	NoMux  Mux = iota // direct terminal
	Tmux              // wrap in tmux DCS passthrough (needs `allow-passthrough on`)
	Screen            // wrap in GNU screen DCS passthrough
)

// Sequence builds the OSC 52 clipboard escape for text, wrapped for the given
// multiplexer. It is pure (no env, no I/O) so callers can assert exact bytes.
func Sequence(text string, mux Mux) string {
	s := osc52.New(text)
	switch mux {
	case Tmux:
		s = s.Tmux()
	case Screen:
		s = s.Screen()
	}
	return s.String()
}

// detectMux reads the environment to decide whether a multiplexer wrapper is
// needed. $TMUX is set inside tmux; GNU screen sets $TERM to screen* without
// setting $TMUX.
func detectMux() Mux {
	switch {
	case os.Getenv("TMUX") != "":
		return Tmux
	case strings.HasPrefix(os.Getenv("TERM"), "screen"):
		return Screen
	}
	return NoMux
}

// Copy writes the OSC 52 clipboard sequence for text to w in a SINGLE write.
// One write keeps the escape contiguous on the wire: a TUI renderer writing
// frames to the same tty from another goroutine could otherwise interleave a
// frame inside a split sequence and make the terminal fail to parse it. The
// caller is responsible for passing a tty-backed writer (see the TUI's
// isatty guard).
func Copy(w io.Writer, text string) error {
	_, err := io.WriteString(w, Sequence(text, detectMux()))
	return err
}
```

- [ ] **Step 4: Promote the dependency and run the tests**

Run: `go mod tidy && go test ./internal/clipboard/ -v`
Expected: PASS (3 tests). `go.mod` now lists `github.com/aymanbagabas/go-osc52/v2` as a direct require (the `// indirect` comment is gone).

- [ ] **Step 5: Commit**

```bash
git add internal/clipboard/ go.mod go.sum
git commit -m "feat(clipboard): pure OSC 52 sequence builder + single-write Copy

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: Scope the `.` menu to context-only (row → window)

**Files:**
- Modify: `internal/tui/footer.go` (add `bindingScope`, scope every binding)
- Modify: `internal/tui/action_menu.go:21-38` (`availableActions`)
- Test: `internal/tui/action_menu_test.go`

- [ ] **Step 1: Add the scope type and field, and set scopes**

In `internal/tui/footer.go`, add the type above `footerBinding` and a `scope` field:

```go
// bindingScope tells the . action menu how relevant a binding is. The footer
// shows all available bindings regardless of scope; the menu shows only the
// non-global ones (row first, then window), because global actions belong to
// the whole app and already have their own hotkeys in the footer tail.
type bindingScope int

const (
	scopeGlobal bindingScope = iota // whole-app: footer tail only, NOT in the . menu
	scopeWindow                     // acts on the focused panel / a set of rows
	scopeRow                        // acts on the selected row
)

// footerBinding is one advertised key: a canonical key name (consumed by the
// TestHelpFooterCoverage drift guard), the rendered label, the availability
// predicate, and the scope (see the . menu). The governing rule: the footer
// never shows an unavailable key; it may omit available ones for brevity (W, B,
// shift+tab, pgup/pgdn are usable but documented only in the ? help window). A
// when may therefore be stricter than the Update gate — never looser.
type footerBinding struct {
	id    string // stable action id ("" for pure-navigation keys); see the . menu
	key   string
	label string
	when  func(Model) bool
	scope bindingScope
}
```

Then set `scope:` on every entry. `contextBindings` are all `scopeRow` **except `stash`** which is `scopeWindow`:

```go
var contextBindings = []footerBinding{
	{"switch", "s", "[s]witch", func(m Model) bool { return m.focus == panelBranches && m.canSwitchBranch() }, scopeRow},
	{"branch", "b", "[b]ranch", func(m Model) bool { return m.focus == panelBranches && m.canOpenBranchPopup() }, scopeRow},
	{"worktree", "w", "[w]orktree", func(m Model) bool { return m.focus == panelBranches && m.canOpenWorktreePopup() }, scopeRow},
	{"delete-branch", "d", "[d]elete", func(m Model) bool { return m.focus == panelBranches && m.canDeleteBranch() }, scopeRow},
	{"mark", "m", "[m]ark", func(m Model) bool {
		return m.focus == panelBranches && m.canMark() && !m.markOnFocusedPanel()
	}, scopeRow},
	{"unmark", "m", "[m] unmark", func(m Model) bool {
		return m.focus == panelBranches && m.canMark() && m.markOnFocusedPanel() && m.cursorOnMark()
	}, scopeRow},
	{"pair", "m", "[m] pair", func(m Model) bool {
		return m.focus == panelBranches && m.canMark() && m.markOnFocusedPanel() && !m.cursorOnMark()
	}, scopeRow},
	{"switch-worktree", "enter", "[enter] switch", func(m Model) bool { return m.focus == panelWorktrees && m.canEnterWorktree() }, scopeRow},
	{"delete-worktree", "d", "[d]elete", func(m Model) bool { return m.focus == panelWorktrees && m.canDeleteWorktree() }, scopeRow},
	{"file-diff", "enter", "[enter] diff", func(m Model) bool { return m.canShowFileDiff() }, scopeRow},
	{"stage", "space", "[space] stage", func(m Model) bool { return m.focus == panelFiles && m.canStage() }, scopeRow},
	{"stage-hunks", "H", "[H] hunks", func(m Model) bool { return m.canStageHunks() }, scopeRow},
	{"unstage", "space", "[space] unstage", func(m Model) bool { return m.focus == panelStaged && m.canStage() }, scopeRow},
	{"stash", "s", "[s] stash", func(m Model) bool {
		return m.focus == panelFiles && m.opsIdle() && len(stashCandidates(m.status)) > 0
	}, scopeWindow},
	{"mark-file", "m", "[m] mark", func(m Model) bool { return m.isFilesPanel(m.focus) && m.panelLen(m.focus) > 0 }, scopeRow},
	{"commit-files", "l", "[l] files", func(m Model) bool {
		return m.focus == panelCommits && m.canShowCommitFiles() && !(m.width > 0 && m.width < 40)
	}, scopeRow},
}
```

And `globalBindings` are all `scopeGlobal` (append `, scopeGlobal` to every entry, including the two navigation entries):

```go
var globalBindings = []footerBinding{
	{"resolve", "x", "[x] resolve", func(m Model) bool { return m.opsIdle() && len(m.status.Conflicts()) > 0 }, scopeGlobal},
	{"commit", "c", "[c] commit", Model.canCommit, scopeGlobal},
	{"amend", "C", "[C] amend", Model.canAmend, scopeGlobal},
	{"pull", "p", "[p]ull", Model.opsIdle, scopeGlobal},
	{"push", "P", "[P]ush", func(m Model) bool { return m.opsIdle() && m.status.Branch != "" }, scopeGlobal},
	{"stashes", "S", "[S]tashes", Model.opsIdle, scopeGlobal},
	{"undo", "u", "[u]ndo", Model.opsIdle, scopeGlobal},
	{"order", "o", "[o]rder", Model.opsIdle, scopeGlobal},
	{"view", "z", "[z] view", Model.opsIdle, scopeGlobal},
	{"filter", "/", "[/]filter", Model.opsIdle, scopeGlobal},
	{"repo", "R", "[R]epo", Model.opsIdle, scopeGlobal},
	{"settings", ",", "[,] settings", Model.opsIdle, scopeGlobal},
	{"actions", ".", "[.] actions", Model.opsIdle, scopeGlobal},
	{"", "tab", "[tab] focus", func(Model) bool { return true }, scopeGlobal},
	{"", "ctrl+←/→", "[ctrl+←/→] tab", Model.opsIdle, scopeGlobal},
	{"reload", "r", "[r] reload", func(m Model) bool { return !m.running }, scopeGlobal},
	{"help", "?", "[?] help", func(Model) bool { return true }, scopeGlobal},
	{"quit", "q", "[q] quit", func(Model) bool { return true }, scopeGlobal},
}
```

- [ ] **Step 2: Verify the build still compiles (footer/menu unchanged behavior yet)**

Run: `go build ./internal/tui/`
Expected: PASS — the new field is set everywhere; nothing reads `scope` yet.

- [ ] **Step 3: Write the failing menu-scope tests**

In `internal/tui/action_menu_test.go`, **replace** `TestAvailableActionsExcludesNavAndSelf` (lines 86-107) with the two tests below, and add the `filesMenuModel` helper:

```go
// filesMenuModel is a footerModel focused on the Files panel with one tracked,
// modified, stashable file selected — exercises the row- and window-scoped
// context actions (stage/stage-hunks/file-diff/mark-file are row; stash is
// window).
func filesMenuModel() Model {
	m := footerModel()
	m.loading = false
	m.focus = panelFiles
	m.status.Files = []model.FileStatus{{Path: "dir/f.txt", Kind: model.KindTracked, Staged: '.', Unstaged: 'M'}}
	return m
}

func TestAvailableActionsExcludesGlobals(t *testing.T) {
	m := filesMenuModel()
	ids := map[string]bool{}
	for _, r := range availableActions(m) {
		ids[r.id] = true
	}
	for _, g := range []string{"pull", "repo", "commit", "view", "actions", "quit", "help"} {
		if ids[g] {
			t.Errorf("global action %q must not appear in the . menu", g)
		}
	}
	if !ids["stage"] {
		t.Error("expected the row-scoped stage action in the menu")
	}
}

func TestAvailableActionsRowBeforeWindow(t *testing.T) {
	m := filesMenuModel()
	rows := availableActions(m)
	stageAt, stashAt := -1, -1
	for i, r := range rows {
		switch r.id {
		case "stage":
			stageAt = i
		case "stash":
			stashAt = i
		}
	}
	if stageAt < 0 || stashAt < 0 {
		t.Fatalf("want both stage (row) and stash (window) rows, got %v", rows)
	}
	if stageAt > stashAt {
		t.Errorf("row-scoped stage (%d) must precede window-scoped stash (%d)", stageAt, stashAt)
	}
}
```

- [ ] **Step 4: Run them to verify they fail**

Run: `go test ./internal/tui/ -run 'TestAvailableActions' -v`
Expected: `TestAvailableActionsExcludesGlobals` FAILS (globals still present, since `availableActions` still adds `globalBindings`).

- [ ] **Step 5: Rewrite `availableActions` to be context-only, row before window**

Replace `availableActions` in `internal/tui/action_menu.go` (lines 18-38):

```go
// availableActions returns the currently-available CONTEXT actions as menu
// rows: row-scoped first, then window-scoped, registry order within each group.
// Global (whole-app) actions are excluded — they live in the footer tail and
// have their own hotkeys. Navigation (id == "") is skipped. The dynamic copy
// rows (contextCopyRows) lead the row group.
func availableActions(m Model) []actionRow {
	var row, window []actionRow
	for _, b := range contextBindings {
		if b.id == "" || !b.when(m) {
			continue
		}
		switch b.scope {
		case scopeRow:
			row = append(row, actionRow{id: b.id, key: b.key, label: b.label})
		case scopeWindow:
			window = append(window, actionRow{id: b.id, key: b.key, label: b.label})
		}
	}
	out := append(m.contextCopyRows(), row...)
	return append(out, window...)
}
```

(`contextCopyRows` is added in Task 3; for now add a temporary stub so this compiles and Task 2 stays green:)

```go
// contextCopyRows is fleshed out in Task 3; the empty stub keeps Task 2 green.
func (m Model) contextCopyRows() []actionRow { return nil }
```

- [ ] **Step 6: Update the three tests that assumed globals in the menu**

In `internal/tui/action_menu_test.go`:

1. **Delete** `TestActionMenuRunsPullByKey` (lines 51-64) — pull is global and no longer a menu row; key-replay of a context row is covered by `TestActionMenuRunsStageBySpace`.

2. **Rewrite** `TestMenuActionsAllowlistFiltersAndOrders` (lines 27-39) to use available context ids:

```go
func TestMenuActionsAllowlistFiltersAndOrders(t *testing.T) {
	m := filesMenuModel()
	m.cfg.UI.MenuActions = []string{"file-diff", "stage"}
	mm := m.openActionMenu()
	got := []string{}
	for _, r := range mm.actionMenu.rows {
		got = append(got, r.id)
	}
	if len(got) != 2 || got[0] != "file-diff" || got[1] != "stage" {
		t.Errorf("menu rows = %v, want [file-diff stage] in order", got)
	}
}
```

3. **Rewrite** the assertion in `TestActionMenuRenders` (line 144): `footerModel`'s default focus is `panelBranches` with `main` (IsHead) selected, so `[b]ranch` is available but `[p]ull` is global. Change:

```go
	if !strings.Contains(out, "Actions") || !strings.Contains(out, "[b]ranch") {
		t.Fatalf("rendered menu missing header/rows:\n%s", out)
	}
```

- [ ] **Step 7: Run the full tui test package**

Run: `go test ./internal/tui/ -run 'TestAvailableActions|TestMenuActions|TestActionMenu|TestDot' -v`
Expected: PASS (all menu tests green; `TestActionMenuRunsStageBySpace` still passes — stage is row-scoped).

- [ ] **Step 8: Commit**

```bash
git add internal/tui/footer.go internal/tui/action_menu.go internal/tui/action_menu_test.go
git commit -m "feat(tui): . menu shows context actions only (row before window)

Adds bindingScope; the menu drops whole-app actions and orders row-scoped
before window-scoped. Footer layout unchanged.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3: Menu-only copy actions (commit id / file path / file name)

**Files:**
- Modify: `internal/tui/action_menu.go` (`actionRow` fields, `runVisibleRow`, real `contextCopyRows`, `copyRow`)
- Create: `internal/tui/clipboard_cmd.go` (`copyToClipboardCmd`, `clipboardCopiedMsg`)
- Modify: `internal/tui/model.go` (handle `clipboardCopiedMsg`)
- Test: `internal/tui/action_menu_test.go`, `internal/tui/clipboard_cmd_test.go`

- [ ] **Step 1: Write the failing copy-row tests**

Add to `internal/tui/action_menu_test.go`:

```go
func TestContextCopyRowsCommits(t *testing.T) {
	m := footerModel()
	m.loading = false
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "0123456789abcdef0123456789abcdef01234567", Subject: "x"}}
	rows := m.contextCopyRows()
	if len(rows) != 1 || rows[0].id != "copy-commit-id" {
		t.Fatalf("want one copy-commit-id row, got %v", rows)
	}
	if rows[0].copyText != "0123456789abcdef0123456789abcdef01234567" {
		t.Errorf("copyText = %q, want the full hash", rows[0].copyText)
	}
	if rows[0].run == nil {
		t.Error("copy row must carry a run handler")
	}
}

func TestContextCopyRowsFiles(t *testing.T) {
	m := filesMenuModel() // Files panel, selected "dir/f.txt"
	rows := m.contextCopyRows()
	if len(rows) != 2 {
		t.Fatalf("want path+name copy rows, got %v", rows)
	}
	if rows[0].id != "copy-file-path" || rows[0].copyText != "dir/f.txt" {
		t.Errorf("row[0] = {%q,%q}, want copy-file-path dir/f.txt", rows[0].id, rows[0].copyText)
	}
	if rows[1].id != "copy-file-name" || rows[1].copyText != "f.txt" {
		t.Errorf("row[1] = {%q,%q}, want copy-file-name f.txt", rows[1].id, rows[1].copyText)
	}
}

func TestContextCopyRowsEmpty(t *testing.T) {
	m := footerModel() // default focus panelBranches: no copy rows defined there
	m.loading = false
	if rows := m.contextCopyRows(); len(rows) != 0 {
		t.Errorf("branches panel yields no copy rows, got %v", rows)
	}
}

func TestRunVisibleRowInvokesHandler(t *testing.T) {
	m := filesMenuModel()
	m = m.openActionMenu()
	// The first row is the copy-file-path handler row.
	if m.actionMenu.rows[0].id != "copy-file-path" {
		t.Fatalf("expected copy-file-path to lead the menu, got %q", m.actionMenu.rows[0].id)
	}
	res, cmd := m.runVisibleRow(0)
	if res.(Model).actionMenu != nil {
		t.Error("running a row must close the menu")
	}
	if cmd == nil {
		t.Error("the copy handler must return a clipboard command")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run 'TestContextCopyRows|TestRunVisibleRowInvokesHandler' -v`
Expected: FAIL — `copyText`/`run` fields and the real `contextCopyRows` don't exist yet (stub returns nil).

- [ ] **Step 3: Extend `actionRow` and `runVisibleRow`**

In `internal/tui/action_menu.go`, replace the `actionRow` struct (lines 10-16) and `runVisibleRow` (lines 110-121):

```go
// actionRow is one runnable action in the . menu: its stable id, the key that
// runs it, and the footer-style label. Copy rows instead carry copyText (the
// value placed on the clipboard, resolved at menu-build time) and a run handler
// invoked directly rather than by replaying key.
type actionRow struct {
	id       string
	key      string
	label    string
	copyText string
	run      func(Model) (tea.Model, tea.Cmd)
}
```

```go
// runVisibleRow closes the menu and either invokes the row's direct handler
// (copy actions) or replays its key through Update (every other action, which
// reaches the base-layout handler now that the menu is nil).
func (m Model) runVisibleRow(sel int) (tea.Model, tea.Cmd) {
	vis := m.actionMenu.visible()
	if sel < 0 || sel >= len(vis) {
		m.actionMenu = nil
		return m, nil
	}
	r := vis[sel]
	m.actionMenu = nil
	if r.run != nil {
		return r.run(m)
	}
	return m.Update(synthKey(r.key))
}
```

- [ ] **Step 4: Replace the `contextCopyRows` stub with the real implementation**

In `internal/tui/action_menu.go`, replace the stub with:

```go
// contextCopyRows returns the clipboard copy actions for the current focus and
// selection, with the copied text captured now (the selection is frozen while
// the menu is open). Empty when nothing under the cursor is copyable.
func (m Model) contextCopyRows() []actionRow {
	switch {
	case m.focus == panelCommits:
		if bi, ok := m.backingIndex(panelCommits); ok {
			h := m.commits[bi].Hash
			return []actionRow{m.copyRow("copy-commit-id", "Copy commit id", "Copied commit id "+shortHash(h), h)}
		}
	case m.isFilesPanel(m.focus):
		if bi, ok := m.backingIndex(m.focus); ok {
			p := m.status.Files[bi].Path
			return []actionRow{
				m.copyRow("copy-file-path", "Copy file path", "Copied path: "+p, p),
				m.copyRow("copy-file-name", "Copy file name", "Copied file name: "+path.Base(p), path.Base(p)),
			}
		}
	}
	return nil
}

// copyRow builds a menu-only copy action: its run handler fires the clipboard
// command carrying the pre-resolved success message and text.
func (m Model) copyRow(id, label, okMsg, text string) actionRow {
	return actionRow{
		id:       id,
		label:    label,
		copyText: text,
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.copyToClipboardCmd(okMsg, text)
		},
	}
}
```

Add `"path"` to the import block of `action_menu.go` (it currently imports only `strings`, `tea`, `lipgloss`):

```go
import (
	"path"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)
```

- [ ] **Step 5: Create the clipboard command + message**

Create `internal/tui/clipboard_cmd.go`:

```go
package tui

import (
	"errors"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"

	"github.com/gigagit/gg/internal/clipboard"
)

// clipboardCopiedMsg reports the outcome of a copy action. ok is the success
// status line; err (when non-nil) becomes a "copy failed: …" status.
type clipboardCopiedMsg struct {
	ok  string
	err error
}

var errNoTTY = errors.New("clipboard unavailable (no terminal)")

// copyToClipboardCmd writes text to the clipboard via OSC 52 and reports the
// outcome. It emits to os.Stderr — the same tty as the renderer's stdout but a
// separate stream, so the screen-neutral OSC 52 escape never interleaves inside
// a rendered frame. The isatty guard makes a redirected stderr a no-op instead
// of dumping escape bytes into a file.
func (m Model) copyToClipboardCmd(ok, text string) tea.Cmd {
	return func() tea.Msg {
		if !isatty.IsTerminal(os.Stderr.Fd()) {
			return clipboardCopiedMsg{err: errNoTTY}
		}
		if err := clipboard.Copy(os.Stderr, text); err != nil {
			return clipboardCopiedMsg{err: err}
		}
		return clipboardCopiedMsg{ok: ok}
	}
}
```

- [ ] **Step 6: Handle the message in `model.go`**

In `internal/tui/model.go`, add a case to the Update message switch (next to the other `*Msg` cases, e.g. after the `stageHunksLoadedMsg` case near line 804):

```go
	case clipboardCopiedMsg:
		if msg.err != nil {
			m.statusMsg = "copy failed: " + msg.err.Error()
		} else {
			m.statusMsg = msg.ok
		}
		return m, nil
```

- [ ] **Step 7: Run the copy tests**

Run: `go test ./internal/tui/ -run 'TestContextCopyRows|TestRunVisibleRowInvokesHandler' -v`
Expected: PASS.

- [ ] **Step 8: Write and run the message-mapping test**

Create `internal/tui/clipboard_cmd_test.go`:

```go
package tui

import (
	"errors"
	"testing"
)

func TestClipboardCopiedMsgSetsStatus(t *testing.T) {
	m := footerModel()
	u, _ := m.Update(clipboardCopiedMsg{ok: "Copied path: dir/f.txt"})
	if got := u.(Model).statusMsg; got != "Copied path: dir/f.txt" {
		t.Errorf("statusMsg = %q, want the ok message", got)
	}
}

func TestClipboardCopiedMsgError(t *testing.T) {
	m := footerModel()
	u, _ := m.Update(clipboardCopiedMsg{err: errors.New("boom")})
	if got := u.(Model).statusMsg; got != "copy failed: boom" {
		t.Errorf("statusMsg = %q, want \"copy failed: boom\"", got)
	}
}
```

Run: `go test ./internal/tui/ -run 'TestClipboardCopiedMsg' -v`
Expected: PASS.

- [ ] **Step 9: Promote isatty and run the full tui package**

Run: `go mod tidy && go test ./internal/tui/ ./internal/clipboard/`
Expected: PASS. `go.mod` lists `github.com/mattn/go-isatty` as a direct require.

- [ ] **Step 10: Commit**

```bash
git add internal/tui/action_menu.go internal/tui/clipboard_cmd.go internal/tui/model.go \
        internal/tui/action_menu_test.go internal/tui/clipboard_cmd_test.go go.mod go.sum
git commit -m "feat(tui): . menu copy actions (commit id / file path / file name)

Menu rows can carry a direct run handler; copy rows lead the row group and
write via OSC 52 to a single stderr write (isatty-guarded). Reports outcome
in the status line.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4: Docs

**Files:**
- Modify: `internal/tui/help.go`
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`

- [ ] **Step 1: Note the copy actions in help**

In `internal/tui/help.go`, find the section that documents the `.` action menu (search for `actions` / `.`), and add a line describing the context copy actions. Exact text to add to that section (adjust to the surrounding format):

```
.            action menu — context actions (incl. Copy commit id / file path / file name)
```

If help.go has no `.` line yet, add it under the global keys block. Run the help/footer drift guard to confirm nothing broke:

Run: `go test ./internal/tui/ -run 'TestHelp|TestFooter' -v`
Expected: PASS.

- [ ] **Step 2: Update CHANGELOG**

In `CHANGELOG.md` under `## [Unreleased]`, add to the appropriate subsections:

```markdown
### Added
- TUI `.` action menu: **Copy commit id** (Commits panel), **Copy file path** and **Copy file name** (Files/Staged panels) — written to the clipboard via OSC 52.

### Changed
- TUI `.` action menu now lists only context actions (the selected row's actions first, then panel/window actions); whole-app actions are no longer included (they remain in the footer with their own hotkeys). The `[ui] menu_actions` allowlist now selects/orders among context actions only.
```

- [ ] **Step 3: Update README**

In `README.md`, find the TUI keybindings / action-menu description and update it to state that the `.` menu is context-scoped and offers the copy actions. Add (adjust to surrounding format):

```markdown
- `.` opens the action menu for the current row and panel (it no longer lists whole-app actions). It includes **Copy commit id** on the Commits panel and **Copy file path** / **Copy file name** on the Files/Staged panels (copied to the clipboard via the terminal's OSC 52 sequence).
```

- [ ] **Step 4: Update CLAUDE.md package map**

In `CLAUDE.md`, add a row to the `internal/` package table (alphabetically near `cache`/`buildinfo`):

```markdown
| `clipboard`  | Pure OSC 52 clipboard-sequence builder + single-write `Copy`; no TUI/git deps. Used by the TUI `.` menu copy actions. |
```

- [ ] **Step 5: Commit**

```bash
git add internal/tui/help.go CHANGELOG.md README.md CLAUDE.md
git commit -m "docs: context-scoped . menu + clipboard copy actions

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 5: Full gate + manual verification

- [ ] **Step 1: Run the staged test suite with the race detector**

Run: `./test.sh race`
Expected: vet + gofmt clean, all unit tests pass, e2e passes.

- [ ] **Step 2: Manual clipboard eyeball (cannot be automated)**

Build and run the TUI in a real terminal (Windows Terminal under WSL has OSC 52 enabled by default in recent versions):

Run: `go build ./cmd/gg && ./gg`

- On the Commits panel, press `.`, run **Copy commit id**, paste elsewhere → the full 40-char hash is on the clipboard, status shows `Copied commit id <short>`.
- On the Files panel with a file selected, press `.`, run **Copy file path** → repo-relative path on the clipboard; run **Copy file name** → basename only.
- Confirm whole-app actions (commit/pull/push/view…) no longer appear in the `.` menu, but row/panel actions and the copy actions do.

- [ ] **Step 3: Finish the branch**

Use the **superpowers:finishing-a-development-branch** skill: verify tests pass, then present the merge/PR/keep/discard options. The repo convention is an in-place fast-forward merge onto `main` from the shared checkout (rebase onto current `main` first if it has moved), then remove the worktree and delete the branch.

---

## Self-review notes (author)

- **Spec coverage:** Part A → Task 2; Part B → Task 3; `internal/clipboard` (single-write/stderr/isatty/tmux) → Task 1 + Task 3 Step 5; docs → Task 4; manual eyeball + gate → Task 5. All spec sections mapped.
- **Behavior change documented:** `menu_actions` allowlist now context-only — CHANGELOG Step 2.
- **Refinement vs spec:** the success status line is built at row-build time and carried on `clipboardCopiedMsg.ok` (instead of switching on the label in Update) — same observable behavior, less coupling.
- **Type consistency:** `actionRow{id,key,label,copyText,run}`, `clipboardCopiedMsg{ok,err}`, `clipboard.Sequence(text, Mux)`, `clipboard.Copy(io.Writer, text)`, `Mux{NoMux,Tmux,Screen}` — used consistently across tasks.
- **Build stays green per task:** Task 2 adds a `contextCopyRows` stub so it compiles before Task 3 fills it in.
```
