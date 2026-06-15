# Interactive Rebase — Slice 4a: scriptable CLI `gg rebase -i --plan` — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A scriptable `gg rebase -i [--branch <b>] --plan <file> <newbase>` that consumes a rebase-plan JSON and drives `engine.InteractiveRebase`, with a subprocess integration test that builds `gg` and proves the whole stack end-to-end through a real binary acting as the sequence editor.

**Architecture:** Extend `cmdRebase` (`internal/cli/rebase.go`) with `-i`/`--interactive` + `--plan <file>`. When interactive, read+parse the plan, resolve the branch (flag or current), set `GGBin = os.Executable()`, and run `engine.InteractiveRebase` through the existing `cliDecider` (the `rebase-conflict` decision is answered by `--on-conflict`). The non-interactive `SmartRebase` path is unchanged.

**Tech Stack:** Go 1.26, `internal/cli`, `internal/rebaseplan`, `internal/engine` (Slice 3, merged).

**Spec:** `docs/superpowers/specs/2026-06-15-interactive-rebase-design.md` (Slice 4, CLI half).

**Why a subprocess test, not the TOML e2e harness:** the e2e harness runs the CLI **in-process** (`e2e/runner.go`: `cli.Run(...)`). Interactive rebase needs a real `gg` binary on disk as `GIT_SEQUENCE_EDITOR` — in-process, `os.Executable()` is the test binary, which doesn't route `__rebase-seq`. So the honest full-stack test builds `gg` and runs it as a subprocess (mirroring the Slice-3 engine integration test). The in-process `cli.Run` tests cover only flag/usage errors.

**Conventions:** TDD; gate `./test.sh race`; commits end `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

---

### Task 1: extend `cmdRebase` with `-i` / `--plan`

**Files:**
- Modify: `internal/cli/rebase.go`
- Test: `internal/cli/rebase_test.go` (in-process usage-error cases)

- [ ] **Step 1: Write the failing usage tests**

Add to `internal/cli/rebase_test.go` (package `cli`; ensure imports `bytes`,
`strings`, `testing`, and the test repo helpers `newRepoDir`/`gitRun` already
used by `commit_test.go`):

```go
func TestRebaseInteractiveRequiresPlan(t *testing.T) {
	dir := newRepoDir(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"rebase", "-i", "main"}, strings.NewReader(""), &out, &errb, "")
	if code != 2 {
		t.Fatalf("exit %d, want 2 (usage)", code)
	}
	if !strings.Contains(errb.String(), "--plan") {
		t.Fatalf("stderr %q should mention --plan", errb.String())
	}
}

func TestRebasePlanRequiresInteractive(t *testing.T) {
	dir := newRepoDir(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"rebase", "--plan", "/tmp/x.json", "main"}, strings.NewReader(""), &out, &errb, "")
	if code != 2 {
		t.Fatalf("exit %d, want 2 (usage)", code)
	}
}

