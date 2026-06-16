# Conflict hunk picker — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Resolve a merge/rebase conflict at the region and line level in the TUI — for each conflict region take the whole **current** side, the whole **incoming** side, or assemble it line-by-line (lines land in pick order).

**Architecture:** A pure `internal/hunkpick` package (decision model + conflict-marker parser + byte-faithful assembly) drives a view-stack picker surface; a thin engine op writes the assembled bytes to the working tree and stages the file. Mirrors the existing `textdiff` (pure) + `diffView` (TUI) split. The pure package is built so the later hunk-staging sub-project reuses it.

**Tech Stack:** Go 1.26, Bubble Tea, existing `engine`/`domain`/`git`/`repogate` layers.

**Spec:** `docs/superpowers/specs/2026-06-16-conflict-hunk-picker-design.md`.

**Conventions (verified in the codebase):**
- View-stack surface: `surface{ render(m Model) string; update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) }`; `m.pushSurface(s)` / `m.popSurface()` / `m.stackTop()`. The top surface owns all keys (`model.go`: `if s := m.stackTop(); s != nil { return s.update(m, msg) }`) and the whole screen (`view.go`: returns `clipToHeight(s.render(m), h)` before the popup branches). Pointer-receiver surfaces. A stack surface renders its **own footer inline** — the global `footerLine` is not reached, so **no `footerLine` branch is added** (same as the irebase editor).
- `internal/tui` reaches git only through `internal/domain` (archtest-guarded). Engine ops act on the `GitOps` interface; `*git.Repo` satisfies it (compile assertion in `internal/engine/gitops.go`).
- Test helpers already in the tui test package: `key(s string) tea.KeyMsg` (KeyRunes), `keyType(t tea.KeyType) tea.KeyMsg`, `keyMsg(s string)`, `pressRune(t, m, r)` — **reuse them, do not redefine**. In `internal/git`: `newTestRepo(t) (dir string, runner gitexec.Runner)`. In `internal/engine`: `newConflictRepo(t) (dir string, repo *git.Repo)`.
- New keybindings land in `help.go` (the `?` pane). The drift guard `TestHelpFooterCoverage` only checks that footer-registry keys have help rows, so adding new help rows for surface-only keys is safe.
- Gate: `./test.sh race`. Commits end with `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

---

### Task 1: pure core — `internal/hunkpick`

**Files:**
- Create: `internal/hunkpick/hunkpick.go`
- Create: `internal/hunkpick/conflict.go`
- Test: `internal/hunkpick/hunkpick_test.go`, `internal/hunkpick/conflict_test.go`

- [ ] **Step 1: Write the failing decision-model + assembly test**

Create `internal/hunkpick/hunkpick_test.go`:

```go
package hunkpick

import (
	"bytes"
	"testing"
)

func block(cur, inc []string) *Block { return &Block{Current: cur, Incoming: inc} }

func docOf(items ...Item) *Doc { return &Doc{Items: items, FinalNewline: true} }

func TestResolvedTakeSides(t *testing.T) {
	b := block([]string{"a"}, []string{"b"})
	b.Mode = TakeIncoming
	d := docOf(Item{Literal: []string{"top"}}, Item{Block: b}, Item{Literal: []string{"end"}})
	out, ok := d.Resolved()
	if !ok {
		t.Fatal("Resolved ok=false, want true")
	}
	if string(out) != "top\nb\nend\n" {
		t.Fatalf("Resolved = %q", out)
	}
}

func TestResolvedUndecidedBlocksCompletion(t *testing.T) {
	d := docOf(Item{Block: block([]string{"a"}, []string{"b"})})
	if _, ok := d.Resolved(); ok {
		t.Fatal("undecided block must make Resolved ok=false")
	}
	if d.Pending() != 1 {
		t.Fatalf("Pending = %d, want 1", d.Pending())
	}
}

func TestLineByLinePickOrder(t *testing.T) {
	b := block([]string{"foo", "log"}, []string{"bar", "log"})
	b.Mode = LineByLine
	// toggle incoming:bar, then current:foo, then incoming:log → result in that order
	b.ToggleLine(Incoming, 0)
	b.ToggleLine(Current, 0)
	b.ToggleLine(Incoming, 1)
	d := docOf(Item{Block: b})
	out, ok := d.Resolved()
	if !ok {
		t.Fatal("ok=false")
	}
	if string(out) != "bar\nfoo\nlog\n" {
		t.Fatalf("line-by-line order = %q, want bar/foo/log", out)
	}
}

func TestToggleLineRemovesPreservingOrder(t *testing.T) {
	b := block([]string{"x", "y"}, nil)
	b.Mode = LineByLine
	b.ToggleLine(Current, 0) // x
	b.ToggleLine(Current, 1) // y
	b.ToggleLine(Current, 0) // remove x → only y left
	if b.Picked(Current, 0) {
		t.Fatal("current:0 should be unpicked")
	}
	if !b.Picked(Current, 1) {
		t.Fatal("current:1 should remain picked")
	}
	out, _ := docOf(Item{Block: b}).Resolved()
	if !bytes.Equal(out, []byte("y\n")) {
		t.Fatalf("Resolved = %q, want y", out)
	}
}

func TestSetAll(t *testing.T) {
	d := docOf(
		Item{Block: block([]string{"a"}, []string{"b"})},
		Item{Literal: []string{"mid"}},
		Item{Block: block([]string{"c"}, []string{"d"})},
	)
	d.SetAll(TakeCurrent)
	out, ok := d.Resolved()
	if !ok || string(out) != "a\nmid\nc\n" {
		t.Fatalf("SetAll(current) → %q ok=%v", out, ok)
	}
}

