# Endpoint Hardening Implementation Plan (plan 1a)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `model.Endpoint` impossible to construct in an invalid state, so
that holding one proves it is consistent — before plan 1b adds three new kinds
to it.

**Architecture:** Parse-don't-validate. `Endpoint`'s fields become unexported
and the only way in is a validating constructor; the zero value becomes
`EndpointInvalid` rather than a silently-valid working tree; every switch over
`EndpointKind` gets explicit case arms and a panicking `default:` instead of
falling through to the commit case; and one table-driven test enumerates every
kind so a kind added without a row fails the build.

**Tech Stack:** Go 1.26, stdlib only. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-09-16-unified-links-design.md` — §4.0
("Validity is a property of the type, not a checklist"). Read §3.1 for the
bounded/unbounded rule that `Bounded()` encodes.

## Global Constraints

- **This is a pure refactor. No new `EndpointKind` values, no behaviour
  change.** `EndpointRef` and `EndpointPair` belong to plan 1b. If a task
  tempts you to add them, stop — the whole point of splitting 1a out is that
  one diff must not both fix these traps and add to them.
- **The build stays green at every commit.** Each task compiles and passes
  `./test.sh unit` on its own. No task may leave `go build ./...` broken.
- **`internal/tui` and `internal/cli` never import `internal/git`** — they
  reach git through `internal/domain` (enforced by `internal/archtest`).
  Nothing here should touch that, but do not work around it if it fires.
- **Tests use a real `git` in a `t.TempDir()`** (`newRepo` / `newTestRepo`
  helpers) or `gitexec.FakeRunner` for argv assertions. Follow TDD.
- **Run tests in the FOREGROUND.** Backgrounding `./test.sh` has repeatedly
  stalled; wait for it.
- Run `./test.sh unit` per task; the controller runs the full `./test.sh race`
  gate once at the end, not per task.

---

## Current state

`internal/model/model.go:258-327` holds the type. Today:

```go
const (
	EndpointWorkTree EndpointKind = iota // 0 — SO Endpoint{} IS A WORKING TREE
	EndpointIndex
	EndpointCommit
	EndpointShelf
)

type Endpoint struct {
	Kind    EndpointKind
	Hash    string // commit hash when Kind == EndpointCommit; "" otherwise
	ShelfID string // shelf entry id when Kind == EndpointShelf; "" otherwise
}
```

Three methods switch on `Kind` and end with `default:` meaning **commit**:
`Display()`, `FileRef(path)`, `CacheTag()`. `IsLive()` is a boolean expression,
not a switch.

### The zero-value sites (audited — all safe, do not "fix" them)

`model.Endpoint{}` appears 14 times outside tests. All are safe under
`EndpointInvalid`, and Task 1 must not change them:

| site | why it is safe |
|---|---|
| `internal/cli/compare.go:131,135,140,148,152,157` | error returns, paired with a non-zero exit code; the value is never read |
| `internal/domain/compare_entries.go:40,48` | error returns alongside a non-nil `error` |
| `internal/mcp/compare.go:45,49,52,56` | error returns alongside a non-nil `error` |
| `internal/tui/files_view.go:46-47` | `closeFilesView` resets `filesLeft`/`filesRight`; both are read **only** under `filesMode == filesModeCompare` (`files_view.go:797-798`, `session_snapshot.go:233-235`), and `closeFilesView` sets `filesMode = filesModeChanged` on the line before |

Under `EndpointInvalid` these become *more* correct: the reset now means
"unset" instead of accidentally meaning "the working tree".

### Scale

| | files | sites |
|---|---|---|
| construction, non-test | 14 | 54 |
| construction, tests | 21 | ~103 |
| field reads (`.Kind`/`.Hash`/`.ShelfID`), non-test | — | ~67 |

---

## File Structure

| file | responsibility after this plan |
|---|---|
| `internal/model/model.go` | `EndpointKind` (with `EndpointInvalid` first and an `endpointKindCount` sentinel last), the `Endpoint` struct with **unexported** fields, the constructors, the accessors, and four total methods each with explicit case arms and a panicking default |
| `internal/model/endpoint_exhaustive_test.go` | **new.** The one table that enumerates every kind. A kind added without a row fails here. |
| `internal/model/endpoint_test.go` | existing; gains constructor-validation tests |
| 14 non-test + 21 test files | construction sites respelled to constructors; field reads respelled to accessors |

`Endpoint` is **never serialized** — `internal/tui/session_snapshot.go`
converts through its own `snapEndpoint` wire type with string kind names
(`endpointProto`, `session_snapshot.go:164`). So renumbering the iota is safe;
Task 1 Step 1 verifies that claim rather than trusting it.

---

## Task 1: `EndpointInvalid`, explicit case arms, and the exhaustiveness table

Renumbering the iota and hardening the switches are **one task** because they
are one concern: the moment `Endpoint{}` stops being a working tree, the three
`default:`-means-commit arms would start rendering it as an empty commit. They
must change together.

**Files:**
- Modify: `internal/model/model.go:258-327`
- Create: `internal/model/endpoint_exhaustive_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `model.EndpointInvalid EndpointKind`; the unexported sentinel
  `endpointKindCount EndpointKind`; `Display() string`, `FileRef(path string) FileRef`,
  `CacheTag() string`, `IsLive() bool` all panic on `EndpointInvalid` and on any
  unknown kind. Field names are **unchanged** in this task (`Kind`, `Hash`,
  `ShelfID` stay exported) so nothing outside `model` needs editing yet.

