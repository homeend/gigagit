// Package exttool is the hardcoded catalog of external tools/AI agents gg can
// run per task category (conflict resolution, resolve-and-complete,
// commit-message generation, and code review), plus their detection.
// Supporting a new tool is a code change (one Builtins entry), never a
// runtime definition — the agentinit philosophy.
// The catalog's command TEMPLATES never execute directly: the Settings wizard
// materializes them as editable [[tools.command]] blocks in the gg config, and
// only config content runs.
package exttool

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/homeend/gigagit/internal/template"
)

// Category is a task category a command belongs to.
type Category string

const (
	CatConflict      Category = "conflict"
	CatCommitMessage Category = "commit_message"
	CatReview        Category = "review"
	// CatConflictComplete is the resolve-AND-complete task: unlike CatConflict
	// (whose contract forbids --continue — gg's ContinueOp owns the sequencer),
	// this category's agent deliberately OWNS the sequencer for the run:
	// resolve, stage, continue, repeat through further rebase rounds, then
	// report an overview via GG_MESSAGE_FILE. Yolo-only: one bypass-flag
	// variant per agent, no cautious row.
	CatConflictComplete Category = "conflict_complete"
)

// Mode is how a command runs: terminal = suspend the TUI and hand over the
// real terminal (interactive agents, GUI mergetools); capture = run headless
// in the background while gg's TUI stays up with a "running" box (the
// commit_message/review lanes, and headless conflict agents like `kimi -p`
// that draw no terminal UI of their own).
type Mode string

const (
	ModeTerminal Mode = "terminal"
	ModeCapture  Mode = "capture"
)

// CommandTemplate is one catalog default command. Command contains <bin>
// and may contain <env:NAME> (both replaced at generation time by
// GenerateCommand/GenerateCommandFor) plus runtime tokens resolved by
// template.ResolveCommand. Defaults use only <bin>/<env:...>/path/enum
// tokens for dynamic content — never a raw prose token — per the injection
// posture in the design spec.
type CommandTemplate struct {
	Category Category
	Name     string // menu label; unique per category across the catalog
	Mode     Mode
	PerFile  bool   // true = runs once per conflicted file (mergetools)
	WhenOp   string // "" = any paused op; else merge|rebase|cherry-pick|revert
	// OptIn marks an aggressive variant (a yolo/auto-approve mode that
	// bypasses the agent's own permission prompts): the Settings wizard
	// shows OptIn rows UNCHECKED by default so adding one is an explicit
	// opt-in; everything else defaults checked.
	OptIn   bool
	Command string
}

// Tool is one catalog entry. Bins are candidate binary names probed via
// LookPath; ExtraProbes are absolute paths probed via Stat for installs that
// are typically off PATH (Meld on Windows).
type Tool struct {
	ID          string
	Label       string
	Bins        []string
	ExtraProbes []string
	Commands    []CommandTemplate
}

// claudeConflictPrompt is the double-quoted conflict-resolution prompt shared
// by both Claude conflict templates. It uses only generation-time tokens
// (<env:...>) for its dynamic content — no raw prose token — per the
// injection-posture amendment: the prompt reads the paused op and the context
// file (op/source/target/conflicted paths) from GG_* env vars rather than
// having gg substitute untrusted values into the prompt text itself.
const claudeConflictPrompt = `"A git <env:GG_OP> operation is paused with conflicts in this repository.
   Read the context file at <env:GG_CONTEXT_FILE> for the operation's parties and the conflicted paths.
   Inspect both sides' history to understand intent, resolve each conflict by editing the files,
   then run git add on each resolved file. Do NOT run git commit or any --continue command --
   stop when everything is staged and summarize what you chose and why."`

// claudeConflictCommand is the default (permission-gated) Claude template.
//
// The double-quoted prompt is the FIRST argument after <bin>, with
// --allowedTools/--disallowedTools following it — NOT the other way around.
// Claude's --allowedTools/--disallowedTools are variadic: they greedily
// consume every following argument until the next recognized flag, so a
// trailing prompt placed after --disallowedTools gets eaten word-by-word as
// deny rules (surfacing as "Permission deny rule ... matches no known tool")
// and Claude launches with no prompt at all.
const claudeConflictCommand = `<bin> ` + claudeConflictPrompt + ` \
  --permission-mode acceptEdits \
  --allowedTools "Read" "Edit" "Bash(git status)" "Bash(git diff *)" "Bash(git log *)" "Bash(git add *)" \
  --disallowedTools "Bash(git commit *)" "Bash(git merge *)" "Bash(git rebase *)" "Bash(git push *)"`

