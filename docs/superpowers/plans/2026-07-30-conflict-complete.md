# AI Resolve-and-Complete (`conflict_complete`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A new yolo-only agentic-task category `conflict_complete`: one agent invocation from the conflict window's `t` picker resolves a paused merge/rebase's conflicts, completes the operation (repeating through further rebase rounds), and reports an overview gg shows in the report viewer.

**Architecture:** A fourth `exttool` category alongside `conflict`/`commit_message`/`review`. Same run machinery as the conflict lane (terminal handover via `tea.ExecProcess`, capture for Kimi), plus one new plumbing piece: gg creates an empty `$GG_MESSAGE_FILE` before the run and, on a clean exit, opens its content in the existing `reviewView` full-screen viewer (closing the conflict process first — it preempts the layer stack for keys). The spec is `docs/superpowers/specs/2026-07-30-conflict-complete-design.md` in this worktree.

**Tech Stack:** Go 1.26, Bubble Tea TUI, real-git tests in `t.TempDir()`.

## Global Constraints

- ALL work happens in the worktree `/mnt/t/others/gigagit.worktrees/feat-conflict-complete` on branch `feat/conflict-complete`. Prefix every build/test command with `cd /mnt/t/others/gigagit.worktrees/feat-conflict-complete &&`. Write/Edit tools MUST use worktree-absolute paths (an absolute path into `/mnt/t/others/gigagit/...` would silently edit the main checkout).
- Every git commit message ends with the two trailers:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_018qZ6XFQKhGNVnyAhJqAgJ7`.
- Engine/CLI prose and all protocol values (category strings, option values, `GG_*` env names) stay English. Only TUI display strings go through `i18n.T`, always with a string-literal key, and every new key must be added to ALL FOUR bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`) in the same commit — `internal/tui/i18n_scan_test.go` enforces this.
- The OptIn invariant: `OptIn` is true ⇔ the command text carries a permission-bypass flag (`--dangerously-skip-permissions`, `--brave`, `--dangerously-bypass-approvals-and-sandbox`). `TestOptInMarksExactlyThePermissionBypassVariants` (internal/exttool/exttool_test.go:237) enforces this mechanically — do not fight it.
- Catalog `Name` values are unique per category and are shown raw in pickers/wizard (config content, never i18n).
- File paths below are worktree-relative; `internal/...` means `/mnt/t/others/gigagit.worktrees/feat-conflict-complete/internal/...`.

---

### Task 1: exttool catalog — category, prompts, rows

