# Saved commit pairs — a second kind of saved change-set

Date: 2026-09-19 · Branch: `feat/saved-pairs` · Status: DRAFT for review

Builds on `2026-09-09-merge-preview-design.md`, `2026-09-10-preview-notes-design.md`
and `2026-09-16-unified-links-design.md` (whose §4.5 single-store fold stays
parked — see §9).

---

## 1. Problem

A merge preview (`target...source`, branch NAMES, recomputed from live tips)
is today the only change-set a user can save, name, annotate and hand to an
agent. The other change-set gg already shows — the diff between two marked
commits (`m`, `m`, `.` → Compare selection) — is throwaway: close the view and
it is gone, it has no entry in the Previews tab, and it cannot carry notes.

The user's goal (2026-09-19, verbatim intent): *compare some change set with
some other change set and check how much they differ* — a previous version of
a branch against a merge preview; three agents given the same job, compared
against each other. A commit-pair diff "is the same as a merge request to some
extent", so the two kinds must be **complementary**: saved in one place,
linked the same way, annotated the same way, and — next phase — comparable
with each other.

## 2. Goals

1. Save the diff between two commits as a named entry beside merge previews.
2. Point at it with a `gg://` link that lands ON the saved entry, so an agent
   can be told "this diff contains those changes, check them and do X".
3. Review notes on a saved pair, through the same lane previews use.
4. Shape every piece so that **set × set comparison of any two saved entries**
   (§8, next phase) needs no store, listing or link change.

### Non-goals

- Set × set comparison UI (deferred to the next phase; designed-for in §8).
- Live / moving pairs. A moving comparison is what a merge preview is.
- Saving a 3+ commit ◉ range, or a pair with ◇ Working tree / ◇ Staged.
- Folding previews and pairs into one `savedcompare` store (§9).
- Entry-scoped notes (§6.1).

## 3. Rulings (user, 2026-09-19 — do not re-ask)

| # | ruling |
|---|---|
| P1 | Pairs are a **second record kind beside** merge previews. `previews.toml` and `MergePreview` are untouched; no migration, nothing discarded. |
| P2 | A pair is **two full shas, frozen at save time, always** — even when a marked commit was a branch tip. |
| P3 | The entry's link carries a new **`?preview=<id>` hint** so opening it lands on the saved entry. |
| P4 | Saved pairs **support review notes**. |
| P5 | Set × set compare moves to the **next phase**, but this design must make it cheap. |
| P6 | Save gesture = **`m` + `m` then the `.` menu**. `space` is unchanged: it always opens the diff, never saves. |

## 4. Model

### 4.1 The record

```go
// internal/model/pair.go
type SavedPair struct {
    ID      string    `toml:"id"`    // sha256(A + "\x00" + B)[:8] — direction-sensitive
    A       string    `toml:"a"`     // full 40-hex sha: the OLD side
    B       string    `toml:"b"`     // full 40-hex sha: the NEW side
    Label   string    `toml:"label"` // default "<a7>..<b7> <subject of B>"
    Created time.Time `toml:"created"`
}
```

The id is a pure function of the address, exactly like `preview.ID`, so it is
identical on every machine — that is what lets a hint carry it (§5).
`A..B` and `B..A` are two entries, as `main→feat` and `feat→main` are today.

### 4.2 The store — `internal/pairstore` (new DAG leaf)

A copy of `internal/preview`'s shape: `Store` interface (`Add` → `ErrExists`
+ the stored record, `Get`, `List` insertion-ordered, `Rename`, `Remove`),
`FileStore` over **`pairs.toml`** under XDG state behind `internal/filelock`.

**Why a separate file and not a second array in `previews.toml`:** an older
`gg` that reads and rewrites `previews.toml` would silently drop an array it
does not know. A separate file cannot be damaged by an old binary, and needs
no format bump or preflight requirement.

Owned by `domain`; frontends never import it (archtest leaf gate, like
`linkhist`). New `stateBaseDir` kind `"pairs"` — a kind string is a directory
on a user's disk; `TestStateBaseDirCallersPassKnownKinds` learns it.

### 4.3 Domain — one listing over both kinds

