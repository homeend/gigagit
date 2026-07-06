# External Tools Stage 3 (`review`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run an external AI agent over a branch, a commit/range, or the uncommitted changes; capture its review report; persist it durably; show it in a read-only in-TUI viewer (with `e` → `$EDITOR`) and via a scriptable `gg review` CLI verb.

**Architecture:** A new `engine.ReviewChanges` capture op (sibling of `GenerateMessage`) reuses the Stage-2 `CaptureRunner` seam + the `$GG_MESSAGE_FILE` output-channel contract + a `$GG_REVIEW_DIFF` file. `domain.ReviewReport` resolves a `ReviewTarget` (branch/range/working) to a `DiffSpec`, runs the op via `Execute`, and persists the report under the gg state dir. The TUI adds a full-screen `reviewView` layer + `.`-menu entries; the CLI adds a `gg review` verb.

**Tech Stack:** Go 1.26, Bubble Tea TUI, shells out to `git` and to configured agent binaries.

## Global Constraints

- **Reuse Stage 2 verbatim:** the `CaptureRunner` seam (`OpDeps.CaptureRunner` / `ShellCaptureRunner`, `ErrWaitDelay`-on-exit0 = success) and the **`$GG_MESSAGE_FILE` output-channel contract — non-empty file content wins over stdout**. Do not reimplement capture.
- **Both catalog tools ship `Mode: ModeCapture`** (verified 2026-07-07). Claude: `claude -p "/code-review <range>" --output-format json --permission-mode acceptEdits --allowedTools "Read" "Bash(git diff *)" "Bash(git log *)" "Bash(git show *)" "Bash(git status *)"` (stdout `.result` is the report). Junie: `junie --task "<prompt referencing ${GG_REVIEW_DIFF}, ${GG_MESSAGE_FILE}, <range>>" --output-format json --skip-update-check` (writes the report to `$GG_MESSAGE_FILE`; its `--review` flag can't take a range, only working changes).
- **`ReviewChanges.LockMode()` = `repogate.Read`** (git reads only; approval is the frontend's job).
- **`<range>` is a PROSE token** (git rev-ranges have no spaces) — substituted literally, never shell-quoted.
- **Reports are durable and accumulate** at `<state>/gg/reviews/<repoKey(commonDir)>/<YYYYMMDD-HHMM>-<sanitized-range>.md`. `<state>/gg` resolves via the LocalAppData / `XDG_STATE_HOME` / `~/.local/state` ladder (copy `shelfBaseDir`). `repoKey` = `sha256(filepath.Clean(commonDir))[:8]` (`domain.repoKey`, `shelfstore.go:76`). Sanitize the range for the filename by replacing every byte that is `/`, whitespace, a control byte, or `:` with `-`; **keep `..`** (safe inside one filename segment).
- **Layering:** `internal/tui` and `internal/cli` never import `internal/git` (archtest-guarded) — reach git through `domain`. Frontends run ops via `domain.Execute`.
- **Tests:** real `git` in `t.TempDir()` (`newRepo`/`newTestRepo`). Capture-path tests use the **real `ShellCaptureRunner` + scripted `sh -c`**, NEVER a real agent; gate them with `if testing.Short(){t.Skip()}` and `if runtime.GOOS=="windows"{t.Skip("uses sh")}` (the Stage-2 idiom).
- **On CLI surface change:** bump `agentskill.Version`, document `gg review` in `internal/agentskill/using-gg.md`, note `gg init --update`.

---

### Task 1: `<range>` command token

**Files:**
- Modify: `internal/template/command.go` (`CmdCtx` struct ~`:19-24`; `resolveCommandToken` switch ~`:53-60`; `commandTokens` map ~`:96-100`)
- Test: `internal/template/command_test.go`

**Interfaces:**
- Produces: `template.CmdCtx.Range string` (a new prose field); `<range>` resolves to `ctx.Range` literally; `template.ValidateCommandTokens` accepts `<range>`.

- [ ] **Step 1: Write the failing test.** Add to `command_test.go`:

```go
func TestResolveRangeToken(t *testing.T) {
	got, err := ResolveCommand(`review <range>`, nil, CmdCtx{Range: "main..HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "review main..HEAD" {
		t.Fatalf("got %q, want %q", got, "review main..HEAD")
	}
	// prose token: NOT shell-quoted even though a range can contain '/'
	got, _ = ResolveCommand(`review <range>`, nil, CmdCtx{Range: "feature/x..main"})
	if got != "review feature/x..main" {
		t.Fatalf("range must substitute literally, got %q", got)
	}
	if err := ValidateCommandTokens("x <range>", false); err != nil {
		t.Fatalf("<range> should be a valid token: %v", err)
	}
}
```

- [ ] **Step 2: Run it, verify it fails.** Run: `go test ./internal/template/ -run TestResolveRangeToken`. Expected: FAIL (`unknown command token <range>`).

- [ ] **Step 3: Implement.** In `command.go`, add `Range` to the prose group of `CmdCtx`:

```go
type CmdCtx struct {
	Op, Source, Target, Range, Repo   string
	ConflictedFiles                   []string
	File, Local, Base, Remote, Merged string
	ContextFile                       string
}
```

Add a case in `resolveCommandToken` beside the other prose cases (before the `repo` case):

```go
	case "range":
		return ctx.Range, nil
```

Add to the non-per-file group of `commandTokens`:

```go
	"op": false, "source": false, "target": false, "range": false, "conflicted-files": false,
```

(`ValidateCommandTokens` reads the map generically — no other change.)

- [ ] **Step 4: Run tests, verify pass.** Run: `go test ./internal/template/`. Expected: PASS.

- [ ] **Step 5: Commit.** `git add internal/template/command.go internal/template/command_test.go && git commit -m "feat(template): add <range> prose command token for review"`

---

### Task 2: `engine.ReviewChanges` capture op

**Files:**
- Create: `internal/engine/review_changes.go`
- Test: `internal/engine/review_changes_test.go`
- Reuses (same package, unexported): `buildSummary`, `writeTempFile`, `MaxDiffBytes` from `generate_message.go`; `CaptureSpec`, `deps.captureRunner()`.

**Interfaces:**
- Produces: `engine.ReviewChanges{Command string; Dir string; Env []string; Diff model.DiffSpec; RangeLabel string}` with `LockMode() repogate.Mode` = `Read` and `Run(ctx, OpDeps) (Result, error)` returning the captured report in `Result.Captured`. Sets env `GG_CONTEXT_FILE`, `GG_REVIEW_DIFF`, `GG_MESSAGE_FILE`, `GG_REPO`.
- Consumes: `model.DiffSpec{Cached bool; Rev string; Paths []string}` (a range goes in `Rev`, e.g. `"main..HEAD"`; working+staged is the zero value with `Rev:""`, `Cached:false`).

- [ ] **Step 1: Write the failing test.** Create `internal/engine/review_changes_test.go`:

```go
package engine

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repogate"
)

func TestReviewChangesLockModeRead(t *testing.T) {
	if (ReviewChanges{}).LockMode() != repogate.Read {
		t.Fatal("want Read")
	}
}

// A task-agent writes the report to $GG_MESSAGE_FILE; that content wins over stdout.
func TestReviewChangesPrefersMessageFile(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	dir, repo := newRepo(t)
	// two commits so HEAD~1..HEAD is a real range
	stageAndCommit(t, dir, repo, "a.txt", "one\n", "c1")
	stageAndCommit(t, dir, repo, "a.txt", "one\ntwo\n", "c2")

	cmd := `printf 'stdout is a report, ignore me\n'; ` +
		`printf 'REVIEW: looks fine\n' > "$GG_MESSAGE_FILE"`
	res, err := ReviewChanges{Command: cmd, Dir: dir, Diff: model.DiffSpec{Rev: "HEAD~1..HEAD"}, RangeLabel: "HEAD~1..HEAD"}.
		Run(context.Background(), OpDeps{Repo: repo, CaptureRunner: ShellCaptureRunner{}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Captured != "REVIEW: looks fine\n" {
		t.Fatalf("captured=%q, want the message-file content (file wins over stdout)", res.Captured)
	}
}

// A stdout tool (Claude) leaves the file empty; stdout is used.
func TestReviewChangesFallsBackToStdout(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	dir, repo := newRepo(t)
	stageAndCommit(t, dir, repo, "a.txt", "one\n", "c1")

	res, err := ReviewChanges{Command: `printf 'the whole review on stdout\n'`, Dir: dir, Diff: model.DiffSpec{Rev: "HEAD"}, RangeLabel: "HEAD"}.
		Run(context.Background(), OpDeps{Repo: repo, CaptureRunner: ShellCaptureRunner{}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Captured != "the whole review on stdout\n" {
		t.Fatalf("captured=%q, want stdout", res.Captured)
	}
}

// $GG_REVIEW_DIFF holds the RANGE diff (not --cached); the context file names the range.
func TestReviewChangesWritesRangeDiffAndContext(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/cp")
	}
	dir, repo := newRepo(t)
	stageAndCommit(t, dir, repo, "a.txt", "one\n", "c1")
	stageAndCommit(t, dir, repo, "a.txt", "one\nTWO\n", "c2")

	// Copy the two provisioned files out so we can assert on them post-run.
	cmd := `cp "$GG_REVIEW_DIFF" "` + dir + `/seen.diff"; cp "$GG_CONTEXT_FILE" "` + dir + `/seen.ctx"; printf x > "$GG_MESSAGE_FILE"`
	_, err := ReviewChanges{Command: cmd, Dir: dir, Diff: model.DiffSpec{Rev: "HEAD~1..HEAD"}, RangeLabel: "HEAD~1..HEAD"}.
		Run(context.Background(), OpDeps{Repo: repo, CaptureRunner: ShellCaptureRunner{}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	diffSeen := readFile(t, dir+"/seen.diff")
	if !strings.Contains(diffSeen, "TWO") {
		t.Fatalf("review diff must be the range diff (contain TWO):\n%s", diffSeen)
	}
	ctxSeen := readFile(t, dir+"/seen.ctx")
	if !strings.Contains(ctxSeen, "HEAD~1..HEAD") {
		t.Fatalf("context file must name the range:\n%s", ctxSeen)
	}
}
```

Add these helpers to the test file only if `newRepo`/`stageAndCommit`/`readFile` are not already present in the `engine` test package — check first with `grep -rn "func newRepo\|func stageAndCommit\|func readFile" internal/engine/*_test.go`. `newRepo` already exists (used by `generate_message_test.go`). If `stageAndCommit`/`readFile` are missing, add:

```go
func stageAndCommit(t *testing.T, dir string, repo *git.Repo, name, content, msg string) {
	t.Helper()
	stageFile(t, dir, repo, name, content) // stageFile already exists in generate_message_test.go
	if err := repo.Commit(context.Background(), msg, false, false); err != nil {
		t.Fatal(err)
	}
}
func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
```

(Verify the exact `repo.Commit` signature with `grep -n "func (r \*Repo) Commit" internal/git/*.go` and match it; if committing is awkward, use the existing `newTestRepo` helper which may already create commits — `grep -rn "func newTestRepo\|func newRepo" internal/engine internal/git`.)

- [ ] **Step 2: Run it, verify it fails.** Run: `go test ./internal/engine/ -run TestReviewChanges`. Expected: FAIL (`undefined: ReviewChanges`).

- [ ] **Step 3: Implement.** Create `internal/engine/review_changes.go`:

```go
package engine

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repogate"
)

// ReviewChanges runs a review agent headless over op.Diff and returns its
// captured report (Result.Captured). It writes three temp files — a labeled
// summary ($GG_CONTEXT_FILE, naming the range), the full unified diff of
// op.Diff ($GG_REVIEW_DIFF, capped at MaxDiffBytes), and an empty output file
// ($GG_MESSAGE_FILE) — runs the (resolved, approved) command via the
// CaptureRunner, then removes them.
//
// It shares the Stage-2 output-channel contract: a task-agent MAY write its
// report to $GG_MESSAGE_FILE and non-empty file content WINS over stdout; a
// stdout tool (Claude's --output-format json .result) leaves it empty and
// stdout is used. LockMode Read: git reads only; approval is the frontend's job.
type ReviewChanges struct {
	Command    string         // resolved, approved shell command line
	Dir        string         // repo/worktree root
	Env        []string       // caller env additions (e.g. GG_TASK=review)
	Diff       model.DiffSpec // the range/working diff to review
	RangeLabel string         // human range label for the summary (e.g. "main..HEAD")
}

var _ Operation = ReviewChanges{}

func (op ReviewChanges) LockMode() repogate.Mode { return repogate.Read }

func (op ReviewChanges) Run(ctx context.Context, deps OpDeps) (Result, error) {
	diff, err := deps.Repo.DiffPatch(ctx, op.Diff)
	if err != nil {
		return Result{}, err
	}
	stat, _ := deps.Repo.DiffNumstat(ctx, op.Diff)

	truncated := len(diff) > MaxDiffBytes
	diffBody := diff
	if truncated {
		diffBody = fmt.Sprintf("(diff truncated: %d bytes exceeds the %d KiB cap — inspect specific files with git)\n",
			len(diff), MaxDiffBytes>>10)
	}
	diffPath, err := writeTempFile("gg-review-*.diff", diffBody)
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(diffPath)
	ctxPath, err := writeTempFile("gg-review-ctx-*.txt", op.reviewSummary(diffPath, stat, truncated))
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(ctxPath)
	msgPath, err := writeTempFile("gg-review-msg-*.md", "")
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(msgPath)

	env := append(append([]string{}, os.Environ()...), op.Env...)
	env = append(env,
		"GG_CONTEXT_FILE="+ctxPath,
		"GG_REVIEW_DIFF="+diffPath,
		"GG_MESSAGE_FILE="+msgPath,
		"GG_REPO="+op.Dir,
	)
	stdout, runErr := deps.captureRunner().Capture(ctx,
		CaptureSpec{Dir: op.Dir, Env: env, Command: op.Command},
		func(line string) { deps.emit(ctx, GitLine{Raw: line}) })
	captured := string(stdout)
	if fileMsg, rerr := os.ReadFile(msgPath); rerr == nil && strings.TrimSpace(string(fileMsg)) != "" {
		captured = string(fileMsg)
	}
	if runErr != nil {
		return Result{Captured: captured}, runErr
	}
	return Result{Captured: captured, Summary: "reviewed " + op.RangeLabel}, nil
}

func (op ReviewChanges) reviewSummary(diffPath, stat string, truncated bool) string {
	var b strings.Builder
	b.WriteString("# gg — review the changes below.\n")
	rangeLabel := op.RangeLabel
	if rangeLabel == "" {
		rangeLabel = "(working changes)"
	}
	b.WriteString("# Range: " + rangeLabel + "\n")
	b.WriteString("# Full unified diff: " + diffPath)
	if truncated {
		b.WriteString("  (truncated — inspect files with git)")
	}
	b.WriteString("\n\n## Files changed (git diff --numstat)\n")
	stat = strings.ReplaceAll(stat, "\x00", "\n") // -z is NUL-delimited
	if strings.TrimSpace(stat) == "" {
		b.WriteString("(no changes)\n")
	} else {
		b.WriteString(strings.TrimRight(stat, "\n") + "\n")
	}
	return b.String()
}
```

- [ ] **Step 4: Run tests, verify pass.** Run: `go test ./internal/engine/ -run TestReviewChanges`. Expected: PASS (all four).

- [ ] **Step 5: Commit.** `git add internal/engine/review_changes.go internal/engine/review_changes_test.go && git commit -m "feat(engine): ReviewChanges capture op (range/working diff -> report; reuses \$GG_MESSAGE_FILE channel)"`

---

### Task 3: `review` catalog templates

**Files:**
- Modify: `internal/exttool/exttool.go` (add two command consts near `junieCommitCommand`; add two entries to the Claude and Junie `Commands` slices in `Builtins()`)
- Test: `internal/exttool/exttool_test.go`

**Interfaces:**
- Produces: catalog `review` templates — Claude (`ModeCapture`, `<range>` + `--output-format json`), Junie (`ModeCapture`, `--task` writing to `${GG_MESSAGE_FILE}`, reading `${GG_REVIEW_DIFF}`).

- [ ] **Step 1: Write the failing test.** Add to `exttool_test.go`:

```go
func TestBuiltinsReviewTemplates(t *testing.T) {
	var claude, junie *CommandTemplate
	for _, tl := range Builtins() {
		for i := range tl.Commands {
			c := &tl.Commands[i]
			if c.Category != CatReview {
				continue
			}
			if c.Mode != ModeCapture {
				t.Fatalf("%s: review must be capture (verified 2026-07-07)", c.Name)
			}
			switch tl.ID {
			case "claude":
				claude = c
			case "junie":
				junie = c
			}
		}
	}
	if claude == nil || junie == nil {
		t.Fatal("want claude + junie review templates")
	}
	gc := GenerateCommandFor(*claude, "claude", "linux")
	if !strings.Contains(gc, "/code-review <range>") {
		t.Fatalf("claude review must run /code-review over <range>: %q", gc)
	}
	gj := GenerateCommandFor(*junie, "junie", "linux")
	if !strings.Contains(gj, "${GG_MESSAGE_FILE}") || !strings.Contains(gj, "${GG_REVIEW_DIFF}") {
		t.Fatalf("junie review must write GG_MESSAGE_FILE and read GG_REVIEW_DIFF: %q", gj)
	}
	if !strings.HasPrefix(gj, `junie --task "`) {
		t.Fatalf("junie prompt must be first after --task: %q", gj)
	}
}
```

- [ ] **Step 2: Run it, verify it fails.** Run: `go test ./internal/exttool/ -run TestBuiltinsReviewTemplates`. Expected: FAIL (no review templates).

- [ ] **Step 3: Implement.** Add consts after `junieCommitCommand` in `exttool.go` (the `<range>` token is a runtime token, left intact by `GenerateCommandFor`, resolved later by `template.ResolveCommand`):

```go
// claudeReviewCommand — verified 2026-07-07: `claude -p "/code-review <range>"`
// runs headless and .result is a clean severity-structured markdown report.
// <range> is a runtime token (resolved by template.ResolveCommand). For the
// uncommitted target <range> resolves empty and /code-review reviews the tree.
const claudeReviewCommand = `<bin> -p "/code-review <range>" \
  --output-format json \
  --permission-mode acceptEdits \
  --allowedTools "Read" "Bash(git diff *)" "Bash(git log *)" "Bash(git show *)" "Bash(git status *)"`