func TestNoFinalNewlinePreserved(t *testing.T) {
	d := &Doc{Items: []Item{{Literal: []string{"only"}}}, FinalNewline: false}
	out, _ := d.Resolved()
	if string(out) != "only" {
		t.Fatalf("no-final-newline = %q, want %q", out, "only")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/hunkpick/ -run 'TestResolved|TestLineByLine|TestToggle|TestSetAll|TestNoFinal' -v`
Expected: FAIL — package/types undefined.

- [ ] **Step 3: Implement the decision model + assembly**

Create `internal/hunkpick/hunkpick.go`:

```go
// Package hunkpick is a pure, dependency-free model for picking the resolution
// of a two-version document region by region (and line by line within a
// region). The conflict resolver is its first consumer; hunk staging reuses the
// same Doc/Block model with a different constructor.
package hunkpick

import "bytes"

// Side identifies one of the two pickable versions.
type Side int

const (
	Current Side = iota // git stage :2: (its "ours")
	Incoming            // git stage :3: (its "theirs")
)

// Mode is how a block resolves.
type Mode int

const (
	Undecided Mode = iota
	TakeCurrent
	TakeIncoming
	LineByLine
)

// Pick is one line chosen in line-by-line mode: a side and an index into that
// side's lines.
type Pick struct {
	Side Side
	Line int
}

// Block is one decidable region: the two candidate versions plus the decision.
type Block struct {
	Current  []string
	Incoming []string
	Mode     Mode
	Picks    []Pick // ordered; only meaningful when Mode == LineByLine
}

// lines returns the slice for side s.
func (b *Block) lines(s Side) []string {
	if s == Current {
		return b.Current
	}
	return b.Incoming
}

// Picked reports whether (s, line) is currently in the ordered pick list.
func (b *Block) Picked(s Side, line int) bool {
	for _, p := range b.Picks {
		if p.Side == s && p.Line == line {
			return true
		}
	}
	return false
}

// ToggleLine appends (s, line) to the ordered picks if absent, or removes it if
// present (the remaining picks keep their order). Pure — the caller is
// responsible for having set Mode == LineByLine.
func (b *Block) ToggleLine(s Side, line int) {
	for i, p := range b.Picks {
		if p.Side == s && p.Line == line {
			b.Picks = append(b.Picks[:i], b.Picks[i+1:]...)
			return
		}
	}
	b.Picks = append(b.Picks, Pick{Side: s, Line: line})
}

// resolved appends this block's resolved lines to out, or reports ok=false when
// the block is still Undecided.
func (b *Block) resolved(out []string) ([]string, bool) {
	switch b.Mode {
	case TakeCurrent:
		return append(out, b.Current...), true
	case TakeIncoming:
		return append(out, b.Incoming...), true
	case LineByLine:
		for _, p := range b.Picks {
			ls := b.lines(p.Side)
			if p.Line >= 0 && p.Line < len(ls) {
				out = append(out, ls[p.Line])
			}
		}
		return out, true
	default:
		return out, false
	}
}

// Item is exactly one of: literal passthrough text (Literal != nil), or a
// decidable block (Block != nil).
type Item struct {
	Literal []string
	Block   *Block
}

// Doc is the whole file as an ordered mix of passthrough text and blocks.
type Doc struct {
	Items        []Item
	FinalNewline bool // whether the source file ended with a newline
}

// Blocks returns the decidable blocks in file order (pointers into Items).
func (d *Doc) Blocks() []*Block {
	var bs []*Block
	for _, it := range d.Items {
		if it.Block != nil {
			bs = append(bs, it.Block)
		}
	}
	return bs
}

// Pending counts blocks still Undecided.
func (d *Doc) Pending() int {
	n := 0
	for _, b := range d.Blocks() {
		if b.Mode == Undecided {
			n++
		}
	}
	return n
}

// SetAll sets every block's mode (used for take-all-current / -incoming).
func (d *Doc) SetAll(m Mode) {
	for _, b := range d.Blocks() {
		b.Mode = m
	}
}

// Resolved assembles the whole document. ok=false if any block is Undecided.
func (d *Doc) Resolved() (out []byte, ok bool) {
	var lines []string
	for _, it := range d.Items {
		if it.Block == nil {
			lines = append(lines, it.Literal...)
			continue
		}
		var done bool
		lines, done = it.Block.resolved(lines)
		if !done {
			return nil, false
		}
	}
	buf := bytes.Join(toBytes(lines), []byte("\n"))
	if d.FinalNewline && len(lines) > 0 {
		buf = append(buf, '\n')
	}
	return buf, true
}

func toBytes(ss []string) [][]byte {
	out := make([][]byte, len(ss))
	for i, s := range ss {
		out[i] = []byte(s)
	}
	return out
}
```

- [ ] **Step 4: Run the decision-model tests**

Run: `go test ./internal/hunkpick/ -run 'TestResolved|TestLineByLine|TestToggle|TestSetAll|TestNoFinal' -v`
Expected: PASS.

- [ ] **Step 5: Write the failing parser test**

Create `internal/hunkpick/conflict_test.go`:

```go
package hunkpick

import "testing"

func TestParseConflictTwoWay(t *testing.T) {
	src := "top\n<<<<<<< HEAD\nfoo\nlog\n=======\nbar\nlog\n>>>>>>> feature\nend\n"
	d, err := ParseConflict([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if !d.FinalNewline {
		t.Fatal("FinalNewline should be true")
	}
	bs := d.Blocks()
	if len(bs) != 1 {
		t.Fatalf("got %d blocks, want 1", len(bs))
	}
	if len(bs[0].Current) != 2 || bs[0].Current[0] != "foo" || bs[0].Current[1] != "log" {
		t.Fatalf("current = %v", bs[0].Current)
	}
	if len(bs[0].Incoming) != 2 || bs[0].Incoming[0] != "bar" {
		t.Fatalf("incoming = %v", bs[0].Incoming)
	}
	// literal passthrough preserved: take current → top/foo/log/end
	d.SetAll(TakeCurrent)
	out, _ := d.Resolved()
	if string(out) != "top\nfoo\nlog\nend\n" {
		t.Fatalf("resolved = %q", out)
	}
}

func TestParseConflictDiff3SkipsBase(t *testing.T) {
	src := "<<<<<<< HEAD\nours\n||||||| base\nbasetext\n=======\ntheirs\n>>>>>>> x\n"
	d, err := ParseConflict([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	b := d.Blocks()[0]
	if len(b.Current) != 1 || b.Current[0] != "ours" {
		t.Fatalf("current = %v", b.Current)
	}
	if len(b.Incoming) != 1 || b.Incoming[0] != "theirs" {
		t.Fatalf("incoming = %v (base must be skipped)", b.Incoming)
	}
}

func TestParseConflictMalformed(t *testing.T) {
	// unterminated region
	if _, err := ParseConflict([]byte("<<<<<<< x\nours\n=======\ntheirs\n")); err == nil {
		t.Fatal("missing >>>>>>> should error")
	}
	// separator with no start
	if _, err := ParseConflict([]byte("=======\n")); err == nil {
		t.Fatal("stray ======= should error")
	}
}

func TestParseConflictNoFinalNewline(t *testing.T) {
	d, err := ParseConflict([]byte("plain line"))
	if err != nil {
		t.Fatal(err)
	}
	if d.FinalNewline {
		t.Fatal("FinalNewline should be false")
	}
	if len(d.Blocks()) != 0 {
		t.Fatal("no markers → no blocks")
	}
}
```

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/hunkpick/ -run TestParseConflict -v`
Expected: FAIL — `ParseConflict` undefined.

- [ ] **Step 7: Implement the conflict parser**

Create `internal/hunkpick/conflict.go`:

```go
package hunkpick

import (
	"errors"
	"strings"
)

// conflict-marker prefixes (git writes exactly seven characters).
const (
	markStart = "<<<<<<<"
	markBase  = "|||||||"
	markSep   = "======="
	markEnd   = ">>>>>>>"
)

// ParseConflict splits a conflicted working-tree file into ordered Items:
// passthrough text becomes Literal items; each <<<<<<< / ======= / >>>>>>>
// region becomes a Block{Current, Incoming}. diff3 ||||||| base lines are
// skipped. Unbalanced or out-of-order markers return an error.
func ParseConflict(content []byte) (*Doc, error) {
	final := len(content) > 0 && content[len(content)-1] == '\n'
	text := string(content)
	if final {
		text = text[:len(text)-1]
	}
	var lines []string
	if text != "" {
		lines = strings.Split(text, "\n")
	}

	d := &Doc{FinalNewline: final}
	var lit []string
	flushLit := func() {
		if len(lit) > 0 {
			d.Items = append(d.Items, Item{Literal: lit})
			lit = nil
		}
	}

	const (
		stOut = iota
		stCurrent
		stBase
		stIncoming
	)
	state := stOut
	var cur, inc []string
	for _, ln := range lines {
		switch {
		case strings.HasPrefix(ln, markStart):
			if state != stOut {
				return nil, errors.New("hunkpick: nested <<<<<<< marker")
			}
			flushLit()
			state, cur, inc = stCurrent, nil, nil
		case strings.HasPrefix(ln, markBase) && state == stCurrent:
			state = stBase
		case strings.HasPrefix(ln, markSep):
			if state != stCurrent && state != stBase {
				return nil, errors.New("hunkpick: ======= without <<<<<<<")
			}
			state = stIncoming
		case strings.HasPrefix(ln, markEnd):
			if state != stIncoming {
				return nil, errors.New("hunkpick: >>>>>>> without =======")
			}
			d.Items = append(d.Items, Item{Block: &Block{Current: cur, Incoming: inc}})
			state = stOut
		default:
			switch state {
			case stOut:
				lit = append(lit, ln)
			case stCurrent:
				cur = append(cur, ln)
			case stIncoming:
				inc = append(inc, ln)
				// stBase lines are intentionally dropped.
			}
		}
	}
	if state != stOut {
		return nil, errors.New("hunkpick: unterminated conflict region")
	}
	flushLit()
	return d, nil
}
```

- [ ] **Step 8: Run the parser tests + full package**

Run: `go test ./internal/hunkpick/ -v`
Expected: PASS (all decision-model and parser tests).

- [ ] **Step 9: Commit**

```bash
git add internal/hunkpick/
git commit -m "feat(hunkpick): pure conflict-region decision model + marker parser

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: git verbs + domain read + engine op

**Files:**
- Create: `internal/git/worktree_file.go` (+ test `internal/git/worktree_file_test.go`)
- Modify: `internal/domain/query.go`
- Create: `internal/engine/conflict_hunks.go` (+ test `internal/engine/conflict_hunks_test.go`)
- Modify: `internal/engine/gitops.go`

- [ ] **Step 1: Write the failing verb test**

Create `internal/git/worktree_file_test.go`:

```go
package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadWriteWorktreeFile(t *testing.T) {
	dir, runner := newTestRepo(t) // has README.md committed
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	got, err := repo.ReadWorktreeFile(ctx, "README.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\n" {
		t.Fatalf("read = %q, want hello", got)
	}

	if err := repo.WriteWorktreeFile(ctx, "README.md", []byte("changed\n")); err != nil {
		t.Fatal(err)
	}
	on, _ := os.ReadFile(filepath.Join(dir, "README.md"))
	if string(on) != "changed\n" {
		t.Fatalf("on-disk = %q, want changed", on)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/git/ -run TestReadWriteWorktreeFile -v`
Expected: FAIL — methods undefined.

- [ ] **Step 3: Implement the verbs**

Create `internal/git/worktree_file.go`:

```go
package git

import (
	"context"
	"os"
	"path/filepath"
)

// ReadWorktreeFile reads path (repo-root-relative, slash-separated) from the
// working tree. Used to load a conflicted file's marker text for the hunk
// picker. Not a git invocation — a plain filesystem read, located here because
// the repo already resolves its own top-level.
func (r *Repo) ReadWorktreeFile(ctx context.Context, path string) ([]byte, error) {
	top, err := r.TopLevel(ctx)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(top, filepath.FromSlash(path)))
}

// WriteWorktreeFile writes content to path (repo-root-relative) in the working
// tree, truncating an existing file (its mode is preserved by the OS since the
// file already exists). Used by ResolveConflictHunks to write the assembled
// resolution before staging.
func (r *Repo) WriteWorktreeFile(ctx context.Context, path string, content []byte) error {
	top, err := r.TopLevel(ctx)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(top, filepath.FromSlash(path)), content, 0o644)
}
```

- [ ] **Step 4: Run the verb test**

Run: `go test ./internal/git/ -run TestReadWriteWorktreeFile -v`
Expected: PASS.

- [ ] **Step 5: Add the gated domain read**

In `internal/domain/query.go`, add after the `CommitRange` method (the file already imports `context` and `model`):

```go
// ConflictedFile reads the working-tree bytes of a conflicted path (with its
// merge markers) under a Read reservation. Backs the conflict hunk picker.
func (s *Service) ConflictedFile(ctx context.Context, path string) ([]byte, error) {
	return query(ctx, s, "conflicted-file:"+path, func(c context.Context) ([]byte, error) {
		return s.repo.ReadWorktreeFile(c, path)
	})
}
```

- [ ] **Step 6: Add `WriteWorktreeFile` to `GitOps`**

In `internal/engine/gitops.go`, add to the interface (near the other tree-mutating verbs such as `StagePaths`/`CheckoutSide`):

```go
	WriteWorktreeFile(ctx context.Context, path string, content []byte) error
```

- [ ] **Step 7: Write the failing engine-op test**

Create `internal/engine/conflict_hunks_test.go`:

```go
package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveConflictHunksWritesAndStages(t *testing.T) {
	dir, repo := newConflictRepo(t) // uu.txt is UU (ours/theirs), md.txt is DU
	ctx := context.Background()

	// Assemble an interleaved resolution for uu.txt and resolve md.txt to clear
	// both conflicts so the index for uu.txt is clean afterward.
	content := []byte("ours\ntheirs\n")
	_, err := ResolveConflictHunks{Path: "uu.txt", Content: content}.
		Run(ctx, OpDeps{Repo: repo, Events: make(chan Event, 16)})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "uu.txt")); string(b) != "ours\ntheirs\n" {
		t.Fatalf("uu.txt on disk = %q", b)
	}
	st, _ := repo.Status(ctx)
	for _, f := range st.Conflicts() {
		if f.Path == "uu.txt" {
			t.Fatal("uu.txt should no longer be unmerged after resolve")
		}
	}
}
```

- [ ] **Step 8: Run to verify it fails**

Run: `go test ./internal/engine/ -run TestResolveConflictHunks -v`
Expected: FAIL — `ResolveConflictHunks` undefined (and the `GitOps` interface gains a method, so the compile assertion forces `*git.Repo` to implement `WriteWorktreeFile` — already done in Step 3).

- [ ] **Step 9: Implement the op**

Create `internal/engine/conflict_hunks.go`:

```go
package engine

import "context"

// ResolveConflictHunks writes a hunk-resolved file to the working tree and
// stages it, clearing the unmerged index entry. Content is assembled by the
// frontend (the TUI picker) via internal/hunkpick. Runs with the default
// exclusive (TreeWrite) reservation.
type ResolveConflictHunks struct {
	Path    string
	Content []byte
}

func (op ResolveConflictHunks) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "resolving", Detail: op.Path})
	if err := deps.Repo.WriteWorktreeFile(ctx, op.Path, op.Content); err != nil {
		return Result{}, err
	}
	if err := deps.Repo.StagePaths(ctx, []string{op.Path}); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "resolved " + op.Path + " (hunks)", Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = ResolveConflictHunks{}
```

- [ ] **Step 10: Run the op test + build**

Run: `go build ./... && go test ./internal/engine/ -run TestResolveConflictHunks -v && go test ./internal/git/ ./internal/domain/`
Expected: build clean; tests PASS.

- [ ] **Step 11: Commit**

```bash
git add internal/git/worktree_file.go internal/git/worktree_file_test.go internal/domain/query.go internal/engine/conflict_hunks.go internal/engine/conflict_hunks_test.go internal/engine/gitops.go
git commit -m "feat(engine): ResolveConflictHunks writes the assembled file and stages it

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: the picker surface

**Files:**
- Create: `internal/tui/conflict_picker.go`
- Test: `internal/tui/conflict_picker_test.go`

The surface holds the parsed `Doc`, a flattened block list, and a 2D cursor
(block index, side, line). It owns the screen and renders its own footer.

- [ ] **Step 1: Write the failing surface tests**

Create `internal/tui/conflict_picker_test.go` (reuses the package-level `key`/`keyType` helpers from `irebase_view_test.go`):

```go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/hunkpick"
)

func pickerDoc() *hunkpick.Doc {
	d, _ := hunkpick.ParseConflict([]byte(
		"top\n<<<<<<< HEAD\nfoo\n=======\nbar\n>>>>>>> x\nmid\n<<<<<<< HEAD\nA\nB\n=======\nC\n>>>>>>> x\n"))
	return d
}

func TestConflictPickerTakeSides(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{stack: &viewStack{entries: []surface{e}}, width: 80, height: 24}
	// region 0 → current, region 1 → incoming
	m, _ = e.update(m, key("c"))
	m, _ = e.update(m, key("n")) // next region
	m, _ = e.update(m, key("i"))
	if e.doc.Pending() != 0 {
		t.Fatalf("Pending = %d, want 0", e.doc.Pending())
	}
	out, ok := e.doc.Resolved()
	if !ok || string(out) != "top\nfoo\nmid\nC\n" {
		t.Fatalf("resolved = %q ok=%v", out, ok)
	}
}

func TestConflictPickerTakeAll(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{stack: &viewStack{entries: []surface{e}}, width: 80, height: 24}
	m, _ = e.update(m, key("I")) // take all incoming
	out, ok := e.doc.Resolved()
	if !ok || string(out) != "top\nbar\nmid\nC\n" {
		t.Fatalf("take-all-incoming = %q ok=%v", out, ok)
	}
}

func TestConflictPickerSpaceTogglesLineByLine(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{stack: &viewStack{entries: []surface{e}}, width: 80, height: 24}
	// focus region 0, current side, line 0; space picks it line-by-line
	m, _ = e.update(m, keyMsg("space"))
	b := e.doc.Blocks()[0]
	if b.Mode != hunkpick.LineByLine || !b.Picked(hunkpick.Current, 0) {
		t.Fatalf("space did not start line-by-line pick: mode=%v", b.Mode)
	}
}

func TestConflictPickerSideSwitchAndCursor(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{stack: &viewStack{entries: []surface{e}}, width: 80, height: 24}
	m, _ = e.update(m, keyType(rightKey())) // → incoming
	if e.side != hunkpick.Incoming {
		t.Fatal("→ should focus incoming side")
	}
	m, _ = e.update(m, keyType(leftKey())) // ← current
	if e.side != hunkpick.Current {
		t.Fatal("← should focus current side")
	}
}

func TestConflictPickerEnterGateAndApply(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{stack: &viewStack{entries: []surface{e}}, width: 80, height: 24, svc: nil}
	// enter while pending: no apply, status set, surface still on top
	m, _ = e.update(m, keyMsg("enter"))
	if m.statusMsg == "" || m.stackTop() == nil {
		t.Fatal("enter with pending regions should warn and keep the surface")
	}
}

func TestConflictPickerRendersMarkers(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{stack: &viewStack{entries: []surface{e}}, width: 80, height: 24}
	out := e.render(m)
	if out == "" {
		t.Fatal("render produced nothing")
	}
}

func rightKey() tea.KeyType { return tea.KeyRight }
func leftKey() tea.KeyType  { return tea.KeyLeft }
```

> Note: `tea` is imported transitively via the helpers; add `tea "github.com/charmbracelet/bubbletea"` to the import block (the `rightKey`/`leftKey` helpers reference `tea.KeyType`).

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestConflictPicker -v`
Expected: FAIL — `newConflictPicker`/`conflictPicker` undefined.

- [ ] **Step 3: Implement the surface**

Create `internal/tui/conflict_picker.go`:

```go
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/hunkpick"
)

var (
	pickerDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	pickerFocus = lipgloss.NewStyle().Bold(true)
)

// conflictPicker is the region/line-level conflict resolver surface. Rows show
// the file top-to-bottom; conflict regions render side-by-side. A 2D cursor
// (block index, side, line) drives the picks.
type conflictPicker struct {
	path   string
	doc    *hunkpick.Doc
	blocks []*hunkpick.Block
	bi     int          // focused block index into blocks
	side   hunkpick.Side // focused column
	line   int          // cursor line within the focused side
	scroll int
}

func newConflictPicker(path string, doc *hunkpick.Doc) *conflictPicker {
	return &conflictPicker{path: path, doc: doc, blocks: doc.Blocks(), side: hunkpick.Current}
}

// cur returns the focused block, or nil when there are none.
func (e *conflictPicker) cur() *hunkpick.Block {
	if e.bi < 0 || e.bi >= len(e.blocks) {
		return nil
	}
	return e.blocks[e.bi]
}

// clampLine keeps the cursor within the focused side's line count.
func (e *conflictPicker) clampLine() {
	b := e.cur()
	if b == nil {
		e.line = 0
		return
	}
	n := len(b.Current)
	if e.side == hunkpick.Incoming {
		n = len(b.Incoming)
	}
	if e.line >= n {
		e.line = n - 1
	}
	if e.line < 0 {
		e.line = 0
	}
}

func (e *conflictPicker) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	b := e.cur()
	switch msg.String() {
	case "esc":
		return m.popSurface(), nil
	case "left":
		e.side = hunkpick.Current
		e.clampLine()
	case "right":
		e.side = hunkpick.Incoming
		e.clampLine()
	case "up", "k":
		if e.line > 0 {
			e.line--
		} else if e.bi > 0 {
			e.bi--
			e.clampLine()
			e.line = e.sideLen() - 1
			if e.line < 0 {
				e.line = 0
			}
		}
	case "down", "j":
		if e.line < e.sideLen()-1 {
			e.line++
		} else if e.bi < len(e.blocks)-1 {
			e.bi++
			e.line = 0
		}
	case "n":
		if e.bi < len(e.blocks)-1 {
			e.bi++
			e.line = 0
		}
	case "p":
		if e.bi > 0 {
			e.bi--
			e.line = 0
		}
	case "c":
		if b != nil {
			b.Mode = hunkpick.TakeCurrent
		}
	case "i":
		if b != nil {
			b.Mode = hunkpick.TakeIncoming
		}
	case "C":
		e.doc.SetAll(hunkpick.TakeCurrent)
	case "I":
		e.doc.SetAll(hunkpick.TakeIncoming)
	case " ":
		if b != nil && e.sideLen() > 0 {
			if b.Mode != hunkpick.LineByLine {
				b.Mode = hunkpick.LineByLine
				b.Picks = nil
			}
			b.ToggleLine(e.side, e.line)
		}
	case "enter":
		if n := e.doc.Pending(); n > 0 {
			m.statusMsg = fmt.Sprintf("%d region(s) left to resolve", n)
			e.focusFirstUndecided()
			return m, nil
		}
		out, ok := e.doc.Resolved()
		if !ok {
			m.statusMsg = "internal error: unresolved regions"
			return m, nil
		}
		m = m.popSurface()
		m.conflictPopup = nil
		m.reopenConflict = true
		return m.startOp(engine.ResolveConflictHunks{Path: e.path, Content: out})
	}
	return m, nil
}

func (e *conflictPicker) sideLen() int {
	b := e.cur()
	if b == nil {
		return 0
	}
	if e.side == hunkpick.Incoming {
		return len(b.Incoming)
	}
	return len(b.Current)
}

func (e *conflictPicker) focusFirstUndecided() {
	for i, b := range e.blocks {
		if b.Mode == hunkpick.Undecided {
			e.bi, e.line, e.side = i, 0, hunkpick.Current
			return
		}
	}
}

// badge labels a block's current decision.
func badge(b *hunkpick.Block) string {
	switch b.Mode {
	case hunkpick.TakeCurrent:
		return "✓ current"
	case hunkpick.TakeIncoming:
		return "✓ incoming"
	case hunkpick.LineByLine:
		return "line-by-line"
	default:
		return "· undecided"
	}
}

func (e *conflictPicker) render(m Model) string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Resolve conflicts: %s    %d regions · %d left\n",
		e.path, len(e.blocks), e.doc.Pending())
	b.WriteString(strings.Repeat("─", min(w, 60)) + "\n")

	colW := (w - 5) / 2
	if colW < 8 {
		colW = 8
	}
	blockNo := 0
	for _, it := range e.doc.Items {
		if it.Block == nil {
			for _, l := range it.Literal {
				b.WriteString(pickerDim.Render("  " + truncate(l, w-2)))
				b.WriteString("\n")
			}
			continue
		}
		blk := it.Block
		focused := blockNo == e.bi
		marker := "  "
		if focused {
			marker = "▶ "
		}
		header := fmt.Sprintf("%sregion %d/%d — %s", marker, blockNo+1, len(e.blocks), badge(blk))
		if focused {
			b.WriteString(pickerFocus.Render(header))
		} else {
			b.WriteString(pickerDim.Render(header))
		}
		b.WriteString("\n")
		rows := len(blk.Current)
		if len(blk.Incoming) > rows {
			rows = len(blk.Incoming)
		}
		for r := 0; r < rows; r++ {
			left := cell(blk, hunkpick.Current, r, focused && e.side == hunkpick.Current && e.line == r, colW)
			right := cell(blk, hunkpick.Incoming, r, focused && e.side == hunkpick.Incoming && e.line == r, colW)
			b.WriteString(left + " ║ " + right + "\n")
		}
		if blk.Mode == hunkpick.LineByLine {
			b.WriteString(pickerDim.Render("  result:") + "\n")
			tmp := &hunkpick.Doc{Items: []hunkpick.Item{{Block: blk}}}
			if out, ok := tmp.Resolved(); ok {
				for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
					b.WriteString("    " + truncate(l, w-4) + "\n")
				}
			}
		}
		blockNo++
	}
	b.WriteString("\n[←/→] side  [↑/↓] line  [space] pick line  [c]urrent [i]ncoming  [C/I] all  [n/p] region  [enter] apply  [esc] cancel")
	return b.String()
}

