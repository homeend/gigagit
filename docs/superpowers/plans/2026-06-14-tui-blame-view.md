# TUI Blame View (`b`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a file **Blame** view (`b` on a file → single full-width pane, each line gutter-tagged with the commit that last touched it, consecutive same-commit lines collapsed into one block), built as the **second consumer** of the existing TUI view-stack, cross-linked with the History view.

**Architecture:** A new `git blame --porcelain` verb feeds a `Service.Blame` query (Read reservation, coalesced). A `blameView` `surface` rides the existing `viewStack` (push/pop, render/key/mouse already routed). Pure helpers `groupBlame` (collapse runs) and `blameAge` (compact relative time) keep the view testable. `enter` on a block pushes the existing `historyView` at that commit; `esc`/`b` pops; History gains a `b` that pushes blame at its selected commit — closing the cross-link cycle.

**Tech Stack:** Go 1.26, Bubble Tea (Elm value-receiver `Model`), lipgloss, system `git` via `gitcmd`/`gitexec`, all git access through `domain.Service`.

**Spec:** `docs/superpowers/specs/2026-06-13-tui-blame-view-design.md`.

---

## File structure

| File | New? | Responsibility |
|---|---|---|
| `internal/model/blame.go` | new | `BlameLine` data type (one source line + its last-touching commit). |
| `internal/git/blame.go` | new | `ParseBlamePorcelain` parser + `(*Repo).Blame` verb. |
| `internal/git/blame_test.go` | new | Parser (header-once/repeat/uncommitted/rename) + verb (real repo) tests. |
| `internal/domain/query.go` | modify | `Service.Blame` Read-reservation query. |
| `internal/domain/query_test.go` | modify | `Service.Blame` gated-query test (FakeRunner). |
| `internal/tui/blame_view.go` | new | `blameView` surface, `blameBlock`, `groupBlame`, `blameAge`, load cmd, render, update. |
| `internal/tui/blame_view_test.go` | new | grouping/age/render/update + entry-point tests. |
| `internal/tui/model.go` | modify | `blameMsg` case; extend `case "b"` for the Status panel. |
| `internal/tui/files_view.go` | modify | `case "b"` on a tree row; files-view hint gains `[b] blame`. |
| `internal/tui/diff_view.go` | modify | `case "b"` in `updateDiffViewKey`. |
| `internal/tui/diff_render.go` | modify | `diffHintFor` adds `[b] blame` (compacted to survive width-100 truncation). |
| `internal/tui/history_view.go` | modify | `case "b"` → push blame at the selected commit. |
| `internal/tui/diff_view_test.go` | modify | drop `"b"` from the swallowed-keys list (now a real action). |
| `internal/tui/files_view_test.go` | modify | drop `"b"` from the swallowed-keys list. |
| `internal/tui/help.go` | modify | `b` rows + "Blame view (b)" section. |
| `CHANGELOG.md`, `README.md` | modify | user-facing surface note. |

**Reused without modification:** `surface`/`viewStack`/`pushSurface`/`popSurface`/`stackTop` (`stack.go`), the render/key/mouse stack hooks (`view.go`/`model.go`/`mouse.go` — the mouse arm already swallows while the stack is non-empty), `navContext` and `newHistoryView`/`loadHistoryListCmd` (`history_view.go`), and the render helpers `truncate`/`padRight`/`windowRows`/`selectedRow`/`shortHash`/`overlayDims`/`clipToHeight`.

---

## Task 1: `BlameLine` model type

**Files:**
- Create: `internal/model/blame.go`

- [ ] **Step 1: Write the type**

```go
package model

// BlameLine is one source line annotated with the commit that last changed it.
// Hash "" means the line is not yet committed (a working-tree change); git
// reports such lines under the all-zero sha, which the parser normalises to "".
type BlameLine struct {
	Hash    string // full commit sha; "" for not-yet-committed
	Author  string // author name
	Time    int64  // author-time, unix epoch seconds
	Summary string // commit subject (first line)
	LineNo  int    // final line number, 1-based
	Content string // the source line text (no trailing newline)
}
```

- [ ] **Step 2: Build to verify it compiles**

