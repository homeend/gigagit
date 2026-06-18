# Remote-branch checkout (`c` / `s`, SmartCheckout) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On the Remotes tab, `c` materializes the selected remote-tracking branch as a local tracking branch (staying put) and `s` does the same then switches — both fast-forward-safe, never clobbering a diverged local branch.

**Architecture:** A new `SmartCheckout` engine op composes four new local-only git verbs (existence check, ancestor check, tracking-branch create, local fast-forward) and, for the switch case, runs the existing `SmartSwitch{…}.Run(ctx, deps)` inline (shared `OpDeps`, no nested `domain.Execute` — avoids a second repogate reservation). The TUI window-scopes `c`/`s` to `panelRemotes`.

**Tech Stack:** Go 1.26; engine ops implement `Operation.Run(ctx, OpDeps) (Result, error)` and `emit` `Event`s; tests use a real `git` in `t.TempDir()` (`newRepo`/`newTestRepo`) and `loadedModel` (real `domain.Service`) for TUI routing.

## Global Constraints

- Module `github.com/gigagit/gg`, Go 1.26. Branch off `main`; the human merges.
- A git verb is ONE invocation built with `gitcmd`, run via `r.Runner.Run`. Exit-code booleans (`show-ref`, `merge-base --is-ancestor`) treat exit 1 as `false` (mirror `CurrentBranch`'s `res.ExitCode == 1` handling); any other non-zero is a real error.
- `internal/tui` reaches git only through `internal/domain`/`engine`; ops run via `m.startOp(...)` → `domain.Execute`.
- A new verb an op needs must be added to the `engine.GitOps` interface; the `var _ GitOps = (*git.Repo)(nil)` proof keeps `*git.Repo` honest.
- `FastForwardToRef` uses FULLY-QUALIFIED refs (`refs/remotes/<remote>/<branch>:refs/heads/<branch>`) — verified 2026-06-18 to FF a non-checked-out branch, reject divergence (exit 1), and refuse a checked-out branch (exit 128).
- TDD: failing test → minimal code → green → commit. `./test.sh unit` before each task's done; `./test.sh race` before wrap-up.
- Test-file naming: never end a test file with a `_GOOS`/`_GOARCH` token before `_test.go`.
- New keybindings land in BOTH `help.go` and the footer.
- Docs: update `CHANGELOG.md` (always) and `README.md` (Remotes tab now has `c`/`s`). No CLI surface change → no `agentskill` bump (CLI checkout is deferred).

---

### Task 1: Four local-only git verbs + GitOps

**Files:**
- Modify: `internal/git/mutate.go` (add `LocalBranchExists`, `IsAncestor`, `CreateTrackingBranch` near `CreateBranch`)
- Modify: `internal/git/sync.go` (add `FastForwardToRef` near `FastForwardRef`)
- Modify: `internal/engine/gitops.go` (add the four signatures to `GitOps`)
- Test: `internal/git/mutate_test.go`, `internal/git/sync_test.go` (append)

**Interfaces:**
- Produces: `(*git.Repo).LocalBranchExists(ctx, name string) (bool, error)`
- Produces: `(*git.Repo).IsAncestor(ctx, a, b string) (bool, error)`
- Produces: `(*git.Repo).CreateTrackingBranch(ctx, name, upstream string) error`
- Produces: `(*git.Repo).FastForwardToRef(ctx, branch, source string) error`

- [ ] **Step 1: Write failing verb tests**

Append to `internal/git/mutate_test.go` (use the real-git `newTestRepo(t)` that returns `(dir, runner)`; build `repo := &Repo{Runner: runner}` and a local `git(args...)` exec helper like `TestRepoRemoteBranches` already does):

```go
func TestLocalBranchExists(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	if ok, err := repo.LocalBranchExists(context.Background(), "main"); err != nil || !ok {
		t.Fatalf("main exists: ok=%v err=%v", ok, err)
	}
	if ok, err := repo.LocalBranchExists(context.Background(), "nope"); err != nil || ok {
		t.Fatalf("nope: ok=%v err=%v (want false,nil)", ok, err)
	}
}

func TestIsAncestor(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	git := func(a ...string) {
		c := exec.Command("git", a...); c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil { t.Fatalf("git %v: %v\n%s", a, err, out) }
	}
	git("commit", "--allow-empty", "-m", "c2")
	if ok, err := repo.IsAncestor(context.Background(), "HEAD~1", "HEAD"); err != nil || !ok {
		t.Fatalf("HEAD~1 ancestor of HEAD: ok=%v err=%v", ok, err)
	}
	if ok, err := repo.IsAncestor(context.Background(), "HEAD", "HEAD~1"); err != nil || ok {
		t.Fatalf("HEAD not ancestor of HEAD~1: ok=%v err=%v (want false,nil)", ok, err)
	}
}

func TestCreateTrackingBranch(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	git := func(a ...string) string {
		c := exec.Command("git", a...); c.Dir = dir
		out, err := c.CombinedOutput()
		if err != nil { t.Fatalf("git %v: %v\n%s", a, err, out) }
		return string(out)
	}
	git("update-ref", "refs/remotes/origin/foo", "HEAD")
	if err := repo.CreateTrackingBranch(context.Background(), "foo", "origin/foo"); err != nil {
		t.Fatalf("CreateTrackingBranch: %v", err)
	}
	up := strings.TrimSpace(git("for-each-ref", "--format=%(upstream:short)", "refs/heads/foo"))
	if up != "origin/foo" {
		t.Fatalf("upstream = %q, want origin/foo", up)
	}
}
```

Append to `internal/git/sync_test.go` (same real-git helper pattern):

```go
func TestFastForwardToRef(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	git := func(a ...string) {
		c := exec.Command("git", a...); c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil { t.Fatalf("git %v: %v\n%s", a, err, out) }
	}
	// origin/foo two commits ahead; local foo at the older base.
	git("commit", "--allow-empty", "-m", "c2")
	git("commit", "--allow-empty", "-m", "c3")
	git("update-ref", "refs/remotes/origin/foo", "HEAD")
	git("branch", "foo", "HEAD~2")

	if err := repo.FastForwardToRef(context.Background(), "foo", "refs/remotes/origin/foo"); err != nil {
		t.Fatalf("FastForwardToRef (ff): %v", err)
	}
	// Diverged foo now errors.
	git("branch", "-f", "foo", "HEAD~1")
	git("commit", "--allow-empty", "-m", "divergent-on-main-not-foo") // moves HEAD/main, leaves foo behind+diverged base
	git("update-ref", "refs/heads/foo", "HEAD~1") // foo at a different line than origin/foo
	// Make foo truly diverge from origin/foo:
	git("symbolic-ref", "HEAD", "refs/heads/foo")
	git("commit", "--allow-empty", "-m", "foo-only")
	git("symbolic-ref", "HEAD", "refs/heads/main")
	if err := repo.FastForwardToRef(context.Background(), "foo", "refs/remotes/origin/foo"); err == nil {
		t.Fatal("FastForwardToRef on diverged branch should error")
	}
}
```

NOTE: the diverged-setup above is finicky; if it's awkward in practice, simplify
to: create `foo` with its own empty commit (`git checkout -b foo && git commit
--allow-empty`), point `origin/foo` at a sibling commit on `main`, then assert
the FF errors. The assertion that matters is **"FF of a non-ancestor errors."**

- [ ] **Step 2: Run them — expect failure**

Run: `go test ./internal/git/ -run 'TestLocalBranchExists|TestIsAncestor|TestCreateTrackingBranch|TestFastForwardToRef'`
Expected: build failure — the four methods are undefined.

- [ ] **Step 3: Implement the three mutate verbs**

In `internal/git/mutate.go`, after `CreateBranch`:

```go
// LocalBranchExists reports whether refs/heads/<name> exists.
func (r *Repo) LocalBranchExists(ctx context.Context, name string) (bool, error) {
	argv := gitcmd.New("show-ref").Arg("--verify", "--quiet", "refs/heads/"+name).ToArgv()
	res, err := r.Runner.Run(ctx, "git show-ref (branch exists)", argv)
	if err != nil {
		if res.ExitCode == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// IsAncestor reports whether commit a is an ancestor of commit b (a fast-forward
// from a to b is possible). a == b counts as true.
func (r *Repo) IsAncestor(ctx context.Context, a, b string) (bool, error) {
	argv := gitcmd.New("merge-base").Arg("--is-ancestor", a, b).ToArgv()
	res, err := r.Runner.Run(ctx, "git merge-base --is-ancestor", argv)
	if err != nil {
		if res.ExitCode == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// CreateTrackingBranch creates refs/heads/<name> at <upstream> with tracking
// configured, without switching to it.
func (r *Repo) CreateTrackingBranch(ctx context.Context, name, upstream string) error {
	argv := gitcmd.New("branch").Arg("--track", name, upstream).ToArgv()
	_, err := r.Runner.Run(ctx, "git branch --track", argv)
	return err
}
```

- [ ] **Step 4: Implement `FastForwardToRef`**

In `internal/git/sync.go`, after `FastForwardRef`:

```go
// FastForwardToRef fast-forwards a NON-checked-out local branch to source (a
// fully-qualified local ref, e.g. refs/remotes/origin/foo) without a checkout or
// network access. Fails if the update is not a fast-forward, or if <branch> is
// checked out in any worktree.
func (r *Repo) FastForwardToRef(ctx context.Context, branch, source string) error {
	refspec := source + ":refs/heads/" + branch
	argv := gitcmd.New("fetch").Arg("--no-write-fetch-head", ".", refspec).ToArgv()
	_, err := r.Runner.Run(ctx, "git fetch (ff-to-ref)", argv)
	return err
}
```

- [ ] **Step 5: Add the four to `GitOps`**

In `internal/engine/gitops.go`, add inside the interface (near `CreateBranch`/`FastForwardRef`):

```go
	LocalBranchExists(ctx context.Context, name string) (bool, error)
	IsAncestor(ctx context.Context, a, b string) (bool, error)
	CreateTrackingBranch(ctx context.Context, name, upstream string) error
	FastForwardToRef(ctx context.Context, branch, source string) error
```

- [ ] **Step 6: Run verb tests + build — expect green**

Run: `go test ./internal/git/ -run 'TestLocalBranchExists|TestIsAncestor|TestCreateTrackingBranch|TestFastForwardToRef'`
Expected: PASS.
Run: `go build ./...`
Expected: clean (the `_ GitOps = (*git.Repo)(nil)` proof compiles).

- [ ] **Step 7: Commit**

```bash
git add internal/git/mutate.go internal/git/sync.go internal/git/mutate_test.go internal/git/sync_test.go internal/engine/gitops.go
git commit -m "feat(git): local-only verbs for remote checkout (exists/ancestor/track/ff-to-ref)"
```

---

### Task 2: `SmartCheckout` engine op

**Files:**
- Create: `internal/engine/smart_checkout.go`
- Test: `internal/engine/smart_checkout_test.go`

**Interfaces:**
- Consumes: the four Task-1 verbs via `OpDeps.Repo`; `SmartSwitch` (same package); `CurrentBranch`, `WorktreeForBranch` (existing GitOps).
- Produces: `engine.SmartCheckout{RemoteRef, Local string; Intent CheckoutIntent}`, `engine.CheckoutIntent` with `CheckoutStay`/`CheckoutSwitch`.

- [ ] **Step 1: Write failing op tests**

Create `internal/engine/smart_checkout_test.go` (mirror `smart_switch_test.go`: `newRepo(t)` → `(dir, repo)`; `OpDeps{Repo: repo, Decider: MapDecider{}}`):

```go
package engine

import (
	"context"
	"os/exec"
	"testing"
)

func gitIn(t *testing.T, dir string, a ...string) {
	t.Helper()
	c := exec.Command("git", a...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", a, err, out)
	}
}

func TestSmartCheckoutAbsentLocalStay(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "update-ref", "refs/remotes/origin/foo", "HEAD")
	res, err := SmartCheckout{RemoteRef: "origin/foo", Local: "foo", Intent: CheckoutStay}.
		Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if ok, _ := repo.LocalBranchExists(context.Background(), "foo"); !ok {
		t.Fatal("local foo was not created")
	}
	if cur, _ := repo.CurrentBranch(context.Background()); cur == "foo" {
		t.Fatal("CheckoutStay must not switch")
	}
}

func TestSmartCheckoutAbsentLocalSwitch(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "update-ref", "refs/remotes/origin/foo", "HEAD")
	_, err := SmartCheckout{RemoteRef: "origin/foo", Local: "foo", Intent: CheckoutSwitch}.
		Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("checkout+switch: %v", err)
	}
	if cur, _ := repo.CurrentBranch(context.Background()); cur != "foo" {
		t.Fatalf("current = %q, want foo", cur)
	}
}

func TestSmartCheckoutExistingBehindFastForwards(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c2")
	gitIn(t, dir, "update-ref", "refs/remotes/origin/foo", "HEAD") // ahead
	gitIn(t, dir, "branch", "foo", "HEAD~1")                       // behind
	_, err := SmartCheckout{RemoteRef: "origin/foo", Local: "foo", Intent: CheckoutStay}.
		Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("ff checkout: %v", err)
	}
	if ok, _ := repo.IsAncestor(context.Background(), "refs/remotes/origin/foo", "foo"); !ok {
		t.Fatal("foo was not fast-forwarded to origin/foo")
	}
}

func TestSmartCheckoutDivergedRefuses(t *testing.T) {
	dir, repo := newRepo(t)
	// foo gets its own commit; origin/foo points at a different commit on main.
	gitIn(t, dir, "branch", "foo")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "main-only")
	gitIn(t, dir, "update-ref", "refs/remotes/origin/foo", "HEAD")
	gitIn(t, dir, "checkout", "foo")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "foo-only")
	gitIn(t, dir, "checkout", "main")
	_, err := SmartCheckout{RemoteRef: "origin/foo", Local: "foo", Intent: CheckoutStay}.
		Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	if err == nil {
		t.Fatal("diverged local foo must refuse")
	}
}

func TestSmartCheckoutCurrentBranchRefuses(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "checkout", "-b", "foo")
	gitIn(t, dir, "update-ref", "refs/remotes/origin/foo", "HEAD")
	_, err := SmartCheckout{RemoteRef: "origin/foo", Local: "foo", Intent: CheckoutStay}.
		Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	if err == nil {
		t.Fatal("checkout of the current branch must refuse")
	}
}
```

NOTE: confirm `newRepo(t)` initializes the default branch as `main` (the other
engine tests assume it). If it's `master`, adjust the `checkout main` lines.

- [ ] **Step 2: Run them — expect failure**

Run: `go test ./internal/engine/ -run TestSmartCheckout`
Expected: build failure — `SmartCheckout`/`CheckoutStay`/`CheckoutSwitch` undefined.

- [ ] **Step 3: Implement `SmartCheckout`**

Create `internal/engine/smart_checkout.go`:

```go
package engine

import (
	"context"
	"fmt"
)

// CheckoutIntent selects whether SmartCheckout leaves the current branch (c) or
// switches to the materialized branch (s).
type CheckoutIntent int

const (
	CheckoutStay   CheckoutIntent = iota // c
	CheckoutSwitch                       // s
)

// SmartCheckout materializes a remote-tracking branch as a local tracking
// branch, fast-forward-safe, optionally switching to it. RemoteRef is the short
// remote ref ("origin/foo"); Local is the target local name ("foo").
type SmartCheckout struct {
	RemoteRef string
	Local     string
	Intent    CheckoutIntent
}

func (op SmartCheckout) Run(ctx context.Context, deps OpDeps) (Result, error) {
	exists, err := deps.Repo.LocalBranchExists(ctx, op.Local)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		deps.emit(ctx, Progress{Step: "creating tracking branch", Detail: op.Local})
		if err := deps.Repo.CreateTrackingBranch(ctx, op.Local, op.RemoteRef); err != nil {
			return Result{}, err
		}
	} else {
		// Reuse the existing local branch only when it can fast-forward to the
		// remote ref. All refusals run before any mutation (no partial state).
		cur, err := deps.Repo.CurrentBranch(ctx)
		if err != nil {
			return Result{}, err
		}
		if cur == op.Local {
			return Result{}, fmt.Errorf("%s is the current branch; use pull to update it", op.Local)
		}
		if wt, err := deps.Repo.WorktreeForBranch(ctx, op.Local); err != nil {
			return Result{}, err
		} else if wt != nil {
			return Result{}, fmt.Errorf("%s is checked out in another worktree: %s", op.Local, wt.Path)
		}
		ff, err := deps.Repo.IsAncestor(ctx, op.Local, op.RemoteRef)
		if err != nil {
			return Result{}, err
		}
		if !ff {
			return Result{}, fmt.Errorf("%s has diverged from %s; cannot fast-forward", op.Local, op.RemoteRef)
		}
		deps.emit(ctx, Progress{Step: "fast-forwarding", Detail: op.Local})
		if err := deps.Repo.FastForwardToRef(ctx, op.Local, "refs/remotes/"+op.RemoteRef); err != nil {
			return Result{}, err
		}
	}

	if op.Intent == CheckoutSwitch {
		// Reuse SmartSwitch for autostash + the stash-pop-conflict decision.
		// Run inline (shared deps) — never via domain.Execute, which would take
		// a nested repogate reservation under the one we already hold.
		return SmartSwitch{Branch: op.Local}.Run(ctx, deps)
	}
	return Result{Summary: "checked out " + op.RemoteRef + " as " + op.Local, Changed: true}, nil
}

var _ Operation = SmartCheckout{}
```

- [ ] **Step 4: Run op tests — expect green**

Run: `go test ./internal/engine/ -run TestSmartCheckout -v`
Expected: all five PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/smart_checkout.go internal/engine/smart_checkout_test.go
git commit -m "feat(engine): SmartCheckout op (ff-safe remote-branch checkout, stay/switch)"
```

---

### Task 3: TUI wiring — `selectedRemote`, `c`/`s` handlers, footer, help

**Files:**
- Modify: `internal/tui/avail.go` (add `selectedRemote`, `canCheckoutRemote`)
- Modify: `internal/tui/model.go` (`case "c":` ~line 428; `case "s":` ~line 445)
- Modify: `internal/tui/footer.go` (`contextBindings` table)
- Modify: `internal/tui/help.go` (Remotes panel section)
- Test: `internal/tui/avail_test.go` or `nav_test.go` (append routing + gating tests)

**Interfaces:**
- Consumes: `engine.SmartCheckout`, `engine.CheckoutStay`, `engine.CheckoutSwitch` (Task 2); `Model.remoteBranches`, `panelRemotes` (chunk 1).
- Produces: `(Model).selectedRemote() (model.RemoteBranch, bool)`, `(Model).canCheckoutRemote() bool`.

- [ ] **Step 1: Write failing routing/gating tests**

Append to `internal/tui/nav_test.go` (uses `loadedModel`, which has a real svc so `startOp` works; fabricate a remote ref in the repo via the model's repo is awkward, so just drive the synchronous routing flags):

```go
func TestCheckoutRemoteRoutesCAndS(t *testing.T) {
	m := loadedModel(t)
	m.remoteBranches = []model.RemoteBranch{{Name: "origin/foo", Remote: "origin", Branch: "foo"}}
	m.focus = panelRemotes
	m.sel[panelRemotes] = 0

	// c on the Remotes tab starts an op (running=true) and does NOT open the commit popup.
	u, _ := m.Update(keyMsg("c"))
	mc := u.(Model)
	if mc.commitPopup != nil {
		t.Fatal("c on Remotes must not open the commit popup")
	}
	if !mc.running {
		t.Fatal("c on Remotes should start SmartCheckout (running)")
	}

	// s on the Remotes tab starts an op too.
	m2 := loadedModel(t)
	m2.remoteBranches = []model.RemoteBranch{{Name: "origin/foo", Remote: "origin", Branch: "foo"}}
	m2.focus = panelRemotes
	m2.sel[panelRemotes] = 0
	u2, _ := m2.Update(keyMsg("s"))
	if !u2.(Model).running {
		t.Fatal("s on Remotes should start SmartCheckout (running)")
	}
}

func TestCanCheckoutRemoteGating(t *testing.T) {
	m := loadedModel(t)
	if m.canCheckoutRemote() {
		t.Fatal("no remote selected -> false")
	}
	m.remoteBranches = []model.RemoteBranch{{Name: "origin/foo", Remote: "origin", Branch: "foo"}}
	m.sel[panelRemotes] = 0
	if !m.canCheckoutRemote() {
		t.Fatal("a remote selected + idle -> true")
	}
}
```

NOTE: ensure `model` is imported in `nav_test.go` (chunk-1 nav tests already use
`model.RemoteBranch`, so it should be). The async op goroutine from `startOp`
runs against `loadedModel`'s real repo (which has no `origin/foo`), so it will
error in the background — harmless: the test only asserts the *synchronous*
routing (`running`, `commitPopup`), set before the goroutine matters.

- [ ] **Step 2: Run — expect failure**

Run: `go test ./internal/tui/ -run 'TestCheckoutRemoteRoutesCAndS|TestCanCheckoutRemoteGating'`
Expected: build failure — `selectedRemote`/`canCheckoutRemote` undefined.

- [ ] **Step 3: Add `selectedRemote` + `canCheckoutRemote`**

In `internal/tui/avail.go`, after `selectedWorktree`:

```go
// selectedRemote resolves the Remotes panel selection through the view
// transforms. ok is false when the visible list is empty.
func (m Model) selectedRemote() (model.RemoteBranch, bool) {
	bi, ok := m.backingIndex(panelRemotes)
	if !ok {
		return model.RemoteBranch{}, false
	}
	return m.remoteBranches[bi], true
}

// canCheckoutRemote gates c/s on the Remotes tab: a remote row is selected and
// no op is running.
func (m Model) canCheckoutRemote() bool {
	_, ok := m.selectedRemote()
	return m.opsIdle() && ok
}
```

- [ ] **Step 4: Route `c` and `s` on `panelRemotes`**

In `internal/tui/model.go`, change `case "c":` (~line 428) to branch first:

```go
		case "c":
			if m.focus == panelRemotes && m.canCheckoutRemote() {
				rb, _ := m.selectedRemote()
				return m.startOp(engine.SmartCheckout{RemoteRef: rb.Name, Local: rb.Branch, Intent: engine.CheckoutStay})
			}
			if m.canCommit() {
				m.commitPopup = &commitPopup{}
			}
```

In `case "s":` (~line 445), add a `panelRemotes` branch at the very top of the case (before the Files-stash branch):

```go
		case "s":
			if m.focus == panelRemotes && m.canCheckoutRemote() {
				rb, _ := m.selectedRemote()
				return m.startOp(engine.SmartCheckout{RemoteRef: rb.Name, Local: rb.Branch, Intent: engine.CheckoutSwitch})
			}
			if m.focus == panelFiles && m.opsIdle() {
```

(`engine` is already imported in `model.go`.)

- [ ] **Step 5: Footer entries**

In `internal/tui/footer.go`, add to `contextBindings` (after the Worktrees rows):

```go
	{"checkout-remote", "c", "[c]heckout", func(m Model) bool { return m.focus == panelRemotes && m.canCheckoutRemote() }, scopeRow},
	{"switch-remote", "s", "[s]witch", func(m Model) bool { return m.focus == panelRemotes && m.canCheckoutRemote() }, scopeRow},
```

- [ ] **Step 6: Help copy**

In `internal/tui/help.go`, replace the chunk-1 placeholder under the Remotes
section:

```go
		h("Remotes panel"),
		r("", "remote-tracking branches (refs/remotes), read-only for now"),
```

with:

```go
		h("Remotes panel"),
		r("c", "checkout: create or fast-forward a local tracking branch (stay on the current branch)"),
		r("s", "checkout and switch to it — fast-forward-safe; refuses if the local branch has diverged"),
```

- [ ] **Step 7: Run TUI tests + build — expect green**

Run: `go test ./internal/tui/ -run 'TestCheckoutRemoteRoutesCAndS|TestCanCheckoutRemoteGating'`
Expected: PASS.
Run: `go test ./internal/tui/`
Expected: ok (no regressions; if a help/footer golden test enumerates the
Remotes section, update it to the new c/s rows).

- [ ] **Step 8: Commit**

```bash
git add internal/tui/avail.go internal/tui/model.go internal/tui/footer.go internal/tui/help.go internal/tui/nav_test.go
git commit -m "feat(tui): c/s checkout actions on the Remotes tab (SmartCheckout)"
```

---

### Task 4: Docs + full gate

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `README.md`

**Interfaces:** none.

- [ ] **Step 1: CHANGELOG entry**

In `CHANGELOG.md`, under `### Added`, after the chunk-1 Remotes bullet:

```markdown
- TUI: **checkout from the Remotes tab** — `c` materializes the selected
  remote-tracking branch as a local tracking branch (staying on the current
  branch); `s` does the same and switches to it. Both are fast-forward-safe: an
  existing local branch is reused only when it fast-forwards to the remote ref,
  and a diverged branch is refused (never clobbered). `s` autostashes like a
  normal switch.
```

- [ ] **Step 2: README entry**

In `README.md`, extend the Remotes mention (the `ctrl+←/→` row from chunk 1, or
the branches key table) to note `c`/`s`:

```markdown
On the **Remotes** tab: `c` checks out the selected remote branch as a local
tracking branch (stay), `s` checks out and switches to it — both
fast-forward-safe (a diverged local branch is refused).
```

Place it as a new table row or sentence consistent with the surrounding format
(read the Remotes/branches rows first and match their style).

- [ ] **Step 3: Full gate**

Run: `./test.sh unit`
Expected: all green.
Run: `./test.sh race`
Expected: all green (incl. e2e).

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md README.md
git commit -m "docs: Remotes-tab checkout (c/s) in CHANGELOG and README"
```

---

## Self-Review notes

- **Spec coverage:** verbs (Task 1), `SmartCheckout` with stay/switch + all four
  table states + the two checked-out refusals (Task 2), `c`/`s` window-scoped
  wiring + footer + help (Task 3), docs (Task 4). CLI explicitly deferred (spec
  non-goal) — no task, correct.
- **Type consistency:** `SmartCheckout{RemoteRef, Local, Intent}` and
  `CheckoutStay`/`CheckoutSwitch` defined in Task 2, consumed verbatim in Task 3.
  The four verb signatures in Task 1 match their `GitOps` additions and their
  `SmartCheckout` call sites.
- **No-partial-state:** Task 2's exists-branch refusals (current / worktree /
  diverged) all precede `FastForwardToRef`; the absent path's only mutation is
  the create. Matches the spec invariant.
- **Watch-items carried:** fully-qualified FF refspec (Task 1 constraint +
  verified), `newRepo` default-branch name (Task 2 note), help/footer golden
  tests (Task 3 step 7).
