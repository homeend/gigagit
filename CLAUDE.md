# CLAUDE.md — gigagit (`gg`)

Guidance for Claude Code (and humans) working in this repo.

## What this is

**gigagit** (`gg`) is a Go terminal git client aimed at very large monorepos
(~100GB, ~20GB head). It blends GitKraken's one-key smart operations with
lazygit's fast keyboard TUI, runs cross-platform (Linux/macOS/Windows), and
shells out to the system `git` binary rather than reimplementing git.

Module: `github.com/homeend/gigagit` · Go 1.26.

## Workflow

**NEVER USE SUB AGENTS.** Do every task in the main session yourself — no
Agent/Task tool, no parallel implementer fan-out, no "dispatch to a
subagent" step. Plans are executed sequentially by the one session that
wrote them.

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
| `engine`     | Operations + the `Event`/`Decider`/`Result` contract. Smart ops (`SmartPull`, `SmartSwitch`, `SmartMerge`, `SmartRebase`), `Commit`, `Push`, `Stash`, worktree/patch/export/config ops, AI capture ops (`GenerateMessage`, `ReviewChanges`, `CompleteConflict`), branch-version snapshots, `ApplyMigration` (which runs the `MigrationAction` it is handed — `DiscardRefs` or a domain-supplied converter — and only then stamps the format marker). Ops act on a `GitOps` interface; seams: `HookRunner`, `CaptureRunner`. English event prose + localizable `Msg{Format,Args}` channel via `msg.go` helpers only. |
| `preflight`  | Pure feature-requirement registry + resolver: features declare data-format ranges, a superseded machine-local store (`LegacyStore`), a minimum git version, a usable forge CLI and a criticality (a `Silent` feature never raises a notice); a `Migration` names its action and says whether it is `Lossless` (the zero value asks for consent). `Resolve` maps probe results to Satisfied/Repairable/Unsatisfiable. DAG leaf; `domain` runs the probes and owns the migration op. |
| `domain`     | Frontend-facing command + query layer: `Execute` under the repo-gate reservation; singleflight-coalesced reads (`Snapshot`/`Status`/`CommitFeed`/…); the cached `Differ`; conflict/paused-op detection; review + conflict-complete report wrappers; branch-version and repo-health queries; once-per-session forge detection + read-only pull-request queries (`PullRequests`/`PRComments`/`PRFetchOp`/`PRPair`). Owns the **compare algebra**: `EvalEndpoint`/`EvalLink` turn an endpoint or a `gg://` link into a `FileSet` (bounded = a key set, unbounded = a whole tree) and `CompareSets` is total over the 2×2 — no frontend screens a pair. |
| `repogate`   | Per-repo reservation gate (Read/RefWrite/TreeWrite, writer-preferring FIFO, escalation), process-global registry keyed by git common dir. |
| `git`        | Thin git verbs on `*git.Repo` — one verb ≈ one git invocation. Worktree file I/O rejects paths escaping the tree; stat-level probes (`PausedOpIn`, `LockFiles`) avoid git invocations; version-ref naming/parsing shared with engine/domain. |
| `rebaseplan` | Pure interactive-rebase plan model + todo/message rendering. Quoting targets **git's own POSIX sh** (even on Windows): single-quote dance + backslash→slash conversion. |
| `gitcmd`     | Fluent argv builder (`New("sub").Arg(...).ArgIf(cond, ...).ToArgv()`). |
| `gitexec`    | `Runner` interface (`Run`/`Stream`), real `ExecRunner`, `FakeRunner` for tests. Cancellation sends SIGTERM (not SIGKILL) so git releases its lockfiles; `WaitDelay` bounds the grace; `LimitRunner` caps concurrent git subprocesses. |
| `gitwatch`   | Pure fsnotify wrapper + `.git`-layout path→source map + debounced `Watcher`; backs event-driven auto-refresh. No git/TUI/domain imports. |
| `linknav`    | The one `gg://` link → navigate builder (`Resolve`, `Command`, `HunkLine`, `AtLink`, `RepoOnly`) shared by the CLI (`gg open`, `gg session navigate`) and the TUI's `#` paste prompt; carries the `?<kind>=<id>` landing hint and treats a hint as a place, so a hint-only link navigates; domain + steer + model, never a frontend. |
| `linkhist`   | Per-repo 20-entry MRU of copied `gg://` links (records only, TOML + the shared file lock under XDG state); dedup-to-top on the link text. Owned by `domain`; frontends never import it. |
| `filelock`   | The one cross-process `O_EXCL` lock file (owner token, stale-lock breaker, Windows `ErrPermission` retry) behind every per-repo state file — `notes`, `preview` and `linkhist` share it. Stdlib-only DAG leaf. |
| `steer`      | Live-steering protocol leaf: a per-worktree file inbox (presence with mtime liveness, temp+rename command/reply files, an fsnotify wake) that `gg session` uses to drive a running TUI or `gg web` page. Targets name the working tree, the index, a commit, or a MERGE PREVIEW (a branch pair; the consumer resolves the tip). stdlib + fsnotify only; `tui`/`cli`/`web`/`mcp` import it directly. |
| `gitconfdocs`| Pure curated git-config catalog (~64 keys with defaults/kinds) behind the config explorer; staleness-tested against `git help -c`. DAG leaf. |
| `i18n`       | TUI translation layer: English-text-as-key TOML bundles (embedded ja/ko/zh/ru + user overlays). AST-gate tests in `internal/tui` enforce literal keys, full four-bundle coverage, and verb agreement. Engine/CLI prose and decision option VALUES stay English (agent-facing protocol); only rendering is localized. |
| `model`      | Shared plain data types (`Status`, `Branch`, `Worktree`, `Commit`, `FileAddress`, `Endpoint`, `GitLock`, …). Also the `gg://` `Link` grammar (`ParseLink`/`String`/`Address`; targets: a sha, `@staged`, `@ref:<name>`, `@<a>..<b>`, `<target>...<source>`, plus a trailing `?<kind>=<id>` hint — `?` is a separator, so no path, checkout path or refname may hold one). |
| `tui`        | Bubble Tea Elm-style UI (value-receiver `Model`, panels, layer stack, modal Decider, async ops). Per-source refresh registry + background auto-refresh lane + file-watch; command palette; popups embed `popupMax` for ctrl+t maximize. New ops must be mapped in `opAffectedSources`. |
| `cli`        | Scriptable frontend; `cliDecider` answers forks from flags or stdin. Agent-facing terse verbs (`log`/`diff`/`show`/`add`/…), `gg batch`, `gg review`, `gg apply`, `gg versions`, `gg unlock`, `gg compare` (incl. `--save`/`--saved`/`--list`/`--remove`/`--rename`), `gg note`, `gg links`, `gg pr` (read-only), `gg skill path`. |
| `mcp`        | MCP stdio frontend (`gg mcp`): read surface (UI state, bookmarks/shelves, compare, export, `gg://` link resolve/list/compare) + gated mutations (cherry-pick, write-to-worktree). Domain-only frontend. |
| `web`        | Loopback-only browser frontend (`gg web`): embedded SPA over domain queries + an op transport (SSE events, parking web Decider), AI review/conflict lanes. Domain-only frontend; loopback + Host/Origin guards, allowlist resolution for wire values. |
| `forge`      | Read-only forge (pull-request) seam: the `Provider` interface + the `gh` CLI implementation (JSON/GraphQL parsers, `RepoSlug`); PR/comment types live in `model`. Never mutates the forge. Owned by `domain`; frontends never import it. |
| `worktree`   | Shared worktree template resolution used by the TUI popup and the CLI. |
| `repos`      | Machine-local MRU registry of opened repositories (XDG state) behind the repo switcher. Entries carry the remote repository NAME (computed by the caller — this package stays a DAG leaf) so `domain.ResolveLink` can find a `gg://` link's checkout. |
| `agentskill` | Two embedded skills ("using-gg", "reviewing-with-gg") behind a Skill value type (go:embed + per-skill version marker) that teach AI agents the gg CLI and the review-notes lane. |
| `agentinit`  | Hardcoded agent registry + detect/status/install behind `gg init` and the TUI Settings popup. |
| `exttool`    | Catalog of external tools/AI agents gg can run per task category (`conflict`, `commit_message`, `review`, `conflict_complete`); template generation + detection; `$GG_MESSAGE_FILE`-wins-over-stdout capture contract. |
| `config`     | TOML config (`.gg.toml`), field-level overlay (defaults→global→repo), `<seq>` counters, `[[tools.command]]` and `[[branches.filter]]` blocks, scoped line-edit writers. Repo config may live committed or machine-private (one active file). |
| `template`   | Pure token resolver for branch/path templates and external-tool commands (per-token-kind quoting; validation makes bad templates inert); `FlattenForCmd` cmd.exe repair; shared conflict-context doc rendering. |
| `textdiff`   | Pure line-alignment engine (Myers + guards) behind the side-by-side diff; optional word-level intraline spans. |
| `syntax`     | Pure chroma wrapper: `Detect(path)` → lexer, `Lex(lang, src)` → per-source-line token runs (`Tok{Start,End,Class}`, rune offsets, coarse `Class` enum). DAG leaf; `domain.Differ` attaches runs to `Diff.OldTok/NewTok`. |
| `theme`      | Pure colour-role catalogue (`Terminal` zero-value = inherit, `Dark` Campbell, `Light` neutral grey/charcoal) behind `[ui] theme`, plus `Override`/`Overlay`/`Merge`/`RoleDocs` over one `roleFields` table for the config's `[themes.<name>]` per-role overrides; the TUI builds lipgloss styles from it (`styles.go`) and paints the frame (`paint.go`). DAG leaf. |
| `commitgraph`| Pure single-line commit-graph lane engine; no git/TUI/lipgloss imports. |
| `changeset`  | Pure before/after change-set comparison (`Compare`) behind drift detection: a status flip like M→A is a resurrection and alarms (`Added`), a path only in the before set is absorbed upstream and stays quiet (`Removed`). DAG leaf: stdlib only, no git. |
| `branchfilter`| Pure five-slot branch-filter model: `Slot` (TOML shape), `Compile` (RE2 + age parse, inert-with-reason), `Apply` (hide / show-only + exempt rows) — the ONE matcher the TUI and web share. DAG leaf. |
| `cache`      | Generic injected in-memory LRU cache factory (entry-count + byte budget); first consumer is the commit-diff cache. |
| `fsprobe`    | Pure per-OS probe classifying paths on slow "foreign" filesystem mounts (9p/WSL drvfs, cifs/smb, nfs, fuse; UNC on Windows) behind the repo-switcher slow-fs warning; fail-open, callers probe off-thread. DAG leaf. |
| `clipboard`  | System-clipboard writer: native OS command first (WSL-interop-gated `clip.exe`, Wayland-socket-resolved `wl-copy`, …), OSC 52 fallback; `Probe()` backs the clipboard notices. |
| `shelf`      | Non-git per-file/per-commit content store (blobs + TOML index under XDG state); shelved commits keep a tar + best-effort format-patch snapshot. Owned by `domain`; frontends never import it. |
| `bookmark`   | Persistent registry of richly-addressed file/commit references (records only, no blobs). Owned by `domain`. |
| `notes`      | Machine-local review-note store (TOML + O_EXCL lock, write-time cap, `Sweep`); records only. Owned by `domain`; frontends never import it. |
| `notebatch`  | Pure parser for the two agent JSON note-batch shapes (hunk agent-context v1, comment apply); shared by CLI, MCP and the review importer. DAG leaf. |
| `savedcompare`| Machine-local registry of saved comparisons: an entry is a PAIR of `gg://` links or a SET (one link — a saved merge preview). Records only, TOML + the shared file lock under XDG state; absorbed the former `preview` store, converting it losslessly on first run. Owned by `domain`; frontends never import it. |
| `profile`    | Named git-identity presets, global + per-repo scoped. Owned by `domain`. |
| `promptstate`| Machine-local UX memory: suppressed prompts, dismissed notices, approved external-tool command hashes (`CommandHash` shared by TUI and web), the active branch-filter slot per repo per list. |
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
- **Every terminal handover rides `handover()`** (`internal/tui/autowrap.go`), never a raw `tea.ExecProcess`: the TUI runs with terminal autowrap off and the child needs it on (an AST-free grep test enforces this).
- **TUI `Model` is a value receiver** with pointer fields (`modal`, `popup`) for state that must persist across the value copy.
- **A window opened from a popup returns to that popup when closed.** The
  popup is hidden while the window is open, never destroyed. Layer-stack
  windows get this from `pushLayer`/`popLayer`; a popup handing off to the
  files view (not a layer) goes through `handOffToFilesView`, which parks the
  stack for the view's esc/`l` to restore. `clearLayers` is only for a popup
  handing off to an *operation* (details: `docs/CLAUDE-details.md`).
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

