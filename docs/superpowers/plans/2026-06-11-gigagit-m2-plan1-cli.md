# gigagit M2 — Plan 1: CLI frontend — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a scriptable CLI that drives the existing engine operations as `gg` subcommands (`status`, `commit`, `pull`, `push`, `switch`, `stash`, `undo`), sharing the same `engine` the TUI uses — proving the engine is genuinely frontend-agnostic.

**Architecture:** A new `internal/cli` package with a testable `Run(workdir, args, stdout, stderr) int` dispatcher. Each command parses its own `flag.FlagSet`, builds an `engine.Operation`, and runs it via a shared `runOperation` helper that streams `Progress` events to stderr and resolves forks with a `cliDecider` (policy from flags; interactive stdin prompt as a fallback; a clear error when neither is available — the non-blocking contract from spec §4). `cmd/gg` delegates known subcommands to `cli.Run` and still launches the TUI when invoked with no subcommand.

**Tech Stack:** Go 1.26, standard library only (`flag`, `io`, `context`); existing internal packages (`engine`, `git`, `gitexec`, `observ`, `model`). Tests call `cli.Run` directly against real throwaway repos and assert exit codes + output + repo state.

---

## Shared interfaces

```go
// internal/cli
func Run(workdir string, args []string, stdout, stderr io.Writer) int // exit code
func openRepo(workdir string) *git.Repo

type cliDecider struct {
	policy      map[string]string // decision ID -> chosen option
	in          io.Reader
	out         io.Writer
	interactive bool
}
func (d cliDecider) Decide(ctx context.Context, req engine.DecisionRequest) (engine.DecisionResponse, error)

// runOperation runs op, streaming Progress to progress, and returns the result.
func runOperation(ctx context.Context, repo *git.Repo, op engine.Operation, dec engine.Decider, progress io.Writer) (engine.Result, error)
```

---

## Task 1: CLI core — decider, runOperation, openRepo

**Files:**
- Create: `internal/cli/core.go`
- Test: `internal/cli/core_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/core_test.go`:
```go
package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/engine"
)

func newRepoDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "initial")
	return dir
}

func TestCLIDeciderPolicyAnswers(t *testing.T) {
	d := cliDecider{policy: map[string]string{"non-fast-forward": "rebase"}}
	resp, err := d.Decide(context.Background(), engine.DecisionRequest{ID: "non-fast-forward"})
	if err != nil || resp.Option != "rebase" {
		t.Fatalf("resp=%v err=%v, want rebase", resp, err)
	}
}

func TestCLIDeciderNonInteractiveUnansweredErrors(t *testing.T) {
	d := cliDecider{policy: map[string]string{}, interactive: false}
	_, err := d.Decide(context.Background(), engine.DecisionRequest{ID: "non-fast-forward", Options: []string{"rebase", "abort"}})
	if err == nil {
		t.Fatal("non-interactive unanswered decision must error")
	}
}

func TestRunOperationCommitsAndStreamsProgress(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	repo := openRepo(dir)

	var prog bytes.Buffer
	res, err := runOperation(context.Background(), repo, engine.Commit{Message: "second", All: true}, cliDecider{}, &prog)
	if err != nil {
		t.Fatalf("runOperation: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if !strings.Contains(prog.String(), "committing") {
		t.Fatalf("progress missing 'committing':\n%s", prog.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/`
Expected: FAIL — undefined `cliDecider`, `runOperation`, `openRepo`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/core.go`:
```go
// Package cli implements gigagit's scriptable command-line frontend over the
// shared engine.
package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

// openRepo builds a Repo rooted at workdir with an always-on span ring.
func openRepo(workdir string) *git.Repo {
	return &git.Repo{Runner: gitexec.NewExecRunner("git", workdir, observ.NewRing(200))}
}

// cliDecider resolves engine forks from a flag-supplied policy, falling back to
// an interactive stdin prompt, and erroring when neither can answer.
type cliDecider struct {
	policy      map[string]string
	in          io.Reader
	out         io.Writer
	interactive bool
}