```go
type ChangeSetKind int // ChangeSetInvalid first, then MergePreviewKind, CommitPairKind

type ChangeSetEntry struct {
    Kind   ChangeSetKind
    ID     string
    Label  string
    Link   model.Link      // @target...source?preview=<id>  |  @<a>..<b>?preview=<id>
    Status ChangeSetStatus // OK | Gone (a branch or a sha no longer resolves)
    Notes  int             // ◆N badge count (0 when notes are disabled)
}

func (s *Service) ChangeSets(ctx) ([]ChangeSetEntry, error) // previews first, then pairs; insertion order within each
func (s *Service) ChangeSetGet(ctx, idOrLabel string) (ChangeSetEntry, error) // ids of the two kinds share one namespace; a collision is a hard error naming both
func (s *Service) PairAdd(ctx, a, b, label string) (model.SavedPair, error)
func (s *Service) PairRename(ctx, id, label string) error
func (s *Service) PairRemove(ctx, id string) error
func (s *Service) PairOpen(ctx, id string) (PreviewEndpoints, error)
```

**The Previews surface of every frontend renders from `ChangeSets`, never
from the two stores.** `Link` is the load-bearing field: every entry, of
either kind, is `EvalLink`-able into a BOUNDED `FileSet`. This is ruling P5's
whole cost — see §8.

`PairAdd` resolves both revs to full shas (CLI callers may pass short shas,
tags or branch names; they freeze at save time), refuses `a == b`, and accepts
unrelated commits (a two-dot diff needs no ancestry). It is a machine-local
file write under a Read reservation — a domain call like `PreviewAdd`, **not**
an engine op.

`PairOpen` returns the same `PreviewEndpoints` shape `PreviewOpen` does
(`left = A`, `right = B`), so the files view opens through the existing
compare path with `compare:true`.

**Gone commits.** A sha that no longer exists (gc, another clone) gives the
entry `Status: Gone`: listed, greyed, not openable; rename / remove / copy
link still work. Never auto-removed.

## 5. Links

### 5.1 Grammar — one new hint kind

`model.linkHintKinds` gains `"preview"`. `LinkHintIDOK` already admits 8 hex.

```
gg://<repo>@<a>..<b>?preview=<id>                 a saved pair
gg://<repo>/<path>@<a>..<b>:42?preview=<id>       a line in it (new side)
gg://<repo>@<target>...<source>?preview=<id>      a saved MERGE PREVIEW — same hint, for symmetry
```

`web/static/links.js` moves in lockstep (`TestLinkDescJSMatchesGo` and the
hint-kind twin).

### 5.2 Meaning — a hint is a place

Unlike `bookmark`/`shelf` ids, a preview id is derivable from the address, so
the hint adds no *content* — it adds the **landing**: "open this as the saved
entry in the Previews surface", versus the bare link's "open this diff".

Consumer rules (both TUI and web, one table-driven test that makes the two
kinds DISAGREE on one fixture — the unified-links lesson):

