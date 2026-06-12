# Agent Init (`gg init`) — Design Spec

**Date:** 2026-06-12
**Status:** Approved
**Scope:** Teach AI coding agents to use gg: an embedded "using-gg" skill,
installed into detected agents' instruction locations via `gg init` (CLI) and a
new Settings popup (TUI). Both surfaces drive one shared engine.

## Goal

A gg user (or an agent itself) runs one command; gg detects which AI agents are
present (Claude Code, Junie, Codex, OpenCode, …), asks which to set up, and
installs a skill that teaches the agent to drive git through the gg CLI —
content that ships inside the gg binary and can only change when the binary
does.

## Principles

- **Single source of truth, compiled in.** The skill content lives at
  `internal/agentskill/using-gg.md` and is embedded with `go:embed`. The binary
  needs no external files; installed copies are derived artifacts. The content
  carries an integer **version marker**; installed copies update only when a
  newer binary's init runs (same version → no-op).
- **Agents are defined in the codebase only.** The detection registry is a
  hardcoded slice in `internal/agentinit`. Supporting a new agent = one
  registry entry + rebuild. No runtime-defined agents, no agents config file.
- **One engine, two front doors.** `internal/agentinit` owns detect / status /
  install; the CLI command and the TUI Settings popup are thin presentations of
  the same functions (the `internal/worktree` / `internal/repos` precedent).

## 1. `internal/agentskill` — the embedded skill

```go
//go:embed using-gg.md
var body string

// Version is bumped whenever using-gg.md changes; installed copies carry it
// so init can tell new/outdated/up-to-date apart.
const Version = 1

func Body() string // canonical markdown body (no frontmatter, no markers)
```

`using-gg.md` content requirements (~400 words, agent-directed):

- **When to use:** prefer `gg` over raw git for the operations it covers —
  the smart ops carry safety rails (auto-stash, never-drop-stash, guards).
- **Command reference:** `status`, `commit -m [-a]`,
  `pull [--background] [--on-conflict rebase|merge|abort]`, `push`,
  `switch <branch>`, `stash [-m]`, `undo`, `worktree list|add|remove
  [--with-branch] [--force]`, `repo list|switch <query>`, `inspect`.
- **The non-interactive rule (the part agents must internalize):** decision
  forks are answered by policy flags; in a non-TTY context an unanswered
  decision **errors with the available options instead of hanging** — so
  always pass the relevant policy flag, and read stderr for the option list
  when exit ≠ 0.
- Exit-code semantics (0 ok / 1 failed / 2 usage), `--cwd-file` + shell-init
  cd-following, worktree branch/path templates from `.gg.toml`.
- A version line (rendered from the marker) so humans can see what's installed.

## 2. `internal/agentinit` — registry + engine

```go
type Mode int
const (
    WholeFile    Mode = iota // gg owns the target file entirely (safe to overwrite)
    ManagedBlock             // replace a marker-delimited block, preserve the rest
)

type Agent struct {
    ID     string // stable: "claude-project", "junie", ...
    Label  string // human: "Claude Code (project)"
    Detect string // existing path that marks the agent present; "~/" prefix = home
    Target string // file to install into; "~/" prefix = home
    Mode   Mode
}

func Builtins() []Agent

type Status int // StatusNew | StatusUpToDate | StatusOutdated

type Detection struct {
    Agent  Agent
    Target string // resolved absolute target path
    Status Status
}

// Detect returns the registry entries whose Detect path exists, with each
// target's install status (parsed from the version marker in the target file).
func Detect(projDir, homeDir string) []Detection

// Install writes the embedded skill into d.Target according to d.Agent.Mode,
// creating parent directories as needed. Idempotent.
func Install(d Detection) error
```

### Built-in registry (v1)

