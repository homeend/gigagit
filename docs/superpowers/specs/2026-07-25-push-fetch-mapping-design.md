# Push fetch-refspec mapping — design

Date: 2026-07-25 · Status: approved for planning · Branch: `feat/push-fetch-mapping`

## Problem

In clones with a narrowed fetch refspec — what `git clone --single-branch`
produces, and `--depth` implies `--single-branch` — a successful
`git push -u origin <branch>` never creates or moves
`refs/remotes/origin/<branch>`, and `%(upstream:short)` stays empty even
though `branch.<name>.remote/merge` are set. gg's Commits-panel ↓↑ tip
markers and ahead/behind counts are built from exactly those two inputs
(the feed's `%D` decorations and each branch's `Upstream` field), so for an
unmapped branch the ↑ marker can never appear and no amount of refreshing
helps. Narrowed clones are the norm for very large monorepos, which is why
the symptom reads as "push sometimes doesn't update the icons, especially
in the monorepo" — it is per-branch (mapped branches like `main` work),
not random.

Verified empirically (2026-07-25, git 2.43): in a `--single-branch` clone,
`push -u origin feat` succeeds with `remote.origin.fetch =
+refs/heads/main:refs/remotes/origin/main`; afterwards `refs/remotes/`
contains only `origin/main`, `%(upstream:short)` on `feat` is empty, and
`%D` at the feat tip is `HEAD -> feat`. In a full clone the same push
yields `origin/feat` in both. Adding
`+refs/heads/feat:refs/remotes/origin/feat` to the refspec and running one
`git fetch origin feat` (near-free — the remote just received our objects)
fixes both.

The TUI's post-push refresh wiring was audited and is correct (bounded 5s
pre-push tag check falls through to a plain push on timeout; post-op reads
are untimed, uncancellable, error-surfacing; `LoadInitial` re-walks). No
code-side timing fix is needed; this feature closes the config-side gap.

## Decisions (user)

- **Ask every time** at push: when the pushed branch is not mapped, a
  decision (add mapping / skip) on each affected push. No silent config
  writes, no remembered answer.
- **Notice + fix action** at startup: a notification-center notice lists
  local branches whose upstream is configured but unresolvable, with an
  action that adds their per-branch mappings and fetches just those
  branches. Covers branches broken before this feature or pushed outside
  gg.
- Remedy is always **per-branch mapping** (`+refs/heads/X:refs/remotes/<remote>/X`),
  never widening to the wildcard — a wildcard's next fetch could be a
  massive download on a monorepo remote.

## Design

### Engine: post-push probe + decision (in `engine.Push`)

A shared helper `ensureRemoteTracking(ctx, deps, remote, branch, res
Result) Result` runs after every successful push, **before** the `Done`
event, at all three success exits of `Push.Run`: the plain-push success,
the forced-push success (`op.push`), and the rebase-recovery re-push
(`recoverRejected`).

1. **Probe** — resolve `refs/heads/<branch>` and
   `refs/remotes/<remote>/<branch>` via the existing `ForEachRef` verb
   (exact ref name as the pattern; component-boundary matching plus an
   exact-refname filter, the `BranchVersions` over-match guard). Equal →
   return `res` unchanged. This is the only added cost on healthy repos:
   two cheap ref lookups. Observing what git actually did (rather than
   parsing refspecs) catches every config variant.
2. **Decide** — fork `PromptReq("fetch_mapping.add", "%s/%s is not
   tracked by the fetch refspec — tip markers and ahead/behind cannot
   follow it. Add a tracking mapping for this branch?",
   []string{"add", "skip"}, remote, branch)`. `"add"` proceeds; any
   other answer, esc, or a decider error → skip (the post-create-hook
   convention: safe default skip, never fail the already-succeeded
   push).
3. **Add path** — duplicate guard first: `ConfigGetAll` on
   `remote.<remote>.fetch`; skip the config write when the exact spec
   line is already present (possible after a previous add whose fetch
   failed). Then `ConfigAdd` (local scope) of
   `+refs/heads/<b>:refs/remotes/<remote>/<b>`, then
   `FetchBranches(remote, [branch])` with output streamed as `GitLine`.
   Success appends to the summary via `AppendSummary` ("; mapped %s/%s
   for tracking"). Any failure appends a note and never fails the push.
4. **Skip path** — no summary suffix (the user just answered; no noise).

`Push`'s `LockMode` is unchanged (default `TreeWrite` covers the ref
write the fetch performs).

### Engine: `AddFetchMappings` op (the notice's fix action)

`AddFetchMappings{Remote string, Branches []string}` — empty `Branches`
is a no-op (the `PushTags` precedent). For each branch: the same
duplicate guard + `ConfigAdd`; then one `FetchBranches(Remote, Branches)`
invocation for all of them, streamed as `GitLine`. Summary: "mapped N
branches for tracking" (two-key singular/plural convention). A fetch
failure fails the op (fetching is its purpose), after the config lines
are already in place — re-running is idempotent thanks to the guard.
`LockMode()` is `RefWrite` (writes remote-tracking refs + a config file;
never touches the working tree).

