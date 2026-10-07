# Cross-review Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
> **gigagit rule:** NO implementer subagents — this session executes every task itself (CLAUDE.md). A read-only review subagent at the end is fine.

**Goal:** `/gg-cross-review <gg-link> [2|3] [focus]` — the agent runs 2–3 headless copies of itself on different models through `gg review`, merges their reviews, investigates every discrepancy, and stores ONE review in gg.

**Architecture:** `gg review` gains `--model` (an agent's model flag from the exttool catalog, or a `<model>` command token), `--link` (the `gg review save` resolver), `--no-save [--json]` (run, print the document, store nothing) and `--tools [--json]` (list review tools). The domain splits running a review from storing it. A new embedded user-invoked skill carries the flow and the merge rules.

**Tech Stack:** Go 1.26, embedded markdown skills, TOML e2e scenarios.

**Spec:** `docs/superpowers/specs/2026-10-07-cross-review-design.md`

**Worktree:** `/work/gigagit/.claude/worktrees/cross-review` (branch `feat/cross-review`). Every command runs there (`cd` first — the shell cwd resets to the main checkout).

## Global Constraints

- Model flags (verified 2026-10-07 on the installed CLIs): `claude` → `--model <m>`, `codex` → `-m <m>`, `junie` → `--model=<m>`, `kimi` → `-m <m>`, `antigravity` (`agy`) → `--model <m>`.
- The model value is quoted with the `template` package's per-OS rule (POSIX single quotes; double quotes on Windows), never spliced raw.
- A command holding `<model>` never gets the flag appended; a tool with neither a known flag nor `<model>` refuses `--model`: `review tool "<name>" cannot take a model — add <model> to its command`, exit 2. An empty `--model` adds nothing.
- `--json` only with `--no-save` (else exit 2). `--link` excludes `--preview`, `--working` and a positional (exit 2). `--no-save` excludes `--notes` (exit 2).
- Only the merged review is stored; reviewers always run `--no-save`.
- No catalog template TEXT changes (the golden version guard must stay untouched).
- Skills: bump `agentskill.Version` (using-gg) and `ReviewVersion` (reviewing-with-gg) once each; refresh the dogfood copies by rendering `agentskill.<Skill>.SkillFile()` (NOT `gg init --update` before merge — it rewrites the user's global installs).
- `internal/cli` never imports `internal/git`.

## Review Focus

1. A model name with a space or quote (`"my model"`, `o'3`) — expect it quoted as one argv value, never a shell break (Task 3 test).
2. Two concurrent `--no-save --json` runs on one link — expect two independent outputs, neither waiting nor clobbering (Task 4 concurrency test).
3. A tool whose reply is prose under `--no-save --json` — expect exit 1, stdout empty, the raw text on stderr (Task 4).
4. A `--link` naming another worktree's working changes — expect the review to run in THAT checkout (the link's `Checkout`), as `review save` does (Task 4 test with a second worktree).
5. A custom command with `<model>` run WITHOUT `--model` — expect the slot to vanish (empty), not a literal `<model>` or an error (Task 1).

---

### Task 1: `template` — the `<model>` token and an exported quote

**Files:**
- Modify: `internal/template/command.go` (CmdCtx, resolveCommandToken, commandTokens, new helpers)
- Test: `internal/template/model_token_test.go` (create)

**Interfaces:**
- Produces: `CmdCtx.Model string`; token `<model>` (empty value → empty string; else quoted); `func HasModelSlot(tmpl string) bool`; `func QuoteArg(s string) string` (= `quoteArgFor(s, runtime.GOOS)`).

- [ ] **Step 1: Failing test** — `model_token_test.go`:

```go
package template

import "testing"

func TestModelToken(t *testing.T) {
	t.Parallel()
	got, err := resolveCommandFor(`tool --model <model> -p x`, nil, CmdCtx{Model: "son net"}, "linux")
	if err != nil || got != `tool --model 'son net' -p x` {
		t.Fatalf("got %q, %v", got, err)
	}
	got, err = resolveCommandFor(`tool --model=<model>`, nil, CmdCtx{Model: "o'3"}, "linux")
	if err != nil || got != `tool --model='o'\''3'` {
		t.Fatalf("quote: got %q, %v", got, err)
	}
	got, err = resolveCommandFor(`tool <model> -p x`, nil, CmdCtx{}, "linux")
	if err != nil || got != `tool  -p x` {
		t.Fatalf("empty model: got %q, %v", got, err)
	}
	if err := ValidateCommandTokens(`tool <model>`, false); err != nil {
		t.Fatalf("<model> must be a known token: %v", err)
	}
	if !HasModelSlot(`a <model> b`) || HasModelSlot(`a <prompt> b`) {
		t.Fatal("HasModelSlot")
	}
	if QuoteArg("x y") == "x y" {
		t.Fatal("QuoteArg must quote")
	}
}
```

- [ ] **Step 2: Run, expect FAIL** — `go test ./internal/template/ -run TestModelToken` → `unknown field Model`.

- [ ] **Step 3: Implement**
  - `CmdCtx`: add `Model string // <model>: the review's model ("" = the tool's default)`.
  - `resolveCommandToken`: `case "model": if ctx.Model == "" { return "", nil }; return quoteArgFor(ctx.Model, goos), nil`.
  - `commandTokens`: add `"model": false`.
  - Add:
    ```go
    // HasModelSlot reports whether a command template carries a <model> slot —
    // then `gg review --model` fills it instead of appending the agent's flag.
    func HasModelSlot(tmpl string) bool {
    	for _, m := range tokenRe.FindAllStringSubmatch(tmpl, -1) {
    		if p, _, _ := cutColon(m[1]); p == "model" {
    			return true
    		}
    	}
    	return false
    }

    // QuoteArg shell-quotes one argv value for this OS (quoteArgFor).
    func QuoteArg(s string) string { return quoteArgFor(s, runtime.GOOS) }
    ```