// junieReviewPrompt — Junie is a task-agent: its stdout is a report, so the
// review comes back through $GG_MESSAGE_FILE (the Stage-2 channel). Junie's own
// --review flag reviews only uncommitted working changes and can't take a
// range, so we feed it the diff at $GG_REVIEW_DIFF instead (verified 2026-07-07).
const junieReviewPrompt = `"You are reviewing a code change. The full diff to review is in the file at <env:GG_REVIEW_DIFF> (range <range>). Write a concise code review — findings with severity and a short summary — into the file at <env:GG_MESSAGE_FILE> (an absolute path outside the repository). Do NOT modify any repository files and do NOT run git commit."`

const junieReviewCommand = `<bin> --task ` + junieReviewPrompt + ` --output-format json --skip-update-check`
```

Add to the Claude `Commands` slice (after the commit_message entry):

```go
					{Category: CatReview, Name: "Claude", Mode: ModeCapture, Command: claudeReviewCommand},
```

Add to the Junie `Commands` slice:

```go
					{Category: CatReview, Name: "Junie", Mode: ModeCapture, Command: junieReviewCommand},
```

- [ ] **Step 4: Run tests, verify pass.** Run: `go test ./internal/exttool/`. Expected: PASS.

- [ ] **Step 5: Commit.** `git add internal/exttool/exttool.go internal/exttool/exttool_test.go && git commit -m "feat(exttool): review catalog templates for Claude (/code-review) and Junie (file channel)"`

---

### Task 4: `domain.ReviewReport` + `ReviewTarget` + report persistence

**Files:**
- Create: `internal/domain/review.go`
- Test: `internal/domain/review_test.go`
- Reference: `internal/domain/shelfstore.go` (`shelfBaseDir`, `repoKey`), `internal/domain/service.go` (`Execute`, `workdir`, `GitCommonDir`).

**Interfaces:**
- Produces:
  - `domain.ReviewTarget{Kind ReviewKind; Range string; Diff model.DiffSpec}` and `type ReviewKind int` with `ReviewBranch/ReviewRange/ReviewWorking`.
  - `func (s *Service) ReviewReport(ctx context.Context, target ReviewTarget, resolvedCommand string, env []string, now time.Time) (ReviewResult, error)` returning `ReviewResult{Path, Content, Range string}`. `now` is passed in (testable; the caller stamps `time.Now()`).
  - `func (s *Service) BranchReviewTarget(ctx context.Context, tip string) (ReviewTarget, error)` — resolves `<base>..<tip>` (base = `git merge-base main <tip>`, else `@{upstream}`, else the tip alone).
- Consumes: `engine.ReviewChanges` (Task 2).

- [ ] **Step 1: Write the failing test.** Create `internal/domain/review_test.go`:

```go
package domain

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