**Files:**
- Modify: `internal/exttool/exttool.go`
- Test: `internal/exttool/exttool_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `exttool.CatConflictComplete Category = "conflict_complete"` (used by config validation, the TUI picker, wizard defaults); five new `CommandTemplate` rows in `Builtins()` with the exact Names listed below.

- [ ] **Step 1: Write the failing test**

Append to `internal/exttool/exttool_test.go` (package-internal style like the file's existing tests — bare `Builtins()`, `ModeTerminal`, etc.):

```go
// TestConflictCompleteTemplates pins the resolve-and-complete category's
// contract: every agent gets exactly ONE row (yolo-only — no cautious
// variant), terminal handover except Kimi (headless -p, capture), the
// permission-bypass flag present except Kimi (whose -p refuses yolo flags
// but auto-approves anyway), and a prompt that (a) instructs the matching
// --continue command, (b) forbids --abort, and (c) routes the overview
// through GG_MESSAGE_FILE. Meld is a mergetool, not an agent: no row.
func TestConflictCompleteTemplates(t *testing.T) {
	want := map[string]struct {
		mode  Mode
		optIn bool
		flag  string // required bypass-flag substring; "" = none allowed
	}{
		"claude":      {ModeTerminal, true, "--dangerously-skip-permissions"},
		"junie":       {ModeTerminal, true, "--brave"},
		"codex":       {ModeTerminal, true, "--dangerously-bypass-approvals-and-sandbox"},
		"antigravity": {ModeTerminal, true, "--dangerously-skip-permissions"},
		"kimi":        {ModeCapture, false, ""},
	}
	for _, tl := range Builtins() {
		var rows []CommandTemplate
		for _, ct := range tl.Commands {
			if ct.Category == CatConflictComplete {
				rows = append(rows, ct)
			}
		}
		spec, ok := want[tl.ID]
		if !ok {
			if len(rows) != 0 {
				t.Errorf("%s: unexpected conflict_complete rows: %v", tl.ID, rows)
			}
			continue
		}
		if len(rows) != 1 {
			t.Fatalf("%s: want exactly one conflict_complete row, got %d", tl.ID, len(rows))
		}
		ct := rows[0]
		if ct.Mode != spec.mode {
			t.Errorf("%s: mode = %s, want %s", tl.ID, ct.Mode, spec.mode)
		}
		if ct.OptIn != spec.optIn {
			t.Errorf("%s: OptIn = %v, want %v", tl.ID, ct.OptIn, spec.optIn)
		}
		if spec.flag != "" && !strings.Contains(ct.Command, spec.flag) {
			t.Errorf("%s: command missing bypass flag %s", tl.ID, spec.flag)
		}
		if ct.PerFile {
			t.Errorf("%s: conflict_complete must not be per-file", tl.ID)
		}
		for _, must := range []string{"--continue", "<env:GG_MESSAGE_FILE>", "--abort", "<env:GG_CONTEXT_FILE>"} {
			if !strings.Contains(ct.Command, must) {
				t.Errorf("%s: prompt must mention %s", tl.ID, must)
			}
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-complete && go test ./internal/exttool -run TestConflictCompleteTemplates -v`
Expected: FAIL — `undefined: CatConflictComplete` (compile error).

- [ ] **Step 3: Implement the category and rows**

In `internal/exttool/exttool.go`:

3a. Extend the category consts (line ~25) and the package doc's category list:

```go
const (
	CatConflict         Category = "conflict"
	CatCommitMessage    Category = "commit_message"
	CatReview           Category = "review"
	// CatConflictComplete is the resolve-AND-complete task: unlike CatConflict
	// (whose contract forbids --continue — gg's ContinueOp owns the sequencer),
	// this category's agent deliberately OWNS the sequencer for the run:
	// resolve, stage, continue, repeat through further rebase rounds, then
	// report an overview via GG_MESSAGE_FILE. Yolo-only: one bypass-flag
	// variant per agent, no cautious row.
	CatConflictComplete Category = "conflict_complete"
)
```

3b. Add the prompt/command constants after the existing agy constants (line ~278). The contract text is shared; per-agent transport differs:

```go
// Resolve-and-complete (CatConflictComplete) prompts. The shared contract:
// read the paused op from GG_OP/GG_CONTEXT_FILE, resolve every conflict,
// git add, run the MATCHING --continue with GIT_EDITOR=true (so no editor
// can block a handover-less run), repeat through further rebase rounds,
// NEVER --abort or push, stop-and-leave-paused when unsafe, and write the
// overview to GG_MESSAGE_FILE (the "absolute path outside the repository"
// phrasing is load-bearing for task-agents — the Junie precedent).
const claudeCompletePrompt = `"A git <env:GG_OP> operation is paused with conflicts in this repository.
   Read the context file at <env:GG_CONTEXT_FILE> for the operation's parties and the conflicted paths.
   Inspect both sides' history to understand intent, resolve every conflict by editing the files,
   and run git add on each resolved file. Then COMPLETE the operation: run the matching continue
   command (git merge --continue, git rebase --continue, git cherry-pick --continue, or
   git revert --continue) with GIT_EDITOR=true so no editor can block. If the operation pauses
   again on new conflicts, resolve them the same way and continue again, until it finishes.
   NEVER run any --abort command and never push. If a conflict cannot be resolved safely, stop
   and leave the operation paused instead. Finally, write an overview to the file at
   <env:GG_MESSAGE_FILE> (an absolute path outside the repository): which operation was paused,
   each file and how you resolved it, how many continue rounds ran, and the final state."`

// claudeCompleteCommand: prompt FIRST (the variadic-flag ordering contract),
// bypass flag only — bypass mode never consults allow/deny lists.
const claudeCompleteCommand = `<bin> ` + claudeCompletePrompt + ` --dangerously-skip-permissions`

const junieCompletePrompt = `"A git <env:GG_OP> operation is paused with conflicts in this repository. Read the context file at <env:GG_CONTEXT_FILE> for the operation's parties and the conflicted paths. Resolve every conflict by editing the files, run git add on each resolved file, then COMPLETE the operation: run the matching continue command (git merge --continue, git rebase --continue, git cherry-pick --continue, or git revert --continue) with GIT_EDITOR=true so no editor can block. If it pauses again on new conflicts, resolve and continue again until it finishes. NEVER run any --abort command and never push. If a conflict cannot be resolved safely, stop and leave the operation paused. Finally write an overview (which operation, each file and how you resolved it, how many continue rounds ran, the final state) into the file at <env:GG_MESSAGE_FILE> (an absolute path outside the repository)."`

const junieCompleteCommand = `<bin> --prompt ` + junieCompletePrompt + ` --brave`

const codexCompletePrompt = junieCompletePrompt

const codexCompleteCommand = `<bin> ` + codexCompletePrompt + ` --dangerously-bypass-approvals-and-sandbox`

const agyCompletePrompt = junieCompletePrompt

const agyCompleteCommand = `<bin> --prompt-interactive ` + agyCompletePrompt + ` --dangerously-skip-permissions`

// kimiCompleteCommand: headless -p (Kimi has no interactive-with-prompt
// mode, and -p REFUSES --yolo/--auto — but print mode approves its own
// edits and was verified 2026-07-20 running `git add` autonomously, so it
// can honestly attempt the task without a flag; hence NOT OptIn, per the
// invariant). ModeCapture like kimiConflictCommand: -p draws no UI, a
// handover would show a dead screen. MUST be re-verified against a real
// paused rebase before merge — if print mode refuses to run the --continue
// commands, DELETE this row (the spec's Kimi conditional).
const kimiCompletePrompt = junieCompletePrompt

const kimiCompleteCommand = `<bin> -p ` + kimiCompletePrompt
```

3c. Add one row per agent in `Builtins()`, after each tool's existing `CatConflict` rows (NOT for meld):

```go
// claude:
{Category: CatConflictComplete, Name: "Claude — resolve & complete (yolo)", Mode: ModeTerminal, OptIn: true, Command: claudeCompleteCommand},
// junie:
{Category: CatConflictComplete, Name: "Junie — resolve & complete (yolo)", Mode: ModeTerminal, OptIn: true, Command: junieCompleteCommand},
// codex:
{Category: CatConflictComplete, Name: "Codex — resolve & complete (yolo)", Mode: ModeTerminal, OptIn: true, Command: codexCompleteCommand},
// antigravity:
{Category: CatConflictComplete, Name: "Antigravity — resolve & complete (yolo)", Mode: ModeTerminal, OptIn: true, Command: agyCompleteCommand},
// kimi:
{Category: CatConflictComplete, Name: "Kimi — resolve & complete", Mode: ModeCapture, Command: kimiCompleteCommand},
```

- [ ] **Step 4: Run the exttool tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-complete && go test ./internal/exttool -v`
Expected: PASS, including `TestOptInMarksExactlyThePermissionBypassVariants` (the new rows satisfy OptIn ⇔ bypass-flag automatically) and `TestConflictCompleteTemplates`. If `TestBuiltinsCatalogInvariants` asserts per-category or per-tool command counts, extend its expectations to include the new rows — do not weaken its assertions.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-conflict-complete
git add internal/exttool/exttool.go internal/exttool/exttool_test.go
git commit -m "feat(exttool): conflict_complete category — yolo resolve-and-complete rows"
```
(append the two Global Constraints trailers to this and every commit message)

---

### Task 2: config validation accepts the category

**Files:**
- Modify: `internal/config/tools.go`
- Test: `internal/config/tools_test.go`

**Interfaces:**
- Consumes: nothing from Task 1 (config must stay free of the exttool dependency — the category is a string literal here, matching the existing pattern at internal/config/tools.go:56).
- Produces: `config.ValidateToolCommand` accepting `Category == "conflict_complete"`; `per_file` with it stays invalid (the existing "per_file only for conflict" rule).

- [ ] **Step 1: Write the failing test**

Append to `internal/config/tools_test.go` (match the file's existing package/test style):

```go
func TestValidateToolCommandConflictComplete(t *testing.T) {
	ok := ToolCommand{Category: "conflict_complete", Name: "Agent", Mode: "terminal", Command: "agent"}
	if err := ValidateToolCommand(ok); err != nil {
		t.Fatalf("conflict_complete must validate, got %v", err)
	}
	perFile := ok
	perFile.PerFile = true
	if err := ValidateToolCommand(perFile); err == nil {
		t.Fatal("per_file with conflict_complete must be invalid")
	}
	whenOp := ok
	whenOp.WhenOp = "rebase"
	if err := ValidateToolCommand(whenOp); err != nil {
		t.Fatalf("when_op with conflict_complete must validate, got %v", err)
	}
}
```

If the test file's package is `config_test`, prefix `config.` accordingly (mirror neighboring tests).

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-complete && go test ./internal/config -run TestValidateToolCommandConflictComplete -v`
Expected: FAIL — `unknown category "conflict_complete"`.

- [ ] **Step 3: Implement**

In `internal/config/tools.go`:
- `ValidateToolCommand`'s category switch becomes:

```go
	switch tc.Category {
	case "conflict", "commit_message", "review", "conflict_complete":
	default:
		return fmt.Errorf("tools: unknown category %q (want conflict|commit_message|review|conflict_complete)", tc.Category)
	}
```

- Update the `ToolCommand.Category` struct comment to `// conflict | commit_message | review | conflict_complete`.
- Check `internal/config/template.go` for a `[tools]`/category comment in the generated config template (`grep -n "commit_message" internal/config/template.go`); if the category list appears there, add `conflict_complete` to it.

- [ ] **Step 4: Run the config tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-complete && go test ./internal/config`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/tools.go internal/config/tools_test.go
git add internal/config/template.go  # only if changed
git commit -m "feat(config): accept the conflict_complete tool category"
```

---

### Task 3: wizard — the whole category defaults unchecked

**Files:**
- Modify: `internal/tui/settings_tools.go:51-57`
- Test: `internal/tui/settings_tools_test.go`

**Interfaces:**
- Consumes: `exttool.CatConflictComplete` (Task 1).
- Produces: `defaultToolChecked(rows []toolWizardRow) []bool` (same signature) with the category-level unchecked rule.

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/settings_tools_test.go`:

```go
// The whole conflict_complete category is aggressive (it completes the
// user's paused operation), so EVERY row of it starts unchecked in the
// wizard — including Kimi's, which carries no bypass flag (not OptIn).
func TestDefaultToolCheckedConflictCompleteUnchecked(t *testing.T) {
	rows := []toolWizardRow{
		{tmpl: exttool.CommandTemplate{Category: exttool.CatConflict, Name: "A"}},
		{tmpl: exttool.CommandTemplate{Category: exttool.CatConflictComplete, Name: "B", OptIn: true}},
		{tmpl: exttool.CommandTemplate{Category: exttool.CatConflictComplete, Name: "C"}}, // Kimi shape
		{tmpl: exttool.CommandTemplate{Category: exttool.CatConflictComplete, Name: "D"}, existing: true},
	}
	got := defaultToolChecked(rows)
	want := []bool{true, false, false, true} // existing rows always show checked
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d: checked = %v, want %v", i, got[i], want[i])
		}
	}
}
```

(`settings_tools_test.go` already imports `exttool`; add the import if not.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-complete && go test ./internal/tui -run TestDefaultToolCheckedConflictCompleteUnchecked -v`
Expected: FAIL — row 2 (`C`, non-OptIn conflict_complete) comes back checked.

- [ ] **Step 3: Implement**

Replace the body of `defaultToolChecked` in `internal/tui/settings_tools.go`:

```go
// defaultToolChecked computes the wizard's initial checkbox states: a new
// row defaults checked, EXCEPT an OptIn template (an aggressive
// yolo/auto-approve variant) and EVERY conflict_complete row — that whole
// category completes the user's paused operation autonomously, so adding
// any of it is an explicit opt-in even where no bypass flag exists to mark
// OptIn (Kimi). An existing row stays checked as before — it is skipped on
// apply regardless.
func defaultToolChecked(rows []toolWizardRow) []bool {
	checked := make([]bool, len(rows))
	for i, row := range rows {
		aggressive := row.tmpl.OptIn || row.tmpl.Category == exttool.CatConflictComplete
		checked[i] = row.existing || !aggressive
	}
	return checked
}
```

- [ ] **Step 4: Run the tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-complete && go test ./internal/tui -run 'TestDefaultToolChecked|TestOpenToolsWizard|TestApplyToolsWizard'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/settings_tools.go internal/tui/settings_tools_test.go
git commit -m "feat(tui): wizard defaults every conflict_complete row unchecked"
```

---

### Task 4: conflict-window picker lists the new rows (gated on a paused op)

**Files:**
- Modify: `internal/tui/tools.go` (new `completeToolChoices`), `internal/tui/conflict_process.go:175-191` (the `t` handler) and `:642` (the hints count)
- Test: `internal/tui/tools_test.go`, `internal/tui/conflict_tools_test.go`

**Interfaces:**
- Consumes: `exttool.CatConflictComplete` (Task 1); config validation (Task 2) — without it `toolUsable` marks the blocks inert.
- Produces: `completeToolChoices(cmds []config.ToolCommand, op string) []config.ToolCommand` — nil when `op == ""`, else the `WhenOp`-matching rows.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/tools_test.go`:

```go
func TestCompleteToolChoices(t *testing.T) {
	cmds := []config.ToolCommand{
		{Category: "conflict_complete", Name: "A", Mode: "terminal", Command: "x"},
		{Category: "conflict_complete", Name: "B", Mode: "terminal", WhenOp: "rebase", Command: "x"},
	}
	if got := completeToolChoices(cmds, ""); got != nil {
		t.Fatalf("no paused op: nothing to complete, got %v", got)
	}
	if got := completeToolChoices(cmds, "merge"); len(got) != 1 || got[0].Name != "A" {
		t.Fatalf("merge: want [A] (B is when_op=rebase), got %v", got)
	}
	if got := completeToolChoices(cmds, "rebase"); len(got) != 2 {
		t.Fatalf("rebase: want both rows, got %v", got)
	}
}
```

Append to `internal/tui/conflict_tools_test.go` (uses the existing `conflictModelWithTools` helper at the top of that file, which starts the process with `m.conflict.Op = "merge"`):

```go
func TestConflictTKeyIncludesCompleteRows(t *testing.T) {
	cmds := []config.ToolCommand{
		{Category: "conflict", Name: "Fix", Mode: "terminal", Command: "helper"},
		{Category: "conflict_complete", Name: "Finish (yolo)", Mode: "terminal", Command: "agent"},
	}
	m, p := conflictModelWithTools(t, cmds...)
	m, _ = p.update(m, keyRunes("t"))
	if p.st != confToolPick {
		t.Fatalf("st = %v, want confToolPick", p.st)
	}
	if len(p.toolChoices) != 2 || p.toolChoices[1].Name != "Finish (yolo)" {
		t.Fatalf("want [Fix, Finish (yolo)], got %v", p.toolChoices)
	}
}

func TestConflictTKeyCompleteRowsNeedPausedOp(t *testing.T) {
	cmds := []config.ToolCommand{
		{Category: "conflict", Name: "Fix", Mode: "terminal", Command: "helper"},
		{Category: "conflict_complete", Name: "Finish (yolo)", Mode: "terminal", Command: "agent"},
	}
	m, p := conflictModelWithTools(t, cmds...)
	p.src.Op = "" // conflicts exist but no paused sequencer op — nothing to complete
	m, _ = p.update(m, keyRunes("t"))
	if len(p.toolChoices) != 1 || p.toolChoices[0].Name != "Fix" {
		t.Fatalf("no paused op: want only the conflict row, got %v", p.toolChoices)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-complete && go test ./internal/tui -run 'TestCompleteToolChoices|TestConflictTKey' -v`
Expected: FAIL — `undefined: completeToolChoices`, and the picker tests see only one row.

- [ ] **Step 3: Implement**

3a. In `internal/tui/tools.go`, after `conflictToolChoices`:

```go
// completeToolChoices filters conflict_complete commands: they exist to
// COMPLETE a paused sequencer op, so with no paused op (op == "") there is
// nothing to offer; when_op narrows further when set. Pure, for tests.
func completeToolChoices(cmds []config.ToolCommand, op string) []config.ToolCommand {
	if op == "" {
		return nil
	}
	var out []config.ToolCommand
	for _, tc := range cmds {
		if tc.WhenOp != "" && tc.WhenOp != op {
			continue
		}
		out = append(out, tc)
	}
	return out
}
```

3b. In `internal/tui/conflict_process.go`, the `t` handler (line ~184), after the existing `choices := conflictToolChoices(...)` line add:

```go
		choices = append(choices, completeToolChoices(m.toolCommands(string(exttool.CatConflictComplete)), p.src.Op)...)
```

Add `"github.com/homeend/gigagit/internal/exttool"` to the file's imports.

3c. Same file, line ~642, the hints count includes both categories so `[t]` advertises when only complete rows are configured:

```go
	nTools := len(m.toolCommands("conflict")) + len(m.toolCommands(string(exttool.CatConflictComplete)))
	hintParts := append(conflictHints(files, sel, inProgress, nTools), i18n.T("[L] leave"), i18n.T("[z] mode"))
```

- [ ] **Step 4: Run the tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-complete && go test ./internal/tui -run 'TestCompleteToolChoices|TestConflictTKey|TestConflictHints'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/tools.go internal/tui/tools_test.go internal/tui/conflict_process.go internal/tui/conflict_tools_test.go
git commit -m "feat(tui): conflict window t picker lists resolve-and-complete rows"
```

---

### Task 5: message-file plumbing + overview viewer + i18n keys

**Files:**
- Modify: `internal/tui/tools.go` (pendingToolRun field), `internal/tui/conflict_process.go` (`buildToolRun`, `cleanupPending`, `toolFinished`), `internal/tui/model.go:2301-2314` (process-gone cleanup), `internal/i18n/lang/{ja,ko,zh,ru}.toml`
- Test: `internal/tui/conflict_tools_test.go` (also extend `cleanupToolTemp`'s patterns)

**Interfaces:**
- Consumes: `completeToolChoices` wiring (Task 4), `newReviewView(title, path, content string) *reviewView` (existing, `internal/tui/review_view.go:34`), `layerOf[*reviewView](m)` (existing generic layer accessor).
- Produces: `pendingToolRun.messageFile string`; `removeOverviewFile(pending *pendingToolRun)`; run env additionally carries `GG_MESSAGE_FILE=<path>` and `GG_TASK=conflict_complete` for this category.

- [ ] **Step 1: Extend cleanupToolTemp and write the failing tests**

In `internal/tui/conflict_tools_test.go`, add `"gg-overview-*"` to `cleanupToolTemp`'s `patterns` slice. Then append:

```go
func TestBuildToolRunCompleteCreatesOverviewFile(t *testing.T) {
	cleanupToolTemp(t)
	tc := config.ToolCommand{Category: "conflict_complete", Name: "Agent (yolo)", Mode: "terminal", Command: "agent"}
	m, p := conflictModelWithTools(t, tc)
	m, _ = p.update(m, keyRunes("t"))
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter}) // pick the only row
	if p.pending == nil {
		t.Fatal("want a pending run")
	}
	if p.pending.messageFile == "" {
		t.Fatal("conflict_complete run must create an overview file")
	}
	t.Cleanup(func() { os.Remove(p.pending.messageFile) })
	if _, err := os.Stat(p.pending.messageFile); err != nil {
		t.Fatalf("overview file must exist on disk: %v", err)
	}
	var hasMsg, hasTask bool
	for _, e := range p.pending.env {
		if e == "GG_MESSAGE_FILE="+p.pending.messageFile {
			hasMsg = true
		}
		if e == "GG_TASK=conflict_complete" {
			hasTask = true
		}
	}
	if !hasMsg || !hasTask {
		t.Fatalf("env must carry GG_MESSAGE_FILE and GG_TASK, got %v", p.pending.env)
	}
	for _, f := range p.pending.cleanup {
		if f == p.pending.messageFile {
			t.Fatal("overview file must NOT be in cleanup (the viewer needs it)")
		}
	}
}

