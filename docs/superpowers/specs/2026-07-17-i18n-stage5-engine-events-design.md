# i18n stage 5 — localized TUI rendering of engine events

**Date:** 2026-07-17 · **Status:** approved design · **Branch:** `feat/i18n-stage5`

## Goal

A ja/ko/zh/ru user sees the three engine-driven TUI surfaces in their
language: the busy line while an operation runs (`Progress`), the status
line after it finishes (`Result.Summary`), and the decision modal's
question text (`DecisionRequest.Prompt`). Decision *option labels* already
translate (stage 3, `optionDisplayName`); this stage adds the prompt
sentence above them.

## Non-goals (stay byte-identical English)

- **CLI output** (`✓ <summary>` lines, prompts printed to stdin, usage
  text) — the agent-facing surface, English by design.
- **`operations.log` / `errors.log` / observ spans** — grep-able,
  shareable diagnostics.
- **e2e scenario assertions** — unchanged and green is the proof the
  agent surface didn't move.
- **`GitLine` raw git output** — never translated.
- **Engine error strings** (159 `fmt.Errorf`/`errors.New` sites and
  wrapped git stderr). Errors already render inside the translated
  `friendlyOpError` frame (`error: %s` and the friendly push-error
  rewrites are translated; the raw tail stays English). Unchanged.
- **MCP** — future frontend; it consumes the English channel like the CLI.

## Current state (survey, 2026-07-17, post-stage-4 main `3cce51f`)

| Surface | Sites | TUI render path today |
|---|---|---|
| `Result.Summary` | 107 construction sites | `model.go` opEventMsg `engine.Done` case; opFinishedMsg; `op.go:109` statusRefreshedMsg; `gitconfig_popup.go:412` |
| `Progress{Step, Detail}` | 83 emit sites | `model.go` opEventMsg `engine.Progress` case (`step` or `step: detail`) |
| `DecisionRequest.Prompt` | 34 construction sites (7 direct `DecisionNeeded` emits; all forks flow through `OpDeps.decide`) | decision modal (`decisionState{req}`) |

All three are pre-interpolated English strings when they reach the TUI, so
render-site translation alone cannot work (the sentinel-comparison class —
never parse rendered text). The engine must carry the unformatted
(format, args) pair alongside.

## Design

### Approach: dual-channel with English fallback

The English string fields stay exactly as they are — every existing
consumer (CLI, e2e, oplog, tests) is untouched. Alongside each English
string the engine carries the unformatted pair; the TUI prefers the pair,
falls back to the English string when absent. A missed or future site
degrades to English, never breaks.

Rejected alternatives: (a) retyping `Result.Summary` to a structured type —
same behavior, much wider blast radius across domain/CLI/e2e consumers,
loses the fail-soft fallback; (b) TUI-side pattern-matching of the English
text — the forbidden sentinel-comparison class.

### Engine contract (`internal/engine`)

New value type (new file `internal/engine/msg.go`):

```go
// Msg is one localizable sentence fragment: an English format string
// (doubling as the i18n catalog key) plus its interpolation args. The
// zero Msg means "no localizable channel — render the English string".
type Msg struct {
    Format string
    Args   []any
}

func (m Msg) empty() bool { return m.Format == "" }
```

`Result` gains a parts slice (suffix-append sites need more than one
format):

```go
type Result struct {
    Summary      string
    SummaryParts []Msg // English invariant: Summary == concat(Sprintf(part)) when non-empty
    Changed      bool
    Path         string
    Captured     string
}
```

Helpers (in `msg.go`) keep `Summary` and `SummaryParts` in lockstep —
operation code never hand-builds `Summary` strings again:

```go
// WithSummary sets the summary from one format: Summary = Sprintf,
// SummaryParts = [{format, args}]. Value method; chainable off a literal:
//   res := engine.Result{Changed: true, Path: abs}.WithSummary("worktree created: %s", abs)
func (r Result) WithSummary(format string, args ...any) Result

// AppendSummary appends a suffix part to BOTH channels. The glue lives
// inside the suffix format (leading "; " or " (" — the footer-fragment
// precedent):
//   res = res.AppendSummary(" (your changes remain stashed)")
func (r Result) AppendSummary(format string, args ...any) Result
```

`Progress` gains a detail message for glue-bearing details only:

```go
type Progress struct {
    Step      string // stable 52-word vocabulary; translated directly as its own key
    Detail    string
    DetailMsg Msg // set only when Detail contains English glue; empty = Detail is pure data, shown verbatim
}

// Progressf builds a glue-bearing progress event:
//   Progressf("rebasing", "%s onto %s", op.Branch, op.Onto)
// fills Step, Detail = Sprintf(format, args), DetailMsg = {format, args}.
func Progressf(step, format string, args ...any) Progress
```

