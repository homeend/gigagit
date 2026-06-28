# CLAUDE.md — gigagit (`gg`)

Guidance for Claude Code (and humans) working in this repo.

## What this is

**gigagit** (`gg`) is a Go terminal git client aimed at very large monorepos
(~100GB, ~20GB head). It blends GitKraken's one-key smart operations with
lazygit's fast keyboard TUI, runs cross-platform (Linux/macOS/Windows), and
shells out to the system `git` binary rather than reimplementing git.

Module: `github.com/homeend/gigagit` · Go 1.26.

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
| `domain`     | Frontend-facing command + query layer: `Execute` runs an operation under its repo-gate reservation; `Snapshot`/`Status`/`Worktrees`/`ShowFile`/`CommitFiles`/`TopLevel`/`CurrentBranch`/`GitCommonDir` run reads under a Read reservation, parallel and singleflight-coalesced; `CommitFeed` is the paged commit-history read-model backing the Commits panel (carries a `LogScope` — branch selection plus path/author/message/date filters (any filter axis suppresses the TUI commit-graph); supersede-cancels a superseded in-flight walk; `model.Commit.Refs` holds `%D` ref decorations). Also hosts the `Differ` (plain/enhanced, plain/cached decorator over lazy byte-sources) serving commit diffs from the cache. `Status`/`Snapshot` run an **EOL-only reconcile** (`statusFiltered`/`dropEOLOnly` + the `git.ModifiedIgnoringEOL` verb): a tracked file whose only unstaged change is line endings (CRLF↔LF) is dropped from the modified set unless `SetShowEOLOnlyChanges(true)` (from `[ui] show_eol_only_changes`). Emits the op span. Backs the TUI's per-source refresh registry: each source calls a domain query and `domain` exposes two registry-specific additions — `Conflict(ctx, st)` (derives conflict state from a status the caller already read, avoiding a second round-trip) and `CommitTimes(ctx, shas)` (gated head-time lookup so the TUI worktrees source needn't import `internal/git` directly). |
| `repogate`   | Per-repo reservation gate (Read/RefWrite/TreeWrite, writer-preferring FIFO, escalation), process-global registry keyed by git common dir. |
| `git`        | Thin git verbs on `*git.Repo` (status, branches, worktrees, sync, stash, …). One verb ≈ one git invocation. |
| `gitcmd`     | Fluent argv builder (`New("sub").Arg(...).ArgIf(cond, ...).ToArgv()`). |
| `gitexec`    | The `Runner` interface (`Run`/`Stream`), the real `ExecRunner`, and `FakeRunner` for tests. `LimitRunner` bounds concurrent git subprocesses; it now honours `ctx` while acquiring the semaphore (bug #4 fixed — a cancelled background read releases the slot immediately rather than waiting). |
| `gitwatch`   | Pure fsnotify wrapper + `.git`-layout path→source map (`Plan`) + drvfs `Supported` gate + debounced `Watcher`; no git/TUI/domain imports. Backs event-driven auto-refresh (Phase D). TUI maps `gitwatch.Source`→`sourceKey` and feeds `enqueueDue`. Watches worktrees, reflog, branches, remotes (recursive ref-tree watching for branches/remotes). |
| `model`      | Shared plain data types (`Status`, `Branch`, `Worktree`, `Commit`, …). `model.FileAddress` (worktree/branch/commit/shelf-id/path + `FileState`) is the one shared address/display behind both a shelf entry's `Origin` and a bookmark's address (`Bookmark.Address()`), rendered by `FileAddress.Display()`. |
| `tui`        | Bubble Tea Elm-style UI (value-receiver `Model`, panels, modal Decider, async ops). A per-source refresh registry (`source.go`) drives actions, `r`, and startup: eight named sources (status, branches, remote branches, tags, reflog, worktrees, commit feed, identity) each load independently via their domain query and emit a `dataAvailableMsg`; an op→source mapping restricts which sources each action reloads; per-panel ⏳ spinners track in-flight loads. Repo-switch (`reRoot`) still uses the legacy `loadCmd`. `refresh.go` adds the **background auto-refresh scheduler**: a single-lane FIFO — `refreshTick` calls `dueItems` (which calls `scheduledInterval` per item — fixed configured seconds floored at `min_seconds`, no adaptive engine) to enqueue overdue items; `enqueueDue` deduplicates by type; the lane drains exactly one item at a time (`bgBusy`/`bgActiveItem`/`bgQueue`), so background reads never overlap. Manual `r` and background reads record their duration in a per-item rolling ring (`refreshDur`, capped at 10 samples) via `recordDuration`; the **app-start fan-out is excluded** (`dataAvailableMsg.startup` → not measured). Durations are **informational only** — they are displayed as avg stats in the "Refresh rates" editor and do not affect scheduling. The `fetch` and `remote_tags` rows are config-gated (network) and opt-in only (`[refresh] fetch = 0` → never runs); both are modeled as synthetic `refreshItem`s (`fetchItem`/`isFetch`, `remoteTagsItem`/`isRemoteTags`) so `reloadAll`/`r` never triggers a network call. Additionally, `remoteTagsItem` is **auto-enqueued on every `srcTags` arrival** (`dataAvailableMsg{source: srcTags}`) when `autoRemoteTagsEnabled()` (= `!cfg.Refresh.DisableRemoteTagsAuto`) and `len(m.tags) > 0`; this fires on app load and after any tag-mutating op, independent of `[refresh] enabled`. The Settings `,` menu includes an **"Auto remote-tag refresh"** toggle that calls `toggleAutoRemoteTags()` → `SetGlobalDisableRemoteTagsAuto` (mirrors `toggleAutoRefresh`). `bgRefreshHint` shows a `⟳ <source>…` status hint while the lane is busy (suppressed when the active item's rolling average is < 1s, so fast reads don't flicker). `startOp` cancels `bgCancel` so a user action preempts any in-flight background read immediately. The `[refresh] enabled` toggle (Settings `,`) calls `SetGlobalRefreshEnabled`. Settings → **"Refresh rates"** is an **inline editor** (`ratesView`/`ratesSel`/`ratesEditing`/`ratesField`): ↑/↓ selects a source row, enter opens a numeric text field, enter saves via `saveRefreshInterval` (calls `config.SetRefreshInterval` to write `[refresh] <source>` to the **repo `.gg.toml`**, updates in-memory config, reseeds `refreshLastRun`), `w` toggles file-watch mode for watch-capable sources via `toggleRefreshWatch` (calls `config.SetRefreshWatch`, rewires the watcher), esc cancels. **Phase D event-driven refresh** (`watch.go`): `watchActive` in `refresh.go` marks sources whose watcher is running so `dueItems` skips polling for them (watcher events drive `enqueueDue` directly instead); `watch.go` builds the `gitwatch.Watcher` (worktrees/reflog/branches/remotes), maps `gitwatch.Source`→`sourceKey`, and delivers `watchEventMsg` into the refresh lane on each filesystem event (`watchAffectedSources` expands a primary source to its consequences — a branch/remote change also reloads `srcFeed` so the Commits panel's `%D` decorations/tip markers update, mirroring `opAffectedSources`); disabled on WSL2 9p (`m.watchSupported=false` → interval fallback). The Refresh-rates editor shows a `[x]`/`[ ]` file-watch checkbox on watch-capable rows. **Commits-panel row rendering** (`commit_ident.go`, `view.go`): `commitIdent` carries `tags []string` and `count int`; `markerField()` renders the fixed 3-cell marker area — a superscript badge (`■²`…`■⁺`) fills the filler cell when ≥2 local tips (dropped when `■▲` both present). `commitDecoGroup(id, budget)` is the single shared layout helper called by BOTH `commitIdentRowAt` (row string) and `commitDecoratorsRange` (yellow tag coloring) with the SAME `commitGroupBudget` — any divergence colors the wrong columns (the SYNC INVARIANT). `groupBase = identStart + identW`. Tags colored yellow (color 220) via `tagDecoStyle` through the existing single-pass `commitLineDecorator`; full group collapses to `(+N)` when `lipgloss.Width(group) > commitGroupBudget(boxW, identW)`; old `pills()` removed. |
| `cli`        | Scriptable command frontend; `cliDecider` answers forks from a flag policy or stdin. |
| `worktree`   | Shared worktree template resolution used by BOTH the TUI popup and the CLI. |
| `repos`      | Machine-local MRU registry of opened repositories (XDG state file) behind the repo switcher. |
| `agentskill` | The embedded "using-gg" skill (go:embed + version marker) that teaches AI agents the gg CLI. |
| `agentinit`  | Hardcoded agent registry + detect/status/install behind `gg init` and the TUI Settings popup. |
| `config`     | TOML config (`.gg.toml`), field-level overlay (defaults→global→repo), `<seq>` counters. Sections: `[worktree]`/`[ui]`/`[debug]`/`[refresh]`. The `[refresh]` section holds the master `enabled` bool, per-source interval seconds (`status`/`branches`/`remotes`/`worktrees`/`tags`/`reflog`/`feed`/`fetch`/`remote_tags`, all defaulting to 0/off), `min_seconds` (default 10; floor on any auto-refresh interval so cheap sources don't poll faster), `disable_remote_tags_auto` (inverted bool, default `false` = auto-refresh ON; independent of `enabled` — disabling it suppresses the `srcTags`-arrival auto-enqueue, not the timed `remote_tags` interval), and per-source file-watch bools (`worktrees_watch`/`reflog_watch`/`branches_watch`/`remotes_watch`, all default `false`). No adaptive keys (`disable_adaptive`/`max_read_seconds`/`backoff_factor` are gone). Read-only at runtime **except** five non-destructive line-edit writers: `SetGlobalDebugLogOperations` (`[debug] log_operations`), `SetGlobalRefreshEnabled` (`[refresh] enabled`), `SetRefreshInterval` (`[refresh] <source>` in the **repo** `.gg.toml` — backing the Settings "Refresh rates" inline editor), `SetGlobalDisableRemoteTagsAuto` (`[refresh] disable_remote_tags_auto` in the **global** config — backing the Settings "Auto remote-tag refresh" toggle), and `SetRefreshWatch` (`[refresh] <source>_watch` in the **repo** `.gg.toml` — backing the `w` file-watch toggle in the same editor). Both `gg config init` and `gg config populate` are generated from the `settingDocs` registry in `template.go` (single source of truth); `populate` is an additive, comments-only top-up that appends missing keys as `[populated]`-marked lines and never overwrites user values. |
| `template`   | Pure branch/path template resolver (`<parent-branch>`, `<repo>`, `<date:…>`, `<seq:…>`, `<user:…>`, …). |
| `textdiff`   | Pure line-alignment engine (Myers + guards) behind the side-by-side diff view; no git/TUI imports. An `Enhanced` option adds word-level intraline spans on changed rows. |
| `commitgraph`| Pure single-line commit-graph lane engine (`Lay(commits []{Hash,Parents})` → per-row Unicode glyph cells + node lane); no git/TUI/lipgloss imports. Consumed by the TUI Commits panel (cached in `m.commitGraphRows`, drawn only in natural feed order — `commitGraphOn`). |
| `cache`      | Generic injected in-memory LRU cache factory (`Factory.Cache(name) Cache`, `GetOrLoad`/`Load[V]`); two-bound eviction (entry count + byte budget via `Sized`). Keys are caller-chosen hashes. First consumer: the commit-diff cache. |
| `clipboard`  | System-clipboard writer behind a single `Copy(tty, text)`: prefers a native OS command (`clip.exe` on WSL/Windows, `pbcopy` on macOS, `wl-copy`/`xclip`/`xsel` on Linux), falls back to the pure OSC 52 escape for remote/SSH or when no native command exists (SSH detected → OSC 52 first so it reaches the local terminal; WSL detected via kernel osrelease so `clip.exe` beats WSLg `wl-copy`). The OSC 52 sequence builder + tmux/screen passthrough wrapping stay pure/unit-testable; no TUI/git deps. Used by the TUI `.` menu copy actions. |
| `shelf`      | Non-git, per-file content store ("the shelf") behind a fixed `Store` interface (default impl: content-addressed blob files + atomic-rewrite TOML index under the XDG state dir, keyed by git common dir); named buckets with an implicit `default` + hidden support; paged `List`. Owned by `domain`; frontends never import it (archtest-guarded). The shared `model.FileRef` (Unstaged/Staged/Commit/Shelf) + `domain.ResolveBytes` + the generic `engine.WriteFile` op give "compare anything" / "copy anywhere as unstaged". Each entry's `Origin` is a `model.FileAddress` (captured at shelve-time), rendered bookmark-style by `FileAddress.Display()`. The TUI surfaces the shelf as a global `G` quick-switcher popup (no left tab) mirroring the bookmark `g` popup; cross-store compares (focused-vs-shelf, bookmark↔shelf) reuse `pendingCompare` + the shared `openPickerDiff` (closes both popups + clears the stack). |
| `bookmark`   | Persistent registry of richly-addressed file references ("bookmarks") behind a fixed `Store` interface (default impl: atomic-rewrite `bookmarks.toml` under the XDG state dir, keyed by git common dir; records only, no blobs). Owned by `domain`; frontends never import it (archtest-guarded). A bookmark's **identity = its address** (`model.Bookmark`: worktree/branch/commit/shelf-id/path + state), the **content determinator = a blob SHA when permanent** (committed/shelf → frozen) else **live-by-address** (a worktree's working/index file). `domain.BookmarkBytes` resolves it (git verbs `CatFileBlob`/`ShowFileInDir`/`BlobSHA`); jump/compare/paste reuse the `Differ` + `engine.WriteFile`. Bookmarks also include **path-less commit pointers** (`Bookmark.IsCommit()`: `StateCommitted` + empty `Path`, no blob SHA — the commit is the anchor); created via the Commits `.` menu, `enter` in the `g` switcher whole-tree-compares one (base) against the selected Commits-panel commit (subject), reusing `Endpoint`/`CompareFiles`. `BookmarkBytes`/paste/file-compare are guarded against them. |
| `profile`    | Writable registry of named git-identity presets ("app profiles") behind a fixed `Store` interface (default impl: atomic-rewrite `profiles.toml` under the XDG state dir). **Two-scoped** — a `global` store (every repo) plus a per-repo store keyed by git common dir; `domain` owns both and merges them, tagging each row's `model.ProfileScope`. Owned by `domain`; frontends never import it (archtest-guarded). Backs the Settings → "Identity & profiles" surface. Identity reads/writes use the `git.ConfigGet`/`ConfigSet` verbs (local/global/effective scopes) + the `domain.Identity` query + the `engine.SetIdentity` op — **the first feature in gg that writes git config** (`internal/config` stays read-only at runtime). |
| `prefix`     | Writable registry of reusable, templated branch-name prefixes ("skeletons") behind a fixed `Store` interface (default impl: atomic-rewrite `prefixes.toml` under the XDG state dir). **Two-scoped** like `profile` — a `global` store plus a per-repo store keyed by git common dir; `domain` owns both, merges + tags `model.ProfileScope`, and validates tokens (`domain.ValidatePrefixValue`: all `<…>` tokens except `<branch>`, including interactive `<user:LABEL>`). Owned by `domain` (`Prefixes`/`AddPrefix`/`RemovePrefix`); frontends never import it (archtest-guarded). Surfaced by the TUI prefix picker (create-branch `ctrl+p`, create-worktree `p`; a `templateFill` step collects `<user:…>` labels), the Settings → "Branch prefixes" manager, and `gg prefix ls\|add\|rm`. |
| `shellinit`  | `gg shell-init [bash|zsh|fish]` wrappers (cd-on-switch via `--cwd-file`). |
| `observ`     | Observability: span ring buffer, tracing, redaction, panic dump. `SetSpanSink(w)` mirrors every recorded span (each op + each git invocation, redacted JSON lines) to a writer — the basis of the TUI **operation log** (`internal/tui/oplog.go`, toggled from `,` Settings) and the `--time-track` CLI flag. Also a process-global **failure seam** (`NoteFailure`/`SetFailureSink`/`SessionFailures`): `domain` records every genuine, frontend-surfaced failure (each `query` + `Execute` error path, excluding `context.Canceled`/`DeadlineExceeded`) to a bounded session ring and an always-on `errors.log` (TUI-wired beside `operations.log`), surfaced by the Settings → "Session errors" viewer. |
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
