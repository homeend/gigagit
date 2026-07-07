# Import / apply a patch — design

**Date:** 2026-07-07
**Status:** approved for planning

## Purpose

gg can export a commit (or one file's change) as a `git am`-able patch
(`gg commit export-patch`, TUI `.`-menu "Export as patch"). This feature adds
the inverse: point gg at a patch file on disk and apply it to the repo.
Round-trips gg's own exports and accepts foreign patches (plain `git diff`
output included).

## Decisions (from brainstorming)

- **Source: a file path.** No clipboard/stdin sources in this stage.
- **Two apply modes, user-chosen:**
  - **Working-tree** (`git apply --3way`): the patch's diff lands as
    *unstaged* changes for the user to review/stage/commit. Works with any
    unified diff.
  - **Recreate-commits** (`git am --3way`): replays a format-patch mailbox as
    real commit(s), preserving author/date/message.
- **Format detection:** a mailbox (format-patch output, first line starts
  with `From `) offers both modes; a plain diff offers working-tree only.
- **Conflicts — atomic am, 3-way apply:**
  - `am` is **all-or-nothing**: on any failure gg runs `git am --abort`
    (when the am actually started) and reports "patch does not apply
    cleanly; nothing changed". gg deliberately does NOT model a paused
    `git am` (`PausedOpIn` returns `""` for a bare `rebase-apply` dir and
    keeps doing so).
  - `apply --3way` may leave standard conflict markers + unmerged index
    entries; those flow into gg's **existing** conflict process (`x`,
    resolve files, commit) with no new machinery.
- **Frontends: both.** TUI command-palette entry + `gg apply` CLI verb.

## Layers

### `internal/git` — thin verbs (one invocation each)

- `ApplyPatch(ctx, path string, threeWay bool) error` —
  `git apply [--3way] <path>`. No `--index`/`--cached`: changes stay
  unstaged. The caller cannot tell "applied with conflicts" from "failed
  entirely" by exit code alone (both exit 1); the **engine** disambiguates
  by probing status for unmerged entries after a non-zero exit (see below).
- `AmMailbox(ctx, path string, threeWay bool) error` —
  `git am [--3way] <path>`.
- `AmAbort(ctx) error` — `git am --abort`.
- `AmInProgress(gitDir string) bool` — stat-level probe for
  `rebase-apply/applying` (the am-specific marker file; distinct from
  `rebase-apply/rebasing`), used only by the engine's rollback logic —
  NOT added to `PausedOpIn`.
