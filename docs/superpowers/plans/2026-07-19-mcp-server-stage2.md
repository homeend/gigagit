# gg MCP Server Stage 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The two stage-2 mutating MCP tools — `gg_cherry_pick` (two-lane live/patch replay of a shelved/bookmarked commit) and `gg_write_to_worktree` (restore/paste a stored file version as an unstaged change) — plus read-only/destructive annotations across the whole 13-tool roster.

**Architecture:** Pure additions to the existing `internal/mcp` frontend: each tool is a new file registering itself from `sdkServer()`, running the existing engine ops (`CherryPick`, `ApplyPatch{ApplyModeCommits}`, `WriteFile`) through the stage-1 `runOp`/`staticDecider` machinery with per-call decision policies. Outcome classification uses the ops' `(Result.Changed, error)` contract — never summary-string sniffing. Consent is the MCP client's own destructive-tool prompt, driven by SDK `ToolAnnotations`.

**Tech Stack:** Go 1.26, `github.com/modelcontextprotocol/go-sdk` v1.6.1 (already pinned), stage-1 test harness (real repos, in-memory transport).

**Spec:** `docs/superpowers/specs/2026-07-19-mcp-server-stage2-design.md` — the authoritative contracts.

## Global Constraints

- Tool names exactly `gg_cherry_pick` and `gg_write_to_worktree`; every reply carries `repo{common_dir, worktree}` via `s.repoInfo()`; every handler starts with `s.repoCheck()`.
- Decisions are answered ONLY from parameters via `staticDecider` (fail-loud on unexpected ids). New policy entries: `"cherry-pick-conflict"` → `"keep-conflicts"`/`"abort"` (exactly the `gg shelf cherry-pick --on-conflict` mapping); `"overwrite"` → `"overwrite"`/`"cancel"` (already spoken).
- `on_conflict` defaults to `"abort"`; `mode` defaults to `"auto"`; `overwrite` defaults to false.
- Outcome classification (cherry-pick live lane, mirrors the CLI `finish()` semantics):
  `err==nil && Changed` = applied; `err==nil && !Changed` = clean abort → tool error with the retry hint; `err!=nil && Changed` = conflicts left in tree → SUCCESS reply with `conflicts:true`; `err!=nil && !Changed` = plain tool error.