- [ ] **Step 4: Run, expect PASS** — `go test ./internal/template/`.
- [ ] **Step 5: Commit** — `feat(template): the <model> command token`.

---

### Task 2: `exttool` — each agent's model flag

**Files:**
- Modify: `internal/exttool/exttool.go` (`Tool.ModelFlag`, the five Builtins entries)
- Test: `internal/exttool/model_flag_test.go` (create)

**Interfaces:**
- Produces: `Tool.ModelFlag string` — the text put before the quoted model: `"--model "`, `"-m "` or `"--model="`; `""` = none known. `func ModelFlagFor(agentID string) string`.

- [ ] **Step 1: Failing test**

```go
package exttool

import "testing"

// Every agent with a headless review template can take a model.
func TestReviewAgentsHaveAModelFlag(t *testing.T) {
	t.Parallel()
	want := map[string]string{"claude": "--model ", "codex": "-m ", "junie": "--model=", "kimi": "-m ", "antigravity": "--model "}
	for _, tl := range Builtins() {
		capture := false
		for _, c := range tl.Commands {
			if c.Category == CatReview && c.Mode == ModeCapture {
				capture = true
			}
		}
		if !capture {
			continue
		}
		if tl.ModelFlag != want[tl.ID] || tl.ModelFlag == "" {
			t.Errorf("%s: ModelFlag %q, want %q", tl.ID, tl.ModelFlag, want[tl.ID])
		}
		if ModelFlagFor(tl.ID) != tl.ModelFlag {
			t.Errorf("ModelFlagFor(%s)", tl.ID)
		}
	}
	if ModelFlagFor("") != "" || ModelFlagFor("meld") != "" {
		t.Error("unknown agents have no flag")
	}
}
```

