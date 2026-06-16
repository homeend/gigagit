# Hunk/line staging — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stage a modified file at the region/line level in the TUI — `H` on the Files panel opens a shared hunk picker; per hunk take the whole index side (leave unstaged), the whole working-tree side (stage it), or assemble the staged content line-by-line.

**Architecture:** Reuse the pure `internal/hunkpick` core with a new `FromDiff` constructor (textdiff-driven). Generalize the conflict resolver's surface into a shared `hunkPicker` (injected labels/gate/apply). A new `StageHunks` engine op sets the index blob directly (`hash-object -w` + `update-index --cacheinfo`) so the working tree is never touched.

**Tech Stack:** Go 1.26, Bubble Tea, existing `engine`/`domain`/`git`/`textdiff`/`hunkpick` layers.

**Spec:** `docs/superpowers/specs/2026-06-16-hunk-staging-design.md`.

**Key facts (verified on current `main`, tip `35d70f8`):**
- `internal/tui/conflict_picker.go` holds `conflictPicker` (fields `path,doc,blocks,bi,side,line`; methods `update`/`render`/`cur`/`sideLen`/`clampLine`/`focusFirstUndecided`; helpers `badge`,`cell`). It is the surface to generalize.
- `hunkpick` (`internal/hunkpick`): `ParseConflict`, `Doc{Items,FinalNewline}`, `Item{Literal|Block}`, `Block{Current,Incoming []string;Mode;Picks}`, `Doc.Blocks()/Pending()/SetAll(mode)/Resolved()`, `Block.ToggleLine/Picked`. Pure (only imports `bytes`,`strings`,`errors`).
- `textdiff.Compare(old, newB []byte, opts Options) Result`; `Result.Rows []Row`; `Row{Kind,Left,Right,...}`; `Kind` consts `Same`/`Changed`/`Del`/`Add`. Pure.
- Domain reads: index blob = `svc.ShowFile(ctx, "", path)`; working-tree bytes = `svc.ConflictedFile(ctx, path)` (renamed to `WorktreeFile` in Task 4). `*git.Repo` satisfies `engine.GitOps` (compile assertion in `internal/engine/gitops.go`); `*git.Repo` is the only implementer.
- `gitexec.Runner` has **no stdin** (`Run`/`RunEnv`/`Stream` only) → `hash-object` reads a temp file, not stdin.
- TUI key dispatch is one big `switch msg.String()` in `internal/tui/model.go`'s `tea.KeyMsg` arm. `opFinishedMsg` ends with `m.loadCmd()` (full panel reload), so an op-based apply needs no extra refresh. `panelFiles`/`panelStaged` exist; `m.isFilesPanel(p)`, `m.opsIdle()`, `m.backingIndex(p)`, `m.canStage()` exist.
- Test helpers (tui pkg): `key(s)`, `keyType(t)`, `keyMsg(s)`, `pressRune`. `internal/git`: `newTestRepo(t)(dir,runner)`. `internal/engine`: `newConflictRepo(t)(dir,*git.Repo)`.
- New TUI keys must land in `help.go` AND `footer.go` (drift guard `TestHelpFooterCoverage`). A stack surface renders its own footer inline (no `footerLine` branch).
- Gate: `./test.sh race`. Commits end `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

---

### Task 1: `hunkpick.FromDiff` — build a Doc from a line diff

**Files:**
- Create: `internal/hunkpick/fromdiff.go`
- Test: `internal/hunkpick/fromdiff_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/hunkpick/fromdiff_test.go`:

```go
package hunkpick

import "testing"

func TestFromDiffSplitsLiteralAndBlocks(t *testing.T) {
	// left (index) vs right (working tree): line 2 changed, a line appended.
	left := []byte("a\nb\nc\n")
	right := []byte("a\nB\nc\nd\n")
	d := FromDiff(left, right)
	if !d.FinalNewline {
		t.Fatal("FinalNewline should follow the right side")
	}
	// Default is undecided until the caller sets modes; take-incoming = the
	// working-tree content reproduced exactly.
	d.SetAll(TakeIncoming)
	out, ok := d.Resolved()
	if !ok || string(out) != "a\nB\nc\nd\n" {
		t.Fatalf("take-incoming = %q ok=%v, want the working tree", out, ok)
	}
	// take-current (index) = the original index content reproduced exactly.
	d.SetAll(TakeCurrent)
	out, _ = d.Resolved()
	if string(out) != "a\nb\nc\n" {
		t.Fatalf("take-current = %q, want the index", out)
	}
	if len(d.Blocks()) == 0 {
		t.Fatal("a changed file must yield at least one block")
	}
}

