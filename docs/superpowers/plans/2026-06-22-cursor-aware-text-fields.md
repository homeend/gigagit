# Cursor-Aware Text Fields Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every true editable TUI field a visible cursor with word-jump line editing, via one shared pure `textfield` value type.

**Architecture:** A new pure value type `internal/tui/textfield.go` holds a `[]rune` buffer + a `cursor` rune-index. It owns ONLY editing keys through `HandleEditKey(tea.KeyMsg) bool` (returns false for anything the popup must keep: Enter/Tab/Esc/Up/Down/Ctrl+S). `\n` is an ordinary rune, so the same type backs single-line fields and the multi-line commit description. Each editable popup swaps its bare `string` for a `textfield` and renders it via `View(focused)`.

**Tech Stack:** Go 1.26, Bubble Tea, lipgloss (existing). No new dependencies.

## Global Constraints

- Module `github.com/gigagit/gg`, Go 1.26. `internal/tui` must NOT import `internal/git` (archtest-guarded) — this work touches only `internal/tui` + docs.
- TDD: write the failing test first, watch it fail, implement minimally, watch it pass, commit. Tests are pure UI — no real git needed.
- `TUI Model` is a value receiver; popups are pointer-receiver structs reached via the layer stack. `textfield` methods are pointer-receiver and mutate in place.
- Commit message footer (every commit):
  ```
  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_018bxAFvuaSjJw7NfpJe1rsj
  ```
- Run `./test.sh` (or `go test ./internal/tui/...`) per task; `./test.sh race` before the final commit.
- **Name fields suppress spaces** (branch/rename-branch/tag-checkout/tag-name): these popups must intercept `tea.KeySpace` and drop it BEFORE delegating to `HandleEditKey`, preserving today's "branch names cannot contain spaces" guard. Space-allowing fields: commit title/desc, tag message, stash name, shelf dest, worktree branch + path fields.
- Quick-switcher FILTER inputs are OUT OF SCOPE and stay append-only: `bookmark_popup.go` `filter`, `repo_popup.go` `query`, `content_popup.go` `query`.

---

### Task 1: The `textfield` core type

**Files:**
- Create: `internal/tui/textfield.go`
- Test: `internal/tui/textfield_test.go`

**Interfaces:**
- Produces (consumed by all later tasks):
  - `type textfield struct { runes []rune; cursor int }`
  - `func newTextField(s string) textfield` — buffer = s, cursor at end
  - `func (f *textfield) Value() string`
  - `func (f *textfield) SetValue(s string)` — replace buffer, cursor → end
  - `func (f *textfield) HandleEditKey(msg tea.KeyMsg) bool` — true if consumed
  - `func (f *textfield) InsertNewline()` (Task 2)
  - `func (f *textfield) Up()` / `func (f *textfield) Down()` (Task 2)
  - `func (f *textfield) View(focused bool) string` (Task 3)

- [ ] **Step 1: Write the failing test**

