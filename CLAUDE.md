# CLAUDE.md — gigagit (`gg`)

Guidance for Claude Code (and humans) working in this repo.

## What this is

**gigagit** (`gg`) is a Go terminal git client aimed at very large monorepos
(~100GB, ~20GB head). It blends GitKraken's one-key smart operations with
lazygit's fast keyboard TUI, runs cross-platform (Linux/macOS/Windows), and
shells out to the system `git` binary rather than reimplementing git.

Module: `github.com/homeend/gigagit` · Go 1.26.

## Workflow

**Always develop features in a worktree.** Create a dedicated worktree with a
feature branch (`gg branch create <feat/...>` + `gg worktree add --branch
<feat/...>`), commit the work there, then `gg merge` it back into `main` and
remove the worktree. Never build feature work up uncommitted (or commit it
directly) in the main checkout.

## Architecture

A **frontend-agnostic core engine** drives the frontends over thin git verbs:

```
   TUI (Bubble Tea)   CLI (scriptable)   MCP (stdio)   Web (loopback)
                \            |            /       /
                 \           |           /       /
                  internal/domain  ← commands run via Execute under a
                                     per-repo reservation (repogate)
                             |
                   internal/engine  ← operations emit Events,
                                       resolve forks via a Decider
                             |
     internal/git (verbs) → internal/gitcmd (argv builder)
                             → internal/gitexec (process Runner + Fake)
```

The keystone contract: an **`Operation`** (`Operation.Run(ctx, OpDeps) (Result, error)`)
streams a tagged-union **`Event`** (`Progress`/`GitLine`/`DecisionNeeded`/`Timing`/`Done`)
and resolves mid-flight forks through a **`Decider`** that only ever picks from an
**option list**. This is designed for the non-blocking MCP case, so the TUI (a
modal), CLI (a flag-driven policy), and web (a parking SSE modal) fall out of
the same contract. The hero feature is a worktree-aware **SmartPull** decision
tree.

**Deep per-package/per-feature detail lives in `docs/CLAUDE-details.md`**
(the archived long-form package map: every op's semantics, decision ids,
gotchas, and design rationale). Consult it before changing an existing
feature; keep THIS file's map to one line per package.

### Package map (`internal/`)