// claudeConflictYoloCommand is the OptIn yolo variant: same prompt (its
// do-NOT-commit clause stays as guidance), but --dangerously-skip-permissions
// bypasses Claude's permission evaluation entirely — so no
// --allowedTools/--disallowedTools flags: bypass mode never consults them and
// listing them would be dead weight. The prompt stays the FIRST argument
// after <bin> (same ordering contract as claudeConflictCommand).
const claudeConflictYoloCommand = `<bin> ` + claudeConflictPrompt + ` --dangerously-skip-permissions`

// junieConflictPrompt is the double-quoted conflict-resolution prompt shared
// by both Junie conflict templates. Same shape and injection posture as
// claudeConflictPrompt (generation-time <env:...> tokens only), but shipped
// via --prompt rather than a positional task argument — see junieConflictCommand.
const junieConflictPrompt = `"A git <env:GG_OP> operation is paused with conflicts in this repository. Read the context file at <env:GG_CONTEXT_FILE> for the operation's parties and the conflicted paths. Resolve each conflict by editing the files, then run git add on each resolved file. Do NOT run git commit or any --continue command - stop when everything is staged and summarize what you chose."`

// junieConflictCommand is the default Junie template: --prompt starts Junie's
// normal interactive mode with the prompt pre-submitted (see Builtins' doc
// comment for why the catalog abandoned --merge/--rebase).
const junieConflictCommand = `<bin> --prompt ` + junieConflictPrompt

// junieConflictYoloCommand is the OptIn yolo variant: same prompt, with
// --brave appended to turn on Junie's Brave Mode (auto-approve, interactive
// only — and gg always runs conflict commands under terminal handover, i.e.
// exactly Junie's interactive mode).
const junieConflictYoloCommand = junieConflictCommand + ` --brave`

// claudeCommitPrompt: capture-lane commit-message prompt. Dynamic content via
// <env:...> only (injection posture). Prompt is the FIRST arg after `<bin> -p`.
const claudeCommitPrompt = `"Write a git commit message for the staged changes. Read the summary at <env:GG_CONTEXT_FILE> (files changed, recent-commit style) and, for detail, the full diff at <env:GG_STAGED_DIFF>. Output ONLY the commit message and nothing else: a concise imperative subject line (max ~72 chars), a blank line, then a short body explaining what changed and why. No preamble, no markdown headings, no code fences. If the diff file notes it was truncated, inspect specific files with git."`

// claudeCommitCommand is the default (capture-mode) commit_message template.
// --output-format json gives a parseable envelope (.result). Read is on the
// allowlist so the agent can open the context files; git verbs stay as a
// drill-down fallback for a truncated diff. As with the conflict templates,
// Claude's variadic --allowedTools must come LAST, after the prompt.
const claudeCommitCommand = `<bin> -p ` + claudeCommitPrompt + ` \
  --output-format json \
  --allowedTools "Read" "Bash(git diff *)" "Bash(git log *)" "Bash(git show *)" "Bash(git status *)"`

// junieCommitPrompt is the capture-lane commit-message prompt for Junie.
// Junie is a task-agent: its stdout (--output-format json .result) is only a
// work report, never the message itself (verified 2026-07-06 — the .result was
// "### Summary / ### Changes / ### Verification", not a commit message). So the
// message comes back through the file at $GG_MESSAGE_FILE, which the engine
// reads and prefers over stdout (see engine.GenerateMessage's output-channel
// contract). The "absolute path outside the repository" clause is load-bearing:
// a coding agent can refuse to write outside its project root, and that hint is
// what tells Junie the write is allowed (probe #3, 2026-07-06).
const junieCommitPrompt = `"Your task is to write a git commit message for the staged changes into the file at <env:GG_MESSAGE_FILE> (an absolute path outside the repository). The change summary is at <env:GG_CONTEXT_FILE> and the full diff at <env:GG_STAGED_DIFF>. Write ONLY the commit message to that file: a concise subject line, a blank line, then a short body. Do not run git commit and do not modify any other files."`

