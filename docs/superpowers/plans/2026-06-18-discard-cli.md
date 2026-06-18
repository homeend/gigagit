# `gg discard` CLI Command Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a scriptable `gg discard` command that throws away unstaged changes through the existing `engine.Discard` operation.

**Architecture:** A thin CLI frontend. `cmdDiscard` parses flags, reads `svc.Status()`, classifies the requested paths (untracked→Remove, tracked→Restore) or sets `All`, gates on `--yes`/TTY confirmation, and runs `engine.Discard` via `runOperation`. No engine changes.

**Tech Stack:** Go 1.26, `flag` package, the `cli` real-git test harness (`runCLI`/`newRepoDir`/`gitRun`), the declarative e2e TOML harness.

## Global Constraints

- No engine changes — `engine.Discard{Restore, Remove, All}` already ships.
- `gg discard` must be added to BOTH the `Run` switch AND the `var commands` map in `internal/cli/cli.go`; otherwise `cmd/gg/main.go`'s `IsCommand` check fails and `gg discard` silently launches the TUI.
- `--yes`/`-y` is required to proceed in both the targeted and `--all` cases; without it, prompt y/N on an interactive TTY, else refuse.
- All precondition/usage failures exit **2**; an op error exits 1; success exits 0.
- All requested paths are validated before anything runs — one bad path fails the whole command with nothing discarded.
- `--all` refuses when any conflict exists (mirrors the TUI's `D`).
- Any CLI surface change requires updating `internal/agentskill/using-gg.md` and bumping `agentskill.Version` (CLAUDE.md).
- TDD; run `./test.sh race` before merge.

---

### Task 1: `cmdDiscard` + wiring + unit tests

**Files:**
- Create: `internal/cli/discard.go`
- Modify: `internal/cli/cli.go` (switch case + `commands` map)
- Test: `internal/cli/discard_test.go`

**Interfaces:**
- Consumes: `svc.Status(ctx) (model.WorkingTreeStatus, error)`; `model.FileStatus{Path, Kind}`, `model.KindUntracked`/`KindUnmerged`; `st.Conflicts()`; `engine.Discard{Restore, Remove, All}`; `runOperation(ctx, svc, op, cliDecider{}, stderr)`; `finish(res, err, stdout, stderr)`; `stdinIsTerminal()`.
- Produces:
  - `func cmdDiscard(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int`
  - `func classifyDiscard(paths []string, st model.WorkingTreeStatus, stderr io.Writer) (restore, remove []string, code int)`
  - `func confirmDiscard(prompt string, in io.Reader, out io.Writer) bool`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/discard_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// discardConflictRepo builds a repo with one unresolved merge conflict on
// c.txt, returning the repo dir. Other helpers (newRepoDir, gitRun) are shared.
func discardConflictRepo(t *testing.T) string {
	t.Helper()
	dir := newRepoDir(t)
	gitRun(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("feat\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "feat")
	gitRun(t, dir, "checkout", "main")
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("main\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "main")
	// merge is expected to conflict (non-zero) — ignore the error.
	cmd := exec.Command("git", "-C", dir, "merge", "feat")
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	_ = cmd.Run()
	return dir
}

func TestDiscardAllYes(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)

	code, _, errb := runCLI(t, dir, "discard", "--all", "-y")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "README.md")); string(b) != "hi\n" {
		t.Fatalf("README.md = %q, want reverted", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new.txt should be removed, stat err = %v", err)
	}
}

func TestDiscardPathsYes(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)

	code, _, errb := runCLI(t, dir, "discard", "-y", "README.md", "new.txt")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "README.md")); string(b) != "hi\n" {
		t.Fatalf("README.md = %q, want reverted", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new.txt should be removed")
	}
}

func TestDiscardBareUsage(t *testing.T) {
	dir := newRepoDir(t)
	code, _, _ := runCLI(t, dir, "discard")
	if code != 2 {
		t.Fatalf("bare discard exit = %d, want 2", code)
	}
}

