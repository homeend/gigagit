# Drag & drop compare in the `gg web` Previews section — design

Date: 2026-09-21 · Branch: `feat/previews-dnd-compare` (off main `f5df3940`)
Mock: `docs/superpowers/specs/mocks/previews-dnd-compare-mock.html`

## 1. Goal

In the sidebar's **Previews** section, drag one row onto another to compare the
two change-sets — the same gesture that starts a merge or rebase in the
Branches section. The comparison is the existing link comparison
(`/api/compare-links` through `runLinkCompare`), so two sets land in the
symmetric view when it is switched on.

Nothing new is computed. The feature is a new *way in* to a screen that
exists.

## 2. What a row is

The Previews list holds three kinds of row:

| kind | what it is | its ONE link | draggable / drop target |
|---|---|---|---|
| merge preview | `source → target` | `gg://repo@target...source?preview=<id>` — what its **copy gg link** copies | yes |
| commit pair | `a..b` | `e.link` from the wire | yes |
| comparison | TWO links | none — there is no one link | **no** |

A comparison row is refused on both ends, exactly as the TUI's marks refuse it
(`previewRowLink`): comparing a comparison has no meaning in the algebra.

A merge-preview row whose link cannot be built (a ref name the link grammar
cannot carry — `linkRefOK` false) is not draggable and not a target either. It
already has no **copy gg link** row for the same reason.

The link used is *exactly* the one **copy gg link** copies (the builder in
`links.js` is reused, not re-derived), hint included — so the comparison's
labels read "preview: <label>" / "pair: <label>", not a raw link.

A row in a broken state (`missing: feat/x`, `no common base`, `error`) stays
draggable: the server is the one judge of a link, and its refusal is shown on
the op line (`compare: …`, red). The client never screens a pair.

## 3. The gesture

1. **dragstart** on a draggable row — remembers the row's id + kind.
2. **dragover** another draggable row — the row gets the same outline + tint a
   branch row gets (`.drop-target`). Over itself, over a comparison row, over
   the empty area: no highlight, and the browser shows its own "no drop"
   cursor (no `preventDefault`).
3. **drop** — opens the shared context menu at the pointer. The menu row is
   the confirmation, as it is for branches:

   ```
   compare  <dragged>  ↔  <dropped-on>
   compare  <dropped-on>  ↔  <dragged>
   ─────────
   compare and save…
   ─────────
   cancel
   ```

   Labels are the rows' own labels. Left = old side, right = new side; the
   first row puts the DRAGGED row on the left.
4. A **compare** row runs `runLinkCompare("left=…&right=…")`: "comparing…
   reading both sides" on the op line, then the files view with the
   `preview comparison` / `commit-pair comparison` / `link comparison` badge,
   symmetric when the preference is on and the window is wide enough.
5. **compare and save…** asks for a label (the existing prompt), saves the
   comparison (`POST /api/saved-compares {left, right, label}`, dragged =
   left), refreshes the list, and opens it by id. "already saved as <label>"
   opens the existing one.

Rows are looked up by id in `state.previews` / `state.savedCompares` at drop
time — the list's `innerHTML` is replaced on every live refresh, so an element
reference would go stale mid-drag.

## 4. The way in without a mouse drag

The branch list's rule (`sidebar.js`): drag is the power-user path, the menu is
the accessible one. So the right-click menu of a merge-preview row and of a
pair row gains

```
compare with…
```

which opens the existing **compare with link** dialog with the LEFT field
prefilled with this row's link (the right field is left as it was; its history
dropdown lists recently copied links). No new dialog.

## 5. Decisions — my recommendation, yours to overrule

| # | question | recommendation | why |
|---|---|---|---|
| D1 | drop opens a menu, or compares at once? | **menu** | it is what "the same way as merge/rebase" does; a drop is easy to make by accident; the menu is where direction and "save" live. Cost: one extra click. |
| D2 | which rows take part? | **merge previews + pairs**; comparisons refused | a comparison has no one link; matches the TUI twin. |
| D3 | invalid drop | **no highlight + the browser's no-drop cursor**; nothing happens on release | same as the branch list (a branch onto itself). No error text for a non-event. |
| D4 | keyboard twin in the web | **`compare with…` menu row** (§4), no new keys | the sidebar has no row cursor to mark with; the dialog already exists. |
| D5 | TUI twin | **out of scope** | `feat/preview-marks-compare` (space/`m` marks two Previews rows → compare) is another session's branch, 6 commits, NOT merged yet. This work does not touch it. |
| D6 | default direction | **dragged = left (old), dropped-on = right (new)** | reads like the sentence "compare A with B"; row 2 of the menu and `x` in the symmetric view both flip it. |

## 6. Where the code goes

All client code lives in `static/previews.js` — it owns `#previews-list`, and
`sidebar.js` cannot import it (the import cycle noted at its top).

- `renderPreviews` / `savedRowHTML`: `draggable="true"` on merge-preview and
  pair rows; merge rows gain `data-kind="preview"`.
- `rowLink(e)`: the one link of a row — `e.link` for a pair, the `links.js`
  preview builder for a merge preview (exported from `links.js` as
  `previewRowLink`, and `registerRows("preview", …)` switches to it so there is
  one builder).
- five delegated listeners on `#previews-list` (`dragstart`, `dragover`,
  `dragleave`, `dragend`, `drop`) mirroring the branch list's.
- `showPreviewPairMenu(src, dst, x, y)` and the `compare with…` rows.
- `style.css`: `#previews-list li.drop-target` joins the `#branches-list` rule
  (the class is styled per list — without this the highlight is invisible).
- the Previews section's help text (`registerHelp` in `previews.js`).

No server change. No new preference.

## 7. Errors

- The server refuses a side → op line, red: `compare: <server message>`.
- A row vanished between dragstart and drop (live refresh removed it) → the
  drop does nothing.
- Save fails → `save: <message>` on the op line; nothing opens.

## 8. Testing

- **Go wiring guard** (`internal/web/previewsjs_test.go`): the five listeners,
  `draggable`, `data-kind="preview"`, the menu labels, the
  `#previews-list li.drop-target` CSS rule, the help line.
- **Playwright probe** (synthetic HTML5 drag events with a real
  `DataTransfer`), asserting VISIBILITY:
  - the target row's outline is painted (computed style, not the class);
  - a comparison row and the dragged row itself never highlight;
  - the drop menu is visible with both directions and "compare and save…";
  - row 1 lands in the files view with the kind badge, dragged = left;
    row 2 lands reversed;
  - "compare and save…" adds a comparison row and opens it;
  - `compare with…` opens the dialog with the left field filled;
  - a broken row's drop says so on the op line.
  The probe must FAIL against builds with (a) the `dragover`
  `preventDefault` removed, (b) the CSS rule removed, (c) the drop handler
  removed.
- `./test.sh race` on the merged tree.

## 8.1 As built

- `draggable` is painted by KIND (merge preview, pair), not by whether the
  link builds: the first paint can precede the repo identity. A linkless row
  cancels its own `dragstart`.
- The guard-removal runs: no `preventDefault` → the permission check fails; no
  CSS rule → both outline checks fail; no `drop` listener → the menu and every
  landing fail. A real-mouse drag in Chromium was checked as well.
- A synthetic `drop` ignores the drop permission, so the probe asserts
  `dragover` is cancelled directly.

- The TUI twin (D5) landed on main while this was built (`2c8691b3`, space /
  `m` marks on two Previews rows); main was merged in, nothing here touches it.

## 9. Out of scope

The TUI twin (D5); dragging rows from other sections (bookmarks, shelf,
branches) onto a Previews row; reordering the list by drag; touch devices.
