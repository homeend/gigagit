# gg shelf cherry-pick (CLI lane + deferred hardening) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `gg shelf cherry-pick [--patch] [--on-conflict=keep|abort] <entry-id>` — the non-interactive twin of the TUI's `a` cherry-pick — plus four hardening items deferred from the TUI feature's final review.

**Architecture:** A new subcommand case in `cmdShelf` (`internal/cli/shelf.go`) composes existing pieces only: `domain.CommitLookup` probes whether the commit object still exists, then either `engine.CherryPick` (live lane, `cmdCherryPick`'s decider setup) or `domain.ShelfPatchFile` → `engine.ApplyPatch{ApplyModeCommits}` (patch lane) runs via `runOperation`/`finish`. No engine or domain changes. The hardening items are small TUI guards plus pinned tests.

**Tech Stack:** Go 1.26, standard `flag`, real git in `t.TempDir()` for tests.

**Spec:** `docs/superpowers/specs/2026-07-12-cli-shelf-cherry-pick-design.md`

## Global Constraints

- Exit codes (spec, verbatim): **0 = commit created (or a requested `--on-conflict=abort` rolled back cleanly, matching `gg cherry-pick`); 1 = failure OR conflicts left in the tree; 2 = usage.**
- Error/status wordings are load-bearing — copy them **verbatim** from the task steps (tests assert substrings).
- `internal/cli` and `internal/tui` never import `internal/git` or `internal/shelf` in non-test files (archtest-guarded; `_test.go` files are exempt).
- Tests use a real `git` in `t.TempDir()`; shelf-touching CLI tests MUST isolate state via the existing `shelfRepo(t)` pattern (`t.Setenv("XDG_STATE_HOME", …)` + `t.Setenv("HOME", …)`) **before** creating entries.
- Run `gofmt -w` on every touched file before committing (aligned struct blocks have broken the vet+gofmt gate before).
- Every commit message ends with the two trailers:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01CpRmKAbmQKQAKjHXv82aJ9`
- Work happens in the worktree `.claude/worktrees/cli-shelf-cherry-pick` on branch `feat/cli-shelf-cherry-pick` — verify with `git branch --show-current` before the first commit.
- Test commands below run from the worktree root.

---

### Task 1: Cross-field blob-reclaim pin tests (`internal/shelf`)

`FileStore.Remove` claims to reclaim a removed entry's blobs only when no
surviving entry references them in *either* field (`SHA` or `PatchSHA`) —
content-addressing can share one blob across fields. The existing tests cover
same-field sharing only. These tests pin the cross-field branches — the one
place where a regression silently loses data. **Expected: they PASS as
written** (the loop at `file_store.go` `Remove` already checks
`e.SHA == sha || e.PatchSHA == sha`). If any FAILS, stop and report — that is
a real data-loss bug, not a test problem.

**Files:**
- Test: `internal/shelf/file_store_test.go` (append)

**Interfaces:**
- Consumes: `NewFileStore(dir)`, `fs.PutCommit(bucket string, addr model.FileAddress, tar, patch []byte, label string)`, `fs.Put(bucket, addr, data)` — all existing.
- Produces: nothing (test-only).

- [ ] **Step 1: Append the three pin tests**

Append to `internal/shelf/file_store_test.go` (same package; `fs.blobPath` is
unexported but accessible; `model`, `os` already imported):

```go
// Cross-field reclaim: a removed entry's PATCH blob may be a survivor's TAR
// blob (and vice versa). Remove must check both fields of every survivor
// before deleting — this is the branch where a regression means data loss.

func TestRemoveKeepsPatchBlobSharedWithSurvivorTar(t *testing.T) {
	fs := NewFileStore(t.TempDir())
	a := model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5f6"}
	b := model.FileAddress{State: model.StateCommitted, Commit: "f6e5d4c3b2a1"}
	// Survivor's TAR bytes == removed entry's PATCH bytes → one shared blob.
	survivor, err := fs.PutCommit("", a, []byte("shared-bytes"), nil, "")
	if err != nil {
		t.Fatalf("PutCommit survivor: %v", err)
	}
	victim, err := fs.PutCommit("", b, []byte("tar-two"), []byte("shared-bytes"), "")
	if err != nil {
		t.Fatalf("PutCommit victim: %v", err)
	}
	if survivor.SHA != victim.PatchSHA {
		t.Fatal("fixture broken: expected the tar and patch blobs to dedup to one SHA")
	}
	if err := fs.Remove(victim.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(fs.blobPath(survivor.SHA)); err != nil {
		t.Fatalf("survivor's tar blob was reclaimed via the victim's PatchSHA: %v", err)
	}
}

func TestRemoveKeepsTarBlobSharedWithSurvivorPatch(t *testing.T) {
	fs := NewFileStore(t.TempDir())
	a := model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5f6"}
	b := model.FileAddress{State: model.StateCommitted, Commit: "f6e5d4c3b2a1"}
	// Survivor's PATCH bytes == removed entry's TAR bytes → one shared blob.
	survivor, err := fs.PutCommit("", a, []byte("tar-one"), []byte("shared-bytes"), "")
	if err != nil {
		t.Fatalf("PutCommit survivor: %v", err)
	}
	victim, err := fs.PutCommit("", b, []byte("shared-bytes"), nil, "")
	if err != nil {
		t.Fatalf("PutCommit victim: %v", err)
	}
	if survivor.PatchSHA != victim.SHA {
		t.Fatal("fixture broken: expected the patch and tar blobs to dedup to one SHA")
	}
	if err := fs.Remove(victim.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := fs.GetPatch(survivor.ID); err != nil {
		t.Fatalf("survivor's patch was reclaimed via the victim's SHA: %v", err)
	}
}

func TestRemoveKeepsFileBlobSharedWithSurvivorPatch(t *testing.T) {
	fs := NewFileStore(t.TempDir())
	a := model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5f6"}
	fileAddr := model.FileAddress{State: model.StateUnstaged, Worktree: "/w", Path: "x.txt"}
	survivor, err := fs.PutCommit("", a, []byte("tar-one"), []byte("shared-bytes"), "")
	if err != nil {
		t.Fatalf("PutCommit survivor: %v", err)
	}
	victim, err := fs.Put("", fileAddr, []byte("shared-bytes"))
	if err != nil {
		t.Fatalf("Put victim: %v", err)
	}
	if survivor.PatchSHA != victim.SHA {
		t.Fatal("fixture broken: expected the patch and file blobs to dedup to one SHA")
	}
	if err := fs.Remove(victim.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := fs.GetPatch(survivor.ID); err != nil {
		t.Fatalf("survivor's patch was reclaimed via a removed FILE entry's SHA: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests — expect PASS**

Run: `go test ./internal/shelf/ -run 'TestRemoveKeeps.*Shared' -v`
Expected: 3 PASS (these pin existing behavior). If any FAILS: **stop, do not
"fix" the test** — report BLOCKED with the failure output (it means `Remove`
really loses data on cross-field sharing).

- [ ] **Step 3: gofmt and commit**

```bash
gofmt -w internal/shelf/file_store_test.go
git add internal/shelf/file_store_test.go
git commit -m "test(shelf): pin cross-field blob reclaim in Remove

A removed entry's patch blob can be a survivor's tar blob (and vice
versa, and a plain file entry's blob) under content-addressing. Pin
that Remove checks both fields of every survivor before deleting —
deferred from the cherry-pick-shelf-bookmark final review.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CpRmKAbmQKQAKjHXv82aJ9"
```

---

### Task 2: `gg shelf cherry-pick` (CLI)

**Files:**
- Modify: `internal/cli/shelf.go` (usage string, subcommand switch, new function)
- Test: `internal/cli/shelf_test.go` (append)

**Interfaces:**
- Consumes (all existing, signatures verbatim):
  - `svc.CommitLookup(ctx, rev string) (model.LogLine, bool, error)` — missing commit is `(zero, false, nil)`, NOT an error.
  - `svc.ShelfPatchFile(ctx, entryID string) (string, error)` — temp file path; caller deletes.
  - `shelfEntryByID(svc, ctx, id) (model.ShelfEntry, bool)` — same file.
  - `e.IsCommit() bool`, `e.Origin.Commit`, `e.PatchSHA` on `model.ShelfEntry`.
  - `engine.CherryPick{Commit string}`, `engine.ApplyPatch{Path string, Mode engine.ApplyMode}` with `engine.ApplyModeCommits`.
  - `cliDecider{policy, in, out, interactive}`, `stdinIsTerminal()`, `runOperation(ctx, svc, op, dec, stderr)`, `finish(res, err, stdout, stderr)` — the `cmdCherryPick`/`cmdApply` patterns.
  - Test helpers: `shelfRepo(t)`, `runCLI(t, workdir, args...)`, `runCLIStdin(t, workdir, in, args...)`, `gitRun(t, dir, args...)`, `newRepoDir(t)`.
- Produces: the `cherry-pick` case reachable via `gg shelf cherry-pick …` and (automatically) `gg batch`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/shelf_test.go`. Add `"os/exec"` to its imports (it
currently imports `os`, `path/filepath`, `strings`, `testing`).

```go
// --- gg shelf cherry-pick ---

// shelfCherryPickFixture shelves a commit that adds pick.txt on a side
// branch, then returns HEAD to main. Returns the repo dir, the shelf entry
// id, and the commit sha.
func shelfCherryPickFixture(t *testing.T) (dir, id, sha string) {
	t.Helper()
	dir = shelfRepo(t)
	gitRun(t, dir, "checkout", "-b", "feat")
	if err := os.WriteFile(filepath.Join(dir, "pick.txt"), []byte("picked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add pick.txt")
	sha = revParseHead(t, dir)
	id = shelveCommit(t, dir, sha)
	gitRun(t, dir, "checkout", "main")
	return dir, id, sha
}

func revParseHead(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// shelveCommit runs `gg shelf commit <sha>` and returns the printed entry id.
func shelveCommit(t *testing.T, dir, sha string) string {
	t.Helper()
	code, out, errb := runCLI(t, dir, "shelf", "commit", sha)
	if code != 0 {
		t.Fatalf("shelf commit exit %d: %s", code, errb)
	}
	id := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "shelved commit as "))
	if id == "" {
		t.Fatalf("could not parse entry id from %q", out)
	}
	return id
}

// gcAway makes sha unreachable and prunes it, then proves it is gone.
func gcAway(t *testing.T, dir, sha string) {
	t.Helper()
	gitRun(t, dir, "reflog", "expire", "--expire=all", "--all")
	gitRun(t, dir, "gc", "--prune=now")
	cat := exec.Command("git", "-C", dir, "cat-file", "-e", sha)
	if err := cat.Run(); err == nil {
		t.Fatalf("commit %s was not pruned; fixture does not exercise the gc'd lane", sha)
	}
}

func TestShelfCherryPickLiveLane(t *testing.T) {
	dir, id, _ := shelfCherryPickFixture(t)
	code, out, errb := runCLI(t, dir, "shelf", "cherry-pick", id)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb)
	}
	if !strings.Contains(out, "cherry-picked") {
		t.Fatalf("live lane must run engine.CherryPick (summary says cherry-picked); stdout: %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "pick.txt")); err != nil {
		t.Fatal("pick.txt missing on main after cherry-pick")
	}
}

func TestShelfCherryPickPatchFlagForcesReplay(t *testing.T) {
	dir, id, _ := shelfCherryPickFixture(t)
	code, out, errb := runCLI(t, dir, "shelf", "cherry-pick", "--patch", id)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb)
	}
	// Both lanes mint a new sha; lane selection is observable in the op
	// summary — ApplyPatch says "applied …", CherryPick says "cherry-picked".
	if !strings.Contains(out, "applied") || strings.Contains(out, "cherry-picked") {
		t.Fatalf("--patch must force the ApplyPatch lane; stdout: %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "pick.txt")); err != nil {
		t.Fatal("pick.txt missing on main after patch replay")
	}
	subj, err := exec.Command("git", "-C", dir, "log", "-1", "--format=%s").Output()
	if err != nil || strings.TrimSpace(string(subj)) != "add pick.txt" {
		t.Fatalf("replayed commit subject = %q err %v, want add pick.txt", subj, err)
	}
}

func TestShelfCherryPickGcdFallsBackToPatch(t *testing.T) {
	dir, id, sha := shelfCherryPickFixture(t)
	gitRun(t, dir, "branch", "-D", "feat")
	gcAway(t, dir, sha)
	code, out, errb := runCLI(t, dir, "shelf", "cherry-pick", id)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb)
	}
	if !strings.Contains(out, "applied") {
		t.Fatalf("gc'd commit must fall back to the patch lane; stdout: %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "pick.txt")); err != nil {
		t.Fatal("pick.txt missing on main after gc'd-lane replay")
	}
}

// mergeShelfFixture shelves a MERGE commit — ShelfAddCommit stores it
// tar-only (format-patch cannot represent a merge), so the entry has no
// patch. Returns dir, entry id, merge sha, and main's pre-merge sha.
func mergeShelfFixture(t *testing.T) (dir, id, sha, preMerge string) {
	t.Helper()
	dir = shelfRepo(t)
	gitRun(t, dir, "checkout", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir, "side.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "side change")
	gitRun(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "main.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "main change")
	preMerge = revParseHead(t, dir)
	gitRun(t, dir, "merge", "--no-ff", "-m", "merge side", "side")
	sha = revParseHead(t, dir)
	id = shelveCommit(t, dir, sha)
	return dir, id, sha, preMerge
}

func TestShelfCherryPickPatchFlagOnPatchlessEntry(t *testing.T) {
	dir, id, _, _ := mergeShelfFixture(t)
	code, _, errb := runCLI(t, dir, "shelf", "cherry-pick", "--patch", id)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errb, "has no stored patch") {
		t.Fatalf("stderr: %q", errb)
	}
}

func TestShelfCherryPickGcdWithoutPatch(t *testing.T) {
	dir, id, sha, preMerge := mergeShelfFixture(t)
	gitRun(t, dir, "update-ref", "refs/heads/main", preMerge)
	gitRun(t, dir, "checkout", "-f", "main")
	gitRun(t, dir, "branch", "-D", "side")
	gcAway(t, dir, sha)
	code, _, errb := runCLI(t, dir, "shelf", "cherry-pick", id)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errb, "no longer exists and this entry has no stored patch") {
		t.Fatalf("stderr: %q", errb)
	}
}

func TestShelfCherryPickFileEntryRefused(t *testing.T) {
	dir := shelfRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errb := runCLI(t, dir, "shelf", "add", "README.md")
	if code != 0 {
		t.Fatalf("shelf add exit %d: %s", code, errb)
	}
	id := strings.TrimSpace(out)
	code, _, errb = runCLI(t, dir, "shelf", "cherry-pick", id)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errb, "not a shelved commit") {
		t.Fatalf("stderr: %q", errb)
	}
}

func TestShelfCherryPickUnknownID(t *testing.T) {
	dir := shelfRepo(t)
	code, _, errb := runCLI(t, dir, "shelf", "cherry-pick", "no-such-id")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errb, "no entry") {
		t.Fatalf("stderr: %q", errb)
	}
}

// shelfConflictFixture shelves a commit that conflicts with main's HEAD.
func shelfConflictFixture(t *testing.T) (dir, id string) {
	t.Helper()
	dir = shelfRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "base")
	gitRun(t, dir, "checkout", "-b", "feat")
	if err := os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "commit", "-am", "feat change")
	sha := revParseHead(t, dir)
	id = shelveCommit(t, dir, sha)
	gitRun(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "commit", "-am", "main change")
	return dir, id
}

