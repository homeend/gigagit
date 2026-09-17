# Unified links — plan 1b: the grammar and the compare algebra

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan
> task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** make every `gg://` link resolvable to a *file set* and make any two
file sets comparable, from the CLI, with no UI.

**Architecture:** three new grammar forms (`@ref:<name>`, `@<a>..<b>`,
`?<hint>`) feed two new `model.Endpoint` kinds (`EndpointRef` unbounded,
`EndpointPair` bounded) behind the existing smart constructors. `domain` gains
one evaluation function (`EvalEndpoint` → `FileSet`) and one comparison
function (`CompareSets`, the 2×2 of the spec's §3.5). The ad-hoc pairing rules
that live in `internal/cli/compare.go` move into `domain` so all four frontends
get one answer. `gg compare` and `gg link` then speak links.

**Tech Stack:** Go 1.26, stdlib only. Tests use a real `git` binary in a
`t.TempDir()` via `internal/gittest`.

**Spec:** `docs/superpowers/specs/2026-09-16-unified-links-design.md` — read
§3 (the model), §4.0–4.2 (architecture) and §6 (error handling) before
starting. This plan implements the row labelled **1b** in the spec's §8.

**Predecessor:** plan 1a
(`docs/superpowers/plans/2026-09-16-endpoint-hardening.md`), merged to main as
`6f2ee807`. It made `model.Endpoint` impossible to build in an invalid state.
This plan is the first one that adds kinds on top of that foundation.

---

## Global Constraints

These apply to **every** task. They are copied from the spec and from rulings
made while writing this plan; do not re-litigate them.

- **A pair is bounded. A point is unbounded.** (spec §3.1) A link evaluates
  either to a finite enumerated path set (bounded) or to every file in the
  repo (unbounded).
- **Compare is closed:** every comparison yields a BOUNDED result. (spec §3.5)
- **`unbounded × bounded` projects DOWN onto the bounded side's paths.** You
  may never grow the bounded side. (spec §3.5)
- **`model.Endpoint` has unexported fields.** The only way to build one is a
  constructor. Never write a composite literal outside `internal/model`.
- **Adding an `EndpointKind` without a row in
  `internal/model/endpoint_exhaustive_test.go` fails the build**, by design
  (the `endpointKindCount` sentinel). That is the guard, not a nuisance.
- **Every switch over `EndpointKind` gets an explicit arm per kind plus
  `panic(endpointKindBug(<method>, e.kind))`.** No `default:` that means
  "commit".
- **A moving name must never reach a cache key.** `Endpoint.CacheTag()` is the
  session diff-cache key (`internal/tui/diff_view.go`'s `compareDiffKey`, and
  `domain.CompareFiles`'s singleflight key). Plan 1a's headline bug was a
  rev-spec landing there. See **Ruling R3** below for how `EndpointRef`
  answers this.
- **No `^` or `~` in a link.** The spec reserves them (§3.2) but 1b does not
  implement them: a producer resolves the parent itself and emits an explicit
  `@<parent-sha>..<sha>` pair. The spec's §3.4 stash example spells
  `@<sha>^..<sha>` illustratively; the real link a producer emits is
  `@<parent-sha>..<sha>`.
- **Every user-visible TUI string goes through `i18n.T` with a literal key
  present in all four bundles** (`internal/i18n/lang/{ja,ko,ru,zh}.toml`).
  This plan touches no TUI strings, but the CLI prose it adds stays **English**
  — CLI/engine prose is the agent-facing protocol and is never translated.
- **`internal/cli` and `internal/tui` never import `internal/git`.** They go
  through `internal/domain`. `internal/archtest` fails the build otherwise.
- **Implementers run tests in the FOREGROUND**, and only on the packages they
  touched (`go test ./internal/model/ ./internal/domain/ -count=1`). The
  controller runs the full `./test.sh race` gate. Never pipe `./test.sh`
  through `tail` — you would read tail's exit code, not the gate's.
- **Work happens in the worktree**
  `/mnt/t/others/gigagit/.claude/worktrees/unified-links-1b` on branch
  `feat/unified-links-1b`. Prefix every shell command with
  `cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b`, and use
  absolute paths for Write/Edit. Never commit in the main checkout.
- **Commit after every task.** Never `git add -A`; stage the exact paths.

### Rulings made while writing this plan (deviations from the spec, recorded)

**R1 — `PairEndpoint` takes two SHAS and has no `threeDot` flag.** The spec's
§4.0 sketch is `PairEndpoint(a, b string, threeDot bool)`. This plan uses
`PairEndpoint(a, b string) (Endpoint, error)` with both halves 7..64 hex.
Reason: three-dot means `merge-base(target, source)..source`, and the merge
base is a *resolution*, not a spelling — `domain.PreviewEndpoints` already
resolves it. Keeping `threeDot` on the Endpoint would mean the Endpoint holds
two possibly-unresolved branch names, which is exactly the moving-name-in-a-
cache-key trap of plan 1a. The **link** still carries the three-dot pair of
branch names (that is what travels between machines); `domain` resolves it to a
sha pair when it builds the Endpoint.

**R2 — `EndpointRef` holds only the name and is never a compare endpoint.**
`domain.EvalEndpoint` resolves a Ref to a `EndpointCommit` as its first act, so
no Ref ever reaches `DiffTreeFiles`, `Differ`, or a cache key.

**R3 — `EndpointRef.CacheTag()` PANICS.** It is the one method that cannot have
a safe answer: returning the ref name puts a moving value in the diff-cache key
(the 1a bug), and returning `""` collides two different refs inside
`CompareFiles`'s singleflight key. Holding an unresolved Ref where a cache key
is required is a programming error, which is precisely what `endpointKindBug`
is for. `IsLive()` is `true` (a tip moves) and `FileRef()` is well-defined
(`git show <ref>:<path>` is correct), so only `CacheTag` refuses.
Task 3 extends the exhaustiveness table with a `cacheTagPanics` column for this.

**R6 — a bounded set records PRESENCE, not just paths.** A `FileSet` carries,
for every enumerated member, whether that member has BYTES at the set's own
endpoint. A change-set's deleted members do not: `@a..b` enumerates a path that
`git show b:<path>` cannot read. Without this the comparison reads a file that
is not there and hard-errors instead of reporting `A`/`D`. The shipped
`shelfCommitCompare` never met the case — a tar member always has bytes — so
this is new with `EndpointPair`, and Task 5's fixture deletes a file to pin it.

**R7 — `PairEndpoint(x, x)` is LEGAL and evaluates to the empty bounded set.**
A fully-merged branch has `merge-base(target, source) == source`, so a
three-dot preview link of one legitimately produces a pair of identical shas.
Spec §6 says an empty bounded set is a result, not an error.

**R4 — `gg compare --save` is NOT in 1b.** It needs `internal/savedcompare`,
which the spec puts in plan 3 (§8). 1b prints the comparison; it does not store
it.

**R5 — an all-digit branch name cannot ride a `ref:` link.** `@ref:123` parses
as target `ref` + line `123` (the `:<line>` suffix wins, by the shipped
`splitLinkLine` rule), and target `ref` then fails validation with a clear
error. That is a refusal, not a wrong answer, and it is the correct trade: the
line suffix is far more common than a numeric branch name. Task 2 pins it with
a test so nobody "fixes" it into ambiguity later.

---

## File Structure

| file | status | responsibility |
|---|---|---|
| `internal/model/link.go` | modify | the `?<hint>` and `@ref:`/`@a..b` grammar; `LinkHint`; `LinkPathOK`/`LinkAbsOK`/`LinkRefOK` gain `?` |
| `internal/model/link_test.go` | modify | round-trip + reject tables for the new forms |
| `internal/model/model.go` | modify | `EndpointRef`, `EndpointPair`, their constructors and method arms |
| `internal/model/endpoint_exhaustive_test.go` | modify | two new rows + the `cacheTagPanics` column |
| `internal/domain/fileset.go` | **create** | `FileSet` and `EvalEndpoint` — a link's evaluation |
| `internal/domain/fileset_test.go` | **create** | evaluation against a real repo |
| `internal/domain/comparesets.go` | **create** | `CompareSets` — the 2×2 algebra |
| `internal/domain/comparesets_test.go` | **create** | the kind×kind matrix |
| `internal/domain/compare_entries.go` | modify | `shelfCompareFiles` becomes a thin adapter over `CompareSets` |
| `internal/domain/evallink.go` | **create** | `EvalLink`: `model.Link` → `FileSet`; the one pairing answer |
| `internal/domain/evallink_test.go` | **create** | link → file set, including the hint rules |
| `internal/git/lsfiles.go` | **create** | `ListFiles(ctx, cached bool)` — the tracked-path probe for the index and the working tree |
| `internal/cli/compare.go` | modify | accept a `gg://` link as either token; delegate pairing to domain |
| `internal/cli/link.go` | modify | `--ref`, `--pair`, `--bookmark`, `--shelf` |
| `e2e/scenarios/links-compare.toml` | **create** | `gg link` → `gg compare` round trip |
| `CHANGELOG.md` | modify | the feature entry |
| `internal/agentskill/using-gg.md` | modify | the new CLI surface + `Version` bump |

---

## Task 1: The `?<hint>` grammar

**Why first:** it is the only change that touches the *reject sets*
(`LinkPathOK`, `LinkAbsOK`, `LinkRefOK`), and every later form has to be
parsed in a body that has already had its hint stripped. Getting it in first
means Task 2 never has to think about `?`.

**Files:**
- Modify: `internal/model/link.go`
- Test: `internal/model/link_test.go`

**Interfaces:**
- Produces:
  ```go
  type LinkHint struct {
      Kind string // "bookmark", "shelf" or "stash"; "" = no hint
      ID   string
  }
  func (h LinkHint) String() string // "bookmark=auth-fix"; "" when Kind == ""
  // Link gains:  Hint LinkHint
  ```
- Consumes: nothing.

- [ ] **Step 1: Write the failing tests**

Append to `internal/model/link_test.go`:

```go
// The hint names WHICH UI surface a link was copied from (spec §3.3). It is
// the last thing in the grammar, it never changes what the link ADDRESSES,
// and it round-trips.
func TestLinkHintRoundTrip(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"gg://gigagit@abc1234def?bookmark=auth-fix",
		"gg://gigagit@abc1234def?shelf=commit-x-9f3a1",
		"gg://gigagit?shelf=wt-parser-9f3a1",
		"gg://gigagit/internal/a.go@abc1234def:42?bookmark=b1",
		"gg://gigagit/internal/a.go@abc1234def#3?shelf=s1",
		"gg://gigagit@9c1f2a3456?stash=0",
	} {
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			l, err := ParseLink(s)
			if err != nil {
				t.Fatalf("ParseLink(%q): %v", s, err)
			}
			if got := l.String(); got != s {
				t.Errorf("String(ParseLink(%q)) = %q", s, got)
			}
		})
	}
}

// The hint parses into its two halves, and the rest of the link is untouched
// by its presence: same address with and without.
func TestLinkHintSplitsAndDoesNotChangeTheAddress(t *testing.T) {
	t.Parallel()
	withHint, err := ParseLink("gg://gigagit/a.go@abc1234def:7?bookmark=auth-fix")
	if err != nil {
		t.Fatal(err)
	}
	if withHint.Hint.Kind != "bookmark" || withHint.Hint.ID != "auth-fix" {
		t.Fatalf("hint = %+v, want {bookmark auth-fix}", withHint.Hint)
	}
	plain, err := ParseLink("gg://gigagit/a.go@abc1234def:7")
	if err != nil {
		t.Fatal(err)
	}
	withHint.Hint = LinkHint{}
	if withHint != plain {
		t.Errorf("the hint changed the address:\n with = %+v\n without = %+v", withHint, plain)
	}
}

func TestLinkHintRejects(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"gg://gigagit@abc1234def?",              // empty hint
		"gg://gigagit@abc1234def?bookmark",      // no "="
		"gg://gigagit@abc1234def?bookmark=",     // no id
		"gg://gigagit@abc1234def?=auth-fix",     // no kind
		"gg://gigagit@abc1234def?branch=main",   // unknown kind
		"gg://gigagit@abc1234def?shelf=a?b",     // a second "?"
		"gg://gigagit/a?.go@abc1234def",         // "?" inside a path
	} {
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseLink(s); err == nil {
				t.Fatalf("ParseLink(%q) should have failed", s)
			} else if !errors.Is(err, ErrLink) {
				t.Fatalf("error should wrap ErrLink, got %v", err)
			}
		})
	}
}

// The reject sets gain "?" so no producer can emit a link that reparses as a
// different place.
func TestQuestionMarkIsNotExpressible(t *testing.T) {
	t.Parallel()
	if LinkPathOK("a?.go") {
		t.Error("LinkPathOK must reject a path containing ?")
	}
	if LinkAbsOK("/home/u/re?po") {
		t.Error("LinkAbsOK must reject a checkout path containing ?")
	}
	if LinkRefOK("feat/a?b") {
		t.Error("LinkRefOK must reject a refname containing ?")
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
go test ./internal/model/ -run 'TestLinkHint|TestQuestionMark' -count=1
```

Expected: compile failure — `l.Hint` undefined, `LinkHint` undefined.

- [ ] **Step 3: Implement**

In `internal/model/link.go`:

1. Add the type, above `Link`:

```go
// LinkHint is the HTML-anchor half of a link: WHICH UI surface it was copied
// from (spec §3.3). Three rules, in priority order:
//
//  1. Compare IGNORES the hint. A bookmarked commit and the same commit
//     picked off the log are one endpoint — there is no
//     bookmark × shelf × commit matrix, only the 2×2 of §3.5.
//  2. Navigate HONOURS it: same address, different landing.
//  3. It DEGRADES, never fails: on another machine the address still
//     resolves and the hint is dropped with a notice.
//
// The one exception is a shelved WORKING-TREE file, whose bytes were never in
// git: there the hint is the only content source, and the link has no address
// at all. That link cannot travel between machines, by construction.
type LinkHint struct {
	Kind string // "bookmark", "shelf" or "stash"; "" = no hint
	ID   string // the machine-local id; never empty when Kind is set
}

// String renders "kind=id", or "" for the zero value.
func (h LinkHint) String() string {
	if h.Kind == "" {
		return ""
	}
	return h.Kind + "=" + h.ID
}

// linkHintKinds is the closed set. A hint whose kind is not here is refused
// rather than carried: an unknown landing is a link this build cannot honour,
// and silently dropping it would make the link mean something else.
var linkHintKinds = map[string]bool{"bookmark": true, "shelf": true, "stash": true}
```

2. Add `Hint LinkHint` to `Link` (after `Hunk`).

3. Add `?` to the three reject sets — each is a one-character edit to the
   `ContainsAny` set, plus the doc comment:

```go
// LinkPathOK reports whether p can be expressed inside a link. A path holding
// '@', ':', '#' or '?' cannot: those are the grammar's own separators.
// Producers call this and refuse to copy rather than emit something that
// reparses as a different place.
func LinkPathOK(p string) bool { return !strings.ContainsAny(p, "@:#?") }
```

In `LinkAbsOK`, change `strings.ContainsAny(s, "@#")` to
`strings.ContainsAny(s, "@#?")` and say so in the comment (`?` joins `@` and
`#` as "not expressible anywhere in the path"; `:` stays exempt for the drive
colon).

In `LinkRefOK`, change `strings.ContainsAny(s, "@:# \t")` to
`strings.ContainsAny(s, "@:#? \t")`, AND tighten its existing `".."` guard:
the function rejects `"..."` today, but git forbids `".."` anywhere in a
refname (`git check-ref-format`), and leaving two dots legal means
`@ref:main..feat` parses as a ref literally named `main..feat` instead of
failing. Change `strings.Contains(s, "...")` to `strings.Contains(s, "..")` —
the three-dot case is subsumed. Add to `TestQuestionMarkIsNotExpressible`:

```go
	if LinkRefOK("main..feat") {
		t.Error("LinkRefOK must reject a refname containing .. (git forbids it, and it collides with the change-set form)")
	}
```

4. In `String()`, render the hint LAST, after the line/hunk switch and before
   `return b.String()`:

```go
	if h := l.Hint.String(); h != "" {
		b.WriteByte('?')
		b.WriteString(h)
	}
```

5. In `ParseLink`, strip the hint FIRST — before the `#<hunk>` scan, because
   the grammar puts the hint after the hunk. Insert immediately after
   `body := s[len(LinkScheme):]`:

```go
	// ?<hint> is the LAST element of the grammar, so it is stripped FIRST:
	// everything before it is an ordinary link, and nothing else in the
	// grammar may contain '?' (LinkPathOK / LinkAbsOK / LinkRefOK all reject
	// it), which is what makes the FIRST '?' unambiguously the separator.
	if i := strings.IndexByte(body, '?'); i >= 0 {
		h, err := parseLinkHint(body[i+1:])
		if err != nil {
			return Link{}, err
		}
		l.Hint = h
		body = body[:i]
	}
```

…but `l` is declared *after* `body` today. Move the `l := Link{Side: NoteSideNew}`
declaration up so it sits immediately after `body := s[len(LinkScheme):]`, then
insert the block above it. (The existing `if body == "" { return linkErr("no
repository") }` check stays where it is, after the `#` scan.)

6. Add the hint parser at the bottom of the file, next to `LinkRefOK`:

```go
// parseLinkHint reads "<kind>=<id>". Both halves are mandatory, the kind must
// be one linkHintKinds knows, and the id may not contain a grammar separator
// — a hint that cannot round-trip is refused at parse time rather than
// silently reshaped.
func parseLinkHint(s string) (LinkHint, error) {
	i := strings.IndexByte(s, '=')
	if i < 0 {
		return LinkHint{}, fmt.Errorf("%w: a hint reads <kind>=<id>, got %q", ErrLink, s)
	}
	kind, id := s[:i], s[i+1:]
	if !linkHintKinds[kind] {
		return LinkHint{}, fmt.Errorf("%w: unknown hint kind %q (want bookmark, shelf or stash)", ErrLink, kind)
	}
	if id == "" {
		return LinkHint{}, fmt.Errorf("%w: hint %q has no id", ErrLink, kind)
	}
	if strings.ContainsAny(id, "@:#?/ \t") {
		return LinkHint{}, fmt.Errorf("%w: %q is not a hint id", ErrLink, id)
	}
	return LinkHint{Kind: kind, ID: id}, nil
}
```

- [ ] **Step 4: Run the tests and watch them pass**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
go test ./internal/model/ -count=1
```

Expected: PASS, **including the pre-existing round-trip tables** — the hint is
additive and must not change any existing link's parse.

- [ ] **Step 5: Check no producer now emits a refused link**

`LinkPathOK`/`LinkAbsOK`/`LinkRefOK` got stricter, so a path or refname with
`?` that used to produce a link now refuses. That is the intent. Confirm the
callers all handle a `false` return by refusing to copy rather than by
proceeding:

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
grep -rn "LinkPathOK\|LinkAbsOK\|LinkRefOK" internal/ --include=*.go | grep -v _test
```

Read each hit. Every one must be a guard whose failure path refuses. If one
proceeds, that is a bug to fix in this task.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
git add internal/model/link.go internal/model/link_test.go
git commit -m "feat(model): gg:// links carry a ?<hint> naming the surface they were copied from"
```

---

## Task 2: `@ref:<name>` and `@<a>..<b>` in the grammar

**Files:**
- Modify: `internal/model/link.go`
- Test: `internal/model/link_test.go`

**Interfaces:**
- Consumes: `LinkHint` and the `?`-stripping body from Task 1.
- Produces:
  ```go
  // LinkTarget gains:
  //   Ref  string     // set iff the target was "ref:<name>"; State == StateCommitted
  //   Pair *LinkPair  // set iff the target was "<a>..<b>"
  type LinkPair struct{ A, B string } // two-dot; each half a sha or a refname
  ```

- [ ] **Step 1: Write the failing tests**

Append to `internal/model/link_test.go`:

```go
// @ref:<name> addresses a branch or tag TIP — unbounded, a point (spec §3.2).
func TestLinkRefRoundTrip(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"gg://gigagit@ref:main",
		"gg://gigagit@ref:feat/unified-links",
		"gg://gigagit/internal/a.go@ref:main",
		"gg://gigagit/internal/a.go@ref:main:42",
		"gg://gigagit/internal/a.go@ref:main#3",
		"gg://gigagit@ref:main?bookmark=b1",
	} {
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			l, err := ParseLink(s)
			if err != nil {
				t.Fatalf("ParseLink(%q): %v", s, err)
			}
			if l.Target.State != StateCommitted || l.Target.Ref == "" {
				t.Fatalf("target = %+v, want a committed ref target", l.Target)
			}
			if l.Target.Commit != "" || l.Target.Preview != nil || l.Target.Pair != nil {
				t.Fatalf("a ref target must set ONLY Ref, got %+v", l.Target)
			}
			if got := l.String(); got != s {
				t.Errorf("String(ParseLink(%q)) = %q", s, got)
			}
		})
	}
}

