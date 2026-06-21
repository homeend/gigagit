# Whole-Tree Comparison ("compare commits / commit vs stage / commit vs unstaged")

**Date:** 2026-06-21
**Status:** Design approved (brainstorm); staged build, human merges per stage.

## Motivation

GitKraken lets you mark two or more commits — or your working copy plus a
commit — and see what changed between them. `gg` today can only show one
commit *versus its own parent* (the files view), and can compare *single files*
across stores (the `.`-menu "Compare against bookmark/shelf"). There is no way
to ask "what changed between commit A and commit B?" or "what does my working
tree look like relative to commit X?" as a whole-tree view.

This feature adds whole-tree comparison between two **endpoints**, where an
endpoint is a commit, the index (staged), or the working tree (unstaged). It
also closes commit-ops-pipeline-backlog **#2b** (whole-commit compare vs
working tree), which was parked for exactly this.

## Reference: what GitKraken does

- **Two commits** selected → the **difference between the two** (`git diff A B`),
  not squashed.
- **Three or more** → the **combined/squashed** diff across the range.
- **WIP + a commit** → combined diff folding in uncommitted changes.

So GitKraken's mental model is: choose two endpoints (each a commit or the
working copy) and show what changed between them; 3+ collapses a range.

## Core model (shared by every entry point)

### Endpoint

```go
// model.Endpoint names one side of a whole-tree comparison.
type Endpoint struct {
    Kind EndpointKind // Commit | Index | WorkTree
    Hash string       // commit hash when Kind == Commit; "" otherwise
}

type EndpointKind int
const (
    EndpointWorkTree EndpointKind = iota // the working tree (unstaged)
    EndpointIndex                        // the index (staged)
    EndpointCommit                       // a commit, by Hash
)
```

`Endpoint.Display()` → `"Working Tree"`, `"Staged"`, or the short hash.

### Changed-file list verb

A comparison's file list comes from a single new git verb that selects the
correct `git diff --name-status` form for the endpoint pair:

```go
func (r *Repo) DiffTreeFiles(ctx, left, right Endpoint) ([]model.CommitFile, error)
```

| left → right            | git invocation                                  |
|-------------------------|-------------------------------------------------|
| Commit A → Commit B     | `git diff --name-status -M A B`                 |
| Commit A → Index        | `git diff --cached --name-status -M A`          |
| Commit A → WorkTree     | `git diff --name-status -M A`                   |
| Index   → WorkTree      | `git diff --name-status -M`                     |

Reuses the existing `ParseNameStatus`. **The verb supports only these four
forward forms** (left = older, right = newer) and errors on any other pair.
Every call site already orders endpoints older→newer (a commit before the
index, the index before the working tree), so the reverse pairs never occur —
specifying a status-inverting reverse branch would be dead code owing a
rename-reversal test under this repo's TDD. Ordering is normalized at the call
site, not inside the verb.

### Per-file content

Each file's two sides resolve through the **existing** `model.FileRef` /
`domain.ResolveBytes`, which already yields commit / staged / unstaged content
per path (`SourceCommit` with a locator, `SourceStaged`, `SourceUnstaged`). An
`Endpoint` maps to a `FileRef.Source`+`Locator` directly.

### Cache key (correctness, stage 1)

The commit-diff cache **cannot** be reused as-is. Today every per-file diff is
`commit^ ↔ commit` — both sides immutable — so its key
(`"commit:"+hash+":"+path`) is a stable *content* identity. The instant an
endpoint is WorkTree or Index, the same key would map to changing content and
serve stale bytes after the file changes on disk. The generalized loader must:

- key on **both** endpoints + path (e.g. `left.cacheTag()+":"+right.cacheTag()+":"+path`), and
- set `Request.Key = ""` (the existing cache-bypass hatch `differ.go` already
  documents for "working-tree diffs") whenever **either** side is WorkTree or
  Index.

Commit↔commit comparisons stay fully cached. This matters in stage 1
specifically, because commit-vs-working-tree *is* stage 1.

### Generalized files view

The TUI files view is currently hard-wired to "commit `hash` vs its parent"
(`m.filesHash`, per-file diff `commit^ ↔ commit`). It is generalized to carry a
`left, right Endpoint` pair:

- File list = `DiffTreeFiles(left, right)` (was `CommitFiles(hash)`).
- Per-file diff sides resolve from `left`/`right` via `ResolveBytes` (was
  `ShowFile(rev~, path)` / `ShowFile(rev, path)`).
- The existing single-commit open is just the special case
  `left = Commit(hash^)`, `right = Commit(hash)` — preserved exactly, so today's
  behavior is unchanged.
- Header/title shows `left.Display() ↔ right.Display()`.

