# Changelog

All notable changes to gigagit (`gg`) are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
No tagged release has been cut yet; everything lives under **Unreleased**.

## [Unreleased]

### Added

#### Branch management
- Branch management: `b` (create-branch popup) / `B` (create and smart-switch)
  / `d` (delete with confirm + unmerged force fork) on the TUI Branches panel,
  and `gg branch create <name> [<start>]` / `gg branch delete [--force] <name>`
  in the CLI. The embedded using-gg agent skill is now v2.

#### Foundation & engine (M1)
- Frontend-agnostic core **engine**: the `Operation` contract with a streamed
  `Event` union (`Progress`/`GitLine`/`DecisionNeeded`/`Timing`/`Done`) and a
  `Decider` for mid-flight, option-list-only forks.
- Thin git verb layer (`internal/git`) over a `gitcmd` argv builder and a
  `gitexec` process runner (with a `FakeRunner` for tests).
- Smart operations: `SmartPull` (worktree-aware divergence decision tree),
  `SmartSwitch` (auto-stash/restore), `Commit`, `Push`, `Stash`, and a
  ref-only `UndoLastCommit`.
- Observability: span ring buffer, tracing, redaction, and a panic debug dump
  (`gg inspect`, `--debug-dump`, `--trace`).
- Interactive **TUI** (Bubble Tea): size-aware multi-panel layout, a modal
  Decider, and a panic-safe dump on crash.

#### CLI (M2)
- Scriptable commands: `gg status`, `commit`, `pull` (`--background`,
  `--on-conflict`), `push`, `switch`, `stash`, `undo`.
- Forks resolved by flags, an interactive stdin prompt, or a clear error when
  neither is available; unknown-subcommand guard.

#### Worktree management (M2)
- TOML config (`.gg.toml`) with field-level overlay and worktree branch/path
  **templates** (`<parent-branch>`, `<repo>`, `<date:…>`, `<seq:…>`, `<user:…>`);
  per-repo `<seq>` counters in `<git-common-dir>/gg/state.toml`.
- **Create**: TUI `w` create popup (live preview, editable name) and `W`
  create-and-switch; `gg worktree add [<start-point>]`; shared resolution in
  `internal/worktree`.
- **Switch**: `enter` on the Worktrees panel re-roots the TUI into the selected
  worktree; `gg shell-init [bash|zsh|fish]` follows the switch with a real `cd`
  via `--cwd-file`.
- **Delete**: TUI `d` on the Worktrees panel and
  `gg worktree remove [--with-branch] [--force] <path>`. Reactive-force model —
  choose worktree-only vs. worktree+branch up front, then force is offered only
  when git refuses (dirty tree or unmerged branch). Engine guards refuse
  removing the current or primary worktree.

#### TUI list UX
- `shift+tab` cycles panel focus backwards; `pgup`/`pgdn` move the selection by
  25% of the focused panel's viewport.
- `o` cycles a panel's sort order (`default → name ↑/↓ → date ↑/↓`) — each
  panel defines its own name/date semantics (branches: committer date;
  worktrees: HEAD commit date; status files: mtime; commits: commit time).
  Branches default to newest-first.
- `/` filters any list panel by case-insensitive substring (`enter` keeps the
  filter, `esc` clears it); selection, paging, and all action keys operate on
  the filtered, sorted view.

#### Repo switcher
- gg auto-records every repository it opens in a machine-local MRU registry
  (`~/.local/state/gg/repos.toml`); dead paths are pruned automatically.
- TUI: `R` opens a switcher popup — filter as you type, `enter` re-roots into
  the chosen repo (the shell follows via `--cwd-file`), `ctrl+d` forgets an
  entry.
- CLI: `gg repo list` and `gg repo switch <query>` (unique substring match
  prints the path and writes `--cwd-file` so a wrapped shell `cd`s there).

#### Agent init
- `gg init` detects installed AI coding agents (Claude Code, Junie, Codex,
  OpenCode, Cursor, AGENTS.md, …) and installs an embedded "using-gg" skill
  teaching them to drive git through the gg CLI. Already-installed targets are
  checked by default (applying refreshes them); new agents are explicit
  opt-in. `--all`, `--update`, `--agents <ids>`, `--list` for scripting.
- TUI: `,` opens a Settings popup with the same agent-skill picker.
- The skill ships inside the gg binary (version-marked); installed copies
  change only when a newer binary's init runs.

#### Developer tooling
- Project skills in `.claude/skills/`: `adding-features` (engine→TUI→CLI wiring
  checklist for new operations/commands) and `adding-tui-windows` (panel vs
  popup vs modal taxonomy and wiring), both validated against baseline runs.

[Unreleased]: https://github.com/gigagit/gg