// @<a>..<b> is the two-dot CHANGE-SET — bounded, a pair (spec §3.2). Each half
// is a sha or a refname; mixing is allowed, because the common producer emits
// a resolved parent sha and a user may reasonably type a branch name.
func TestLinkPairRoundTrip(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"gg://gigagit@abc1234def..abc9999fff",
		"gg://gigagit@main..feat/x",
		"gg://gigagit@abc1234def..feat/x",
		// Both halves short and hex: at the GRAMMAR layer these are refnames,
		// not shas, and domain resolves them. A length rule here would forbid
		// a branch literally named "abc".
		"gg://gigagit@abc..def",
		"gg://gigagit/internal/a.go@abc1234def..abc9999fff",
		"gg://gigagit@abc1234def..abc9999fff?stash=0",
	} {
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			l, err := ParseLink(s)
			if err != nil {
				t.Fatalf("ParseLink(%q): %v", s, err)
			}
			if l.Target.Pair == nil {
				t.Fatalf("target = %+v, want a pair", l.Target)
			}
			if got := l.String(); got != s {
				t.Errorf("String(ParseLink(%q)) = %q", s, got)
			}
		})
	}
}

// Three dots keep their SHIPPED meaning (a merge preview of two BRANCH names)
// and must not be swallowed by the new two-dot form.
func TestThreeDotStillParsesAsAPreview(t *testing.T) {
	t.Parallel()
	l, err := ParseLink("gg://gigagit@main...feat/x")
	if err != nil {
		t.Fatal(err)
	}
	if l.Target.Preview == nil || l.Target.Pair != nil {
		t.Fatalf("target = %+v, want a preview and no pair", l.Target)
	}
}

func TestLinkRefAndPairRejects(t *testing.T) {
	t.Parallel()
	for name, s := range map[string]string{
		"empty ref":            "gg://gigagit@ref:",
		"ref with @":           "gg://gigagit@ref:fe@at",
		"pair missing a half":  "gg://gigagit@abc1234def..",
		"pair missing b half":  "gg://gigagit@..abc1234def",
		"pair with three dots": "gg://gigagit@a..b..c",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseLink(s); err == nil {
				t.Fatalf("ParseLink(%q) should have failed", s)
			} else if !errors.Is(err, ErrLink) {
				t.Fatalf("error should wrap ErrLink, got %v", err)
			}
		})
	}
}

