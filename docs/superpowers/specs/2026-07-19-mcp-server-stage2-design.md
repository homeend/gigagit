# gg MCP server — stage 2 (mutating tools) — design

**Date:** 2026-07-19 · **Status:** approved direction, spec for planning
**Builds on:** stage 1 (`docs/superpowers/specs/2026-07-19-mcp-server-stage1-design.md`,
merged `55a0ecaa`) — the `internal/mcp` frontend, its harness, `staticDecider`/`runOp`,
and the session-snapshot glue ("the snapshot emits the id, the action tool takes it").

## Goal

Complete the "act on what I selected" story: an agent that saw the user's highlighted
shelf entry or bookmark via `gg_ui_state` (or listed them) can now **re-apply a
shelved/bookmarked commit onto the current branch** and **write a stored file version
into the working tree** — the first MCP tools that mutate the repository, each gated by
the MCP client's own destructive-tool consent prompt.

## Scope: two tools

1. **`gg_cherry_pick`** — re-apply a shelved or bookmarked commit (the TUI `a`-key /
   `gg shelf cherry-pick` two-lane logic).
2. **`gg_write_to_worktree`** — write a shelf file entry, one member of a shelved
   commit, or a bookmark into the working tree as an unstaged change (one tool for both
   "restore" and "paste" — the same `engine.WriteFile` primitive under both).

**Deliberately dropped from the old deferred list: standalone `gg_apply_patch`.**
Agents already have `gg apply <path>` in the CLI; a patch file on disk is not a
selection-driven object (nothing in `gg_ui_state` points at one); and the gc'd-commit
patch replay lives *inside* `gg_cherry_pick`. A standalone tool would duplicate the CLI
without the selection glue that justifies the MCP surface. Also out (YAGNI, user framed
the agent as a consumer of selections, not a producer): write-side shelf/bookmark tools
(`gg_shelf_add` etc.), a gg-side approval store (the MCP client's consent prompt is the
stage-2 consent story), and any TUI-driving capability.

## Non-goals / unchanged invariants

- No normal-git op exposure (commit/pull/push/merge/rebase stay with the CLI/TUI).
- Stage-1 machinery is reused, not reworked: same repo binding (`Server`/`repoCheck`),
  same error contract (one-line English naming the fix), same test harness, same
  archtest layering. `staticDecider` gains exactly one new policy entry.
- Decisions remain option-lists answered from parameters — an MCP tool must never wedge
  on a question (`staticDecider` stays fail-loud on any unexpected decision id).

## Tool contracts

### `gg_cherry_pick`

Params:
- `source` — exactly one of `{"shelf": "<entry-id>"}` or `{"bookmark": "<id>"}`.
- `on_conflict` — `"abort"` (default) | `"keep"`. Maps to the live lane's
  `"cherry-pick-conflict"` decision (options `"keep-conflicts"`/`"abort"` — the
  `gg shelf cherry-pick --on-conflict` mapping verbatim).
- `mode` — `"auto"` (default) | `"patch"` (force the stored-patch replay even when the
  commit object still exists). Shelf sources only: `mode:"patch"` with a bookmark
  source is a usage error (bookmarks store no patch).

