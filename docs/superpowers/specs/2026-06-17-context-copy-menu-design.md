# Context-scoped action menu + clipboard copy actions — Design

**Date:** 2026-06-17
**Status:** Approved (brainstorm)

## Problem

Two related gaps in the `.` action menu:

1. **The menu is not contextual.** `availableActions` lists *every* available
   binding — panel/row actions **and** whole-app actions (commit, pull, push,
   undo, view, order, filter, settings, repo, reload, resolve, help…). A
   "context menu" should offer what applies to the thing under the cursor and
   its panel, not the whole app (those already live in the footer tail and have
   their own hotkeys).

2. **There is no way to copy identifiers to the clipboard.** A user looking at
   a commit or a file has no quick path to copy the commit id, the file path,
   or the file name.

## Goals

- The `.` menu shows only **row-scoped** then **window-scoped** actions;
  whole-app (global) actions are excluded.
- Add three menu-only copy actions:
  - Commits panel → **Copy commit id** (the full hash).
  - Files / Staged panel → **Copy file path** (repo-relative) and
    **Copy file name** (basename).
- Copies go to the clipboard via the terminal's **OSC 52** escape sequence
  (cross-platform, no external binary, works over SSH/WSL/tmux when the terminal
  supports it).

## Non-goals (YAGNI)

- No new top-level hotkeys for the copy actions — they are menu-only.
- No copy of short hash, author, subject, branch name, or worktree path (can be
  added later under the same mechanism if asked).
- No clipboard *read*.
- No change to the footer layout or to any global binding's behavior.

---

## Part A — Scope the menu to context only

### Binding scope

Add a scope to `footerBinding` (`internal/tui/footer.go`):

```go
type bindingScope int

const (
	scopeGlobal bindingScope = iota // whole-app: footer tail only, NOT in the . menu
	scopeWindow                     // acts on the focused panel / a set of rows
	scopeRow                        // acts on the selected row
)
```

Set `scope` on every existing binding:

- **`globalBindings`** → all `scopeGlobal` (resolve, commit, amend, pull, push,
  stashes, undo, order, view, filter, repo, settings, actions, reload, help,
  quit, and the two navigation entries).
- **`contextBindings`** → `scopeRow`, **except `stash`** (the `s`-on-Files
  binding stashes the whole tracked-changes set, not the cursor row) →
  `scopeWindow`.

The zero value is `scopeGlobal`, so any binding that forgets to set a scope
defaults to "not in the menu" — the safe direction.

### Menu assembly

`availableActions(m)` changes from "context bindings then global bindings" to
**context-only, row before window**:

1. Walk `contextBindings` (skip `id == ""`); collect those with
   `scope == scopeRow` whose `when(m)` is true.
2. Then collect those with `scope == scopeWindow` whose `when(m)` is true.
3. Append the dynamic copy rows (Part B) — these are `scopeRow`, so they sort
   into the row group (see Part B for exact ordering).
4. `globalBindings` are **not** consulted.

The existing skips (`id == ""`, `"actions"`, `"quit"`) become moot for
the registry walk because those are all global now, but keep the guards
harmless.

`footerLine` (the footer) is **unchanged** — it still renders
`contextBindings` then the predicated global tail.

### `menu_actions` allowlist interaction

`openActionMenu` still narrows/orders by `m.cfg.UI.MenuActions` when set. Since
`availableActions` now yields only context actions, a global id listed in the
allowlist matches nothing and silently drops. This is a deliberate behavior
change — document it in CHANGELOG and the config docs: the menu allowlist
selects/orders among context actions only.

---

## Part B — Menu-only copy actions

### Row handler

The menu runs a row by replaying its key (`runVisibleRow` → `synthKey` →
`Update`). Let a row instead carry an optional handler and a captured payload:

```go
type actionRow struct {
	id    string
	key   string
	label string

	// copyText is the value a copy row places on the clipboard, resolved at
	// menu-build time (selection is frozen while the menu is open). Empty for
	// ordinary key-replay rows. Exposed for tests.
	copyText string
	// run, when non-nil, is invoked directly instead of replaying key. Copy
	// rows set it to a closure that copies copyText and reports via statusMsg.
	run func(Model) (tea.Model, tea.Cmd)
}
```