// junieCommitCommand is the default (capture-mode) Junie commit_message
// template. No yolo variant: Junie's --brave is interactive-only (Brave
// Mode), useless on a headless capture run.
const junieCommitCommand = `<bin> --task ` + junieCommitPrompt + ` --output-format json --skip-update-check`

// claudeReviewCommand — verified 2026-07-07: `claude -p "/code-review <range>"`
// runs headless and .result is a clean severity-structured markdown report.
// <range> is a runtime token (resolved by template.ResolveCommand). For the
// uncommitted target <range> resolves empty and /code-review reviews the tree.
const claudeReviewCommand = `<bin> -p "/code-review <range>" \
  --output-format json \
  --permission-mode acceptEdits \
  --allowedTools "Read" "Bash(git diff *)" "Bash(git log *)" "Bash(git show *)" "Bash(git status *)"`

// junieReviewPrompt — Junie is a task-agent: its stdout is a report, so the
// review comes back through $GG_MESSAGE_FILE (the Stage-2 channel). Junie's own
// --review flag reviews only uncommitted working changes and can't take a
// range, so we feed it the diff at $GG_REVIEW_DIFF instead (verified 2026-07-07).
const junieReviewPrompt = `"You are reviewing a code change. The full diff to review is in the file at <env:GG_REVIEW_DIFF> (range <range>). Write a concise code review — findings with severity and a short summary — into the file at <env:GG_MESSAGE_FILE> (an absolute path outside the repository). Do NOT modify any repository files and do NOT run git commit."`

const junieReviewCommand = `<bin> --task ` + junieReviewPrompt + ` --output-format json --skip-update-check`

// kimiCommitPrompt is the capture-lane commit-message prompt for the Kimi
// Code CLI. Verified 2026-07-20 against a live install (kimi 0.27.0): in
// print mode (`-p/--prompt <prompt>`) stdout is a work report — reasoning
// bullets plus a "To resume this session" trailer — never the clean answer,
// so like Junie the message comes back through $GG_MESSAGE_FILE (the engine
// reads that file and prefers it over stdout). A live `kimi -p` probe the
// same day confirmed the agent reads $GG_CONTEXT_FILE/$GG_STAGED_DIFF and
// writes $GG_MESSAGE_FILE with no permission prompt, exit 0. The "absolute
// path outside the repository" clause is load-bearing (a coding agent can
// refuse to write outside its project root) — same rationale as Junie's.
const kimiCommitPrompt = `"Your task is to write a git commit message for the staged changes into the file at <env:GG_MESSAGE_FILE> (an absolute path outside the repository). The change summary is at <env:GG_CONTEXT_FILE> and the full diff at <env:GG_STAGED_DIFF>. Write ONLY the commit message to that file: a concise subject line, a blank line, then a short body. Do not run git commit and do not modify any other files."`

// kimiCommitCommand is the default (capture-mode) Kimi commit_message
// template. `kimi -p` takes the prompt as its flag value, so there is no
// variadic-flag argv-order hazard. No yolo variant: print mode is headless
// either way (the probes above needed no approval flag).
const kimiCommitCommand = `<bin> -p ` + kimiCommitPrompt

// kimiReviewPrompt — same stdout-is-a-report posture as kimiCommitPrompt:
// the review comes back through $GG_MESSAGE_FILE, fed the diff at
// $GG_REVIEW_DIFF. <range> is a runtime token (resolved by
// template.ResolveCommand), empty for the uncommitted target.
const kimiReviewPrompt = `"You are reviewing a code change. The full diff to review is in the file at <env:GG_REVIEW_DIFF> (range <range>). Write a concise code review — findings with severity and a short summary — into the file at <env:GG_MESSAGE_FILE> (an absolute path outside the repository). Do NOT modify any repository files and do NOT run git commit."`

const kimiReviewCommand = `<bin> -p ` + kimiReviewPrompt

// kimiConflictPrompt is the conflict-resolution prompt for Kimi, same shape
// and injection posture as claudeConflictPrompt (generation-time <env:...>
// tokens only), including the sequencer-boundary clause.
const kimiConflictPrompt = `"A git <env:GG_OP> operation is paused with conflicts in this repository. Read the context file at <env:GG_CONTEXT_FILE> for the operation's parties and the conflicted paths. Resolve each conflict by editing the files, then run git add on each resolved file. Do NOT run git commit or any --continue command -- stop when everything is staged and summarize what you chose."`

