# CLAUDE.md — gigagit (`gg`)

Guidance for Claude Code (and humans) working in this repo.

## What this is

**gigagit** (`gg`) is a Go terminal git client aimed at very large monorepos
(~100GB, ~20GB head). It blends GitKraken's one-key smart operations with
lazygit's fast keyboard TUI, runs cross-platform (Linux/macOS/Windows), and
shells out to the system `git` binary rather than reimplementing git.

Module: `github.com/gigagit/gg` · Go 1.26.

## Architecture

A **frontend-agnostic core engine** drives three frontends over thin git verbs:

```
        TUI (Bubble Tea)   CLI (scriptable)   MCP (future)
                   \            |            /
                    \           |           /
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
modal) and CLI (a flag-driven policy) fall out of the same contract. The hero
feature is a worktree-aware **SmartPull** decision tree.

### Package map (`internal/`)

| Package      | Responsibility |
|--------------|----------------|
| `engine`     | Operations + the `Event`/`Decider`/`Result` contract. Smart ops: `SmartPull`, `SmartSwitch`, `SmartMerge`, `SmartRebase`, `Commit`, `Push`, `Stash`, `UndoLastCommit`, `CreateWorktree`, `RemoveWorktree`. Ops act on a `GitOps` interface (`*git.Repo` satisfies it). |
| `domain`     | Frontend-facing command + query layer: `Execute` runs an operation under its repo-gate reservation; `Snapshot`/`Status`/`Worktrees`/`ShowFile`/`CommitFiles`/`TopLevel`/`CurrentBranch`/`GitCommonDir` run reads under a Read reservation, parallel and singleflight-coalesced; `CommitFeed` is the paged commit-history read-model backing the Commits panel. Also hosts the `Differ` (plain/enhanced, plain/cached decorator over lazy byte-sources) serving commit diffs from the cache. Emits the op span. |
| `repogate`   | Per-repo reservation gate (Read/RefWrite/TreeWrite, writer-preferring FIFO, escalation), process-global registry keyed by git common dir. |
| `git`        | Thin git verbs on `*git.Repo` (status, branches, worktrees, sync, stash, …). One verb ≈ one git invocation. |
| `gitcmd`     | Fluent argv builder (`New("sub").Arg(...).ArgIf(cond, ...).ToArgv()`). |
| `gitexec`    | The `Runner` interface (`Run`/`Stream`), the real `ExecRunner`, and `FakeRunner` for tests. `LimitRunner` bounds concurrent git subprocesses. |
| `model`      | Shared plain data types (`Status`, `Branch`, `Worktree`, `Commit`, …). |
| `tui`        | Bubble Tea Elm-style UI (value-receiver `Model`, panels, modal Decider, async ops). |
| `cli`        | Scriptable command frontend; `cliDecider` answers forks from a flag policy or stdin. |
| `worktree`   | Shared worktree template resolution used by BOTH the TUI popup and the CLI. |
| `repos`      | Machine-local MRU registry of opened repositories (XDG state file) behind the repo switcher. |
| `agentskill` | The embedded "using-gg" skill (go:embed + version marker) that teaches AI agents the gg CLI. |
| `agentinit`  | Hardcoded agent registry + detect/status/install behind `gg init` and the TUI Settings popup. |
| `config`     | TOML config (`.gg.toml`), field-level overlay (defaults→global→repo), `<seq>` counters. |
| `template`   | Pure branch/path template resolver (`<parent-branch>`, `<repo>`, `<date:…>`, `<seq:…>`, `<user:…>`, …). |
| `textdiff`   | Pure line-alignment engine (Myers + guards) behind the side-by-side diff view; no git/TUI imports. An `Enhanced` option adds word-level intraline spans on changed rows. |
| `cache`      | Generic injected in-memory LRU cache factory (`Factory.Cache(name) Cache`, `GetOrLoad`/`Load[V]`); two-bound eviction (entry count + byte budget via `Sized`). Keys are caller-chosen hashes. First consumer: the commit-diff cache. |
| `clipboard`  | Pure OSC 52 clipboard-sequence builder + single-write `Copy`; no TUI/git deps. Used by the TUI `.` menu copy actions. |
| `shellinit`  | `gg shell-init [bash|zsh|fish]` wrappers (cd-on-switch via `--cwd-file`). |
| `observ`     | Observability: span ring buffer, tracing, redaction, panic dump. |
| `buildinfo`  | Version/commit injected via `-ldflags` at build. |
| `app`        | Wires layers into runnable surfaces (`inspect`, panic `DumpRepo`). |
| `e2e` (top-level) | Declarative e2e harness: scenarios/*.toml → real repo (+ HTTP git server) → in-process CLI runs → semantic state assertions. |

Entry point: `cmd/gg/main.go` — routes `shell-init`/`inspect`/CLI subcommands, else launches the TUI.

## Conventions

- **A git verb is one invocation.** Build argv with `gitcmd`, run via `r.Runner.Run`/`.Stream`. Don't shell out directly.
- **Operations never block on a human.** They `emit` events and `decide` via the `Decider`; the channel send selects on `ctx.Done`.
- **Frontends run operations via `domain.Execute`**, never by assembling
  `OpDeps` directly. Ops needing less than exclusive access declare
  `LockMode()` (see SmartPull's background ref-write); escalation happens
  only at boundaries with no partial state.
  Frontend reads likewise go through domain queries — `Snapshot` for the TUI
  startup load, `Status`/`Worktrees` for the CLI — not direct `internal/git`
  verb calls.
- **Decisions are option-lists only** (no free text mid-flight). Frontends map them: TUI modal, CLI policy/stdin, MCP `MapDecider`.
- **TUI `Model` is a value receiver** with pointer fields (`modal`, `popup`) for state that must persist across the value copy.
- **`internal/tui` and `internal/cli` never import `internal/git`** — they reach git through `internal/domain` (guarded by `internal/archtest`). `cmd/gg` and `internal/app` are the composition root and may construct concrete git types.
- **Tests use a real `git`** in a `t.TempDir()` (see `newRepo`/`newTestRepo` helpers) or the `FakeRunner` for argv assertions. Follow TDD.
- **`main` is the trunk** (not `master`, which is stale). Branch features off `main`; the human merges them.

## Build / test

```bash
go build ./cmd/gg            # or ./build.sh [linux|windows|all]
./test.sh                    # staged: vet+gofmt → unit tests → e2e last
./test.sh race               # the same with -race — run before merging
./test.sh unit | e2e         # one stage only (test.cmd mirrors on Windows)
```

## Development workflow

Features follow: **brainstorm → spec → plan → subagent-driven execution → human merges**
(superpowers skills). Specs live in `docs/superpowers/specs/`, plans in
`docs/superpowers/plans/`, one feature branch off `main` per feature, with a
final review before merge.

**Project skills** (in `.claude/skills/`): `adding-features` — the full
engine→TUI→CLI wiring checklist for a new operation/command;
`adding-tui-windows` — panel vs popup vs modal taxonomy and wiring;
`writing-e2e-scenarios` — schema + operation contracts for authoring
`e2e/scenarios/*.toml`. Use them whenever adding a feature, TUI surface, or
e2e scenario.

**After each completed stage/feature, update the project docs:**
`CHANGELOG.md` (always), `README.md` (if user-facing surface changed), this
`CLAUDE.md` (if the architecture/package map/conventions changed), and — when
the CLI surface changed — `internal/agentskill/using-gg.md` (bump
`agentskill.Version`, then `gg init --update` to refresh installed copies).

## Status

See `CHANGELOG.md` for what's shipped. Roadmap: workspace group sync (named
repo groups + parallel background-pull; needs concurrent-op decision routing,
shared with MCP), then M3 (MCP server + heavy ops: staging, interactive rebase,
conflict editor, diff, visual graph, sparse-checkout).