## Entry points (all open the generalized files view)

### A. Direct context menu on a commit
`.`-menu on a Commits-panel row gains:
- **Compare against working tree** → `(Commit(hash), WorkTree)`
- **Compare against staged** → `(Commit(hash), Index)`

No marking needed; the common "what does my working copy look like vs this
commit" case is one menu pick.

### B. Mark, then compare
- A key (`m`) on a Commits row (or a WIP row) **marks** it as the compare base;
  the marked row shows a `[base]` tag.
- On another row → `.`-menu **Compare with marked** opens
  `(marked, current)` ordered older→newer by feed position.
- Mirrors the existing per-file `pendingCompare` flow, lifted to whole-tree.

### C. WIP rows in the feed
Two synthetic rows pinned at the top of the Commits panel:
- **● Working Tree** (unstaged)
- **● Staged** (index)

They participate in mark / select / range exactly like commits, so
"Staged ↔ a commit" or "Working Tree ↔ Staged" fall out of the same flows.
Initially drawn only in the **natural list** (suppressed under the graph-lane
renderer, like other feed-disturbing states) to avoid perturbing the lane
engine; revisited later if wanted.

> **Stage 3 is the riskiest stage** — the only one touching the
> paging / graph-lane / scope / mouse-hit machinery, and the one where the
> user's intent was thinnest (they named the context menu explicitly; WIP-rows
> is an inference). **Reassess at stage 3:** if it fights the lane/paging code,
> drop it and keep stages 1, 2, 4, 5 — entry points A/B/D already cover
> commit↔commit and (via the context menu) commit↔working/staged. Do not
> pre-commit to building it.

### D. Multi-select set + shift+range
- Toggle rows into a `◉` **selected set** (reusing the Phase-3a selected-set
  pattern); **Compare selected** runs:
  - exactly 2 → `git diff A B` (difference)
  - 3+ → **squashed** range `git diff oldest^ newest`
- **shift+↑/↓** extends a contiguous range; the combined diff shows
  automatically.
- **v1 squash simplification:** the set is treated as a contiguous range by its
  min/max feed position (`oldest^..newest`). Arbitrary non-contiguous squash
  (a synthetic tree) is out of scope for v1.
- **Guard `oldest^`:** a root commit has no parent and a merge has several.
  Guard the range base the way the reset feature guards non-ancestor resets —
  fall back to the empty tree for a root oldest, and use first-parent for a
  merge (or refuse with a notice). Never let `oldest^` fail unguarded.

## CLI

`gg compare <left> [<right>]`
- Endpoints: a commit-ish, `@staged`, or `@worktree`.
- `right` defaults to `@worktree`.
- Prints the changed-file list (status + path), mirroring `gg status`'s shape.
- e2e scenario asserts file-list contents for commit↔commit and commit↔worktree.
- **Registration:** add `compare` to the `cli.go` `commands` map **and** the
  `cmd/gg/main.go` unknown-command help string — the exact drift just fixed in
  known-bugs #1. `TestEverySwitchCaseIsRegistered` now guards the map, but the
  help string is still a by-hand update; verify it while there.

## Staging (each its own feature branch, full cycle, human merges)

1. **Core + direct context menu** — `model.Endpoint`, `DiffTreeFiles` verb,
   generalized files view (single-commit open preserved), and entry point **A**
   (commit vs working tree / staged). Smallest end-to-end slice; immediately
   useful.
2. **Mark & compare** — entry point **B** (commit↔commit).
3. **WIP rows** — entry point **C** (working-tree/staged join marking/selection).
4. **Multi-select set + shift+range** — entry point **D** with the squash
   semantics.
5. **CLI** `gg compare` + agentskill bump.

## Testing

- **git verb:** real-repo `DiffTreeFiles` over every kind pair; assert the
  changed-file set against a hand-built repo (renames included, `-M`).
- **domain:** endpoint→`FileRef` resolution round-trips through `ResolveBytes`.
- **engine:** none new in stage 1 (no `Operation`; this is a read/query +
  view feature). Later stages add no engine ops either — comparison is a query.
- **TUI:** generalized files view still opens a single commit unchanged
  (regression); context-menu rows open the right endpoint pair; mark/compare
  and selected-set produce the right `(left,right)`.
- **CLI:** `gg compare` endpoint parsing + file-list output; e2e scenario.
- Gate: `gofmt`, `go vet`, `./test.sh race`.

## Non-goals (v1)

- Non-contiguous squash via a synthetic tree.
- WIP rows inside the graph-lane renderer.
- Editing/staging from the comparison view (it is read-only, like the current
  commit files view).
- A standalone "compare" surface separate from the files view.