// (uses the builtin min from Go 1.21+; no local helper needed)

// cell renders one candidate line with an optional checkbox (line-by-line) and
// cursor highlight.
func cell(blk *hunkpick.Block, side hunkpick.Side, r int, cursor bool, w int) string {
	var lines []string
	if side == hunkpick.Current {
		lines = blk.Current
	} else {
		lines = blk.Incoming
	}
	text := ""
	if r < len(lines) {
		text = lines[r]
	}
	box := ""
	if blk.Mode == hunkpick.LineByLine && r < len(lines) {
		if blk.Picked(side, r) {
			box = "[x] "
		} else {
			box = "[ ] "
		}
	}
	body := truncate(box+text, w)
	if cursor {
		return selectedRow.Render(padRight("> "+body, w+2))
	}
	if r >= len(lines) {
		return padRight("", w+2)
	}
	return padRight("  "+body, w+2)
}
```

> `render` uses the builtin `min` (Go 1.21+); no local helper is defined.

- [ ] **Step 4: Run the surface tests**

Run: `go test ./internal/tui/ -run TestConflictPicker -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/conflict_picker.go internal/tui/conflict_picker_test.go
git commit -m "feat(tui): conflict hunk picker surface (region/line selection, 2D cursor)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: wiring — open the picker, relabel current/incoming, help

