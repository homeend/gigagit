# Push Fetch-Refspec Mapping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** After a push whose branch isn't covered by the remote's fetch refspec (single-branch/depth monorepo clones), gg asks add/skip and, on add, writes a per-branch mapping + fetches that one branch so the ↓↑ tip markers work; a notification-center notice fixes pre-existing broken branches the same way.

**Architecture:** A post-push probe + Decider fork inside `engine.Push` (all three success exits), backed by four new one-invocation git verbs; a sibling `AddFetchMappings` op behind a new `RepoHealth`-driven notice; CLI `--map/--no-map` flags pre-answer the decision.

**Tech Stack:** Go 1.26, real-git tests in `t.TempDir()`, Bubble Tea TUI, declarative e2e TOML harness.

**Spec:** `docs/superpowers/specs/2026-07-25-push-fetch-mapping-design.md`

## Global Constraints

- All commands run in the feature worktree: `/mnt/t/others/gigagit.worktrees/feat-push-fetch-mapping` (branch `feat/push-fetch-mapping`). `cd` there first in every session.
- Engine English strings (`Result.Summary`, `Progress`, `DecisionRequest.Prompt`) are the stable CLI/agent surface — build them ONLY via `WithSummary`/`AppendSummary`/`Progressf`/`PromptReq` with English literal formats (AST gates enforce).
- Decision option VALUES are English protocol: `"add"`, `"skip"` — never translated in comparisons; only labels translate via `optionDisplayName`.
- Every new i18n key must exist in ALL FOUR bundles: `internal/i18n/lang/{ja,ko,zh,ru}.toml`. Gates: `TestEngineProseKeysInBundles`, `options_vocab_test.go`, `i18n_scan_test.go`.
- Per-branch mapping only (`+refs/heads/X:refs/remotes/<remote>/X`); never widen to the wildcard.
- A failure after a successful push must NEVER fail the push (append a summary note instead).
- One git verb = one invocation, argv via `gitcmd.New(...)`.
- TDD; commit at the end of every task; run `go vet ./... && gofmt -l .` before each commit (must be clean).
- End every commit message with:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`
  `Claude-Session: https://claude.ai/code/session_01F2WLH1cCf6LUgUUaioa1pd`

---

### Task 1: git verbs — ConfigAdd, ConfigGetAll, ConfigGetRegexp, FetchBranches

**Files:**
- Modify: `internal/git/config.go` (append three verbs after `ConfigUnset`)
- Modify: `internal/git/sync.go` (append `FetchBranches` after `FetchAll`)
- Test: `internal/git/config_test.go` (append), `internal/git/sync_fetchbranches_test.go` (create)

**Interfaces:**
- Consumes: `*git.Repo`, `gitcmd.New`, `ConfigScope` (existing), test helper `newTestRepo(t) (string, gitexec.Runner)` from `internal/git/repo_test.go`.
- Produces (later tasks call these exact signatures):
  - `func (r *Repo) ConfigAdd(ctx context.Context, scope ConfigScope, key, value string) error`
  - `func (r *Repo) ConfigGetAll(ctx context.Context, key string) ([]string, error)` — missing key → `(nil, nil)`
  - `func (r *Repo) ConfigGetRegexp(ctx context.Context, pattern string) ([][2]string, error)` — no match → `(nil, nil)`; each element is `[key, value]`
  - `func (r *Repo) FetchBranches(ctx context.Context, remote string, branches []string) error`

- [ ] **Step 1: Write the failing tests**

Append to `internal/git/config_test.go`:

```go
func TestConfigAddAndGetAllMultivar(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	// Missing key: empty, no error (the ConfigGet exit-1 pattern).
	vals, err := repo.ConfigGetAll(ctx, "remote.origin.fetch")
	if err != nil || len(vals) != 0 {
		t.Fatalf("missing key: vals=%v err=%v", vals, err)
	}

	if err := repo.ConfigAdd(ctx, ConfigLocal, "remote.origin.fetch", "+refs/heads/main:refs/remotes/origin/main"); err != nil {
		t.Fatalf("add 1: %v", err)
	}
	if err := repo.ConfigAdd(ctx, ConfigLocal, "remote.origin.fetch", "+refs/heads/feat:refs/remotes/origin/feat"); err != nil {
		t.Fatalf("add 2: %v", err)
	}
	vals, err = repo.ConfigGetAll(ctx, "remote.origin.fetch")
	if err != nil {
		t.Fatalf("get-all: %v", err)
	}
	want := []string{
		"+refs/heads/main:refs/remotes/origin/main",
		"+refs/heads/feat:refs/remotes/origin/feat",
	}
	if len(vals) != 2 || vals[0] != want[0] || vals[1] != want[1] {
		t.Fatalf("get-all = %v, want %v", vals, want)
	}
}

func TestConfigGetRegexp(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	// No match: empty, no error.
	kvs, err := repo.ConfigGetRegexp(ctx, `^branch\.`)
	if err != nil || len(kvs) != 0 {
		t.Fatalf("no match: kvs=%v err=%v", kvs, err)
	}

	if err := repo.ConfigSet(ctx, ConfigLocal, "branch.feat.remote", "origin"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ConfigSet(ctx, ConfigLocal, "branch.feat.merge", "refs/heads/feat"); err != nil {
		t.Fatal(err)
	}
	kvs, err = repo.ConfigGetRegexp(ctx, `^branch\.`)
	if err != nil || len(kvs) != 2 {
		t.Fatalf("kvs=%v err=%v", kvs, err)
	}
	// git lowercases section+key; value is verbatim.
	if kvs[0][0] != "branch.feat.remote" || kvs[0][1] != "origin" {
		t.Fatalf("kvs[0] = %v", kvs[0])
	}
	if kvs[1][0] != "branch.feat.merge" || kvs[1][1] != "refs/heads/feat" {
		t.Fatalf("kvs[1] = %v", kvs[1])
	}
}
```

Create `internal/git/sync_fetchbranches_test.go`:

```go
package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

// TestFetchBranches: a clone with a NARROWED refspec + FetchBranches for one
// branch updates exactly that branch's remote-tracking ref.
func TestFetchBranches(t *testing.T) {
	root := t.TempDir()
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL="+filepath.Join(root, "gitconfig"),
			"GIT_CONFIG_SYSTEM="+os.DevNull)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	origin := filepath.Join(root, "origin.git")
	run(root, "init", "--bare", "-b", "main", origin)
	seed := filepath.Join(root, "seed")
	run(root, "clone", origin, seed)
	os.WriteFile(filepath.Join(seed, "a.txt"), []byte("1\n"), 0o644)
	run(seed, "add", "-A")
	run(seed, "commit", "-m", "init")
	run(seed, "push", "origin", "main")
	run(seed, "switch", "-c", "feat")
	run(seed, "commit", "--allow-empty", "-m", "feat1")
	run(seed, "push", "origin", "feat")

	local := filepath.Join(root, "local")
	run(root, "clone", "--single-branch", origin, local)
	repo := &Repo{Runner: gitexec.NewExecRunner("git", local, observ.NewRing(50))}
	ctx := context.Background()

	// Map feat, then fetch ONLY feat.
	if err := repo.ConfigAdd(ctx, ConfigLocal, "remote.origin.fetch", "+refs/heads/feat:refs/remotes/origin/feat"); err != nil {
		t.Fatal(err)
	}
	if err := repo.FetchBranches(ctx, "origin", []string{"feat"}); err != nil {
		t.Fatalf("FetchBranches: %v", err)
	}
	refs, err := repo.ForEachRef(ctx, "refs/remotes/origin/feat")
	if err != nil || len(refs) != 1 {
		t.Fatalf("tracking ref after fetch: refs=%v err=%v", refs, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/git -run 'TestConfigAddAndGetAllMultivar|TestConfigGetRegexp|TestFetchBranches' -v`
Expected: compile FAIL — `repo.ConfigGetAll undefined` (and the others).

- [ ] **Step 3: Implement the verbs**

Append to `internal/git/config.go`:

```go
// ConfigAdd appends one value to a (possibly multi-valued) config key at the
// given scope (Local or Global only; an Effective/unknown scope falls back to
// Local). `git config --add` never replaces existing values — the fetch-
// refspec mapping use case must not clobber a user's refspec list.
func (r *Repo) ConfigAdd(ctx context.Context, scope ConfigScope, key, value string) error {
	f, ok := scope.flag()
	if !ok {
		f = "--local"
	}
	b := gitcmd.New("config").Arg(f, "--add", key, value)
	_, err := r.Runner.Run(ctx, "git config --add", b.ToArgv())
	return err
}

// ConfigGetAll reads every value of a (possibly multi-valued) key at the
// effective scope, in definition order. A key that is unset returns
// (nil, nil): `git config --get-all` exits 1 for a missing key (the
// ConfigGet exit-1 pattern).
func (r *Repo) ConfigGetAll(ctx context.Context, key string) ([]string, error) {
	b := gitcmd.New("config").Arg("--get-all", key)
	res, err := r.Runner.Run(ctx, "git config --get-all", b.ToArgv())
	if err != nil {
		if res.ExitCode == 1 {
			return nil, nil // key unset
		}
		return nil, err
	}
	var out []string
	for _, ln := range strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n") {
		if ln != "" {
			out = append(out, ln)
		}
	}
	return out, nil
}

// ConfigGetRegexp lists every effective-scope key matching pattern as
// [key, value] pairs. -z framing (records NUL-terminated, key separated
// from value by a newline) survives multi-line values — the
// ConfigListScoped precedent. No match returns (nil, nil): exit 1, the
// ConfigGet pattern.
func (r *Repo) ConfigGetRegexp(ctx context.Context, pattern string) ([][2]string, error) {
	b := gitcmd.New("config").Arg("-z", "--get-regexp", pattern)
	res, err := r.Runner.Run(ctx, "git config --get-regexp", b.ToArgv())
	if err != nil {
		if res.ExitCode == 1 {
			return nil, nil // no key matched
		}
		return nil, err
	}
	var out [][2]string
	for _, rec := range strings.Split(res.Stdout, "\x00") {
		if rec == "" {
			continue
		}
		key, val, _ := strings.Cut(rec, "\n")
		out = append(out, [2]string{key, val})
	}
	return out, nil
}
```

Append to `internal/git/sync.go` (after `FetchAll`):

```go
// FetchBranches updates the remote-tracking refs for exactly the named
// branches (`git fetch <remote> <branch>…`). Callers guarantee non-empty
// branches (engine ops no-op before calling). --no-write-fetch-head matches
// Fetch's concurrency contract.
func (r *Repo) FetchBranches(ctx context.Context, remote string, branches []string) error {
	b := gitcmd.New("fetch").Arg("--no-write-fetch-head", remote)
	for _, br := range branches {
		b = b.Arg(br)
	}
	_, err := r.Runner.Run(ctx, "git fetch (branches)", b.ToArgv())
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/git -run 'TestConfigAddAndGetAllMultivar|TestConfigGetRegexp|TestFetchBranches' -v`
Expected: PASS (3 tests). Then `go test ./internal/git` — all PASS.

- [ ] **Step 5: Commit**

```bash
go vet ./internal/git && gofmt -l internal/git   # must print nothing
git add internal/git
git commit -m "feat(git): ConfigAdd/ConfigGetAll/ConfigGetRegexp/FetchBranches verbs"
```

---

### Task 2: engine — post-push probe + fetch_mapping.add decision

**Files:**
- Create: `internal/engine/fetch_mapping.go`
- Modify: `internal/engine/gitops.go` (three interface additions next to `ConfigSet`/`ConfigUnset` at lines ~109-110)
- Modify: `internal/engine/ops_basic.go` (the three push success exits)
- Test: `internal/engine/fetch_mapping_test.go` (create)

**Interfaces:**
- Consumes: Task 1 verbs; existing `deps.decide`, `PromptReq`, `Result.AppendSummary`, `GitOps.ForEachRef(ctx, prefix) ([]model.RefInfo, error)`, `MapDecider`, test helper `newRepo(t) (string, *git.Repo)` from `ops_basic_test.go`.
- Produces:
  - `const FetchMappingDecisionID = "fetch_mapping.add"` (Task 6 CLI policy key)
  - `func ensureRemoteTracking(ctx context.Context, deps OpDeps, remote, branch string, res Result) Result` (package-private; called from the three `ops_basic.go` sites)

- [ ] **Step 1: Write the failing tests**

Create `internal/engine/fetch_mapping_test.go`:

```go
package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

// narrowClone builds origin(bare, main pushed) + a --single-branch clone on a
// new local branch "feat" with one commit — the fetch refspec maps only main.
func narrowClone(t *testing.T) *git.Repo {
	t.Helper()
	root := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL="+filepath.Join(root, "gitconfig"),
			"GIT_CONFIG_SYSTEM="+os.DevNull)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	origin := filepath.Join(root, "origin.git")
	run(root, "init", "--bare", "-b", "main", origin)
	seed := filepath.Join(root, "seed")
	run(root, "clone", origin, seed)
	os.WriteFile(filepath.Join(seed, "a.txt"), []byte("1\n"), 0o644)
	run(seed, "add", "-A")
	run(seed, "commit", "-m", "init")
	run(seed, "push", "origin", "main")
	local := filepath.Join(root, "local")
	run(root, "clone", "--single-branch", origin, local)
	run(local, "switch", "-c", "feat")
	run(local, "commit", "--allow-empty", "-m", "feat1")
	return &git.Repo{Runner: gitexec.NewExecRunner("git", local, observ.NewRing(50))}
}

func TestPushUnmappedBranchAddMapsAndFetches(t *testing.T) {
	repo := narrowClone(t)
	ctx := context.Background()
	ch := make(chan Event, 64)
	res, err := Push{Remote: "origin", Branch: "feat", SetUpstream: true}.Run(ctx,
		OpDeps{Repo: repo, Events: ch, Decider: MapDecider{FetchMappingDecisionID: "add"}})
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if !res.Changed {
		t.Fatal("push should report Changed")
	}
	if !strings.Contains(res.Summary, "mapped origin/feat for tracking") {
		t.Fatalf("summary = %q", res.Summary)
	}
	specs, _ := repo.ConfigGetAll(ctx, "remote.origin.fetch")
	want := "+refs/heads/feat:refs/remotes/origin/feat"
	found := false
	for _, s := range specs {
		if s == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("refspec %q not in %v", want, specs)
	}
	refs, err := repo.ForEachRef(ctx, "refs/remotes/origin/feat")
	if err != nil || len(refs) != 1 {
		t.Fatalf("tracking ref: %v err=%v", refs, err)
	}
}

func TestPushUnmappedBranchSkipLeavesConfigAlone(t *testing.T) {
	repo := narrowClone(t)
	ctx := context.Background()
	res, err := Push{Remote: "origin", Branch: "feat", SetUpstream: true}.Run(ctx,
		OpDeps{Repo: repo, Events: make(chan Event, 64), Decider: MapDecider{FetchMappingDecisionID: "skip"}})
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if res.Summary != "pushed" {
		t.Fatalf("summary = %q, want plain %q", res.Summary, "pushed")
	}
	if refs, _ := repo.ForEachRef(ctx, "refs/remotes/origin/feat"); len(refs) != 0 {
		t.Fatalf("tracking ref should not exist: %v", refs)
	}
}

func TestPushUnmappedBranchDeciderErrorSkips(t *testing.T) {
	repo := narrowClone(t)
	// MapDecider with no entry errors (ErrDecisionRequired) → must skip, not fail.
	res, err := Push{Remote: "origin", Branch: "feat", SetUpstream: true}.Run(context.Background(),
		OpDeps{Repo: repo, Events: make(chan Event, 64), Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("push must not fail on a decider error after success: %v", err)
	}
	if res.Summary != "pushed" {
		t.Fatalf("summary = %q", res.Summary)
	}
}

func TestPushMappedBranchNeverAsks(t *testing.T) {
	dir, repo := newRepo(t)
	ctx := context.Background()
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	origin := filepath.Join(root, "origin.git")
	run("init", "--bare", "-b", "main", origin)
	run("remote", "add", "origin", origin) // default wildcard refspec
	failing := DeciderFunc(func(context.Context, DecisionRequest) (DecisionResponse, error) {
		t.Fatal("decision must not fire for a mapped branch")
		return DecisionResponse{}, nil
	})
	res, err := Push{Remote: "origin", Branch: "main", SetUpstream: true}.Run(ctx,
		OpDeps{Repo: repo, Events: make(chan Event, 64), Decider: failing})
	if err != nil || res.Summary != "pushed" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/engine -run 'TestPushUnmapped|TestPushMapped' -v`
Expected: compile FAIL — `FetchMappingDecisionID undefined`.

- [ ] **Step 3: Add the GitOps methods and the helper**

In `internal/engine/gitops.go`, directly after the `ConfigUnset` line (~110):

```go
	ConfigAdd(ctx context.Context, scope git.ConfigScope, key, value string) error
	ConfigGetAll(ctx context.Context, key string) ([]string, error)
	FetchBranches(ctx context.Context, remote string, branches []string) error
```

Create `internal/engine/fetch_mapping.go`:

```go
package engine

import (
	"context"
	"slices"

	"github.com/homeend/gigagit/internal/git"
)

// FetchMappingDecisionID is the post-push "this branch isn't covered by the
// fetch refspec" fork: options "add" (write a per-branch mapping and fetch
// just that branch) and "skip". Raised only AFTER a successful push whose
// remote-tracking ref did not end up at the pushed tip — the single-branch/
// shallow monorepo clone case, where the ↓↑ tip markers and ahead/behind can
// never follow the branch.
const FetchMappingDecisionID = "fetch_mapping.add"

// fetchSpec is the per-branch mapping written on "add". Never the wildcard:
// widening the refspec could make the next `git fetch` a mass download on a
// monorepo remote.
func fetchSpec(remote, branch string) string {
	return "+refs/heads/" + branch + ":refs/remotes/" + remote + "/" + branch
}

// exactRefHash returns the hash of the ref exactly named ref, or "" — the
// BranchVersions over-match guard (for-each-ref patterns match on component
// boundaries, so an exact name can still return children of ref/).
func exactRefHash(refs []model.RefInfo, ref string) string {
	for _, r := range refs {
		if r.Ref == ref {
			return r.Hash
		}
	}
	return ""
}

// ensureRemoteTracking runs after a SUCCESSFUL push of branch to remote,
// before the Done event: when the remote-tracking ref did not move to the
// pushed tip (the fetch refspec does not map the branch), it forks the
// FetchMappingDecisionID decision and, on "add", writes the per-branch
// mapping + fetches only that branch (near-free — the remote just received
// our objects). Every failure path returns res with at most a summary note:
// the push already succeeded and must never be failed retroactively. A
// decider error skips (the post-create-hook convention: safe default skip).
func ensureRemoteTracking(ctx context.Context, deps OpDeps, remote, branch string, res Result) Result {
	localRef := "refs/heads/" + branch
	remoteRef := "refs/remotes/" + remote + "/" + branch
	locals, err := deps.Repo.ForEachRef(ctx, localRef)
	if err != nil {
		return res // cannot resolve the pushed tip; not worth a note
	}
	tip := exactRefHash(locals, localRef)
	if tip == "" {
		return res
	}
	remotes, err := deps.Repo.ForEachRef(ctx, remoteRef)
	if err == nil && exactRefHash(remotes, remoteRef) == tip {
		return res // mapped and current — the healthy fast path
	}
	choice, derr := deps.decide(ctx, PromptReq(FetchMappingDecisionID,
		"%s/%s is not tracked by the fetch refspec — tip markers and ahead/behind cannot follow it. Add a tracking mapping for this branch?",
		[]string{"add", "skip"}, remote, branch))
	if derr != nil || choice.Option != "add" {
		return res
	}
	key := "remote." + remote + ".fetch"
	spec := fetchSpec(remote, branch)
	have, _ := deps.Repo.ConfigGetAll(ctx, key)
	if !slices.Contains(have, spec) { // idempotent after a previous add whose fetch failed
		if err := deps.Repo.ConfigAdd(ctx, git.ConfigLocal, key, spec); err != nil {
			return res.AppendSummary("; could not add fetch mapping: %s", err.Error())
		}
	}
	deps.emit(ctx, Progress{Step: "fetching", Detail: remote + " " + branch})
	if err := deps.Repo.FetchBranches(ctx, remote, []string{branch}); err != nil {
		return res.AppendSummary("; fetch mapping added but fetch failed: %s", err.Error())
	}
	return res.AppendSummary("; mapped %s/%s for tracking", remote, branch)
}
```