func TestFromDiffNoChangeHasNoBlocks(t *testing.T) {
	d := FromDiff([]byte("x\ny\n"), []byte("x\ny\n"))
	if len(d.Blocks()) != 0 {
		t.Fatalf("identical sides → 0 blocks, got %d", len(d.Blocks()))
	}
}

func TestFromDiffAllAdd(t *testing.T) {
	// empty index (untracked-like) → one block, all incoming.
	d := FromDiff(nil, []byte("new1\nnew2\n"))
	bs := d.Blocks()
	if len(bs) != 1 || len(bs[0].Current) != 0 || len(bs[0].Incoming) != 2 {
		t.Fatalf("all-add block wrong: %+v", bs)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/hunkpick/ -run TestFromDiff -v`
Expected: FAIL — `FromDiff` undefined.

- [ ] **Step 3: Implement `FromDiff`**

Create `internal/hunkpick/fromdiff.go`:

```go
package hunkpick

import "github.com/gigagit/gg/internal/textdiff"

// FromDiff builds a Doc from a line diff of two file versions (left = the
// baseline, e.g. the index; right = the new side, e.g. the working tree).
// Equal runs become Literal items; each contiguous changed run becomes a
// Block{Current: left lines, Incoming: right lines}. Blocks start Undecided;
// the caller sets the default mode. Used by hunk staging.
func FromDiff(left, right []byte) *Doc {
	res := textdiff.Compare(left, right, textdiff.Options{})
	d := &Doc{FinalNewline: len(right) > 0 && right[len(right)-1] == '\n'}

	var lit []string
	var cur, inc []string
	inBlock := false
	flushLit := func() {
		if len(lit) > 0 {
			d.Items = append(d.Items, Item{Literal: lit})
			lit = nil
		}
	}
	flushBlock := func() {
		if inBlock {
			d.Items = append(d.Items, Item{Block: &Block{Current: cur, Incoming: inc}})
			cur, inc, inBlock = nil, nil, false
		}
	}

	for _, row := range res.Rows {
		if row.Kind == textdiff.Same {
			flushBlock()
			lit = append(lit, row.Left)
			continue
		}
		// a changed row (Changed/Del/Add): start/continue a block.
		flushLit()
		inBlock = true
		switch row.Kind {
		case textdiff.Changed:
			cur = append(cur, row.Left)
			inc = append(inc, row.Right)
		case textdiff.Del:
			cur = append(cur, row.Left)
		case textdiff.Add:
			inc = append(inc, row.Right)
		}
	}
	flushBlock()
	flushLit()
	return d
}
```

- [ ] **Step 4: Run + verify the package**

Run: `go test ./internal/hunkpick/ -v`
Expected: PASS (FromDiff tests + the existing ParseConflict/decision-model tests).

- [ ] **Step 5: Commit**

```bash
git add internal/hunkpick/fromdiff.go internal/hunkpick/fromdiff_test.go
git commit -m "feat(hunkpick): FromDiff builds a Doc from a line diff (staging)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: `StageBlob` verb + `StageHunks` op

**Files:**
- Create: `internal/git/stage_blob.go` (+ test `internal/git/stage_blob_test.go`)
- Create: `internal/engine/stage_hunks.go` (+ test `internal/engine/stage_hunks_test.go`)
- Modify: `internal/engine/gitops.go`

- [ ] **Step 1: Write the failing verb test**

Create `internal/git/stage_blob_test.go`:

```go
package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestStageBlobSetsIndexNotWorktree(t *testing.T) {
	dir, runner := newTestRepo(t) // README.md = "hello\n" committed & in index
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	// Modify the working tree so index != working tree.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("WORKING\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Stage a DIFFERENT content than either side (proves we set the index to
	// exactly the bytes given, and the working tree is untouched).
	if err := repo.StageBlob(ctx, "README.md", []byte("STAGED\n")); err != nil {
		t.Fatal(err)
	}

	// Working tree unchanged on disk.
	if b, _ := os.ReadFile(filepath.Join(dir, "README.md")); string(b) != "WORKING\n" {
		t.Fatalf("working tree = %q, want WORKING (untouched)", b)
	}
	// Index now holds STAGED.
	out, err := exec.Command("git", "-C", dir, "show", ":README.md").CombinedOutput()
	if err != nil {
		t.Fatalf("git show :README.md: %v\n%s", err, out)
	}
	if string(out) != "STAGED\n" {
		t.Fatalf("index = %q, want STAGED", out)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/git/ -run TestStageBlob -v`
Expected: FAIL — `StageBlob` undefined.

- [ ] **Step 3: Implement the verb**

Create `internal/git/stage_blob.go`:

```go
package git

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
)

// StageBlob sets the index entry for path to exactly content, without touching
// the working tree: hash-object writes a blob (with --path so clean filters
// apply as if the bytes were at path), then update-index --cacheinfo rewrites
// the index entry. The mode is taken from the file's current index entry, so
// the executable bit is preserved. The Runner has no stdin, so the content is
// hashed from a temp file outside the working tree.
func (r *Repo) StageBlob(ctx context.Context, path string, content []byte) error {
	mode, err := r.indexMode(ctx, path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "gg-stage-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	res, err := r.Runner.Run(ctx, "git hash-object",
		gitcmd.New("hash-object").Arg("-w", "--path="+path, "--").Arg(tmp.Name()).ToArgv())
	if err != nil {
		return err
	}
	sha := strings.TrimSpace(res.Stdout)
	if sha == "" {
		return fmt.Errorf("hash-object: empty sha for %s", path)
	}
	_, err = r.Runner.Run(ctx, "git update-index",
		gitcmd.New("update-index").Arg("--cacheinfo", mode+","+sha+","+path).ToArgv())
	return err
}

// indexMode returns the octal mode (e.g. "100644") of path's current index
// entry. An empty result means the path is not in the index.
func (r *Repo) indexMode(ctx context.Context, path string) (string, error) {
	res, err := r.Runner.Run(ctx, "git ls-files",
		gitcmd.New("ls-files").Arg("-s", "--").Arg(path).ToArgv())
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(res.Stdout)
	if out == "" {
		return "", fmt.Errorf("stage: %s is not tracked", path)
	}
	// format: "<mode> <sha> <stage>\t<path>"
	mode, _, ok := strings.Cut(out, " ")
	if !ok {
		return "", fmt.Errorf("stage: cannot parse ls-files output %q", out)
	}
	return mode, nil
}
```

- [ ] **Step 4: Run the verb test**

Run: `go test ./internal/git/ -run TestStageBlob -v`
Expected: PASS.

- [ ] **Step 5: Add `StageBlob` to `GitOps`**

In `internal/engine/gitops.go`, add near `WriteWorktreeFile`:

```go
	StageBlob(ctx context.Context, path string, content []byte) error
```

- [ ] **Step 6: Write the failing op test**

Create `internal/engine/stage_hunks_test.go`:

```go
package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestStageHunksStagesContentLeavesWorktree(t *testing.T) {
	dir, repo := newConflictRepo(t) // gives us a real repo; we ignore the conflict
	ctx := context.Background()
	// Start clean on a tracked file.
	run := func(args ...string) {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		_ = c.Run()
	}
	run("merge", "--abort")
	os.WriteFile(filepath.Join(dir, "uu.txt"), []byte("line1\nWORK\n"), 0o644)

	_, err := StageHunks{Path: "uu.txt", Content: []byte("line1\nSTAGED\n")}.
		Run(ctx, OpDeps{Repo: repo, Events: make(chan Event, 16)})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "uu.txt")); string(b) != "line1\nWORK\n" {
		t.Fatalf("working tree = %q, want WORK (untouched)", b)
	}
	out, _ := exec.Command("git", "-C", dir, "show", ":uu.txt").CombinedOutput()
	if string(out) != "line1\nSTAGED\n" {
		t.Fatalf("index = %q, want STAGED", out)
	}
}
```

- [ ] **Step 7: Run to verify it fails**

Run: `go test ./internal/engine/ -run TestStageHunks -v`
Expected: FAIL — `StageHunks` undefined (and `GitOps` gained `StageBlob`, satisfied by Step 3).

- [ ] **Step 8: Implement the op**

Create `internal/engine/stage_hunks.go`:

```go
package engine

import "context"

// StageHunks sets the index entry for Path to exactly Content (assembled by the
// TUI staging picker via internal/hunkpick), leaving the working tree
// untouched. Default exclusive (TreeWrite) reservation.
type StageHunks struct {
	Path    string
	Content []byte
}

func (op StageHunks) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "staging", Detail: op.Path})
	if err := deps.Repo.StageBlob(ctx, op.Path, op.Content); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "staged hunks in " + op.Path, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = StageHunks{}
```

- [ ] **Step 9: Run the op test + build**

Run: `go build ./... && go test ./internal/git/ ./internal/engine/ -run 'TestStageBlob|TestStageHunks' -v`
Expected: build clean; both PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/git/stage_blob.go internal/git/stage_blob_test.go internal/engine/stage_hunks.go internal/engine/stage_hunks_test.go internal/engine/gitops.go
git commit -m "feat(engine): StageHunks sets the index blob, working tree untouched

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: generalize the surface into a shared `hunkPicker`

**Files:**
- Modify: `internal/tui/conflict_picker.go` (rename type + inject params)
- Modify: `internal/tui/conflict_picker_test.go` (type assertions: `*conflictPicker` → `*hunkPicker`)
- Test: add a staging-params surface test to `internal/tui/conflict_picker_test.go`

- [ ] **Step 1: Rewrite the surface as a parameterized `hunkPicker`**

Replace the type, constructor, and the `badge`/`render`/`enter` parts of `internal/tui/conflict_picker.go`. The 2D-cursor movement (`left/right/up/down/j/k/n/p`), `c/i/C/I`, `space`, `cur`/`sideLen`/`clampLine`/`focusFirstUndecided`, and `cell` are UNCHANGED. The full new file:

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

// hunkPicker is the shared region/line picker surface. It serves both the
// conflict resolver (current/incoming, every region must be decided) and hunk
// staging (index/working, no gate) — the difference is the injected labels,
// the requireAll gate, and the apply callback. A 2D cursor (block index, side,
// line) drives the picks.
type hunkPicker struct {
	title      string // header prefix, e.g. "Resolve conflicts: f" / "Stage hunks: f"
	leftLabel  string // "current" / "index"
	rightLabel string // "incoming" / "working"
	requireAll bool   // gate enter on Pending==0 (conflicts) vs apply freely (staging)
	apply      func(m Model, content []byte) (Model, tea.Cmd)

	doc    *hunkpick.Doc
	blocks []*hunkpick.Block
	bi     int
	side   hunkpick.Side
	line   int
}

// newConflictPicker wires the conflict-resolution params.
func newConflictPicker(path string, doc *hunkpick.Doc) *hunkPicker {
	return &hunkPicker{
		title:      "Resolve conflicts: " + path,
		leftLabel:  "current",
		rightLabel: "incoming",
		requireAll: true,
		apply: func(m Model, content []byte) (Model, tea.Cmd) {
			m = m.popSurface()
			m.conflictPopup = nil
			m.reopenConflict = true
			return m.startOp(engine.ResolveConflictHunks{Path: path, Content: content})
		},
		doc: doc, blocks: doc.Blocks(), side: hunkpick.Current,
	}
}

// newStagePicker wires the hunk-staging params.
func newStagePicker(path string, doc *hunkpick.Doc) *hunkPicker {
	return &hunkPicker{
		title:      "Stage hunks: " + path,
		leftLabel:  "index",
		rightLabel: "working",
		requireAll: false,
		apply: func(m Model, content []byte) (Model, tea.Cmd) {
			m = m.popSurface()
			return m.startOp(engine.StageHunks{Path: path, Content: content})
		},
		doc: doc, blocks: doc.Blocks(), side: hunkpick.Current,
	}
}

func (e *hunkPicker) cur() *hunkpick.Block {
	if e.bi < 0 || e.bi >= len(e.blocks) {
		return nil
	}
	return e.blocks[e.bi]
}

func (e *hunkPicker) sideLen() int {
	b := e.cur()
	if b == nil {
		return 0
	}
	if e.side == hunkpick.Incoming {
		return len(b.Incoming)
	}
	return len(b.Current)
}

func (e *hunkPicker) clampLine() {
	n := e.sideLen()
	if e.line >= n {
		e.line = n - 1
	}
	if e.line < 0 {
		e.line = 0
	}
}

func (e *hunkPicker) focusFirstUndecided() {
	for i, b := range e.blocks {
		if b.Mode == hunkpick.Undecided {
			e.bi, e.line, e.side = i, 0, hunkpick.Current
			return
		}
	}
}

func (e *hunkPicker) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
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
		if e.requireAll {
			if n := e.doc.Pending(); n > 0 {
				m.statusMsg = fmt.Sprintf("%d region(s) left to resolve", n)
				e.focusFirstUndecided()
				return m, nil
			}
		}
		out, ok := e.doc.Resolved()
		if !ok {
			m.statusMsg = "internal error: undecided regions"
			return m, nil
		}
		return e.apply(m, out)
	}
	return m, nil
}