// An ALL-DIGIT branch name cannot ride a ref link: the ":<line>" suffix wins,
// by the shipped splitLinkLine rule, so "@ref:123" reads as target "ref" plus
// line 123 and then fails target validation. That is a deliberate refusal, not
// a wrong answer — the line suffix is far commoner than a numeric branch — and
// this test exists so nobody "fixes" it into ambiguity. (Plan 1b ruling R5.)
func TestAllDigitBranchNameIsRefused(t *testing.T) {
	t.Parallel()
	if _, err := ParseLink("gg://gigagit@ref:123"); err == nil {
		t.Fatal("an all-digit ref name must be refused, not silently reparsed")
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
go test ./internal/model/ -run 'TestLinkRef|TestLinkPair|TestThreeDot|TestAllDigit' -count=1
```

Expected: compile failure — `Target.Ref` and `Target.Pair` undefined.

- [ ] **Step 3: Implement**

In `internal/model/link.go`:

1. Add the pair type next to `LinkPreview`:

```go
// LinkPair is the two-dot half of a link target: a CHANGE-SET, git's
// `<a>..<b>`. Unlike LinkPreview (which is deliberately a pair of branch
// NAMES so it travels), each half here may be a sha or a refname: the common
// producer resolves a commit's first parent and emits two shas, while a
// human may reasonably type `@main..feat/x`.
//
// A pair is BOUNDED (spec §3.1): it evaluates to the files that differ
// between its two halves, not to a whole tree.
type LinkPair struct{ A, B string }
```

2. Extend `LinkTarget`:

```go
	// Ref is set iff the target read "ref:<name>" — a branch or tag TIP.
	// State is StateCommitted and Commit is EMPTY: which commit the tip is
	// today is a per-machine, per-moment question only domain can answer.
	// A ref target is UNBOUNDED: it is a point, the whole tree at that tip.
	Ref string
	// Pair is set iff the target carried git's two-dot form (<a>..<b>).
	// State is StateCommitted and Commit is EMPTY. A pair is BOUNDED.
	Pair *LinkPair
```

3. In `String()`, inside the `case StateCommitted:` arm, add the two new forms
   **before** the existing preview/commit handling, so each target renders
   exactly one way:

```go
		if r := l.Target.Ref; r != "" {
			b.WriteString("@ref:")
			b.WriteString(r)
			break
		}
		if p := l.Target.Pair; p != nil {
			b.WriteByte('@')
			b.WriteString(p.A)
			b.WriteString("..")
			b.WriteString(p.B)
			break
		}
```

4. In `ParseLink`'s `default:` target arm, the order is **three dots, then
   `ref:`, then two dots, then a bare sha**. Three-dot must stay first so
   `a...b` is never read as the pair `a` / `.b`. Insert after the existing
   three-dot block and before the `isHexLink(tail)` check:

```go
		if name, ok := strings.CutPrefix(tail, "ref:"); ok {
			if !LinkRefOK(name) {
				return linkErr("%q is not a branch or tag name", name)
			}
			l.Target = LinkTarget{State: StateCommitted, Ref: name}
			break
		}
		if i := strings.Index(tail, ".."); i >= 0 {
			a, bb := tail[:i], tail[i+2:]
			if a == "" || bb == "" {
				return linkErr("a change-set names <a>..<b>, got %q", tail)
			}
			// Each half is a sha or a refname. LinkRefOK already rejects the
			// grammar's separators AND ".." (Task 1 tightened it), so a half
			// that would reparse as a different pair cannot get through.
			okHalf := func(s string) bool { return isShaLink(s) || LinkRefOK(s) }
			if !okHalf(a) || !okHalf(bb) {
				return linkErr("%q is not a pair of commits or branch names", tail)
			}
			l.Target = LinkTarget{State: StateCommitted, Pair: &LinkPair{A: a, B: bb}}
			break
		}
```

Note `LinkRefOK` accepts a short hex string like `"abc"`, so `@abc..def`
passes `okHalf` as a pair of *refnames*, which is why it sits in the
round-trip table rather than the reject table. That is correct at the grammar
layer: `domain` resolves each half and reports "unknown revision" if neither a
ref nor a commit exists there.

5. Update `ParseLink`'s doc comment: add the `@ref:<name>` and `@<a>..<b>`
   lines to the grammar block, and note the hint.

6. Update the `default:` error prose so it names every accepted form:

```go
		return linkErr("target must be \"staged\", a commit sha of 7 to 64 hex characters, \"ref:<name>\", <a>..<b> or <target>...<source>, got %q", tail)
```

7. `Address()` is documented as not meaningful for a preview link; extend that
   comment to cover a ref and a pair target for the same reason (the commit is
   a per-machine resolution).

- [ ] **Step 4: Run the tests and watch them pass**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
go test ./internal/model/ -count=1
```

Expected: PASS. If a pre-existing test breaks, the new arms are stealing a
target that used to parse another way — fix the ORDER, not the old test.

- [ ] **Step 5: Mirror the grammar in the JS twin**

`internal/web/static/links.js` holds the browser's copy of the grammar. Check
whether it parses targets at all:

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
grep -n "staged\|\\.\\.\\.\|LinkScheme\|gg://" internal/web/static/links.js
```

If it only *builds* links (never parses a target), no change is needed here —
say so in the commit message. If it parses, add the three forms and keep the
Go tests' examples as its fixtures.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
git add internal/model/link.go internal/model/link_test.go
git commit -m "feat(model): gg:// links address a branch tip (@ref:<name>) and a change-set (@<a>..<b>)"
```

---

## Task 3: `EndpointRef` and `EndpointPair`

**Files:**
- Modify: `internal/model/model.go`
- Test: `internal/model/endpoint_exhaustive_test.go`

**Interfaces:**
- Consumes: nothing from Tasks 1–2 (the Endpoint layer knows nothing about
  links).
- Produces:
  ```go
  const (
      EndpointRef  EndpointKind = … // a branch/tag tip by NAME — UNBOUNDED
      EndpointPair                  // a resolved <a>..<b> sha pair — BOUNDED
  )
  func RefEndpoint(name string) (Endpoint, error)   // LinkRefOK
  func PairEndpoint(a, b string) (Endpoint, error)  // both 7..64 hex
  func (e Endpoint) Ref() string                     // "" for other kinds
  func (e Endpoint) PairA() string                   // "" for other kinds
  func (e Endpoint) PairB() string                   // "" for other kinds
  ```

- [ ] **Step 1: Add the two table rows FIRST and watch the build break**

This is the guard doing its job: add the rows before the kinds exist, confirm
the package does not compile, and you have proved the table is load-bearing.

In `internal/model/endpoint_exhaustive_test.go`, add a column to
`endpointCase` and two rows to `endpointCases()`:

```go
type endpointCase struct {
	kind     EndpointKind
	name     string
	build    func(*testing.T) Endpoint
	display  string
	live     bool
	cacheTag string
	// cacheTagPanics marks the one kind whose CacheTag has NO safe answer:
	// EndpointRef. Returning the ref name would put a MOVING value in the
	// session diff-cache key (plan 1a's headline bug), and returning "" would
	// make two different refs collide inside CompareFiles's singleflight key.
	// Holding an unresolved ref where a cache key is needed is a programming
	// error, so it panics like any other endpointKindBug. When this is set,
	// cacheTag is ignored.
	cacheTagPanics bool
	bounded  bool
	source   FileSource
	locator  string
}
```

Add `bounded:` to every existing row (`false` for worktree/index/commit,
`true` for shelf), then the two new rows:

```go
		{
			kind: EndpointRef,
			name: "ref",
			build: func(t *testing.T) Endpoint {
				t.Helper()
				e, err := RefEndpoint("feat/unified-links")
				if err != nil {
					t.Fatalf("RefEndpoint: %v", err)
				}
				return e
			},
			display:        "feat/unified-links",
			live:           true, // a tip MOVES: nothing may cache a diff against it
			cacheTagPanics: true,
			bounded:        false, // a point: the whole tree at that tip
			source:         SourceCommit,
			locator:        "feat/unified-links", // `git show <ref>:<path>` is correct
		},
		{
			kind: EndpointPair,
			name: "pair",
			build: func(t *testing.T) Endpoint {
				t.Helper()
				e, err := PairEndpoint("abc1234def5678", "abc9999fff0000")
				if err != nil {
					t.Fatalf("PairEndpoint: %v", err)
				}
				return e
			},
			display:  "abc1234..abc9999",
			live:     false,
			cacheTag: "pair:abc1234def5678..abc9999fff0000",
			bounded:  true,
			source:   SourceCommit,
			locator:  "abc9999fff0000", // the NEW side is what a file read means
		},
```

Extend `TestEndpointMethodsMatchTheTable` with the two new assertions:

```go
			if got := e.Bounded(); got != c.bounded {
				t.Errorf("Bounded() = %v, want %v", got, c.bounded)
			}
			if c.cacheTagPanics {
				func() {
					defer func() {
						if recover() == nil {
							t.Errorf("CacheTag() on kind %s must panic", c.name)
						}
					}()
					_ = e.CacheTag()
				}()
			} else if got := e.CacheTag(); got != c.cacheTag {
				t.Errorf("CacheTag() = %q, want %q", got, c.cacheTag)
			}
```

Add the constructor-reject table:

```go
// A pair of one commit is legal and means "nothing changed" (ruling R7).
func TestPairEndpointAcceptsIdenticalHalves(t *testing.T) {
	t.Parallel()
	if _, err := PairEndpoint("abc1234def5678", "abc1234def5678"); err != nil {
		t.Fatalf("PairEndpoint(x, x) must be legal: %v", err)
	}
}

func TestNewEndpointConstructorsReject(t *testing.T) {
	t.Parallel()
	if _, err := RefEndpoint(""); err == nil {
		t.Error("RefEndpoint(\"\") must fail")
	}
	if _, err := RefEndpoint("fe@at"); err == nil {
		t.Error("RefEndpoint must reject a name LinkRefOK refuses")
	}
	for _, tc := range [][2]string{
		{"", "abc1234def5678"},
		{"abc1234def5678", ""},
		{"abc1", "abc1234def5678"},              // too short
		{"zzzz123def5678", "abc1234def5678"},    // not hex
		// NOTE: PairEndpoint(x, x) is NOT rejected -- ruling R7. A fully
		// merged branch's three-dot pair legitimately has base == source,
		// and an empty bounded set is a result, not an error (spec section 6).
	} {
		if _, err := PairEndpoint(tc[0], tc[1]); err == nil {
			t.Errorf("PairEndpoint(%q, %q) must fail", tc[0], tc[1])
		}
	}
}
```

- [ ] **Step 2: Run and watch it fail to compile**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
go test ./internal/model/ -count=1
```

Expected: `undefined: EndpointRef`, `undefined: RefEndpoint`, etc. That is the
exhaustiveness guard proving itself.

- [ ] **Step 3: Implement the kinds**

In `internal/model/model.go`:

1. Extend the iota block — **above `endpointKindCount`**:

```go
	EndpointCommit                // a commit, by Hash
	EndpointShelf                 // a shelved commit's frozen changed-file set, by ShelfID
	EndpointRef                   // a branch/tag TIP by name — unbounded, and it MOVES
	EndpointPair                  // a resolved <a>..<b> sha pair — bounded (a change-set)
```

2. Extend the struct:

```go
type Endpoint struct {
	kind    EndpointKind
	hash    string // commit hash when kind == EndpointCommit; "" otherwise
	shelfID string // shelf entry id when kind == EndpointShelf; "" otherwise
	ref     string // branch/tag name when kind == EndpointRef; "" otherwise
	// a, b are the two RESOLVED shas of an EndpointPair. Deliberately shas and
	// not names, and deliberately no three-dot flag (plan 1b ruling R1): a
	// three-dot pair means merge-base(target, source)..source, and the merge
	// base is a RESOLUTION that only domain can make. Storing names here would
	// put a moving value in CacheTag, which is plan 1a's headline bug.
	a, b string
}
```

3. Accessors, next to `Hash`/`ShelfID`:

```go
// Ref is the branch/tag name, or "" for any other kind.
func (e Endpoint) Ref() string { return e.ref }

// PairA and PairB are the older and newer sha of a pair endpoint, or "" for
// any other kind.
func (e Endpoint) PairA() string { return e.a }
func (e Endpoint) PairB() string { return e.b }
```

4. Constructors, next to `CommitEndpoint`/`ShelfEndpoint`:

```go
// RefEndpoint names a branch or tag TIP. It is UNBOUNDED — a point, the whole
// tree at that tip — and it MOVES, so domain resolves it to a commit endpoint
// before anything compares or caches against it (plan 1b ruling R2).
func RefEndpoint(name string) (Endpoint, error) {
	if !LinkRefOK(name) {
		return Endpoint{}, fmt.Errorf("%w: %q is not a branch or tag name", ErrEndpoint, name)
	}
	return Endpoint{kind: EndpointRef, ref: name}, nil
}

// PairEndpoint names a CHANGE-SET: the files that differ between two resolved
// commits, a → b, older → newer. Both halves must already be full object ids —
// see the field comment on Endpoint.a for why names are refused here.
//
// a == b is LEGAL and means the empty change-set (ruling R7): a fully merged
// branch's three-dot pair has merge-base(target, source) == source, and an
// empty bounded set is a result, not an error (spec §6).
func PairEndpoint(a, b string) (Endpoint, error) {
	if !commitHashOK(a) {
		return Endpoint{}, fmt.Errorf("%w: pair's older side %q is not a commit id", ErrEndpoint, a)
	}
	if !commitHashOK(b) {
		return Endpoint{}, fmt.Errorf("%w: pair's newer side %q is not a commit id", ErrEndpoint, b)
	}
	return Endpoint{kind: EndpointPair, a: a, b: b}, nil
}
```

`commitHashOK` is the 7..64-hex predicate `CommitEndpoint` already applies —
if it is inline there, extract it to a helper first (a pure rename, no
behaviour change) so all three constructors share one rule.

5. Method arms. Each existing switch gets two more explicit cases:

```go
// Display
	case EndpointRef:
		return e.ref
	case EndpointPair:
		return shortEndpointHash(e.a) + ".." + shortEndpointHash(e.b)
```

with

```go
// shortEndpointHash is the 7-character display form of an object id, matching
// Display's commit arm.
func shortEndpointHash(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}
```

(refactor `Display`'s commit arm to use it, so there is one rule).

```go
// FileRef
	case EndpointRef:
		// `git show <ref>:<path>` is correct and is NOT a cache key, so a ref
		// is a legitimate file source even though CacheTag refuses it.
		return FileRef{Source: SourceCommit, Locator: e.ref, Path: path}
	case EndpointPair:
		// A pair's file content is its NEW side: the change-set's members are
		// read as they are at b. The older side is reached through the
		// comparison, not through a single FileRef.
		return FileRef{Source: SourceCommit, Locator: e.b, Path: path}

// IsLive
	case EndpointRef:
		return true // a tip moves; nothing may cache a diff against it
	case EndpointPair:
		return false // two resolved shas: frozen

// CacheTag
	case EndpointRef:
		// NO SAFE ANSWER — see plan 1b ruling R3 and the cacheTagPanics column
		// in endpoint_exhaustive_test.go. Resolve the ref to a commit first.
		panic(endpointKindBug("CacheTag", e.kind))
	case EndpointPair:
		return "pair:" + e.a + ".." + e.b

// Bounded
	case EndpointRef:
		return false // a POINT: the whole tree at that tip
	case EndpointPair:
		return true // a PAIR: a finite change-set
```

- [ ] **Step 4: Run the tests and watch them pass**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
go test ./internal/model/ -count=1
go build ./...
```

Expected: PASS and a clean build. A compile error elsewhere means a switch over
`EndpointKind` lives outside `internal/model` and needs the new arms — find it
and add them:

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
grep -rn "EndpointCommit\|EndpointShelf" internal/ --include=*.go | grep -v _test | grep -v internal/model/
```

Every hit is a place that now sees two more possible kinds. Read each. A
`switch` with a `default:` that means "commit" is the trap plan 1a removed from
`internal/model`; if one survives in `git`, `domain`, `cli`, `web` or `mcp`,
fix it here rather than leaving the new kinds to fall into it. `DiffTreeFiles`
(`internal/git/compare.go:22`) already returns an explicit "unsupported
endpoint pair" error for anything it does not know — that is the correct shape
and needs no change.

- [ ] **Step 5: Prove the method-value trap is absent**

Plan 1a shipped two bugs where `x.Hash` (no parens) was passed into an `any`
context — a legal method VALUE that compiles, passes vet and is silently wrong.
The three new accessors (`Ref`, `PairA`, `PairB`) are fresh instances of the
same hazard. Prove absence by renaming them and rebuilding:

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
sed -i 's/\.PairA()/.PairAXX()/g; s/) PairA()/) PairAXX()/' $(grep -rl "PairA" internal/ --include=*.go)
go build ./... 2>&1 | head
# expect: errors ONLY at call sites you just renamed. Then revert:
git checkout -- internal/
```

If the build is clean after the rename, some call site used the method VALUE.
(Skip this step if `grep -rn "\.PairA\b\|\.PairB\b\|\.Ref\b" internal/` shows
every use is immediately followed by `()`.)

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
git add internal/model/model.go internal/model/endpoint_exhaustive_test.go
git commit -m "feat(model): Endpoint gains a ref tip (unbounded) and a resolved sha pair (bounded)"
```

---

## Task 4: `FileSet` and `EvalEndpoint`

**Files:**
- Create: `internal/domain/fileset.go`, `internal/domain/fileset_test.go`
- Create: `internal/git/lsfiles.go`, `internal/git/lsfiles_test.go`

**Interfaces:**
- Consumes: `model.EndpointRef`, `model.EndpointPair`, `model.PairEndpoint`
  from Task 3.
- Produces:
  ```go
  type FileSet struct{ /* unexported */ }
  func (f FileSet) Bounded() bool
  func (f FileSet) Paths() []string          // sorted; nil when unbounded
  func (f FileSet) Endpoint() model.Endpoint // always a RESOLVED endpoint
  func (s *Service) EvalEndpoint(ctx context.Context, e model.Endpoint) (FileSet, error)
  func (s *Service) endpointPaths(ctx context.Context, e model.Endpoint) (map[string]bool, error)
  ```

- [ ] **Step 1: Write the failing test**

Create `internal/domain/fileset_test.go`:

```go
package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
)

// EvalEndpoint is the spec's §3.1 rule made executable: a POINT evaluates to
// an unbounded set (every file in the repo), a PAIR to a bounded one (the
// enumerated paths it changed).
func TestEvalEndpointBoundedness(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "hi\n")
	svc := newTestService(t, dir)
	ctx := context.Background()

	head, ok, err := svc.ResolveRev(ctx, "HEAD")
	if err != nil || !ok {
		t.Fatalf("ResolveRev(HEAD): %v ok=%v", err, ok)
	}
	gittest.Write(t, dir, "b.txt", "two\n")
	gittest.Run(t, dir, "add", "b.txt")
	gittest.Run(t, dir, "commit", "-m", "second")
	second, _, err := svc.ResolveRev(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	commit, err := model.CommitEndpoint(second)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := model.PairEndpoint(head, second)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := model.RefEndpoint("main")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		ep      model.Endpoint
		bounded bool
		paths   []string
	}{
		{"worktree", model.WorkTreeEndpoint(), false, nil},
		{"index", model.IndexEndpoint(), false, nil},
		{"commit", commit, false, nil},
		{"ref", ref, false, nil},
		{"pair", pair, true, []string{"b.txt"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs, err := svc.EvalEndpoint(ctx, tc.ep)
			if err != nil {
				t.Fatalf("EvalEndpoint: %v", err)
			}
			if fs.Bounded() != tc.bounded {
				t.Fatalf("Bounded() = %v, want %v", fs.Bounded(), tc.bounded)
			}
			if tc.paths != nil {
				got := fs.Paths()
				if len(got) != len(tc.paths) || got[0] != tc.paths[0] {
					t.Fatalf("Paths() = %v, want %v", got, tc.paths)
				}
			}
		})
	}
}

