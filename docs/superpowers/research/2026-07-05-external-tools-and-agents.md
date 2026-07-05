# Research: running external tools & AI agents from gg

Date: 2026-07-05 · Status: **research** (pre-brainstorm input, not a spec)

## 1. The question

Let the user run external tools from the TUI for three task categories, with
user-definable commands per category (3–4 slots each) plus auto-detection of
known tools that generates sensible default commands:

| Category | Candidate tools |
|---|---|
| Merge-conflict resolution | Meld, Junie, Claude Code |
| Commit-message generation | Claude Code, Junie |
| Review a branch / commit range | Claude Code, Junie |

The open design problem: **how does gg hand the agent enough context that it
knows exactly what is wanted?**

## 2. Key findings (TL;DR)

1. **Agentic CLIs don't need context handed to them — they need cwd + a precise
   task statement.** Both Claude Code and Junie are full agents with their own
   git/file/shell access. Launched with the working directory set to the
   repo/worktree, they run `git status`, `git diff`, `git log` themselves. So
   "providing context" collapses to: (a) spawn in the right directory, (b)
   render a task prompt from state gg already has (conflicted paths, paused-op
   kind, source/target branches, marked commit range), (c) pick the right
   execution mode. Piping diffs is the *exception* (bounded content only —
   Claude caps piped stdin at 10 MB), not the rule, and self-discovery is
   strictly better in gg's target ~100 GB monorepos.

2. **Junie has first-class flags for exactly our categories**: `--merge
   <branch-or-commit>` (merge-conflict resolution task), `--rebase <ref>`
   (rebase-conflict task), `--review ["text"]` (code-review task). GA since
   2026-06-17, standalone binary `junie`, works without a JetBrains IDE,
   BYOK auth possible.

3. **Claude Code's built-in review is CLI-scriptable over a local ref range**:
   `claude -p "/code-review main...feature"` — no PR/GitHub needed. Commit
   messages come out clean via `-p --output-format json` + `.result` (or
   `--json-schema` + `.structured_output`).

4. **Execution mode splits cleanly per category** (mirrors lazygit's
   `output: terminal|popup|log` enum):
   - *Conflicts* → **interactive terminal handover** (suspend TUI, child owns
     the terminal, resume on exit). Multi-round editing; Junie has no CLI
     flag to bypass its own approval UI; Meld is a GUI.
   - *Commit message* → **headless capture** (run silently, capture stdout,
     inject the text into the commit popup).
   - *Review* → both are viable; headless capture into a viewer is the
     scriptable default, interactive handover as an alternative.

5. **Agent exit ≠ task done.** Neither CLI's exit status proves the merge is
   resolved. gg already owns the source of truth: after the child exits,
   re-probe status — the existing `domain.Conflict` / `git.PausedOpIn` /
   resume-paused-op machinery detects "conflicts resolved but op not
   continued" *today*. The external agent slots into that state machine for
   free: let it edit + `git add`, keep `merge --continue` (`engine.ContinueOp`)
   in gg.

6. **gg already has almost every seam this needs** (§7): a tool registry with
   detect/install and a Settings picker (`agentinit`), interactive handover
   (`tea.ExecProcess` in `edit_actions.go`/`open_external.go`), a gated
   user-command runner with env injection and streamed output
   (`ShellHookRunner` + `HookDecisionID`), a placeholder engine with
   interactive fill (`internal/template` + `template_fill.go`), range
   selection (`compareSelectionEndpoints`), and the settingDocs config
   pipeline. New work: PATH-based detection, a `[tools]` config shape (list
   writer), the launcher op, and menu/popup surfaces.

## 3. Per-tool invocation reference

### 3.1 Claude Code (`claude`)

Verified against code.claude.com/docs, 2026-07-05.

- **Modes**: interactive `claude "prompt"` (TUI, human drives) vs headless
  `claude -p "prompt"` (one shot, prints, exits). All flags work in both.
- **Output capture**: `--output-format json` → `.result`, `.session_id`,
  `.total_cost_usd`. `--json-schema '<schema>'` → validated
  `.structured_output` (cleanest way to suppress preamble — the schema is the
  contract). `--max-turns`, `--max-budget-usd` as headless guardrails.
