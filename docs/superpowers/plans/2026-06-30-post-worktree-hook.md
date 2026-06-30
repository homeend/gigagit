# Post-worktree-create hook — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run a user-configured shell script inside a newly created worktree (e.g. to copy gitignored files from the main checkout), wired into the engine op so the TUI and CLI both honor it.

**Architecture:** A new `PostCreateHook` config field (`[worktree]`, multi-line literal TOML) is read by each frontend and passed into the `CreateWorktree`/`CreateWorktreeForBranch` engine ops. The ops run it through a new fakeable `HookRunner` seam (`ShellHookRunner`: writes the script to a temp file, runs it via `$SHELL` with `cwd=new worktree`, stdin=/dev/null, streaming output as `GitLine`). A new 80%-wide multi-line TUI editor edits the script; a per-create toggle and a CLI `--no-hook` flag skip it.

**Tech Stack:** Go 1.26, `github.com/pelletier/go-toml/v2`, Bubble Tea / lipgloss, the engine `Operation`/`Event` contract.

## Global Constraints

- Module `github.com/homeend/gigagit`; Go 1.26.
- A git verb is one invocation; the hook is NOT a git verb — it runs through the new `HookRunner` seam, never `gitexec` (which is git-locked).
- Operations never block on a human: the hook is non-interactive (stdin = /dev/null).
- `internal/tui` and `internal/cli` never import `internal/git`; they reach git via `internal/domain` (archtest-guarded).
- Tests use a real `git` in a `t.TempDir()` (`newRepo` helper) or fakes; follow TDD.
- Run `./test.sh` (and `./test.sh race` before merge). gofmt + vet gate runs first — keep code gofmt-clean.
- Injected hook env (verbatim names): `GG_MAIN_WORKTREE`, `GG_WORKTREE_PATH`, `GG_BRANCH`, `GG_REPO`.
- Config stored as a TOML multi-line **literal** string (`'''…'''`); the writer must be delimiter-aware (a script line like `[ -d x ]` must not be parsed as a section header). Empty script removes the key.

---

### Task 1: Config field + overlay + settingDoc

**Files:**
- Modify: `internal/config/config.go:17-21` (struct), `:156-166` (overlay)
- Modify: `internal/config/template.go:24-27` (settingDocs)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.WorktreeConfig.PostCreateHook string` (toml `post_create_hook`).

- [ ] **Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

```go
func TestLoadPostCreateHookMultiline(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, ".gg.toml")
	body := "[worktree]\npost_create_hook = '''\ncp \"$GG_MAIN_WORKTREE/.env\" .\nmake setup\n'''\n"
	if err := os.WriteFile(repo, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(filepath.Join(dir, "none-global.toml"), repo)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := "cp \"$GG_MAIN_WORKTREE/.env\" .\nmake setup\n"
	if cfg.Worktree.PostCreateHook != want {
		t.Fatalf("PostCreateHook = %q, want %q", cfg.Worktree.PostCreateHook, want)
	}
}

func TestOverlayPostCreateHookRepoWins(t *testing.T) {
	dst := WorktreeConfig{PostCreateHook: "global-hook"}
	overlayWorktree(&dst, WorktreeConfig{PostCreateHook: "repo-hook"})
	if dst.PostCreateHook != "repo-hook" {
		t.Fatalf("overlay = %q, want repo-hook", dst.PostCreateHook)
	}
	overlayWorktree(&dst, WorktreeConfig{}) // empty = unset, must not clear
	if dst.PostCreateHook != "repo-hook" {
		t.Fatalf("empty src cleared hook: %q", dst.PostCreateHook)
	}
}
```

(Ensure `os`, `path/filepath` are imported in the test file.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run 'PostCreateHook' -v`
Expected: FAIL — `cfg.Worktree.PostCreateHook` undefined (compile error).

- [ ] **Step 3: Add the field**

In `internal/config/config.go`, extend `WorktreeConfig` (after `BranchTemplates`, line 20):

```go
type WorktreeConfig struct {
	PathTemplate          string   `toml:"path_template"`
	DefaultBranchTemplate string   `toml:"default_branch_template"`
	BranchTemplates       []string `toml:"branch_templates"`
	// PostCreateHook is a shell script run after a worktree is created (cwd =
	// the new worktree; env GG_MAIN_WORKTREE/GG_WORKTREE_PATH/GG_BRANCH/GG_REPO).
	// Stored as a multi-line TOML literal ('''…'''). Empty = disabled.
	PostCreateHook string `toml:"post_create_hook"`
}
```

Add the overlay block in `overlayWorktree` (after the `BranchTemplates` block, line 165):

```go
	if src.PostCreateHook != "" {
		dst.PostCreateHook = src.PostCreateHook
	}
```

- [ ] **Step 4: Add the settingDoc**

In `internal/config/template.go`, after line 27 (`branch_templates` entry):

```go
	{"worktree", "post_create_hook", nil, "shell script run after creating a worktree (cwd=new worktree; env GG_MAIN_WORKTREE/GG_WORKTREE_PATH/GG_BRANCH/GG_REPO); multi-line '''…''' literal; default: none"},
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/config/ -run 'PostCreateHook|TestSettingDocsCoverAllFields' -v`
Expected: PASS (including the coverage test, which now sees the new field documented).

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/template.go internal/config/config_test.go
git commit -m "feat(config): add [worktree] post_create_hook field"
```

---

### Task 2: Delimiter-aware multi-line config writer (+ scalar-writer safety)

**Files:**
- Modify: `internal/config/write.go` (add `SetWorktreePostCreateHook`, `setMultilineLiteral`, `opensMultiline`; patch `setScalarLine` to skip multiline blocks)
- Test: `internal/config/write_test.go`

**Interfaces:**
- Produces: `config.SetWorktreePostCreateHook(path, script string) error` — writes/removes `[worktree] post_create_hook` as a `'''…'''` block, preserving the rest of the file.

- [ ] **Step 1: Write the failing tests**

Add to `internal/config/write_test.go` (create the file if absent, `package config`, imports `os`, `path/filepath`, `strings`, `testing`):

```go
func TestSetWorktreePostCreateHookRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gg.toml")
	script := "cp \"$GG_MAIN_WORKTREE/.env\" .\nmake setup\n"
	if err := SetWorktreePostCreateHook(path, script); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(filepath.Join(t.TempDir(), "no-global.toml"), path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Worktree.PostCreateHook != script {
		t.Fatalf("round-trip = %q, want %q", cfg.Worktree.PostCreateHook, script)
	}
}

