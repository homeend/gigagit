# Web AI Conflict Lane Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run a headless `conflict_complete` agent from the web UI's paused-operation banner: it resolves conflicts, stages, runs the matching `--continue`, and its overview opens in the report viewer.

**Architecture:** A new `frontends` visibility tag on tool commands keeps the TUI picker unchanged while web-only headless catalog rows appear; a new engine capture op (`CompleteConflict`, `ReviewChanges` sibling) runs the agent; two web endpoints mirror the review lane's chooser→approval→run transport; the client reuses the `#review` overlay in a conflict mode.

**Tech Stack:** Go 1.26, real-git tests in `t.TempDir()`, vanilla JS/CSS web client, headless-CDP browser verification.

Spec: `docs/superpowers/specs/2026-08-01-web-ai-conflict-lane-design.md` (approved).

## Global Constraints

- Branch `feat/web-ai-conflict-lane` (off `web-dev`), worktree `/mnt/t/others/gigagit.worktrees/feat-web-ai-conflict-lane`. EVERY build/test command is prefixed `cd /mnt/t/others/gigagit.worktrees/feat-web-ai-conflict-lane && `.
- The command text NEVER comes off the wire: the web client sends a tool NAME; the server resolves the command from the effective config.
- Approval store: `promptstate.ApproveToolCommand`, key `promptstate.CommandHash(tc.Command)` (the config TEMPLATE text), scoped by git common dir — shared with the TUI, unchanged.
- `frontends` allowed values are exactly `"tui"`, `"web"`, `"cli"`; empty list = visible everywhere (backward compatible). Unknown value → block inert via `ValidateToolCommand`.
- OptIn invariant: OptIn ⇔ the command carries a permission-bypass flag.
- Engine op `LockMode()` is `repogate.Read` (the AGENT mutates, not gg; web reads stay alive during the run).
- Engine prose: every new `Result` summary is built via the `msg.go` lockstep helpers (`WithSummary`) with an English literal format, and that format string gets an entry in ALL FOUR bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`) — `internal/tui/engine_prose_test.go` enforces this.
- English protocol values everywhere on the wire (`op`, tool names, decision options).
- TDD: write the failing test first for every behavior change. Frequent commits.
- Never push; never merge to `web-dev` — the controller owns the merge after the final review and race gate.

---

### Task 1: `frontends` field on tool commands (config)

**Files:**
- Modify: `internal/config/tools.go`
- Test: `internal/config/tools_test.go`

**Interfaces:**
- Produces: `ToolCommand.Frontends []string` (toml `frontends`); `config.ToolVisibleIn(tc ToolCommand, frontend string) bool`; `ValidateToolCommand` rejects unknown values; `AppendToolCommands` writes a `frontends = ["web"]` line when non-empty.

- [ ] **Step 1: Write the failing tests** (append to `internal/config/tools_test.go`)

```go
func TestToolCommandFrontends(t *testing.T) {
	tc := ToolCommand{Category: "conflict_complete", Name: "X", Mode: "capture", Command: "x"}
	if err := ValidateToolCommand(tc); err != nil {
		t.Fatalf("no frontends should validate: %v", err)
	}
	tc.Frontends = []string{"tui", "web", "cli"}
	if err := ValidateToolCommand(tc); err != nil {
		t.Fatalf("known frontends should validate: %v", err)
	}
	tc.Frontends = []string{"gui"}
	if err := ValidateToolCommand(tc); err == nil {
		t.Fatal("unknown frontend value must be rejected")
	}
}

func TestToolVisibleIn(t *testing.T) {
	empty := ToolCommand{}
	for _, f := range []string{"tui", "web", "cli"} {
		if !ToolVisibleIn(empty, f) {
			t.Errorf("empty Frontends must be visible in %s", f)
		}
	}
	webOnly := ToolCommand{Frontends: []string{"web"}}
	if ToolVisibleIn(webOnly, "tui") || !ToolVisibleIn(webOnly, "web") {
		t.Error("web-only row: hidden from tui, visible in web")
	}
}

func TestAppendToolCommandsWritesFrontends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gg.toml")
	err := AppendToolCommands(path, []ToolCommand{{
		Category: "conflict_complete", Name: "X", Mode: "capture",
		Frontends: []string{"web"}, Command: "echo hi",
	}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "frontends = [\"web\"]\n") {
		t.Fatalf("frontends line missing:\n%s", raw)
	}
	// Round-trip: the written file parses back with the field intact.
	cfg, err := Load("", path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tools.Command) != 1 || len(cfg.Tools.Command[0].Frontends) != 1 || cfg.Tools.Command[0].Frontends[0] != "web" {
		t.Fatalf("round-trip lost frontends: %+v", cfg.Tools.Command)
	}
}
```

Also assert the no-frontends case writes NO `frontends` line (extend `TestAppendToolCommandsWritesFrontends` or the existing append test): `strings.Contains` must be false for a command with empty `Frontends`.

- [ ] **Step 2: Run to verify failure**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-ai-conflict-lane && go test ./internal/config/ -run 'TestToolCommandFrontends|TestToolVisibleIn|TestAppendToolCommandsWritesFrontends' -v`
Expected: compile FAIL (`Frontends` / `ToolVisibleIn` undefined).

- [ ] **Step 3: Implement** in `internal/config/tools.go`:

Add to `ToolCommand` (after `WhenOp`):

```go
	// Frontends limits which frontends OFFER this command: any of
	// "tui", "web", "cli". Empty = everywhere (every pre-tag config keeps
	// behaving as before). A mode a frontend cannot run (terminal in the
	// web) is excluded by the mode gate regardless of this tag.
	Frontends []string `toml:"frontends"`
```

Add to `ValidateToolCommand` (before the final return):

```go
	for _, f := range tc.Frontends {
		switch f {
		case "tui", "web", "cli":
		default:
			return fmt.Errorf("tools: %s: unknown frontend %q (want tui|web|cli)", tc.Name, f)
		}
	}
```

Add the helper:

```go
// ToolVisibleIn reports whether frontend ("tui"|"web"|"cli") offers tc.
// Empty Frontends means everywhere.
func ToolVisibleIn(tc ToolCommand, frontend string) bool {
	if len(tc.Frontends) == 0 {
		return true
	}
	for _, f := range tc.Frontends {
		if f == frontend {
			return true
		}
	}
	return false
}
```