Add `"github.com/homeend/gigagit/internal/model"` to the imports (used by `exactRefHash`).

In `internal/engine/ops_basic.go`, change ALL THREE push success exits. Site 1 (plain push, inside `Push.Run`):

```go
	err := deps.Repo.Push(ctx, op.Remote, op.Branch, op.SetUpstream, git.PushNoForce)
	if err == nil {
		res := ensureRemoteTracking(ctx, deps, op.Remote, op.Branch,
			Result{Changed: true}.WithSummary("pushed"))
		deps.emit(ctx, Done{Result: res})
		return res, nil
	}
```

Site 2 (forced push, `func (op Push) push`):

```go
	if err := deps.Repo.Push(ctx, op.Remote, op.Branch, op.SetUpstream, force); err != nil {
		return Result{}, err
	}
	res := ensureRemoteTracking(ctx, deps, op.Remote, op.Branch,
		Result{Changed: true}.WithSummary("pushed"))
	deps.emit(ctx, Done{Result: res})
	return res, nil
```

Site 3 (rebase-recovery re-push, end of `recoverRejected`):

```go
	if err := deps.Repo.Push(ctx, op.Remote, op.Branch, op.SetUpstream, git.PushNoForce); err != nil {
		return Result{}, err // second rejection or other error: surface, no loop
	}
	res := ensureRemoteTracking(ctx, deps, op.Remote, op.Branch,
		Result{Changed: true}.WithSummary("rebased and pushed"))
	deps.emit(ctx, Done{Result: res})
	return res, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/engine -run 'TestPushUnmapped|TestPushMapped' -v`