- No summary-string parsing for control flow; `Result.Summary` is carried in replies as display text only.
- `on_conflict:"keep"` leaving conflicts is a SUCCESSFUL reply (`conflicts:true` + conflicted paths from `svc.Status(ctx)` → `.Conflicts()`), never a tool error.
- Path safety inherited: `engine.WriteFile` → `Repo.WriteWorktreeFile` → `worktreePath` guard; do not add a second path check, DO test the rejection.
- Error contract: one-line English naming the fix. Exact messages are given per task below.
- Annotations: all 11 stage-1 read tools `ReadOnlyHint: true`; `gg_export`, `gg_cherry_pick`, `gg_write_to_worktree` non-read-only with `DestructiveHint` explicitly true; `OpenWorldHint` explicitly false on all 13 (gg's domain is the local repo).
- `internal/mcp` import rules unchanged (archtest); `internal/agentskill/using-gg.md` untouched.
- TDD; `./test.sh race` green before merge (Task 4). Commit per task.

## File Structure

| File | Responsibility |
|---|---|
| `internal/mcp/cherrypick.go` (new) | `gg_cherry_pick` tool: source resolve, two lanes, outcome classification |
| `internal/mcp/cherrypick_test.go` (new) | live/patch/conflict/refusal tests (incl. the gcAway recipe) |
| `internal/mcp/write.go` (new) | `gg_write_to_worktree` tool |
| `internal/mcp/write_test.go` (new) | restore/paste/overwrite/unchanged/refusal tests |
| `internal/mcp/types.go` (modify) | `readOnlyAnnotations()` / `mutatingAnnotations()` helpers |
| `internal/mcp/server.go` (modify) | two new `register*` calls in `sdkServer()` |
| `internal/mcp/{state,bookmarks,shelf,compare,export}.go` (modify) | `Annotations:` field on each Tool literal (Task 3) |
| `internal/mcp/server_test.go` (modify) | roster test grows the annotations axis |
| `CHANGELOG.md`, `CLAUDE.md`, `README.md` (modify) | Task 4 |

---

### Task 1: `gg_cherry_pick`

**Files:**
- Create: `internal/mcp/cherrypick.go`, `internal/mcp/cherrypick_test.go`
- Modify: `internal/mcp/server.go` (add `s.registerCherryPickTool(srv)` to `sdkServer()`)

**Interfaces:**
- Consumes: harness (`newTestEnv`/`call`/`callErr`, `gitRun`, `seedShelfCommit` from shelf_test.go, `seedBookmark` from bookmarks_test.go); `svc.ShelfFind`, `svc.BookmarkGet`, `svc.CommitLookup(ctx, rev) (model.LogLine, bool, error)`, `svc.ShelfPatchFile(ctx, id) (string, error)` (caller removes the temp file), `svc.Status(ctx) (model.WorkingTreeStatus, error)` + `.Conflicts() []model.FileStatus`; `engine.CherryPick{Commit}`, `engine.ApplyPatch{Path, Mode: engine.ApplyModeCommits}`; `runOp`, `staticDecider` (decider.go).
- Produces: `registerCherryPickTool(srv *sdk.Server)`; nothing else consumed later (Task 3 adds its `Annotations:` field to this file's Tool literal).

- [ ] **Step 1: Write the failing tests**

`internal/mcp/cherrypick_test.go`:

```go
package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// commitOnBranch adds a commit changing path to content and returns its sha.
func commitOnBranch(t *testing.T, e *testEnv, path, content, msg string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.dir, path), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, e.dir, "add", "-A")
	gitRun(t, e.dir, "commit", "-m", msg)
	return gitRun(t, e.dir, "rev-parse", "HEAD")
}

// gcAway makes sha unreachable and prunes it, then proves it is gone
// (the internal/cli/shelf_test.go recipe). Uses exec directly for the
// verification probe because gitRun t.Fatal's on the expected failure.
func gcAway(t *testing.T, e *testEnv, sha string) {
	t.Helper()
	gitRun(t, e.dir, "reflog", "expire", "--expire=all", "--all")
	gitRun(t, e.dir, "gc", "--prune=now")
	cat := exec.Command("git", "-C", e.dir, "cat-file", "-e", sha)
	if err := cat.Run(); err == nil {
		t.Fatalf("commit %s was not pruned; fixture does not exercise the gc'd lane", sha)
	}
}

func TestCherryPickLiveShelf(t *testing.T) {
	e := newTestEnv(t)
	sha := commitOnBranch(t, e, "b.txt", "shelved content\n", "feat: b")
	ce := seedShelfCommitAt(t, e, sha, "b label")
	gitRun(t, e.dir, "reset", "--hard", "HEAD~1") // move branch back; commit still reachable? keep it simple: pick onto a sibling state
	out := e.call(t, "gg_cherry_pick", map[string]any{"source": map[string]any{"shelf": ce.ID}})
	if out["lane"] != "live" || out["conflicts"] == true {
		t.Fatalf("reply = %v", out)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "b.txt")); err != nil {
		t.Fatalf("picked file missing: %v", err)
	}
	subj := gitRun(t, e.dir, "log", "-1", "--format=%s")
	if subj != "feat: b" {
		t.Fatalf("subject = %q", subj)
	}
}

func TestCherryPickLiveBookmark(t *testing.T) {
	e := newTestEnv(t)
	sha := commitOnBranch(t, e, "c.txt", "c\n", "feat: c")
	cb := seedCommitBookmark(t, e, sha)
	gitRun(t, e.dir, "reset", "--hard", "HEAD~1")
	out := e.call(t, "gg_cherry_pick", map[string]any{"source": map[string]any{"bookmark": cb.ID}})
	if out["lane"] != "live" {
		t.Fatalf("reply = %v", out)
	}
	if gitRun(t, e.dir, "log", "-1", "--format=%s") != "feat: c" {
		t.Fatalf("commit not applied")
	}
}

func TestCherryPickPatchLaneAfterGC(t *testing.T) {
	e := newTestEnv(t)
	sha := commitOnBranch(t, e, "d.txt", "d\n", "feat: d")
	ce := seedShelfCommitAt(t, e, sha, "d label")
	gitRun(t, e.dir, "reset", "--hard", "HEAD~1")
	gcAway(t, e, sha)
	out := e.call(t, "gg_cherry_pick", map[string]any{"source": map[string]any{"shelf": ce.ID}})
	if out["lane"] != "patch" {
		t.Fatalf("expected patch lane, reply = %v", out)
	}
	if out["subject"] != "d label" {
		t.Fatalf("patch-lane subject must be the entry label, got %v", out["subject"])
	}
	if gitRun(t, e.dir, "log", "-1", "--format=%s") != "feat: d" {
		t.Fatalf("replayed commit missing")
	}
}

func TestCherryPickModePatchForced(t *testing.T) {
	e := newTestEnv(t)
	sha := commitOnBranch(t, e, "f.txt", "f\n", "feat: f")
	ce := seedShelfCommitAt(t, e, sha, "")
	gitRun(t, e.dir, "reset", "--hard", "HEAD~1")
	out := e.call(t, "gg_cherry_pick", map[string]any{
		"source": map[string]any{"shelf": ce.ID}, "mode": "patch",
	})
	if out["lane"] != "patch" {
		t.Fatalf("mode:patch must force the replay, reply = %v", out)
	}
}

func TestCherryPickConflictAbortDefault(t *testing.T) {
	e := newTestEnv(t)
	sha := commitOnBranch(t, e, "a.txt", "hello\nconflicting\n", "feat: conflict")
	ce := seedShelfCommitAt(t, e, sha, "")
	gitRun(t, e.dir, "reset", "--hard", "HEAD~1")
	commitOnBranch(t, e, "a.txt", "hello\ndiverged\n", "feat: diverge")
	msg := e.callErr(t, "gg_cherry_pick", map[string]any{"source": map[string]any{"shelf": ce.ID}})
	if !strings.Contains(msg, "on_conflict") {
		t.Fatalf("abort error must name the retry: %s", msg)
	}
	if gitRun(t, e.dir, "status", "--porcelain") != "" {
		t.Fatalf("abort must leave a clean tree")
	}
}

func TestCherryPickConflictKeep(t *testing.T) {
	e := newTestEnv(t)
	sha := commitOnBranch(t, e, "a.txt", "hello\nconflicting\n", "feat: conflict")
	ce := seedShelfCommitAt(t, e, sha, "")
	gitRun(t, e.dir, "reset", "--hard", "HEAD~1")
	commitOnBranch(t, e, "a.txt", "hello\ndiverged\n", "feat: diverge")
	out := e.call(t, "gg_cherry_pick", map[string]any{
		"source": map[string]any{"shelf": ce.ID}, "on_conflict": "keep",
	})
	if out["conflicts"] != true {
		t.Fatalf("keep must report conflicts, reply = %v", out)
	}
	files := out["conflicted_files"].([]any)
	if len(files) != 1 || files[0] != "a.txt" {
		t.Fatalf("conflicted_files = %v", files)
	}
	data, _ := os.ReadFile(filepath.Join(e.dir, "a.txt"))
	if !strings.Contains(string(data), "<<<<<<<") {
		t.Fatalf("markers missing: %s", data)
	}
}

func TestCherryPickRefusals(t *testing.T) {
	e := newTestEnv(t)
	fe := seedShelfFile(t, e)
	fb := seedBookmark(t, e)

	msg := e.callErr(t, "gg_cherry_pick", map[string]any{"source": map[string]any{"shelf": fe.ID}})
	if !strings.Contains(msg, "gg_write_to_worktree") {
		t.Fatalf("file shelf entry refusal: %s", msg)
	}
	msg = e.callErr(t, "gg_cherry_pick", map[string]any{"source": map[string]any{"bookmark": fb.ID}})
	if !strings.Contains(msg, "gg_write_to_worktree") {
		t.Fatalf("file bookmark refusal: %s", msg)
	}
	msg = e.callErr(t, "gg_cherry_pick", map[string]any{"source": map[string]any{}})
	if !strings.Contains(msg, "exactly one") {
		t.Fatalf("source validation: %s", msg)
	}
	msg = e.callErr(t, "gg_cherry_pick", map[string]any{
		"source": map[string]any{"bookmark": "x"}, "mode": "patch",
	})
	if !strings.Contains(msg, "shelf") {
		t.Fatalf("mode:patch+bookmark must be a usage error: %s", msg)
	}
	msg = e.callErr(t, "gg_cherry_pick", map[string]any{
		"source": map[string]any{"shelf": "nope"},
	})
	if !strings.Contains(msg, "not found") {
		t.Fatalf("unknown id: %s", msg)
	}
	msg = e.callErr(t, "gg_cherry_pick", map[string]any{
		"source": map[string]any{"shelf": seedShelfCommit(t, e).ID}, "on_conflict": "merge",
	})
	if !strings.Contains(msg, `"keep"`) {
		t.Fatalf("bad on_conflict must list the options: %s", msg)
	}
}

func TestCherryPickBookmarkGoneCommit(t *testing.T) {
	e := newTestEnv(t)
	sha := commitOnBranch(t, e, "g.txt", "g\n", "feat: g")
	cb := seedCommitBookmark(t, e, sha)
	gitRun(t, e.dir, "reset", "--hard", "HEAD~1")
	gcAway(t, e, sha)
	msg := e.callErr(t, "gg_cherry_pick", map[string]any{"source": map[string]any{"bookmark": cb.ID}})
	if !strings.Contains(msg, "no longer exists") {
		t.Fatalf("gone-commit bookmark: %s", msg)
	}
}
```

Two seed helpers this file needs, add them here (they generalize the existing seeds, which are pinned to `e.sha`):

```go
// seedShelfCommitAt shelves sha as a commit entry with label.
func seedShelfCommitAt(t *testing.T, e *testEnv, sha, label string) model.ShelfEntry {
	t.Helper()
	entry, err := e.svc.ShelfAddCommit(context.Background(), sha, label)
	if err != nil {
		t.Fatalf("ShelfAddCommit: %v", err)
	}
	return entry
}

// seedCommitBookmark stores a path-less commit-pointer bookmark for sha.
func seedCommitBookmark(t *testing.T, e *testEnv, sha string) model.Bookmark {
	t.Helper()
	b, err := e.svc.BookmarkAdd(context.Background(), model.Bookmark{
		State: model.StateCommitted, Commit: sha,
	})
	if err != nil {
		t.Fatalf("BookmarkAdd: %v", err)
	}
	return b
}
```

(add `"context"`, `"os/exec"`, and `"github.com/homeend/gigagit/internal/model"` to the test file's imports).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/mcp/ -run TestCherryPick -v`
Expected: FAIL — tool not registered.

- [ ] **Step 3: Implement**

`internal/mcp/cherrypick.go`:

```go
package mcp

import (
	"context"
	"fmt"
	"os"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

type cherryPickSourceIn struct {
	Shelf    string `json:"shelf,omitempty"`    // shelf entry id (commit entry)
	Bookmark string `json:"bookmark,omitempty"` // bookmark id (commit pointer)
}

type cherryPickIn struct {
	Source     cherryPickSourceIn `json:"source"`
	OnConflict string             `json:"on_conflict,omitempty"` // abort (default) | keep
	Mode       string             `json:"mode,omitempty"`        // auto (default) | patch
}

type cherryPickOut struct {
	Repo            RepoInfo `json:"repo"`
	Lane            string   `json:"lane"` // live|patch
	Commit          string   `json:"commit"`
	Subject         string   `json:"subject,omitempty"`
	Summary         string   `json:"summary,omitempty"`
	Conflicts       bool     `json:"conflicts,omitempty"`
	ConflictedFiles []string `json:"conflicted_files,omitempty"`
}

func (s *Server) registerCherryPickTool(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{
		Name: "gg_cherry_pick",
		Description: "Re-apply a shelved or bookmarked COMMIT onto the current branch. " +
			"Live cherry-pick while the commit object exists; a shelved commit whose object was " +
			"gc'd (or mode:\"patch\") replays its stored patch atomically (git am --3way). " +
			"on_conflict: \"abort\" (default, rolls back) or \"keep\" (leaves conflict markers " +
			"in the tree and reports the conflicted files). MUTATES the repository.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in cherryPickIn) (*sdk.CallToolResult, cherryPickOut, error) {
		out := cherryPickOut{Repo: s.repoInfo()}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		if (in.Source.Shelf == "") == (in.Source.Bookmark == "") {
			return nil, out, fmt.Errorf("pass exactly one of source.shelf (entry id) or source.bookmark (id)")
		}
		var policy map[string]string
		switch in.OnConflict {
		case "", "abort":
			policy = map[string]string{"cherry-pick-conflict": "abort"}
		case "keep":
			policy = map[string]string{"cherry-pick-conflict": "keep-conflicts"}
		default:
			return nil, out, fmt.Errorf(`on_conflict must be "abort" or "keep" (got %q)`, in.OnConflict)
		}
		switch in.Mode {
		case "", "auto", "patch":
		default:
			return nil, out, fmt.Errorf(`mode must be "auto" or "patch" (got %q)`, in.Mode)
		}

		// Resolve the source to a commit sha (+ patch availability for shelf).
		var (
			sha      string
			label    string // shelve-time label; patch-lane subject fallback
			hasPatch bool
			shelfID  string
		)
		if in.Source.Shelf != "" {
			entry, err := s.svc.ShelfFind(ctx, in.Source.Shelf)
			if err != nil {
				return nil, out, fmt.Errorf("shelf entry not found: %s", in.Source.Shelf)
			}
			if !entry.IsCommit() {
				return nil, out, fmt.Errorf("shelf entry %s is a file entry — use gg_write_to_worktree to restore it", in.Source.Shelf)
			}
			sha, label, hasPatch, shelfID = entry.Origin.Commit, entry.Label, entry.PatchSHA != "", entry.ID
		} else {
			if in.Mode == "patch" {
				return nil, out, fmt.Errorf(`mode:"patch" needs a shelf source — bookmarks store no patch`)
			}
			b, err := s.svc.BookmarkGet(ctx, in.Source.Bookmark)
			if err != nil {
				return nil, out, fmt.Errorf("bookmark not found: %s", in.Source.Bookmark)
			}
			if !b.IsCommit() {
				return nil, out, fmt.Errorf("bookmark %s is a file bookmark — use gg_write_to_worktree to paste it", in.Source.Bookmark)
			}
			sha = b.Commit
		}
		out.Commit = shortSha(sha)

		line, found, err := s.svc.CommitLookup(ctx, sha)
		if err != nil {
			return nil, out, fmt.Errorf("resolving %s: %v", shortSha(sha), err)
		}

		if found && in.Mode != "patch" { // live lane
			out.Lane, out.Commit, out.Subject = "live", line.Hash, line.Subject
			res, opErr := runOp(ctx, s.svc, engine.CherryPick{Commit: sha}, staticDecider{policy: policy})
			out.Summary = res.Summary
			switch {
			case opErr == nil && res.Changed: // applied cleanly
				return nil, out, nil
			case opErr == nil && !res.Changed: // conflict hit, policy aborted, tree rolled back
				return nil, out, fmt.Errorf("cherry-pick hit conflicts and was aborted — retry with on_conflict:\"keep\" to keep them in the tree")
			case res.Changed: // conflicts left in the tree (keep, or a preserved-stash restore conflict)
				out.Conflicts = true
				if st, sErr := s.svc.Status(ctx); sErr == nil {
					for _, f := range st.Conflicts() {
						out.ConflictedFiles = append(out.ConflictedFiles, f.Path)
					}
				}
				return nil, out, nil
			default:
				return nil, out, fmt.Errorf("cherry-pick failed: %v", opErr)
			}
		}

		// Patch lane: shelf only.
		if shelfID == "" {
			return nil, out, fmt.Errorf("commit %s no longer exists and this bookmark stores no patch — only a shelf entry with a stored patch can be replayed; shelve commits you may want to restore later", shortSha(sha))
		}
		if !hasPatch {
			return nil, out, fmt.Errorf("commit %s no longer exists and shelf entry %s has no stored patch (shelved before patch support, or a merge commit) — use gg_export to recover its files", shortSha(sha), shelfID)
		}
		tmp, err := s.svc.ShelfPatchFile(ctx, shelfID)
		if err != nil {
			return nil, out, fmt.Errorf("materializing the stored patch: %v", err)
		}
		defer os.Remove(tmp)
		out.Lane, out.Subject = "patch", label
		res, opErr := runOp(ctx, s.svc, engine.ApplyPatch{Path: tmp, Mode: engine.ApplyModeCommits}, staticDecider{policy: map[string]string{}})
		out.Summary = res.Summary
		if opErr != nil { // ApplyModeCommits is atomic: any failure rolled back
			return nil, out, fmt.Errorf("patch replay failed (branch unchanged): %v", opErr)
		}
		return nil, out, nil
	})
}

// shortSha trims a full sha for display (the CLI's convention).
func shortSha(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
```

In `internal/mcp/server.go`'s `sdkServer()`, add after `s.registerExportTool(srv)`:

```go
	s.registerCherryPickTool(srv)
```

Note: if a `shortSha` helper already exists in `internal/mcp` under another name, reuse it instead of redefining.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mcp/ -run TestCherryPick -v`
Expected: PASS (9 tests). Then `go test ./internal/mcp/` (whole package, no regressions).

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/cherrypick.go internal/mcp/cherrypick_test.go internal/mcp/server.go
git commit -m "feat(mcp): gg_cherry_pick — two-lane live/patch replay with on_conflict policy"
```

---

### Task 2: `gg_write_to_worktree`

**Files:**
- Create: `internal/mcp/write.go`, `internal/mcp/write_test.go`
- Modify: `internal/mcp/server.go` (add `s.registerWriteTool(srv)` to `sdkServer()`)

**Interfaces:**
- Consumes: harness + seeds (`seedShelfFile`, `seedShelfCommit`, `seedBookmark` — and Task 1's `seedCommitBookmark`); `svc.ShelfFind`, `svc.ShelfBlob`, `svc.ResolveBytes(FileRef{SourceShelf, Locator, Path})`, `svc.BookmarkGet`/`BookmarkBytes`; `engine.WriteFile{Path, Data}` + `engine.ErrWriteCancelled`; `runOp`/`staticDecider`.
- Produces: `registerWriteTool(srv *sdk.Server)`.

- [ ] **Step 1: Write the failing tests**

`internal/mcp/write_test.go`:

```go
package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteRestoreShelfFileDefaultPath(t *testing.T) {
	e := newTestEnv(t)
	fe := seedShelfFile(t, e) // shelved a.txt = "hello\nworld\n"
	if err := os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("clobbered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := e.call(t, "gg_write_to_worktree", map[string]any{
		"source": map[string]any{"shelf": fe.ID}, "overwrite": true,
	})
	if out["path"] != "a.txt" {
		t.Fatalf("default path must be the origin path: %v", out["path"])
	}
	data, _ := os.ReadFile(filepath.Join(e.dir, "a.txt"))
	if string(data) != "hello\nworld\n" {
		t.Fatalf("restored content = %q", data)
	}
}

func TestWriteExplicitPathAndMember(t *testing.T) {
	e := newTestEnv(t)
	ce := seedShelfCommit(t, e)
	out := e.call(t, "gg_write_to_worktree", map[string]any{
		"source": map[string]any{"shelf": ce.ID, "member": "a.txt"},
		"path":   "restored/copy.txt",
	})
	if out["path"] != "restored/copy.txt" {
		t.Fatalf("path = %v", out["path"])
	}
	data, err := os.ReadFile(filepath.Join(e.dir, "restored", "copy.txt"))
	if err != nil || string(data) != "hello\nworld\n" {
		t.Fatalf("member write: %q err=%v", data, err)
	}
}

func TestWriteBookmarkPaste(t *testing.T) {
	e := newTestEnv(t)
	b := seedBookmark(t, e) // committed a.txt bookmark
	out := e.call(t, "gg_write_to_worktree", map[string]any{
		"source": map[string]any{"bookmark": b.ID}, "path": "pasted.txt",
	})
	if out["bytes"].(float64) != 12 {
		t.Fatalf("bytes = %v", out["bytes"])
	}
	if _, err := os.Stat(filepath.Join(e.dir, "pasted.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestWriteOverwriteRefusedThenAccepted(t *testing.T) {
	e := newTestEnv(t)
	fe := seedShelfFile(t, e)
	if err := os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("different\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := e.callErr(t, "gg_write_to_worktree", map[string]any{
		"source": map[string]any{"shelf": fe.ID},
	})
	if !strings.Contains(msg, "overwrite:true") {
		t.Fatalf("refusal must name the fix: %s", msg)
	}
	out := e.call(t, "gg_write_to_worktree", map[string]any{
		"source": map[string]any{"shelf": fe.ID}, "overwrite": true,
	})
	if out["unchanged"] == true {
		t.Fatalf("overwrite write must report a real write: %v", out)
	}
}

func TestWriteUnchangedNoOp(t *testing.T) {
	e := newTestEnv(t)
	fe := seedShelfFile(t, e) // a.txt already has identical content
	out := e.call(t, "gg_write_to_worktree", map[string]any{
		"source": map[string]any{"shelf": fe.ID},
	})
	if out["unchanged"] != true {
		t.Fatalf("identical bytes must be a reported no-op: %v", out)
	}
}

func TestWriteRefusals(t *testing.T) {
	e := newTestEnv(t)
	ce := seedShelfCommit(t, e)
	fe := seedShelfFile(t, e)

	msg := e.callErr(t, "gg_write_to_worktree", map[string]any{
		"source": map[string]any{"shelf": ce.ID},
	})
	if !strings.Contains(msg, "gg_shelf_commit_files") {
		t.Fatalf("commit entry without member: %s", msg)
	}
	msg = e.callErr(t, "gg_write_to_worktree", map[string]any{
		"source": map[string]any{"shelf": fe.ID, "member": "a.txt"},
	})
	if !strings.Contains(msg, "omit member") {
		t.Fatalf("file entry with member: %s", msg)
	}
	sha := gitRun(t, e.dir, "rev-parse", "HEAD")
	cb := seedCommitBookmark(t, e, sha)
	msg = e.callErr(t, "gg_write_to_worktree", map[string]any{
		"source": map[string]any{"bookmark": cb.ID},
	})
	if !strings.Contains(msg, "gg_cherry_pick") {
		t.Fatalf("commit bookmark refusal must hint gg_cherry_pick: %s", msg)
	}
	msg = e.callErr(t, "gg_write_to_worktree", map[string]any{"source": map[string]any{}})
	if !strings.Contains(msg, "exactly one") {
		t.Fatalf("source validation: %s", msg)
	}
	msg = e.callErr(t, "gg_write_to_worktree", map[string]any{
		"source": map[string]any{"shelf": fe.ID}, "path": "../outside.txt",
	})
	if msg == "" {
		t.Fatalf("outside-tree path must be rejected")
	}
	if _, err := os.Stat(filepath.Join(e.dir, "..", "outside.txt")); err == nil {
		t.Fatalf("outside-tree file was written")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/mcp/ -run TestWrite -v`
Expected: FAIL — tool not registered.

- [ ] **Step 3: Implement**

`internal/mcp/write.go`:

```go
package mcp

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

type writeSourceIn struct {
	Shelf    string `json:"shelf,omitempty"`    // shelf entry id
	Member   string `json:"member,omitempty"`   // member path inside a shelved commit
	Bookmark string `json:"bookmark,omitempty"` // bookmark id (file bookmark)
}

type writeIn struct {
	Source    writeSourceIn `json:"source"`
	Path      string        `json:"path,omitempty"` // repo-relative destination; default = origin path
	Overwrite bool          `json:"overwrite,omitempty"`
}

type writeOut struct {
	Repo      RepoInfo `json:"repo"`
	Path      string   `json:"path"`
	Bytes     int      `json:"bytes"`
	Unchanged bool     `json:"unchanged,omitempty"`
}

func (s *Server) registerWriteTool(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{
		Name: "gg_write_to_worktree",
		Description: "Write a stored file version — a shelf file entry, one member of a shelved " +
			"commit, or a file bookmark — into the working tree as an UNSTAGED change. path " +
			"defaults to the entry's own origin path; an existing different file is refused " +
			"unless overwrite:true; identical content is a no-op. MUTATES the working tree.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in writeIn) (*sdk.CallToolResult, writeOut, error) {
		out := writeOut{Repo: s.repoInfo()}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		if (in.Source.Shelf == "") == (in.Source.Bookmark == "") {
			return nil, out, fmt.Errorf("pass exactly one of source.shelf (entry id) or source.bookmark (id)")
		}

		var (
			data       []byte
			originPath string
		)
		if in.Source.Shelf != "" {
			entry, err := s.svc.ShelfFind(ctx, in.Source.Shelf)
			if err != nil {
				return nil, out, fmt.Errorf("shelf entry not found: %s", in.Source.Shelf)
			}
			switch {
			case entry.IsCommit() && in.Source.Member == "":
				return nil, out, fmt.Errorf("shelf entry %s is a commit — pass source.member (list members with gg_shelf_commit_files)", in.Source.Shelf)
			case !entry.IsCommit() && in.Source.Member != "":
				return nil, out, fmt.Errorf("shelf entry %s is a file entry — omit member", in.Source.Shelf)
			case entry.IsCommit():
				data, err = s.svc.ResolveBytes(ctx, model.FileRef{Source: model.SourceShelf, Locator: in.Source.Shelf, Path: in.Source.Member})
				if err != nil {
					return nil, out, fmt.Errorf("reading member %q of %s: %v", in.Source.Member, in.Source.Shelf, err)
				}
				originPath = in.Source.Member
			default:
				data, err = s.svc.ShelfBlob(ctx, in.Source.Shelf)
				if err != nil {
					return nil, out, fmt.Errorf("reading shelf entry %s: %v", in.Source.Shelf, err)
				}
				originPath = entry.Origin.Path
			}
		} else {
			b, err := s.svc.BookmarkGet(ctx, in.Source.Bookmark)
			if err != nil {
				return nil, out, fmt.Errorf("bookmark not found: %s", in.Source.Bookmark)
			}
			if b.IsCommit() {
				return nil, out, fmt.Errorf("bookmark %s is a commit pointer — use gg_cherry_pick to re-apply it or gg_export to copy its files", in.Source.Bookmark)
			}
			data, err = s.svc.BookmarkBytes(ctx, b)
			if err != nil {
				return nil, out, fmt.Errorf("reading bookmark %s: %v", in.Source.Bookmark, err)
			}
			originPath = b.Path
		}

		dest := in.Path
		if dest == "" {
			dest = originPath
		}
		if dest == "" {
			return nil, out, fmt.Errorf("this source has no origin path — pass path explicitly")
		}
		out.Path, out.Bytes = dest, len(data)

		policy := map[string]string{"overwrite": "cancel"}
		if in.Overwrite {
			policy["overwrite"] = "overwrite"
		}
		res, err := runOp(ctx, s.svc, engine.WriteFile{Path: dest, Data: data}, staticDecider{policy: policy})
		if err != nil {
			if errors.Is(err, engine.ErrWriteCancelled) {
				return nil, out, fmt.Errorf("file exists: %s — pass overwrite:true to replace it", dest)
			}
			return nil, out, fmt.Errorf("write failed: %v", err)
		}
		out.Unchanged = !res.Changed // WriteFile: identical bytes = Changed:false no-op
		return nil, out, nil
	})
}
```

In `internal/mcp/server.go`'s `sdkServer()`, add after `s.registerCherryPickTool(srv)`:

```go
	s.registerWriteTool(srv)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mcp/ -run TestWrite -v` then `go test ./internal/mcp/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/write.go internal/mcp/write_test.go internal/mcp/server.go
git commit -m "feat(mcp): gg_write_to_worktree — restore/paste a stored file version as unstaged"
```

---

### Task 3: Roster annotations

**Files:**
- Modify: `internal/mcp/types.go` (helpers), `internal/mcp/state.go`, `internal/mcp/bookmarks.go`, `internal/mcp/shelf.go`, `internal/mcp/compare.go`, `internal/mcp/export.go`, `internal/mcp/cherrypick.go`, `internal/mcp/write.go` (one `Annotations:` field per Tool literal)
- Modify: `internal/mcp/server_test.go` (roster test grows the annotations axis)

**Interfaces:**
- Consumes: `sdk.ToolAnnotations{ReadOnlyHint bool, DestructiveHint *bool, IdempotentHint bool, OpenWorldHint *bool, Title string}` (v1.6.1 — DestructiveHint/OpenWorldHint are POINTERS, defaults true when nil).
- Produces: `readOnlyAnnotations()`, `mutatingAnnotations()` in types.go.

- [ ] **Step 1: Extend the roster test (failing first)**

Replace `TestServerListsStageOneTools` in `internal/mcp/server_test.go` with:

```go
func TestServerToolRosterAndAnnotations(t *testing.T) {
	e := newTestEnv(t)
	res, err := e.cs.ListTools(context.Background(), &sdk.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	readOnly := map[string]bool{
		"gg_ui_state": true, "gg_bookmarks_list": true, "gg_bookmark_get": true,
		"gg_bookmark_read": true, "gg_shelf_buckets": true, "gg_shelf_list": true,
		"gg_shelf_commit_files": true, "gg_shelf_read": true,
		"gg_compare_trees": true, "gg_compare_file": true,
		// non-read-only:
		"gg_export": false, "gg_cherry_pick": false, "gg_write_to_worktree": false,
	}
	got := map[string]*sdk.Tool{}
	for _, tool := range res.Tools {
		got[tool.Name] = tool
	}
	for name, ro := range readOnly {
		tool, ok := got[name]
		if !ok {
			t.Errorf("tool %s not registered", name)
			continue
		}
		if tool.Annotations == nil {
			t.Errorf("%s: missing annotations", name)
			continue
		}
		if tool.Annotations.ReadOnlyHint != ro {
			t.Errorf("%s: ReadOnlyHint = %v, want %v", name, tool.Annotations.ReadOnlyHint, ro)
		}
		if tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint {
			t.Errorf("%s: OpenWorldHint must be explicitly false", name)
		}
		if !ro && (tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint) {
			t.Errorf("%s: mutating tool must carry DestructiveHint true", name)
		}
	}
	if len(got) != len(readOnly) {
		t.Errorf("roster size = %d, want %d: %v", len(got), len(readOnly), got)
	}
}
```

Run: `go test ./internal/mcp/ -run TestServerToolRosterAndAnnotations -v`
Expected: FAIL — annotations missing.

- [ ] **Step 2: Add the helpers and wire every tool**

Append to `internal/mcp/types.go`:

```go
import sdk "github.com/modelcontextprotocol/go-sdk/mcp"

func boolPtr(b bool) *bool { return &b }

// readOnlyAnnotations marks a tool as not modifying anything; gg's world is
// the local repo, so OpenWorld is explicitly false on every tool.
func readOnlyAnnotations() *sdk.ToolAnnotations {
	return &sdk.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPtr(false)}
}

// mutatingAnnotations marks a tool that changes state (the repo, or a target
// directory for export) — MCP clients gate these behind their consent prompt.
func mutatingAnnotations() *sdk.ToolAnnotations {
	return &sdk.ToolAnnotations{
		ReadOnlyHint:    false,
		DestructiveHint: boolPtr(true),
		OpenWorldHint:   boolPtr(false),
	}
}
```

(merge the import into the file's import block properly). Then add to every `&sdk.Tool{...}` literal:
- `Annotations: readOnlyAnnotations(),` — the 10 read tools (state.go ×1, bookmarks.go ×3, shelf.go ×4, compare.go ×2).
- `Annotations: mutatingAnnotations(),` — export.go, cherrypick.go, write.go.

- [ ] **Step 3: Tests pass**

Run: `go test ./internal/mcp/ -v`
Expected: PASS (whole package, including the renamed roster test; no other test referenced the old name).

- [ ] **Step 4: Commit**

```bash
git add internal/mcp/
git commit -m "feat(mcp): read-only/destructive annotations across the 13-tool roster"
```

---

### Task 4: Docs + full verification

**Files:**
- Modify: `CHANGELOG.md`, `CLAUDE.md`, `README.md`

**Interfaces:** none (prose). `internal/agentskill/using-gg.md` deliberately untouched.

- [ ] **Step 1: CHANGELOG**

Under `## [Unreleased]`'s `### Added` (create the block if a release cut it), matching house style:

```markdown
- `gg mcp` stage 2 — the first mutating MCP tools, each gated by the MCP
  client's destructive-tool consent prompt: `gg_cherry_pick` re-applies a
  shelved or bookmarked commit onto the current branch (live cherry-pick while
  the commit exists; a shelved commit's stored patch replays atomically via
  `git am --3way` after a gc, or with `mode:"patch"`; `on_conflict:"abort"`
  rolls back, `"keep"` leaves the conflicts and reports the conflicted files),
  and `gg_write_to_worktree` writes a shelf file entry, a shelved-commit
  member, or a file bookmark into the working tree as an unstaged change
  (destination defaults to the entry's own path; `overwrite` guard; identical
  content is a reported no-op). All 13 tools now carry read-only/destructive
  annotations.
```

- [ ] **Step 2: CLAUDE.md**

Append to the `mcp` package-map row's description cell (before its closing `|`):

```markdown
**Stage 2 (mutating):** `gg_cherry_pick` — the `gg shelf cherry-pick` two-lane logic (live `engine.CherryPick` with the `"cherry-pick-conflict"` decision answered from `on_conflict` (abort default / keep→`keep-conflicts`), else `ShelfPatchFile` → `engine.ApplyPatch{ApplyModeCommits}`); outcome classified by the `(Result.Changed, error)` contract, never summary sniffing — `(nil, !Changed)` = clean abort → error with retry hint, `(err, Changed)` = conflicts-in-tree → SUCCESS reply with `conflicts:true` + paths from `Status().Conflicts()`. `gg_write_to_worktree` — restore/paste via `engine.WriteFile` (dest defaults to the origin path; `"overwrite"` decision from the param; `unchanged` = `!Changed`; path safety inherited from `WriteWorktreeFile`'s guard). All 13 tools carry `ToolAnnotations` (`readOnlyAnnotations`/`mutatingAnnotations` in types.go — v1.6.1 gotcha: `DestructiveHint`/`OpenWorldHint` are POINTERS, nil means true); consent = the MCP client's destructive-tool prompt, no gg-side approval store.
```

Update the Status/roadmap MCP clause: stage 2 (mutating tools) shipped; remaining MCP roadmap = heavy-ops surface.

- [ ] **Step 3: README**

In the `## MCP server (`gg mcp`)` section, after the Export bullet, add:

```markdown
- **Mutating tools (stage 2)** — `gg_cherry_pick` re-applies a shelved or
  bookmarked commit onto the current branch (falling back to the shelved
  commit's stored patch when the original was gc'd), and
  `gg_write_to_worktree` restores/pastes a stored file version as an unstaged
  change. Both are annotated destructive, so your MCP client asks before
  running them; `on_conflict` / `overwrite` parameters control the risky
  paths explicitly.
```

And update the intro sentence "Stage 1 never mutates the repository" to reflect that stage-2 tools mutate only behind the client's consent prompt.

- [ ] **Step 4: Full verification**

```bash
go build ./... && ./test.sh race
```

Expected: every stage green, no races.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md CLAUDE.md README.md
git commit -m "docs: MCP server stage 2 (mutating tools + annotations)"
```

---

## Self-Review Notes (author)

- Spec coverage: tool contracts ↔ Tasks 1–2 (every param, lane, refusal message, and the keep-is-success rule); annotations/consent ↔ Task 3; errors ↔ callErr assertions; testing list ↔ the three test files (live shelf+bookmark, gc'd replay, mode:patch, abort/keep conflicts, refusals incl. mode:patch+bookmark and gone-commit bookmark; write default/explicit/member/overwrite-pair/unchanged/path-less/outside-tree); docs ↔ Task 4. `./test.sh race` ↔ Task 4.
- Deliberate judgment calls the implementer should NOT "fix": conflicts-in-tree classification intentionally covers BOTH keep-conflicts and the preserved-stash restore-conflict (the CLI's `finish()` treats them identically; the reply's `summary` distinguishes them for the reader); the patch lane passes an empty decision policy (ApplyModeCommits asks nothing — a decision reaching `staticDecider` there SHOULD fail loud); `TestCherryPickLiveShelf` resets the branch back before picking so the pick actually changes the tree while the commit object stays reachable via the shelf's tar+patch AND git's object store (reset alone doesn't gc it).
- Type consistency: `seedShelfCommitAt`/`seedCommitBookmark` (Task 1) are consumed by Task 2's tests; `shortSha` defined once in cherrypick.go; annotations helpers defined in Task 3 and referenced nowhere earlier.