- **Safety**: `--permission-mode` (`acceptEdits` for conflict editing,
  `dontAsk` for read-only headless tasks — auto-denies anything not
  allowlisted, never blocks on a prompt) + `--allowedTools` /
  `--disallowedTools` with prefix rules, e.g.
  `--allowedTools "Bash(git diff *)" "Bash(git log *)" "Read"`. Never
  `bypassPermissions` from gg. Protected paths (`.git`, `.claude`, gitconfig)
  are never auto-approved for file edits in any normal mode.
- **Context**: cwd = repo/worktree; `--add-dir <main-worktree>` when spawning
  in a linked worktree. Piped stdin hard-capped at **10 MB** (v2.1.128+) —
  official guidance is file paths / self-discovery for big inputs.
  `@file` mentions in `-p` prompts: UNVERIFIED, avoid; use plain paths.
- **Auth (headless)**: works once any of `ANTHROPIC_API_KEY`,
  `CLAUDE_CODE_OAUTH_TOKEN` (`claude setup-token`), or a cached interactive
  login exists. On auth failure, `-p` fails with an error on stderr — surface
  it and point at `claude` interactive login.
- **Detection**: `exec.LookPath("claude")`; `claude --version` (parse
  defensively for a leading semver token; exact format undocumented).

Per-category commands:

```bash
# (1) Conflicts — interactive handover; edits + staging allowed, sequencer kept in gg
claude --permission-mode acceptEdits \
  --allowedTools "Read" "Edit" "Bash(git status)" "Bash(git diff *)" "Bash(git log *)" "Bash(git add *)" \
  --disallowedTools "Bash(git commit *)" "Bash(git merge *)" "Bash(git rebase *)" "Bash(git push *)" \
  "A git <op> of <source> into <target> is paused with conflicts in: <conflicted-files>.
   Inspect both sides' history to understand intent, resolve each conflict, then 'git add'
   the resolved files. Do NOT run git commit / --continue — stop when everything is staged
   and summarize what you chose and why."

# (2) Commit message — headless, read-only, clean JSON extraction
claude -p --permission-mode dontAsk \
  --allowedTools "Bash(git diff *)" "Bash(git log *)" "Bash(git status *)" \
  --output-format json \
  --json-schema '{"type":"object","properties":{"subject":{"type":"string"},"body":{"type":"string"}},"required":["subject"]}' \
  "Write a commit message for the staged changes (git diff --cached). Match the style of recent commits."
# → parse .structured_output.subject/.body; add --model haiku / --effort low for latency

# (3) Range review — headless via the bundled skill (accepts local ref ranges)
claude -p "/code-review <range>" --output-format json
# or a custom-report variant:
claude -p --permission-mode dontAsk \
  --allowedTools "Bash(git diff *)" "Bash(git log *)" "Bash(git show *)" \
  --append-system-prompt "Report findings as file:line — issue, grouped by severity." \
  "Review the commit range <range> for bugs, regressions, and security issues."
```

`--bare` (skip CLAUDE.md/hooks/MCP discovery) makes headless calls faster and
deterministic but loses repo commit-style conventions — probably *off* for
commit messages, *on* worth considering for review. Note `--bare` also
ignores OAuth-token auth (API key only).

### 3.2 JetBrains Junie (`junie`)

Verified against junie.jetbrains.com/docs + JetBrains blog, 2026-07-05.
GA 2026-06-17; standalone binary; Linux/macOS/Windows; installs to
`~/.local/bin/junie` (curl installer), also npm `@jetbrains/junie` / brew.

- **Modes**: interactive REPL (`junie`, or `junie --prompt "text"` to
  pre-submit); **print mode** = one-shot headless (`junie "task"` or
  `--task "text"`), with `--output-format text|json` and
  `--json-output-file <path>`.
- **Category flags (first-class!)**:
  - `junie --merge <branch-or-commit>` — merge-conflict resolution task
  - `junie --rebase <branch-or-commit>` — rebase-conflict resolution task
  - `junie --review ["scope text"]` — code-review task (scope is natural
    language; no structured range flag — phrase `"compare against <ref>"`)
- **Approval model**: per-action approval in its own UI; "brave mode" /
  allowlist are **config-file or interactive-only** (`config.json` `"brave"`,
  `~/.junie/allowlist.json`) — **no CLI flag found** to bypass approval.
  ⇒ conflicts and review are terminal-handover categories for Junie;
  whether `--merge`/`--review` behave as pure print-mode without a TTY is
  UNVERIFIED — verify empirically before shipping.