func TestShelfCherryPickConflictKeep(t *testing.T) {
	dir, id := shelfConflictFixture(t)
	code, _, _ := runCLI(t, dir, "shelf", "cherry-pick", "--on-conflict=keep", id)
	if code != 1 {
		t.Fatalf("exit %d, want 1 (conflicts left in tree)", code)
	}
	got, err := os.ReadFile(filepath.Join(dir, "shared.txt"))
	if err != nil || !strings.Contains(string(got), "<<<<<<<") {
		t.Fatalf("conflict markers missing (err %v): %q", err, got)
	}
}

func TestShelfCherryPickConflictAbort(t *testing.T) {
	dir, id := shelfConflictFixture(t)
	code, out, errb := runCLI(t, dir, "shelf", "cherry-pick", "--on-conflict=abort", id)
	// Parity with `gg cherry-pick --on-conflict=abort`: the abort is the
	// requested outcome and the rollback succeeded → exit 0, "aborted" summary.
	if code != 0 {
		t.Fatalf("exit %d, want 0 (stderr: %s)", code, errb)
	}
	if !strings.Contains(out, "aborted") {
		t.Fatalf("abort summary missing: %q", out)
	}
	got, err := os.ReadFile(filepath.Join(dir, "shared.txt"))
	if err != nil || string(got) != "main\n" {
		t.Fatalf("abort must leave the tree clean (err %v): %q", err, got)
	}
}