func TestToolFinishedCompleteOpensOverviewViewer(t *testing.T) {
	m, p := conflictModelWithTools(t)
	mf, err := os.CreateTemp(t.TempDir(), "overview-*.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mf.WriteString("## Overview\nresolved a.txt (kept both hunks)\n"); err != nil {
		t.Fatal(err)
	}
	mf.Close()
	pending := &pendingToolRun{
		tc:          config.ToolCommand{Category: "conflict_complete", Name: "Agent", Mode: "terminal", Command: "agent"},
		messageFile: mf.Name(),
	}
	m2, cmd := p.toolFinished(m, toolFinishedMsg{pending: pending, start: time.Now()})
	if m2.proc != nil {
		t.Fatal("process must close so the viewer gets keys (proc preempts layers)")
	}
	if layerOf[*reviewView](m2) == nil {
		t.Fatal("want the overview open in a reviewView layer")
	}
	if cmd == nil {
		t.Fatal("want a reload cmd (state must re-derive)")
	}
	if _, err := os.Stat(mf.Name()); err != nil {
		t.Fatalf("overview file must survive for the viewer's [e]: %v", err)
	}
}

func TestToolFinishedCompleteEmptyOverviewIsStatusNote(t *testing.T) {
	m, p := conflictModelWithTools(t)
	mf, err := os.CreateTemp(t.TempDir(), "overview-*.md")
	if err != nil {
		t.Fatal(err)
	}
	mf.Close()
	pending := &pendingToolRun{
		tc:          config.ToolCommand{Category: "conflict_complete", Name: "Agent", Mode: "terminal", Command: "agent"},
		messageFile: mf.Name(),
	}
	m2, _ := p.toolFinished(m, toolFinishedMsg{pending: pending, start: time.Now()})
	if m2.proc == nil {
		t.Fatal("empty overview: the process stays open")
	}
	if m2.statusMsg == "" {
		t.Fatal("want a status note about the missing overview")
	}
	if _, err := os.Stat(mf.Name()); !os.IsNotExist(err) {
		t.Fatal("empty overview file must be removed")
	}
}