**Files:**
- Modify: `internal/tui/conflict_popup.go` (enter opens picker; relabel labels)
- Modify: `internal/tui/op.go` (load cmd + msg)
- Modify: `internal/tui/model.go` (handle the loaded msg → push picker)
- Modify: `internal/tui/help.go`
- Test: `internal/tui/conflict_picker_test.go` (append the load→push test)

- [ ] **Step 1: Add the load cmd + msg**

In `internal/tui/op.go`, after `loadIrebaseCmd` (the file imports `context` and `model`; add `hunkpick` is NOT needed here):

```go
// conflictFileLoadedMsg carries a conflicted file's marker text for the picker.
type conflictFileLoadedMsg struct {
	path    string
	content []byte
	err     error
}

// loadConflictFileCmd reads a conflicted file's working-tree bytes off the UI
// thread; the resulting msg parses + pushes the picker.
func (m Model) loadConflictFileCmd(path string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		c, err := svc.ConflictedFile(context.Background(), path)
		return conflictFileLoadedMsg{path: path, content: c, err: err}
	}
}
```

- [ ] **Step 2: Open the picker from the conflict popup**

In `internal/tui/conflict_popup.go`, in `updateConflictPopupKey`, add an `"enter"` case inside the first `switch msg.String()` block (alongside `esc`/`up`/`down`/`A`):