func TestSetWorktreePostCreateHookReplaceAndRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gg.toml")
	if err := SetWorktreePostCreateHook(path, "echo one\n"); err != nil {
		t.Fatal(err)
	}
	if err := SetWorktreePostCreateHook(path, "echo two\n"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Count(string(raw), "post_create_hook") != 1 {
		t.Fatalf("expected exactly one hook block, got:\n%s", raw)
	}
	if !strings.Contains(string(raw), "echo two") || strings.Contains(string(raw), "echo one") {
		t.Fatalf("replace failed:\n%s", raw)
	}
	if err := SetWorktreePostCreateHook(path, ""); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(path)
	if strings.Contains(string(raw), "post_create_hook") {
		t.Fatalf("empty script must remove key:\n%s", raw)
	}
}

func TestSetWorktreePostCreateHookIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gg.toml")
	script := "cp a b\nmake\n"
	if err := SetWorktreePostCreateHook(path, script); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	cfg, _ := Load(filepath.Join(t.TempDir(), "ng.toml"), path)
	if err := SetWorktreePostCreateHook(path, cfg.Worktree.PostCreateHook); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Fatalf("re-save not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// Regression: a hook whose script contains lines that look like TOML structure
// ([ … ], key = value, # comment) must not corrupt a subsequent scalar write.
func TestScalarWriteSurvivesHookBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gg.toml")
	script := "[ -d node_modules ] || npm ci\n# set up\nfoo = bar\n"
	if err := SetWorktreePostCreateHook(path, script); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if err := SetRefreshInterval(path, "branches", 30); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(filepath.Join(t.TempDir(), "ng.toml"), path)
	if err != nil {
		t.Fatalf("Load after scalar write: %v", err)
	}
	if cfg.Worktree.PostCreateHook != script {
		t.Fatalf("hook corrupted by scalar write: %q", cfg.Worktree.PostCreateHook)
	}
	if cfg.Refresh.Branches != 30 {
		t.Fatalf("branches = %d, want 30", cfg.Refresh.Branches)
	}
	_ = before
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run 'PostCreateHook|ScalarWriteSurvives' -v`
Expected: FAIL — `SetWorktreePostCreateHook` undefined.

- [ ] **Step 3: Implement the writer + scalar-writer safety**

In `internal/config/write.go`, add the public writer and helpers, and patch `setScalarLine`'s scan loop.

Add after `SetRefreshWatch` (line 51):

```go
// SetWorktreePostCreateHook persists `[worktree] post_create_hook = '''…'''` to
// the given config file (the repo .gg.toml) as a TOML multi-line LITERAL string,
// preserving comments and unrelated lines. A trailing newline in script is
// trimmed so re-saving a parsed value is idempotent; an empty script removes
// the key. Backs the Settings "Worktree post-create hook" editor.
func SetWorktreePostCreateHook(path, script string) error {
	return setMultilineLiteral(path, "worktree", "post_create_hook", script)
}
```

Add the helpers (anywhere below `setScalarLine`):

```go
// opensMultiline reports whether a trimmed line opens a multi-line TOML string
// ('''/""") that is NOT closed on the same line, returning the closing
// delimiter. Used so line-oriented writers skip a multi-line value's interior
// (a script line like "[ -d x ]" must not be mistaken for a section header).
func opensMultiline(trimmed string) (delim string, opens bool) {
	for _, d := range []string{"'''", `"""`} {
		if i := strings.Index(trimmed, "= "+d); i >= 0 {
			if !strings.Contains(trimmed[i+len("= "+d):], d) {
				return d, true
			}
		}
	}
	return "", false
}

// setMultilineLiteral sets `key = '''value'''` under [section] via a
// line-oriented, delimiter-aware edit. An empty value removes the key.
func setMultilineLiteral(path, section, key, value string) error {
	if path == "" {
		return fmt.Errorf("config: no config path; refusing to write")
	}
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	header := "[" + section + "]"

	var lines []string
	if len(raw) > 0 {
		lines = strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	}

	// Replacement block (empty value ⇒ delete the key). TrimRight so a parsed
	// value's trailing newline does not accumulate a blank line on re-save.
	var block []string
	if strings.TrimSpace(value) != "" {
		block = append([]string{key + " = '''"}, strings.Split(strings.TrimRight(value, "\n"), "\n")...)
		block = append(block, "'''")
	}

	var (
		inSection      bool
		headerAt       = -1
		startAt        = -1
		endAt          = -1
		skipUntil      string
	)
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if skipUntil != "" {
			if strings.Contains(trimmed, skipUntil) {
				skipUntil = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inSection = trimmed == header
			if inSection {
				headerAt = i
			}
			continue
		}
		if inSection && startAt == -1 && lineAssignsKey(trimmed, key) {
			startAt = i
			if d, ok := opensMultiline(trimmed); ok {
				endAt = len(lines) - 1
				for j := i + 1; j < len(lines); j++ {
					if strings.Contains(strings.TrimSpace(lines[j]), d) {
						endAt = j
						break
					}
				}
			} else {
				endAt = i // single-line assignment
			}
			continue
		}
		if d, ok := opensMultiline(trimmed); ok {
			skipUntil = d
		}
	}

	switch {
	case startAt >= 0:
		tail := append([]string{}, lines[endAt+1:]...)
		lines = append(lines[:startAt], append(block, tail...)...)
	case headerAt >= 0:
		if len(block) > 0 {
			lines = append(lines[:headerAt+1], append(block, lines[headerAt+1:]...)...)
		}
	default:
		if len(block) > 0 {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, header)
			lines = append(lines, block...)
		}
	}

	if len(lines) == 0 {
		return atomicWriteFile(path, []byte(""))
	}
	return atomicWriteFile(path, []byte(strings.Join(lines, "\n")+"\n"))
}
```

Patch `setScalarLine` (lines 77-91) to skip multiline interiors. Replace the loop with:

```go
	var skipUntil string
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if skipUntil != "" {
			if strings.Contains(trimmed, skipUntil) {
				skipUntil = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inSection = trimmed == header
			if inSection {
				headerAt = i
			}
			continue
		}
		if inSection && lineAssignsKey(trimmed, key) {
			lines[i] = want
			replacedAt = i
			break
		}
		if d, ok := opensMultiline(trimmed); ok {
			skipUntil = d
		}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run 'PostCreateHook|ScalarWriteSurvives|TestSet' -v`
Expected: PASS (existing `SetRefreshInterval`/`SetRefreshWatch` tests still green).

- [ ] **Step 5: Commit**

```bash
git add internal/config/write.go internal/config/write_test.go
git commit -m "feat(config): delimiter-aware multi-line writer for post_create_hook"
```

---

### Task 3: Engine `HookRunner` seam + `ShellHookRunner`

**Files:**
- Create: `internal/engine/hook_runner.go`
- Modify: `internal/engine/operation.go:17-25` (add `HookRunner` field + nil-safe accessor)
- Test: `internal/engine/hook_runner_test.go`

**Interfaces:**
- Produces:
  ```go
  type HookSpec struct { Dir string; Env []string; Script string }
  type HookRunner interface {
      Run(ctx context.Context, spec HookSpec, onLine func(string)) (exitCode int, err error)
  }
  type ShellHookRunner struct{}            // default real impl
  func (d OpDeps) hookRunner() HookRunner  // nil ⇒ ShellHookRunner{}
  ```
  Contract: a non-zero script exit is reported via `exitCode`, NOT as `err`. `err` is non-nil only for setup failures (temp file, exec start). stdin is /dev/null; `ctx` cancellation kills the child. stdout+stderr stream merged, one `onLine` call per line.

- [ ] **Step 1: Write the failing test**

Create `internal/engine/hook_runner_test.go`:

```go
package engine

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestShellHookRunnerStreamsAndEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script")
	}
	dir := t.TempDir()
	var got []string
	code, err := ShellHookRunner{}.Run(context.Background(),
		HookSpec{
			Dir:    dir,
			Env:    append([]string{}, "GG_BRANCH=feat/x"),
			Script: "printf 'a\\nb\\n'\necho \"branch=$GG_BRANCH\"\npwd\n",
		},
		func(line string) { got = append(got, line) })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"a", "b", "branch=feat/x"} {
		if !contains(got, want) {
			t.Fatalf("missing %q in output:\n%s", want, joined)
		}
	}
}

func TestShellHookRunnerNonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script")
	}
	code, err := ShellHookRunner{}.Run(context.Background(),
		HookSpec{Dir: t.TempDir(), Script: "exit 3\n"},
		func(string) {})
	if err != nil {
		t.Fatalf("non-zero exit must not be a Run error: %v", err)
	}
	if code != 3 {
		t.Fatalf("exit = %d, want 3", code)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if strings.TrimRight(s, "\r") == want {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run ShellHookRunner -v`
Expected: FAIL — `ShellHookRunner`, `HookSpec` undefined.

- [ ] **Step 3: Implement the runner**

Create `internal/engine/hook_runner.go`:

```go
package engine

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"runtime"
)

// HookSpec is one hook invocation: a working directory, a full environment
// (caller already merged the inherited env + GG_* vars), and the script body.
type HookSpec struct {
	Dir    string
	Env    []string
	Script string
}

// HookRunner runs a post-create hook script. A non-zero script exit is returned
// via exitCode (not err); err is non-nil only for a setup/exec failure. The hook
// is non-interactive: stdin is the null device. ctx cancellation kills it.
type HookRunner interface {
	Run(ctx context.Context, spec HookSpec, onLine func(string)) (exitCode int, err error)
}

// ShellHookRunner runs the script via the user's $SHELL (POSIX) or cmd.exe
// (Windows) by writing it to a temp file and executing that file — uniform
// across platforms and free of arg-length / newline-quoting limits.
type ShellHookRunner struct{}

func (ShellHookRunner) Run(ctx context.Context, spec HookSpec, onLine func(string)) (int, error) {
	ext := ".sh"
	if runtime.GOOS == "windows" {
		ext = ".bat"
	}
	f, err := os.CreateTemp("", "gg-hook-*"+ext)
	if err != nil {
		return -1, err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.WriteString(spec.Script); err != nil {
		f.Close()
		return -1, err
	}
	if err := f.Close(); err != nil {
		return -1, err
	}

	shell, args := hookShellArgv(name)
	cmd := exec.CommandContext(ctx, shell, args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.Stdin = nil // nil ⇒ /dev/null: a prompting hook gets EOF, never hangs.
	lw := &hookLineWriter{onLine: onLine}
	cmd.Stdout = lw
	cmd.Stderr = lw

	err = cmd.Run()
	lw.flush()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), nil // script failure: report code, not err
		}
		return -1, err
	}
	return 0, nil
}

// hookShellArgv chooses the interpreter for the temp script file.
func hookShellArgv(path string) (string, []string) {
	if runtime.GOOS == "windows" {
		comspec := os.Getenv("COMSPEC")
		if comspec == "" {
			comspec = "cmd"
		}
		return comspec, []string{"/C", path}
	}
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	return sh, []string{path}
}

// hookLineWriter splits streamed bytes into lines, calling onLine per complete
// line; flush emits any trailing partial line.
type hookLineWriter struct {
	onLine func(string)
	buf    bytes.Buffer
}

func (w *hookLineWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	for {
		line, err := w.buf.ReadString('\n')
		if err != nil { // no full line yet; keep the remainder
			w.buf.Reset()
			w.buf.WriteString(line)
			break
		}
		w.onLine(line[:len(line)-1])
	}
	return len(p), nil
}

func (w *hookLineWriter) flush() {
	if rest := w.buf.String(); rest != "" {
		w.onLine(rest)
	}
}
```

Add the seam to `internal/engine/operation.go`. Extend `OpDeps` (after `Escalate`, line 24):

```go
	// HookRunner runs a post-create worktree hook. Nil ⇒ ShellHookRunner{}
	// (production default); engine tests inject a fake.
	HookRunner HookRunner