### Git verbs (one invocation each; added to `GitOps`)

- `ConfigAdd(ctx, scope, key, value)` — `git config --add <key> <value>`.
- `ConfigGetAll(ctx, key) ([]string, error)` — `git config --get-all
  <key>`; missing key (exit 1) → empty slice, no error (the `ConfigUnset`
  exit-code pattern).
- `ConfigGetRegexp(ctx, pattern) ([][2]string, error)` — `git config
  --get-regexp <pattern> -z`; missing (exit 1) → empty, no error.
- `FetchBranches(ctx, remote, branches []string)` — `git fetch <remote>
  <b1> <b2> …`; empty branches is a caller error (ops no-op before
  calling).

### Domain: `RepoHealth` detection

`RepoHealth` gains `UnmappedBranches []string`: branches where
`branch.<n>.remote` + `branch.<n>.merge` are configured but the branch's
`%(upstream:short)` is empty. Detection = one
`ConfigGetRegexp("^(branch\\..*\\.(remote|merge)|remote\\..*\\.fetch)$")`
(one invocation covers the branch config AND which remotes have fetch
refspecs) plus one branches for-each-ref read. Two additional cheap
invocations in the health snapshot; ref-count-bound, not repo-size-bound,
so it stays "stat-level" even on the monorepo. Only branches whose
configured remote has a fetch refspec in that same read are listed (a
remote with no refspec at all is a different problem, not this
notice's).

### TUI

- The push decision needs **no new modal code** (any `DecisionRequest`
  renders via the existing modal Decider). `optionDisplayName` gains an
  `"add"` case ("skip" exists via the hook decision); both keys in all
  four bundles; `options_vocab_test` enforces.
- **Notice** (notify.go / notice_popup.go): stable id
  `narrow-fetch-refspec`, title naming the count ("N branches aren't
  tracked by the fetch refspec"), body explaining the ↓↑/ahead-behind
  consequence. Actions: "Add mappings + fetch N branches" → dispatches
  `AddFetchMappings{Remote: "origin", Branches: health.UnmappedBranches}`
  via `startOp`; plus the standard "Not now" (session) and "Never for
  this repo" (promptstate) rows. Standard blink segment.
- `opAffectedSources`: `AddFetchMappings` → `{srcBranches, srcRemotes,
  srcFeed}`. (`Push`'s existing mapping already covers the new outcome —
  the fetch moves a remote ref, and branches/remotes/feed all reload.)

### CLI (`gg push`)

The `--hook/--no-hook` precedent: `--map` pre-answers `"add"`, `--no-map`
pre-answers `"skip"`, mutually exclusive (usage error otherwise). With
neither flag: interactive stdin prompt via the existing `cliDecider`;
when stdin is not a terminal the decision defaults to `"skip"`.
`internal/agentskill/using-gg.md` documents the flags;
`agentskill.Version` bumped; `gg init --update` refreshes installed
copies.

### i18n

All new user-visible strings follow the stage-5 dual-channel rules: the
prompt via `PromptReq`, summary suffixes via `AppendSummary`, any
Progress line via `Progressf`/existing `Step` vocabulary ("fetching"
exists via the Fetch op). New keys added to all four bundles
(`ja/ko/zh/ru`); `engine_prose_test`, `options_vocab_test`, and the
i18n scan gates enforce coverage. Engine English strings (the CLI/agent
surface) stay byte-stable as always.

## Testing

- **Engine (FakeRunner)**: tracking ref present → no decision, summary
  unchanged; absent → decision forked; `"add"` → `config --add` +
  `fetch <remote> <branch>` argv; duplicate guard skips the config write
  but still fetches; decider error → skip, push still succeeds; the
  forced-push and rebase-recovery exits also probe (at least one covered
  explicitly). `AddFetchMappings`: no-op on empty, argv shape, fetch
  failure fails the op.
- **Git verbs (real repo)**: `ConfigAdd`/`ConfigGetAll`/`ConfigGetRegexp`
  round-trip incl. missing-key exit-1 mapping; `FetchBranches` against a
  local file remote.
- **Domain**: `RepoHealth.UnmappedBranches` on a fixture with a narrowed
  refspec + a pushed-but-unmapped branch; empty on a healthy clone.
- **e2e**: one scenario — file remote, narrow the refspec in setup, `gg
  push --map`, assert the refspec line exists and
  `refs/remotes/origin/<branch>` resolves; a `--no-map` variant asserts
  neither. (Schema details per the writing-e2e-scenarios skill at
  planning time.)

## Docs

`CHANGELOG.md`; `README.md` (push behavior + notice); `CLAUDE.md` (engine
entry for the push decision + `AddFetchMappings`, git verbs list,
`RepoHealth` field); `using-gg.md` + version bump.

## Out of scope

- Negative refspecs can still block an added mapping (the fetch then
  moves nothing); documented, not handled.
- Remotes other than the one being pushed to (gg pushes `origin`);
  triangular setups unchanged.
- Widening to the wildcard refspec — deliberately never done by gg.
- MCP surface (no push tool exists).