func TestReviewReportPersistsAndReturns(t *testing.T) {
	dir, repo := newRepo(t) // domain test helper; grep to confirm name
	svc := New(repo)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	commitFile(t, dir, repo, "a.txt", "one\n", "c1")
	commitFile(t, dir, repo, "a.txt", "one\ntwo\n", "c2")

	target := ReviewTarget{Kind: ReviewRange, Range: "HEAD~1..HEAD", Diff: model.DiffSpec{Rev: "HEAD~1..HEAD"}}
	// A resolved command that just echoes a fixed report to stdout.
	cmd := `printf 'REPORT: one finding\n'`
	when := time.Date(2026, 7, 7, 1, 30, 0, 0, time.UTC)
	res, err := svc.ReviewReport(context.Background(), target, cmd, []string{"GG_TASK=review"}, when)
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if !strings.Contains(res.Content, "REPORT: one finding") {
		t.Fatalf("content=%q", res.Content)
	}
	if !strings.Contains(res.Path, "reviews") || !strings.HasSuffix(res.Path, ".md") {
		t.Fatalf("path=%q, want a reviews/*.md file", res.Path)
	}
	if got := readFile(t, res.Path); !strings.Contains(got, "REPORT: one finding") {
		t.Fatalf("persisted file content=%q", got)
	}
	// filename carries the sanitized range + the stamped time
	if !strings.Contains(res.Path, "20260707-0130") || !strings.Contains(res.Path, "HEAD~1..HEAD") {
		t.Fatalf("filename should carry timestamp+range: %q", res.Path)
	}
}

