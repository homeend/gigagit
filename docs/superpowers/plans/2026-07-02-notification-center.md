# Notification Center Implementation Plan (Stage 2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On repo load gg runs cheap health checks; unresolved findings surface as a blinking red `! N notice` status segment and a `!` dialog whose actions fix the finding through real engine ops — first check: a big repo without a commit-graph file gets a one-keystroke "write it + keep it fresh" recommendation.

**Architecture:** A new `domain.RepoHealth` query (stat-level facts + one config lookup) feeds a TUI-side notice builder (`internal/tui/notify.go`); notices live on the `Model`, blink via a self-stopping 800ms tick, and act through two new engine ops — `WriteCommitGraph` (streams `git commit-graph write --reachable`) and `SetGitConfig` (the generic sibling of `SetIdentity`) — chained with the existing `pendingPushTags` pattern. Per-repo "never again" dismissals persist through stage 1's `promptstate` store (`DismissNotice`, already shipped).

**Tech Stack:** Go 1.26, Bubble Tea, real-git `t.TempDir()` tests + `gitexec.FakeRunner` argv assertions.

**Spec:** `docs/superpowers/specs/2026-07-02-related-prompts-notifications-git-config-design.md` (Stage 2 section).

## Global Constraints

- Work on branch `feat/notification-center` in the worktree `.claude/worktrees/notification-center` — never the shared checkout. Use worktree-relative paths.
- TDD: failing test first → implement → pass → gofmt → commit.
- `internal/tui`/`internal/cli` must NOT import `internal/git` (archtest). **This forces one spec deviation:** the spec wrote `SetGitConfig{Scope, Key, Value}`, but `git.ConfigScope` cannot appear in a TUI-constructed op — use `Global bool` exactly like the existing `SetIdentity`. Document nothing else as changed.
- A git verb is one invocation; argv via `gitcmd`; run via `r.Runner.Run`/`.Stream`.
- Operations never block on a human; both new ops are decision-free; `LockMode()` = `repogate.Read` (a commit-graph is a derived cache under `.git/objects`; a config write touches neither refs nor the working tree).
- Frontends run ops via `domain.Execute` (TUI: `m.startOp`), reads via domain queries — never ad-hoc git calls from the TUI.
- The blink is style alternation on a dedicated ~800ms tick that runs ONLY while unread notices exist (terminal-native blink escapes are unreliable). Opening the dialog marks all notices read and the tick stops re-arming.
- `!` is a global key with the same routing as `g`/`G`: only reachable in navigation mode, therefore inert while any filter/text field captures input, swallowed by any open popup.
- Notice states: unread → read (session) → dismissed (session, "Not now" — re-evaluated on next load) or **never for this repo** (persisted via `promptstate.Store.DismissNotice`, keyed by git common dir + notice id).
- The Settings "Commit-graph" row and the notice's "write + keep fresh" action must share ONE code path.
- Never trap: every popup esc-closes.
- New global keys need a `footerBinding` in `footer.go` AND a row in `helpContent()` (`help.go`) — `TestHelpFooterCoverage` fails otherwise.
- Commit trailer on every commit:
  ```
  Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n
  ```
- `gofmt -w` every touched file before committing. `./test.sh race` green before the final task ends.

---

### Task 1: `git.CommitGraphWrite` verb + GitOps method

**Files:**
- Create: `internal/git/commitgraph.go`
- Modify: `internal/engine/gitops.go` (add one method to the `GitOps` interface)
- Test: `internal/git/commitgraph_test.go`

**Interfaces:**
- Consumes: `gitcmd.New`, `r.Runner.Stream` (existing; see `internal/git/worktree.go:34` `AddWorktree` for the exact streaming-verb shape), test helpers `newTestRepo(t)`/`writeFile`/`commitAll` in the git package (see `internal/git/archive_test.go`).
- Produces: `func (r *Repo) CommitGraphWrite(ctx context.Context, onLine func(string)) error` — added verbatim to the `GitOps` interface so Task 2's op can call it. Note: `gitexec.Stream` forwards stdout lines only (stderr is buffered separately), and `git commit-graph write` prints progress to stderr — so `onLine` will usually stay silent; the TUI's elapsed-time busy line covers long runs.

- [ ] **Step 1: Write the failing test**

Create `internal/git/commitgraph_test.go`:

```go
package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestCommitGraphWriteArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	r := &Repo{Runner: f}
	if err := r.CommitGraphWrite(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.Calls[0].Argv, " "); got != "commit-graph write --reachable" {
		t.Fatalf("argv = %q, want 'commit-graph write --reachable'", got)
	}
}

func TestCommitGraphWriteRealRepoCreatesFile(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	writeFile(t, dir, "a.txt", "hello\n")
	commitAll(t, dir, "one commit")

	if err := repo.CommitGraphWrite(context.Background(), nil); err != nil {
		t.Fatalf("CommitGraphWrite: %v", err)
	}
	cg := filepath.Join(dir, ".git", "objects", "info", "commit-graph")
	if _, err := os.Stat(cg); err != nil {
		t.Fatalf("commit-graph file not written at %s: %v", cg, err)
	}
}
```

Note: if `newTestRepo`/`writeFile`/`commitAll` have different names or shapes in this package, match the helpers `archive_test.go` actually uses — do not invent new ones.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/git/ -run TestCommitGraphWrite`
Expected: FAIL to build — `CommitGraphWrite` undefined.

- [ ] **Step 3: Implement the verb**

Create `internal/git/commitgraph.go`:

```go
package git

import (
	"context"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// CommitGraphWrite writes the repo's commit-graph file
// (`git commit-graph write --reachable`) — the derived cache that makes
// ordered commit walks (--date-order paging) near-flat on big repos. Output
// lines are forwarded to onLine (nil allowed; git's progress goes to stderr,
// which Stream buffers rather than forwards, so onLine is usually silent);
// the write is cancellable via ctx — it can take ~a minute on a
// million-commit repo.
func (r *Repo) CommitGraphWrite(ctx context.Context, onLine func(string)) error {
	if onLine == nil {
		onLine = func(string) {}
	}
	argv := gitcmd.New("commit-graph").Arg("write", "--reachable").ToArgv()
	_, err := r.Runner.Stream(ctx, "git commit-graph write", argv, onLine)
	return err
}
```

In `internal/engine/gitops.go`, add to the `GitOps` interface next to `ConfigSet` (line ~99):

```go
	CommitGraphWrite(ctx context.Context, onLine func(string)) error
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/git/ -run TestCommitGraphWrite && go build ./...`
Expected: PASS, build clean (`*git.Repo` still satisfies `GitOps`).

- [ ] **Step 5: gofmt and commit**

```bash
gofmt -w internal/git/commitgraph.go internal/git/commitgraph_test.go internal/engine/gitops.go
git add internal/git/commitgraph.go internal/git/commitgraph_test.go internal/engine/gitops.go
git commit -m "feat(git): CommitGraphWrite verb — git commit-graph write --reachable

One invocation, streamed + ctx-cancellable (a million-commit repo takes ~a
minute). Backs the notification center's commit-graph recommendation.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n"
```

---

### Task 2: `engine.WriteCommitGraph` + `engine.SetGitConfig` ops

**Files:**
- Create: `internal/engine/write_commit_graph.go`
- Create: `internal/engine/set_git_config.go`
- Test: `internal/engine/write_commit_graph_test.go`
- Test: `internal/engine/set_git_config_test.go`

**Interfaces:**
- Consumes: `GitOps.CommitGraphWrite` (Task 1), `GitOps.ConfigSet` (existing), `OpDeps.emit`, `Progress`/`GitLine`/`Done` events, `repogate.Read`. Exemplar: `internal/engine/set_identity.go` (read it first — `SetGitConfig` is its generic sibling and must match its shape). Test helper `newRepo(t) (string, *git.Repo)` in `internal/engine/ops_basic_test.go:15`.
- Produces (used by Tasks 5–6):
  - `type WriteCommitGraph struct{}` — decision-free op, `LockMode() repogate.Read`.
  - `type SetGitConfig struct { Key, Value string; Global bool }` — decision-free op, `LockMode() repogate.Read`. `Global` false = repo-local scope (mirrors `SetIdentity.Global`; the TUI cannot import `git.ConfigScope`).

- [ ] **Step 1: Write the failing tests**

Create `internal/engine/write_commit_graph_test.go`:

```go
package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteCommitGraphCreatesFile(t *testing.T) {
	dir, repo := newRepo(t)
	res, err := WriteCommitGraph{}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "objects", "info", "commit-graph")); err != nil {
		t.Fatalf("commit-graph file missing: %v", err)
	}
}

func TestWriteCommitGraphEmitsProgressAndDone(t *testing.T) {
	_, repo := newRepo(t)
	events := make(chan Event, 16)
	if _, err := (WriteCommitGraph{}).Run(context.Background(), OpDeps{Repo: repo, Events: events}); err != nil {
		t.Fatalf("run: %v", err)
	}
	close(events)
	var sawProgress, sawDone bool
	for e := range events {
		switch e.(type) {
		case Progress:
			sawProgress = true
		case Done:
			sawDone = true
		}
	}
	if !sawProgress || !sawDone {
		t.Fatalf("events: progress=%v done=%v, want both", sawProgress, sawDone)
	}
}
```

Create `internal/engine/set_git_config_test.go`:

```go
package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/git"
)

