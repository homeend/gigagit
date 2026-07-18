# CLI cherry-pick of a shelved commit — design

**Date:** 2026-07-12
**Status:** implemented
**Branch:** feat/cli-shelf-cherry-pick
**Follow-up to:** docs/superpowers/specs/2026-07-12-cherry-pick-shelf-bookmark-design.md
(its "Deferred work" section is this feature's mandate)

## Problem

The TUI's `a` key cherry-picks a shelved commit through two lanes — a live
`engine.CherryPick` while the commit object exists, a `git am --3way` replay
of the shelve-time format-patch snapshot after it is gc'd. Scripts and agents
have no equivalent: `gg cherry-pick <sha>` covers only the live case and
knows nothing about shelf entries. The user queued this CLI lane as the next
task when approving the TUI feature.

The TUI feature's final review also left four hardening items that belong to
this branch (recorded in its spec and memory): a cross-field blob-reclaim
test, a modal-clobber guard on async probe results, pickGen bumps on non-esc
switcher closes, and a filter-mode `a`-stays-query-rune test.

## Decision summary (user-approved)

- Subcommand name: **`gg shelf cherry-pick <entry-id>`** — matches the TUI
  action's name and the existing top-level `gg cherry-pick`.
- Live lane supports **`--on-conflict=keep|abort`** — full parity with
  `gg cherry-pick`'s policy flag.
- Patch lane is flag-forcible via **`--patch`**.
- Exit codes per the `gg apply` convention: 0 applied, 1 failure OR kept
  conflicts, 2 usage.
- Deferred hardening folded into the same branch.
- NOT in this branch: the `toolExecCmd` $SHELL fix (separate bugfix branch).

## Non-goals

- **Bookmarks.** A commit bookmark stores no snapshot (record-only design
  contract); its live case is already `gg cherry-pick <sha>`. No
  `gg bookmark cherry-pick`.
- **A `--bucket` flag.** `ShelfAddCommit` has no bucket parameter, so every
  shelved commit lives in the default bucket; the existing `shelfEntryByID`
  scan finds them all.
- **A `--live` force flag.** Without `--patch` the live lane is preferred
  whenever the commit exists; forcing live on a gc'd commit cannot work.
- **New engine/domain surface.** Both lanes compose existing pieces:
  `engine.CherryPick`, `engine.ApplyPatch{ApplyModeCommits}`,
  `domain.CommitLookup`, `domain.ShelfPatchFile`.

## Design

### 1. `gg shelf cherry-pick` (`internal/cli/shelf.go`)

New case `"cherry-pick"` in `cmdShelf`'s subcommand switch — `gg shelf` is
already dispatched through `runOne` and allowlisted for `gg batch`, so batch
support is inherited, and each batch line's empty-stdin contract makes an
unanswered decision fail loud with its options (the standard non-interactive
behavior).

Usage: `gg shelf cherry-pick [--patch] [--on-conflict=keep|abort] <entry-id>`
(flags precede the positional — the `gg review`/`gg apply` convention).

Flow:

1. Parse flags. `--on-conflict` outside `keep|abort` → usage error, exit 2.
   Missing/extra positional → usage line on stderr, exit 2.
2. Resolve the entry with the existing `shelfEntryByID` pager scan.
   Not found → `shelf cherry-pick: no entry "<id>"` on stderr, exit 1.
3. `!e.IsCommit()` → `shelf cherry-pick: <id> is not a shelved commit` on
   stderr, exit 1 (the TUI's file-entry notice, CLI-toned).
4. Probe `svc.CommitLookup(ctx, e.Origin.Commit)`; a probe *error* (not a
   missing commit — that is `found=false, err=nil`) → exit 1.
5. **Lane A — live** (`found && !--patch`): run
   `engine.CherryPick{Commit: e.Origin.Commit}` with the exact
   `cmdCherryPick` decider setup — policy map
   `{"cherry-pick-conflict": "keep-conflicts"|"abort"}` from `--on-conflict`,
   `cliDecider{policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}` —
   then `finish(res, err, stdout, stderr)`.
6. **Lane B — patch** (`!found || --patch`): requires `e.PatchSHA != ""`.
   - Missing patch, commit gone: `shelf cherry-pick: commit <short> no longer
     exists and this entry has no stored patch (shelved before patch support,
     or a merge commit)` — exit 1.
   - Missing patch with `--patch` (commit may still exist): `shelf
     cherry-pick: entry has no stored patch` — exit 1.
   - Otherwise: `svc.ShelfPatchFile(ctx, e.ID)` → temp path (deferred
     `os.Remove`), then `engine.ApplyPatch{Path: tmp, Mode:
     engine.ApplyModeCommits}` with a bare `cliDecider{}` (the mode never
     forks; `git am --3way` is atomic — on failure it rolls back), then
     `finish`.

`stdin` threads into the subcommand for the live lane's interactive decider
(`cmdShelf` already receives it for `restore`/`export`).

Exit codes: 0 = commit created (or a requested `--on-conflict=abort` rolled
back cleanly — engine returns `Changed:false, err==nil`, matching
`gg cherry-pick`); 1 = failure OR conflicts left in the tree (live lane under
`--on-conflict=keep`, which returns `Changed:true` plus a non-nil error —
`finish` maps any error to 1); 2 = usage.

### 2. Deferred hardening (same branch)

**2a. Cross-field blob-reclaim test** (`internal/shelf`). `Remove` claims to
delete a removed entry's blobs only when no survivor references them in
*either* field (SHA or PatchSHA). Add the two cross-field cases the existing
tests skip: (i) removed entry's `PatchSHA` equals a survivor's `SHA`;
(ii) removed entry's `SHA` equals a survivor's `PatchSHA`. Both blobs must
survive on disk. Expected test-only — this is the one branch where a
regression means data loss, so it gets a pinned test.

**2b. Modal-clobber guard** (`internal/tui`). `handlePickProbe` currently
drops a result when `msg.gen != m.pickGen || m.running`; a probe returning
while an unrelated modal is open still replaces it. Add `m.modal != nil` to
the drop condition, with a status notice (`cherry-pick: cancelled (another
dialog opened) — press a again`) so the drop is visible. Apply the same
guard to `pushTagCheckMsg` (identical race, 5s window): when a modal is open
on arrival, drop the check with a status notice (`push cancelled (another
dialog opened) — press P again`) instead of clobbering the dialog or
dispatching a push under it.

**2c. pickGen on non-esc closes** (`internal/tui`). The switcher popups bump
`pickGen` on esc only; closing via enter (open diff / compare / whole-tree
compare) leaves a stale probe able to open its modal over the new view. Bump
`pickGen` on every path that closes the `g`/`G` switcher — exact call sites
located at plan time (candidates: `openPickerDiff`, the commit-bookmark
enter-compare path, `clearLayers` callers within the popups).

**2d. Filter-mode `a` test** (`internal/tui`). Assert that while a switcher
is filtering, `a` appends to the query and dispatches no probe (test-only;
the behavior already exists).

### 3. Docs & skill (CLI surface changed)

- `internal/agentskill/using-gg.md`: document the new subcommand; bump
  `agentskill.Version`; run `gg init --update`; COMMIT the regenerated
  installed SKILL.md copy (the dogfood copy is tracked — prior-feature
  gotcha).
- `CHANGELOG.md` (Unreleased), `README.md` (CLI reference: shelf commands),
  `CLAUDE.md` (`cli` package-map row gains the subcommand).
- Memory: mark the queued-task memory done; feature memory updated at merge.

## Testing (TDD)

CLI tests in `internal/cli/shelf_test.go` (real repo via the existing
helpers, the `cherrypick_test.go`/`apply_test.go` patterns):

- Live lane: shelve a commit on a side branch, switch back, run the command →
  commit lands, exit 0.
- `--patch` forces the mailbox replay even though the commit exists — both
  lanes mint a new sha, so assert lane selection on the printed op summary
  (`finish` echoes `Result.Summary`: ApplyPatch's wording, not CherryPick's)
  plus the commit landing (subject matches).
- Gc'd lane: shelve, then rewrite the branch away (`git reset --hard` +
  `git gc --prune=now` in the fixture, or delete the ref and prune) → patch
  replay lands, exit 0.
- Gc'd + no patch (entry created without a patch, e.g. index doctored or a
  merge commit shelved) → exit 1, message names both possible causes.
- `--patch` on a patch-less entry → exit 1 `entry has no stored patch`.
- File entry → exit 1 `not a shelved commit`; unknown id → exit 1.
- Conflict + `--on-conflict=keep` → exit 1, conflicted file present in tree;
  `--on-conflict=abort` → exit 0, tree clean (parity: `gg cherry-pick
  --on-conflict=abort` exits 0 — the abort was the requested outcome and the
  rollback succeeded; scripts detect "didn't land" from the `aborted:`
  summary).
- Usage errors (no positional, two positionals, bad `--on-conflict`) → 2.
- `gg batch` drives the command (one line in an existing batch test, not a
  new e2e scenario).

TUI hardening tests: 2a in `internal/shelf`; 2b/2c/2d in `internal/tui`
(construct the racing state directly — modal open, then deliver the msg).

No new e2e scenario: the command composes ops that e2e already covers, and
the CLI tests exercise the full dispatch through `Run`.
