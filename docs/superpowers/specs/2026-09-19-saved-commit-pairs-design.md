# Saved commit pairs — store a commits diff, show it in the Previews tab

Date: 2026-09-19 (rev 2) · Branch: `feat/saved-pairs` · Status: DRAFT for review

Rev 2 rewrites rev 1 against main `ea94b45d`, where plan 3a of
`2026-09-16-unified-links-design.md` shipped `internal/savedcompare` and
CONVERTED merge previews into it. Rev 1's own store (`pairstore`,
`pairs.toml`, `ChangeSets`) is withdrawn — the store already exists.

---

## 1. Problem

The diff between two marked commits (`m`, `m`, `.` → Compare selection) is
throwaway: close the view and it is gone. A merge preview is the only
change-set a user can save and name.

The user's goal: saved change-sets of BOTH kinds, complementary, so that one
can later be compared with another (a previous state of a branch vs a merge
preview; three agents' attempts at the same job).

## 2. Scope (user ruling, 2026-09-19)

**In:** (1) the mechanics that store a commits diff; (2) showing it in the
TUI Previews tab.

**Out — other sessions own it or it waits for them:**

| out | owner / reason |
|---|---|
| "Compare with link…" palette, set × set compare UI | plan 3b, in development |
| web Previews tab listing saved entries | plan 3c |
| `?preview=<id>` landing hint | deferred; rev 1 §5 — note its id rule was wrong (§7) |
| review notes on a pair | deferred; rev 1 §6 stays the intended design |
| MCP listing | follows 3c |

## 3. Rulings that still bind

| # | ruling |
|---|---|
| P2 | A pair is **two FULL shas, frozen at save time**, even when a marked commit was a branch tip. |
| P6 | Save gesture = **`m` + `m`, then the `.` menu**. `space` is unchanged — it always opens the diff. |
| — | From plan 3a: `Add` dedups on the **(Left, Right) pair, never the id**; entries hold link TEXT; the id is `sha256(leftText\0rightText)[:8]`; the store lives under `stateBaseDir("previews")`. |

## 4. Model — no new store, no new type on disk

A saved commit pair is a **set-shaped** `savedcompare.Entry`:

```
Entry{ Left: gg://<repo>@<A40>..<B40>, Right: nil, Label, Created }
```

— exactly the shape a merge preview has (`Left: @target...source, Right: nil`),
differing only in `Left.Target`: `Pair != nil` instead of `Preview != nil`.
That is the whole "complementary" requirement at the data level, and it is
why the future set × set compare needs nothing from this feature: both kinds
are already `EvalLink`-able bounded sets in one store.

**Today's gap, precisely:**

1. nothing PRODUCES such an entry — `gg compare --save` always stores a Left
   AND a Right; only `PreviewAdd` writes a set, and only a three-dot one;
2. nothing SHOWS one — `previewFromEntry` (`domain/preview.go:50`) drops any
   entry whose `Left.Target.Preview` is nil, and the TUI reads `PreviewList`.

### 4.1 Domain — `internal/domain/pair.go` (new file)

```go
// CommitPair is a saved commits diff as a frontend sees it.
type CommitPair struct {
    ID, Label string
    A, B      string    // full shas: old side, new side
    Created   time.Time
}
func (p CommitPair) DefaultLabel() string // "<a7>..<b7>"

type PairState int // PairInvalid, PairOK, PairMissingA, PairMissingB
type PairSummary struct { State PairState; Files int }

func (s *Service) PairAdd(ctx, a, b, label string) (CommitPair, error)
func (s *Service) PairList(ctx) ([]CommitPair, error)
func (s *Service) PairGet(ctx, idOrLabel string) (CommitPair, error)
func (s *Service) PairSummary(ctx, a, b string) (PairSummary, error)
func (s *Service) PairOpen(ctx, a, b string) (PreviewEndpoints, error)
```

- **`PairAdd`** resolves `a` and `b` to full commit shas (so a CLI caller may
  pass a short sha, tag or branch — it FREEZES here, ruling P2), refuses
  `a == b` after resolution, accepts unrelated commits (a two-dot diff needs
  no ancestry), composes `Left` from `s.LinkRepo(ctx)` +
  `LinkTarget{State: Committed, Pair: &LinkPair{A, B}}`, and calls the store's
  `Add`. `ErrExists` returns the stored row with `ErrPairExists`, so a
  frontend focuses it — the `PreviewAdd` contract. Empty label →
  `DefaultLabel()`. A machine-local file write under a Read reservation — a
  domain call like `PreviewAdd`, **not** an engine op.
- **`pairFromEntry(e) (CommitPair, bool)`** — the recogniser, twin of
  `previewFromEntry`: `Right == nil`, `Left.Target.Pair != nil`,
  `Left.Path == ""`, no hint, both halves FULL shas. Anything else (a
  name-pair someone stored through `SavedCompareAdd`, a path-narrowed set)
  is **not** a CommitPair and stays out of the list — this feature does not
  promise to render shapes it did not produce.
- **`PairList`/`PairGet`** filter `st.List()` through it, as `PreviewList`
  does. **Rename and remove need nothing new**: `SavedCompareRename/Remove`
  are id-keyed and shape-agnostic.
- **`PairSummary`**: both shas exist → `PairOK` + the changed-file count of
  `A..B`; else the missing side. A frozen pair never "moves", so unlike
  `PreviewSummary` there is no movement detection and the count is cacheable
  by `(A,B)` for the session.
- **`PairOpen`** → `PreviewEndpoints{Left: commit A, Right: commit B}` — the
  shape `PreviewOpen` returns, so the TUI opens it through the same
  compare-files path.

**The two recognisers must DISAGREE on one fixture** (the unified-links
lesson): one store holding a preview entry, a pair entry and a Left+Right
comparison → `PreviewList` sees exactly the first, `PairList` exactly the
second, neither sees the third.

### 4.2 Gone commits

A sha that no longer resolves (gc, another clone) → `PairMissingA/B`: the row
is listed with `missing: <sha7>`, cannot open; rename / remove / copy link
still work. Never auto-removed.

## 5. TUI

### 5.1 Saving — Commits panel `.` menu

Two rows directly after *Compare selection*: **Save to previews** and
**Save reversed to previews**.

| ◉ set | rows |
|---|---|
| exactly 2 commits | shown |
| 0, 1, 3+ | hidden |
| holds ◇ Working tree or ◇ Staged | hidden (cannot freeze) |

Direction: **older → newer by feed order** — the order *Compare selection*
already diffs in — never mark order; *reversed* swaps. On save: status
"saved to previews: <label>"; on `ErrPairExists`: "already saved as <label>".
No naming prompt on save (rename with `e` in the tab) — one keystroke, like
`s` on a preview. The marks are left as they are.

The same two rows appear in the files view's commits-side `.` menu when the
open comparison is a two-commit pair.

### 5.2 Previews tab

`previewRow` becomes a two-kind row:

```go
type previewRow struct {
    kind  previewRowKind     // previewRowInvalid first, then rowMerge, rowPair
    rec   model.MergePreview // rowMerge
    pair  domain.CommitPair  // rowPair
    psum  domain.PairSummary // rowPair
    ...                      // sum / notes / byPath / err unchanged
}
func (r previewRow) id() string; func (r previewRow) label() string; created()
```

`readPreviews` appends `PairList` rows after the merge previews. Row text:

```
<label>  <a7>..<b7>  <N files | missing: <sha7>>
```

No `◆N` badge (notes are out of scope). `previewList.Key/Name/Date` and the
haystack go through the accessors, so sort chips and `/` filter work across
both kinds.

**Every `selectedPreview()` consumer must handle both kinds** — six sites
today (`link.go:193`, `model.go:2404`, `preview_actions.go:81/92/103/163`):

| key | merge preview (unchanged) | pair |
|---|---|---|
| enter | open preview | `PairOpen` → compare-files view, `compare:true`, **`filesPreviewSet` NOT armed** |
| `e` | rename | rename (same popup, `SavedCompareRename`) |
| `d` | remove (confirm) | remove (same confirm, `SavedCompareRemove`) |
| `s` | save reversed | save reversed (`PairAdd(b, a)`) |
| `a` | add form (source/target) | — (the form stays branch-only; pairs are saved from Commits) |
| copy gg link | `@target...source` | bare `gg://<repo>@<A>..<B>` — the stored `Left`, verbatim; recorded in `linkhist` |

A gate test enumerates `selectedPreview()` callers by AST and fails when a new
one appears without a row in this table's test — so a seventh consumer cannot
silently treat a pair as a merge preview (zero-valued `rec` → empty branch
names → a confusing git error).

Help (`?`) + footer advert; every string via `i18n.T` in all four bundles;
the save action refreshes `srcPreviews`.

## 6. CLI — the minimum that makes the mechanics scriptable

```
gg preview add <a>..<b> [--label L]   # ONE positional holding ".." and not "..." = a pair
gg preview add <source> <target>      # unchanged
gg preview list                       # both kinds; pair rows print <a7>..<b7> where a preview prints source → target
gg preview show|rm|rename|diff <id|label>   # either kind
```

A refname cannot contain `..`, so the one-positional form is unambiguous.
`gg compare.go` is deliberately NOT touched (plan 3b/3c sessions are near it).
Exit codes: 2 bad spec / unresolvable rev / `a == b`; 1 already saved (prints
id + label, like `preview add` today) or a missing commit on show/diff.

`using-gg.md` gains the pair form + a `Version` bump.

## 7. Notes for the deferred phases (so they are not re-derived wrong)

- **Hint ids are a LOOKUP, never a checksum.** Rev 1 said "hint id ≠
  ID(address) → exit 2". Wrong against the shipped store: the id hashes link
  TEXT (the repo may be spelled by remote name or by path), and converted
  previews keep legacy ids. A future `?preview=<id>` consumer finds the entry
  by id, else by matching `Left`, else opens show-once.
- **Pair notes** = ordinary committed notes on `B`, new side, via
  `PreviewNoteSet{Tip: B, Base: A, Commits: A..B}`; flips `linkShapes` for the
  note verbs (`cli/note.go`, `cli/session.go` highlight). Arming
  `filesPreviewSet` for a pair is that phase's switch.
- **Set × set compare** needs nothing from here: both kinds are set-shaped
  entries whose `Left` evaluates to a bounded `FileSet`.

## 8. Error handling

| case | behaviour |
|---|---|
| `a == b` after resolution | refuse: "a pair needs two different commits" |
| a rev does not resolve at save | CLI exit 2 naming it; TUI status line |
| already saved | existing row returned; TUI focuses it, CLI exit 1 |
| commit gone | row listed `missing: <sha7>`; open/diff refuse; rename/remove work |
| store disabled / locked / unreadable | the existing `ErrSavedComparesDisabled` + `filelock` behaviour, surfaced unchanged |

## 9. Testing (TDD; every guard watched failing with the GUARD removed)

- domain: `PairAdd` freezes a branch name to a full sha (advance the branch →
  the stored pair is unchanged); `a == b`; unrelated commits accepted; dedup
  returns the stored row; the three-entry DISAGREE fixture (§4.1);
  `pairFromEntry` rejects short-sha, named, path-narrowed and hinted sets;
  `PairSummary` missing sides; §7 guarantee — `EvalLink(pair Left)` is bounded
  and `CompareSets(pair, preview)` succeeds.
- TUI: menu gating table (0/1/2/3 marks, ◇ rows, direction by feed order with
  marks made newest-first); Previews rows for both kinds; each of the six
  consumers on a pair row; the AST consumer gate; rename/remove round-trip.
  Tests `t.Parallel()`, repos via `testRepo`.
- Headless smoke (`tui-capture.sh`): mark two → `.` → Save → Previews tab row
  visible → enter opens the files view → copy link.
- CLI + e2e: next free scenario number — add, list, show, diff, rm; the
  one-positional parse; a three-dot arg still routes to the merge-preview arm.

## 10. Plan shape

One plan, one session, no subagents, in a worktree; ordered so no commit
leaves a dark window (plan 3a's defect #1): domain recogniser + `PairAdd`
first → CLI → TUI tab rendering both kinds → TUI save rows → docs
(`CHANGELOG.md`, `README.md`, `using-gg.md` + version, `docs/CLAUDE-details.md`).
`CLAUDE.md` needs no change (no new package). Race gate on the MERGED tree
before asking to merge — plans 3b/3c touch `internal/tui` concurrently
(`action_menu.go` is the likely conflict).

## 11. Open questions

None blocking. For the plan: whether pair rows sort after merge previews or
interleave by creation time under the default sort (assumed: after).
