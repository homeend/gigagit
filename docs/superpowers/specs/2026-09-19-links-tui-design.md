# `gg://` links in the TUI — design for plan 3b

**Date:** 2026-09-19 · **Parent spec:** `docs/superpowers/specs/2026-09-16-unified-links-design.md`
(§3.4, §5.1, §5.2, §8). This document does not reopen any ruling there; it
settles what §5.2's five bullets leave unsaid once they meet the code.

**Status:** design, awaiting review. Everything under *Decisions* is a call I
made so the plan could be written; each says what it costs to overrule.

---

## 1. What reading the code found

§5.2 reads like five menu rows. It is not. Six facts, each verified in the
tree at `ea94b45d`:

**F1 — The TUI never records a copied link.** `domain.RecordLink` has callers
in `cli`, `web` and `mcp` only. `contextLinkRow` (`internal/tui/link.go:209`)
is a plain `copyRow`. §5's one rule — *every "copy gg link" click, on any
surface, appends to the history* — is broken on the surface the user lives
in, so a `#` history picker built today would list only CLI and web copies.

**F2 — The describing text lives where the TUI cannot reach it.** `linkDesc`
and `linkRecordFields` are unexported in `internal/cli/link.go`; `tui` cannot
import `cli`. The web has its own JS twin under a Go↔JS gate
(`TestLinkDescJSMatchesGo`). A third copy in the TUI is the defect shape this
feature keeps paying for.

**F3 — The TUI compare view cannot show a link comparison.**
`openCompareFiles(left, right model.Endpoint)` loads through
`domain.CompareFiles` → `git.DiffTreeFiles`, whose `default:` arm refuses
every pair endpoint. The CLI and MCP compare links through
`EvalLink` → `CompareSets(FileSet, FileSet)`. The view is endpoint-shaped; a
link comparison is set-shaped. *This, not the popup, is the work.*
Two consequences:
- `compareTag` is `CacheTag(left)+CacheTag(right)`. `gg://r@sha` and
  `gg://r/foo.go@sha` share an endpoint, so opening the second after the
  first hits "already showing — keep it" and silently does nothing.
- The per-file diff loader (`loadCompareDiffCmd`) already keys on the row's
  `A`/`D` status and reads `Endpoint.FileRef(path)`, and a pair's `FileRef`
  is its `b` side — so per-file diffs work unchanged once the *list* is right.

**F4 — Three frontends each hand-roll "link text → file set", and they
disagree.** `cli/compare.go:compareLinkSet` parses, **locates** (splits a
local-form link's checkout from its file — its doc comment calls skipping
this "a wrong answer with no error"), refuses another checkout, then
evaluates. `mcp/links.go` parses, compares the repo halves, and evaluates
**without locating**. Suspected consequence: `gg:///abs/checkout/f.go@sha`
through MCP evaluates as the whole tree. The plan's first step there is a
test that proves or refutes it. Either way the TUI must not become a fourth
copy.

**F5 — §3.4's untracked rule was deferred twice and is not implemented.**
Plan 1b's coverage table deferred the third-parent rule "to plan 2 with the
other stash surfaces"; plan 2 shipped no stash surface. `EvalEndpoint`'s pair
arm knows nothing of stashes, so `@<sha>^..<sha>` for a `-u` stash silently
omits the untracked files. 3b ships the first stash producer, so 3b is where
this either lands or is knowingly re-deferred.

**F6 — The bookmark and shelf switchers have no copy-link at all.** They are
key-driven popups with no `.` menu; neither file mentions a link. Yet §5.2
lists both as copy surfaces, and "bookmark↔shelf on links" is unreachable
from the dialog unless an entry's link can be copied. `L` (the diff view's
copy-link key) is free in both.

Also confirmed: there is no "trunk" notion anywhere in `domain`, `git` or
`config` (§5.1 says as much); `StashEntry` carries only `stash@{N}` and a
subject, so a stash link needs a git call and cannot be built in the
synchronous menu build; `PreviewList` returns only SET-shaped rows, so a pair
saved from the TUI would be invisible in the TUI.

---

## 2. Decisions

