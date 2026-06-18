# Facade-driven e2e for remote-branch ops — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. When authoring the e2e scenarios, also consult the project `writing-e2e-scenarios` skill.

**Goal:** Add `gg checkout` and `gg remote ls` CLI facades for the existing `SmartCheckout`/`RemoteBranches` engine code, plus a `stdout_contains` e2e assertion, then cover them with git-server scenarios that fetch real remote-tracking refs.

**Architecture:** The CLI commands are pure serializations of engine commands (build the `Operation`/run the query, no logic), mirroring `cmdSwitch`/`cmdWorktreeList`. The existing e2e harness (`e2e/gitserver.go` `git http-backend` + clone) drives them and asserts git state; a new `stdout_contains` on the `[[run]]` step lets us assert the pure-read listing's output.

**Tech Stack:** Go 1.26; `internal/cli` (flag-parsed facades → `runOperation`/`svc` queries), `internal/domain` (`query()` under a Read reservation), `e2e` (TOML scenarios → real repo + HTTP git server → `cli.Run` → state assertions).

## Global Constraints

- Module `github.com/gigagit/gg`, Go 1.26. Branch off `main`; the human merges.
- CLI commands contain no business logic: parse argv → build `engine.Operation` / call `svc` query → `runOperation`/print. Mirror `cmdSwitch` (`internal/cli/ops.go:59`).
- `Local` is the remote ref minus its first path segment (`strings.Cut(ref, "/")`), matching the TUI's `RemoteBranch.Branch` (`origin/feature/x` → `feature/x`).
- e2e scenarios assert **git state + exit codes**; only `stdout_contains` asserts output, and only for read commands (`remote ls`) — never `SmartCheckout`'s summary text.
- A CLI surface change bumps `agentskill.Version` and updates `internal/agentskill/using-gg.md` (per CLAUDE.md). `gg init --update` is a documented user step, not run by tests.
- TDD: failing test → minimal code → green → commit. `./test.sh unit` before each task's done; `./test.sh race` (incl. e2e) before wrap-up.
- Test-file naming: never end a test file with a `_GOOS`/`_GOARCH` token before `_test.go`.
- e2e scenario files are numbered `sNN_*.toml`; the current max is `s42`. New files continue at `s43`. Confirm with `ls e2e/scenarios/` at the start of Task 3.

---

### Task 1: e2e harness — `stdout_contains` on `[[run]]`

**Files:**
- Modify: `e2e/scenario.go` (`Run` struct; add `Run.MissingStdout`)
- Modify: `e2e/harness_test.go` (split stdout/stderr; assert)
- Test: `e2e/scenario_test.go` (append)

**Interfaces:**
- Produces: `Run.StdoutContains []string`; `func (r Run) MissingStdout(out string) []string`

- [ ] **Step 1: Write the failing unit tests**

Append to `e2e/scenario_test.go`:

```go
func TestRunMissingStdout(t *testing.T) {
	r := Run{StdoutContains: []string{"origin/foo", "origin/main"}}
	if miss := r.MissingStdout("origin/foo\norigin/main\n"); len(miss) != 0 {
		t.Fatalf("all present, got missing %v", miss)
	}
	miss := r.MissingStdout("origin/foo\n")
	if len(miss) != 1 || miss[0] != "origin/main" {
		t.Fatalf("missing = %v, want [origin/main]", miss)
	}
	if m := (Run{}).MissingStdout(""); m != nil {
		t.Fatalf("no expectations -> nil, got %v", m)
	}
}

func TestLoadScenarioParsesStdoutContains(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.toml")
	os.WriteFile(path, []byte(`name = "x"
[[run]]
cmd = ["remote", "ls"]
exit = 0
stdout_contains = ["origin/foo"]
[expect]
`), 0o644)
	sc, err := LoadScenario(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(sc.Runs) != 1 || len(sc.Runs[0].StdoutContains) != 1 || sc.Runs[0].StdoutContains[0] != "origin/foo" {
		t.Fatalf("StdoutContains not parsed: %+v", sc.Runs)
	}
}
```