func TestRebaseInteractiveBadPlanFile(t *testing.T) {
	dir := newRepoDir(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"rebase", "-i", "--plan", "/nonexistent/plan.json", "main"}, strings.NewReader(""), &out, &errb, "")
	if code != 2 {
		t.Fatalf("exit %d, want 2 (unreadable plan)", code)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cli/ -run 'TestRebaseInteractive|TestRebasePlan' -v`
Expected: FAIL — `-i`/`--plan` are unknown flags (parse error gives exit 2 but
the `--plan`-mention / interactive-specific assertions fail), or the flags are
ignored.

- [ ] **Step 3: Extend `cmdRebase`**

In `internal/cli/rebase.go`, add the imports `os` and
`github.com/gigagit/gg/internal/rebaseplan` to the import block, and rewrite
`cmdRebase`:

```go
func cmdRebase(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("rebase", flag.ContinueOnError)
	fs.SetOutput(stderr)
	branch := fs.String("branch", "", "branch to rebase (default: the current branch)")
	onConflict := fs.String("on-conflict", "", "answer a rebase conflict: keep|abort")
	interactive := fs.Bool("i", false, "interactive rebase, driven by --plan")
	fs.BoolVar(interactive, "interactive", false, "alias for -i")
	planPath := fs.String("plan", "", "interactive rebase plan file (JSON); requires -i")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "usage: gg rebase [-i --plan <file>] [--branch <b>] [--on-conflict=keep|abort] <newbase>")
		return 2
	}

	policy := map[string]string{}
	switch *onConflict {
	case "":
	case "keep":
		policy["rebase-conflict"] = "keep-conflicts"
	case "abort":
		policy["rebase-conflict"] = "abort"
	default:
		fmt.Fprintf(stderr, "rebase: invalid --on-conflict %q (keep|abort)\n", *onConflict)
		return 2
	}
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}

	if *interactive || *planPath != "" {
		if !*interactive || *planPath == "" {
			fmt.Fprintln(stderr, "rebase: -i requires --plan <file> (the TUI builds the plan interactively)")
			return 2
		}
		raw, err := os.ReadFile(*planPath)
		if err != nil {
			fmt.Fprintln(stderr, "rebase: --plan:", err)
			return 2
		}
		plan, err := rebaseplan.Unmarshal(raw)
		if err != nil {
			fmt.Fprintln(stderr, "rebase: --plan: invalid plan:", err)
			return 2
		}
		br := *branch
		if br == "" {
			br, err = svc.CurrentBranch(context.Background())
			if err != nil {
				fmt.Fprintln(stderr, "rebase:", err)
				return 1
			}
		}
		ggBin, err := os.Executable()
		if err != nil {
			fmt.Fprintln(stderr, "rebase:", err)
			return 1
		}
		res, err := runOperation(context.Background(), svc,
			engine.InteractiveRebase{Branch: br, Onto: fs.Arg(0), Plan: plan, GGBin: ggBin}, dec, stderr)
		return finish(res, err, stdout, stderr)
	}

	res, err := runOperation(context.Background(), svc,
		engine.SmartRebase{Branch: *branch, Onto: fs.Arg(0)}, dec, stderr)
	return finish(res, err, stdout, stderr)
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/cli/ -run 'TestRebaseInteractive|TestRebasePlan' -v && go build ./...`
Expected: PASS; build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/rebase.go internal/cli/rebase_test.go
git commit -m "feat(cli): gg rebase -i --plan drives InteractiveRebase

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: subprocess integration test (full stack through a real `gg`)

**Files:**
- Test: `internal/cli/rebase_integration_test.go`

- [ ] **Step 1: Write the failing integration test**

Create `internal/cli/rebase_integration_test.go` (package `cli`):

```go
package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/rebaseplan"
)

// runGit runs git in dir with a frozen identity, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// gg rebase -i --plan, run as a real built binary (so it can serve as the
// rebase sequence editor), rewords the oldest commit and drops the middle one.
func TestRebaseInteractiveCLIEndToEnd(t *testing.T) {
	ggBin := filepath.Join(t.TempDir(), "gg-test-bin")
	if out, err := exec.Command("go", "build", "-o", ggBin, "github.com/gigagit/gg/cmd/gg").CombinedOutput(); err != nil {
		t.Fatalf("build gg: %v\n%s", err, out)
	}

	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "r.txt"), []byte("r\n"), 0o644)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")
	runGit(t, dir, "checkout", "-b", "work")
	for _, n := range []string{"wip1", "wip2", "wip3"} {
		os.WriteFile(filepath.Join(dir, n+".txt"), []byte(n+"\n"), 0o644)
		runGit(t, dir, "add", ".")
		runGit(t, dir, "commit", "-m", n)
	}

	plan := rebaseplan.Plan{Entries: []rebaseplan.Entry{
		{Sha: runGit(t, dir, "rev-parse", "work~2"), Action: rebaseplan.Reword, Orig: "wip1", NewMsg: "wip1 reworded"},
		{Sha: runGit(t, dir, "rev-parse", "work~1"), Action: rebaseplan.Drop, Orig: "wip2"},
		{Sha: runGit(t, dir, "rev-parse", "work"), Action: rebaseplan.Pick, Orig: "wip3"},
	}}
	b, _ := rebaseplan.Marshal(plan)
	planPath := filepath.Join(dir, "plan.json")
	os.WriteFile(planPath, b, 0o644)

	cmd := exec.Command(ggBin, "rebase", "-i", "--plan", planPath, "main")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gg rebase -i: %v\n%s", err, out)
	}

	got := runGit(t, dir, "log", "--pretty=%s", "main..work") // newest-first
	want := "wip3\nwip1 reworded"
	if got != want {
		t.Fatalf("subjects =\n%q\nwant\n%q", got, want)
	}
}
```

- [ ] **Step 2: Run to verify it passes**

Run: `go test ./internal/cli/ -run TestRebaseInteractiveCLIEndToEnd -v`
Expected: PASS — the built `gg` rebases `work` onto `main` via the plan;
`main..work` becomes `[wip3, wip1 reworded]`.

> This is the slowest test in the package (it builds `gg`). It is the only
> place the CLI → engine → real `git rebase -i` → real `gg __rebase-seq/-message`
> stack runs as a shipped binary; it also exercises the Windows-quoting
> watch-item on Windows CI (the exec line quotes the `gg` path).

- [ ] **Step 3: Commit**

```bash
git add internal/cli/rebase_integration_test.go
git commit -m "test(cli): end-to-end gg rebase -i --plan through a built binary

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: docs + agent skill

