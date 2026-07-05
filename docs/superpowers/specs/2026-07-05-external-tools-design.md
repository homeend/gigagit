# External tools & AI agents — design

Date: 2026-07-05 · Status: approved design (stage 1 detailed; stages 2–3 sketched)
Research: `docs/superpowers/research/2026-07-05-external-tools-and-agents.md`

## Goal

Let the user run external tools from gg for three task categories, each with
user-definable commands plus auto-detected defaults for known tools:

| Category (config value) | Known tools with defaults |
|---|---|
| `conflict` — resolve merge/rebase/cherry-pick/revert conflicts | Claude Code, Junie, Meld |
| `commit_message` — generate a commit message (stage 2) | Claude Code, Junie |
| `review` — review a branch / commit range (stage 3) | Claude Code, Junie |

The context-provisioning insight (from research): agentic CLIs need cwd +
a precise task statement rendered from state gg already has — not piped
diffs. Classic mergetools (Meld) need the per-file `LOCAL/BASE/REMOTE/MERGED`
quartet. Both are covered by one command-template mechanism.

## Staging

- **Stage 1 (this spec, in full): shared infrastructure + `conflict`.**
  Catalog + detection, `[tools]` config + writers, detect wizard,
  placeholder/env resolution, terminal-handover execution lane, first-run
  approval, conflict-window tool picker.
- **Stage 2: `commit_message`.** Adds the capture lane (headless run →
  captured stdout → commit-popup fields).