func TestDiscardAllNoYesNonInteractive(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644)
	// runCLI feeds an empty, non-TTY stdin, so without -y this must refuse.
	code, _, _ := runCLI(t, dir, "discard", "--all")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (refused without --yes)", code)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "README.md")); string(b) != "dirty\n" {
		t.Fatalf("README.md changed despite refusal: %q", b)
	}
}

func TestDiscardAllWithPathsRejected(t *testing.T) {
	dir := newRepoDir(t)
	code, _, _ := runCLI(t, dir, "discard", "--all", "README.md")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (--all + paths)", code)
	}
}

func TestDiscardUnmatchedPath(t *testing.T) {
	dir := newRepoDir(t)
	code, _, _ := runCLI(t, dir, "discard", "-y", "nope.txt")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (unmatched path)", code)
	}
}

func TestDiscardConflictedPathRejected(t *testing.T) {
	dir := discardConflictRepo(t)
	code, _, _ := runCLI(t, dir, "discard", "-y", "c.txt")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (conflicted path)", code)
	}
}

func TestDiscardAllRefusesOnConflict(t *testing.T) {
	dir := discardConflictRepo(t)
	code, _, _ := runCLI(t, dir, "discard", "--all", "-y")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (conflict present)", code)
	}
}

func TestConfirmDiscard(t *testing.T) {
	var out strings.Builder
	if !confirmDiscard("? ", strings.NewReader("y\n"), &out) {
		t.Fatal(`"y" should confirm`)
	}
	if !confirmDiscard("? ", strings.NewReader("YES\n"), &out) {
		t.Fatal(`"YES" should confirm`)
	}
	if confirmDiscard("? ", strings.NewReader("n\n"), &out) {
		t.Fatal(`"n" should not confirm`)
	}
	if confirmDiscard("? ", strings.NewReader(""), &out) {
		t.Fatal(`empty should not confirm`)
	}
}
```

Add `"os/exec"` to the import block (used by `discardConflictRepo`).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run Discard -v`
Expected: FAIL — `discard` is an unknown command (exit 2 for the wrong reason) and `confirmDiscard` is undefined (compile error). Compile error is the dominant failure.

- [ ] **Step 3: Implement `cmdDiscard`**

Create `internal/cli/discard.go`:

```go
package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/model"
)

// cmdDiscard implements `gg discard [--yes] (--all | <path>...)`. It throws away
// unstaged changes: tracked edits are restored from the index (staged hunks
// kept), untracked files deleted. Destructive, so it requires --yes — or, on an
// interactive terminal, a y/N confirmation.
func cmdDiscard(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("discard", flag.ContinueOnError)
	fs.SetOutput(stderr)
	all := fs.Bool("all", false, "discard ALL unstaged changes")
	yes := fs.Bool("yes", false, "confirm the discard (required; or answer y/N on a TTY)")
	fs.BoolVar(yes, "y", false, "alias for --yes")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	paths := fs.Args()

	if *all && len(paths) > 0 {
		fmt.Fprintln(stderr, "discard: --all takes no paths")
		return 2
	}
	if !*all && len(paths) == 0 {
		fmt.Fprintln(stderr, "usage: gg discard [--yes] (--all | <path>...)")
		return 2
	}

	st, err := svc.Status(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	var op engine.Discard
	var summary string
	if *all {
		if len(st.Conflicts()) > 0 {
			fmt.Fprintln(stderr, "discard: resolve conflicts before discarding all")
			return 2
		}
		op = engine.Discard{All: true}
		summary = "all unstaged changes"
	} else {
		restore, remove, code := classifyDiscard(paths, st, stderr)
		if code != 0 {
			return code
		}
		op = engine.Discard{Restore: restore, Remove: remove}
		summary = fmt.Sprintf("%d file(s)", len(restore)+len(remove))
	}

	if !*yes {
		if !stdinIsTerminal() {
			fmt.Fprintln(stderr, "discard: pass --yes to confirm (this is destructive)")
			return 2
		}
		if !confirmDiscard("Discard "+summary+"? [y/N] ", stdin, stderr) {
			fmt.Fprintln(stderr, "discard: aborted")
			return 0
		}
	}

	res, err := runOperation(context.Background(), svc, op, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

// classifyDiscard splits the requested paths into restore (tracked) and remove
// (untracked) lists by looking each up in st. All paths are validated before
// returning: an unmatched or conflicted path fails the whole command (code 2)
// with nothing discarded. code is 0 on success.
func classifyDiscard(paths []string, st model.WorkingTreeStatus, stderr io.Writer) (restore, remove []string, code int) {
	byPath := make(map[string]model.FileStatus, len(st.Files))
	for _, f := range st.Files {
		byPath[f.Path] = f
	}
	for _, p := range paths {
		f, ok := byPath[p]
		if !ok {
			fmt.Fprintf(stderr, "discard: no unstaged change for %q\n", p)
			return nil, nil, 2
		}
		switch f.Kind {
		case model.KindUnmerged:
			fmt.Fprintf(stderr, "discard: %q is conflicted; resolve it first\n", p)
			return nil, nil, 2
		case model.KindUntracked:
			remove = append(remove, p)
		default:
			restore = append(restore, p)
		}
	}
	return restore, remove, 0
}

// confirmDiscard prints prompt to out and returns true only when the first line
// read from in is an affirmative (y/yes, case-insensitive).
func confirmDiscard(prompt string, in io.Reader, out io.Writer) bool {
	fmt.Fprint(out, prompt)
	line, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}
```

