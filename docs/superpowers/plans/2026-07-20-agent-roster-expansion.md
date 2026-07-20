# Agent Roster Expansion (Codex, Antigravity, Kimi Code) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect three newly installed AI agents (OpenAI Codex, Google Antigravity `agy`, Moonshot Kimi Code) in gg's agent-skills picker and ship live-verified external-tools catalog entries for the conflict / commit_message / review categories.

**Architecture:** Two hardcoded registries grow by one entry per agent — `agentinit.Builtins()` (skill picker) and `exttool.Builtins()` (tool catalog); no TUI changes anywhere (pickers/wizard consume the registries generically). One small mechanism change: `exttool.Detect` learns `~/`-prefixed `ExtraProbes` via an injected home dir.

**Tech Stack:** Go 1.26, stdlib only. Pure unit tests (no git, no network).

**Spec:** `docs/superpowers/specs/2026-07-20-agent-roster-expansion-design.md` (all output-shape probes already done — the templates below are the verified shapes; do NOT "improve" flags or prompt wording).

## Global Constraints

- Command templates: dynamic content ONLY via `<env:GG_*>` generation tokens and the `<range>` prose token inside double-quoted prompt text; never a raw prose token (enforced by `TestBuiltinTemplateTokensValidate`).
- Prompt precedes flags in every template (`<bin> [subcmd] "<prompt>" [flags]`).
- OptIn invariant (new, replaces name-based rule): a template whose command contains a permission-bypass token (`--dangerously-`, `--yolo`, `--brave`) MUST have `OptIn: true`, and every `OptIn: true` template MUST contain one. Bypass flags are always written in long form.
- Antigravity capture templates carry `--dangerously-skip-permissions` AND `OptIn: true` together — the pairing is the safety property (spec Part 2).
- Every new catalog entry carries an evidence comment quoting the real binary's version, date, and probe outcome (the adding-external-tools rule). Versions: codex-cli 0.144.6, agy 1.1.4, kimi 0.27.0, all probed 2026-07-20.
- Exact registry values (spec Part 1):
  `{ID: "antigravity", Label: "Antigravity (global)", Detect: "~/.gemini/antigravity-cli", Target: "~/.gemini/config/skills/using-gg/SKILL.md", Mode: ModeSkillFile}` and
  `{ID: "kimi", Label: "Kimi Code (global)", Detect: "~/.kimi-code", Target: "~/.kimi-code/skills/using-gg/SKILL.md", Mode: ModeSkillFile}`.
- `internal/exttool` and `internal/agentinit` stay archtest leaves — no new imports beyond stdlib (`path/filepath` is fine).
- Run `gofmt -l` on touched packages before each commit; each task ends with its package's tests green.

---

### Task 1: `ExtraProbes` home expansion in `exttool.Detect`