func (d cliDecider) Decide(_ context.Context, req engine.DecisionRequest) (engine.DecisionResponse, error) {
	if opt, ok := d.policy[req.ID]; ok {
		return engine.DecisionResponse{Option: opt}, nil
	}
	if !d.interactive || d.in == nil {
		return engine.DecisionResponse{}, fmt.Errorf(
			"%s needs a decision (options: %s); rerun with the matching flag",
			req.ID, strings.Join(req.Options, ", "))
	}
	fmt.Fprintf(d.out, "%s\n  options: %s\n> ", req.Prompt, strings.Join(req.Options, ", "))
	line, _ := bufio.NewReader(d.in).ReadString('\n')
	choice := strings.TrimSpace(line)
	for _, o := range req.Options {
		if o == choice {
			return engine.DecisionResponse{Option: o}, nil
		}
	}
	return engine.DecisionResponse{}, fmt.Errorf("invalid choice %q for %s", choice, req.ID)
}

// runOperation runs op, printing each Progress step to progress, and returns the
// operation result. The op runs in a goroutine so events stream live; decisions
// are resolved by dec (which may prompt).
func runOperation(ctx context.Context, repo *git.Repo, op engine.Operation, dec engine.Decider, progress io.Writer) (engine.Result, error) {
	events := make(chan engine.Event, 32)
	var (
		res engine.Result
		err error
	)
	done := make(chan struct{})
	go func() {
		res, err = op.Run(ctx, engine.OpDeps{Repo: repo, Events: events, Decider: dec})
		close(events)
		close(done)
	}()
	for e := range events {
		if p, ok := e.(engine.Progress); ok {
			if p.Detail != "" {
				fmt.Fprintf(progress, "→ %s: %s\n", p.Step, p.Detail)
			} else {
				fmt.Fprintf(progress, "→ %s\n", p.Step)
			}
		}
	}
	<-done
	return res, err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ && go vet ./internal/cli/ && gofmt -l internal/cli`
Expected: PASS; gofmt clean.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/core.go internal/cli/core_test.go
git commit -m "feat: add CLI core (decider, runOperation, openRepo)"
```

---

## Task 2: Dispatcher + `status` and `commit`

**Files:**
- Create: `internal/cli/cli.go`
- Test: `internal/cli/cli_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cli_test.go`:
```go
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(t *testing.T, workdir string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(workdir, args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestStatusCommand(t *testing.T) {
	dir := newRepoDir(t)
	code, out, _ := runCLI(t, dir, "status")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "main") {
		t.Fatalf("status output missing branch:\n%s", out)
	}
}

func TestCommitCommand(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	code, _, errb := runCLI(t, dir, "commit", "-m", "second", "--all")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	// Tree should be clean afterward.
	code, out, _ := runCLI(t, dir, "status")
	if code != 0 || !strings.Contains(out, "clean") {
		t.Fatalf("expected clean status after commit, got:\n%s", out)
	}
}

func TestCommitRequiresMessage(t *testing.T) {
	dir := newRepoDir(t)
	code, _, _ := runCLI(t, dir, "commit")
	if code == 0 {
		t.Fatal("commit without -m should fail")
	}
}

func TestUnknownCommand(t *testing.T) {
	dir := newRepoDir(t)
	code, _, errb := runCLI(t, dir, "frobnicate")
	if code == 0 {
		t.Fatal("unknown command should return non-zero")
	}
	if !strings.Contains(errb, "unknown") {
		t.Fatalf("expected 'unknown' in stderr:\n%s", errb)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestStatus|TestCommit|TestUnknown'`
Expected: FAIL — undefined `Run`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/cli.go`:
```go
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gigagit/gg/internal/engine"
)

