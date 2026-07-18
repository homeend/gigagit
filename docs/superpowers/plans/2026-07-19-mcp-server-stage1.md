# gg MCP Server Stage 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A `gg mcp` stdio MCP server exposing gg's non-git value — the live TUI session snapshot, bookmarks, shelves, gg-specific compare and export — as 11 read-only/safe tools, plus the TUI-side session-snapshot writer that feeds `gg_ui_state`.

**Architecture:** Two independent halves joined by one file: the TUI serializes an agent-relevant slice of its `Model` to `$XDG_STATE_HOME/gg/sessions/<EncodeRepoKey(commonDir)>/ui-state.json` (piggybacking on the existing 1-second heartbeat tick, write-on-change, atomic rename); a new `internal/mcp` package — a domain-only frontend like `internal/cli` — serves MCP over stdio via the official Go SDK, reading that file for `gg_ui_state` and calling existing `domain` methods for everything else. One new git verb (`DiffNoIndex`) backs the per-file compare tool.

**Tech Stack:** Go 1.26, `github.com/modelcontextprotocol/go-sdk` **v1.6.1** (the one new dependency; stdio transport in production, in-memory transports in tests), existing `internal/domain`/`internal/engine` surface, real-git tests in `t.TempDir()`.

**Spec:** `docs/superpowers/specs/2026-07-19-mcp-server-stage1-design.md` — the authoritative tool contracts and snapshot schema.

## Global Constraints

- Stage 1 is the **safe surface only**: no repo mutation. The only writes are the TUI's snapshot file and `gg_export` writing into a caller-named directory.
- `internal/mcp` may import `domain`/`model`/`config`/`buildinfo` + the SDK; it must **never** import `internal/git`, `internal/tui`, or `internal/cli` (archtest-enforced, Task 4).
- SDK version is exactly `github.com/modelcontextprotocol/go-sdk v1.6.1`.
- Tool names exactly: `gg_ui_state`, `gg_bookmarks_list`, `gg_bookmark_get`, `gg_bookmark_read`, `gg_shelf_buckets`, `gg_shelf_list`, `gg_shelf_commit_files`, `gg_shelf_read`, `gg_compare_trees`, `gg_compare_file`, `gg_export`.
- All JSON field names lower_snake_case. Optional/empty object fields carry `omitempty` (absent, not null) — except `gg_ui_state`'s `session`, which is literally `null` when absent.
- Defaults: `max_bytes` 262144, list `limit` 100, shelf `bucket` `"default"`.
- Snapshot payload: English protocol values only (hashes, paths, engine op names). The `status` field is the one documented display-only exception (may be localized; agents must not parse it).
- Error contract: every tool failure is a one-line English message naming the fix where one exists; no stack traces or raw git stderr dumps as the lead line.
- Engine ops run only via `domain.Execute`; the only reachable decision is id `"overwrite"` (options `"overwrite"`/`"cancel"`), answered from the `overwrite` tool parameter; any other decision id is a loud error.
- Snapshot path helper: `config.SessionSnapshotPath(commonDir)` → `<state>/gg/sessions/<EncodeRepoKey(commonDir)>/ui-state.json`, `<state>` resolved exactly like `repos.DefaultStatePath` (Windows `%LocalAppData%`, else `$XDG_STATE_HOME`, else `~/.local/state`).
- Follow TDD; run `go build ./...` + the named test commands per step; `./test.sh race` must be green before merge (run once at the end, Task 9).
- Commit after every task (small commits within a task where steps say so).

## File Structure

| File | Responsibility |
|---|---|
| `internal/git/diffnoindex.go` (new) | `DiffNoIndex` verb: `git diff --no-index`, exit 1 tolerated |
| `internal/domain/query.go` (modify) | `Service.DiffNoIndex` query wrapper |
| `internal/config/config.go` (modify) | `SessionSnapshotPath` + `stateHome` helper |
| `internal/tui/session_snapshot.go` (new) | snapshot struct, builder, write/remove, proto-name mappers |
| `internal/tui/model.go` (modify) | Model fields, heartbeat hook, reRoot hook |
| `internal/tui/op.go` (modify) | `opName` set in `startOp` |
| `internal/tui/run.go` (modify) | snapshot-target init + exit removal |
| `internal/mcp/server.go` (new) | package doc, `Server`, `New`, `sdkServer`, `Serve`, repo helpers |
| `internal/mcp/types.go` (new) | `RepoInfo`, shared reply shapes |
| `internal/mcp/payload.go` (new) | `textPayload` text/binary/truncation contract |
| `internal/mcp/state.go` (new) | `gg_ui_state` |
| `internal/mcp/bookmarks.go` (new) | `gg_bookmarks_list` / `gg_bookmark_get` / `gg_bookmark_read` |
| `internal/mcp/shelf.go` (new) | `gg_shelf_buckets` / `gg_shelf_list` / `gg_shelf_commit_files` / `gg_shelf_read` |
| `internal/mcp/compare.go` (new) | `gg_compare_trees` / `gg_compare_file` |
| `internal/mcp/export.go` (new) | `gg_export` |
| `internal/mcp/decider.go` (new) | `staticDecider` + `runOp` event drain |
| `internal/mcp/server_test.go` (new) | shared test harness (real repo + in-memory MCP session) |
| `internal/domain/shelf.go` (modify) | `Service.ShelfFind` (entry metadata by id) |
| `cmd/gg/main.go` (modify) | route `gg mcp` |
| `internal/archtest/import_guard_test.go` (modify) | add `internal/mcp` to both guards |

---

### Task 1: `DiffNoIndex` git verb + domain wrapper

**Files:**
- Create: `internal/git/diffnoindex.go`
- Create: `internal/git/diffnoindex_test.go`
- Modify: `internal/domain/query.go` (append)
- Test: `internal/domain/` builds (wrapper is exercised through Task 7's MCP tests)

**Interfaces:**
- Consumes: `gitcmd.New`, `r.Runner.Run` (`gitexec.Result{Stdout, ExitCode}`), `query[T]` helper in `internal/domain/query.go`.
- Produces: `func (r *Repo) DiffNoIndex(ctx context.Context, a, b string) (string, error)` and `func (s *Service) DiffNoIndex(ctx context.Context, a, b string) (string, error)` — unified diff text, `""` when identical. Task 7 calls the domain one.

- [ ] **Step 1: Write the failing test**

`internal/git/diffnoindex_test.go`:

```go
package git

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

// Exit 1 from `git diff --no-index` means "files differ" — the normal outcome,
// not an error (the ConfigUnset exit-5 pattern).
func TestDiffNoIndexExitOneIsDiff(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git diff --no-index", gitexec.Result{Stdout: "--- a\n+++ b\n@@\n", ExitCode: 1})
	f.SetError("git diff --no-index", errors.New("exit status 1"))
	r := &Repo{Runner: f}
	out, err := r.DiffNoIndex(context.Background(), "/tmp/a", "/tmp/b")
	if err != nil {
		t.Fatalf("exit 1 must not be an error: %v", err)
	}
	if !strings.Contains(out, "+++ b") {
		t.Fatalf("diff output lost: %q", out)
	}
	call := f.Calls[len(f.Calls)-1]
	want := []string{"diff", "--no-index", "--", "/tmp/a", "/tmp/b"}
	if len(call.Argv) != len(want) {
		t.Fatalf("argv = %v, want %v", call.Argv, want)
	}
	for i := range want {
		if call.Argv[i] != want[i] {
			t.Fatalf("argv = %v, want %v", call.Argv, want)
		}
	}
}

func TestDiffNoIndexIdentical(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git diff --no-index", gitexec.Result{Stdout: "", ExitCode: 0})
	r := &Repo{Runner: f}
	out, err := r.DiffNoIndex(context.Background(), "/tmp/a", "/tmp/a")
	if err != nil || out != "" {
		t.Fatalf("identical files: out=%q err=%v", out, err)
	}
}

func TestDiffNoIndexRealError(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git diff --no-index", gitexec.Result{ExitCode: 128, Stderr: "fatal: bad"})
	f.SetError("git diff --no-index", errors.New("exit status 128"))
	r := &Repo{Runner: f}
	if _, err := r.DiffNoIndex(context.Background(), "/nope", "/nope2"); err == nil {
		t.Fatal("exit 128 must surface as an error")
	}
}
```

Note: if `Repo` construction in this package's tests conventionally differs from `&Repo{Runner: f}`, match the neighboring `internal/git` FakeRunner tests — but `&Repo{Runner: f}` is the shape `internal/domain/bookmark_test.go` uses.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run TestDiffNoIndex -v`
Expected: FAIL — `r.DiffNoIndex undefined`.

- [ ] **Step 3: Write the verb**

`internal/git/diffnoindex.go`:

```go
package git

import (
	"context"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// DiffNoIndex diffs two absolute filesystem paths outside any git index
// (`git diff --no-index -- <a> <b>`). One invocation. git exits 1 when the
// files differ — the normal "has a diff" outcome, not an error — so exit 1
// is mapped to success with the diff text (the ConfigUnset exit-5 pattern);
// exit 0 is an empty diff (identical). Any other failure propagates.
func (r *Repo) DiffNoIndex(ctx context.Context, a, b string) (string, error) {
	argv := gitcmd.New("diff").Arg("--no-index", "--", a, b).ToArgv()
	res, err := r.Runner.Run(ctx, "git diff --no-index", argv)
	if err != nil {
		if res.ExitCode == 1 {
			return res.Stdout, nil
		}
		return "", err
	}
	return res.Stdout, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/git/ -run TestDiffNoIndex -v`
Expected: PASS (3/3).

- [ ] **Step 5: Add the domain wrapper**

Append to `internal/domain/query.go`:

```go
// DiffNoIndex returns the unified diff between two absolute filesystem paths
// (`git diff --no-index`), under a Read reservation. "" means identical.
// Backs the MCP gg_compare_file tool, which materializes both sides to temp
// files first.
func (s *Service) DiffNoIndex(ctx context.Context, a, b string) (string, error) {
	return query(ctx, s, "diffNoIndex:"+a+":"+b, func(ctx context.Context) (string, error) {
		return s.repo.DiffNoIndex(ctx, a, b)
	})
}
```

- [ ] **Step 6: Build + full package tests**

Run: `go build ./... && go test ./internal/git/ ./internal/domain/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/git/diffnoindex.go internal/git/diffnoindex_test.go internal/domain/query.go
git commit -m "feat(git): DiffNoIndex verb + domain wrapper (exit 1 = diff, not error)"
```

---

### Task 2: `config.SessionSnapshotPath`

**Files:**
- Modify: `internal/config/config.go` (append near `EncodeRepoKey`/`PrivateRepoPath`)
- Test: `internal/config/config_test.go` (append)

**Interfaces:**
- Consumes: `EncodeRepoKey` (same file).
- Produces: `func SessionSnapshotPath(commonDir string) string` — `""` disables the snapshot. Task 3 (writer) and Task 4 (`gg_ui_state`) both call it, which is what keeps writer and reader on the same file.

- [ ] **Step 1: Write the failing test**

Append to `internal/config/config_test.go`:

```go
func TestSessionSnapshotPath(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/xdg-state")
	got := SessionSnapshotPath("/mnt/repo/.git")
	want := filepath.Join("/xdg-state", "gg", "sessions", EncodeRepoKey("/mnt/repo/.git"), "ui-state.json")
	if got != want {
		t.Fatalf("SessionSnapshotPath = %q, want %q", got, want)
	}
	if SessionSnapshotPath("") != "" {
		t.Fatal("empty commonDir must disable the snapshot (empty path)")
	}
	other := SessionSnapshotPath("/mnt/other/.git")
	if other == got {
		t.Fatal("distinct repos must map to distinct snapshot paths")
	}
}
```

(`filepath` is already imported by this test file; if not, add it.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestSessionSnapshotPath -v`
Expected: FAIL — `undefined: SessionSnapshotPath`.

- [ ] **Step 3: Implement**

Append to `internal/config/config.go` (after `PrivateRepoPath`; add `runtime` to imports if absent):

```go
// stateHome resolves the machine-local state root exactly like
// repos.DefaultStatePath: %LocalAppData% on Windows, else $XDG_STATE_HOME,
// else ~/.local/state. "" when no home exists.
func stateHome() string {
	if runtime.GOOS == "windows" {
		if lad := os.Getenv("LocalAppData"); lad != "" {
			return lad
		}
	}
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return s
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state")
}

// SessionSnapshotPath is the TUI session-snapshot file for the repo whose git
// COMMON dir is commonDir (always absolute — the GitCommonDir verb passes
// --path-format=absolute): <state>/gg/sessions/<EncodeRepoKey(commonDir)>/ui-state.json.
// Keyed by common dir so every worktree of a repo shares one session identity.
// "" (snapshot disabled) when commonDir is "" or no state root exists.
func SessionSnapshotPath(commonDir string) string {
	if commonDir == "" {
		return ""
	}
	root := stateHome()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "gg", "sessions", EncodeRepoKey(commonDir), "ui-state.json")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestSessionSnapshotPath -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): SessionSnapshotPath — per-repo TUI session snapshot location"
```

---

### Task 3: TUI session-snapshot writer

**Files:**
- Create: `internal/tui/session_snapshot.go`
- Create: `internal/tui/session_snapshot_test.go`
- Modify: `internal/tui/model.go` (Model fields; `heartbeatMsg` case ~line 1861; `reRoot` ~line 2968)
- Modify: `internal/tui/op.go` (`startOp` ~line 186; the `opFinishedMsg` handling that sets `m.running = false`)
- Modify: `internal/tui/run.go`

**Interfaces:**
- Consumes: `config.SessionSnapshotPath` (Task 2); Model accessors `m.backingIndex(panel)`, `m.selectedBranch()`, `m.selectedRemote()`, `m.selectedWorktree()`, `m.focusedBookmark()`, `m.diffLayer()`, `m.currentBranchTipHash()`, `m.status.Conflicts()`, `m.isFilesPanel(p)`, `layerOf[*bookmarkPopup](m)`, `layerOf[*shelfPopup](m)`; `engine.OpName(op)`.
- Produces: `buildSessionSnapshot(m Model) sessionSnapshot` (pure), `(m Model) maybeWriteSnapshot() Model`, `(m Model) initSnapshotTarget() Model`, `writeSnapshotFile(path string, data []byte)`, `removeSnapshotFile(path string)`; new Model fields `snapshotPath`, `snapshotCommonDir`, `snapshotWorktree string`, `lastSnapshot []byte`, `opName string`. The JSON emitted here is the exact contract `gg_ui_state` (Task 4) returns — schema in the spec's "Schema (version 1)" section.

- [ ] **Step 1: Write the failing tests**

`internal/tui/session_snapshot_test.go`:

```go
package tui

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

func TestBuildSessionSnapshotFields(t *testing.T) {
	m := newTestModel(t)
	m.snapshotCommonDir = "/repo/.git"
	m.snapshotWorktree = "/repo"
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "aaa111", Subject: "one"}, {Hash: "bbb222", Subject: "two"}}
	m.sel[panelCommits] = 0
	m.commitCompareSet = map[string]bool{"bbb222": true}
	m.fileMarks = map[string]bool{"b.go": true, "a.go": true}
	m.conflict = domain.ConflictState{Op: "merge", Source: "feat", Target: "main"}
	m.statusMsg = "hello"

	s := buildSessionSnapshot(m)
	if s.Version != 1 {
		t.Fatalf("version = %d", s.Version)
	}
	if s.PID != os.Getpid() {
		t.Fatalf("pid = %d", s.PID)
	}
	if s.Repo.CommonDir != "/repo/.git" || s.Repo.Worktree != "/repo" {
		t.Fatalf("repo = %+v", s.Repo)
	}
	if s.Focus.Panel != "commits" {
		t.Fatalf("focus.panel = %q", s.Focus.Panel)
	}
	if s.Cursor.Commit == nil || s.Cursor.Commit.Hash != "aaa111" || s.Cursor.Commit.Subject != "one" {
		t.Fatalf("cursor.commit = %+v", s.Cursor.Commit)
	}
	if !slices.Equal(s.MarkedCommits, []string{"bbb222"}) {
		t.Fatalf("marked_commits = %v", s.MarkedCommits)
	}
	if !slices.Equal(s.MarkedFiles, []string{"a.go", "b.go"}) {
		t.Fatalf("marked_files must be sorted: %v", s.MarkedFiles)
	}
	if s.Conflict == nil || s.Conflict.Op != "merge" || s.Conflict.Target != "main" {
		t.Fatalf("conflict = %+v", s.Conflict)
	}
	if s.Status != "hello" {
		t.Fatalf("status = %q", s.Status)
	}
	if s.FilesView != nil || s.Switcher != nil || s.Filter != nil {
		t.Fatalf("closed surfaces must be nil: %+v %+v %+v", s.FilesView, s.Switcher, s.Filter)
	}
	if s.WrittenAt != "" {
		t.Fatal("builder must not stamp written_at (stamped at write time)")
	}
}

