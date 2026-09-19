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
   system: N changes while the thing it names does not. **(2026-09-19: the
   TUI's stash copy row emits NO hint — landing the pair already shows the
   stash. See `2026-09-19-links-tui-design.md` D4.)**
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
| `@B...A` | `@E...D` | bnd × bnd | two branches each bounded against their own base (§5.1) — "did these two branches change the same files, the same way?" |

---

## 4. Architecture

### 4.0 Validity is a property of the type, not a checklist

**Parse, don't validate.** There are exactly two funnels in this design, and
after each one the value in hand carries its own guarantee. Nothing downstream
re-checks, because nothing downstream *can* hold an invalid value.

| funnel | produces | guarantees |
|---|---|---|
| `model.ParseLink(s)` | a `Link` | the grammar of §3.2, including `LinkRefOK`/`LinkPathOK` |
| a `model.*Endpoint(...)` constructor | an `Endpoint` | the kind's own field invariants, and its boundedness |

Three traps in the shipped type make this mandatory rather than stylistic.
All three were verified against the tree before this section was written:

1. **54 raw `model.Endpoint{...}` literals** exist outside tests. Three new
   kinds means 54 sites that might have to care.
2. **The zero value is silently valid.** `EndpointWorkTree = iota` is `0`, so
   `model.Endpoint{}` *is* a working-tree endpoint — and
   `internal/cli/compare.go` returns exactly that on its error paths
   (`return model.Endpoint{}, 2`). The exit code saves it today; the shape is
   a bug waiting for a caller that reads the value first.
3. **`default:` already means "commit".** In `Endpoint.FileRef`, `Display` and
   `CacheTag` the switch falls through to the commit case. Adding
   `EndpointRef` with no other change would silently yield
   `FileRef{Source: SourceCommit, Locator: ""}` — a wrong answer, no error, in
   three methods.

Endpoint is **never serialized** — `internal/tui/session_snapshot.go` converts
through its own `snapEndpoint` wire type via `endpointProto` — so unexported
fields cost nothing.

```go
type Endpoint struct {
    kind     EndpointKind // unexported: the only way in is a constructor
    hash     string
    ref      string
    shelfID  string
    a, b     string
    threeDot bool
    bounded  bool // computed ONCE at construction, never re-derived
}

func WorkTreeEndpoint() Endpoint
func IndexEndpoint() Endpoint
func CommitEndpoint(hash string) (Endpoint, error)          // 7..64 hex
func RefEndpoint(name string) (Endpoint, error)             // LinkRefOK
func ShelfEndpoint(id string) (Endpoint, error)
func PairEndpoint(a, b string, threeDot bool) (Endpoint, error)

func (e Endpoint) Kind() EndpointKind
func (e Endpoint) Bounded() bool // reads the field; nobody re-derives the rule
```

Four rules follow, and they are the whole of the enforcement:

- **`EndpointInvalid EndpointKind = iota` comes first**, shifting
  `EndpointWorkTree` to 1. `Endpoint{}` is then unusable, and every error
  return is obviously wrong instead of plausibly right. This is a behaviour
  change to existing code and must be done as its own step.
- **Every switch over a kind gets a panicking `default:`** naming the kind,
  replacing the three silent commit fall-throughs.
- **Boundedness is stored, not computed by callers.** §3.1's rule is applied
  exactly once, in the constructors.
- **One table test enumerates every kind**, asserting each has a constructor,
  a `Bounded()` answer, a `FileRef`, a `CacheTag` and a `Display`. A kind
  added without a row fails the build. That is the single place the rules are
  checked — not thousands of lines across dozens of files.

The rejected alternative was a closed interface with real variant types
(`CommitEnd`, `PairEnd`, …). It is a stronger guarantee, but Go does not
exhaustiveness-check type switches, so it still needs the same table test —
while reshaping all 54 sites plus `Differ`, `CompareFiles`, MCP and the TUI,
rather than respelling them.

### 4.1 No new address type

gg already has five address-ish types — `model.Link`, `FileAddress`,
`FileRef`, `Endpoint`, `Bookmark`. This design adds **none**.

`model.Endpoint` is already "one side of a whole-tree comparison" with
`FileRef(path)`, `IsLive()` and `CacheTag()`. It gains the new kinds and the
ability to state its own boundedness:

It gains two kinds — `EndpointRef` (a branch/tag tip, unbounded) and
`EndpointPair` (`A..B` or `A...B`, bounded) — behind the constructors of §4.0,
which is also where the field layout lives.

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
| `internal/savedcompare` | `{ID, Left model.Link, Right *model.Link, Label, Created}` — absorbs merge previews, `Right == nil` ⇒ a saved SET (§4.5) | `internal/preview` |

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

**Decision (amended 2026-09-19): CONVERT.** The original decision here was to
discard `previews.toml` on consent, on the reasoning that conversion cost
design effort the user base did not justify. Reading the code reversed all
three premises:

- **The conversion is the function this section already writes.** A preview
  becomes `{Left: gg://repo@target...source, Right: nil}` — that sentence is
  the whole body. Both things that could have made it lossy do not:
  `preview.ID` is `sha256(source \x00 target)` truncated, so an id is
  recomputable (and is carried over verbatim anyway), and `PreviewNotes`
  keys on the branch NAMES, never on a preview record — the notes are
  ordinary committed notes at the source tip and need no migration at all.
- **The discard is the expensive path.** It needs a file-backed action in
  `ApplyMigration`, a `storeFiles` sibling to `storeRefs`, a registered
  preflight feature, and consent prose rendered on CLI, TUI and web.
  Conversion needs none of it: consent exists to make data loss honest, and
  a lossless conversion has no loss to confess.
- **The discard is wrong on a dual-environment checkout.** `StampStoreFormat`
  writes a git ref (`refs/gg/meta/<store>/<format>`, inside `.git`);
  `previews.toml` lives under XDG state. One `.git` opened from both WSL and
  Windows is one marker and two state directories. Consenting from one side
  stamps the marker and deletes that side's file; the other side then probes
  `Format=2, HasData=true`, reads Satisfied, never prompts, and orphans a
  live `previews.toml` silently and permanently.

**There are no format numbers in this migration.** `previews.toml` and
`savedcompare.toml` are different files, so the complete probe is "does the
legacy file still exist on this machine". No marker, no ref, no scope
mismatch — each environment converts its own copy the first time it runs.
`DataFormat` and the branch-versions feature are untouched.

**A merge preview is one bounded *set*, not a comparison**, so the store holds
both shapes explicitly:

```go
type Entry struct {
    ID      string      // stable; converted previews keep their OLD id verbatim
    Left    model.Link
    Right   *model.Link // nil ⇒ a saved SET (a merge preview), not a pair
    Label   string
    Created time.Time
}
```

A merge preview migrates to `{Left: gg://repo@target...source, Right: nil}`.
Preview notes keep anchoring on the source tip, which that link still yields.
A saved comparison sets both. The Previews tab lists both — they are the same
kind (bounded), and either can be an endpoint of the next comparison.

`ID` derives from the LINK TEXTS, `sha256(left \x00 right)` truncated —
**one derivation for both shapes, never branching on shape**, because a hash
that branches is the signature two-arms defect. Converted entries carry their
old id across so existing `gg preview <id>` invocations keep working. `Add`
dedups on `(Left, Right)`, never on the id.

`model.Link` gains `MarshalText`/`UnmarshalText` over the existing
`String`/`ParseLink` pair, so the stored file holds readable link text rather
than an exploded struct — and so the store's own file is a standing
`String(Parse(s)) == s` witness.

The store reuses `stateBaseDir("previews")/<repo-key>/` — deliberately the
same kind string and the same directory. The converter then finds
`previews.toml` as a sibling with no path plumbing, and
`statebasedir_callers_test.go`'s pinned kind set stays unchanged. A kind
string is a directory on a user's disk; adding one here buys nothing.

#### 4.5.1 The migration framework, generalized

The preflight machinery is pluggable on the CHECK half and hardcoded on the
DO half. `Requirement` is an interface any feature may implement; `Migration`
is pure data (`{Store, From, To, Describe}`) with no action in it, and the
work is fixed in `engine.ApplyMigration`: delete the refs it was handed, stamp
the marker. A feature can declare what it needs and what the user would lose,
but not how to fix it — the one fix in the box is *delete these refs*.

Plan 3a closes that asymmetry, in both halves:

**The check.** `preflight.Probes` carries only git-ref marker formats and the
git version, which is why a machine-local store cannot be probed honestly.
Add a machine-local probe channel and a `Requirement` implementation over it:

```go
type LegacyStore struct{ Store string } // FitTooOld when the legacy file is present
```