// A ref endpoint MUST be resolved to a commit before it leaves EvalEndpoint:
// its CacheTag panics by design (plan 1b ruling R3), so a FileSet that still
// held one would blow up the moment anything keyed a cache on it.
func TestEvalEndpointResolvesARefToACommit(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "hi\n")
	svc := newTestService(t, dir)

	ref, err := model.RefEndpoint("main")
	if err != nil {
		t.Fatal(err)
	}
	fs, err := svc.EvalEndpoint(context.Background(), ref)
	if err != nil {
		t.Fatalf("EvalEndpoint: %v", err)
	}
	if fs.Endpoint().Kind() != model.EndpointCommit {
		t.Fatalf("a ref must evaluate to a COMMIT endpoint, got kind %d", fs.Endpoint().Kind())
	}
	// The proof: this would panic if the ref survived.
	if tag := fs.Endpoint().CacheTag(); tag == "main" || tag == "" {
		t.Fatalf("CacheTag = %q — a moving name must never reach the cache key", tag)
	}
}

// An unresolvable ref is an error, not an empty set (spec §6).
func TestEvalEndpointUnknownRef(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, gittest.BasicRepo(t, "hi\n"))
	ref, err := model.RefEndpoint("no-such-branch")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EvalEndpoint(context.Background(), ref); err == nil {
		t.Fatal("an unresolvable ref must error")
	}
}
```

**Before writing this test, find the existing service-construction helper.**
`internal/domain`'s tests already build a `*Service` against a temp repo; grep
for it and use that name instead of the placeholder `newTestService`:

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
grep -rn "func newTestService\|func newSvc\|domain.New(" internal/domain/*_test.go | head
```

Likewise check `internal/gittest`'s real helper names (`BasicRepo`, `Run`,
`Write`) before using them:

```bash
grep -rn "^func " internal/gittest/*.go | head -20
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
go test ./internal/domain/ -run TestEvalEndpoint -count=1
```

Expected: compile failure — `EvalEndpoint` undefined.

- [ ] **Step 3a: Two carry-overs from Task 3's review, in `internal/model/model.go`**

Both are small and both belong here, because this task is the reason the first
one can never fire in practice.

1. **Give `EndpointRef.CacheTag()`'s panic its own message.** It currently
   reuses `endpointKindBug`, whose text reads "unknown EndpointKind N; add a
   case arm and a row…" — which is actively misleading here: the arm and the
   row both exist, and the panic is a deliberate refusal, not a missing case.
   A developer who hits it needs to be told what to do instead. Replace just
   that arm's panic argument:

```go
	case EndpointRef:
		// NO SAFE ANSWER, deliberately (plan 1b ruling R3). A ref name MOVES,
		// so returning it would key the session diff cache on a value that
		// changes underneath it — the exact bug plan 1a shipped a fix for —
		// and returning "" would collide two different refs inside
		// domain.CompareFiles's singleflight key. Resolve the ref to a commit
		// first: domain.EvalEndpoint does it, and every compare path runs
		// through it.
		panic("model: CacheTag on an unresolved ref endpoint (" + e.ref +
			"); resolve it to a commit first — see domain.EvalEndpoint")
```

Update the `cacheTagPanics` row's comment in
`endpoint_exhaustive_test.go` to point at this message rather than at
`endpointKindBug`. The test asserts only that it panics, so no assertion
changes.

2. **Route `CommitEndpoint` through `commitHashOK`.** Task 3 extracted that
   predicate but left `CommitEndpoint` with its own inline copy of the 7..64
   bound, so the bound now lives in two places and a future change could
   update one and miss the other. Keep `CommitEndpoint`'s distinct error text
   — that is why it was left alone — but take the *predicate* from the shared
   helper:

```go
func CommitEndpoint(hash string) (Endpoint, error) {
	if !commitHashOK(hash) {
		return Endpoint{}, fmt.Errorf("%w: %q is not a commit id (want 7 to 64 hex characters)", ErrEndpoint, hash)
	}
	return Endpoint{kind: EndpointCommit, hash: hash}, nil
}
```

Read `CommitEndpoint`'s current error text before you replace it and keep the
wording it already has if a test asserts on it — `grep -rn "is not a commit" internal/`.

Run `go test ./internal/model/ -count=1` after these two before moving on.

- [ ] **Step 3: Add the tracked-path probe to `internal/git`**

`endpointPaths` needs the member set of an unbounded endpoint. A commit has
`TreeFiles`; the index and the working tree do not. Create
`internal/git/lsfiles.go`:

```go
package git

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// ListFiles returns tracked paths relative to the working-tree root.
//
// deleted=false is the INDEX's entries (plain `git ls-files`, which is
// `--cached` — they are the same list). That IS the index endpoint's member
// set, and it is the tracked half of the working tree's.
//
// deleted=true is `git ls-files --deleted`: entries the index still holds but
// which are gone from disk. The working-tree member set must SUBTRACT these —
// git has no "files on disk" listing, and calling a `rm`'d file present sends
// a later byte read after a file that is not there.
//
// -z so paths with spaces or non-ASCII bytes (which git otherwise quotes)
// come through raw, matching UntrackedFiles and DiffTreeFiles.
func (r *Repo) ListFiles(ctx context.Context, deleted bool) ([]string, error) {
	b := gitcmd.New("ls-files").Arg("-z").ArgIf(deleted, "--deleted")
	res, err := r.Runner.Run(ctx, "git ls-files (member set)", b.ToArgv())
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range strings.Split(res.Stdout, "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}
```

