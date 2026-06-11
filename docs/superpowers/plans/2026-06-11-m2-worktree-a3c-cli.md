# M2 Worktree A3c — `gg worktree` CLI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `gg worktree list` and `gg worktree add [start-point]` to the CLI, sharing the worktree name-resolution logic with the TUI popup (no duplication) by extracting it into a new `internal/worktree` package first.

**Architecture:** Extract the popup's pure resolve logic (`Templates` + `Resolve` + label/seq extraction + repo-name + seq-peek) into `internal/worktree`, refactor the TUI popup to use it, then build the CLI commands on the same package. `gg worktree add` loads config, resolves the default templates (prompting on stdin for each `<user:LABEL>`), creates via `engine.CreateWorktree`, bumps `<seq>` counters on success, prints the branch+path, and writes the new path to `--cwd-file` (so a `gg`-wrapped shell follows it).

**Tech Stack:** Go 1.26, existing `internal/{config,template,engine,git,tui,cli}`, `cmd/gg`. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-06-11-worktree-management-design.md` §11 (CLI: `gg worktree add [start-point]` prompts stdin for `<user:>`, creates, prints; `gg worktree list`; `--cwd-file` written so a wrapped shell follows).

**Conventions (read before starting):**
- TDD red→green. After each task: `go test ./...`, `go vet ./...`, `gofmt -l internal cmd` clean.
- LF line endings only (`.gitattributes`; Windows-mounted drive).
- Commit messages end with a `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` trailer.
- Plain `fmt.Errorf`. CLI subcommands follow the pattern in `internal/cli/cli.go` (a `cmdX(repo, args, stdout, stderr) int` returning an exit code; `finish(res, err, ...)` maps results).
- Engine/git tests use real repos (`newRepo`/`newTestRepo`). CLI tests use the `internal/cli` test helpers; see `internal/cli/cli_test.go`.

---

## File Structure

- `internal/worktree/worktree.go` (new): `Templates`, `(Templates).Labels`, `(Templates).SeqNames`, `Resolve`, `RepoName`, `PeekSeqs` — the shared, pure resolve logic.
- `internal/tui/worktree_popup.go` (modify): use `internal/worktree`; drop the now-duplicated local funcs.
- `internal/cli/cli.go` (modify): add `stdin`/`cwdFile` to `Run`, register `worktree`, dispatch to `cmdWorktree`.
- `internal/cli/worktree.go` (new): `cmdWorktree` (`list` + `add`).
- `cmd/gg/main.go` (modify): pass `os.Stdin` and the extracted `cwdFile` to `cli.Run`.
- Tests: `internal/worktree/worktree_test.go`, `internal/cli/worktree_test.go`, plus edits to `internal/cli/cli_test.go`.

---

## Task 1: `internal/worktree` package — shared resolve logic

**Files:** Create `internal/worktree/worktree.go`, `internal/worktree/worktree_test.go`.

- [ ] **Step 1: Write the failing test** — `internal/worktree/worktree_test.go`

```go
package worktree

import (
	"math/rand/v2"
	"reflect"
	"testing"
	"time"

	"github.com/gigagit/gg/internal/template"
)

func testCtx() template.Ctx {
	return template.Ctx{
		ParentBranch: "main",
		Repo:         "aaa",
		Seqs:         map[string]int{"issue": 7},
		Now:          func() time.Time { return time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC) },
		Rand:         rand.New(rand.NewPCG(1, 2)),
	}
}

func TestResolveTwoPhase(t *testing.T) {
	tm := Templates{Branch: "issue/<seq:issue>", Path: "../<repo>.worktrees/<branch>"}
	branch, path, err := Resolve(tm, "", nil, testCtx())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if branch != "issue/7" || path != "../aaa.worktrees/issue-7" {
		t.Fatalf("got (%q,%q), want (issue/7, ../aaa.worktrees/issue-7)", branch, path)
	}
}

func TestResolveFixedBranch(t *testing.T) {
	tm := Templates{Branch: "ignored", Path: "wt/<branch>"}
	branch, path, err := Resolve(tm, "hand/edited", nil, testCtx())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if branch != "hand/edited" || path != "wt/hand-edited" {
		t.Fatalf("got (%q,%q), want (hand/edited, wt/hand-edited)", branch, path)
	}
}