func TestToolFinishedCompleteFailureDiscardsOverview(t *testing.T) {
	m, p := conflictModelWithTools(t)
	mf, err := os.CreateTemp(t.TempDir(), "overview-*.md")
	if err != nil {
		t.Fatal(err)
	}
	mf.WriteString("partial overview from a crashed run\n")
	mf.Close()
	pending := &pendingToolRun{
		tc:          config.ToolCommand{Category: "conflict_complete", Name: "Agent", Mode: "terminal", Command: "agent"},
		messageFile: mf.Name(),
	}
	m2, _ := p.toolFinished(m, toolFinishedMsg{pending: pending, start: time.Now(), err: fmt.Errorf("exit status 1")})
	if p.st != confReporting {
		t.Fatalf("failure must report, st = %v", p.st)
	}
	if layerOf[*reviewView](m2) != nil {
		t.Fatal("a failed run must not open the viewer")
	}
	if _, err := os.Stat(mf.Name()); !os.IsNotExist(err) {
		t.Fatal("failed run's overview file must be removed")
	}
}
```

(Add `"fmt"` to the test file's imports if missing. `conflictModelWithTools` sets `m.proc = p` already — verify at its definition; if it does not, set `m.proc = p` before calling `toolFinished` in each test.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-complete && go test ./internal/tui -run 'TestBuildToolRunComplete|TestToolFinishedComplete' -v`
Expected: FAIL — `unknown field messageFile` (compile error).

