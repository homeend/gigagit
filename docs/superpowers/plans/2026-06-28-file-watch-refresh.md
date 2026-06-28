# Phase D — File-watch auto-refresh — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refresh a panel the instant its underlying git state changes by watching a fixed, small set of `.git` files with `fsnotify`, feeding the existing single-lane refresh queue; interval polling stays the fallback.

**Architecture:** A new pure-of-git/TUI package `internal/gitwatch` wraps `fsnotify`: `Plan` (pure path→source map), `Supported` (statfs 9P gate), and `Watcher` (debounced events). The TUI owns one watcher; a blocking `tea.Cmd` turns each event into a `watchEventMsg` whose handler calls the existing `enqueueDue`. A per-source `watch` toggle lives in the Refresh-rates editor; the interval field is the drvfs/off fallback.

**Tech Stack:** Go 1.26, Bubble Tea (Elm value-receiver Model), `github.com/fsnotify/fsnotify`, `github.com/pelletier/go-toml/v2`.

## Global Constraints

- **Module:** `github.com/homeend/gigagit`; Go 1.26. Spec: `docs/superpowers/specs/2026-06-28-file-watch-refresh-design.md`.
- **No whole-worktree watching, ever.** Only fixed, small `.git` file/dir sets. `status` is excluded; `tags`/`feed`/`identity` are out of scope.
- **v1 watch set = worktrees, branches, reflog, remotes.** D1 implements worktrees + reflog; D2 adds branches + remotes.
- **Watcher is only a trigger.** It must reuse the existing lane via `enqueueDue(queue, active, busy, due)` — no new refresh machinery, no new lane.
- **`internal/gitwatch` imports only fsnotify + stdlib** — no `internal/git`, `internal/tui`, `internal/domain`, `lipgloss`. (Mirrors `commitgraph`/`textdiff`; archtest-guarded boundary style.)
- **drvfs gate:** `Supported` returns false for 9P/v9fs mounts (Linux fs magic `0x01021997`), true otherwise. A watch-on source on an unsupported fs falls back to interval polling.
- **`internal/tui` and `internal/cli` never import `internal/git`** — reach git only through `internal/domain` (archtest-guarded).
- **A git verb is one invocation** built with `gitcmd` and run via `r.Runner`.
- **Config stays read-only at runtime except narrow line-edit writers.** New writer `SetRefreshWatch` mirrors `SetRefreshInterval` (writes the repo `.gg.toml`).
- **Watch-active = eligible AND watch-on AND supported.** Only sources the implemented `Plan` actually covers are eligible (so a premature `branches_watch=true` in D1 still polls, never goes stale).
- **Advertise the editor `w` key in both the editor help line and (if surfaced) the footer** (project convention).
- **Commit trailers:** every commit body ends with:
  ```
  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_012KeCYxqdcKWUv9Zk9QYKFz
  ```
- **Tests use a real `git` in `t.TempDir()`** (ext4 `/tmp` here, so the live-watch path is exercised) or `FakeRunner` for argv. Build the test binary as `go build -o ./gg ./cmd/gg` from the worktree.

---

## File Structure

**New — `internal/gitwatch/`:**
- `source.go` — `Source` enum + `String()`.
- `plan.go` — `Group` type + `Plan(commonDir, worktreeDir, enabled) []Group` (pure).
- `supported_linux.go` / `supported_other.go` — `Supported(commonDir) bool` (build-tag split).
- `watcher.go` — `Watcher`, `New`, `Events`, `Close` (fsnotify + debounce; non-recursive in D1, recursive added in D2).
- `*_test.go` — pure `Plan` tests, `Supported` test, live `Watcher` integration test.

**Modified:**
- `internal/config/config.go` — `RefreshConfig` watch bools + `overlayRefresh`.
- `internal/config/write.go` — `SetRefreshWatch`.
- `internal/config/template.go` — `settingDocs` entries for the `*_watch` keys.
- `internal/git/worktree.go` — `GitDir` verb (`rev-parse --absolute-git-dir`).
- `internal/domain/query.go` — `GitDir` query.
- `internal/tui/refresh.go` — `watchActive`, `dueItems` signature, watch-eligibility set.
- `internal/tui/source.go` — `gitwatch.Source → sourceKey` map, `enabledWatchSources`.
- `internal/tui/watch.go` (new) — `watchEventMsg`, `watchReadyMsg`, `watchClosedMsg`, `startWatchCmd`, `watchListenCmd`, handlers' helpers.
- `internal/tui/model.go` — model fields, `configReadyMsg` batch, `watch*Msg` cases, `reRoot` teardown/rebuild, `refreshTick` call site.
- `internal/tui/settings_popup.go` — editor watch column + `w` toggle + help line.
- `go.mod`, `go.sum`.
- `CHANGELOG.md`, `README.md`, `CLAUDE.md`.

---

# Phase D1 — infrastructure + trivial sources (worktrees, reflog)

### Task 1: Add fsnotify + `gitwatch.Source` + `Supported`

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `internal/gitwatch/source.go`
- Create: `internal/gitwatch/supported_linux.go`
- Create: `internal/gitwatch/supported_other.go`
- Test: `internal/gitwatch/supported_test.go`

**Interfaces:**
- Produces: `gitwatch.Source` (`Branches|Remotes|Reflog|Worktrees`), `(Source).String() string`, `gitwatch.Supported(commonDir string) bool`, `gitwatch.V9fsMagic` (exported const for the test).

- [ ] **Step 1: Add the dependency**

Run from the worktree root:
```bash
go get github.com/fsnotify/fsnotify@latest
```
Expected: `go.mod` gains a `require github.com/fsnotify/fsnotify vX.Y.Z` line; `go.sum` updated.

- [ ] **Step 2: Write `source.go`**

```go
// Package gitwatch watches a fixed, small set of a repository's .git internal
// files and reports which data sources a change affects. It is the event-driven
// trigger behind gigagit's auto-refresh: instead of polling on a timer, the TUI
// refreshes a panel the moment the .git files backing it change.
//
// The package is pure of gigagit's git/TUI/domain layers — it depends only on
// fsnotify and the standard library — so its .git-layout knowledge (which files
// back which source) is unit-testable in isolation.
package gitwatch

// Source is a watch-eligible data source. gitwatch owns this enum so the package
// stays decoupled from internal/tui's sourceKey; the TUI maps Source→sourceKey.
type Source int

const (
	Branches Source = iota
	Remotes
	Reflog
	Worktrees
)

// String renders a Source for logs/tests.
func (s Source) String() string {
	switch s {
	case Branches:
		return "branches"
	case Remotes:
		return "remotes"
	case Reflog:
		return "reflog"
	case Worktrees:
		return "worktrees"
	}
	return "unknown"
}
```

- [ ] **Step 3: Write the failing `Supported` test**

`internal/gitwatch/supported_test.go`:
```go
package gitwatch

import "testing"

func TestV9fsMagicConstant(t *testing.T) {
	// WSL2 drvfs (/mnt/*) mounts report the 9P filesystem magic. Pin it so the
	// drvfs gate can't silently drift.
	if V9fsMagic != 0x01021997 {
		t.Fatalf("V9fsMagic = %#x, want 0x01021997", V9fsMagic)
	}
}

func TestSupportedOnTempDir(t *testing.T) {
	// t.TempDir() is /tmp (ext4 here), a normal local fs → watching is viable.
	if !Supported(t.TempDir()) {
		t.Fatal("Supported(tempdir) = false, want true on a local fs")
	}
}
```

- [ ] **Step 4: Run it to verify it fails**

Run: `go test ./internal/gitwatch/ -run TestSupported -run TestV9fs -v`
Expected: FAIL — `undefined: V9fsMagic`, `undefined: Supported`.

- [ ] **Step 5: Write `supported_linux.go`**

```go
//go:build linux

package gitwatch

import "syscall"

// V9fsMagic is the Linux superblock magic for the 9P (Plan 9) filesystem, which
// WSL2 uses for drvfs mounts under /mnt. inotify does not reliably deliver
// events on such mounts, so file-watching is disabled there.
const V9fsMagic = 0x01021997

// Supported reports whether file-watching is viable for the repository whose git
// common dir is commonDir. It returns false for 9P/v9fs mounts (WSL2 drvfs) and
// true otherwise; a statfs error fails open (returns true) so a normal repo is
// never wrongly disabled.
func Supported(commonDir string) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(commonDir, &st); err != nil {
		return true
	}
	return int64(st.Type) != int64(V9fsMagic)
}
```

- [ ] **Step 6: Write `supported_other.go`**