- [ ] **Step 1: Verify no EndpointKind integer is persisted or compared to a literal**

Renumbering is only safe if no stored value or literal int depends on the
current numbering. Run all three:

```bash
cd /mnt/t/others/gigagit.worktrees/feat-unified-links
grep -rn 'EndpointKind(' internal/ cmd/
grep -rn 'Kind: *[0-9]' internal/ cmd/
grep -rn 'EndpointKind' internal/ cmd/ | grep -i 'toml\|json\|marshal\|encode\|decode'
```

Expected: the first two print nothing; the third prints nothing (the only
serializer is `snapEndpoint`, which uses the strings `"worktree"`, `"index"`,
`"commit"`, `"shelf"`). **If any of them prints a hit, stop and report it** —
the renumbering is not safe and this plan needs revising.

- [ ] **Step 2: Write the failing exhaustiveness test**

Create `internal/model/endpoint_exhaustive_test.go`:

```go
package model

import "testing"

// endpointCase is one row of the table that every EndpointKind must have.
// Adding a kind to the iota block without adding a row here fails
// TestEveryEndpointKindHasATableRow.
type endpointCase struct {
	kind     EndpointKind
	name     string
	build    func() Endpoint
	display  string
	live     bool
	cacheTag string
	source   FileSource
	locator  string
}

func endpointCases() []endpointCase {
	return []endpointCase{
		{
			kind:     EndpointWorkTree,
			name:     "worktree",
			build:    func() Endpoint { return Endpoint{Kind: EndpointWorkTree} },
			display:  "Working Tree",
			live:     true,
			cacheTag: "worktree",
			source:   SourceUnstaged,
			locator:  "",
		},
		{
			kind:     EndpointIndex,
			name:     "index",
			build:    func() Endpoint { return Endpoint{Kind: EndpointIndex} },
			display:  "Staged",
			live:     true,
			cacheTag: "index",
			source:   SourceStaged,
			locator:  "",
		},
		{
			kind:     EndpointCommit,
			name:     "commit",
			build:    func() Endpoint { return Endpoint{Kind: EndpointCommit, Hash: "abc1234def5678"} },
			display:  "abc1234",
			live:     false,
			cacheTag: "abc1234def5678",
			source:   SourceCommit,
			locator:  "abc1234def5678",
		},
		{
			kind:     EndpointShelf,
			name:     "shelf",
			build:    func() Endpoint { return Endpoint{Kind: EndpointShelf, ShelfID: "wt-parser-9f3a1"} },
			display:  "shelf #wt-parser (frozen)",
			live:     false,
			cacheTag: "shelf:wt-parser-9f3a1",
			source:   SourceShelf,
			locator:  "wt-parser-9f3a1",
		},
	}
}

// TestEveryEndpointKindHasATableRow is the guard: every kind between
// EndpointInvalid (exclusive) and endpointKindCount (exclusive) must appear in
// endpointCases exactly once.
func TestEveryEndpointKindHasATableRow(t *testing.T) {
	t.Parallel()
	seen := map[EndpointKind]int{}
	for _, c := range endpointCases() {
		seen[c.kind]++
	}
	for k := EndpointInvalid + 1; k < endpointKindCount; k++ {
		switch seen[k] {
		case 1:
		case 0:
			t.Errorf("EndpointKind %d has no row in endpointCases; add one", k)
		default:
			t.Errorf("EndpointKind %d has %d rows in endpointCases; want exactly 1", k, seen[k])
		}
	}
	if len(endpointCases()) != int(endpointKindCount)-1 {
		t.Errorf("endpointCases has %d rows, want %d (one per kind except Invalid)",
			len(endpointCases()), int(endpointKindCount)-1)
	}
}

func TestEndpointMethodsMatchTheTable(t *testing.T) {
	t.Parallel()
	for _, c := range endpointCases() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e := c.build()
			if got := e.Display(); got != c.display {
				t.Errorf("Display() = %q, want %q", got, c.display)
			}
			if got := e.IsLive(); got != c.live {
				t.Errorf("IsLive() = %v, want %v", got, c.live)
			}
			if got := e.CacheTag(); got != c.cacheTag {
				t.Errorf("CacheTag() = %q, want %q", got, c.cacheTag)
			}
			ref := e.FileRef("some/path.go")
			if ref.Source != c.source {
				t.Errorf("FileRef().Source = %v, want %v", ref.Source, c.source)
			}
			if ref.Locator != c.locator {
				t.Errorf("FileRef().Locator = %q, want %q", ref.Locator, c.locator)
			}
			if ref.Path != "some/path.go" {
				t.Errorf("FileRef().Path = %q, want %q", ref.Path, "some/path.go")
			}
		})
	}
}

// TestInvalidEndpointPanics pins the new contract: an unset Endpoint is a
// programming error, loudly, rather than a silently empty commit.
func TestInvalidEndpointPanics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		call func(Endpoint)
	}{
		{"Display", func(e Endpoint) { _ = e.Display() }},
		{"FileRef", func(e Endpoint) { _ = e.FileRef("p") }},
		{"CacheTag", func(e Endpoint) { _ = e.CacheTag() }},
		{"IsLive", func(e Endpoint) { _ = e.IsLive() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Errorf("%s on a zero Endpoint did not panic", tc.name)
				}
			}()
			tc.call(Endpoint{})
		})
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-unified-links
go test ./internal/model/ -run 'TestEveryEndpointKindHasATableRow|TestEndpointMethodsMatchTheTable|TestInvalidEndpointPanics' -v
```