Lanes (exactly `internal/cli/shelf.go`'s `cherry-pick` logic):
- Resolve the source: a shelf entry must be a COMMIT entry (`ShelfFind` +
  `IsCommit()`; a file entry is refused naming `gg_write_to_worktree`); a bookmark must
  be a commit pointer (`IsCommit()`; a file bookmark is refused the same way).
- **Live lane:** `CommitLookup` finds the commit → `engine.CherryPick` via
  `runOp` with `staticDecider{policy: {"cherry-pick-conflict": <mapped>}}`.
- **Patch lane** (shelf source only; commit gc'd, or `mode:"patch"`):
  `domain.ShelfPatchFile` → `engine.ApplyPatch{Mode: ApplyModeCommits}` — atomic
  (`git am --3way` with rollback; nothing half-applied), refuses when a `git am` is
  already in progress (the engine's own guard). A patch-less entry (pre-patch-support,
  merge commit, oversized patch) is a clear error suggesting `gg_export`.
- A bookmark whose commit is gone has no patch fallback → clear error saying the
  commit no longer exists and only a shelf entry with a stored patch can be replayed.

Reply: `{repo, lane: "live"|"patch", commit, subject, summary, conflicts?, conflicted_files?}`.
- `commit`/`subject` — the picked commit. Live lane: short sha + subject from the
  `CommitLookup` probe. Patch lane: the entry's `Origin.Commit` (the commit is gc'd, so
  git cannot supply a subject — `subject` is the entry's shelve-time `Label` when set,
  else omitted).
- **`on_conflict:"keep"` that leaves conflicts is a SUCCESSFUL reply**, not a tool
  error: `conflicts: true` plus the conflicted paths (read from status after the op).
  Rationale: the agent explicitly opted into keep — the conflicted tree is the
  requested outcome, and a structured reply beats an error the agent must parse. (The
  engine's keep-conflicts shape is `Changed:true` + non-nil error — the handler
  translates that specific case; any other error is a real tool error.)
- `on_conflict:"abort"` hitting conflicts rolls back (branch unchanged) and IS a tool
  error naming the retry: "cherry-pick hit conflicts and was aborted — retry with
  on_conflict:keep to keep them in the tree".

### `gg_write_to_worktree`

Params:
- `source` — exactly one of `{"shelf": "<entry-id>", "member"?: "<path>"}` or
  `{"bookmark": "<id>"}`.
- `path` — optional destination, repo-relative; default = the entry's own origin path
  (shelf: `Origin.Path` / the `member`; bookmark: `Path`) — the TUI restore-prefill
  behavior.
- `overwrite` — default false.

Behavior:
- Resolve bytes exactly as stage 1's read tools do: shelf file entry → `ShelfBlob`;
  shelf commit entry + `member` → `ResolveBytes(FileRef{SourceShelf, Locator, Path})`
  (a commit entry without `member` is refused naming `gg_shelf_commit_files`; a file
  entry with `member` is refused); bookmark → `BookmarkGet` + `BookmarkBytes` (a
  path-less commit bookmark is refused naming `gg_cherry_pick`/`gg_export`).
- Write via `engine.WriteFile{Path, Data}` through `runOp` + `staticDecider` — the
  existing `"overwrite"` decision answered from the param; `ErrWriteCancelled` → tool
  error "file exists: <path> — pass overwrite:true". Identical existing bytes are the
  op's silent no-op, reported as `unchanged: true`.
- Path safety is inherited, not reimplemented: `WriteFile` writes through
  `Repo.WriteWorktreeFile`, whose `worktreePath` guard rejects any destination
  resolving outside the working tree — an agent-supplied `path` cannot escape.

Reply: `{repo, path, bytes, unchanged?}` (`path` = the repo-relative destination
actually written; `bytes` = payload size).

## Annotations & consent

Both tools set the SDK's `Tool.Annotations` to declare them non-read-only (destructive
hint on, idempotent off), so MCP clients surface their own consent prompt before each
call. Stage 1's eleven read/export tools gain the complementary read-only annotation in
the same change (export stays non-read-only: it writes outside the repo). That
client-side prompt IS the stage-2 consent story; no gg-side approval persistence.

## Errors

Stage-1 contract verbatim: every failure is a one-line English message naming the fix
where one exists ("shelf entry X is a file entry — use gg_write_to_worktree",
"commit no longer exists and this bookmark stores no patch — only a shelf entry can be
replayed; shelve commits you may want to restore later", "file exists: … — pass
overwrite:true"). Failures record through the existing domain `NoteFailure` seam.

## Testing

Stage-1 harness (`newTestEnv`, real repo, in-memory transport), extended:
- Live-lane pick of a shelved commit and of a commit bookmark (new branch state
  asserted: commit landed, subject preserved).
- Patch-lane replay after the source commit is destroyed (branch reset + reflog
  expire + `git gc --prune=now` — the CLI test's recipe), asserting an equivalent
  commit lands.
- Conflict paths: `on_conflict:"abort"` → tool error + clean tree;
  `on_conflict:"keep"` → success reply with `conflicts:true` + paths + markers in tree.
- `mode:"patch"` forcing replay while the commit exists; patch-less entry error;
  file-entry/file-bookmark refusals; bookmark-with-gone-commit error.
- Write: default-path restore, explicit path, member write, overwrite refuse/accept
  pair, `unchanged:true`, path-less-bookmark refusal, outside-tree path rejected.
- Annotations: a test asserting every registered tool carries the intended read-only /
  destructive annotation (the roster test grows a second axis).
- Full `./test.sh race` before merge.

## Documentation

`CHANGELOG.md` (the two tools + annotations), `CLAUDE.md` `mcp` package-map row
(append stage-2 sentence), `README.md` MCP section (a "Mutating tools (stage 2)"
bullet noting the client-side consent prompt). `internal/agentskill/using-gg.md`
unchanged (same rationale as stage 1).

## Deferred (stage 3+, explicitly out)

Heavy-ops MCP surface (staging, rebase, conflict editing), pid-liveness probing for
stale snapshots, multi-session awareness, TUI-driving, write-side shelf/bookmark
tools, a gg-side approval store.