func TestBuildSessionSnapshotFilterAndScope(t *testing.T) {
	m := newTestModel(t)
	m.filterPanel = panelCommits
	m.filterQuery = "fix"
	m.highlightQuery = "wip"
	m.commitScopeBranches = []string{"main", "feat"}
	s := buildSessionSnapshot(m)
	if s.Filter == nil || s.Filter.Panel != "commits" || s.Filter.Query != "fix" || s.Filter.Highlight != "wip" {
		t.Fatalf("filter = %+v", s.Filter)
	}
	if !slices.Equal(s.CommitScope, []string{"main", "feat"}) {
		t.Fatalf("commit_scope = %v", s.CommitScope)
	}
}

func TestSnapshotJSONKeys(t *testing.T) {
	m := newTestModel(t)
	s := buildSessionSnapshot(m)
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"version", "pid", "repo", "focus", "cursor"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("marshalled snapshot missing %q: %s", key, data)
		}
	}
}

func TestMaybeWriteSnapshotOnlyOnChange(t *testing.T) {
	m := newTestModel(t)
	m.snapshotPath = filepath.Join(t.TempDir(), "ui-state.json")

	m = m.maybeWriteSnapshot()
	data1, err := os.ReadFile(m.snapshotPath)
	if err != nil {
		t.Fatalf("first heartbeat must write: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data1, &raw); err != nil {
		t.Fatalf("snapshot is not valid JSON: %v", err)
	}
	if raw["written_at"] == "" {
		t.Fatal("written file must carry written_at")
	}

	m = m.maybeWriteSnapshot() // nothing changed
	data2, _ := os.ReadFile(m.snapshotPath)
	if !bytes.Equal(data1, data2) {
		t.Fatal("unchanged state must not rewrite the snapshot")
	}

	m.statusMsg = "changed"
	m = m.maybeWriteSnapshot()
	data3, _ := os.ReadFile(m.snapshotPath)
	if bytes.Equal(data2, data3) {
		t.Fatal("changed state must rewrite the snapshot")
	}

	removeSnapshotFile(m.snapshotPath)
	if _, err := os.Stat(m.snapshotPath); !os.IsNotExist(err) {
		t.Fatal("removeSnapshotFile must delete the file")
	}
}

func TestMaybeWriteSnapshotDisabled(t *testing.T) {
	m := newTestModel(t)
	m.snapshotPath = "" // no state root / not a repo
	m = m.maybeWriteSnapshot()
	// Nothing to assert beyond "no panic": disabled means no write target.
}
```

Note on `newTestModel(t)`: it lives in `internal/tui/source_test.go:17` and returns a ready `Model`. If any of the maps used above (`sel`, `commitCompareSet`, `fileMarks`) are nil on the model it returns, initialize them in the test before assigning.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestBuildSessionSnapshot|TestSnapshotJSONKeys|TestMaybeWriteSnapshot' -v`
Expected: FAIL — `undefined: buildSessionSnapshot` etc.

- [ ] **Step 3: Implement the snapshot file**

`internal/tui/session_snapshot.go`:

```go
package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/model"
)

// sessionSnapshot is the agent-facing session state contract (schema v1) the
// TUI publishes for `gg mcp`'s gg_ui_state tool. Values are English protocol
// data (hashes, repo-relative paths, engine op names) — never translated
// display strings. Status is the one documented exception: a verbatim copy of
// the visible status line, display-only, explicitly non-parseable.
// See docs/superpowers/specs/2026-07-19-mcp-server-stage1-design.md.
type sessionSnapshot struct {
	Version       int            `json:"version"`
	PID           int            `json:"pid"`
	WrittenAt     string         `json:"written_at,omitempty"` // RFC 3339 UTC; stamped at write time
	Repo          snapRepo       `json:"repo"`
	Focus         snapFocus      `json:"focus"`
	Cursor        snapCursor     `json:"cursor"`
	MarkedCommits []string       `json:"marked_commits,omitempty"`
	MarkedFiles   []string       `json:"marked_files,omitempty"`
	FilesView     *snapFilesView `json:"files_view,omitempty"`
	Switcher      *snapSwitcher  `json:"switcher,omitempty"`
	Filter        *snapFilter    `json:"filter,omitempty"`
	CommitScope   []string       `json:"commit_scope,omitempty"`
	Conflict      *snapConflict  `json:"conflict,omitempty"`
	RunningOp     string         `json:"running_op,omitempty"`
	Status        string         `json:"status,omitempty"`
}

type snapRepo struct {
	CommonDir string `json:"common_dir"`
	Worktree  string `json:"worktree"`
	Branch    string `json:"branch,omitempty"`
	Head      string `json:"head,omitempty"`
}

type snapFocus struct {
	Panel     string `json:"panel"`
	LeftTab   string `json:"left_tab,omitempty"`
	BottomTab string `json:"bottom_tab,omitempty"`
}

type snapCommit struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
}

type snapCursor struct {
	Commit       *snapCommit `json:"commit,omitempty"`
	Branch       string      `json:"branch,omitempty"`
	RemoteBranch string      `json:"remote_branch,omitempty"`
	Tag          string      `json:"tag,omitempty"`
	Worktree     string      `json:"worktree,omitempty"`
	File         string      `json:"file,omitempty"`
}

type snapEndpoint struct {
	Kind string `json:"kind"` // worktree|index|commit
	Hash string `json:"hash,omitempty"`
}

type snapFilesView struct {
	Mode         string        `json:"mode"` // changed|full_tree|compare|stash|shelf
	Title        string        `json:"title,omitempty"`
	Commit       string        `json:"commit,omitempty"`
	Left         *snapEndpoint `json:"left,omitempty"`
	Right        *snapEndpoint `json:"right,omitempty"`
	ShelfID      string        `json:"shelf_id,omitempty"`
	SelectedFile string        `json:"selected_file,omitempty"`
	DiffOpen     bool          `json:"diff_open,omitempty"`
}

type snapSwitcher struct {
	Kind       string `json:"kind"` // bookmark|shelf
	SelectedID string `json:"selected_id"`
	Display    string `json:"display,omitempty"`
}

type snapFilter struct {
	Panel     string `json:"panel,omitempty"`
	Query     string `json:"query,omitempty"`
	Highlight string `json:"highlight,omitempty"`
}

type snapConflict struct {
	Op              string   `json:"op"`
	Source          string   `json:"source,omitempty"`
	Target          string   `json:"target,omitempty"`
	ConflictedFiles []string `json:"conflicted_files,omitempty"`
}

// panelProtoName maps a panel to its stable protocol name.
func panelProtoName(p panel) string {
	switch p {
	case panelBranches:
		return "branches"
	case panelWorktrees:
		return "worktrees"
	case panelRemotes:
		return "remotes"
	case panelFiles:
		return "files"
	case panelStaged:
		return "staged"
	case panelCommits:
		return "commits"
	case panelTags:
		return "tags"
	case panelReflog:
		return "reflog"
	}
	return ""
}

// filesModeProtoName maps a filesMode to its stable protocol name.
func filesModeProtoName(fm filesMode) string {
	switch fm {
	case filesModeCompare:
		return "compare"
	case filesModeFullTree:
		return "full_tree"
	case filesModeStash:
		return "stash"
	case filesModeShelf:
		return "shelf"
	}
	return "changed"
}

func endpointProto(e model.Endpoint) *snapEndpoint {
	switch e.Kind {
	case model.EndpointWorkTree:
		return &snapEndpoint{Kind: "worktree"}
	case model.EndpointIndex:
		return &snapEndpoint{Kind: "index"}
	default:
		return &snapEndpoint{Kind: "commit", Hash: e.Hash}
	}
}

// buildSessionSnapshot serializes the agent-relevant slice of the Model. Pure:
// no I/O, no clock (WrittenAt is stamped by maybeWriteSnapshot so the
// write-on-change compare ignores time). Cursor values resolve through the
// same accessors the `.` menus use (backingIndex / selected* / focusedBookmark),
// so what the agent sees is exactly what an action would act on.
func buildSessionSnapshot(m Model) sessionSnapshot {
	s := sessionSnapshot{
		Version: 1,
		PID:     os.Getpid(),
		Repo: snapRepo{
			CommonDir: m.snapshotCommonDir,
			Worktree:  m.snapshotWorktree,
			Branch:    m.status.Branch,
			Head:      m.currentBranchTipHash(),
		},
		Focus: snapFocus{
			Panel:     panelProtoName(m.focus),
			LeftTab:   panelProtoName(m.activeLeftTab),
			BottomTab: panelProtoName(m.activeBottomTab),
		},
		Status: m.statusMsg,
	}
	if bi, ok := m.backingIndex(panelCommits); ok && bi < len(m.commits) {
		c := m.commits[bi]
		s.Cursor.Commit = &snapCommit{Hash: c.Hash, Subject: c.Subject}
	}
	if b, ok := m.selectedBranch(); ok {
		s.Cursor.Branch = b.Name
	}
	if r, ok := m.selectedRemote(); ok {
		s.Cursor.RemoteBranch = r.Name
	}
	if bi, ok := m.backingIndex(panelTags); ok && bi < len(m.tags) {
		s.Cursor.Tag = m.tags[bi].Name
	}
	if w, ok := m.selectedWorktree(); ok {
		s.Cursor.Worktree = w.Path
	}
	for _, c := range m.commits { // feed order; WIP sentinels never match a hash
		if m.commitCompareSet[c.Hash] {
			s.MarkedCommits = append(s.MarkedCommits, c.Hash)
		}
	}
	if len(m.fileMarks) > 0 {
		for p := range m.fileMarks {
			s.MarkedFiles = append(s.MarkedFiles, p)
		}
		slices.Sort(s.MarkedFiles)
	}
	if m.filesView != nil {
		fv := &snapFilesView{
			Mode:     filesModeProtoName(m.filesMode),
			Title:    m.filesTitle,
			Commit:   m.filesHash,
			DiffOpen: m.diffLayer() != nil,
		}
		if m.filesMode == filesModeCompare {
			fv.Left = endpointProto(m.filesLeft)
			fv.Right = endpointProto(m.filesRight)
		}
		if m.filesMode == filesModeShelf {
			fv.ShelfID = m.filesShelfID
		}
		if b, ok := m.focusedBookmark(); ok {
			fv.SelectedFile = b.Path
		}
		s.FilesView = fv
	} else if m.isFilesPanel(m.focus) {
		if b, ok := m.focusedBookmark(); ok {
			s.Cursor.File = b.Path
		}
	}
	if p := layerOf[*bookmarkPopup](m); p != nil {
		if b, ok := p.selected(); ok {
			s.Switcher = &snapSwitcher{Kind: "bookmark", SelectedID: b.ID, Display: b.Address().Display()}
		}
	} else if p := layerOf[*shelfPopup](m); p != nil {
		if e, ok := p.selected(); ok {
			s.Switcher = &snapSwitcher{Kind: "shelf", SelectedID: e.ID, Display: e.Origin.Display()}
		}
	}
	if m.filterQuery != "" || m.highlightQuery != "" {
		s.Filter = &snapFilter{Query: m.filterQuery, Highlight: m.highlightQuery}
		if m.filterQuery != "" {
			s.Filter.Panel = panelProtoName(m.filterPanel)
		}
	}
	if len(m.commitScopeBranches) > 0 {
		s.CommitScope = slices.Clone(m.commitScopeBranches)
	}
	if m.conflict.Op != "" || m.status.Counts().Conflicted > 0 {
		c := &snapConflict{Op: m.conflict.Op, Source: m.conflict.Source, Target: m.conflict.Target}
		for _, f := range m.status.Conflicts() {
			c.ConflictedFiles = append(c.ConflictedFiles, f.Path)
		}
		s.Conflict = c
	}
	if m.running {
		s.RunningOp = m.opName
	}
	return s
}

// maybeWriteSnapshot serializes the current snapshot and writes it only when
// the payload (timestamp excluded) differs from the last written one. Called
// from the perpetual 1s heartbeat; best-effort — a failed write never
// disturbs the TUI.
func (m Model) maybeWriteSnapshot() Model {
	if m.snapshotPath == "" {
		return m
	}
	snap := buildSessionSnapshot(m)
	data, err := json.Marshal(snap) // WrittenAt empty here — the compare key
	if err != nil || bytes.Equal(data, m.lastSnapshot) {
		return m
	}
	m.lastSnapshot = data
	snap.WrittenAt = time.Now().UTC().Format(time.RFC3339)
	out, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return m
	}
	writeSnapshotFile(m.snapshotPath, out)
	return m
}

// writeSnapshotFile writes data atomically (temp file + rename), best-effort.
func writeSnapshotFile(path string, data []byte) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, "ui-state-*.tmp")
	if err != nil {
		return
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(name)
		return
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
	}
}

// removeSnapshotFile deletes the session file (clean exit / repo switch),
// best-effort. The file doubles as session presence.
func removeSnapshotFile(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}

// initSnapshotTarget resolves where this session's snapshot lives. Called from
// run.go at startup and from reRoot after a repo switch (with the new svc).
func (m Model) initSnapshotTarget() Model {
	ctx := context.Background()
	m.snapshotCommonDir, m.snapshotPath = "", ""
	if cd, err := m.svc.GitCommonDir(ctx); err == nil && cd != "" {
		m.snapshotCommonDir = cd
		m.snapshotPath = config.SessionSnapshotPath(cd)
	}
	if top, err := m.svc.TopLevel(ctx); err == nil {
		m.snapshotWorktree = top
	}
	m.lastSnapshot = nil
	return m
}
```

- [ ] **Step 4: Add the Model fields**

In `internal/tui/model.go`, inside the `Model` struct near the existing `statusMsg`/`running` fields (~line 175), add:

```go
	// Session snapshot (agent-facing; see session_snapshot.go). snapshotPath
	// "" = disabled (no repo / no state root). lastSnapshot is the last
	// serialized payload (timestamp-less) for write-on-change.
	snapshotPath      string
	snapshotCommonDir string
	snapshotWorktree  string
	lastSnapshot      []byte
	opName            string // engine.OpName of the in-flight op; "" when idle
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestBuildSessionSnapshot|TestSnapshotJSONKeys|TestMaybeWriteSnapshot' -v`
Expected: PASS. (If `newTestModel` leaves `m.layers` nil, `layerOf` already handles nil — see `layer_stack.go:86`.)

- [ ] **Step 6: Wire the heartbeat, op name, reRoot, and exit hooks**

(a) `internal/tui/model.go` `case heartbeatMsg:` (~line 1861) — insert the snapshot check before the existing return, so it becomes:

```go
	case heartbeatMsg:
		// (existing busy-line comment/logic unchanged)
		m = m.maybeWriteSnapshot()
		return m, tea.Batch(cmd, heartbeatCmd())
```

(b) `internal/tui/op.go` in `startOp` (~line 186), right after `m.running = true`:

```go
	m.opName = engine.OpName(op)
```

(c) In the `opFinishedMsg` handler (grep `m.running = false` in `model.go`/`op.go`; add at every site that ends the op):

```go
	m.opName = ""
```

(d) `internal/tui/model.go` `reRoot` (~line 2968): at the top of the function add `removeSnapshotFile(m.snapshotPath)`; then, after the new `domain` service is assigned to the model being returned (the new model built for the new repo), chain `.initSnapshotTarget()` on it (or `nm = nm.initSnapshotTarget()` before returning).

(e) `internal/tui/run.go`: after the existing `cfg` block (which already computed `top` via `svc.TopLevel`) and before `tea.NewProgram`, add:

```go
	m = m.initSnapshotTarget()
```

and extend the existing `final.(Model)` block after `p.Run()`:

```go
	if fm, ok := final.(Model); ok {
		if fm.opCancel != nil {
			fm.opCancel()
		}
		removeSnapshotFile(fm.snapshotPath)
	}
```

(replacing the current `if fm, ok := ...; ok && fm.opCancel != nil` form).

Note: `initSnapshotTarget` runs two cached domain reads (`GitCommonDir`, `TopLevel`) with a real git call each at startup — both singleflight-cached queries the TUI startup performs anyway; in `newTestModel`-based unit tests it is never called, so tests need no git.

- [ ] **Step 7: Build + full TUI tests**

Run: `go build ./... && go test ./internal/tui/`
Expected: PASS (the existing suite catches any Update-path regression).

- [ ] **Step 8: Commit**

```bash
git add internal/tui/session_snapshot.go internal/tui/session_snapshot_test.go internal/tui/model.go internal/tui/op.go internal/tui/run.go
git commit -m "feat(tui): publish session snapshot for MCP (heartbeat write-on-change)"
```

---

### Task 4: `internal/mcp` scaffold, `gg_ui_state`, `gg mcp` routing, archtest

**Files:**
- Modify: `go.mod`/`go.sum` (`go get github.com/modelcontextprotocol/go-sdk@v1.6.1`)
- Create: `internal/mcp/server.go`, `internal/mcp/types.go`, `internal/mcp/state.go`
- Create: `internal/mcp/server_test.go`, `internal/mcp/state_test.go`
- Modify: `cmd/gg/main.go`
- Modify: `internal/archtest/import_guard_test.go`

**Interfaces:**
- Consumes: `domain.Open(workdir)`, `svc.GitCommonDir`, `svc.TopLevel`, `config.SessionSnapshotPath`, `buildinfo.Version`, SDK (`sdk.NewServer`, `sdk.AddTool`, `sdk.StdioTransport`, `sdk.NewInMemoryTransports`, `sdk.NewClient`).
- Produces: `mcp.Serve(ctx context.Context, workdir string) error` (cmd/gg calls it); `Server{svc, commonDir, worktree, repoErr}`, `New(svc *domain.Service) *Server`, `(s *Server) sdkServer() *sdk.Server`, `(s *Server) repoInfo() RepoInfo`, `(s *Server) repoCheck() error`; `RepoInfo{CommonDir, Worktree string}` with json tags `common_dir`/`worktree`; test harness `newTestEnv(t)` + `(e *testEnv) call/callErr` used by Tasks 5–8. Tool registration hooks `registerBookmarkTools`, `registerShelfTools`, `registerCompareTools`, `registerExportTool` are declared as empty methods here and filled by Tasks 5–8.

- [ ] **Step 1: Add the dependency**

```bash
go get github.com/modelcontextprotocol/go-sdk@v1.6.1 && go mod tidy
```

Expected: `go.mod` gains `github.com/modelcontextprotocol/go-sdk v1.6.1` (plus its transitive requirements, e.g. `github.com/google/jsonschema-go`).

- [ ] **Step 2: Write the failing tests**

`internal/mcp/server_test.go` (the shared harness Tasks 5–8 build on):

```go
package mcp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/domain"
)

// gitRun runs git in dir with a hermetic identity, failing the test on error.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

type testEnv struct {
	dir string // repo worktree
	svc *domain.Service
	cs  *sdk.ClientSession
	sha string // seeded commit (full sha)
}

// newTestEnv builds a real one-commit repo (a.txt), isolates XDG state/config,
// and connects an in-memory MCP client to a fully registered server.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "seed: a.txt")
	sha := gitRun(t, dir, "rev-parse", "HEAD")

	svc := domain.Open(dir)
	srv := New(svc).sdkServer()
	ct, st := sdk.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return &testEnv{dir: dir, svc: svc, cs: cs, sha: sha}
}

func resultText(res *sdk.CallToolResult) string {
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

// call invokes a tool and decodes its JSON reply, failing on any error.
func (e *testEnv) call(t *testing.T, name string, args map[string]any) map[string]any {
	t.Helper()
	res, err := e.cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s: unexpected tool error: %s", name, resultText(res))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(resultText(res)), &out); err != nil {
		t.Fatalf("%s: bad JSON reply %q: %v", name, resultText(res), err)
	}
	return out
}

// callErr invokes a tool expecting a tool error, returning its message.
func (e *testEnv) callErr(t *testing.T, name string, args map[string]any) string {
	t.Helper()
	res, err := e.cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	if !res.IsError {
		t.Fatalf("%s: expected a tool error, got: %s", name, resultText(res))
	}
	return resultText(res)
}

func TestServerListsStageOneTools(t *testing.T) {
	e := newTestEnv(t)
	res, err := e.cs.ListTools(context.Background(), &sdk.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	if !got["gg_ui_state"] {
		t.Fatalf("gg_ui_state not registered; got %v", got)
	}
}

func TestEveryReplyCarriesRepoInfo(t *testing.T) {
	e := newTestEnv(t)
	out := e.call(t, "gg_ui_state", nil)
	repo, ok := out["repo"].(map[string]any)
	if !ok || repo["common_dir"] == "" || repo["worktree"] == "" {
		t.Fatalf("repo info missing: %v", out["repo"])
	}
}
```

`internal/mcp/state_test.go`:

```go
package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
)

func snapshotPathFor(t *testing.T, e *testEnv) string {
	t.Helper()
	cd, err := e.svc.GitCommonDir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p := config.SessionSnapshotPath(cd)
	if p == "" {
		t.Fatal("no snapshot path (XDG_STATE_HOME should be set by newTestEnv)")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUIStateNoSession(t *testing.T) {
	e := newTestEnv(t)
	out := e.call(t, "gg_ui_state", nil)
	if out["session"] != nil {
		t.Fatalf("session must be null with no snapshot, got %v", out["session"])
	}
	if hint, _ := out["hint"].(string); !strings.Contains(hint, "no gg TUI session") {
		t.Fatalf("hint = %v", out["hint"])
	}
}

func TestUIStateReadsSnapshot(t *testing.T) {
	e := newTestEnv(t)
	p := snapshotPathFor(t, e)
	body := `{"version":1,"pid":42,"focus":{"panel":"commits"},"marked_commits":["abc"]}`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out := e.call(t, "gg_ui_state", nil)
	sess, ok := out["session"].(map[string]any)
	if !ok {
		t.Fatalf("session = %v", out["session"])
	}
	if sess["pid"].(float64) != 42 {
		t.Fatalf("session.pid = %v", sess["pid"])
	}
	if focus := sess["focus"].(map[string]any); focus["panel"] != "commits" {
		t.Fatalf("session.focus = %v", sess["focus"])
	}
}

func TestUIStateVersionTooNew(t *testing.T) {
	e := newTestEnv(t)
	p := snapshotPathFor(t, e)
	if err := os.WriteFile(p, []byte(`{"version":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := e.callErr(t, "gg_ui_state", nil)
	if !strings.Contains(msg, "newer") {
		t.Fatalf("expected version-too-new error, got: %s", msg)
	}
}

func TestUIStateCorruptSnapshot(t *testing.T) {
	e := newTestEnv(t)
	p := snapshotPathFor(t, e)
	if err := os.WriteFile(p, []byte(`{not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := e.callErr(t, "gg_ui_state", nil)
	if !strings.Contains(msg, "unreadable") {
		t.Fatalf("expected unreadable error, got: %s", msg)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/mcp/ -v`
Expected: FAIL to build — `undefined: New` etc.

- [ ] **Step 4: Implement the scaffold**

`internal/mcp/types.go`:

```go
package mcp

// RepoInfo identifies which repository answered — every tool reply carries it
// so an agent juggling projects can sanity-check the target.
type RepoInfo struct {
	CommonDir string `json:"common_dir"`
	Worktree  string `json:"worktree"`
}
```

`internal/mcp/server.go`:

```go
// Package mcp implements gigagit's MCP (Model Context Protocol) frontend: a
// stdio server exposing gg's NON-git value — the TUI session snapshot,
// bookmarks, shelves, gg-specific compare and export — to AI agents. Stage 1
// is the safe surface only (reads, compares, export-to-a-directory); it never
// mutates the repository. A domain-only frontend like internal/cli: it never
// imports internal/git (archtest-enforced).
package mcp

import (
	"context"
	"fmt"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/buildinfo"
	"github.com/homeend/gigagit/internal/domain"
)

// Server wires gg's domain service to the MCP tool surface.
type Server struct {
	svc       *domain.Service
	commonDir string // absolute git common dir; "" when repo resolution failed
	worktree  string // worktree top-level; "" when repo resolution failed
	repoErr   error  // startup repo-resolution failure, surfaced per-tool
}

// New resolves the repo identity once. A failure is remembered, not fatal:
// the server still starts (a server that dies at startup shows up as an
// opaque client-side failure) and every tool reports the problem clearly.
func New(svc *domain.Service) *Server {
	s := &Server{svc: svc}
	ctx := context.Background()
	cd, err := svc.GitCommonDir(ctx)
	if err != nil {
		s.repoErr = fmt.Errorf("not a git repository (run gg mcp from inside a repo): %v", err)
		return s
	}
	s.commonDir = cd
	if top, err := svc.TopLevel(ctx); err == nil {
		s.worktree = top
	}
	return s
}

func (s *Server) repoInfo() RepoInfo {
	return RepoInfo{CommonDir: s.commonDir, Worktree: s.worktree}
}

func (s *Server) repoCheck() error { return s.repoErr }

// sdkServer builds the SDK server with every stage-1 tool registered.
func (s *Server) sdkServer() *sdk.Server {
	srv := sdk.NewServer(&sdk.Implementation{Name: "gg", Version: buildinfo.Version}, nil)
	s.registerStateTool(srv)
	s.registerBookmarkTools(srv)
	s.registerShelfTools(srv)
	s.registerCompareTools(srv)
	s.registerExportTool(srv)
	return srv
}

// Filled by the bookmark/shelf/compare/export tool files.
func (s *Server) registerBookmarkTools(srv *sdk.Server) {}
func (s *Server) registerShelfTools(srv *sdk.Server)    {}
func (s *Server) registerCompareTools(srv *sdk.Server)  {}
func (s *Server) registerExportTool(srv *sdk.Server)    {}

// Serve runs the MCP server over stdio until ctx ends or the client closes.
// workdir resolves the repo like the CLI does (the process cwd for gg mcp).
func Serve(ctx context.Context, workdir string) error {
	return New(domain.Open(workdir)).sdkServer().Run(ctx, &sdk.StdioTransport{})
}
```

(The four empty `register*` methods are placeholders that Tasks 5–8 replace with real registrations — each of those tasks DELETES its stub from `server.go` and defines the method in its own file.)

`internal/mcp/state.go`:

```go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/config"
)

type uiStateOut struct {
	Repo    RepoInfo       `json:"repo"`
	Session map[string]any `json:"session"` // nil marshals as null = no live session
	Hint    string         `json:"hint,omitempty"`
}

func (s *Server) registerStateTool(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{
		Name: "gg_ui_state",
		Description: "Current gg TUI session snapshot for this repository: focused panel, " +
			"per-panel cursor values, marked commits/files, the open files/diff/compare view " +
			"and its selected file, the open bookmark/shelf switcher's highlighted entry, " +
			"active filters, conflict and running-operation state. session is null when no " +
			"gg TUI is running for this repo. The status field is display-only text — do not parse it.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, uiStateOut, error) {
		out := uiStateOut{Repo: s.repoInfo()}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		data, err := os.ReadFile(config.SessionSnapshotPath(s.commonDir))
		if err != nil {
			out.Hint = "no gg TUI session snapshot for this repository"
			return nil, out, nil
		}
		var sess map[string]any
		if err := json.Unmarshal(data, &sess); err != nil {
			return nil, out, fmt.Errorf("session snapshot unreadable: %v", err)
		}
		if v, _ := sess["version"].(float64); v > 1 {
			return nil, out, fmt.Errorf("session snapshot version %d is newer than this gg — upgrade gg", int(v))
		}
		out.Session = sess
		return nil, out, nil
	})
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/mcp/ -v`
Expected: PASS (6 tests).

- [ ] **Step 6: Route `gg mcp` and update the usage line**

In `cmd/gg/main.go`, after the `inspect` routing block (~line 43–), add (mirroring its style; `context` is already imported):

```go
	if len(args) > 0 && args[0] == "mcp" {
		if err := mcp.Serve(context.Background(), "."); err != nil {
			fmt.Fprintln(os.Stderr, "gg mcp:", err)
			os.Exit(1)
		}
		return
	}
```

Add the import `"github.com/homeend/gigagit/internal/mcp"`. In the commands usage line (~line 67) insert `mcp` into the list (after `init`, before `inspect`).

- [ ] **Step 7: Extend archtest**

In `internal/archtest/import_guard_test.go`:

(a) `TestFrontendsDoNotImportGit` — extend the scanned-frontends slice:

```go
	for _, pkg := range []string{
		"github.com/homeend/gigagit/internal/tui",
		"github.com/homeend/gigagit/internal/cli",
		"github.com/homeend/gigagit/internal/mcp",
	} {
```

(b) `TestLayeringDAG` — `internal/mcp` is a frontend beside `tui`/`cli`: append `"mcp"` to the forbidden list of **every** existing row (each currently ends `"tui", "cli", "app"` → `"tui", "cli", "mcp", "app"`), and add the new row:

```go
		"mcp":         {"tui", "cli", "app"},
		"domain":      {"tui", "cli", "mcp", "app"},
```

(i.e. the `domain` row shown updated as the pattern for all rows).

Run: `go test ./internal/archtest/ -v`
Expected: PASS.

- [ ] **Step 8: Build everything + smoke the binary**

```bash
go build ./... && go build -o ./gg ./cmd/gg
printf '' | ./gg mcp; echo "exit=$?"
```

Expected: builds; the server sees stdin close immediately and exits promptly — exit 0 or a clean stdio-closed error, NOT a hang, NOT a panic.

- [ ] **Step 9: Commit**

```bash
git add go.mod go.sum internal/mcp/ cmd/gg/main.go internal/archtest/import_guard_test.go
git commit -m "feat(mcp): internal/mcp frontend scaffold + gg_ui_state + gg mcp stdio subcommand"
```

---

### Task 5: Bookmark tools (`gg_bookmarks_list` / `gg_bookmark_get` / `gg_bookmark_read`)

**Files:**
- Create: `internal/mcp/payload.go`, `internal/mcp/bookmarks.go`
- Create: `internal/mcp/payload_test.go`, `internal/mcp/bookmarks_test.go`
- Modify: `internal/mcp/server.go` (delete the `registerBookmarkTools` stub)

**Interfaces:**
- Consumes: harness from Task 4 (`newTestEnv`, `call`, `callErr`); `svc.BookmarkList(ctx, skip, limit)`, `svc.BookmarkGet(ctx, id)`, `svc.BookmarkBytes(ctx, b)`; `model.Bookmark` (fields `ID, Worktree, Branch, Commit, ShelfID, Path, State, Label, Created`, methods `IsCommit()`, `Address()` → `model.FileAddress` with `Display()`); `model.FileState` constants `StateCommitted, StateShelf, StateStaged, StateUnstaged, StateUntracked`.
- Produces: `textPayload(data []byte, maxBytes int) filePayload` and `filePayload{Text, Binary, Truncated, Size, Hint}` — reused verbatim by Task 6's reads and Task 7's binary detection; `bookmarkRow` + `bookmarkRowFrom(b model.Bookmark) bookmarkRow` reused by nothing else (bookmark-local); `fileStateProto(model.FileState) string`.

- [ ] **Step 1: Write the failing payload test**

`internal/mcp/payload_test.go`:

```go
package mcp

import (
	"strings"
	"testing"
)

func TestTextPayloadPlain(t *testing.T) {
	p := textPayload([]byte("hello"), 0) // 0 → default cap
	if p.Text != "hello" || p.Binary || p.Truncated || p.Size != 5 {
		t.Fatalf("payload = %+v", p)
	}
}

func TestTextPayloadBinary(t *testing.T) {
	p := textPayload([]byte{0x00, 0x01, 'a'}, 0)
	if !p.Binary || p.Text != "" || p.Size != 3 {
		t.Fatalf("payload = %+v", p)
	}
	if !strings.Contains(p.Hint, "gg_export") {
		t.Fatalf("binary hint must point at gg_export: %+v", p)
	}
}

func TestTextPayloadInvalidUTF8IsBinary(t *testing.T) {
	p := textPayload([]byte{0xff, 0xfe, 0xfd}, 0)
	if !p.Binary {
		t.Fatalf("invalid UTF-8 must be binary: %+v", p)
	}
}

func TestTextPayloadTruncates(t *testing.T) {
	data := []byte(strings.Repeat("é", 100)) // 2 bytes per rune
	p := textPayload(data, 51)               // odd cap lands mid-rune
	if !p.Truncated || p.Size != 200 {
		t.Fatalf("payload = %+v", p)
	}
	if len(p.Text) != 50 { // backed off to the rune boundary
		t.Fatalf("truncation split a rune: len=%d", len(p.Text))
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/mcp/ -run TestTextPayload -v`
Expected: FAIL — `undefined: textPayload`.

- [ ] **Step 3: Implement the payload helper**

`internal/mcp/payload.go`:

```go
package mcp

import (
	"bytes"
	"unicode/utf8"
)

const defaultMaxBytes = 262144

// filePayload is the shared text/binary/truncation reply contract for every
// content-reading tool (bookmark_read, shelf_read).
type filePayload struct {
	Text      string `json:"text,omitempty"`
	Binary    bool   `json:"binary,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Size      int    `json:"size"`
	Hint      string `json:"hint,omitempty"`
}

// textPayload classifies data: binary (NUL byte or invalid UTF-8) yields no
// text and a gg_export hint; text over maxBytes is truncated at a rune
// boundary. maxBytes <= 0 means the default cap.
func textPayload(data []byte, maxBytes int) filePayload {
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	p := filePayload{Size: len(data)}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		p.Binary = true
		p.Hint = "binary content — use gg_export to copy it to a directory"
		return p
	}
	if len(data) > maxBytes {
		cut := maxBytes
		for cut > 0 && !utf8.RuneStart(data[cut]) {
			cut--
		}
		p.Text = string(data[:cut])
		p.Truncated = true
		return p
	}
	p.Text = string(data)
	return p
}
```

- [ ] **Step 4: Payload tests pass**

Run: `go test ./internal/mcp/ -run TestTextPayload -v`
Expected: PASS.

- [ ] **Step 5: Write the failing bookmark-tool tests**

`internal/mcp/bookmarks_test.go`:

```go
package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// seedBookmark stores a committed-file bookmark for a.txt at the seed commit.
func seedBookmark(t *testing.T, e *testEnv) model.Bookmark {
	t.Helper()
	b, err := e.svc.BookmarkAdd(context.Background(), model.Bookmark{
		State: model.StateCommitted, Commit: e.sha, Path: "a.txt",
	})
	if err != nil {
		t.Fatalf("BookmarkAdd: %v", err)
	}
	return b
}

func TestBookmarksListAndGet(t *testing.T) {
	e := newTestEnv(t)
	b := seedBookmark(t, e)

	out := e.call(t, "gg_bookmarks_list", nil)
	rows, ok := out["bookmarks"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("bookmarks = %v", out["bookmarks"])
	}
	row := rows[0].(map[string]any)
	if row["id"] != b.ID || row["state"] != "committed" || row["path"] != "a.txt" {
		t.Fatalf("row = %v", row)
	}
	if row["display"] == "" {
		t.Fatal("display missing")
	}

	got := e.call(t, "gg_bookmark_get", map[string]any{"id": b.ID})
	bk := got["bookmark"].(map[string]any)
	if bk["commit"] != e.sha {
		t.Fatalf("get.commit = %v", bk["commit"])
	}
}

func TestBookmarkGetUnknownID(t *testing.T) {
	e := newTestEnv(t)
	msg := e.callErr(t, "gg_bookmark_get", map[string]any{"id": "nope"})
	if !strings.Contains(msg, "bookmark not found") {
		t.Fatalf("error = %s", msg)
	}
}

func TestBookmarkRead(t *testing.T) {
	e := newTestEnv(t)
	b := seedBookmark(t, e)
	out := e.call(t, "gg_bookmark_read", map[string]any{"id": b.ID})
	if out["text"] != "hello\nworld\n" {
		t.Fatalf("text = %q", out["text"])
	}
	if out["size"].(float64) != 12 {
		t.Fatalf("size = %v", out["size"])
	}
}

func TestBookmarkReadCommitPointerRefused(t *testing.T) {
	e := newTestEnv(t)
	cb, err := e.svc.BookmarkAdd(context.Background(), model.Bookmark{
		State: model.StateCommitted, Commit: e.sha, // no Path → commit pointer
	})
	if err != nil {
		t.Fatal(err)
	}
	msg := e.callErr(t, "gg_bookmark_read", map[string]any{"id": cb.ID})
	if !strings.Contains(msg, "gg_export") {
		t.Fatalf("commit-pointer refusal must hint gg_export: %s", msg)
	}
}
```

- [ ] **Step 6: Run to verify failure**

Run: `go test ./internal/mcp/ -run TestBookmark -v`
Expected: FAIL — tools not registered (`unexpected tool error` / unknown tool).

- [ ] **Step 7: Implement the bookmark tools**

Delete the `func (s *Server) registerBookmarkTools(srv *sdk.Server) {}` stub from `server.go`, then create `internal/mcp/bookmarks.go`:

```go
package mcp

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/model"
)

// fileStateProto maps a FileState to its stable protocol name.
func fileStateProto(st model.FileState) string {
	switch st {
	case model.StateCommitted:
		return "committed"
	case model.StateShelf:
		return "shelf"
	case model.StateStaged:
		return "staged"
	case model.StateUntracked:
		return "untracked"
	default:
		return "unstaged"
	}
}

type bookmarkRow struct {
	ID       string `json:"id"`
	Display  string `json:"display"`
	State    string `json:"state"`
	Worktree string `json:"worktree,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Commit   string `json:"commit,omitempty"`
	ShelfID  string `json:"shelf_id,omitempty"`
	Path     string `json:"path,omitempty"`
	Label    string `json:"label,omitempty"`
	IsCommit bool   `json:"is_commit"`
	Created  string `json:"created,omitempty"`
}

func bookmarkRowFrom(b model.Bookmark) bookmarkRow {
	r := bookmarkRow{
		ID: b.ID, Display: b.Address().Display(), State: fileStateProto(b.State),
		Worktree: b.Worktree, Branch: b.Branch, Commit: b.Commit,
		ShelfID: b.ShelfID, Path: b.Path, Label: b.Label, IsCommit: b.IsCommit(),
	}
	if !b.Created.IsZero() {
		r.Created = b.Created.UTC().Format(time.RFC3339)
	}
	return r
}

type bookmarksListIn struct {
	Skip  int `json:"skip,omitempty"`
	Limit int `json:"limit,omitempty"`
}

type bookmarksListOut struct {
	Repo      RepoInfo      `json:"repo"`
	Bookmarks []bookmarkRow `json:"bookmarks"`
}

type bookmarkIDIn struct {
	ID string `json:"id"`
}

type bookmarkGetOut struct {
	Repo     RepoInfo    `json:"repo"`
	Bookmark bookmarkRow `json:"bookmark"`
}

type bookmarkReadIn struct {
	ID       string `json:"id"`
	MaxBytes int    `json:"max_bytes,omitempty"`
}

type bookmarkReadOut struct {
	Repo RepoInfo `json:"repo"`
	filePayload
}

func (s *Server) registerBookmarkTools(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "gg_bookmarks_list",
		Description: "List gg bookmarks (rich file/commit references saved by the user). Paged: skip/limit, limit defaults to 100.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in bookmarksListIn) (*sdk.CallToolResult, bookmarksListOut, error) {
		out := bookmarksListOut{Repo: s.repoInfo(), Bookmarks: []bookmarkRow{}}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 100
		}
		bs, err := s.svc.BookmarkList(ctx, in.Skip, limit)
		if err != nil {
			return nil, out, fmt.Errorf("listing bookmarks: %v", err)
		}
		for _, b := range bs {
			out.Bookmarks = append(out.Bookmarks, bookmarkRowFrom(b))
		}
		return nil, out, nil
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name:        "gg_bookmark_get",
		Description: "Full metadata of one gg bookmark by id.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in bookmarkIDIn) (*sdk.CallToolResult, bookmarkGetOut, error) {
		out := bookmarkGetOut{Repo: s.repoInfo()}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		if in.ID == "" {
			return nil, out, fmt.Errorf("id is required")
		}
		b, err := s.svc.BookmarkGet(ctx, in.ID)
		if err != nil {
			return nil, out, fmt.Errorf("bookmark not found: %s", in.ID)
		}
		out.Bookmark = bookmarkRowFrom(b)
		return nil, out, nil
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name:        "gg_bookmark_read",
		Description: "Read a bookmarked file's content (text; binary is flagged and read via gg_export instead). max_bytes caps the text, default 262144.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in bookmarkReadIn) (*sdk.CallToolResult, bookmarkReadOut, error) {
		out := bookmarkReadOut{Repo: s.repoInfo()}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		if in.ID == "" {
			return nil, out, fmt.Errorf("id is required")
		}
		b, err := s.svc.BookmarkGet(ctx, in.ID)
		if err != nil {
			return nil, out, fmt.Errorf("bookmark not found: %s", in.ID)
		}
		if b.IsCommit() {
			return nil, out, fmt.Errorf("bookmark %s is a commit pointer — nothing to read; use gg_export to copy the commit's files", in.ID)
		}
		data, err := s.svc.BookmarkBytes(ctx, b)
		if err != nil {
			return nil, out, fmt.Errorf("reading bookmark %s: %v", in.ID, err)
		}
		out.filePayload = textPayload(data, in.MaxBytes)
		return nil, out, nil
	})
}
```

Note: embedded `filePayload` in `bookmarkReadOut` flattens its json fields into the reply (`text`, `size`, …) — if the SDK's schema inference rejects the embedded struct, inline the five fields instead (same tags).

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/mcp/ -v`
Expected: PASS (all bookmark + payload + Task-4 tests).

- [ ] **Step 9: Commit**

```bash
git add internal/mcp/payload.go internal/mcp/payload_test.go internal/mcp/bookmarks.go internal/mcp/bookmarks_test.go internal/mcp/server.go
git commit -m "feat(mcp): bookmark tools — list/get/read with shared text/binary payload contract"
```

---

### Task 6: Shelf tools (`gg_shelf_buckets` / `gg_shelf_list` / `gg_shelf_commit_files` / `gg_shelf_read`) + `domain.ShelfFind`

**Files:**
- Modify: `internal/domain/shelf.go` (add `ShelfFind`)
- Create: `internal/mcp/shelf.go`, `internal/mcp/shelf_test.go`
- Modify: `internal/mcp/server.go` (delete the `registerShelfTools` stub)

**Interfaces:**
- Consumes: `svc.ShelfBuckets`, `svc.ShelfList(ctx, bucket, skip, limit)`, `svc.ShelfBlob(ctx, id)`, `svc.ShelfCommitFiles(ctx, id)`, `svc.ShelfAdd(ctx, addr model.FileAddress, bucket string)`, `svc.ShelfAddCommit(ctx, sha, label)`, `svc.ResolveBytes(ctx, model.FileRef{Source: model.SourceShelf, Locator: id, Path: member})`; `model.ShelfEntry` (`ID, Bucket, Kind, Origin, Label, Size, PatchSHA, Created`, `IsCommit()`); `textPayload` (Task 5).
- Produces: `func (s *Service) ShelfFind(ctx context.Context, entryID string) (model.ShelfEntry, error)` in domain (Task 8's export uses it too); `shelfRow` reply shape.

- [ ] **Step 1: Add `domain.ShelfFind` (with the store-nil guard every shelf method uses)**

Append to `internal/domain/shelf.go`:

```go
// ShelfFind returns an entry's metadata by id (a local index read; no
// reservation). The entry-kind discriminator for callers that must branch on
// file-vs-commit without fetching the blob (the MCP read/export tools).
func (s *Service) ShelfFind(ctx context.Context, entryID string) (model.ShelfEntry, error) {
	st := s.shelfStore(ctx)
	if st == nil {
		return model.ShelfEntry{}, ErrShelfDisabled
	}
	return st.Find(entryID)
}
```

If the `shelf.Store` interface method is named differently than `Find(id)` (check `internal/shelf/store.go` — CLAUDE.md documents it as "Store.Find, Get's metadata sibling"), match it.

Run: `go build ./internal/domain/`
Expected: builds.

- [ ] **Step 2: Write the failing tests**

`internal/mcp/shelf_test.go`:

```go
package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// seedShelfFile shelves a.txt's unstaged working copy into the default bucket.
func seedShelfFile(t *testing.T, e *testEnv) model.ShelfEntry {
	t.Helper()
	entry, err := e.svc.ShelfAdd(context.Background(), model.FileAddress{
		State: model.StateUnstaged, Worktree: e.dir, Branch: "main", Path: "a.txt",
	}, "default")
	if err != nil {
		t.Fatalf("ShelfAdd: %v", err)
	}
	return entry
}

// seedShelfCommit shelves the seed commit as a commit entry.
func seedShelfCommit(t *testing.T, e *testEnv) model.ShelfEntry {
	t.Helper()
	entry, err := e.svc.ShelfAddCommit(context.Background(), e.sha, "seed label")
	if err != nil {
		t.Fatalf("ShelfAddCommit: %v", err)
	}
	return entry
}

func TestShelfBucketsAndList(t *testing.T) {
	e := newTestEnv(t)
	fe := seedShelfFile(t, e)
	ce := seedShelfCommit(t, e)

	buckets := e.call(t, "gg_shelf_buckets", nil)
	bs := buckets["buckets"].([]any)
	if len(bs) == 0 {
		t.Fatalf("buckets = %v", buckets)
	}

	out := e.call(t, "gg_shelf_list", nil) // default bucket
	rows := out["entries"].([]any)
	if len(rows) != 2 {
		t.Fatalf("entries = %v", rows)
	}
	kinds := map[string]string{}
	for _, r := range rows {
		row := r.(map[string]any)
		kinds[row["id"].(string)] = row["kind"].(string)
	}
	if kinds[fe.ID] != "file" || kinds[ce.ID] != "commit" {
		t.Fatalf("kinds = %v", kinds)
	}
}

func TestShelfCommitFiles(t *testing.T) {
	e := newTestEnv(t)
	ce := seedShelfCommit(t, e)
	out := e.call(t, "gg_shelf_commit_files", map[string]any{"id": ce.ID})
	files := out["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	f := files[0].(map[string]any)
	if f["path"] != "a.txt" {
		t.Fatalf("file = %v", f)
	}
}

func TestShelfCommitFilesOnFileEntry(t *testing.T) {
	e := newTestEnv(t)
	fe := seedShelfFile(t, e)
	msg := e.callErr(t, "gg_shelf_commit_files", map[string]any{"id": fe.ID})
	if !strings.Contains(msg, "file entry") {
		t.Fatalf("error = %s", msg)
	}
}

func TestShelfReadFileEntry(t *testing.T) {
	e := newTestEnv(t)
	fe := seedShelfFile(t, e)
	out := e.call(t, "gg_shelf_read", map[string]any{"id": fe.ID})
	if out["text"] != "hello\nworld\n" {
		t.Fatalf("text = %q", out["text"])
	}
}

func TestShelfReadCommitMember(t *testing.T) {
	e := newTestEnv(t)
	ce := seedShelfCommit(t, e)
	out := e.call(t, "gg_shelf_read", map[string]any{"id": ce.ID, "member": "a.txt"})
	if out["text"] != "hello\nworld\n" {
		t.Fatalf("member text = %q", out["text"])
	}
}

func TestShelfReadCommitWithoutMemberRefused(t *testing.T) {
	e := newTestEnv(t)
	ce := seedShelfCommit(t, e)
	msg := e.callErr(t, "gg_shelf_read", map[string]any{"id": ce.ID})
	if !strings.Contains(msg, "gg_shelf_commit_files") {
		t.Fatalf("refusal must hint the member list: %s", msg)
	}
}

func TestShelfReadUnknownID(t *testing.T) {
	e := newTestEnv(t)
	msg := e.callErr(t, "gg_shelf_read", map[string]any{"id": "nope"})
	if !strings.Contains(msg, "not found") {
		t.Fatalf("error = %s", msg)
	}
}
```

Note: `seedShelfFile`'s `FileAddress` field set mirrors how the TUI shelves a working file; if `ShelfAdd` needs a different address shape (check its doc comment in `internal/domain/shelf.go`), adjust the seed helper — the tool code under test does not change.

- [ ] **Step 3: Run to verify failure**

Run: `go test ./internal/mcp/ -run TestShelf -v`
Expected: FAIL — tools not registered.

- [ ] **Step 4: Implement the shelf tools**

Delete the `registerShelfTools` stub from `server.go`, create `internal/mcp/shelf.go`:

```go
package mcp

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/model"
)

type shelfBucketRow struct {
	Name   string `json:"name"`
	Hidden bool   `json:"hidden,omitempty"`
}

type shelfBucketsOut struct {
	Repo    RepoInfo         `json:"repo"`
	Buckets []shelfBucketRow `json:"buckets"`
}

type shelfListIn struct {
	Bucket string `json:"bucket,omitempty"`
	Skip   int    `json:"skip,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type shelfRow struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"` // file|commit
	OriginDisplay string `json:"origin_display"`
	Label         string `json:"label,omitempty"`
	Path          string `json:"path,omitempty"`
	Commit        string `json:"commit,omitempty"`
	Size          int64  `json:"size"`
	HasPatch      bool   `json:"has_patch"`
	CreatedAt     string `json:"created_at,omitempty"`
}

func shelfRowFrom(e model.ShelfEntry) shelfRow {
	kind := "file"
	if e.IsCommit() {
		kind = "commit"
	}
	r := shelfRow{
		ID: e.ID, Kind: kind, OriginDisplay: e.Origin.Display(), Label: e.Label,
		Path: e.Origin.Path, Commit: e.Origin.Commit, Size: e.Size,
		HasPatch: e.PatchSHA != "",
	}
	if !e.Created.IsZero() {
		r.CreatedAt = e.Created.UTC().Format(time.RFC3339)
	}
	return r
}

type shelfListOut struct {
	Repo    RepoInfo   `json:"repo"`
	Entries []shelfRow `json:"entries"`
}

type shelfIDIn struct {
	ID string `json:"id"`
}

type shelfCommitFilesOut struct {
	Repo  RepoInfo         `json:"repo"`
	Files []commitFileRow  `json:"files"`
}

type shelfReadIn struct {
	ID       string `json:"id"`
	Member   string `json:"member,omitempty"`
	MaxBytes int    `json:"max_bytes,omitempty"`
}

type shelfReadOut struct {
	Repo RepoInfo `json:"repo"`
	filePayload
}

func (s *Server) registerShelfTools(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "gg_shelf_buckets",
		Description: "List gg shelf buckets (named groups of shelved files/commits).",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, shelfBucketsOut, error) {
		out := shelfBucketsOut{Repo: s.repoInfo(), Buckets: []shelfBucketRow{}}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		bs, err := s.svc.ShelfBuckets(ctx)
		if err != nil {
			return nil, out, fmt.Errorf("listing shelf buckets: %v", err)
		}
		for _, b := range bs {
			out.Buckets = append(out.Buckets, shelfBucketRow{Name: b.Name, Hidden: b.Hidden})
		}
		return nil, out, nil
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name:        "gg_shelf_list",
		Description: "List shelf entries in a bucket (default \"default\"). kind is \"file\" (one shelved file) or \"commit\" (a frozen commit snapshot). Paged: skip/limit, limit defaults to 100.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in shelfListIn) (*sdk.CallToolResult, shelfListOut, error) {
		out := shelfListOut{Repo: s.repoInfo(), Entries: []shelfRow{}}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		bucket := in.Bucket
		if bucket == "" {
			bucket = "default"
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 100
		}
		es, err := s.svc.ShelfList(ctx, bucket, in.Skip, limit)
		if err != nil {
			return nil, out, fmt.Errorf("listing shelf bucket %q: %v", bucket, err)
		}
		for _, e := range es {
			out.Entries = append(out.Entries, shelfRowFrom(e))
		}
		return nil, out, nil
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name:        "gg_shelf_commit_files",
		Description: "List the member files of a shelved COMMIT entry (path, status letter, old_path for renames).",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in shelfIDIn) (*sdk.CallToolResult, shelfCommitFilesOut, error) {
		out := shelfCommitFilesOut{Repo: s.repoInfo(), Files: []commitFileRow{}}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		if in.ID == "" {
			return nil, out, fmt.Errorf("id is required")
		}
		entry, err := s.svc.ShelfFind(ctx, in.ID)
		if err != nil {
			return nil, out, fmt.Errorf("shelf entry not found: %s", in.ID)
		}
		if !entry.IsCommit() {
			return nil, out, fmt.Errorf("shelf entry %s is a file entry — use gg_shelf_read without member", in.ID)
		}
		files, err := s.svc.ShelfCommitFiles(ctx, in.ID)
		if err != nil {
			return nil, out, fmt.Errorf("listing members of %s: %v", in.ID, err)
		}
		for _, f := range files {
			out.Files = append(out.Files, commitFileRowFrom(f))
		}
		return nil, out, nil
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name:        "gg_shelf_read",
		Description: "Read a shelf entry's content: a file entry's bytes, or ONE member of a commit entry (member = repo-relative path from gg_shelf_commit_files). Text only; binary is flagged. max_bytes caps the text, default 262144.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in shelfReadIn) (*sdk.CallToolResult, shelfReadOut, error) {
		out := shelfReadOut{Repo: s.repoInfo()}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		if in.ID == "" {
			return nil, out, fmt.Errorf("id is required")
		}
		entry, err := s.svc.ShelfFind(ctx, in.ID)
		if err != nil {
			return nil, out, fmt.Errorf("shelf entry not found: %s", in.ID)
		}
		var data []byte
		switch {
		case entry.IsCommit() && in.Member == "":
			return nil, out, fmt.Errorf("shelf entry %s is a commit — pass member (list members with gg_shelf_commit_files)", in.ID)
		case !entry.IsCommit() && in.Member != "":
			return nil, out, fmt.Errorf("shelf entry %s is a file entry — omit member", in.ID)
		case entry.IsCommit():
			data, err = s.svc.ResolveBytes(ctx, model.FileRef{Source: model.SourceShelf, Locator: in.ID, Path: in.Member})
			if err != nil {
				return nil, out, fmt.Errorf("reading member %q of %s: %v", in.Member, in.ID, err)
			}
		default:
			data, err = s.svc.ShelfBlob(ctx, in.ID)
			if err != nil {
				return nil, out, fmt.Errorf("reading shelf entry %s: %v", in.ID, err)
			}
		}
		out.filePayload = textPayload(data, in.MaxBytes)
		return nil, out, nil
	})
}
```

`commitFileRow`/`commitFileRowFrom` are defined in Task 7's `compare.go` — since this task lands first, define them HERE (in `shelf.go`) and Task 7 reuses them:

```go
// commitFileRow is the shared changed-file reply shape (shelf commit members,
// compare results): status letter A/M/D/R/C/T plus the pre-rename path.
type commitFileRow struct {
	Path    string `json:"path"`
	Status  string `json:"status"`
	OldPath string `json:"old_path,omitempty"`
}

