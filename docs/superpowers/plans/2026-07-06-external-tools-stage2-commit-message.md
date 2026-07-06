# External Tools Stage 2 — `commit_message` Capture Lane — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A `ctrl+g` "generate" key in the commit popup runs a configured
`commit_message` agent headless, hands it the pre-generated staged diff, captures
its output, parses it into subject/body, and fills the (editable) commit fields.

**Architecture:** A new headless **capture lane** — an `engine.GenerateMessage`
operation (behind a `CaptureRunner` seam mirroring `ShellHookRunner`) that builds
two context artifacts from the staged diff, runs the agent, and returns its
captured stdout — plus a pure format-agnostic parser in `internal/exttool`, the
`commit_message` catalog templates, and the commit-popup TUI flow (approval +
confirm-replace + gen-guarded async fill). Distinct from Stage 1's terminal
handover; approval is reused from Stage 1 via the promptstate template-hash.

**Tech Stack:** Go 1.26, Bubble Tea (value-receiver `Model`, pointer layer
fields), the existing engine/domain/git/exttool/promptstate packages.

**Spec:** `docs/superpowers/specs/2026-07-06-external-tools-stage2-commit-message.md`
(read it for full rationale and the live-probe evidence behind the defaults).

## Global Constraints

Every task's requirements implicitly include these (copied from the spec):

- **Prompt is the FIRST argument** after `<bin>` (for capture, after `<bin> -p` /
  `<bin> --task`). Claude's `--allowedTools`/`--disallowedTools` are variadic and
  eat any trailing prompt (the Stage-1 `e83ff4d` bug). Variadic flags go LAST.
- **Catalog dynamic content uses `<env:NAME>` generation tokens only** (rendered
  `${NAME}` POSIX / `%NAME%` Windows by `GenerateCommandFor`), never a raw prose
  value. Commit templates reference `<env:GG_CONTEXT_FILE>` / `<env:GG_STAGED_DIFF>`.
- **Parser is format-agnostic** (`structured_output` → `result` → raw text); the
  feature never hinges on `--json-schema`.
- **Claude is primary; Junie is best-effort** (its capture `.result` is a markdown
  report — the editable fields absorb it). **No yolo variants** — Junie `--brave`
  is interactive-only, useless headless.
- **`MaxDiffBytes = 200 KiB`** cap on the diff; over-cap → stat + a truncation note.
- **Approval is reused** via the promptstate template-hash (`toolCommandHash` over
  the config command text); a **confirm-replace** gate fires only when a field is
  non-empty. **Nothing auto-commits** — `ctrl+s` (the user) stays the only commit
  trigger.
- **Async completion is gen-guarded** (`genGen`, the `pushCheckGen` pattern); `esc`
  cancels the run's `ctx`; the finish handler re-finds the live `*commitPopup` on
  the layer stack (value-receiver `Model`, pointer layer).
- **TUI-only.** No CLI verb, no e2e. `internal/tui` never imports `internal/git`
  (reach git via `internal/domain`; `internal/exttool` is a leaf the TUI imports).