```go
//go:build !linux

package gitwatch

// V9fsMagic mirrors the Linux constant so cross-platform tests compile; it is
// unused off Linux.
const V9fsMagic = 0x01021997

// Supported reports whether file-watching is viable. On non-Linux platforms
// (macOS kqueue, Windows ReadDirectoryChangesW) the drvfs problem does not
// apply, so watching is always considered viable.
func Supported(commonDir string) bool { return true }
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/gitwatch/ -v`
Expected: PASS (`TestV9fsMagicConstant`, `TestSupportedOnTempDir`).

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum internal/gitwatch/
git commit -m "feat(gitwatch): add fsnotify dep, Source enum, drvfs Supported gate

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_012KeCYxqdcKWUv9Zk9QYKFz"
```

---

### Task 2: `gitwatch.Plan` for non-recursive sources (Reflog, Worktrees)

**Files:**
- Create: `internal/gitwatch/plan.go`
- Test: `internal/gitwatch/plan_test.go`

**Interfaces:**
- Consumes: `gitwatch.Source` (Task 1).
- Produces:
  - `type Group struct { Dir string; Recursive bool; Match func(base string) []Source }`
  - `func Plan(commonDir, worktreeDir string, enabled []Source) []Group`

  D1 handles only `Reflog` and `Worktrees`; `Branches`/`Remotes` in `enabled` are ignored until D2. Paths in returned `Group.Dir` are `filepath.Join`-cleaned. Watch sets:
  - `Reflog` → one group: `Dir = worktreeDir/logs`, `Match` returns `[]Source{Reflog}` for base `"HEAD"`, else `nil`.
  - `Worktrees` → one group: `Dir = commonDir/worktrees`, `Match` returns `[]Source{Worktrees}` for **any** base (a worktree add/remove/lock touches files under it), i.e. always non-nil.

- [ ] **Step 1: Write the failing test**

`internal/gitwatch/plan_test.go`:
```go
package gitwatch

import (
	"path/filepath"
	"testing"
)

// groupFor returns the planned group whose Dir == want, or fails.
func groupFor(t *testing.T, groups []Group, want string) Group {
	t.Helper()
	for _, g := range groups {
		if g.Dir == want {
			return g
		}
	}
	t.Fatalf("no group for dir %q in %v", want, dirsOf(groups))
	return Group{}
}

func dirsOf(groups []Group) []string {
	out := make([]string, len(groups))
	for i, g := range groups {
		out[i] = g.Dir
	}
	return out
}

func hasSource(ss []Source, want Source) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestPlanReflogWatchesLogsHEAD(t *testing.T) {
	c := filepath.Join("/repo", ".git")
	w := filepath.Join("/repo", ".git", "worktrees", "wt1")
	groups := Plan(c, w, []Source{Reflog})
	g := groupFor(t, groups, filepath.Join(w, "logs"))
	if !hasSource(g.Match("HEAD"), Reflog) {
		t.Error("logs/HEAD change should affect Reflog")
	}
	if g.Match("ORIG_HEAD") != nil {
		t.Error("logs/ORIG_HEAD must not affect Reflog")
	}
	if g.Recursive {
		t.Error("reflog group must be non-recursive")
	}
}

func TestPlanWorktreesWatchesWorktreesDir(t *testing.T) {
	c := filepath.Join("/repo", ".git")
	groups := Plan(c, c, []Source{Worktrees})
	g := groupFor(t, groups, filepath.Join(c, "worktrees"))
	if !hasSource(g.Match("anything"), Worktrees) {
		t.Error("any change under worktrees/ should affect Worktrees")
	}
}