func commitFileRowFrom(f model.CommitFile) commitFileRow {
	return commitFileRow{Path: f.Path, Status: f.Status, OldPath: f.OldPath}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/mcp/ -v && go test ./internal/domain/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/domain/shelf.go internal/mcp/shelf.go internal/mcp/shelf_test.go internal/mcp/server.go
git commit -m "feat(mcp): shelf tools — buckets/list/commit-files/read + domain.ShelfFind"
```

---

### Task 7: Compare tools (`gg_compare_trees` / `gg_compare_file`)

**Files:**
- Create: `internal/mcp/compare.go`, `internal/mcp/compare_test.go`
- Modify: `internal/mcp/server.go` (delete the `registerCompareTools` stub)

**Interfaces:**
- Consumes: `svc.CommitLookup(ctx, rev) (model.LogLine, bool, error)` (short sha + subject), `svc.CompareFiles(ctx, left, right model.Endpoint)`, `svc.ResolveBytes(ctx, model.FileRef)`, `svc.BookmarkGet`/`svc.BookmarkBytes`, `svc.DiffNoIndex(ctx, a, b)` (Task 1); `model.Endpoint{Kind, Hash}`, `model.FileRef{Source, Locator, Path}` with `model.SourceUnstaged/SourceStaged/SourceCommit/SourceShelf`; `commitFileRow` (Task 6); `textPayload`'s binary rule (NUL/invalid-UTF-8 — reimplemented locally as `isBinary`).
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Write the failing tests**

`internal/mcp/compare_test.go`:

```go
package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareTreesWorktreeVsCommit(t *testing.T) {
	e := newTestEnv(t)
	// change a.txt in the working tree
	if err := os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("hello\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := e.call(t, "gg_compare_trees", map[string]any{
		"left":  map[string]any{"kind": "commit", "rev": "HEAD"},
		"right": map[string]any{"kind": "worktree"},
	})
	files := out["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	f := files[0].(map[string]any)
	if f["path"] != "a.txt" || f["status"] != "M" {
		t.Fatalf("file = %v", f)
	}
	if out["left_display"] == "" || out["right_display"] != "worktree" {
		t.Fatalf("displays = %v / %v", out["left_display"], out["right_display"])
	}
}

func TestCompareTreesBadRev(t *testing.T) {
	e := newTestEnv(t)
	msg := e.callErr(t, "gg_compare_trees", map[string]any{
		"left":  map[string]any{"kind": "commit", "rev": "no-such-rev"},
		"right": map[string]any{"kind": "worktree"},
	})
	if !strings.Contains(msg, "unknown revision") {
		t.Fatalf("error = %s", msg)
	}
}

func TestCompareTreesBadKind(t *testing.T) {
	e := newTestEnv(t)
	msg := e.callErr(t, "gg_compare_trees", map[string]any{
		"left":  map[string]any{"kind": "banana"},
		"right": map[string]any{"kind": "worktree"},
	})
	if !strings.Contains(msg, `"worktree", "index", or "commit"`) {
		t.Fatalf("error = %s", msg)
	}
}

func TestCompareFileCommitVsWorktree(t *testing.T) {
	e := newTestEnv(t)
	if err := os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("hello\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := e.call(t, "gg_compare_file", map[string]any{
		"left":  map[string]any{"source": "commit", "locator": "HEAD", "path": "a.txt"},
		"right": map[string]any{"source": "unstaged", "path": "a.txt"},
	})
	if out["identical"] == true {
		t.Fatal("files differ")
	}
	diff := out["unified_diff"].(string)
	if !strings.Contains(diff, "-world") || !strings.Contains(diff, "+changed") {
		t.Fatalf("diff = %s", diff)
	}
	if strings.Contains(diff, "ui-state") || strings.Contains(diff, os.TempDir()) {
		t.Fatalf("diff leaks temp paths: %s", diff)
	}
	if !strings.Contains(diff, "--- "+out["left_display"].(string)) {
		t.Fatalf("diff header must carry the display label: %s", diff)
	}
}