func TestSetGitConfigWritesLocalScope(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	_, repo := newRepo(t)
	ctx := context.Background()

	res, err := SetGitConfig{Key: "fetch.writeCommitGraph", Value: "true"}.Run(ctx, OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	v, set, _ := repo.ConfigGet(ctx, git.ConfigLocal, "fetch.writeCommitGraph")
	if !set || v != "true" {
		t.Fatalf("local fetch.writeCommitGraph = %q set=%v, want true", v, set)
	}
	if _, gset, _ := repo.ConfigGet(ctx, git.ConfigGlobal, "fetch.writeCommitGraph"); gset {
		t.Fatal("global was written; expected local-only")
	}
}

func TestSetGitConfigWritesGlobalScope(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	_, repo := newRepo(t)
	ctx := context.Background()

	if _, err := (SetGitConfig{Key: "fetch.writeCommitGraph", Value: "true", Global: true}).Run(ctx, OpDeps{Repo: repo}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, gset, _ := repo.ConfigGet(ctx, git.ConfigGlobal, "fetch.writeCommitGraph"); !gset {
		t.Fatal("global fetch.writeCommitGraph not set")
	}
	if _, lset, _ := repo.ConfigGet(ctx, git.ConfigLocal, "fetch.writeCommitGraph"); lset {
		t.Fatal("local was written; expected global-only")
	}
}

func TestSetGitConfigRequiresKey(t *testing.T) {
	_, repo := newRepo(t)
	if _, err := (SetGitConfig{Value: "x"}).Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("empty key must error")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/engine/ -run 'TestWriteCommitGraph|TestSetGitConfig'`
Expected: FAIL to build — types undefined.

- [ ] **Step 3: Implement both ops**

Create `internal/engine/write_commit_graph.go`:

```go
package engine

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/repogate"
)

// WriteCommitGraph writes the repo's commit-graph file so ordered commit
// walks (the Commits panel's --date-order paging) go from O(walk) to
// near-flat. Decision-free; backs the notification center's commit-graph
// recommendation and the Settings "Commit-graph" row.
type WriteCommitGraph struct{}

// LockMode: the commit-graph is a derived cache under .git/objects/info —
// it touches neither refs nor the working tree.
func (op WriteCommitGraph) LockMode() repogate.Mode { return repogate.Read }

func (op WriteCommitGraph) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "writing commit-graph", Detail: "git commit-graph write --reachable"})
	err := deps.Repo.CommitGraphWrite(ctx, func(line string) {
		deps.emit(ctx, GitLine{Raw: line})
	})
	if err != nil {
		return Result{}, fmt.Errorf("write commit-graph: %w", err)
	}
	res := Result{Summary: "commit-graph written", Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = WriteCommitGraph{}
```

Create `internal/engine/set_git_config.go`:

```go
package engine

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/repogate"
)

// SetGitConfig writes one git config key at one scope — the generic sibling
// of SetIdentity (which stays as the dedicated identity-pair op). Backs the
// notification center's "enable fetch.writeCommitGraph" action; stage 3's
// config explorer reuses it. Decision-free: the scope is fixed before any
// work (Global true = ~/.gitconfig, false = this repo's .git/config — a
// bool, not git.ConfigScope, so frontends can construct it without
// importing internal/git).
type SetGitConfig struct {
	Key    string
	Value  string
	Global bool
}

// LockMode: a config write touches neither refs nor the work tree (and a
// global write is not even repo-scoped), so the lightest reservation suffices.
func (op SetGitConfig) LockMode() repogate.Mode { return repogate.Read }

func (op SetGitConfig) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Key == "" {
		return Result{}, fmt.Errorf("set git config: key is required")
	}
	scope := git.ConfigLocal
	where := "in this repo"
	if op.Global {
		scope = git.ConfigGlobal
		where = "globally"
	}
	deps.emit(ctx, Progress{Step: "setting git config", Detail: op.Key + " " + where})
	if err := deps.Repo.ConfigSet(ctx, scope, op.Key, op.Value); err != nil {
		return Result{}, fmt.Errorf("set git config: %s: %w", op.Key, err)
	}
	res := Result{Summary: fmt.Sprintf("%s = %s set %s", op.Key, op.Value, where), Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = SetGitConfig{}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/engine/ -run 'TestWriteCommitGraph|TestSetGitConfig'`
Expected: PASS (5 tests). Then `go test ./internal/engine/` — full package stays green.

- [ ] **Step 5: gofmt and commit**

```bash
gofmt -w internal/engine/write_commit_graph.go internal/engine/set_git_config.go internal/engine/write_commit_graph_test.go internal/engine/set_git_config_test.go
git add internal/engine/write_commit_graph.go internal/engine/set_git_config.go internal/engine/write_commit_graph_test.go internal/engine/set_git_config_test.go
git commit -m "feat(engine): WriteCommitGraph + SetGitConfig ops

WriteCommitGraph streams git commit-graph write --reachable (LockMode Read —
a derived cache under .git/objects). SetGitConfig is the generic sibling of
SetIdentity (Global bool, not git.ConfigScope, so frontends can construct it
without importing internal/git); stage 3's config explorer will reuse it.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n"
```

---

### Task 3: `model.RepoHealth` + `domain.RepoHealth` query

**Files:**
- Create: `internal/model/health.go`
- Create: `internal/domain/repohealth.go`
- Test: `internal/domain/repohealth_test.go`

**Interfaces:**
- Consumes: the domain `query(ctx, s, "name", fn)` wrapper (see `internal/domain/identity.go` — the exemplar for a config-reading query), `s.repo.GitCommonDir` (returns an ABSOLUTE path — `--path-format=absolute`, `internal/git/worktree.go:61`), `s.repo.ConfigGet`, test helper `newRealRepo(t) (string, *Service)` (see `internal/domain/identity_test.go`).
- Produces (used by Tasks 4–6):
  - `model.RepoHealth{ GitCommonDir string; PackBytes int64; HasCommitGraph bool; WriteCommitGraphSet bool; WriteCommitGraphValue string }`
  - `func (s *Service) RepoHealth(ctx context.Context) (model.RepoHealth, error)` — runs under a Read reservation like every query.

- [ ] **Step 1: Write the failing test**

Create `internal/domain/repohealth_test.go`:

```go
package domain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/git"
)

func TestRepoHealthFreshRepo(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	dir, svc := newRealRepo(t)

	h, err := svc.RepoHealth(context.Background())
	if err != nil {
		t.Fatalf("RepoHealth: %v", err)
	}
	if h.GitCommonDir == "" || !filepath.IsAbs(h.GitCommonDir) {
		t.Fatalf("GitCommonDir = %q, want absolute path", h.GitCommonDir)
	}
	if h.HasCommitGraph {
		t.Fatal("fresh repo must not report a commit-graph")
	}
	if h.WriteCommitGraphSet {
		t.Fatal("fetch.writeCommitGraph is unset in a fresh repo")
	}
	_ = dir
}

func TestRepoHealthSeesCommitGraphAndConfig(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	// Write a commit-graph with real git and set the config key locally.
	cmd := exec.Command("git", "commit-graph", "write", "--reachable")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit-graph write: %v\n%s", err, out)
	}
	if err := svc.Repo().ConfigSet(ctx, git.ConfigLocal, "fetch.writeCommitGraph", "true"); err != nil {
		t.Fatalf("config set: %v", err)
	}

	h, err := svc.RepoHealth(ctx)
	if err != nil {
		t.Fatalf("RepoHealth: %v", err)
	}
	if !h.HasCommitGraph {
		t.Fatal("must detect the commit-graph file")
	}
	if !h.WriteCommitGraphSet || h.WriteCommitGraphValue != "true" {
		t.Fatalf("WriteCommitGraph = %q set=%v, want true/true", h.WriteCommitGraphValue, h.WriteCommitGraphSet)
	}
}

func TestRepoHealthCountsPackBytes(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	dir, svc := newRealRepo(t)

	// Repack everything into a pack file so objects/pack has real content.
	cmd := exec.Command("git", "repack", "-ad")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git repack: %v\n%s", err, out)
	}

	h, err := svc.RepoHealth(context.Background())
	if err != nil {
		t.Fatalf("RepoHealth: %v", err)
	}
	if h.PackBytes <= 0 {
		t.Fatalf("PackBytes = %d after repack, want > 0", h.PackBytes)
	}
}
```

Note: `newRealRepo(t)` must produce a repo with at least one commit for `repack`/`commit-graph write` to have content — check its definition; if it creates an empty repo, add one commit in the test the way `identity_test.go`'s siblings do.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/domain/ -run TestRepoHealth`
Expected: FAIL to build — `RepoHealth` undefined.

- [ ] **Step 3: Implement**

Create `internal/model/health.go`:

```go
package model

// RepoHealth is the cheap repo-health snapshot behind the notification
// center: stat-level filesystem facts plus one config lookup — no expensive
// git walks, safe to run in the background on every repo load.
type RepoHealth struct {
	GitCommonDir          string // absolute git common dir (doubles as the per-repo dismissal key)
	PackBytes             int64  // total size of *.pack under objects/pack
	HasCommitGraph        bool   // objects/info/commit-graph file OR commit-graphs/ chain dir present
	WriteCommitGraphSet   bool   // fetch.writeCommitGraph set in local or global scope
	WriteCommitGraphValue string // the set value ("" when unset)
}
```

