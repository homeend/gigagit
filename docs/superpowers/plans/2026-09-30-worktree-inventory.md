# Worktree Inventory for Agents Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (this repo forbids implementer subagents — the session that wrote the plan executes it). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An agent running inside gg can list worktrees with a computed `free` verdict, claim one atomically, and release it; the user reserves worktrees against agents and sees/removes reserves and claims in the TUI.

**Architecture:** Two new DAG-leaf packages — `sessionreg` (each TUI publishes its live agent sessions to a machine-wide state file, mtime liveness) and `wtclaim` (an O_EXCL claim file in a worktree's git admin dir). `domain` composes them with stat-level git probes and per-worktree `git status` into `WorktreeInventory`, `ClaimWorktree`, `ReleaseWorktree`, `WorktreeMarks`. The CLI exposes `gg worktree list --json/--free`, `claim`, `release`, `reserve`, `unreserve`; the TUI publishes its registry and shows marks + menu rows.

**Tech Stack:** Go 1.26, `github.com/pelletier/go-toml/v2`, Bubble Tea v1, real `git` in tests.

**Spec:** `docs/superpowers/specs/2026-09-30-worktree-inventory-design.md`

**Revision 2026-10-01:** revised after a Fable review (missing/status-failed reasons, claim lock, live-session check at claim time, process-alive guard, single-target claim, repo-only `reserved`, test isolation).

## Global Constraints

- Only agents with `GG_SESSION_ID` set may claim; claim without it exits **2** with `only an agent running inside gg can claim`; claim by a session that is not alive exits **2** with `session <id> is not running in any gg`.
- `GG_SESSION_ID` value = `<pid>-<process start unixnano>/<session id>`.
- A claim has **no TTL, no heartbeat**. A session is dead iff its process's registry is live and does not list it running, OR its process has no live registry AND the pid (from `<pid>-<start>/…`) is gone. A stale registry of a LIVE process never kills a claim. Dead claims are deleted by whoever reads them — under the claim lock, after re-reading.
- Every claim create / sweep / release runs under `filelock.Acquire(<gitDir>/gg-claim.lock)`.
- The claim's `agent` comes from the session's registry entry, not the environment.
- An empty claim file (crashed mid-write) is dead once its mtime is 10 s old.
- Registry live window = **5 s** (same as `steer.LiveWindow`); file `<state>/gg/sessions/<proc>.json`, `<state>` resolved via `stateBaseDir` (XDG_STATE_HOME first).
- Claim file: `<worktree git dir>/gg-claim` (linked: `<common>/worktrees/<name>/gg-claim`; main: `<main>/.git/gg-claim`), TOML keys `session`, `agent`, `since`, `note`.
- `[agents] stale_after` default `"14d"` (parsed by `branchfilter.ParseAge`), `[agents] reserved` list (REPO file only — a global value is ignored; relative to main worktree or absolute), `[agents] allow_main` default false. Repo config is anchored on the MAIN worktree (`wts[0].Path`), never the cwd worktree.
- `blocked_by` reason strings exactly: `missing`, `main`, `detached`, `paused-op`, `git-lock`, `reserved`, `claimed`, `tui`, `session`, `dirty-recent`, `status-failed`.
- `git-lock` probes the worktree's own git dir only (documented choice).
- `sessionreg` and `wtclaim` are frontend-forbidden in `internal/archtest` (CLI tests write registry JSON by hand).
- `recycle`: `"none"` clean, `"shelve"` stale-dirty, JSON `null` when not free.
- `--free` order: clean first, then stale-dirty by oldest `last_change`.
- `git status` runs only for worktrees not already blocked by the file-level checks.
- Paths compare via `domain.SameCheckout`.
- Every user-visible TUI string goes through `i18n.T` with a literal key in all four bundles (ja/ko/zh/ru); decision option values stay English.
- `internal/tui` and `internal/cli` never import `internal/git`.
- Tests use real git in `t.TempDir()`, call `t.Parallel()` where they do not `t.Setenv`. The domain and cli `TestMain`s pin `XDG_STATE_HOME` (the tui one already does) so no test reads or sweeps the developer's real registry. `sh`/`sleep` session tests `t.Skip` on Windows like the existing ones.

## Review Focus

1. **Worktree path given in another notation** (relative, trailing slash, WSL `/mnt/…` vs Windows) — `claim`/`release`/`reserve` must resolve it to the listed worktree via `matchWorktreeArg`/`SameCheckout`, never write a claim next to a non-worktree. Test: Task 8 `TestWorktreeClaimRelativePath`.
2. **Owner TUI crashed vs. merely stalled** — a stale registry whose process is gone makes the claim dead (swept, a new claim succeeds); a stale registry whose process is ALIVE keeps the claim. Tests: Task 7 `TestClaimSweepsClaimOfDeadRegistry`, `TestStalledRegistryKeepsClaim`.
3. **Deleted file among dirty paths** — `last_change` must ignore unstat-able paths rather than failing the whole inventory. Test: Task 6 `TestInventoryLastChangeSkipsDeletedPaths`.
4. **Malformed `stale_after`** in config — the CLI must report the config error (exit 1), not panic or treat everything as fresh. Test: Task 8 `TestWorktreeListBadStaleAfter`.
5. **A worktree whose directory was deleted** (prunable) — must be `missing`, never free, and a claim on it must be refused instead of writing `gg-claim` into the cwd. Tests: Task 6 `TestInventoryMissingWorktree`, Task 7 `TestClaimRefusesMissingWorktree`.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/git/headfile.go` (modify) | add `GitDirAt(dir)`; `HeadAt` uses it |
| `internal/agentsession/proctag.go` (create) | `ProcTag()` — `<pid>-<start unixnano>`, once per process |
| `internal/agentsession/session.go` (modify) | child env `GG_SESSION_ID=<ProcTag()>/<id>` |
| `internal/sessionreg/sessionreg.go` (create) | registry file types + `Write`/`Touch`/`Remove`/`Live`, `PIDOf`, `ProcOf` |
| `internal/sessionreg/procalive_unix.go`, `procalive_windows.go` (create) | `ProcAlive(pid)` |
| `internal/archtest/import_guard_test.go` (modify) | forbid `sessionreg`/`wtclaim` in frontends |
| `internal/wtclaim/wtclaim.go` (create) | claim file `Create`/`Read`/`Remove`/`Age`, `WithLock` |
| `internal/config/config.go`, `template.go`, `write.go` (modify) | `[agents]` section, settingDocs, `SetAgentsReserved` |
| `internal/domain/sessionpublish.go` (create) | `PublishSessions`, `SessionRegistryDir`, `liveSessions` |
| `internal/domain/wtinventory.go` (create) | `InventoryPolicy`, `WorktreeInfo`, `WorktreeInventory` |
| `internal/domain/wtclaim.go` (create) | `ClaimWorktree`, `ReleaseWorktree`, `WorktreeMarks`, `SetWorktreeReserved` |
| `internal/cli/worktree.go` (modify) + `internal/cli/worktree_agents.go` (create) | list flags + claim/release/reserve/unreserve |
| `internal/gitwatch/plan.go` (modify) | claim-file groups → `Worktrees` |
| `internal/tui/run.go`, `model.go`, `load.go`, `source.go`, `view.go`, `agent_start_popup.go`, `recycle_worktree.go` (modify), `worktree_marks.go` (create) | publisher, marks, menu rows, hint, picker |
| `internal/i18n/lang/*.toml` (modify) | new keys |
| docs | `using-gg.md`, `agentskill.Version`, CHANGELOG, README, CLAUDE.md rows, CLAUDE-details |

---

### Task 1: `GitDirAt` and a process-unique `GG_SESSION_ID`

**Files:**
- Modify: `internal/git/headfile.go`
- Create: `internal/agentsession/proctag.go`
- Modify: `internal/agentsession/session.go:67`
- Test: `internal/git/headfile_test.go`, `internal/agentsession/session_test.go` (the existing env test that expects `envid`)

**Interfaces:**
- Produces: `func git.GitDirAt(dir string) string` ("" when unreadable); `func agentsession.ProcTag() string`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/git/headfile_test.go
func TestGitDirAtMainAndLinked(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "x\n")
	if got := GitDirAt(dir); got != filepath.Join(dir, ".git") {
		t.Fatalf("main GitDirAt = %q", got)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	if out, err := exec.Command("git", "-C", dir, "worktree", "add", "-b", "b", wt).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	got := GitDirAt(wt)
	if filepath.Base(filepath.Dir(got)) != "worktrees" {
		t.Fatalf("linked GitDirAt = %q, want <common>/worktrees/<name>", got)
	}
	if GitDirAt(t.TempDir()) != "" {
		t.Fatal("non-repo must be empty")
	}
}
```

In `internal/agentsession/session_test.go`, find the test that asserts the child printed `envid` for `$GG_SESSION_ID` and change the expectation to `ProcTag()+"/envid"`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/git -run TestGitDirAt ./internal/agentsession -run Env`
Expected: FAIL — `undefined: GitDirAt`, `undefined: ProcTag`.

- [ ] **Step 3: Implement**

```go
// internal/git/headfile.go — extract from HeadAt
// GitDirAt is the git directory of the working tree at dir, read from the
// files alone: <dir>/.git when it is a directory (a primary worktree), else
// the `gitdir:` target of the .git FILE (a linked worktree; relative targets
// resolve against dir). "" when dir is not a working tree.
func GitDirAt(dir string) string {
	gitDir := filepath.Join(dir, ".git")
	fi, err := os.Stat(gitDir)
	if err != nil {
		return ""
	}
	if fi.IsDir() {
		return gitDir
	}
	raw, err := os.ReadFile(gitDir)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(line, "gitdir:") {
		return ""
	}
	target := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	if !filepath.IsAbs(target) {
		target = filepath.Join(dir, target)
	}
	return filepath.Clean(target)
}
```

`HeadAt` becomes: `gitDir := GitDirAt(dir); if gitDir == "" { return "" }` followed by its existing HEAD-reading tail.

```go
// internal/agentsession/proctag.go
package agentsession

import (
	"os"
	"strconv"
	"sync"
	"time"
)

var procTag = sync.OnceValue(func() string {
	return strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
})

// ProcTag names this gg process uniquely on the machine: <pid>-<start
// unixnano>. A session's GG_SESSION_ID is <ProcTag>/<id>, and a TUI's session
// registry file is <ProcTag>.json, so an id resolves to its registry.
func ProcTag() string { return procTag() }
```

`internal/agentsession/session.go:67`: replace `"GG_SESSION_ID="+string(id)` with `"GG_SESSION_ID="+ProcTag()+"/"+string(id)`.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/git ./internal/agentsession`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/git/headfile.go internal/git/headfile_test.go internal/agentsession/proctag.go internal/agentsession/session.go internal/agentsession/session_test.go
git commit -m "feat(agentsession): process-unique GG_SESSION_ID; git.GitDirAt"
```

---

### Task 2: `sessionreg` — the published session registry

**Files:**
- Create: `internal/sessionreg/sessionreg.go`, `internal/sessionreg/procalive_unix.go`, `internal/sessionreg/procalive_windows.go`
- Modify: `internal/archtest/import_guard_test.go` (two `forbidden` rows)
- Test: `internal/sessionreg/sessionreg_test.go`

**Interfaces:**
- Produces:
```go
type Entry struct { ID, Dir, Agent, Label, State, Started string }  // State "running"|"exited"; ID = "<proc>/<id>"
type Registry struct { Proc string /* from the file name, not stored */; PID int; Started, Worktree string; Sessions []Entry }
const LiveWindow = 5 * time.Second
func PIDOf(sessionID string) int      // "<pid>-<start>/<id>" -> pid; 0 if malformed
func ProcOf(sessionID string) string  // "<pid>-<start>/<id>" -> "<pid>-<start>"
func ProcAlive(pid int) bool          // false for pid <= 0
func Write(dir, proc string, r Registry) error
func Touch(dir, proc string) error
func Remove(dir, proc string)
func Live(dir string) []Registry
```

- [ ] **Step 1: Write the failing tests**

```go
package sessionreg

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteLiveRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r := Registry{PID: 1, Worktree: "/w", Sessions: []Entry{{ID: "p/s1", Dir: "/w2", Agent: "claude", State: "running"}}}
	if err := Write(dir, "p", r); err != nil {
		t.Fatal(err)
	}
	got := Live(dir)
	if len(got) != 1 || got[0].Sessions[0].ID != "p/s1" || got[0].Worktree != "/w" {
		t.Fatalf("Live = %+v", got)
	}
}

func TestLiveSweepsStaleAndGarbage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := Write(dir, "old", Registry{PID: 2}); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * LiveWindow)
	os.Chtimes(filepath.Join(dir, "old.json"), past, past)
	os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{"), 0o644)
	if got := Live(dir); len(got) != 0 {
		t.Fatalf("Live = %+v, want none", got)
	}
	for _, n := range []string{"old.json", "bad.json"} {
		if _, err := os.Stat(filepath.Join(dir, n)); !os.IsNotExist(err) {
			t.Fatalf("%s not swept", n)
		}
	}
}

func TestTouchKeepsAlive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	Write(dir, "p", Registry{PID: 3})
	past := time.Now().Add(-2 * LiveWindow)
	os.Chtimes(filepath.Join(dir, "p.json"), past, past)
	if err := Touch(dir, "p"); err != nil {
		t.Fatal(err)
	}
	if len(Live(dir)) != 1 {
		t.Fatal("touched registry must be live")
	}
}

func TestLiveMissingDir(t *testing.T) {
	t.Parallel()
	if got := Live(filepath.Join(t.TempDir(), "nope")); got != nil {
		t.Fatalf("Live = %+v", got)
	}
}

