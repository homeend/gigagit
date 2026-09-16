# Unified `gg://` links, link-set comparison, and the link history

Status: **agreed** (brainstorm 2026-09-16) · Branch: `feat/unified-links`

Supersedes nothing; extends `docs/superpowers/specs/2026-09-10-gg-links-design.md`
(the shipped link grammar) and `2026-09-10-preview-notes-design.md` (merge
previews).

---

## 1. Problem

`gg://` links today address a file, a line, a hunk, a commit or a merge
preview. Three things a user looks at cannot be linked at all — **bookmarks**,
**shelf entries** and **branches** — and a link cannot be compared against
another link. Comparison instead lives in per-surface menus: `gg compare`
accepts a second, ad-hoc spelling (`bookmark:<id>`, `shelf:<id>`, `@staged`,
`@worktree`), and the TUI/web pair-compare flows are menu-driven selections
that only work within one entry kind.

The consequences:

- An AI agent cannot be handed "this shelf entry" or "this branch" by
  reference. The CLI/MCP surface has no spelling for them.
- Comparing across kinds (a bookmark against a shelf entry, a commit against
  a preview) needs a new menu per pair, so most pairs simply do not exist.
- The pairing rules live in `internal/cli/compare.go:93-95`, so the TUI, web
  and MCP each answer the same question differently or not at all.

## 2. Goals

1. Every element a user can point at has **one** link spelling.
2. A link can be **navigated** to — it reveals the element on the surface it
   was copied from.
3. A link can be **resolved and compared from the CLI and MCP**, so an agent
   can reference and diff anything gg can show.
4. Any two links can be compared, with a **single** rule that terminates and
   never produces a nonsense result.
5. A comparison can be **saved** and later reopened.
6. Every link a user copies is remembered in a short, described **history**,
   so a compare dialog can be filled without re-navigating.

### Non-goals

- GitHub PR read/write via `gh`. Agreed as a **separate project**: it shares
  no machinery with this spine (new external binary, remote read/write lane,
  review-state model, a capability detector closer to `exttool`/`agentinit`
  than to `preflight`). It is brainstormed on its own after this ships.
- Cross-repository comparison. See §9.
- Free-text search over links.

---

## 3. The model

### 3.1 The one rule

> **A pair is bounded. A point is unbounded.**

Every link, once resolved, evaluates to a **file set** of one of two kinds:

| kind | meaning | examples |
|---|---|---|
| **unbounded** | every file in the repository at some state | a commit tree, a branch tip, the working tree, the index |
| **bounded** | a finite, enumerated set of paths | a merge preview's changed files, a shelf entry's members, a single file, a bookmark |

A commit is a *point*, so `gg://repo@abc123` is the **tree at that commit** —
unbounded. To address what a commit *changed*, spell the pair.

### 3.2 Grammar

```
gg://<repo>[/<path>][@<target>][:<line>|#<hunk>][?<hint>]
```

| `<target>` | kind | status |
|---|---|---|
| *(absent)* — working tree | unbounded | shipped |
| `staged` | unbounded | shipped |
| `<sha>` (7..64 hex) | unbounded — tree at that commit | shipped |
| `ref:<name>` — a branch or tag tip | unbounded | **new** |
| `<a>...<b>` — merge preview (three-dot) | bounded | shipped |
| `<a>..<b>` — two-dot change-set | bounded | **new** |
| *(any of the above with a `/<path>`)* | bounded, one element | shipped |

**Separator safety** — verified with `git check-ref-format`:

| char | legal in a refname? | consequence |
|---|---|---|
| `:` | no | `ref:<name>` and the `:<line>` suffix are both unambiguous |
| `^` `~` | no | free for future revision operators |
| `?` | **no** | safe as the hint separator; a refname can never reach it |
| `@` `#` `=` | **yes** | already rejected by the shipped `model.LinkRefOK` |

`LinkRefOK` is reused as-is for the `ref:` form. `LinkPathOK` gains `?` to its
reject set.

**No shorthand for "this commit's change."** A producer copying from a commit
diff view resolves the first parent at copy time and emits the explicit pair
`@abc122..abc123`. This keeps the link portable and self-describing, and adds
exactly one grammar form rather than a `^!`-style operator that would fight
zsh history expansion on the CLI.