func TestShelfCherryPickUsageErrors(t *testing.T) {
	dir := shelfRepo(t)
	for _, args := range [][]string{
		{"shelf", "cherry-pick"},
		{"shelf", "cherry-pick", "a", "b"},
		{"shelf", "cherry-pick", "--on-conflict=bogus", "a"},
	} {
		if code, _, _ := runCLI(t, dir, args...); code != 2 {
			t.Fatalf("%v: exit %d, want 2", args, code)
		}
	}
}

func TestBatchDrivesShelfCherryPick(t *testing.T) {
	dir, id, _ := shelfCherryPickFixture(t)
	code, out, errb := runCLIStdin(t, dir, "shelf cherry-pick "+id+"\n", "batch")
	if code != 0 {
		t.Fatalf("batch exit %d: %s", code, errb)
	}
	if !strings.Contains(out, "#1 ok shelf cherry-pick") {
		t.Fatalf("batch framing missing: %q", out)
	}
}
```

Note: `runCLIStdin` lives in `batch_test.go` (same package — no import
needed).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestShelfCherryPick|TestBatchDrivesShelfCherryPick' -v 2>&1 | head -30`
Expected: every test FAILS with `shelf: unknown subcommand "cherry-pick"` in
stderr (exit 2 where 0/1 expected). Compile errors mean a helper name drifted — fix the test, not the production code.