| # | decision | cost of overruling |
|---|---|---|
| **D1** | **Recording happens at the clipboard chokepoint.** `copyToClipboardCmd` records any copied text that parses as a link. Producers cannot forget, and a new copy row records for free. | Per-row recording needs a gate over every producer instead of one test. |
| **D2** | **`domain.DescribeLink(ctx, l) string`** absorbs `cli.linkDesc` + `cli.linkRecordFields`. CLI, TUI and the JS gate all point at it. A stash-shaped pair describes as `stash: <subject>`. | — |
| **D3** | **Implement §3.4 now** (F5): a pair whose `b` is stash-shaped includes the third parent's files, each read from the third parent. | Re-deferring means the first stash link gg ever produces is wrong for every `-u` stash. |
| **D4** | **A stash link carries no `?stash=N` hint.** Landing a pair link already opens `a ↔ b`, which *is* the stash; §3.4 calls the hint the most fragile in the system. | Add it later; hints are additive. |
| **D5** | **Copy rows also cover remote-tracking branches and tags.** They share the Branches `.` menu anchor and the one producer (`@ref:<name>`); §5.1's table already says "branch / tag tip". | Drop two table rows. |
| **D6** | **One domain door: `domain.CompareLinks(ctx, leftText, rightText, opts)`.** Parse → locate → same-checkout → `EvalLink` ×2 → `CompareSets`. CLI, MCP and TUI all call it (F4). | Three copies of a correctness rule. |
| **D7** | **The Previews panel lists saved pairs in 3b**, not 3c. §4.5 already says "the Previews tab lists both". Without it, *Save comparison* writes rows the TUI cannot show, re-open, rename or remove. | 3b ships a write-only feature until 3c — and 3c is the *web*. |
| **D8** | **bookmark↔shelf: re-plumb two arms, keep one.** commit↔commit goes through the D6 door (same answer, one path). commit↔file, refused today, becomes **legal** (unbounded × one member). **file↔file keeps its two-ref diff**: it compares two arbitrary blobs even when their paths differ, which the key-based algebra deliberately cannot do — on links, `a.go` vs `b.go` is one `D` and one `A`, never a diff. | Routing file↔file through links is a behaviour regression. |
| **D9** | **3b is two plans.** **3b-1** "links in, links out" (F1, F2, F5, F6, copy rows, `#` picker, `gg compare --remove/--rename`). **3b-2** "the dialog" (D6, the set-shaped view, the dialog + base picker, save, D7, D8). §8 already notes the split point: the copy rows and the picker need neither the store nor the dialog. Each ends green and mergeable. | One plan larger than 3a. |
| **D10** | **`gg compare --remove <id\|label>` and `--rename <id\|label> <label>`** ride 3b-1. Both domain verbs exist with no CLI caller. | — |

---

## 3. Design

### 3.1 Describing and recording (D1, D2)

```go
// internal/domain/linkdesc.go
func LinkDesc(kind, id, subject string) string          // moved verbatim from cli
func (s *Service) DescribeLink(ctx, l model.Link) string // fields + LinkDesc; best-effort lookups
func (s *Service) RecordCopiedLink(ctx, text string)     // ParseLink → DescribeLink → RecordLink; a parse failure records nothing
```

`DescribeLink` keeps `linkRecordFields`' priority order (hint → target →
path → `link: <text>`) and adds one arm **before** the fallback: a pair whose
`b` is stash-shaped (§3.3) → `stash: <subject of b>`.

`copyToClipboardCmd(ok, text)` gains, **before** the clipboard write and
regardless of its outcome: `if isLinkText(text) { svc.RecordCopiedLink(ctx,
text) }`. When the clipboard is broken (WSL interop down) the history is the
only place the link survives, which is when it matters most. Best-effort, off
the UI thread, never changes the status line. The write itself goes through a
per-Model `clipWrite` seam so a test can run the command. `internal/tui` has exactly one
call to `clipboard.Copy`; a grep gate keeps it that way, which is what makes
"chokepoint" a fact rather than a hope.

`TestLinkDescJSMatchesGo` repoints at `domain.LinkDesc`.

### 3.2 Copy rows (D4, D5, F6)

One new producer beside `linkFor` / `previewLinkFor`:

```go
func (m Model) refLinkFor(name string) (string, bool)      // gg://repo@ref:<name>; refuses !LinkRefOK
func (m Model) pairLinkFor(a, b string) (string, bool)     // gg://repo@<a>..<b>; both full shas
```

| surface | gesture | link |
|---|---|---|
| Branches / Remotes / Tags row | `.` → Copy link (after "Copy commit id") | `@ref:<name>` — the NAME rides (R2) |
| Stash window row | `.` → Copy link (after "Copy stash ref") | `@<p1>..<sha>`, both resolved |
| Bookmark switcher row | `L` | the bookmark's address + `?bookmark=<id>` |
| Shelf switcher row | `L` | file: `gg://repo/<path>?shelf=<id>` · commit: `gg://repo@<sha>?shelf=<id>` |

`contextLinkText` gains the branch/remote/tag arms (so `gg session`'s
snapshot link follows). The **stash row is the one asynchronous copy row**:
its `run` fires `resolveStashPairCmd(ref)` → `svc.StashPair(ctx, ref)
(parent, sha string, err)` → on the message, build the link and hand it to
`copyToClipboardCmd`. `stash@{N}` is an *input* to the resolve and never
reaches the link; the gate asserts the copied text contains neither
`stash@` nor `{`.

Both switchers' `L` is inert in compare-pick mode, like every other key
there, and is added to their `?` cheat sheets.

### 3.3 Stashes in the algebra (D3, §3.4)

`EvalEndpoint`'s pair arm, after enumerating `a..b`:

1. `b` is **stash-shaped** iff it has exactly three parents, `a` is the
   first, and the third parent is a **root** commit. The root test is
   load-bearing: an octopus merge also has three parents, and on a monorepo
   its third parent's tree is 100k files entering as additions. A stash's
   untracked commit is always parentless; a merged root is not a thing anyone
   has.
2. When stash-shaped, every file in the third parent's tree joins the set
   with `has = true` and a **per-path byte source** of the third parent.

```go
func (f FileSet) Source(path string) model.Endpoint // f.ep unless overridden for this path
```