- [ ] **Step 4: Wire it into the dispatcher**

In `internal/cli/cli.go`, add the switch case (alongside the others in `Run`):

```go
	case "discard":
		return cmdDiscard(svc, rest, stdin, stdout, stderr)
```

And add `discard` to the `var commands` map so `cmd/gg` routes it to the CLI (not the TUI):

```go
var commands = map[string]bool{
	"status": true, "commit": true, "pull": true, "push": true,
	"switch": true, "branch": true, "stash": true, "undo": true, "merge": true, "rebase": true, "worktree": true,
	"discard": true,
	"inspect": true, "repo": true, "init": true,
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run Discard -v`
Expected: PASS (all nine tests).

- [ ] **Step 6: Run the full cli package**

Run: `go test ./internal/cli/`
Expected: PASS (no regressions).

- [ ] **Step 7: Commit**

```bash
git add internal/cli/discard.go internal/cli/cli.go internal/cli/discard_test.go
git commit -m "feat(cli): gg discard command (restore tracked + clean untracked)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 2: Docs (agentskill + changelog + readme) + e2e scenario

**Files:**
- Modify: `internal/agentskill/using-gg.md` (add the command)
- Modify: `internal/agentskill/agentskill.go` (bump `Version` 9 → 10)
- Create: `e2e/scenarios/s42_discard_all.toml`
- Modify: `CHANGELOG.md`, `README.md`

**Interfaces:** none (docs + integration test only).

- [ ] **Step 1: Add the e2e scenario (the real CLI→engine→git test)**

Create `e2e/scenarios/s42_discard_all.toml`:

```toml
name = "discard --all: reverts a tracked edit and removes an untracked file"

[input]
steps = [
  { write = "README.md", content = "hi\n" },
  { commit = "initial" },
  { write = "README.md", content = "hi\nwip\n" },
  { write = "scratch.txt", content = "junk\n" },
]

[[run]]
cmd  = ["discard", "--all", "-y"]
exit = 0

[expect]
branch = "main"
clean  = true