- [ ] **Step 3: Implement the subcommand**

In `internal/cli/shelf.go`:

(a) Add `"os"` to the imports.

(b) Update the usage line in `cmdShelf` and add the case (keep the existing
order, insert `cherry-pick` after `commit`):

```go
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg shelf <add|commit|cherry-pick|restore|export|list|rm> ...")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		return shelfAdd(svc, rest, stdout, stderr)
	case "commit":
		return shelfCommit(svc, rest, stdout, stderr)
	case "cherry-pick":
		return shelfCherryPick(svc, rest, stdin, stdout, stderr)
```

(c) Append the function:

```go
// shelfCherryPick re-applies a shelved commit onto the current branch: a live
// `git cherry-pick` while the commit object still exists, or a replay of the
// shelve-time format-patch snapshot (`git am --3way`, atomic) once it is
// gc'd. --patch forces the snapshot lane. The non-interactive twin of the
// TUI switchers' `a` key. Exit codes per the gg apply convention: 0 applied,
// 1 failure or conflicts left in the tree, 2 usage.
func shelfCherryPick(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("shelf cherry-pick", flag.ContinueOnError)
	fs.SetOutput(stderr)
	patch := fs.Bool("patch", false, "force the stored-patch replay (git am) even if the commit exists")
	onConflict := fs.String("on-conflict", "", "answer a live-lane cherry-pick conflict: keep|abort")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "usage: gg shelf cherry-pick [--patch] [--on-conflict=keep|abort] <entry-id>")
		return 2
	}
	policy := map[string]string{}
	switch *onConflict {
	case "":
	case "keep":
		policy["cherry-pick-conflict"] = "keep-conflicts"
	case "abort":
		policy["cherry-pick-conflict"] = "abort"
	default:
		fmt.Fprintf(stderr, "shelf cherry-pick: invalid --on-conflict %q (keep|abort)\n", *onConflict)
		return 2
	}
	ctx := context.Background()
	e, ok := shelfEntryByID(svc, ctx, fs.Arg(0))
	if !ok {
		fmt.Fprintf(stderr, "shelf cherry-pick: no entry %q\n", fs.Arg(0))
		return 1
	}
	if !e.IsCommit() {
		fmt.Fprintf(stderr, "shelf cherry-pick: %s is not a shelved commit\n", e.ID)
		return 1
	}
	sha := e.Origin.Commit
	_, found, err := svc.CommitLookup(ctx, sha)
	if err != nil {
		fmt.Fprintln(stderr, "shelf cherry-pick:", err)
		return 1
	}
	if found && !*patch { // live lane: the commit object still exists
		dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}
		res, err := runOperation(ctx, svc, engine.CherryPick{Commit: sha}, dec, stderr)
		return finish(res, err, stdout, stderr)
	}
	// Patch lane: forced by --patch, or the commit is gone.
	if e.PatchSHA == "" {
		if *patch {
			fmt.Fprintf(stderr, "shelf cherry-pick: entry %s has no stored patch\n", e.ID)
		} else {
			short := sha
			if len(short) > 7 {
				short = short[:7]
			}
			fmt.Fprintf(stderr, "shelf cherry-pick: commit %s no longer exists and this entry has no stored patch (shelved before patch support, or a merge commit)\n", short)
		}
		return 1
	}
	tmp, err := svc.ShelfPatchFile(ctx, e.ID)
	if err != nil {
		fmt.Fprintln(stderr, "shelf cherry-pick:", err)
		return 1
	}
	defer os.Remove(tmp)
	res, err := runOperation(ctx, svc, engine.ApplyPatch{Path: tmp, Mode: engine.ApplyModeCommits}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}
```