`compareOne` / `sameBytes` and the TUI's set-shaped diff loader read through
`Source(path)`, never `Endpoint()` directly, for a member's bytes.
`Endpoint()` keeps its meaning (the set's own resolved endpoint).

Cost: one `rev-list --parents -n 1 <b>` per pair evaluation; the root probe
and the tree read only when `b` has three parents.

### 3.4 The history picker (one component, three hosts)

```go
type linkHistPicker struct { rows []histRow; sel int; loaded, active bool }
```

Hosts: the `#` prompt (3b-1) and both dialog fields (3b-2). The host loads it
with a gen-tagged `linkHistCmd` when it opens — never a synchronous read in
`pushLayer`. Rendered under the input as up to 20 rows of
`<desc>  <elided link>`; `↓` from the input enters the list, `↑` from row 0
returns, `enter` on a row **fills the field** and returns focus to it.
In the `#` prompt a fill also submits (it is a navigation prompt; a second
enter buys nothing). Empty history renders one dim line, not an empty box.
`frontends never import linkhist` stays true: the load command copies each
entry into a TUI-local `histRow{link, desc}` at the message boundary, so the
package is never named.

### 3.5 The one door (D6)

```go
type LinkComparison struct {
    Left, Right FileSet            // resolved; Source(path) is the byte source
    LeftText, RightText string     // as given — the identity of this comparison
    Files []model.CommitFile
}
func (s *Service) CompareLinks(ctx, leftText, rightText string, o ResolveOpts) (LinkComparison, error)
```

Errors are typed per side (`*LinkSideError{Side, Err}`) so the CLI keeps its
exit codes (2 for grammar/cross-repo, 1 for runtime) and the dialog can put
the message under the right field. Cross-repository stays refused (§9).
`cli.compareLinkSet` and the MCP tool body shrink to a call. A link mixed
with the CLI's old vocabulary (`@worktree`, `bookmark:<id>`) already maps
onto a link through `compareTokenLink`, so the CLI uses the door for every
pair of tokens it can spell as links and its existing path for the rest.

### 3.6 The set-shaped compare view (F3)

`openLinkCompare(c domain.LinkComparison)` opens the files view in compare
mode with the file list **already in hand**:

- `filesLeft/Right = c.Left.Endpoint(), c.Right.Endpoint()`; a new
  `filesSets *domain.LinkComparison` holds the sets for `Source(path)`.
- `compareTag = "links:" + LeftText + "\x00" + RightText` — the texts, never
  the endpoints (F3's collision).
- Title: `DescribeLink(left) ↔ DescribeLink(right)`.
- The diff loader takes byte sources from `Source(path)` when `filesSets` is
  set, else today's `Endpoint.FileRef`.
- `.` in this view gains **Save comparison…** (3.8). The branch-pair scope
  cycle is unavailable (`comparePair == nil`), as for every non-branch compare.

Dialog submit and this view open in the **same task**: a door with no caller
and a view with no opener are not a mergeable boundary.

### 3.7 The dialog and the base picker (§5.1)

Palette: **"Compare with link…"** (alphabetical, between *Browse remote
branches* and *File blame*). A `linkComparePopup` with two link fields and up
to two base rows.

**Boundability is a pure function of the parsed link**, in `model` so 3c's JS
twin can be gated against it:

```go
type LinkBoundKind int // LinkBoundNone, LinkBoundRef, LinkBoundCommit
func (l Link) BoundKind() LinkBoundKind
```

`Ref != ""` and no path → `LinkBoundRef`; `Commit != ""`, no path, no pair,
no preview → `LinkBoundCommit`; everything else (a path, a pair, a preview,
worktree, index, an unparseable field) → none. A base row is **shown only
when its field's kind is not none**.

The suggestion is one domain call, reused by 3c:

```go
type BaseSuggestion struct{ Base string; Why string } // Why: "upstream" | "trunk" | "parent" | ""
func (s *Service) SuggestBase(ctx, l model.Link) (BaseSuggestion, error)
```

- ref → its upstream; else **trunk** = `refs/remotes/origin/HEAD`'s target,
  else local `main`, else local `master`; else empty (row shown, nothing
  preselected). A base equal to the ref itself is skipped.
- commit → its first parent's full sha.

The base row is a text field prefilled with the suggestion and labelled with
`Why`; it fuzzy-completes against `branchNameCandidates()` exactly as the
preview-add popup does. **Nothing is rewritten until the user presses enter
on the base row.** Then the link field becomes — via `model.Link.String()`,
never concatenation —

| kind | rewrite |
|---|---|
| ref `A`, base `B` | `@B...A` (**target first**) |
| commit `sha`, parent `p` | `@p..sha` |

and the row disappears, because the field is now bounded. Editing the field
back to a point brings it back. There is no dialog state a `gg compare`
invocation could not express.

Keys: `tab`/`shift+tab` walk link 1 → base 1 → link 2 → base 2 (skipping
hidden rows); `↓` opens the history under a link field; `ctrl+s` swaps sides;
`enter` on link 2 (or `ctrl+enter` anywhere) submits; errors render under the
field `LinkSideError.Side` names; `esc` closes. `popupMax` embedded.

### 3.8 Saving, and listing what was saved (D7)

**Save comparison…** prompts for a label (default
`savedcompare.DefaultLabel`) and calls `SavedCompareAdd(ctx, LeftText,
RightText, label)`; `ErrSavedCompareExists` is a notice ("already saved as
…"), not an error.

The Previews panel loads `SavedCompareList`. SET rows keep today's rendering
and live summary (through the unchanged `Preview*` façade). PAIR rows render
`<label>  <left desc> ↔ <right desc>`; `enter` re-runs `CompareLinks` on the
stored texts and opens 3.6's view; rename and remove route by shape to
`SavedCompareRename/Remove`; preview-only actions decline with a status line.
A pair row's Copy link is refused (a pair is two links); its two halves are
offered as **Copy left link** / **Copy right link**.

### 3.9 bookmark↔shelf (D8)

| first pick | second pick | today | 3b |
|---|---|---|---|
| commit | commit | `startEntryCompare` | both sides → links → `CompareLinks` → 3.6 |
| commit | file | refused ("cannot compare a commit against a file") | **legal**: same door |
| file | commit | refused | **legal**: same door |
| file | file | two-ref diff | **unchanged** |

Only the **cross** flow changes (`pendingCompare` between the two
switchers). bookmark↔bookmark and shelf↔shelf keep `startEntryCompare`, per
§5.2. The two refusal strings lose their cross callers; they stay if a
same-kind flow still uses them, else they leave all four bundles.

### 3.10 CLI (D10)

`gg compare --remove <id|label>` and `gg compare --rename <id|label>
<new-label>`. Unknown spec → exit 2, the message `--saved` already prints.
Both print `# removed: …` / `# renamed: …` to **stderr**; stdout stays empty.
`s95_saved_compare.toml` grows the rows; `using-gg` bumps to v84.

---

## 4. Errors

| case | behaviour |
|---|---|
| unparseable field | inline under that field; no base row; submit disabled |
| link names another checkout | inline: cross-repository compare is not supported yet (§9) |
| stash dropped between menu-open and copy | status line; nothing copied, nothing recorded |
| no merge base for `@B...A` | inline under that side — `ErrNoMergeBase`'s text |
| history unreadable / disabled | the picker shows "no copied links yet"; the prompt still works |
| recording fails | silent (best-effort, as everywhere) |
| saved pair whose link no longer resolves | the row stays listed; `enter` reports the error; remove still works |

---

## 5. Testing — what each gate must be unable to pass

Every task carries a break table ("with the GUARD removed"). The ones that
are easy to get wrong:

| subject | the fixture that makes the test able to see it |
|---|---|
| chokepoint records | a fake linkhist store; break = delete the `isLinkText` branch. Also assert a **non-link** copy records nothing. |
| stash link never holds `stash@{N}` | two stashes; copy the *second*, push a third, resolve the copied link → still the same sha |
| §3.4 untracked members | a `-u` stash with one tracked edit and one untracked file; assert the two arms **differ** up front (`a..b` alone lacks the untracked path) |
| octopus guard | a real three-parent merge of non-root branches evaluates to **exactly** `a..b` |
| `compareTag` collision | open `gg://r@sha ↔ X`, then `gg://r/f.go@sha ↔ X`; the second must replace the first |
| target-first rewrite | assert the field **text** is `@main...feat/x`; left and right fixtures differ in kind (a ref and a commit) so a side swap cannot pass |
| nothing rewritten untouched | tab through a base row without enter → field text byte-identical |
| one door | CLI, MCP and TUI answer identically for a **local-form file link** (F4) |
| pair rows vs set rows | one panel fixture holding both; rename the pair through the panel, assert the SET's label did not move |
| i18n | the existing AST gates; every new string in ja/ko/zh/ru |

Headless confirmation before each merge: `./tui-capture.sh` runs for the
copy rows, the `#` picker, and the dialog → view → save → panel round trip,
against the **built binary**.

---

## 6. Plans

| plan | tasks | stands alone because |
|---|---|---|
| **3b-1** links in, links out | `DescribeLink` move · clipboard chokepoint · §3.4 stash algebra + `FileSet.Source` · `StashPair` + the four copy surfaces · the history picker in `#` · `--remove`/`--rename` · docs | every link the TUI shows can be copied, is recorded, and can be pasted back; no dialog needed |
| **3b-2** the dialog | `CompareLinks` door (CLI+MCP moved onto it) · `BoundKind` + `SuggestBase` · the set-shaped view **with** the dialog that opens it · save · Previews panel pair rows · bookmark↔shelf cross arms · docs | needs 3b-1's picker and `Source`; ends with the full round trip |

Plan 3b-2 is written after 3b-1 merges, against the code that then exists.

## 7. What 3c inherits

`CompareLinks`, `SuggestBase`, `Link.BoundKind` (with a Go↔JS gate like
`linkDesc`'s), `DescribeLink`, and the rewrite table of 3.7. The web dialog
is layout and transport only.

## 8. Out of scope

Cross-repository compare (§9) · a hunk-level link from the picker · the web
(3c) · the parked items in `memory/unified-links-feature.md` · renaming the
`Preview*` seams (3c, when the panel's other half lands).
