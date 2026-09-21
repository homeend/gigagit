# Symmetric Merge Preview — design

Date: 2026-09-22 · Branch: `feat/symmetric-merge-preview`

## Purpose

Two branches often fix the same thing independently. A **symmetric merge
preview** answers "how does A's change differ from B's, each measured
against the same base": it compares *what A would bring into base* with
*what B would bring into base*.

The user picks branches **A** and **B**, then explicitly picks a **base**.
gg saves the comparison `@base...A` vs `@base...B` and opens it at once.

## Rulings (agreed 2026-09-21, do not re-ask)

1. Meaning: a file-by-file diff between the two change-sets. Two branches
   that fix the same thing. Not an overlap/conflict-risk view.
2. Frontends: TUI, web and CLI.
3. The base has **no default**. The user must pick it explicitly.
4. It is **one** symmetric item, not three rows (option A below).
5. An already-saved identical entry is reused, not duplicated.
6. After creating it, gg **opens the comparison**.
7. It stays **live**: the links hold branch names, resolved on every open.
8. Loading indicators are required. Opening can take a minute on a big
   repo.

## Storage — option A: one entry, no new format

A symmetric preview is **one saved comparison** (a two-link `savedcompare`
entry, the existing `rowCompare` kind):

- Left: `gg://<repo>/@<base>...<A>` (merge preview A → base)
- Right: `gg://<repo>/@<base>...<B>` (merge preview B → base)

No store field is added and the data format does not bump.

**Recognition is by shape.** An entry is symmetric iff both halves are
whole-tree three-dot preview links (no path, no line, no hint) with the
**same target** (base) and different sources. A comparison saved by hand
from two previews that share a base is therefore symmetric too. That is
intended, because it is the same data.

Liveness already holds: `previewLinkFor` spells branch NAMES, and
`SavedCompareAdd` stores link text verbatim, so each open re-resolves the
tips.

The standalone merge previews `A → base` and `B → base` are **not**
created as separate rows. The symmetric row opens either one on demand.
Deleting the row removes the single entry.

## Domain (`internal/domain/symmetric.go`)

- `SymmetricPreviewAdd(ctx, a, b, base, label string) (SavedCompare, error)`
  - Refuses: an empty name; `a == b`; `base == a` or `base == b`; a name
    that does not resolve to a commit (`ResolveRev`); a name the link
    grammar cannot carry (`model.LinkRefOK`).
  - Builds both links from one helper (`previewLinkText(repo, source,
    target)`), the same construction as `entryFromPreview`: TARGET (base)
    first in the three-dot spelling, A on the left, B on the right.
  - Default label: `A vs B (base: <base>)`, with no arrow glyph.
  - Stores it via `SavedCompareAdd`. A duplicate returns the existing entry
    with `ErrSavedCompareExists`, and frontends treat that as success and
    open it.
- `SymmetricOf(c SavedCompare) (Symmetric, bool)` where
  `Symmetric{A, B, Base string}` is the ONE recogniser every frontend uses.
  It is pure and parses both texts with `model.ParseLink`.
- Opening runs through the existing `CompareLinks(left, right)`, with no
  new compare code.

## TUI

- **Creating it:** mark branch A (`m`), move to B, press `m`. The pair
  menu (`mark.go`) gains **"Symmetric merge preview %s, %s…"** after
  "Merge preview %s → %s…".
- **Base picker:** a popup listing local branches with the cursor on **no
  row** (the first `j`/`↓` lands on row 0) and `/` type-to-filter. Enter
  with no selection does nothing. A and B are listed but refused with a
  status message. `esc` returns to the pair menu (a window opened from a
  popup returns to it).
- **Submitting:** a busy state ("saving symmetric merge preview…"), then
  `startLinkCompare(left, right)`. That path already shows the
  **"comparing…"** popup until the view lands, and `esc` there cancels.
- **Previews tab:**
  - A symmetric `rowCompare` gets a `sym` badge (ASCII: new glyphs must be
    EAW-neutral) and shows `A vs B` plus `base: <base>` in its description.
  - Enter opens the comparison.
  - The `.` menu adds "Open merge preview %s → %s" for A → base and for
    B → base. Each opens the ordinary preview view.
  - Delete, rename and copy-link behave as for any saved comparison.
- `opAffectedSources`: saving refreshes `srcPreviews`.
- Every new string goes through `i18n.T` in all four bundles.

## Web

- **Creating it, route 1:** the branch drag-and-drop menu
  (`showBranchPairMenu`) gains **"symmetric merge preview src, dst…"**
  below the compare row.
- **Creating it, route 2:** the Previews section menu gains **"new
  symmetric merge preview…"**: prompt A, then prompt B, then prompt base,
  each validated with `knownName`.
- **The base prompt starts EMPTY** in both routes. An empty submit or an
  unknown name is refused with an opLine and the prompt stays open.
- **Server:** `POST /api/previews/symmetric {a, b, base, label}` goes
  behind `writeGuard` and returns the saved compare (201, or 200 for an
  existing entry) plus a refusal text on 400.
- **Loading:** the row that started the flow and the Previews header show a
  busy state until the save returns. The comparison then opens through the
  existing symmetric compare view, which has its own busy dialog. Route it
  through `runOnce` so a double click does not stack.
- **Previews list:** the row gets a `sym` badge. Its menu adds "open merge
  preview A → base" and "… B → base". Delete removes the entry.
- The server hands the recognised `{a, b, base}` to the page with each
  saved compare. There is no JS link parser (a standing rule).

## CLI

- `gg preview add --symmetric --base <base> <A> <B> [--label L]`:
  - Saves the entry and prints the id on stderr, like the other saves.
  - A duplicate prints "already saved as <label>" and exits 0.
  - Without `--base` it refuses with usage, since there is no default.
- `gg compare --saved <id>` already runs it.
- `gg compare --list` and `gg preview list` tag symmetric rows with `sym`
  and `base: <base>`.
- Update `internal/agentskill/using-gg.md` and bump `agentskill.Version`.

## Errors

| Case | Result |
|------|--------|
| A == B, base == A/B, empty base | refused before anything is stored |
| unknown / unresolvable name | refused, names the bad one |
| name the link grammar cannot carry (`?` etc.) | refused |
| already saved | existing entry reused and opened |
| no state dir | `ErrSavedComparesDisabled`, surfaced as is |
| open fails (e.g. branch deleted later) | entry stays saved; the error is shown in the compare surface |

## Testing

- **Domain, against real git:**
  - Assert the stored link TEXT: base first in the three-dot form, A on
    the left and B on the right, on a fixture where all three names
    differ. A double swap would round-trip unnoticed.
  - Each refusal.
  - A duplicate returns the existing entry.
  - After moving A's tip, the reopened comparison reflects the new tip
    (liveness).
- **`SymmetricOf`:** the positive case; the hand-saved case; different
  bases → false; a path-bearing link → false; a commit-pair link → false.
- **TUI:**
  - Model tests for the pair-menu row, the base picker (no preselection;
    esc goes back; A/B refused), the Previews badge and the `.` menu rows.
  - A `tui-capture` run of the whole flow.
  - The i18n gates.
- **Web:**
  - Go handler tests (guard, 400s, 201/200).
  - A Playwright probe that asserts the row is **visible**, the base
    prompt starts empty and the busy state appears.
  - Run the probe against the unfixed build first, to watch it fail.
- **CLI:** the e2e scenario `symmetric_preview.toml`, covering add, list
  and a duplicate.

## Out of scope

- MCP surface.
- Grouping several real entries (option B).
- An overlap/conflict-risk view.
- Frozen (commit-pinned) symmetric previews.
