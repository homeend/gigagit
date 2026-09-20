# Review notes on a saved commit pair — design

Date: 2026-09-20 · Branch: `feat/pair-notes` (off main `514c303b`) ·
Follows: `2026-09-19-saved-commit-pairs-design.md` §7 ·
Builds on: notes in merge previews (feature A, `internal/domain/previewnotes.go`)

## 1. Goal

An agent is handed a commit-pair link (`gg://<repo>@<A>..<B>`), annotates the
diff with notes, and the user reads those notes inside the pair — in the saved
pair opened from the Previews tab, or in the pair opened from the link itself.

## 2. Rules already decided (not re-opened here)

- A pair note is an **ordinary committed note on commit B, new side**. Nothing
  new is stored. It is visible wherever B is shown, and it survives removing
  the saved entry.
- The old side (A) is **not addressable**: `ErrPreviewOldSide`, exit 2 — the
  same rule as a merge preview and `gg review A..B`.
- **Generalize, don't fork**: a pair's note scope is a
  `PreviewNoteSet{Tip: B, Base: A, Commits: A..B}` with `Source`/`Target`
  empty. One resolver, one loader, one counts cache — all reused unchanged.
- Out of scope (owned elsewhere / blocked): the `?preview=<id>` hint, the
  set×set compare UI (plan 3b-2, `feat/links-compare`), the web Previews tab
  and MCP pair *listing* (plan 3c).

## 3. What the code looks like today (verified 2026-09-20)

- Nothing keys on `Source != ""`. The real sweep is the **five readers of
  `set.Source`/`set.Target`** — `cli/previewflag.go:93`,
  `domain/forge_notes.go:72`, `tui/link.go:215,229,231` — plus every
  `res.Preview != nil` / `previewTargetFromLink` dispatch
  (`cli/note.go:331,658`, `cli/noteapply.go:114`, `cli/session.go` highlight,
  `cli/linkconsume.go:47`).
- `loadPreviewNotes`, `PreviewNotesFor/At/All`, `PreviewNoteCounts`,
  `PreviewHunkAnchor` read only `Tip`/`Base`/`Commits` — they work for a pair
  set as they are.
- TUI: a diff opened from a compare file list is stamped note-addressable iff
  `m.filesPreviewSet != nil` (`files_view.go:833`). Badges, `}`/`{`, `c`, the
  old-side refusals all hang off that one pointer. A pair open leaves it nil.
- The file-list badge refresh after a note write lives under `m.previewOpen`
  only (`preview_open.go:275-295`); a pair has no `previewOpen`.
- `steerNavigatePair` (`steer_nav.go:460`) — the landing of `gg open <pair
  link>` and of the `#` paste — opens a plain compare too. **`feat/links-compare`
  rewrites this function's body** (it lands through a set-shaped view; the two
  endpoints stay commit A and commit B). This design must not depend on how
  that view is opened.
- Grammar (probed): `gg://<repo>/<path>@<A>..<B>:42` and `#3` already parse
  and `gg diff` accepts them; `:old:` parses on a pair link as well.

## 4. Design

### 4.1 Domain — `internal/domain/pairnotes.go` (new)

```go
// IsPair: the set was built from two commits, not from a branch pair.
func (set PreviewNoteSet) IsPair() bool { return set.OK() && set.Source == "" }

// PairNotes is a commit pair's note scope. Entry-independent: nothing reads
// the savedcompare store, so an unsaved pair link gets the same scope.
func (s *Service) PairNotes(ctx context.Context, a, b string) (PreviewNoteSet, error)
```