In `AppendToolCommands`, after the `when_op` line:

```go
		if len(tc.Frontends) > 0 {
			quoted := make([]string, len(tc.Frontends))
			for i, f := range tc.Frontends {
				quoted[i] = fmt.Sprintf("%q", f)
			}
			fmt.Fprintf(&b, "frontends = [%s]\n", strings.Join(quoted, ", "))
		}
```

- [ ] **Step 4: Run tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-ai-conflict-lane && go test ./internal/config/ -v -run Tool`
Expected: PASS (including all pre-existing tool tests).

- [ ] **Step 5: Commit**

```bash
git add internal/config/tools.go internal/config/tools_test.go
git commit -m "feat(config): frontends visibility tag on tool commands"
```

---

### Task 2: shared conflict-context helpers in `template`

**Files:**
- Create: `internal/template/contextdoc.go`, `internal/template/contextdoc_test.go`
- Modify: `internal/tui/tool_run.go` (delegate `cQuotePath` + `toolContextFile` body)

**Interfaces:**
- Produces: `template.CQuotePath(p string) string` and `template.ConflictContextDoc(op, source, target string, files []string) string` — byte-identical output to the TUI's current `cQuotePath`/`toolContextFile` content. Task 5's engine op consumes both.

- [ ] **Step 1: Write the failing test** (`internal/template/contextdoc_test.go`)

```go
package template

import "testing"

func TestCQuotePath(t *testing.T) {
	if got := CQuotePath("plain/path.txt"); got != "plain/path.txt" {
		t.Errorf("clean path must be byte-exact, got %q", got)
	}
	if got := CQuotePath("a\nb"); got != `"a\nb"` {
		t.Errorf("newline path: got %q", got)
	}
	if got := CQuotePath("a\x01b"); got != `"a\001b"` {
		t.Errorf("control byte: got %q", got)
	}
	if got := CQuotePath(`a"b\c`); got != `a"b\c` {
		t.Errorf("quote/backslash WITHOUT control bytes stays unquoted, got %q", got)
	}
}