[expect.files]
"README.md" = "hi\n"          # the unstaged edit was reverted
```

- [ ] **Step 2: Run the e2e scenario to verify it passes**

Run: `go test ./e2e/ -run 'TestScenarios/s42' -v` (or the package's scenario runner — check `e2e/*_test.go` for the exact test name with `grep -n "func Test" e2e/*_test.go` and target it).
Expected: PASS. If it fails with the TUI launching / "unknown command", revisit Task 1 Step 4 (the `commands` map).

- [ ] **Step 3: Add the command to the agent skill**

In `internal/agentskill/using-gg.md`, under `## Commands` (after the `gg stash` block), add:

```markdown
- `gg discard [--yes|-y] (--all | <path>...)` — throw away unstaged changes:
  tracked edits are reverted (staged hunks kept), untracked files deleted.
  Destructive, so `--yes` is required (or a y/N prompt on a TTY). `--all`
  discards everything unstaged and refuses while the repo is conflicted; named
  paths must appear in `gg status` and a conflicted path is rejected.
```

- [ ] **Step 4: Bump the agent-skill version**

In `internal/agentskill/agentskill.go`, change:

```go
const Version = 9
```
to:
```go
const Version = 10
```

- [ ] **Step 5: Run the agentskill tests**

Run: `go test ./internal/agentskill/`
Expected: PASS (the version-marker tests track the new `Version`).

- [ ] **Step 6: Update CHANGELOG.md**

Under `## [Unreleased]` → `### Added`, add:

```markdown
- CLI: `gg discard [--yes] (--all | <path>...)` discards unstaged changes —
  reverting tracked edits (staged hunks kept) and deleting untracked files —
  through the same engine operation as the TUI's `d`/`D`. Requires `--yes`
  (or a y/N prompt on a terminal); `--all` refuses while the repo is conflicted.
```

- [ ] **Step 7: Update README.md**

Run `grep -n 'gg stash\|gg commit\|## CLI\|gg switch' README.md` to find the CLI command list. Add a `gg discard` line mirroring the others' style next to `gg stash`. If README has no CLI command list (only the TUI keybinding table), skip and note it in the commit message.

- [ ] **Step 8: Run the full staged gate with race**

Run: `./test.sh race`
Expected: vet + gofmt clean, all unit tests PASS, e2e PASS (including s42).

- [ ] **Step 9: Commit**

```bash
git add internal/agentskill/using-gg.md internal/agentskill/agentskill.go e2e/scenarios/s42_discard_all.toml CHANGELOG.md README.md
git commit -m "docs(cli): document gg discard; e2e scenario; bump agentskill v10

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

## Self-Review

**Spec coverage:**
- Command surface (`--all` / paths / bare) → Task 1 (`cmdDiscard` flag parsing). ✓
- `--all` + paths mutually exclusive → Task 1 (`TestDiscardAllWithPathsRejected`). ✓
- `--yes` required, TTY prompt fallback, non-interactive refusal → Task 1 (`confirmDiscard`, `stdinIsTerminal` gate, `TestDiscardAllNoYesNonInteractive`). ✓
- Classification untracked→Remove / tracked→Restore → Task 1 (`classifyDiscard`, `TestDiscardPathsYes`). ✓
- Unmatched path / conflicted path validated up front → Task 1 (`TestDiscardUnmatchedPath`, `TestDiscardConflictedPathRejected`). ✓
- `--all` refuses on conflict → Task 1 (`TestDiscardAllRefusesOnConflict`). ✓
- Exit codes (2 for preconditions) → asserted in each negative test. ✓
- `commands`-map wiring → Task 1 Step 4 + Task 2 e2e scenario guards it. ✓
- agentskill update + version bump → Task 2 Steps 3-4. ✓
- CHANGELOG + README → Task 2 Steps 6-7. ✓
- No engine changes → only `internal/cli`, docs, and `e2e/scenarios` touched. ✓

**Placeholder scan:** No TBD/TODO; full code in every code step. The two
"check the exact name" notes (e2e test runner name, README CLI list) are
explicit grep-to-resolve instructions, not vague hand-waves.

**Type consistency:** `cmdDiscard(svc, args, stdin, stdout, stderr) int`,
`classifyDiscard(paths, st, stderr) (restore, remove []string, code int)`, and
`confirmDiscard(prompt, in, out) bool` signatures match between the
implementation (Task 1 Step 3) and the tests (Step 1) and dispatcher (Step 4).