Create `internal/domain/repohealth.go`:

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// RepoHealth gathers the cheap facts behind the notification center's health
// checks, under a Read reservation: pack size and commit-graph presence are
// filesystem stats on the git common dir; fetch.writeCommitGraph is read at
// both explicit scopes so an inherited global "true" also counts as set.
func (s *Service) RepoHealth(ctx context.Context) (model.RepoHealth, error) {
	return query(ctx, s, "repohealth", func(ctx context.Context) (model.RepoHealth, error) {
		var h model.RepoHealth
		cd, err := s.repo.GitCommonDir(ctx)
		if err != nil {
			return h, err
		}
		h.GitCommonDir = strings.TrimSpace(cd)
		h.PackBytes = packBytes(filepath.Join(h.GitCommonDir, "objects", "pack"))
		h.HasCommitGraph = pathExists(filepath.Join(h.GitCommonDir, "objects", "info", "commit-graph")) ||
			pathExists(filepath.Join(h.GitCommonDir, "objects", "info", "commit-graphs"))
		if v, set, _ := s.repo.ConfigGet(ctx, git.ConfigLocal, "fetch.writeCommitGraph"); set {
			h.WriteCommitGraphSet, h.WriteCommitGraphValue = true, v
		} else if v, set, _ := s.repo.ConfigGet(ctx, git.ConfigGlobal, "fetch.writeCommitGraph"); set {
			h.WriteCommitGraphSet, h.WriteCommitGraphValue = true, v
		}
		return h, nil
	})
}

// packBytes sums the *.pack files in dir (flat — git keeps packs in one
// directory). A missing dir reads as 0, not an error.
func packBytes(dir string) int64 {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pack") {
			continue
		}
		if info, err := e.Info(); err == nil {
			total += info.Size()
		}
	}
	return total
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/domain/ -run TestRepoHealth`
Expected: PASS (3 tests). Then `go test ./internal/domain/ ./internal/model/` — green.

- [ ] **Step 5: gofmt and commit**

```bash
gofmt -w internal/model/health.go internal/domain/repohealth.go internal/domain/repohealth_test.go
git add internal/model/health.go internal/domain/repohealth.go internal/domain/repohealth_test.go
git commit -m "feat(domain): RepoHealth query — pack size, commit-graph presence, fetch.writeCommitGraph

Stat-level facts under a Read reservation; no git walks. GitCommonDir rides
along as the per-repo notice-dismissal key.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n"
```

---

### Task 4: TUI notice core — health check, notice list, blink, status segment

**Files:**
- Create: `internal/tui/notify.go`
- Modify: `internal/tui/model.go` (Model struct fields; `New()` at ~line 207; `Init()` at ~line 230; new `repoHealthMsg`/`noticeBlinkMsg` cases in `Update`; `reRoot` at ~line 2484)
- Modify: `internal/tui/view.go` (`renderInterface` status-segment assembly, ~lines 343–390)
- Test: `internal/tui/notify_test.go`

**Interfaces:**
- Consumes: `svc.RepoHealth` (Task 3), `engine.SetGitConfig`/`engine.WriteCommitGraph` (Task 2), `m.promptStore promptstate.Store` + `defaultPromptStatePath()` (stage 1, already on Model), `m.startOp` (`internal/tui/op.go:184`).
- Produces (used by Tasks 5–6):
  - Model fields: `notices []notice`, `noticesUnread bool`, `blinkOn bool`, `noticeGen int`, `noticeSessionDismissed map[string]bool`, `repoHealth model.RepoHealth`, `repoHealthKnown bool`, `pendingNoticeConfig *engine.SetGitConfig`.
  - `type notice struct { id, repoKey, title string; detail []string; actions []noticeAction }`
  - `type noticeAction struct { label string; run func(Model) (Model, tea.Cmd); never bool }`
  - `func (m Model) repoHealthCmd(gen int) tea.Cmd` → `repoHealthMsg{gen int; health model.RepoHealth; err error}`
  - `func (m Model) applyRepoHealth(msg repoHealthMsg) (Model, tea.Cmd)` (the Update-case body)
  - `func (m Model) noticeSegment() string` (status-bar segment; "" when none)
  - `func (m Model) removeNotice(id string) Model`, `func (m Model) startCommitGraphWriteAndEnable() (Model, tea.Cmd)` (THE shared action-1 code path)
  - `const noticeCommitGraph = "commit_graph_recommend"`, `const bigRepoPackBytes = 100 << 20`
  - `noticeBlinkMsg{}` / `noticeBlinkCmd()` (~800ms `tea.Tick`)
  - Test helpers `noticeTestModel(t) (Model, promptstate.Store)` and `bigRepoHealth() model.RepoHealth` in the test file — Tasks 5–6 reuse both (same package).

- [ ] **Step 1: Write the failing test**

Create `internal/tui/notify_test.go`:

```go
package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/promptstate"
)

// noticeTestModel: loaded model + temp prompt store (never the developer's
// real prompts.toml — same rule as promptTestModel in related_prompts_test.go).
func noticeTestModel(t *testing.T) (Model, promptstate.Store) {
	t.Helper()
	m, _ := settingsModel(t)
	st := promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	m.promptStore = st
	return m, st
}

// bigRepoHealth fabricates a health snapshot that should trigger the
// commit-graph notice: big pack, no graph, config unset.
func bigRepoHealth() model.RepoHealth {
	return model.RepoHealth{
		GitCommonDir: "/fake/common/dir",
		PackBytes:    bigRepoPackBytes + 1,
	}
}

func TestRepoHealthMsgBuildsCommitGraphNotice(t *testing.T) {
	m, _ := noticeTestModel(t)
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: bigRepoHealth()})
	m = nm.(Model)
	if len(m.notices) != 1 || m.notices[0].id != noticeCommitGraph {
		t.Fatalf("notices = %+v, want the commit-graph notice", m.notices)
	}
	if !m.noticesUnread {
		t.Fatal("a fresh notice must start unread (blinking)")
	}
	if seg := m.noticeSegment(); !strings.Contains(seg, "1 notice") {
		t.Fatalf("status segment = %q, want '! 1 notice …'", seg)
	}
}

func TestNoNoticeWhenRepoSmallOrGraphPresentOrConfigSet(t *testing.T) {
	m, _ := noticeTestModel(t)
	small := bigRepoHealth()
	small.PackBytes = bigRepoPackBytes - 1
	graphed := bigRepoHealth()
	graphed.HasCommitGraph = true
	configured := bigRepoHealth()
	configured.WriteCommitGraphSet = true
	for name, h := range map[string]model.RepoHealth{"small": small, "graphed": graphed, "configured": configured} {
		nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: h})
		if got := nm.(Model).notices; len(got) != 0 {
			t.Fatalf("%s: notices = %+v, want none", name, got)
		}
	}
}

func TestStaleHealthResultDropped(t *testing.T) {
	m, _ := noticeTestModel(t)
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen - 1, health: bigRepoHealth()})
	if got := nm.(Model).notices; len(got) != 0 {
		t.Fatalf("stale gen must be dropped, got %+v", got)
	}
}

func TestPersistedDismissalFiltersNotice(t *testing.T) {
	m, st := noticeTestModel(t)
	h := bigRepoHealth()
	if err := st.DismissNotice(h.GitCommonDir, noticeCommitGraph); err != nil {
		t.Fatal(err)
	}
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: h})
	if got := nm.(Model).notices; len(got) != 0 {
		t.Fatalf("persisted dismissal must filter the notice, got %+v", got)
	}
}

func TestSessionDismissalSurvivesHealthRereadWithinSession(t *testing.T) {
	m, _ := noticeTestModel(t)
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: bigRepoHealth()})
	m = nm.(Model)
	m = m.removeNotice(noticeCommitGraph)
	m.noticeSessionDismissed[noticeCommitGraph] = true // what "Not now" records
	nm, _ = m.Update(repoHealthMsg{gen: m.noticeGen, health: bigRepoHealth()})
	if got := nm.(Model).notices; len(got) != 0 {
		t.Fatalf("a session-dismissed notice must not resurrect on a mid-session re-read, got %+v", got)
	}
}

func TestBlinkTickStopsWhenRead(t *testing.T) {
	m, _ := noticeTestModel(t)
	nm, cmd := m.Update(repoHealthMsg{gen: m.noticeGen, health: bigRepoHealth()})
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("a fresh unread notice must arm the blink tick")
	}
	before := m.blinkOn
	nm, cmd = m.Update(noticeBlinkMsg{})
	m = nm.(Model)
	if m.blinkOn == before {
		t.Fatal("blink tick must flip the phase while unread")
	}
	if cmd == nil {
		t.Fatal("blink must re-arm while unread")
	}
	m.noticesUnread = false // what opening the dialog does
	_, cmd = m.Update(noticeBlinkMsg{})
	if cmd != nil {
		t.Fatal("blink must stop re-arming once read")
	}
}