| ID | Detect | Target | Mode |
|----|--------|--------|------|
| `claude-project` | `.claude` | `.claude/skills/using-gg/SKILL.md` | WholeFile |
| `claude-global` | `~/.claude` | `~/.claude/skills/using-gg/SKILL.md` | WholeFile |
| `junie` | `.junie` | `.junie/skills/using-gg/SKILL.md` | WholeFile (Junie discovers Agent Skills from `<projectRoot>/.junie/skills/`, same SKILL.md format as Claude Code) |
| `junie-global` | `~/.junie` | `~/.junie/skills/using-gg/SKILL.md` | WholeFile (Junie CLI's user-scope skills; project-scope wins on name clash) |
| `codex` | `~/.codex` | `~/.codex/AGENTS.md` | ManagedBlock |
| `opencode` | `~/.config/opencode` | `~/.config/opencode/AGENTS.md` | ManagedBlock |
| `agents-md` | `AGENTS.md` | `AGENTS.md` | ManagedBlock |
| `cursor` | `.cursor` | `.cursor/rules/using-gg.mdc` | WholeFile |
| `gemini` | `GEMINI.md` | `GEMINI.md` | ManagedBlock |
| `copilot` | `.github` | `.github/copilot-instructions.md` | ManagedBlock |
| `windsurf` | `.windsurfrules` | `.windsurfrules` | ManagedBlock |

### Rendering per mode

- **WholeFile (Claude Code):** YAML frontmatter
  (`name: using-gg`, `description: Use when performing git operations …`) +
  a version comment line + the body. The whole file is gg-owned; install
  overwrites it. Cursor's `.mdc` gets the body with a version line (no YAML
  beyond what cursor rules use — plain markdown body is acceptable).
- **ManagedBlock (shared files):** the body wrapped in
  `<!-- gg:using-gg:v<N>:begin -->` … `<!-- gg:using-gg:end -->`. Install
  replaces an existing block (any version) in place, else appends the block
  (with a separating blank line); all surrounding content is preserved
  byte-for-byte. A missing target file is created containing just the block.

### Status detection

Read the target: no file or no marker/frontmatter version → `StatusNew`;
marker version < `agentskill.Version` → `StatusOutdated`; equal →
`StatusUpToDate`. (Greater-than also reports `StatusUpToDate` — an older
binary never downgrades a newer install.)

## 3. CLI — `gg init`

```
$ gg init
Detected agents:
  1. [ ] Claude Code (project)   .claude/skills/using-gg/SKILL.md   new
  2. [x] Claude Code (global)    ~/.claude/skills/using-gg/SKILL.md outdated v1→v2
  3. [x] Junie                   .junie/guidelines.md               up to date
Apply? [enter]=checked / a=all / numbers (e.g. 1,3) / [q]uit:
```

- **Checkbox semantics (the core rule):** a target that already has the skill
  installed (`up to date` or `outdated`) is **checked by default** — applying
  refreshes it. A `new` target is **unchecked** — installing into an agent for
  the first time is always explicit opt-in.
- Selection input on stdin: **empty/enter** applies the checked defaults
  (refreshes everything already installed; if nothing is installed yet, says
  so and exits 0 without writing); `a` = all; comma/space-separated numbers =
  exactly those targets; `q` = abort (exit 0, nothing written).
- Flags (scripting): `--all`; `--update` (the checked defaults — refresh every
  already-installed target; the post-upgrade verb); `--agents
  claude-project,junie` (by ID; unknown ID = error, exit 2); `--list` (print
  detections with checkbox state and exit 0).
- **Non-interactive** (stdin not a TTY) with no selection flag: print the list
  and exit 1 with a hint to use `--all`/`--agents` (the cliDecider philosophy —
  never hang, never guess).
- Nothing detected: say so, exit 0.
- After install: print one line per target (`✓ installed …` / `✓ refreshed …`).
- Pure file I/O — **no engine operation, no git, no Decider**. `gg init` works
  outside a git repository (and must not crash there); the registry-touch in
  `cli.Run` already tolerates non-repos.
- Wiring: `commands` map + dispatch in `cli.Run`; `cmd/gg/main.go` help string
  gains `init`.

## 4. TUI — Settings popup

Per the adding-tui-windows taxonomy this is a transient picker → popup
(pointer field), designed as a **generic settings menu** so future options have
a home.

- **`,`** (comma, unbound) opens the Settings popup: a short menu list; v1 has
  one entry — **"Set up agent skills (using-gg)"**.