func TestLiveFillsProc(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	Write(dir, "123-456", Registry{PID: 123})
	if got := Live(dir); len(got) != 1 || got[0].Proc != "123-456" {
		t.Fatalf("Live = %+v", got)
	}
}

func TestPIDOfAndProcOf(t *testing.T) {
	t.Parallel()
	if PIDOf("4711-99/s3") != 4711 || ProcOf("4711-99/s3") != "4711-99" {
		t.Fatal("parse")
	}
	if PIDOf("p/s1") != 0 || PIDOf("") != 0 {
		t.Fatal("malformed must be 0")
	}
}

func TestProcAlive(t *testing.T) {
	t.Parallel()
	if !ProcAlive(os.Getpid()) {
		t.Fatal("self must be alive")
	}
	if ProcAlive(0) || ProcAlive(-1) {
		t.Fatal("non-positive pid must be dead")
	}
	c := exec.Command(os.Args[0], "-test.run=^$") // exits at once
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	if ProcAlive(c.Process.Pid) {
		t.Skip("pid reused already — cannot assert on this machine")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/sessionreg`
Expected: FAIL — package has no non-test files / undefined symbols.

- [ ] **Step 3: Implement**

```go
// Package sessionreg is the machine-wide registry of live agent sessions: each
// gg TUI publishes <proc>.json listing the sessions it hosts, so another gg
// process (the CLI) can tell which sessions are running and which worktree a
// TUI is rooted in. Liveness is the file's mtime (refreshed every second by
// the owner), the steer presence precedent. DAG leaf: stdlib only.
package sessionreg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LiveWindow is how stale a registry may get before it counts as belonging
// to a crashed process (steer.LiveWindow's value).
const LiveWindow = 5 * time.Second

type Entry struct {
	ID      string `json:"id"` // <proc>/<session id> — the child's GG_SESSION_ID
	Dir     string `json:"dir"`
	Agent   string `json:"agent,omitempty"`
	Label   string `json:"label,omitempty"`
	State   string `json:"state"` // "running" | "exited"
	Started string `json:"started,omitempty"`
}

type Registry struct {
	PID      int     `json:"pid"`
	Started  string  `json:"started,omitempty"`
	Worktree string  `json:"worktree"` // the TUI's own worktree
	Sessions []Entry `json:"sessions"`
}

func file(dir, proc string) string { return filepath.Join(dir, proc+".json") }

// Write replaces proc's registry atomically (temp + rename).
func Write(dir, proc string, r Registry) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+proc+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), file(dir, proc))
}

// Touch refreshes proc's liveness.
func Touch(dir, proc string) error {
	now := time.Now()
	return os.Chtimes(file(dir, proc), now, now)
}

// Remove deletes proc's registry (clean shutdown).
func Remove(dir, proc string) { os.Remove(file(dir, proc)) }

// Live returns every registry fresher than LiveWindow, removing stale and
// unparsable ones so the next reader does not even stat them.
func Live(dir string) []Registry {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Registry
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		p := filepath.Join(dir, name)
		st, err := e.Info()
		if err != nil {
			continue
		}
		if time.Since(st.ModTime()) > LiveWindow {
			os.Remove(p)
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var r Registry
		if json.Unmarshal(data, &r) != nil {
			os.Remove(p)
			continue
		}
		r.Proc = strings.TrimSuffix(name, ".json")
		out = append(out, r)
	}
	return out
}

// ProcOf is the "<pid>-<start>" part of a session id — its registry's name.
func ProcOf(sessionID string) string {
	proc, _, _ := strings.Cut(sessionID, "/")
	return proc
}

// PIDOf is the pid inside a session id; 0 when the id is malformed.
func PIDOf(sessionID string) int {
	pid, _, _ := strings.Cut(ProcOf(sessionID), "-")
	n, err := strconv.Atoi(pid)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
```

`Registry.Proc` carries `json:"-"`. Add `strconv` to the imports.

```go
// procalive_unix.go
//go:build !windows

package sessionreg

import (
	"errors"
	"syscall"
)

// ProcAlive reports whether a process with pid exists (signal 0; EPERM means
// it exists but belongs to someone else).
func ProcAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
```

```go
// procalive_windows.go
//go:build windows

package sessionreg

import "golang.org/x/sys/windows"

const stillActive = 259 // STILL_ACTIVE

func ProcAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil {
		return true
	}
	return code == stillActive
}
```

(add `"errors"` to the Windows file's imports). The package doc changes to "DAG leaf: stdlib + x/sys". Cross-compile check: `GOOS=windows go vet ./internal/sessionreg`.

`internal/archtest/import_guard_test.go` `forbidden` map — add:

```go
		"github.com/homeend/gigagit/internal/sessionreg": "frontends must reach the session registry through internal/domain",
		"github.com/homeend/gigagit/internal/wtclaim":    "frontends must reach worktree claims through internal/domain",
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/sessionreg ./internal/archtest && GOOS=windows go vet ./internal/sessionreg`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/sessionreg internal/archtest/import_guard_test.go
git commit -m "feat(sessionreg): machine-wide registry of live agent sessions"
```

---

### Task 3: `wtclaim` — the claim file

**Files:**
- Create: `internal/wtclaim/wtclaim.go`
- Test: `internal/wtclaim/wtclaim_test.go`

**Interfaces:**
- Produces:
```go
type Claim struct { Session, Agent string; Since time.Time; Note string }
var ErrClaimed = errors.New("worktree already claimed")
const FileName = "gg-claim"
func Create(gitDir string, c Claim) error      // ErrClaimed when present
func Read(gitDir string) (Claim, bool, error)  // ok=false when absent
func Remove(gitDir string) error               // nil when absent
func Age(gitDir string) (time.Duration, bool)  // time since the claim file's mtime
func WithLock(gitDir string, fn func() error) error  // filelock on <gitDir>/gg-claim.lock
```

- [ ] **Step 1: Write the failing tests**

```go
package wtclaim

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCreateReadRemove(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	c := Claim{Session: "p/s1", Agent: "claude", Since: time.Unix(1700000000, 0).UTC(), Note: "https://x/issues/1"}
	if err := Create(d, c); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Read(d)
	if err != nil || !ok || got.Session != c.Session || got.Agent != c.Agent || got.Note != c.Note || !got.Since.Equal(c.Since) {
		t.Fatalf("Read = %+v %v %v", got, ok, err)
	}
	if a, ok := Age(d); !ok || a > time.Minute {
		t.Fatalf("Age = %v %v", a, ok)
	}
	if err := Create(d, c); !errors.Is(err, ErrClaimed) {
		t.Fatalf("second Create = %v, want ErrClaimed", err)
	}
	if err := Remove(d); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := Read(d); ok {
		t.Fatal("claim still present")
	}
	if err := Remove(d); err != nil {
		t.Fatalf("Remove absent = %v", err)
	}
}

func TestWithLockSerialises(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	var mu sync.Mutex
	inside, max := 0, 0
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			WithLock(d, func() error {
				mu.Lock()
				inside++
				max = maxInt(max, inside)
				mu.Unlock()
				time.Sleep(10 * time.Millisecond)
				mu.Lock()
				inside--
				mu.Unlock()
				return nil
			})
		}()
	}
	wg.Wait()
	if max != 1 {
		t.Fatalf("max concurrent = %d, want 1", max)
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func TestCreateRaceOneWinner(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	var wg sync.WaitGroup
	wins := make(chan int, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if Create(d, Claim{Session: "p/s" + string(rune('0'+i))}) == nil {
				wins <- i
			}
		}()
	}
	wg.Wait()
	close(wins)
	if n := len(wins); n != 1 {
		t.Fatalf("winners = %d, want 1", n)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/wtclaim`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Implement**

```go
// Package wtclaim is the claim an agent running inside gg holds on a
// worktree: one small TOML file in that worktree's git dir, created with
// O_EXCL so of two racing claimers exactly one wins. It dies with `git
// worktree remove`. Whether a claim is still ALIVE (its session running) is
// the caller's question — this package only stores it. DAG leaf.
package wtclaim

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

const FileName = "gg-claim"

var ErrClaimed = errors.New("worktree already claimed")

type Claim struct {
	Session string    `toml:"session"`
	Agent   string    `toml:"agent"`
	Since   time.Time `toml:"since"`
	Note    string    `toml:"note"`
}

func path(gitDir string) string { return filepath.Join(gitDir, FileName) }

func Create(gitDir string, c Claim) error {
	data, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path(gitDir), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return ErrClaimed
	}
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path(gitDir))
		return err
	}
	return f.Close()
}

func Read(gitDir string) (Claim, bool, error) {
	data, err := os.ReadFile(path(gitDir))
	if errors.Is(err, os.ErrNotExist) {
		return Claim{}, false, nil
	}
	if err != nil {
		return Claim{}, false, err
	}
	var c Claim
	if err := toml.Unmarshal(data, &c); err != nil {
		return Claim{}, false, err
	}
	return c, true, nil
}