func TestPlanIgnoresUnimplementedSourcesInD1(t *testing.T) {
	c := filepath.Join("/repo", ".git")
	groups := Plan(c, c, []Source{Branches, Remotes})
	if len(groups) != 0 {
		t.Errorf("D1 Plan must ignore Branches/Remotes, got %v", dirsOf(groups))
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/gitwatch/ -run TestPlan -v`
Expected: FAIL — `undefined: Plan`, `undefined: Group`.

- [ ] **Step 3: Write `plan.go`**

```go
package gitwatch

import "path/filepath"

// Group is one watched directory plus the predicate deciding which sources a
// change to a given basename within it affects. Events on a directory are
// non-recursive unless Recursive is set (handled by the Watcher in D2).
type Group struct {
	Dir       string
	Recursive bool
	Match     func(base string) []Source
}

// Plan returns the directories to watch for the enabled sources, each with the
// predicate mapping a changed basename to the affected sources. It is pure: all
// .git-layout knowledge lives here. commonDir is the git common dir ($C);
// worktreeDir is the per-worktree git dir ($W) — equal to $C in the main
// worktree. Sources not yet implemented (Branches/Remotes in D1) are ignored.
func Plan(commonDir, worktreeDir string, enabled []Source) []Group {
	var groups []Group
	for _, s := range enabled {
		switch s {
		case Reflog:
			groups = append(groups, Group{
				Dir: filepath.Join(worktreeDir, "logs"),
				Match: func(base string) []Source {
					if base == "HEAD" {
						return []Source{Reflog}
					}
					return nil
				},
			})
		case Worktrees:
			groups = append(groups, Group{
				Dir:   filepath.Join(commonDir, "worktrees"),
				Match: func(base string) []Source { return []Source{Worktrees} },
			})
		}
	}
	return groups
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/gitwatch/ -run TestPlan -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/gitwatch/plan.go internal/gitwatch/plan_test.go
git commit -m "feat(gitwatch): Plan path→source map for reflog and worktrees

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_012KeCYxqdcKWUv9Zk9QYKFz"
```

---

### Task 3: `gitwatch.Watcher` over non-recursive groups (debounced)

**Files:**
- Create: `internal/gitwatch/watcher.go`
- Test: `internal/gitwatch/watcher_test.go`

**Interfaces:**
- Consumes: `Group`, `Source`, `Plan` (Tasks 1–2).
- Produces:
  - `func New(groups []Group, debounce time.Duration) (*Watcher, error)`
  - `func (w *Watcher) Events() <-chan Source`
  - `func (w *Watcher) Close() error`

  Behavior: watches each `Group.Dir` (skips a dir that does not exist — not fatal). On an fsnotify event, take the base name of the event path, run the group's `Match`; for each returned `Source`, schedule a debounced emission. `Events()` delivers each debounced `Source` once per quiet window. `Close` stops fsnotify, halts timers, and closes the events channel. Recursive groups are watched only at their top dir in D1 (D2 extends).

- [ ] **Step 1: Write the failing live test**

`internal/gitwatch/watcher_test.go`:
```go
package gitwatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// waitSource waits up to d for src on the watcher's channel.
func waitSource(t *testing.T, w *Watcher, src Source, d time.Duration) {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case got, ok := <-w.Events():
			if !ok {
				t.Fatalf("events channel closed before %v", src)
			}
			if got == src {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %v", src)
		}
	}
}

func TestWatcherFiresOnReflogWrite(t *testing.T) {
	dir := t.TempDir() // ext4 /tmp → inotify works
	logs := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := New(Plan(dir, dir, []Source{Reflog}), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	// Write logs/HEAD after the watch is established.
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(logs, "HEAD"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitSource(t, w, Reflog, 2*time.Second)
}

func TestWatcherCloseClosesChannel(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "worktrees"), 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := New(Plan(dir, dir, []Source{Worktrees}), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := <-w.Events(); ok {
		t.Error("events channel should be closed after Close")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/gitwatch/ -run TestWatcher -v`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Write `watcher.go`**

```go
package gitwatch

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher watches a set of planned groups and emits a debounced Source whenever
// the .git files backing that source change. It is safe to Close once; the
// events channel is closed on shutdown.
type Watcher struct {
	fsw    *fsnotify.Watcher
	groups []Group
	out    chan Source

	debounce time.Duration
	mu       sync.Mutex
	timers   map[Source]*time.Timer

	done   chan struct{}
	closed bool
}

// New builds a Watcher over groups and starts its event loop. A group whose Dir
// does not exist is skipped (not an error) — a repo may have no worktrees/ or
// logs/ yet. debounce coalesces a burst of events per source into one emission.
func New(groups []Group, debounce time.Duration) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		fsw:      fsw,
		groups:   groups,
		out:      make(chan Source, 16),
		debounce: debounce,
		timers:   map[Source]*time.Timer{},
		done:     make(chan struct{}),
	}
	for _, g := range groups {
		if _, statErr := os.Stat(g.Dir); statErr != nil {
			continue // dir absent — nothing to watch yet
		}
		_ = fsw.Add(g.Dir) // best-effort; a failed add just means no events from it
	}
	go w.loop()
	return w, nil
}

// Events delivers debounced source changes. It is closed when the Watcher stops.
func (w *Watcher) Events() <-chan Source { return w.out }

// loop translates fsnotify events into debounced Source emissions.
func (w *Watcher) loop() {
	defer close(w.out)
	for {
		select {
		case <-w.done:
			return
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.handle(ev)
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			// fsnotify error: ignore the individual error; the lane still serves
			// manual refresh. A persistent failure degrades to interval/manual.
		}
	}
}

// handle maps one event to its sources and schedules debounced emissions.
func (w *Watcher) handle(ev fsnotify.Event) {
	base := filepath.Base(ev.Name)
	dir := filepath.Dir(ev.Name)
	for _, g := range w.groups {
		if g.Dir != dir {
			continue
		}
		for _, s := range g.Match(base) {
			w.schedule(s)
		}
	}
}

// schedule (re)arms the per-source debounce timer; the source is emitted once
// the quiet window elapses.
func (w *Watcher) schedule(s Source) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	if t := w.timers[s]; t != nil {
		t.Stop()
	}
	src := s
	w.timers[s] = time.AfterFunc(w.debounce, func() {
		select {
		case w.out <- src:
		case <-w.done:
		}
	})
}

// Close stops watching and closes the events channel. Safe to call once.
func (w *Watcher) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	for _, t := range w.timers {
		t.Stop()
	}
	w.mu.Unlock()
	close(w.done)
	return w.fsw.Close()
}
```

Note: `loop` ends and `close(w.out)` runs when `w.fsw.Close()` causes `fsw.Events` to close (or `done` is observed). The `<-w.done` case guarantees prompt shutdown.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/gitwatch/ -run TestWatcher -v`
Expected: PASS. If `TestWatcherFiresOnReflogWrite` flakes, raise the wait to 3s — `/tmp` inotify is fast but CI load varies.

- [ ] **Step 5: Run the whole package with race**

Run: `go test -race ./internal/gitwatch/ -v`
Expected: PASS, no race (timers + close are mutex-guarded).

- [ ] **Step 6: Commit**

```bash
git add internal/gitwatch/watcher.go internal/gitwatch/watcher_test.go
git commit -m "feat(gitwatch): debounced fsnotify Watcher over planned groups

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_012KeCYxqdcKWUv9Zk9QYKFz"
```

---

### Task 4: Config — watch bools, overlay, `SetRefreshWatch`, settingDocs

**Files:**
- Modify: `internal/config/config.go` (`RefreshConfig`, `overlayRefresh`)
- Modify: `internal/config/write.go` (`SetRefreshWatch`)
- Modify: `internal/config/template.go` (`settingDocs`)
- Test: `internal/config/config_test.go`, `internal/config/write_test.go`

**Interfaces:**
- Produces:
  - `RefreshConfig` fields `WorktreesWatch, BranchesWatch, ReflogWatch, RemotesWatch bool` (TOML `worktrees_watch`, `branches_watch`, `reflog_watch`, `remotes_watch`).
  - `func SetRefreshWatch(path, source string, on bool) error` — writes `[refresh] <source>_watch = <bool>` to `path` (the repo `.gg.toml`). `source` is the bare source key (`"worktrees"`, etc.); the function appends `_watch`.

- [ ] **Step 1: Write the failing config tests**

Append to `internal/config/config_test.go`:
```go
func TestOverlayRefreshWatchBools(t *testing.T) {
	base := Defaults()
	layer := Config{Refresh: RefreshConfig{WorktreesWatch: true, ReflogWatch: true}}
	overlayRefresh(&base.Refresh, layer.Refresh)
	if !base.Refresh.WorktreesWatch || !base.Refresh.ReflogWatch {
		t.Fatal("watch bools should overlay when true")
	}
	if base.Refresh.BranchesWatch || base.Refresh.RemotesWatch {
		t.Fatal("unset watch bools must stay false")
	}
}
```

Append to `internal/config/write_test.go`:
```go
func TestSetRefreshWatchRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gg.toml")
	if err := SetRefreshWatch(path, "worktrees", true); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load("", path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Refresh.WorktreesWatch {
		t.Fatal("worktrees_watch did not round-trip to true")
	}
}

func TestSetRefreshWatchPreservesOtherKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gg.toml")
	if err := SetRefreshInterval(path, "worktrees", 30); err != nil {
		t.Fatal(err)
	}
	if err := SetRefreshWatch(path, "worktrees", true); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load("", path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Refresh.Worktrees != 30 || !cfg.Refresh.WorktreesWatch {
		t.Fatalf("interval=%d watch=%v; want 30/true", cfg.Refresh.Worktrees, cfg.Refresh.WorktreesWatch)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/config/ -run 'TestOverlayRefreshWatch|TestSetRefreshWatch' -v`
Expected: FAIL — `WorktreesWatch` / `SetRefreshWatch` undefined.

- [ ] **Step 3: Add the struct fields**

In `internal/config/config.go`, extend `RefreshConfig` (after the `MinSeconds` field, before the closing brace):
```go
	// Per-source file-watch toggles (Phase D). When true AND the repo's fs
	// supports inotify (not WSL2 9p drvfs), the source refreshes on .git file
	// change instead of on its interval; otherwise it falls back to the interval.
	WorktreesWatch bool `toml:"worktrees_watch"`
	BranchesWatch  bool `toml:"branches_watch"`
	ReflogWatch    bool `toml:"reflog_watch"`
	RemotesWatch   bool `toml:"remotes_watch"`
```

- [ ] **Step 4: Overlay the bools**

In `overlayRefresh` (after the `MinSeconds` block), add (normal polarity, default false → only true overlays, matching `Enabled`):
```go
	if src.WorktreesWatch {
		dst.WorktreesWatch = true
	}
	if src.BranchesWatch {
		dst.BranchesWatch = true
	}
	if src.ReflogWatch {
		dst.ReflogWatch = true
	}
	if src.RemotesWatch {
		dst.RemotesWatch = true
	}
```

- [ ] **Step 5: Add `SetRefreshWatch`**

In `internal/config/write.go`, after `SetRefreshInterval`:
```go
// SetRefreshWatch persists `[refresh] <source>_watch = <bool>` to the given
// config file (the repo .gg.toml), preserving the rest of the file. Backs the
// Refresh-rates editor's per-source file-watch toggle. source is the bare key
// (e.g. "worktrees"); "_watch" is appended.
func SetRefreshWatch(path, source string, on bool) error {
	return setScalarLine(path, "refresh", source+"_watch", strconv.FormatBool(on))
}
```

- [ ] **Step 6: Document the keys in `settingDocs`**

Open `internal/config/template.go`, find the `[refresh]` `settingDocs` entries (the same registry that documents `worktrees`, `min_seconds`, etc.), and add four rows alongside them, matching the existing struct/format used there, e.g.:
```go
	{section: "refresh", key: "worktrees_watch", value: "false", comment: "refresh worktrees on .git file change (off → use interval); ignored on WSL2 9p mounts"},
	{section: "refresh", key: "branches_watch", value: "false", comment: "refresh branches on ref change (Phase D2)"},
	{section: "refresh", key: "reflog_watch", value: "false", comment: "refresh reflog on logs/HEAD change"},
	{section: "refresh", key: "remotes_watch", value: "false", comment: "refresh remotes on ref/FETCH_HEAD change (Phase D2)"},
```
(Match the actual `settingDoc` field names in this file — read the existing `[refresh]` rows first and mirror them exactly. The literals above are the intended content.)

- [ ] **Step 7: Run the config tests**

Run: `go test ./internal/config/ -v`
Expected: PASS, including the two new tests and the existing `gg config init`/`populate` template tests (which now include the four keys).

- [ ] **Step 8: Commit**

```bash
git add internal/config/
git commit -m "feat(config): per-source [refresh] *_watch bools + SetRefreshWatch writer

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_012KeCYxqdcKWUv9Zk9QYKFz"
```

---

### Task 5: Scheduler — `watchActive` skip in `dueItems`

**Files:**
- Modify: `internal/tui/refresh.go` (`dueItems` signature + `watchActive` + eligibility)
- Modify: `internal/tui/model.go:1487` (refreshTick call already passes `time.Now()`; `dueItems` is called inside `refreshTick`, so update there)
- Test: `internal/tui/refresh_test.go`

**Interfaces:**
- Produces:
  - `func watchEligible(it refreshItem) bool` — true only for sources the implemented `Plan` covers. **D1: worktrees, reflog.** (D2 adds branches, remotes.)
  - `func watchOn(cfg config.RefreshConfig, it refreshItem) bool` — the source's `*_watch` config bool.
  - `func watchActive(cfg config.RefreshConfig, watchSupported bool, it refreshItem) bool` = `watchEligible(it) && watchOn(cfg, it) && watchSupported`.
  - `dueItems(now, lastRun, cfg, watchSupported bool, suppressed bool)` — **new `watchSupported` param**; skips items where `watchActive(...)` is true.

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/refresh_test.go`:
```go
func TestWatchActiveTruthTable(t *testing.T) {
	cfg := config.RefreshConfig{WorktreesWatch: true, BranchesWatch: true}
	wt := refreshItem{source: srcWorktrees}
	br := refreshItem{source: srcBranches}
	st := refreshItem{source: srcStatus}
	// worktrees: eligible (D1) + on + supported → active
	if !watchActive(cfg, true, wt) {
		t.Error("worktrees watch should be active when on+supported")
	}
	// unsupported fs → not active (falls back to polling)
	if watchActive(cfg, false, wt) {
		t.Error("worktrees watch must be inactive on unsupported fs")
	}
	// branches: NOT eligible in D1 even though branches_watch=true
	if watchActive(cfg, true, br) {
		t.Error("branches must not be watch-active in D1 (not yet implemented)")
	}
	// status: never eligible
	if watchActive(cfg, true, st) {
		t.Error("status is never watch-eligible")
	}
}

func TestDueItemsSkipsWatchActive(t *testing.T) {
	cfg := config.RefreshConfig{Enabled: true, Worktrees: 30, WorktreesWatch: true, MinSeconds: 10}
	last := map[refreshItem]time.Time{} // nothing seen → everything otherwise due
	// supported → worktrees is watch-active → must NOT be due via the timer
	due := dueItems(time.Now(), last, cfg, true, false)
	for _, it := range due {
		if it.source == srcWorktrees && !it.isFetch {
			t.Error("watch-active worktrees must not be polled")
		}
	}
	// unsupported → worktrees falls back to interval polling → IS due
	due = dueItems(time.Now(), last, cfg, false, false)
	found := false
	for _, it := range due {
		if it.source == srcWorktrees {
			found = true
		}
	}
	if !found {
		t.Error("on unsupported fs, watch-on worktrees must poll at its interval")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run 'TestWatchActive|TestDueItemsSkipsWatch' -v`
Expected: FAIL — `watchActive` undefined, `dueItems` arg count wrong.

- [ ] **Step 3: Add the helpers in `refresh.go`**

After `refreshTomlKey`:
```go
// watchEligible reports whether a file-watcher actually covers this item. Only
// sources the implemented gitwatch.Plan watches are eligible — so a config that
// sets a *_watch bool for a not-yet-wired source still polls (never goes stale).
// D1: worktrees, reflog. (D2 adds branches, remotes.)
func watchEligible(it refreshItem) bool {
	if it.isFetch {
		return false
	}
	switch it.source {
	case srcWorktrees, srcReflog:
		return true
	}
	return false
}

// watchOn reports the source's [refresh] *_watch config bool.
func watchOn(cfg config.RefreshConfig, it refreshItem) bool {
	switch it.source {
	case srcWorktrees:
		return cfg.WorktreesWatch
	case srcBranches:
		return cfg.BranchesWatch
	case srcReflog:
		return cfg.ReflogWatch
	case srcRemotes:
		return cfg.RemotesWatch
	}
	return false
}

// watchActive reports whether this source is currently driven by the file
// watcher (eligible AND toggled on AND the fs supports watching). Watch-active
// sources are excluded from interval polling — the watcher triggers them.
func watchActive(cfg config.RefreshConfig, watchSupported bool, it refreshItem) bool {
	return watchEligible(it) && watchOn(cfg, it) && watchSupported
}
```

- [ ] **Step 4: Change `dueItems` to skip watch-active items**

Replace the `dueItems` signature/body:
```go
func dueItems(now time.Time, lastRun map[refreshItem]time.Time, cfg config.RefreshConfig, watchSupported bool, suppressed bool) []refreshItem {
	if !cfg.Enabled || suppressed {
		return nil
	}
	var due []refreshItem
	for _, it := range scheduledItems {
		if watchActive(cfg, watchSupported, it) {
			continue // driven by the file watcher, not the timer
		}
		secs, on := scheduledInterval(cfg, it)
		if !on {
			continue
		}
		last, seen := lastRun[it]
		if !seen || now.Sub(last) >= time.Duration(secs)*time.Second {
			due = append(due, it)
		}
	}
	return due
}
```

- [ ] **Step 5: Update the `refreshTick` call site**

In `refresh.go` `refreshTick`, change the `dueItems` call to pass `m.watchSupported`:
```go
	due := dueItems(now, m.refreshLastRun, m.cfg.Refresh, m.watchSupported, false)
```
(`m.watchSupported` is added to the Model in Task 7; until then this won't compile — so Task 5 and Task 7 land together if needed. To keep Task 5 self-contained, add the `watchSupported bool` field to the Model struct in this task with a one-line declaration, and Task 7 wires its assignment.)

Add to `internal/tui/model.go` Model struct (near `repoConfigPath`):
```go
	watchSupported bool // gitwatch.Supported(commonDir); false on WSL2 9p → watch sources fall back to polling
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/tui/ -run 'TestWatchActive|TestDueItems' -v`
Expected: PASS. Also run `go build ./...` to confirm the new field + signature compile.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/refresh.go internal/tui/refresh_test.go internal/tui/model.go
git commit -m "feat(tui): exclude watch-active sources from interval polling

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_012KeCYxqdcKWUv9Zk9QYKFz"
```

---

### Task 6: Domain `GitDir` query (per-worktree git dir, `$W`)

**Files:**
- Modify: `internal/git/worktree.go` (add `GitDir` verb)
- Modify: `internal/domain/query.go` (add `GitDir` query)
- Test: `internal/git/worktree_verbs_test.go`, `internal/domain/query_test.go`

**Interfaces:**
- Produces:
  - `func (r *Repo) GitDir(ctx context.Context) (string, error)` — `git rev-parse --absolute-git-dir` (the per-worktree `.git` dir; equals the common dir in the main worktree, a `…/worktrees/<name>` path in a linked worktree).
  - `func (s *Service) GitDir(ctx context.Context) (string, error)` — gated Read query named `"gitdir"`.

- [ ] **Step 1: Write the failing verb test**

Append to `internal/git/worktree_verbs_test.go`:
```go
func TestGitDirIsAbsolute(t *testing.T) {
	repo := newTestRepo(t) // existing helper: real git in a temp dir
	gd, err := repo.GitDir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(gd) {
		t.Errorf("GitDir = %q, want absolute", gd)
	}
}
```
(Use the same repo-construction helper the sibling tests in this file use; mirror `TestGitCommonDirIsAbsolute`.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/git/ -run TestGitDirIsAbsolute -v`
Expected: FAIL — `repo.GitDir` undefined.

- [ ] **Step 3: Add the verb**

In `internal/git/worktree.go`, after `GitCommonDir`:
```go
// GitDir returns the absolute path of THIS worktree's git directory
// (`git rev-parse --path-format=absolute --absolute-git-dir`). For the main
// worktree this equals GitCommonDir; for a linked worktree it is the
// per-worktree dir under <common>/worktrees/<name>, which holds this worktree's
// HEAD and logs/HEAD.
func (r *Repo) GitDir(ctx context.Context) (string, error) {
	argv := gitcmd.New("rev-parse").Arg("--path-format=absolute", "--absolute-git-dir").ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-parse (git-dir)", argv)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
```
(`--absolute-git-dir` already prints an absolute path; `--path-format=absolute` is belt-and-braces and matches `GitCommonDir`'s form.)

- [ ] **Step 4: Add the domain query**

In `internal/domain/query.go`, after `GitCommonDir`:
```go
// GitDir returns this worktree's git dir path, under a Read reservation.
func (s *Service) GitDir(ctx context.Context) (string, error) {
	return query(ctx, s, "gitdir", s.repo.GitDir)
}
```

- [ ] **Step 5: Write the failing domain test**

Append to `internal/domain/query_test.go` (mirror `TestGitCommonDirGatedQuery`):
```go
func TestGitDirGatedQuery(t *testing.T) {
	svc := newTestService(t) // same helper TestGitCommonDirGatedQuery uses
	gd, err := svc.GitDir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(gd) {
		t.Errorf("GitDir = %q, want absolute", gd)
	}
}
```
(Match the exact construction helper used by the neighboring gated-query tests.)

- [ ] **Step 6: Run both tests**

Run: `go test ./internal/git/ ./internal/domain/ -run GitDir -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/git/worktree.go internal/git/worktree_verbs_test.go internal/domain/query.go internal/domain/query_test.go
git commit -m "feat(git,domain): GitDir verb + query (per-worktree git dir)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_012KeCYxqdcKWUv9Zk9QYKFz"
```

---

### Task 7: TUI wiring — build watcher, deliver events into the lane, lifecycle

**Files:**
- Create: `internal/tui/watch.go`
- Modify: `internal/tui/source.go` (source map, `enabledWatchSources`)
- Modify: `internal/tui/model.go` (fields, `configReadyMsg` batch, `watch*Msg` cases, `reRoot` rebuild)
- Test: `internal/tui/watch_test.go`

**Interfaces:**
- Consumes: `gitwatch.{Source,Plan,Supported,New,Watcher}`, `domain.{GitCommonDir,GitDir}`, `enqueueDue`, `watchEligible/watchOn`.
- Produces:
  - Model fields: `watcher *gitwatch.Watcher`, `watchGen int` (watchSupported added in Task 5).
  - `func watchSourceKey(s gitwatch.Source) sourceKey` — `Branches→srcBranches`, `Remotes→srcRemotes`, `Reflog→srcReflog`, `Worktrees→srcWorktrees`.
  - `func enabledWatchSources(cfg config.RefreshConfig) []gitwatch.Source` — the watch-eligible (`watchEligible`) sources whose `*_watch` bool is on. D1 yields a subset of `{Reflog, Worktrees}`.
  - `type watchReadyMsg struct { gen int; watcher *gitwatch.Watcher; supported bool }`
  - `type watchEventMsg struct { gen int; source sourceKey }`
  - `type watchClosedMsg struct { gen int }`
  - `func (m Model) startWatchCmd(gen int) tea.Cmd` — off-thread: queries common+git dir, computes `Supported`, builds the watcher for `enabledWatchSources`, returns `watchReadyMsg`.
  - `func watchListenCmd(w *gitwatch.Watcher, gen int) tea.Cmd` — blocks on `w.Events()`, returns `watchEventMsg` (or `watchClosedMsg` when the channel closes).

- [ ] **Step 1: Write `internal/tui/watch.go`**

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/gitwatch"
)

// watchReadyMsg carries a freshly-built watcher (or a nil watcher when watching
// is unsupported / no sources are enabled). gen drops a build superseded by a
// repo switch or a toggle.
type watchReadyMsg struct {
	gen       int
	watcher   *gitwatch.Watcher
	supported bool
}

// watchEventMsg is one debounced file-change for a source. gen ties it to the
// current watcher; a stale gen (after rebuild) is ignored.
type watchEventMsg struct {
	gen    int
	source sourceKey
}

// watchClosedMsg lands when a watcher's events channel closes (after Close).
type watchClosedMsg struct{ gen int }

// watchSourceKey maps a gitwatch.Source to the TUI's sourceKey.
func watchSourceKey(s gitwatch.Source) sourceKey {
	switch s {
	case gitwatch.Branches:
		return srcBranches
	case gitwatch.Remotes:
		return srcRemotes
	case gitwatch.Reflog:
		return srcReflog
	case gitwatch.Worktrees:
		return srcWorktrees
	}
	return srcStatus // unreachable; status is never watch-eligible
}

// enabledWatchSources returns the gitwatch sources to watch: those that are both
// watch-eligible (implemented) and toggled on in config.
func enabledWatchSources(cfg config.RefreshConfig) []gitwatch.Source {
	var out []gitwatch.Source
	add := func(it refreshItem, s gitwatch.Source) {
		if watchEligible(it) && watchOn(cfg, it) {
			out = append(out, s)
		}
	}
	add(refreshItem{source: srcWorktrees}, gitwatch.Worktrees)
	add(refreshItem{source: srcReflog}, gitwatch.Reflog)
	add(refreshItem{source: srcBranches}, gitwatch.Branches)
	add(refreshItem{source: srcRemotes}, gitwatch.Remotes)
	return out
}

// startWatchCmd builds a watcher off the UI thread. It resolves the common and
// per-worktree git dirs, computes Supported, and — if supported and at least one
// source is enabled — constructs the watcher. Always returns a watchReadyMsg
// (watcher may be nil).
func (m Model) startWatchCmd(gen int) tea.Cmd {
	svc := m.svc
	cfg := m.cfg.Refresh
	return func() tea.Msg {
		ctx := context.Background()
		common, err := svc.GitCommonDir(ctx)
		if err != nil || common == "" {
			return watchReadyMsg{gen: gen, supported: false}
		}
		supported := gitwatch.Supported(common)
		if !supported {
			return watchReadyMsg{gen: gen, supported: false}
		}
		enabled := enabledWatchSources(cfg)
		if len(enabled) == 0 {
			return watchReadyMsg{gen: gen, supported: true}
		}
		worktreeDir, err := svc.GitDir(ctx)
		if err != nil || worktreeDir == "" {
			worktreeDir = common // fall back; reflog HEAD lives at common in the main worktree
		}
		w, werr := gitwatch.New(gitwatch.Plan(common, worktreeDir, enabled), watchDebounce)
		if werr != nil {
			return watchReadyMsg{gen: gen, supported: true} // nil watcher → polling fallback
		}
		return watchReadyMsg{gen: gen, watcher: w, supported: true}
	}
}

// watchListenCmd blocks until the watcher emits a source, then returns it. When
// the channel closes (watcher stopped), it returns watchClosedMsg so the loop
// ends cleanly.
func watchListenCmd(w *gitwatch.Watcher, gen int) tea.Cmd {
	return func() tea.Msg {
		s, ok := <-w.Events()
		if !ok {
			return watchClosedMsg{gen: gen}
		}
		return watchEventMsg{gen: gen, source: watchSourceKey(s)}
	}
}
```

Add the imports `"context"` and `"time"` are needed; add a debounce const. In `watch.go` add:
```go
import "context"
```
and a const (top of file, after imports):
```go
const watchDebounce = 200 * time.Millisecond
```
(Adjust the import block to include `context` and `time`.)

- [ ] **Step 2: Add Model fields**

In `internal/tui/model.go` Model struct, near `watchSupported` (added in Task 5):
```go
	watcher  *gitwatch.Watcher // file-watcher; nil when unsupported or no sources enabled
	watchGen int               // bumped per (re)build; stale watch msgs are dropped
```
Add the import `"github.com/homeend/gigagit/internal/gitwatch"` to model.go.

- [ ] **Step 3: Kick off the watcher after config loads**

In the `configReadyMsg` handler (model.go ~467), after `m, cmd = m.reloadAllCmd(true, true)`, batch in the watcher build:
```go
	case configReadyMsg:
		m.cfg = msg.cfg
		m.repoConfigPath = msg.repoTOML
		now := time.Now()
		for _, it := range scheduledItems {
			m.refreshLastRun[it] = now
		}
		var cmd tea.Cmd
		m, cmd = m.reloadAllCmd(true, true)
		m.watchGen++
		return m, tea.Batch(cmd, m.startWatchCmd(m.watchGen))
```

- [ ] **Step 4: Handle the watcher messages**

Add cases to the `Update` switch (next to the other refresh cases, e.g. after the `heartbeatMsg` case):
```go
	case watchReadyMsg:
		if msg.gen != m.watchGen {
			if msg.watcher != nil {
				_ = msg.watcher.Close() // superseded build; discard
			}
			return m, nil
		}
		m.watchSupported = msg.supported
		m.watcher = msg.watcher
		if m.watcher == nil {
			return m, nil
		}
		return m, watchListenCmd(m.watcher, m.watchGen)
	case watchEventMsg:
		if msg.gen != m.watchGen || m.watcher == nil {
			return m, nil // stale watcher
		}
		// Only enqueue if the source is still watch-active (toggle could have
		// flipped). Then re-arm the listener.
		if watchActive(m.cfg.Refresh, m.watchSupported, refreshItem{source: msg.source}) {
			m.bgQueue = enqueueDue(m.bgQueue, m.bgActiveItem, m.bgBusy, []refreshItem{{source: msg.source}})
		}
		return m, watchListenCmd(m.watcher, m.watchGen)
	case watchClosedMsg:
		// Channel closed (Close called). Nothing to re-arm; a rebuild issues a
		// fresh listener with a new gen.
		return m, nil
```

- [ ] **Step 5: Rebuild on repo switch**

In `reRoot` (model.go ~2159), before `m.svc = domain.OpenTUI(path)`, close the old watcher and bump the gen so any in-flight listener/build is dropped:
```go
	if m.watcher != nil {
		_ = m.watcher.Close()
		m.watcher = nil
	}
	m.watchGen++
	m.watchSupported = false
```
The new repo's watcher is built by the `configReadyMsg`/load path? `reRoot` uses `loadCmd`, not `bootstrapCmd`. So also append `m.startWatchCmd(m.watchGen)` to reRoot's returned command:
```go
	m.loadGen++
	return m, tea.Batch(m.loadCmd(), m.startWatchCmd(m.watchGen))
```
(`startWatchCmd` reads `m.svc`, which is the NEW svc set above — correct, because it's captured when the closure is built after the reassignment.)

- [ ] **Step 6: Write the handler test**

`internal/tui/watch_test.go`:
```go
package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/gitwatch"
)

func TestWatchSourceKeyMapping(t *testing.T) {
	cases := map[gitwatch.Source]sourceKey{
		gitwatch.Worktrees: srcWorktrees,
		gitwatch.Reflog:    srcReflog,
		gitwatch.Branches:  srcBranches,
		gitwatch.Remotes:   srcRemotes,
	}
	for in, want := range cases {
		if got := watchSourceKey(in); got != want {
			t.Errorf("watchSourceKey(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestEnabledWatchSourcesD1(t *testing.T) {
	cfg := config.RefreshConfig{WorktreesWatch: true, ReflogWatch: true, BranchesWatch: true}
	got := enabledWatchSources(cfg)
	// D1: worktrees+reflog eligible; branches not yet eligible → excluded.
	if len(got) != 2 {
		t.Fatalf("got %v, want exactly worktrees+reflog", got)
	}
}

func TestWatchEventEnqueuesWhenActive(t *testing.T) {
	m := newTestModel(t) // existing helper that builds a usable Model; adjust to repo's helper
	m.cfg.Refresh = config.RefreshConfig{Enabled: true, WorktreesWatch: true}
	m.watchSupported = true
	m.watchGen = 1
	// Simulate a watcher present so the handler doesn't early-return.
	// (watcher pointer non-nil is required; a zero-value Watcher is fine for this
	// branch because watchListenCmd is the returned cmd, not invoked here.)
	m.watcher = &gitwatch.Watcher{}
	m2, _ := m.Update(watchEventMsg{gen: 1, source: srcWorktrees})
	mm := m2.(Model)
	found := false
	for _, it := range mm.bgQueue {
		if it.source == srcWorktrees {
			found = true
		}
	}
	if !found {
		t.Error("watchEventMsg should enqueue an active source into bgQueue")
	}
}

func TestWatchEventIgnoresStaleGen(t *testing.T) {
	m := newTestModel(t)
	m.cfg.Refresh = config.RefreshConfig{Enabled: true, WorktreesWatch: true}
	m.watchSupported = true
	m.watchGen = 2
	m.watcher = &gitwatch.Watcher{}
	m2, _ := m.Update(watchEventMsg{gen: 1, source: srcWorktrees}) // stale
	if len(m2.(Model).bgQueue) != 0 {
		t.Error("stale-gen watch event must be ignored")
	}
}
```
**Implementer note:** use the package's actual Model test constructor (grep `func newTestModel`/`func newModel` in `internal/tui/*_test.go`); if none exists, build the minimal Model literal the other tests use, ensuring `srcInflight`/`srcGen`/`refreshLastRun` maps are initialized as those tests do. The `gitwatch.Watcher{}` zero value is only used as a non-nil sentinel — its methods are not called on this path.

- [ ] **Step 7: Run the tests + race**

Run: `go test ./internal/tui/ -run 'TestWatch|TestEnabledWatch' -v` then `go test -race ./internal/tui/ -run TestWatch -v`
Expected: PASS.

- [ ] **Step 8: Build the binary**

Run: `go build -o ./gg ./cmd/gg`
Expected: builds clean. (Manual smoke: see Task 9.)

- [ ] **Step 9: Commit**

```bash
git add internal/tui/watch.go internal/tui/watch_test.go internal/tui/source.go internal/tui/model.go
git commit -m "feat(tui): file-watcher lifecycle + deliver events into the refresh lane

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_012KeCYxqdcKWUv9Zk9QYKFz"
```

---

### Task 8: Refresh-rates editor — watch toggle (`w`)

**Files:**
- Modify: `internal/tui/settings_popup.go` (render column + `w` key + help line)
- Test: `internal/tui/settings_popup_test.go` (or the file holding the rates-editor tests)

**Interfaces:**
- Consumes: `watchEligible`, `watchOn`, `m.watchSupported`, `config.SetRefreshWatch`, `enabledWatchSources`, `startWatchCmd`.
- Produces:
  - `func (m Model) toggleRefreshWatch(it refreshItem) (Model, tea.Cmd)` — flips the source's `*_watch` config bool, persists via `config.SetRefreshWatch` to `m.repoConfigPath`, reseeds `refreshLastRun[it]`, rebuilds the watcher (`m.watchGen++` + `startWatchCmd`), and returns the rebuild cmd. No-op (returns `m, nil`) for a non-watch-eligible row.

- [ ] **Step 1: Write the failing test**

Append to the rates-editor test file:
```go
func TestToggleRefreshWatchFlipsEligible(t *testing.T) {
	m := newTestModel(t)
	m.repoConfigPath = "" // no write; in-memory flip still applies (matches saveRefreshInterval)
	m.cfg.Refresh = config.RefreshConfig{}
	m2, _ := m.toggleRefreshWatch(refreshItem{source: srcWorktrees})
	if !m2.cfg.Refresh.WorktreesWatch {
		t.Error("toggle should set WorktreesWatch true")
	}
	m3, _ := m2.toggleRefreshWatch(refreshItem{source: srcWorktrees})
	if m3.cfg.Refresh.WorktreesWatch {
		t.Error("second toggle should clear WorktreesWatch")
	}
}

func TestToggleRefreshWatchIgnoresIneligible(t *testing.T) {
	m := newTestModel(t)
	before := m.cfg.Refresh
	m2, cmd := m.toggleRefreshWatch(refreshItem{source: srcStatus})
	if m2.cfg.Refresh != before || cmd != nil {
		t.Error("status row must not toggle a watch bool")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run TestToggleRefreshWatch -v`
Expected: FAIL — `toggleRefreshWatch` undefined.

- [ ] **Step 3: Implement `toggleRefreshWatch`**

In `settings_popup.go` (or `refresh.go` near `saveRefreshInterval` — keep it with the other persisters; put it in `refresh.go`):
```go
// toggleRefreshWatch flips a source's file-watch toggle, persists it to the repo
// .gg.toml, reseeds its lastRun, and rebuilds the watcher. No-op for a source
// that is not watch-eligible.
func (m Model) toggleRefreshWatch(it refreshItem) (Model, tea.Cmd) {
	if !watchEligible(it) {
		return m, nil
	}
	want := !watchOn(m.cfg.Refresh, it)
	setRefreshWatchField(&m.cfg.Refresh, it, want)
	if m.refreshLastRun == nil {
		m.refreshLastRun = map[refreshItem]time.Time{}
	}
	m.refreshLastRun[it] = time.Now()
	if m.repoConfigPath != "" {
		if err := config.SetRefreshWatch(m.repoConfigPath, refreshTomlKey(it), want); err != nil {
			m.statusMsg = "watch toggled but not saved: " + err.Error()
		}
	}
	m.watchGen++
	if m.watcher != nil {
		_ = m.watcher.Close()
		m.watcher = nil
	}
	return m, m.startWatchCmd(m.watchGen)
}

// setRefreshWatchField writes want into the *_watch field for it.
func setRefreshWatchField(cfg *config.RefreshConfig, it refreshItem, want bool) {
	switch it.source {
	case srcWorktrees:
		cfg.WorktreesWatch = want
	case srcBranches:
		cfg.BranchesWatch = want
	case srcReflog:
		cfg.ReflogWatch = want
	case srcRemotes:
		cfg.RemotesWatch = want
	}
}
```

- [ ] **Step 4: Wire the `w` key in the editor**

In `settings_popup.go` `update`, inside the `if p.ratesView {` block's non-editing `switch msg.Type` — add a `String()`-based branch. Since the block switches on `msg.Type`, add before it (still within `if p.ratesView` and `!p.ratesEditing`):
```go
		if msg.String() == "w" {
			m2, cmd := m.toggleRefreshWatch(scheduledItems[p.ratesSel])
			return m2, cmd
		}
```
Place this right after `if p.ratesView {` opens and before the `if p.ratesEditing {` check is already handled — concretely, add it at the top of the `// non-editing` path (after the editing branch returns). Ensure it does not intercept while editing (it is outside the `if p.ratesEditing` block).

- [ ] **Step 5: Render the watch column + help line**

In the `else if p.ratesView` render block, change the per-row value cell to show watch state, and update the help line. Replace the value-cell computation so that for a watch-eligible row whose watch bool is on:
```go
				var valCell string
				if p.ratesEditing && i == p.ratesSel {
					valCell = p.ratesField.View(true) + "s"
				} else if watchEligible(it) && watchOn(m.cfg.Refresh, it) {
					if m.watchSupported {
						valCell = "watch"
					} else {
						// drvfs: watch unavailable → falls back to the interval
						secs, on := scheduledInterval(m.cfg.Refresh, it)
						if on {
							valCell = fmt.Sprintf("watch (9p→%ds)", secs)
						} else {
							valCell = "watch (9p→off)"
						}
					}
				} else {
					secs, on := scheduledInterval(m.cfg.Refresh, it)
					if on {
						valCell = fmt.Sprintf("every %ds", secs)
					} else {
						valCell = "off"
					}
				}
```
And the non-editing help line (currently `"\n[↑/↓] select  [enter] edit  [esc] back"`):
```go
				b.WriteString("\n[↑/↓] select  [enter] edit  [w] file-watch  [esc] back")
```

- [ ] **Step 6: Run the tests + build**

Run: `go test ./internal/tui/ -run 'TestToggleRefreshWatch|TestRates' -v` then `go build -o ./gg ./cmd/gg`
Expected: PASS + clean build.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/settings_popup.go internal/tui/refresh.go internal/tui/*_test.go
git commit -m "feat(tui): Refresh-rates editor file-watch toggle (w)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_012KeCYxqdcKWUv9Zk9QYKFz"
```

---

### Task 9: D1 verification + docs

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`

- [ ] **Step 1: Full staged test run**

Run: `./test.sh` then `./test.sh race`
Expected: all green (vet+gofmt → unit → e2e). Note: the worktree-create e2e flakes (`s14`/`s17`, documented Stream pipe issue) are unrelated; retry once if they appear.

- [ ] **Step 2: Manual WSL2 smoke (the user's explicit "test it in WSL2" ask)**

```bash
# This repo is on /mnt/t (9p) → watcher disabled by design:
go build -o ./gg ./cmd/gg
# 1) On /mnt (9p): open gg, Settings , → Refresh rates → select worktrees → w.
#    Expect the row to show "watch (9p→…)" (watchSupported=false).
# 2) On a WSL2-native (ext4) repo:
mkdir -p ~/gw-test && cd ~/gw-test && git init -q && git commit -q --allow-empty -m init
#    open gg here, enable [refresh] enabled + toggle worktrees/reflog watch (w),
#    then in another shell: `git worktree add ../gw-wt -b t1` and
#    `git commit --allow-empty -m x` → the Worktrees/Reflog panels refresh within ~1s.
```
Record the outcome (both cases) in the PR/changelog notes. This is a manual gate, not automated.

- [ ] **Step 3: Update `CHANGELOG.md`**

Add an entry under the current unreleased section:
```markdown
- **File-watch auto-refresh (Phase D1):** worktrees and reflog panels can refresh
  the moment their `.git` files change (fsnotify) instead of on a timer. Toggle
  per source with `w` in Settings → Refresh rates. Disabled automatically on
  WSL2 `/mnt` (9p) mounts, where the source falls back to interval polling. New
  `internal/gitwatch` package; new `[refresh] *_watch` keys.
```

- [ ] **Step 4: Update `README.md`**

In the refresh/settings section, document the `w` toggle, the `[refresh] worktrees_watch`/`reflog_watch` keys, and the drvfs fallback behavior. (Mirror the existing "Refresh rates" prose.)

- [ ] **Step 5: Update `CLAUDE.md`**

- Add `gitwatch` to the package map: *"Pure fsnotify wrapper + `.git`-layout path→source map (`Plan`) + drvfs `Supported` gate + debounced `Watcher`; no git/TUI/domain imports. Backs event-driven auto-refresh (Phase D). TUI maps `gitwatch.Source`→`sourceKey` and feeds `enqueueDue`."*
- In the `config` row, add the `[refresh] *_watch` keys and `SetRefreshWatch` writer.
- In the `tui` `refresh.go` description, note `watchActive` excludes watch-active sources from polling, and `watch.go` builds the watcher + delivers `watchEventMsg` into the lane.

- [ ] **Step 6: Commit**

```bash
git add CHANGELOG.md README.md CLAUDE.md
git commit -m "docs: Phase D1 file-watch refresh (changelog, readme, package map)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_012KeCYxqdcKWUv9Zk9QYKFz"
```

---

# Phase D2 — refs-tree sources (branches, remotes)

### Task 10: `Plan` recursive groups for Branches + Remotes

**Files:**
- Modify: `internal/gitwatch/plan.go`
- Test: `internal/gitwatch/plan_test.go`

**Interfaces:**
- Produces (extends `Plan`): for `Branches` and `Remotes`, returns recursive ref groups + shared-file groups. Watch sets:
  - `Branches`: `Group{Dir: commonDir/refs/heads, Recursive: true, Match: any→[Branches]}`, plus on `commonDir` a `Match` returning `[Branches]` for base `packed-refs`, plus on `worktreeDir` a `Match` returning `[Branches]` for base `HEAD`.
  - `Remotes`: `Group{Dir: commonDir/refs/remotes, Recursive: true, Match: any→[Remotes]}`, plus on `commonDir` `Match` returning `[Remotes]` for bases `packed-refs`, `FETCH_HEAD`, `config`.
  - **Shared `commonDir` group:** since both branches and remotes care about `commonDir/packed-refs`, a single `Group{Dir: commonDir}` whose `Match` returns the union (`packed-refs`→ both enabled of {Branches,Remotes}; `FETCH_HEAD`/`config`→ Remotes; `HEAD` is in worktreeDir not commonDir for linked worktrees, so HEAD is matched on the worktreeDir group). Build the commonDir group's `Match` from the enabled set so it only emits enabled sources.

- [ ] **Step 1: Write the failing tests**

Append to `plan_test.go`:
```go
func TestPlanBranchesWatchesRefsHeadsRecursive(t *testing.T) {
	c := filepath.Join("/repo", ".git")
	groups := Plan(c, c, []Source{Branches})
	g := groupFor(t, groups, filepath.Join(c, "refs", "heads"))
	if !g.Recursive {
		t.Error("refs/heads group must be recursive")
	}
	if !hasSource(g.Match("main"), Branches) {
		t.Error("a ref under refs/heads should affect Branches")
	}
}

func TestPlanPackedRefsAffectsBothWhenEnabled(t *testing.T) {
	c := filepath.Join("/repo", ".git")
	groups := Plan(c, c, []Source{Branches, Remotes})
	g := groupFor(t, groups, c) // the commonDir shared group
	ss := g.Match("packed-refs")
	if !hasSource(ss, Branches) || !hasSource(ss, Remotes) {
		t.Errorf("packed-refs should affect both Branches and Remotes, got %v", ss)
	}
	if cfg := g.Match("config"); !hasSource(cfg, Remotes) || hasSource(cfg, Branches) {
		t.Errorf("config should affect Remotes only, got %v", cfg)
	}
}

func TestPlanPackedRefsOnlyEnabledSource(t *testing.T) {
	c := filepath.Join("/repo", ".git")
	groups := Plan(c, c, []Source{Branches}) // remotes NOT enabled
	g := groupFor(t, groups, c)
	if ss := g.Match("packed-refs"); hasSource(ss, Remotes) {
		t.Errorf("packed-refs must not emit Remotes when remotes disabled, got %v", ss)
	}
}

func TestPlanBranchesWatchesHEADOnWorktreeDir(t *testing.T) {
	c := filepath.Join("/repo", ".git")
	w := filepath.Join(c, "worktrees", "wt1")
	groups := Plan(c, w, []Source{Branches})
	g := groupFor(t, groups, w)
	if !hasSource(g.Match("HEAD"), Branches) {
		t.Error("HEAD change should affect Branches (current-branch line)")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/gitwatch/ -run TestPlan -v`
Expected: FAIL (new branches/remotes assertions).

- [ ] **Step 3: Extend `Plan`**

Add the branches/remotes cases. Because several sources share `commonDir` and `worktreeDir` group dirs, build those two grouped predicates from the enabled set rather than emitting duplicate groups. Concretely, refactor `Plan` to:
```go
func Plan(commonDir, worktreeDir string, enabled []Source) []Group {
	on := map[Source]bool{}
	for _, s := range enabled {
		on[s] = true
	}
	var groups []Group

	if on[Reflog] {
		groups = append(groups, Group{
			Dir:   filepath.Join(worktreeDir, "logs"),
			Match: func(base string) []Source { if base == "HEAD" { return []Source{Reflog} }; return nil },
		})
	}
	if on[Worktrees] {
		groups = append(groups, Group{
			Dir:   filepath.Join(commonDir, "worktrees"),
			Match: func(base string) []Source { return []Source{Worktrees} },
		})
	}
	if on[Branches] {
		groups = append(groups, Group{
			Dir: filepath.Join(commonDir, "refs", "heads"), Recursive: true,
			Match: func(base string) []Source { return []Source{Branches} },
		})
	}
	if on[Remotes] {
		groups = append(groups, Group{
			Dir: filepath.Join(commonDir, "refs", "remotes"), Recursive: true,
			Match: func(base string) []Source { return []Source{Remotes} },
		})
	}
	// Shared commonDir group: packed-refs (branches+remotes), FETCH_HEAD/config (remotes).
	if on[Branches] || on[Remotes] {
		groups = append(groups, Group{
			Dir: commonDir,
			Match: func(base string) []Source {
				var out []Source
				switch base {
				case "packed-refs":
					if on[Branches] {
						out = append(out, Branches)
					}
					if on[Remotes] {
						out = append(out, Remotes)
					}
				case "FETCH_HEAD", "config":
					if on[Remotes] {
						out = append(out, Remotes)
					}
				}
				return out
			},
		})
	}
	// Worktree HEAD affects the branches view (current-branch line).
	if on[Branches] {
		groups = append(groups, Group{
			Dir: worktreeDir,
			Match: func(base string) []Source { if base == "HEAD" { return []Source{Branches} }; return nil },
		})
	}
	return groups
}
```
**Note:** when `worktreeDir == commonDir` (main worktree) and both Branches is on, two groups share `Dir == commonDir` (the shared group and the HEAD group). The `groupFor` test helper returns the first match; the Watcher (Task 11) must merge predicates for duplicate dirs (run *all* groups whose `Dir` matches), so keep the Watcher's `handle` iterating every group, not breaking on first — it already does. To keep `Plan` tests unambiguous, the `TestPlanBranchesWatchesHEADOnWorktreeDir` test uses a linked worktree (`w != c`) so the HEAD group has a distinct dir.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/gitwatch/ -run TestPlan -v`
Expected: PASS (all Plan tests, D1 + D2).

- [ ] **Step 5: Commit**

```bash
git add internal/gitwatch/plan.go internal/gitwatch/plan_test.go
git commit -m "feat(gitwatch): Plan recursive ref groups for branches and remotes

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_012KeCYxqdcKWUv9Zk9QYKFz"
```

---

### Task 11: Watcher recursive subtree watching

**Files:**
- Modify: `internal/gitwatch/watcher.go`
- Test: `internal/gitwatch/watcher_test.go`

**Interfaces:**
- Consumes: `Group.Recursive`.
- Behavior change: for a `Recursive` group, `New` walks the subtree and `Add`s a watch for every existing subdirectory; on a directory-create event under a recursive group's tree, the new dir is `Add`ed (so refs created in a freshly-made namespace dir are caught). `handle` already iterates all groups; for a recursive group, an event whose `dir` is the group's `Dir` **or any descendant of it** matches.

- [ ] **Step 1: Write the failing tests**

Append to `watcher_test.go`:
```go
func TestWatcherFiresOnNestedRefCreate(t *testing.T) {
	gitdir := t.TempDir()
	heads := filepath.Join(gitdir, "refs", "heads", "feature")
	if err := os.MkdirAll(heads, 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := New(Plan(gitdir, gitdir, []Source{Branches}), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	time.Sleep(20 * time.Millisecond)
	// write refs/heads/feature/foo (a nested loose ref)
	if err := os.WriteFile(filepath.Join(heads, "foo"), []byte("deadbeef\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitSource(t, w, Branches, 2*time.Second)
}

func TestWatcherFiresOnNewNamespaceDir(t *testing.T) {
	gitdir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(gitdir, "refs", "heads"), 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := New(Plan(gitdir, gitdir, []Source{Branches}), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	time.Sleep(20 * time.Millisecond)
	// create a new namespace dir, then a ref inside it
	ns := filepath.Join(gitdir, "refs", "heads", "team")
	if err := os.MkdirAll(ns, 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // let the new-dir Add land
	if err := os.WriteFile(filepath.Join(ns, "x"), []byte("c0ffee\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitSource(t, w, Branches, 2*time.Second)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/gitwatch/ -run 'TestWatcherFiresOnNested|TestWatcherFiresOnNewNamespace' -v`
Expected: FAIL — nested dirs are not watched (only the top dir was `Add`ed in D1), so the ref write under `refs/heads/feature` produces no event.

- [ ] **Step 3: Implement recursive watching**

In `watcher.go`, replace the per-group `Add` in `New` with a helper that walks recursive groups, and handle new-dir creation in `handle`:
```go
// in New, replace the watch-add loop:
	for _, g := range groups {
		w.addGroup(g)
	}
```
Add methods:
```go
import "io/fs" // add to imports if not present

// addGroup adds the watch(es) for one group: just its dir for non-recursive, or
// the whole existing subtree for recursive groups.
func (w *Watcher) addGroup(g Group) {
	if !g.Recursive {
		if _, err := os.Stat(g.Dir); err == nil {
			_ = w.fsw.Add(g.Dir)
		}
		return
	}
	_ = filepath.WalkDir(g.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries; missing root is fine
		}
		if d.IsDir() {
			_ = w.fsw.Add(path)
		}
		return nil
	})
}

// underRecursive reports the sources of a recursive group whose tree contains dir.
func (w *Watcher) recursiveMatch(dir, base string) []Source {
	var out []Source
	for _, g := range w.groups {
		if !g.Recursive {
			continue
		}
		if dir == g.Dir || isUnder(dir, g.Dir) {
			out = append(out, g.Match(base)...)
		}
	}
	return out
}

// isUnder reports whether path is the dir parent or a descendant of base.
func isUnder(path, base string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
```
Update `handle` to (a) match recursive groups by subtree, and (b) `Add` newly-created dirs:
```go
func (w *Watcher) handle(ev fsnotify.Event) {
	base := filepath.Base(ev.Name)
	dir := filepath.Dir(ev.Name)

	// A new directory under a recursive group must start being watched, and the
	// create itself is a change for that group's source.
	if ev.Op&(fsnotify.Create) != 0 {
		if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
			for _, g := range w.groups {
				if g.Recursive && (ev.Name == g.Dir || isUnder(ev.Name, g.Dir)) {
					_ = w.fsw.Add(ev.Name)
				}
			}
		}
	}

	// Non-recursive groups: exact dir match.
	for _, g := range w.groups {
		if g.Recursive || g.Dir != dir {
			continue
		}
		for _, s := range g.Match(base) {
			w.schedule(s)
		}
	}
	// Recursive groups: dir is the group root or a descendant.
	for _, s := range w.recursiveMatch(dir, base) {
		w.schedule(s)
	}
}
```
Add `"strings"` to the imports.

- [ ] **Step 4: Run the tests + race**

Run: `go test -race ./internal/gitwatch/ -v`
Expected: PASS (all watcher tests, D1 + D2).

- [ ] **Step 5: Commit**

```bash
git add internal/gitwatch/watcher.go internal/gitwatch/watcher_test.go
git commit -m "feat(gitwatch): recursive subtree watching for ref groups

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_012KeCYxqdcKWUv9Zk9QYKFz"
```

---

### Task 12: Enable branches + remotes end-to-end + docs

**Files:**
- Modify: `internal/tui/refresh.go` (`watchEligible` adds branches, remotes)
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`
- Test: `internal/tui/refresh_test.go`, `internal/tui/watch_test.go`

**Interfaces:**
- `watchEligible` now returns true for `srcWorktrees, srcReflog, srcBranches, srcRemotes`.

- [ ] **Step 1: Write the failing test**

Append to `watch_test.go`:
```go
func TestEnabledWatchSourcesD2(t *testing.T) {
	cfg := config.RefreshConfig{WorktreesWatch: true, ReflogWatch: true, BranchesWatch: true, RemotesWatch: true}
	got := enabledWatchSources(cfg)
	if len(got) != 4 {
		t.Fatalf("D2 should enable all four, got %v", got)
	}
}
```
And update `TestEnabledWatchSourcesD1` to reflect that branches now counts — rename it or adjust: with D2, a `BranchesWatch:true` config yields 3 (worktrees+reflog+branches). Change the D1 test's expectation accordingly (it becomes redundant with the D2 test; replace it with the D2 test).

Append to `refresh_test.go`:
```go
func TestWatchEligibleD2IncludesRefs(t *testing.T) {
	for _, s := range []sourceKey{srcWorktrees, srcReflog, srcBranches, srcRemotes} {
		if !watchEligible(refreshItem{source: s}) {
			t.Errorf("%v should be watch-eligible in D2", s)
		}
	}
	if watchEligible(refreshItem{source: srcStatus}) {
		t.Error("status must never be eligible")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run 'TestWatchEligibleD2|TestEnabledWatchSourcesD2' -v`
Expected: FAIL — branches/remotes not yet eligible.

- [ ] **Step 3: Extend `watchEligible`**

```go
func watchEligible(it refreshItem) bool {
	if it.isFetch {
		return false
	}
	switch it.source {
	case srcWorktrees, srcReflog, srcBranches, srcRemotes:
		return true
	}
	return false
}
```

- [ ] **Step 4: Run the TUI tests + full suite**

Run: `go test ./internal/tui/ -v` then `./test.sh race`
Expected: PASS / all green.

- [ ] **Step 5: Manual WSL2 smoke (branches/remotes)**

On the `~`-hosted ext4 repo: enable `branches_watch`/`remotes_watch` (`w`), then in another shell `git branch wip`, `git fetch`, `git checkout -b x` → Branches/Remotes panels refresh within ~1s. On `/mnt/t` (9p): rows show `watch (9p→…)`, no watcher. Record outcome.

- [ ] **Step 6: Docs — flip D2 from "Phase D2" to shipped**

- `CHANGELOG.md`: add branches+remotes to the file-watch entry.
- `README.md`: list all four watch-eligible windows.
- `CLAUDE.md`: update the `gitwatch` line to "worktrees, reflog, branches, remotes (recursive ref-tree watching)"; drop the "(Phase D2)" qualifiers in the `[refresh]` key notes.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/refresh.go internal/tui/*_test.go CHANGELOG.md README.md CLAUDE.md
git commit -m "feat(tui): enable file-watch for branches and remotes (Phase D2)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_012KeCYxqdcKWUv9Zk9QYKFz"
```

---

## Self-Review (plan author)

**Spec coverage:**
- `gitwatch.Source`/`Plan`/`Supported`/`Watcher` → Tasks 1–3, 10–11. ✓
- Config bools + `SetRefreshWatch` + settingDocs → Task 4. ✓
- Scheduler `watchActive` skip → Task 5. ✓
- `$W` git dir → Task 6. ✓
- TUI lifecycle + `enqueueDue` delivery + repo-switch rebuild → Task 7. ✓
- Editor watch toggle + drvfs annotation + advertise key → Task 8. ✓
- Testing (pure Plan, Supported, live watcher, scheduler, config, handler, WSL2 manual) → spread across tasks + Task 9/12 manual gates. ✓
- Phasing D1/D2 → task split. ✓
- fsnotify dep → Task 1. ✓

**Placeholder scan:** Two intentional "match the existing helper" notes (Task 4 settingDocs literals; Task 6/7 test constructors) — these point the implementer at a concrete existing pattern in the repo rather than guessing names; the desired content is given. Not blanks.

**Type consistency:** `watchActive(cfg, watchSupported, it)`, `dueItems(now, lastRun, cfg, watchSupported, suppressed)`, `watchEligible(it)`, `watchOn(cfg, it)`, `enabledWatchSources(cfg)`, `watchSourceKey(s)`, `startWatchCmd(gen)`, `watchListenCmd(w, gen)`, `toggleRefreshWatch(it)`, `setRefreshWatchField(cfg, it, want)` — used consistently across Tasks 5/7/8. `watchSupported` field added in Task 5, assigned in Task 7. `watchGen`/`watcher` fields added in Task 7. ✓

**Known cross-task dependency:** Task 5 adds the `watchSupported` field (so `refreshTick` compiles); Task 7 adds `watcher`/`watchGen` and the assignment. If a reviewer runs Task 5 in isolation, `go build` passes (field exists, defaults false). ✓