For a merge commit the producer uses the **first** parent. The resulting link
records the chosen parent explicitly, so the choice is visible, not implicit.

### 3.3 The hint — `?<hint>`

`?bookmark=<id>` and `?shelf=<id>` record **which UI surface the link was
copied from**. It is the HTML-anchor idea: the address is the identity, the
hint is where to open it.

Three rules, in priority order:

1. **Compare ignores the hint.** A bookmarked commit and the same commit
   picked off the log are one endpoint. No bookmark×shelf×commit matrix — the
   matrix stays the 2×2 of §3.5.
2. **Navigate honours the hint.** Same address, different landing: reveal the
   bookmark row, the shelf row, or the commit in the log.
3. **The hint degrades; it never fails.** On another machine the address still
   resolves and the hint is dropped with a notice.

**Exception — the hint carries content when there is no address.** A shelf
entry's blob may correspond to nothing in git:

| shelf entry | link | content comes from |
|---|---|---|
| shelved **commit** | `gg://repo@abc123?shelf=<id>` | `abc123` while it resolves; the frozen tar once it is gc'd |
| shelved **file** (from the working tree) | `gg://repo?shelf=<id>` | the hint — those bytes were never in git |

The first row is exactly the hybrid semantics `domain.ResolveCommitEntryEndpoint`
already implements. The second is the only link in the system with no address
at all, and therefore the only one that cannot travel between machines.

**Bookmarks need no hint-only form.** A `model.Bookmark` *is* a stored
`FileAddress`, so its link is the address it points at, plus `?bookmark=<id>`
for the landing. Losing the hint loses nothing but the sidebar landing.

**Merge previews keep their shipped spelling** — `LinkPreview{Source,Target}`
carries the *branch-name pair*, deliberately, so it travels. No hint.

### 3.4 Stashes

A stash **is a commit**, so it needs no new grammar. `internal/git/stash.go`
already says so: `StashCommit` "resolves a stash ref (e.g. stash@{0}) to its
commit SHA so the file tree / diff can read it as an ordinary commit."

| link | kind | meaning |
|---|---|---|
| `gg://repo@<stash-sha>` | unbounded | the tree the stash captured |
| `gg://repo@<stash-sha>^..<stash-sha>` | bounded | what the stash changes |

The first-parent rule of §3.2 is already correct here by construction: a
stash's first parent is the HEAD it was made on, which is exactly what
`git stash show` diffs against.

Two rules specific to stashes:

1. **`stash@{N}` must never appear in a link.** It is positional — pushing,
   popping or dropping any stash shifts every N, so the same link text would
   silently name a different stash. The producer resolves to the sha at copy
   time, exactly as it already does for a commit's parent. `?stash=<n>`
   survives only as a landing hint, and it is the most fragile hint in the
   system: N changes while the thing it names does not.
2. **A stash link can dangle, with no fallback.** A stash commit is reachable
   only through `refs/stash` and its reflog; pop or drop it and the sha
   becomes gc-able. Unlike a shelved commit there is no frozen tar, so this is
   a plain `CommitGoneError`. The message should point at shelving as the way
   to keep it.

**Untracked files are included.** `StashPush` takes `includeUntracked`, so gg
can create three-parent stashes — `[HEAD, index, untracked-tree]` — whose
untracked files live in the *third parent*, not in the stash's own tree. A
plain `<sha>^..<sha>` would silently omit them. The bounded set is therefore
tracked changes **plus** the third parent's files, entering as additions:

```
M  internal/tui/diff.go       (tracked)
M  internal/domain/query.go   (tracked)
A  scratch/notes.txt          (untracked, from parent 3)
```

This is deliberately *not* `git stash show` — it is "what I stashed", which is
the user's mental model, and gg already knows which it is because
`StashPush` recorded the flag. Comparing such a stash against a commit then
reports the untracked file as A/D, which is what §3.5 already does for any key
absent from the other side. Cost: one extra tree read.

### 3.5 The compare algebra

```
unbounded × unbounded  →  BOUNDED   git diff A B: the differing paths
bounded   × bounded    →  BOUNDED   symmetric over the union of member paths;
                                    present in one only ⇒ A / D
unbounded × bounded    →  BOUNDED   project the tree onto the bounded side's
                                    paths; a key absent from the tree ⇒ A / D
```