// kimiConflictCommand is the conflict template: a headless `-p` run. Kimi
// has no "start interactive mode with the prompt pre-submitted" flag, and
// `-y`/`--auto` are REJECTED in combination with `-p` ("Cannot combine
// --prompt with --yolo", 0.27.0) — but print mode needs neither: it approves
// the edits itself. Verified end-to-end 2026-07-20 (kimi 0.27.0): against a
// real paused merge, `kimi -p` read $GG_CONTEXT_FILE, edited the conflicted
// file, ran `git add`, and exited 0.
//
// The template is ModeCapture, not the terminal handover the other conflict
// agents use: `kimi -p` draws no UI until its final response, so under a
// handover the user stares at a dead screen for minutes. Capture keeps gg's
// TUI up with a "running" box instead; kimi's stdout is only a work report
// (see kimiCommitPrompt), so nothing is lost by not showing it live.
const kimiConflictCommand = `<bin> -p ` + kimiConflictPrompt

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

// Resolve-and-complete (CatConflictComplete) prompts. The shared contract:
// read the paused op from GG_OP/GG_CONTEXT_FILE, resolve every conflict,
// git add, run the MATCHING --continue with GIT_EDITOR=true (so no editor
// can block a handover-less run), repeat through further rebase rounds,
// NEVER --abort or push, stop-and-leave-paused when unsafe, and write the
// overview to GG_MESSAGE_FILE (the "absolute path outside the repository"
// phrasing is load-bearing for task-agents — the Junie precedent).
const claudeCompletePrompt = `"A git <env:GG_OP> operation is paused with conflicts in this repository.
   Read the context file at <env:GG_CONTEXT_FILE> for the operation's parties and the conflicted paths.
   Inspect both sides' history to understand intent, resolve every conflict by editing the files,
   and run git add on each resolved file. Then COMPLETE the operation: run the matching continue
   command (git merge --continue, git rebase --continue, git cherry-pick --continue, or
   git revert --continue) with GIT_EDITOR=true so no editor can block. If the operation pauses
   again on new conflicts, resolve them the same way and continue again, until it finishes.
   NEVER run any --abort command and never push. If a conflict cannot be resolved safely, stop
   and leave the operation paused instead. Finally, write an overview to the file at
   <env:GG_MESSAGE_FILE> (an absolute path outside the repository): which operation was paused,
   each file and how you resolved it, how many continue rounds ran, and the final state."`

// claudeCompleteCommand: prompt FIRST (the variadic-flag ordering contract),
// bypass flag only — bypass mode never consults allow/deny lists.
const claudeCompleteCommand = `<bin> ` + claudeCompletePrompt + ` --dangerously-skip-permissions`

const junieCompletePrompt = `"A git <env:GG_OP> operation is paused with conflicts in this repository. Read the context file at <env:GG_CONTEXT_FILE> for the operation's parties and the conflicted paths. Resolve every conflict by editing the files, run git add on each resolved file, then COMPLETE the operation: run the matching continue command (git merge --continue, git rebase --continue, git cherry-pick --continue, or git revert --continue) with GIT_EDITOR=true so no editor can block. If it pauses again on new conflicts, resolve and continue again until it finishes. NEVER run any --abort command and never push. If a conflict cannot be resolved safely, stop and leave the operation paused. Finally write an overview (which operation, each file and how you resolved it, how many continue rounds ran, the final state) into the file at <env:GG_MESSAGE_FILE> (an absolute path outside the repository)."`

const junieCompleteCommand = `<bin> --prompt ` + junieCompletePrompt + ` --brave`

const codexCompletePrompt = junieCompletePrompt

const codexCompleteCommand = `<bin> ` + codexCompletePrompt + ` --dangerously-bypass-approvals-and-sandbox`

const agyCompletePrompt = junieCompletePrompt

const agyCompleteCommand = `<bin> --prompt-interactive ` + agyCompletePrompt + ` --dangerously-skip-permissions`

// kimiCompleteCommand: headless -p (Kimi has no interactive-with-prompt
// mode, and -p REFUSES --yolo/--auto — but print mode approves its own
// edits and was verified 2026-07-20 running `git add` autonomously, so it
// can honestly attempt the task without a flag; hence NOT OptIn, per the
// invariant). ModeCapture like kimiConflictCommand: -p draws no UI, a
// handover would show a dead screen. MUST be re-verified against a real
// paused rebase before merge — if print mode refuses to run the --continue
// commands, DELETE this row (the spec's Kimi conditional).
const kimiCompletePrompt = junieCompletePrompt

