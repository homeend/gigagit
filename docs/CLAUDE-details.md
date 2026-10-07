> **Archive** — full per-package/per-feature detail moved out of CLAUDE.md on 2026-08-10.
> CLAUDE.md now holds only the load-bearing essentials; new deep feature detail accrues HERE (or in CHANGELOG.md / memory), not in the CLAUDE.md package map.

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
| `engine`     | Operations + the `Event`/`Decider`/`Result` contract. Smart ops: `SmartPull`, `SmartSwitch`, `SmartMerge`, `SmartRebase`, `Commit`, `Push`, `Stash`, `UndoLastCommit`, `CreateWorktree`, `RemoveWorktree`. `SmartCheckout`'s diverged and current-branch refusals are typed errors (`CheckoutDivergedError`, `CheckoutCurrentBranchError`; messages unchanged) so frontends can offer recovery (check-out-as-a-different-name; the TUI pre-empts the current-branch case at dispatch with a pull-now/checkout-as prompt). Also `PushTags{Remote, Names}` — pushes multiple tags in a single `git push <remote> refs/tags/…` invocation; empty `Names` is a no-op. Ops act on a `GitOps` interface (`*git.Repo` satisfies it). `Stage` has one fork: when `git add` refuses explicit paths because .gitignore excludes them (detected on the header line that survives `advice.addIgnoredFile=false`), `IgnoredPathsDecisionID = "stage.ignored"` offers `["force-add","abort"]` — `force-add` retries all requested paths with `git add -f` (idempotent: git stages the non-ignored subset before exiting 1), anything else (incl. a decider that cannot answer — the web stage handler, deciderless runs) returns git's refusal unchanged. `IgnoredPathsRefusal(err)` is exported for the TUI, whose synchronous `stageCmd` runs deciderless and re-raises the fork as a frontend modal that re-runs the op with the decision pre-answered; the CLI answers it via `gg add -f`. Git names the deepest ignored ancestor dir in the refusal (e.g. `docs/specs` for `docs/specs/a.md`) — those labels feed the prompt only, never the retry argv. A `HookRunner` seam (`OpDeps.HookRunner` interface / `ShellHookRunner` real impl) lets `CreateWorktree`/`CreateWorktreeForBranch` run a `PostCreateHook` after `AddWorktree` (stdin=/dev/null; output streamed as `GitLine` events; failure is non-fatal and noted in the op summary; env: `GG_MAIN_WORKTREE`/`GG_WORKTREE_PATH`/`GG_BRANCH`/`GG_REPO`). The hook is gated by an **approval decision** (`HookDecisionID = "post_create_hook.run"`, options `["run","skip"]`): the script is shown and the hook runs only when the decider answers `"run"`; the safe default is skip (any error or non-`"run"` response skips). `CreateWorktree` also carries `Keep WorktreeKeep` (`KeepNone`/`KeepStaged`/`KeepUnstaged`, the worktree-from-commit "keep changes" modes): a non-`KeepNone` mode pre-validates the start point via `ParentCount` and refuses a root or merge commit up front with a typed `WorktreeKeepParentError` — nothing is created — then, right after `AddWorktree` and before the post-create hook, runs `ResetInDir` (`reset --soft`/`--mixed` against `StartPoint^`) so the new branch lands on the commit's parent with the commit's own diff left staged/unstaged in the fresh worktree; the summary gains a "(commit's changes staged/unstaged)" suffix. Also `ExportToDir{Dir, Files []model.ExportFile}` — the copy-to-temp-dir primitive and **the first op that writes outside the working tree** (writes via `os` directly, not `deps.Repo.WriteWorktreeFile`); an existing `Dir` triggers an overwrite/cancel decision (`ErrExportCancelled` on cancel); `LockMode()` is `Read` (touches neither refs nor the working tree). Also `ExportFile{Path, Data}` — the file-grained sibling of `WriteFile` and the **second op that writes outside the working tree** (after `ExportToDir`); writes `Data` to the absolute `Path` via `os` directly; an existing `Path` with different bytes triggers the same overwrite/cancel decision (`ErrWriteCancelled` on cancel), identical bytes are a silent no-op; `LockMode()` is `Read`. Backs export-a-commit/file-as-patch. Also `WriteCommitGraph` (streams `git commit-graph write --reachable`; LockMode Read — a derived cache under .git/objects) and `SetGitConfig{Key, Value, Global bool}` — the generic single-key config write (Global is a bool, not git.ConfigScope, so frontends can construct it); both back the notification center; stage 3's config explorer reuses SetGitConfig. `SetGitConfig` now also carries `Unset bool` — one op for set AND unset (Value ignored on unset). `Commit`'s summary now names the commit it made (`committed <short-sha> <subject>` / `amended ...`), read back via `git.CommitLine` right after the commit lands (best-effort — a failed read only costs the sha in the summary). `Stage{Paths, All, Unstage}` stages/unstages paths in the index; `All` (`git add -A`, incl. untracked) is mutually exclusive with `Paths`/`Unstage`. `PruneWorktrees{}` runs `git worktree prune` to drop stale worktree admin entries; both ops rely on the default `TreeWrite` reservation (no custom `LockMode()`). Also `GenerateMessage{Command, Dir, Env}` — the commit-popup `ctrl+g` generate mechanic: runs a resolved `commit_message` external-tool command headless and returns the captured message as `Result.Captured`. It first writes three temp files — a labeled summary (`GG_CONTEXT_FILE`: numstat + recent commit subjects), the full `git diff --cached` (`GG_STAGED_DIFF`, replaced by a stat-only note past `MaxDiffBytes`), and an **empty output file** (`GG_MESSAGE_FILE`) — runs the command through a `CaptureRunner` seam (`OpDeps.CaptureRunner` interface / `ShellCaptureRunner` real impl: temp script + shell, stdin `/dev/null`, stdout captured to a buffer, stderr streamed as `GitLine`), then removes the temp files; `LockMode()` is `Read` (git reads only, no ref/tree writes — approval is the caller's/TUI's responsibility, not the op's). **Output-channel contract:** a tool MAY write the message to `$GG_MESSAGE_FILE` instead of stdout; **non-empty file content wins over stdout**. This exists because a task-agent (Junie) treats "write a commit message" as work-to-do and emits only a `### Summary/Changes/Verification` status report on stdout — the message never reaches stdout, so it comes back through the file; a tool whose stdout already IS the message (Claude's `.result`) leaves the file empty and stdout is used. The contract generalizes to any future capture lane (stage 3's review). Also `ReviewChanges{Command, Dir, Env, Diff model.DiffSpec, RangeLabel string}` — the `.`-menu/`gg review` review mechanic, a sibling of `GenerateMessage` reusing the same primitives: it writes a labeled summary (`GG_CONTEXT_FILE`: the range label + numstat), the full diff of `op.Diff` (`GG_REVIEW_DIFF` — the `GenerateMessage`/`$GG_STAGED_DIFF` analog, capped at `MaxDiffBytes`), and an empty `GG_MESSAGE_FILE`, then runs the command via the same `CaptureRunner` seam; `LockMode()` is `Read`. It shares the `$GG_MESSAGE_FILE` output-channel contract verbatim (non-empty file content wins over stdout — Junie's review comes back this way), but differs from `GenerateMessage` in what it diffs (`op.Diff`, a range or the working diff, not `--cached`) and returns the captured text as-is (no subject/body parsing — a review is free-form). Also `CompleteConflict{Command, Dir, Env, Op, Source, Target, ConflictedFiles}` — the headless resolve-and-complete capture op behind the web AI conflict lane, a `ReviewChanges` sibling that differs in one load-bearing way: it resolves `op.Command` ITSELF (unlike `ReviewChanges`, which takes an already-resolved command) because a custom `<context-file>` token needs the real temp path, which exists only inside the op; it writes the shared `template.ConflictContextDoc(op.Op, op.Source, op.Target, op.ConflictedFiles)` to `GG_CONTEXT_FILE` (the exact bytes the TUI's conflict tool runs write) plus an empty `GG_MESSAGE_FILE`, sets all twelve `GG_*` env vars (the TUI's `toolEnv` eleven plus `GG_MESSAGE_FILE`, capture-lane-only; the file/local/base/remote/merged ones empty — no per-file target) and `GG_TASK=conflict_complete`, and shares the `$GG_MESSAGE_FILE`-wins-over-stdout contract (an empty overview is not an error, the TUI's "reported no overview" stance). `LockMode()` is `Read` — deliberately: the AGENT mutates the tree and refs, gg only reads, so a Read reservation keeps other frontends' reads (status, commits) alive for the run's full minutes-long duration while still excluding gg's own tree/ref-writing ops; the TUI precedent for this category was no reservation at all ($EDITOR standing), so Read is strictly safer. Validation ("is anything paused?") is left to the domain wrapper, the same split `ReviewChanges` uses. Also `ApplyPatch{Path, Mode}` — imports a patch file from disk (the inverse of `ExportFile`; reads the file head via `os` directly, the read-side analog of that op's outside-the-tree precedent). `ApplyModeCommits` first refuses up front if a `git am` is ALREADY in progress (`AmInProgress` probed before `AmMailbox` runs) — without this, `AmMailbox` would fail immediately, the post-failure `AmInProgress` guard would see true, and the rollback would `am --abort` the USER'S own paused am (started outside gg) instead of one gg itself began. Once clear, it runs `git am --3way` and is atomic: on any failure an `AmInProgress`-guarded `AmAbort` rolls the branch back to its pre-am state (even mid-way through a multi-patch mailbox), nothing half-applied; refuses a plain diff up front (`ErrNotMailbox` — `git am` has no author/message to work with on a bare diff). `ApplyModeWorkingTree` is NOT a bare `git apply --3way`: real git's `--3way` implies `--index`, so it tries a plain `git apply` first (changes land unstaged, the mode's contract) and only falls back to `--3way` on a miss; a clean fallback apply is followed by a surgical unstage (`git.PatchPaths` reads back just the patch's own touched paths, `UnstagePaths` restores only those, leaving any unrelated pre-staged user changes alone — a known gap for a renamed file, where `PatchPaths` can only report the new path). A dirty `--3way` retry instead leaves standard conflict markers + unmerged index entries in the tree (the SmartMerge keep-conflicts `Result`+error shape: `Changed:true` AND a non-nil error, so the TUI's status refresh feeds the existing conflict process and the CLI exits 1). `ApplyModeAuto` (TUI only) forks a detected mailbox through the `apply_patch.mode` decision (options `working-tree`/`commits`/`abort`, safe option first, `abort` last so the TUI's `abortOption` — which maps esc to the option named `"abort"` if present — maps esc to a genuine cancel instead of silently running `git am`) — a decider error surfaces raw (the `ExportFile` convention), and the `abort` answer (plus, as a fallback, any unrecognized answer) maps to `ErrApplyCancelled`; a plain diff under `Auto` goes straight to the working-tree path, no decision. `LockMode()` is the default `TreeWrite` (no override) — both paths mutate the tree and/or refs. **i18n dual-channel (stage 5):** every op's English `Result.Summary`, `Progress.Detail`, and `DecisionRequest.Prompt` stay byte-identical (the CLI/log/e2e/agent surface is unchanged), and each additionally carries a localizable `Msg{Format, Args}` pair (`Result.SummaryParts`, `Progress.DetailMsg`, `DecisionRequest.PromptMsg`) built ONLY through the lockstep helpers in `msg.go` — `Result{…}.WithSummary(fmt, args)`, `.AppendSummary(suffixFmt, args)` (suffix glue lives inside the format), `Progressf(step, fmt, args)` (a Detail mixing English words into data; pure-data Details stay plain `Progress{}`), and `PromptReq(id, fmt, opts, args)`. Formats are always English literals at the call site (an AST gate in `internal/tui` forbids dynamic formats and hand-built `Summary:`/`Prompt:` strings); `Progress.Step` is a stable ~52-word vocabulary translated directly as its own key. The TUI renders the localizable channel (see the `i18n` entry); CLI/domain/oplog consume the unchanged English strings. Also `RepairWorktree{Path}` — runs `git worktree repair <path>` (default TreeWrite; no decisions — the repair confirm is TUI-side), rebinding a linked worktree's two absolute-path link records to the current environment's notation; backs the TUI's cross-environment worktree repair. Also `MoveWorktree{Path, Dest}` — runs `git worktree move` (default TreeWrite) from the MAIN worktree's directory rather than the tree being moved (Windows cannot rename a directory any process holds as cwd); refuses the main worktree, an existing/missing destination, and a destination nested inside the source, all as plain errors. The rename face (TUI `e` / `gg worktree rename`) is the same op with `Dest` computed by callers as the old parent joined with the new name. A locked worktree (an interrupted `add` can leave one) forks the `move-worktree-locked` decision (`unlock-and-move`/`abort`) before retrying once unlocked. `Result.Path` carries `Dest` so frontends can follow the move: the TUI chains its `pendingSwitch` `guardedReRoot` the same way a branch switch does when the moved worktree was the current one, and the CLI writes the equivalent new path to the shell-init cwd-file when the process's own cwd sat inside the moved tree. The TUI additionally tracks `pendingWorktreeMoveOld` to drop the old path from the repo-switcher MRU registry (`repos.Remove`) on a changed result — the destination self-registers on next open, including immediately via the chained reRoot. **Branch versions (operations history):** `VersionsPolicy{Enabled, MaxAgeDays}` lives on `OpDeps.Versions` — the zero value disables snapshots so bare `OpDeps` construction (tests, direct callers) stays unaffected; `domain.Execute` injects the config-driven policy on every real dispatch. `snapshotBranchTip(ctx, deps, branch, opToken)` (`snapshot_version.go`) is the best-effort helper every trigger op calls before mutating: no-ops when disabled/branch is empty/unborn; else resolves the branch tip, writes `refs/gg/versions/<branch>/<unix-ts>-<opToken>` via the new `git.UpdateRef` (bumping the timestamp on a same-second/same-op collision until the ref name is free), then prunes that branch's version refs older than `MaxAgeDays` (skipped at `<=0`) via `ForEachRef`+`DeleteRef`; any failure emits a `GitLine` note and returns — recording must never fail the real operation. Call sites (op token in parens): `SmartMerge` (`"merge"`, the branch merged INTO), `SmartRebase` (`"rebase"`), `InteractiveRebase` (`"interactive-rebase"`, called only AFTER the plan passes full validation, so a rejected plan never leaves a stray snapshot), `Commit` on `Amend` (`"amend"`; a plain commit does not snapshot), `UndoLastCommit` (`"undo-commit"`), `Reset` (`"reset"`, its reset-to-remote-tip lane), `DeleteBranch` (`"delete-branch"`, so a deleted branch's history is recoverable), `RestoreBranchVersion` itself (`"restore"`, so a restore is itself undoable), and `SmartPull`'s rebase/merge/reset-to-remote lanes (`"pull"`) — its fast-forward and background checkout-pull lanes are deliberately NOT triggers (checkout-pull: "additive in the common case; revisit with workspace groups"). Two new ops: `RestoreBranchVersion{Branch, Ref}` (default TreeWrite) — snapshots the target branch first, then: current branch → `git reset --hard` (a dirty tree forks a `restore-dirty` proceed/cancel decision); a branch checked out in ANOTHER worktree → typed refusal naming it; any other branch (including a DELETED one) → `git.UpdateRef` on `refs/heads/<Branch>`, which is how a deleted branch's history comes back. `DeleteBranchVersion{Ref}` (`LockMode()` RefWrite) — removes one version ref via `DeleteRef`, refusing any ref outside `refs/gg/versions/` so a frontend bug can never delete a real branch/tag through it. **Push fetch-refspec mapping:** `ensureRemoteTracking(ctx, deps, remote, branch, res)` (`fetch_mapping.go`) runs at all three `Push` success exits (`ops_basic.go`, right before `Done`): after a push lands, if the pushed branch's local tip doesn't match `refs/remotes/<remote>/<branch>` (the remote-tracking ref never moved — a single-branch/`--depth` clone whose fetch refspec doesn't cover the branch), it forks `FetchMappingDecisionID = "fetch_mapping.add"` (options `["add","skip"]`, safe default skip); `"add"` writes ONE per-branch mapping `+refs/heads/<branch>:refs/remotes/<remote>/<branch>` at local scope (never the wildcard — would risk a mass download on a monorepo remote) via the new `git.ConfigAdd` (idempotent via `git.ConfigGetAll`), then fetches just that branch via `git.FetchBranches`; every failure path (decider error, config write, fetch) only appends a summary note — a push that already succeeded is never failed retroactively. New op `AddFetchMappings{Remote, Branches}` (`LockMode()` RefWrite) is the notification center's batch fix: maps + fetches every named branch in one call (skipping already-mapped ones); empty `Branches` is a no-op; backs the `narrow_fetch_refspec` notice's "Add mappings + fetch these branches" action. **Stale fetch-mapping heal:** the flip side — a per-branch mapping whose branch was later DELETED on the remote makes every plain `git fetch` exit 128 (`fatal: couldn't find remote ref refs/heads/<b>`; git reports only the FIRST missing ref per fetch, and only exact refspecs are fatal — wildcards match-nothing silently), blocking pulls of unrelated branches. `healStaleFetchMappings` (`fetch_heal.go`), called by `SmartPull.pullCurrent` and the `Fetch` op when their plain fetch fails (the explicit-refspec fetches — `FastForwardRef`, `Pull`, `PullInWorktree`, `FetchBranches` — bypass configured refspecs and can't hit this), parses the fatal line, confirms the branch is an exact `refs/heads/<b>` source in `remote.<r>.fetch` (`ConfigGetRegexp`; remote hint narrows to one remote, "" scans all — the fetch-all case), verifies via `ListRemoteHeads` which mapped branches are really gone, and forks `StaleFetchMappingDecisionID = "fetch_mapping.stale"` ONCE for all of them (options `["remove-and-retry","abort"]`): remove unsets each stale value via `git.ConfigUnsetValue` (`--unset --fixed-value`, sibling refspecs survive), best-effort-deletes the dangling `refs/remotes/<r>/<b>`, and retries the fetch once. Any other outcome — unparseable error, no matching mapping, ls-remote failure, abort, or a decider error (the background auto-refresh lane fetches with an empty `MapDecider`; piped CLI) — returns the ORIGINAL fetch error: config is never mutated unseen. CLI: `gg pull --on-stale-mapping remove|abort`; `gg remote fetch` got an interactive decider for this fork.. **Push destination is explicit:** `git.Push` pushes `<branch>:refs/heads/<branch>`, never the bare `<branch>` refspec — git resolves a one-sided refspec's destination through `push.default` and `remote.<name>.push` (`map_refspec` in builtin/push.c), so `push.default = upstream` pushed a branch tracking `origin/main` ONTO main and a `remote.origin.push` wildcard redirected it, both silently, while gg's UI only ever says "push `<branch>`". Tags (`refs/tags/<name>`, explicit) and `--delete` are not remapped (verified) and stay as they were. **Rejected-push diagnosis:** `diagnoseRejection` (`push_diagnose.go`) runs on a non-fast-forward rejection BEFORE the `push-rejected` fork, because git reports the same rejection for two opposite situations — "the remote gained commits" and "I rewrote my own history, so the remote holds the OLD copies of my commits" — and they want opposite answers (a `pull --rebase` onto stale copies drops the rewritten commits as patch duplicates, restores the published ones and replays the remote's commits on top, undoing the rewrite). It never fetches: `git.RemoteBranchTip` (`ls-remote --heads`, exact ref-name match — ls-remote patterns match on component boundaries) names the remote tip; a tip whose object we don't hold is proof of real remote work (`divRemoteNew`); a tip we DO hold is checked against the branch's own reflog first (`git.BranchReflogContains`, `rev-list -g` — the branch itself pointed there before ⇒ `divLocalRewrite` even when conflict resolution changed every replayed diff; a missing reflog answers false and falls through), then counted with `git.CountRange` (`rev-list --count <refs/heads/branch>..<tip>`) and `git.CountRangeUnique` (`--cherry-pick --right-only`) — zero patch-unique remote commits ⇒ `divLocalRewrite`, all ⇒ `divRemoteNew`, some ⇒ `divMixed`. Every failure — and a zero total, which means the rejection came from a ref other than `refs/heads/<branch>` (a `push.default=upstream` / `remote.*.push` mapping rewrote the destination) — degrades to `divUnknown` and the pre-existing prompt. Only the prompt PROSE and the option ORDER change (`force` leads a local rewrite; `rebase` still leads a mixed remote, with a warning); option VALUES are untouched, so `gg push --on-reject …`, the MCP `MapDecider` and the web parking modal keep working unchanged. **Stale git locks:** `RemoveGitLocks{Paths}` (`remove_git_locks.go`) deletes stranded git lockfiles — the recovery half of the SIGKILL-strands-index.lock fix (see `gitexec`). Writes via `os` directly (the `ExportToDir`/`ExportFile` outside-the-tree precedent) and takes the DEFAULT TreeWrite reservation on purpose: fully exclusive is what guarantees no gg operation is running git while a lock is removed. `checkLockPath` refuses anything that isn't an absolute path with a known lockfile name (`git.IsLockFilePath`) sitting DIRECTLY inside `GitDir`/`GitCommonDir` (both added to the `GitOps` interface for this) — the `DeleteBranchVersion` refuse-outside-the-namespace precedent, so a frontend bug can't become an arbitrary file delete; the whole batch is validated BEFORE the first removal, and an already-gone lock is success (someone finished and cleaned up), not an error. The op deliberately does NOT judge staleness — gg can't see git processes it didn't start, so the frontend shows each lock's age and the human decides. **Cherry-pick:** `CherryPick{Commits []string}` applies the commits (pass oldest first) as ONE `git cherry-pick` sequencer run; a conflict forks `cherry-pick-conflict` (`keep-conflicts` leaves the paused sequence for the resume lane, `abort` rewinds the WHOLE batch — all-or-nothing); an already-applied commit auto-aborts with a legible error for a single-commit op but is `--skip`ped (new `CherryPickSkip` verb, loop bounded by len(Commits)) for a multi-commit op, with the skip count in the summary. The TUI Commits-menu row targets the ◉ marked set (oldest→newest by feed rank) whenever any valid mark exists, else the cursor commit (`cherryPickTargets`); WIP rows and merge commits are refused at dispatch. **Fast-forward:** `FastForward{Commit, Branch}` — advances a branch to `Commit` when it is strictly ahead (`IsAncestor` guard; equal tips = unchanged no-op). Empty `Branch` = the current branch (the Commits-panel/CLI lane) via `MergeFFOnly`; a named non-current `Branch` (the branch-pair lane) moves the ref via `FastForwardToRef` (`git fetch . <sha>:refs/heads/<b>` — ff-only, no working tree touched; git itself refuses a branch checked out in another worktree and that refusal is the op error). Decision-free. The pair frontends probe direction first via `domain.FastForwardPair(a,b)` → `FFPair{Behind, Ahead, OK}` (two RevParse + up to two IsAncestor under one Read query; OK=false for equal/diverged/unrelated). **Recycle a worktree:** `RecycleWorktree{Dir, Branch, Now}` checks a local branch out in ANOTHER worktree (the first op to act outside the current one): it takes a `-C <dir>` `GitOps` view from `OpDeps.RepoAt` (nil ⇒ `ErrNoRepoAt`; `domain.Execute` wires `git.Repo.InDir`), refuses up front a bare/unknown dir, the worktree gg runs in, a branch checked out anywhere, a paused op (`git.PausedOpIn` on the target's git dir) and a lock file (`git.LockFiles`), then — when staged+unstaged+conflicted+untracked > 0 (NOT `IsDirty`, which ignores untracked) — raises `RecycleDirtyDecisionID = "recycle.dirty"` (`commit`: `add -A` + commit `RecycleCommitMessage(now)` on the leaving branch; `discard`: `reset --hard` + clean on `:/` without `-x` — NOT `Discard{All}`, whose `restore --worktree` keeps staged hunks the switch would carry over; `shelve`: `add -A`, then `OpDeps.ShelveStaged(ctx, dir, oldBranch)` — domain's `shelveStagedIn` lists `diff --cached --name-status -M HEAD` and reads `show :<path>` through `repo.InDir(dir)` DIRECTLY (a domain query would take a Read reservation under the op's TreeWrite and deadlock), stores ONE files entry `WIP on <branch>` (origin = target, staged), and writes a shelf-level note (source agent, author gg) listing deletions and `old → new` renames; an all-deletions change stores one empty `delete.me` member; a failed note removes the entry; only then the same discard as `discard`; anything else cancels with no change), and finally `git switch`. The `recycle.dirty` prompt carries an overview of the dirt as its third arg (`recycleDirtyOverview`: up to 10 rows `XY path` in git's status order, `.` for an unchanged side — never a space, both modals collapse space runs — `??` untracked, `UU` conflicted, `old → new` renames, then `… and N more`); built from the status the op already read, untranslated. The TUI modal alone special-cases these rows (`recycleOverviewRow` in `view.go`): a row wider than the modal has its path cut by `elidePath` instead of word-wrapped. TreeWrite by default; the gate is per common dir so it covers the other worktree. Summary `recycled <old> → <branch> in <dir>[; committed <sha>|; shelved as "WIP on <old>"|; changes discarded]` — the pair leads so a status-bar end-cut loses the path, not the branches. `RemoteRef` ("origin/foo") makes it recycle onto a remote branch: after the target's own refusals (so a refused recycle never creates a branch) it runs `SmartCheckout{RemoteRef, Local: Branch, Intent: Stay}` INLINE (shared deps, no nested reservation) — create tracking / fast-forward / `CheckoutDivergedError` — and only then the dirty prompt; an abort there keeps the branch (`recycle cancelled; checked out <ref> as <b>`, Changed); success reads `recycled <old> → <b> from <ref> in <dir>` (the remote before the path, so a status-bar end-cut keeps it). TUI: the Remotes-tab row arms `pendingCheckout{…, recycleDir}` so the diverged modal's rename popup re-dispatches the recycle (verb "recycle"); CLI: `resolveRecycleBranch` — local name wins, else a `RemoteBranches` match (`--as` renames). **Pull over dirt:** `SmartPull` runs every pull (current ff-only, the diverged question's rebase/merge, `PullInWorktree` through a `RepoAt` view) via `pullClearingDirt` (`pull_dirty.go`): only when `git.IsLocalChangesRefusal(err)` (git's English "would be overwritten by" / "cannot pull with rebase") does it raise `PullDirtyDecisionID = "pull.dirty"` (`shelve`/`discard`/`abort`, the recycle prompt's overview as arg 3, answered with recycle's `StageAll`+`ShelveStaged`+`discardAll` / `discardAll`), then retry once; abort = `pull cancelled` (unchanged, no error). `pullDirt.settled` stops an unanswered or failed question from falling through to the `non-fast-forward` question. The TUI's `recycleOverviewRow` elision covers both ids; CLI `gg pull --on-dirty`. |
| `domain`     | Frontend-facing command + query layer: `Execute` runs an operation under its repo-gate reservation; `Snapshot`/`Status`/`Worktrees`/`ShowFile`/`CommitFiles`/`TopLevel`/`CurrentBranch`/`GitCommonDir` run reads under a Read reservation, parallel and singleflight-coalesced; `CommitFeed` is the paged commit-history read-model backing the Commits panel (carries a `LogScope` — branch selection plus path/author/message/date filters (any filter axis suppresses the TUI commit-graph); supersede-cancels a superseded in-flight walk; `model.Commit.Refs` holds `%D` ref decorations). Also hosts the `Differ` (plain/enhanced, plain/cached decorator over lazy byte-sources) serving commit diffs from the cache. `Status`/`Snapshot` run an **EOL-only reconcile** (`statusFiltered`/`dropEOLOnly` + the `git.ModifiedIgnoringEOL` verb): a tracked file whose only unstaged change is line endings (CRLF↔LF) is dropped from the modified set unless `SetShowEOLOnlyChanges(true)` (from `[ui] show_eol_only_changes`). Emits the op span. Backs the TUI's per-source refresh registry: each source calls a domain query and `domain` exposes two registry-specific additions — `Conflict(ctx, st)` (derives conflict state from a status the caller already read, avoiding a second round-trip; its git probes run under their own Read reservation — taken at the `Conflict` entry point, NOT inside `conflictState`, whose other caller `loadSnapshot` already holds one — and a clean status returns without touching the gate). It now also reports a paused sequencer op (merge/rebase/cherry-pick/revert) even with zero conflicted files — the conflicted-path probes are unchanged (extracted into `attributeOp`), but a clean `Counts().Conflicted == 0` status falls back to a stat-level `git.PausedOpIn` probe over the worktree's git dir, resolved and cached once per `Service` (`gitDirCached`/`cachedGitDir`); the clean steady state (no paused op, git dir already cached) still costs zero git invocations and no gate acquisition, preserving the "clean status is free" contract. `CommitTimes(ctx, shas)` (gated head-time lookup so the TUI worktrees source needn't import `internal/git` directly). Also `RemoteTagsFresh(ctx) (map[string]bool, error)` — a non-coalescing (no singleflight) remote-tag lookup so the caller's context (e.g. a 5s timeout) fully governs it; records NO failure on error/timeout (a cancelled `P` must not spam `errors.log`). `CommitPatch(ctx, sha) ([]byte, string, error)` / `FilePatch(ctx, sha, path) ([]byte, string, error)` return a `git format-patch`-format patch (whole commit / one file's change within it) plus a default file name (`<shortsha>.patch` / `<shortsha>-<basename>.patch`); both refuse a merge commit up front via `ErrMergeCommitPatch` — `git format-patch -1` doesn't error on a merge, it silently emits a *different* commit's patch, so the parent count (`git.ParentCount`) is checked before calling `git.FormatPatch`. `ExportDefaultDir(ctx) (string, error)` is the default patch-export directory: the parent of the MAIN worktree root (mirrors `TempExportBase`'s main-worktree anchor). `RepoHealth(ctx)` — cheap health snapshot (pack bytes via one ReadDir, commit-graph presence via stat, `fetch.writeCommitGraph` at both explicit scopes) behind the TUI notification center; GitCommonDir rides along as the per-repo dismissal key. `GitConfigRows(ctx)` — the explorer read-model: one row per catalog key ⋈ set local/global values (case-insensitive join — git lowercases set keys), non-catalog set keys appended. `ConflictFileVersions(ctx, path, hasBase)` — materializes a conflicted path's index stages (`:2:`/`:1:`/`:3:`) into a LOCAL/BASE/REMOTE temp-file trio for a per-file external mergetool (an empty temp file when `hasBase` is false, matching git-mergetool behavior; `<merged>` is the real worktree file, never materialized), returning a `cleanup` the caller runs after the tool exits. `CompareOrigins(ctx, a, b)` — per-branch changed-path sets from the merge base (`MergeBase` + two `M..tip` numstat path lists; `ErrNoMergeBase` when histories are unrelated), backing the TUI branch-compare origin filter. `ReviewReport(ctx, target ReviewTarget, agent, resolvedCommand string, env []string) (ReviewResult, error)` runs `engine.ReviewChanges`; an empty captured report is a hard error; a commit/range/branch review is stored as a review note through `SaveReview` (see "Review notes" below) and returned as `ReviewResult{NoteID, Warn, Content, Range, Label}` — a working-changes review is returned but not stored. `ReviewTarget{Kind, Range, Label, Diff model.DiffSpec}` is the small value the TUI/CLI build (`Kind` one of `ReviewBranch`/`ReviewRange`/`ReviewWorking`): `Range` is the **injection-safe** value spliced into the tool's `<range>` token / `DiffSpec.Rev` (hex SHAs, or a user-typed rev), `Label` is the human display string (branch name / commit title / typed range / `working changes`) used ONLY for the status bar, viewer title, report filename, and the agent's `# Range:` context header — never executed. `ReviewTarget.DisplayLabel()` is the `Label → Range → "working changes"` fallback (a site that forgets `Label` degrades to the visible hex range, not a silent mislabel); `BranchReviewTarget(ctx, tip)` resolves a branch's target — `git.MergeBase(ctx, "main", tip)`, falling back to `git.UpstreamRef(ctx, tip)`, falling back to the tip's own change (`tip^..tip`) when neither exists. `CompleteConflictReport(ctx, commandTemplate string, env []string) (CompleteConflictResult, error)` is the web AI conflict lane's wrapper around `engine.CompleteConflict` — a `ReviewReport` sibling that refuses up front (`"no paused operation to complete"`) when `Conflict(ctx, st).Op == ""`, else gathers the conflicted paths via `model.WorkingTreeStatus.Conflicts()` (`unmergedPaths`, the `web` package's own `unmergedStatusPaths` copy — `web` can't reach this unexported helper directly), dispatches the op, unwraps a JSON-enveloped stdout via `exttool.ParseCaptureReport` (plain text passes through unchanged — the `ReviewReport` convention), and re-probes status afterward to report `StillPaused` (a stop-early agent that only advanced a rebase round). `CompleteConflictResult{Overview, Op, StillPaused}` is display-only — nothing is persisted, unlike `ReviewReport`'s file — and an empty `Overview` is not an error (TUI parity: "reported no overview"). `BranchVersions(ctx, branch) ([]model.BranchVersion, error)` — one `ForEachRef` under `refs/gg/versions/<branch>`, parsed via `git.ParseVersionRef` (a prefix-match ref belonging to a DIFFERENT branch is filtered out, since the prefix alone can over-catch a nested name), newest first. `AllVersionBranches(ctx) ([]model.VersionedBranch, error)` — one `ForEachRef` over the whole `refs/gg/versions/` namespace, grouped by branch (count + latest timestamp), cross-referenced against the live branch list to mark `Deleted` — the entry point for the TUI's deleted-branch recovery picker. `SetVersionsPolicy(p engine.VersionsPolicy) *Service` stores the policy (`atomic.Value`; unset defaults to enabled/90 days, the `SetShowEOLOnlyChanges` precedent) that `Execute` injects into `OpDeps.Versions` on every dispatch — the TUI calls it at both config-arrival paths (bootstrap + reRoot) and from the Settings "Operations history" editor after a live change. `RepoHealth` also reports `UnmappedBranches []string` — local branches with a configured upstream (`branch.<n>.remote` is exactly `"origin"`, `.merge` is set, and `origin` has some fetch refspec) that nonetheless resolves to no tracking ref, via one `git.ConfigGetRegexp` cross-referenced against the live branch list (`unmappedFromConfig`, `repohealth.go`) — detection is scoped to `origin` on purpose, matching the notice's fix action, which only ever writes/fetches `origin` — the narrowed-fetch-refspec state a push alone can't detect ahead of time; feeds the `narrow_fetch_refspec` notice.. `StaleLocks(ctx)` (`lockerror.go`) is the stat-level stale-lock query (`git.LockFiles` over `gitDirCached` + the common dir) behind `gg unlock`; `RepoHealth` inlines the same call rather than delegating (it already holds a Read reservation, and `StaleLocks` would take a second). `domain.IsLockError(err)` re-exports `git.IsLockError` because `internal/tui`/`internal/cli` may not import `internal/git` (archtest) — the same re-export role `Conflict` plays for the conflict probes. **Compare two commit entries (bookmarks/shelved commits):** `ResolveCommitEntryEndpoint(ctx, sha, shelfID)` turns one side of a commit-entry comparison into a hybrid `model.Endpoint` — the live sha (`EndpointCommit`) while it resolves via `CommitLookup` (which serves only as the existence probe; its short-sha result must never leak into the endpoint), the frozen tar (`EndpointShelf`, shelf side only) once a gc'd sha is gone, and a `CommitGoneError` for a bookmark with no shelf fallback (a bookmark stores no blobs). Resolution is strictly per side, so mixed states compose (a shelf↔shelf pair with one gc'd sha becomes frozen↔live, landing in the shelf↔commit lane). `CompareFiles`/`ComparePatch` grew a shelf-aware lane: any pair with an `EndpointShelf` side is diffed WITHOUT git — `shelfCompareFiles` dispatches to `shelfShelfCompare` (both sides frozen: two `ShelfCommitFiles` member lists ⋈'d, differing status via `ResolveBytes`-compared bytes) or `shelfCommitCompare` (one side frozen: the shelf's own members compared against `TreeFiles`/`ShowFile` on the live commit, scoped to the shelf's changed-file set — the frozen tar cannot speak for paths the shelved commit never touched; direction (`shelfIsRight`) decides whether a missing member reads `A` or `D`). `ComparePatch`'s frozen lane materializes each differing file pair into temp files and runs `DiffNoIndex`, relabelling headers via the shared `RelabelNoIndexDiff` (`compare_entries.go`, also used by `mcp`'s `gg_compare_file`, which now calls it instead of its own private copy) — a binary pair (a NUL byte or invalid UTF-8 on either side) is rendered as a `Binary files a/<path> and b/<path> differ` line directly, never diffed (git diff --no-index would otherwise print the temp paths, since a binary pair has no `@@` hunk to flip the relabeller's header latch); per the "unreadable/missing shelf blob surfaces as-is" rule, a resolve failure on a side the file list says IS present propagates to the caller — only a side an `A`/`D` status says is genuinely missing is left unresolved. Plan 1b added the **compare algebra** — `FileSet` (bounded key set vs unbounded point), `EvalEndpoint`/`EvalLink`, `CompareSets` over the 2×2, `LocateLink`/`SameCheckout` carved out of `ResolveLink` — which replaced the CLI's deleted `validComparePair`; the lanes, the rulings and `ComparePatch`'s remaining gap have their own section below. |
| `repogate`   | Per-repo reservation gate (Read/RefWrite/TreeWrite, writer-preferring FIFO, escalation), process-global registry keyed by git common dir. |
| `git`        | Thin git verbs on `*git.Repo` (status, branches, worktrees, sync, stash, …). One verb ≈ one git invocation. `ReadWorktreeFile`/`WriteWorktreeFile` (plain fs I/O, not git) reject any path resolving outside the working tree (`worktreePath` — `filepath.Join` Cleans `../` upward instead of rejecting it, and the destination can be raw user input from `bookmark paste`/`shelf restore`/TUI text fields). `ArchiveFiles(ctx, rev, paths)` — `git archive --format=tar <rev> -- <paths>`, returning the tar bytes; backs shelf-a-commit and commit-bookmark export (no working-tree checkout needed). `FormatPatch(ctx, rev, paths...)` — `git format-patch -1 --binary --stdout <rev> [-- <paths>]`, returning the mailbox-format patch bytes (whole commit, or scoped to paths while keeping the commit's From/Subject header); backs export-as-patch. `ParentCount(ctx, rev)` — `git rev-list --parents --max-count=1 <rev>` parent-count, used to refuse a merge commit before calling `FormatPatch` (`-1` on a merge silently emits a different commit's patch, not an error) and, a second caller, `engine.CreateWorktree`'s keep-modes pre-check (refuses a root/merge `StartPoint` before anything is created). `ResetInDir(ctx, dir, ref, soft)` — `git -C <dir> reset --soft|--mixed <ref>`, a reset scoped to ANOTHER worktree's checkout (the `ShowFileInDir` `-C` precedent); backs `engine.CreateWorktree`'s keep-changes modes, run right after `AddWorktree` to land the new branch on the commit's parent with its diff staged/unstaged. `ConfigKeys` (the `git help -c` catalog), `ConfigListScoped` (`git config --list --show-scope -z`; local/global only; -z survives multiline values), `ConfigUnset` (`--unset-all`, not plain `--unset` — a multivar key like `safe.directory` would otherwise exit 5 without removing anything and get reported as a successful unset; exit 5 now unambiguously means "key was not set" = no-op). `LogLines`/`CommitLine` (`git log --format=%h%x1f%s`) back terse `<short-sha> <subject>` history reads. `DiffNumstat`/`DiffPatch` (`git diff --numstat -z` / `git diff`, both taking a `model.DiffSpec`: working tree, `--cached`, or a rev/range, optionally path-scoped) back `gg diff`'s stat and patch modes. `ShowNumstat`/`ShowPatch` are the `git show` analogs for a single commit. `StageAll` (`git add -A`), alongside the existing `StagePaths`/`UnstagePaths`, backs `Stage{All}`. `PruneWorktrees` (`git worktree prune`) backs the engine op of the same name. `PausedOpIn(gitDir)` — a stat-level sequencer probe (checks `MERGE_HEAD`/`rebase-merge`/`rebase-apply/rebasing`/`CHERRY_PICK_HEAD`/`REVERT_HEAD` via `os.Stat`, no git invocation) reporting which op (if any) is paused independent of the conflicted-file count, so an op whose conflicts were resolved outside gg but never continued is still detected; backs `domain.Conflict`'s paused-op fallback. Also fixed: `ParseStatusV2`'s untracked branch now sets `Staged`/`Unstaged` to `?` (was left zero-valued, rendering as blank instead of git's `??` in `gg status`); the ignored branch stays zero-valued (git status never runs with `--ignored`, and setting `!` there would double-count in `model.Counts()`). `MergeBase(ctx, a, b)` (`git merge-base <a> <b>`) and `UpstreamRef(ctx, ref)` (`git rev-parse --abbrev-ref <ref>@{upstream}`) back `domain.BranchReviewTarget`'s base resolution; either errors when there's no common ancestor / no configured upstream, which the caller treats as "try the next fallback". `IsMailboxPatch(data []byte) bool` — pure sniff of a patch file's head: first non-empty line starts with `From ` (git mailsplit's From_ sentinel, what a `git format-patch` mailbox starts with; a plain `git diff` starts `diff --git`/`---`); no git invocation. `ApplyPatch(ctx, path, threeWay)` (`git apply [--3way] <path>`, no `--index`/`--cached` so changes land unstaged) applies a unified diff to the working tree; with `threeWay` a missed hunk falls back to a 3-way merge and may leave conflict markers + unmerged index entries — git exits non-zero for BOTH that case and applied-nothing, so callers (`engine.ApplyPatch`) probe status to tell them apart. `PatchPaths(ctx, path)` (`git apply --numstat -z <path>`) reads back the paths a patch touches — the patch file only, no working-tree/index effect — backing `engine.ApplyPatch`'s post-fallback surgical unstage; for a rename it reports only the new path (verified against git 2.43: `git apply --numstat -z`, unlike `git diff --numstat -z`, does not emit the empty-path/old/new rename record shape), a documented gap in its sole caller. `AmMailbox(ctx, path, threeWay)` (`git am [--3way] <path>`) replays a mailbox as commits preserving author/date/message. `AmAbort(ctx)` (`git am --abort`) rolls back an in-progress am. `AmInProgress(ctx)` is a stat-level probe for `rebase-apply/applying` — the am-specific marker, distinct from a paused rebase's `rebase-apply/rebasing` on the same apply backend — deliberately kept OUT of `PausedOpIn` (gg does not model a paused `git am`; `engine.ApplyPatch` keeps it atomic instead). `DiffNoIndex(ctx, a, b)` (`git diff --no-index -- a b`; exit 1 = "differs", mapped to success — the ConfigUnset exit-5 pattern) backs the MCP per-file compare. `WorktreeRepair(ctx, path)` (`git worktree repair <path>`) rebinds a moved or cross-environment worktree's admin gitdir file + `.git` back-link; backs `engine.RepairWorktree`. `UpdateRef(ctx, ref, sha)` (`git update-ref <ref> <sha>`, creates if missing), `DeleteRef(ctx, ref)` (`git update-ref -d <ref>`), and `ForEachRef(ctx, prefix)` (`git for-each-ref --format=%(refname)%00%(objectname)%00%(subject) <prefix>`, one invocation, NUL-separated so a subject can't corrupt the parse) back branch-version snapshots (`engine.snapshotBranchTip`) and their domain queries — `internal/git/versionref.go`'s `VersionRefPrefix = "refs/gg/versions/"`, `VersionRef(branch, opToken, unix)` (builds the ref name), and `ParseVersionRef(ref)` (splits back to branch/opToken/unix, parsing from the END — the last `/`-segment is always `<ts>-<op>`, so a branch name whose own last segment looks like `123-fix` still parses unambiguously) are the shared naming/parsing pair used by both the engine writer and the domain reader. `LogScoped`'s `git log` invocation now carries `--decorate-refs-exclude=refs/gg/*` so a version snapshot ref never leaks into a commit's `%D` decorations (e.g. the old tip still reachable after a merge). `ConfigAdd(ctx, scope, key, value)` (`git config <scope> --add <key> <value>`) appends a multivar value without clobbering existing ones — backs the fetch-refspec mapping write. `ConfigGetAll(ctx, key)` (`git config --get-all <key>`) reads every value of a multivar key, `nil` on unset (exit 1, the `ConfigGet` pattern) — the idempotency check before `ConfigAdd`. `ConfigGetRegexp(ctx, pattern)` (`git config -z --get-regexp <pattern>`) lists every matching key/value pair (`-z` framing survives multiline values), `nil` when nothing matches (exit 1 → empty, same pattern) — backs `domain.RepoHealth`'s unmapped-branch scan. `FetchBranches(ctx, remote, branches)` (`git fetch --no-write-fetch-head <remote> <branches...>`) fetches only the named branches — the near-free per-branch fetch behind `engine.ensureRemoteTracking`/`AddFetchMappings`, deliberately narrower than a full `git fetch`.. `lockfile.go` is the stale-lock layer (no git invocation): `LockFiles(dirs...)` stats a FIXED candidate list (`index.lock`/`HEAD.lock`/`FETCH_HEAD.lock`/`ORIG_HEAD.lock`/`shallow.lock`/`packed-refs.lock`/`config.lock`) across the dirs given — callers pass the worktree git dir AND the common dir, since `index.lock`/`HEAD.lock` are per-worktree while `packed-refs.lock`/`config.lock` are shared — returning `[]model.GitLock` (path/name/mtime), deduped; the fixed list is deliberate (the `PausedOpIn` stat-probe precedent), since walking `refs/` for `*.lock` is unbounded on a monorepo and this runs on every repo load. `IsLockFilePath(p)` gates what `engine.RemoveGitLocks` may delete; `IsLockError(err)` classifies git's "Another git process seems to be running" / "Unable to create … .lock: File exists" failure by message (no exit code distinguishes it). Note `git status` takes `index.lock` too — it rewrites the index to refresh the stat cache, which is why a killed background status was the common stranding path. |
| `rebaseplan` | Pure interactive-rebase plan model + todo/message rendering (no git, no os/exec — the gg-as-editor subcommands in `cmd/gg` do the I/O). `ShellPath(p, goos)` / `SequenceEditor(ggBin, planPath, goos)` quote a path for **the shell git itself runs**, which is NOT the platform shell: on Windows git executes `GIT_SEQUENCE_EDITOR` and every `exec` todo line through its own bundled POSIX sh. So the quoting is POSIX everywhere (single quotes + the `'\''` dance — `internal/template`'s `quoteArgFor`, which quotes for the USER's shell, would be wrong here), and a Windows path's backslashes are converted to forward slashes BEFORE quoting: unquoted they were eaten as escapes (`t:\others\gg.exe` → `t:othersgg.exe`, "command not found"), and single-quoting them instead hands sh a literal backslash path it will not resolve. The conversion is Windows-only because a backslash is a legal POSIX filename character. `RewriteTodo` takes the same `goos`. The same defect broke any path containing a SPACE on every platform, which is what `TestInteractiveRebaseGGBinWithSpace` exercises with a real git. |
| `gitcmd`     | Fluent argv builder (`New("sub").Arg(...).ArgIf(cond, ...).ToArgv()`). |
| `gitexec`    | The `Runner` interface (`Run`/`Stream`), the real `ExecRunner`, and `FakeRunner` for tests. **Cancellation terminates gracefully:** `ExecRunner.prepare` (shared by `RunEnv`/`Stream`) sets `cmd.Cancel` so a cancelled context sends SIGTERM via `terminate` (`signal_unix.go` / `signal_windows.go`) instead of Go's default `Process.Kill()`. Git releases its lockfiles from a `sigchain` handler that SIGKILL cannot run, so the old default stranded `.git/index.lock` on every cancelled op — and gg cancels git on EVERY user action (`tui.startOp` preempts the background refresh lane), which is why a keypress during a slow `git status`'s index writeback broke the repo until the user deleted the lock by hand. `WaitDelay` now doubles as the grace bound: a git that ignores SIGTERM is still hard-killed, so a cancelled read can't hold the repo gate. Windows has no SIGTERM (`os.Process.Signal` supports only Kill), so `terminate` there IS a kill and locks can still leak — recovery is `git.LockFiles`/`engine.RemoveGitLocks`. `LimitRunner` bounds concurrent git subprocesses; it now honours `ctx` while acquiring the semaphore (bug #4 fixed — a cancelled background read releases the slot immediately rather than waiting). `Stream` streams stdout through a line-splitting `io.Writer` (`cmd.Stdout = lineWriter`) rather than `StdoutPipe()`+a reader goroutine, so os/exec's own copier drains under `cmd.Wait()` — no race where `Wait` closes the pipe under a mid-read reader (the flaky `git worktree add` "read |0: file already closed"); `WaitDelay` still force-closes a grandchild-held pipe. |
| `gitwatch`   | Pure fsnotify wrapper + `.git`-layout path→source map (`Plan`) + drvfs `Supported` gate + debounced `Watcher`; no git/TUI/domain imports. Backs event-driven auto-refresh (Phase D). TUI maps `gitwatch.Source`→`sourceKey` and feeds `enqueueDue`. Watches worktrees, reflog, branches, remotes (recursive ref-tree watching for branches/remotes). |
| `gitconfdocs` | Pure curated slice of git's config catalog (~64 keys: real default, one-line description, bool/enum/string/int kind) behind the git-config explorer's editable rows; `Lookup` is case-insensitive (git lowercases set keys). A staleness test asserts every curated key still exists in `git help -c` (skipped without git). Archtest DAG leaf; the TUI imports it directly like `commitgraph`/`textdiff`. |
| `i18n`       | TUI translation layer: English-text-as-key lookup over TOML bundles (`T(key, args...)`; miss → the key itself = English fallback). Embedded ja/ko/zh/ru bundles + custom/overlay files in `$XDG_CONFIG_HOME/gg/lang/<code>.toml` (`config.LangDir()`); per-key verb-mismatch rejection at load (fail-soft); process-global `atomic.Pointer` catalog set by `SetLanguage` at both TUI config-arrival paths and the Settings Language picker (persists via `SetGlobalUILanguage` to the GLOBAL config). `CheckVerbs` (`verbs.go`) is the load-time guard: each `*` dynamic width/precision counts as its own consumed-argument entry in the multiset comparison (not skipped), and an explicit `%[n]` index is range-checked against the key's argument count, with a malformed bracket rejected via a sentinel verb that can never match — stricter than a naive verb-letter diff. `SetLanguage` surfaces an existing-but-unreadable custom bundle file (e.g. EACCES) as a real error rather than treating it as not-found (`Available()` stays lenient — a probe, not a load). Archtest DAG leaf. Enforcement lives in `internal/tui/i18n_scan_test.go`: a go/ast scan requires every `i18n.T` key to be a string literal and every catalog key to exist in all four embedded bundles (orphan + coverage + verb checks) — footer/settings registries became funcs (`contextBindings()`/`globalBindings()`, `settingsMenuTitle`) so labels re-evaluate on language switch. CLI/engine prose and decision option values stay English by design. `internal/tui/i18n_display.go` holds the translate-at-render display funcs (`opDisplayName`, `describeConflict`, `conflictNotice`, `pausedNotice`, `sourceDisplayName`) that sit between i18n and the domain/engine layer: op and source VALUES stay English protocol (state, config keys, comparisons), only the rendered sentence is localized — status segments, the resume-paused-op prompt, footer-override modes, and the conflict-process indicator all route through here. Footer hint strips are the one sanctioned fragment-concatenation exception (list-like, not sentences — conditional hint tokens like "  [i] msg" are their own keys); it must not generalize to prose. Multi-count messages use a two-key singular/plural convention (e.g. push-tip-tags) rather than one string with an embedded count; `ru` collapses these to a count-neutral colon form. The Settings Language picker shows a dim hint (via `config.FileUILanguage(path)`, a one-file `[ui] language` reader distinct from the layered `config.Load`) when the active repo config sets `[ui] language` itself, since a repo override wins over the global choice being picked. Stage 3 adds `optionDisplayName` (`i18n_display.go`) — the single render site translating decision-modal option LABELS, while `Options` list values, `onResolve`/decider comparisons, and the esc→`"abort"` mapping stay English protocol — enforced by `internal/tui/options_vocab_test.go`, a go/ast scan over engine+tui requiring every statically declared `Options: []string{…}` value to have an `optionDisplayName` case AND exist in all four bundles (caught a missing `"overwrite"` case on its first run). `i18n.ActiveTranslations()` exposes the active catalog's key→string map so `statusIsError` (`view.go`) can classify a TRANSLATED status message as an error: it derives translated error prefixes from the catalog's own `<prefix>: %s`-shaped keys (a verb-less key sharing the prefix, e.g. a footer label, is skipped so it can't false-positive), taking each translation's head up to its first `%`-verb — a translation that reorders its topic word past the verb yields a too-short head and is skipped (the message renders unstyled, never mis-styled), keeping the translated classification congruent with the English one. `padCell` (now living in `i18n_display.go`) is the one shared byte-vs-display-width-safe column padder, used by the Settings refresh-rates editor, the identity & profiles popup, and the repo-config-location popup. With decision-modal options, six more popups (identity & profiles, git-config explorer, notification center, repo-config location, review viewer chrome, hook editor), and ~110 more `statusMsg` call sites now translated, the TUI is fully translated except engine/CLI prose (English by design, the agent-facing surface). Stage 4 closes the remaining chrome: pair-op picker labels/footer, browse/switcher chrome (fuzzy file finder, files view, bookmark/shelf popups, command palette, repo popup), conflict process/picker and review-tool chooser, prefix/tool-approval/shell-escape/checkout-as/eager-search chrome, all 18 textfield-style popups, and ~100 action-menu row labels package-wide. `internal/tui/menu_labels_test.go` (`TestActionMenuLabelsTranslated`) is the enforcement gate alongside `options_vocab_test.go`: an AST scan requiring every `actionRow` composite literal's `label:` field, and every call-site argument at a same-package function/method's `label string` parameter position (catching prose reaching a row positionally through a helper with no `label:` token at the call site), to route through `i18n.T` with every key present in all four bundles. `maxLabelWidth` (`i18n_display.go`, alongside `padCell`) computes a label-column pad width from the *translated* label set rather than a fixed English-length floor, fixing width regressions in the git-config explorer, identity & profiles, and repo-config-location popups. Notices now rebuild on a language switch (previously built once with titles baked in). With stage 4, the TUI is fully translated; only engine/CLI prose stays English by design (the agent-facing surface). Stage 5 localizes the TUI's RENDERING of engine events (op status, progress, decision prompts) without moving the agent surface: the engine keeps emitting its English strings verbatim (CLI/log/e2e unchanged) and additionally carries a localizable `engine.Msg{Format, Args}` channel (see the `engine` entry), which the single render seam `internal/tui/i18n_engine.go` (`renderSummary`/`renderProgress`/`renderPrompt`) translates via `i18n.T(format, args...)`, falling back to the English string when the channel is empty. This is the ONE sanctioned non-literal-key `i18n.T` call site (`i18n_scan_test.go` allowlists exactly that file). `internal/tui/engine_prose_test.go` is the coverage+drift gate: `engineProseKeys` (a pure collector) feeds `TestEngineProseKeysInBundles` (every engine format/step literal must exist in all four bundles) and the `i18n_scan_test.go` used-key union; `TestEngineProseNoDynamic`/`TestEngineProseHelperOnly`/`TestEngineProseFloor` enforce that every engine sentence originates as a literal at a helper call site and that no engine code hand-builds `Summary:`/`Prompt:` strings. `options_vocab_test.go` learned the `PromptReq` pass-through (skips the forwarded `options` param, collects option values from `PromptReq` call-site args). ~200 keys ×4 (bundles ~1,385 each). Engine ERROR prose stays English (it renders inside the already-translated `friendlyOpError` frame). |
| `model`      | Shared plain data types (`Status`, `Branch`, `Worktree`, `Commit`, …). `model.FileAddress` (worktree/branch/commit/shelf-id/path + `FileState`) is the one shared address/display behind both a shelf entry's `Origin` and a bookmark's address (`Bookmark.Address()`), rendered by `FileAddress.Display()`. Also `model.RepoHealth` — the stat-level health snapshot behind the notification center (now carrying `StaleLocks []GitLock`). `model.GitLock{Path, Name, ModTime}` is one git lockfile found on disk; the mtime is the only staleness signal gg can offer, so it is what the UI renders. `Endpoint` (whole-tree compare, `EndpointWorkTree`/`EndpointIndex`/`EndpointCommit`/`EndpointShelf`) gained **`EndpointShelf`** — a shelved commit's frozen changed-file set, addressed by `ShelfID` (no `Hash`); `Display()` renders it `shelf #<id> (frozen)` (the compare-view title's "frozen" label), `FileRef(path)` maps it to `FileRef{Source: SourceShelf, Locator: ShelfID, Path: path}`, and it is NOT `IsLive()` (a frozen tar, like a commit, never changes on disk). Plan 1b added two kinds: **`EndpointRef`** (a branch or tag TIP, UNBOUNDED, carrying the NAME — `CacheTag()` PANICS on one by design, ruling R3, because a moving name must never become a cache key) and **`EndpointPair`** (a resolved `<a>..<b>` change-set, BOUNDED). Both are resolved away by `domain.EvalEndpoint`, the one place a ref is allowed to die. `Link` grew `Target.Ref`, `Target.Pair` and `Hint`. |
| `tui`        | Bubble Tea Elm-style UI (value-receiver `Model`, panels, modal Decider, async ops). A per-source refresh registry (`source.go`) drives actions, `r`, and startup: eight named sources (status, branches, remote branches, tags, reflog, worktrees, commit feed, identity) each load independently via their domain query and emit a `dataAvailableMsg`; an op→source mapping restricts which sources each action reloads; per-panel ⏳ spinners track in-flight loads. Repo-switch (`reRoot`) still uses the legacy `loadCmd`. `refresh.go` adds the **background auto-refresh scheduler**: a single-lane FIFO — `refreshTick` calls `dueItems` (which calls `scheduledInterval` per item — fixed configured seconds floored at `min_seconds`, no adaptive engine) to enqueue overdue items; `enqueueDue` deduplicates by type; the lane drains exactly one item at a time (`bgBusy`/`bgActiveItem`/`bgQueue`), so background reads never overlap. Manual `r` and background reads record their duration in a per-item rolling ring (`refreshDur`, capped at 10 samples) via `recordDuration`; the **app-start fan-out is excluded** (`dataAvailableMsg.startup` → not measured). Durations are **informational only** — they are displayed as avg stats in the "Refresh rates" editor and do not affect scheduling. The `fetch` and `remote_tags` rows are config-gated (network) and opt-in only (`[refresh] fetch = 0` → never runs); both are modeled as synthetic `refreshItem`s (`fetchItem`/`isFetch`, `remoteTagsItem`/`isRemoteTags`) so `reloadAll`/`r` never triggers a network call. Additionally, `remoteTagsItem` is **auto-enqueued on every `srcTags` arrival** (`dataAvailableMsg{source: srcTags}`) when `autoRemoteTagsEnabled()` (= `!cfg.Refresh.DisableRemoteTagsAuto`) and `len(m.tags) > 0`; this fires on app load and after any tag-mutating op, independent of `[refresh] enabled`. The Settings `,` menu includes an **"Auto remote-tag refresh"** toggle that calls `toggleAutoRemoteTags()` → `SetGlobalDisableRemoteTagsAuto` (mirrors `toggleAutoRefresh`). `bgRefreshHint` shows a `⟳ <source>…` status hint while the lane is busy (suppressed when the active item's rolling average is < 1s, so fast reads don't flicker). `startOp` cancels `bgCancel` so a user action preempts any in-flight background read immediately. The `[refresh] enabled` toggle (Settings `,`) calls `SetGlobalRefreshEnabled`. Settings → **"Refresh rates"** is an **inline editor** (`ratesView`/`ratesSel`/`ratesEditing`/`ratesField`): ↑/↓ selects a source row, enter opens a numeric text field, enter saves via `saveRefreshInterval` (calls `config.SetRefreshInterval` to write `[refresh] <source>` to the **repo `.gg.toml`**, updates in-memory config, reseeds `refreshLastRun`), `w` toggles file-watch mode for watch-capable sources via `toggleRefreshWatch` (calls `config.SetRefreshWatch`, rewires the watcher), esc cancels. **Phase D event-driven refresh** (`watch.go`): `watchActive` in `refresh.go` marks sources whose watcher is running so `dueItems` skips polling for them (watcher events drive `enqueueDue` directly instead); `watch.go` builds the `gitwatch.Watcher` (worktrees/reflog/branches/remotes), maps `gitwatch.Source`→`sourceKey`, and delivers `watchEventMsg` into the refresh lane on each filesystem event (`watchAffectedSources` expands a primary source to its consequences, mirroring `opAffectedSources`: branches→{branches,feed}, remotes→{remotes,feed,branches} (Upstream/Ahead/Behind), worktrees→{worktrees,branches,feed}, reflog→{reflog} — the feed reload is what makes the Commits panel's `%D` decorations/↓↑ tip markers update; reflog stays minimal because HEAD/ref moves are already caught by the branches watcher's `refs/heads`+`$W/HEAD` watch); disabled on WSL2 9p (`m.watchSupported=false` → interval fallback). The Refresh-rates editor has a labelled **file-watch** column with a `[x]`/`[ ]` checkbox on watch-capable rows (toggle `space`/`w`). **Commits-panel row rendering** (`commit_ident.go`, `view.go`): `commitIdent` carries `tags []string` and `count int`; `markerField()` renders the fixed 3-cell marker area — a superscript badge (`↓²`…`↓⁺`) fills the filler cell when ≥2 local tips (dropped when `↓↑` both present). `commitDecoGroup(id, budget)` is the single shared layout helper called by BOTH `commitIdentRowAt` (row string) and `commitDecoratorsRange` (yellow tag coloring) with the SAME `commitGroupBudget` — any divergence colors the wrong columns (the SYNC INVARIANT). `groupBase = identStart + identW`. Tags colored yellow (color 220) via `tagDecoStyle` through the existing single-pass `commitLineDecorator`; full group collapses to `(+N)` when `lipgloss.Width(group) > commitGroupBudget(boxW, identW)`; old `pills()` removed. `filter_memo.go` adds `commitFilterMemo` — the memoized filtered Commits display index (keyed by query/wipRows-content/feedLen/baseHash/sort, invalidated by `graphLayerReset`, incrementally narrowed while typing and tail-extended on paging) so `/`-filter navigation stays O(1) per keypress on huge feeds. **P pre-push branch-tip tag check** (`remote_tags.go`, `model.go`): `startPush` checks `tagsAtCommit(m.tags, currentBranchTipHash())` — if any exist it increments `pushCheckGen` and dispatches `pushTagCheckCmd` (a 5s `context.WithTimeout` around `svc.RemoteTagsFresh`); if no tip tags it starts the branch push immediately (no network). `pushTagCheckMsg` carries `gen`/`tipTags`/`remoteSet`/`err`; stale results (`msg.gen != m.pushCheckGen`) are silently dropped. On a fresh result: if all tags are pushed or the check timed out → straight push; otherwise a modal offers *Push branch + tags* / *Push branch only* / *Cancel*. Choosing *Push branch + tags* sets `pendingPushTags` and starts `engine.Push`. On branch-push success (`opFinishedMsg` with `msg.res.Changed == true`), `pendingPushTags` is captured-and-cleared, `pendingRemoteTagAdds` is set, and `engine.PushTags{Remote:"origin", Names}` is chained. A cancelled or aborted push returns `Changed:false` (err==nil) and must NOT chain — `pendingPushTags` is cleared unconditionally but only captured when `Changed:true`. On `PushTags` success, `applyPendingRemoteTag` drains `pendingRemoteTagAdds` into `m.remoteTagNames` (optimistic `▲` update). `pendingPushTags`/`pendingRemoteTagAdds` are also cleared and `pushCheckGen` is bumped by `reRoot` so a stale in-flight check arriving after a repo switch is dropped. Settings → **"Worktree post-create hook"** opens a `hookEditorPopup` (wide, multi-line; Enter = newline, Ctrl+S = save via `SetWorktreePostCreateHook`, Esc = cancel). When a hook is configured, the create-worktree popup shows an `[h]` toggle backed by a `runHook bool` field (default true). When `runHook` is true, the engine emits the `HookDecisionID` approval modal (run/skip) showing the script before running anything; toggling `[h]` off is a **pre-skip** that suppresses even the approval prompt. **"Shelf this commit" / "Bookmark this commit"** (Commits AND Reflog panels' `.` menu) push a `commitNamePopup` (`commit_name_popup.go`) instead of creating the entry directly: a one-line `textfield` pre-filled with the commit subject, `ctrl+s` inserts the commit's short sha at the cursor (`p.name.insert`), `enter` creates (routes to `shelfAddCommitCmd`/`bookmarkAddCmd` per `forShelf`), `esc` cancels via `popLayer`. An empty name falls back to the commit subject for a bookmark (`commitBookmark`) or leaves the shelf entry's `Label` unset. **Related-option prompts** (`related_prompts.go`, `related_prompt_popup.go`): a Settings toggle may push ONE follow-up popup about a related option — registry entries carry a stable suppression id, a live-config `when` precondition, and an `apply` that reuses the related Settings row's own code path (both show_graph↔commit_sort entries call `cycleCommitSort()`); options Yes / Not now (esc) / don't-ask-again (persisted via `promptstate`); hook = `maybeRelatedPrompt(setting, newValue)` after the toggle in the Settings enter handler. **Notification center** (`notify.go`, `notice_popup.go`): `domain.RepoHealth` runs in the background on startup/reRoot/Settings-open (gen-guarded `repoHealthMsg`); notices carry stable ids + op-backed actions; an info-only `clipboard_tool_missing` notice (from `clipboard.Probe`, riding on `repoHealthMsg.clipAvail` — an environment probe, not a git read) warns when a local X11/Wayland session lacks its clipboard helper (`xclip`/`wl-clipboard`), dismiss-only since gg can't install packages; unread notices blink the red `! N notice` status segment (style alternation on a self-stopping 800ms tick — never terminal blink escapes); `!` opens the dialog (list → per-notice actions; esc backs out); "Not now" = session dismissal (`noticeSessionDismissed`, cleared on reRoot), "Never for this repo" persists via `promptstate.DismissNotice`; the write+enable action chains `WriteCommitGraph` → `SetGitConfig` via `pendingNoticeConfig` (the pendingPushTags pattern); Settings "Commit-graph" row shares `startCommitGraphWriteAndEnable`. **Git config explorer** (`gitconfig_popup.go`, opened from the command palette — moved out of Settings `,`, see below): full-height searchable catalog (repoPopup-pattern `/` filter + `z` modes), columns key\|local\|global\|default with explicit dim `(unset)`; curated rows (`gitconfdocs`) edit via `l`/`g`/`u` — option picker for bool/enum, textfield for string/int, unset chooser over set scopes — through `gitConfigWriteCmd` (stageCmd-style synchronous `domain.Execute(SetGitConfig)`) which chains a rows re-read AND a repo-health re-read inside the same cmd, after the write lands (one `gitConfigRowsMsg` carrying an optional `health`, applied via `applyRepoHealth` — NOT a parallel `tea.Batch`, which would let the health read race the write it observes); non-curated rows read-only; gen-guarded loads (`gitConfigGen`). **Resume-paused-op prompt** (`resume_prompt_popup.go`): every status arrival (the `dataLoadedMsg`/`loadCmd` snapshot path AND the per-source `dataAvailableMsg{source: srcStatus}` arrival — covering `r`, background refresh, and file-watch) calls `maybeResumePrompt()`, which pushes a one-shot Continue/Abort/Not now popup (`↑`/`↓` select, `enter` chooses, `c`/`a` shortcuts, `esc` = Not now) the first time it observes `m.conflict.Op != ""` with zero conflicted files; a one-shot flag (`resumePromptShown`) is set only when the popup is actually shown (a busy skip — op running, another window open — retries next arrival) and re-arms when the paused-and-resolved state clears, and is also reset by `reRoot`. The popup dispatches `engine.ContinueOp`/`engine.AbortOp` on Continue/Abort. `canEnterConflict()` (`avail.go`) is the shared predicate — `opsIdle() && (conflicts > 0 || m.conflict.Op != "")` — gating both the `x` dispatch and its footer binding, so `x` opens the conflict process straight into its continue/abort state even with nothing unmerged. `view.go` renders a persistent `⏸ <op> paused (<source>) — press [x] to continue or abort` status segment (source in parens only when attributable) whenever `m.conflict.Op != ""` and no process is open, so declining the prompt never traps the user. **External tools** (`tool_run.go`, `conflict_process.go`): the conflict window's `t` key opens a `category="conflict"` command picker as a conflict-process-owned sub-state (the `hunkPicker` pattern — the process preempts the layer stack for keys while open, hidden from the footer/`?` help when zero commands are configured); repo-level commands (agents) list whenever an op is paused (filtered by `when_op` when set), per-file commands (Meld) list only when the focused file is a both-sides conflict. Selecting one fills any `<user:LABEL>` tokens, then a first-run approval popup shows the fully resolved command (Run/Cancel) — remembered per repo via `promptstate.ApproveToolCommand`, keyed by a hash of the template text, so an edited command re-prompts. Running hands over the real terminal via `tea.ExecProcess` over a temp script (`toolScript`/`toolExecCmd`, the `edit_actions.go` precedent — no engine op / repogate reservation, the same standing as `$EDITOR`); a per-file run materializes the LOCAL/BASE/REMOTE quartet via `domain.ConflictFileVersions` plus a per-run **context file** (`toolContextFile` — op/source/target header lines then one conflicted path per line, everything C-quoted via `cQuotePath` when it carries a control byte so no value can forge an extra line) exposed as `<context-file>`/`GG_CONTEXT_FILE` and ten more `GG_*` vars (`toolEnv`); on return, a per-file run whose `<merged>` mtime changed offers a mark-resolved decision, and all temp files (script, context file, quartet) are cleaned up best-effort. The `t` picker (`completeToolChoices`, `internal/tui/tools.go`) also lists any configured `category="conflict_complete"` commands alongside the `conflict` ones (only when an op is paused — with no paused op there is nothing to complete — further narrowed by `when_op`): a `CatConflictComplete` run gets the same env plus a fresh empty temp file exposed as `GG_MESSAGE_FILE`/`GG_TASK=conflict_complete` (`buildToolRun`), left OUT of `pending.cleanup` on purpose since a clean exit hands it to the viewer. On `toolFinished`, a clean (non-error, non-cancelled) `conflict_complete` exit with a non-empty message file closes the conflict process FIRST — `p.proc = nil` — then pushes the file straight into `newReviewView` as "Resolution overview — `<tool name>`" (the review-lane viewer, `e`-editable, `/`-searchable); closing first matters because the process preempts the layer stack for keys while open, so a viewer pushed over it would be key-dead. An empty overview instead removes the temp file and reports "`<tool> reported no overview`"; a failed/cancelled/interrupted run always removes it (`removeOverviewFile`) since there is nothing worth keeping. If the operation is still paused after a `conflict_complete` run (the agent stopped early rather than finishing), the ordinary `⏸ <op> paused` status segment and `[x]` lead back into the conflict process exactly as after `[L]` leave. The commit popup's **`ctrl+g`** (`commit_generate.go`) runs the stage-2 capture lane for `commit_message` commands: a numbered chooser when more than one is configured, the same first-run approval as the conflict lane (`tool_approval.go`'s shared `toolCommandApproved`/`rememberToolApproval`/`approvalBoxView`, keyed on the CONFIG template text so a different staged diff doesn't re-prompt), and a confirm-replace gate when the title/description already hold text — each a `commitPopup` sub-state (not a pushed layer), run in order ahead of dispatch. The resolved command runs headless via `engine.GenerateMessage` (no `tea.ExecProcess`/terminal handover), synchronously inside the returned `tea.Cmd` (the `stageCmd` pattern); its captured stdout is parsed by `exttool.ParseCaptureMessage` into a subject/body pair that fills the popup's fields. A generation counter (`genGen`, bumped on dispatch AND on esc-cancel) gen-guards the async result so a stale, cancelled, or popup-closed run can't clobber live state — essential because a ctx-killed subprocess returns `*exec.ExitError` ("signal: killed"), not `context.Canceled`, so only the gen check (not an error-type check) can tell a deliberate cancel from a real failure. Nothing commits automatically — the filled fields still need `ctrl+s` (the commit box renders at a wider-than-standard default via `commitNormalWidth`, and maximizes through the shared mechanism below). **Popup fullscreen-maximize** (`popup_max.go`): a shared embeddable `popupMax` (flag + `toggleMaximize`) plus `popupResolveWidth`/`popupMaxRowCap`/`popupResolveRowCap` let ANY centered-box popup toggle to a near-fullscreen box with **`ctrl+t`**, handled CENTRALLY in `Model.Update` via the `maximizableLayer` interface (`layer` + `toggleMaximize`): when the top layer embeds `popupMax`, `ctrl+t` flips its `maximized` flag and the popup's render honors it — `popupResolveWidth` widens the box, and fixed-cap list popups lift their row cap via `popupResolveRowCap`. `ctrl+t` never collides with typed text, so EVERY popup is maximizable (including text editors like the commit-message/hook-script popups) with NO per-popup key wiring. `ctrl+t` also fullscreens the focused panel. Full-screen surfaces (`isFullScreenLayer`: history/blame/rebase/hunk/diff) don't embed `popupMax` and are skipped. Uses `ctrl+t` not `ctrl+shift+t` — terminals send the same control byte for both. **Stage-3 review** (`review.go`, `review_view.go`): four self-gating `.`-menu rows — Commits panel "Review this commit" (`reviewTargetForCommit`: `sha^..sha`, or the bare `sha` for a root commit), Branches panel "Review branch `<name>`" (resolves `domain.BranchReviewTarget` off the UI thread via `reviewBranchTargetCmd`, gen-guarded), Files panel "Review working changes", and Commits panel "Review marked range (AI)" (`markedRangeReviewRow`, shown when ≥2 commits are ◉-marked; reuses `compareSelectionEndpoints` so it scopes identically to "Compare selection" — `older..newer` for 2 marks, `oldest^..newest` for 3+ — refused when either endpoint is a WIP row; the marked commits' feed hashes are already hex so `Range` stays injection-safe, with a human `Label` from the marks' short-shas/subjects) — each dispatching into `reviewLane`, a `popupMax`-embedding layer (no host popup, unlike the commit lane's sub-state fields) that owns the whole stack while open: a numbered chooser (`>1` `review` command), the shared `toolCommandApproved`/`approvalBoxView` first-run approval gate, then a spinner while `domain.ReviewReport` runs synchronously inside the returned `tea.Cmd` (the `stageCmd` pattern). A Model-level `reviewGen` counter (bumped on dispatch and on esc-cancel) gen-guards the async `reviewDoneMsg` the same way `commit_generate.go`'s `genGen` does — necessary because a ctx-killed agent returns `*exec.ExitError`, not `context.Canceled`. On success the lane is replaced by `reviewView` (`newReviewView(title, path, content)`), a full-screen read-only report viewer (plain-text scroll, `/` search via `jumpToNextMatch`, **`e`** reopens the same report file in `$EDITOR` via `openInEditorCmd`, `esc` closes); a failed/empty run surfaces `msg.err` on the status line instead of opening an empty viewer. **Command palette** (`command_palette.go`, `ctrl+p`): a registry of `paletteCommand`s (`paletteCommands()`, label + optional right-aligned key hint + run handler), rendered as a list and dispatched on `enter`. Alongside "Show commit" (`#`) and Find (`F`, opens the fuzzy file finder) it now carries **File history** / **File blame** — a `filePathPopup` whose `repoRelPath` normalizes a typed path (relative, absolute, or `./`-prefixed) to repo-relative before opening the existing history/blame view, with no pre-validation (a nonexistent file surfaces as "(no history)" or a blame error rather than being rejected up front) — and **Open repo** — a `repoPathPopup` that expands a leading `~` via `os.UserHomeDir()`, validates the path off the UI thread via `domain.OpenTUI(path).TopLevel()`, and on success `reRoot`s to it (an inline error on failure; a stale resolve is dropped the same way `gotoCommitResolvedMsg` is). The **Git config explorer** and **agent-skills picker** both moved out of the Settings `,` menu into the palette; the agent-skills entry still opens through `Settings` internally (`openSettings` then `openAgentPicker`) but sets `pickerFromPalette` so `esc` returns straight to the palette instead of the Settings menu. **Apply patch** (`apply_patch.go`): the command palette (`ctrl+p`) gains a second entry, **"Apply patch…"** (`Model.openApplyPatchPopup`), opening `applyPatchPopup` — mirrors `exportPatchPopup`: a one-line editable path field pre-filled with `ExportDefaultDir` (resolved off-thread via `applyPatchDirMsg`, the `startExportCommitPatch` pattern — a resolve error just opens with an empty prefill, non-fatal), `enter` dispatches `engine.ApplyPatch{Mode: ApplyModeAuto}` and pops the popup, `esc` cancels. A mailbox patch forks the existing modal Decider (`apply_patch.mode`: working-tree changes / recreate commits / abort, esc maps to the explicit `abort` option and genuinely cancels); a plain diff applies straight to the working tree. `opAffectedSources` maps `engine.ApplyPatch` → `{status, feed, branches}` (one op covers both modes: commits mode moves the branch tip and adds commits, working-tree mode changes status — possibly to conflicted, flowing into the ordinary status→conflict-process wiring via the same refresh). **Compare branches** (`branch_compare.go`): 4th Branches pair-op row "Compare A ↔ B" → `openBranchCompare` (full tip-to-tip compare view, full-name title override, async `CompareOrigins` load gen-gated by `compareTag`); `f` cycles the origin filter (all / left-only / right-only) rebuilding rows from the retained raw list; `comparePairState` cleared in `closeFilesView`. **Cherry-pick a commit entry** (`pick_commit.go`): `a` in the `g`/`G` switchers (commit bookmark / shelved commit only — a file entry gets a notice) runs a gen-guarded `domain.CommitLookup` probe (`pickGen`, bumped on switcher close/reRoot so a stale resolve is dropped) and forks three lanes: commit still exists → confirm modal → `engine.CherryPick`; gone but the shelf entry stores a patch snapshot → confirm modal → `domain.ShelfPatchFile` (temp file) → `engine.ApplyPatch{Mode: ApplyModeCommits}` (atomic `git am --3way`); gone with no patch (a bookmark, or a pre-patch/merge shelf entry) → a clear status notice. **Shell escape** (`shell_escape.go`): `ctrl+o` handled above the process/layer routing → interactive `$SHELL`/`%COMSPEC%` subshell via `tea.ExecProcess`, cwd = worktree, `GG=1`; palette "Run shell command…" one-off with pause + `scopeShellCmd` recall; return path = full reload; non-zero exit ≠ error, `$EDITOR` standing, no approval gate. **Session snapshot (MCP stage 1)** (`session_snapshot.go`): the perpetual 1s heartbeat serializes an agent-facing `sessionSnapshot` (schema v1: repo/focus/cursor/marks/files-view/switcher/filter/conflict/running-op; English protocol values, `status` display-only) and atomically writes `config.SessionSnapshotPath(commonDir)` only when the timestamp-less payload changed; removed on clean exit and reRoot (the file doubles as session presence); `startOp` now records `opName` (`engine.OpName`) for the `running_op` field. **Worktree switch guard** (`crossenv.go`, `switch_guard.go`): every production `reRoot` site routes through `guardedReRoot(path, offerRepair)` — stat OK → switch; a WSL↔Windows foreign-notation path whose translation exists (`translatePath`; injected goos+stat seams `guardStat`/`guardGOOS`) gets a repair/cancel modal at the two worktree-targeting sites — the Worktrees-panel enter and the Branches-panel "go to worktree" prompt (`engine.RepairWorktree` on the translated path, then a `pendingRepairSwitch` capture-only-on-success chain to the repaired path); anything else is refused with a status message, session untouched. **Branch versions** (`versions_popup.go`, `versions_settings.go`): the Branches panel's `.` menu row `branchVersionsRow` ("Previous versions…", self-gated on `opsIdle()` + a valid backing row, NOT on "does this branch have any recorded versions" — that would cost a git read on every `.`-menu open, so the popup itself shows the empty state) opens `versionsPopup` (`popupMax`-embedding, gen-guarded by `m.versionsGen`) straight into `versionsModeVersions` for the selected branch; the command palette's "Branch versions…" opens the same popup in `versionsModeBranches` — a picker over `domain.AllVersionBranches` (deleted branches marked) that drills into versions mode on `enter`, the recovery path for a deleted branch's history. In versions mode: `enter` whole-tree-compares the selected version against the branch's current tip (both endpoints resolved to tip hashes first — the mutable-ref-poisons-the-diff-LRU gotcha); `r` opens a reset-branch/new-branch-at-version/cancel decision (reset dispatches `engine.RestoreBranchVersion`, new-branch opens the existing create-branch popup pre-filled with the version sha as the start point); `d` deletes the snapshot after confirming (`engine.DeleteBranchVersion`); `y` copies the version sha. `opAffectedSources` maps `RestoreBranchVersion` → `{status, branches, feed, worktrees}` (a reset of the current branch also touches the working tree; a restored branch could be one a worktree tracks) and `DeleteBranchVersion` → `{}` (no panel shows version refs). Settings (`,`) → **"Operations history"** (`settingsMenuOpsHist`) is a small sub-view with two rows — "Retention" (shows `<N> days` / "keep forever"; `enter` opens a numeric textfield accepting digits and a leading `-`, saved via `saveVersionsRetention` → `config.SetVersionsMaxAgeDays` on the active repo `.gg.toml`, rejecting `0` and anything below `-1` with a status message instead of writing) and "Recording" (on/off toggle via `toggleVersionsRecording` → `config.SetVersionsDisabled`, the inverted-polarity key) — both update the live `m.cfg.Versions` AND call `svc.SetVersionsPolicy` immediately, so the very next operation honors the change without a reload; `versionsPolicyFromConfig(cfg)` is the shared mapper called from both config-arrival paths — `load.go`'s `loadCmd` (`dataLoadedMsg`, reRoot) and `source.go`'s `bootstrapCmd` (`configReadyMsg`, startup), the same pair `show_graph`/`applyLanguage` apply config at — and from the Settings writers. **Full-message / last-error viewer** (`error_popup.go`; `Model.lastError` captured centrally in the `Update` wrapper whenever a changed `statusMsg` satisfies `statusNeedsFull`): the status bar stays ONE line and the message leads with the `[E] full details` pointer (leading, so back-cutting truncation can never eat it); the global `E` key opens `newErrorPopup` — a `contentPopup` with `mode = modeWrap` and `noCursor` (prose, nothing to select) — showing the untruncated message, split on newlines to keep git's line structure. **`statusNeedsFull(msg, width)` (`view.go`) is the capture predicate, deliberately NOT `statusIsError` alone:** that predicate matches a 7-prefix allowlist while the TUI sets ~40 distinct error-prefixed messages — including the per-source refresh failures `sourceErr` builds (`branches: <git stderr>`, `worktrees: …`), of which only `commits:` happened to be listed, so a repo switch failing on the branches reload truncated a paragraph of git stderr with no way to read the rest. Growing the allowlist would just miss the next prefix, so the second condition is **structural**: any message wider than the bar is kept, error or not — nothing has to be registered. `statusIsError` still solely governs the RED styling, and the viewer's title/`danger` frame follow it too (`Full message`, unframed, when the captured text is not a failure). The pointer renders on any error plus any truncated message whose full text is actually held (`m.lastError == m.statusMsg`), so it can never point at text the popup would not show. **`Model.loadedOK`** guards the other half: a failed `dataLoadedMsg` normally renders a bare full-screen `error: …` (correct at startup — there is no interface yet), but once a load has succeeded a later failure (a repo switch into an unreadable repo) is reported through the status bar instead, keeping the panels and footer so `[E]` and every key still work. `sanitizeForDisplay` strips cursor-moving C0 controls and expands tabs: ssh writes CRLF and a surviving `\r` returns the terminal to column 0 mid-line, overwriting the box's own border — invisible to width math, since `\r` measures zero columns (the one-line bar never hit this because `oneLine` collapses every whitespace run). `contentPopup.box` sizes the window by WRAPPED display lines in `modeWrap` (`renderWindow`'s `h` is a line budget, not a row count), so a one-line message is not clipped to its first row. **Save a popup's text** (`save_content.go`): `s` in any `contentPopup` (the `[E]` viewer, help, …) and in the notification dialog writes the window's text to `os.CreateTemp("", "gg-<slug>-*.txt")` and reports the path via `contentSavedMsg` → `saved to %s`. The RAW lines are written, never the wrapped/truncated render, so a `sudo …` block the box split across rows comes back as one pasteable line. Deliberately a FILE, not the clipboard: a terminal multiplexer selects whole terminal-width lines (a centred box comes with the panels either side of it), and gg's clipboard path can silently no-op while reporting success — the reason someone is reading a fix instruction out of a popup may be that copying is what broke. **Stale-lock notice** (`notify.go`): `staleLockNotice` fires whenever `RepoHealth.StaleLocks` is non-empty and is inserted FIRST in `rebuildNotices` — it is a BLOCKER (every operation touching that file fails), not an advisory like the rest, so it outranks them. Its detail lines name each lock with `lockAgeLabel` (the age is the only staleness signal available) and explicitly warn that a LIVE git may hold one; the fix action dispatches `engine.RemoveGitLocks`. Two triggers, not just repo load: `maybeStaleLockNotice(err)` runs from the `opFinishedMsg` error branch (safe — every early return between it and the `refreshHealthAfterOp` re-read is on the success branch), arming a health re-read and appending a `[!]` pointer to the status message. It also CLEARS the notice's session dismissal, deliberately breaking the uniform lifecycle: for an advisory "Not now" rightly holds until the next load, but here a dismissal would leave the user with an unfixable error and no path out (never trap the user), and a fresh failure is new information rather than the same advice resurfacing. `opAffectedSources` maps `RemoveGitLocks` → `{status}` (explicit, so it doesn't fall through to "all" and fire the remote-tags network probe). **Dirty-tree switch guard:** `s` on the Branches panel against a dirty tree (staged+unstaged+conflicted > 0) forks a `"switch-dirty"` modal — *worktree* (opens the create-worktree popup for the branch, dirty tree untouched) / *carry changes* (today's autostash `SmartSwitch`) / *cancel* — instead of silently autostashing, the `checkoutCurrentBranchModal` pre-dispatch precedent; a clean tree keeps the existing confirm and the checked-out-elsewhere case still takes the go-to-worktree prompt. **Pair-op fast-forward row (mark.go):** pairing two branches now probes `FastForwardPair` off-thread first (`loadPairOpsCmd` → `pairOpsMsg`), then opens the popup; `ffPairOp(behind, ahead)` is inserted after merge/rebase only when the probe says OK, its closures ignoring (marked, selected) order because the probe fixed the direction. The msg handler drops stale probes (mark died/moved). Fail-open: probe error ⇒ popup without the row; svc-less models (tests) open synchronously without probing. `FastForward` is mapped in `opAffectedSources` (status/feed/branches). |
| `cli`        | Scriptable command frontend; `cliDecider` answers forks from a flag policy or stdin (the interactive prompt's blocking stdin read runs in a goroutine selected against `ctx.Done`, so a cancelled op can't stay wedged on a human). `gg merge [--no-ff] [-m <msg> | -F <file>] <source>`: `-m`/`-F` (`-` = stdin) set `SmartMerge.Message`, which implies `NoFF` (the engine ORs them: `deps.Repo.Merge(ctx, dir, src, msg, noFF || msg != "")`); `-m`+`-F`, an empty message or an unreadable file exit 2 before the op runs. `gg worktree add` supports `--hook` (answer `"run"` without prompting) and `--no-hook` (answer `"skip"` without prompting); with neither flag the `HookDecisionID` decision is answered by prompting stdin interactively and defaults to `"skip"` when stdin is not a terminal. `gg worktree add --from <commit> [--keep staged|unstaged] [<branch-name>]` creates the new branch AT `<commit>` (default name `<current-branch>_<short-sha>`, a trailing positional overrides it; mutually exclusive with `--branch`, since `--from` always makes a new branch), while `gg worktree add --branch <name> [<path>]` checks out an EXISTING branch, an optional `<path>` (resolved against the handler's `workdir` like `worktree move`, then handed to the engine absolute so its main-worktree-relative rule never applies) replacing the template path and silencing its `<user:>` prompts/`<seq>` counters, while `--keep` lands that branch on the commit's PARENT with the commit's diff left staged/unstaged instead — usage errors (exit 2) for `--keep` without `--from`, an unrecognized `--keep` value, `--from`+`--branch`, and more than one trailing positional; a root/merge commit under `--keep` surfaces the engine's `WorktreeKeepParentError` (exit 1). Agent-facing terse verbs added: `gg log [-n N] [<rev>|<A..B>]`, `gg diff [--stat\|--name-only] [--cached] [<rev>|<A..B>] [-- <paths>...]` (`splitDashDash` pre-splits args on a literal `--` BEFORE `flag.Parse`, which would otherwise eat a leading `--` and misread the first path as a rev), and `gg show <commit> [--patch] [-- <file>...]` — all read-only, backed by new `domain` query methods over the `LogLines`/`DiffNumstat`/`DiffPatch`/`ShowNumstat`/`ShowPatch` git verbs. `gg add (-A \| <path>...)` / `gg unstage <path>...` route to `engine.Stage`. `gg branch current`/`gg branch ls` and `gg worktree prune` round out the read/admin surface. `gg worktree rename [--force] <worktree> <new-name>` / `gg worktree move [--force] <worktree> <new-path>` share one handler (`cmdWorktreeMove`, a `rename bool` face flag); `<worktree>` resolves by path (absolute, cwd-relative, or main-worktree-relative) or by branch name, identically to `worktree remove`'s matcher. A relative `<new-path>` resolves against the handler's `workdir` param, not a bare `filepath.Abs` — production `workdir` is `"."` so it's byte-identical today, but the in-process e2e harness runs `t.Parallel()` subtests sharing one OS process cwd, so resolving against the real cwd would hit the wrong sandbox; `workdir` is always the CLI's own notion of "here". Before dispatching `engine.MoveWorktree`, the handler checks whether the process's own cwd sits inside the tree being moved and, if so, `os.Chdir`s out to the destination's future parent first (same Windows constraint as the engine op) and remembers the relative offset, so on a changed result it can write the equivalent new path to the shell-init `cwdFile` for the wrapping shell to `cd` into. `gg batch [--keep-going]` runs a script of `gg` commands from stdin against ONE shared service (one repo discovery for the whole script): `runOne` is the dispatch switch extracted out of `Run` so both a single CLI invocation and each batch line share it; `tokenizeBatchLine` is the quote-aware (no shell features) line splitter; each line's section is framed `#<idx> ok <cmdline>` / `#<idx> !<exit> <cmdline>` via `prefixWriter` (stderr lines get `! `), with a `#done <n> ok[, <m> failed[ (stopped)]]` trailer; stops after the first failing line unless `--keep-going`. Sub-commands run with an empty stdin reader (`strings.NewReader("")`), so a decision fork fails loud with its options instead of blocking mid-script — the same non-interactive contract as a lone `gg` invocation. `gg review [--tool <name>] [--working] [<rev>|<A..B>]` — flags MUST precede the positional (`--tool` takes a value, so it can't be partitioned out from after a positional the way `show`'s bool-only `--patch` can); default target is the current branch's work (`domain.BranchReviewTarget(ctx, "HEAD")`), a single-rev positional reviews that commit's own change (`rev^..rev`), an `A..B` positional is used as-is, `--working` reviews uncommitted changes; `--tool` selects a configured `review`-category command by name (the sole candidate is used with no `--tool`, zero/multiple is an error listing names). Runs `domain.ReviewReport`, prints the report to stdout, and reports the persisted path on stderr. Exit 0 on a produced report, 1 on tool failure/empty report/no review tool configured, 2 on a usage error. Routed through `runOne`, so `gg batch` can drive it. `gg apply [--am|--working] <path>` imports a patch file (the inverse of `gg commit export-patch`): flags precede the positional (the `gg review` convention); the CLI always passes an explicit mode — `ApplyModeAuto` (and its mailbox decision) is TUI-only, so `gg apply` never forks mid-run. Default (no flag) = working-tree mode for any format (safe, non-committing); `--am` recreates commits from a format-patch mailbox (typed refusal on a plain diff); both flags is a usage error. `cmdApply` threads `workdir` (as `cmdReview` does) and joins a RELATIVE `<path>` against it rather than the process cwd — production-invariant (`cmd/gg` passes `"."`, so the two coincide there) but load-bearing for in-process callers with an explicit workdir (e2e, a future MCP server), since the patch is read through two lenses: the engine sniffs its head via `os` (process cwd) while `git apply`/`am` run with the repo as cwd. Exit 0 = applied cleanly; 1 = failure OR applied-with-conflicts (conflicts left in the tree, the `gg merge --on-conflict=keep` convention); 2 = usage. Routed through `runOne`, so `gg batch` can drive it too. `gg shelf cherry-pick [--patch] [--on-conflict=keep|abort] <entry-id>` re-applies a shelved commit — the CLI twin of the TUI switchers' `a` key: live `engine.CherryPick` while the commit exists (`--on-conflict` maps to the `cherry-pick-conflict` fork like `gg cherry-pick`), otherwise (or under `--patch`) `domain.ShelfPatchFile` → `engine.ApplyPatch{ApplyModeCommits}` (atomic `git am --3way`; a patch-less entry — pre-patch-support or a merge commit — exits 1 with a clear message). Exit codes per the `gg apply` convention; lives in `cmdShelf`, so `gg batch` drives it. `gg versions [<branch>]` lists a branch's recorded pre-operation snapshots (branch-versions/operations-history feature), newest first, terse `<ts-op-id> <short-sha> <ISO-time> <subject>` (default branch: current; a detached HEAD with no branch argument fails loud, exit 1 — nothing to default to); an unrecorded branch prints `(no versions)` and still exits 0. `gg versions restore [--discard] <branch> <id|latest>` resolves `id` (an `<unix>-<op>` token from `gg versions`, or the literal `latest`) to its version ref via the same `BranchVersions` read, then dispatches `engine.RestoreBranchVersion`; `--discard` pre-answers the `restore-dirty` decision (the current-branch lane's uncommitted-changes fork) with `"proceed"` — without it a dirty tree fails loud under the CLI's non-interactive default. Both routed through `runOne`, so `gg batch` drives them too. `gg unlock [--yes]` lists stranded git lockfiles (`svc.StaleLocks`, one line each: name/age/path) and with `--yes` removes them via `engine.RemoveGitLocks`. Read-only by default — the destructive direction is never the default in a scriptable frontend where nobody reads the output. Exit 0 clean OR removed, **1 when locks are present without `--yes`** (so a script can use it as a precondition check), 2 usage. Routed through `runOne`, so `gg batch` drives it. `gg compare [--patch] <left> [<right>]` prints the changed-file list (`--patch`: unified diffs) between two endpoints; each is a **`gg://` link** (the primary spelling, and either side may be one — it mixes freely with the rest, it is not a mode), a commit-ish, `@staged`/`@index`, `@worktree` (default `<right>`), or — the commit-entry compare feature — `bookmark:<id>` / `shelf:<id>` (`resolveCompareSpec`): a stored commit entry resolved via `domain.ResolveCommitEntryEndpoint` (hybrid — the live sha while it exists, a shelved entry's frozen tar once gc'd, noted on stderr as `# frozen compare: …` so stdout stays parseable; a gone bookmark has no fallback and exits 1). **THERE IS NO INVALID PAIR.** `resolveCompareSpec` returns a `domain.FileSet`, not a `model.Endpoint` — a link is the one token that can name a BOUNDED side (a `@<a>..<b>` change-set, or any link with a `/<path>`) and an endpoint alone cannot say "these files" — and `domain.CompareSets` is total over the bounded/unbounded 2×2, so the `validComparePair` predicate that used to screen the two endpoints is DELETED, along with both refusals it printed: the reversed live pair ("order endpoints oldest→newest…") and the frozen shelf side against `@staged`/`@worktree`. `gg compare @worktree main` compares (inverted), and `gg compare shelf:<id> @worktree` answers "is my shelved work already in my tree?" — see **The compare algebra** below for the lanes and for `ComparePatch`'s two refusals (`ErrComparePatchPair` for a reversed live pair, and `patchLosesTheKeySet` for a side whose key set `--patch` would drop — both exit 2 in gg's own words). A link argument is LOCATED before it is evaluated (`domain.LocateLink`, never `ResolveLink`: a parsed local link's checkout and path are undivided, and flattening a change-set into an address would answer a different question); a link naming another checkout is refused (exit 2) — cross-repository compare needs two repogate reservations in a fixed global order and is deferred. `gg link` gained `--ref <branch|tag>` (validated against `svc.ResolveRev`, so a typo fails at the producer), `--pair <a>..<b>` (both halves resolved to FULL shas — `%h` honours `core.abbrev`, legal down to 4, while the grammar needs 7..64; `a...b` is redirected to `--preview`), and `--bookmark`/`--shelf` hint flags; the five target flags are counted, not pairwise-checked (five flags would be ten checks). |
| `mcp`        | MCP (Model Context Protocol) frontend: `gg mcp` serves stdio via the official Go SDK. Stage 1 = the SAFE surface only (no repo mutation): `gg_ui_state` reads the TUI session snapshot (`config.SessionSnapshotPath`, written by `internal/tui/session_snapshot.go`); bookmark/shelf list-get-read tools; `gg_compare_trees` (`CompareFiles`) / `gg_compare_file` (ResolveBytes → temp files → the `DiffNoIndex` verb, headers relabelled); `gg_export` (`ExportBookmark`/`ExportShelfEntry` → `engine.ExportToDir`, the `overwrite` decision answered from the tool's `overwrite` param via a fail-loud `staticDecider`). Domain-only frontend like `cli` (archtest: never imports `internal/git`/`tui`/`cli`); every reply carries `repo{common_dir, worktree}`; text reads share the `textPayload` text/binary/truncation contract (max_bytes default 262144). **Stage 2 (mutating):** `gg_cherry_pick` — the `gg shelf cherry-pick` two-lane logic (live `engine.CherryPick` with the `"cherry-pick-conflict"` decision answered from `on_conflict` (abort default / keep→`keep-conflicts`), else `ShelfPatchFile` → `engine.ApplyPatch{ApplyModeCommits}`); outcome classified by the `(Result.Changed, error)` contract, never summary sniffing — `(nil, !Changed)` = clean abort → error with retry hint, `(err, Changed)` = conflicts-in-tree → SUCCESS reply with `conflicts:true` + paths from `Status().Conflicts()`. `gg_write_to_worktree` — restore/paste via `engine.WriteFile` (dest defaults to the origin path; `"overwrite"` decision from the param; `unchanged` = `!Changed`; path safety inherited from `WriteWorktreeFile`'s guard). All 13 tools carry `ToolAnnotations` (`readOnlyAnnotations`/`mutatingAnnotations` in types.go — v1.6.1 gotcha: `DestructiveHint`/`OpenWorldHint` are POINTERS, nil means true); consent = the MCP client's destructive-tool prompt, no gg-side approval store. |
| `web`        | Browser frontend probe (`gg web`, read surface + first write increment): loopback-only HTTP server + embedded single-page UI (no node toolchain) over domain queries — `CommitFeed` paging, `commitgraph` lane cells, `CommitFiles`, and the shared cached `Differ` as JSON. Domain-only frontend like `cli`/`mcp` (archtest-guarded). Non-loopback `--addr` refused and the request `Host` header validated against loopback (DNS-rebinding defense, `hostGuard`); untrusted `sha`/`path` rejected before reaching git argv (`isGitArgSafe`); no auth token (loopback bind + hostGuard + writeGuard is the accepted single-user posture). Increment 1 of the write pathway adds `GET /api/status`, `POST /api/stage` (→ `engine.Stage` via a drain-events `runOp` + fail-loud `noDecider`, behind `writeGuard`: JSON content type else 415, loopback Origin else 403) and uncached working-tree diffs (`/api/diff?wt=unstaged|staged`, `Request{Key:""}`). Increment 2 adds the op transport (`oprun.go`/`ophttp.go`): `POST /api/op` (switch only) → one live `opRun` (replay buffer + SSE fan-out at `GET /api/op/{id}/events`; `engine.Done`/`Timing` not forwarded — done is synthesized from Execute's return) + `POST /api/op/{id}/decide` feeding a parking `webDecider` (5-min timeout). `done{changed:true}` resets the cached CommitFeed. `GET /api/branches` + SPA sidebar/status-line/decision modal ride on it. `op:"commit"` (engine.Commit, message required) is the transport's second op — the status pane's commit box submits it (Ctrl+Enter; a form-field keydown guard keeps j/k/s/u from firing while typing). `op:"pull"` (engine.SmartPull zero-value: current branch, its remote, PullAndStay) is op #3 — the non-fast-forward fork is the first live parking modal in production. `op:"push"` (engine.Push current-branch/origin/set-upstream; branch resolved server-side; `force` became wire-settable later — see the force-push entry) is op #4. Ops #5-7: `op:"delete-branch"`/`op:"delete-tag"` (isGitArgSafe-guarded names; DeleteTag is decision-free so the CLIENT confirms via showLocalConfirm) and `op:"remove-worktree"` (client path is an identifier resolved against the server's own Worktrees list — allowlist, not sanitization; 404 on no match) — all three behind sidebar right-click menus (generic `showCtxMenu`, destructive rows red, served-worktree row exempt from remove). `GET /api/worktrees` + `GET /api/tags` (capped at 100, `truncated` flag) feed the sidebar's worktrees/tags sections; a tag click opens commit detail via the feed-independent `openCommitByHash`. Ops #8-11: `op:"stash"` (all changes + untracked, message from the commit box) and `op:"stash-apply"/"stash-pop"/"stash-drop"` (ref allowlist-resolved against the server's own StashList; drop confirmed client-side — decision-free op) behind a 4th `stashes` sidebar section (`GET /api/stashes`, rows ref/subject/sha with the sha resolved at list time so left-click opens the existing commit detail). Stash rows also carry `untracked_sha` (the `^3` untracked-files parent, server-resolved; the client merges its file list into the stash drill-in with a per-file sha override — the TUI's stash drill-in merges the ^3 file list with per-line sha overrides — both frontends fixed); the diff header is `#diff-title` + a `#diff-nav` toolbar (file/change-block arrows over `fileCursor`/DOM-derived blocks). Transport hardening: `decide` publishes a `resolved` wire event (replay-idempotent modals; a second tab's modal closes live); the client's `es.onerror` distinguishes transient (CONNECTING → reconnecting hint) from permanent (CLOSED or 5 straight failures → op declared lost: UI unlocked, modal closed, `refreshAfterOp`); destructive modal options render red via a client `DANGER_OPTIONS` set (options are English protocol values); sidebar visibility + per-section collapse persist in localStorage (`gg.sidebar.*`); stash apply/pop/drop send the listed `sha` and the server 409s on mismatch (freshness guard atop the ref allowlist; resolve errors never block); detail opens are gen-guarded (`state.detailGen`, bumped by `drillOut`) and the diff-nav arrows disable on notice renders. Re-root: the domain service lives behind an `atomic.Pointer` accessor (`s.service()`, read once per request); `POST /api/reroot` (writeGuard) resolves the client path by allowlist (own `Worktrees()` + the `internal/repos` MRU registry — archtest-legal for frontends), preflights the candidate BEFORE swapping (broken target → 409, old root keeps serving), then swaps + drops the feed and the finished op record under the same `opMu` section that blocks a concurrent op start. Wave 3: a **command palette** (ctrl+k/ctrl+p, footer chip) over the layer stack — pull/push/refresh/toggles/help plus a **switch repo…** mode (the first `GET /api/repos` consumer, feeding `doReroot`; esc backs out of repo mode when drilled in from cmd mode; every close path blurs the palette input or the form-field guard would eat all global keys) — and a **global ☰ menu** (top-bar button reusing the generic ctx-menu; `stopPropagation` so the document outside-click closer doesn't eat the opening click). **Hunk-staging, inline in the diff** (reworked from live feedback — a separate block list lost context): the server tags an ELIGIBLE `wt=unstaged` diff's rows with hunk ordinals + the staging freshness hash (`/api/diff` gains per-row `hunk` + a `hunks{count,hash}` object — valid because the diff rows and `hunkpick.FromDiff` derive from the SAME textdiff alignment of the same index↔worktree pair; a run/block count mismatch, rename, or truncation just yields an untagged diff). The client highlights tagged blocks in place (context + line numbers intact), click toggles a block (`getSelection().isCollapsed` guard so text-selection doesn't toggle), and a diff-header `#hunk-bar` (stage selected (n)/all/none) POSTs positional picks + the hash to the unchanged `/api/stage-hunks`; the diff RELOADS after every round (the hash moved) and on 409. **Per-file discard** (`op:"discard"`): path resolved server-side against a fresh Status read (unknown → 404, conflicted → 422, untracked → `Discard{Remove}`/clean, else `Discard{Restore}`/restore --worktree keeping staged hunks), client offers red-confirm RMB rows "discard changes"/"delete untracked file". The hunks CRLF guard narrowed to **mixed-EOL only** — `hunkpick.Doc` now carries `EOL` (set by `FromDiff` when both sides are consistently CRLF; `Resolved()` joins with it), so pure-CRLF files round-trip byte-faithfully in the web AND the TUI `H` picker (previously silently rewritten to LF). The worktree ctx-menu's "switch here" drives it (client reloads into the new root); serve + successful re-root `repos.Touch` the active root (best-effort) and `GET /api/repos` lists the MRU for the coming picker. Overlays ride a client layer stack (`pushLayer`/`closeLayer(id)`/`topLayer` in app.js): the top layer's onKey sees keys first, an unhandled esc closes the top, and `closeLayer` removes a layer from ANY position (the transport can close a parked modal under an open help); modal/help/ctx-menu are layers with unchanged behavior, and wave-3 popups build on the same stack. Hunk staging (backend): `GET /api/hunks?path=` (blocks + sha256(index+NUL+worktree) freshness hash) and `POST /api/stage-hunks {path,picks,hash}` (writeGuard) recompute `hunkpick.FromDiff` fresh, 409 on hash drift (picks are positional), flip picked blocks to TakeIncoming, and run `engine.StageHunks` via runOp — untracked/binary refused like the TUI, CRLF refused deliberately (the TUI's picker silently rewrites CRLF→LF; shared-fix candidate). Ops #12-13: `op:"merge"` / `op:"rebase"` (`engine.SmartMerge{Source,Target}` / `engine.SmartRebase{Branch,Onto}`; both names isGitArgSafe-validated, existence and source==target left to the engine) behind sidebar **branch drag-and-drop** — dragging a branch onto another opens the shared ctx-menu with both directions spelled out (`merge A into B` / `rebase A onto B`), the menu row being the confirmation, matching the TUI pair-op popup; a conflict forks `merge-conflict`/`rebase-conflict` into the existing parking modal. Both ops are worktree-aware in the engine, so an arbitrary drop pair needs no client-side precondition. The commits pane's line mode (`graphMode="off"`) now draws its dot as a ONE-CELL SVG (`flatDotSVG(color)`, the same `CELL_W`/`ROW_H`/`HALF`/`MID` constants `graphSVG` uses) instead of a text `●` glyph, so the dot sits exactly on the leftmost lane's centre in both modes and cannot drift with the font; `wtRowHTML` uses the same dot, passing its yellow explicitly since an SVG circle takes a `fill` rather than the inherited CSS `color`. **Resizable panes:** each layout shows exactly three grid children — left pane, a 5px drag handle, right pane — so the *other* mode's handle must be hidden alongside its panes or it would claim a column (`#panes.solo #rs-detail`, `#panes.detail #rs-sidebar`, `#panes.solo.nosb #rs-sidebar`). Both handles resize the FIRST column, so the new width is always the pointer's offset from the `#panes` left edge. The `RESIZERS` registry (app.js) holds the CSS custom property, the localStorage key, and the default per handle; the pattern is **want vs. applied** — the user's chosen width is stored and persisted, while `clampPaneWidth` (min 120, always keeping 200 for the right pane) decides what the current window can afford, re-applied on every `window.resize`, so shrinking the window squeezes the pane without forgetting the choice. `setPointerCapture` is wrapped in try/catch: it throws on a synthetic pointer id, which must not abort the drag (the headless-CDP verification path depends on this). No server change and no Go test — the whole feature is `index.html`/`style.css`/`app.js`. **Branch copy rows:** the branch ctx-menu carries copy-branch-name / copy-commit-id / copy-worktree-absolute-path, all served from data the sidebar already fetched — `branchRow.Hash` is `%(objectname:short)` (git's abbreviated sha, the same value the TUI's row copies, so it is reported in full rather than re-abbreviated), and the worktree row comes from a client-side `state.worktrees` ⋈ `state.branches` join (`worktreePathForBranch`), omitted when no worktree has that branch — the TUI's `worktreeAbsPathForBranch` gate. `copyText(text, what)` now reports success on the status line, not just failure: a clipboard write is otherwise indistinguishable from a no-op. Destructive rows stay last in every ctx-menu. **Op #14 + branch-scoped sync:** `op:"fetch"` (`engine.Fetch{}`, zero args, decision-free) sits in the ☰ menu and palette; `op:"pull"` and `op:"push"` each grew an OPTIONAL `branch` — omitted keeps their original meaning (current branch, resolved server-side for push), named dispatches `SmartPull{Branch, Intent: PullInBackground}` / `Push{Branch}` with the name `isGitArgSafe`-validated. No client precondition is needed for a named pull: `SmartPull` routes `target == cur` down its own `pullCurrent` lane regardless of intent. The CLIENT is what distinguishes the two lanes — the branch ctx-menu offers the confirming current-branch `doPull()` on the HEAD row and the non-confirming `doPullBranch()` (labelled `(stay here)`) elsewhere, because only the checked-out lane can rewrite the working tree. **Solo mode** (`solo.go`): `POST /api/solo {branch}` (writeGuard) narrows the commit list to one branch — `Server.solo` (a branch NAME, guarded by `s.mu` beside `feed`) plus `soloScope()` → `LogScope{Branches:[…]}`, which is ref SELECTION not a content filter, so the lane graph still renders. The key move is that the scope is **re-derived at every feed build**: `feedFor` calls `f.SetScope(soloScope(s.solo))` (SetScope only records the refspec — the caller's `LoadInitial` does the walk, so a rebuild is free), and `setSolo` just stores the name and nils the feed. That dissolves the obvious trap — `resetFeed` after `done{changed:true}` replaces the feed object, and a scope applied once to the old one would silently vanish, kicking you out of solo on an unrelated commit. An unknown branch is refused (404, resolved against `svc.Branches` — the `remove-worktree` allowlist precedent) rather than merely sanitized: `isGitArgSafe` already covers argv, but a scope that cannot render would fail every later `/api/commits` and take the client's exit affordance down with it. `/api/commits` reports `solo` on the response it scopes, so a reload or a second tab learns the scope without a second call; the client renders a `solo: <name> ✕` chip in the top bar from `state.solo` (which survives a failing commits load) and the branch ctx-menu swaps *solo this branch* for an explicit exit row on the soloed branch. **Ops #15-17 + the one-line prompt:** `op:"create-branch"` (`CreateBranch{Name, StartPoint}` — `branch` on the wire is the START POINT, optional, "" = HEAD), `op:"rename-branch"` (`RenameBranch{Old: branch, New: name}`) and `op:"create-worktree"` (`CreateWorktreeForBranch{Branch, Path}` — for an EXISTING branch; the new-branch-and-worktree variant with its template resolution stays TUI-only). Name validation is only `isGitArgSafe` here on purpose: the engine runs every new name through `git check-ref-format` and reports a clearer refusal than any allowlist this layer could invent (a test pins that `bad..name` fails IN the op and creates nothing). `create-worktree` passes the repo's configured `[worktree] post_create_hook` (read via `s.postCreateHook`, the same committed-`.gg.toml` probe `feedFor` uses for commit-sort; any failure → "" → no hook) rather than silently skipping it — the engine's approval decision shows the script and parks in the browser modal, defaulting to skip, so honouring the config costs nothing in safety. Client: `openPrompt({title, value, placeholder, onSubmit})` (app.js) is the shared one-line text prompt — a layer like any other, `onSubmit` receives the TRIMMED value and is never called empty, and `closePrompt` **blurs the input** before closing (a still-focused field makes the form-field guard swallow every global key — the palette's lesson). `defaultWorktreePath(branch)` prefills a sibling of the MAIN worktree (`state.worktrees[0]`, which git lists first) named `<repo>-<branch>` with `/` → `-`, so creating a worktree from inside another does not nest them — the TUI's main-worktree anchor. The worktree row is gated on `!worktreePathForBranch(b.name)` (git allows one worktree per branch). **Branch ↔ branch compare** (`compare.go`): `GET /api/compare?a=&b=` resolves both names against the server's own branch list (404 on unknown — the solo/remove-worktree allowlist precedent; an unknown name would otherwise yield an empty compare indistinguishable from "identical"), then returns `CompareFiles(tipA, tipB)` with each row tagged `origin` (`"a"`/`"b"`/`"both"`) from `CompareOrigins` — the per-file projection of the TUI's two path sets, a rename counting on either of its paths. `origins_error` (e.g. `no common ancestor`) rides ALONGSIDE the files rather than replacing them: the comparison stands without a merge base, only the per-side filter goes away. `/api/diff` gains a third form, `handleRevDiff` (`left=&right=&path=&status=`), for a file at two unrelated revisions — the compare rows' diffs; `left`/`right` are the TIP HASHES the compare response returned, never branch names, since a name in a diff endpoint poisons the session-lived commit↔commit diff cache (the TUI's `openBranchCompare` lesson). Client: the drop menu's third row (read-only, below the two history-rewriting ops) calls `openCompare`, which sets `state.filesMode = "compare"` — a third mode alongside `commit`/`status`, and safe to add because every existing site tests `=== "status"` only — plus `state.compare{a,b,aHash,bHash,all,filter,originsError}`; `applyCompareFilter` rebuilds `state.files` from the retained raw list so filtering costs no fetch, and `renderCompareBar` keys off `filesMode` (NOT off `state.compare`) so the bar cannot linger into the next commit's detail screen. `openFile` picks `left`/`right` over `sha` on the same branch. A global **create branch…** row (☰ + palette) sends `create-branch` with no `branch`, which the server already reads as HEAD. **Branch versions** (`versions.go`, ops #18-19): `GET /api/versions?branch=` returns `domain.BranchVersions` rows (ref/hash/short/subject/op/unix). The branch is NOT allowlisted against the live branch list, unlike solo/remove-worktree — there a bad value broke something downstream, here the read is one `for-each-ref` under a prefix, so an unknown branch is simply empty, and a DELETED branch's versions are precisely what the read is for; `isGitArgSafe` is the whole guard. `op:"restore-version"` (`RestoreBranchVersion{Branch, Ref}`) and `op:"delete-version"` (`DeleteBranchVersion{Ref}`) both validate only the leading dash and lean on the engine's own checks — the ref must belong to the named branch, and delete refuses anything outside `refs/gg/versions/` (both pinned by wire-level tests that assert the real branch survives). A dirty current branch forks `restore-dirty` into the existing parking modal; every other restore lane prompts for nothing, so the CLIENT confirms first (the delete-tag/discard convention). Client: `openVersions(branch)` fills a `#versions` list overlay pushed on the layer stack with no `onKey` (the stack's default esc-closes applies), `versionWhen` renders an age rather than a date (what you want is how far back to go), and a row click opens the shared ctx-menu with restore / copy commit id / delete — per-row buttons would crowd the line, and the ctx-menu matches the sidebar's language. That handler must `stopPropagation` AND `preventDefault`, and is bound to BOTH `click` and `contextmenu`: without the former the document-level outside-click closer sees the same click bubble up and shuts the menu in the event that opened it (the ☰ button's lesson — the row looks dead), and without the latter a right click falls through to the browser's own menu. The sidebar menus never hit this because they are `contextmenu`-only. `hideCtxMenu` now also EMPTIES the menu rather than only hiding it: a closed menu still holding its last rows reads as open to anything inspecting the DOM, which is how the original browser check of this popup passed while the menu was in fact being closed on every click. **Overlay stacking** (`style.css`): `#ctx-menu` (40) and `#modal` (30) sit ABOVE every panel overlay — help 11, palette 21, versions 21, prompt 22 — because both are RAISED FROM those surfaces (a version row's menu, and the confirm that row raises) rather than sitting beside them; at the original 20/10 they painted underneath the popup, which reads as a dead click, not as a hidden element. This is invisible to a `hidden`-class assertion: only `document.elementFromPoint` at the surface's own centre proves what the pointer would actually hit, and that is what the verification for this now does. The row is added to the branch menu unconditionally (no "has versions" gate — that would cost a read on every menu open; the popup shows the empty state, the TUI's `branchVersionsRow` rule). **Force push** (a posture change, decided 2026-07-27): `op:"push"` now accepts `force`, reversing the earlier "Force is never wire-settable" rule. The reversal is safe because of what the flag actually does — `engine.Push{Force:true}` does NOT force-push; it asks the `push-force` decision (`force-with-lease`/`force`/`abort`, safe option first) and pushes whatever comes back. So the wire flag only reaches that prompt — the same one the rejection-recovery path already parks in the browser modal — without needing a rejection first, and a silent force push remains inexpressible from a client. Hence the client row (`doForcePush`) has NO local confirm: the modal IS the confirm, and its two force options already render red via `DANGER_OPTIONS`. Pinned by three tests: the flag parks `push-force` (not `push-rejected`) with the exact option order, abort leaves the remote tip and its commits intact, and answering `force` really overwrites; a fourth pins that a flag-less push still takes the unchanged `push-rejected` path. **AI review** (`review.go`): the first web surface that executes something other than git. `GET /api/review/tools?target=branch\|working&branch=` resolves the target (`domain.BranchReviewTarget` / `WorkingReviewTarget`) and lists every structurally valid `review`-category command from the EFFECTIVE config (`s.effectiveConfig` — global + the active per-repo file, `gg review`'s resolution, distinct from `postCreateHook`'s narrower committed-only probe), each with the command **resolved for this target** and an `approved` flag; `POST /api/review {target,branch,tool,approve}` (writeGuard) re-resolves everything server-side and starts the run. **The command text never comes off the wire** — the client sends a tool NAME, so a client bug cannot become a remote shell; and an unapproved command is refused `403 {needs_approval, command}` before anything runs, the client showing that text and re-posting with `approve:true`. Approval is remembered via `promptstate.ApproveToolCommand` keyed by `promptstate.CommandHash(tc.Command)` (the CONFIG template text) under the git common dir — **the same store, key and scope the TUI's lanes use**, so approving in either frontend covers the other and an edited command re-prompts in both (verified live: Claude/Junie already read as approved in the web from TUI-granted approvals). The branch is guarded by `isGitArgSafe` only — the `/api/versions` precedent, not `/api/compare`'s allowlist — because an unknown name simply fails in `ResolveCommit`, and `BranchReviewTarget` resolves BOTH endpoints to hex before they reach the tool's `<range>` token. To carry this, the op transport grew a **generalized run lane**: `startRun(kind, runFunc)` where `runFunc(ctx, svc, events, dec) (Result, extra map[string]any, error)` — `startOp` is now a thin wrapper whose fn is a bare `Execute`, while the review lane's fn calls `domain.ReviewReport` (which owns persistence and takes no events/decider, so the lane's only wire traffic is the terminal done, matching the TUI's bare spinner) and returns the report/path/label in `extra`, which is merged into the `done` event. `POST /api/op/{id}/cancel` (writeGuard) is **agent-lane-only** (`run.kind != "review" && run.kind != "conflict_complete"` → 409; widened for the conflict lane below — same reasoning, a second headless-agent kind): an agent can hang for minutes holding the single lane, whereas interrupting a git op half-way is a separate question. Cancellation is recognised by the `run.cancelled` flag `requestCancel` sets, NEVER by the error's type or text — a ctx-killed subprocess reports its signal (the TUI's review lane learned the same) — and surfaces as `done{error:"cancelled", cancelled:true}`. Client: `followOp(opID, label, kind, onDone)` is split out of `startOp` so the review lane reuses the identical stream handling (including the lost-connection rules) from a different start endpoint; an `onDone` REPLACES the generic done handling, since a review changes nothing in the repo. One `#review` overlay walks chooser → approval → progress (they are steps of one decision, not three surfaces) and `#report` is a plain-text viewer — deliberately not a markdown renderer. Cancelling while the START REQUEST is in flight must cancel the run once its id arrives rather than returning early: doing the latter orphans an agent nobody is following, holding the lane until it finishes on its own (a real bug the browser check caught, whose own first version passed on the broken build because a busy lane made the follow-up start 409 and start nothing). **AI conflict resolution** (`conflictai.go`): the review lane's structure reused for a paused-op agent — `GET /api/conflict/tools` (409 `nothing is paused` when `Conflict().Op == ""`) lists every structurally valid, web-visible (`config.ToolVisibleIn(tc, "web")`), `mode="capture"` `conflict_complete` command from the effective config, filtered by `WhenOp` against the live paused op, each resolved for DISPLAY with the real op/source/target/conflicted-paths and a literal `$GG_CONTEXT_FILE` placeholder (the real temp path exists only at run time) plus an `approved` flag from the SAME `promptstate` store/key the review lane and the TUI's lanes use — approving a command in one frontend covers all. `POST /api/conflict/complete {tool, approve}` re-resolves server-side (the command text never comes off the wire, the review lane's rule), 403s with `{needs_approval, command}` on first run, then `startRun("conflict_complete", …)` calls `domain.CompleteConflictReport` and returns `Changed:true` on EVERY successful run (not just a fully-resolved one — a stop-early agent mid-rebase may still have created commits, so a spurious feed reset is cheaper than a stale commits pane), with `{report, tool, op, still_paused}` merged into `done`. Client: the conflict banner grows an "AI resolve…" button reusing the `#review` overlay as a mode-aware lane (`rev.mode` = `"review"` | `"conflict"` drives title/copy and conflict-mode's still-paused handling); `onDone(ev, kind)` threads the run's OWN kind from the SSE done event rather than reading `rev.mode` after the fact — `reviewCancel()` nulls `rev` before the cancel round-trip's done event lands, so deriving `isConflict` from `rev.mode` at that point silently fell back to `"review"` on every cancelled conflict run (wrong label, and the conflict-only refresh never fired); `kind` survives independently via `state.op`, `rev`/`state.task` stay display-only. A successful conflict run always refreshes the repo (the op's state may have changed whether or not it fully finished) and opens the overview in the same report viewer as a review, titled "Resolution overview — `<tool>`". **Repo-picker identity** (`repos.go`): `GET /api/repos` rows carry `current` — whether that entry IS the served repo — because the two sides of that question are spelled differently and only the server can reconcile them: `/api/repo` reports git's `rev-parse --show-toplevel` (FORWARD slashes even on Windows) while the MRU registry stores `filepath.Clean`'d paths (BACKslashes there). The client's original `r.path !== cur` therefore never matched on Windows, so the switch-repo picker listed the repo already open and picking it re-rooted onto itself (Linux was unaffected — a report, not a test, found it). `sameRepoPath` normalizes via `path` (not `filepath`) so its behavior does not depend on the OS running it, then compares case-insensitively when `pathGOOS == "windows"`; `pathGOOS` is a seam so the Windows rules are exercised from a Linux test run. **Backgrounding a review** (client-only; no server change): a run can be PARKED — `parkReview()` sets `rev.parked`, closes the `#review` layer and hands the run to a top-bar `#task-chip`, while `followOp`'s SSE stream keeps running untouched. `unparkReview()` (clicking the running chip) pushes the layer back. `reviewDone` branches on the captured `rev.parked`: parked → the result is STORED in `state.task` and the chip goes loud (filled + a FINITE-iteration CSS blink, the TUI notice's rule — an endless blink is noise you stop seeing), NOT opened; `collectTask()` (clicking the ready chip) opens the report and clears it. `closeReviewLane` clears a *running* chip (a cancel or a lost stream must not leave a spinner nobody drives) but `reviewDone` re-sets a finished one right after, so the ordering is load-bearing — it also means `reviewDone` must capture `title`/`parked`/`label` BEFORE calling it. **esc and the backdrop PARK a live run rather than cancelling it** — esc means "off my screen" everywhere else here, and destroying minutes of agent work on the reflex key would be a trap; cancelling stays the labelled button. Parking does NOT free the op lane (the run still holds it, and making it concurrent would only move the wait: `ReviewChanges` holds a repogate Read reservation, so a TreeWrite op would hang at the gate instead of being refused). What that buys is reads — commits, diffs, files, branches all keep working. It also created a dead-click risk the dialog used to cover, so every op entry point now guards with `opBusy()` (`app.js`) which REPORTS instead of returning silently; the one `if (state.op) return` deliberately left alone is inside `opLine`'s 30-second expiry timer, where reporting would re-arm the timer forever. That same timer exempts a parked run (`parkedRunning()`): the never-expire-while-an-op-runs rule assumes the line is being rewritten by the op's own progress events, and a parked review emits none, so its line would otherwise sit there for the whole run. On completion the parked lane HIDES the line rather than replacing it (the chip is the announcement; a second permanent status line saying the same thing is clutter you must dismiss by hand) — except on failure, where an error that vanished silently would be worse. The `#report` viewer deliberately has NO backdrop-closes handler, unlike every picker overlay: `collectTask` clears the chip the moment the report opens, so a stray outside click discarded the only copy in the UI — which is exactly what double-clicking the ready chip did (first click opens, second lands on the backdrop). The parked-run status line is a HANDLE, not a notice: `taskLine()` marks it `.task` (cleared by the next `opLine`, so the class can never outlive its message), a click on it calls `unparkReview()`, and the strip's dismiss-on-double-click SKIPS it. That dismissal is advertised only by `#op-head`, which `style.css` renders for `.err` lines only — so on the background-run line it was invisible, and double-clicking the biggest thing on screen naming your running review silently deleted it (reported twice). **Commit right-click + single-commit history edits** (`commitedit.go`, op #20): the commits pane had NO `contextmenu` handler at all, so right-clicking a commit fell through to the BROWSER's menu (a user report). `#commits-window` now opens the shared ctx-menu with show / copy commit id / copy subject plus the three history edits, gated on `row.parents === 1` — `commitRow` gained a parent COUNT (not the ids: the count is exactly what the gate needs), and a merge or the root shows the copy rows alone, the TUI's `commitEditRow` gate. `e.preventDefault()` runs only AFTER the row resolves to a commit: preventing above the guards left the Working-tree row with neither gg's menu nor the browser's — a right-click that did nothing, caught by the browser check. `op:"commit-edit" {sha, edit}` (`edit` one of `drop`/`move-up`/`move-down`) mirrors the TUI's `commit_rebase_ops.go`: `buildCommitEdit` resolves the current branch, derives the base via `rebaseplan.OntoFor`, reads `onto..branch` with `domain.CommitRange` and builds the plan with `rebaseplan.BuildSingleEdit`, then runs `engine.InteractiveRebase`. **The wire never carries a plan** — a commit id and one of three verbs is the whole vocabulary, and the plan is built from a range the server reads itself. The sha is required to be plain hex (`isHexSha`), NOT merely `isGitArgSafe`: it reaches git argv inside `<sha>~1`, so any other rev expression would let a client choose the rebase base (every feed value is already full hex). Refusals are staged — 400 for a malformed request, 422 for well-formed-but-inapplicable (detached HEAD, root commit, commit not on this branch, and the engine's own merge-in-range refusal), with nothing run in either case. `execPath` is `os.Executable` behind a package seam because `InteractiveRebase.GGBin` must be a real gg binary and under `go test` that is the test binary, which would re-run the suite instead of writing a todo file (internal/engine's `buildGG` has the same problem); production is safe because `gg web` is served from the same `cmd/gg` binary that routes `__rebase-seq`. **Interactive-rebase plan editor** (`irebase.go`, op #21) closes backlog item 18: `GET /api/rebase-range?branch=&onto=` lists `onto..branch` (both names allowlisted against the live branch list — the `/api/compare` rule, since an unknown name would come back as an empty range indistinguishable from "already equal") with each commit's FULL message, so a reword can edit the body. `op:"interactive-rebase" {branch, onto, plan:[{sha,action,msg}]}` runs `buildInteractiveRebase`. **ORDER is the invariant to hold**: `rebaseplan.Plan.Entries` is git todo order (OLDEST first) and so is the wire in both directions; the editor displays newest-first (the TUI's `irebaseEditor` does the same) and reverses at exactly two points — when the range arrives and when the plan is sent. Both reversals are pinned by browser checks: dropping the send-side one makes `Groups()` refuse a squash with no preceding target (the op fails), while dropping the load-side one still SUCCEEDS and silently edits the wrong commits. **TRUST**: the client may reorder and annotate, never invent — the plan is revalidated against a freshly read range and must name every commit in it exactly once (a plain length check would let a swapped sha through; the once-each rule catches a duplicate, which has the right length AND only in-range shas), which doubles as the staleness guard (409 when a commit lands under an open editor). `Entry.Orig` always comes from the server's read, never the wire: it is inert for a plain pick (git keeps the object's message) but composes the message of a **squash target**, so trusting `msg` there would rewrite a message through a row the UI showed as a pick. Squash-at-index-0 is refused server-side and disabled client-side, and a row MOVED into the oldest slot stops being a squash. Nothing runs until *start*: cancel, esc and the backdrop all close the editor with the branch untouched. **Version↔tip compare + all-branches picker:** `/api/compare` gains an explicit rev form (`revs=1` — both sides plain hex ids via the commit-edit `isHexSha` guard, no branch-list resolution; NOT an allowlist erosion, since the allowlist's rationale is name-specific: an unknown name yields an empty compare indistinguishable from "identical", an unknown hash fails loudly, and hex-only is a stricter argv gate than `isGitArgSafe`), `/api/diff`'s `left`/`right` now ENFORCE the hashes-only contract their doc comment promised, and `GET /api/version-branches` (one `AllVersionBranches` read; the `deleted` flag is the point) feeds a `#vbranches` picker overlay (palette + ☰ "branch versions…") that sits at z-index 20 — deliberately BELOW `#versions` (21), so drilling into a branch stacks the versions overlay on top and esc drills back out via the layer stack's default. The version row menu gains "compare against current tip" (rev-form `openCompare` with `{revs, aLabel, bLabel}` — `state.compare.a/b` hold display labels, every git-facing consumer reads `aHash`/`bHash`; the tip is the client's cached abbreviated sha, immutable for its object), omitted for a deleted branch (restore first — the TUI's rule); restore from the picker path closes BOTH overlays before the op (stale rows). **Staged detail layout (GitKraken flow):** `state.layout` is now three-valued — `list` (sidebar \| commits), `files` (commit opened: the SIDEBAR STAYS, commits shrink to the flexible middle column, file list = fixed RIGHT column, NO diff, nothing auto-opens), `diff` (file opened: diff replaces the commits area, list stays right; the `detail` CSS class now means this stage). The file list is placed right via CSS `order` (no DOM moves — every `$("files-list")` handler stays put); `#panes` keeps the exactly-three-visible-children grid rule in list/diff; `files` is the one five-children mode (sidebar | handle | commits | handle | files; its `nosb` variant drops back to three), so `clampPaneWidth(cfg, w)` reserves the OTHER handle's width there — two fixed columns on screen at once must not squeeze the flexible commits column to nothing. The sidebar `b` toggle works in list AND files (refused only on the diff screen). All five entry points (`openCommit`/`openCommitByHash`/`openStashDetail`/`openWorkingTree`/`openCompare`) go through `enterFilesStage()` (sets layout `files` and CLEARS `#diff-title`/`#diff-body`/`state.lastDiff`, so a later diff entry can't flash the previous drill's diff and a window resize can't resurrect it) and no longer call `openFile(0)`; `openFile` switches to layout `diff` in its SYNC prefix, so an esc during a slow diff fetch cannot be undone by the fetch completing. `drillOut` STEPS one stage (diff → files → list; only the files→list step bumps `detailGen`). `applyCompareFilter` renders its empty state into the FILE LIST (`li.sect` — the diff pane is off-screen in the files stage) and only auto-opens under an already-open diff. `RESIZERS` entries support `right: true` (`rs-detail`): the drag measures from the panes' RIGHT edge, since `--files-w` is now the third column. **Ops #22-23 + the conflict surface:** `/api/status` gains a `conflict` object (`{op,source,target,desc,conflicted}` via `domain.Conflict` on the same status read) present whenever a sequencer op (merge/rebase/cherry-pick/revert) is paused, even with zero conflicted files. `op:"continue"`/`op:"abort"` (`engine.ContinueOp{}`/`AbortOp{}`, zero-arg — the engine dispatches to whichever op is paused and owns all refusals) back a `#conflict-bar` banner: Continue is gated on zero conflicted files, Abort is red behind a client confirm (`DANGER_OPTIONS` gained "abort merge/rebase/cherry-pick/revert", never bare "abort"). `GET /api/conflict-hunks?path=` lists a conflicted file's pickable blocks via `hunkpick.ParseConflict` — fresh-status eligibility (unknown path 404, known-but-not-conflicted 422), a binary/no-markers 422 naming the mark-resolved escape hatch, and a sha256 freshness hash; wire items are `{kind:"text",lines}` or `{kind:"block",index,ours,theirs}`. `POST /api/resolve-hunks {path,picks,hash}` (writeGuard) requires EVERY block picked (a partial resolve would stage a file still carrying conflict markers), 409s on hash drift, writes+stages via `engine.ResolveConflictHunks`, and returns fresh status in the body — the stage-hunks response convention. Client: the dead "conflicted — resolve in the TUI" row is gone, replaced by an in-diff-pane block picker (`#cf-doc`, ours/theirs per region + take-all buttons, `#resolve-bar`'s full-coverage gate, reload on 409); the ctx-menu's "mark resolved (stage as-is)" reuses the existing `/api/stage`. **Commits-panel parity:** `/` quick filter (client-only narrowing of loaded rows; flat rendering; explicit load-more hint row), `#`/palette goto-sha (`GET /api/resolve` → reveal-in-list with fallback to detail), and file history/blame overlays (`GET /api/filelog`, `GET /api/blame` — read-only GETs over `domain.FileLog`/`Blame`; `domain.ResolveRev` is the full-sha resolve `CommitLookup` can't provide). Entry points: file-row ctx menus in all three files modes, palette path prompts, diff-toolbar buttons driven by `state.diffCtx`. The history overlay reuses the `/api/diff` commit form for parent-vs-commit file diffs; `diffHTML(d, paneWidth)` is the extracted shared diff renderer. **Big-repo suggestion** (`health.go`, `uiconfig.go`, op #24): `GET /api/health` projects `domain.RepoHealth` (big = pack ≥ 100MB, the TUI's `bigRepoPackBytes` floor; `Server.packThreshold` is the test seam) + effective `[ui] show_graph`/`commit_sort` + the two banner ids' promptstate dismissal state; a dismissible client banner (plain DOM after `#conflict-bar`, never a layer) offers "graph off + plain sort" → `POST /api/ui-config` (enum-allowlisted values only, writes the COMMITTED `.gg.toml`, a commit_sort write drops the feed) and "write commit-graph + keep fresh" → `op:"commit-graph"` (one server-side run chaining `engine.WriteCommitGraph` → `engine.SetGitConfig{fetch.writeCommitGraph=true}` — the key/value never come off the wire); "never" → `POST /api/notice-dismiss` (ids allowlisted to `commit_graph_recommend` — the TUI's id, shared dismissal — and `web_graph_off_suggest`); "not now" is sessionStorage. The client also honors `[ui] show_graph` as the default graph mode when localStorage has no `gg.graph` override (boot fetches health before the first commits render). **Drag-drop pair menu fast-forward:** `showBranchPairMenu` (sidebar.js) is async — it awaits `GET /api/ff-pair?a&b` (ffpair.go; names allowlisted against the branch list, 404 on unknown) and inserts a "fast-forward <behind> to <ahead>" row after merge/rebase when ok; fetch failure fails open (menu without the row). The row starts the existing `op:"fast-forward"` with `branch`=behind + `onto`=ahead — `onto` present means advance the NAMED branch to onto's tip (both allowlisted), absent keeps the original advance-the-current-branch lane. |
| `worktree`   | Shared worktree template resolution used by BOTH the TUI popup and the CLI. |
| `repos`      | Machine-local MRU registry of opened repositories (XDG state file) behind the repo switcher. |
| `agentskill` | Two embedded skills ("using-gg", "reviewing-with-gg") behind a Skill value type (go:embed + per-skill version marker) that teach AI agents the gg CLI and the review-notes lane. |
| `agentinit`  | Hardcoded agent registry + detect/status/install behind `gg init` and the TUI Settings popup. Roster expansion (2026-07-20): `antigravity` (detect `~/.gemini/antigravity-cli` — the agy home, not bare `~/.gemini`; skill → `~/.gemini/config/skills/using-gg/SKILL.md`, agy's global customization root) and `kimi` (detect `~/.kimi-code`; skill → `~/.kimi-code/skills/using-gg/SKILL.md`), both ModeSkillFile. |
| `exttool`    | Hardcoded catalog of external tools/AI agents (Claude Code, Junie, Meld, OpenAI Codex, Antigravity, Kimi Code) gg can run per task category (`conflict` and `commit_message` shipped; `review` sketched for a later stage): per-category default `CommandTemplate`s whose dynamic content is only generation-time tokens (`<bin>`, `<env:NAME>`) — never a raw prose substitution — plus injected-probe detection (`Detect(look, stat)`, the `internal/clipboard` seam pattern: `exec.LookPath` over `Bins`, `os.Stat` over `ExtraProbes` for off-PATH installs like Meld on Windows). Leaf; the TUI imports it directly like `agentinit`. The Settings "External tools" wizard materializes a template via `GenerateCommand`/`GenerateCommandFor` — `<bin>` → the detected binary (quoted if it contains whitespace), `<env:NAME>` → `${NAME}` (POSIX) / `%NAME%` (Windows) — into an editable `[[tools.command]]` config block; only config content ever executes. `commit_message` templates (Claude, Junie) run `ModeCapture` (headless; the caller captures stdout, no terminal handover) and read the staged diff via `GG_CONTEXT_FILE`/`GG_STAGED_DIFF` env vars rather than a token — the commit-popup `ctrl+g` generate mechanic. `ParseCaptureMessage(captured) (subject, body string, err error)` is the format-agnostic parser behind it: handles plain text, Claude's `--output-format json` (`.result`), its `--json-schema` envelope (`.structured_output`), and the **raw message text a task-agent writes to `$GG_MESSAGE_FILE`** (Junie — the engine reads that file and prefers it over stdout, so what reaches the parser is a clean message via the raw-text path, not Junie's stdout report); a tool-reported error (`is_error`) surfaces as `err`, an empty/unusable result as `ErrEmptyMessage`. Junie's `commit_message` template now writes to `$GG_MESSAGE_FILE` (the "absolute path outside the repository" clause is load-bearing — a coding agent can refuse to write outside its project root). `SplitMessage` is the shared first-line-is-subject splitter (also backs the commit-popup amend pre-fill). Stage 3 un-inerts `mode = "capture"` for a new `CatReview` ("review") category (`toolUsable`, previously `commit_message`-only) and ships `review` templates for Claude and Junie, both `ModeCapture`, verified against the real binaries: Claude's `claude -p "/code-review <range>" --output-format json --permission-mode acceptEdits --allowedTools …` runs its own git analysis from the `<range>` token; Junie's runs via the same task-agent + `$GG_MESSAGE_FILE` file-channel mechanism as its `commit_message` template, fed the diff at `<env:GG_REVIEW_DIFF>` (Junie's own `--review` flag can't take a range). Roster expansion (2026-07-20): Codex (interactive conflict; capture via `codex exec --sandbox read-only --output-last-message "$GG_MESSAGE_FILE"` — the harness writes the file, so stdout noise is moot), Antigravity (`agy --prompt-interactive` conflict; capture lanes OptIn + `--dangerously-skip-permissions` + $GG_MESSAGE_FILE channel — headless agy auto-denies permission-gated tools, even reads), Kimi Code (headless `-p` for ALL lanes incl. conflict — no interactive-with-prompt flag exists, and `-p` REFUSES `--yolo`/`--auto` ("Cannot combine --prompt with --yolo", 0.27.0) so there is no yolo row; print mode approves its own edits; capture via $GG_MESSAGE_FILE since `-p` stdout is a work report; first `~/`-expanded ExtraProbes entry). OptIn invariant generalized: OptIn ⇔ the command carries a permission-bypass flag, not "(yolo)" naming. `Detect(look, stat, home)` — `~/` probes expand against the injected home ("" skips them). `GenerateCommandFor` also flattens the materialized command through `template.FlattenForCmd` when `goos == "windows"`: the catalog templates are multi-line for source readability, which a POSIX shell runs fine but cmd.exe cannot run at all. This is deliberately BELT AND BRACES with the runtime repair in the three script writers — generation keeps the CONFIG honest (it is the text the approval popup shows before a command's first run, so what runs must be what was shown), while the runtime repair is what fixes the blocks already on disk, since the wizard is append-only and never rewrites an existing block. `CatConflictComplete` ("conflict_complete") is a fourth category — a resolve-AND-complete task where, unlike `CatConflict` (whose contract forbids `--continue`, since gg's `ContinueOp` owns the sequencer), the agent deliberately OWNS the sequencer for the run: resolve, stage, run the matching `--continue`, repeat through further rebase rounds, then report an overview via `GG_MESSAGE_FILE`; never `--abort`. It is **yolo-only by construction** — one bypass-flag command per agent per lane (terminal, and for Claude/Codex/Antigravity a headless capture sibling), no cautious row — Claude/Junie/Codex/Antigravity via their terminal-handover bypass-flag variant (`claudeCompleteCommand`/`junieCompleteCommand`/`codexCompleteCommand`/`agyCompleteCommand`), and Kimi via `ModeCapture` (`kimiCompleteCommand`, `kimi -p`) since Kimi's print mode has no bypass flag to combine with (`-p` refuses `--yolo`/`--auto`) yet still approves its own edits — so it is NOT `OptIn` despite being yolo-equivalent; `defaultToolChecked` (`internal/tui/settings_tools.go`) accounts for this by defaulting every `CatConflictComplete` row unchecked in the wizard regardless of its `OptIn` value, not just the `OptIn` ones. `CommandTemplate` also carries `Frontends []string` (mirrors `config.ToolCommand.Frontends`; materialized verbatim into the generated `[[tools.command]]` block by `GenerateCommand`/`GenerateCommandFor`) — the existing terminal-handover conflict_complete rows are tagged `["tui"]` (a browser has no terminal), and the web AI conflict lane adds three NEW `ModeCapture` headless rows tagged `["web"]`: Claude (`-p --dangerously-skip-permissions`), Codex (`exec --dangerously-bypass-approvals-and-sandbox`, deliberately WITHOUT `--output-last-message` — that flag would overwrite the agent's own `$GG_MESSAGE_FILE` overview with its final chat message instead), and Antigravity (`-p --dangerously-skip-permissions`); all three are `OptIn` like their terminal siblings, and flag combinations were re-verified against the real binaries 2026-08-01. Kimi's existing single row is retagged `["tui", "web"]` (its `-p` mode was already headless-only, so the same row serves both frontends). Junie's headless row (added 2026-09-29) is a plain `junie --task … --skip-update-check`: `--task` is its non-interactive mode, which approves its own edits and git commands with no flag (`--brave` is interactive-only and unneeded), so the row is not OptIn — live-verified on a paused merge and a 4-round rebase. Claude's and Junie's headless complete rows are tagged `["tui", "web"]` (codex/antigravity stay web-only until verified), and both agents also have a headless resolve-only `conflict` row — `Claude (yolo, headless)` (`claude -p … --dangerously-skip-permissions`; the permission-gated shape auto-denies every git command under `-p`, so it could edit but never stage) and `Junie (headless)` (`junie --task …`). |
| `config`     | TOML config (`.gg.toml`), field-level overlay (defaults→global→repo), `<seq>` counters. Sections: `[worktree]`/`[ui]`/`[debug]`/`[refresh]`/`[versions]`. The `[versions]` section (`VersionsConfig{Disabled, MaxAgeDays}`) governs branch-version (operations history) snapshots: `disabled` (bool, default `false`) is a kill-switch, **inverted on purpose** — the zero-is-unset field-overlay rule means a plain `enabled=false` at the repo layer could never override a global `true` (the `[refresh] disable_remote_tags_auto` precedent); `max_age_days` (int, default `90`) prunes older snapshots, with `-1` meaning keep forever and `0` meaning unset (falls back to 90 through the overlay — so the "forever" sentinel must be `-1`, not `0`). The `[worktree]` section includes `post_create_hook` — a multi-line TOML literal string (`'''…'''`) shell script run after a worktree is created; empty = disabled. The `[ui]` section includes `commit_sort` — `date-order` (**default** when the key is missing; `git --date-order`, a global topological sort so the commit-graph forks render correctly) or `plain` (git's lazy fast order; the graph can draw a disconnected lane stub on date/topology disagreement, e.g. after a squash). Applied to the feed via `CommitFeed.SetSortMode` (`pagerForMode`); `GG_COMMIT_PAGER` env still overrides. Also `show_graph` — `on` (**default** when the key is missing; lane graph) or `off` (flat ●-gutter list, same as the `.` menu's "Show as list"); a string (not a bool) so the repo layer can override a global `off` back to `on` under the zero-is-unset overlay rule; applied to `Model.commitListMode` at both config-arrival paths (`configReadyMsg` startup + `dataLoadedMsg` reRoot). Also `language` — TUI display language code (empty = English; `SetGlobalUILanguage` writes the **global** config from the Settings picker). The `[refresh]` section holds the master `enabled` bool, per-source interval seconds (`status`/`branches`/`remotes`/`worktrees`/`tags`/`reflog`/`feed`/`fetch`/`remote_tags`, all defaulting to 0/off), `min_seconds` (default 10; floor on any auto-refresh interval so cheap sources don't poll faster), `disable_remote_tags_auto` (inverted bool, default `false` = auto-refresh ON; independent of `enabled` — disabling it suppresses the `srcTags`-arrival auto-enqueue, not the timed `remote_tags` interval), and per-source file-watch bools (`worktrees_watch`/`reflog_watch`/`branches_watch`/`remotes_watch`, all default `false`). No adaptive keys (`disable_adaptive`/`max_read_seconds`/`backoff_factor` are gone). Read-only at runtime **except** ten non-destructive line-edit writers: `SetGlobalDebugLogOperations` (`[debug] log_operations`), `SetGlobalRefreshEnabled` (`[refresh] enabled`), `SetRefreshInterval` (`[refresh] <source>` in the **repo** `.gg.toml` — backing the Settings "Refresh rates" inline editor), `SetGlobalDisableRemoteTagsAuto` (`[refresh] disable_remote_tags_auto` in the **global** config — backing the Settings "Auto remote-tag refresh" toggle), `SetRefreshWatch` (`[refresh] <source>_watch` in the **repo** `.gg.toml` — backing the `w` file-watch toggle in the same editor), `SetWorktreePostCreateHook` (`[worktree] post_create_hook` in the **repo** `.gg.toml` — the first multi-line writer; delimiter-aware so scalar writers remain safe), `SetCommitSort` (`[ui] commit_sort` in the **repo** `.gg.toml` — backing the Settings "Commit sort" cycle), `SetShowGraph` (`[ui] show_graph` in the **repo** `.gg.toml` — backing the Settings "Show graph" toggle), and `SetVersionsMaxAgeDays`/`SetVersionsDisabled` (`[versions] max_age_days`/`disabled`, both the **active repo** `.gg.toml` — backing the Settings "Operations history" retention field and recording toggle). Both `gg config init` and `gg config populate` are generated from the `settingDocs` registry in `template.go` (single source of truth); `populate` is an additive, comments-only top-up that appends missing keys as `[populated]`-marked lines and never overwrites user values. Also the `[tools]` section — `[[tools.command]]` blocks (`ToolCommand`); lists **CONCATENATE** across global+repo (a deliberate exception to the zero-is-unset field-overlay rule), the repo block winning a `(category,name)` collision (`overlayTools`); `AppendToolCommands` is the append-only writer (creates the file if missing, never touches an existing block, refuses a command body containing the `'''` TOML literal delimiter); `ValidateToolCommand` makes a structurally invalid block (unknown category/mode, empty name/command, `per_file` outside `conflict`, unknown `when_op`) inert at load rather than a startup error. `ToolCommand` also carries `Frontends []string` (toml `frontends`; any of `"tui"`/`"web"`/`"cli"`, empty = everywhere) — `ValidateToolCommand` rejects an unknown value (block goes inert, the same fate as a bad category/mode) and `AppendToolCommands` writes the field only when non-empty (an omitted `frontends` line round-trips to the empty/everywhere default). `ToolVisibleIn(tc, frontend)` is the read side every picker filters through (empty `Frontends` visible everywhere; otherwise a membership check) — it exists because the web AI conflict lane ships headless catalog rows that must stay invisible to the TUI's terminal-only picker and vice versa. Per-repo config now has TWO possible homes: the committed `<repo>/.gg.toml` and a machine-local private file `PrivateRepoPath(mainWorktree)` = `$XDG_CONFIG_HOME/gg/projects/<EncodeRepoKey(mainWorktree)>/config.toml` (keyed on the MAIN worktree so all linked worktrees share it). `Load` is unchanged (2-arg): the caller resolves `ActiveRepoConfigPath(committed, private)` — the private file if it exists on disk, else committed — and passes THAT single path as `repoPath`, so gg reads AND writes one active per-repo file (pure replace, not layering — a committed inverted-polarity/zero-is-unset key must never shadow a private "off"). `CopyRepoConfig`/`RemoveRepoConfig` back the whole-file copy/move. TUI `repoconfig_popup.go` (Settings "Repo settings location") drives it. `SessionSnapshotPath(commonDir)` — the per-repo TUI session-snapshot location under the state root (`repos.DefaultStatePath` roots), shared by the TUI writer and `gg mcp` reader. |
| `template`   | Pure branch/path template resolver (`<parent-branch>`, `<repo>`, `<date:…>`, `<seq:…>`, `<user:…>`, …). Also a command-context resolver (`ResolveCommand`/`CmdCtx`) behind external-tool commands: per-token-kind quoting — path tokens (`<repo>`/`<file>`/`<local>`/`<base>`/`<remote>`/`<merged>`/`<context-file>`) shell-quoted via `quoteArgFor`, prose tokens (`<op>`/`<source>`/`<target>`/`<conflicted-files>`/`<user:LABEL>`) substituted literally; `<bin>`/`<env:NAME>` are generation-time-only and error if seen at resolve time; `ValidateCommandTokens` makes an invalid template inert (unknown token, `<bin>`/`<env:…>` outside a catalog template, a per-file token in a repo-level command) without resolving any values. Stage 3 adds **`<range>`** — a prose token like `<op>`/`<source>`/`<target>`, substituted literally with the review target's range label (e.g. `main..HEAD`); empty for the uncommitted target (a review command reaches the diff via `$GG_REVIEW_DIFF` instead). Also `FlattenForCmd(s)` — the cmd.exe repair applied to every command gg writes to a temp `.bat` (`tui.toolScript`, `engine.ShellCaptureRunner`, `engine.ShellHookRunner`). cmd.exe cannot express a POSIX line continuation OR a double-quoted string spanning lines, both of which gg's own templates use: it ran the tool from the FIRST line with a truncated argument and NO flags, then treated the remaining lines as separate commands — which is why *Claude (yolo)* launched without `--dangerously-skip-permissions` (the flag sits at the end of the prompt's fifth line) and plain *Claude* launched without `--permission-mode acceptEdits` (eight lines of `\`-continuations). Only those two shapes are joined, so a genuine multi-line batch script (a post-create hook) still runs line by line; output is CRLF-separated. The trailing-backslash rule does misread a line legitimately ending in one (`dir C:\foo\`), which is why it is scoped to commands gg assembles from templates. Also `CQuotePath(p)`/`ConflictContextDoc(op, source, target, files)` — moved from `internal/tui` so the TUI's conflict tool runs and the engine's new `CompleteConflict` op share ONE implementation (byte-identical behavior; the TUI now delegates to these rather than keeping its own copy): `CQuotePath` renders a path the way git prints one containing control characters (double-quoted, `\n`/`\r`/`\t`/`\"`/`\\` as C escapes, any other byte `< 0x20` as `\NNN` octal, byte-wise not rune-wise so UTF-8 continuation bytes — always `>= 0x80` — can never be mistaken for a control), and a clean path passes through byte-exact and unquoted; `ConflictContextDoc` renders the per-run context-file body (`op:`/`source:`/`target:` header lines then one `CQuotePath`-quoted conflicted path per line) — the shared file both the TUI's `toolContextFile` and the engine's `CompleteConflict` write to `$GG_CONTEXT_FILE`, so no value can forge an extra line. |
| `textdiff`   | Pure line-alignment engine (Myers + guards) behind the side-by-side diff view; no git/TUI imports. An `Enhanced` option adds word-level intraline spans on changed rows. |
| `commitgraph`| Pure single-line commit-graph lane engine (`Lay(commits []{Hash,Parents})` → per-row Unicode glyph cells + node lane); no git/TUI/lipgloss imports. Consumed by the TUI Commits panel (cached in `m.commitGraphRows`, drawn only in natural feed order — `commitGraphOn`). |
| `cache`      | Generic injected in-memory LRU cache factory (`Factory.Cache(name) Cache`, `GetOrLoad`/`Load[V]`); two-bound eviction (entry count + byte budget via `Sized`). Keys are caller-chosen hashes. First consumer: the commit-diff cache. |
| `clipboard`  | System-clipboard writer behind a single `Copy(tty, text)`: prefers a native OS command (`clip.exe` on WSL/Windows, `pbcopy` on macOS, `wl-copy`/`xclip`/`xsel` on Linux; `nativeCopyCmd` returns a `nativeCopy{argv, env}` so a subprocess env override can ride along), falls back to the pure OSC 52 escape for remote/SSH or when no native command exists (SSH detected → OSC 52 first so it reaches the local terminal; WSL detected via kernel osrelease so `clip.exe` beats WSLg `wl-copy`). **Wayland-in-tmux fix:** `wl-copy` is gated on `resolveWaylandDisplay` (not `env("WAYLAND_DISPLAY") != "")` — tmux strips `WAYLAND_DISPLAY` from its environment, so the display is recovered by scanning `$XDG_RUNTIME_DIR` for a live `wayland-N` socket (`findWaylandSocket`, absolute path) and injected into the `wl-copy` child; same tmux env-staleness rationale as `detectWSL` reading osrelease. **WSL-interop gate:** `clip.exe` is chosen only when `wslInteropOK(readFile)` confirms the kernel can execute Windows binaries — `exec.LookPath` finds `clip.exe` whether or not WSL interop is registered, so presence alone had gg picking a command that fails at exec time with `exec format error` and silently falls through to OSC 52 (whose write always succeeds → a green "Copied …" while the clipboard never changed). The probe reads `/proc/sys/fs/binfmt_misc/` (`status` not `disabled`, plus a `WSLInterop`/`WSLInterop-late` entry whose first line is `enabled`); an **unreadable binfmt_misc resolves to true on purpose** — absence of evidence isn't evidence, and a false positive would strip `clip.exe` from a machine where it works (WSL1, a sandboxed `/proc`). With interop dead, selection falls through to `wl-copy`/`xclip`/`xsel`, which under WSLg still reaches the Windows clipboard. Deliberately **not cached** (interop can be repaired mid-session and copy must pick it up with no restart). `Probe() Availability` runs the same detection and reports whether a native command exists and, if a **local display** (X11/Wayland) is present without its tool, what to install (`xclip`/`wl-clipboard`) — headless/SSH leaves `Install` empty (OSC 52 is the expected path, no false-positive nag); backs the TUI "install a clipboard tool" notice. `Availability.WSLInteropBroken` reports the interop state independently of `Available` (a machine with `wl-copy` copies fine and needs no notice, but the state is still true) and backs the `wsl_interop_broken` notice, which fires only when nothing covers it and **supersedes** `clipboard_tool_missing` so one problem yields one notice. The OSC 52 sequence builder + tmux/screen passthrough wrapping stay pure/unit-testable; no TUI/git deps. `clipboardStdin` UTF-16LE-encodes stdin specifically for `clip.exe`/`clip` — its stdin-encoding heuristic misdetects short pure-ASCII payloads (a git tag name) as already UTF-16 and stores them verbatim, pasting back as CJK mojibake; a 40-char SHA carries enough signal to be detected correctly, which is why only short copies showed the bug. Used by the TUI `.` menu copy actions. |
| `shelf`      | Non-git, per-file content store ("the shelf") behind a fixed `Store` interface (default impl: content-addressed blob files + atomic-rewrite TOML index under the XDG state dir, keyed by git common dir); named buckets with an implicit `default` + hidden support; paged `List`. Owned by `domain`; frontends never import it (archtest-guarded). The shared `model.FileRef` (Unstaged/Staged/Commit/Shelf) + `domain.ResolveBytes` + the generic `engine.WriteFile` op give "compare anything" / "copy anywhere as unstaged". Each entry's `Origin` is a `model.FileAddress` (captured at shelve-time), rendered bookmark-style by `FileAddress.Display()`. The TUI surfaces the shelf as a global `G` quick-switcher popup (no left tab) mirroring the bookmark `g` popup; cross-store compares (focused-vs-shelf, bookmark↔shelf) reuse `pendingCompare` + the shared `openPickerDiff` (closes both popups + clears the stack). An entry can also be a **shelved commit** (`model.ShelfKind = ShelfKindCommit`, `ShelfEntry.IsCommit()`): `Store.PutCommit(bucket, addr, tar, patch []byte, label string)` stores a `git archive` tar of a commit's changed files as the blob (capped at `MaxCommitArchiveBytes = 200<<20`), keyed by a path-less `model.FileAddress{State: StateCommitted, Commit: sha}`; `label` is `model.ShelfEntry.Label` — a human name, display-only and not part of the entry's id, "" = none. `PutCommit` now also stores an **optional second blob** — the commit's `git format-patch` mailbox (`ShelfEntry.PatchSHA`/`PatchSize`; read back via `Store.GetPatch(entryID)`, `ErrNoPatch` when absent; `Remove`'s blob reclaim covers BOTH blobs) — so a shelved commit can be re-applied as a commit (`git am`) even after the original object is gc'd. `domain.ShelfAddCommit(ctx, sha, label)` builds the tar (`commitChangedPaths` + `git.ArchiveFiles`) and calls `PutCommit`, giving a durable snapshot that survives `git gc`/history rewrite the same way a file entry survives deletion of its source; the patch snapshot is captured **best-effort** (`CommitPatch` — a merge commit, an oversized patch, or a format-patch failure just skips it: tar-only, never an error). `domain.ShelfPatchFile(ctx, entryID)` materializes the stored patch into a temp file for the TUI's `a` cherry-pick fallback lane. A labeled entry shows ` — <label>` in `gg shelf list` and the `G` switcher (`shelfEntryDisplay`). `domain.ExportShelfEntry`/`ExportBookmark` unpack a commit entry's tar (or archive a commit bookmark live) into `[]model.ExportFile` for the copy-to-temp-dir flow (`domain.TempExportBase` + `engine.ExportToDir`); each gets a per-type default subdir name (`commit-<shortsha>` / the entry id / `bookmark-<label>`). Shelf refs are **member-aware**: `ResolveBytes` on `FileRef{SourceShelf, Locator, Path}` whose entry is a COMMIT extracts that one member from the tar (`shelfResolve` — discriminated by the ENTRY KIND via `Store.Find`, Get's metadata sibling, never by content-sniffing, so a shelved `.tar` *file* stays a blob; a path-less commit ref stays the whole tar for export); `domain.ShelfCommitFiles` lists the members (tar header scan, no data copy). These back the TUI's **`filesModeShelf`**: `enter` on a commit entry in the `G` switcher opens the files view on the frozen members (tree-only like compare mode — no live commit list; `focusedBookmark` yields `{StateShelf, ShelfID, Path}` so the standard `.`-menu rows — Copy to working dir (the per-file restore), Compare against working dir, View file / Open in external editor (frozen bytes, NOT `ShowFile("",path)`=index) — light up with zero new action code). A **shelved commit is itself comparable**: the `G` switcher's `m`+`m` (mark-two) and `c` (cross-picker, against a commit bookmark) whole-tree-compare two commit entries via `domain.ResolveCommitEntryEndpoint` + shelf-aware `CompareFiles` — live sha-vs-sha while both commits exist, falling back to the shelved side's frozen tar (scoped to its own changed-file set) after a gc; `gg compare shelf:<id> …` is the CLI equivalent.  **Three entry kinds** (`model.ShelfKind`): `File` (one blob), `Commit` (tar of the commit's changed files + optional format-patch, `Origin.Commit` set), `Files` (tar of several working/index files shelved together as one named set, no sha). The rule every consumer follows: member-wise paths (list/read a member, restore one, browse, export, the `gg://` shelf endpoint) key on `IsArchive()` (Commit OR Files); sha-backed actions (cherry-pick, commit compares, `Rev:` links) key on `IsCommit()`. A files set is refused by the sha lanes with a notice; an older `gg` reading the index treats it as a plain file entry. |
| `bookmark`   | Persistent registry of richly-addressed file references ("bookmarks") behind a fixed `Store` interface (default impl: atomic-rewrite `bookmarks.toml` under the XDG state dir, keyed by git common dir; records only, no blobs). Owned by `domain`; frontends never import it (archtest-guarded). A bookmark's **identity = its address** (`model.Bookmark`: worktree/branch/commit/shelf-id/path + state), the **content determinator = a blob SHA when permanent** (committed/shelf → frozen) else **live-by-address** (a worktree's working/index file). `domain.BookmarkBytes` resolves it (git verbs `CatFileBlob`/`ShowFileInDir`/`BlobSHA`); jump/compare/paste reuse the `Differ` + `engine.WriteFile`. Bookmarks also include **path-less commit pointers** (`Bookmark.IsCommit()`: `StateCommitted` + empty `Path`, no blob SHA — the commit is the anchor); created via the Commits `.` menu, `enter` in the `g` switcher whole-tree-compares one (base) against the selected Commits-panel commit (subject), reusing `Endpoint`/`CompareFiles`. `BookmarkBytes`/paste/file-compare are guarded against them. Both a commit bookmark and a shelved commit can be copied to a temp dir (`[t]` in the `g`/`G` switchers, or `gg shelf export`) via `domain.ExportBookmark`/`ExportShelfEntry` → `engine.ExportToDir`. A **commit bookmark is itself comparable**: the `g` switcher's `m`+`m` (mark-two, another commit bookmark) and `c` (cross-picker, against a shelved commit) whole-tree-compare via `domain.ResolveCommitEntryEndpoint` + shelf-aware `CompareFiles` — live sha-vs-sha while both commits exist, falling back to a shelved side's frozen tar after a gc (a bookmark itself has no fallback: a gone bookmarked commit is a `domain.CommitGoneError`, a clear notice); `gg compare bookmark:<id> …` is the CLI equivalent. |
| `profile`    | Writable registry of named git-identity presets ("app profiles") behind a fixed `Store` interface (default impl: atomic-rewrite `profiles.toml` under the XDG state dir). **Two-scoped** — a `global` store (every repo) plus a per-repo store keyed by git common dir; `domain` owns both and merges them, tagging each row's `model.ProfileScope`. Owned by `domain`; frontends never import it (archtest-guarded). Backs the Settings → "Identity & profiles" surface. Identity reads/writes use the `git.ConfigGet`/`ConfigSet` verbs (local/global/effective scopes) + the `domain.Identity` query + the `engine.SetIdentity` op — **the first feature in gg that writes git config** (`internal/config` stays read-only at runtime). |
| `promptstate` | Machine-local UX-memory store (`<state>/gg/prompts.toml`, atomic-rewrite TOML beside `operations.log`): globally suppressed related-option prompt ids + per-repo dismissed notice ids (stage-2 notification center). TUI-owned by design — pure UX, no git semantics, NOT config (no `.gg.toml`/settingDocs); a leaf in the archtest layering DAG. Also per-repo **approved external-tool command hashes** (`ApproveToolCommand`/`ApprovedToolCommands`) — keyed by a hash of the resolved-command *template text*, so approving once covers every run until the config text changes. That key format is `promptstate.CommandHash` (`hash.go`), which lives here beside the store rather than in a frontend: BOTH the TUI's lanes (`tui.toolCommandHash` now delegates) and the web's review lane record approvals in this one file, they cannot import each other, and a private copy in each would silently split the two the moment one drifted. |
| `prefix`     | Writable registry of reusable, templated branch-name prefixes ("skeletons") behind a fixed `Store` interface (default impl: atomic-rewrite `prefixes.toml` under the XDG state dir). **Two-scoped** like `profile` — a `global` store plus a per-repo store keyed by git common dir; `domain` owns both, merges + tags `model.ProfileScope`, and validates tokens (`domain.ValidatePrefixValue`: all `<…>` tokens except `<branch>`, including interactive `<user:LABEL>`). Owned by `domain` (`Prefixes`/`AddPrefix`/`RemovePrefix`); frontends never import it (archtest-guarded). Surfaced by the TUI prefix picker (create-branch `ctrl+p`, create-worktree `p`; a `templateFill` step collects `<user:…>` labels), the Settings → "Branch prefixes" manager, and `gg prefix ls\|add\|rm`. |
| `texttmpl`   | Writable registry of **text templates**: titled, multi-line texts with the prefix token grammar (`model.TextTemplate{ID, Title, Body, Scope, Created}`), the `prefix` twin — two `FileStore`s (`texttemplates.toml` under the `texttemplates` state kind: `global/` + a per-repo key dir), atomic rewrite, no lock. The id is a slug of the TITLE (`ID`, letters and digits of any script; `ErrNoID` for none), unique per scope (`ErrDuplicate` on add and on a rename), rows list alphabetically. Resolution is `template.ResolveText` (not `Resolve`): an unknown `<…>` stays literal, a token never spans a line or contains `<` (so `a < b <user:x>` works), `<branch>` is the current branch verbatim ("" when detached), a known token with bad arguments is an error, substituted values are never rescanned; `template.TextTokens` gives the labels / seq names / "automatic" tokens. Domain (`texttemplates.go`): `TextTemplates`/`Add`/`Update`/`Remove`/`FindTextTemplate` (exact id beats a unique prefix; repo wins a tie), `ValidateTextTemplate` (title ≤ 80 runes one line, body ≤ 64 KiB, dry resolve), `RenderTextTemplate` PEEKS `<seq:…>` counters and `TakeTextTemplate` consumes them and resolves with the numbers it was handed. Frontends: the TUI window `textTemplatesView` (`alt+x`; modes browse / fill / rendered / form / confirm; `y` copies and — only once the copy worked — bumps via `TakeTextTemplateSeqs` and pops the WHOLE window (a failed copy keeps the text on screen); the body goes to `$EDITOR` as a temp `.md` through `handover()` in two messages — `textTemplateDraftMsg` writes the file, `textTemplateEditedMsg` reads it back and removes it; a text that fails validation keeps the form open with the text as the next `draft`), the CLI `gg template list/show/render/add/edit/rm` (`render` takes unless `--peek`; a relative `-F` path resolves against the workdir), and the web overlay (`static/texttemplates.js`; `/api/text-templates` + `/update` `/remove` `/render` (peek) `/take` (`{id, scope}`: bumps the counters of the STORED text after a successful Copy — names never come from the wire)). A render in flight is a state of its own in both UIs (TUI `rendering`/`renderGen`, web `{kind:"rendering"}`): keys are swallowed, esc drops it, a late or superseded result never lands. A save the store refuses (`textTemplateSaveFailedMsg`) reopens the form with the written text as the `draft`. Rules added by the follow-up: a file that cannot be parsed is an ERROR on every store call (never an empty registry — the next write would replace the user's texts); `ResolveText` refuses `<branch:…>` and `<user:>` itself (so the dry resolve in `ValidateTextTemplate` catches them), and a title holds no control character (a TAB breaks the CLI rows); `TakeTextTemplate` consumes its counters through `config.BumpSeqs` (one lock, one write — all or none; `BumpPrefixSeqs` stays best-effort); `FindTextTemplate` dedupes by id, so a prefix naming one id in both scopes resolves to the repo row, and its not-found text names no command (the CLI appends the `gg template list` hint); `ErrDuplicate` is wrapped with the id and the holder's title, and every frontend shows that text; a NEW template is global in all three frontends (`gg template add --repo` for this repo); the TUI form sets `handingOff` from enter until the editor returns (keys swallowed), the fill step windows its fields around the focused one, and a cut title rides `tipFull` to the bottom bar (the all-notes pattern); the web form leaves through `canLeaveForm` (unsaved text asks once), and both UIs select the saved row by id AND scope. e2e scenarios share ONE global store: a scenario that adds a global template shows up in every other scenario's window — use `--repo` there. **Follow-up 2:** a rendered text that is TAKEN by copy (TUI `y`, web `/take`) consumes its counters through `domain.TakeTextTemplateSeqs` (`config.BumpSeqs`, one write) — `BumpPrefixSeqs` stays the best-effort per-name bump for branch prefixes only. CLI exit codes: `findTemplate` returns 2 for an unknown (`IsTextTemplateNotFound`) or ambiguous (`IsTextTemplateAmbiguous`) id and 1 for anything else (a damaged store). The delete question is strict in both UIs (`y` / `n` / esc; other keys ignored). The web overlay keeps its decisions in pure top-level functions (`ttFormContent` — title + text, NOT the scope; `ttLeaveForm`, `ttFormNotice`, `ttConfirmKey`, `ttSelectIndex`) so `texttemplatesjs_test.go` can run them under node; in form mode `showErr` stores the error in `mode.err` and `paintFormNotice` draws error + notice on the one `.serr` line. `FindTextTemplate` reads only the scopes the search covers, so a damaged scope is its answer only then (`--global` ignores a damaged repo store); the web's `storedTextTemplate` (render / take) maps unknown or ambiguous to 404 and a damaged store to 500 with the reason. `textTemplateCopiedMsg.seqErr` carries a failed counter take to the status line (the window still closes — the text was copied). A late web answer (save, delete, the list `reload(id, scope, from)` after either) is routed by `ttAnswerLanding(mode === from, !mode)` for the step object that asked: `step` acts in it, `browse` may redraw the list, `elsewhere` (another step open) only replaces `data` and re-finds the selection — never `mode = null`/`render()`. `close()` clears `mode`, so nothing lands in a closed overlay; failures outside the step go to `opLine`; `form.saving` drops a second save. |
| `shellinit`  | `gg shell-init [bash|zsh|fish]` wrappers (cd-on-switch via `--cwd-file`). |
| `observ`     | Observability: span ring buffer, tracing, redaction, panic dump. `SetSpanSink(w)` mirrors every recorded span (each op + each git invocation, redacted JSON lines) to a writer — the basis of the TUI **operation log** (`internal/tui/oplog.go`, toggled from `,` Settings) and the `--time-track` CLI flag. Also a process-global **failure seam** (`NoteFailure`/`SetFailureSink`/`SessionFailures`): `domain` records every genuine, frontend-surfaced failure (each `query` + `Execute` error path, excluding `context.Canceled`/`DeadlineExceeded`) to a bounded session ring and an always-on `errors.log` (TUI-wired beside `operations.log`), surfaced by the Settings → "Session errors" viewer. |
| `buildinfo`  | Version/commit injected via `-ldflags` at build; when those defaults are untouched, `init` falls back to `runtime/debug.ReadBuildInfo` — module version for `go install` builds, `vcs.revision` (+`-dirty`) for plain `go build` from a checkout — so `gg version` never reports `dev (none)` for a versioned install. |
| `app`        | Wires layers into runnable surfaces (`inspect`, panic `DumpRepo`). |
| `e2e` (top-level) | Declarative e2e harness: scenarios/*.toml → real repo (+ HTTP git server) → in-process CLI runs → semantic state assertions. `[[run]]` carries an optional `stdin` field (a TOML string fed to the command's stdin; "" preserves the prior empty-reader behavior) so a scenario can drive a command that reads stdin, e.g. `gg batch` (`s79_cli_batch.toml`). |

**Diff syntax runs.** `domain.Request.Path` selects the lexer; `plainDiffer`
lexes each side ≤ `MaxSyntaxBytes` (1 MB) when `DifferOptions.Syntax()` is
true (Service: `SetSyntaxHighlighting`, default on, `[ui] diff_syntax`). Runs
live on `Diff.OldTok/NewTok` indexed by source line-1 (never on
`textdiff.Row`, whose cached instances are shared read-only); cache keys gain
an `s:` segment so toggling never serves the wrong entry. TUI: `sanitizeCell`
builds parallel emph/class masks over the tab-expanded runes; `styledRuns`
groups by (emph, class) and emphasis wins. Web: `left_tok`/`right_tok`
`[start,end,cls]` triples → `.tk-<cls>` spans via `renderCell`.

The list/text surfaces reach the same colouring through **`winRow.cls`**, an
optional class-per-display-rune mask `renderWindow` slices alongside the text
in all three long-line modes (nil = the byte-identical plain path every other
caller takes; `cls` wins over `decorate`, and a reverse-video row style —
`selectedRow` — drops it, since reverse would turn per-token foregrounds into
per-token backgrounds). Its callers lex off the UI thread: the blame view
(`lexBlame`, a thin gate over `domain.LexBlameLines`, which reassembles the
content lines and which the web blame overlay shares — `/api/blame` sends
`tok` in `/api/diff`'s `[start, end, class]` shape, painted by `renderCell`,
and the `.tk-*` colour rules are selector-scoped to `table.diff, #blame-body`,
so a new coloured web surface must join that list or its spans paint nothing)
and the "View file" preview (`lexPreview` + `fileContentLinesTok`); both refuse
a file holding a bare `\r` (`domain.HasBareCR`), which they turn into a line
break and `syntax.Lex` does not.

The staging pickers (`H`) open **untouched**: the loaders call
`hunkpick.Doc.StartUntouched()`, which puts every block in `Untouched` (it
resolves to its Current lines, yet `SideState`/`LinePicked` read nothing as
picked and `EnsurePicks` starts from an empty list) and sets `Doc.Rest`, the
mode `ToggleSideAll` returns a cleared one-sided block to. The picker adds the
staging-only rules (`toggleSide`/`settle` in `conflict_picker.go`): a hunk left
with no tick goes back to `Untouched`, and `i` on a hunk with no right-side
line toggles the taken-empty state (`Skipped()`, drawn `— removed`). The
conflict picker (`requireAll`) never holds `Untouched`.

The picker's **side colours** (theme roles `picker_left` / `picker_right`):
`ensureOutput` builds `outSide` beside `outLines` — one `outMark` per output
line from the `ResolvedPicks` provenance, `outNone` for literals, placeholders
and `Untouched` hunks — and `renderOutput` lays the text at `w-1` behind the
`outBar` column (repeated on every display line of a wrapped line). A ticked
line's gutter takes the side colour through `winCell.gutterStyle` →
`cellPiece.preStyle`, which `renderPiece` ignores on a reverse-video cell (the
cursor row stays one plain block).

The **hunk picker** (conflict resolver + hunk staging/unstaging) reaches the
same colouring through **`winCell.mask`** — a `runMask{cls, emph}` per display
rune that `cellPieces` slices alongside the body in all three modes and
`renderPiece` paints with `styledRuns` (empty mask = the byte-identical plain
path; a reverse-video cell style drops it, which is what keeps the cursor row
plain). `emph` is carried but unused today: phase 6's in-view search paints its
hits through it. `lexPickerDoc` assembles the current-side and incoming-side
full-file texts the `hunkpick.Doc` describes — shared literal context plus each
block's own lines — and lexes them CONCURRENTLY under `domain.MaxSyntaxBytes`.
It runs in a `tea.Cmd` (`hunkPicker.lexCmd`) returned from the four open sites
in `model.go`, which push the picker unlexed: chroma costs ~1.8 s on a 1 MB Go
file and the UI thread must not wait for it. The finished runs come back as a
`pickerLexedMsg`, applied only if that picker is still live — anywhere on the
layer stack (`hasLayer`, so a popup opened mid-lex only covers it), or the
conflict process's own `cp.picker` — `setSyntax` invalidates the sanitized
caches and the repaint is layout-stable, since a mask never changes a line's
text or width. `withSyntax` is the synchronous form, kept for tests. The two
sides are numbered INDEPENDENTLY, since a block contributes a different
number of lines to each;
`ensureSan` walks both cursors at once and stores `sanLine{text, mask}` per
line, literal context taking the current side's runs. The output pane assembles
from `Block.ResolvedPicks` provenance so it reuses those very `sanLine` values
— no re-sanitize and no re-lex per pick. A side holding a bare `\r` is refused,
as in `lexBlame`/`lexPreview`.

**Diff view line cursor.** The full-screen diff view (`internal/tui/diff_cursor.go`)
keeps a current line, `curLine`, indexing `diffView.lines` (the logical
stream: one entry per aligned row plus fold separators), never `disp` — a
wrapped line owns several display rows, and `lineStart` maps the cursor to
its first one. It is independent of `cur` (the focused change block): `n`/`p`
seed the cursor through `focusBlock`, but a free scroll (arrows, wheel)
moves neither. `cursorRow()` is the contract later phases (review notes)
anchor on — the row under the cursor, `RightNo` on the new side or `LeftNo`
on a Del row. `alignCursor(mode cursorAlign, body int)` places the cursor
line's first display row at the top/centre/bottom (`alignTop`/`alignCenter`/
`alignBottom`, the `z`-key cycle order); `diffPaneLines(v, w, body, curStart,
curEnd, style)` paints the marked range — the history pane calls it with
`0, 0, "off"` so its rows are never marked. Since §4.7 the cursor also has a
SIDE (`diffView.onOld`, false = the new/right pane): `diffPaneLines` builds two
`cellMark`s per row and gives the cursor mark to that side's cell only, so the
other cell renders as if this were not the cursor row (its attention band wins
there). `cursorRow()` still hands back the whole aligned row; `cursorCell()` is
the side-aware reader — the source text plus that side's line number, refusing
a cell the side has not got (`sidePresent`). The marker DOES paint a gap cell
on the cursor side in `row` mode, which is the one relaxation of the phase-0
"a gap filler is never marked" rule; attention bands still skip them. `e` resolves the editor's goto
syntax via `editorCommandAt(editor, absPath, line)`, keyed off the editor
program name: `vim`/`nvim`/`vi`/`nano`/`emacs`/`micro`/`kak` get `+line
path`, `code`/`code-insiders`/`codium`/`cursor` get `--goto path:line`,
`hx`/`subl`/`zed` get `path:line`, anything else (or `line <= 0`) gets the
plain path — and is gated off when there is no single file to open — `v.compare` (a
two-sided compare has no cursor-worthy file) or an in-flight load/error/
binary/too-large state; `v.rev == ""` opens the live working-tree file
(`editFileAtCmd`), a set `rev` opens a read-only temp copy of `rev:path`
(`openInEditorAtCmd` + `ShowFile`, `viewExternalCmd`).

**Review notes (phase 1).** A note (`model.Note`) is a machine-local record —
address, side, a 1-based inclusive line range, and a fingerprint
(`model.NoteContextHash`: each anchored line trimmed of whitespace, joined
with `\n`, hex sha256) — never git content. `internal/notes.FileStore` keeps
`notes.toml` under `<XDG state>/gg/notes/<repoKey>/`, rewritten atomically
(temp+rename) under a cross-process `notes.toml.lock` (`O_CREATE|O_EXCL`, 2s
retry budget, a lock older than 30s is a crashed writer's and gets broken);
every mutation re-reads under the lock, applies, drops replies whose root is
gone, caps the store oldest-root-first when `[notes] max_entries` is
exceeded, and writes only when something actually changed; `Load` never
writes, so a read-heavy session cannot race the sweep or another `gg`.
`domain` owns resolution: `resolveOne` matches a note's stored range against
its side's current text — active in place, active if `findAnchor`'s outward
scan finds the same fingerprint elsewhere, else stale (clamped into the
file) when the side exists but the anchor doesn't, else orphaned when the
whole side is absent — and `noteSideLines` supplies that per-`FileState` old
side (index for unstaged, HEAD for staged, absent for untracked/shelf,
`<sha>^` for committed). `NotesFor` resolves against a diff a frontend
already holds (zero extra reads); `NotesAt` is the stateless door — reads
both sides itself — for callers with no open diff (the web handlers, and
phase-2 CLI). `NoteCounts` (`ByPath`/`ByCommit`/`ByCommitPath`, the last
keyed `"<sha>:<path>"`) is cached on the Service and invalidated by every
mutation, by the startup sweep when it drops anything, and by the exported
`InvalidateNoteCounts` — which the two EXPLICIT refresh paths call (the TUI's
`srcNotes` read, which is never polled, and `GET /api/notes/counts`) so a
badge cannot outlive its notes and a second `gg`'s write is visible; it
counts unresolved THREADS only, so painters never touch the store or read
file content, and `}`/`{`'s file-step reads `ByCommitPath` to find the next
annotated file without a store round trip. `NoteAdd` REFUSES a note whose
side it cannot read or that names an absent side rather than storing an empty
`ContextHash` (which can never re-anchor, so the next sweep would silently
delete it); `NoteEdit`/`NoteReply`/`NoteRemove` return `domain.ErrNoteNotFound`,
a sentinel wrapping `notes.ErrNotFound` so a frontend can `errors.Is` it
without importing `internal/notes`. Every note is
worktree-scoped except commit and shelf notes (worktree-agnostic): the store
is shared by every worktree of a repo, so `sameNoteTarget` and the sweep both
match on the note's own `Address.Worktree`, never the calling Service's. The
startup sweep (`StartNotesSweep`, a per-`Service` `sync.Once`) runs off-thread
from the same three sites a frontend applies `[notes]`/`[versions]` config
policy (`internal/tui/load.go`, `internal/tui/source.go`'s
`applyUIPolicies`, `internal/web/settings.go`); it drops a note only when it
has expired past `max_age_days` OR resolves as non-active, and a read that
merely FAILED (a parked reservation, a timeout) never counts as "gone" —
only a git/OS "not there" answer does, so a slow mount or a paused op can
never wipe the store. **Which diffs carry notes.** The address is stamped by the LOADER, on
`diffView.noteAddr` (TUI) / `state.diffCtx.notes` (web), never derived from
panel focus at key time: focus cannot tell a staged diff from an unstaged one,
and `Address.State` is exactly the pair of texts the sweep re-reads, so a
misfiled note resolves against the wrong side and is deleted. The addressable
loaders are the Status panel (`StateUnstaged`, or `StateUntracked` for a file
with no index entry), the Staged panel (`StateStaged`), a commit file diff and
the file-history diffs (`StateCommitted`, `hash^` -> `hash`). Everything else
leaves the address ZERO and notes are inert there — every two-sided
comparison, the shelf-vs-working and bookmark-vs-working diffs, and the web's
compare mode (no ◆ rows, no keys, no ◆N badges on the file list): their old
side is the compared revision, which no stored address names.

In the TUI, `dRow.note *noteLine` is one synthetic
DISPLAY row `relayout` appends after its owning line's content rows. A thread
lays out as a hunk-style BOX (`noteBoxLines`: `noteRowTop` carrying the title
"agent note · author · path R204", a blank, the summary rows in bold, the
rationale rows, `↳ author:` reply blocks, a blank, `noteRowBottom`), drawn by
`noteRowCells` in the pane of the note's side (old = left, new = right) with
the other pane blank; text is word-wrapped to the pane at layout time
(`noteInnerWidth`, so a pasted paragraph in the summary takes rows, never a
cut), dimmed grey when stale, filtered per-row by the `a` agent-layer toggle
(frame rows count as agent only when the whole thread is). `cursorDispRange`
stops before the first note row so the cursor never rests on one, and a note
hidden under a fold marks the fold separator (`dRow.noteMark`) instead. Web
mirrors the same shape: one `<tr class="note">` per thread keyed `data-note`
(reply blocks carry their own `data-note`), `noteBoxHTML` placing a
`.notebox` in the side's pane (`td.note-gap` fills the other) and spanning
the row in the single-column layouts; a `notes` SSE event (`emitNotes`)
tells every open page to re-fetch after a mutation. `srcNotes` is a refresh source (badges only) — it is never polled
by the background scheduler, only fired after a note mutation.

Two TUI `.` rows are WHOLE-DIFF rather than cursor-scoped (`diffHasNotes`: a
diff on top carrying ≥1 thread), so a file's notes are reachable without first
hunting for an annotated line. `note-list` (`notes_list_popup.go`) lists one
row per root in `v.notes` order — `◆ new:15  ada  summary  +2`, the ◆ painted
by `rowDecorator` in the thread's frame colour (plain on the reverse-video
selected row), only the summary trimmed so `+N` survives a narrow box —
type-to-filter over summary+author, `enter` lands the cursor via `gotoNote`
(the `}` fold-expand path, then `revealCursorNotes`), `esc` closes.
`note-remove-all` (`note_remove_all_popup.go`) is a TYPED confirmation on the
`remove all` token (trimmed, case-folded) behind `domain.NotesClear`, which
drops every note matching `sameNoteTarget` — roots and the replies that
inherit their address — in ONE `Store.Sweep` write, worktree-scoped like
`NotesFor`. The popup quotes roots PLUS replies, so it names the
same number the notice reports afterwards (they can still differ when the store
holds orphaned notes at the address — `NotesFor` hides those, the clear takes
them). That outcome goes to `m.diffNotice`, the diff surface's own bottom-left
box, NOT just `statusMsg`: a full-screen layer draws no status line, so a
message left there is never seen. Jumping to a thread the `a` layer hides lifts
that layer (view flag AND session flag) before laying out — landing the cursor
on a line whose box is filtered away reads as a dead key — and a jump whose
anchor has vanished says so in the same notice box. `expandFoldFor` is the
fold-expand step `jumpNote` and `gotoNote` share: re-find the anchor in the
REBUILT stream, because a partial-mode index is stale afterwards.

**Notes in merge previews.** A preview note is an ordinary committed note on
the source tip (`FileAddress{StateCommitted, <tip>, Path}`, `Side` new) —
nothing new is stored; what's new is the READ. `domain.PreviewNoteSet`
(`internal/domain/previewnotes.go`) is a preview's note scope: `Source`/
`Target` (the pair's saved NAMES), `Tip` (full sha of the source tip — the
write target), `Base` (merge-base(target, source)) and `Commits`
(merge-base..source, newest first, via `RevListRange`, cached alongside
`PreviewSummary`'s hash pair under `preview-revlist:<srcHash>:<tgtHash>`); the
zero value (`OK() == false`) means "not previewable" and is NOT an error —
callers just show no notes and no badge (spec ruling 6). `DiffSpec()` builds
the ONE patch (`<base>..<tip>`) every --preview surface numbers hunks
against, so hunk numbers can never drift between `gg diff --preview --hunks`
and `gg note add --preview --hunk N`. The gather-and-resolve rule:
`loadPreviewNotes` walks the note store ONCE against `set.commitSet()` (a
membership map, not one store query per commit) and keeps only committed,
NEW-side notes whose commit is IN the set — old-side notes on those commits
are ignored (they belong to that commit's own parent→commit picture, not the
preview's merge-base→tip one) — then `PreviewNotesFor` (caller holds a
`Diff`, the TUI) / `PreviewNotesAt` (no diff — web/CLI/MCP, reads the tip's
file content via `ShowFile`) resolve those notes against the tip's CURRENT
text through the same `resolveNotes` the ordinary note path uses.
`PreviewStatus(model.NoteStatus) string` maps `NoteStale` → `"outdated"` at
render/wire time ONLY — the CLI's `noteStatusWord` feeds it into
`renderNoteLine` (`internal/cli/note.go`), `notewire.ToWireNotePreview`
feeds MCP/`GET /api/preview/notes`, the TUI's `noteBoxTitle`
(`internal/tui/diff_notes.go`) checks `v.previewSet != nil` to say
"outdated" instead of "stale", and the web's `noteBoxHTML`
(`internal/web/static/files.js`) does the same off `state.diffCtx.preview`
— `model.NoteStatus` itself gains no value, so the store and resolver stay
untouched. A note whose
path is gone from the tip resolves orphaned and is hidden by `keepResolved`
but still counted: `PreviewNoteCounts` counts from the STORE, unresolved,
cached on `Service` in a plain `map[string]previewCountEntry` keyed
`tip+":"+base` (NOT `factory.Cache("preview")`, which is keyed only on
hashes and would survive a note write), dropped whole by
`invalidateNoteCounts`. A note whose commit left the branch entirely (a
rebase) is excluded earlier, by the `commitSet` membership test itself, so it
is neither shown nor counted. In the TUI, `diffView.previewSet
*domain.PreviewNoteSet` is the stamp a preview diff view carries (set by the
loader in `files_view.go`, copied across a fresh view by `inheritIdentity` —
ruling 17's extracted method, the one trap where `*dv = *msg.view` would
silently drop it); `note_keys.go`'s `previewNoteSet`/`previewNoteScope`
read it (never Model state at key time) to route note reads through the
preview path. The old-side refusal is three separate sites sharing only
the wording "notes in a preview anchor on the new side", never code:
`openNotePopup` (`note_popup.go`) checks `m.previewNoteSet() != nil` when
`noteAnchorsAtCursor()` comes back empty and sets `m.statusMsg` via
`i18n.T` (a cursor on the merge-base-only side has no anchor to offer);
`steer_attn.go` refuses a steering `highlight add` the same way, scoped to
the TIP commit via `previewNoteScope()` (an English literal, not
`i18n.T` — ruling 13, agent-facing protocol); and
`domain.ErrPreviewOldSide` (`internal/domain/previewnotes.go`) is the
separate, narrower case inside `PreviewHunkAnchor` — a `--hunk N` that
resolves to the OLD side (a delete-only hunk) — shared by the CLI's
`gg note add --preview --hunk N` and the MCP note tools. `note_remove_all_popup.go` scopes "Remove
all notes…" to the tip's own notes, hiding the row entirely when nothing
on the tip is removable (ruling 20).

**Phase 2 — the agent lane.** `domain.DiffHunks`/`HunkRange` parse git's `@@`
headers (`ParseDiffHunks`, pure) and `(*Service).HunkDiffSpec` is the ONE rule
for which patch a hunk number refers to: a bare commit means that commit's own
change against its parent (`<c>^..<c>`), the pair of texts a commit note
anchors to, NOT `git diff <c>`. It is a `*Service` method, not a pure
function: a bare commit is probed for a parent via `ResolveRev(ctx, rev+"^")`
first, because a ROOT commit has no `<c>^` to diff against — `<c>^..<c>` fails
outright, and git's `<c>^!` shorthand (tried and rejected: verified against
real git, it silently degrades to plain `<c>` — index/worktree vs `<c>` — the
instant `<c>` has no parent, since `<c>^!` desugars to the plain rev with no
parent to exclude) does NOT fix it either. A root commit instead diffs
against `domain.EmptyTreeSHA1`, git's well-known empty-tree object id.
`domain.NoteTarget` is
the shared flags→`FileAddress` rule (`ErrNoteTargetUsage` = exit 2);
`NoteAddresses` enumerates targets for a bare `gg note list`; `NoteGet` lets the
batch importer validate every `replyTo` before the first write.
`domain.WireNote`/`ToWireNote` is the one JSON note shape (web, CLI `--json`,
MCP). `internal/notebatch` is a pure leaf parsing BOTH agent batch shapes
(agent-context v1 and `comment apply`), shared by the CLI, MCP and the
`gg review --notes` importer; hunk's `low|medium|high` confidence maps onto
`model.Note.Confidence` as 0.3/0.6/0.9. Every `gg note …` verb applies
`[notes]` from the effective config, does its work FIRST, then waits at most 2 s
for the startup sweep (`WaitNotesSweep`). `agentskill` now carries a `Skill`
value type with two instances (`UsingGG`, `ReviewingWithGG`), each with its own
`gg:<name>:vN` marker; `agentinit` installs both per agent and reports the WORST
of the two statuses in its single `Detection` row.

**Merge previews.** A saved preview (`model.MergePreview`, `internal/preview`'s
TOML store under `<state>/gg/previews/<repo-key>/previews.toml`, atomic
rewrite + lock, records only) stores the pair as branch **names** — never
hashes — so it keeps working across rebases, resets and remote updates.
Everything downstream resolves those names against the LIVE repo: `domain`'s
`PreviewSummary(ctx, source, target)` does two `ResolveRev` calls first (a
miss returns `PreviewMissingSource`/`PreviewMissingTarget` without touching
the cache), then builds the cache key from the two resulting **hashes**
(`"preview-summary:" + srcHash + ":" + tgtHash`) — names never reach a cache
key, so a renamed-then-recreated branch or two differently-named branches
sharing a tip can never collide or serve a stale entry. Only a genuine tip
move (a different hash pair) costs the three summary git calls
(`MergeBase`, `CountLeftRight`, `DiffNameOnlyRange`); an unchanged pair is
two rev-parses and a cache hit. `PreviewOpen` wraps `PreviewSummary` into the
two `model.Endpoint`s (`merge-base(target, source)` and the source tip) the
compare pipeline actually diffs. The TUI's `srcPreviews` source (`readPreviews`
in `preview_panel.go`) is, like `srcNotes`, **never interval-polled on its
own** — but it is not merely manual either: `chainPreviewsRead` (`preview_open.go`)
rides along every `srcBranches`/`srcRemotes` arrival (`model.go`'s `previewsChain`,
set in the `dataAvailableMsg` switch and batched into whatever command that
arm returns, skipped only on the startup fan-out, which already reads
previews directly), because a saved pair or an open preview names branch
tips that only those two refreshes can move — plus an explicit `r`, a repo
reroot, and every preview mutation (add/rename/remove/save-reversed). Every
CHAINED previews read goes through `chainPreviewsRead`, never a bare
`reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{})`: the chain bumps
`srcGen`, so if a MANUAL previews read is still in flight its message
early-returns on the arrival handler's gen check BEFORE clearing
`srcLoading[srcPreviews]` — and a silent chain never sets that flag, so
nothing clears it again and `m.loading` (with every action guard and `r`
itself) sticks true for the session. `chainPreviewsRead` inherits the
in-flight read's manual flag so the superseding read clears it instead.
The races are real, not theoretical: at startup bootstrap's manual
all-source fan-out runs against the very snapshot whose arrival chains, and
a mutation chain is one keystroke from `r` (add, save, `r`). Since
branches/remotes ARE on the background auto-refresh lane, this chain is what
lets an idle TUI notice a moved tip and re-open a preview without the user
pressing anything. The legacy snapshot-load path additionally CHAINS a
`srcPreviews` reload off the snapshot's own arrival rather than firing it
from `reRoot` itself, because a state-dir read plus a couple of rev-parses
would otherwise win the race against the full `Snapshot` load and flip
`m.ready`/`m.loading` early, dropping `reRoot`'s blank-screen gate while the
OLD repo's branches/status were still in the model. `reRoot` still bumps
`srcGen` for every source, including `srcPreviews`, before any of this — so
a chained read that lands after a second, faster repo switch (or a switch
away and back) carries a stale generation and is dropped on arrival, the
same guard every other source relies on. Two decisions: `preview-pair` (`internal/tui/preview_actions.go`,
raised from the Branches pair picker) offers `["show once", "show and
save", "swap direction", "abort"]` — "show once" opens the compare view
transiently (empty id, nothing stored, though an open transient preview still
re-arms on tip movement like a saved one), "show and save" saves without
switching focus onto the Previews tab (so the dialog can fire from any tab),
"swap direction" re-raises the same dialog with source/target reversed rather
than making the user re-pair in the other order, and "abort" is last so
`abortOption`/esc cancels; `preview-remove` (the Previews tab's `d`) offers
`["Remove", "Cancel"]` with "Cancel" last for the same esc-safety reason. An
open preview reacts to every `srcPreviews` landing (`afterPreviewsRefresh` in
`preview_open.go`): it re-opens itself (and says which side moved) when a
tip change altered the compare tag, silently reconciles — updating only the
tracked `srcHash`/`tgtHash` on `previewOpenState`, nothing the user sees —
when a tip moved (e.g. the target advanced off the fork point) without
changing the merge-base..source diff, so a later refresh doesn't see them
differ and re-announce the same non-event forever, and closes with a notice
when the pair's state stopped being `PreviewOK` (merged, a side went
missing, no common base) or the record itself was removed.

Entry point: `cmd/gg/main.go` — routes `shell-init`/`inspect`/CLI subcommands, else launches the TUI.

### Theme (`internal/theme`, `internal/tui/styles.go`, `internal/tui/paint.go`)

The role table (every field of `theme.Theme` plus what `Terminal`/`Dark`/`Light`
pin each one to) lives in the spec, not duplicated here:
`docs/superpowers/specs/2026-09-09-tui-themes-design.md` §3/§3.1. `theme` is a
pure DAG leaf (no lipgloss/tui imports); `internal/tui/styles.go` builds a
`styles` struct of lipgloss styles from a `theme.Theme` (`buildStyles`), and
`internal/tui/paint.go`'s `paintFrame` lays the theme's `Bg`/`Fg` under every
rendered cell as a post-process over `View()`'s output.

**`st()` — never cache across a switch.** `st()` reads the active `*styles`
through a package-level `atomic.Pointer[styles]`; `setTheme` swaps it wholesale
(never mutates the struct in place), so a live theme switch races with
nothing (see `internal/tui` NEVER assigning package-var styles directly — that
was the pre-theme pattern and would be a data race under `./test.sh race`).
The rule for call sites: call `st()` fresh every time a style is needed —
**never** stash its result in a package var or a struct field that outlives
one render — and hoist the `st()` call at most once per render *function*
(not once per file/package): a local var caches the atomic load so a
function with many style reads pays it once, and freshness comes entirely
from never letting that cached value outlive the function call, not from
any ordering guarantee against `setTheme` (Bubble Tea's `Update`/`View` run
on one goroutine and `setTheme` fires from the Settings `Update` path, so a
swap can't land mid-`View` anyway; the atomic pointer's real job is letting
the TUI's own parallel tests build styles for different themes without a
race, per the comment atop `styles.go`).

**`paintFrame`'s contract.** `paintFrame(frame, w, h, bg, fg)` is a no-op
(`return frame` unchanged) when both `bg` and `fg` are empty — this is what
makes the `terminal` theme byte-identical to the pre-theme renderer. When
either is set, it renders a probe space through a `lipgloss.Style` to harvest
the profile-downgraded SGR prefix, then: (1) re-asserts that SGR after
**every** reset lipgloss/cellbuf emits inside a line — both the full
`"\x1b[0m"` form `lipgloss.Style.Render` uses and the bare `"\x1b[m"` form
(`ansi.ResetStyle`) that `cellbuf.Wrap` injects when a `Width`-bearing style
wraps already-styled input (missing the bare form was a real regression fixed
separately — see `36cdba99`); (2) pads every line to `w` display cells using
`ansi.StringWidth` (wide-glyph aware — a byte/rune count would under-pad a
line with CJK or box-drawing content) — a line already `≥ w` is never
truncated; (3) pads the whole frame to `h` lines with fully-painted blank
rows. The SGR is harvested from an actual lipgloss render (not hand-built),
so the truecolor→256→16 profile downgrade applies to the painted background
exactly like it does to every other style in the frame.

**`roleFields` — add a role, add a row.** User overrides
(`[themes.<name>]` in the config, decoded into `config.Config.Themes` as
`map[string]theme.Override`) are applied by `theme.Overlay`, layered global→repo
by `theme.Merge`, rendered into `gg config populate`'s commented example blocks
by `theme.RoleDocs` + `Theme.AsOverride`, and read back per key by
`Override.Value`. All five walk ONE ordered table, `roleFields` in
`internal/theme/override.go` — key, description, and pointer accessors into a
`Theme` and an `Override`. So a NEW colour role is: the `Theme` field, the
`roles()` entry, the `Override` field with its snake_case `toml` tag, and a
`roleFields` row. `TestRoleFieldsCoverEveryRole` reflects over the `Override`
tags and fails if the row is missing (and a sibling test catches two rows
pointing at the same field), which is the whole point of the table — nothing
downstream hand-writes a per-role branch. `Overlay` never fails: an invalid
value is skipped and reported as `key=value` for the status bar, an `""` entry
inside `lanes`/`syntax` means "keep the theme's own" (so the generated
`terminal` block, whose values are all empty, is inert), and a wrong-length
list is rejected whole. `applyTheme` compares the RESOLVED theme (not its
name) before returning `tea.ClearScreen`, because a repo switch can change
colours under one theme name.

**The colour editor ↔ writer contract.** Settings → "Theme colours…"
(`internal/tui/theme_editor_popup.go`) edits ONE role at a time, addressed by
`theme.RoleRef` (`internal/theme/roles.go`): all 54 of them — the `roleFields`
scalars, then `lanes[0..6]`, then `syntax[Plain..Attr]`. Because `theme` is a
DAG leaf it MIRRORS the syntax class names; `TestThemeSyntaxRoleNamesMatch-
SyntaxClasses` in `internal/tui` pins that mirror (and the `syntax.Class`
order) against the real constants. The popup keeps the global and repo
override layers APART — `m.cfg.Themes` only carries their merge — because it
writes the global one and refuses a role the repo file pins (a `(repo)` row):
a line written under a repo shadow would silently do nothing. Preview always
goes through the same pipeline as save (`Overlay(base, Merge(global', repo))`,
then `setTheme`), never through `RoleRef.Set` on the RESOLVED theme, where an
empty value would mean "inherit the terminal legacy literal" instead of "the
theme's own default".

Writes go to `config.SetThemeRole` → `setLineInSection`
(`internal/config/write.go`), `setScalarLine`'s sibling for a dotted,
possibly COMMENTED header. Its rules, in order of how easy they are to get
wrong: a commented header (`# [themes.light]   # … [populated]`) counts as the
section and is uncommented IN PLACE — appending a second `[themes.light]` is a
TOML parse error, i.e. a gg that will not start, which is why every writer test
round-trips through `Load`. That substitution is gated on `hasActiveSection`:
once an ACTIVE table of the name exists anywhere in the file it wins, and a
commented same-name header is an ordinary boundary. Both orders corrupt the
file otherwise — a commented block BEFORE the real table gets uncommented into
a duplicate, and one AFTER it has its `# key = …` line activated where it sits,
inside whatever active section precedes it (`[debug].bg`), which parses fine and
loses the colour on the next start. Any bracketed line outside a multi-line
string is a boundary too, including shapes this writer cannot name
(`[themes."my theme"]`), so a key can never leak across one; a commented role line inside the block is replaced
in place (its `[populated]` doc tail is dropped, so a later `d` DELETES that
line rather than re-commenting it); removal re-comments only a line that still
carries `[populated]`; `sectionHeader` treats a commented header as ending the
previous section but rejects anything that is not a bare bracketed name, so a
shell line inside a `[[tools.command]]` script is never mistaken for one. A
list role writes its WHOLE array line, but only the edited entry carries a
colour: `themeSetRole` expands a missing list from the ZERO theme, so the
untouched slots stay `""` ("keep the base value"). Expanding from the real base
— `RoleRef.OverrideSet`'s other mode — would pin six lanes to today's palette,
mark them all overridden, and leave `d` unable to take the list back to unset.
The whole-theme reset (`D`, a `confirming` sub-mode of the popup: `y` fires,
any other key keeps) goes through `config.RemoveThemeTable`, which deletes the
ACTIVE `[themes.<name>]` table only: gg-written rows are dropped, rows still
carrying `[populated]` are re-commented, and the header follows the body —
re-commented when anything non-blank survives (a `[populated]` row — the table
was the populate example block, which must survive as an inert example — or a
comment of the user's own, which must not drift into the section above),
otherwise removed, with the blank that would have been left doubled or at
EOF. The header cannot be judged on its own because `setLineInSection`
rewrote it to a bare `[themes.<name>]` when it uncommented it, dropping the
marker. The popup counts overrides with
`themeOverrideCount` (rows whose `OverrideGet(global)` is set — the `*` rows)
and refuses to ask when it is 0, so nothing is ever written for nothing.
`textfield.HandleEditKey` maps Delete with the cursor at the END of the buffer
to a backspace (the user's erase key sends `^[[3~`); the cursor-less string
filters carry `tea.KeyDelete` in their Backspace cases for the same reason.
`d` (and an emptied field, which takes the same path) blanks the entry in place
and drops the line once every entry is empty again.

**Serial-test rule.** Both `setTheme` (swaps the process-global `styles`
pointer) and `lipgloss.SetColorProfile` (process-global) make any test that
exercises a live theme swap or a color-profile downgrade **serial** — no
`t.Parallel()` — restoring the prior value via `defer`; see
`TestSetThemeSwapsAndRestores` in `internal/tui/styles_test.go` and the NOTE
comment atop `internal/tui/paint_test.go`. Tests that only build/read styles
for a fixed theme (no swap, no profile change) stay `t.Parallel()` as normal.
The package's `TestMain` also points `XDG_CONFIG_HOME` at an empty dir: every
model built through `loadCmd` reads `config.DefaultGlobalPath()`, so without
it a developer whose own global config sets `[ui] theme = "light"` had that
theme applied by the serial settings tests and left active for every later
parallel test reading `st()`.

**Colour-profile caveat.** `dark`/`light` use truecolor hex (`#rrggbb`) and a
few 256-cube indexes; lipgloss's automatic profile detection downgrades hex
to the nearest 256-colour cube entry on a `TERM=xterm-256color`-class
terminal (visually close, not identical) and to the nearest of 16 ANSI colours
on a plain 16-colour terminal — at that point most of the theme's role
palette collapses onto a handful of basic slots and the theme is, in
practice, off. `COLORTERM=truecolor` (or a terminal that sets it) is what
gets the intended truecolor rendering; headless capture tooling must export
it explicitly since tmux hands new sessions the environment it was started
with (see `driving-tui-headless`).

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
- **A window opened from a popup returns to that popup when closed** (ruled
  2026-09-16 after *Previous versions… → enter → esc* dropped the user onto the
  panels instead of the versions list). The popup is hidden while the window
  is open, never destroyed, so it comes back with its own state (branch, row,
  drilled-from-list flag). Layer-stack windows get this for free from
  `pushLayer`/`popLayer`. The files view is NOT a layer, so a popup handing
  off to it goes through `Model.handOffToFilesView(open)`
  (`internal/tui/layer_stack.go`): it takes the stack off the model, runs the
  opener, then parks the taken stack in `Model.filesReturnLayers` — armed
  AFTER the opener because every opener's clean slate (`beginFilesView` →
  `closeFilesView`) zeroes the field. The view's esc/`l` handlers read the
  field before `closeFilesView` and `restoreParkedLayers` it; every other
  teardown (repo switch, steer navigation, narrow terminal, *Commits touching
  this*, `closePreviewView`) drops it because `closeFilesView` zeroes it. A
  re-open inside a live view (enter on the commit list to drill into the tree,
  a switcher pushed over the view) carries the park across (`beginFilesView`);
  a popup opened over an already-open view replaces the park — latest opener
  wins. Hand-off sites: `versionsPopup.onEnter` (both branches),
  `compareCommitBookmark`, the `entryCompareMsg` handler, the shelf switcher's
  shelved-commit enter (`openShelfCommitFiles` no longer touches the stack).
  `clearLayers` remains only for a popup handing off to an *operation*
  (identity apply, Reset branch, New branch at version, cherry-pick, apply
  patch): the op changes what the popup listed, so it lands in the panels.
  Tests: `internal/tui/files_return_layers_test.go`.
- **`internal/tui` and `internal/cli` never import `internal/git`** — they reach git through `internal/domain` (guarded by `internal/archtest`). `cmd/gg` and `internal/app` are the composition root and may construct concrete git types.
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

To read the TUI headlessly (no terminal/VM) — e.g. when porting a panel to
another frontend — `./tui-capture.sh "<keyscript>"` drives the real `gg` under
a tmux PTY and writes a plain-text snapshot per screen; keystrokes run against
a live repo, so point `--repo` at a scratch clone for anything beyond
navigation. See the `driving-tui-headless` skill.

To author a scenario for that harness, `gg --record <file>` dumps a live TUI
session's keystrokes to `<file>` in the same keyscript format (quit excluded);
replay it with `tui-capture.sh` (pass the recording file as the positional —
a file-mode replay, one keystroke per line, no `;`/`label:` splitting).

## Development workflow

Features follow: **brainstorm → spec → plan → subagent-driven execution → human merges**
(superpowers skills). Specs live in `docs/superpowers/specs/`, plans in
`docs/superpowers/plans/`, one feature branch off `main` per feature, with a
final review before merge.

**Project skills** (in `.claude/skills/`): `adding-features` — the full
engine→TUI→CLI wiring checklist for a new operation/command;
`adding-tui-windows` — panel vs popup vs modal taxonomy and wiring;
`writing-e2e-scenarios` — schema + operation contracts for authoring
`e2e/scenarios/*.toml`; `adding-external-tools` — the checklist for adding
a new agent/tool to the `exttool` catalog pool (verify against the real
binary, template rules, OptIn variants); `defining-agentic-tasks` — what an
AI agent is expected to DO per task category (conflict / commit_message /
review contracts, the prompts that encode them, and the using-gg sync rule
for any agent-facing surface change); `debugging-clipboard-copy` — triage for
any "copy doesn't work" report (the environmental causes that look like gg
bugs, and why a green "Copied …" proves nothing); `handling-git-locks` — how
git's lockfiles work, why cancelling a git subprocess strands them, and the
two-layer fix (graceful SIGTERM + detect/remove); read it before touching any
code that cancels, kills, or times out git, or that removes files under `.git`.
Use them whenever adding a feature, TUI surface, e2e scenario, external tool,
or agentic task — or when triaging a copy/paste or git-lock failure.

**After each completed stage/feature, update the project docs:**
`CHANGELOG.md` (always), `README.md` (if user-facing surface changed), this
`CLAUDE.md` (if the architecture/package map/conventions changed), and — when
the CLI surface changed — `internal/agentskill/using-gg.md` (bump
`agentskill.Version`, then `gg init --update` to refresh installed copies).

## Status

See `CHANGELOG.md` for what's shipped. `gg mcp` stage 1 (read-only: UI state,
bookmarks/shelves, compare, export) and stage 2 (mutating: cherry-pick,
write-to-worktree, both gated by the MCP client's consent prompt) have
shipped. Roadmap: workspace group sync (named repo groups + parallel
background-pull; needs concurrent-op decision routing, shared with MCP), then
the remaining MCP surface — heavy ops (staging, interactive rebase, conflict
editor, diff, visual graph, sparse-checkout).

### Live steering (`internal/steer`, phase 3)

**Layout**, under the phase-0 session dir
`<state>/gg/sessions/<EncodeRepoKey(commonDir)>/`:

```text
ui-state.json                      # the phase-0 snapshot, repo-level, untouched
steer/<EncodeRepoKey(worktree)>/   # ONE inbox per WORKTREE
  tui.json                         # {"pid","worktree","started"}; removed on exit and on reRoot
  web.json                         # + {"url"}
  cmd-<unixnano>-<pid>.json        # one command, temp+rename, unlinked by the consumer
  reply-<id>.json                  # the answer, unlinked by the CLI that read it
```

The snapshot is keyed by the git COMMON dir and shared by every worktree; the
inbox deliberately is not, so `gg session navigate` run in worktree A steers the
window showing A. Both re-home on `reRoot` through the same
`snapshotTargetMsg`.

**Liveness is a fresh mtime**, never a pid probe: each session re-touches its
presence on the 1 s tick (`heartbeatMsg` in the TUI, a ticker goroutine in
`gg web`), and `steer.Live` reports a presence under 5 s old (`steer.LiveWindow`),
sweeping an older one. One `os.Chtimes`/`os.Stat` per second, no per-OS process
API, immune to pid reuse. `Presence.PID` is display-only.

**Command ids** are `<unixnano>-<pid>` (`steer.NewID`), which sorts by post
time — `Drain` returns commands in name order. A command over 16 KiB
(`steer.MaxCommandBytes`), unparsable, or carrying no `id` is deleted and
dropped: a reply file is named after the id, so an id-less command is
unanswerable by construction. `wait:false` gets no reply. `Discard` sweeps
`cmd-*` AND `reply-*` at startup, so a crashed session's commands never replay
and a reply the CLI abandoned on timeout never outlives its session.

**Two sessions on the same worktree both drain the same inbox**, and whichever
unlinks a command file first applies it. This is rare (a second TUI on one
worktree), deliberately not guarded, and recorded here rather than arbitrated:
any lock would have to survive a SIGKILL, which is the exact problem mtime
liveness exists to avoid.

**Routing, the reply wait and every exit code live in `internal/cli/session.go`**
(`sendSteer`/`routeFor`): no live session for the worktree is exit 1; when both
a TUI and a page are live, one command id (`c.ID`, filled by `steer.NewID` if
unset) goes to both — one HTTP POST to the page, one inbox file to the TUI —
so a single reply can be correlated against a single request. `navigate`'s wait
blocks up to `steerReplyWaitForTest` (a 2 s `var`, shortened only by a test;
production never touches it); an unanswered wait prints `queued: no answer from
the TUI within 2s` and exits 0 — the command is still in the inbox and may yet
land, so a slow TUI is not reported as a failure. The `--next-comment`/
`--prev-comment` arm and the bare `--rev` arm of `gg session navigate` each
refuse every other target flag (`--file`, `--cached`, `--hunk`, `--new-line`,
`--old-line`) at exit 2, so a target-shaped mistake reports usage instead of
silently dropping half the command.

**Refusal rules** (`Model.steerRefusal`, `internal/tui/steer.go`) — a command is
answered `ok:false` with a reason and applied to nothing when: an op is running
or a load is in flight (`!m.opsIdle()`), a decision modal is up (`m.modal`), a
process owns the screen (`m.proc`, which is where the conflict resolver lives),
the action menu is open, any typing surface has focus (`filterTyping`,
`highlightTyping`, `recallOpen`, `filesView.typing`, `filesPreview.typing`,
`stashView.typing`), or `topLayer()` is outside the poppable whitelist
(`nil`, `*diffView`, `*historyView`, `*blameView`, `*contentPopup`) — which is
how the hunk/stage picker, the rebase editor and the repo switcher are refused
without enumerating every layer type. A second pre-dispatch check
(`steerEnumRefusal`, same file) mirrors the web endpoint's `toSteerWire`: an
unrecognised `target.state` (`unstaged|staged|untracked|commit`, `""` = the
unstaged default) or `line.side` (`new|old`, `""` = new) is refused with
`unknown target state %q` / `unknown side %q` rather than silently defaulted —
otherwise a bogus state keys a band no open diff can ever match while the agent
is answered `ok:true`, and a bogus side quietly means "new".

**The navigate pipeline** parks a `pendingSteer` and drains it in the very
handler that owns the load it waits on — `dataAvailableMsg`/`srcStatus` for the
one status retry, `commitFilesMsg` for a commit's file list, `diffMsg` for the
landing. The landing must happen AFTER `*dv = *msg.view` in the `diffMsg` arm,
which overwrites `curLine`/`offset` with the loader's values. Every failure
branch routes through `failPending`, and a pending older than
`steerPendingTTL` (5 s) is expired by the heartbeat — a leaked pending would
land the cursor on the next unrelated diff.

**Two deliberate refusals.** A commit that is not loaded in the feed is refused
(`gotoLoadedCommit`, the half of `gotoCommitByHash` that never starts the eager
deep search) rather than triggering a search that can raise a prompt: never
raise a decision on an agent's behalf. A conflicted row is refused rather than
opened: the conflict editor is the user's to open.

**`--hunk N` is resolved by the CLI, never the TUI** — phase 2's `HunkDiffSpec`
+ `HunkRange` (which reads the same `DiffHunks` parse `gg diff --hunks` uses)
turn it into the FIRST line of the hunk's new span (old side for a pure
deletion), so the consumer only ever sees file + target + side + line and one
landing path serves `--hunk`, `--new-line` and `--old-line`. An untracked path
has no git hunks and is refused with "untracked files have no hunks; use
--new-line".

**Attention marks** live in `m.attention map[attentionKey][]steerMark`, keyed by
path + target state + commit. They survive the interval auto-refresh on purpose:
a rebuild nobody asked for must not wipe an agent's bands. They die on
`highlight_clear`, a `reload` whose sources include `status` or `all`, and
`reRoot`. A `notes`-only `reload` KEEPS them: every note mutation (`gg note
add`, `gg review --notes`, the MCP note tools) auto-posts one, so clearing there
would make the documented highlight-then-note flow erase the band it had just
painted; `status`/`all` rebuild the diff geometry the ranges are anchored
against, which is the one real drift reason. Both frontends implement exactly
this rule (`steerReload` in `internal/tui/steer_attn.go` and in
`internal/web/static/live.js`). The cursor row outranks a band in
`diffPaneLines` — in `[ui] diff_cursor = "number"` mode the band still paints
the row, since only the gutter carries the cursor there.

**Web.** `gg web` writes `web.json` with its URL after `listen` and removes it on
shutdown and `reRoot`. `POST /api/session/steer` validates through the same
allowlists as the notes lane, checks the op gate itself (409
`operation in flight` — the CLI prints it and, unless a TUI is also live to
answer, exits 1) and emits on the live hub BYPASSING `liveHub.emit`'s gate,
which would silently drop a one-off instruction. The endpoint answers 202;
there is no page acknowledgement, and the CLI prints `web: sent`. A `reload`
with `sources` containing `"all"` is expanded client-side into the TUI's hard
reload — status, notes, branches (the whole sidebar) and the feed — since the
browser's `refreshSources` only knows the individual names. A `navigate` onto a
working-tree file the agent named opens the working-tree stage
(`openWorkingTree(0)`) before it looks the file up in `state.statusEntries`; if
the file turns out not to be there, the page still ends up parked on the
working-tree file list rather than doing nothing.

**Protocol prose stays English** — every `Reply.Detail`/`Error`, every command
JSON value, every CLI line. Only the on-screen notices the TUI posts
(`"▸ agent opened %s"`, `"▸ agent asked for a reload"`,
`"▸ agent marked lines in %s"`, `"▸ agent cleared its marks"`,
`"▸ agent moved the focus"`) go through `i18n.T` and live in all four bundles.

**Test seams:** the inbox dir is a field (`m.steerDir`, `Server.steerDir`,
`mcp.Server.steerDir`) or a parameter (`cli.runSession(dir, …)`), never read from
the environment in a test — the tui suite runs `t.Parallel()` and `t.Setenv`
panics there.

### gg links (`gg://`, `internal/model/link.go` + `internal/domain/linkresolve.go`)

**Range links (2026-10-02).** `:<a>-<b>` / `:old:<a>-<b>` names lines a..b
on one side: `model.Link.End` (0 = one line; `a == b` collapses to the
single-line link and keeps its lenient rule below), read by `splitLinkLine`
(a range and `#hunk` conflict like a line and a hunk). An UNCOMMITTED range
carries `model.BlockFingerprint` in the same `~<8hex>` slot: FNV-1a 32 over
the trimmed lines joined by `\n` ("" for an all-blank block). The resolver
only CHECKS it (`anchorLink`): a block that is not where the link says —
edited, moved, past the end, unreadable — is `domain.ErrLinkStale` (typed
`*LinkStaleError{Path, First, Last}` so the TUI's `#` prompt can translate
it); there is NO moved state for a range (user ruling: do not guess). CLI
verbs exit 1 through `linkExit`. `Resolved.End` → `steer.Line.End` (via
`linknav.lineOf`; `AtLink` and the TUI's pure `steerCommandForLink` keep it) →
the landing: `landSteer` calls `markLandedRange` (a FROZEN `lineSel`, cursor
on the first line, the fold hiding the end opened, the end clamped to the
side's last line), a parked stack landing carries `stackLanding.end`, and a
content link sets `openFile.pendingEnd` (`landPendingLine` already selected
ranges for overviews). Producers: `diffLinkSelection` (cursor side; first and
last selected row that HAS a number there; the block text from the file's
`full` rows so folded lines count; a stack selection crossing files is
refused) + `rangeLink` (post-processes the first line's link, so every target
kind is covered by the existing builders); `contextFileLinkRow` for the
viewer/preview (`L` there is bound only while a selection is live); CLI
`gg link <path>:<a>-<b>` (`Service.LinkBlockFingerprint`). `L` keeps the
selection. `gg link text` / `gg_link_text` print the lines over
`Service.LinkText` (uncommitted → `linkSideLines`; a commit's `:old:` = its
parent; a pair's = commit a). `gg note add <range link>` stores `Range
[a,b]`; every note whose range spans several lines draws a bar (`▎`,
`cellMark.note` / `diffView.noteSpans`) in the gutter's separator column
beside its lines. The CLI's old `splitLinkRange` shim (highlight add only,
broke on `~fp`) is gone. Web this round: `steerWire.EndLine` + the op line
"the link names lines a-b"; no selection restore, no range copy.

**Range links in gg web (2026-10-02).** State: `state.diffRange` / a stack
slot's `range` = `{side, first, last}` (one at a time, stack-wide), carried
into `diffHTML` on the note context (`nc.range`) and painted IN the render
(`markCls`: `rng-l`/`rng-r` band, `nbar-l`/`nbar-r` note bar) — a class
added after the fact would vanish on the next notes refresh. Band rows are
pinned against folding, so setting a range unfolds it. Pure helpers in
`files.js` (`extendRange`, `rangeRows`, `noteBars`) and `links.js`
(`blockFingerprint`, `linkFor(…, end, block)`), pinned against Go by
`blockfpjs_test.go` / `rangemarkjs_test.go`. The gesture is `rangeGesture`
(shift+click on `table.diff td.no` — matched by the table because the
gesture's own repaint detaches the row before later handlers run); the
note-anchor click, the staging click and the outside-click clear all stand
aside for it (the dblclick stager too). `setDiffRange` repaints and returns
how many of the range's lines the diff holds; `clearDiffRange` strips the
classes IN PLACE (no repaint — `markDiffRow`'s callers hold the row they
marked), and `markDiffRow(…, keepRange)` calls it on a plain mark. The band
is gated on `notesOn` (the history overlay renders through `diffHTML` too).
In the one-text-column layouts the range stays on the mark's side when the
clicked number cell says nothing (a context row, an empty cell). Viewer: `view.range` + `view.rangeOwn` (a hand/link range clears on
click or esc; an overview anchor's band does not), `viewerRange` clamps.
Landing: `live.js` passes `end_line` to `setDiffRange` / `landStackLine(…,
end)` / `markViewerRange`. The web's `c` with a band up writes ONE note over
it: `addNotePrompt` reads `rangeDiff()` (stackview.js — the slot HOLDING the
range, not the cursor slot) and `rangeAnchor(range)` → `{side, first, no}`,
posts `first` beside `line` (`/api/notes/add`: range `first..line`, 0 = one
line; the server leaves the hash to domain), refuses a preview's old side
with no fall-forward, and hands a clear of THAT band to `noteWrite`'s `saved`
hook (success only). The `c` gate reads the same slot (`noteKey`'s ctx
argument); collapsing a stack section drops its range. Follow-ups (2026-10-02):
a link's line is digits only (`linkNum`); `firstHeldLine` lands a range link
on the first line the diff holds; `setDiffRange` repaints only the touched
stack slots (`repaintStackSlots`) and `quietReloadSlot` drops the band of a slot whose
rows changed; `markCls(r, only)` bands a unified one-side row only on the band's
side; `c` refuses a partly held range; the note title names a range
(`R8-10`, both frontends); `domain.ErrNoteRange` → 400. Left on purpose: a
one-line shift+click in a compare still marks the row (it is the only way
to start a range there), bare-CR numbering, a checkout path ending `:<n>-`.
Keyboard marking (2026-10-02, user-picked keys): shift+↓/↑ = `rangeKey` →
`stepDiffRange` (files.js; `markHere` = the band's file, else the marked
row's, else the cursor slot; pure `stepRange` moves the end AWAY from the
mark to the next line the side holds) and the viewer's `stepViewerRange`
(pure `viewerStep`, the cursor is the anchor); `L` = `copyMarkLink` /
`copyViewerLinkHere`, sharing `diffRowLink` / `viewerLinkHere` with the
menus. `markHere` follows `c`: the band's slot, else the anchor slot and ITS
`.row` (collapsing a slot drops its `.row` and `.range`); a preview starts
on the new side; the marked row is pinned against folding; a viewer band
the cursor walked off restarts from the cursor. Tests:
`rangekeysjs_test.go`. `rangeKey` runs before `diffScrollKey`. Narrow layout: a context row
carries `data-ono` (its old line — NOT data-lno, which means "split layout"
to the click code); `diffRowAt` is the one (side, line) → row lookup. Tests: `rangenotejs_test.go`, `TestNotesAddRange`.
Cheap steps (2026-10-02): `setDiffRange` bands IN PLACE (`paintBandInPlace`:
the classes `bandCls` gives in the render — `markCls` calls it — toggled on
the rows entering/leaving, found by a sibling walk from the band's first
`data-i`, `renderedRows`) when the table says it wears bands (`data-band`,
= notesOn; `data-cols="3"` = unified, where a del/add row bands only its
side) and every band row is rendered. Else a repaint: now for shift+click /
landings, deferred for a keyboard step (`repaintBandLater`: one repaint for
the steps a held key makes meanwhile, throttled to half its last cost;
`bandPending.end` is shown after it; a repaint for a diff the reader left is
dropped). Shrinking leaves the rows the band had unfolded (as
`clearDiffRange`). The moving end is kept in sight by `keepRowInSight` —
pure `sightScroll` against `#diff-top` + the slot's `.stk-head` heights and
the bottom bars — never `scrollIntoView`, which parks it under the sticky
headers. The viewer's twin is `paintViewerBand`. Tests:
`TestBandClassAndSight`, `TestRangeKeysCheapStepWiring`; browser probe with a
20k-line file: step 700–1000 ms → ~1–6 ms, viewer ~250 ms → <1 ms, a held
key in the changes-only view settles ~0.2 s after the last key (was ~3.5 s).

**A note over marked lines (2026-10-02).** `c` with more than one row marked
in the diff asks `noteAnchorOfMarks` (`note_popup.go`), which reuses
`diffLinkSelection` (side, trim rule, block from the file's full rows) and
fills ONE anchor `{side, first, line, NoteContextHash(block)}` — no side
field. The address is read where the marks are (cursor lent to `sel.row`, as
`contextLinkText` does), so frozen marks in another file of a stack note that
file. `notePopup.note()` builds the stored note (`Range{first, line}`);
`noteMutatedMsg.clearMarks` clears `lsel` on a successful write only. One
marked row with the cursor on it falls through to the cursor's one-line
note. The selection is read BEFORE the cursor is lent (loose marks end at
the cursor); the gates (address, preview old side) read the marks' file.

**Line fingerprints (2026-10-01).** An UNCOMMITTED line link (working tree,
`@staged`, `?view=content`) may end `:<n>~<fp>`: `model.LineFingerprint` =
8 hex of FNV-1a 32 over the `TrimSpace`d line ("" for a blank line; NOT
`NoteContextHash` — the browser builds links synchronously). It is parsed out
of the tail after the LAST `:` only when a NUMBER precedes the `~`, so a path
may still hold `~`; on a committed target it is refused. `domain.ResolveLink`
re-finds it once for every consumer (`anchorLink` → `linkSideLines`: the
working file, the index for `:old:` / `@staged`, `HEAD` for `@staged:old:`)
and sets `Resolved.Anchor` (`same` | `moved` nearest-wins | `changed`),
never an error, and ONLY for a fingerprinted link (no checkout is opened
otherwise — `TestResolveLinkCwdMatchStopsBeforeTheRegistryWalk`).
`domain.AnchorNote` is the one English sentence (stderr `gg: …`, steer reply,
the web's op line); the TUI's `anchorNotice` is its translated twin, added in
`navigateLanded` — the single landing funnel. `steer.Line` carries
`asked`/`anchor`/`matches`. Producers: `Service.LinkLineFingerprint` (CLI),
`buildLinkFor`'s `text` (TUI, from the row / `contentLine.raw`), `linkFor`'s
6th argument (web, from `d.rows[tr.dataset.i]` — never the rendered cell;
`lineFingerprint` spells Go's trim as a regex because `String.trim` differs on
U+FEFF and U+0085). `gg open`'s `--at` startup lands on the resolved line
without the on-screen note (stderr already said it). A `#` paste that
SWITCHES checkout carries the anchor beside the plain at-link
(`gotoLinkSwitch.line` → `Model.startAtAnchor` → `consumeStartAt` re-attaches
it to the rebuilt command when the line still matches). The web file viewer
fingerprints its cursor line only while `view.src === "worktree"`. `linkSideLines`
applies the Differ's own two rules (`MaxDiffBytes`, `textdiff.IsBinary`) so
the resolver and the view a link was copied from agree on what is text.

**Grammar** (`model.ParseLink` / `Link.String()`, the only place it lives):

```text
gg://<repo>/<path>[@<target>][:<line>][?<hint>]   <line> = <n> | old:<n>
gg://<repo>/<path>[@<target>]#<hunk>[?<hint>]     <hunk> and <line> are exclusive
gg://<repo>@<commit>[?<hint>]                     a commit, no path
gg://<repo>                                       the repository
<repo>   = <name>  remote-named    | /<absolute checkout path>  local
<target> = <7..64 hex> | staged | ref:<name> | <a>..<b> | <target>...<source>
           | (absent) = the working tree
<hint>   = <kind>=<id>,  kind ∈ {bookmark, shelf, stash, preview, version, view}
```

(64, not 40: a sha-256 repository's commit ids are 64 hex characters and every
producer writes the FULL sha.)

**Three target forms, two of which are not a commit.** `ref:<name>` is a
branch or tag TIP and keeps the NAME on purpose — the link addresses "the
branch", not whichever commit it sits on today — so it is UNBOUNDED (a point,
the whole tree there) and its commit is a per-machine, per-moment question.
`<a>..<b>` is git's two-dot CHANGE-SET and is BOUNDED (only what it changed);
each half may be a sha or a refname, unlike a preview's `<target>...<source>`,
which is deliberately two branch NAMES so it travels (two shas there are
refused at parse). `Link.Address()` is meaningless for all three — it yields an
empty commit — which is why `domain.ResolveLink` branches on
`Target.Preview`/`Ref`/`Pair` before reaching it.

**`ResolveLink` REFUSES a ref or pair link**, by design: there is no single
commit to flatten a change-set into, and `model.FileAddress` has no field for a
key set. The checkout/path SPLIT is still needed for those links, so
`domain.LocateLink` was carved out of `ResolveLink` to answer that half alone —
a consumer that evaluates the target itself (`domain.EvalLink`'s callers)
locates, takes the returned `relPath` as the link's `Path`, and keeps the
target it parsed. **Consequence to know:** `gg link --ref main` prints a link
`gg link resolve` then rejects (exit 2, `ErrLink`) with "a branch-tip or
change-set link cannot be navigated yet … hand it to `gg compare`" — the guard
is checked BEFORE the empty-sha one, which used to catch these links and blame
a sha they never had; the message reaches six verbs through
`cli.resolveLinkArg`. `gg compare` reads the same link happily, because it goes
through `LocateLink` + `EvalLink`. `domain.SameCheckout` exports the resolver's own
path-equality rule so a frontend comparing `LocateLink`'s answer against its
`TopLevel` cannot invent a second one.

**The hint is a LANDING, never an address** (spec §3.3). Rule 1: compare
IGNORES it — a bookmarked commit and the same commit picked off the log are one
endpoint, which is what keeps the algebra a 2×2 instead of a
bookmark × shelf × commit matrix. Rule 2: navigate honours it (not wired yet).
Rule 3: it degrades, never fails. A `?bookmark=` hint therefore never changes
an answer at all. `?shelf=` has the spec's TWO exceptions, and only those two —
both cases where the shelf entry is the only place the BYTES survive, so the
hint is a fallback content source, never a second identity:

1. A link with **NO target** — a shelved WORKING-TREE file whose bytes were
   never in git. `EndpointForLink` turns exactly that shape into a
   `ShelfEndpoint`, and `EvalEndpoint`'s shelf arm splits on the ENTRY KIND (as
   `shelfResolve` does for bytes) so a FILE entry becomes a bounded set of ONE
   member, its `Origin.Path` (an entry recording no origin path is refused
   rather than given the key `""`). Before that split the arm sent every shelf
   endpoint through `ShelfCommitFiles`, which refuses a file entry — so the one
   link in the system with no address at all could be BUILT and never
   EVALUATED.
2. A **COMMIT link whose sha no longer resolves** (spec §3.3 table row 1, §6
   "gc'd, frozen tar present → use the tar"): the arm routes through
   `ResolveCommitEntryEndpoint`, the same hybrid `gg compare shelf:<id>` uses,
   so both spellings of one entry answer alike. While the sha resolves it
   returns the plain `CommitEndpoint` an unhinted link would, which is what
   keeps rule 1 true for every live link.

A hint whose kind is outside the closed set, or whose id holds a separator, is
refused at parse rather than carried. `model.LinkHintKindOK` /
`LinkHintIDOK` are that rule, exported so the producers (including links.js's
JS twin) refuse the same things the parser does — one rule, one definition.

#### Navigating a hint, and the per-verb acceptance table (2026-09-17)

**A hint is a PLACE.** `linknav.RepoOnly` reports false for a link carrying
one, so `gg://<repo>?shelf=<id>` navigates: its landing IS the reveal. The
wire shape is a navigate with no `File`, `Commit` or `Target` — only
`hint_kind`/`hint_id` — and all three acceptance points (linknav, the web's
`toSteerWire`, `gg session navigate`) take it.

**Web link comparisons are live, pairs are not.** `linkcompare.js
refreshLinkCompare` re-asks `/api/compare-links` with `state.compare.links`
after a sidebar/status/feed refresh and hands differing rows to `files.js
updateLinkCompareFiles` (in place: cursor follows its path, never
`openLinkCompare`, which resets the screen). It skips `state.compare.pair` and
a comparison that is not on screen, stays silent on error, and drops an answer
whose `state.compare` identity changed meanwhile. The pair file menu's rev rows
use `hereRev = rev || pairCtx().b` and are withheld from a `right_spec` row;
the link contributor still receives the comparison's own (empty) rev.
**`?preview=<id>` — a saved Previews entry (merge preview OR commit pair).**
One kind for both set-shaped entries; a two-link comparison has no single link
and never carries it. Producers are ENTRY-level only: the TUI Previews row
copy (`withPreviewHint` in `tui/link.go`), the web rows (the pair row's
server-built `link` in `web/savedcompares.go`, the merge row in `links.js`)
and `gg link --preview <id|label>` with no path (`savedSetID`; a typed range
— including one that spells a pair's default `<a7>..<b7>` label — names no
entry). A file or line copied inside an open preview stays bare. The store
keeps the bare text. Consumers look the entry up AFTER the address landed —
by id, else by the entry naming the same set (previews: source + target
names; pairs: the two full shas), else a notice — because the id hashes link
TEXT, which spells the repository by name on one machine and by path on
another. TUI: `revealSavedSet` (no load to wait for — Previews is a startup
source); under an open files view it moves the tab + cursor and re-points
`filesReturnFocus`, never the keyboard. Web: `previews.js revealSavedSet`,
routed by `live.js revealHint`, one re-fetch before a miss is believed.
Address-less is refused in `finishLink` by name. Both steer validators ask
`model.LinkHintKindOK` instead of retyping the set. `DescribeLink` reads the
hinted link as `preview: <label>` / `pair: <label>` when saved, else falls
through to the address arms.

**`?version=<unix>-<op>` — a recorded branch version's frozen preview**
(`docs/superpowers/specs/2026-09-28-version-links-design.md`). The address is
the pair the record froze (`@<base>..<ours>`); the id is the ref's last
element and carries no branch (hint ids reject `/`). `domain.FindVersion`
is the ONE lookup (one `for-each-ref` over the store): a record with this id
AND this pair, else this id, else the newest record freezing this pair, else
a miss — the tie-break matters because a pull records both of its branches
in the same second under one token. Producers (`tui.versionLinkFor`,
`cli.versionLinkText`, `web.versionLink`) validate both shas with
`CommitEndpoint` first: the ref trailer is never sha-checked on write. The
TUI lands the pair as usual and then parks a `versionsPopup` on the row
under the open compare (`filesReturnLayers`, esc restores it) — or pushes it
live if the compare has closed; the web pushes its versions layer on TOP of
the landed compare (it has no "under") with the row flashed. The drift
notice's *Copy preview link* is the first `noticeAction{keep: true}`: acting
no longer always removes the notice. Machine-local: version refs are never
pushed and `Ours` is a rewritten tip.

**Presence is checked in two layers, deliberately.** `domain.Resolved` is a
value with no notice channel, so it cannot report a degraded hint. A link
WITH an address carries the hint through `finishLink`'s common prologue
(never a per-target arm — the ref and pair arms `return` early) and the
CONSUMER looks the entry up in its own store, revealing it or noticing.
A link WITHOUT an address checks presence in DOMAIN and hard-errors, because
the hint is the only content source. Spec §3.3 rule 3 versus §6, in code.

**The consumer must agree with domain about presence.** `shelf.FileStore.Find`
scans EVERY bucket; the TUI's `loadShelfCmd` and `/api/shelf` list the DEFAULT
bucket only. An entry put anywhere else by `gg shelf add --bucket <name>`
resolved as present and was then reported "gone" — and the TUI answered
`steerFail`, so the navigate FAILED on an entry that exists. Both consumers
now look across buckets on the reveal path. Do not "fix" this by narrowing
`ShelfFind`: `evallink.go` resolves a shelf id all-bucket for compare, and
narrowing it would make navigate and compare disagree instead.

**Per-verb acceptance (ruling R4).** `linkShapes` is a struct in the
resolver's SIGNATURE, so a caller cannot forget to declare what it takes:

| verb | `@ref:` | `@<a>..<b>` |
|---|---|---|
| `gg diff`, `gg open`, `gg session navigate` | yes | yes |
| `gg show`, every `gg note` verb, `gg session highlight` | yes | **no, exit 2** |

A change-set's only single commit is its newer end; anchoring a note there
would silently widen a bounded file set into a whole tree. `gg link resolve`
takes both and REPORTS which it got (`ref <name>` / `pair <a>..<b>`, plus
`ref`/`pair_a`/`pair_b` in `--json`) — before that both shapes resolved to
identical output, so the one field distinguishing them was invisible.

#### The copied-link history (`internal/linkhist`, 2026-09-17)

A per-repo 20-entry MRU, records only, keyed on the link TEXT: re-copying
moves a row to the top and replaces its `Desc`. `Desc` is captured AT COPY
TIME (ruling R7) — the context that describes a link, which row the user was
on, is gone by the time anything lists it. The forms are spec §4.3's table;
`internal/cli.linkDesc` and links.js's twin are pinned against each other by
`TestLinkDescJSMatchesGo`, because the CLI and the web write the SAME ring.

Producers: `gg link`, `gg compare` on a link positional (only at exit 0,
ruling R8), and every "copy gg link" row in the browser. There were five such
rows in the web and only three shared a helper — the other two called
`copyText` directly — so `TestEveryCopyGGLinkRowRecords` reads every
`static/*.js` for the label and fails if its row does not record.

The web posts to `/api/linkhist` after the clipboard write, fire-and-forget:
a history that cannot be written must never make the copy look failed, the
same posture `RecordLink` takes. The ring is SERVED, never kept in browser
storage — `gg web` binds a random port every run and storage is per-origin,
so a client-side ring would vanish on the next start.

**Testing gotcha worth keeping.** The obvious concurrency test — two
goroutines recording `2*Max` links, then asserting `len == Max` — CANNOT
FAIL: the cap guarantees the count however many entries an interleaved
read-merge dropped. Record exactly `Max` and check the SET by name. And
`filelock.Acquire`'s `O_EXCL` create is self-serialising for same-process
callers too, so the store's `sync.Mutex` has no independent correctness role;
only a test that holds the lock FILE externally proves the cross-process lock.


**The local form has no delimiter** between the checkout and the file path, so
`ParseLink` does not try to split it: `Repo.Abs` holds the whole absolute
prefix and `Path` stays empty. A PRODUCER, which knows both halves, sets `Abs`
to the checkout and `Path` to the file; both render to the same bytes, so the
round-trip contract for a local link is `String(Parse(s)) == s`, not struct
equality. `domain.ResolveLink` performs the split. The Windows form
`gg:///C:/src/repo/f.go` strips ONE leading `/` when a drive letter follows,
and the line suffix is recognised only when the text after the LAST `:` is a
number — which is how the drive colon survives.

**Resolution** (`domain.ResolveLink`, a package function, not a `Service`
method — it may open a service for another checkout): the cwd's repo when its
identity matches, then `internal/repos` entries in MRU order. Several matches
narrow by containment of the target commit (`svc.ResolveRev`, which doubles as
the short→full sha expansion; there is no second verb), then by a live gg
session. A WORKING-TREE link that still has more than one candidate is refused
(`ErrLinkAmbiguous`, both listed) — two checkouts hold different working files,
so guessing would point at the wrong content; a COMMIT link takes the most
recently opened container, whose content is identical by construction.
Liveness arrives as an injected `ResolveOpts.LiveFn`: `domain` must not import
`internal/steer`, and a parallel test must be able to say "this one is live".

**`internal/repos` stays a DAG leaf.** It stores a `remote` name per entry but
never computes one: `Touch(statePath, repoPath, remote, now)` takes it from the
caller (the TUI's load/config-ready cmds and `gg web`'s `touchMRU`; the
one-shot CLI passes `""` rather than pay two git invocations per command, and
an empty value never erases a stored one). `SetRemote` is the resolver's lazy
backfill for entries an older gg wrote, and deliberately does NOT bump
`LastOpened` — resolving a link is not opening a repository. "This checkout
has NO remote" is memoised as `repos.NoRemote` (`"-"`), so a remoteless entry
costs one probe ever rather than two git invocations on every resolve;
`linkNameEq` refuses the sentinel, and `Touch`'s empty-remote rule never
erases it. The registry walk is skipped entirely when the cwd already answers
a working-tree link (`ResolveLink` short-circuits on exactly that pair).

**A CLI positional is a link iff it starts with `gg://`.** No heuristic: `gg
show <commit>` and `gg diff <rev>` are untouched. A link replaces the flags
that name the same thing — `--file`, `--cached`, `--rev`, `--hunk`,
`--new-line`, `--old-line` for `note add`/`session navigate`; `--file`,
`--cached`, `--rev`, `--start`, `--side` for `session highlight add`; the
`<rev>` positional, `--cached` and `-- <paths>` for `diff`/`show` — and
passing both is exit 2, never a silent override. TWO per-verb asymmetries are
deliberate: **`gg diff <link>` uses only the link's file and target, ignoring
its `:<line>` AND its `#<hunk>`** (the hunk numbering it would need is
`--hunks`' own, so pass `--hunks` to see that hunk), and **`gg show` requires
a link to a COMMIT** — a working-tree or staged link is exit 2, since there is
no commit to show. `gg note list <link>` likewise ignores the line/hunk: it
lists that FILE's threads. The other note verbs take a link too, shaped by
what they can use (`noteLinkShape` in internal/cli/note.go): `reply`/`rm`
take a REPOSITORY link only (no file or `@target` part — the id names the
note, the link picks the checkout whose store holds it, since ids are per
repository), `clear` takes a file link in place of `--file/--cached/--rev` or
a repository link, `apply` a repository or `@staged`/`@<sha>` link in place
of `--cached/--rev` but never one with a file. A bare id the current
repository's store does not hold is `domain.ErrNoteNotFound` (its own type,
text "note not found", unwrapping to notes.ErrNotFound); `noteIDExit` turns it
into "no note <id> in the store of <top>" plus the two ways out. `#` starts a shell comment, so `gg link`'s usage
text and both skills tell the user to quote a link carrying `#<hunk>` — gg
does not guess.

**Producers refuse rather than mis-render.** A path containing `@`, `:`, `#`
or `?` cannot be expressed, so `model.LinkPathOK` gates every producer
(`tui.linkFor`, web's `linkFor`, `gg link`), and `model.LinkAbsOK` gates the
local form's CHECKOUT path in the same three places — laxer by design: a
leading `X:` drive prefix is skipped and a `:` elsewhere is legal (only a
NUMBER after the last `:` is a line), but `@`, `#` and `?` are fatal wherever
they appear. `model.LinkRefOK` is the third of the set and gates a branch or
tag name (`@ref:<name>`, and each half of `<a>..<b>`): it rejects `@ : # ?`,
whitespace, and `..` — the last so a half can never reparse as a different
pair. **`?` joined all three when the hint arrived**: the FIRST `?` in a link
is the hint separator, stripped before anything else, and that is only
unambiguous because nothing else in the grammar may hold one. `gg link` adds a
final `linkRoundTrips` check — it re-parses what it is about to print and
refuses if `ParseLink` cannot read it back — because a hint id is the user's
own free-form argument and has no purpose-built predicate of its own.

**THE JS TWINS IN `internal/web/static/links.js` DRIFTED ONCE AND MUST NOT
AGAIN.** Plan 1b tightened the three Go predicates and left their hand-written
copies behind: `linkPathOK` tested `/[@:#]/`, `linkAbsOK` `/[@#]/`, `linkRefOK`
`/[@:# \t]/` plus a `"..."` check — no `?` anywhere, and three dots where Go
screens TWO. A tracked file or checkout path holding a literal `?` (legal on
Linux/macOS) therefore produced a `gg web` copy-link that JS accepted and Go's
parser then read as a malformed hint: a link the user had just copied from the
browser could not be pasted back into the CLI. All three now match Go exactly
(`/[@:#?]/`, `/[@#?]/`, `/[@:#? \t]/` + `.includes("..")`). The drift stayed
GREEN because `linksjs_test.go`'s table — which computes its expectations from
`model.LinkPathOK`/`LinkAbsOK`/`LinkRefOK` through `model.Link.String()`, so it
cannot disagree with Go by construction — fed no `?` and no two-dot case; it
now feeds eight. Add a case there for any future tightening rather than
trusting a reading of the regexes. (One divergence is deliberate and stays:
`linkFor`'s `rev.length < 40` is stricter than Go's 7..64 hex, so it can only
refuse, never mis-emit an abbreviation.) `gg link`
checks the path argument in one specific ORDER: `@`/a non-hunk `#` on the RAW
argument (so the message is gg's own, not the parser's prose about a target
the user never typed), then the suffix split, then the rebase onto the top
level, then `LinkPathOK` on the REBASED path — which is what lets a Windows
absolute argument (`C:\repo\a.txt`, all drive colon) through.
An UNTRACKED file uses the plain working-tree form
— the grammar has no `untracked` target, and index→file is the pair the
resolver reads for it anyway. A shelf entry has no link at all.

**The TUI key is `L`, help-and-menu-only.** `diffHintFor`
(`internal/tui/diff_render.go`) measures 139 of its 140 allowed columns, so `L`
appears in `help.go`'s Diff view section and the `.` menu's "Copy link" row,
never in the footer — the same treatment `E`, `R` and `a` get. `contextLinkText`
is the single producer behind the key, the menu row and the snapshot's
`cursor.link`, which rides the existing 1 s write-on-change heartbeat.

**Web rows use `act`, never `run`.** `showCtxMenu`'s click handler
(`internal/web/static/layers.js`) calls `menu._items[i].act()` with no guard;
`run` belongs to the command palette's separate dispatcher. The diff-line row
is offered wherever `linkFor` yields a link — NOT gated on `notesArmed()`: a
plain two-commit compare has no notes yet its lines are addressable.

**A compare with no note scope still links its lines (2026-10-01).** The
compare loader stamps `diffView.cmp` (the two endpoints + both paths);
`compareLinkText` reads it when `diffNoteAddress` refuses. Two commits → the
pair `@<a>..<b>` with `:<line>` or `:old:<line>` (`linkAnchorAtCursor`: a
PAIR's old side is commit a and travels, a merge preview's is the merge base
and does not — saved pairs follow the same rule). Any other compare addresses
the cursor side as the version it shows (working tree / `@staged` / that
commit); a shelf or link-member side refuses. Web twin: `ctx.cmpPair` from
`commitDiffCtx` (plain `openCompare` lane only, never an `aSpec` entry
compare), and the stack renders `data-no`/`data-lno` for such a slot. A copy
made in the full-screen diff confirms in `diffNotice` (`clipboardCopiedMsg`),
middle-elided. A compare's FILE-TREE row copies the pair's file form
(`contextLinkText` arm 3a', before `focusedBookmark`, which would answer
`path@<b>`); web: the file menu passes `cmpPair`. `tui.Headless` stubs `clipWrite`: e2e never touches the
machine's clipboard.

**Follow-ups (2026-10-01).** A steered landing sets the PANE too
(`diffView.landOnSide`, from `landSteer` and a parked stack landing; a live
selection keeps its side) — the side is part of the address. Web: a diff
with no note context still links its lines through a link-only context
(`sidesLinkCtx`/`rowLinkCtx` → `ctx.cmpSides`, the entry-diff lane's two side
specs); `state.diffLinkCtx` is tied to `detailGen` so a stale one never names
a later diff's rows, and `diffLinkCtx(row)` is the one door the row menu
reads. `linkFor` lowers `cmpSides` onto the existing shapes (pair, or the
clicked side's own version). A browser probe that reuses a state dir inherits
the STACKED preference — a "single-file" run may really be a stack.

**Web preview links lift `ctx.compare` for ONE case.** `links.js`'s `linkFor`
refuses any compare ctx unless it also carries `ctx.preview = {source,
target}` — an open merge preview is the one compare with an address (git's
three-dot pair). `state.diffCtx` already carries `preview` (files.js's
`openFile`), so the diff-line row needs no new ctx; the compare file-row
menu and the Previews group contributor (`registerRows("preview", …)`) pass
the pair explicitly. A preview has no old side: `linkFor` drops an old-side
line to the file form, and the diff-line handler first swaps a row's LEFT
cell for its `data-rno` (the wide layout only — the narrow layout carries
no `data-lno`/`data-rno`, so there every old-side row is a deletion; user
ruling 2026-09-16). Branch names go through `linkRefOK`, the JS twin of
`model.LinkRefOK`; `linksjs_test.go` pins the rules against
`model.Link.String()` under node, applying the old-side drop on the Go side
by hand (String() would force the side and still render `:N`).

**`{{cwd}}` in an e2e `[[run]] cmd`** expands to that run's working directory
in slash form. It exists because a local-form link is an absolute path the
sandbox only has at run time; it is the harness's ONLY substitution.


**Content links** (`?view=content`, 2026-09-24). `view` is the one hint kind
that names where a link LANDS rather than the surface it was copied from.
`model.ContentHint` is its only value; `ParseLink` refuses it on any `@target`,
an `old:` line and a `#hunk`, and on a remote-form link with no path — the
local form learns its path only in `ResolveLink`, which refuses an
address-less one there. The CLI's one per-verb gate (`linkShapes.Content`)
lets only `gg open` / `gg session navigate` take it. The TUI lands it in
`steerNavigateContent`: a deadlined `WorktreeFilesPresent` stat on the Update
thread, then `openFileViewer` — a full-screen layer (`fileViewer`) that is
the files view's View-file preview lifted onto the stack: same loader
(`loadFileContentSrcCmd`, syntax), same box (`renderPreviewBox`), same keys
via `activePreview()` (the ONE accessor every preview key, the line cursor
and the `.` menu's line rows ask — the viewer on top answers first, else the
focused right-column preview). A new preview feature must go through
`activePreview`, or it will work in one viewer only. Producers:
`contextFileLinkRow` (TUI, spliced after `copy-link`, checks presence off
thread before copying), `linkFor` + `/api/worktree-present` (web),
`gg link --content`. The web page refuses it in `toSteerWire`; `gg open --web`
refuses before touching a page. `compare` ignores the hint (rule 1), so a
content link compares as the working-tree file. **Line focus (v2):** the
line rides `steer.Command.Line`; `openFileViewer(path, line)` parks it on
`fileViewer.pendingLine` because the load is async and the `fileContentMsg`
fill resets the cursor — `landPendingLine` applies it after the fill
(centred, clamped to the last line with a status, dropped on a placeholder
whose lines are not `src`). `fileRowPath` returns `(path, line, ok)`: only
the viewer answers a line (`cur+1`); the list rows go through
`fileListRowPath` and stay line-less. `gg link --content <path>:<line>`
checks the line against `contentLineCount`, which must split exactly like
`fileContentLinesTok` (normalize CR/CRLF, THEN trim trailing newlines). Still
deferred: a web content viewer + landing. **Open-file document (open files plan 1, 2026-09-24):** a viewed
file is an `openFile` (`open_file.go`: source — working tree / commit sha /
shelf id — path, the `contentPopup` viewing state, a tag unique per document
from a process-global counter, `pendingLine`) with ONE `fill`. The files
view's `m.filesPreview` and the full-screen `fileViewer` (which embeds
`*openFile`) are two frames over it. `fileContentMsg` finds its document via
`liveDoc(tag)` — EVERY `fileViewer` on the stack (a covered one too), then
the preview — never "the topmost viewer" (that dropped the first of two
files opened in a row), then (plan 3) the open-files list by tag; a closed
document is not found: its load is stale. `key()` (source + path) is the reuse key for the open-files list.
**Open-files list (plan 2):** `m.openFiles` (`open_files.go`) is
per worktree (`m.currentWorktree`), most recently shown first, cap 100
(20 until 2026-10-01: overviews, never evicted, squeezed the user's files; gg web's `maxOpenFiles` matches);
`touch` evicts the least recently shown doc that `docShown` says is in no
frame (a covered viewer counts as shown). A doc leaves the list ONLY via
`closeDoc` (esc in the viewer, `closePreview`, `x` in the switcher) or
eviction — every other teardown (files view closed, `clearLayers`, a new
preview) leaves it in the background. A doc is in ONE frame: `detachDoc`
before showing it elsewhere. `openFileViewer`/`openPreviewSrc` look the key
up first (reuse; `keepPlace()` so the reload restores cursor+top — a
`pendingLine` wins); commit/shelf docs that are loaded are never re-read.
The switcher is `sessionsPopup` with a parallel `files` slice (quit mode
lists none); `bringToFront` focuses the preview when the doc IS the preview,
else pushes a viewer frame. `focusedDoc` (viewer on top, else the focused
preview) drives Copy file link: any non-working-tree doc gets the
disk-match rule. **Watching (plan 3, `open_files_watch.go`):** one path —
stat → compare → reload. The heartbeat's `openFilesTick(now)` stats the
current worktree's `srcWorktree` docs off-thread (shown: every tick;
background: every `backgroundPollEvery` 5 s via `d.checked`) — plain
`os.Stat`, never git; skipped while `m.loading` (svc already names the new
tree) or a round is in flight (`docWatch.polling`); `docWatch.gen` (bumped
by `reRoot`) + the msg's `wt` drop stale rounds. `loadDoc`/`loadDocWith` is
the ONE load dispatch: it sets `d.loading` (a poll skips the doc — a second
load would reset a `pendingLine` the first lands) and, for a working-tree
doc, stats BEFORE reading and carries that stat (`fileContentMsg.disk`), so
the baseline is never newer than the bytes; a missing file loads as the
`(file deleted on disk)` placeholder. A watch reload sets `msg.reload`:
`fill` takes `keepPlace()` at FILL time (a cursor moved during a slow read
is not snapped back); `keepPlace` is a no-op on a placeholder and `fill`
consumes `keep` only on real lines, so the place survives a deletion. A load
for a doc in no frame routes via `openFiles.findTag` (every worktree — the
`loading` flag must clear) and fills at `viewerGeom()`. fsnotify
(`internal/filewatch`, dir watches + exact-path filter) is only a WAKE-UP —
an event zeroes `d.checked` and polls; built lazily off-thread only when
`watchSupported`, closed with the last watched doc and on `reRoot`.
**Web binary/image previews (2026-09-28):** `handleFileContent` runs
`domain.IsBinary` before `contentRows` → `fileContentBody{Binary, Size}` plus
`Image/Width/Height` from `termimg.Probe` (header only); `handleFileRaw`
(`GET /api/file-raw`, same src/rev/path validation, `readVersion`) serves
IMAGES ONLY (415 otherwise) with `image/<kind>`, nosniff, no-store;
`versionLines` (steer) treats binary as nothing to land on. JS: `fmtBytes` in
core.js; viewer.js `placeholderFor`/`imageOf`/`imageHTML` (exported;
wtfinder.js reuses `imageHTML` via `wtPlaceholder`/`wtImage`, whose pure
test stubs `fmtBytes` since the section runs without imports), `.vimg` in
style.css. The web
DIFF of an image pair still says binary (the TUI's three layouts have no web
counterpart yet). Verified with a Playwright probe (finder + viewer, JPEG +
a NUL blob) — see [[playwright-web-verification]].
**Image pairs in the diff view (2026-09-28, `diff_images.go`):** the differ
decodes a Binary pair's sides (`domain.Diff.OldImg/NewImg`, shrunk to
`diffImagePx`, weighed by `Size`); `applyDiff` → `setImages` builds the info
lines; `hasImages` = binary with ≥1 decoded side. `imageLines(w, h)` lays
them out per `imgLayout` (side-by-side / stacked / single; a one-sided pair
is always single), cached by layout+box+side in `imgKey/imgLines`;
`paintImageRows` reuses `imageRowDecorator` (Ascii → `termimg.Ramp`). Keys
are taken BEFORE the main switch in `updateDiffViewKey`: ctrl+w cycles the
layout (session default `Model.diffImgLayout`, copied by every constructor),
tab flips the side in single. The stacked view (`H`) keeps the per-file
`(binary file)` placeholder.
**Binary + image previews (2026-09-28):** `loadFileContentSrcCmd` (the ONE
loader behind the F preview, the full-screen viewer and background docs)
refuses binary content (`domain.IsBinary`: NUL or invalid UTF-8) — a JPEG
rendered as text cost ~600 ms a frame (an 11 KB "line" through the charWrap
layout) and leaked C1 controls to the terminal. An image (`termimg.Decode`,
stdlib PNG/JPEG/GIF) rides `fileContentMsg.img` (pre-shrunk to
`previewImagePx`); `fill` stores it on the popup and `renderPreviewBox`
calls `p.fitImage(innerW, rowsCap)` each frame (cached by size): the info
line + one `contentLine` per cell row (`cells` set, text = ▀ per cell) painted
by `imageRowDecorator` (fg = top pixel, bg = bottom) — or, under
`termenv.Ascii`, `termimg.Ramp` glyphs. Image rows have `src` false: not
copyable, no line cursor semantics, `landPendingLine` ignores them.
`dropForDisplay` (error_popup.go) is the shared sanitiser predicate: C0, DEL,
C1 (U+0080–U+009F) and bidi overrides/isolates; `displayCls` mirrors it.
**F = the working-tree files window (2026-09-25):** a files-view mode,
`filesModeWorktree`, not a slot: it rides the left-column render, the
right-column `m.filesPreview`, `closeFilesView` and the esc/`handOffToFilesView`
return. Its state is `m.wtFiles` (all paths, the untracked set, the query,
typing); the fuzzy query stays OUT of `filesView.query` (whose substring
`visible()` would drop fuzzy matches) — `wtSetQuery` rebuilds the rows
from `fuzzy.Rank` (cap 200; since 2026-09-28 `internal/fuzzy` = fzf's
`src/algo` matcher in-process + a parser for fzf's `'exact ^prefix suffix$
!not` syntax, smart case, path bonus scheme — every fuzzy surface shares it) through `commitFileLines`, so F shows the commit
view's tree (root files, then one heading per directory; a query's matches
stay grouped) with a status column — `?` untracked, else the unstaged letter,
else the staged one, blank when clean (`statusLetters`, built once at load) —
and the cursor on the best-ranked match (the first file row with no query),
never on a heading; `wtTitle` counts file rows only. **Sticky heading:**
`renderFilesView` hands the built window to `renderFileRows`, whose first
line always names the directory of the row beneath it (`.` at the root; the
heading itself when it is the top row, so it never shows twice) —
`renderWindowTop` (window.go) is `renderWindow` reporting its first row.
The row geometry (`filesGeometry`/`filesGeom.cursorLine`, `filesRowsCap`,
`filesChromeLines`) is SHARED with the reveal tooltip (`filesTreeReveal`),
which used to mirror the math by hand and landed one line off once the
sticky line existed. `updateWorktreeFilesKey` is taken
right after the preview's select/search hooks, so the commit-list side's key
logic never sees this mode. The list = `LsFiles` minus the status's `'D'`
entries plus its `KindUntracked` ones (`worktreeFileList`). The live preview
is an UNREGISTERED `srcWorktree` doc (scrolling must not fill or evict the
open-files list); it loads on `wtPreviewMsg` after a 150 ms settle (gen +
selected-path gate); `watchedDocs`/`watchedDoc` include it. `ctrl+t` sets
`m.filesFull` → `layout()` gives the files view the whole body.
**Preview + reply (follow-up):**
the files view's focused preview answers `fileRowPath` with its cursor line
(`focusedFilesPreview`); because it shows ANOTHER version, the row snapshots
the shown lines and the off-thread check compares them to the disk
(`sameContentLines`) — a mismatch copies nothing (`fileLinkCheckedMsg.changed`).
The navigate reply rides the load (`contentLandedMsg` wraps the
`fileContentMsg`), so it can say `at line N` / the clamp; a failed load
fails the navigate.
**Web viewer (open files 5a, 2026-09-26):** `GET /api/file-content`
(`filecontent.go`: src worktree|commit|shelf — shelf bytes via `ShelfBlob` —
lines split on `\n` with a trailing `\r` trimmed, `tokTriples` runs under the
TUI's lex rule, `too_large` over `MaxDiffBytes`, `missing` for a working-tree
file gone from disk) feeds `static/viewer.js`, a `mountOverlay("viewer")`
layer whose markup is built at IMPORT (bindSearchBar needs its bar in the DOM).
The overlay stops above `#foot` and swaps the footer's chips while open (hints
live in the bottom bar). *view file* rows come in through a dedicated
`"fileview"` menu key that files.js splices beside history/blame (a plain
`registerRows("file")` appends after the discard rows; files.js must not
import viewer.js). Content links land via live.js's `steerNavigateContent`
(a `view` hint is a LANDING kind, never revealed — it returns before the
land-then-reveal pair); `#` takes a `gg://` link through
`GET /api/link-command` (linknav → `toSteerWire`, or `{checkout}` for another
checkout). The `toSteerWire` and `gg open --web` content refusals are gone.
**Web View all notes (2026-09-29):** `GET /api/notes/overview` (`allnotes.go`)
flattens one `domain.NotesOverview` (notes via `ToWireNote`, every group an
array, 409 when notes are disabled) for `static/allnotes.js`, a
`mountOverlay("allnotes")` layer at z-index 21 (under `#modal`, which the
`ctrl+d` confirm raises over it). `anBuildRows`/`anVisible`/`anAgo` are the
TUI's `buildAllNotesRows`/`visible`/`coarseAgo` ported one to one (node test
`allnotesjs_test.go`). Opening a row HIDES the popup (state kept) and
registers `setDiffBack` (files.js): the diff's esc (`drillOut`) calls the
popup back only while `noteCollapseKey(state.diffCtx)` still names the diff
it opened — stepping to another file drops the return. `landNote(id)` polls
~4 s for the `tr.note[data-note]` row (notes paint after the diff; a stack
loads lazily) and lands like `}`/`{` (`landNoteRow`). A review row opens
`openReview(id, {kind: "popup", run})` (reviews.js): `goBack` puts the commit
list under it and runs `run`, which reopens and re-reads the popup.
**Web open files (5b, 2026-09-26):** the list lives on the `Server`
(`ofs`, `openfiles.go`, pure core + mutex), keyed by `svc.Root()` — NOT on the
live hub, which `startLive` replaces on every re-root and settings write. A
tab is a page LOAD (`core.js` `tabId`, never stored) sent as
`/api/events?tab=` and in every POST; `handleEvents` counts the tab's streams
BEFORE the hello and drops what it showed when the last one ends, so the page
re-reports `shown` on EVERY hello (a hub swap reads background → shown after
Chrome's ~3 s EventSource retry). Ops: open (reuse by src+rev+path; tab ""
= background open) / focus / background / close (esc: removed unless another
tab shows it; `everywhere` = the switcher's x) / cursor (never broadcast) /
shown. `open`/`focus` stat a working-tree entry BEFORE the page's content GET
(stat-before-read); the path must be `filepath.IsLocal` (the poller stats
it). `openfiles_watch.go` polls (shown 1 s, background 5 s, filewatch only
wakes, never on 9p) and SKIPS the whole round while an op runs — advancing the
baseline there would lose the op's edit. `open_files`/`file_changed` go out
via `fanOut` (the gate never drops them). Page: `viewer.js` `closeViewer(how)`
— esc = close, ctrl+] and the `.` menu's hand-offs = background; a load-seq
token lets a link landing beat a reload; `layers.js` `pushFoot`/`popFoot` is a
chip stack so the switcher (`openfiles.js`) nests over the viewer.
**Web agent verbs (5c, 2026-09-26):** `POST /api/session/steer` answers
`files`, a background navigate and `file_focus` ITSELF — 200 + a
`steer.Reply` (`steer_files.go`); every other verb keeps its 202. `files` and
a background open are answered BEFORE the op-in-flight 409 (neither rides the
hub's steer lane: a background open only `fanOut`s `open_files`, now carrying
`opened` for the page's toast); `file_focus` keeps the 409 and emits a steer
whose wire carries `file_id`, NEVER a path (a path may name two versions —
`ofs.resolve` tries id, then an id-as-path, then path, like the TUI's
`findOpenFile`). Reply words are the TUI's (`steerLanded` = `landedDetail`),
the line count from the server's own read (`readVersion`, shared with
`/api/file-content`); a focus with no live tab stream says `; no gg web tab
is open to show it`. `toSteerWire`: a background open needs a content link;
`file_id` must be `f<n>`. CLI: `steerLive(both)` replaces `steerTUI` — the
TUI when live (its reply decides), else the web (`steer.PostHTTPReply`); a
focus/background open with both live goes to both, the web's answer printed
`web: …`. e2e: `[input] web = true` (`e2e/web.go`) serves the sandbox with an
in-process `web.Serve` and waits for its presence.
**Agent verbs (plan 4, 2026-09-25):** steer additions `Command.Background`,
`Command.FileID` (NOT `ID` — that is the command's id), `Reply.Files` and the
wire row `steer.OpenFile` (id `f<seq>` = `openFile.seq`, the tag's suffix;
source/rev/line/state), which is also the snapshot's `open_files`
(`openFilesProto`). `applySteer` now validates (`steerEnumRefusal`) FIRST,
then routes `files` and a background navigate PAST `steerRefusal` — they
never move the screen — while `file_focus` keeps every refusal. A background
navigate (`steer_files.go`) never parks a `pendingSteer`, refuses another
worktree outright (the switch notice would be a screen change), leaves a
file already on screen untouched (no line applied), else find-or-new +
`registerDocEv` + `loadDoc`, the reply riding the load. `contentLandedMsg`
carries `lead` ("opened X", "opened X in the background", "focused X") and
`evicted`; `landedDetail` builds every such reply, and every content-link
reply names a file closed over the cap (`registerDocEv` returns it — never
scrape `statusMsg`, it is i18n). `file_focus` = `findOpenFile` (id, else the
id tried as a path, else the path — most recently shown version) +
`bringToFront`; a loaded commit/shelf doc lands its line synchronously. CLI:
`steerTUI` posts to the TUI inbox ONLY (a web page gets nothing; web-only →
exit 1), `--background` never launches a TUI; `gg web`'s `toSteerWire`
refuses `Background`, `files` and `file_focus` by name until stage 5.
**Web F finder (5d, 2026-09-27):** `GET /api/worktree-files` (`worktreefiles.go`) serves
F's list — `domain.WorktreeFileList` (moved from the TUI, which calls it too: ls-files minus
status `D` plus the status's untracked), read once per F open (`fresh=1`) and cached per
Server with a build number `gen`; a query ranks with `fuzzy.Rank` (cap 200, `limited`), no
query pages the sorted list 200 at a time (`offset`/`next`; a page whose `gen` moved restarts
the list). `/api/files` and its per-HEAD cache are gone. Page: `static/wtfinder.js` is a LAYER
(so the viewer, history, blame, the `.` menu and the switcher open over it and esc returns)
drawn IN the panes — `#panes.wtf` shows `#wtf` in the files pane and `#wtf-preview` in the
diff pane over any layout (three classes outrank the layout rules; the panes' own children
hide at (3,1,0)+) and touches nothing underneath, so esc restores the stage (the three panes' scroll is saved on open and put back on close — hiding a pane's children resets it). The preview is
NOT an open file (it reads `/api/file-content` directly, 150 ms settle, gen + path drop stale
answers); `ctrl+]` = `/api/open-files` `open` with `tab ""`. diff closes F and opens
`openWorktreeFileDiff` (viewer.js — unstaged, else staged: `/api/diff` has no HEAD ↔ working
tree lane). A steered navigate onto the panes closes F (`live.js`); a content navigate opens
the viewer over it.

**F follows the disk (5d minors, 2026-09-27):** the preview is still no open file (D9), so the
server poller never sees it — the PAGE re-stats it instead: while F is up a 1 s timer
(`WT_STAMP_MS`) asks `GET /api/file-stamp?path=` (one `statDisk`, no read; `"<size>:<mtime ns>"`,
`"missing"`, `""` = stat failed, never a change) and re-reads through `showPreview(…, keep)`
only when the stamp moved (`wtStampChanged`), keeping the scroll. `/api/file-content?src=worktree`
returns the stamp taken BEFORE its read, so an edit mid-read still reloads. Skipped while a
surface covers F (the top layer is not `wtf`), the tab is hidden, or a check is out; a failed
reload keeps the new stamp (no retry per tick). Widths: one rAF-coalesced `ResizeObserver` on
`#wtf-list` + `#wtf-ptitle` re-renders the rows (list scroll kept) and re-cuts the title from
`previewPath` (`paintPTitle`). NB the list is `--files-w` px wide — only the `rs-detail` drag
changes it, a window resize only the title.

### The compare algebra (`internal/domain`, plan 1b, 2026-09-17)

**`FileSet` is the unit, not `Endpoint`** (`internal/domain/fileset.go`). A set
is either UNBOUNDED — a point, one endpoint standing for a whole tree — or
BOUNDED, a key set of paths plus the endpoint its bytes come from. Only a set
can say "these files"; an endpoint can only say "this text", which is why
`resolveCompareSpec` in the CLI returns a `FileSet` and every non-link arm
funnels through `EvalEndpoint` to get one.

- `EvalEndpoint(ctx, ep)` — an endpoint's set. A shelf endpoint is BOUNDED
  (a commit entry's tar members, or a FILE entry's single `Origin.Path`); a
  pair endpoint is BOUNDED (the paths its two halves differ in);
  worktree/index/commit are unbounded. **It is also the one place a
  `EndpointRef` is allowed to die** (ruling R2): a ref MOVES, so
  `Endpoint.CacheTag` PANICS on one by design — a moving name must never become
  a cache key — and every caller therefore resolves through `EvalEndpoint`
  before comparing or caching. `EndpointForLink` may hand back an unresolved
  ref, and its doc says so: display-only callers may use it directly.
- `EvalLink(ctx, l)` — a link's set: `EndpointForLink` for the target, then
  narrowing to the link's `/<path>` when it has one. A file link is therefore
  bounded to one key even when its target is a whole tree. `narrowTo` also
  marks the result `narrowed`: the endpoint is carried over untouched, so
  "this set is a projection" is not derivable from it afterwards, and a
  consumer that re-derives sets FROM endpoints (`ComparePatch`'s shelf lane)
  would otherwise widen it back silently.
- `CompareSets(ctx, left, right)` — the algebra, and the whole of it:
  unbounded × unbounded is git's own diff; bounded × bounded is symmetric over
  the union of members; a mixed pair PROJECTS the tree onto the bounded side's
  paths. **The asymmetry is load-bearing: you only ever scale DOWN** — growing
  the bounded side to the tree's size would compare one file against every file
  in a 100 GB monorepo. Every row yields a bounded result, which is what makes
  compare CLOSED: a result is the same kind as a merge preview, so it can be an
  endpoint of the next comparison.

**`validComparePair` is deleted** (it lived in `internal/cli/compare.go`). It
was a frontend predicate that screened `gg compare`'s two endpoints and refused
two shapes: a "reversed" live pair (`gg compare @worktree main` → "order
endpoints oldest→newest…") and a frozen shelf side against `@staged`/`@worktree`
("a frozen past can't diff against a moving target"). Both were git's own argv
limitations showing through one frontend, not statements about what a user may
ask — and because it guarded only the `gg compare` door, MCP's
`gg_compare_trees` answered the same reversed pair with a raw
`DiffTreeFiles: unsupported endpoint pair 1 → 3`. `CompareSets` is total over
the 2×2, so both refusals are gone from every frontend: a reversed live pair is
asked FORWARD and the answer inverted (`invertCompareRows`), and a frozen shelf
entry against the index or working tree is an ordinary bounded × unbounded
projection. `TestNoInvalidComparePairRemains` keeps the deleted predicate's two
fixtures and asserts the opposite, so the removal cannot be quietly undone.

**Two consequences that bite.** (1) The forward unbounded × unbounded lane now
passes through `sortedCompareRows`: `CompareFiles` hands back git's order with
untracked files appended, so without it one comparison had two orders depending
on the direction it was asked. It COPIES rather than sorting in place — `query`
hands concurrent coalesced callers the leader's slice header, so sorting in
place would reorder somebody else's result. (2) `ComparePatch` is NOT total the
way `CompareSets` is: it still takes two ENDPOINTS, `model.DiffSpec` has no
`-R`, and `livePairSpec` now ends in an explicit refusal
(`domain.ErrComparePatchPair`) where it used to fall through to an empty
`DiffSpec` — which git reads as "index → working tree", i.e. a diff of
something else entirely, with no error. `gg compare --patch` maps that sentinel
to its own message (exit 2) naming the two ways out.

`--patch` refuses a SECOND shape for the same reason, and this one was a silent
wrong answer rather than a raw message: it takes endpoints, so a key set it
cannot re-derive from one is simply dropped — `gg compare --patch
gg://repo/b.txt@<sha> HEAD` printed the two commits' whole-tree diff while the
default listing showed the projection, two different comparisons with nothing
on screen to say so. The condition is asked **PER SIDE**
(`FileSet.patchLosesKeySet`, behind the door `domain.ComparePatchSets` — it lived in `internal/cli` until 2026-09-20), and that is the whole of it: a side survives exactly
when `EvalEndpoint(fs.Endpoint())` would reproduce its set.

```go
f.narrowed || (f.bounded && f.ep.Kind() != model.EndpointShelf)
```

**A side that loses is RENDERED, not refused (2026-09-20).** `ComparePatchSets`
sends two surviving sides to `ComparePatch` (one git invocation) and everything
else — a lost set, or a pair `ComparePatch` answers with `ErrComparePatchPair`
(a reversed live pair) — to `patchPerMember`: `CompareSets`' own rows, bytes
from `fs.Source(path)` (never `Endpoint()`: a `-u` stash's third parent), the
left side of a rename row at `OldPath`, `diff --no-index` per row. The shelf
lane is the same function. `model.DiffSpec` has no `Reverse` on purpose: per
member, a reversed pair needs no `-R`. Spec
`docs/superpowers/specs/2026-09-20-compare-patch-projected-design.md`.

Unbounded has nothing to lose. A non-narrowed SHELF set survives because
`ComparePatch`'s shelf lane re-derives both sets with `EvalEndpoint` and renders
per member (so `gg compare --patch shelf:<gc'd id> <commit>` answers, and must
keep answering). A NARROWED set never survives, shelf or not, because `narrowTo`
carries the endpoint over untouched — which is exactly what `FileSet.narrowed`
exists to report, since neither bounded-ness nor kind can see it. Everything
else bounded loses, above all a PAIR, whose endpoint is commit *b* and not the
pair (`EvalEndpoint`'s Pair arm), so re-deriving hands back b's whole tree.

**Asking this per PAIR is a bug, and it shipped once.** A first version
short-circuited on "a shelf is on either side ⇒ `ComparePatch` re-derives both
sets", which is true of the SHELF side only: `gg compare --patch <pair-link>
shelf:<id>` then dropped the only file the change-set named and rendered a file
it never named, reporting `M` where the listing said `A`. The per-side form
closes it and reproduces every row the pair form got right. A `TODO(plan 3)` in both files records what closing the
remaining gap needs (a set-taking `ComparePatch` sibling; a patch of a
projection is a new rendering question, not a refactor).

**Every `switch` over `EndpointKind` outside `internal/model` is a seam.** The
two new kinds (`EndpointRef`, `EndpointPair`) made three of them wrong by
omission: `internal/web`'s `commitEntrySide`/`compareSideWire` had a
"shelf, else `commit:` + `ep.Hash()`" default that wired a ref or pair to the
browser as a commit with an EMPTY hash — no error anywhere, the client just
described nothing. Both are per-kind with no default arm now and report an
inexpressible kind as a 500; they use `%d` rather than `ep.Display()` because
`Display` has no `EndpointInvalid` arm and PANICS on the zero endpoint, and
`internal/web` has no `recover()` middleware.

### Symmetric merge previews (2026-09-22)

NOT a store kind: an ordinary TWO-link `savedcompare` entry,
`Left: gg://<repo>@<base>...<A>`, `Right: gg://<repo>@<base>...<B>` (target
first, A left — `previewLinkText` is the one construction, shared with
`entryFromPreview`). `domain.SymmetricOf` is the ONLY recogniser: both halves
whole-tree preview links (no path/line/hunk/hint), same repo, same target,
different sources. A hand-saved comparison of two such previews is therefore
symmetric too (ruling: same data). Frontends never parse links for it: the
TUI stores `previewRow.sym`, the web gets `symmetric: {a, b, base}` on each
comparison row. Creating it: `SymmetricPreviewAdd` (a duplicate returns the
existing entry + `ErrSavedCompareExists`, opened as success everywhere);
`POST /api/saved-compares/symmetric` (409 on a duplicate, the web's
convention); `gg preview add --symmetric --base` (the id on STDOUT, a
duplicate exits 0). The TUI base popup is stacked on the pair menu via
`pairOp.stacked` (the picker normally pops itself before `open`). The base
never has a default in any frontend.

### Saved commit pairs (2026-09-19)

A saved commit pair is a SET-shaped `savedcompare.Entry` —
`Left: gg://<repo>@<A40>..<B40>`, `Right: nil` — the shape a merge preview has,
differing only in `Left.Target` (`Pair` vs `Preview`). No store, file or state
kind of its own. `domain/pair.go` is `domain/preview.go`'s twin:

- `PairAdd` FREEZES both revs to full shas (ruling: a pair never follows a
  branch) and dedups through the store's (Left, Right) rule.
- `pairFromEntry` and `previewFromEntry` are two recognisers over one store and
  must DISAGREE on a shared fixture (`TestPairAndPreviewRecognisersDisagree`):
  each surface sees exactly its own rows, and a Left+Right comparison is seen
  by neither. `pairFromEntry` also rejects pair sets it did not produce (names,
  short shas, a path, a hint).
- `PairRename`/`PairRemove` are guarded by `PairGet`, mirroring the preview
  pair: neither surface can relabel or delete the other's rows.
- `PairOpen` counts files TWO-dot via `CompareFiles` and caches the count by
  `(a, b)` in the `"preview"` cache — a frozen pair is immutable and the
  Previews tab re-reads on every branches arrival. Only `PairOK` is cached.
- TUI: `previewRow` is a two-kind row whose ZERO kind is the merge preview (so
  pre-pair fixtures stay valid). `rec` is read only through `merge()`, and only
  in `preview_panel.go` — `TestPreviewRowRecIsReadOnlyInItsOwnFile` (AST). A
  pair opens as a plain commit comparison: `previewOpen` and `filesPreviewSet`
  stay unset. The save rows take their direction from
  `compareSelectionEndpoints`, by construction.
- Deferred, with the rule already decided: a `?preview=<id>` hint must be a
  LOOKUP (by id, else by matching `Left`, else show-once), never a checksum —
  the id hashes link TEXT and converted previews keep legacy ids. Pair notes =
  ordinary notes on `B`, new side, via `PreviewNoteSet{Tip: B, Base: A}`.
  Spec: `docs/superpowers/specs/2026-09-19-saved-commit-pairs-design.md`.

### Pair notes (2026-09-20)

A commit pair is a NOTE SCOPE built by `domain.PairNotes(a, b)` —
`PreviewNoteSet{Tip: b, Base: a, Commits: a..b}` with EMPTY `Source`/`Target`
(`set.IsPair()`). Nothing forks: `loadPreviewNotes`, `PreviewNotesFor/At/All`,
`PreviewNoteCounts`, `PreviewHunkAnchor` read only Tip/Base/Commits. Spec
`docs/superpowers/specs/2026-09-20-pair-notes-design.md`.

- **`b` is always a member of `Commits`.** `a..b` is empty when `b` is an
  ancestor of `a` (a reversed save); without the prepend the write target's own
  notes are never gathered (`TestPairNotesReversedPairStillHoldsItsTip`).
- **`PairNotes` reads no saved entry** — an unsaved pair link gets the same
  scope, which is what makes the agent hand-off work before the `?preview=`
  hint exists.
- **`domain.NoteScopeResolve`** is THE parser of `--preview` and MCP's
  `preview` arg (`...` → preview, `..` → pair, else id/label with the preview
  winning a shared label). `cli.resolvePreviewTarget` and `mcp.previewSet` are
  thin calls onto it; every refusal message lives there.
- **`domain.Resolved.Preview` stays nil for a pair link.** `linknav`,
  `tui/steer.go` and `web/steer.go` dispatch on it and refuse a preview with
  empty names; `gg open <pair link>` keeps riding steer `State:"pair"`. The CLI
  builds the scope in `cli/notescope.go` (`noteScopeFromLink`) instead.
  `linkDiffSpec` keeps its own pair arm (no rev-list for a plain `gg diff`);
  `TestPairLinkDiffSpecEqualsItsNoteScope` pins the two specs equal.
- Readers of `set.Source/Target` must ask `IsPair()` first: `cli.scopeName`
  (prose), `gg link --preview` (emits `--pair`), `tui.scopeLinkFor`.
  `forgeNotesFor` is naturally empty (`ParsePRRef("")`).
- **TUI arming is a message, not an opener** (`tui/saved_pair_notes.go`):
  `pairNotesCmd(a, b)` → `pairNotesMsg`, armed iff the generation matches AND
  the view on screen compares commit a with commit b (`showsCommitPair` reads
  the ENDPOINTS, never `compareTag` — a link-shaped compare tags differently)
  AND no merge preview is open. Dispatched from `handlePairOpenMsg`, from
  `steerNavigatePair`'s endpoint-shaped fallback, and — since plan 3b-2 lands a
  change-set navigate through the link-shaped compare — from `openLinkCompare`
  (`steeredPairNotesCmd`: only a `State:"pair"` navigate arms, so a generic
  link compare of two far-apart points never pays the rev-list). Always built
  AFTER the view opens (`beginFilesView`/`openCompareFiles` bump `previewGen`).
- **The late stamp.** A steered landing opens the file's diff the moment the
  list lands, which can beat the scope. The handler stamps the live diff layer
  (matched by the `cmp:<left>:<right>:` prefix of `m.diffTag`; `v.compare` is
  false for an added file, so it is NOT the test) — and the `diffMsg` arm keeps
  a stamp that landed on the live view while the load was in flight, because
  the loader's `inheritIdentity` snapshot predates it.
- Badge refresh: the `srcNotes` arm re-dispatches `pairNotesRefreshCmd` (a pair
  has no `previewOpen`, nothing to re-resolve, nothing that can "move").

### Stacked diff view in `gg web` (plan 1 — web core, 2026-09-22)

Spec `docs/superpowers/specs/2026-09-22-stacked-diff-view-design.md`, plan
`docs/superpowers/plans/2026-09-22-stacked-diff-web-core.md`. `S` flips
`/api/uistate` `stacked_diff`; while on, `openFile(i)` routes to
`stackview.js` `openStack(i)` instead of a single diff.

- **Two modules.** `stack.js` is pure and import-free (slots, `stackRows`,
  `nextToLoad`, `reconcileSlots`, `countsFromDiff`; node-tested by
  `stackjs_test.go`). `stackview.js` owns the DOM: `state.stack = {list,
  group, slots, near, anchor, inFlight}`.
- **Same-stack test is array identity.** `openStack` scrolls when
  `state.stack.list === activeFileList()` (and the working-tree group
  matches); a new commit, a compare filter change or a status re-read hands
  out a new array → a rebuild (the status path goes through
  `reconcileStack`, which keeps slot OBJECTS so in-flight loads land).
- **One URL builder.** `fileDiffURL(f)` (files.js) is what both the
  single-file opens and the stack loader fetch — never build a diff URL
  inline again (`TestFileDiffURLIsShared`).
- **Counts before first paint.** `buildStack` awaits `/api/numstat` before
  `paintStack`: placeholder heights come from the counts. Painted without
  them every section is ~3 rows, the whole set is "near", and the loader
  fetches the first three files wherever the reader is. Entry / link sets
  have no numstat source and count from each loaded diff.
- **Teardown points.** `setLayout(mode !== "diff")`, a single-file
  `openFile`, the conflict row's **open resolver** (→ `openStatusDiff`), and
  an emptied working-tree group (`enterFilesStage`). While a stack is up
  every single-diff global (`lastDiff`, `diffCtx`, `diffRow`, `notes`) is
  null, which is how search, history/blame, the fold listener and the live
  re-renders all stand aside. `rerenderDiffKeepingPlace` / `toggleDiffView`
  hand off to `rerenderStack`.
- **A single diff still loading must not paint over a stack.** Click a file
  then press `S` before its diff lands: the late answer used to overwrite
  `#diff-body`. `openFile` / `openStatusDiff` now drop a superseded answer
  (`detailGen`, which `buildStack` bumps) and `renderDiff` refuses while
  `state.stack` is set. Only the working-tree probe caught it.
- **Scroll pinning is manual.** Placeholders are estimates; `repaintSlot`
  puts the reader's header back when a section above it grows, and
  `#diff-pane:has(.stk)` sets `overflow-anchor: none` (browser anchoring
  lost its node whenever the reader's section repainted in the same beat).
- **Web keys.** `S`, `-`, `_`; `/` and `@` toast (search reads one diff); `j`/`k`
  scroll the stack via `openFile`. NOT `n`/`p` — `p` is pull.
- **Deferred.** Notes/cursor (the per-slot accessor arrives with them),
  search, hunk staging, the TUI.

### Stacked diff in the symmetric compare (plan 2, 2026-09-22)

Plan `docs/superpowers/plans/2026-09-22-stacked-diff-web-symmetric.md`.
`stackOn()` is just the pref: in the symmetric view `activeFileList()` IS
`symRows(c)` (visible, direction-aware rows), so the stack needed only:

- **One pair builder.** `stack.js` `symPair(f, c)` (flip-aware; `=` asked as
  `M`; null = neither side has content) is read by `openSymRow` AND
  `fileDiffURL`'s sym arm (`TestSymPairShared`). `GLYPH` / `KIND_TIP` /
  `noContentWhy` moved to `stack.js` too.
- **A sym row is `Object.hasOwn(GLYPH, f.kind)`**, never "has a kind": a
  working-tree status entry carries `kind: "tracked"`. The first cut used
  `!!f.kind` and every working-tree slot became header-only (only the plan-1
  working-tree probe caught it).
- **Opens before the first paint.** `buildStack` awaits the counts; `applySym`
  / `setFilter` open row 0 and then the kept row in the same tick. The
  second open lands on `st.painted === false` and only moves `st.want`,
  where the first paint then scrolls.
- **Rebuild paths.** Filter / flip / `v` / the width gate hand out a new
  `state.files` → a rebuild on the kept path; `updateLinkCompareFiles`
  rebuilds on the cursor's (clamped) file instead of `drillOut` (the sym
  view's esc leaves the screen); an empty view tears the stack down in
  `applyCompareFilter`'s `symEmpty` arm.
- **The one-pane tail** (`--stk-tail`, `.stk::after`): sets with no numstat
  (links, entries) paint 3-row placeholders, the pane ran out of scroll
  before a late header reached the top, and `syncCursor` then moved the
  highlight to the file above. Probe `stack-probe/sym.mjs` (scratchpad).
- **Pan bars over mixed tables.** `mountPanBars` sizes each bar by the
  largest OVERFLOW (line width − its own cell's width) over every table on
  the host, and shows a bar per side when ANY table is two-column. It used to
  read the first table only: a one-column file on top gave one bar measured
  against a full-width cell → no scrollbar while two-column files below were
  cut (user-found in the symmetric stack; probe `stack-probe/mixed.mjs`).
- **Painted thumbs.** Each `.hbar` stays the native scroller (wheel, touch,
  `scrollLeft` — search and restore drive it) but its scrollbar is hidden;
  `mountThumb` paints `.hthumb` over it (drag pans, a track click pages).
  Firefox/macOS overlay scrollbars only show on hover — the user saw NO bar
  in Firefox. Headless Chromium hides scrollbars too; Playwright's Firefox
  (`npx playwright install firefox`) is how to see what Firefox users see.
- **Per-file bars in a stack** (user ruling). Each section is
  `head · .stk-body · .hbars.stk-hbars`; `mountSlotBars` mounts with the
  BODY as pan host, so `--pan-l/--pan-r` and `host._pan` are per file. The
  section's bars are `position: sticky; bottom: 0` inside the section: at the
  pane bottom while the file fills it, at the file's end after. `mountSlotBars`
  runs BEFORE `repaintSlot`'s pin (the bars add height). `#diff-hbars` hides
  while a stack is up; `renderDiff` brings it back. Probe `perfile.mjs`.

### Stacked diff view in the TUI (plan 3, 2026-09-22)

Spec `docs/superpowers/specs/2026-09-22-stacked-diff-view-design.md` §5.2 /
§6.2 / §8 / §9, plan `docs/superpowers/plans/2026-09-22-stacked-diff-tui.md`.
`S` in the full-screen diff view shows EVERY file of the list the diff was
opened from, one header line per file with its rows below. The preference is
`promptstate` `tui_stacked_diff`, machine-global and independent of the web's
(design R7), so with it on `enter` on any file opens that list's stack
scrolled to the file.

- **One stream, not a second surface.** `diffView.stk` (`diff_stack.go`) is
  the whole feature: `v.lines` became `[]diffLine` — `textdiff.Line` plus a
  file index and a kind (body / header / placeholder) — so every `.Row` /
  `.Fold` reader, the display-row layout, wrap and scroll modes, the search
  and the cursor work unchanged. The pure `textdiff` leaf learns nothing.
- **Each file is framed.** `spliceStack` puts a blank `lineGap` above every
  header (not the first file's — nothing is above it) and a full-width
  `lineRule` under it, so files read as blocks rather than one column. Neither
  is a cursor stop, and `stackFile` therefore carries BOTH `start` (its first
  line) and `hdr` (its header); anchors measure from `start`, so the frame
  lines shift nothing across a re-splice.
- **The jump blocks ARE the headers.** `spliceStack` sets `v.blocks` to the
  header line indices, so `n`/`p`, the wrap arming, `focusBlock`,
  `deriveOrdinal` and `f`/`ctrl+w`'s re-anchor all step file to file with no
  new navigation code. Change-block stepping inside one file therefore moved
  to `ctrl+↑`/`ctrl+↓` while stacked (plan 4a, `changeInFile`).
- **Per-file data lives on the file.** `stackFile.d` is the per-file view one
  of the ORDINARY single-file loaders built (`treeFileLoad` — factored out of
  `openDiffForFileLine` — or `loadStatusDiffCmd`). Syntax runs are indexed by
  SOURCE LINE NUMBER, so `toksFor(li)` must resolve through the line's own
  file or file B's line 5 paints with file A's tokens; `gutter()` is the
  widest loaded file, so the numbers line up.
- **Loading is a function of where the reader is.** `pumpStack` runs after
  every key, arrival and resize; `wantLoads` (pure, table-tested) picks the
  unfolded, non-conflict, idle-or-stale files whose header is within two
  screens, nearest first, at most `stackMaxInflight` (3). `stackCmd`
  re-labels the loader's `diffMsg` as `stackFileMsg` — the `diffMsg` handler
  does `*dv = *msg.view` and would throw the stack away.
- **An arrival must not move the screen.** `applyStackFile` re-finds the
  cursor and the viewport by `stackAnchor{file, inFile, sub}` around the
  re-splice, because a file loading ABOVE the viewport shifts every index
  below it. `stackFileMsg` is generation-gated (`Model.stackSeq`).
- **The cursor rests on headers** (not on placeholders): a folded file is
  header-only, and `-` has to be able to unfold the cursor's file.
  `cursorRow()` returns false there, which makes notes, copy-line, `e` and
  the header's line number inert on a header through the existing `hadRow`
  convention. `v.title` is kept synced to the cursor's file, so `h`, `b`,
  `e`, the copy rows, export and bookmarks need no stack awareness.
- **Notes are per file** (plan 4a, below): a stack's own `noteAddr` stays
  zero, and every note surface reads the CURSOR's file instead.
- **Working tree.** A stack is ONE section (Files or Staged);
  `reconcileStatusStack` hangs off `withStatus` (the one status write point):
  gone files drop, new ones are inserted in list order, a file already read
  is marked `stackStale` and KEEPS its rows on screen until the queue
  re-reads it (no flicker on a background refresh), the cursor keeps its file
  by PATH, and an emptied section closes the view. Conflicted rows are
  header-only with the resolver behind `enter`.
- **Counts.** One `stackStatCmd` per stack (`CommitStat`, `DiffStat{Rev}` for
  a commit↔commit compare, `DiffStat{Cached}` for a section); sources with no
  git pair (shelf, entry, preview, a live endpoint) count from the rows as
  each file loads. Numstat wins; rows fill in what it did not name.
- **Not stacked:** full-tree mode (not a change set) and a picker compare
  with no list — `S` there says "nothing to stack here".
- **Footer trade:** `[e] edit` left the diff footer for `[S] stack` (it keeps
  its `.` menu and help rows) and "notes" lost its plural; the stacked view
  has its OWN footer line. `diffHintFor(long, stacked)`.

### Review links (2026-10-05)

Spec/plan: `docs/superpowers/specs/2026-10-05-review-links-design.md`,
`docs/superpowers/plans/2026-10-05-review-links.md` (stage 1 of 3; stage 2 =
answers per remark, stage 3 = a second review answering the first).

- Hint kind `review` (`model.ReviewHintKind`); the address is the reviewed
  change (`domain.reviewTarget`: one commit, or `<base>..<tip>` full shas).
  An address-less `?review=` is refused in `finishLink` (like preview/version).
- `ResolveLink` runs `checkReviewHint` after `finishLink`: refuses ONLY a
  proven mismatch (`ErrReviewLinkMismatch`, wrapped in `ErrLink`); a deleted
  review or an unreadable store passes. It checks with `opts.Cwd` when the
  link is in the caller's checkout — a fresh `Open` would not see an injected
  note store (tests) and, generally, is a second service for nothing.
- `domain/review_link.go`: `ReviewID` (`latest`), `ReviewLink`,
  `ScopeLinkText` (Range review row), `CommitFileLinkText` (Notes row),
  `ReviewRemarks` (N = the read-time note-id index), `ReviewShow` (the JSON
  `gg review show --json` and MCP `gg_review_show` share).
- Landing: one path. `steerNavigate` sends a review hint to
  `steerNavigateReview` (TUI, `review_hint.go`: off-thread lookup →
  `openReviewFrom`, back = the review's commit; deleted → plain landing +
  notice). gg web: `live.js` `steerNavigateReview` (pinned by
  `steerhintjs_test.go`).
- Copy: `asyncCopyLinkRow` builds the link when the row RUNS (git off the
  Update thread) and copies through `copyToClipboardCmd` (records in gg
  links). Web asks the server: `GET /api/review/{id}/link`,
  `GET /api/notes/row-link`.
- CLI: `gg review show` is dispatched before `gg review`'s flag parse;
  links go through `resolveLinkArg` (the cli one-door guard).

### Review answers — remark threads + resolved (2026-10-05, review links stage 2)

Spec/plan: `docs/superpowers/specs/2026-10-05-review-answers-design.md`,
`docs/superpowers/plans/2026-10-05-review-answers.md`. User rulings: an
answer is a REPLY in the remark's thread (no verdict words); the GitHub model
— resolved (stored, anyone toggles) + collapsed (view state, `o`/`O`); a
resolved thread starts folded once per view; Resolve on EVERY note thread;
forge threads keep GitHub's state (no local reply / resolve); row tallies in
plain words (`12 remarks · 4 resolved`, short `4/12 resolved`), never ✓/✗/✔.

- **A remark reply is a stored child of its review.** `ParentID` = the
  review's note id (an older gg's orphan prune keeps it), `Remark =
  "review:<id>:<n>"` (the remark answered), stored at the REVIEW's address (commit-level /
  worktree-level, so the sweeps never expire it); `model.StoredRootID` /
  `Note.StoredParent()` map the remark id to the review id for the store's
  `dropOrphanReplies` (runs on EVERY part write — without the mapping the
  next write anywhere deletes B's replies), `capOldestFirst`, `Remove` (a
  review takes its remark replies) and `FileStore.Put` routing. Readers that
  walk stored notes skip `IsRemarkReply()` notes (overview, preview `mine`).
- **Join by fingerprint.** A reply stamps `RemarkFP` (sha256 of path, side,
  range, summary) and `RemarkSummary`; `Review.RemarkThreads()` joins replies
  and `review:` resolutions to the remark with that fp (same index first, else
  the moved remark, else an OUTDATED thread listed with the review's other
  notes). `reviewDocNotes` attaches them, so the TUI/web/working-tree diffs
  get remark threads for free.
- **`[[resolved]]` table per note part** (`model.ThreadResolution{Root, By,
  At, RemarkFP}`), written under the part lock; every part write drops
  entries whose root is gone (`keepRootedResolutions`). `Store.Resolve`
  routes to the root's part (a remark: its review's). Domain
  `NoteResolve(id, resolved, by)` takes a root, a reply or a remark id;
  `withResolutions` stamps `ResolvedNote.Resolution` on every public thread
  reader (forge: from the `resolved` tag; never a remark — its state comes
  from the fingerprint join). Every remark WRITE first re-keys the review's
  entries to their remarks' current ids (`rekeyRemarkResolutions`, all out
  then all in) — a re-save that moved remarks must not let one remark's
  resolve/reopen land on another's entry; a reply to an answer joins the
  answer's thread (`replyInThread`); an outdated thread is not addressable
  (no id printed, resolve refused).
- **Links:** `Note.Link` (a gg:// link or a full sha), normalised by
  `Service.NoteLink` inside `NoteAdd`/`NoteReply` (`ErrNoteLink`).
- **Frontends:** CLI `gg note reply --link`, `gg note resolve|unresolve`,
  `review:latest:<n>`, `gg review show` prints `id`, `resolved by`, replies,
  outdated threads, `N of M resolved` (the remark line format is pinned by
  s108 — unchanged); `gg note apply` comments take `link` + `resolve` (reply
  items only; resolutions applied after the replies, restored + rolled back on
  a failure). MCP `gg_note_reply`, `gg_note_resolve`. TUI: `R` on a remark
  (the review view still refuses `c`/`E`), `x` = Resolve/Reopen (help + `.`
  menu only — the diff footer is full), links open from the `.` menu ("Open
  link: …" through the `#` prompt — the diff cursor never sits on note rows),
  reply popup has a link field. Web: `POST /api/notes/resolve`, `replyable`
  on the wire, "Reply with a link…" row (the prompt has two fields), link
  line click, tallies on rows.
- `reviewHintMsg` carries its `svc`: a lookup that lands after a repo switch
  answers the steer with "the repository changed…" and does nothing.

### Note rows' "." menu: Open + Delete only (2026-10-05)

User ruling: a review is opened or removed, nothing else. A commit's three
note rows — Reviews (`noteID`), Range reviews (`noteScope`), Notes
(`notedPath`) — carry no path, and `availableActions` returns
`noteRowMenu()` alone for them (before any copy row): Open = enter
(`openDiffForFileLine`), Delete = a confirm defaulting to Cancel. A review
goes through `confirmStoredDelete` (`onStoredDeleted` drops the row via
`dropFilesRows`); a Range review / Notes row through
`domain.NotesClearAtCommit(commit, path, scope)` — exactly the threads
`ScopesByCommit` / `PlainByCommitPath` count (roots + replies; a reply has
no `Preview` of its own, so it follows its root; reviews never touched) —
then `dropFilesRows` (an emptied group loses its heading) and a srcNotes
reload. Web: right-click on the same rows = the same pair (`reviewMenu`
with an open callback, `scopeRowMenu`, `notedRowMenu`) over `POST
/api/notes/clear-row {commit, path|scope}`; the Branches review sub-row
menu gained Open too.

### Range review rows in a commit's Files view (2026-10-02)

A note written in a merge preview or a commit pair is an ordinary committed
note on the range's NEWEST commit, stamped with its scope (`Note.Preview`:
`<target>...<source>` by branch names, `<a7>..<b7>` for a pair). Most such
notes sit on files that commit does not change, so its Files view cannot put
them on a file row.

- `domain.NoteCounts.ScopesByCommit[sha]` groups the commit's notes by scope
  (sorted, thread counts); `PlainByCommitPath["<sha>:<path>"]` counts the notes
  written in NO scope. They replaced `PreviewsByCommitPath`.
- The commit's list is `withReviewLines(withScopeLines(withNotedLines(files)))`:
  a "Range reviews" heading with one `noteScope` row per scope (◆ N = every
  note of that scope on the commit, changed files included — the row stands
  for the whole review), then "Notes" for plain notes on unchanged files
  (`notedElsewhere` reads `PlainByCommitPath`).
- enter → `openScopeRow` → `domain.ScopeAtCommit(scope, commit)`: a pair is its
  own two commits (short shas resolved); a merge preview is
  `merge-base(target, commit)..commit` — frozen at the commit, whatever the
  branch did since. A missing target, or a commit already in the target (the
  branch was merged: the merge base IS the commit), is an error shown on the
  status line — never an empty diff. Then `PairOpen` + `openCompareFiles` +
  `pairNotesCmd`, exactly a saved pair's open, so the view is a PAIR scope
  and gathers every note along the range, not only that scope's.
- The way back is `m.filesBack` (`scopeBack{commit, scope}`): set AFTER
  `openCompareFiles` (which runs `closeFilesView` and zeroes it), read by
  `leaveReviewView`, which reopens the commit's files and sets
  `filesLandScope` AFTER `openChangedFiles` so the cursor lands on the row.
  Any other re-open inside the range view drops the way back; esc then closes.
- The web port (2026-10-02): `/api/notes/counts` carries `scopes_by_commit`
  (`{scope, label, n}`, the label from `domain.NoteScopeLabel` — the one
  wording both frontends print); `reviews.js` draws the rows after the Reviews
  ones and shares their cursor (`state.reviewSel = "scope:<scope>"`,
  `headRowIds`). `openRangeReview` asks `GET /api/scope-range` (the scope is
  allowlisted against the commit's own counts before it reaches git), opens
  `/api/compare-links?a=&b=` — a pair landing, so the pair note lane arms
  itself — and hangs the way back on the comparison (`state.compare.back`);
  `drillOut` asks `leaveRangeReview` before leaving the files stage.
- The Commits marker (2026-10-02): a commit with any `ScopesByCommit` entry is
  REVIEWED — `Model.commitReviewed` (TUI ✎) and `reviewMarkTitle` in
  reviews.js's pure section (web ✎ + tooltip; commits.js's one `rvmark` site
  goes through it). The TUI row's ◆ N is `NoteCounts.PlainCommitNotes` — the
  notes written in no scope — so a range review is never counted twice.
- Review notes show ONLY in the review (2026-10-02, user ruling): a note with
  a scope (`Note.Preview`) is the review's, never the commit's own view's.
  `domain.PlainNotes` filters a commit's own reads — the TUI's `loadNotesCmd`
  / `stackNotesCmd` / the Notes-row popup, the web's `/api/notes` (unless
  `scoped=1`) — and every commit-view badge / note-step predicate reads
  `PlainByCommitPath` (web: `plain_by_commit_path`). View all notes opens a
  note where it is stored: its diff sets `diffView.rangeNotes` (web:
  `showRangeNotes()` → `diffCtx.scoped`). The other half: a range opened from
  a Range review row narrows its set with `PreviewNoteSet.Only = <scope>` —
  `loadPreviewNotes` keeps that scope's notes alone, the counts cache keys on
  it, and `Pair()` returns it so a note written there joins the review (TUI
  `pairNotesCmd(a, b, only)`; web `state.compare.pair.scope` →
  `/api/pair/notes?scope=`, and `notePreview` accepts a scope the commit
  already holds without resolving it). Since the user's second ruling (2026-10-02)
  EVERY scope does: `PreviewNoteSet.scope()` is `Only`, else the set's own
  `Pair()` name, and `loadPreviewNotes` keeps the roots whose `Note.Preview`
  equals it plus their replies — a saved preview/pair, a pair link, CLI
  `--preview` and MCP preview reads included. Only a pull request's set
  (source `refs/gg/pr/<n>`) is exempt: its local notes carry no portable
  name. A note that must show in a preview has to be WRITTEN in it (test
  fixtures stamp `Preview`); address reads (`--rev`, `NotesAt`) stay whole.
- View all notes → a review's note (2026-10-02): the TUI's enter on a note
  with `Note.Preview` goes to `openAllNotesReviewNote` (all_notes_scope.go):
  a loading diff layer over the popup, then `ScopeAtCommit` + `PairNotes`
  (`Only` = the scope) + the file's row from `CompareFiles` (its status decides
  which sides the diff reads), and `onAllNotesScope` stamps the layer
  (`previewSet`, `noteAddr`, the `cmp:` diffTag, the landing's tag) before
  `loadCompareDiffCmd`. On a range that cannot be resolved it pops the layer
  and opens the stored commit (`rangeNotes` diff). The web's `openTarget`
  takes the note's `preview` and tries `openScopeRange` (reviews.js — the
  shared core of `openRangeReview`) first, the commit second.
- The web's Notes rows (2026-10-02): `notedElsewherePaths` (reviews.js pure
  section) over `plain_by_commit_path`; rows carry `data-noted`, share the
  head-row cursor (`state.reviewSel = "noted:<path>"`), and open
  `openNotesWindow` (shelfnotes.js) with `/api/notes` for that path.
- WHERE a review shows (user rulings, 2026-10-02) — three kinds, each in one
  place: (1) a PREVIEW's review (scope `<target>...<source>`,
  `domain.IsPreviewScope`) shows in its preview and NEVER on a commit — not
  in `ScopesShownOn`, so no ✎ and no row; it is listed in View all notes and
  opens there as "Preview review". (2) a RANGE review (a commit pair) and
  (3) a BRANCH's AI review (`Review.Branch`) show only on the branch they
  were created on: `domain.ReviewShownOn(reviewBranch, viewing)`, where
  viewing = `Model.viewBranches()` / reviews.js `viewBranches()` — the solo
  scope, else the checked-out branch; empty (detached) shows everything, and
  so does a review with no branch recorded. `NoteAdd` → `stampReview` writes
  `Note.PreviewBranch` (pair: the checked-out branch) and `Note.PreviewBase`
  (preview: the merge base at write time; `ScopeAtCommit` prefers it, so the
  range opens after a merge). Filters: `NoteCounts.ScopesShownOn`,
  `ReviewsShownOn`, `NotesOverview.ShownOn` (TUI `onAllNotes`; web
  `/api/notes/overview?on=<branch>`). `PlainCommitNotes` still subtracts
  every scope, so a hidden review never turns into the commit's ◆ N. Test
  fixtures that expect a range review row must be ON the review's branch.
- `unfoldFilesForOpen` (files.js): opening a commit (`openCommit`,
  `openCommitByHash`) unfolds a folded file list and stores it — a commit
  opens onto its files, never onto the strip. The exception is a caller going
  straight on to one file's diff (`openCommitByHash(…, {thenFile: true})`: a
  steered link, the viewer's diff, View all notes), where the fold stays.

### Review notes inside a stack (plan 4a, 2026-09-23)

Spec §11 item 1, plan `docs/superpowers/plans/2026-09-22-stacked-notes.md`.
The TUI half needed no new data plumbing: `stackFile.d` is the single-file
view the file's ORDINARY loader built, so its `noteAddr`, `previewSet` and
rows are that loader's stamps — one file deeper than before, same rule.

- **Every anchor is scoped to a file.** `lineAnchorIn(lo, hi, …)` and
  `noteAnchorLineIn` replaced the whole-stream searches, and `noteRowIndex`
  runs once per file over `notesOf(i)` inside `fileLineRange(i)`. Without
  that, line numbers repeat (every file has a line 12) and the first file
  carrying the number collects every file's notes. A forge FILE-level note
  anchors on its own file's first body line, not on the stack's.
- **`stackNotesMsg`, never `notesLoadedMsg`.** The single-file message is
  gated on the view's ONE `diffTag` and replaces the whole view's notes; in a
  stack that drops every answer but the last and writes it over file 0 — the
  same trap `stackFileMsg` avoided for `diffMsg`. `applyStackFile` fires
  `stackNotesCmd` for the file that just arrived; `loadNotesCmd` fans out over
  every LOADED file, which carries each existing refresh site (the `srcNotes`
  arrival, a mutation, a PR re-poll) into the stack unchanged.
- **`curNoteView()` is the accessor** the design reserved: the cursor's
  `stk.files[curFile()].d`, or the view itself. `diffNoteAddress`,
  `previewNoteSet` and `previewNoteScope` read it, so `c`/`E`/`R` act on the
  file under the cursor. Notes stay inert exactly where they are inert
  single-file — a file whose loader stamped no address (every two-sided
  compare). No stack-specific refusal exists any more.
- **`}` / `{` cross the stack.** The in-view walk already sees every loaded
  file's notes; `stackNoteStep` handles the rest — the next file that carries
  notes (`notedStackFile`: its resolved notes, else `NoteCounts`, which
  answers WITHOUT the file's diff), unfolded and queued, with the landing
  parked on `diffStack.land`. Nothing is armed: a stack holds every file of
  the list already, so there is no file to step TO.
- **`stackLanding` is one parking slot for two gestures**: `dir != 0` = this
  file's first/last note (`}`/`{`), `no > 0` = this exact line (a `gg://`
  link, `gg session navigate`). `drainStackLanding` consumes it when that
  file's rows — and, for a note landing, its notes — are in the stream. A
  note landing that finds nothing KEEPS the landing (its notes are still in
  flight); a line landing clears it.
- **`landSteer` resolves the file first** when stacked, and re-reads its
  bounds on EVERY probe: `expandFoldFor` lengthens the stream, so a range
  captured up front cuts the search short. A parked steer reaches a stack
  through the `stackFileMsg` handler (`drainPendingDiff`), because a stack's
  files never arrive as `diffMsg`.
- **Leaving with `S` keeps the line.** `unstack` parks a `lineLanding`
  (`Model.diffLand`, tag-gated) that the `diffMsg` handler drains, because
  the single view is re-opened asynchronously and lands on its first change.
- **The notes LIST covers the stack**, one row per thread with its file named
  (`noteListEntriesIn`), and `gotoNote` unfolds the thread's file before
  anchoring inside it. `Remove all notes…` is per ADDRESS, so it stays the
  CURSOR's file and is not offered while that file has none.
- **Footer:** the stacked line gains `[c}{] note` and lands on exactly 140
  columns; `ctrl+↑`/`ctrl+↓` had no room and live in the `?` help.
**The web half** (same plan, second merge) is the `activeDiff()` accessor §5.1
reserved, and nothing more:

- **`diffHTML` takes the note context explicitly** — `nctx = {ctx, notes,
  row}`, threaded into `noteRowsHTML` / `fileNoteRowsHTML` / `noteBoxHTML`,
  `curCls`, the changes-only fold's `noted` pin and `attnKey`. NOT a
  module-level "current slot": a stack paints one slot while another's notes
  are in flight, and a shared variable would race them.
- **One context builder, two callers.** `commitDiffCtx` / `statusDiffCtx` were
  factored out of `openFile` / `openStatusDiff`, and `rowNoteCtx(f)` mirrors
  `openFile`'s own dispatch for a stack's slot. A second builder would drift
  silently, and the drift is a note filed against the wrong file.
- **A slot fetches its own notes** in `load()` (`notesFor(ctx)`, the read half
  of `fetchNotes`), seeds the forge's resolved threads into the view-wide
  collapse set once, and repaints through `repaintSlot` so the reader's pin
  keeps their place. `fetchNotes` in a stack fans out over the loaded slots
  (`refreshStackNotes`) — one address per file, so there is no single re-read.
- **Both GATES read the active slot** (each cost a probe run): `noteKey` in
  keys.js gated on `notesArmed()` — the GLOBAL ctx, which is null while a
  stack is up, so every note key was dead; and `#diff-body`'s click listener
  did the same, so no row could be marked. They now read `activeDiff().ctx`
  and the clicked row's own file (`rowSlotCtx`).
- **`landStackLine`** is the web twin of the TUI's `stackLanding`: unfold,
  fetch, then find the row INSIDE that file's section — a pane-wide lookup
  marks whichever file carries the number first, and every file has a line 23.
- **Probe:** `stack-probe/notes.mjs` (+ `mknotes.sh`) and `land.mjs`, run
  against the unfixed build first, chromium AND firefox. The stored
  `stacked_diff` pref makes `S` a TOGGLE across runs — the probe drops
  `ui-state.json` and re-presses `S` if the first one turned the stack off.

- **Gotchas found in verification:** a conflicted header must show no counts
  (its body is the resolver line); `tui-capture.sh` now isolates
  `XDG_STATE_HOME` itself (`--state`, `env` INSIDE the tmux command — an
  exported variable never reaches the pane), and a capture re-run in the same
  state dir inherits the pref the previous run's `S` wrote, so a snapshot can
  silently exercise the single-file path instead.


### In-view search inside a stack (plan 4b, 2026-09-23)

Spec §11 item 2, plan `docs/superpowers/plans/2026-09-23-stacked-search.md`.
**The TUI already searched a stack** and nobody had noticed: `searchLines()`
walks `v.lines` and skips only the lines that are not bodies, so every file
whose diff has arrived and is unfolded was in the one document all along
(`TestStackSearchAlreadySpansLoadedFiles` pins it). 4b is therefore about the
part of the document that is NOT in the stream — a folded file's rows, and a
file that has never been fetched — plus one real ordering bug.

- **The late-load re-find ran too early.** `applyStackFile` called
  `rebuild()`, whose tail re-finds the query against `v.curLine`, and only
  THEN remapped the cursor through its `stackAnchor`. A file arriving above
  the reader lengthens the stream, so the re-find measured from an index the
  new stream no longer meant and the current hit snapped to a neighbour's.
  `rebuild()` now splits into `rebuildLines()` + `refindAfterRebuild()`, and
  the stack's loader runs the re-find AFTER the remap. Same shape as plan 3's
  `reanchorAfterRebuild` gotcha; a dense-hit fixture is needed to see it (two
  far-apart hits re-snap to the same one and the test passes vacuously).
- **Strict first, then hunt, then wrap (D2/D3).** `stackHitStep` tries
  `stepHitStrict` (new in `textsearch.go`, `stepHit` without the wrap); a wrap
  is the signal that the stream has run out in that direction, so it looks for
  the next `searchableFile` that is folded or unfetched, unfolds it, moves the
  cursor to its header — which is also what puts it inside `wantLoads`'
  window — and parks a `stackHunt{file, dir}`. `drainStackHunt`, called from
  `applyStackFile`, lands on that file's edge hit or hands the step on to the
  next candidate. One file per round trip: `/` itself loads nothing.
- **`searchableFile` is the predicate everywhere.** Conflicted, binary, too
  large, errored and content-identical files can never hold a hit, so they are
  neither hunted nor counted as unsearched — otherwise the `+` never clears
  and the cascade walks into a dead end.
- **The badge's `+` (D1).** `searchBadge()` = `search.badge()` plus `+` while
  `unsearchedFiles() > 0`; the count is over the whole stack, not the file.
  Like `badge()` it carries no `i18n.T` — it is punctuation around the user's
  own query.
- **A hunt is cancelled by any other key (D5)**, cleared beside `noteLand` at
  the top of `updateDiffViewKey`, so an answer already in flight cannot yank
  the cursor away from wherever the reader went.
- **Verified** with `tui-capture.sh` over a 30-file fixture
  (`stack-probe/mksearchmany.sh`): `/needle` reads `1/1+` in `f00.txt`, one
  `]` cascades to `f27.txt` (`2/2`, no `+`, `file 28/30`), the next wraps back.
  A 3-file fixture proves nothing here — its whole stack loads eagerly.

**The web half** (same plan, second merge) had to build what the TUI already
had, and the lever is that a stack is ONE document:

- **One `diffSearch`, keyed per slot.** `before()` orders positions by a
  NUMERIC row, so each slot's rows are keyed `k * STACK_ROW_SPAN (1e9) + the
  row's index in its own diff`. That single change buys document order across
  files, globally unique `data-h` (there is one Search, so hit indices are
  global), and leaves `inviewsearch.js`, `searchbar.js`, `renderCell` and
  `goToDiffHit` exactly as the single-file view uses them. Per-slot Search
  objects would have needed all four to learn about slots.
- **The render reports, the stack re-finds.** Single-file the render re-finds
  (the fold set lives inside `diffHTML`); a stack cannot, or the hit list would
  hold only the last slot's hits. `diffHTML` now takes `hctx = {search, base,
  lines}`: given one it PAINTS from the stack's search and hands its searchable
  lines to the sink instead of re-finding. `refindStack` concatenates the
  cached `s.lines` and re-finds once, then repaints only the slots whose hits
  changed (`s.hadHits`). One collapse per slot, and the search and the paint
  provably share a fold set.
- **Re-find sites:** `load()` (a slot arriving under a live query brings rows
  the search has never seen), `rerenderStack()` (`f`, `w`, a resize change
  which rows exist) and `reconcileStack()` — which also RE-ANCHORS, because a
  working-tree refresh renumbers the slots and a composite row then names
  another file. Each also calls `diffSearchBar.paint()` or the count goes
  stale (the in-view-search probe's own lesson).
- **The gate was `!state.lastDiff`** in `diffSearchKey`, which is null while a
  stack is up — keys.js's "search works in the single-file view" toast was only
  the visible half and is gone. Same shape as 4a's two dead note gates.
- **The bar grew two optional host verbs:** `count()` (a stacked count spans
  files not yet searched and says so with `+`) and `step(delta)` (a host that
  must FETCH to reach the next hit steps itself — `bindSearchBar`'s own step is
  synchronous). `awaitSlot` was lifted out of `landStackLine` and is shared.
- **Probe:** `stack-probe/search.mjs` + `mksearchmany.sh`, against the unfixed
  build first (it fails at `/` — the stack refused it) then chromium AND
  firefox. It caught one defect no unit test was red for: **esc left the tint
  painted**, because `refindStack` returned early on an empty query and the
  bar's clear-then-render is the only thing that unpaints.
- **A probe bug worth remembering:** `stack-probe/land.mjs` (plan 4a's link
  landing) started failing on `main` as well — the pane went EMPTY and
  `landStackLine` was never reached. It navigated to `rev-parse HEAD` while
  CLICKING the commit "three", and `mkrepo.sh` has since grown a later "many"
  commit in which `c.txt` does not exist: `steerNavigateLand` opened that
  commit, found no such file (`findIndex` → −1) and returned, leaving the stage
  switched and no diff open. The probe now resolves the sha it means
  (`rev-list -1 --grep=^three$`). That it reproduced IDENTICALLY on the
  unchanged `main` is what proved it was the probe and not the feature — run
  the differential before debugging your own diff. A navigate naming a file the
  target commit does not carry used to land on an empty pane with no notice
  (pre-existing, not 4b). FIXED 2026-09-24: `steerNavigateLand` (live.js)
  opens the file through `openNamedFile`, which says `gg link: <file> is not
  in <commit …|the working-tree diff|preview t...s|ref|a..b>` on a miss, and a
  line the diff lacks says `line N is not in <file>'s diff` (single file and
  stack) — the TUI's steerFail wording, as a red op-line, since the web steer
  is fire-and-forget and the page is the only place to report it. Probe:
  `stack-probe/navmiss.mjs`.


### Change navigation: from the viewport, and across a stack (2026-09-23)

Two user-reported bugs, one root: the change keys reasoned from a cursor the
reader could not see.

- **`n`/`p` stepped from a change that had scrolled out of the pane.** A free
  scroll (arrows, wheel) moves the viewport and deliberately NOT the cursor, so
  `v.cur` can point far off screen; with a single change in the file `n` then
  only primed the wrap and the reader needed a second press. `reseatFromViewport`
  (diff_view.go) re-seats `cur` on the first change at or below the pane's top
  (`n`) or the last one above it (`p`) whenever the focused block is outside
  `[offset, offset+body)`, and that re-seat IS the step. On screen, the shipped
  stepping and its wrap arm are untouched. The web has had this rule since the
  in-view search work (`visibleChangeBlock`) — but only on re-render; its own
  `stepChange` stepped from a stale index until 2026-09-24, when it gained the
  same rule as the pure `changeStepTarget(tops, cur, top, bottom, delta)`
  (files.js, node-tested in `changestepjs_test.go`) plus keys: `.`/`,` in the
  diff layout (USER RULING: `p` is pull and `-` folds a stacked file, so not
  n/p or -/=) and a `. next change` footer chip. The web does not wrap, so
  where the TUI hands over to its wrap arm the web stays on the last / first
  change; the conflict picker keeps stepping its regions by index.
  **In a stack** (`stackChangeStep`, stackview.js — the web's `huntChange`,
  2026-09-24 after the user found `.` stalling at the last loaded file, and
  `,` stranded after a click on a file low in the list): the rendered rows
  decide first; a folded or never-read file between the start and that target
  (pure `stackHuntSlots`, node-tested) is opened — unfold, scroll to it, wait
  — and the step lands on its edge change; empty files are passed over. The
  landed change is kept as its ROW (`st.changeRow`), never an ordinal: a file
  loading above shifts ordinals, and a stack inherited the single-file open's
  `diffBlockIdx = 0`, so the first `.` skipped the first change. Two
  `awaitSlot` fixes shared with `]`/`[` and link landings: it returns only once
  the slot is PAINTED (`s.inLoad` spans load() up to its repaint — `load` was
  "ok" before the notes fetch, so a hunt read the placeholder, found no rows
  and skipped the file), and its 8 s cap counts only unpicked time, never a
  fetch in flight. Probe: `stack-probe/changehunt.mjs` (fixture on /mnt/t —
  in /tmp everything loads in time and nothing reproduces).
- **In a stack `n` stepped FILES** (plan 3 made `v.blocks` the header indices),
  so a one-file stack had nowhere to step: `n` primed a wrap and the second
  press landed on the file header at the top of the scroll, with no line cursor
  at all. **USER RULING 2026-09-23, reversing that:** `v.blocks` are now every
  file's change starts in stream coordinates, so `n`/`p` walk the stack as one
  document and all the stepping, wrapping and ordinal code is reused unchanged;
  `N`/`P` — inert in a stack until now — became the file step, which is what
  they already mean single-file; `ctrl+↑/↓` keep the in-file walk; `J` still
  jumps by name. `stackFile.hdr` remains each file's header index, and
  `goToStackFile` is the one "go to this file" (N/P, J, and the stack's own
  opening all use it).
- **A change in a folded or never-fetched file is still reachable**: `n`/`p`
  reuse 4b's parked step — `stackHunt` gained a `kind` (`huntHit` for `]`/`[`,
  `huntChange` for `n`/`p`) and `ownsKey`, so each gesture's own keys keep its
  hunt alive and every other key abandons it. `drainStackHunt` lands on the
  file's edge CHANGE for a change hunt and its edge HIT for a search one.
- The footer traded `[J] files` for `[N/P] file` (both lines sit exactly on the
  140-column budget); `J` keeps its `.` menu row and its help row. The stacked
  header shows `change X/N  file Y/M`.

### Line staging in gg web — GitKraken's model (2026-09-23)

User ruling, reached over three rounds of live feedback, with GitKraken's own
help page as the reference: select ROWS, then right-click to act, at once. It
replaced, in turn, a block pick (a click took the whole hunk) and a per-cell
pick (a click took one side of one line) — both pick-then-apply with a
`stage selected (n)` bar. Neither round had been asked for; each was a guess
at "what the user means" that a question would have avoided.

- **The unit is the ROW.** A modified row is ONE change (its old line replaced
  by its new one) whichever cell is clicked; a removed-only row is its old
  line, an added-only row its new line. Staging half a modified row would
  leave both lines, or neither, in the index — so it is not offered.
- **Server: `applyRowSelection` (hunks.go)** builds the new index content row
  by row, git add -p style: a selected row contributes its TARGET version, an
  unselected row keeps its current one. Two lanes, both oriented the way the
  reader sees them: `unstaged` (index → working tree, target = the working
  tree) and `staged` (HEAD → index, target = HEAD — which is how a row is
  unstaged). Whole hunks ride the same door (`whole: true`). The original
  `{picks: [ordinals]}` request still decodes, as whole hunks.
- **Row identity on the wire:** `/api/diff` tags rows with `hunk` and `hr`
  (the row's ordinal inside its hunk) in BOTH lanes, and `hunks.lane`. The
  latch is stricter than before: `sameBlockShape` requires the displayed rows
  (the Differ's alignment) and the doc's rows (`textdiff.Compare` with zero
  options, what `FromDiff` uses) to split into the same blocks of the same
  row counts — a row ordinal is meaningless otherwise, so the diff simply goes
  untagged.
- **Client:** ONE selection spans everything on screen — every file of a
  stack, which reads as one document (user ruling 2026-09-24, after live use:
  per-file selections miscounted across files, a shift range could not cross
  files, and nothing deselected). Each file keeps its share
  (`scope.hunks.sel`, positional against ITS bytes); `selectionOrder` /
  `currentSelection` / `applySelection` treat the shares as one, with one
  global `rowAnchor`. `selectStep` (pure, node-tested) applies a click: plain
  = that row alone, ctrl/cmd toggles, shift = the range in document order
  across files; a selection holds ONE lane. `hunkMenuRows` counts the whole
  selection (`selectionSize`); `stageSelection` → `stageJobs` paints every
  file's prediction, then POSTs one file at a time (a failed file reverts
  alone), then one status + quiet per-file re-reads (one structural
  `reconcileStack` if any file left the stack).
- **Speed = git process count** (WSL's /mnt drives: 50–300 ms per git
  call, whatever it does). `/api/stage-hunks` takes a batch
  (`{"files": [...]}`, one entry per file, a stale hash anywhere = 409 and
  nothing staged): one `engine.StageHunks{Files}` → `git.StageBlobs` (one
  `ls-files -s -z`, a `hash-object` per file, ONE `update-index`), and the
  batch answer is `{diffs: [{path, lane, diff}]}` built by
  `worktreeDiffPayload` from bytes in hand (the index is read back only when
  the content has a `\r`, i.e. an eol filter may have rewritten it; any
  OTHER clean filter — LFS, `filter.*.clean` — makes the answered hash
  wrong, so the next action 409s and re-reads: safe, one extra trip) — NO
  status; the client lands the diffs, then `fetchStatus()` in the
  background. The single-file form still answers the status. `StageHunks`
  is `IndexOnly()`, so `Execute` skips the versions preflight probe — and
  so is whole-file `engine.Stage` (stage / unstage / all: TUI space, web
  `/api/stage`, `gg add`), 3 git processes less per action (2026-09-24);
  `git.Repo.Root` (set by `domain.Open` once `resolveRoot` succeeds) makes
  `TopLevel` free, so a working-tree read is no longer a `rev-parse`; and
  `/api/diff`'s wt form reads each side once (`memoSide`) for both the
  alignment and the staging doc. Pinned by `stagebatch_test.go` (git call
  counts) and the domain/git counting tests.
- **Deselect:** a left click on anything that is not a selectable row (or the
  ctx menu) clears the selection — a document-level listener; Esc clears it
  before a search or the diff itself (`keys.js`). **Double-click** on a row
  that was selected stages the whole selection — `preClickSel` is taken at the
  sequence's FIRST click (`e.detail <= 1`), because that click has already
  collapsed the selection to one row by the time `dblclick` fires; on an
  unselected row it clears and stages that one row.
- **Gotcha — a modifier-click is a TEXT gesture to the browser.** Shift-click
  extends the page's text selection from the previous click, and the plain-
  click guard ("don't act mid text-selection") then swallowed it. A mousedown
  with shift/ctrl/cmd on a selectable row is `preventDefault`ed, and a
  modified click clears the text selection itself.
- **Gotcha — CSS rounds must REPLACE, not add.** Round two's rules were
  written beside round one's, so round one's whole-row box and the ✓ in both
  gutters kept painting over a model that no longer had them. The guard
  `TestSelectionMarksTheWholeRow` now fails if any earlier round's selector
  (`pick-l`, `pick-w`, `tr.hk.picked`, `#hunk-bar`, the ✓) reappears.
- **The screen moves first (user request: "change the UI, send, revert on
  error, else update selectively").** `optimisticRows` predicts the diff after
  the action — staging turns a modified/added row into context and drops a
  removed one, renumbering the index side (left); unstaging is the mirror on
  the right — and `applyRowStage` paints it BEFORE the POST, with the file's
  tags stripped (they name the old bytes) so nothing can act on stale
  ordinals meanwhile. An error restores the previous diff and selection; a
  409 re-reads instead. Success re-reads ONE file quietly (`quietRefreshFile`:
  no placeholder, scroll and folds kept) — the old path re-opened it through
  `openStatusDiff`, which is where the "loading…" flash, the fold reset and
  the jump to the first change came from.
- **The stack's live refresh was the other flicker.** Every index change
  fires the watcher, and `reconcileStack` repainted EVERY file. With the same
  files in the same order and status it now re-reads each loaded slot
  quietly (`quietReloadSlot`) and repaints one only when its rows changed;
  the full repaint stays for a file entering or leaving the section. The
  probe proves it with a control: the previous build replaces an untouched
  file's table on a stage elsewhere, this one does not.
- Double-click acts on the one row under it (`actOnRow`), in either lane.
- **Probes:** `stack-probe/gkoptimistic.mjs` holds and fails the POST with
  request interception, so "painted before the answer", "no loading flash"
  and "reverted on error" are measured, not assumed.
- **Probes:** `stack-probe/gkstage.mjs` (single file, both lanes; reads the
  index with real git after every action) and `gkstack.mjs` (per-file
  selections in a stack), red on the installed build at the first assert,
  green in chromium and firefox; `commitbox.mjs` for the sidebar. The older
  `hunks.mjs` / `hunkline.mjs` / `hunkux.mjs` test models that no longer exist.

### Working-tree hunk staging inside a stack (plan 4c, 2026-09-23)

Spec §11 item 3, plan `docs/superpowers/plans/2026-09-23-stacked-hunks.md`.
Two premise checks decided this plan's size, and both held:

- **The working-tree stack already reconciles.** `reconcileStatusStack`
  (`diff_stack_keys.go`) runs from `withStatus` — the ONE place the
  Files/Staged membership is derived — so a staging round's status write
  already drops the files that left the section, inserts new ones, marks a
  re-read file `stackStale` (its old rows stay on screen until the lazy queue
  reaches it) and keeps the cursor BY PATH. Nothing had to be built for "what
  the stack does after a stage"; `TestWorkingTreeStackReconcilesOnStatusWrite`
  and its empty-section twin pin it.
- **There is no inline picking lane in the TUI to make per-file.** Hunk
  staging is a full-screen `hunkPicker` LAYER, opened from the Files / Staged
  panel's `H`. So the diff view got that same `H` (`diff_stack_hunks.go`) and
  the picker opens OVER the stack — `pushLayer` returns to the layer beneath,
  which is how the reader keeps their place. The web half picks inline per
  slot; that asymmetry is deliberate, not an oversight.

What `H` does: `hunkFileHere()` names the file it acts on — the CURSOR's file
in a stack (never the Files panel's selection behind the view), the open file
in a single working-tree diff — and returns a refusal string instead where the
staging op could not apply: a conflict (which keeps its `enter` resolver door,
spec §9), an untracked file, a staged `A` in the unstage lane, a running
operation, or a commit / compare stack. The Staged section opens the UNSTAGE
picker. `hunkKeyApplies()` gates the chrome, on the view rather than per file:
a chip flickering as the cursor moved between files would be noise.

Gotchas this cost:

- **A refusal the reader cannot SEE is a key that does nothing.** The first
  build posted `hunkFileHere`'s reason to `m.statusMsg` — the PANELS' status
  bar, which a full-screen diff covers — so `H` on an untracked file looked
  dead (the user met it that way). Refusals raised inside the diff view go to
  `m.diffNotice`, the view's own transient cue, like every other notice there.
  The tests that let it ship asserted the FIELD; they now render `View()`.
  And `hunkKeyApplies` gates the footer chip on the FILE in a single-file diff
  (one file, so it cannot flicker) while a stack stays view-level.

- **A single-file working-tree diff has no reconcile of its own** — nothing in
  the TUI reloads one on a status change at all. So the picker's apply parks
  `Model.hunkReload{path, staged}` and `statusRefreshedMsg` consumes it once
  (`takeHunkReload`): the file is re-read where it is still in its section, and
  the diff CLOSES with a notice where it is not. Parked rather than
  unconditional on purpose — reloading on every status write would throw the
  reader to the top of the file whenever a watch-driven refresh landed. A
  stack arms nothing (`armHunkReload` returns early), because its reconcile
  keeps the reader's place and a reload would blank the view.
- **Both footer lines sat exactly on the 140-column budget.** A working-tree
  diff therefore BUYS its key: `diffHintFor(long, stacked, wt)` trades
  `[h/b] hist` for `[H] hunks` there and nowhere else. `h`/`b` stay in `?` and
  in `ctrl+p` (File history / File blame), and `H` also has a `.` menu row
  (`diff-hunks`), which is what keeps it discoverable on the single-file line.
- The 4a-era help row claiming "in-view hunk staging and the changes-only fold
  are single-file for now" was half false after this; it now speaks for the
  fold alone, and the orphaned key came out of all four bundles.
- **Evidence** (`tui-capture.sh`, three modified files with two hunks each):
  `tab enter S n H` → the picker titled `Stage hunks: three.txt   2 hunks`
  while the panel behind still had `one.txt` selected; `space ctrl+s` → back in
  the stack, still `file 2/3 three.txt`, whose header had become
  `▾ M three.txt +2 −1` — the stale re-read landing, with `git status` showing
  `MM three.txt`.


**Web half.** `diffHunks` — the module-level "the open file's staging state" —
was exactly the global notes (4a) and search (4b) had each had to lose:

- `diffHTML` gained a THIRD explicit context, `kctx = {picks}`, beside `nctx`
  and `hctx`; `hunkCls`/`hunkAttr` read it and never the global. The
  single-file view passes its own `diffHunks` in, so its lane is unchanged.
- Every slot arms `s.hunks = {path, hash, count, picks}` from its OWN diff
  (`d.hunks`, which only `/api/diff?wt=unstaged` carries) — slots already
  fetch through `fileDiffURL`, so no server work was needed — and paints with
  its own picks. A click resolves the row's slot (`hunkSlotAt`) and toggles
  THAT file's picks; `paintHunkPicks` repaints only that section, because
  another slot's rows carry picks of their own.
- **The bar cannot follow the anchor.** `st.anchor` is derived from the scroll
  position (`syncCursor`), so a pick click followed by any scroll — or by the
  file-list re-render a pick triggers — swung the bar to whatever file sat at
  the pane top and staged THAT. The browser probe caught it in the act: a pick
  in beta, then a pick in delta, posted `path: "beta.txt"`. A slot with LIVE
  picks now owns the bar (`st.pickK`), which falls back to the anchor once
  nothing is picked, and the button names the file (`stage selected (1) in
  delta.txt`) because it need not be the one on screen. This was the risk the
  plan's self-review flagged and left to be decided with evidence.
- **A refresh must not eat picks in other files.** `reconcileSlots` re-fetches
  every kept slot, which re-arms `hunks` — so picks are parked in `hunksPrev`
  and restored when the fresh `hash` is identical (the bytes did not move, so
  the ordinals still name the same hunks) and dropped otherwise. Staging one
  file therefore leaves the reader's picks in another intact.
- A staging round in a stack does NOT call `reopenAfterHunkStage` (it re-opens
  the file as a single diff and tears the stack down): the 200 body is a fresh
  status, so `applyStatus` → `reconcileStatusView` → `reconcileStack` keeps the
  reader's place and re-fetches the file that changed. `reconcileStatusView`'s
  gone-file guard is likewise the single-file lane's only — in a stack each
  slot answers for itself.
- **Probe:** `stack-probe/hunks.mjs` + `mkhunks.sh` (four files, two hunks
  each, plus an untracked one), red on the unfixed build at the first assert
  (no `data-hunk` in a stack at all), then chromium AND firefox. Two of its own
  lessons: it must REBUILD its fixture per run (a previous run's staging left
  the index half full and produced one wrong diagnosis), and `#op-line` carries
  the word "Problem" in its static markup, so an error check reads `#op-text`
  and the `hidden` class, never the element's text.

Known parity gap, pre-existing: the web has no inline UNSTAGE for the staged
section (the TUI's `H` covers both lanes). Recorded in `docs/web-tui-parity.md`.


### Line select / copy inside a stack (plan 4d, 2026-09-24)

Plan `docs/superpowers/plans/2026-09-24-stacked-select-copy.md`. Premise
checked first: `f` was ALREADY stack-wide (web `toggleDiffView` →
`rerenderStack`; TUI `spliceStack` reads `v.partial`) and the TUI range
already spanned files (header/gap/rule lines are skipped by `selectedLines`).

- **TUI:** `rebuildLines()` used to clear `lsel` on every re-splice — a lazy
  `stackFileMsg`, `-`/`_`, `stackNotesMsg`, a working-tree reconcile — so a
  range scrolled across files vanished. The stack branch now wraps the splice
  in `holdSel()` / `restoreSel()`: each end as a `stackAnchor` plus the file's
  PATH (a reconcile renumbers files; the reconcile holds BEFORE swapping the
  list, since the rebuild's own hold reads old indexes against the new list).
  A folded end lands on its file's header; `selKept`/`selKeptAt` remember the
  UNCLAMPED hold while `lsel` is untouched, so fold + unfold gives the range
  back (else `_`,`_` shrank it to the headers). What changes a file's own lines
  still clears, after the rebuild: `f`, and `expandFoldFor` (partial off for
  a note). Tests: `diff_stack_select_test.go`.
- **Web:** copy = the browser drag. `sideCopyText(cells, side)` (pure, guarded
  by `sidecopyjs_test.go`) keeps one side: other-side cells drop, as does the
  absent half of an add/del row; a side-less cell (unified context) is both.
  A mousedown stamps `#diff-body[data-selside]`; style.css makes the other
  side, `.stk-head` and `tr:not([data-i])` (folds, notes) `user-select: none`
  so the highlight IS the copy. `diffSelectionText()` walks every
  `tr[data-i] td.side` against ALL selection ranges (Firefox splits around
  unselectable content), clipping to each range and dropping an edge cell with
  nothing selected. A `document` `copy` listener and the diff ctx menu's
  *copy* row both use it. The capture-phase outside-click listener ignores a
  `detail <= 1` click with a non-collapsed selection inside `#diff-body` (a
  drag's release); a double-click (detail 2) still clears.
  Known edges (not fixed): a drag RELEASED over another pane clicks on the
  common ancestor, outside `#diff-body`, so it still clears the marks; a
  mousedown INSIDE an existing selection starts the browser's text
  drag-and-drop, keeps the old selection, and the stamp re-labels its side.
  The guard test pins the outside-click listener's text verbatim — put new
  early returns ABOVE the `closest(...#ctx-menu)` line.
- **Found during 4d — notes in a stacked merge preview (TUI):** the compare
  loader INHERITS the note address from the layer on top (`inheritIdentity`),
  and in a stack that layer is the stack view, which names no file — only the
  seed file (its view copied in) kept notes. `stampPreviewNotes(dv, path)` is
  now shared by the single-file opener and `applyStackFile`. Same root for the
  box title: `noteBoxLines(r, innerW, owner)` reads the title's path and
  preview scope from the note's FILE view. Rule: any render/load path that
  reads `v.noteAddr` / `v.previewSet` must go through the file's own view in
  a stack.
- **Probe gotcha:** Firefox ignores `clipboardData` passed to a synthetic
  `new ClipboardEvent("copy", …)` — press a real `Control+c` and read the
  payload in a window-level `copy` listener instead (`stack-probe/dragcopy.mjs`).

### Drag & drop compare in the `gg web` Previews section (2026-09-21)

Spec `docs/superpowers/specs/2026-09-21-web-previews-dnd-compare-design.md`.
Client only, all in `static/previews.js` (it owns `#previews-list`; `sidebar.js`
cannot import it — the import cycle).

- **`rowLink(e)` is the one link of a row**: a pair's `e.link`, a merge
  preview's `links.js` `previewRowLink(e)` (the builder its "copy gg link" row
  uses — one builder, so the compared text IS the copied text, hint included),
  `""` for a comparison (two links). A row without a link is refused by the
  listeners on both ends.
- **`draggable` is decided by KIND, not by `rowLink`**: the first paint can
  land before `state.repo`, and a merge row that stayed undraggable until the
  next refresh would be a heisenbug. `dragstart` cancels a linkless row.
- Merge rows now carry `data-kind="preview"`; `findRow(id, kind)` picks the
  list (`pair`/`compare` → `state.savedCompares`). Handlers test the kind by
  VALUE — truthiness used to mean "a saved row".
- The drag state is `{id, kind}` in `state.dragPreview`, and both ends are
  looked up at DROP time: `renderPreviews` replaces `innerHTML` on every live
  refresh, so an element held across a drag is stale.
- `dragover`'s `preventDefault` IS the drop permission; returning before it
  gives the browser's no-drop cursor for free (self, comparison rows).
- `.drop-target` is styled PER LIST (`#branches-list li…, #previews-list li…`).
- The client never screens what the two links are: the server refuses, the op
  line says it. "compare and save…" opens the saved row BY ID (also when the
  pair of links was already saved).
- Probe note: a synthetic `drop` fires whether or not `dragover` was cancelled,
  so the permission needs its own assertion (`!dispatchEvent(dragover)`).

### Symmetric comparison view in `gg web` (2026-09-21)

Spec `docs/superpowers/specs/2026-09-21-web-symmetric-compare-design.md`,
research note and the approved static mock beside it (`mocks/`).

- **Domain:** `LinkComparison.SymmetricRows()` (`domain/symrows.go`) — the
  UNION of two bounded sets' members, sorted, each side `absent | present |
  deleted` (`MemberState`), `Differs` READ from `c.Files` (never re-derived, so
  the view cannot disagree with the listing). Membership comes from the path
  list, never `Has`: a nil `has` map answers true for any path. `ok == false`
  unless both sets are bounded. Pure — no git call.
- **Why it exists:** `compareOne` omits a path with bytes on neither side
  (deleted × absent, deleted × deleted) and an identical one. Those rows are
  exactly what the classic list cannot show.
- **Wire:** `sym` on `/api/compare-links`, only for two bounded sets and never
  on a pair landing (`a`+`b`); `files` stays byte-identical (the live refresh's
  `JSON.stringify` equality check reads it).
- **Web:** `static/symcompare.js`; `files.js` holds dispatch lines only.
  `symActive()` = offered ∧ `uistate.sym_compare` ∧ `innerWidth >= 1200` ∧ the
  file list not minimized. **`compareRows(c)` is the ONE writer of a
  comparison's `state.files`** (`applyCompareFilter`, `updateLinkCompareFiles`)
  — a live refresh once would have handed the painter plain rows. While active
  `state.files` holds the VISIBLE aligned rows with a direction-aware synthesized
  `status` (`=` when the row does not differ): one index space for the cursor,
  the stepper and both lists.
- The right list IS `#files-list` (its click/context-menu handlers keep
  working; a gap has no `data-i`, so they cannot act on it); `#symleft-pane`
  mirrors it. Row alignment = a measured spacer (`syncGeometry`, NOT rounded —
  the panes' borders put the list on a fractional pixel) + mirrored `scrollTop`.
- The grid (`#panes.detail.sym`) exists only in the diff stage and is painted
  by `paintGrid()` — called from `setLayout` AND at the top of `applySym`,
  because a toggle or a resize with a diff already open changes no layout.
  `esc` from the view leaves the comparison (it has no files-only stage).
- Chips use `data-sf` / `data-symtoggle`: `#compare-bar`'s own handler takes
  every `button[data-f]`. A zero-count filter is disabled (and its key inert).
- **An empty view must not `drillOut`.** esc leaves the comparison here, so
  `applyCompareFilter`'s empty branch returns `symEmpty()` first — without it a
  live refresh that empties the rows closes the screen. The OPEN path recovers
  by itself (`symReapply`), so only the refresh path proves this guard: the
  probe calls `updateLinkCompareFiles([], [])` directly.
- `applyFilesHidden` re-applies the view (`symReapply`): `symActive()` reads
  `!state.filesHidden`, and `.sym` would otherwise outlive it and beat
  `.nofiles` in the cascade.
- Dialog words: `/api/link-base` also answers `desc` (`DescribeLink`) and
  `origin` ("copied from a saved <hint kind>") for any PARSED link, base-able or
  not; `linkcompare.js setDesc` paints them under the field (a history pick
  paints the row's own label at once, the lookup confirms). `DescribeLink`'s
  "link:" fallback for an UNSAVED pair is pinned by three domain tests — left
  alone. Sides carry `kind` (`linkTargetKind`) → `compareKindLabel` → the
  `#files-kind` badge, which `enterFilesStage` DERIVES from the open comparison
  (never clears): esc from a diff re-enters that stage inside the same one.
  `runLinkCompare` owns the "comparing…" op line for every entry; the dialog
  adds `setBusy`. Probe: scratchpad `web/dlgprobe.mjs` (delays the route 900ms
  so the busy state is observable).
- Known limit: the live refresh's equality key is `files`, so a `sym`-only
  change (both sets gain an identical file) waits for a reopen.
- Probe: scratchpad `web/symprobe.mjs` + `run-symprobe.sh` (a FRESH state copy
  per run — the preference persists server-side and would flip the first
  click). It fails against a build with `sym` stripped.

### Pair notes in `gg web` (2026-09-20)

Spec `docs/superpowers/specs/2026-09-20-pair-notes-web-design.md` (N1–N10). No
domain change.

- **The scope rides the COMPARISON** (`state.compare.pair`), never
  `state.previewOpen` — that slot means "tips that can move" and is re-resolved
  by branch name on every refresh. `/api/compare-links` names `pair` for the
  `a + b` form and a saved pair's `id` ONLY; two typed links never arm.
- `pairCtx()` is the one predicate (compare mode, a pair, layout not `list` —
  `drillOut` leaves `state.compare` standing). Its diff context is
  `preview: {pair: {a, b}, source: "", target: ""}`: **every reader of
  `preview.source/target` must ask `.pair` first** (`noteQuery`, `fetchNotes`'
  URL, `linkFor`) — the JS twin of Go's `IsPair()`.
- `openEntryFileDiff` takes an optional `ctx`; with one it fetches notes in the
  same `Promise.all` as the diff. A row carrying `right_spec` gets none.
- Counts share `state.previewCounts`, so `openLinkCompare` nulls it; a `notes`
  event reaches the badges through `refreshNoteCounts` → `loadPairCounts`.
- `GET /api/pair/notes` is the twin of `/api/preview/notes` (and of
  `/api/pr/notes`): full ids instead of allowlisted names.

### Links in `gg web` — the dialog, the view, the Previews tab (plan 3c, 2026-09-20)

Spec `docs/superpowers/specs/2026-09-20-links-web-design.md` (W1–W10), plan
`docs/superpowers/plans/2026-09-20-links-web.md`. Layout and transport only:
no new domain function.

- **No JS link parser, so no JS rewrite (W1).** `static/links.js` PRODUCES
  links; nothing in `static/` parses one. `GET /api/link-base?link=` answers
  `SuggestBase`'s LOCATED kind + suggestion (`{kind: none|ref|commit, base,
  why}`); with `&base=` it answers `{link}` from `model.Link.WithBase(base,
  sug.Self)`. `Self` never crosses the wire. A link still being typed (does not
  parse, sha does not resolve yet) is `{"kind":"none"}` 200 — no row — while a
  REWRITE asked for by name is 422. This replaced the 3b spec's "Go↔JS gate for
  `BoundKind`": there is no JS twin to drift.
- **`GET /api/compare-links` is the web's door** (`linkcompare.go`). Exactly ONE
  input form: `left`+`right` (texts) · `id` (a saved comparison's texts, or a
  saved pair → `pairSides`) · `a`+`b` (two FULL ids → `pairSides`). `pairSides`
  = the point `a` against `@a..b`, built as `model.Link`s. Errors:
  `*LinkSideError` → `{error, side}` with the CAUSE's message; `model.ErrLink`
  400, any other side error 422, a bare error 500. Link text is NOT run through
  `isGitArgSafe` (it holds `://`, `@`, `?`); the door parses it. The web passes
  `ResolveOpts{Cwd, RegistryPath: s.reposStatePath()}` (`linkOpts`), so — unlike
  MCP — the cross-repository refusal is reachable and tested.
- **Per-member byte sources ride the wire per ROW.** A side's `spec` is
  `linkSideSpec(FileSet.Endpoint())`; a row carries `left_spec`/`right_spec`
  only where `Source(path)` differs (a `-u` stash's untracked file → the third
  parent). `linkSideSpec` is per KIND with no answering default; a pair spells
  as `commit:<b>` (`Endpoint.FileRef`'s rule). The vocabulary is
  `/api/entry-diff`'s, so there is no new diff lane.
- **`/api/entry-diff` takes `old_path`.** It read both sides at the new path,
  so an `R` row's left side was absent. `leftPathOf(old, path)` is NOT
  `oldPathFor` — that one returns "" for a non-rename. The cache key gains
  `<old` only when the paths differ, so it still shares entries with
  `handleRevDiff`.
- **Client:** `files.js openLinkCompare(body)` sets `state.compare.links`
  (+ `aSpec/bSpec`, empty hashes, `previewOpen = null`); `openFile`'s `links` arm
  runs BEFORE the hash lane (a source gate pins the order) and passes
  `f.left_spec || aSpec`. With no single revision, the file menu drops its rev
  rows. `linkcompare.js runLinkCompare(query, onErr)` is the one client path
  into the route — dialog, saved row, pair landing. `getJSON` now keeps
  `err.data` (the `side`). `openPrompt({allowEmpty})` lets an empty label reach
  the store, which owns the default.
- **Dialog keys:** the overlay returns true for every key but
  `preventDefault`s only the ones it acts on, so typing reaches the field. With
  a history list open, esc closes the LIST. `enter` on a base input rewrites and
  never compares. `lookupBase` never assigns a link field (a source gate reads
  its body); the stale-answer guard compares the asked text with the field's.
- **Previews tab:** `/api/preview` is untouched (the PR lane and
  `reopenPreviewIfMoved` hang off `state.previews`). `GET /api/saved-compares`
  serves pairs then comparisons; `fetchPreviews` awaits it, so no caller
  changed. Rows carry `data-kind`; `rowEntry` picks the list by it.
  `savedEntry(id)` is the ONE classifier behind open / rename / remove: it asks
  domain (`PairGet`, `SavedCompare.IsSet`), requires the id to match EXACTLY
  (domain's getters also accept a label), and reports a merge preview's id as
  not found. A comparison row has no live summary (unbounded work per refresh);
  a pair's is domain's cached count.
- **Parked:** review notes on a web pair row (`armPreview` is keyed on branch
  names); a live re-open of a link comparison when a ref under it moves.
- **Probes** (scratchpad, not committed) assert computed `display`, and were
  each run against a deliberately broken build first: no `#id.hidden` rules, a
  pick that submits, a lookup that auto-applies, the old landing lane.

### Links in the TUI — the compare dialog and the one door (plan 3b-2, 2026-09-20)

**`domain.CompareLinks(ctx, leftText, rightText, opts)` is the one door** from
two link TEXTS to a comparison, and `EvalLinkText` is its one-sided half (the
CLI's `gg compare HEAD <link>`). Order inside is a correctness rule: parse →
**locate** → same-checkout → `EvalLink`. A parsed LOCAL-form link
(`gg:///abs/dir/f.go@…`) holds checkout and file undivided in `Repo.Abs` with
`Path == ""`; skip the locate and a file link silently evaluates as the whole
tree (MCP shipped exactly that). `internal/archtest`
(`TestFrontendsReachLinkSetsThroughTheDoor`) forbids any `.EvalLink(` call in
`tui`/`cli`/`mcp`/`web`. Failures carry their side — `*LinkSideError{Side, Err}`,
which **unwraps**, so exit codes and `errors.Is` key on the cause. Another
checkout is `ErrLinkCrossRepo`, its own sentinel (wrapping `model.ErrLink` would
call a well-formed link "bad"); the CLI exits 2 on either. **The cross-repo
refusal needs a registry to be reachable**: with none, a foreign link has no
candidate and fails first as `ErrLinkUnknownRepo`. MCP passes no registry by
design, so it can only ever report "unknown here" — do not "fix" that.

**The set-shaped view** (`link_compare.go`). `startLinkCompare` runs the door
off-thread and `openLinkCompare` opens the files view with the list already in
hand; `Model.filesSets` holds the two sets. The view's identity is
`linkCompareTag` = the two TEXTS — `gg://r@sha` and `gg://r/f@sha` share an
endpoint, so an endpoint tag calls the second "already showing".
`compareSides(row)` is where a row's bytes come from: `filesLeft/filesRight`
for an endpoint compare, `FileSet.Source(path)` per side for a link compare;
the diff tag AND the Differ cache key are built from what it returns, so a
third-parent read is never served under `b`'s key. `closeFilesView` clears
`filesSets` and `linkCompareWant` (the in-flight tag — a stale or cancelled
load is dropped; a failed one clears it so the same pair can be retried). A
load that lands while a popup is on top hands off (`handOffToFilesView`), so
the popup is parked and returns when the view closes. `gg session state` still
reports such a view as its two ENDPOINTS — it is a report, nothing restores
from it.

**Pair landing goes through it.** `steerNavigatePair` (the funnel for `#`,
`gg open`, `gg session navigate`) builds `pointLinkFor(a)` ↔ `pairLinkFor(a, b)`
and parks `pendingSteer` on the text tag whether or not a file was named: the
view does not exist until the comparison lands, so the reply and the
`▸ opened a..b` notice are raised in `drainPendingCompare`, not at dispatch.
The view's endpoints are still commit a / commit b (a pair set's own endpoint
is its `b`), never a `PairEndpoint`. No link form for the checkout → the old
endpoint path.

**The dialog** (`link_compare_popup.go`, `link_compare_base.go`). Two
`linkCompareSide`s, each a field + a `linkHistPicker` + its own error; a
`linkHistHost` now returns ALL its pickers and one load fills each. A pick
FILLS its field — only `#` submits on pick. `ctrl+enter` is deliberately not
bound (terminals do not deliver it). The dialog uses the WIDE popup width and
shows a history list only once `↓` opened it. **Base rows:**
`model.Link.BoundKind()` is pure and is only the cheap gate — `None` is
certain, but it cannot tell a local-form FILE link from a whole tree, so the
row exists on `domain.SuggestBase`'s located `Kind`, never on the pure one. A
suggestion answers for the exact text it was asked about (`sugFor`/`asked`), so
an edit or a swap in flight drops it. `enter` on the row is the ONLY rewrite:
`model.Link.WithBase(base, self)` → `@B...A` (target first) or
`@<parent>..<self>`, where `self` is `BaseSuggestion.Self`, the FULL sha — the
field may hold an abbreviation a user typed. `tab` completes a *typed* ref base
(`branchSuggestions`, shared with the preview form); an untouched suggestion is
never completed away. Trunk = `refs/remotes/origin/HEAD`
(`git.RemoteDefaultBranch`) → local `main` → local `master`, skipping the ref
itself; gg has no trunk setting and this order is the whole notion.

**Saved comparisons in the Previews tab.** `previewRow` has THREE kinds:
`rowMerge`, `rowPair` (a commit pair — one SET entry holding `@A..B`) and
`rowCompare` (a two-link entry). "Pair" is `rowPair`'s word; never call a
comparison one. `readPreviews` takes exactly the non-set `SavedCompareList`
entries (the set ones are already listed through `PreviewList`/`PairList`).
Every site that branches on `merge()` and used to assume a pair otherwise must
check `compare()` FIRST — a comparison row's `pair` is zero
(`model.go` enter, `previewSwapCmd`, `contextLinkText`). A comparison has no
single Copy link; `comparisonLinkRows` offers each half. The save label starts
EMPTY: the store fills its own default, a rule `tui` cannot import.

**bookmark↔shelf.** `pendingCompare.link` / `compareLink` carry the first
pick's link into the other switcher; a non-empty link is what routes the second
pick through `startCrossCompare`. commit↔commit and commit↔file go through the
door; **file↔file keeps the two-ref diff** (paths may differ; on links that is
one `D` and one `A`). Commit entries are pre-checked with
`ResolveCommitEntryEndpoint` so a dead bookmark still gets `entryGoneText`'s
sticky notice. A focused file from a `.` menu has no link and keeps the
refusal. The same-kind MARK flow (`m`, `m`) is untouched.

### Links in the TUI — recording, stash pairs, the `#` history (plan 3b-1, 2026-09-19)

**Recording lives at the clipboard writer.** `internal/tui` has exactly one
reference to `clipboard.Copy` — `New` hands it to `Model.clipWrite` — and
`copyToClipboardCmd` is the only caller of that field
(`TestTheTUIHasOneClipboardWriter`, counted over the token stream so comments
may name it). Text that is a link is recorded there through
`domain.RecordCopiedLink`, BEFORE the write and whatever its outcome. A new
copy row therefore records for free; do not add a per-row record call, and do
not add a second writer. A `Model` literal (no `New`) has a nil `clipWrite`
and reports `errNoClipboardWriter` rather than touching the real clipboard.

**`domain.DescribeLink`** is the one describer (`LinkDesc` + the field
derivation that used to be `cli.linkRecordFields`). Its JS twin in
`links.js` is pinned by `TestLinkDescJSMatchesGo`, now in `internal/domain`.
Priority: hint → **stash-looking pair** → **path** (at any point, in any pair
or preview — the point is already in the link text) → preview → ref → commit →
`link: <text>`. The local-form split runs first, or a path is never seen.

**Two stash rules, two functions — never merge them.**
`stashShape(a, b)` is the SET rule and is structural only: `b` has exactly
three parents, `a` is the first, the third is a ROOT. It decides whether
`EvalEndpoint`'s pair arm adds the third parent's files (spec §3.4); the root
test is what keeps an octopus merge from pouring a whole tree into the set.
`looksLikeAStash(a, b)` is the DESCRIPTION rule: first-parent match, two or
three parents, subject starts `WIP on ` / `On `. It only words a history row.

**`FileSet.Source(path)`** is where a member's bytes live — the set's endpoint
unless overridden (`FileSet.src`, today only a `-u` stash's untracked files).
Read member bytes through it, never through `Endpoint()`. `narrowTo` rebuilds
a set from its endpoint and must carry the override across
(`TestNarrowedStashLinkKeepsTheThirdParentSource`). A comparison only READS a
member that exists on both sides — an `A`/`D` row is decided without bytes —
so a test of `Source` needs the path on both sides with different content.

**The stash copy row is the one asynchronous copy row**: `stash@{N}` is
positional, so the row resolves it (`domain.StashPair` → first parent + sha,
both full) and copies on the reply (`stashLinkMsg`). Its `copyText` is empty
by construction. The ref never reaches the link.

**`linkHistPicker`** (`linkhist_picker.go`) is one component with several
hosts (`linkHistHost`): the `#` prompt now, the compare dialog's two fields in
3b-2. The host owns the field, the picker the list; loads are gen-tagged
(`Model.linkHistGen`) and never happen in `pushLayer`. `esc` in the list
returns to the field and must not close the host. Rows are TUI-local
`histRow`s so the package never imports `internal/linkhist`.

**Known window until 3b-2:** `gg compare <stash link>` lists a `-u` stash's
untracked file; landing the same link through `#` still opens
`openCompareFiles(commit a, commit b)` → `CompareFiles`, the tracked-only
diff. 3b-2 routes pair landing through the set-shaped view.

### Preview links (feature B, 2026-09-15)

`model.LinkTarget.Preview *LinkPreview{Source, Target}` is set iff the target
text carries git's three-dot pair; `State` stays `StateCommitted` with an EMPTY
`Commit`, because which commit a preview addresses is a per-machine question.
`Link.Address()` is therefore meaningless for a preview link — the resolver
(`finishLink`, the ONLY `.Address()` caller on a `model.Link` in the tree)
branches first, calls `PreviewNotes` on a checkout that holds BOTH branches, and
fills `Resolved.Preview` plus an `Addr` on the source tip. `internal/cli`'s
`previewTargetFromLink` is the one adapter onto the `--preview` code path, so a
link and the flag cannot diverge; `linkDiffSpec` returns the preview's own patch
so hunk numbers agree. The steer wire's `target.state = "preview"` carries
`source`/`target` and an EMPTY `commit`: the consumer resolves the tip itself,
so a tip that moves between post and apply is honoured. The TUI parks a
`steerStagePreview` pending drained by the `compareFilesMsg` handler
(`openCompareFiles` does set `m.filesHash` to one of the two endpoints, the
same field a plain single-commit view uses — but that hash cannot tell a
preview's file list apart from an unrelated commit view that happens to share
it, so the open `(source, target)` PAIR is what actually identifies a preview,
and that is the gate). `gg open` reaches the TUI through the
`cli.LaunchTUI` seam that `cmd/gg`'s `launchTUI` installs, keeping
`internal/cli` free of an `internal/tui` import. The link→navigate builder
itself (`Command`, `HunkLine`, `AtLink`, `RepoOnly`, `Opts`/`Resolve`) lives in
`internal/linknav`, shared by the CLI wrappers and the TUI's `#` prompt
(`goto_link.go`): a pasted link is resolved off-thread, a same-checkout place
goes through `applySteer` id-less (refusals → status bar, notice "opened"),
another checkout turns the prompt into a confirm whose enter calls `reRoot`
and arms `startAt`/`startAtPending` (resetting `startAtPreviewsSeen`) so the
landing rides the `--at` gate; a bare repository link only switches (or
notices "this checkout"), and `gg open <bare link>` launches the TUI there.
`gg open --web` is the browser arm: a live page is steered over HTTP alone
(never the TUI inbox, so a TUI live beside it stays put); a live TUI with NO
page is asked to serve its own (`askTUIToServe`: the `serve` inbox command,
`Reply.Detail` = the URL — see "The TUI serves its own web page"), else the
`cli.LaunchWeb` seam (`cmd/gg`'s `runWeb`, shared with the `web` subcommand)
starts `gg web` in the link's checkout with `web.Serve`'s `startAt` — validated
through `toSteerWire` before the port is bound and handed to the page ONCE by
`GET /api/session/start-at`. The page's gate is `boot()`'s
`Promise.allSettled(firstLoad)` after `connectLive()`: every first-load fetch
(status, branches, previews, note counts) has settled before `applyStartAt`
runs, the web twin of `startAtReady` (a preview landing must find its saved
row, or `openPreviewForPair` opens a show-once twin). A stale presence
(server gone, web.json inside the liveness window) is a `*url.Error` from the
POST and falls back to the launch; a page that ANSWERS (400/409) is alive and
exits 1 instead. `openBrowser` honours `$BROWSER` first (a command plus
arguments, URL appended; `BROWSER=true` for headless checks). `gg open --web`
runs the server in the foreground until it is stopped.


### In-view text search (phase 6, spec §4.3)

`internal/tui/textsearch.go` is the whole leaf: `textSearch` (query, typing,
direction, `hits`, `cur`, `origin`), a rune-walking case-insensitive matcher,
at/after (`nearestHit`) and strictly-after (`stepHit`) stepping with wrap, the
`badge()`, the minimal `panFor`, and the two key handlers every host routes
through — `searchTypingKey` (history recall FIRST, then esc/enter/backspace/
space/runes; everything else swallowed) and `searchCommandKey` (`/ @ ] [`, and
`esc` only while a query is live).

**One index space: display runes.** Every host searches the DISPLAY string it
paints (`sanitizeLine`/`sanitizeCell`'s disp, `contentLine.text`,
`sanLine.text`), so a hit's `[start, end)` are already the painter's offsets —
no raw→display mapping, and `hitCols` converts to display COLUMNS (wide glyphs)
only for the pan.

**Painting is an overlay, never a mutation.** The per-display-rune `emph` mask
widened from `[]bool` to `[]emphLevel` (`none | word | hit | current`);
`overlayHits(emph, off, n, hits)` returns its input untouched when nothing
intersects (so the no-search render allocates nothing and is byte-identical)
and otherwise paints a COPY — `textdiff.Row` spans and the picker's `sanLine`
masks are shared cache values that the output pane and other views read.
`cellSeg.off` records where a wrapped segment starts, so wrap mode overlays at
render instead of re-laying-out on every keystroke.

**Reverse video keeps emphasis.** `window.go` and `twocol.go` drop only the
CLASS mask on a reversed row/cell (reverse would turn per-token foregrounds
into per-token backgrounds); bold reads either way, and the current hit is by
definition on the cursor row.

**The CURRENT hit is defined relative to its row** (`styles.currentHitStyle`,
the one style every host paints through in `styledRuns`). It first shipped as
`diffEmph.Underline(true)`, an absolute brighter foreground, and the user
reported it drowned in syntax colour ("it is hard to notice the found line, as
the view is coloured"). Now: with the theme role `search_current_bg` set (dark
`#6B5F11`, light `#FFE680`) the hit is an explicit BACKGROUND patch that also
clears reverse, so it overrides the diff's cursor-row band and the reversed
cursor rows of blame/picker alike; with the role unset — the Terminal theme,
and `legacy`, which never carries it — the hit FLIPS reverse video against its
base: inverted over an ordinary row, un-inverted (a hole) over a reversed one.
Bold stays, underline is gone. The flip works only because every host composes
a row PER SEGMENT (`colouredLine`/`renderPiece` render the row style around
each run, never once around the whole line): a segment rendered with
`Reverse(false)` emits no `7`, which on a clean, just-reset terminal state is
exactly a hole.

**Both cells of an unchanged row paint.** `diffPaneLines` mirrors
`lh = rh` when the row is `Same` and the left side has no hits of its own: the
sides hold identical text, so the display offsets (and the `cur` flag) carry
over unchanged. Searching stays one-sided — this is a PAINT-time mirror, so
`]`/`[` still stop once per unchanged line (the user's report was "I see
highlighting only on the right").

**Stepping is relative to the cursor, not to `cur`.** `]` is the first hit
strictly after the cursor position, `[` the last strictly before; both wrap.
Direction (`/` vs `@`) only picks which hit the incremental search snaps to and
which glyph the badge leads with. Each host's `searchPos()` returns the current
hit while the cursor still sits on it and the head of the cursor line
(`col: -1`) otherwise, which is what makes a `j`-then-`]` sequence find the hit
on the line the user just walked to.

**Per-host addressing.** Diff: `row` = index into `v.lines` (folded rows are
not searched — it is an IN-VIEW search), `side` 0/1, and a row whose sides are
identical is searched on the right only so `]` never stops twice on one piece
of text (both of its cells still PAINT the hit — see above). Blame and preview: one column, `side` 0, the gutter is the window's
frozen prefix and so is never searchable. Picker: a FLAT candidate row (per
block, every `Current` line then every `Incoming` line) plus `searchBase` to map
a 2D cursor forward and `searchRows` to map a hit back; literals and the output
pane are outside that space. Search state is per VIEW: stepping to another file
replaces the `diffView`, so the query does not follow.

**Line selection + copy (phase 5, spec §4.7).** One pure type in
`internal/tui/lineselect.go` — `lineSel{on, anchor, end, fixed}` with
`start`/`mark`/`press`/`clear`/`bounds`/`contains` — serves the diff view,
blame and the View-file preview. `press` is the whole space key: start a range
at the cursor, freeze its end, start a new one. `bounds(cur)` follows the LIVE
cursor while the range is loose and the frozen `end` once it is fixed, which is
why moving the cursor extends a one-space selection and leaves a two-space one
alone — no host does any bookkeeping for that.

Each host embeds it as `lsel` over its own LOGICAL line indexes (`v.lines`,
`b.lines`, `p.lines` — never a display row) and owns one key hook,
`<host>SelectKey`, placed BEFORE its search hook and declining while
`search.typing`. That placement is the only way to get the spec's esc order —
search typing → selection → committed query → close — because each host's
search hook handles the typing case and the committed-query case in the same
call.

Copy goes through ACTION ROWS, never a bare clipboard call: each host has one
`…CopyLineRows` function returning `Copy line` (`copy-line`) and
`Copy selected lines (N)` (`copy-selected-lines`) built with `m.copyRow`, the
`.` menu splices them ahead of the file path/name/commit rows, and the `enter`
key looks the second row up with `rowByID` and runs it. So the key and the menu
can never copy different text, and a test reads the exact payload off
`row.copyText` — the same seam `contextLinkRow` established for `L`.

What is copied is SOURCE text: `textdiff.Row.Left/Right` in the diff,
`model.BlameLine.Content` in blame, and — new — `contentLine.raw` in the
preview, which `fileContentLinesTok` fills from the normalized-but-unsanitized
line so a tab stays a tab. `contentLine.src` distinguishes a genuinely empty
source line (`src` true, `raw` "" — copyable, it is a line) from a placeholder
such as `(loading…)` (`src` false — not a line of the file, no Copy line row,
and `space` is inert on it). A diff range skips a fold entry and a cell the
cursor side has not got, which is the user's "a range across a collapsed fold
copies only the visible lines" ruling.

Painting is a base-style SWAP, not a new emphasis level: `styles.selectionStyle`
follows `currentHitStyle`'s two-mode contract (the `selection_bg` role as a
background patch that clears reverse, or a flip of reverse video when unset)
minus the bold — a stripe marks extent, not one hit. The three hosts reach it
differently, because of what their rows are made of: the diff carries a new
`cellMark.sel` bit whose `bodyFor` wraps each cell's resolved base (only the
cursor side's mark ever sets it, and never on a gap); blame needs a BODY-ONLY
style, because its commit gutter is `winRow.prefix` and `winRow.style` covers
the prefix too, so `winRow.body *lipgloss.Style` styles the text and the padding
while the prefix keeps `style` — a pointer, since `lipgloss.Style` holds
interface fields and has no safe zero comparison, and a reversed body drops the
class mask exactly as a reversed style does; the preview's rows carry no prefix
at all, so its `winRow.style` IS the body style and nothing new was needed.
The rule any new painter must keep: **a REVERSED base drops the syntax class
mask** — in `renderWindow` for blame and the preview, and in `styledRuns` for
the diff cells — because a reversed base with per-token FOREGROUNDS paints
per-token BACKGROUNDS, giving every syntax token in a selected line its own
coloured block.

The preview also gained the cursor it never had: `contentPopup.cur` (read only
by `renderFilePreview` and the preview key paths — the same struct backs the
help window and the files tree, which never look at it) moved by `alt+↑/↓`
through `movePreviewCursor` + `ensureCursorVisible`, painted with
`st().diffCursorRow` under `[ui] diff_cursor` (`number` falls back to the band:
there is no gutter to number). `searchPos()` now anchors on `cur` while it
sits inside the visible window `[sel, sel+rowsCap)`, and on the top visible
line otherwise — a cursor paged far off-screen must not search from there —
which retires the deferred "preview `searchPos` ignores `p.sel` after a free
scroll" item; `snapHit` lands `cur` on the hit so `]`/`[` and the next search
step measure from it; `previewOrigin` carries `cur` so esc restores it with
the pager.

Invalidation: the diff clears on `rebuild()` (the `f` toggle), on `ctrl+w`
(which relayouts and re-anchors rather than rebuilding) and on a `diffMsg`
arrival — but NOT on a resize, which changes no line index; blame on `blameMsg`;
the preview on `fileContentMsg` and with the struct itself on close. Search keys
never clear it. The diff's `onOld`, by contrast, is CARRIED across `diffMsg`
(it is the user's choice, not the loader's) and is not inherited by a new file.

### Branch filters (`internal/branchfilter`, spec `docs/superpowers/specs/2026-09-16-branch-filters-design.md`)

Five numbered slots (`alt+1…5`) on the Branches and Remotes panels, one active
slot per list, shared by the TUI and `gg web`. `internal/branchfilter` is the
ONE evaluator (stdlib-only DAG leaf): `Slot` is the TOML shape, `Compile`
parses ages (`ParseAge`: `d`/`w`/`m`/`y`, calendar-free approximations) and the
regex, and never returns an error — a bad block travels inertly inside
`Compiled.Err` so every caller shows the SAME reason instead of a startup
failure. `Compiled.Empty` (no clause set) is a distinct, also-inert state
(`Usable() = Err == nil && !Empty`). `CompileAll([]Slot) ([5]Compiled,
warnings)` places blocks by `Slot.Slot-1`; a block whose slot is out of range,
or a second block for an already-filled slot, is **skipped with a warning**
(first wins) rather than either erroring or silently flipping the rule — Settings
renders `warnings`. `Apply(c, rows, exempt, now)` evaluates every row: a
`hide` mode hides a match, a `show` mode hides a NON-match; a row that would
be hidden but is in `exempt` becomes `Verdict.Exempt` instead of `Hidden`, so
callers dim-mark it rather than dropping it. Within a slot every set clause
(age + name) is **AND**ed together — there is no cross-slot combination, only
one slot is ever active per list.

**Config: `[[branches.filter]]` lives under a `[branches]` header, not a bare
`[branch_filters]` name.** `gg config populate` writes one plain `[section]`
header per settings group and then the array-of-table blocks under it; a
top-level table NAMED after the array (`[branch_filters]`) would collide with
the `[[branches.filter]]` blocks themselves in TOML's namespace. `[branches]`
sidesteps that: it is populate's documentation header, and `filter` is the
one field under it (`internal/config/branches.go`).

**Overlay is the second deliberate exception to field-level overlay** (the
first is `[[tools.command]]`): `overlayBranchFilters` replaces a global slot N
WHOLE with a repo slot N — a filter is one rule, not independently
inheritable fields. A slot the repo file omits falls through from global.
Duplicates within ONE file are left in place (`CompileAll` keeps the first),
so a hand-edit never silently flips a rule out from under the user.
`config.SetBranchFilter`/`RemoveBranchFilter` are scoped line-edit writers
(precedent: the theme-role and tools writers) that replace or delete exactly
the block whose `slot = N`, preserving everything else byte-for-byte;
`BranchFilterScopes(globalPath, repoPath, slot) (inGlobal, inRepo bool)`
decodes each file independently (no overlay) so Settings/the popup can show
provenance and target the right file on delete.

**Peel repo before global.** `d` (TUI) / "remove" (web) target the REPO
definition first when one exists, and only fall through to the global
definition when the repo has none — removing a repo override never deletes
the global rule as a side effect, and the second press (now showing
`scope: "global"`) removes that. Both surfaces compute this from
`BranchFilterScopes`, never by guessing from `Compiled` alone.

**Edit-form scope defaults to repo only when the slot is ALREADY defined in
the repo file** (`inRepo` from `BranchFilterScopes`), else global — both the
TUI popup and the web settings form. The opposite default (repo whenever a
repo config path exists) was tried and reverted: it would silently write a
brand-new rule into a possibly-committed `.gg.toml`, and it made a global
copy of an already-repo-overridden rule invisible to `d`/remove while still
activating it in every OTHER repo. An unset slot's form opens on **global**.

**Active-slot memo (TUI only): key = (slot, list length, pointer identity of
row 0), not content.** `branchFilterMemo{slot, n, head, hidden, exempt,
count}` — every refresh path assigns branches/remotes a NEW backing slice
(never mutates a row in place), so the `head` pointer changes whenever the
data actually could have; the memo short-circuits `displayIndices`, which
fires many times per keystroke. `bfMemo.invalidate()` (a shared pointer,
survives the value-receiver `Model` copy) is called at all 7 sites that
assign `m.branches`/`m.remoteBranches`/`m.worktrees` — the key alone would
miss worktree-only or upstream-only changes, since exemptions also read the
worktree list and HEAD's upstream, neither of which moves the filtered
list's own pointer — and once more from `applyBranchFilterConfig`, because
the key carries the slot NUMBER, never the RULE: an edited rule saved under
the same slot number would otherwise keep serving stale verdicts.

**The repo key is the git common dir the health probe resolved — ONLY that,
never the worktree-path fallback `toolRepoKey` uses.** `bfRepoKey()` returns
`""` until `m.repoHealthKnown`, gating on the flag rather than trusting
`m.repoHealth` directly: `reRoot` clears `repoHealthKnown` but NOT the
`repoHealth` struct itself, so between a repo switch and the new probe's
answer the struct still holds the OLD repo's common dir. Without the gate, the
switch's own config/data-loaded path would load repo A's remembered slots
into repo B's model, latch the load, and let an `alt+N` pressed in that
window persist into A's promptstate record. The web keys the identical
`prompts.toml` record by `svc.GitCommonDir(ctx)` — the two frontends would
split their memory if either used a different key.

**Two-flag load latch, because reRoot's own two async replies race.**
`reRoot` batches the health probe and the full data load together, and the
probe (one cheap read) wins essentially every time. `loadBranchFilterSlots`
requires BOTH `bfCfgApplied` (set only in `applyBranchFilterConfig`, i.e. this
repo's OWN rules are compiled) and a non-empty `bfRepoKey()`, then latches
`bfSlotsLoaded` so a later arrival cannot clobber a since-made `alt+N`. A
single flag was tried first and failed: the probe would land first, key in
hand but still holding the OLD repo's `branchFilters`, validate the new
repo's remembered slot against the WRONG rules (dropping a usable slot to 0,
or keeping a slot number whose rule happened to be inert under the old
config), and latch — turning the config's own, correct load into a no-op.
Whichever of the two lands second now completes the load; both flags are
reset in `reRoot`. Startup is unaffected: `run.go` compiles the config
synchronously before `Init` dispatches the first probe.

**Remotes: sort → filter → cap, in that order — a web-only concern**
(`internal/web/remotes.go`, a comment records the reason; the TUI's Remotes
panel has no row cap, so it only needs sort → filter). Filtering after the
cap would mean "the 100 most recent remote branches" silently shrank to fewer
visible rows whenever a filter hid some of them; filtering before it keeps
the section's meaning intact and lets `filter.hidden` count the truly-hidden
rows rather than the ones the cap would have dropped anyway. Verdicts are
computed from the SORTED slice so index alignment holds through the later cap.

**A missing exemption input turns the filter OFF for that request, never
"apply to everything".** On `/api/remotes` the branch list IS the exemption
input (HEAD's upstream), so `remoteVerdicts(active, rbs, bs, berr)` returns a
nil rule when `svc.Branches` failed: the payload carries `filter: null` and
every row shows, rather than a transient read failure hiding the very row
`f`/find and pull land on. `/api/branches` degrades the same direction (a
failed worktree read only loses the OTHER checkouts; HEAD stays exempt
either way). Likewise, an unresolved repo key (`svc.GitCommonDir` failed and
the process cache is cold) makes `activeBranchFilter` return nil, and the PUT
that would have stored a slot under it answers **503 "repo not resolved — not
remembered"** instead of writing a `[branch_filter.""]` promptstate record no
reader ever looks up. Two parsing counterparts, both in the same spirit:
`ParseAge` REFUSES a value past `time.Duration`'s ~292-year ceiling (`293y`,
`3600m`) rather than letting the multiply wrap negative — a wrapped age reads
as an unset clause, and a hide-mode slot with no name clause would then hide
every non-exempt row — and `branchFilterBlocks` strips a trailing `#` comment
off the `slot =` value before `Atoi`, because the spec's own example comments
that line and a slot parsed as 0 makes `RemoveBranchFilter` a silent no-op
and `SetBranchFilter` append a duplicate.

**Web keys on `e.code`, never `e.key`.** `branchFilterKey(e)` in
`branchfilter.js` matches `e.code === "Digit1".."Digit5"` with `altKey` (and
`shiftKey` for Remotes) — with Alt held, `e.key` is a dead or accented
character on several keyboard layouts, while `e.code` names the physical key
regardless of layout or modifier. **`notifications.js` collision guard:** its
own, separate keydown listener opened the notification centre on a bare `!`
with no modifier check, and on a US layout `alt+shift+1` PRODUCES `!` — so
the chip's own alt+shift+1 for Remotes was reopening the notice popup instead
(or as well). Consuming the key in `keys.js` cannot stop a second listener on
the same document target, so the guard (`|| e.altKey || e.ctrlKey ||
e.metaKey`) had to go into `notifications.js` itself, not the filter code.
**`showCtxMenu` has no `disabled` row kind**, so an unusable slot (inert or
empty) in the chip menu renders as a non-clickable `{header: "4  (invalid —
…)"}` row instead — visible so the five numbered slots always read as five,
inert so it cannot be activated. If a later feature wants a true
greyed-out row, `showCtxMenu` needs a real disabled kind added (a shared
`layers.js` change, not something this feature could do locally).

**Not `ctrl+1…5`.** Terminals typically deliver `ctrl+3` as a bare ESC (it is
the same control code), and browsers reserve `ctrl+digit` for tab switching
on every major platform; `alt+digit` is the only chord free on both surfaces
(a Linux Firefox exception: it binds `alt+1…8` to tab switching too, and
content-side `preventDefault()` does not reliably suppress it — the chip
menu is the fallback there, and the help text names both paths).

**Exemptions** (`domain.ExemptBranches`/`ExemptRemoteBranches`): HEAD, any
branch checked out in ANY worktree, and — on the Remotes list only — the
current branch's own upstream. These never actually disappear from a filtered
list; a rule that would have hidden them instead marks `Verdict.Exempt`, and
both frontends render a dim `∗` (with an explanatory hover/title) rather than
dropping the row — switching away from an exempt row would otherwise strand
the user on state gg itself just hid.

**Rulings carried from the spec (do not re-litigate):** keys are alt+1…5;
radio per list (the active slot's own key clears it); scope covers local AND
remote branches, with the active slot independent per list; slots are defined
in TOML plus an editor UI (TUI Settings popup, web Settings view); the active
slot is persisted per repo in `prompts.toml`, shared by TUI and web; there is
exactly one evaluator in Go and the web receives verdicts, never re-deriving
them client-side; HEAD/worktree-checked-out/upstream rows are always shown,
merely marked; within a slot, set clauses AND together and `mode` alone picks
hide vs. show-only.

**Known pre-existing bug (found by Task 5, not introduced by this feature):**
a long SELECTED branch row overflows the panel's right border — reproduced
with NO filter active and NO `∗` marker present (`main
(/tmp/…/bf-scratch)` bleeding past `│`), so the selected-row render path
skips whatever elision unselected rows get. The `∗` marker widens such a row
by two columns, which makes an already-overflowing row overflow further, but
does not itself cause the overflow.

### Blame age highlight (`internal/timespan`, spec `docs/superpowers/specs/2026-09-17-blame-recent-highlight-design.md`)

- **What:** in the blame view `d` opens a one-field age-filter popup; enter
  turns the highlight ON with that filter, `D` turns it off (no dialog), `d`
  again edits. The highlight is OFF whenever blame opens (revision 2): `on`
  and the `timespan.Filter` live on the `blameView`, the Model keeps only
  `blameRecentLast` (the text that prefills the dialog; `""` → `7d`; the
  first typed rune replaces the prefill, like the web prompt's select).
  Web: `state.blameRecent{on, f, last}`, `openFileBlame` resets `on`.
- **Grammar** (`timespan.ParseFilter`): up to two signed halves. `-<span>` =
  younger than (age ≤ span), `+<span>` = older than (age ≥ span), both
  INCLUSIVE; a leading unsigned span is the `-` half; each sign owns every
  token up to the next sign, whitespace after a sign ok; both halves = a
  range (`+1d 2h -7d 4h`, order-free). A span is tokens `<digits><unit>`
  (either part optional, not both): bare number = DAYS, bare unit = one of it
  (`+w`), `m` = MINUTES (branchfilter's `m` is months — separate grammar).
  Errors: a second `-`/`+`, a sign with nothing after it, older > younger
  (`+7d -1d`), empty, zero total, unknown unit, junk after a unit (`1mo`,
  `1.5d`). `+7d -7d` is legal. `Filter.String()` = canonical echo, older
  first: `-7d`, `+30d`, `+1d2h -7d4h`. The web port (filehist.js
  parseFilter/formatFilter/filterMatches) is pinned to the exported
  `timespan.Table` by `internal/web/timespanjs_test.go` — extend the table,
  never one side.
- **Recency:** age = `now − authorTime` at render (wall clock, NOT the blamed
  rev's date — blaming an old commit may highlight nothing); uncommitted
  lines (hash `""`) are age 0 → match a `-` half, never a `+` half.
- **Painting (TUI):** `st().blameRecentStyle(base)` = the row style for a
  matching row — a background over gutter AND code from theme role
  `blame_recent_bg` (Dark `#1F3A26`, Light `#DFF5E3`); with the role empty
  (Terminal theme) the row goes BOLD instead (bold survives syntax colours; a
  guessed tint would clash with the host palette). Precedence: cursor row
  (`selectedRow` reverse video) wins whole-row, the selection stripe wins on
  the code half, then the tint, then plain. A background-only style keeps
  the `cls` token foregrounds (only a REVERSE style discards cls).
- **Advertising:** footer hint ends with `[d] age` (last, so a narrow
  terminal clips it rather than `[esc/b] back`); header badge = the filter
  text, right-aligned like the search badge (`+1d -7d · 3/12` when both).
  Web: `d`/`D` in the blame layer's `onKey` beside `w`; `.bline` rows stamped
  `data-t` / `data-u`; `applyBlameRecent()` toggles `.brecent` without a
  refetch (scroll mode's sticky `.bgut`/`.bno` carry the tint too); the
  title carries ` · +1d -7d`; `#blame-hint` and the `?` help name the keys.
- **Keys ruling:** `d`/`D`, not `h`/`H` — `h` means history in the diff view.

### Forge pull requests, read-only (`internal/forge`, plan 1 of 3, spec `docs/superpowers/specs/2026-09-19-forge-prs-design.md`)

**Shape.** `model.PullRequest` / `model.ForgeComment` are provider-neutral and
live in `model` because frontends must name them but may not import a
domain-owned package. `forge.Provider` (`Detect`, `ListOpen`, `PR`, `Comments`,
`BaseRepo`, `HeadRefspec`) is the seam; `forge.GH` is the only implementation
and runs `gh` through a `gitexec.ExecRunner` built with the gh binary path
(`$GG_GH_BIN`, else `gh`) — the runner was already binary-agnostic. Every gh
call has a 30 s timeout. `TestGHArgvIsReadOnly` pins that no mutating token
(`-X`, `comment`, `review`, `edit`, `merge`, …) ever appears in an argv.

**Detection is once per `domain.Service`** (user ruling): `ForgeStatus` probes
on its first call (`gh pr list --limit 1 --json number` = installed + authed +
repo resolves) and caches forever; a user who fixes gh restarts gg. It is a
network round trip, so frontends call it off the UI thread. `Preflight` NEVER
probes — it reads the `forgeProbe()` snapshot (nil = unprobed = unsatisfiable),
and `ForgeStatus` invalidates the preflight cache after its one probe. **Locks:**
the probe runs OUTSIDE `forgeMu` (it can take its full 30 s, and everything
that calls `Preflight` — notices, `FeatureEnabled`, the web gate — reads
`forgeProbe` under that lock); concurrent first callers wait on the
`forgeProbing` channel so `Detect` still runs once, and `forgeProbe` says
"unprobed" while it is in flight. Order is `preflightMu` → `forgeMu`, so
`ForgeStatus` calls `invalidatePreflight` with `forgeMu` released. The `forge`
feature is `Optional` + `Silent`: `noticesForVerdicts` skips silent verdicts, so
a box without gh shows no notice (the CLI, which was asked, prints the reason).

**Known PRs never disappear when they close** (user ruling). `PullRequests` =
`ListOpen` ∪ known-but-not-open, where known = listed open earlier this
session (`forgeSeen`) or has a `refs/gg/pr/<n>` ref. The REF is the
cross-session record — there is no state file, and refs are never pruned
automatically. A non-open PR is read once with `PR(n)` and cached
(`forgeTerminal`); `ErrNotFound` → state `unavailable` (cached), a transient
error → `unavailable` uncached. `PRForgetOp` drops the session entry and the op
deletes the ref. Open rows first, each group newest-updated first.

**The fetch.** `engine.FetchPRHead` (RefWrite) force-fetches
`refs/pull/<n>/head` (the provider's `HeadRefspec`, fully qualified — no DWIM)
into `refs/gg/pr/<n>` with `--no-tags --no-write-fetch-head`; when the ref
already equals the PR's `HeadSHA` it costs one rev-parse and no network.
`PRFetchOp` picks the remote: the configured remote whose URL `RepoSlug`s to
the base repo gh reports (ssh and https spellings compare equal), else gh's
`sshUrl`/`url`. GitHub serves fork PR heads from the BASE repo, so forks need
nothing special. `refs/gg/*` is already excluded from graph decorations.

**`PRPair`** picks the diff's left side: an open PR → its target branch; a
closed/merged one → the forge's recorded `baseRefOid` (after a merge the
target tip contains the head, so `target...head` is empty); each falls
through when it does not resolve locally, ending at `<remote>/<target>`.

**Comments** come from ONE `gh api graphql` call per PR using gh's own
`{owner}`/`{repo}` placeholders (GraphQL because REST has no `isResolved`),
single-page by design: 100 threads × 50 comments, 100 conversation comments,
100 reviews, `hasNextPage` anywhere → `Truncated`. `PRComments` buckets them:
`Inline` (line or file-level threads with a current position), `Outdated`
(GitHub nulls `line`; the parser keeps `originalLine` as a LABEL only — never
anchor an outdated thread), `Hub` (conversation + review verdicts,
chronological). A `COMMENTED` review with an empty body is dropped: it is only
the envelope of its inline comments. A deleted account's null author is
`ghost`. Buckets are `[]`, never `null`, on the wire.

**Testing.** `internal/forge/testdata/fakegh` is a Go-built fake `gh`
(`forgetest.BuildFakeGH`, built once per process, Windows-safe) answering from
`$GG_FAKEGH_DIR`, else `<cwd>/.git/fakegh/*.json` — which is how an e2e TOML
scenario seeds it with plain `write` steps. The e2e `TestMain` points
`GG_GH_BIN` at it for EVERY scenario, so the suite can never reach the real
gh. Tests that set that env are serial. Domain tests inject a `fakeForge` via
`svc.SetForgeProviders`.

### Forge pull requests in the TUI (plan 2 of 3, `docs/superpowers/plans/2026-09-19-forge-prs-2-tui.md`)

**The list is a synthetic refresh item, not a source.** `prsItem =
refreshItem{isPRs: true}` (like `fetchItem`/`remoteTagsItem`) with its own
`prsLoadedMsg`, read by `readPRsCmd` (`pr_panel.go`). It is a NETWORK read
through `gh` that may take its whole 30 s timeout, so it must never hold
`srcLoading`/`m.loading`, never block `r` behind `anySourceInflight`, and never
ride an "all sources" fan-out. `prsGen` drops a stale arrival, `prsInflight`
stops a second read, a `context.Canceled` arrival (a user op pre-empted the
background lane) is not a failure. `dataAvailableMsg`'s lane-freeing check
excludes every synthetic item — their zero `source` is `srcStatus`.

**The poll ignores `[refresh] enabled`.** `dueItems` skips `isPRs`; `prsDue`
(pure) checks `cfg.PRsSeconds()` with the `min_seconds` floor and no master
gate, and `refreshTick` appends `prsItem` only when `m.forgeShown`.
`RefreshConfig.PRs` is a `*int` (`toml:"prs"`): the overlay's zero-is-unset rule
could not otherwise express "0 = off" over a 300 default. It is a Settings →
Refresh rates row like the others (`scheduledItems`).

**One probe, a sticky tab.** `kickForgeProbe` (the `configReadyMsg` startup arm,
and `dataLoadedMsg` after a `reRoot`) dispatches the one read per repo session;
its `domain.ForgeStatus` rides the message. The first available status sets
`forgeShown`, which is what `m.leftTabs()` / `topTabSegsWith` / `activateTab`
consult; it never flips back in that repo (a later failure is `prsErr`: the
header's `! github: …` over the previous list, or the empty-panel text with
`[r] retry`). `reRoot` resets all of it and moves an active PR tab to Branches.
Test seam: `domain.ForgeDisabled` (set in the TUI `TestMain`) keeps every test
off the real `gh`; `pr_read_serial_test.go` lifts it serially against the fake.

**enter = three hops.** `openPRCmd` resolves `svc.PRFetchOp` off-thread (it
asks the forge for the base repo) → `prFetchReadyMsg` arms `pendingPROpen` and
`startOp`s the `FetchPRHead` → `opFinishedMsg` success runs
`openPRPreviewCmd` (`svc.PRPair` → `PreviewOpen(head, base)`), a one-off
preview (`id ""`). `previewOpenMsg.title` / `previewOpenState.title` override
`previewTitle`, and `reopenPreviewCmd` carries the open pair's title along so a
previews refresh cannot rename a PR diff to "Merge preview: refs/gg/pr/7 → …".
`opAffectedSources` maps both PR ops to an EMPTY, non-nil slice (nil = all =
the remote-tags ls-remote probe). `ForgetPR` re-reads the list through
`pendingPRsReload` — the list is not a registry source, so `pendingSources`
cannot carry it.

**The hub** (`pr_hub.go`) is `prHubPopup{*contentPopup}`: scroll, `/` filter,
`s`, `ctrl+t` and esc-to-opener come from the embedded popup; it adds `y` (copy
URL) and `r` (reload), advertised on the popup's `footer` line. Async fill
follows the commit-message viewer, gated on the PR number. The list read has no
body, so the hub re-reads the PR (`svc.PullRequest`) with its comments.
`model.PullRequest.ReviewState` is LOWER-case (`approved`,
`changes_requested`, `review_required`) — the parser lower-cases
`reviewDecision`.

**Fixture gotcha.** A scratch repo whose `origin` reaches a local bare repo
through `url.<path>.insteadOf` does not work: `git remote get-url` returns the
rewritten path, `RepoSlug` no longer matches the forge's slug, and the fetch
falls back to the forge URL — the real GitHub. Point `repo-view.json`'s `url`
at the bare repo instead.

### Forge review comments in the PR diff (plan 3 of 3, `docs/superpowers/plans/2026-09-19-forge-prs-3-comments.md`)

**How domain knows a preview is a PR.** The PR diff is a one-off preview whose
source is `refs/gg/pr/<n>`; `git.ParsePRRef(set.Source)` is the whole
detection — nothing is threaded through the TUI.

**Fetch and read are split.** `PRCommentsRefresh(ctx, n)` is the ONLY network
call: it caches `PRComments` per PR under `forgeMu` (provider call outside the
lock) and reports `changed` from a stable signature (ids, bodies, positions,
resolved, `Updated.Unix()` — never `reflect.DeepEqual` over `time.Time`). The
readers — `PreviewNotesFor`, `PreviewNotesAll`, `PreviewNoteCounts` — consult
the cache only, so opening a PR diff never waits on `gh`. `PreviewNotesAt`
(CLI/web/MCP) does not merge them: the CLI has `gg pr comments`.

**Conversion** (`forge_notes.go`): one root per thread, replies nested and
inheriting the root's anchor; id `forge:<id>`, `NoteSourceForge`, body's first
line = Summary, rest = Rationale, `resolved` tag, `Range{0,0}` for a file-level
comment, `Status` active. Forge notes are appended AFTER
`keepResolved(resolveNotes(...))` — a preview resolves against the new side
only and would drop every LEFT-side comment as stale. `PreviewNoteCounts`
merges into a FRESH map (the store half is a shared cached instance) and never
caches the forge half. `ErrNotesDisabled` is swallowed when forge notes exist.
`NoteEdit`/`NoteReply`/`NoteRemove` reject `forge:` ids with `ErrReadOnlyNote`;
`NotesClear` works by address in the store, so it cannot touch one.

**TUI.** `isFileLevelNote` (forge source + zero range — a STORED zero-range
note is malformed and stays unanchored) anchors on `v.lines[0]`.
`withEditableNoteTarget` filters forge threads out of E/R/Delete and says
"forge comments are read-only" through `m.diffNotice` — the full-screen diff
hides the status bar, so `statusMsg` alone is invisible there.
`diffHasTipNotes` skips forge notes. Collapse: `diffView.collapsed[rootID]`,
row kind `noteRowCollapsed`, seeded once per id in `setNotes`
(`collapseSeeded`), `relayoutKeepingCursor` after a toggle; `o`/`O` are
help-and-menu-only (the diff footer is at its 140-column budget).

**The comment poll is its own tick.** `refreshTick` returns early under any
layer and the diff IS a layer, so `prCommentsTick` runs beside it in the
heartbeat arm (gated on no op/modal/load, `scheduledInterval(prsItem)`,
`prCommentsLast`, one in flight). `handlePRCommentsMsg` clears
`prCommentsInflight` BEFORE the PR-number check — a read whose view closed
under it would otherwise block every later PR diff from fetching.
`previewOpenMsg.prNumber` / `previewOpenState.prNumber` ride a re-resolve like
`title`; `handlePreviewOpenMsg` chains the first fetch when `prNumber > 0`.

### Forge pull requests in the web UI (plan 4, `docs/superpowers/plans/2026-09-19-forge-prs-4-web-list.md`, spec `docs/superpowers/specs/2026-09-19-forge-prs-web-design.md`)

- **The wire value is the PR NUMBER.** The preview endpoints allowlist
  `source`/`target` against the branch lists; `refs/gg/pr/<n>` and a merged
  PR's base sha are not branches and are never accepted from the page.
  `GET /api/pr/open?n=` finds the PR among the rows the page was SHOWN (404
  otherwise), resolves `PRPair` server-side and answers the preview-open shape
  (`previewOpenBody`, shared with `writePreviewOpen`) plus `pr`. Its
  `source`/`target` are DISPLAY names (the head may live in a fork).
  `prs_static_test.go` pins that prs.js puts nothing but `n` on a PR URL.
- **A GET never calls the forge.** `prCache` (`internal/web/prs.go`, keyed to
  the service pointer like `remoteTagCache`) holds the last listing;
  `loadPRs` is the one lane that probes + lists, then emits the live source
  `prs`. `GET /api/pr` kicks ONE background load for an unprobed service and
  answers `{available, loaded, error, prs}` at once; `POST /api/pr/refresh` is
  the guarded synchronous re-list (the header's ⟳). Forge callers in the web:
  the list lane, the `pr-fetch` builder (`PRFetchOp` reads the head sha), and
  plan 5's comments refresh.
- **`state:"unfetched"` is a LIVE ref check** (`domain.PRFetched`), never the
  cached row's `fetched`: the page asks right after `pr-fetch` finishes, while
  the post-run re-list is still in flight.
- **Ops ride `RegisterOp`** (`op_pr.go`); the `OpBuilder` cleanup slot is the
  post-run hook (`kickPRs(svc, true)`, plus `dropCachedPR` for a forget, which
  removes only a NON-open row). `opStartRequest.Number` is new.
- **The `prs` live lane is outside the master switch** (ruling 13):
  `prsInterval` = `PRsSeconds()` floored by `min_seconds`; `tickOnce` runs its
  due-check after the op-in-flight gate and BEFORE `!cfg.Enabled`; `startLive`
  starts the tick loop for prs alone when refresh is disabled. `onPRs` KICKS
  (the ticker is one lane, a gh list takes seconds) and only a `ready` cache —
  a service nobody asked about, or one without a forge, never earns an
  interval gh call. `prs` is deliberately not in `liveSources`.
- **Client:** `#prs-header` / `#prs-list` are born `class="hidden"` and need
  their own `#id.hidden` rule (hiding is per id here). `prs.js` un-hides on
  `available`, re-asks on a 1/2/4 s backoff while `loaded:false` (the
  kick-then-emit race), and hands `window.__ggRefreshPRs` to sidebar.js (no
  import — the previews.js cycle). `prsrow.js` is import-free and node-tested;
  `review_state` is LOWER-case on the wire.
- **A PR diff rides `state.previewOpen` with `pr: n`.** Because its names are
  display text: `armPreview` skips `loadPreviewCounts`, `noteQuery`/`fetchNotes`
  read it as a plain commit diff on the head sha, `linkFor` refuses, and
  `reopenPreviewIfMoved` returns early. Plan 5 replaces the first three with
  `pr=n` reads.
- **The hub captures its clock seams at construction** (`h.now`, `h.tick`).
  With prs on by default EVERY `startLive` now runs a tick loop, even with
  refresh disabled; a loop leaked by a test that never Closes read the package
  `liveNow`/`liveTick` and raced the next test's `useFakeClock` (the race gate
  caught it).
- **A timed-out probe is not `off`** (`loadPRs`): domain does not cache a
  cancelled probe, so the web stays unprobed with an error text and the next
  read retries. A listing that finishes after a re-root returns without
  touching the cache (`prCache.at` would hand it back to the old service).
- **`fetchPRs` was called in live.js without an import**: the ReferenceError
  was swallowed by the refresh lane's catch and only the browser interval
  check (refresh disabled, `prs = 10`, no click) saw it.
  `TestFetchPRsIsImportedWhereItIsCalled` pins it. `fetchPRs` is single-flight
  with ONE queued re-run — a dropped call could keep a pre-update answer.
- **The PR cache is domain-level** (`internal/domain/forge_cache.go`), so
  every frontend gets it: the LISTING seeds `forgePRCache` (it carries the head
  sha), `baseRepo` is asked once per session, and `PRFetchOp` reads both from
  the cache — zero forge calls for a PR the user can see. With the cached
  `HeadSHA`, `FetchPRHead`'s idempotence check turns an unchanged PR into a
  purely local op. Eviction is LAZY (on the next cache access; no timer
  goroutine): entries unused for `prCacheIdle` (5 min) go; `forgeNow` is the
  test clock. `PRRevalidate` ALWAYS asks the forge, refreshes the cache (and
  `forgeTerminal` for a no-longer-open PR) and reports `Moved` (local ref
  missing or ≠ the forge's head).
- **Stale-while-revalidate, both frontends.** Web: a `fetched` row opens from
  `GET /api/pr/open` at once, then `POST /api/pr/revalidate?n=` (guarded; folds
  the answer into the cached ROW without a re-list); `moved` + the PR still on
  screen → `pr-fetch` + re-open. TUI: `handlePreviewOpenMsg` fires
  `prRevalidateCmd` after the view is up; a moved head re-runs `openPRCmd` and
  `prRevalidateSkip` stops that reopen from revalidating again (a fetch that
  cannot reach the forge's head would loop). The in-flight flag is cleared
  before the "still my view" check, as in `handlePRCommentsMsg`.
- **The loading mask** (`#pr-mask`, prs.js `maskOn/maskOff`) is
  `position: fixed`, sized from `#panes` minus the sidebar, dismissable by a
  click, cleared in a `finally` and by a 90 s safety timer. A browser check's
  visibility helper must not use `offsetParent` for it — that is null for
  every fixed element.
- **`compare.previewBar`** replaces the origin-filter buttons for a merge
  preview / PR (they could only ever be disabled there).
- **Tests:** web `TestMain` sets `domain.ForgeDisabled`; PR tests inject
  `fakeForge` via `SetForgeProviders` and a ticker-less hub (`prServe`), so
  they need no env. Browser fixture = the TUI one copied, with PR 12 a REAL
  merged PR (head ≠ base) — head == base reads as preview state `merged`.

### Forge PRs in the web UI — threads, note folding, details (plan 5, `docs/superpowers/plans/2026-09-20-forge-prs-5-web-comments.md`)

- **Domain:** `PreviewNotesAt` (the diff-less read: web, CLI, MCP) mirrors
  `PreviewNotesFor` — `forgeNotesFor(set, path)` appended AFTER
  `keepResolved`, returned alone when the file has no stored notes or the
  machine has no note store. `PRCommentsCached(n)` reads the comment cache
  without a forge call. `WireNote` gained `read_only` (source = forge),
  `resolved`, `file_level` (forge + zero range) and `created` (RFC 3339
  string — a `time.Time` would never `omitempty`).
- **Routes (`internal/web/prnotes.go`), all keyed on `?n=`:**
  `GET /api/pr/notes[&path=]` = `/api/preview/notes`' shape over
  `PreviewNotes(pair.Head, pair.Base)` — the HEAD is the set's source, because
  the domain recognises a PR by `ParsePRRef(set.Source)`; no path = counts
  only; an unfetched PR answers the empty shape. It never calls the forge.
  `POST /api/pr/comments/refresh` → `{changed}` and `POST /api/pr/details` →
  `{pr (with body), hub, outdated, truncated}` are the two forge-spending
  calls, write-guarded, under `prRevalidateBudget`. **Spec amendment:** details
  was drafted as a GET; the PR body is not in the listing, so it always costs
  a forge call and R2 (a GET never calls the forge) makes it a POST.
  `/api/pr/open` also sends `link_source` / `link_target` (the PRPair) OUT;
  they are never read back (R1).
- **Client:** `notebox.js` (import-free, node-tested) owns the title text and
  the collapse set. `state.noteCollapsed` holds ROOT ids and is re-seeded
  (resolved forge threads) only when `path + rev` changes — a note write or a
  comment re-poll re-reads the same diff and must keep hand-made folds. The
  fold is a CLASS on the `<tr>` toggled in place (`toggleNoteCollapsed`);
  `renderDiff` would reset the ‹/› stepper and jolt the scroll. Whole-file
  threads (`file_level`, line 0) are skipped by `noteRowsHTML` and emitted by
  `fileNoteRowsHTML` right after the `<colgroup>`, full-span even in the split
  layout. The agent-off filter exempts `read_only`. `E`/`R` and the ◆ menu
  refuse a read-only note (`readOnlyNote`).
- **Re-poll:** `refreshPRComments(n)` (`prs.js`, `runOnce("pr-comments")`) runs
  at the end of `showPR` — after the diff is on screen, never on the click
  path — and from `live.js` on every `prs` event while a PR is open. When
  `changed`: `fetchNotes()` if a file of that PR is open (it also carries the
  counts), else `loadPRCounts(n)`.
- **Details overlay:** `#prdetails` has its OWN `#prdetails.hidden` rule (the
  web hides by ID; without it the backdrop covers the page from load — the
  browser run with the rule removed dies on the first click). `prdetails.js`
  owns the compare-bar chip's click, so `files.js` never imports it.
- **Links:** `linkFor` swaps a PR ctx's display pair for
  `{linkSource, linkTarget}` and refuses without them.
- **Browser-check lessons:** in the split layout `tr[data-no] td.side` is the
  LEFT cell — clicking it marks the OLD side, and `c` then (correctly) refuses
  on a preview; the sidebar is not on screen while a file's diff is open, so
  test a sidebar row's menu before opening a file; wrap the whole script in
  try/catch so an abort prints as a FAIL instead of an empty result table.
- **Details, stale-first (follow-up):** `openPRDetails` pushes the layer
  BEFORE any request (loading line inside), then `GET /api/pr/details`
  (cache-only: `domain.PRDetailsCached` = a FULL cached PR + a comments entry,
  `{cached:false}` otherwise; `PullRequest` now `putPRLocked(pr, true)`), then
  the POST. `showing` guards a slow answer against repainting another PR's
  overlay; a refresh keeps `scrollTop`. Fixture note: a listed row overwrites
  the cached TITLE (the body survives), so list/view fixtures that disagree
  show the list's title until the POST lands.

### Forge PR text as markdown — parser core + web (plan 1, `docs/superpowers/plans/2026-09-20-forge-prs-markdown-1-core-web.md`, spec `docs/superpowers/specs/2026-09-20-forge-prs-markdown-design.md`)

- **One parser, in Go.** `internal/markdown` (leaf: stdlib + `syntax`) —
  `Parse(src) Doc` is total and bounded (`MaxInput` 256 KiB → plain
  paragraphs, `MaxDepth` 8, `MaxTableCols` 64, `MaxLexLines` 2000). The `Doc`
  JSON tags ARE the web wire shape; `testdata/*.md → *.json` goldens pin it
  and the web's node test (`markdownjs_test.go`) paints those same files.
  Regenerate with `go test ./internal/markdown/ -update`.
- **Subset, deliberately:** ATX headings, paragraphs (every newline is a hard
  break, as in GitHub comments), bullet/ordered/task lists, quotes, fenced
  code, pipe tables, rules; strong/em/del, code spans, links, angle + bare
  autolinks, images, `@mention` / `#123` / `org/repo#9` refs, escapes. Raw
  HTML, reference links, footnotes, setext headings, indented code, emoji
  shortcodes and entities are literal text.
- **`safeURL` is the one gate** for link/image destinations: only a clean
  `http(s)` URL ever lands in `Inline.URL`. Verdicts: ok / show (relative,
  `mailto:` → the text plus ` (dest)`) / drop (`javascript:`, `data:`,
  `vbscript:`, sniffed with whitespace and control bytes squeezed out).
- **Closer searches are memoised per run** (`noCloser`, `noTicks`) and link
  scans are length-capped, so a wall of unmatched `*` / `[` / backticks stays
  linear (`TestInlineIsBounded`). `FuzzParse` checks the consumer invariants
  (`checkDoc`) and that no LETTER is lost (digits are: an ordered list keeps
  its number in `Start`).
- **Fence colouring:** `colourCode` hands the lower-cased fence language to
  `syntax.Lex` (chroma resolves aliases: golang, sh, js, py, yml…). `Lang` is
  kept only when it is a tame identifier, so an info string never reaches a
  class name or an attribute. `suggestion` is never lexed.
- **The thread split** is `markdown.Summary(body) (summary, rest, rawFirst)`:
  prose/heading → first line (markers stripped) + the rest; a list → first
  item's line, rest = the WHOLE body; fence/quote/table/rule → a label
  (`code`, `suggestion`, `quote`, `table`, `—`), rest = the whole body. Plain
  prose splits exactly as the old `strings.Cut` did
  (`TestSummaryKeepsThePlainSplit`). `rawFirst` rides on
  `ResolvedNote.SummarySrc` (a domain type — nothing new is stored).
- **Trees are opt-in on the wire.** `ToWireNoteRendered` (web only) attaches
  `md` (the rationale) and `summary_md` to FORGE notes, replies included;
  `ToWireNote`/`ToWireNotePreview` — what the CLI and MCP give agents — never
  do, and a stored note is never parsed. `/api/pr/details` (both verbs) adds
  `body_md` and per-comment `md` BESIDE the raw strings. `web` reaches the
  parser through `domain.ParseMarkdown` / `domain.MarkdownDoc`. Parsing is
  local, so R2 holds.
- **`static/markdown.js` is a painter**, import-free and DOM-free
  (`mdHTML`, `mdInlineHTML`, `esc` injected). Closed tag list, headings
  painted as `h3…h6` with `.md-hN` classes, `href` only for `^https?://`
  (re-checked there), images as links, tok classes `^[a-z]{1,4}$`, aligns
  from a fixed set, every value type-checked, depth-capped. A forge note /
  comment WITHOUT a tree falls back to `esc(raw)`.
- **Gotchas:** the `.tk-*` colours are selector-scoped — `#prdetails .tk-*`
  was added (note boxes already sit under `table.diff`); `#prdetails-box h3`
  out-ranks a plain `.md .md-h`, so those rules name `#prdetails` too; the
  host boxes are `white-space: pre-wrap`, `.md` resets it; a right-click on
  `a[href]` inside a note returns early so the browser's own menu shows.
- **Browser fixture:** `prweb/runmd.sh` swaps in `threads-7.md.json` +
  `pr-view-7.md.json` (markdown + hostile strings), runs `pw/md.mjs`
  (29 checks), restores `pr-view-7.base.json`; `prweb/mdguards.py` removes one
  guard at a time and expects named checks red.

### Forge PR text as markdown — the TUI (plan 2, `docs/superpowers/plans/2026-09-20-forge-prs-markdown-2-tui.md`)

- **`mdRows(doc, width) []mdRow`** (`tui/md_render.go`) is the one layout:
  text rows + a per-rune mask, `len(cls) == len([]rune(text))` always. The
  mask is the EXISTING `[]syntax.Class` channel (`contentLine.cls` →
  `winRow.cls` → `styledRuns` → `styles.syntaxStyle`) extended with TUI-local
  pseudo-classes `mdStrong…mdQuote` (values ≥ 100, never in `syntax`), which
  `syntaxStyle` routes to `mdStyle` — ATTRIBUTES over the base style (bold,
  italic, strikethrough, underline, faint); only `mdCode` takes a colour, the
  String syntax colour (bold when the theme has none). No new theme roles —
  a spec §7 amendment. Wrap, hscroll, search emphasis and the reverse-video
  mask drop therefore apply unchanged.
- **Width:** `width > 0` word-wraps prose (display-width aware, hard-splits an
  over-long word, hangs list/quote continuations by prefixing inner rows laid
  out at `width - prefixW`); code and tables are CLIPPED (`mdClip`), never
  reflowed; `mdRows` clips every row once more, so a prefix on a degenerate
  width cannot overflow. `width <= 0` = one row per logical line.
- **The hub does not pre-wrap** (`prMarkdownLines`, `mdRows(doc, 0)`): it is a
  `modeWrap` popup, `renderWindow` wraps at draw time with its intrinsic hang
  indent (`wrapAlignIndent` = everything before the first letter/digit, which
  is exactly a list marker or quote bar), so ctrl+t maximize re-flows for
  free. `prTextLines` remains for the outdated threads' HUNKS (code).
  Behaviour change: a paragraph line's leading whitespace folds, as on the
  forge.
- **Note boxes pre-wrap** (`forgeNoteBodyLines`, width = the box): `noteLine`
  gained `cls`; the summary comes from `ResolvedNote.SummarySrc`'s inline
  tree behind the plain `↳ author: ` lead (`mdInlineRows`), a LABEL summary
  (empty `SummarySrc`) stays plain, and a thread summarised `suggestion`
  drops its leading block's caption (the web does the same through
  `mdHTML(doc, esc, {skipFirstCaption})`). `noteRowCells` paints a masked row
  with `styledRuns`; a stale box drops the mask (grey throughout). Local
  notes take the old path byte-identically (`cls == nil`).
- **Code tokens** keep their real syntax classes: `mdSanitizeMapped` carries
  the parser's rune offsets across tab expansion / control-char flattening.
- Every rune of forge text passes `sanitizeLine` BEFORE its mask is built.

### Forge PR search — seam, domain, CLI, TUI (plan 1, `docs/superpowers/plans/2026-09-20-forge-prs-search-1-core-cli-tui.md`, spec `docs/superpowers/specs/2026-09-20-forge-prs-search-design.md`)

- **Seam.** `forge.PRQuery{State, Text, Limit}` + `Provider.Search` →
  `(prs, more, err)`. `Normalize()` (state `""`→`all`, limit `0`→50, 1…200,
  text trimmed) runs in `domain` before any provider sees the query. States are
  forge-neutral words (`all|open|closed|merged`); `forge.PRStates()` is ALSO the
  frontends' cycle order (all → closed → merged → open).
- **gh.** One `gh pr list --state S --limit N+1 --search "<text> sort:updated-desc" --json …`
  (FakeRunner name `gh pr search`). `--search` is ALWAYS present: gh orders a
  plain listing by CREATION and a searched one by best match (checked against a
  real repo), so without the qualifier a cut at N is not the N newest-updated.
  A text with its own `sort:` field is passed verbatim. The text is the VALUE of
  `--search`, so a leading dash is safe. The extra row is the `more` probe.
- **fakegh** routes `pr list` to `pr-search.json` iff the argv holds `--search`
  (Detect and ListOpen never do) — it does not filter or limit.
- **Domain.** `PRSearch` (single-flight per normalized query) / `PRSearchLast`
  (session-only, cloned out, never calls the forge; a failed search keeps the
  previous answer). A text matching `^#?[0-9]+$` (n > 0) is a NUMBER LOOKUP via
  `PullRequest(n)` — state ignored, `ErrNotFound` → empty result, no error.
  Rows seed the PR cache (`putPRLocked(pr, false)`) but NEVER `forgeSeen`: a
  search must not grow `PullRequests`' list (ruling 4) — a found PR becomes
  known only through its fetched ref. Frontends use `domain.PRQuery`,
  `PRStateFilter*`, `PRStateFilters()`, `NormalizePRQuery`.
- **CLI.** The value flags take the NEXT argument unconditionally (or `=`), so
  `--search -label:bug` parses; they are a usage error on any verb but `list`.
- **TUI.** `prSearchPopup` (layer, two zones: prompt / rows). In the prompt every
  printable key is text; in the rows zone every key is swallowed. Answers are
  gated on `busy && msg.q == asked` (the `prHubMsg.number` pattern). The rows
  call `openPRCmd` / `openPRHub` directly — `canOpenPR`/`selectedPR` are
  `focus == panelPRs`-bound and read the TAB's row. The popup stays on the stack
  through the fetch op; `handlePreviewOpenMsg` parks it with
  `handOffToFilesView` ONLY when `layerOf[*prSearchPopup]` is live — gating on
  "any top layer" would park a hub or note popup during a moved-tip re-open.
  `enter` on a PR the tab does not list arms `pendingPRsReload`; opFinished
  BATCHES that re-read with the PR open (it used to assign, dropping the open).
  `prRows()` is now `prRowsFor(m.prs)` — one painter for the tab and the popup.
- Footer binding `pr-search` is `scopeWindow` (it needs no row: an empty list is
  when search matters). Palette entry carries `feature: FeatureForge` and is
  filtered on `!m.forgeShown`.

### Forge PR search — the web (plan 2, `docs/superpowers/plans/2026-09-20-forge-prs-search-2-web.md`)

- `prsearch.go`: `POST /api/pr/search` `{text,state}` (writeGuard, limit fixed at
  `PRSearchDefaultLimit`, 45 s budget; bad state 400, no forge 404, forge
  failure 502) and `GET /api/pr/search`. The server keeps NO result cache:
  `domain.PRSearchLast` is per-service, session-only and never calls the forge —
  it IS the GET (R2). Answer `{query:{text,state}|null, prs:[prRow], more}`.
- `cachedPR` (prs.go) falls back to the last search's rows — that is what lets
  `/api/pr/open?n=`, revalidate and the details routes accept a searched-only
  NUMBER (R1). A NEW search replaces those rows, so an older result's number
  stops resolving unless it was fetched meanwhile (then the polled list has it).
- Page: `#pr-search-box` sits right BEFORE `#prs-list` and folds with the section
  through `#pr-search-box:has(+ #prs-list.collapsed)` (the fold class lives on
  the list; the adjacency is pinned by a static test). `#pr-search-box` and
  `#pr-search-results` each have their own `.hidden` rule (R3). `prRowHTML` is
  the one row painter for both lists; `knownPR(n)` looks in both.
  `window.__ggFocusPRSearch` hands `A` to keys.js (import cycle); it unfolds the
  section by clicking its header. `openPR` marks `pr.fetched` after a fetch so a
  second click on a result does not fetch again.
- Browser fixture: scratchpad `prweb/runsearch.sh` (a gh wrapper that LOGS its
  argv — the R2 check counts `--search` lines), `pw/search.mjs` (32 checks),
  `prweb/searchguards.py` (8 guards, restore by re-writing the saved text).

### Wrap mode wraps on words (`internal/tui/window.go`)

- `winOpts.charWrap` (default false) picks modeWrap's layout, and BOTH
  `renderWindow` and `wrapContentLines` go through `wrapRow`, so a line count
  can never disagree with the layout. Default = `wrapHangWords` (break after
  the last space that fits; only an over-wide word is split; never a break
  inside the row's own lead, which would strand a marker) +
  `wrapAlignIndentProse` (a `<digits><. or )><space>` marker hangs like a
  bullet; a row that merely starts with a number keeps the plain rule).
  `charWrap: true` = the old `wrapHang` + `wrapAlignIndent`, set by the CODE
  views only: `blame_view`, `file_preview`, and the *View content*
  `contentPopup` (`contentPopup.charWrap`). The two-column diff/blame cells
  (`twocol.go`) have their own wrap and are untouched.
- **Invariant kept:** every segment is a VERBATIM rune slice of the text
  (continuations behind pad spaces) — the break keeps its space at the END of
  the line, nothing is dropped — so `wrapSegMask` maps class and emphasis
  masks unchanged. Consequence: a word that exactly fills a line is given up
  to the next line rather than starting it with the space.
- `winRow.noWrap` / `contentLine.noWrap`: a PREFORMATTED row is rendered as a
  cutoff row inside modeWrap (one line, `…`) and counts as one line. Set from
  `mdRow.pre` (code lines, table rows and rules) and by `prTextLines` (hunks).
- `forgeLabelText` translates the domain's label summaries in the note box and
  the collapsed row; it keys on `SummarySrc == ""`, so a reviewer who really
  wrote "quote" is left alone.

### Agent sessions core (`internal/agentsession`, plan 1, 2026-09-24)

Spec `docs/superpowers/specs/2026-09-24-agent-sessions-design.md` (incl. the
spike findings), plan `docs/superpowers/plans/2026-09-24-agent-sessions-plan-1-core.md`.

- **Three goroutines per session.** `pumpOut` PTY → emulator (+ taps +
  `Changed`); `pumpIn` emulator → queue → a writer → PTY; `wait` records the
  exit. `pumpIn` exists because the emulator answers the child's terminal
  queries (DA, cursor-position report) through its output pipe — without it
  Claude Code stalls at startup — and keys/pastes are encoded into that same
  pipe.
- **The emulator's output is a synchronous `io.Pipe` written under its own
  lock** (inside `Write` for replies, inside `SendKey`/`Paste` for input), so
  `pumpIn` drains into a 1024-chunk queue and never blocks; a child that
  stops reading stdin would otherwise freeze `pumpOut` and the caller.
  `TestPasteToNonReaderDoesNotBlock` runs the child in RAW mode — in
  canonical mode Linux drops overflow input and the test cannot see a stall.
- **Locking.** `SafeEmulator` locks each method, but `CellAt` returns a live
  cell pointer and `Close` is unlocked. The session's `ioMu` serialises every
  emulator mutation, `closeIO`, and the snapshot reads (`Screen`,
  `screenText`). `closeIO` ends `pumpIn` by closing the emulator's
  `InputPipe()` writer (an `*io.PipeWriter`), never `Emulator.Close`, whose
  flag write races the blocked `Read`.
- **Exit ordering.** `wait` lets `pumpOut` drain (`drainOutput`: until
  end-of-stream, 150 ms of silence, or 2 s) before marking `Exited` and
  closing, or the child's last output is lost. The parent closes its slave fd
  after `Start`; Linux then reports EIO (= end of stream) on the master — the
  kernel also hangs up the TTY when the leader exits, so Linux never needs
  the quiet rule. ConPTY never ends the stream on its own: xpty keeps the
  output pipe's WRITE end open in our process until `Close` (its handles are
  unexported and `Close` would close them again, so no early partial
  release) — the quiet rule is what keeps a Windows exit off the 2 s bound
  (`TestWindowsExitIsPrompt`).
- **Surrogate halves (Windows).** Bubble Tea v1's console reader
  (`coninput`) casts each UTF-16 unit to a rune, so an emoji arrives as a
  high then a low surrogate, often in two `KeyMsg`s; `string(rune)` of a
  half is U+FFFD. The focused console joins them (`joinSurrogates`, the
  pending half in `consoleState.highHalf`). gg's own text inputs likely
  share the bug.
- **Cursor on a wide glyph.** `cursorLine` moves a cursor that sits on a
  wide glyph's right half (a zero cell) onto the glyph; reversing the zero
  cell as a space pushed the row one column wider.
- **Kill.** Unix: the child is a session leader (`Setsid`+`Setctty`);
  SIGTERM to `-pid`, SIGKILL after 3 s or once the leader is reaped. The
  process-group test's grandchild ignores SIGHUP: the kernel HUPs the
  foreground group when the leader dies, which hides a leader-only kill.
  Windows: a kill-on-close job object; `TerminateJobObject`.
- **Child env** drops `TMUX`/`TMUX_PANE` (agents otherwise think they run in
  a tmux pane) and sets `TERM=xterm-256color`, `GG_SESSION_ID`.
- **domain.** `Sessions()` is the one process-global manager (repogate
  precedent; test seam `UseSessionManager`), type aliases keep frontends off
  `agentsession` (archtest). `StartSession` runs `$SHELL -c <line>` with NO
  `exec` prefix (on a compound line it would run only the first command).
  On Windows it passes a VERBATIM `"%COMSPEC%" /S /C "<line>"` via
  `StartSpec.CmdLine` → `SysProcAttr.CmdLine`: `x/conpty` otherwise composes
  the line from argv with `\"` escaping that cmd.exe cannot parse (a quoted
  `"C:\Program Files\…\claude.exe"` would break); separate lines join with
  ` & `.
  `EnsureSessionCommands` treats any existing `session` block, even an
  invalid one, as configured.
- **exttool/config.** `category = "session"` and `mode = "session"` only
  come together (`ValidateToolCommand`, catalog invariant test).
- **Windows input (for the TUI stage):** Bubble Tea v1 turns a bare
  Ctrl/Alt/Win key-down into `KeyRunes{0}` (only Shift is filtered) — drop
  NUL-only rune messages; batched `KeyRunes` go through `SendText`.

### Versioned external-tool templates (2026-09-30)

Spec `docs/superpowers/specs/2026-09-30-tool-template-versions-design.md`.

- **Families, not rows.** Catalog rows sharing (Category, Name) are one
  family; each row is a VARIANT with an agent-version `Range` (half-open
  `>=X.Y <X.Y`, "" = any, alone). `Builtins` fills `Version` 0→1.
  `exttool.Pick(tool, v, known)` = one row per family (unknown version →
  the variant with the highest lower bound); `Variant(...)` resolves a
  block (known version → its range, else the stamp's `agent_range`, else
  the family's only variant).
- **Guard.** `TestCatalogVersionBumpGuard` +
  `testdata/catalog_versions.golden` (version + normalised fingerprint per
  family, Range and OptIn included): a changed family with an unraised
  Version fails, a stale golden fails; `-update` rewrites it.
- **Stamp.** `config.ToolCommand.{TemplateVersion,AgentRange,Fingerprint}`;
  `AppendToolCommands` / `ReplaceToolCommand` compute the fingerprint from
  the block being written (so a test that wants an "edited" block must edit
  the FILE after writing). `ToolFingerprint` = mode, per_file, when_op,
  sorted frontends, command with whitespace runs collapsed and blank lines
  dropped. Unstamped = edited. Every catalog writer goes through
  `domain.NewToolBlock`.
- **Status** (`domain.ToolTemplateStatuses(ctx, paths, dets)`; the Service
  method = global + active repo file + real detection): only EFFECTIVE
  blocks (repo shadows global) of a DETECTED family. Kinds: current,
  customised (stamped, edited, template unchanged — silent), update
  available (behind, or an unstamped block that differs from the target),
  unsupported (known agent version outside every range). No automatic
  writes, ever (user ruling). `ApplyToolUpdate` refuses when the file's
  block no longer fingerprints equal to the status's.
- **Agent version.** `domain.AgentVersion` runs `<bin> --version` (3 s,
  stdin closed), cached per (resolved path, mtime). Test seams:
  `agentVersionRun`, and `domain.ToolStatusesDisabled` (set in the tui and
  web TestMains — no frontend test may probe the machine's real agents).
- **Keep mine** = `promptstate.DeclineToolUpdate(OfferKey)` where OfferKey =
  block key + fingerprint of the OFFERED block, stored hashed
  (`ToolUpdateID`) — a later template change is a new offer.
- **TUI.** `toolStatusesMsg` (gen-guarded by `noticeGen`, batched beside
  the repo-health read on startup/reRoot; reRoot clears the list); the
  notices (`tool_template_update`, counted, no "Never"; and
  `tool_agent_unsupported_<hash>`) are derived in `rebuildNotices` OUTSIDE
  the health half. Settings → External tools rows show the suffix; `u`
  opens `toolUpdatePopup` (template text scrolls so the answer keys never
  leave the screen; the fingerprint line is hard-split by cells). Take new /
  Keep mine settle the status immediately (`settleToolStatus`,
  `rebuildNotices`) — the re-read runs `--version` and lands late.
- **Writers.** `ReplaceToolCommand(If)` / `ReplaceToolCommandBodies` find
  headers with `tomlHeaders`/`scanTOMLLine` (multi-line strings, quoted
  values, comments), target the LAST same-key block (the effective one),
  and refuse a result whose decoded document differs outside the block
  (`sameOutsideToolBlock` / `sameExceptToolCommandBodies`). Every tool
  writer holds `filelock` on `<config>.lock`; `ApplyToolUpdate`'s
  "same block?" check (`ReplaceToolCommandIf`, fingerprint) runs under it.
  `ToolBlockLine` positions the review's editor.
- **Installers** (wizard, `Ensure*`) use `domain.InstallTemplates` →
  `exttool.PickBest` (the variant for the agent's version; out of range →
  newest). The wizard is on the UI thread: `probe=false`, cache only — the
  background status read warms the cache for every detected ranged tool.
- **Web.** `GET /api/exttools` → `template_offers`; `POST
  /api/exttools/update|keep {offer_id}` resolve the id against a FRESH
  status read (the wire never names a path or a command). Seam:
  `Server.toolStatuses`.

### Agent console in the TUI (plan 2, 2026-09-24)

Plan `docs/superpowers/plans/2026-09-24-agent-sessions-plan-2-tui.md`.

- **`m.console *consoleState`** is a right-column owner like `stashView` /
  `filesPreview` (rendered first in `renderInterface`'s right-column switch;
  maximised = the whole body). Opening the stash list or a file preview hides
  it. It is NOT in `fullscreenYielded`: a ctrl+t fullscreen left panel simply
  hides the console with the Commits column (the layout deletes the box, so
  nothing can focus or resize it) and it returns when the pin drops;
  `openConsole` clears an active pin so a freshly shown console is never born
  hidden. `reRoot` never touches it — sessions and their console outlive
  worktree/repo switches.
- **Key routing** (`updateConsoleKey`) sits right after the decision modal and
  BEFORE `ctrl+o`/`ctrl+p`/`proc`/the layer stack: a focused console must get
  the chords agents use (Claude: ctrl+o, esc, ctrl+t…). Only `stepOutKey()` /
  `sessionsKey()` are intercepted. Unfocused (its column focused), it claims
  enter/ctrl+t/esc and the step-out key (it closes, as esc does — so the key
  twice backgrounds a focused console), X (= the sub-row's
  `killRemoveSessionRow`; x/X on an exited agent = `removeConsoleSession`), lets `consolePassthrough` (focus moves, quit, help, menu,
  palette, repo-wide globals) through, and swallows the rest so j/k, /, o…
  never act on the hidden Commits list.
- **Key encoding** (`encodeConsoleKey`): runes → `SendText` (batched typing is
  one message), paste → `Paste`, specials → `uv.KeyPressEvent` so the emulator
  honours DECCKM etc. A NUL-only `KeyRunes` is dropped (Windows bare-modifier
  key-down). Bubble Tea v1 aliases: KeyEnter=ctrl+m, KeyTab=ctrl+i,
  KeyEsc=ctrl+[, KeyBackspace=ctrl+? — one map entry each.
- **Repaint**: `waitSessionCmd` blocks on the console's own `Subscribe()`
  channel (since 2026-09-28; `Changed()` is gone) then sleeps
  33 ms, so a chatty agent costs ≤30 frames/s; `gen` drops a replaced console's
  waiter. `waitSessionsCmd` (armed in `Init`) carries list changes →
  `onSessionsChanged` (exit notices via `m.sessionStates`).
- **Size**: `consoleInner(boxW, boxH)` = `boxW-4 × boxH-3`; `syncConsoleSize`
  runs on WindowSizeMsg, open, maximise and shrink (Resize is a no-op when
  unchanged). The cursor is painted only for a focused, running console
  (`ScreenWithCursor`).
- **Worktrees list is entry-based** (`worktreeEntries`: a worktree, then its
  sessions). A session row's Name/Date are its parent's (stable sort keeps it
  under the parent), its Haystack includes the parent row (a / filter never
  strands it), its Key is `path\x00id`; `backingIndex(panelWorktrees)` maps
  entries to `m.worktrees` and returns ok=false on a session row — every
  worktree action/copy row ignores it, the WIP pseudo-row precedent.
- **Quit guard**: every quit path ends in `tea.QuitMsg`, so
  `tea.WithFilter(quitFilter)` in `run.go` turns it into `quitHeldMsg` while
  sessions live and `!m.quitConfirmed` → the quit-mode popup; `Q` sets
  `quitConfirmed` and runs `killAllAndQuitCmd`. `Run()` kills whatever is left
  after the program ends (safety net).
- **alt+a / alt+t = cycle by last use** (2026-10-01): `agentsession.Info`
  carries `LastUsed` (starts at `Started`; moves on `SendKey`/`SendText`/
  `Paste` and `Session.Touch()`) and `Terminal` (set by `StartTerminal` via
  `StartSpec.Terminal`). `sessionsByLastUsed(list, terminal)` = running
  sessions of one kind, newest use first. `cycleSessions` walks a RING: the
  sessions, then the console's return point (2026-10-06). `consoleState.ret`
  (`consoleReturn`) is captured by the first console shown over a
  non-console screen (`captureReturn`: the live layer stack is PARKED only
  when its top is a full-screen VIEW — diff/history/blame/file viewer; a
  popup or editor on top stays live and keeps the keyboard, so a console
  opening on its own never takes it — and the ctrl+t pin / stash list / file
  preview (restored only inside the same files view) / focus saved) and carried over
  when a console replaces a console, so "the screen before the agent"
  survives a cycle; `closeConsole` restores it (parked layers go BENEATH
  whatever is live — a popup opened over the agent stays on top; focus only
  when it sat in the console's column). `ret.full` (a full-screen layer
  parked or an active pin) shows every console of the cycle maximised;
  `maximized && !focused` exists only there: enter/ctrl+t focus, esc /
  step-out return, only `consoleFullPassthrough` (q, ctrl+c, ?, ctrl+o,
  alt+a/alt+t) reaches gg — openers like F/S would open behind it — and it
  owns its keys even over a focused files tree; focus snaps back to the
  column; step-out (and an exit) from a focused full console stays full
  (`consoleFull`), from a ctrl+t-maximised docked one docks. `dropConsole`
  (another right-column owner) restores parked layers too — a parked view
  is never lost; `detachConsole` is the bare clear. A view pushed OVER a
  shown console, then alt+a: the console returns first, the cycle restarts
  with the whole stack. Parked views still get their async results: `Update` →
  `dispatchParkedAware` hands a non-input message to `dispatch` with the
  parked stack put back beneath the live one, then re-parks what is left
  (a diff load, a stack file, history/blame lists, a resize relayout). A
  repo switch (`reRoot`, and `settleConsole` again) runs
  `forgetConsoleReturn` (parked views / stash list / preview go; pin and
  focus stay; full only if the pin was);
  `steerRefusal` reads a parked stack's top when the live stack is empty.
  Only a FOCUSED show is a use: `openConsole` and the unfocused console's
  `enter`/`ctrl+t` (`touchConsole`) Touch; the unfocused show must not, or
  the walk would reorder itself. The web counts through typing only
  (a page's screen stream may reattach on its own, so attaching is not a
  use). The gate is `cycleReachable` (`avail.go`: base panels or a
  diff/history/blame/file viewer on top — steerRefusal's poppable views —
  not while a search is typed); a focused console intercepts alt+a/alt+t
  before its program (`updateConsoleKey`). An unfocused full console keeps
  the "unfocused never resizes" rule: it renders at the PTY's current size
  until enter. Not ctrl+a (a common tmux prefix — gg never sees it) nor
  ctrl+l (Commits' load-more). Not configurable; not in the web.
- **A console is repo-scoped** (2026-10-01, `console_scope.go`): it shows
  only a session whose `Dir` is one of `m.worktrees` (`inRepo`, the Worktrees
  sub-rows' `filepath.Clean` rule). `reRoot` arms `consoleSwitch`; the
  dataLoadedMsg success arm runs `settleConsoleAfterSwitch` once the NEW
  worktrees are in the model (reRoot itself cannot know them — no git on the
  Update goroutine; the blank-screen gate hides the console meanwhile): a
  foreign console closes (session untouched, status says ctrl+\ brings it
  back), a same-repo worktree switch keeps it. alt+a/alt+t and their footer
  hints cycle `repoSessions` only. The ctrl+\ popup still lists every repo;
  enter (and the AI tasks tab's live console) on a foreign session goes
  through `openSessionAnywhere` = reRoot to the session's worktree +
  `consoleSwitch.open`, refused while `steerRefusal` says something owns the
  screen (the hosted-web switch precedent). This reversed `7a27e2dc`'s
  "popup opens another repo's session without reRoot".
- **Probe recipe**: `tui-capture.sh` sets only XDG_STATE_HOME and a tmux
  server hands sessions its own env, so point gg at a scratch config with a
  `--gg` wrapper script that exports `XDG_CONFIG_HOME` and `exec`s the binary;
  a `[[tools.command]] category="session" command='bash --norc'` block makes a
  deterministic agent. The Worktrees tab is `C-Right C-Right` from Branches.

### Web attach — agent consoles in `gg web` (plan 1, 2026-09-28)

Spec `docs/superpowers/specs/2026-09-28-web-attach-design.md`, plan
`docs/superpowers/plans/2026-09-28-web-attach-plan-1-watch-and-type.md`.
Rulings (do not re-ask): scope C (watch/type + lifecycle + task sessions;
lifecycle is plan 2), rendering = the server's screen repainted (no
xterm.js), input over POST (no WebSocket), the focused viewer owns the
size, the TUI's two keys + one tabbed popup, a layer over the panes, a
dedicated SSE stream per console, session states (erbrus port) = plan 3.

- **Frame protocol** (`console_stream.go`): `GET /api/session-screen?id=`
  → `hello {frame, palette}` (a full frame + the 16 basic colours), then
  `frame`, `exited {code}`, `gone`. A frame is `{full, cols, rows, cx, cy,
  cursor, alt, lines:[{y, runs:[{t, fg, bg, b,i,u,r,d,s}]}]}`; a partial
  frame lists only the rows that changed since the frame THIS stream last
  wrote, a full one every row (fresh attach, size change, or after the
  stream was skipped). Runs come from `agentsession.ScreenRuns()` (cells
  under `ioMu`, equal styles merged, a wide glyph's right half skipped,
  trailing default blanks trimmed — a blank row has no runs; colours as
  `#rrggbb`; the cursor is NOT painted into the runs, the page draws it).
- **Producer** (`screenFeeds`): one goroutine per session while it has
  streams; holds its OWN subscriptions (`sess.Subscribe()` for the screen,
  `domain.Sessions().Subscribe()` for removal — the `agentsession.Broadcaster`
  gives every subscriber its own coalescing slot, so the TUI's console in the
  same process never steals a wakeup), coalesces 40 ms, snapshots once and
  `publish`es to every stream subscriber (1-slot buffers: a full one is
  skipped and its next screen is marked `Full`); `send` delivers exit/gone
  with a bounded wait. Removal arrives on the manager signal (`Get(id)`
  re-checked; no poll).
- **Input** (`console_input.go`): `POST /api/session-input {id, keys, paste}`
  decodes every key BEFORE sending any (a bad key refuses the batch), 409
  for an exited session; keys are `domain.ConsoleKey {k, mod, text}` —
  allowlisted names, mod bits shift=1 ctrl=2 alt=4 — mapped by
  `domain.ConsoleKeyEvent` to the SAME emulator events the TUI's
  `consoleSpecial` sends; plain text (no ctrl/alt) goes through `SendText`
  as one write (IME commits, bursts). `POST /api/session-size` clamps to
  20..500 × 5..300.
- **Page** (`static/console.js`): a fixed layer positioned over the panes
  right of the sidebar (over everything when maximized or the sidebar is
  hidden), a `<div>` per row / `<span>` per run, the cell measured from a
  20-char probe, the cursor a positioned block (outline when unfocused).
  Keys queue while a POST is in flight and flush as one batch; `paste`
  rides the paste event. `keyToWire` returns null for the browser's own
  keys (ctrl+w/t/n, ctrl+tab, ctrl+shift+letter, F5/F11/F12) so they keep
  their default. `m` maximizes (Chrome owns ctrl+t). The console and the
  switcher never import each other: the console asks for the switcher via
  a `gg:switcher` document event.
- **Live events:** `/api/events` carries Reason `sessions` (the whole list)
  via `fanOut`, bypassing the op gate; `live.js` feeds it to the switcher,
  the console (retitle / close on gone) and the sidebar (`takeSessions`).
- **Size ownership on the TUI side:** `syncConsoleSizeIfFocused` on
  `tea.WindowSizeMsg`; `enter` on an unfocused console calls
  `syncConsoleSize` (focus gain takes the size back). Open, maximize and
  the ctrl+] un-maximize keep pushing (the user's own actions).
- **Browser check** (`attach_browser_test.go`, `GG_BROWSER_CHECK=1`): plan 1
  has no web start path, so a test hosts a real `sh` session + page and
  holds until `GG_BROWSER_DONE` appears; playwright drives it. Gotcha: the
  steer-hint wiring pin expects the exact string `revealHintEntry } from
  "./sidebar.js"` in live.js — keep that import's last name.

### Web attach — session lifecycle on the page (plan 2, 2026-10-02)

Plan `docs/superpowers/plans/2026-10-02-web-attach-plan-2-lifecycle.md`.
Rulings (user, do not re-ask): a hosted page's start is the TERMINAL's
start; finished-task results in the web viewer are a separate follow-up;
the page has the TUI's `X` (kill and remove).

- **Endpoints** (`session_lifecycle.go`), POSTs behind `writeGuard`:
  `GET /api/session-commands?worktree=` → `{commands:[{name, command
  (resolved), approved, found}], added, config_path}` — `SessionCommands(cfg,
  "web")`; with NO session command configured it runs
  `EnsureSessionCommands` inside the request (the page shows "Detecting
  installed agents…"); a start never detects. `EnsureSessionCommands`
  re-reads the global file under a process mutex before appending (the
  caller's config may be stale — terminal and page each hold their own),
  and the TUI's ensure reloads its config even when nothing was added.
  `POST /api/session-start {worktree, tool | terminal,
  approve, cols, rows}` → `{session}`; 400 a path that is not one of
  `svc.Worktrees` (exact match — the allowlist) or an unknown tool, 409
  unreachable (probed BEFORE the approval question), 403 + `needs_approval`
  (template hash, resolved text — the AI lanes' pattern, shared store with
  the TUI), size clamped (none = 100×30). `POST /api/session-kill {id,
  remove}` (409 already exited) and `POST /api/session-remove {id}` (409
  running); 404 unknown id. Handlers never broadcast: the manager's signal
  reaches the tabs through `watchSessions`.
- **The starter seam:** `Host.SetSessionStarter(fn(ctx,
  domain.SessionStartRequest) (SessionID, error))`. Unset (standalone
  `gg web`): `startSessionHere` — `GG_INBOX` = the page's steer inbox, no
  agent channel. Set by the TUI (`tui/websession.go`, the `SetSwitcher`
  shape: `sessionStarterFor` → `webSessionRequestMsg` → Update →
  `webSessionStartedMsg`): the terminal's own env (`childEnv`, `agentURL`),
  `childInbox` recorded, a status line, NO console. The server bounds the
  round trip at 30 s (`Server.startTimeout`) — a wedged terminal cannot
  hang the page. The TUI reloads its config in that handler when it has no
  session command yet: the page's first-run detect just wrote them, and
  `AppendToolCommands` does not dedupe.
- **Page:** `sessions.js` (dialog phases detecting → choose → approve →
  starting; `dialogStep` is pure; a start in flight takes no key, and a REPEATED
  enter does nothing — a held key must not pick a row and approve it), menu
  rows through `registerRows` on `worktree`, `branch` (gate =
  `worktreePathForBranch`) and the new `session` key (the sub-row's
  right-click menu in `sidebar.js`). `killSession` / `removeSession` live in
  `console.js` and confirm through `showLocalConfirm` — console.js → ops.js
  → sidebar.js → console.js is an import cycle that is safe only because
  the confirm is called from handlers, never at module load. In the
  switcher `k` is kill on the Agents tab and "up" on the other two.
- **Console rendering rules (2026-10-02 fixes):** a console row is
  `.conrow` — NEVER `.crow`, the commit row's class (flex, nowrap, padded),
  which once deformed every screen. The cell is a `.conrow`'s height × a
  twentieth of twenty M's (`measureCell`; the font's content box is shorter
  than the row). Non-ASCII stretches above U+024F are wrapped in `.cg`
  boxes of N × `--cw` (`textHTML`), and a wide glyph is its own run with
  `w` (cells) from the emulator, because a browser's fallback font has its
  own widths. Session sub-rows render under branch rows too
  (`sessionSubRows`, `sessionRowMenu` in sidebar.js). The console GIVES
  WAY: `files.js setLayout` fires a `gg:panes` document event, which closes
  the console only within 10 s of a click outside it (background refreshes
  call setLayout too); `layers.js pushLayer` (a surface with a lower
  z-index than the console) and `live.js steerNavigateLand` fire it with
  `force`; a click on a sidebar row that is not a session sub-row closes it
  directly.
- **Browser check:** the `attach_browser_test.go` host seeds a `Shell`
  session command in its isolated global config; the lifecycle script
  starts it from the worktree menu.

### Web attach — session states (plan 3, 2026-10-03)

- **Claude's idle box (fix 2026-10-03):** the rule above `❯` may carry the session's NAME (`──── title ─`), so the waiting rule is `^─{8,}[^\n]*\n❯`. An unmatched screen keeps the previous state (`Unknown` never changes `State`), so a missed idle rule shows as a turn that stays `working` until the 120 s stall marks it `stalled · working`.

Plan `docs/superpowers/plans/2026-10-02-web-attach-plan-3-states.md`.
Rulings (user): states surface to agents + TUI + web; `gg agent wait` and
the report channel are the NEXT plan.

- **`internal/agentstate`** (port of erbrus `internal/screen`, stdlib
  only): `Tail(text, 15)` → `Classify(rules, lines)` in structural order
  **working → waiting → question → unknown**, so a phrase quoted in
  scrollback never outranks the prompt box. Patterns run in `(?m)` mode
  over the joined tail (`^`/`$` bound a line, `\n` spans two). Built-in
  rules per gg agent id: `claude`, `codex`, `junie`, `kimi` (erbrus's
  verified sets), `antigravity` (captured live 2026-10-03: braille spinner,
  a `>` box BETWEEN two rules — the echoed user line has a rule above only —
  trust/permission dialogs), `""` generic; `HasDefaults(id)` is false for
  the generic set. No ANSI stripping: `Session.Text()` is already plain
  cells (a wide glyph's right half is a SPACE tmux captures never had —
  fixtures from gg's own emulator in `live_test.go`). `DialogOptions(raw,
  lines)`: numbered (`❯ 1. Yes`, key = the digit) else cursor-style
  (`> Yes, I trust`, key `pick:<i>`, `Current`) read from the UNTRIMMED
  screen (the cursor line fixes the text column; radio lists skip deeper
  descriptions); carried on the wire, never pressed.
- **`domain.SessionStates()`** (`session_states.go`): one process-global
  `StateWatcher` over `Sessions()`, started on first use (frontends call it
  at startup; `SessionActivityOf(id)` never starts it — agent verbs and
  tests read through that). It subscribes to the manager (start/exit/remove
  → re-sync) AND to every running session (output → 300 ms coalesce →
  `observe`); a 2 s tick serves the stall clock. `observe(now)`: unknown
  changes nothing (mid-redraw debounce); a transition sets
  `State`+`Since`; notices ride a numbered ring of 64
  (`Notices(after)`, `NoticeSeq()`) — each frontend keeps its own cursor,
  so the terminal and the page it hosts each get every notice once;
  `Subscribe()` is a Broadcaster. **Grace**: the first 30 s after start post
  no notice (sign-in spinners read as working), the badge shows at once; a
  question still up when the grace ends is posted then (a trust dialog), an
  idle-after-working inside it never. **Stalled** = `LastOutput` older than
  120 s while working — or unknown, but only with DEDICATED rules (a generic
  agent would be called stalled at every idle prompt) — or, while working,
  `agentstate.StallKey(tail)` (the tail without a leading spinner glyph and
  elapsed-time counters) unchanged for `spinStallAfter` 11 min: a hung API
  call keeps Claude's timer ticking. Not two: Claude's thinking spinner
  shows no token counter (live 2026-10-04), so a long think looks the same;
  past ten, because Claude's Bash tool and `agent_wait` both end by 10 min
  and a run at its cap must not read as a stall. The notice says
  `Spinning`. **Idle hold**: working → idle shows only once idle has held
  `idleSettle` (2 s), from when it began (`pendingIdle`; the loop arms a
  timer for it). An Unknown read mid-hold keeps `pendingIdle` and arms
  nothing: every screen change wakes the watcher, so the redraw's end is
  read and a Waiting read re-arms the timer (re-checked 2026-10-05). **Own menus**: `Rules.Own` (built-in, Claude's
  "Esc to back/go back/close/clear" footers) read unknown, keeping the state
  — matched on the LAST tail line only: the same words quoted in a diff above
  a permission dialog must not hide it (review fix). Exited sessions are
  dropped. **Rules**: a command with any `screen_*` list is compiled at
  start (`SessionRules(tc)`: a set list replaces the agent's built-in list
  of that kind, a missing one keeps it — a partial Claude block must not
  lose the idle box; review fix) and bound to the session id in a `ruleStore`
  outside the watcher (`bindSessionRules`), so a custom command with lists
  is classified and a start never depends on the watcher (`ruleStore.keep`
  prunes only ids an observe has SEEN live — a bind that lands between an
  observe's `List()` and its `keep` is not dropped; review fix); terminals and
  custom commands without lists are skipped; an invalid pattern →
  `SessionRulesWarning(tc)` (TUI status / web `warning` on the start reply)
  and the built-ins apply. `UseSessionManager` resets the watcher (it
  follows the manager); `UseStateTiming` and `UseSessionStates` +
  `NewStaticStates`/`PostNotice` are the test seams. Protocol words
  (`SessionActivity.Name()`): `working` · `idle` · `question` · `""`.
- **Config**: `ToolCommand.ScreenWorking/ScreenWaiting/ScreenQuestion`
  (`screen_*` lists, `HasScreenRules`), written by `renderToolCommand` as
  TOML basic strings (`tomlString`: `%q` would emit `\x`), NOT in
  `ToolFingerprint`, and carried over by `ReplaceToolCommandIf` when the
  new block brings none (a template upgrade keeps tuned lists).
- **Agents**: `AgentEntry.Activity/ActivitySince/Stalled`;
  `AgentScreen` → `AgentScreenResult{State, Text, Activity, Options}`;
  `gg agent list` prints the activity word after the state, `gg agent
  screen` leads with `activity: question (1. Yes · 2. No)`. Skill v130.
- **Web**: `sessionWire.agent_state/since/step_for/stalled/options`
  (`sessionsWireWith(list, tasks, lookup)`); `liveMsg.Notices`
  (`activityNoticeWire{id, kind, label, worktree, quiet_s}`) on the
  `sessions` event from `watchSessionStates` (own subscription + cursor from
  `NoticeSeq()` at start — nothing replayed). Page: `core.js` pure
  `activityLabel(s, now)` / `activityAttn(s)` / `noticeText(n)` (prepended
  to the sidebar/switcher/console pure-section tests via
  `activitySection`); narrow sub-rows REPLACE `running 12m`, the switcher
  rows and the console title APPEND (` · idle 3m`); `.attn` rows and the
  title's `.act.attn` use `--act-attn`; `live.js` toasts each notice unless
  `consoleFocusedId()` is that session.
- **TUI** (`session_activity.go`): `activityWatch`/`actSeq` on pointer
  fields (shared across the value copy), `waitActivityCmd` nil in quiet
  mode (golden screens never start the watcher), `onSessionActivity` →
  status line (newest wins; the focused console's session is skipped);
  `activityText`/`activityAttn`; sub-rows replace, popup rows and the
  console title append; `sessionDecorators` paints attention rows with
  `st().activityAttn` (the AttentionWarn colour as a foreground). i18n keys:
  `working %s`, `idle %s`, `needs input`, `stalled · %s`, `no output`, the
  three notice sentences, the rule warning, one help row.
- **Browser check**: `TestAttachBrowserHost` also hosts a Claude-shaped
  `sh` session (`AgentID: "claude"`: a rule + `❯` box, Enter → a numbered
  dialog, Enter → the box) under `UseStateTiming(0, 2 s)`; the Playwright
  script asserts the sub-row reads idle → needs input (attention colour),
  the toast, the console title and the switcher row — all seen failing on
  the unfixed build first.
- **Live verification 2026-10-03** (throwaway recorder, not committed):
  claude (trust dialog → question with cursor options), junie (trust →
  question; spinner → working; box → idle), kimi (trust → question;
  moon spinner → working; box → idle), codex 0.160.0 (update/trust dialogs
  → question; "Working (" → working; box → idle; the STARTUP box over "? for
  shortcuts" needed a rule), antigravity 1.2.15 (all new). Gotcha: the
  recorder's Enter accepted codex's "Update now" dialog — never send keys to
  a live agent whose screen you have not read.

### The TUI serves its own web page (2026-09-28)

Spec `docs/superpowers/specs/2026-09-28-web-hosted-in-tui-design.md`, plan
`docs/superpowers/plans/2026-09-28-web-hosted-in-tui.md`. The stage between
web attach plans 1 and 2.

- **Why:** an agent session is process memory (`domain.Sessions()`), and
  `gg web` was its own process, so the plan-1 browser console could only
  ever show sessions of its own process. Now the TUI hosts the page.
- **Composition (`cmd/gg/main.go`):** `tui.NewWebHost = func(svc) tui.WebHost
  { return web.NewHost(svc, domain.OpenTUI, true) }` beside `cli.LaunchWeb`;
  `internal/tui` and `internal/web` still never import each other. The
  opener matters: a service the hosted server opens ITSELF
  (`handleReroot`'s target) must take the TUI's ssh-batch runner, or an
  ssh prompt could land on the raw-mode terminal. `extractWebFlags`
  (`--web`, `--web-addr`, the `--cwd-file` pattern) → `tui.RunOptions`.
- **`web.Host` (`host.go`):** `NewHost(svc, opener, hosted)` → `Start(ctx,
  addr)` (refuses a FOREIGN live `web.json` when hosted — `ErrPageLive`
  wraps the other URL; standalone keeps replacing it), `Reroot(svc)` =
  `adoptService` (the post-swap tail lifted out of `handleReroot`: swap
  under `opMu`, drop `cur`/`feed`, `restartLive`, `touchMRU`,
  `rehomeSteerPresence`), `URL`, `OpenBrowser`, `Close` (announceShutdown →
  `http.Server.Shutdown(shutdownGrace)` → `srv.Close` → presence removed →
  the presence ticker's ctx cancelled). `Serve` builds its own server and
  runs `newHostOver(srv, false)` — `serveURLHook` is its test seam. The
  hosted server does NOT `applyUIPolicies` at start: the TUI already pushes
  them onto the shared Service (`load.go`).
- **Hosted rules:** `Server.hosted` → `/api/repo` `hosted:true`. The page
  keeps every switch affordance (palette `switch repo…`/`open repo (path)…`,
  ☰ Repositories, worktree `switch here`, locks' `go to worktree`;
  `hostedjs_test.go` forbids a `state.hosted` gate coming back). A hosted
  `POST /api/reroot` resolves + repairs + preflights as standalone, then —
  instead of `adoptService` — calls the TUI's switcher
  (`Host.SetSwitcher`): the terminal re-roots, its `webRerootCmd` moves the
  page, and only then does the switcher return, so the reply's repo info and
  the page's reload see the new Service. A refusal is a 409 with the TUI's
  reason ("the terminal is busy: …"); `ErrPageLive` is not a failure (the
  swap happened). No switcher installed → the old 409 "the terminal owns
  the current repository". `doReroot` (ops.js) raises the `#switching` veil
  (z 99, under server-down's 100; keys swallowed in capture) before the POST
  and drops it on any failure; success leaves it up until `reloadForSwitch()`
  (after which an aborted request cannot drop it). `adoptService` fans
  `{reason:"switched", worktree}` out on the OLD hub before `restartLive`
  closes it, and every hello carries `worktree`; live.js `followSwitch`
  veils + reloads on either (a hello naming another worktree than the tab's
  first). That is how a TUI-made switch reaches the page.
- **TUI (`webhost.go`):** `Model.web *webHostState` (host, url, starting,
  `pendingServe`, and the page-switch lane: `switches` chan + `stop` +
  `pendingSwitch` replies), `webOpts` (the flags); `startWebCmd` installs
  `switcherFor(w)` on the host; `onWebStarted` arms `waitWebSwitchCmd`;
  `onWebSwitchRequest` refuses on `steerRefusal()` (busy: op, modal, typing,
  any popup layer — the commit popup included) else `reRoot`s with status
  "switched from the web page" and parks the reply until `onWebReroot`
  answers it; `closeWeb` closes `stop` (the wait returns nil, late requests
  get "the terminal is closing"); `openInBrowser` (palette entry
  gated on `NewWebHost != nil`; first use starts, later uses reopen);
  `startupWebCmd` in `Init` when `[web] serve` or `--web`; `webAddr()` =
  flag > `[web] addr` > "" (random); `reRoot` batches `webRerootCmd`;
  `Run`'s tail calls `closeWeb` after the KillAll. Settings → **Web page**
  (`web_settings_popup.go`): URL, `Serve at startup` toggle
  (`config.SetWebServe`, GLOBAL file), `Address` field (`SetWebAddr`; ""
  = random), `Open in browser`. Steer `serve` (`steerServe`, before the
  busy refusal like `files`): OK+URL, or parks the command until
  `onWebStarted` answers it.
- **The Broadcaster (`agentsession/broadcast.go`):** `Subscribe() (<-chan
  struct{}, cancel)` — a 1-slot channel PER subscriber; `Signal` never
  blocks. `Manager`, `Session` and `domain.TaskManager` expose `Subscribe`;
  `Changed()` is GONE (a single shared channel let two readers in one
  process steal each other's wakeups). Subscription lifetime rule: a
  subscription lives on the owner's pointer state and is never re-made per
  wakeup — the TUI's `sessionWatch`/`taskTrack.current()` re-subscribe only
  when the process-global manager is swapped (tests), a console's `screen`
  subscription is dropped by `dropConsole` (EVERY `m.console = nil` path
  goes through it), the web producer cancels both of its subscriptions on
  stop. The web producer's 1 s removal poll is gone (manager signal →
  `Get(id)` → `gone`).
- **Browser check recipe (own tmux session, isolated XDG dirs, port 0):**
  `gg --web --web-addr 127.0.0.1:0` under tmux with `BROWSER=true`; read the
  URL from `web.json` under `$XDG_STATE_HOME` (never a log); start a
  terminal from the Branches `.` menu (`.`, type `term`, enter — the row is
  "Open terminal in <wt>"); `ctrl+]` steps out; playwright asserts
  visibility of the Agents row, the console prompt, typed output, the
  palette without switch rows, the worktree menu without `switch here`;
  quit = `q` then `Q` (the quit-mode popup's confirm key) → the page's
  `#server-down` veil. Unfixed install: no "Open in browser" in `ctrl+p`
  (tui-capture.sh), no `web.json`.

### Agent console: UTF-8 in OSC payloads (fix, 2026-09-24)

`x/ansi`'s transition table (`parser/transition_table.go`, Osc_string and
Dcs_string) dispatches on 0x9C (C1 ST) even when that byte is a UTF-8
continuation byte, so `ESC ] 0 ; ✳ Claude Code BEL` (✳ = E2 9C B3) ends at
0x9C and " Claude Code" is PRINTED at the cursor. Claude Code retitles on
every spinner frame, so the leak lands wherever the cursor is (the logo row,
the input box). `agentsession.oscFilter` (pumpOut only, stateful across reads)
drops bytes >= 0x80 inside OSC/DCS/APC/PM/SOS payloads. Found with
`GG_SESSION_TRACE`; the user's ConPTY trace is the regression fixture
`internal/agentsession/testdata/claude-conpty-title.raw`. Upstream fix would
belong in charmbracelet/x/ansi — drop the filter once a released x/vt parses
UTF-8 payloads correctly (the fixture test tells).

### Agent console: mouse pass-through, scroll mode, copy (2026-10-07)

Spec `docs/superpowers/specs/2026-10-06-console-mouse-scroll-design.md`.
Probe: Claude Code with `"tui": "fullscreen"` runs in the alt screen with
mouse tracking `?1000/1002/1003/1006h` (so x/vt has NO scrollback for it) and
copies its own drag-selection with OSC 52 — which x/vt ignores.

- **`agentsession`:** `Input()` = mouse tracking (bitmask fed by x/vt's
  `EnableMode`/`DisableMode` callbacks over modes 9/1000/1001/1002/1003) +
  `IsAltScreen` + emulator size; `SendMouse` (x/vt encodes per the child's
  mode, no-op without tracking) and `ScrollKey` (the alt-screen wheel's
  ↑/↓) touch neither LastUsed nor LastInput — a hover wheel must not reorder
  alt+a or look like typing to `agent_wait`/the report store. RIS (`reset`)
  needs nothing: x/vt's fullReset re-applies every mode through setMode, so
  `DisableMode` fires (pinned by `TestResetClearsInputModes`).
  `oscFilter` buffers OSC 52 (`clipCap` 1 MiB base64; over-cap → `Clip.Over`;
  `?` read requests and empty writes ignored) → `Session.Clipboard()` with a `Seq`.
  `History()`: scrollback lines are cloned by x/vt on push and never mutated,
  so the snapshot clones only the header slice (`slices.Clone(sb.Lines())` —
  NOT the live slice: eviction `slices.Delete`s it in place) + clones the
  screen rows; rows render lazily with `RowMarks` (reverse = selection,
  underline = cursor — cell-level because rows are the child's ANSI).
  `Text` skips wide right halves (zero cells), trims trailing blanks, joins
  rows with `\n` (no soft-wrap flag in x/vt).
- **TUI router** (`console_mouse.go`, first thing in `handleMouse`, ahead of
  its press-only gate): owner check = no modal/proc/menu/layer; box geometry
  `consoleRect` (maximised = (0,1) full body; else `pos[panelCommits]`),
  content = box + (2, 2). Tracking child → forwarded, clamped to the
  EMULATOR size (the PTY can differ from the box), `console.held` keeps a
  drag's release flowing when the pointer leaves the box. Alt screen without
  tracking → 3 × ↑/↓ per notch. Otherwise → scroll mode. Scroll mode is
  checked FIRST: once on, it keeps the mouse even after the program takes
  the mouse or the alt screen (user ruling 2026-10-07). A left press
  focuses (`focusConsoleByClick`); the wheel never does. A non-wheel press
  while something is held = a lost release: `endHeldMouse` sends the child
  the held button's release, drops a pending press, `finishDrag`s (copies)
  a scroll drag, then routes the press anew (outside the box `handleMouse`
  re-runs with the copy batched). Only the HELD button pressed again counts
  (another button is a chord); the release goes to `heldAt`, the last
  forwarded cell, so a selecting program does not stretch its selection.
- **Scroll mode** (`console_scroll.go`): `consoleState.scroll` holds the
  History, `top`, `cursor`, `lineSel`, `charSel`. Keys only while
  `consoleOwnsKeys()`; esc clears a selection first, then leaves; esc/q never
  reach the agent; any other key leaves and continues through
  `updateConsoleKey`. `syncConsoleSize` drops the snapshot when the PTY size
  really changes. "new output" = `LastOutput()` after `History.Taken()`
  (cheap gate) AND `History.Outgrown(s.Extent())`: Extent = scrollback
  length, rows to the screen's last non-blank one, which screen, and
  scrollback-full. A changed scrollback length (growth, or ED3 wiping it),
  a further-reaching screen, or a screen switch is new output; a spinner's
  redraw in place is not. At the 10k cap no length moves, so there any
  output lights it (the LastOutput gate alone).
  Rows are cut to the box with `ansi.Truncate` (`fitConsoleRow`, live and
  scroll): an unfocused console keeps a PTY size that can be wider —
  except one alt+a shows: `cycleSessions` syncs it to its box (the user asked
  to see it; x/vt does not reflow, so old lines stay cut). Clicks: `consoleClicks`
  (400 ms, same cell) — a double/triple click copies on the PRESS.
- **OSC 52 → clipboard:** `consumeConsoleClip` on every `consoleChangedMsg`;
  `clipSeq` is seeded at show time so a copy made before the console showed
  is never replayed; only a FOCUSED console copies (user ruling 2026-10-07)
  — unfocused, the write is spent (`clipSeq` advances), never copied on
  focus. Line count trims one trailing `\n`.

### Worktree terminal + GG_INBOX (AI tasks plan 1, 2026-09-25)

- **Open terminal** (Worktrees `.`): `domain.StartTerminal` runs the shell as
  argv (no `-c`): `[console] shell`, else `$SHELL`/`/bin/sh`, on Windows
  `pwsh` → `powershell` → `%COMSPEC%`/`cmd.exe` (`terminalShell`). Label
  `Terminal`; opens through `applyAgentStarted` like an agent.
- **Branches `.` too (2026-09-27):** `branchSessionRows` (agent_start_popup.go)
  offers `Start agent in <wt>` / `Open terminal in <wt>` on a branch that
  `worktreePathOf` (the row's own worktree-marker lookup, current worktree
  included) resolves — so the rows exist exactly where the row shows a
  worktree path — and hands the path to the same `startAgentFor` /
  `openTerminal`.
- **Session sub-rows under branches (v2, 2026-09-28):** `branchEntries()`
  (worktree_sessions.go) interleaves `brEntry{br, sess}` sub-rows after each
  branch whose `worktreePathOf` matches a session's `Dir` (sessions only, no
  ◆ tasks); `branchList` (viewstate.go) is entry-based like `worktreeList`
  (Name/Date = the parent's, Key = `name\x00id`, `Haystack` = the branch row
  + all its sub-rows so a `/` query naming a session keeps the branch —
  `worktreeList.Haystack` follows the same unit rule since the follow-up; the
  footer's `open-session` `[enter] open` binding is footer-only in the
  label-coverage gate like `remove-session`) and
  implements `parented` — `displayIndices` indexes the branch-filter slot's
  per-BRANCH `hidden` verdicts through `Parent(i)`, never by row.
  `backingIndex(panelBranches)` and `rowKeyAt` refuse/key sub-rows the
  Worktrees way. `selectedSession` dispatches on `m.focus`
  (Worktrees → `wtEntry`, Branches → `brEntry`), so the `x`/`enter`
  handlers, `sessionMenuRows`, `canRemoveSessionRow` and the `[x] resolve`
  yield need no per-panel gate; the Worktrees-only `Start agent` /
  `Open terminal` rows inside `sessionMenuRows` are gated to `panelWorktrees`
  explicitly because `selectedWorktree` reads the Worktrees cursor whatever
  the focus. Rendering: `branchOnlyRows` returns the 1:1 branch rows plus
  the indicator-gutter width; `branchRowsFor` indents `sessionRowBody` to it
  so `└` sits under the name (`branchRows()` keeps its no-arg shape for the
  tests that index it per branch).
- **`x` on a session sub-row (Worktrees tab, 2026-09-27):** `removeSessionRow`
  removes an exited session / refuses a running one; the key handler checks
  `selectedSession` BEFORE `canEnterConflict`, and the global `[x] resolve`
  footer hint yields on such a row (`canRemoveSessionRow` gates `[x] remove`).
- **`X` on a session sub-row (2026-09-28):** `killRemoveSessionRow` raises a
  `session-kill-remove` Kill/Cancel modal (default Cancel), Kill →
  `agentsession.Manager.KillAndRemove`: kill, then `Remove` once `Done` closes
  (a goroutine; the session stays listed and live-counted while it dies, so
  `KillAll`/the quit guard still see it — never delete from the map early).
  The row and any console docked on it go away through the ordinary
  `onSessionsChanged` removal path. On an exited row X is x. The `.` menu's
  Kill and remove session confirms the same way; `canKillRemoveSessionRow` gates
  `[X] kill+remove`. The `.` menu row is `session-kill-remove`.
- **GG_INBOX.** `StartSession`/`StartTerminal` take `env`; the TUI passes
  `GG_INBOX=<steerDir>` (`childEnv`, nil when steering is off) and records
  `childInbox[id]`. `cli.preferredInbox` sends every `gg session` verb (and
  `gg open`, which shares `sendSteer`) to a LIVE `$GG_INBOX` first; the seam
  is `sessionGetenv`.
- **Kept inboxes** (`steer_kept.go`): after a switch, gg keeps presence in
  each inbox a running child holds, drains it on the 1 s heartbeat (no
  watcher), never touches one another gg's live presence owns (PID check),
  and releases it when the last such child ends; `closeSteerInbox` spares a
  held inbox; exit releases all. `steer.Command.From` (json `-`, set by
  `Drain`) routes each reply back to the sender's inbox.
- **Worktree mismatch** (`steer_switch_ask.go`, spec ruling 9): commands carry
  `Worktree` (only the worktree-bound ones: a file/diff navigate, highlight —
  stamped where the CLI builds them). A bound command from another worktree
  is not applied: one-slot `m.steerAsk` → notice `steer_switch` (Switch to
  <b> and show / Ignore) and an immediate exit-1 reply. Accepting checks the
  target (`checkSwitchTarget`), `reRoot`s and replays the navigate through
  `m.startAtCmd` (consumed by `consumeStartAt`, Wait/Worktree/From cleared so
  it cannot loop). The ask is rebuilt by `rebuildNotices`, NOT filtered by
  `noticeSessionDismissed` (every action sets that id); reRoot clears it.
- **Foreign-notation worktree** (`sessionPlace`, switch_guard.go): Start
  agent / Open terminal run the switch guard's probe. A worktree git recorded
  under the other environment's notation (`/mnt/t/…` from Windows) runs in
  its TRANSLATED path via `StartSpec.Cwd` (process cwd) while `Dir` stays the
  identity (sub-rows, the delete guard group by it), with a status note that
  git there fails until repaired; an unreachable one refuses.

### AI tasks — the task core (AI tasks plan 2, 2026-09-25)

No UI in plan 2; see "AI tasks — the TUI" below.

- **Engine split.** `GenerateMessage`, `ReviewChanges`, `CompleteConflict` and
  the new `ConflictAgent` (whole-operation `conflict` agent; the name
  `ResolveConflict` was taken by the per-file resolve op) are
  `engine.CaptureTask`s: `Prepare(ctx, deps) (TaskInputs, error)` →
  run → `Collect(in, stdout) (Result, error)`; `Run` is that chain, so web
  and CLI are unchanged. `TaskInputs.Env` is a DELTA (op.Env + `GG_*`):
  `Run` prepends `os.Environ()`, and a session builds its own env and
  filters gg's host-terminal vars (`TMUX`) — a full env would re-add them.
  `Collect` = non-empty `$GG_MESSAGE_FILE` wins over stdout, CRLF → LF (now
  for every op). `Cleanup` is idempotent; a failing Prepare cleans up.
- **Keys** (`task_kinds.go`): `commit message — <wt> @ <HEAD7>`,
  `review — <a7>..<b7>` (hex runs ≥8 shortened; working changes →
  `review — <wt> working changes`), `resolve conflict — <wt> <op> <HEAD7>`,
  `resolve & complete — <wt> <op> <HEAD7>`; `<wt>` = the directory's base
  name in either notation. Builders: `CommitMessageTask`, `ReviewTask`,
  `ConflictTask(complete)`; they set `Parse` (commit →
  `ParseCaptureMessage` → "subject\n\nbody"; review/conflict →
  `ParseCaptureReport`) and `ResultOptional` for the conflict kinds.
- **What is a task** (`TaskModeOf`): `capture` → headless; `interactive`,
  or `terminal` with `per_file = false` → interactive; per-file terminal =
  mergetool (handover, not a task). `interactive` + `per_file` is invalid.
  `TaskChoices` groups a kind's commands by agent (`agentIDFor`, label from
  the catalogue; a custom command is its own agent).
  `EnsureInteractiveCommands` appends the safe interactive commit/review
  rows only when a category has no interactive row at all.
- `gg review` skips interactive rows; the web lanes are capture-only. (The
  TUI's `laneToolCommands` filter was retired in plan 3.)
- **Scheduler** (`domain.Tasks()`, process-global like `Sessions()`, holds
  the submitting `*Service` per task): FIFO per key (a queued task blocks
  later same-key tasks), global cap over running tasks of every mode,
  `SetMaxParallel` clamps 1..10 (`config.TasksConfig.Parallel()` gives the
  warning). Headless = `svc.Execute(op)` with an events channel feeding the
  tail (stderr lines + stdout, `taskhist.TrimTail`). Interactive =
  `svc.PrepareTask` (op's reservation) → `startLine` session (label
  `<agent> · <kind>`) → `resultWatcher` (filewatch wake + poll, a result
  must read identically twice 100 ms apart, sha256 dedup, one last read on
  exit). `Cancel`: queued → cancelled at once; running → ctx cancel
  (interactive kills the session). End table: ≥1 result → done; killed
  before one → cancelled; exit 0 without one → done only if
  `ResultOptional`, else failed.
- **History** (`taskhist`, `stateBaseDir("tasks")`, all repos): written
  when a task ENDS, before its end state is visible via `Get`/`List`
  (no orphaned "running" rows after a crash). `<id>.result` / `<id>.tail`
  beside `tasks.toml`; pruning at 50 deletes the files. A failed write
  switches the process to a `MemStore`; `TakeStoreProblem` reports it once.
- **Frontend hooks for plan 3:** `Subscribe()` (per-subscriber coalescing;
  was the one-slot `Changed()` until 2026-09-28), `List`/`Get`
  (`Results` counts results — apply each once), `Live()` (quit guard),
  `Load(key)` (the dialog's wait line), `History`/`HistoryResult`/
  `HistoryTail`/`RemoveHistory`.

### AI tasks — the TUI (AI tasks plan 3, 2026-09-26)

- **The TUI's `Tasks()` subscription** (`taskTrack.current()`; was the ONE
  consumer of the one-slot `Tasks().Changed()` until 2026-09-28): `waitTasksCmd` (in `Init`) →
  `tasksChangedMsg` → `onTasksChanged`, which re-arms it. `Model.taskTrack`
  (maps, shared across value copies; `ensureTaskTrack` for literal Models)
  remembers per task: results applied (`seen` vs `TaskInfo.Results`), end
  reported, the `GG_INBOX` handed over (copied into `childInbox` once the
  session exists, so `steer_kept` answers it), foreground (open the console
  once `Session` is set), ids `x`'d from the tab.
- **Launch dialog** (`task_launch_popup.go`): `openTaskLaunch(taskLaunch)`
  pushes the layer, then one async hop computes `svc.TaskKey` and (commit /
  review) runs `EnsureInteractiveCommands`. Rows = mode × command of the
  chosen `TaskChoice`; `launchChoices` drops `<user:…>` and `per_file`
  commands and mismatched `when_op`. `taskLookPath(commandProgram(cmd))`
  marks "not found". Choice memory: `promptstate.TaskLaunch` per kind
  (agent identity `id:<tool>` / `name:<cmd>`, mode, command name). Approval
  hashes `tc.Command`; conflict kinds show the template (the op resolves it
  after writing its context file). Submit builds the spec off-thread and
  sets `Env` (GG_INBOX), `Cwd` (`sessionPlace`), console size.
- **Result application:** `applyTaskResult` switches on kind.
  `canShowResult` = same checkout (`domain.SameCheckout`) + no proc + no
  focused console + no modal; otherwise a sticky `… ready — ctrl+\`.
  Commit message: open box → fill (empty) or `offer` (ask-before-replace
  sub-screen); else `pendingCommitMsg[worktree]`, consumed by
  `openCommitBox` (`c`). Review: `openReviewNote` (the note, `srcNote` viewer); a working-changes review keeps the `SaveTaskResult` viewer copy. Conflict:
  overview viewer (path "" — `e` is inert) + `loadCmd`; a conflict task
  ending done without a result also reloads.
- **Commit box:** foreground from the box closes it (the docked console
  gets keys only with no layer on top). Headless from the box waits on
  `genTask` (spinner; `esc` = `Tasks().Cancel`, `b` = close (not ctrl+b: tmux's prefix), result
  becomes pending).
- **Conflict window `t`:** `conflictPickerRows` → in-place rows (per-file
  mergetools and whole-op commands needing `<user:…>` input, which keep the
  old handover/overview path) + agent rows (`toolAgents`) that set
  `m.proc = nil` (the process preempts layer keys) and open the dialog;
  `cancelTaskLaunch` reopens the window for conflict kinds.
- **Headless tab** (`task_tab.go`, `sessionsPopup.tab`): rows =
  `List()` then history records not in it; history is cached on the popup
  (read on open and in `onTasksChanged`, never per frame). Opens on the
  tab when only tasks exist (also in quit mode).
- **◆ rows:** `wtEntry.task` (live headless only, `SameCheckout`);
  `wtEntry.sub()` replaces every "is a session row" check.
- **Quit:** `liveWork()` = live sessions + live tasks without a running
  session; `killAllAndQuitCmd` and the `run.go` safety net run
  `Tasks().KillAll` before `Sessions().KillAll`.
- **Tests** swap the global manager with `useTestTasks` (serial tests only;
  `NewTaskManager(nil)` keeps records in memory).


### Agent orchestration — wait + report (stage 3b, 2026-10-03)

Spec `docs/superpowers/specs/2026-10-03-agent-wait-report-design.md`.

- **`domain.AgentWait(ctx, caller, target, until, timeout)`** (`agentwait.go`)
  is a level-triggered long-poll, NOT a reader of the notice ring (notices
  are a human channel: held for the 30 s grace, no exit kind). It
  re-evaluates on every wake of `SessionStates().Subscribe()`,
  `Sessions().Subscribe()`, the report store's broadcaster and a 500 ms
  tick (the settle clock). Target "" = the caller's DIRECT children
  (`childrenOf`); an explicit id may be any descendant (`reach`).
- **Deliverable** = kind asked ∧ unread ∧ (idle/question only) the
  activity's `Since` is after the worker's `Session.LastInput()` (the start
  time while nothing was typed) ∧ (idle only) held ≥ `idleSettle` 2 s
  (`UseIdleSettle` in tests). Order: report > exit > question > idle; one
  event per return. An EXIT is delivered whatever `until` asked (a parent
  looping on `until: report` over a crashed worker would never end), and
  every result that names a worker carries `state` (running | exited). Timeout (default 45 s, cap 600 s) is a normal result
  `{timed_out, id?, activity, since}`; a ctx end is an error.
- **Delivery marks** live in the spawn registry beside the records:
  `marks[caller][worker] = {idleSince, questionSince, reportSeq, exit}`,
  advanced under the registry lock (two waiters never get the same event),
  pruned with the records. The loop checks `ctx` BEFORE evaluating, so a
  cancelled call consumes nothing; the go-sdk cancels the handler ctx on
  `notifications/cancelled` (a client ctx cancel sends it) but NOT on a
  dropped connection — hence `gg agent wait` cancels on SIGINT/SIGTERM
  (`waitSignalContext`), and a SIGKILLed waiter can still swallow one event
  (the level reads are the recovery). A parent that restarts is a new caller and sees
  pending events again.
- **Reports** (`agentreport.go`): `reports[full id]`, ≤ 20, `Seq` is
  process-global and monotonic; kept until the session is REMOVED (not
  until it exits), so "final report, then exit" is read as report then
  exit. `AgentReportVerb` refuses an empty/oversized (64 KiB) text and an
  exited session, stores the text through `cleanReportText` (no OSC/DCS/
  CSI/ESC sequences, no C0 but tab+newline, no DEL/C1 — it reaches the
  status line and terminals), signals the waiters and posts an `ActivityNotice{Kind:
  "report", Text: first line}` at once (no grace).
- **Row rule**: `domain.SessionReportOf(id)` returns the latest report
  while the session RUNS and `LastInput` is not after its `At` (an exited
  row says exited; nobody can answer it) — "a report is news until
  somebody talks to the worker" (level state cannot tell "idle after
  reporting" from "idle after the next turn"). Frontends add: a question
  wins. TUI `sessionBadge` / web `activityLabel`; final only changes the
  word (`done` vs `reported`). The wire carries `report_at`,
  `report_final`, `report_line` only while the badge shows.
- **`Session.LastInput()`** is stamped by `SendKey`/`SendText`/`Paste`,
  never by `Touch` — the human's console keystrokes count exactly like a
  parent's `agent_send`.
- **Known edges**: two dialogs back to back with no working stretch keep
  one `Since` (the second shows only in the timeout result / the screen);
  a worker that ignores a send is reported by the timeout. **Deferred to
  stage 4**: interactive AI-task agents have no session token
  (`runInteractive` uses `startLine`), so they cannot `agent_report`.
- core.js is an ES module: a helper used from another file must be in the
  export list AND that file's import (`reportLine` was caught only by the
  browser check; two `activityWiring` rows guard it now).

### Agent state pipeline (2026-10-05, refactor — no behaviour change)

The STRUCTURE of state detection since then. The stage sections below keep
their rulings, but name things that are gone — read them through this map:

| Gone | Now |
|---|---|
| `Rules`, `DefaultRules(id)`, `Compile(…)` | `Profile`, `ForAgent(id)`, `WithScreen(id, …)` |
| `Classify(rules, lines)` | `Screen.Read` (inside `Profile.Read`) |
| `SignalState`, `ClassifyWith`, `Signal{Title, Progress}` | `Title.Read` / `ProgressReport.Read` composed by `First`; `Observation{Title, Progress}` |
| `titleIdle`, `Signal.Trusted` | `Verdict.IdleHint` + `sessionTracker.animated` |
| `HasDefaults(id)` | `Known(id)` |
| `defaults`, `ownMenus`, `titleRules`, `progressAgents` | the `agents` table (`agents.go`) |
| `SessionRules(tc)`, `bindSessionRules` | `SessionProfile(tc)`, `bindSessionProfile` |
| `rulesFor` | `profileFor` |
| watcher maps `pendingIdle`, `pendingQ`, `progress`, `animated` | `sessionTracker` fields (`progress` → `stall`) |
| `agentstate.Progress(lines)`, `Reading.Progress` (stall key) | `agentstate.StallKey(lines)`, `Reading.StallKey` |
| `sessionTracker.dedicated` | `Reading.Dedicated` (copied from the `Profile`) |
| `SessionActivity.Settle`, `idleHold` | `SessionActivity.ReadyAt`, `readyAt` |
| `stateSource.Text` / `Signals` / `LastOutput` | `stateSource.Observe` |

```
StateWatcher.observe(now)          THE entry (domain/session_states.go)
  for each running session:
    profileFor(info)               bound Profile (screen_* block) or agentstate.ForAgent(id)
    src.Observe(id)                agentstate.Observation{Text, Lines, Title, Progress} + last output
    profile.Read(obs)              agentstate.Reading: Verdict{State, IdleHint, Spinning}
                                   + Options (Question), StepFor, StallKey, Dedicated
    tracker.Step(rd, …)            domain/session_tracker.go: trust, idle hold, grace,
                                   delayed question, both stalls → SessionActivity, notices
  → states (Get), notice ring, Subscribe  → TUI · web · agent verbs · agent_wait (ReadyAt)
```

| Where | What lives there |
|---|---|
| `agentstate/detector.go` | `Observation`, `Verdict`, `Detector` (the one interface), `Reading`, `Profile.Read`, `First` |
| `agentstate/screen.go` | `Screen` — Working → Waiting → Own (last line) → Question |
| `agentstate/signals.go` | `Title` (question/spinner decide, idle only hints), `ProgressReport` (OSC 9;4) |
| `agentstate/agents.go` | THE table: per agent id its signal parts + screen patterns; `ForAgent`, `Known`, `WithScreen` |
| `domain/session_tracker.go` | per-session state over time (`sessionTracker.Step`, `stateTiming`) |
| `domain/session_states.go` | the watcher: `observe`, source, profile binding, notices, run loop |
| `domain/agentwait.go` | delivery only: `now ≥ ReadyAt` and newer than the caller's input |

`First(parts…)`: the first part with a verdict decides and carries the
hints of the parts before it; later parts are not asked (a Codex "Action
Required" title carries no idle hint). `WithScreen(id, …)`: a known agent's
empty lists keep its built-ins, `Own`/title/progress stay; an unknown
command gets only its own lists; always `Dedicated`. `SessionActivity.ReadyAt`
= idle `Since + hold` (700 ms trusted hint / 2 s), others `Since`; a zero
`ReadyAt` (static watcher) → `Since + idleSettle`.

**Adding a signal:** write a `Detector` part (pure, reads the
`Observation`), add it to the agent's `signals` in `agents.go`; if it needs
new raw data, add a field to `Observation` and fill it in
`managerSource.Observe`. Timing belongs in the tracker, never in a part.

### Agent states from the title (2026-10-04)

Live capture under `script`, env as gg gives it: Claude Code 2.1.289 titles
a turn `◐ <topic>` / `◑ <topic>` (OSC 0, frames every 960 ms while it has
focus; the glyph stays ◐ without it) and an idle or a permission dialog
`✳ <topic>`; ≤ 2.1.227 used braille frames (herdr). Under a multiplexer —
`TMUX`, `STY` or `ZELLIJ` in its env, flag `tengu_static_title_under_mux` —
it is always `✳`, so `childEnv` strips all three. Codex 0.160.0: braille
spinner + ` | repo` while working, no glyph idle, `Action Required` when
blocked (herdr). Kimi Code 2.1.1: OSC 9;4 `4;3` working, `4;0` done —
but only when it believes its host terminal shows progress (Windows
Terminal `WT_SESSION`, ConEmu, ghostty, WezTerm in its env); elsewhere it
sends none and Kimi is read from the screen alone, as before.
Junie/agy send nothing usable.

- `agentsession.oscFilter` records the last OSC 0/2 title (full UTF-8,
  256-byte cap cut to valid UTF-8) and OSC 9;4 state from the raw bytes; it
  reads the OSC number first and buffers only 0/2/9 payloads (an OSC 52
  clipboard payload is never held). The emulator input is unchanged.
  `Session.Signals()` = `{Title, Progress}` (−1 = no report yet).
- `agentstate`: built-in `Rules.TitleWorking/TitleQuestion/TitleIdle` +
  `ProgressBusy` (no config lists; `SessionRules` carries them past a
  `screen_*` block like `Own`). `ClassifyWith` order (herdr): title
  question → title working / busy progress → the screen (`Classify`). An
  idle title never decides — Claude titles any waiting screen `✳` (a
  picker opened mid-turn, a dialog the rules miss), so it only confirms an
  idle the screen shows (`titleIdle`, trusted only).
- Guard: an idle title counts only once the same session has shown a
  working title/progress (`StateWatcher.animated` → `Signal.Trusted`): a
  title frozen at `✳` keeps the screen-only 2 s behaviour.
- Watcher: a titled idle holds `titleSettle` 700 ms, else `idleSettle` 2 s;
  `SessionActivity.Settle` is the hold the idle passed and `agent_wait`
  trusts the idle after it (0 → `idleSettle`). Stall rules unchanged — the
  title frames are output like the spinner timer; the 10-min
  spinner-only rule still catches a hung call.

### Agent orchestration — minors batch (2026-10-04)

The 3a/3b/4 deferred minors; the states rules (idle hold, spinner stall,
own menus) are in the session-states section above.
- **Wait timeout:** `waitTimeout(0)` = default; < 0 or > 600 s →
  `ErrWaitTimeoutRange`. MCP `timeout_s` is `*int` (left out = default, an
  explicit ≤ 0 refused); the CLI sends `timeout_s` only when `--timeout` was
  given. `gg agent wait` flags go through `flagIs`/`flagValue` (one or two
  dashes, `=value` or the next arg).
- **Report flags:** `--final` / `-F` only BEFORE the text, `--` ends them;
  `-F` reads through `io.LimitReader(MaxReportBytes+1)` and refuses past it.
- **Report notices:** `reportNoticeGap` 5 s per worker (`registry.noticed`,
  pruned with the reports; `UseReportNoticeGap` for tests); a final always
  posts; a held-back one still `Wake()`s the watcher's subscribers — the TUI
  files report tours and the rows repaint on those wakes.
- **Clocks:** `agentsession.stamp` keeps the monotonic reading (nanos since
  one process origin) for `LastInput` / `LastOutput`; `time.Unix(0, n)`
  would drop it.
- **TUI:** `actSeq` starts at `domain.SessionNoticeSeq()` (never starts the
  watcher); `startNote` joins the worktree note and the screen-rule warning;
  `consoleTitleFit` drops hints, then middle-cuts the worktree, then drops it.
  Tours: `fileBriefTour` / `fileReportTours` / `openTour` return
  `checkTourCmd` (CheckAnchors off the UI thread); `tourSeq` moves only on a
  successful file, `tourRefused` says a cap refusal once per report;
  `consoleSwitch.gen` (bumped by reRoot) tells openTour a switch it made from
  one already in flight.
- **Store:** `agentdocs.Store.tourMu` makes FileTour's look-up + add one step.
  `domain.tourTitle` keeps titles one line and ≤ `TourMaxTitle` (200, pinned
  to `agentdocs.MaxOverviewTitle`), middle-cutting label / worktree.
- **Templates:** `config.CarryScreenRules` — the offered upgrade block
  carries the block's screen_* lists, so the TUI popup and web preview show
  what will be written.
- **Browser host:** `TestAttachBrowserHost` also runs an "Other" worker in a
  second real worktree; the scratch playwright check opens its report and
  asserts the switch and the tour after the reload.
- **Left as documented:** a turn shorter than the 300 ms coalesce never shows
  working, so its idle is never fresh for agent_wait (the skill: a timed_out
  with `activity: idle` → read agent_screen); a SIGKILLed waiter can swallow
  one event.

### Agent orchestration — tours (stage 4, 2026-10-03)

Spec `docs/superpowers/specs/2026-10-03-agent-tours-design.md`.

- `domain.AgentTour(full, kind)` composes a tour (`AgentTourDoc{Key, Root,
  Dir, Title, Text, Seq}`): kind `brief` from the spawn record (a brief over
  `TourMaxBytes` = 64 KiB is cut at a line end + a closing line), kind
  `report` from the latest report (`Final report — …` when final). Titles
  are English data (`Brief — <label> · <worktree> (HH:MM of the start)`).
  `AgentTourKinds(full)` says which exist. domain does NOT import agentdocs;
  a tui test pins `TourMaxBytes == agentdocs.MaxOverviewBytes`.
- `agentdocs.Store.FileTour(key, root, dir, title, text)` keys an overview
  (`brief:<full id>` / `report:<full id>`): replaced in place while open,
  filed anew once the user closed it. `TourID(key)` = the open one.
- TUI: `onAgentSpawned` files the brief (`fileBriefTour`); every activity
  wake runs `fileReportTours` — per session, the latest report's seq vs
  `*m.tourSeq` (so a closed report tour comes back only with a NEW report;
  quiet mode files none). The cap refusal goes to the status line, the spawn
  answer is not affected. Open brief / Open report (`tourMenuRows`,
  `openTour`) re-file, then show at once on the current worktree, else
  `guardedReRoot(dir, false)` + `consoleSwitch.tour`, shown by
  `settleConsoleAfterSwitch` after the snapshot synced the overviews.
- Web: the session wire carries `has_brief` / `has_report`; `POST
  /api/agent-tour {id, kind}` files the tour INSIDE `listDocs` (+
  `ensureOpenID` for the served worktree) — the page focuses it right after
  the answer and the follow pass is asynchronous (a plain FileTour answered
  404 to that focus; found by the browser check). The answer carries
  `here` (store keys compared server-side — the page never compares path
  spellings, which differ on Windows). Another worktree's tour:
  `doReroot(path, carry)`; ONLY `reloadForSwitch` writes `sessionStorage
  gg-open-tour` (`switchCarry`), every failure arm (busy, refusal, cancelled
  or failed repair) drops it — a key written before the switch survived a
  refused switch and opened a stale tour on a later reload (review finding).
  `openPendingTour()` reads it at boot. The ctrl+\ switcher rows have no menu (planning ruling: ask first).
- `UseSessionManager` (test seam) now also installs a fresh spawn registry:
  ids restart at s1, and the old manager's records/reports leaked onto the
  new s1.

## Review view (structured reviews, 2026-09-28)

Spec: `docs/superpowers/specs/2026-09-28-review-view-design.md`; plan
`docs/superpowers/plans/2026-09-28-review-view.md`.

- **The document:** `notebatch.ParseReview` (unwraps a ``` fence or Claude's
  `{"result": …}` envelope; `version` 1, non-empty `summary`, exactly one
  range per note with 1 <= a <= b; every failure wraps `ErrNotReviewDoc`) →
  `ReviewDoc{Overview, Meta, Files[{Path, Summary, Meta, Notes}]}`; meta is
  key-sorted `MetaKV` text; `Canonical()` is the stored form.
  `engine.ReviewOutputInstruction()` is the brief's "Review output" section;
  `exttool.structuredReviewTask` is the one prompt every built-in review
  template carries (no double quotes inside — it sits in a quoted shell arg).
- **Storage:** `SaveReview` canonicalises; `Review.Doc` is parsed on read
  (nil = prose); `ReviewResult.Structured`.
- **Read time:** `ReviewRevs` (single commit vs range: base..tip resolved;
  a scope of `<commit>^..<commit>` is a single commit), `ReviewFiles`,
  `ReviewNotesFor(ctx, id, path, diff)` (read-only `review:<id>:<n>` ids,
  `<n>` = document order; a note fits when its range lies in the side's
  lines — no context hash: the view IS the reviewed commit),
  `ReviewFileCounts` / `ReviewOtherNotes` (`reviewSplit` reads each named
  file's two sides once). `model.IsReadOnlyNoteID` guards every mutation.
- **TUI:** `openReview(id, title)` is the ONE entry (async `reviewViewMsg`);
  prose → `openReviewNote` (the `srcNote` viewer, `openFile.markdown` →
  `loadMarkdownSrcCmd`). A single commit opens via `openChangedFiles`, a
  range via `openCompareFiles`; then `m.filesReview` is set and the lines
  are rebuilt by `reviewTreeLines` in the `commitFilesMsg` /
  `compareFilesMsg` handlers (a commit-list move clears the mode).
  `stampReviewNotes` (beside `stampPreviewNotes`, single file and stack)
  sets `diffView.reviewID` (carried by `inheritIdentity` and the late-stamp
  restore); `loadNotesCmd`/`stackNotesCmd` then read `ReviewNotesFor`;
  `diffNoteAddress` is false there, so no note gesture writes. The stack's
  first element is `stackFile{overview: true, prose: []mdRow}` spliced as
  `lineProse` lines (`diffLine.prose` indexes the row); it is never loaded,
  searched or counted.
- **Migration:** feature `structured-reviews`, `LegacyStore{review-commands}`
  probed by `legacyReviewCommandsPresent` (global + active repo config,
  `exttool.UpgradeReviewCommand` over each `review` command), consent-only
  action `upgrade-review-commands` → `config.ReplaceToolCommandBodies`.
  `UpgradeReviewCommand` matches the GENERATED rendering (either OS, any
  binary, quoted when it has a space) of every template in
  `exttool.SupersededReviewCommands` — when a review template changes again,
  move the old text into `review_upgrade.go` and add a pair.
- **Polish (2026-09-28):** the overview popup is `noCursor` (a pager);
  `o` pushes `reviewOtherNotesPopup` (enter → `openFileAtCommit`). Deleting
  (`review_delete.go`): `deleteReviewRow` (Branches review sub-row, or the
  review view when the files view is front) and View all notes' `ctrl+d`
  (`allNotesDelete`, reviews and note threads) → `confirmStoredDelete`
  (decision id `review-remove`, Cancel default) → `svc.NoteRemove` →
  `storedDeletedMsg`: `leaveReviewView` (the view's esc, shared) when its
  review went, re-read of an open All notes, `srcNotes` reload. The brief's
  "Review output" asks for `## Summary / ## Findings / ## Verdict`; the
  templates only point at the brief, so changing the brief needs no
  `SupersededReviewCommands` entry — only a template text change does.
- **Test isolation:** domain, cli, mcp and web `TestMain`s pin
  `XDG_CONFIG_HOME` — preflight reads the global config, and a
  `gg migrate --yes` test once rewrote a developer's own.
- **Web (2026-09-29, `internal/web/reviews.go`, `static/reviews.js`):**
  `/api/notes/counts` carries `reviews` (heads → a branch's `li.brev`
  sub-rows, matched on `branch` + tip hash prefix); `/api/commit/{sha}`
  carries the commit's `reviews` (read on an explicit open only);
  `/api/review/{id}` is the whole view (files, counts, summaries, overview
  markdown tree, meta, other notes, raw text; 404 when gone);
  `/api/review/notes` rebuilds the SAME Differ request `/api/diff` makes
  for the review's revs and returns `ReviewNotesFor` (wire `read_only`: set
  by `ToWireNote` for every `review:` id). The page does NOT add a files
  mode: `state.review` overlays commit mode (tip) or compare mode
  (base..tip, `previewBar` for the bar) and is live only while
  `state.files` / `state.compare` is the object it opened
  (`reviewActiveIn`), so every other open lapses it untouched. Its note
  lane (`ctx.review`) is exclusive and `c` refuses. Review rows carry
  `data-review` and never `data-i`/`data-n`. No n/p (web `p` = pull).
  Delete = `POST /api/notes/remove` behind `showLocalConfirm(…,
  ["cancel","delete"])`.

## Note store partitions (2026-10-04)

Spec: `docs/superpowers/specs/2026-10-04-notes-partitions-design.md`.

- **Files** under `$XDG_STATE_HOME/gg/notes/<repoKey>/`: `commits.toml`,
  `previews.toml`, `shelf.toml`, `worktrees/<key>.toml` (key = first 8 bytes
  of sha256 over the cleaned top-level, hex), `worktrees/unscoped.toml`.
- **Routing** (`notes.PartOf`, a ROOT note): shelf id → shelf; no commit
  (domain's `worktreeScopedNote`) → the worktree's part, "" → unscoped;
  `Preview` containing `...` → previews; every other committed note
  (plain, commit/branch reviews, pair `a..b` notes, PR notes) → commits.
- **A reply goes to its parent's part**: replies carry no `Preview`, so
  `PartOf` alone would send a preview note's reply to commits. `Put` looks
  the parent up; when the parent is found nowhere AND a part was unreadable,
  `Put` returns that read error instead of guessing. `Put` of a stored id
  whose part would change → `ErrPartChange`.
- **Per part**: lock (`<file>.lock`), process mutex, `max_entries` cap,
  quarantine (`FileStore.Quarantine` moves only corrupt parts). An emptied
  part's file is deleted. Temp files are `.notes-*.tmp` (never `.toml`, so a
  concurrent `Parts()` listing cannot mistake one for a part).
- **Readers** (`internal/domain/notes_parts.go`): live address → own worktree
  part; commit address → commits + previews (a commit's notes include the
  scoped ones, `PlainNotes` filters later); `loadPreviewNotes` by scope
  (`...` → previews, `a..b` → commits, PR "" → both); `NoteCounts` /
  `NoteAddresses` / `NotesOverview` → the visible parts (shared + own
  worktree); reviews + the branch delete/rename follow-up → commits. Ids,
  `NewID` and the sweep use `LoadAll`.
- **Sweep**: a removed worktree's live notes resolve orphaned (no file to
  read) and are dropped; the emptied part file is deleted. There is NO
  `git worktree list` rule: git names the MAIN checkout of a submodule or a
  `--separate-git-dir` repo by its git dir, not its top level, so such a
  rule dropped every note of that checkout (review finding, removed;
  `TestSweepKeepsNotesOfASeparateGitDirCheckout`). `FileStore.Sweep`
  continues past a corrupt part and joins the errors.
- **Migration** `split-notes` (`FeatureNotes`, `Silent`, `Lossless`,
  `LegacyStore{StoreNotes}`): `FileStore.ConvertLegacy` holds
  `notes.toml.lock`, routes replies with their roots, merges by id (newer
  `Updated` wins, uncapped), renames to `notes.toml.migrated-<unix>`.
  Idempotent: an older gg that recreates `notes.toml` is merged on the next
  open; a conversion that waited on the lock and finds the file gone is a
  no-op. A corrupt legacy file is moved to `notes.toml.corrupt-<unix>` and
  reported (`ErrCorrupt`). Besides `RunAutoMigrations`, every lazy store
  resolution converts (`openNotesStore`): a TUI/hosted-web repo switch and
  `gg mcp` never call `RunAutoMigrations`.

## Review notes (AI reviews stored as notes, 2026-09-27)

Spec: `docs/superpowers/specs/2026-09-27-review-notes-design.md`.

- **A review note** is a `model.Note` with `Address{State: StateCommitted,
  Commit: <full sha>, Path: "", Branch: <name or "">}`, `Tags: ["review"]`,
  `Source: agent`, `Author` = the tool name, `Scope` = the reviewed hex range,
  `Summary` = "Review: <branch or label> (<a7>..<b7>)", `Rationale` = the text.
  `Note.IsReviewNote()` (commit-level + tag) is the ONLY test: `Address.Branch`
  alone proves nothing — the TUI fills it on working-tree line notes.
- **Kind** (`ReviewNoteKind`) is computed on read: `ReviewOnCommit` (no
  branch), `ReviewOnBranch` (the branch still points at the commit),
  `ReviewWasTip` (it moved or is gone). The Branches tab lists current-tip
  reviews only.
- **Store rules:** commit-level notes skip the entry cap (`capOldestFirst`),
  the age limit and the resolve sweep (`sweepNotes`).
- **One writer:** `domain.SaveReview` — randomized retry on `filelock.ErrHeld`
  (15 s budget, 500 ms–2 s jitter; seams `reviewSleep`/`reviewJitter`/
  `reviewClock`/`reviewPutHook`), `notes.ErrCorrupt` → `FileStore.Quarantine`
  (moved to `notes.toml.corrupt-<unix>`) once, then a fresh store. No file
  fallback anywhere.
- **Read model:** `Review`, `Reviews`, `ReviewsForCommit`, `ReviewsForBranch`;
  `NoteCounts.Reviews []ReviewHead` feeds the TUI rows; `NotesOverview` puts
  them on `NoteCommitNotes.Reviews`.
- **Tasks:** `TaskSpec.Store` (bound by `ReviewTask` for non-working targets)
  runs in `setResult`, outside the manager lock; `TaskInfo.NoteID/SaveErr`,
  `taskhist.Record.NoteID`; a stored task writes no `.result`; a failed save
  fails the run and `RetrySave` (tasks tab `s`) redoes it from memory.
- **Branch follow-ups:** `Execute` runs `reviewsFollowBranchOp` after a
  successful `DeleteBranch`/`DeleteRemoteBranch`/`RenameBranch` — AFTER
  releasing the op's reservation (resolving the default note store takes a
  Read reservation; inside, it deadlocked). The TUI maps those ops to
  `srcNotes` too.
- **BranchReviewTarget** fills `Commit` and `Branch`; `HEAD` resolves to the
  checked-out branch, a sha or tag leaves `Branch` empty.
- **TUI:** `srcNote` viewer source (`openReviewNote`); `withReviewLines`
  prepends a "Reviews" heading + one row per review (`contentLine.noteID`, NO
  path since 2026-10-05 — it used to carry a virtual `@notes/review-….md` path
  + status R, which made every path-keyed file action (copy, shelf, bookmark,
  compare, the session snapshot) treat the review as a file). Branches tab is entry-based
  (`brEntry`, the Worktrees sub-row pattern): the branch filter maps entry →
  branch, `backingIndex` refuses a review row.
- **Shelf-entry notes in a shelved set's list:** `withShelfNoteLines` prepends
  a "Notes" heading + one row per entry-level note (`contentLine.shelfNote`,
  NO path — so every path-keyed file action skips it without a guard; enter
  is let through explicitly and opens `openShelfNotes` for that one note).
  `shelfFilesMsg` carries the notes (`loadShelfFilesCmd` reads `ShelfNotes`
  beside the members; a failed read only drops the section) and
  `Model.filesShelfNotes` keeps them. `buildTreeStack` turns each row into a
  prose `stackFile` (the review-overview element, `overview` + `label`), laid
  out once at the stack width. Summaries cut via `elideNoteSummary`: the
  trailing "(branch)" group stays whole (a branch holds slashes — elidePath
  alone left "…/x)"), then the worktree name when even the group cannot fit.
- **Web half:** `openShelfEntry` (sidebar.js) reads `/api/shelf/notes` when
  `e.notes > 0` and hands them to `openEntryCompare` (`shelf_notes`,
  `shelf_entry`, `shelf_label` → `state.compare.shelfNotes`/…).
  `shelfNoteRowsHTML` (files.js) prepends the heading + `li.shelfnote` rows
  carrying `data-note`, NEVER `data-i` — `state.files`, the cursor, staging
  and the context menu are index-keyed and never see them; the list click
  opens `openShelfNotes(entry, label, noteId)` filtered to that note.
  `elideNoteSummary` is ported into core.js's path-elision section (its node
  test pins the TUI's exact outputs). The stack's `stackNotesHTML` leads the
  `.stk` with a `.stk-notes` block (not a `.stk-file`, so the loader, counts
  and n/p skip it); `scrollToFile` lands file 0 at scrollTop 0 when that
  block exists, else aligning the header scrolls the notes out of sight.

### Open-file notes (temporary agent remarks, 2026-09-30)

Spec `docs/superpowers/specs/2026-09-30-open-file-notes-design.md`.

- **Memory only.** `openFile.notes` (`internal/tui/open_file_notes.go`); never
  `internal/notes`. Working-tree documents only. `addNote` sets
  `backgrounded` (esc steps aside, X closes and drops them) and
  `openFilesReg.touch` never evicts a document with notes (the list may grow
  past 20).
- **Virtual rows.** Note boxes are NEVER in `contentPopup.lines`: every line
  index (cursor, selection, search hit, pending line) stays a file line.
  `renderPreviewBox` emits the box rows under the line a note ENDS on
  (`fileNoteRow`, painted by `noteBoxCell` — the diff view's box). The only
  row math that knows about them is `contentPopup.extraRows` +
  `rowsSpan`/`clampTop`/`lastVisible`/`ensureCursorVisible`; with the hook nil
  (no notes) the pager is the plain one-row-per-line arithmetic. New preview
  scroll code must clamp through `p.clampTop`, not `previewClamp`.
- **Tall boxes.** A box takes at most `noteBoxMaxRows(rowsCap)` rows (half
  the window); the rest is "… N more lines" and `enter` opens the whole note
  (`openFileNote`). The pager's unit is a file line, so an uncapped box could
  never be scrolled through. Scroll sites go through `p.scrollBy` (a page
  never skips lines the boxes left no room for).
- **Cross-file `}`/`{`** walks annotated files by `openFile.seq`, never the
  MRU list (`bringToFront` reorders it on every step).
- **`note_add` reads the disk first** unless `docCurrent` (one stat) says the
  loaded lines are still what the disk holds.
- **Gutter.** While a document has notes its lines give up `noteGutterW` (2)
  columns (`winOpts.prefixW`); covered lines carry `│ `. `activePreview`
  subtracts it from the width search/pan use.
- **The store.** Notes live in `internal/agentdocs` (spec
  `2026-10-01-agent-docs-web-design.md`), filed under
  `domain.CheckoutKey(<toplevel>)` + path. `tui.New` holds
  `agentdocs.Shared()`; a page the TUI hosts (`web.NewHost(…, true)`) holds
  the same one, so both show ONE set (same `t<n>`, a dismiss anywhere is a
  dismiss everywhere). A document keeps a COPY (`openFile.notes
  []agentdocs.Note`) that View draws; `agentDocsChangedMsg` (one
  subscription, `agentdocs_track.go`) re-reads the copies. Open-file ids
  come from `Shared().NextFileSeq()` (the hosted page's list uses the same
  counter).
- **Re-anchoring** (`agentdocs` `reanchor`, via `Store.Align`): the Model
  aligns the store to a working-tree load's lines BEFORE `fill`
  (`alignDocNotes`); `fill` then `syncNotes`. A live note follows the
  `textdiff` alignment when all its lines survived unchanged and contiguous,
  else it goes `outdated` (numbers kept, clamped). An outdated note returns
  only when its anchor text is at its old place or at exactly ONE place in
  the file. `Align` is idempotent per content fingerprint; a document whose
  lines are not the content the notes sit on (the browser read a newer file)
  does not adopt them and is re-read (`syncNotes` → stale).
- **Steer.** `note_add|list|show|rm` run before `steerRefusal` (they never
  move the screen); `note_add` on a file that is not open/loaded rides
  `noteLandedMsg`. The CLI (`gg session note`) posts to the TUI when one is
  live, else to gg web (`steerLive(both=false)`). `--to tui|web` (cut from
  the args by `runSessionIn`'s `cutTo`, carried in `sessDir.to`) restricts
  `sessDir.target()` to one side; `$GG_INBOX` wins only while that side is
  live there (`preferredInboxFor`). `status` refuses `--to`, as does a
  `--to` before the verb (`-to` too) or given twice. Past `--` nothing is a
  flag: `cutTo` leaves a `--to` there and `parseSteerFlags` hands everything
  after `--` out as positional (a file named `-x.go`).
- **Web side** (`internal/web`: `agentdocs_follow.go`, `steer_notes.go`,
  `file_notes.go`). The server keeps its own store unless hosted. A follow
  goroutine (started by `Host.Start`, also run by `adoptService`) lists every
  noted file in the page's list (`ensureOpen`, background) and pins it
  (`ofEntry.pinned`: never evicted; a plain `close` op — esc — backgrounds
  it; `everywhere` — x — clears its notes), then fans out
  `Reason: "agentdocs"`. `/api/file-content` aligns + returns `notes`
  (working tree only) — the page never pairs notes with lines from another
  read. `POST /api/file-notes {op:"dismiss"}`. `handleSteer` answers the
  note verbs BEFORE `toSteerWire` (its 400 "unknown command") and the 409.
  The web root is `TopLevel` (cached per service), never `svc.Root()`.
- **Known limits.** A TUI plus a SEPARATE-process `gg web` on one worktree:
  the verbs go to the TUI and that page shows none. Line numbers are over
  canonical lines (`agentdocs.Lines`, the TUI's split: CR and CRLF break
  lines); the web viewer splits only on LF, so a file with bare CRs shows
  its boxes off by the CRs.


## Worktree inventory, claims and guards (agent orchestration stage 1)

Spec `docs/superpowers/specs/2026-09-30-worktree-inventory-design.md`.

- **Guards.** `wtguard` (leaf) is the one interface; domain providers in
  `domain/wtguards.go` (one per source: missing, main, detached, paused-op,
  git-lock, reserved, claimed, tui, session, dirty-recent); the ONLY place
  naming them is `domain/wtguard_set.go` (`WorktreeGuardSet` hook — a
  composition root may wrap `StandardWorktreeGuards`; `GuardSources` holds
  the unexported `liveView`, so only domain builds sources). `wtguard.Run`
  asks cheap guards first and the expensive `git status` guard only when
  nothing blocked; a skipped guard's fact is present and nil.
- **Hard vs overridable.** Hard: missing, paused-op, git-lock,
  status-failed, check-failed. The rest are overridable in recycle via ONE
  `recycle.blocked` decision (`recycle anyway`/`abort`; CLI `--force`),
  EXCEPT `dirty-recent` and `detached`, which recycle handles natively
  (`recycle.dirty`; a detached target by design). A claim has no override.
- **GuardReport runs inside ops** (`OpDeps.Guards`): it must never take the
  repo gate — it reads `s.repo.Worktrees` and `AgentsConfigFrom(wts)`, not
  `s.Worktrees`/`s.AgentsConfig` (gated: the op would wait on itself).
  `TestRecycleThroughExecuteAsksTheGuards` pins it with a 5 s context.
- **Sessions across processes.** `GG_SESSION_ID = <ProcTag>/<id>`
  (`agentsession.ProcTag` = `<pid>-<start unixnano>`); each TUI publishes
  `<state>/gg/sessions/<ProcTag>.json` (`domain.PublishSessions`, started in
  `tui.Run`). A session is dead iff its proc's registry is live and does not
  list it running, OR no live registry exists for its proc AND the pid is
  gone — a stalled TUI (suspend/resume) keeps its claims.
- **Claims** live in `<worktree git dir>/gg-claim` (O_EXCL); every
  create/sweep/release runs under `filelock` on `gg-claim.lock` and re-reads
  before removing (`sameClaim` is field-wise with `time.Equal`). An empty or
  unparsable claim file is a crashed claimer's once it is 10 s old. Only a
  running gg session may claim (`SessionNotLiveError` → CLI exit 2).
- **Config.** `[agents] reserved` is REPO-only (`Load` drops the global
  layer's list) and anchored on the MAIN worktree via
  `Service.AgentsConfig` — the TUI must not use `m.repoConfigPath`, which
  follows the cwd worktree's committed `.gg.toml`.
- **Tests** that reach the registry must pin `XDG_STATE_HOME` (domain, cli
  and tui TestMains do): `sessionreg.Live` deletes stale files.

#### Overview documents (`overview*.go`, `steer_overview.go`)

Spec `2026-09-30-agent-overview-documents-design.md`. An overview is an
`openFile` of kind `srcOverview` carrying `ov *overview` (text, anchors,
selection, laid-out width); never on disk, never evicted, created
`backgrounded`.

- **The store owns it** (since agent-docs web plan 2): text, title, anchors
  and their `Missing` flags live in `agentdocs` (`AddOverview(root, dir, …)`
  — the key plus the worktree on disk the anchors are stat'ed under —
  `SetOverview`, `RemoveOverview`, `CheckAnchors`, `SetAnchorMissing`, the
  limits and their errors). The overview's `f<n>` IS the store's
  (`NextFileSeq`); `newOverviewDocFrom(o)` builds the TUI document under it.
  The TUI keeps a COPY (`d.ov.text`, `d.title`, `anchor.missing`) plus its
  own layout, selection and `from`; `syncOverviews` (in `syncAgentDocs`, on
  every store change and a worktree switch) re-lays out a changed text
  (`adoptOverview`, selection kept by dest), paints new missing flags, adds a
  store overview the list lacks (background), and closes one that left the
  store — on screen with `overview f7 was closed in the browser`. X
  (`closeDoc`) removes it from the store.

- **Anchors come from the one parser.** `markdown.ParseWith(src,
  Options{Anchor})` turns a link whose destination the caller accepts into
  `InAnchor` (`Parse` never does, so forge/web rendering is untouched).
  `agentdocs.ParseAnchorDest` is the grammar: `path`, `path:N`, `path:N-M`,
  `note:t<n>`; absolute paths, `..`, schemes and drives are not anchors.
  `agentdocs.ParseOverview` numbers the anchor inlines (`Text` = index) and
  turns links past `MaxAnchors` into text; the TUI and the web share it.
- **gg lays the rows out itself** (`overviewLines` at the viewer's reading
  column): one display row = one line, so a click maps to a line and a rune.
  Spans are recovered after wrapping through TEMPORARY classes
  `mdAnchorID0+k` (the class mask is `uint8` → at most 100 anchors), rewritten
  to `mdAnchor`; `paint` re-classes Sel/Gone. `renderPreviewBox` re-lays out
  when the width (`ov.w`) or the view (`ov.mode`) changes, keeping the
  selected anchor. Prose wraps at the column; code and table rows
  (`mdRowsWide` leaves `pre` rows whole) follow ctrl+w like every TUI text
  (`overviewView`): `modeScroll` (default) keeps them whole — the renderer
  pans by `hscroll`, `selectAnchor` pans to the anchor (`panTo`), a click adds
  `hscroll`; `modeCutoff` cuts at width-1 + "…", so an anchor past the cut has
  no span and tab skips it (the user chose to cut it); `modeWrap` splits the
  row into width-wide rows, so every anchor has a span. A click selects
  with the cursor on the CLICKED row (`selectAnchorAt`): nothing scrolls under
  the double click's second press. A label over a line break flattens with a
  space (`mdFlat`, `agentdocs.flat`).
- **Open/back.** `openAnchor` stats a path anchor's file OFF the UI thread
  (`anchorStatMsg` → `anchorStatted`, which acts only if that overview is
  still in front and the anchor's dest unchanged), then opens the file (`openFileViewerEv`, a range
  via `pendingEnd` → a fixed `lineSel`) or brings a note's file to the front;
  the file gets `from` (latest jump wins) and `backgrounded`. backspace
  (`anchorBack`) backgrounds it and `bringToFront`s `from`; when `from` is no
  longer in the list it says so and clears `from`. `overview_set` keeps the
  selection by `dest`.
- **Viewer-wide side effects.** The full-screen viewer draws `m.statusMsg` on
  its TITLE line (right side; not while `m.running`, not a `stickyMsg`) for
  every document — it covers the status bar. `landPendingLine` pulls the box
  of a note starting at the landed line into view, as `}` does.
- **Steer.** `overview_add|set|list|show|rm` run before `steerRefusal`; an add
  the screen cannot take lands in the background. add/set/show/rm refuse a
  caller in another worktree (the CLI sends `Worktree` for all four; gg web
  checks the same in `steerOverview`). The CLI refuses a terminal on stdin
  (`readerIsTerminal`) instead of waiting on it. add/set have the store
  check the anchors off-thread (`CheckAnchors` in a cmd → `anchorsCheckedMsg`)
  and answer with the unresolved ones. `steer.MaxCommandBytes` is 512 KiB for
  the 64 KiB text. The CLI posts to the TUI when one is live, else to gg web.
- **Web side** (`internal/web`: `agentdocs_follow.go`, `overview_http.go`,
  `steer_overviews.go`; page `viewer.js` document mode). The follow pass lists
  every overview of the served root under the STORE's id (`ensureOpenID`,
  key `{overview, overview-<seq>.md}`, pinned, title kept current) and
  removes entries whose overview left the store, naming them in the
  `agentdocs` fan-out (`closed`) so a tab showing one closes its viewer.
  `GET /api/overview?id=` runs `CheckAnchors` in the request and serves the
  `ParseOverview` tree plus anchor rows (a live note anchor carries its
  note's path/lines — the page has no note index). The verbs are answered
  before `toSteerWire`; a foreground add has the tabs `file_focus` it, or
  lands in the background while an op is in flight (`(operation in
  flight)`). x on an overview entry → `RemoveOverview`; esc backgrounds it.
  The page: `markdown.js` paints `anchor` as `<a class="md-anchor"
  data-a="k">` only with `{anchors: true}` (plain text otherwise); tab /
  shift+tab select, enter or a SINGLE click opens (after a re-fetch that
  re-checks), a range tints `.vline.vrange`, backspace (`view.from`, one
  step, per tab) comes back with the anchor selected — a failed list fetch
  there says `back failed` and keeps `view.from` (`backOutcome`). The
  backdrop leaves a document as esc does (`escHow`). **Freshness:**
  `Store.OverviewStamp` fingerprints title, text, missing flags and every
  anchored note's place; `/api/overview` returns it (taken BEFORE the copy)
  and the `agentdocs` fan-out carries `stamps`, so a tab re-fetches only
  when its overview's stamp moved — a file deleted on disk shows as gone on
  the next anchor click or reopen, not on an unrelated store change.
  **Turns:** `followDocs` holds `followMu`; a steer's own add + listing
  runs in the same turn (`listDocs`), and what its listing pushed out over
  the cap is fanned out (`evicted`) by the pass that follows, so the agent's
  reply and every tab name it (a recorded eviction carries its worktree; a
  pass over another drops it). A web `note_add` lists the file — and takes
  its baseline stat — inside that turn, only after the store took the note.
  Eviction lines word the cap the server sends (`cap`, `evictedText`); an
  overview refresh's answer drops only when a newer answer already applied
  (`ovSeq`/`ovApplied`, `ovAnswerApplies`) — a newer refresh that merely
  started may fail, and then the older answer still lands; a refresh whose
  fetch fails answers with the last one started (`ovLast`, `ovLastSeq`) when
  a newer one is under way, else false — never with itself. `openAnchorAt`
  reads that answer: false opens nothing and says `could not re-check the
  overview — try the anchor again` (fail-closed: no anchor opens on the list
  from before the click). An open's own overview fetch takes a place in the
  same order (`ovMine`) and counts as applied in `showOverview`, so a refresh
  started before it drops when it lands later — and when a refresh started
  after it landed first, the re-open keeps that newer text on screen. The selected anchor's
  destination (`view.ov.want`, set by `selectAnchor`) outlives a refresh
  that drops it: `keepAnchor(next, want, sel)` selects it again when a later
  text brings it back. A failed open the server already moved the tab to
  gives the server back what the tab shows (`releaseAfterFailedOpen`: focus
  the file still on screen, else background), so `gg session files` never
  says `shown` for an overview no tab could load; the steer reply's `shown`
  still means "the tabs were told". **Browser Back:** an anchor's open
  pushes ONE history entry (`ownBack`, `{gg: "back"}`, re-read from
  `history.state` on load); `popstate` runs `anchorBack` when the file came
  from an anchor; backspace leaves the entry for the next open, so a Back
  after backspace came back is used up instead of leaving gg web (`popstate`
  re-reads `ownBack` from `history.state`, so a Forward that restored the
  entry is reused, never doubled — `armBack`); a Back while a popup sits on
  the file (`topLayer()` is not the viewer) leaves both alone and re-arms,
  except an agent console, which Back closes (re-armed: the next Back comes
  back); a Back whose list fetch or overview open failed re-arms with
  `view.from`. An open that did not land — failed, or left by esc
  meanwhile — reports what the tab shows (`reportShown`, unless a newer
  open started: `openSeq`, which a close does not bump); that report, a
  close, a background and a failed Back's re-focus go through `ofQueue`,
  one after another on `ofSync`, and the next open awaits them.
  The switcher's x on a file not on screen records it (`viewerClosedFile`
  → `closedAt`, the loadSeq at the close), so an open of it still loading
  here drops. An anchor whose re-check lost a race to a re-open or a close
  (`loadSeq` moved) says nothing, whatever the re-check answered; one the
  re-check removed says `that anchor is no longer in the overview`. The
  document's type grows with a large window (`.vdoc` font-size clamp
  12–17px; the 100ch column grows with it).

## Agent spawn (agent orchestration stage 2)

Spec `docs/superpowers/specs/2026-10-01-agent-spawn-design.md`, plans A
(core) and B (reach) under `docs/superpowers/plans/2026-10-01-agent-spawn-*`.

- **Channels.** Inbox/registry FILES are discovery only (`sessionreg`
  `Registry.MCP`, `steer.Presence.MCP` = the URL, never a token). The MAIN
  channel is MCP served by the TUI process: `mcp.AgentHost` (loopback
  `127.0.0.1:0`, `/mcp`, streamable HTTP) behind go-sdk's
  `auth.RequireBearerToken`, whose verifier calls `domain.VerifyAgentToken`
  on EVERY request (`TokenInfo.UserID` = the caller's full session id;
  expiration must be non-zero). Any `Origin` header → 403. The TUI owns it
  through the `tui.AgentHost` seam set by `cmd/gg` (`agentHostAdapter`);
  `startAgentHost` runs before `initSteerInbox` so the presence names it;
  one `defer closeAgentHost` (sync.Once).
- **Tokens.** `domain.StartAgentSession` mints 32 random bytes per AGENT
  session (manual Start agent and spawned workers; never Open terminal or AI
  tasks — R3), passes `GG_MCP_URL` + `GG_SESSION_TOKEN` (+
  `GG_PARENT_SESSION` for a worker) and binds token → session after
  `Manager.Start`. A token dies with its session (Verify checks RUNNING).
  `agentsession.childEnv` strips inherited channel vars. Ambient authority:
  everything the agent runs inherits the token.
- **Spawn registry** (`domain/agentspawn.go`, process-global like
  `Sessions()`): records {Parent, Brief, Worktree, Spawned}, lazy prune on
  read (R2), `AgentDescends` = the reach rule, `reserveSpawnSlot` = the cap
  (live spawned + pending, atomic — R5).
- **`SpawnAgent` order** (`domain/agentverbs.go`): caller running here → not
  spawned (no nesting) → prompt non-empty ≤ 256 KiB → caller-repo service
  (`ServiceForDir`, cached — R4: approval key = that repo's common dir) →
  spawn list non-empty → tool by name → in `[agents] spawn` → `<prompt>`
  slot → approved command TEXT (a repo `.gg.toml` can redefine a name) →
  slot → worktree resolve → claim (held by caller: keep; none: claim for the
  caller with the full guard set) → `StartAgentSession` with
  `AgentKickoff` → `HandOverWorktree` (failure = a `Warning`, the claim
  stays with the caller). The TUI only adds console size, child env and the
  status line (`onAgentSpawnRequest`, 30 s caller wait, 5 min spawn ctx).
- **Claims.** `wtclaim.Claim.Parent`; `wtclaim.Replace` (temp + rename) for
  every rewrite; `settleDeadClaim` reverts a dead holder's claim to a live
  Parent (a failed rewrite keeps the old claim); `youngClaimGrace` (2×
  `LiveWindow`, R6) keeps a fresh claim whose session its live process does
  not list yet; `sessionGuard` exempts `CallerSession`.
- **Reach.** `agentlink` (stdlib + go-sdk) pings a cached session, re-dials a
  dead one, and calls each tool exactly once. `gg mcp` registers forwarders
  (`addForward[In]`, same input structs and definition funcs as the host)
  only when both env vars are set — before repo resolution, so they work
  outside a repo. `gg agent` parses send/kill flags anywhere; `list` outside
  gg reads `Service.LiveAgentHosts()`. `gg init --mcp` runs `claude mcp add
  -s user gg -- <gg> mcp` unless `claude mcp get gg` succeeds (R7).
- **Enter.** `agent_send` presses Enter by default only after text.
- **Tests** that swap `UseSessionManager` / `agentEnv` / `agentGetenv` or
  `t.Setenv` are serial; cli/mcp/e2e TestMains unset the channel env.

## Preview reviews (2026-10-07)

Spec `docs/superpowers/specs/2026-10-07-preview-reviews-design.md`, plan 1
`docs/superpowers/plans/2026-10-07-preview-reviews-1-domain-cli-skills.md`.

- **The tag**: `ReviewTarget.Preview` → `Note.Preview` (`"<target>...<source>"`
  by branch NAMES, or `"<a7>..<b7>"` for a pair). `domain.ScopeReviewTarget(set)`
  is the ONE constructor (CLI `--preview`, `gg review save`, TUI, web); a PR
  set's `scope()` is "" so its review stays untagged. A merge-preview review
  routes to `previews.toml` (PartOf sees `...`), a pair review to
  `commits.toml`; `reviewNotes` reads both.
- **R5 — never a commit's**: `NoteCounts.Reviews` holds UNTAGGED reviews only
  (✎, Branches sub-rows, the web's heads); tagged ones go to
  `NoteCounts.PreviewReviews[scope]`. `ReviewsForCommit` skips them;
  `loadPreviewNotes` skips every review note (no ◆N inflation). `Reviews()` /
  View all notes still list them under the tip commit.
- **R2 — current / older / gone**: `PreviewReviews(ctx, set)`. Current = its
  commit is `set.Tip`; older (`ReviewHead.Older`) = both the scope's base and
  tip still exist; gone = omitted. A tip in `set.commitSet()` costs no git
  call; otherwise `ResolveRev(sha^{commit})` (pruned = not found).
- **Rename does not follow**: saved previews and preview line notes keep the
  names they were made with, so `Note.Preview` is not rewritten either.
- **`gg review save`**: `resolveLinkArg` (Pair + Ref allowed) →
  `openLinkTarget` (acts on the link's checkout) → `LinkReviewTarget`
  (preview/pair → scope; `@ref:` → `BranchReviewTarget`; commit →
  `CommitReviewTarget`; staged, or a working FILE link → working; anything
  else `ErrNoReviewChange`). Input must parse as the review document.
  `--dry-run` prints `{kind,label,range,diff}`; `diff` is `HEAD` for working.
- **`gg-review` skill**: `Skill.front` adds `argument-hint` +
  `disable-model-invocation: true` to the SKILL.md form only; installed
  wherever `delegate` is.

## Working-changes reviews (2026-10-05)

Spec `docs/superpowers/specs/2026-10-04-working-reviews-design.md`.

- **The note**: one root per review, `Address{State: StateUnstaged,
  Worktree: filepath.Clean(top)}`, no Path, tag `review`, `Files
  []model.NoteFile{Path, Blob, Deleted}`. `model.Note.IsWorktreeLevel()`
  (live, worktree, no path/commit/shelf) ⊂ `IsEntryLevel()` (cap-exempt,
  never re-anchored); `IsWorkingReview()` = that + tag. `IsReviewNote()`
  stays commit-only. It routes to `worktrees/<key>.toml`.
- **Fingerprint channel**: `engine.ReviewChanges{Working: true}.Prepare`
  hashes every numstat path (rename old side → `Deleted`) and every
  `ls-files --others` file in-process (`git.HashWorktreeFile` /
  `git.BlobOf`, the repo's object format via `GitOps.ObjectFormat`),
  synthesizes untracked new-file patches under `MaxDiffBytes` (a file past
  the cap is stream-hashed, never read whole, and the diff is reported
  truncated) → `TaskInputs.ReviewFiles` → `Result.ReviewFiles` (Collect) →
  `TaskSpec.Store(ctx, noteID, text, files)` (the task keeps them for
  RetrySave) / `ReviewReport` → `SaveReview{Files}`.
- **Matching**: `domain.WorkingReviewState(worktree, files)` → per path
  matches/changed/gone, `Current` while ≥1 matches; a missing worktree dir =
  all gone. The algorithm comes from the blob's length (40 = sha1, 64 =
  sha256). `workingBlobs` caches ids by (abs path, mtime, size) but never a
  file modified < 2 s ago (racily clean). `WorkingReviews` reads ONLY the
  worktree part (`workingReviewNotes`) so a live read never parses
  commits.toml; `reviewNotes` reads commits + this worktree's part.
- **Readers**: sweep drops a working review when outdated AND past
  max_age_days (matched against its own worktree; replies skipped);
  `NoteCounts.WorkingReviews` (never in ◆N or `Reviews`);
  `NotesOverview.WorkingReviews` (+ `Count`), kept by `ShownOn`;
  `resolveOne` returns active for every entry-level note (commit reviews
  used to be "stale" by accident of `git show <sha>:` listing a tree);
  `NotesFor`/`NotesAt` on a live file address append the matching reviews'
  NEW-side notes (`workingReviewNotesOn`, ids `review:<id>:<n>`); review
  view: `ReviewRevs` = (HEAD sha, "", false), `ReviewFiles` =
  `CompareFiles(HEAD, WorkTree)`, `reviewSplit` places only matching files'
  notes (others → `ReviewOtherNote.Changed`), `ReviewNotesFor` addresses
  the worktree.
- **TUI Files panel Review row**: a pseudo row with backing index
  `reviewRowIdx = -1` prepended by `displayIndices(panelFiles)` while a
  review is current and no `/` filter is active (`filesIdxReview` caches the
  prefixed fast path; `withStatus`/`withWorkingReviews` refresh it).
  `backingIndex`, `rowKeyAt`, `selectedKey` refuse it; walkers that index
  `m.status.Files[u]` skip it (`diff_filenav`, `buildStatusStack`, the stack
  landing). `panelLen` counts it (cursor bound); the tab count uses
  `filesPanelFileCount`. `withWorkingReviews` shifts the cursor so it stays
  on its file. Loaded off-thread after srcNotes (when the counts list any)
  and after every srcStatus refresh.
- **Review links** (merged from main's review links): `reviewTarget` of a
  working review is the working tree (`LinkTarget{State: StateUnstaged}`),
  so its link is `gg://<repo>?review=<id>` — an address-less review hint the
  resolver accepts ONLY when the checkout's store holds that id as a
  working review (`reviewHintService`); `checkReviewHint` matches it to a
  commit-less, pair-less resolution. `ReviewShow.Working` names it.
- **Web**: counts JSON `working_reviews` (current + sorted `matches`);
  `/api/review/{id}` adds `working`/`states`; `/api/diff?wt=head` = HEAD →
  working tree (no staging tags); the review opens as a compare with
  `cmp.worktree` so `fileDiffURL` uses that lane. The page's
  `state.noteCounts` is a field whitelist — a new counts field must be added
  there (files.js) and in core.js's initial value.

### Streaming file history + load more (2026-10-06)

- **Why:** `git log --follow -n 200` on a rarely touched linux file walks the
  whole history (3c509.c: first hit after 0.06 s, full walk 19 s); the old
  buffered `FileLog` showed nothing until git exited.
- `git.Repo.FileLogStream` runs the SAME argv (`fileLogArgv`) through
  `Runner.Stream`; `fileLogParser` completes a commit at its name-status line
  (a merge with no status line completes at the next format line / flush),
  and `ParseFileLog` is built on it so the two cannot drift.
- `domain.Service.FileLogStream` is **ungated and uncoalesced** (user ruling):
  a held Read for a 20 s walk would park Copy to working dir (TreeWrite) and,
  writer-preferring, every later read. The buffered `FileLog` (web/CLI) keeps
  its reservation.
- TUI: `loadHistoryListCmd(h)` builds a `historyWalk`; the goroutine starts
  when the cmd RUNS (callers build-and-drop it in tests with a nil svc). The
  walk batches into `historyChunkMsg` (20 commits or 50 ms — the timer is
  allow-listed in `tick_test.go`, it lives in the goroutine), each carrying
  `next`; `onHistoryChunk` re-arms until `done`. `h.gen` drops a superseded
  walk's chunks; a chunk for a view no longer live (`historyLive`: on the
  stack — console-parked views are restored by `dispatchParkedAware` — or in
  `filesReturnLayers`) cancels the walk. `esc`/`h` call `h.stop()` directly;
  every OTHER teardown is caught by `sweepHistoryWalks`, run at the end of
  `Model.Update`: running walks are tracked in `m.histWalks` (pointer field,
  set by `New`) and a view no longer live (stack, console-parked, files-view
  parked) is stopped in that same message. Do NOT hook `closeFilesView` —
  it also runs right before a parked stack is restored. `run` closes its
  channel on return, so a wait pending on a stopped walk returns nil instead
  of leaking its goroutine.
- **Load more:** `git log --follow` ignores `--skip` (linux: `--skip=200 -n
  200` returned the newest 200), so a load-more walk asks for
  `-n shown+200` and drops the first `shown` emissions. The first walk pins
  its start rev to a sha (`svc.RevParse`, `HEAD` for rev ""), stored in
  `h.start`, so a commit made meanwhile cannot shift the positional skip.
  `h.more` = the walk hit its limit; `↓` on the last commit starts the next
  page and `h.advance` steps onto its first commit when it lands — only if
  the cursor is still on `h.advanceAt` (the old last row).
- **Evidence** (`tui-capture.sh` on linux): 3c509.c — first screen already
  shows the newest commit selected with its diff and `· loading… 4 found`;
  esc → the `git log --follow` process is gone 80 ms later. core.c — the
  200th row is 7b3d61f (matches plain git), `↓` → `loading… 200 found` →
  279 and counting.