// badge labels a block's current decision using the picker's side labels.
func (e *hunkPicker) badge(b *hunkpick.Block) string {
	switch b.Mode {
	case hunkpick.TakeCurrent:
		return "✓ " + e.leftLabel
	case hunkpick.TakeIncoming:
		return "✓ " + e.rightLabel
	case hunkpick.LineByLine:
		return "line-by-line"
	default:
		return "· undecided"
	}
}

func (e *hunkPicker) render(m Model) string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	var b strings.Builder
	if e.requireAll {
		fmt.Fprintf(&b, "%s    %d regions · %d left\n", e.title, len(e.blocks), e.doc.Pending())
	} else {
		fmt.Fprintf(&b, "%s    %d hunks\n", e.title, len(e.blocks))
	}
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
		header := fmt.Sprintf("%shunk %d/%d — %s", marker, blockNo+1, len(e.blocks), e.badge(blk))
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
	fmt.Fprintf(&b, "\n[←/→] side  [↑/↓] line  [space] pick line  [c] %s  [i] %s  [C/I] all  [n/p] hunk  [enter] apply  [esc] cancel",
		e.leftLabel, e.rightLabel)
	return b.String()
}

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

- [ ] **Step 2: Update the existing conflict tests' type assertions**

In `internal/tui/conflict_picker_test.go`, change the two `*conflictPicker` type assertions to `*hunkPicker`:
- `TestConflictFileLoadedPushesPicker`: `if _, ok := m.stackTop().(*hunkPicker); !ok {`
- (any other `.(*conflictPicker)` in that file).