Run: `go build ./internal/model`
Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add internal/model/blame.go
git commit -m "model: add BlameLine type for file blame"
```

---

## Task 2: `ParseBlamePorcelain` parser (TDD)

**Files:**
- Create: `internal/git/blame.go`
- Test: `internal/git/blame_test.go`

- [ ] **Step 1: Write the failing test**

`internal/git/blame_test.go`:

```go
package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseBlamePorcelain(t *testing.T) {
	// Full header on a commit's first appearance; abbreviated (sha + line nums)
	// on repeats; a renamed commit carries previous/filename (ignored cleanly);
	// the all-zero sha is an uncommitted working-tree line.
	data := "" +
		"1111111111111111111111111111111111111111 1 1 2\n" +
		"author Ada\n" +
		"author-mail <a@x>\n" +
		"author-time 1700000000\n" +
		"author-tz +0000\n" +
		"committer Ada\n" +
		"committer-time 1700000000\n" +
		"committer-tz +0000\n" +
		"summary first commit\n" +
		"filename a.go\n" +
		"\tpackage main\n" +
		"1111111111111111111111111111111111111111 2 2\n" +
		"\tfunc main() {}\n" +
		"2222222222222222222222222222222222222222 3 3 1\n" +
		"author Bob\n" +
		"author-time 1690000000\n" +
		"author-tz +0000\n" +
		"summary second\n" +
		"previous 9999999999999999999999999999999999999999 old.go\n" +
		"filename a.go\n" +
		"\tnew line\n" +
		"0000000000000000000000000000000000000000 4 4 1\n" +
		"author Not Committed Yet\n" +
		"author-time 1680000000\n" +
		"author-tz +0000\n" +
		"summary Version of a.go from a.go\n" +
		"filename a.go\n" +
		"\tdirty line\n"

	got := ParseBlamePorcelain([]byte(data))
	if len(got) != 4 {
		t.Fatalf("want 4 blame lines, got %d: %+v", len(got), got)
	}
	if got[0].Hash != "1111111111111111111111111111111111111111" ||
		got[0].Author != "Ada" || got[0].Time != 1700000000 ||
		got[0].Summary != "first commit" || got[0].LineNo != 1 ||
		got[0].Content != "package main" {
		t.Errorf("line 0 wrong: %+v", got[0])
	}
	// Repeat of sha1: metadata reused from the first appearance.
	if got[1].Hash != "1111111111111111111111111111111111111111" ||
		got[1].Author != "Ada" || got[1].LineNo != 2 || got[1].Content != "func main() {}" {
		t.Errorf("line 1 (abbreviated repeat) wrong: %+v", got[1])
	}
	if got[2].Hash != "2222222222222222222222222222222222222222" ||
		got[2].Author != "Bob" || got[2].Content != "new line" || got[2].LineNo != 3 {
		t.Errorf("line 2 (renamed commit) wrong: %+v", got[2])
	}
	// Uncommitted: zero sha normalised to "".
	if got[3].Hash != "" || got[3].Author != "Not Committed Yet" ||
		got[3].Content != "dirty line" || got[3].LineNo != 4 {
		t.Errorf("line 3 (uncommitted) wrong: %+v", got[3])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git -run TestParseBlamePorcelain -v`
Expected: FAIL — `undefined: ParseBlamePorcelain`.

- [ ] **Step 3: Write the parser**

`internal/git/blame.go`:

```go
package git

import (
	"context"
	"strconv"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
	"github.com/gigagit/gg/internal/model"
)

const zeroSha = "0000000000000000000000000000000000000000"

// ParseBlamePorcelain parses `git blame --porcelain` output into one BlameLine
// per source line. Porcelain emits a commit's full header (author, author-time,
// summary, …) only the first time the sha appears and an abbreviated header
// (`<sha> <orig> <final>`) thereafter, so commit metadata is cached by sha and
// reused for the repeats. The all-zero sha (a not-yet-committed line) becomes
// Hash "".
func ParseBlamePorcelain(data []byte) []model.BlameLine {
	type meta struct {
		author  string
		time    int64
		summary string
	}
	cache := map[string]*meta{}
	var out []model.BlameLine
	var cur *model.BlameLine
	curSha := ""

	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		if line[0] == '\t' {
			// Content line closes the current record.
			if cur != nil {
				cur.Content = line[1:]
				if m := cache[curSha]; m != nil {
					cur.Author = m.author
					cur.Time = m.time
					cur.Summary = m.summary
				}
				out = append(out, *cur)
				cur = nil
			}
			continue
		}
		if cur == nil {
			// A header line: "<40-hex sha> <orig> <final> [<num>]".
			f := strings.Fields(line)
			if len(f) >= 3 && len(f[0]) == 40 && isHex(f[0]) {
				sha := f[0]
				if sha == zeroSha {
					sha = ""
				}
				ln := model.BlameLine{Hash: sha}
				if n, err := strconv.Atoi(f[2]); err == nil {
					ln.LineNo = n
				}
				cur = &ln
				curSha = sha
				if _, ok := cache[curSha]; !ok {
					cache[curSha] = &meta{}
				}
			}
			continue
		}
		// Detail lines between a header and its content line populate the cache.
		switch {
		case strings.HasPrefix(line, "author "):
			cache[curSha].author = strings.TrimPrefix(line, "author ")
		case strings.HasPrefix(line, "author-time "):
			if t, err := strconv.ParseInt(strings.TrimPrefix(line, "author-time "), 10, 64); err == nil {
				cache[curSha].time = t
			}
		case strings.HasPrefix(line, "summary "):
			cache[curSha].summary = strings.TrimPrefix(line, "summary ")
		}
	}
	return out
}

func isHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}
```

> The `context`/`gitcmd` imports are unused until Task 3 adds the verb in this
> same file. To keep this step compiling on its own, the verb in Task 3 lands
> before running the full package; if you build `./internal/git` between steps,
> add the verb (Task 3 Step 3) first or temporarily drop the two imports. The
> committed state after Task 3 has both.

- [ ] **Step 4: Run test to verify it passes**

Add the Task-3 verb now (it shares this file and provides the `context`/`gitcmd`
uses) OR run just this test which compiles the package — if the unused-import
error appears, proceed to Task 3 Step 3 first. Once both are present:

Run: `go test ./internal/git -run TestParseBlamePorcelain -v`
Expected: PASS.

> Practical note for the implementer: Tasks 2 and 3 edit the same file and the
> Go compiler rejects unused imports, so implement Task 2's parser and Task 3's
> verb together, then run both tests. Commit them separately (this step's commit
> covers the parser + its test).

- [ ] **Step 5: Commit**

```bash
git add internal/git/blame.go internal/git/blame_test.go
git commit -m "git: ParseBlamePorcelain for blame --porcelain output"
```

---

## Task 3: `(*Repo).Blame` verb (TDD against a real repo)

**Files:**
- Modify: `internal/git/blame.go`
- Test: `internal/git/blame_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/git/blame_test.go` (mirrors `TestFileLog`'s real-repo
setup: `newTestRepo` returns `(dir string, runner gitexec.Runner)`, and
`gitIn(t, dir, …)` runs git in that dir):

```go
func TestBlameVerb(t *testing.T) {
	dir, runner := newTestRepo(t) // repo with an initial commit (README.md)
	repo := &Repo{Runner: runner}

	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "a.go")
	gitIn(t, dir, "commit", "-m", "first")

	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("line one\nline two edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "a.go")
	gitIn(t, dir, "commit", "-m", "second")

	got, err := repo.Blame(context.Background(), "", "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 blame lines, got %d: %+v", len(got), got)
	}
	if got[0].Content != "line one" || got[1].Content != "line two edited" {
		t.Errorf("content wrong: %+v", got)
	}
	if got[0].Hash == "" || got[1].Hash == "" {
		t.Errorf("committed lines should have shas: %+v", got)
	}
	if got[0].Hash == got[1].Hash {
		t.Errorf("the two lines come from different commits, want distinct shas: %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git -run TestBlameVerb -v`
Expected: FAIL — `repo.Blame undefined`.

- [ ] **Step 3: Implement the verb**

Append to `internal/git/blame.go`:

```go
// Blame returns one BlameLine per line of path as of rev (rev "" = working
// tree). One invocation. Blame is whole-file; the caller bounds nothing.
func (r *Repo) Blame(ctx context.Context, rev, path string) ([]model.BlameLine, error) {
	b := gitcmd.New("blame").
		Arg("--porcelain").
		ArgIf(rev != "", rev).
		Arg("--", path)
	res, err := r.Runner.Run(ctx, "git blame", b.ToArgv())
	if err != nil {
		return nil, err
	}
	return ParseBlamePorcelain([]byte(res.Stdout)), nil
}
```

> `gitcmd.New("blame")` → the subcommand; `ArgIf(cond, args...)` appends only
> when `cond` (see `internal/git/file_log.go` for the same idiom). The run label
> `"git blame"` is what `FakeRunner.SetResponse("git blame", …)` matches in
> Task 4.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/git -run 'TestParseBlamePorcelain|TestBlameVerb' -v`
Expected: PASS (both).

- [ ] **Step 5: Run the whole git package**

Run: `go test ./internal/git`
Expected: ok.

- [ ] **Step 6: Commit**

```bash
git add internal/git/blame.go internal/git/blame_test.go
git commit -m "git: Blame verb — per-line porcelain blame"
```

---

## Task 4: `Service.Blame` query (TDD)

**Files:**
- Modify: `internal/domain/query.go`
- Test: `internal/domain/query_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/domain/query_test.go` (mirrors `TestShowFileGatedQuery`:
`New(&git.Repo{Runner: f})` with a `FakeRunner` whose response is matched by the
run label prefix `"git blame"`):

```go
func TestBlameGatedQuery(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git blame", gitexec.Result{Stdout: "" +
		"1111111111111111111111111111111111111111 1 1 1\n" +
		"author Ada\n" +
		"author-time 1700000000\n" +
		"summary only\n" +
		"filename a.go\n" +
		"\thello\n"})
	svc := New(&git.Repo{Runner: f})
	got, err := svc.Blame(context.Background(), "", "a.go")
	if err != nil {
		t.Fatalf("Blame error: %v", err)
	}
	if len(got) != 1 || got[0].Content != "hello" || got[0].Author != "Ada" {
		t.Fatalf("Blame = %+v", got)
	}
}
```

> Confirm the imports `gitexec` and `git` are already present in
> `query_test.go` (they are — `TestShowFileGatedQuery` uses both).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain -run TestBlameGatedQuery -v`
Expected: FAIL — `svc.Blame undefined`.