- **Stage 3: `review`.** Capture lane → report file → existing read-only
  external viewer; report lands in a temp dir or a per-repo dir under the
  gg state dir (user's stated preference).

Stages 2–3 get their own spec/plan when they start; nothing in stage 1 may
block them (the `mode = "capture"` value and the full category enum exist
from day one; in stage 1 a `capture` block is treated like an invalid block —
inert, with a one-time session note naming the reason "not supported yet").

## User-facing behavior (stage 1)

### Detect wizard (Settings)

A new Settings menu row **"External tools…"**:

1. gg probes the built-in catalog for installed tools (PATH lookup +
   platform-specific extra paths, e.g. Meld's Windows install dir).
2. A checkbox picker lists one row per *detected tool × applicable
   category* (stage 1: conflict rows only; the picker is built
   category-generic so stages 2–3 add rows, not UI). Rows whose
   `(category, name)` already exist in config are shown checked and are
   skipped on apply — the wizard never overwrites a user-edited command.
3. Apply appends the catalog's default command blocks to the **global**
   config (`~/.config/gg/config.toml`) — tools are machine-local. A status
   line names the file written.

Manual definitions use the exact same config shape, in either the global
config or the repo `.gg.toml`.

### Running a tool on conflicts

In the conflict window (`x`), a new key **`t`** ("tools") opens a picker of
`category = "conflict"` commands. The picker, `<user:…>` fill, approval
preview, and mark-resolved offer are all **conflict-process-owned
sub-states** (the `hunkPicker` pattern) — the process preempts the layer
stack for keys, so pushed popup layers would be unreachable while it is
open. The `[t]` hint appears in the conflict window's own hint line (and
the `?` help) only when at least one conflict command is configured:

- **Repo-level commands** (`per_file = false`, the agents): always listed
  while an op is paused. Commands with a `when_op` filter are listed only
  when the paused op matches.
- **Per-file commands** (`per_file = true`, Meld): listed when the focused
  file is a both-sides conflict.

Selecting a command:

1. Any `<user:LABEL>` tokens prompt for values (existing `templateFill`).
2. Placeholders resolve (shell-quoted); `GG_*` env vars are prepared.
3. **First-run approval**: a popup shows the fully resolved command; the
   user picks Run / Cancel. Approval is remembered per repo keyed by a
   hash of the resolved-command *template text* (not the per-run values),
   stored in `promptstate`. Any edit to the command text changes the hash
   and re-triggers approval.
4. The TUI suspends and the command runs with the terminal
   (`tea.ExecProcess`), cwd = the current worktree root, via
   `$SHELL -c <command>` (POSIX; default `/bin/sh`) or `cmd /C` (Windows).
5. On return, gg reloads status. For a per-file run whose `<merged>` file
   changed (mtime), an option-list decision offers **"Mark <file> as
   resolved?"** (→ existing `MarkResolved` action). Repo-level agent runs
   need nothing extra: agents are instructed to edit + `git add` only, and
   the existing resume-paused-op machinery ("all resolved, op still
   paused → Continue/Abort prompt") is the completion oracle.

A non-zero exit surfaces the failure in the conflict window's error state;
nothing is rolled back (the tool may have done useful partial work — the
status reload shows the truth).

## Config: the `[tools]` section

```toml
# One block per command; same shape generated or hand-written.
[[tools.command]]
category = "conflict"            # conflict | commit_message | review
name     = "Claude"              # menu label; unique per category
mode     = "terminal"            # terminal | capture (capture: stage 2+)
per_file = false                 # true = runs once per conflicted file
when_op  = ""                    # optional: merge|rebase|cherry-pick|revert
command  = '''claude ... "task text with <placeholders>"'''
```

- **Overlay rule (deliberate exception to zero-is-unset):** command lists
  **concatenate** global + repo; on a `(category, name)` collision the repo
  block wins. Documented in the config template comments.
- **Writer:** a new `config.AppendToolCommands(path, blocks)` appends
  `[[tools.command]]` blocks (multi-line `'''…'''` command values reuse the
  `SetWorktreePostCreateHook` delimiter handling); it creates the file if
  missing. It never modifies existing blocks.
- **settingDocs:** the `[tools]` section is registered with a comment-only
  worked example (no scalar keys to default). The `Config.Tools` field gets
  whatever registry/test accommodation `TestSettingDocsCoverAllFields`
  needs (an explicit exemption with a comment is acceptable — the section
  is list-shaped, not scalar-keyed).
- **Validation at load:** unknown `category`/`mode`, an empty `name` or
  `command`, `per_file` outside `conflict`, or an unknown placeholder token
  make the block *inert* — skipped with a one-time session failure note
  (`observ.NoteFailure`), never a startup error.

## The catalog: `internal/exttool`

A new leaf package (archtest: importable by tui/cli like `agentinit`;
imports nothing above `model`):

```go
type Category string // conflict | commit_message | review
type Mode string     // terminal | capture

type CommandTemplate struct {
    Category Category
    Name     string // e.g. "Claude", "Junie (merge)"
    Mode     Mode
    PerFile  bool
    WhenOp   string // "" = any paused op
    Command  string // template text with <tokens>
}

type Tool struct {
    ID          string   // "claude", "junie", "meld"
    Label       string
    Bins        []string // candidate binary names for LookPath
    ExtraProbes []string // absolute-path probes (Meld on Windows)
    Commands    []CommandTemplate
}

func Builtins() []Tool
type Detection struct { Tool Tool; Path string }
func Detect(look func(string) (string, error), stat func(string) (os.FileInfo, error)) []Detection
```

Detection is `exec.LookPath` over `Bins` plus `os.Stat` over `ExtraProbes`,
both injected (the `internal/clipboard/native.go` seam pattern) for tests.
Supporting a new tool is one `Builtins` entry — the `agentinit` philosophy.

### Stage-1 default commands (catalog contents)

**Claude Code** — repo-level, `terminal` (values via env expansion + the
context file; no raw prose substitution):

```
claude --permission-mode acceptEdits \
  --allowedTools "Read" "Edit" "Bash(git status)" "Bash(git diff *)" "Bash(git log *)" "Bash(git add *)" \
  --disallowedTools "Bash(git commit *)" "Bash(git merge *)" "Bash(git rebase *)" "Bash(git push *)" \
  "A git <env:GG_OP> operation is paused with conflicts in this repository.
   Read the context file at <env:GG_CONTEXT_FILE> for the operation's parties and the conflicted paths.
   Inspect both sides' history to understand intent, resolve each conflict by editing the files,
   then run git add on each resolved file. Do NOT run git commit or any --continue command --
   stop when everything is staged and summarize what you chose and why."
```

**Junie** — two repo-level `terminal` entries with op filters:

- `Junie (merge)`, `when_op = "merge"`: `junie --merge <env:GG_SOURCE>`
- `Junie (rebase)`, `when_op = "rebase"`: `junie --rebase <env:GG_SOURCE>`

Empirical note (research §8): whether `--merge`/`--rebase` correctly adopt
an *already-paused* op must be verified against a live Junie during
implementation; the fallback default is
`junie --prompt "Resolve the conflicts of the paused git <env:GG_OP> in this repository (see the context file at <env:GG_CONTEXT_FILE>). Edit and git add the resolved files; do not run git commit or --continue."`.
The verification outcome decides which text ships in `Builtins()`.

**Meld** — `per_file = true`, `terminal`:

```
meld --auto-merge --output=<merged> <local> <base> <remote>
```

### Stage 2–3 defaults (recorded here, shipped later)

- `commit_message` / Claude (`capture`):
  `claude -p --permission-mode dontAsk --allowedTools "Bash(git diff *)" "Bash(git log *)" "Bash(git status *)" --output-format json --json-schema '{"type":"object","properties":{"subject":{"type":"string"},"body":{"type":"string"}},"required":["subject"]}' "Write a commit message for the staged changes. Match the style of recent commits."`
- `commit_message` / Junie (`capture`):
  `junie --output-format json --json-output-file <out> --task "Write a concise commit message for the currently staged changes. Do not run git commit or modify files - output only the message."`
- `review` / Claude (`capture`): `claude -p "/code-review <range>" --output-format json`
- `review` / Junie (`terminal`): `junie --review "Review the changes in <range>"`

## Placeholders and environment

A command-context resolver in `internal/template` (new entry point reusing
`tokenRe`/`UserLabels`). Quoting is **per token kind**: path-valued tokens
(`<repo>`, `<file>`, `<local>`, `<base>`, `<remote>`, `<merged>`,
`<context-file>`) are shell-quoted on substitution (they sit in argv
positions and may contain spaces); prose-valued tokens (`<op>`, `<source>`,
`<target>`, `<conflicted-files>`, `<user:…>`) substitute **literally** —
the resolver never escapes or rewrites their content. The existing
`<branch>` path-sanitization route is wrong for shell arguments and is not
used.

**Injection posture (user decision, 2026-07-05):** substituted values are
not escaped; instead, dynamic content reaches commands through two safe
channels that need no escaping at all:

1. **The context file** — every conflict-tool run writes a temp file
   (op, source, target, and the conflicted paths one-per-line, byte-exact —
   a backtick or `$` in a file name is just a character in a file) and
   exposes it as `<context-file>` (shell-quoted path token) and
   `GG_CONTEXT_FILE`. This also scales to long context. Cleaned up after
   the run like the quartet temps.
2. **`GG_*` env vars** — shell expansion of a quoted variable is data,
   never code.

The shipped **default templates use only these two channels plus enum/path
tokens** — no default substitutes a raw prose value. The prose tokens
remain available for hand-written commands and are documented with the
caveat: their content is substituted verbatim into shell text, so use
`"$GG_*"`/`<context-file>` when values may contain shell metacharacters.

Two **generation-time-only** tokens exist in catalog templates and are
rejected by the runtime resolver with pointed errors: `<bin>` (replaced by
the detected binary — bare name from PATH, quoted absolute path from an
extra probe) and `<env:NAME>` (rendered per-OS at wizard time as `${NAME}`
on POSIX or `%NAME%` on Windows, so one catalog template generates a
correct command on either platform). The POSIX rendering is deliberately
`${NAME}` without quotes: it nests inside a template's own double-quoted
prompt strings as one word (a `"$NAME"` rendering would alternate quotes
and word-split when the value contains spaces, e.g. a TMPDIR with a space),
and shell variable expansion is never re-parsed for command substitution,
so expanded values remain data.

| Token | Value (stage 1) |
|---|---|
| `<op>` | paused op: `merge` \| `rebase` \| `cherry-pick` \| `revert` |
| `<source>` / `<target>` | `domain.ConflictState.Source` / `.Target` (may be empty; agents recover via git) |
| `<conflicted-files>` | space-separated shell-quoted repo-relative paths |
| `<repo>` | current worktree root (absolute) |
| `<file>` | focused conflicted file (per-file commands) |
| `<local>` `<base>` `<remote>` | quartet temp files from index stages `:2:` `:1:` `:3:` (a missing stage yields an empty temp file, matching git-mergetool behavior); real file extension preserved; 0600 |
| `<merged>` | the conflicted file's real worktree path |
| `<user:LABEL>` | interactive fill (existing mechanism) |

Per-file tokens in a repo-level command (or vice-versa-invalid combinations)
make the block inert at load (validation above). The same values are always
injected as env — `GG_OP`, `GG_SOURCE`, `GG_TARGET`, `GG_CONFLICTED_FILES`,
`GG_REPO`, `GG_FILE`, `GG_LOCAL`, `GG_BASE`, `GG_REMOTE`, `GG_MERGED`,
`GG_CONTEXT_FILE` — so a wrapper script needs no placeholders at all
(post-create-hook precedent). The context file's format is line-oriented:
`op:`/`source:`/`target:` header lines, then `conflicted:` followed by one
repo-relative path per line. A path containing a control character
(newline/CR — legal in git paths) is written **C-quoted the way git itself
prints such paths** (`"innocent.go\nFAKE"`), so one entry can never forge
additional lines; every other path is byte-exact. Header values are safe
unquoted (git refnames forbid control characters). Context file and
quartet temp files are deleted after the run (best-effort).

## Execution plumbing

- **Terminal lane (stage 1's only lane):** built in `internal/tui` beside
  the editor precedent (`edit_actions.go`) — a pure-UI child-process launch
  is the sanctioned `os/exec` exception; no repo mutation happens through
  gg, so no engine op / repogate reservation is taken (same standing as
  `$EDITOR`). The quartet materialization is a **domain read query**
  (`domain.ConflictFileVersions(ctx, path) (local, base, remote string, err)`
  — creates the temp files under a Read reservation using a new
  `git.StageBlobBytes`-style verb over `git show :N:<path>`, reusing
  `internal/git/stage_blob.go` helpers where they fit).
- **Approval memory:** `promptstate` gains per-repo approved-command hashes
  (`ApproveToolCommand(repoKey, hash)` / `ToolCommandApproved`), stored in
  the existing `prompts.toml` (new table), atomic-rewrite as today.
- **Capture lane (stage 2+):** an engine operation behind a
  `HookRunner`-style seam (approval decision + env + line-streamed
  `GitLine` events + captured stdout), per the post-create-hook pattern.
  Out of stage-1 scope; noted so stage 1's types don't preclude it.

## Surfaces summary (stage 1)

| Surface | Change |
|---|---|
| Settings menu | "External tools…" → detect wizard picker |
| Conflict window | `t` → conflict-tool picker; footer binding + `?` help entry |
| Approval popup | resolved-command preview, Run/Cancel |
| Mark-resolved offer | option-list after a per-file run that changed `<merged>` |
| Config template | `[tools]` section with worked example (`gg config init`/`populate`) |

No CLI verbs in stage 1 (`gg tools detect/ls` may come with stage 2+ if
wanted; the MCP future favors having them eventually).

## Safety model

- Nothing executes that isn't in config or explicitly confirmed; catalog
  defaults reach config only through the wizard's apply step.
- First-run approval shows the exact resolved command; remembered per repo
  by template-text hash; any text change re-prompts.
- Per-tool rails live *inside* the generated defaults (Claude allowlists,
  "do not commit/continue" prompt clauses); gg never passes an
  approval-bypass flag it cannot scope.
- The sequencer boundary: external tools stage resolutions; **continuing
  the op stays in gg** (`engine.ContinueOp`), surfaced by the existing
  paused-op machinery.

## Testing

- `exttool`: detection with fake LookPath/Stat (per-platform cases incl.
  Meld's Windows probe); catalog invariants (unique names per category,
  every template's tokens parse, categories/modes valid).
- `template`: command-resolver golden tests — quoting (spaces, quotes in
  paths), empty source/target, unknown-token error, per-file token set.
- `config`: parse + concat-overlay (repo wins on collision) + inert-block
  validation + `AppendToolCommands` writer (fresh file, existing file,
  multi-line command, never touches existing blocks).
- `promptstate`: approve/re-ask on hash change, per-repo isolation, temp
  store injection (registry precedent: prompt ids are forever).
- `tui`: wizard picker rows/apply; conflict picker gating (per_file needs
  both-sides focus; `when_op` filter; key hidden with zero commands);
  approval flow; mark-resolved offer on mtime change — over `newTestRepo`
  fixtures with a real conflict; `tea.ExecProcess` intercepted at the
  `tea.Cmd` boundary.
- `domain`: `ConflictFileVersions` against a real conflicted repo
  (both-sides and add/add missing-base cases).
- e2e: one scenario asserting the wizard-equivalent config write
  (`gg config`-level, since e2e drives the CLI; the TUI picker is unit
  territory).
- Manual/live checklist before merge: Claude handover on a real conflict;
  Junie `--merge` paused-op semantics (decides the shipping default);
  Meld quartet round-trip.

## Documentation & follow-through

`CHANGELOG.md`, `README.md` (new user surface), `CLAUDE.md` package map
(`exttool` row + tui/config notes), config `settingDocs` example. The
using-gg agent skill is unaffected until a CLI verb ships.

## Out of scope (recorded)

- Capture lane, commit-popup injection (stage 2); review reports (stage 3).
- CLI verbs / MCP exposure.
- A data-driven (TOML) catalog — manual `[[tools.command]]` blocks already
  cover unknown tools; revisit only if catalog churn becomes real.
- Structural pre-resolution (mergiraf-style merge drivers) — orthogonal.