- [ ] **Step 4: Run the new tests — expect PASS**

Run: `go test ./internal/cli/ -run 'TestShelfCherryPick|TestBatchDrivesShelfCherryPick' -v`
Expected: all PASS.

- [ ] **Step 5: Run the full cli package**

Run: `go test ./internal/cli/`
Expected: PASS (no existing test regressed).

- [ ] **Step 6: gofmt and commit**

```bash
gofmt -w internal/cli/shelf.go internal/cli/shelf_test.go
git add internal/cli/shelf.go internal/cli/shelf_test.go
git commit -m "feat(cli): gg shelf cherry-pick — re-apply a shelved commit

Live lane runs engine.CherryPick while the commit object exists
(--on-conflict=keep|abort parity with gg cherry-pick); --patch or a
gc'd commit replays the shelve-time format-patch snapshot via
engine.ApplyPatch{ApplyModeCommits} (git am --3way, atomic). Exit
codes per the gg apply convention. Routed through cmdShelf, so
gg batch drives it for free.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CpRmKAbmQKQAKjHXv82aJ9"
```

---

### Task 3: TUI hardening — modal-clobber guards, pickGen closes, filter-mode test

Three small guards deferred from the TUI feature's final review. All in
`internal/tui`.

**Files:**
- Modify: `internal/tui/pick_commit.go` (handlePickProbe guard)
- Modify: `internal/tui/model.go` (pushTagCheckMsg guard, in the `case pushTagCheckMsg:` block around line 1877)
- Modify: `internal/tui/bookmark_popup.go` (pickGen bump in the `case tea.KeyEnter:` branch, ~line 244)
- Modify: `internal/tui/shelf_popup.go` (pickGen bump in the `case tea.KeyEnter:` branch, ~line 215)
- Test: `internal/tui/pick_commit_test.go`, `internal/tui/remote_tags_test.go`, `internal/tui/bookmark_popup_test.go`, `internal/tui/shelf_popup_test.go` (append to each)