- Choosing it (enter) swaps the popup to the **agent picker**: the same
  `Detect` list rendered with a checkbox per row (`[x]`/`[ ]`) and the status.
  **Same checkbox rule as the CLI:** already-installed targets (`up to date` /
  `outdated`) start **checked** — confirming updates them automatically; `new`
  targets start **unchecked** (first-time installs are explicit). `↑/↓` move,
  **space** toggles any row, **enter** applies the checked set, **esc** goes
  back to the menu (esc again closes), ctrl+c quits.
- Install runs synchronously (file writes are instant); the result lands in
  the status line (`installed 2, refreshed 1`); errors surface there too.
- Footer hint gains `[,] settings`.
- Standard popup contract: pointer field on Model, swallows all keys, rendered
  via `overlayCenter`, routing precedence modal → existing popups → settings
  popup → filter → normal keys.

## 5. The update convention (definition of done for future features)

Whenever a feature changes the CLI surface (commands, flags, decision IDs):

1. update `internal/agentskill/using-gg.md`,
2. bump `agentskill.Version`,
3. re-run `gg init` (or the settings popup) wherever the skill is installed.

Recorded in: repo `CLAUDE.md` (docs-update rule), the `adding-features`
project skill (new checklist row), and the assistant's persistent memory.

## 6. Dogfooding

After the feature lands: run `gg init` in this repo, select
`claude-project`, and **commit** the generated
`.claude/skills/using-gg/SKILL.md` — the assistant then uses the gg CLI per
that skill for git operations gg covers.

## 7. Files touched (expected shape)

| File | Change |
|------|--------|
| `internal/agentskill/{agentskill.go, using-gg.md}` (new) | Embedded body + Version + rendering helpers (frontmatter / block wrap). |
| `internal/agentinit/agentinit.go` (new) | Registry, Detect, Install, status parsing. |
| `internal/cli/init.go` (new) | `cmdInit` (list/pick/install + flags). |
| `internal/cli/cli.go` | Registration + dispatch (init needs no repo). |
| `cmd/gg/main.go` | Help string. |
| `internal/tui/settings_popup.go` (new) | Two-level popup (menu → agent picker). |
| `internal/tui/model.go`, `view.go` | `,` key, routing, overlay, footer. |
| `.claude/skills/adding-features/SKILL.md` | New checklist row (the convention). |
| Docs | `CHANGELOG.md`, `README.md`, `CLAUDE.md` (package map + convention). |

## 8. Testing

- **agentskill:** body non-empty, contains the command names and the
  non-interactive rule; version renders into both wrappers.
- **agentinit (temp dirs):** Detect finds only existing paths (project +
  home variants via injected projDir/homeDir); status new/outdated/up-to-date
  (write older marker → outdated; same → up to date); WholeFile install
  creates parents + overwrites; ManagedBlock appends to existing content
  preserving it, replaces an old block in place (surrounding bytes identical),
  creates missing files; idempotent double-install.
- **CLI:** `--list` output (incl. checkbox state); `--all` installs into a
  fake project/home (HOME-injection: `Detect`/`cmdInit` take the home dir from
  a package seam so tests never touch the real `~`); `--update` refreshes
  exactly the already-installed targets and skips `new` ones; `--agents`
  selects by ID, unknown ID errors; **default-checked rule:** stdin empty-enter
  refreshes installed targets only (and is a clean no-op when nothing is
  installed); stdin "1" installs exactly that target; non-interactive without
  flags exits 1; works outside a git repo.
- **TUI:** `,` opens the menu; enter on the entry shows the picker with
  checkboxes — installed targets pre-checked, `new` ones unchecked; space
  toggles; enter applies the checked set into temp targets (Model carries an
  injectable home/proj for agentinit, mirroring `statePath` hermeticity);
  esc back/close; key swallowing; overlay fit.

**Hermeticity:** like `statePath`, the agentinit home/project directories are
injectable; production wires real values (`os.UserHomeDir`, repo top-level /
cwd), tests inject temp dirs. No test may write the developer's real agent
configs.

## 9. Out of scope (YAGNI)

- Runtime/user-defined agent registrations (config files, `init add`).
- Auto-refresh of installed skills without an explicit init/settings action.
- Uninstall (`init remove`); per-agent content customization; localization.
- MCP-specific instructions (that's M3's MCP server feature).
