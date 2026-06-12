# E2E scenario harness — design

Status: spec (validated by `2026-06-12-e2e-schema-validation.md`, 17 scenarios
covering all current smart ops plus future merge/rebase/SmartMerge).

## Goal

A declarative end-to-end test layer for gg: each test is one human- and
agent-readable TOML file that (1) builds a git repository in a known state,
(2) runs gg CLI commands against it, and (3) asserts the **user-visible
semantic outcome** — files and their content, current branch, branches,
stashes and their content, sync state, history shape — never git internals
(no SHAs, no dates, no stash names).

The end goal beyond the harness itself: a project skill that lets agents
author correct e2e scenarios for complex operations (SmartPull, SmartSwitch,
future SmartMerge/rebase) autonomously.

## Requirements

1. Assertions are user-visible semantics only; exit codes matter, CLI text
   output does not.
2. Scenarios are authored by hand (human or agent) — **no recording/golden
   capture**. Consequence: the input state must be fully knowable from the
   scenario file, so input is declarative steps, not opaque archives.
3. Deterministic: same scenario ⇒ same outcome on every run and machine.
4. Frontend-agnostic: the same scenario files must later drive an MCP runner
   (M3) without modification.
5. A packed-repo escape hatch (`input.zip`) for captured real-world repos —
   a later phase (see Build order), not the primary path.

## Non-goals

- TUI testing (covered by the existing `internal/tui` test suite).
- Asserting CLI stdout/stderr text, summaries, or progress lines.
- Asserting commit SHAs, dates, stash names, reflog, packfile layout.
- Performance/scale testing of huge monorepos.

## Architecture

New top-level package **`e2e/`**:

```
e2e/
  harness_test.go   # discovers scenarios/*.toml; one subtest per file
  scenario.go       # schema structs + strict TOML loader (unknown keys = error)
  builder.go        # executes [input] steps; builds origin + clone topology
  gitserver.go      # real HTTP git server: net/http/cgi → `git http-backend`
  runner.go         # Runner interface; CLIRunner → cli.Run() in-process
  assert.go         # semantic assertions; collects ALL mismatches per scenario
  scenarios/
    switch_dirty_autostash.toml
    pull_diverged_rebase.toml
    …
```

Data flow per scenario:

```
scenario.toml ─load→ Scenario
                      │ builder: temp sandbox (origin? → clone) , frozen env
                      ▼
                 [input] steps  ──→  repo(s) in known state
                      │ runner: for each [[run]]: cli.Run(workdir, argv, …)
                      ▼               assert exit code immediately
                 [expect] assertions ──→ aggregated semantic diff report
```

### Determinism rules (builder + runner environment)

- `GIT_AUTHOR_NAME/EMAIL`, `GIT_COMMITTER_NAME/EMAIL` pinned (`gg-e2e <e2e@gg>`).
- `GIT_AUTHOR_DATE`/`GIT_COMMITTER_DATE` start at a fixed epoch and advance
  +1s per commit (stable order, stable content).
- `HOME` → per-test temp dir; `GIT_CONFIG_NOSYSTEM=1`; `GIT_CONFIG_GLOBAL`
  → pinned minimal config (init.defaultBranch=main, protocol.file.allow for
  clones from the sandbox).
- A pinned `.gg.toml` is written into the repo (worktree path template etc.)
  so template-driven paths are stable.
- stdin for `cli.Run` is an empty non-TTY reader ⇒ any unanswered engine
  decision errors deterministically (scenarios must answer forks via flags).
- Subtests run `t.Parallel()`; every scenario is fully sandboxed in
  `t.TempDir()`.

### Remote topology — served over real HTTP

If `[input.origin]` is present:

1. Build the origin repo from `origin.steps` in `sandbox/origin`.
2. Start a per-scenario **HTTP git server** for the sandbox and clone via
   `http://127.0.0.1:<port>/origin` → the scenario's repo is `sandbox/local`.
3. Run `[input].steps` in the clone.
4. Run `origin.after` steps in origin (creates behind/diverged states).

The server (`gitserver.go`) wraps git's own smart-protocol implementation:
an `httptest.Server` whose handler is `net/http/cgi` executing
`git http-backend` (`GIT_PROJECT_ROOT=sandbox`, `GIT_HTTP_EXPORT_ALL=1`,
origin config `http.receivepack=true`). Fetch, clone, pull, and push all
travel the genuine network transport (upload-pack/receive-pack over HTTP),
not filesystem shortcuts — no Docker or external services, works inside
plain `go test` on all platforms, and each parallel subtest gets its own
listener on an ephemeral port.

`[input.origin] transport = "http"` is the default; `transport = "path"`
remains available for scenarios where transport is irrelevant and speed
matters. Anonymous access only — auth/credential flows are out of scope
(future work alongside MCP).

Origin is a normal (non-bare) repo with `receive.denyCurrentBranch=ignore`
so push scenarios work; **origin-side assertions read refs/log only, never
origin's working tree** (which push leaves stale by design).

### Runner abstraction (MCP-ready)

```go
type Runner interface {
    Run(workdir string, argv []string) (exitCode int)
}
```

v1 ships `CLIRunner` calling `cli.Run` in-process (full CLI→engine→real-git
stack, fast, debuggable). M3 adds `MCPRunner` mapping the same argv onto MCP
tool calls. An env var (`GG_E2E_RUNNER=binary`) may later exec the built `gg`
for true process-level runs; scenario files never change.