`runVisibleRow`:

```go
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

### Building the copy rows

A new helper resolves the rows from focus + selection, capturing the text **at
build time** so the handler is a pure copy of a frozen value:

```go
// contextCopyRows returns the clipboard copy actions for the current focus and
// selection. Empty when nothing under the cursor is copyable.
func (m Model) contextCopyRows() []actionRow {
	var out []actionRow
	switch {
	case m.focus == panelCommits:
		if bi, ok := m.backingIndex(panelCommits); ok {
			hash := m.commits[bi].Hash
			out = append(out, m.copyRow("copy-commit-id", "Copy commit id", hash))
		}
	case m.isFilesPanel(m.focus):
		if bi, ok := m.backingIndex(m.focus); ok {
			p := m.status.Files[bi].Path
			out = append(out,
				m.copyRow("copy-file-path", "Copy file path", p),
				m.copyRow("copy-file-name", "Copy file name", path.Base(p)),
			)
		}
	}
	return out
}

func (m Model) copyRow(id, label, text string) actionRow {
	return actionRow{
		id:       id,
		label:    label,
		copyText: text,
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.copyToClipboardCmd(label, text)
		},
	}
}
```

`m.isFilesPanel` already covers both `panelFiles` and `panelStaged`, so copy
path/name work in both file panels. `backingIndex` returns `ok == false` on an
empty list, so empty panels yield no copy rows.

`availableActions` appends `m.contextCopyRows()` **at the front of the row
group** (before the registry's row-scoped bindings), so copy actions lead the
menu — they are the most "this exact thing" of the row actions.

### The clipboard command + status feedback

```go
// copyToClipboardCmd writes text to the clipboard via OSC 52 and reports the
// outcome as a clipboardCopiedMsg (sets statusMsg).
func (m Model) copyToClipboardCmd(label, text string) tea.Cmd {
	return func() tea.Msg {
		err := clipboard.Copy(os.Stderr, text)
		return clipboardCopiedMsg{label: label, text: text, err: err}
	}
}

type clipboardCopiedMsg struct {
	label string
	text  string
	err   error
}
```

`Update` handles `clipboardCopiedMsg`:
- `err != nil` → `statusMsg = "copy failed: " + err.Error()`.
- "Copy commit id" → `statusMsg = "Copied commit id " + shortHash(text)`.
- "Copy file path" → `statusMsg = "Copied path: " + text`.
- "Copy file name" → `statusMsg = "Copied file name: " + text`.

(The status line clears on the next idle key — the recent statusMsg fix.)

---

## The `internal/clipboard` package

A small, pure-as-possible package so the escape bytes are unit-testable and the
TUI stays thin.

```go
package clipboard

import (
	"io"
	"os"
	"strings"

	"github.com/aymanbagabas/go-osc52/v2"
)

// Sequence returns the OSC 52 clipboard escape for text. When tmux is true it
// is wrapped in tmux's DCS passthrough so it reaches the outer terminal.
func Sequence(text string, tmux bool) string {
	s := osc52.New(text)
	if tmux {
		s = s.Tmux()
	}
	return s.String()
}

// Copy writes the OSC 52 clipboard sequence for text to w in a SINGLE Write.
// A single write keeps the sequence contiguous: bubbletea's renderer writes
// frames to the same tty from another goroutine, and a sequence split across
// writes could interleave with a frame and fail to parse. tmux is detected
// from the environment.
func Copy(w io.Writer, text string) error {
	_, err := io.WriteString(w, Sequence(text, inTmux()))
	return err
}