func TestSanitizeRangeForFilename(t *testing.T) {
	cases := map[string]string{
		"main..HEAD":      "main..HEAD",
		"feature/x..main": "feature-x..main",
		"":                "working-changes",
		"a b:c":           "a-b-c",
	}
	for in, want := range cases {
		if got := sanitizeRangeForFilename(in); got != want {
			t.Fatalf("sanitize(%q)=%q, want %q", in, got, want)
		}
	}
}
```

(Confirm the domain test helpers with `grep -rn "func newRepo\|func commitFile\|func readFile" internal/domain/*_test.go`; reuse existing ones. If `commitFile`/`readFile` are absent, add small helpers mirroring the engine test file.)

- [ ] **Step 2: Run it, verify it fails.** Run: `go test ./internal/domain/ -run 'TestReviewReport|TestSanitizeRange'`. Expected: FAIL (undefined).

- [ ] **Step 3: Implement.** Create `internal/domain/review.go`:

```go
package domain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// ReviewKind names the three review targets.
type ReviewKind int

const (
	ReviewBranch ReviewKind = iota
	ReviewRange
	ReviewWorking
)

// ReviewTarget is a resolved review scope: a human Range label plus the DiffSpec
// to feed the agent. Working changes use the zero DiffSpec (Rev "", Cached false).
type ReviewTarget struct {
	Kind  ReviewKind
	Range string // "" for the working-changes target
	Diff  model.DiffSpec
}

// ReviewResult is a produced review: the durable report path and its content.
type ReviewResult struct {
	Path    string
	Content string
	Range   string
}

// ReviewReport runs resolvedCommand over target via engine.ReviewChanges, then
// persists the captured report under <state>/gg/reviews/<repoKey>/. now is
// injected so the filename timestamp is testable.
func (s *Service) ReviewReport(ctx context.Context, target ReviewTarget, resolvedCommand string, env []string, now time.Time) (ReviewResult, error) {
	op := engine.ReviewChanges{
		Command:    resolvedCommand,
		Dir:        s.workdir,
		Env:        env,
		Diff:       target.Diff,
		RangeLabel: target.Range,
	}
	res, err := s.Execute(ctx, op, nil, nil)
	if err != nil {
		return ReviewResult{}, err
	}
	if strings.TrimSpace(res.Captured) == "" {
		return ReviewResult{}, fmt.Errorf("review produced an empty report")
	}
	path, werr := s.writeReviewReport(ctx, target.Range, res.Captured, now)
	if werr != nil {
		return ReviewResult{}, werr
	}
	return ReviewResult{Path: path, Content: res.Captured, Range: target.Range}, nil
}

func (s *Service) writeReviewReport(ctx context.Context, rng, content string, now time.Time) (string, error) {
	base := reviewsBaseDir()
	if base == "" {
		return "", fmt.Errorf("review: no state dir available")
	}
	common, err := s.repo.GitCommonDir(ctx)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, repoKey(strings.TrimSpace(common)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := now.Format("20060102-1504") + "-" + sanitizeRangeForFilename(rng) + ".md"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// reviewsBaseDir mirrors shelfBaseDir (shelfstore.go) with a "reviews" leaf.
func reviewsBaseDir() string {
	if runtime.GOOS == "windows" {
		if lad := os.Getenv("LocalAppData"); lad != "" {
			return filepath.Join(lad, "gg", "reviews")
		}
	}
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "gg", "reviews")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "gg", "reviews")
}

// sanitizeRangeForFilename replaces bytes unsafe inside one filename segment
// (/, whitespace, control bytes, ':') with '-'; '..' is kept. "" -> a stable label.
func sanitizeRangeForFilename(rng string) string {
	rng = strings.TrimSpace(rng)
	if rng == "" {
		return "working-changes"
	}
	var b strings.Builder
	for _, r := range rng {
		switch {
		case r == '/' || r == ':' || r <= ' ':
			b.WriteByte('-')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// BranchReviewTarget resolves <base>..<tip>: base = merge-base with main, then
// @{upstream}, else the tip alone (a branch with no base -> review just its tip).
func (s *Service) BranchReviewTarget(ctx context.Context, tip string) (ReviewTarget, error) {
	base, err := s.repo.MergeBase(ctx, "main", tip)
	if err != nil || strings.TrimSpace(base) == "" {
		if up, uerr := s.repo.UpstreamRef(ctx, tip); uerr == nil && strings.TrimSpace(up) != "" {
			base = strings.TrimSpace(up)
		} else {
			// no base: review the tip commit alone
			return ReviewTarget{Kind: ReviewBranch, Range: tip, Diff: model.DiffSpec{Rev: tip}}, nil
		}
	}
	base = strings.TrimSpace(base)
	rng := base + ".." + tip
	return ReviewTarget{Kind: ReviewBranch, Range: rng, Diff: model.DiffSpec{Rev: rng}}, nil
}
```

**Before coding `BranchReviewTarget`, confirm the git verbs exist:** `grep -n "func (r \*Repo) MergeBase\|func (r \*Repo) UpstreamRef" internal/git/*.go`. If `MergeBase`/`UpstreamRef` do not exist, add the missing verb(s) as a preliminary sub-step in this task following the one-invocation `gitcmd` pattern (`git merge-base main <tip>`, `git rev-parse --abbrev-ref <tip>@{upstream}`), with a tiny verb test — do NOT shell out from domain directly.

- [ ] **Step 4: Run tests, verify pass.** Run: `go test ./internal/domain/ -run 'TestReviewReport|TestSanitizeRange'`. Expected: PASS.

- [ ] **Step 5: Commit.** `git add internal/domain/review.go internal/domain/review_test.go internal/git/ && git commit -m "feat(domain): ReviewReport runs ReviewChanges and persists the report to the state dir"`

---

### Task 5: `gg review` CLI verb

**Files:**
- Create: `internal/cli/review.go`
- Modify: `internal/cli/cli.go` (`runOne` dispatch switch ~`:113`; `var commands` map ~`:134`)
- Test: `internal/cli/review_test.go`; e2e: `e2e/scenarios/s80_cli_review.toml`
- Reference: `internal/cli/show.go`/`log.go` (flag parsing, printing), `internal/cli/diff.go`.

**Interfaces:**
- Consumes: `svc.ReviewReport`, `svc.BranchReviewTarget` (Task 4); `template.ResolveCommand` (Task 1); the config `[[tools.command]]` list.
- Produces: `gg review [<rev>|<A..B>] [--tool <name>] [--working]` → prints the report to stdout, persists it; exit 0 on a report, 1 on tool failure/empty, 2 on usage.

- [ ] **Step 1: Write the failing e2e scenario.** Create `e2e/scenarios/s80_cli_review.toml` modeled on `s79_cli_batch.toml` (read it first). It must: build a repo with two commits; write a repo `.gg.toml` containing a **fake** review tool whose command echoes a fixed report (so no real agent runs), e.g.

```toml
[[tools.command]]
category = "review"
name = "Echo"
mode = "capture"
command = 'printf "FAKE REVIEW of <range>\n"'
```

then run `gg review HEAD~1..HEAD --tool Echo` and assert stdout contains `FAKE REVIEW of HEAD~1..HEAD`. (Consult `.claude/skills/writing-e2e-scenarios` for the exact schema — `[[run]]` args, stdout assertions.)

- [ ] **Step 2: Run it, verify it fails.** Run: `go test ./e2e/ -run s80` (or the harness's scenario runner — check `e2e/`'s test entry). Expected: FAIL (`unknown command review`).

- [ ] **Step 3: Implement.** Create `internal/cli/review.go`:

```go
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/template"
)

func cmdReview(svc *domain.Service, workdir string, rest []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	fs.SetOutput(stderr)
	toolName := fs.String("tool", "", "review tool name (from config); default: the only one")
	working := fs.Bool("working", false, "review uncommitted working changes")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	ctx := context.Background()

	// Resolve the target.
	var target domain.ReviewTarget
	switch {
	case *working:
		target = domain.ReviewTarget{Kind: domain.ReviewWorking, Range: "", Diff: model.DiffSpec{}}
	case fs.NArg() >= 1:
		rng := fs.Arg(0)
		target = domain.ReviewTarget{Kind: domain.ReviewRange, Range: rng, Diff: model.DiffSpec{Rev: rng}}
	default:
		t, err := svc.BranchReviewTarget(ctx, "HEAD")
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		target = t
	}

	// Pick the review tool command from config.
	cmd, err := selectReviewCommand(workdir, *toolName, stderr)
	if err != nil {
		return 1
	}
	resolved, err := template.ResolveCommand(cmd.Command, nil, template.CmdCtx{Range: target.Range, Repo: workdir})
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	res, err := svc.ReviewReport(ctx, target, resolved, []string{"GG_TASK=review"}, time.Now())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	io.WriteString(stdout, res.Content)
	if !strings.HasSuffix(res.Content, "\n") {
		io.WriteString(stdout, "\n")
	}
	fmt.Fprintln(stderr, "report:", res.Path)
	return 0
}

// selectReviewCommand loads config and returns the chosen review command.
func selectReviewCommand(workdir, name string, stderr io.Writer) (config.ToolCommand, error) {
	cfg := loadConfigFor(workdir) // see below
	var cands []config.ToolCommand
	for _, tc := range cfg.Tools.Command {
		if tc.Category != string(exttool.CatReview) {
			continue
		}
		if config.ValidateToolCommand(tc) != nil || template.ValidateCommandTokens(tc.Command, tc.PerFile) != nil {
			continue
		}
		cands = append(cands, tc)
	}
	if len(cands) == 0 {
		fmt.Fprintln(stderr, "error: no review tool configured (see [[tools.command]] category=\"review\")")
		return config.ToolCommand{}, fmt.Errorf("no review tool")
	}
	if name != "" {
		for _, tc := range cands {
			if tc.Name == name {
				return tc, nil
			}
		}
		fmt.Fprintf(stderr, "error: no review tool named %q\n", name)
		return config.ToolCommand{}, fmt.Errorf("no such tool")
	}
	if len(cands) > 1 {
		var names []string
		for _, tc := range cands {
			names = append(names, tc.Name)
		}
		fmt.Fprintf(stderr, "error: multiple review tools; pass --tool (%s)\n", strings.Join(names, ", "))
		return config.ToolCommand{}, fmt.Errorf("ambiguous tool")
	}
	return cands[0], nil
}
```

`loadConfigFor(workdir)` must load the effective config (global + active repo). **Check how the app already loads config** — `grep -rn "config.Load\|ActiveRepoConfigPath\|PrivateRepoPath" cmd/gg internal/app internal/cli` — and factor the existing loader into a small `internal/cli` helper (or call the app's). If the CLI currently never loads config, add a minimal `loadConfigFor` that mirrors the app's resolution: `config.Load(globalPath, config.ActiveRepoConfigPath(committedRepoPath, config.PrivateRepoPath(mainWorktree)))`. Reuse existing helpers; do not hand-roll TOML.

Wire the dispatch in `cli.go` `runOne` switch (note: `cmdReview` needs `workdir`, which `runOne` already has):

```go
	case "review":
		return cmdReview(svc, workdir, rest, stdout, stderr)
```

and add `"review": true,` to `var commands`.

- [ ] **Step 4: Run tests, verify pass.** Run: `go test ./internal/cli/ ./e2e/ -run 'Review|s80'`. Expected: PASS. Also `go build ./cmd/gg`.

- [ ] **Step 5: Commit.** `git add internal/cli/ e2e/scenarios/s80_cli_review.toml && git commit -m "feat(cli): gg review <range> [--tool] [--working] prints and persists an AI review"`

---

### Task 6: TUI review viewer (`reviewView` full-screen layer)

**Files:**
- Create: `internal/tui/review_view.go`
- Modify: `internal/tui/layer_stack.go` (`isFullScreenLayer` switch ~`:34`)
- Test: `internal/tui/review_view_test.go`
- Reference: `internal/tui/history_view.go` (layer shape), `internal/tui/open_external.go` (`openInEditorCmd`), `internal/tui/filter*.go` (`filterMotion`).

**Interfaces:**
- Produces: `type reviewView struct { path, title string; lines []string; scroll int; ... }` implementing the `layer` interface (`update(m, msg) (Model, tea.Cmd)`, `render(m, below string) string`); constructed by `newReviewView(title, path, content string) *reviewView`; pushed via `m.pushLayer`. `e` opens `path` in `$EDITOR`; `esc` pops; `↑/↓`/pgup/pgdn scroll; `/` filter-search (reuse the shared motion).

- [ ] **Step 1: Write the failing test.** Create `internal/tui/review_view_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestReviewViewRendersAndScrolls(t *testing.T) {
	rv := newReviewView("Review: main..HEAD", "/tmp/x.md", "line1\nline2\nline3\n")
	m := Model{width: 80, height: 24}
	out := rv.render(m, "")
	if !strings.Contains(out, "line1") || !strings.Contains(out, "Review: main..HEAD") {
		t.Fatalf("render missing content/title:\n%s", out)
	}
	// esc pops the layer
	m2, _ := rv.update(m.pushLayer(rv), tea.KeyMsg{Type: tea.KeyEsc})
	if len(m2.layers) != 0 {
		t.Fatalf("esc should pop the review layer, layers=%d", len(m2.layers))
	}
}

func TestReviewViewIsFullScreen(t *testing.T) {
	if !isFullScreenLayer(&reviewView{}) {
		t.Fatal("reviewView must be a full-screen layer")
	}
}
```

(Confirm the `Model` field names `width`/`height`/`layers` and the `pushLayer`/pop mechanics against `layer_stack.go` and `model.go`; adjust the test to the real accessors — e.g. layers may be counted via a helper rather than a bare slice.)

- [ ] **Step 2: Run it, verify it fails.** Run: `go test ./internal/tui/ -run TestReviewView`. Expected: FAIL (undefined `newReviewView`).

- [ ] **Step 3: Implement.** Create `internal/tui/review_view.go` modeled on `history_view.go`'s layer shape: store split `lines`, a `scroll` offset, render a titled scrollable box sized to `m.width`/`m.height`, handle `up`/`down`/`pgup`/`pgdn`/`home`/`end` to move `scroll`, `/` to enter the shared filter motion (grep `filterMotion` for the reusable helper and follow one existing caller, e.g. `history_view.go` or `reflog`), `e` → `m.openInEditorCmd(filepath.Base(rv.path), func(ctx) ([]byte, error) { return os.ReadFile(rv.path) })`, `esc` → `m.popLayer()`. Add `*reviewView` to the `isFullScreenLayer` switch:

```go
	case *historyView, *blameView, *irebaseEditor, *hunkPicker, *diffView, *reviewView:
		return true
```

Keep the file focused (~120 lines); it does one thing — display a report.

- [ ] **Step 4: Run tests, verify pass.** Run: `go test ./internal/tui/ -run TestReviewView`. Expected: PASS.

- [ ] **Step 5: Commit.** `git add internal/tui/review_view.go internal/tui/layer_stack.go internal/tui/review_view_test.go && git commit -m "feat(tui): reviewView full-screen report viewer (scroll, / search, e->editor)"`

---

### Task 7: TUI dispatch — `.` menu entries + capture lane

**Files:**
- Create: `internal/tui/review.go` (menu rows + dispatch + spinner state)
- Modify: `internal/tui/tools.go` (`toolUsable` ~`:42` — un-inert `review` capture); `internal/tui/action_menu.go` (append review rows in the panel context builders); `internal/tui/model.go` or the Update switch (handle the new messages)
- Test: `internal/tui/review_test.go`
- Reference: `internal/tui/commit_generate.go` (the capture lane: chooser → approval → dispatch → spinner → gen-guarded apply), `internal/tui/shelf.go`/`bookmark.go` (menu-row pattern), `internal/tui/tool_approval.go` (shared approval).

**Interfaces:**
- Consumes: `svc.ReviewReport`, `svc.BranchReviewTarget` (Task 4); `newReviewView` (Task 6); `m.toolCommands("review")`; the shared `toolCommandApproved`/`rememberToolApproval`/`approvalBoxView`.
- Produces: `.`-menu rows — Commits (`review-commit`: focused commit `<sha>^..<sha>`; `review-range`: two ◉ marks `a..b`), Branches (`review-branch`: `BranchReviewTarget`), Files (`review-working`). Each dispatches the review lane; on success pushes a `reviewView`.

- [ ] **Step 1: Write the failing test.** Create `internal/tui/review_test.go`. Because a live run needs an agent, test the pure pieces: (a) the `ReviewTarget` a focused-commit menu row builds is `<sha>^..<sha>`; (b) the marked-range row builds `<older>..<newer>`; (c) `toolUsable` now accepts a `review` capture block. Example:

```go
func TestReviewTargetFromFocusedCommit(t *testing.T) {
	m := Model{ /* focus=panelCommits, one commit "abc123" */ }
	// set up m so backingIndex(panelCommits) -> a commit with Hash "abc123"
	tgt, ok := m.focusedCommitReviewTarget()
	if !ok || tgt.Range != "abc123^..abc123" {
		t.Fatalf("got %+v ok=%v", tgt, ok)
	}
}

