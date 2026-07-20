# Agent roster expansion: Codex, Gemini CLI, Kimi Code — design

**Date:** 2026-07-20
**Status:** approved (brainstorm 2026-07-20)
**Base:** main `1b08af11`

## Problem

The user has three newly installed AI agents — OpenAI Codex CLI, Google
Gemini CLI, and Moonshot Kimi Code — and gg knows about them only partially:

- The **agent-skills picker** (command palette → "Set up agent skills",
  `internal/agentinit`) detects Codex (`~/.codex` → AGENTS.md block) but has
  no entry that detects a Gemini or Kimi *install* (the existing `gemini` row
  detects a project `GEMINI.md`, which a fresh install never has), so the
  using-gg skill cannot be installed for them.
- The **external-tools catalog** (`internal/exttool`) has no entries for any
  of the three, so the Settings wizard cannot offer them for gg's three
  agentic task categories: conflict resolution, commit-message generation,
  and review.

Relationship to main `1b08af11` (agentskill v49): that change teaches an
agent that already HAS the using-gg skill to self-register `[[tools.command]]`
blocks. This feature is the other half: gg-side detection plus shipped,
verified catalog defaults, so the wizard works without relying on each agent
to self-register.

## Verified binary evidence (2026-07-20, real installs — never web docs)

| Tool | Version | Binary | Evidence |
|---|---|---|---|
| Codex CLI | 0.144.6 | `codex` (PATH, WSL; `~/.local/bin/codex` → `~/.codex/packages/standalone/current/bin/codex`) | `codex --help`: positional `[PROMPT]` "user prompt to start the session"; `--dangerously-bypass-approvals-and-sandbox`; `-s/--sandbox <read-only\|workspace-write\|danger-full-access>`. `codex exec --help`: non-interactive; `-o/--output-last-message <FILE>` "file where the last message from the agent should be written"; `--json`; `--skip-git-repo-check`. |
| Gemini CLI | 0.51.0 | `gemini` (PATH via `npm i -g @google/gemini-cli`) | `gemini --help`: `-p/--prompt` "non-interactive (headless) mode"; `-i/--prompt-interactive` "execute the provided prompt and continue in interactive mode"; `-y/--yolo`; `--approval-mode <default\|auto_edit\|yolo\|plan>`; `-o/--output-format <text\|json\|stream-json>`. `gemini skills list --all` runs WITHOUT auth and lists SKILL.md-convention skills; bundle strings: "Global Skills (~/.gemini/skills" / "Workspace Skills (.gemini/skills". |
| Kimi Code | 0.27.0 | `kimi` (`~/.kimi-code/bin/kimi`; on PATH only via a `.zshrc` export) | `kimi --help`: `-p/--prompt <prompt>` "Run one prompt non-interactively and print the response"; `--output-format <text\|stream-json>` (default text — no json envelope); `-y/--yolo` "Automatically approve all actions"; `--auto` "Start in auto permission mode"; `--skills-dir` "instead of auto-discovered user and project directories"; NO interactive-with-initial-prompt flag. Binary strings document skill discovery at `~/.kimi-code/skills/` (legacy `~/.kimi/skills/`) and project `.kimi-code/skills`. |

Auth status at design time: none of the three is logged in (`codex login
status` → "Not logged in"; no `~/.gemini` oauth creds; no token in
`~/.kimi-code/config.toml`). Consequence: flag-parse probes work now;
output-shape probes need the user to authenticate (see Verification).

## Part 1 — agent-skills picker (`internal/agentinit`)

Two new `Builtins()` rows (one line each; picker/CLI pick them up
automatically):

```go
{ID: "gemini-global", Label: "Gemini CLI (global)", Detect: "~/.gemini", Target: "~/.gemini/skills/using-gg/SKILL.md", Mode: ModeSkillFile},
{ID: "kimi", Label: "Kimi Code (global)", Detect: "~/.kimi-code", Target: "~/.kimi-code/skills/using-gg/SKILL.md", Mode: ModeSkillFile},
```

- Placement: `gemini-global` directly before the existing project-level
  `gemini` row; `kimi` after `codex`.
- Both use `ModeSkillFile` (the Claude/Junie shape): Gemini and Kimi both
  adopted the SKILL.md frontmatter convention, and a lazy, description-
  triggered skill is strictly better than an always-loaded context block.
- The existing project-level `gemini` row (GEMINI.md block) stays.
- Codex needs no change — `~/.codex` → `~/.codex/AGENTS.md` already works
  (verified via `gg init --list` on the user's build; the "not detected"
  report was gg.exe on Windows, where Codex simply isn't installed —
  per-environment detection is correct behavior and out of scope to change).