**The asymmetry is load-bearing: you can only ever scale down.** Growing the
bounded side to the tree's size would mean comparing one file against every
file in the repository. So the bounded side always supplies the key set.

**Compare is closed** — every comparison yields a bounded set. That makes a
compare result the same *kind* as a merge preview, so it is saveable,
linkable, and usable as an endpoint of the next comparison. This is what makes
"compare everything with everything" terminate.

`domain.shelfCommitCompare` is already the third row with a `shelfIsRight`
direction flag. The work is generalizing that one function.

### 3.6 Worked examples

| left | right | lane | result |
|---|---|---|---|
| `@abc123` | `@def456` | unb × unb | `git diff abc123 def456` |
| `@main...feat/x` | `@main...fix/y` | bnd × bnd | which paths each branch touched, and how they differ |
| `/parser.go@abc123` | `@def456` | bnd × unb | `parser.go` at `abc123` vs at `def456`; absent in `def456` ⇒ `D` |
| `?shelf=<id>` | `@abc123` | bnd × unb | the shelf's members, projected onto `abc123`'s tree |
| `@ref:mine` | `@ref:theirs` | unb × unb | the two tips |
| `@abc122..abc123` | `?shelf=<id>` | bnd × bnd | did the shelf hold the same change as the commit? |
| `@<stash>^..<stash>` | `@ref:feat/x` | bnd × unb | the stash's files, projected onto the branch tip — "is my stash already on the branch?" |

---

## 4. Architecture

### 4.1 No new address type

gg already has five address-ish types — `model.Link`, `FileAddress`,
`FileRef`, `Endpoint`, `Bookmark`. This design adds **none**.

`model.Endpoint` is already "one side of a whole-tree comparison" with
`FileRef(path)`, `IsLive()` and `CacheTag()`. It gains the new kinds and the
ability to state its own boundedness:

```go
type Endpoint struct {
    Kind     EndpointKind
    Hash     string // EndpointCommit
    ShelfID  string // EndpointShelf
    Ref      string // NEW: EndpointRef   — a branch/tag tip (unbounded)
    A, B     string // NEW: EndpointPair  — A..B or A...B   (bounded)
    ThreeDot bool   // NEW: which of the two
}

func (e Endpoint) Bounded() bool
```

The one pipeline becomes:

```
Link  →  Resolved  →  Endpoint  →  FileSet  →  CompareSets
```

### 4.2 Two domain functions

```go
// EvalEndpoint turns an endpoint into its file set: the key paths when
// bounded, and the per-path byte reader either way.
func (s *Service) EvalEndpoint(ctx context.Context, e model.Endpoint) (FileSet, error)

// CompareSets is the 2x2 of section 3.4. The result is always bounded.
func (s *Service) CompareSets(ctx context.Context, left, right FileSet) ([]model.CommitFile, error)
```

`domain/compare_entries.go` shrinks to thin adapters. `shelfCommitCompare`
becomes the general projection. The ad-hoc pairing rules in
`internal/cli/compare.go:93-95` **move into domain**, so all four frontends
share one answer.

### 4.3 New packages

Both are DAG leaves owned by `domain`; frontends never import them, per the
`notes`/`shelf`/`preview`/`bookmark` convention.

| package | contents | precedent to copy |
|---|---|---|
| `internal/linkhist` | the 20-entry MRU of created links: `{Link, Desc, Created}`, TOML + `O_EXCL` lock under XDG state | `internal/notes`, `domain/searchstore.go` |
| `internal/savedcompare` | `{Left, Right model.Link, Label string}` — absorbs merge previews (§4.5) | `internal/preview` |

Not named `clipboard` — that package already exists (the OS clipboard writer).

**`linkhist` semantics.** Every "copy gg link" action on any surface appends.
Re-copying an identical link moves it to the top rather than adding a row.
A link the user *pastes* into a compare field and successfully compares is
also appended, so it can be reused. Cap 20, oldest evicted.

`Desc` is stored at creation, not derived later, because the describing
context is gone by read time:

```
branch:   feat/x                gg://gigagit@ref:feat/x
bookmark: auth fix              gg://gigagit@abc123?bookmark=auth-fix
shelf:    WIP parser            gg://gigagit@abc123?shelf=commit-x-9f3a1
preview:  main...feat/x         gg://gigagit@main...feat/x
commit:   abc123 fix auth       gg://gigagit@abc123
stash:    WIP on main           gg://gigagit@9c1f2a3^..9c1f2a3?stash=0
```

The stash row shows why `Desc` must be captured at creation: `stash@{0}` is
gone from the link by design, and the subject ("WIP on main") is the only
thing that makes the row recognizable in the list.

### 4.4 Changed packages

| package | change |
|---|---|
| `model/link.go` | `ref:`, `..`, `?hint`; reuse `LinkRefOK`; `LinkPathOK` gains `?`; extend the `String(Parse(s))==s` round-trip tests |
| `domain/linkresolve.go` | `Resolved` grows `Ref`, `Pair`, `Hint` |
| `linknav` | `Command()` grows the ref and hint cases |
| `steer` | `Target` grows a `ref` state and a hint field; **both** the TUI and web navigate consumers must reveal the hinted surface |
| `cli`, `mcp`, `tui`, `web` | §5 |
| `agentskill` | `using-gg.md` + `Version` bump, then `gg init --update` |
| `web/static/links.js` | the JS grammar mirror moves in lockstep |

### 4.5 Merge previews fold into saved comparisons

A saved comparison and a merge preview are the same *kind* (both bounded) but
not the same *shape*: `MergePreview{Source,Target}` is one side, and preview
notes anchor on its source tip.

**Decision: migrate.** One `savedcompare` store at format 2; `previews.toml`
data is **discarded** on consent rather than converted. gg's user base is
small enough that conversion is not worth the design cost, and a single store
keeps the Previews surface listing one concept.

**A merge preview is one bounded *set*, not a comparison**, so the store holds
both shapes explicitly:

```go
type Entry struct {
    Left  model.Link
    Right *model.Link // nil ⇒ a saved SET (a merge preview), not a pair
    Label string
}
```

A merge preview migrates to `{Left: gg://repo@target...source, Right: nil}`.
Preview notes keep anchoring on the source tip, which that link still yields.
A saved comparison sets both. The Previews tab lists both — they are the same
kind (bounded), and either can be an endpoint of the next comparison.