## Scenario schema (v1, as validated)

```toml
name = "human-readable purpose"

[input]                      # local repo (or clone, when [input.origin] exists)
steps = [
  { write = "path", content = "…" },        # create/overwrite file (mkdir -p)
  { rm = "path" },
  { commit = "subject" },                    # git add -A && commit
  { branch = "name" },                       # create at HEAD, stay
  { switch = "name" },
  { stash = "message" },
  { worktree = "relpath", branch = "name" }, # linked worktree
]
# every step accepts cwd = "relpath" to act inside a linked worktree

[input.origin]               # optional remote topology (see above)
steps = [ … ]                # pre-clone upstream history
after = [ … ]                # post-clone upstream changes

[[run]]                      # repeated; executed in order
cmd  = ["pull", "--on-conflict=rebase"]      # gg CLI argv
cwd  = "relpath"             # optional: run inside a linked worktree
exit = 0                     # required

[expect]
branch      = "main"           # current branch
branches    = ["main", "x"]    # exactly these local branches (order-free)
clean       = true             # mutually exclusive with [expect.status]
ahead       = 0                # vs upstream; omitted = not asserted
behind      = 0
in_progress = "none"           # none|rebase|merge
stashes     = 0                # total stash count; omitted = not asserted
worktrees   = ["wt-x"]         # linked worktrees (relpaths); omitted = not asserted

[expect.files]               # working-tree content, checksum-compared
"a.txt" = "literal\n"
"b.bin" = { sha256 = "hex" }
"c.txt" = { unchanged = true }   # identical to end-of-input state
"d.txt" = { absent = true }
# files not listed are not asserted

[expect.status]              # fine-grained dirty state (when clean is false/omitted)
staged     = ["a.txt"]
unstaged   = []
untracked  = ["n.txt"]
conflicted = []

[[expect.stash]]             # newest first; content via git show stash@{N}:path
contains = { "n.txt" = "draft\n" }

[[expect.log]]               # commit subjects, newest first
branch   = "feature/x"       # default: current branch
subjects = ["local change", { matches = "^Merge" }, "initial"]

[expect.worktree."wt-x"]     # per-worktree scope: files + status
[expect.worktree."wt-x".files]
"a.txt" = "v2\n"

[expect.origin]              # origin-side: log/branches only (refs, not tree)
[[expect.origin.log]]
branch = "main"
subjects = ["pushed change", "initial"]
```

Loader is **strict**: unknown keys, unknown step kinds, or malformed values
fail the test with a clear message — an agent's typo must never silently
assert nothing.

### Failure reporting

Assertions never stop at the first mismatch; the report lists every failed
expectation in semantic terms, e.g.:

```
scenario switch_dirty_autostash:
  current branch: want "feature/login", got "main"
  file notes.txt: want content (sha256 ab12…), got absent
  stash[0]: want to contain notes.txt, stash list is empty
```

A failed `exit` expectation aborts the remaining `[[run]]` blocks (state is
unpredictable past it) but still prints the gg stderr captured for that run.

## Authoring skill (phase 3)

`.claude/skills/writing-e2e-scenarios/SKILL.md`:

- the schema reference (above) with one full worked example;
- step + expectation vocabulary tables;
- an **operation contract table** — for each gg command: which branch you end
  on, what gets stashed/popped (and that autostash includes untracked), which
  decisions exist (ID, options, answering flag), exit-code conventions
  (chosen "abort" ⇒ exit 0; failed op / unanswered decision ⇒ exit 1);
- the rule: *expectations are derived from the operation's contract, never
  from running the scenario first*;
- the S9 lesson from the validation doc (PullAndStay ends on the target
  branch) as a worked "common mistake".

## Build order

1. **Harness core** — schema structs, strict loader, builder (local-only
   steps), CLIRunner, assertions for `branch`/`branches`/`clean`/`files`/
   `status`/`stashes`/`stash`; seed scenarios S1, S2, S13 (switch happy path,
   pop-conflict, undo).
2. **Remote + history** — HTTP git server (`gitserver.go`), `[input.origin]`
   topology, `ahead`/`behind`, `in_progress`, `[[expect.log]]`,
   `[expect.origin]`, worktree steps and scoped assertions; scenarios
   S3–S12, S14.
3. **Authoring skill + corpus** — write the skill, then author the remaining
   corpus for current ops; zip escape hatch (`input.zip`) if still wanted.

Future-op scenarios (S15–S17) stay in the validation doc until merge/rebase
ship; the schema already covers them.

## Risks / open points

- **Stash content inspection** uses `git stash show --include-untracked
  --name-only` + `git show 'stash@{N}:path'`; untracked files in stashes live
  in the third parent commit — the assertion helper must check both paths.
  Cost: contained in `assert.go`.
- **`ahead`/`behind`** require an upstream; asserting them in a no-origin
  scenario is a loader-level validation error.
- **git version drift**: subjects/state checks are stable across modern git;
  CI should still pin a known git version eventually.
- **`git http-backend` on Windows**: ships with Git for Windows but lives in
  `libexec/git-core`; the CGI handler must locate it via
  `git --exec-path` rather than assuming it is on PATH. If a platform proves
  hostile, the `transport = "path"` fallback keeps scenarios runnable while
  the server issue is fixed.