- [ ] **Step 3: Add the query**

In `internal/domain/query.go`, after the `FileLog` method (around line 186), add:

```go
// Blame returns per-line blame for path at rev under a Read reservation,
// coalesced per (rev, path).
func (s *Service) Blame(ctx context.Context, rev, path string) ([]model.BlameLine, error) {
	return query(ctx, s, "blame:"+rev+":"+path, func(ctx context.Context) ([]model.BlameLine, error) {
		return s.repo.Blame(ctx, rev, path)
	})
}
```

> No engine `GitOps`-interface change: `Service.repo` is a concrete `*git.Repo`
> (the `GitOps` interface in `internal/engine` covers write operations only).
> `model` is already imported in `query.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/domain -run TestBlameGatedQuery -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/query.go internal/domain/query_test.go
git commit -m "domain: Service.Blame gated query"
```

---

## Task 5: `blameView` surface — struct, grouping, age, loader (TDD for the pure helpers)

**Files:**
- Create: `internal/tui/blame_view.go`
- Test: `internal/tui/blame_view_test.go`
- Modify: `internal/tui/model.go` (handle `blameMsg`)

- [ ] **Step 1: Write the failing test for the pure helpers**

`internal/tui/blame_view_test.go`:

```go
package tui

import (
	"testing"
	"time"

	"github.com/gigagit/gg/internal/model"
)

func TestGroupBlameCollapsesRuns(t *testing.T) {
	lines := []model.BlameLine{
		{Hash: "aaa"}, {Hash: "aaa"}, {Hash: "bbb"}, {Hash: "aaa"},
	}
	blocks := groupBlame(lines)
	if len(blocks) != 3 {
		t.Fatalf("want 3 blocks (aaa,aaa | bbb | aaa), got %d: %+v", len(blocks), blocks)
	}
	if blocks[0].start != 0 || blocks[0].end != 1 || blocks[0].hash != "aaa" {
		t.Errorf("block 0 wrong: %+v", blocks[0])
	}
	if blocks[1].start != 2 || blocks[1].end != 2 || blocks[1].hash != "bbb" {
		t.Errorf("block 1 wrong: %+v", blocks[1])
	}
	if blocks[2].start != 3 || blocks[2].end != 3 {
		t.Errorf("block 2 wrong: %+v", blocks[2])
	}
}

func TestGroupBlameEdges(t *testing.T) {
	if got := groupBlame(nil); len(got) != 0 {
		t.Errorf("empty input → no blocks, got %+v", got)
	}
	all := groupBlame([]model.BlameLine{{Hash: "x"}, {Hash: "x"}, {Hash: "x"}})
	if len(all) != 1 || all[0].start != 0 || all[0].end != 2 {
		t.Errorf("all-same → one block, got %+v", all)
	}
}

func TestBlockAt(t *testing.T) {
	blocks := []blameBlock{{start: 0, end: 1, hash: "aaa"}, {start: 2, end: 2, hash: "bbb"}}
	if b, ok := blockAt(blocks, 1); !ok || b.hash != "aaa" {
		t.Errorf("line 1 should be in block aaa, got %+v ok=%v", b, ok)
	}
	if b, ok := blockAt(blocks, 2); !ok || b.hash != "bbb" {
		t.Errorf("line 2 should be in block bbb, got %+v ok=%v", b, ok)
	}
	if _, ok := blockAt(blocks, 9); ok {
		t.Error("out-of-range line should not match a block")
	}
}

func TestBlameAge(t *testing.T) {
	now := time.Unix(1_000_000_000, 0)
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{30 * time.Second, "now"},
		{5 * time.Minute, "5m"},
		{3 * time.Hour, "3h"},
		{2 * 24 * time.Hour, "2d"},
		{90 * 24 * time.Hour, "3mo"},
		{800 * 24 * time.Hour, "2y"},
	}
	for _, c := range cases {
		if got := blameAge(now, now.Add(-c.ago)); got != c.want {
			t.Errorf("blameAge(-%s) = %q, want %q", c.ago, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui -run 'TestGroupBlame|TestBlockAt|TestBlameAge' -v`