func TestCompareFileIdentical(t *testing.T) {
	e := newTestEnv(t)
	out := e.call(t, "gg_compare_file", map[string]any{
		"left":  map[string]any{"source": "commit", "locator": "HEAD", "path": "a.txt"},
		"right": map[string]any{"source": "unstaged", "path": "a.txt"},
	})
	if out["identical"] != true {
		t.Fatalf("expected identical, got %v", out)
	}
}

func TestCompareFileBookmarkSide(t *testing.T) {
	e := newTestEnv(t)
	b := seedBookmark(t, e)
	out := e.call(t, "gg_compare_file", map[string]any{
		"left":  map[string]any{"source": "bookmark", "id": b.ID},
		"right": map[string]any{"source": "unstaged", "path": "a.txt"},
	})
	if out["identical"] != true {
		t.Fatalf("bookmark == worktree here, got %v", out)
	}
}

func TestCompareFileBinary(t *testing.T) {
	e := newTestEnv(t)
	if err := os.WriteFile(filepath.Join(e.dir, "bin.dat"), []byte{0x00, 0x01}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dir, "bin2.dat"), []byte{0x00, 0x02}, 0o644); err != nil {
		t.Fatal(err)
	}
	out := e.call(t, "gg_compare_file", map[string]any{
		"left":  map[string]any{"source": "unstaged", "path": "bin.dat"},
		"right": map[string]any{"source": "unstaged", "path": "bin2.dat"},
	})
	if out["binary"] != true {
		t.Fatalf("expected binary flag: %v", out)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/mcp/ -run TestCompare -v`
Expected: FAIL — tools not registered.

- [ ] **Step 3: Implement**

Delete the `registerCompareTools` stub from `server.go`, create `internal/mcp/compare.go`:

```go
package mcp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/model"
)