- [ ] **Step 3: Implement**

3a. `internal/tui/tools.go` — add the field to `pendingToolRun`:

```go
	messageFile string // conflict_complete: the overview file ($GG_MESSAGE_FILE); kept out of cleanup — on success it backs the report viewer
```

3b. `internal/tui/conflict_process.go` — in `buildToolRun`'s `!tc.PerFile` branch (line ~299), replace the two lines that build `p.pending` with:

```go
	if !tc.PerFile {
		resolved, err := template.ResolveCommand(tc.Command, inputs, ctx)
		if err != nil {
			os.Remove(ctxFile)
			p.st = confReporting
			p.errMsg = err.Error()
			return m, nil
		}
		env := toolEnv(ctx)
		messageFile := ""
		if tc.Category == string(exttool.CatConflictComplete) {
			mf, mErr := os.CreateTemp("", "gg-overview-*.md")
			if mErr != nil {
				os.Remove(ctxFile)
				p.st = confReporting
				p.errMsg = mErr.Error()
				return m, nil
			}
			mf.Close()
			messageFile = mf.Name()
			env = append(env, "GG_MESSAGE_FILE="+messageFile, "GG_TASK=conflict_complete")
		}
		p.pending = &pendingToolRun{tc: tc, resolved: resolved, env: env, cleanup: []string{ctxFile}, messageFile: messageFile}
		return p.gateOrRun(m)
	}
```

