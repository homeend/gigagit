# Facade-driven e2e coverage for remote-branch ops — design

Date: 2026-06-19
Status: approved (brainstorm); ready for plan
Branch/worktree: `worktree-e2e-remote-checkout`
Related: chunk 3 of the remotes effort (`2026-06-18-remotes-tab-design.md`,
`2026-06-18-remote-checkout-design.md`)

## Summary

The remote-branch features shipped so far (`SmartCheckout`, `RemoteBranches`) are
TUI-only and were only unit-tested against *fabricated* `refs/remotes/origin/*`
(via `update-ref` + remote config). They have no coverage through a real
`git fetch` from a server, and no coverage of the full command→git-state path.

This adds that coverage by completing the **frontend-agnostic CLI facade** for
these ops and exercising them through the **existing** e2e harness — which
already spins up an in-process `git http-backend` server (`e2e/gitserver.go`),
clones from it, and asserts real git state. No docker, no npm.

Three thin additions, then scenarios:

1. `gg checkout <remote>/<branch> [-s|--switch]` — CLI serialization of
   `engine.SmartCheckout` (mirrors `cmdSwitch`).
2. `gg remote ls` — CLI serialization of a new `svc.RemoteBranches` query;
   prints remote-tracking branches.
3. A `stdout_contains` assertion on the e2e `[[run]]` step (the harness can
   assert git state and exit codes today, but not command output — needed to
   verify a pure-read listing).

## Goals

- Real-server e2e coverage of `SmartCheckout`: stay, switch, fast-forward, and
  the diverged refusal.
- e2e coverage of remote-branch listing after a real clone.
- A reusable `stdout_contains` capability in the e2e harness.
- CLI parity for the two ops (the architecture's frontend-agnostic principle).

## Non-goals

- No TUI changes (the TUI paths are already wired and unit-tested).
- No new engine logic — `SmartCheckout`/`RemoteBranches` are reused as-is.
- No fetch/prune command (still a later chunk); these scenarios rely on the
  clone's remote-tracking refs, not a live re-fetch.
- `gg remote` gets only an `ls` subcommand here (no add/remove/rename).

## Architecture

The CLI commands are pure serializations: parse argv → build the engine
`Operation` (or call the query) → run via `runOperation`/print. They contain no
business logic, mirroring `cmdSwitch` (`internal/cli/ops.go:59`, ~9 lines) and
`cmdWorktreeList`.

### Part A — e2e harness: `stdout_contains`

`e2e/scenario.go`, the `Run` struct gains:

```go
type Run struct {
	Cmd            []string `toml:"cmd"`
	Cwd            string   `toml:"cwd"`
	Exit           *int     `toml:"exit"`
	StdoutContains []string `toml:"stdout_contains"` // substrings the run's stdout must contain
}
```

`e2e/harness_test.go` run loop: today stdout+stderr both go to one combined
`out` buffer. Split them so the assertion targets stdout precisely:

```go
var stdout, stderr bytes.Buffer
for i, run := range sc.Runs {
	stdout.Reset()
	stderr.Reset()
	code := (CLIRunner{}).Run(sb.dir(run.Cwd), run.Cmd, &stdout, &stderr)
	if code != *run.Exit {
		t.Fatalf("run[%d] gg %s: exit %d, want %d\nstdout:\n%s\nstderr:\n%s",
			i, strings.Join(run.Cmd, " "), code, *run.Exit, stdout.String(), stderr.String())
	}
	for _, want := range run.StdoutContains {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("run[%d] gg %s: stdout missing %q\nstdout:\n%s",
				i, strings.Join(run.Cmd, " "), want, stdout.String())
		}
	}
	t.Logf("run[%d] gg %s → exit %d ✓", i, strings.Join(run.Cmd, " "), code)
}
```

`StdoutContains` nil/empty = not asserted (every existing scenario unaffected).

### Part B — domain query `svc.RemoteBranches`

`internal/domain/query.go`, mirroring `svc.Worktrees` (a `query()` call under a
Read reservation, singleflight-coalesced):

```go
// RemoteBranches lists remote-tracking branches (refs/remotes).
func (s *Service) RemoteBranches(ctx context.Context) ([]model.RemoteBranch, error) {
	return query(ctx, s, "remote-branches", s.repo.RemoteBranches)
}
```

(The `git.Repo.RemoteBranches` verb already exists from chunk 1.)

### Part C — `gg checkout`

`internal/cli/ops.go`:

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

Dispatch in `internal/cli/cli.go`: `case "checkout": return cmdCheckout(svc, rest, stdout, stderr)`.

Note: `Local` is the ref minus its first path segment, matching the TUI's
`RemoteBranch.Branch` (`strings.Cut` on the first `/`), so `origin/feature/x` →
`feature/x`.

### Part D — `gg remote ls`

`internal/cli/remote.go` (new file, mirrors `cmdWorktree`/`cmdWorktreeList`):

```go
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
		fmt.Fprintln(stdout, rb.Name) // "origin/foo", one per line
	}
	return 0
}
```

Dispatch: `case "remote": return cmdRemote(svc, rest, stdout, stderr)`.

### Part E — e2e scenarios (`e2e/scenarios/`)

Author per the `writing-e2e-scenarios` skill. The origin gets a `foo` branch so
the clone yields `refs/remotes/origin/foo`. Numbering continues after the
current highest `sNN` (check `ls e2e/scenarios/` at plan time).

1. **checkout stay (absent local)** — origin: main + a `foo` branch with its own
   commit. `gg checkout origin/foo`. Expect `branch="main"` (HEAD unchanged),
   `branches=["foo","main"]`, `[[expect.log]] branch="foo" subjects=[<foo tip>, …]`.
2. **checkout switch** — `gg checkout origin/foo -s`. Expect `branch="foo"`,
   `clean=true`, `[[expect.log]]` (current branch) shows the foo line.
3. **checkout fast-forward (existing behind local)** — clone has a local `foo`
   behind `origin/foo` (origin's `after` advances foo, or the local foo is
   created behind). `gg checkout origin/foo`. Expect `foo` fast-forwarded
   (its log gains the upstream commit).
4. **checkout diverged refuse** — local `foo` has a commit not on `origin/foo`.
   `gg checkout origin/foo` → `exit=1`; `foo`'s log unchanged (still has the
   local-only commit, not the remote one).
