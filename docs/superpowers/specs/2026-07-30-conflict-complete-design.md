# AI resolve-and-complete (`conflict_complete`) — design

Date: 2026-07-30
Status: approved (brainstorm 2026-07-30)

## What it is

A new agentic-task category, `conflict_complete` ("Resolve & complete").
One agent invocation, launched from the conflict window's `t` picker, that:

1. detects which sequencer operation is paused (merge / rebase; the prompt
   is written generically so cherry-pick / revert also work — the agent
   inspects repository state itself),
2. resolves all conflicted files,
3. stages the resolutions,
4. runs the matching `git merge --continue` / `git rebase --continue`
   (or `cherry-pick`/`revert --continue`),
5. keeps resolving any subsequent rebase-round conflicts until the
   operation finishes,
6. writes an overview of everything it did (files touched, resolution
   choices, rounds, final state) to `$GG_MESSAGE_FILE`.

This differs from the existing `conflict` category in exactly one way:
the agent **owns the sequencer** for the duration of the run. The
`conflict` contract's "never `--continue`/`--abort`" rule is deliberately
inverted here; this is the sanctioned exception, encoded in the new
category's prompts.

Decisions made during brainstorming:

- **Loop owner: the agent.** One yolo invocation does everything.
  (Alternative rejected: a gg-driven engine loop invoking the agent per
  conflict round — much more engine work, a full agent startup per round.)
- **Execution lane: terminal handover** (`tea.ExecProcess`, the conflict-
  lane precedent). The user watches the agent work; the overview comes
  back via `$GG_MESSAGE_FILE` after return. (Alternative rejected:
  headless capture — no visibility on a long multi-round rebase.)
- **Surface: the conflict window `t` picker only.** No new keys, no CLI
  or MCP surface (terminal handover doesn't fit scripting; YAGNI).
- **Yolo-only:** exactly one predefined command per capable agent, the
  permission-bypass variant. No cautious rows for this category.

## Contract (the `defining-agentic-tasks` addition)

**conflict_complete** — provides: everything the `conflict` category
provides (`GG_OP`, `GG_SOURCE`, `GG_TARGET`, `GG_CONFLICTED_FILES`,
`GG_REPO`, `GG_CONTEXT_FILE` — header + one conflicted path per line —
a real TTY, cwd = the worktree) **plus** an empty `GG_MESSAGE_FILE` for
the overview, and `GG_TASK=conflict_complete`.

Expects back: conflicts resolved, **the paused operation completed**
(continue run as many times as needed), and a free-form overview written
to `$GG_MESSAGE_FILE`. If the agent concludes the conflicts are not
safely resolvable, it must stop **without** completing or aborting the
operation and say so in the overview — gg's state refresh then shows the
still-paused op exactly as today (resume-paused-op prompt, `⏸` status
segment). The agent must never `--abort` (that is destruction of the
user's in-flight work; gg's conflict process owns abort).

## Catalog rows — yolo-only

All rows `Category: CatConflictComplete`, `Mode: ModeTerminal`
(terminal handover), `OptIn: true` wherever a bypass flag exists (the
existing invariant: OptIn ⇔ the command carries a permission-bypass
flag):

| Agent | Command shape | Notes |
|-------|--------------|-------|
| Claude | interactive `<bin> "<prompt>" --dangerously-skip-permissions` | OptIn |
| Junie | `<bin> …prompt… --brave` | `--brave` is interactive-only, which fits this lane; OptIn |
| Codex | `<bin> …prompt… --dangerously-bypass-approvals-and-sandbox` | OptIn |
| Antigravity | `<bin> --prompt-interactive "<prompt>" --dangerously-skip-permissions` | OptIn |
| Kimi | headless `-p "<prompt>"` — **no flag possible** (`-p` refuses `--yolo`/`--auto`) | included ONLY if real-binary verification shows print mode executes the git commands (`git add`, `--continue`) autonomously, not just edits. If it won't, Kimi gets no row. Not OptIn by the invariant. |
| Meld | — | not an agent; no row |

Wizard behavior: every row of this category defaults **unchecked** in the
Settings wizard — the whole category is aggressive — extending today's
rule (only OptIn rows default unchecked) with a category-level override.

Every prompt is verified against the real binary before shipping (the
`adding-external-tools` rule: probe the actual flag combination).

### Prompt sketch (per-agent wording varies; this is the contract text)

- You are completing a paused git operation. Inspect the repository to
  determine what is paused (merge, rebase, cherry-pick, revert).
- Resolve every conflicted file on its merits (consult both sides'
  intent; `GG_CONTEXT_FILE` lists the conflicted paths).
- `git add` each resolution, then run the matching `--continue`. If a
  rebase pauses again on a new conflict round, repeat until the
  operation completes.
- Never `--abort`; never force-push; touch nothing outside this
  worktree. If a conflict cannot be resolved safely, stop and leave the
  operation paused.
- When done (or stopped), write an overview to the absolute path in
  `$GG_MESSAGE_FILE`: what was paused, each file and how it was
  resolved, how many rounds, the final state.

## TUI flow

- Conflict window `t` opens the existing external-tool picker; it now
  lists `conflict` rows AND `conflict_complete` rows. The new rows are
  gated on a paused op existing (`m.conflict.Op != ""`) — with conflicts
  but no paused sequencer op there is nothing to "complete". Labels like
  "Claude — resolve & complete (yolo)".
- Same first-run approval popup showing the fully resolved command
  (`promptstate.ApproveToolCommand`, keyed on template text).
- Run via the existing `toolScript` / `tea.ExecProcess` handover. gg
  additionally creates the empty `$GG_MESSAGE_FILE` temp file before the
  run and reads it after (both cleaned up best-effort, the existing
  temp-file pattern).
- On return: full state refresh. If the op finished, the conflict
  process closes itself exactly as if the user had resolved by hand; if
  the agent stopped early, the still-paused state flows into the
  existing conflict/resume wiring.
- If `$GG_MESSAGE_FILE` is non-empty: the overview opens in the existing
  full-screen report viewer (`reviewView`), backed by a temp file so
  `e` (open in `$EDITOR`) works. Empty file → a status-bar note
  ("no overview reported"), never an empty viewer.

## Plumbing checklist (implementation-time)

- `CatConflictComplete` const; `toolUsable`; `config.ValidateToolCommand`.
- Per-agent prompt constants + catalog entries (verified against real
  binaries); wizard category-default-unchecked rule.
- Picker: include the second category, gate rows on a paused op.
- `tool_run.go`: message-file creation, post-run read-back, viewer open.
- i18n keys for new labels (picker rows, status notes, viewer title);
  AST gates will enforce.
- Sync rule: `internal/agentskill/using-gg.md` "Registering yourself as
  a gg tool" gains the new category + contract; bump `agentskill.Version`;
  `gg init --update`; CHANGELOG.

## Testing

- Unit: catalog validation (tokens, OptIn invariant test extension,
  category accepted by config), picker gating (paused op vs conflicts-
  only), message-file read-back (fake script writes the file; empty-file
  path).
- gg-side integration: a fake "agent" shell script that resolves,
  stages, continues, and writes the overview file exercises the full
  return path without a real agent.
- `tui-capture` harness: picker shows/hides the new rows correctly.
- Real-agent behavior is verified manually per agent before its row
  ships (Kimi's row existence depends on this).