3c. Same file — `cleanupPending` (esc from the approval box) also discards the overview file:

```go
func (p *conflictProcess) cleanupPending() {
	if p.pending == nil {
		return
	}
	for _, f := range p.pending.cleanup {
		os.Remove(f)
	}
	removeOverviewFile(p.pending)
	p.pending = nil
}
```

3d. Same file — add the helper and rework `toolFinished`. Full replacement body (current version at line ~447; the changes are the three `removeOverviewFile` calls and the new overview block before the final reload):

```go
// removeOverviewFile discards a conflict_complete run's overview temp file
// on the exits that will not open the viewer (failure / cancel / interrupt /
// empty). It is deliberately not in pending.cleanup: the success path hands
// the file to the report viewer, whose [e] open-in-editor needs it on disk.
func removeOverviewFile(pending *pendingToolRun) {
	if pending != nil && pending.messageFile != "" {
		os.Remove(pending.messageFile)
	}
}

func (p *conflictProcess) toolFinished(m Model, msg toolFinishedMsg) (Model, tea.Cmd) {
	if msg.script != "" {
		os.Remove(msg.script)
	}
	m.opCancel = nil // a capture run's cancel func; the run is over either way
	p.toolRunning = ""
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
	logToolExit(msg)
	if msg.canceled {
		removeOverviewFile(msg.pending)
		m.statusMsg = i18n.T("tool cancelled")
	} else if msg.err != nil {
		removeOverviewFile(msg.pending)
		if !toolInterruptExit(msg.err) {
			p.st = confReporting
			p.errMsg = toolExitName(msg.pending) + ": " + msg.err.Error() + outputTail(msg.output, 8)
			return m, nil
		}
		m.statusMsg = i18n.T("tool interrupted")
	}
	if msg.pending != nil && msg.pending.tc.PerFile && changed {
		p.pending = msg.pending
		p.st = confToolMark
		return m, nil
	}
	// conflict_complete: a clean exit's overview opens in the report viewer.
	// The process closes FIRST — it preempts the layer stack for keys
	// (model.go's KeyMsg routing), so a viewer pushed over it would be
	// key-dead. If the operation is still paused (the agent stopped early),
	// the ⏸ status segment and [x] lead back in, exactly as after [L] leave.
	if msg.pending != nil && msg.pending.messageFile != "" && msg.err == nil && !msg.canceled {
		data, _ := os.ReadFile(msg.pending.messageFile)
		if strings.TrimSpace(string(data)) != "" {
			m.proc = nil
			title := i18n.T("Resolution overview — %s", msg.pending.tc.Name)
			m = m.pushLayer(newReviewView(title, msg.pending.messageFile, string(data)))
			return m, m.loadCmd()
		}
		removeOverviewFile(msg.pending)
		m.statusMsg = i18n.T("%s reported no overview", msg.pending.tc.Name)
	}
	p.st = confWorking
	return m, m.loadCmd()
}
```

