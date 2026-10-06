# Streaming File History Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. (gigagit: NO implementer subagents — this session executes it.)

**Goal:** The TUI history window (`h`) shows each commit as git finds it, selects the newest at once, and can load 200 more older commits.

**Architecture:** An incremental `git log --follow` parser behind a streaming git verb (`Repo.FileLogStream`), an ungated domain pass-through (`Service.FileLogStream`), and a TUI walk goroutine that batches commits into `historyChunkMsg`s re-armed by the Update handler. Load more re-runs the walk from a pinned start sha with a bigger `-n`, dropping the commits already shown.

**Tech Stack:** Go 1.26, Bubble Tea, system git.

**Spec:** `docs/superpowers/specs/2026-10-06-streaming-file-history-design.md`

## Global Constraints

- One git verb = one invocation, argv via `gitcmd`, run via `r.Runner.Stream`.
- `internal/tui` never imports `internal/git` in non-test code; it reaches git through `domain`.
- Every user-visible TUI string goes through `i18n.T` with a literal key present in ja/ko/zh/ru (`internal/i18n/lang/*.toml`).
- The streaming walk takes NO repogate reservation (user ruling 2026-10-06); the buffered `FileLog` keeps its reservation.
- Page size 200 (`historyPage`); batch flush at 20 commits or 50 ms.
- Web `/api/filelog` and the CLI are unchanged.
- New tests call `t.Parallel()` (except tests that touch process-global state).

## Review Focus

- A commit must reach the screen when ITS status line arrives, not when the next commit is found (the slow-file case: next hit seconds later) — pinned by `TestFileLogParserEmitsAtStatusLine` (Task 1) and `TestHistoryStreamFirstBatchBeforeDone` (Task 3).
- Leaving the window by any route must not leave a git walk blocked forever on a channel send — pinned by `TestHistoryChunkForGoneViewCancels` + `TestHistoryEscCancelsWalk` (Task 3); every send in the walk selects on `ctx.Done()`.
- A commit made while the window is open must not shift load-more's positional skip — pinned by `TestHistoryLoadMorePinsStartAndSkipsShown` (Task 4): the second walk's argv carries the resolved sha, not `HEAD`.
- A short final page (fewer than the limit) must NOT offer load more — pinned by `TestHistoryShortPageOffersNoLoadMore` (Task 4).
- A view parked under a console still receives its chunks — pinned by the updated `TestParkedViewReceivesItsAsyncResults` (Task 3).

---

### Task 1: git — incremental parser + `FileLogStream`

**Files:**
- Modify: `internal/git/file_log.go`
- Test: `internal/git/file_log_test.go`

**Interfaces:**
- Produces: `func (r *Repo) FileLogStream(ctx context.Context, rev, path string, limit int, emit func(model.FileCommit)) error` (span name `"git log (file history)"`, same as `FileLog`); `ParseFileLog` unchanged in signature.

- [ ] **Step 1: Write the failing tests** (append to `internal/git/file_log_test.go`)

```go
// A commit is complete at its name-status line: the streamed history must not
// hold commit N back until git finds commit N+1 (seconds apart on a rarely
// touched file in a huge repo).
func TestFileLogParserEmitsAtStatusLine(t *testing.T) {
	t.Parallel()
	var p fileLogParser
	if _, ok := p.line("aaa\x1f\x1fAda\x1f1700000000\x1fedit"); ok {
		t.Fatal("a format line alone must not complete a commit")
	}
	fc, ok := p.line("M\ta.go")
	if !ok || fc.Hash != "aaa" || fc.Status != "M" || fc.Path != "a.go" || fc.Subject != "edit" {
		t.Fatalf("status line must complete the commit, got %+v ok=%v", fc, ok)
	}
	if _, ok := p.flush(); ok {
		t.Fatal("nothing is left open after a completed commit")
	}
	// A commit with no status line completes at the next format line / flush.
	if _, ok := p.line("bbb\x1f\x1fBob\x1f1690000000\x1fmerge"); ok {
		t.Fatal("format line must not complete")
	}
	fc, ok = p.line("ccc\x1f\x1fAda\x1f1680000000\x1finit")
	if !ok || fc.Hash != "bbb" || fc.Status != "" {
		t.Fatalf("next format line must complete the open commit, got %+v ok=%v", fc, ok)
	}
	fc, ok = p.flush()
	if !ok || fc.Hash != "ccc" {
		t.Fatalf("flush must return the open commit, got %+v ok=%v", fc, ok)
	}
}

// FileLogStream yields exactly what FileLog returns, in order, across a rename.
func TestFileLogStreamMatchesFileLog(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "one\ntwo\nthree\nfour\n")
	gitIn(t, dir, "add", "a.go")
	gitIn(t, dir, "commit", "-m", "add a.go")
	write("a.go", "one\ntwo\nthree\nfour\nfive\n")
	gitIn(t, dir, "commit", "-am", "edit a.go")
	gitIn(t, dir, "mv", "a.go", "b.go")
	gitIn(t, dir, "commit", "-m", "rename to b.go")
	write("b.go", "one\ntwo\nthree\nfour\nfive\nsix\n")
	gitIn(t, dir, "commit", "-am", "edit b.go")

	want, err := repo.FileLog(context.Background(), "", "b.go", 50)
	if err != nil {
		t.Fatal(err)
	}
	var got []model.FileCommit
	if err := repo.FileLogStream(context.Background(), "", "b.go", 50, func(fc model.FileCommit) {
		got = append(got, fc)
	}); err != nil {
		t.Fatal(err)
	}
	if len(want) != 4 {
		t.Fatalf("FileLog fixture: want 4 commits, got %d: %+v", len(want), want)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stream != FileLog\n got: %+v\nwant: %+v", got, want)
	}
	if got[1].Status != "R" || got[1].OldPath != "a.go" || got[1].Path != "b.go" {
		t.Errorf("rename commit wrong: %+v", got[1])
	}
}

// The limit bounds the stream exactly like FileLog's -n.
func TestFileLogStreamHonoursLimit(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	for i := 0; i < 3; i++ {
		if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(strings.Repeat("x\n", i+1)), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, dir, "add", "a.go")
		gitIn(t, dir, "commit", "-m", "c")
	}
	n := 0
	if err := repo.FileLogStream(context.Background(), "", "a.go", 2, func(model.FileCommit) { n++ }); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("limit 2 emitted %d commits", n)
	}
}
```