const kimiCompleteCommand = `<bin> -p ` + kimiCompletePrompt

// Builtins is the hardcoded catalog. Stage 1 shipped conflict templates;
// stage 2 added commit_message capture templates; stage 3 adds review
// capture templates.
func Builtins() []Tool {
	return []Tool{
		{
			ID: "claude", Label: "Claude Code", Bins: []string{"claude"},
			Commands: []CommandTemplate{
				{Category: CatConflict, Name: "Claude", Mode: ModeTerminal, Command: claudeConflictCommand},
				{Category: CatConflict, Name: "Claude (yolo)", Mode: ModeTerminal, OptIn: true, Command: claudeConflictYoloCommand},
				{Category: CatConflictComplete, Name: "Claude — resolve & complete (yolo)", Mode: ModeTerminal, OptIn: true, Command: claudeCompleteCommand},
				{Category: CatCommitMessage, Name: "Claude", Mode: ModeCapture, Command: claudeCommitCommand},
				{Category: CatReview, Name: "Claude", Mode: ModeCapture, Command: claudeReviewCommand},
			},
		},
		{
			// Empirical note, dated 2026-07-05: the spec's original
			// --merge/--rebase templates do not exist on the real,
			// installed standalone CLI (Junie 26.6.8 (1892.26)) despite web
			// docs suggesting them — `junie --help` lists no --merge/--rebase
			// flags at all, only --task/--prompt/--plan/--session-id
			// /--resume/--brave/--review. A live run of
			// `junie --merge <ref>` failed immediately: exit 1,
			// "Junie failed with the message: Failed to build
			// 'issue.md.junie_standalone'". Per the spec's pre-authorized
			// fallback ("The verification outcome decides which text ships
			// in Builtins()"), both conflict templates below use --prompt
			// instead — `junie --help`: "--prompt=<text>  Start interactive
			// mode with an initial prompt already submitted", which fits
			// gg's terminal-handover model exactly (Junie runs interactively,
			// with the conflict prompt pre-submitted, in the real terminal).
			ID: "junie", Label: "JetBrains Junie", Bins: []string{"junie"},
			Commands: []CommandTemplate{
				{Category: CatConflict, Name: "Junie", Mode: ModeTerminal, Command: junieConflictCommand},
				{Category: CatConflict, Name: "Junie (yolo)", Mode: ModeTerminal, OptIn: true, Command: junieConflictYoloCommand},
				{Category: CatConflictComplete, Name: "Junie — resolve & complete (yolo)", Mode: ModeTerminal, OptIn: true, Command: junieCompleteCommand},
				{Category: CatCommitMessage, Name: "Junie", Mode: ModeCapture, Command: junieCommitCommand},
				{Category: CatReview, Name: "Junie", Mode: ModeCapture, Command: junieReviewCommand},
			},
		},
		{
			ID: "codex", Label: "OpenAI Codex", Bins: []string{"codex"},
			Commands: []CommandTemplate{
				{Category: CatConflict, Name: "Codex", Mode: ModeTerminal, Command: codexConflictCommand},
				{Category: CatConflict, Name: "Codex (yolo)", Mode: ModeTerminal, OptIn: true, Command: codexConflictYoloCommand},
				{Category: CatConflictComplete, Name: "Codex — resolve & complete (yolo)", Mode: ModeTerminal, OptIn: true, Command: codexCompleteCommand},
				{Category: CatCommitMessage, Name: "Codex", Mode: ModeCapture, Command: codexCommitCommand},
				{Category: CatReview, Name: "Codex", Mode: ModeCapture, Command: codexReviewCommand},
			},
		},
		{
			ID: "antigravity", Label: "Antigravity", Bins: []string{"agy"},
			Commands: []CommandTemplate{
				{Category: CatConflict, Name: "Antigravity", Mode: ModeTerminal, Command: agyConflictCommand},
				{Category: CatConflict, Name: "Antigravity (yolo)", Mode: ModeTerminal, OptIn: true, Command: agyConflictYoloCommand},
				{Category: CatConflictComplete, Name: "Antigravity — resolve & complete (yolo)", Mode: ModeTerminal, OptIn: true, Command: agyCompleteCommand},
				{Category: CatCommitMessage, Name: "Antigravity", Mode: ModeCapture, OptIn: true, Command: agyCommitCommand},
				{Category: CatReview, Name: "Antigravity", Mode: ModeCapture, OptIn: true, Command: agyReviewCommand},
			},
		},
		{
			// ExtraProbes covers the standard installer's location: kimi's
			// PATH entry lives in a shell rc file, so a gg launched another
			// way (desktop entry, another shell) would otherwise miss it.
			ID: "kimi", Label: "Kimi Code", Bins: []string{"kimi"},
			ExtraProbes: []string{"~/.kimi-code/bin/kimi"},
			Commands: []CommandTemplate{
				{Category: CatConflict, Name: "Kimi", Mode: ModeCapture, Command: kimiConflictCommand},
				{Category: CatConflictComplete, Name: "Kimi — resolve & complete", Mode: ModeCapture, Command: kimiCompleteCommand},
				{Category: CatCommitMessage, Name: "Kimi", Mode: ModeCapture, Command: kimiCommitCommand},
				{Category: CatReview, Name: "Kimi", Mode: ModeCapture, Command: kimiReviewCommand},
			},
		},
		{
			ID: "meld", Label: "Meld", Bins: []string{"meld"},
			ExtraProbes: []string{`C:\Program Files\Meld\Meld.exe`},
			Commands: []CommandTemplate{
				{Category: CatConflict, Name: "Meld", Mode: ModeTerminal, PerFile: true,
					Command: "<bin> --auto-merge --output=<merged> <local> <base> <remote>"},
			},
		},
	}
}