func TestUnreadOnlyOnNewNoticeIds(t *testing.T) {
	m, _ := noticeTestModel(t)
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: bigRepoHealth()})
	m = nm.(Model)
	m.noticesUnread = false // user opened the dialog (read)
	nm, _ = m.Update(repoHealthMsg{gen: m.noticeGen, health: bigRepoHealth()})
	if nm.(Model).noticesUnread {
		t.Fatal("a re-read carrying the SAME notice ids must not re-blink")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestRepoHealthMsg|TestNoNotice|TestStaleHealth|TestPersistedDismissal|TestSessionDismissal|TestBlink|TestUnreadOnly'`
Expected: FAIL to build — `repoHealthMsg`, `notice`, etc. undefined.

- [ ] **Step 3: Implement `internal/tui/notify.go`**

```go
package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// The notification center: cheap health checks run on every repo load
// (domain.RepoHealth); unresolved findings become notices — a blinking red
// status segment plus the ! dialog whose actions fix the finding through
// real engine ops. Notice lifecycle: unread → read (opening the dialog) →
// dismissed for the session ("Not now"; re-evaluated next load) or never for
// this repo (persisted via promptstate.DismissNotice, keyed by common dir).

// notice is one surfaced health recommendation.
type notice struct {
	id      string   // stable dismissal key (persisted on "never")
	repoKey string   // git common dir — the promptstate dismissal scope
	title   string   // one-line list entry
	detail  []string // body lines shown above the actions
	actions []noticeAction
}

// noticeAction is one dialog choice. run nil = close-only ("Not now");
// never additionally persists the per-repo dismissal.
type noticeAction struct {
	label string
	run   func(Model) (Model, tea.Cmd)
	never bool
}

// noticeCommitGraph is the commit-graph recommendation's stable id.
const noticeCommitGraph = "commit_graph_recommend"

// bigRepoPackBytes is the pack-size floor for "big repo": below it the
// commit-graph win doesn't matter enough to nag about.
const bigRepoPackBytes = 100 << 20

// Blink = style alternation between these two on a dedicated tick;
// terminal-native blink escapes are unreliable.
var (
	noticeHotStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	noticeDimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("124"))
)

// repoHealthMsg carries one background health read; gen guards repo switches.
type repoHealthMsg struct {
	gen    int
	health model.RepoHealth
	err    error
}

// repoHealthCmd reads repo health off the UI thread (startup, reRoot, and
// whenever Settings opens so its Commit-graph row shows fresh state).
func (m Model) repoHealthCmd(gen int) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		h, err := svc.RepoHealth(context.Background())
		return repoHealthMsg{gen: gen, health: h, err: err}
	}
}

// noticeBlinkMsg flips the blink phase while unread notices exist.
type noticeBlinkMsg struct{}

// noticeBlinkCmd schedules the next blink flip (~800ms; only re-armed while
// unread notices exist, so the tick self-stops).
func noticeBlinkCmd() tea.Cmd {
	return tea.Tick(800*time.Millisecond, func(time.Time) tea.Msg { return noticeBlinkMsg{} })
}

// applyRepoHealth is the repoHealthMsg Update case: store the snapshot,
// rebuild the notice list (filtered by persisted + session dismissals), and
// start blinking only when a genuinely NEW notice id appeared — a mid-session
// re-read carrying the same ids must not re-blink.
func (m Model) applyRepoHealth(msg repoHealthMsg) (Model, tea.Cmd) {
	if msg.gen != m.noticeGen {
		return m, nil // stale: a repo switch superseded this read
	}
	if msg.err != nil {
		return m, nil // best-effort: health never surfaces errors in the UI
	}
	m.repoHealth = msg.health
	m.repoHealthKnown = true

	var dismissed map[string]bool
	if m.promptStore != nil {
		dismissed = m.promptStore.DismissedNotices(msg.health.GitCommonDir)
	}
	prev := make(map[string]bool, len(m.notices))
	for _, n := range m.notices {
		prev[n.id] = true
	}
	var next []notice
	if n := commitGraphNotice(msg.health); n != nil && !dismissed[n.id] && !m.noticeSessionDismissed[n.id] {
		next = append(next, *n)
	}
	m.notices = next
	var cmd tea.Cmd
	for _, n := range m.notices {
		if !prev[n.id] {
			if !m.noticesUnread {
				cmd = noticeBlinkCmd()
			}
			m.noticesUnread = true
			m.blinkOn = true
			break
		}
	}
	return m, cmd
}

// commitGraphNotice fires when the repo is big (pack ≥ bigRepoPackBytes), has
// no commit-graph file/chain, and fetch.writeCommitGraph is unset — the case
// where one keystroke makes commit browsing ~10× faster.
func commitGraphNotice(h model.RepoHealth) *notice {
	if h.PackBytes < bigRepoPackBytes || h.HasCommitGraph || h.WriteCommitGraphSet {
		return nil
	}
	return &notice{
		id:      noticeCommitGraph,
		repoKey: h.GitCommonDir,
		title:   "Commit browsing can be ~10× faster in this repo",
		detail: []string{
			fmt.Sprintf("This repo is big (%.0f MB of packs) and has no commit-graph file,", float64(h.PackBytes)/(1<<20)),
			"so ordered commit walks (the Commits panel's paging) re-walk history",
			"every time. Writing one takes a moment and git keeps it fresh when",
			"fetch.writeCommitGraph is on.",
		},
		actions: []noticeAction{
			{label: "Write commit-graph now + keep it fresh (fetch.writeCommitGraph=true)",
				run: Model.startCommitGraphWriteAndEnable},
			{label: "Enable auto-refresh only (graph appears on next fetch/gc)",
				run: func(m Model) (Model, tea.Cmd) {
					return m.startOp(engine.SetGitConfig{Key: "fetch.writeCommitGraph", Value: "true"})
				}},
			{label: "Not now (ask again next load)"},
			{label: "Never for this repo", never: true},
		},
	}
}

// startCommitGraphWriteAndEnable is THE write+enable code path, shared by the
// notice's first action and the Settings "Commit-graph" row: write the graph
// now, then (chained on success in opFinishedMsg) enable auto-refresh.
func (m Model) startCommitGraphWriteAndEnable() (Model, tea.Cmd) {
	m.pendingNoticeConfig = &engine.SetGitConfig{Key: "fetch.writeCommitGraph", Value: "true"}
	return m.startOp(engine.WriteCommitGraph{})
}

// removeNotice drops one notice from the session list.
func (m Model) removeNotice(id string) Model {
	var next []notice
	for _, n := range m.notices {
		if n.id != id {
			next = append(next, n)
		}
	}
	m.notices = next
	return m
}

// noticeSegment renders the status-bar segment: red + phase-alternating while
// unread, calm plain text once read, "" when there is nothing to say (or the
// conflict process owns the screen).
func (m Model) noticeSegment() string {
	n := len(m.notices)
	if n == 0 || m.proc != nil {
		return ""
	}
	seg := fmt.Sprintf("! %d notice", n)
	if n != 1 {
		seg += "s"
	}
	seg += " — press [!]"
	if !m.noticesUnread {
		return seg
	}
	if m.blinkOn {
		return noticeHotStyle.Render(seg)
	}
	return noticeDimStyle.Render(seg)
}
```

- [ ] **Step 4: Wire Model, Init, Update, reRoot, and the status line**

In `internal/tui/model.go`:

1. Model struct — add fields near `pendingPushTags` (~line 51):

```go
	notices               []notice              // session notice list (see notify.go)
	noticesUnread         bool                  // blink while true; opening the ! dialog clears it
	blinkOn               bool                  // current blink phase (style alternation)
	noticeGen             int                   // stale-drop guard for repoHealthMsg across repo switches
	noticeSessionDismissed map[string]bool      // "Not now" ids; cleared on reRoot (re-evaluated next load)
	repoHealth            model.RepoHealth      // last health snapshot (Settings Commit-graph row)
	repoHealthKnown       bool                  // false until the first repoHealthMsg lands
	pendingNoticeConfig   *engine.SetGitConfig  // chained after WriteCommitGraph succeeds
```

2. `New()` (~line 207) — add to the literal:

```go
		noticeSessionDismissed: map[string]bool{},
```

3. `Init()` (~line 230) — add the health check to the batch:

```go
	return tea.Batch(m.bootstrapCmd(), loadSearchHistCmd(m.svc), heartbeatCmd(), m.repoHealthCmd(m.noticeGen))
```

4. `Update` — add two cases next to the other msg cases:

```go
	case repoHealthMsg:
		return m.applyRepoHealth(msg)
	case noticeBlinkMsg:
		if !m.noticesUnread {
			return m, nil // read: stop re-arming
		}
		m.blinkOn = !m.blinkOn
		return m, noticeBlinkCmd()
```

5. `opFinishedMsg` handler — mirror the `pendingPushTags` chain EXACTLY. In the success branch (inside `else`, next to `pushTags = m.pendingPushTags`):

```go
			var noticeCfg *engine.SetGitConfig
			if msg.res.Changed {
				noticeCfg = m.pendingNoticeConfig
			}
```

Declare `var noticeCfg *engine.SetGitConfig` BEFORE the `if msg.err != nil` block (beside `var pushTags []string`) and only assign inside the success branch. After the branch, beside `m.pendingPushTags = nil`:

```go
		m.pendingNoticeConfig = nil // unconditional; covers both error and success paths
```

In the chain-dispatch section, after the `if len(pushTags) > 0 { … }` block:

```go
		if noticeCfg != nil {
			// Chain: the commit-graph write succeeded — now enable auto-refresh.
			return m.startOp(*noticeCfg)
		}
```

6. `reRoot` (~line 2484) — beside the `pushCheckGen++` resets:

```go
	m.notices = nil
	m.noticesUnread = false
	m.noticeGen++ // drop any in-flight health read from the old repo
	m.noticeSessionDismissed = map[string]bool{}
	m.repoHealthKnown = false
	m.pendingNoticeConfig = nil
```

and extend reRoot's final return to re-run the health check:

```go
	return m, tea.Batch(m.loadCmd(), m.startWatchCmd(m.watchGen), m.repoHealthCmd(m.noticeGen))
```

In `internal/tui/view.go`, `renderInterface` — add the segment to the parts assembly (both branches, so a red notice stays visible in error mode but never leads over the error):

```go
	if errMode {
		add(m.statusMsg)
		add(notice)
		add(m.noticeSegment())
		add(markHint)
	} else {
		add(m.noticeSegment())
		add(markHint)
		add(notice)
		add(m.statusMsg)
		add(m.commitBranchHint())
		add(m.bgRefreshHint())
	}
```

(That replaces the existing two branches; `notice` is the pre-existing conflicts variable — keep it.)

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestRepoHealthMsg|TestNoNotice|TestStaleHealth|TestPersistedDismissal|TestSessionDismissal|TestBlink|TestUnreadOnly'`
Expected: PASS (7 tests). Then `go test ./internal/tui/` — the full package must stay green (the Init/reRoot edits touch shared paths).

- [ ] **Step 6: gofmt and commit**

```bash
gofmt -w internal/tui/notify.go internal/tui/notify_test.go internal/tui/model.go internal/tui/view.go
git add internal/tui/notify.go internal/tui/notify_test.go internal/tui/model.go internal/tui/view.go
git commit -m "feat(tui): notice core — repo-health check, commit-graph notice, blinking status segment

domain.RepoHealth runs in the background on startup and repo switch; a big
repo (packs ≥100MB) with no commit-graph and fetch.writeCommitGraph unset
produces the first notice. Unread notices blink the '! N notice' segment via
a self-stopping 800ms style-alternation tick; dismissals: session ('Not
now', re-evaluated next load) or per-repo persisted (promptstate).

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n"
```

---

### Task 5: the `!` notification dialog + op chain end-to-end

**Files:**
- Create: `internal/tui/notice_popup.go`
- Modify: `internal/tui/model.go` (add the `case "!":` global key beside `case "G":` at ~line 1066)
- Modify: `internal/tui/footer.go` (one `globalBindings` entry)
- Modify: `internal/tui/help.go` (one Global row)
- Test: `internal/tui/notice_popup_test.go`

**Interfaces:**
- Consumes: everything Task 4 produced; layer stack (`pushLayer`/`popLayer`/`layerOf`); list-popup plumbing `renderWindow`/`winRow`/`winOpts`/`popupBox`/`popupWideInnerWidth`/`popupTextWidth`/`selectedRow`/`overlayCenter`/`clipToHeight`/`m.overlayDims()` (exemplar: the Session-errors screen in `settings_popup.go` and `repo_popup.go`); `driveOp(t, m, cmd)` + `newRepoDir(t)` test helpers (`internal/tui/op_test.go`); `keyMsg("!")`.
- Produces: `type noticePopup struct { sel int; showActions bool; actSel int; mode dispMode; hscroll int }` implementing `layer`; `func (m Model) openNoticeCenter() (Model, tea.Cmd)` — marks all notices read, pushes the popup.

**Dialog contract (spec):** list of notices; `enter` shows the selected notice's actions (option list); `esc` closes (actions screen → back to list; list → close). Opening marks all as read (stops blinking). Acting or dismissing removes the notice. With zero notices the dialog still opens and says so (mirrors the Session-errors "no errors this session" posture — `!` is advertised in help, it must never dead-end).

- [ ] **Step 1: Write the failing test**

Create `internal/tui/notice_popup_test.go`:

```go
package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// noticeModelWithNotice returns a loaded model (real repo) carrying the
// commit-graph notice, as if a health read just landed.
func noticeModelWithNotice(t *testing.T) (Model, string) {
	t.Helper()
	m, st := noticeTestModel(t)
	_ = st
	dir := m.currentWorktree
	h := bigRepoHealth()
	h.GitCommonDir = filepath.Join(dir, ".git")
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: h})
	return nm.(Model), dir
}

func TestBangOpensDialogAndMarksRead(t *testing.T) {
	m, _ := noticeModelWithNotice(t)
	if !m.noticesUnread {
		t.Fatal("precondition: unread")
	}
	nm, _ := m.Update(keyMsg("!"))
	m = nm.(Model)
	if layerOf[*noticePopup](m) == nil {
		t.Fatal("! must open the notification dialog")
	}
	if m.noticesUnread {
		t.Fatal("opening the dialog must mark notices read")
	}
	if out := m.View(); !strings.Contains(out, "faster in this repo") {
		t.Fatalf("dialog must list the notice title, view:\n%s", out)
	}
}

func TestBangInertWhileFilterTyping(t *testing.T) {
	m, _ := noticeModelWithNotice(t)
	m.filterTyping = true
	m.filterPanel = m.focus
	nm, _ := m.Update(keyMsg("!"))
	if layerOf[*noticePopup](nm.(Model)) != nil {
		t.Fatal("! must be inert while a filter is capturing input")
	}
}

func TestDialogWithZeroNoticesSaysSo(t *testing.T) {
	m, _ := noticeTestModel(t)
	nm, _ := m.Update(keyMsg("!"))
	m = nm.(Model)
	if layerOf[*noticePopup](m) == nil {
		t.Fatal("! must open even with no notices (esc closes)")
	}
	if out := m.View(); !strings.Contains(out, "no notices") {
		t.Fatalf("empty dialog must say 'no notices', view:\n%s", out)
	}
}

func TestEscClosesActionsThenList(t *testing.T) {
	m, _ := noticeModelWithNotice(t)
	nm, _ := m.Update(keyMsg("!"))
	m = nm.(Model)
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // list → actions
	m = nm.(Model)
	p := layerOf[*noticePopup](m)
	if p == nil || !p.showActions {
		t.Fatal("enter on a notice must show its actions")
	}
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // actions → list
	m = nm.(Model)
	if p := layerOf[*noticePopup](m); p == nil || p.showActions {
		t.Fatal("esc must return from actions to the list, not close")
	}
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // list → closed
	if layerOf[*noticePopup](nm.(Model)) != nil {
		t.Fatal("esc on the list must close the dialog")
	}
	if len(nm.(Model).notices) != 1 {
		t.Fatal("closing without acting must keep the notice (read, not dismissed)")
	}
}

func TestNotNowDismissesForSession(t *testing.T) {
	m, _ := noticeModelWithNotice(t)
	nm, _ := m.Update(keyMsg("!"))
	m = nm.(Model)
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // actions
	m = nm.(Model)
	p := layerOf[*noticePopup](m)
	p.actSel = 2 // Not now
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if layerOf[*noticePopup](m) != nil {
		t.Fatal("acting must close the dialog")
	}
	if len(m.notices) != 0 {
		t.Fatal("Not now must remove the notice from the session list")
	}
	if !m.noticeSessionDismissed[noticeCommitGraph] {
		t.Fatal("Not now must record the session dismissal")
	}
}

func TestNeverPersistsDismissal(t *testing.T) {
	m, _ := noticeModelWithNotice(t)
	repoKey := m.notices[0].repoKey
	nm, _ := m.Update(keyMsg("!"))
	m = nm.(Model)
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	p := layerOf[*noticePopup](m)
	p.actSel = 3 // Never for this repo
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if len(m.notices) != 0 {
		t.Fatal("Never must remove the notice")
	}
	st := m.promptStore
	if st == nil || !st.DismissedNotices(repoKey)[noticeCommitGraph] {
		t.Fatal("Never must persist the per-repo dismissal in the prompt store")
	}
}

func TestWriteAndEnableRunsBothOpsForReal(t *testing.T) {
	m, dir := noticeModelWithNotice(t)
	nm, _ := m.Update(keyMsg("!"))
	m = nm.(Model)
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // actions; actSel=0 = write+enable
	m = nm.(Model)
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if !m.running {
		t.Fatal("write+enable must start the WriteCommitGraph op")
	}
	m = driveOp(t, m, cmd) // drives the op AND its chained SetGitConfig

	cg := filepath.Join(dir, ".git", "objects", "info", "commit-graph")
	if _, err := os.Stat(cg); err != nil {
		t.Fatalf("commit-graph file not written at %s: %v", cg, err)
	}
	out, err := exec.Command("git", "-C", dir, "config", "--local", "fetch.writeCommitGraph").Output()
	if err != nil || strings.TrimSpace(string(out)) != "true" {
		t.Fatalf("chained config set missing: %q, %v", out, err)
	}
	if len(m.notices) != 0 {
		t.Fatal("acting must remove the notice")
	}
}

func TestDialogSwallowsGlobalKeys(t *testing.T) {
	m, _ := noticeModelWithNotice(t)
	nm, _ := m.Update(keyMsg("!"))
	m = nm.(Model)
	before := len(m.layers.entries)
	for _, k := range []string{"p", "g", "G", ",", "!", "r"} {
		nm, _ := m.Update(keyMsg(k))
		m = nm.(Model)
	}
	if len(m.layers.entries) != before || layerOf[*noticePopup](m) == nil {
		t.Fatal("dialog must swallow global keys")
	}
}
```

Notes for the implementer: `m.currentWorktree` is the loaded repo dir on a `settingsModel(t)` model — verify the field name (grep `currentWorktree` in `model.go`); if `settingsModel`'s repo has no commits, `git commit-graph write` still succeeds on an empty history only if at least one commit exists — `newRepoDir`/`settingsModel` create an initial commit (verify; if not, add one via the test helpers the file already uses). `driveOp` loops while `m.running`, so the chained op is covered by the same call.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestBang|TestDialog|TestEscCloses|TestNotNow|TestNever|TestWriteAndEnable'`
Expected: FAIL to build — `noticePopup` undefined.

- [ ] **Step 3: Implement the popup + key + footer/help**

Create `internal/tui/notice_popup.go`:

```go
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// noticePopup is the ! notification dialog: a list of notices; enter opens
// the selected notice's actions; esc backs out (actions → list → closed).
// Acting or dismissing removes the notice from the session list.
type noticePopup struct {
	sel         int
	showActions bool
	actSel      int
	mode        dispMode
	hscroll     int
}

// openNoticeCenter opens the dialog and marks every notice read (the blink
// tick stops re-arming on its next fire).
func (m Model) openNoticeCenter() (Model, tea.Cmd) {
	m.noticesUnread = false
	return m.pushLayer(&noticePopup{}), nil
}

func (p *noticePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		if p.showActions {
			p.showActions = false
			return m, nil
		}
		return m.popLayer(), nil
	}
	if !p.showActions {
		switch msg.String() {
		case "z":
			p.mode = p.mode.next()
			p.hscroll = 0
			return m, nil
		case "shift+left":
			if p.mode == modeScroll && p.hscroll > 0 {
				if p.hscroll -= m.hscrollStep(); p.hscroll < 0 {
					p.hscroll = 0
				}
			}
			return m, nil
		case "shift+right":
			if p.mode == modeScroll {
				p.hscroll += m.hscrollStep()
			}
			return m, nil
		}
	}
	switch msg.Type {
	case tea.KeyUp:
		if p.showActions {
			if p.actSel > 0 {
				p.actSel--
			}
		} else if p.sel > 0 {
			p.sel--
		}
	case tea.KeyDown:
		if p.showActions {
			if n := p.currentNotice(m); n != nil && p.actSel < len(n.actions)-1 {
				p.actSel++
			}
		} else if p.sel < len(m.notices)-1 {
			p.sel++
		}
	case tea.KeyEnter:
		if !p.showActions {
			if len(m.notices) == 0 {
				return m, nil
			}
			p.showActions = true
			p.actSel = 0
			return m, nil
		}
		n := p.currentNotice(m)
		if n == nil {
			p.showActions = false
			return m, nil
		}
		act := n.actions[p.actSel]
		m = m.popLayer() // any action closes the dialog
		return m.applyNoticeAction(*n, act)
	}
	return m, nil // swallow everything else
}