3e. `internal/tui/model.go` — the process-gone `toolFinishedMsg` branch (line ~2301) additionally calls `removeOverviewFile(msg.pending)` after the cleanup loop.

3f. i18n — add the two new keys to ALL FOUR bundles (`internal/i18n/lang/ja.toml`, `ko.toml`, `zh.toml`, `ru.toml`), matching each file's existing `"english key" = "translation"` format and alphabetical/section placement conventions:

```toml
# ja.toml
"Resolution overview — %s" = "解決の概要 — %s"
"%s reported no overview" = "%s は概要を報告しませんでした"
# ko.toml
"Resolution overview — %s" = "해결 개요 — %s"
"%s reported no overview" = "%s이(가) 개요를 보고하지 않았습니다"
# zh.toml
"Resolution overview — %s" = "解决概览 — %s"
"%s reported no overview" = "%s 未报告概览"
# ru.toml
"Resolution overview — %s" = "Обзор решения — %s"
"%s reported no overview" = "%s не сообщил обзор"
```

- [ ] **Step 4: Run the tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-complete && go test ./internal/tui`
Expected: PASS — the four new tests, plus the i18n gates (`i18n_scan_test.go`, `menu_labels_test.go`, `engine_prose_test.go`) staying green proves the key/bundle bookkeeping is right.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/tools.go internal/tui/conflict_process.go internal/tui/model.go internal/tui/conflict_tools_test.go internal/i18n/lang
git commit -m "feat(tui): conflict_complete runs report an overview via GG_MESSAGE_FILE"
```

---

### Task 6: docs + agent-skill sync

**Files:**
- Modify: `internal/agentskill/using-gg.md` (the "Registering yourself as a gg tool" section, ~line 330), `internal/agentskill/agentskill.go` (Version 55 → 56), `.claude/skills/using-gg/SKILL.md` (regenerated — do not hand-edit), `.claude/skills/defining-agentic-tasks/SKILL.md`, `CHANGELOG.md`, `README.md`, `CLAUDE.md`