| Package      | Responsibility |
|--------------|----------------|
| `engine`     | Operations + the `Event`/`Decider`/`Result` contract. Smart ops (`SmartPull`, `SmartSwitch`, `SmartMerge`, `SmartRebase`), `Commit`, `Push`, `Stash`, worktree/patch/export/config ops, AI capture ops (`GenerateMessage`, `ReviewChanges`, `CompleteConflict`), branch-version snapshots. Ops act on a `GitOps` interface; seams: `HookRunner`, `CaptureRunner`. English event prose + localizable `Msg{Format,Args}` channel via `msg.go` helpers only. |
| `domain`     | Frontend-facing command + query layer: `Execute` under the repo-gate reservation; singleflight-coalesced reads (`Snapshot`/`Status`/`CommitFeed`/…); the cached `Differ`; conflict/paused-op detection; review + conflict-complete report wrappers; branch-version and repo-health queries. |
| `repogate`   | Per-repo reservation gate (Read/RefWrite/TreeWrite, writer-preferring FIFO, escalation), process-global registry keyed by git common dir. |
| `git`        | Thin git verbs on `*git.Repo` — one verb ≈ one git invocation. Worktree file I/O rejects paths escaping the tree; stat-level probes (`PausedOpIn`, `LockFiles`) avoid git invocations; version-ref naming/parsing shared with engine/domain. |
| `rebaseplan` | Pure interactive-rebase plan model + todo/message rendering. Quoting targets **git's own POSIX sh** (even on Windows): single-quote dance + backslash→slash conversion. |
| `gitcmd`     | Fluent argv builder (`New("sub").Arg(...).ArgIf(cond, ...).ToArgv()`). |
| `gitexec`    | `Runner` interface (`Run`/`Stream`), real `ExecRunner`, `FakeRunner` for tests. Cancellation sends SIGTERM (not SIGKILL) so git releases its lockfiles; `WaitDelay` bounds the grace; `LimitRunner` caps concurrent git subprocesses. |
| `gitwatch`   | Pure fsnotify wrapper + `.git`-layout path→source map + debounced `Watcher`; backs event-driven auto-refresh. No git/TUI/domain imports. |
| `gitconfdocs`| Pure curated git-config catalog (~64 keys with defaults/kinds) behind the config explorer; staleness-tested against `git help -c`. DAG leaf. |
| `i18n`       | TUI translation layer: English-text-as-key TOML bundles (embedded ja/ko/zh/ru + user overlays). AST-gate tests in `internal/tui` enforce literal keys, full four-bundle coverage, and verb agreement. Engine/CLI prose and decision option VALUES stay English (agent-facing protocol); only rendering is localized. |
| `model`      | Shared plain data types (`Status`, `Branch`, `Worktree`, `Commit`, `FileAddress`, `Endpoint`, `GitLock`, …). |
| `tui`        | Bubble Tea Elm-style UI (value-receiver `Model`, panels, layer stack, modal Decider, async ops). Per-source refresh registry + background auto-refresh lane + file-watch; command palette; popups embed `popupMax` for ctrl+t maximize. New ops must be mapped in `opAffectedSources`. |
| `cli`        | Scriptable frontend; `cliDecider` answers forks from flags or stdin. Agent-facing terse verbs (`log`/`diff`/`show`/`add`/…), `gg batch`, `gg review`, `gg apply`, `gg versions`, `gg unlock`, `gg compare`. |
| `mcp`        | MCP stdio frontend (`gg mcp`): read surface (UI state, bookmarks/shelves, compare, export) + gated mutations (cherry-pick, write-to-worktree). Domain-only frontend. |
| `web`        | Loopback-only browser frontend (`gg web`): embedded SPA over domain queries + an op transport (SSE events, parking web Decider), AI review/conflict lanes. Domain-only frontend; loopback + Host/Origin guards, allowlist resolution for wire values. |
| `worktree`   | Shared worktree template resolution used by the TUI popup and the CLI. |
| `repos`      | Machine-local MRU registry of opened repositories (XDG state) behind the repo switcher. |
| `agentskill` | Embedded "using-gg" skill (go:embed + version marker) that teaches AI agents the gg CLI. |
| `agentinit`  | Hardcoded agent registry + detect/status/install behind `gg init` and the TUI Settings popup. |
| `exttool`    | Catalog of external tools/AI agents gg can run per task category (`conflict`, `commit_message`, `review`, `conflict_complete`); template generation + detection; `$GG_MESSAGE_FILE`-wins-over-stdout capture contract. |
| `config`     | TOML config (`.gg.toml`), field-level overlay (defaults→global→repo), `<seq>` counters, `[[tools.command]]` blocks, scoped line-edit writers. Repo config may live committed or machine-private (one active file). |
| `template`   | Pure token resolver for branch/path templates and external-tool commands (per-token-kind quoting; validation makes bad templates inert); `FlattenForCmd` cmd.exe repair; shared conflict-context doc rendering. |
| `textdiff`   | Pure line-alignment engine (Myers + guards) behind the side-by-side diff; optional word-level intraline spans. |
| `commitgraph`| Pure single-line commit-graph lane engine; no git/TUI/lipgloss imports. |
| `cache`      | Generic injected in-memory LRU cache factory (entry-count + byte budget); first consumer is the commit-diff cache. |
| `fsprobe`    | Pure per-OS probe classifying paths on slow "foreign" filesystem mounts (9p/WSL drvfs, cifs/smb, nfs, fuse; UNC on Windows) behind the repo-switcher slow-fs warning; fail-open, callers probe off-thread. DAG leaf. |
| `clipboard`  | System-clipboard writer: native OS command first (WSL-interop-gated `clip.exe`, Wayland-socket-resolved `wl-copy`, …), OSC 52 fallback; `Probe()` backs the clipboard notices. |
| `shelf`      | Non-git per-file/per-commit content store (blobs + TOML index under XDG state); shelved commits keep a tar + best-effort format-patch snapshot. Owned by `domain`; frontends never import it. |
| `bookmark`   | Persistent registry of richly-addressed file/commit references (records only, no blobs). Owned by `domain`. |
| `profile`    | Named git-identity presets, global + per-repo scoped. Owned by `domain`. |
| `promptstate`| Machine-local UX memory: suppressed prompts, dismissed notices, approved external-tool command hashes (`CommandHash` shared by TUI and web). |
| `prefix`     | Templated branch-name prefix registry, global + per-repo scoped. Owned by `domain`. |
| `shellinit`  | `gg shell-init [bash|zsh|fish]` wrappers (cd-on-switch via `--cwd-file`). |
| `observ`     | Observability: span ring buffer + sink (operation log), redaction, panic dump, session failure ring + `errors.log`. |
| `buildinfo`  | Version/commit via `-ldflags`, falling back to `runtime/debug.ReadBuildInfo`. |
| `app`        | Wires layers into runnable surfaces (`inspect`, panic `DumpRepo`). |
| `e2e` (top-level) | Declarative e2e harness: `scenarios/*.toml` → real repo → in-process CLI runs → semantic state assertions. |