// currentNotice resolves the selected notice against the LIVE list (the
// popup holds indices, not copies).
func (p *noticePopup) currentNotice(m Model) *notice {
	if p.sel < 0 || p.sel >= len(m.notices) {
		return nil
	}
	return &m.notices[p.sel]
}

// applyNoticeAction removes the notice (acting or dismissing removes it),
// records the dismissal kind, and runs the action's op if it has one.
func (m Model) applyNoticeAction(n notice, act noticeAction) (Model, tea.Cmd) {
	m = m.removeNotice(n.id)
	m.noticeSessionDismissed[n.id] = true // a mid-session health re-read must not resurrect it
	if act.never {
		if m.promptStore == nil {
			m.statusMsg = "dismissed for this session (no state dir — can't persist)"
		} else if err := m.promptStore.DismissNotice(n.repoKey, n.id); err != nil {
			m.statusMsg = "dismissed for this session (couldn't persist: " + err.Error() + ")"
		} else {
			m.statusMsg = "notice dismissed for this repo — " + defaultPromptStatePath()
		}
	}
	if act.run != nil {
		return act.run(m)
	}
	return m, nil
}

func (p *noticePopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	inner := popupWideInnerWidth(w)
	textW := popupTextWidth(inner)
	var b strings.Builder
	if p.showActions {
		if n := p.currentNotice(m); n != nil {
			b.WriteString(n.title + "\n\n")
			for _, line := range n.detail {
				for _, seg := range wrapWidth(line, textW, 1<<20) {
					b.WriteString(seg + "\n")
				}
			}
			b.WriteString("\n")
			for i, act := range n.actions {
				prefix := "  "
				row := prefix + act.label
				if i == p.actSel {
					row = selectedRow.Render("> " + act.label)
				}
				b.WriteString(row + "\n")
			}
			b.WriteString("\n[↑/↓] select  [enter] choose  [esc] back")
		}
	} else {
		b.WriteString("Notifications\n\n")
		if len(m.notices) == 0 {
			b.WriteString("  no notices for this repo\n")
		} else {
			wr := make([]winRow, len(m.notices))
			for i, n := range m.notices {
				prefix := "  "
				var st lipgloss.Style
				if i == p.sel {
					prefix, st = "> ", selectedRow
				}
				wr[i] = winRow{text: fmt.Sprintf("%s%s", prefix, n.title), style: st}
			}
			rows := len(m.notices)
			if rows > 12 {
				rows = 12
			}
			for _, line := range renderWindow(wr, winOpts{w: textW, h: rows, mode: p.mode, anchor: p.sel, hscroll: p.hscroll}) {
				b.WriteString(line + "\n")
			}
		}
		b.WriteString("\n[↑/↓] select  [enter] actions  [z] mode  [esc] close")
	}
	return overlayCenter(clipToHeight(below, h), popupBox(inner, strings.TrimRight(b.String(), "\n")), w, h)
}
```

In `internal/tui/model.go`, add the global key beside `case "G":` (~line 1066):

```go
		case "!": // open the notification center (global; inert while a text field captures — this switch is only reached in navigation mode)
			return m.openNoticeCenter()