Features follow: **brainstorm → spec → plan → execution by THIS session (never
subagents) → human merges** (superpowers skills). Specs live in `docs/superpowers/specs/`, plans in
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
rebase, conflict editor, diff, visual graph, sparse-checkout). The
hunk-parity roadmap (review notes, the agent lane, live steering via `gg
session`, syntax highlighting) is shipped through phase 3; phases 5 and 6
(viewer extras, in-view search) remain.

<!-- rtk-instructions v2 -->
# RTK (Rust Token Killer) - Token-Optimized Commands

## Golden Rule

**Always prefix commands with `rtk`**. If RTK has a dedicated filter, it uses it. If not, it passes through unchanged. This means RTK is always safe to use.

**Important**: Even in command chains with `&&`, use `rtk`:
```bash
# ❌ Wrong
git add . && git commit -m "msg" && git push

# ✅ Correct
rtk git add . && rtk git commit -m "msg" && rtk git push
```

## RTK Commands by Workflow

### Build & Compile (80-90% savings)
```bash
rtk cargo build         # Cargo build output
rtk cargo check         # Cargo check output
rtk cargo clippy        # Clippy warnings grouped by file (80%)
rtk tsc                 # TypeScript errors grouped by file/code (83%)
rtk lint                # ESLint/Biome violations grouped (84%)
rtk prettier --check    # Files needing format only (70%)
rtk next build          # Next.js build with route metrics (87%)
```

