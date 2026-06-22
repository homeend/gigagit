# Design: the files-view state machine (Stage 1b)

**Status:** Design spec (approved to plan).
**Date:** 2026-06-23.
**Branch:** `feat/files-view-state-machine` (off `main` @ `c94a4f0`).
**Companions:** `2026-06-22-split-layer-windowing-investigation.md` (Phase 1b),
`2026-06-22-windowing-zorder-root-cause.md` (the "reset choreography" bug class).

---

## Goal

Make the files view's transitions the single source of truth for its state, so a
mode change cannot leave a stale field behind. Today the view is a small state
machine coded as ~13 loose `Model` fields whose ~82 assignment sites each
hand-pick which subset to reset — the recurring "half-reset" bug class (both the
full-tree and preview features shipped bugs here).

**Behavior-preserving.** No user-visible change. The win is structural: "forgot to
reset field X on this mode switch" becomes impossible.

## Non-goals

- **No field relocation churn.** We do NOT move the ~593 reads of `m.filesHash`
  etc. into a sub-struct. Field storage location is not the point and is not worth
  the churn; the single source of truth lives in the transition methods. (A
  `filesViewState` sub-struct is a trivial later move *if* it ever becomes the
  precondition for the deferred pane work — not now.)
- No change to the diff, stash list, or layer stack.

## The state machine

The files view has three orthogonal axes, today encoded implicitly:

| Axis | Today (implicit) | Becomes |
|---|---|---|
| **Source mode** | `filesCompare` bool, `filesStashTag != ""`, `filesAllFiles` bool, else changed | a `filesMode` enum |
| **Focus** | `filesTreeFocused` bool | unchanged (set only via `focusTree`/`focusRight`) |
| **Preview overlay** | `filesPreview != nil` | unchanged (set only via `openPreview`/`closePreview`) |

### `filesMode` enum (replaces the boolean-soup discriminator)

```go
type filesMode int
const (
    filesModeChanged  filesMode = iota // a commit's changed files (vs parent)
    filesModeFullTree                  // every file at the commit (ls-tree) — `a` toggle
    filesModeCompare                   // two endpoints (filesLeft/filesRight)
    filesModeStash                     // a stash's files (filesStashTag)
)
```

A new `filesMode` field on `Model` carries the discriminator. The **data** fields
stay and keep their meaning: `filesHash` (changed/fullTree), `filesLeft`/`filesRight`
+ `compareTag` (compare), `filesStashTag` (stash), `filesTitle`, the two
`*contentPopup`s (`filesView` tree, `filesPreview`), the gates
(`filesReadInflight`, `filesPreviewTag`). The booleans `filesCompare` and
`filesAllFiles` are **removed** — their truth is now `mode == filesModeCompare` /
`mode == filesModeFullTree`. (`filesStashTag != ""` stops being a mode signal; the
mode is authoritative.)

### Transition methods — the ONLY mutators of files-view state

Each returns `Model` (and a `tea.Cmd` where it loads), sets the **complete**
consistent field set for the target state, and zeroes everything that does not
belong. Naming mirrors the existing open functions.

| Method | Replaces (current site) | Sets / clears |
|---|---|---|
| `openChangedFiles(hash, title string) (Model, tea.Cmd)` | `l`-key build (`model.go:1056`), reflog (`reflog_view.go:78`) | mode=Changed; hash,title,tree=loading; clears compare(left/right/tag)/stash/preview/fullTree; treeFocused=false; readInflight=true |
| `openCompareFiles(left, right model.Endpoint) (Model, tea.Cmd)` | `files_view.go:136` (exists — re-expressed) | mode=Compare; left,right,compareTag,tree=loading; clears stash/preview/fullTree (compare keys off left/right, not filesHash) |
| `openStashFiles(tag, title string) (Model, tea.Cmd)` | `stash_view.go:130` | mode=Stash; stashTag,title,tree=loading; clears compare(left/right/tag)/preview; clears fullTree |
| `toggleFullTree() (Model, tea.Cmd)` | `a` handler (`files_view.go`) | flips Changed↔FullTree; clears preview; tree=loading; reloads |
| `openPreview(path, hash string) (Model, tea.Cmd)` | `openFilePreview` (`file_preview.go:70`) | preview=loading, previewTag; focusRight |
| `closePreview() Model` | `file_preview.go` / esc branch | preview=nil, previewTag=""; focusTree |
| `focusTree() Model` / `focusRight() Model` | `left`/`right`/`tab` (`files_view.go`) | treeFocused true/false (focusRight inert in Compare, as today) |
| `closeFilesView() Model` | esc/`l` close (`files_view.go`), repo-switch + narrow-close (`model.go`) | zeroes the ENTIRE cluster atomically (the close chokepoint) |