- [ ] **Step 2: FAIL** — `go test ./internal/exttool/ -run TestReviewAgentsHaveAModelFlag` → unknown field.
- [ ] **Step 3: Implement** — `Tool` gains
  ```go
  	// ModelFlag picks the agent's model on its command line: the text before
  	// the quoted model ("--model ", "-m ", "--model="); "" = none known.
  	// Verified against each CLI's --help (2026-10-07).
  	ModelFlag string
  ```
  set it on the claude/junie/codex/antigravity/kimi entries per Global Constraints, and add
  ```go
  // ModelFlagFor is the model flag of the built-in agent id ("" = none).
  func ModelFlagFor(agentID string) string {
  	for _, tl := range Builtins() {
  		if tl.ID == agentID {
  			return tl.ModelFlag
  		}
  	}
  	return ""
  }
  ```
  If `Builtins()` builds fresh structs per call and is costly, still fine (CLI path, once per run).
- [ ] **Step 4: PASS** — `go test ./internal/exttool/` (the version guard must stay green: no template text changed).
- [ ] **Step 5: Commit** — `feat(exttool): each review agent's model flag`.

---

### Task 3: `domain` — apply a model; run a review without storing it

**Files:**
- Modify: `internal/domain/sessions.go:357` (export `ToolAgentID`; keep `agentIDFor` as a call to it or rename callers)
- Create: `internal/domain/review_model.go`
- Modify: `internal/domain/review.go:113-158` (split run from save)
- Test: `internal/domain/review_model_test.go` (create)

**Interfaces:**
- Consumes: `template.CmdCtx.Model`, `template.HasModelSlot`, `template.QuoteArg`, `template.ResolveCommand` (Task 1); `exttool.ModelFlagFor` (Task 2).
- Produces:
  - `func ToolAgentID(tc config.ToolCommand) string`
  - `var ErrNoModelSupport` (sentinel) and `func ResolveReviewCommand(tc config.ToolCommand, ctx template.CmdCtx) (string, error)` — resolves the template with `ctx` (whose `Model` may be set); when `ctx.Model != ""` and the command has no `<model>` slot, appends `" " + flag + template.QuoteArg(ctx.Model)`; no flag → `fmt.Errorf("%w: review tool %q cannot take a model — add <model> to its command", ErrNoModelSupport, tc.Name)`.
  - `func (s *Service) RunReview(ctx, target ReviewTarget, resolvedCommand string, env []string) (ReviewResult, error)` — runs + parses, stores nothing (`NoteID == ""`).
  - `ReviewReport` unchanged in signature and behaviour (now `RunReview` + `SaveReview`).

- [ ] **Step 1: Failing tests** — `review_model_test.go`:

```go
package domain

import (
	"errors"
	"runtime"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/template"
)

func TestResolveReviewCommandModel(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX quoting asserted")
	}
	claude := config.ToolCommand{Name: "Claude", Category: "review", Mode: "capture", Command: `claude -p "review <range>" --output-format json`}
	got, err := ResolveReviewCommand(claude, template.CmdCtx{Range: "a..b", Model: "sonnet"})
	if err != nil || got != `claude -p "review a..b" --output-format json --model 'sonnet'` {
		t.Fatalf("claude: %q %v", got, err)
	}
	junie := config.ToolCommand{Name: "Junie", Category: "review", Mode: "capture", Command: `junie --task x`}
	if got, _ := ResolveReviewCommand(junie, template.CmdCtx{Model: "my model"}); got != `junie --task x --model='my model'` {
		t.Fatalf("junie: %q", got)
	}
	slot := config.ToolCommand{Name: "Mine", Category: "review", Mode: "capture", Command: `claude --model <model> -p x`}
	if got, _ := ResolveReviewCommand(slot, template.CmdCtx{Model: "opus"}); got != `claude --model 'opus' -p x` {
		t.Fatalf("slot wins over the flag: %q", got)
	}
	custom := config.ToolCommand{Name: "Echo", Category: "review", Mode: "capture", Command: `printf x`}
	if _, err := ResolveReviewCommand(custom, template.CmdCtx{Model: "m"}); !errors.Is(err, ErrNoModelSupport) {
		t.Fatalf("custom without <model>: %v", err)
	}
	if got, err := ResolveReviewCommand(custom, template.CmdCtx{}); err != nil || got != "printf x" {
		t.Fatalf("no model, no change: %q %v", got, err)
	}
}

func TestToolAgentID(t *testing.T) {
	t.Parallel()
	if ToolAgentID(config.ToolCommand{Command: `/usr/local/bin/codex exec x`}) != "codex" || ToolAgentID(config.ToolCommand{Command: `printf x`}) != "" {
		t.Fatal("ToolAgentID")
	}
}
```