func TestToolUsableAllowsReviewCapture(t *testing.T) {
	m := Model{cfg: /* minimal */}
	tc := config.ToolCommand{Category: "review", Name: "X", Mode: "capture", Command: "echo hi"}
	if err := m.toolUsable(tc); err != nil {
		t.Fatalf("review capture must be usable: %v", err)
	}
}
```

(Fill the `Model` set-up by copying how `commit_generate_test.go` / `shelf_test.go` construct a `Model` with commits and config; grep for the smallest existing constructor.)

- [ ] **Step 2: Run it, verify it fails.** Run: `go test ./internal/tui/ -run TestReview`. Expected: FAIL.

- [ ] **Step 3: Implement.**
  1. In `tools.go` `toolUsable`, change the capture gate to allow review too:

```go
	if tc.Mode == "capture" && tc.Category != string(exttool.CatCommitMessage) && tc.Category != string(exttool.CatReview) {
		return fmt.Errorf("tools: %s: mode \"capture\" is not supported for category %q", tc.Name, tc.Category)
	}
```

  2. In `review.go`, add menu-row builders mirroring `commitShelfRow` (guard `m.opsIdle()` and the right `m.focus`), each returning an `actionRow` whose `run` starts the review lane for its `ReviewTarget`:
     - `focusedCommitReviewRow()` (panelCommits): `Range = sha + "^.." + sha`, `Diff{Rev: sha+"^.."+sha}` (root commit → `Rev: sha`).
     - `markedRangeReviewRow()` (panelCommits, exactly two ◉ marks): order the two by feed position → `older + ".." + newer`.
     - `branchReviewRow()` (panelBranches): call `svc.BranchReviewTarget` at dispatch time (it needs a ctx) — build the target inside the dispatched `tea.Cmd`, not in the row.
     - `workingReviewRow()` (panelFiles/status): `Range = ""`, `Diff = model.DiffSpec{}`.
     Append them in the matching panel context builders in `action_menu.go` (find the Branches/Files equivalents of `appendCommitContextRows`).
  3. Port the capture lane from `commit_generate.go`: a chooser when `len(m.toolCommands("review")) > 1`, the shared first-run approval, an animated spinner while running, and a **gen-guarded** async result (bump a `reviewGen` on dispatch and on esc — a ctx-killed agent returns `*exec.ExitError`, not `context.Canceled`, so the gen check is what distinguishes cancel from failure). The dispatched `tea.Cmd` resolves the command (`template.ResolveCommand` with `CmdCtx{Range, Repo}`), calls `svc.ReviewReport(ctx, target, resolved, []string{"GG_TASK=review"}, time.Now())`, and returns a `reviewDoneMsg{gen, res, err}`. On success (matching gen) push `newReviewView("Review: "+res.Range, res.Path, res.Content)`; on error set `m.statusMsg`.
  4. Reuse the `commit_generate.go` spinner/tick and approval helpers directly where possible (extract a shared helper only if it is a clean lift; otherwise a small copy is fine — do not over-abstract).

- [ ] **Step 4: Run tests, verify pass.** Run: `go test ./internal/tui/`. Expected: PASS. Then `go build ./cmd/gg`.

- [ ] **Step 5: Commit.** `git add internal/tui/ && git commit -m "feat(tui): review .menu entries + capture lane opening the report viewer"`

---

### Task 8: Docs, agentskill, final verification

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`, `internal/agentskill/using-gg.md`, `internal/agentskill/*` (`Version` const), `.claude/skills/adding-external-tools/*` (only if the catalog contract changed)