**Framework gap to build.** `preflight.StoreProbe{Format, HasData}` →
verdict → consent prose → `engine.ApplyMigration` is exactly the right
machinery ("discards a store's unreadable data and stamps the new format
marker… only after explicit consent"), and `StoreVersions` 1→2 is the
precedent. But `ApplyMigration` today only deletes **git refs** (`op.Refs` →
`DeleteRef`) and stamps via `Repo.StampStoreFormat` — the only registered
store is ref-backed. `previews.toml` is a machine-local XDG **file** store, so
the op needs a file-backed discard action alongside `Refs`. Small and clean,
but it is new work, not free reuse.

---

## 5. Surfaces

The single rule that makes the compare dialog useful: **every "copy gg link"
click, on any surface, appends to the history.**

### 5.1 TUI

- Copy-link rows via the existing `.` type-to-filter action menu, on branch /
  commit / bookmark / shelf / preview / **stash** rows. A commit **row** copies
  the tree; a commit **diff view** copies the pair `@parent..sha`. A stash row
  copies the **pair** (what the stash changes), which is what a stash is for —
  and always the resolved sha, never `stash@{N}`.
- The `#` paste prompt gains a history picker alongside free paste.
- New palette command **"Compare with link…"**: two fields, each filled by
  paste *or* from the 20-row history → the compare view → *Save comparison*.
- Re-plumb: **bookmark↔shelf** compare moves from menu-selection to links.
  bookmark↔bookmark and shelf↔shelf keep their existing menus.
- Every new string routed through `i18n.T` with a literal key in all four
  bundles.

### 5.2 CLI

This is the LLM surface — goal 3.

```
gg link <what>               emit a link for a branch/commit/bookmark/shelf/preview
gg links                     the 20-row history, terse
gg compare <left> [<right>]  links are the primary spelling; today's
                             bookmark:<id> / shelf:<id> / @staged / @worktree
                             keep working, mapped onto links
gg compare --save <label>    store the bounded result
gg open <link>               navigate (grows ref: and ?hint)
```

### 5.3 MCP

- `gg_link_resolve` — link → structured place
- `gg_link_list` — the history
- `gg_compare_links` — two links → file list
- `gg_compare_file` — grows link arguments

Then `agentskill.Version` bump and `gg init --update`.

### 5.4 Web

Same copy rows; `/api/linkhist`; the same two-field dialog; the Previews tab
listing saved comparisons.

**The history must be served over `/api/…`, never browser storage** — `gg web`
binds a random port each run, which empties `localStorage`.

---

## 6. Error handling

| case | behaviour |
|---|---|
| unparseable link | `ErrLink`, CLI exit 2 (shipped convention) |
| unknown / ambiguous repo | `ErrLinkUnknownRepo` / `ErrLinkAmbiguous` — never guess (shipped) |
| hint names a bookmark/shelf absent here, **address resolves** | notice, drop the hint, navigate to the address |
| hint absent **and it was the content source** | hard error — nothing can supply the bytes |
| shelved commit gc'd, frozen tar present | use the tar (shipped `ResolveCommitEntryEndpoint` semantics) |
| commit gone, no fallback | `CommitGoneError` (shipped) |
| stash popped or dropped, sha gc'd | `CommitGoneError` — there is no frozen fallback; the message points at shelving |
| bounded × bounded, disjoint sets | empty result, **not** an error |
| `ref:` does not resolve | error |
| two links naming different repos | refused in phase 1 — §9 |

---

## 7. Testing

- **`model`** — table-driven `String(ParseLink(s)) == s` over every new form;
  reject cases for refnames containing `@`/`#` (`LinkRefOK`) and paths
  containing `?`.
- **`domain`** — the algebra as an explicit **kind×kind matrix** against a
  real git repo in `t.TempDir()`. This is the test that earns the design:
  every cell asserted, including projection direction (the generalization of
  `shelfIsRight` — whether a missing key reads `A` or `D` depends on which
  side is bounded).
- **stashes** — a three-parent stash (`gg stash -u`) whose bounded set must
  include the untracked file from parent 3; and a popped stash whose link
  yields `CommitGoneError` rather than a wrong answer. Both need a real repo.
- **`e2e`** — `gg link` → `gg compare` → `gg compare --save` round-trips as
  TOML scenarios under `e2e/scenarios/`.
- **`web`** — `links.js` pinned against the Go grammar. Precedent: the JS date
  port guarded by `model.CommitDateLayout`.
- **`i18n`** — the four AST gates (`i18n_scan_test.go`, `options_vocab_test.go`,
  `menu_labels_test.go`, `engine_prose_test.go`).
- **migration** — format 1→2 discards `previews.toml` and stamps, only after
  consent.

---

## 8. Phasing

Three plans, each a sound stopping point.

| plan | contents | why it stands alone |
|---|---|---|
| **1** | grammar (`ref:`, `..`, `?hint`) · `Endpoint` kinds · `EvalEndpoint`/`CompareSets` · CLI `compare`/`link` | the whole algebra, fully tested; agents can use it the day it lands, no UI needed |
| **2** | `linkhist` · MCP tools · navigation hints (`linknav`, `steer`, both consumers) · agentskill bump | links become referenceable and navigable everywhere |
| **3** | TUI copy rows + compare palette · web surfaces · `savedcompare` store + the file-backed migration | the UI layer, on a settled core |

---

## 9. Deferred: cross-repository comparison

Nothing in the algebra stops `gg compare gg://repoA@abc gg://repoB@def` — both
sides evaluate to file sets, and the sets do not care which repository
produced them.

What stops it is reservation: every read goes through one `domain.Service`
under one `repogate` reservation, so a cross-repo compare needs two services
and two reservations acquired in a fixed order to avoid deadlock.

**Refused in phase 1** with an explicit message. Revisit once workspace groups
land — the roadmap already needs multi-repo reservation ordering for parallel
background-pull, and it is the same problem solved once.

---

## 10. Open questions

None blocking. Recorded for the plans:

1. The exact `Desc` wording per kind is in §4.3; the commit form includes the
   subject line, which may need truncation at a width the plan chooses.
2. Whether `gg links` should support `--json` for agents, or rely on the
   existing terse format. Decide when writing plan 2.