Expected: PASS (4 tests). Then `go test ./internal/engine` — all PASS (existing push tests use `newRepo` with no remote or a wildcard remote; the probe's healthy fast path keeps them green — if one fails, it is a real regression to fix, not to paper over).

NOTE: `go test ./internal/tui` (the engine-prose/options-vocab i18n gates) is EXPECTED to be red from this task until Task 5 adds the new bundle keys — do not add bundle entries here, and do not "fix" the gates early.

- [ ] **Step 5: Commit**

```bash
go vet ./internal/engine && gofmt -l internal/engine
git add internal/engine
git commit -m "feat(engine): post-push fetch_mapping.add decision maps unmapped branches"
```

---

### Task 3: engine — AddFetchMappings op + TUI source mapping

**Files:**
- Create: `internal/engine/add_fetch_mappings.go`
- Modify: `internal/tui/source.go` (one new `case` in `opAffectedSources`, next to `case engine.Fetch`)
- Test: `internal/engine/add_fetch_mappings_test.go` (create)

**Interfaces:**
- Consumes: Task 1 verbs via `GitOps`; `fetchSpec` from Task 2; `repogate.Mode`; test helper `narrowClone` from Task 2's test file (same package).
- Produces: `engine.AddFetchMappings{Remote string, Branches []string}` — Task 5's notice action dispatches it.

- [ ] **Step 1: Write the failing tests**

Create `internal/engine/add_fetch_mappings_test.go`:

```go
package engine

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/repogate"
)

func TestAddFetchMappingsEmptyIsNoOp(t *testing.T) {
	res, err := AddFetchMappings{Remote: "origin"}.Run(context.Background(), OpDeps{})
	if err != nil || res.Changed {
		t.Fatalf("empty: res=%+v err=%v", res, err)
	}
}

func TestAddFetchMappingsLockModeRefWrite(t *testing.T) {
	if AddFetchMappings{}.LockMode() != repogate.RefWrite {
		t.Fatal("AddFetchMappings must reserve RefWrite")
	}
}

func TestAddFetchMappingsMapsAndFetches(t *testing.T) {
	repo := narrowClone(t) // has local branch "feat", unmapped; origin lacks feat
	ctx := context.Background()
	// Push feat outside the op so origin has it (skip the mapping decision).
	if err := repo.Push(ctx, "origin", "feat", true, 0); err != nil {
		t.Fatalf("seed push: %v", err)
	}
	res, err := AddFetchMappings{Remote: "origin", Branches: []string{"feat"}}.Run(ctx,
		OpDeps{Repo: repo, Events: make(chan Event, 64)})
	if err != nil {
		t.Fatalf("op: %v", err)
	}
	if !res.Changed || res.Summary != "mapped 1 branch for tracking" {
		t.Fatalf("res = %+v", res)
	}
	if refs, _ := repo.ForEachRef(ctx, "refs/remotes/origin/feat"); len(refs) != 1 {
		t.Fatalf("tracking ref missing: %v", refs)
	}
	// Idempotent: run again — the dup guard skips the config write, fetch
	// still succeeds, summary unchanged.
	res2, err := AddFetchMappings{Remote: "origin", Branches: []string{"feat"}}.Run(ctx,
		OpDeps{Repo: repo, Events: make(chan Event, 64)})
	if err != nil || res2.Summary != "mapped 1 branch for tracking" {
		t.Fatalf("rerun: res=%+v err=%v", res2, err)
	}
	specs, _ := repo.ConfigGetAll(ctx, "remote.origin.fetch")
	n := 0
	for _, s := range specs {
		if s == fetchSpec("origin", "feat") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("mapping duplicated: %v", specs)
	}
}
```

Note: `repo.Push`'s force parameter is `git.PushForce`; `0` is `git.PushNoForce`. If the compiler complains, import `internal/git` and pass `git.PushNoForce`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/engine -run TestAddFetchMappings -v`
Expected: compile FAIL — `AddFetchMappings undefined`.

- [ ] **Step 3: Implement the op**

Create `internal/engine/add_fetch_mappings.go`:

```go
package engine

import (
	"context"
	"slices"
	"strings"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/repogate"
)

// AddFetchMappings writes a per-branch fetch-refspec mapping for each named
// branch (skipping ones already present) and then fetches exactly those
// branches, so their remote-tracking refs materialize and the ↓↑ tip markers
// / ahead-behind start working. The notification center's "narrowed fetch
// refspec" fix action. Empty Branches is a no-op (the PushTags precedent).
// Deliberately never widens to the wildcard refspec.
type AddFetchMappings struct {
	Remote   string
	Branches []string
}

var _ Operation = AddFetchMappings{}

// LockMode: writes remote-tracking refs + a config line; never index/
// worktree/HEAD.
func (op AddFetchMappings) LockMode() repogate.Mode { return repogate.RefWrite }

func (op AddFetchMappings) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if len(op.Branches) == 0 {
		return Result{Changed: false}.WithSummary("no branches to map"), nil
	}
	key := "remote." + op.Remote + ".fetch"
	have, err := deps.Repo.ConfigGetAll(ctx, key)
	if err != nil {
		return Result{}, err
	}
	for _, b := range op.Branches {
		spec := fetchSpec(op.Remote, b)
		if slices.Contains(have, spec) {
			continue // idempotent re-run (e.g. after a failed fetch)
		}
		if err := deps.Repo.ConfigAdd(ctx, git.ConfigLocal, key, spec); err != nil {
			return Result{}, err
		}
	}
	deps.emit(ctx, Progress{Step: "fetching", Detail: op.Remote + " " + strings.Join(op.Branches, " ")})
	if err := deps.Repo.FetchBranches(ctx, op.Remote, op.Branches); err != nil {
		return Result{}, err // fetching is the op's purpose; config lines stay, re-run is idempotent
	}
	var res Result
	if len(op.Branches) == 1 {
		res = Result{Changed: true}.WithSummary("mapped 1 branch for tracking")
	} else {
		res = Result{Changed: true}.WithSummary("mapped %d branches for tracking", len(op.Branches))
	}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
```

In `internal/tui/source.go`, add to `opAffectedSources` (next to `case engine.Fetch`):

```go
	case engine.AddFetchMappings:
		// New remote-tracking refs appear (Remotes panel, the feed's %D
		// decorations/↓↑ markers) and tracked branches gain ahead/behind
		// (Branches). Mapped explicitly so it doesn't fall through to "all
		// sources" and auto-fire the srcTags remote-tags network probe.
		return []sourceKey{srcBranches, srcRemotes, srcFeed}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/engine -run TestAddFetchMappings -v`
Expected: PASS (3 tests). Then `go build ./...` — compiles (the tui case references the new op).

- [ ] **Step 5: Commit**

```bash
go vet ./internal/engine ./internal/tui && gofmt -l internal/engine internal/tui
git add internal/engine internal/tui/source.go
git commit -m "feat(engine): AddFetchMappings op — per-branch refspec mappings + scoped fetch"
```

---

### Task 4: domain — RepoHealth.UnmappedBranches

**Files:**
- Modify: `internal/model/health.go` (one field)
- Modify: `internal/domain/repohealth.go` (detection + pure helper)
- Test: `internal/domain/repohealth_test.go` (append)

**Interfaces:**
- Consumes: `ConfigGetRegexp` (Task 1), existing `s.repo.Branches(ctx)` (`model.Branch.Upstream` is `%(upstream:short)`, empty when unresolvable).
- Produces: `model.RepoHealth.UnmappedBranches []string` (sorted) — Task 5's notice reads it.

- [ ] **Step 1: Write the failing test**

Append to `internal/domain/repohealth_test.go` (a pure-function test — no repo or service setup needed):

```go
func TestUnmappedFromConfig(t *testing.T) {
	cfg := [][2]string{
		{"branch.feat.remote", "origin"},
		{"branch.feat.merge", "refs/heads/feat"},
		{"branch.main.remote", "origin"},
		{"branch.main.merge", "refs/heads/main"},
		{"branch.orphan.remote", "gone-remote"}, // remote without a fetch refspec: not listed
		{"branch.orphan.merge", "refs/heads/orphan"},
		{"remote.origin.fetch", "+refs/heads/main:refs/remotes/origin/main"},
	}
	branches := []model.Branch{
		{Name: "feat", Upstream: ""},            // configured but unresolvable → listed
		{Name: "main", Upstream: "origin/main"}, // resolvable → not listed
		{Name: "orphan", Upstream: ""},          // remote has no refspec → not listed
		{Name: "local-only", Upstream: ""},      // no branch config at all → not listed
	}
	got := unmappedFromConfig(cfg, branches)
	if len(got) != 1 || got[0] != "feat" {
		t.Fatalf("unmapped = %v, want [feat]", got)
	}
}
```

Add `"github.com/homeend/gigagit/internal/model"` to the test file's imports if absent.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain -run TestUnmappedFromConfig -v`
Expected: compile FAIL — `unmappedFromConfig undefined`.

- [ ] **Step 3: Implement**

In `internal/model/health.go`, add to the `RepoHealth` struct:

```go
	// UnmappedBranches lists local branches whose upstream is configured
	// (branch.<n>.remote + .merge) but unresolvable because the remote's
	// fetch refspec does not map them (single-branch/shallow clones) — the
	// state where a push never moves the remote-tracking ref. Sorted.
	UnmappedBranches []string
```

In `internal/domain/repohealth.go`, inside the `query` closure after the `fetch.writeCommitGraph` block, add:

```go
		if kvs, err := s.repo.ConfigGetRegexp(ctx, `^(branch\..*\.(remote|merge)|remote\..*\.fetch)$`); err == nil && len(kvs) > 0 {
			if branches, err := s.repo.Branches(ctx); err == nil {
				h.UnmappedBranches = unmappedFromConfig(kvs, branches)
			}
		}
```

And append the pure helper (same file):

```go
// unmappedFromConfig joins branch tracking config against live branches:
// a branch is unmapped when branch.<n>.remote AND branch.<n>.merge are set,
// its remote HAS a fetch refspec (a remote with none is a different
// problem), and yet %(upstream:short) resolved to nothing — the narrowed-
// refspec state where pushes never move the remote-tracking ref. Branch
// names may contain dots, so config keys are parsed from both ends
// (strip "branch." prefix and ".remote"/".merge" suffix), never by Split.
func unmappedFromConfig(kvs [][2]string, branches []model.Branch) []string {
	remoteOf := map[string]string{}   // branch name → configured remote
	hasMerge := map[string]bool{}     // branch name → branch.<n>.merge present
	fetchable := map[string]bool{}    // remote name → has a fetch refspec
	for _, kv := range kvs {
		key := kv[0]
		switch {
		case strings.HasPrefix(key, "branch.") && strings.HasSuffix(key, ".remote"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "branch."), ".remote")
			remoteOf[name] = kv[1]
		case strings.HasPrefix(key, "branch.") && strings.HasSuffix(key, ".merge"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "branch."), ".merge")
			hasMerge[name] = true
		case strings.HasPrefix(key, "remote.") && strings.HasSuffix(key, ".fetch"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "remote."), ".fetch")
			fetchable[name] = true
		}
	}
	var out []string
	for _, b := range branches {
		if b.Upstream != "" {
			continue
		}
		if r := remoteOf[b.Name]; r != "" && hasMerge[b.Name] && fetchable[r] {
			out = append(out, b.Name)
		}
	}
	sort.Strings(out)
	return out
}
```

Add `"sort"` and `"strings"` is already imported; add `sort` if missing.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/domain -run 'TestUnmappedFromConfig|TestRepoHealth' -v`
Expected: PASS, existing RepoHealth tests stay green (repos without branch config get an empty `UnmappedBranches`).

- [ ] **Step 5: Commit**

```bash
go vet ./internal/domain ./internal/model && gofmt -l internal/domain internal/model
git add internal/domain internal/model
git commit -m "feat(domain): RepoHealth.UnmappedBranches — narrowed-refspec detection"
```

---

### Task 5: TUI — narrow-refspec notice + option label + all i18n keys

**Files:**
- Modify: `internal/tui/notify.go` (const + notice builder + `buildNotices` insertion)
- Modify: `internal/tui/i18n_display.go` (`optionDisplayName`: `"add"` case after `"skip"` at ~line 236)
- Modify: `internal/i18n/lang/ja.toml`, `ko.toml`, `zh.toml`, `ru.toml` (all new keys)
- Test: existing AST/bundle gates (`go test ./internal/tui -run 'TestOptionsVocab|TestEngineProse|TestI18n'` — exact names below)

**Interfaces:**
- Consumes: `model.RepoHealth.UnmappedBranches` (Task 4), `engine.AddFetchMappings` (Task 3), existing `notice`/`noticeAction` structs, `Model.startOp`, `m.refreshHealthAfterOp`, existing bundle keys "Not now (ask again next load)" and "Never for this repo".
- Produces: notice id `narrow_fetch_refspec`.

- [ ] **Step 1: Run the gates to capture the failing state**

Run: `go test ./internal/tui -run 'Test' 2>&1 | tail -20`
Expected: FAIL — `options_vocab_test.go` reports the `"add"` option value (introduced by Task 2's `PromptReq`) has no `optionDisplayName` case and is missing from bundles; `engine_prose_test` reports the new engine format strings missing from bundles. (These gates are the failing tests for this task.)

- [ ] **Step 2: Add the notice**

In `internal/tui/notify.go`, after the `noticeClipboard` const:

```go
// noticeNarrowRefspec is the "branches aren't tracked by the fetch refspec"
// recommendation's stable id.
const noticeNarrowRefspec = "narrow_fetch_refspec"
```

After `commitGraphNotice`, add:

```go
// narrowRefspecNotice fires when local branches have a configured upstream
// the fetch refspec cannot resolve (single-branch/shallow clones): pushes
// never move their remote-tracking refs, so the Commits panel's ↓↑ tip
// markers and ahead/behind silently stay stale. The fix action adds a
// per-branch mapping and fetches ONLY those branches — never the wildcard,
// which could trigger a mass download on a monorepo remote.
func narrowRefspecNotice(h model.RepoHealth) *notice {
	if len(h.UnmappedBranches) == 0 {
		return nil
	}
	title := i18n.T("%d branches aren't tracked by the fetch refspec", len(h.UnmappedBranches))
	if len(h.UnmappedBranches) == 1 {
		title = i18n.T("1 branch isn't tracked by the fetch refspec")
	}
	return &notice{
		id:      noticeNarrowRefspec,
		repoKey: h.GitCommonDir,
		title:   title,
		detail: []string{
			i18n.T("This clone's fetch refspec doesn't map these branches, so a push never moves their remote-tracking ref — the ↓↑ tip markers and ahead/behind cannot follow them: %s", strings.Join(h.UnmappedBranches, ", ")),
			i18n.T("gg can add a per-branch mapping and fetch just those branches (no mass download)."),
		},
		actions: []noticeAction{
			{label: i18n.T("Add mappings + fetch these branches"),
				run: func(m Model) (Model, tea.Cmd) {
					m.refreshHealthAfterOp = true
					return m.startOp(engine.AddFetchMappings{Remote: "origin", Branches: m.repoHealth.UnmappedBranches})
				}},
			{label: i18n.T("Not now (ask again next load)")},
			{label: i18n.T("Never for this repo"), never: true},
		},
	}
}
```

(Add `"strings"` to the imports if absent.)

In `buildNotices` (the block at ~line 139 where `commitGraphNotice` and `clipboardNotice` are appended), insert between them, mirroring their exact dismissal-guard shape:

```go
	if n := narrowRefspecNotice(m.repoHealth); n != nil && !dismissed[n.id] && !m.noticeSessionDismissed[n.id] {
		out = append(out, n)
	}
```

(Copy the guard expression style verbatim from the `commitGraphNotice` line — if it uses a different session-dismissal accessor, match it.)

In `internal/tui/i18n_display.go`, in `optionDisplayName` after the `case "skip":` arm:

```go
	case "add":
		return i18n.T("add")
	}