| situation | behaviour |
|---|---|
| hint id matches the address AND the entry is saved here | reveal + flash the Previews row, then open it |
| hint id matches, entry NOT saved here | open the diff **show-once** (the preview precedent), notice "not saved here — `.` → Save to previews" |
| hint id does NOT match `ID(address)` | hard error, exit 2 — a hand-edited or corrupted link |
| a `?preview=` hint with **no address** | hard error (domain, spec'd in unified-links §6: a hint without an address must be the only content source, and this one carries none) |

`linknav.RepoOnly` is already false for any hinted link; no change.

### 5.3 Per-verb acceptance (ruling R4, amended)

| verb | `@ref:` | `@<a>..<b>` | change |
|---|---|---|---|
| `gg diff`, `gg open`, `gg session navigate`, `gg compare` | yes | yes | — |
| every `gg note` verb (`note.go:46`), `gg session highlight` (`session.go:645`) | yes | **yes** | **was exit 2** |
| `gg show` | yes | exit 2 ("use gg diff") | — (matches preview links) |

`linkShapes` in the resolver's signature is the mechanism; the note verbs'
declaration flips. A pair link on a note verb is accepted **saved or not** —
the same as `--preview target...source` works on an unsaved preview.

### 5.4 `linkhist` description

```
pair:     3f2a1c9..9c1f2a3 fix auth      gg://gigagit@3f2a…..9c1f…?preview=1a2b3c4d
```

## 6. Notes on a pair

### 6.1 What a pair note IS

A pair note is an **ordinary committed note on commit B, new side** — the
`gg review A..B` range rule, and exactly what a preview note is on its source
tip. Consequences, stated so nobody is surprised:

- it is visible wherever B's diff is shown, not only inside the saved entry;
- removing the entry does not remove its notes;
- the old side (A) is **not addressable** — `ErrPreviewOldSide`, exit 2, on a
  delete-only hunk, identical to previews.

Entry-scoped notes would be a different store and a different design;
out of scope.

### 6.2 Mechanism — generalize, do not fork

`domain.PreviewNoteSet{Source, Target, Tip, Base, Commits}` with
`DiffSpec() = <base>..<tip>` is already the shape a pair needs:

| field | merge preview | saved pair |
|---|---|---|
| `Tip` | source tip | `B` |
| `Base` | merge base | `A` |
| `Commits` | `base..tip` | `A..B` (rev-list; may be empty for unrelated commits → just `B`) |
| `Source`/`Target` | branch names | empty |

One new constructor `PairNotes(ctx, a, b) (PreviewNoteSet, error)`; the one
resolver and one loader (`PreviewNotesFor/At/All`, `PreviewNoteCounts`,
`PreviewHunkAnchor`) are reused untouched. Because `Tip` is frozen, a pair
note never becomes *outdated* by the entry moving; notes gathered from earlier
commits in `A..B` still resolve by fingerprint → active / moved / outdated.

Code keyed on "is this a preview" by `Source != ""` must be found and re-keyed
on the set being non-zero (`Tip != ""`) — a sweep task in the plan, with a
test that runs one fixture through both kinds.

## 7. Surfaces

### 7.1 TUI

- **Commits panel `.` menu** — two rows, right after *Compare selection*:
  **Save to previews** and **Save reversed to previews**. Shown only when the
  ◉ set is **exactly two commits**; hidden when it holds ◇ Working tree /
  ◇ Staged or any other count. Direction: older → newer by **feed order**
  (the same order *Compare selection* uses), never mark order. `ErrExists` →
  status "already saved as <label>" and the row is revealed. Same two rows in
  the files view's commits-side `.` menu when the open compare is an unsaved
  commit pair (this is also the show-once "save?" affordance of §5.2).
- **Previews tab** renders `ChangeSets`. A pair row: `⇄` kind glyph (previews
  keep theirs), label, `◆N` badge, greyed when Gone. enter = open, `.` menu =
  open / rename / remove (confirm) / copy gg link. `m` stays **unbound** here
  — reserved for §8.
- **Open pair** = files view via the compare path; `filesPreviewSet` armed
  from `PairNotes` so `}`/`{`, `c`, note boxes and `◆N` file badges work as in
  a preview. `contextLinkText` gains the pair arm (heading rows fall back to
  the bare entry link, as previews do).
- `#` paste prompt / `gg open --at`: `steerNavigatePreview` gains the pair arm
  with the §5.2 table.
- Help (`?`) rows + footer advert for the new `.` rows; all strings through
  `i18n.T` in all four bundles; new ops mapped in `opAffectedSources` (the
  Previews source refreshes after add/rename/remove).

### 7.2 CLI

```
gg preview add <a>..<b> [--label L]     # one arg containing ".." = a pair (a refname cannot hold "..")
gg preview add <source> <target>        # unchanged
gg preview list [--json]                # both kinds; a KIND column: preview | pair
gg preview show|rm|rename <id|label>    # either kind, via ChangeSetGet
gg preview diff <id|label|a..b> [--hunks]
gg link --preview <id|label|a..b|target...source> [<path>[:<line>|#<hunk>]]   # emits ?preview=<id> when saved
--preview <…|a..b>                      # on gg diff / note add|list|apply / review — previewflag.go grows the pair arm
```

`gg link resolve` reports `pair <a>..<b>` as today, plus `preview <id>` and
`saved: yes|no`. Exit codes: 2 for a bad spec / mismatched hint / old-side
anchor; 1 for a Gone entry.

### 7.3 MCP

The preview list tool returns both kinds with `kind`, `link`, `status`; the
note tools' `preview` arg accepts a pair spec or id. Read surface + the
existing gated note writes — no new mutation class.

### 7.4 Web

`GET /api/preview` returns `ChangeSets` rows (`kind` added; existing fields
kept for merge previews). `POST /api/preview` accepts `{a, b, label}` beside
`{source, target, label}`; rename / remove / open / diff / notes take either
kind's id. Commit-list: with exactly two commits marked, the commit menu gains
**save to previews** / **save reversed**. Previews group: pair rows, same menu
+ copy gg link (`links.js` `linkFor` pair ctx; `TestEveryCopyGGLinkRowRecords`
covers it). `live.js` `state==="preview"` arm handles the pair + the reveal
flash (`TestRevealHintEntryTargetsHaveAFlashRule` — the row needs a flash CSS
rule that actually paints).

### 7.5 Steer

`steer.Target{State:"preview"}` gains `A`, `B` (mutually exclusive with
`Source`/`Target`; `Commit` stays empty) and the hint id. TUI
`steerEnumRefusal` and web `toSteerWire` validate the exclusivity.

## 8. Designed-for: set × set compare (NEXT phase, not built here)

The goal in §1 lands as: mark one Previews entry (`m`), mark a second → the
files view over

```go
CompareSets(EvalLink(x.Link), EvalLink(y.Link))   // bounded × bounded
```

— the union of both sets' paths; per file, X's new-side bytes vs Y's;
present in one only ⇒ A / D. Three agents = pairwise. Already true at the CLI
today: `gg compare <link> <link>`.

What THIS feature guarantees so that phase is only a gesture + a view:

1. every saved entry, of either kind, exposes an `EvalLink`-able `Link`;
2. frontends list entries through one kind-agnostic `ChangeSets`;
3. `m` is left unbound in the Previews tab / web Previews group;
4. nothing here assumes "a Previews row is a branch pair".

A test pins (1): for each kind, `EvalLink(entry.Link)` is bounded and
`CompareSets` over a pair entry × a preview entry succeeds.

## 9. Relation to unified-links §4.5 (`savedcompare`)

§4.5 planned one store absorbing previews, discarding `previews.toml` on
consent. Ruling P1 chose not to pay that now. This design does not block it:
`ChangeSetEntry` is already the union view §4.5 wanted, and a later fold would
change two stores into one *behind* `ChangeSets` with no frontend change. A
saved COMPARISON (`Right != nil`) remains plan-3 territory.

## 10. Error handling

| case | behaviour |
|---|---|
| `a == b` | refuse: "a pair needs two different commits" |
| rev does not resolve at save | exit 2, names the rev |
| already saved | `ErrExists` → CLI exit 1 with id+label; TUI/web reveal the row |
| entry Gone | listed greyed; open / diff / notes → exit 1 "commit <sha7> is not in this repository" |
| hint id ≠ `ID(address)` | exit 2 |
| id collision between kinds (8 hex, astronomically rare) | `ChangeSetGet` hard-errors naming both; a label disambiguates |
| old-side note anchor | `ErrPreviewOldSide`, exit 2 |
| `pairs.toml` locked / unreadable | the `filelock` + notes-store precedent: error surfaces, no partial write |

## 11. Testing

- `pairstore`: the `preview` store suite + a cross-process lock test that
  holds the lock FILE externally (the store mutex proves nothing).
- domain: `PairAdd` freezes names to shas; Gone status; `ChangeSets` order;
  §8 guarantee test; both-kinds-one-fixture note test (the two arms must
  DISAGREE somewhere — `Source` empty vs set).
- model: `String(Parse(s)) == s` round-trips for the three §5.1 forms; hint
  kind twin in `links.js`.
- Every guard is watched failing **with the guard removed**, not the feature.
- TUI: menu gating table (0/1/2/3 marks, ◇ rows), reveal + show-once + mismatch
  landings; headless `tui-capture.sh` smoke of save → Previews row → open →
  add note → copy link → `#` paste lands on the row.
- CLI/e2e: `s93_saved_pairs.toml` — add, list, link, note add via the link,
  `gg compare <pair link> <preview link>`.
- Web: static-wiring tests + a playwright probe asserting **visibility** of
  the saved row and the flash paint, run against the unfixed build first.

## 12. Phasing — one plan per phase, executed in order by one session

| phase | contents | stands alone because |
|---|---|---|
| **A** | `model.SavedPair`, `pairstore`, domain `ChangeSets`/`Pair*`, TUI save rows + Previews tab, CLI `gg preview` pair arms, MCP list, web save/list/open, bare `@a..b` copy-link | pairs are saveable and openable everywhere; links already work bare |
| **B** | `?preview=<id>` hint: grammar + `links.js`, `steer`, both reveal consumers, show-once landing, `gg link` emits it, `linkhist` desc | links land on the entry |
| **C** | notes on pairs: `PairNotes`, the `Source != ""` sweep, R4 flip for note verbs / highlight / review, TUI + web note lanes in an open pair, MCP note arg | the agent hand-off of §1 is complete |
| *(next)* | §8 set × set compare | — |

Docs per phase: `CHANGELOG.md`, `README.md`, `internal/agentskill/using-gg.md`
(+ `reviewing-with-gg` in C) with a `Version` bump, `docs/CLAUDE-details.md`;
`CLAUDE.md` gets one `pairstore` row in phase A.

## 13. Open questions

None blocking. For the plans: the pair row's kind glyph (`⇄` assumed; must be
single-width — the wide-glyph overflow lesson), and whether `gg preview list`
terse output needs the KIND column first or last.