Expected: **compile failure** — `undefined: EndpointInvalid`,
`undefined: endpointKindCount`.

- [ ] **Step 4: Renumber the iota and add the sentinel**

In `internal/model/model.go`, replace the `const` block at line 261:

```go
const (
	// EndpointInvalid is the ZERO VALUE, and it is deliberately not a usable
	// endpoint: an Endpoint{} is an unset variable or an error return, never
	// "the working tree". Every method panics on it rather than guessing.
	EndpointInvalid EndpointKind = iota
	EndpointWorkTree                    // the working tree (unstaged)
	EndpointIndex                       // the index (staged)
	EndpointCommit                      // a commit, by Hash
	EndpointShelf                       // a shelved commit's frozen changed-file set, by ShelfID

	// endpointKindCount must stay LAST. It is the exhaustiveness bound:
	// endpoint_exhaustive_test.go walks EndpointInvalid+1 .. endpointKindCount
	// and fails for any kind with no table row, so adding a kind above this
	// line without adding a row breaks the build.
	endpointKindCount
)
```

- [ ] **Step 5: Give every switch explicit arms and a panicking default**

Still in `internal/model/model.go`, replace the three switch methods. Each
`default:` that meant "commit" becomes an explicit `case EndpointCommit:`, and
the new `default:` panics:

```go
// Display is the human label for an endpoint.
func (e Endpoint) Display() string {
	switch e.Kind {
	case EndpointWorkTree:
		return "Working Tree"
	case EndpointIndex:
		return "Staged"
	case EndpointShelf:
		id := e.ShelfID
		if len(id) > 9 {
			id = id[:9]
		}
		return "shelf #" + id + " (frozen)"
	case EndpointCommit:
		if len(e.Hash) > 7 {
			return e.Hash[:7]
		}
		return e.Hash
	default:
		panic(endpointKindBug("Display", e.Kind))
	}
}

// FileRef maps the endpoint to a resolvable file reference for path.
func (e Endpoint) FileRef(path string) FileRef {
	switch e.Kind {
	case EndpointWorkTree:
		return FileRef{Source: SourceUnstaged, Path: path}
	case EndpointIndex:
		return FileRef{Source: SourceStaged, Path: path}
	case EndpointShelf:
		return FileRef{Source: SourceShelf, Locator: e.ShelfID, Path: path}
	case EndpointCommit:
		return FileRef{Source: SourceCommit, Locator: e.Hash, Path: path}
	default:
		panic(endpointKindBug("FileRef", e.Kind))
	}
}

// IsLive reports whether the endpoint's content can change on disk (working
// tree or index) and therefore must never be cached.
func (e Endpoint) IsLive() bool {
	switch e.Kind {
	case EndpointWorkTree, EndpointIndex:
		return true
	case EndpointCommit, EndpointShelf:
		return false
	default:
		panic(endpointKindBug("IsLive", e.Kind))
	}
}

// CacheTag is a stable cache-key fragment for the endpoint (only meaningful
// when !IsLive()).
func (e Endpoint) CacheTag() string {
	switch e.Kind {
	case EndpointWorkTree:
		return "worktree"
	case EndpointIndex:
		return "index"
	case EndpointShelf:
		return "shelf:" + e.ShelfID
	case EndpointCommit:
		return e.Hash
	default:
		panic(endpointKindBug("CacheTag", e.Kind))
	}
}

// endpointKindBug is the one panic message shape. An invalid kind is always a
// programming error -- an unset variable reaching a method, or a kind added to
// the iota block without teaching the methods about it -- so it names both the
// method and the kind.
func endpointKindBug(method string, k EndpointKind) string {
	if k == EndpointInvalid {
		return "model.Endpoint." + method + ": endpoint is unset (EndpointInvalid); it was never given a kind"
	}
	return fmt.Sprintf("model.Endpoint.%s: unknown EndpointKind %d; add a case arm and a row in endpoint_exhaustive_test.go", method, k)
}
```

