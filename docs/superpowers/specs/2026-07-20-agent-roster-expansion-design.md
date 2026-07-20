# Agent roster expansion: Codex, Antigravity, Kimi Code — design

**Date:** 2026-07-20
**Status:** approved (brainstorm 2026-07-20; Gemini CLI dropped mid-design —
the user considers it dead, superseded by Antigravity — and Antigravity
added in its place)
**Base:** main `1b08af11`

## Problem

The user has three newly installed AI agents — OpenAI Codex CLI, Google
Antigravity CLI (`agy`), and Moonshot Kimi Code — and gg knows about them
only partially:

- The **agent-skills picker** (command palette → "Set up agent skills",
  `internal/agentinit`) detects Codex (`~/.codex` → AGENTS.md block) but has
  no entry that detects an Antigravity or Kimi *install*, so the using-gg
  skill cannot be installed for them.
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
| Codex CLI | 0.144.6 | `codex` (PATH, WSL; `~/.local/bin/codex` → `~/.codex/packages/standalone/current/bin/codex`) | `codex --help`: positional `[PROMPT]` "user prompt to start the session"; `--dangerously-bypass-approvals-and-sandbox`; `-s/--sandbox <read-only\|workspace-write\|danger-full-access>`. `codex exec --help`: non-interactive; `-o/--output-last-message <FILE>` "file where the last message from the agent should be written". **Live probes (authenticated):** see Part 2. |
| Antigravity CLI | 1.1.4 | `agy` (`~/.local/bin/agy`; home `~/.gemini/antigravity-cli`) | `agy --help`: `-p/--print/--prompt` "Run a single prompt non-interactively and print the response"; `-i/--prompt-interactive` "Run an initial prompt interactively and continue the session"; `--dangerously-skip-permissions` "Auto-approve all tool permission requests without prompting"; `--mode <accept-edits\|plan>`; `--sandbox`; `--print-timeout` (default 5m0s). Bundled docs (`~/.gemini/antigravity-cli/builtin/skills/agy-customizations/`): skills = `skills/<name>/SKILL.md` with name+description frontmatter; global customization root `~/.gemini/config/`; project root `.agents/`; reads `GEMINI.md`/`AGENTS.md` rules hierarchically. **Live probes (authenticated):** see Part 2. |
| Kimi Code | 0.27.0 | `kimi` (`~/.kimi-code/bin/kimi`; on PATH only via a `.zshrc` export) | `kimi --help`: `-p/--prompt <prompt>` "Run one prompt non-interactively and print the response"; `--output-format <text\|stream-json>` (default text); `-y/--yolo`; `--auto`; `--skills-dir` "instead of auto-discovered user and project directories"; NO interactive-with-initial-prompt flag. Binary strings document skill discovery at `~/.kimi-code/skills/` (legacy `~/.kimi/skills/`) and project `.kimi-code/skills`. **Live probes (authenticated):** see Part 2. |

Gemini CLI (0.51.0) was verified during design but dropped from scope: the
user considers it dead. Its shipped project-level `gemini` picker row stays
untouched (Antigravity reads `GEMINI.md` rules, so the row remains useful).

## Part 1 — agent-skills picker (`internal/agentinit`)

Two new `Builtins()` rows (one line each; picker/CLI pick them up
automatically):

```go
{ID: "antigravity", Label: "Antigravity (global)", Detect: "~/.gemini/antigravity-cli", Target: "~/.gemini/config/skills/using-gg/SKILL.md", Mode: ModeSkillFile},
{ID: "kimi", Label: "Kimi Code (global)", Detect: "~/.kimi-code", Target: "~/.kimi-code/skills/using-gg/SKILL.md", Mode: ModeSkillFile},
```

- Placement: `antigravity` after `codex`; `kimi` after `antigravity`.
- Both use `ModeSkillFile` (the Claude/Junie shape): both tools adopted the
  SKILL.md name+description frontmatter convention, and a lazy,
  description-triggered skill is strictly better than an always-loaded
  context block.
- Antigravity's Detect is the agy-specific home `~/.gemini/antigravity-cli`
  (created by its install), NOT plain `~/.gemini` (which a dead gemini-cli
  install also creates). Its Target is under the documented global
  customization root: `~/.gemini/config/skills/`.
- Codex needs no change — `~/.codex` → `~/.codex/AGENTS.md` already works
  (verified via `gg init --list` on the user's build; the "not detected"
  report was gg.exe on Windows, where Codex simply isn't installed —
  per-environment detection is correct behavior and out of scope to change).

