# TUI History View (on a view-stack) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a file **History** view (`h` on a file → commits-left / file-diff-right) built on a new TUI **view stack**, the first piece of the layout layer.

**Architecture:** A `viewStack` of full-screen `surface`s sits at the top of the existing render/key dispatch (additive — existing surfaces are untouched and remain the background a popped stack reveals). History is the first `surface`; its right pane reuses the existing `diffPaneLines` window and `fillDiff`/`ShowFile`. A new rename-correct `FileLog` git verb feeds the commit list. Migrating the *existing* surfaces onto the stack (and Blame) are deliberate follow-ups.

**Tech Stack:** Go 1.26, Bubble Tea (Elm value-receiver `Model`), lipgloss, system `git` via `gitcmd`/`gitexec`.

**Scope note (refines design §4):** the design's step 1 ("wrap *all* existing surfaces as entries") is **deferred**. This plan introduces the stack *additively* and routes only the new History surface through it — lowest-risk path that still ships History on real machinery. Collapsing the remaining `if`-chains into the stack is a follow-up plan.

---

## File structure

| File | New? | Responsibility |
|---|---|---|
| `internal/model/file_commit.go` | new | `FileCommit` data type (a `Commit` + the file's per-commit status/path). |
| `internal/git/file_log.go` | new | `FileLog` verb + `ParseFileLog` parser. |
| `internal/git/file_log_test.go` | new | Parser + verb tests (real repo, rename/delete/root). |
| `internal/tui/stack.go` | new | `surface` interface, `viewStack`, `pushSurface`/`popSurface`. |
| `internal/tui/stack_test.go` | new | push/pop + dispatch-ownership tests. |
| `internal/tui/history_view.go` | new | `historyView` surface: struct, render, key handling, loaders, messages. |
| `internal/tui/history_view_test.go` | new | History list/diff/nav/staleness tests. |
| `internal/tui/model.go` | modify | `stack` field; dispatch hooks in `render`/`Update` key+mouse arms; `h` on Status; history msgs; `diffView.rev` wiring at the status-enter site. |
| `internal/tui/diff_view.go` | modify | add `rev string` field to `diffView`; set it in the two loaders. |
| `internal/tui/files_view.go` | modify | `h` on a tree row pushes History. |
| `internal/tui/diff_render.go` | modify | `h` in `updateDiffViewKey` pushes History; hint line gains `[h]`. |
| `internal/tui/help.go` | modify | document `h` in the help content. |
| `CHANGELOG.md`, `README.md` | modify | user-facing surface note. |

---

## Task 1: `FileCommit` model type

**Files:**
- Create: `internal/model/file_commit.go`

- [ ] **Step 1: Write the type**

```go
package model

// FileCommit is one commit in a single file's history: the commit metadata
// plus the file's status and name *at that commit* (so a diff can address the
// right blob even across renames).
type FileCommit struct {
	Commit         // Hash, Parents, Author, Subject, UnixTime
	Status  string // "A","M","D","R","C","T" — the file's change at this commit
	Path    string // the file's name at this commit (post-rename name)
	OldPath string // parent-side name; set only for renames/copies
}
```

- [ ] **Step 2: Build to verify it compiles**

Run: `go build ./internal/model`
Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add internal/model/file_commit.go
git commit -m "model: add FileCommit type for file history"
```

---

## Task 2: `ParseFileLog` parser (TDD)

**Files:**
- Create: `internal/git/file_log.go`
- Test: `internal/git/file_log_test.go`

- [ ] **Step 1: Write the failing test**

```go
package git

import "testing"

func TestParseFileLog(t *testing.T) {
	// One commit per format line ("%H\x1f%P\x1f%an\x1f%at\x1f%s"), each
	// followed by its --name-status line for the followed file.
	data := "" +
		"aaa\x1fppp\x1fAda\x1f1700000000\x1fmodify auth\n" +
		"M\tsrc/auth.go\n" +
		"\n" +
		"bbb\x1fqqq\x1fBob\x1f1690000000\x1frename file\n" +
		"R100\tsrc/old.go\tsrc/auth.go\n" +
		"\n" +
		"ccc\x1f\x1fAda\x1f1680000000\x1finitial\n" +
		"A\tsrc/old.go\n"

	got := ParseFileLog([]byte(data))
	if len(got) != 3 {
		t.Fatalf("want 3 commits, got %d", len(got))
	}
	if got[0].Hash != "aaa" || got[0].Status != "M" || got[0].Path != "src/auth.go" {
		t.Errorf("commit 0 wrong: %+v", got[0])
	}
	if got[0].Author != "Ada" || got[0].UnixTime != 1700000000 || got[0].Subject != "modify auth" {
		t.Errorf("commit 0 metadata wrong: %+v", got[0])
	}
	if got[1].Status != "R" || got[1].OldPath != "src/old.go" || got[1].Path != "src/auth.go" {
		t.Errorf("rename commit wrong: %+v", got[1])
	}
	if got[2].Status != "A" || got[2].Path != "src/old.go" || len(got[2].Parents) != 0 {
		t.Errorf("root commit wrong: %+v", got[2])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git -run TestParseFileLog -v`
Expected: FAIL — `undefined: ParseFileLog`.

- [ ] **Step 3: Write the parser**

```go
package git

import (
	"context"
	"strconv"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
	"github.com/gigagit/gg/internal/model"
)

// ParseFileLog parses interleaved `git log --name-status --format=<logFormat>`
// output: a format line (contains \x1f) opens a commit; the following
// tab-bearing line is that commit's name-status for the followed file.
func ParseFileLog(data []byte) []model.FileCommit {
	var out []model.FileCommit
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		if strings.Contains(line, "\x1f") {
			f := strings.Split(line, "\x1f")
			if len(f) < 5 {
				continue
			}
			fc := model.FileCommit{Commit: model.Commit{Hash: f[0], Author: f[2], Subject: f[4]}}
			if p := strings.Fields(f[1]); len(p) > 0 {
				fc.Commit.Parents = p
			}
			if t, err := strconv.ParseInt(f[3], 10, 64); err == nil {
				fc.Commit.UnixTime = t
			}
			out = append(out, fc)
			continue
		}
		if n := len(out); n > 0 && strings.Contains(line, "\t") {
			nf := strings.Split(line, "\t")
			out[n-1].Status = nf[0][:1]
			switch {
			case (out[n-1].Status == "R" || out[n-1].Status == "C") && len(nf) >= 3:
				out[n-1].OldPath = nf[1]
				out[n-1].Path = nf[2]
			case len(nf) >= 2:
				out[n-1].Path = nf[1]
			}
		}
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/git -run TestParseFileLog -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/git/file_log.go internal/git/file_log_test.go
git commit -m "git: ParseFileLog for single-file history --name-status output"
```

---

## Task 3: `FileLog` verb (TDD against a real repo)

**Files:**
- Modify: `internal/git/file_log.go`
- Test: `internal/git/file_log_test.go`

- [ ] **Step 1: Write the failing test**

Append to `file_log_test.go` (uses the package's existing `newTestRepo`/`newRepo` helper — match the helper name already used in `internal/git/*_test.go`):

```go
func TestFileLog(t *testing.T) {
	r := newTestRepo(t) // real git in t.TempDir(); see other internal/git tests
	writeFile(t, r, "a.go", "one\n")
	commitAll(t, r, "add a.go")
	writeFile(t, r, "a.go", "one\ntwo\n")
	commitAll(t, r, "edit a.go")
	writeFile(t, r, "b.go", "x\n")
	commitAll(t, r, "add b.go") // unrelated; must NOT appear

	got, err := r.FileLog(context.Background(), "", "a.go", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 commits touching a.go, got %d: %+v", len(got), got)
	}
	if got[0].Subject != "edit a.go" || got[1].Subject != "add a.go" {
		t.Errorf("order/newest-first wrong: %+v", got)
	}
	if got[0].Status != "M" || got[1].Status != "A" {
		t.Errorf("statuses wrong: %+v", got)
	}
}
```

> Helper names: reuse whatever the existing `internal/git` tests use for repo
> setup / writing / committing (check `git/log_test.go` or `git/show_test.go`).
> If a helper is missing, add a tiny local one in this test file mirroring the
> existing pattern. Do not invent a new framework.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git -run TestFileLog -v`
Expected: FAIL — `r.FileLog undefined`.

- [ ] **Step 3: Implement the verb**

Append to `internal/git/file_log.go`:

```go
// FileLog returns the commits that touched path, newest first, following the
// file across renames. rev "" starts from HEAD. One invocation. limit bounds
// history depth for very large repos.
func (r *Repo) FileLog(ctx context.Context, rev, path string, limit int) ([]model.FileCommit, error) {
	b := gitcmd.New("log").
		ArgIf(rev != "", rev).
		Arg("--follow", "-M", "--name-status", "--format="+logFormat, "-n", strconv.Itoa(limit), "--", path)
	res, err := r.Runner.Run(ctx, "git log (file history)", b.ToArgv())
	if err != nil {
		return nil, err
	}
	return ParseFileLog([]byte(res.Stdout)), nil
}
```

> `logFormat` is the existing const in `internal/git/log.go`. Confirm `gitcmd`
> exposes `ArgIf(cond bool, args ...string)` (it does — see CLAUDE.md); if the
> signature differs, branch on `rev != ""` with two `Arg` calls instead.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/git -run TestFileLog -v`
Expected: PASS.

- [ ] **Step 5: Run the whole git package**

Run: `go test ./internal/git`
Expected: ok.

- [ ] **Step 6: Commit**

```bash
git add internal/git/file_log.go internal/git/file_log_test.go
git commit -m "git: FileLog verb — rename-following single-file history"
```

---

## Task 4: The view stack primitive (TDD)

**Files:**
- Create: `internal/tui/stack.go`
- Test: `internal/tui/stack_test.go`
- Modify: `internal/tui/model.go` (add the `stack` field)

- [ ] **Step 1: Add the field**

In `internal/tui/model.go`, inside the `Model` struct (next to `diffView`), add:

```go
	stack *viewStack // top-of-everything full-screen surfaces (history, later blame); nil/empty = none
```

- [ ] **Step 2: Write the failing test**

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeSurface records whether it owned the last update.
type fakeSurface struct{ updated bool }

func (s *fakeSurface) render(m Model) string { return "FAKE" }
func (s *fakeSurface) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	s.updated = true
	return m, nil
}

func TestStackPushPopOwnership(t *testing.T) {
	m := Model{}
	if m.stackTop() != nil {
		t.Fatal("empty stack should have no top")
	}
	s := &fakeSurface{}
	m = m.pushSurface(s)
	if m.stackTop() != s {
		t.Fatal("push did not set top")
	}
	m = m.popSurface()
	if m.stackTop() != nil {
		t.Fatal("pop did not clear top")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/tui -run TestStackPushPopOwnership -v`
Expected: FAIL — undefined `viewStack`/`pushSurface`/`popSurface`/`stackTop`.

- [ ] **Step 4: Implement the stack**

`internal/tui/stack.go`:

```go
package tui

import tea "github.com/charmbracelet/bubbletea"

// surface is a full-screen view layered on top of the panel interface. The top
// of the stack owns keyboard input and the whole screen; popping it reveals the
// surface beneath (or the panel interface), whose state was never torn down.
type surface interface {
	render(m Model) string
	update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
}

type viewStack struct{ entries []surface }

// stackTop returns the active surface, or nil when the stack is empty.
func (m Model) stackTop() surface {
	if m.stack == nil || len(m.stack.entries) == 0 {
		return nil
	}
	return m.stack.entries[len(m.stack.entries)-1]
}

// pushSurface puts s on top. stack is a pointer field so the push persists
// across Model value copies (same rationale as modal/popup).
func (m Model) pushSurface(s surface) Model {
	if m.stack == nil {
		m.stack = &viewStack{}
	}
	m.stack.entries = append(m.stack.entries, s)
	return m
}

// popSurface drops the top surface; a no-op on an empty stack.
func (m Model) popSurface() Model {
	if m.stack != nil && len(m.stack.entries) > 0 {
		m.stack.entries = m.stack.entries[:len(m.stack.entries)-1]
	}
	return m
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/tui -run TestStackPushPopOwnership -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/stack.go internal/tui/stack_test.go internal/tui/model.go
git commit -m "tui: add view-stack primitive for full-screen surfaces"
```

---

## Task 5: Wire the stack into render + key + mouse dispatch

**Files:**
- Modify: `internal/tui/model.go` (render, key arm, mouse arm)

The stack is checked **immediately after the modal**, before `diffView` — it is the new top of the routing invariant.

- [ ] **Step 1: Hook `render()`**

In `internal/tui/view.go` `render()`, right after the `if m.modal != nil { return m.renderModal() }` block, add:

```go
	if s := m.stackTop(); s != nil {
		_, h := m.overlayDims()
		return clipToHeight(s.render(m), h)
	}
```

- [ ] **Step 2: Hook the key arm**

In `internal/tui/model.go` `Update`, in `case tea.KeyMsg:`, right after the modal block (before `if m.diffView != nil`), add:

```go
		if s := m.stackTop(); s != nil {
			if msg.Type == tea.KeyCtrlC {
				return m, tea.Quit
			}
			return s.update(m, msg)
		}
```

- [ ] **Step 3: Hook the mouse arm (swallow mouse while a surface is up)**

In the `case tea.MouseMsg:` arm of `Update` (find it near the key arm), add at the top of the arm, after any modal guard:

```go
		if m.stackTop() != nil {
			return m, nil // history/blame are keyboard-only (v1)
		}
```

> If the mouse arm currently begins by checking `m.diffView`, place this guard
> immediately before that check, mirroring the render/key ordering.

- [ ] **Step 4: Build + full TUI tests (nothing should change yet — stack is empty)**

Run: `go test ./internal/tui`
Expected: ok (no surface is ever pushed yet).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/view.go internal/tui/model.go
git commit -m "tui: route render/key/mouse through the view stack (top priority)"
```

---

## Task 6: `historyView` surface — struct, loaders, messages

**Files:**
- Create: `internal/tui/history_view.go`
- Modify: `internal/tui/model.go` (handle `historyListMsg`/`historyDiffMsg` in the msg switch)

- [ ] **Step 1: Write the surface, navContext, loaders, and messages**

`internal/tui/history_view.go`:

```go
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

// historyMaxCommits bounds file-history depth on huge repos.
const historyMaxCommits = 200

// navContext identifies the file + revision a history/blame view explores.
// rev "" means the working-tree context (history from HEAD).
type navContext struct {
	path string
	rev  string
}

// historyView is the file-history surface: commits left, the file's diff at the
// selected commit on the right (reusing the diff pane).
type historyView struct {
	ctx      navContext
	commits  []model.FileCommit
	sel      int
	loading  bool
	err      error
	diff     *diffView // right pane (reuses diffView rendering + guards)
	listTag  string    // gates stale list loads
	diffTag  string    // gates stale right-pane loads
}

func newHistoryView(ctx navContext) *historyView {
	return &historyView{ctx: ctx, loading: true, listTag: "histlist:" + ctx.rev + ":" + ctx.path}
}

// historyListMsg / historyDiffMsg carry async results, tag-gated like diffMsg.
type historyListMsg struct {
	tag     string
	commits []model.FileCommit
	err     error
}
type historyDiffMsg struct {
	tag  string
	view *diffView
}

// loadHistoryListCmd fetches the commit list off the UI thread.
func (m Model) loadHistoryListCmd(ctx navContext, tag string) tea.Cmd {
	repo := m.repo
	return func() tea.Msg {
		cs, err := repo.FileLog(context.Background(), ctx.rev, ctx.path, historyMaxCommits)
		return historyListMsg{tag: tag, commits: cs, err: err}
	}
}

// loadHistoryDiffCmd builds the right-pane diff for fc: the file at fc vs its
// first parent, addressing the correct (possibly renamed) blob names.
func (m Model) loadHistoryDiffCmd(fc model.FileCommit, tag string) tea.Cmd {
	repo := m.repo
	v := &diffView{title: fc.Path, context: "@ " + shortHash(fc.Hash) + " " + fc.Subject, partial: m.diffPartial}
	return func() tea.Msg {
		var oldB, newB []byte
		if fc.Status != "A" {
			p := fc.Path
			if fc.OldPath != "" {
				p = fc.OldPath
			}
			b, err := repo.ShowFile(context.Background(), fc.Hash+"^", p)
			if err != nil {
				v.err = err
				return historyDiffMsg{tag: tag, view: v}
			}
			oldB = b
		}
		if fc.Status != "D" {
			b, err := repo.ShowFile(context.Background(), fc.Hash, fc.Path)
			if err != nil {
				v.err = err
				return historyDiffMsg{tag: tag, view: v}
			}
			newB = b
		}
		fillDiff(v, oldB, newB)
		return historyDiffMsg{tag: tag, view: v}
	}
}

// selectCmd (re)loads the right pane for the current selection.
func (h *historyView) selectCmd(m Model) tea.Cmd {
	if h.sel < 0 || h.sel >= len(h.commits) {
		return nil
	}
	fc := h.commits[h.sel]
	h.diffTag = "histdiff:" + fc.Hash + ":" + h.ctx.path
	h.diff = &diffView{title: fc.Path, context: "@ " + shortHash(fc.Hash) + " " + fc.Subject, loading: true}
	return m.loadHistoryDiffCmd(fc, h.diffTag)
}
```

- [ ] **Step 2: Handle the messages in `Update`**

In `internal/tui/model.go` `Update`'s message `switch`, add two cases (anywhere among the other `case fooMsg:` arms):

```go
	case historyListMsg:
		if h, ok := m.stackTop().(*historyView); ok && h.listTag == msg.tag {
			h.loading = false
			h.err = msg.err
			h.commits = msg.commits
			h.sel = 0
			if len(h.commits) > 0 {
				return m, h.selectCmd(m)
			}
		}
		return m, nil
	case historyDiffMsg:
		if h, ok := m.stackTop().(*historyView); ok && h.diffTag == msg.tag {
			msg.view.partial = m.diffPartial
			msg.view.rebuild()
			h.diff = msg.view
		}
		return m, nil
```

> `m.stackTop().(*historyView)` is nil-safe: a nil `surface` interface fails the
> assertion and `ok` is false.

- [ ] **Step 3: Build**

Run: `go build ./internal/tui`
Expected: success (render/update methods for `historyView` come in Task 7; until then `*historyView` does not satisfy `surface` — so ALSO add the stubs now to compile):

Add to `history_view.go` (filled in Task 7):

```go
func (h *historyView) render(m Model) string { return "" }
func (h *historyView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) { return m, nil }
```

Run: `go build ./internal/tui`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/history_view.go internal/tui/model.go
git commit -m "tui: historyView surface scaffolding + async loaders/messages"
```

---

## Task 7: `historyView` render + key handling (TDD)

**Files:**
- Modify: `internal/tui/history_view.go`
- Test: `internal/tui/history_view_test.go`

- [ ] **Step 1: Write the failing test**

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

func histFixture() *historyView {
	return &historyView{
		ctx: navContext{path: "a.go", rev: ""},
		commits: []model.FileCommit{
			{Commit: model.Commit{Hash: "aaaaaaa", Subject: "edit", Author: "Ada"}, Status: "M", Path: "a.go"},
			{Commit: model.Commit{Hash: "bbbbbbb", Subject: "add", Author: "Bob"}, Status: "A", Path: "a.go"},
		},
	}
}

func TestHistoryRenderListsCommits(t *testing.T) {
	m := Model{width: 100, height: 30}
	h := histFixture()
	out := h.render(m)
	if !contains(out, "edit") || !contains(out, "add") {
		t.Errorf("history render missing commit subjects:\n%s", out)
	}
	if !contains(out, "a.go") {
		t.Errorf("history header missing path:\n%s", out)
	}
}

func TestHistoryDownMovesSelectionAndReloads(t *testing.T) {
	m := Model{width: 100, height: 30, repo: nil}
	h := histFixture()
	m = m.pushSurface(h)
	_, cmd := h.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if h.sel != 1 {
		t.Fatalf("j should move selection to 1, got %d", h.sel)
	}
	if cmd == nil {
		t.Fatal("moving selection should fire a right-pane reload cmd")
	}
}

func TestHistoryEscPops(t *testing.T) {
	m := Model{width: 100, height: 30}
	h := histFixture()
	m = m.pushSurface(h)
	m, _ = h.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.stackTop() != nil {
		t.Fatal("esc should pop the history surface")
	}
}
```

> `contains` — use the test helper already present in `internal/tui` tests
> (`strings.Contains` wrapper). If none exists, inline `strings.Contains`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui -run TestHistory -v`
Expected: FAIL (stub render returns "", stub update no-ops).

- [ ] **Step 3: Implement render + update (replace the Task-6 stubs)**

```go
// historyBodyRows is the list/pane height: full height minus header + hint.
func (m Model) historyBodyRows() int {
	_, h := m.overlayDims()
	n := h - 2
	if n < 1 {
		n = 1
	}
	return n
}

func (h *historyView) render(m Model) string {
	w, scrH := m.overlayDims()
	body := h.historyBodyRows()

	header := truncate("history: "+h.ctx.path, w)
	hint := truncate("[↑↓] commit  [enter] open diff  [esc] back  [q] quit", w)

	// Left list. Right pane shown only when wide enough (>=60); else list-only.
	split := w >= 60
	listW := w
	if split {
		listW = (w - 1) / 2
	}

	rows := make([]string, 0, len(h.commits))
	for i, fc := range h.commits {
		line := shortHash(fc.Hash) + "  " + fc.Status + "  " + truncate(fc.Subject, listW-16)
		if i == h.sel {
			rows = append(rows, selectedRow.Render(padRight(truncate("> "+line, listW), listW)))
		} else {
			rows = append(rows, padRight(truncate("  "+line, listW), listW))
		}
	}
	win, _, _ := windowRows(rows, body, h.sel)
	if h.loading {
		win = []string{padRight("  (loading…)", listW)}
	} else if h.err != nil {
		win = []string{padRight(truncate("  error: "+h.err.Error(), listW), listW)}
	} else if len(h.commits) == 0 {
		win = []string{padRight("  (no history)", listW)}
	}
	for len(win) < body {
		win = append(win, padRight("", listW))
	}

	var bodyStr string
	if split {
		right := h.renderRightPane(m, w-listW-1, body)
		leftCol := make([]string, body)
		rightCol := splitLines(right, body)
		for i := 0; i < body; i++ {
			l := ""
			if i < len(win) {
				l = win[i]
			}
			r := ""
			if i < len(rightCol) {
				r = rightCol[i]
			}
			leftCol[i] = l + "│" + r
		}
		bodyStr = joinLines(leftCol)
	} else {
		bodyStr = joinLines(win)
	}

	out := header + "\n" + bodyStr + "\n" + hint
	return clipToHeight(out, scrH)
}

// renderRightPane draws the selected commit's diff using the shared diff pane.
func (h *historyView) renderRightPane(m Model, w, body int) string {
	if h.diff == nil {
		return padBox("  (select a commit)", w, body)
	}
	v := h.diff
	switch {
	case v.loading:
		return padBox("  (loading…)", w, body)
	case v.err != nil:
		return padBox(truncate("  error: "+v.err.Error(), w), w, body)
	case v.binary:
		return padBox("  (binary file)", w, body)
	case v.tooLarge:
		return padBox("  (file too large)", w, body)
	}
	lines := m.diffPaneLines(v, w, body)
	for len(lines) < body {
		lines = append(lines, "")
	}
	return joinLines(lines[:body])
}

func (h *historyView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "esc", "h":
		return m.popSurface(), nil
	case "down", "j":
		if h.sel < len(h.commits)-1 {
			h.sel++
			return m, h.selectCmd(m)
		}
	case "up", "k":
		if h.sel > 0 {
			h.sel--
			return m, h.selectCmd(m)
		}
	}
	return m, nil
}
```

Add the small render helpers at the bottom of `history_view.go` (if equivalents
already exist in `internal/tui`, call those instead and delete these):

```go
import "strings"

func splitLines(s string, n int) []string { return strings.Split(s, "\n") }
func joinLines(ls []string) string        { return strings.Join(ls, "\n") }
func padBox(s string, w, body int) string {
	lines := make([]string, body)
	lines[0] = padRight(truncate(s, w), w)
	for i := 1; i < body; i++ {
		lines[i] = padRight("", w)
	}
	return joinLines(lines)
}
```

> `padRight`, `truncate`, `windowRows`, `selectedRow`, `shortHash`,
> `clipToHeight`, `diffPaneLines` all already exist in `internal/tui`. Do NOT
> redefine them. `splitLines`/`joinLines` are trivial wrappers — if `strings`
> is already imported in this file, drop the second import line.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui -run TestHistory -v`
Expected: PASS.

- [ ] **Step 5: gofmt + full package**

Run: `gofmt -l internal/tui/history_view.go && go test ./internal/tui`
Expected: no gofmt output; tests ok.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/history_view.go internal/tui/history_view_test.go
git commit -m "tui: history view render (commits | diff) + j/k/esc navigation"
```

---

## Task 8: `diffView.rev` field for the diff-view entry point

**Files:**
- Modify: `internal/tui/diff_view.go` (struct + both loaders), `internal/tui/model.go` + `internal/tui/files_view.go` (the two construction sites)

- [ ] **Step 1: Add the field**

In `diffView` struct (diff_view.go), add after `context`:

```go
	rev string // the commit-ish this diff's NEW side came from; "" = working tree (used by `h`→history)
```

- [ ] **Step 2: Set it in the loaders**

In `loadStatusDiffCmd` (diff_view.go) where `v := &diffView{...}` is built, leave `rev` empty (working tree) — explicit for clarity:

```go
	v := &diffView{title: f.Path, context: "HEAD → working tree", rev: "", partial: m.diffPartial}
```

In `loadCommitDiffCmd`, set the commit:

```go
	v := &diffView{title: line.path, context: "@ " + strings.TrimPrefix(m.filesTitle, "Files "), rev: hash, partial: m.diffPartial}
```

Also set `rev` on the two **eager** `diffView` constructions that show the
loading state (so `h` works before the load returns):
- `internal/tui/model.go` status-enter site (currently `m.diffView = &diffView{title: f.Path, context: "HEAD → working tree", loading: true}`): add `rev: ""`.
- `internal/tui/files_view.go` enter site (`m.diffView = &diffView{title: l.path, context: ..., loading: true}`): add `rev: m.filesHash`.

- [ ] **Step 3: Build + tests**

Run: `go test ./internal/tui`
Expected: ok (no behavior change yet).

- [ ] **Step 4: Commit**

```bash
git add internal/tui/diff_view.go internal/tui/model.go internal/tui/files_view.go
git commit -m "tui: record source rev on diffView for history pivot"
```

---

## Task 9: Wire the three `h` entry points (TDD)

**Files:**
- Modify: `internal/tui/model.go` (Status panel `h`), `internal/tui/files_view.go` (tree `h`), `internal/tui/diff_render.go`/`diff_view.go` (diff `h` + hint)
- Test: `internal/tui/history_view_test.go`

- [ ] **Step 1: Write the failing test**

Append to `history_view_test.go`:

```go
func TestStatusHOpensHistory(t *testing.T) {
	m := Model{width: 100, height: 30, repo: nil, focus: panelStatus, sel: map[panel]int{}}
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "a.go"}}}
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	got := mm.(Model)
	h, ok := got.stackTop().(*historyView)
	if !ok {
		t.Fatal("h on a Status file should push a historyView")
	}
	if h.ctx.path != "a.go" || h.ctx.rev != "" {
		t.Errorf("wrong navContext: %+v", h.ctx)
	}
}
```

> Confirm `model.WorkingTreeStatus`/`FileStatus` field names against
> `internal/model/model.go`; `canShowFileDiff()` must be true for a single
> unfiltered file — if it gates on more, set those fields in the fixture.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui -run TestStatusHOpensHistory -v`
Expected: FAIL — `h` currently does nothing on Status.

- [ ] **Step 3: Status panel `h`**

In `internal/tui/model.go`, in the main key `switch`, add a `case "h":` (near the `enter` Status arm):

```go
		case "h":
			if m.focus == panelStatus && m.canShowFileDiff() {
				bi, _ := m.backingIndex(panelStatus)
				f := m.status.Files[bi]
				ctx := navContext{path: f.Path, rev: ""}
				h := newHistoryView(ctx)
				m = m.pushSurface(h)
				return m, m.loadHistoryListCmd(ctx, h.listTag)
			}
```

- [ ] **Step 4: Files-view tree `h`**

In `internal/tui/files_view.go` `updateFilesViewKey`, add a `case "h":` (near the `enter` arm, guarded like it):

```go
		case "h":
			if !m.filesTreeFocused {
				return m, nil
			}
			vis := p.visible()
			if p.sel < 0 || p.sel >= len(vis) || vis[p.sel].path == "" {
				return m, nil
			}
			ctx := navContext{path: vis[p.sel].path, rev: m.filesHash}
			h := newHistoryView(ctx)
			m = m.pushSurface(h)
			return m, m.loadHistoryListCmd(ctx, h.listTag)
```

- [ ] **Step 5: Diff-view `h`**

In `internal/tui/diff_view.go` `updateDiffViewKey`, add a `case "h":`:

```go
	case "h":
		ctx := navContext{path: v.title, rev: v.rev}
		h := newHistoryView(ctx)
		m = m.pushSurface(h)
		return m, m.loadHistoryListCmd(ctx, h.listTag)
```

And extend the hint in `internal/tui/diff_render.go` (`diffHint` const) to include `[h] history`:

```go
const diffHint = "[↑↓] scroll  [pgup/pgdn] page  [n/p] next/prev change  [f] toggle partial  [h] history  [esc] close  [q] quit"
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/tui -run TestStatusHOpensHistory -v && go test ./internal/tui`
Expected: PASS; package ok.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/model.go internal/tui/files_view.go internal/tui/diff_view.go internal/tui/diff_render.go internal/tui/history_view_test.go
git commit -m "tui: wire h (history) from Status, files-view, and diff view"
```

---

## Task 10: Help text + user docs + full verification

**Files:**
- Modify: `internal/tui/help.go`, `internal/tui/files_view.go` (files-view hint), `CHANGELOG.md`, `README.md`

- [ ] **Step 1: Help content**

In `internal/tui/help.go`, add an `h` row to the key list rendered by `helpContent()` (match the existing format), e.g.:

```
  h        file history (Status file / files-view row / diff view)
```

- [ ] **Step 2: Files-view hint**

In `internal/tui/files_view.go` `renderFilesView`, extend the `hint` string to mention history:

```go
	hint := "[enter] diff  [h] history  [/] search  [esc] close"
```

- [ ] **Step 3: CHANGELOG + README**

Add a CHANGELOG entry under the current unreleased section:

```
- TUI: file **History** view — press `h` on a Status file, a files-view row, or
  inside the diff view to see the commits that touched the file (left) with the
  file's diff at the selected commit (right). Built on a new full-screen view
  stack (first piece of the layout layer). Rename-following via `git log
  --follow`.
```

Add an `h` line wherever README documents TUI keys (the keybindings table/list).

- [ ] **Step 4: Full staged verification**

Run: `./test.sh`
Expected: vet+gofmt clean, unit + e2e pass.

- [ ] **Step 5: Race pass before hand-off**

Run: `./test.sh race`
Expected: PASS with `-race`.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/help.go internal/tui/files_view.go CHANGELOG.md README.md
git commit -m "docs(tui): document the h/history key (help, changelog, readme)"
```

---

## Follow-ups (out of scope; separate specs/plans)

1. **Blame (`b`)** — grouped-block gutter; blame↔history cross-link (UR-4 second half).
2. **Surface migration** — wrap the base grid, diff, files-view, popups, and modal as stack entries and delete the three `if`-chains (design D1, deferred here).
3. **History narrow-mode `enter`** — open the selected commit's diff full-screen when width < 60 (v1 shows list-only).
4. **`gg log <file>` CLI** — the CLI/MCP surface for file history (FR-4).

---

## Self-review

**Spec coverage:** UR-1 history layout → Tasks 6–7; UR-3 three entry points → Task 9; CR-7 `navContext` → Task 6; rename correctness (design §3.3) → Tasks 2–3 + `loadHistoryDiffCmd` (Task 6); shared diff-pane window (FR-3) → `diffPaneLines` reuse (Task 7); view-stack machinery (D1 partial, D2) → Tasks 4–5; perf guards (§3.5) → `historyMaxCommits` + reused `maxDiffBytes` (Tasks 3,6); responsive (§3.6) → split-at-60 in render (Task 7). Blame (UR-2) and full migration are explicitly deferred follow-ups.

**Placeholder scan:** No "TBD"/"handle edge cases"/"similar to". Each code step shows full code. The few `>` notes ask the engineer to confirm an existing helper name against the current tree (verification, not missing content).

**Type consistency:** `surface`/`viewStack`/`stackTop`/`pushSurface`/`popSurface` used consistently (Tasks 4–9). `navContext{path,rev}`, `historyView` fields (`ctx,commits,sel,loading,err,diff,listTag,diffTag`), `newHistoryView`, `loadHistoryListCmd(ctx,tag)`, `loadHistoryDiffCmd(fc,tag)`, `selectCmd(m)`, `historyListMsg{tag,commits,err}`, `historyDiffMsg{tag,view}` all match across tasks. `FileCommit{Commit,Status,Path,OldPath}` and `FileLog(ctx,rev,path,limit)`/`ParseFileLog` consistent (Tasks 1–3, 6). `diffView.rev` defined (Task 8) before its use in diff-view `h` (Task 9).