// Detection is one detected tool. Bin is argv-ready: the bare binary name for
// a PATH hit (portable config), the absolute path for an ExtraProbes hit.
type Detection struct {
	Tool Tool
	Bin  string
}

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

// envTokRe matches a generation-time <env:NAME> token in a catalog template.
// NAME follows shell env-var naming: an uppercase letter/underscore start,
// then uppercase letters/digits/underscores.
var envTokRe = regexp.MustCompile(`<env:([A-Z_][A-Z0-9_]*)>`)

// GenerateCommand materializes a template for a detected binary, for the
// running OS. See GenerateCommandFor for what "materialize" means.
func GenerateCommand(tmpl CommandTemplate, bin string) string {
	return GenerateCommandFor(tmpl, bin, runtime.GOOS)
}

// GenerateCommandFor is GenerateCommand with the OS as a parameter (a test
// seam for exercising both renderings from one process). <bin> is replaced
// with bin, double-quoted when it contains whitespace (a Windows install
// path). Every <env:NAME> generation token becomes a per-OS reference to the
// GG_* environment variable gg always sets when it runs the command —
// `${NAME}` on POSIX, `%NAME%` on Windows — so one catalog template
// generates a correct command on either platform without gg ever
// substituting the underlying value (and needing to escape it) itself. The
// POSIX rendering is deliberately unquoted `${NAME}`, not `"$NAME"`: it
// nests inside a template's own double-quoted prompt strings as one word,
// where `"$NAME"` would alternate quotes and word-split the value when it
// contains spaces (e.g. a TMPDIR with a space); shell variable expansion is
// never re-parsed for command substitution, so the expanded value remains
// data either way. Runtime tokens (<op>, <source>, quartet paths,
// <context-file>, ...) pass through untouched for template.ResolveCommand.
func GenerateCommandFor(tmpl CommandTemplate, bin, goos string) string {
	if strings.ContainsAny(bin, " \t") {
		bin = `"` + bin + `"`
	}
	out := strings.ReplaceAll(tmpl.Command, "<bin>", bin)
	out = envTokRe.ReplaceAllStringFunc(out, func(tok string) string {
		name := envTokRe.FindStringSubmatch(tok)[1]
		if goos == "windows" {
			return "%" + name + "%"
		}
		return "${" + name + "}"
	})
	if goos == "windows" {
		// The catalog templates are written multi-line for readability, which
		// a POSIX shell accepts (a quoted string spans lines; a trailing \
		// continues one) but cmd.exe cannot run at all — it would launch the
		// tool from the first line with no flags. Materialize the single-line
		// form so the config says what will actually run: it is shown for
		// approval before its first run, and gg must not execute text the
		// user did not see.
		out = template.FlattenForCmd(out)
	}
	return out
}