**Interfaces:**
- Consumes: the shipped behavior of Tasks 1–5 (documentation must describe what actually runs).
- Produces: nothing code-facing.

- [ ] **Step 1: using-gg.md**

In the "Registering yourself as a gg tool" section:
- The opening sentence's task list ("three tasks") becomes four, adding: **resolving-and-completing a paused operation** (the same `t` picker; the agent resolves, stages, runs the matching `--continue` through further rebase rounds, and writes an overview to `$GG_MESSAGE_FILE`; never `--abort`).
- The TOML example's category comment becomes `# conflict | commit_message | review | conflict_complete`.
- The per-category contract list gains a `conflict_complete` entry: same env as `conflict` plus an empty `GG_MESSAGE_FILE` and `GG_TASK=conflict_complete`; expects the operation completed and the overview in the file; stop-without-completing (never abort) when unsafe.
- The structurally-invalid-block sentence's category list needs no change (it names the rule, not the list).

- [ ] **Step 2: Version bump + regenerate the tracked skill**

- `internal/agentskill/agentskill.go`: `const Version = 56`.
- Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-complete && go build ./cmd/gg && ./gg init --update`
- Verify `.claude/skills/using-gg/SKILL.md` changed (`git status`); it gets committed with this task.

- [ ] **Step 3: defining-agentic-tasks skill**

In `.claude/skills/defining-agentic-tasks/SKILL.md`:
- Add a row to "The tasks" table: `| Resolve & complete | conflict_complete | conflict window t picker | terminal handover (capture for Kimi); overview via $GG_MESSAGE_FILE |`.
- Add a contract paragraph after **conflict**: provides the conflict env plus empty `GG_MESSAGE_FILE` + `GG_TASK=conflict_complete`; expects conflicts resolved, the paused op COMPLETED (the sanctioned exception to conflict's never-continue rule — this category's agent owns the sequencer for the run), overview in the file; never `--abort`; stop-and-leave-paused when unsafe.
- In the **conflict** paragraph, after "gg's `ContinueOp` owns the sequencer", add: "(`conflict_complete` is the deliberate exception — its contract hands the sequencer to the agent)".

- [ ] **Step 4: CHANGELOG, README, CLAUDE.md**

- `CHANGELOG.md`: entry under the unreleased/top section describing the feature (new category, yolo-only rows per agent, t-picker surface, overview viewer).
- `README.md`: in the conflict-window / external-tools documentation, mention the new picker rows and what they do (find the section via `grep -n "conflict" README.md | head`).
- `CLAUDE.md`: extend the `exttool` package-map entry (new category + yolo-only invariant + Kimi caveat) and the `tui` entry's external-tools paragraph (picker lists both categories; overview opens in the report viewer; process closes first).

- [ ] **Step 5: Commit**

```bash
git add internal/agentskill .claude/skills/using-gg/SKILL.md .claude/skills/defining-agentic-tasks/SKILL.md CHANGELOG.md README.md CLAUDE.md
git commit -m "docs: conflict_complete category — using-gg v56, skills, changelog"
```

---

### Task 7: full suite + real-binary spot-check

**Files:** none new.

- [ ] **Step 1: Full staged suite**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-complete && ./test.sh`
Expected: vet+gofmt, unit, e2e all PASS. Fix anything that fails before proceeding (report failures honestly).

- [ ] **Step 2: Real-binary verification (the adding-external-tools rule)**

For each agent binary present on this machine (`command -v claude junie codex agy kimi`), sanity-check the flags the templates use actually exist in `--help` output (e.g. `claude --help | grep -c dangerously-skip-permissions`). Record findings as dated comments on the new template constants (the catalog convention). The full end-to-end run against a real paused rebase is a HUMAN step — flag it in the completion report, including the spec's Kimi conditional: if Kimi's print mode is shown not to run the `--continue` commands, delete its row and its test expectations.

- [ ] **Step 3: Build and deliver a test binary**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-conflict-complete && go build -o gg ./cmd/gg`
Deliver `/mnt/t/others/gigagit.worktrees/feat-conflict-complete/gg` to the user (SendUserFile, absolute path) so they can exercise the flow against a real conflicted repo before deciding on the merge. Do NOT merge — the user owns merging.

---

## Self-review notes

- Spec coverage: category+contract (T1/T5), yolo-only rows incl. Kimi conditional (T1, T7), wizard unchecked (T3), picker gating on paused op (T4), approval gate (already shared — no change needed, verified by existing `TestToolPickEnterResolvesAndAsksApproval`), message-file + viewer + empty-file note (T5), no CLI/MCP surface (nothing added — by design), sync rule + docs (T6), testing strategy (each task + T7).
- The capture lane (Kimi) reuses `execCaptureToolCmd` unchanged; `toolFinished` handles both modes through the same message, so T5's logic covers it without extra code.
- `tui-capture` scenario for the picker was considered and dropped: the unit tests in T4 drive the exact same `update` path the harness would, without needing a live conflicted repo.