5. **remote ls** — after clone, `gg remote ls` → `exit=0`,
   `stdout_contains=["origin/foo"]` (and `origin/main`).

### Part F — docs

- `internal/agentskill/using-gg.md`: document `gg checkout <remote>/<branch>
  [-s]` and `gg remote ls`; bump `agentskill.Version`. (`gg init --update`
  refresh is a user step, noted in the plan, not run by tests.)
- `CHANGELOG.md`: a CLI entry for both commands.

## Testing (TDD)

- **harness** (`e2e/scenario_test.go` / a harness unit test): a `[[run]]` with
  `stdout_contains` fails when the substring is absent, passes when present;
  existing scenarios (no `stdout_contains`) are unaffected.
- **domain** (`query_test.go`): `svc.RemoteBranches` returns the repo's
  remote-tracking branches (fabricated ref), under the Read path.
- **cli** (`ops`/`remote` cli tests, the package's existing style): `gg checkout
  origin/foo` builds `SmartCheckout{RemoteRef:"origin/foo", Local:"foo",
  Intent:CheckoutStay}`; `-s` ⇒ `CheckoutSwitch`; a bare name errors (exit 2);
  `gg remote ls` prints each ref. (Mirror how existing cli tests assert — likely
  a `FakeRunner` or temp repo; match the package.)
- **e2e**: the five scenarios above (the integration coverage).

## Risks / watch-items

- **Combined→split buffers**: existing scenarios print to stderr on error; the
  split must keep the failure message showing both streams (it does). Run the
  full e2e suite to confirm no scenario depended on the combined buffer.
- **`stdout_contains` vs noisy ops**: only assert it on read commands (`remote
  ls`); `SmartCheckout`'s summary text is not asserted (state is).
- **Scenario `foo` setup**: confirm the builder's `{branch=…}`/`{switch=…}` steps
  run on the origin repo and that the clone fetches all branches (default clone
  does). Verify the diverged case actually produces a non-fast-forward (mirror
  the engine test's setup that already proved this).
- **CLI surface ⇒ agentskill**: per CLAUDE.md, a CLI change bumps
  `agentskill.Version`; don't skip it.
- **Test-file naming**: avoid `_GOOS`/`_GOARCH` tokens before `_test.go`.

## Slicing for the plan

1. Harness `stdout_contains` (struct field + run loop split + a harness test).
2. `svc.RemoteBranches` domain query (+ test).
3. `gg checkout` command + dispatch + cli test + the 4 checkout scenarios.
4. `gg remote ls` command + dispatch + cli test + the listing scenario.
5. agentskill (`using-gg.md` + version bump) + CHANGELOG; full race gate.