The `newConflictPicker(...)` calls and `e.doc`/`e.side` field reads are unchanged (same fields on `hunkPicker`).

- [ ] **Step 3: Add a staging-params surface test**

Append to `internal/tui/conflict_picker_test.go`:

```go
func TestStagePickerNoGateAppliesImmediately(t *testing.T) {
	d := hunkpick.FromDiff([]byte("a\nb\n"), []byte("a\nB\n"))
	d.SetAll(hunkpick.TakeCurrent) // default: nothing staged
	e := newStagePicker("f.txt", d)
	if e.requireAll {
		t.Fatal("staging picker must not gate on Pending")
	}
	// Resolved with the default = the index content (a no-op stage).
	out, ok := d.Resolved()
	if !ok || string(out) != "a\nb\n" {
		t.Fatalf("default staging resolve = %q ok=%v, want the index", out, ok)
	}
	// Take the working side of the one hunk → staging it reproduces the work tree.
	e.doc.Blocks()[0].Mode = hunkpick.TakeIncoming
	out, _ = e.doc.Resolved()
	if string(out) != "a\nB\n" {
		t.Fatalf("staged = %q, want the working tree", out)
	}
}
```

- [ ] **Step 4: Run the tui surface tests**

Run: `go test ./internal/tui/ -run 'Picker|Conflict' -v`
Expected: PASS (existing conflict picker tests + the new staging test).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/conflict_picker.go internal/tui/conflict_picker_test.go
git commit -m "refactor(tui): generalize conflictPicker into a shared hunkPicker

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: wiring — `H` opens the staging picker; rename the worktree read