- `IsMailboxPatch(data []byte) bool` — pure sniff: first non-empty line
  starts with `From ` (git mailsplit's sentinel). Lives in `internal/git`
  beside the verbs; no git invocation.

### `internal/engine` — `ApplyPatch` operation

```go
type ApplyPatchMode int
const (
    ApplyModeAuto ApplyPatchMode = iota // detect; decision on a mailbox
    ApplyModeWorkingTree                // git apply --3way
    ApplyModeCommits                    // git am --3way (mailbox only)
)
type ApplyPatch struct {
    Path string          // patch file on disk (may be outside the repo)
    Mode ApplyPatchMode
}
```

Flow:

1. Read the file head (first ~4KB) via `os` directly — the read-side
   analog of `ExportFile`/`ExportToDir`'s outside-the-tree precedent.
   Missing/unreadable file → error, nothing run.
2. Detect format. `ApplyModeCommits` on a non-mailbox →
   `ErrNotMailbox` (typed): `git am` on a bare diff has no
   author/message to work with.
3. `ApplyModeAuto` + mailbox → `DecisionNeeded` with
   `ApplyModeDecisionID = "apply_patch.mode"`, options
   `["working-tree", "commits"]` (safe option first). A decider error or
   any other answer cancels: `ErrApplyCancelled`, nothing run. `Auto` +
   plain diff → working-tree directly, no decision.
4. **Commits path:** `AmMailbox(ctx, path, true)`. On error: if
   `AmInProgress` → `AmAbort` (rolls the branch back to the pre-am state,
   even mid-way through a multi-patch mailbox), then return a typed
   `AmFailedError` wrapping git's message; summary reports nothing
   changed. On success: summary `applied N commit(s) from <base>`
   (N read back via `git log` count against the pre-am HEAD, best-effort —
   the `Commit` op's read-back precedent).
5. **Working-tree path:** `ApplyPatch(ctx, path, true)`. Exit 0 → clean,
   `Changed: true`, summary `applied <base> to working tree`. Non-zero →
   probe `git status` for unmerged entries: some present → applied WITH
   conflicts (`Changed: true`, summary names the conflicted-file count;
   the TUI's status refresh + conflict process take it from there); none →
   the apply failed atomically, return the error (tree untouched —
   `git apply` without `--reject` is all-or-nothing when 3-way fallback
   is impossible).
6. `LockMode()`: default `TreeWrite` (no override) — both paths mutate
   the tree and/or refs.

### `internal/domain`

Nothing new: `Execute(ApplyPatch{…})` and the existing `ExportDefaultDir`
(seed for the TUI path prompt) cover it.

### `internal/cli` — `gg apply`

```
gg apply [--am | --working] <path>
```

- Flags precede the positional (the `gg review` convention).
- Default (no flag) = working-tree mode, for any format: safe,
  non-committing. `--am` = recreate commits (errors on a plain diff).
  `--working` is the explicit spelling of the default. Both flags →
  usage error.
- The CLI always passes an explicit mode — `ApplyModeAuto` (and its
  decision) is TUI-only, so `gg apply` never forks mid-run.
- Exit 0 = applied cleanly; 1 = op failure (am conflict, apply failure,
  bad file) **and** applied-with-conflicts (conflicted files listed on
  stderr — the `gg merge --on-conflict=keep` convention: conflicts left
  in the tree exit 1); 2 = usage. Routed through `runOne` so `gg batch`
  drives it. How the conflicted-but-applied outcome travels from op to
  exit code follows the SmartMerge keep-conflicts precedent (planning
  will mirror the existing mechanism).

### `internal/tui`

- **Trigger:** a command-palette (`ctrl+p`) entry "Apply patch…" — the
  palette registry is built to grow, and this is a repo-level action not
  bound to any panel.
- **Path popup** (`applyPatchPopup`, mirroring `exportPatchPopup`): a
  one-line editable path field pre-filled with `ExportDefaultDir(ctx) + "/"`
  (resolved off-thread, the `startExportCommitPatch` pattern), enter
  dispatches `startOp(engine.ApplyPatch{Path, Mode: ApplyModeAuto})`,
  esc cancels. Embeds `popupMax`.
- **Mode fork:** the existing modal Decider renders the
  `apply_patch.mode` decision (Working-tree changes / Recreate commits;
  esc = cancel, nothing applied).
- **`opAffectedSources`:** map `ApplyPatch` → {status, feed, branches}
  (am moves the branch tip and adds commits; apply changes status). The
  conflicted-apply case then flows through the normal status→conflict
  wiring.

## Error handling summary

| Case | Behavior |
|---|---|
| File missing/unreadable | Op error before any git runs |
| `--am` on a plain diff | Typed `ErrNotMailbox` |
| am conflict (any patch in the mailbox) | `git am --abort`, typed error, **branch unchanged** |
| apply conflict (3-way possible) | Markers + unmerged entries land; `Changed:true`; existing conflict process (CLI exits 1) |
| apply impossible (no 3-way base) | Op error, **tree unchanged** |
| Decision cancelled (TUI esc) | `ErrApplyCancelled`, nothing run |

## Testing

- `internal/git`: FakeRunner argv asserts for all three verbs;
  `IsMailboxPatch` table test (format-patch head, plain diff, empty).
- `internal/engine` (real git in `t.TempDir()`):
  - **Round-trip:** commit → `FormatPatch` → `ApplyPatch{Commits}` on a
    reset branch → commit recreated, author/message preserved.
  - apply-clean: unstaged changes present, nothing staged/committed.
  - apply-conflict: markers present, `Changed:true`, HEAD unchanged.
  - am-conflict: op errors, HEAD unchanged, no `rebase-apply` left.
  - Auto+mailbox emits the decision; scripted decider answers both ways;
    cancel answer runs nothing.
  - `Commits` on plain diff → `ErrNotMailbox`.
- `internal/cli`: flag parsing/usage-error cases; exit codes.
- `internal/tui`: palette entry opens the popup; enter dispatches the op;
  decision modal renders; `opAffectedSources` coverage.
- `e2e/scenarios/`: `gg commit export-patch` → `gg apply --am` round-trip
  scenario (+ a `--working` case).

## Out of scope (YAGNI)

- Clipboard / stdin patch sources.
- Modeling `git am` as a resumable paused op (PausedOpIn / ContinueOp /
  AbortOp / resume prompt stay untouched).
- `git apply --check` dry-run, `--reject` mode, `git am --skip`.
- Applying from a URL.

## Docs to update on completion

`CHANGELOG.md`; `README.md` (new verb + palette entry);
`CLAUDE.md` package map (engine op, git verbs, CLI verb, TUI trigger);
`internal/agentskill/using-gg.md` + `agentskill.Version` bump
(CLI surface changed) + `gg init --update`.