- [ ] **Step 1: CHANGELOG** — add an "External tools (stage 3: AI review)" entry under `Unreleased` → Added: the three review targets, the in-TUI viewer (`e`→`$EDITOR`), the durable reports, and `gg review`.
- [ ] **Step 2: README** — document `gg review [<rev>|<A..B>] [--tool <name>] [--working]` in the CLI section, and the Commits/Branches/Files `.`-menu "Review … (AI)" entries.
- [ ] **Step 3: CLAUDE.md** — in the package map: `engine.ReviewChanges` (beside `GenerateMessage`, noting the shared `$GG_MESSAGE_FILE` contract + `$GG_REVIEW_DIFF`), `domain.ReviewReport`/`ReviewTarget` + the `reviews` state dir, the `tui` `reviewView` + review `.`-menu lane, the `template` `<range>` token, the `exttool` review templates, and the `cli` `gg review` verb.
- [ ] **Step 4: agentskill** — add a `gg review` entry to `internal/agentskill/using-gg.md`, bump the `// gg:using-gg:vNN` marker and the `agentskill.Version` const (grep for the current value), and note in the commit body that `gg init --update` refreshes installed copies.
- [ ] **Step 5: Full suite + commit.** Run: `./test.sh race`. Expected: `all green`. Then `git add -A && git commit -m "docs: stage-3 review — CHANGELOG/README/CLAUDE.md/using-gg (agentskill vNN)"`.