- **Context**: full agent, runs git itself; auto-loads `.junie/AGENTS.md` /
  `AGENTS.md` / legacy `.junie/guidelines.md` (+ global `~/.junie/AGENTS.md`).
  `-p/--project <path>` sets the working dir. Model/effort:
  `--model`, `--provider`, `--effort minimal..max`. Auth: JetBrains account
  OAuth, `JUNIE_API_KEY`/`--auth`, or BYOK provider keys.
- **Headless commit message** (the one solid print-mode fit):

```bash
junie --output-format json --json-output-file "$OUT" \
  --task "Write a concise conventional commit message for the currently staged changes.
          Do not run git commit or modify any files — output only the message text."
```

- **Detection**: `exec.LookPath("junie")`; fallback probe `~/.local/bin/junie`;
  secondary signal `~/.junie/` or project `.junie/`. `junie --version`.
  A parallel EAP channel still exists — probe capabilities at runtime rather
  than hardcoding flag assumptions.

### 3.3 Meld (`meld`)

Per-file 3-way GUI merge only — no repo-level conflict walking; the loop is
the caller's job. Canonical invocation (from git's own `mergetools/meld`
adaptor):

```bash
meld --auto-merge --output="$MERGED" "$LOCAL" "$BASE" "$REMOTE"
```