func Remove(gitDir string) error {
	err := os.Remove(path(gitDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Age is how long ago the claim file was last written.
func Age(gitDir string) (time.Duration, bool) {
	st, err := os.Stat(path(gitDir))
	if err != nil {
		return 0, false
	}
	return time.Since(st.ModTime()), true
}

// WithLock runs fn holding the cross-process lock beside the claim, so a
// check-then-write (claim over a dead claim, sweep, release) can never
// remove a claim someone else created after fn's read.
func WithLock(gitDir string, fn func() error) error {
	release, err := filelock.Acquire(filepath.Join(gitDir, FileName+".lock"))
	if err != nil {
		return err
	}
	defer release()
	return fn()
}
```

Add `"github.com/homeend/gigagit/internal/filelock"` to the imports.

Note on the racing reader: a reader that hits the file between `OpenFile` and `Write` gets an empty file → `toml.Unmarshal` of "" yields a zero `Claim` with `ok=true`. The domain treats a claim with an empty `Session` as mid-write and alive until its `Age` passes 10 s — see Task 7.

- [ ] **Step 4: Run to verify pass**

Run: `go test -race ./internal/wtclaim`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/wtclaim
git commit -m "feat(wtclaim): O_EXCL claim file in a worktree's git dir"
```

---

### Task 4: `[agents]` config section + reserve writer

**Files:**
- Modify: `internal/config/config.go` (section struct, `Config` field, `Defaults()`, `overlayAgents`, `Load` wiring next to `overlayTasks`)
- Modify: `internal/config/template.go` (three `settingDoc` entries)
- Modify: `internal/config/write.go` (`SetAgentsReserved`)
- Test: `internal/config/config_test.go`, `internal/config/write_test.go`

**Interfaces:**
- Produces:
```go
type AgentsConfig struct {
	Reserved   []string `toml:"reserved"`
	StaleAfter string   `toml:"stale_after"`
	AllowMain  bool     `toml:"allow_main"`
}
// Config gains: Agents AgentsConfig `toml:"agents"`
func SetAgentsReserved(path string, paths []string) error
```

- [ ] **Step 1: Write the failing tests**

```go
// config_test.go
func TestAgentsLayers(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	global := filepath.Join(d, "g.toml")
	repo := filepath.Join(d, "r.toml")
	os.WriteFile(global, []byte("[agents]\nstale_after = \"7d\"\nreserved = [\"a\"]\n"), 0o644)
	os.WriteFile(repo, []byte("[agents]\nreserved = [\"b\", \"c\"]\nallow_main = true\n"), 0o644)
	c, err := Load(global, repo)
	if err != nil {
		t.Fatal(err)
	}
	if c.Agents.StaleAfter != "7d" || !c.Agents.AllowMain || len(c.Agents.Reserved) != 2 || c.Agents.Reserved[0] != "b" {
		t.Fatalf("Agents = %+v", c.Agents)
	}
	os.WriteFile(repo, []byte("[agents]\nallow_main = true\n"), 0o644)
	c, _ = Load(global, repo)
	if len(c.Agents.Reserved) != 0 {
		t.Fatalf("a GLOBAL reserved list must be ignored, got %q", c.Agents.Reserved)
	}
	if Defaults().Agents.StaleAfter != "14d" {
		t.Fatalf("default stale_after = %q", Defaults().Agents.StaleAfter)
	}
}

// write_test.go
func TestSetAgentsReservedRoundTrip(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), ".gg.toml")
	os.WriteFile(p, []byte("# keep me\n[ui]\ntheme = \"dark\"\n"), 0o644)
	if err := SetAgentsReserved(p, []string{"../wt a", `C:\x`}); err != nil {
		t.Fatal(err)
	}
	c, err := Load("", p)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Agents.Reserved) != 2 || c.Agents.Reserved[0] != "../wt a" || c.Agents.Reserved[1] != `C:\x` {
		t.Fatalf("Reserved = %q", c.Agents.Reserved)
	}
	raw, _ := os.ReadFile(p)
	if !strings.Contains(string(raw), "# keep me") {
		t.Fatal("comment lost")
	}
	if err := SetAgentsReserved(p, nil); err != nil {
		t.Fatal(err)
	}
	c, _ = Load("", p)
	if len(c.Agents.Reserved) != 0 {
		t.Fatalf("Reserved after clear = %q", c.Agents.Reserved)
	}
}
```

(If `Load("", p)` is not how an absent global is passed in existing tests, copy the form `TestUIWheelStepLayers` uses.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/config -run 'TestAgentsLayers|TestSetAgentsReserved|TestSettingDocsCoverAllFields'`
Expected: FAIL — `c.Agents undefined`.

- [ ] **Step 3: Implement**

```go
// config.go
// AgentsConfig governs which worktrees an orchestrating agent may take
// (`gg worktree list --free`, `gg worktree claim`).
type AgentsConfig struct {
	// Reserved worktrees are never handed to an agent: paths relative to the
	// main worktree, or absolute.
	Reserved []string `toml:"reserved"`
	// StaleAfter is how long a dirty worktree must sit untouched before an
	// agent may recycle it (shelving its changes): "<n>d|w|m|y".
	StaleAfter string `toml:"stale_after"`
	// AllowMain lets agents take the main checkout.
	AllowMain bool `toml:"allow_main"`
}

func overlayAgents(dst *AgentsConfig, src AgentsConfig) {
	if len(src.Reserved) > 0 {
		dst.Reserved = src.Reserved
	}
	if src.StaleAfter != "" {
		dst.StaleAfter = src.StaleAfter
	}
	if src.AllowMain {
		dst.AllowMain = true
	}
}
```

Add `Agents AgentsConfig \`toml:"agents"\`` to `Config`; `Agents: AgentsConfig{StaleAfter: "14d"}` in `Defaults()`; call `overlayAgents(&cfg.Agents, layer.Agents)` in `Load` for both layers exactly where `overlayTasks` is called, and right after the GLOBAL layer's call add `cfg.Agents.Reserved = nil` with the comment `// reserved is repo-only: paths belong to one repo, and a global entry could never be unreserved from it`.

Reserved clearing: because overlay ignores an empty slice, `SetAgentsReserved(path, nil)` must REMOVE the line — an empty `reserved = []` in the repo file would be ignored and a global list would win anyway, which is the documented overlay rule.

```go
// write.go
// SetAgentsReserved persists `[agents] reserved` to the given (repo) config
// file, preserving comments; an empty list removes the key.
func SetAgentsReserved(path string, paths []string) error {
	if len(paths) == 0 {
		return removeScalarLine(path, "agents", "reserved")
	}
	q := make([]string, len(paths))
	for i, p := range paths {
		q[i] = strconv.Quote(p)
	}
	return setScalarLine(path, "agents", "reserved", "["+strings.Join(q, ", ")+"]")
}
```

If `write.go` has no `removeScalarLine`, add it next to `setScalarLine`, reusing its section/key line scan: find the `key = …` line inside `[section]` and drop it; missing file or key → nil. (`strconv.Quote` output is a valid TOML basic string for these paths: backslashes and quotes are escaped the same way.)

`template.go`: add `"agents"` to `Template()`'s explicit section list (`template.go:188`, after `"tasks"`), and add three `settingDoc` entries for `agents.reserved`, `agents.stale_after`, `agents.allow_main`, copying the shape of the `tasks.max_parallel` entry (section, key, default rendering, one-line comment).

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/config`
Expected: PASS (including `TestSettingDocsCoverAllFields`).

- [ ] **Step 5: Commit**

```bash
gg add internal/config
git commit -m "feat(config): [agents] reserved / stale_after / allow_main"
```

---

### Task 5: Domain — publish and read live sessions

**Files:**
- Create: `internal/domain/sessionpublish.go`
- Modify: `internal/domain/main_test.go` (pin `XDG_STATE_HOME`)
- Test: `internal/domain/sessionpublish_test.go`

**Interfaces:**
- Consumes: `sessionreg.*` (Task 2), `agentsession.ProcTag` (Task 1), `Sessions()`.
- Produces:
```go
func SessionRegistryDir() string                               // stateBaseDir("sessions")
func PublishSessions(ctx context.Context, dir string, worktree func() string)
type liveView struct {
	running map[string]bool     // full session id -> running
	agents  map[string]string   // full session id -> agent tool id
	procs   map[string]bool     // procs with a LIVE registry (plus this process)
	byDir   map[string][]SessionRef
	tuis    []string            // TUI worktrees
}
type SessionRef struct { ID, Agent, State string }
func readLive(dir string) liveView                             // registries + this process's Sessions()
func sessionDead(id string, lv liveView) bool
```

- [ ] **Step 1: Write the failing test**

```go
func TestPublishSessionsWritesAndRemoves(t *testing.T) {
	// Not parallel: swaps the process-global session manager.
	if runtime.GOOS == "windows" {
		t.Skip("sleep-based session")
	}
	mgr := agentsession.NewManager()
	defer UseSessionManager(mgr)()
	dir := t.TempDir()
	wt := t.TempDir()
	s, err := mgr.Start(agentsession.StartSpec{Label: "t", AgentID: "claude", Dir: wt, Argv: []string{"sleep", "30"}, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.KillAll(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { PublishSessions(ctx, dir, func() string { return "/tui/wt" }); close(done) }()
	id := agentsession.ProcTag() + "/" + string(s.Info().ID)
	waitFor(t, func() bool { return readLive(dir).running[id] })
	lv := readLive(dir)
	if len(lv.tuis) != 1 || lv.tuis[0] != "/tui/wt" || len(lv.byDir[filepath.Clean(wt)]) != 1 {
		t.Fatalf("live = %+v", lv)
	}
	cancel()
	<-done
	if regs := sessionreg.Live(dir); len(regs) != 0 {
		t.Fatalf("registry not removed: %+v", regs)
	}
}
```

`waitFor` — add to the test file if the package has none:

```go
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in 5s")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
```

(Check first with `grep -rn 'func waitFor' internal/domain` and reuse an existing helper.)

```go
func TestSessionDead(t *testing.T) {
	t.Parallel()
	self := fmt.Sprintf("%d-1", os.Getpid())
	lv := liveView{
		running: map[string]bool{"live-1/s1": true, "live-1/s2": false},
		procs:   map[string]bool{"live-1": true},
	}
	cases := map[string]bool{
		"live-1/s1":    false, // listed running
		"live-1/s2":    true,  // its registry is live and says exited
		"live-1/s9":    true,  // its registry is live and does not list it
		self + "/s1":   false, // no live registry, but the process is alive (stalled TUI)
		"0-1/s1":       true,  // no registry, no process
		"garbage":      true,
	}
	for id, want := range cases {
		if got := sessionDead(id, lv); got != want {
			t.Errorf("sessionDead(%q) = %v, want %v", id, got, want)
		}
	}
}
```

Pin the state dir in `internal/domain/main_test.go` next to `XDG_CONFIG_HOME`:

```go
	state, err := os.MkdirTemp("", "gg-domain-state")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", state)
	// … after m.Run(): os.RemoveAll(state)
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/domain -run TestPublishSessions`
Expected: FAIL — undefined `PublishSessions`.

- [ ] **Step 3: Implement**

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/sessionreg"
)

// SessionRegistryDir is where every TUI publishes its live sessions.
func SessionRegistryDir() string { return stateBaseDir("sessions") }

type SessionRef struct{ ID, Agent, State string }

// PublishSessions keeps this process's registry current until ctx ends:
// rewritten on every session-list change, touched every second, removed on
// return. worktree reports the TUI's current worktree (it changes on reRoot).
func PublishSessions(ctx context.Context, dir string, worktree func() string) {
	if dir == "" {
		return
	}
	proc := agentsession.ProcTag()
	started := time.Now().UTC().Format(time.RFC3339)
	changed, stop := Sessions().Subscribe()
	defer stop()
	// dirty: the last write failed (on Windows a rename over a file a reader
	// holds open fails) — retried on the next tick so a new session is never
	// left unlisted until the NEXT change.
	dirty := false
	write := func() { dirty = sessionreg.Write(dir, proc, snapshotRegistry(started, worktree())) != nil }
	write()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	defer sessionreg.Remove(dir, proc)
	lastWT := worktree()
	for {
		select {
		case <-ctx.Done():
			return
		case <-changed:
			write()
		case <-tick.C:
			if wt := worktree(); wt != lastWT || dirty {
				lastWT = wt
				write()
			} else if sessionreg.Touch(dir, proc) != nil {
				write() // swept by a reader while we stalled
			}
		}
	}
}

func snapshotRegistry(started, wt string) sessionreg.Registry {
	r := sessionreg.Registry{PID: os.Getpid(), Started: started, Worktree: wt}
	for _, in := range Sessions().List() {
		r.Sessions = append(r.Sessions, sessionreg.Entry{
			ID: agentsession.ProcTag() + "/" + string(in.ID), Dir: in.Dir, Agent: in.AgentID,
			Label: in.Label, State: sessionStateName(in.State), Started: in.Started.UTC().Format(time.RFC3339),
		})
	}
	return r
}

func sessionStateName(s agentsession.State) string {
	if s == agentsession.Running {
		return "running"
	}
	return "exited"
}

type liveView struct {
	running map[string]bool
	agents  map[string]string
	procs   map[string]bool
	byDir   map[string][]SessionRef
	tuis    []string
}

// sessionDead: its process's registry is live and does not list it running,
// or its process has no live registry and is gone. A registry that only went
// stale while its process lives (suspend, a frozen host) never kills.
func sessionDead(id string, lv liveView) bool {
	if lv.running[id] {
		return false
	}
	proc := sessionreg.ProcOf(id)
	if proc == "" || proc == id {
		return true // malformed
	}
	if lv.procs[proc] {
		return true
	}
	return !sessionreg.ProcAlive(sessionreg.PIDOf(id))
}

// readLive merges every live registry with THIS process's own sessions (so a
// TUI's marks are right before its first registry write, and a registry
// swept during a stall never hides our own sessions).
func readLive(dir string) liveView {
	lv := liveView{running: map[string]bool{}, agents: map[string]string{},
		procs: map[string]bool{agentsession.ProcTag(): true}, byDir: map[string][]SessionRef{}}
	add := func(e sessionreg.Entry) {
		if _, dup := lv.running[e.ID]; dup {
			return
		}
		lv.running[e.ID] = e.State == "running"
		lv.agents[e.ID] = e.Agent
		d := filepath.Clean(e.Dir)
		lv.byDir[d] = append(lv.byDir[d], SessionRef{ID: e.ID, Agent: e.Agent, State: e.State})
	}
	for _, e := range snapshotRegistry("", "").Sessions {
		add(e)
	}
	if dir != "" {
		for _, r := range sessionreg.Live(dir) {
			lv.procs[r.Proc] = true
			if r.Worktree != "" {
				lv.tuis = append(lv.tuis, r.Worktree)
			}
			for _, e := range r.Sessions {
				add(e)
			}
		}
	}
	return lv
}
```

Check `Sessions().Subscribe()` signature matches `Manager.Subscribe() (<-chan struct{}, func())` (it does, `manager.go:35`).

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/domain -run 'TestPublishSessions|TestSessionDead' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/domain/sessionpublish.go internal/domain/sessionpublish_test.go internal/domain/main_test.go
git commit -m "feat(domain): publish live agent sessions to the session registry"
```

---

### Task 6: Domain — `WorktreeInventory`

**Files:**
- Create: `internal/domain/wtinventory.go`
- Test: `internal/domain/wtinventory_test.go`

**Interfaces:**
- Consumes: `readLive` (Task 5), `git.GitDirAt` (Task 1), `wtclaim.Read` (Task 3), `config.AgentsConfig` (Task 4), `git.PausedOpIn`, `git.LockFiles`, `branchfilter.ParseAge`, `SameCheckout`.
- Produces:
```go
type InventoryPolicy struct {
	StaleAfter time.Duration
	Reserved   []string   // as configured (relative to main or absolute)
	AllowMain  bool
	Now        func() time.Time // nil = time.Now
}
func PolicyFromConfig(c config.AgentsConfig) (InventoryPolicy, error)
type DirtyInfo struct { Staged, Unstaged, Untracked int; LastChange *time.Time }
type ClaimInfo struct { Session, Agent string; Since time.Time; Note string }
type WorktreeInfo struct {
	Path, Branch, Head string
	Main, Detached bool
	Dirty *DirtyInfo
	PausedOp string
	GitLock bool
	TUI bool
	Sessions []SessionRef
	Reserved bool
	Claim *ClaimInfo
	Free bool
	BlockedBy []string
	Recycle string // "none" | "shelve" | "" (not free)
}
func (s *Service) WorktreeInventory(ctx context.Context, pol InventoryPolicy, freeOnly bool) ([]WorktreeInfo, error)
func (s *Service) inventory(ctx context.Context, pol InventoryPolicy, only string) ([]WorktreeInfo, error) // only != "": that worktree alone (claim path — no status fan-out)
func (s *Service) UseSessionRegistryDir(dir string)   // test seam; "" = SessionRegistryDir()
```

- [ ] **Step 1: Write the failing tests**

```go
// helper: a repo with linked worktrees, a per-Service registry dir.
func inventoryRepo(t *testing.T) (main string, svc *Service, reg string) {
	t.Helper()
	main, svc = newRealRepo(t)
	reg = t.TempDir()
	svc.UseSessionRegistryDir(reg)
	return
}

func addWT(t *testing.T, main, branch string) string {
	t.Helper()
	wt := filepath.Join(t.TempDir(), branch)
	if out, err := exec.Command("git", "-C", main, "worktree", "add", "-b", branch, wt).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	return wt
}

func find(t *testing.T, infos []WorktreeInfo, path string) WorktreeInfo {
	t.Helper()
	for _, w := range infos {
		if SameCheckout(w.Path, path) {
			return w
		}
	}
	t.Fatalf("%s not in inventory", path)
	return WorktreeInfo{}
}

func pol() InventoryPolicy { return InventoryPolicy{StaleAfter: 14 * 24 * time.Hour} }

func TestInventoryReasons(t *testing.T) {
	t.Parallel()
	main, svc, _ := inventoryRepo(t)
	clean := addWT(t, main, "clean")
	fresh := addWT(t, main, "fresh")
	os.WriteFile(filepath.Join(fresh, "new.txt"), []byte("x"), 0o644)
	stale := addWT(t, main, "stale")
	f := filepath.Join(stale, "old.txt")
	os.WriteFile(f, []byte("x"), 0o644)
	old := time.Now().Add(-30 * 24 * time.Hour)
	os.Chtimes(f, old, old)
	det := addWT(t, main, "det")
	exec.Command("git", "-C", det, "checkout", "--detach").Run()
	locked := addWT(t, main, "locked")
	os.WriteFile(filepath.Join(git.GitDirAt(locked), "index.lock"), nil, 0o644)

	p := pol()
	p.Reserved = []string{clean} // absolute form
	infos, err := svc.WorktreeInventory(context.Background(), p, false)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]string{
		main: {"main"}, clean: {"reserved"}, fresh: {"dirty-recent"},
		det: {"detached"}, locked: {"git-lock"},
	}
	for path, want := range cases {
		if got := find(t, infos, path).BlockedBy; !slices.Equal(got, want) {
			t.Errorf("%s blocked_by = %v, want %v", filepath.Base(path), got, want)
		}
	}
	s := find(t, infos, stale)
	if !s.Free || s.Recycle != "shelve" || s.Dirty == nil || s.Dirty.Untracked != 1 {
		t.Fatalf("stale = %+v", s)
	}
}

func TestInventoryReservedRelativeToMain(t *testing.T) {
	t.Parallel()
	main, svc, _ := inventoryRepo(t)
	wt := addWT(t, main, "rel")
	rel, _ := filepath.Rel(main, wt)
	p := pol()
	p.Reserved = []string{rel}
	infos, _ := svc.WorktreeInventory(context.Background(), p, false)
	if w := find(t, infos, wt); !w.Reserved || w.Free {
		t.Fatalf("rel reserved = %+v", w)
	}
}

func TestInventoryPausedOp(t *testing.T) {
	t.Parallel()
	main, svc, _ := inventoryRepo(t)
	wt := addWT(t, main, "paused")
	os.WriteFile(filepath.Join(git.GitDirAt(wt), "MERGE_HEAD"), []byte("0000000000000000000000000000000000000000\n"), 0o644)
	infos, _ := svc.WorktreeInventory(context.Background(), pol(), false)
	if got := find(t, infos, wt).BlockedBy; !slices.Equal(got, []string{"paused-op"}) {
		t.Fatalf("blocked_by = %v", got)
	}
}

func TestInventorySessionAndTUIFromRegistry(t *testing.T) {
	t.Parallel()
	main, svc, reg := inventoryRepo(t)
	a := addWT(t, main, "a")
	b := addWT(t, main, "b")
	sessionreg.Write(reg, "other", sessionreg.Registry{PID: 9, Worktree: b,
		Sessions: []sessionreg.Entry{{ID: "other/s1", Dir: a, Agent: "claude", State: "running"}}})
	infos, _ := svc.WorktreeInventory(context.Background(), pol(), false)
	if got := find(t, infos, a).BlockedBy; !slices.Equal(got, []string{"session"}) {
		t.Fatalf("a blocked_by = %v", got)
	}
	if got := find(t, infos, b).BlockedBy; !slices.Equal(got, []string{"tui"}) {
		t.Fatalf("b blocked_by = %v", got)
	}
	if find(t, infos, a).Dirty != nil {
		t.Fatal("status must be skipped for a blocked worktree")
	}
}

func TestInventoryFreeOrder(t *testing.T) {
	t.Parallel()
	main, svc, _ := inventoryRepo(t)
	s1 := addWT(t, main, "s1")
	s2 := addWT(t, main, "s2")
	c := addWT(t, main, "c")
	for i, wt := range []string{s1, s2} {
		f := filepath.Join(wt, "d.txt")
		os.WriteFile(f, []byte("x"), 0o644)
		at := time.Now().Add(-time.Duration(20+i*10) * 24 * time.Hour) // s2 older
		os.Chtimes(f, at, at)
	}
	infos, _ := svc.WorktreeInventory(context.Background(), pol(), true)
	var got []string
	for _, w := range infos {
		got = append(got, filepath.Base(w.Path))
	}
	if want := []string{filepath.Base(c), "s2", "s1"}; !slices.Equal(got, want) {
		t.Fatalf("free order = %v, want %v", got, want)
	}
}

func TestInventoryLastChangeSkipsDeletedPaths(t *testing.T) {
	t.Parallel()
	main, svc, _ := inventoryRepo(t)
	wt := addWT(t, main, "del")
	// BasicRepo commits a tracked file; delete it and add an old untracked one.
	tracked := firstTrackedFile(t, wt)
	os.Remove(filepath.Join(wt, tracked))
	f := filepath.Join(wt, "o.txt")
	os.WriteFile(f, []byte("x"), 0o644)
	old := time.Now().Add(-30 * 24 * time.Hour)
	os.Chtimes(f, old, old)
	infos, err := svc.WorktreeInventory(context.Background(), pol(), false)
	if err != nil {
		t.Fatal(err)
	}
	w := find(t, infos, wt)
	if w.Dirty == nil || w.Dirty.LastChange == nil || w.Dirty.LastChange.After(time.Now().Add(-29*24*time.Hour)) {
		t.Fatalf("dirty = %+v", w.Dirty)
	}
}

func firstTrackedFile(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "ls-files").Output()
	if err != nil || len(out) == 0 {
		t.Fatalf("ls-files: %v", err)
	}
	return strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
}

func TestInventoryMissingWorktree(t *testing.T) {
	t.Parallel()
	main, svc, _ := inventoryRepo(t)
	wt := addWT(t, main, "gone")
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	infos, err := svc.WorktreeInventory(context.Background(), pol(), false)
	if err != nil {
		t.Fatal(err)
	}
	w := find(t, infos, wt)
	if w.Free || !slices.Equal(w.BlockedBy, []string{"missing"}) {
		t.Fatalf("missing worktree = %+v", w)
	}
}

func TestInventoryOnlyOneWorktree(t *testing.T) {
	t.Parallel()
	main, svc, _ := inventoryRepo(t)
	a := addWT(t, main, "a")
	addWT(t, main, "b")
	infos, err := svc.inventory(context.Background(), pol(), a)
	if err != nil || len(infos) != 1 || !SameCheckout(infos[0].Path, a) {
		t.Fatalf("inventory(only) = %+v %v", infos, err)
	}
}

func TestPolicyFromConfigBadAge(t *testing.T) {
	t.Parallel()
	if _, err := PolicyFromConfig(config.AgentsConfig{StaleAfter: "soon"}); err == nil {
		t.Fatal("want error")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/domain -run 'TestInventory|TestPolicyFromConfig'`
Expected: FAIL — undefined `WorktreeInventory`.

- [ ] **Step 3: Implement**

Add to `Service` struct: `sessRegDir string // UseSessionRegistryDir override; "" = SessionRegistryDir()` and:

```go
func (s *Service) UseSessionRegistryDir(dir string) { s.mu.Lock(); s.sessRegDir = dir; s.mu.Unlock() }

func (s *Service) registryDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessRegDir != "" {
		return s.sessRegDir
	}
	return SessionRegistryDir()
}
```

```go
// wtinventory.go
package domain

// (imports: context, fmt, os, path/filepath, slices, sort, sync, time,
//  branchfilter, config, git, model, wtclaim)

func PolicyFromConfig(c config.AgentsConfig) (InventoryPolicy, error) {
	p := InventoryPolicy{Reserved: c.Reserved, AllowMain: c.AllowMain}
	age := c.StaleAfter
	if age == "" {
		age = "14d"
	}
	d, err := branchfilter.ParseAge(age)
	if err != nil {
		return p, fmt.Errorf("[agents] stale_after: %w", err)
	}
	p.StaleAfter = d
	return p, nil
}

func (s *Service) WorktreeInventory(ctx context.Context, pol InventoryPolicy, freeOnly bool) ([]WorktreeInfo, error) {
	out, err := s.inventory(ctx, pol, "")
	if err != nil || !freeOnly {
		return out, err
	}
	out = slices.DeleteFunc(out, func(w WorktreeInfo) bool { return !w.Free })
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Dirty, out[j].Dirty
		ac, bc := a == nil || a.LastChange == nil, b == nil || b.LastChange == nil
		if ac != bc {
			return ac // clean first
		}
		if ac {
			return false
		}
		return a.LastChange.Before(*b.LastChange) // oldest change first
	})
	return out, nil
}

func (s *Service) inventory(ctx context.Context, pol InventoryPolicy, only string) ([]WorktreeInfo, error) {
	wts, err := s.Worktrees(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now
	if pol.Now != nil {
		now = pol.Now
	}
	lv := readLive(s.registryDir())
	mainPath := ""
	if len(wts) > 0 {
		mainPath = wts[0].Path
	}
	reserved := resolveReserved(mainPath, pol.Reserved)
	out := make([]WorktreeInfo, 0, len(wts))
	for i, w := range wts {
		if w.Bare || w.Path == "" || (only != "" && !SameCheckout(w.Path, only)) {
			continue
		}
		out = append(out, s.cheapInfo(w, i == 0, pol, lv, reserved))
	}
	// git status only for the still-free ones, in parallel (the Runner's
	// LimitRunner caps concurrent git processes).
	var wg sync.WaitGroup
	for i := range out {
		if len(out[i].BlockedBy) > 0 {
			continue
		}
		wg.Add(1)
		go func(w *WorktreeInfo) {
			defer wg.Done()
			s.fillDirty(ctx, w, pol.StaleAfter, now())
		}(&out[i])
	}
	wg.Wait()
	for i := range out {
		w := &out[i]
		w.Free = len(w.BlockedBy) == 0
		switch {
		case !w.Free:
			w.Recycle = ""
		case w.Dirty != nil && w.Dirty.Staged+w.Dirty.Unstaged+w.Dirty.Untracked > 0:
			w.Recycle = "shelve"
		default:
			w.Recycle = "none"
		}
	}
	return out, nil
}

// resolveReserved turns configured paths into absolute cleaned ones.
func resolveReserved(mainPath string, list []string) []string {
	out := make([]string, 0, len(list))
	for _, p := range list {
		if !filepath.IsAbs(p) && mainPath != "" {
			p = filepath.Join(mainPath, p)
		}
		out = append(out, filepath.Clean(p))
	}
	return out
}

func (s *Service) cheapInfo(w model.Worktree, isMain bool, pol InventoryPolicy, lv liveView, reserved []string) WorktreeInfo {
	info := WorktreeInfo{Path: w.Path, Branch: w.Branch, Head: w.Head, Main: isMain, Detached: w.Detached || w.Branch == ""}
	block := func(r string) { info.BlockedBy = append(info.BlockedBy, r) }
	gitDir := git.GitDirAt(w.Path)
	if gitDir == "" {
		// The directory (or its .git) is gone: a prunable worktree. Nothing
		// else is knowable, and a claim would have nowhere to live.
		block("missing")
		return info
	}
	if isMain && !pol.AllowMain {
		block("main")
	}
	if info.Detached {
		block("detached")
	}
	if info.PausedOp = git.PausedOpIn(gitDir); info.PausedOp != "" {
		block("paused-op")
	}
	// This worktree's own git dir only: common-dir locks (packed-refs.lock
	// during a fetch) would block every worktree at once and say nothing
	// about this one.
	if info.GitLock = len(git.LockFiles(gitDir)) > 0; info.GitLock {
		block("git-lock")
	}
	for _, r := range reserved {
		if SameCheckout(r, w.Path) {
			info.Reserved = true
			block("reserved")
			break
		}
	}
	if c, ok := liveClaim(gitDir, lv); ok {
		info.Claim = &c
		block("claimed")
	}
	for _, t := range lv.tuis {
		if SameCheckout(t, w.Path) {
			info.TUI = true
			block("tui")
			break
		}
	}
	for d, refs := range lv.byDir {
		if !SameCheckout(d, w.Path) {
			continue
		}
		info.Sessions = append(info.Sessions, refs...)
		for _, r := range refs {
			if r.State == "running" && !slices.Contains(info.BlockedBy, "session") {
				block("session")
			}
		}
	}
	return info
}

func (s *Service) fillDirty(ctx context.Context, w *WorktreeInfo, staleAfter time.Duration, now time.Time) {
	st, err := s.repo.InDir(w.Path).Status(ctx)
	if err != nil {
		// Never read a failed status as "clean": that would hand an agent a
		// worktree whose changes we could not see.
		w.BlockedBy = append(w.BlockedBy, "status-failed")
		return
	}
	c := st.Counts()
	d := &DirtyInfo{Staged: c.Staged, Unstaged: c.Unstaged, Untracked: c.Untracked}
	for _, f := range st.Files {
		fi, err := os.Stat(filepath.Join(w.Path, f.Path))
		if err != nil {
			continue // deleted in the tree: nothing to date
		}
		if mt := fi.ModTime(); d.LastChange == nil || mt.After(*d.LastChange) {
			d.LastChange = &mt
		}
	}
	w.Dirty = d
	if len(st.Files) > 0 && (d.LastChange == nil || now.Sub(*d.LastChange) < staleAfter) {
		w.BlockedBy = append(w.BlockedBy, "dirty-recent")
	}
}
```

A dirty tree whose every path is deleted has `LastChange == nil`: it blocks as `dirty-recent` (no evidence it is old — safer). The free-order sort treats "no LastChange" as clean, which only matters for free worktrees, where every dirty one has a date.

Conflicted files count in `len(st.Files)` but not in Staged/Unstaged/Untracked — a conflicted tree is already blocked as `paused-op`, so the `recycle` switch never sees one.

`liveClaim` is defined in Task 7; for this task add a stub in `wtclaim.go` of the domain that Task 7 replaces:

```go
// liveClaim reports gitDir's claim when its session is alive (Task 7 adds the sweep).
func liveClaim(gitDir string, lv liveView) (ClaimInfo, bool) {
	if gitDir == "" {
		return ClaimInfo{}, false
	}
	c, ok, err := wtclaim.Read(gitDir)
	if err != nil || !ok {
		return ClaimInfo{}, false
	}
	return ClaimInfo{Session: c.Session, Agent: c.Agent, Since: c.Since, Note: c.Note}, true
}
```

Verify `model.FileStatus` has `.Path` (used by `statusRows` — yes) and that untracked directories come back as the dir path (stat works on a dir).

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/domain -run 'TestInventory|TestPolicyFromConfig' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/domain/wtinventory.go internal/domain/wtinventory_test.go internal/domain/wtclaim.go internal/domain/service.go
git commit -m "feat(domain): worktree inventory with free verdict for agents"
```

---

### Task 7: Domain — claims (claim, release, dead sweep, marks, reserve)

**Files:**
- Modify: `internal/domain/wtclaim.go`
- Test: `internal/domain/wtclaim_test.go`

**Interfaces:**
- Consumes: Tasks 3, 5, 6.
- Produces:
```go
var ErrNotAgent = errors.New("only an agent running inside gg can claim")
type SessionNotLiveError struct{ ID string }                 // Error(): "session <id> is not running in any gg"
var ErrNotHolder = errors.New("the claim belongs to another session")
type NotFreeError struct{ Path string; BlockedBy []string }   // Error(): "<path> is not free: a, b"
var ErrUnknownWorktree = errors.New("no such worktree")
func (s *Service) ClaimWorktree(ctx context.Context, path, sessionID, note string, pol InventoryPolicy) error
func (s *Service) ReleaseWorktree(ctx context.Context, path, sessionID string, force bool) (released bool, err error)
type WorktreeMark struct { Reserved bool; Claim *ClaimInfo }
func (s *Service) WorktreeMarks(wts []model.Worktree, reserved []string) map[string]WorktreeMark  // key = worktree Path as listed; wts = the list the caller just loaded
func (s *Service) SetWorktreeReserved(ctx context.Context, cfgPath string, current []string, path string, on bool) error
```

- [ ] **Step 1: Write the failing tests**

```go
func TestClaimAndRelease(t *testing.T) {
	t.Parallel()
	main, svc, reg := inventoryRepo(t)
	wt := addWT(t, main, "c")
	sessionreg.Write(reg, "p", sessionreg.Registry{PID: 1, Sessions: []sessionreg.Entry{{ID: "p/s1", Dir: main, Agent: "claude", State: "running"}}})
	ctx := context.Background()
	if err := svc.ClaimWorktree(ctx, wt, "", "", pol()); !errors.Is(err, ErrNotAgent) {
		t.Fatalf("no session = %v", err)
	}
	if err := svc.ClaimWorktree(ctx, wt, "p/s1", "https://x/1", pol()); err != nil {
		t.Fatal(err)
	}
	var nf *NotFreeError
	if err := svc.ClaimWorktree(ctx, wt, "p/s1", "", pol()); !errors.As(err, &nf) || !slices.Contains(nf.BlockedBy, "claimed") {
		t.Fatalf("second claim = %v", err)
	}
	infos, _ := svc.WorktreeInventory(ctx, pol(), false)
	if w := find(t, infos, wt); w.Claim == nil || w.Claim.Note != "https://x/1" || w.Claim.Agent != "claude" {
		t.Fatalf("claim = %+v (agent must come from the registry entry)", w.Claim)
	}
	if _, err := svc.ReleaseWorktree(ctx, wt, "p/s9", false); !errors.Is(err, ErrNotHolder) {
		t.Fatalf("non-holder release = %v", err)
	}
	if ok, err := svc.ReleaseWorktree(ctx, wt, "p/s1", false); err != nil || !ok {
		t.Fatalf("release = %v %v", ok, err)
	}
	if ok, _ := svc.ReleaseWorktree(ctx, wt, "p/s1", false); ok {
		t.Fatal("second release must report no claim")
	}
}

func TestClaimSweepsClaimOfDeadRegistry(t *testing.T) {
	t.Parallel()
	main, svc, reg := inventoryRepo(t)
	wt := addWT(t, main, "d")
	sessionreg.Write(reg, "dead", sessionreg.Registry{PID: 1, Sessions: []sessionreg.Entry{{ID: "dead/s1", Dir: main, State: "running"}}})
	ctx := context.Background()
	if err := svc.ClaimWorktree(ctx, wt, "dead/s1", "", pol()); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * sessionreg.LiveWindow)
	os.Chtimes(filepath.Join(reg, "dead.json"), past, past)
	sessionreg.Write(reg, "p", sessionreg.Registry{PID: 2, Sessions: []sessionreg.Entry{{ID: "p/s2", Dir: main, State: "running"}}})
	if err := svc.ClaimWorktree(ctx, wt, "p/s2", "", pol()); err != nil {
		t.Fatalf("claim over a dead claim = %v", err)
	}
}

func TestStalledRegistryKeepsClaim(t *testing.T) {
	t.Parallel()
	main, svc, reg := inventoryRepo(t)
	wt := addWT(t, main, "stall")
	proc := fmt.Sprintf("%d-1", os.Getpid()) // a live process, not this one's ProcTag
	sessionreg.Write(reg, proc, sessionreg.Registry{PID: os.Getpid(), Sessions: []sessionreg.Entry{{ID: proc + "/s1", Dir: main, State: "running"}}})
	ctx := context.Background()
	if err := svc.ClaimWorktree(ctx, wt, proc+"/s1", "", pol()); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * sessionreg.LiveWindow)
	os.Chtimes(filepath.Join(reg, proc+".json"), past, past) // the TUI froze
	infos, _ := svc.WorktreeInventory(ctx, pol(), false)
	if w := find(t, infos, wt); w.Claim == nil {
		t.Fatal("a stalled registry of a LIVE process must keep its claim")
	}
}

func TestClaimRefusesDeadSession(t *testing.T) {
	t.Parallel()
	main, svc, _ := inventoryRepo(t)
	wt := addWT(t, main, "nolive")
	var nl *SessionNotLiveError
	if err := svc.ClaimWorktree(context.Background(), wt, "0-1/s1", "", pol()); !errors.As(err, &nl) {
		t.Fatalf("claim by a dead session = %v", err)
	}
	if _, err := os.Stat(filepath.Join(git.GitDirAt(wt), wtclaim.FileName)); !os.IsNotExist(err) {
		t.Fatal("no claim file may be written")
	}
}

func TestClaimRefusesMissingWorktree(t *testing.T) {
	t.Parallel()
	main, svc, reg := inventoryRepo(t)
	wt := addWT(t, main, "gone")
	sessionreg.Write(reg, "p", sessionreg.Registry{PID: 1, Sessions: []sessionreg.Entry{{ID: "p/s1", Dir: main, State: "running"}}})
	os.RemoveAll(wt)
	var nf *NotFreeError
	if err := svc.ClaimWorktree(context.Background(), wt, "p/s1", "", pol()); !errors.As(err, &nf) || !slices.Contains(nf.BlockedBy, "missing") {
		t.Fatalf("claim on a missing worktree = %v", err)
	}
	if _, err := os.Stat(wtclaim.FileName); !os.IsNotExist(err) {
		t.Fatal("gg-claim written into the cwd")
	}
}

func TestEmptyClaimFileDiesAfterGrace(t *testing.T) {
	t.Parallel()
	main, svc, _ := inventoryRepo(t)
	wt := addWT(t, main, "empty")
	f := filepath.Join(git.GitDirAt(wt), wtclaim.FileName)
	os.WriteFile(f, nil, 0o644)
	infos, _ := svc.WorktreeInventory(context.Background(), pol(), false)
	if find(t, infos, wt).Claim == nil {
		t.Fatal("a fresh empty claim is mid-write: alive")
	}
	past := time.Now().Add(-time.Minute)
	os.Chtimes(f, past, past)
	infos, _ = svc.WorktreeInventory(context.Background(), pol(), false)
	if find(t, infos, wt).Claim != nil {
		t.Fatal("an old empty claim is a crashed claimer's: dead")
	}
}

func TestExitedSessionClaimIsDead(t *testing.T) {
	t.Parallel()
	main, svc, reg := inventoryRepo(t)
	wt := addWT(t, main, "e")
	sessionreg.Write(reg, "p", sessionreg.Registry{PID: 1, Sessions: []sessionreg.Entry{{ID: "p/s1", Dir: main, State: "running"}}})
	ctx := context.Background()
	svc.ClaimWorktree(ctx, wt, "p/s1", "", pol())
	sessionreg.Write(reg, "p", sessionreg.Registry{PID: 1, Sessions: []sessionreg.Entry{{ID: "p/s1", Dir: main, State: "exited"}}})
	infos, _ := svc.WorktreeInventory(ctx, pol(), false)
	if w := find(t, infos, wt); w.Claim != nil || !w.Free {
		t.Fatalf("exited-session claim must be dead: %+v", w)
	}
	if _, err := os.Stat(filepath.Join(git.GitDirAt(wt), wtclaim.FileName)); !os.IsNotExist(err) {
		t.Fatal("dead claim not swept")
	}
}

func TestReleaseForce(t *testing.T) {
	t.Parallel()
	main, svc, reg := inventoryRepo(t)
	wt := addWT(t, main, "f")
	sessionreg.Write(reg, "p", sessionreg.Registry{PID: 1, Sessions: []sessionreg.Entry{{ID: "p/s1", Dir: main, State: "running"}}})
	ctx := context.Background()
	svc.ClaimWorktree(ctx, wt, "p/s1", "", pol())
	if ok, err := svc.ReleaseWorktree(ctx, wt, "", true); err != nil || !ok {
		t.Fatalf("force release = %v %v", ok, err)
	}
}

func TestWorktreeMarksAndReserve(t *testing.T) {
	t.Parallel()
	main, svc, _ := inventoryRepo(t)
	wt := addWT(t, main, "m")
	cfg := filepath.Join(main, ".gg.toml")
	ctx := context.Background()
	if err := svc.SetWorktreeReserved(ctx, cfg, nil, wt, true); err != nil {
		t.Fatal(err)
	}
	c, _ := config.Load("", cfg)
	rel, _ := filepath.Rel(main, wt)
	if !slices.Equal(c.Agents.Reserved, []string{filepath.ToSlash(rel)}) {
		t.Fatalf("reserved = %q, want relative %q", c.Agents.Reserved, rel)
	}
	wts, _ := svc.Worktrees(ctx)
	marks := svc.WorktreeMarks(wts, c.Agents.Reserved)
	reservedSeen := false
	for p, mk := range marks {
		if SameCheckout(p, wt) && mk.Reserved {
			reservedSeen = true
		}
	}
	if !reservedSeen {
		t.Fatalf("marks = %+v", marks)
	}
	if err := svc.SetWorktreeReserved(ctx, cfg, c.Agents.Reserved, wt, false); err != nil {
		t.Fatal(err)
	}
	c, _ = config.Load("", cfg)
	if len(c.Agents.Reserved) != 0 {
		t.Fatalf("reserved after unreserve = %q", c.Agents.Reserved)
	}
}
```



- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/domain -run 'TestClaim|TestExited|TestReleaseForce|TestWorktreeMarks'`
Expected: FAIL — undefined `ClaimWorktree`.

- [ ] **Step 3: Implement** (replace the Task 6 stub)

```go
package domain

// (imports: context, errors, fmt, os, path/filepath, slices, strings, time,
//  config, git, model, wtclaim)

var (
	ErrNotAgent        = errors.New("only an agent running inside gg can claim")
	ErrNotHolder       = errors.New("the claim belongs to another session")
	ErrUnknownWorktree = errors.New("no such worktree")
)

type SessionNotLiveError struct{ ID string }

func (e *SessionNotLiveError) Error() string { return "session " + e.ID + " is not running in any gg" }

type NotFreeError struct {
	Path      string
	BlockedBy []string
}

func (e *NotFreeError) Error() string {
	return e.Path + " is not free: " + strings.Join(e.BlockedBy, ", ")
}

type WorktreeMark struct {
	Reserved bool
	Claim    *ClaimInfo
}

// emptyClaimGrace: a claim file with no session is a claimer between O_EXCL
// and its write — alive this long, then a crashed claimer's.
const emptyClaimGrace = 10 * time.Second

func claimDead(c wtclaim.Claim, gitDir string, lv liveView) bool {
	if c.Session == "" {
		age, ok := wtclaim.Age(gitDir)
		return ok && age > emptyClaimGrace
	}
	return sessionDead(c.Session, lv)
}

// liveClaim reports gitDir's claim when alive. A dead one is removed under
// the claim lock after a re-read, so a sweeper never deletes a claim someone
// created after its first read.
func liveClaim(gitDir string, lv liveView) (ClaimInfo, bool) {
	if gitDir == "" {
		return ClaimInfo{}, false
	}
	c, ok, err := wtclaim.Read(gitDir)
	if err != nil || !ok {
		return ClaimInfo{}, false
	}
	if claimDead(c, gitDir, lv) {
		_ = wtclaim.WithLock(gitDir, func() error {
			again, ok, err := wtclaim.Read(gitDir)
			if err == nil && ok && again == c && claimDead(again, gitDir, lv) {
				return wtclaim.Remove(gitDir)
			}
			return nil
		})
		return ClaimInfo{}, false
	}
	return ClaimInfo{Session: c.Session, Agent: c.Agent, Since: c.Since, Note: c.Note}, true
}

func (s *Service) worktreeAt(ctx context.Context, path string) (model.Worktree, error) {
	wts, err := s.Worktrees(ctx)
	if err != nil {
		return model.Worktree{}, err
	}
	for _, w := range wts {
		if SameCheckout(w.Path, path) {
			return w, nil
		}
	}
	return model.Worktree{}, ErrUnknownWorktree
}

func (s *Service) ClaimWorktree(ctx context.Context, path, sessionID, note string, pol InventoryPolicy) error {
	if sessionID == "" {
		return ErrNotAgent
	}
	lv := readLive(s.registryDir())
	if !lv.running[sessionID] {
		return &SessionNotLiveError{ID: sessionID}
	}
	infos, err := s.inventory(ctx, pol, path) // this worktree only; sweeps a dead claim
	if err != nil {
		return err
	}
	if len(infos) == 0 {
		return ErrUnknownWorktree
	}
	target := infos[0]
	if !target.Free {
		return &NotFreeError{Path: target.Path, BlockedBy: target.BlockedBy}
	}
	gitDir := git.GitDirAt(target.Path)
	if gitDir == "" {
		return &NotFreeError{Path: target.Path, BlockedBy: []string{"missing"}}
	}
	return wtclaim.WithLock(gitDir, func() error {
		// Between the inventory read and the lock a dead claim may have
		// appeared dead-then-live or been swept; decide again, locked.
		if c, ok, err := wtclaim.Read(gitDir); err == nil && ok {
			if !claimDead(c, gitDir, lv) {
				return &NotFreeError{Path: target.Path, BlockedBy: []string{"claimed"}}
			}
			if err := wtclaim.Remove(gitDir); err != nil {
				return err
			}
		}
		err := wtclaim.Create(gitDir, wtclaim.Claim{Session: sessionID, Agent: lv.agents[sessionID],
			Since: time.Now().UTC().Truncate(time.Second), Note: note})
		if errors.Is(err, wtclaim.ErrClaimed) {
			return &NotFreeError{Path: target.Path, BlockedBy: []string{"claimed"}}
		}
		return err
	})
}

func (s *Service) ReleaseWorktree(ctx context.Context, path, sessionID string, force bool) (bool, error) {
	w, err := s.worktreeAt(ctx, path)
	if err != nil {
		return false, err
	}
	gitDir := git.GitDirAt(w.Path)
	if gitDir == "" {
		return false, nil
	}
	released := false
	err = wtclaim.WithLock(gitDir, func() error {
		c, ok := liveClaimLocked(gitDir, readLive(s.registryDir()))
		if !ok {
			return nil
		}
		if !force && c.Session != sessionID {
			return ErrNotHolder
		}
		released = true
		return wtclaim.Remove(gitDir)
	})
	return released, err
}

// liveClaimLocked is liveClaim for a caller already holding the claim lock
// (filelock is not re-entrant).
func liveClaimLocked(gitDir string, lv liveView) (ClaimInfo, bool) {
	c, ok, err := wtclaim.Read(gitDir)
	if err != nil || !ok {
		return ClaimInfo{}, false
	}
	if claimDead(c, gitDir, lv) {
		_ = wtclaim.Remove(gitDir)
		return ClaimInfo{}, false
	}
	return ClaimInfo{Session: c.Session, Agent: c.Agent, Since: c.Since, Note: c.Note}, true
}

// WorktreeMarks is the TUI's cheap view: reserve + live claim per worktree,
// no git status, over the list the caller just loaded.
func (s *Service) WorktreeMarks(wts []model.Worktree, reserved []string) map[string]WorktreeMark {
	if len(wts) == 0 {
		return nil
	}
	res := resolveReserved(wts[0].Path, reserved)
	lv := readLive(s.registryDir())
	out := map[string]WorktreeMark{}
	for _, w := range wts {
		var m WorktreeMark
		for _, r := range res {
			if SameCheckout(r, w.Path) {
				m.Reserved = true
			}
		}
		if c, ok := liveClaim(git.GitDirAt(w.Path), lv); ok {
			m.Claim = &c
		}
		if m.Reserved || m.Claim != nil {
			out[w.Path] = m
		}
	}
	return out
}

// SetWorktreeReserved adds (on) or removes path in [agents] reserved of the
// repo config at cfgPath. current is the configured (repo-only) list; new
// entries are stored relative to the main worktree (slash-separated) so a
// committed .gg.toml works on every machine.
func (s *Service) SetWorktreeReserved(ctx context.Context, cfgPath string, current []string, path string, on bool) error {
	w, err := s.worktreeAt(ctx, path)
	if err != nil {
		return err
	}
	wts, _ := s.Worktrees(ctx)
	mainPath := wts[0].Path
	abs := resolveReserved(mainPath, current)
	var next []string
	for i, r := range abs {
		if !SameCheckout(r, w.Path) {
			next = append(next, current[i])
		}
	}
	if on {
		entry := w.Path
		if rel, err := filepath.Rel(mainPath, w.Path); err == nil {
			entry = filepath.ToSlash(rel)
		}
		next = append(next, entry)
	}
	return config.SetAgentsReserved(cfgPath, next)
}
```

`wtclaim.Claim` holds only comparable fields, so `again == c` compiles; the TOML round trip of the same bytes yields equal `time.Time` values.

Note `resolveReserved` must handle slash-separated relative entries on Windows: `filepath.Join` accepts `/` there, so no change needed.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/domain -count=1`
Expected: PASS (whole package — the inventory tests from Task 6 now run with the sweep).

- [ ] **Step 5: Commit**

```bash
gg add internal/domain/wtclaim.go internal/domain/wtclaim_test.go
git commit -m "feat(domain): session-bound worktree claims with dead-claim sweep; reserve marks"
```

---

### Task 8: CLI — `list --json/--free`, `claim`, `release`, `reserve`, `unreserve`

**Files:**
- Modify: `internal/cli/worktree.go` (dispatch + `cmdWorktreeList` flags + usage strings)
- Create: `internal/cli/worktree_agents.go`
- Modify: `internal/cli/main_test.go` (pin `XDG_STATE_HOME`, same shape as Task 5's domain TestMain)
- Test: `internal/cli/worktree_agents_test.go`

**Interfaces:**
- Consumes: Tasks 6–7 domain API; `matchWorktreeArg(wts, arg)` (exists in `worktree.go`).
- Produces: CLI behaviour per spec §7.

- [ ] **Step 1: Write the failing tests**

```go
package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// agentEnv pins XDG_STATE_HOME (the CLI reads the registry from
// domain.SessionRegistryDir() = <state>/gg/sessions) and a running session.
func agentEnv(t *testing.T, dir string) {
	t.Helper()
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	reg := filepath.Join(state, "gg", "sessions")
	// Written by hand: the frontends may not import sessionreg (archtest).
	body, _ := json.Marshal(map[string]any{"pid": 1, "worktree": "", "sessions": []map[string]string{
		{"id": "p/s1", "dir": dir, "agent": "claude", "state": "running"}}})
	if err := os.MkdirAll(reg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reg, "p.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GG_SESSION_ID", "p/s1")
}

func TestWorktreeListJSONAndFree(t *testing.T) {
	dir := newCLIRepo(t)
	agentEnv(t, dir)
	wt := cliWorktree(t, dir, "a", "wt-a")
	code, out, errb := runCLI(t, dir, "worktree", "list", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	var infos []map[string]any
	if err := json.Unmarshal([]byte(out), &infos); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if len(infos) != 2 || infos[0]["main"] != true {
		t.Fatalf("infos = %v", infos)
	}
	for _, k := range []string{"path", "branch", "head", "dirty", "paused_op", "git_lock", "tui", "sessions", "reserved", "claim", "free", "blocked_by", "recycle"} {
		if _, ok := infos[1][k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
	code, out, _ = runCLI(t, dir, "worktree", "list", "--free")
	if code != 0 || !strings.Contains(out, filepath.Base(wt)) || strings.Contains(out, "main\t") {
		t.Fatalf("--free text = %q", out)
	}
}

func TestWorktreeClaimReleaseRoundTrip(t *testing.T) {
	dir := newCLIRepo(t)
	agentEnv(t, dir)
	wt := cliWorktree(t, dir, "a", "wt-a")
	if code, _, errb := runCLI(t, dir, "worktree", "claim", "--note", "https://x/1", wt); code != 0 {
		t.Fatalf("claim exit %d: %s", code, errb)
	}
	code, _, errb := runCLI(t, dir, "worktree", "claim", wt)
	if code != 1 || !strings.Contains(errb, "claimed") {
		t.Fatalf("second claim = %d %q", code, errb)
	}
	if code, out, _ := runCLI(t, dir, "worktree", "release", wt); code != 0 || !strings.Contains(out, "released") {
		t.Fatalf("release = %d %q", code, out)
	}
}

func TestWorktreeClaimWithoutSession(t *testing.T) {
	dir := newCLIRepo(t)
	wt := cliWorktree(t, dir, "a", "wt-a")
	t.Setenv("GG_SESSION_ID", "")
	code, _, errb := runCLI(t, dir, "worktree", "claim", wt)
	if code != 2 || !strings.Contains(errb, "only an agent running inside gg can claim") {
		t.Fatalf("claim = %d %q", code, errb)
	}
}

func TestWorktreeClaimDeadSession(t *testing.T) {
	dir := newCLIRepo(t)
	agentEnv(t, dir)
	wt := cliWorktree(t, dir, "a", "wt-a")
	t.Setenv("GG_SESSION_ID", "0-1/s9")
	code, _, errb := runCLI(t, dir, "worktree", "claim", wt)
	if code != 2 || !strings.Contains(errb, "is not running in any gg") {
		t.Fatalf("claim by a dead session = %d %q", code, errb)
	}
}

func TestWorktreeClaimRelativePath(t *testing.T) {
	dir := newCLIRepo(t)
	agentEnv(t, dir)
	wt := cliWorktree(t, dir, "a", "wt-a")
	rel, _ := filepath.Rel(dir, wt)
	if code, _, errb := runCLI(t, dir, "worktree", "claim", rel+string(filepath.Separator)); code != 0 {
		t.Fatalf("claim rel exit %d: %s", code, errb)
	}
	if code, _, _ := runCLI(t, dir, "worktree", "claim", filepath.Join(dir, "not-a-wt")); code != 1 {
		t.Fatalf("unknown path exit = %d, want 1", code)
	}
}

func TestWorktreeReleaseNoClaim(t *testing.T) {
	dir := newCLIRepo(t)
	agentEnv(t, dir)
	wt := cliWorktree(t, dir, "a", "wt-a")
	code, out, _ := runCLI(t, dir, "worktree", "release", wt)
	if code != 0 || !strings.Contains(out, "no claim") {
		t.Fatalf("release = %d %q", code, out)
	}
}

func TestWorktreeReleaseNonHolder(t *testing.T) {
	dir := newCLIRepo(t)
	agentEnv(t, dir)
	wt := cliWorktree(t, dir, "a", "wt-a")
	runCLI(t, dir, "worktree", "claim", wt)
	t.Setenv("GG_SESSION_ID", "p/other")
	if code, _, _ := runCLI(t, dir, "worktree", "release", wt); code != 1 {
		t.Fatalf("non-holder release exit = %d", code)
	}
	if code, _, _ := runCLI(t, dir, "worktree", "release", "--force", wt); code != 0 {
		t.Fatalf("force release exit = %d", code)
	}
}

func TestWorktreeReserveUnreserve(t *testing.T) {
	dir := newCLIRepo(t)
	agentEnv(t, dir)
	wt := cliWorktree(t, dir, "a", "wt-a")
	if code, _, errb := runCLI(t, dir, "worktree", "reserve", wt); code != 0 {
		t.Fatalf("reserve exit %d: %s", code, errb)
	}
	code, _, errb := runCLI(t, dir, "worktree", "claim", wt)
	if code != 1 || !strings.Contains(errb, "reserved") {
		t.Fatalf("claim reserved = %d %q", code, errb)
	}
	if code, _, _ := runCLI(t, dir, "worktree", "unreserve", wt); code != 0 {
		t.Fatalf("unreserve exit %d", code)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".gg.toml"))
	if strings.Contains(string(raw), "wt-a") {
		t.Fatalf(".gg.toml still lists it:\n%s", raw)
	}
}

func TestWorktreeListBadStaleAfter(t *testing.T) {
	dir := newCLIRepo(t)
	os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte("[agents]\nstale_after = \"soon\"\n"), 0o644)
	code, _, errb := runCLI(t, dir, "worktree", "list", "--json")
	if code != 1 || !strings.Contains(errb, "stale_after") {
		t.Fatalf("bad stale_after = %d %q", code, errb)
	}
}
```

These tests use `t.Setenv`, so no `t.Parallel()`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/cli -run 'TestWorktree(ListJSON|Claim|Release|Reserve|ListBad)'`
Expected: FAIL — unknown flag `--json` / unknown subcommand.

- [ ] **Step 3: Implement**

`worktree.go` dispatch: add `case "claim"`, `"release"`, `"reserve"`, `"unreserve"` calling the functions below; update both usage strings to `<list|add|remove|move|rename|prune|recycle|claim|release|reserve|unreserve>`. `case "list": return cmdWorktreeList(svc, args[1:], stdout, stderr)`.

```go
// worktree_agents.go
package cli

// (imports: context, encoding/json, errors, flag, fmt, io, os, path/filepath,
//  time, config, domain)

// agentsConfig loads [agents] plus the active repo config path it came
// from — anchored on the MAIN worktree, never the cwd worktree's copy of
// .gg.toml (reserve writes must land in one place for the whole repo).
func agentsConfig(svc *domain.Service) (config.AgentsConfig, string, error) {
	ctx := context.Background()
	wts, err := svc.Worktrees(ctx)
	if err != nil || len(wts) == 0 {
		return config.AgentsConfig{}, "", fmt.Errorf("no worktrees: %v", err)
	}
	mainPath := wts[0].Path
	path := config.ActiveRepoConfigPath(filepath.Join(mainPath, ".gg.toml"), config.PrivateRepoPath(mainPath))
	cfg, err := config.Load(config.DefaultGlobalPath(), path)
	if err != nil {
		return config.AgentsConfig{}, "", err
	}
	return cfg.Agents, path, nil
}

type jsonDirty struct {
	Staged     int        `json:"staged"`
	Unstaged   int        `json:"unstaged"`
	Untracked  int        `json:"untracked"`
	LastChange *time.Time `json:"last_change"`
}
type jsonSession struct {
	ID    string `json:"id"`
	Agent string `json:"agent"`
	State string `json:"state"`
}
type jsonClaim struct {
	Session string    `json:"session"`
	Agent   string    `json:"agent"`
	Since   time.Time `json:"since"`
	Note    string    `json:"note"`
}
type jsonWorktree struct {
	Path      string        `json:"path"`
	Branch    string        `json:"branch"`
	Head      string        `json:"head"`
	Main      bool          `json:"main"`
	Detached  bool          `json:"detached"`
	Dirty     *jsonDirty    `json:"dirty"`
	PausedOp  string        `json:"paused_op"`
	GitLock   bool          `json:"git_lock"`
	TUI       bool          `json:"tui"`
	Sessions  []jsonSession `json:"sessions"`
	Reserved  bool          `json:"reserved"`
	Claim     *jsonClaim    `json:"claim"`
	Free      bool          `json:"free"`
	BlockedBy []string      `json:"blocked_by"`
	Recycle   *string       `json:"recycle"`
}

func toJSON(w domain.WorktreeInfo) jsonWorktree {
	j := jsonWorktree{Path: w.Path, Branch: w.Branch, Head: w.Head, Main: w.Main, Detached: w.Detached,
		PausedOp: w.PausedOp, GitLock: w.GitLock, TUI: w.TUI, Reserved: w.Reserved, Free: w.Free,
		BlockedBy: w.BlockedBy, Sessions: []jsonSession{}}
	if j.BlockedBy == nil {
		j.BlockedBy = []string{}
	}
	if d := w.Dirty; d != nil {
		j.Dirty = &jsonDirty{Staged: d.Staged, Unstaged: d.Unstaged, Untracked: d.Untracked, LastChange: d.LastChange}
	}
	for _, s := range w.Sessions {
		j.Sessions = append(j.Sessions, jsonSession{ID: s.ID, Agent: s.Agent, State: s.State})
	}
	if c := w.Claim; c != nil {
		j.Claim = &jsonClaim{Session: c.Session, Agent: c.Agent, Since: c.Since, Note: c.Note}
	}
	if w.Recycle != "" {
		r := w.Recycle
		j.Recycle = &r
	}
	return j
}

func cmdWorktreeList(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("worktree list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "one JSON object per worktree with the facts an orchestrating agent needs")
	freeOnly := fs.Bool("free", false, "only worktrees an agent may take, best first")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx := context.Background()
	if !*asJSON && !*freeOnly {
		wts, err := svc.Worktrees(ctx)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		for _, w := range wts {
			printWorktreeLine(stdout, w.Branch, w.Path)
		}
		return 0
	}
	ac, _, err := agentsConfig(svc)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	pol, err := domain.PolicyFromConfig(ac)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	infos, err := svc.WorktreeInventory(ctx, pol, *freeOnly)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if *asJSON {
		out := make([]jsonWorktree, 0, len(infos))
		for _, w := range infos {
			out = append(out, toJSON(w))
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	}
	for _, w := range infos {
		printWorktreeLine(stdout, w.Branch, w.Path)
	}
	return 0
}

func printWorktreeLine(w io.Writer, branch, path string) {
	if branch == "" {
		branch = "(detached)"
	}
	fmt.Fprintf(w, "%s\t%s\n", branch, path)
}

func cmdWorktreeClaim(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("worktree claim", flag.ContinueOnError)
	fs.SetOutput(stderr)
	note := fs.String("note", "", "why the worktree is taken (e.g. the issue URL); shown in the TUI")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg worktree claim [--note <text>] <path>")
		return 2
	}
	sid := os.Getenv("GG_SESSION_ID")
	if sid == "" {
		fmt.Fprintln(stderr, "worktree claim: "+domain.ErrNotAgent.Error())
		return 2
	}
	path, code := resolveWorktreeArg(svc, fs.Arg(0), "claim", stderr)
	if code != 0 {
		return code
	}
	ac, _, err := agentsConfig(svc)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	pol, err := domain.PolicyFromConfig(ac)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if err := svc.ClaimWorktree(context.Background(), path, sid, *note, pol); err != nil {
		fmt.Fprintln(stderr, "worktree claim:", err)
		var nl *domain.SessionNotLiveError
		if errors.As(err, &nl) {
			return 2
		}
		return 1
	}
	fmt.Fprintf(stdout, "claimed %s\n", path)
	return 0
}

func cmdWorktreeRelease(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("worktree release", flag.ContinueOnError)
	fs.SetOutput(stderr)
	force := fs.Bool("force", false, "release a claim held by another session")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg worktree release [--force] <path>")
		return 2
	}
	path, code := resolveWorktreeArg(svc, fs.Arg(0), "release", stderr)
	if code != 0 {
		return code
	}
	ok, err := svc.ReleaseWorktree(context.Background(), path, os.Getenv("GG_SESSION_ID"), *force)
	if err != nil {
		fmt.Fprintln(stderr, "worktree release:", err)
		return 1
	}
	if !ok {
		fmt.Fprintf(stdout, "no claim on %s\n", path)
		return 0
	}
	fmt.Fprintf(stdout, "released %s\n", path)
	return 0
}

func cmdWorktreeReserve(svc *domain.Service, args []string, stdout, stderr io.Writer, on bool) int {
	verb := map[bool]string{true: "reserve", false: "unreserve"}[on]
	if len(args) != 1 {
		fmt.Fprintf(stderr, "usage: gg worktree %s <path>\n", verb)
		return 2
	}
	path, code := resolveWorktreeArg(svc, args[0], verb, stderr)
	if code != 0 {
		return code
	}
	ac, cfgPath, err := agentsConfig(svc)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if err := svc.SetWorktreeReserved(context.Background(), cfgPath, ac.Reserved, path, on); err != nil {
		fmt.Fprintf(stderr, "worktree %s: %v\n", verb, err)
		return 1
	}
	fmt.Fprintf(stdout, "%sd %s\n", verb, path)
	return 0
}

// resolveWorktreeArg maps a user/agent-typed path to the listed worktree. A
// relative arg is taken against workdir (the CLI's "here"), never the
// process cwd — matchWorktreeArg's own filepath.Abs uses the latter.
func resolveWorktreeArg(svc *domain.Service, workdir, arg, verb string, stderr io.Writer) (string, int) {
	wts, err := svc.Worktrees(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return "", 1
	}
	if !filepath.IsAbs(arg) {
		arg = filepath.Join(workdir, arg)
	}
	m := matchWorktreeArg(wts, filepath.Clean(arg))
	if m == nil {
		fmt.Fprintf(stderr, "worktree %s: no worktree at %q\n", verb, arg)
		return "", 1
	}
	return m.Path, 0
}
```

Delete the old `cmdWorktreeList(svc, stdout, stderr)` in `worktree.go`. Thread `workdir` from `cmdWorktree` into `cmdWorktreeClaim`/`Release`/`Reserve` (as `cmdWorktreeAdd` receives it) and pass it to every `resolveWorktreeArg` call; the function signatures above gain a leading `workdir string` parameter accordingly (e.g. `cmdWorktreeClaim(svc, workdir, args, stdout, stderr)`). `matchWorktreeArg` compares `wts[i].Path == target` exactly, so the cleaned absolute arg matches. Imports gain `errors`; `os` stays (for `GG_SESSION_ID`).

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/cli -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/cli/worktree.go internal/cli/worktree_agents.go internal/cli/worktree_agents_test.go
git commit -m "feat(cli): gg worktree list --json/--free, claim, release, reserve, unreserve"
```

---

### Task 9: gitwatch — claim files refresh the Worktrees source

**Files:**
- Modify: `internal/gitwatch/plan.go`
- Test: `internal/gitwatch/plan_test.go`

**Interfaces:**
- Produces: two extra `Group`s when `Worktrees` is enabled.

- [ ] **Step 1: Write the failing test**

```go
func TestPlanWorktreesWatchesClaims(t *testing.T) {
	t.Parallel()
	groups := Plan("/c", "/c", []Source{Worktrees})
	hit := func(dir, base string) bool {
		for _, g := range groups {
			if g.Dir == dir || (g.Recursive && strings.HasPrefix(dir, g.Dir+string(filepath.Separator))) {
				if slices.Contains(g.Match(base), Worktrees) {
					return true
				}
			}
		}
		return false
	}
	if !hit(filepath.Join("/c", "worktrees", "wt-a"), "gg-claim") {
		t.Fatal("a linked worktree's gg-claim must refresh Worktrees")
	}
	if !hit("/c", "gg-claim") {
		t.Fatal("the main worktree's gg-claim must refresh Worktrees")
	}
	if hit(filepath.Join("/c", "worktrees", "wt-a"), "index") {
		t.Fatal("index churn must not refresh Worktrees")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/gitwatch -run TestPlanWorktreesWatchesClaims`
Expected: FAIL on the first assertion.

- [ ] **Step 3: Implement** — inside `if on[Worktrees] {…}` after the existing group:

```go
		claim := func(base string) []Source {
			if base == "gg-claim" {
				return []Source{Worktrees}
			}
			return nil
		}
		// An agent's claim lives in the worktree's own git dir; index and
		// HEAD churn in the same dirs match nothing.
		groups = append(groups,
			Group{Dir: filepath.Join(commonDir, "worktrees"), Recursive: true, Match: claim},
			Group{Dir: commonDir, Match: claim},
		)
```

Check whether an existing non-recursive group already watches `commonDir` (e.g. for Branches' `packed-refs`/`HEAD`); two groups on one dir must both be consulted — read `watcher.go` dispatch (`line ~98`) and, if it stops at the first matching group, merge the claim predicate into that group's `Match` instead.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/gitwatch`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/gitwatch
git commit -m "feat(gitwatch): a worktree claim file refreshes the Worktrees source"
```

---

### Task 10: TUI — publisher, marks, menu rows, hint, recycle picker

**Files:**
- Modify: `internal/tui/run.go` (start `PublishSessions`)
- Modify: `internal/tui/model.go` (field `worktreeMarks map[string]domain.WorktreeMark`; set at `:1612`, `:1670` sites + publish worktree; apply payload at `:1923`)
- Modify: `internal/tui/load.go` (dataLoadedMsg `worktreeMarks`), `internal/tui/source.go` (`worktreesPayload.marks`)
- Create: `internal/tui/worktree_marks.go` (row prefix, hint, menu rows, confirm)
- Modify: `internal/tui/view.go` (`worktreeRows` prefix; bottom bar `add(m.worktreeClaimHint())`)
- Modify: `internal/tui/agent_start_popup.go` (`sessionMenuRows` worktree branch appends mark rows)
- Modify: `internal/tui/recycle_worktree.go` (suffix + confirm for claimed/reserved)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml`
- Modify: help/footer sources (find with `grep -rn '"Recycle a worktree"' internal/tui` and the Worktrees footer builder)
- Test: `internal/tui/worktree_marks_test.go`

**Interfaces:**
- Consumes: `domain.PublishSessions`, `domain.SessionRegistryDir`, `svc.WorktreeMarks(wts, reserved)`, `svc.ReleaseWorktree`, `svc.SetWorktreeReserved`, `m.cfg.Agents.Reserved`, `m.repoConfigPath` (verify it is the main-worktree-anchored ACTIVE repo config; if it follows the cwd worktree, resolve the main one the way Task 8's `agentsConfig` does).
- Produces: `func (m Model) worktreeMarkPrefix(path string) string`, `func (m Model) worktreeClaimHint() string`, `func (m Model) worktreeMarkRows(wt model.Worktree) []actionRow`.

- [ ] **Step 1: Write the failing tests**

```go
package tui

func TestWorktreeRowShowsMarks(t *testing.T) {
	t.Parallel()
	m := worktreeMarksModel(t) // helper below
	rows := m.worktreeRows(m.worktreeEntries())
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "⚑ claude") || !strings.Contains(joined, "⊘") {
		t.Fatalf("rows = %q", rows)
	}
}

func TestWorktreeClaimHintOnSelectedRow(t *testing.T) {
	t.Parallel()
	m := worktreeMarksModel(t)
	m.focus = panelWorktrees
	m.sel[panelWorktrees] = 1 // the claimed worktree
	if h := m.worktreeClaimHint(); !strings.Contains(h, "https://x/1") || !strings.Contains(h, "claude") {
		t.Fatalf("hint = %q", h)
	}
}

func TestWorktreeMarkMenuRows(t *testing.T) {
	t.Parallel()
	m := worktreeMarksModel(t)
	m.focus = panelWorktrees
	m.sel[panelWorktrees] = 1
	ids := rowIDs(m.sessionMenuRows())
	if !slices.Contains(ids, "worktree-release-claim") || !slices.Contains(ids, "worktree-reserve") {
		t.Fatalf("ids = %v", ids)
	}
	m.sel[panelWorktrees] = 2 // the reserved one
	if ids := rowIDs(m.sessionMenuRows()); !slices.Contains(ids, "worktree-unreserve") {
		t.Fatalf("ids = %v", ids)
	}
}

func TestRecyclePickerMarksClaimedAndReserved(t *testing.T) {
	t.Parallel()
	m := worktreeMarksModel(t)
	m = m.openRecyclePicker("loose", "")
	var labels []string
	for _, r := range m.actionMenu.rows {
		labels = append(labels, r.label)
	}
	joined := strings.Join(labels, "\n")
	if !strings.Contains(joined, "(claimed by claude)") || !strings.Contains(joined, "(reserved)") {
		t.Fatalf("picker rows = %q", labels)
	}
}

func TestReleaseClaimAsksFirst(t *testing.T) {
	t.Parallel()
	m := worktreeMarksModel(t)
	m.focus = panelWorktrees
	m.sel[panelWorktrees] = 1
	var row actionRow
	for _, r := range m.sessionMenuRows() {
		if r.id == "worktree-release-claim" {
			row = r
		}
	}
	mm, _ := row.run(m)
	if mm.(Model).modal == nil {
		t.Fatal("release must confirm first")
	}
}

// worktreeMarksModel: main + two linked worktrees; marks injected directly.
func worktreeMarksModel(t *testing.T) Model {
	t.Helper()
	m := New(nil)
	m.worktrees = []model.Worktree{
		{Path: "/r/main", Branch: "main"},
		{Path: "/r/wt-a", Branch: "a"},
		{Path: "/r/wt-b", Branch: "b"},
	}
	m.currentWorktree = "/r/main"
	m.worktreeMarks = map[string]domain.WorktreeMark{
		"/r/wt-a": {Claim: &domain.ClaimInfo{Session: "p/s1", Agent: "claude", Since: time.Now(), Note: "https://x/1"}},
		"/r/wt-b": {Reserved: true},
	}
	return m
}

func rowIDs(rs []actionRow) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.id)
	}
	return out
}
```

(If `New(nil)` is not the package's idiom for a model without a repo, build the model the way `worktree_view_test.go` does — copy its constructor.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui -run 'TestWorktreeRowShowsMarks|TestWorktreeClaimHint|TestWorktreeMarkMenuRows|TestReleaseClaimAsksFirst'`
Expected: FAIL — `m.worktreeMarks undefined`.

- [ ] **Step 3: Implement**

`worktree_marks.go`:

```go
package tui

// (imports: context, fmt, path/filepath, tea, domain, i18n, model, engine)

// worktreeMarkPrefix is the Worktrees-row mark column: ⚑ <agent> for a live
// claim, ⊘ for a reserve, "" otherwise.
func (m Model) worktreeMarkPrefix(path string) string {
	mk, ok := m.worktreeMarks[path]
	if !ok {
		return ""
	}
	s := ""
	if mk.Claim != nil {
		agent := mk.Claim.Agent
		if agent == "" {
			agent = i18n.T("agent")
		}
		s += "⚑ " + agent + "  "
	}
	if mk.Reserved {
		s += "⊘  "
	}
	return s
}

// worktreeClaimHint puts the selected worktree's claim in the bottom bar.
func (m Model) worktreeClaimHint() string {
	if m.focus != panelWorktrees {
		return ""
	}
	e, ok := m.selectedWorktreeEntry()
	if !ok || e.sess != "" || e.task != "" {
		return ""
	}
	mk := m.worktreeMarks[m.worktrees[e.wt].Path]
	if mk.Claim == nil {
		if mk.Reserved {
			return i18n.T("⊘ reserved: agents never take this worktree")
		}
		return ""
	}
	c := mk.Claim
	h := i18n.T("⚑ claimed by %s since %s", c.Agent, c.Since.Local().Format("2006-01-02 15:04"))
	if c.Note != "" {
		h += " · " + c.Note
	}
	return h
}

func (m Model) worktreeMarkRows(wt model.Worktree) []actionRow {
	path := wt.Path
	mk := m.worktreeMarks[path]
	var rows []actionRow
	if mk.Claim != nil {
		agent := mk.Claim.Agent
		rows = append(rows, actionRow{id: "worktree-release-claim", label: i18n.T("Release claim (%s)", agent),
			run: func(m Model) (tea.Model, tea.Cmd) { return m.confirmReleaseClaim(path, agent), nil }})
	}
	if mk.Reserved {
		rows = append(rows, actionRow{id: "worktree-unreserve", label: i18n.T("Unreserve"),
			run: func(m Model) (tea.Model, tea.Cmd) { return m.setReserved(path, false) }})
	} else {
		rows = append(rows, actionRow{id: "worktree-reserve", label: i18n.T("Reserve (no agents)"),
			run: func(m Model) (tea.Model, tea.Cmd) { return m.setReserved(path, true) }})
	}
	return rows
}

func (m Model) confirmReleaseClaim(path, agent string) Model {
	dir := shortWorktreeName(path)
	svc := m.svc
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "worktree-release-claim",
			Prompt:  i18n.T("%s is working in %s. Release its claim?", agent, dir),
			Options: []string{"Release", "Cancel"},
		},
		sel: 1,
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt != "Release" {
				return m, nil
			}
			if _, err := svc.ReleaseWorktree(context.Background(), path, "", true); err != nil {
				m.statusMsg = i18n.T("release claim: %s", err.Error())
				return m, nil
			}
			m.statusMsg = i18n.T("released the claim on %s", dir)
			return m.reloadSourcesCmd([]sourceKey{srcWorktrees}, reloadOpts{})
		},
	}
	return m
}

func (m Model) setReserved(path string, on bool) (tea.Model, tea.Cmd) {
	if m.repoConfigPath == "" {
		m.statusMsg = i18n.T("no repo config to write")
		return m, nil
	}
	if err := m.svc.SetWorktreeReserved(context.Background(), m.repoConfigPath, m.cfg.Agents.Reserved, path, on); err != nil {
		m.statusMsg = i18n.T("reserve: %s", err.Error())
		return m, nil
	}
	return m.reloadSourcesCmd([]sourceKey{srcWorktrees}, reloadOpts{})
}
```

The reserve write changes the repo config; confirm the TUI reloads `m.cfg` after a config write (the Settings writers do — follow what the branch-filter popup does after `config.SetBranchFilter`, `branch_filter_popup.go:325`) so `m.cfg.Agents.Reserved` and the marks agree. The `svc` captured in `confirmReleaseClaim` must be the live one at resolve time — prefer `m.svc` inside `onResolve` (it receives the current `m`).

`sessionMenuRows` (`agent_start_popup.go`), in the final `if wt, ok := m.selectedWorktree(); ok && wt.Path != ""` branch: append `m.worktreeMarkRows(wt)...` to the returned rows.

`view.go worktreeRows`: `out = append(out, marker+m.worktreeMarkPrefix(w.Path)+branch+"  "+w.Path)`. Bottom bar: `add(m.worktreeClaimHint())` right after `add(m.commitBranchHint())`.

Data: `source.go` `worktreesPayload` gains `marks map[string]domain.WorktreeMark`; in the `srcWorktrees` loader add `marks := svc.WorktreeMarks(wts, reserved)` (the list the loader just read — no second `git worktree list`) where `reserved` is captured from `m.cfg.Agents.Reserved` when the load cmd is built (the loader closure already captures model state — follow how it captures `svc`). `model.go:1923` apply: `m.worktreeMarks = p.marks`. `load.go`: same for the initial snapshot (`dataLoadedMsg.worktreeMarks`, filled next to `worktrees` using `cfg.Agents.Reserved`, applied at `model.go:1695`).

Session changes must refresh marks: in the existing sessions-changed handler (`waitSessionsCmd` result), add `srcWorktrees` to the reload when the session count or states changed — find it with `grep -n 'waitSessionsCmd' internal/tui/*.go`.

Publisher (`run.go`, before `p := tea.NewProgram`):

```go
	pubCtx, pubCancel := context.WithCancel(context.Background())
	go domain.PublishSessions(pubCtx, domain.SessionRegistryDir(), publishedWorktree)
	defer pubCancel()
```

and in `worktree_marks.go`:

```go
var publishedWT atomic.Value // string: the TUI's current worktree for the session registry

func publishedWorktree() string { s, _ := publishedWT.Load().(string); return s }
```

with `publishedWT.Store(m.currentWorktree)` right after each of the two assignments (`model.go:1612`, `:1670`). Tests never call `Run`, so no registry is written by the suite.

Recycle picker (`recycle_worktree.go openRecyclePicker`): after the `(agent session running)` suffix, append `"  " + i18n.T("(claimed by %s)", agent)` for a live claim and `"  " + i18n.T("(reserved)")` for a reserve; pass a `guarded` bool (live || claimed || reserved) into `recycleInto`, whose confirm prompt becomes `i18n.T("%s is in use (%s). Recycle it anyway?", dir, reason)` for claimed/reserved, keeping the existing session prompt unchanged.

Footer/help: add `[.] reserve / release claim` to the Worktrees footer and help the way `[x] remove` was added for session sub-rows (see memory: footer-only ids in the action-menu coverage gate — run `go test ./internal/tui -run 'Coverage|Footer|Help'` and add `worktree-reserve`, `worktree-unreserve`, `worktree-release-claim` wherever that gate lists menu-only ids).

i18n: add every new key to `internal/i18n/lang/{ja,ko,zh,ru}.toml`: `agent`, `⊘ reserved: agents never take this worktree`, `⚑ claimed by %s since %s`, `Release claim (%s)`, `Unreserve`, `Reserve (no agents)`, `%s is working in %s. Release its claim?`, `Release`, `release claim: %s`, `released the claim on %s`, `no repo config to write`, `reserve: %s`, `(claimed by %s)`, `(reserved)`, `%s is in use (%s). Recycle it anyway?`, plus the footer/help strings. Follow the `adding-translations` skill (verb agreement gate).

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/tui -count=1`
Expected: PASS, including the i18n/option-vocab/menu-label gates.

- [ ] **Step 5: Commit**

```bash
gg add internal/tui internal/i18n
git commit -m "feat(tui): worktree claim/reserve marks, menu rows, hint; publish the session registry"
```

---

### Task 11: Docs, skill, full gate

**Files:**
- Modify: `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go` (`Version` 106 → 107, or next free if main moved)
- Modify: `CHANGELOG.md`, `README.md` (Configuration `[agents]`; worktree commands), `CLAUDE.md` (two map rows: `sessionreg`, `wtclaim`), `docs/CLAUDE-details.md` (claim/registry gotchas)

- [ ] **Step 1: using-gg section** — add under the worktree section:

```markdown
### Picking a worktree for another agent

Only an agent running inside gg (it has `GG_SESSION_ID`) can claim.

1. `gg worktree list --free --json` — free worktrees, best first. Each has
   `recycle`: pass it as `--on-dirty` (`none` = clean, omit the flag;
   `shelve` = stale changes go to a shelf).
2. `gg worktree claim --note <issue-url> <path>` — atomic; exit 1 with the
   `blocked_by` reasons if someone else got there first. Pick the next one.
3. `gg worktree recycle [--on-dirty=shelve] <path> <branch>`.
4. When done: `gg worktree release <path>` (exit 0 even with no claim).

A claim ends by itself when your session ends. `gg worktree list --json`
without `--free` lists every worktree with `blocked_by` reasons.
`git-lock` can appear for one listing while another reader's `git status`
holds `index.lock` — list again before giving up on a worktree.
```

- [ ] **Step 2: bump `agentskill.Version`**, CHANGELOG entry (new section at the top, matching the file's format), README, CLAUDE.md rows:

```
| `sessionreg` | Machine-wide registry of live agent sessions: each TUI publishes `<proc>.json` (sessions + its worktree) under XDG state, mtime liveness + sweep (the steer presence precedent). DAG leaf. |
| `wtclaim` | An agent's claim on a worktree: one O_EXCL TOML file (`gg-claim`) in the worktree's git dir; liveness is the caller's question. DAG leaf. |
```

- [ ] **Step 3: Sync the dogfood skill**: `go build -o bin/gg ./cmd/gg && ./bin/gg init --update` (in the worktree; never `git add -A` — `bin/` holds the binary).

- [ ] **Step 4: Full gate**

Run: `./test.sh race 2>&1 | tee /tmp/claude-1000/-work-gigagit/63e7bf78-bedf-425b-857e-5933228d7cb7/scratchpad/race.log; grep -n 'all green' /tmp/claude-1000/-work-gigagit/63e7bf78-bedf-425b-857e-5933228d7cb7/scratchpad/race.log`
Expected: the log contains `all green` (a `| tail` exit code is not proof).

- [ ] **Step 5: Live check** — build `bin/gg`, start the TUI in the worktree under `./tui-capture.sh`, start an agent session (`sh`) in a linked worktree from the Branches menu, in that session run `gg worktree claim --note test <other-wt>`, capture the Worktrees tab (⚑ marker + hint), kill the session, capture again (marker gone), run `gg worktree list --json` from outside (claim null, session exited).

- [ ] **Step 6: Commit**

```bash
gg add CHANGELOG.md README.md CLAUDE.md docs/CLAUDE-details.md internal/agentskill .claude/skills
git commit -m "docs: worktree inventory for agents — skill, changelog, readme, package map"
```

Then the final whole-branch review (a read-only review subagent is allowed), and ask the user before `gg merge`.