**Files:**
- Modify: `internal/domain/query.go` (rename `ConflictedFile` → `WorktreeFile`)
- Modify: `internal/tui/op.go` (rename the conflict caller; add `loadStageHunksCmd` + msg)
- Modify: `internal/tui/model.go` (handle `stageHunksLoadedMsg`; `H` dispatch)
- Modify: `internal/tui/avail.go` (`canStageHunks` predicate)
- Modify: `internal/tui/footer.go`, `internal/tui/help.go`
- Test: `internal/tui/conflict_picker_test.go` (append a wiring test)

- [ ] **Step 1: Rename the domain read**

In `internal/domain/query.go`, rename `ConflictedFile` → `WorktreeFile` (keep the body and cache key; update the doc comment to "working-tree bytes of a path"):

```go
// WorktreeFile reads the working-tree bytes of a path under a Read reservation.
// Backs the conflict hunk picker (marker text) and hunk staging (the new side).
func (s *Service) WorktreeFile(ctx context.Context, path string) ([]byte, error) {
	return query(ctx, s, "worktree-file:"+path, func(c context.Context) ([]byte, error) {
		return s.repo.ReadWorktreeFile(c, path)
	})
}
```

In `internal/tui/op.go`, update the one caller inside `loadConflictFileCmd`: `svc.ConflictedFile(...)` → `svc.WorktreeFile(...)`.