`fmt` is already imported by `internal/model/model.go` (used by
`FileAddress.Display`). Confirm with `head -20 internal/model/model.go`; add it
to the import block only if it is absent.

- [ ] **Step 6: Run the model tests**

```bash
go test ./internal/model/ -v
```

Expected: PASS, including the three new tests.

- [ ] **Step 7: Run the full unit suite to catch any caller that passed an unset endpoint**

```bash
./test.sh unit
```

Expected: PASS. **If anything panics with `endpoint is unset`, do not add a
guard at the panic site** — that is the trap this task exists to find. Report
the stack trace: a real caller is reading an endpoint it never set, which is a
bug in that caller, and the controller decides how to fix it.

- [ ] **Step 8: Commit**

```bash
git add internal/model/model.go internal/model/endpoint_exhaustive_test.go
git commit -m "refactor(model): the zero Endpoint is invalid, and every kind switch is exhaustive

EndpointWorkTree was iota 0, so model.Endpoint{} WAS a working tree -- and
Display/FileRef/CacheTag all ended in a default: arm that meant 'commit', so
any kind they had not been taught about silently became a commit with an empty
hash. Adding a kind would have been a wrong answer with no error, in three
methods.

EndpointInvalid now takes 0, each method has an explicit arm per kind and a
panicking default, and an endpointKindCount sentinel plus one table test make a
kind added without a row fail the build.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01GiF4qfVtboFEjZAFP1bJ27"
```

---

## Task 2: Validating constructors and `Bounded()`

Fields stay exported in this task, so nothing outside `model` changes yet. This
keeps the diff reviewable: constructors arrive with their tests, and Task 3
does the mechanical migration separately.

**Files:**
- Modify: `internal/model/model.go` (after the `Endpoint` struct)
- Modify: `internal/model/endpoint_test.go`

**Interfaces:**
- Consumes: `EndpointInvalid`, `endpointKindCount` from Task 1.
- Produces:
  - `func WorkTreeEndpoint() Endpoint`
  - `func IndexEndpoint() Endpoint`
  - `func CommitEndpoint(hash string) (Endpoint, error)`
  - `func ShelfEndpoint(id string) (Endpoint, error)`
  - `func (e Endpoint) Bounded() bool`
  - `var ErrEndpoint = errors.New("bad endpoint")` — every constructor error
    wraps it, so callers test with `errors.Is` rather than matching prose.

- [ ] **Step 1: Write the failing constructor tests**

Append to `internal/model/endpoint_test.go`:

```go
func TestCommitEndpointRejectsBadHashes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		hash string
	}{
		{"empty", ""},
		{"too short", "abc123"},                      // 6 < 7
		{"not hex", "zzzzzzz"},
		{"too long", strings.Repeat("a", 65)},        // 65 > 64
		{"has whitespace", "abc1234 "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := CommitEndpoint(tc.hash); !errors.Is(err, ErrEndpoint) {
				t.Errorf("CommitEndpoint(%q) error = %v, want one wrapping ErrEndpoint", tc.hash, err)
			}
		})
	}
}

func TestCommitEndpointAcceptsValidHashes(t *testing.T) {
	t.Parallel()
	for _, h := range []string{
		"abc1234",                      // 7, the minimum
		"e3b815eb61c32228c9963e667c0076d6d42308c3", // 40, sha-1
		strings.Repeat("a", 64),        // 64, sha-256
		"ABC1234",                      // upper-case hex is still hex
	} {
		e, err := CommitEndpoint(h)
		if err != nil {
			t.Fatalf("CommitEndpoint(%q): %v", h, err)
		}
		if e.Kind != EndpointCommit {
			t.Errorf("CommitEndpoint(%q).Kind = %v, want EndpointCommit", h, e.Kind)
		}
		if e.Hash != h {
			t.Errorf("CommitEndpoint(%q).Hash = %q, want %q", h, e.Hash, h)
		}
	}
}

func TestShelfEndpointRejectsAnEmptyID(t *testing.T) {
	t.Parallel()
	if _, err := ShelfEndpoint(""); !errors.Is(err, ErrEndpoint) {
		t.Errorf("ShelfEndpoint(\"\") error = %v, want one wrapping ErrEndpoint", err)
	}
}

func TestShelfEndpointAcceptsAnID(t *testing.T) {
	t.Parallel()
	e, err := ShelfEndpoint("wt-parser-9f3a1")
	if err != nil {
		t.Fatalf("ShelfEndpoint: %v", err)
	}
	if e.Kind != EndpointShelf || e.ShelfID != "wt-parser-9f3a1" {
		t.Errorf("ShelfEndpoint = %+v, want kind shelf and the id", e)
	}
}

func TestLiveEndpointConstructors(t *testing.T) {
	t.Parallel()
	if got := WorkTreeEndpoint(); got.Kind != EndpointWorkTree {
		t.Errorf("WorkTreeEndpoint().Kind = %v, want EndpointWorkTree", got.Kind)
	}
	if got := IndexEndpoint(); got.Kind != EndpointIndex {
		t.Errorf("IndexEndpoint().Kind = %v, want EndpointIndex", got.Kind)
	}
}

// TestBoundedMatchesTheSpecRule pins section 3.1: a shelf entry is a finite,
// enumerated set of paths; a tree, a tip and the index are not.
func TestBoundedMatchesTheSpecRule(t *testing.T) {
	t.Parallel()
	for _, c := range endpointCases() {
		want := c.kind == EndpointShelf
		if got := c.build().Bounded(); got != want {
			t.Errorf("%s: Bounded() = %v, want %v", c.name, got, want)
		}
	}
}

func TestBoundedPanicsOnAnUnsetEndpoint(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("Bounded() on a zero Endpoint did not panic")
		}
	}()
	_ = Endpoint{}.Bounded()
}
```

Add `"errors"` and `"strings"` to that file's import block if absent.

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test ./internal/model/ -run 'Endpoint' -v
```

Expected: compile failure — `undefined: CommitEndpoint`, `undefined: ErrEndpoint`,
`e.Bounded undefined`.

- [ ] **Step 3: Add the constructors and `Bounded()`**

In `internal/model/model.go`, directly after the `Endpoint` struct:

```go
// ErrEndpoint wraps every constructor refusal, so a caller can tell a
// malformed endpoint from a git failure without matching on prose.
var ErrEndpoint = errors.New("bad endpoint")

// WorkTreeEndpoint names the working tree. It cannot fail: there is nothing
// to validate.
func WorkTreeEndpoint() Endpoint { return Endpoint{Kind: EndpointWorkTree} }

// IndexEndpoint names the index. It cannot fail.
func IndexEndpoint() Endpoint { return Endpoint{Kind: EndpointIndex} }

// CommitEndpoint names the tree at a commit. hash must be 7..64 hex
// characters -- 64, not 40, because a sha-256 repository's commit ids are 64
// hex characters. The bound matches model.ParseLink's, so a link and an
// endpoint never disagree about what a commit id looks like.
func CommitEndpoint(hash string) (Endpoint, error) {
	if len(hash) < 7 || len(hash) > 64 {
		return Endpoint{}, fmt.Errorf("%w: commit hash must be 7..64 characters, got %d", ErrEndpoint, len(hash))
	}
	for i := 0; i < len(hash); i++ {
		if !isHexDigit(hash[i]) {
			return Endpoint{}, fmt.Errorf("%w: commit hash must be hex, got %q", ErrEndpoint, hash)
		}
	}
	return Endpoint{Kind: EndpointCommit, Hash: hash}, nil
}

// ShelfEndpoint names a shelved commit's frozen changed-file set.
func ShelfEndpoint(id string) (Endpoint, error) {
	if id == "" {
		return Endpoint{}, fmt.Errorf("%w: shelf id is required", ErrEndpoint)
	}
	return Endpoint{Kind: EndpointShelf, ShelfID: id}, nil
}