NOTE: ensure `os` and `path/filepath` are imported in `scenario_test.go` (add if absent).

- [ ] **Step 2: Run them — expect failure**

Run: `go test ./e2e/ -run 'TestRunMissingStdout|TestLoadScenarioParsesStdoutContains'`
Expected: build failure — `StdoutContains`/`MissingStdout` undefined.

- [ ] **Step 3: Add the field + helper**

In `e2e/scenario.go`, the `Run` struct:

```go
type Run struct {
	Cmd            []string `toml:"cmd"`
	Cwd            string   `toml:"cwd"`
	Exit           *int     `toml:"exit"`
	StdoutContains []string `toml:"stdout_contains"`
}

// MissingStdout returns the StdoutContains substrings absent from out.
func (r Run) MissingStdout(out string) []string {
	var miss []string
	for _, want := range r.StdoutContains {
		if !strings.Contains(out, want) {
			miss = append(miss, want)
		}
	}
	return miss
}
```

Ensure `strings` is imported in `scenario.go` (it already uses regexp/parse helpers — add `strings` if not present).

- [ ] **Step 4: Run unit tests — expect green**

Run: `go test ./e2e/ -run 'TestRunMissingStdout|TestLoadScenarioParsesStdoutContains'`
Expected: PASS.

- [ ] **Step 5: Honor it in the run loop**

In `e2e/harness_test.go`, replace the single combined buffer with split buffers and add the assertion:

```go
			sb := buildSandbox(t, sc)
			var stdout, stderr bytes.Buffer
			for i, run := range sc.Runs {
				stdout.Reset()
				stderr.Reset()
				code := (CLIRunner{}).Run(sb.dir(run.Cwd), run.Cmd, &stdout, &stderr)
				if code != *run.Exit {
					t.Fatalf("run[%d] gg %s: exit %d, want %d\nstdout:\n%s\nstderr:\n%s",
						i, strings.Join(run.Cmd, " "), code, *run.Exit, stdout.String(), stderr.String())
				}
				if miss := run.MissingStdout(stdout.String()); len(miss) > 0 {
					t.Fatalf("run[%d] gg %s: stdout missing %v\nstdout:\n%s",
						i, strings.Join(run.Cmd, " "), miss, stdout.String())
				}
				t.Logf("run[%d] gg %s → exit %d ✓", i, strings.Join(run.Cmd, " "), code)
			}
```

- [ ] **Step 6: Run the full e2e suite — expect green (no regressions)**

Run: `go test ./e2e/`
Expected: ok — every existing scenario still passes (none set `stdout_contains`; the buffer split keeps failure output intact).

- [ ] **Step 7: Commit**

```bash
git add e2e/scenario.go e2e/harness_test.go e2e/scenario_test.go
git commit -m "feat(e2e): stdout_contains assertion on the [[run]] step"
```

---

### Task 2: domain query `svc.RemoteBranches`

**Files:**
- Modify: `internal/domain/query.go` (add the query near `Worktrees`)
- Test: `internal/domain/query_test.go` (append)

**Interfaces:**
- Consumes: `(*git.Repo).RemoteBranches` (chunk 1)
- Produces: `(*domain.Service).RemoteBranches(ctx) ([]model.RemoteBranch, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/domain/query_test.go` (uses the `fakeReads()` FakeRunner harness already in the file):

```go
func TestRemoteBranchesQuery(t *testing.T) {
	f := fakeReads()
	f.SetResponse("git for-each-ref (remotes)", gitexec.Result{Stdout: "origin/main\x00abc123\x001700000000\norigin/foo\x00def456\x001700000100\n"})
	rbs, err := New(&git.Repo{Runner: f}).RemoteBranches(context.Background())
	if err != nil {
		t.Fatalf("RemoteBranches: %v", err)
	}
	if len(rbs) != 2 || rbs[1].Name != "origin/foo" || rbs[1].Branch != "foo" {
		t.Fatalf("rbs = %+v", rbs)
	}
}
```

- [ ] **Step 2: Run it — expect failure**