**The action.** `Migration` names an action instead of implying one, and
`ApplyMigration` keeps the reservation, the events and the summary while
delegating the body:

```go
// engine
type MigrationAction interface {
    Apply(ctx context.Context, deps OpDeps) (n int, err error) // n = entries affected
}
type DiscardRefs     struct{ Refs []string } // today's hardcoded body, extracted verbatim
type ConvertPreviews struct{ Dir string }    // previews.toml → savedcompare.toml, then remove

type ApplyMigration struct {
    Feature string
    Store   string
    To      int
    Action  MigrationAction // replaces Refs
}
```

Domain still decides WHAT (it already computes `storeRefs`; it now also
constructs the action) and the op still only runs it under the lock, so
`ApplyMigration`'s "dumb executor" contract is preserved rather than weakened.

**Consent.** `Migration` gains `Lossless bool` — **not** `Consent bool`, and
the polarity is the whole point: the zero value must be the SAFE one. Spelled
as `Consent`, a migration that simply forgot to declare would destroy a user's
data unasked, and the branch-versions migration — the one that really does
destroy — declares nothing in this field at all. Forgetting has to fail
towards the consent screen, never past it.

So `Lossless: false` (the default) is today's flow exactly: `PendingMigrations`
reports it and the three consent screens are unchanged. `Lossless: true` runs
without asking and reports what it did. `Service.RunAutoMigrations(ctx)` is
called once per process from the composition root and costs one `os.Stat` when
there is nothing to do.
---

## 5. Surfaces

The single rule that makes the compare dialog useful: **every "copy gg link"
click, on any surface, appends to the history.**

### 5.1 Bounding a side in the compare dialog

Shared by the TUI and web dialogs. A branch link is unbounded (§3.1) because
no base can be guessed — but the *dialog* can ask, which puts the choice in
front of the user instead of in the resolver.

The layout, both dialogs:

```
┌─ link comparison ──────────────────────────────────────────┐
│                                                            │
│  [ link 1 — branch A / file / commit / whatever         ]  │
│      [ base — branch B ]   shown only when link 1 is a     │
│                            branch; the set becomes every   │
│                            file a merge preview of A into  │
│                            B would change                  │
│                                                            │
│  [ link 2 — branch D / file / commit / whatever         ]  │
│      [ base — branch E ]   likewise for link 2             │
│                                                            │
└────────────────────────────────────────────────────────────┘
```

Each side carries its **own, independent** base row, and that independence is
the point: bounding both sides turns branch↔branch from "compare two tips"
into "did these two branches change the same files, the same way" — a
bounded × bounded comparison. It is the case §3.1 could not express from a
bare branch link, and the dialog reaches it without any grammar change.

**Direction.** In gg's preview vocabulary `PreviewAdd(source, target)` renders
`merge-base(target, source)..source`, spelled `@target...source`. So the
selected link is the **source** and the base is the **target**: choosing base
`B` for branch `A` rewrites the field to `@B...A`.

**Bounding is a link rewrite in the field, not dialog-only state.** Choosing
base `main` for `@ref:feat/x` rewrites that field to `@main...feat/x`. The
field always holds a real link, so a saved comparison, its CLI equivalent and
the link in the history are byte-identical and reproducible. There is no
hidden dialog mode a `gg compare` invocation could not express.

**Three-dot**, matching previews exactly: `internal/cli/preview.go` documents
the semantics as `merge-base(target, source)..source`, and `PreviewSummary`
already exposes the base it used. The dialog reuses that, so a branch bounded
in the dialog and a saved preview of the same pair are the same set.

Bounding generalizes to any *point* — it is just "pick a second point to make
a pair":

| unbounded side | the picker offers | rewritten to |
|---|---|---|
| branch / tag tip | its upstream, else the trunk — **preselected, visible and changeable** | `@base...branch` (three-dot) |
| commit | its first parent | `@parent..sha` (two-dot; identical to three-dot for a single parent) |
| working tree / index | nothing — no base exists | no affordance shown |

**Offered, never required.** Leaving both sides unbounded compares the two
tips, which is the agreed meaning of branch↔branch. The affordance never
blocks the dialog and never rewrites a field the user did not touch.

**New machinery.** gg has no default-base notion today — `PreviewAdd(ctx,
source, target, label)` always takes both names explicitly, and nothing infers
one. The suggestion order (upstream, else trunk) is therefore new, and it is
a *suggestion in a picker*, never a silent default: the §3.1 "never guess"
rule holds because the user sees and confirms the base before the rewrite.