type treeSideIn struct {
	Kind string `json:"kind"`          // worktree|index|commit
	Rev  string `json:"rev,omitempty"` // required for kind=commit; any rev-parseable name
}

type compareTreesIn struct {
	Left  treeSideIn `json:"left"`
	Right treeSideIn `json:"right"`
}

type compareTreesOut struct {
	Repo         RepoInfo        `json:"repo"`
	LeftDisplay  string          `json:"left_display"`
	RightDisplay string          `json:"right_display"`
	Files        []commitFileRow `json:"files"`
}

// endpointFor resolves one compare side. A commit rev resolves to its sha
// BEFORE the compare (the resolve-to-tip-hash rule: never key a diff on a
// mutable name).
func (s *Server) endpointFor(ctx context.Context, side treeSideIn) (model.Endpoint, string, error) {
	switch side.Kind {
	case "worktree":
		return model.Endpoint{Kind: model.EndpointWorkTree}, "worktree", nil
	case "index":
		return model.Endpoint{Kind: model.EndpointIndex}, "index", nil
	case "commit":
		if side.Rev == "" {
			return model.Endpoint{}, "", fmt.Errorf("rev is required for kind \"commit\"")
		}
		line, ok, err := s.svc.CommitLookup(ctx, side.Rev)
		if err != nil {
			return model.Endpoint{}, "", fmt.Errorf("resolving %q: %v", side.Rev, err)
		}
		if !ok {
			return model.Endpoint{}, "", fmt.Errorf("unknown revision: %s", side.Rev)
		}
		return model.Endpoint{Kind: model.EndpointCommit, Hash: line.Hash}, line.Hash + " " + line.Subject, nil
	default:
		return model.Endpoint{}, "", fmt.Errorf(`kind must be "worktree", "index", or "commit" (got %q)`, side.Kind)
	}
}

