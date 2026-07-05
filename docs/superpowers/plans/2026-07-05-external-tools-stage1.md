# External Tools Stage 1 (infrastructure + conflict resolution) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run user-configurable external tools/AI agents (Claude Code, Junie, Meld) on merge/rebase/cherry-pick/revert conflicts from the gg TUI, with a detect wizard that generates default commands into the global config.

**Architecture:** A new leaf package `internal/exttool` holds the hardcoded tool catalog + PATH detection. `[tools]` config blocks (`[[tools.command]]`) hold the executable commands (generated or hand-written). A command-context token resolver joins `internal/template`. The conflict process (`x` window) gains process-owned sub-states: tool picker → `<user:…>` fill → first-run approval (remembered via `promptstate`) → terminal handover (`tea.ExecProcess` running a temp script via `$SHELL`/`cmd`) → optional mark-resolved offer. `domain.ConflictFileVersions` materializes the per-file `LOCAL/BASE/REMOTE` quartet from index stages.

**Tech Stack:** Go 1.26, Bubble Tea, go-toml/v2, real-git tests in `t.TempDir()`.

**Spec:** `docs/superpowers/specs/2026-07-05-external-tools-design.md` (approved). Read it once before Task 1 if anything below seems ambiguous — the spec governs.

## Global Constraints

- Layering (archtest-guarded): `internal/tui`/`internal/cli` never import `internal/git`; `internal/exttool` is a **leaf** (imports stdlib only) importable by tui like `gitconfdocs`; register it in `internal/archtest` wherever the DAG test lists leaves.
- The conflict process preempts the layer stack for keys (`internal/tui/model.go:884` routes to `m.proc` before `m.topLayer()`), so ALL new conflict-tool UI is **process-owned sub-state**, never a pushed layer.
- Config shape (exact): `[[tools.command]]` blocks with keys `category` (`conflict|commit_message|review`), `name`, `mode` (`terminal|capture`), `per_file`, `when_op` (`""|merge|rebase|cherry-pick|revert`), `command` (multi-line `'''…'''` literal).
- Overlay rule for tool commands (deliberate exception to zero-is-unset): **concatenate global + repo; repo wins on (category, name) collision**.
- Stage 1 executes ONLY `mode = "terminal"`. A `capture` block (and any invalid block) is **inert**: skipped with one `observ.NoteFailure` per session per block, never a startup error.
- First-run approval: remembered per repo (`GitCommonDir` when known, else current worktree path) by `sha256(command template text)` truncated to 16 hex chars; any text change re-prompts. Stored via `promptstate`.
- Token quoting is per token kind: path tokens (`<repo> <file> <local> <base> <remote> <merged>`) shell-quoted; prose tokens (`<op> <source> <target> <conflicted-files> <user:…>`) raw. `<bin>` is generation-time only; the runtime resolver rejects it with a pointed error.
- The sequencer boundary: generated agent commands must never run `git commit` / `--continue`; continuing stays in gg (`engine.ContinueOp`). Catalog command texts below are copied **verbatim** from the spec.
- Tests: real `git` in `t.TempDir()` (see `internal/domain/compare_test.go newRealRepo`, `internal/domain/conflict_test.go mergeConflictDir/svcAt`) or pure fakes. TDD: failing test first. `gofmt -l` clean, `go vet ./...` clean after every task.
- Commit after every task with a `feat(scope):`/`test(scope):` message. Work happens in the worktree `/mnt/t/others/gigagit/.claude/worktrees/external-tools` on branch `feat/external-tools` — verify with `git branch --show-current` before the first commit.

## File Structure

| File | Responsibility |
|---|---|
| `internal/exttool/exttool.go` (new) | Catalog types, `Builtins()`, `Detect()`, `GenerateCommand()` |
| `internal/exttool/exttool_test.go` (new) | Detection with fake LookPath/Stat; catalog invariants |
| `internal/exttool/tokens_test.go` (new, Task 2) | Every builtin template's tokens validate |
| `internal/template/command.go` (new) | `CmdCtx`, `ResolveCommand`, `ValidateCommandTokens`, quoting |
| `internal/template/command_test.go` (new) | Golden resolution/quoting/validation tests |
| `internal/config/tools.go` (new) | `ToolCommand`/`ToolsConfig` types, overlay, structural validation, `AppendToolCommands` writer |
| `internal/config/tools_test.go` (new) | Parse/overlay/writer/validation tests |
| `internal/config/config.go` (modify) | `Config.Tools` field + overlay call |
| `internal/config/template.go` (modify) | `[tools]` settingDocs entry + section loop |
| `internal/config/template_test.go` (modify) | Coverage check for the `tools` section |
| `internal/promptstate/store.go` + `file_store.go` (modify) | Approved-tool-command records |
| `internal/domain/conflict_versions.go` (new) | `ConflictFileVersions` quartet query |
| `internal/domain/conflict_versions_test.go` (new) | Real-repo quartet tests |
| `internal/tui/tools.go` (new) | Validated command accessor + conflict choice filtering + env/hash helpers |
| `internal/tui/tools_test.go` (new) | Accessor/gating/inert-note tests |
| `internal/tui/tool_run.go` (new) | Script-file execution, `toolFinishedMsg`, quartet cmd |
| `internal/tui/conflict_process.go` (modify) | `t` key + tool sub-states (pick/fill/approve/mark) |
| `internal/tui/conflict_tools_test.go` (new) | Process-state walk tests |
| `internal/tui/settings_popup.go` (modify) | "External tools" menu row + wizard view |
| `internal/tui/settings_tools_test.go` (new) | Wizard rows/apply tests |
| `internal/tui/model.go` (modify) | `toolNoted` field, `toolFinishedMsg` handler |
| `internal/archtest/import_guard_test.go` (modify) | `exttool` as DAG leaf |
| `CHANGELOG.md`, `README.md`, `CLAUDE.md` (modify) | Docs (Task 9) |

---

### Task 1: `internal/exttool` — catalog + detection

**Files:**
- Create: `internal/exttool/exttool.go`
- Test: `internal/exttool/exttool_test.go`

**Interfaces:**
- Consumes: nothing (leaf package, stdlib only).
- Produces (later tasks rely on these exact names):
  - `type Category string`; consts `CatConflict Category = "conflict"`, `CatCommitMessage Category = "commit_message"`, `CatReview Category = "review"`
  - `type Mode string`; consts `ModeTerminal Mode = "terminal"`, `ModeCapture Mode = "capture"`
  - `type CommandTemplate struct { Category Category; Name string; Mode Mode; PerFile bool; WhenOp string; Command string }`
  - `type Tool struct { ID, Label string; Bins []string; ExtraProbes []string; Commands []CommandTemplate }`
  - `func Builtins() []Tool`
  - `type Detection struct { Tool Tool; Bin string }`
  - `func Detect(look func(string) (string, error), stat func(string) (os.FileInfo, error)) []Detection`
  - `func GenerateCommand(tmpl CommandTemplate, bin string) string`

- [ ] **Step 1: Write the failing test**

`internal/exttool/exttool_test.go`:

```go
package exttool

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// fakeLook simulates exec.LookPath over a fixed set of installed binaries.
func fakeLook(installed map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		if p, ok := installed[name]; ok {
			return p, nil
		}
		return "", errors.New("not found")
	}
}

// fakeStat simulates os.Stat over a fixed set of existing paths.
func fakeStat(existing map[string]bool) func(string) (os.FileInfo, error) {
	return func(path string) (os.FileInfo, error) {
		if existing[path] {
			return nil, nil // callers only check the error
		}
		return nil, os.ErrNotExist
	}
}

func TestDetectFindsBinsOnPath(t *testing.T) {
	dets := Detect(fakeLook(map[string]string{"claude": "/usr/bin/claude", "meld": "/usr/bin/meld"}), fakeStat(nil))
	got := map[string]string{}
	for _, d := range dets {
		got[d.Tool.ID] = d.Bin
	}
	if got["claude"] != "claude" {
		t.Errorf("claude Bin = %q, want bare name %q (PATH hit)", got["claude"], "claude")
	}
	if got["meld"] != "meld" {
		t.Errorf("meld Bin = %q, want %q", got["meld"], "meld")
	}
	if _, ok := got["junie"]; ok {
		t.Errorf("junie detected but not installed")
	}
}

func TestDetectExtraProbeYieldsAbsolutePath(t *testing.T) {
	// Meld's Windows install dir is off PATH; an ExtraProbes hit must return
	// the absolute path so the generated command can run it.
	var meld Tool
	for _, tl := range Builtins() {
		if tl.ID == "meld" {
			meld = tl
		}
	}
	if len(meld.ExtraProbes) == 0 {
		t.Fatal("meld has no ExtraProbes; expected the Windows install path")
	}
	probe := meld.ExtraProbes[0]
	dets := Detect(fakeLook(nil), fakeStat(map[string]bool{probe: true}))
	if len(dets) != 1 || dets[0].Tool.ID != "meld" || dets[0].Bin != probe {
		t.Fatalf("dets = %+v, want one meld detection with Bin=%q", dets, probe)
	}
}

func TestBuiltinsCatalogInvariants(t *testing.T) {
	seen := map[string]bool{}
	for _, tl := range Builtins() {
		if tl.ID == "" || tl.Label == "" || len(tl.Bins) == 0 {
			t.Errorf("tool %+v: ID/Label/Bins must be set", tl)
		}
		for _, ct := range tl.Commands {
			switch ct.Category {
			case CatConflict, CatCommitMessage, CatReview:
			default:
				t.Errorf("%s/%s: bad category %q", tl.ID, ct.Name, ct.Category)
			}
			switch ct.Mode {
			case ModeTerminal, ModeCapture:
			default:
				t.Errorf("%s/%s: bad mode %q", tl.ID, ct.Name, ct.Mode)
			}
			switch ct.WhenOp {
			case "", "merge", "rebase", "cherry-pick", "revert":
			default:
				t.Errorf("%s/%s: bad when_op %q", tl.ID, ct.Name, ct.WhenOp)
			}
			if ct.PerFile && ct.Category != CatConflict {
				t.Errorf("%s/%s: per_file outside conflict", tl.ID, ct.Name)
			}
			if !strings.Contains(ct.Command, "<bin>") {
				t.Errorf("%s/%s: command must start from <bin>", tl.ID, ct.Name)
			}
			key := string(ct.Category) + "\x00" + ct.Name
			if seen[key] {
				t.Errorf("duplicate (category,name): %s/%s", ct.Category, ct.Name)
			}
			seen[key] = true
		}
	}
}

func TestStage1CatalogIsConflictOnly(t *testing.T) {
	for _, tl := range Builtins() {
		for _, ct := range tl.Commands {
			if ct.Category != CatConflict {
				t.Errorf("stage 1 ships conflict templates only; found %s/%s", ct.Category, ct.Name)
			}
		}
	}
}

func TestGenerateCommand(t *testing.T) {
	ct := CommandTemplate{Command: "<bin> --auto-merge <local>"}
	if got := GenerateCommand(ct, "meld"); got != "meld --auto-merge <local>" {
		t.Errorf("bare bin: got %q", got)
	}
	if got := GenerateCommand(ct, `C:\Program Files\Meld\Meld.exe`); got != `"C:\Program Files\Meld\Meld.exe" --auto-merge <local>` {
		t.Errorf("spaced bin must be double-quoted: got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/exttool/ 2>&1 | head -5`
Expected: build FAIL — package does not exist / undefined symbols.

- [ ] **Step 3: Write the implementation**

`internal/exttool/exttool.go`:

```go
// Package exttool is the hardcoded catalog of external tools/AI agents gg can
// run per task category (conflict resolution now; commit-message and review in
// later stages), plus their detection. Supporting a new tool is a code change
// (one Builtins entry), never a runtime definition — the agentinit philosophy.
// The catalog's command TEMPLATES never execute directly: the Settings wizard
// materializes them as editable [[tools.command] ] blocks in the gg config, and
// only config content runs.
package exttool

import (
	"os"
	"strings"
)

// Category is a task category a command belongs to.
type Category string

const (
	CatConflict      Category = "conflict"
	CatCommitMessage Category = "commit_message"
	CatReview        Category = "review"
)

// Mode is how a command runs: terminal = suspend the TUI and hand over the
// real terminal (interactive agents, GUI mergetools); capture = run headless
// and capture stdout (stage 2+).
type Mode string

const (
	ModeTerminal Mode = "terminal"
	ModeCapture  Mode = "capture"
)

// CommandTemplate is one catalog default command. Command contains <bin>
// (replaced at generation time by GenerateCommand) plus runtime tokens
// resolved by template.ResolveCommand.
type CommandTemplate struct {
	Category Category
	Name     string // menu label; unique per category across the catalog
	Mode     Mode
	PerFile  bool   // true = runs once per conflicted file (mergetools)
	WhenOp   string // "" = any paused op; else merge|rebase|cherry-pick|revert
	Command  string
}

// Tool is one catalog entry. Bins are candidate binary names probed via
// LookPath; ExtraProbes are absolute paths probed via Stat for installs that
// are typically off PATH (Meld on Windows).
type Tool struct {
	ID          string
	Label       string
	Bins        []string
	ExtraProbes []string
	Commands    []CommandTemplate
}

const claudeConflictCommand = `<bin> --permission-mode acceptEdits \
  --allowedTools "Read" "Edit" "Bash(git status)" "Bash(git diff *)" "Bash(git log *)" "Bash(git add *)" \
  --disallowedTools "Bash(git commit *)" "Bash(git merge *)" "Bash(git rebase *)" "Bash(git push *)" \
  "A git <op> (bringing <source> into <target>) is paused with conflicts in: <conflicted-files>.
   Inspect both sides' history to understand intent, resolve each conflict by editing the files,
   then run git add on each resolved file. Do NOT run git commit or any --continue command --
   stop when everything is staged and summarize what you chose and why."`

// Builtins is the hardcoded catalog. Stage 1 ships conflict templates only;
// commit_message/review defaults land with their stages (recorded in the spec).
func Builtins() []Tool {
	return []Tool{
		{
			ID: "claude", Label: "Claude Code", Bins: []string{"claude"},
			Commands: []CommandTemplate{
				{Category: CatConflict, Name: "Claude", Mode: ModeTerminal, Command: claudeConflictCommand},
			},
		},
		{
			ID: "junie", Label: "JetBrains Junie", Bins: []string{"junie"},
			Commands: []CommandTemplate{
				// Empirical note (spec): whether --merge/--rebase adopt an
				// already-paused op is verified live before merge; the fallback
				// is a --prompt task (see the spec's Junie entry).
				{Category: CatConflict, Name: "Junie (merge)", Mode: ModeTerminal, WhenOp: "merge", Command: "<bin> --merge <source>"},
				{Category: CatConflict, Name: "Junie (rebase)", Mode: ModeTerminal, WhenOp: "rebase", Command: "<bin> --rebase <source>"},
			},
		},
		{
			ID: "meld", Label: "Meld", Bins: []string{"meld"},
			ExtraProbes: []string{`C:\Program Files\Meld\Meld.exe`},
			Commands: []CommandTemplate{
				{Category: CatConflict, Name: "Meld", Mode: ModeTerminal, PerFile: true,
					Command: "<bin> --auto-merge --output=<merged> <local> <base> <remote>"},
			},
		},
	}
}

// Detection is one detected tool. Bin is argv-ready: the bare binary name for
// a PATH hit (portable config), the absolute path for an ExtraProbes hit.
type Detection struct {
	Tool Tool
	Bin  string
}

// Detect probes the catalog with injected lookups (exec.LookPath / os.Stat in
// production — the clipboard nativeArgv seam pattern) so tests never touch the
// developer's machine. First Bins hit wins; ExtraProbes are consulted only
// when no Bins name resolves.
func Detect(look func(string) (string, error), stat func(string) (os.FileInfo, error)) []Detection {
	var out []Detection
	for _, tl := range Builtins() {
		bin := ""
		for _, b := range tl.Bins {
			if _, err := look(b); err == nil {
				bin = b
				break
			}
		}
		if bin == "" {
			for _, p := range tl.ExtraProbes {
				if _, err := stat(p); err == nil {
					bin = p
					break
				}
			}
		}
		if bin != "" {
			out = append(out, Detection{Tool: tl, Bin: bin})
		}
	}
	return out
}