```

- [ ] **Step 3: Add every new key to all four bundles**

Append to each of `internal/i18n/lang/{ja,ko,zh,ru}.toml` (translations below are the deliverable — refine wording freely, but keep every `%` verb identical to the English key; the loader rejects verb mismatches). Keys (English = key):

Engine prose (from Tasks 2–3):
1. `"%s/%s is not tracked by the fetch refspec — tip markers and ahead/behind cannot follow it. Add a tracking mapping for this branch?"`
2. `"; mapped %s/%s for tracking"`
3. `"; could not add fetch mapping: %s"`
4. `"; fetch mapping added but fetch failed: %s"`
5. `"mapped 1 branch for tracking"`
6. `"mapped %d branches for tracking"`
7. `"no branches to map"`
8. `"fetching"` — Step word; check first (`grep -c '^"fetching"' internal/i18n/lang/ja.toml`), add only if missing.

Option label: 9. `"add"`

TUI notice:
10. `"1 branch isn't tracked by the fetch refspec"`
11. `"%d branches aren't tracked by the fetch refspec"`
12. `"This clone's fetch refspec doesn't map these branches, so a push never moves their remote-tracking ref — the ↓↑ tip markers and ahead/behind cannot follow them: %s"`
13. `"gg can add a per-branch mapping and fetch just those branches (no mass download)."`
14. `"Add mappings + fetch these branches"`

Suggested translations (ja / ko / zh / ru per key number):

- 1: `"%s/%s は fetch refspec に追跡されていません — 先端マーカーと ahead/behind が追従できません。このブランチの追跡マッピングを追加しますか？"` / `"%s/%s 은(는) fetch refspec에 추적되지 않습니다 — 팁 마커와 ahead/behind가 따라갈 수 없습니다. 이 브랜치의 추적 매핑을 추가할까요?"` / `"%s/%s 未被 fetch refspec 跟踪 — 分支尖端标记和 ahead/behind 无法跟随。为该分支添加跟踪映射？"` / `"%s/%s не отслеживается fetch refspec — маркеры вершин и ahead/behind не смогут обновляться. Добавить отображение для этой ветки?"`
- 2: `"、%s/%s を追跡用にマッピングしました"` → NOTE: keep the leading English glue inside the translation naturally, e.g. ja `"; %s/%s を追跡用にマッピングしました"` / ko `"; %s/%s 을(를) 추적용으로 매핑했습니다"` / zh `"；已映射 %s/%s 用于跟踪"` / ru `"; настроено отслеживание %s/%s"`
- 3: ja `"; fetch マッピングを追加できませんでした: %s"` / ko `"; fetch 매핑을 추가하지 못했습니다: %s"` / zh `"；无法添加 fetch 映射：%s"` / ru `"; не удалось добавить fetch-отображение: %s"`
- 4: ja `"; fetch マッピングは追加されましたが fetch に失敗しました: %s"` / ko `"; fetch 매핑은 추가되었지만 fetch에 실패했습니다: %s"` / zh `"；已添加 fetch 映射但 fetch 失败：%s"` / ru `"; отображение добавлено, но fetch не удался: %s"`
- 5: ja `"1 個のブランチを追跡用にマッピングしました"` / ko `"브랜치 1개를 추적용으로 매핑했습니다"` / zh `"已映射 1 个分支用于跟踪"` / ru `"настроено отслеживание 1 ветки"`
- 6: ja `"%d 個のブランチを追跡用にマッピングしました"` / ko `"브랜치 %d개를 추적용으로 매핑했습니다"` / zh `"已映射 %d 个分支用于跟踪"` / ru `"настроено отслеживание веток: %d"`
- 7: ja `"マッピングするブランチがありません"` / ko `"매핑할 브랜치가 없습니다"` / zh `"没有要映射的分支"` / ru `"нет веток для отображения"`
- 8 (only if missing): ja `"フェッチ中"` / ko `"페치 중"` / zh `"抓取中"` / ru `"получение"`
- 9: ja `"追加"` / ko `"추가"` / zh `"添加"` / ru `"добавить"`
- 10: ja `"1 個のブランチが fetch refspec に追跡されていません"` / ko `"브랜치 1개가 fetch refspec에 추적되지 않습니다"` / zh `"1 个分支未被 fetch refspec 跟踪"` / ru `"1 ветка не отслеживается fetch refspec"`
- 11: ja `"%d 個のブランチが fetch refspec に追跡されていません"` / ko `"브랜치 %d개가 fetch refspec에 추적되지 않습니다"` / zh `"%d 个分支未被 fetch refspec 跟踪"` / ru `"веток не отслеживается fetch refspec: %d"`
- 12: ja `"このクローンの fetch refspec はこれらのブランチをマッピングしていないため、push してもリモート追跡 ref が動きません — ↓↑ 先端マーカーと ahead/behind が追従できません: %s"` / ko `"이 클론의 fetch refspec이 이 브랜치들을 매핑하지 않아 push해도 원격 추적 ref가 움직이지 않습니다 — ↓↑ 팁 마커와 ahead/behind가 따라갈 수 없습니다: %s"` / zh `"此克隆的 fetch refspec 未映射这些分支，push 后远程跟踪 ref 不会移动 — ↓↑ 尖端标记和 ahead/behind 无法跟随：%s"` / ru `"fetch refspec этого клона не отображает эти ветки: push не двигает их remote-tracking ref — маркеры ↓↑ и ahead/behind не обновляются: %s"`
- 13: ja `"gg はブランチごとのマッピングを追加し、そのブランチだけを fetch できます（大量ダウンロードなし）。"` / ko `"gg가 브랜치별 매핑을 추가하고 해당 브랜치만 fetch할 수 있습니다(대량 다운로드 없음)."` / zh `"gg 可以添加按分支的映射并只抓取这些分支（不会大量下载）。"` / ru `"gg может добавить отображение для каждой ветки и получить только их (без массовой загрузки)."`
- 14: ja `"マッピングを追加してこれらのブランチを fetch"` / ko `"매핑 추가 + 이 브랜치들 fetch"` / zh `"添加映射并抓取这些分支"` / ru `"Добавить отображения и получить эти ветки"`

Follow the file's existing `"key" = "translation"` TOML format and placement (bundles are NOT sorted — append at the end is fine).

- [ ] **Step 4: Run the gates to verify they pass**

Run: `go test ./internal/tui ./internal/i18n`
Expected: PASS — including the options-vocab, engine-prose, menu-labels, and i18n scan gates.

- [ ] **Step 5: Commit**

```bash
go vet ./internal/tui ./internal/i18n && gofmt -l internal/tui internal/i18n
git add internal/tui internal/i18n
git commit -m "feat(tui): narrow-refspec notice + fix action; i18n for fetch-mapping decision"
```

---

### Task 6: CLI — gg push --map / --no-map

**Files:**
- Modify: `internal/cli/ops.go` (`cmdPush`: two flags + policy entries)

**Interfaces:**
- Consumes: `engine.FetchMappingDecisionID` (Task 2), existing `cliDecider{policy, ...}` and `stdinIsTerminal()`.
- Produces: `gg push --map` / `gg push --no-map` (Task 7's e2e drives `--map`; Task 8 documents).

- [ ] **Step 1: Add the flags**

In `cmdPush` (internal/cli/ops.go), after the `onReject` flag declaration:

```go
	mapFlag := fs.Bool("map", false, "if the pushed branch isn't covered by the fetch refspec, add a per-branch mapping + fetch it without prompting")
	noMap := fs.Bool("no-map", false, "never add a fetch-refspec mapping for the pushed branch")