Add `"reflect"`, `"strings"` and `"github.com/homeend/gigagit/internal/model"` to the test imports.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/git -run 'FileLogParser|FileLogStream' -count=1`
Expected: build FAIL — `undefined: fileLogParser`, `repo.FileLogStream undefined`.

- [ ] **Step 3: Implement** — replace the body of `internal/git/file_log.go` below the imports with:

```go
// fileLogArgv is the one argv FileLog and FileLogStream share.
// core.quotepath=false keeps non-ASCII paths raw in the --name-status lines
// (git otherwise octal-quotes them), so the parsed Path round-trips through
// ShowFile. -z is avoided here: it would mangle the --format/name-status
// interleave the parser relies on.
func fileLogArgv(rev, path string, limit int) []string {
	return gitcmd.New("log").
		Config("core.quotepath=false").
		ArgIf(rev != "", rev).
		Arg("--follow", "-M", "--name-status", "--format="+logFormat, "-n", strconv.Itoa(limit), "--", path).
		ToArgv()
}

// FileLog returns the commits that touched path, newest first, following the
// file across renames. rev "" starts from HEAD. One invocation. limit bounds
// history depth for very large repos.
func (r *Repo) FileLog(ctx context.Context, rev, path string, limit int) ([]model.FileCommit, error) {
	res, err := r.Runner.Run(ctx, "git log (file history)", fileLogArgv(rev, path, limit))
	if err != nil {
		return nil, err
	}
	return ParseFileLog([]byte(res.Stdout)), nil
}

// FileLogStream is FileLog handing each commit to emit as git prints it
// (newest first), so a caller can show the newest commits while git is still
// walking a huge history. One invocation; emit runs on the reader goroutine
// and may block (git then blocks on its full stdout pipe).
func (r *Repo) FileLogStream(ctx context.Context, rev, path string, limit int, emit func(model.FileCommit)) error {
	var p fileLogParser
	_, err := r.Runner.Stream(ctx, "git log (file history)", fileLogArgv(rev, path, limit), func(line string) {
		if fc, ok := p.line(line); ok {
			emit(fc)
		}
	})
	if err != nil {
		return err
	}
	if fc, ok := p.flush(); ok {
		emit(fc)
	}
	return nil
}

// ParseFileLog parses interleaved `git log --name-status --format=<logFormat>`
// output: a format line (contains \x1f) opens a commit; the following
// tab-bearing line is that commit's name-status for the followed file.
func ParseFileLog(data []byte) []model.FileCommit {
	var p fileLogParser
	var out []model.FileCommit
	for _, line := range strings.Split(string(data), "\n") {
		if fc, ok := p.line(line); ok {
			out = append(out, fc)
		}
	}
	if fc, ok := p.flush(); ok {
		out = append(out, fc)
	}
	return out
}

// fileLogParser is the line-at-a-time parser behind ParseFileLog and
// FileLogStream. A commit completes at its name-status line — not at the next
// commit's format line — so a stream never holds the newest hit back until git
// finds the next one. A commit with no status line (a merge) completes at the
// next format line or at flush.
type fileLogParser struct {
	open *model.FileCommit
}

// line feeds one output line and returns the commit it completed, if any.
func (p *fileLogParser) line(line string) (model.FileCommit, bool) {
	if line == "" {
		return model.FileCommit{}, false
	}
	if strings.Contains(line, "\x1f") {
		f := strings.Split(line, "\x1f")
		if len(f) < 5 {
			return model.FileCommit{}, false
		}
		prev, had := p.flush()
		fc := model.FileCommit{Commit: model.Commit{Hash: f[0], Author: f[2], Subject: f[4]}}
		if ps := strings.Fields(f[1]); len(ps) > 0 {
			fc.Commit.Parents = ps
		}
		if t, err := strconv.ParseInt(f[3], 10, 64); err == nil {
			fc.Commit.UnixTime = t
		}
		p.open = &fc
		return prev, had
	}
	if p.open == nil || !strings.Contains(line, "\t") {
		return model.FileCommit{}, false
	}
	nf := strings.Split(line, "\t")
	fc := p.open
	fc.Status = nf[0][:1]
	switch {
	case (fc.Status == "R" || fc.Status == "C") && len(nf) >= 3:
		fc.OldPath = nf[1]
		fc.Path = nf[2]
	case len(nf) >= 2:
		fc.Path = nf[1]
	}
	return p.flush()
}

