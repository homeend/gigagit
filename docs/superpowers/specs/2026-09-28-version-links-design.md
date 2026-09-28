# `gg://` links to branch versions — design

Status: for review · 2026-09-28 · branch `feat/version-links`

Parents: `2026-09-16-unified-links-design.md` §3.3 (what a hint is: it changes
the LANDING, never the meaning, and it degrades), `2026-09-20-link-preview-hint-design.md`
(the lookup-by-id-else-by-set ladder this copies), the branch-versions store
(`refs/gg/versions/<branch>/<unix>-<op>`, `docs/CLAUDE-details.md`). Nothing
here re-opens a ruling; §6 lists the decisions that are new.

## 1. Problem

After a merge, rebase or pull gg may raise the `!` notice "<branch>'s change
set may have drifted from its recorded version" and list the paths it now
introduces. The recorded version is one row of the Branch versions popup, and
`enter` on that row opens the frozen preview gg recorded before the operation
(merge base → the branch's contribution at the time). There is no way to hand
that place to anyone: the popup copies a sha (`y`), the notice only says "open
Branch versions", and `gg versions` prints an id nobody can paste into
`gg diff`. The user's flow — Branches `.` menu → Previous versions… → pick a
version → copy a link, paste it to an LLM or a teammate's session — ends one
step short.

The spike (memory `version-links-drift-research`) found the grammar already
carries the preview: it IS the pair link `gg://<repo>@<base>..<ours>`. What is
missing is (a) a producer, and (b) a landing hint, so that opening the link
does not stop at a bare diff but reveals the Branch versions row it came from.

## 2. What ships

One link per recorded two-branch version — its PREVIEW, exactly what `enter`
opens — carrying one new hint kind, `version`:

```
gg://<repo>@<base>..<ours>?version=<unix>-<op>
```

`<base>` and `<ours>` are the record's full shas (`model.BranchVersion.Base`
/ `.Ours`); `<unix>-<op>` is the id `gg versions` already prints (the last
path element of the ref). A one-branch record (amend, reset, undo-commit,
delete-branch, restore) has no `Base`/`Ours` and gets NO link: every producer
says "this version records no preview" (`domain.ErrNoPreview`'s words).

### 2.1 Grammar (model)

- `linkHintKinds` gains `"version"` — the one definition `ParseLink`, every
  producer, both steer validators (they already call `model.LinkHintKindOK`)
  and `links.js`'s kind gate share. `parseLinkHint`'s "unknown hint kind"
  message lists it. The Go↔JS gate (`linksjs_test.go`) gets a `version` row.
- The id is `<unix>-<op>`: digits, a dash, an op token (`merge`, `rebase`,
  `pull`, `interactive-rebase`, …). `LinkHintIDOK` admits it as is — no branch
  name rides the hint, because hint ids reject `/` and branch names carry it.
  `model` gains `func (v BranchVersion) ID() string`, the ONE spelling of the
  id; `cli/versions.go`'s two `fmt.Sprintf("%d-%s", …)` sites switch to it.
- Parse-time acceptance is broad, like `preview`: any address may carry
  `?version=` and still parses. Only the pair form is ever PRODUCED, and only
  a pair gets the by-shas fallback of §2.2.
- An address-less `?version=<id>` is a hard error in `domain` (the existing
  hint-only default arm), with its own message: "a version hint needs the
  preview it names (@<base>..<ours>); <id> alone names nothing". The record
  is not the link's content; the pair is.

### 2.2 Meaning — same address, different landing

The link lands exactly as the bare pair link does today (a bounded change-set
compare, through `linknav` → `steerNavigatePair` in the TUI, `runLinkCompare`
on the web). AFTERWARDS the consumer asks domain for the record and reveals
the Branch versions surface on that row:

| lookup, in order | result |
|---|---|
| 1. a record whose id is the hint's, AND whose `(Base, Ours)` equal the link's pair | reveal that row |
| 2. a record whose id is the hint's (pair differs — a foreign id that happens to collide) | reveal that row |
| 3. no id match; a record whose `(Base, Ours)` equal the pair, newest first | reveal that row |
| 4. nothing (record deleted with `d`, pruned, or another machine) | notice: `version <id> is not recorded here; the link still landed` — the diff is open, nothing is stored |

Step 1 is not defensive: an id is unique only PER BRANCH (the same-second
bump in `snapshotBranchTipAt` runs against one branch's records), and a pull
records BOTH the pulled branch and the target in the same second with the
same `pull` token (`smart_pull.go:105` and `:155`), so two branches can hold
the same id. The pair breaks the tie; the pair is the link's meaning, the id
only its origin.

The record pins its shas: the snapshot commit's parents are the tip and
`Ours` (`git/versionrecord.go`), so while a record exists its pair resolves.
Once the record is gone AND gc has run, `Ours` may not resolve — that is the
ordinary pair refusal ("<sha> does not resolve here"), raised before any hint
is looked at, and it is the whole story of the link on another machine:
version refs are never pushed and `Ours` is a rewritten tip. **A version link
is machine-local.** The producers do not say so on every copy; `using-gg`
does, once.

A disabled versions store (`ErrFeatureDisabled`) is a MISS (row 4), never a
failure: the diff landed, the hint has nothing to reveal.

### 2.3 The domain query

```go
// FindVersion answers a ?version= hint: the record for id, tie-broken by the
// pair (base, ours); else the newest record freezing that pair. ok=false is a
// miss (unknown id, deleted record, or the store disabled), never an error.
func (s *Service) FindVersion(ctx context.Context, id, base, ours string) (branch string, v model.BranchVersion, ok bool, err error)
```

One `VersionRefs(git.VersionRefPrefix)` over the whole store (the same single
`for-each-ref` `AllVersionBranches` runs), candidates = refs whose last
element is `id`, filtered per §2.2. Used by both consumers, by `DescribeLink`
(§2.5) and by the web door (§2.4). `versionRefForID` in `cli/versions.go`
stays branch-scoped for `gg versions show/restore` — those verbs take a
branch — but switches to `BranchVersion.ID()`.

### 2.4 Consumers

**TUI** — `navigateLanded` gains a `version` arm shaped like `bookmark`/`shelf`,
NOT like `preview`: the versions rows are not a startup source, so the
lookup is a `tea.Cmd` (`loadVersionForHintCmd(id, a, b, gen)` → domain
`FindVersion` + `BranchVersions(branch)`), parked as a `pendingHint` under the
same `hintGen` guard and timeout. A `for-each-ref` never runs on the Update
thread. On arrival with the current generation:

- miss → `m.statusMsg` = the row-4 notice;
- hit → build a `versionsPopup{mode: versionsModeVersions, branch, rows,
  sel: <index of the record>}` — the exact state "popup → enter" leaves
  behind — and, because the landed compare is already open, PARK it:
  `m.filesReturnLayers = []layer{p}` (written directly; `handOffToFilesView`
  arms its park after opening, and here the view is open already). This
  replaces whatever the landing had parked, the "latest opener wins" rule.
  esc/`l` on the compare then restores the popup on that row, a further esc
  closes it (not `fromList`).
- the compare was closed before the load returned (`m.filesView == nil`):
  push the popup live instead; the user sees the row, `enter` reopens the
  preview.
- a newer `hintGen`: drop, as today.

`steerNavigateHintOnly` needs no arm: an address-less version link is refused
by domain before a Command exists (§2.1), and the default refusal covers a
hand-crafted one. `linkOriginText` (`tui/link_compare_desc.go`) gains
`version` → "copied from a recorded version". Every new string is an
`i18n.T` literal present in all four bundles.

**Web** — the page holds no branch to ask `/api/versions?branch=` with, so
the server gets one door, `GET /api/version-find?id=&a=&b=` →
`{found, branch, ref}` over `FindVersion`.
`live.js revealHint` routes `version` BEFORE `revealHintEntry` (whose
unknown-kind path prints "cannot reveal"): a miss is the row-4 `opLine`; a
hit calls `openVersions(branch)` and flashes the `li` whose `_rows[i].ref`
matches (the rows key on `data-i`, not `data-id`, so the match is by ref). The
layer lands ON TOP of the landed compare — the reverse of the TUI's park,
because `openVersionPreview` closes the layer before opening a compare and
the web has no "under" — so esc shows the diff. `#versions-list` joins the
`li.flash` rule and `TestRevealHintEntryTargetsHaveAFlashRule`'s target list.

**Steer validators** (`tui/steer.go`, `web/steer.go`) admit `version` for free
through `LinkHintKindOK`. The doc comments on `steer.Command.HintKind` and
`mcp/links.go` that enumerate kinds are updated; MCP carries `hint_kind`/
`hint_id` through untouched, so `gg_link_resolve` answers a version link
today. **CLI** `gg open` / `gg session navigate` and the TUI `#` paste prompt
all land via `linknav`, which already forwards the hint.

### 2.5 Description

`linkDescFields` gains `version`: when `FindVersion` hits, `version: <branch>
· <op> · <YYYY-MM-DD HH:MM>`; on a miss it falls THROUGH to the pair arms
(`link: gg://…`) rather than printing an id. `links.js`'s formatter twin gains
the same kind; `TestLinkDescJSMatchesGo` pins it. So `gg links` and the
history popup describe a copied version link by what it is.

### 2.6 Producers

| surface | today | with this change |
|---|---|---|
| TUI Branch versions popup, versions mode | `y` copies the sha | `L` copies the preview link (the bookmark popup's key for the same thing). Hint line: `[enter] preview  [r] restore  [d] delete  [y] copy sha  [L] copy link` (all four bundles). One-branch row → status "this version records no preview". |
| TUI `!` drift notice | Dismiss | + `Copy preview link` beside Dismiss, only when the finding names a record with a preview; a paused-only notice (`Ref == ""`) keeps Dismiss alone. `run` builds the link, copies it via `copyToClipboardCmd`, records it (`RecordCopiedLink`) — and the notice STAYS OPEN: `applyNoticeAction` removes the notice for every action today, so `noticeAction` gains a `keep` flag (copying is not dismissing; a copy that destroyed the Added list would be the dismiss-only-surface mistake). Dismiss still removes it. |
| `gg versions <branch>` | `<id> <short> <when> <subject>` | each two-branch row is followed by one indented continuation line holding the link: `  gg://<repo>@<base>..<ours>?version=<id>`. One-branch rows and a checkout with no link form (`LinkRepo` error) get no line. The first column stays the id, the tail stays the subject. |
| CLI post-op drift (`printDrift`) | Added / absorbed lines | + one line after them: `  recorded version: gg://…?version=<id>` — the LLM-facing surface the feature was asked for, and free once `DriftReport` carries the record (below). |
| web versions row menu | open / compare / restore / copy commit id / delete | + `copy gg link` (server-built `link` field on each `/api/versions` row, like the saved-pair row; empty for a one-branch record, and the menu row is then not offered). |

Every producer builds a `model.Link{Repo, Target: {State: committed, Pair:
{A: Base, B: Ours}}, Hint: {version, v.ID()}}` and prints `String()` — never
string concatenation — after `model.CommitEndpoint` has validated both shas:
the ref trailer is split on whitespace and never sha-checked (the popup's
own `onEnter` comment), so an unusable record declines with the existing
"the recorded commit is not usable" rather than emitting a link that cannot
parse.

`DriftReport` gains the record it compared against (`Version
model.BranchVersion`, filled by `DriftSince`, which already holds it): the
notice is a pure builder re-derived on every rebuild, so its action must not
need a load, and `printDrift` reads the same field.

`gg link` gets no `--version` flag: `gg versions` prints the link, and a
version is not something an agent spells by hand.

### 2.7 Docs

`using-gg.md`: the grammar block gains the `?version=` row; the `gg versions`
bullet documents the continuation line and says the link is machine-local
(the ref is never pushed and `ours` is a rewritten tip); the hint paragraph
names the kind. `agentskill.Version` is bumped and `gg init --update` re-run.
`CHANGELOG.md` gets a section; `README.md` gets one line under links.
`docs/CLAUDE-details.md`'s links/versions notes gain the ladder of §2.2.

## 3. Out of scope

A `.` action menu inside the versions popup (no popup has one today; `L` plus
the hint line is the bookmark popup's shape — decision 3) · a web drift-panel
copy button · a before/after link pair reproducing the drift finding (the
spike showed `gg compare` cannot see an M→A resurrection between two bounded
links; the notice keeps gg's own Added list) · a `gg link --version` flag · a
hint on file/line links copied from inside an open version preview (grammar
allows it; nothing produces it) · any MCP surface change · blessing
`@ref:refs/gg/versions/…` (it resolves by accident today and stays that way).

## 4. Tests

- model: parse/render round-trip with the new kind on a pair address; every op
  token the engine records (`merge`, `rebase`, `pull`, `interactive-rebase`,
  `restore`, `amend`, `reset`, `undo-commit`, `delete-branch`) passes
  `LinkHintIDOK` as `<unix>-<op>`; `BranchVersion.ID()` matches the ref's last
  element; an address-less `?version=` is refused by the hint-only arm with
  the specific message.
- domain: `FindVersion` — by id; two branches sharing an id resolve by pair
  (a pull fixture); foreign id with a matching pair; miss; store disabled is
  a miss not an error. `DescribeLink` — label on a hit, fall-through on a
  miss (one fixture, two outcomes). `DriftSince` fills `Version`.
- TUI: `L` copies the link with the hint; `L` on a one-branch row says
  "records no preview"; the drift notice offers the action only with a
  preview-bearing record, and the action copies + records; landing reveals
  the popup parked under the compare on the right row; esc restores it; the
  compare closed before the load → popup pushed live; stale generation dropped;
  miss → notice; `linkOriginText` names the kind; i18n gates.
- CLI: `gg versions` prints the continuation line for a two-branch row and
  none for a one-branch row; `printDrift` prints the recorded-version line;
  `gg link resolve` echoes `hint_kind version`; e2e: `s82_cli_versions.toml`
  gains `stdout_contains = ["?version="]`, and a new `s95_version_links.toml`
  copies the printed link back through `gg diff <link>` and `gg link resolve`.
- web: handler tests for `/api/version-find` (hit, tie-break, miss) and the
  row `link`; node gates (`links.js` kind gate, formatter twin, flash rule);
  a playwright probe run first against a build with the `version` arm
  removed (computed `display` of the versions layer, the flashed row).
- Break table per task, as always.

## 5. Sequence for the plan

1. model (`ID()`, kind, tests) → 2. domain (`FindVersion`, `DriftReport.Version`,
`DescribeLink`) → 3. CLI (`gg versions` line, `printDrift`, e2e) → 4. TUI
producers (`L`, notice action, hint line) → 5. TUI consumer (arm, park) →
6. web (door, reveal, row link, gates, probe) → 7. docs + skill bump.

## 6. Decisions made here (tell me if any is wrong)

1. Parse-time acceptance is broad (any address may carry `?version=`); only
   pairs are produced, only pairs get the by-shas fallback.
2. Address-less `?version=` is a hard error with its own message.
3. No `.` menu in the versions popup: `L` + the hint line, the bookmark
   popup's shape. (The agreed note said "L + . menu"; adding the first popup
   action menu is a surface of its own — say so and it becomes a task.)
4. `gg versions` prints the link as an indented continuation line under each
   two-branch row, keeping `<id>` first and `<subject>` last for existing
   parsers; the alternative was a `--links` flag.
5. `printDrift` (CLI) and the web versions row menu gain the link too — two
   producers beyond the agreed list, both one-liners over the same field.
6. Web reveal = the versions layer pushed on top of the landed compare with
   the row flashed; esc shows the diff.
7. `DriftReport` carries the compared record, so the notice action and
   `printDrift` need no second load.
8. The lookup ladder tie-breaks an id by the pair FIRST (a pull records two
   branches under one id), then falls back to id alone, then to pair alone.
9. `using-gg` is bumped: the grammar row, the `gg versions` continuation line
   and the machine-local caveat.
10. The notice's `Copy preview link` keeps the notice open (`noticeAction.keep`);
    today every action removes the notice, which would make the copy delete
    the very list the user is about to research.
