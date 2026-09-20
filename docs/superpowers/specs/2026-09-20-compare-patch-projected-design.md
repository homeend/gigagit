# `gg compare --patch` of a file set — design

The last parked item of the unified-links work ("projected `--patch` +
`DiffSpec.Reverse`"). Builds on `domain.ComparePatchSets` (the rule that used
to live in `internal/cli`). Status: **built** on `feat/compare-patch-projected`.

## 1. Today

`gg compare A B` is total: any two endpoints or links compare, because the
listing works on FILE SETS. `gg compare --patch A B` is not. `ComparePatch`
takes two ENDPOINTS, so it refuses (exit 2) whenever

- a side is a file set its endpoint cannot reproduce — any link with a
  `/<path>`, any `@<a>..<b>` change-set (`*PatchLosesSetError`), or
- the pair is a reversed live pair — `@worktree` then a commit
  (`ErrComparePatchPair`; `model.DiffSpec` has no `-R`).

The refusal is honest, but it covers the commonest spelling in the product:
every "copy gg link" button emits a link with a `/<path>`.

## 2. What reading the code found

- **F1. The renderer already exists.** `ComparePatch`'s shelf lane renders PER
  MEMBER: for each row of the comparison it resolves both sides' bytes, runs
  `git diff --no-index` on two temp files and relabels the header to
  `a/<path>` / `b/<path>`. It needs nothing from git but bytes, so it can
  render ANY comparison `CompareSets` can list.
- **F2. It reads bytes from the wrong place for a set.** The shelf lane calls
  `left.FileRef(path)`. A set's member bytes come from `fs.Source(path)` — a
  `-u` stash keeps untracked files on a third parent — and a rename's left
  side lives at `OldPath` (the 3c `leftPathOf` lesson).
- **F3. The old TODO asked the wrong question** ("which hunks of a file does a
  projection contain"). A file set is a set of FILES. Its patch is the
  whole-file diff of each listed member — exactly the rows the default listing
  prints, which is the property the refusal existed to protect.
- **F4. `DiffSpec.Reverse` is not needed.** A reversed live pair is one more
  comparison `CompareSets` already lists (it asks git forward and inverts the
  statuses); rendered per member, left-to-right, it needs no `-R`. Adding a
  field to `model.DiffSpec` — which every diff cache key and four frontends
  read — for one CLI lane is the larger change for the smaller result.

## 3. Decisions

| # | Decision |
|---|---|
| Q1 | **`ComparePatchSets` renders instead of refusing.** Whole endpoints keep the one-invocation lane (`ComparePatch`); a side that loses its key set, or a pair `ComparePatch` answers with `ErrComparePatchPair`, goes to the per-member lane. `*PatchLosesSetError` is deleted — nothing can raise it. |
| Q2 | **The per-member lane is `CompareSets`' own rows**, in its order: bytes from `left.Source(leftPathOf(row))` and `right.Source(row.Path)`; an `A` row reads no left side, a `D` row no right side; a read error on a side the row says is present propagates (the shelf lane's rule). A rename is labelled `a/<old>` → `b/<new>`. Binary members print `Binary files a/… and b/… differ`. |
| Q3 | **The shelf lane moves onto the same function.** One per-member renderer, not two: `ComparePatch`'s shelf arm calls it with the two `EvalEndpoint` sets it already derives. Its output is pinned byte-for-byte by the existing tests. |
| Q4 | **`DiffSpec.Reverse` is not added** (F4). The reversed live pair renders through Q2. |
| Q5 | **Cost is stated, not hidden.** The per-member lane is 1 `diff --no-index` + up to 2 blob reads per row. It runs only where the fast lane cannot describe the comparison; a whole-tree reversed pair of thousands of files is slow and correct, where it used to be refused. |
| Q6 | CLI: the two refusal messages and their exit 2 go away; `using-gg` is rewritten for the verb and `agentskill.Version` bumped. The web and MCP are untouched (they call `ComparePatch` with endpoints by construction). |

## 4. Tests

Domain: a file link × a point renders ONLY that file though the endpoints
differ in two (the look-alike whole-endpoint call must list both — one fixture,
two answers) · a pair × a point renders the pair's members only · pair × shelf
(the row that once answered wrongly) · a `-u` stash pair × its parent renders
the untracked file's bytes (F2: `Source`) · a rename reads the left side at
`OldPath` · a reversed live pair equals the forward patch with the sides
swapped · the shelf lane's existing goldens unchanged.

CLI: the four refusal tests flip to "exit 0 and exactly these files"; e2e
scenarios asserting exit 2 for `--patch` are updated.

Break table: `Source` → `Endpoint` (stash row red) · `leftPathOf` → `row.Path`
(rename row red) · route a narrowed set to the fast lane (file-link row red:
two files) · drop the `ErrComparePatchPair` fallback (reversed row red).