Test it in `internal/git/lsfiles_test.go` with a real repo: two tracked files,
one untracked and one tracked-then-`rm`'d. Assert that `ListFiles(ctx, false)`
holds both tracked names (including the `rm`'d one — it is still in the index)
and not the untracked one, and that `ListFiles(ctx, true)` holds exactly the
`rm`'d one. Follow the `newTestRepo` pattern the package's other tests use
(`grep -n "func newTestRepo" internal/git/*_test.go`).

Add `ListFiles` to the `GitOps`-style interface the `Service` holds **only if
the service reaches git through one** — check:

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
grep -n "TreeFiles" internal/domain/*.go | grep -v _test | head
```

If `s.repo` is the concrete `*git.Repo`, nothing more is needed.

- [ ] **Step 4: Implement `FileSet` and `EvalEndpoint`**

Create `internal/domain/fileset.go`:

```go
package domain

import (
	"context"
	"fmt"
	"sort"

	"github.com/homeend/gigagit/internal/model"
)

// FileSet is what a link evaluates to — the spec's §3.1 one rule made a type.
//
//	A PAIR is BOUNDED.   A POINT is UNBOUNDED.
//
// A bounded set carries an enumerated, sorted path list; an unbounded one
// stands for every file in the repository at some state and carries no list
// (enumerating a 100GB monorepo to compare one file against it is exactly what
// §3.5's "you can only ever scale DOWN" forbids).
//
// Either way the set carries a RESOLVED endpoint, which is the per-path byte
// source: fs.Endpoint().FileRef(p) → ResolveBytes. "Resolved" is load-bearing
// — EvalEndpoint turns an EndpointRef into an EndpointCommit, so no moving
// name survives into a comparison or a cache key.
type FileSet struct {
	ep      model.Endpoint
	paths   []string
	bounded bool // explicit, NOT len(paths) > 0: an empty bounded set is legal
	// has answers "does this member have BYTES at ep" for every enumerated
	// path. It is NOT redundant with paths (ruling R6): a change-set
	// enumerates the files it DELETED, and `git show <b>:<deleted>` cannot
	// read them. Without this the comparison asks for a file that is not
	// there and hard-errors instead of reporting A/D. nil ⇒ every member has
	// bytes, which is the shelf and whole-tree case.
	has map[string]bool
}

// Bounded reports whether the set enumerates its paths.
func (f FileSet) Bounded() bool { return f.bounded }

// Paths is the sorted key set, or nil when unbounded.
func (f FileSet) Paths() []string { return f.paths }

// Endpoint is the resolved byte source for a path in this set.
func (f FileSet) Endpoint() model.Endpoint { return f.ep }

// Has reports whether path has readable bytes at this set's endpoint. Only
// meaningful for a bounded set; the unbounded lane asks endpointPaths instead.
func (f FileSet) Has(path string) bool {
	if f.has == nil {
		return true
	}
	return f.has[path]
}

// boundedSetWith and unboundedSet are the only two constructors, so the
// invariant "bounded ⇒ paths is sorted and non-nil" holds by construction.
// A nil has means "every member has bytes".
func boundedSetWith(ep model.Endpoint, paths []string, has map[string]bool) FileSet {
	if paths == nil {
		paths = []string{}
	}
	sort.Strings(paths)
	return FileSet{ep: ep, paths: paths, bounded: true, has: has}
}

func unboundedSet(ep model.Endpoint) FileSet { return FileSet{ep: ep} }

// EvalEndpoint turns an endpoint into its file set (spec §4.2). It is the
// ONLY place an EndpointRef is allowed to die: a ref is resolved to a commit
// here, before the set — and therefore any cache key or git invocation built
// from it — can see it (plan 1b ruling R2).
//
// Deliberately NOT wrapped in one query(): each underlying read takes its own
// Read reservation, and nesting a gated read inside a held reservation can
// deadlock behind a queued writer. shelfCompareFiles's doc comment records the
// same rule.
func (s *Service) EvalEndpoint(ctx context.Context, e model.Endpoint) (FileSet, error) {
	switch e.Kind() {
	case model.EndpointWorkTree, model.EndpointIndex, model.EndpointCommit:
		return unboundedSet(e), nil

	case model.EndpointRef:
		sha, ok, err := s.ResolveRev(ctx, e.Ref())
		if err != nil {
			return FileSet{}, err
		}
		if !ok {
			return FileSet{}, fmt.Errorf("unknown revision %q", e.Ref())
		}
		commit, err := model.CommitEndpoint(sha)
		if err != nil {
			return FileSet{}, err
		}
		return unboundedSet(commit), nil

	case model.EndpointPair:
		a, err := model.CommitEndpoint(e.PairA())
		if err != nil {
			return FileSet{}, err
		}
		b, err := model.CommitEndpoint(e.PairB())
		if err != nil {
			return FileSet{}, err
		}
		files, err := s.CompareFiles(ctx, a, b)
		if err != nil {
			return FileSet{}, err
		}
		// The set's byte source is the pair's NEW side: a member's content
		// means "as it is at b". A member the pair DELETED has NO bytes at b,
		// so it is enumerated with has[path] = false and CompareSets reports
		// it through A/D instead of trying to read it (ruling R6).
		//
		// KNOWN GAP (1b): DiffTreeFiles passes -M, so a rename arrives as one
		// "R" row carrying the NEW path only; the old path is not enumerated.
		// A renamed file therefore compares as an addition on this side. That
		// matches what `git diff --name-status` reports and is left as-is.
		paths := make([]string, 0, len(files))
		has := make(map[string]bool, len(files))
		for _, f := range files {
			paths = append(paths, f.Path)
			has[f.Path] = f.Status != "D"
		}
		return boundedSetWith(b, paths, has), nil

	case model.EndpointShelf:
		members, err := s.ShelfCommitFiles(ctx, e.ShelfID())
		if err != nil {
			return FileSet{}, err
		}
		// Every tar member has bytes, so has stays nil.
		paths := make([]string, 0, len(members))
		for _, f := range members {
			paths = append(paths, f.Path)
		}
		return boundedSetWith(e, paths, nil), nil

	default:
		return FileSet{}, fmt.Errorf("EvalEndpoint: unusable endpoint kind %d", e.Kind())
	}
}

// endpointPaths is the MEMBER SET of an unbounded endpoint — the probe
// "does this tree contain <path>". It exists only for the unbounded × bounded
// lane of CompareSets, where a key absent from the tree reads as A or D
// (spec §3.5).
//
// One listing, not one probe per path: a bounded side may hold hundreds of
// members, and `git cat-file -e` per path would be hundreds of invocations.
func (s *Service) endpointPaths(ctx context.Context, e model.Endpoint) (map[string]bool, error) {
	var list []string
	switch e.Kind() {
	case model.EndpointCommit:
		files, err := s.TreeFiles(ctx, e.Hash())
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			list = append(list, f.Path)
		}
	case model.EndpointIndex:
		var err error
		if list, err = s.ListFiles(ctx, false); err != nil {
			return nil, err
		}
	case model.EndpointWorkTree:
		// tracked ∪ untracked − deleted. The subtraction matters: ls-files
		// lists the INDEX, so a tracked file the user `rm`'d still appears
		// there, and calling it "present" would send ResolveBytes after a
		// file that is not on disk.
		tracked, err := s.ListFiles(ctx, false)
		if err != nil {
			return nil, err
		}
		untracked, err := s.UntrackedFiles(ctx)
		if err != nil {
			return nil, err
		}
		gone, err := s.ListFiles(ctx, true)
		if err != nil {
			return nil, err
		}
		removed := make(map[string]bool, len(gone))
		for _, p := range gone {
			removed[p] = true
		}
		for _, p := range append(tracked, untracked...) {
			if !removed[p] {
				list = append(list, p)
			}
		}
	default:
		// A bounded endpoint never reaches here: CompareSets asks for the
		// member set only of the side its own Bounded() said is unbounded.
		return nil, fmt.Errorf("endpointPaths: %d is not an unbounded endpoint", e.Kind())
	}
	set := make(map[string]bool, len(list))
	for _, p := range list {
		set[p] = true
	}
	return set, nil
}
```

`s.ListFiles` and `s.UntrackedFiles` may not exist as `Service` methods yet.
Check and add thin wrappers next to `TreeFiles` in `internal/domain/query.go`,
following its exact `query(ctx, s, "<key>", …)` shape:

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
grep -n "func (s \*Service) TreeFiles\|func (s \*Service) UntrackedFiles" internal/domain/*.go
```

- [ ] **Step 5: Run the tests and watch them pass**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
go test ./internal/git/ ./internal/domain/ -count=1
```

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
git add internal/domain/fileset.go internal/domain/fileset_test.go \
        internal/domain/query.go internal/git/lsfiles.go internal/git/lsfiles_test.go
git commit -m "feat(domain): EvalEndpoint turns an endpoint into a bounded or unbounded FileSet"
```

---

## Task 5: `CompareSets` — the 2×2

**Files:**
- Create: `internal/domain/comparesets.go`, `internal/domain/comparesets_test.go`
- Modify: `internal/domain/compare_entries.go` (fold `shelfCompareFiles` in)

**Interfaces:**
- Consumes: `FileSet`, `EvalEndpoint`, and `endpointHas(ctx, e, paths)` from
  Task 4. NOTE: Task 4's own section below still shows an earlier
  `endpointPaths(ctx, e)` that enumerated the whole unbounded side; a review
  found that violates the design rule and it was reworked before this task
  started. The shipped name and signature are
  `endpointHas(ctx context.Context, e model.Endpoint, paths []string) (map[string]bool, error)`
  — read the function in `internal/domain/fileset.go`, not Task 4's text.
- Produces:
  ```go
  func (s *Service) CompareSets(ctx context.Context, left, right FileSet) ([]model.CommitFile, error)
  ```

- [ ] **Step 1: Write the failing matrix test**

This is the test that earns the design. Create
`internal/domain/comparesets_test.go` with an explicit **kind × kind matrix**
against a real repo. Build a fixture with three commits so every lane has real
data:

```go
package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
)

// compareFixture is a repo with three commits:
//
//	c1: a.txt = "one"
//	c2: a.txt = "two",  b.txt = "bee"   (c1..c2 changes a.txt and ADDS b.txt)
//	c3: a.txt = "three", b.txt DELETED  (c2..c3 changes a.txt and DELETES b.txt)
//
// The deletion is load-bearing, not decoration: the pair c2..c3 ENUMERATES
// b.txt while having no bytes for it at c3, which is the case ruling R6
// exists for. A fixture with no deletions lets a comparison that blindly
// reads every member pass.
type compareFixture struct {
	dir        string
	svc        *Service
	c1, c2, c3 string
}

// NOTE on helper names, checked against the tree (a previous task's brief got
// these wrong): `internal/domain`'s service-plus-repo helper is
// `newRealRepo(t) (string, *Service)` in compare_test.go, NOT `newTestService`.
// `internal/gittest` has `BasicRepo` and `Run` but NO `Write` — use
// `os.WriteFile`. And `gittest.BasicRepo(t, s)` creates **README.md** holding
// s, not a.txt, so this fixture writes a.txt itself.
func newCompareFixture(t *testing.T) compareFixture {
	t.Helper()
	dir, svc := newRealRepo(t)
	ctx := context.Background()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rev := func() string {
		t.Helper()
		sha, ok, err := svc.ResolveRev(ctx, "HEAD")
		if err != nil || !ok {
			t.Fatalf("ResolveRev(HEAD): %v ok=%v", err, ok)
		}
		return sha
	}

	write("a.txt", "one\n")
	gittest.Run(t, dir, "add", "a.txt")
	gittest.Run(t, dir, "commit", "-m", "c1")
	c1 := rev()

	write("a.txt", "two\n")
	write("b.txt", "bee\n")
	gittest.Run(t, dir, "add", "a.txt", "b.txt")
	gittest.Run(t, dir, "commit", "-m", "c2")
	c2 := rev()

	write("a.txt", "three\n")
	gittest.Run(t, dir, "rm", "-f", "b.txt")
	gittest.Run(t, dir, "add", "a.txt")
	gittest.Run(t, dir, "commit", "-m", "c3")
	c3 := rev()

	return compareFixture{dir: dir, svc: svc, c1: c1, c2: c2, c3: c3}
}

func (f compareFixture) eval(t *testing.T, e model.Endpoint) FileSet {
	t.Helper()
	fs, err := f.svc.EvalEndpoint(context.Background(), e)
	if err != nil {
		t.Fatalf("EvalEndpoint: %v", err)
	}
	return fs
}

func statuses(files []model.CommitFile) map[string]string {
	m := make(map[string]string, len(files))
	for _, f := range files {
		m[f.Path] = f.Status
	}
	return m
}

// The three lanes of spec §3.5, each asserted on real data, and each asserted
// to yield a BOUNDED result (compare is CLOSED — that closure is what makes
// "compare everything with everything" terminate).
func TestCompareSetsMatrix(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)
	ctx := context.Background()
	mustCommit := func(h string) model.Endpoint {
		e, err := model.CommitEndpoint(h)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	mustPair := func(a, b string) model.Endpoint {
		e, err := model.PairEndpoint(a, b)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}

	t.Run("unbounded x unbounded: the differing paths", func(t *testing.T) {
		got, err := f.svc.CompareSets(ctx, f.eval(t, mustCommit(f.c1)), f.eval(t, mustCommit(f.c3)))
		if err != nil {
			t.Fatal(err)
		}
		st := statuses(got)
		// b.txt was added at c2 and deleted at c3, so it is absent from BOTH
		// trees and never appears.
		if st["a.txt"] != "M" || len(st) != 1 {
			t.Fatalf("c1 vs c3 = %v, want exactly a.txt M", st)
		}
	})

	t.Run("bounded x bounded: symmetric over the union", func(t *testing.T) {
		// c1..c2 enumerates {a.txt (M), b.txt (A)}; c2..c3 enumerates
		// {a.txt (M), b.txt (D)}. BOTH sets contain b.txt — but the right set
		// has no BYTES for it (it is the file c3 deleted), which is exactly
		// ruling R6. A comparison that read every member would hard-error
		// here on `git show <c3>:b.txt`.
		got, err := f.svc.CompareSets(ctx, f.eval(t, mustPair(f.c1, f.c2)), f.eval(t, mustPair(f.c2, f.c3)))
		if err != nil {
			t.Fatalf("a deleted member must not be read: %v", err)
		}
		st := statuses(got)
		// a.txt is in both sets with bytes on both, differing → M.
		if st["a.txt"] != "M" {
			t.Fatalf("a.txt = %q, want M; full result %v", st["a.txt"], st)
		}
		// b.txt: bytes on the left (it exists at c2), none on the right ⇒ D.
		if st["b.txt"] != "D" {
			t.Fatalf("b.txt = %q, want D (no bytes on the right); full result %v", st["b.txt"], st)
		}
	})

	t.Run("unbounded x bounded: projected onto the bounded side", func(t *testing.T) {
		// c1's TREE is {a.txt}. The pair c1..c2 is {a.txt, b.txt}. The result
		// must be keyed on the PAIR's paths — never on the whole tree.
		got, err := f.svc.CompareSets(ctx, f.eval(t, mustCommit(f.c1)), f.eval(t, mustPair(f.c1, f.c2)))
		if err != nil {
			t.Fatal(err)
		}
		st := statuses(got)
		if len(st) != 2 {
			t.Fatalf("the result must be keyed on the BOUNDED side's 2 paths, got %v", st)
		}
		if st["a.txt"] != "M" {
			t.Fatalf("a.txt = %q, want M", st["a.txt"])
		}
		// b.txt does not exist at c1, and the bounded side is the RIGHT one,
		// so its absence on the left reads as ADDED.
		if st["b.txt"] != "A" {
			t.Fatalf("b.txt = %q, want A (absent on the unbounded LEFT)", st["b.txt"])
		}
	})

	t.Run("bounded x unbounded: the direction flips", func(t *testing.T) {
		// The mirror of the previous case. This is the generalization of
		// shelfCommitCompare's shelfIsRight flag: which side is bounded
		// decides whether a missing key reads A or D.
		got, err := f.svc.CompareSets(ctx, f.eval(t, mustPair(f.c1, f.c2)), f.eval(t, mustCommit(f.c1)))
		if err != nil {
			t.Fatal(err)
		}
		st := statuses(got)
		if st["b.txt"] != "D" {
			t.Fatalf("b.txt = %q, want D (absent on the unbounded RIGHT)", st["b.txt"])
		}
	})
}

// DISJOINT bounded sets are a result, never an error (spec §6). Disjoint does
// NOT mean empty: every member of one side is absent from the other, so the
// result is all A and D. (Two EMPTY sets are the separate, degenerate case,
// and they do compare to nothing.)
func TestCompareSetsDisjointBoundedIsAResultNotAnError(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)
	ctx := context.Background()
	c1 := mustTestCommit(t, f.c1)
	c2 := mustTestCommit(t, f.c2)
	left := boundedSetWith(c1, []string{"a.txt"}, map[string]bool{"a.txt": true})
	right := boundedSetWith(c2, []string{"b.txt"}, map[string]bool{"b.txt": true})

	got, err := f.svc.CompareSets(ctx, left, right)
	if err != nil {
		t.Fatalf("disjoint sets must not error: %v", err)
	}
	st := statuses(got)
	if st["a.txt"] != "D" || st["b.txt"] != "A" || len(st) != 2 {
		t.Fatalf("disjoint sets compare to all A/D, got %v", st)
	}

	empty, err := f.svc.CompareSets(ctx,
		boundedSetWith(c1, nil, nil), boundedSetWith(c1, nil, nil))
	if err != nil {
		t.Fatalf("two empty sets must not error: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("two empty sets compare to nothing, got %v", empty)
	}
}

func mustTestCommit(t *testing.T, h string) model.Endpoint {
	t.Helper()
	e, err := model.CommitEndpoint(h)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// Compare is CLOSED: the result is a bounded set, so it can be an endpoint of
// the next comparison. This is the property that makes "compare everything
// with everything" terminate (spec §3.5).
func TestCompareSetsResultIsBounded(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)
	got, err := f.svc.CompareSets(context.Background(),
		f.eval(t, mustTestCommit(t, f.c1)), f.eval(t, mustTestCommit(t, f.c3)))
	if err != nil {
		t.Fatal(err)
	}
	// "Bounded" here means: the caller can enumerate it. A []CommitFile IS
	// the enumeration, so the assertion is that it is finite and complete,
	// which the matrix test already pins. What this test adds is the
	// round trip: feeding the result's paths back in as a bounded set works.
	paths := make([]string, 0, len(got))
	for _, cf := range got {
		paths = append(paths, cf.Path)
	}
	again := boundedSetWith(mustTestCommit(t, f.c3), paths, nil)
	if !again.Bounded() {
		t.Fatal("a comparison result must be re-usable as a bounded set")
	}
}
```

- [ ] **Step 2: Run and watch it fail**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
go test ./internal/domain/ -run TestCompareSets -count=1
```

Expected: compile failure — `CompareSets` undefined.

- [ ] **Step 3: Implement**

Create `internal/domain/comparesets.go`:

```go
package domain

import (
	"bytes"
	"context"
	"sort"

	"github.com/homeend/gigagit/internal/model"
)

// CompareSets is the spec's §3.5 algebra, and the whole of it:
//
//	unbounded × unbounded  →  BOUNDED   git diff A B: the differing paths
//	bounded   × bounded    →  BOUNDED   symmetric over the union of members;
//	                                    present in one only ⇒ A / D
//	unbounded × bounded    →  BOUNDED   project the tree onto the bounded
//	                                    side's paths; a key absent from the
//	                                    tree ⇒ A / D
//
// THE ASYMMETRY IS LOAD-BEARING: you can only ever scale DOWN. Growing the
// bounded side to the tree's size would mean comparing one file against every
// file in a 100GB monorepo, so the bounded side always supplies the key set.
//
// Every row yields a bounded result, which is what makes compare CLOSED: a
// result is the same KIND as a merge preview, so it is saveable, linkable and
// usable as an endpoint of the next comparison.
//
// Statuses follow the tree-diff convention the whole codebase already uses:
// only-in-left → D, only-in-right → A, differing bytes → M, identical →
// omitted. Results are sorted by path.
func (s *Service) CompareSets(ctx context.Context, left, right FileSet) ([]model.CommitFile, error) {
	switch {
	case !left.Bounded() && !right.Bounded():
		// Both points: git already answers this in one invocation, INCLUDING
		// the untracked-file handling a working-tree side needs. Reuse it
		// rather than re-deriving it here.
		return s.CompareFiles(ctx, left.Endpoint(), right.Endpoint())

	case left.Bounded() && right.Bounded():
		return s.compareBoundedPair(ctx, left, right)

	case right.Bounded():
		// unbounded × bounded: the RIGHT side supplies the keys, so a key the
		// left (unbounded) side lacks reads as ADDED.
		return s.compareProjected(ctx, left, right, right)

	default:
		// bounded × unbounded: the LEFT side supplies the keys, so a key the
		// right (unbounded) side lacks reads as DELETED.
		return s.compareProjected(ctx, left, right, left)
	}
}

// compareBoundedPair walks the UNION of two enumerated sets. A path in only
// one of them is A or D by which side holds it; a path in both is compared by
// bytes.
func (s *Service) compareBoundedPair(ctx context.Context, left, right FileSet) ([]model.CommitFile, error) {
	inRight := make(map[string]bool, len(right.Paths()))
	for _, p := range right.Paths() {
		inRight[p] = true
	}
	inLeft := make(map[string]bool, len(left.Paths()))
	var out []model.CommitFile
	for _, p := range left.Paths() {
		inLeft[p] = true
		// Membership is not presence (ruling R6): a change-set ENUMERATES the
		// files it deleted, and those have no bytes at its endpoint. Decide
		// A/D/M from presence on both sides, and only read bytes when both
		// sides actually have some.
		row, err := s.compareOne(ctx, left, right, p, left.Has(p), inRight[p] && right.Has(p))
		if err != nil {
			return nil, err
		}
		if row.Status != "" {
			out = append(out, row)
		}
	}
	for _, p := range right.Paths() {
		if !inLeft[p] && right.Has(p) {
			out = append(out, model.CommitFile{Status: "A", Path: p})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// compareProjected is the unbounded × bounded lane. keys is whichever of
// left/right is the bounded one — it supplies the key set, always, because
// §3.5 says you may only ever scale DOWN.
//
// The old shelfCommitCompare took a shelfIsRight flag to decide whether a
// missing key read A or D. That flag is gone: compareOne derives it from WHICH
// SIDE holds the path, which is the same answer and cannot be passed wrong.
func (s *Service) compareProjected(ctx context.Context, left, right, keys FileSet) ([]model.CommitFile, error) {
	unbounded := left
	if !right.Bounded() {
		unbounded = right
	}
	// endpointHas takes the KEY SET, deliberately: the unbounded side is never
	// enumerated. A `git ls-tree -r` over a ~1M-path head, per comparison, to
	// answer a question about a handful of files is the "you can only ever
	// scale DOWN" rule violated head-on. Task 4 reworked the probe for exactly
	// this call — read its doc comment before changing the shape here.
	present, err := s.endpointHas(ctx, unbounded.Endpoint(), keys.Paths())
	if err != nil {
		return nil, err
	}
	// Each side answers presence its own way: the unbounded side from the
	// listing, the bounded side from its own has map.
	var out []model.CommitFile
	for _, p := range keys.Paths() {
		onLeft := left.Bounded() && left.Has(p) || !left.Bounded() && present[p]
		onRight := right.Bounded() && right.Has(p) || !right.Bounded() && present[p]
		row, err := s.compareOne(ctx, left, right, p, onLeft, onRight)
		if err != nil {
			return nil, err
		}
		if row.Status != "" {
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// compareOne is the single A/D/M decision, given whether path has bytes on
// each side. It is the ONLY place that decides a status, so the two lanes
// above cannot drift apart:
//
//	left only   → D      right only  → A
//	neither     → omitted (nothing to say: it is in the key set because the
//	             bounded side enumerates it, but nobody holds bytes)
//	both        → read both and compare; identical ⇒ omitted
//
// A zero CommitFile (Status == "") means "omit this path".
func (s *Service) compareOne(ctx context.Context, left, right FileSet, path string, onLeft, onRight bool) (model.CommitFile, error) {
	switch {
	case onLeft && !onRight:
		return model.CommitFile{Status: "D", Path: path}, nil
	case !onLeft && onRight:
		return model.CommitFile{Status: "A", Path: path}, nil
	case !onLeft && !onRight:
		return model.CommitFile{}, nil
	}
	same, err := s.sameBytes(ctx, left.Endpoint(), right.Endpoint(), path)
	if err != nil {
		return model.CommitFile{}, err
	}
	if same {
		return model.CommitFile{}, nil
	}
	return model.CommitFile{Status: "M", Path: path}, nil
}

// sameBytes reads one path from both endpoints and compares. Both sides are
// read through FileRef/ResolveBytes, which is what lets a frozen shelf tar and
// a live commit sit on either side without this function knowing the
// difference.
func (s *Service) sameBytes(ctx context.Context, left, right model.Endpoint, path string) (bool, error) {
	lb, err := s.ResolveBytes(ctx, left.FileRef(path))
	if err != nil {
		return false, err
	}
	rb, err := s.ResolveBytes(ctx, right.FileRef(path))
	if err != nil {
		return false, err
	}
	return bytes.Equal(lb, rb), nil
}
```

- [ ] **Step 4: Run the tests and watch them pass**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
go test ./internal/domain/ -run TestCompareSets -count=1
```

- [ ] **Step 5: Fold `shelfCompareFiles` into `CompareSets`**

`shelfShelfCompare` is now `compareBoundedPair` and `shelfCommitCompare` is now
`compareProjected`. Replace the bodies in
`internal/domain/compare_entries.go` with the adapter, keeping the exported
behaviour byte-identical:

```go
// shelfCompareFiles lists the files that differ when at least one side is a
// frozen shelf entry. It is now a thin adapter: a shelf endpoint evaluates to
// a BOUNDED set (its members) and everything else to an unbounded one, so the
// two shelf lanes are just two rows of the general algebra — shelf↔shelf is
// bounded × bounded, shelf↔commit is bounded × unbounded, and the
// shelfIsRight direction flag is gone: compareOne derives the same answer
// from which side actually holds the path.
//
// Deliberately NOT wrapped in one query(): each underlying read takes its own
// Read reservation, and nesting a gated read inside a held reservation can
// deadlock behind a queued writer.
func (s *Service) shelfCompareFiles(ctx context.Context, left, right model.Endpoint) ([]model.CommitFile, error) {
	l, err := s.EvalEndpoint(ctx, left)
	if err != nil {
		return nil, err
	}
	r, err := s.EvalEndpoint(ctx, right)
	if err != nil {
		return nil, err
	}
	return s.CompareSets(ctx, l, r)
}
```

Delete `shelfShelfCompare` and `shelfCommitCompare`.

**Also harden `livePairSpec` in the same file.** Task 3's audit found it: its
`default:` arm returns `model.DiffSpec{}`, which git reads as
`index → working tree`. Today only the four kinds it enumerates can reach it,
but Task 7 hands `ComparePatch` whatever endpoint a link produced — so an
`EndpointPair` or `EndpointRef` arriving here would SILENTLY DIFF THE WRONG
THING instead of failing. That is the worst class of bug this plan can ship.
Give it an explicit arm per kind and an error return:

```go
// livePairSpec maps a non-shelf endpoint pair onto the DiffSpec vocabulary.
//
// It used to end in a `default:` that meant "index → working tree", which was
// safe only while the four enumerated kinds were the only ones that existed.
// They are not any more: a pair or a ref endpoint reaching that arm would
// have produced a diff of something else entirely, with no error — so an
// unhandled pair is now a refusal.
func livePairSpec(left, right model.Endpoint) (model.DiffSpec, error) {
	switch {
	case left.Kind() == model.EndpointCommit && right.Kind() == model.EndpointCommit:
		return model.DiffSpec{Rev: left.Hash() + ".." + right.Hash()}, nil
	case left.Kind() == model.EndpointCommit && right.Kind() == model.EndpointIndex:
		return model.DiffSpec{Cached: true, Rev: left.Hash()}, nil
	case left.Kind() == model.EndpointCommit && right.Kind() == model.EndpointWorkTree:
		return model.DiffSpec{Rev: left.Hash()}, nil
	case left.Kind() == model.EndpointIndex && right.Kind() == model.EndpointWorkTree:
		return model.DiffSpec{}, nil // bare `git diff` is already index → worktree
	}
	return model.DiffSpec{}, fmt.Errorf("livePairSpec: unsupported endpoint pair %d → %d", left.Kind(), right.Kind())
}
```

Update its one caller in `ComparePatch` to handle the error. Add a test in
`internal/domain/comparesets_test.go` that a pair endpoint reaching
`ComparePatch` errors rather than returning a diff of the working tree:

```go
// A pair endpoint reaching the LIVE patch lane must refuse, not silently
// render `git diff` (index → working tree). livePairSpec's old default arm
// would have done exactly that.
func TestComparePatchRefusesAnUnsupportedPair(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)
	pair, err := model.PairEndpoint(f.c1, f.c2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ComparePatch(context.Background(), pair, model.WorkTreeEndpoint()); err == nil {
		t.Fatal("a pair endpoint must not reach the live patch lane silently")
	}
}
```

- [ ] **Step 6: Run the WHOLE domain suite — the old shelf tests are the proof**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
go test ./internal/domain/ -count=1
```

Expected: PASS, with `internal/domain/compare_entries_test.go`'s existing shelf
cases green **unchanged**. Those tests are the regression proof that the
generalization preserved behaviour — do not edit them to fit the new code. If
one fails, the algebra disagrees with the shipped shelf semantics and the
ALGEBRA is what to fix.

One behaviour to watch: the old `shelfCommitCompare` listed the tree with
`TreeFiles(commitHash)`; `endpointHas` asks a pathspec-limited `ls-tree` for a
commit, which answers the same question for the paths that matter. If a test
fails on the `D`/`A` direction, re-read `compareProjected`'s `missing`
parameter against the old `shelfIsRight` flag — `shelfIsRight == true` meant
the shelf was the RIGHT/newer side, which is `missing = "A"`.

- [ ] **Step 7: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
git add internal/domain/comparesets.go internal/domain/comparesets_test.go internal/domain/compare_entries.go
git commit -m "feat(domain): CompareSets implements the bounded/unbounded compare algebra; the shelf lanes fold into it"
```

---

## Task 6: `EvalLink` — one link, one file set

**Files:**
- Create: `internal/domain/evallink.go`, `internal/domain/evallink_test.go`

**Interfaces:**
- Consumes: `model.LinkHint`/`LinkTarget.Ref`/`LinkTarget.Pair` (Tasks 1–2),
  `FileSet`/`EvalEndpoint` (Task 4), `CompareSets` (Task 5).
- Produces:
  ```go
  func (s *Service) EndpointForLink(ctx context.Context, l model.Link) (model.Endpoint, error)
  func (s *Service) EvalLink(ctx context.Context, l model.Link) (FileSet, error)
  ```

- [ ] **Step 1: Write the failing test**

Create `internal/domain/evallink_test.go`. Cover, at minimum:

```go
// EvalLink is where the spec's §3.3 rule 1 lives: COMPARE IGNORES THE HINT.
// A bookmarked commit and the same commit picked off the log are ONE endpoint
// — that is what keeps the matrix a 2×2 instead of a bookmark × shelf ×
// commit grid.
func TestEvalLinkIgnoresTheHint(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)
	ctx := context.Background()

	plain, err := model.ParseLink("gg://x@" + f.c2)
	if err != nil {
		t.Fatal(err)
	}
	hinted, err := model.ParseLink("gg://x@" + f.c2 + "?bookmark=whatever")
	if err != nil {
		t.Fatal(err)
	}
	a, err := f.svc.EvalLink(ctx, plain)
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.svc.EvalLink(ctx, hinted)
	if err != nil {
		t.Fatal(err)
	}
	if a.Endpoint() != b.Endpoint() || a.Bounded() != b.Bounded() {
		t.Fatalf("the hint changed the endpoint: %+v vs %+v", a.Endpoint(), b.Endpoint())
	}
}

// A /<path> makes ANY link bounded to exactly one member (spec §3.2's last
// grammar row). This is the row that makes "compare one file against a
// commit" work without a special case.
func TestEvalLinkWithAPathIsBoundedToOne(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)
	l, err := model.ParseLink("gg://x/a.txt@" + f.c2)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := f.svc.EvalLink(context.Background(), l)
	if err != nil {
		t.Fatal(err)
	}
	if !fs.Bounded() || len(fs.Paths()) != 1 || fs.Paths()[0] != "a.txt" {
		t.Fatalf("a path link must bound the set to that one path, got bounded=%v paths=%v", fs.Bounded(), fs.Paths())
	}
}

// A /<path> naming a file that does not EXIST at the target is still a legal
// bounded set — it is bounded to one member with no bytes — and comparing it
// reports A or D rather than failing to read the file (ruling R6).
func TestEvalLinkWithAnAbsentPath(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)
	ctx := context.Background()
	// b.txt exists at c2 and was deleted at c3.
	gone, err := model.ParseLink("gg://x/b.txt@" + f.c3)
	if err != nil {
		t.Fatal(err)
	}
	there, err := model.ParseLink("gg://x/b.txt@" + f.c2)
	if err != nil {
		t.Fatal(err)
	}
	ls, err := f.svc.EvalLink(ctx, there)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := f.svc.EvalLink(ctx, gone)
	if err != nil {
		t.Fatalf("a link to an absent path must evaluate, not error: %v", err)
	}
	got, err := f.svc.CompareSets(ctx, ls, rs)
	if err != nil {
		t.Fatalf("comparing against an absent path must not error: %v", err)
	}
	if len(got) != 1 || got[0].Path != "b.txt" || got[0].Status != "D" {
		t.Fatalf("got %v, want exactly b.txt D", got)
	}
}

// A three-dot preview link resolves to the merge base, exactly as the shipped
// preview vocabulary does: merge-base(target, source)..source. The endpoint is
// a PAIR of resolved shas — never two branch names (plan 1b ruling R1).
func TestEvalLinkThreeDotResolvesToTheMergeBase(t *testing.T) {
	t.Parallel()
	// Build: main at c1, a branch "topic" with one commit on top.
	// Assert the endpoint is EndpointPair and PairA() is the merge base.
	// (Use gittest to create the branch; read the base with
	// `git merge-base main topic` through gittest.Run and compare.)
}

// The worked examples of spec §3.6, end to end: a link on each side, the lane
// they land in, and the result.
func TestCompareTwoLinks(t *testing.T) {
	t.Parallel()
	f := newCompareFixture(t)
	ctx := context.Background()
	left, err := model.ParseLink("gg://x/a.txt@" + f.c1)
	if err != nil {
		t.Fatal(err)
	}
	right, err := model.ParseLink("gg://x@" + f.c3)
	if err != nil {
		t.Fatal(err)
	}
	ls, err := f.svc.EvalLink(ctx, left)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := f.svc.EvalLink(ctx, right)
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.CompareSets(ctx, ls, rs)
	if err != nil {
		t.Fatal(err)
	}
	// bounded (one file) × unbounded (a tree): the result is keyed on a.txt
	// alone, and a.txt's bytes differ between c1 and c3.
	if len(got) != 1 || got[0].Path != "a.txt" || got[0].Status != "M" {
		t.Fatalf("got %v, want exactly a.txt M", got)
	}
}
```

Fill in `TestEvalLinkThreeDotResolvesToTheMergeBase`'s body — it is the one
case the plan leaves you to write, because the fixture shape depends on
`gittest`'s branch helper, which you will have read by now. Do not leave it
empty.

- [ ] **Step 2: Run and watch it fail**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
go test ./internal/domain/ -run 'TestEvalLink|TestCompareTwoLinks' -count=1
```

- [ ] **Step 3: Implement**

Create `internal/domain/evallink.go`:

```go
package domain

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/model"
)

// EndpointForLink is the ONE answer to "what does this link address" — the
// pairing rules that used to live in internal/cli/compare.go, moved here so
// the TUI, CLI, MCP and web cannot disagree (spec §4.2).
//
// The hint is IGNORED here, deliberately: spec §3.3 rule 1 says compare
// ignores it, and that is what keeps the algebra a 2×2 instead of a
// bookmark × shelf × commit matrix. The ONE exception is a hint that is the
// only CONTENT source — a shelved working-tree file, whose bytes were never in
// git — which is why a shelf hint on a link with NO address becomes a shelf
// endpoint rather than a working-tree one.
func (s *Service) EndpointForLink(ctx context.Context, l model.Link) (model.Endpoint, error) {
	t := l.Target

	// The address-less shelf link: the hint is the content (spec §3.3).
	if t.State == model.StateUnstaged && t.Commit == "" && t.Ref == "" &&
		t.Pair == nil && t.Preview == nil && l.Hint.Kind == "shelf" {
		return model.ShelfEndpoint(l.Hint.ID)
	}

	switch {
	case t.State == model.StateUnstaged:
		return model.WorkTreeEndpoint(), nil
	case t.State == model.StateStaged:
		return model.IndexEndpoint(), nil
	}

	switch {
	case t.Ref != "":
		return model.RefEndpoint(t.Ref)

	case t.Pair != nil:
		a, err := s.resolveHalf(ctx, t.Pair.A)
		if err != nil {
			return model.Endpoint{}, err
		}
		b, err := s.resolveHalf(ctx, t.Pair.B)
		if err != nil {
			return model.Endpoint{}, err
		}
		return model.PairEndpoint(a, b)

	case t.Preview != nil:
		// Three dots mean merge-base(target, source)..source — the shipped
		// preview vocabulary (internal/cli/preview.go). The base is a
		// RESOLUTION, which is why the endpoint holds two shas and no
		// three-dot flag (plan 1b ruling R1).
		base, err := s.MergeBase(ctx, t.Preview.Target, t.Preview.Source)
		if err != nil {
			return model.Endpoint{}, err
		}
		src, err := s.resolveHalf(ctx, t.Preview.Source)
		if err != nil {
			return model.Endpoint{}, err
		}
		return model.PairEndpoint(base, src)

	case t.Commit != "":
		return model.CommitEndpoint(t.Commit)
	}
	return model.Endpoint{}, fmt.Errorf("%w: the link addresses nothing comparable", model.ErrLink)
}

// resolveHalf turns one half of a pair — a sha or a refname — into a full
// object id. Full, never `%h`: a short sha honours core.abbrev (legal down to
// 4) and model.CommitEndpoint requires 7..64, so a short resolver turns a
// legal repo config into a hard failure. That regression is plan 1a's third
// bug; do not reintroduce it by reaching for CommitLookup.
func (s *Service) resolveHalf(ctx context.Context, rev string) (string, error) {
	sha, ok, err := s.ResolveRev(ctx, rev)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("unknown revision %q", rev)
	}
	return sha, nil
}

// EvalLink is the whole pipeline of spec §4.1 in one call:
//
//	Link → Endpoint → FileSet
//
// A /<path> narrows the set to that ONE member, whatever the target was: the
// spec's last grammar row, and the reason "compare one file against a commit"
// needs no special case anywhere else.
func (s *Service) EvalLink(ctx context.Context, l model.Link) (FileSet, error) {
	ep, err := s.EndpointForLink(ctx, l)
	if err != nil {
		return FileSet{}, err
	}
	fs, err := s.EvalEndpoint(ctx, ep)
	if err != nil {
		return FileSet{}, err
	}
	if l.Path != "" {
		return s.narrowTo(ctx, fs, l.Path)
	}
	return fs, nil
}

// narrowTo bounds a set to ONE path — the spec's last grammar row, a link
// with a /<path>.
//
// The narrowed set still has to answer "does that path have bytes here"
// (ruling R6), or a link naming a file that does not exist at its target
// would send a byte read after nothing instead of reporting A/D. A set that
// is already bounded knows; an unbounded one is asked once, through the same
// listing CompareSets would use.
func (s *Service) narrowTo(ctx context.Context, fs FileSet, path string) (FileSet, error) {
	if fs.Bounded() {
		return boundedSetWith(fs.Endpoint(), []string{path},
			map[string]bool{path: fs.Has(path)}), nil
	}
	present, err := s.endpointHas(ctx, fs.Endpoint(), []string{path})
	if err != nil {
		return FileSet{}, err
	}
	return boundedSetWith(fs.Endpoint(), []string{path},
		map[string]bool{path: present[path]}), nil
}
```

`s.MergeBase` may not exist. Check, and if not, add it next to the other
preview queries following their exact shape:

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
grep -rn "MergeBase\|merge-base" internal/domain/*.go internal/git/*.go | grep -v _test | head
```

`domain.PreviewEndpoints` already resolves a preview's base — prefer reusing
whatever it calls over adding a second path to the same answer.

- [ ] **Step 4: Run the tests and watch them pass**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
go test ./internal/domain/ -count=1
```

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
git add internal/domain/evallink.go internal/domain/evallink_test.go
git commit -m "feat(domain): EvalLink resolves a gg:// link to a file set; the pairing rules leave the CLI"
```

---

## Task 7: the CLI — `gg compare <link> <link>` and `gg link --ref/--pair`

**Files:**
- Modify: `internal/cli/compare.go`
- Modify: `internal/cli/link.go`
- Test: `internal/cli/compare_test.go`, `internal/cli/link_test.go`

**Interfaces:**
- Consumes: `domain.EvalLink`, `domain.CompareSets`, `model.ParseLink`.
- Produces: no new Go API — this is the user-facing surface.

- [ ] **Step 1: Write the failing CLI tests**

Add to `internal/cli/compare_test.go` (follow the package's existing
`runCLI`-style harness — `grep -n "func run\|cli.Run(" internal/cli/*_test.go | head`):

```go
// gg compare takes a gg:// link on EITHER side, and mixing a link with the
// old vocabulary works — links are the primary spelling, not a mode.
func TestCompareAcceptsLinks(t *testing.T) { /* … */ }