// flush returns the open commit, if any, and closes it.
func (p *fileLogParser) flush() (model.FileCommit, bool) {
	if p.open == nil {
		return model.FileCommit{}, false
	}
	fc := *p.open
	p.open = nil
	return fc, true
}
```

- [ ] **Step 4: Run the package tests**

Run: `go test ./internal/git -run 'FileLog' -count=1`
Expected: PASS (incl. the existing `TestParseFileLog`, `TestFileLog`, `TestFileLogNonASCIIPathRoundTrip`).

- [ ] **Step 5: Commit**

```bash
git add internal/git/file_log.go internal/git/file_log_test.go
git commit -m "feat(git): FileLogStream — file history commit by commit"
```

---

### Task 2: domain — ungated `Service.FileLogStream`

**Files:**
- Modify: `internal/domain/query.go` (next to `FileLog`, ~line 542)
- Test: `internal/domain/query_test.go`

**Interfaces:**
- Consumes: `(*git.Repo).FileLogStream` (Task 1).
- Produces: `func (s *Service) FileLogStream(ctx context.Context, rev, path string, limit int, emit func(model.FileCommit)) error`.

- [ ] **Step 1: Write the failing test** (append to `internal/domain/query_test.go`)

```go
// The streamed file history takes NO reservation: a 20 s walk on a huge repo
// must not make a tree write (Copy to working dir) wait for it.
func TestFileLogStreamTakesNoReservation(t *testing.T) {
	f := gitexec.NewFakeRunner()
	svc := New(&git.Repo{Runner: f})
	var held []repogate.Entry
	f.SetHandler("git log (file history)", func(ctx context.Context, argv []string) (gitexec.Result, error) {
		held = svc.gateFor(ctx).Queue()
		return gitexec.Result{Stdout: "aaa\x1f\x1fAda\x1f1700000000\x1fadd\nA\ta.go\n"}, nil
	})
	var got []model.FileCommit
	err := svc.FileLogStream(context.Background(), "", "a.go", 200, func(fc model.FileCommit) {
		got = append(got, fc)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != 0 {
		t.Fatalf("FileLogStream held the gate mid-walk: %+v", held)
	}
	if len(got) != 1 || got[0].Hash != "aaa" || got[0].Status != "A" || got[0].Path != "a.go" {
		t.Fatalf("emitted %+v", got)
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/domain -run TestFileLogStreamTakesNoReservation -count=1`
Expected: build FAIL — `svc.FileLogStream undefined`.

- [ ] **Step 3: Implement** — add below `FileLog` in `internal/domain/query.go`:

```go
// FileLogStream walks path's history like FileLog but hands each commit to
// emit as git prints it, newest first. It deliberately takes NO reservation
// and is not coalesced: git log reads immutable objects from a start rev it
// resolves once and never takes index.lock, so a concurrent tree write cannot
// tear it — while holding Read for a walk that can run 20 s on a huge repo
// would park every write (and, writer-preferring, every later read) behind it.
func (s *Service) FileLogStream(ctx context.Context, rev, path string, limit int, emit func(model.FileCommit)) error {
	err := s.repo.FileLogStream(ctx, rev, path, limit, emit)
	if err != nil && ctx.Err() == nil {
		observ.NoteFailure("filelog stream "+path, err)
	}
	return err
}
```

- [ ] **Step 4: Run the test**

Run: `go test ./internal/domain -run 'FileLog' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/query.go internal/domain/query_test.go
git commit -m "feat(domain): FileLogStream, ungated"
```

---

### Task 3: TUI — stream the history list

**Files:**
- Modify: `internal/tui/history_view.go` (view state, loader, chunk handler, header, esc)
- Modify: `internal/tui/model.go:1308-1318` (replace the `historyListMsg` case), `internal/tui/model.go:2952-2954`
- Modify callers: `internal/tui/diff_view.go:1088-1090`, `internal/tui/file_path_popup.go:185-187`, `internal/tui/files_view.go:1082-1084`, `internal/tui/file_finder.go:55-57`, `internal/tui/blame_view.go:483-485` — `m.loadHistoryListCmd(ctx, hv.listTag)` → `m.loadHistoryListCmd(hv)`
- Modify: `internal/tui/alt_cycle_test.go:403-417` (`TestParkedViewReceivesItsAsyncResults` → chunk msg)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml` (2 keys)
- Test: create `internal/tui/history_stream_test.go`

**Interfaces:**
- Consumes: `(*domain.Service).FileLogStream`, `(*domain.Service).RevParse(ctx, rev) (string, error)`.
- Produces (used by Task 4):
  - `const historyPage = 200`, `historyBatchMax = 20`, `historyBatchDelay = 50 * time.Millisecond`
  - `historyView` fields `cancel context.CancelFunc`, `gen int`, `streaming bool`, `streamErr error`, `start string`, `more bool`, `advance bool`
  - `func (h *historyView) stop()`
  - `func (m Model) loadHistoryListCmd(h *historyView) tea.Cmd` — walks from `h.start` (or `h.ctx.rev`), skipping `len(h.commits)`, limit `len(h.commits)+historyPage`
  - `type historyChunkMsg struct { view *historyView; gen int; start string; commits []model.FileCommit; done, more bool; err error; next <-chan historyChunkMsg }`
  - `func waitHistoryChunk(ch <-chan historyChunkMsg) tea.Cmd`
  - `func (m Model) onHistoryChunk(msg historyChunkMsg) (Model, tea.Cmd)`

- [ ] **Step 1: Write the failing tests** — create `internal/tui/history_stream_test.go`:

```go
package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
)

// histRunner fakes the file-history walk: Stream emits min(-n, total) commits
// c000, c001, … (newest first), then — when block is set — waits for release
// or ctx cancellation. "git rev-parse" answers the pinned start sha.
type histRunner struct {
	total   int
	block   bool
	release chan struct{}
	ctxDone chan struct{}

	mu    sync.Mutex
	walks [][]string
}

func newHistRunner(total int, block bool) *histRunner {
	return &histRunner{total: total, block: block, release: make(chan struct{}), ctxDone: make(chan struct{})}
}

func (r *histRunner) Run(ctx context.Context, name string, argv []string) (gitexec.Result, error) {
	if name == "git rev-parse" {
		return gitexec.Result{Stdout: "feedface\n"}, nil
	}
	return gitexec.Result{}, fmt.Errorf("histRunner: no %q", name)
}

func (r *histRunner) RunEnv(ctx context.Context, name string, argv, _ []string) (gitexec.Result, error) {
	return r.Run(ctx, name, argv)
}

func (r *histRunner) Stream(ctx context.Context, name string, argv []string, onLine func(string)) (gitexec.Result, error) {
	r.mu.Lock()
	r.walks = append(r.walks, argv)
	r.mu.Unlock()
	n := r.total
	for i, a := range argv {
		if a == "-n" && i+1 < len(argv) {
			if v, err := strconv.Atoi(argv[i+1]); err == nil && v < n {
				n = v
			}
		}
	}
	for i := 0; i < n; i++ {
		onLine(fmt.Sprintf("c%03d\x1f\x1fAda\x1f%d\x1fsubject %d", i, 1700000000-i, i))
		onLine("M\ta.go")
	}
	if r.block {
		select {
		case <-r.release:
		case <-ctx.Done():
			close(r.ctxDone)
			return gitexec.Result{}, ctx.Err()
		}
	}
	return gitexec.Result{}, nil
}

func (r *histRunner) walk(i int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.walks[i]
}

func fileHistModel(r *histRunner) (Model, *historyView) {
	m := Model{width: 100, height: 30, svc: domain.New(&git.Repo{Runner: r})}
	h := newHistoryView(navContext{path: "a.go"})
	return m.pushLayer(h), h
}

// drainHistory feeds a walk's messages to the model until its done message.
func drainHistory(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	msg := cmd().(historyChunkMsg)
	for {
		m, _ = m.onHistoryChunk(msg)
		if msg.done {
			return m
		}
		msg = waitHistoryChunk(msg.next)().(historyChunkMsg)
	}
}

// The newest commits land, get selected and start their diff while git is
// still walking; the header counts them until the walk ends.
func TestHistoryStreamFirstBatchBeforeDone(t *testing.T) {
	t.Parallel()
	r := newHistRunner(2, true)
	m, h := fileHistModel(r)
	msg := m.loadHistoryListCmd(h)().(historyChunkMsg)
	if msg.done {
		t.Fatal("first batch must arrive while the walk still runs")
	}
	m, next := m.onHistoryChunk(msg)
	if len(h.commits) != 2 || h.sel != 0 || h.loading || !h.streaming {
		t.Fatalf("after first batch: commits=%d sel=%d loading=%v streaming=%v", len(h.commits), h.sel, h.loading, h.streaming)
	}
	if h.diffTag == "" || next == nil {
		t.Fatal("first batch must select row 0 and load its diff, and re-arm the walk")
	}
	if out := h.render(m, ""); !strings.Contains(out, "loading… 2 found") {
		t.Fatalf("header must count the found commits while loading:\n%s", out)
	}
	close(r.release)
	done := waitHistoryChunk(msg.next)().(historyChunkMsg)
	if !done.done {
		t.Fatalf("expected the done message, got %+v", done)
	}
	m, _ = m.onHistoryChunk(done)
	if h.streaming || h.more {
		t.Fatalf("after done: streaming=%v more=%v", h.streaming, h.more)
	}
	// Only the header: the right pane still shows its diff placeholder.
	if hdr := strings.SplitN(h.render(m, ""), "\n", 2)[0]; strings.Contains(hdr, "loading…") {
		t.Fatalf("header still says loading after the walk ended: %q", hdr)
	}
}

// esc stops the git walk at once.
func TestHistoryEscCancelsWalk(t *testing.T) {
	t.Parallel()
	r := newHistRunner(1, true)
	m, h := fileHistModel(r)
	msg := m.loadHistoryListCmd(h)().(historyChunkMsg)
	m, _ = m.onHistoryChunk(msg)
	m, _ = h.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.topLayer() != nil {
		t.Fatal("esc must pop the history view")
	}
	select {
	case <-r.ctxDone:
	case <-time.After(2 * time.Second):
		t.Fatal("esc did not cancel the git walk")
	}
}

// A chunk for a view that went another way (repo switch, clearLayers) cancels
// the walk and is not re-armed.
func TestHistoryChunkForGoneViewCancels(t *testing.T) {
	t.Parallel()
	r := newHistRunner(1, true)
	m, h := fileHistModel(r)
	msg := m.loadHistoryListCmd(h)().(historyChunkMsg)
	m = m.clearLayers()
	_, cmd := m.onHistoryChunk(msg)
	if cmd != nil {
		t.Fatal("a gone view's walk must not be re-armed")
	}
	select {
	case <-r.ctxDone:
	case <-time.After(2 * time.Second):
		t.Fatal("a gone view's walk was not cancelled")
	}
}

// A superseded walk's chunks are dropped.
func TestHistoryStaleGenDropped(t *testing.T) {
	t.Parallel()
	m, h := fileHistModel(newHistRunner(0, false))
	h.gen = 2
	m, cmd := m.onHistoryChunk(historyChunkMsg{view: h, gen: 1, commits: histFixture().commits, done: true})
	if len(h.commits) != 0 || cmd != nil {
		t.Fatalf("stale chunk applied: commits=%d", len(h.commits))
	}
}

// The whole walk ends in one list, in git's order.
func TestHistoryStreamDrainsAll(t *testing.T) {
	t.Parallel()
	r := newHistRunner(45, false) // > historyBatchMax: several batches
	m, h := fileHistModel(r)
	_ = drainHistory(t, m, m.loadHistoryListCmd(h))
	if len(h.commits) != 45 || h.commits[0].Hash != "c000" || h.commits[44].Hash != "c044" {
		t.Fatalf("drained %d commits, first=%v", len(h.commits), h.commits)
	}
}
```

Add `tea "github.com/charmbracelet/bubbletea"` to the imports.

Also replace `TestParkedViewReceivesItsAsyncResults` in `internal/tui/alt_cycle_test.go` (lines 403-417) with:

```go
// A view parked under a console still receives its async results: a history
// still loading when the cycle parked it is filled when its walk lands.
func TestParkedViewReceivesItsAsyncResults(t *testing.T) {
	m, _ := fullScreenAgent(t)
	h := &historyView{loading: true, gen: 1}
	m.console.ret.layers = append(m.console.ret.layers, h)
	mm, _ := m.Update(historyChunkMsg{view: h, gen: 1, done: true})
	m = mm.(Model)
	if h.loading {
		t.Fatal("the parked history never got its list")
	}
	if m.console == nil || m.topLayer() != nil || len(m.console.ret.layers) != 2 {
		t.Fatalf("the stack must stay parked: console=%+v top=%T", m.console, m.topLayer())
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/tui -run 'HistoryStream|HistoryEscCancels|HistoryChunkForGone|HistoryStaleGen|ParkedViewReceives' -count=1`
Expected: build FAIL — `undefined: historyChunkMsg`, `m.onHistoryChunk undefined`, …

- [ ] **Step 3: Implement in `internal/tui/history_view.go`**

Replace `historyMaxCommits` with:

```go
// historyPage is one walk's worth of file history; load more asks for the
// next page.
const historyPage = 200

// A walk hands commits to the UI in batches: at historyBatchMax pending
// commits, or historyBatchDelay after the first pending one — so the newest
// commit shows within 50 ms even when git takes seconds to find the next.
const (
	historyBatchMax   = 20
	historyBatchDelay = 50 * time.Millisecond
)
```

Replace the `historyView` struct, `newHistoryView`, `historyListMsg` and `loadHistoryListCmd` with:

```go
type historyView struct {
	ctx     navContext
	commits []model.FileCommit
	sel     int
	mode    dispMode // left list text display mode; z cycles
	hscroll int      // modeScroll horizontal offset
	loading bool     // no commit has arrived yet
	err     error    // the walk failed before finding any commit
	diff    *diffView // right pane (reuses diffView rendering + guards)
	diffTag string    // gates stale right-pane loads

	// The streamed walk (loadHistoryListCmd). gen gates a superseded walk's
	// chunks; cancel stops the running git; start pins the first walk's start
	// sha so a load-more walk skips exactly the commits already shown.
	cancel    context.CancelFunc
	gen       int
	streaming bool  // a walk is running
	streamErr error // the walk failed after some commits arrived
	start     string
	more      bool // the last walk filled its page: older commits may exist
	advance   bool // load more: step onto the first new commit when it lands
}

func newHistoryView(ctx navContext) *historyView {
	return &historyView{ctx: ctx, loading: true}
}

// stop cancels a running walk (its git process is terminated).
func (h *historyView) stop() {
	if h.cancel != nil {
		h.cancel()
		h.cancel = nil
	}
}

// historyChunkMsg is one batch of a streamed walk. next yields the walk's
// following message; the handler re-arms it until done.
type historyChunkMsg struct {
	view    *historyView
	gen     int
	start   string // the walk's resolved start rev (pinned for load more)
	commits []model.FileCommit
	done    bool
	more    bool // done and the walk hit its limit
	err     error
	next    <-chan historyChunkMsg
}

type historyDiffMsg struct {
	tag  string
	view *diffView
}

// loadHistoryListCmd starts a streamed walk for h: the first walk, or — with
// commits already listed — load more, which re-walks from the pinned start
// with a page more and drops the commits already shown (git log --follow
// ignores --skip).
func (m Model) loadHistoryListCmd(h *historyView) tea.Cmd {
	h.stop()
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	h.gen++
	h.streaming = true
	h.streamErr = nil
	w := historyWalk{
		svc:   m.svc,
		gen:   h.gen,
		view:  h,
		rev:   h.ctx.rev,
		pin:   h.start == "",
		path:  h.ctx.path,
		skip:  len(h.commits),
		limit: len(h.commits) + historyPage,
	}
	if !w.pin {
		w.rev = h.start
	}
	// The walk starts when the cmd runs, not when it is built: a caller that
	// builds the cmd and drops it starts no git.
	return func() tea.Msg {
		out := make(chan historyChunkMsg)
		go w.run(ctx, out)
		return <-out
	}
}

// waitHistoryChunk delivers a walk's next message.
func waitHistoryChunk(ch <-chan historyChunkMsg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

// historyWalk is one streamed git log --follow, run off the UI thread.
type historyWalk struct {
	svc         *domain.Service
	view        *historyView
	gen         int
	rev, path   string
	pin         bool
	skip, limit int
}

// run streams the walk into out in batches. Every send selects on ctx, so a
// walk whose view went away never blocks forever.
func (w historyWalk) run(ctx context.Context, out chan historyChunkMsg) {
	start := w.rev
	if w.pin {
		r := w.rev
		if r == "" {
			r = "HEAD"
		}
		if sha, err := w.svc.RevParse(ctx, r); err == nil {
			start = sha
		}
	}
	found := make(chan model.FileCommit, historyBatchMax)
	type result struct {
		n   int
		err error
	}
	finished := make(chan result, 1)
	go func() {
		n := 0
		err := w.svc.FileLogStream(ctx, start, w.path, w.limit, func(fc model.FileCommit) {
			n++
			if n <= w.skip {
				return
			}
			select {
			case found <- fc:
			case <-ctx.Done():
			}
		})
		finished <- result{n: n, err: err}
	}()

	msg := historyChunkMsg{view: w.view, gen: w.gen, start: start, next: out}
	var batch []model.FileCommit
	var tick <-chan time.Time
	send := func(done, more bool, err error) bool {
		m := msg
		m.commits, m.done, m.more, m.err = batch, done, more, err
		select {
		case out <- m:
			msg.start = "" // only the first message carries it
			batch, tick = nil, nil
			return true
		case <-ctx.Done():
			return false
		}
	}
	for {
		select {
		case fc := <-found:
			batch = append(batch, fc)
			if len(batch) == 1 {
				tick = time.After(historyBatchDelay)
			}
			if len(batch) >= historyBatchMax && !send(false, false, nil) {
				return
			}
		case <-tick:
			if !send(false, false, nil) {
				return
			}
		case r := <-finished:
			// Every emit's send completed before finished was written: drain.
			for drained := false; !drained; {
				select {
				case fc := <-found:
					batch = append(batch, fc)
				default:
					drained = true
				}
			}
			send(true, r.err == nil && r.n >= w.limit, r.err)
			return
		case <-ctx.Done():
			return
		}
	}
}

// historyLive reports whether h is still a view the user can come back to:
// on the stack (a console's parked views are put back for non-key messages,
// see dispatchParkedAware) or parked by a hand-off to the files view.
func (m Model) historyLive(h *historyView) bool {
	if m.hasLayer(h) {
		return true
	}
	for _, l := range m.filesReturnLayers {
		if l == h {
			return true
		}
	}
	return false
}

// onHistoryChunk applies one batch of a streamed walk.
func (m Model) onHistoryChunk(msg historyChunkMsg) (Model, tea.Cmd) {
	h := msg.view
	if h == nil || h.gen != msg.gen {
		return m, nil // superseded: loadHistoryListCmd cancelled that walk
	}
	if !m.historyLive(h) {
		h.stop()
		return m, nil
	}
	if msg.start != "" {
		h.start = msg.start
	}
	var cmds []tea.Cmd
	if len(msg.commits) > 0 {
		first := len(h.commits) == 0
		h.commits = append(h.commits, msg.commits...)
		h.loading = false
		switch {
		case first:
			h.sel = 0
			cmds = append(cmds, h.selectCmd(m))
		case h.advance:
			h.advance = false
			if h.sel < len(h.commits)-1 {
				h.sel++
				cmds = append(cmds, h.selectCmd(m))
			}
		}
	}
	if !msg.done {
		return m, tea.Batch(append(cmds, waitHistoryChunk(msg.next))...)
	}
	h.stop()
	h.streaming, h.loading, h.advance = false, false, false
	h.more = msg.more
	if msg.err != nil && !errors.Is(msg.err, context.Canceled) {
		if len(h.commits) == 0 {
			h.err = msg.err
		} else {
			h.streamErr = msg.err
		}
	}
	return m, tea.Batch(cmds...)
}

// headerSuffix is the walk's state after the path: the running count, or a
// failure that came after some commits were already listed.
func (h *historyView) headerSuffix() string {
	switch {
	case h.streaming && len(h.commits) > 0:
		return i18n.T(" · loading… %d found", len(h.commits))
	case h.streamErr != nil:
		return i18n.T(" · error: %s", h.streamErr.Error())
	}
	return ""
}
```

Add `"errors"` and `"time"` to the imports.

In `render`, replace the header line with:

```go
	sfx := h.headerSuffix()
	pathW := w - lipgloss.Width(sfx)
	if pathW < 1 {
		pathW = 1
	}
	header := truncate(elidePath(i18n.T("history: %s", h.ctx.path), pathW)+sfx, w) // keep the file name
```

In `update`, change the `esc`/`h` case to stop the walk first:

```go
	case "esc", "h":
		h.stop()
		return m.popLayer(), nil
```

- [ ] **Step 4: Route the message and update the openers**

In `internal/tui/model.go` replace the `case historyListMsg:` block (lines 1308-1318) with:

```go
	case historyChunkMsg:
		return m.onHistoryChunk(msg)
```

In `internal/tui/model.go:2952-2954`, `diff_view.go:1088-1090`, `file_path_popup.go:185-187`, `files_view.go:1082-1084`, `file_finder.go:55-57`, `blame_view.go:483-485`: replace `m.loadHistoryListCmd(ctx, hv.listTag)` (`h.listTag` in model.go) with `m.loadHistoryListCmd(hv)` (`h` in model.go). If `ctx` becomes unused at a call site, keep it — it is passed to `newHistoryView(ctx)` there. Then:

Run: `rtk proxy grep -rn "listTag\|historyListMsg\|historyMaxCommits" internal/`
Expected: no matches.

- [ ] **Step 5: Translations** — add to each bundle next to `"history: %s"` (line ~1616):

`internal/i18n/lang/ja.toml`
```toml
" · loading… %d found" = " · 読み込み中… %d 件"
" · error: %s" = " · エラー: %s"
```
`internal/i18n/lang/ko.toml`
```toml
" · loading… %d found" = " · 불러오는 중… %d개 찾음"
" · error: %s" = " · 오류: %s"
```
`internal/i18n/lang/zh.toml`
```toml
" · loading… %d found" = " · 加载中… 已找到 %d 个"
" · error: %s" = " · 错误: %s"
```
`internal/i18n/lang/ru.toml`
```toml
" · loading… %d found" = " · загрузка… найдено %d"
" · error: %s" = " · ошибка: %s"
```

(If a key already exists in a bundle, the TOML load fails on the duplicate — `rtk proxy grep -n '" · error: %s"' internal/i18n/lang/*.toml` first and reuse it.)

- [ ] **Step 6: Run the TUI tests**

Run: `go test ./internal/tui -run 'History|ParkedViewReceives|I18n|Vocab|MenuLabels|EngineProse' -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/tui internal/i18n/lang
git commit -m "feat(tui): stream file history — newest commits show while git walks"
```

---

### Task 4: TUI — load more

**Files:**
- Modify: `internal/tui/history_view.go` (`listRows`, `update` down/j)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml` (1 key)
- Test: `internal/tui/history_stream_test.go`

**Interfaces:**
- Consumes: everything Task 3 produces (`historyPage`, `h.more`, `h.advance`, `h.start`, `loadHistoryListCmd(h)`, `drainHistory`, `histRunner`).

- [ ] **Step 1: Write the failing tests** (append to `internal/tui/history_stream_test.go`)

```go
// A full page offers load more; ↓ on the last commit re-walks from the
// PINNED start with a page more, drops what is shown, and steps onto the
// first older commit.
func TestHistoryLoadMorePinsStartAndSkipsShown(t *testing.T) {
	t.Parallel()
	r := newHistRunner(historyPage+5, false)
	m, h := fileHistModel(r)
	m = drainHistory(t, m, m.loadHistoryListCmd(h))
	if len(h.commits) != historyPage || !h.more {
		t.Fatalf("first page: %d commits, more=%v", len(h.commits), h.more)
	}
	h.sel = len(h.commits) - 1 // the row ends the list: render with it in view
	if out := h.render(m, ""); !strings.Contains(out, "load 200 older commits") {
		t.Fatalf("a full page must offer load more:\n%s", out)
	}
	m, cmd := h.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if cmd == nil {
		t.Fatal("j on the last commit of a full page must start load more")
	}
	m = drainHistory(t, m, cmd)
	argv := strings.Join(r.walk(1), " ")
	if !strings.Contains(argv, " feedface ") || !strings.Contains(argv, "-n 400") {
		t.Fatalf("load more must walk from the pinned sha with a page more: %s", argv)
	}
	if len(h.commits) != historyPage+5 || h.commits[historyPage].Hash != fmt.Sprintf("c%03d", historyPage) {
		t.Fatalf("load more appended wrong commits: %d, [200]=%v", len(h.commits), h.commits[historyPage].Hash)
	}
	if h.sel != historyPage {
		t.Fatalf("selection must step onto the first older commit, sel=%d", h.sel)
	}
	if h.more {
		t.Fatal("a short second page must clear load more")
	}
}

// A short page is all history: no load-more row, and j on the last commit
// does nothing.
func TestHistoryShortPageOffersNoLoadMore(t *testing.T) {
	t.Parallel()
	r := newHistRunner(5, false)
	m, h := fileHistModel(r)
	m = drainHistory(t, m, m.loadHistoryListCmd(h))
	if h.more {
		t.Fatal("5 < page: more must be false")
	}
	h.sel = len(h.commits) - 1
	if out := h.render(m, ""); strings.Contains(out, "older commits") {
		t.Fatalf("a short page must not offer load more:\n%s", out)
	}
	if _, cmd := h.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}); cmd != nil {
		t.Fatal("j on the last commit of a short page must do nothing")
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/tui -run 'HistoryLoadMore|HistoryShortPage' -count=1`
Expected: FAIL — no "load 200 older commits" row; `j` returns nil cmd.

- [ ] **Step 3: Implement**

At the end of `listRows` (before `return rows, anchor`):

```go
	if h.more && !h.streaming {
		rows = append(rows, winRow{text: truncate(i18n.T("    ↓ load %d older commits", historyPage), listW), style: s.dim})
	}
```

Replace the `"down", "j"` case in `update` with:

```go
	case "down", "j":
		if h.sel < len(h.commits)-1 {
			h.sel++
			return m, h.selectCmd(m)
		}
		// On the last commit of a full page: fetch the next page and step onto
		// its first commit when it lands.
		if h.more && !h.streaming && len(h.commits) > 0 {
			h.more, h.advance = false, true
			return m, m.loadHistoryListCmd(h)
		}
```

Translations, next to the Task 3 keys:

```toml
# ja
"    ↓ load %d older commits" = "    ↓ さらに古いコミットを %d 件読み込む"
# ko
"    ↓ load %d older commits" = "    ↓ 이전 커밋 %d개 더 불러오기"
# zh
"    ↓ load %d older commits" = "    ↓ 加载更早的 %d 个提交"
# ru
"    ↓ load %d older commits" = "    ↓ загрузить ещё %d более старых коммитов"
```

(Only the value goes in each file; the `# xx` lines mark which file.)

- [ ] **Step 4: Run the TUI tests**

Run: `go test ./internal/tui -run 'History|I18n|Vocab|MenuLabels' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui internal/i18n/lang
git commit -m "feat(tui): history load more — ↓ past the last commit fetches 200 older"
```

---

### Task 5: Docs, full gate, manual check on linux

**Files:**
- Modify: `CHANGELOG.md`, `docs/CLAUDE-details.md` (history entry), `internal/tui/help.go` (history row, if it lists history keys), `README.md` (only if it describes history loading)

- [ ] **Step 1: Docs**

- `CHANGELOG.md` (Unreleased, top): "File history (`h`) streams: commits appear as git finds them, the newest is selected and diffed at once, the header counts `loading… N found`; leaving the window stops the walk. A full 200-commit page ends with `↓ load 200 older commits` — ↓ past the last commit fetches the next page. The walk holds no repo lock, so Copy to working dir works mid-load."
- `docs/CLAUDE-details.md`: find the history-view entry (`rtk proxy grep -n "history" docs/CLAUDE-details.md | head`) and add: streamed via `domain.FileLogStream` (ungated, uncoalesced), `historyChunkMsg` + `gen`, load more = re-walk from the pinned sha with `-n shown+200`, skipping `shown` (git log --follow ignores --skip).
- `internal/tui/help.go`: if a history help row lists `↑↓`, extend its text with "(↓ past the last commit loads 200 older)" — a changed key needs all four bundles updated (adding-translations skill).

- [ ] **Step 2: Full gate**

Run: `./test.sh race 2>&1 | tee /tmp/claude-1000/-work-gigagit/4840d1cc-6dab-4edb-a1c6-23b99ce18e71/scratchpad/race.log | tail -5; grep -c "all green" /tmp/claude-1000/-work-gigagit/4840d1cc-6dab-4edb-a1c6-23b99ce18e71/scratchpad/race.log`
Expected: "all green" present (count ≥ 1).

- [ ] **Step 3: Manual check on linux**

```bash
go build -o bin/gg ./cmd/gg
```
Drive `bin/gg` in `/home/homeend/others/linux` via `./tui-capture.sh` (driving-tui-headless skill): open history on `drivers/net/ethernet/3com/3c509.c` → within ~1 s a commit + its diff are on screen with `loading… N found`; `enter` → `.` shows Copy to working dir; esc back; on `kernel/sched/core.c` the 200-page row appears and ↓ past the end loads more. Confirm no `git log` process lingers after esc (`pgrep -fa "git log --follow"`).

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md docs/CLAUDE-details.md internal/tui/help.go internal/i18n/lang README.md
git commit -m "docs: streaming file history + load more"
```
