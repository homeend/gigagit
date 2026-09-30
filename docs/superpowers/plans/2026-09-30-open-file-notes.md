# Open-File Notes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task IN THIS SESSION (project rule: no implementer subagents; the final whole-branch review may be a read-only subagent). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An agent can put temporary remarks on the lines of a file open in the TUI; the user reads them inline, dismisses them, and copies a reference back to the agent; the remarks die with the open file.

**Architecture:** Notes are plain structs on the TUI's `openFile` document (memory only). The file preview draws them as *virtual rows* — never inserted into `contentPopup.lines`, so every line index stays a file line. Four new steer commands (`note_add|list|show|rm`) are answered by the TUI itself, and a new `gg session note` CLI family posts them to a live TUI only.

**Tech Stack:** Go 1.26, Bubble Tea / lipgloss (`internal/tui`), `internal/steer` file inbox, `internal/textdiff` (Myers line alignment), stdlib `flag` CLI.

**Spec:** `docs/superpowers/specs/2026-09-30-open-file-notes-design.md`

## Global Constraints

- Work ONLY in `/work/gigagit/.claude/worktrees/open-file-notes` (branch `feat/open-file-notes`). Every shell command starts with `cd /work/gigagit/.claude/worktrees/open-file-notes &&`; Write/Edit use absolute paths under it.
- Commit with `gg add <explicit paths>` then `git commit -F <msgfile>` (message file in the session scratchpad). NEVER `git add -A` (a built `bin/gg` lives in the worktree). Every commit message ends with the two attribution lines:
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01DgPp1yxfxJFBtpCKDenChf`.
- Notes are memory-only: never touch `internal/notes`, never write a file for them.
- Working-tree documents only (`srcWorktree`); 50 notes per file, summary ≤ 500 runes, rationale ≤ 4000 runes — an over-limit add is REFUSED, never cut.
- A file with notes: esc backgrounds (`backgrounded = true`), only X / switcher x closes, never evicted by the 20-file cap.
- A file WITHOUT notes must render and behave byte-identically to today (the existing preview/viewer tests are the guard — do not edit them to pass).
- Every user-visible TUI string goes through `i18n.T` with a literal key present in `internal/i18n/lang/{ja,ko,zh,ru}.toml`. Steer reply prose (`Detail`/`Error`) and CLI output stay English.
- `internal/tui` and `internal/cli` never import `internal/git`.
- New tests call `t.Parallel()`.
- Run package tests with `rtk go test ./internal/<pkg> -run '<regex>' -count=1`; the full gate is `./test.sh race` and is green ONLY when its log ends with `all green`.

## Review Focus

1. **A note on the LAST line of a file** — the user must be able to scroll until its whole box is visible (the pager's max top must count note rows). Pinned in Task 3 (`TestFileNoteBoxOnTheLastLineIsReachable`).
2. **Hostile note text** — a summary/rationale holding `\n`, `\t`, `\r`, ANSI escapes, a 300-column word, or CJK wide glyphs must never draw a row wider than the box or more rows than were counted. Pinned in Task 3 (`TestFileNoteBoxRowsAreExactlyTheBoxWidth`).
3. **A very narrow terminal** (inner width < 8) — the box must not panic or go negative. Pinned in Task 3 (`TestFileNoteBoxSurvivesANarrowBox`).
4. **The file closes (or fails to read) between `note add` and its load landing** — the agent gets a failed reply, gg does not panic. Pinned in Task 5 (`TestNoteAddOnAFileClosedBeforeItLandsFails`).
5. **A reload that replaces the whole file** (truncated alignment, emptied file, placeholder) — notes go outdated and clamp, nothing panics, and they recover when the text returns. Pinned in Task 2 (`TestFileNotesSurviveAnEmptiedAndRestoredFile`).

---

## File Structure

| File | Responsibility |
|------|----------------|
| `internal/tui/open_file_notes.go` (new) | `fileNote`, add/remove/lookup on `openFile`, re-anchoring, box rows, row counting |
| `internal/tui/open_file_notes_test.go` (new) | model, lifetime, re-anchor tests |
| `internal/tui/open_file_notes_render_test.go` (new) | virtual-row rendering + scrolling tests |
| `internal/tui/open_file_note_keys.go` (new) | `previewNoteKey`, note stepping, `.` menu rows, hint helpers |
| `internal/tui/open_file_note_keys_test.go` (new) | key/menu/hint tests |
| `internal/tui/steer_file_notes.go` (new) | the four steer handlers + wire conversion |
| `internal/tui/steer_file_notes_test.go` (new) | steer handler tests |
| `internal/cli/session_note.go` (new) | `gg session note add|list|show|rm|clear` |
| `internal/cli/session_note_test.go` (new) | CLI parsing/output/exit-code tests |
| `internal/tui/open_file.go` | `notes`/`noteW` fields; `fill` re-anchors; `landPendingLine` clamps through notes |
| `internal/tui/open_files.go` | eviction skips noted files; `openFilesProto` note count |
| `internal/tui/content_popup.go` | `extraRows` hook field |
| `internal/tui/preview_select.go` | `clampTop`, `rowsSpan`, `lastVisible`, note-aware `ensureCursorVisible`, gutter in `activePreview` |
| `internal/tui/file_preview.go` | `renderPreviewBox` virtual rows + gutter + hint; `snapHit` |
| `internal/tui/diff_render.go` | factor `noteBoxCell` out of `noteRowCells` |
| `internal/tui/file_viewer.go`, `files_view.go`, `mouse.go` | `previewNoteKey` hook; `clampTop` call sites |
| `internal/tui/action_menu.go`, `footer.go`, `help.go`, `sessions_popup.go` | menu rows, hints, help row, switcher count |
| `internal/tui/steer.go`, `model.go` | dispatch, enum refusal, `noteLandedMsg` |
| `internal/steer/steer.go` | `Command` fields, `FileNote`, `Reply.Notes`, `OpenFile.Notes` |
| `internal/cli/session.go` | `note` dispatch + usage |
| `internal/i18n/lang/{ja,ko,zh,ru}.toml` | new keys |
| docs: `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`, `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go` | user/agent docs, skill version 105 → 106 |

---

### Task 1: The note model and its lifetime

**Files:**
- Create: `internal/tui/open_file_notes.go`, `internal/tui/open_file_notes_test.go`
- Modify: `internal/tui/open_file.go` (struct `openFile`, after the `backgrounded` field), `internal/tui/open_files.go:58-66` (`touch`)

**Interfaces:**
- Consumes: `openFile`, `docLoaded`, `newOpenFile`, `fileSource{kind: srcWorktree}`, `openFilesReg.touch`, `Model.escDoc/closeDoc`, test helpers `docLines(n)`, `loadedNavModel(t)`, `bgDoc`, `fill20`.
- Produces:
  - `type fileNote struct{ id string; seq int64; start, end int; anchor []string; summary, rationale, author string; outdated bool }`
  - `func (d *openFile) addNote(start, end int, summary, rationale, author string) (*fileNote, error)`
  - `func (d *openFile) removeNote(id string) bool`
  - `func (d *openFile) clearNotes() int`
  - `func (d *openFile) noteAt(line int) *fileNote` (1-based line; first note covering it)
  - `func (d *openFile) sortNotes()`
  - `func (m Model) findFileNote(id string) (*openFile, *fileNote)`
  - `const maxFileNotes = 50`, `maxFileNoteSummary = 500`, `maxFileNoteRationale = 4000`
  - field `openFile.notes []*fileNote`

- [ ] **Step 1: Write the failing tests**

`internal/tui/open_file_notes_test.go`:

```go
package tui

import (
	"strings"
	"testing"
)

// notedDoc is a loaded working-tree document of n lines ("line 1"…).
func notedDoc(t *testing.T, n int) *openFile {
	t.Helper()
	d := newOpenFile(fileSource{kind: srcWorktree}, "f.go")
	d.fill(fileContentMsg{lines: docLines(n)}, 10, 80)
	return d
}