// The old spellings keep working, unchanged, with the same exit codes.
func TestCompareBackCompatVocabulary(t *testing.T) { /* … */ }

// An unparseable link is exit 2 (usage), a resolve failure is exit 1 — the
// shipped convention (spec §6).
func TestCompareLinkExitCodes(t *testing.T) { /* … */ }

// A link naming a DIFFERENT repository is refused with an explicit message,
// not silently compared against this one (spec §9: cross-repo is deferred).
func TestCompareRefusesACrossRepoLink(t *testing.T) { /* … */ }
```

Write the bodies out in full when you implement — the harness shape decides
them, and the plan cannot guess the helper's name. Each must assert the exit
code AND the stdout/stderr text.

Add to `internal/cli/link_test.go`:

```go
// gg link --ref emits a branch-tip link; --pair emits a change-set link.
func TestLinkRefAndPairFlags(t *testing.T) { /* … */ }

// --bookmark / --shelf attach the landing hint.
func TestLinkHintFlags(t *testing.T) { /* … */ }

// The flags that name a target are mutually exclusive, like --cached/--rev.
func TestLinkTargetFlagsAreExclusive(t *testing.T) { /* … */ }
```

- [ ] **Step 2: Run and watch them fail**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
go test ./internal/cli/ -run 'TestCompare|TestLink' -count=1
```

