# gigagit (`gg`) — M1 Design

**Date:** 2026-06-11
**Status:** Approved for implementation planning
**Scope:** Milestone 1 only. M2/M3 captured as roadmap context (§10), not specified here.

## 1. Summary

gigagit is a fast, user-friendly terminal git client for very large repositories
(monorepos with a ~20GB head and ~100GB total history). It blends GitKraken's
*simplified, auto-orchestrated operations* with lazygit's *fast keyboard-driven
TUI*, and adds a monorepo-first wedge (worktree-aware smart operations, partial
clone / sparse-checkout awareness) plus a future agent-facing MCP layer.

The binary is `gg`, written in Go, cross-platform (Windows, macOS, Linux).

**M1 delivers:** the core engine + a TUI that can view repo state and perform
the *smart sync* operations end-to-end.

## 2. Goals & Non-Goals

### Goals (M1)
- A pure-Go **core engine** that shells out to the system `git` binary.
- A **TUI** (multi-panel) to view status, branches, commit list, and diffs.
- **Smart operations:** smart pull, smart switch, commit, push, stash.
- **Ref-only undo** (reflog-backed) for operations that move refs.
- **Never block the UI**: all engine operations are async, stream progress, and
  are cancellable.
- An engine↔frontend contract designed for the hardest consumer (an MCP agent
  that cannot block on a human), so CLI and MCP fall out of it later.

### Non-Goals (M1 — deferred, see §10)
- Visual commit graph rendering (commit *list* only in M1).
- Hunk/line staging, interactive rebase, conflict editor, rich/side-by-side diff.
- Provider integrations (PRs/issues), Workspaces (multi-repo), AI features.
- Full sparse-checkout / partial-clone *management* UI (status is sparse-*aware*,
  but managing sparse sets is M3).
- CLI and MCP frontends (M2/M3). Their needs shape the M1 engine API but are not
  built in M1.
- Compensating (working-tree) undo. M1 undo is ref-only and labeled as such.

## 3. Architecture

A pure-Go core engine shells out to the user's system `git`. Three thin
frontends sit on top; only the TUI is built in M1.

```
┌─────────────┐  ┌─────────────┐  ┌─────────────┐
│ TUI (M1)    │  │ CLI (M2)    │  │ MCP (M3)    │   frontends
└──────┬──────┘  └──────┬──────┘  └──────┬──────┘
       └────────────────┼────────────────┘
                 ┌───────▼────────┐
                 │  core engine   │  operations, decisions, events
                 └───────┬────────┘
                 ┌───────▼────────┐
                 │  git runner    │  exec system git, parse porcelain
                 └────────────────┘
```

### Module layout
Mirrors lazygit's proven sublayering (studied directly from its source), with one
deliberate divergence recorded in §3.1.