// GenerateCommand materializes a template for a detected binary: <bin> is
// replaced with bin, double-quoted when it contains whitespace (a Windows
// install path). Runtime tokens pass through untouched.
func GenerateCommand(tmpl CommandTemplate, bin string) string {
	if strings.ContainsAny(bin, " \t") {
		bin = `"` + bin + `"`
	}
	return strings.ReplaceAll(tmpl.Command, "<bin>", bin)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/exttool/ -v 2>&1 | tail -15`
Expected: PASS (6 tests).

- [ ] **Step 5: Register the leaf in archtest**

Open `internal/archtest/import_guard_test.go`, find where `gitconfdocs` is declared a leaf/allowed import in the layering DAG (`TestLayeringDAG`), and add `exttool` in the same way (a leaf: may be imported by tui/cli/config layers, imports nothing internal). Run: `go test ./internal/archtest/` — expected PASS.

- [ ] **Step 6: gofmt, vet, commit**

```bash
gofmt -l internal/exttool && go vet ./internal/exttool/ ./internal/archtest/
git add internal/exttool internal/archtest
git commit -m "feat(exttool): tool catalog (claude/junie/meld) + injected-probe detection"
```

---

### Task 2: `internal/template` — command-context resolver

**Files:**
- Create: `internal/template/command.go`
- Test: `internal/template/command_test.go`
- Test: `internal/exttool/tokens_test.go`

**Interfaces:**
- Consumes: `tokenRe`, `cutColon` (existing, `internal/template/tokens.go`); `exttool.Builtins`/`GenerateCommand` (Task 1, cross-check test only).
- Produces:
  - `type CmdCtx struct { Op, Source, Target, Repo string; ConflictedFiles []string; File, Local, Base, Remote, Merged string }`
  - `func ResolveCommand(tmpl string, inputs map[string]string, ctx CmdCtx) (string, error)` (uses `runtime.GOOS`)
  - `func resolveCommandFor(tmpl string, inputs map[string]string, ctx CmdCtx, goos string) (string, error)` (test seam)
  - `func ValidateCommandTokens(tmpl string, perFile bool) error`

- [ ] **Step 1: Write the failing tests**

`internal/template/command_test.go`:

```go
package template

import (
	"strings"
	"testing"
)

func repoCtx() CmdCtx {
	return CmdCtx{
		Op: "merge", Source: "feature", Target: "main",
		Repo:            "/work/my repo",
		ConflictedFiles: []string{"a.go", "dir/b c.go"},
	}
}

func TestResolveCommandProseTokensRaw(t *testing.T) {
	got, err := resolveCommandFor(`agent "resolve <op> of <source> into <target>: <conflicted-files>"`, nil, repoCtx(), "linux")
	if err != nil {
		t.Fatal(err)
	}
	want := `agent "resolve merge of feature into main: a.go dir/b c.go"`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestResolveCommandPathTokensQuoted(t *testing.T) {
	ctx := repoCtx()
	ctx.File = "dir/b c.go"
	ctx.Local, ctx.Base, ctx.Remote = "/tmp/l 1", "/tmp/b", "/tmp/r"
	ctx.Merged = "/work/my repo/dir/b c.go"
	got, err := resolveCommandFor(`meld --output=<merged> <local> <base> <remote>`, nil, ctx, "linux")
	if err != nil {
		t.Fatal(err)
	}
	want := `meld --output='/work/my repo/dir/b c.go' '/tmp/l 1' '/tmp/b' '/tmp/r'`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestResolveCommandPosixQuoteEscapesSingleQuote(t *testing.T) {
	ctx := repoCtx()
	ctx.File = "it's.go"
	ctx.Local, ctx.Base, ctx.Remote, ctx.Merged = "/t/l", "/t/b", "/t/r", "/t/m"
	got, err := resolveCommandFor(`tool <file>`, nil, ctx, "linux")
	if err != nil {
		t.Fatal(err)
	}
	if want := `tool 'it'\''s.go'`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveCommandWindowsQuoting(t *testing.T) {
	ctx := repoCtx()
	got, err := resolveCommandFor(`tool <repo>`, nil, ctx, "windows")
	if err != nil {
		t.Fatal(err)
	}
	if want := `tool "/work/my repo"`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveCommandUserInputs(t *testing.T) {
	got, err := resolveCommandFor(`agent --hint <user:hint>`, map[string]string{"hint": "be careful"}, repoCtx(), "linux")
	if err != nil {
		t.Fatal(err)
	}
	if want := `agent --hint be careful`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if _, err := resolveCommandFor(`agent <user:hint>`, nil, repoCtx(), "linux"); err == nil {
		t.Error("missing user input must error")
	}
}

func TestResolveCommandErrors(t *testing.T) {
	cases := []struct{ name, tmpl string }{
		{"unknown token", `tool <nope>`},
		{"bin at runtime", `<bin> --merge <source>`},
		{"per-file token without file context", `tool <local>`},
	}
	for _, c := range cases {
		if _, err := resolveCommandFor(c.tmpl, nil, repoCtx(), "linux"); err == nil {
			t.Errorf("%s: want error, got none", c.name)
		}
	}
}

func TestResolveCommandEmptyProseIsAllowed(t *testing.T) {
	// Source/Target may be empty (e.g. a revert with no attribution); agents
	// recover via git, so the resolver must not fail.
	ctx := CmdCtx{Op: "revert", Repo: "/r", ConflictedFiles: []string{"a"}}
	got, err := resolveCommandFor(`agent "<op> <source>"`, nil, ctx, "linux")
	if err != nil {
		t.Fatal(err)
	}
	if want := `agent "revert "`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestValidateCommandTokens(t *testing.T) {
	if err := ValidateCommandTokens(`a <op> <source> <target> <conflicted-files> <repo> <user:x>`, false); err != nil {
		t.Errorf("repo-level tokens: %v", err)
	}
	if err := ValidateCommandTokens(`m <file> <local> <base> <remote> <merged>`, true); err != nil {
		t.Errorf("per-file tokens: %v", err)
	}
	if err := ValidateCommandTokens(`m <local>`, false); err == nil {
		t.Error("per-file token in repo-level command must error")
	}
	if err := ValidateCommandTokens(`m <bogus>`, true); err == nil {
		t.Error("unknown token must error")
	}
	if err := ValidateCommandTokens(`<bin> x`, false); err == nil || !strings.Contains(err.Error(), "generated") {
		t.Errorf("<bin> must error mentioning generation, got %v", err)
	}
}
```

`internal/exttool/tokens_test.go`:

```go
package exttool

import (
	"testing"

	"github.com/homeend/gigagit/internal/template"
)

// Every builtin template must validate once <bin> is generated away — a
// catalog typo must fail here, not in a user's config.
func TestBuiltinTemplateTokensValidate(t *testing.T) {
	for _, tl := range Builtins() {
		for _, ct := range tl.Commands {
			gen := GenerateCommand(ct, "tool")
			if err := template.ValidateCommandTokens(gen, ct.PerFile); err != nil {
				t.Errorf("%s/%s: %v", tl.ID, ct.Name, err)
			}
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/template/ ./internal/exttool/ 2>&1 | head -5`
Expected: build FAIL — `CmdCtx`/`resolveCommandFor`/`ValidateCommandTokens` undefined.

- [ ] **Step 3: Write the implementation**

`internal/template/command.go`:

```go
package template

import (
	"fmt"
	"runtime"
	"strings"
)

// CmdCtx carries the values for external-tool command tokens. Prose fields
// (Op/Source/Target/ConflictedFiles) substitute raw — the default commands
// embed them inside prompt strings, and git refnames cannot contain spaces.
// Path fields (Repo/File/Local/Base/Remote/Merged) substitute shell-quoted:
// they sit in argv positions and may contain spaces. Per-file fields are ""
// for a repo-level command; using their tokens then is an error.
type CmdCtx struct {
	Op, Source, Target, Repo          string
	ConflictedFiles                   []string
	File, Local, Base, Remote, Merged string
}

// ResolveCommand substitutes every <...> token in an external-tool command.
// inputs supplies <user:LABEL> values (raw). Unknown tokens, <bin> (a
// generation-time token), a missing user input, or a per-file token with no
// per-file context are errors — never silently passed through.
func ResolveCommand(tmpl string, inputs map[string]string, ctx CmdCtx) (string, error) {
	return resolveCommandFor(tmpl, inputs, ctx, runtime.GOOS)
}

func resolveCommandFor(tmpl string, inputs map[string]string, ctx CmdCtx, goos string) (string, error) {
	var firstErr error
	out := tokenRe.ReplaceAllStringFunc(tmpl, func(tok string) string {
		body := tok[1 : len(tok)-1]
		val, err := resolveCommandToken(body, inputs, ctx, goos)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return val
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

func resolveCommandToken(body string, inputs map[string]string, ctx CmdCtx, goos string) (string, error) {
	prefix, rest, hasColon := cutColon(body)
	switch prefix {
	case "op":
		return ctx.Op, nil
	case "source":
		return ctx.Source, nil
	case "target":
		return ctx.Target, nil
	case "conflicted-files":
		return strings.Join(ctx.ConflictedFiles, " "), nil
	case "repo":
		return quoteArgFor(ctx.Repo, goos), nil
	case "file", "local", "base", "remote", "merged":
		v := map[string]string{
			"file": ctx.File, "local": ctx.Local, "base": ctx.Base,
			"remote": ctx.Remote, "merged": ctx.Merged,
		}[prefix]
		if v == "" {
			return "", fmt.Errorf("template: <%s> requires a per-file conflict context", prefix)
		}
		return quoteArgFor(v, goos), nil
	case "user":
		if !hasColon {
			return "", fmt.Errorf("template: <user> requires a label, e.g. <user:hint>")
		}
		v, ok := inputs[rest]
		if !ok {
			return "", fmt.Errorf("template: missing input for <user:%s>", rest)
		}
		return v, nil
	case "bin":
		return "", fmt.Errorf("template: <bin> is resolved when the command is generated — replace it with the tool binary")
	default:
		return "", fmt.Errorf("template: unknown command token <%s>", body)
	}
}

// commandTokens is the runtime vocabulary; the bool marks per-file-only tokens.
var commandTokens = map[string]bool{
	"op": false, "source": false, "target": false, "conflicted-files": false,
	"repo": false, "user": false,
	"file": true, "local": true, "base": true, "remote": true, "merged": true,
}

// ValidateCommandTokens checks a command template's token set without
// resolving values: unknown tokens, <bin>, and per-file tokens in a
// repo-level command are errors. Used to make a bad config block inert with
// a pointed message instead of failing at run time.
func ValidateCommandTokens(tmpl string, perFile bool) error {
	for _, m := range tokenRe.FindAllStringSubmatch(tmpl, -1) {
		prefix, _, _ := cutColon(m[1])
		if prefix == "bin" {
			return fmt.Errorf("template: <bin> is only valid in catalog templates; it is replaced when the command is generated")
		}
		perFileOnly, known := commandTokens[prefix]
		if !known {
			return fmt.Errorf("template: unknown command token <%s>", m[1])
		}
		if perFileOnly && !perFile {
			return fmt.Errorf("template: <%s> is only valid in a per_file command", prefix)
		}
	}
	return nil
}

// quoteArgFor shell-quotes one argv value: POSIX single quotes (embedded
// single quotes via '\''), or double quotes on Windows (cmd.exe).
func quoteArgFor(s, goos string) string {
	if goos == "windows" {
		return `"` + s + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/template/ ./internal/exttool/ 2>&1 | tail -5`
Expected: PASS (both packages).

- [ ] **Step 5: gofmt, vet, commit**

```bash
gofmt -l internal/template internal/exttool && go vet ./internal/template/ ./internal/exttool/
git add internal/template internal/exttool
git commit -m "feat(template): command-context token resolver with per-kind quoting"
```

---

### Task 3: `internal/config` — the `[tools]` section

**Files:**
- Create: `internal/config/tools.go`
- Test: `internal/config/tools_test.go`
- Modify: `internal/config/config.go` (Config struct ~line 119, Load ~line 150)
- Modify: `internal/config/template.go` (settingDocs + section loop line 93)
- Modify: `internal/config/template_test.go` (coverage `check` calls ~line 30)

**Interfaces:**
- Consumes: `atomicWriteFile` (existing, `write.go`).
- Produces:
  - `type ToolCommand struct { Category string; Name string; Mode string; PerFile bool; WhenOp string; Command string }` (toml tags: `category name mode per_file when_op command`)
  - `type ToolsConfig struct { Command []ToolCommand \`toml:"command"\` }`; `Config.Tools ToolsConfig \`toml:"tools"\``
  - `func (tc ToolCommand) Key() string` — `tc.Category + "\x00" + tc.Name`
  - `func ValidateToolCommand(tc ToolCommand) error` (structural only; token validation is template's)
  - `func AppendToolCommands(path string, cmds []ToolCommand) error`

- [ ] **Step 1: Write the failing tests**

`internal/config/tools_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCfg(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadParsesToolCommands(t *testing.T) {
	dir := t.TempDir()
	repo := writeCfg(t, dir, ".gg.toml", `
[[tools.command]]
category = "conflict"
name = "Claude"
mode = "terminal"
per_file = false
when_op = ""
command = '''
claude "resolve <op>"
'''
`)
	cfg, err := Load("", repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tools.Command) != 1 {
		t.Fatalf("got %d commands, want 1", len(cfg.Tools.Command))
	}
	tc := cfg.Tools.Command[0]
	if tc.Category != "conflict" || tc.Name != "Claude" || tc.Mode != "terminal" {
		t.Errorf("parsed %+v", tc)
	}
	if want := "claude \"resolve <op>\"\n"; tc.Command != want {
		t.Errorf("command = %q, want %q", tc.Command, want)
	}
}

func TestToolsOverlayConcatRepoWins(t *testing.T) {
	dir := t.TempDir()
	global := writeCfg(t, dir, "global.toml", `
[[tools.command]]
category = "conflict"
name = "Claude"
mode = "terminal"
command = "claude-global"

[[tools.command]]
category = "conflict"
name = "Meld"
mode = "terminal"
per_file = true
command = "meld-global"
`)
	repo := writeCfg(t, dir, ".gg.toml", `
[[tools.command]]
category = "conflict"
name = "Claude"
mode = "terminal"
command = "claude-repo"
`)
	cfg, err := Load(global, repo)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, tc := range cfg.Tools.Command {
		got[tc.Name] = tc.Command
	}
	if len(cfg.Tools.Command) != 2 {
		t.Fatalf("want concat of 2 distinct commands, got %d: %v", len(cfg.Tools.Command), got)
	}
	if got["Claude"] != "claude-repo" {
		t.Errorf("repo must win the (category,name) collision: got %q", got["Claude"])
	}
	if got["Meld"] != "meld-global" {
		t.Errorf("global-only command must survive: got %q", got["Meld"])
	}
}

func TestValidateToolCommand(t *testing.T) {
	ok := ToolCommand{Category: "conflict", Name: "X", Mode: "terminal", Command: "x"}
	if err := ValidateToolCommand(ok); err != nil {
		t.Errorf("valid command rejected: %v", err)
	}
	bad := []ToolCommand{
		{Category: "nope", Name: "X", Mode: "terminal", Command: "x"},
		{Category: "conflict", Name: "", Mode: "terminal", Command: "x"},
		{Category: "conflict", Name: "X", Mode: "sideways", Command: "x"},
		{Category: "conflict", Name: "X", Mode: "terminal", Command: ""},
		{Category: "commit_message", Name: "X", Mode: "terminal", PerFile: true, Command: "x"},
		{Category: "conflict", Name: "X", Mode: "terminal", WhenOp: "push", Command: "x"},
	}
	for i, tc := range bad {
		if err := ValidateToolCommand(tc); err == nil {
			t.Errorf("bad[%d] %+v: want error", i, tc)
		}
	}
}

func TestAppendToolCommands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml") // file does not exist yet
	cmds := []ToolCommand{{Category: "conflict", Name: "Meld", Mode: "terminal", PerFile: true,
		Command: "meld --auto-merge --output=<merged> <local> <base> <remote>"}}
	if err := AppendToolCommands(path, cmds); err != nil {
		t.Fatal(err)
	}
	// Round-trips through Load.
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tools.Command) != 1 || cfg.Tools.Command[0].Name != "Meld" || !cfg.Tools.Command[0].PerFile {
		t.Fatalf("round-trip got %+v", cfg.Tools.Command)
	}
	// Appending more preserves existing content byte-for-byte.
	before, _ := os.ReadFile(path)
	if err := AppendToolCommands(path, []ToolCommand{{Category: "conflict", Name: "Claude", Mode: "terminal", Command: "claude x"}}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(after), string(before)) {
		t.Error("append must not rewrite existing content")
	}
	cfg, err = Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tools.Command) != 2 {
		t.Fatalf("want 2 after second append, got %d", len(cfg.Tools.Command))
	}
	// A command containing the ''' delimiter is refused, not corrupted.
	if err := AppendToolCommands(path, []ToolCommand{{Category: "conflict", Name: "Evil", Mode: "terminal", Command: "x''' oops"}}); err == nil {
		t.Error("''' in command must be refused")
	}
}

// The scalar writers must not corrupt a file holding [[tools.command]] blocks
// (their multi-line command values could contain section-header-like lines).
func TestScalarWriterSurvivesToolBlocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := AppendToolCommands(path, []ToolCommand{{Category: "conflict", Name: "X", Mode: "terminal",
		Command: "line1\n[worktree]\nline3"}}); err != nil {
		t.Fatal(err)
	}
	if err := SetGlobalRefreshEnabled(path, true); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Refresh.Enabled {
		t.Error("refresh.enabled not set")
	}
	if len(cfg.Tools.Command) != 1 || cfg.Tools.Command[0].Command != "line1\n[worktree]\nline3\n" {
		t.Errorf("tool block corrupted: %+v", cfg.Tools.Command)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ 2>&1 | head -5`
Expected: build FAIL — `ToolCommand` undefined.

- [ ] **Step 3: Write the implementation**

`internal/config/tools.go`:

```go
package config

import (
	"fmt"
	"os"
	"strings"
)

// ToolCommand is one external-tool command ([[tools.command]] block): a menu
// label plus the shell command to run for a task category. Blocks are written
// by the Settings "External tools" wizard or by hand; only config content
// ever executes (catalog templates are generation-time input).
type ToolCommand struct {
	Category string `toml:"category"` // conflict | commit_message | review
	Name     string `toml:"name"`     // menu label; unique per category
	Mode     string `toml:"mode"`     // terminal | capture (capture: stage 2+)
	PerFile  bool   `toml:"per_file"` // conflict only: run once per conflicted file
	WhenOp   string `toml:"when_op"`  // "" = any paused op; else merge|rebase|cherry-pick|revert
	Command  string `toml:"command"`  // shell command with <token> placeholders
}

// ToolsConfig is the [tools] section.
type ToolsConfig struct {
	Command []ToolCommand `toml:"command"`
}

// Key identifies a command for the overlay collision rule.
func (tc ToolCommand) Key() string { return tc.Category + "\x00" + tc.Name }

// overlayTools implements the tools-list overlay: CONCATENATE global + repo,
// repo winning a (category,name) collision in place. This is a deliberate
// exception to the field-level zero-is-unset rule — lists merge, they do not
// replace — documented in the settingDocs comment.
func overlayTools(dst *ToolsConfig, src ToolsConfig) {
	for _, tc := range src.Command {
		replaced := false
		for i, have := range dst.Command {
			if have.Key() == tc.Key() {
				dst.Command[i] = tc
				replaced = true
				break
			}
		}
		if !replaced {
			dst.Command = append(dst.Command, tc)
		}
	}
}

// ValidateToolCommand checks a block's structural fields. Token validation
// (template.ValidateCommandTokens) is the frontend's job — config stays free
// of the template dependency. An invalid block is made inert by the caller,
// never a startup error.
func ValidateToolCommand(tc ToolCommand) error {
	switch tc.Category {
	case "conflict", "commit_message", "review":
	default:
		return fmt.Errorf("tools: unknown category %q (want conflict|commit_message|review)", tc.Category)
	}
	if strings.TrimSpace(tc.Name) == "" {
		return fmt.Errorf("tools: a command needs a name")
	}
	switch tc.Mode {
	case "terminal", "capture":
	default:
		return fmt.Errorf("tools: %s: unknown mode %q (want terminal|capture)", tc.Name, tc.Mode)
	}
	if strings.TrimSpace(tc.Command) == "" {
		return fmt.Errorf("tools: %s: empty command", tc.Name)
	}
	if tc.PerFile && tc.Category != "conflict" {
		return fmt.Errorf("tools: %s: per_file is only valid for category = \"conflict\"", tc.Name)
	}
	switch tc.WhenOp {
	case "", "merge", "rebase", "cherry-pick", "revert":
	default:
		return fmt.Errorf("tools: %s: unknown when_op %q", tc.Name, tc.WhenOp)
	}
	return nil
}

// AppendToolCommands appends [[tools.command]] blocks to the config file at
// path (creating it if missing), never touching existing content — the wizard
// must not overwrite a user-edited command. Command bodies are written as
// multi-line ''' literals; a body containing ''' is refused (TOML literal
// strings cannot escape their delimiter).
func AppendToolCommands(path string, cmds []ToolCommand) error {
	if path == "" {
		return fmt.Errorf("config: no config path; refusing to write")
	}
	for _, tc := range cmds {
		if strings.Contains(tc.Command, "'''") {
			return fmt.Errorf("config: %s: command must not contain ''' (TOML literal delimiter)", tc.Name)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var b strings.Builder
	b.Write(raw)
	for _, tc := range cmds {
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n\n") {
			if strings.HasSuffix(b.String(), "\n") {
				b.WriteString("\n")
			} else {
				b.WriteString("\n\n")
			}
		}
		fmt.Fprintf(&b, "[[tools.command]]\n")
		fmt.Fprintf(&b, "category = %q\n", tc.Category)
		fmt.Fprintf(&b, "name = %q\n", tc.Name)
		fmt.Fprintf(&b, "mode = %q\n", tc.Mode)
		fmt.Fprintf(&b, "per_file = %t\n", tc.PerFile)
		fmt.Fprintf(&b, "when_op = %q\n", tc.WhenOp)
		b.WriteString("command = '''\n")
		b.WriteString(strings.TrimRight(tc.Command, "\n"))
		b.WriteString("\n'''\n")
	}
	return atomicWriteFile(path, []byte(b.String()))
}
```

`internal/config/config.go` edits:

```go
// In the Config struct (after Refresh):
	Tools    ToolsConfig    `toml:"tools"`

// In Load's overlay block (after overlayRefresh):
			overlayTools(&cfg.Tools, layer.Tools)
```

`internal/config/template.go` edits — add to `settingDocs` (after the refresh entries):

```go
	{"tools", "command", nil, "external-tool commands as [[tools.command]] blocks: category (conflict|commit_message|review), name, mode (terminal|capture), per_file, when_op, command (multi-line '''…''' literal; tokens: <op> <source> <target> <conflicted-files> <repo> <file> <local> <base> <remote> <merged> <user:LABEL>); global + repo lists CONCATENATE, repo wins a (category,name) collision; generate defaults via Settings → External tools"},
```

and change the section loop:

```go
	for _, section := range []string{"worktree", "ui", "debug", "refresh", "tools"} {
```

`internal/config/template_test.go` edit — `TestSettingDocsCoverAllFields` reflects over section structs; `ToolsConfig`'s only field is the `command` list, covered by the entry above. Add after the `check("refresh", …)` line:

```go
	check("tools", reflect.TypeOf(ToolsConfig{}))
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ 2>&1 | tail -5`
Expected: PASS (including the pre-existing template/populate tests — `gg config init`/`populate` pick the section up from settingDocs automatically; if a populate test asserts an exact section list, extend it with `tools`).

- [ ] **Step 5: gofmt, vet, commit**

```bash
gofmt -l internal/config && go vet ./internal/config/
git add internal/config
git commit -m "feat(config): [tools] section — [[tools.command]] blocks, concat overlay, append writer"
```

---

### Task 4: `internal/promptstate` — approved tool commands

**Files:**
- Modify: `internal/promptstate/store.go` (interface), `internal/promptstate/file_store.go`
- Test: extend `internal/promptstate/file_store_test.go` (or create if the file layout differs — check `ls internal/promptstate/`)

**Interfaces:**
- Produces (interface additions):
  - `ApprovedToolCommands(repoKey string) map[string]bool`
  - `ApproveToolCommand(repoKey, hash string) error`
- On-disk: `approved_tools` map in `prompts.toml` (`map[string][]string`, keyed by repoKey).

- [ ] **Step 1: Find every Store implementor**

Run: `grep -rln "promptstate.Store" internal/ | sort`
Every non-`FileStore` implementor (test fakes in `internal/tui/*_test.go`) must gain the two new methods in Step 3 or compilation breaks — list them now.

- [ ] **Step 2: Write the failing test**

Append to the promptstate test file:

```go
func TestApproveToolCommand(t *testing.T) {
	fs := NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	if fs.ApprovedToolCommands("/repo/a")["abc123"] {
		t.Fatal("empty store must not approve")
	}
	if err := fs.ApproveToolCommand("/repo/a", "abc123"); err != nil {
		t.Fatal(err)
	}
	if err := fs.ApproveToolCommand("/repo/a", "abc123"); err != nil { // idempotent
		t.Fatal(err)
	}
	if !fs.ApprovedToolCommands("/repo/a")["abc123"] {
		t.Error("approval not persisted")
	}
	if fs.ApprovedToolCommands("/repo/b")["abc123"] {
		t.Error("approval leaked across repos")
	}
	// Survives reopen (it's on disk).
	fs2 := NewFileStore(fs.path)
	if !fs2.ApprovedToolCommands("/repo/a")["abc123"] {
		t.Error("approval lost on reload")
	}
}
```

(If `fs.path` is unexported-inaccessible from the test file's package, the test lives in `package promptstate` — it does, matching `file_store.go`.)

- [ ] **Step 3: Run to verify failure, then implement**

Run: `go test ./internal/promptstate/ 2>&1 | head -3` — expected: undefined methods.

`store.go` — add to the `Store` interface:

```go
	// ApprovedToolCommands returns the external-tool command hashes approved
	// for repoKey (first-run approval memory; hash = truncated sha256 of the
	// command template text, so any edit re-prompts).
	ApprovedToolCommands(repoKey string) map[string]bool
	// ApproveToolCommand records a per-repo command approval (idempotent) and persists.
	ApproveToolCommand(repoKey, hash string) error
```

`file_store.go` — extend `records`:

```go
	ApprovedTools     map[string][]string `toml:"approved_tools"`
```

initialize it in `read()`'s `empty` value and nil-guard (mirror `DismissedNotices` exactly), and add:

```go
// ApprovedToolCommands returns the tool-command hashes approved for repoKey.
func (fs *FileStore) ApprovedToolCommands(repoKey string) map[string]bool {
	return toSet(fs.read().ApprovedTools[repoKey])
}

// ApproveToolCommand records a per-repo tool-command approval (idempotent) and persists.
func (fs *FileStore) ApproveToolCommand(repoKey, hash string) error {
	r := fs.read()
	if toSet(r.ApprovedTools[repoKey])[hash] {
		return nil
	}
	r.ApprovedTools[repoKey] = append(r.ApprovedTools[repoKey], hash)
	return fs.write(r)
}
```

Then add the two methods to every fake found in Step 1 (return `map[string]bool{}` / record into a map field so TUI tests can assert).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/promptstate/ ./internal/tui/ 2>&1 | tail -3`
Expected: PASS (tui compiles again with extended fakes).

- [ ] **Step 5: gofmt, vet, commit**

```bash
gofmt -l internal/promptstate internal/tui && go vet ./internal/promptstate/
git add internal/promptstate internal/tui
git commit -m "feat(promptstate): per-repo approved external-tool command hashes"
```

---

### Task 5: `domain.ConflictFileVersions` — the per-file quartet

**Files:**
- Create: `internal/domain/conflict_versions.go`
- Test: `internal/domain/conflict_versions_test.go`

**Interfaces:**
- Consumes: `s.repo.ShowFile(ctx, rev, path)` (existing verb — `rev=":2"` yields `git show :2:<path>`), the `query()` helper (Read reservation), fixtures `mergeConflictDir(t)`/`svcAt(dir)` from `internal/domain/conflict_test.go`.
- Produces: `func (s *Service) ConflictFileVersions(ctx context.Context, path string, hasBase bool) (local, base, remote string, cleanup func(), err error)` — temp-file paths; `cleanup` removes all three; `base` is an empty temp file when `hasBase` is false (git-mergetool behavior for add/add).

- [ ] **Step 1: Write the failing test**

`internal/domain/conflict_versions_test.go`:

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConflictFileVersionsBothSides(t *testing.T) {
	dir := mergeConflictDir(t) // paused merge of feature into main, one conflicted file
	svc := svcAt(dir)
	// Discover the conflicted path from status (fixture-agnostic).
	st, err := svc.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	conf := st.Conflicts()
	if len(conf) == 0 {
		t.Fatal("fixture has no conflicts")
	}
	path := conf[0].Path

	local, base, remote, cleanup, err := svc.ConflictFileVersions(context.Background(), path, conf[0].ConflictHasBase())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	lb, _ := os.ReadFile(local)
	rb, _ := os.ReadFile(remote)
	if len(lb) == 0 || len(rb) == 0 {
		t.Fatalf("local/remote must carry each side's content (%d/%d bytes)", len(lb), len(rb))
	}
	if string(lb) == string(rb) {
		t.Error("local and remote sides must differ in a conflict")
	}
	if conf[0].ConflictHasBase() {
		if _, err := os.Stat(base); err != nil {
			t.Errorf("base temp missing: %v", err)
		}
	}
	// Temp names keep the real extension for tool syntax highlighting.
	if ext := filepath.Ext(path); ext != "" {
		for _, p := range []string{local, base, remote} {
			if !strings.HasSuffix(p, ext) {
				t.Errorf("%s should keep extension %s", p, ext)
			}
		}
	}
	cleanup()
	for _, p := range []string{local, base, remote} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("cleanup left %s", p)
		}
	}
}

func TestConflictFileVersionsNoBase(t *testing.T) {
	dir := mergeConflictDir(t)
	svc := svcAt(dir)
	st, _ := svc.Status(context.Background())
	path := st.Conflicts()[0].Path
	_, base, _, cleanup, err := svc.ConflictFileVersions(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	data, err := os.ReadFile(base)
	if err != nil {
		t.Fatalf("hasBase=false must still create an empty base temp: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("base must be empty when hasBase=false, got %d bytes", len(data))
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/domain/ -run TestConflictFileVersions 2>&1 | head -3`
Expected: build FAIL — method undefined.

- [ ] **Step 3: Write the implementation**

`internal/domain/conflict_versions.go`:

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
)

// conflictVersions is the query result shape (query() returns one value).
type conflictVersions struct{ local, base, remote string }

// ConflictFileVersions materializes the index stages of a conflicted path
// into temp files for an external per-file merge tool: local = stage :2
// (ours), remote = stage :3 (theirs), base = stage :1 (common ancestor) —
// or an empty temp file when hasBase is false (an add/add conflict has no
// stage 1; git mergetool behaves the same). Temp names keep the real file
// extension so GUI tools syntax-highlight. cleanup removes all three files;
// callers must invoke it after the tool exits (best-effort).
func (s *Service) ConflictFileVersions(ctx context.Context, path string, hasBase bool) (local, base, remote string, cleanup func(), err error) {
	v, err := query(ctx, s, "conflictversions:"+path, func(ctx context.Context) (conflictVersions, error) {
		var out conflictVersions
		var made []string
		fail := func(err error) (conflictVersions, error) {
			for _, p := range made {
				os.Remove(p)
			}
			return conflictVersions{}, err
		}
		write := func(kind, rev string) (string, error) {
			var data []byte
			if rev != "" {
				var err error
				if data, err = s.repo.ShowFile(ctx, rev, path); err != nil {
					return "", err
				}
			}
			f, err := os.CreateTemp("", "gg-"+kind+"-*"+filepath.Ext(path))
			if err != nil {
				return "", err
			}
			made = append(made, f.Name())
			if _, err := f.Write(data); err != nil {
				f.Close()
				return "", err
			}
			return f.Name(), f.Close()
		}
		var werr error
		if out.local, werr = write("local", ":2"); werr != nil {
			return fail(werr)
		}
		baseRev := ""
		if hasBase {
			baseRev = ":1"
		}
		if out.base, werr = write("base", baseRev); werr != nil {
			return fail(werr)
		}
		if out.remote, werr = write("remote", ":3"); werr != nil {
			return fail(werr)
		}
		return out, nil
	})
	if err != nil {
		return "", "", "", nil, err
	}
	cleanup = func() {
		os.Remove(v.local)
		os.Remove(v.base)
		os.Remove(v.remote)
	}
	return v.local, v.base, v.remote, cleanup, nil
}
```

(If `query`'s signature differs — check its definition in `internal/domain/service.go` — adapt the call, keeping the Read reservation semantics.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/domain/ -run TestConflictFileVersions -v 2>&1 | tail -5`
Expected: PASS (2 tests).

- [ ] **Step 5: gofmt, vet, commit**

```bash
gofmt -l internal/domain && go vet ./internal/domain/
git add internal/domain
git commit -m "feat(domain): ConflictFileVersions — LOCAL/BASE/REMOTE quartet temp files from index stages"
```

---

### Task 6: TUI — validated command accessor + conflict tool picker

**Files:**
- Create: `internal/tui/tools.go`
- Test: `internal/tui/tools_test.go`
- Modify: `internal/tui/model.go` (add `toolNoted map[string]bool` field near `promptStore` ~line 73; initialize `toolNoted: map[string]bool{}` in `New()` ~line 255)
- Modify: `internal/tui/conflict_process.go` (states, `t` key, hints)
- Test: `internal/tui/conflict_tools_test.go`

**Interfaces:**
- Consumes: `config.ToolCommand`/`ValidateToolCommand` (Task 3), `template.ValidateCommandTokens` (Task 2), `observ.NoteFailure(source string, err error)`, `model.FileStatus.ConflictClass()/ConflictHasBase()`, `domain.ConflictState`.
- Produces (Task 7 relies on):
  - `func (m Model) toolCommands(category string) []config.ToolCommand` — merged from `m.cfg.Tools.Command`, structurally valid, token-valid, `mode == "terminal"` only (a capture block notes "not supported yet"); each invalid block gets ONE `observ.NoteFailure("tools", err)` per session (dedup by `tc.Key()` in `m.toolNoted`).
  - `func conflictToolChoices(cmds []config.ToolCommand, op string, focused *model.FileStatus) []config.ToolCommand` — pure: `when_op` filter (`tc.WhenOp == "" || tc.WhenOp == op`); `per_file` commands included only when `focused != nil && focused.ConflictClass() == model.ConflictBothSides`.
  - `conflictProcess` fields: `toolChoices []config.ToolCommand`, `toolSel int`, plus new `confState` values `confToolPick`, `confToolFill`, `confToolApprove`, `confToolMark` (fill/approve/mark are wired in Task 7; declare all four now so the enum is stable).
  - `conflictHints(files, sel, inProgress string, nTools int)` — extended signature.

- [ ] **Step 1: Write the failing tests**

`internal/tui/tools_test.go`:

```go
package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/model"
)

func toolCfg(cmds ...config.ToolCommand) Model {
	m := New(nil) // the zero-service constructor pattern (see commit_branch_ops_test.go)
	m.cfg.Tools.Command = cmds
	return m
}

func TestToolCommandsFiltersInvalidAndCapture(t *testing.T) {
	m := toolCfg(
		config.ToolCommand{Category: "conflict", Name: "OK", Mode: "terminal", Command: "x <op>"},
		config.ToolCommand{Category: "conflict", Name: "Cap", Mode: "capture", Command: "x"},
		config.ToolCommand{Category: "conflict", Name: "BadTok", Mode: "terminal", Command: "x <bogus>"},
		config.ToolCommand{Category: "bogus", Name: "BadCat", Mode: "terminal", Command: "x"},
		config.ToolCommand{Category: "commit_message", Name: "Other", Mode: "terminal", Command: "x"},
	)
	got := m.toolCommands("conflict")
	if len(got) != 1 || got[0].Name != "OK" {
		t.Fatalf("toolCommands = %+v, want just OK", got)
	}
	if len(m.toolNoted) != 3 { // Cap, BadTok, BadCat noted once each; Other is valid, different category
		t.Errorf("noted %d blocks, want 3: %v", len(m.toolNoted), m.toolNoted)
	}
	m.toolCommands("conflict") // second call must not re-note
	if len(m.toolNoted) != 3 {
		t.Errorf("re-noting on repeat call: %v", m.toolNoted)
	}
}

func TestConflictToolChoices(t *testing.T) {
	repoLevel := config.ToolCommand{Category: "conflict", Name: "Agent", Mode: "terminal", Command: "a"}
	mergeOnly := config.ToolCommand{Category: "conflict", Name: "JM", Mode: "terminal", WhenOp: "merge", Command: "j"}
	perFile := config.ToolCommand{Category: "conflict", Name: "Meld", Mode: "terminal", PerFile: true, Command: "m"}
	all := []config.ToolCommand{repoLevel, mergeOnly, perFile}

	both := &model.FileStatus{Path: "a.go", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'} // UU = both modified
	names := func(cs []config.ToolCommand) []string {
		var out []string
		for _, c := range cs {
			out = append(out, c.Name)
		}
		return out
	}
	if got := names(conflictToolChoices(all, "merge", both)); len(got) != 3 {
		t.Errorf("merge+both-sides: %v, want all 3", got)
	}
	if got := names(conflictToolChoices(all, "rebase", both)); len(got) != 2 || got[0] != "Agent" || got[1] != "Meld" {
		t.Errorf("rebase filters when_op=merge: %v", got)
	}
	if got := names(conflictToolChoices(all, "merge", nil)); len(got) != 2 {
		t.Errorf("no focused file drops per_file: %v", got)
	}
}
```

(If constructing `model.FileStatus` with `Staged/Unstaged` bytes does not yield `ConflictBothSides`, check `internal/model/conflict.go` for the field/XY encoding used by `ConflictClass()` and build the fixture the way `internal/tui/conflict_process_test.go` does — mirror an existing test fixture rather than inventing one.)

`internal/tui/conflict_tools_test.go`:

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// NOTE: keyRunes(s) already exists in this package (irebase_view_test.go) —
// use it, do NOT redeclare it.

func conflictModelWithTools(t *testing.T, cmds ...config.ToolCommand) (Model, *conflictProcess) {
	t.Helper()
	m := toolCfg(cmds...)
	m.conflict = domain.ConflictState{Op: "merge", Source: "feature", Target: "main"}
	m.status.Files = []model.FileStatus{{Path: "a.go", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'}}
	m2, _ := startConflictProcess(m)
	p, ok := m2.proc.(*conflictProcess)
	if !ok {
		t.Fatal("conflict process did not open")
	}
	return m2, p
}

func TestConflictTKeyOpensPicker(t *testing.T) {
	m, p := conflictModelWithTools(t,
		config.ToolCommand{Category: "conflict", Name: "Agent", Mode: "terminal", Command: "a <op>"})
	m, _ = p.update(m, keyRunes("t"))
	if p.st != confToolPick {
		t.Fatalf("state = %v, want confToolPick", p.st)
	}
	if len(p.toolChoices) != 1 || p.toolChoices[0].Name != "Agent" {
		t.Errorf("choices = %+v", p.toolChoices)
	}
	// esc returns to the list.
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if p.st != confListing {
		t.Errorf("esc: state = %v, want confListing", p.st)
	}
	_ = m
}

func TestConflictTKeyNoCommandsIsNoop(t *testing.T) {
	m, p := conflictModelWithTools(t) // no commands configured
	m, _ = p.update(m, keyRunes("t"))
	if p.st != confListing {
		t.Errorf("t with zero commands must stay in listing, got %v", p.st)
	}
	if m.statusMsg == "" {
		t.Error("expected a status hint about configuring tools")
	}
}

func TestConflictHintsAdvertiseTools(t *testing.T) {
	files := []model.FileStatus{{Path: "a.go", Staged: 'U', Unstaged: 'U'}}
	withTools := conflictHints(files, 0, "merge", 1)
	found := false
	for _, h := range withTools {
		if h == "[t] tools" {
			found = true
		}
	}
	if !found {
		t.Errorf("hints missing [t] tools: %v", withTools)
	}
	for _, h := range conflictHints(files, 0, "merge", 0) {
		if h == "[t] tools" {
			t.Error("[t] shown with zero commands")
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run 'TestToolCommands|TestConflictTool|TestConflictTKey|TestConflictHints' 2>&1 | head -5`
Expected: build FAIL — undefined symbols.

- [ ] **Step 3: Implement**

`internal/tui/tools.go`:

```go
package tui

import (
	"fmt"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/template"
)

// toolCommands returns the runnable external-tool commands for a category:
// structurally valid, token-valid, terminal-mode. An invalid or (stage 1)
// capture-mode block is INERT — skipped with one session failure note per
// block (never a startup error), so a config typo degrades a menu instead of
// breaking the app.
func (m Model) toolCommands(category string) []config.ToolCommand {
	var out []config.ToolCommand
	for _, tc := range m.cfg.Tools.Command {
		if tc.Category != category {
			continue
		}
		if err := m.toolUsable(tc); err != nil {
			m.noteToolOnce(tc.Key(), err)
			continue
		}
		out = append(out, tc)
	}
	return out
}

// toolUsable is the full stage-1 usability check for one block.
func (m Model) toolUsable(tc config.ToolCommand) error {
	if err := config.ValidateToolCommand(tc); err != nil {
		return err
	}
	if tc.Mode == "capture" {
		return fmt.Errorf("tools: %s: mode \"capture\" is not supported yet (terminal only)", tc.Name)
	}
	return template.ValidateCommandTokens(tc.Command, tc.PerFile)
}

// noteToolOnce records one failure note per block per session (m.toolNoted is
// a map field, so it persists across the value-receiver copies).
func (m Model) noteToolOnce(key string, err error) {
	if m.toolNoted == nil || m.toolNoted[key] {
		return
	}
	m.toolNoted[key] = true
	observ.NoteFailure("tools", err)
}

// conflictToolChoices filters conflict commands for the paused op and the
// focused file: when_op must match (empty = any), and a per_file command
// needs a focused both-sides conflict to act on. Pure, for tests.
func conflictToolChoices(cmds []config.ToolCommand, op string, focused *model.FileStatus) []config.ToolCommand {
	var out []config.ToolCommand
	for _, tc := range cmds {
		if tc.WhenOp != "" && tc.WhenOp != op {
			continue
		}
		if tc.PerFile && (focused == nil || focused.ConflictClass() != model.ConflictBothSides) {
			continue
		}
		out = append(out, tc)
	}
	return out
}
```

`internal/tui/model.go` edits: add the field + init:

```go
	// near promptStore (~line 73):
	toolNoted map[string]bool // tool-config blocks already failure-noted this session

	// in New() (~line 255), beside promptStore:
	toolNoted: map[string]bool{},
```

`internal/tui/conflict_process.go` edits:

1. Extend the state enum (after `confReporting`):

```go
	confToolPick    // choosing an external tool command ([t])
	confToolFill    // collecting <user:…> values for the chosen command
	confToolApprove // first-run approval: showing the resolved command
	confToolMark    // per-file run changed the file: offer mark-resolved
```

2. Add fields to `conflictProcess`:

```go
	toolChoices []config.ToolCommand // picker rows while confToolPick
	toolSel     int
	toolFill    *templateFill   // <user:…> collection while confToolFill
	pending     *pendingToolRun // resolved run while confToolApprove/executing (Task 7)
```

(`config` import: `"github.com/homeend/gigagit/internal/config"`. Declare `pendingToolRun` as an empty struct placeholder in `tools.go` ONLY if Task 7 is not yet merged — Task 7 defines it fully; to keep this task compiling standalone, define it in this task as its Task-7 shape, see below Interfaces of Task 7, with the fields but no behavior.)

Define now (in `internal/tui/tools.go`) so both tasks compile:

```go
// pendingToolRun is a resolved, ready-to-execute tool command (built by the
// pick/fill flow; executed after approval in tool_run.go).
type pendingToolRun struct {
	tc       config.ToolCommand
	resolved string   // command with all tokens substituted
	env      []string // extra GG_* environment entries
	cleanup  []string // temp files to remove after the run (quartet)
	file     string   // per-file: repo-relative conflicted path
	merged   string   // per-file: absolute worktree path of the file
}
```

3. In `updateListing`, add the `t` case (before the per-file action fallthrough):

```go
	case "t": // run an external tool on the conflicts
		var focused *model.FileStatus
		if p.sel >= 0 && p.sel < len(p.files) {
			focused = &p.files[p.sel]
		}
		choices := conflictToolChoices(m.toolCommands("conflict"), p.src.Op, focused)
		if len(choices) == 0 {
			m.statusMsg = "no external tools configured — Settings (,) → External tools"
			return m, nil
		}
		p.toolChoices, p.toolSel = choices, 0
		p.st = confToolPick
		return m, nil
```

4. In `update`'s state switch, route the new states (Task 6 wires only `confToolPick`; fill/approve/mark bodies come in Task 7 — give them minimal esc-to-listing handlers now):

```go
	case confToolPick:
		return p.updateToolPick(m, msg)
	case confToolFill, confToolApprove, confToolMark:
		if msg.String() == "esc" {
			p.st = confListing
			return m, nil
		}
		return m, nil
```

and add:

```go
// updateToolPick drives the tool picker: ↑/↓ select, enter chooses, esc backs
// out to the file list. Choosing hands off to the fill/approve flow (Task 7's
// startToolRun); until then enter is a stub that returns to listing.
func (p *conflictProcess) updateToolPick(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		p.st = confListing
		return m, nil
	case "up", "k":
		if p.toolSel > 0 {
			p.toolSel--
		}
	case "down", "j":
		if p.toolSel < len(p.toolChoices)-1 {
			p.toolSel++
		}
	case "enter":
		return p.startToolRun(m) // Task 7; stub below until then
	}
	return m, nil
}
```

with a temporary stub (replaced in Task 7):

```go
// startToolRun begins the chosen command's fill→approve→execute flow (Task 7).
func (p *conflictProcess) startToolRun(m Model) (Model, tea.Cmd) {
	p.st = confListing
	m.statusMsg = "tool execution lands in the next task"
	return m, nil
}
```

5. Render: in `render`'s state switch add:

```go
	case confToolPick:
		return overlayCenter(bg, conflictToolPickBox(m, p.toolChoices, p.toolSel), w, h)
	case confToolFill, confToolApprove, confToolMark:
		return overlayCenter(bg, conflictMsgBox(m, "…"), w, h) // real boxes in Task 7
```

and add the picker box:

```go
// conflictToolPickBox draws the external-tool picker: one row per command,
// the command's first line dimmed beneath the selection hints.
func conflictToolPickBox(m Model, choices []config.ToolCommand, sel int) string {
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	textW := popupTextWidth(inner)
	var b strings.Builder
	b.WriteString("Run external tool\n\n")
	for i, tc := range choices {
		prefix, st := "  ", lipgloss.NewStyle()
		if i == sel {
			prefix, st = "> ", selectedRow
		}
		label := tc.Name
		if tc.PerFile {
			label += "  (this file)"
		}
		b.WriteString(st.Render(truncate(prefix+label, textW)) + "\n")
	}
	b.WriteString("\n[↑/↓] select  [enter] run  [esc] back")
	return popupBox(inner, b.String())
}
```

(`truncate` — if no such helper exists in the tui package, use `lipgloss` width-clipping the way `conflictListBox` handles rows via `renderWindow`, or simply skip truncation; check `grep -rn "func truncate" internal/tui/` and mirror what exists.)

6. Hints: change `conflictHints(files, sel, inProgress)` to `conflictHints(files []model.FileStatus, sel int, inProgress string, nTools int)`; append before `"[A] resolve all"`:

```go
	if nTools > 0 {
		parts = append(parts, "[t] tools")
	}
```

Update the caller in `conflictListBox` (which has `m`): `conflictHints(files, sel, inProgress, len(m.toolCommands("conflict")))` — `conflictListBox` already receives `m`. Update any existing `conflictHints` tests for the new parameter (pass `0` to preserve their expectations).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ 2>&1 | tail -5`
Expected: PASS (new tests + all pre-existing conflict tests still green).

- [ ] **Step 5: gofmt, vet, commit**

```bash
gofmt -l internal/tui && go vet ./internal/tui/
git add internal/tui
git commit -m "feat(tui): conflict-window external-tool picker (t) over validated [tools] commands"
```

---

### Task 7: TUI — fill → approve → execute → mark-resolved

**Files:**
- Create: `internal/tui/tool_run.go`
- Modify: `internal/tui/conflict_process.go` (replace the `startToolRun` stub; real fill/approve/mark handlers + render boxes)
- Modify: `internal/tui/model.go` (handle `toolFinishedMsg` + `toolReadyMsg` beside `editorFinishedMsg` ~line 2022)
- Test: extend `internal/tui/conflict_tools_test.go`; create pure-helper tests in `internal/tui/tool_run_test.go`

**Interfaces:**
- Consumes: `pendingToolRun` (Task 6), `template.ResolveCommand`/`CmdCtx` (Task 2), `svc.ConflictFileVersions` (Task 5), `m.promptStore.ApprovedToolCommands/ApproveToolCommand` (Task 4), `tea.ExecProcess` (editor precedent `edit_actions.go:100`), `engine.ResolveConflict{Path, Action: engine.MarkResolved}`.
- Produces:
  - `func toolCommandHash(command string) string` — `sha256` hex truncated to 16 chars
  - `func toolEnv(ctx template.CmdCtx) []string` — `GG_OP GG_SOURCE GG_TARGET GG_CONFLICTED_FILES GG_REPO GG_FILE GG_LOCAL GG_BASE GG_REMOTE GG_MERGED` (always all ten; empty values allowed)
  - `func toolScript(resolved string) (path string, err error)` — temp `gg-tool-*.sh`/`.bat`, `0o700`
  - `func toolExecCmd(script, dir string, extraEnv []string) *exec.Cmd` — `$SHELL`(default `/bin/sh`) + script, or `%COMSPEC%`(default `cmd`) `/C` script on Windows (mirror `engine.hookShellArgv`, `internal/engine/hook_runner.go:72`)
  - `type toolReadyMsg struct { pending *pendingToolRun; err error }` — quartet materialized
  - `type toolFinishedMsg struct { pending *pendingToolRun; script string; preMtime time.Time; err error }`
  - `func (m Model) toolRepoKey() string` — `m.repoHealth.GitCommonDir` when non-empty, else `m.currentWorktree`

- [ ] **Step 1: Write the failing pure-helper tests**

`internal/tui/tool_run_test.go`:

```go
package tui

import (
	"os"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/template"
)

func TestToolCommandHash(t *testing.T) {
	a, b := toolCommandHash("cmd one"), toolCommandHash("cmd two")
	if len(a) != 16 || a == b {
		t.Errorf("hash: %q vs %q", a, b)
	}
	if a != toolCommandHash("cmd one") {
		t.Error("hash must be deterministic")
	}
}

func TestToolEnv(t *testing.T) {
	env := toolEnv(template.CmdCtx{Op: "merge", Source: "f", Target: "main",
		Repo: "/r", ConflictedFiles: []string{"a.go", "b.go"},
		File: "a.go", Local: "/t/l", Base: "/t/b", Remote: "/t/r", Merged: "/r/a.go"})
	sort.Strings(env)
	want := []string{
		"GG_BASE=/t/b", "GG_CONFLICTED_FILES=a.go b.go", "GG_FILE=a.go",
		"GG_LOCAL=/t/l", "GG_MERGED=/r/a.go", "GG_OP=merge", "GG_REMOTE=/t/r",
		"GG_REPO=/r", "GG_SOURCE=f", "GG_TARGET=main",
	}
	if strings.Join(env, "|") != strings.Join(want, "|") {
		t.Errorf("env = %v\nwant %v", env, want)
	}
}

func TestToolScriptAndExecCmd(t *testing.T) {
	script, err := toolScript("echo hello")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(script)
	data, _ := os.ReadFile(script)
	if !strings.Contains(string(data), "echo hello") {
		t.Errorf("script content: %q", data)
	}
	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(script, ".bat") {
			t.Errorf("windows script must be .bat: %s", script)
		}
	} else if !strings.HasSuffix(script, ".sh") {
		t.Errorf("posix script must be .sh: %s", script)
	}
	cmd := toolExecCmd(script, "/tmp", []string{"GG_OP=merge"})
	if cmd.Dir != "/tmp" {
		t.Errorf("Dir = %q", cmd.Dir)
	}
	joined := strings.Join(cmd.Env, "|")
	if !strings.Contains(joined, "GG_OP=merge") {
		t.Error("extra env missing")
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), script) {
		t.Errorf("argv %v must reference the script", cmd.Args)
	}
}
```

Extend `internal/tui/conflict_tools_test.go`:

```go
func TestToolPickEnterResolvesAndAsksApproval(t *testing.T) {
	m, p := conflictModelWithTools(t,
		config.ToolCommand{Category: "conflict", Name: "Agent", Mode: "terminal", Command: `agent "<op> <conflicted-files>"`})
	m.currentWorktree = "/work/repo"
	m, _ = p.update(m, keyRunes("t"))
	m, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if p.st != confToolApprove {
		t.Fatalf("state = %v, want confToolApprove (repo-level command needs no quartet)", p.st)
	}
	if p.pending == nil || p.pending.resolved != `agent "merge a.go"` {
		t.Fatalf("pending = %+v", p.pending)
	}
	if cmd != nil {
		t.Error("no async work expected for a repo-level command")
	}
	// esc cancels without approving.
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if p.st != confListing || p.pending != nil {
		t.Errorf("esc must clear the pending run: st=%v pending=%v", p.st, p.pending)
	}
}

func TestToolApproveEnterReturnsExecCmd(t *testing.T) {
	m, p := conflictModelWithTools(t,
		config.ToolCommand{Category: "conflict", Name: "Agent", Mode: "terminal", Command: "true"})
	m, _ = p.update(m, keyRunes("t"))
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter}) // → approve
	m, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("approving must return the ExecProcess command")
	}
	_ = m
}

func TestToolUserFillStepPrecedesApproval(t *testing.T) {
	m, p := conflictModelWithTools(t,
		config.ToolCommand{Category: "conflict", Name: "Agent", Mode: "terminal", Command: "agent <user:hint>"})
	m, _ = p.update(m, keyRunes("t"))
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if p.st != confToolFill || p.toolFill == nil {
		t.Fatalf("state = %v, want confToolFill", p.st)
	}
	for _, r := range "go" {
		m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter}) // last field → done
	if p.st != confToolApprove || p.pending == nil || p.pending.resolved != "agent go" {
		t.Fatalf("after fill: st=%v pending=%+v", p.st, p.pending)
	}
	_ = m
}

func TestToolMarkResolvedOffer(t *testing.T) {
	// A finished per-file run whose merged file changed (mtime moved past the
	// snapshot) must offer mark-resolved; an unchanged one must reload instead.
	m, p := conflictModelWithTools(t,
		config.ToolCommand{Category: "conflict", Name: "Meld", Mode: "terminal", PerFile: true, Command: "true"})
	f := filepath.Join(t.TempDir(), "a.go")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(f)
	// preMtime deliberately BEFORE the file's mtime = "the tool wrote it".
	pre := fi.ModTime().Add(-2 * time.Second)
	pending := &pendingToolRun{tc: config.ToolCommand{PerFile: true}, file: "a.go", merged: f}
	m2, _ := p.toolFinished(m, toolFinishedMsg{pending: pending, preMtime: pre})
	if p.st != confToolMark {
		t.Fatalf("changed merged file must offer mark-resolved, got %v", p.st)
	}
	_ = m2

	// Unchanged file (preMtime after the mtime): no offer, reload command instead.
	m3, p3 := conflictModelWithTools(t,
		config.ToolCommand{Category: "conflict", Name: "Meld", Mode: "terminal", PerFile: true, Command: "true"})
	post := fi.ModTime().Add(2 * time.Second)
	_, cmd := p3.toolFinished(m3, toolFinishedMsg{pending: pending, preMtime: post})
	if p3.st == confToolMark {
		t.Fatal("unchanged merged file must not offer mark-resolved")
	}
	if cmd == nil {
		t.Error("unchanged path must reload status")
	}
}
```

(Imports for this test file: `os`, `path/filepath`, `time`, plus the existing ones.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run 'TestTool' 2>&1 | head -5`
Expected: build FAIL.

- [ ] **Step 3: Implement**

`internal/tui/tool_run.go`:

```go
package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/template"
)

// toolCommandHash keys first-run approval memory: the TEMPLATE text (not the
// per-run resolved values), so approving once covers every run until the
// config text changes.
func toolCommandHash(command string) string {
	sum := sha256.Sum256([]byte(command))
	return hex.EncodeToString(sum[:])[:16]
}

// toolRepoKey scopes approvals per repo: the git common dir when the repo
// health probe has resolved it (startup/reRoot), else the worktree path.
func (m Model) toolRepoKey() string {
	if m.repoHealth.GitCommonDir != "" {
		return m.repoHealth.GitCommonDir
	}
	return m.currentWorktree
}

// toolEnv renders the CmdCtx as GG_* env entries — the no-placeholder path
// for wrapper scripts (the post-create-hook pattern). All ten are always set.
func toolEnv(ctx template.CmdCtx) []string {
	return []string{
		"GG_OP=" + ctx.Op,
		"GG_SOURCE=" + ctx.Source,
		"GG_TARGET=" + ctx.Target,
		"GG_CONFLICTED_FILES=" + strings.Join(ctx.ConflictedFiles, " "),
		"GG_REPO=" + ctx.Repo,
		"GG_FILE=" + ctx.File,
		"GG_LOCAL=" + ctx.Local,
		"GG_BASE=" + ctx.Base,
		"GG_REMOTE=" + ctx.Remote,
		"GG_MERGED=" + ctx.Merged,
	}
}

// toolScript writes the resolved command to a temp script (0700) so the shell
// owns all quoting semantics — the same trick ShellHookRunner uses; a raw
// `sh -c`/`cmd /C` argv would re-open quoting problems on Windows.
func toolScript(resolved string) (string, error) {
	ext := ".sh"
	if runtime.GOOS == "windows" {
		ext = ".bat"
	}
	f, err := os.CreateTemp("", "gg-tool-*"+ext)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(resolved + "\n"); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	if err := os.Chmod(f.Name(), 0o700); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// toolExecCmd builds the interpreter invocation for the script (mirrors
// engine.hookShellArgv): $SHELL (default /bin/sh), or %COMSPEC% /C on Windows.
func toolExecCmd(script, dir string, extraEnv []string) *exec.Cmd {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		comspec := os.Getenv("COMSPEC")
		if comspec == "" {
			comspec = "cmd"
		}
		cmd = exec.Command(comspec, "/C", script)
	} else {
		sh := os.Getenv("SHELL")
		if sh == "" {
			sh = "/bin/sh"
		}
		cmd = exec.Command(sh, script)
	}
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	return cmd
}

// toolReadyMsg delivers an async-built pendingToolRun (per-file quartet).
type toolReadyMsg struct {
	pending *pendingToolRun
	err     error
}

// toolFinishedMsg signals the handed-over tool process exited.
type toolFinishedMsg struct {
	pending  *pendingToolRun
	script   string
	preMtime time.Time
	err      error
}

// execToolCmd suspends the TUI and runs the pending command with the real
// terminal (the editor-handover precedent). preMtime snapshots the per-file
// target so the return path can offer mark-resolved only on a real change.
func (m Model) execToolCmd(pending *pendingToolRun) tea.Cmd {
	script, err := toolScript(pending.resolved)
	if err != nil {
		return func() tea.Msg { return toolFinishedMsg{pending: pending, err: err} }
	}
	var preMtime time.Time
	if pending.merged != "" {
		if fi, err := os.Stat(pending.merged); err == nil {
			preMtime = fi.ModTime()
		}
	}
	cmd := toolExecCmd(script, m.currentWorktree, pending.env)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return toolFinishedMsg{pending: pending, script: script, preMtime: preMtime, err: err}
	})
}
```

`internal/tui/conflict_process.go` — replace the Task 6 stub and minimal handlers:

```go
// startToolRun resolves the chosen command's context. A command with <user:…>
// tokens collects them first; a per-file command materializes the quartet
// asynchronously; everything else goes straight to the approval gate.
func (p *conflictProcess) startToolRun(m Model) (Model, tea.Cmd) {
	if p.toolSel < 0 || p.toolSel >= len(p.toolChoices) {
		return m, nil
	}
	tc := p.toolChoices[p.toolSel]
	fill := newTemplateFill(tc.Command)
	if fill.needsInput() {
		p.toolFill = &fill
		p.st = confToolFill
		return m, nil
	}
	return p.buildToolRun(m, tc, map[string]string{})
}

// buildToolRun assembles the CmdCtx and pendingToolRun for tc; per-file
// commands go async through ConflictFileVersions (confWorking meanwhile).
func (p *conflictProcess) buildToolRun(m Model, tc config.ToolCommand, inputs map[string]string) (Model, tea.Cmd) {
	ctx := template.CmdCtx{
		Op: p.src.Op, Source: p.src.Source, Target: p.src.Target,
		Repo: m.currentWorktree,
	}
	for _, f := range p.files {
		ctx.ConflictedFiles = append(ctx.ConflictedFiles, f.Path)
	}
	if !tc.PerFile {
		resolved, err := template.ResolveCommand(tc.Command, inputs, ctx)
		if err != nil {
			p.st = confReporting
			p.errMsg = err.Error()
			return m, nil
		}
		p.pending = &pendingToolRun{tc: tc, resolved: resolved, env: toolEnv(ctx)}
		return p.gateOrRun(m)
	}
	// Per-file: quartet first (async), then resolve in the toolReadyMsg handler.
	f := p.files[p.sel]
	ctx.File = f.Path
	ctx.Merged = filepath.Join(m.currentWorktree, f.Path)
	p.st = confWorking
	svc, hasBase, path := m.svc, f.ConflictHasBase(), f.Path
	return m, func() tea.Msg {
		local, base, remote, cleanup, err := svc.ConflictFileVersions(context.Background(), path, hasBase)
		if err != nil {
			return toolReadyMsg{err: err}
		}
		_ = cleanup // temp paths recorded on the pending run; removed on finish
		ctx.Local, ctx.Base, ctx.Remote = local, base, remote
		resolved, rerr := template.ResolveCommand(tc.Command, inputs, ctx)
		if rerr != nil {
			os.Remove(local)
			os.Remove(base)
			os.Remove(remote)
			return toolReadyMsg{err: rerr}
		}
		return toolReadyMsg{pending: &pendingToolRun{
			tc: tc, resolved: resolved, env: toolEnv(ctx),
			cleanup: []string{local, base, remote},
			file:    path, merged: ctx.Merged,
		}}
	}
}

// gateOrRun applies the first-run approval gate: an already-approved command
// (per repo, by template hash) runs immediately; otherwise the approval box
// shows the exact resolved command first.
func (p *conflictProcess) gateOrRun(m Model) (Model, tea.Cmd) {
	hash := toolCommandHash(p.pending.tc.Command)
	if m.promptStore != nil && m.promptStore.ApprovedToolCommands(m.toolRepoKey())[hash] {
		return p.runPending(m)
	}
	p.st = confToolApprove
	return m, nil
}

// runPending hands the terminal to the pending command.
func (p *conflictProcess) runPending(m Model) (Model, tea.Cmd) {
	pending := p.pending
	p.pending = nil
	p.st = confWorking
	return m, m.execToolCmd(pending)
}

// updateToolApprove: enter approves (persisted) and runs; esc cancels.
func (p *conflictProcess) updateToolApprove(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		p.cleanupPending()
		p.st = confListing
		return m, nil
	case "enter":
		if m.promptStore != nil {
			_ = m.promptStore.ApproveToolCommand(m.toolRepoKey(), toolCommandHash(p.pending.tc.Command))
		}
		return p.runPending(m)
	}
	return m, nil
}

// updateToolFill collects <user:…> values, then proceeds like startToolRun.
func (p *conflictProcess) updateToolFill(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	done, cancel := p.toolFill.handleKey(msg)
	if cancel {
		p.toolFill = nil
		p.st = confListing
		return m, nil
	}
	if done {
		inputs := p.toolFill.inputs()
		p.toolFill = nil
		return p.buildToolRun(m, p.toolChoices[p.toolSel], inputs)
	}
	return m, nil
}

// updateToolMark: the per-file tool changed the file — offer to mark resolved.
func (p *conflictProcess) updateToolMark(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		path := p.pending.file
		p.pending = nil
		p.st = confWorking
		return m.startOp(engine.ResolveConflict{Path: path, Action: engine.MarkResolved})
	case "n", "esc":
		p.pending = nil
		p.st = confWorking
		return m, m.loadCmd()
	}
	return m, nil
}

// cleanupPending removes a cancelled run's quartet temp files.
func (p *conflictProcess) cleanupPending() {
	if p.pending == nil {
		return
	}
	for _, f := range p.pending.cleanup {
		os.Remove(f)
	}
	p.pending = nil
}

// toolReady receives the async per-file build.
func (p *conflictProcess) toolReady(m Model, msg toolReadyMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		p.st = confReporting
		p.errMsg = msg.err.Error()
		return m, nil
	}
	p.pending = msg.pending
	return p.gateOrRun(m)
}

// toolFinished receives the handed-over process's exit: clean temps, surface
// a failure, offer mark-resolved when a per-file run changed its file, and
// reload so the conflict list re-derives.
func (p *conflictProcess) toolFinished(m Model, msg toolFinishedMsg) (Model, tea.Cmd) {
	if msg.script != "" {
		os.Remove(msg.script)
	}
	changed := false
	if msg.pending != nil && msg.pending.merged != "" {
		if fi, err := os.Stat(msg.pending.merged); err == nil && fi.ModTime().After(msg.preMtime) {
			changed = true
		}
	}
	if msg.pending != nil {
		for _, f := range msg.pending.cleanup {
			os.Remove(f)
		}
	}
	if msg.err != nil {
		p.st = confReporting
		p.errMsg = "tool exited with an error: " + msg.err.Error()
		return m, nil
	}
	if msg.pending != nil && msg.pending.tc.PerFile && changed {
		p.pending = msg.pending
		p.st = confToolMark
		return m, nil
	}
	p.st = confWorking
	return m, m.loadCmd()
}
```

Update the `update` state routing (replace Task 6's minimal handlers):

```go
	case confToolFill:
		return p.updateToolFill(m, msg)
	case confToolApprove:
		return p.updateToolApprove(m, msg)
	case confToolMark:
		return p.updateToolMark(m, msg)
```

Render boxes (replace Task 6's placeholder case):

```go
	case confToolFill:
		var b strings.Builder
		b.WriteString("Tool inputs\n\n")
		for _, line := range p.toolFill.view(popupContentWidth(w)) {
			b.WriteString(line + "\n")
		}
		b.WriteString("\n[tab/enter] next  [esc] cancel")
		return overlayCenter(bg, popupBox(popupInnerWidth(w), b.String()), w, h)
	case confToolApprove:
		var b strings.Builder
		b.WriteString("Run this command?  (" + p.pending.tc.Name + ")\n\n")
		b.WriteString(p.pending.resolved + "\n\n")
		b.WriteString("Approval is remembered for this repo until the command text changes.\n")
		b.WriteString("[enter] run  [esc] cancel")
		return overlayCenter(bg, popupBox(popupInnerWidth(w), b.String()), w, h)
	case confToolMark:
		msg := "The tool changed " + p.pending.file + ".\n\nMark it as resolved (git add)?\n\n[y/enter] mark resolved  [n/esc] not now"
		return overlayCenter(bg, popupBox(popupInnerWidth(w), msg), w, h)
```

(Imports: `conflict_process.go` gains `os`, `path/filepath`, `"github.com/homeend/gigagit/internal/template"`, `"github.com/homeend/gigagit/internal/config"`.)

`internal/tui/model.go` — beside the `editorFinishedMsg` case (~line 2022):

```go
	case toolReadyMsg:
		if cp, ok := m.proc.(*conflictProcess); ok {
			return cp.toolReady(m, msg)
		}
		return m, nil
	case toolFinishedMsg:
		if cp, ok := m.proc.(*conflictProcess); ok {
			return cp.toolFinished(m, msg)
		}
		// Process gone (shouldn't happen): still clean up and resync.
		if msg.script != "" {
			os.Remove(msg.script)
		}
		if msg.pending != nil {
			for _, f := range msg.pending.cleanup {
				os.Remove(f)
			}
		}
		return m, m.loadCmd()
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 5: gofmt, vet, commit**

```bash
gofmt -l internal/tui && go vet ./internal/tui/
git add internal/tui
git commit -m "feat(tui): tool run flow — user-fill, first-run approval, terminal handover, mark-resolved offer"
```

---

### Task 8: Settings — "External tools" detect wizard

**Files:**
- Modify: `internal/tui/settings_popup.go` (menu const/order line 35-52; fields line 20-33; enter dispatch line 336; view — mirror the agents picker)
- Test: `internal/tui/settings_tools_test.go`

**Interfaces:**
- Consumes: `exttool.Detect/Builtins/GenerateCommand/Detection` (Task 1), `config.AppendToolCommands/ToolCommand/DefaultGlobalPath/Load` (Task 3), agents-picker patterns in the same file.
- Produces:
  - `settingsMenuTools = "External tools"` inserted into `settingsMenu` right after `settingsMenuAgents`
  - `settingsPopup` fields: `toolsView bool`, `toolRows []toolWizardRow`, `toolChecked []bool` (reuses `p.sel`)
  - `type toolWizardRow struct { det exttool.Detection; tmpl exttool.CommandTemplate; existing bool }`
  - `func (m Model) openToolsWizard() Model`
  - `func (m Model) applyToolsWizard(rows []toolWizardRow, checked []bool, globalPath string) (Model, int, error)` — returns rows written (path injectable for tests)

- [ ] **Step 1: Write the failing test**

`internal/tui/settings_tools_test.go`:

```go
package tui

import (
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/exttool"
)

func wizardRows(existing map[string]bool) []toolWizardRow {
	var rows []toolWizardRow
	for _, tl := range exttool.Builtins() {
		for _, ct := range tl.Commands {
			rows = append(rows, toolWizardRow{
				det:      exttool.Detection{Tool: tl, Bin: tl.Bins[0]},
				tmpl:     ct,
				existing: existing[string(ct.Category)+"\x00"+ct.Name],
			})
		}
	}
	return rows
}

func TestApplyToolsWizardWritesMissingOnly(t *testing.T) {
	m := toolCfg() // empty tools config
	path := filepath.Join(t.TempDir(), "config.toml")
	rows := wizardRows(map[string]bool{"conflict\x00Claude": true}) // Claude pre-existing
	checked := make([]bool, len(rows))
	for i := range checked {
		checked[i] = true
	}
	m2, n, err := m.applyToolsWizard(rows, checked, path)
	if err != nil {
		t.Fatal(err)
	}
	wantWritten := len(rows) - 1 // all but the existing Claude row
	if n != wantWritten {
		t.Errorf("wrote %d rows, want %d", n, wantWritten)
	}
	cfg, err := config.Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tools.Command) != wantWritten {
		t.Errorf("config has %d commands, want %d", len(cfg.Tools.Command), wantWritten)
	}
	for _, tc := range cfg.Tools.Command {
		if tc.Name == "Claude" {
			t.Error("existing Claude block must be skipped, not rewritten")
		}
		if tc.Command == "" || tc.Category != "conflict" {
			t.Errorf("generated block malformed: %+v", tc)
		}
	}
	// The in-memory config was reloaded onto the model.
	if len(m2.cfg.Tools.Command) != wantWritten {
		t.Errorf("model cfg not refreshed: %d", len(m2.cfg.Tools.Command))
	}
	// Generated commands must not contain <bin>.
	for _, tc := range m2.cfg.Tools.Command {
		if contains := tc.Command; contains != "" && stringsContains(contains, "<bin>") {
			t.Errorf("<bin> leaked into generated command: %q", tc.Command)
		}
	}
}

func TestApplyToolsWizardUncheckedSkipped(t *testing.T) {
	m := toolCfg()
	path := filepath.Join(t.TempDir(), "config.toml")
	rows := wizardRows(nil)
	checked := make([]bool, len(rows)) // all false
	_, n, err := m.applyToolsWizard(rows, checked, path)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("wrote %d rows, want 0", n)
	}
}
```

(`stringsContains` = `strings.Contains` — import `strings` and call it directly; the wrapper name above is illustrative, use the real call.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run TestApplyToolsWizard 2>&1 | head -3`
Expected: build FAIL.

- [ ] **Step 3: Implement**

In `internal/tui/settings_popup.go`:

1. Add the const + menu order (after `settingsMenuAgents` in both):

```go
	settingsMenuTools       = "External tools"
```

```go
var settingsMenu = []string{settingsMenuAgents, settingsMenuTools, settingsMenuIdentity, /* …unchanged tail… */}
```

2. Add fields to `settingsPopup`:

```go
	toolsView   bool            // true = external-tools wizard screen
	toolRows    []toolWizardRow // detected tool × catalog command rows
	toolChecked []bool
```

3. The wizard row type + open/apply (same file or a new `settings_tools.go` — same package either way; prefer `settings_tools.go` to keep `settings_popup.go` from growing):

```go
// toolWizardRow is one detected tool × catalog command template pairing shown
// in the External-tools wizard.
type toolWizardRow struct {
	det      exttool.Detection
	tmpl     exttool.CommandTemplate
	existing bool // a (category,name) block already in config — shown, never rewritten
}

// openToolsWizard detects installed catalog tools and builds the wizard rows.
// New rows default checked (opening the wizard signals intent to add);
// existing rows show checked but are always skipped on apply.
func (m Model) openToolsWizard() Model {
	p := layerOf[*settingsPopup](m)
	have := map[string]bool{}
	for _, tc := range m.cfg.Tools.Command {
		have[tc.Key()] = true
	}
	p.toolRows = nil
	for _, det := range exttool.Detect(exec.LookPath, os.Stat) {
		for _, ct := range det.Tool.Commands {
			key := string(ct.Category) + "\x00" + ct.Name
			p.toolRows = append(p.toolRows, toolWizardRow{det: det, tmpl: ct, existing: have[key]})
		}
	}
	p.toolChecked = make([]bool, len(p.toolRows))
	for i := range p.toolChecked {
		p.toolChecked[i] = true
	}
	p.sel = 0
	p.toolsView = true
	return m
}

// applyToolsWizard appends the checked, not-yet-configured rows to the config
// file at globalPath and reloads the effective config. Returns the number of
// blocks written. globalPath is a parameter so tests never touch the real
// global config.
func (m Model) applyToolsWizard(rows []toolWizardRow, checked []bool, globalPath string) (Model, int, error) {
	var blocks []config.ToolCommand
	for i, row := range rows {
		if i >= len(checked) || !checked[i] || row.existing {
			continue
		}
		blocks = append(blocks, config.ToolCommand{
			Category: string(row.tmpl.Category),
			Name:     row.tmpl.Name,
			Mode:     string(row.tmpl.Mode),
			PerFile:  row.tmpl.PerFile,
			WhenOp:   row.tmpl.WhenOp,
			Command:  exttool.GenerateCommand(row.tmpl, row.det.Bin),
		})
	}
	if len(blocks) == 0 {
		return m, 0, nil
	}
	if err := config.AppendToolCommands(globalPath, blocks); err != nil {
		return m, 0, err
	}
	if cfg, err := config.Load(globalPath, m.repoConfigPath); err == nil {
		m.cfg = cfg
	}
	return m, len(blocks), nil
}
```

4. Wire the enter dispatch (in the menu `case tea.KeyEnter` switch, after `settingsMenuAgents`):

```go
			case settingsMenuTools:
				return m.openToolsWizard(), nil
```

5. Wire the wizard screen's keys: in `update`, the esc handler gains (beside the `p.picker` esc):

```go
		if p.toolsView {
			p.toolsView = false
			return m, nil
		}
```

and the picker-key block at the bottom of `update` must branch: when `p.toolsView` is true, up/down/space toggle over `p.toolRows`/`p.toolChecked` (mirror the agents picker exactly), and enter applies:

```go
	case tea.KeyEnter:
		m2, n, err := m.applyToolsWizard(p.toolRows, p.toolChecked, config.DefaultGlobalPath())
		p.toolsView = false
		if err != nil {
			m2.statusMsg = "external tools: " + err.Error()
			return m2, nil
		}
		if n == 0 {
			m2.statusMsg = "external tools: nothing to write (already configured or unchecked)"
			return m2, nil
		}
		m2.statusMsg = fmt.Sprintf("external tools: %d command(s) written to %s", n, config.DefaultGlobalPath())
		return m2, nil
```

Route this correctly: the current bottom block is guarded by the agents picker being open; give the toolsView its own handler before it (mirror how `errorsView`/`ratesView` branch early).

6. Render: add a `toolsView` screen to the settings `render` (find the agents-picker rendering and mirror it): title "External tools — detected", one row per `toolRows`: `[x] <Tool.Label> — <category>: <Name>` plus a dim ` (configured)` suffix when `row.existing`; hint line `"[space] toggle  [enter] write to global config  [esc] back"`; when `len(p.toolRows) == 0` render `"no known tools detected on this machine (looked for: claude, junie, meld)"`.

(Imports gained by the tui files: `os/exec`, `os`, `"github.com/homeend/gigagit/internal/exttool"`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 5: gofmt, vet, commit**

```bash
gofmt -l internal/tui && go vet ./internal/tui/
git add internal/tui
git commit -m "feat(tui): Settings External-tools wizard — detect catalog tools, write default commands to global config"
```

---

### Task 9: Docs, help, full suite

**Files:**
- Modify: `CHANGELOG.md` (top of Unreleased/Added), `README.md` (user surface), `CLAUDE.md` (package map), `internal/tui/help.go` (if a help/cheat-sheet registry exists — check `grep -n "conflict" internal/tui/help.go`)

- [ ] **Step 1: Help entry**

If `internal/tui/help.go` lists conflict-window keys, add `t — run external tool (Claude/Junie/Meld…)` beside the other conflict keys. (The in-window hint line already advertises it from Task 6; the memory rule is help AND the window's own hint/footer.)

- [ ] **Step 2: CHANGELOG.md**

Add at the top of the Unreleased Added section:

```markdown
- External tools (stage 1: conflicts): run a configured agent or mergetool on a
  paused merge/rebase/cherry-pick/revert from the conflict window (`t`) —
  repo-level agents (Claude Code, Junie) get a task prompt built from gg's
  conflict state and hand over the terminal; per-file tools (Meld) get the
  LOCAL/BASE/REMOTE/MERGED quartet and an after-run mark-resolved offer.
  Commands live in `[[tools.command]]` config blocks (global+repo lists
  concatenate, repo wins name collisions); Settings → "External tools" detects
  installed tools and writes editable defaults to the global config; the first
  run of each command shows it for approval (remembered per repo until the
  text changes).
```

- [ ] **Step 3: README.md**

Add a short "External tools" subsection under the features/TUI section: the `t` key in the conflict window, the Settings wizard, a 6-line `[[tools.command]]` example (copy the Meld block from the spec), and the token list one-liner.

- [ ] **Step 4: CLAUDE.md package map**

Add an `exttool` row to the package table:

```markdown
| `exttool`    | Hardcoded external-tool catalog (Claude Code, Junie, Meld): per-category default command templates (`<bin>` + runtime tokens) and injected-probe detection (`Detect(look, stat)`). Leaf; the TUI imports it directly. The Settings "External tools" wizard materializes templates as `[[tools.command]]` config blocks via `GenerateCommand`; only config content executes. |
```

and extend the `config` row (one sentence): `[tools]` section — `[[tools.command]]` blocks; lists CONCATENATE across global+repo with repo winning `(category,name)` collisions; `AppendToolCommands` append-only writer. Extend the `tui` row (one sentence): conflict window `t` = external-tool picker (process-owned sub-states; first-run approval via promptstate; terminal handover via `tea.ExecProcess` running a temp script). Extend the `promptstate` row: + per-repo approved tool-command hashes. Extend the `domain` row: `ConflictFileVersions(ctx, path, hasBase)` — index-stage quartet temp files. Extend the `template` row: command-context resolver (`ResolveCommand`/`CmdCtx`, per-kind quoting).

- [ ] **Step 5: Full suite + race**

```bash
./test.sh 2>&1 | tail -15        # staged: vet+gofmt → unit → e2e
./test.sh race 2>&1 | tail -5    # before merge
```
Expected: all green. (No e2e scenario is added in stage 1 — the wizard is TUI-only and the config writer is unit-covered; `gg config populate` picks up `[tools]` from settingDocs and its existing e2e keeps passing.)

- [ ] **Step 6: Commit**

```bash
git add CHANGELOG.md README.md CLAUDE.md internal/tui
git commit -m "docs: external tools stage 1 — changelog, readme, package map, help entry"
```

---

## Post-merge manual checklist (human, not CI)

1. Real conflicted merge + `t` → Claude: terminal handover, agent stages fixes, gg's resume prompt offers Continue on return.
2. Junie: verify `--merge <source>` picks up the already-paused merge; if it starts a NEW merge instead, switch the catalog default to the spec's `--prompt` fallback text (one-line change in `exttool.go` + `TestBuiltins…` update) BEFORE merge.
3. Meld: per-file quartet opens, saving in Meld triggers the mark-resolved offer.
4. Windows (or WSL→Windows binaries): the `.bat` + `cmd /C` path and Meld's `ExtraProbes` detection.
