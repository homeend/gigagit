---
name: defining-agentic-tasks
description: Use when defining or changing an AGENTIC TASK in gigagit — what an AI agent is expected to DO (conflict resolution, commit-message generation, change review), the input/output contract of a task, the predefined prompts that encode it, or when any agent-facing surface changes and the embedded using-gg skill must be synced.
---

# Agentic Tasks: what gg expects an agent to do

**Boundary — read this first.** Three concerns, three owners; never blur:

- **What an agent is expected to DO** (task semantics, input/output
  contracts, the prompts encoding them) — THIS skill.
- **How gg runs an external process** (catalog `Tool` entries, detection
  probes, template/token rules, modes, the wizard, the approval gate) —
  `adding-external-tools`. An external tool is not necessarily an agent
  (Meld is a mergetool). Refer to it; never duplicate its content here.
- **How an agent interacts with gg** (CLI verbs, MCP, self-registration)
  — the embedded agent-facing skill `internal/agentskill/using-gg.md`.

## The tasks

| Task | `category` | Human surface | Execution lane |
|------|-----------|---------------|----------------|
| Conflict resolution | `conflict` | conflict window `t` picker | terminal handover (no engine op) |
| Commit-message generation | `commit_message` | commit popup `ctrl+g` | `engine.GenerateMessage` (headless capture) |
| Change review | `review` | `gg review`, TUI review rows | `engine.ReviewChanges` via `domain.ReviewReport` (headless capture) |

## The contract per task

What gg PROVIDES and what it EXPECTS BACK. These are promises to every
agent; changing one is a breaking change to all configured tools.

**conflict** — provides: `GG_OP`, `GG_SOURCE`, `GG_TARGET`,
`GG_CONFLICTED_FILES`, `GG_REPO`, `GG_CONTEXT_FILE` (header + one
conflicted path per line), a real TTY, cwd = the worktree. Expects: edit
the conflicted files, `git add` the results, exit. **Never `git commit`,
`--continue`, or `--abort`** — gg's `ContinueOp` owns the sequencer, and
the resume-paused-op prompt is the completion oracle. (`per_file = true`
is the classic-mergetool variant — quartet paths, one file — not an
agentic task.)

**commit_message** — provides: `GG_CONTEXT_FILE` (numstat + recent
subjects), `GG_STAGED_DIFF` (full staged diff; stat-only note past
`MaxDiffBytes`), an empty `GG_MESSAGE_FILE`, `GG_REPO`,
`GG_TASK=commit_message`; stdin /dev/null, no TTY. Expects: the message —
subject line, blank, body — via stdout OR written to `$GG_MESSAGE_FILE`
(**non-empty file wins**; the channel exists because a task-agent's stdout
is a status report, not the answer). Parsed by
`exttool.ParseCaptureMessage` (plain text, Claude JSON `.result`,
structured-output envelope, raw file text). Never commits.

**review** — provides: the `<range>` token (injection-safe range; empty
for working changes), `GG_CONTEXT_FILE` (range label + numstat),
`GG_REVIEW_DIFF` (full diff), empty `GG_MESSAGE_FILE`, `GG_REPO`,
`GG_TASK=review`. Expects: a free-form markdown report, same
stdout-or-file channel. gg persists it (`domain.ReviewReport`) and must
never receive repo mutations from a review run.

## The prompts ARE the contract

The predefined per-agent command strings in `internal/exttool/exttool.go`
(`claudeConflictPrompt`, `claudeCommitPrompt`, `junieCommitPrompt`,
`claudeReviewCommand`, `junieReviewPrompt`, …) are where these
expectations are actually written down and injected into user config.
Changing what a task expects means changing those strings for EVERY
catalog agent that supports the category — and mirroring the same
expectation in `using-gg.md`'s "Registering yourself as a gg tool"
section, which is what a self-registering agent reads instead of the
catalog. Template mechanics (token safety, prompt-before-variadic-flags,
OptIn variants) are `adding-external-tools` §3 — follow it there.

## Detection gates which agents get offered

An agent's predefined entries appear in the Settings wizard only when the
agent is detected on this machine, and detection is agent-specific
(mechanics: `adding-external-tools` §2). What you decide HERE when adding
a new agent: which of the three tasks it can honestly perform, and how its
prompts meet the contracts above given its quirks — a headless CLI whose
stdout is the answer uses the stdout channel (Claude); a task-agent that
"does work" uses the `$GG_MESSAGE_FILE` file channel with the
"absolute path outside the repository" phrasing (Junie); a tool with no
usable headless mode gets `conflict` only.

## The sync rule (maintenance tail — always runs)

Any change to a task contract, a new task category, a changed agent-facing
CLI verb/flag/exit code, or a changed MCP surface ⇒ update
`internal/agentskill/using-gg.md` IN THE SAME CHANGE:

1. Edit the matching section (Commands / decision rule / "Registering
   yourself as a gg tool" / MCP).
2. Bump `agentskill.Version` (`internal/agentskill/agentskill.go`).
3. `go build ./cmd/gg && ./gg init --update` in your worktree — this
   refreshes the TRACKED `.claude/skills/using-gg/SKILL.md`; **commit it**
   with the change (global agent copies refresh as a side effect).
4. CHANGELOG entry.

`adding-features` step 7b points here for its trigger.

## Adding a NEW task category (sketch)

New engine op on the capture-lane primitives (`GenerateMessage`/
`ReviewChanges`: context file + diff file + `GG_MESSAGE_FILE` +
`CaptureRunner` seam) → add the category to `config.ValidateToolCommand`
and `exttool` (`Cat*` const, `toolUsable`) → author prompts per catalog
agent (contracts above; mechanics in `adding-external-tools`) → TUI/CLI
surface reusing the shared approval gate → the sync rule.