func isHexDigit(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

// Bounded reports whether the endpoint evaluates to a FINITE, enumerated set
// of paths rather than every file in the repository (spec section 3.1). A
// shelf entry carries its own member list; a tree, a tip and the index do not.
//
// DELIBERATE DEVIATION FROM THE SPEC. Section 4.0 sketches a stored `bounded
// bool` field, "computed ONCE at construction, never re-derived". A switch on
// the kind is used instead, because boundedness is a total function of the
// kind alone -- for every kind in this plan AND for the two 1b adds
// (EndpointRef is unbounded, EndpointPair is bounded). A stored field would
// duplicate the kind and introduce a second thing that can disagree with it.
// The spec's actual requirement -- that the rule live in exactly one place and
// no consumer re-derive it -- is met by this one switch, pinned by
// TestBoundedMatchesTheSpecRule. If 1b finds a kind whose boundedness is NOT
// determined by the kind, revisit this.
func (e Endpoint) Bounded() bool {
	switch e.Kind {
	case EndpointShelf:
		return true
	case EndpointWorkTree, EndpointIndex, EndpointCommit:
		return false
	default:
		panic(endpointKindBug("Bounded", e.Kind))
	}
}
```

Add `"errors"` to `internal/model/model.go`'s import block if absent.

- [ ] **Step 4: Run the model tests**

```bash
go test ./internal/model/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/model/model.go internal/model/endpoint_test.go
git commit -m "feat(model): validating Endpoint constructors and Bounded()

Constructors are the funnel: CommitEndpoint checks the 7..64 hex bound that
ParseLink already uses, so a link and an endpoint cannot disagree about what a
commit id looks like. Bounded() encodes spec section 3.1 once, so no caller
re-derives the rule.

Fields stay exported here; the call-site migration is its own commit.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01GiF4qfVtboFEjZAFP1bJ27"
```

---

## Task 3: Migrate every construction site to a constructor

Mechanical and compiler-checked at the end. Fields are still exported, so this
task can be done file by file with a green build throughout.

**Files** (construction sites, non-test — 54 across 14):

`internal/cli/compare.go` (10) · `internal/tui/wip_rows.go` (7) ·
`internal/mcp/compare.go` (7) · `internal/tui/commit_scope.go` (6) ·
`internal/web/compare.go` (4) · `internal/tui/session_snapshot.go` (4) ·
`internal/tui/files_view.go` (4) · `internal/domain/compare_entries.go` (4) ·
`internal/tui/versions_popup.go` (2) · `internal/tui/file_finder.go` (2) ·
`internal/tui/branch_compare.go` (2) · `internal/tui/bookmark_popup.go` (2) ·
`internal/domain/version_preview.go` (2) · `internal/domain/preview.go` (2)

**Test files** (~103 across 21) — migrate these too; list them with:

```bash
grep -rln 'model\.Endpoint{\|Endpoint{Kind:' internal/ cmd/ | grep _test
```

**Interfaces:**
- Consumes: the four constructors and `ErrEndpoint` from Task 2.
- Produces: no new API. After this task, no file outside `internal/model`
  contains a `model.Endpoint{` composite literal except the bare
  `model.Endpoint{}` error/reset returns listed below.

- [ ] **Step 1: Respell the literals**

Apply these mappings everywhere:

| before | after |
|---|---|
| `model.Endpoint{Kind: model.EndpointWorkTree}` | `model.WorkTreeEndpoint()` |
| `model.Endpoint{Kind: model.EndpointIndex}` | `model.IndexEndpoint()` |
| `model.Endpoint{Kind: model.EndpointCommit, Hash: h}` | `model.CommitEndpoint(h)` — **returns an error; see Step 2** |
| `model.Endpoint{Kind: model.EndpointShelf, ShelfID: id}` | `model.ShelfEndpoint(id)` — returns an error |

**Leave `model.Endpoint{}` alone.** The 14 bare zero-value sites listed under
"Current state" are error returns and one guarded reset; they must stay as
`model.Endpoint{}`, which now correctly reads as "unset".

Inside `internal/model`'s own tests, drop the `model.` prefix
(`WorkTreeEndpoint()`, not `model.WorkTreeEndpoint()`).

- [ ] **Step 2: Handle the two fallible constructors**

`CommitEndpoint` and `ShelfEndpoint` return an error. At each site:

- **A function that already returns `error`:** propagate it.

  ```go
  e, err := model.CommitEndpoint(sha)
  if err != nil {
      return model.Endpoint{}, err
  }
  ```

- **A function that cannot return an error** (several TUI sites): the hash came
  from git or from a store, so a failure is a programming error, not a user
  error. Use the `must` helper — add it once per package that needs it, in the
  file being edited:

  ```go
  // mustCommitEndpoint is for hashes that came from git or a gg store, where a
  // malformed one is a bug in the caller rather than bad user input.
  func mustCommitEndpoint(hash string) model.Endpoint {
      e, err := model.CommitEndpoint(hash)
      if err != nil {
          panic(err)
      }
      return e
  }
  ```

- **In tests:** use the `must` form with `t.Helper()`:

  ```go
  func mustCommit(t *testing.T, hash string) model.Endpoint {
      t.Helper()
      e, err := model.CommitEndpoint(hash)
      if err != nil {
          t.Fatalf("CommitEndpoint(%q): %v", hash, err)
      }
      return e
  }
  ```

**Do not silently swallow the error** (`e, _ := model.CommitEndpoint(h)`)
anywhere. That reintroduces exactly the silent-wrong-value problem this plan
removes.

- [ ] **Step 3: Verify no composite literals remain**

```bash
grep -rn 'model\.Endpoint{[^}]' internal/ cmd/
grep -rn 'Endpoint{Kind:' internal/ cmd/
```

Expected: both print nothing. (The first pattern deliberately excludes the bare
`model.Endpoint{}`, which is allowed.)

- [ ] **Step 4: Build and test**

```bash
go build ./... && ./test.sh unit
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor: build every Endpoint through a constructor

54 composite literals outside tests and ~103 in tests became constructor
calls, so a malformed endpoint is now refused where it is built rather than
discovered as a wrong diff later. The bare model.Endpoint{} error returns stay
as they are -- under EndpointInvalid they correctly read as 'unset'.

Fallible sites either propagate the error or use a package-local must* helper
where the hash came from git or a gg store; no site discards the error.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01GiF4qfVtboFEjZAFP1bJ27"
```

---

## Task 4: Unexport the fields, add accessors

The atomic step, and the one that delivers the guarantee: after it, a
`model.Endpoint` in hand can only have come from a constructor. The compiler
finds every remaining reader.

`Kind` the field and `Kind()` the method cannot coexist, so the rename and the
accessors land together.

**Files:**
- Modify: `internal/model/model.go`
- Modify: every file the compiler names in Step 2 (~67 field reads outside
  tests, plus tests)

**Interfaces:**
- Consumes: everything from Tasks 1-3.
- Produces:
  - `func (e Endpoint) Kind() EndpointKind`
  - `func (e Endpoint) Hash() string` — `""` unless the kind is `EndpointCommit`
  - `func (e Endpoint) ShelfID() string` — `""` unless the kind is `EndpointShelf`
  - `func (e Endpoint) Valid() bool` — `e.kind != EndpointInvalid`

- [ ] **Step 1: Unexport the fields and add the accessors**

In `internal/model/model.go`:

```go
// Endpoint names one side of a whole-tree comparison.
//
// Its fields are unexported on purpose: the only way to make one is a
// constructor (WorkTreeEndpoint, IndexEndpoint, CommitEndpoint,
// ShelfEndpoint), each of which validates. Holding an Endpoint therefore
// PROVES it is consistent, and no consumer re-checks. The zero value is
// EndpointInvalid -- an unset variable or an error return, never a usable
// endpoint.
type Endpoint struct {
	kind    EndpointKind
	hash    string // commit hash when kind == EndpointCommit; "" otherwise
	shelfID string // shelf entry id when kind == EndpointShelf; "" otherwise
}

// Kind is the endpoint's kind. EndpointInvalid means unset.
func (e Endpoint) Kind() EndpointKind { return e.kind }

// Valid reports whether the endpoint came from a constructor.
func (e Endpoint) Valid() bool { return e.kind != EndpointInvalid }

// Hash is the commit id, or "" for any other kind.
func (e Endpoint) Hash() string { return e.hash }

// ShelfID is the shelf entry id, or "" for any other kind.
func (e Endpoint) ShelfID() string { return e.shelfID }
```

Then update the five methods and four constructors inside `model` to use the
lower-case field names (`e.kind`, `e.hash`, `e.shelfID`).

- [ ] **Step 2: Let the compiler find every reader**

```bash
go build ./... 2>&1 | head -60
```

Expected: a list of `e.Kind undefined (type model.Endpoint has no field or
method Kind, but does have method Kind)`-style errors. Work through them:

| before | after |
|---|---|
| `e.Kind` | `e.Kind()` |
| `e.Hash` | `e.Hash()` |
| `e.ShelfID` | `e.ShelfID()` |

Repeat `go build ./...` until clean, then:

```bash
go vet ./... && gofmt -l internal cmd
```

Expected: no output from either.

- [ ] **Step 3: Rebuild `endpointCases()` through the constructors**

`internal/model/endpoint_exhaustive_test.go` is `package model`, so its
`build` funcs would still compile against the lower-case fields — but that
would leave the one table that defines the contract reaching around the
funnel. Respell each row's `build` to use its constructor. `CommitEndpoint`
and `ShelfEndpoint` return errors, so the field gains a `*testing.T`:

```go
	build    func(*testing.T) Endpoint
```

```go
		{
			kind:  EndpointWorkTree,
			name:  "worktree",
			build: func(*testing.T) Endpoint { return WorkTreeEndpoint() },
			// ... display/live/cacheTag/source/locator unchanged
		},
		{
			kind:  EndpointIndex,
			name:  "index",
			build: func(*testing.T) Endpoint { return IndexEndpoint() },
			// ...
		},
		{
			kind: EndpointCommit,
			name: "commit",
			build: func(t *testing.T) Endpoint {
				t.Helper()
				e, err := CommitEndpoint("abc1234def5678")
				if err != nil {
					t.Fatalf("CommitEndpoint: %v", err)
				}
				return e
			},
			// ...
		},
		{
			kind: EndpointShelf,
			name: "shelf",
			build: func(t *testing.T) Endpoint {
				t.Helper()
				e, err := ShelfEndpoint("wt-parser-9f3a1")
				if err != nil {
					t.Fatalf("ShelfEndpoint: %v", err)
				}
				return e
			},
			// ...
		},
```

Update the two call sites accordingly: `c.build()` becomes `c.build(t)` in
`TestEndpointMethodsMatchTheTable` (Task 1) and in
`TestBoundedMatchesTheSpecRule` (Task 2). `TestEveryEndpointKindHasATableRow`
never calls `build`, so it is unchanged.

- [ ] **Step 4: Fix the remaining test files**

```bash
go test ./... 2>&1 | grep -E '^(#|\S+\.go:)' | head -60
```

Work through the same three accessor mappings until it compiles.

- [ ] **Step 5: Prove the guarantee holds**

```bash
grep -rn 'Endpoint{[Kk]ind\|\.Kind =\|\.Hash =\|\.ShelfID =' internal/ cmd/
```

Expected: nothing. No code outside `internal/model` can set an Endpoint field.

- [ ] **Step 6: Run the full unit suite**

```bash
./test.sh unit
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "refactor(model): Endpoint's fields are unexported; constructors are the only way in

Holding a model.Endpoint now proves it is consistent -- the fields are
unreachable, so every value came from a validating constructor, and no consumer
re-checks. Kind/Hash/ShelfID become accessors; Valid() reports whether the
value was ever given a kind.

This completes plan 1a: the three traps (54 raw literals, a zero value that was
silently a working tree, and default:-means-commit in three methods) are gone
before plan 1b adds EndpointRef and EndpointPair on top.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01GiF4qfVtboFEjZAFP1bJ27"
```

---

## Task 5: Documentation

**Files:**
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: the completed refactor.
- Produces: nothing code-facing.

- [ ] **Step 1: Add the CHANGELOG entry**

Under the current unreleased heading, matching the surrounding entries' voice:

```markdown
- **`model.Endpoint` cannot be built in an invalid state.** Its fields are
  unexported and the only way in is a validating constructor, so holding one
  proves it is consistent. The zero value is now `EndpointInvalid` rather than
  a silently-valid working tree, and every switch over `EndpointKind` has an
  explicit arm per kind plus a panicking default — previously `Display`,
  `FileRef` and `CacheTag` all fell through to the commit case, so a kind they
  had not been taught about became a commit with an empty hash. One table test
  enumerates every kind, so a kind added without a row fails the build.
```

No `README.md` change: this is internal, with no user-visible surface.
No `internal/agentskill/using-gg.md` change: the CLI surface is unchanged.
No `CLAUDE.md` change: no package responsibility or convention moved.

- [ ] **Step 2: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs(changelog): Endpoint hardening

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01GiF4qfVtboFEjZAFP1bJ27"
```

---

## Done when

- [ ] `grep -rn 'model\.Endpoint{[^}]' internal/ cmd/` prints nothing
- [ ] `grep -rn 'Endpoint{Kind:' internal/ cmd/` prints nothing
- [ ] `go build ./... && go vet ./... && gofmt -l internal cmd` is silent
- [ ] `./test.sh unit` passes
- [ ] `./test.sh race` passes — **the controller runs this once, at the end**
- [ ] Adding a fifth kind to the iota block and running
      `go test ./internal/model/` fails with "has no row in endpointCases"
      (verify by hand, then revert the experiment)