```go
	case "enter":
		if p.sel < 0 || p.sel >= len(p.files) {
			return m, nil
		}
		f := p.files[p.sel]
		if f.ConflictClass() != model.ConflictBothSides {
			m.statusMsg = "hunk picker: only for files modified on both sides"
			return m, nil
		}
		return m, m.loadConflictFileCmd(f.Path)
```

Then relabel the whole-file action hints (current/incoming, not ours/theirs) in `actionHint`:

```go
		if f.ConflictClass() == model.ConflictBothSides {
			parts = append(parts, "[enter] pick hunks", "[o] current", "[t] incoming", "[m] mark resolved")
```

(The `o`/`t` keys are unchanged — only the labels change. `keepModifiedAction` and the engine `KeepOurs`/`KeepTheirs` constants stay: they map to git's `--ours`/`--theirs`, which the engine correctly speaks.)

- [ ] **Step 3: Handle the loaded msg in `model.go`**

In `internal/tui/model.go`'s message `switch` (add `"github.com/gigagit/gg/internal/hunkpick"` and `"github.com/gigagit/gg/internal/textdiff"` to imports), after the `irebaseLoadedMsg` case:

```go
	case conflictFileLoadedMsg:
		if msg.err != nil {
			m.statusMsg = "conflict: " + msg.err.Error()
			return m, nil
		}
		if textdiff.IsBinary(msg.content) {
			m.statusMsg = "hunk picker: binary file"
			return m, nil
		}
		doc, err := hunkpick.ParseConflict(msg.content)
		if err != nil {
			m.statusMsg = "hunk picker: " + err.Error()
			return m, nil
		}
		if len(doc.Blocks()) == 0 {
			m.statusMsg = "hunk picker: no conflict regions found"
			return m, nil
		}
		m = m.pushSurface(newConflictPicker(msg.path, doc))
		return m, nil
```

(`m.conflictPopup` stays set, so cancelling the picker with `esc` reveals it unchanged; applying clears it and sets `reopenConflict`.)

- [ ] **Step 4: Help rows**

In `internal/tui/help.go`, update the existing "Conflicts (x)" rows to current/incoming and add a picker section:

```go
		r("o/t", "keep current / incoming (both-modified files)"),
```

and after the conflicts section add:

```go
		h("Conflict hunk picker (enter on a both-modified file)"),
		r("←/→", "switch the focused side (current ↔ incoming)"),
		r("↑/k ↓/j", "move the line cursor (steps across regions)"),
		r("space", "pick the cursor line (builds the result in pick order)"),
		r("c / i", "take the whole region from current / incoming"),
		r("C / I", "take all regions from current / incoming"),
		r("n / p", "jump to the next / previous region"),
		r("enter", "apply when every region is resolved"),
		r("esc", "cancel (close without resolving)"),
```

Also update the README-facing whole-file text in `help.go` if it says "ours"/"theirs" elsewhere (the `o/t` row above is the only one).

- [ ] **Step 5: Append the load→push test**

Append to `internal/tui/conflict_picker_test.go`:

```go
func TestConflictFileLoadedPushesPicker(t *testing.T) {
	m := Model{width: 80, height: 24}
	content := []byte("<<<<<<< HEAD\na\n=======\nb\n>>>>>>> x\n")
	updated, _ := m.Update(conflictFileLoadedMsg{path: "f.txt", content: content})
	m = updated.(Model)
	if _, ok := m.stackTop().(*conflictPicker); !ok {
		t.Fatal("loaded conflict file should push the picker surface")
	}
}

func TestConflictFileLoadedBinaryNoOp(t *testing.T) {
	m := Model{width: 80, height: 24}
	updated, _ := m.Update(conflictFileLoadedMsg{path: "f.bin", content: []byte("\x00\x01\x02")})
	m = updated.(Model)
	if m.stackTop() != nil {
		t.Fatal("binary file must not push a surface")
	}
}
```

- [ ] **Step 6: Run the tui package**

Run: `go test ./internal/tui/`
Expected: PASS, including `TestHelpFooterCoverage` and the existing conflict-popup tests (the relabel is text-only; if a popup test asserts the literal "ours"/"theirs" hint string, update that assertion to "current"/"incoming").

- [ ] **Step 7: Commit**

```bash
git add internal/tui/conflict_popup.go internal/tui/op.go internal/tui/model.go internal/tui/help.go internal/tui/conflict_picker_test.go
git commit -m "feat(tui): open the conflict hunk picker; relabel current/incoming

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: docs

**Files:**
- Modify: `README.md`, `CHANGELOG.md`

- [ ] **Step 1: README**

In the TUI key table, update the `x` row's "keep ours/theirs" to "keep current/incoming", and add that pressing `enter` on a both-modified file opens the hunk picker (`←/→` side, `↑/↓` line, `space` pick line, `c`/`i` whole region, `C`/`I` all, `n`/`p` jump, `enter` apply, `esc` cancel).

- [ ] **Step 2: CHANGELOG**

Under `## [Unreleased]` → `### Added`:

```markdown
- TUI: **conflict hunk picker** — press `enter` on a both-modified file in the
  `x` conflict resolver to open a GitKraken-style region/line editor. Per
  conflict region: take the whole **current** or **incoming** side (`c`/`i`),
  or `space` to pick individual lines from either side (they land in the result
  in pick order); `C`/`I` take all regions one way; `←/→` switch side, `↑/↓`
  move the line cursor, `n`/`p` jump regions, `enter` applies once every region
  is resolved. The whole-file resolver's labels are now **current/incoming**
  (clearer than ours/theirs, which invert during a rebase).
```

- [ ] **Step 3: Commit**

```bash
git add README.md CHANGELOG.md
git commit -m "docs: conflict hunk picker in README/CHANGELOG

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Final verification (after all tasks)

- [ ] `./test.sh race` — vet+gofmt clean, all unit + e2e green.
- [ ] Manual smoke (REQUIRED — no e2e covers the TUI path): build, create a real both-modified conflict, open the picker, take-current on one region, line-by-line on another, `enter`, confirm the file is resolved and staged and the conflict popup reopens on the smaller set.
- [ ] `superpowers:finishing-a-development-branch`.
- [ ] After merge, RE-RUN `./test.sh race` on merged `main`.

---

## Self-Review

**1. Spec coverage:**
- Pure decision model (current/incoming, take-whole, line-by-line in pick order, take-all) → Task 1 (`hunkpick`). ✓
- Conflict-marker parser (2-way, diff3 base skipped, malformed errors, final-newline) → Task 1 (`conflict.go`). ✓
- Picker surface: 2D cursor (`←/→` side, `↑/↓` line), `space` pick, `c`/`i`, `C`/`I`, `n`/`p`, region/side/cursor markers, result preview, pending-gate → Task 3. ✓
- Engine op writes worktree + stages; `WriteWorktreeFile` verb in `GitOps` → Task 2. ✓
- Entry from the `x` popup on both-modified text files; binary/parse guards → Task 4. ✓
- Relabel ours/theirs → current/incoming (UI only; engine keeps git terms) → Task 4. ✓
- Help section; footer rendered inline (no `footerLine` branch) → Task 4 + Task 3. ✓
- Docs → Task 5. ✓

**2. Placeholder scan:** complete code throughout; the two conditional notes (drop local `min` if the builtin is in scope; update a popup test assertion only if it pins the literal hint string) are concrete.

**3. Type consistency:** `hunkpick.{Side,Mode,Pick,Block,Item,Doc}` and methods (`ParseConflict`, `Resolved`, `Pending`, `SetAll`, `ToggleLine`, `Picked`, `Blocks`) are used identically across Tasks 1, 3, 4. `engine.ResolveConflictHunks{Path, Content}` and `WriteWorktreeFile(ctx, path, content)` match across Tasks 2–4. `conflictFileLoadedMsg{path, content, err}` and `loadConflictFileCmd` are consistent between `op.go` and `model.go` (Task 4). `newConflictPicker(path, doc)` matches between Task 3 and Task 4.

**Out of scope (v1):** base as a third pick column; CRLF normalization; CLI/MCP hunk resolution; hunk **staging** (the next sub-project, which reuses `internal/hunkpick`).