---

## Self-Review (completed by plan author)

**1. Spec coverage:** three targets → Tasks 4/5/7; capture pipeline + `$GG_MESSAGE_FILE` → Task 2; `<range>` + `$GG_REVIEW_DIFF` → Tasks 1/2; durable reports → Task 4; viewer + `e`→editor → Task 6; `gg review` → Task 5; catalog (both `capture`, verified) → Task 3; un-inert review capture → Task 7; docs/agentskill → Task 8. All spec sections map to a task.

**2. Placeholder scan:** none — the two "confirm the git verb/helper exists" notes (Task 4 `MergeBase`/`UpstreamRef`, Task 5 config loader) are explicit verify-or-add sub-steps with the exact git commands to add, not TBDs.

**3. Type consistency:** `ReviewChanges{Command,Dir,Env,Diff,RangeLabel}` (Task 2) is consumed unchanged by `ReviewReport` (Task 4); `ReviewTarget{Kind,Range,Diff}` and `ReviewResult{Path,Content,Range}` are consistent across Tasks 4/5/7; `template.CmdCtx.Range` (Task 1) is used by Tasks 5/7; `newReviewView(title,path,content)` (Task 6) is called by Task 7; env `GG_MESSAGE_FILE`/`GG_REVIEW_DIFF`/`GG_CONTEXT_FILE` names match between Task 2 (op sets them) and Task 3 (templates read them).