type fileSideIn struct {
	Source  string `json:"source"`            // unstaged|staged|commit|shelf|bookmark
	Locator string `json:"locator,omitempty"` // rev (commit) or shelf entry id
	ID      string `json:"id,omitempty"`      // bookmark id (source=bookmark)
	Path    string `json:"path,omitempty"`    // repo-relative; member path for shelf commits
}

type compareFileIn struct {
	Left  fileSideIn `json:"left"`
	Right fileSideIn `json:"right"`
}

type compareFileOut struct {
	Repo         RepoInfo `json:"repo"`
	LeftDisplay  string   `json:"left_display"`
	RightDisplay string   `json:"right_display"`
	Identical    bool     `json:"identical"`
	Binary       bool     `json:"binary,omitempty"`
	LeftSize     int      `json:"left_size,omitempty"`
	RightSize    int      `json:"right_size,omitempty"`
	UnifiedDiff  string   `json:"unified_diff,omitempty"`
}

// resolveFileSide fetches one side's bytes + display label.
func (s *Server) resolveFileSide(ctx context.Context, side fileSideIn) ([]byte, string, error) {
	if side.Source == "bookmark" {
		if side.ID == "" {
			return nil, "", fmt.Errorf("id is required for source \"bookmark\"")
		}
		b, err := s.svc.BookmarkGet(ctx, side.ID)
		if err != nil {
			return nil, "", fmt.Errorf("bookmark not found: %s", side.ID)
		}
		if b.IsCommit() {
			return nil, "", fmt.Errorf("bookmark %s is a commit pointer — compare a file, or use gg_compare_trees against its commit", side.ID)
		}
		data, err := s.svc.BookmarkBytes(ctx, b)
		if err != nil {
			return nil, "", fmt.Errorf("reading bookmark %s: %v", side.ID, err)
		}
		return data, b.Address().Display(), nil
	}
	if side.Path == "" {
		return nil, "", fmt.Errorf("path is required for source %q", side.Source)
	}
	ref := model.FileRef{Locator: side.Locator, Path: side.Path}
	display := side.Source + ":" + side.Path
	switch side.Source {
	case "unstaged":
		ref.Source = model.SourceUnstaged
	case "staged":
		ref.Source = model.SourceStaged
	case "commit":
		if side.Locator == "" {
			return nil, "", fmt.Errorf("locator (a revision) is required for source \"commit\"")
		}
		ref.Source = model.SourceCommit
		display = side.Locator + ":" + side.Path
	case "shelf":
		if side.Locator == "" {
			return nil, "", fmt.Errorf("locator (a shelf entry id) is required for source \"shelf\"")
		}
		ref.Source = model.SourceShelf
		display = "shelf:" + side.Locator + ":" + side.Path
	default:
		return nil, "", fmt.Errorf(`source must be "unstaged", "staged", "commit", "shelf", or "bookmark" (got %q)`, side.Source)
	}
	data, err := s.svc.ResolveBytes(ctx, ref)
	if err != nil {
		return nil, "", fmt.Errorf("reading %s: %v", display, err)
	}
	return data, display, nil
}