Run: `go test ./internal/domain/ -run TestRemoteBranchesQuery`
Expected: build failure — `RemoteBranches` undefined on `*Service`.

- [ ] **Step 3: Add the query**

In `internal/domain/query.go`, after `Worktrees` (~line 163):

```go
// RemoteBranches lists remote-tracking branches (refs/remotes).
func (s *Service) RemoteBranches(ctx context.Context) ([]model.RemoteBranch, error) {
	return query(ctx, s, "remote-branches", s.repo.RemoteBranches)
}
```

- [ ] **Step 4: Run the test — expect green**

Run: `go test ./internal/domain/ -run TestRemoteBranchesQuery`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/query.go internal/domain/query_test.go
git commit -m "feat(domain): standalone RemoteBranches query (Read reservation)"
```

---

### Task 3: `gg checkout` command + checkout scenarios

**Files:**
- Modify: `internal/cli/ops.go` (add `cmdCheckout`)
- Modify: `internal/cli/cli.go` (dispatch `case "checkout"`)
- Test: `internal/cli/ops_test.go` (append cli tests)
- Create: `e2e/scenarios/s43_checkout_stay.toml`, `s44_checkout_switch.toml`, `s45_checkout_fast_forward.toml`, `s46_checkout_diverged_refused.toml`

**Interfaces:**
- Consumes: `engine.SmartCheckout`, `engine.CheckoutStay`, `engine.CheckoutSwitch` (chunk 2)
- Produces: CLI command `checkout`

- [ ] **Step 1: Write failing cli tests**

Append to `internal/cli/ops_test.go` (uses `runCLI`, `newRepoDir`, `gitIn` already in the package; `cloneBehind` shows the clone pattern):

```go
// cloneWithRemoteFoo builds origin (main + a foo branch ahead) and clones it,
// returning the clone dir with refs/remotes/origin/foo present, no local foo.
func cloneWithRemoteFoo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone := filepath.Join(root, "clone")
	gitIn(t, root, "init", "--bare", origin)
	gitIn(t, root, "clone", origin, seed)
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v1\n"), 0o644)
	gitIn(t, seed, "checkout", "-b", "main")
	gitIn(t, seed, "add", ".")
	gitIn(t, seed, "commit", "-m", "c1")
	gitIn(t, seed, "push", "-u", "origin", "main")
	gitIn(t, seed, "checkout", "-b", "foo")
	os.WriteFile(filepath.Join(seed, "g.txt"), []byte("foo\n"), 0o644)
	gitIn(t, seed, "add", ".")
	gitIn(t, seed, "commit", "-m", "foo-c2")
	gitIn(t, seed, "push", "-u", "origin", "foo")
	gitIn(t, root, "clone", origin, clone)
	gitIn(t, clone, "checkout", "main")
	return clone
}

func TestCheckoutStayCreatesLocalTrackingBranch(t *testing.T) {
	clone := cloneWithRemoteFoo(t)
	code, _, errb := runCLI(t, clone, "checkout", "origin/foo")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	// Stayed on main; created local foo.
	cur, _ := gitOutput(t, clone, "symbolic-ref", "--short", "HEAD")
	if strings.TrimSpace(cur) != "main" {
		t.Fatalf("HEAD = %q, want main (stay)", cur)
	}
	if _, err := gitOutputErr(clone, "rev-parse", "--verify", "refs/heads/foo"); err != nil {
		t.Fatalf("local foo not created: %v", err)
	}
}

func TestCheckoutSwitchChecksOutTheBranch(t *testing.T) {
	clone := cloneWithRemoteFoo(t)
	code, _, errb := runCLI(t, clone, "checkout", "origin/foo", "-s")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	cur, _ := gitOutput(t, clone, "symbolic-ref", "--short", "HEAD")
	if strings.TrimSpace(cur) != "foo" {
		t.Fatalf("HEAD = %q, want foo (switch)", cur)
	}
}