`--auto-merge` pre-resolves non-conflicting hunks. Detection: `LookPath("meld")`
on Linux/macOS-with-PATH; Windows default install path
`C:\Program Files\Meld\Meld.exe` (usually NOT on PATH — needs an explicit
path setting, same as git's `mergetool.meld.path`).

Two integration strategies:
1. **Shell out to git**: `git mergetool --tool=meld --no-prompt -- <file>` —
   git stages the temp files, runs the loop, applies `trustExitCode`/mtime
   heuristics, and `git add`s on success. Least code; inherits git's UX.
2. **Direct invocation**: gg materializes `LOCAL/BASE/REMOTE` itself from the
   index stages (`:2:`, `:1:`, `:3:` — verbs `CheckoutSide`/`CheckoutBaseStage`
   already exist; a `git show :N:<path>`-to-temp-file variant is small) and
   runs the tool per file under `tea.ExecProcess`. Full control, fits gg's
   event model, and the same quartet works for *any* classic mergetool
   (kdiff3, vscode, …) via git's `mergetools/` registry shapes.

## 4. Context-provisioning patterns (the design core)

Four patterns, in order of preference per situation:

| Pattern | What | When |
|---|---|---|
| **A. Agentic self-discovery** | cwd = repo/worktree + a task prompt rendered from gg state (op kind, source/target, conflicted paths, range) | Default for Claude/Junie in all three categories. Scales to huge repos; agent paginates its own git reads. |
| **B. Env injection** | `GG_*` vars on the child process (the post-create-hook precedent: `GG_MAIN_WORKTREE`, `GG_WORKTREE_PATH`, …) — e.g. `GG_OP`, `GG_SOURCE`, `GG_TARGET`, `GG_CONFLICTED_FILES`, `GG_RANGE` | Complements A for *arbitrary user commands* (wrapper scripts) that can't take a templated prompt. Cheap; do it always. |
| **C. File quartet** | `LOCAL/BASE/REMOTE/MERGED` temp files per conflicted file (git mergetool protocol) | Classic mergetools (Meld); per-file granularity. |
| **D. Content piping / temp files** | Bounded content on stdin or a temp file referenced in the prompt | Only for pre-bounded payloads (a single file's staged diff). Claude stdin caps at 10 MB; avoid whole-repo diffs. |

The prompt in pattern A is where gg's knowledge becomes agent instructions.
Everything needed is already in the model:

- conflicts: `m.conflict.Op` (merge/rebase/cherry-pick/revert),
  `ConflictState.Source/Target`, `m.status.Conflicts()` paths,
  per-file conflict class (both-sides vs modify/delete)
- commit message: staged-file list (`Counts().Staged`), amend flag
- review: `compareSelectionEndpoints()` (◉ marks → range), selected
  branch vs its upstream/main, or a single commit sha

**Boundary rule worth adopting for conflicts** (both agents): the agent may
edit files and `git add`; **continuing the sequencer stays in gg**
(`engine.ContinueOp`), which already drives that state machine, and the
existing paused-op detection (stat-level `PausedOpIn` + zero-conflicts →
resume prompt) is the *completion oracle* after the handover returns.

## 5. Prior art

- **lazygit `customCommands`** — closest existing design. Per-entry:
  `key`, `context` (panel), `command` (Go-template), `prompts`
  (input/menu/menuFromCommand/confirm → `.Form.<key>`), and the crucial
  **`output` enum**: `none | terminal | log | logWithPty | popup`
  (`terminal` = suspend TUI + inherit the real terminal; the rest capture).
  Template roots are per-panel selections: `.SelectedCommit.Sha`,
  `.SelectedCommitRange.From/.To`, `.SelectedFile.Name`,
  `.CheckedOutBranch.Name`, plus `quote` and `runCommand` helpers.
- **git mergetool** — the tool-registry model: built-in adaptors
  (`mergetools/` dir, 24 tools) each knowing one tool's argv shape;
  `mergetool.<tool>.cmd` with `$LOCAL/$BASE/$REMOTE/$MERGED`;
  `trustExitCode` vs mtime heuristic; `git mergetool --tool-help` lists
  *available vs installed* — exactly the auto-detection UX wanted here.
- **Other agent CLIs** converge on the same one-shot convention (aider
  `--message`, Codex `codex exec`, Gemini `gemini -p`, stdin piping, exit
  codes) — so a *generic* user-defined command slot covers future tools
  without gg changes. Notably, **no agent CLI ships a mature
  conflict-resolution pipeline** (aider's is an open feature request);
  Junie's `--merge`/`--rebase` flags are the most direct offering in the
  market right now. (Structural pre-resolution à la mergiraf is a
  merge-*driver* concern, orthogonal to this feature.)

## 6. Proposed shape (sketch, for the brainstorm)

**Registry + config overlay, mirroring `agentinit` + git's mergetools:**

- A hardcoded catalog of known tools (claude, junie, meld, …): binary
  name(s), detection probe, and per-category **default command templates**
  (the §3 command lines, with `<...>` placeholders). Supporting a new tool =
  one catalog entry (agentinit's stated philosophy).
- A `[tools]` config section holding the *user's* commands per category
  (3–4 slots each, as requested). Auto-generation = "for each detected
  catalog tool, offer to write its default command into the config" (visible,
  editable text — the user can tune prompts). Catalog defaults never run
  invisibly; what executes is always what's in config (or explicitly
  confirmed).
- Each command entry carries: label, category, command template, **mode**
  (`terminal` handover vs `capture`), and optionally per-file vs repo-level
  (conflicts only, for Meld-style tools).

**Execution**, two lanes:
- `terminal` → build `*exec.Cmd`, `tea.ExecProcess` (existing editor
  precedent), on return: reload status + let the existing resume-paused-op /
  conflict machinery judge the result.
- `capture` → an engine operation behind a `HookRunner`-style seam
  (approval-gated like `HookDecisionID`, env-injected, output streamed as
  `GitLine` events / captured for the popup) — commit-message text lands in
  the commit popup fields; review reports land in a scrollable viewer (or
  the external read-only temp-file viewer).

**Placeholders**: extend `internal/template`'s token engine (pure leaf,
importable everywhere) with a command vocabulary:
`<conflicted-files>`, `<op>`, `<source>`, `<target>`, `<range>`, `<commit>`,
`<branch>`, `<upstream>`, `<file>`, `<local>/<base>/<remote>/<merged>`,
plus the existing `<user:LABEL>` interactive fill (the TUI's `templateFill`
step already collects those). Needs shell-quoting-aware substitution (NOT
`sanitizeSegment`) — likely argv-level substitution rather than string
splicing, to dodge quoting bugs.

**Surfaces**: conflict process (`x`) gains "Resolve with <tool>…" rows;
commit popup gains a generate-message binding; Commits/Branches `.` menus
gain "Review range/branch with <tool>…" rows (gated on
`compareSelectionEndpoints`); Settings gains a "External tools" manager
(detect → check → write defaults, like the agent-skill picker).

**Safety**: always show the exact command before first run (approval
decision, option-list, fail-closed — the post-create-hook pattern);
per-tool safety rails live *inside* the generated defaults (Claude
allowlists, "do not git commit" prompt clauses); never pass
approval-bypass flags gg can't scope.

## 7. Existing gg seams (from the codebase survey)

| Need | Existing seam |
|---|---|
| Tool catalog + Settings picker | `internal/agentinit` (hardcoded `Builtins()`, `Detect`, `Install`, Settings picker in `settings_popup.go`) — but detection is config-dir stat, NOT PATH; add `exec.LookPath` (injectable-seam precedent: `internal/clipboard/native.go`) |
| Terminal handover | `tea.ExecProcess` in `internal/tui/edit_actions.go` (live edit → status reload on return) and `open_external.go` (0400 temp view, no reload) — the only two ExecProcess sites |
| Gated user-command execution | `internal/engine/hook_runner.go` (`HookSpec{Dir,Env,Script}`, per-OS shell, line-streamed output, stdin=/dev/null) + `post_create_hook.go` (`HookDecisionID` approval, fail-closed, non-fatal, `GG_*` env) |
| Conflict context | `conflict_process.go` state machine, `model.FileStatus` conflict classes, `domain.ConflictState{Op,Source,Target}`, git verbs `CheckoutSide`/`CheckoutBaseStage`, `PausedOpIn` |
| Completion oracle | resume-paused-op machinery: paused op + zero conflicts detected on every status arrival |
| Commit-message injection | `commit_popup.go` `commitPopup{title, desc textfield}` — set fields, `message()` recombines |
| Range selection | `commitCompareSet` ◉ marks + `commit_scope.go` `compareSelectionEndpoints()` |
| Menu wiring | `action_menu.go` `actionRow` + `appendCommitContextRows`; `footer.go` `contextBindings` |
| Config | `settingDocs` registry (`template.go`) + zero-is-unset overlay; multi-line writer precedent `SetWorktreePostCreateHook`. **Gap**: list-of-commands shape needs a new TOML writer + an overlay policy for lists |
| Placeholder engine | `internal/template` (`tokenRe`, `UserLabels`, `Ctx`) + `template_fill.go` — extend token set; current `<branch>` sanitization is path-oriented, wrong for shell args |
| Layering constraints | tui/cli never import git (archtest); mutations via `domain.Execute`; decisions are option-lists; editor-style pure-UI process launch in tui is the allowed exception |

## 8. Open questions for the brainstorm

1. **Config vs catalog authority**: generated defaults written into `.gg.toml`
   (editable, visible, but can go stale) vs resolved at runtime from the
   catalog with config only for user overrides (fresh, but invisible)?
   Leaning: write-on-request via a Settings action, like `gg config populate`.
2. **Conflict granularity**: repo-level agent runs vs per-file mergetool loop —
   two different command kinds in the same category, or two categories?
3. **Where does a captured review land**: popup viewer, read-only temp file in
   `$EDITOR`, or a new scrollable report window? (Reports can be long.)
4. **Commit-message UX**: fill the popup fields directly (needs
   subject/body split — the JSON-schema route gives that for Claude; Junie
   returns free text) vs preview-then-accept?
5. **Trust model for user-edited commands**: approval every run, first run
   only (hash-remembered via `promptstate`?), or per-command "always allow"?
6. **Windows**: `cmd /C` quoting for templated commands, Meld's off-PATH
   install path, agent CLI availability under WSL vs native.
7. **Batch/CLI parity**: should `gg resolve --with <tool>`, `gg commit
   --message-from <tool>`, `gg review <range> --with <tool>` exist for
   scripting parity (and the MCP future)?
8. **Junie print-mode semantics** for `--merge`/`--review` without a TTY are
   UNVERIFIED — needs a live experiment before the spec fixes Junie defaults.

## 9. Sources

Claude Code: code.claude.com/docs (cli-reference, headless, permission-modes,
commands, code-review, authentication, setup, env-vars).
Junie: junie.jetbrains.com/docs (junie-cli, parameters, junie-headless,
junie-review-agent, guidelines-and-memory, action-allowlist,
junie-cli-configuration, environment-variables), blog.jetbrains.com/junie
(2026-03 Beta, 2026-04 IDE connect, 2026-06 GA), github.com/JetBrains/junie.
Prior art: github.com/jesseduffield/lazygit docs/Custom_Command_Keybindings.md,
git-scm.com/docs/git-mergetool, github.com/git/git mergetools/ (incl. meld
adaptor), meldmerge.org, aider.chat/docs (scripting, git),
developers.openai.com/codex/noninteractive, Gemini CLI headless docs.
Items marked UNVERIFIED in §3/§8 could not be confirmed against primary docs.