**Verification (auth-free):** after installing the file, `gemini skills list`
must show `using-gg` as discovered and Enabled.
**Pre-authorized fallback:** if Gemini treats dropped-in skills as
disabled-by-default (inert without `gemini skills enable`), the
`gemini-global` row becomes `Mode: ModeBlock`, `Target: "~/.gemini/GEMINI.md"`
(Gemini's global context file). Kimi has no auth-free list command; its own
binary documents the discovery path, so the SKILL.md row ships as specified.

## Part 2 — external-tools catalog (`internal/exttool`)

Three new `Tool` entries. All templates follow the existing injection
posture: dynamic values only via `<env:GG_*>` generation tokens (rendered
`${NAME}` / `%NAME%`) and the runtime `<range>` prose token inside
double-quoted prompt text; prompts state the sequencer boundary (edit +
`git add` only; never `git commit` / `--continue`); prompt precedes flags.

### Codex (`ID: "codex"`, `Label: "OpenAI Codex"`, `Bins: []string{"codex"}`)

- **conflict** (`Name: "Codex"`, ModeTerminal):
  `<bin> "<codexConflictPrompt>"` — interactive session started with the
  prompt; Codex's own in-terminal approvals apply (default sandbox
  workspace-write + ask).
- **conflict yolo** (`Name: "Codex (yolo)"`, ModeTerminal, OptIn):
  `<bin> "<codexConflictPrompt>" --dangerously-bypass-approvals-and-sandbox`.
- **commit_message** (`Name: "Codex"`, ModeCapture):
  `<bin> exec "<codexCommitPrompt>" --sandbox read-only --output-last-message "<env:GG_MESSAGE_FILE>"`
  — the native file channel: Codex's harness (not the sandboxed agent)
  writes the final message to `$GG_MESSAGE_FILE`, which the engine prefers
  over stdout, so `codex exec`'s log-stream stdout is irrelevant.
  read-only sandbox still allows reading `$GG_CONTEXT_FILE`/`$GG_STAGED_DIFF`.
- **review** (`Name: "Codex"`, ModeCapture): same shape with the review
  prompt (reads `$GG_REVIEW_DIFF`, labels `(range <range>)`) and
  `--output-last-message "<env:GG_MESSAGE_FILE>"`.
- Prompt texts mirror the Claude prompts' content (conflict: read
  `$GG_OP`/`$GG_CONTEXT_FILE`, resolve, `git add`, stop; commit: output ONLY
  the message; review: findings with severity), adjusted to say the final
  assistant message IS the deliverable.
- Note: `--output-last-message`'s argument is the first standalone (outside
  prompt quotes) `<env:...>` use in the catalog — the template text carries
  its own double quotes (`"<env:GG_MESSAGE_FILE>"` → `"${GG_MESSAGE_FILE}"`)
  so a temp path with spaces cannot word-split.

### Gemini CLI (`ID: "gemini"`, `Label: "Gemini CLI"`, `Bins: []string{"gemini"}`)

- **conflict** (`Name: "Gemini"`, ModeTerminal):
  `<bin> -i "<geminiConflictPrompt>"` — prompt pre-submitted, stays
  interactive (the Junie `--prompt` pattern); default approval mode prompts
  in-terminal.
- **conflict yolo** (`Name: "Gemini (yolo)"`, ModeTerminal, OptIn):
  `<bin> -i "<geminiConflictPrompt>" --yolo`.
- **commit_message** (`Name: "Gemini"`, ModeCapture):
  `<bin> -p "<geminiCommitPrompt>" --output-format json` — headless; the
  JSON envelope's `response` field carries the message (see Part 3).
- **review** (`Name: "Gemini"`, ModeCapture):
  `<bin> -p "<geminiReviewPrompt>" --output-format json`.

### Kimi Code (`ID: "kimi"`, `Label: "Kimi Code"`, `Bins: []string{"kimi"}`, `ExtraProbes: []string{"~/.kimi-code/bin/kimi"}`)

- **conflict** (`Name: "Kimi"`, ModeTerminal):
  `<bin> -p "<kimiConflictPrompt>"` — Kimi has no interactive-with-prompt
  flag, so the conflict lane is a headless agentic run printing to the real
  terminal under the normal handover.
  **VERIFIED 2026-07-20 (kimi 0.27.0, live probe):** plain `-p` (no
  `--auto`/`--yolo`) edited a file and ran `git add` in a scratch repo
  headlessly — `git status --porcelain` showed the staged `M ` entry, exit
  0. Under gg's handover a TTY is present, so Kimi may additionally choose
  to prompt for approvals — acceptable (that is interactive mode); the
  OptIn yolo variant covers users who want no prompts.
- **conflict yolo** (`Name: "Kimi (yolo)"`, ModeTerminal, OptIn):
  `<bin> -p "<kimiConflictPrompt>" --yolo`.