**Files:**
- Modify: `internal/exttool/exttool.go` (Detect + new helper)
- Modify: `internal/tui/settings_tools.go:33` (the only production call site)
- Test: `internal/exttool/exttool_test.go`

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces: `Detect(look func(string) (string, error), stat func(string) (os.FileInfo, error), home string) []Detection` — third param `home`; `~/`-prefixed probes expand against it, empty `home` skips them. Also unexported `detectIn(tools []Tool, look, stat, home)` so tests can exercise a synthetic tool (Task 4's kimi entry is the first builtin `~/` probe, and this task must not depend on it).

- [ ] **Step 1: Write the failing tests**

Append to `internal/exttool/exttool_test.go`:

```go
func TestDetectTildeProbeExpandsAgainstHome(t *testing.T) {
	tool := Tool{ID: "fake", Label: "Fake", Bins: []string{"fakebin"},
		ExtraProbes: []string{"~/.fake/bin/fake"},
		Commands:    []CommandTemplate{{Category: CatConflict, Name: "Fake", Mode: ModeTerminal, Command: "<bin>"}}}

	// ~/ probe expands against home; the expanded absolute path is the Bin.
	want := filepath.Join("/home/u", ".fake", "bin", "fake")
	dets := detectIn([]Tool{tool}, fakeLook(nil), fakeStat(map[string]bool{want: true}), "/home/u")
	if len(dets) != 1 || dets[0].Bin != want {
		t.Fatalf("dets = %+v, want one detection with Bin=%q", dets, want)
	}

	// Empty home skips ~/ probes entirely (hermeticity — tests must never
	// resolve against the developer's real home).
	dets = detectIn([]Tool{tool}, fakeLook(nil), fakeStat(map[string]bool{want: true}), "")
	if len(dets) != 0 {
		t.Fatalf("empty home must skip ~/ probes, got %+v", dets)
	}

	// A PATH hit still wins over the probe and keeps the bare name.
	dets = detectIn([]Tool{tool}, fakeLook(map[string]string{"fakebin": "/usr/bin/fakebin"}), fakeStat(nil), "/home/u")
	if len(dets) != 1 || dets[0].Bin != "fakebin" {
		t.Fatalf("PATH hit must win with bare name, got %+v", dets)
	}
}
```

Add `"path/filepath"` to the test file's imports. Update the TWO existing
`Detect(` calls in this file (`TestDetectFindsBinsOnPath`,
`TestDetectExtraProbeYieldsAbsolutePath`) to pass a third argument `""`
(they exercise absolute probes / PATH only).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/exttool/ -run 'TestDetect' -v`
Expected: FAIL — `undefined: detectIn` (and a compile error on the 2-arg `Detect` calls until Step 3 lands; that is fine, compile failure IS the red state).

- [ ] **Step 3: Implement**

In `internal/exttool/exttool.go`, add `"path/filepath"` to imports and replace the whole `Detect` function with:

```go
// Detect probes the catalog with injected lookups (exec.LookPath / os.Stat in
// production — the clipboard nativeArgv seam pattern) so tests never touch the
// developer's machine. First Bins hit wins; ExtraProbes are consulted only
// when no Bins name resolves. A probe with a "~/" prefix expands against
// home (empty home skips it — the agentinit hermeticity rule); this covers
// installs whose PATH entry lives only in a shell rc file, like Kimi Code's
// ~/.kimi-code/bin.
func Detect(look func(string) (string, error), stat func(string) (os.FileInfo, error), home string) []Detection {
	return detectIn(Builtins(), look, stat, home)
}

func detectIn(tools []Tool, look func(string) (string, error), stat func(string) (os.FileInfo, error), home string) []Detection {
	var out []Detection
	for _, tl := range tools {
		bin := ""
		for _, b := range tl.Bins {
			if _, err := look(b); err == nil {
				bin = b
				break
			}
		}
		if bin == "" {
			for _, p := range tl.ExtraProbes {
				if strings.HasPrefix(p, "~/") {
					if home == "" {
						continue
					}
					p = filepath.Join(home, p[2:])
				}
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
```

In `internal/tui/settings_tools.go`, replace the call site (line ~33):

```go
	home, _ := os.UserHomeDir()
	for _, det := range exttool.Detect(exec.LookPath, os.Stat, home) {
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/exttool/ ./internal/tui/ 2>&1 | tail -5`
Expected: both packages PASS (tui compiles and its wizard tests, which build `Detection` values directly, are unaffected).

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/exttool internal/tui   # must print nothing
git add internal/exttool internal/tui
git commit -m "feat(exttool): ~/-prefixed ExtraProbes expand against an injected home"
```

---

### Task 2: Codex catalog entry

**Files:**
- Modify: `internal/exttool/exttool.go` (consts + one `Builtins()` entry)
- Test: `internal/exttool/exttool_test.go`

**Interfaces:**
- Consumes: nothing from Task 1 (Codex uses `Bins` only).
- Produces: catalog entry `ID: "codex"`; const names `codexConflictPrompt`, `codexConflictCommand`, `codexConflictYoloCommand`, `codexCommitPrompt`, `codexCommitCommand`, `codexReviewPrompt`, `codexReviewCommand`.

- [ ] **Step 1: Write the failing pin test**

Append to `internal/exttool/exttool_test.go`:

```go
// TestCodexTemplates pins the verified codex shapes (codex-cli 0.144.6,
// probed 2026-07-20): exec is the capture lane, the final message arrives
// via --output-last-message (the native GG_MESSAGE_FILE channel), and the
// file argument is double-quoted in the template so a temp path with spaces
// cannot word-split — the first standalone <env:> use in the catalog.
func TestCodexTemplates(t *testing.T) {
	var codex Tool
	for _, tl := range Builtins() {
		if tl.ID == "codex" {
			codex = tl
		}
	}
	if codex.ID == "" {
		t.Fatal("codex not in catalog")
	}
	byName := map[string]CommandTemplate{}
	for _, ct := range codex.Commands {
		byName[string(ct.Category)+"/"+ct.Name] = ct
	}

	commit := byName["commit_message/Codex"]
	gen := GenerateCommandFor(commit, "codex", "linux")
	if !strings.HasPrefix(gen, `codex exec "`) {
		t.Fatalf("codex commit prompt not first after exec: %q", gen)
	}
	if !strings.Contains(gen, `--output-last-message "${GG_MESSAGE_FILE}"`) {
		t.Fatalf("codex commit must write the quoted message file: %q", gen)
	}
	if !strings.Contains(gen, "--sandbox read-only") {
		t.Fatalf("codex capture lanes must be read-only sandboxed: %q", gen)
	}

	review := byName["review/Codex"]
	gr := GenerateCommandFor(review, "codex", "linux")
	if !strings.Contains(gr, "${GG_REVIEW_DIFF}") || !strings.Contains(gr, "<range>") {
		t.Fatalf("codex review must read GG_REVIEW_DIFF and label <range>: %q", gr)
	}
	if !strings.Contains(gr, `--output-last-message "${GG_MESSAGE_FILE}"`) {
		t.Fatalf("codex review must write the quoted message file: %q", gr)
	}

	yolo := byName["conflict/Codex (yolo)"]
	if !yolo.OptIn {
		t.Fatal("codex yolo conflict must be OptIn")
	}
	if !strings.Contains(yolo.Command, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("codex yolo must bypass approvals: %q", yolo.Command)
	}
	if def := byName["conflict/Codex"]; def.OptIn || strings.Contains(def.Command, "--dangerously-") {
		t.Fatalf("default codex conflict must not bypass approvals: %+v", def)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/exttool/ -run TestCodexTemplates -v`
Expected: FAIL — "codex not in catalog".

- [ ] **Step 3: Add the consts and entry**

In `internal/exttool/exttool.go`, after the `junieReviewCommand` const block, add:

```go
// Codex templates — verified against the REAL binary, codex-cli 0.144.6,
// 2026-07-20. `codex --help`: positional [PROMPT] starts an interactive
// session; --dangerously-bypass-approvals-and-sandbox exists on the
// interactive CLI. `codex exec --help`: non-interactive; -s/--sandbox
// read-only; -o/--output-last-message <FILE> "file where the last message
// from the agent should be written". Live probe (authenticated, inside a
// git repo, stdin /dev/null): exit 0, the message file held exactly the
// final message, stdout carried only the final message (session log on
// stderr). The trust gate fires only OUTSIDE a git repo, so no
// --skip-git-repo-check.
const codexConflictPrompt = `"A git <env:GG_OP> operation is paused with conflicts in this repository. Read the context file at <env:GG_CONTEXT_FILE> for the operation's parties and the conflicted paths. Inspect both sides' history to understand intent, resolve each conflict by editing the files, then run git add on each resolved file. Do NOT run git commit or any --continue command - stop when everything is staged and summarize what you chose and why."`

const codexConflictCommand = `<bin> ` + codexConflictPrompt

const codexConflictYoloCommand = codexConflictCommand + ` --dangerously-bypass-approvals-and-sandbox`

// codexCommitCommand: the final assistant message IS the deliverable —
// codex's harness (not the sandboxed agent) writes it to $GG_MESSAGE_FILE
// via --output-last-message, which the engine prefers over stdout. The file
// argument is double-quoted in the template (the first standalone <env:>
// use in the catalog) so a temp path with spaces cannot word-split.
const codexCommitPrompt = `"Write a git commit message for the staged changes. Read the summary at <env:GG_CONTEXT_FILE> (files changed, recent-commit style) and, for detail, the full diff at <env:GG_STAGED_DIFF>. Your final message must be ONLY the commit message and nothing else: a concise imperative subject line (max ~72 chars), a blank line, then a short body explaining what changed and why. No preamble, no markdown headings, no code fences. If the diff file notes it was truncated, inspect specific files with git."`

const codexCommitCommand = `<bin> exec ` + codexCommitPrompt + ` --sandbox read-only --output-last-message "<env:GG_MESSAGE_FILE>"`

const codexReviewPrompt = `"You are reviewing a code change. The full diff to review is in the file at <env:GG_REVIEW_DIFF> (range <range>). Your final message must be ONLY a concise code review - findings with severity and a short summary. Do NOT modify any repository files and do NOT run git commit."`

const codexReviewCommand = `<bin> exec ` + codexReviewPrompt + ` --sandbox read-only --output-last-message "<env:GG_MESSAGE_FILE>"`
```

In `Builtins()`, insert after the junie entry (before meld — agents grouped, mergetool last):

```go
		{
			ID: "codex", Label: "OpenAI Codex", Bins: []string{"codex"},
			Commands: []CommandTemplate{
				{Category: CatConflict, Name: "Codex", Mode: ModeTerminal, Command: codexConflictCommand},
				{Category: CatConflict, Name: "Codex (yolo)", Mode: ModeTerminal, OptIn: true, Command: codexConflictYoloCommand},
				{Category: CatCommitMessage, Name: "Codex", Mode: ModeCapture, Command: codexCommitCommand},
				{Category: CatReview, Name: "Codex", Mode: ModeCapture, Command: codexReviewCommand},
			},
		},
```

- [ ] **Step 4: Run the package tests**

Run: `go test ./internal/exttool/ -v 2>&1 | tail -15`
Expected: all PASS — including the generic invariants and the still-name-based `TestOptInMarksExactlyTheYoloVariants` (codex's OptIn row is named "(yolo)", so it satisfies the old rule; the rule changes in Task 3).

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/exttool
git add internal/exttool
git commit -m "feat(exttool): OpenAI Codex catalog entry — exec capture via --output-last-message"
```

---

### Task 3: Antigravity catalog entry + OptIn invariant rewrite

**Files:**
- Modify: `internal/exttool/exttool.go` (consts + one `Builtins()` entry)
- Modify: `internal/exttool/exttool_test.go` (`TestOptInMarksExactlyTheYoloVariants` → semantic rewrite; drop the `no yolo for capture lane` fatal in `TestBuiltinsCommitMessageTemplates`)

**Interfaces:**
- Consumes: nothing from earlier tasks (Antigravity uses `Bins` only).
- Produces: catalog entry `ID: "antigravity"`; const names `agyConflictPrompt`, `agyConflictCommand`, `agyConflictYoloCommand`, `agyCommitPrompt`, `agyCommitCommand`, `agyReviewPrompt`, `agyReviewCommand`; test `TestOptInMarksExactlyThePermissionBypassVariants` (replaces the name-based test).

- [ ] **Step 1: Write the failing pin test**

Append to `internal/exttool/exttool_test.go`:

```go
// TestAntigravityTemplates pins the verified agy shapes (agy 1.1.4, probed
// 2026-07-20). Headless -p AUTO-DENIES every permission-gated tool (even
// read_file on gg's context files, which live outside the workspace);
// --mode accept-edits does not lift it and agy has no CLI allowlist flag.
// The only per-run remedy is --dangerously-skip-permissions, so BOTH
// capture templates carry it and are OptIn — the pairing is the safety
// property. The conflict default runs interactively (TTY) where agy
// prompts normally, so it carries no bypass flag.
func TestAntigravityTemplates(t *testing.T) {
	var agy Tool
	for _, tl := range Builtins() {
		if tl.ID == "antigravity" {
			agy = tl
		}
	}
	if agy.ID == "" {
		t.Fatal("antigravity not in catalog")
	}
	if len(agy.Bins) != 1 || agy.Bins[0] != "agy" {
		t.Fatalf("antigravity Bins = %v, want [agy]", agy.Bins)
	}
	for _, ct := range agy.Commands {
		if ct.Mode == ModeCapture {
			if !ct.OptIn || !strings.Contains(ct.Command, "--dangerously-skip-permissions") {
				t.Errorf("%s/%s: capture lane must pair OptIn with --dangerously-skip-permissions: OptIn=%v cmd=%q",
					ct.Category, ct.Name, ct.OptIn, ct.Command)
			}
			if !strings.Contains(ct.Command, "${GG_MESSAGE_FILE}") && !strings.Contains(ct.Command, "<env:GG_MESSAGE_FILE>") {
				t.Errorf("%s/%s: capture lane must use the GG_MESSAGE_FILE channel: %q", ct.Category, ct.Name, ct.Command)
			}
		}
	}
	byName := map[string]CommandTemplate{}
	for _, ct := range agy.Commands {
		byName[string(ct.Category)+"/"+ct.Name] = ct
	}
	def := byName["conflict/Antigravity"]
	if def.OptIn || strings.Contains(def.Command, "--dangerously-") {
		t.Fatalf("default conflict lane must not bypass permissions: %+v", def)
	}
	if !strings.Contains(def.Command, "--prompt-interactive") {
		t.Fatalf("conflict lane must pre-submit the prompt interactively: %q", def.Command)
	}
	gen := GenerateCommandFor(byName["commit_message/Antigravity"], "agy", "linux")
	if !strings.HasPrefix(gen, `agy -p "`) {
		t.Fatalf("agy commit prompt not first after -p: %q", gen)
	}
	if !strings.Contains(gen, "${GG_CONTEXT_FILE}") || !strings.Contains(gen, "${GG_STAGED_DIFF}") {
		t.Fatalf("agy commit missing input env refs: %q", gen)
	}
	gr := GenerateCommandFor(byName["review/Antigravity"], "agy", "linux")
	if !strings.Contains(gr, "${GG_REVIEW_DIFF}") || !strings.Contains(gr, "<range>") {
		t.Fatalf("agy review must read GG_REVIEW_DIFF and label <range>: %q", gr)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/exttool/ -run TestAntigravityTemplates -v`
Expected: FAIL — "antigravity not in catalog".

- [ ] **Step 3: Add the consts and entry**

In `internal/exttool/exttool.go`, after the codex const block, add:

```go
// Antigravity templates — verified against the REAL binary, agy 1.1.4,
// 2026-07-20. `agy --help`: -p/--print runs one prompt non-interactively;
// -i/--prompt-interactive runs an initial prompt and stays interactive;
// --dangerously-skip-permissions auto-approves tool permission requests.
// Live probes (authenticated): headless -p AUTO-DENIES permission-gated
// tools ("a tool required the \"read_file\" permission that headless mode
// cannot prompt for, so it was auto-denied") — even reading gg's context
// files, which live outside the workspace; --mode accept-edits does NOT
// lift the denial; there is no CLI allowlist flag (only settings.json
// permissions.allow, user config gg must not edit). With
// --dangerously-skip-permissions the outside-workspace read AND the
// GG_MESSAGE_FILE write both succeeded exactly. --sandbox was probed and
// REJECTED: it polluted stdout with agent narration. Because the capture
// lanes bypass agy's own permission prompts they are OptIn (wizard shows
// them unchecked); the interactive conflict default needs no flag — agy
// prompts in-terminal under gg's handover.
const agyConflictPrompt = `"A git <env:GG_OP> operation is paused with conflicts in this repository. Read the context file at <env:GG_CONTEXT_FILE> for the operation's parties and the conflicted paths. Inspect both sides' history to understand intent, resolve each conflict by editing the files, then run git add on each resolved file. Do NOT run git commit or any --continue command - stop when everything is staged and summarize what you chose."`

const agyConflictCommand = `<bin> --prompt-interactive ` + agyConflictPrompt

const agyConflictYoloCommand = agyConflictCommand + ` --dangerously-skip-permissions`

// The capture lanes use the file channel (the Junie contract: the engine
// prefers a non-empty $GG_MESSAGE_FILE over stdout) because agy's -p stdout
// was observed narration-prefixed in one probe and clean in another — not
// reliably parseable — while the probed file write delivered the payload
// byte-exact.
const agyCommitPrompt = `"Write a git commit message for the staged changes into the file at <env:GG_MESSAGE_FILE> (an absolute path outside the repository). The change summary is at <env:GG_CONTEXT_FILE> and the full diff at <env:GG_STAGED_DIFF>. Write ONLY the commit message to that file: a concise imperative subject line (max ~72 chars), a blank line, then a short body. Do not run git commit and do not modify any other files."`

const agyCommitCommand = `<bin> -p ` + agyCommitPrompt + ` --dangerously-skip-permissions`

const agyReviewPrompt = `"You are reviewing a code change. The full diff to review is in the file at <env:GG_REVIEW_DIFF> (range <range>). Write a concise code review - findings with severity and a short summary - into the file at <env:GG_MESSAGE_FILE> (an absolute path outside the repository). Do NOT modify any repository files and do NOT run git commit."`

const agyReviewCommand = `<bin> -p ` + agyReviewPrompt + ` --dangerously-skip-permissions`
```

In `Builtins()`, insert after the codex entry:

```go
		{
			ID: "antigravity", Label: "Antigravity", Bins: []string{"agy"},
			Commands: []CommandTemplate{
				{Category: CatConflict, Name: "Antigravity", Mode: ModeTerminal, Command: agyConflictCommand},
				{Category: CatConflict, Name: "Antigravity (yolo)", Mode: ModeTerminal, OptIn: true, Command: agyConflictYoloCommand},
				{Category: CatCommitMessage, Name: "Antigravity", Mode: ModeCapture, OptIn: true, Command: agyCommitCommand},
				{Category: CatReview, Name: "Antigravity", Mode: ModeCapture, OptIn: true, Command: agyReviewCommand},
			},
		},
```

- [ ] **Step 4: Rewrite the OptIn invariant test**

The old rule (OptIn ⇔ name contains "(yolo)") and the capture-lane blanket
(`no yolo for capture lane`) are both invalidated by design: Antigravity's
capture rows are OptIn without "(yolo)" in the name. Replace them with the
semantic invariant.

In `internal/exttool/exttool_test.go`, REPLACE the whole
`TestOptInMarksExactlyTheYoloVariants` function with:

```go
// TestOptInMarksExactlyThePermissionBypassVariants: OptIn's meaning is "this
// template bypasses the agent's own permission prompts" — the wizard shows
// such rows unchecked. That is exactly the set of templates carrying a
// bypass flag: --dangerously-* (claude, codex, antigravity), --yolo (kimi),
// --brave (junie). Name-based "(yolo)" matching stopped being the rule when
// antigravity's capture lanes (which NEED the bypass to work headless at
// all) joined as OptIn rows without the suffix.
func TestOptInMarksExactlyThePermissionBypassVariants(t *testing.T) {
	bypass := func(cmd string) bool {
		return strings.Contains(cmd, "--dangerously-") ||
			strings.Contains(cmd, "--yolo") ||
			strings.Contains(cmd, "--brave")
	}
	for _, tl := range Builtins() {
		for _, ct := range tl.Commands {
			if want := bypass(ct.Command); ct.OptIn != want {
				t.Errorf("%s/%s: OptIn = %v, want %v (OptIn ⇔ a permission-bypass flag)", tl.ID, ct.Name, ct.OptIn, want)
			}
			if strings.Contains(ct.Name, "(yolo)") && !ct.OptIn {
				t.Errorf("%s/%s: a (yolo)-named variant must be OptIn", tl.ID, ct.Name)
			}
		}
	}
}
```

In `TestBuiltinsCommitMessageTemplates`, DELETE these three lines (the OptIn
semantics now live in the invariant test above):

```go
			if c.OptIn {
				t.Fatalf("%s: no yolo for capture lane", c.Name)
			}
```

- [ ] **Step 5: Run the package tests**

Run: `go test ./internal/exttool/ -v 2>&1 | tail -15`
Expected: all PASS, including the renamed invariant and `TestAntigravityTemplates`.

- [ ] **Step 6: Commit**

```bash
gofmt -l internal/exttool
git add internal/exttool
git commit -m "feat(exttool): Antigravity catalog entry — OptIn capture lanes pair with permission bypass"
```

---

### Task 4: Kimi Code catalog entry

**Files:**
- Modify: `internal/exttool/exttool.go` (consts + one `Builtins()` entry)
- Test: `internal/exttool/exttool_test.go`

**Interfaces:**
- Consumes: Task 1's `~/` `ExtraProbes` expansion (kimi ships the first builtin `~/` probe).
- Produces: catalog entry `ID: "kimi"`; const names `kimiConflictPrompt`, `kimiConflictCommand`, `kimiConflictYoloCommand`, `kimiCommitPrompt`, `kimiCommitCommand`, `kimiReviewPrompt`, `kimiReviewCommand`.

- [ ] **Step 1: Write the failing pin test**

Append to `internal/exttool/exttool_test.go`:

```go
// TestKimiTemplates pins the verified kimi shapes (kimi 0.27.0, probed
// 2026-07-20): -p stdout is decorated (responses arrive prefixed "• "), so
// both capture lanes deliver through the GG_MESSAGE_FILE channel, which a
// plain -p run can write (outside-workspace read+write and headless
// `git add` were all probed working with no approval flags). Kimi has no
// interactive-with-prompt flag, so the conflict lane is a headless -p run
// under the normal terminal handover.
func TestKimiTemplates(t *testing.T) {
	var kimi Tool
	for _, tl := range Builtins() {
		if tl.ID == "kimi" {
			kimi = tl
		}
	}
	if kimi.ID == "" {
		t.Fatal("kimi not in catalog")
	}
	if len(kimi.ExtraProbes) != 1 || kimi.ExtraProbes[0] != "~/.kimi-code/bin/kimi" {
		t.Fatalf("kimi ExtraProbes = %v, want the ~/.kimi-code install probe", kimi.ExtraProbes)
	}
	byName := map[string]CommandTemplate{}
	for _, ct := range kimi.Commands {
		byName[string(ct.Category)+"/"+ct.Name] = ct
	}
	for _, key := range []string{"commit_message/Kimi", "review/Kimi"} {
		gen := GenerateCommandFor(byName[key], "kimi", "linux")
		if !strings.HasPrefix(gen, `kimi -p "`) {
			t.Fatalf("%s: prompt not first after -p: %q", key, gen)
		}
		if !strings.Contains(gen, "${GG_MESSAGE_FILE}") {
			t.Fatalf("%s: must write to GG_MESSAGE_FILE (stdout is •-decorated): %q", key, gen)
		}
		if strings.Contains(gen, "--yolo") || strings.Contains(gen, "--auto") {
			t.Fatalf("%s: plain -p suffices, no approval flags (probed): %q", key, gen)
		}
	}
	if !strings.Contains(GenerateCommandFor(byName["commit_message/Kimi"], "kimi", "linux"), "${GG_STAGED_DIFF}") {
		t.Fatal("kimi commit must read GG_STAGED_DIFF")
	}
	gr := GenerateCommandFor(byName["review/Kimi"], "kimi", "linux")
	if !strings.Contains(gr, "${GG_REVIEW_DIFF}") || !strings.Contains(gr, "<range>") {
		t.Fatalf("kimi review must read GG_REVIEW_DIFF and label <range>: %q", gr)
	}
	yolo := byName["conflict/Kimi (yolo)"]
	if !yolo.OptIn || !strings.Contains(yolo.Command, "--yolo") {
		t.Fatalf("kimi yolo conflict must be OptIn with --yolo: %+v", yolo)
	}
	if def := byName["conflict/Kimi"]; def.OptIn || strings.Contains(def.Command, "--yolo") {
		t.Fatalf("default kimi conflict must not auto-approve: %+v", def)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/exttool/ -run TestKimiTemplates -v`
Expected: FAIL — "kimi not in catalog".

- [ ] **Step 3: Add the consts and entry**

In `internal/exttool/exttool.go`, after the antigravity const block, add:

```go
// Kimi Code templates — verified against the REAL binary, kimi 0.27.0,
// 2026-07-20. `kimi --help`: -p/--prompt runs one prompt non-interactively
// and prints the response; -y/--yolo auto-approves all actions; there is NO
// interactive-with-initial-prompt flag, so the conflict lane is a headless
// -p run in the real terminal under the normal handover. Live probes
// (authenticated): plain -p edited a file and ran `git add` headlessly
// (staged entry confirmed, exit 0); plain -p read AND wrote files outside
// the workspace with no approval flags; -p stdout decorates the response
// with a leading "• " (which would corrupt a commit subject), so both
// capture lanes deliver through the $GG_MESSAGE_FILE channel instead of
// stdout.
const kimiConflictPrompt = `"A git <env:GG_OP> operation is paused with conflicts in this repository. Read the context file at <env:GG_CONTEXT_FILE> for the operation's parties and the conflicted paths. Inspect both sides' history to understand intent, resolve each conflict by editing the files, then run git add on each resolved file. Do NOT run git commit or any --continue command - stop when everything is staged and summarize what you chose."`

const kimiConflictCommand = `<bin> -p ` + kimiConflictPrompt

const kimiConflictYoloCommand = kimiConflictCommand + ` --yolo`

const kimiCommitPrompt = `"Write a git commit message for the staged changes into the file at <env:GG_MESSAGE_FILE> (an absolute path outside the repository). The change summary is at <env:GG_CONTEXT_FILE> and the full diff at <env:GG_STAGED_DIFF>. Write ONLY the commit message to that file: a concise imperative subject line (max ~72 chars), a blank line, then a short body. Do not run git commit and do not modify any other files."`

const kimiCommitCommand = `<bin> -p ` + kimiCommitPrompt

const kimiReviewPrompt = `"You are reviewing a code change. The full diff to review is in the file at <env:GG_REVIEW_DIFF> (range <range>). Write a concise code review - findings with severity and a short summary - into the file at <env:GG_MESSAGE_FILE> (an absolute path outside the repository). Do NOT modify any repository files and do NOT run git commit."`

const kimiReviewCommand = `<bin> -p ` + kimiReviewPrompt
```

In `Builtins()`, insert after the antigravity entry (meld stays last):

```go
		{
			ID: "kimi", Label: "Kimi Code", Bins: []string{"kimi"},
			ExtraProbes: []string{"~/.kimi-code/bin/kimi"},
			Commands: []CommandTemplate{
				{Category: CatConflict, Name: "Kimi", Mode: ModeTerminal, Command: kimiConflictCommand},
				{Category: CatConflict, Name: "Kimi (yolo)", Mode: ModeTerminal, OptIn: true, Command: kimiConflictYoloCommand},
				{Category: CatCommitMessage, Name: "Kimi", Mode: ModeCapture, Command: kimiCommitCommand},
				{Category: CatReview, Name: "Kimi", Mode: ModeCapture, Command: kimiReviewCommand},
			},
		},
```

- [ ] **Step 4: Run the package tests**

Run: `go test ./internal/exttool/ -v 2>&1 | tail -15`
Expected: all PASS — note `TestOptInMarksExactlyThePermissionBypassVariants` now also validates kimi (`--yolo` ⇒ OptIn on the yolo row, absent from the plain rows).

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/exttool
git add internal/exttool
git commit -m "feat(exttool): Kimi Code catalog entry — file-channel capture, ~/ install probe"
```

---

### Task 5: agent-skills picker rows (agentinit)

**Files:**
- Modify: `internal/agentinit/agentinit.go` (`Builtins()`)
- Test: `internal/agentinit/agentinit_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks (separate package).
- Produces: registry rows `antigravity` and `kimi` (exact values in Global Constraints).

- [ ] **Step 1: Write the failing tests**

Append to `internal/agentinit/agentinit_test.go` (same fixture/byID helpers as `TestJunieGlobalDetectedFromHome`; the fixture's dir list creates nested paths):

```go
func TestAntigravityDetectedFromHome(t *testing.T) {
	// agy's home is the agy-specific ~/.gemini/antigravity-cli (created by
	// its install), NOT plain ~/.gemini — a dead gemini-cli install also
	// creates that. The skill lands under agy's documented global
	// customization root ~/.gemini/config/skills/.
	proj, home := fixture(t, nil, []string{".gemini/antigravity-cli"})
	d, ok := byID(Detect(proj, home), "antigravity")
	if !ok {
		t.Fatal("antigravity not detected from ~/.gemini/antigravity-cli")
	}
	if d.Agent.Mode != ModeSkillFile {
		t.Fatalf("antigravity mode = %v, want ModeSkillFile", d.Agent.Mode)
	}
	if err := Install(d); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".gemini", "config", "skills", "using-gg", "SKILL.md"))
	if err != nil {
		t.Fatalf("global skill not installed: %v", err)
	}
	if !strings.Contains(string(data), "name: using-gg") {
		t.Error("antigravity SKILL.md missing frontmatter")
	}
}

func TestAntigravityNotDetectedFromBareGeminiDir(t *testing.T) {
	proj, home := fixture(t, nil, []string{".gemini"})
	if _, ok := byID(Detect(proj, home), "antigravity"); ok {
		t.Error("bare ~/.gemini (gemini-cli leftover) must not detect antigravity")
	}
}

func TestKimiDetectedFromHome(t *testing.T) {
	proj, home := fixture(t, nil, []string{".kimi-code"})
	d, ok := byID(Detect(proj, home), "kimi")
	if !ok {
		t.Fatal("kimi not detected from ~/.kimi-code")
	}
	if d.Agent.Mode != ModeSkillFile {
		t.Fatalf("kimi mode = %v, want ModeSkillFile", d.Agent.Mode)
	}
	if err := Install(d); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".kimi-code", "skills", "using-gg", "SKILL.md")); err != nil {
		t.Fatalf("global skill not installed: %v", err)
	}
}
```

(`fixture` already uses `os.MkdirAll` for home paths, so the nested
`.gemini/antigravity-cli` dir needs no helper change — verified 2026-07-20.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/agentinit/ -run 'TestAntigravity|TestKimi' -v`
Expected: FAIL — "antigravity not detected…" / "kimi not detected…".

- [ ] **Step 3: Add the registry rows**

In `internal/agentinit/agentinit.go` `Builtins()`, insert directly after the `codex` row:

```go
		// Antigravity CLI (agy 1.1.4, verified 2026-07-20): skills are
		// skills/<name>/SKILL.md with name+description frontmatter under a
		// customization root; the global root is ~/.gemini/config/ (per the
		// bundled agy-customizations docs). Detect the agy-specific home,
		// not plain ~/.gemini (gemini-cli also creates that).
		{ID: "antigravity", Label: "Antigravity (global)", Detect: "~/.gemini/antigravity-cli", Target: "~/.gemini/config/skills/using-gg/SKILL.md", Mode: ModeSkillFile},
		// Kimi Code (kimi 0.27.0, verified 2026-07-20): user skills are
		// auto-discovered from ~/.kimi-code/skills/ (its --skills-dir help
		// and binary strings document the path).
		{ID: "kimi", Label: "Kimi Code (global)", Detect: "~/.kimi-code", Target: "~/.kimi-code/skills/using-gg/SKILL.md", Mode: ModeSkillFile},
```

- [ ] **Step 4: Run the package tests**

Run: `go test ./internal/agentinit/ -v 2>&1 | tail -10`
Expected: all PASS (existing detection tests unaffected — the new Detect dirs don't exist in their fixtures).

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/agentinit
git add internal/agentinit
git commit -m "feat(agentinit): Antigravity + Kimi Code global skill rows"
```

---

### Task 6: Docs

**Files:**
- Modify: `CHANGELOG.md` (Unreleased → Added)
- Modify: `docs/superpowers/specs/2026-07-05-external-tools-design.md` (catalog section)
- Modify: `CLAUDE.md` (`exttool` and `agentinit` package-map rows)
- Modify: `README.md` (External tools paragraph)

**Interfaces:** consumes the shipped entry/row names from Tasks 2–5; produces no code.

- [ ] **Step 1: CHANGELOG entry**

Add under `## [Unreleased]` / `### Added` (create the subsection if absent):

```markdown
- External-tools catalog: OpenAI Codex, Antigravity (`agy`), and Kimi Code
  entries for conflict resolution, commit-message generation, and review —
  all shapes verified against the real binaries (codex-cli 0.144.6,
  agy 1.1.4, kimi 0.27.0). Codex captures via its native
  `--output-last-message` file channel; Kimi and Antigravity deliver
  through `$GG_MESSAGE_FILE`. Antigravity's commit/review rows are
  **opt-in** (unchecked in the wizard): headless `agy -p` auto-denies every
  permission-gated tool, so those templates must carry
  `--dangerously-skip-permissions`. The wizard never overwrites existing
  `[[tools.command]]` blocks — already-configured categories pick up the
  new tools by checking their new rows.
- Agent-skills picker: detects Antigravity (`~/.gemini/antigravity-cli`)
  and Kimi Code (`~/.kimi-code`) and installs the using-gg skill into their
  global skill directories (`~/.gemini/config/skills/`,
  `~/.kimi-code/skills/`).
- External-tool detection now finds Kimi Code's standard install
  (`~/.kimi-code/bin/kimi`) even when the shell PATH export is missing
  (`ExtraProbes` gained `~/` expansion).
```

- [ ] **Step 2: external-tools spec catalog section**

In `docs/superpowers/specs/2026-07-05-external-tools-design.md`, at the end of the `### Stage 2–3 defaults (recorded here, shipped later)` section (or after the catalog contents section if that fits the file better), append:

```markdown
### Roster expansion (2026-07-20)

Three tools joined the catalog with live-verified defaults — see
`2026-07-20-agent-roster-expansion-design.md` for the probe evidence:

- **OpenAI Codex** (`codex`): interactive conflict lane (positional
  prompt); capture lanes via `codex exec --sandbox read-only
  --output-last-message "$GG_MESSAGE_FILE"`.
- **Antigravity** (`agy`): `--prompt-interactive` conflict lane; capture
  lanes are OptIn and pair `--dangerously-skip-permissions` with the
  `$GG_MESSAGE_FILE` channel (headless agy auto-denies permission-gated
  tools; no CLI allowlist exists).
- **Kimi Code** (`kimi`): headless `-p` conflict lane under terminal
  handover (no interactive-with-prompt flag exists); capture lanes via the
  `$GG_MESSAGE_FILE` channel (plain `-p` stdout is `• `-decorated). First
  `~/`-expanded `ExtraProbes` entry (`~/.kimi-code/bin/kimi`).

The OptIn rule generalized with this batch: OptIn ⇔ the command carries a
permission-bypass flag (`--dangerously-*`, `--yolo`, `--brave`) — no longer
"(yolo)" name matching.
```

- [ ] **Step 3: CLAUDE.md package-map rows**

In the `exttool` row, replace the sentence fragment
"Hardcoded catalog of external tools/AI agents (Claude Code, Junie, Meld)"
with
"Hardcoded catalog of external tools/AI agents (Claude Code, Junie, Meld, OpenAI Codex, Antigravity, Kimi Code)"
and append to the row's end:

```
Roster expansion (2026-07-20): Codex (interactive conflict; capture via `codex exec --sandbox read-only --output-last-message "$GG_MESSAGE_FILE"` — the harness writes the file, so stdout noise is moot), Antigravity (`agy --prompt-interactive` conflict; capture lanes OptIn + `--dangerously-skip-permissions` + $GG_MESSAGE_FILE channel — headless agy auto-denies permission-gated tools, even reads), Kimi Code (headless `-p` for all lanes — no interactive-with-prompt flag; capture via $GG_MESSAGE_FILE since `-p` stdout is `• `-decorated; first `~/`-expanded ExtraProbes entry). OptIn invariant generalized: OptIn ⇔ the command carries a permission-bypass flag, not "(yolo)" naming. `Detect(look, stat, home)` — `~/` probes expand against the injected home ("" skips them).
```

In the `agentinit` row, append:

```
Roster expansion (2026-07-20): `antigravity` (detect `~/.gemini/antigravity-cli` — the agy home, not bare `~/.gemini`; skill → `~/.gemini/config/skills/using-gg/SKILL.md`, agy's global customization root) and `kimi` (detect `~/.kimi-code`; skill → `~/.kimi-code/skills/using-gg/SKILL.md`), both ModeSkillFile.
```

- [ ] **Step 4: README External tools paragraph**

In `README.md` (~line 476), replace
"— currently Claude Code, Junie, and Meld —"
with
"— currently Claude Code, Junie, Meld, OpenAI Codex, Antigravity, and Kimi Code —".

- [ ] **Step 5: Verify and commit**

Run: `go build ./... && ./test.sh unit 2>&1 | tail -5`
Expected: build OK, unit stage green (docs-only change; this is the whole-tree sanity gate before the final review).

```bash
git add CHANGELOG.md CLAUDE.md README.md docs/superpowers/specs/2026-07-05-external-tools-design.md
git commit -m "docs: agent roster expansion — changelog, catalog spec, package map, README"
```