Pure-data sites stay as they are: `Progress{Step: "committing", Detail:
op.Message}` — the render seam translates `Step` and shows `Detail`
verbatim. Only the ~10 sites whose Detail mixes English words into the
data ("X onto Y", "X at Y", "(recreate commits)", "(working tree)",
"wrote %s (%d/%d)") convert to `Progressf`. Details that are pure data
joined by symbols (`op.Branch + " → " + abs`) count as pure data — "→"
is not prose.

`DecisionRequest` gains the prompt pair, built by one constructor:

```go
type DecisionRequest struct {
    ID        string
    Prompt    string
    PromptMsg Msg
    Options   []string
}

// PromptReq builds a request with both prompt channels in lockstep:
//   PromptReq(deleteBranchID, "Delete branch %s?", []string{"delete", "cancel"}, op.Name)
func PromptReq(id, format string, options []string, args ...any) DecisionRequest
```

All 34 prompt construction sites convert. The one variable-built prompt
(`ops_basic.go:137`, `Prompt: prompt`) is restructured so each conditional
branch calls `PromptReq` with its own literal format — every English
sentence must originate as a literal format at a helper call site (the
enforcement gate depends on this).

Variable-built summaries follow the same rule: sites like
`conflict.go:67` / `cherry_pick.go:76` / `apply_patch.go:145` that
assemble a `summary` variable across branches restructure so each branch
produces its `Result` via `WithSummary`/`AppendSummary` with literal
formats. Conditional suffixes (e.g. `create_worktree.go`'s hook-failure
`note`) become conditional `AppendSummary` calls.

**English invariant:** for every helper-built value,
`Summary == strings.Join(Sprintf(each part), "")` and
`Prompt == Sprintf(PromptMsg...)` and `Detail == Sprintf(DetailMsg...)`.
Because `i18n.T` on a catalog miss does `fmt.Sprintf(key, args...)`
(`internal/i18n/i18n.go:48`), English rendering through the seam is
byte-identical to the stored English string by construction.

### TUI render seam (`internal/tui/i18n_engine.go`, new file)

Sibling of `i18n_display.go`; the only place `i18n.T` is called with a
non-literal key:

```go
// renderSummary: len(SummaryParts) == 0 → res.Summary verbatim (fallback);
// else concat of i18n.T(part.Format, part.Args...).
func renderSummary(res engine.Result) string

// renderProgress: i18n.T(e.Step) plus ": " + detail when present;
// detail = i18n.T(DetailMsg...) when set, else e.Detail verbatim.
func renderProgress(e engine.Progress) string

// renderPrompt: PromptMsg set → i18n.T(...), else req.Prompt verbatim.
func renderPrompt(req engine.DecisionRequest) string
```

Consumer switches (the complete list — every TUI site that shows engine
prose):

1. `model.go` opEventMsg, `engine.Progress` case → `renderProgress(e)`.
2. `model.go` opEventMsg, `engine.Done` case → `renderSummary(e.Result)`.
3. `model.go` opFinishedMsg success path (`msg.res.Summary != ""`) →
   `renderSummary(msg.res)` (guard on the rendered string being non-empty).
4. `op.go:109` statusRefreshedMsg — the summary is pre-rendered via
   `renderSummary(res)` at the cmd site (the message keeps carrying a
   plain string).
5. `gitconfig_popup.go:412` gitConfigRowsMsg — same pre-render.
6. The decision modal's prompt line (`view.go:1563`, which splits
   `m.modal.req.Prompt` on `"\n"`) → split `renderPrompt(m.modal.req)`
   instead (option labels keep going through `optionDisplayName`; the
   esc→`"abort"` protocol mapping is untouched). The CLI's stdin prompt
   (`cli/core.go:35`) keeps printing the English `req.Prompt`.

Mid-session language switch: an already-shown status line stays in the
old language (it's a baked string in `m.statusMsg` — the existing,
accepted staleness class); the next event renders in the new language.

### Enforcement (two AST gates)

**1. `internal/tui/i18n_scan_test.go` (amended).** The render seam calls
`i18n.T` with a non-literal key — the first sanctioned exception to the
literal-key rule. The scan gains a file-level allowlist containing exactly
`i18n_engine.go`. The orphan check (every catalog key must be used) gains
the engine-collected key set (from gate 2) as "used" — otherwise the ~170
new keys would all flag as orphans.

