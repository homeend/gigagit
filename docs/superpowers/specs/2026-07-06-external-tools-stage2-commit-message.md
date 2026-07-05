# External Tools — Stage 2: `commit_message` (capture lane) — Design

**Status:** approved (2026-07-06), ready for plan.
**Builds on:** Stage 1 (`docs/superpowers/specs/2026-07-05-external-tools-design.md`),
merged to `main` at `47603e2`.
**Scope:** TUI-only. Generate a commit message with an external agent, straight
into the commit popup's editable subject/body fields.

## Goal

In the commit popup (`c`), one keypress runs a configured `commit_message`
agent **headless**, captures its output, parses it into subject + body, and
fills the (editable) fields — the user reviews/edits and commits with `ctrl+s`
exactly as today. This adds the **capture lane** the Stage-1 spec reserved
(`mode = "capture"`), distinct from Stage 1's terminal handover.

## What the real binaries do (live-probed 2026-07-06 — the design rests on this)

Stage 1's worst time-sinks were all web-docs-vs-reality, so the Stage-2 defaults
were verified against the installed binaries before this spec. Isolated
throwaway repo, one real invocation each:

**Claude Code 2.1.201** — clean, the primary tool:
- `claude -p "…"` → the final answer as **raw text** on stdout (no tool
  narration leaks). `-p` prints only the final assistant message.
- `--output-format json` → an envelope; the answer is in **`.result`** (string),
  with a boolean **`.is_error`**. Example: `{"type":"result","subtype":"success",
  "is_error":false,…,"result":"hello",…}`.
- `--json-schema '{…subject,body…}'` → the envelope additionally carries
  **`.structured_output`** as a *parsed object* `{"subject":"…","body":"…"}`
  (and `.result` holds the same JSON as an escaped string). It genuinely
  inspected the staged diff and wrote a real subject/body. Slower (a final
  structured tool call); not needed for the default.