**Files:**
- Modify: `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go`
- Modify: `README.md`, `CHANGELOG.md`

- [ ] **Step 1: Document the CLI flag in the agent skill**

In `internal/agentskill/using-gg.md`, extend the `gg rebase` bullet to mention
the interactive form:

```markdown
- `gg rebase [--branch <b>] [--on-conflict=keep|abort] <newbase>` — replay a
  branch's commits onto `<newbase>` … (existing text) …
  `gg rebase -i --plan <file> <newbase>` runs an **interactive** rebase from a
  plan file (a gg rebase-plan JSON: ordered `{sha, action: pick|reword|squash|drop,
  orig, new_msg}`); the TUI builds this plan interactively. Conflicts answer to
  `--on-conflict`.
```

- [ ] **Step 2: Bump the agent-skill version**

In `internal/agentskill/agentskill.go`, increment `const Version` by one (e.g.
`7` → `8` — use whatever the current value is).

- [ ] **Step 3: Verify agentskill tests**

Run: `go test ./internal/agentskill/`
Expected: PASS (the dogfood copy test may require `go build ./cmd/gg && ./gg init --update`; if it fails on the project SKILL.md copy, run that and re-add).

- [ ] **Step 4: README + CHANGELOG**

In `README.md`, update the `gg rebase` line in the CLI list to note
`-i --plan <file>`. In `CHANGELOG.md` under `## [Unreleased]` → `### Added`:

```markdown
#### Interactive rebase (engine + scriptable CLI)
- `gg rebase -i --plan <file> <newbase>` drives an interactive rebase from a
  plan (pick/reword/squash/drop + reorder), executed via git's interactive
  rebase with gg acting as the sequence editor. Squash composes a combined
  message (target subject + each squashed commit's message line-by-line). The
  working tree is preserved across the rebase (stash-wrap); conflicts pause for
  `git rebase --continue` or `--on-conflict=abort`. (The TUI editor that builds
  the plan lands next.)
```

- [ ] **Step 5: Refresh installed skill copies**

Run: `go build ./cmd/gg && ./gg init --update`
Expected: refreshes installed `using-gg` copies (no-op if none installed); the
dogfood project copy `.claude/skills/using-gg/SKILL.md` updates.

- [ ] **Step 6: Commit**

```bash
git add internal/agentskill/using-gg.md internal/agentskill/agentskill.go README.md CHANGELOG.md .claude/skills/using-gg/SKILL.md
git commit -m "docs: document gg rebase -i --plan; bump agentskill

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Final verification (after all tasks)

- [ ] `./test.sh race` — vet+gofmt clean, all unit + e2e green.
- [ ] `superpowers:finishing-a-development-branch`.
- [ ] **After merge, RE-RUN `./test.sh race` on merged `main`** — drift discipline (main is moving fast; expect to rebase).

---

## Self-Review

**1. Spec coverage (Slice 4, CLI half):**
- "thin scriptable `gg rebase -i <base> --plan <file>`" → Task 1. ✓
- "consumes the same serialized plan the TUI builds" → `rebaseplan.Unmarshal`. ✓
- "answered by `--on-conflict`" → the `rebase-conflict` policy mapping (reused from `SmartRebase`). ✓
- "frontend sets `GGBin` from `os.Executable()`" → Task 1. ✓
- Full-stack e2e → Task 2 subprocess test (the TOML harness can't host it; documented). ✓
- Docs + agentskill → Task 3. ✓

**2. Placeholder scan:** complete code throughout; the agentskill version bump says "use whatever the current value is" because main moves — the action (increment by one) is concrete.

**3. Type consistency:** `engine.InteractiveRebase{Branch, Onto, Plan, GGBin}` matches the Slice-3 op exactly; `rebaseplan.Plan`/`Entry{Sha,Action,Orig,NewMsg}` and `Marshal`/`Unmarshal` match Slice 2; the `rebase-conflict` decision id + `keep-conflicts`/`abort` options match the op and the existing `--on-conflict` mapping.

**Deferred to Slice 4b:** the TUI editor view (the 3rd pair-op "Interactive rebase {marked} onto {selected}", per-row actions, reorder, Reset/Cancel/Start) — builds the same `rebaseplan.Plan` in-memory and runs the op with `GGBin = os.Executable()`.