```

After the existing `--force/--force-with-lease` mutual-exclusion check:

```go
	if *mapFlag && *noMap {
		fmt.Fprintln(stderr, "push: choose at most one of --map/--no-map")
		return 2
	}
```

After the existing `policy` construction switch (the `policy = rp` default arm), append — the worktree-add `--hook/--no-hook` precedent, including the non-tty safety default:

```go
	if policy == nil {
		policy = map[string]string{}
	}
	switch {
	case *mapFlag:
		policy[engine.FetchMappingDecisionID] = "add"
	case *noMap:
		policy[engine.FetchMappingDecisionID] = "skip"
	case !stdinIsTerminal():
		policy[engine.FetchMappingDecisionID] = "skip" // pipelines never mutate config unseen
	}
```

- [ ] **Step 2: Verify by build + behavior**

Run: `go build ./cmd/gg && go test ./internal/cli`
Expected: build OK, existing CLI tests PASS. (End-to-end behavior is Task 7's scenario; there is no isolated cmdPush unit harness.)

Run: `go run ./cmd/gg push --map --no-map 2>&1; echo exit=$?` (in any repo dir)
Expected: `push: choose at most one of --map/--no-map`, `exit=2`.

- [ ] **Step 3: Commit**

```bash
go vet ./internal/cli && gofmt -l internal/cli
git add internal/cli
git commit -m "feat(cli): gg push --map/--no-map pre-answer the fetch-mapping decision"
```

---

### Task 7: e2e — git_config setup step + push-mapping scenarios

**Files:**
- Modify: `e2e/scenario.go` (Step fields + `kind()` + validation)
- Modify: `e2e/builder.go` (`runSteps` case)
- Create: `e2e/scenarios/s83_push_fetch_mapping.toml`
- Create: `e2e/scenarios/s84_push_no_map.toml`

**Interfaces:**
- Consumes: `gg push --map/--no-map` (Task 6), engine summary text `mapped origin/feat for tracking` (Task 2 — `finish` prints `✓ <summary>` to STDOUT, so `stdout_contains` sees it).
- Produces: setup step `{ git_config = "<key>", value = "<v>" }` (documented in Task 8).

- [ ] **Step 1: Add the step kind**

In `e2e/scenario.go` `Step` struct, after the `TagMessage` field:

```go
	GitConfig string `toml:"git_config"` // config key; Value holds the value (`git config <key> <value>`)
	Value     string `toml:"value"`