func inTmux() bool {
	return os.Getenv("TMUX") != "" || strings.HasPrefix(os.Getenv("TERM"), "screen")
}
```

### Why stderr, single write, isatty

These three points are the part a once-through manual eyeball will *not* catch
(an interleaving race fails intermittently), so they are pinned here:

1. **Single `Write`.** Build the whole sequence as one string and emit it with
   one `io.WriteString`. One syscall is contiguous on the wire and cannot be
   split by a concurrent frame write.
2. **Write to stderr, not stdout.** bubbletea's renderer owns `os.Stdout`
   (alt-screen frames). OSC 52 changes no screen state, and terminals parse
   escape sequences from whatever is written to the tty device — so writing to
   `os.Stderr` (the same tty, a separate stream) reaches the terminal's
   clipboard without ever interleaving inside a rendered frame.
3. **isatty guard at the call site.** The TUI command checks
   `isatty.IsTerminal(os.Stderr.Fd())` (`github.com/mattn/go-isatty`, already in
   the dependency tree — promoted from indirect) before calling
   `clipboard.Copy`; when stderr is redirected to a file the copy is a no-op
   (reported as "clipboard unavailable") rather than dumping escape bytes into
   the file.
4. **tmux/screen passthrough** via `go-osc52`'s `.Tmux()` so the sequence
   reaches the outer terminal under a multiplexer.

`go-osc52` is currently an indirect dependency; importing it directly promotes
it to a direct require (via `go mod tidy`).

### Manual verification (cannot be e2e-tested)

The cross-layer TUI→terminal copy has no automated test (`tui` can't import
`git`; the terminal isn't in the loop under test). Verify by hand in Windows
Terminal (WSL): open the `.` menu on a commit, run Copy commit id, paste
elsewhere, confirm the full hash landed. Repeat for file path/name on the Files
panel.

---

## Testing

**`internal/clipboard` (pure, fully testable):**
- `TestSequencePlain`: `Sequence("hello", false)` equals
  `"\x1b]52;c;aGVsbG8=\x07"` (base64 of "hello").
- `TestSequenceTmux`: `Sequence("hello", true)` is wrapped in tmux DCS
  passthrough (starts with `\x1bPtmux;` … ends with `\x1b\\`).
- `TestCopyWritesSequenceOnce`: `Copy(&buf, "hi")` writes exactly
  `Sequence("hi", inTmux())` to the buffer.

**`internal/tui`:**
- `TestAvailableActionsExcludesGlobals`: with a model where global predicates
  are true, the menu rows contain no global id (e.g. no `commit`, `pull`,
  `view`) and only context ids.
- `TestAvailableActionsRowBeforeWindow`: on the Files panel with stashable
  changes, row-scoped ids (incl. the copy rows) precede the window-scoped
  `stash` row.
- `TestContextCopyRowsCommits`: commits panel with a selected commit yields a
  single `copy-commit-id` row whose `copyText` is the full hash.
- `TestContextCopyRowsFiles`: Files panel with a selected file yields
  `copy-file-path` (== `f.Path`) and `copy-file-name` (== basename) rows.
- `TestContextCopyRowsEmpty`: empty panel / non-copyable focus yields no copy
  rows.
- `TestRunVisibleRowInvokesHandler`: a row with a `run` handler is invoked by
  `runVisibleRow` (handler returns a recognizable Cmd / the menu closes).
- `TestClipboardCopiedMsgSetsStatus`: each label maps to the right `statusMsg`;
  an error sets "copy failed: …".
- **Update existing `action_menu_test.go`**: tests that asserted global rows
  appear in the menu must change to reflect context-only contents. Budget for
  edits, not just additions.

---

## File structure

| File | Change |
|---|---|
| `internal/clipboard/clipboard.go` | **New.** `Sequence`, `Copy`, `inTmux`. |
| `internal/clipboard/clipboard_test.go` | **New.** Sequence/Copy tests. |
| `internal/tui/footer.go` | Add `bindingScope` type + `scope` field; set scope on every binding. |
| `internal/tui/action_menu.go` | `actionRow` gains `copyText`/`run`; `availableActions` context-only + row-before-window + copy rows; `runVisibleRow` calls `run`; add `contextCopyRows`, `copyRow`. |
| `internal/tui/op.go` (or a new `clipboard_cmd.go`) | `copyToClipboardCmd`, `clipboardCopiedMsg`. |
| `internal/tui/model.go` | Handle `clipboardCopiedMsg`; isatty guard before copy. |
| `internal/tui/help.go` | Note the `.` menu's context copy actions. |
| `internal/tui/*_test.go` | New tui tests; update `action_menu_test.go`. |
| `CHANGELOG.md` | Always. |
| `README.md` | Menu-is-contextual + copy actions. |
| `CLAUDE.md` | Add the `clipboard` package to the package map. |

No CLI surface change → no `using-gg.md` / `agentskill.Version` bump.
```