Expected: FAIL — undefined `groupBlame`/`blameBlock`/`blockAt`/`blameAge`.

- [ ] **Step 3: Write the surface struct, helpers, loader, and stubs**

`internal/tui/blame_view.go`:

```go
package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

// blameView is the single-file blame surface: the file's content with a
// per-line gutter naming the commit that last touched it. It reuses navContext
// (defined in history_view.go); rev "" blames HEAD's working content.
type blameView struct {
	ctx     navContext
	lines   []model.BlameLine
	blocks  []blameBlock // grouped runs, recomputed after each load
	sel     int          // line cursor (index into lines)
	loading bool
	err     error
	tag     string // gates stale loads
}

// blameBlock is a maximal run of consecutive lines sharing a commit. hash ""
// means the run is uncommitted.
type blameBlock struct {
	start, end int
	hash       string
	author     string
	time       int64
}

func newBlameView(ctx navContext) *blameView {
	return &blameView{ctx: ctx, loading: true, tag: "blame:" + ctx.rev + ":" + ctx.path}
}

// groupBlame collapses maximal runs of lines sharing a Hash into blocks.
func groupBlame(lines []model.BlameLine) []blameBlock {
	var blocks []blameBlock
	for i, ln := range lines {
		if i == 0 || lines[i-1].Hash != ln.Hash {
			blocks = append(blocks, blameBlock{start: i, end: i, hash: ln.Hash, author: ln.Author, time: ln.Time})
		} else {
			blocks[len(blocks)-1].end = i
		}
	}
	return blocks
}

// blockAt returns the block containing line, if any.
func blockAt(blocks []blameBlock, line int) (blameBlock, bool) {
	for _, b := range blocks {
		if line >= b.start && line <= b.end {
			return b, true
		}
	}
	return blameBlock{}, false
}

// blameAge is a compact relative age (now/5m/3h/2d/3mo/2y) for the gutter.
// ageString in repo_popup.go caps at days and is too wide for a fixed gutter.
func blameAge(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 365*24*time.Hour:
		days := int(d.Hours() / 24)
		if days < 30 {
			return fmt.Sprintf("%dd", days)
		}
		return fmt.Sprintf("%dmo", days/30)
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/24/365))
	}
}

// blameMsg carries the async blame result, tag-gated like historyListMsg.
type blameMsg struct {
	tag   string
	lines []model.BlameLine
	err   error
}

// loadBlameCmd fetches blame off the UI thread.
func (m Model) loadBlameCmd(ctx navContext, tag string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ls, err := svc.Blame(context.Background(), ctx.rev, ctx.path)
		return blameMsg{tag: tag, lines: ls, err: err}
	}
}

// render/update implemented in Task 6 — stubs so *blameView satisfies surface.
func (b *blameView) render(m Model) string                          { return "" }
func (b *blameView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) { return m, nil }
```

- [ ] **Step 4: Handle `blameMsg` in `Update`**

In `internal/tui/model.go`, in `Update`'s message `switch`, right after the
`case historyDiffMsg:` arm (around line 184), add:

```go
	case blameMsg:
		if b, ok := m.stackTop().(*blameView); ok && b.tag == msg.tag {
			b.loading = false
			b.err = msg.err
			b.lines = msg.lines
			b.blocks = groupBlame(msg.lines)
			b.sel = 0
		}
		return m, nil
```

> `m.stackTop().(*blameView)` is nil-safe: a nil or non-blame top fails the
> assertion and `ok` is false.

- [ ] **Step 5: Run tests + build**