### Test (60-99% savings)
```bash
rtk cargo test          # Cargo test failures only (90%)
rtk go test             # Go test failures only (90%)
rtk jest                # Jest failures only (99.5%)
rtk vitest              # Vitest failures only (99.5%)
rtk playwright test     # Playwright failures only (94%)
rtk pytest              # Python test failures only (90%)
rtk rake test           # Ruby test failures only (90%)
rtk rspec               # RSpec test failures only (60%)
rtk test <cmd>          # Generic test wrapper - failures only
```

### Git (59-80% savings)
```bash
rtk git status          # Compact status
rtk git log             # Compact log (works with all git flags)
rtk git diff            # Compact diff (80%)
rtk git show            # Compact show (80%)
rtk git add             # Ultra-compact confirmations (59%)
rtk git commit          # Ultra-compact confirmations (59%)
rtk git push            # Ultra-compact confirmations
rtk git pull            # Ultra-compact confirmations
rtk git branch          # Compact branch list
rtk git fetch           # Compact fetch
rtk git stash           # Compact stash
rtk git worktree        # Compact worktree
```

Note: Git passthrough works for ALL subcommands, even those not explicitly listed.

### GitHub (26-87% savings)
```bash
rtk gh pr view <num>    # Compact PR view (87%)
rtk gh pr checks        # Compact PR checks (79%)
rtk gh run list         # Compact workflow runs (82%)
rtk gh issue list       # Compact issue list (80%)
rtk gh api              # Compact API responses (26%)
```