- `cmd/gg` — entrypoint, flag parsing, launches the TUI.
- `internal/gitexec` — the process layer (lazygit's `oscommands`): exec the git
  binary, context cancellation / process-group kill, env, **streaming via an
  `onLine` callback**, and **credential-prompt handling**. Exposes a small
  `Runner` interface with a **fake** implementation for tests.
- `internal/gitcmd` — a fluent command builder (lazygit's `GitCommandBuilder`):
  `Arg/ArgIf/Config/Dir(-C path)/GitDir/WorktreePath`. The `.Dir`/`WorktreePath`
  args are how we target *another worktree* in smart-pull.
- `internal/git` — domain-grouped **thin git verbs** (lazygit's `git_commands`):
  `branch`, `commit`, `status`, `stash`, `sync` (fetch/pull/push/fast-forward),
  `worktree`, plus a **loader/command split** — `*_loader.go` reads+parses
  (porcelain v2 status, branch/log/worktree lists), the command files mutate.
- `internal/model` — shared data types (lazygit's `models`): `Branch`, `Commit`,
  `File`, `Worktree`, `StashEntry`, `WorkingTreeState`.
- `internal/engine` — operation types and the `Operation`/`Decider`/`Event`
  contract, plus the concrete **smart operations** (the orchestration; see §3.1).
- `internal/tui` — the Bubble Tea application. Sub-structured along lazygit's
  GUI lines: **panels** (≈ lazygit `context`s, the focusable views), **update
  handlers** (≈ `controllers`, keybinding → action), **presentation**
  (model → rendered strings), and the modal `Decider` implementation.

The engine package has **zero** dependency on any frontend package. Frontends
depend on the engine, never the reverse.

### 3.1 Layering: adopted from lazygit, with one deliberate divergence

**Adopted** (validated against lazygit source): the `oscommands`→`git_commands`→
`models` layering, the runner interface + fake, the fluent command builder, the
loader/command split, the DI aggregator, and concrete sync primitives — a
`FastForward(branch, remote, remoteBranch)` that fetches a refspec without
checkout (= §5 step 2), `Pull` options carrying `WorktreeGitDir`/`WorktreePath`
(= §5 step 3), `--no-write-fetch-head` to allow concurrent fetch/pull, and a
background-fetch variant that fails (not prompts) on credential request.

**Divergence (justified by the three-frontend goal):** in lazygit the *smart
workflow orchestration* — "is the branch tracking? behind? prompt for upstream
or force-push? stash then checkout then pull then restore?" — lives in
`pkg/gui/controllers/*` (the UI layer), and the command layer even imports
`gocui.Task` for credential prompts. That orchestration is therefore **not
reusable by a CLI or MCP frontend**. gg moves all such orchestration **down into
the frontend-agnostic `engine` as `Operation`s**, and replaces the `gocui.Task`
leak with the `Decider`/`Event` abstraction (§4). This is the single most
important structural choice: it is what lets one engine drive all three
frontends, which lazygit's design cannot do.

## 4. The engine↔frontend contract (keystone)

Every operation is designed so that an MCP agent — which **cannot block on a
human** — can drive it. The TUI and CLI are then strictly easier cases.

```go
// An Operation is a long-running, cancellable git workflow.
type Operation interface {
    Run(ctx context.Context, repo *Repo, deps OpDeps) (Result, error)
}

type OpDeps struct {
    Events  chan<- Event       // progress / log / decision-needed stream
    Decider Decider            // resolves mid-flight forks
    Policy  ResolutionPolicy   // pre-answers, e.g. {OnNonFastForward: Abort}
}

// Decider resolves a fork the operation cannot decide on its own.
type Decider interface {
    Decide(ctx context.Context, req DecisionRequest) (DecisionResponse, error)
}
```

### Event stream
`Event` is a tagged union consumed by every frontend:
- `Progress{Step, Detail}` — high-level step ("stashing", "fetching", "pulling").
- `GitLine{Raw}` — a raw line of git stdout/stderr (for a live log view).
- `DecisionNeeded{Request}` — emitted alongside the `Decider` call so passive
  observers can render the prompt.
- `Done{Result}` — terminal event.

### Decision resolution per frontend
- **TUI (M1):** `Decider` renders a modal; the selected option is returned. The
  cancel key cancels `ctx`.
- **CLI (M2):** `Decider` resolves from flags/`Policy`; if interactive and
  unanswered, prompts; in non-interactive mode with no policy answer, errors.
- **MCP (M3):** `Decider` resolves **only** from `Policy`. On an unanswered
  `DecisionRequest` it does **not** block — the operation returns a structured
  `decision_required` result that the agent re-invokes with an added answer.

This single envelope (request + policy → event stream with a decision-needed
event → result) is what makes all three frontends share one engine.

## 5. Smart pull — decision tree (the differentiator)

Encoded as logic. Given a target branch `T` and the user's *intent*
(`pull-and-stay` = end up on T, vs `pull-in-background` = just update T's ref):

```
1. Is T the current branch?
   └ yes → fetch + fast-forward-only pull
           └ if non-fast-forward → DecisionNeeded{rebase | merge | abort}

2. T is NOT current. Does intent require being ON T?
   └ no  → git fetch <remote> T:T          ★ updates the ref, ZERO checkout/stash
           └ if not fast-forwardable → DecisionNeeded{checkout-and-resolve | abort}
   └ yes → continue to 3

3. Is T checked out in ANOTHER worktree?  (git refuses a normal checkout here)
   └ yes → operate inside that worktree (fetch + pull there)
           └ if that worktree is dirty → DecisionNeeded{stash-there | abort}
   └ no  → continue to 4

4. Is the current working tree dirty?
   └ yes → auto-stash → switch to T → pull → (switch back if intent says so)
           → stash pop
           └ pop conflicts → DecisionNeeded{keep-stash | resolve | abort}
             (the stash is NEVER dropped while a conflict is unresolved)
   └ no  → switch to T → pull → (return per intent)
```

Step 2 (`git fetch <remote> T:T`) is the headline simplification: most
"pull branch X" intents do not require a checkout at all, so we skip the
stash/switch/restore dance that a GUI performs internally. lazygit implements
exactly this as `SyncCommands.FastForward`; gg reuses that primitive and adds
the decision tree on top of it.

**Safety rule:** any auto-stash created by smart pull is tagged
(`gg-autostash:<branch>:<timestamp>`) and is never dropped until its contents
are confirmed reapplied. On unresolved conflict we stop and hand control to the
`Decider`.

## 6. Non-blocking & big-repo strategy (invariant)

On a 20GB head, `status` / `fetch` / `pull` take seconds-to-minutes. Therefore:

- **No engine call ever blocks the UI thread.** Operations run in a goroutine,
  stream `Progress` events, and honor `ctx` cancellation (which sends the git
  subprocess a kill on its process group).
- The `status` call is built from day one to use **fsmonitor**,
  **untracked-cache**, and **sparse-aware** flags. Without these, status alone is
  unusable at monorepo scale. (Managing the sparse set itself is M3; *respecting*
  it is M1.)
- Long operations show a spinner + the current `Progress.Step` and an optional
  live git-log pane fed by `GitLine` events.

## 7. TUI framework: Bubble Tea

**Decision:** Bubble Tea, with Bubbles (components) and Lip Gloss (styling).

Rationale: Bubble Tea's Elm-style message loop maps 1:1 onto the §4 `Event`
stream — async git output arrives as messages, with no manual goroutine/redraw
coordination. Alternative considered: gocui (used by lazygit), rejected as more
imperative and harder to wire the async/cancellable contract onto cleanly.

### M1 layout
Multi-panel, keyboard-driven:
- **Left column:** Branches panel (local + remote-tracking), Status panel
  (working-tree changes, porcelain v2).
- **Right column:** context panel showing the selected item's diff or commit
  details; the Commits panel shows a linear recent-commit *list* (no graph).
- **Footer:** context-sensitive keybindings + a status/progress line.
- **Modal:** the `Decider` surface for smart-operation forks.

Core keybindings (subject to refinement during planning): `p` smart pull,
`P` push, `c` commit, `s` switch, `S` stash, `z` undo (ref-only), `Tab` cycle
panels, arrows/`hjkl` navigate, `q` quit, `?` help.

## 8. Error handling

- The git runner returns a structured `GitError{ExitCode, Stderr, Cmd}`; the
  engine maps known stderr signatures (e.g. "already checked out",
  "non-fast-forward", merge-conflict markers) into typed conditions that drive
  the decision tree, rather than leaking raw strings to the UI.
- Unmapped git failures surface verbatim via a `Done{Result}` carrying the error
  plus the captured `GitLine` log, so the user can see exactly what git said.
- Cancellation (`ctx`) is a first-class outcome, not an error: it leaves the repo
  in a safe, documented state (e.g. mid-stash is always resolvable).
- **Credential handling** (push/pull/fetch over HTTPS/SSH): the `gitexec` runner
  detects git's credential prompt and routes it through the `Decider` (TUI: a
  secure modal; MCP: must be pre-supplied via `Policy`/credential helper, never
  blocks). Background fetch uses a *fail-on-credential* variant so it never hangs
  waiting for input. (lazygit handles this via `PromptOnCredentialRequest` /
  `FailOnCredentialRequest`; gg keeps it behind the frontend-agnostic `Decider`.)

## 9. Testing

- **Engine:** tested against **real throwaway git repositories** created in temp
  dirs — not mocks — because the entire risk surface is git's real behavior.
  Fixtures cover: clean tree, dirty tree, target-branch-in-another-worktree,
  non-fast-forward remote, stash-pop conflict.
- **Parsers:** table-driven tests over captured porcelain v2 / branch / log /
  worktree output.
- **TUI:** `Model`/`Update` logic unit-tested via Bubble Tea message dispatch;
  `View` rendering smoke-tested for panic-freedom and basic layout.
- **Smart pull:** each branch of the §5 tree has a dedicated fixture + test
  asserting the chosen path and the post-condition (ref state, working tree,
  stash list).

## 10. Roadmap context (not specified here)

- **M2 — CLI + Workspaces.** Add the CLI frontend (every smart op as a
  subcommand sharing the engine) and **Workspaces**: group & sync multiple repos,
  multi-repo smart operations. (User flagged Workspaces as important.)
- **M3 — MCP + heavy ops.** Add the MCP server (agent-facing, policy-driven, no
  blocking) and the deferred git features: hunk/line staging, interactive rebase,
  conflict editor, rich/side-by-side diff, visual commit graph, sparse-checkout
  management.

## 11. Open questions for planning

- Exact keybinding map (refine against lazygit muscle memory).
- Whether `pull-and-stay` vs `pull-in-background` intent is a per-invocation
  modal choice or a setting (default proposed: infer from whether T is current;
  modal-confirm when ambiguous).
- Minimum supported `git` version (proposal: pin to a version that guarantees
  porcelain v2 `-z`, worktree list `--porcelain`, and fsmonitor support).