Run: `go test ./internal/tui -run 'TestGroupBlame|TestBlockAt|TestBlameAge' -v && go build ./internal/tui`
Expected: PASS; build succeeds.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/blame_view.go internal/tui/blame_view_test.go internal/tui/model.go
git commit -m "tui: blameView scaffolding — grouping, age, async load"
```

---

## Task 6: `blameView` render + key handling (TDD)

**Files:**
- Modify: `internal/tui/blame_view.go`
- Test: `internal/tui/blame_view_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/blame_view_test.go`:

```go
func blameFixture() *blameView {
	b := &blameView{
		ctx: navContext{path: "a.go", rev: ""},
		lines: []model.BlameLine{
			{Hash: "aaaaaaa", Author: "Ada", Time: 1, LineNo: 1, Content: "package main"},
			{Hash: "aaaaaaa", Author: "Ada", Time: 1, LineNo: 2, Content: "func main() {}"},
			{Hash: "", Author: "Not Committed Yet", LineNo: 3, Content: "dirty"},
		},
	}
	b.blocks = groupBlame(b.lines)
	return b
}

func TestBlameRenderGutterFirstLineOnly(t *testing.T) {
	m := Model{width: 100, height: 30}
	b := blameFixture()
	out := b.render(m)
	if !contains(out, "package main") || !contains(out, "func main()") {
		t.Errorf("blame render missing source lines:\n%s", out)
	}
	if !contains(out, "a.go") {
		t.Errorf("blame header missing path:\n%s", out)
	}
	if !contains(out, "Ada") {
		t.Errorf("gutter missing author on the block's first line:\n%s", out)
	}
	if !contains(out, "uncommitted") {
		t.Errorf("uncommitted block should show an (uncommitted) gutter:\n%s", out)
	}
}

func TestBlameDownMovesCursorClamped(t *testing.T) {
	m := Model{width: 100, height: 30}
	b := blameFixture()
	m = m.pushSurface(b)
	for i := 0; i < 5; i++ {
		m, _ = b.update(m, keyMsg("j"))
	}
	if b.sel != len(b.lines)-1 {
		t.Fatalf("j should clamp at the last line %d, got %d", len(b.lines)-1, b.sel)
	}
}

func TestBlameEnterOnCommitPushesHistory(t *testing.T) {
	m := Model{width: 100, height: 30}
	b := blameFixture()
	m = m.pushSurface(b)
	b.sel = 0 // a committed block (aaaaaaa)
	m, cmd := b.update(m, keyMsg("enter"))
	h, ok := m.stackTop().(*historyView)
	if !ok {
		t.Fatal("enter on a committed block should push a historyView")
	}
	if h.ctx.path != "a.go" || h.ctx.rev != "aaaaaaa" {
		t.Errorf("history navContext wrong: %+v", h.ctx)
	}
	if cmd == nil {
		t.Error("pushing history should fire its list-load cmd")
	}
}

func TestBlameEnterOnUncommittedIsNoop(t *testing.T) {
	m := Model{width: 100, height: 30}
	b := blameFixture()
	m = m.pushSurface(b)
	b.sel = 2 // the uncommitted block
	m, _ = b.update(m, keyMsg("enter"))
	if _, ok := m.stackTop().(*blameView); !ok {
		t.Fatal("enter on an uncommitted block should be a no-op (stay on blame)")
	}
}

func TestBlameEscAndBPop(t *testing.T) {
	for _, key := range []string{"esc", "b"} {
		m := Model{width: 100, height: 30}
		b := blameFixture()
		m = m.pushSurface(b)
		m, _ = b.update(m, keyMsg(key))
		if m.stackTop() != nil {
			t.Fatalf("%q should pop the blame surface", key)
		}
	}
}
```

> **Imports:** these tests reference no `tea.` symbol directly (they drive input
> through the `keyMsg(...)` helper), so keep the Task-5 import block exactly as
> it is — `testing`, `time`, `model`. Do **not** add a `tea` import; it would be
> unused and break the build. `keyMsg(s string) tea.KeyMsg` (in `model_test.go`)
> and `contains(s, sub string) bool` (in `worktree_popup_test.go`) are existing
> package-internal test helpers — use them as-is.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui -run TestBlame -v`
Expected: FAIL (stub `render` returns `""`, stub `update` no-ops).

- [ ] **Step 3: Implement render + update (replace the Task-5 stubs)**

In `internal/tui/blame_view.go`, replace the two stub lines with:

```go
// blameGutterW is the fixed gutter width: shortHash(7) + space + author(12) +
// space + age(≤6), padded.
const blameGutterW = 28

func (m Model) blameBodyRows() int {
	_, h := m.overlayDims()
	n := h - 2
	if n < 1 {
		n = 1
	}
	return n
}

func (b *blameView) render(m Model) string {
	w, scrH := m.overlayDims()
	body := m.blameBodyRows()

	header := truncate("blame: "+b.ctx.path+revSuffix(b.ctx.rev), w)
	hint := truncate("[↑↓] line  [enter] history  [esc/b] back  [q] quit", w)

	gw := blameGutterW
	if gw > w-10 {
		gw = w - 10
	}
	if gw < 0 {
		gw = 0
	}
	codeW := w - gw - 1
	if codeW < 1 {
		codeW = 1
	}

	now := time.Now()
	rows := make([]string, len(b.lines))
	for i, ln := range b.lines {
		gutter := padRight("", gw)
		if i == 0 || b.lines[i-1].Hash != ln.Hash {
			gutter = padRight(truncate(blameGutterText(ln, now), gw), gw)
		}
		row := gutter + "│" + truncate(ln.Content, codeW)
		if i == b.sel {
			row = selectedRow.Render(padRight(truncate(row, w), w))
		}
		rows[i] = row
	}

	win, _, _ := windowRows(rows, body, b.sel)
	switch {
	case b.loading:
		win = []string{"  (loading…)"}
	case b.err != nil:
		win = []string{truncate("  error: "+b.err.Error(), w)}
	case len(b.lines) == 0:
		win = []string{"  (empty)"}
	}
	for len(win) < body {
		win = append(win, "")
	}

	out := header + "\n" + strings.Join(win[:body], "\n") + "\n" + hint
	return clipToHeight(out, scrH)
}

// revSuffix annotates the header with the blamed revision, if any.
func revSuffix(rev string) string {
	if rev == "" {
		return ""
	}
	return " @ " + shortHash(rev)
}

// blameGutterText is the per-block remark: hash, author (≤12), compact age.
func blameGutterText(ln model.BlameLine, now time.Time) string {
	if ln.Hash == "" {
		return "(uncommitted)"
	}
	author := padRight(truncate(ln.Author, 12), 12)
	return shortHash(ln.Hash) + " " + author + " " + blameAge(now, time.Unix(ln.Time, 0))
}

func (b *blameView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "esc", "b":
		return m.popSurface(), nil
	case "down", "j":
		if b.sel < len(b.lines)-1 {
			b.sel++
		}
		return m, nil
	case "up", "k":
		if b.sel > 0 {
			b.sel--
		}
		return m, nil
	case "enter":
		blk, ok := blockAt(b.blocks, b.sel)
		if !ok || blk.hash == "" {
			return m, nil
		}
		ctx := navContext{path: b.ctx.path, rev: blk.hash}
		hv := newHistoryView(ctx)
		m = m.pushSurface(hv)
		return m, m.loadHistoryListCmd(ctx, hv.listTag)
	}
	return m, nil
}
```

Add `"strings"` to the `blame_view.go` import block (it now uses
`strings.Join`).

> `truncate`, `padRight`, `windowRows`, `selectedRow`, `shortHash`,
> `overlayDims`, `clipToHeight` already exist in `internal/tui` — do not
> redefine them. `newHistoryView`/`loadHistoryListCmd`/`navContext` come from
> `history_view.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui -run TestBlame -v`
Expected: PASS.

- [ ] **Step 5: gofmt + full package**

Run: `gofmt -l internal/tui/blame_view.go && go test ./internal/tui`
Expected: no gofmt output; package ok.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/blame_view.go internal/tui/blame_view_test.go
git commit -m "tui: blame view render (grouped gutter) + j/k/enter/esc nav"
```

---

## Task 7: Wire the four `b` entry points (TDD)

**Files:**
- Modify: `internal/tui/model.go` (extend `case "b"` for Status), `internal/tui/files_view.go` (tree `b` + hint), `internal/tui/diff_view.go` (diff `b`), `internal/tui/diff_render.go` (hint), `internal/tui/history_view.go` (history `b`)
- Test: `internal/tui/blame_view_test.go`, and update `internal/tui/diff_view_test.go` + `internal/tui/files_view_test.go`

- [ ] **Step 1: Write the failing entry-point tests**

Append to `internal/tui/blame_view_test.go`:

```go
func TestStatusBOpensBlame(t *testing.T) {
	m := Model{width: 100, height: 30, focus: panelStatus, sel: map[panel]int{}}
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "a.go"}}}
	mm, _ := m.Update(keyMsg("b"))
	got := mm.(Model)
	bv, ok := got.stackTop().(*blameView)
	if !ok {
		t.Fatal("b on a Status file should push a blameView")
	}
	if bv.ctx.path != "a.go" || bv.ctx.rev != "" {
		t.Errorf("wrong navContext: %+v", bv.ctx)
	}
}

func TestFilesViewBOpensBlame(t *testing.T) {
	m := Model{width: 100, height: 30}
	m.filesView = &contentPopup{lines: []contentLine{{text: "a.go", path: "a.go"}}}
	m.filesTreeFocused = true
	m.filesHash = "abc123"
	mm, _ := m.updateFilesViewKey(keyMsg("b"))
	bv, ok := mm.(Model).stackTop().(*blameView)
	if !ok {
		t.Fatal("b on a files-view row should push a blameView")
	}
	if bv.ctx.path != "a.go" || bv.ctx.rev != "abc123" {
		t.Errorf("wrong navContext: %+v", bv.ctx)
	}
}

func TestDiffViewBOpensBlame(t *testing.T) {
	m := Model{width: 100, height: 30}
	m.diffView = &diffView{title: "a.go", rev: "abc123"}
	mm, _ := m.updateDiffViewKey(keyMsg("b"))
	bv, ok := mm.(Model).stackTop().(*blameView)
	if !ok {
		t.Fatal("b in the diff view should push a blameView")
	}
	if bv.ctx.path != "a.go" || bv.ctx.rev != "abc123" {
		t.Errorf("wrong navContext: %+v", bv.ctx)
	}
}