- [ ] **Step 3: Teach `gg compare` links**

In `internal/cli/compare.go`, `resolveCompareSpec` gains a link arm as its
FIRST case, and the function's return type changes from
`(model.Endpoint, int)` to `(domain.FileSet, int)` — because a link can be
bounded and an Endpoint alone cannot say so:

```go
	case isLinkArg(tok):
		l, err := model.ParseLink(tok)
		if err != nil {
			fmt.Fprintln(stderr, "compare:", err)
			return domain.FileSet{}, 2
		}
		// Cross-repo compare is deferred (spec §9): both sides evaluate to
		// file sets happily, but each read goes through ONE domain.Service
		// under ONE repogate reservation, so two repos need two reservations
		// acquired in a fixed order to avoid deadlock. Refuse explicitly
		// rather than silently comparing against the wrong checkout.
		if err := svc.LinkNamesThisRepo(ctx, l); err != nil {
			fmt.Fprintln(stderr, "compare:", err)
			return domain.FileSet{}, 2
		}
		// RESOLVE BEFORE EVALUATING — this is a correctness requirement, not
		// a formality. A PARSED local-form link (gg:///abs/checkout/dir/f.go)
		// carries the checkout AND the file path undivided in Repo.Abs with
		// Link.Path == "" — the grammar puts no delimiter between them, and
		// only this machine's repo registry can split them, which is exactly
		// what domain.ResolveLink does. Hand an unresolved local link to
		// EvalLink and a FILE link silently evaluates as a WHOLE-TREE link:
		// a wrong answer with no error, which is the one class this plan has
		// refused to ship. Use the Resolved value's own path and address, not
		// the raw parsed Link. Task 6 recorded this as a precondition on
		// EvalLink's doc comment; honouring it is this task's job.
		fs, err := svc.EvalLink(ctx, l)
		if err != nil {
			fmt.Fprintln(stderr, "compare:", err)
			if errors.Is(err, model.ErrLink) {
				return domain.FileSet{}, 2
			}
			return domain.FileSet{}, 1
		}
		return fs, 0
```