// Run dispatches a CLI subcommand against the repo at workdir, writing to
// stdout/stderr, and returns a process exit code.
func Run(workdir string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg <command> [args]")
		return 2
	}
	repo := openRepo(workdir)
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "status":
		return cmdStatus(repo, stdout, stderr)
	case "commit":
		return cmdCommit(repo, rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", cmd)
		return 2
	}
}

func cmdStatus(repo *repoT, stdout, stderr io.Writer) int {
	st, err := repo.Status(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	line := "on branch " + st.Branch
	if st.Upstream != "" {
		line += fmt.Sprintf(" (%s ↑%d ↓%d)", st.Upstream, st.Ahead, st.Behind)
	}
	fmt.Fprintln(stdout, line)
	if c := st.Counts(); c.Staged+c.Unstaged+c.Untracked+c.Conflicted == 0 {
		fmt.Fprintln(stdout, "working tree clean")
		return 0
	}
	for _, f := range st.Files {
		x, y := f.Staged, f.Unstaged
		if x == 0 {
			x = ' '
		}
		if y == 0 {
			y = ' '
		}
		fmt.Fprintf(stdout, "%c%c %s\n", x, y, f.Path)
	}
	return 0
}

func cmdCommit(repo *repoT, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("commit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	msg := fs.String("m", "", "commit message (required)")
	all := fs.Bool("all", false, "stage modified/deleted tracked files first (-a)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *msg == "" {
		fmt.Fprintln(stderr, "commit: -m message is required")
		return 2
	}
	res, err := runOperation(context.Background(), repo,
		engine.Commit{Message: *msg, All: *all}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

// finish prints the result summary (or error) and maps to an exit code.
func finish(res engine.Result, err error, stdout, stderr io.Writer) int {
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if res.Summary != "" {
		fmt.Fprintln(stdout, "✓ "+res.Summary)
	}
	return 0
}
```

Note: `repoT` is a short alias for `*git.Repo`'s element type to keep signatures readable. Add at the top of `cli.go` (after imports), and add the `git` import:
```go
import "github.com/gigagit/gg/internal/git"
type repoT = git.Repo
```
(So `*repoT` == `*git.Repo`. Adjust the import block to include `git`.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ && go vet ./internal/cli/ && gofmt -l internal/cli`
Expected: PASS; gofmt clean.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cli.go internal/cli/cli_test.go
git commit -m "feat: add CLI dispatcher with status and commit commands"
```

---

## Task 3: `pull`, `push`, `switch`, `stash`, `undo`

**Files:**
- Create: `internal/cli/ops.go`
- Modify: `internal/cli/cli.go` (register the new commands)
- Test: `internal/cli/ops_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/ops_test.go`:
```go
package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// cloneBehind builds origin (main at v2) and a clone whose main is one behind.
func cloneBehind(t *testing.T) string {
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
	gitIn(t, seed, "commit", "-m", "v1")
	gitIn(t, seed, "push", "-u", "origin", "main")
	gitIn(t, root, "clone", origin, clone)
	gitIn(t, clone, "checkout", "main")
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v2\n"), 0o644)
	gitIn(t, seed, "add", ".")
	gitIn(t, seed, "commit", "-m", "v2")
	gitIn(t, seed, "push", "origin", "main")
	return clone
}

func TestPullFastForward(t *testing.T) {
	clone := cloneBehind(t)
	code, out, errb := runCLI(t, clone, "pull")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	if !strings.Contains(out, "pulled") {
		t.Fatalf("expected 'pulled' in output:\n%s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(clone, "f.txt")); string(b) != "v2\n" {
		t.Fatalf("f.txt = %q, want v2 after pull", string(b))
	}
}

func TestSwitchCommand(t *testing.T) {
	dir := newRepoDir(t)
	gitIn(t, dir, "branch", "feature")
	code, out, errb := runCLI(t, dir, "switch", "feature")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	if !strings.Contains(out, "feature") {
		t.Fatalf("expected 'feature' in output:\n%s", out)
	}
}

func TestSwitchRequiresBranch(t *testing.T) {
	dir := newRepoDir(t)
	code, _, _ := runCLI(t, dir, "switch")
	if code == 0 {
		t.Fatal("switch without a branch should fail")
	}
}

func TestStashAndUndo(t *testing.T) {
	dir := newRepoDir(t)
	// stash a change
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644)
	if code, _, errb := runCLI(t, dir, "stash"); code != 0 {
		t.Fatalf("stash exit = %d (stderr: %s)", code, errb)
	}
	// make a commit, then undo it
	gitIn(t, dir, "commit", "--allow-empty", "-m", "to-undo")
	code, out, errb := runCLI(t, dir, "undo")
	if code != 0 {
		t.Fatalf("undo exit = %d (stderr: %s)", code, errb)
	}
	if !strings.Contains(out, "undid") {
		t.Fatalf("expected 'undid' in output:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestPull|TestSwitch|TestStash'`
Expected: FAIL — pull/switch/stash/undo not registered.

- [ ] **Step 3: Implement**

Create `internal/cli/ops.go`:
```go
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gigagit/gg/internal/engine"
)

func cmdPull(repo *repoT, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pull", flag.ContinueOnError)
	fs.SetOutput(stderr)
	background := fs.Bool("background", false, "update the branch's ref without checking it out")
	onConflict := fs.String("on-conflict", "", "how to resolve divergence: rebase|merge|abort")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	intent := engine.PullAndStay
	if *background {
		intent = engine.PullInBackground
	}
	branch := ""
	if fs.NArg() > 0 {
		branch = fs.Arg(0)
	}
	policy := map[string]string{}
	if *onConflict != "" {
		policy["non-fast-forward"] = *onConflict
	}
	dec := cliDecider{policy: policy}
	res, err := runOperation(context.Background(), repo,
		engine.SmartPull{Branch: branch, Intent: intent}, dec, stderr)
	return finish(res, err, stdout, stderr)
}

func cmdPush(repo *repoT, args []string, stdout, stderr io.Writer) int {
	cur, err := repo.CurrentBranch(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if cur == "" {
		fmt.Fprintln(stderr, "push: detached HEAD; cannot push")
		return 1
	}
	res, err := runOperation(context.Background(), repo,
		engine.Push{Remote: "origin", Branch: cur, SetUpstream: true}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

func cmdSwitch(repo *repoT, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || args[0] == "" {
		fmt.Fprintln(stderr, "switch: a branch name is required")
		return 2
	}
	res, err := runOperation(context.Background(), repo,
		engine.SmartSwitch{Branch: args[0]}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

func cmdStash(repo *repoT, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("stash", flag.ContinueOnError)
	fs.SetOutput(stderr)
	msg := fs.String("m", "gg stash", "stash message")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	res, err := runOperation(context.Background(), repo,
		engine.Stash{Message: *msg}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

func cmdUndo(repo *repoT, args []string, stdout, stderr io.Writer) int {
	res, err := runOperation(context.Background(), repo,
		engine.UndoLastCommit{}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}
```

In `internal/cli/cli.go`, register the new commands in the `switch cmd` block (alongside `status`/`commit`):
```go
	case "pull":
		return cmdPull(repo, rest, stdout, stderr)
	case "push":
		return cmdPush(repo, rest, stdout, stderr)
	case "switch":
		return cmdSwitch(repo, rest, stdout, stderr)
	case "stash":
		return cmdStash(repo, rest, stdout, stderr)
	case "undo":
		return cmdUndo(repo, rest, stdout, stderr)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ && go vet ./internal/cli/ && gofmt -l internal/cli`
Expected: PASS; gofmt clean.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/ops.go internal/cli/cli.go internal/cli/ops_test.go
git commit -m "feat: add pull/push/switch/stash/undo CLI commands"
```

---

## Task 4: Wire `cmd/gg` to the CLI + interactive decider

**Files:**
- Modify: `internal/cli/cli.go` (make the dispatcher's decider interactive when stdin is a TTY)
- Modify: `cmd/gg/main.go` (route known subcommands to `cli.Run`)
- Test: `internal/cli/cli_test.go` (add a help/usage assertion)

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/cli_test.go`:
```go
func TestNoArgsreturnsUsage(t *testing.T) {
	dir := newRepoDir(t)
	code, _, errb := runCLI(t, dir) // no subcommand
	if code == 0 {
		t.Fatal("no command should return non-zero")
	}
	if !strings.Contains(errb, "usage") {
		t.Fatalf("expected usage on stderr:\n%s", errb)
	}
}

func TestKnownCommandsList(t *testing.T) {
	// IsCommand reports whether a token is a gg CLI subcommand (used by cmd/gg
	// to decide between the CLI and launching the TUI).
	for _, c := range []string{"status", "commit", "pull", "push", "switch", "stash", "undo", "inspect"} {
		if !IsCommand(c) {
			t.Fatalf("%q should be a known command", c)
		}
	}
	if IsCommand("definitely-not-a-command") {
		t.Fatal("unknown token must not be a command")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestNoArgs|TestKnownCommands'`
Expected: FAIL — undefined `IsCommand`.

- [ ] **Step 3: Implement**

In `internal/cli/cli.go`, add the exported `IsCommand` and a known-commands set, and make `Run` build an interactive decider for `pull` when stdin is a terminal. First add (package level):
```go
import (
	"os"
	// ... existing imports ...
	"golang.org/x/term" // NOTE: see Step 3a — if adding a dep is undesired, use the fallback below
)

var commands = map[string]bool{
	"status": true, "commit": true, "pull": true, "push": true,
	"switch": true, "stash": true, "undo": true, "inspect": true,
}

// IsCommand reports whether tok is a gg CLI subcommand.
func IsCommand(tok string) bool { return commands[tok] }
```

**Step 3a — avoid a new dependency for TTY detection.** Do NOT add `golang.org/x/term`. Instead detect a terminal with a tiny stdlib check. Add to `cli.go`:
```go
import (
	"os"
	// ... existing ...
)

// stdinIsTerminal reports whether os.Stdin is an interactive terminal.
func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
```
(Remove the `golang.org/x/term` import entirely; it was illustrative.)

Then make `cmdPull` build an interactive decider when no policy flag was given and stdin is a TTY. Change the `dec` construction in `cmdPull` (ops.go) to:
```go
	dec := cliDecider{policy: policy, in: os.Stdin, out: stderr, interactive: stdinIsTerminal()}
```
and add `"os"` to `ops.go` imports. (Other commands keep `cliDecider{}` — their decisions, like stash-pop conflicts, are terminal and should surface as errors rather than prompt.)

Note: `inspect` is listed in `commands` so `cmd/gg` routes it, but `Run` itself doesn't handle `inspect` (the existing `app.Inspect` path in `cmd/gg` does). Keep `inspect` OUT of `Run`'s switch (it will hit the default `unknown` branch if ever passed to `Run`); `cmd/gg` must route `inspect` to its existing handler BEFORE calling `cli.Run`. See Step 4.

- [ ] **Step 4: Wire `cmd/gg/main.go`**

Rewrite `cmd/gg/main.go` so it routes `inspect` to the existing inspect path, other known commands to `cli.Run`, and no/unknown-subcommand to the TUI:
```go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gigagit/gg/internal/app"
	"github.com/gigagit/gg/internal/cli"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
	"github.com/gigagit/gg/internal/tui"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "inspect" {
		runInspect(args[1:])
		return
	}
	if len(args) > 0 && cli.IsCommand(args[0]) {
		os.Exit(cli.Run(".", args, os.Stdout, os.Stderr))
	}
	// No (or unknown) subcommand: launch the TUI.
	ring := observ.NewRing(200)
	repo := &git.Repo{Runner: gitexec.NewExecRunner("git", ".", ring)}
	defer func() {
		if r := recover(); r != nil {
			path := filepath.Join(os.TempDir(), fmt.Sprintf("gg-panic-%d.json", time.Now().Unix()))
			_ = app.DumpRepo(context.Background(), path, repo, ring, []string{fmt.Sprintf("panic: %v", r)})
			fmt.Fprintf(os.Stderr, "gg panicked; debug dump written to %s\n", path)
			panic(r)
		}
	}()
	if err := tui.Run(repo); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func runInspect(args []string) {
	fs := flag.NewFlagSet("inspect", flag.ExitOnError)
	dumpPath := fs.String("debug-dump", "", "write a debug dump JSON file to this path")
	trace := fs.Bool("trace", false, "enable verbose timing trace to stderr")
	_ = fs.Parse(args)

	opts := app.Options{WorkDir: ".", Stdout: os.Stdout, DumpPath: *dumpPath}
	if *trace || os.Getenv("GG_TRACE") == "1" {
		opts.Trace = os.Stderr
	}
	if err := app.Inspect(context.Background(), opts); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 5: Verify build + full suite**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l internal cmd`
Expected: build OK; all PASS; vet clean; gofmt prints nothing.

- [ ] **Step 6: Smoke check**

Run:
```bash
go build -o /tmp/gg ./cmd/gg
/tmp/gg status
/tmp/gg --help 2>&1 | head -1 || true   # not a command → TUI path; do NOT run bare gg headless
```
Actually only verify the CLI commands (they don't need a TTY):
```bash
/tmp/gg status && echo "---" && /tmp/gg frobnicate; echo "exit=$?"
```
Expected: `status` prints the branch/working-tree summary; `frobnicate` is unknown → but note `frobnicate` is not a known command so `cmd/gg` would launch the TUI (blocks!). DO NOT run an unknown token through the built binary headless. Only run `/tmp/gg status` (and other real commands) for the smoke check. Paste `gg status` output.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cli.go internal/cli/ops.go internal/cli/cli_test.go cmd/gg/main.go
git commit -m "feat: route gg subcommands to the CLI; interactive pull decider"
```

---

## Self-Review

**Spec coverage (M2 CLI half of the roadmap):**
- CLI frontend sharing the engine: `Run` + per-command `engine.Operation`s via `runOperation` → Tasks 1–3.
- Commands: status, commit, pull (with `--background`/`--on-conflict`), push, switch, stash, undo → Tasks 2–3.
- §4 decider contract on the CLI: policy from flags; interactive prompt fallback; non-interactive unanswered → error (never blocks) → Tasks 1, 4.
- `cmd/gg` dispatch: inspect → app, known commands → cli, else TUI → Task 4.

**Deferred (note):** **Workspaces** (multi-repo grouping + multi-repo operations) is the other half of M2 — its own design + plan next. Per-command `--help` text beyond flag defaults, shell completion, and colored CLI output are post-M2 polish.

**Placeholder scan:** none — every step has complete code. (Step 3's `golang.org/x/term` line is explicitly called out as illustrative and replaced by the stdlib `stdinIsTerminal` in Step 3a — the implemented code adds no dependency.)

**Type consistency:** `repoT = git.Repo` alias used consistently (`*repoT` == `*git.Repo`); `runOperation`/`cliDecider`/`finish` signatures match call sites; `engine.SmartPull{Branch,Intent}`, `engine.Push{Remote,Branch,SetUpstream}`, `engine.SmartSwitch{Branch}`, `engine.Stash{Message}`, `engine.UndoLastCommit{}`, `engine.Commit{Message,All}` match the engine; `IsCommand`/`Run` exported signatures match `cmd/gg` usage.

---

## Plan sequence

- M1 (Foundation → Interactive TUI) ✅ complete.
- **M2 Plan 1 — CLI frontend** (this document).
- M2 Plan 2 — Workspaces (multi-repo): own design + plan.
- M3 — MCP server + heavy ops (staging, interactive rebase, conflict editor, visual graph, sparse-checkout management).