func TestHistoryBOpensBlameAtSelected(t *testing.T) {
	m := Model{width: 100, height: 30}
	h := &historyView{
		ctx: navContext{path: "a.go", rev: ""},
		commits: []model.FileCommit{
			{Commit: model.Commit{Hash: "aaaaaaa", Subject: "edit"}, Status: "M", Path: "a.go"},
			{Commit: model.Commit{Hash: "bbbbbbb", Subject: "add"}, Status: "A", Path: "a.go"},
		},
		sel: 1,
	}
	m = m.pushSurface(h)
	m, cmd := h.update(m, keyMsg("b"))
	bv, ok := m.stackTop().(*blameView)
	if !ok {
		t.Fatal("b in history should push a blameView")
	}
	if bv.ctx.path != "a.go" || bv.ctx.rev != "bbbbbbb" {
		t.Errorf("blame should target the selected commit, got %+v", bv.ctx)
	}
	if cmd == nil {
		t.Error("pushing blame should fire its load cmd")
	}
}
```

> Confirm `model.WorkingTreeStatus`/`FileStatus` field names against
> `internal/model/model.go` (the history view's `TestStatusHOpensHistory` uses
> the same shape — copy it). `canShowFileDiff()` must be true for a single
> unfiltered file; if it gates on more, set those fields in the fixture exactly
> as `TestStatusHOpensHistory` does.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui -run 'TestStatusBOpensBlame|TestFilesViewBOpensBlame|TestDiffViewBOpensBlame|TestHistoryBOpensBlameAtSelected' -v`
Expected: FAIL — `b` does nothing on Status / files-view / diff / history yet.

- [ ] **Step 3: Status panel `b` (extend the existing `case "b"`)**

In `internal/tui/model.go`, the existing `case "b":` (around line 335) only
handles the Branches panel. Extend it — **do not add a second `case "b"`** (Go
rejects duplicate cases):

```go
		case "b":
			if m.focus == panelBranches && m.canOpenBranchPopup() {
				if mm, ok := m.openBranchPopup(false); ok {
					return mm, nil
				}
			}
			if m.focus == panelStatus && m.canShowFileDiff() {
				bi, _ := m.backingIndex(panelStatus)
				f := m.status.Files[bi]
				ctx := navContext{path: f.Path, rev: ""}
				bv := newBlameView(ctx)
				m = m.pushSurface(bv)
				return m, m.loadBlameCmd(ctx, bv.tag)
			}
```

- [ ] **Step 4: Files-view tree `b`**

In `internal/tui/files_view.go` `updateFilesViewKey`, add a `case "b":`
immediately after the existing `case "h":` block (same guards):

```go
	case "b":
		if !m.filesTreeFocused {
			return m, nil
		}
		vis := p.visible()
		if p.sel < 0 || p.sel >= len(vis) || vis[p.sel].path == "" {
			return m, nil
		}
		ctx := navContext{path: vis[p.sel].path, rev: m.filesHash}
		bv := newBlameView(ctx)
		m = m.pushSurface(bv)
		return m, m.loadBlameCmd(ctx, bv.tag)
```

- [ ] **Step 5: Diff-view `b`**

In `internal/tui/diff_view.go` `updateDiffViewKey`, add a `case "b":`
immediately after the existing `case "h":` block:

```go
	case "b":
		ctx := navContext{path: v.title, rev: v.rev}
		bv := newBlameView(ctx)
		m = m.pushSurface(bv)
		return m, m.loadBlameCmd(ctx, bv.tag)
```

- [ ] **Step 6: History-view `b` (blame at the selected commit)**

In `internal/tui/history_view.go` `(*historyView).update`, add a `case "b":`
inside the `switch msg.String()` (e.g. after the `case "esc", "h":` arm):

```go
	case "b":
		if h.sel >= 0 && h.sel < len(h.commits) {
			fc := h.commits[h.sel]
			ctx := navContext{path: h.ctx.path, rev: fc.Hash}
			bv := newBlameView(ctx)
			m = m.pushSurface(bv)
			return m, m.loadBlameCmd(ctx, bv.tag)
		}
```

- [ ] **Step 7: Drop `"b"` from the two swallowed-keys tests**

`b` is now a real action in the diff view and files view, so the
"swallows action keys" tests must no longer list it (they assert `cmd == nil`,
which a blame push violates).

In `internal/tui/diff_view_test.go` `TestDiffViewSwallowsActionKeys`, remove
`"b"` from the slice:

```go
	for _, k := range []string{"p", "P", "s", "S", "u", "d", "w", "m", "l", "R", ",", "/", "?", "tab", "enter"} {
```

In `internal/tui/files_view_test.go` `TestFilesViewSwallowsActionKeys`, remove
`"b"`:

```go
	for _, key := range []string{"p", "s", "m", "d", "w", "o", "R", ",", "r", "?"} {
```

- [ ] **Step 8: Diff hint — add `[b] blame` (kept under width 100)**

In `internal/tui/diff_render.go`, the `diffHintFor` return must keep
`[esc] close` visible after truncation at width 100 (`TestRenderDiffViewPanes`).
Compact `partial`→`part` and `history`→`hist` to make room. Replace the return:

```go
	return "[↑↓] scroll  [n/p] change  [f] part  [w] lines:" + mode + pan + "  [h] hist  [b] blame  [esc] close  [q] quit"
```

> Width check (scroll mode, the longest): `[esc] close` ends at column 99 ≤ 100,
> so it survives. No test asserts the `partial`/`history` label text — only
> `[esc] close` presence — so the abbreviations are safe.

- [ ] **Step 9: Files-view hint — add `[b] blame`**

In `internal/tui/files_view.go` `renderFilesView`, extend the hint:

```go
	hint := "[enter] diff  [h] history  [b] blame  [/] search  [esc] close"
```

- [ ] **Step 10: Run the failing tests + the affected packages**

Run: `go test ./internal/tui -run 'TestStatusBOpensBlame|TestFilesViewBOpensBlame|TestDiffViewBOpensBlame|TestHistoryBOpensBlameAtSelected|TestDiffViewSwallowsActionKeys|TestFilesViewSwallowsActionKeys|TestRenderDiffViewPanes' -v && go test ./internal/tui`
Expected: all PASS; package ok.