func TestResolvePropagatesError(t *testing.T) {
	if _, _, err := Resolve(Templates{Branch: "b-<bogus>", Path: "p/<branch>"}, "", nil, testCtx()); err == nil {
		t.Fatal("expected unknown-token error")
	}
}

func TestLabelsAndSeqNamesUnionInOrder(t *testing.T) {
	tm := Templates{Branch: "<user:user>/<seq:b>", Path: "<user:issue>-<seq:a>-<user:user>"}
	if got := tm.Labels(); !reflect.DeepEqual(got, []string{"user", "issue"}) {
		t.Fatalf("Labels = %v, want [user issue]", got)
	}
	if got := tm.SeqNames(); !reflect.DeepEqual(got, []string{"b", "a"}) {
		t.Fatalf("SeqNames = %v, want [b a]", got)
	}
}

func TestRepoName(t *testing.T) {
	if got := RepoName("/work/acme-monorepo"); got != "acme-monorepo" {
		t.Fatalf("RepoName = %q, want acme-monorepo", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/worktree/ -v`
Expected: FAIL — package/symbols undefined.

- [ ] **Step 3: Implement** — `internal/worktree/worktree.go`

```go
// Package worktree holds the frontend-agnostic logic for resolving a new
// worktree's branch and path from configured templates, shared by the TUI popup
// and the CLI so neither duplicates it.
package worktree

import (
	"path/filepath"

	"github.com/gigagit/gg/internal/config"
	"github.com/gigagit/gg/internal/template"
)

// Templates is the branch + path template pair for worktree creation.
type Templates struct {
	Branch string
	Path   string
}

// Labels returns the distinct <user:LABEL> labels across both templates, in
// order of first appearance (branch first). A frontend collects a value for each.
func (t Templates) Labels() []string {
	var out []string
	for _, l := range template.UserLabels(t.Branch) {
		out = appendDistinct(out, l)
	}
	for _, l := range template.UserLabels(t.Path) {
		out = appendDistinct(out, l)
	}
	return out
}

// SeqNames returns the distinct <seq:NAME> names across both templates.
func (t Templates) SeqNames() []string {
	var out []string
	for _, n := range template.SeqNames(t.Branch) {
		out = appendDistinct(out, n)
	}
	for _, n := range template.SeqNames(t.Path) {
		out = appendDistinct(out, n)
	}
	return out
}

// Resolve resolves the branch then the path (two-phase: the path template is
// resolved with ctx.Branch set, so template's "<branch> is path-only" rule
// holds). When fixedBranch != "" it is used verbatim as the branch (edit mode).
func Resolve(t Templates, fixedBranch string, inputs map[string]string, ctx template.Ctx) (branch, path string, err error) {
	if fixedBranch != "" {
		branch = fixedBranch
	} else {
		branch, err = template.Resolve(t.Branch, inputs, ctx)
		if err != nil {
			return "", "", err
		}
	}
	ctx.Branch = branch
	path, err = template.Resolve(t.Path, inputs, ctx)
	if err != nil {
		return branch, "", err
	}
	return branch, path, nil
}

// RepoName returns the <repo> token value for a worktree root path.
func RepoName(worktreeRoot string) string {
	return filepath.Base(worktreeRoot)
}

// PeekSeqs reads the next value of each named counter (no mutation).
func PeekSeqs(gitCommonDir string, names []string) map[string]int {
	out := make(map[string]int, len(names))
	for _, n := range names {
		out[n] = config.PeekSeq(gitCommonDir, n)
	}
	return out
}

func appendDistinct(dst []string, s string) []string {
	for _, x := range dst {
		if x == s {
			return dst
		}
	}
	return append(dst, s)
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/worktree/ -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/worktree
git add internal/worktree/worktree.go internal/worktree/worktree_test.go
git commit -m "feat(worktree): shared package for resolving worktree templates

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: Refactor the TUI popup to use `internal/worktree`

Remove the now-duplicated local logic so the popup and CLI share one implementation.

**Files:** Modify `internal/tui/worktree_popup.go`, `internal/tui/worktree_popup_test.go`.

- [ ] **Step 1: Move the resolve tests out of the popup test file**

In `internal/tui/worktree_popup_test.go`, DELETE these four tests (they now live in `internal/worktree`): `TestResolveWorktreeNamesTwoPhase`, `TestResolveWorktreeNamesFixedBranch`, `TestResolveWorktreeNamesPropagatesError`, `TestResolveWorktreeNamesUserInput`. Also delete the `testCtx()` helper in that file IF it is only used by those four tests (search the file — if another test uses `testCtx`, keep it). If removing `testCtx` makes the `math/rand/v2` or `time` imports unused, remove those imports too.

- [ ] **Step 2: Refactor the popup** — in `internal/tui/worktree_popup.go`:

Add `"github.com/gigagit/gg/internal/worktree"` to the import block.

DELETE the local functions `resolveWorktreeNames`, `distinctAppend`, `peekSeqs`, and `repoNameFrom` (they moved to `internal/worktree`).

Change `recompute` to call the shared resolver:
```go
func (p *worktreePopup) recompute() {
	fixed := p.branchOverride
	if p.state == stEdit {
		fixed = p.editBuf
	}
	tm := worktree.Templates{Branch: p.branchTmpl, Path: p.pathTmpl}
	p.previewBranch, p.previewPath, p.previewErr = worktree.Resolve(tm, fixed, p.inputs, p.tctx())
}
```

Change the label/seq/repo/seq-peek wiring in `openWorktreePopup`. Replace the block that builds `labels`/`seqNames` (the four `for ... distinctAppend` loops) and the `repoName:`/`seqs:` struct fields with:
```go
	tm := worktree.Templates{Branch: bt, Path: pt}
	labels := tm.Labels()
	seqNames := tm.SeqNames()

	p := &worktreePopup{
		startPoint: m.branches[m.sel[panelBranches]].Name,
		branchTmpl: bt,
		pathTmpl:   pt,
		repoName:   worktree.RepoName(m.currentWorktree),
		labels:     labels,
		inputs:     map[string]string{},
		seqNames:   seqNames,
		seqs:       worktree.PeekSeqs(m.gitCommonDir, seqNames),
		seed:       rand.Uint64(),
		now:        time.Now(),
	}
```

Change `consumedSeqNames` to use the shared helper:
```go
func (p *worktreePopup) consumedSeqNames() []string {
	if p.branchOverride != "" {
		return worktree.Templates{Path: p.pathTmpl}.SeqNames()
	}
	return p.seqNames
}
```

Then FIX IMPORTS: `internal/tui/worktree_popup.go` no longer uses `config` or `path/filepath` directly (both moved into `internal/worktree`). Remove `"github.com/gigagit/gg/internal/config"` and `"path/filepath"` from its import block. Keep `math/rand/v2` (used by `tctx`/`rand.Uint64`), `time`, `strings`, `tea`, `engine`, `template`, and the new `worktree`.

- [ ] **Step 3: Run to verify the refactor is behavior-preserving**

Run: `go build ./internal/tui/` then `go test ./internal/tui/`
Expected: builds clean (no unused imports), and ALL existing popup tests still pass (open, input, edit, create, create-and-switch, render, overlay). If the build complains about an unused import, remove it; if a test fails, the refactor changed behavior — fix the call, not the test.

- [ ] **Step 4: Vet + format**

Run: `go vet ./internal/tui/` and `gofmt -l internal/tui`
Expected: clean / empty.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/tui
git add internal/tui/worktree_popup.go internal/tui/worktree_popup_test.go
git commit -m "refactor(tui): use internal/worktree for popup name resolution

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3: Thread `stdin` and `cwdFile` through `cli.Run`

`gg worktree add` prompts on stdin for `<user:>` values and writes the created path to `--cwd-file`. Give `Run` access to both.

**Files:** Modify `internal/cli/cli.go`, `internal/cli/cli_test.go`, `cmd/gg/main.go`.

- [ ] **Step 1: Update the test call site first (it documents the new signature)** — in `internal/cli/cli_test.go`, find the call `code := Run(workdir, args, &out, &errb)` and change it to:
```go
	code := Run(workdir, args, strings.NewReader(""), &out, &errb, "")
```
Add `"strings"` to that test file's imports if not present.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cli/ -run . -count=1 2>&1 | head`
Expected: FAIL/compile error — `Run` still has the old signature (too few arguments / type mismatch).

- [ ] **Step 3: Change `Run`'s signature** — in `internal/cli/cli.go`, change:
```go
func Run(workdir string, args []string, stdout, stderr io.Writer) int {
```
to:
```go
func Run(workdir string, args []string, stdin io.Reader, stdout, stderr io.Writer, cwdFile string) int {
```
The existing subcommand dispatch passes `stdout, stderr` to each `cmdX` unchanged. Add a `worktree` case that forwards the new params (the command itself is built in Task 4 — for THIS task, add the case calling a stub so the package compiles):
```go
	case "worktree":
		return cmdWorktree(repo, rest, stdin, stdout, stderr, cwdFile)
```
And register it in the `commands` map:
```go
var commands = map[string]bool{
	"status": true, "commit": true, "pull": true, "push": true,
	"switch": true, "stash": true, "undo": true, "worktree": true, "inspect": true,
}
```
Add a temporary stub at the bottom of `internal/cli/cli.go` (Task 4 replaces it):
```go
// cmdWorktree is implemented in internal/cli/worktree.go (Task 4).
func cmdWorktree(repo *repoT, args []string, stdin io.Reader, stdout, stderr io.Writer, cwdFile string) int {
	fmt.Fprintln(stderr, "worktree: not yet implemented")
	return 2
}
```

- [ ] **Step 4: Update `cmd/gg/main.go`** — change the CLI dispatch call. Find:
```go
	if len(args) > 0 && cli.IsCommand(args[0]) {
		os.Exit(cli.Run(".", args, os.Stdout, os.Stderr))
	}
```
and change it to:
```go
	if len(args) > 0 && cli.IsCommand(args[0]) {
		os.Exit(cli.Run(".", args, os.Stdin, os.Stdout, os.Stderr, cwdFile))
	}
```
(`cwdFile` is already extracted at the top of `main` via `extractCwdFile`, from A3b. `os.Stdin` is available.)

- [ ] **Step 5: Run to verify it passes + builds**

Run: `go build ./...` then `go test ./internal/cli/`
Expected: build OK; CLI tests pass (the stub isn't exercised by existing tests). Confirm `gofmt -l internal cmd` empty.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/cli cmd/gg
git add internal/cli/cli.go internal/cli/cli_test.go cmd/gg/main.go
git commit -m "feat(cli): thread stdin and --cwd-file into Run; register worktree

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4: `gg worktree list`

**Files:** Create `internal/cli/worktree.go` (with the real `cmdWorktree` dispatching `list`); remove the stub from `internal/cli/cli.go`; create `internal/cli/worktree_test.go`.

- [ ] **Step 1: Write the failing test** — `internal/cli/worktree_test.go`

```go
package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newCLIRepo makes a temp git repo with one commit on main and returns its dir.
func newCLIRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "initial")
	return dir
}

func TestWorktreeList(t *testing.T) {
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "list"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	// The main worktree (on branch main) is listed.
	if !strings.Contains(out.String(), "main") {
		t.Fatalf("worktree list output missing main:\n%s", out.String())
	}
}

func TestWorktreeUnknownSub(t *testing.T) {
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"worktree", "bogus"}, strings.NewReader(""), &out, &errb, ""); code == 0 {
		t.Fatal("unknown worktree subcommand should be a non-zero exit")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cli/ -run 'TestWorktreeList|TestWorktreeUnknownSub' -v`
Expected: FAIL — the stub prints "not yet implemented" and returns 2 for `list`.

- [ ] **Step 3: Implement** — first DELETE the stub `cmdWorktree` from `internal/cli/cli.go` (added in Task 3). Then create `internal/cli/worktree.go`:

```go
package cli

import (
	"context"
	"fmt"
	"io"
)

// cmdWorktree dispatches `gg worktree <sub>`.
func cmdWorktree(repo *repoT, args []string, stdin io.Reader, stdout, stderr io.Writer, cwdFile string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg worktree <list|add> [args]")
		return 2
	}
	switch args[0] {
	case "list":
		return cmdWorktreeList(repo, stdout, stderr)
	case "add":
		return cmdWorktreeAdd(repo, args[1:], stdin, stdout, stderr, cwdFile)
	default:
		fmt.Fprintf(stderr, "worktree: unknown subcommand %q (use list or add)\n", args[0])
		return 2
	}
}

func cmdWorktreeList(repo *repoT, stdout, stderr io.Writer) int {
	wts, err := repo.Worktrees(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, w := range wts {
		branch := w.Branch
		if branch == "" {
			branch = "(detached)"
		}
		fmt.Fprintf(stdout, "%s\t%s\n", branch, w.Path)
	}
	return 0
}

// cmdWorktreeAdd is implemented in Task 5.
func cmdWorktreeAdd(repo *repoT, args []string, stdin io.Reader, stdout, stderr io.Writer, cwdFile string) int {
	fmt.Fprintln(stderr, "worktree add: not yet implemented")
	return 2
}
```

(`repoT` is a same-package alias for `git.Repo`, so this file needs no `git` import. The stub's unused params are legal Go; Task 5 fills the body.)

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/cli/ -run 'TestWorktreeList|TestWorktreeUnknownSub' -v`
Expected: PASS. Then `go test ./internal/cli/` — all pass.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/cli
git add internal/cli/cli.go internal/cli/worktree.go internal/cli/worktree_test.go
git commit -m "feat(cli): add gg worktree list

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 5: `gg worktree add [start-point]`

Resolve the configured default templates (prompting stdin for `<user:>`), create the worktree, bump `<seq>` on success, print, and write `--cwd-file`.

**Files:** Modify `internal/cli/worktree.go`; extend `internal/cli/worktree_test.go`.

- [ ] **Step 1: Write the failing test** — append to `internal/cli/worktree_test.go`

```go
func TestWorktreeAddCreatesAndPrints(t *testing.T) {
	dir := newCLIRepo(t)
	// A repo config with a deterministic-ish template that needs one user input.
	os.WriteFile(filepath.Join(dir, ".gg.toml"),
		[]byte("[worktree]\nbranch_templates = []\ndefault_branch_template = \"issue/<user:id>\"\npath_template = \"../<repo>.worktrees/<branch>\"\n"),
		0o644)

	cwdFile := filepath.Join(t.TempDir(), "cwd")
	var out, errb bytes.Buffer
	// stdin supplies the <user:id> value.
	code := Run(dir, []string{"worktree", "add", "main"}, strings.NewReader("77\n"), &out, &errb, cwdFile)
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	// Output names the created branch.
	if !strings.Contains(out.String(), "issue/77") {
		t.Fatalf("output missing branch issue/77:\n%s", out.String())
	}
	// The worktree exists on disk.
	wt := filepath.Clean(filepath.Join(dir, "..", filepath.Base(dir)+".worktrees", "issue-77"))
	if _, err := os.Stat(filepath.Join(wt, "README.md")); err != nil {
		t.Fatalf("worktree not created at %s: %v", wt, err)
	}
	// --cwd-file received the created path.
	got, _ := os.ReadFile(cwdFile)
	if strings.TrimSpace(string(got)) != wt {
		t.Fatalf("cwd-file = %q, want %q", strings.TrimSpace(string(got)), wt)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cli/ -run TestWorktreeAddCreatesAndPrints -v`
Expected: FAIL — the stub prints "not yet implemented".

- [ ] **Step 3: Implement** — in `internal/cli/worktree.go`, replace the `cmdWorktreeAdd` stub with the real implementation. Update the import block to:
```go
import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gigagit/gg/internal/config"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/template"
	"github.com/gigagit/gg/internal/worktree"
)
```
(`context`/`fmt`/`io` are still used by `cmdWorktree`/`cmdWorktreeList`; the rest are new for `cmdWorktreeAdd`.)

```go
func cmdWorktreeAdd(repo *repoT, args []string, stdin io.Reader, stdout, stderr io.Writer, cwdFile string) int {
	ctxBg := context.Background()

	// Start point: explicit arg, else the current branch.
	startPoint := ""
	if len(args) > 0 {
		startPoint = args[0]
	}
	if startPoint == "" {
		cur, err := repo.CurrentBranch(ctxBg)
		if err != nil || cur == "" {
			fmt.Fprintln(stderr, "worktree add: cannot determine current branch; pass a start-point")
			return 2
		}
		startPoint = cur
	}

	top, err := repo.TopLevel(ctxBg)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	gitCommonDir, err := repo.GitCommonDir(ctxBg)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	cfg, err := config.Load(config.DefaultGlobalPath(), filepath.Join(top, ".gg.toml"))
	if err != nil {
		fmt.Fprintln(stderr, "error: loading config:", err)
		return 1
	}

	tm := worktree.Templates{
		Branch: cfg.Worktree.DefaultBranchTemplate,
		Path:   cfg.Worktree.PathTemplate,
	}

	// Prompt stdin for each <user:LABEL>.
	inputs := map[string]string{}
	reader := bufio.NewReader(stdin)
	for _, label := range tm.Labels() {
		fmt.Fprintf(stdout, "%s: ", label)
		line, _ := reader.ReadString('\n')
		inputs[label] = strings.TrimRight(line, "\r\n")
	}

	seqNames := tm.SeqNames()
	ctx := template.Ctx{
		ParentBranch: startPoint,
		Repo:         worktree.RepoName(top),
		Seqs:         worktree.PeekSeqs(gitCommonDir, seqNames),
		Now:          time.Now,
		Rand:         rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
	}
	branch, path, err := worktree.Resolve(tm, "", inputs, ctx)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	res, err := runOperation(ctxBg, repo,
		engine.CreateWorktree{StartPoint: startPoint, Branch: branch, Path: path},
		cliDecider{}, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	// Consume the counters the templates used, now that creation succeeded.
	for _, name := range seqNames {
		_, _ = config.BumpSeq(gitCommonDir, name)
	}

	fmt.Fprintf(stdout, "✓ created worktree %s at %s\n", branch, res.Path)
	if cwdFile != "" && res.Path != "" {
		_ = os.WriteFile(cwdFile, []byte(res.Path), 0o644)
	}
	return 0
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/cli/ -run TestWorktreeAddCreatesAndPrints -v`
Expected: PASS. Then `go test ./internal/cli/` — all pass.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/cli
git add internal/cli/worktree.go internal/cli/worktree_test.go
git commit -m "feat(cli): add gg worktree add [start-point] (prompt, create, cwd-file)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 6: Full-package verification

**Files:** none (verification only)

- [ ] **Step 1: Full suite** — `go test ./...` — Expected: all PASS.
- [ ] **Step 2: Race** — `go test -race ./internal/tui/ ./internal/cli/ ./internal/engine/` — Expected: PASS, no races.
- [ ] **Step 3: Vet** — `go vet ./...` — Expected: no output.
- [ ] **Step 4: Format** — `gofmt -l internal cmd` — Expected: empty (else `gofmt -w` and amend).
- [ ] **Step 5: Confirm no duplication** — `go list -deps ./internal/cli ./internal/tui | grep gigagit/gg/internal/worktree` should show BOTH the cli and tui packages depend on `internal/worktree` (they share the resolver). And grep that the popup no longer defines its own `resolveWorktreeNames`: `! grep -rn "func resolveWorktreeNames" internal/tui/`.
- [ ] **Step 6: Manual smoke (document result):** `go build -o /tmp/gg ./cmd/gg`; in a temp repo `/tmp/gg worktree list` prints the main worktree; `/tmp/gg worktree add main` (with a no-`<user:>` config) creates and prints a worktree.

No commit needed if everything is already committed.

---

## Self-Review Notes (plan author)

- **Spec coverage:** §11 `gg worktree list` → Task 4; `gg worktree add [start-point]` prompting stdin for `<user:>`, creating, printing → Task 5; `--cwd-file` written for the wrapped shell → Tasks 3,5; start-point defaults to current branch → Task 5.
- **No duplication (the whole point of A3c):** the resolve logic lives once in `internal/worktree`; the TUI popup (Task 2) and CLI (Tasks 4–5) both call it. Task 6 Step 5 asserts both depend on it and the popup's local copy is gone.
- **Behavior preservation:** Task 2 is a pure refactor guarded by the full existing popup test suite; the moved pure tests now live in `internal/worktree` (Task 1).
- **Seq semantics match the TUI:** `PeekSeqs` for resolution, `BumpSeq` once per template `<seq>` only after a successful create (Task 5), consistent with the popup.
- **Type consistency:** `worktree.Templates{Branch,Path}`, `.Labels()`, `.SeqNames()`, `worktree.Resolve(tm, fixed, inputs, ctx)`, `worktree.RepoName`, `worktree.PeekSeqs`, `cli.Run(workdir, args, stdin, stdout, stderr, cwdFile)`, `cmdWorktree(repo, args, stdin, stdout, stderr, cwdFile)` are used consistently across tasks and the `cmd/gg` call site.