- **commit_message** (`Name: "Kimi"`, ModeCapture):
  `<bin> -p "<kimiCommitPrompt>"` where the prompt uses the Junie
  file-channel wording: write ONLY the message to `$GG_MESSAGE_FILE` ("an
  absolute path outside the repository"); the engine prefers the non-empty
  file over stdout.
  **VERIFIED 2026-07-20 (kimi 0.27.0, live probes):** `-p` stdout is
  decorated (the response is prefixed `• `, e.g. `• ok`), so plain-text
  stdout parsing would corrupt a subject line — the file channel is the
  primary shape, not a fallback. Plain `-p` read a file outside the
  workspace and wrote the exact payload to another outside-workspace file
  with no approval flags (exit 0), so no `--auto` is needed.
- **review** (`Name: "Kimi"`, ModeCapture): `<bin> -p "<kimiReviewPrompt>"`
  — same file-channel wording, review content (reads `$GG_REVIEW_DIFF`,
  labels `(range <range>)`), writes the report to `$GG_MESSAGE_FILE`.

## Part 3 — capture-parser extension (`internal/exttool/parse.go`)

Gemini's `--output-format json` envelope is `{"response": "...", "stats":
{...}}` — not Claude's `{"result": ...}`. Extend `captureEnvelope`:

```go
Response *string `json:"response"` // Gemini CLI --output-format json
Error    *struct {
    Message string `json:"message"`
} `json:"error"` // defensive: Gemini error envelope
```

- `ParseCaptureMessage`: new cases after the existing `Result` case —
  `env.Response != nil` behaves exactly like `Result` (strip fence, split
  subject/body); a non-nil `Error` with a non-empty message returns it as
  `err` (like `is_error`).
- `ParseCaptureReport`: same two cases (`Response` passed through unchanged,
  `Error` → err).
- Unknown JSON still falls through to raw text (unchanged contract).
- **Verification item (needs auth):** the exact envelope field names from a
  live `gemini -p "Reply with exactly: ok" --output-format json` run; the
  parser cases ship against the observed shape.

## Part 4 — `ExtraProbes` home expansion (`internal/exttool`)

Kimi's standard install (`~/.kimi-code/bin/kimi`) is on PATH only when the
user's shell sourced the export line; gg launched another way would miss it.
`ExtraProbes` currently supports absolute paths only.

- `Detect` gains a third parameter: `Detect(look, stat, home string)`.
  A probe with a `~/` prefix expands against `home`; empty `home` skips
  `~/` probes (agentinit's hermeticity rule — tests never touch the real
  home). Absolute probes behave as before. A `~/` probe hit yields the
  expanded absolute path as `Detection.Bin` (argv-ready, quoted at
  generation if it has spaces).
- Production call sites (the Settings wizard; find all at plan time) pass
  `os.UserHomeDir()`'s result, "" on error.

## Verification protocol

1. **Auth-free, at implementation time:** flag-parse probes (a wrong flag
   errors before auth; an auth error therefore proves the flags parsed);
   `gemini skills list` discovery check for Part 1; all `--help` evidence
   quoted with version + date in comments next to each entry (the
   adding-external-tools rule).
2. **Auth-needed:** the user authenticates the CLIs they want fully
   verified (`codex login`, first-run Gemini auth). Then:
   `codex exec "Reply with exactly: ok" --sandbox read-only
   --output-last-message <tmp>` (file content check); `gemini -p "Reply with
   exactly: ok" --output-format json` (envelope shape).
   **Kimi is DONE** (2026-07-20, kimi 0.27.0 authenticated): stdout
   decoration (`• ` prefix) confirmed → file channel primary;
   outside-workspace read+write under plain `-p` confirmed; headless
   `git add` under plain `-p` confirmed. No Kimi probes remain.
3. A template whose auth-needed probe cannot run ships in its primary shape
   with the entry comment stating what remains unverified; the changelog
   says the same. Pre-authorized fallbacks above are applied only on
   observed failures, never speculatively.

## Tests

- `exttool`: existing generic invariants auto-cover the new entries
  (`TestBuiltinsCatalogInvariants`, `TestBuiltinTemplateTokensValidate`).
  Add: a pin test for the Codex quoted file-channel flag (order + quotes);
  parser cases for `.response` (message + report), the Gemini error
  envelope, and unknown-JSON fall-through regression; `Detect` `~/`
  expansion (fake stat; empty-home skip; absolute probes unaffected).
- `agentinit`: extend the existing detection table tests with the two new
  rows (fake home dirs).
- Full suite + race before merge, per repo convention.

## Docs

- `CHANGELOG.md` — Added entry; note the existing-config caveat (the wizard
  never overwrites existing `[[tools.command]]` blocks — users who want the
  new defaults for an already-configured category add them via the wizard's
  new rows; no old blocks change meaning).
- `docs/superpowers/specs/2026-07-05-external-tools-design.md` — catalog
  section gains the three tools (the skill's rule when defaults change).
- `CLAUDE.md` — `exttool` and `agentinit` package-map rows amended.
- `README.md` — only if it enumerates supported agents (check at plan time).
- `internal/agentskill/using-gg.md` — unchanged (no CLI surface change; no
  version bump).

## Out of scope

- Cross-environment detection (gg.exe seeing WSL-side installs, or vice
  versa) — detection is intentionally per-environment; installing an agent
  on the other side lights up the existing entries with zero gg changes.
- Windows `ExtraProbes` for the three agents (install locations vary; PATH
  `Bins` detection covers standard installs).
- Any TUI changes — both registries feed the existing picker/wizard/approval
  surfaces automatically.
- Editing the using-gg skill content (self-registration section shipped
  separately in `1b08af11`).