func TestCheckoutRequiresRemoteQualifiedRef(t *testing.T) {
	dir := newRepoDir(t)
	if code, _, _ := runCLI(t, dir, "checkout"); code == 0 {
		t.Fatal("checkout without a ref should fail")
	}
	if code, _, _ := runCLI(t, dir, "checkout", "foo"); code == 0 {
		t.Fatal("checkout with a non-qualified ref should fail")
	}
}
```

NOTE: `gitOutput`/`gitOutputErr` are illustrative — use the cli package's existing output helper if one exists (grep `func gitOut` / `runGit` in `internal/cli/*_test.go`); otherwise add a tiny `exec.Command` helper returning stdout. Match the package's real helpers rather than inventing names.

- [ ] **Step 2: Run them — expect failure**

Run: `go test ./internal/cli/ -run 'TestCheckout'`
Expected: failures — `checkout` is an unknown command (exit 2 from dispatch default; the success tests fail).

- [ ] **Step 3: Add `cmdCheckout`**

In `internal/cli/ops.go` (after `cmdSwitch`):

```go
func cmdCheckout(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("checkout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	doSwitch := fs.Bool("switch", false, "switch to the branch after checking it out")
	fs.BoolVar(doSwitch, "s", false, "switch to the branch after checking it out")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "checkout: a remote branch (e.g. origin/foo) is required")
		return 2
	}
	ref := fs.Arg(0)
	remote, local, ok := strings.Cut(ref, "/")
	if !ok || remote == "" || local == "" {
		fmt.Fprintln(stderr, "checkout: expected <remote>/<branch>, e.g. origin/foo")
		return 2
	}
	intent := engine.CheckoutStay
	if *doSwitch {
		intent = engine.CheckoutSwitch
	}
	res, err := runOperation(context.Background(), svc,
		engine.SmartCheckout{RemoteRef: ref, Local: local, Intent: intent}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}
```

Ensure `strings` is imported in `ops.go` (add if absent).

- [ ] **Step 4: Dispatch it**

In `internal/cli/cli.go`, add to the command switch (near `case "switch"`):

```go
	case "checkout":
		return cmdCheckout(svc, rest, stdout, stderr)
```

- [ ] **Step 5: Run cli tests — expect green**

Run: `go test ./internal/cli/ -run 'TestCheckout'`
Expected: PASS.

- [ ] **Step 6: Write the four checkout scenarios**

Create `e2e/scenarios/s43_checkout_stay.toml`:

```toml
name = "checkout: create a local tracking branch and stay"

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

[[run]]
cmd  = ["checkout", "origin/foo"]
exit = 0

[expect]
branch   = "main"
clean    = true
branches = ["foo", "main"]

[[expect.log]]
branch   = "foo"
subjects = ["foo-c2", "c1"]
```

Create `e2e/scenarios/s44_checkout_switch.toml`:

```toml
name = "checkout -s: create a local tracking branch and switch to it"

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

[[run]]
cmd  = ["checkout", "origin/foo", "-s"]
exit = 0

[expect]
branch   = "foo"
clean    = true
branches = ["foo", "main"]

[[expect.log]]
subjects = ["foo-c2", "c1"]
```

Create `e2e/scenarios/s45_checkout_fast_forward.toml` (local `foo` is created behind origin/foo, then checkout fast-forwards it):

```toml
name = "checkout: fast-forward an existing local branch to the remote ref"

[input]
steps = [
  { branch = "foo" },
]

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

[[run]]
cmd  = ["checkout", "origin/foo"]
exit = 0

[expect]
branch   = "main"
clean    = true
branches = ["foo", "main"]

[[expect.log]]
branch   = "foo"
subjects = ["foo-c2", "c1"]
```

NOTE on s45: `[input].steps` run on the **clone**, whose HEAD is `main` at `c1`,
so `{branch="foo"}` creates local `foo` at `c1` — behind `origin/foo` (`foo-c2`).
The checkout must fast-forward it to `foo-c2`. If the harness applies input
steps before the clone's `origin/foo` exists, reorder is impossible (origin is
built first, then clone, then input steps — confirmed in `buildSandbox`), so
this holds.

Create `e2e/scenarios/s46_checkout_diverged_refused.toml` (local `foo` has a commit not on origin/foo):

```toml
name = "checkout: refuse to fast-forward a diverged local branch"

[input]
steps = [
  { branch = "foo" },
  { switch = "foo" },
  { write = "h.txt", content = "local\n" },
  { commit = "local-divergent" },
  { switch = "main" },
]

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

[[run]]
cmd  = ["checkout", "origin/foo"]
exit = 1

[expect]
branch = "main"

[[expect.log]]
branch   = "foo"
subjects = ["local-divergent", "c1"]
```

NOTE on s46: local `foo` = `c1`→`local-divergent`; `origin/foo` = `c1`→`foo-c2`;
they diverge at `c1`, so the FF is refused (`exit = 1`) and `foo`'s log still
shows `local-divergent`, never `foo-c2`.

- [ ] **Step 7: Run the new scenarios**

Run: `go test ./e2e/ -run 'TestScenarios/s43|TestScenarios/s44|TestScenarios/s45|TestScenarios/s46' -v`
Expected: all four PASS. If `branches`/`log` assertions mismatch, inspect the
`-v` state dump and fix the TOML (not the engine — the engine is already tested).

- [ ] **Step 8: Commit**

```bash
git add internal/cli/ops.go internal/cli/cli.go internal/cli/ops_test.go e2e/scenarios/s43_checkout_stay.toml e2e/scenarios/s44_checkout_switch.toml e2e/scenarios/s45_checkout_fast_forward.toml e2e/scenarios/s46_checkout_diverged_refused.toml
git commit -m "feat(cli): gg checkout <remote>/<branch> [-s]; e2e scenarios"
```

---

### Task 4: `gg remote ls` command + listing scenario

**Files:**
- Create: `internal/cli/remote.go`
- Modify: `internal/cli/cli.go` (dispatch `case "remote"`)
- Test: `internal/cli/remote_test.go`
- Create: `e2e/scenarios/s47_remote_ls.toml`

**Interfaces:**
- Consumes: `(*domain.Service).RemoteBranches` (Task 2); `Run.StdoutContains` (Task 1)
- Produces: CLI command `remote` with `ls`/`list`

- [ ] **Step 1: Write the failing cli test**

Create `internal/cli/remote_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestRemoteListPrintsRemoteBranches(t *testing.T) {
	clone := cloneWithRemoteFoo(t) // from ops_test.go (same package)
	code, out, errb := runCLI(t, clone, "remote", "ls")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	if !strings.Contains(out, "origin/foo") || !strings.Contains(out, "origin/main") {
		t.Fatalf("remote ls output missing refs:\n%s", out)
	}
	if strings.Contains(out, "origin/HEAD") {
		t.Fatalf("origin/HEAD symref should be filtered:\n%s", out)
	}
}

func TestRemoteUnknownSubcommand(t *testing.T) {
	dir := newRepoDir(t)
	if code, _, _ := runCLI(t, dir, "remote", "bogus"); code == 0 {
		t.Fatal("unknown remote subcommand should fail")
	}
}
```

- [ ] **Step 2: Run them — expect failure**

Run: `go test ./internal/cli/ -run 'TestRemote'`
Expected: failure — `remote` is an unknown command.

- [ ] **Step 3: Add the command**

Create `internal/cli/remote.go`:

```go
package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/gigagit/gg/internal/domain"
)

func cmdRemote(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "ls" || args[0] == "list" {
		return cmdRemoteList(svc, stdout, stderr)
	}
	fmt.Fprintf(stderr, "remote: unknown subcommand %q (try: ls)\n", args[0])
	return 2
}

func cmdRemoteList(svc *domain.Service, stdout, stderr io.Writer) int {
	rbs, err := svc.RemoteBranches(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, rb := range rbs {
		fmt.Fprintln(stdout, rb.Name)
	}
	return 0
}
```

- [ ] **Step 4: Dispatch it**

In `internal/cli/cli.go`, add (near `case "worktree"`):

```go
	case "remote":
		return cmdRemote(svc, rest, stdout, stderr)
```

- [ ] **Step 5: Run cli tests — expect green**

Run: `go test ./internal/cli/ -run 'TestRemote'`
Expected: PASS.

- [ ] **Step 6: Write the listing scenario**

Create `e2e/scenarios/s47_remote_ls.toml`:

```toml
name = "remote ls: list remote-tracking branches after a clone"

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

[[run]]
cmd             = ["remote", "ls"]
exit            = 0
stdout_contains = ["origin/foo", "origin/main"]

[expect]
branch = "main"
clean  = true
```

- [ ] **Step 7: Run the scenario**

Run: `go test ./e2e/ -run 'TestScenarios/s47' -v`
Expected: PASS (exit 0 and both refs in stdout).

- [ ] **Step 8: Commit**

```bash
git add internal/cli/remote.go internal/cli/cli.go internal/cli/remote_test.go e2e/scenarios/s47_remote_ls.toml
git commit -m "feat(cli): gg remote ls (list remote-tracking branches); e2e scenario"
```

---

### Task 5: agentskill + CHANGELOG + full gate

**Files:**
- Modify: `internal/agentskill/using-gg.md`
- Modify: `internal/agentskill/*.go` (the `Version` constant)
- Modify: `CHANGELOG.md`

**Interfaces:** none.

- [ ] **Step 1: Document the commands in the agent skill**

In `internal/agentskill/using-gg.md`, add to the command reference (match the
file's existing format — read a nearby command entry first):

```markdown
- `gg checkout <remote>/<branch> [-s|--switch]` — check out a remote-tracking
  branch as a local tracking branch (fast-forward-safe; refuses a diverged local
  branch). `-s` also switches to it.
- `gg remote ls` — list remote-tracking branches (one `remote/branch` per line).
```

- [ ] **Step 2: Bump the agentskill version**

Find the version constant: `grep -rn 'Version' internal/agentskill/*.go | grep -v _test`.
Increment it (match the existing scheme — e.g. patch bump). Run:
`go test ./internal/agentskill/` — expect PASS (the embedded-content/version test stays consistent).

- [ ] **Step 3: CHANGELOG entry**

In `CHANGELOG.md`, under `### Added`:

```markdown
- CLI: `gg checkout <remote>/<branch> [-s]` checks out a remote-tracking branch
  as a local tracking branch (fast-forward-safe via the same `SmartCheckout`
  engine op as the TUI's Remotes-tab `c`/`s`; `-s` switches to it). `gg remote
  ls` lists remote-tracking branches. The e2e harness gained a `stdout_contains`
  assertion to cover command output.
```

- [ ] **Step 4: Full gate**

Run: `./test.sh unit`
Expected: all green.
Run: `./test.sh race`
Expected: all green (incl. the five new e2e scenarios).

- [ ] **Step 5: Commit**

```bash
git add internal/agentskill/using-gg.md internal/agentskill/*.go CHANGELOG.md
git commit -m "docs: document gg checkout + remote ls in agent skill and CHANGELOG"
```

---

## Self-Review notes

- **Spec coverage:** harness `stdout_contains` (Task 1), `svc.RemoteBranches`
  (Task 2), `gg checkout` + 4 scenarios (Task 3), `gg remote ls` + scenario
  (Task 4), agentskill/CHANGELOG (Task 5). All spec parts mapped.
- **Dependency order:** Task 4's listing scenario needs Task 1 (`stdout_contains`)
  and Task 2 (`RemoteBranches`); both precede it. Task 3's checkout scenarios
  assert state only, so they need only the `checkout` command.
- **Type consistency:** `engine.SmartCheckout{RemoteRef, Local, Intent}` and
  `CheckoutStay`/`CheckoutSwitch` (chunk 2) used verbatim; `Run.StdoutContains`
  / `Run.MissingStdout` defined in Task 1 and consumed in Task 1's loop + Task 4.
- **No engine changes:** scenarios exercise existing, already-tested engine code
  through the new CLI facades; failures in Task 3/4 indicate TOML/wiring, not
  engine logic.
- **Watch-items:** cli test output-helper names (Task 3 NOTE — match the package),
  `s45` step-ordering assumption (verified against `buildSandbox`), agentskill
  version bump (Task 5).