func isBinary(data []byte) bool {
	return bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data)
}

// relabelDiff strips the temp-path noise from git diff --no-index output:
// drops the "diff --git"/"index" header lines and rewrites ---/+++ to the
// human display labels.
func relabelDiff(diff, leftDisplay, rightDisplay string) string {
	lines := strings.Split(diff, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		switch {
		case strings.HasPrefix(ln, "diff --git "), strings.HasPrefix(ln, "index "):
			continue
		case strings.HasPrefix(ln, "--- "):
			out = append(out, "--- "+leftDisplay)
		case strings.HasPrefix(ln, "+++ "):
			out = append(out, "+++ "+rightDisplay)
		default:
			out = append(out, ln)
		}
	}
	return strings.Join(out, "\n")
}

func (s *Server) registerCompareTools(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "gg_compare_trees",
		Description: "Whole-tree compare between two endpoints (worktree, index, or a commit by rev): the changed-file list with status letters. Use gg_compare_file for one file's diff.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in compareTreesIn) (*sdk.CallToolResult, compareTreesOut, error) {
		out := compareTreesOut{Repo: s.repoInfo(), Files: []commitFileRow{}}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		left, ld, err := s.endpointFor(ctx, in.Left)
		if err != nil {
			return nil, out, fmt.Errorf("left: %v", err)
		}
		right, rd, err := s.endpointFor(ctx, in.Right)
		if err != nil {
			return nil, out, fmt.Errorf("right: %v", err)
		}
		out.LeftDisplay, out.RightDisplay = ld, rd
		files, err := s.svc.CompareFiles(ctx, left, right)
		if err != nil {
			return nil, out, fmt.Errorf("comparing: %v", err)
		}
		for _, f := range files {
			out.Files = append(out.Files, commitFileRowFrom(f))
		}
		return nil, out, nil
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name:        "gg_compare_file",
		Description: "Unified diff between two file versions. Each side: {source: unstaged|staged|commit|shelf, locator, path} or {source: bookmark, id}. locator = revision for commit, shelf entry id for shelf (path = member for shelved commits).",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in compareFileIn) (*sdk.CallToolResult, compareFileOut, error) {
		out := compareFileOut{Repo: s.repoInfo()}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		leftData, ld, err := s.resolveFileSide(ctx, in.Left)
		if err != nil {
			return nil, out, fmt.Errorf("left: %v", err)
		}
		rightData, rd, err := s.resolveFileSide(ctx, in.Right)
		if err != nil {
			return nil, out, fmt.Errorf("right: %v", err)
		}
		out.LeftDisplay, out.RightDisplay = ld, rd
		if bytes.Equal(leftData, rightData) {
			out.Identical = true
			return nil, out, nil
		}
		if isBinary(leftData) || isBinary(rightData) {
			out.Binary = true
			out.LeftSize, out.RightSize = len(leftData), len(rightData)
			return nil, out, nil
		}
		dir, err := os.MkdirTemp("", "gg-mcp-diff-*")
		if err != nil {
			return nil, out, fmt.Errorf("temp dir: %v", err)
		}
		defer os.RemoveAll(dir)
		a, b := filepath.Join(dir, "left"), filepath.Join(dir, "right")
		if err := os.WriteFile(a, leftData, 0o600); err != nil {
			return nil, out, fmt.Errorf("temp file: %v", err)
		}
		if err := os.WriteFile(b, rightData, 0o600); err != nil {
			return nil, out, fmt.Errorf("temp file: %v", err)
		}
		diff, err := s.svc.DiffNoIndex(ctx, a, b)
		if err != nil {
			return nil, out, fmt.Errorf("diffing: %v", err)
		}
		out.UnifiedDiff = relabelDiff(diff, ld, rd)
		return nil, out, nil
	})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mcp/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/compare.go internal/mcp/compare_test.go internal/mcp/server.go
git commit -m "feat(mcp): compare tools — tree-level changed files + per-file unified diff"
```

---

### Task 8: `gg_export` + static decider

**Files:**
- Create: `internal/mcp/decider.go`, `internal/mcp/export.go`
- Create: `internal/mcp/export_test.go`
- Modify: `internal/mcp/server.go` (delete the `registerExportTool` stub)

**Interfaces:**
- Consumes: `svc.Execute(ctx, op, events chan<- engine.Event, dec engine.Decider)`, `engine.ExportToDir{Dir, Files}`, `engine.ErrExportCancelled`, decision id `"overwrite"` with options `"overwrite"`/`"cancel"` (see `internal/engine/writefile.go:22`), `svc.ExportBookmark(ctx, b) ([]model.ExportFile, string, error)`, `svc.ExportShelfEntry(ctx, e) ([]model.ExportFile, string, error)`, `svc.TempExportBase(ctx)`, `svc.BookmarkGet`, `svc.ShelfFind` (Task 6).
- Produces: `staticDecider{policy map[string]string}` and `runOp(ctx, svc, op, dec) (engine.Result, error)` — stage 2 will reuse both.

- [ ] **Step 1: Write the failing tests**

`internal/mcp/export_test.go`:

```go
package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportShelfCommitToDir(t *testing.T) {
	e := newTestEnv(t)
	ce := seedShelfCommit(t, e)
	dest := filepath.Join(t.TempDir(), "out")
	out := e.call(t, "gg_export", map[string]any{
		"shelf": ce.ID,
		"dir":   dest,
	})
	if out["dir"] != dest {
		t.Fatalf("dir = %v", out["dir"])
	}
	if int(out["count"].(float64)) != 1 {
		t.Fatalf("count = %v", out["count"])
	}
	data, err := os.ReadFile(filepath.Join(dest, "a.txt"))
	if err != nil || string(data) != "hello\nworld\n" {
		t.Fatalf("exported file: %q err=%v", data, err)
	}
}

