# Remotes-tab fetch + prune — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax. When authoring e2e scenarios, also consult the project `writing-e2e-scenarios` skill.

**Goal:** Add separate **Fetch** (`git fetch --all`) and **Prune** (`git remote prune <all remotes>`) actions — engine ops, TUI (`f` + a `.`-menu row), and `gg remote fetch`/`gg remote prune` — that refresh remote-tracking refs and the Branches `(↓N)` counts.

**Architecture:** Two RefWrite engine ops over three new git verbs. The TUI runs them via `m.startOp` (snapshot reload refreshes the Remotes list + behind-counts); the CLI runs them via `runOperation` under `gg remote`. e2e gains a `stdout_excludes` assertion (to prove prune removed a ref) and a builder `branch_delete` step (to delete an origin branch).

**Tech Stack:** Go 1.26; `internal/engine` (ops implement `Operation.Run`; RefWrite via `LockMode()`), `internal/git` (verbs = one invocation via `gitcmd`), `internal/tui` (footer binding + `.`-menu row), `internal/cli` (`cmdRemote` subcommands), `e2e` (TOML scenarios over the in-process `git http-backend`).

## Global Constraints

- Module `github.com/gigagit/gg`, Go 1.26. Branch off `main`; the human merges.
- A git verb is ONE invocation via `gitcmd`, run through `r.Runner`. Both ops act on **all** remotes (no per-row targeting).
- **Fetch is non-streaming**: `Runner.Stream` scans only stdout, but `git fetch` writes progress to stderr — streaming forwards nothing. So `FetchAll` uses plain `Run` and the `Fetch` op emits one `Progress{Step:"fetching all remotes"}`. (This is a deliberate simplification of the spec's "streams progress lines".)
- Both ops are **RefWrite** (`LockMode() repogate.RefWrite`) — they touch refs, not the working tree (mirror `SmartPull`'s background-fetch reservation, `smart_pull.go:33`).
- New verbs go on the `engine.GitOps` interface; the `var _ GitOps = (*git.Repo)(nil)` proof keeps `*git.Repo` honest.
- TUI `f` is gated strictly on `m.focus == panelRemotes` (don't shadow other panels; the only existing `f` is in the diff view).
- A CLI surface change bumps `agentskill.Version` + updates `using-gg.md` + refreshes the dogfood `.claude/skills/using-gg/SKILL.md` via `gg init --update` (else `TestDogfoodSkillCopyInSync` fails).
- TDD: failing test → minimal code → green → commit. `./test.sh unit` per task; `./test.sh race` before wrap-up. No `_GOOS`/`_GOARCH` token before `_test.go`.
- Scenario files are `sNN_*.toml`; current max is `s48`. New files continue at `s49`. Confirm with `ls e2e/scenarios/` at Task 4.

---

### Task 1: git verbs — `FetchAll`, `RemoteNames`, `PruneRemotes`

**Files:**
- Modify: `internal/git/sync.go` (verbs; add `strings` import if absent)
- Modify: `internal/engine/gitops.go` (add 3 signatures to `GitOps`)
- Test: `internal/git/sync_test.go` (append)

**Interfaces:**
- Produces: `(*git.Repo).FetchAll(ctx) error`, `RemoteNames(ctx) ([]string, error)`, `PruneRemotes(ctx, names ...string) error`

- [ ] **Step 1: Write failing verb tests**

Append to `internal/git/sync_test.go` (real-git; reuse `gitExec` from `mutate_test.go`, same package, which sets the author env):

```go
func TestRemoteNames(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	gitExec(t, dir, "remote", "add", "origin", "https://example.invalid/x.git")
	gitExec(t, dir, "remote", "add", "upstream", "https://example.invalid/y.git")
	names, err := repo.RemoteNames(context.Background())
	if err != nil {
		t.Fatalf("RemoteNames: %v", err)
	}
	if len(names) != 2 || names[0] != "origin" || names[1] != "upstream" {
		t.Fatalf("names = %v, want [origin upstream]", names)
	}
}

func TestFetchAllUpdatesTrackingRef(t *testing.T) {
	// origin (bare) ← seed pushes main + foo; clone; origin advances foo; FetchAll.
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone := filepath.Join(root, "clone")
	gitExec(t, root, "init", "--bare", origin)
	gitExec(t, root, "clone", origin, seed)
	gitExec(t, seed, "checkout", "-b", "main")
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v1\n"), 0o644)
	gitExec(t, seed, "add", ".")
	gitExec(t, seed, "commit", "-m", "c1")
	gitExec(t, seed, "push", "-u", "origin", "main")
	gitExec(t, root, "clone", origin, clone)
	// origin advances main via the seed.
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v2\n"), 0o644)
	gitExec(t, seed, "add", ".")
	gitExec(t, seed, "commit", "-m", "c2")
	gitExec(t, seed, "push", "origin", "main")

	repo := &Repo{Runner: runnerIn(clone)} // see NOTE
	if err := repo.FetchAll(context.Background()); err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	// refs/remotes/origin/main now points at c2 (the clone never checked it out).
	if ok, _ := repo.IsAncestor(context.Background(), "refs/remotes/origin/main", "refs/remotes/origin/main"); !ok {
		t.Fatal("sanity")
	}
	got := gitExec(t, clone, "log", "-1", "--format=%s", "refs/remotes/origin/main")
	if strings.TrimSpace(got) != "c2" {
		t.Fatalf("origin/main subject = %q, want c2 (fetch did not update)", got)
	}
}

func TestPruneRemotes(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone := filepath.Join(root, "clone")
	gitExec(t, root, "init", "--bare", origin)
	gitExec(t, root, "clone", origin, seed)
	gitExec(t, seed, "checkout", "-b", "main")
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v1\n"), 0o644)
	gitExec(t, seed, "add", ".")
	gitExec(t, seed, "commit", "-m", "c1")
	gitExec(t, seed, "push", "-u", "origin", "main")
	gitExec(t, seed, "push", "origin", "main:foo") // create origin/foo
	gitExec(t, root, "clone", origin, clone)       // clone has refs/remotes/origin/foo
	gitExec(t, seed, "push", "origin", "--delete", "foo")

	repo := &Repo{Runner: runnerIn(clone)}
	if err := repo.PruneRemotes(context.Background(), "origin"); err != nil {
		t.Fatalf("PruneRemotes: %v", err)
	}
	if _, err := gitExecErr(clone, "rev-parse", "--verify", "refs/remotes/origin/foo"); err == nil {
		t.Fatal("origin/foo tracking ref should be pruned")
	}
	gitExec(t, clone, "rev-parse", "--verify", "refs/remotes/origin/main") // live ref survives
	if err := repo.PruneRemotes(context.Background()); err != nil {
		t.Fatalf("PruneRemotes() with no names should be a no-op: %v", err)
	}
}
```

NOTE: the git package's `newTestRepo` returns `(dir, runner)` bound to `dir`.
These tests need a runner bound to `clone` instead. Add a tiny helper in the
test file: `func runnerIn(dir string) gitexec.Runner { return
gitexec.NewExecRunner("git", dir, observ.NewRing(50)) }` (imports already present
in `sync_test.go`). Also add `gitExecErr(dir, args...) (string, error)` — a
non-fatal variant of `gitExec` (run, return stdout+err) — for the absence check;
if `mutate_test.go` already has such a helper, reuse it.

- [ ] **Step 2: Run them — expect failure**

Run: `go test ./internal/git/ -run 'TestRemoteNames|TestFetchAll|TestPruneRemotes'`
Expected: build failure — the three methods (and maybe `runnerIn`/`gitExecErr`) are undefined.

- [ ] **Step 3: Implement the verbs**

In `internal/git/sync.go` (add `"strings"` to imports if not present):

```go
// FetchAll updates tracking refs for every configured remote (no prune).
func (r *Repo) FetchAll(ctx context.Context) error {
	argv := gitcmd.New("fetch").Arg("--all", "--no-write-fetch-head").ToArgv()
	_, err := r.Runner.Run(ctx, "git fetch --all", argv)
	return err
}

// RemoteNames lists configured remote names, one per line.
func (r *Repo) RemoteNames(ctx context.Context) ([]string, error) {
	argv := gitcmd.New("remote").ToArgv()
	res, err := r.Runner.Run(ctx, "git remote", argv)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, ln := range strings.Split(res.Stdout, "\n") {
		if s := strings.TrimSpace(ln); s != "" {
			names = append(names, s)
		}
	}
	return names, nil
}

// PruneRemotes removes tracking refs for branches deleted on the named remotes,
// in one invocation. Empty names is a no-op (no error).
func (r *Repo) PruneRemotes(ctx context.Context, names ...string) error {
	if len(names) == 0 {
		return nil
	}
	argv := gitcmd.New("remote").Arg("prune").Arg(names...).ToArgv()
	_, err := r.Runner.Run(ctx, "git remote prune", argv)
	return err
}
```

- [ ] **Step 4: Add the three to `GitOps`**

In `internal/engine/gitops.go`, near the existing `Fetch`:

```go
	FetchAll(ctx context.Context) error
	RemoteNames(ctx context.Context) ([]string, error)
	PruneRemotes(ctx context.Context, names ...string) error
```

- [ ] **Step 5: Run verb tests + build — expect green**

Run: `go test ./internal/git/ -run 'TestRemoteNames|TestFetchAll|TestPruneRemotes'` → PASS.
Run: `go build ./...` → clean (GitOps proof compiles).

- [ ] **Step 6: Commit**

```bash
git add internal/git/sync.go internal/git/sync_test.go internal/engine/gitops.go
git commit -m "feat(git): FetchAll / RemoteNames / PruneRemotes verbs"
```

---

### Task 2: engine ops — `Fetch` and `Prune`

**Files:**
- Create: `internal/engine/fetch.go`, `internal/engine/prune.go`
- Test: `internal/engine/fetch_test.go`, `internal/engine/prune_test.go`

**Interfaces:**
- Consumes: the Task-1 verbs via `OpDeps.Repo`
- Produces: `engine.Fetch{}`, `engine.Prune{}` (both `Operation` + `LockMode`)

- [ ] **Step 1: Write failing op tests**

Create `internal/engine/fetch_test.go` and `internal/engine/prune_test.go`. Reuse
the engine package's real-git helpers (`gitIn` from `remove_worktree_test.go`,
and the bare-origin clone pattern). Sketch (fill in the clone setup mirroring
`smart_pull_test.go`'s origin helpers if present, else inline like Task 1):

```go
// fetch_test.go
func TestFetchUpdatesAllRemotes(t *testing.T) {
	dir, repo := newCloneAheadOrigin(t) // helper: clone whose origin/main advanced; see NOTE
	_ = dir
	res, err := Fetch{}.Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	// origin/main tracking ref advanced.
	if ok, _ := repo.IsAncestor(context.Background(), "refs/remotes/origin/main", "HEAD"); ok {
		t.Fatal("origin/main should now be AHEAD of HEAD after fetch")
	}
}

func TestFetchLockModeIsRefWrite(t *testing.T) {
	if Fetch{}.LockMode() != repogate.RefWrite {
		t.Fatal("Fetch must be RefWrite")
	}
}
```

```go
// prune_test.go
func TestPruneRemovesDeletedUpstreamRef(t *testing.T) {
	dir, repo := newCloneWithDeletedOriginFoo(t) // origin/foo existed at clone, deleted upstream
	_ = dir
	res, err := Prune{}.Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if ok, _ := repo.LocalBranchExists(context.Background(), "x"); ok {
		_ = ok // unrelated; placeholder to keep import; remove
	}
	// origin/foo tracking ref is gone — verify via a raw rev-parse helper or
	// RemoteBranches not listing it.
}

func TestPruneNoRemotesIsNoOp(t *testing.T) {
	_, repo := newRepo(t) // no remotes configured
	res, err := Prune{}.Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("Prune no-remotes: %v", err)
	}
	if res.Changed {
		t.Fatalf("no remotes → not Changed, got %+v", res)
	}
}
```

NOTE: the exact clone helpers (`newCloneAheadOrigin`, `newCloneWithDeletedOriginFoo`)
mirror Task 1's bare-origin setup; if `smart_pull_test.go` already has an origin
helper that returns `(dir, *git.Repo)` bound to the clone, reuse/extend it rather
than duplicating. For the prune assertion, use `RemoteBranches(ctx)` (chunk 1
verb) and assert `origin/foo` is absent — cleaner than raw rev-parse.

- [ ] **Step 2: Run them — expect failure**

Run: `go test ./internal/engine/ -run 'TestFetch|TestPrune'`
Expected: build failure — `Fetch`/`Prune` undefined.

- [ ] **Step 3: Implement `Fetch`**

Create `internal/engine/fetch.go`:

```go
package engine

import (
	"context"

	"github.com/gigagit/gg/internal/repogate"
)

// Fetch updates tracking refs for every configured remote. RefWrite: it changes
// refs, not the working tree.
type Fetch struct{}

func (op Fetch) LockMode() repogate.Mode { return repogate.RefWrite }

func (op Fetch) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "fetching all remotes"})
	if err := deps.Repo.FetchAll(ctx); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "fetched all remotes", Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = Fetch{}
```

- [ ] **Step 4: Implement `Prune`**

Create `internal/engine/prune.go`:

```go
package engine

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/repogate"
)

// Prune drops tracking refs for branches deleted upstream, across all remotes.
// RefWrite: refs only.
type Prune struct{}

func (op Prune) LockMode() repogate.Mode { return repogate.RefWrite }

func (op Prune) Run(ctx context.Context, deps OpDeps) (Result, error) {
	names, err := deps.Repo.RemoteNames(ctx)
	if err != nil {
		return Result{}, err
	}
	if len(names) == 0 {
		return Result{Summary: "no remotes to prune"}, nil
	}
	deps.emit(ctx, Progress{Step: "pruning", Detail: strings.Join(names, " ")})
	if err := deps.Repo.PruneRemotes(ctx, names...); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "pruned remotes: " + strings.Join(names, " "), Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = Prune{}
```

- [ ] **Step 5: Run op tests — expect green**

Run: `go test ./internal/engine/ -run 'TestFetch|TestPrune' -v` → all PASS.
(Remove any placeholder lines from the test sketches before finalizing.)

- [ ] **Step 6: Commit**

```bash
git add internal/engine/fetch.go internal/engine/prune.go internal/engine/fetch_test.go internal/engine/prune_test.go
git commit -m "feat(engine): Fetch (all remotes) and Prune ops (RefWrite)"
```

---

### Task 3: e2e harness — `stdout_excludes` + `branch_delete` build step

**Files:**
- Modify: `e2e/scenario.go` (`Run.StdoutExcludes` + `PresentExcluded`; `Step.BranchDelete` + `kind()`)
- Modify: `e2e/builder.go` (`runSteps` case)
- Modify: `e2e/harness_test.go` (assert excludes)
- Test: `e2e/scenario_test.go` (append)

**Interfaces:**
- Produces: `Run.StdoutExcludes []string`, `func (r Run) PresentExcluded(out string) []string`, the `branch_delete` step kind.

- [ ] **Step 1: Write failing unit tests**

Append to `e2e/scenario_test.go`:

```go
func TestRunPresentExcluded(t *testing.T) {
	r := Run{StdoutExcludes: []string{"origin/foo"}}
	if bad := r.PresentExcluded("origin/main\n"); len(bad) != 0 {
		t.Fatalf("absent → none, got %v", bad)
	}
	bad := r.PresentExcluded("origin/foo\norigin/main\n")
	if len(bad) != 1 || bad[0] != "origin/foo" {
		t.Fatalf("present → reported, got %v", bad)
	}
}

func TestLoadScenarioParsesStdoutExcludesAndBranchDelete(t *testing.T) {
	path := writeScenario(t, `name = "x"
[input]
steps = [{ write = "f.txt", content = "x\n" }, { commit = "c1" }, { branch = "foo" }, { branch_delete = "foo" }]
[[run]]
cmd = ["remote", "ls"]
exit = 0
stdout_excludes = ["origin/foo"]
[expect]
`)
	sc, err := LoadScenario(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(sc.Runs[0].StdoutExcludes) != 1 || sc.Runs[0].StdoutExcludes[0] != "origin/foo" {
		t.Fatalf("StdoutExcludes not parsed: %+v", sc.Runs)
	}
}
```

- [ ] **Step 2: Run them — expect failure**

Run: `go test ./e2e/ -run 'TestRunPresentExcluded|TestLoadScenarioParsesStdoutExcludesAndBranchDelete'`
Expected: build failure (`PresentExcluded`/`StdoutExcludes` undefined) or a `branch_delete` kind error from validation.

- [ ] **Step 3: Add `StdoutExcludes` + `PresentExcluded`**

In `e2e/scenario.go`, extend `Run` and add the helper (next to `MissingStdout`):

```go
	StdoutExcludes []string `toml:"stdout_excludes"`
```
```go
// PresentExcluded returns the StdoutExcludes substrings that wrongly appear in out.
func (r Run) PresentExcluded(out string) []string {
	var present []string
	for _, bad := range r.StdoutExcludes {
		if strings.Contains(out, bad) {
			present = append(present, bad)
		}
	}
	return present
}
```

- [ ] **Step 4: Add the `branch_delete` build step**

In `e2e/scenario.go`, add to `Step`:

```go
	BranchDelete string `toml:"branch_delete"`
```
In `Step.kind()`, add a branch alongside the others (return `"branch_delete"`
when `s.BranchDelete != ""`; include it in the "exactly one field" validation so
it doesn't read as ambiguous). In `e2e/builder.go` `runSteps`, add:

```go
		case "branch_delete":
			b.git(t, dir, "branch", "-D", st.BranchDelete)
```

- [ ] **Step 5: Honor `stdout_excludes` in the run loop**

In `e2e/harness_test.go`, after the `MissingStdout` check:

```go
				if bad := run.PresentExcluded(stdout.String()); len(bad) > 0 {
					t.Fatalf("run[%d] gg %s: stdout unexpectedly contains %v\nstdout:\n%s",
						i, strings.Join(run.Cmd, " "), bad, stdout.String())
				}
```

- [ ] **Step 6: Run unit tests + full e2e — expect green**

Run: `go test ./e2e/ -run 'TestRunPresentExcluded|TestLoadScenarioParsesStdoutExcludesAndBranchDelete'` → PASS.
Run: `go test ./e2e/` → ok (existing scenarios unaffected).

- [ ] **Step 7: Commit**

```bash
git add e2e/scenario.go e2e/builder.go e2e/harness_test.go e2e/scenario_test.go
git commit -m "feat(e2e): stdout_excludes assertion + branch_delete build step"
```

---

### Task 4: CLI `gg remote fetch` / `gg remote prune` + scenarios

**Files:**
- Modify: `internal/cli/remote.go` (`cmdRemote` subcommands)
- Test: `internal/cli/remote_test.go` (append)
- Create: `e2e/scenarios/s49_remote_fetch.toml`, `s50_remote_prune.toml`

**Interfaces:**
- Consumes: `engine.Fetch{}`, `engine.Prune{}` (Task 2); `stdout_excludes` + `branch_delete` (Task 3)

- [ ] **Step 1: Write failing cli tests**

Append to `internal/cli/remote_test.go` (reuse `cloneWithRemoteFoo` from
`ops_test.go`, and `gitIn`/`runCLI`):

```go
func TestRemoteFetchUpdatesTrackingRefs(t *testing.T) {
	clone := cloneWithRemoteFoo(t)
	// Advance origin/foo by pushing a new commit from a second clone of the same origin.
	// Simpler: delete then recreate is overkill; assert exit 0 + that a later checkout sees foo.
	if code, _, errb := runCLI(t, clone, "remote", "fetch"); code != 0 {
		t.Fatalf("remote fetch exit = %d (stderr: %s)", code, errb)
	}
	// foo is fetchable as a local branch after fetch.
	if code, _, errb := runCLI(t, clone, "checkout", "origin/foo"); code != 0 {
		t.Fatalf("checkout after fetch exit = %d (stderr: %s)", code, errb)
	}
}

func TestRemotePruneDropsDeletedRef(t *testing.T) {
	clone := cloneWithRemoteFoo(t)
	// Delete foo on the origin this clone points at.
	origin := gitInOut(t, clone, "config", "--get", "remote.origin.url")
	gitIn(t, strings.TrimSpace(origin), "branch", "-D", "foo") // origin is a bare/working path
	if code, _, errb := runCLI(t, clone, "remote", "prune"); code != 0 {
		t.Fatalf("remote prune exit = %d (stderr: %s)", code, errb)
	}
	_, out, _ := runCLI(t, clone, "remote", "ls")
	if strings.Contains(out, "origin/foo") {
		t.Fatalf("origin/foo should be pruned:\n%s", out)
	}
}
```

NOTE: `cloneWithRemoteFoo` clones from a path-based bare `origin.git`; the
`remote.origin.url` is that path, so deleting `foo` there works. Add a tiny
`gitInOut(t, dir, args...) string` if the package lacks a stdout-returning git
helper (or reuse `runGit`). If wiring the origin-path deletion is awkward, prefer
asserting prune purely through the e2e scenario (Step 4) and keep the cli test to
`remote fetch` exit-0 + `remote prune` exit-0.

- [ ] **Step 2: Run — expect failure**

Run: `go test ./internal/cli/ -run 'TestRemoteFetch|TestRemotePrune'`
Expected: failure — `fetch`/`prune` are unknown `remote` subcommands.

- [ ] **Step 3: Add the subcommands**

In `internal/cli/remote.go`, extend `cmdRemote` (it currently handles `ls`/`list`);
add `engine` + `context` imports if absent:

```go
func cmdRemote(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 0 || args[0] == "ls" || args[0] == "list":
		return cmdRemoteList(svc, stdout, stderr)
	case args[0] == "fetch":
		res, err := runOperation(context.Background(), svc, engine.Fetch{}, cliDecider{}, stderr)
		return finish(res, err, stdout, stderr)
	case args[0] == "prune":
		res, err := runOperation(context.Background(), svc, engine.Prune{}, cliDecider{}, stderr)
		return finish(res, err, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "remote: unknown subcommand %q (try: ls, fetch, prune)\n", args[0])
		return 2
	}
}
```

- [ ] **Step 4: Run cli tests — expect green**

Run: `go test ./internal/cli/ -run 'TestRemoteFetch|TestRemotePrune'` → PASS.

- [ ] **Step 5: Write the two scenarios**

Create `e2e/scenarios/s49_remote_fetch.toml` (origin advances `foo` after the
clone; fetch; then a switch lands on the new commit):

```toml
name = "remote fetch: update tracking refs from all remotes"

[input]
steps = []

[input.origin]
steps = [
  { write = "f.txt", content = "v1\n" },
  { commit = "c1" },
  { branch = "foo" },
  { switch = "foo" },
  { write = "g.txt", content = "foo\n" },
  { commit = "foo-c2" },
  { switch = "main" },
]
after = [
  { switch = "foo" },
  { write = "g.txt", content = "foo2\n" },
  { commit = "foo-c3" },
  { switch = "main" },
]

[[run]]
cmd  = ["remote", "fetch"]
exit = 0
[[run]]
cmd  = ["checkout", "origin/foo", "-s"]
exit = 0

[expect]
branch = "foo"

[[expect.log]]
subjects = ["foo-c3", "foo-c2", "c1"]
```

Create `e2e/scenarios/s50_remote_prune.toml` (origin deletes `foo` after the
clone; prune; `remote ls` excludes it):

```toml
name = "remote prune: drop tracking refs for upstream-deleted branches"

[input]
steps = []

[input.origin]
steps = [
  { write = "f.txt", content = "v1\n" },
  { commit = "c1" },
  { branch = "foo" },
  { switch = "foo" },
  { write = "g.txt", content = "foo\n" },
  { commit = "foo-c2" },
  { switch = "main" },
]
after = [
  { branch_delete = "foo" },
]

[[run]]
cmd  = ["remote", "prune"]
exit = 0
[[run]]
cmd             = ["remote", "ls"]
exit            = 0
stdout_contains = ["origin/main"]
stdout_excludes = ["origin/foo"]

[expect]
branch = "main"
clean  = true
```

NOTE on s50: `after` runs on the **origin** repo (`buildSandbox` → `runSteps(sc.Input.Origin.After, sb.OriginDir)`), so `branch_delete=foo` deletes origin's `foo`; `git remote prune` then drops the clone's `refs/remotes/origin/foo`. Confirm origin isn't checked out on `foo` at delete time (origin ends its `steps` on `main`, so `branch -D foo` is safe).

- [ ] **Step 6: Run the scenarios**

Run: `go test ./e2e/ -run 'TestScenarios/s49_remote_fetch|TestScenarios/s50_remote_prune' -v`
Expected: both PASS. (Renumber if `ls e2e/scenarios/` shows a collision above `s48`.)

- [ ] **Step 7: Commit**

```bash
git add internal/cli/remote.go internal/cli/remote_test.go e2e/scenarios/s49_remote_fetch.toml e2e/scenarios/s50_remote_prune.toml
git commit -m "feat(cli): gg remote fetch / prune; e2e scenarios"
```

---

### Task 5: TUI — `f` fetch + Prune menu action

**Files:**
- Modify: `internal/tui/model.go` (`case "f"` on the Remotes tab)
- Modify: `internal/tui/avail.go` (a `canFetchRemotes` predicate)
- Modify: `internal/tui/footer.go` (`fetch` binding)
- Modify: `internal/tui/action_menu.go` (a Prune row, appended like `shelfTabRows`)
- Modify: `internal/tui/help.go` (Remotes section)
- Test: `internal/tui/nav_test.go` + `action_menu_test.go` (append)

**Interfaces:**
- Consumes: `engine.Fetch{}`, `engine.Prune{}`; `panelRemotes`, `m.startOp`.

- [ ] **Step 1: Write failing tui tests**

Append to `internal/tui/nav_test.go`:

```go
func TestFetchKeyOnRemotesTabStartsFetch(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelRemotes
	u, _ := m.Update(keyMsg("f"))
	if !u.(Model).running {
		t.Fatal("f on the Remotes tab should start Fetch")
	}
	// f elsewhere does not start an op.
	m2 := loadedModel(t)
	m2.focus = panelBranches
	u2, _ := m2.Update(keyMsg("f"))
	if u2.(Model).running {
		t.Fatal("f on the Branches tab must not start Fetch")
	}
}
```

Append to `internal/tui/action_menu_test.go` (match its existing style for
invoking `availableActions`):

```go
func TestPruneRowOnRemotesTab(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelRemotes
	found := false
	for _, r := range availableActions(m) {
		if r.id == "prune-remotes" {
			found = true
		}
	}
	if !found {
		t.Fatal("Remotes tab . menu should offer Prune")
	}
}
```

- [ ] **Step 2: Run — expect failure**

Run: `go test ./internal/tui/ -run 'TestFetchKeyOnRemotesTab|TestPruneRowOnRemotesTab'`
Expected: failures (no `f` handler / no prune row).

- [ ] **Step 3: Add `canFetchRemotes` + the `f` handler**

In `internal/tui/avail.go`:

```go
// canFetchRemotes gates f / Prune on the Remotes tab.
func (m Model) canFetchRemotes() bool {
	return m.focus == panelRemotes && m.opsIdle()
}
```

In `internal/tui/model.go`, add a `case "f":` to the main key switch (the one at
~line 411; `f` is otherwise unbound at base — confirm no existing base `case "f"`):

```go
		case "f":
			if m.canFetchRemotes() {
				return m.startOp(engine.Fetch{})
			}
```

- [ ] **Step 4: Footer binding**

In `internal/tui/footer.go` `contextBindings`, add (window scope — acts on the tab, not a row):

```go
	{"fetch", "f", "[f]etch", func(m Model) bool { return m.canFetchRemotes() }, scopeWindow},
```

- [ ] **Step 5: Prune menu row**

In `internal/tui/action_menu.go`, add a method and append it in `availableActions`
right before the `return` (mirroring `shelfTabRows`):

```go
// remotePruneRow offers Prune on the Remotes tab (no dedicated key).
func (m Model) remotePruneRow() (actionRow, bool) {
	if !m.canFetchRemotes() {
		return actionRow{}, false
	}
	return actionRow{
		id:    "prune-remotes",
		label: "Prune remotes (drop deleted branches)",
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.startOp(engine.Prune{}) },
	}, true
}
```
In `availableActions`, before the final `return out`:
```go
	if r, ok := m.remotePruneRow(); ok {
		out = append(out, r)
	}
```
(Confirm `engine` is imported in `action_menu.go`; add if needed.)

- [ ] **Step 6: Help copy**

In `internal/tui/help.go`, under the Remotes section, add:

```go
		r("f", "fetch all remotes (updates tracking refs and the behind-counts)"),
		r(".", "Prune (.-menu): drop tracking refs for branches deleted upstream"),
```

- [ ] **Step 7: Run tui tests + package — expect green**

Run: `go test ./internal/tui/ -run 'TestFetchKeyOnRemotesTab|TestPruneRowOnRemotesTab'` → PASS.
Run: `go test ./internal/tui/` → ok (update any footer/help golden test that enumerates Remotes bindings).

- [ ] **Step 8: Commit**

```bash
git add internal/tui/model.go internal/tui/avail.go internal/tui/footer.go internal/tui/action_menu.go internal/tui/help.go internal/tui/nav_test.go internal/tui/action_menu_test.go
git commit -m "feat(tui): f fetch + Prune . menu action on the Remotes tab"
```

---

### Task 6: agentskill + README + CHANGELOG + full gate

**Files:** `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go`, `.claude/skills/using-gg/SKILL.md`, `README.md`, `CHANGELOG.md`

- [ ] **Step 1: agentskill doc**

In `internal/agentskill/using-gg.md`, extend the `gg remote` entry:

```markdown
- `gg remote ls | fetch | prune` — `ls` lists remote-tracking branches; `fetch`
  updates tracking refs for all remotes (`git fetch --all`); `prune` drops
  tracking refs for branches deleted upstream.
```

- [ ] **Step 2: Bump version + refresh dogfood copy**

Edit `internal/agentskill/agentskill.go`: `Version` 12 → 13.
Run: `go build -o /tmp/gg ./cmd/gg && /tmp/gg init --update` (refreshes
`.claude/skills/using-gg/SKILL.md`).
Run: `go test ./internal/agentskill/` → PASS.

- [ ] **Step 3: README + CHANGELOG**

`README.md` CLI verbs — replace the `gg remote ls` line with:
```markdown
gg remote ls | fetch | prune    # list remote branches / fetch all / prune deleted
```
`CHANGELOG.md` under `### Added`:
```markdown
- CLI/TUI: **fetch + prune** for remote-tracking refs. `gg remote fetch`
  (`git fetch --all`) and `gg remote prune` (drop refs for branches deleted
  upstream); on the TUI Remotes tab, `f` fetches and the `.` menu offers Prune.
  Both refresh the Remotes list and the Branches `(↓N)` behind-counts.
```

- [ ] **Step 4: Full gate**

Run: `./test.sh unit` → green. Run: `./test.sh race` → green (incl. s49/s50).

- [ ] **Step 5: Commit**

```bash
git add internal/agentskill/using-gg.md internal/agentskill/agentskill.go .claude/skills/using-gg/SKILL.md README.md CHANGELOG.md
git commit -m "docs: document gg remote fetch/prune; agentskill v13"
```

---

## Self-Review notes

- **Spec coverage:** verbs (T1), Fetch+Prune ops RefWrite (T2), `stdout_excludes` + the origin-delete step (T3, resolving the spec's open Risk with a targeted `branch_delete` step), CLI (T4), TUI `f`+Prune-menu (T5), docs (T6). All mapped.
- **Spec deviation (intentional):** FetchAll is non-streaming — `Runner.Stream` only scans stdout, but `git fetch` progress is on stderr, so streaming adds nothing. Recorded in Global Constraints.
- **Dependency order:** T4's prune scenario needs T2 (ops) + T3 (`stdout_excludes` + `branch_delete`); both precede it. T5 needs T2.
- **Type consistency:** `engine.Fetch{}`/`engine.Prune{}`, `Run.StdoutExcludes`/`PresentExcluded`, `canFetchRemotes`, `remotePruneRow`/id `"prune-remotes"` defined once and reused.
- **Watch-items:** `f` must be unbound at base before adding (T5 confirms); `branch_delete` must join `kind()`'s exclusivity check (T3); agentskill v13 + dogfood refresh (T6); scenario renumber if `ls` shows collisions (T4).
