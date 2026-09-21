# The `?preview=<id>` landing hint — design

Status: for review · 2026-09-20 · branch `feat/link-preview-hint`

Parents: `2026-09-16-unified-links-design.md` §3.3 (what a hint is),
`2026-09-19-saved-commit-pairs-design.md` rev 1 §5 (this hint, as first
drawn) and rev 2 §7 (the correction: the id is a LOOKUP, never a checksum).
Nothing here re-opens a ruling; §5 lists the few decisions that are new.

## 1. Problem

A link copied off a SAVED row of the Previews surface — a merge preview
(`gg://r@main...feat/x`) or a frozen commit pair (`gg://r@<a>..<b>`) — lands
as the bare diff. The reader is not told it is an entry they (or a teammate's
identical store) hold under a label, and the Previews row is not revealed.
Bookmarks and shelf entries already have this: `?bookmark=<id>` /
`?shelf=<id>` land on the address and then reveal the row.

## 2. What ships

One new hint kind, `preview`, covering BOTH set-shaped saved entries:

```
gg://<repo>@<target>...<source>?preview=<id>      a saved merge preview
gg://<repo>@<a>..<b>?preview=<id>                 a saved commit pair
```

A two-link saved COMPARISON has no single link ("the link for this place has
no answer"), so it never carries the hint.

### 2.1 Grammar (model)

`linkHintKinds` gains `"preview"` — the one definition `ParseLink`, every
producer, both steer validators and `links.js` share. `LinkHintIDOK` already
admits a saved id. The Go↔JS gates (`linksjs_test.go`) get a `preview` row.

### 2.2 Meaning — same address, different landing

The hint never changes what the link addresses (hint rule 1) and it degrades,
never fails (rule 3). The address lands exactly as it does today; afterwards
the consumer looks the entry up in its OWN loaded Previews list:

| lookup, in order (the rev 2 ruling, unchanged) | result |
|---|---|
| 1. an entry with this id | reveal that row |
| 2. an entry naming the same set as the link's address (previews: same source + target names; pairs: same two full shas) | reveal that row |
| 3. nothing | notice: `preview <id> is not saved here; the link still landed` — this is rev 1's "show once": the diff is open, nothing is stored |

Step 2 compares the PARSED halves, never link text — the id hashes link
text, and the text spells the repository by remote name on one machine and by
path on another, which is exactly why step 2 exists.

An `?preview=` hint with **no address** stays a hard error in
`domain.finishLink` (the existing default arm — the hint carries no content
of its own, so the link names nothing). Only the message is made specific.

### 2.3 Producers — entry-level copies only

| surface | today | with this change |
|---|---|---|
| TUI Previews tab, copy link on a merge / pair row (`currentLink`) | bare set link | + `?preview=<row id>` |
| web Previews group, `copy gg link` on a merge / pair row (server-built `link`) | bare set link | + `?preview=<row id>` |
| `gg link --preview <x>` | bare set link | + the hint **only when `<x>` resolved to a saved entry** (id or label); a typed `target...source` / `a..b` stays bare |

A file or line link copied from INSIDE an open preview stays bare: its
landing is the line, and a second reveal underneath would be noise
(non-goal; trivially added later — the grammar already allows it).

### 2.4 Consumers

- **TUI** — `navigateLanded` gains a `preview` arm. The Previews rows are
  already a loaded source; the arm matches per §2.2, sets `previewFocusID`
  semantics directly (`restorePanelSel(panelPreviews, id)` + `activateTab`)
  so that closing the landed diff returns the user to the revealed row, and
  otherwise sets the notice. If the source has not loaded yet it parks a
  `pendingHint` on the `srcPreviews` arrival, with the same generation guard
  and timeout the bookmark/shelf arms use. `steerNavigateHintOnly` needs no
  arm (address-less is refused upstream).
- **web** — `revealHintEntry` gains `preview` → `#previews-list`, section
  `previews`. The list is unpaged, so there is no by-id fetch: a miss after a
  `loadPreviews()` refresh is a real miss. The `li.flash` rule is covered by
  `TestRevealHintEntryTargetsHaveAFlashRule`, which lists every target list.
- **both steer validators** (`tui/steer.go`, `web/steer.go`) admit `preview`.
- **CLI / MCP** need nothing: `gg open`, `gg session navigate` and
  `gg_link_resolve` already carry `hint_kind` / `hint_id` through.

### 2.5 Description

`linkDescFields`' hint switch gains `preview`: the saved entry's label when
this store has it (`preview: <label>`), else it falls THROUGH to the address
arms (`preview: main...feat/x`, or the pair's `link:` fallback) rather than
printing a bare id nobody can read. `links.js`'s formatter twin gains the
same kind; `TestLinkDescJSMatchesGo` pins it.

## 3. Out of scope

Hints on file/line links inside a preview · a hint for two-link comparisons
· saving an unsaved entry from the notice (the `.` menu / save chip already
do it) · any change to the per-verb R4 table.

## 4. Tests

- model: parse/render round-trip with the new kind, name-form AND local-form
  rows; an address-less `?preview=` is refused by `finishLink`.
- domain: `DescribeLink` — label when saved, fall-through when not; the two
  must disagree on one fixture (a saved and an unsaved link of the same set).
- TUI: reveal by id · reveal by matching set when the id is foreign ·
  notice on a miss · the parked reveal drains once and is spent by a newer generation ·
  producer adds the hint for merge + pair rows and none for a comparison row.
- web: handler test for the row `link`; node gates; a playwright probe
  (computed `display`, flash class, run first against a build with the
  `preview` arm removed).
- CLI: `gg link --preview <label>` carries the hint, `--preview a..b` does
  not; e2e scenario row in `links-*.toml`.
- Break table per task, as always.

## As built (2026-09-21)

- §2.4 TUI: no `pendingHint` parking — the Previews rows are a startup
  source and a start-at landing already waits for that fan-out, so the arm is
  synchronous (`revealSavedSet`). Under an open files view it also re-points
  `filesReturnFocus`, or esc would hand the keyboard to a tab no longer shown
  (found on the real binary, not by a unit test).
- §2.4 web: the reveal lives in `previews.js` (`revealSavedSet`), routed by a
  new `live.js revealHint`; `steerNavigate` stays the pinned thin wrapper.
  The flash rule has its own gate (`TestRevealSavedSetTargetHasAFlashRule`).
- §2.3 web: the merge row's link is built in JS (`links.js`), the pair row's
  on the server — two producers, both covered by the playwright probe.
- §2.3 CLI: a typed range that happens to spell a saved pair's DEFAULT label
  (`<a7>..<b7>`) is a range, not an entry — it stays bare.
- §2.5: a saved pair describes as `pair: <label>` (rev 1 §5.4's word).
- Both steer validators now call `model.LinkHintKindOK`.

## 5. Decisions made here (tell me if any is wrong)

1. One kind, `preview`, for merge previews AND pairs (rev 1 §5.1).
2. Only entry-level copies produce the hint (§2.3).
3. Address-less `?preview=` remains a hard error.
4. `using-gg` is bumped: `gg link --preview <saved>` now prints a hinted link.