- [ ] **Step 2: Add the staging load cmd + msg**

In `internal/tui/op.go`, after `loadConflictFileCmd`:

```go
// stageHunksLoadedMsg carries the two sides for the staging picker.
type stageHunksLoadedMsg struct {
	path        string
	index, work []byte
	err         error
}

// loadStageHunksCmd reads the index blob and the working-tree bytes off the UI
// thread; the resulting msg builds the diff and pushes the staging picker.
func (m Model) loadStageHunksCmd(path string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		idx, err := svc.ShowFile(context.Background(), "", path)
		if err != nil {
			return stageHunksLoadedMsg{path: path, err: err}
		}
		work, werr := svc.WorktreeFile(context.Background(), path)
		if werr != nil {
			return stageHunksLoadedMsg{path: path, err: werr}
		}
		return stageHunksLoadedMsg{path: path, index: idx, work: work}
	}
}
```

- [ ] **Step 3: Handle the msg in `model.go`**

In `internal/tui/model.go`'s message `switch`, after the `conflictFileLoadedMsg` case (the imports already include `hunkpick` and `textdiff` from the conflict picker):

```go
	case stageHunksLoadedMsg:
		if msg.err != nil {
			m.statusMsg = "stage hunks: " + msg.err.Error()
			return m, nil
		}
		if textdiff.IsBinary(msg.index) || textdiff.IsBinary(msg.work) {
			m.statusMsg = "stage hunks: binary file"
			return m, nil
		}
		doc := hunkpick.FromDiff(msg.index, msg.work)
		doc.SetAll(hunkpick.TakeCurrent) // default: nothing staged
		if len(doc.Blocks()) == 0 {
			m.statusMsg = "stage hunks: nothing to stage"
			return m, nil
		}
		m = m.pushSurface(newStagePicker(msg.path, doc))
		return m, nil
```

- [ ] **Step 4: `H` dispatch + predicate**

In `internal/tui/avail.go`, add:

```go
// canStageHunks reports whether the Files panel's selected row is a tracked,
// non-conflicted file the hunk-staging picker can open.
func (m Model) canStageHunks() bool {
	if m.focus != panelFiles || !m.opsIdle() {
		return false
	}
	bi, ok := m.backingIndex(panelFiles)
	if !ok {
		return false
	}
	f := m.status.Files[bi]
	return f.Kind != model.KindUntracked && f.Kind != model.KindUnmerged
}
```

In `internal/tui/model.go`'s `tea.KeyMsg` switch, add a case (next to the other Files-panel keys):

```go
		case "H":
			if m.canStageHunks() {
				bi, _ := m.backingIndex(panelFiles)
				return m, m.loadStageHunksCmd(m.status.Files[bi].Path)
			}
```

- [ ] **Step 5: Footer + help**

In `internal/tui/footer.go`'s `contextBindings`, after the `"stage"` entry:

```go
	{"stage-hunks", "H", "[H] hunks", func(m Model) bool { return m.canStageHunks() }},
```

In `internal/tui/help.go`, add to the **Files panel** section:

```go
		r("H", "stage hunks: open the region/line staging picker"),
```

and generalize the existing "Conflict hunk picker" help heading to cover both (rename the heading to `"Hunk picker (conflict resolve / H stage)"`; the key rows are identical for both).

- [ ] **Step 6: Append the wiring test**

Append to `internal/tui/conflict_picker_test.go`:

```go
func TestStageHunksLoadedPushesPicker(t *testing.T) {
	m := Model{width: 80, height: 24}
	updated, _ := m.Update(stageHunksLoadedMsg{path: "f.txt", index: []byte("a\nb\n"), work: []byte("a\nB\n")})
	m = updated.(Model)
	if _, ok := m.stackTop().(*hunkPicker); !ok {
		t.Fatal("stageHunksLoadedMsg should push the hunk picker")
	}
}

func TestStageHunksLoadedNoChangeNoOp(t *testing.T) {
	m := Model{width: 80, height: 24}
	updated, _ := m.Update(stageHunksLoadedMsg{path: "f.txt", index: []byte("a\n"), work: []byte("a\n")})
	m = updated.(Model)
	if m.stackTop() != nil {
		t.Fatal("no changes → no surface")
	}
}
```

- [ ] **Step 7: Run the tui package**

Run: `go test ./internal/tui/`
Expected: PASS, including `TestHelpFooterCoverage` (the `H` footer key now has a help row).

- [ ] **Step 8: Commit**

```bash
git add internal/domain/query.go internal/tui/op.go internal/tui/model.go internal/tui/avail.go internal/tui/footer.go internal/tui/help.go internal/tui/conflict_picker_test.go
git commit -m "feat(tui): H on the Files panel opens the hunk-staging picker

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: docs

**Files:**
- Modify: `README.md`, `CHANGELOG.md`

- [ ] **Step 1: README** — in the TUI key table, on the Files-panel description add: `H` opens the region/line **staging picker** (`←/→` side index↔working, `↑/↓` line, `space` pick line, `c`/`i` whole hunk, `C`/`I` all, `n`/`p` jump, `enter` apply, `esc` cancel); `space` stays whole-file stage.

- [ ] **Step 2: CHANGELOG** — under `## [Unreleased]` → `### Added`:

```markdown
- TUI: **hunk/line staging** — press `H` on a file in the Files panel to open a
  GitKraken-style staging picker (the same surface as the conflict resolver).
  Per hunk: stage the whole working-tree side (`i`), keep the index side (`c`),
  or `space` to stage individual lines (in pick order); `C`/`I` apply to all
  hunks; `enter` stages the selection — the working tree is never modified (only
  the index). `space` still stages the whole file.
```

- [ ] **Step 3: Commit**

```bash
git add README.md CHANGELOG.md
git commit -m "docs: hunk/line staging in README/CHANGELOG

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Final verification (after all tasks)

- [ ] `./test.sh race` — vet+gofmt clean, all unit + e2e green.
- [ ] Manual smoke (REQUIRED — no e2e covers the TUI path): build, modify a tracked file in two places, `H`, stage one hunk, `enter`; confirm `git diff --cached` shows only that hunk, `git diff` shows the rest, the file on disk is unchanged, and the Files/Staged panels refresh.
- [ ] `superpowers:finishing-a-development-branch`.
- [ ] After merge, RE-RUN `./test.sh race` on merged `main`.

---

## Self-Review

**1. Spec coverage:**
- Shared `hunkPicker` (inject title/labels/requireAll/apply) → Task 3. ✓
- `hunkpick.FromDiff` (textdiff split; staging defaults TakeCurrent) → Task 1 + Task 4 Step 3. ✓
- Index-set op (`hash-object -w` + `update-index --cacheinfo`, working tree untouched, mode preserved) → Task 2. ✓
- Read sides (index `ShowFile("",path)`, working `WorktreeFile`) + rename → Task 4 Steps 1–2. ✓
- `H` entry, tracked/modified/non-binary guards, status refresh via op reload → Task 4. ✓
- Help + footer (drift guard) → Task 4 Step 5. ✓
- Docs → Task 5. ✓

**2. Placeholder scan:** complete code throughout. The one deliberate note is the two typo'd error strings in `indexMode` flagged for correction in Task 2 Step 3 — fix them on the way in.

**3. Type consistency:** `hunkpick.FromDiff(left, right []byte) *Doc` consistent (Task 1, Task 4). `engine.StageHunks{Path, Content}` + `git.StageBlob(ctx, path, content)` + `GitOps.StageBlob` consistent (Task 2, Task 3 apply). `hunkPicker` type + `newConflictPicker`/`newStagePicker` consistent (Task 3, Task 4). `stageHunksLoadedMsg{path,index,work,err}` + `loadStageHunksCmd` consistent (Task 4 Steps 2–3). `domain.WorktreeFile` consistent (Task 4 Steps 1–2, and `loadStageHunksCmd`). `canStageHunks` consistent (Task 4 Steps 4–5).

**Out of scope (v1):** unstaging hunks from the Staged panel; untracked partial staging; CRLF normalization; CLI/MCP hunk staging.