**Verification (at implementation, authenticated):** after installing the
file, `agy -p "List the names of the skills you have available"` must name
`using-gg` (skill names/descriptions are injected into context per the
progressive-disclosure docs, so this needs no file permissions). Kimi has no
cheap list probe; its own binary documents the discovery path, so the
SKILL.md row ships as specified.

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
  over stdout. read-only sandbox still allows reading
  `$GG_CONTEXT_FILE`/`$GG_STAGED_DIFF`.
  **VERIFIED 2026-07-20 (codex 0.144.6 authenticated, live probe):** run
  inside a git repo with stdin `/dev/null` → exit 0, the message file holds
  exactly the final message (no decoration), stdout carries only the final
  message (session log goes to stderr). The trust gate ("Not inside a
  trusted directory") fires only OUTSIDE a git repo — gg always runs the
  lane in a repo, so no `--skip-git-repo-check` is needed.
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

### Antigravity (`ID: "antigravity"`, `Label: "Antigravity"`, `Bins: []string{"agy"}`)

- **conflict** (`Name: "Antigravity"`, ModeTerminal):
  `<bin> --prompt-interactive "<agyConflictPrompt>"` — prompt pre-submitted,
  stays interactive (the Junie `--prompt` pattern); agy prompts for tool
  approvals in-terminal as usual.
- **conflict yolo** (`Name: "Antigravity (yolo)"`, ModeTerminal, OptIn):
  `<bin> --prompt-interactive "<agyConflictPrompt>" --dangerously-skip-permissions`.
- **commit_message** (`Name: "Antigravity"`, ModeCapture, **OptIn**):
  `<bin> -p "<agyCommitPrompt>" --dangerously-skip-permissions` where the
  prompt uses the file-channel wording: write ONLY the message to
  `$GG_MESSAGE_FILE` ("an absolute path outside the repository").
- **review** (`Name: "Antigravity"`, ModeCapture, **OptIn**):
  `<bin> -p "<agyReviewPrompt>" --dangerously-skip-permissions` — review
  content (reads `$GG_REVIEW_DIFF`, labels `(range <range>)`), writes the
  report to `$GG_MESSAGE_FILE`.
- **Why the capture lanes carry `--dangerously-skip-permissions` and are
  OptIn — VERIFIED 2026-07-20 (agy 1.1.4 authenticated, live probes):**
  headless `-p` AUTO-DENIES any permission-gated tool: reading a file
  outside the workspace produced empty output with "a tool required the
  \"read_file\" permission that headless mode cannot prompt for, so it was
  auto-denied". `--mode accept-edits` does NOT lift the denial (probed).
  agy has no CLI allowlist flag (only `permissions.allow` rules in
  settings.json — user-machine config gg must not edit). The only working
  per-run remedy is `--dangerously-skip-permissions` (probed: outside-
  workspace read AND write both succeed, exit 0). Because that bypasses
  agy's own permission prompts, the catalog's OptIn rule applies — the
  wizard shows these rows unchecked; adding them is an explicit opt-in.
  `--sandbox` was probed as defense-in-depth and REJECTED: it polluted
  stdout with agent narration before the payload.
  The file channel (not stdout) is the primary shape because a bare `-p`
  response was observed clean (`ok`) in one probe but narration-prefixed in
  another — stdout is not reliably parseable, while the probed file write
  delivered the exact payload.
- The default (non-OptIn) conflict lane needs no permission flag: it runs
  under terminal handover with a TTY, where agy can prompt interactively.

### Kimi Code (`ID: "kimi"`, `Label: "Kimi Code"`, `Bins: []string{"kimi"}`, `ExtraProbes: []string{"~/.kimi-code/bin/kimi"}`)

- **conflict** (`Name: "Kimi"`, ModeTerminal):
  `<bin> -p "<kimiConflictPrompt>"` — Kimi has no interactive-with-prompt
  flag, so the conflict lane is a headless agentic run printing to the real
  terminal under the normal handover.
  **VERIFIED 2026-07-20 (kimi 0.27.0 authenticated, live probe):** plain
  `-p` (no `--auto`/`--yolo`) edited a file and ran `git add` in a scratch
  repo headlessly — `git status --porcelain` showed the staged `M ` entry,
  exit 0. Under gg's handover a TTY is present, so Kimi may additionally
  choose to prompt for approvals — acceptable (that is interactive mode);
  the OptIn yolo variant covers users who want no prompts.
- **conflict yolo** (`Name: "Kimi (yolo)"`, ModeTerminal, OptIn):
  `<bin> -p "<kimiConflictPrompt>" --yolo`.
- **commit_message** (`Name: "Kimi"`, ModeCapture):
  `<bin> -p "<kimiCommitPrompt>"` where the prompt uses the file-channel
  wording: write ONLY the message to `$GG_MESSAGE_FILE` ("an absolute path
  outside the repository"); the engine prefers the non-empty file over
  stdout.
  **VERIFIED 2026-07-20 (kimi 0.27.0 authenticated, live probes):** `-p`
  stdout is decorated (the response is prefixed `• `, e.g. `• ok`), so
  plain-text stdout parsing would corrupt a subject line — the file channel
  is the primary shape, not a fallback. Plain `-p` read a file outside the
  workspace and wrote the exact payload to another outside-workspace file
  with no approval flags (exit 0), so no `--auto` is needed.
- **review** (`Name: "Kimi"`, ModeCapture): `<bin> -p "<kimiReviewPrompt>"`
  — same file-channel wording, review content (reads `$GG_REVIEW_DIFF`,
  labels `(range <range>)`), writes the report to `$GG_MESSAGE_FILE`.

## Part 3 — `ExtraProbes` home expansion (`internal/exttool`)

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

(No capture-parser change is needed anywhere: Codex and Kimi and Antigravity
all deliver through the `$GG_MESSAGE_FILE` channel or clean plain text — the
Gemini `-o json` envelope extension died with Gemini's removal from scope.)

## Verification status

All output-shape probes are DONE (2026-07-20, all three CLIs
authenticated): Codex exec file channel; Kimi file channel + headless
`git add`; Antigravity permission-denial behavior, skip-permissions
read/write, file channel. The `--help` evidence above covers flag parsing.

Remaining at implementation time:
1. The interactive conflict lanes (Codex positional prompt, Antigravity
   `--prompt-interactive`) rest on `--help` evidence — interactive sessions
   cannot be probed headlessly (same standing as Claude's conflict lane).
2. The Part 1 skill-discovery probe for Antigravity (drop the file, ask agy
   to list its skills). Kimi has no cheap probe; ships on documented paths.
3. Full TUI flow exercised once per the adding-external-tools skill (wizard
   → picker → approval → run) for at least one of the three tools.

## Tests

- `exttool`: existing generic invariants auto-cover the new entries
  (`TestBuiltinsCatalogInvariants`, `TestBuiltinTemplateTokensValidate`).
  Add: a pin test for the Codex quoted file-channel flag (order + quotes);
  a pin test that both Antigravity capture templates carry
  `--dangerously-skip-permissions` AND `OptIn: true` together (the pairing
  is the safety property); `Detect` `~/` expansion (fake stat; empty-home
  skip; absolute probes unaffected; expanded path returned as Bin).
- `agentinit`: extend the existing detection table tests with the two new
  rows (fake home dirs).
- Full suite + race before merge, per repo convention.

## Docs

- `CHANGELOG.md` — Added entry; note the existing-config caveat (the wizard
  never overwrites existing `[[tools.command]]` blocks) and that the
  Antigravity capture rows are OptIn because headless agy requires its
  permission bypass.
- `docs/superpowers/specs/2026-07-05-external-tools-design.md` — catalog
  section gains the three tools (the skill's rule when defaults change).
- `CLAUDE.md` — `exttool` and `agentinit` package-map rows amended.
- `README.md` — only if it enumerates supported agents (check at plan time).
- `internal/agentskill/using-gg.md` — unchanged (no CLI surface change; no
  version bump).

## Out of scope

- Gemini CLI catalog/picker additions (dropped: the user considers it dead;
  the existing project-level `gemini` picker row stays because Antigravity
  reads `GEMINI.md`).
- Cross-environment detection (gg.exe seeing WSL-side installs, or vice
  versa) — detection is intentionally per-environment; installing an agent
  on the other side lights up the existing entries with zero gg changes.
- Windows `ExtraProbes` for the three agents (install locations vary; PATH
  `Bins` detection covers standard installs).
- Any TUI changes — both registries feed the existing picker/wizard/approval
  surfaces automatically.
- Editing the using-gg skill content (self-registration section shipped
  separately in `1b08af11`).