Create `internal/tui/textfield_test.go`:

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// runesMsg builds a KeyRunes message for the given string.
func runesMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func keyMsg(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func TestTextFieldInsertAndValue(t *testing.T) {
	var f textfield
	f.HandleEditKey(runesMsg("abc"))
	if f.Value() != "abc" {
		t.Fatalf("Value = %q, want abc", f.Value())
	}
	if f.cursor != 3 {
		t.Fatalf("cursor = %d, want 3", f.cursor)
	}
}

func TestTextFieldInsertMidBuffer(t *testing.T) {
	f := newTextField("ac")
	f.HandleEditKey(keyMsg(tea.KeyLeft)) // cursor: 2 -> 1
	f.HandleEditKey(runesMsg("b"))       // insert at 1
	if f.Value() != "abc" {
		t.Fatalf("Value = %q, want abc", f.Value())
	}
	if f.cursor != 2 {
		t.Fatalf("cursor = %d, want 2", f.cursor)
	}
}

func TestTextFieldBackspaceAndDelete(t *testing.T) {
	f := newTextField("abc")
	f.HandleEditKey(keyMsg(tea.KeyBackspace)) // "ab", cursor 2
	if f.Value() != "ab" {
		t.Fatalf("after backspace Value = %q, want ab", f.Value())
	}
	f.HandleEditKey(keyMsg(tea.KeyHome))   // cursor 0
	f.HandleEditKey(keyMsg(tea.KeyDelete)) // delete 'a' -> "b"
	if f.Value() != "b" {
		t.Fatalf("after delete Value = %q, want b", f.Value())
	}
}

func TestTextFieldArrowClamp(t *testing.T) {
	f := newTextField("ab")
	for i := 0; i < 5; i++ {
		f.HandleEditKey(keyMsg(tea.KeyLeft))
	}
	if f.cursor != 0 {
		t.Fatalf("cursor = %d, want clamp at 0", f.cursor)
	}
	for i := 0; i < 5; i++ {
		f.HandleEditKey(keyMsg(tea.KeyRight))
	}
	if f.cursor != 2 {
		t.Fatalf("cursor = %d, want clamp at 2", f.cursor)
	}
}

func TestTextFieldHomeEndSpace(t *testing.T) {
	f := newTextField("hi")
	f.HandleEditKey(keyMsg(tea.KeyHome))
	if f.cursor != 0 {
		t.Fatalf("home cursor = %d, want 0", f.cursor)
	}
	f.HandleEditKey(keyMsg(tea.KeyEnd))
	if f.cursor != 2 {
		t.Fatalf("end cursor = %d, want 2", f.cursor)
	}
	f.HandleEditKey(keyMsg(tea.KeySpace))
	if f.Value() != "hi " {
		t.Fatalf("Value = %q, want 'hi '", f.Value())
	}
}

func TestTextFieldWordJumpsAndDeleteWord(t *testing.T) {
	f := newTextField("foo bar baz") // cursor 11
	f.HandleEditKey(keyMsg(tea.KeyCtrlLeft))
	if f.cursor != 8 { // start of "baz"
		t.Fatalf("word-left cursor = %d, want 8", f.cursor)
	}
	f.HandleEditKey(tea.KeyMsg{Type: tea.KeyLeft, Alt: true}) // alt+left = word-left
	if f.cursor != 4 { // start of "bar"
		t.Fatalf("alt word-left cursor = %d, want 4", f.cursor)
	}
	f.HandleEditKey(keyMsg(tea.KeyCtrlW)) // delete "bar " back to "foo "? no: deletes word before cursor = "foo"
	if f.Value() != "bar baz" {
		t.Fatalf("after ctrl+w Value = %q, want 'bar baz'", f.Value())
	}
}

func TestTextFieldWordRight(t *testing.T) {
	f := newTextField("foo bar")
	f.HandleEditKey(keyMsg(tea.KeyHome))
	f.HandleEditKey(keyMsg(tea.KeyCtrlRight))
	if f.cursor != 3 { // end of "foo"
		t.Fatalf("word-right cursor = %d, want 3", f.cursor)
	}
}

func TestTextFieldSetValue(t *testing.T) {
	var f textfield
	f.SetValue("hello")
	if f.Value() != "hello" || f.cursor != 5 {
		t.Fatalf("SetValue Value=%q cursor=%d, want hello/5", f.Value(), f.cursor)
	}
}

func TestTextFieldHandleEditKeyReturnsFalse(t *testing.T) {
	var f textfield
	for _, kt := range []tea.KeyType{tea.KeyEnter, tea.KeyTab, tea.KeyEsc, tea.KeyUp, tea.KeyDown, tea.KeyCtrlS} {
		if f.HandleEditKey(keyMsg(kt)) {
			t.Fatalf("HandleEditKey consumed %v, want false", kt)
		}
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/tui/ -run TestTextField -v`
Expected: FAIL — `undefined: textfield` / `newTextField`.

- [ ] **Step 3: Write the implementation**

Create `internal/tui/textfield.go`:

```go
package tui

import (
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// textfield is a cursor-aware editable text buffer shared by the editable
// popups. cursor is a rune index in [0, len(runes)]. A '\n' is an ordinary
// rune, so the same type backs single-line fields and the multi-line commit
// description. The quick-switcher FILTER inputs deliberately do NOT use it.
type textfield struct {
	runes  []rune
	cursor int
}

// newTextField makes a field pre-filled with s, cursor at the end.
func newTextField(s string) textfield {
	r := []rune(s)
	return textfield{runes: r, cursor: len(r)}
}

// Value returns the buffer as a string.
func (f *textfield) Value() string { return string(f.runes) }

// SetValue replaces the buffer and puts the cursor at the end.
func (f *textfield) SetValue(s string) {
	f.runes = []rune(s)
	f.cursor = len(f.runes)
}

// insert puts rs at the cursor and advances past them.
func (f *textfield) insert(rs []rune) {
	out := make([]rune, 0, len(f.runes)+len(rs))
	out = append(out, f.runes[:f.cursor]...)
	out = append(out, rs...)
	out = append(out, f.runes[f.cursor:]...)
	f.runes = out
	f.cursor += len(rs)
}

func (f *textfield) backspace() {
	if f.cursor == 0 {
		return
	}
	f.runes = append(f.runes[:f.cursor-1], f.runes[f.cursor:]...)
	f.cursor--
}

func (f *textfield) deleteFwd() {
	if f.cursor >= len(f.runes) {
		return
	}
	f.runes = append(f.runes[:f.cursor], f.runes[f.cursor+1:]...)
}

func (f *textfield) left() {
	if f.cursor > 0 {
		f.cursor--
	}
}

func (f *textfield) right() {
	if f.cursor < len(f.runes) {
		f.cursor++
	}
}

// wordLeft moves to the start of the word before the cursor (skipping any
// spaces first, then the word's runes).
func (f *textfield) wordLeft() {
	i := f.cursor
	for i > 0 && unicode.IsSpace(f.runes[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(f.runes[i-1]) {
		i--
	}
	f.cursor = i
}

// wordRight moves to the end of the word after the cursor.
func (f *textfield) wordRight() {
	i, n := f.cursor, len(f.runes)
	for i < n && unicode.IsSpace(f.runes[i]) {
		i++
	}
	for i < n && !unicode.IsSpace(f.runes[i]) {
		i++
	}
	f.cursor = i
}

// deleteWord removes the word before the cursor.
func (f *textfield) deleteWord() {
	start := f.cursor
	f.wordLeft()
	f.runes = append(f.runes[:f.cursor], f.runes[start:]...)
}

// lineStart returns the index just after the previous '\n' before i (or 0).
func (f *textfield) lineStart(i int) int {
	for i > 0 && f.runes[i-1] != '\n' {
		i--
	}
	return i
}

// lineEnd returns the index of the next '\n' at or after i (or len).
func (f *textfield) lineEnd(i int) int {
	n := len(f.runes)
	for i < n && f.runes[i] != '\n' {
		i++
	}
	return i
}

func (f *textfield) home() { f.cursor = f.lineStart(f.cursor) }
func (f *textfield) end()  { f.cursor = f.lineEnd(f.cursor) }

// HandleEditKey applies one editing key. Returns true if it consumed the key,
// false for any key the popup must handle itself (Enter, Tab, Esc, Up, Down,
// Ctrl+S, …). Navigation/submit semantics stay with the popup.
func (f *textfield) HandleEditKey(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyRunes:
		f.insert(msg.Runes)
	case tea.KeySpace:
		f.insert([]rune{' '})
	case tea.KeyBackspace, tea.KeyCtrlH:
		f.backspace()
	case tea.KeyDelete:
		f.deleteFwd()
	case tea.KeyLeft:
		if msg.Alt {
			f.wordLeft()
		} else {
			f.left()
		}
	case tea.KeyRight:
		if msg.Alt {
			f.wordRight()
		} else {
			f.right()
		}
	case tea.KeyCtrlLeft:
		f.wordLeft()
	case tea.KeyCtrlRight:
		f.wordRight()
	case tea.KeyHome:
		f.home()
	case tea.KeyEnd:
		f.end()
	case tea.KeyCtrlW:
		f.deleteWord()
	default:
		return false
	}
	return true
}
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/tui/ -run TestTextField -v`
Expected: PASS (all `TestTextField*`).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/textfield.go internal/tui/textfield_test.go
git commit -m "feat(tui): cursor-aware textfield core type" -m "Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_018bxAFvuaSjJw7NfpJe1rsj"
```

---

### Task 2: Multi-line support (newline, Up/Down, line-aware Home/End)

**Files:**
- Modify: `internal/tui/textfield.go`
- Test: `internal/tui/textfield_test.go`

**Interfaces:**
- Produces: `InsertNewline()`, `Up()`, `Down()`. (Home/End already snap to the current line via `lineStart`/`lineEnd` from Task 1 — this task adds tests proving it with newlines present.)

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/textfield_test.go`:

```go
func TestTextFieldNewlineAndHomeEnd(t *testing.T) {
	var f textfield
	f.HandleEditKey(runesMsg("ab"))
	f.InsertNewline()
	f.HandleEditKey(runesMsg("cd"))
	if f.Value() != "ab\ncd" {
		t.Fatalf("Value = %q, want 'ab\\ncd'", f.Value())
	}
	f.HandleEditKey(keyMsg(tea.KeyHome)) // start of 2nd line
	if f.cursor != 3 {
		t.Fatalf("home cursor = %d, want 3 (start of 2nd line)", f.cursor)
	}
	f.HandleEditKey(keyMsg(tea.KeyEnd)) // end of 2nd line
	if f.cursor != 5 {
		t.Fatalf("end cursor = %d, want 5", f.cursor)
	}
}

func TestTextFieldUpDown(t *testing.T) {
	f := newTextField("abcd\nxy") // cursor 7 (end, col 2 on line 2)
	f.Up()                        // to line 1, col 2 -> index 2
	if f.cursor != 2 {
		t.Fatalf("Up cursor = %d, want 2", f.cursor)
	}
	f.Down() // back to line 2, col 2 -> index 7
	if f.cursor != 7 {
		t.Fatalf("Down cursor = %d, want 7", f.cursor)
	}
}

func TestTextFieldUpDownColumnClamp(t *testing.T) {
	f := newTextField("a\nlongline") // cursor at end of line 2
	f.Up()                           // line 1 only has 1 col -> clamp to index 1
	if f.cursor != 1 {
		t.Fatalf("Up clamp cursor = %d, want 1", f.cursor)
	}
}

func TestTextFieldUpOnFirstLineNoOp(t *testing.T) {
	f := newTextField("abc")
	f.HandleEditKey(keyMsg(tea.KeyHome))
	f.Up()
	if f.cursor != 0 {
		t.Fatalf("Up on first line cursor = %d, want 0", f.cursor)
	}
	f.HandleEditKey(keyMsg(tea.KeyEnd))
	f.Down()
	if f.cursor != 3 {
		t.Fatalf("Down on last line cursor = %d, want 3", f.cursor)
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/tui/ -run TestTextFieldNewline -v`
Expected: FAIL — `f.InsertNewline undefined`.

- [ ] **Step 3: Write the implementation**

Append to `internal/tui/textfield.go`:

```go
// InsertNewline inserts a line break at the cursor. The popup calls this for
// the commit description's Enter (single-line fields never call it).
func (f *textfield) InsertNewline() { f.insert([]rune{'\n'}) }

// Up moves the cursor to the same column on the previous line (best effort,
// clamped to that line's length). No-op on the first line.
func (f *textfield) Up() {
	ls := f.lineStart(f.cursor)
	if ls == 0 {
		return
	}
	col := f.cursor - ls
	prevStart := f.lineStart(ls - 1)
	prevEnd := ls - 1 // index of the '\n' ending the previous line
	if prevStart+col > prevEnd {
		f.cursor = prevEnd
	} else {
		f.cursor = prevStart + col
	}
}

// Down moves the cursor to the same column on the next line (best effort,
// clamped). No-op on the last line.
func (f *textfield) Down() {
	le := f.lineEnd(f.cursor)
	if le >= len(f.runes) {
		return
	}
	col := f.cursor - f.lineStart(f.cursor)
	nextStart := le + 1
	nextEnd := f.lineEnd(nextStart)
	if nextStart+col > nextEnd {
		f.cursor = nextEnd
	} else {
		f.cursor = nextStart + col
	}
}
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/tui/ -run TestTextField -v`
Expected: PASS (all, including the new multi-line cases).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/textfield.go internal/tui/textfield_test.go
git commit -m "feat(tui): textfield multi-line newline + up/down" -m "Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_018bxAFvuaSjJw7NfpJe1rsj"
```

---

### Task 3: `View(focused)` cursor rendering

**Files:**
- Modify: `internal/tui/textfield.go`
- Test: `internal/tui/textfield_test.go`

**Interfaces:**
- Produces: `func (f *textfield) View(focused bool) string`.

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/textfield_test.go` (force a color profile so the reverse SGR is emitted in the non-TTY test, mirroring the commit-lane-color tests):

```go
// forceColor flips lipgloss to TrueColor for the duration of the test so the
// reverse SGR is emitted in the non-TTY test binary, then restores the previous
// profile. This mirrors internal/tui/commit_color_test.go and diff_render_test.go
// — a bare SetColorProfile leaks global state into later tests that assert
// plain output (e.g. window_test.go), so the save/restore is mandatory.
func forceColor(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

func TestTextFieldViewUnfocusedPlain(t *testing.T) {
	f := newTextField("abc")
	if got := f.View(false); got != "abc" {
		t.Fatalf("unfocused View = %q, want plain 'abc'", got)
	}
}

func TestTextFieldViewFocusedCursorAtRune(t *testing.T) {
	forceColor(t)
	f := newTextField("ab")
	f.HandleEditKey(keyMsg(tea.KeyLeft)) // cursor on 'b'
	got := f.View(true)
	if !strings.Contains(got, "\x1b[7mb") {
		t.Fatalf("focused View = %q, want reverse cell on 'b'", got)
	}
}

func TestTextFieldViewFocusedCursorAtEnd(t *testing.T) {
	forceColor(t)
	f := newTextField("ab") // cursor at end
	got := f.View(true)
	if !strings.HasPrefix(got, "ab") || !strings.Contains(got, "\x1b[7m") {
		t.Fatalf("focused-at-end View = %q, want 'ab' + reverse block", got)
	}
}
```

Add `"strings"`, `"github.com/charmbracelet/lipgloss"`, and `"github.com/muesli/termenv"` to the test file's import block. **Do NOT use a bare `SetColorProfile`** — the save/restore in `forceColor` is required so the global profile flip does not leak into order-dependent tests like `window_test.go` that assert plain output.

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/tui/ -run TestTextFieldView -v`
Expected: FAIL — `f.View undefined`.

- [ ] **Step 3: Write the implementation**

Append to `internal/tui/textfield.go` (add `"github.com/charmbracelet/lipgloss"` to its import block):

```go
// cursorCell renders the character under the cursor in reverse video.
var cursorCell = lipgloss.NewStyle().Reverse(true)

// View renders the field's text. Unfocused: plain text. Focused: the rune at
// the cursor is shown reverse-video; at end-of-buffer (or end-of-line) a
// reverse space marks the insertion point so it is always visible. The caller
// owns surrounding labels and any per-line indentation; for a multi-line buffer
// the caller may split View(true) on "\n" (the reverse cell never spans lines).
func (f *textfield) View(focused bool) string {
	if !focused {
		return string(f.runes)
	}
	if f.cursor >= len(f.runes) {
		return string(f.runes) + cursorCell.Render(" ")
	}
	at := f.runes[f.cursor]
	if at == '\n' {
		return string(f.runes[:f.cursor]) + cursorCell.Render(" ") + "\n" + string(f.runes[f.cursor+1:])
	}
	return string(f.runes[:f.cursor]) + cursorCell.Render(string(at)) + string(f.runes[f.cursor+1:])
}
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/tui/ -run TestTextField -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/textfield.go internal/tui/textfield_test.go
git commit -m "feat(tui): textfield View with reverse-video cursor" -m "Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_018bxAFvuaSjJw7NfpJe1rsj"
```

---

### Task 4: Migrate `commit_popup` (title + multi-line desc) + reword reuse

**Files:**
- Modify: `internal/tui/commit_popup.go`
- Modify: `internal/tui/model.go:661` (`&commitPopup{}` — no change needed, zero textfields work) and `internal/tui/model.go:1288` (amend prefill)
- Modify: `internal/tui/irebase_view.go:103` and `:65`, `internal/tui/reword_popup.go:39` (construction + `.title` read)
- Test: `internal/tui/commit_popup_test.go` (new or extend existing)

**Interfaces:**
- Consumes: `textfield`, `newTextField`, `HandleEditKey`, `InsertNewline`, `Up`, `Down`, `Value`, `View` from Tasks 1-3.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/commit_popup_textfield_test.go`:

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCommitPopupCursorEdit(t *testing.T) {
	p := &commitPopup{}
	// type "abc" into the title, move left twice, insert "X" -> "aXbc"
	p.applyEditKey(runesMsg("abc"))
	p.applyEditKey(keyMsg(tea.KeyLeft))
	p.applyEditKey(keyMsg(tea.KeyLeft))
	p.applyEditKey(runesMsg("X"))
	if got := p.title.Value(); got != "aXbc" {
		t.Fatalf("title = %q, want aXbc", got)
	}
}

func TestCommitPopupEnterAdvancesThenNewline(t *testing.T) {
	p := &commitPopup{}
	p.applyEditKey(runesMsg("subj"))
	p.applyEditKey(keyMsg(tea.KeyEnter)) // title -> desc
	if p.field != 1 {
		t.Fatalf("field = %d, want 1 after Enter on title", p.field)
	}
	p.applyEditKey(runesMsg("line1"))
	p.applyEditKey(keyMsg(tea.KeyEnter)) // newline in desc
	p.applyEditKey(runesMsg("line2"))
	if got := p.desc.Value(); got != "line1\nline2" {
		t.Fatalf("desc = %q, want 'line1\\nline2'", got)
	}
}

func TestCommitPopupSubmitCancel(t *testing.T) {
	p := &commitPopup{}
	if s, c := p.applyEditKey(keyMsg(tea.KeyCtrlS)); !s || c {
		t.Fatalf("ctrl+s = (%v,%v), want submit", s, c)
	}
	if s, c := p.applyEditKey(keyMsg(tea.KeyEsc)); s || !c {
		t.Fatalf("esc = (%v,%v), want cancel", s, c)
	}
}

func TestCommitMessageAndSplit(t *testing.T) {
	p := &commitPopup{title: newTextField("subj"), desc: newTextField("body")}
	if got := p.message(); got != "subj\n\nbody" {
		t.Fatalf("message = %q", got)
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/tui/ -run TestCommitPopupCursor -v`
Expected: FAIL — `p.title.Value undefined` (title is still a string).

- [ ] **Step 3: Write the implementation**

In `internal/tui/commit_popup.go`, change the struct and the three methods:

```go
type commitPopup struct {
	title textfield
	desc  textfield
	field int // 0 = title, 1 = description
	amend bool
}

func (p *commitPopup) message() string {
	t := strings.TrimSpace(p.title.Value())
	if strings.TrimSpace(p.desc.Value()) == "" {
		return t
	}
	return t + "\n\n" + p.desc.Value()
}
```

Replace `applyEditKey` with:

```go
func (p *commitPopup) applyEditKey(msg tea.KeyMsg) (submit, cancel bool) {
	switch msg.Type {
	case tea.KeyEsc:
		return false, true
	case tea.KeyCtrlS:
		return true, false
	case tea.KeyTab, tea.KeyShiftTab:
		p.field = (p.field + 1) % 2
		return false, false
	case tea.KeyEnter:
		if p.field == 0 {
			p.field = 1 // title → description
		} else {
			p.desc.InsertNewline()
		}
		return false, false
	case tea.KeyUp:
		if p.field == 1 {
			p.desc.Up()
		}
		return false, false
	case tea.KeyDown:
		if p.field == 1 {
			p.desc.Down()
		}
		return false, false
	}
	if p.field == 0 {
		p.title.HandleEditKey(msg)
	} else {
		p.desc.HandleEditKey(msg)
	}
	return false, false
}
```

In `update`, change the empty-title guard:

```go
		if strings.TrimSpace(p.title.Value()) == "" {
```

Replace `renderCommitFields` to use `View`:

```go
func renderCommitFields(p *commitPopup) string {
	var b strings.Builder
	titleCur, descCur := "  ", "  "
	if p.field == 0 {
		titleCur = "> "
	} else {
		descCur = "> "
	}
	b.WriteString(titleCur + "title:       " + p.title.View(p.field == 0) + "\n")
	descLines := strings.Split(p.desc.View(p.field == 1), "\n")
	b.WriteString(descCur + "description: " + descLines[0] + "\n")
	for _, l := range descLines[1:] {
		b.WriteString("             " + l + "\n")
	}
	return b.String()
}
```

Fix the construction/read sites:
- `internal/tui/model.go:1288`: `&commitPopup{title: newTextField(title), desc: newTextField(desc), amend: true}`
- `internal/tui/irebase_view.go:103`: `e.reword = &commitPopup{title: newTextField(t), desc: newTextField(d)}`
- `internal/tui/irebase_view.go:65`: `if strings.TrimSpace(e.reword.title.Value()) == "" {`
- `internal/tui/reword_popup.go:39`: `popup: commitPopup{title: newTextField(t), desc: newTextField(d)}`
- `internal/tui/model.go:661`: `&commitPopup{}` stays (zero textfields are valid empty fields).

> Check for any other `.title`/`.desc`/`.message()` reads on a `commitPopup`/`rewordPopup` with `grep -rn "\.title\|\.desc" internal/tui/reword_popup.go internal/tui/irebase_view.go` and route each through `.Value()`.

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/tui/ -run 'TestCommit' -v && go build ./cmd/gg`
Expected: PASS and a clean build (catches any missed `.title`/`.desc` read site).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/commit_popup.go internal/tui/model.go internal/tui/irebase_view.go internal/tui/reword_popup.go internal/tui/commit_popup_textfield_test.go
git commit -m "feat(tui): cursor editing in the commit/amend/reword popup" -m "Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_018bxAFvuaSjJw7NfpJe1rsj"
```

---

### Task 5: Migrate `branch_popup` + `rename_branch_popup` (name fields, space-suppressed)

**Files:**
- Modify: `internal/tui/branch_popup.go`, `internal/tui/rename_branch_popup.go`
- Modify construction: `internal/tui/branch_popup.go:25`, `internal/tui/commit_scope.go:208`, `:629`, `internal/tui/rename_branch_popup.go:26`
- Test: `internal/tui/branch_popup_textfield_test.go`

**Interfaces:**
- Consumes: `textfield`, `newTextField`, `HandleEditKey`, `Value`, `View`.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/branch_popup_textfield_test.go`:

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestBranchPopupCursorEditNoSpace(t *testing.T) {
	p := &branchPopup{startPoint: "main"}
	m := Model{}
	m, _ = p.update(m, runesMsg("feat"))
	m, _ = p.update(m, keyMsg(tea.KeyLeft))
	m, _ = p.update(m, runesMsg("X")) // insert before 't' -> "feaXt"
	m, _ = p.update(m, keyMsg(tea.KeySpace))
	_ = m
	if got := p.name.Value(); got != "feaXt" {
		t.Fatalf("name = %q, want feaXt (space dropped)", got)
	}
}

func TestRenameBranchPrefilledCursorAtEnd(t *testing.T) {
	p := &renameBranchPopup{old: "old", name: newTextField("old")}
	m := Model{}
	m, _ = p.update(m, runesMsg("er")) // append -> "older"
	_ = m
	if got := p.name.Value(); got != "older" {
		t.Fatalf("name = %q, want older", got)
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/tui/ -run 'TestBranchPopupCursor|TestRenameBranch' -v`
Expected: FAIL — `p.name.Value undefined`.

- [ ] **Step 3: Write the implementation**

`branch_popup.go`: change `name string` → `name textfield`. Replace the `update` switch body:

```go
	switch msg.Type {
	case tea.KeyEsc:
		m = m.popLayer()
	case tea.KeyEnter:
		if p.name.Value() == "" {
			return m, nil
		}
		op := engine.CreateBranch{Name: p.name.Value(), StartPoint: p.startPoint}
		if p.switchAfter {
			m.pendingSwitchBranch = p.name.Value()
		}
		m = m.popLayer()
		return m.startOp(op)
	case tea.KeySpace:
		// Branch names cannot contain spaces — drop it.
	default:
		p.name.HandleEditKey(msg)
	}
	return m, nil
```

In `box`: `b.WriteString("name: " + p.name.View(true) + "\n\n")`.

`rename_branch_popup.go`: change `name string` → `name textfield`. Replace the `update` switch:

```go
	switch msg.Type {
	case tea.KeyEsc:
		m = m.popLayer()
	case tea.KeyEnter:
		if p.name.Value() == "" || p.name.Value() == p.old {
			m = m.popLayer()
			return m, nil
		}
		op := engine.RenameBranch{Old: p.old, New: p.name.Value()}
		m = m.popLayer()
		return m.startOp(op)
	case tea.KeySpace:
		// branch names cannot contain spaces — drop it.
	default:
		p.name.HandleEditKey(msg)
	}
	return m, nil
```

In `box`: `b.WriteString("name: " + p.name.View(true) + "\n\n")`.

Fix constructions:
- `branch_popup.go:25`: `&branchPopup{startPoint: m.branches[bi].Name, switchAfter: switchAfter}` (name left zero — fine).
- `commit_scope.go:208`: `&branchPopup{startPoint: hash}` (unchanged).
- `commit_scope.go:629`: `&renameBranchPopup{old: name, name: newTextField(name)}`.
- `rename_branch_popup.go:26`: `&renameBranchPopup{old: cur, name: newTextField(cur)}`.

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/tui/ -run 'TestBranch|TestRename' -v && go build ./cmd/gg`
Expected: PASS + clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/branch_popup.go internal/tui/rename_branch_popup.go internal/tui/commit_scope.go internal/tui/branch_popup_textfield_test.go
git commit -m "feat(tui): cursor editing in create/rename branch popups" -m "Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_018bxAFvuaSjJw7NfpJe1rsj"
```

---

### Task 6: Migrate `tag_popup` (name + message) + `tag_checkout_popup`

**Files:**
- Modify: `internal/tui/tag_popup.go`, `internal/tui/tag_checkout_popup.go`
- Modify construction: `internal/tui/commit_scope.go:229` (tagPopup — no prefill), `internal/tui/tags_actions.go:38` (tagCheckoutPopup prefill)
- Test: `internal/tui/tag_popup_textfield_test.go`

**Interfaces:**
- Consumes: `textfield`, `newTextField`, `HandleEditKey`, `Value`, `View`.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/tag_popup_textfield_test.go`:

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTagPopupNameNoSpaceMessageSpace(t *testing.T) {
	p := &tagPopup{commit: "deadbeef"}
	m := Model{}
	m, _ = p.update(m, runesMsg("v1"))
	m, _ = p.update(m, keyMsg(tea.KeySpace)) // dropped in name
	m, _ = p.update(m, keyMsg(tea.KeyTab))   // -> message
	m, _ = p.update(m, runesMsg("a"))
	m, _ = p.update(m, keyMsg(tea.KeySpace)) // kept in message
	m, _ = p.update(m, runesMsg("b"))
	_ = m
	if p.name.Value() != "v1" {
		t.Fatalf("name = %q, want v1", p.name.Value())
	}
	if p.message.Value() != "a b" {
		t.Fatalf("message = %q, want 'a b'", p.message.Value())
	}
}

func TestTagCheckoutPrefillNoSpace(t *testing.T) {
	p := &tagCheckoutPopup{tag: "v1", name: newTextField("v1")}
	m := Model{}
	m, _ = p.update(m, runesMsg("-x"))
	m, _ = p.update(m, keyMsg(tea.KeySpace))
	_ = m
	if p.name.Value() != "v1-x" {
		t.Fatalf("name = %q, want v1-x", p.name.Value())
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/tui/ -run 'TestTagPopup|TestTagCheckout' -v`
Expected: FAIL — `p.name.Value undefined`.

- [ ] **Step 3: Write the implementation**

`tag_popup.go`: change `name string` and `message string` → `textfield`. Replace the body keys after `KeyTab`/`KeyEnter`:

```go
	case tea.KeyEnter:
		if p.name.Value() == "" {
			return m, nil
		}
		op := engine.CreateTag{Name: p.name.Value(), Commit: p.commit, Message: p.message.Value()}
		m = m.popLayer()
		return m.startOp(op)
	default:
		if p.onMsg {
			p.message.HandleEditKey(msg) // message allows spaces
		} else if msg.Type != tea.KeySpace {
			p.name.HandleEditKey(msg) // name drops spaces
		}
	}
	return m, nil
```

(Keep the `KeyCtrlC`, `KeyEsc`, `KeyTab` cases as they are; remove the old `KeyBackspace`/`KeySpace`/`KeyRunes` cases — `default` now covers them.)

In `box`: `b.WriteString(nameMark + "name:    " + p.name.View(!p.onMsg) + "\n")` and `b.WriteString(msgMark + "message: " + p.message.View(p.onMsg) + "  (empty = lightweight)\n")`.

`tag_checkout_popup.go`: change `name string` → `textfield`. Replace `update` switch:

```go
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		if p.name.Value() == "" {
			return m, nil
		}
		op := engine.CheckoutTag{Name: p.tag, Branch: p.name.Value()}
		m = m.popLayer()
		return m.startOp(op)
	case tea.KeySpace:
		// Branch names cannot contain spaces — drop it.
	default:
		p.name.HandleEditKey(msg)
	}
	return m, nil
```

In `box`: `b.WriteString("name: " + p.name.View(true) + "\n\n")`.

Fix constructions:
- `commit_scope.go:229`: `&tagPopup{commit: hash}` (name/message zero — fine).
- `tags_actions.go:38`: `&tagCheckoutPopup{tag: name, name: newTextField(name)}`.

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/tui/ -run 'TestTag' -v && go build ./cmd/gg`
Expected: PASS + clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/tag_popup.go internal/tui/tag_checkout_popup.go internal/tui/commit_scope.go internal/tui/tags_actions.go internal/tui/tag_popup_textfield_test.go
git commit -m "feat(tui): cursor editing in create-tag and tag-checkout popups" -m "Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_018bxAFvuaSjJw7NfpJe1rsj"
```

---

### Task 7: Migrate `stash_popup` name field

**Files:**
- Modify: `internal/tui/stash_popup.go` (struct, `update` name branch, `op`, `box`, construction at `:72`)
- Test: `internal/tui/stash_popup_textfield_test.go`

**Interfaces:**
- Consumes: `textfield`, `newTextField`, `HandleEditKey`, `Value`, `View`. The file list (`field == 1`) is unchanged.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/stash_popup_textfield_test.go`:

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestStashPopupNameCursorEdit(t *testing.T) {
	p := &stashPopup{name: newTextField("WIP"), field: 0}
	m := Model{}
	m, _ = p.update(m, runesMsg(" fix"))   // space allowed in stash name -> "WIP fix"
	m, _ = p.update(m, keyMsg(tea.KeyHome)) // cursor to start
	m, _ = p.update(m, runesMsg("X"))       // -> "XWIP fix"
	_ = m
	if got := p.name.Value(); got != "XWIP fix" {
		t.Fatalf("name = %q, want 'XWIP fix'", got)
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/tui/ -run TestStashPopupName -v`
Expected: FAIL — `p.name.Value undefined`.

- [ ] **Step 3: Write the implementation**

`stash_popup.go`: change `name string` → `name textfield`. In `op()`: `Message: p.name.Value()`. In `update`, replace the trailing name-field block (the `switch msg.Type` after `// name field`) with:

```go
	// name field
	p.name.HandleEditKey(msg)
	return m, nil
```

In `update`'s `ctrl+s` branch, the `op.Message` empty check already runs against `p.op()` output, so no change there beyond `op()` using `.Value()`.

In `box`, replace the manual `▏` cursor with `View`:

```go
	b.WriteString("Stash changes\n\nname: " + p.name.View(p.field == 0) + "\n\n")
```

(Delete the `nameCursor` local and its `if p.field == 0` block.)

Fix construction at `stash_popup.go:72`:

```go
	m = m.pushLayer(&stashPopup{name: newTextField("WIP on " + m.status.Branch), files: cand, field: 1})
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/tui/ -run TestStash -v && go build ./cmd/gg`
Expected: PASS + clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/stash_popup.go internal/tui/stash_popup_textfield_test.go
git commit -m "feat(tui): cursor editing in the stash name field" -m "Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_018bxAFvuaSjJw7NfpJe1rsj"
```

---

### Task 8: Migrate `shelf_actions` restore-destination field

**Files:**
- Modify: `internal/tui/shelf_actions.go` (`shelfRestorePopup` struct, `update`, `render`)
- Construction at `:100` needs no change (dest starts empty → zero textfield)
- Test: `internal/tui/shelf_restore_textfield_test.go`

**Interfaces:**
- Consumes: `textfield`, `HandleEditKey`, `Value`, `View`.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/shelf_restore_textfield_test.go`:

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestShelfRestoreCursorEdit(t *testing.T) {
	p := &shelfRestorePopup{entryID: "e1", origin: "a/b.txt"}
	m := Model{}
	m, _ = p.update(m, runesMsg("dir/file"))
	m, _ = p.update(m, keyMsg(tea.KeyLeft))
	m, _ = p.update(m, keyMsg(tea.KeyLeft))
	m, _ = p.update(m, runesMsg("X")) // insert two from end -> "dir/fiXle"
	_ = m
	if got := p.dest.Value(); got != "dir/fiXle" {
		t.Fatalf("dest = %q, want dir/fiXle", got)
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/tui/ -run TestShelfRestoreCursor -v`
Expected: FAIL — `p.dest.Value undefined`.

- [ ] **Step 3: Write the implementation**

`shelf_actions.go`: change `dest string` → `dest textfield`. In `update`, replace the editing cases:

```go
	case tea.KeyEnter:
		dest := strings.TrimSpace(p.dest.Value())
		if dest == "" {
			return m, nil // a destination is mandatory
		}
		entry := p.entryID
		m = m.popLayer()
		blob, err := m.svc.ShelfBlob(context.Background(), entry)
		if err != nil {
			m.statusMsg = "shelf restore: " + err.Error()
			return m, nil
		}
		return m.startOp(engine.WriteFile{Path: dest, Data: blob})
	default:
		p.dest.HandleEditKey(msg)
	}
	return m, nil
```

(Keep `KeyCtrlC` and `KeyEsc`; remove the old `KeyBackspace`/`KeySpace`/`KeyRunes` cases — `default` covers them. Paths may contain spaces, so no space-suppression here.)

In `render`: `b.WriteString("dest: " + p.dest.View(true) + "\n\n")`.

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/tui/ -run TestShelf -v && go build ./cmd/gg`
Expected: PASS + clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/shelf_actions.go internal/tui/shelf_restore_textfield_test.go
git commit -m "feat(tui): cursor editing in the shelf restore-destination field" -m "Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_018bxAFvuaSjJw7NfpJe1rsj"
```

---

### Task 9: Migrate `worktree_popup` (input map + editBuf)

**Files:**
- Modify: `internal/tui/worktree_popup.go` (struct fields `inputs`, `editBuf`; `fixedBranch`, `recompute`, the `stInput` + `stEdit` key handling, `box`, both constructors + init loops)
- Test: `internal/tui/worktree_popup_textfield_test.go`

**Interfaces:**
- Consumes: `textfield`, `newTextField`, `HandleEditKey`, `Value`, `View`.
- Note: `worktree.Resolve` keeps its `map[string]string` signature; `recompute` builds a plain string map from the fields.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/worktree_popup_textfield_test.go`:

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestWorktreeEditBufCursor(t *testing.T) {
	p := &worktreePopup{state: stEdit, editBuf: newTextField("feat")}
	m := Model{}
	m, _ = p.update(m, keyMsg(tea.KeyLeft))
	m, _ = p.update(m, keyMsg(tea.KeyLeft))
	m, _ = p.update(m, runesMsg("X")) // "feXat"
	_ = m
	if got := p.editBuf.Value(); got != "feXat" {
		t.Fatalf("editBuf = %q, want feXat", got)
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/tui/ -run TestWorktreeEditBuf -v`
Expected: FAIL — `p.editBuf.Value undefined`.

- [ ] **Step 3: Write the implementation**

In `worktree_popup.go`:

Struct (line ~36, ~46):
```go
	inputs   map[string]textfield // label -> field
	...
	editBuf  textfield // stEdit working buffer
```

`fixedBranch` (line 73): `return p.editBuf.Value()`.

`recompute` (line 84) — build a string map for Resolve:
```go
func (p *worktreePopup) recompute() {
	fixed := p.fixedBranch()
	tm := worktree.Templates{Branch: p.branchTmpl, Path: p.pathTmpl}
	vals := make(map[string]string, len(p.inputs))
	for l, f := range p.inputs {
		vals[l] = f.Value()
	}
	p.previewBranch, p.previewPath, p.previewErr = worktree.Resolve(tm, fixed, vals, p.tctx())
}
```

Both constructors: `inputs: map[string]textfield{}` (lines 116, 154). The seeded `editBuf` (line 160): `editBuf: newTextField(prefillBranch)`. Init loops (lines 123, 163): `p.inputs[l] = textfield{}`.

`stInput` key handling (lines 178-200) — delegate to the field (value-in-map: copy, mutate, store back):
```go
		case tea.KeyEsc:
			m = m.popLayer()
		case tea.KeyEnter, tea.KeyTab:
			p.fieldIdx++
			if p.fieldIdx >= len(p.labels) {
				p.fieldIdx = len(p.labels) - 1
				p.state = stAction
			}
			p.recompute()
		default:
			lbl := p.labels[p.fieldIdx]
			f := p.inputs[lbl]
			if f.HandleEditKey(msg) {
				p.inputs[lbl] = f
				p.recompute()
			}
```

`stEdit` key handling (lines 202-222):
```go
		case tea.KeyEnter:
			p.branchOverride = p.editBuf.Value()
			p.state = stAction
			p.recompute()
		case tea.KeyEsc:
			p.state = stAction
			p.recompute()
		default:
			if p.editBuf.HandleEditKey(msg) {
				p.recompute()
			}
```

`stAction` "e" branch (line 232): `p.editBuf = newTextField(p.previewBranch)`.

`box` (line 265): `b.WriteString(cursor + lbl + ": " + p.inputs[lbl].View(p.state == stInput && p.labels[p.fieldIdx] == lbl) + "\n")`. Line 273 (`branch = p.editBuf`): `branch = p.editBuf.Value()`, and where the edit buffer is shown render `p.editBuf.View(p.state == stEdit)`.

> **This is the one task that must be executed with the file open, not blind.** Read `box` (lines ~252-300) in FULL before editing. The invariant: **exactly one** field shows the cursor at a time. The focused bool is:
> - input rows: `p.state == stInput && p.labels[p.fieldIdx] == lbl` (true for only the active label),
> - the edit buffer: `p.state == stEdit`.
>
> Route every `p.inputs[...]` / `p.editBuf` read through `.View(focused)` for **display** and `.Value()` for **logic** (e.g. `fixedBranch`, `branchOverride`). After the edit, eyeball that no row shows a cursor while `p.state == stAction`, and that the cursor never appears on two fields simultaneously.

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/tui/ -run TestWorktree -v && go build ./cmd/gg`
Expected: PASS + clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/worktree_popup.go internal/tui/worktree_popup_textfield_test.go
git commit -m "feat(tui): cursor editing in the create-worktree popup fields" -m "Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_018bxAFvuaSjJw7NfpJe1rsj"
```

---

### Task 10: Docs + full race suite

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `README.md` (only if a documented key-hint changed — the popups' footers gain ←/→ navigation implicitly; add a one-line note if the README documents popup editing)
- Verify: whole suite with `-race`

- [ ] **Step 1: Update CHANGELOG**

Add under the top/unreleased section of `CHANGELOG.md`:

```markdown
- TUI editable text fields (commit title/description, branch name, rename
  branch, tag name/message, tag-checkout name, stash name, shelf restore
  destination, create-worktree fields) now have a visible cursor with full
  line editing: ←/→ move, Home/End, insert/delete at the cursor, word-jumps
  (Alt/Ctrl+←/→), and Ctrl+W delete-word. Search/filter inputs are unchanged.
```

- [ ] **Step 2: README check**

Run: `grep -n "commit\|branch name\|popup" README.md | head`
If the README documents popup text entry, add a one-line note that fields support cursor + arrow-key editing. If not, skip (no user-facing key table to change).

- [ ] **Step 3: Full race suite**

Run: `./test.sh race`
Expected: PASS (vet+gofmt → unit → e2e, all green under `-race`).

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md README.md
git commit -m "docs: cursor-aware text fields in editable popups" -m "Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_018bxAFvuaSjJw7NfpJe1rsj"
```

---

## Self-Review

- **Spec coverage:** word-jump editing (Tasks 1) ✓; all true editable fields — commit title/desc (4), branch (5), rename-branch (5), tag name+message (6), tag-checkout (6), stash name (7), shelf dest (8), worktree fields (9) ✓; multi-line desc in the same textfield (2, 4) ✓; cursor View (3) ✓; filters excluded (Global Constraints, no task touches them) ✓; space-suppression for name fields (Global Constraints + Tasks 5/6) ✓; reword reuse (4) ✓; worktree.Resolve signature preserved (9) ✓; docs (10) ✓.
- **Placeholder scan:** every code step shows full code; the two "read the surrounding code" notes (worktree `box`, commit reword read sites) are bounded audit instructions with the exact routing rule (`.Value()` for logic, `.View()` for display), not deferred work.
- **Type consistency:** `textfield`, `newTextField`, `HandleEditKey`, `InsertNewline`, `Up`, `Down`, `Value`, `SetValue`, `View` are used identically across all tasks. Name fields call `HandleEditKey` only after dropping `tea.KeySpace`.
