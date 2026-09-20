# `gg://` links in `gg web` — design for plan 3c

Parent: `2026-09-16-unified-links-design.md` (§5.1, §5.5, §8) and
`2026-09-19-links-tui-design.md` (§3.5–3.8, §7). This document records only
what reading `internal/web` forced. Everything already ruled (D1–D10, P1–P11,
the one rule, target first, "pair" vs "comparison") stands and is not
repeated.

3c is **layout and transport**. It adds no algebra, no store and no domain
function. Every new route is a thin call into something 3b-2 shipped.

---

## 1. What reading the code found

| # | finding | consequence |
|---|---|---|
| F1 | **The web has no link PARSER.** `static/links.js` is a producer (`linkFor`) plus three validity predicates. Nothing in `static/` turns link text back into parts. | A JS twin of `BoundKind`/`WithBase` would need a JS port of `ParseLink` + `String` — the whole grammar — for one dialog. See W1. |
| F2 | The web already has a **spec-addressed per-file diff lane**: `/api/entry-diff?left=<spec>&right=<spec>&path=` with the closed vocabulary `worktree \| staged \| commit:<hex> \| bookmark:<id> \| shelf:<id>`, opened by `openEntryCompare` / `openEntryFileDiff`. | Every endpoint `FileSet.Source(path)` can return after evaluation (worktree, index, shelf, commit, pair → its `b`) is spellable in it. **No new diff lane.** |
| F3 | `/api/entry-diff` reads BOTH sides at one `path`. `CompareSets` can return `R` rows with an `OldPath` (the unbounded × unbounded lane). | The left side of a rename reads the wrong path. `old_path` is added to the lane (the precedent is `handleDiff`'s `oldPathFor`). |
| F4 | `getJSON` throws `Error(body.error)` and drops the rest of the body; `postJSON` keeps it as `err.data`. | The dialog must know WHICH side failed. `getJSON` gains the same `err.data`. |
| F5 | The web **Previews tab lists merge previews only** (`/api/preview` → `PreviewList`). Saved commit pairs never reached the web — that session deferred "web Previews tab" to 3c by name. | 3c adds BOTH missing kinds: commit pairs and comparisons. |
| F6 | `PreviewRename/Remove` and `PairRename/Remove` each refuse an id of another shape. `SavedCompareRename/Remove` are shape-agnostic. | The web routes by kind **server-side**, and guards the comparison arm itself (`!IsSet()`), or a comparison surface could rename a merge preview by id. |
| F7 | Web pair landing (`live.js openCompareForPair`) goes through `/api/compare?revs=1` — endpoint-shaped. A `-u` stash link's untracked file is missing there: the window the TUI closed in 3b-2 is still open in the web. | Closed in 3c, through the door (W7). |
| F8 | `GET /api/linkhist` has no JS reader. Every copy POSTs; nothing lists. | The dialog is its first consumer. No server change. |
| F9 | The copy-row gate `TestEveryCopyGGLinkRowRecords` matches `label:\s*"copy gg link[^"]*"`. | New copy rows must START with `copy gg link` or the gate cannot see them. |
| F10 | The web never passes `ResolveOpts`. It owns a registry path (`s.reposStatePath()`). | The web passes `ResolveOpts{Cwd: svc, RegistryPath: s.reposStatePath()}` — so, unlike MCP, the cross-repository refusal IS reachable here and is tested. |

---

## 2. Decisions

| # | decision |
|---|---|
| **W1** | **No JS twin of `BoundKind` / `WithBase`. The server classifies and rewrites.** One read route (§3.3) returns the located kind and the suggestion, and — given a base — the rewritten link TEXT, produced by `model.Link.WithBase` itself. This **replaces** §7's "Go↔JS gate like `linkDesc`'s": that line assumed a JS parser exists (F1). The rewrite stays literally the ONE rewrite; a second one in JS is the drift the gate would have existed to catch. The contract is gated by a handler test on the returned TEXT instead. The base row needs a server call anyway (`SuggestBase` reads git), so this costs no extra round trip before the user acts. |
| **W2** | One read route is the web's door: `GET /api/compare-links` → `domain.CompareLinks`. It takes exactly one of three input forms (§3.1) so that a dialog submit, a saved row and a pair landing are ONE lane. |
| **W3** | The view is `openLinkCompare(body)`, a sibling of `openEntryCompare`. Byte sources ride the wire **per row**, only where `Source(path)` differs from the side's own spec (§3.2) — the web's `compareSides`. |
| **W4** | The Previews tab renders three kinds. `/api/preview` is left exactly as it is (the PR lane, `reopenPreviewIfMoved` and `state.previews` lookups all hang off it). A new `GET /api/saved-compares` serves the other two kinds. Order: merge previews, then commit pairs, then comparisons; store order within each. |
| **W5** | Save, rename and remove for the two new kinds are three write-guarded routes under `/api/saved-compares`, routed by kind server-side (F6). An empty label is passed through untouched — the store owns the default. |
| **W6** | A comparison row offers **copy gg link — left** and **copy gg link — right** (F9), through `copyLink()` so both record. A pair row offers **copy gg link** (its one link). |
| **W7** | A `@<a>..<b>` navigate lands through the door, not `/api/compare?revs=1`. The server builds both link texts from the two shas (§3.1, form 3); the client sends no link text it would have to compose. |
| **W8** | One plan, nine tasks. The Previews tab does not split off: without it a saved comparison has no way back in. |
| **W9** | Review notes on a web pair row stay **parked**. `armPreview` is keyed on branch names; pair notes are keyed on empty names and armed by message. A link comparison, like every compare but a merge preview, has notes off (`diffCtx.compare`). |
| **W10** | The dialog keeps no preference. Nothing goes to `/api/uistate`; nothing goes to browser storage. |

---

## 3. Design

### 3.1 `GET /api/compare-links`

Exactly one input form; two or none is 400.

| form | query | the server compares |
|---|---|---|
| texts | `left=<link>&right=<link>` | the two texts as given |
| saved | `id=<saved id>` | a comparison's stored texts; for a commit PAIR (`@A..B`), the point link of `A` against the stored pair link |
| pair | `a=<full hex>&b=<full hex>` | the point link of `a` against `@a..b`, both built with `model.Link{…}.String()` over `domain.LinkRepo` |

Forms 2 and 3 share one helper, `pairSides(repo, a, b) (left, right string)`.
A merge-preview id under form 2 is 404: previews open through `/api/preview/open`.

Link text is **not** run through `isGitArgSafe` — it holds `://`, `@` and `?`.
The door parses it; nothing reaches argv unparsed.

Response:

```json
{
  "left":  {"text": "gg://…", "desc": "branch: feat/x", "spec": "commit:<hex>"},
  "right": {"text": "gg://…", "desc": "stash: wip",     "spec": "commit:<hex>"},
  "label": "only for form 2",
  "files": [
    {"path": "a.go", "status": "M"},
    {"path": "scratch.txt", "status": "M", "right_spec": "commit:<third parent>"},
    {"path": "new.go", "status": "R", "old_path": "old.go"}
  ]
}
```

`spec` is the side's `FileSet.Endpoint()`; `left_spec` / `right_spec` appear
on a row only when `Left.Source(old_path or path)` / `Right.Source(path)`
differs from it. `linkSideSpec(ep)` maps per KIND with no default arm (the
`commitEntrySide` rule): worktree → `worktree`, index → `staged`, shelf →
`shelf:<id>`, commit → `commit:<hash>`, pair → `commit:<b>`. A ref or the
invalid endpoint is a server bug: 500.

Errors:

| cause | status | body |
|---|---|---|
| `*LinkSideError`, cause `model.ErrLink` | 400 | `{"error", "side": "left"\|"right"}` |
| `*LinkSideError`, any other cause (cross-repo, unknown repo, ambiguous, no merge base, commit gone) | 422 | same |
| a bare error from `CompareSets` | 500 | `{"error"}` |
| form 2, unknown id | 404 | `{"error"}` |

### 3.2 The view

`openLinkCompare(body)` sets `state.compare = {a: left.desc, b: right.desc,
aSpec, bSpec, links: {left: left.text, right: right.text}, all: files,
filter: "all", originsError: ""}`, clears `state.previewOpen`, and titles the
pane `<left desc> ↔ <right desc>` (form 2: the label first).

`openFile` gains one arm, **before** the frozen arm: when
`state.compare.links` is set it calls `openEntryFileDiff` with
`left: f.left_spec || aSpec`, `right: f.right_spec || bSpec`, `path`,
`old_path`, `status`. `/api/entry-diff` reads its left side at `old_path`
when one is given (F3).

The compare bar shows `all (N)` — a link comparison has no origin sets — and,
when `links` is set, a **save comparison…** chip (§3.5).

### 3.3 `GET /api/link-base`

| query | answer |
|---|---|
| `link=<text>` | `{"kind": "none"\|"ref"\|"commit", "base": "main", "why": "upstream"\|"trunk"\|"parent"\|""}` — `SuggestBase`'s LOCATED kind. An unparseable link is `{"kind":"none"}`, 200: the row simply is not there; the comparison reports the grammar. |
| `link=<text>&base=<b>` | `{"link": "<rewritten text>"}` via `WithBase(b, suggestion.Self)`; a refusal is 422 `{"error"}`. `Self` never crosses the wire: the client cannot supply a wrong one. |

Both are reads; neither is write-guarded.

### 3.4 The dialog

`static/linkcompare.js`, overlay `#linkcmp` in `index.html` with its own
`#linkcmp.hidden` rule, joined to the layer stack (`pushLayer` / `closeLayer`),
opened from the palette and the `menu` rows as **compare with link…**.

- Two link inputs. Under each, a base row (`#linkcmp-base-l/-r`), hidden
  unless `/api/link-base` says the kind is not `none`. The lookup is debounced
  per field, runs through `runOnce`, and a late answer for text the field no
  longer holds is dropped.
- The base input is prefilled with the suggestion and labelled with `why`.
  **Nothing is rewritten until the user presses enter on the base row** (or
  clicks its *bound* button). Then the link field takes the server's text and
  the row hides, because the field is now bounded.
- `↓` in a link field (or its ▾ button) opens that field's history list from
  `GET /api/linkhist`. A pick FILLS the field and never submits.
- **swap** exchanges the two fields, their base rows and their errors.
- `enter` in link 1 moves to link 2; in link 2 it submits. Submit with an
  empty side is refused inline, with no request.
- A failure renders under the field `side` names (F4); a bare failure renders
  under the form. `esc` closes; the form's text survives a re-open within the
  page's life.
- Its own input guard comes FIRST in `onKey` (the help overlay's lesson):
  typing in a field must not reach the page's single-key bindings.

### 3.5 Saving

**save comparison…** prompts for a label (empty by default) and POSTs
`/api/saved-compares {left, right, label}`. 409 → `already saved as <label>`
as a status line, not an error. Success emits `previews`.

### 3.6 The Previews tab

`GET /api/saved-compares` →

```json
{"entries": [
  {"id","label","kind":"pair","link","a","b","state":"ok"|"missing-a"|"missing-b","files":3},
  {"id","label","kind":"compare","left","right","left_desc","right_desc"}
], "disabled": false}
```

`kind` is decided by link SHAPE (a SET holding a preview is skipped — it is
`/api/preview`'s; a SET holding a pair is `pair`; two links is `compare`). A
comparison row carries no live summary: evaluating two arbitrary links per
row per refresh is unbounded work, and the TUI row carries none either.

| kind | row | click | menu |
|---|---|---|---|
| merge | unchanged | unchanged | unchanged |
| pair | `<label>  <a8>..<b8>  N files` | `/api/compare-links?id=` | rename… · save reversed · copy gg link · remove |
| compare | `<label>  <left desc> ↔ <right desc>` | `/api/compare-links?id=` | rename… · save reversed · copy gg link — left · copy gg link — right · remove |

`POST /api/saved-compares` takes `{left,right,label}` (→ `SavedCompareAdd`)
or `{a,b,label}` (→ `PairAdd`); `POST /api/saved-compares/rename {id,label}`
and `DELETE /api/saved-compares?id=` look the entry up, route a pair to
`PairRename/Remove`, a comparison to `SavedCompareRename/Remove`, and answer
404 for a merge preview's id (F6). All three emit `previews`; the client
refetches both lists on that event.

### 3.7 Pair landing (W7)

`live.js`'s `pair` arm calls `/api/compare-links?a=&b=` when both halves are
full shas — what `domain.finishLink` always sends — and keeps today's
`openCompareForPair` for the defensive name case, which the door cannot spell
without a link text.

---

## 4. Errors

| case | behaviour |
|---|---|
| unparseable field | no base row; on submit, the grammar error under that field |
| another checkout, in the registry | under that field: cross-repository compare is not supported yet |
| another checkout, not in the registry | under that field: unknown repository |
| no merge base for `@B...A` | under that field |
| base refused (a ref bounded by itself, a short sha, a name the grammar cannot hold) | under the base row; the link field is untouched |
| saved comparison whose link no longer resolves | the row stays; the click reports; rename and remove still work |
| `/api/linkhist` empty or disabled | the list says "no copied links yet"; the field still works |
| saved-compare store disabled | the two new kinds are absent; merge rows unaffected |

---

## 5. Testing — what each gate must be unable to pass

| subject | the fixture that lets the test see it |
|---|---|
| the web goes through the door | a **local-form FILE link** on one side: the response must list ONE file. Break = call `EvalLink` on the parsed link (the archtest also forbids it). |
| per-member sources | a `-u` stash with one tracked edit and one untracked file, the untracked path REWRITTEN in the working tree so the row is `M` and bytes must be read. Assert the row's `right_spec` differs from `right.spec`, and that `/api/entry-diff` with it returns the stash's bytes. Break = drop the per-row spec. |
| `old_path` | an unbounded × unbounded rename; the left cell must hold the OLD file's bytes. Break = ignore `old_path`. |
| side of an error | bad left + good right, then swapped: `side` must flip. One fixture, two arms that must disagree. |
| target-first rewrite | a ref on the left and a commit on the right; assert the TEXT `…@main...feat/x` and `…@<parent>..<full sha>`. Break = swap `Source`/`Target`. |
| located kind | a local-form file link at a ref → `kind: "none"`, where the pure `BoundKind` says `ref`. Break = answer from `l.BoundKind()`. |
| nothing rewritten untouched | browser: tab through a base row, submit → the request's `left` is byte-identical to what was typed. |
| kind routing | one store holding a merge preview, a pair and a comparison; rename the comparison through `/api/saved-compares/rename` and assert the other two labels did not move; then try the MERGE id there → 404, label unmoved. Break = call `SavedCompareRename` unguarded. |
| every new mutating route | `TestEveryMutatingRouteIsWriteGuarded` (existing) |
| every new copy row records | `TestEveryCopyGGLinkRowRecords` (existing; F9). Break = label `copy left link`. |
| visibility | the browser probe asserts computed `display` of `#linkcmp`, each base row and each error line, **against the unfixed build first, twice**, on a port and binary proven to be the probe's own. |

---

## 5a. As built

- `GET /api/link-base` answers `{"kind":"none"}` not only for an unparseable
  link but for any link `SuggestBase` cannot answer yet (a sha that does not
  resolve while it is being typed). With `&base=` the same conditions are 422.
- `/api/compare-links`' error `error` field carries the CAUSE's message, without
  `LinkSideError`'s `left: ` prefix — `side` says it.
- The classifier is `savedEntry`, and it asks domain (`PairGet`,
  `SavedCompare.IsSet`) instead of re-parsing link shapes; it also requires an
  EXACT id, since domain's getters accept a label too.
- `POST /api/saved-compares` refuses a lone `left`: `SavedCompareAdd` would
  store it as a one-link SET.
- Tasks 6–8 landed as one commit: they share `savedEntry` and one file.
- The file menu in a link comparison drops the rows that need one revision
  (history, blame, bookmark, shelf) — found while wiring the view.
- `openPrompt` gained `allowEmpty`; an empty label must reach the store.

**Found, not fixed (domain, pre-existing — 3c changes no domain code).**
`domain.linkDescFields` tests the target (`Ref`, `Commit`) before the `Path`, so
a FILE link at a ref describes as `branch: feat/x`, not as the file. A
comparison of `@ref:feat/x` against `/a.txt@ref:feat/x` is therefore titled
`branch: feat/x ↔ branch: feat/x` — in the web AND in the TUI, which share the
function — and the copied-link history shows the same. The comparison itself is
right (the file link narrows to one file); only the title is. The web's handler
test asserts a side's description is non-empty, which cannot see this. A fix
belongs in `DescribeLink`, with its own test on a file-at-ref link.

## 6. Out of scope

Cross-repository compare · pair notes in the web (W9) · the 200-row cap on
`/api/bookmarks` and `/api/shelf` · renaming the `Preview*` seams · a JS link
parser · a live re-open of a link comparison when a ref under it moves.