Entry point: `cmd/gg/main.go` — routes `shell-init`/`inspect`/CLI subcommands, else launches the TUI.

## Conventions

- **A git verb is one invocation.** Build argv with `gitcmd`, run via `r.Runner.Run`/`.Stream`. Don't shell out directly.
- **Operations never block on a human.** They `emit` events and `decide` via the `Decider`; the channel send selects on `ctx.Done`.
- **Frontends run operations via `domain.Execute`**, never by assembling
  `OpDeps` directly. Ops needing less than exclusive access declare
  `LockMode()`; escalation happens only at boundaries with no partial state.
  Frontend reads likewise go through domain queries, not direct `internal/git`
  verb calls.
- **Decisions are option-lists only** (no free text mid-flight). Frontends map
  them: TUI modal, CLI policy/stdin, MCP `MapDecider`, web parking modal.
  Include an explicit `abort` option when cancel must be expressible (the TUI
  maps esc to it).
- **TUI `Model` is a value receiver** with pointer fields (`modal`, `popup`) for state that must persist across the value copy.
- **`internal/tui` and `internal/cli` never import `internal/git`** — they reach git through `internal/domain` (guarded by `internal/archtest`; `mcp` and `web` are domain-only too). `cmd/gg` and `internal/app` are the composition root and may construct concrete git types.
- **Every user-visible TUI string is translated**: route it through `i18n.T`
  with a literal key present in all four bundles — AST-gate tests
  (`i18n_scan_test.go`, `options_vocab_test.go`, `menu_labels_test.go`,
  `engine_prose_test.go`) fail otherwise. Engine/CLI prose stays English.
- **Tests use a real `git`** in a `t.TempDir()` (see `newRepo`/`newTestRepo` helpers) or the `FakeRunner` for argv assertions. Follow TDD.
- **`main` is the trunk** (not `master`, which is stale). Branch features off `main`; the human merges them.

## Build / test

```bash
go build ./cmd/gg            # or ./build.sh [linux|windows|all]
./build.sh install           # into GOBIN, version-stamped (safe while a gg mcp server runs)
./test.sh                    # staged: vet+gofmt → unit tests → e2e last
./test.sh race               # the same with -race — run before merging
./test.sh unit | e2e         # one stage only (test.cmd mirrors on Windows)
```

To read the TUI headlessly (no terminal/VM), `./tui-capture.sh "<keyscript>"`
drives the real `gg` under a tmux PTY and writes a plain-text snapshot per
screen (see the `driving-tui-headless` skill). `gg --record <file>` dumps a
live session's keystrokes in the same keyscript format for replay.

## Development workflow

Features follow: **brainstorm → spec → plan → subagent-driven execution → human merges**
(superpowers skills). Specs live in `docs/superpowers/specs/`, plans in
`docs/superpowers/plans/`, one feature branch off `main` per feature, with a
final review before merge.

**Project skills** (in `.claude/skills/`): `adding-features` (engine→TUI→CLI
wiring checklist), `adding-tui-windows`, `writing-e2e-scenarios`,
`adding-external-tools`, `defining-agentic-tasks`, `adding-translations`,
`adding-config-entries`, `adding-notifications`,
`adding-related-option-prompts`, `updating-git-config-options`,
`debugging-clipboard-copy`, `handling-git-locks`, `driving-tui-headless`,
`using-gg`. Use the matching skill whenever adding a feature, TUI surface,
e2e scenario, external tool, config entry, notification, or agentic task —
or when triaging a copy/paste or git-lock failure.

**After each completed stage/feature, update the project docs:**
`CHANGELOG.md` (always), `README.md` (if user-facing surface changed), and —
when the CLI surface changed — `internal/agentskill/using-gg.md` (bump
`agentskill.Version`, then `gg init --update`). Update THIS file only when
the architecture, package responsibilities, or conventions changed — and keep
map rows to one line: per-feature detail, decision ids, and gotchas go to
`docs/CLAUDE-details.md` (or CHANGELOG/memory), never into this file.

## Status

See `CHANGELOG.md` for what's shipped. All four frontends are live: TUI, CLI,
MCP (read surface + gated mutations), and the web probe (`gg web`, grown
through several waves). Roadmap: workspace group sync (named repo groups +
parallel background-pull; needs concurrent-op decision routing, shared with
MCP), then the remaining MCP surface — heavy ops (staging, interactive
rebase, conflict editor, diff, visual graph, sparse-checkout).