Add a RunReview test with a real repo (reuse `newRealRepo(t)` from the domain tests): a tool command `printf '{"version":1,"summary":"S","files":[]}' > "$GG_MESSAGE_FILE"` (sh; skip on Windows) over `ReviewTarget` for `HEAD`; assert `Structured`, `NoteID == ""`, and that `svc.ReviewsFor…`/the review store holds nothing (use whatever `SaveReview`'s read counterpart is — `s.ReviewShow(ctx, "latest")` returning an error is enough).

- [ ] **Step 2: FAIL** — `go test ./internal/domain/ -run 'TestResolveReviewCommandModel|TestToolAgentID|TestRunReview'`.
- [ ] **Step 3: Implement**
  - `sessions.go`: rename `agentIDFor` → `ToolAgentID` (exported doc: "the built-in agent a command runs, by its program's base name; \"\" for a custom command") and update its callers (`grep -rn agentIDFor internal/`).
  - `review_model.go`: `ErrNoModelSupport = errors.New("no model support")`; `ResolveReviewCommand` as in Interfaces (resolve with `template.ResolveCommand(tc.Command, nil, ctx)`; if `ctx.Model == "" || template.HasModelSlot(tc.Command)` return it; else flag := `exttool.ModelFlagFor(ToolAgentID(tc))`; empty → the wrapped error; else append).
  - `review.go`: move the body up to `out := ReviewResult{…}` into `runReview(ctx, target, resolvedCommand, env) (ReviewResult, []model.NoteFile, error)`; `RunReview` returns its first and last results; `ReviewReport` = `runReview` + the existing `SaveReview` block.
- [ ] **Step 4: PASS** — `go build ./... && go test ./internal/domain/ -run 'Review|ToolAgentID'`.
- [ ] **Step 5: Commit** — `feat(domain): resolve a review command with a model; run a review without storing it`.

---

### Task 4: `gg review` — `--model`, `--link`, `--no-save`, `--json`, `--tools`

**Files:**
- Modify: `internal/cli/review.go` (flags, target resolution, tool listing, output)
- Test: `internal/cli/review_cross_test.go` (create)

**Interfaces:**
- Consumes: `domain.ResolveReviewCommand`, `domain.ErrNoModelSupport`, `domain.ToolAgentID`, `(*Service).RunReview` (Task 3); `resolveLinkArg`, `openLinkTarget`, `(*Service).LinkReviewTarget` (existing, `review_save.go:52-60`).
- Produces: the CLI surface the skill (Task 5) uses:
  - `gg review --tools --json` → `[{"name","agent","mode","model":bool}]` (every CLI-visible valid review tool, interactive ones included with their mode); text form one `name\tagent\tmode\tmodel=yes|no` line each.
  - `gg review --tool T --model M --link L --no-save --json` → stdout = the canonical review document JSON + `\n`; exit 0.

- [ ] **Step 1: Failing tests** — `review_cross_test.go` (package cli; reuse `isolateReviewEnv`, `newRepoDir`, `runGit`, `writeReviewTool`, `runCLI`; sh tests skip on Windows). Cases:

```go
// fakeAgent puts an executable named bin (e.g. "claude") first on PATH that
// writes its argv, one per line, into $GG_MESSAGE_FILE as a review document's summary.
func fakeAgent(t *testing.T, bin string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based fake agent")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nargs=$(printf '%s|' \"$@\")\nprintf '{\"version\":1,\"summary\":\"ARGS %s\",\"files\":[]}' \"$args\" > \"$GG_MESSAGE_FILE\"\n"
	if err := os.WriteFile(filepath.Join(dir, bin), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
```

  1. `TestReviewModelAppendsTheAgentFlag` — `fakeAgent(t,"claude")`; `writeReviewTool(dir,"Claude","claude -p x")`; `gg review --tool Claude --model "son net" --no-save --json HEAD` → exit 0; stdout parses as JSON whose `summary` contains `--model|son net|` (one argv value with the space).
  2. `TestReviewModelRefusedForACustomTool` — `printf x` tool + `--model m` → exit 2, stderr contains `cannot take a model`.
  3. `TestReviewNoSaveStoresNothing` — structured printf tool; `--no-save --json HEAD` → exit 0, stdout is the document; stderr has no `note:`; then `gg review show latest` exits non-zero (nothing stored).
  4. `TestReviewNoSaveJSONRefusesProse` — `printf 'prose\n'` tool; `--no-save --json HEAD` → exit 1, stdout `""`, stderr contains `not a gg review document` and `prose`.
  5. `TestReviewFlagConflicts` — exit 2 for each: `--json HEAD` (no --no-save); `--link L --working`; `--link L HEAD`; `--link L --preview x`; `--no-save --notes HEAD`.
  6. `TestReviewLinkMatchesReviewSave` — build a preview link as `review_save_test.go:reviewSaveRepo` does; `gg review save <link> --dry-run --json` gives `diff`; a fake tool `printf '{"version":1,"summary":"R <range>","files":[]}' > "$GG_MESSAGE_FILE"`; `gg review --tool Echo --link <link> --no-save --json` → summary holds the same range the dry-run's `diff` names.
  7. `TestReviewLinkRunsInTheLinksCheckout` — add a second worktree with an uncommitted change; `gg link` for its working tree (`gg link --working` from that worktree, or the form `review_save_test.go` uses); run `gg review --link` from the MAIN worktree with a tool `printf '{"version":1,"summary":"PWD %s","files":[]}' "$PWD" > "$GG_MESSAGE_FILE"` → summary names the second worktree's path.
  8. `TestReviewToolsJSON` — two tools (a `claude …` capture, a `printf` capture) + one interactive → JSON array with `agent:"claude",model:true`, `agent:"",model:false`, and the interactive row's `mode:"interactive"`.
  9. `TestReviewNoSaveRunsConcurrently` — tool `sleep 1; printf '{"version":1,"summary":"M <model>","files":[]}' > "$GG_MESSAGE_FILE"` (custom tool WITH `<model>`); run two `runCLI` calls in goroutines with `--model a` / `--model b`; both exit 0, outputs hold `M a` and `M b` respectively (the quoted value splices into the printf's own single quotes, so the shell drops them), and wall time < 1.9 s (they did not serialize).

(If `runCLI` mutates process-global state that makes concurrent calls unsafe, run the two as `exec.Command` of a test-built binary instead — check `runCLI`'s implementation first and note the choice as a Ruling.)

- [ ] **Step 2: FAIL** — `go test ./internal/cli/ -run 'TestReview(Model|NoSave|FlagConflicts|Link|Tools)'`.
- [ ] **Step 3: Implement** in `cmdReview`:
  - New flags: `model := fs.String("model", "", "run the review tool on this model (its CLI's model flag, or <model> in the command)")`, `linkArg := fs.String("link", "", "review what a gg:// link names (as gg review save does)")`, `noSave := fs.Bool("no-save", false, "print the review; store nothing")`, `asJSON := fs.Bool("json", false, "with --no-save: print the review document JSON")`, `listTools := fs.Bool("tools", false, "list the review tools")`.
  - `--tools`: before target resolution; reuse `selectReviewCommand`'s filter in a new `reviewTools(cfg)` (keep interactive rows, mark `mode`); `model = template.HasModelSlot(tc.Command) || exttool.ModelFlagFor(domain.ToolAgentID(tc)) != ""`; print JSON array or text lines; return 0.
  - Conflicts per Global Constraints → the usage line + exit 2.
  - `--link`: `res, err := resolveLinkArg(ctx, svc, *linkArg, linkShapes{Pair: true, Ref: true}, "review")` (error → `linkExit("review", err, stderr)`), `tsvc := openLinkTarget(res)`, `target, err = tsvc.LinkReviewTarget(ctx, res)`; from here on use `tsvc` for running/saving and `res.Checkout` as the command's `Repo`.
  - Resolution: replace `template.ResolveCommand(...)` with `domain.ResolveReviewCommand(cmd, template.CmdCtx{Range: target.Range, Repo: repoDir, Model: *model})`; `errors.Is(err, domain.ErrNoModelSupport)` → print, exit 2.
  - `--no-save`: `res, err := runSvc.RunReview(...)`; error → exit 1. `--json`: `!res.Structured` → `error: the review is not a gg review document` + the raw content on stderr, exit 1; else `fmt.Fprintln(stdout, res.Content)`, exit 0. Without `--json`: `printReview(stdout, res.Content)` + the existing prose warning (without "stored as text"), exit 0.
  - Update the usage strings and `cmdReview`'s doc comment.
- [ ] **Step 4: PASS** — `go test ./internal/cli/ -run Review` (the whole existing review suite must stay green).
- [ ] **Step 5: Commit** — `feat(cli): gg review --model, --link, --no-save --json, --tools`.

---

### Task 5: the `/gg-cross-review` skill

**Files:**
- Create: `internal/agentskill/gg-cross-review.md`
- Modify: `internal/agentskill/agentskill.go` (embed, `GGCrossReviewVersion = 1`, `GGCrossReview` skill, `All()`), package doc comment
- Modify: `internal/agentskill/agentskill_test.go` (`TestAllReturnsEverySkill` → 5; frontmatter test)
- Modify: `internal/agentinit/agentinit_test.go:390` (also assert gg-cross-review installed)
- Create: `.claude/skills/gg-cross-review/SKILL.md` (rendered dogfood copy)

**Interfaces:**
- Consumes: the CLI surface of Task 4.
- Produces: `agentskill.GGCrossReview` (name `gg-cross-review`, front `argument-hint: "<gg-link> [2|3] [what to focus on]"` + `disable-model-invocation: true`).

- [ ] **Step 1: Failing tests** — in `agentskill_test.go`: `All()` has 5 skills ending `gg-review, gg-cross-review`; `TestGGCrossReviewFrontmatter` asserts the SKILL.md holds `name: gg-cross-review\n`, the argument hint, `disable-model-invocation: true\n`, and the body strings `gg review --tools --json`, `--no-save --json`, `--link`, `--model`, `## Disagreements resolved`, `## Reviewers`, `gg review save`, `raised_by`. Add `TestDogfoodGGCrossReviewCopyInSync` mirroring the delegate one. In `agentinit_test.go` assert `d.TargetOf(agentskill.GGCrossReview)` carries its marker.
- [ ] **Step 2: FAIL** — `go test ./internal/agentskill/ ./internal/agentinit/`.
- [ ] **Step 3: Write the skill** — `gg-cross-review.md`, following spec §2 and §3 exactly, in the voice of `gg-review.md` (second person, numbered steps, exact commands). Sections: intro (what the user typed; never launch gg's TUI or web), **Steps** 1–9 (spec §2), **Merging** (spec §3: agreement / discrepancy kinds / the three rulings with their `meta` keys / the summary sections / document `meta`), **When a reviewer fails** (named in `## Reviewers`; at least one success to go on), and the one-file-question escape from `gg-review.md`. Use `<tmp>` = a fresh directory from `mktemp -d`. Name background running explicitly: "start each reviewer as a background task (Claude Code: run_in_background; others: `&` and `wait`) and wait for all — one review takes minutes."
  Then the registry: `//go:embed gg-cross-review.md` → `ggCrossReviewBody`; `GGCrossReviewVersion = 1`; `GGCrossReview` built like `GGReview` with description `Review the change a gg:// link names with 2–3 copies of yourself on different models, settle their disagreements, and store one merged review in gg.` (no ": " — plain-scalar rule); `All()` appends it. Render the dogfood copy (scratch `main` that writes `agentskill.GGCrossReview.SkillFile()` to `.claude/skills/gg-cross-review/SKILL.md`, then delete the scratch program).
- [ ] **Step 4: PASS** — `go test ./internal/agentskill/ ./internal/agentinit/ ./internal/cli/ -run 'Skill|Init|All|GG'`.
- [ ] **Step 5: Commit** — `feat(skill): /gg-cross-review`.

---

### Task 6: docs, skills text, e2e, gates

**Files:**
- Modify: `internal/agentskill/using-gg.md` (the `gg review` bullet: the five flags), `agentskill.go` (`Version` +1), `.claude/skills/using-gg/SKILL.md` (re-render)
- Modify: `internal/agentskill/reviewing-with-gg.md` (a pointer: `/gg-cross-review` beside `/gg-review`), `ReviewVersion` +1, `.claude/skills/reviewing-with-gg/SKILL.md` (re-render)
- Modify: `README.md` (review section), `CHANGELOG.md` (new top section), `docs/CLAUDE-details.md` (review lane: model flags, `--link`, `--no-save`, `RunReview` vs `ReviewReport`; the skill)
- Create: `e2e/scenarios/s110_review_model_nosave.toml` (number: next free — `ls e2e/scenarios | sort -V | tail`)

- [ ] **Step 1: e2e scenario** — load the writing-e2e-scenarios skill; one scenario: a repo with a commit, a `[[tools.command]]` review tool `printf '{"version":1,"summary":"M <model>","files":[]}' > "$GG_MESSAGE_FILE"`, run `gg review --tool Echo --model a --no-save --json HEAD`, assert stdout holds `M a` (the quoted model splices into the printf's own quotes) and that `gg review show latest` fails (nothing stored). Run `./test.sh e2e` — watch it fail on main's binary first if the harness allows (else note it), then pass.
- [ ] **Step 2: docs** — using-gg `gg review` bullet gains: `--model <m>`, `--link <gg-link>`, `--no-save [--json]`, `--tools [--json]`, the refusals and exit codes; reviewing-with-gg: "For a review by several models with the disagreements settled, the user runs `/gg-cross-review <link> [2|3]`." Bump both counters, re-render both dogfood copies, `go test ./internal/agentskill/`.
  CHANGELOG top section:
  ```markdown
  ## Cross-review: several models, one review

  ### Added

  - **`/gg-cross-review <gg-link> [2|3] [focus]`** — a new embedded skill
    (user-invoked): the agent asks how many reviewers (2 or 3) and which of
    its own models, runs that many headless copies of itself on the link
    in parallel, merges their reviews — one remark per agreed issue,
    crediting the models that raised it — investigates every disagreement
    itself and rules on it (`## Disagreements resolved`), and stores ONE
    review with `gg review save`. Run `gg init --update` to install it.
  - **`gg review --model <m>`** runs the review tool on a model: gg adds the
    agent's own flag (Claude `--model`, Codex `-m`, Junie `--model=`, Kimi
    `-m`, Antigravity `--model`) or fills `<model>` in a custom command.
  - **`gg review --link <gg-link>`** reviews what a link names, as
    `gg review save` does; **`--no-save [--json]`** prints the review (the
    document JSON) and stores nothing; **`--tools [--json]`** lists the
    review tools and whether each takes a model.
  - Skills: using-gg v<N> and reviewing-with-gg v<M>.
  ```
  README: in the review section, a short paragraph on `/gg-cross-review` and the new flags. CLAUDE-details: a "Cross-review (2026-10-07)" section with the facts above (model-flag table, slot wins over flag, `RunReview` stores nothing, the skill is the only surface).
- [ ] **Step 3: gates** — `./test.sh` then `./test.sh race`; green ONLY on "all green" in the log.
- [ ] **Step 4: Live smoke** (one real model run, read-only): in a scratch repo with one commit and the user's real Claude review tool configured (`gg init`-detected template), run `gg review --tool Claude --model haiku --no-save --json HEAD` and check the output is a review document. Skip with a ledger note if no real agent is configured in the scratch env.
- [ ] **Step 5: Commit** — `docs: cross-review`.
- [ ] **Step 6: Final review** — read-only review subagent over `git diff main...feat/cross-review`; then ASK the user before merging.