```

Add the nil-safe accessor near `escalate` (after line 33):

```go
// hookRunner is the nil-safe HookRunner (style of emit/escalate).
func (d OpDeps) hookRunner() HookRunner {
	if d.HookRunner == nil {
		return ShellHookRunner{}
	}
	return d.HookRunner
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/engine/ -run ShellHookRunner -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/hook_runner.go internal/engine/operation.go internal/engine/hook_runner_test.go
git commit -m "feat(engine): HookRunner seam + ShellHookRunner"
```

---

### Task 4: Run the hook from `CreateWorktree`

**Files:**
- Create: `internal/engine/post_create_hook.go`
- Modify: `internal/engine/create_worktree.go:14-18` (field), `:43-45` (run hook before Done)
- Test: `internal/engine/create_worktree_test.go`

**Interfaces:**
- Consumes: `OpDeps.hookRunner()`, `mainWorktreeRoot`, `HookSpec`.
- Produces: `engine.CreateWorktree.PostCreateHook string`; helper `runPostCreateHook(ctx, deps, worktreePath, branch, script string) (note string)` — emits `Progress`+`GitLine`s, returns a `Summary` suffix (e.g. ` (post-create hook failed: exit 1)`), empty on success/skip.

- [ ] **Step 1: Write the failing test**

Add to `internal/engine/create_worktree_test.go` (add imports `strings` if needed):

```go
type fakeHookRunner struct {
	called bool
	spec   HookSpec
	lines  []string
	code   int
	err    error
}

func (h *fakeHookRunner) Run(_ context.Context, spec HookSpec, onLine func(string)) (int, error) {
	h.called = true
	h.spec = spec
	for _, l := range h.lines {
		onLine(l)
	}
	return h.code, h.err
}

func hookEnv(spec HookSpec, key string) string {
	for _, kv := range spec.Env {
		if strings.HasPrefix(kv, key+"=") {
			return kv[len(key)+1:]
		}
	}
	return ""
}

func TestCreateWorktreeRunsHook(t *testing.T) {
	dir, repo := newRepo(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-hook")
	fh := &fakeHookRunner{lines: []string{"copied .env"}}
	ch := make(chan Event, 64)
	res, err := CreateWorktree{StartPoint: "main", Branch: "f/h", Path: wt, PostCreateHook: "echo hi"}.Run(
		context.Background(), OpDeps{Repo: repo, Events: ch, HookRunner: fh})
	close(ch)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !fh.called {
		t.Fatal("hook not called")
	}
	if fh.spec.Dir != res.Path {
		t.Fatalf("hook Dir = %q, want %q", fh.spec.Dir, res.Path)
	}
	if got := hookEnv(fh.spec, "GG_WORKTREE_PATH"); got != res.Path {
		t.Fatalf("GG_WORKTREE_PATH = %q, want %q", got, res.Path)
	}
	if got := hookEnv(fh.spec, "GG_BRANCH"); got != "f/h" {
		t.Fatalf("GG_BRANCH = %q, want f/h", got)
	}
	if hookEnv(fh.spec, "GG_MAIN_WORKTREE") == "" {
		t.Fatal("GG_MAIN_WORKTREE unset")
	}
	var sawLine bool
	for _, e := range drain(ch) {
		if g, ok := e.(GitLine); ok && g.Raw == "copied .env" {
			sawLine = true
		}
	}
	if !sawLine {
		t.Fatal("hook output not streamed as GitLine")
	}
}

func TestCreateWorktreeEmptyHookSkips(t *testing.T) {
	dir, repo := newRepo(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-nohook")
	fh := &fakeHookRunner{}
	_, err := CreateWorktree{StartPoint: "main", Branch: "f/n", Path: wt}.Run(
		context.Background(), OpDeps{Repo: repo, HookRunner: fh})
	if err != nil {
		t.Fatal(err)
	}
	if fh.called {
		t.Fatal("empty hook must not run")
	}
}

func TestCreateWorktreeHookFailureNonFatal(t *testing.T) {
	dir, repo := newRepo(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-failhook")
	fh := &fakeHookRunner{code: 1}
	res, err := CreateWorktree{StartPoint: "main", Branch: "f/f", Path: wt, PostCreateHook: "false"}.Run(
		context.Background(), OpDeps{Repo: repo, HookRunner: fh})
	if err != nil {
		t.Fatalf("hook failure must not fail the op: %v", err)
	}
	if !res.Changed {
		t.Fatal("worktree should still count as created")
	}
	if !strings.Contains(res.Summary, "exit 1") {
		t.Fatalf("Summary should note hook failure, got %q", res.Summary)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/engine/ -run 'CreateWorktreeRunsHook|EmptyHookSkips|HookFailureNonFatal' -v`
Expected: FAIL — `PostCreateHook` field undefined.

- [ ] **Step 3: Implement the helper**

Create `internal/engine/post_create_hook.go`:

```go
package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// runPostCreateHook runs the configured post-create hook in the new worktree.
// It is non-fatal: the worktree already exists, so a hook error/non-zero exit is
// surfaced (a GitLine plus a returned Summary suffix) but never fails the op.
// Returns "" on success or when no hook is configured.
func runPostCreateHook(ctx context.Context, deps OpDeps, worktreePath, branch, script string) string {
	if strings.TrimSpace(script) == "" {
		return ""
	}
	main, _ := mainWorktreeRoot(ctx, deps) // best-effort; "" if unavailable
	env := append(os.Environ(),
		"GG_MAIN_WORKTREE="+main,
		"GG_WORKTREE_PATH="+worktreePath,
		"GG_BRANCH="+branch,
		"GG_REPO="+filepath.Base(main),
	)
	deps.emit(ctx, Progress{Step: "running post-create hook", Detail: worktreePath})
	code, err := deps.hookRunner().Run(ctx, HookSpec{Dir: worktreePath, Env: env, Script: script},
		func(line string) { deps.emit(ctx, GitLine{Raw: line}) })
	switch {
	case err != nil:
		deps.emit(ctx, GitLine{Raw: "post-create hook error: " + err.Error()})
		return " (post-create hook error: " + err.Error() + ")"
	case code != 0:
		msg := fmt.Sprintf("post-create hook exited with code %d", code)
		deps.emit(ctx, GitLine{Raw: msg})
		return fmt.Sprintf(" (post-create hook failed: exit %d)", code)
	}
	return ""
}
```

Add the field to `CreateWorktree` (`internal/engine/create_worktree.go`, after `Path`, line 17):

```go
	Path           string
	PostCreateHook string // shell script run in the new worktree; "" = none
```

Run the hook before `Done` (replace lines 43-45):

```go
	note := runPostCreateHook(ctx, deps, abs, op.Branch, op.PostCreateHook)
	res := Result{Summary: "worktree created: " + abs + note, Changed: true, Path: abs}
	deps.emit(ctx, Done{Result: res})
	return res, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/engine/ -run 'CreateWorktree' -v`
Expected: PASS (existing CreateWorktree tests still green — empty `PostCreateHook` skips).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/post_create_hook.go internal/engine/create_worktree.go internal/engine/create_worktree_test.go
git commit -m "feat(engine): run post-create hook from CreateWorktree"
```

---

### Task 5: Run the hook from `CreateWorktreeForBranch`

**Files:**
- Modify: `internal/engine/create_worktree_for_branch.go:11-14` (field), `:60-62` (run hook before Done)
- Test: `internal/engine/create_worktree_for_branch_test.go`

**Interfaces:**
- Consumes: `runPostCreateHook` (Task 4), `fakeHookRunner` (Task 4, same package).
- Produces: `engine.CreateWorktreeForBranch.PostCreateHook string`.

- [ ] **Step 1: Write the failing test**

Add to `internal/engine/create_worktree_for_branch_test.go`:

```go
func TestCreateWorktreeForBranchRunsHook(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "hooked/b")
	wt := filepath.Join(filepath.Dir(dir), "wt-fb-hook")
	fh := &fakeHookRunner{lines: []string{"setup done"}}
	res, err := CreateWorktreeForBranch{Branch: "hooked/b", Path: wt, PostCreateHook: "echo hi"}.Run(
		context.Background(), OpDeps{Repo: repo, HookRunner: fh})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !fh.called || fh.spec.Dir != res.Path {
		t.Fatalf("hook not run in worktree: called=%v dir=%q want=%q", fh.called, fh.spec.Dir, res.Path)
	}
	if got := hookEnv(fh.spec, "GG_BRANCH"); got != "hooked/b" {
		t.Fatalf("GG_BRANCH = %q, want hooked/b", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run CreateWorktreeForBranchRunsHook -v`
Expected: FAIL — `PostCreateHook` field undefined.

- [ ] **Step 3: Implement**

Add the field to `CreateWorktreeForBranch` (after `Path`, line 13):

```go
	Path           string
	PostCreateHook string // shell script run in the new worktree; "" = none
```

Run the hook before `Done` (replace lines 60-62):

```go
	note := runPostCreateHook(ctx, deps, abs, op.Branch, op.PostCreateHook)
	res := Result{Summary: "worktree created: " + abs + note, Changed: true, Path: abs}
	deps.emit(ctx, Done{Result: res})
	return res, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/engine/ -v`
Expected: PASS (whole engine package).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/create_worktree_for_branch.go internal/engine/create_worktree_for_branch_test.go
git commit -m "feat(engine): run post-create hook from CreateWorktreeForBranch"
```

---

### Task 6: TUI multi-line hook editor popup

**Files:**
- Create: `internal/tui/hook_editor.go`
- Test: `internal/tui/hook_editor_test.go`

**Interfaces:**
- Consumes: `textfield` (`newTextField`, `Value`, `InsertNewline`, `Up`, `Down`, `View`, `HandleEditKey`), `pushLayer`/`popLayer`, `m.cfg.Worktree.PostCreateHook`, `m.repoConfigPath`, `config.SetWorktreePostCreateHook`, `m.overlayDims`, `overlayCenter`, `clipToHeight`, `modalStyle`.
- Produces: `hookEditorPopup` (a `layer`), `func (m Model) openHookEditor() Model`, `func (m Model) saveHook(script string) (Model, tea.Cmd)`.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/hook_editor_test.go`:

```go
package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
)

func typeRunes(m Model, p *hookEditorPopup, s string) Model {
	for _, r := range s {
		m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func TestHookEditorSeedsFromConfig(t *testing.T) {
	m := Model{}
	m.cfg.Worktree.PostCreateHook = "echo seeded"
	m = m.openHookEditor()
	p := layerOf[*hookEditorPopup](m)
	if p == nil {
		t.Fatal("editor not pushed")
	}
	if p.buf.Value() != "echo seeded" {
		t.Fatalf("seed = %q, want 'echo seeded'", p.buf.Value())
	}
}

func TestHookEditorSavesToRepoConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gg.toml")
	m := Model{}
	m.repoConfigPath = path
	m = m.openHookEditor()
	p := layerOf[*hookEditorPopup](m)
	m = typeRunes(m, p, "echo one")
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter}) // newline, not submit
	m = typeRunes(m, p, "echo two")
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})

	if m.cfg.Worktree.PostCreateHook != "echo one\necho two" {
		t.Fatalf("in-memory cfg = %q", m.cfg.Worktree.PostCreateHook)
	}
	cfg, err := config.Load(filepath.Join(t.TempDir(), "ng.toml"), path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Worktree.PostCreateHook != "echo one\necho two\n" {
		t.Fatalf("persisted = %q", cfg.Worktree.PostCreateHook)
	}
	if layerOf[*hookEditorPopup](m) != nil {
		t.Fatal("editor should close after save")
	}
	_ = os.Remove
}

func TestHookEditorEscDoesNotSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gg.toml")
	m := Model{}
	m.repoConfigPath = path
	m = m.openHookEditor()
	p := layerOf[*hookEditorPopup](m)
	m = typeRunes(m, p, "echo nope")
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("esc must not write config")
	}
	if layerOf[*hookEditorPopup](m) != nil {
		t.Fatal("editor should close on esc")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run HookEditor -v`
Expected: FAIL — `hookEditorPopup`, `openHookEditor` undefined.

- [ ] **Step 3: Implement the editor**

Create `internal/tui/hook_editor.go`:

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
)

// hookEditorPopup is the wide multi-line editor for the [worktree]
// post_create_hook script (Settings → "Worktree post-create hook"). Enter
// inserts a newline; Ctrl+S saves to the repo .gg.toml; Esc cancels.
type hookEditorPopup struct {
	buf textfield
}

// openHookEditor pushes the hook editor, seeded with the current script.
func (m Model) openHookEditor() Model {
	return m.pushLayer(&hookEditorPopup{buf: newTextField(m.cfg.Worktree.PostCreateHook)})
}

func (p *hookEditorPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyCtrlS:
		return m.saveHook(p.buf.Value())
	case tea.KeyEnter:
		p.buf.InsertNewline()
	case tea.KeyUp:
		p.buf.Up()
	case tea.KeyDown:
		p.buf.Down()
	default:
		p.buf.HandleEditKey(msg)
	}
	return m, nil
}

// saveHook persists the script to the repo .gg.toml, updates in-memory config,
// and closes the editor (mirrors saveRefreshInterval's surface behavior).
func (m Model) saveHook(script string) (Model, tea.Cmd) {
	m.cfg.Worktree.PostCreateHook = script
	if m.repoConfigPath == "" {
		m.statusMsg = "hook set (not saved: no repo config path)"
		return m.popLayer(), nil
	}
	if err := config.SetWorktreePostCreateHook(m.repoConfigPath, script); err != nil {
		m.statusMsg = "hook set but not saved: " + err.Error()
	} else {
		m.statusMsg = "post-create hook saved"
	}
	return m.popLayer(), nil
}

func (p *hookEditorPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m, w, h), w, h)
}

func (p *hookEditorPopup) box(m Model, w, h int) string {
	boxW := w * 8 / 10
	if boxW < 20 {
		boxW = w
	}
	// Visible script rows: leave room for title + blank + help (≈5 lines).
	rows := h - 6
	if rows < 3 {
		rows = 3
	}

	lines := strings.Split(p.buf.View(true), "\n")
	cur := strings.Count(string(p.buf.runes[:p.buf.cursor]), "\n") // cursor's line
	top := 0
	if cur >= rows {
		top = cur - rows + 1
	}
	end := top + rows
	if end > len(lines) {
		end = len(lines)
	}

	var b strings.Builder
	b.WriteString("Worktree post-create hook (runs in the new worktree)\n")
	b.WriteString("env: GG_MAIN_WORKTREE  GG_WORKTREE_PATH  GG_BRANCH  GG_REPO\n\n")
	b.WriteString(strings.Join(lines[top:end], "\n"))
	if end-top < rows {
		b.WriteString(strings.Repeat("\n", rows-(end-top)))
	}
	b.WriteString("\n\n[type] edit  [enter] newline  [ctrl+s] save  [esc] cancel")
	return modalStyle.Width(boxW).Render(b.String()) + "\n"
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run HookEditor -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/hook_editor.go internal/tui/hook_editor_test.go
git commit -m "feat(tui): wide multi-line post-create-hook editor"
```

---

### Task 7: Wire the editor into the Settings menu

**Files:**
- Modify: `internal/tui/settings_popup.go:37-47` (menu const + slice), `:238-265` (Enter dispatch)
- Test: `internal/tui/settings_popup_test.go` (or the existing settings test file)

**Interfaces:**
- Consumes: `openHookEditor` (Task 6).
- Produces: a `settingsMenuHook` entry that opens the editor.

- [ ] **Step 1: Write the failing test**

Add to the settings test file (e.g. `internal/tui/settings_popup_test.go`; create if absent with `package tui` + imports `testing`, `tea "github.com/charmbracelet/bubbletea"`):

```go
func TestSettingsOpensHookEditor(t *testing.T) {
	m := m0() // existing zero-Model helper; if none, use Model{}
	m = m.openSettings()
	sp := layerOf[*settingsPopup](m)
	if sp == nil {
		t.Fatal("settings not open")
	}
	// Move selection to the hook entry.
	for i, name := range settingsMenu {
		if name == settingsMenuHook {
			sp.menuSel = i
		}
	}
	m, _ = sp.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if layerOf[*hookEditorPopup](m) == nil {
		t.Fatal("Enter on hook entry should open the editor")
	}
}
```

(If no `m0()` helper exists in the package, replace `m0()` with `Model{}`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run SettingsOpensHookEditor -v`
Expected: FAIL — `settingsMenuHook` undefined.

- [ ] **Step 3: Add the menu entry + dispatch**

In `internal/tui/settings_popup.go`, add the const (after `settingsMenuRates`, line 44):

```go
	settingsMenuHook        = "Worktree post-create hook"
```

Add it to the `settingsMenu` slice (line 47) — place before `settingsMenuOpLog` so worktree-related settings group near the top:

```go
var settingsMenu = []string{settingsMenuAgents, settingsMenuIdentity, settingsMenuPrefixes, settingsMenuHook, settingsMenuOpLog, settingsMenuErrors, settingsMenuAutoRefresh, settingsMenuRemoteTags, settingsMenuRates}
```

Add the Enter case (in the switch at line 240, e.g. after the `settingsMenuPrefixes` case):

```go
			case settingsMenuHook:
				return m.openHookEditor(), nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'Settings' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/settings_popup.go internal/tui/settings_popup_test.go
git commit -m "feat(tui): Settings entry to edit the worktree post-create hook"
```

---

### Task 8: Per-create skip toggle in the worktree popup

**Files:**
- Modify: `internal/tui/worktree_popup.go:28-56` (struct field), the `openWorktreePopup` initializer (set `runHook: true`), `:267-319` (box hint), the stAction key switch, `:324-350` (`startCreateFromPopup`/`createOp`)
- Test: `internal/tui/worktree_popup_test.go`

**Interfaces:**
- Consumes: `engine.CreateWorktree.PostCreateHook`, `engine.CreateWorktreeForBranch.PostCreateHook` (Tasks 4-5).
- Produces: `worktreePopup.runHook bool`; `createOp(hook string)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/worktree_popup_test.go`:

```go
func TestWorktreeCreateOpCarriesHookWhenEnabled(t *testing.T) {
	p := &worktreePopup{previewBranch: "b/x", previewPath: "/tmp/x", runHook: true}
	op := p.createOp("echo hi")
	cw, ok := op.(engine.CreateWorktree)
	if !ok {
		t.Fatalf("op type = %T", op)
	}
	if cw.PostCreateHook != "echo hi" {
		t.Fatalf("PostCreateHook = %q, want 'echo hi'", cw.PostCreateHook)
	}
}

func TestWorktreeCreateOpOmitsHookWhenDisabled(t *testing.T) {
	p := &worktreePopup{previewBranch: "b/x", previewPath: "/tmp/x", existing: true, runHook: false}
	op := p.createOp("") // startCreateFromPopup passes "" when runHook is false
	cwb := op.(engine.CreateWorktreeForBranch)
	if cwb.PostCreateHook != "" {
		t.Fatalf("PostCreateHook = %q, want empty", cwb.PostCreateHook)
	}
}

func TestWorktreeHKeyTogglesHook(t *testing.T) {
	m := Model{}
	m.cfg.Worktree.PostCreateHook = "echo hi"
	p := &worktreePopup{state: stAction, runHook: true}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if p.runHook {
		t.Fatal("h should toggle runHook off")
	}
}
```

(Ensure the test file imports `engine` and `tea`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'WorktreeCreateOp|WorktreeHKey' -v`
Expected: FAIL — `runHook` field / `createOp(string)` signature mismatch.

- [ ] **Step 3: Implement**

Add the field to `worktreePopup` (after `branchOverride`, near line 51):

```go
	runHook bool // run the configured post-create hook on create (default true)
```

In `openWorktreePopup` (where the `&worktreePopup{...}` literal is built, ~line 130), set `runHook: true,`.

Change `createOp` to accept the hook (`internal/tui/worktree_popup.go:341-350`):

```go
func (p *worktreePopup) createOp(hook string) engine.Operation {
	if p.existing {
		return engine.CreateWorktreeForBranch{Branch: p.previewBranch, Path: p.previewPath, PostCreateHook: hook}
	}
	return engine.CreateWorktree{
		StartPoint:     p.startPoint,
		Branch:         p.previewBranch,
		Path:           p.previewPath,
		PostCreateHook: hook,
	}
}
```

Update the call in `startCreateFromPopup` (replace line 336):

```go
	hook := ""
	if p.runHook {
		hook = m.cfg.Worktree.PostCreateHook
	}
	return m.startOp(p.createOp(hook))
```

Add the `h` key in the stAction `default` switch (in `update`, alongside the `"e"`/`"p"` cases):

```go
		case "h":
			if m.cfg.Worktree.PostCreateHook != "" {
				p.runHook = !p.runHook
			}
			return m, nil
```

Add the hint + toggle line in `box` (in the `default` (stAction) case, before writing the key hints, ~line 311). Insert just before the `switch p.state` block's `default` hint write:

```go
	if p.state == stAction && m.cfg.Worktree.PostCreateHook != "" {
		mark := "[x]"
		if !p.runHook {
			mark = "[ ]"
		}
		b.WriteString(mark + " run post-create hook  ([h] toggle)\n")
	}
```

Then in the `default` hint string, append ` [h] hook`:

```go
		if p.existing {
			b.WriteString("[w] create  [W] create & switch  [h] hook  [esc] cancel")
		} else {
			b.WriteString("[w] create  [W] create & switch  [e] edit name  [p] use a prefix  [h] hook  [esc] cancel")
		}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run Worktree -v`
Expected: PASS (existing worktree-popup tests still green; any caller of `createOp()` now passes a hook arg — only `startCreateFromPopup` calls it).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/worktree_popup.go internal/tui/worktree_popup_test.go
git commit -m "feat(tui): per-create post-create-hook skip toggle (h)"
```

---

### Task 9: CLI `--no-hook` flag

**Files:**
- Modify: `internal/cli/worktree.go:58-168` (`cmdWorktreeAdd`)
- Test: `internal/cli/worktree_test.go`

**Interfaces:**
- Consumes: `cfg.Worktree.PostCreateHook`, `engine.CreateWorktree.PostCreateHook`, `engine.CreateWorktreeForBranch.PostCreateHook`.
- Produces: `--no-hook` flag; the op carries the hook unless `--no-hook`.

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/worktree_test.go` (follow the file's existing harness for invoking `cmdWorktreeAdd`; assert the created worktree contains a file the hook wrote). Minimal behavioral test:

```go
func TestWorktreeAddRunsConfiguredHook(t *testing.T) {
	repoDir, svc := newCLIServiceRepo(t) // existing helper that yields a *domain.Service on a real repo
	// Configure a hook that drops a marker file into the new worktree.
	cfgPath := filepath.Join(repoDir, ".gg.toml")
	if err := config.SetWorktreePostCreateHook(cfgPath, "touch hook-ran\n"); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	code := cmdWorktreeAdd(svc, []string{"../wt-cli-hook"}, strings.NewReader(""), &out, &errBuf, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errBuf.String())
	}
	marker := filepath.Join(filepath.Dir(repoDir), "wt-cli-hook", "hook-ran")
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("hook did not run: %v", err)
	}
}

func TestWorktreeAddNoHookFlag(t *testing.T) {
	repoDir, svc := newCLIServiceRepo(t)
	cfgPath := filepath.Join(repoDir, ".gg.toml")
	if err := config.SetWorktreePostCreateHook(cfgPath, "touch hook-ran\n"); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	code := cmdWorktreeAdd(svc, []string{"--no-hook", "../wt-cli-nohook"}, strings.NewReader(""), &out, &errBuf, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errBuf.String())
	}
	marker := filepath.Join(filepath.Dir(repoDir), "wt-cli-nohook", "hook-ran")
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("--no-hook must skip the hook")
	}
}
```

> Note: if `newCLIServiceRepo` does not exist, reuse whatever real-repo `*domain.Service` helper the CLI tests already use (grep `cmdWorktreeAdd(` in `internal/cli/*_test.go`); adapt the setup lines accordingly. The behavioral assertions stay the same.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'WorktreeAddRunsConfiguredHook|NoHookFlag' -v`
Expected: FAIL — `--no-hook` not defined / hook not wired.

- [ ] **Step 3: Implement**

In `cmdWorktreeAdd` (`internal/cli/worktree.go`), add the flag (after `forBranch`, line 62):

```go
	noHook := fs.Bool("no-hook", false, "skip the configured [worktree] post_create_hook")
```

Compute the hook from config and set it on the op (replace the op-construction block, lines 160-163):

```go
	hook := cfg.Worktree.PostCreateHook
	if *noHook {
		hook = ""
	}
	var op engine.Operation = engine.CreateWorktree{StartPoint: startPoint, Branch: branch, Path: path, PostCreateHook: hook}
	if *forBranch != "" {
		op = engine.CreateWorktreeForBranch{Branch: branch, Path: path, PostCreateHook: hook}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run Worktree -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/worktree.go internal/cli/worktree_test.go
git commit -m "feat(cli): run [worktree] post_create_hook on worktree add (+ --no-hook)"
```

---

### Task 10: Docs + agent skill

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`, `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go` (Version)

- [ ] **Step 1: CHANGELOG**

Add an entry under the top/unreleased section of `CHANGELOG.md`:

```markdown
- **Post-worktree-create hook**: configure a per-repo shell script (`[worktree] post_create_hook`) that runs inside a newly created worktree — e.g. to copy gitignored files (`.env`, local config) from the main checkout. Runs for both the TUI and `gg worktree add`. Edit it in Settings (`,` → "Worktree post-create hook", a wide multi-line editor); skip it per-create with `[h]` in the create-worktree popup or `--no-hook` on the CLI. The script runs with `cwd` = the new worktree and env `GG_MAIN_WORKTREE` / `GG_WORKTREE_PATH` / `GG_BRANCH` / `GG_REPO`; output streams into the busy log, and a hook failure is reported without rolling back the worktree.
```

- [ ] **Step 2: README**

Add a short subsection (near the worktree / config docs) describing `[worktree] post_create_hook`, the env vars, the Settings editor, and `--no-hook`. Include the example:

````markdown
### Post-worktree hook

After `gg` creates a worktree it can run a per-repo shell script — handy for
copying gitignored files the new worktree won't have. Set it in `.gg.toml`:

```toml
[worktree]
post_create_hook = '''
cp "$GG_MAIN_WORKTREE/.env" .
make setup
'''
```

Runs with `cwd` = the new worktree. Env: `GG_MAIN_WORKTREE` (the main checkout),
`GG_WORKTREE_PATH`, `GG_BRANCH`, `GG_REPO`. Edit it from Settings (`,`), skip it
per-create with `h` in the create popup or `gg worktree add --no-hook`.
````

- [ ] **Step 3: CLAUDE.md**

- In the `engine` package-map row, note the `HookRunner` seam (`OpDeps.HookRunner`/`ShellHookRunner`) and that `CreateWorktree`/`CreateWorktreeForBranch` carry a `PostCreateHook` run non-interactively after `AddWorktree` (non-fatal; streams `GitLine`; env `GG_MAIN_WORKTREE`/`GG_WORKTREE_PATH`/`GG_BRANCH`/`GG_REPO`).
- In the `config` row, add `post_create_hook` to `[worktree]` and `SetWorktreePostCreateHook` to the list of line-edit writers (note it is the first multi-line writer; delimiter-aware so scalar writers stay safe).
- In the `tui` row, note the `hookEditorPopup` (Settings → "Worktree post-create hook") and the create-popup `h` skip toggle.

- [ ] **Step 4: using-gg.md + version bump**

- In `internal/agentskill/using-gg.md`, document `gg worktree add --no-hook` and the `[worktree] post_create_hook` behavior (one short paragraph).
- Bump `agentskill.Version` in `internal/agentskill/agentskill.go` (increment the version marker).

- [ ] **Step 5: Verify build + full suite, then commit**

Run: `go build ./cmd/gg && ./test.sh`
Expected: build OK; vet/gofmt/unit/e2e all pass.

```bash
git add CHANGELOG.md README.md CLAUDE.md internal/agentskill/using-gg.md internal/agentskill/agentskill.go
git commit -m "docs: post-worktree-create hook (CHANGELOG/README/CLAUDE/using-gg + version bump)"
```

---

## Self-Review

**Spec coverage:**
- Config field + multi-line literal storage → Tasks 1-2. ✓
- Engine-located, frontend-agnostic execution → Tasks 3-5. ✓
- `$SHELL`, cwd=new worktree, GG_* env (incl. `GG_MAIN_WORKTREE` for the copy source) → Tasks 3-4. ✓
- Non-interactive (stdin=/dev/null), ctx-cancel → Task 3. ✓
- Output streams as GitLine; hook failure non-fatal + noted in Summary → Tasks 4-5. ✓
- 80%-wide multi-line editor, Enter=newline, Ctrl+S save, Esc cancel → Task 6. ✓
- Settings entry → Task 7. ✓
- Per-create skip toggle → Task 8. ✓
- CLI `--no-hook` → Task 9. ✓
- Docs + agent skill → Task 10. ✓
- Advisor hazards: delimiter-aware writer + scalar-writer safety + corruption/idempotency tests (Task 2); null stdin + ctx-cancel + temp-file execution (Task 3). ✓

**Type consistency:** `createOp(hook string)` is defined in Task 8 and its only caller (`startCreateFromPopup`) is updated in the same task. `runPostCreateHook(...) string` defined in Task 4, reused verbatim in Task 5. `fakeHookRunner`/`hookEnv` defined in Task 4, reused in Task 5 (same package). `HookSpec`/`HookRunner`/`ShellHookRunner`/`hookRunner()` defined in Task 3, consumed by Tasks 4-5. `SetWorktreePostCreateHook` defined in Task 2, consumed by Tasks 6 and 9.

**Notes for the executor:**
- `mainWorktreeRoot` and `filepath.Base(main)` give `GG_REPO` without importing `internal/worktree` into `internal/engine` (avoids a new dependency edge).
- The op holds the `TreeWrite` gate for the hook's full duration (a long `npm install` blocks other repo ops on the same common dir). This is intended; `exec.CommandContext` ensures a cancelled op kills the child.
- Very long single lines in the editor may wrap (best-effort horizontal handling); acceptable for v1.