func TestExportBookmarkDefaultDir(t *testing.T) {
	e := newTestEnv(t)
	b := seedBookmark(t, e)
	out := e.call(t, "gg_export", map[string]any{"bookmark": b.ID})
	dir, _ := out["dir"].(string)
	if dir == "" {
		t.Fatalf("no default dir: %v", out)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	files := out["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	if _, err := os.Stat(filepath.Join(dir, files[0].(string))); err != nil {
		t.Fatalf("exported file missing: %v", err)
	}
}

func TestExportExistingDirRefusedWithoutOverwrite(t *testing.T) {
	e := newTestEnv(t)
	ce := seedShelfCommit(t, e)
	dest := filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	msg := e.callErr(t, "gg_export", map[string]any{"shelf": ce.ID, "dir": dest})
	if !strings.Contains(msg, "overwrite:true") {
		t.Fatalf("refusal must name the fix: %s", msg)
	}
	// and with overwrite it succeeds
	out := e.call(t, "gg_export", map[string]any{"shelf": ce.ID, "dir": dest, "overwrite": true})
	if int(out["count"].(float64)) != 1 {
		t.Fatalf("overwrite export failed: %v", out)
	}
}

func TestExportNeedsExactlyOneSource(t *testing.T) {
	e := newTestEnv(t)
	msg := e.callErr(t, "gg_export", map[string]any{})
	if !strings.Contains(msg, "exactly one") {
		t.Fatalf("error = %s", msg)
	}
	msg = e.callErr(t, "gg_export", map[string]any{"bookmark": "x", "shelf": "y"})
	if !strings.Contains(msg, "exactly one") {
		t.Fatalf("error = %s", msg)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/mcp/ -run TestExport -v`
Expected: FAIL — tool not registered.

- [ ] **Step 3: Implement the decider + runner**

`internal/mcp/decider.go`:

```go
package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// staticDecider answers engine forks from a fixed policy (the CLI flag-policy
// pattern). Stage 1's only reachable decision is ExportToDir's "overwrite";
// anything else fails loud — an MCP tool must never wedge on a question.
type staticDecider struct {
	policy map[string]string
}

func (d staticDecider) Decide(_ context.Context, req engine.DecisionRequest) (engine.DecisionResponse, error) {
	if opt, ok := d.policy[req.ID]; ok {
		return engine.DecisionResponse{Option: opt}, nil
	}
	return engine.DecisionResponse{}, fmt.Errorf(
		"unexpected decision %q (options: %s)", req.ID, strings.Join(req.Options, ", "))
}

// runOp executes op via domain.Execute, draining events (the MCP reply
// carries the outcome; per-line progress has no channel here).
func runOp(ctx context.Context, svc *domain.Service, op engine.Operation, dec engine.Decider) (engine.Result, error) {
	events := make(chan engine.Event, 32)
	done := make(chan struct{})
	var (
		res engine.Result
		err error
	)
	go func() {
		res, err = svc.Execute(ctx, op, events, dec)
		close(events)
		close(done)
	}()
	for range events {
	}
	<-done
	return res, err
}
```

`internal/mcp/export.go`:

```go
package mcp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

type exportIn struct {
	Bookmark  string `json:"bookmark,omitempty"` // bookmark id
	Shelf     string `json:"shelf,omitempty"`    // shelf entry id
	Dir       string `json:"dir,omitempty"`      // absolute target; default = gg's temp-export area
	Overwrite bool   `json:"overwrite,omitempty"`
}

type exportOut struct {
	Repo  RepoInfo `json:"repo"`
	Dir   string   `json:"dir"`
	Files []string `json:"files"`
	Count int      `json:"count"`
}

func (s *Server) registerExportTool(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "gg_export",
		Description: "Copy a bookmark or shelf entry (a file, or a whole shelved/bookmarked commit's files) into a local directory. Exactly one of bookmark/shelf. dir defaults to gg's temp-export area; an existing dir is refused unless overwrite:true. Never touches the repository.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in exportIn) (*sdk.CallToolResult, exportOut, error) {
		out := exportOut{Repo: s.repoInfo(), Files: []string{}}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		if (in.Bookmark == "") == (in.Shelf == "") {
			return nil, out, fmt.Errorf("pass exactly one of bookmark (id) or shelf (entry id)")
		}
		var (
			files  []model.ExportFile
			subdir string
		)
		if in.Bookmark != "" {
			b, err := s.svc.BookmarkGet(ctx, in.Bookmark)
			if err != nil {
				return nil, out, fmt.Errorf("bookmark not found: %s", in.Bookmark)
			}
			files, subdir, err = s.svc.ExportBookmark(ctx, b)
			if err != nil {
				return nil, out, fmt.Errorf("collecting bookmark %s: %v", in.Bookmark, err)
			}
		} else {
			entry, err := s.svc.ShelfFind(ctx, in.Shelf)
			if err != nil {
				return nil, out, fmt.Errorf("shelf entry not found: %s", in.Shelf)
			}
			files, subdir, err = s.svc.ExportShelfEntry(ctx, entry)
			if err != nil {
				return nil, out, fmt.Errorf("collecting shelf entry %s: %v", in.Shelf, err)
			}
		}
		dir := in.Dir
		if dir == "" {
			base, err := s.svc.TempExportBase(ctx)
			if err != nil {
				return nil, out, fmt.Errorf("resolving the default export dir: %v — pass dir explicitly", err)
			}
			dir = filepath.Join(base, subdir)
		}
		policy := map[string]string{"overwrite": "cancel"}
		if in.Overwrite {
			policy["overwrite"] = "overwrite"
		}
		if _, err := runOp(ctx, s.svc, engine.ExportToDir{Dir: dir, Files: files}, staticDecider{policy: policy}); err != nil {
			if errors.Is(err, engine.ErrExportCancelled) {
				return nil, out, fmt.Errorf("directory exists: %s — pass overwrite:true to replace its contents", dir)
			}
			return nil, out, fmt.Errorf("export failed: %v", err)
		}
		out.Dir = dir
		for _, f := range files {
			out.Files = append(out.Files, f.RelPath)
		}
		out.Count = len(files)
		return nil, out, nil
	})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mcp/ -v`
Expected: PASS (whole package).

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/decider.go internal/mcp/export.go internal/mcp/export_test.go internal/mcp/server.go
git commit -m "feat(mcp): gg_export — copy bookmarks/shelf entries to a directory (static decider)"
```

---

### Task 9: Docs + full verification

**Files:**
- Modify: `CHANGELOG.md`, `CLAUDE.md`, `README.md`

**Interfaces:** none (prose). `internal/agentskill/using-gg.md` is deliberately **unchanged** (spec: the skill teaches CLI verbs; agents don't launch MCP servers).

- [ ] **Step 1: CHANGELOG**

Add under a new `### Added` block at the top of `CHANGELOG.md` (matching the existing entry style):

```markdown
- `gg mcp` — an MCP (Model Context Protocol) stdio server exposing gg's
  non-git value to AI agents: `gg_ui_state` (the live TUI session snapshot —
  focus, cursor selections, ◉-marked commits, marked files, open diff/compare
  view, open bookmark/shelf switcher, filters, conflict/running-op state),
  bookmark tools (`gg_bookmarks_list`/`gg_bookmark_get`/`gg_bookmark_read`),
  shelf tools (`gg_shelf_buckets`/`gg_shelf_list`/`gg_shelf_commit_files`/
  `gg_shelf_read`), compare tools (`gg_compare_trees`/`gg_compare_file`), and
  `gg_export` (copy a bookmark/shelf entry into a local directory). Stage 1 is
  the safe surface only — no repo mutation. Register with
  `claude mcp add gg -- gg mcp` from the repo directory.
- The TUI now publishes a per-repo session snapshot
  (`<state>/gg/sessions/<repo-key>/ui-state.json`, atomic write-on-change from
  the 1s heartbeat, removed on exit/repo-switch) that backs `gg_ui_state`.
```

- [ ] **Step 2: CLAUDE.md**

(a) Package map: add a row after `cli`:

```markdown
| `mcp`        | MCP (Model Context Protocol) frontend: `gg mcp` serves stdio via the official Go SDK. Stage 1 = the SAFE surface only (no repo mutation): `gg_ui_state` reads the TUI session snapshot (`config.SessionSnapshotPath`, written by `internal/tui/session_snapshot.go`); bookmark/shelf list-get-read tools; `gg_compare_trees` (`CompareFiles`) / `gg_compare_file` (ResolveBytes → temp files → the `DiffNoIndex` verb, headers relabelled); `gg_export` (`ExportBookmark`/`ExportShelfEntry` → `engine.ExportToDir`, the `overwrite` decision answered from the tool's `overwrite` param via a fail-loud `staticDecider`). Domain-only frontend like `cli` (archtest: never imports `internal/git`/`tui`/`cli`); every reply carries `repo{common_dir, worktree}`; text reads share the `textPayload` text/binary/truncation contract (max_bytes default 262144). |
```

(b) In the `tui` row append a sentence:

```markdown
**Session snapshot (MCP stage 1)** (`session_snapshot.go`): the perpetual 1s heartbeat serializes an agent-facing `sessionSnapshot` (schema v1: repo/focus/cursor/marks/files-view/switcher/filter/conflict/running-op; English protocol values, `status` display-only) and atomically writes `config.SessionSnapshotPath(commonDir)` only when the timestamp-less payload changed; removed on clean exit and reRoot (the file doubles as session presence); `startOp` now records `opName` (`engine.OpName`) for the `running_op` field.
```

(c) In the `git` row append: ``DiffNoIndex(ctx, a, b)` (`git diff --no-index -- a b`; exit 1 = "differs", mapped to success — the ConfigUnset exit-5 pattern) backs the MCP per-file compare.``

(d) In the `config` row append: ``SessionSnapshotPath(commonDir)` — the per-repo TUI session-snapshot location under the state root (`repos.DefaultStatePath` roots), shared by the TUI writer and `gg mcp` reader.``

(e) Update the Status/roadmap line: M3's MCP scope is now "expose gg's non-git value (UI state + bookmarks/shelves + compare/export; stage 2: cherry-pick/restore/apply)" instead of the old heavy-git-ops list.

- [ ] **Step 3: README**

Add a section (near the CLI/agent material):

````markdown
## MCP server (`gg mcp`)

`gg mcp` serves gg's non-git value to AI agents over the Model Context
Protocol (stdio). It deliberately does NOT expose normal git operations —
agents already have the `gg` CLI for those — but the things only gg knows:

- **`gg_ui_state`** — what the gg TUI is showing right now: focused panel,
  cursor commit/branch/tag/worktree, ◉-marked commits, marked files, the open
  diff/compare view and its selected file, the highlighted bookmark/shelf
  entry in an open `g`/`G` switcher, active filters, conflict/paused-op state.
  (The TUI publishes a snapshot file under your XDG state dir; no TUI running
  → `session: null`.)
- **Bookmarks** — `gg_bookmarks_list`, `gg_bookmark_get`, `gg_bookmark_read`.
- **Shelves** — `gg_shelf_buckets`, `gg_shelf_list`, `gg_shelf_commit_files`,
  `gg_shelf_read`.
- **Compare** — `gg_compare_trees` (changed files between worktree/index/any
  commit), `gg_compare_file` (unified diff between any two file versions,
  including bookmarks and shelved-commit members).
- **Export** — `gg_export` copies a bookmark or shelf entry into a local
  directory. Stage 1 never mutates the repository.

Register it with Claude Code from your repo directory:

```sh
claude mcp add gg -- gg mcp
```
````

- [ ] **Step 4: Full verification**

```bash
go build ./... && ./test.sh race
```

Expected: every stage green (vet/gofmt, unit, e2e), no races.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md CLAUDE.md README.md
git commit -m "docs: MCP server stage 1 (gg mcp, session snapshot, tool surface)"
```

---

## Self-Review Notes (author)

- Spec coverage: snapshot writer (Task 3) ↔ spec "The session snapshot"; `gg_ui_state` (Task 4) ↔ tool 1; Tasks 5–8 ↔ tools 2–11; archtest/routing (Task 4) ↔ "Architecture"; docs (Task 9) ↔ "Documentation". Not-a-repo behavior ↔ Task 4's `repoErr`. Decider policy ↔ Task 8. Error contract exercised by `callErr` assertions throughout.
- Known judgment calls the implementer should NOT "fix": `commitFileRow` lives in `shelf.go` (Task 6 lands before Task 7); the four `register*` stubs in Task 4 exist so the package compiles before Tasks 5–8 replace them; `gg_ui_state` re-reads the snapshot file per call (no caching — the file IS the cache).
- The two seed helpers (`seedShelfFile` address shape, `&Repo{Runner: f}` construction) note where to adapt if a neighboring convention differs — the tool code under test is unaffected.
