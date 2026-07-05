---
name: adding-external-tools
description: Use when adding a new external tool or AI agent (like Claude Code, Junie, Meld) to gigagit's external-tools catalog pool, or when changing an existing tool's default commands.
---

# Adding an External Tool to the Catalog

Supporting a new tool is **one `Tool` entry in `exttool.Builtins()`**
(`internal/exttool/exttool.go`) — never a runtime definition. The wizard,
picker, approval gate, execution, and logging all pick it up automatically;
no TUI/config code changes are needed for a standard addition.

## Checklist

1. **Verify against the REAL binary first, not web docs.** Run
   `<tool> --help` on an actual install and read the flags yourself. Web
   documentation lied twice during stage 1: Junie's documented
   `--merge`/`--rebase` don't exist in the standalone CLI (it died with
   `Failed to build 'issue.md.junie_standalone'`), while the undocumented
   `--brave` does exist. Quote the `--help` evidence (with the tool version
   and date) in a comment next to the entry.

2. **Write the `Tool` entry:**
   - `ID`/`Label` — stable id, human label.
   - `Bins` — candidate binary names for `LookPath` (bare name lands in the
     generated command, keeping config portable).
   - `ExtraProbes` — absolute install paths for off-PATH platforms (Meld on
     Windows: `C:\Program Files\Meld\Meld.exe`); a probe hit puts the
     absolute path into the command (auto-quoted if it has spaces).
   - `Commands` — one `CommandTemplate` per category entry (stage 1:
     `CatConflict` only).

3. **Command template rules** (all enforced by tests — see §4):
   - Start from `<bin>` (replaced at generation).
   - **Prompt BEFORE variadic flags.** Claude's `--allowedTools`/
     `--disallowedTools` consume every following argument — a trailing
     prompt gets eaten word-by-word ("Permission deny rule \"A\"…"). If the
     tool has list-valued flags, positional text must precede them.
   - **No raw prose tokens in defaults.** Dynamic values flow through
     `<env:GG_*>` generation tokens (rendered `${NAME}` POSIX / `%NAME%`
     Windows) and the per-run context file (`<env:GG_CONTEXT_FILE>`), so a
     hostile filename/refname is data, never shell code. Path tokens
     (`<local> <base> <remote> <merged> <repo> <file> <context-file>`) are
     fine — they substitute shell-quoted.
   - Agents must be told the **sequencer boundary**: edit + `git add` only;
     never `git commit` or `--continue` (gg's `ContinueOp` owns that, and
     the resume-paused-op prompt is the completion oracle).
   - Classic mergetools: `PerFile: true` + the quartet tokens
     (`--output=<merged> <local> <base> <remote>`); gated to both-sides
     conflicts automatically.
   - `WhenOp` filters an entry to one paused op (`merge`/`rebase`/…);
     empty = any.
   - Aggressive variants (yolo/auto-approve modes): separate entry with
     `OptIn: true` — the wizard renders it unchecked so it is an explicit
     choice. Only add one if the binary really has a CLI flag for it.

4. **Tests.** The generic invariants cover new entries automatically
   (`TestBuiltinsCatalogInvariants`: unique `(category,name)`, `<bin>`
   present, valid enums; `TestBuiltinTemplateTokensValidate`: tokens
   resolve post-generation on linux+windows; the no-raw-prose invariant).
   Add a pin test only when specific flags/ordering matter (see
   `TestClaudeYoloTemplate` pinning prompt-before-flags).

5. **Runtime behavior you inherit for free:** temp-script execution under
   `$SHELL`/`cmd /C` with the 11 `GG_*` env vars; first-run approval
   (remembered per repo by template hash — any text change re-prompts);
   hold-terminal-on-failure (`press Enter`) except interrupt exits;
   ctrl-C/SIGTERM (including signal-death of the wrapper) treated as a
   normal quit, not a failure; one exit-disposition span per run in
   `operations.log` when `[debug] log_operations` is on.

6. **Existing-config caveat.** The wizard never overwrites existing
   `[[tools.command]]` blocks. After changing a catalog default, users with
   the old block must delete it and re-run the wizard — say so in the
   changelog entry.

7. **Docs:** update the spec's catalog section
   (`docs/superpowers/specs/2026-07-05-external-tools-design.md`) if the
   default-command set changed, plus `CHANGELOG.md`; `README.md`/`CLAUDE.md`
   only if the user-facing surface or package contracts moved.

## Live verification before shipping a default

Run the command shape headlessly against the real binary where possible
(e.g. `claude -p "Reply with exactly: ok" <flags> --output-format json`
proves flag parsing without an interactive session), then exercise the full
TUI flow once: wizard → conflict `x` → `t` → approval → handover → return.
