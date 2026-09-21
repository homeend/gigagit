# Showing a comparison of two change-sets — research and guidelines

Date: 2026-09-21 · Scope: `gg web`, saved comparisons (two `gg://` links) ·
Status: research for a design; nothing here is decided until you say so.

## 1. What is being shown

A saved comparison compares two **change-sets** (two merge previews, a commit
pair vs a preview, three agents' attempts pairwise). Each side is a *small set
of files with content*. Domain already answers it (`CompareSets`, bounded ×
bounded): the union of both sets' paths, identical files omitted, each row
`D` (only left) / `A` (only right) / `M` (both, bytes differ).

Today the web paints that as an ordinary files view: one list, one diff.
Three things get lost:

1. **Which side a file belongs to.** `A`/`D` are borrowed from a tree diff.
   Here they mean "only the right set touches this" — not "somebody added a
   file". The letter misleads.
2. **Agreement.** A file both sets changed *identically* vanishes. For "did
   three agents do the same thing?" that is the most interesting row.
3. **Direction.** Nothing says which set is old and which is new, and it
   cannot be flipped.

One more fact the listing hides and domain already knows (`FileSet.has`): a
set can contain a file **because it deleted it**. So per side a path is in one
of three states — *not in this set* · *in the set, has content* · *in the set,
deleted by it*.

## 2. How other tools do it

| Tool | What it compares | Presentation | Take-away |
|---|---|---|---|
| **Beyond Compare / Meld / Araxis / WinMerge** — folder compare | two trees | **Two aligned lists, one row per path; a file on one side only ("orphan") leaves a gap on the other.** Colour by state; toolbar filters *all / differences / orphans / same*. | This is exactly your idea, and the mature form of it. Alignment + gaps make presence readable without any letters. Filters are what keep it usable. |
| **Gerrit** — patch set A vs patch set B | two versions of one change | One file list; two dropdowns "Diff against: A ▸ B"; the diff's left is A, right is B. Files unchanged between A and B are hidden. | Direction is an explicit, always-visible control — not implied. But a single list cannot say "only in A". |
| **GitLab MR "compare versions"**, **GitHub compare** | two revisions | Same as Gerrit: one list, selectors on top. GitHub has a "switch base" swap. | The **swap control** is standard and expected. |
| **`git range-diff` / Phabricator interdiff** | two versions of a patch series | A *diff of diffs*: lines prefixed twice (`+-`, `-+`). | Precise, famously hard to read. Not what a side-by-side UI wants; what it tries to say ("what did v2 change about the change") is better said by comparing the *resulting content*, which `CompareSets` already does. |
| **JetBrains "Compare directories" / VS Code compare folders ext.** | two trees | Aligned two-column list again, centre column with a per-row symbol (`→`, `←`, `≠`, `=`). | A **centre gutter symbol per row** reads faster than colour alone. |
| **Kaleidoscope / Araxis three-way** | 3 inputs | three aligned columns | Alignment generalises to N sets — relevant later for "three agents". |

Convergence: every tool whose job is *two collections* (not two revisions of
one thing) uses **aligned rows with gaps**. Every tool whose job is *review*
makes **direction an explicit control**. Nobody ships diff-of-diffs as the
primary view.

## 3. Guidelines I'd follow

1. **Alignment is the feature.** Same rows, same order, both sides; a gap
   where a set has no such file. Sort by path once, shared.
2. **Say "not in this set", never "added/deleted", for membership.** Keep
   *deleted* for what it really is: the set deletes that file.
3. **Three per-side states, three looks:**
   *in set, has content* → normal row, clickable ·
   *in set, deleted by it* → struck-through + `D`, clickable ·
   *not in set* → empty placeholder, not clickable, not focusable.
4. **A centre gutter symbol per row** between the lists' meaning, shown on the
   row itself: `≠` differ · `=` identical · `◁` only left · `▷` only right.
   Colour supports it; it never carries it alone.
5. **Show agreement, dimmed, behind a filter.** Chips: *differences* (default)
   · *all* · *only one side* · *identical*. The count on each chip is the
   summary ("12 differ · 3 identical · 4 only left · 1 only right").
6. **One selection, two highlights.** Clicking either side selects the ROW;
   both sides highlight; keyboard `j/k` moves rows, not columns.
7. **Direction is a visible, clickable control over the diff**:
   `left label  →  right label`, click (or `x`) flips to `←`. A flip swaps the
   diff's old/new; **the columns do not move** (spatial memory beats
   symmetry). The labels are the saved links' descriptions, as today's title.
8. **What a row's diff is when one side is missing:** the whole file as an
   addition/removal *in the arrow's direction* — same as today, but the header
   says "only in <label>" rather than `A`/`D`.
9. **Opt-in by button, gated by width.** A `⇄ symmetric` toggle in the files
   header, only on a link comparison; remembered in `/api/uistate`
   (localStorage dies with the random port). Below the width where two lists
   + a diff stop fitting (~1200px), the toggle is disabled with a tooltip.
10. **Don't build diff-of-diffs.** If "what did each set change" matters, a
    later step can let a side's row open *that set's own diff of the file*
    (left column row → the preview's diff) — cheap, and far more readable.

## 4. What I would NOT show

- Line counts per side, rename arrows, per-side commit info — noise at this
  size; the diff header already carries what matters.
- A tree with folders in v1. These sets are small; a flat, shared, sorted path
  list keeps alignment trivial. (Folder grouping can come later, collapsed
  identically on both sides.)
- Notes badges in the columns: a comparison of two sets has no note scope.

## 5. Cost, roughly

- **Web**: a second list pane + gutter + arrow + toggle + uistate key;
  the diff pane is reused as is. Flip = re-request the row with sides swapped
  (the door already takes left/right).
- **Domain/wire**: one small addition — each row carries `left`/`right`
  member state (`absent | present | deleted`), and an opt-in to include
  identical rows. `FileSet` already holds both facts; `CompareSets` just does
  not report them. No change for any other caller.
- **TUI / CLI / MCP**: untouched (the extra row fields are additive).

## 6. Decisions I need from you

1. Guideline 5 — include **identical** files (dimmed, filterable)? Recommended: yes.
2. Guideline 3 — distinguish **"deleted by this set"** from **"not in this
   set"**? Recommended: yes; it is the one thing today's view gets wrong.
3. Guideline 7 — the flip swaps **only the diff**, columns stay. OK?
4. Guideline 10 — a side's row opening *that set's own diff* : v1, later, or never?
   Recommended: later.
5. Width gate ~1200px and the toggle remembered per user (not per comparison). OK?

Sources: Beyond Compare — Understanding the Folder Compare Display
(scootersoftware.com/v5help/dir_understanding_the_display.html), Filtering the
Folder Compare View (scootersoftware.com/v4help/dir_filtering_the_view.html);
Gerrit — Review UI Overview
(gerrit-review.googlesource.com/Documentation/user-review-ui.html), Patch Sets
(…/concept-patch-sets.html); git-range-diff (git-scm.com/docs/git-range-diff);
GitButler — patch-based review with range-diff
(blog.gitbutler.com/interdiff-review-with-git-range-diff).