- [ ] **Step 11: Commit**

```bash
git add internal/tui/model.go internal/tui/files_view.go internal/tui/diff_view.go internal/tui/diff_render.go internal/tui/history_view.go internal/tui/blame_view_test.go internal/tui/diff_view_test.go internal/tui/files_view_test.go
git commit -m "tui: wire b (blame) from Status, files-view, diff, and history"
```

---

## Task 8: Help text + user docs + full verification

**Files:**
- Modify: `internal/tui/help.go`, `CHANGELOG.md`, `README.md`

- [ ] **Step 1: Help content**

In `internal/tui/help.go`, add `b` rows mirroring the existing `h` rows and a
"Blame view (b)" section after the "History view (h)" section:

- In the **Status panel** group (after the `r("h", …)` line):
```go
		r("b", "blame: who last changed each line, grouped by commit"),
```
- In the **Commit files view (l)** group (after its `r("h", …)` line):
```go
		r("b", "blame of the selected file (tree side)"),
```
- In the **Diff view** group (after its `r("h", …)` line):
```go
		r("b", "blame of this file at the shown revision"),
```
- After the existing `h("History view (h)")` block, add:
```go
		h("Blame view (b)"),
		r("↑/k ↓/j", "move the line cursor"),
		r("enter", "file history at the commit under the cursor"),
		r("esc/b", "back"),
		r("q/ctrl+c", "quit"),
```

- [ ] **Step 2: CHANGELOG**

Add under the current unreleased section of `CHANGELOG.md`:

```
- TUI: file **Blame** view — press `b` on a Status file, a files-view row, or
  inside the diff view to see each line tagged with the commit that last changed
  it (consecutive same-commit lines collapse into one gutter remark with author
  and age). `enter` on a block opens that commit's file history; from history,
  `b` blames the selected commit. Second consumer of the view stack.
```

- [ ] **Step 3: README**

Add `b` next to `h` wherever README documents TUI keys (the keybindings
table/list), e.g.:

```
| `b` | Blame the selected file (Status / files-view / diff view) |
```

> Match the README's existing table/list format; if `h` is a list item rather
> than a table row, add `b` in the same style.

- [ ] **Step 4: Full staged verification**

Run: `./test.sh`
Expected: vet+gofmt clean, unit + e2e pass.

- [ ] **Step 5: Race pass before hand-off**

Run: `./test.sh race`
Expected: PASS with `-race`.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/help.go CHANGELOG.md README.md
git commit -m "docs(tui): document the b/blame key (help, changelog, readme)"
```

---

## Follow-ups (out of scope; separate specs/plans)

1. **Reblame-parent walking** — a key to blame the parent of the block's commit in place (deferred; `enter`→history covers drilling for now).
2. **Surface migration** — wrap the base grid, diff, files-view, popups, and modal as stack entries and delete the nil-pointer `if`-chains.
3. **`gg blame` / `gg log <file>` CLI** — the scriptable surface for blame/history.

---

## Self-review

**Spec coverage (against `2026-06-13-tui-blame-view-design.md`):**
- §3.1 `BlameLine` → Task 1.
- §3.2 `ParseBlamePorcelain` (header-once/abbrev, uncommitted zero-sha, rename) → Task 2.
- §3.3 `(*Repo).Blame` verb → Task 3.
- §3.4 `Service.Blame` query → Task 4.
- §4.1–4.3 struct, grouping, async load → Task 5; §4.2 `blameBlock`/`groupBlame`/`blockAt` → Task 5.
- §4.4 render (single pane, gutter-first-line-only, uncommitted, highlight, scroll, hint) → Task 6.
- §4.5 update (j/k clamp, enter→history commit/uncommitted, esc/b pop) → Task 6.
- §5 four entry points (Status / files / diff / history-at-selected), each with a positive push test (`TestStatusBOpensBlame`, `TestFilesViewBOpensBlame`, `TestDiffViewBOpensBlame`, `TestHistoryBOpensBlameAtSelected`); mouse already swallowed → Task 7.
- §6 testing (parser, verb, query, group, age, render, update, entry points) → Tasks 2–7.
- §7 docs (CHANGELOG, README, help, no agentskill bump) → Task 8.
- §2 non-goals (reblame, line-range, ignore-revs, long-line hscroll, mouse, CLI) → none added; reblame & CLI listed as follow-ups.

**Placeholder scan:** No "TBD"/"handle edge cases"/"similar to". Each code step shows full, compilable code (no fake statements in any test block). The `>` notes ask the engineer to confirm an existing helper/field name against the current tree (verification, not missing content) and pin the import block.

**Type consistency:** `blameView{ctx,lines,blocks,sel,loading,err,tag}`, `blameBlock{start,end,hash,author,time}`, `newBlameView`, `groupBlame`, `blockAt`, `blameAge`, `blameGutterText`, `revSuffix`, `blameMsg{tag,lines,err}`, `loadBlameCmd(ctx,tag)`, `blameBodyRows`, `blameGutterW` all used consistently across Tasks 5–8. `model.BlameLine{Hash,Author,Time,Summary,LineNo,Content}` and `ParseBlamePorcelain`/`(*Repo).Blame`/`Service.Blame(ctx,rev,path)` consistent (Tasks 1–4, used in Task 5's loader). `navContext{path,rev}`, `newHistoryView`, `loadHistoryListCmd` reused exactly as defined in `history_view.go`. Entry points all push a `*blameView` and fire `loadBlameCmd` with `bv.tag`.
```