- **Machine caveat (not a product bug):** on a box whose shell rewrites `git`
  (this repo's RTK hook turns `git diff` → `rtk git diff`), the
  `Bash(git diff *)` allowlist entry does not match and Claude logs
  `permission_denials` — it still finished via other reads. gg spawns `claude`
  in a **plain** subshell (no RTK), so the allowlist matches there. Confirms
  the allowlist — not an interactive prompt — is the real guard in `-p` mode
  (a denied tool is denied, never hangs).

**Junie 26.6.29** — usable but report-shaped:
- `--json-output-file` is **optional**: "If not specified, output is printed to
  stdout." So `junie --task "…" --output-format json` yields capturable JSON on
  **stdout** — same channel as Claude, no file dance.
- The answer is in **`.result`** (same field name as Claude) **but wrapped as a
  markdown report**: `### Summary\n- …\n\n### Changes\n- …\n\n### Verification\n- …`.
  Junie always frames output as a coding-agent report, so its `.result` is *not*
  a clean commit message.
- `--brave` is "interactive only" ⇒ **no yolo variant** for the headless lane.
- `--review` exists (Stage 3, not here).

**Design consequences:**
1. The parser must be **format-agnostic** (structured object → `.result` string →
   raw text), so the feature never hinges on `--json-schema`.
2. **Claude is the real generator.** **Junie is best-effort** — the parser
   unwraps `.result` and splits it; because the message lands in **editable
   fields the user confirms before replacing**, a report-shaped Junie result is
   trim-and-go, not fatal. The limitation is documented, not hidden. (User
   chose best-effort-Junie over Claude-only.)

## Architecture

```
commit popup (ctrl+g)
  → resolve commit_message tool(s)  [exttool catalog + config]
  → shared approval (first run per repo, promptstate hash)   ← extracted from Stage 1
  → confirm-replace (only if fields non-empty)
  → domain.Execute(engine.GenerateMessage{…})   ← the capture lane (an engine op)
        1. build the staged diff (git diff --cached, capped) + recent-subjects
           header → per-run context file; export as $GG_CONTEXT_FILE
        2. headless run via CaptureRunner (stdin=/dev/null, stderr streamed as
           GitLine, stdout captured, ctx-cancellable)
        3. remove the context file
  → exttool.ParseCaptureMessage(stdout) → {subject, body}
  → gen-guarded fill of the popup's title/desc fields
  → user edits, ctrl+s commits (unchanged)
```

## Context provisioning — give the agent the changes upfront, pre-formatted

The agent is handed the staged changes directly rather than left to discover
them (the user's Stage-1 principle: *"provide content as a file… so the context
can be long and we needn't care about escaping"*). Self-discovery was measured
slow and fragile — the probe's self-discovering run took **57 s / 8 turns** and
hit **3 `permission_denials`** hunting for the diff. Upfront context turns the
common case into ~1 turn with no git-tool gamble.

gg pre-generates **two tmp artifacts** per run and names both to the agent (an
agent-appropriate *summary* plus the *raw detail* on demand):

**1. `$GG_CONTEXT_FILE` — a labeled summary the agent reads first.** Pre-formatted,
not raw plumbing:

```
# gg — write a commit message for the staged changes.
# Output ONLY the message (subject, blank line, body). Do not commit or edit files.
# Full unified diff: <absolute path to the $GG_STAGED_DIFF file>

## Files changed (git diff --cached --stat)
<git.DiffNumstat rendered as a stat block>

## Recent commit subjects (match this style)
<git.LogLines, ~20 newest subjects>
```

The "Full unified diff:" line carries the **resolved absolute path** (not the
literal `$GG_STAGED_DIFF`, which would not expand inside a file the agent reads
as text).

**2. `$GG_STAGED_DIFF` — the full unified staged diff** (`git diff --cached` via
`git.DiffPatch(DiffSpec{Cached:true})`). **Capped** at `MaxDiffBytes` (200 KiB
for Stage 2; configurability deferred); if the patch exceeds the cap the file
carries a *"diff truncated at N KiB — inspect specific files with git"* note
instead of the full body (the stat is already in the summary, so the agent still
knows every file that changed). The default keeps a **read-only git allowlist**
precisely so the agent can drill into individual files in the truncated case.

Both artifacts:
- are **freeform** (labeled text / raw diff bytes) — unlike Stage 1's
  line-structured conflict context file, they are **not** C-quoted (that would
  corrupt the diff) and carry **no** line-forgery risk because gg never parses
  them back; they are opaque content the agent reads;
- are removed after the run (best-effort, like Stage 1's temp files);
- (prompt-injection via diff content is the inherent risk any agent reading repo
  content carries, bounded by the allowlist / permission mode — not by gg
  escaping, per the Stage-1 posture).

The op sets `GG_CONTEXT_FILE`, `GG_STAGED_DIFF`, `GG_REPO`,
`GG_TASK=commit_message` in the env; the default commands reference
`$GG_CONTEXT_FILE` (and, for tools that want raw detail directly,
`$GG_STAGED_DIFF`). A `format-patch`-style patch is deliberately **not**
provided — it embeds a commit message header, which is circular for this task.

The capture lane is a **real `engine.Operation`** (not Stage 1's
`tea.ExecProcess` handover): it runs headless, streams progress, returns
captured output, and is `ctx`-cancellable — exactly the Operation contract.
It mirrors the `HookRunner`/`ShellHookRunner` seam used by the post-create hook.

## Components

### 1. Pure parser — `internal/exttool.ParseCaptureMessage`

New pure function in the existing leaf `internal/exttool` (the external-tool
domain leaf the TUI already imports directly). No git/TUI imports; fully
unit-tested.

```go
// ParseCaptureMessage interprets an agent's captured stdout as a commit
// message. It is format-agnostic across the shapes gg's catalog tools emit.
func ParseCaptureMessage(stdout []byte) (subject, body string, err error)
```

Algorithm (first match wins):
1. Trimmed stdout begins with `{` and parses as JSON:
   a. `is_error == true` → `err` = the tool's error text (`.result` if present,
      else a generic message); **no subject/body** (caller injects nothing).
   b. `.structured_output` is an object with a non-empty string `subject` →
      use `{subject, body}` verbatim (Claude `--json-schema`).
   c. top-level `subject` is a non-empty string → use `{subject, body}`
      (defensive; some tools may emit this directly).
   d. `.result` is a string → set `text = .result`, fall through to (2).
   e. JSON but none of the above → `text = raw stdout`, fall through to (2).
2. Plain text: `text` → `splitMessageText(text)`:
   - subject = first non-blank line, trimmed;
   - body = everything after the first blank line following the subject
     (trimmed of leading blank lines), else "".
3. Empty/whitespace-only result (and not an `is_error`) → `err = ErrEmptyMessage`.

`splitMessageText` is the canonical split; `internal/tui`'s existing
`splitMessage` (used for amend pre-fill) is refactored to delegate to it so
there is one split rule. (`exttool` is a leaf; `tui` may import it — the
dependency direction is legal.)

**Junie note:** a report-shaped `.result` splits to `subject = "### Summary"`.
Accepted as best-effort; the fields are editable and confirmed. No fragile
report-scraping heuristics in Stage 2.

### 2. Catalog — `internal/exttool` `commit_message` templates

Add `CommandTemplate`s for `Category = commit_message`, `Mode = capture`.
Prompt is the **first** argument after `<bin>` (the Stage-1 variadic-flag fix
applies to every stage). No `<user:…>`, `<op>`, `<source>` tokens — the message
task is static; the agent reads the pre-generated context files (below).

The prompts read the pre-generated `$GG_CONTEXT_FILE` / `$GG_STAGED_DIFF`
(shell-expanded to real paths before the agent sees them). `Read` is on Claude's
allowlist so it can open those files; the git allowlist stays as a drill-down
fallback for the truncated-diff case.

**Claude (`capture`)** — default (plain `--output-format json`, split
`.result`; `--json-schema` is a documented power-user alternative the parser
also handles via `.structured_output`):

```
claude -p "Write a git commit message for the staged changes. Read the summary at $GG_CONTEXT_FILE (files changed, recent-commit style) and, for detail, the full diff at $GG_STAGED_DIFF. Output ONLY the commit message and nothing else: a concise imperative subject line (max ~72 chars), a blank line, then a short body explaining what changed and why. No preamble, no markdown headings, no code fences. If the diff file notes it was truncated, inspect specific files with git." --output-format json --allowedTools "Read" "Bash(git diff *)" "Bash(git log *)" "Bash(git show *)" "Bash(git status *)"
```

**Junie (`capture`)** — best-effort:

```
junie --task "Write a git commit message for the staged changes. The change summary is at $GG_CONTEXT_FILE and the full diff at $GG_STAGED_DIFF. Output ONLY the commit message: a concise subject line, a blank line, then a short body. Do not run git commit and do not modify any files." --output-format json --skip-update-check
```

(`--skip-update-check` — the help lists it "Useful for CI or automation".)

The wizard already lists rows per *detected tool × applicable category*; adding
these templates makes `commit_message` rows appear automatically (Stage-1 UI is
category-generic). Existing wizard behavior (checked-if-already-in-config,
append-only, never overwrite) is unchanged.

### 3. Capture op — `internal/engine.GenerateMessage`

```go
type GenerateMessage struct {
    Command string   // the resolved shell command line (already token-resolved + approved)
    Dir     string   // working dir (the repo/worktree root)
    Env     []string // extra env from the caller (e.g. GG_TASK); file-path vars are added by the op
}
func (op GenerateMessage) Run(ctx context.Context, deps OpDeps) (Result, error)
func (op GenerateMessage) LockMode() LockMode // = Read (git reads for the diff; gg writes no refs/tree)
```

The op is self-contained (matters for the MCP future): it builds the context,
runs the agent, captures, cleans up.

1. **Build context** via `deps.Repo` (the git reads that justify `LockMode Read`):
   `DiffPatch(DiffSpec{Cached:true})` for the full staged diff (capped at
   `MaxDiffBytes`), `DiffNumstat(DiffSpec{Cached:true})` for the stat, and
   `LogLines(~20)` for recent subjects. Write the full diff to a temp
   `$GG_STAGED_DIFF` file and the labeled summary to a temp `$GG_CONTEXT_FILE`
   (format in *Context provisioning*). Add `GG_CONTEXT_FILE`, `GG_STAGED_DIFF`,
   `GG_REPO` to the env (alongside the caller's `GG_TASK`).
2. **Run `op.Command` headless** through a new `CaptureRunner` seam on `OpDeps`
   (real: `ShellCaptureRunner`; fake in tests). stdin = `/dev/null`; stdout
   captured to a buffer; stderr streamed line-by-line as `GitLine` events.
   `exec.CommandContext` so `ctx` cancellation kills the process tree (reuse the
   Stage-1 `WaitDelay` guard). The shell expands `$GG_CONTEXT_FILE` /
   `$GG_STAGED_DIFF` in the command at run time (env channel — never re-resolved,
   the Stage-1 `${NAME}` posture).
3. **Return** the captured stdout. **Result contract:** add a single `Captured
   string` field to `engine.Result` (empty for every other op) — the minimal way
   to return bytes through `domain.Execute`'s `(Result, error)`. Remove the temp
   files (best-effort) on the way out.

- **No approval decision inside the op** — approval is handled TUI-side by the
  shared promptstate-hash flow (below), identical to Stage-1 conflict tools, so
  the op just runs an already-approved command. (This is the Stage-1 tool
  posture, not the post-create-hook's in-op `HookDecisionID`; chosen for
  consistency with the conflict lane and the "approve once per repo per
  template" memory.)
- If nothing is staged the diff is empty; the TUI's nothing-staged guard means
  the op is not reached in that case, but the op still tolerates an empty diff
  (writes an empty-diff summary rather than erroring).

```go
type CaptureRunner interface {
    // Capture runs command in dir with env appended to the process env,
    // streaming stderr lines to emit, returning full stdout. ctx kills it.
    Capture(ctx context.Context, dir, command string, env []string, emit func(line string)) (stdout []byte, err error)
}
```

`ShellCaptureRunner` writes the command to a temp script and runs it via the
platform shell (`sh script` / `cmd /C script.bat`) — the Stage-1 `toolScript`
mechanism **without** the hold-on-failure wrapper (we must capture raw stdout,
not decorated terminal output). Temp script removed on return.

### 4. Domain — `internal/domain`

- `GenerateMessage(ctx, spec) (string, error)` (or route through the existing
  `Execute` + read `Result.Captured`) — runs the capture op under its
  reservation, wires the real `ShellCaptureRunner` into `OpDeps`, returns the
  captured stdout. Failure recorded via the existing failure seam (excluding
  `context.Canceled`).
- A cheap **staged-changes** predicate the TUI can consult (reuse the existing
  `Status`/`Snapshot` counts already loaded — no new git call needed).

### 5. TUI — `internal/tui/commit_popup.go` (+ a small shared approval layer)

New `commitPopup` state: `generating bool`, `genGen int` (gen-guard), a spinner
tick, and the chosen `commit_message` command.

Trigger key **`ctrl+g`** ("generate"); hint added to the popup's key line and
`?` help (per the advertise-in-help-and-footer convention). On `ctrl+g`:

1. **Nothing staged** (no staged changes) → `statusMsg "nothing staged to
   describe"`, no-op. (Scope: generate targets the **staged index**; amend with
   nothing staged is disabled — regenerating from a commit's full diff is a
   deferred follow-up.)
2. **Resolve tools:** `m.toolCommands("commit_message")`. Zero → `statusMsg
   "no commit-message tool configured (Settings → External tools)"`, no-op.
   One → use it. More than one → a small chooser layer (the Stage-1 picker
   shape), then continue.
3. **Approval:** check `promptstate` approved-hash for the resolved command.
   Not approved → push the **shared approval popup** (resolved-command preview,
   Run/Cancel; on Run, remember the hash) then continue; approved → continue.
4. **Confirm-replace:** if `title` or `desc` is non-empty → a Replace/Cancel
   confirm before running (avoids a wasted ~30–60 s run the user would discard);
   both empty → run directly.
5. **Run:** dispatch the capture op as an async `tea.Cmd`; set
   `generating = true`, bump `genGen`, start the spinner. Footer/status shows
   `⟳ generating message… [esc] cancel`. `startOp`-style `bgCancel`/ctx so
   `esc` cancels the process immediately.
6. **Completion** (`genMessageMsg{gen, subject, body, err}`): **gen-guard** —
   drop if `msg.gen != popup.genGen`, if the commit popup is no longer the live
   layer, or if the repo switched (`reRoot` bumps a guard / drops the layer).
   The handler must **re-find the live `commitPopup` on the layer stack**
   (value-receiver `Model`; the popup is a pointer field). `err` → `statusMsg`,
   fields untouched, `generating = false`. Success → set `title = subject`,
   `desc = body`, `generating = false`; focus returns to the title field.

**Shared approval layer (refactor):** Stage 1's approval lives inside the
conflict process (`conflictProcess` sub-state). Extract a reusable
`toolApprovalPopup` layer (resolved-command preview, Run/Cancel, promptstate
remember on Run) that **both** the conflict lane and the commit-popup lane use.
No behavior change to the conflict lane; the extraction is verified by its
existing tests plus the new commit-popup path.

## Config (worked example)

`gg config init` / `populate` gain a `commit_message` example under `[tools]`
(same shape as the Stage-1 conflict block), e.g.:

```toml
[[tools.command]]
category = "commit_message"
name     = "Claude"
mode     = "capture"
command  = '''
claude -p "Write a git commit message for the staged changes. Read the summary at $GG_CONTEXT_FILE and, for detail, the full diff at $GG_STAGED_DIFF. Output ONLY the message: a concise subject line, a blank line, then a short body. No preamble." --output-format json --allowedTools "Read" "Bash(git diff *)" "Bash(git log *)" "Bash(git status *)"
'''
```

(`$GG_CONTEXT_FILE` / `$GG_STAGED_DIFF` are written literally in the `'''…'''`
TOML literal — no escaping; the shell expands them at run time.)

`mode = "capture"` is now a **live** mode (Stage 1 treated it as inert with a
"not supported yet" note; that note is removed for `commit_message`). Validation
(`ValidateToolCommand`) already accepts `capture` and the `commit_message`
category; only the inert-treatment note changes.

## Error handling & edge cases

| Case | Behavior |
|---|---|
| No `commit_message` tool configured | `ctrl+g` = hint, no-op (key still shown; points at Settings) |
| Nothing staged | hint, no-op |
| Fields non-empty | Replace/Cancel confirm before running |
| Agent non-zero exit / spawn failure | `statusMsg` with tool name + error; fields untouched |
| `is_error: true` envelope | parsed as failure; surface `.result` text; inject nothing |
| Empty/blank output | `statusMsg "empty message"`; fields untouched |
| `esc` during run | cancel ctx (kills process); `generating=false`; fields untouched |
| Popup closed / repo switched mid-run | stale `genMessageMsg` dropped (gen-guard) |
| Junie report-shaped output | best-effort split; user edits (documented) |

Nothing ever auto-commits; `ctrl+s` (the user) remains the only commit trigger.

## Testing

- **Parser** (`internal/exttool`, pure unit): Claude plain text; Claude json
  envelope (`.result`); Claude `--json-schema` (`.structured_output`);
  `is_error:true`; Junie report `.result`; top-level `subject`; garbage /
  non-JSON; empty. Assert subject/body/err for each. Fixtures drawn from the
  live probe outputs recorded in this spec.
- **Engine** (`internal/engine`, `FakeCaptureRunner` capturing the env + cwd it
  is handed): success returns `Result.Captured`; non-zero exit → error; `ctx`
  cancel → cancellation; stderr lines surfaced as `GitLine`; `LockMode()==Read`.
  **Context build** (real git in a `t.TempDir` repo with a staged change): the
  `$GG_CONTEXT_FILE` the runner receives exists and contains the stat + recent
  subjects; `$GG_STAGED_DIFF` exists and contains the patch; an over-`MaxDiffBytes`
  diff yields a truncation note (stat retained); an empty staged set is tolerated;
  both temp files are gone after `Run` returns.
- **TUI** (`internal/tui`): `ctrl+g` with zero tools / nothing staged = no-op +
  hint; gen-guard drops a stale completion; confirm-replace path; success fills
  subject/body; `esc` cancels. Inject a temp `promptstate` store (the
  `promptTestModel` pattern — `New(nil)` wires the real machine file).
- **Shared approval**: the extracted layer's Run/Cancel + remember, exercised
  from both lanes; conflict-lane regression covered by existing tests.
- No e2e (TUI-only; no CLI verb).

## Out of scope / deferred

- **CLI verb** (`gg commit --generate`, `gg tools ls/detect`) — user chose
  TUI-only. Revisit in a later stage.
- **Stage 3 `review`** — capture lane → report file → external viewer.
- **Amend from a commit's full diff** — Stage 2 targets the staged index only.
- **Junie clean output** — revisit if JetBrains adds a terse/non-report mode.
- **Configurable timeout** — rely on user `esc`; agents legitimately run long.

## Surfaces summary

| Surface | Change |
|---|---|
| `internal/exttool` | `commit_message` templates (Claude, Junie) + `ParseCaptureMessage` |
| `internal/engine` | `GenerateMessage` capture op (builds the diff+summary context artifacts, runs headless, captures) + `CaptureRunner` seam + `Result.Captured` |
| `internal/domain` | expose the capture op (wires `ShellCaptureRunner`); staged-changes predicate (reused counts) |
| `internal/tui/commit_popup.go` | `ctrl+g` generate: resolve → approve → confirm → run → gen-guarded fill; spinner/cancel |
| `internal/tui` (approval) | extract shared `toolApprovalPopup` (conflict + commit lanes) |
| Config template | `commit_message` worked example; `capture` mode now live |
| Docs | CHANGELOG, README (commit-popup key), CLAUDE.md (engine/exttool notes), help/footer |