- `a`, `b` are resolved with `ResolveRev` to full shas; a missing commit gives
  the ZERO set and no error (the preview lane's ruling 6: no set, no badge).
- `Commits = RevListRange(A, B)`, cached in the `"preview"` cache under
  `pair-revlist:<A>:<B>`.
- **Guard: B is always a member of `Commits`.** `A..B` is empty when B is an
  ancestor of A (a reversed save — `previewSwapCmd` produces exactly that), and
  then the write target's own notes would never be gathered. `PairNotes`
  prepends B when absent.
- `DiffSpec()` needs no change: `A..B` two-dot is the pair's patch, the same
  one `PairOpen` counts files over.
- A pair note is never "outdated" because the *entry* moved (the shas are
  frozen). A note on an intermediate commit of `A..B` whose lines B changed
  still resolves as outdated — correct, and the same resolver says so.

```go
// NoteScopeResolve is THE parser of a --preview argument and of MCP's
// `preview` arg. It replaces the (source,target)-returning PreviewResolve at
// both callers, so CLI and MCP cannot disagree.
func (s *Service) NoteScopeResolve(ctx context.Context, spec string) (PreviewNoteSet, error)
```

Order: `<target>...<source>` → merge preview · `<a>..<b>` → pair · otherwise
an id/label, `PreviewGet` first, then `PairGet` on `ErrPreviewNotFound` (the
order `gg preview show/rm/rename` already use). A pair that is not previewable
here is an error with the pair lane's words (`missing commit: <sha>`), a
preview keeps today's messages byte for byte. `PreviewResolve` stays for its
remaining caller(s) if any; if none remain it is deleted.

`forge_notes.go:72`: `ParsePRRef("")` must answer !ok for a pair set — asserted
by a test, no code change expected.

### 4.2 CLI

One new adapter, `internal/cli/notescope.go`:

```go
// noteScopeFromLink: the previewTarget for a resolved link — from
// res.Preview (as today) OR built from res.Pair via svc.PairNotes.
func noteScopeFromLink(ctx context.Context, svc *domain.Service, res domain.Resolved) (previewTarget, bool, error)
```

- **`domain.Resolved.Preview` is NOT populated for a pair link.**
  `linknav`, `tui/steer.go:273` and `web/steer.go:133` dispatch on it and
  refuse a preview with empty names; `gg open <pair link>` keeps riding
  `State:"pair"` untouched.
- `linkShapes{Ref: true, Pair: true}` on every `gg note` verb
  (`note.go:46`) and on `gg session highlight add` (`session.go:645`).
  **`gg show` stays `{Ref: true}` → exit 2.** Each note-verb site swaps
  `previewTargetFromLink(*link)` for `noteScopeFromLink` — a dispatch-line
  edit; the verbs then run the existing preview lane: gather along `A..B`,
  write to B new side, `#N` numbered over `A..B` via `PreviewHunkAnchor`.
- **As built (amended 2026-09-20):** `linkDiffSpec` KEEPS its own `res.Pair`
  arm — routing it through the adapter would cost a rev-list on every plain
  `gg diff <pair link>`, which needs no notes. Both yield the identical
  `DiffSpec{Rev: "<A>..<B>"}`; `TestPairLinkDiffSpecEqualsItsNoteScope` pins
  them equal so hunk numbers cannot drift.
- A pair link carrying `:old:` on a note verb or highlight → exit 2 with
  `ErrPreviewOldSide`. (`gg diff` keeps accepting it; it anchors nothing.)
- `previewTarget.Source/Target` are empty for a pair; any message that prints
  them gets a pair arm printing `<a7>..<b7>` (sites enumerated in the plan).
- `--preview` (`previewflag.go`): `resolvePreviewTarget` collapses onto
  `NoteScopeResolve`; help text becomes
  `<id>, <label>, <target>...<source> or <a>..<b>`. This covers `gg diff`,
  `gg note add|list|apply`, `gg review` — and makes **`gg preview diff <pair>`
  work** (the leftover from the last stage), since it shares the parser.
- Highlight: a pair link's steer target is already commit B
  (`targetOf(res.Addr)`), so the band lands on the pair diff once that view is
  stamped with `noteAddr.Commit == B` (4.4). `#N` on a highlight uses
  `PreviewHunkAnchor` through the same adapter.

### 4.3 MCP

`mcp/notes.go previewSet` collapses onto `NoteScopeResolve`; the `preview` arg
doc gains the pair forms. Apply keeps SKIPPING old-side items with
`skipped`/`warning`, as for a preview. No pair *listing* tool (plan 3c).

### 4.4 TUI — `internal/tui/saved_pair_notes.go` (new)

Arming is **message-based and opener-independent**, so it survives
`feat/links-compare`'s rewrite of the landing:

```go
type pairNotesMsg struct{ a, b string; set domain.PreviewNoteSet; counts map[string]int; gen int }
func (m Model) pairNotesCmd(a, b string) tea.Cmd   // PairNotes + PreviewNoteCounts, off-thread
```

- The handler arms `filesPreviewSet`/`filesPreviewCounts` iff the gen matches
  AND the view on screen is still a compare whose endpoints are commit `a` and
  commit `b` (`m.filesLeft.Hash()`, `m.filesRight.Hash()`) — never a tag
  comparison, because links-compare changes the tag.
- Dispatched from exactly two places, one line each:
  `handlePairOpenMsg` (saved row) and `steerNavigatePair` (link landing, `#`
  paste, `gg open`). The second is the only line this branch adds to a function
  another branch rewrites — a known, trivial merge touchpoint.
- A diff opened in the first frame before the set arrives would be unstamped;
  the handler therefore also stamps an already-open diff layer of that view and
  fires `loadNotesCmd` (same move `handlePreviewOpenMsg` makes for counts).
- **Refresh after a note write**: on the notes-source reload, when
  `filesPreviewSet.IsPair()`, re-dispatch `pairNotesCmd` (counts only change).
  No `previewOpenState` for a pair: there is no tip to follow, nothing to
  re-resolve, no "moved" notice.
- `closeFilesView` already clears both fields — unchanged.
- **Badges**: `previewRow` gains the pair's root-note count (`readPreviews`
  calls `PairNotes` + `PreviewNoteCounts` for an OK pair, as it does for a merge
  row); the row renders the same `◆N`. File-list badges, `}`/`{`, `c`, note
  boxes, `(outdated)`/`⊘`, remove-all `(N of M)` then work through the
  existing `filesPreviewSet` gates with no further change.