**Interfaces:**
- Consumes: `footerModel()` (test model), `decisionState`, `engine.DecisionRequest`, `pickProbeMsg`/`pickTarget` (pick_commit.go), `pushTagCheckMsg` (remote_tags.go: fields `gen int`, `tipTags []model.Tag`, `remoteSet map[string]bool`, `err error`), `runeKey(s)`, `commitBookmarkFixture(id, sha, label string) model.Bookmark` (pick_commit_test.go:148), `shelfPopModel(entries...)`/`shEntry(id, path)` (shelf_popup_test.go), `bookmarkCopyModel(items...)` (bookmark_popup_test.go).
- Produces: nothing new — behavior guards only.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/pick_commit_test.go` (imports already cover these):

```go
func TestPickProbeDroppedWhenModalOpen(t *testing.T) {
	m := pickModel()
	m.modal = &decisionState{req: engine.DecisionRequest{
		ID: "other", Prompt: "unrelated?", Options: []string{"x", "y"}}}
	mm, cmd := m.Update(pickProbeMsg{gen: m.pickGen, target: pickTarget{sha: "abc"},
		line: model.LogLine{Hash: "abc1234", Subject: "s"}, found: true})
	m = mm.(Model)
	if m.modal == nil || m.modal.req.ID != "other" {
		t.Fatal("an open modal must not be clobbered by a returning pick probe")
	}
	if cmd != nil {
		t.Fatal("a dropped probe must dispatch nothing")
	}
	if !strings.Contains(m.statusMsg, "press a again") {
		t.Fatalf("the drop must be visible; statusMsg = %q", m.statusMsg)
	}
}
```

(`pick_commit_test.go` needs `"github.com/homeend/gigagit/internal/engine"`
added to its imports if not already present.)

Append to `internal/tui/remote_tags_test.go`:

```go
func TestPushTagCheckDroppedWhenModalOpen(t *testing.T) {
	m := footerModel()
	m.pushCheckGen = 3
	m.modal = &decisionState{req: engine.DecisionRequest{
		ID: "other", Prompt: "unrelated?", Options: []string{"x", "y"}}}
	// One unpushed tip tag: without the guard this opens the push-with-tags
	// modal (clobbering "other") — or worse, starts a push under the dialog.
	msg := pushTagCheckMsg{gen: 3, tipTags: []model.Tag{{Name: "v9"}},
		remoteSet: map[string]bool{}}
	mm, cmd := m.Update(msg)
	m = mm.(Model)
	if m.modal == nil || m.modal.req.ID != "other" {
		t.Fatal("an open modal must not be clobbered by a returning push-tag check")
	}
	if m.running || cmd != nil {
		t.Fatalf("no push may start under an open dialog (running=%v cmd=%v)", m.running, cmd)
	}
	if !strings.Contains(m.statusMsg, "press P again") {
		t.Fatalf("the drop must be visible; statusMsg = %q", m.statusMsg)
	}
}
```

Append to `internal/tui/bookmark_popup_test.go`:

```go
func TestBookmarkPopupEnterBumpsPickGen(t *testing.T) {
	m := bookmarkCopyModel(commitBookmarkFixture("b1", "a1b2c3d4e5f6a7b8", "subj"))
	before := m.pickGen
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if m.pickGen <= before {
		t.Fatalf("enter closes/re-stacks the switcher and must invalidate an in-flight probe; pickGen %d -> %d", before, m.pickGen)
	}
}

func TestBookmarkPopupAKeyStaysQueryRuneWhileFiltering(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &bookmarkPopup{filtering: true}
	_, cmd := p.update(m, runeKey("a"))
	if p.filter != "a" {
		t.Fatalf(`"a" while filtering must be a literal char; filter=%q`, p.filter)
	}
	if cmd != nil {
		t.Fatal(`"a" while filtering must not dispatch a probe`)
	}
}
```

(`bookmark_popup_test.go` may need `tea "github.com/charmbracelet/bubbletea"`
added to its imports.)

Append to `internal/tui/shelf_popup_test.go`:

```go
func TestShelfPopupEnterBumpsPickGen(t *testing.T) {
	m := shelfPopModel(shEntry("e1", "dir/x.go"))
	before := m.pickGen
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if m.pickGen <= before {
		t.Fatalf("enter closes/re-stacks the switcher and must invalidate an in-flight probe; pickGen %d -> %d", before, m.pickGen)
	}
}