func TestConflictContextDoc(t *testing.T) {
	got := ConflictContextDoc("merge", "feat/x", "main", []string{"a.txt", "b\nc.txt"})
	want := "op: merge\nsource: feat/x\ntarget: main\nconflicted:\na.txt\n\"b\\nc.txt\"\n"
	if got != want {
		t.Errorf("doc mismatch:\n got %q\nwant %q", got, want)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-ai-conflict-lane && go test ./internal/template/ -run 'TestCQuotePath|TestConflictContextDoc' -v`
Expected: compile FAIL (undefined).

- [ ] **Step 3: Implement** `internal/template/contextdoc.go` — MOVE the logic from `internal/tui/tool_run.go`:

```go
package template

import (
	"fmt"
	"strings"
)

// CQuotePath renders p the way git prints a path containing control
// characters: double-quoted, with \n \r \t \" \\ as their usual C escapes and
// every other control byte (< 0x20) as a \NNN octal escape. A path with no
// control bytes is returned byte-exact and unquoted. Byte-wise, not rune-wise:
// UTF-8 continuation/lead bytes are >= 0x80 and can never look like controls.
// (Moved from internal/tui so the engine's CompleteConflict op and the TUI's
// context-file writer share one implementation.)
func CQuotePath(p string) string {
	// ... the exact body of internal/tui/tool_run.go's cQuotePath ...
}

// ConflictContextDoc renders the per-run context file body handed to a
// conflict agent: op/source/target header lines then the conflicted paths one
// per line, each value C-quoted only when it carries a control byte, so no
// value can forge an extra line. Both the TUI's tool runs and the engine's
// CompleteConflict op write exactly these bytes.
func ConflictContextDoc(op, source, target string, files []string) string {
	var b strings.Builder
	b.WriteString("op: " + CQuotePath(op) + "\n")
	b.WriteString("source: " + CQuotePath(source) + "\n")
	b.WriteString("target: " + CQuotePath(target) + "\n")
	b.WriteString("conflicted:\n")
	for _, f := range files {
		b.WriteString(CQuotePath(f) + "\n")
	}
	return b.String()
}
```

(Copy the `cQuotePath` body verbatim from `internal/tui/tool_run.go:65-101`; keep the `fmt` import for the octal branch.)

In `internal/tui/tool_run.go`: replace `cQuotePath`'s body with `return template.CQuotePath(p)` (keep the function so call sites don't churn), and replace `toolContextFile`'s string-building section with `b := template.ConflictContextDoc(ctx.Op, ctx.Source, ctx.Target, ctx.ConflictedFiles)` feeding the existing temp-file write. Delete the now-unused local imports if any.

- [ ] **Step 4: Run tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-ai-conflict-lane && go test ./internal/template/ ./internal/tui/ 2>&1 | tail -5`
Expected: PASS — the TUI's existing context-file/quoting tests prove byte-identity.

- [ ] **Step 5: Commit**

```bash
git add internal/template/contextdoc.go internal/template/contextdoc_test.go internal/tui/tool_run.go
git commit -m "refactor(template): shared CQuotePath + ConflictContextDoc for conflict agents"
```

---

### Task 3: exttool catalog — `Frontends` on templates + headless rows

**Files:**
- Modify: `internal/exttool/exttool.go`
- Modify: the wizard's template→ToolCommand materialization (grep `internal/tui/settings_tools.go` for where `config.ToolCommand{` is built from a `CommandTemplate`; add `Frontends: tmpl.Frontends`)
- Test: `internal/exttool/exttool_test.go`

**Interfaces:**
- Produces: `CommandTemplate.Frontends []string`; three new catalog rows (names below) consumed by Task 7's web filter; existing `conflict_complete` rows tagged.

- [ ] **Step 1: Real-binary flag verification (record findings in code comments, dated 2026-08-01):**

```bash
claude --help 2>&1 | grep -E '^\s+-p|--dangerously-skip-permissions'
codex exec --help 2>&1 | grep -E 'dangerously-bypass-approvals-and-sandbox'
agy --help 2>&1 | grep -E '^\s+-p|--dangerously-skip-permissions'
junie --help 2>&1 | grep -iE 'brave|task|yolo|permission'
```

Expected: claude lists `-p` and `--dangerously-skip-permissions`; `codex exec --help` accepts `--dangerously-bypass-approvals-and-sandbox` (if it is NOT listed on the exec subcommand, STOP and report BLOCKED — the codex row's shape is wrong); agy lists `-p` and `--dangerously-skip-permissions`; junie lists `--brave` as interactive-only and NO headless bypass flag → **no Junie web row** (record that finding in a comment beside the Junie templates). If a binary is missing from PATH, note it and verify the remaining ones — but at least `claude --help` must succeed.

- [ ] **Step 2: Write the failing tests** (append to `internal/exttool/exttool_test.go`)

```go
func TestConflictCompleteFrontendTags(t *testing.T) {
	for _, tl := range Builtins() {
		for _, ct := range tl.CommandTemplates {
			if ct.Category != CatConflictComplete {
				continue
			}
			switch ct.Mode {
			case ModeTerminal:
				if len(ct.Frontends) != 1 || ct.Frontends[0] != "tui" {
					t.Errorf("%s/%s: terminal conflict_complete row must be tagged [tui], got %v", tl.ID, ct.Name, ct.Frontends)
				}
			case ModeCapture:
				hasWeb := false
				for _, f := range ct.Frontends {
					if f == "web" {
						hasWeb = true
					}
				}
				if !hasWeb {
					t.Errorf("%s/%s: capture conflict_complete row must be visible in web, got %v", tl.ID, ct.Name, ct.Frontends)
				}
			}
		}
	}
}

func TestHeadlessCompleteRows(t *testing.T) {
	want := map[string]bool{ // tool ID -> expects a web-tagged capture complete row
		"claude": true, "codex": true, "antigravity": true, "kimi": true,
		"junie": false, // no headless bypass flag — cannot honestly attempt the task
	}
	for _, tl := range Builtins() {
		expect, tracked := want[tl.ID]
		if !tracked {
			continue
		}
		got := false
		for _, ct := range tl.CommandTemplates {
			if ct.Category == CatConflictComplete && ct.Mode == ModeCapture {
				got = true
			}
		}
		if got != expect {
			t.Errorf("%s: capture conflict_complete row present=%v want %v", tl.ID, got, expect)
		}
	}
}
```

(The tool-ID field name and `CommandTemplates` accessor: match the existing test file's iteration style around line 600 — adjust identifiers to what `exttool_test.go` already uses.)

- [ ] **Step 3: Run to verify failure**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-ai-conflict-lane && go test ./internal/exttool/ -run 'FrontendTags|HeadlessComplete' -v`
Expected: compile FAIL (`Frontends` undefined on CommandTemplate).

- [ ] **Step 4: Implement** in `internal/exttool/exttool.go`:

Add to `CommandTemplate`:

```go
	// Frontends limits where the materialized command is OFFERED (config
	// `frontends` field): "tui" / "web" / "cli"; empty = everywhere.
	Frontends []string
```

New command constants (beside the existing complete commands; prompts are reused verbatim):

```go
// Headless (capture) resolve-and-complete variants — the web frontend's rows
// (a browser has no terminal to hand over). Same prompts, same contract; the
// permission-bypass flags make them OptIn like their terminal siblings.
// Verified 2026-08-01 (real binaries): claude -p + --dangerously-skip-permissions;
// codex exec + --dangerously-bypass-approvals-and-sandbox (the agent itself
// writes $GG_MESSAGE_FILE — no --output-last-message, which would overwrite
// the agent's overview with its final chat message); agy -p +
// --dangerously-skip-permissions (probe-verified for the commit lane
// 2026-07-20: bypass lifts headless auto-deny for reads AND the message-file
// write). Junie has NO headless variant: --brave is interactive-only, so a
// headless Junie cannot approve its own edits and cannot honestly attempt
// the task.
const claudeCompleteHeadlessCommand = `<bin> -p ` + claudeCompletePrompt + ` --dangerously-skip-permissions`

const codexCompleteHeadlessCommand = `<bin> exec ` + codexCompletePrompt + ` --dangerously-bypass-approvals-and-sandbox`

const agyCompleteHeadlessCommand = `<bin> -p ` + agyCompletePrompt + ` --dangerously-skip-permissions`
```

Catalog row edits in `Builtins()`:
- Claude: existing complete row gains `Frontends: []string{"tui"}`; add after it
  `{Category: CatConflictComplete, Name: "Claude — resolve & complete (yolo, headless)", Mode: ModeCapture, OptIn: true, Frontends: []string{"web"}, Command: claudeCompleteHeadlessCommand},`
- Junie: existing complete row gains `Frontends: []string{"tui"}` (no new row).
- Codex: existing row + `Frontends: []string{"tui"}`; add
  `{Category: CatConflictComplete, Name: "Codex — resolve & complete (yolo, headless)", Mode: ModeCapture, OptIn: true, Frontends: []string{"web"}, Command: codexCompleteHeadlessCommand},`
- Antigravity: existing row + `Frontends: []string{"tui"}`; add
  `{Category: CatConflictComplete, Name: "Antigravity — resolve & complete (yolo, headless)", Mode: ModeCapture, OptIn: true, Frontends: []string{"web"}, Command: agyCompleteHeadlessCommand},`
- Kimi: existing capture row gains `Frontends: []string{"tui", "web"}`.

In the TUI wizard materialization (`internal/tui/settings_tools.go`, wherever `config.ToolCommand{...}` is built from a template): add `Frontends: tmpl.Frontends` (grep for `WhenOp:` in that file to find the literal). If materialization instead flows through an exttool helper, set it there — follow the code.

- [ ] **Step 5: Run tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-ai-conflict-lane && go test ./internal/exttool/ ./internal/tui/ 2>&1 | tail -5`
Expected: PASS — including the pre-existing all-builtins template-validity loop (which now also covers the three new commands) and the "exactly one conflict_complete row" test at `exttool_test.go:610-635`, which MUST be updated: the terminal+headless pairs mean Claude/Codex/Antigravity now have TWO complete rows. Adjust that test to assert: exactly one `ModeTerminal` complete row for the terminal agents, at most one `ModeCapture` complete row per tool, rather than one total.

- [ ] **Step 6: Commit**

```bash
git add internal/exttool/exttool.go internal/exttool/exttool_test.go internal/tui/settings_tools.go
git commit -m "feat(exttool): headless resolve-and-complete rows for web + frontends tags"
```

---

### Task 4: frontend visibility filters (TUI, CLI, web review)

**Files:**
- Modify: `internal/tui/tools.go` (`toolCommands`), `internal/cli/review.go` (candidate loop ~line 115), `internal/web/review.go` (`reviewCommands`)
- Test: `internal/tui/tools_test.go` (or the file holding existing `toolCommands` tests), `internal/cli/review_test.go`, `internal/web/review_test.go`

**Interfaces:**
- Consumes: `config.ToolVisibleIn` (Task 1).

- [ ] **Step 1: Write the failing tests.** One per frontend, same shape — a `frontends = ["web"]`-tagged row must vanish from the TUI list and the CLI candidate set, and a `frontends = ["tui"]` row from the web review list:

TUI (place beside existing toolCommands tests; construct a `Model` with `cfg.Tools.Command` the way neighboring tests do):

```go
func TestToolCommandsFrontendFilter(t *testing.T) {
	m := Model{cfg: config.Config{Tools: config.ToolsConfig{Command: []config.ToolCommand{
		{Category: "conflict_complete", Name: "WebOnly", Mode: "capture", Frontends: []string{"web"}, Command: "x"},
		{Category: "conflict_complete", Name: "Everywhere", Mode: "capture", Command: "x"},
		{Category: "conflict_complete", Name: "TuiToo", Mode: "capture", Frontends: []string{"tui", "web"}, Command: "x"},
	}}}}
	got := m.toolCommands("conflict_complete")
	if len(got) != 2 || got[0].Name != "Everywhere" || got[1].Name != "TuiToo" {
		t.Fatalf("web-only row must be hidden from the TUI, got %+v", got)
	}
}
```

CLI: extend an existing `gg review` test fixture config with a `frontends = ["web"]` review block and assert it is neither auto-picked as sole candidate nor listed in the multiple-candidates error. Web: extend a `reviewCommands`/tools-listing test with a `frontends = ["tui"]` review block and assert it is absent from `GET /api/review/tools`.

- [ ] **Step 2: Run to verify failures** (each new test FAILs against the unfiltered lists).

- [ ] **Step 3: Implement** — three one-line filters:
  - `internal/tui/tools.go` `toolCommands`, after the category check: `if !config.ToolVisibleIn(tc, "tui") { continue }`
  - `internal/cli/review.go` candidate loop, after the category check: `if !config.ToolVisibleIn(tc, "cli") { continue }`
  - `internal/web/review.go` `reviewCommands`, after the category check: `if !config.ToolVisibleIn(tc, "web") { continue }`

- [ ] **Step 4: Run tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-ai-conflict-lane && go test ./internal/tui/ ./internal/cli/ ./internal/web/ 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/tools.go internal/cli/review.go internal/web/review.go internal/tui/*_test.go internal/cli/*_test.go internal/web/*_test.go
git commit -m "feat: honor frontends visibility tag in tui/cli/web pickers"
```

---

### Task 5: engine `CompleteConflict` op

**Files:**
- Create: `internal/engine/complete_conflict.go`, `internal/engine/complete_conflict_test.go`
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml` (one key each)

**Interfaces:**
- Consumes: `template.ConflictContextDoc`/`CQuotePath` (Task 2), `template.ResolveCommand`, the `CaptureRunner` seam (`deps.captureRunner()`, `CaptureSpec` — see `review_changes.go`), `writeTempFile`.
- Produces: `engine.CompleteConflict{Command, Dir, Env, Op, Source, Target, ConflictedFiles}` returning `Result.Captured` = overview. Task 6 consumes it.

- [ ] **Step 1: Write the failing tests** (`internal/engine/complete_conflict_test.go`; reuse the fake CaptureRunner pattern from `generate_message_test.go`/`review_changes_test.go` — read one of them first and mirror its OpDeps construction):

Test cases (one function each or table where natural):
1. **Env + context file**: fake runner records `CaptureSpec`; assert env contains `GG_OP=merge`, `GG_SOURCE=feat/x`, `GG_TARGET=main`, `GG_CONFLICTED_FILES=a.txt b.txt`, `GG_REPO=<dir>`, `GG_TASK=conflict_complete`, non-empty `GG_CONTEXT_FILE=`/`GG_MESSAGE_FILE=` values, and empty `GG_FILE=`/`GG_LOCAL=`/`GG_BASE=`/`GG_REMOTE=`/`GG_MERGED=`; read the context file DURING the fake run (capture its path from env, read inside the fake) and assert its bytes equal `template.ConflictContextDoc("merge", "feat/x", "main", []string{"a.txt", "b.txt"})`.
2. **File wins over stdout**: fake writes "file overview" into the `GG_MESSAGE_FILE` path and returns stdout "stdout noise" → `Result.Captured == "file overview"`.
3. **Stdout fallback**: fake leaves the file empty, returns stdout "from stdout" → Captured == "from stdout".
4. **Both empty is OK**: no error, `Captured == ""`.
5. **Runner error**: fake returns an error → op returns it, with whatever was captured.
6. **Resolve error runs nothing**: `Command: "x <nosuchtoken>"` → error, fake runner never invoked.
7. **Temp files removed**: capture both paths from env inside the fake; after Run returns, `os.Stat` both → `IsNotExist`.
8. **Lock mode**: `CompleteConflict{}.LockMode() == repogate.Read`.
9. **Command resolution sees the real context file**: `Command: "cat <context-file>"`-style template — assert the fake receives a resolved command containing the real temp path (quoted).

- [ ] **Step 2: Run to verify failure** — compile FAIL.

- [ ] **Step 3: Implement** `internal/engine/complete_conflict.go`:

```go
package engine

import (
	"context"
	"os"
	"strings"

	"github.com/homeend/gigagit/internal/repogate"
	"github.com/homeend/gigagit/internal/template"
)

// CompleteConflict runs a resolve-and-complete agent headless against the
// currently paused sequencer operation: the agent resolves every conflict,
// stages, runs the matching --continue itself (it OWNS the sequencer for the
// run — the CatConflictComplete contract), and reports an overview. The op
// writes a context file (op/source/target + conflicted paths, C-quoted — the
// exact bytes the TUI's tool runs write) and an empty $GG_MESSAGE_FILE, then
// resolves op.Command ITSELF (unlike ReviewChanges, which takes a resolved
// command: a custom <context-file> token needs the real temp path, which
// exists only here) and runs it via the CaptureRunner. Output-channel
// contract as ever: non-empty $GG_MESSAGE_FILE wins over stdout; an empty
// overview is NOT an error (the TUI's "reported no overview" stance).
//
// LockMode is Read — deliberately: the AGENT mutates the tree and refs, not
// gg; gg only reads. Read keeps other frontends' reads (status, commits)
// alive during a minutes-long run while still excluding gg's own tree- and
// ref-writing ops. The TUI precedent for this category is no reservation at
// all ($EDITOR standing); Read is strictly safer. Validation ("is anything
// paused?") is the domain wrapper's job, the ReviewChanges split.
type CompleteConflict struct {
	Command         string   // command TEMPLATE text (config); resolved by the op
	Dir             string   // worktree root the agent runs in
	Env             []string // caller env additions
	Op              string   // paused op: merge|rebase|cherry-pick|revert
	Source          string   // the op's parties (context values, not executed)
	Target          string
	ConflictedFiles []string // repo-relative conflicted paths
}

var _ Operation = CompleteConflict{}

func (op CompleteConflict) LockMode() repogate.Mode { return repogate.Read }

func (op CompleteConflict) Run(ctx context.Context, deps OpDeps) (Result, error) {
	ctxPath, err := writeTempFile("gg-context-*.txt",
		template.ConflictContextDoc(op.Op, op.Source, op.Target, op.ConflictedFiles))
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(ctxPath)
	msgPath, err := writeTempFile("gg-overview-*.md", "")
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(msgPath)

	resolved, err := template.ResolveCommand(op.Command, nil, template.CmdCtx{
		Op: op.Op, Source: op.Source, Target: op.Target,
		ConflictedFiles: op.ConflictedFiles, Repo: op.Dir, ContextFile: ctxPath,
	})
	if err != nil {
		return Result{}, err
	}

	env := append(append([]string{}, os.Environ()...), op.Env...)
	env = append(env,
		"GG_OP="+op.Op,
		"GG_SOURCE="+op.Source,
		"GG_TARGET="+op.Target,
		"GG_CONFLICTED_FILES="+strings.Join(op.ConflictedFiles, " "),
		"GG_REPO="+op.Dir,
		"GG_FILE=", "GG_LOCAL=", "GG_BASE=", "GG_REMOTE=", "GG_MERGED=",
		"GG_CONTEXT_FILE="+ctxPath,
		"GG_MESSAGE_FILE="+msgPath,
		"GG_TASK=conflict_complete",
	)
	stdout, runErr := deps.captureRunner().Capture(ctx,
		CaptureSpec{Dir: op.Dir, Env: env, Command: resolved},
		func(line string) { deps.emit(ctx, GitLine{Raw: line}) })
	captured := string(stdout)
	if fileMsg, rerr := os.ReadFile(msgPath); rerr == nil && strings.TrimSpace(string(fileMsg)) != "" {
		captured = string(fileMsg)
	}
	if runErr != nil {
		return Result{Captured: captured}, runErr
	}
	return Result{Captured: captured}.WithSummary("conflict agent finished (%s)", op.Op), nil
}
```

Match the exact `CmdCtx` field names against `internal/template` (grep `type CmdCtx`) — adjust if e.g. the conflicted-files field is named differently.

Add the summary key to all four bundles (`internal/i18n/lang/*.toml`, alphabetical/nearby-key placement matching each file's style):

```toml
"conflict agent finished (%s)" = "コンフリクト エージェントが完了しました（%s）"   # ja
"conflict agent finished (%s)" = "충돌 에이전트 실행 완료 (%s)"                    # ko
"conflict agent finished (%s)" = "冲突代理运行完成（%s）"                          # zh
"conflict agent finished (%s)" = "агент разрешения конфликтов завершил работу (%s)"  # ru
```

- [ ] **Step 4: Run tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-ai-conflict-lane && go test ./internal/engine/ -run CompleteConflict -v && go test ./internal/tui/ -run 'EngineProse|I18n' 2>&1 | tail -3`
Expected: PASS — the engine-prose bundle gate must see the new key in all four bundles.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/complete_conflict.go internal/engine/complete_conflict_test.go internal/i18n/lang/
git commit -m "feat(engine): CompleteConflict capture op for headless resolve-and-complete agents"
```

---

### Task 6: domain wrapper `CompleteConflictReport`

**Files:**
- Create: `internal/domain/complete_conflict.go`, `internal/domain/complete_conflict_test.go`

**Interfaces:**
- Consumes: `engine.CompleteConflict` (Task 5), `s.Status`, `s.Conflict`, `s.TopLevel`, `exttool.ParseCaptureReport`.
- Produces: `func (s *Service) CompleteConflictReport(ctx context.Context, commandTemplate string, env []string) (CompleteConflictResult, error)` with `CompleteConflictResult{Overview, Op string; StillPaused bool}`. Task 7 consumes it.

- [ ] **Step 1: Write the failing tests** (`internal/domain/complete_conflict_test.go`; use the package's existing real-git repo helper — grep `func newTestRepo` / how `review_test.go` builds a Service):

1. **No paused op → error**: clean repo, `CompleteConflictReport(ctx, "echo hi", nil)` → error containing "no paused operation"; nothing else runs.
2. **Completes a real merge**: build a conflicted merge (two branches editing one file — mirror `internal/web/conflict_test.go`'s `conflictedMergeState` setup), then run with the command template:
   `git checkout --theirs f.txt && git add f.txt && GIT_EDITOR=true git merge --continue && printf 'took theirs\n' > "$GG_MESSAGE_FILE"`
   Assert: no error, `Overview == "took theirs"`, `Op == "merge"`, `StillPaused == false`, and a follow-up `Status` shows zero conflicted files with `MERGE_HEAD` gone.
3. **Stop-early leaves paused**: same fixture, command `echo gave up` (touches nothing) → no error, `StillPaused == true`, `Op == "merge"`, Overview == "gave up".
4. **Env reaches the agent**: command `printf '%s' "$GG_OP" > "$GG_MESSAGE_FILE"` → Overview == "merge".

Guard the shell-dependent tests with the package's existing pattern for POSIX-only tests (grep for `runtime.GOOS == "windows"` skips in domain/engine tests and mirror it).

- [ ] **Step 2: Run to verify failure** — compile FAIL.

- [ ] **Step 3: Implement** `internal/domain/complete_conflict.go`:

```go
package domain

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/model"
)

// CompleteConflictResult is what a resolve-and-complete agent run produced.
type CompleteConflictResult struct {
	Overview    string // the agent's overview (may be "" — not an error)
	Op          string // the op that was paused when the run started
	StillPaused bool   // true when the agent stopped early (op still paused)
}

// CompleteConflictReport runs a conflict_complete agent command headless
// against the currently paused operation and reports its overview. The
// command arrives as TEMPLATE text (the config block); the engine op resolves
// it after creating the context file. Refuses when nothing is paused.
// Display-only: unlike ReviewReport nothing is persisted, and an empty
// overview is not an error (TUI parity).
func (s *Service) CompleteConflictReport(ctx context.Context, commandTemplate string, env []string) (CompleteConflictResult, error) {
	st, err := s.Status(ctx)
	if err != nil {
		return CompleteConflictResult{}, err
	}
	cs := s.Conflict(ctx, st)
	if cs.Op == "" {
		return CompleteConflictResult{}, fmt.Errorf("no paused operation to complete")
	}
	files := unmergedPaths(st)
	top, err := s.TopLevel(ctx)
	if err != nil {
		return CompleteConflictResult{}, err
	}
	res, err := s.Execute(ctx, engine.CompleteConflict{
		Command: commandTemplate, Dir: top, Env: env,
		Op: cs.Op, Source: cs.Source, Target: cs.Target, ConflictedFiles: files,
	}, nil, nil)
	if err != nil {
		return CompleteConflictResult{}, err
	}
	overview := strings.TrimSpace(res.Captured)
	if overview != "" {
		// Unwrap a JSON-enveloped stdout (Claude --output-format json) the
		// way ReviewReport does; plain text passes through unchanged.
		report, perr := exttool.ParseCaptureReport(res.Captured)
		if perr != nil {
			return CompleteConflictResult{}, perr
		}
		overview = strings.TrimSpace(report)
	}
	out := CompleteConflictResult{Overview: overview, Op: cs.Op}
	if st2, serr := s.Status(ctx); serr == nil {
		out.StillPaused = s.Conflict(ctx, st2).Op != ""
	}
	return out, nil
}

// unmergedPaths lists the conflicted paths in status order.
func unmergedPaths(st model.WorkingTreeStatus) []string {
	var out []string
	for _, f := range st.Files {
		if f.Kind == model.KindUnmerged {
			out = append(out, f.Path)
		}
	}
	return out
}
```

(Check `model.WorkingTreeStatus`'s file-slice field name against `internal/web/conflict.go`'s eligibility loop and match it. If a same-named helper already exists in domain or web, reuse/hoist rather than duplicating.)

- [ ] **Step 4: Run tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-ai-conflict-lane && go test ./internal/domain/ -run CompleteConflict -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/complete_conflict.go internal/domain/complete_conflict_test.go
git commit -m "feat(domain): CompleteConflictReport wrapper for the web AI conflict lane"
```

---

### Task 7: web endpoints + cancel widening

**Files:**
- Create: `internal/web/conflictai.go`, `internal/web/conflictai_test.go`
- Modify: `internal/web/server.go` (two routes), `internal/web/review.go` (`handleOpCancel` kind check)

**Interfaces:**
- Consumes: `s.effectiveConfig`, `s.promptStore`/`s.toolRepoKey` (review.go), `s.startRun` (oprun.go), `svc.CompleteConflictReport` (Task 6), `config.ToolVisibleIn`, `exttool.CatConflictComplete`.
- Produces: `GET /api/conflict/tools`, `POST /api/conflict/complete` (writeGuard); done-event extra keys `report`, `tool`, `op`, `still_paused` (Task 8 consumes).

- [ ] **Step 1: Write the failing wire tests** (`internal/web/conflictai_test.go`; reuse the paused-merge fixture helper from `conflict_test.go` and the test-server construction from `review_test.go`/`opconflict_test.go`):

1. **tools 409 when nothing paused.**
2. **tools filtering**: config with four `conflict_complete` blocks — `mode="terminal"` (hidden), `mode="capture"` untagged (shown), `mode="capture" frontends=["tui"]` (hidden), `mode="capture" frontends=["web"] when_op="rebase"` against a paused MERGE (hidden) — assert exactly the untagged capture row is listed, with `approved:false` and a non-empty resolved `command`; response carries `op:"merge"` and `conflicted >= 1`.
3. **approval flow**: `POST /api/conflict/complete {tool}` → 403 with `needs_approval:true` and the resolved command; re-POST with `approve:true` → 202 with `op_id`; a SECOND run of the same tool later needs no approve (promptstate remembered).
4. **run completes the merge**: fake-agent command template (the Task 6 shell line) → follow the SSE (or poll the run's done via the existing test helper in `ophttp`/review tests) → done extra has `report:"took theirs"`, `still_paused:false`; `/api/status` afterwards has no `conflict` object.
5. **stop-early**: `echo gave up` agent → done ok with `still_paused:true`; `/api/status` still carries the conflict object.
6. **cancel widened**: a `conflict_complete` run accepts `POST /api/op/{id}/cancel`; a plain git op (e.g. dispatch `op:"fetch"` or reuse an existing non-review kind from the ophttp tests) still gets 409.
7. **unknown tool** → 400; **nothing paused on POST** → 409.

- [ ] **Step 2: Run to verify failure** — 404s/compile errors.

- [ ] **Step 3: Implement** `internal/web/conflictai.go` (mirror `review.go`'s structure and comments-of-record):

```go
// handleConflictTools: GET /api/conflict/tools — 409 when nothing is paused;
// else the paused-op facts + every runnable headless conflict_complete
// command, resolved for display with the real op/source/target/paths and the
// literal $GG_CONTEXT_FILE placeholder (the real path exists only at run
// time; catalog rows reference env vars and are unaffected).
func (s *Server) handleConflictTools(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	st, err := svc.Status(r.Context())
	if err != nil { writeErr(w, http.StatusInternalServerError, err); return }
	cs := svc.Conflict(r.Context(), st)
	if cs.Op == "" { writeErr(w, http.StatusConflict, errors.New("nothing is paused")); return }
	cmds, err := s.conflictCompleteCommands(r.Context(), svc, cs.Op)
	if err != nil { writeErr(w, http.StatusInternalServerError, err); return }
	top, err := svc.TopLevel(r.Context())
	if err != nil { writeErr(w, http.StatusInternalServerError, err); return }
	files := unmergedStatusPaths(st) // same loop as conflict.go's; hoist if one exists
	var approved map[string]bool
	if store := s.promptStore(); store != nil {
		approved = store.ApprovedToolCommands(s.toolRepoKey(r.Context(), svc))
	}
	rows := make([]reviewToolRow, 0, len(cmds))
	for _, tc := range cmds {
		resolved, rerr := template.ResolveCommand(tc.Command, nil, template.CmdCtx{
			Op: cs.Op, Source: cs.Source, Target: cs.Target,
			ConflictedFiles: files, Repo: top, ContextFile: "$GG_CONTEXT_FILE",
		})
		if rerr != nil { continue } // <user:…>-token rows etc. are inert here
		rows = append(rows, reviewToolRow{Name: tc.Name, Command: resolved,
			Approved: approved[promptstate.CommandHash(tc.Command)]})
	}
	writeJSON(w, map[string]any{
		"op": cs.Op, "source": cs.Source, "target": cs.Target,
		"desc": cs.Describe(), "conflicted": len(files), "tools": rows,
	})
}
```

`conflictCompleteCommands` (the filter): effective config → `tc.Category == string(exttool.CatConflictComplete)` && `tc.Mode == "capture"` && `config.ToolVisibleIn(tc, "web")` && (`tc.WhenOp == "" || tc.WhenOp == pausedOp`) && `config.ValidateToolCommand(tc) == nil` && `template.ValidateCommandTokens(tc.Command, tc.PerFile) == nil`.

`handleConflictComplete` (POST, writeGuard): decode `{tool, approve}`; re-read status+conflict (409 when none); pick by name from the filtered set (400 unknown, listing names); approval gate copied VERBATIM from `handleReviewStart` (403 `needs_approval` with the display-resolved command; `approve:true` records best-effort); then:

```go
	run, err := s.startRun("conflict_complete", func(ctx context.Context, svc *domain.Service, _ chan<- engine.Event, _ engine.Decider) (engine.Result, map[string]any, error) {
		res, rerr := svc.CompleteConflictReport(ctx, tc.Command, nil)
		if rerr != nil {
			return engine.Result{}, nil, rerr
		}
		return engine.Result{Summary: "conflict agent finished"},
			map[string]any{"report": res.Overview, "tool": tc.Name, "op": res.Op, "still_paused": res.StillPaused},
			nil
	})
```

409 on busy lane; 202 `{op_id, tool}`.

Routes in `server.go` beside the review ones: `GET /api/conflict/tools` → `handleConflictTools`; `POST /api/conflict/complete` → writeGuard-wrapped `handleConflictComplete`.

`handleOpCancel` in `review.go`: replace `run.kind != "review"` with

```go
	if run.kind != "review" && run.kind != "conflict_complete" {
```

and update its comment: agent runs (review, conflict_complete) are cancellable — they can hang for minutes holding the single lane; interrupting a GIT op stays a separate design question.

- [ ] **Step 4: Run tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-ai-conflict-lane && go test ./internal/web/ 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web/conflictai.go internal/web/conflictai_test.go internal/web/server.go internal/web/review.go
git commit -m "feat(web): conflict AI lane endpoints — tools listing, approval-gated run, cancel"
```

---

### Task 8: client — AI resolve button + conflict mode of the review lane

**Files:**
- Modify: `internal/web/static/index.html`, `internal/web/static/app.js`, `internal/web/static/style.css`

No Go tests — this task is verified by the browser check (Task 9). Keep the JS in the existing style (no frameworks, `esc()` everything rendered via innerHTML).

- [ ] **Step 1: index.html** — inside `#conflict-bar`, before `#conflict-continue`:

```html
  <button id="conflict-ai">AI resolve…</button>
```

- [ ] **Step 2: app.js — generalize the lane.** The `rev` state gains `mode: "review" | "conflict"` (set `mode: "review"` in `startReview`). Changes, all inside the existing "AI review" section:

`reviewTitle()`:

```js
function reviewTitle() {
  if (!rev) return "";
  if (rev.mode === "conflict") return "AI resolve — " + (rev.label || rev.op || "");
  return "Review " + (rev.label || (rev.target === "working" ? "working changes" : rev.branch)) + " (AI)";
}
```

New entry point (beside `startReview`, same guards — a dead click must report):

```js
async function startConflictAI() {
  if (rev) {
    if (rev.parked) opLine("an agent is already running in the background — open the chip to watch or cancel it", true);
    return;
  }
  if (state.op) {
    opLine("AI resolve: an operation is already running", true);
    return;
  }
  let info;
  try {
    info = await getJSON("/api/conflict/tools");
  } catch (e) {
    opLine("AI resolve: " + (e.message || e), true);
    return;
  }
  const tools = info.tools || [];
  if (!tools.length) {
    opLine('AI resolve: no headless conflict agent configured — add a [[tools.command]] block with category = "conflict_complete", mode = "capture", frontends = ["web"]', true);
    return;
  }
  rev = { mode: "conflict", op: info.op, label: info.desc || info.op, tools, sel: 0, phase: "choose", tool: null };
  pushLayer("review", $("review"), { onKey: reviewKey });
  if (tools.length === 1) reviewPick(tools[0]);
  else renderReview();
}
$("conflict-ai").addEventListener("click", startConflictAI);
```

`reviewRun(approve)` — post per mode (only the marked lines change):

```js
  const isConflict = rev.mode === "conflict";
  resp = await postJSON(
    isConflict ? "/api/conflict/complete" : "/api/review",
    isConflict ? { tool: tool.name, approve: !!approve }
               : { target, branch, tool: tool.name, approve: !!approve }
  );
  ...
  followOp(resp.op_id,
    (isConflict ? "AI resolving " : "reviewing ") + (rev.label || ""),
    isConflict ? "conflict_complete" : "review",
    reviewDone);
```

`reviewDone(ev)` — capture `mode` with the other pre-close captures, refresh after a conflict run, report still-paused, reuse the report keys:

```js
function reviewDone(ev) {
  const title = rev && rev.mode === "conflict"
    ? "Resolution overview — " + ((rev.tool && rev.tool.name) || "agent")
    : reviewTitle() || "Review";
  const isConflict = !!(rev && rev.mode === "conflict");
  const parked = !!(rev && rev.parked);
  const label = (state.task && state.task.label) || (rev && rev.label) || "";
  closeReviewLane();
  if (isConflict) refreshAfterOp(); // the repo changed (or didn't) — reality first
  if (parked) {
    state.task = {
      kind: isConflict ? "conflict" : "review",
      label,
      status: ev.ok ? "done" : ev.cancelled ? "cancelled" : "failed",
      title, path: ev.path, report: ev.report, error: ev.error,
    };
    if (state.task.status === "cancelled") state.task = null;
    renderTaskChip(true);
    if (ev.ok) hideOpLine();
    else opLine((isConflict ? "AI resolve failed: " : "review failed: ") + (ev.error || "unknown error"), true);
    return;
  }
  if (ev.ok) {
    if (isConflict) {
      if (ev.still_paused) opLine("the agent left the " + (ev.op || "operation") + " paused — finish it manually or run another agent", true);
      else opLine((ev.op || "operation") + " completed");
      if (ev.report) openReport(title, "", ev.report);
      else if (!ev.still_paused) opLine((ev.op || "operation") + " completed — the agent reported no overview");
      return;
    }
    openReport(title, ev.path, ev.report);
    opLine(ev.summary || "review done");
    return;
  }
  if (ev.cancelled) opLine(isConflict ? "AI resolve cancelled" : "review cancelled");
  else opLine((isConflict ? "AI resolve failed: " : "review failed: ") + (ev.error || "unknown error"), true);
}
```

(`refreshAfterOp` — use the existing post-op refresh helper by its real name in app.js; grep for the one the lost-connection path calls. On a failed conflict run also refresh — add `if (isConflict && !ev.ok) refreshAfterOp();` is already covered by the unconditional `if (isConflict) refreshAfterOp();` above.)

`renderReview()` — running-phase copy must not claim "reading the diff" for a conflict run:

```js
  body.innerHTML =
    `<div class="rnote">${esc(rev.tool ? rev.tool.name : "")} ` +
    (rev.mode === "conflict"
      ? "is resolving the conflicts and completing the operation — this can take a few minutes."
      : "is reading the diff — this can take a few minutes.") +
    ` You can put it in the background and carry on reading the repo; the chip in the top bar lights up when it finishes.</div>`;
```

and the chooser hint: `hint.textContent = rev.mode === "conflict" ? "choose an agent · enter runs · esc cancels" : "choose a review tool · enter runs · esc cancels";`

`parkReview()` / `renderTaskChip()` / `collectTask()` — carry the kind:

```js
function parkReview() {
  if (!rev || rev.phase !== "running") return;
  rev.parked = true;
  state.task = { kind: rev.mode === "conflict" ? "conflict" : "review", label: rev.label || "", status: "running" };
  closeLayer("review");
  renderTaskChip(false);
  taskLine((state.task.kind === "conflict" ? "AI resolve" : "review") + " running in the background — click here to watch or cancel it");
}
```

In `renderTaskChip`, derive the noun once: `const noun = t.kind === "conflict" ? "AI resolve" : "review";` and use it in the three labels (`"⟳ " + noun + …`, `"✓ " + noun + " ready"` → for conflict use `"✓ " + noun + " done"` is fine as `"✓ AI resolve done"`; keep `"✗ " + noun + " failed"`) and the two `title` strings. In `collectTask`, the failure line becomes `opLine(noun + " failed: …")` with the same derivation.

- [ ] **Step 3: style.css** — `#conflict-ai` inherits the bar's button styling automatically if buttons in `#conflict-bar` share a selector; check and, if `#conflict-continue`/`#conflict-abort` are styled by id, add `#conflict-ai` to the non-danger rule.

- [ ] **Step 4: Build + eyeball**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-ai-conflict-lane && go build ./... && go vet ./internal/web/`
Expected: clean. (Behavioral verification is Task 9.)

- [ ] **Step 5: Commit**

```bash
git add internal/web/static/index.html internal/web/static/app.js internal/web/static/style.css
git commit -m "feat(web): AI resolve lane in the conflict banner"
```

---

### Task 9: docs

**Files:**
- Modify: `CHANGELOG.md`, `CLAUDE.md`, `README.md`

- [ ] **Step 1: CHANGELOG.md** — one bullet under Unreleased:

```
- Web: AI conflict resolution — an "AI resolve…" button on the paused-operation
  banner runs a headless `conflict_complete` agent (chooser → one-time command
  approval shared with the TUI → background run with park/cancel); the agent
  resolves, stages, runs the matching `--continue`, and its overview opens in
  the report viewer. New `frontends` tag on `[[tools.command]]` blocks
  ("tui"/"web"/"cli", empty = everywhere) keeps the TUI picker unchanged while
  new web-only headless catalog rows (Claude/Codex/Antigravity; Kimi's existing
  capture row is shared) power the web lane. Engine: new `CompleteConflict`
  capture op (LockMode Read — the agent mutates, gg only reads).
```

- [ ] **Step 2: CLAUDE.md** — extend the package-map rows: `engine` (CompleteConflict op — fields, context/message files, resolves its own command, LockMode Read rationale), `config` (`frontends` field + `ToolVisibleIn`), `exttool` (headless complete rows + `Frontends` on templates, Junie exclusion rationale), `template` (`CQuotePath`/`ConflictContextDoc` shared helpers), `domain` (`CompleteConflictReport`), `web` (the lane: endpoints, cancel widening, client mode). Follow the existing terse-with-gotchas style.

- [ ] **Step 3: README.md** — extend the web-frontend blockquote/bullet with the AI resolve line.

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md CLAUDE.md README.md
git commit -m "docs: web AI conflict lane"
```

---

## Controller verification (after all tasks; not a subagent task)

1. Full gates: `cd <worktree> && ./test.sh` then `nohup ./test.sh race` + poll pid (quiet machine).
2. Headless-CDP browser check (both loopback hosts; old build first, twice): fixture repo with a conflicted merge + a fake-agent `[[tools.command]]` block (`category="conflict_complete"`, `mode="capture"`, `frontends=["web"]`, command = the checkout-theirs/add/continue/overview shell line). Assert: `#conflict-ai` visible via `elementFromPoint`; click → overlay approval step shows the resolved command; run → banner clears; report overlay opens titled "Resolution overview — …" with the overview text. Second fixture/flag: stop-early agent → banner stays, status line names the paused op. Old build: `#conflict-ai` absent → FAILs.
3. One live real-Claude run against a fixture conflict (the behavioral end-to-end for a true agent).
4. Final whole-branch review, then hand to the user for the merge decision.