The other three arms (`bookmark:`, `shelf:`, the default) keep their logic and
end with `return s.EvalEndpoint(ctx, ep)` instead of `return ep, 0`.

`svc.LinkNamesThisRepo` does not exist. `domain.ResolveLink` already answers
"which checkout does this link name" (`Resolved.Checkout`) and already has
`ErrLinkUnknownRepo`/`ErrLinkAmbiguous`. Implement the guard as a small helper
in `internal/cli/compare.go` that calls `resolveLinkArg` (already in
`internal/cli/link.go`) and compares `res.Checkout` against `svc.TopLevel(ctx)`,
refusing with:

```
compare: gg://<other>/… names a different repository; cross-repository compare is not supported yet
```

Then `validComparePair` **goes away entirely** — the algebra is total, so there
is no invalid pair any more. Task 6 confirmed there is no domain equivalent of
its ordering refusal and none is needed: `CompareSets` handles every ordering,
including the shelf × live pair the CLI currently rejects. Drop it
deliberately, and say in the commit message that you did. Delete it and its tests, and delete the two
"order endpoints oldest→newest" / "a frozen shelf entry pairs only with…"
error branches from `cmdCompare`. This is the point of the plan: the pairing
rules stop being a CLI concern.

> **Careful:** deleting `validComparePair` changes behaviour for the OLD
> vocabulary too — `gg compare @worktree main` used to be a usage error and
> now compares. That is intended (the algebra makes it meaningful), but it
> MUST be called out in the CHANGELOG and any e2e scenario that asserted the
> refusal must be updated, not deleted. Search for it:
> ```bash
> grep -rn "oldest→newest\|pairs only with" e2e/ internal/ --include=* | grep -v '\.git'
> ```

`cmdCompare` then reads:

```go
	left, code := resolveCompareSpec(svc, args[0], stderr)
	if code != 0 {
		return code
	}
	right, code := domain.FileSet{}, 0
	if len(args) > 1 {
		if right, code = resolveCompareSpec(svc, args[1], stderr); code != 0 {
			return code
		}
	} else {
		if right, code = resolveCompareSpec(svc, "@worktree", stderr); code != 0 {
			return code
		}
	}
	…
	files, err := svc.CompareSets(context.Background(), left, right)
```

`--patch` keeps calling `svc.ComparePatch(ctx, left.Endpoint(), right.Endpoint())`
for now. Note in the code comment that `ComparePatch` still takes endpoints,
not sets, so a `--patch` of a bounded×bounded pair renders the endpoints'
whole diff rather than the projection — **and add a `TODO(plan 3)` naming
that gap**, or fix it by giving `ComparePatch` a set-taking sibling if it is
cheap. Decide during implementation and say which you chose in the commit
message.

**Harden the THREE remaining "default means commit" sites, not one.** Task 3's
review found a third after the audit named two, and the pattern is exactly the
cross-task seam that bit plan 1a: a fix round hardened 2 of 4 frontends while
another task had introduced the same shape in the other 2. Do all of them in
this task, and say in the commit message which four sites you touched.

`internal/web/compare.go`'s `commitEntrySide` (~line 290) and `compareSideWire`
(~line 434) both read `if ep.Kind() == EndpointShelf { … } else { "commit:" +
ep.Hash() }`. A ref or pair endpoint takes the `else` and is wired to the
browser as `commit:` with an EMPTY hash. Give each an explicit per-kind
decision and a refusal for a kind the wire cannot express, matching how the
surrounding handlers report an error (`writeErr` or its local equivalent —
read the file).

**Also harden `endpointProto` in `internal/tui/session_snapshot.go`.** Task 3's
audit found it: its `default:` arm serializes any unknown kind as
`commit:""`. The TUI does not build a ref or pair endpoint in this plan, so it
is unreachable today — but it is precisely the "default means commit" trap the
predecessor plan existed to remove, and leaving one behind while adding two
kinds that would fall into it is how the next silent bug ships. Give it an
explicit arm per kind. A kind the snapshot cannot express is an error or a
panic, matching whatever the surrounding code does with an impossible state —
read the function and its caller before choosing, and say which you chose and
why in the commit message.

Update the usage string to mention links:

```go
"usage: gg compare [--patch] <left> [<right>]   (endpoints: a gg:// link, a commit, @staged, @worktree, bookmark:<id>, shelf:<id>; right defaults to @worktree)"
```

- [ ] **Step 4: Teach `gg link` the new targets**

In `internal/cli/link.go`'s `runLink`, add three flags beside `--cached` and
`--rev`:

```go
	ref := fs.String("ref", "", "address a branch or tag TIP (unbounded: the whole tree there)")
	pair := fs.String("pair", "", "address a CHANGE-SET, <a>..<b> (bounded: what it changed)")
	bookmark := fs.String("bookmark", "", "attach a ?bookmark=<id> landing hint")
	shelf := fs.String("shelf", "", "attach a ?shelf=<id> landing hint")
```

Mutual exclusion: `--cached`, `--rev`, `--preview`, `--ref` and `--pair` all
name the target, so at most one may be set. Replace the two-way check with a
count:

```go
	set := 0
	for _, on := range []bool{*cached, *rev != "", pf.set(), *ref != "", *pair != ""} {
		if on {
			set++
		}
	}
	if set > 1 {
		fmt.Fprintf(stderr, "link: --cached, --rev, --preview, --ref and --pair name the target; use one\n%s\n", linkUsage)
		return 2
	}
```

`--bookmark` and `--shelf` are also mutually exclusive with each other (one
landing), but compose with any target.

In `buildLink`, add the two target arms and the hint, threading the new
parameters through. Rather than growing the signature to eight parameters,
introduce a small options struct:

```go
// linkOpts is what `gg link` was asked to address. Exactly one target field
// is set; the hint composes with any of them.
type linkOpts struct {
	Cached  bool
	Rev     string
	Ref     string
	Pair    string
	Preview *model.LinkPreview
	Hint    model.LinkHint
}
```

and change `buildLink(ctx, svc, workdir, pathArg string, o linkOpts)`. Update
its existing call sites and tests.

The `--ref` arm validates with `model.LinkRefOK` and refuses with the same
prose shape the preview arm uses. The `--pair` arm splits on `..`, resolves
BOTH halves through `svc.ResolveRev` to full shas (so the link is portable and
self-describing — spec §3.2), and emits `LinkPair{A: fullA, B: fullB}`.
Refuse a `...` in `--pair` with "use --preview for a merge preview".

Update `linkUsage`.

- [ ] **Step 5: Run the tests and watch them pass**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
go test ./internal/cli/ ./internal/domain/ ./internal/model/ -count=1
go build ./...
```

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
git add internal/cli/compare.go internal/cli/link.go internal/cli/compare_test.go internal/cli/link_test.go
git commit -m "feat(cli): gg compare takes gg:// links on either side; gg link addresses a tip and a change-set"
```

---

## Task 8: e2e, docs and the agent skill

**Files:**
- Create: `e2e/scenarios/links-compare.toml`
- Modify: `CHANGELOG.md`
- Modify: `internal/agentskill/using-gg.md` and `internal/agentskill/agentskill.go` (`Version`)

- [ ] **Step 1: Write the e2e scenario**

Read two existing scenarios first so the TOML shape is right:

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
ls e2e/scenarios/ | head -20
cat e2e/scenarios/$(ls e2e/scenarios/ | head -1)
```

The scenario must round-trip the whole feature with no Go test helpers:

1. build a repo with two commits,
2. `gg link --rev HEAD` → capture the link,
3. `gg link --pair <c1>..<c2>` → capture,
4. `gg compare <link1> <link2>` → assert the changed-file list,
5. `gg compare <link-with-a-path> <link-to-a-commit>` → assert exactly one row,
6. `gg link --ref main --bookmark b1` → assert the string round-trips through
   `gg link resolve`.

- [ ] **Step 2: Run the e2e stage**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
./test.sh e2e > /tmp/e2e-1b.log 2>&1; echo "EXIT=$?"; tail -40 /tmp/e2e-1b.log
```

Note the redirect-then-read: piping through `tail` would give you tail's exit
code, not the gate's.

- [ ] **Step 3: Update `CHANGELOG.md`**

Under `## Unreleased`, one entry per user-visible change. Be exact — the
CHANGELOG has been wrong three times in this feature's history, and each error
was a claim the code did not support. In particular:

- `gg compare` accepts `gg://` links on either side.
- **Behaviour change:** `gg compare` no longer refuses a "reversed" pair
  (`gg compare @worktree main`). The compare algebra is total, so the ordering
  rule that produced "order endpoints oldest→newest" is gone.
- `gg link` gains `--ref`, `--pair`, `--bookmark` and `--shelf`.
- The link grammar gains `@ref:<name>`, `@<a>..<b>` and `?<hint>`.
- `?` is no longer expressible in a link path, checkout path or refname.

Before committing, re-read each bullet against the diff and delete any you
cannot point at a line for.

- [ ] **Step 4: Update the agent skill**

`internal/agentskill/using-gg.md` teaches agents the CLI. Add the new `gg
compare` and `gg link` forms with one worked example each, then bump
`agentskill.Version` (grep for it). The user runs `gg init --update`
themselves after the merge — do not run it.

- [ ] **Step 5: Update `docs/CLAUDE-details.md` and `CLAUDE.md`**

`CLAUDE.md`'s package map gets a one-line change to the `domain` row (it now
owns the compare algebra) and the `model` row (the grammar grew). Per-feature
detail — the algebra's lanes, the `EndpointRef` CacheTag ruling, the deleted
`validComparePair` — goes to `docs/CLAUDE-details.md`, never into `CLAUDE.md`.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
git add e2e/scenarios/links-compare.toml CHANGELOG.md \
        internal/agentskill/using-gg.md internal/agentskill/agentskill.go \
        CLAUDE.md docs/CLAUDE-details.md
git commit -m "docs(links): e2e round trip, CHANGELOG, agent skill and the package map"
```

---

## Controller checklist (after every task, and once at the end)

1. **Per task:** dispatch a fresh reviewer with the task's section and the
   diff. A reviewer who cannot reproduce a claimed behaviour must say so —
   plan 1a's zero-commit crash was found because a reviewer REPRODUCED an
   implementer's "it falls through gracefully" claim and proved it false.
2. **Whole-branch review at the end, not only per-task.** In plan 1a the
   whole-branch pass found the one bug no per-task review could see: a fix
   round hardened 2 of 4 frontends while a mechanical task had introduced the
   same bug in the other 2. The cross-task seams here are:
   - every `switch` over `EndpointKind` outside `internal/model`,
   - every producer that calls `LinkPathOK`/`LinkAbsOK`/`LinkRefOK` (Task 1
     made all three stricter),
   - every caller of the deleted `validComparePair`,
   - `web/static/links.js` against the Go grammar.
3. **The race gate, by the controller only:**
   ```bash
   cd /mnt/t/others/gigagit/.claude/worktrees/unified-links-1b
   ./test.sh race > /tmp/race-1b.log 2>&1; echo "EXIT=$?"; tail -40 /tmp/race-1b.log
   ```
   Read `EXIT`, never a pipeline's tail.
4. **Do not merge.** The user merges. Ask first.

---

## Self-review against the spec

| spec section | covered by |
|---|---|
| §3.1 the one rule | Task 4 (`FileSet.Bounded`), Task 3 (`Endpoint.Bounded` arms) |
| §3.2 grammar — `ref:` | Task 2 |
| §3.2 grammar — `..` | Task 2 |
| §3.2 separator safety | Task 1 (the `?` reject sets) |
| §3.3 the hint, rules 1–3 | Task 1 (grammar), Task 6 (`EndpointForLink` ignores it; the address-less shelf exception) |
| §3.4 stashes | **Partly.** A stash is a commit, so `@<parent>..<sha>` works with no new code and the e2e scenario can exercise it. The **untracked-files-from-parent-3** rule (a `-u` stash) is NOT implemented here — it needs a stash-aware `EvalEndpoint` arm. **Deferred to plan 2 with the other stash surfaces**; recorded here so it is not lost. |
| §3.5 the algebra | Task 5 |
| §3.6 worked examples | Task 6 (`TestCompareTwoLinks`), Task 8 (e2e) |
| §4.0 validity | Task 3 (constructors + the table) |
| §4.1 no new address type | Task 3 — `Endpoint` grew two kinds; no new type |
| §4.2 two domain functions | Tasks 4, 5, 6 |
| §4.3 new packages | plan 2 / plan 3 — not 1b |
| §4.4 changed packages | `model`, `domain`, `cli`, `git` here; `linknav`/`steer`/`agentskill` in plan 2 (Task 8 bumps the skill for the CLI surface only) |
| §4.5 previews fold in | plan 3 |
| §5.1 the base picker | plan 3 (a UI affordance) |
| §5.3 CLI | Task 7; **`--save` deferred** (ruling R4) |
| §6 error handling | Tasks 6, 7 |
| §7 testing | every task; the kind×kind matrix is Task 5 |
| §9 cross-repo refusal | Task 7 |

**Rulings pinned by a test:** R1/R3 by `endpoint_exhaustive_test.go`'s
`cacheTagPanics` row and `TestEvalEndpointResolvesARefToACommit`; R5 by
`TestAllDigitBranchNameIsRefused`; R6 by the fixture's deletion in
`TestCompareSetsMatrix` and by `TestEvalLinkWithAnAbsentPath`; R7 by
`TestPairEndpointAcceptsIdenticalHalves`.