```

In `kind()`, alongside the other kind detections:

```go
	if s.GitConfig != "" {
		kinds = append(kinds, "git_config")
	}
```

And with the other pairing validations (mirror the `Content`/`TagMessage` style):

```go
	if s.Value != "" && k != "git_config" {
		return "", fmt.Errorf("step %+v: value is only valid with git_config", s)
	}
```

In `e2e/builder.go` `runSteps`, add a case:

```go
		case "git_config":
			b.git(t, dir, "config", st.GitConfig, st.Value)
```

- [ ] **Step 2: Write the scenarios**

Create `e2e/scenarios/s83_push_fetch_mapping.toml`:

```toml
name = "push --map adds a fetch-refspec mapping and the tracking ref appears"

[input.origin]
transport = "path"
steps = [
  { write = "a.txt", content = "1" },
  { commit = "init" },
]

[input]
steps = [
  # Narrow the clone's refspec to main only — the single-branch monorepo shape.
  { git_config = "remote.origin.fetch", value = "+refs/heads/main:refs/remotes/origin/main" },
  { branch = "feat" },
  { switch = "feat" },
  { write = "f.txt", content = "x" },
  { commit = "feat work" },
]

[[run]]
cmd = ["push", "--map"]
stdout_contains = ["pushed", "mapped origin/feat for tracking"]

# Second push: the branch is now mapped and current — no decision, no note.
[[run]]
cmd = ["push"]
stdout_contains = ["pushed"]
stdout_excludes = ["mapped"]

[expect]
branch = "feat"
ahead = 0            # resolvable upstream, in sync — only true if the mapping + fetch landed

[expect.origin]
branches = ["feat", "main"]
```

Create `e2e/scenarios/s84_push_no_map.toml`:

```toml
name = "push --no-map pushes but never touches the fetch refspec"

[input.origin]
transport = "path"
steps = [
  { write = "a.txt", content = "1" },
  { commit = "init" },
]

[input]
steps = [
  { git_config = "remote.origin.fetch", value = "+refs/heads/main:refs/remotes/origin/main" },
  { branch = "feat" },
  { switch = "feat" },
  { write = "f.txt", content = "x" },
  { commit = "feat work" },
]

[[run]]
cmd = ["push", "--no-map"]
stdout_contains = ["pushed"]
stdout_excludes = ["mapped"]

[expect]
branch = "feat"

[expect.origin]
branches = ["feat", "main"]
```

Consult `.claude/skills/writing-e2e-scenarios/SKILL.md` if the loader rejects a construct (e.g. `branch` + `switch` step split); adjust the steps, not the assertions. If the harness's `ahead` expectation errors on scenario s83 because it reads ahead differently, drop the `ahead = 0` line and rely on the two `stdout` contracts + origin branches.

- [ ] **Step 3: Run the scenarios**

Run: `go test ./e2e -run 'TestScenarios/s83|TestScenarios/s84' -v` (check the actual test name with `grep -rn "func Test" e2e/*_test.go | head -3` and adjust the -run pattern to how scenario names surface).
Expected: PASS both. Then the full `go test ./e2e` — all PASS.

- [ ] **Step 4: Commit**

```bash
go vet ./e2e && gofmt -l e2e
git add e2e
git commit -m "test(e2e): git_config setup step + push fetch-mapping scenarios"
```

---

### Task 8: docs, agent skill, full suite

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`, `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go`, `.claude/skills/writing-e2e-scenarios/SKILL.md`

**Interfaces:** consumes everything above; produces the released docs.

- [ ] **Step 1: Update the docs**

- `CHANGELOG.md`: new entry at top, e.g. "Push now detects a narrowed fetch refspec (single-branch/depth clones): after a successful push of an unmapped branch it offers to add a per-branch mapping + fetch it, so the Commits panel's ↓↑ tip markers and ahead/behind work; a notification-center notice fixes already-broken branches the same way; `gg push --map/--no-map`."
- `README.md`: in the push feature description, one short paragraph on the decision + the notice (user-facing wording, no internals).
- `CLAUDE.md` package map: extend the `engine` row (post-push `fetch_mapping.add` decision via `ensureRemoteTracking` at all three push exits, never fails a succeeded push; new op `AddFetchMappings{Remote, Branches}`, LockMode RefWrite, empty no-op, per-branch specs only); the `git` row (`ConfigAdd`, `ConfigGetAll`, `ConfigGetRegexp` — exit-1 → empty pattern — and `FetchBranches`); the `domain` row (`RepoHealth` gains `UnmappedBranches`).
- `internal/agentskill/using-gg.md` line ~89: extend the push usage to
  `gg push [--force | --force-with-lease] [--on-reject ...] [--map | --no-map] [<branch>]` and add one line: "`--map` adds a per-branch fetch-refspec mapping when the clone's refspec doesn't cover the pushed branch (single-branch/depth clones); `--no-map` declines; with neither, non-interactive runs skip."
- `internal/agentskill/agentskill.go`: `const Version = 53` → `54`.
- `.claude/skills/writing-e2e-scenarios/SKILL.md`: document the `git_config` step (`{ git_config = "<key>", value = "<v>" }` runs `git config` in the step's cwd).

- [ ] **Step 2: Refresh installed skill copies**

Run: `go run ./cmd/gg init --update`
Expected: reports refreshed using-gg copies for detected agents.

- [ ] **Step 3: Full suite**

Run: `./test.sh race`
Expected: vet+gofmt clean, unit green, e2e green. Fix anything red before committing.

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md README.md CLAUDE.md internal/agentskill .claude/skills/writing-e2e-scenarios
git commit -m "docs: push fetch-refspec mapping — changelog, README, CLAUDE.md, using-gg v54"
```

- [ ] **Step 5: Build and deliver the binary**

```bash
go build -o ./gg ./cmd/gg
```

Report the absolute path `/mnt/t/others/gigagit.worktrees/feat-push-fetch-mapping/gg` for manual testing. Do NOT merge — the user owns merges into main.