func TestShelfPopupAKeyStaysQueryRuneWhileFiltering(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &shelfPopup{filtering: true}
	_, cmd := p.update(m, runeKey("a"))
	if p.filter != "a" {
		t.Fatalf(`"a" while filtering must be a literal char; filter=%q`, p.filter)
	}
	if cmd != nil {
		t.Fatal(`"a" while filtering must not dispatch a probe`)
	}
}
```

(`shelf_popup_test.go` may need the `tea` import too. In every touched test
file, add whichever of `engine`/`model`/`strings`/`tea` the compiler reports
missing — these are additions to existing import blocks, not new deps.)

- [ ] **Step 2: Run tests to verify the guards fail and the filter tests pass**

Run: `go test ./internal/tui/ -run 'TestPickProbeDroppedWhenModalOpen|TestPushTagCheckDroppedWhenModalOpen|TestBookmarkPopupEnterBumpsPickGen|TestShelfPopupEnterBumpsPickGen|TestBookmarkPopupAKeyStaysQueryRune|TestShelfPopupAKeyStaysQueryRune' -v`
Expected: the two modal-clobber tests and the two enter-bump tests FAIL; the
two filter-mode tests PASS (they pin existing behavior — that is fine, they
guard a regression the other changes could introduce).

- [ ] **Step 3: Implement the guards**

(a) `internal/tui/pick_commit.go`, in `handlePickProbe` — extend the existing
guard (currently `if msg.gen != m.pickGen || m.running {`):

```go
	if msg.gen != m.pickGen || m.running {
		return m, nil // stale (switcher closed / repo switched) or an op raced in
	}
	if m.modal != nil {
		// Another dialog opened while the probe ran — never clobber it.
		m.statusMsg = "cherry-pick: cancelled (another dialog opened) — press a again"
		return m, nil
	}
```

(b) `internal/tui/model.go`, in `case pushTagCheckMsg:` — insert after the
gen check and before `m.statusMsg = ""`:

```go
	case pushTagCheckMsg:
		if msg.gen != m.pushCheckGen {
			return m, nil // superseded (another P / op / repo switch)
		}
		if m.modal != nil {
			// Another dialog opened during the 5s check — never clobber it
			// (and never start a push under it).
			m.statusMsg = "push cancelled (another dialog opened) — press P again"
			return m, nil
		}
		m.statusMsg = ""
```

(c) `internal/tui/bookmark_popup.go`, top of the `case tea.KeyEnter:` branch
in `(*bookmarkPopup).update` (the branch that starts with
`if p.compareRef != nil {`):

```go
	case tea.KeyEnter:
		m.pickGen++ // every enter path leaves or re-stacks the switcher; drop an in-flight probe
		if p.compareRef != nil {
```

(d) `internal/tui/shelf_popup.go`, same change at the top of its
`case tea.KeyEnter:` branch (the one that starts with `e, ok := p.selected()`):

```go
	case tea.KeyEnter:
		m.pickGen++ // every enter path leaves or re-stacks the switcher; drop an in-flight probe
		e, ok := p.selected()
```

- [ ] **Step 4: Run the six tests — expect PASS**

Run: `go test ./internal/tui/ -run 'TestPickProbeDroppedWhenModalOpen|TestPushTagCheckDroppedWhenModalOpen|TestBookmarkPopupEnterBumpsPickGen|TestShelfPopupEnterBumpsPickGen|TestBookmarkPopupAKeyStaysQueryRune|TestShelfPopupAKeyStaysQueryRune' -v`
Expected: 6 PASS.

- [ ] **Step 5: Run the full tui package**

Run: `go test ./internal/tui/`
Expected: PASS. If an existing test fails on the new guards (e.g. a pick test
that legitimately expects a modal to open), read it carefully: the guard must
only drop results while a DIFFERENT modal is already open — an existing
failure more likely means the guard was inserted in the wrong place than that
the test needs changing.

- [ ] **Step 6: gofmt and commit**

```bash
gofmt -w internal/tui/pick_commit.go internal/tui/model.go internal/tui/bookmark_popup.go internal/tui/shelf_popup.go internal/tui/pick_commit_test.go internal/tui/remote_tags_test.go internal/tui/bookmark_popup_test.go internal/tui/shelf_popup_test.go
git add internal/tui/
git commit -m "fix(tui): async probe results never clobber an open modal

pickProbeMsg and pushTagCheckMsg now drop (with a visible status
notice) when another dialog is open on arrival; the g/G switchers
bump pickGen on every enter close, not just esc, so a stale probe
can't surface over the view enter opened. Plus a pinned test that a
filter-mode 'a' stays a query rune. Deferred from the
cherry-pick-shelf-bookmark final review.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CpRmKAbmQKQAKjHXv82aJ9"
```

---

### Task 4: Docs, agentskill bump, regenerated dogfood skill

**Files:**
- Modify: `internal/agentskill/using-gg.md` (shelf command block, ~lines 201–225)
- Modify: `internal/agentskill/agentskill.go:19` (`Version = 47` → `48`)
- Modify: `.claude/skills/using-gg/SKILL.md` (regenerated by `gg init --update` — do NOT hand-edit)
- Modify: `CHANGELOG.md` (Unreleased section)
- Modify: `README.md` (CLI reference block ~line 148–152; `G`-switcher row ~line 77)
- Modify: `CLAUDE.md` (`cli` package-map row)

**Interfaces:**
- Consumes: the Task 2 command exactly as shipped (name, flags, exit codes).
- Produces: nothing (docs).

- [ ] **Step 1: Document the command in using-gg.md**

In `internal/agentskill/using-gg.md`, after the `gg shelf export` bullet
(~line 225), add:

```markdown
- `gg shelf cherry-pick [--patch] [--on-conflict=keep|abort] <entry-id>` —
  re-apply a **shelved commit** onto the current branch. While the original
  commit object still exists this is a live `git cherry-pick`
  (`--on-conflict` pre-answers a conflict: `keep` leaves conflict markers in
  the tree and exits 1, `abort` rolls back and exits 1). Once the commit is
  gc'd or the history rewritten, gg replays the patch snapshot frozen at
  shelve time (`git am --3way`, atomic — all-or-nothing); `--patch` forces
  that lane even while the commit exists. An entry shelved before patch
  support, or a merge commit, has no snapshot: the gc'd case then exits 1
  with a clear message. Exit 0 = commit created, 1 = failure or conflicts
  left, 2 = usage.
```

- [ ] **Step 2: Bump the skill version**

In `internal/agentskill/agentskill.go` line 19: `const Version = 47` →
`const Version = 48`.

- [ ] **Step 3: Regenerate the installed dogfood copy**

```bash
go run ./cmd/gg init --update
git diff --stat .claude/skills/using-gg/SKILL.md
```

Expected: the file changed and its marker line now reads
`<!-- gg:using-gg:v48 -->`. Commit this regenerated file — it is tracked
(prior-feature gotcha: bumping Version without committing the regenerated
copy leaves the repo dirty for the next contributor).

- [ ] **Step 4: CHANGELOG, README, CLAUDE.md**

`CHANGELOG.md` — add under `## [Unreleased]` (create the section after the
title if it does not exist, matching the file's existing style):

```markdown
- `gg shelf cherry-pick [--patch] [--on-conflict=keep|abort] <entry-id>` — re-apply a
  shelved commit from the command line: a live `git cherry-pick` while the commit
  exists, or an atomic replay of the shelve-time patch snapshot (`git am --3way`)
  once it's gc'd; `--patch` forces the replay lane. Exit codes per `gg apply`
  (0 applied, 1 failure/conflicts, 2 usage); works under `gg batch`. The CLI twin
  of the `a` key in the TUI's `g`/`G` switchers.
- TUI hardening: async probe results (cherry-pick commit probe, pre-push tag check)
  no longer clobber an open dialog — they drop with a visible status notice; the
  `g`/`G` switchers invalidate an in-flight cherry-pick probe on every close path,
  not just esc.
```

`README.md` — in the CLI reference block (after the `gg shelf export` line
~152), add:

```
gg shelf cherry-pick [--patch] [--on-conflict=keep|abort] <entry-id>  # re-apply a shelved commit: live cherry-pick, or atomic patch replay (git am --3way) once gc'd; --patch forces the replay
```

`README.md` — in the `G` quick-switcher row (~line 77), after the sentence
describing the `a` key's patch-snapshot fallback (ending `…gets a clear
notice instead)`), insert: ` (CLI: \`gg shelf cherry-pick <entry-id>\`)`.

`CLAUDE.md` — in the `cli` package-map row, after the sentence describing
`gg apply` (ends `Routed through `runOne`, so `gg batch` can drive it too.`),
append:

```
`gg shelf cherry-pick [--patch] [--on-conflict=keep|abort] <entry-id>` re-applies a shelved commit — the CLI twin of the TUI switchers' `a` key: live `engine.CherryPick` while the commit exists (`--on-conflict` maps to the `cherry-pick-conflict` fork like `gg cherry-pick`), otherwise (or under `--patch`) `domain.ShelfPatchFile` → `engine.ApplyPatch{ApplyModeCommits}` (atomic `git am --3way`; a patch-less entry — pre-patch-support or a merge commit — exits 1 with a clear message). Exit codes per the `gg apply` convention; lives in `cmdShelf`, so `gg batch` drives it.
```

- [ ] **Step 5: Verify the whole tree**

Run: `./test.sh`
Expected: vet+gofmt gate, unit tests, and e2e all green.

- [ ] **Step 6: Commit**

```bash
git add internal/agentskill/ .claude/skills/using-gg/SKILL.md CHANGELOG.md README.md CLAUDE.md
git commit -m "docs: gg shelf cherry-pick — using-gg v48, README/CHANGELOG/CLAUDE.md

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CpRmKAbmQKQAKjHXv82aJ9"
```