```

In `internal/tui/footer.go`, add to `globalBindings` (after the `"shelf"` entry):

```go
	{"notices", "!", "[!] notices", func(m Model) bool { return len(m.notices) > 0 }, scopeGlobal},
```

In `internal/tui/help.go`, add a Global row after the `G` row:

```go
		r("!", "notification center: health recommendations for this repo (e.g. write a commit-graph on a big repo — makes commit browsing ~10× faster). ↑↓ select, enter shows a notice's actions, esc closes. 'Not now' asks again next load; 'Never for this repo' is remembered in prompts.toml"),
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestBang|TestDialog|TestEscCloses|TestNotNow|TestNever|TestWriteAndEnable|TestHelpFooterCoverage'`
Expected: PASS — including `TestHelpFooterCoverage` (the footer binding + help row pair).
Then the full package: `go test ./internal/tui/` — green.

- [ ] **Step 5: gofmt and commit**

```bash
gofmt -w internal/tui/notice_popup.go internal/tui/notice_popup_test.go internal/tui/model.go internal/tui/footer.go internal/tui/help.go
git add internal/tui/notice_popup.go internal/tui/notice_popup_test.go internal/tui/model.go internal/tui/footer.go internal/tui/help.go
git commit -m "feat(tui): ! notification dialog — list, per-notice actions, real op wiring

! opens the notification center (global key, inert while typing; advertised
in footer + help). Enter shows a notice's actions: the commit-graph notice
offers write+enable (WriteCommitGraph chained into SetGitConfig), enable
only, Not now (session), Never for this repo (persisted via promptstate).
Opening marks notices read and stops the blink.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n"
```

---

### Task 6: Settings "Commit-graph" row

**Files:**
- Modify: `internal/tui/settings_popup.go` (menu constant + `settingsMenu` slice + `settingsMenuLabel` + the enter case + `openSettings`)
- Test: `internal/tui/settings_commitgraph_test.go`

**Interfaces:**
- Consumes: `m.repoHealth`/`m.repoHealthKnown`/`m.repoHealthCmd`/`m.noticeGen`/`m.startCommitGraphWriteAndEnable()` (Task 4).
- Produces: `settingsMenuCommitGraph = "Commit-graph"` menu row. Label states: `Commit-graph: (checking…)` (health unknown), `missing — enter writes + keeps fresh`, `present, auto-refresh on`, `present, auto-refresh off — enter writes + keeps fresh`. Enter applies the SAME write+enable path as notice action 1; a no-op (already present + on) just sets a statusMsg.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/settings_commitgraph_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

func commitGraphMenuIndex(t *testing.T) int {
	t.Helper()
	for i, entry := range settingsMenu {
		if entry == settingsMenuCommitGraph {
			return i
		}
	}
	t.Fatal("Commit-graph entry missing from the settings menu")
	return -1
}