**2. `internal/tui/engine_prose_test.go` (new, the `options_vocab_test.go`
pattern).** An AST scan over `internal/engine` (non-test files):

- Collects every format literal at `WithSummary(`, `AppendSummary(`,
  `Progressf(` (both the step and format args), `PromptReq(` (the format
  arg), and every `Progress{Step: "…"}` composite-literal step value.
- Each collected literal must exist in **all four** bundles.
- A non-literal format/step argument at any of these calls **fails** the
  scan (the options-vocab precedent: restructure to per-branch literals).
- **Drift gate:** direct construction of the English channel outside
  `msg.go` fails — a `Result` composite literal with a `Summary:` field,
  any assignment to `.Summary`, and a `DecisionRequest` composite literal
  with a `Prompt:` field are all violations (helpers only). `Progress`
  composites remain legal (the pure-data form is the common case).
- A `collected < 150` floor guards a scan gone blind (there are ~200
  literals on day one).
- Mutation-proof the gate red once (drop one key from one bundle, watch
  it fail) before relying on it.

### Bundles and translation QA

- ~107 summary formats + 52 step words + ~10 detail formats + ~34
  prompts + ~6 suffix parts ≈ **~195–210 new keys ×4** (`ja`/`ko`/`zh`/`ru`
  bundles grow ~1,180 → ~1,385 keys). Keys inserted in place, never
  re-sorted; all four bundles in the same commit as the code that
  introduces the key.
- **Plurals:** keep the existing single-key English forms (`"%d file(s)"`)
  — English output must stay byte-identical. CJK doesn't inflect; ru uses
  its established count-neutral colon-form convention.
- Established conventions apply: ko particle alternation (`%s이(가)`),
  ru glossary families (индекс/удалённые), argument reorder only via
  indexed verbs (`%[n]s`), `CheckVerbs` passes at load.
- One QA pass per language at the end (native-review subagents, the
  stage-3/4 pattern). The error-key topic-head audit does not apply
  (these are not `<topic>: %s` error keys).

### What operation code looks like after

```go
// before
res := Result{Summary: "created branch " + op.Name, Changed: true}
// after
res := Result{Changed: true}.WithSummary("created branch %s", op.Name)

// before (suffix append)
res.Summary += " (your changes remain stashed)"
// after
res = res.AppendSummary(" (your changes remain stashed)")

// before (glue-bearing progress)
deps.emit(ctx, Progress{Step: "rebasing", Detail: op.Branch + " onto " + op.Onto + where})
// after — `where` folds into the format's branches or an arg with its own literal
deps.emit(ctx, Progressf("rebasing", "%s onto %s%s", op.Branch, op.Onto, where))
// NOTE: a suffix arg like `where` that itself contains English words must
// instead become its own literal-format branch — the gate cannot see
// through an arg. The plan enumerates these case by case.

// before (decision)
resp, err := deps.decide(ctx, DecisionRequest{ID: deleteBranchID,
    Prompt: "Delete branch " + op.Name + "?", Options: opts})
// after
resp, err := deps.decide(ctx, engine.PromptReq(deleteBranchID,
    "Delete branch %s?", opts, op.Name))
```

## Testing

- **Engine unit:** a helper-invariant test (`Summary` equals the
  concatenated `Sprintf` of `SummaryParts`; same for prompt/detail) plus
  the existing per-op tests — which assert English `Summary` strings and
  must stay green **unchanged** (regression proof #1).
- **TUI unit:** render-seam tests under a throwaway test bundle
  (translated path) and with empty parts (fallback path); a decision-modal
  prompt render test.
- **AST gates:** both gates run in `./test.sh` unit stage; each
  red-proven by mutation during development.
- **e2e:** the whole suite passes **unchanged** (regression proof #2 —
  the CLI surface didn't move).
- `./test.sh race` before merge.

## Docs

- `CHANGELOG.md` — always.
- `CLAUDE.md` — engine package-map entry (Msg, helpers, dual-channel
  contract), i18n entry (the new gate, the seam file, the literal-rule
  exception).
- `README.md` — one line in the i18n section (op status/progress/prompts
  now localized).
- **No `agentskill` bump** — CLI output is unchanged.

## Out of scope / future

- Localizing engine **error** prose beyond the existing `friendlyOpError`
  frame (would need typed errors per site; revisit only on user demand).
- CLI output localization — English by design; this stage's dual-channel
  makes it *possible* later (a locale-aware CLI could render parts), but
  it is explicitly not built.
- The shared-AST-predicates hygiene extraction (three scan tests now
  duplicate helpers; stage-4 deferred item) — fold in only if gate 2's
  implementation naturally extracts them; do not force it.