### JavaScript/TypeScript Tooling (70-90% savings)
```bash
rtk pnpm list           # Compact dependency tree (70%)
rtk pnpm outdated       # Compact outdated packages (80%)
rtk pnpm install        # Compact install output (90%)
rtk npm run <script>    # Compact npm script output
rtk npx <cmd>           # Compact npx command output
rtk prisma              # Prisma without ASCII art (88%)
rtk uv run <cmd>        # Compact uv project command output
```

### Files & Search (60-75% savings)
```bash
rtk ls <path>           # Tree format, compact (65%)
rtk read <file>         # Code reading with filtering (60%)
rtk grep <pattern>      # Search grouped by file (75%). Format flags (-c, -l, -L, -o, -Z) run raw.
rtk find <pattern>      # Find grouped by directory (70%)
```

### Analysis & Debug (70-90% savings)
```bash
rtk err <cmd>           # Filter errors only from any command
rtk log <file>          # Deduplicated logs with counts
rtk json <file>         # JSON structure without values
rtk deps                # Dependency overview
rtk env                 # Environment variables compact
rtk summary <cmd>       # Smart summary of command output
rtk diff                # Ultra-compact diffs
```

### Infrastructure (85% savings)
```bash
rtk docker ps           # Compact container list
rtk docker images       # Compact image list
rtk docker logs <c>     # Deduplicated logs
rtk kubectl get         # Compact resource list
rtk kubectl logs        # Deduplicated pod logs
```

### Network (65-70% savings)
```bash
rtk curl <url>          # Compact HTTP responses (70%)
rtk wget <url>          # Compact download output (65%)
```

### Meta Commands
```bash
rtk gain                # View token savings statistics
rtk gain --history      # View command history with savings
rtk discover            # Analyze Claude Code sessions for missed RTK usage
rtk proxy <cmd>         # Run command without filtering (for debugging)
rtk init                # Add RTK instructions to CLAUDE.md
rtk init --global       # Add RTK to ~/.claude/CLAUDE.md
```

## Token Savings Overview

| Category | Commands | Typical Savings |
|----------|----------|-----------------|
| Tests | vitest, playwright, cargo test | 90-99% |
| Build | next, tsc, lint, prettier | 70-87% |
| Git | status, log, diff, add, commit | 59-80% |
| GitHub | gh pr, gh run, gh issue | 26-87% |
| Package Managers | pnpm, npm, npx | 70-90% |
| Files | ls, read, grep, find | 60-75% |
| Infrastructure | docker, kubectl | 85% |
| Network | curl, wget | 65-70% |

Overall average: **60-90% token reduction** on common development operations.
<!-- /rtk-instructions -->