The async load-result handlers (`dataLoaded`/`treeFiles`/`compareFiles`/preview
msgs in `model.go`) keep populating `filesView.lines` etc.; they read the mode but
do not transition it — receiving content is not a state change.

## What this fixes (concretely)

The scattered resets that currently must be remembered per site:
- closing must zero all 13 → now one `closeFilesView()`.
- `a` (toggle full-tree) must drop the preview (`filesPreview`/`filesPreviewTag`)
  and not touch compare → now inside `toggleFullTree()`.
- entering compare must drop `filesAllFiles` and the preview → now inside
  `openCompareFiles()`.
- opening a preview must focus right and gate stale loads → now inside
  `openPreview()`.

Each rule lives in exactly one method instead of being re-derived at every caller.

## Migration approach (behavior-preserving, incremental)

Mirrors the just-completed diff-as-layer migration (accessor/method insulation +
the suite as the regression net):

1. **Add the `filesMode` enum + field; derive it from the existing booleans** at
   the open sites (set `mode` alongside the current `filesCompare`/`filesAllFiles`
   writes). Add `mode`-based helpers `inCompareMode()`/`inFullTree()` returning the
   same truth as the booleans. No removal yet — pure addition, green.
2. **Introduce the transition methods**, each wrapping the exact current field
   writes at its site; convert the 4 open entry-points, the `a` toggle, preview
   open/close, focus changes, and every close/reset path to call them. Green after
   each cluster.
3. **Remove `filesCompare` and `filesAllFiles`**; repoint their ~47 reads to
   `inCompareMode()`/`inFullTree()` (mode-derived). Green.
4. Each step keeps the files-view / compare / preview / stash / reflog test suites
   green; behavior is identical throughout.

## Testing

**Regression net:** `files_view*_test.go`, `compare_*_test.go`,
`files_view_preview_test.go`, `files_view_alltree_test.go`, `stash_view_test.go`,
`reflog_*_test.go`, `mark_test.go`. `./test.sh race` before merge.

**New tests (the bug-class guards):**
1. **Close zeroes everything:** open the view in full-tree mode with a preview open
   and a compare tag set, call `closeFilesView()`, assert every cluster field is at
   its zero value (no stale `filesAllFiles`/`filesPreview`/`compareTag`).
2. **Mode-switch resets siblings:** from full-tree-with-preview, `openCompareFiles`
   → assert `filesPreview == nil`, `mode == filesModeCompare`, full-tree truth
   false.
3. **`toggleFullTree` drops the preview:** open a preview in full-tree, `toggleFullTree`
   → preview gone, back in changed mode.
4. **Enum/boolean parity (transitional):** during step 1, a test that
   `inCompareMode()`/`inFullTree()` equal the old booleans across each open path
   (guards the derivation before the booleans are deleted).

## Acceptance criteria

- A `filesMode` field is the authoritative source mode; `filesCompare` and
  `filesAllFiles` are gone (reads go through `inCompareMode()`/`inFullTree()`).
- Every files-view state change goes through a transition method; no caller pokes
  individual cluster fields outside those methods (grep-verifiable: assignments to
  the cluster live only in the transition methods + the async line-populate
  handlers).
- `closeFilesView()` is the single close chokepoint, zeroing the whole cluster.
- Behavior unchanged; full suite + `./test.sh race` green; CHANGELOG updated.

## Risks

- **Broad but mechanical** (~82 write-sites, 6 files). The transition methods are
  thin wrappers over existing writes; the suite covers every transition. Mitigation:
  the staged approach (add enum → route writes → delete booleans), green per step.
- **`stash`/`reflog` open paths** are easy to miss — they each build the tree
  `contentPopup` directly (`stash_view.go:130`, `reflog_view.go:78`). The grep gate
  in acceptance (no cluster assignment outside the transition methods) catches a
  missed site.
- **The async handlers populate `filesView.lines` / `filesPreview.lines`** — these
  mutate the popup's *contents* (`m.filesView.lines = …`), they are NOT cluster
  reassignments (`m.filesHash = …`), so they fall naturally outside the grep gate
  (which targets `m\.files\w+ = ` assignments). No whitelist needed; just don't
  conflate "set the popup's lines" with "transition the state".