- `capture` mode becomes **live** for `commit_message` (the Stage-1 "not supported
  yet" inert-treatment is lifted for this category).

---

## File Structure

**Created:**
- `internal/exttool/parse.go` — `ParseCaptureMessage`, `SplitMessage` (pure).
- `internal/exttool/parse_test.go`
- `internal/engine/capture_runner.go` — `CaptureRunner`, `ShellCaptureRunner`, `CaptureSpec`.
- `internal/engine/capture_runner_test.go`
- `internal/engine/generate_message.go` — `GenerateMessage` op + `MaxDiffBytes`.
- `internal/engine/generate_message_test.go`
- `internal/tui/commit_generate.go` — `ctrl+g` flow: `startGenerate`, `genMessageCmd`, `genMessageMsg`, gen-guard fill.
- `internal/tui/commit_generate_test.go`
- `internal/tui/tool_approval.go` — shared approval predicate/remember/render.

**Modified:**
- `internal/engine/operation.go` — `Result.Captured`, `OpDeps.CaptureRunner` + nil-safe `captureRunner()`.
- `internal/engine/gitops.go` (or wherever `GitOps` is defined) — add `DiffPatch`/`DiffNumstat`/`LogLines` to the interface if absent.
- `internal/exttool/exttool.go` — `commit_message` templates (Claude, Junie) + prompt consts.
- `internal/tui/commit_popup.go` — generate sub-state fields; `ctrl+g` + esc-while-generating in `update`; spinner line in `box`; `splitMessage` delegates to `exttool.SplitMessage`.
- `internal/tui/conflict_process.go` — repoint approval predicate/remember/render at the shared helpers.
- `internal/tui/tools.go` (or wherever `capture` is marked inert) — make `commit_message`+`capture` a live path.
- `internal/config/template.go` — `commit_message` worked example in the `[tools]` block.
- `CHANGELOG.md`, `README.md`, `CLAUDE.md`, TUI help/footer.

---

## Task 1: Pure parser — `exttool.ParseCaptureMessage` + `SplitMessage`

**Files:**
- Create: `internal/exttool/parse.go`, `internal/exttool/parse_test.go`
- Modify: `internal/tui/commit_popup.go` (delegate `splitMessage`)

**Interfaces:**
- Produces: `func ParseCaptureMessage(stdout []byte) (subject, body string, err error)`;
  `func SplitMessage(msg string) (subject, body string)`; `var ErrEmptyMessage error`.
- `SplitMessage` is byte-for-byte the current `tui.splitMessage` logic, relocated
  so both the parser and the amend pre-fill share one rule.

- [ ] **Step 1: Write the failing test** (`internal/exttool/parse_test.go`). Fixtures
  drawn from the spec's live probes.

```go
package exttool

import "testing"

func TestParseCaptureMessage(t *testing.T) {
	cases := []struct{ name, in, subj, body string; wantErr bool }{
		{"claude_plain", "Fix the thing\n\nBecause reasons.", "Fix the thing", "Because reasons.", false},
		{"claude_json_result", `{"type":"result","is_error":false,"result":"Fix the thing\n\nBecause reasons."}`, "Fix the thing", "Because reasons.", false},
		{"claude_structured", `{"type":"result","is_error":false,"result":"{\"subject\":\"S\"}","structured_output":{"subject":"Add cap","body":"Bound the diff."}}`, "Add cap", "Bound the diff.", false},
		{"is_error", `{"type":"result","is_error":true,"result":"tool blew up"}`, "", "", true},
		{"junie_report", "{\"result\":\"### Summary\\n- did a thing\\n\\n### Changes\\n- x\"}", "### Summary", "- did a thing\n\n### Changes\n- x", false},
		{"top_level_subject", `{"subject":"Direct","body":"B"}`, "Direct", "B", false},
		{"garbage_nonjson", "just text here", "just text here", "", false},
		{"empty", "   \n  ", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			subj, body, err := ParseCaptureMessage([]byte(c.in))
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
			if err == nil && (subj != c.subj || body != c.body) {
				t.Fatalf("got (%q,%q) want (%q,%q)", subj, body, c.subj, c.body)
			}
		})
	}
}
```

- [ ] **Step 2: Run it — expect FAIL** (`go test ./internal/exttool/ -run TestParseCaptureMessage`): undefined `ParseCaptureMessage`.

- [ ] **Step 3: Implement** (`internal/exttool/parse.go`):

```go
package exttool

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// ErrEmptyMessage means the tool produced no usable message and no explicit error.
var ErrEmptyMessage = errors.New("exttool: empty commit message from tool")

type captureEnvelope struct {
	IsError          bool `json:"is_error"`
	Result           string `json:"result"`
	Subject          string `json:"subject"` // defensive: some tools may emit top-level
	Body             string `json:"body"`
	StructuredOutput *struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
	} `json:"structured_output"`
}

// ParseCaptureMessage interprets an agent's captured stdout as a commit message
// (subject + body), format-agnostic across the shapes gg's catalog tools emit:
// Claude plain text; Claude --output-format json (.result); Claude --json-schema
// (.structured_output); Junie --output-format json (.result, report-wrapped);
// and non-JSON fallbacks. A tool-reported error (is_error) is returned as err.
func ParseCaptureMessage(stdout []byte) (subject, body string, err error) {
	t := bytes.TrimSpace(stdout)
	if len(t) > 0 && t[0] == '{' {
		var env captureEnvelope
		if json.Unmarshal(t, &env) == nil {
			switch {
			case env.IsError:
				msg := strings.TrimSpace(env.Result)
				if msg == "" {
					msg = "tool reported an error"
				}
				return "", "", errors.New(msg)
			case env.StructuredOutput != nil && strings.TrimSpace(env.StructuredOutput.Subject) != "":
				return strings.TrimSpace(env.StructuredOutput.Subject),
					strings.TrimSpace(env.StructuredOutput.Body), nil
			case strings.TrimSpace(env.Subject) != "":
				return strings.TrimSpace(env.Subject), strings.TrimSpace(env.Body), nil
			case env.Result != "":
				subject, body = SplitMessage(strings.TrimSpace(env.Result))
				if subject == "" {
					return "", "", ErrEmptyMessage
				}
				return subject, body, nil
			}
		}
		// JSON-looking but unrecognized: fall through to raw-text handling.
	}
	subject, body = SplitMessage(string(t))
	if subject == "" {
		return "", "", ErrEmptyMessage
	}
	return subject, body, nil
}

// SplitMessage splits a commit message into (subject, body): the first line is
// the subject, the rest (after leading blank lines) the body. The one canonical
// split rule (the TUI amend pre-fill delegates here).
func SplitMessage(msg string) (subject, body string) {
	msg = strings.TrimRight(msg, "\n")
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		return msg[:i], strings.TrimLeft(msg[i+1:], "\n")
	}
	return msg, ""
}
```

- [ ] **Step 4: Run it — expect PASS.**

- [ ] **Step 5: Delegate the TUI split.** In `internal/tui/commit_popup.go`, replace
  the body of `splitMessage` with `return exttool.SplitMessage(msg)` (add the
  `exttool` import). Keeps amend byte-identical (same logic, one home). Run
  `go test ./internal/tui/ -run 'Commit|Amend|Reword'` — expect PASS.

- [ ] **Step 6: Commit** (`feat(exttool): format-agnostic commit-message parser + shared SplitMessage`).

---

## Task 2: Engine capture seam — `CaptureRunner` + `ShellCaptureRunner` + `Result.Captured`

**Files:**
- Create: `internal/engine/capture_runner.go`, `internal/engine/capture_runner_test.go`
- Modify: `internal/engine/operation.go`

**Interfaces:**
- Produces: `type CaptureSpec struct{ Dir string; Env []string; Command string }`;
  `type CaptureRunner interface { Capture(ctx, CaptureSpec, onLine func(string)) ([]byte, error) }`;
  `type ShellCaptureRunner struct{}`; `Result.Captured string`; `OpDeps.CaptureRunner`
  + `func (d OpDeps) captureRunner() CaptureRunner` (nil ⇒ `ShellCaptureRunner{}`).
- Reuses `hookShellArgv` and `hookLineWriter` from `hook_runner.go` (same package).

- [ ] **Step 1: Write the failing test** (`capture_runner_test.go`):

```go
package engine

import (
	"context"
	"strings"
	"testing"
)

func TestShellCaptureRunnerCapturesStdoutStreamsStderr(t *testing.T) {
	if testing.Short() { t.Skip() }
	var lines []string
	out, err := ShellCaptureRunner{}.Capture(context.Background(),
		CaptureSpec{Command: "printf 'hello\\nworld'; printf 'progress\\n' 1>&2"},
		func(l string) { lines = append(lines, l) })
	if err != nil { t.Fatal(err) }
	if string(out) != "hello\nworld" { t.Fatalf("stdout=%q", out) }
	if len(lines) != 1 || lines[0] != "progress" { t.Fatalf("stderr lines=%v", lines) }
}

func TestShellCaptureRunnerNonZeroExitReturnsErr(t *testing.T) {
	if testing.Short() { t.Skip() }
	_, err := ShellCaptureRunner{}.Capture(context.Background(),
		CaptureSpec{Command: "exit 3"}, func(string) {})
	if err == nil { t.Fatal("want error on non-zero exit") }
}
```

(Skip the shell tests on Windows via a `runtime.GOOS == "windows"` guard in the
test, or an `sh`-availability check — mirror how existing engine tests gate POSIX
shell behavior.)

- [ ] **Step 2: Run — expect FAIL** (undefined `ShellCaptureRunner`).

- [ ] **Step 3: Implement** (`capture_runner.go`), mirroring `ShellHookRunner`:

```go
package engine

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// CaptureSpec is one headless capture invocation. Command is a shell command
// line, run via a temp script + the platform shell (the ShellHookRunner path).
type CaptureSpec struct {
	Dir     string
	Env     []string
	Command string
}

// CaptureRunner runs a command headless and returns its full stdout, streaming
// stderr lines to onLine. Unlike HookRunner a non-zero exit is an error (a
// failed capture has no usable output). stdin is /dev/null; ctx kills it.
type CaptureRunner interface {
	Capture(ctx context.Context, spec CaptureSpec, onLine func(string)) (stdout []byte, err error)
}

// ShellCaptureRunner is the production CaptureRunner: temp script + $SHELL/cmd,
// stdout to a buffer, stderr streamed as lines.
type ShellCaptureRunner struct{}

func (ShellCaptureRunner) Capture(ctx context.Context, spec CaptureSpec, onLine func(string)) ([]byte, error) {
	ext := ".sh"
	if runtime.GOOS == "windows" {
		ext = ".bat"
	}
	f, err := os.CreateTemp("", "gg-capture-*"+ext)
	if err != nil {
		return nil, err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.WriteString(spec.Command); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}

	shell, args := hookShellArgv(name)
	cmd := exec.CommandContext(ctx, shell, args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.Stdin = nil // /dev/null: a prompting agent gets EOF, never hangs.
	var out bytes.Buffer
	cmd.Stdout = &out
	lw := &hookLineWriter{onLine: onLine}
	cmd.Stderr = lw
	cmd.WaitDelay = 3 * time.Second // Stage-1 grandchild-pipe guard

	err = cmd.Run()
	lw.flush()
	return out.Bytes(), err // err is *exec.ExitError on non-zero exit
}
```

- [ ] **Step 4: Add `Result.Captured` + `OpDeps.CaptureRunner`** (`operation.go`):
  add `Captured string` to `Result` (doc: "captured stdout, set only by capture
  ops like GenerateMessage"); add `CaptureRunner CaptureRunner` to `OpDeps` and a
  nil-safe `func (d OpDeps) captureRunner() CaptureRunner { if d.CaptureRunner == nil { return ShellCaptureRunner{} }; return d.CaptureRunner }`.

- [ ] **Step 5: Run** `go test ./internal/engine/ -run Capture` — expect PASS; `go build ./...`.

- [ ] **Step 6: Commit** (`feat(engine): CaptureRunner seam + Result.Captured for the capture lane`).

---

## Task 3: Engine op — `GenerateMessage`

**Files:**
- Create: `internal/engine/generate_message.go`, `internal/engine/generate_message_test.go`
- Modify: `GitOps` interface definition (add verbs if absent).

**Interfaces:**
- Consumes: `deps.Repo.DiffPatch/DiffNumstat(ctx, model.DiffSpec{Cached:true})`,
  `deps.Repo.LogLines(ctx, "HEAD", 20)`, `deps.captureRunner()`, `deps.emit`.
- Produces: `type GenerateMessage struct{ Command, Dir string; Env []string }`;
  `func (GenerateMessage) Run(...) (Result, error)`; `LockMode()==repogate.Read`;
  `const MaxDiffBytes = 200 << 10`. Returns `Result.Captured` = agent stdout.

- [ ] **Step 1: Ensure `GitOps` declares the verbs.** Grep
  (`grep -rn 'GitOps' internal/engine/`). First confirm `*git.Repo` is the ONLY
  implementer — if any engine test uses a hand-written `GitOps` fake/mock, widening
  the interface breaks its compilation (almost certainly safe: engine tests use a
  real repo + `FakeRunner` at the gitexec layer, but confirm before widening). If
  `DiffPatch`, `DiffNumstat`, `LogLines` are not already methods on `GitOps`, add
  their exact signatures (they exist on `*git.Repo`, which satisfies `GitOps`):
  `DiffPatch(context.Context, model.DiffSpec) (string, error)`,
  `DiffNumstat(context.Context, model.DiffSpec) (string, error)`,
  `LogLines(context.Context, string, int) ([]model.LogLine, error)`.

- [ ] **Step 2: Write the failing test** (`generate_message_test.go`). Real git in a
  `t.TempDir` (use the package's `newRepo`/`newTestRepo` helper) with one staged
  change; a fake runner recording the spec:

```go
type fakeCapture struct {
	spec   CaptureSpec
	stdout string
	err    error
}
func (f *fakeCapture) Capture(_ context.Context, s CaptureSpec, _ func(string)) ([]byte, error) {
	f.spec = s
	return []byte(f.stdout), f.err
}

func TestGenerateMessageBuildsContextAndCaptures(t *testing.T) {
	dir, repo := newTestRepo(t)            // helper: real repo + *git.Repo
	writeFile(t, dir, "a.txt", "one\ntwo\n")
	gitAdd(t, dir, "a.txt")                // stage a change

	fc := &fakeCapture{stdout: `{"result":"Subject line\n\nBody text."}`}
	res, err := GenerateMessage{Command: "true", Dir: dir}.Run(
		context.Background(), OpDeps{Repo: repo, CaptureRunner: fc})
	if err != nil { t.Fatal(err) }
	if res.Captured != fc.stdout { t.Fatalf("captured=%q", res.Captured) }

	// The runner received env pointing at two existing files with the right content.
	env := envMap(fc.spec.Env)                        // helper: []"K=V" → map
	diffPath, ctxPath := env["GG_STAGED_DIFF"], env["GG_CONTEXT_FILE"]
	assertFileContains(t, diffPath, "a.txt")          // the staged patch
	assertFileContains(t, ctxPath, "a.txt")           // stat block names the file
	// Temp files are cleaned up after Run returns.
	if _, err := os.Stat(diffPath); !os.IsNotExist(err) { t.Fatal("diff temp not removed") }
}

func TestGenerateMessageLockModeRead(t *testing.T) {
	if GenerateMessage{}.LockMode() != repogate.Read { t.Fatal("want Read") }
}
```

Add an over-cap test (stage a >`MaxDiffBytes` change → `GG_STAGED_DIFF` file holds a
truncation note, `GG_CONTEXT_FILE` still holds the stat) and an empty-staged test
(no staged change → no error).

- [ ] **Step 3: Run — expect FAIL.**

- [ ] **Step 4: Implement** (`generate_message.go`):

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

// MaxDiffBytes caps the staged diff handed to the agent; a larger diff is
// replaced by a stat + truncation note (the stat still lists every file).
const MaxDiffBytes = 200 << 10

// GenerateMessage runs a commit_message agent headless and returns its captured
// stdout (Result.Captured). It first writes two context artifacts from the
// staged diff — a labeled summary ($GG_CONTEXT_FILE) and the full unified diff
// ($GG_STAGED_DIFF) — then runs the (resolved, approved) command via the
// CaptureRunner, then removes them. LockMode Read: git reads only; no ref/tree
// writes by gg. Approval is the caller's (TUI) responsibility, not the op's.
type GenerateMessage struct {
	Command string   // resolved, approved shell command line
	Dir     string   // repo/worktree root
	Env     []string // caller env additions (e.g. GG_TASK=commit_message)
}

func (op GenerateMessage) LockMode() repogate.Mode { return repogate.Read }

func (op GenerateMessage) Run(ctx context.Context, deps OpDeps) (Result, error) {
	diff, err := deps.Repo.DiffPatch(ctx, model.DiffSpec{Cached: true})
	if err != nil {
		return Result{}, err
	}
	stat, _ := deps.Repo.DiffNumstat(ctx, model.DiffSpec{Cached: true})
	log, _ := deps.Repo.LogLines(ctx, "HEAD", 20)

	truncated := len(diff) > MaxDiffBytes
	diffBody := diff
	if truncated {
		diffBody = fmt.Sprintf("(diff truncated: %d bytes exceeds the %d KiB cap — inspect specific files with git)\n",
			len(diff), MaxDiffBytes>>10)
	}
	diffPath, err := writeTempFile("gg-staged-*.diff", diffBody)
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(diffPath)
	ctxPath, err := writeTempFile("gg-ctx-*.txt", buildSummary(diffPath, stat, log, truncated))
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(ctxPath)

	env := append(append([]string{}, os.Environ()...), op.Env...)
	env = append(env,
		"GG_CONTEXT_FILE="+ctxPath,
		"GG_STAGED_DIFF="+diffPath,
		"GG_REPO="+op.Dir,
	)
	stdout, runErr := deps.captureRunner().Capture(ctx,
		CaptureSpec{Dir: op.Dir, Env: env, Command: op.Command},
		func(line string) { deps.emit(ctx, GitLine{Raw: line}) })
	if runErr != nil {
		return Result{Captured: string(stdout)}, runErr
	}
	return Result{Captured: string(stdout), Summary: "generated commit message"}, nil
}

func buildSummary(diffPath, stat string, log []model.LogLine, truncated bool) string {
	var b strings.Builder
	b.WriteString("# gg — write a commit message for the staged changes.\n")
	b.WriteString("# Output ONLY the message (subject, blank line, body). Do not commit or edit files.\n")
	b.WriteString("# Full unified diff: " + diffPath)
	if truncated {
		b.WriteString("  (truncated — inspect files with git)")
	}
	b.WriteString("\n\n## Files changed (git diff --cached --stat)\n")
	if strings.TrimSpace(stat) == "" {
		b.WriteString("(no staged changes)\n")
	} else {
		b.WriteString(stat + "\n")
	}
	b.WriteString("\n## Recent commit subjects (match this style)\n")
	for _, l := range log {
		b.WriteString(l.Subject + "\n")
	}
	return b.String()
}

func writeTempFile(pattern, content string) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
```

  (`DiffNumstat` returns the `-z`/numstat form; if a human-readable stat block is
  preferred, render `l.Hash`/counts as needed — the test only asserts the file
  names appear, so either is acceptable. Keep it simple: the numstat string is
  fine as the "stat block".)

- [ ] **Step 5: Run** `go test ./internal/engine/ -run GenerateMessage` — expect PASS; `go build ./...`.

- [ ] **Step 6: Commit** (`feat(engine): GenerateMessage capture op — staged diff → context files → agent`).

---

## Task 4: Catalog — `commit_message` templates

**Files:** Modify `internal/exttool/exttool.go`; test `internal/exttool/exttool_test.go`.

**Interfaces:** adds `CatCommitMessage`/`ModeCapture` rows to `Builtins()` for
`claude` and `junie`. No new exported symbols.

- [ ] **Step 1: Write the failing test** (append to `exttool_test.go`):

```go
func TestBuiltinsCommitMessageTemplates(t *testing.T) {
	var claude, junie *CommandTemplate
	for _, tl := range Builtins() {
		for i := range tl.Commands {
			c := &tl.Commands[i]
			if c.Category != CatCommitMessage { continue }
			if c.Mode != ModeCapture { t.Fatalf("%s: commit_message must be capture", c.Name) }
			if c.OptIn { t.Fatalf("%s: no yolo for capture lane", c.Name) }
			switch tl.ID {
			case "claude": claude = c
			case "junie": junie = c
			}
		}
	}
	if claude == nil || junie == nil { t.Fatal("want claude + junie commit_message templates") }

	// Prompt is the FIRST arg after <bin> (-p/--task); env tokens render per-OS.
	gen := GenerateCommandFor(*claude, "claude", "linux")
	if !strings.HasPrefix(gen, `claude -p "`) { t.Fatalf("claude prompt not first: %q", gen) }
	if !strings.Contains(gen, "${GG_CONTEXT_FILE}") || !strings.Contains(gen, "${GG_STAGED_DIFF}") {
		t.Fatalf("claude missing env refs: %q", gen)
	}
	// Variadic --allowedTools must come AFTER the prompt.
	if strings.Index(gen, "--allowedTools") < strings.Index(gen, `"`) {
		t.Fatal("allowedTools before prompt")
	}
}
```

- [ ] **Step 2: Run — expect FAIL.**

- [ ] **Step 3: Implement** — add prompt consts + Builtins rows (`exttool.go`):

```go
// claudeCommitPrompt: capture-lane commit-message prompt. Dynamic content via
// <env:...> only (injection posture). Prompt is the FIRST arg after `<bin> -p`.
const claudeCommitPrompt = `"Write a git commit message for the staged changes. Read the summary at <env:GG_CONTEXT_FILE> (files changed, recent-commit style) and, for detail, the full diff at <env:GG_STAGED_DIFF>. Output ONLY the commit message and nothing else: a concise imperative subject line (max ~72 chars), a blank line, then a short body explaining what changed and why. No preamble, no markdown headings, no code fences. If the diff file notes it was truncated, inspect specific files with git."`

// --output-format json → a parseable envelope (.result). Read is on the
// allowlist so the agent can open the context files; git verbs stay as a
// drill-down fallback for a truncated diff. Variadic --allowedTools is LAST.
const claudeCommitCommand = `<bin> -p ` + claudeCommitPrompt + ` \
  --output-format json \
  --allowedTools "Read" "Bash(git diff *)" "Bash(git log *)" "Bash(git show *)" "Bash(git status *)"`

// junieCommitPrompt: best-effort — Junie's json .result is a markdown report
// (spec §probes); the parser splits it and the editable fields absorb it.
const junieCommitPrompt = `"Write a git commit message for the staged changes. The change summary is at <env:GG_CONTEXT_FILE> and the full diff at <env:GG_STAGED_DIFF>. Output ONLY the commit message: a concise subject line, a blank line, then a short body. Do not run git commit and do not modify any files."`

const junieCommitCommand = `<bin> --task ` + junieCommitPrompt + ` --output-format json --skip-update-check`
```

  In `Builtins()`, append to claude's `Commands`:
  `{Category: CatCommitMessage, Name: "Claude", Mode: ModeCapture, Command: claudeCommitCommand}`
  and to junie's:
  `{Category: CatCommitMessage, Name: "Junie", Mode: ModeCapture, Command: junieCommitCommand}`.

- [ ] **Step 4: Run** `go test ./internal/exttool/` — expect PASS.

- [ ] **Step 5: Commit** (`feat(exttool): commit_message capture templates (Claude, Junie)`).

---

## Task 5: Shared tool-approval helpers

**Files:** Create `internal/tui/tool_approval.go`; modify `internal/tui/conflict_process.go`;
test `internal/tui/tool_approval_test.go`.

**Interfaces:**
- Produces: `func (m Model) toolCommandApproved(command string) bool`;
  `func (m Model) rememberToolApproval(command string)` (no-op when `m.promptStore == nil`);
  `func approvalBoxView(command string, width int) string` (the resolved-command
  preview + Run/Cancel hint box).
- Note: the conflict lane keeps its process-owned `confToolApprove` sub-state
  (the conflict process preempts the layer stack, so a pushed approval layer is
  unreachable — the Stage-1 reason). It **adopts these helpers** for the
  predicate, the remember, and the box render; the commit-popup lane uses the
  same three from its own sub-state. Shared logic, not a shared layer.

- [ ] **Step 1: Write the failing test** — inject a temp promptstate store (the
  `promptTestModel` pattern; do NOT let `New(nil)` touch the real machine file):

```go
func TestToolApprovalRoundTrip(t *testing.T) {
	m := promptTestModel(t)                 // helper: Model with a temp promptstate.Store
	const cmd = `claude -p "x"`
	if m.toolCommandApproved(cmd) { t.Fatal("fresh command must be unapproved") }
	m.rememberToolApproval(cmd)
	if !m.toolCommandApproved(cmd) { t.Fatal("remembered command must be approved") }
	if m.toolCommandApproved(cmd+" --edited") { t.Fatal("edited text must re-prompt") }
}
```

- [ ] **Step 2: Run — expect FAIL.**

- [ ] **Step 3: Implement** (`tool_approval.go`) — factor from the existing
  `conflict_process.go` gate (`ApprovedToolCommands`/`ApproveToolCommand` +
  `toolCommandHash` + `toolRepoKey`):

```go
func (m Model) toolCommandApproved(command string) bool {
	if m.promptStore == nil {
		return false
	}
	return m.promptStore.ApprovedToolCommands(m.toolRepoKey())[toolCommandHash(command)]
}

func (m Model) rememberToolApproval(command string) {
	if m.promptStore == nil {
		return
	}
	_ = m.promptStore.ApproveToolCommand(m.toolRepoKey(), toolCommandHash(command))
}

// approvalBoxView renders the first-run approval preview shared by the conflict
// and commit-message lanes: the fully resolved command + a Run/Cancel hint.
func approvalBoxView(command string, width int) string { /* extract from confToolApprove render */ }
```

- [ ] **Step 4: Repoint the conflict lane** — replace its inline promptstate
  predicate/remember calls with `toolCommandApproved`/`rememberToolApproval`, and
  its approval-box drawing with `approvalBoxView`. Run
  `go test ./internal/tui/ -run 'Conflict|Tool'` — expect PASS (conflict tests are
  the safety net).

- [ ] **Step 5: Commit** (`refactor(tui): shared tool-approval predicate/remember/render`).

---

## Task 6: Commit-popup generate mechanic (`ctrl+g`)

**Files:** Create `internal/tui/commit_generate.go`, `internal/tui/commit_generate_test.go`;
modify `internal/tui/commit_popup.go`, `internal/tui/model.go` (the `genMessageMsg`
case + a `genCancel` field), and the `capture`-inert guard site.

**Interfaces:**
- Adds to `commitPopup`: `generating bool`, `genGen int`, `genCmd config.ToolCommand`.
- Adds to `Model`: `genCancel context.CancelFunc`.
- Produces: `func (m Model) startGenerate(p *commitPopup) (Model, tea.Cmd)`;
  `func (m Model) genMessageCmd(command string, gen int, ctx context.Context) tea.Cmd`;
  `type genMessageMsg struct{ gen int; subject, body string; err error }`;
  `func (m Model) applyGeneratedMessage(msg genMessageMsg) Model`;
  `func (m Model) escGenerate(p *commitPopup) Model` (the esc-cancel branch).

**Implementer notes:**
- `svc.Execute` holds the repo's **Read** reservation for the op's *entire* run —
  i.e. the full 30–60 s agent runtime, not just the context-build. Read is shared
  (concurrent background reads are fine) and the user is parked in the popup, so
  this is low-harm; but the writer-preferring gate means a write op attempted
  during generation queues until it finishes. Acceptable for Stage 2 — a known
  property, not a bug to fix.
- Keep `ctrl+g` in `commitPopup.update`, never in `applyEditKey` (reword/irebase
  share `applyEditKey`; generate must not leak into a message-only reword).

- [ ] **Step 1: Make `capture` live for `commit_message`.** Grep the Stage-1
  inert-treatment (`grep -rn 'capture' internal/tui/tools.go internal/tui/*.go |
  grep -i 'support\|inert\|note'`). Where a `capture`-mode command is skipped/noted
  as unsupported, allow it through for `CatCommitMessage`. Keep `review`+`capture`
  inert (Stage 3). Add/adjust a unit test asserting `toolCommands("commit_message")`
  returns a configured capture command.

- [ ] **Step 2: Write the failing test** (`commit_generate_test.go`) — a model with a
  configured commit_message tool + a staged change; drive `ctrl+g`; assert the
  fields fill on a `genMessageMsg`, and the guards no-op:

```go
func TestGenerateFillsFieldsGenGuarded(t *testing.T) {
	m := commitGenTestModel(t)              // helper: staged change + a commit_message tool + approved
	p := &commitPopup{}
	m = m.pushLayer(p)
	m, _ = m.startGenerate(p)               // dispatches; sets generating + genGen
	if !p.generating { t.Fatal("want generating") }
	gen := p.genGen

	// A stale result (wrong gen) is dropped.
	m = m.applyGeneratedMessage(genMessageMsg{gen: gen - 1, subject: "stale"})
	if p.title.Value() == "stale" { t.Fatal("stale result must be dropped") }

	// The live result fills subject/body and clears generating.
	m = m.applyGeneratedMessage(genMessageMsg{gen: gen, subject: "Add cap", body: "Bound diff."})
	if p.title.Value() != "Add cap" || p.desc.Value() != "Bound diff." { t.Fatal("fields not filled") }
	if p.generating { t.Fatal("still generating") }
}

func TestGenerateNoOpGuards(t *testing.T) {
	m := commitGenTestModel(t)
	// nothing staged → hint, no dispatch
	m2 := m ; m2.status = model.WorkingTreeStatus{}
	p := &commitPopup{} ; m2 = m2.pushLayer(p)
	m2, cmd := m2.startGenerate(p)
	if cmd != nil || p.generating { t.Fatal("nothing-staged must no-op") }
	if m2.statusMsg == "" { t.Fatal("want a hint") }
}

func TestGenerateEscCancelDropsLateResult(t *testing.T) {
	m := commitGenTestModel(t)
	p := &commitPopup{} ; m = m.pushLayer(p)
	m, _ = m.startGenerate(p)
	gen := p.genGen
	m = m.escGenerate(p)                    // the esc-while-generating handler
	if p.generating { t.Fatal("esc must stop generating") }
	// A result from the cancelled run (with the old gen + a killed error) is DROPPED
	// silently — no spurious statusMsg, no field change.
	m = m.applyGeneratedMessage(genMessageMsg{gen: gen, err: errKilled})
	if m.statusMsg != "" { t.Fatalf("cancel must not surface an error: %q", m.statusMsg) }
	if p.title.Value() != "" { t.Fatal("fields must be untouched") }
}
```

(Factor the esc branch into `escGenerate(p)` so the test can call it directly.)

- [ ] **Step 3: Implement** (`commit_generate.go`):

```go
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/template"
)

type genMessageMsg struct {
	gen     int
	subject string
	body    string
	err     error
}

// startGenerate begins a commit_message capture run for the commit popup. Task 7
// inserts the chooser/approval/confirm gates between the guards and the dispatch.
func (m Model) startGenerate(p *commitPopup) (Model, tea.Cmd) {
	if m.status.Counts().Staged == 0 { // confirm the Counts staged field name
		m.statusMsg = "nothing staged to describe"
		return m, nil
	}
	cmds := m.toolCommands(string(exttool.CatCommitMessage))
	if len(cmds) == 0 {
		m.statusMsg = "no commit-message tool configured (Settings → External tools)"
		return m, nil
	}
	chosen := cmds[0] // Task 7: chooser when len > 1
	resolved, err := template.ResolveCommand(chosen.Command, template.CmdCtx{Repo: m.currentWorktree})
	if err != nil {
		m.statusMsg = "generate: " + err.Error()
		return m, nil
	}
	return m.dispatchGenerate(p, resolved)
}

// dispatchGenerate arms the run (Task 7's gates call this once approved/confirmed).
func (m Model) dispatchGenerate(p *commitPopup, resolvedCommand string) (Model, tea.Cmd) {
	p.generating = true
	p.genGen++
	ctx, cancel := context.WithCancel(context.Background())
	m.genCancel = cancel
	return m, m.genMessageCmd(resolvedCommand, p.genGen, ctx)
}

func (m Model) genMessageCmd(command string, gen int, ctx context.Context) tea.Cmd {
	svc, dir := m.svc, m.currentWorktree
	return func() tea.Msg {
		op := engine.GenerateMessage{Command: command, Dir: dir, Env: []string{"GG_TASK=commit_message"}}
		res, err := svc.Execute(ctx, op, nil, nil) // synchronous, stageCmd pattern
		if err != nil {
			return genMessageMsg{gen: gen, err: err}
		}
		subject, body, perr := exttool.ParseCaptureMessage([]byte(res.Captured))
		if perr != nil {
			return genMessageMsg{gen: gen, err: perr}
		}
		return genMessageMsg{gen: gen, subject: subject, body: body}
	}
}

// applyGeneratedMessage fills the live commit popup, gen-guarded.
func (m Model) applyGeneratedMessage(msg genMessageMsg) Model {
	p := m.topCommitPopup() // helper: *commitPopup if it's the active layer, else nil
	if p == nil || msg.gen != p.genGen {
		return m // stale / popup closed / repo switched
	}
	p.generating = false
	m.genCancel = nil
	if msg.err != nil {
		m.statusMsg = "generate: " + msg.err.Error()
		return m
	}
	p.title = newTextField(msg.subject)
	p.desc = newTextField(msg.body)
	p.field = 0
	return m
}
```

- [ ] **Step 4: Wire keys + msg + spinner.**
  - `commit_popup.go` `update` — **in `update`, NOT `applyEditKey`** (reword/irebase
    reuse `applyEditKey` and have no staged index; generate must not leak there).
    At the top, before `applyEditKey`:
    `if msg.Type == tea.KeyCtrlG && !p.generating { return m.startGenerate(p) }`
    and, while generating, route esc to `escGenerate` and swallow the rest:
    `if p.generating { if msg.Type == tea.KeyEsc { return m.escGenerate(p), nil }; return m, nil }`.
    `escGenerate(p)` cancels the run ctx, clears `genCancel`, **bumps `p.genGen`**,
    and clears `p.generating`. The `genGen` bump is essential: a ctx-killed
    subprocess returns from `svc.Execute` as `*exec.ExitError` ("signal: killed"),
    NOT `context.Canceled`, so the gen-guard in `applyGeneratedMessage` is what
    drops the late error result — do NOT rely on `errors.Is(err, context.Canceled)`.
    Without the bump, every deliberate esc shows a spurious
    `"generate: signal: killed"` statusMsg (the regression `TestGenerateEscCancelDropsLateResult` covers).
  - `commit_popup.go` `box`: when `p.generating`, render a status line
    `⟳ generating message… ([esc] to cancel)`; add `[ctrl+g] generate` to the hint line.
  - `model.go` `Update`: add `case genMessageMsg: return m.applyGeneratedMessage(msg), nil`.
  - Add `genCancel context.CancelFunc` to `Model`; clear + cancel it in `reRoot`
    (a stale run after a repo switch must not fill the new repo's popup — `genGen`
    also guards, but cancel frees the process).
  - Add `topCommitPopup()` helper (inspect the top layer for `*commitPopup`).

- [ ] **Step 5: Run** `go test ./internal/tui/ -run Generate` and `go build ./...` — expect PASS.

- [ ] **Step 6: Commit** (`feat(tui): commit-popup ctrl+g generate mechanic (capture lane)`).

---

## Task 7: Generate gates — chooser, approval, confirm-replace

**Files:** Modify `internal/tui/commit_generate.go`, `internal/tui/commit_popup.go`;
test `internal/tui/commit_generate_test.go`.

**Interfaces:**
- Adds `commitPopup` sub-state: `approving string` (resolved cmd awaiting Run/Cancel),
  `confirming string` (resolved cmd awaiting Replace/Cancel), `choosing []config.ToolCommand`.
- These are sub-states of `commitPopup` (it owns keys while open), NOT pushed layers.

- [ ] **Step 1: Write failing tests:** (a) `len(cmds) > 1` → `choosing` populated,
  selecting one advances; (b) an unapproved command → `approving` set, showing
  `approvalBoxView`, `y`/enter runs + `rememberToolApproval`, `esc` cancels;
  (c) non-empty title/desc → `confirming` set before the run, Replace runs / Cancel aborts;
  (d) empty fields skip the confirm.

- [ ] **Step 2: Run — expect FAIL.**

- [ ] **Step 3: Implement** — insert the gates into `startGenerate` (replacing the
  `chosen := cmds[0]` shortcut and the direct `dispatchGenerate`):
  1. `len(cmds) > 1` → set `p.choosing = cmds`, return (render a numbered picker in
     `box`; a digit/enter selects → resolve → step 2).
  2. resolve → if `!m.toolCommandApproved(chosen.Command)` → set `p.approving = resolved`,
     return (render `approvalBoxView`; `y`/enter → `m.rememberToolApproval(chosen.Command)`
     + step 3; `esc`/`n` → clear, no-op).
  3. if `strings.TrimSpace(p.title.Value())+strings.TrimSpace(p.desc.Value()) != ""` →
     set `p.confirming = resolved`, return (render "Replace current message? [y]es / [esc] no";
     `y`/enter → `dispatchGenerate`; `esc` → clear, no-op). Else `dispatchGenerate` directly.
  - Route the sub-state keys at the top of `commit_popup.go` `update` (before the
    generate/edit keys): when `p.choosing != nil` / `p.approving != ""` /
    `p.confirming != ""`, handle that sub-state's keys and return. Approval remembers
    on the config command text (`chosen.Command`), not the resolved text (hash stability).

- [ ] **Step 4: Run** `go test ./internal/tui/ -run Generate` — expect PASS.

- [ ] **Step 5: Commit** (`feat(tui): generate gates — tool chooser, first-run approval, confirm-replace`).

---

## Task 8: Config example + docs

**Files:** Modify `internal/config/template.go` (or the `settingDocs`/`[tools]`
example source), `CHANGELOG.md`, `README.md`, `CLAUDE.md`, TUI help/footer.

- [ ] **Step 1:** Add a `commit_message` `[[tools.command]]` worked example to the
  generated config template (`gg config init`/`populate`), mirroring the Stage-1
  conflict block, using the Task-4 Claude command with `${GG_CONTEXT_FILE}` /
  `${GG_STAGED_DIFF}`. If a test asserts the generated template content, extend it.
  Run `go test ./internal/config/`.

- [ ] **Step 2:** Add the `ctrl+g` binding to the commit-popup help (`help.go`) AND
  the popup hint line (the advertise-in-help-and-footer convention).

- [ ] **Step 3:** Docs: `CHANGELOG.md` (always); `README.md` (the commit-popup
  generate key); `CLAUDE.md` (engine `GenerateMessage`/`CaptureRunner`/`Result.Captured`,
  exttool `ParseCaptureMessage` + `commit_message` templates, the capture-lane
  note in the `tui` row). No `internal/agentskill/using-gg.md` change (no CLI surface).

- [ ] **Step 4:** Run `./test.sh` (full) — expect green. Commit
  (`docs: external-tools stage 2 — config example, help, changelog, readme, CLAUDE.md`).

---

## Final

After Task 8: dispatch the whole-branch code review (`scripts/review-package
76f8c8b HEAD`), fix Critical/Important findings, run `./test.sh race`, then
`superpowers:finishing-a-development-branch` (the human merges). Live-verify
`ctrl+g` against real `claude` once before declaring done (the Stage-1 lesson).