- **Links** (`tui/link.go:215,229,231`): one helper
  `m.scopeLinkFor(set, path, line)` — a merge set → `previewLinkFor(Source,
  Target, …)`, a pair set → a pair link with path and line
  (`pairLinkFor` grows `(a, b, path, line)`; the bare form is path `""`). A
  deletion row copies line 0, as in a preview.
- Steering refusals (`steer_attn.go:144`, `previewNoteScope`) key on `set.Tip`
  and need no change.

No new user-visible strings are expected; if one appears it gets all four
bundles. Help gains one line under Previews: notes work in a saved diff.

### 4.5 Docs / skills

`CHANGELOG.md`, `README.md` (notes on a saved diff), `docs/CLAUDE-details.md`
(pair-notes paragraph + the B-in-Commits guard), `using-gg.md` (the pair forms
on `gg note`, `--preview`, highlight; "a saved diff follows the preview rule:
new side only") and `reviewing-with-gg.md` (hand back a pair link) — both
versions bumped; `.claude/skills` copies regenerated by a throwaway test.

## 5. Testing

Domain (`pairnotes_test.go`, parallel):
- note on B and on an intermediate commit are gathered; a note on A, an
  old-side note on B, and a note on a commit outside `A..B` are not.
- **reversed pair**: B ancestor of A → `Commits == [B]`, a note on B is
  gathered — watched failing with the prepend guard removed.
- missing commit → zero set, nil error.
- **disagreement fixture** (one repo: branch `feat` with tip B, merge-base A):
  `PreviewNotes(feat, main)` and `PairNotes(A, B)` have identical
  Tip/Base/Commits, yet `IsPair()` differs — and the adapters downstream must
  emit a preview link vs a pair link, a `preview` vs a `pair` steer target.
- `NoteScopeResolve`: `...`, `..`, preview id, pair id, pair label, unknown →
  `ErrPreviewNotFound`; a label shared by both kinds resolves to the preview.
- `forgeNotesFor(pairSet)` is empty.

CLI (`note_pair_test.go`): `gg note add <pair link>:N` writes to B new side;
`note list <bare pair link>` lists all paths; `#N` numbers over `A..B` (a
fixture where B's own `B^..B` hunk numbering differs); delete-only hunk and
`:old:` → exit 2; `gg show <pair link>` still exit 2 (guard watched failing
with `Pair: true` wrongly set); `--preview <a>..<b>` and `--preview <pair id>`
equal the link form byte for byte; `gg preview diff <pair id>`; highlight add.
e2e `s97_pair_notes.toml`: preview add a..b → note add via link → note list
--preview <id> → rm entry → note still on B.

MCP: note add/list with `preview: "<a>..<b>"` and a pair id.

TUI (`saved_pair_notes_test.go`): open saved pair → set armed, file badge,
diff stamped with Commit B; stale `pairNotesMsg` (other endpoints / old gen)
dropped — watched failing with the endpoint gate removed; counts refresh after
a note mutation; pair row `◆N`; `contextLinkText` yields a pair link with
path:line in a pair diff and a preview link in the twin preview fixture;
steered pair landing arms the set. Headless smoke with `tui-capture.sh`:
saved pair → file → note box visible, `}` lands on it.

Gate: re-merge main, build the merged tree, `./test.sh race` on it.

## 5.1 As-built notes (2026-09-20)

- The late scope (4.4) needed a second half: `diffMsg` replaces the whole view
  with the loader's snapshot, which predates the stamp — the `diffMsg` arm now
  keeps a note stamp that landed on the live view while the load was in flight.
  `v.compare` is false for an added file, so the live diff is matched by its
  `cmp:<left>:<right>:` tag prefix instead.
- `gg link --preview <pair>` emits the change-set link (it sets `--pair`).
- A highlight band needs commit `b` in the loaded feed (`resolveAttnKey`), the
  pre-existing rule a merge preview's tip already lives under; unchanged here.
- Selected-row overflow in the Previews tab: not reproduced at 150 columns with
  a `◆N` pair row; left in the backlog.

## 6. Risks

- `feat/links-compare` lands first: one-line conflict in `steerNavigatePair`;
  the endpoint-based gate (not tag-based) is what keeps the handler valid in
  its set-shaped view. If that view's endpoints stop being two commits, the
  gate simply never arms — notes inert, nothing broken — and the fix is theirs
  to coordinate.
- `readPreviews` cost: +1 cached rev-list per OK pair row per previews read
  (the known parked cost for merge rows; same mitigation later).
- Pre-existing selected-row overflow in the Previews tab: a `◆N` suffix makes
  it one glyph worse. Fixed here only if the cause is a one-line width clamp;
  otherwise left in the backlog.