func TestCommitGraphLabelStates(t *testing.T) {
	m, _ := noticeTestModel(t)
	idx := commitGraphMenuIndex(t)

	if got := settingsMenuLabel(m, idx); !strings.Contains(got, "checking") {
		t.Fatalf("unknown health: label = %q, want '(checking…)'", got)
	}
	m.repoHealthKnown = true
	m.repoHealth = model.RepoHealth{}
	if got := settingsMenuLabel(m, idx); !strings.Contains(got, "missing") {
		t.Fatalf("missing graph: label = %q", got)
	}
	m.repoHealth = model.RepoHealth{HasCommitGraph: true, WriteCommitGraphSet: true, WriteCommitGraphValue: "true"}
	if got := settingsMenuLabel(m, idx); !strings.Contains(got, "auto-refresh on") {
		t.Fatalf("present+on: label = %q", got)
	}
	m.repoHealth = model.RepoHealth{HasCommitGraph: true}
	if got := settingsMenuLabel(m, idx); !strings.Contains(got, "auto-refresh off") {
		t.Fatalf("present+off: label = %q", got)
	}
}

func TestCommitGraphRowEnterStartsWriteAndEnable(t *testing.T) {
	m, _ := noticeTestModel(t)
	m.repoHealthKnown = true
	m.repoHealth = model.RepoHealth{} // missing → write+enable
	nm, _ := m.Update(keyMsg(","))
	m = nm.(Model)
	p := layerOf[*settingsPopup](m)
	p.menuSel = commitGraphMenuIndex(t)
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if !m.running {
		t.Fatal("enter on the missing-graph row must start the write+enable op")
	}
	if m.pendingNoticeConfig == nil {
		t.Fatal("the SetGitConfig chain must be armed (same code path as the notice action)")
	}
	m = driveOp(t, m, cmd)
}

func TestCommitGraphRowNoOpWhenHealthy(t *testing.T) {
	m, _ := noticeTestModel(t)
	m.repoHealthKnown = true
	m.repoHealth = model.RepoHealth{HasCommitGraph: true, WriteCommitGraphSet: true, WriteCommitGraphValue: "true"}
	nm, _ := m.Update(keyMsg(","))
	m = nm.(Model)
	p := layerOf[*settingsPopup](m)
	p.menuSel = commitGraphMenuIndex(t)
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if m.running {
		t.Fatal("already present + auto-refresh on: enter must be a no-op")
	}
	if m.statusMsg == "" {
		t.Fatal("the no-op must say why in the status line")
	}
}

func TestOpenSettingsRefreshesHealth(t *testing.T) {
	m, _ := noticeTestModel(t)
	nm, cmd := m.Update(keyMsg(","))
	_ = nm
	if cmd == nil {
		t.Fatal("opening Settings must re-read repo health so the Commit-graph label is fresh")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestCommitGraph|TestOpenSettings'`
Expected: FAIL to build — `settingsMenuCommitGraph` undefined.

- [ ] **Step 3: Implement**

In `internal/tui/settings_popup.go`:

1. Constants block — add:

```go
	settingsMenuCommitGraph = "Commit-graph"
```

2. `settingsMenu` slice — insert after `settingsMenuShowGraph`:

```go
var settingsMenu = []string{settingsMenuAgents, settingsMenuIdentity, settingsMenuPrefixes, settingsMenuHook, settingsMenuOpLog, settingsMenuErrors, settingsMenuAutoRefresh, settingsMenuRemoteTags, settingsMenuRates, settingsMenuCommitSort, settingsMenuShowGraph, settingsMenuCommitGraph}
```

3. `settingsMenuLabel` — add a case:

```go
	if settingsMenu[i] == settingsMenuCommitGraph {
		if !m.repoHealthKnown {
			return settingsMenuCommitGraph + ": (checking…)"
		}
		switch {
		case !m.repoHealth.HasCommitGraph:
			return settingsMenuCommitGraph + ": missing — enter writes + keeps fresh"
		case m.repoHealth.WriteCommitGraphValue == "true":
			return settingsMenuCommitGraph + ": present, auto-refresh on"
		default:
			return settingsMenuCommitGraph + ": present, auto-refresh off — enter writes + keeps fresh"
		}
	}
```

4. Enter handler — add a case beside `settingsMenuShowGraph`:

```go
			case settingsMenuCommitGraph:
				if !m.repoHealthKnown {
					m.statusMsg = "still checking the repo — try again in a moment"
					return m, nil
				}
				if m.repoHealth.HasCommitGraph && m.repoHealth.WriteCommitGraphValue == "true" {
					m.statusMsg = "commit-graph present and auto-refresh already on"
					return m, nil
				}
				if m.running {
					return m, nil // an op is already in flight
				}
				// Same code path as the notice's "write + keep fresh" action.
				return m.startCommitGraphWriteAndEnable()
```

5. `openSettings` — refresh health so the label is current:

```go
func (m Model) openSettings() (Model, tea.Cmd) {
	m = m.pushLayer(&settingsPopup{})
	return m, m.repoHealthCmd(m.noticeGen)
}
```

`openSettings` changes signature from `Model` to `(Model, tea.Cmd)` — update its call site (the `,` key case in `model.go`: `return m.openSettings()` — grep `openSettings()` and adjust; there is one production call site plus tests).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestCommitGraph|TestOpenSettings'`
Expected: PASS. Then the full package: `go test ./internal/tui/` — some existing settings tests call `m.openSettings()`; fix their call sites for the new signature if they break (mechanical: `m = m.openSettings()` → `m, _ = m.openSettings()`).

- [ ] **Step 5: gofmt and commit**

```bash
gofmt -w internal/tui/settings_popup.go internal/tui/settings_commitgraph_test.go
git add internal/tui/settings_popup.go internal/tui/settings_commitgraph_test.go internal/tui/model.go
git commit -m "feat(tui): Settings 'Commit-graph' row — state label + one-key write+enable

Shows present/missing + auto-refresh state (health re-read on Settings
open); enter runs the same write+enable path as the notice action. Already
healthy → status no-op.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n"
```

---

### Task 7: skills — `adding-notifications` + `updating-git-config-options`

**Files:**
- Create: `.claude/skills/adding-notifications/SKILL.md`
- Create: `.claude/skills/updating-git-config-options/SKILL.md`

**Interfaces:**
- Consumes: the shipped shapes from Tasks 1–6 — VERIFY every named identifier against the real code before committing (the skill documents reality, not this plan).
- Produces: two project skills for future sessions.

- [ ] **Step 1: Write `.claude/skills/adding-notifications/SKILL.md`**

```markdown
---
name: adding-notifications
description: Use when adding a repo-health check/notice to gigagit's notification center (the blinking `! N notice` segment + `!` dialog), or when changing notice lifecycle, dismissal, or blink behavior in internal/tui/notify.go.
---

# Adding a notification (health check + notice)

## What this is

On repo load (startup, `R` switch, Settings open) gg runs `domain.RepoHealth`
in the background; `internal/tui/notify.go` turns the facts into **notices**
— each with a stable id, a title, detail lines, and an action list. Unread
notices blink the red `! N notice` status segment (style alternation on a
self-stopping ~800ms tick); `!` opens the dialog. Adding a notice =
(1) facts in `RepoHealth`, (2) one builder func, (3) one line in
`applyRepoHealth`.

## Checklist

1. **Detection facts** — extend `model.RepoHealth` + `domain.RepoHealth`
   (`internal/domain/repohealth.go`). Facts must be CHEAP: filesystem stats
   on the git common dir, or a `ConfigGet`. No git walks — the check runs on
   every load and must never delay first paint.
2. **Builder** — add `func <x>Notice(h model.RepoHealth) *notice` in
   `notify.go` returning nil unless the recommendation applies. Give it:
   - `id`: a stable snake_case key (e.g. `commit_graph_recommend`). It is
     the PERSISTED dismissal key — never rename a shipped id.
   - `repoKey`: `h.GitCommonDir` (the promptstate dismissal scope).
   - `actions`: label + `run func(Model) (Model, tea.Cmd)`; a fixing action
     must reuse existing engine ops via `m.startOp` (see
     `updating-git-config-options` for config writes). Always include
     `{label: "Not now (ask again next load)"}` (run nil) and
     `{label: "Never for this repo", never: true}` — never trap the user
     into fixing.
3. **Register** — in `applyRepoHealth`, add alongside the commit-graph line:
   `if n := <x>Notice(msg.health); n != nil && !dismissed[n.id] && !m.noticeSessionDismissed[n.id] { next = append(next, *n) }`
4. **Dismissal lifecycle** — free. "Not now" records
   `m.noticeSessionDismissed[id]` (cleared on reRoot → re-evaluated next
   load); "Never" persists via `promptstate.Store.DismissNotice(repoKey, id)`.
   Session-dismissal filtering is what stops a mid-session health re-read
   (Settings open triggers one) from resurrecting a Not-now'd notice.
5. **Chaining two ops** — set `m.pendingNoticeConfig = &engine.SetGitConfig{…}`
   before `m.startOp(firstOp)`; the `opFinishedMsg` handler chains it on
   success (`Changed:true`) and clears it unconditionally (the
   `pendingPushTags` pattern).

## Tests to write (exemplars: `notify_test.go`, `notice_popup_test.go`)

- Detection unit tests on the builder: fires on the exact conditions, nil on
  each negated condition.
- Drive `repoHealthMsg` through `Update`: notice appears, unread+blink armed
  only for NEW ids, stale gen dropped, persisted + session dismissals filter.
- Action wiring end-to-end on a real repo (`driveOp`) when the action runs ops.
- Always inject a temp prompt store
  (`promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))`).

## Rules

- Health check failures are silent (best-effort) — never a statusMsg.
- The blink is style alternation; never emit terminal blink escapes.
- The segment hides while `m.proc != nil` (conflict process owns the screen).
- Notices are per-repo session state: reRoot clears the list, bumps
  `noticeGen`, resets session dismissals.
```