func TestFileNoteAddKeepsTheRangeTextAndMarksTheFileBackgrounded(t *testing.T) {
	t.Parallel()
	d := notedDoc(t, 10)
	n, err := d.addNote(3, 4, "  look here  ", "because", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(n.id, "t") || n.start != 3 || n.end != 4 || n.summary != "look here" || n.author != "agent" {
		t.Fatalf("note = %+v", n)
	}
	if len(n.anchor) != 2 || n.anchor[0] != "line 3" || n.anchor[1] != "line 4" {
		t.Fatalf("anchor = %q, want the two lines' text", n.anchor)
	}
	if !d.backgrounded {
		t.Fatal("a file with a note must survive esc (backgrounded)")
	}
}

func TestFileNoteAddRefusals(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", maxFileNoteSummary+1)
	for _, tc := range []struct {
		name       string
		start, end int
		summary    string
		rationale  string
		want       string
	}{
		{"zero line", 0, 1, "s", "", "a line number is 1-based"},
		{"backwards", 4, 3, "s", "", "the range ends before it starts"},
		{"past the end", 9, 11, "s", "", "line 11 is past the end of f.go (10 lines)"},
		{"no summary", 1, 1, "   ", "", "a note needs a summary"},
		{"long summary", 1, 1, long, "", "the summary is longer than 500 characters"},
		{"long rationale", 1, 1, "s", strings.Repeat("y", maxFileNoteRationale+1), "the rationale is longer than 4000 characters"},
	} {
		d := notedDoc(t, 10)
		if _, err := d.addNote(tc.start, tc.end, tc.summary, tc.rationale, ""); err == nil || err.Error() != tc.want {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
		if len(d.notes) != 0 || d.backgrounded {
			t.Errorf("%s: a refused add changed the document", tc.name)
		}
	}
}

func TestFileNoteAddRefusesOtherSourcesAndPlaceholders(t *testing.T) {
	t.Parallel()
	c := newOpenFile(fileSource{kind: srcCommit, rev: "abc"}, "f.go")
	c.fill(fileContentMsg{lines: docLines(3)}, 10, 80)
	if _, err := c.addNote(1, 1, "s", "", ""); err == nil || err.Error() != "temporary notes go on working-tree files only" {
		t.Errorf("commit version: err = %v", err)
	}
	p := newOpenFile(fileSource{kind: srcWorktree}, "f.go") // still "(loading…)"
	if _, err := p.addNote(1, 1, "s", "", ""); err == nil || err.Error() != "f.go has no lines to note" {
		t.Errorf("placeholder: err = %v", err)
	}
}

func TestFileNoteCapPerFile(t *testing.T) {
	t.Parallel()
	d := notedDoc(t, 10)
	for i := 0; i < maxFileNotes; i++ {
		if _, err := d.addNote(1, 1, "s", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.addNote(1, 1, "s", "", ""); err == nil || err.Error() != "f.go already carries 50 notes" {
		t.Fatalf("err = %v", err)
	}
}

func TestFileNotesStayOrderedByLineThenAge(t *testing.T) {
	t.Parallel()
	d := notedDoc(t, 10)
	a, _ := d.addNote(7, 7, "a", "", "")
	b, _ := d.addNote(2, 3, "b", "", "")
	c, _ := d.addNote(2, 2, "c", "", "")
	got := []*fileNote{d.notes[0], d.notes[1], d.notes[2]}
	if got[0] != b || got[1] != c || got[2] != a {
		t.Fatalf("order = %s %s %s, want b c a", got[0].summary, got[1].summary, got[2].summary)
	}
	if d.noteAt(3) != b || d.noteAt(7) != a || d.noteAt(5) != nil {
		t.Fatal("noteAt does not find the covering note")
	}
	if !d.removeNote(b.id) || d.removeNote(b.id) || len(d.notes) != 2 {
		t.Fatal("removeNote must remove exactly once")
	}
	if n := d.clearNotes(); n != 2 || len(d.notes) != 0 {
		t.Fatalf("clearNotes = %d, left %d", n, len(d.notes))
	}
}

func TestEvictionSkipsAFileWithNotes(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	fill20(m) // f0.txt is the oldest
	src := fileSource{kind: srcWorktree}
	f0 := m.openFiles.find(m.currentWorktree, docKey(src, "f0.txt"))
	if _, err := f0.addNote(1, 1, "keep me", "", ""); err != nil {
		t.Fatal(err)
	}
	bgDoc(m, src, "new.txt", 1)
	if m.openFiles.find(m.currentWorktree, docKey(src, "f0.txt")) == nil {
		t.Fatal("the annotated file was evicted")
	}
	if m.openFiles.find(m.currentWorktree, docKey(src, "f1.txt")) != nil {
		t.Fatal("want the oldest file WITHOUT notes (f1.txt) evicted instead")
	}
}

func TestEvictionGrowsTheListWhenEveryFileHasNotes(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	fill20(m)
	for _, d := range m.openFiles.list(m.currentWorktree) {
		if _, err := d.addNote(1, 1, "n", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	bgDoc(m, fileSource{kind: srcWorktree}, "new.txt", 1)
	if got := len(m.openFiles.list(m.currentWorktree)); got != maxOpenFiles+1 {
		t.Fatalf("list = %d files, want %d (nothing evictable)", got, maxOpenFiles+1)
	}
}

func TestEscKeepsANotedFileAndXDropsItWithItsNotes(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	src := fileSource{kind: srcWorktree}
	d := bgDoc(m, src, "a.txt", 5)
	d.backgrounded = false // opened in the foreground
	m = m.pushLayer(&fileViewer{d})
	n, err := d.addNote(2, 2, "s", "", "")
	if err != nil {
		t.Fatal(err)
	}
	m = m.escDoc(d)
	if m.openFiles.find(m.currentWorktree, docKey(src, "a.txt")) != d || len(d.notes) != 1 {
		t.Fatal("esc closed a file that carries notes")
	}
	if fd, fn := m.findFileNote(n.id); fd != d || fn != n {
		t.Fatalf("findFileNote = %v %v", fd, fn)
	}
	m = m.closeDoc(d)
	if m.openFiles.find(m.currentWorktree, docKey(src, "a.txt")) != nil {
		t.Fatal("X did not close the file")
	}
	if fd, _ := m.findFileNote(n.id); fd != nil {
		t.Fatal("a closed file's note is still findable")
	}
}
```

Note: `bgDoc` builds lines with no `raw`; that is fine for these tests (anchors are then empty strings).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/open-file-notes && rtk go test ./internal/tui -run 'TestFileNote|TestEviction|TestEscKeepsANotedFile' -count=1`
Expected: build FAIL — `d.addNote undefined`, `maxFileNoteSummary undefined`.

- [ ] **Step 3: Implement the model**

In `internal/tui/open_file.go`, add to `openFile` right after the `backgrounded bool` field:

```go
	// notes are the temporary remarks an agent left on this file
	// (open_file_notes.go), ordered by start line then age. They live and
	// die with the document: nothing stores them.
	notes []*fileNote
```

Create `internal/tui/open_file_notes.go`:

```go
package tui

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

// Temporary notes on an open file: remarks an agent puts on the lines of a
// working-tree document so the user can read them next to the code. They
// are memory only — closing the file (X) drops them, and nothing writes
// them to the notes store. Spec: 2026-09-30-open-file-notes-design.md.

const (
	maxFileNotes         = 50
	maxFileNoteSummary   = 500
	maxFileNoteRationale = 4000
)

// fileNote is one remark. start/end are 1-based lines of the content shown
// NOW; anchor is those lines' text as it was when the note was placed or
// last re-anchored, which is how a reload finds the lines again.
type fileNote struct {
	id         string // "t<seq>" — unique in the process, across all files
	seq        int64
	start, end int
	anchor     []string
	summary    string
	rationale  string
	author     string
	outdated   bool // its lines are gone from the file
}

// fileNoteSeq numbers notes. Process-global, so an id names one note
// without naming its file.
var fileNoteSeq atomic.Int64

// addNote puts a remark on lines start..end. The errors are English
// protocol prose: they go back to the agent that asked.
func (d *openFile) addNote(start, end int, summary, rationale, author string) (*fileNote, error) {
	summary = strings.TrimSpace(summary)
	switch {
	case d.src.kind != srcWorktree:
		return nil, errors.New("temporary notes go on working-tree files only")
	case !docLoaded(d) || d.p.img != nil:
		return nil, fmt.Errorf("%s has no lines to note", d.path)
	case start < 1:
		return nil, errors.New("a line number is 1-based")
	case end < start:
		return nil, errors.New("the range ends before it starts")
	case end > len(d.p.lines):
		return nil, fmt.Errorf("line %d is past the end of %s (%d lines)", end, d.path, len(d.p.lines))
	case summary == "":
		return nil, errors.New("a note needs a summary")
	case utf8.RuneCountInString(summary) > maxFileNoteSummary:
		return nil, fmt.Errorf("the summary is longer than %d characters", maxFileNoteSummary)
	case utf8.RuneCountInString(rationale) > maxFileNoteRationale:
		return nil, fmt.Errorf("the rationale is longer than %d characters", maxFileNoteRationale)
	case len(d.notes) >= maxFileNotes:
		return nil, fmt.Errorf("%s already carries %d notes", d.path, maxFileNotes)
	}
	if author == "" {
		author = "agent"
	}
	seq := fileNoteSeq.Add(1)
	n := &fileNote{
		id: "t" + strconv.FormatInt(seq, 10), seq: seq,
		start: start, end: end, anchor: d.rawLines(start, end),
		summary: summary, rationale: rationale, author: author,
	}
	d.notes = append(d.notes, n)
	d.sortNotes()
	d.backgrounded = true // esc steps aside; only X closes (and drops the notes)
	return n, nil
}

// rawLines is the source text of lines start..end (1-based, inclusive).
func (d *openFile) rawLines(start, end int) []string {
	out := make([]string, 0, end-start+1)
	for i := start; i <= end && i <= len(d.p.lines); i++ {
		out = append(out, d.p.lines[i-1].raw)
	}
	return out
}

// sortNotes keeps notes in reading order: by start line, then oldest first.
func (d *openFile) sortNotes() {
	sort.SliceStable(d.notes, func(i, j int) bool {
		a, b := d.notes[i], d.notes[j]
		if a.start != b.start {
			return a.start < b.start
		}
		return a.seq < b.seq
	})
}

// removeNote drops the note with that id; false when the file has none.
func (d *openFile) removeNote(id string) bool {
	for i, n := range d.notes {
		if n.id == id {
			d.notes = append(d.notes[:i], d.notes[i+1:]...)
			return true
		}
	}
	return false
}

// clearNotes drops every note and reports how many there were.
func (d *openFile) clearNotes() int {
	n := len(d.notes)
	d.notes = nil
	return n
}

// noteAt is the first note covering the 1-based line, or nil.
func (d *openFile) noteAt(line int) *fileNote {
	for _, n := range d.notes {
		if n.start <= line && line <= n.end {
			return n
		}
	}
	return nil
}

// findFileNote is the note with that id among the current worktree's open
// files, and the file carrying it; nil, nil when it is gone.
func (m Model) findFileNote(id string) (*openFile, *fileNote) {
	for _, d := range m.openFiles.list(m.currentWorktree) {
		for _, n := range d.notes {
			if n.id == id {
				return d, n
			}
		}
	}
	return nil, nil
}
```

In `internal/tui/open_files.go`, `touch` — change the eviction condition and its comment:

```go
// touch puts d first in wt's list, adding it when new. Over the cap it drops
// the least recently shown document that is not on screen (shown reports
// that) and carries no notes — an agent's remarks are never pushed out — and
// returns it; nil when nothing was dropped (the list then grows past the cap).
```

```go
			if !shown(l[i]) && len(l[i].notes) == 0 {
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/open-file-notes && rtk go test ./internal/tui -run 'TestFileNote|TestEviction|TestEscKeepsANotedFile|TestOpenFile|TestBackgroundNavigate' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/open-file-notes && gg add internal/tui/open_file_notes.go internal/tui/open_file_notes_test.go internal/tui/open_file.go internal/tui/open_files.go && git commit -F <msgfile>
# subject: feat(tui): temporary notes on an open file — the model and its lifetime
```

---

### Task 2: Notes follow their text across a reload

**Files:**
- Modify: `internal/tui/open_file_notes.go` (append), `internal/tui/open_file.go:131-170` (`fill`)
- Test: `internal/tui/open_file_notes_test.go` (append)

**Interfaces:**
- Consumes: Task 1's `fileNote`, `sortNotes`; `textdiff.Compare(old, new []byte, textdiff.Options{}) textdiff.Result` with `Row{Kind, LeftNo, RightNo}` and `textdiff.Same`; `fileContentLinesTok([]byte, nil) []contentLine`; `contentLine.raw`.
- Produces: `func (d *openFile) reanchorNotes(old, cur []string)` (old == nil: the content before was a placeholder); `func rawOf(lines []contentLine) []string`.

- [ ] **Step 1: Write the failing tests** (append to `open_file_notes_test.go`; add `"github.com/homeend/gigagit/internal/tui"`-free imports only — the file stays in package `tui`)

```go
// textLines is content lines for the given source lines.
func textLines(ss ...string) []contentLine {
	return fileContentLinesTok([]byte(strings.Join(ss, "\n")+"\n"), nil)
}

// reload lands new content on d the way a watch reload does.
func reload(d *openFile, ss ...string) {
	d.fill(fileContentMsg{lines: textLines(ss...), reload: true}, 10, 80)
}

func abcDoc(t *testing.T) (*openFile, *fileNote) {
	t.Helper()
	d := newOpenFile(fileSource{kind: srcWorktree}, "f.go")
	d.fill(fileContentMsg{lines: textLines("a", "b", "c", "d", "e")}, 10, 80)
	n, err := d.addNote(3, 4, "on c and d", "", "") // anchor: c, d
	if err != nil {
		t.Fatal(err)
	}
	return d, n
}

func TestFileNoteMovesWhenLinesAreInsertedAbove(t *testing.T) {
	t.Parallel()
	d, n := abcDoc(t)
	reload(d, "NEW", "a", "b", "c", "d", "e")
	if n.start != 4 || n.end != 5 || n.outdated {
		t.Fatalf("note = %d-%d outdated=%v, want 4-5 live", n.start, n.end, n.outdated)
	}
}

func TestFileNoteMovesWhenLinesAreDeletedAbove(t *testing.T) {
	t.Parallel()
	d, n := abcDoc(t)
	reload(d, "b", "c", "d", "e")
	if n.start != 2 || n.end != 3 || n.outdated {
		t.Fatalf("note = %d-%d outdated=%v, want 2-3 live", n.start, n.end, n.outdated)
	}
}

func TestFileNoteGoesOutdatedWhenItsLinesAreEditedAndRecoversOnRevert(t *testing.T) {
	t.Parallel()
	d, n := abcDoc(t)
	reload(d, "a", "b", "C-EDITED", "d", "e")
	if !n.outdated || n.start != 3 || n.end != 4 {
		t.Fatalf("note = %d-%d outdated=%v, want 3-4 outdated", n.start, n.end, n.outdated)
	}
	reload(d, "a", "b", "c", "d", "e")
	if n.outdated || n.start != 3 || n.end != 4 {
		t.Fatalf("after the revert: %d-%d outdated=%v, want 3-4 live", n.start, n.end, n.outdated)
	}
}

func TestOutdatedFileNoteIsClampedWhenTheFileShrinks(t *testing.T) {
	t.Parallel()
	d, n := abcDoc(t)
	reload(d, "a")
	if !n.outdated || n.start != 1 || n.end != 1 {
		t.Fatalf("note = %d-%d outdated=%v, want 1-1 outdated", n.start, n.end, n.outdated)
	}
}

func TestOutdatedFileNoteDoesNotJumpToAnAmbiguousCopy(t *testing.T) {
	t.Parallel()
	d := newOpenFile(fileSource{kind: srcWorktree}, "f.go")
	d.fill(fileContentMsg{lines: textLines("x", "}", "y", "}", "z")}, 10, 80)
	n, _ := d.addNote(2, 2, "this brace", "", "")
	reload(d, "x", "}!", "y", "}", "z") // its line was edited; another "}" exists
	if !n.outdated || n.start != 2 {
		t.Fatalf("note = line %d outdated=%v, want it to stay outdated on line 2", n.start, n.outdated)
	}
}

func TestFileNoteFollowsAUniqueMovedBlockAfterGoingOutdated(t *testing.T) {
	t.Parallel()
	d, n := abcDoc(t)
	reload(d, "a", "b", "e")           // c, d deleted → outdated
	reload(d, "a", "b", "e", "c", "d") // …and pasted at the end
	if n.outdated || n.start != 4 || n.end != 5 {
		t.Fatalf("note = %d-%d outdated=%v, want 4-5 live", n.start, n.end, n.outdated)
	}
}

// Review Focus 5.
func TestFileNotesSurviveAnEmptiedAndRestoredFile(t *testing.T) {
	t.Parallel()
	d, n := abcDoc(t)
	d.fill(fileContentMsg{lines: []contentLine{{text: "(empty file)"}}, reload: true}, 10, 80) // a placeholder
	if len(d.notes) != 1 || n.start != 3 {
		t.Fatalf("a placeholder fill touched the notes: %+v", n)
	}
	d.fill(fileContentMsg{err: errTestLoad, reload: true}, 10, 80)
	if len(d.notes) != 1 {
		t.Fatal("a failed load dropped the notes")
	}
	reload(d, "a", "b", "c", "d", "e")
	if n.outdated || n.start != 3 || n.end != 4 {
		t.Fatalf("after the file came back: %d-%d outdated=%v, want 3-4 live", n.start, n.end, n.outdated)
	}
	big := make([]string, 6000) // a rewrite far past any alignment guard
	for i := range big {
		big[i] = "q" + strings.Repeat("z", i%7)
	}
	reload(d, big...)
	if !n.outdated || n.start < 1 || n.end > len(big) {
		t.Fatalf("after a full rewrite: %d-%d outdated=%v", n.start, n.end, n.outdated)
	}
}
```

Add near the top of the test file: `var errTestLoad = errors.New("boom")` and import `"errors"`.

- [ ] **Step 2: Run to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/open-file-notes && rtk go test ./internal/tui -run 'TestFileNote|TestOutdatedFileNote' -count=1`
Expected: FAIL — the move/outdated assertions (notes keep their old numbers, `outdated` never set).

- [ ] **Step 3: Implement re-anchoring**

Append to `internal/tui/open_file_notes.go` (add `"github.com/homeend/gigagit/internal/textdiff"` to the imports):

```go
// rawOf is the source text of content lines.
func rawOf(lines []contentLine) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.raw
	}
	return out
}

// reanchorNotes moves every note to where its lines are in cur, the content
// that just landed. old is the content shown before (nil when that was a
// placeholder — a file deleted on disk, a failed read).
//
// A live note follows the line alignment: it moves when every one of its
// lines survived unchanged and still sits together. Otherwise it goes
// outdated and keeps its numbers, clamped to the file. An outdated note —
// and any note when there is nothing to align against — comes back only
// when its remembered text is at its old place again, or sits at exactly
// ONE place in the file: a lone "}" must never adopt some other brace.
func (d *openFile) reanchorNotes(old, cur []string) {
	var to []int
	if old != nil {
		to = sameLineMap(old, cur)
	}
	for _, n := range d.notes {
		if !n.outdated && to != nil {
			if s, ok := mapRange(to, n.start, n.end); ok {
				n.start, n.end = s, s+(n.end-n.start)
				continue
			}
		} else if s := relocate(cur, n.anchor, n.start); s > 0 {
			n.start, n.end, n.outdated = s, s+len(n.anchor)-1, false
			continue
		}
		n.outdated = true
		if n.end > len(cur) {
			n.end = len(cur)
		}
		if n.start > n.end {
			n.start = n.end
		}
		if n.start < 1 {
			n.start, n.end = 1, 1
		}
	}
	d.sortNotes()
}

// sameLineMap maps each old line (1-based index) to the new line it survived
// as, unchanged; 0 = edited or gone.
func sameLineMap(old, cur []string) []int {
	res := textdiff.Compare([]byte(strings.Join(old, "\n")+"\n"), []byte(strings.Join(cur, "\n")+"\n"), textdiff.Options{})
	to := make([]int, len(old)+1)
	for _, r := range res.Rows {
		if r.Kind == textdiff.Same && r.LeftNo >= 1 && r.LeftNo <= len(old) {
			to[r.LeftNo] = r.RightNo
		}
	}
	return to
}

// mapRange maps old lines start..end through to: ok only when every line
// survived and they are still consecutive.
func mapRange(to []int, start, end int) (int, bool) {
	if start < 1 || end >= len(to) || to[start] == 0 {
		return 0, false
	}
	for i := start; i <= end; i++ {
		if to[i] != to[start]+(i-start) {
			return 0, false
		}
	}
	return to[start], true
}

// relocate finds anchor in cur: at start when it is there, else at its one
// and only occurrence. 0 = not found, or found more than once.
func relocate(cur, anchor []string, start int) int {
	if len(anchor) == 0 {
		return 0
	}
	at := func(s int) bool { // s is 1-based
		if s < 1 || s+len(anchor)-1 > len(cur) {
			return false
		}
		for i, a := range anchor {
			if cur[s-1+i] != a {
				return false
			}
		}
		return true
	}
	if at(start) {
		return start
	}
	found := 0
	for s := 1; s+len(anchor)-1 <= len(cur); s++ {
		if at(s) {
			if found != 0 {
				return 0
			}
			found = s
		}
	}
	return found
}
```

In `internal/tui/open_file.go`, `fill`: capture the old lines before they are replaced, and re-anchor after. Directly after the line `p := d.p`:

```go
	var before []string
	reanchor := len(d.notes) > 0
	if reanchor && docLoaded(d) {
		before = rawOf(p.lines)
	}
```

and directly after the `if msg.err != nil { … } else { … }` block that assigns `p.lines`:

```go
	if reanchor && docLoaded(d) && msg.img == nil {
		d.reanchorNotes(before, rawOf(p.lines))
	}
```

- [ ] **Step 4: Run to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/open-file-notes && rtk go test ./internal/tui -run 'TestFileNote|TestOutdatedFileNote|TestOpenFile' -count=1`
Expected: PASS. If `TestFileNotesSurviveAnEmptiedAndRestoredFile` shows the placeholder line is `src: true` in this codebase, build the placeholder with `[]contentLine{{text: "(empty file)"}}` exactly as written (src false) — that is what `docLoaded` keys on.

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/open-file-notes && gg add internal/tui/open_file_notes.go internal/tui/open_file_notes_test.go internal/tui/open_file.go && git commit -F <msgfile>
# subject: feat(tui): open-file notes follow their text across a reload
```

---

### Task 3: Draw the notes — virtual rows, the gutter, and scrolling that counts them

**Files:**
- Modify: `internal/tui/diff_render.go` (`noteRowCells`), `internal/tui/content_popup.go` (struct `contentPopup`), `internal/tui/preview_select.go` (`activePreview`, `ensureCursorVisible`, `movePreviewCursor`), `internal/tui/file_preview.go` (`snapHit`, `renderPreviewBox`), `internal/tui/open_file.go` (`openFile` field, `fill`, `landPendingLine`), `internal/tui/open_file_notes.go`, `internal/tui/file_viewer.go:82,115`, `internal/tui/files_view.go:1259`, `internal/tui/mouse.go:151`
- Create: `internal/tui/open_file_notes_render_test.go`
- i18n: `internal/i18n/lang/{ja,ko,zh,ru}.toml`

**Interfaces:**
- Consumes: `noteLine`, `noteRowKind` consts, `noteBoxFrame`, `noteBodyLines(r domain.ResolvedNote, rootID string, depth, innerW int, stale bool) []noteLine`, `sanitizeLine`, `winRow{text, cls, prefix, noWrap, decorate, style, emph}`, `winOpts{prefixW}`, `previewClamp`, `previewRowMark`.
- Produces:
  - `func noteBoxCell(nl noteLine, paneW int) string`
  - `contentPopup.extraRows func(from, to int) int` — note rows hanging under lines `[from, to)` (0-based line indexes); nil = none
  - `func (p *contentPopup) rowsSpan(from, to int) int`, `clampTop(top, rowsCap int) int`, `lastVisible(rowsCap int) int`
  - `openFile.noteW int`; `func (d *openFile) gutterW() int`; `const noteGutterW = 2`
  - `func (n *fileNote) title() string`, `func (n *fileNote) boxLines(innerW int) []noteLine`
  - `func (d *openFile) syncNoteRows()` — (un)installs `p.extraRows`; called by add/remove/clear
  - `func (m Model) previewDoc(p *contentPopup) *openFile`

- [ ] **Step 1: Write the failing tests**

`internal/tui/open_file_notes_render_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// notedViewer is a 40-line file ("line 1"…) open in the full-screen viewer.
func notedViewer(t *testing.T) (Model, *openFile) {
	t.Helper()
	m := loadedNavModel(t)
	d := newOpenFile(fileSource{kind: srcWorktree}, "a.txt")
	d.fill(fileContentMsg{lines: docLines(40)}, 10, 80)
	m.openFiles.touch(m.currentWorktree, d, m.docShown)
	return m.pushLayer(&fileViewer{d}), d
}

func boxText(m Model, d *openFile, w, h int) []string {
	return strings.Split(ansi.Strip(m.renderPreviewBox(d.p, "a.txt", w, h, true, true)), "\n")
}

func indexOf(lines []string, sub string) int {
	for i, l := range lines {
		if strings.Contains(l, sub) {
			return i
		}
	}
	return -1
}

func TestFileWithoutNotesRendersExactlyAsBefore(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	before := m.renderPreviewBox(d.p, "a.txt", 80, 20, true, true)
	n, _ := d.addNote(2, 2, "s", "", "")
	d.removeNote(n.id)
	d.syncNoteRows()
	if after := m.renderPreviewBox(d.p, "a.txt", 80, 20, true, true); after != before {
		t.Fatal("a file whose notes are gone renders differently from one that never had any")
	}
}

func TestFileNoteBoxSitsUnderItsLastLine(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	if _, err := d.addNote(2, 3, "the summary", "why it matters", ""); err != nil {
		t.Fatal(err)
	}
	out := boxText(m, d, 80, 24)
	l3, top, sum, why, l4 := indexOf(out, "line 3"), indexOf(out, "lines 2-3"), indexOf(out, "the summary"), indexOf(out, "why it matters"), indexOf(out, "line 4")
	if !(l3 >= 0 && l3 < top && top < sum && sum < why && why < l4) {
		t.Fatalf("order line3=%d title=%d summary=%d rationale=%d line4=%d\n%s", l3, top, sum, why, l4, strings.Join(out, "\n"))
	}
	if !strings.Contains(out[top], "agent") || !strings.Contains(out[top], "╭") {
		t.Errorf("title row = %q, want the framed `agent · t<n> · lines 2-3`", out[top])
	}
	for _, ln := range []int{indexOf(out, "line 2"), l3} {
		if !strings.Contains(out[ln], "│ line") {
			t.Errorf("covered row %q lacks the range mark", out[ln])
		}
	}
	if strings.Contains(out[l4], "│ line") {
		t.Errorf("row %q is outside the range but wears the mark", out[l4])
	}
}

func TestOutdatedFileNoteSaysSoInItsTitle(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	n, _ := d.addNote(2, 2, "s", "", "")
	n.outdated = true
	out := boxText(m, d, 80, 24)
	if i := indexOf(out, "outdated"); i < 0 || !strings.Contains(out[i], "line 2") {
		t.Fatalf("no `… line 2 · outdated` title:\n%s", strings.Join(out, "\n"))
	}
}

// Review Focus 1.
func TestFileNoteBoxOnTheLastLineIsReachable(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	if _, err := d.addNote(40, 40, "at the very end", "", ""); err != nil {
		t.Fatal(err)
	}
	boxText(m, d, 80, 20)                        // lays the box out (noteW)
	d.p.sel = d.p.clampTop(len(d.p.lines), 16) // what `end` does in an 80x20 viewer box (16 rows)
	out := boxText(m, d, 80, 20)
	if indexOf(out, "line 40") < 0 || indexOf(out, "at the very end") < 0 || indexOf(out, "╰") < 0 {
		t.Fatalf("scrolled to the end, the last note's box is not fully shown:\n%s", strings.Join(out, "\n"))
	}
}

func TestCursorStaysVisibleBelowATallNote(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	if _, err := d.addNote(2, 2, "s", strings.Repeat("a rationale line\n", 12), ""); err != nil {
		t.Fatal(err)
	}
	m.renderPreviewBox(d.p, "a.txt", 80, 20, true, true) // lays the box out (noteW)
	rowsCap := 20 - 2 - 2
	d.p.sel, d.p.cur = 0, 0
	for i := 0; i < 6; i++ {
		d.p.cur++
		d.p.ensureCursorVisible(rowsCap)
	}
	out := boxText(m, d, 80, 20)
	if indexOf(out, "line 7") < 0 {
		t.Fatalf("the cursor line (7) left the window:\n%s", strings.Join(out, "\n"))
	}
}

// Review Focus 2.
func TestFileNoteBoxRowsAreExactlyTheBoxWidth(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	hostile := "tab\there \x1b[31mred\x1b[0m\rcr " + strings.Repeat("W", 300) + " 日本語の長い説明文"
	if _, err := d.addNote(1, 1, hostile, "one\ntwo\n\nfour "+hostile, ""); err != nil {
		t.Fatal(err)
	}
	raw := strings.Split(m.renderPreviewBox(d.p, "a.txt", 60, 40, true, true), "\n")
	for i, l := range raw {
		if w := lipgloss.Width(l); w != 60 {
			t.Fatalf("row %d is %d columns wide, want 60: %q", i, w, ansi.Strip(l))
		}
	}
	if len(raw) != 40 {
		t.Fatalf("the box is %d rows tall, want 40", len(raw))
	}
}

// Review Focus 3.
func TestFileNoteBoxSurvivesANarrowBox(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	if _, err := d.addNote(1, 2, "summary text", "rationale text", ""); err != nil {
		t.Fatal(err)
	}
	for _, w := range []int{1, 4, 6, 9, 12} {
		_ = m.renderPreviewBox(d.p, "a.txt", w, 10, true, true) // must not panic
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/open-file-notes && rtk go test ./internal/tui -run 'TestFileWithoutNotes|TestFileNoteBox|TestOutdatedFileNoteSays|TestCursorStaysVisible' -count=1`
Expected: build FAIL — `d.syncNoteRows undefined`, `d.p.clampTop undefined`.

- [ ] **Step 3: Factor the box painter** (`internal/tui/diff_render.go`)

Replace `noteRowCells` with two functions; the `switch` body moves unchanged into `noteBoxCell`:

```go
// noteBoxCell paints one box row paneW columns wide — the box alone. The
// diff view sets it in a pane (noteRowCells); the file preview draws it
// under a line (open_file_notes.go). Frame rows draw the rounded rule (the
// title sits in the top one), summary rows bold, rationale rows dim, and a
// stale box is grey throughout.
func noteBoxCell(nl noteLine, paneW int) string {
	if paneW < 4 {
		paneW = 4
	}
	s := st()
	frame := s.noteFrameUser
	if nl.agent {
		frame = s.noteFrameAgent
	}
	text := s.noteBody
	if nl.kind == noteRowSummary {
		text = s.noteSummary
	}
	if nl.stale {
		frame, text = s.noteFrameStale, s.noteDim
	}
	inner := paneW - noteBoxFrame
	var cell string
	switch nl.kind {
	// … the five cases, byte-for-byte as they are today …
	}
	return cell
}

// noteRowCells paints one box row as a full diff row: the box in the pane the
// note belongs to (old = left, new = right) and blank space in the other, so
// the note visibly hangs off one version of the file. The text was wrapped to
// the pane when the rows were laid out; truncate is only a guard against a
// width the layout has not caught up with.
func noteRowCells(nl noteLine, paneW int) string {
	if paneW < 4 {
		paneW = 4
	}
	cell := noteBoxCell(nl, paneW)
	blank := strings.Repeat(" ", paneW)
	if nl.side == model.NoteSideOld {
		return cell + "│" + blank
	}
	return blank + "│" + cell
}
```

Run `rtk go test ./internal/tui -run 'Note' -count=1` — the existing diff note tests must still pass before going on.

- [ ] **Step 4: The row-count hook and the clamps**

`internal/tui/content_popup.go`, in `contentPopup` after the `cur int` field:

```go
	// extraRows reports the display rows hanging UNDER lines [from, to) that
	// are not lines themselves — an open file's note boxes. nil (every
	// window but an annotated file preview) = none, and the pager math is
	// then exactly the plain one-row-per-line arithmetic.
	extraRows func(from, to int) int
```

`internal/tui/preview_select.go` — add, and rewrite `ensureCursorVisible`:

```go
// rowsSpan is how many display rows lines [from, to) take in a one-row-per-
// line mode: one each, plus whatever hangs under them (extraRows).
func (p *contentPopup) rowsSpan(from, to int) int {
	if to <= from {
		return 0
	}
	n := to - from
	if p.extraRows != nil {
		n += p.extraRows(from, to)
	}
	return n
}

// clampTop clamps a pager top line for this preview. It is previewClamp,
// except that rows hanging under lines count: the last screenful is the
// lowest top from which everything to the end of the file still fits, so a
// note under the last line can be scrolled fully into view.
func (p *contentPopup) clampTop(top, rowsCap int) int {
	n := len(p.lines)
	if p.extraRows == nil || p.mode == modeWrap {
		return previewClamp(top, n, rowsCap, p.mode)
	}
	maxTop, used := n, 0
	for maxTop > 0 {
		need := p.rowsSpan(maxTop-1, maxTop)
		if used+need > rowsCap {
			break
		}
		used += need
		maxTop--
	}
	if maxTop > n-1 { // even the last line with its notes is taller than the window
		maxTop = n - 1
	}
	if top > maxTop {
		top = maxTop
	}
	if top < 0 {
		top = 0
	}
	return top
}

// lastVisible is the last line whose own row is inside a rowsCap-row window
// starting at sel.
func (p *contentPopup) lastVisible(rowsCap int) int {
	last := p.sel + rowsCap - 1
	if p.extraRows != nil {
		for last > p.sel && p.rowsSpan(p.sel, last)+1 > rowsCap {
			last--
		}
	}
	return last
}
```

```go
func (p *contentPopup) ensureCursorVisible(rowsCap int) {
	if rowsCap < 1 {
		rowsCap = 1
	}
	if p.cur < p.sel {
		p.sel = p.cur
	}
	if p.cur >= p.sel+rowsCap {
		p.sel = p.cur - rowsCap + 1
	}
	// Rows hanging under the lines above the cursor push it down further.
	for p.extraRows != nil && p.sel < p.cur && p.rowsSpan(p.sel, p.cur)+1 > rowsCap {
		p.sel++
	}
	p.sel = p.clampTop(p.sel, rowsCap)
}
```

In `movePreviewCursor`, replace the block that starts `if p.cur >= p.sel+rowsCap {` with:

```go
	if last := p.lastVisible(rowsCap); p.cur > last {
		p.cur = last
		if p.cur > len(p.lines)-1 {
			p.cur = len(p.lines) - 1
		}
		// Wrap mode shows fewer than rowsCap rows when lines wrap, so the
		// "bottom row" may sit one row low there — the same approximation
		// ensureCursorVisible makes; it keeps the top legal either way.
		p.ensureCursorVisible(rowsCap)
		return
	}
```

In `activePreview`, take the note gutter off the width the callers pan/search with:

```go
	if fv, isViewer := m.topLayer().(*fileViewer); isViewer {
		rows, innerW = fv.geom(m)
		return fv.p, rows, max(innerW-fv.gutterW(), 1), true
	}
	if m.filesPreview != nil && !m.filesTreeFocused {
		return m.filesPreview.p, m.filePreviewRowsCap(), max(m.filePreviewInnerW()-m.filesPreview.gutterW(), 1), true
	}
```

Replace every preview `previewClamp(X, len(p.lines), rows, p.mode)` with `p.clampTop(X, rows)` at: `file_viewer.go:82` and `:115`, `mouse.go:151` (`fv.p.clampTop(fv.p.sel+wheel, rows)`), `files_view.go:1259`, `open_file.go:161` (`p.clampTop(keep.top, rows)`). Leave `files_worktree.go` alone (F's live preview is unregistered and takes no notes).

`internal/tui/file_preview.go`, `snapHit` — replace the `switch { case h.row < p.sel … }` block and the `previewClamp` line after it with one call (it is the same arithmetic, plus the note rows):

```go
	p.ensureCursorVisible(rowsCap)
```

`internal/tui/open_file.go`, `landPendingLine` — replace its last assignment:

```go
	p.cur = line - 1
	p.sel = p.clampTop(p.cur-rows/2, rows)
	if p.extraRows != nil {
		p.ensureCursorVisible(rows) // a note box above may have pushed the line out
	}
	return notice
```

- [ ] **Step 5: Box rows, the gutter, and the hook's owner** (`internal/tui/open_file_notes.go`)

Add `noteW int` to `openFile` in `open_file.go`, after `notes`:

```go
	// noteW is the width the note boxes were last drawn at (0 = never):
	// the pager counts their rows with it between frames.
	noteW int
```

Append to `open_file_notes.go` (imports gain `domain`, `i18n`, `model`):

```go
// noteGutterW is the column an annotated file's lines give up on the left
// for the range mark ("│ " on a line a note covers).
const noteGutterW = 2

// gutterW is the width of d's range-mark gutter: none without notes, so an
// ordinary file is laid out exactly as before.
func (d *openFile) gutterW() int {
	if len(d.notes) == 0 || !docLoaded(d) || d.p.img != nil {
		return 0
	}
	return noteGutterW
}

// title is the box's top-rule text: who, which note, which lines.
func (n *fileNote) title() string {
	t := i18n.T("%s · %s · lines %d-%d", n.author, n.id, n.start, n.end)
	if n.start == n.end {
		t = i18n.T("%s · %s · line %d", n.author, n.id, n.start)
	}
	if n.outdated {
		t = i18n.T("%s · outdated", t)
	}
	return t
}

// boxLines lays the note out as the diff view's note box, innerW columns of
// text inside the frame (<= 0: no wrapping). The body rows are the diff's
// own (noteBodyLines), so a remark reads the same in both places.
func (n *fileNote) boxLines(innerW int) []noteLine {
	frame := func(kind noteRowKind, text string) noteLine {
		return noteLine{id: n.id, rootID: n.id, kind: kind, side: model.NoteSideNew, text: text, stale: n.outdated, agent: true}
	}
	r := domain.ResolvedNote{Note: model.Note{
		ID: n.id, Source: model.NoteSourceAgent, Author: n.author, Side: model.NoteSideNew,
		Summary: n.summary, Rationale: n.rationale,
	}}
	rows := []noteLine{frame(noteRowTop, n.title()), frame(noteRowBlank, "")}
	rows = append(rows, noteBodyLines(r, n.id, 0, innerW, n.outdated)...)
	return append(rows, frame(noteRowBlank, ""), frame(noteRowBottom, ""))
}

// noteRowsUnder is the rows the boxes under lines [from, to) take (0-based
// line indexes): a box hangs under the line its note ENDS on.
func (d *openFile) noteRowsUnder(from, to int) int {
	rows := 0
	for _, n := range d.notes {
		if i := n.end - 1; i >= from && i < to {
			rows += len(n.boxLines(d.noteW - noteBoxFrame))
		}
	}
	return rows
}

// syncNoteRows installs the pager's row-count hook while d has notes and
// takes it away when the last one goes, so a file without notes is back on
// the plain one-row-per-line path.
func (d *openFile) syncNoteRows() {
	if len(d.notes) == 0 {
		d.p.extraRows = nil
		return
	}
	d.p.extraRows = d.noteRowsUnder
}

// previewDoc is the open document p belongs to — a viewer on the stack or
// the files view's preview — or nil (the help window, F's live preview).
func (m Model) previewDoc(p *contentPopup) *openFile {
	if d := m.filesPreview; d != nil && d.p == p {
		return d
	}
	if m.layers != nil {
		for _, l := range m.layers.entries {
			if fv, ok := l.(*fileViewer); ok && fv.p == p {
				return fv.openFile
			}
		}
	}
	return nil
}
```

Call `d.syncNoteRows()` at the end of `addNote` (before `return n, nil`), at the end of `clearNotes`, and in `removeNote` just before `return true`.

- [ ] **Step 6: Draw them** (`internal/tui/file_preview.go`, `renderPreviewBox`)

Replace from `p.fitImage(innerW, rowsCap)` through the end of the `for i, l := range window { … }` loop with:

```go
	p.fitImage(innerW, rowsCap) // an image document: its cells for this box
	vis := p.lines
	// An annotated file: its notes are VIRTUAL rows — never in p.lines, so
	// every line index (cursor, selection, search hit) stays a file line —
	// and its lines give up noteGutterW columns for the range mark.
	var notes []*fileNote
	gut := 0
	if d := m.previewDoc(p); d != nil && d.gutterW() > 0 {
		notes, gut = d.notes, d.gutterW()
		d.noteW = max(innerW-gut, 4)
	}
	start := p.clampTop(p.sel, rowsCap)
	wr := make([]winRow, 0, rowsCap)
	cursorOff := m.cursorStyle() == "off"
	for row := start; row < len(vis) && len(wr) < rowsCap; row++ {
		l := vis[row]
		r := winRow{text: l.text, cls: l.cls}
		if l.cells != nil {
			r.decorate = imageRowDecorator(l.cells)
		}
		// The preview rows carry no prefix, so winRow.style IS the body style —
		// no winRow.body needed here, and reverse video correctly drops the
		// class mask on a stripe that inverts (per-token foregrounds would
		// become per-token backgrounds).
		//
		// [ui] diff_cursor governs the preview cursor too; "number" falls back
		// to the band, because there is no gutter to carry a number.
		if rowStyle, marked := previewRowMark(p, row, cursorOff, l); marked {
			r.style = rowStyle
		}
		if p.search.active() {
			if hs := p.search.hitsOn(row, 0); len(hs) > 0 {
				r.emph = overlayHits(nil, 0, len([]rune(l.text)), hs)
			}
		}
		for _, n := range notes {
			if n.start <= row+1 && row+1 <= n.end {
				r.prefix = "│ " // the range mark: this line is under a note
				break
			}
		}
		wr = append(wr, r)
		for _, n := range notes {
			if n.end == row+1 {
				for _, nl := range n.boxLines(innerW - gut - noteBoxFrame) {
					wr = append(wr, fileNoteRow(nl, innerW, gut))
				}
			}
		}
	}
```

Keep the comment block about the pager that precedes `p.fitImage` as it is. Further down, the right-aligned position still reads `start+1`; change the `renderWindow` call to pass the gutter:

```go
		win := renderWindow(wr, winOpts{w: innerW, h: rowsCap, mode: p.mode, anchor: 0, hscroll: p.hscroll, charWrap: !p.prose, prefixW: gut})
```

Append to `open_file_notes.go`:

```go
// fileNoteRow is one box row as a preview window row: blank text the window
// pads to its width, painted by a decorator — so the box ignores the
// horizontal scroll and is never reflowed by wrap mode.
func fileNoteRow(nl noteLine, w, gut int) winRow {
	return winRow{noWrap: true, decorate: func(string, int, int) string {
		return strings.Repeat(" ", gut) + noteBoxCell(nl, w-gut)
	}}
}
```

Narrow-box guard: `noteBoxCell` is only safe from about 8 columns up (its title is cut to `paneW-6`). In `fileNoteRow`, when `w-gut < 8` return `winRow{noWrap: true}` (a blank row) instead — add that `if` before the `return`.

The old `end` / `window` locals are gone with the loop rewrite; delete them if the compiler reports them unused.

- [ ] **Step 7: Translations**

Use the `adding-translations` skill. Add to the `[strings]` table of each bundle (keep each file's existing ordering convention):

| key | ja | ko | zh | ru |
|-----|----|----|----|----|
| `"%s · %s · line %d"` | `"%s · %s · %d 行目"` | `"%s · %s · %d번 줄"` | `"%s · %s · 第 %d 行"` | `"%s · %s · строка %d"` |
| `"%s · %s · lines %d-%d"` | `"%s · %s · %d-%d 行目"` | `"%s · %s · %d-%d번 줄"` | `"%s · %s · 第 %d-%d 行"` | `"%s · %s · строки %d-%d"` |
| `"%s · outdated"` | `"%s · 古い"` | `"%s · 오래됨"` | `"%s · 已过期"` | `"%s · устарела"` |

- [ ] **Step 8: Run the tests**

Run: `cd /work/gigagit/.claude/worktrees/open-file-notes && rtk go test ./internal/tui -count=1`
Expected: PASS for the whole package — the new render tests AND every existing preview/viewer/search/diff-note/i18n-gate test untouched. If an existing preview test fails, the no-notes path drifted: fix the code, not the test.

- [ ] **Step 9: Commit**

```bash
cd /work/gigagit/.claude/worktrees/open-file-notes && gg add internal/tui/diff_render.go internal/tui/content_popup.go internal/tui/preview_select.go internal/tui/file_preview.go internal/tui/open_file.go internal/tui/open_file_notes.go internal/tui/open_file_notes_render_test.go internal/tui/file_viewer.go internal/tui/files_view.go internal/tui/mouse.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml && git commit -F <msgfile>
# subject: feat(tui): draw open-file notes inline — virtual rows, range gutter, note-aware scrolling
```

---

### Task 4: Keys, the `.` menu, hints, help and the switcher count

**Files:**
- Create: `internal/tui/open_file_note_keys.go`, `internal/tui/open_file_note_keys_test.go`
- Modify: `internal/tui/file_viewer.go` (`update`, after the `previewSearchKey` hook), `internal/tui/files_view.go:823-825` (after the `previewSearchKey` hook), `internal/tui/action_menu.go` (`availableActions`, after the `backgroundRow` append), `internal/tui/file_preview.go` (`renderPreviewBox` hint), `internal/tui/footer.go:277,286-289`, `internal/tui/help.go:90`, `internal/tui/sessions_popup.go:181-184`
- i18n: the four bundles

**Interfaces:**
- Consumes: `m.focusedDoc() (*openFile, bool)`, `m.activePreview()`, `m.bringToFront(d) (Model, tea.Cmd)`, `m.copyToClipboardCmd(okMsg, text)`, `m.copyRow(id, label, okMsg, text) actionRow`, `actionRow{id,key,label,copyText,run}`, Task 1/3 API.
- Produces:
  - `func (m Model) previewNoteKey(msg tea.KeyMsg) (Model, tea.Cmd, bool)`
  - `func (m Model) stepFileNote(d *openFile, dir int) (Model, tea.Cmd, bool)`
  - `func (n *fileNote) reference(path string) string` → `gg note t7 <path>:119-120` (`:119` for one line)
  - `func (m Model) fileNoteRows() []actionRow`
  - `func (m Model) noteHint(d *openFile, hint string) string`

- [ ] **Step 1: Write the failing tests**

`internal/tui/open_file_note_keys_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestNoteReferenceText(t *testing.T) {
	t.Parallel()
	d := notedDoc(t, 10)
	a, _ := d.addNote(3, 4, "s", "", "")
	b, _ := d.addNote(7, 7, "s", "", "")
	if got, want := a.reference("dir/f.go"), "gg note "+a.id+" dir/f.go:3-4"; got != want {
		t.Errorf("reference = %q, want %q", got, want)
	}
	if got, want := b.reference("f.go"), "gg note "+b.id+" f.go:7"; got != want {
		t.Errorf("reference = %q, want %q", got, want)
	}
}

func TestBraceKeysStepThroughAFilesNotes(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	d.addNote(5, 6, "first", "", "")
	d.addNote(20, 20, "second", "", "")
	m = fvKeys(t, m, key("}"))
	if d.p.cur != 4 {
		t.Fatalf("} put the cursor on line %d, want 5", d.p.cur+1)
	}
	m = fvKeys(t, m, key("}"))
	if d.p.cur != 19 {
		t.Fatalf("second } put the cursor on line %d, want 20", d.p.cur+1)
	}
	m = fvKeys(t, m, key("}"))
	if d.p.cur != 19 || m.statusMsg == "" {
		t.Fatalf("} past the last note: cursor %d status %q, want it to stay and say so", d.p.cur+1, m.statusMsg)
	}
	m = fvKeys(t, m, key("{"))
	if d.p.cur != 4 {
		t.Fatalf("{ put the cursor on line %d, want 5", d.p.cur+1)
	}
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "first") {
		t.Fatalf("the note stepped to is not on screen:\n%s", out)
	}
}

func TestBraceKeyMovesToTheNextOpenFileWithNotes(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t) // a.txt — a real file in the repo, 40 lines
	d.addNote(5, 5, "in a", "", "")
	// A loaded commit version: bringing it to the front re-reads nothing, so
	// the test needs no second file on disk. (addNote refuses a commit
	// version; the note is planted directly — only the stepping is under test.)
	other := newOpenFile(fileSource{kind: srcCommit, rev: "abc"}, "b.txt")
	other.fill(fileContentMsg{lines: docLines(10)}, 10, 80)
	other.notes = []*fileNote{{id: "t-x", seq: 1 << 40, start: 3, end: 3, summary: "in other", author: "agent"}}
	other.syncNoteRows()
	m.openFiles.touch(m.currentWorktree, other, m.docShown)
	m.openFiles.touch(m.currentWorktree, d, m.docShown) // d is the most recent again
	m = fvKeys(t, m, key("}"), key("}"))                // a's note, then onwards
	fv, ok := m.topLayer().(*fileViewer)
	if !ok || fv.openFile != other || other.p.cur != 2 {
		t.Fatalf("top = %T cursor %d, want the other file's viewer on line 3", m.topLayer(), other.p.cur+1)
	}
}

func TestDismissAndReferenceActOnTheCursorLinesNote(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	copied := new(string)
	m.clipWrite = func(_ io.Writer, s string) (string, error) { *copied = s; return "fake", nil }
	n, _ := d.addNote(5, 6, "first", "", "")
	m = fvKeys(t, m, key("r"), key("d")) // cursor on line 1: no note there
	if *copied != "" || len(d.notes) != 1 {
		t.Fatal("r/d acted on a line that carries no note")
	}
	m = fvKeys(t, m, key("}"), key("r"))
	if want := n.reference("a.txt"); *copied != want {
		t.Fatalf("copied %q, want %q", *copied, want)
	}
	m = fvKeys(t, m, key("d"))
	if len(d.notes) != 0 || d.p.extraRows != nil {
		t.Fatal("d did not dismiss the note")
	}
	if fv, ok := m.topLayer().(*fileViewer); !ok || fv.openFile != d {
		t.Fatal("dismissing the last note closed the file")
	}
}

func TestNoteRowsInTheActionMenuAndHints(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	ours := map[string]bool{"note-next": true, "note-prev": true, "copy-note-ref": true, "note-dismiss": true}
	for _, r := range availableActions(m) {
		if ours[r.id] {
			t.Fatalf("row %q offered on a file without notes", r.id)
		}
	}
	if strings.Contains(ansi.Strip(m.View()), "[}/{] notes") {
		t.Fatal("the note keys are advertised on a file without notes")
	}
	d.addNote(1, 1, "s", "", "")
	ids := map[string]bool{}
	for _, r := range availableActions(m) {
		ids[r.id] = true
	}
	for _, want := range []string{"note-next", "note-prev", "copy-note-ref", "note-dismiss"} {
		if !ids[want] {
			t.Errorf(". menu lacks %q (have %v)", want, ids)
		}
	}
	if !strings.Contains(ansi.Strip(m.View()), "[}/{] notes") {
		t.Fatal("the viewer's hint line does not advertise the note keys")
	}
}

func TestSwitcherRowShowsTheNoteCount(t *testing.T) {
	t.Parallel()
	d := notedDoc(t, 10)
	if got := openFileRowText(d, false); strings.Contains(got, "note") {
		t.Fatalf("row %q names notes on a file without any", got)
	}
	d.addNote(1, 1, "s", "", "")
	if got := openFileRowText(d, false); !strings.HasSuffix(got, "1 note") {
		t.Fatalf("row = %q, want it to end `1 note`", got)
	}
	d.addNote(2, 2, "s", "", "")
	if got := openFileRowText(d, false); !strings.HasSuffix(got, "2 notes") {
		t.Fatalf("row = %q, want it to end `2 notes`", got)
	}
}
```

Add `"io"` to the imports.

- [ ] **Step 2: Run to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/open-file-notes && rtk go test ./internal/tui -run 'TestNoteReference|TestBraceKey|TestDismissAndReference|TestNoteRowsInTheActionMenu|TestSwitcherRowShows' -count=1`
Expected: build FAIL — `a.reference undefined`.

- [ ] **Step 3: Implement the keys**

Create `internal/tui/open_file_note_keys.go`:

```go
package tui

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// The user's side of an open file's temporary notes: } / { step through
// them (then on to the next open file that has any — the diff view's rule),
// d dismisses the note under the cursor, r copies a reference to it.

// reference is what the user pastes to the agent to talk about this note:
// the id (gg session note show resolves it) plus path:lines, which still
// mean something once the note is gone.
func (n *fileNote) reference(path string) string {
	ref := "gg note " + n.id + " " + path + ":" + strconv.Itoa(n.start)
	if n.end != n.start {
		ref += "-" + strconv.Itoa(n.end)
	}
	return ref
}

// anyFileNotes reports whether any open file of this worktree has notes.
func (m Model) anyFileNotes() bool {
	for _, d := range m.openFiles.list(m.currentWorktree) {
		if len(d.notes) > 0 {
			return true
		}
	}
	return false
}

// previewNoteKey gives the focused open file's notes first refusal on a key.
// It declines everything while no open file has notes, and d / r on a line
// that carries none, so those keys keep whatever else they mean there.
func (m Model) previewNoteKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	d, ok := m.focusedDoc()
	if !ok || !m.anyFileNotes() {
		return m, nil, false
	}
	switch msg.String() {
	case "}":
		return m.stepFileNote(d, 1)
	case "{":
		return m.stepFileNote(d, -1)
	case "d":
		if n := d.noteAt(d.p.cur + 1); n != nil {
			d.removeNote(n.id)
			m.statusMsg = i18n.T("note %s dismissed", n.id)
			return m, nil, true
		}
	case "r":
		if n := d.noteAt(d.p.cur + 1); n != nil {
			return m, m.copyToClipboardCmd(i18n.T("Copied note reference %s", n.id), n.reference(d.path)), true
		}
	}
	return m, nil, false
}

// stepFileNote moves to the next (dir > 0) or previous note: inside d first,
// by start line from the cursor; past its last one, the next open file with
// notes comes to the front on its first (last) note.
func (m Model) stepFileNote(d *openFile, dir int) (Model, tea.Cmd, bool) {
	line := d.p.cur + 1
	var hit *fileNote
	for _, n := range d.notes {
		if dir > 0 && n.start > line {
			hit = n
			break
		}
		if dir < 0 && n.start < line {
			hit = n // keep going: the LAST one above the cursor
		}
	}
	if hit != nil {
		_, rows, _, _ := m.activePreview()
		d.p.cur = hit.start - 1
		d.p.ensureCursorVisible(rows)
		// Bring the box in too, as far as that keeps the cursor line on screen.
		for d.p.sel < d.p.cur && d.p.rowsSpan(d.p.sel, hit.end) > rows {
			d.p.sel++
		}
		return m, nil, true
	}
	l := m.openFiles.list(m.currentWorktree)
	at := 0
	for i, e := range l {
		if e == d {
			at = i
		}
	}
	for i := 1; i < len(l); i++ {
		e := l[((at+dir*i)%len(l)+len(l))%len(l)]
		if len(e.notes) == 0 {
			continue
		}
		target := e.notes[0]
		if dir < 0 {
			target = e.notes[len(e.notes)-1]
		}
		e.pendingLine = target.start
		nm, load := m.bringToFront(e)
		if load == nil { // already loaded and not re-read: land the line now
			rows, _ := nm.viewerGeom()
			e.landPendingLine(rows)
		}
		return nm, load, true
	}
	if len(d.notes) == 0 {
		return m, nil, false // nothing anywhere to step to: not our key
	}
	m.statusMsg = i18n.T("no more notes")
	return m, nil, true
}

// fileNoteRows are the . menu's rows for the focused open file's notes —
// the keys for the mouse. None while it has no notes.
func (m Model) fileNoteRows() []actionRow {
	d, ok := m.focusedDoc()
	if !ok || len(d.notes) == 0 {
		return nil
	}
	rows := []actionRow{
		{id: "note-next", key: "}", label: i18n.T("Next note"), run: func(m Model) (tea.Model, tea.Cmd) {
			nm, cmd, _ := m.stepFileNote(d, 1)
			return nm, cmd
		}},
		{id: "note-prev", key: "{", label: i18n.T("Previous note"), run: func(m Model) (tea.Model, tea.Cmd) {
			nm, cmd, _ := m.stepFileNote(d, -1)
			return nm, cmd
		}},
	}
	n := d.noteAt(d.p.cur + 1)
	if n == nil {
		n = d.notes[0] // the menu has no cursor of its own: offer the first note
	}
	ref := m.copyRow("copy-note-ref", i18n.T("Copy note reference"), i18n.T("Copied note reference %s", n.id), n.reference(d.path))
	ref.key = "r"
	return append(rows, ref, actionRow{id: "note-dismiss", key: "d", label: i18n.T("Dismiss note"), run: func(m Model) (tea.Model, tea.Cmd) {
		d.removeNote(n.id)
		m.statusMsg = i18n.T("note %s dismissed", n.id)
		return m, nil
	}})
}

// noteHint leads hint with the note keys while d carries notes.
func (m Model) noteHint(d *openFile, hint string) string {
	if d == nil || len(d.notes) == 0 {
		return hint
	}
	return i18n.T("[}/{] notes  [d] dismiss  [r] reference") + "  " + hint
}
```

The test `TestNoteRowsInTheActionMenuAndHints` puts the note on line 1 with the cursor on line 1, so `noteAt` is non-nil there; the `d.notes[0]` fallback covers a menu opened from a line without a note.

- [ ] **Step 4: Wire the hook, the menu, the hints, help and the switcher**

`internal/tui/file_viewer.go`, `update` — after the `previewSearchKey` block:

```go
	if nm, cmd, ok := m.previewNoteKey(msg); ok {
		return nm, cmd
	}
```

`internal/tui/files_view.go` — after the `previewSearchKey` block at lines 823-825:

```go
	// …then the focused preview's notes: } { step, d / r act on the note
	// under the cursor. It declines a key it has nothing to do with.
	if nm, cmd, handled := m.previewNoteKey(msg); handled {
		return nm, cmd
	}
```

`internal/tui/action_menu.go`, `availableActions` — right after the `backgroundRow` append:

```go
		rows = append(rows, m.fileNoteRows()...)
```

`internal/tui/file_preview.go`, `renderPreviewBox` — just before `if p.lsel.on {` in the hint block:

```go
	hint = m.noteHint(m.previewDoc(p), hint)
```

`internal/tui/footer.go` — wrap the three `file: …` returns of a focused right-column preview (line 277, and the `backgrounded` / plain pair at 286-289) so the bottom bar advertises the keys: `return m.noteHint(m.filesPreview, i18n.T("file: …")), true` (the `i18n.T` call and its literal key stay exactly as they are — only the wrapper is new).

`internal/tui/help.go` — add a row after the `ctrl+]` row (line 90):

```go
		r("} / {", i18n.T("in a file viewer or preview: the next / previous note an agent left on the open files (past a file's last note, the next open file that has notes); d dismisses the note under the cursor, r copies a reference to it for the agent. The notes are temporary: closing the file (X) drops them")),
```

`internal/tui/sessions_popup.go`, `openFileRowText` — after the `if docLoaded(d) { row += … }` block:

```go
	switch n := len(d.notes); {
	case n == 1:
		row += "  " + i18n.T("1 note")
	case n > 1:
		row += "  " + i18n.T("%d notes", n)
	}
```

The `srcCommit`/`srcShelf` returns below it never see notes (working-tree files only), and a working-tree row returns `row` — confirm the function's final `return row` follows the switch so the count is the suffix.

- [ ] **Step 5: Translations**

First `grep -n '"1 note"\|"%d notes"' internal/i18n/lang/ja.toml` — reuse a key that already exists instead of adding a duplicate.

| key | ja | ko | zh | ru |
|-----|----|----|----|----|
| `"note %s dismissed"` | `"ノート %s を閉じました"` | `"노트 %s 닫음"` | `"已关闭备注 %s"` | `"Заметка %s скрыта"` |
| `"Copied note reference %s"` | `"ノート参照 %s をコピーしました"` | `"노트 참조 %s 복사됨"` | `"已复制备注引用 %s"` | `"Ссылка на заметку %s скопирована"` |
| `"no more notes"` | `"これ以上ノートはありません"` | `"더 이상 노트가 없습니다"` | `"没有更多备注"` | `"заметок больше нет"` |
| `"Next note"` | `"次のノート"` | `"다음 노트"` | `"下一条备注"` | `"Следующая заметка"` |
| `"Previous note"` | `"前のノート"` | `"이전 노트"` | `"上一条备注"` | `"Предыдущая заметка"` |
| `"Copy note reference"` | `"ノート参照をコピー"` | `"노트 참조 복사"` | `"复制备注引用"` | `"Копировать ссылку на заметку"` |
| `"Dismiss note"` | `"ノートを閉じる"` | `"노트 닫기"` | `"关闭备注"` | `"Скрыть заметку"` |
| `"[}/{] notes  [d] dismiss  [r] reference"` | `"[}/{] ノート  [d] 閉じる  [r] 参照"` | `"[}/{] 노트  [d] 닫기  [r] 참조"` | `"[}/{] 备注  [d] 关闭  [r] 引用"` | `"[}/{] заметки  [d] скрыть  [r] ссылка"` |
| `"1 note"` | `"ノート 1 件"` | `"노트 1개"` | `"1 条备注"` | `"1 заметка"` |
| `"%d notes"` | `"ノート %d 件"` | `"노트 %d개"` | `"%d 条备注"` | `"заметок: %d"` |
| the help row's English text (copy it verbatim as the key) | `"ファイルビューアまたはプレビューで: エージェントが開いているファイルに残した次 / 前のノート (ファイルの最後のノートを過ぎると、ノートのある次の開いているファイルへ); d はカーソル下のノートを閉じ、r はエージェント向けの参照をコピーします。ノートは一時的で、ファイルを閉じる (X) と消えます"` | `"파일 뷰어 또는 미리보기에서: 에이전트가 열린 파일에 남긴 다음 / 이전 노트 (파일의 마지막 노트를 지나면 노트가 있는 다음 열린 파일로); d는 커서 아래 노트를 닫고, r은 에이전트에게 줄 참조를 복사합니다. 노트는 임시이며 파일을 닫으면 (X) 사라집니다"` | `"在文件查看器或预览中: 代理在已打开文件上留下的下一条 / 上一条备注 (过了文件的最后一条备注后, 转到下一个带备注的已打开文件); d 关闭光标处的备注, r 复制给代理的引用。备注是临时的: 关闭文件 (X) 即丢弃"` | `"в просмотре файла: следующая / предыдущая заметка, оставленная агентом в открытых файлах (после последней заметки файла — следующий открытый файл с заметками); d скрывает заметку под курсором, r копирует ссылку на неё для агента. Заметки временные: закрытие файла (X) удаляет их"` |

- [ ] **Step 6: Run the tests**

Run: `cd /work/gigagit/.claude/worktrees/open-file-notes && rtk go test ./internal/tui ./internal/i18n -count=1`
Expected: PASS (including the i18n AST gates and `TestFileViewerActionMenuIsTheViewersOwn`, which runs on a file without notes).

- [ ] **Step 7: Commit**

```bash
cd /work/gigagit/.claude/worktrees/open-file-notes && gg add internal/tui/open_file_note_keys.go internal/tui/open_file_note_keys_test.go internal/tui/file_viewer.go internal/tui/files_view.go internal/tui/action_menu.go internal/tui/file_preview.go internal/tui/footer.go internal/tui/help.go internal/tui/sessions_popup.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml && git commit -F <msgfile>
# subject: feat(tui): open-file notes — } { step, d dismiss, r reference, menu rows, hints, switcher count
```

---

### Task 5: The steer protocol and the TUI's handlers

**Files:**
- Modify: `internal/steer/steer.go` (`Command`, `Reply`, `OpenFile`), `internal/tui/steer.go` (`applySteer`, `steerEnumRefusal`), `internal/tui/model.go` (Update, next to `case contentLandedMsg:`), `internal/tui/open_files.go` (`openFilesProto`)
- Create: `internal/tui/steer_file_notes.go`, `internal/tui/steer_file_notes_test.go`

**Interfaces:**
- Consumes: `steerOK`, `steerFail`, `m.answerSteer`, `m.findOpenFile(id, path)`, `m.registerDocEv(d) (Model, *openFile)`, `evictedPath(ev) string`, `m.loadDoc(d) tea.Cmd` (its message is a `fileContentMsg`), `m.svc.WorktreeFilesPresent(ctx, []string) (map[string]bool, error)`, `busyOr`, `updateThreadCtx(updateThreadGitTimeout)`, `domain.SameCheckout`, `m.snapshotWorktree`, Task 1's `addNote`/`removeNote`/`clearNotes`/`findFileNote`.
- Produces (wire):
  - `steer.Command` fields `NoteID string json:"note_id,omitempty"`, `Summary string json:"summary,omitempty"`, `Rationale string json:"rationale,omitempty"`, `Author string json:"author,omitempty"`
  - `steer.FileNote{ID, FileID, Path string; Start, End int; Summary, Rationale, Author string; Outdated bool; Text []string}`
  - `steer.Reply.Notes []FileNote json:"notes,omitempty"`, `steer.OpenFile.Notes int json:"notes,omitempty"`
  - commands `note_add` (File|FileID, Start, End, Summary, Rationale, Author, Worktree), `note_list` (optional File|FileID), `note_show` (NoteID), `note_rm` (NoteID, or File|FileID = all of that file)
  - reply details: `noted <path>:<start>[-<end>] as <id>`, `no notes`, `removed <id>`, `removed <n> notes from <path>`; errors `no open file <x>`, `no note <id>`, `<path> was closed before the note landed`

- [ ] **Step 1: Write the failing tests**

`internal/tui/steer_file_notes_test.go`:

```go
package tui

import (
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/steer"
)

func noteAdd(id, file string, start, end int, summary string) steer.Command {
	return steer.Command{ID: id, Cmd: "note_add", File: file, Start: start, End: end, Summary: summary, Wait: true}
}

func awaitNote(t *testing.T, m Model, id string) steer.Reply {
	t.Helper()
	r, ok := steer.AwaitReply(m.steerDir, id, time.Second)
	if !ok {
		t.Fatalf("no reply to %s", id)
	}
	return r
}

func TestNoteAddOpensTheFileInTheBackgroundAndAnswersWithTheNote(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.filterTyping = true // never refused: nothing on screen moves
	nm, cmd := m.applySteer(noteAdd("n-1", "a.txt", 18, 19, "the edited line"))
	nm = pumpAll(t, nm, cmd)
	if layerOf[*fileViewer](nm) != nil || nm.filesPreview != nil {
		t.Fatal("note add put the file on screen")
	}
	d := nm.openFiles.find(nm.currentWorktree, docKey(fileSource{kind: srcWorktree}, "a.txt"))
	if d == nil || len(d.notes) != 1 {
		t.Fatalf("doc = %+v, want a.txt open with one note", d)
	}
	r := awaitNote(t, nm, "n-1")
	n := d.notes[0]
	if !r.OK || r.Detail != "noted a.txt:18-19 as "+n.id || len(r.Notes) != 1 {
		t.Fatalf("reply = %+v", r)
	}
	if w := r.Notes[0]; w.ID != n.id || w.FileID != d.id() || w.Path != "a.txt" || w.Start != 18 || w.End != 19 || w.Summary != "the edited line" || w.Author != "agent" {
		t.Fatalf("wire note = %+v", w)
	}
}

func TestNoteAddOnAnOpenLoadedFileAnswersAtOnce(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	top, cur := d.p.sel, d.p.cur
	nm, cmd := m.applySteer(noteAdd("n-2", "a.txt", 5, 5, "s"))
	runSteerCmd(t, cmd)
	r := awaitNote(t, nm, "n-2")
	if !r.OK || len(d.notes) != 1 || d.p.sel != top || d.p.cur != cur {
		t.Fatalf("reply=%+v notes=%d top=%d cur=%d — want one note, the reader's place untouched", r, len(d.notes), d.p.sel, d.p.cur)
	}
}

func TestNoteAddRefusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		c    steer.Command
		want string
	}{
		{"missing file", noteAdd("r-1", "nope.txt", 1, 1, "s"), "nope.txt is not in the working tree"},
		{"past the end", noteAdd("r-2", "a.txt", 39, 41, "s"), "line 41 is past the end of a.txt (40 lines)"},
		{"unknown id", steer.Command{ID: "r-3", Cmd: "note_add", FileID: "f999999", Start: 1, End: 1, Summary: "s", Wait: true}, "no open file f999999"},
		{"other worktree", func() steer.Command {
			c := noteAdd("r-4", "a.txt", 1, 1, "s")
			c.Worktree = "/somewhere/else"
			return c
		}(), ""},
	} {
		m := loadedNavModel(t)
		nm, cmd := m.applySteer(tc.c)
		nm = pumpAll(t, nm, cmd)
		r := awaitNote(t, nm, tc.c.ID)
		if r.OK || (tc.want != "" && r.Error != tc.want) {
			t.Errorf("%s: reply = %+v, want the error %q", tc.name, r, tc.want)
		}
	}
	for _, tc := range []struct {
		c    steer.Command
		want string
	}{
		{steer.Command{Cmd: "note_add", Start: 1, End: 1, Summary: "s"}, "note_add needs a file"},
		{steer.Command{Cmd: "note_add", File: "a.txt", Summary: "s"}, "a line number is 1-based"},
		{steer.Command{Cmd: "note_add", File: "a.txt", Start: 3, End: 2, Summary: "s"}, "the range ends before it starts"},
		{steer.Command{Cmd: "note_add", File: "a.txt", Start: 1, End: 1}, "a note needs a summary"},
		{steer.Command{Cmd: "note_show"}, "note_show needs a note id"},
		{steer.Command{Cmd: "note_rm"}, "note_rm needs a note id or a file"},
	} {
		if got := steerEnumRefusal(tc.c); got != tc.want {
			t.Errorf("steerEnumRefusal(%+v) = %q, want %q", tc.c, got, tc.want)
		}
	}
}

// Review Focus 4.
func TestNoteAddOnAFileClosedBeforeItLandsFails(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	nm, cmd := m.applySteer(noteAdd("c-1", "a.txt", 1, 1, "s"))
	d := nm.openFiles.find(nm.currentWorktree, docKey(fileSource{kind: srcWorktree}, "a.txt"))
	nm = nm.closeDoc(d) // the user pressed x before the load landed
	nm = pumpAll(t, nm, cmd)
	r := awaitNote(t, nm, "c-1")
	if r.OK || r.Error != "a.txt was closed before the note landed" {
		t.Fatalf("reply = %+v", r)
	}
}

func TestNoteListShowAndRemove(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	a, _ := d.addNote(2, 3, "first", "why", "")
	b, _ := d.addNote(9, 9, "second", "", "claude")

	nm, cmd := m.applySteer(steer.Command{ID: "l-1", Cmd: "note_list", Wait: true})
	runSteerCmd(t, cmd)
	if r := awaitNote(t, nm, "l-1"); !r.OK || len(r.Notes) != 2 || r.Notes[0].ID != a.id || r.Notes[1].Author != "claude" {
		t.Fatalf("list = %+v", r)
	}
	if got := nm.openFilesProto(); len(got) != 1 || got[0].Notes != 2 {
		t.Fatalf("open files = %+v, want the note count 2", got)
	}

	nm, cmd = nm.applySteer(steer.Command{ID: "s-1", Cmd: "note_show", NoteID: a.id, Wait: true})
	runSteerCmd(t, cmd)
	r := awaitNote(t, nm, "s-1")
	if !r.OK || len(r.Notes) != 1 || len(r.Notes[0].Text) != 2 || r.Notes[0].Text[0] != "line 2" || r.Notes[0].Rationale != "why" {
		t.Fatalf("show = %+v", r)
	}

	nm, cmd = nm.applySteer(steer.Command{ID: "d-1", Cmd: "note_rm", NoteID: a.id, Wait: true})
	runSteerCmd(t, cmd)
	if r := awaitNote(t, nm, "d-1"); !r.OK || r.Detail != "removed "+a.id || len(d.notes) != 1 {
		t.Fatalf("rm = %+v notes=%d", r, len(d.notes))
	}
	nm, cmd = nm.applySteer(steer.Command{ID: "s-2", Cmd: "note_show", NoteID: a.id, Wait: true})
	runSteerCmd(t, cmd)
	if r := awaitNote(t, nm, "s-2"); r.OK || r.Error != "no note "+a.id {
		t.Fatalf("show of a removed note = %+v", r)
	}
	nm, cmd = nm.applySteer(steer.Command{ID: "d-2", Cmd: "note_rm", File: "a.txt", Wait: true})
	runSteerCmd(t, cmd)
	if r := awaitNote(t, nm, "d-2"); !r.OK || r.Detail != "removed 1 notes from a.txt" || len(d.notes) != 0 {
		t.Fatalf("clear = %+v", r)
	}
	_ = b
	nm, cmd = nm.applySteer(steer.Command{ID: "l-2", Cmd: "note_list", Wait: true})
	runSteerCmd(t, cmd)
	if r := awaitNote(t, nm, "l-2"); !r.OK || len(r.Notes) != 0 || r.Detail != "no notes" {
		t.Fatalf("empty list = %+v", r)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/open-file-notes && rtk go test ./internal/tui -run 'TestNoteAdd|TestNoteListShow' -count=1`
Expected: build FAIL — `unknown field Summary in struct literal of type steer.Command`.

- [ ] **Step 3: The wire types** (`internal/steer/steer.go`)

Update the `Cmd` field comment to list `"note_add" | "note_list" | "note_show" | "note_rm"` and add to `Command`, after `FileID`:

```go
	// NoteID names a temporary open-file note ("t<n>") for note_show and
	// note_rm. Summary/Rationale/Author are note_add's text; File/FileID
	// name its file and Start/End its 1-based line range.
	NoteID    string `json:"note_id,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Rationale string `json:"rationale,omitempty"`
	Author    string `json:"author,omitempty"`
```

To `Reply`, after `Files`:

```go
	// Notes is the note verbs' answer: the note added, the notes listed, or
	// the one shown (with Text).
	Notes []FileNote `json:"notes,omitempty"`
```

To `OpenFile`, after `State`:

```go
	Notes  int    `json:"notes,omitempty"` // temporary notes an agent left on it
```

And the new type:

```go
// FileNote is one temporary note on a file open in a live TUI. It lives in
// that TUI's memory only and is gone when the file is closed. Protocol data.
type FileNote struct {
	ID        string   `json:"id"`      // "t<n>"
	FileID    string   `json:"file_id"` // the open file's "f<n>"
	Path      string   `json:"path"`
	Start     int      `json:"start"` // 1-based, as the lines sit NOW
	End       int      `json:"end"`
	Summary   string   `json:"summary"`
	Rationale string   `json:"rationale,omitempty"`
	Author    string   `json:"author,omitempty"`
	Outdated  bool     `json:"outdated,omitempty"` // its lines are gone from the file
	Text      []string `json:"text,omitempty"`     // note_show only: the lines it sits on
}
```

- [ ] **Step 4: Dispatch and validation** (`internal/tui/steer.go`)

In `applySteer`'s first `switch`, add a case (import `strings` if the file lacks it):

```go
	case strings.HasPrefix(c.Cmd, "note_"):
		return m.steerFileNote(c)
```

and extend the comment above the switch: `// …and neither do an agent's temporary notes (steer_file_notes.go).`

In `steerEnumRefusal`, before the `file_focus` check:

```go
	switch c.Cmd {
	case "note_add":
		switch {
		case c.FileID == "" && c.File == "":
			return "note_add needs a file"
		case c.Start < 1:
			return "a line number is 1-based"
		case c.End < c.Start:
			return "the range ends before it starts"
		case strings.TrimSpace(c.Summary) == "":
			return "a note needs a summary"
		}
	case "note_show":
		if c.NoteID == "" {
			return "note_show needs a note id"
		}
	case "note_rm":
		if c.NoteID == "" && c.FileID == "" && c.File == "" {
			return "note_rm needs a note id or a file"
		}
	}
```

- [ ] **Step 5: The handlers**

Create `internal/tui/steer_file_notes.go`:

```go
package tui

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// The temporary-note steer verbs (gg session note add|list|show|rm). Like
// the open-files list verbs they never move the screen, so applySteer runs
// them past steerRefusal: a note appears under its line wherever the user
// is. Reply prose is English protocol text.

// noteLandedMsg carries a note_add that had to open (or re-read) its file
// first: the note can only be checked against loaded lines. tag names the
// document, which may have been closed by the time the load lands.
type noteLandedMsg struct {
	load fileContentMsg
	cmd  steer.Command
	tag  string
	path string
	lead string // "; closed <path> (20 files open)" when the open pushed a file out
}

func (m Model) steerFileNote(c steer.Command) (Model, tea.Cmd) {
	switch c.Cmd {
	case "note_add":
		return m.steerNoteAdd(c)
	case "note_list":
		return m.steerNoteList(c)
	case "note_show":
		return m.steerNoteShow(c)
	case "note_rm":
		return m.steerNoteRm(c)
	}
	return m, m.answerSteer(c, steerFail(c, "unknown command "+strconv.Quote(c.Cmd)))
}

// fileNoteProto is a note in its wire form; text adds the lines it sits on.
func fileNoteProto(d *openFile, n *fileNote, text bool) steer.FileNote {
	w := steer.FileNote{ID: n.id, FileID: d.id(), Path: d.path, Start: n.start, End: n.end,
		Summary: n.summary, Rationale: n.rationale, Author: n.author, Outdated: n.outdated}
	if text && docLoaded(d) {
		w.Text = d.rawLines(n.start, n.end)
	}
	return w
}

// steerNoteAdd puts a note on an open working-tree file, opening the file
// in the background first when it is not open (or not loaded yet).
func (m Model) steerNoteAdd(c steer.Command) (Model, tea.Cmd) {
	// Another worktree is refused, not asked about: the switch notice would
	// be the very screen change a note promises not to make.
	if c.Worktree != "" && !domain.SameCheckout(c.Worktree, m.snapshotWorktree) {
		return m, m.answerSteer(c, steerFail(c, "gg is showing worktree "+m.snapshotWorktree+", not "+c.Worktree))
	}
	src := fileSource{kind: srcWorktree}
	var d *openFile
	if c.FileID != "" {
		if d = m.findOpenFile(c.FileID, ""); d == nil || d.id() != c.FileID {
			return m, m.answerSteer(c, steerFail(c, "no open file "+c.FileID))
		}
	} else {
		d = m.openFiles.find(m.currentWorktree, docKey(src, c.File))
	}
	if d != nil && docLoaded(d) && !d.loading {
		return m.finishNoteAdd(c, d, "")
	}
	path := c.File
	if d != nil {
		path = d.path
	}
	if d == nil {
		ctx, cancel := updateThreadCtx(updateThreadGitTimeout)
		defer cancel()
		present, err := m.svc.WorktreeFilesPresent(ctx, []string{path})
		if err := busyOr(err); err != nil {
			return m, m.answerSteer(c, steerFail(c, "checking "+path+": "+err.Error()))
		}
		if !present[path] {
			return m, m.answerSteer(c, steerFail(c, path+" is not in the working tree"))
		}
		d = newOpenFile(src, path)
	} else {
		d.keepPlace()
	}
	status := m.statusMsg
	m, ev := m.registerDocEv(d)
	m.statusMsg = status // an agent's note never takes over the status line
	lead := ""
	if p := evictedPath(ev); p != "" {
		lead = "; closed " + p + " (" + strconv.Itoa(maxOpenFiles) + " files open)"
	}
	load, tag := m.loadDoc(d), d.tag
	return m, func() tea.Msg {
		return noteLandedMsg{load: load().(fileContentMsg), cmd: c, tag: tag, path: path, lead: lead}
	}
}

// finishNoteAdd adds the note to a loaded document and answers.
func (m Model) finishNoteAdd(c steer.Command, d *openFile, lead string) (Model, tea.Cmd) {
	n, err := d.addNote(c.Start, c.End, c.Summary, c.Rationale, c.Author)
	if err != nil {
		return m, m.answerSteer(c, steerFail(c, err.Error()))
	}
	where := d.path + ":" + strconv.Itoa(n.start)
	if n.end != n.start {
		where += "-" + strconv.Itoa(n.end)
	}
	r := steerOK(c, "noted "+where+" as "+n.id+lead)
	r.Notes = []steer.FileNote{fileNoteProto(d, n, false)}
	return m, m.answerSteer(c, r)
}

// noteLanded finishes a note_add whose file load just arrived.
func (m Model) noteLanded(msg noteLandedMsg) (Model, tea.Cmd) {
	tm, fill := m.Update(msg.load)
	m = tm.(Model)
	d := m.openFiles.findTag(msg.tag)
	switch {
	case d == nil:
		return m, tea.Batch(fill, m.answerSteer(msg.cmd, steerFail(msg.cmd, msg.path+" was closed before the note landed")))
	case msg.load.err != nil:
		return m, tea.Batch(fill, m.answerSteer(msg.cmd, steerFail(msg.cmd, "reading "+msg.path+": "+msg.load.err.Error())))
	}
	m, reply := m.finishNoteAdd(msg.cmd, d, msg.lead)
	return m, tea.Batch(fill, reply)
}

// steerNoteList answers with every note of the worktree's open files, or of
// the one file named.
func (m Model) steerNoteList(c steer.Command) (Model, tea.Cmd) {
	docs := m.openFiles.list(m.currentWorktree)
	if c.FileID != "" || c.File != "" {
		d := m.findOpenFile(c.FileID, c.File)
		if d == nil {
			name := c.FileID
			if name == "" {
				name = c.File
			}
			return m, m.answerSteer(c, steerFail(c, "no open file "+name))
		}
		docs = []*openFile{d}
	}
	r := steerOK(c, "")
	for _, d := range docs {
		for _, n := range d.notes {
			r.Notes = append(r.Notes, fileNoteProto(d, n, false))
		}
	}
	if len(r.Notes) == 0 {
		r.Detail = "no notes"
	}
	return m, m.answerSteer(c, r)
}

// steerNoteShow answers with one note and the lines it sits on now.
func (m Model) steerNoteShow(c steer.Command) (Model, tea.Cmd) {
	d, n := m.findFileNote(c.NoteID)
	if n == nil {
		return m, m.answerSteer(c, steerFail(c, "no note "+c.NoteID))
	}
	r := steerOK(c, "")
	r.Notes = []steer.FileNote{fileNoteProto(d, n, true)}
	return m, m.answerSteer(c, r)
}

// steerNoteRm removes one note by id, or every note of a file.
func (m Model) steerNoteRm(c steer.Command) (Model, tea.Cmd) {
	if c.NoteID != "" {
		d, n := m.findFileNote(c.NoteID)
		if n == nil {
			return m, m.answerSteer(c, steerFail(c, "no note "+c.NoteID))
		}
		d.removeNote(n.id)
		return m, m.answerSteer(c, steerOK(c, "removed "+n.id))
	}
	d := m.findOpenFile(c.FileID, c.File)
	if d == nil {
		name := c.FileID
		if name == "" {
			name = c.File
		}
		return m, m.answerSteer(c, steerFail(c, "no open file "+name))
	}
	return m, m.answerSteer(c, steerOK(c, "removed "+strconv.Itoa(d.clearNotes())+" notes from "+d.path))
}
```

`internal/tui/model.go`, Update — next to `case contentLandedMsg:`:

```go
	case noteLandedMsg:
		return m.noteLanded(msg)
```

`internal/tui/open_files.go`, `openFilesProto` — before `out = append(out, f)`:

```go
		f.Notes = len(d.notes)
```

`steerNoteAdd` passes `c.End`; the CLI always sends both ends, and `steerEnumRefusal` already refused `End < Start`.

- [ ] **Step 6: Run the tests**

Run: `cd /work/gigagit/.claude/worktrees/open-file-notes && rtk go test ./internal/tui ./internal/steer -count=1`
Expected: PASS. If `TestNoteAddOnAFileClosedBeforeItLandsFails` finds the document still present (a closed document's load is a stale load and `findTag` must miss it), check that the test closes the doc BEFORE `pumpAll` runs the load command.

- [ ] **Step 7: Commit**

```bash
cd /work/gigagit/.claude/worktrees/open-file-notes && gg add internal/steer/steer.go internal/tui/steer.go internal/tui/model.go internal/tui/open_files.go internal/tui/steer_file_notes.go internal/tui/steer_file_notes_test.go && git commit -F <msgfile>
# subject: feat(steer,tui): note_add|list|show|rm — an agent's temporary notes on open files
```

---

### Task 6: `gg session note`

**Files:**
- Create: `internal/cli/session_note.go`, `internal/cli/session_note_test.go`
- Modify: `internal/cli/session.go` (`runSession`: usage string + `case "note"`)

**Interfaces:**
- Consumes: `steerLive(dir, c, both, noWait, stdout, stderr) (steer.Reply, int, bool)`, `routeFor(dir) sessionRoute{tuiOK, webOK}`, `preferredInbox(dir)`, `parseSteerFlags(fs, args)`, `callerWorktree(svc)`, `fileIDPattern`, test helpers `livePresence`, `liveWebPresence`, `answer`.
- Produces: `func sessionNote(dir string, svc *domain.Service, args []string, stdout, stderr io.Writer) int`.

CLI contract:

```
gg session note add <path|file-id>:<start>[-<end>] --summary "…" [--rationale "…"] [--author <name>] [--json]
gg session note list [<path>|<file-id>] [--json]
gg session note show <note-id> [--json]
gg session note rm <note-id>
gg session note clear <path>|<file-id>
```

`<path>` is repo-relative in git slash form, passed through as-is (as `gg session files focus` takes it). Exit 0 ok; 1 no live TUI / refused; 2 usage.

- [ ] **Step 1: Write the failing tests**

`internal/cli/session_note_test.go`:

```go
package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/steer"
)

var twoNotes = []steer.FileNote{
	{ID: "t3", FileID: "f1", Path: "src/a.go", Start: 10, End: 12, Summary: "first", Author: "agent"},
	{ID: "t4", FileID: "f1", Path: "src/a.go", Start: 40, End: 40, Summary: "second", Author: "agent", Outdated: true},
}

func TestSessionNoteAddPostsTheRangeAndText(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		arg        string
		id, path   string
		start, end int
	}{
		{"src/a.go:10-12", "", "src/a.go", 10, 12},
		{"src/a.go:7", "", "src/a.go", 7, 7},
		{"f3:7-9", "f3", "", 7, 9},
		{"dir:x/a.go:5", "", "dir:x/a.go", 5, 5},
	} {
		dir := t.TempDir()
		livePresence(t, dir)
		seen := answer(t, dir, func(c steer.Command) steer.Reply {
			return steer.Reply{ID: c.ID, OK: true, Detail: "noted x as t9", Notes: twoNotes[:1]}
		})
		var out, errb bytes.Buffer
		args := []string{"note", "add", tc.arg, "--summary", "look", "--rationale", "why", "--author", "claude"}
		if code := runSession(dir, nil, args, &out, &errb); code != 0 {
			t.Fatalf("%s: exit = %d (stderr %q)", tc.arg, code, errb.String())
		}
		c := <-seen
		if c.Cmd != "note_add" || c.FileID != tc.id || c.File != tc.path || c.Start != tc.start || c.End != tc.end ||
			c.Summary != "look" || c.Rationale != "why" || c.Author != "claude" || !c.Wait {
			t.Errorf("%s: posted %+v", tc.arg, c)
		}
		if strings.TrimSpace(out.String()) != "noted x as t9" {
			t.Errorf("%s: stdout = %q", tc.arg, out.String())
		}
	}
}

func TestSessionNoteAddJSONPrintsTheNote(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Notes: twoNotes[:1]} })
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"note", "add", "a.go:1", "--summary", "s", "--json"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb.String())
	}
	var got steer.FileNote
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got.ID != "t3" {
		t.Fatalf("stdout = %s err=%v", out.String(), err)
	}
}

func TestSessionNoteListPrintsRowsAndJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	seen := answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Notes: twoNotes} })
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"note", "list", "src/a.go"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb.String())
	}
	want := "t3\tsrc/a.go\t10-12\tfirst\nt4\tsrc/a.go\t40-40\tsecond (outdated)\n"
	if out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
	if c := <-seen; c.Cmd != "note_list" || c.File != "src/a.go" {
		t.Errorf("posted %+v", c)
	}

	dir2 := t.TempDir()
	livePresence(t, dir2)
	answer(t, dir2, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Detail: "no notes"} })
	out.Reset()
	if code := runSession(dir2, nil, []string{"note", "list", "--json"}, &out, &errb); code != 0 || strings.TrimSpace(out.String()) != "[]" {
		t.Fatalf("empty --json: exit=%d stdout=%q", code, out.String())
	}
}

func TestSessionNoteShowPrintsTheNoteAndItsLines(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	n := steer.FileNote{ID: "t3", FileID: "f1", Path: "src/a.go", Start: 10, End: 11, Summary: "first", Rationale: "because\nof this", Author: "agent", Text: []string{"x := 1", "y := 2"}}
	seen := answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Notes: []steer.FileNote{n}} })
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"note", "show", "t3"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb.String())
	}
	want := "t3\tsrc/a.go:10-11\tagent\nfirst\n\nbecause\nof this\n\n10\tx := 1\n11\ty := 2\n"
	if out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
	if c := <-seen; c.Cmd != "note_show" || c.NoteID != "t3" {
		t.Errorf("posted %+v", c)
	}
}

func TestSessionNoteRmAndClear(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	seen := answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Detail: "removed t3"} })
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"note", "rm", "t3"}, &out, &errb); code != 0 || strings.TrimSpace(out.String()) != "removed t3" {
		t.Fatalf("rm: exit=%d stdout=%q stderr=%q", code, out.String(), errb.String())
	}
	if c := <-seen; c.Cmd != "note_rm" || c.NoteID != "t3" || c.File != "" {
		t.Errorf("rm posted %+v", c)
	}

	dir2 := t.TempDir()
	livePresence(t, dir2)
	seen2 := answer(t, dir2, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Detail: "removed 2 notes from a.go"} })
	if code := runSession(dir2, nil, []string{"note", "clear", "f1"}, &out, &errb); code != 0 {
		t.Fatalf("clear: exit=%d stderr=%q", code, errb.String())
	}
	if c := <-seen2; c.Cmd != "note_rm" || c.NoteID != "" || c.FileID != "f1" {
		t.Errorf("clear posted %+v", c)
	}
}

func TestSessionNoteRefusedExitsOne(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, Error: "no note t9"} })
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"note", "show", "t9"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "no note t9") {
		t.Fatalf("exit=%d stderr=%q", code, errb.String())
	}
}

func TestSessionNoteNeedsALiveTUI(t *testing.T) {
	t.Parallel()
	var out, errb bytes.Buffer
	if code := runSession(t.TempDir(), nil, []string{"note", "list"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "no gg session for this worktree") {
		t.Fatalf("nothing live: exit=%d stderr=%q", code, errb.String())
	}
	dir := t.TempDir()
	liveWebPresence(t, dir, "http://127.0.0.1:1") // only gg web: never posted to
	errb.Reset()
	if code := runSession(dir, nil, []string{"note", "list"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "temporary notes need a gg TUI") {
		t.Fatalf("web only: exit=%d stderr=%q", code, errb.String())
	}
}

func TestSessionNoteUsageErrorsExitTwo(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"note"},
		{"note", "bogus"},
		{"note", "add", "a.go:1"},                      // no --summary
		{"note", "add", "a.go", "--summary", "s"},      // no line
		{"note", "add", "a.go:0", "--summary", "s"},    // 0 is not a line
		{"note", "add", "a.go:5-3", "--summary", "s"},  // backwards
		{"note", "show"},
		{"note", "show", "a.go"},                       // not a note id
		{"note", "rm", "f1"},                           // rm takes a note id; clear takes a file
		{"note", "clear"},
		{"note", "list", "a", "b"},
	} {
		var out, errb bytes.Buffer
		if code := runSession(t.TempDir(), nil, args, &out, &errb); code != 2 {
			t.Errorf("%v: exit = %d, want 2 (stderr %q)", args, code, errb.String())
		}
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/open-file-notes && rtk go test ./internal/cli -run 'TestSessionNote' -count=1`
Expected: FAIL — `session: unknown subcommand "note"` (exit 2 everywhere).

- [ ] **Step 3: Implement**

Create `internal/cli/session_note.go`:

```go
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

const noteUsage = "usage: gg session note add <path|file-id>:<start>[-<end>] --summary \"…\" [--rationale \"…\"] [--author <name>] [--json]\n" +
	"       gg session note list [<path>|<file-id>] [--json]\n" +
	"       gg session note show <note-id> [--json]\n" +
	"       gg session note rm <note-id>\n" +
	"       gg session note clear <path>|<file-id>"

var (
	noteIDPattern = regexp.MustCompile(`^t[0-9]+$`)
	noteRange     = regexp.MustCompile(`^(.+):([0-9]+)(?:-([0-9]+))?$`)
)

// sessionNote is `gg session note …`: temporary notes on the files open in a
// live gg TUI. They live in that TUI's memory — so only a TUI can answer.
func sessionNote(dir string, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, noteUsage)
		return 2
	}
	switch args[0] {
	case "add":
		return sessionNoteAdd(dir, svc, args[1:], stdout, stderr)
	case "list":
		return sessionNoteList(dir, args[1:], stdout, stderr)
	case "show":
		return sessionNoteShow(dir, args[1:], stdout, stderr)
	case "rm":
		return sessionNoteRm(dir, args[1:], false, stdout, stderr)
	case "clear":
		return sessionNoteRm(dir, args[1:], true, stdout, stderr)
	}
	fmt.Fprintln(stderr, noteUsage)
	return 2
}

// steerNotes posts a note command to the live TUI and waits for its answer.
// A gg web page is never asked: it has no notes to answer with.
func steerNotes(dir string, c steer.Command, stdout, stderr io.Writer) (steer.Reply, int, bool) {
	if r := routeFor(preferredInbox(dir)); !r.tuiOK {
		if r.webOK {
			fmt.Fprintln(stderr, "temporary notes need a gg TUI (only gg web is live for this worktree)")
		} else {
			fmt.Fprintln(stderr, "no gg session for this worktree")
		}
		return steer.Reply{}, 1, false
	}
	rep, code, ok := steerLive(dir, c, false, false, stdout, stderr)
	if ok && !rep.OK {
		fmt.Fprintln(stderr, rep.Error)
		return rep, 1, false
	}
	return rep, code, ok
}

// fileTarget splits a <path>|<file-id> argument.
func fileTarget(s string) (id, path string) {
	if fileIDPattern.MatchString(s) {
		return s, ""
	}
	return "", s
}

func sessionNoteAdd(dir string, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session note add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	summary := fs.String("summary", "", "the remark, one short paragraph")
	rationale := fs.String("rationale", "", "the longer explanation")
	author := fs.String("author", "", "who is speaking (default: agent)")
	asJSON := fs.Bool("json", false, "print the new note as JSON")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, noteUsage)
		return 2
	}
	mt := noteRange.FindStringSubmatch(pos[0])
	if mt == nil {
		fmt.Fprintln(stderr, "session note add: name the lines — <path>:<start>[-<end>]")
		return 2
	}
	start, _ := strconv.Atoi(mt[2])
	end := start
	if mt[3] != "" {
		end, _ = strconv.Atoi(mt[3])
	}
	switch {
	case start < 1:
		fmt.Fprintln(stderr, "session note add: a line number is 1-based")
		return 2
	case end < start:
		fmt.Fprintln(stderr, "session note add: the range ends before it starts")
		return 2
	case strings.TrimSpace(*summary) == "":
		fmt.Fprintln(stderr, "session note add: --summary is required")
		return 2
	}
	id, path := fileTarget(mt[1])
	c := steer.Command{Cmd: "note_add", FileID: id, File: path, Start: start, End: end,
		Summary: *summary, Rationale: *rationale, Author: *author}
	if svc != nil {
		c.Worktree = callerWorktree(svc)
	}
	r, code, ok := steerNotes(dir, c, stdout, stderr)
	if !ok {
		return code
	}
	if *asJSON && len(r.Notes) == 1 {
		data, _ := json.MarshalIndent(r.Notes[0], "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	fmt.Fprintln(stdout, r.Detail)
	return 0
}

func sessionNoteList(dir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session note list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the notes as JSON")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) > 1 {
		fmt.Fprintln(stderr, noteUsage)
		return 2
	}
	c := steer.Command{Cmd: "note_list"}
	if len(pos) == 1 {
		c.FileID, c.File = fileTarget(pos[0])
	}
	r, code, ok := steerNotes(dir, c, stdout, stderr)
	if !ok {
		return code
	}
	if *asJSON {
		notes := r.Notes
		if notes == nil {
			notes = []steer.FileNote{}
		}
		data, _ := json.MarshalIndent(notes, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	for _, n := range r.Notes {
		sum := n.Summary
		if n.Outdated {
			sum += " (outdated)"
		}
		fmt.Fprintf(stdout, "%s\t%s\t%d-%d\t%s\n", n.ID, n.Path, n.Start, n.End, sum)
	}
	return 0
}

func sessionNoteShow(dir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session note show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the note as JSON")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 || !noteIDPattern.MatchString(pos[0]) {
		fmt.Fprintln(stderr, noteUsage)
		return 2
	}
	r, code, ok := steerNotes(dir, steer.Command{Cmd: "note_show", NoteID: pos[0]}, stdout, stderr)
	if !ok {
		return code
	}
	if len(r.Notes) != 1 {
		fmt.Fprintln(stderr, "no note "+pos[0])
		return 1
	}
	n := r.Notes[0]
	if *asJSON {
		data, _ := json.MarshalIndent(n, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	head := fmt.Sprintf("%s\t%s:%d-%d\t%s", n.ID, n.Path, n.Start, n.End, n.Author)
	if n.Outdated {
		head += "\t(outdated)"
	}
	fmt.Fprintln(stdout, head)
	fmt.Fprintln(stdout, n.Summary)
	if n.Rationale != "" {
		fmt.Fprintf(stdout, "\n%s\n", n.Rationale)
	}
	if len(n.Text) > 0 {
		fmt.Fprintln(stdout)
		for i, l := range n.Text {
			fmt.Fprintf(stdout, "%d\t%s\n", n.Start+i, l)
		}
	}
	return 0
}

// sessionNoteRm is `note rm <note-id>` and, with file set, `note clear
// <path>|<file-id>` — every note of one open file.
func sessionNoteRm(dir string, args []string, file bool, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session note rm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 || (!file && !noteIDPattern.MatchString(pos[0])) {
		fmt.Fprintln(stderr, noteUsage)
		return 2
	}
	c := steer.Command{Cmd: "note_rm", NoteID: pos[0]}
	if file {
		c.NoteID = ""
		c.FileID, c.File = fileTarget(pos[0])
	}
	r, code, ok := steerNotes(dir, c, stdout, stderr)
	if !ok {
		return code
	}
	fmt.Fprintln(stdout, r.Detail)
	return 0
}
```

`internal/cli/session.go`, `runSession`: change the usage string to `"usage: gg session <status|navigate|reload|focus|highlight|files|note> [flags]"` and add:

```go
	case "note":
		return sessionNote(dir, svc, args[1:], stdout, stderr)
```

Then find every other place that enumerates the session verbs and add `note`: run `grep -rn "session files" internal/cli cmd internal/shellinit README.md docs/CLAUDE-details.md | grep -v _test` and update help text / completion lists found there (leave `session_files.go` itself alone).

- [ ] **Step 4: Run the tests**

Run: `cd /work/gigagit/.claude/worktrees/open-file-notes && rtk go test ./internal/cli -count=1`
Expected: PASS. (`TestSessionNoteShowPrintsTheNoteAndItsLines` pins the exact text layout — adjust the CODE to match it, not the test.)

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/open-file-notes && gg add internal/cli/session_note.go internal/cli/session_note_test.go internal/cli/session.go && git commit -F <msgfile>
# subject: feat(cli): gg session note add|list|show|rm|clear — temporary notes on open files
# (add any help/completion file the grep turned up to the gg add list)
```

---

### Task 7: Skill, docs, a live check, the gate

**Files:**
- Modify: `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go:22` (`Version = 105` → `106`), `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`

**Interfaces:** consumes everything above; produces no code.

- [ ] **Step 1: Teach the skill** — use the `defining-agentic-tasks` skill's sync checklist. In `internal/agentskill/using-gg.md`, directly after the "Walking the user through several files" paragraph of the **Open files** section, add:

```markdown
**Temporary notes — explain on the code itself.** When the user says "show me
on the files", put short remarks on the lines you are talking about. The user
reads each one in a box under its lines in the gg TUI.

- `gg session note add <path>:<start>[-<end>] --summary "…" [--rationale "…"]
  [--author <name>] [--json]` — put a note on those lines of the file AS IT IS
  ON DISK. A file that is not open is opened in the background first. Answers
  `noted <path>:<lines> as t<n>`. `<path>` is repo-relative; an open file's id
  (`f3:10-12`) works too. Limits: 50 notes per file, summary 500 characters,
  rationale 4000 — an over-limit note is refused, never cut.
- `gg session note list [<path>|<file-id>] [--json]` —
  `<id>\t<path>\t<start>-<end>\t<summary>`; `(outdated)` marks a note whose
  lines have since changed.
- `gg session note show <note-id> [--json]` — the note and the lines it sits on
  now.
- `gg session note rm <note-id>` / `gg session note clear <path>|<file-id>`.

These notes are TEMPORARY: they live in the running gg TUI's memory, follow
their lines when the file changes, and are gone when the user closes the file
(X) or quits gg. They are not `gg note` review notes and never reach the notes
store. They need a live gg TUI (exit 1 otherwise; `gg web` alone cannot hold
them).

The walkthrough: `gg session note add` on each file (that opens them), then
`gg session files focus <id>:<line>` one file at a time as you explain it.
Keep a summary to one sentence; put the reasoning in `--rationale`.

When the user's message holds `gg note t<n> <path>:<lines>`, they copied a
reference to one of your notes: run `gg session note show t<n>` to see which
remark they mean (if it answers `no note t<n>`, the user closed it — the path
and lines still say where it was).
```

Bump `const Version = 105` to `106` in `internal/agentskill/agentskill.go`. Run `rtk go test ./internal/agentskill ./internal/exttool -count=1` (version-marker tests).

- [ ] **Step 2: Project docs**

- `CHANGELOG.md`: a new top entry — "Open-file notes: an agent can leave temporary remarks on the lines of a file open in the TUI (`gg session note add|list|show|rm|clear`); read inline under the lines, `}`/`{` step, `d` dismiss, `r` copy a reference for the agent; the notes follow their text across reloads and die with the file (X). Skill v106."
- `README.md`: in the agent/session verbs section add the `gg session note` family (one line each); in the file viewer keys add `} {`, `d`, `r`.
- `docs/CLAUDE-details.md`: a short "Open-file notes" subsection — memory-only on `openFile.notes`; VIRTUAL rows rule (never in `contentPopup.lines`; `extraRows` hook + `clampTop`/`rowsSpan`/`lastVisible` are the only row math that knows about them); re-anchor rule (alignment for live notes; outdated notes return only at their old place or a UNIQUE occurrence); the steer verbs bypass `steerRefusal`; the CLI posts to a TUI only.

- [ ] **Step 3: Build the verify binary and watch it work**

```bash
cd /work/gigagit/.claude/worktrees/open-file-notes && go build -o bin/gg ./cmd/gg && ls -la bin/gg
```

Use the `driving-tui-headless` skill: start `/work/gigagit/.claude/worktrees/open-file-notes/bin/gg` in its own tmux session (`tmux new-session -d -s claude-open-file-notes …`, never touch another session) on a scratch repo copy with a 60-line file, then from a second shell in that repo run, in order, capturing the pane after each:

1. `bin/gg session note add big.txt:10-12 --summary "first remark" --rationale "the reason"` → stdout `noted big.txt:10-12 as t1`; the screen did not change.
2. `bin/gg session note add big.txt:60 --summary "last line"`; `bin/gg session files` shows the file with its note count in `--json`.
3. `bin/gg session files focus big.txt:10` → the capture shows `│ ` on lines 10-12 and the box `agent · t1 · lines 10-12` under line 12.
4. Send `}` → cursor on line 60 and the box fully visible at the bottom; send `r`, then `{`, then `d` → the first box is gone.
5. Append a line at the top of `big.txt` on disk; after the reload the remaining note reads `line 61`.
6. Send `esc` → back to the panels, ctrl+\\ → Open files tab shows `big.txt … 1 note`; `x` there, then `bin/gg session note list` → no output.

Kill only the `claude-open-file-notes` tmux session afterwards. Record each capture's verdict in the final report; fix any mismatch before going on.

- [ ] **Step 4: The gate**

```bash
cd /work/gigagit/.claude/worktrees/open-file-notes && ./test.sh race > /tmp/claude-1000/-work-gigagit/a4b167fd-580d-4ece-b364-9661f516a9af/scratchpad/race.log 2>&1; tail -5 /tmp/claude-1000/-work-gigagit/a4b167fd-580d-4ece-b364-9661f516a9af/scratchpad/race.log
```

Expected: the log's last line is `all green`. Anything else is a failure — read the log, fix, re-run.

- [ ] **Step 5: Commit the docs**

```bash
cd /work/gigagit/.claude/worktrees/open-file-notes && gg add internal/agentskill/using-gg.md internal/agentskill/agentskill.go CHANGELOG.md README.md docs/CLAUDE-details.md && git commit -F <msgfile>
# subject: docs: open-file notes — skill v106, changelog, README, details
```

- [ ] **Step 6: Whole-branch review, then hand over**

Dispatch ONE read-only review subagent on the most capable model over `git diff main...feat/open-file-notes` with the spec and this plan's Review Focus as its brief (no edits, no commits, no test runs that write). Apply what holds up (superpowers:receiving-code-review), re-run Step 4, then give the user the verify binary's absolute path (`/work/gigagit/.claude/worktrees/open-file-notes/bin/gg`) and ASK before merging (`gg merge -F <msgfile> --into main feat/open-file-notes`, then `./build.sh install` and `gg init --update`).