**The CLI stays explicit.** No `--base` inference; an agent spells the pair.
The dialog is the convenience layer.

### 5.2 TUI

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
  **(2026-09-19, narrowed: the commit arms move and commit↔file becomes
  legal; file↔file keeps its two-ref diff, because it compares two blobs
  whose paths may differ and the key-based algebra cannot. See
  `2026-09-19-links-tui-design.md` D8, §3.9.)**
- **(2026-09-19)** The bookmark and shelf switchers gain `L` (copy link); the
  Previews panel lists saved PAIRS as well as previews (§4.5 already said
  so; it was phased under 3c by mistake — D7).
- Every new string routed through `i18n.T` with a literal key in all four
  bundles.

### 5.3 CLI

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

### 5.4 MCP

- `gg_link_resolve` — link → structured place
- `gg_link_list` — the history
- `gg_compare_links` — two links → file list
- `gg_compare_file` — grows link arguments

Then `agentskill.Version` bump and `gg init --update`.

### 5.5 Web

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
| bounded × bounded, disjoint sets | a result of all A/D, **not** an error (disjoint does not mean empty: every member of one side is absent from the other, which §3.5 already spells as A/D. Only two EMPTY sets compare to nothing.) |
| `ref:` does not resolve | error |
| two links naming different repos | refused in phase 1 — §9 |

---

## 7. Testing

- **`model.Endpoint` exhaustiveness** — the §4.0 table: one row per kind,
  asserting a constructor, `Bounded()`, `FileRef`, `CacheTag` and `Display`.
  A kind added without a row fails the build. Plus: every constructor rejects
  its bad inputs, and `Endpoint{}` is invalid.
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

Seven plans, each a sound stopping point. (Plan 1a grew a Task 3b during execution — see the plan. Plan 3 was split into 3a/3b/3c on 2026-09-19: as one plan it was larger than 1a+1b+2 combined, and the three pieces have a clean dependency order — only the dialogs need the store, and only the store needs the migration.)

| plan | contents | why it stands alone |
|---|---|---|
| **1a** | `Endpoint` hardening (§4.0) alone: unexported fields + constructors, `EndpointInvalid` first, panicking defaults, the exhaustiveness table, 54 literals respelled | a pure refactor with no new kinds — it must land and go green *before* any new kind exists, or it is fixing three traps and adding to them in one diff |
| **1b** | grammar (`ref:`, `..`, `?hint`) · the new `Endpoint` kinds · `EvalEndpoint`/`CompareSets` · CLI `compare`/`link` | the whole algebra, fully tested; agents can use it the day it lands, no UI needed |
| **2** | `linkhist` · MCP tools · navigation hints (`linknav`, `steer`, both consumers) · agentskill bump | links become referenceable and navigable everywhere |
| **3a** | `savedcompare` store + the previews CONVERSION (§4.5) · the generalized `Migration` action and machine-local probe (§4.5.1) · a domain façade over `PreviewAdd/List/Get/Rename/Remove` · `gg compare --save/--saved/--list` | `model.MergePreview` reaches only four consumer files, so the façade keeps every frontend compiling untouched: the store lands, converts and is usable from the CLI with no UI work at all |
| **3b-1** | the TUI RECORDS copied links (it never did) · `domain.DescribeLink` · §3.4's untracked-files rule, deferred since 1b · copy rows (branch/remote/tag, **stash**, bookmark, shelf) · the `#` prompt history picker · `gg compare --remove/--rename` | every link the TUI shows can be copied, is recorded, and can be pasted back; needs neither the store nor the dialog |
| **3b-2** | `domain.CompareLinks` (one door for CLI, MCP, TUI) · a SET-shaped TUI compare view + pair-link landing through it · the "Compare with link…" palette command + the shared base picker (§5.1) · *Save comparison* · the Previews panel lists saved pairs · bookmark↔shelf cross arms | the dialog and the only view that can show its answer land together |
| **3c** | web two-field dialog + the same base picker · the web Previews tab listing saved comparisons | the web half of the same two surfaces, on a base picker already settled in 3b-2 |

(3b was split into 3b-1/3b-2 on 2026-09-19: reading the TUI found that it
never recorded a copied link, that its compare view cannot show a link
comparison at all, and that §3.4 was still unimplemented. Design:
`docs/superpowers/specs/2026-09-19-links-tui-design.md`.)

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