- [ ] **Step 2: Write `.claude/skills/updating-git-config-options/SKILL.md`**

```markdown
---
name: updating-git-config-options
description: Use when a gigagit feature needs to READ or WRITE a git config option (git config --local/--global) — the verbs, the SetGitConfig engine op, why writes are ops, and how the TUI/notice/Settings surfaces share one path.
---

# Updating git config options

## The stack, bottom to top

1. **Verbs** (`internal/git/config.go`):
   `ConfigGet(ctx, scope, key) (value string, set bool, err error)` — exit 1
   = unset, not an error; `ConfigSet(ctx, scope, key, value) error`.
   Scopes: `git.ConfigLocal` / `git.ConfigGlobal` / `git.ConfigEffective`
   (merged; get-only). One verb = one `git config` invocation.
2. **The op** (`internal/engine/set_git_config.go`):
   `engine.SetGitConfig{Key, Value string, Global bool}` — decision-free,
   `LockMode() = repogate.Read`. **Why an op and not a direct verb call:**
   frontends may not import `internal/git` (archtest), and running through
   `domain.Execute` buys the repo-gate reservation, op events (busy line,
   oplog spans), and error surfacing for free. `Global` is a bool, not
   `git.ConfigScope`, precisely so the TUI can construct it.
   (`SetIdentity` remains the dedicated user.name/email pair op.)
3. **Reads from a frontend** go through a domain query, never a raw verb:
   `domain.Identity` (both scopes distinct) and `domain.RepoHealth`
   (`fetch.writeCommitGraph`, local-then-global) are the exemplars.

## Choosing scope

- Repo-specific behavior (e.g. `fetch.writeCommitGraph` for one big repo):
  **local** (`Global: false`) — never surprise the user's other repos.
- User preference expressed as "always": **global**, and only when the user
  explicitly chose it.

## The surfaces that share this path

- Notice actions (`notify.go`): `m.startOp(engine.SetGitConfig{…})`, or
  chained after another op via `m.pendingNoticeConfig` (the
  `pendingPushTags` chain pattern in `opFinishedMsg`).
- Settings rows (`settings_popup.go`): the "Commit-graph" row calls the SAME
  shared func as the notice action (`startCommitGraphWriteAndEnable`) — one
  code path, two entrances.
- Stage 3's git-config explorer will add set/unset on curated keys through
  the same op (extend with `Unset bool` + a `git.ConfigUnset` verb there —
  do NOT add a second write op).

## Tests

- Verb argv: `gitexec.FakeRunner` + `f.Calls[0].Argv` assertion.
- Op behavior: real git in `t.TempDir()` (`newRepo(t)` in engine tests) with
  `t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))`
  and `t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)` so the developer's real
  config is never read or written. Assert the OTHER scope untouched.
- TUI wiring: `driveOp(t, m, cmd)` then read the value back with raw
  `exec.Command("git", "-C", dir, "config", "--local", key)`.

## Rules

- `internal/config` (`.gg.toml`) stays read-only at runtime except its
  registered line-edit writers — git config and gg config are different
  systems; don't cross them.
- Never shell out to `git config` from the TUI/CLI directly — archtest will
  fail the build, and you'd skip the gate + oplog.
```

- [ ] **Step 3: Verify both skills against reality**

Run: `grep -rn "startCommitGraphWriteAndEnable\|pendingNoticeConfig\|DismissNotice\|noticeSessionDismissed\|SetGitConfig{" internal/tui/ internal/engine/ --include="*.go" | grep -v _test | head -20`
Expected: every identifier both skills name appears. Fix the skills (not the code) on any mismatch.

- [ ] **Step 4: Commit**

```bash
git add .claude/skills/adding-notifications/ .claude/skills/updating-git-config-options/
git commit -m "docs(skills): adding-notifications + updating-git-config-options

How to add a health check/notice (cheap detection, stable ids, dismissal
lifecycle, op-backed actions) and the one sanctioned git-config write path
(verbs → SetGitConfig op → domain.Execute; local vs global; the three
surfaces that share it).

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n"
```

---

### Task 8: docs + full race gate + test binary

**Files:**
- Modify: `CHANGELOG.md` (top of `### Added` under `## [Unreleased]`)
- Modify: `README.md` (new Notifications paragraphs near the Settings docs)
- Modify: `CLAUDE.md` (package map: `engine`, `domain`, `model`, `tui` rows)

**Interfaces:** none — documents Tasks 1–7. Verify wording against the real code.

- [ ] **Step 1: CHANGELOG entry** (top of `### Added`):

```markdown
- **Notification center.** On repo load gg runs cheap health checks; findings
  show as a blinking red `! N notice` status segment and a **`!`** dialog.
  First check: a big repo (packs ≥ 100 MB) with no commit-graph file and
  `fetch.writeCommitGraph` unset gets "Commit browsing can be ~10× faster in
  this repo" with one-keystroke fixes — *Write commit-graph now + keep it
  fresh* (runs `git commit-graph write --reachable`, then sets
  `fetch.writeCommitGraph=true` locally), *Enable auto-refresh only*, *Not
  now* (asks again next load), or *Never for this repo* (persisted in
  `<state>/gg/prompts.toml`). Settings gains a **"Commit-graph"** row showing
  the same state with the same one-key fix. New engine ops
  `WriteCommitGraph` + `SetGitConfig` back it (the generic config write that
  stage 3's explorer will reuse).
```

- [ ] **Step 2: README** — after the Settings-area paragraphs (near the related-option-prompts paragraph added in stage 1), add:

```markdown
### Notifications

gg checks repo health in the background on every load. When it finds
something worth fixing, a red **`! N notice`** segment blinks in the status
bar; press **`!`** to open the notification center, pick a notice, and choose
an action. The first check targets big repos (≥ 100 MB of packs) without a
commit-graph file: writing one (`git commit-graph write --reachable`) makes
ordered commit browsing roughly 10× faster, and enabling
`fetch.writeCommitGraph` keeps it fresh from then on. Actions: write + keep
fresh, enable only, *Not now* (asks again next load), or *Never for this
repo* (remembered in `<state>/gg/prompts.toml`). The Settings (`,`) →
"Commit-graph" row shows the current state and applies the same fix.
```

- [ ] **Step 3: CLAUDE.md package map** — append to these rows (inside each cell, before its closing `|`):

- `engine` row: ` Also \`WriteCommitGraph\` (streams \`git commit-graph write --reachable\`; LockMode Read — a derived cache under .git/objects) and \`SetGitConfig{Key, Value, Global bool}\` — the generic single-key config write (Global is a bool, not git.ConfigScope, so frontends can construct it); both back the notification center; stage 3's config explorer reuses SetGitConfig.`
- `domain` row: ` \`RepoHealth(ctx)\` — cheap health snapshot (pack bytes via one ReadDir, commit-graph presence via stat, \`fetch.writeCommitGraph\` at both explicit scopes) behind the TUI notification center; GitCommonDir rides along as the per-repo dismissal key.`
- `tui` row: ` **Notification center** (\`notify.go\`, \`notice_popup.go\`): \`domain.RepoHealth\` runs in the background on startup/reRoot/Settings-open (gen-guarded \`repoHealthMsg\`); notices carry stable ids + op-backed actions; unread notices blink the red \`! N notice\` status segment (style alternation on a self-stopping 800ms tick — never terminal blink escapes); \`!\` opens the dialog (list → per-notice actions; esc backs out); "Not now" = session dismissal (\`noticeSessionDismissed\`, cleared on reRoot), "Never for this repo" persists via \`promptstate.DismissNotice\`; the write+enable action chains \`WriteCommitGraph\` → \`SetGitConfig\` via \`pendingNoticeConfig\` (the pendingPushTags pattern); Settings "Commit-graph" row shares \`startCommitGraphWriteAndEnable\`.`
- `model` row: ` Also \`model.RepoHealth\` — the stat-level health snapshot behind the notification center.`

- [ ] **Step 4: Full verification**

```bash
./test.sh race
```

Expected: all stages green (vet+gofmt → unit → e2e). This takes several minutes — set a generous timeout and do not kill it early. Fix anything that fails before committing.

- [ ] **Step 5: Commit and build**

```bash
git add CHANGELOG.md README.md CLAUDE.md
git commit -m "docs: record the notification center (stage 2)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n"
go build -o ./gg ./cmd/gg
```

Report the ABSOLUTE binary path `/mnt/t/others/gigagit/.claude/worktrees/notification-center/gg` for manual verification (open the linux bench repo → `!` should surface the commit-graph notice if that repo still lacks `fetch.writeCommitGraph`). The user merges — do not merge.
